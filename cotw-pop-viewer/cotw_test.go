package cotw

import (
	"os"
	"testing"
)

// integrationOptions returns options pointing at real data, skipping the test
// when this machine has no COTW install or save.
func integrationOptions(t *testing.T) (saveDir string, opts Options) {
	t.Helper()
	saveDir = os.Getenv("COTW_SAVE_DIR")
	if saveDir == "" {
		dir, err := DefaultSaveDir()
		if err != nil {
			t.Skipf("no COTW save folder: %v", err)
		}
		saveDir = dir
	}
	opts = Options{InstallDir: os.Getenv("COTW_INSTALL_DIR")}
	if opts.InstallDir == "" {
		dir, err := DefaultInstallDir()
		if err != nil {
			t.Skipf("no COTW install: %v", err)
		}
		opts.InstallDir = dir
	}
	return saveDir, opts
}

func TestParseSaveIntegration(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}

	info := save.Game
	t.Logf("game: %s (%s) at %s", info.Version, info.VersionSource, info.InstallDir)
	t.Logf("cache: %s rebuilt=%v species=%d strings=%d popdefs=%d",
		info.CachePath, info.CacheRebuilt, info.Species, info.Strings, info.PopDefs)

	sum := save.Summarize()
	t.Logf("save: %d reserves (%d populated), %d species populations, %d groups, %d animals, %d need zones",
		sum.Reserves, sum.Populated, sum.Species, sum.Groups, sum.Animals, sum.NeedZones)

	if sum.Populated == 0 || sum.Animals == 0 {
		t.Fatal("parsed no animals")
	}
	if sum.UnmappedSpecies != 0 {
		t.Errorf("%d species populations were not resolved to a species", sum.UnmappedSpecies)
	}

	for _, r := range save.Reserves {
		if r.Err != "" {
			t.Errorf("reserve %d (%s): %s", r.Index, r.File, r.Err)
			continue
		}
		if len(r.Species) == 0 {
			continue
		}
		if r.ReserveSeed == 0 {
			t.Logf("reserve %d has seed 0", r.Index)
		}
		for _, sp := range r.Species {
			if sp.NameHashID == 0 {
				continue // empty population slot
			}
			if sp.SpeciesKey == "" || sp.SpeciesName == "" || sp.Definition == "" {
				t.Errorf("reserve %d population %d: id 0x%08x unresolved (key=%q name=%q definition=%q)",
					r.Index, sp.Index, sp.NameHashID, sp.SpeciesKey, sp.SpeciesName, sp.Definition)
			}
			counted := 0
			for _, g := range sp.Groups {
				if len(g.NeedZoneGuids) == 0 {
					t.Errorf("reserve %d pop %d group %d has no need zone ids", r.Index, sp.Index, g.Index)
				}
				counted += len(g.Animals)
			}
			if counted != sp.AnimalCount {
				t.Errorf("reserve %d pop %d: counted %d animals, aggregate says %d",
					r.Index, sp.Index, counted, sp.AnimalCount)
			}
		}
	}
}

func TestNeedZoneLinks(t *testing.T) {
	saveDir, opts := integrationOptions(t)
	save, err := ParseSave(saveDir, opts)
	if err != nil {
		t.Fatalf("ParseSave: %v", err)
	}

	var zones int
	for _, r := range save.Reserves {
		zones += len(r.NeedZones)
	}
	if zones == 0 {
		t.Skip("this save has no discovered need zones")
	}

	for _, r := range save.Reserves {
		for _, z := range r.NeedZones {
			if z.EndHour < z.StartHour {
				t.Errorf("reserve %d zone 0x%08x: end %.1f before start %.1f", r.Index, z.IDU, z.EndHour, z.StartHour)
			}
			if z.Position.X == 0 && z.Position.Y == 0 && z.Position.Z == 0 {
				t.Errorf("reserve %d zone 0x%08x has a zero position", r.Index, z.IDU)
			}
			if z.AnimalTypeHash != 0 && z.AnimalSpeciesKey == "" {
				t.Logf("reserve %d zone 0x%08x: species hash 0x%08x unmapped", r.Index, z.IDU, z.AnimalTypeHash)
			}
		}
		for _, sp := range r.Species {
			for _, g := range sp.Groups {
				for _, z := range g.NeedZones {
					ref := GroupRef{SpeciesIndex: sp.Index, SpeciesKey: sp.SpeciesKey, GroupIndex: g.Index}
					if !containsGroupRef(z.Groups, ref) {
						t.Errorf("reserve %d group %d: zone 0x%08x has no back reference", r.Index, g.Index, z.IDU)
					}
				}
			}
		}
	}
	t.Logf("verified %d need zones", zones)
}

func TestCacheReuse(t *testing.T) {
	_, opts := integrationOptions(t)

	first, err := OpenGame(opts)
	if err != nil {
		t.Fatalf("OpenGame: %v", err)
	}
	if first.Info().Species == 0 || first.Info().Strings == 0 {
		t.Fatalf("game data is empty: %+v", first.Info())
	}

	cached, err := OpenGame(opts)
	if err != nil {
		t.Fatalf("OpenGame (cached): %v", err)
	}
	if cached.Info().CacheRebuilt {
		t.Error("second OpenGame rebuilt the cache even though the version did not change")
	}
	if cached.Info().Version != first.Info().Version {
		t.Errorf("version changed between loads: %q then %q", first.Info().Version, cached.Info().Version)
	}

	if os.Getenv("COTW_TEST_REBUILD") == "" {
		t.Log("skipping forced rebuild (set COTW_TEST_REBUILD=1 to include it)")
		return
	}
	forced, err := OpenGame(Options{InstallDir: opts.InstallDir, ForceRebuild: true})
	if err != nil {
		t.Fatalf("OpenGame (forced): %v", err)
	}
	if !forced.Info().CacheRebuilt {
		t.Error("ForceRebuild did not rebuild the cache")
	}
}

func TestReserveNamesResolve(t *testing.T) {
	_, opts := integrationOptions(t)
	game, err := OpenGame(opts)
	if err != nil {
		t.Fatalf("OpenGame: %v", err)
	}
	keys := game.ReserveKeys()
	if len(keys) == 0 {
		t.Fatal("no reserve keys were found in the game data")
	}
	named := 0
	for _, key := range keys {
		if game.ReserveName(key) != "" {
			named++
		}
	}
	if named == 0 {
		t.Fatalf("%d reserve keys but none had a localized name", len(keys))
	}
	t.Logf("%d reserve keys, %d named", len(keys), named)
}
