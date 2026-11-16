package cotw

import "time"

// Save is a parsed COTW save folder: every reserve's animal populations plus the
// discovered need zones.
type Save struct {
	// Dir is the save folder that was read.
	Dir string `json:"dir"`
	// SteamID is the account folder name (the numeric directory).
	SteamID string `json:"steam_id,omitempty"`
	// Game describes the lookup data that named everything below.
	Game GameInfo `json:"game"`
	// ParsedAt is when this structure was produced.
	ParsedAt time.Time `json:"parsed_at"`
	// Reserves is ordered by the file number (animal_population_N).
	Reserves []*Reserve `json:"reserves"`
	// Warnings holds per-file problems that did not abort the parse.
	Warnings []string `json:"warnings,omitempty"`
	// Trophies is the resolved per-species trophy table (maximum trophy rating
	// and whether only males carry the organ) that the animals' tier fields
	// were computed from. Keyed by species key.
	Trophies map[string]SpeciesTrophy `json:"trophies,omitempty"`

	game  *Game      `json:"-"`
	opts  Options    `json:"-"`
	hours *hourStore `json:"-"`
}

// Game returns the lookup data used to resolve names.
func (s *Save) GameData() *Game { return s.game }

// Reserve looks a reserve up by its file number.
func (s *Save) Reserve(index int) *Reserve {
	for _, r := range s.Reserves {
		if r.Index == index {
			return r
		}
	}
	return nil
}

// Reserve is one animal_population_N file.
type Reserve struct {
	Index int `json:"index"`
	// Key is the game's reserve key ("hirschfelden"), when known.
	Key  string `json:"key,omitempty"`
	Name string `json:"name,omitempty"`
	File string `json:"file"`

	AlreadyConverted      bool   `json:"already_converted"`
	ReserveSeed           uint32 `json:"reserve_seed"`
	AtypicalsCooldownTime uint16 `json:"atypicals_cooldown_time"`

	// Species is ordered as the file stores it (one entry per population).
	Species []*SpeciesPopulation `json:"species"`
	// NeedZones are the discovered need zones of this reserve.
	NeedZones []*NeedZone `json:"need_zones"`

	TotalAnimals int `json:"total_animals"`
	TotalGroups  int `json:"total_groups"`

	// Err is set when the file existed but could not be parsed.
	Err string `json:"error,omitempty"`
}

// SpeciesPopulation is one population entry: a species in a reserve.
type SpeciesPopulation struct {
	Index int `json:"index"`
	// SpeciesKey is the game's species key, e.g. "wild_boar".
	SpeciesKey string `json:"species_key,omitempty"`
	// SpeciesName is the localized display name, e.g. "WILD BOAR".
	SpeciesName string `json:"species_name,omitempty"`
	// Definition is the population definition the reserve uses, e.g.
	// "wild_boar_general". A species has several (plain, "general", regional).
	Definition string `json:"definition,omitempty"`
	// Known reports whether the id was resolved to a species.
	Known bool `json:"known"`

	NameHashID uint32 `json:"name_hash_id"`

	InitialTotalScore       float64 `json:"initial_total_score"`
	InitialNumAnimals       int32   `json:"initial_num_animals"`
	Revision                uint32  `json:"revision"`
	RepopulationSkipCounter uint16  `json:"repopulation_skip_counter"`

	Groups []*Group `json:"groups"`

	AnimalCount int     `json:"animal_count"`
	MaleCount   int     `json:"male_count"`
	FemaleCount int     `json:"female_count"`
	GroupCount  int     `json:"group_count"`
	MaxWeight   float64 `json:"max_weight"`
	MaxScore    float64 `json:"max_score"`
}

// Group is one spawn group (herd, pack, flock).
type Group struct {
	Index       int    `json:"index"`
	SpawnAreaID int32  `json:"spawn_area_id"`
	ID          uint32 `json:"id"`
	// NeedZoneGuids are the ids of the zones this group uses. They include
	// zones the player has not discovered yet.
	NeedZoneGuids []uint32  `json:"need_zone_guids"`
	Animals       []*Animal `json:"animals"`
	// NeedZones are the zones this group uses, ordered by id. The player has
	// discovered some of them (Discovered=true, straight from
	// found_need_zones_adf); the rest are filled in from the shipped geometry so
	// a group's zone list is complete either way.
	NeedZones []*NeedZone `json:"need_zones,omitempty"`
	// SpawnArea is the centre and extent of this group's spawn area, from the
	// geometry the game ships for SpawnAreaID (see zones.go). Nil when the id
	// is not in the shipped spawn bitmaps.
	SpawnArea *ZoneGeometry `json:"spawn_area,omitempty"`
}

