package cotw

import "sort"

// Group timetables: which need zone a group is at, and when.
//
// The game stores neither a timetable nor a slot per zone as data. What it
// stores, for every group, is the ordered list of the zone ids that group uses
// (NeedZonePathGuids), and that list is the group's day: entry i is the zone the
// group is at during slot i of its species' timetable.
//
// Two checks on the reference save show the order is the slot order:
//
//   - For every zone exactly one group uses, and whose group uses it once, the
//     NeedZoneScheduleIndex the save recorded for it equals the position it
//     occupies in that group's path (94 of 94).
//   - For every group entry the timetable covers, the activity type the shipped
//     geometry gives the zone at that position equals the timetable's type for
//     the slot the position takes (104,270 of 104,270).
//
// The slot a zone's own record carries is the slot it was found in, which for a
// zone several groups share (about a third of them) can be another group's slot:
// of 278 discovered entries in shared zones, 123 name a slot other than the one
// the group using them is in (GroupSlot.RecordedStartHour keeps that record
// beside the slot's own window).
//
// The windows themselves are not shipped: this file takes them from the species
// timetable, slot by slot, from the best source that covers that slot (see
// hourResolver.timetable).

// slotWindow is one slot of a species' daily timetable with its provenance.
type slotWindow struct {
	slot   int8
	typ    int32
	start  float64
	end    float64
	source string
}

// timetable returns a species' daily timetable on a reserve as one entry per
// slot, keyed by slot index.
//
// Sources are merged slot by slot, most specific first. The ordering matters:
// this reserve's own discoveries and the published page for *this reserve* come
// before anything species-wide, because a timetable is per reserve and
// species-wide agreement (the store's species entry, or discoveries on other
// reserves) would otherwise leak one reserve's variant into another - that is
// how Delta's raccoon picked up the New England variant.
func (r *hourResolver) timetable(reserveKey, speciesKey string) []slotWindow {
	bySlot := map[int8]slotWindow{}
	add := func(slots []ScheduleSlot, source string) {
		for _, sl := range slots {
			if _, taken := bySlot[sl.Index]; taken {
				continue
			}
			bySlot[sl.Index] = slotWindow{slot: sl.Index, typ: sl.Type, start: sl.Start, end: sl.End, source: source}
		}
	}
	// An explicit override names the windows it wants.
	if slots, ok := slotsForKey(r.options, reserveKey, speciesKey); ok {
		add(numberedCopy(slots), HourSourceOptions)
	}
	// This reserve's own discoveries: exact for the slots they cover.
	if bySpecies, ok := r.evidence.byReserve[reserveKey]; ok {
		if slots, ok := bySpecies[speciesKey]; ok {
			add(slots, HourSourceSave)
		}
	}
	// An earlier parse's discoveries on this reserve.
	if slots, ok := r.learned.reserveHours(reserveKey, speciesKey); ok {
		add(slots, HourSourceLearned)
	}
	// The published page for this reserve: its own variant, or the row the page
	// shows for it.
	if slots, ok := PublishedZoneHoursFor(reserveKey, speciesKey); ok {
		add(slots, HourSourceWiki)
	}
	// Only now the species-wide fallbacks, for slots no reserve-specific source
	// covers: discoveries every revealing reserve agreed on, then the store.
	if slots, ok := r.evidence.bySpecies[speciesKey]; ok {
		add(slots, HourSourceSave)
	}
	if slots, ok := r.learned.speciesHours(reserveKey, speciesKey); ok {
		add(slots, HourSourceLearned)
	}
	out := make([]slotWindow, 0, len(bySlot))
	for _, w := range bySlot {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].slot < out[j].slot })
	return out
}

// numberedCopy returns the slots numbered by start hour, the numbering the game
// uses, when the caller supplied windows without slot indexes of their own.
func numberedCopy(slots []ScheduleSlot) []ScheduleSlot {
	out := copySlots(slots)
	seen := map[int8]bool{}
	for _, sl := range out {
		if sl.Index < 0 || seen[sl.Index] {
			numberSlots(out)
			return out
		}
		seen[sl.Index] = true
	}
	return out
}

