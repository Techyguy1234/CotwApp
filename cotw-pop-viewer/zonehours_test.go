package cotw

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPublishedZoneHoursTable checks the transcribed community table parses and
// numbers its slots the way the game does.
func TestPublishedZoneHoursTable(t *testing.T) {
	slots, ok := PublishedZoneHours("whitetail_deer")
	if !ok {
		t.Fatal("whitetail_deer missing from the published table")
	}
	if len(slots) != 6 {
		t.Fatalf("whitetail_deer slots = %d, want 6", len(slots))
	}
	for i, sl := range slots {
		if sl.Index != int8(i) {
			t.Errorf("slot %d index = %d, want %d (slots numbered by start hour)", i, sl.Index, i)
		}
		if i > 0 && slots[i-1].Start > sl.Start {
			t.Errorf("slots not ordered by start hour: %v then %v", slots[i-1], sl)
		}
		if sl.Type < 1 || sl.Type > 3 {
			t.Errorf("slot %d has type %d, want 1..3", i, sl.Type)
		}
	}
	if slots[0].Type != 3 || slots[0].Start != 0 || slots[0].End != 4 {
		t.Errorf("first slot = %+v, want resting 0-4", slots[0])
	}

	// A reserve with its own page keeps its variant.
	variant, ok := PublishedZoneHoursFor("delta", "whitetail_deer")
	if !ok {
		t.Fatal("whitetail_deer variant missing for delta")
	}
	if len(variant) == len(slots) {
		t.Errorf("delta variant should differ from the default (6 vs %d slots)", len(variant))
	}

	// A reserve without a variant falls back to the default.
	if plain, ok := PublishedZoneHoursFor("nowhere", "whitetail_deer"); !ok || len(plain) != len(slots) {
		t.Errorf("unknown reserve should fall back to the default table")
	}
	if len(PublishedZoneHoursSpecies()) < 95 {
		t.Errorf("published table covers %d species, expected the 100 transcribed",
			len(PublishedZoneHoursSpecies()))
	}
}

// TestHourStoreRoundTrip checks that learned timetables survive a save/load and
// that a store from another game version is ignored.
func TestHourStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gamedata.json.hours")

	store := newHourStore("v1")
	if !store.learn("yukon", "moose", []ScheduleSlot{
		{Index: 0, Type: 3, Start: 0, End: 4},
		{Index: 1, Type: 1, Start: 4, End: 8},
	}) {
		t.Fatal("learn reported no change on an empty store")
	}
	if store.learn("yukon", "moose", []ScheduleSlot{
		{Index: 0, Type: 3, Start: 0, End: 4},
		{Index: 1, Type: 1, Start: 4, End: 8},
	}) {
		t.Fatal("learn reported a change for identical slots")
	}
	if err := store.save(path, "v1"); err != nil {
		t.Fatalf("save: %v", err)
	}

	back, stale := loadHourStore(path, "v1")
	if stale {
		t.Fatal("store from the same version reported stale")
	}
	slots, ok := back.hours("yukon", "moose")
	if !ok || len(slots) != 2 {
		t.Fatalf("round trip lost slots: %v %v", slots, ok)
	}
	if _, ok := back.hours("yukon", "moose"); !ok || back.Reserves["yukon"]["moose"][1].End != 8 {
		t.Fatal("round trip changed the stored window")
	}

	// A species-wide entry applies elsewhere, but not once a reserve disagrees.
	back.Species["moose"] = copySlots(slots)
	if _, ok := back.hours("layton_lake_district", "moose"); !ok {
		t.Fatal("species-wide entry should cover other reserves")
	}
	back.learn("layton_lake_district", "moose", []ScheduleSlot{{Index: 0, Type: 3, Start: 1, End: 5}})
	if _, ok := back.hours("hirschfelden", "moose"); ok {
		t.Fatal("conflicting reserves should stop the species-wide entry from being used")
	}

	// A different game version invalidates the store.
	if _, stale := loadHourStore(path, "v2"); !stale {
		t.Fatal("store from another game version should be reported stale")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("store file missing: %v", err)
	}
}

