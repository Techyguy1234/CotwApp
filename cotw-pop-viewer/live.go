//go:build windows

package cotw

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Live reading of the running game.
//
// Everything in this file is read only: the game's process is opened with
// PROCESS_VM_READ, nothing is written, and no code is injected or hooked.
//
// # Where the animal data lives
//
// The game keeps two unrelated structures for the same animals.
//
// 1. The population mirror: one fixed 32 byte record per animal, byte for byte
// the save's AnimalData struct.
//
//	+0  u8  gender              (1 male, 2 female)
//	+4  f32 weight
//	+8  f32 score
//	+12 u8  featureModifiers
//	+13 u8  isScripted
//	+16 u32 visualVariationSeed
//	+20 u32 id                  (always 0, exactly as in the save)
//	+24 f32 mapPosition.X       (always 0: the engine never fills this in)
//	+28 f32 mapPosition.Y
//
// That mirror is what the save file is written from. It is held in four to
// eight duplicate copies per animal and is byte identical to the save, so
// reading it adds nothing a save read does not already give. LiveStatus uses
// it to prove a world is really loaded.
//
// 2. The materialised animal entities: one per animal in the loaded reserve,
// carrying the animal's current world position. Positions reads this one.
//
//	sentinel-188 f32 X          \
//	sentinel-184 f32 Y           |  current world position (Y is elevation)
//	sentinel-180 f32 Z          /
//	sentinel-176 f32 -1000
//	sentinel-172 f32 rotationX
//	sentinel-168 f32 rotationY
//	sentinel-104 f32 weight
//	sentinel-100 f32 score
//	sentinel- 84 u32 visualVariationSeed
//	sentinel- 24 f32 1.0
//	sentinel+  0 u32 0x7F7FFFFF  \
//	sentinel+  4 u32 0xDEADBEEF  /  the marker that identifies the record
//
// The marker is what makes the entities findable. In a loaded reserve the
// eight byte sequence 0x7F7FFFFF, 0xDEADBEEF occurs exactly once per animal
// and nowhere else, so one heap scan for it returns precisely the reserve's
// animals with no false positives. The positions are live: over two minutes of
// real time roughly 2% of a reserve's animals moved, by 1 m up to 1.5 km,
// while the rest stood still at a need zone.
//
// The seed inside an entity is the same VisualVariationSeed the save stores,
// which is how each entity is tied to its species, gender and trophy maximum.
// Every animal in a loaded reserve has an entity, so every seed resolves.

// LiveGame is a read-only session on the running game.
//
// Creating a session opens the process handle; addresses the reader locates are
// cached for the life of the session, so repeated reads are cheap. Create a new
// session after the player leaves a reserve or the game restarts.
type LiveGame struct {
	game     *Game
	proc     *process
	procName string
	// opts are the save parsing options the session was opened with.
	opts Options
	// world caches the located world-state object for the current session.
	world *worldState
	// save caches the parsed save used to name the live animals, together
	// with the reserve it was parsed for.
	save        *Save
	saveReserve int
	// entityAddrs caches the sentinel addresses of the reserve's animal
	// entities, with the reserve they were found in. Re-reading a few
	// thousand addresses is milliseconds; a heap scan is a minute, so the
	// addresses are reused until they stop describing the reserve.
	entityAddrs   []uintptr
	entityReserve int
}

// OpenLive opens a read-only session on the running game. It returns
// ErrGameNotRunning when the game is not running.
func (g *Game) OpenLive() (*LiveGame, error) {
	return g.OpenLiveWith(Options{})
}

// OpenLiveWith is OpenLive with explicit save-parsing options, for callers that
// need Positions to resolve species against game data it was given rather than
// auto detection.
func (g *Game) OpenLiveWith(opts Options) (*LiveGame, error) {
	pid, name, ok := findGamePID()
	if !ok {
		return nil, ErrGameNotRunning
	}
	proc, err := openProcessHandle(pid)
	if err != nil {
		return nil, err
	}
	return &LiveGame{game: g, proc: proc, procName: name, opts: opts, saveReserve: -1}, nil
}

// worldState is the game's live copy of the save's world state: the
// ReserveWorldData object that says which reserve is loaded.
type worldState struct {
	addr    uintptr
	reserve int
}