// Animal is one animal in a group.
type Animal struct {
	// GenderRaw is the stored code: 1 = male, 2 = female.
	GenderRaw uint8   `json:"gender_raw"`
	Gender    string  `json:"gender"`
	Weight    float64 `json:"weight"`
	Score     float64 `json:"score"`
	// FeatureModifiers is the animalFeatureModifiers bitfield byte the game stores
	// per animal. 0 in all 41,734 animals of this save, and the game data does not
	// document what the bits mean (no enum member names in any typedef, no tuning
	// asset). Reported raw so it can be correlated if a set bit is ever seen.
	FeatureModifiers    uint8   `json:"feature_modifiers"`
	IsScripted          bool    `json:"is_scripted"`
	VisualVariationSeed uint32  `json:"visual_variation_seed"`
	ID                  uint32  `json:"id"`
	MapX                float64 `json:"map_x"`
	MapY                float64 `json:"map_y"`

	// Tier is the trophy medal this animal's Score rates, under the game's
	// rating rule (see trophy.go). TierUnknown when the species' maximum
	// trophy rating is not known.
	Tier TrophyTier `json:"tier"`
	// One bool per trophy tier, as requested: the tier fields spelled out so
	// callers can filter without comparing an enum.
	IsDiamond  bool `json:"is_diamond"`
	IsGold     bool `json:"is_gold"`
	IsSilver   bool `json:"is_silver"`
	IsBronze   bool `json:"is_bronze"`
	IsGreatOne bool `json:"is_great_one"`
	IsNone     bool `json:"is_none"`
}

// Vec3 is a position in reserve world coordinates (Y is elevation).
type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// GroupRef points at the population group that uses a need zone.
type GroupRef struct {
	SpeciesIndex int    `json:"species_index"`
	SpeciesKey   string `json:"species_key,omitempty"`
	GroupIndex   int    `json:"group_index"`
}

// NeedZone is a discovered need zone.
//
// The game only saves a zone once the player has found it, so a group can
// reference zone ids that have no entry here.
type NeedZone struct {
	ReserveIndex int `json:"reserve_index"`
	// ID is the stored id as signed; IDU is the same value unsigned, which is
	// what a group's NeedZoneGuids hold.
	ID  int32  `json:"id"`
	IDU uint32 `json:"id_u"`

	// Type is the stored activity id (1, 2 and 3 occur). The game does not
	// record which of feed/drink/rest it means; the hour window does.
	Type int32 `json:"type"`
	// Discovered is true when the player has found this zone (the save holds
	// it); false when the entry was filled in from the shipped geometry for a
	// zone only the group's NeedZoneGuids know about.
	Discovered bool    `json:"discovered"`
	Position   Vec3    `json:"position"`
	MapIconID  uint64  `json:"map_icon_id"`
	StartHour  float64 `json:"start_hour"`
	EndHour    float64 `json:"end_hour"`
	// Windows lists every window of this zone's type, the same way ZoneInfo
	// does; StartHour/EndHour are only set when there is exactly one.
	Windows []HourWindow `json:"windows,omitempty"`
	// HourSource records where the window came from (see HourSource* constants).
	HourSource string `json:"hour_source,omitempty"`

	// AnimalTypeHash is hash32("animal_<codex id>_name").
	AnimalTypeHash    uint32 `json:"animal_type_hash"`
	AnimalSpeciesKey  string `json:"animal_species_key,omitempty"`
	AnimalSpeciesName string `json:"animal_species_name,omitempty"`

	ScheduleIndex    int8     `json:"schedule_index"`
	DiscoveredGroups []uint32 `json:"discovered_groups"`

	// Groups are the population groups that reference this zone.
	Groups []GroupRef `json:"groups,omitempty"`
}

// Gender names.
const (
	GenderMale    = "male"
	GenderFemale  = "female"
	GenderUnknown = "unknown"
)
