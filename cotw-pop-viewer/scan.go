package cotw

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"cotw-pop-viewer/adf"
)

// Progress is reported while the game archives are being scanned.
type Progress struct {
	ArcsTotal     int
	ArcsDone      int
	EntriesRead   int
	StringLookups int
	DataBooks     int
	Arc           string
}

// tabEntry is one record of a .tab archive index.
type tabEntry struct {
	Hash   uint32
	Offset int64
	Size   int64
}

// archive is one game0.arc + game0.tab pair.
type archive struct {
	name string
	path string
	tab  string
}

var arcNamePattern = regexp.MustCompile(`^game(\d+)\.arc$`)

// findArchives lists the game's archive pairs, ordered by their number.
func findArchives(installDir string) ([]archive, error) {
	dir := filepath.Join(installDir, "archives_win64")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cotw: game archives not found in %s: %w", dir, err)
	}
	type pair struct {
		n int
		a archive
	}
	var pairs []pair
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := arcNamePattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		base := strings.TrimSuffix(filepath.Join(dir, e.Name()), ".arc")
		if _, err := os.Stat(base + ".tab"); err != nil {
			continue
		}
		pairs = append(pairs, pair{n: n, a: archive{name: e.Name(), path: base + ".arc", tab: base + ".tab"}})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].n < pairs[j].n })
	out := make([]archive, len(pairs))
	for i, p := range pairs {
		out[i] = p.a
	}
	return out, nil
}

// readTab parses a .tab index: a 12 byte header followed by 12 byte records of
// [hash, offset, size].
func readTab(path string) ([]tabEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 12 || string(data[0:4]) != "TAB\x00" {
		return nil, fmt.Errorf("cotw: %s is not a TAB index", filepath.Base(path))
	}
	records := (len(data) - 12) / 12
	out := make([]tabEntry, 0, records)
	for p := 12; p+12 <= len(data); p += 12 {
		out = append(out, tabEntry{
			Hash:   binary.LittleEndian.Uint32(data[p:]),
			Offset: int64(binary.LittleEndian.Uint32(data[p+4:])),
			Size:   int64(binary.LittleEndian.Uint32(data[p+8:])),
		})
	}
	return out, nil
}

// scanResult is everything the asset scan collects.
type scanResult struct {
	strings     map[uint32]string // localization hash -> text (chosen language)
	sheetNames  []string          // every XLS sheet name (candidate species keys)
	speciesCols []string          // species_data header row (more species keys)
	codexIDs    []string          // wildlife codex ids ("animal_..." stripped)
	reserveKeys []string          // reserve keys from the codex Regions sheet
	maleOnly    []string          // species whose trophy organ only males carry
	zoneGeom    []ZoneGeometry    // need zone geometry from the spawn bitmaps
	spawnAreas  []ZoneGeometry    // group spawn area geometry, likewise
	lookups     int
	books       int
}

// newScanResult returns an empty result with its map ready.
func newScanResult() *scanResult {
	return &scanResult{strings: make(map[uint32]string, 256)}
}

const (
	headReadLimit = 64 << 10  // ADF header + tables are far smaller than this
	fullReadLimit = 128 << 20 // biggest asset we will pull in whole (the main
	// localization lookup is ~39 MB and holds most of the strings)
	zlibHeadOutput = 512 << 10
)

