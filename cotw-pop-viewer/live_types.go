package cotw

import (
	"errors"
	"time"
)

// Types shared by the live readers on every platform.
//
// The live reader itself is Windows only (see live.go), but its public API is
// declared here so an application that compiles on several platforms still
// gets the same names, and the offline stub in live_unsupported.go can answer
// them with ErrLiveUnsupported.

// ErrGameNotRunning is returned when the game process cannot be found.
var ErrGameNotRunning = errors.New("cotw: game is not running")

// ErrNotInReserve is returned when the game is running but no reserve world is
// loaded (main menu, reserve select, trophy lodge).
var ErrNotInReserve = errors.New("cotw: no reserve is loaded, so there are no animals to read")

// LiveStatus describes what the running game is showing.
type LiveStatus struct {
	// Running reports whether the game process exists.
	Running bool `json:"running"`
	// PID and ProcessName identify the running executable.
	PID         int    `json:"pid,omitempty"`
	ProcessName string `json:"process_name,omitempty"`
	// InReserve is true when the player is in a reserve world rather than at
	// the main menu or in a trophy lodge.
	InReserve bool `json:"in_reserve"`
	// ReserveIndex is the save file number (animal_population_N) of the loaded
	// reserve, or -1 when not in a reserve.
	ReserveIndex int `json:"reserve_index"`
	// ReserveKey and ReserveName name that reserve, when known.
	ReserveKey  string `json:"reserve_key,omitempty"`
	ReserveName string `json:"reserve_name,omitempty"`
	// TrophyLodge is set when the player is in one of the trophy lodge worlds.
	TrophyLodge string `json:"trophy_lodge,omitempty"`
	// ReadAt is when the status was read.
	ReadAt time.Time `json:"read_at"`
	// Source says how the reserve was determined: "memory" when it came from
	// the running game, "save" when it came from the save folder's
	// reserveworlddata (written when a reserve is entered).
	Source string `json:"source"`
}

// LiveAnimal is one animal as the running game currently holds it.
type LiveAnimal struct {
	// SpeciesKey and SpeciesName name the species, when the population entry
	// could be resolved against the game data.
	SpeciesKey  string `json:"species_key,omitempty"`
	SpeciesName string `json:"species_name,omitempty"`
	// Group is the spawn group the animal belongs to.
	Group int `json:"group"`
	// Gender is "male" or "female".
	Gender string `json:"gender"`
	// Weight in kilograms and Score as the game stores it.
	Weight float64 `json:"weight"`
	Score  float64 `json:"score"`
	// VisualVariationSeed is the per-animal random seed that decides the
	// animal's fur variant. It is stable for the life of the animal and is the
	// only unique identifier a save carries.
	VisualVariationSeed uint32 `json:"visual_variation_seed"`
	// Tier and the per-tier flags rate the animal from Score (see trophy.go).
	Tier       TrophyTier `json:"tier"`
	IsDiamond  bool       `json:"is_diamond"`
	IsGold     bool       `json:"is_gold"`
	IsSilver   bool       `json:"is_silver"`
	IsBronze   bool       `json:"is_bronze"`
	IsGreatOne bool       `json:"is_great_one"`
	IsNone     bool       `json:"is_none"`
	// Position is the animal's live world position, read from the game's
	// materialised entity for it. Positions fills this; Status and the
	// population mirror do not.
	Position Vec3 `json:"position"`
	// PositionSet reports whether Position was read from a live entity.
	PositionSet bool `json:"position_known"`
}

// applyWorld fills the reserve fields of a status from a reserve number.
func applyWorld(st *LiveStatus, reserve int) {
	if reserve < 0 {
		return
	}
	st.InReserve = true
	st.ReserveIndex = reserve
	if lodge, ok := TrophyLodgeWorlds[reserve]; ok {
		st.TrophyLodge = lodge
		return
	}
	st.ReserveKey = reserveKeyForIndex(reserve, nil)
}
