package cotw

import (
	"testing"
)

// groupPaths indexes, for a reserve, how many times each zone id is used across
// the paths of the groups that reference it.
func groupPaths(save *Save, reserveIndex int) map[uint32]int {
	out := map[uint32]int{}
	r := save.Reserve(reserveIndex)
	if r == nil {
		return out
	}
	for _, sp := range r.Species {
		for _, g := range sp.Groups {
			for _, id := range g.NeedZoneGuids {
				if id != EmptyZoneID {
					out[id]++
				}
			}
		}
	}
	return out
}

// TestGroupSlotsCoverThePath checks that every group's timetable has one entry
// per zone path entry, in slot order, naming the same zones.
func TestGroupSlotsCoverThePath(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	opts.DisableScheduleLearning = true // keep the test from touching the cache
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}
	groups := 0
	for _, r := range save.Reserves {
		for _, sp := range r.Species {
			for _, g := range sp.Groups {
				if len(g.NeedZoneGuids) == 0 {
					continue
				}
				groups++
				if len(g.Slots) != len(g.NeedZoneGuids) {
					t.Fatalf("%s %s group %d: %d slots for %d path entries",
						r.Key, sp.SpeciesKey, g.Index, len(g.Slots), len(g.NeedZoneGuids))
				}
				// Slots is a timetable, ordered by slot index; between them the
				// entries must cover the path exactly once each.
				fromPath := map[uint32]int{}
				for _, id := range g.NeedZoneGuids {
					fromPath[id]++
				}
				seen := map[int8]bool{}
				for i, gs := range g.Slots {
					if fromPath[gs.ZoneID] == 0 {
						t.Errorf("%s %s group %d slot %d: zone 0x%08x is not in the path",
							r.Key, sp.SpeciesKey, g.Index, gs.Slot, gs.ZoneID)
					}
					fromPath[gs.ZoneID]--
					if gs.Slot < 0 {
						continue // no source could place this zone in the timetable
					}
					if seen[gs.Slot] {
						t.Errorf("%s %s group %d: slot %d used twice", r.Key, sp.SpeciesKey, g.Index, gs.Slot)
					}
					seen[gs.Slot] = true
					if i > 0 && g.Slots[i-1].Slot >= 0 && g.Slots[i-1].Slot > gs.Slot {
						t.Errorf("%s %s group %d: slots out of order at %d", r.Key, sp.SpeciesKey, g.Index, i)
					}
				}
				for id, left := range fromPath {
					if left != 0 {
						t.Errorf("%s %s group %d: path zone 0x%08x not covered by the slots",
							r.Key, sp.SpeciesKey, g.Index, id)
					}
				}
			}
		}
	}
	if groups == 0 {
		t.Skip("no groups in this save")
	}
	t.Logf("checked %d groups", groups)
}

// TestGroupSlotsUseOneWindowPerSlot is the regression test for the bug this file
// exists for: a group's zones must be spread over the day, one window each, not
// every window of their activity type.
func TestGroupSlotsUseOneWindowPerSlot(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	opts.DisableScheduleLearning = true // keep the test from touching the cache
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}
	entries, spread, repeated := 0, 0, 0
	for _, r := range save.Reserves {
		for _, sp := range r.Species {
			for _, g := range sp.Groups {
				// Every group entry carries at most the one window of its slot.
				for _, z := range g.NeedZones {
					if len(z.Windows) > 1 {
						t.Errorf("%s %s group %d zone 0x%08x: %d windows (%v), a zone is used at one slot",
							r.Key, sp.SpeciesKey, g.Index, z.IDU, len(z.Windows), z.Windows)
					}
					if len(z.Windows) == 1 && z.StartHour != z.Windows[0].Start {
						t.Errorf("%s group %d zone 0x%08x: StartHour disagrees with its window",
							r.Key, g.Index, z.IDU)
					}
				}
				// Zones of one activity type must land in the type's different
				// slots, so their windows must differ whenever the species has
				// more than one slot of that type and the windows are known.
				byType := map[int32]map[[2]float64]bool{}
				known := map[int32]int{}
				for _, gs := range g.Slots {
					if !gs.HasWindow || gs.Type == 0 {
						continue
					}
					if byType[gs.Type] == nil {
						byType[gs.Type] = map[[2]float64]bool{}
					}
					byType[gs.Type][[2]float64{gs.StartHour, gs.EndHour}] = true
					known[gs.Type]++
				}
				for typ, windows := range byType {
					if known[typ] < 2 {
						continue
					}
					entries++
					if len(windows) > 1 {
						spread++
					} else {
						repeated++
					}
				}
			}
		}
	}
	if entries == 0 {
		t.Skip("no group has two zones of one type with a known window")
	}
	t.Logf("%d (group, type) pairs with several zones: %d spread over different windows, %d sharing one",
		entries, spread, repeated)
	if repeated > entries/10 {
		t.Errorf("%d of %d (group, type) pairs still put several zones at the same time", repeated, entries)
	}
}

