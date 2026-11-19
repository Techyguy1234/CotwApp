package cotw

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ParseSave reads a COTW save folder and returns everything it contains.
//
// dir is the account folder holding animal_population_0..20 (and usually
// found_need_zones_adf, regions_adf, ...). An empty dir means the first save
// folder found. opts may be zero valued; the game installation is auto-detected
// and its lookup cache is reused, rebuilt only when the installed version
// changed.
func ParseSave(dir string, opts Options) (*Save, error) {
	if dir == "" {
		auto, err := DefaultSaveDir()
		if err != nil {
			return nil, err
		}
		dir = auto
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("cotw: save folder: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cotw: %s is not a folder", dir)
	}

	game, err := OpenGame(opts)
	if err != nil {
		return nil, err
	}

	files, err := populationFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("cotw: no animal_population_N files in %s", dir)
	}

	save := &Save{
		Dir:      dir,
		SteamID:  parseSteamID(dir),
		Game:     game.Info(),
		ParsedAt: time.Now().UTC(),
		Reserves: make([]*Reserve, 0, len(files)),
		Warnings: []string{},
		game:     game,
		opts:     opts,
	}

	// Timetables learned from earlier saves, so a species discovered once keeps
	// its windows for every later parse.
	store, stale := loadHourStore(hourStorePath(game.info.CachePath), game.Version())
	save.hours = store
	if stale {
		save.Warnings = append(save.Warnings,
			"schedule store: written for another game version, ignored and rebuilt from this save")
	}

	byIndex := make(map[int]*Reserve, len(files))
	for _, index := range sortedKeys(files) {
		r := game.parseReserve(files[index], index, opts.ReserveIndexKeys)
		if r.Err != "" {
			save.Warnings = append(save.Warnings, fmt.Sprintf("%s: %s", r.File, r.Err))
		}
		save.Reserves = append(save.Reserves, r)
		byIndex[index] = r
	}

	// Name the species: one batch lookup so a single word scan covers every id
	// that the cheap name shapes did not resolve.
	unknown := map[uint32]bool{}
	for _, r := range save.Reserves {
		for _, sp := range r.Species {
			unknown[sp.NameHashID] = true
		}
	}
	hashes := make([]uint32, 0, len(unknown))
	for h := range unknown {
		hashes = append(hashes, h)
	}
	sort.Slice(hashes, func(i, j int) bool { return hashes[i] < hashes[j] })
	assignSpecies(save.Reserves, game.ResolvePopulationDefinitions(hashes), game)

	// Rate every animal's trophy tier from its score and its species maximum.
	applyTrophyTiers(save, opts, game)

	// Give every group the location of its spawn area.
	attachSpawnAreas(save.Reserves, game)

	// Discovered need zones.
	zonePath := filepath.Join(dir, NeedZoneFileName)
	if NeedZoneFileExists(dir) {
		zones, err := game.loadNeedZones(zonePath)
		if err != nil {
			save.Warnings = append(save.Warnings, fmt.Sprintf("%s: %v", NeedZoneFileName, err))
		}
		for index, zs := range zones {
			if r := byIndex[index]; r != nil {
				r.NeedZones = zs
			}
		}
		for _, r := range save.Reserves {
			linkNeedZones(r)
		}
	}

	// Complete every group's zone list with the zones the player has not found.
	fillGroupNeedZones(save)

	// Give every group its daily timetable: one slot per zone path entry, each
	// with the window of that slot.
	save.assignGroupSlots()

	// Keep what this save revealed so later parses need fewer discoveries.
	save.learnZoneHours()

	return save, nil
}

// Summary is a small aggregate of a parsed save, handy for UI headers.
type Summary struct {
	Reserves        int `json:"reserves"`
	Populated       int `json:"populated_reserves"`
	Species         int `json:"species_populations"`
	Groups          int `json:"groups"`
	Animals         int `json:"animals"`
	NeedZones       int `json:"need_zones"`
	UnmappedSpecies int `json:"unmapped_species"`
}

// Summarize aggregates a parsed save.
func (s *Save) Summarize() Summary {
	out := Summary{Reserves: len(s.Reserves)}
	for _, r := range s.Reserves {
		if len(r.Species) == 0 {
			continue
		}
		out.Populated++
		out.NeedZones += len(r.NeedZones)
		for _, sp := range r.Species {
			out.Species++
			out.Groups += len(sp.Groups)
			out.Animals += sp.AnimalCount
			if !sp.Known {
				out.UnmappedSpecies++
			}
		}
	}
	return out
}
