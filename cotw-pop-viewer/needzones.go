package cotw

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"cotw-pop-viewer/adf"
)

// NeedZoneFileName is the save file holding the discovered need zones.
const NeedZoneFileName = "found_need_zones_adf"

// EmptyZoneID is the placeholder a group's NeedZoneGuids use for an unused slot.
const EmptyZoneID uint32 = 0xFFFFFFFF

// loadNeedZones parses found_need_zones_adf into zones grouped by reserve index.
func (g *Game) loadNeedZones(path string) (map[int][]*NeedZone, error) {
	a, err := LoadADFFile(path)
	if err != nil {
		return nil, err
	}
	root := a.InstanceOfTypePrefix("NeedZoneSave")
	if root == nil && len(a.Values) > 0 {
		root = a.Values[0]
	}
	if root == nil {
		return nil, fmt.Errorf("cotw: no NeedZoneSave instance in %s", filepath.Base(path))
	}
	data := root.Field("NZData")
	if data == nil {
		return nil, fmt.Errorf("cotw: no NZData array in %s", filepath.Base(path))
	}

	out := make(map[int][]*NeedZone, len(data.Elems))
	for _, rv := range data.Elems {
		if rv == nil || rv.Kind != adf.KindStruct {
			continue
		}
		reserveIndex := 0
		if f := rv.Field("ReserveId"); f != nil {
			if v, err := f.Uint64(); err == nil {
				reserveIndex = int(v)
			}
		}
		zones := rv.Field("NeedZoneData")
		if zones == nil {
			continue
		}
		for _, zv := range zones.Elems {
			zone := g.decodeNeedZone(zv, reserveIndex)
			if zone == nil {
				continue
			}
			out[reserveIndex] = append(out[reserveIndex], zone)
		}
	}
	return out, nil
}

// decodeNeedZone reads one NeedZoneBaseData entry.
//
// Current saves store: Position Vector3 @0 (X, Y = elevation, Z), NeedZoneId s32
// @12, NeedType s32 @16, MapIconId u64 @24, Start/End hours f32 @32/@36,
// AnimalTypeLocalizationName u32 @40, NeedZoneScheduleIndex s8 @44,
// DiscoveredGroups A[uint32] @48.
func (g *Game) decodeNeedZone(v *adf.Value, reserveIndex int) *NeedZone {
	if v == nil || v.Kind != adf.KindStruct {
		return nil
	}
	z := &NeedZone{ReserveIndex: reserveIndex, DiscoveredGroups: []uint32{}}
	if f := v.Field("NeedZoneId"); f != nil {
		if raw, err := f.Int64(); err == nil {
			z.ID = int32(raw)
			z.IDU = uint32(raw)
		}
	}
	if f := v.Field("NeedType"); f != nil {
		if raw, err := f.Int64(); err == nil {
			z.Type = int32(raw)
		}
	}
	if p := v.Field("Position"); p != nil {
		if f := p.Field("X"); f != nil {
			z.Position.X, _ = f.Float64()
		}
		if f := p.Field("Y"); f != nil {
			z.Position.Y, _ = f.Float64()
		}
		if f := p.Field("Z"); f != nil {
			z.Position.Z, _ = f.Float64()
		}
	}
	if f := v.Field("MapIconId"); f != nil {
		z.MapIconID, _ = f.Uint64()
	}
	if f := v.Field("NeedZoneStartTimeHours"); f != nil {
		z.StartHour, _ = f.Float64()
	}
	if f := v.Field("NeedZoneEndTimeHours"); f != nil {
		z.EndHour, _ = f.Float64()
	}
	if f := v.Field("AnimalTypeLocalizationName"); f != nil {
		z.AnimalTypeHash, _ = f.Uint32()
		if key := g.speciesKeyForZoneHash(z.AnimalTypeHash); key != "" {
			z.AnimalSpeciesKey = key
			if rec, ok := g.SpeciesByKey(key); ok {
				z.AnimalSpeciesName = rec.Name
			}
		}
	}
	if f := v.Field("NeedZoneScheduleIndex"); f != nil {
		if raw, err := f.Int64(); err == nil {
			z.ScheduleIndex = int8(raw)
		}
	}
	if f := v.Field("DiscoveredGroups"); f != nil {
		for _, e := range f.Elems {
			if id, err := e.Uint32(); err == nil {
				z.DiscoveredGroups = append(z.DiscoveredGroups, id)
			}
		}
	}
	return z
}

// speciesKeyForZoneHash resolves a need zone's AnimalTypeLocalizationName, which
// is hash32("animal_<codex id>_name"), back to a species key.
func (g *Game) speciesKeyForZoneHash(hash uint32) string {
	if i, ok := g.zoneSpecies[hash]; ok {
		return g.species[i].Key
	}
	return ""
}