// tableAt looks a slot up in a timetable sorted by slot index.
func tableAt(table []slotWindow, slot int8) (slotWindow, bool) {
	i := sort.Search(len(table), func(i int) bool { return table[i].slot >= slot })
	if i < len(table) && table[i].slot == slot {
		return table[i], true
	}
	return slotWindow{}, false
}

// slotMap returns, for each path position of a species, the timetable slot the
// group uses there.
//
// Normally the position is the slot. When the timetable's activity type at a
// position does not match the type the shipped geometry gives the zones there
// (the published table numbers a few species' slots in another order), the whole
// species falls back on the zones' own types: the k-th zone of an activity type
// along the path takes the k-th slot of that type.
//
// A position whose slot cannot be placed is -1; its zone gets no window.
func slotMap(table []slotWindow, derived []int32, positions int) []int8 {
	out := make([]int8, positions)
	aligned := true
	for p := 0; p < positions; p++ {
		w, ok := tableAt(table, int8(p))
		if !ok || derived[p] == 0 {
			continue
		}
		if w.typ != derived[p] {
			aligned = false
			break
		}
	}
	if aligned {
		for p := range out {
			out[p] = int8(p)
		}
		return out
	}
	rank := map[int32]int{}
	for p := 0; p < positions; p++ {
		out[p] = -1
		zoneType := derived[p]
		if zoneType == 0 {
			// The zone's own type is unknown here; keep the position when the
			// timetable has a slot of that index.
			if _, ok := tableAt(table, int8(p)); ok {
				out[p] = int8(p)
			}
			continue
		}
		want := rank[zoneType]
		rank[zoneType]++
		seen := 0
		for _, w := range table {
			if w.typ != zoneType {
				continue
			}
			if seen == want {
				out[p] = w.slot
				break
			}
			seen++
		}
	}
	return out
}

// groupZoneTypes maps the zone ids one group lists to their activity type, from
// the entries the group carries (the shipped geometry, or the save's own stored
// type for a zone the player has found).
func groupZoneTypes(g *Group) map[uint32]int32 {
	out := make(map[uint32]int32, len(g.NeedZones))
	for _, z := range g.NeedZones {
		if z.Type != 0 {
			out[z.IDU] = z.Type
		}
	}
	return out
}

// derivedSlotTypes recovers a species' slot activity types from the save and the
// install: for each position of the group paths, the type most of the species'
// groups use there. It needs no published table, and it is what decides which
// slot window a zone gets when a published table orders its slots differently.
func derivedSlotTypes(groups []*Group, positions int) []int32 {
	counts := make([]map[int32]int, positions)
	for _, g := range groups {
		if g == nil {
			continue
		}
		types := groupZoneTypes(g)
		for pos, id := range g.NeedZoneGuids {
			if pos >= positions || id == EmptyZoneID {
				continue
			}
			t, ok := types[id]
			if !ok {
				continue
			}
			if counts[pos] == nil {
				counts[pos] = map[int32]int{}
			}
			counts[pos][t]++
		}
	}
	out := make([]int32, positions)
	for pos, c := range counts {
		best, bestN := int32(0), -1
		for t, n := range c {
			if n > bestN {
				best, bestN = t, n
			}
		}
		out[pos] = best
	}
	return out
}

// zoneUse is one slot a group uses a zone at, with the window that slot has.
type zoneUse struct {
	slot      int8
	hasWindow bool
	start     float64
	end       float64
	source    string
}

// sortedSlots lists the distinct slots a zone is used at, in slot order.
func sortedSlots(uses []zoneUse) []int8 {
	if len(uses) == 0 {
		return nil
	}
	out := make([]int8, 0, len(uses))
	for _, u := range uses {
		out = append(out, u.slot)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// useWindows returns the distinct windows a zone is used in, in slot order, and
// the source that supplied the first of them.
func useWindows(uses []zoneUse) ([]HourWindow, string) {
	ordered := make([]zoneUse, len(uses))
	copy(ordered, uses)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].slot < ordered[j].slot })
	type key struct{ start, end float64 }
	seen := map[key]bool{}
	windows := make([]HourWindow, 0, len(ordered))
	source := ""
	for _, u := range ordered {
		if !u.hasWindow {
			continue
		}
		k := key{u.start, u.end}
		if seen[k] {
			continue
		}
		seen[k] = true
		windows = append(windows, HourWindow{Start: u.start, End: u.end})
		if source == "" {
			source = u.source
		}
	}
	if len(windows) == 0 {
		return nil, HourSourceNone
	}
	return windows, source
}