// scanAssets walks every archive and extracts the localization strings and the
// XLS data books.
func scanAssets(installDir string, language int, progress func(Progress)) (*scanResult, error) {
	arcs, err := findArchives(installDir)
	if err != nil {
		return nil, err
	}
	if len(arcs) == 0 {
		return nil, fmt.Errorf("cotw: no game archives found under %s", installDir)
	}

	res := newScanResult()
	var (
		mu     sync.Mutex
		done   int
		shared = newScanResult()
		wg     sync.WaitGroup
		sem    = make(chan struct{}, runtimeWorkers())
	)

	for _, a := range arcs {
		wg.Add(1)
		go func(a archive) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			local := newScanResult()
			entriesRead := 0
			f, err := os.Open(a.path)
			if err == nil {
				tab, terr := readTab(a.tab)
				if terr == nil {
					for _, e := range tab {
						if e.Size <= 0 {
							continue
						}
						if err := scanEntry(f, e, local, language); err != nil {
							continue
						}
						entriesRead++
					}
				}
				f.Close()
			}

			mu.Lock()
			for h, s := range local.strings {
				shared.strings[h] = s
			}
			shared.sheetNames = append(shared.sheetNames, local.sheetNames...)
			shared.speciesCols = append(shared.speciesCols, local.speciesCols...)
			shared.codexIDs = append(shared.codexIDs, local.codexIDs...)
			shared.reserveKeys = append(shared.reserveKeys, local.reserveKeys...)
			shared.maleOnly = append(shared.maleOnly, local.maleOnly...)
			shared.zoneGeom = append(shared.zoneGeom, local.zoneGeom...)
			shared.spawnAreas = append(shared.spawnAreas, local.spawnAreas...)
			shared.lookups += local.lookups
			shared.books += local.books
			done++
			p := Progress{
				ArcsTotal: len(arcs), ArcsDone: done, EntriesRead: entriesRead,
				StringLookups: shared.lookups, DataBooks: shared.books, Arc: a.name,
			}
			mu.Unlock()
			if progress != nil {
				progress(p)
			}
		}(a)
	}
	wg.Wait()

	res.strings = shared.strings
	res.sheetNames = shared.sheetNames
	res.speciesCols = shared.speciesCols
	res.codexIDs = shared.codexIDs
	res.reserveKeys = shared.reserveKeys
	res.maleOnly = shared.maleOnly
	res.zoneGeom = shared.zoneGeom
	res.spawnAreas = shared.spawnAreas
	res.lookups = shared.lookups
	res.books = shared.books
	return res, nil
}

// scanEntry inspects one archive entry and extracts it when it is a string
// lookup or a data book.
func scanEntry(f *os.File, e tabEntry, out *scanResult, language int) error {
	head := make([]byte, headReadLimit)
	n, err := f.ReadAt(head, e.Offset)
	head = head[:n]
	if n < 4 {
		if err != nil && n == 0 {
			return nil
		}
	}
	if !isADFImage(head) {
		return nil
	}

	a, err := parseADFImageBest(f, e, head)
	if err != nil {
		return nil
	}

	for i := range a.Instances {
		name := a.InstanceTypeName(i)
		v := a.Values[i]
		if v == nil {
			continue
		}
		switch name {
		case "StringLookup":
			extractStringLookup(v, language, out.strings)
			out.lookups++
		case "XLSBook":
			extractDataBook(v, out)
			out.books++
		case "BitmapData":
			extractBitmapZones(v, out)
		}
	}
	return nil
}

// parseADFImageBest parses an entry from the head window, falling back to the
// whole entry when the head was not enough (large assets keep their tables past
// the window).
func parseADFImageBest(f *os.File, e tabEntry, head []byte) (*adf.Adf, error) {
	if a, err := parseHeadBytes(head); err == nil {
		return a, nil
	}
	if int64(len(head)) >= e.Size || e.Size > fullReadLimit {
		return nil, fmt.Errorf("cotw: %s entry at %d unreadable", "archive", e.Offset)
	}
	raw := make([]byte, e.Size)
	if _, err := f.ReadAt(raw, e.Offset); err != nil {
		return nil, err
	}
	return parseADFImage(raw)
}

// parseHeadBytes parses an ADF image prefix: bare, or a zlib wrapped one
// inflated as far as the window allows.
func parseHeadBytes(head []byte) (*adf.Adf, error) {
	if len(head) >= 4 && string(head[0:4]) == " FDA" {
		return adf.Parse(head)
	}
	body, err := decompressZlibPrefix(head)
	if err != nil {
		return nil, err
	}
	return adf.Parse(body)
}

// isADFImage reports whether the bytes start a bare or zlib wrapped ADF.
func isADFImage(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	if string(data[0:4]) == " FDA" {
		return true
	}
	return len(data) >= 20 && string(data[0:4]) == "SAVE" && string(data[16:20]) == "COMP"
}

// decompressZlibPrefix inflates as much of a zlib stream as it can, up to
// zlibHeadOutput bytes.
func decompressZlibPrefix(head []byte) ([]byte, error) {
	if len(head) < 36 {
		return nil, fmt.Errorf("cotw: short zlib entry")
	}
	body, err := decompressZlibBodyLimited(head[32:], zlibHeadOutput)
	if err != nil {
		return nil, err
	}
	return stripCompressionPrefix(body)
}