// linkNeedZones attaches every discovered zone to the groups that reference it.
//
// Zone ids are not unique within a reserve (two entries can share one) and a
// group can list the same id twice, so links are deduplicated.
func linkNeedZones(r *Reserve) {
	for _, z := range r.NeedZones {
		z.Discovered = true
	}
	if len(r.NeedZones) == 0 {
		return
	}
	byID := make(map[uint32][]*NeedZone, len(r.NeedZones))
	for _, z := range r.NeedZones {
		byID[z.IDU] = append(byID[z.IDU], z)
	}
	for _, sp := range r.Species {
		for _, group := range sp.Groups {
			seen := make(map[uint32]bool, len(group.NeedZoneGuids))
			for _, guid := range group.NeedZoneGuids {
				if seen[guid] {
					continue
				}
				seen[guid] = true
				matches := byID[guid]
				if len(matches) == 0 {
					continue
				}
				ref := GroupRef{SpeciesIndex: sp.Index, SpeciesKey: sp.SpeciesKey, GroupIndex: group.Index}
				for _, zone := range matches {
					group.NeedZones = append(group.NeedZones, zone)
					// A group's species comes from its population entry and is
					// better evidence than the zone's own species hash.
					if zone.AnimalSpeciesKey == "" && sp.SpeciesKey != "" {
						zone.AnimalSpeciesKey = sp.SpeciesKey
						zone.AnimalSpeciesName = sp.SpeciesName
					}
					if !containsGroupRef(zone.Groups, ref) {
						zone.Groups = append(zone.Groups, ref)
					}
				}
			}
		}
	}
}

func containsGroupRef(refs []GroupRef, want GroupRef) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}

// NeedZoneFileExists reports whether a save folder has a need zone file.
func NeedZoneFileExists(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, NeedZoneFileName))
	return err == nil && !info.IsDir()
}

// fillGroupNeedZones gives every group an entry for each zone it uses, including
// the zones the player has not discovered. Those carry Discovered=false, take
// their type and position from the shipped geometry, and their hours from
// whatever timetable the library could resolve.
func fillGroupNeedZones(save *Save) {
	if save == nil || save.game == nil {
		return
	}
	resolver := save.newHourResolver()
	for _, r := range save.Reserves {
		if len(r.Species) == 0 {
			continue
		}
		for _, sp := range r.Species {
			for _, g := range sp.Groups {
				if g == nil || len(g.NeedZoneGuids) == 0 {
					continue
				}
				have := make(map[uint32]bool, len(g.NeedZoneGuids))
				for _, z := range g.NeedZones {
					have[z.IDU] = true
				}
				var refX, refZ float64
				haveRef := false
				if g.SpawnArea != nil {
					refX, refZ, haveRef = g.SpawnArea.Position.X, g.SpawnArea.Position.Z, true
				}
				for _, guid := range g.NeedZoneGuids {
					if guid == EmptyZoneID || have[guid] {
						continue
					}
					have[guid] = true
					z := &NeedZone{
						ReserveIndex:      r.Index,
						ID:                int32(guid),
						IDU:               guid,
						AnimalSpeciesKey:  sp.SpeciesKey,
						AnimalSpeciesName: sp.SpeciesName,
						Groups: []GroupRef{{
							SpeciesIndex: sp.Index,
							SpeciesKey:   sp.SpeciesKey,
							GroupIndex:   g.Index,
						}},
					}
					if geom, ok := save.zoneGeometryFor(guid, refX, refZ, haveRef); ok {
						z.Type = geom.Type
						z.Position = geom.Position
					}
					g.NeedZones = append(g.NeedZones, z)
				}
				// Hours for every entry: stored for a discovered zone, resolved
				// from the timetables otherwise.
				for _, z := range g.NeedZones {
					if z.Discovered {
						if z.EndHour != z.StartHour {
							z.Windows = []HourWindow{{Start: z.StartHour, End: z.EndHour}}
							z.HourSource = HourSourceSave
						}
						continue
					}
					if z.AnimalSpeciesKey == "" || z.Type == 0 {
						continue
					}
					slots, source := resolver.windows(r.Key, z.AnimalSpeciesKey, z.Type)
					if len(slots) == 0 {
						continue
					}
					z.HourSource = source
					z.Windows = make([]HourWindow, 0, len(slots))
					for _, sl := range slots {
						z.Windows = append(z.Windows, HourWindow{Start: sl.Start, End: sl.End})
					}
					if len(slots) == 1 {
						z.ScheduleIndex = slots[0].Index
						z.StartHour, z.EndHour = slots[0].Start, slots[0].End
					}
				}
				sort.Slice(g.NeedZones, func(i, j int) bool { return g.NeedZones[i].IDU < g.NeedZones[j].IDU })
			}
		}
	}
}