// Timetable returns a species' daily timetable on a reserve: one entry per slot,
// ordered by slot index, with the slot's activity type and window.
//
// This is the same timetable the group slots are built from: slots this save
// revealed keep the window the save recorded, and slots only the learned store
// or the published table cover are filled in from there. It is empty when no
// source covers the species.
func (s *Save) Timetable(reserveIndex int, speciesKey string) []ScheduleSlot {
	if s == nil || speciesKey == "" {
		return nil
	}
	r := s.Reserve(reserveIndex)
	if r == nil {
		return nil
	}
	table := s.newHourResolver().timetable(r.Key, speciesKey)
	out := make([]ScheduleSlot, 0, len(table))
	for _, w := range table {
		out = append(out, ScheduleSlot{Index: w.slot, Type: w.typ, Start: w.start, End: w.end})
	}
	return out
}

// assignGroupSlots gives every group its daily timetable.
func (s *Save) assignGroupSlots() {
	if s == nil || s.game == nil {
		return
	}
	res := s.newHourResolver()
	for _, r := range s.Reserves {
		for _, sp := range r.Species {
			if len(sp.Groups) == 0 {
				continue
			}
			positions := 0
			for _, g := range sp.Groups {
				if g != nil && len(g.NeedZoneGuids) > positions {
					positions = len(g.NeedZoneGuids)
				}
			}
			table := res.timetable(r.Key, sp.SpeciesKey)
			derived := derivedSlotTypes(sp.Groups, positions)
			slots := slotMap(table, derived, positions)
			for _, g := range sp.Groups {
				assignGroupSlots(table, slots, g)
			}
		}
	}
}

// assignGroupSlots fills one group's Slots and the windows of the zones the
// player has not found.
func assignGroupSlots(table []slotWindow, slots []int8, g *Group) {
	if g == nil || len(g.NeedZoneGuids) == 0 {
		if g != nil {
			g.Slots = nil
		}
		return
	}
	byID := make(map[uint32]*NeedZone, len(g.NeedZones))
	for _, z := range g.NeedZones {
		byID[z.IDU] = z
	}
	out := make([]GroupSlot, 0, len(g.NeedZoneGuids))
	for pos, id := range g.NeedZoneGuids {
		gs := GroupSlot{Slot: -1, ZoneID: id}
		if pos < len(slots) {
			gs.Slot = slots[pos]
		}
		if id != EmptyZoneID {
			z := byID[id]
			if z != nil {
				gs.Type = z.Type
				gs.Discovered = z.Discovered
				if z.Discovered {
					gs.RecordedStartHour, gs.RecordedEndHour = z.StartHour, z.EndHour
				}
			}
			if gs.Slot >= 0 {
				if w, ok := tableAt(table, gs.Slot); ok {
					gs.HasWindow = true
					gs.StartHour, gs.EndHour, gs.HourSource = w.start, w.end, w.source
					applySlotWindow(z, w)
				} else if gs.Discovered && gs.RecordedEndHour > gs.RecordedStartHour {
					// No timetable covers this slot, so the window the save
					// recorded for the zone itself is the best there is.
					gs.HasWindow = true
					gs.StartHour, gs.EndHour = gs.RecordedStartHour, gs.RecordedEndHour
					gs.HourSource = HourSourceSave
				}
			}
		}
		out = append(out, gs)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Slot < 0 || out[j].Slot < 0 {
			return out[j].Slot < 0 && out[i].Slot >= 0
		}
		return out[i].Slot < out[j].Slot
	})
	g.Slots = out
}

// applySlotWindow fills the window of a zone entry the player has not found. A
// discovered entry keeps what the save stored: that is the engine's own record
// of the zone, written when the player found it, and for a zone several groups
// share it names the slot of the group that discovered it, which can differ from
// the slot this group uses it at (GroupSlot.RecordedStartHour).
func applySlotWindow(z *NeedZone, w slotWindow) {
	if z == nil || z.Discovered {
		return
	}
	z.ScheduleIndex = w.slot
	z.StartHour, z.EndHour = w.start, w.end
	z.Windows = []HourWindow{{Start: w.start, End: w.end}}
	z.HourSource = w.source
}