// TestHourResolverPrecedence checks the source order: options, then this save,
// then an earlier save, then the published table.
func TestHourResolverPrecedence(t *testing.T) {
	store := newHourStore("v1")
	store.learn("layton_lake_district", "moose", []ScheduleSlot{
		{Index: 0, Type: 3, Start: 0, End: 4},
		{Index: 1, Type: 1, Start: 4, End: 8},
	})
	store.Species["moose"] = copySlots(store.Reserves["layton_lake_district"]["moose"])

	res := &hourResolver{
		evidence: scheduleEvidence{
			byReserve: map[string]map[string][]ScheduleSlot{
				"yukon": {"moose": {{Index: 0, Type: 3, Start: 2, End: 6}}},
			},
			bySpecies: map[string][]ScheduleSlot{},
		},
		learned: store,
	}

	// This save's own discovery wins for its reserve.
	if slots, src := res.windows("yukon", "moose", 3); src != HourSourceSave || len(slots) != 1 || slots[0].Start != 2 {
		t.Fatalf("reserve evidence: %v %q", slots, src)
	}
	// The species-wide learned entry covers a reserve with no evidence of its own.
	if slots, src := res.windows("hirschfelden", "moose", 1); src != HourSourceLearned || len(slots) != 1 || slots[0].Start != 4 {
		t.Fatalf("learned species entry: %v %q", slots, src)
	}
	// The published table fills a type the learned entry does not cover.
	if slots, src := res.windows("hirschfelden", "snow_leopard", 1); src != HourSourceWiki || len(slots) == 0 {
		t.Fatalf("published fallback: %v %q", slots, src)
	}
	// An explicit override beats everything else for undiscovered zones.
	res.options = map[string][]ScheduleSlot{
		"hirschfelden/moose": {{Type: 1, Start: 6, End: 9}},
	}
	if slots, src := res.windows("hirschfelden", "moose", 1); src != HourSourceOptions || slots[0].Start != 6 {
		t.Fatalf("override: %v %q", slots, src)
	}
	// A species-wide override applies to every reserve, reserve evidence included.
	res.options = map[string][]ScheduleSlot{
		"moose": {{Type: 1, Start: 6, End: 9}},
	}
	if slots, src := res.windows("yukon", "moose", 1); src != HourSourceOptions || slots[0].Start != 6 {
		t.Fatalf("override should win over learned evidence too: %v %q", slots, src)
	}
	// Nothing to say for an unknown species.
	if _, src := res.windows("yukon", "not_a_species", 1); src != HourSourceNone {
		t.Fatalf("unknown species source = %q", src)
	}
}

// TestZoneHoursAreFilledIn checks, on the real save, that undiscovered zones get
// a window from this save, the learned store or the published table — and that
// every filled window is self-consistent.
func TestZoneHoursAreFilledIn(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	opts.DisableScheduleLearning = true // keep the test from touching the cache
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}

	sources := map[string]int{}
	filled, empty := 0, 0
	for _, r := range save.Reserves {
		for _, z := range save.Zones(r.Index) {
			if len(z.Windows) > 0 {
				filled++
				sources[z.HourSource]++
				for _, w := range z.Windows {
					if !(w.End > w.Start || w.End == 24) {
						t.Fatalf("%s zone %d: nonsense window %v", r.Key, z.ID, w)
					}
				}
				if len(z.Windows) == 1 && (z.StartHour != z.Windows[0].Start || z.EndHour != z.Windows[0].End) {
					t.Fatalf("%s zone %d: StartHour/EndHour disagree with the single window", r.Key, z.ID)
				}
			} else {
				empty++
			}
		}
	}
	t.Logf("zones with a window: %d (%v), without: %d", filled, sources, empty)
	if sources[HourSourceWiki] == 0 {
		t.Error("no zone used the published fallback table; the resolver never reached it")
	}
	if sources[HourSourceSave] == 0 {
		t.Error("no zone was filled from this save's discoveries")
	}
	if empty > filled/4 {
		t.Errorf("%d zones still without a window, only %d with one", empty, filled)
	}

	// Watched species that are undiscovered upstream must now be covered.
	for _, key := range []string{"snow_leopard", "tahr", "water_buffalo"} {
		if _, ok := PublishedZoneHours(key); !ok {
			t.Errorf("%s missing from the published table", key)
		}
	}
}
