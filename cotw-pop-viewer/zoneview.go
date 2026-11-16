package cotw

import (
	"fmt"
	"math"
	"sort"
)

// ZoneTypeName names a need zone type: 1 feeding, 2 drinking, 3 resting. The
// game stores the number only; the mapping was established by matching
// discovered windows against the published timetables, which agree on 157 of
// 158 comparable zones.
func ZoneTypeName(zoneType int32) string {
	switch zoneType {
	case 1:
		return "feeding"
	case 2:
		return "drinking"
	case 3:
		return "resting"
	}
	return ""
}

// String renders a zone the way fmt prints it: id, activity, position and the
// window, so printing a group's zone list is readable without unpacking fields.
func (z *NeedZone) String() string {
	if z == nil {
		return "<nil zone>"
	}
	return fmt.Sprintf("#%d %s at (%.0f, %.0f) %s found=%t",
		z.IDU, ZoneTypeName(z.Type), z.Position.X, z.Position.Z, windowsText(z.Windows, z.StartHour, z.EndHour), z.Discovered)
}

// String renders a ZoneInfo the same way as a NeedZone.
func (z ZoneInfo) String() string {
	return fmt.Sprintf("#%d %s at (%.0f, %.0f) %s found=%t",
		z.ID, ZoneTypeName(z.Type), z.Position.X, z.Position.Z, windowsText(z.Windows, z.StartHour, z.EndHour), z.Discovered)
}

// windowsText renders a window list for printing.
func windowsText(windows []HourWindow, start, end float64) string {
	switch len(windows) {
	case 0:
		return "hours unknown"
	case 1:
		return fmt.Sprintf("%02.0f:00-%02.0f:00", start, end)
	}
	return fmt.Sprintf("%d windows (%02.0f:00 first)", len(windows), windows[0].Start)
}

// ZoneInfo is one need zone of a reserve: discovered or not, with the geometry
// the game ships.
type ZoneInfo struct {
	// ID is the need zone id. It is the same id a group lists in
	// NeedZoneGuids and the one a discovered zone stores.
	ID uint32 `json:"id"`
	// Type is the need zone type (1..3).
	Type int32 `json:"type"`
	// Position and Extent come from the game's shipped geometry.
	Position Vec3 `json:"position"`
	Extent   Vec3 `json:"extent"`
	// HasGeometry is false when the id is not in the shipped bitmaps, in which
	// case Position and Extent are zero.
	HasGeometry bool `json:"has_geometry"`
	// Discovered reports whether the player has found this zone (it is then
	// also present in Reserve.NeedZones).
	Discovered bool `json:"discovered"`
	// ScheduleIndex, StartHour and EndHour are the zone's daily window. For a
	// discovered zone they are its stored values; for an undiscovered one they
	// are derived from the species' schedule when that is unambiguous, and
	// left zero otherwise.
	ScheduleIndex int8    `json:"schedule_index"`
	StartHour     float64 `json:"start_hour"`
	EndHour       float64 `json:"end_hour"`
	// Windows lists every window of this zone's type. It has one entry when the
	// window is unambiguous (StartHour/EndHour are then also set); a species
	// with several zones of the same type has one window per such slot, and the
	// zone's own slot cannot be told apart until it is discovered.
	Windows []HourWindow `json:"windows,omitempty"`
	// HourSource says where the window came from: HourSourceSave,
	// HourSourceOptions, HourSourceLearned, HourSourceWiki, or HourSourceNone.
	HourSource string `json:"hour_source,omitempty"`
	// SpeciesKey and SpeciesName name the species that uses the zone, taken
	// from the groups that reference it.
	SpeciesKey  string `json:"species_key,omitempty"`
	SpeciesName string `json:"species_name,omitempty"`
	// Groups are the groups that use this zone.
	Groups []GroupRef `json:"groups,omitempty"`
}

