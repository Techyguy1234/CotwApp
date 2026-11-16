package cotw

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"cotw-pop-viewer/adf"
)

// Options configures game data discovery and caching.
type Options struct {
	// InstallDir is the COTW install folder (the one holding archives_win64).
	// Empty means auto-detect from the Steam library folders.
	InstallDir string
	// CachePath is the cache file. Empty means <user cache>/cotw-save-reader/gamedata.json.
	CachePath string
	// Language selects the localization column to cache (0 = English, 1 French,
	// 2 German, 3 Spanish, 4 Russian, 5 Polish, 6 Japanese, 7 Portuguese,
	// 8 Czech, 9 Chinese).
	Language int
	// ForceRebuild ignores an existing cache and re-scans the archives.
	ForceRebuild bool
	// DisableDeepDefinitionSearch skips the word scan that names regional
	// population definitions; unresolved species are then reported by hash.
	DisableDeepDefinitionSearch bool
	// Progress, when set, receives scan progress.
	Progress func(Progress)
	// ReserveIndexKeys overrides or extends the built-in mapping from save file
	// number to reserve key.
	ReserveIndexKeys map[int]string
	// TrophyMaximums overrides or extends the per-species maximum trophy
	// ratings used to rate each animal's tier (see trophy.go). The game does
	// not ship these as data, so the library carries documented community
	// values; supply your own here to correct or add species.
	TrophyMaximums map[string]float64
	// ZoneHours overrides the daily window of a species' need zones for zones
	// the player has not discovered. Keys are a species key ("whitetail_deer")
	// or a reserve/species pair ("rockies/whitetail_deer"); the pair wins. The
	// game does not ship a timetable, so this sits above the learned cache and
	// the published fallback but below what the save itself revealed.
	ZoneHours map[string][]ScheduleSlot
	// DisableScheduleLearning stops the library from writing the timetables it
	// learns from saves into the schedule store beside the cache.
	DisableScheduleLearning bool
}

// GameInfo describes the loaded game data.
type GameInfo struct {
	InstallDir    string    `json:"install_dir"`
	Version       string    `json:"version"`
	VersionSource string    `json:"version_source"`
	CachePath     string    `json:"cache_path"`
	CacheRebuilt  bool      `json:"cache_rebuilt"`
	BuiltAt       time.Time `json:"built_at"`
	Language      int       `json:"language"`
	Species       int       `json:"species"`
	Strings       int       `json:"strings"`
	PopDefs       int       `json:"pop_defs"`
	DataBooks     int       `json:"data_books"`
}

// Game is the lookup data extracted from the installed game: localized strings,
// species records, population definitions and reserve names.
type Game struct {
	info         GameInfo
	opts         Options
	strings      map[uint32]string
	species      []SpeciesRecord
	speciesByKey map[string]int
	popDefs      map[uint32]PopDef
	reserveKeys  []string
	reserveNames map[string]string
	// zoneSpecies maps hash32("animal_<codex id>_name") to a species index.
	zoneSpecies map[uint32]int
	// maleOnlyTrophy holds the species whose trophy organ is carried by males
	// only, read from the install's own species organ sheets.
	maleOnlyTrophy map[string]bool
	// zoneGeom holds need zone geometry by zone id and spawn area geometry by
	// spawn area id, both from the game's spawn bitmaps (see zones.go).
	zoneGeom   map[uint32]ZoneGeometry
	spawnAreas map[uint32]ZoneGeometry
	// zoneAll keeps every record as scanned (duplicates included) so the
	// sidecar round-trips losslessly, and zoneCandidates keeps the ids that
	// have more than one record.
	zoneAll        []ZoneGeometry
	zoneCandidates map[uint32][]ZoneGeometry
	// zoneCollisions counts zone ids that appeared more than once in the
	// shipped bitmaps (clipped across tiles); the largest volume was kept.
	zoneCollisions int
	tokens         []string
	deepMu         sync.Mutex
	tried          map[uint32]bool
	cacheMu        sync.Mutex
}

// cacheFormat is bumped when the cached fields change, so an older cache is
// rebuilt rather than silently missing data. 3 added the male-only trophy set,
// 4 added need zone and spawn area geometry.
const cacheFormat = 4

