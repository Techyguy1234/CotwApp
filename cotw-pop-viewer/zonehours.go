package cotw

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"
)

// HourWindow is one daily window of a need zone, in game hours (0..24).
type HourWindow struct {
	Start float64 `json:"start_hour"`
	End   float64 `json:"end_hour"`
}

// Where a zone's window came from.
const (
	// HourSourceNone means the window is unknown: the species has never been
	// discovered in a save and no fallback covers it.
	HourSourceNone = ""
	// HourSourceSave means the zone is discovered and the game stored its window.
	HourSourceSave = "save"
	// HourSourceOptions means Options.ZoneHours supplied it.
	HourSourceOptions = "options"
	// HourSourceLearned means an earlier parse learned it from a save and it was
	// kept in the cache's schedule store.
	HourSourceLearned = "learned"
	// HourSourceWiki means the published community table supplied it.
	HourSourceWiki = "wiki"
)

// zoneHourTable is the published fallback timetable, keyed by species with
// optional per-reserve variants.
type zoneHourTable struct {
	defaults map[string][]ScheduleSlot
	variants map[string]map[string][]ScheduleSlot
}

// PublishedZoneHours returns the published timetable of a species, if the
// fallback table covers it. Index is the slot's rank when the slots are sorted
// by start hour, which is how the game numbers them (checked against 164
// discovered zones).
func PublishedZoneHours(speciesKey string) ([]ScheduleSlot, bool) {
	slots, ok := wikiZoneHours.defaults[speciesKey]
	return copySlots(slots), ok
}

// PublishedZoneHoursFor returns the published timetable for a species on a
// reserve, preferring the page-specific variant when one exists.
func PublishedZoneHoursFor(reserveKey, speciesKey string) ([]ScheduleSlot, bool) {
	if bySpecies, ok := wikiZoneHours.variants[reserveKey]; ok {
		if slots, ok := bySpecies[speciesKey]; ok {
			return copySlots(slots), true
		}
	}
	return PublishedZoneHours(speciesKey)
}

// PublishedZoneHoursSpecies lists every species the fallback table covers.
func PublishedZoneHoursSpecies() []string {
	out := make([]string, 0, len(wikiZoneHours.defaults))
	for k := range wikiZoneHours.defaults {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// parseZoneHourTable reads the compact table format:
//
//	species|type:start-end,...
//	@reserve|species|type:start-end,...
func parseZoneHourTable(data string) *zoneHourTable {
	t := &zoneHourTable{
		defaults: map[string][]ScheduleSlot{},
		variants: map[string]map[string][]ScheduleSlot{},
	}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "|")
		if strings.HasPrefix(line, "@") {
			if len(fields) != 3 {
				continue
			}
			reserve := strings.TrimPrefix(fields[0], "@")
			if t.variants[reserve] == nil {
				t.variants[reserve] = map[string][]ScheduleSlot{}
			}
			t.variants[reserve][fields[1]] = parseSlots(fields[2])
			continue
		}
		if len(fields) != 2 {
			continue
		}
		t.defaults[fields[0]] = parseSlots(fields[1])
	}
	for _, slots := range t.defaults {
		numberSlots(slots)
	}
	for _, bySpecies := range t.variants {
		for _, slots := range bySpecies {
			numberSlots(slots)
		}
	}
	return t
}

// parseSlots reads "1:4-8,3:11-14" into slots, ignoring any index.
func parseSlots(s string) []ScheduleSlot {
	out := []ScheduleSlot{}
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		colon := strings.IndexByte(item, ':')
		dash := strings.IndexByte(item, '-')
		if colon < 0 || dash < colon {
			continue
		}
		zoneType, err1 := strconv.Atoi(item[:colon])
		start, err2 := strconv.ParseFloat(item[colon+1:dash], 64)
		end, err3 := strconv.ParseFloat(item[dash+1:], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		out = append(out, ScheduleSlot{Index: -1, Type: int32(zoneType), Start: start, End: end})
	}
	return out
}

