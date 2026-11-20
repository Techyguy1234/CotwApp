package cotw

import (
	"testing"
)

func TestZZFound(t *testing.T) {
	dir, opts := integrationOptions(t)
	opts.DisableScheduleLearning = true
	save, err := ParseSave(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, r := range save.Reserves {
		t.Logf("reserve %2d %-14s discovered=%d", r.Index, r.Key, len(r.NeedZones))
		total += len(r.NeedZones)
		for _, z := range r.NeedZones {
			if z.IDU == 970632343 {
				t.Logf("  -> id 970632343 IS discovered here: type=%s pos=(%.0f,%.0f)", ZoneTypeName(z.Type), z.Position.X, z.Position.Z)
			}
		}
	}
	t.Logf("total discovered zones in save: %d", total)
	for _, r := range save.Reserves {
		if r.Key != "hirschfelden" {
			continue
		}
		t.Logf("reserve %d %s: %d species, %d discovered zones", r.Index, r.Key, len(r.Species), len(r.NeedZones))
		for _, z := range r.NeedZones {
			t.Logf("disc 0x%08x type=%s pos=(%.0f,%.0f) window=%g-%g slot=%d species=%s groups=%d",
				z.IDU, ZoneTypeName(z.Type), z.Position.X, z.Position.Z, z.StartHour, z.EndHour, z.ScheduleIndex, z.AnimalSpeciesKey, len(z.Groups))
		}
		// which ids do the wild boar groups list, and are they found?
		for _, sp := range r.Species {
			if sp.SpeciesKey != "wild_boar" {
				continue
			}
			ids := map[uint32]int{}
			for _, g := range sp.Groups {
				for _, id := range g.NeedZoneGuids {
					if id != EmptyZoneID {
						ids[id]++
					}
				}
			}
			for id, n := range ids {
				t.Logf("wild_boar zone 0x%08x used %d times", id, n)
			}
			break
		}
	}
}
