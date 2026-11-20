package cotw

import (
	"fmt"
	"os"
	"path/filepath"

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

// linkNeedZones records which groups use each discovered zone and marks the
// reserve's zone records as found. It does not build the groups' own zone lists:
// those are one entry per schedule slot (see fillGroupNeedZones), because a group
// can use the same zone at two different slots of its day.
//
// Zone ids are not unique within a reserve (two entries can share one), so the
// zone's Groups list is deduplicated but every matching record is linked.
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

// fillGroupNeedZones gives every group one entry for every slot of its zone path,
// including the zones the player has not discovered.
//
// The entries are one per path position, not one per distinct zone: a group that
// returns to the same zone later in its day lists that zone again, at the slot it
// returns in. Deduplicating here is what made a 7-slot group print 6 zones with
// one window missing (the second slot overwrote the first), so the list is
// built straight from NeedZoneGuids, in path order, and assignGroupSlots then
// gives each entry the window of its own slot.
//
// A discovered zone's entry starts as a copy of the reserve's record, so that
// two slots using one zone do not share windows; the copy carries that zone's
// stored type, position and id. An undiscovered zone takes its type and position
// from the geometry the game ships.
func fillGroupNeedZones(save *Save) {
	if save == nil || save.game == nil {
		return
	}
	for _, r := range save.Reserves {
		if len(r.Species) == 0 {
			continue
		}
		// A reserve can store two zones under one id; the first record is the
		// one a group entry is copied from (ZoneInfo merges them by id anyway).
		byID := make(map[uint32]*NeedZone, len(r.NeedZones))
		for _, z := range r.NeedZones {
			if _, ok := byID[z.IDU]; !ok {
				byID[z.IDU] = z
			}
		}
		for _, sp := range r.Species {
			for _, g := range sp.Groups {
				if g == nil {
					continue
				}
				if len(g.NeedZoneGuids) == 0 {
					g.NeedZones = nil
					continue
				}
				var refX, refZ float64
				haveRef := false
				if g.SpawnArea != nil {
					refX, refZ, haveRef = g.SpawnArea.Position.X, g.SpawnArea.Position.Z, true
				}
				ref := GroupRef{SpeciesIndex: sp.Index, SpeciesKey: sp.SpeciesKey, GroupIndex: g.Index}
				zones := make([]*NeedZone, 0, len(g.NeedZoneGuids))
				for _, guid := range g.NeedZoneGuids {
					if guid == EmptyZoneID {
						continue
					}
					z := &NeedZone{
						ReserveIndex:      r.Index,
						ID:                int32(guid),
						IDU:               guid,
						Slot:              -1,
						AnimalSpeciesKey:  sp.SpeciesKey,
						AnimalSpeciesName: sp.SpeciesName,
					}
					if found := byID[guid]; found != nil {
						copied := *found
						copied.Slot = -1
						copied.Windows = nil
						copied.HourSource = ""
						z = &copied
					} else if geom, ok := save.zoneGeometryFor(guid, refX, refZ, haveRef); ok {
						z.Type = geom.Type
						z.Position = geom.Position
					}
					if !containsGroupRef(z.Groups, ref) {
						z.Groups = append(z.Groups, ref)
					}
					zones = append(zones, z)
				}
				g.NeedZones = zones
			}
		}
	}
}
