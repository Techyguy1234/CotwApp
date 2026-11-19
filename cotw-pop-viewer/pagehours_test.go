package cotw

import "testing"

// TestPublishedHoursMatchPages checks the windows the library resolves against
// the community pages, for a sample of reserve/species pairs: every pair that
// has disagreed before (Delta's raccoon, whitetail and feral pig, the Rockies
// puma and bighorn, Hirschfelden's set, Australia's feral pig), plus the pairs
// whose slot order the pages only get right when the page-specific variant is
// used.
//
// Midnight is written 00:00 on the pages and 24 in the table; both mean the end
// of the day, so ends are compared modulo that.
func TestPublishedHoursMatchPages(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	opts.DisableScheduleLearning = true
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}
	// The pages, transcribed: reserve -> species -> windows.
	pages := map[string]map[string][][3]float64{
		"hirschfelden": {
			"canada_goose": {{3, 0, 5}, {1, 5, 7}, {3, 7, 9}, {1, 9, 11}, {3, 11, 13}, {1, 13, 15}, {3, 15, 17}, {1, 17, 19}, {3, 19, 24}},
			"eu_bison":     {{3, 0, 4}, {1, 4, 7}, {1, 7, 10}, {2, 10, 14}, {3, 14, 17}, {1, 17, 20}, {3, 20, 24}},
			"eu_rabbit":    {{1, 5, 9}, {1, 12, 16}, {1, 19, 23}},
			"fallow_deer":  {{3, 0, 3}, {3, 3, 6}, {1, 6, 10}, {2, 10, 13}, {3, 13, 17}, {1, 17, 21}, {3, 21, 24}},
		},
		"delta": {
			"raccoon":        {{2, 0, 3}, {1, 3, 6}, {3, 6, 10}, {1, 10, 13}, {3, 13, 16}, {3, 16, 20}, {1, 20, 24}},
			"whitetail_deer": {{3, 0, 4}, {1, 4, 8}, {2, 8, 11}, {3, 11, 14}, {1, 14, 18}, {1, 18, 21}, {3, 21, 24}},
			"feral_pig":      {{3, 0, 3}, {2, 3, 6}, {1, 6, 10}, {3, 10, 14}, {1, 14, 17}, {1, 17, 20}, {3, 20, 24}},
		},
		"rockies": {
			"puma":          {{2, 0, 3}, {1, 3, 6}, {3, 6, 9}, {3, 9, 12}, {3, 12, 15}, {1, 15, 18}, {3, 18, 21}, {1, 21, 24}},
			"bighorn_sheep": {{3, 0, 3}, {1, 3, 6}, {1, 6, 9}, {3, 9, 12}, {2, 12, 16}, {1, 16, 20}, {3, 20, 24}},
		},
		"australia": {
			"feral_pig": {{2, 0, 3}, {3, 3, 6}, {1, 6, 10}, {3, 10, 14}, {1, 14, 17}, {1, 17, 20}, {3, 20, 24}},
		},
	}
	for _, r := range save.Reserves {
		bySpecies, ok := pages[r.Key]
		if !ok {
			continue
		}
		for _, sp := range r.Species {
			want, ok := bySpecies[sp.SpeciesKey]
			if !ok {
				continue
			}
			got := map[[3]float64]bool{}
			for _, sl := range save.Timetable(r.Index, sp.SpeciesKey) {
				got[[3]float64{float64(sl.Type), sl.Start, sl.End}] = true
			}
			if len(got) != len(want) {
				t.Errorf("%s %s: %d windows, page has %d (%v)", r.Key, sp.SpeciesKey, len(got), len(want), got)
				continue
			}
			for _, wnt := range want {
				if !got[wnt] {
					t.Errorf("%s %s: page window %v missing from the timetable %v", r.Key, sp.SpeciesKey, wnt, got)
				}
			}
		}
	}
}

// TestHourRendering checks the text form the pages use, so displaying a window
// never shows midnight as 24.
func TestHourRendering(t *testing.T) {
	cases := []struct {
		w    HourWindow
		want string
	}{
		{HourWindow{Start: 4, End: 8}, "04:00-08:00"},
		{HourWindow{Start: 21, End: 24}, "21:00-00:00"},
		{HourWindow{Start: 0, End: 5}, "00:00-05:00"},
		{HourWindow{Start: 8.5, End: 10.25}, "08:30-10:15"},
	}
	for _, c := range cases {
		if got := c.w.String(); got != c.want {
			t.Errorf("%v.String() = %q, want %q", c.w, got, c.want)
		}
	}
	if got, want := (ScheduleSlot{Index: 2, Type: 2, Start: 8, End: 11}).String(), "08:00-11:00 drinking"; got != want {
		t.Errorf("slot string = %q, want %q", got, want)
	}
}