// numberSlots assigns each slot the index the game uses: its rank when the
// slots are ordered by start hour.
func numberSlots(slots []ScheduleSlot) {
	if len(slots) == 0 {
		return
	}
	order := make([]int, len(slots))
	for i := range order {
		order[i] = i
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && slots[order[j]].Start < slots[order[j-1]].Start; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	for rank, i := range order {
		slots[i].Index = int8(rank)
	}
}

// hourStore is what earlier parses learned about species timetables. It is kept
// next to the game cache but separate from it: gamedata.json is derived from the
// installed game, this is derived from save files.
type hourStore struct {
	Format   int                                  `json:"format"`
	Version  string                               `json:"game_version,omitempty"`
	Updated  string                               `json:"updated,omitempty"`
	Species  map[string][]ScheduleSlot            `json:"species,omitempty"`
	Reserves map[string]map[string][]ScheduleSlot `json:"reserves,omitempty"`
}

// hourStoreFormat is the on-disk format version of the schedule store.
const hourStoreFormat = 1

// hourStorePath is the schedule store that sits beside a cache path.
func hourStorePath(cachePath string) string {
	if cachePath == "" {
		cachePath = defaultCachePath()
	}
	return cachePath + ".hours"
}

// loadHourStore reads the schedule store, discarding it when it was written for
// a different game version (the timetable itself may have changed).
func loadHourStore(path, gameVersion string) (*hourStore, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return newHourStore(gameVersion), false
	}
	var st hourStore
	if err := json.Unmarshal(raw, &st); err != nil || st.Format != hourStoreFormat {
		return newHourStore(gameVersion), false
	}
	if st.Version != gameVersion {
		return newHourStore(gameVersion), true // stale: ignore, will be relearned
	}
	if st.Species == nil {
		st.Species = map[string][]ScheduleSlot{}
	}
	if st.Reserves == nil {
		st.Reserves = map[string]map[string][]ScheduleSlot{}
	}
	return &st, false
}

func newHourStore(gameVersion string) *hourStore {
	return &hourStore{
		Format:   hourStoreFormat,
		Version:  gameVersion,
		Species:  map[string][]ScheduleSlot{},
		Reserves: map[string]map[string][]ScheduleSlot{},
	}
}

// hours returns the stored timetable for a reserve/species pair, falling back
// to the species-wide timetable learned elsewhere.
func (h *hourStore) hours(reserveKey, speciesKey string) ([]ScheduleSlot, bool) {
	if h == nil {
		return nil, false
	}
	if bySpecies, ok := h.Reserves[reserveKey]; ok {
		if slots, ok := bySpecies[speciesKey]; ok && len(slots) > 0 {
			return slots, true
		}
	}
	slots, ok := h.Species[speciesKey]
	if !ok || len(slots) == 0 {
		return nil, false
	}
	// Trust the species-wide entry only while no reserve contradicts it: some
	// species keep different hours on different reserves.
	for _, bySpecies := range h.Reserves {
		if other, ok := bySpecies[speciesKey]; ok && !sameSlots(other, slots) {
			return nil, false
		}
	}
	return slots, true
}

// learn records what a save revealed about one reserve. It reports whether
// anything changed. The species-wide entry is left to the caller, which knows
// whether every reserve agreed.
func (h *hourStore) learn(reserveKey, speciesKey string, slots []ScheduleSlot) bool {
	if h == nil || len(slots) == 0 {
		return false
	}
	changed := false
	if h.Reserves[reserveKey] == nil {
		h.Reserves[reserveKey] = map[string][]ScheduleSlot{}
	}
	if !sameSlots(h.Reserves[reserveKey][speciesKey], slots) {
		h.Reserves[reserveKey][speciesKey] = copySlots(slots)
		changed = true
	}
	return changed
}