// Status reports whether the game is running and which reserve it is on.
//
// The reserve is resolved in two steps. First the running game is asked: the
// reader looks for the ReserveWorldData object it keeps for the loaded world.
// If that cannot be located (the world state object is not itself typed by a
// name in memory), the status falls back to the save folder's
// reserveworlddata_adf, which the game rewrites when a reserve is entered, and
// reports Source "save" so callers can tell the two apart.
//
// The fallback is a guess: that file still names the last reserve the player
// entered while they sit at the main menu or in a trophy lodge, so a status
// with Source "save" and InReserve true is not proof that a reserve world is
// loaded. Positions does not have that weakness - it answers ErrNotInReserve
// when no animal entities exist, which is what actually happens outside a
// reserve.
func (l *LiveGame) Status() (LiveStatus, error) {
	st := LiveStatus{
		PID:          l.PID(),
		ProcessName:  l.procName,
		ReserveIndex: -1,
		ReadAt:       time.Now(),
	}
	if !l.Running() {
		return st, ErrGameNotRunning
	}
	st.Running = true

	if ws := l.worldState(); ws != nil {
		applyWorld(&st, ws.reserve)
		st.Source = "memory"
		l.nameReserve(&st)
		return st, nil
	}

	reserve, ok := l.reserveFromSave()
	if !ok {
		st.Source = "memory"
		return st, nil
	}
	applyWorld(&st, reserve)
	st.Source = "save"
	l.nameReserve(&st)
	return st, nil
}

// nameReserve fills ReserveName from the game data when the reserve key is
// known.
func (l *LiveGame) nameReserve(st *LiveStatus) {
	if l.game == nil || st.ReserveKey == "" {
		return
	}
	st.ReserveName = l.game.ReserveName(st.ReserveKey)
}

// worldState returns the cached world state, relocating it when the cached
// object has been freed.
func (l *LiveGame) worldState() *worldState {
	if l.world != nil {
		if l.stillValid(l.world.addr) {
			return l.world
		}
		l.world = nil
	}
	ws, _ := l.locateWorldState()
	l.world = ws
	return ws
}

// reserveFromSave reads the reserve number the game last wrote into
// reserveworlddata_adf.
func (l *LiveGame) reserveFromSave() (int, bool) {
	dirs := FindSaveDirs()
	if len(dirs) == 0 {
		return 0, false
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, "reserveworlddata_adf")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		a, err := LoadADFFile(path)
		if err != nil {
			continue
		}
		v := a.InstanceOfTypePrefix("ReserveWorldData")
		if v == nil {
			continue
		}
		f := v.Field("Reserve")
		if f == nil {
			continue
		}
		n, err := f.Int64()
		if err != nil || n < 0 || n > 64 {
			continue
		}
		return int(n), true
	}
	return 0, false
}

// reserveIndex reports which reserve is loaded, cheaply.
//
// A world state already located in memory is used when there is one; otherwise
// the answer comes from the reserveworlddata_adf the game writes when a reserve
// is entered, which is a file read rather than a heap scan. That file can still
// name the previous reserve at the main menu, so the answer is only a hint -
// Positions confirms it by requiring animal entities to exist.
func (l *LiveGame) reserveIndex() (int, bool) {
	if l.world != nil && l.stillValid(l.world.addr) {
		return l.world.reserve, true
	}
	return l.reserveFromSave()
}

// stillValid reports whether addr still holds a readable world-state object.
func (l *LiveGame) stillValid(addr uintptr) bool {
	buf := make([]byte, 4)
	return l.proc.readExact(addr, buf) == nil
}

// Running reports whether the game process still exists.
func (l *LiveGame) Running() bool { return l != nil && l.proc != nil && l.proc.alive() }

// PID returns the game's process id.
func (l *LiveGame) PID() int {
	if l == nil || l.proc == nil {
		return 0
	}
	return int(l.proc.pid)
}

// Close releases the process handle.
func (l *LiveGame) Close() {
	if l != nil && l.proc != nil {
		l.proc.close()
		l.proc = nil
	}
}