// OpenGame loads the game's lookup data, rebuilding the cache when the installed
// game version changed (or when no usable cache exists).
//
// The version is checked on every call: the install's version stamp, else the
// Steam app manifest build id, else a signature over the archive files.
func OpenGame(opts Options) (*Game, error) {
	installDir := opts.InstallDir
	if installDir == "" {
		dir, err := DefaultInstallDir()
		if err != nil {
			return nil, err
		}
		installDir = dir
	}
	info, err := os.Stat(installDir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("cotw: install folder %s: %v", installDir, err)
	}

	version, source := detectGameVersion(installDir)
	cachePath := opts.CachePath
	if cachePath == "" {
		cachePath = defaultCachePath()
	}

	g := &Game{
		opts:    opts,
		strings: make(map[uint32]string, 1<<15),
		popDefs: make(map[uint32]PopDef, 2048),
		info: GameInfo{
			InstallDir:    installDir,
			Version:       version,
			VersionSource: source,
			CachePath:     cachePath,
			Language:      opts.Language,
		},
	}

	if !opts.ForceRebuild {
		if c, err := readCache(cachePath); err == nil && c.Format == cacheFormat &&
			c.Version == version && c.Language == opts.Language {
			// The zone geometry lives in a sidecar file; a cache without it is
			// incomplete, so fall through and rebuild.
			if geoms, err := readZoneSidecar(cachePath); err == nil {
				g.applyCache(c)
				g.setZoneGeometry(geoms)
				g.info.CacheRebuilt = false
				return g, nil
			}
		}
	}

	if err := g.rebuild(); err != nil {
		return nil, err
	}
	g.info.CacheRebuilt = true
	_ = g.writeCache()
	if cachePath != "" {
		_ = writeZoneSidecar(cachePath, g.allZoneGeometry())
	}
	return g, nil
}

// Info returns a copy of the game data description.
func (g *Game) Info() GameInfo { return g.info }

// InstallDir returns the game install folder used for the lookup data.
func (g *Game) InstallDir() string { return g.info.InstallDir }

// Version returns the game version the cache was built from.
func (g *Game) Version() string { return g.info.Version }

// String resolves a localization hash (need zone species, reserve names, UI).
func (g *Game) String(hash uint32) (string, bool) {
	s, ok := g.strings[hash]
	return s, ok
}

// Species returns every species the game data knows.
func (g *Game) Species() []SpeciesRecord {
	out := make([]SpeciesRecord, len(g.species))
	copy(out, g.species)
	return out
}

// SpeciesByKey looks up a species by its internal key.
func (g *Game) SpeciesByKey(key string) (SpeciesRecord, bool) {
	key = strings.ToLower(key)
	if i, ok := g.speciesByKey[key]; ok {
		return g.species[i], true
	}
	return SpeciesRecord{}, false
}

// PopulationDefinition resolves a population NameHashId to its definition and
// species. Unresolved ids get a word scan the first time they are asked for
// (unless disabled in the options).
func (g *Game) PopulationDefinition(hash uint32) (PopDef, SpeciesRecord, bool) {
	defs := g.ResolvePopulationDefinitions([]uint32{hash})
	if def, ok := defs[hash]; ok {
		return def, g.species[def.Species], true
	}
	return PopDef{}, SpeciesRecord{}, false
}

// ResolvePopulationDefinitions looks several ids up at once, running one word
// scan for whichever ids are still unknown.
func (g *Game) ResolvePopulationDefinitions(hashes []uint32) map[uint32]PopDef {
	out := make(map[uint32]PopDef, len(hashes))
	unknown := make(map[uint32]bool)
	g.deepMu.Lock()
	for _, h := range hashes {
		if def, ok := g.popDefs[h]; ok {
			out[h] = def
		} else {
			unknown[h] = true
		}
	}
	g.deepMu.Unlock()
	if len(unknown) > 0 && !g.opts.DisableDeepDefinitionSearch {
		g.resolveDeep(unknown)
		g.deepMu.Lock()
		for h := range unknown {
			if def, ok := g.popDefs[h]; ok {
				out[h] = def
			}
		}
		g.deepMu.Unlock()
	}
	return out
}

// MaleOnlyTrophySpecies returns the species whose trophy organ is carried by
// males only, as read from the installed game's per-species organ sheets (their
// "Trophy type" column is "male" rather than "both"). A female of one of these
// species is rated no medal regardless of its score.
func (g *Game) MaleOnlyTrophySpecies() map[string]bool {
	out := make(map[string]bool, len(g.maleOnlyTrophy))
	for k, v := range g.maleOnlyTrophy {
		out[k] = v
	}
	return out
}

// ReserveKeys returns every reserve key found in the game data.
func (g *Game) ReserveKeys() []string {
	out := make([]string, len(g.reserveKeys))
	copy(out, g.reserveKeys)
	return out
}

// ReserveName returns the localized name of a reserve key ("hirschfelden" ->
// "HIRSCHFELDEN HUNTING RESERVE"), or "".
func (g *Game) ReserveName(key string) string { return g.reserveNames[strings.ToLower(key)] }