// Zones returns every need zone of a reserve, including zones the player has
// not discovered, with the geometry shipped in the game's spawn bitmaps.
//
// The zone set comes from the groups: each group lists the zone ids it uses, so
// the reserve's zones are the union of those ids and the discovered zones. A few
// zone ids are used by more than one reserve's layout; for those, the record
// nearest the spawn areas of the groups referencing the zone is chosen.
func (s *Save) Zones(reserveIndex int) []ZoneInfo {
	r := s.Reserve(reserveIndex)
	if r == nil || s.game == nil {
		return nil
	}
	resolver := s.newHourResolver()

	byID := make(map[uint32]*ZoneInfo, 256)
	order := make([]uint32, 0, 256)
	get := func(id uint32) *ZoneInfo {
		if zi, ok := byID[id]; ok {
			return zi
		}
		zi := &ZoneInfo{ID: id}
		byID[id] = zi
		order = append(order, id)
		return zi
	}

	// Spawn-area centres of the groups using each zone, used to pick the right
	// record when an id exists in several reserves' layouts.
	spawnX := map[uint32]float64{}
	spawnZ := map[uint32]float64{}
	spawnN := map[uint32]int{}

	for _, sp := range r.Species {
		for _, g := range sp.Groups {
			for _, id := range g.NeedZoneGuids {
				zi := get(id)
				ref := GroupRef{SpeciesIndex: sp.Index, SpeciesKey: sp.SpeciesKey, GroupIndex: g.Index}
				if !containsGroupRef(zi.Groups, ref) {
					zi.Groups = append(zi.Groups, ref)
				}
				if zi.SpeciesKey == "" && sp.SpeciesKey != "" {
					zi.SpeciesKey, zi.SpeciesName = sp.SpeciesKey, sp.SpeciesName
				}
				if g.SpawnArea != nil {
					spawnX[id] += g.SpawnArea.Position.X
					spawnZ[id] += g.SpawnArea.Position.Z
					spawnN[id]++
				}
			}
		}
	}

	for _, z := range r.NeedZones {
		zi := get(z.IDU)
		zi.Discovered = true
		zi.ScheduleIndex = z.ScheduleIndex
		zi.StartHour, zi.EndHour = z.StartHour, z.EndHour
		if z.EndHour != z.StartHour {
			zi.Windows = []HourWindow{{Start: z.StartHour, End: z.EndHour}}
			zi.HourSource = HourSourceSave
		}
		if zi.Type == 0 {
			zi.Type = z.Type
		}
		if zi.SpeciesKey == "" {
			zi.SpeciesKey, zi.SpeciesName = z.AnimalSpeciesKey, z.AnimalSpeciesName
		}
	}

	out := make([]ZoneInfo, 0, len(order))
	for _, id := range order {
		zi := byID[id]
		geom, ok := s.zoneGeometryFor(id, spawnX[id]/float64(max(spawnN[id], 1)), spawnZ[id], spawnN[id] > 0)
		if ok {
			zi.Position = geom.Position
			zi.Extent = geom.Extent
			zi.HasGeometry = true
			if zi.Type == 0 {
				zi.Type = geom.Type
			}
		}
		// Fill an undiscovered zone's window from the best available timetable:
		// this save's discoveries, then an earlier save's, then the published
		// fallback (Options.ZoneHours overrides all three).
		if !zi.Discovered && len(zi.Windows) == 0 && zi.SpeciesKey != "" {
			if slots, source := resolver.windows(r.Key, zi.SpeciesKey, zi.Type); len(slots) > 0 {
				zi.HourSource = source
				zi.Windows = make([]HourWindow, 0, len(slots))
				for _, sl := range slots {
					zi.Windows = append(zi.Windows, HourWindow{Start: sl.Start, End: sl.End})
				}
				if len(slots) == 1 {
					zi.ScheduleIndex = slots[0].Index
					zi.StartHour, zi.EndHour = slots[0].Start, slots[0].End
				}
			}
		}
		out = append(out, *zi)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SpeciesKey != out[j].SpeciesKey {
			return out[i].SpeciesKey < out[j].SpeciesKey
		}
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// zoneGeometryFor returns the record for a zone id, choosing between several
// candidates by proximity to a reference point (the mean spawn-area centre of
// the groups that use the zone).
func (s *Save) zoneGeometryFor(id uint32, refX, refZ float64, haveRef bool) (ZoneGeometry, bool) {
	candidates := s.game.ZoneCandidates(id)
	switch {
	case len(candidates) == 0:
		return ZoneGeometry{}, false
	case len(candidates) == 1 || !haveRef:
		geom, ok := s.game.ZoneGeometry(id)
		return geom, ok
	}
	best := candidates[0]
	bestDist := math.Hypot(best.Position.X-refX, best.Position.Z-refZ)
	for _, c := range candidates[1:] {
		if d := math.Hypot(c.Position.X-refX, c.Position.Z-refZ); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best, true
}

// ScheduleSlot is one window in a species' daily schedule.
type ScheduleSlot struct {
	Index int8    `json:"index"`
	Type  int32   `json:"type"`
	Start float64 `json:"start_hour"`
	End   float64 `json:"end_hour"`
}

// SpeciesSchedule is the daily need-zone schedule of one species.
//
// The schedule belongs to the species, not to a single zone: every zone the
// player has discovered agrees on which slot has which type and hours, on every
// reserve.
type SpeciesSchedule struct {
	SpeciesKey string         `json:"species_key"`
	Slots      []ScheduleSlot `json:"slots"`
}

// slot returns the slot of the given zone type, when the species has exactly
// one slot of that type.
func (s speciesSchedules) slot(speciesKey string, zoneType int32) (ScheduleSlot, bool) {
	sch := s[speciesKey]
	if sch == nil {
		return ScheduleSlot{}, false
	}
	var found ScheduleSlot
	n := 0
	for _, sl := range sch.Slots {
		if sl.Type == zoneType {
			found = sl
			n++
		}
	}
	return found, n == 1
}

type speciesSchedules map[string]*SpeciesSchedule

// scheduleEvidence is the timetable evidence a save carries, both per reserve
// and per species.
//
// A species can have different windows on different reserves (the published
// tables disagree for whitetail deer, coyote, mule deer, gray wolf and a few
// others), so the reserve's own discoveries always win. The species-wide entry
// is kept only where every reserve that revealed the species agrees.
type scheduleEvidence struct {
	byReserve map[string]map[string][]ScheduleSlot
	bySpecies map[string][]ScheduleSlot
}

// scheduleEvidence collects what each discovered zone revealed.
func (s *Save) scheduleEvidence() scheduleEvidence {
	type slotKey struct {
		reserve string
		species string
		index   int8
	}
	seen := map[slotKey]map[[3]float64]bool{}
	for _, r := range s.Reserves {
		for _, z := range r.NeedZones {
			key := z.AnimalSpeciesKey
			if key == "" {
				continue
			}
			k := slotKey{r.Key, key, z.ScheduleIndex}
			if seen[k] == nil {
				seen[k] = map[[3]float64]bool{}
			}
			seen[k][[3]float64{float64(z.Type), z.StartHour, z.EndHour}] = true
		}
	}

	// Per reserve: keep a slot only when every discovered zone in it agrees.
	type pairKey struct{ reserve, species string }
	indexes := map[pairKey][]int8{}
	for k := range seen {
		p := pairKey{k.reserve, k.species}
		indexes[p] = append(indexes[p], k.index)
	}
	byReserve := map[string]map[string][]ScheduleSlot{}
	for p, idxs := range indexes {
		sort.Slice(idxs, func(i, j int) bool { return idxs[i] < idxs[j] })
		for _, idx := range idxs {
			values := seen[slotKey{p.reserve, p.species, idx}]
			if len(values) != 1 {
				continue
			}
			for v := range values {
				if byReserve[p.reserve] == nil {
					byReserve[p.reserve] = map[string][]ScheduleSlot{}
				}
				byReserve[p.reserve][p.species] = append(byReserve[p.reserve][p.species], ScheduleSlot{
					Index: idx, Type: int32(v[0]), Start: v[1], End: v[2],
				})
			}
		}
	}

	// Per species: keep a slot only when every reserve that revealed it agrees.
	bySpecies := map[string][]ScheduleSlot{}
	speciesIndexes := map[string]map[int8][]ScheduleSlot{}
	for _, bySpecies := range byReserve {
		for sp, slots := range bySpecies {
			if speciesIndexes[sp] == nil {
				speciesIndexes[sp] = map[int8][]ScheduleSlot{}
			}
			for _, sl := range slots {
				speciesIndexes[sp][sl.Index] = append(speciesIndexes[sp][sl.Index], sl)
			}
		}
	}
	for sp, byIndex := range speciesIndexes {
		idxs := make([]int8, 0, len(byIndex))
		for idx := range byIndex {
			idxs = append(idxs, idx)
		}
		sort.Slice(idxs, func(i, j int) bool { return idxs[i] < idxs[j] })
		for _, idx := range idxs {
			cands := byIndex[idx]
			first := cands[0]
			agree := true
			for _, c := range cands[1:] {
				if c.Type != first.Type || c.Start != first.Start || c.End != first.End {
					agree = false
					break
				}
			}
			if agree {
				bySpecies[sp] = append(bySpecies[sp], first)
			}
		}
	}
	return scheduleEvidence{byReserve: byReserve, bySpecies: bySpecies}
}

// speciesSchedule builds every species' timetable from the discovered zones,
// limited to the slots that are the same on every reserve.
func (s *Save) speciesSchedule() speciesSchedules {
	out := speciesSchedules{}
	for key, slots := range s.scheduleEvidence().bySpecies {
		out[key] = &SpeciesSchedule{SpeciesKey: key, Slots: slots}
	}
	return out
}

// hourResolver answers what windows a zone type has, from the best source
// available: an explicit option, this save, an earlier save, the published
// table — in that order.
type hourResolver struct {
	evidence scheduleEvidence
	options  map[string][]ScheduleSlot
	learned  *hourStore
}

func (s *Save) newHourResolver() *hourResolver {
	return &hourResolver{
		evidence: s.scheduleEvidence(),
		options:  s.opts.ZoneHours,
		learned:  s.hours,
	}
}

func (r *hourResolver) windows(reserveKey, speciesKey string, zoneType int32) ([]ScheduleSlot, string) {
	if slots, ok := slotsForKey(r.options, reserveKey, speciesKey); ok {
		if w := windowsOfType(slots, zoneType); len(w) > 0 {
			return w, HourSourceOptions
		}
	}
	if bySpecies, ok := r.evidence.byReserve[reserveKey]; ok {
		if slots, ok := bySpecies[speciesKey]; ok {
			if w := windowsOfType(slots, zoneType); len(w) > 0 {
				return w, HourSourceSave
			}
		}
	}
	if slots, ok := r.evidence.bySpecies[speciesKey]; ok {
		if w := windowsOfType(slots, zoneType); len(w) > 0 {
			return w, HourSourceSave
		}
	}
	if slots, ok := r.learned.hours(reserveKey, speciesKey); ok {
		if w := windowsOfType(slots, zoneType); len(w) > 0 {
			return w, HourSourceLearned
		}
	}
	if slots, ok := PublishedZoneHoursFor(reserveKey, speciesKey); ok {
		if w := windowsOfType(slots, zoneType); len(w) > 0 {
			return w, HourSourceWiki
		}
	}
	return nil, HourSourceNone
}

// slotsForKey looks an override up by reserve/species, then by species alone.
func slotsForKey(m map[string][]ScheduleSlot, reserveKey, speciesKey string) ([]ScheduleSlot, bool) {
	if len(m) == 0 {
		return nil, false
	}
	if slots, ok := m[reserveKey+"/"+speciesKey]; ok && len(slots) > 0 {
		return slots, true
	}
	if slots, ok := m[speciesKey]; ok && len(slots) > 0 {
		return slots, true
	}
	return nil, false
}

func windowsOfType(slots []ScheduleSlot, zoneType int32) []ScheduleSlot {
	var out []ScheduleSlot
	for _, sl := range slots {
		if sl.Type == zoneType {
			out = append(out, sl)
		}
	}
	return out
}

// learnZoneHours records this save's timetables in the schedule store so later
// parses need fewer discoveries.
func (s *Save) learnZoneHours() {
	if s.hours == nil || s.opts.DisableScheduleLearning || s.game == nil {
		return
	}
	ev := s.scheduleEvidence()
	changed := false
	for reserve, bySpecies := range ev.byReserve {
		for sp, slots := range bySpecies {
			if s.hours.learn(reserve, sp, slots) {
				changed = true
			}
		}
	}
	for sp, slots := range ev.bySpecies {
		if prev, ok := s.hours.Species[sp]; !ok || !sameSlots(prev, slots) {
			s.hours.Species[sp] = copySlots(slots)
			changed = true
		}
	}
	if !changed {
		return
	}
	if err := s.hours.save(hourStorePath(s.game.info.CachePath), s.game.Version()); err != nil {
		s.Warnings = append(s.Warnings, fmt.Sprintf("schedule store: %v", err))
	}
}

// SpeciesSchedules returns the daily need-zone schedule of every species the
// save has enough discovered zones for.
func (s *Save) SpeciesSchedules() []SpeciesSchedule {
	all := s.speciesSchedule()
	out := make([]SpeciesSchedule, 0, len(all))
	for _, sch := range all {
		out = append(out, *sch)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SpeciesKey < out[j].SpeciesKey })
	return out
}

// attachSpawnAreas gives every group the centre and extent of the spawn area the
// game ships for its SpawnAreaID.
func attachSpawnAreas(reserves []*Reserve, game *Game) {
	if game == nil || len(game.spawnAreas) == 0 {
		return
	}
	for _, r := range reserves {
		for _, sp := range r.Species {
			for _, g := range sp.Groups {
				if g == nil {
					continue
				}
				if geom, ok := game.SpawnAreaGeometry(g.SpawnAreaID); ok {
					copied := geom
					g.SpawnArea = &copied
				}
			}
		}
	}
}