// extractStringLookup pulls the (hash -> text) pairs of one language out of a
// StringLookup instance.
func extractStringLookup(root *adf.Value, language int, dst map[uint32]string) {
	languages := root.Field("Languages")
	text := root.Field("Text")
	if languages == nil || text == nil {
		return
	}
	if language < 0 || language >= len(languages.Elems) {
		language = 0
	}
	if language >= len(languages.Elems) {
		return
	}
	blob := text.Bytes
	pairs := languages.Elems[language].Field("SortedPairs")
	if pairs == nil {
		return
	}
	for _, p := range pairs.Elems {
		h := p.Field("Hash")
		off := p.Field("TextOffset")
		if h == nil || off == nil {
			continue
		}
		hash, err := h.Uint32()
		if err != nil {
			continue
		}
		pos, err := off.Uint32()
		if err != nil || int(pos) >= len(blob) {
			continue
		}
		end := pos
		for int(end) < len(blob) && blob[end] != 0 {
			end++
		}
		if _, exists := dst[hash]; exists {
			continue
		}
		dst[hash] = string(blob[pos:end])
	}
}

// extractDataBook records the sheet names of an XLS data book and, for the two
// books that carry them, the species columns, codex ids and reserve keys.
func extractDataBook(root *adf.Value, out *scanResult) {
	book := newDataBook(root)
	if book == nil || len(book.sheets) == 0 {
		return
	}
	lowercaseOnly := true
	for _, s := range book.sheets {
		name := book.sheetName(s)
		if name == "" || strings.ToLower(name) != name {
			lowercaseOnly = false
			break
		}
	}
	if lowercaseOnly && len(book.sheets) >= 50 {
		for _, s := range book.sheets {
			out.sheetNames = append(out.sheetNames, book.sheetName(s))
		}
		// These are the per-species organ sheets. Their "Trophy type" column
		// says whether the trophy organ is carried by males only ("male") or by
		// both sexes ("both"), which is what decides if a female rates NONE.
		for _, s := range book.sheets {
			name := book.sheetName(s)
			col := book.column(s, "Trophy type")
			if name == "" || col < 0 {
				continue
			}
			_, rows := book.dims(s)
			sawMale, sawBoth := false, false
			for r := 1; r < rows; r++ {
				if text, ok := book.cellAt(s, r, col).(string); ok {
					switch text {
					case "male":
						sawMale = true
					case "both":
						sawBoth = true
					}
				}
			}
			// "male" with no "both" means only males carry the organ. A sheet
			// with neither (weight-based species) is not male-only.
			if sawMale && !sawBoth {
				out.maleOnly = append(out.maleOnly, name)
			}
		}
	}

	if sheet := book.sheet("species_data"); sheet != nil {
		row := book.row(sheet, 0)
		for i, cell := range row {
			if i < 2 {
				continue // "0.0" label, "base" world name
			}
			if text, ok := cell.(string); ok && looksLikeSpeciesKey(text) {
				out.speciesCols = append(out.speciesCols, text)
			}
		}
	}

	if sheet := book.sheet("Wildlife"); sheet != nil {
		if col := book.column(sheet, "id"); col >= 0 {
			_, rows := book.dims(sheet)
			for r := 1; r < rows; r++ {
				if text, ok := book.cellAt(sheet, r, col).(string); ok {
					text = strings.TrimPrefix(text, "animal_")
					if text != "" && !strings.HasPrefix(text, "animal") {
						out.codexIDs = append(out.codexIDs, text)
					}
				}
			}
		}
	}

	if sheet := book.sheet("Regions"); sheet != nil {
		if col := book.column(sheet, "reserve_id"); col >= 0 {
			seen := map[string]bool{}
			_, rows := book.dims(sheet)
			for r := 1; r < rows; r++ {
				text, ok := book.cellAt(sheet, r, col).(string)
				if !ok {
					continue
				}
				key := reserveKeyFromLocalizationKey(text)
				if key == "" || seen[key] {
					continue
				}
				seen[key] = true
				out.reserveKeys = append(out.reserveKeys, key)
			}
		}
	}
}

// reserveKeyFromLocalizationKey turns "reserve_hirschfelden_name_uppercase" into
// "hirschfelden".
func reserveKeyFromLocalizationKey(s string) string {
	if !strings.HasPrefix(s, "reserve_") {
		return ""
	}
	key := strings.TrimPrefix(s, "reserve_")
	key = strings.TrimSuffix(key, "_name_uppercase")
	key = strings.TrimSuffix(key, "_name")
	if key == "" || strings.Contains(key, "_id") {
		return ""
	}
	return key
}

// looksLikeSpeciesKey filters out non species values from a data book header.
func looksLikeSpeciesKey(s string) bool {
	if s == "" || len(s) > 40 || s != strings.ToLower(s) {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return strings.Contains(s, "_") || len(s) > 3
}