// resolveDeep runs the regional-definition word scan for the ids that have not
// been tried yet and keeps whatever it finds, so later parses inherit it (and it
// is written back to the cache).
func (g *Game) resolveDeep(want map[uint32]bool) {
	g.deepMu.Lock()
	if g.tried == nil {
		g.tried = make(map[uint32]bool, 128)
	}
	pending := make(map[uint32]bool, len(want))
	for h := range want {
		if _, known := g.popDefs[h]; known || g.tried[h] {
			continue
		}
		g.tried[h] = true
		pending[h] = true
	}
	g.deepMu.Unlock()
	if len(pending) == 0 {
		return
	}

	found := deepDefinitionSearch(pending, g.species, g.tokens)
	if len(found) == 0 {
		return
	}
	g.deepMu.Lock()
	for h, def := range found {
		g.popDefs[h] = def
	}
	g.deepMu.Unlock()

	_ = g.writeCache()
}

// rebuild scans the game archives and builds the in-memory tables.
func (g *Game) rebuild() error {
	scan, err := scanAssets(g.info.InstallDir, g.opts.Language, g.opts.Progress)
	if err != nil {
		return err
	}

	g.strings = scan.strings
	g.species = buildSpecies(scan)
	g.tokens = localizationTokens(scan.strings)
	g.maleOnlyTrophy = make(map[string]bool, len(scan.maleOnly))
	for _, key := range scan.maleOnly {
		g.maleOnlyTrophy[key] = true
	}
	geoms := make([]ZoneGeometry, 0, len(scan.zoneGeom)+len(scan.spawnAreas))
	geoms = append(geoms, scan.zoneGeom...)
	geoms = append(geoms, scan.spawnAreas...)
	g.setZoneGeometry(geoms)
	g.reserveKeys = dedupeStrings(scan.reserveKeys)
	sort.Strings(g.reserveKeys)

	g.speciesByKey = make(map[string]int, len(g.species))
	for i, sp := range g.species {
		g.speciesByKey[strings.ToLower(sp.Key)] = i
	}

	g.popDefs = cheapPopDefs(g.species)
	g.reserveNames = buildReserveNames(g.reserveKeys, scan.strings)
	g.buildZoneSpecies()

	g.info.Species = len(g.species)
	g.info.Strings = len(g.strings)
	g.info.PopDefs = len(g.popDefs)
	g.info.DataBooks = scan.books
	g.info.BuiltAt = time.Now().UTC()
	return nil
}

// buildZoneSpecies indexes the need-zone species hashes
// (hash32("animal_<codex id>_name")) for O(1) lookups.
func (g *Game) buildZoneSpecies() {
	g.zoneSpecies = make(map[uint32]int, len(g.species)*3)
	for i := range g.species {
		sp := &g.species[i]
		candidates := make([]string, 0, 6)
		if sp.CodexID != "" {
			candidates = append(candidates, sp.CodexID)
		}
		candidates = append(candidates, speciesNameCandidates(sp.Key)...)
		for _, c := range candidates {
			h := adf.Hash32String("animal_" + c + "_name")
			if _, exists := g.zoneSpecies[h]; !exists {
				g.zoneSpecies[h] = i
			}
		}
	}
}

// buildSpecies merges the species keys from the data books with the wildlife
// codex ids and resolves each display name from the localization.
func buildSpecies(scan *scanResult) []SpeciesRecord {
	keys := dedupeStrings(append(append([]string{}, scan.sheetNames...), scan.speciesCols...))
	sort.Strings(keys)

	codex := make(map[string]string, len(scan.codexIDs)) // compact form -> codex id
	for _, id := range dedupeStrings(scan.codexIDs) {
		codex[strings.ReplaceAll(id, "_", "")] = id
	}

	out := make([]SpeciesRecord, 0, len(keys))
	for _, key := range keys {
		if !looksLikeSpeciesKey(key) {
			continue
		}
		rec := SpeciesRecord{Key: key}
		compact := strings.ReplaceAll(key, "_", "")
		if id, ok := codex[compact]; ok {
			rec.CodexID = id
		}
		// Try the codex spelling first, then every spelling variant of the key.
		candidates := make([]string, 0, 6)
		if rec.CodexID != "" {
			candidates = append(candidates, rec.CodexID)
		}
		candidates = append(candidates, speciesNameCandidates(key)...)
		for _, c := range candidates {
			if name, ok := scan.strings[adf.Hash32String("animal_"+c+"_name")]; ok {
				rec.Name = name
				if rec.CodexID == "" {
					rec.CodexID = c
				}
				break
			}
		}
		out = append(out, rec)
	}
	return out
}