func (h *hourStore) save(path, gameVersion string) error {
	h.Format = hourStoreFormat
	h.Version = gameVersion
	h.Updated = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.MarshalIndent(h, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// dirOf is filepath.Dir without the import cycle of spelling it out everywhere.
func dirOf(path string) string {
	if i := strings.LastIndexAny(path, "/\\"); i > 0 {
		return path[:i]
	}
	return "."
}

func sameSlots(a, b []ScheduleSlot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || a[i].Index != b[i].Index ||
			a[i].Start != b[i].Start || a[i].End != b[i].End {
			return false
		}
	}
	return true
}

func copySlots(in []ScheduleSlot) []ScheduleSlot {
	if len(in) == 0 {
		return nil
	}
	out := make([]ScheduleSlot, len(in))
	copy(out, in)
	return out
}

// sortStrings sorts in place (kept local so this file needs no sort import).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// wikiZoneHours is the published fallback timetable.
var wikiZoneHours = parseZoneHourTable(wikiZoneHoursData)

// wikiZoneHoursData is the transcribed community table. It is consulted only
// when neither this save nor an earlier one has revealed a species' timetable.
const wikiZoneHoursData = `
# theHunter: Call of the Wild need-zone times.
# Transcribed from https://thehuntercotw.wordpress.com/home/zone-times/
# (15 reserve pages, fetched 2026-11-13). These are community values, not
# game files: the game does not ship a timetable (see README). They are the
# fallback for zones whose species has never been discovered in a save.
#
#   species|type:start-end,...            default timetable
#   @reserve|species|type:start-end,...   page-specific variant
#
# type: 1 = feeding, 2 = drinking, 3 = resting. An end of 24 means midnight.

# --- defaults ---
american_alligator|1:0-4,3:4-8,3:8-12,3:12-16,3:16-20,1:20-24
antelope_jackrabbit|1:0-4,1:4-7,3:7-11,3:11-14,3:14-17,1:17-20,1:20-24
axis_deer|3:0-3,2:3-6,1:6-9,1:9-12,3:12-15,3:15-18,3:18-21,1:21-24
banteng|3:0-4,1:4-7,1:7-10,3:10-14,1:14-17,2:17-20,3:20-24
barasingha|3:0-3,1:3-6,1:6-9,3:9-12,2:12-15,1:15-18,3:18-21,3:21-24
beceite_ibex|1:0-4,3:4-7,3:7-10,2:10-14,3:14-17,1:17-21,3:21-24
bengal_tiger|3:0-4,2:4-7,1:7-10,3:10-14,1:14-17,3:17-20,1:20-24
bighorn_sheep|3:0-4,1:4-8,3:8-11,2:11-14,1:14-18,1:18-21,3:21-24
black_bear|1:0-4,2:4-8,3:8-12,3:12-16,3:16-20,1:20-24
black_grouse|3:0-4,1:4-8,1:8-11,3:11-14,1:14-18,1:18-21,3:21-24
blackbuck|3:0-3,3:3-6,1:6-9,1:9-12,3:12-15,1:15-16,2:18-21,3:21-24
blacktail_deer|3:0-4,1:4-8,3:8-12,1:12-16,2:16-20,3:20-24
blue_sheep|3:0-4,1:4-7,1:7-11,3:11-14,2:14-17,1:17-20,3:20-24
blue_wildebeest|3:0-3,1:3-6,2:6-9,1:9-12,3:12-15,1:15-18,3:18-21,3:21-24
bobcat|1:0-3,2:3-6,1:6-9,3:9-12,3:12-16,3:16-20,1:20-24
canada_goose|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
cape_buffalo|3:0-3,3:3-6,1:6-9,2:9-12,1:12-15,1:15-18,3:18-21,3:21-24
caribou|3:0-4,2:4-8,1:8-12,3:12-16,1:16-20,3:20-24
chamois|3:0-4,1:4-8,1:8-12,3:12-16,3:16-20,1:20-24
cinnamon_teal|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
collared_peccary|2:0-3,3:3-7,1:7-11,3:11-14,1:14-17,3:17-20,1:20-24
coyote|2:0-4,1:4-7,3:7-10,3:10-14,3:14-17,1:17-20,1:20-24
eastern_cottontail_rabbit|3:0-4,1:4-7,1:7-10,1:10-13,3:13-17,1:17-21,3:21-24
eastern_grey_kangaroo|2:0-3,1:3-6,3:6-9,3:9-12,3:12-15,3:15-18,1:18-21,1:21-24
eastern_wild_turkey|3:0-4,1:4-8,1:8-11,3:11-14,1:14-17,1:17-20,3:20-24
eu_bison|3:0-4,1:4-7,1:7-10,2:10-14,3:14-17,1:17-20,3:20-24
eu_hare|1:0-3,1:3-6,3:6-10,3:10-14,3:14-18,1:18-21,1:21-24
eu_rabbit|1:5-9,1:12-16,1:19-23
eurasian_brown_bear|1:0-4,2:4-8,3:8-12,3:12-16,3:16-20,1:20-24
eurasian_lynx|1:0-3,2:3-6,1:6-9,3:9-12,3:12-16,3:16-20,1:20-24
eurasian_teal|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
eurasian_wigeon|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
fallow_deer|3:0-3,3:3-6,1:6-10,2:10-13,3:13-17,1:17-21,3:21-24
feral_goat|3:0-3,3:3-6,1:6-9,1:9-12,2:12-15,3:15-18,1:18-21,3:21-24
feral_pig|2:0-3,3:3-6,1:6-10,3:10-14,1:14-17,1:17-20,3:20-24
gemsbok|1:0-3,2:3-6,1:6-10,3:10-13,3:13-16,3:16-20,1:20-24
goldeneye|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
gray_fox|1:0-4,1:4-7,3:7-10,3:10-13,3:13-17,2:17-20,1:20-24
gray_wolf|1:0-3,1:3-6,3:6-10,3:10-14,3:14-17,2:17-20,1:20-24
gredos_ibex|1:0-4,3:4-7,3:7-10,2:10-14,3:14-17,1:17-21,3:21-24
green_wing_teal|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
greylag_goose|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
grizzly_bear|1:0-4,2:4-8,3:8-12,3:12-16,3:16-20,1:20-24
harlequin_duck|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
hazel_grouse|3:0-4,1:4-8,1:8-11,3:11-14,1:14-18,1:18-21,3:21-24
hog_deer|3:0-4,1:4-7,3:7-10,3:10-13,2:13-17,1:17-21,3:21-24
iberian_mouflon|1:0-3,3:3-7,3:7-11,3:11-15,1:15-18,2:18-21,1:21-24
iberian_wolf|1:0-3,2:3-6,3:6-10,3:10-14,3:14-18,1:18-21,1:21-24
jackrabbit|1:0-3,1:3-6,3:6-10,3:10-14,3:14-18,1:18-21,1:21-24
javan_rusa|1:0-3,3:3-6,1:6-9,3:9-13,3:13-17,1:17-20,2:20-24
lesser_kudu|1:0-3,1:3-7,3:7-10,3:10-14,3:14-18,2:18-21,1:21-24
lion|1:0-4,1:4-8,3:8-12,2:12-15,3:15-18,3:18-21,3:21-24
magpie_goose|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
mallard|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
mexican_bobcat|1:0-3,2:3-6,1:6-9,3:9-12,3:12-16,3:16-20,1:20-24
moose|3:0-4,1:4-8,3:8-12,2:12-16,1:16-20,3:20-24
mountain_goat|3:0-4,1:4-7,1:7-10,1:10-13,3:13-17,1:17-20,3:20-24
mountain_hare|3:0-4,1:4-7,1:7-10,1:10-13,3:13-17,1:17-21,3:21-24
mule_deer|1:0-3,1:3-6,3:6-9,3:9-12,3:12-15,2:15-18,3:18-21,1:21-24
nilgai|3:0-4,1:4-8,2:8-12,3:12-16,1:16-20,3:20-24
northern_bobwhite_quail|3:0-4,1:4-7,1:7-10,3:10-13,1:13-16,1:16-20,3:20-24
northern_red_muntjac|3:0-3,1:3-6,1:6-9,1:9-12,3:12-15,2:15-18,3:18-21,3:21-24
pheasant|3:0-4,1:4-8,1:8-11,3:11-14,1:14-18,1:18-21,3:21-24
plains_bison|3:0-4,1:4-8,2:8-12,3:12-16,1:16-20,3:20-24
prong_horn|3:0-3,3:3-6,1:6-9,1:9-12,3:12-15,1:15-18,2:18-21,3:21-24
puma|1:0-3,1:3-6,3:6-9,3:9-12,3:12-15,1:15-18,3:18-21,2:21-24
raccoon|2:0-3,1:3-6,3:6-10,1:10-13,3:13-16,3:16-20,1:20-24
raccoon_dog|2:0-3,1:3-6,3:6-10,1:10-13,3:13-16,3:16-20,1:20-24
red_deer|3:0-3,3:3-6,2:6-10,3:10-13,1:13-17,1:17-21,3:21-24
red_fox|1:0-4,1:4-7,3:7-10,3:10-13,3:13-17,2:17-20,1:20-24
reindeer|3:0-4,2:4-8,1:8-12,3:12-16,1:16-20,3:20-24
rio_grande_turkey|3:0-4,1:4-7,1:7-11,3:11-14,1:14-17,1:17-21,3:21-24
rock_ptarmigan|3:0-4,1:4-8,1:8-11,3:11-14,1:14-18,1:18-21,3:21-24
rockymountain_elk|3:0-4,2:4-8,1:8-12,3:12-16,1:16-20,3:20-24
roe_deer|3:0-4,1:4-7,1:7-10,3:10-14,2:14-17,1:17-20,3:20-24
ronda_ibex|1:0-4,3:4-7,3:7-10,2:10-14,3:14-17,1:17-21,3:21-24
roosevelt_elk|3:0-4,2:4-8,1:8-12,3:12-16,1:16-20,3:20-24
saltwater_crocodile|1:0-4,3:4-8,3:8-12,3:12-16,3:16-20,1:20-24
sambar|1:0-3,3:3-6,1:6-9,3:9-13,3:13-17,2:17-20,1:20-24
scrub_hare|1:0-3,1:3-6,3:6-10,3:10-14,3:14-18,1:18-21,1:21-24
siberian_musk_deer|3:0-4,3:4-8,2:8-12,1:12-16,1:16-20,3:20-24
sidestriped_jackal|1:0-3,1:3-6,3:6-10,3:10-14,3:14-18,1:18-21,1:21-24
sika_deer|3:0-3,3:3-6,1:6-10,2:10-13,3:13-17,1:17-21,3:21-24
snow_leopard|3:0-3,1:3-6,3:6-9,1:9-12,1:12-15,3:15-18,1:18-21,3:21-24
southeastern_ibex|1:0-4,3:4-7,3:7-11,2:10-14,3:14-17,1:17-21,3:21-24
springbok|3:0-4,1:4-8,1:8-11,1:11-14,3:14-18,1:18-21,3:21-24
stubble_quail|3:0-4,1:4-7,1:7-10,3:10-13,1:13-16,1:16-20,3:20-24
tahr|3:0-4,1:4-7,1:7-11,3:11-14,1:14-17,1:17-20,3:20-24
tibetan_fox|1:0-3,3:3-6,1:6-9,3:9-13,3:13-17,2:17-20,1:20-24
tufted_duck|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
tundra_bean_goose|3:0-5,1:5-7,3:7-9,1:9-11,3:11-13,1:13-15,3:15-17,1:17-19,3:19-24
warthog|3:0-3,3:3-6,1:6-9,1:9-12,1:12-15,2:15-18,3:18-21,3:21-24
water_buffalo|3:0-3,3:3-6,1:6-9,1:9-12,2:12-15,1:15-18,3:18-21,3:21-24
western_capercaillie|3:0-4,1:4-8,1:8-11,3:11-14,1:14-18,1:18-21,3:21-24
whitetail_deer|3:0-4,1:4-8,2:8-12,3:12-16,1:16-20,3:20-24
wild_boar|2:0-3,1:3-6,3:6-10,3:10-14,3:14-17,1:17-21,1:21-24
wild_turkey|3:0-4,1:4-8,1:8-11,3:11-14,1:14-17,1:17-20,3:20-24
wild_yak|3:0-4,1:4-8,2:8-12,3:12-16,1:16-20,3:20-24
willow_ptarmigan|3:0-4,1:4-8,1:8-11,3:11-14,1:14-18,1:18-21,3:21-24
woolly_hare|1:0-4,1:4-7,3:7-11,3:11-14,3:14-17,1:17-20,1:20-24

# --- reserve-specific variants (only where a page differs) ---
@rockies|bighorn_sheep|3:0-3,1:3-6,1:6-9,3:9-12,2:12-16,1:16-20,3:20-24
@mexico|coyote|1:0-4,1:4-7,3:7-11,3:11-14,3:14-17,1:17-21,2:21-24
@nh|coyote|2:0-3,1:3-7,3:7-10,3:10-14,3:14-17,1:17-20,1:20-24
@nz|feral_goat|3:0-3,3:3-6,1:6-9,1:9-12,3:12-15,2:15-18,1:18-21,3:21-24
@nz|feral_pig|3:0-3,2:3-6,1:6-10,3:10-14,1:14-17,1:17-20,3:20-24
@yukon|gray_wolf|2:0-3,1:3-6,3:6-10,3:10-14,3:14-18,1:18-21,1:21-24
@mexico|mule_deer|1:0-4,1:4-7,3:7-11,3:11-14,2:14-17,3:17-20,1:20-24
@nh|raccoon|1:0-3,2:3-6,3:6-10,1:10-13,3:13-16,3:16-20,1:20-24
@delta|whitetail_deer|3:0-4,1:4-8,2:8-11,3:11-14,1:14-18,1:18-21,3:21-24
@mexico|whitetail_deer|3:0-4,1:4-8,2:8-11,3:11-14,1:14-18,1:18-21,3:21-24
`