// TestPathOrderIsTheSlotOrder checks the evidence the slot assignment rests on:
// for every zone only one group uses, the slot index the save recorded for that
// zone is the position the zone occupies in that group's path.
func TestPathOrderIsTheSlotOrder(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	opts.DisableScheduleLearning = true // keep the test from touching the cache
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}
	checked, agree := 0, 0
	for _, r := range save.Reserves {
		paths := groupPaths(save, r.Index)
		for _, z := range r.NeedZones {
			if paths[z.IDU] != 1 {
				continue // shared, reused or unreferenced: one record, several uses
			}
			slot, ok := groupSlotOf(save, r.Index, z.IDU)
			if !ok {
				continue
			}
			checked++
			if slot == z.ScheduleIndex {
				agree++
			} else {
				t.Errorf("%s zone 0x%08x: save says slot %d, path position %d",
					r.Key, z.IDU, z.ScheduleIndex, slot)
			}
		}
	}
	if checked == 0 {
		t.Skip("this save has no singly-used discovered zone")
	}
	t.Logf("%d of %d singly-used discovered zones sit at the slot the save recorded", agree, checked)
	if agree != checked {
		t.Error("the path order is not the slot order on this save")
	}
}

// TestSlotTypesMatchShippedGeometry checks that the activity type of the zone a
// position holds equals the type of the timetable slot that position takes, for
// essentially every group entry.
func TestSlotTypesMatchShippedGeometry(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	opts.DisableScheduleLearning = true // keep the test from touching the cache
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}
	entries, agree, mismatched := 0, 0, 0
	for _, r := range save.Reserves {
		for _, sp := range r.Species {
			bySlot := map[int8]ScheduleSlot{}
			for _, sl := range save.Timetable(r.Index, sp.SpeciesKey) {
				bySlot[sl.Index] = sl
			}
			for _, g := range sp.Groups {
				for _, gs := range g.Slots {
					if gs.ZoneID == EmptyZoneID || gs.Type == 0 || !gs.HasWindow {
						continue
					}
					// A slot the timetable does not cover cannot be judged; its
					// window is the save's own record for the zone.
					sl, known := bySlot[gs.Slot]
					if !known {
						continue
					}
					entries++
					if sl.Type == gs.Type {
						agree++
					} else {
						mismatched++
					}
				}
			}
		}
	}
	if entries == 0 {
		t.Skip("no group entries with a window")
	}
	t.Logf("%d of %d group entries use a slot of their zone's own activity type (%d differ)",
		agree, entries, mismatched)
	if agree*1000 < entries*99 {
		t.Errorf("only %.1f%% of entries landed on a matching slot", 100*float64(agree)/float64(entries))
	}
}

// TestSlotWindowsAgreeWithTheSave checks that the window a slot gets is the
// window the save recorded for a zone the player discovered in that slot.
//
// It holds for every zone only one group uses. For a zone several groups share
// at different times of day, the save's single recorded window can only name one
// of those uses, so those entries are reported rather than asserted.
func TestSlotWindowsAgreeWithTheSave(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	opts.DisableScheduleLearning = true // keep the test from touching the cache
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}
	single, singleAgree, shared, sharedAgree := 0, 0, 0, 0
	for _, r := range save.Reserves {
		paths := groupPaths(save, r.Index)
		for _, sp := range r.Species {
			for _, g := range sp.Groups {
				for _, gs := range g.Slots {
					if !gs.Discovered || !gs.HasWindow {
						continue
					}
					match := gs.StartHour == gs.RecordedStartHour && gs.EndHour == gs.RecordedEndHour
					if paths[gs.ZoneID] <= 1 {
						single++
						if match {
							singleAgree++
						} else {
							t.Errorf("%s zone 0x%08x: slot %d window %g-%g, save recorded %g-%g",
								r.Key, gs.ZoneID, gs.Slot, gs.StartHour, gs.EndHour,
								gs.RecordedStartHour, gs.RecordedEndHour)
						}
					} else {
						shared++
						if match {
							sharedAgree++
						}
					}
				}
			}
		}
	}
	if single+shared == 0 {
		t.Skip("no discovered zone with a window in this save")
	}
	t.Logf("singly-used: %d of %d agree with the recorded window; shared: %d of %d",
		singleAgree, single, sharedAgree, shared)
	if single == 0 {
		t.Skip("no singly-used discovered zone with a window")
	}
}

// groupSlotOf returns the slot the one group that uses a zone puts it at. The
// caller has already checked the save-wide use count is one.
func groupSlotOf(save *Save, reserveIndex int, id uint32) (int8, bool) {
	r := save.Reserve(reserveIndex)
	if r == nil {
		return 0, false
	}
	for _, sp := range r.Species {
		for _, g := range sp.Groups {
			for _, gs := range g.Slots {
				if gs.ZoneID == id {
					return gs.Slot, true
				}
			}
		}
	}
	return 0, false
}
