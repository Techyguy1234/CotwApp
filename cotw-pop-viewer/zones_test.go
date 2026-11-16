package cotw

import (
	"math"
	"testing"
)

// TestZoneGeometryCoversEveryGroup checks that every population group gets a
// position from the shipped spawn-area geometry, which is what makes an
// undiscovered reserve mappable.
func TestZoneGeometryCoversEveryGroup(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}
	groups, placed := 0, 0
	for _, r := range save.Reserves {
		for _, sp := range r.Species {
			for _, g := range sp.Groups {
				groups++
				if g.SpawnArea != nil {
					placed++
				}
			}
		}
	}
	if groups == 0 {
		t.Skip("no groups in this save")
	}
	if placed != groups {
		t.Errorf("groups with a spawn-area position: %d of %d", placed, groups)
	}
}

// TestZoneGeometryMatchesDiscoveredZones checks the shipped geometry against
// every zone the player has actually found: the box centre must equal the saved
// position and the bitmap layer must equal the saved need zone type.
func TestZoneGeometryMatchesDiscoveredZones(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}
	checked := 0
	for _, r := range save.Reserves {
		if len(r.NeedZones) == 0 {
			continue
		}
		infos := make(map[uint32]ZoneInfo, len(r.NeedZones))
		for _, zi := range save.Zones(r.Index) {
			infos[zi.ID] = zi
		}
		for _, z := range r.NeedZones {
			zi, ok := infos[z.IDU]
			if !ok || !zi.HasGeometry {
				t.Errorf("%s zone %d: no shipped geometry", r.Key, z.IDU)
				continue
			}
			if zi.Type != z.Type {
				t.Errorf("%s zone %d: shipped type %d, save says %d", r.Key, z.IDU, zi.Type, z.Type)
			}
			d := math.Hypot(zi.Position.X-z.Position.X, zi.Position.Z-z.Position.Z)
			if d > 5 {
				t.Errorf("%s zone %d: position off by %.1f m (shipped %.0f/%.0f, saved %.0f/%.0f)",
					r.Key, z.IDU, d, zi.Position.X, zi.Position.Z, z.Position.X, z.Position.Z)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Skip("no discovered need zones in this save")
	}
	t.Logf("validated %d discovered zones against the shipped geometry", checked)
}

// TestSpeciesScheduleIsConsistent checks that a species' schedule slots never
// disagree with each other, which is what lets an undiscovered zone's hours be
// derived.
func TestSpeciesScheduleIsConsistent(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}
	schedules := save.SpeciesSchedules()
	if len(schedules) == 0 {
		t.Skip("no discovered zones to derive a schedule from")
	}
	for _, sch := range schedules {
		seen := map[int8]ScheduleSlot{}
		for _, slot := range sch.Slots {
			if prev, ok := seen[slot.Index]; ok && (prev.Type != slot.Type || prev.Start != slot.Start || prev.End != slot.End) {
				t.Errorf("%s slot %d is not consistent: %+v vs %+v", sch.SpeciesKey, slot.Index, prev, slot)
			}
			seen[slot.Index] = slot
		}
	}
	t.Logf("schedules for %d species", len(schedules))
}

// TestGroupNeedZonesCoverEveryGuid checks that a group's NeedZones list every
// zone it uses, not just the ones the player has discovered.
func TestGroupNeedZonesCoverEveryGuid(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}
	groups, empty, missing := 0, 0, 0
	undiscovered, withWindow := 0, 0
	for _, r := range save.Reserves {
		for _, sp := range r.Species {
			for _, g := range sp.Groups {
				var realGuids int
				for _, guid := range g.NeedZoneGuids {
					if guid != EmptyZoneID {
						realGuids++
					}
				}
				if realGuids == 0 {
					continue
				}
				groups++
				if len(g.NeedZones) == 0 {
					empty++
				}
				have := make(map[uint32]bool, len(g.NeedZones))
				for _, z := range g.NeedZones {
					// A reserve can store two zones under one id, so an id may
					// legitimately appear twice; the guid itself must not.
					have[z.IDU] = true
					if !z.Discovered {
						undiscovered++
						if len(z.Windows) > 0 {
							withWindow++
						}
					}
				}
				for _, guid := range g.NeedZoneGuids {
					if guid == EmptyZoneID {
						continue
					}
					if !have[guid] {
						missing++
					}
				}
			}
		}
	}
	if groups == 0 {
		t.Skip("no groups in this save")
	}
	if empty > 0 {
		t.Errorf("%d of %d groups have zone guids but an empty NeedZones list", empty, groups)
	}
	if missing > 0 {
		t.Errorf("%d zone guids have no NeedZones entry", missing)
	}
	t.Logf("%d groups, %d undiscovered zone entries (%d with a window)",
		groups, undiscovered, withWindow)
}