// Positions reads every animal in the loaded reserve, with its live world
// position, straight from the running game.
//
// The game materialises one entity per animal of the loaded reserve, each
// marked by the eight byte sequence 0x7F7FFFFF, 0xDEADBEEF. One heap scan for
// that marker returns exactly the reserve's animals - no false positives, and
// unlike a save read it is live: an animal that has been shot is gone, and the
// positions are where the animals actually are right now, updating as they
// travel between need zones.
//
// Species, gender and trophy tier come from the animal's VisualVariationSeed
// matched against the save, which is the only place the game records what a
// seed belongs to. Weight and score are read from the live entity, so they are
// current even when the save file is not.
//
// The result is empty (with a nil error) in a reserve that has no animals, and
// ErrNotInReserve when no reserve world is loaded.
//
// # Cost
//
// Finding the entities needs one scan of the game's heap, which takes a couple
// of minutes on a 10 GB process; the addresses are then cached on the session,
// so later calls re-read a few thousand addresses and return in milliseconds.
// Create a new session (OpenLive) after the player changes reserve.
func (l *LiveGame) Positions() ([]LiveAnimal, error) {
	if !l.Running() {
		return nil, ErrGameNotRunning
	}
	reserve, ok := l.reserveIndex()
	if !ok {
		return nil, ErrNotInReserve
	}
	save, err := l.saveForReserve(reserve)
	if err != nil {
		return nil, err
	}
	r := save.Reserve(reserve)
	if r == nil {
		return nil, fmt.Errorf("cotw: reserve %d is not in the save", reserve)
	}

	// The save is the only thing that says which species a seed belongs to.
	type owner struct {
		speciesKey  string
		speciesName string
		group       int
		gender      uint8
		genderName  string
		tier        SpeciesTrophy
	}
	index := make(map[uint32]owner)
	for _, sp := range r.Species {
		trophy := save.Trophies[sp.SpeciesKey]
		for _, g := range sp.Groups {
			for _, a := range g.Animals {
				index[a.VisualVariationSeed] = owner{
					speciesKey:  sp.SpeciesKey,
					speciesName: sp.SpeciesName,
					group:       g.Index,
					gender:      a.GenderRaw,
					genderName:  a.Gender,
					tier:        trophy,
				}
			}
		}
	}

	ents := l.entitiesFor(reserve)
	if len(ents) == 0 {
		// No materialised animal entities means no reserve world is actually
		// loaded - the main menu, or a trophy lodge. Status can be fooled
		// here (see its note on Source), the entities cannot.
		return nil, ErrNotInReserve
	}
	out := make([]LiveAnimal, 0, len(ents))
	for _, e := range ents {
		la := LiveAnimal{
			Weight:              float64(e.weight),
			Score:               float64(e.score),
			VisualVariationSeed: e.seed,
			Position:            e.position,
			PositionSet:         true,
		}
		if o, ok := index[e.seed]; ok {
			la.SpeciesKey = o.speciesKey
			la.SpeciesName = o.speciesName
			la.Group = o.group
			la.Gender = o.genderName
			la.Tier = o.tier.TierFor(float64(e.score), isFemale(o.gender))
			setLiveTierFlags(&la)
		} else {
			// An animal the save does not know: spawned since it was written.
			// Its position and stats are real, its species is not knowable
			// until the save catches up.
			la.Group = -1
			la.Tier = TierUnknown
		}
		out = append(out, la)
	}
	return out, nil
}

// isFemale reports whether a stored gender code is female.
func isFemale(code uint8) bool { return code == 2 }

// setLiveTierFlags fills the per-tier booleans from the tier.
func setLiveTierFlags(la *LiveAnimal) {
	la.IsDiamond = la.Tier == TierDiamond
	la.IsGold = la.Tier == TierGold
	la.IsSilver = la.Tier == TierSilver
	la.IsBronze = la.Tier == TierBronze
	la.IsGreatOne = la.Tier == TierGreatOne
	la.IsNone = la.Tier == TierNone
}

// saveForReserve returns the parsed save, caching it for the reserve it was
// parsed for. A save is a few seconds of work, so Positions reuses it until
// the player changes reserve.
func (l *LiveGame) saveForReserve(reserve int) (*Save, error) {
	if l.save != nil && l.saveReserve == reserve {
		return l.save, nil
	}
	save, err := ParseSave("", l.opts)
	if err != nil {
		return nil, err
	}
	l.save = save
	l.saveReserve = reserve
	return save, nil
}