// buildReserveNames maps each reserve key to its localized display name.
func buildReserveNames(keys []string, localized map[uint32]string) map[string]string {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		for _, form := range []string{
			"reserve_" + key + "_name_uppercase",
			"reserve_" + key + "_name",
		} {
			if name, ok := localized[adf.Hash32String(form)]; ok {
				out[strings.ToLower(key)] = name
				break
			}
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// --- cache ----------------------------------------------------------------

type cacheFile struct {
	Format       int               `json:"format"`
	Version      string            `json:"version"`
	Language     int               `json:"language"`
	BuiltAt      time.Time         `json:"built_at"`
	Strings      map[string]string `json:"strings"`
	Species      []SpeciesRecord   `json:"species"`
	PopDefs      []cachePopDef     `json:"pop_defs"`
	ReserveKeys  []string          `json:"reserve_keys"`
	Tokens       []string          `json:"tokens"`
	ReserveNames map[string]string `json:"reserve_names"`
	// MaleOnlyTrophy lists species whose trophy organ only males carry, read
	// from the game's per-species organ sheets.
	MaleOnlyTrophy []string `json:"male_only_trophy,omitempty"`
	// ZoneCollisions is a rebuild-time diagnostic: how many zone ids had more
	// than one bitmap record.
	ZoneCollisions int `json:"zone_collisions,omitempty"`
}

type cachePopDef struct {
	Hash    uint32 `json:"hash"`
	Name    string `json:"name"`
	Species int    `json:"species"`
}

func (g *Game) applyCache(c *cacheFile) {
	g.strings = make(map[uint32]string, len(c.Strings))
	for k, v := range c.Strings {
		var h uint32
		if _, err := fmt.Sscanf(k, "%d", &h); err == nil {
			g.strings[h] = v
		}
	}
	g.species = c.Species
	g.speciesByKey = make(map[string]int, len(c.Species))
	for i, sp := range c.Species {
		g.speciesByKey[strings.ToLower(sp.Key)] = i
	}
	g.popDefs = make(map[uint32]PopDef, len(c.PopDefs))
	for _, d := range c.PopDefs {
		g.popDefs[d.Hash] = PopDef{Name: d.Name, Species: d.Species}
	}
	g.reserveKeys = c.ReserveKeys
	g.tokens = c.Tokens
	g.reserveNames = c.ReserveNames
	if g.reserveNames == nil {
		g.reserveNames = map[string]string{}
	}
	g.maleOnlyTrophy = make(map[string]bool, len(c.MaleOnlyTrophy))
	for _, key := range c.MaleOnlyTrophy {
		g.maleOnlyTrophy[key] = true
	}
	g.zoneCollisions = c.ZoneCollisions
	g.buildZoneSpecies()

	g.info.BuiltAt = c.BuiltAt
	g.info.Species = len(g.species)
	g.info.Strings = len(g.strings)
	g.info.PopDefs = len(g.popDefs)
}

func (g *Game) toCache() *cacheFile {
	c := &cacheFile{
		Format:       cacheFormat,
		Version:      g.info.Version,
		Language:     g.info.Language,
		BuiltAt:      g.info.BuiltAt,
		Strings:      make(map[string]string, len(g.strings)),
		Species:      g.species,
		ReserveKeys:  g.reserveKeys,
		Tokens:       g.tokens,
		ReserveNames: g.reserveNames,
	}
	for key := range g.maleOnlyTrophy {
		c.MaleOnlyTrophy = append(c.MaleOnlyTrophy, key)
	}
	sort.Strings(c.MaleOnlyTrophy)
	c.ZoneCollisions = g.zoneCollisions
	for h, s := range g.strings {
		c.Strings[fmt.Sprint(h)] = s
	}
	c.PopDefs = make([]cachePopDef, 0, len(g.popDefs))
	for h, d := range g.popDefs {
		c.PopDefs = append(c.PopDefs, cachePopDef{Hash: h, Name: d.Name, Species: d.Species})
	}
	sort.Slice(c.PopDefs, func(i, j int) bool { return c.PopDefs[i].Hash < c.PopDefs[j].Hash })
	return c
}

func (g *Game) writeCache() error {
	g.cacheMu.Lock()
	defer g.cacheMu.Unlock()
	if g.info.CachePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(g.info.CachePath), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(g.toCache())
	if err != nil {
		return err
	}
	tmp := g.info.CachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, g.info.CachePath)
}

func readCache(path string) (*cacheFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c cacheFile
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// defaultCachePath is <user cache dir>/cotw-save-reader/gamedata.json.
func defaultCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "cotw-save-reader", "gamedata.json")
}

func runtimeWorkers() int {
	n := runtime.NumCPU()
	if n > 8 {
		n = 8
	}
	if n < 1 {
		n = 1
	}
	return n
}

// decompressZlibBodyLimited inflates at most limit bytes of a zlib stream; the
// partial result is returned even when the stream was cut short.
func decompressZlibBodyLimited(compressed []byte, limit int64) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("cotw: zlib: %w", err)
	}
	defer zr.Close()
	body, readErr := io.ReadAll(io.LimitReader(zr, limit))
	if len(body) == 0 && readErr != nil {
		return nil, fmt.Errorf("cotw: zlib: %w", readErr)
	}
	return body, nil
}
