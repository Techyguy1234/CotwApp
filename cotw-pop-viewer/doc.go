// Package cotw reads theHunter: Call of the Wild animal population saves and
// resolves everything in them against the installed game's own data files.
//
// Typical use:
//
//	save, err := cotw.ParseSave("", cotw.Options{})
//	if err != nil {
//		return err
//	}
//	for _, reserve := range save.Reserves {
//		fmt.Println(reserve.Name, reserve.TotalAnimals)
//		for _, sp := range reserve.Species {
//			fmt.Println("  ", sp.SpeciesName, sp.AnimalCount, "animals in", sp.GroupCount, "groups")
//		}
//		for _, zone := range reserve.NeedZones {
//			fmt.Println("  zone", zone.Type, zone.Position, zone.StartHour, zone.EndHour)
//		}
//	}
//
// ParseSave reads only. It never writes to a save file.
//
// # Live game
//
// The same game data also describes a running game. OpenLive opens a read-only
// handle on theHunterCotW_F.exe (no writes, no injection) and Status reports
// whether the game is running and which reserve the player is on:
//
//	live, err := game.OpenLive()
//	if errors.Is(err, cotw.ErrGameNotRunning) {
//		return err
//	}
//	defer live.Close()
//	st, _ := live.Status()
//	if st.InReserve {
//		fmt.Println("playing", st.ReserveName)
//	}
//
// Reading animals live is Positions:
//
//	animals, err := live.Positions()
//	if errors.Is(err, cotw.ErrNotInReserve) {
//		return err
//	}
//	for _, a := range animals {
//		fmt.Println(a.SpeciesName, a.Gender, a.Tier, a.Position, a.Score)
//	}
//
// Positions returns every animal of the loaded reserve with its current world
// position, weight, score and trophy tier, read from the game's materialised
// animal entities. The positions are live, moving as the animals travel
// between need zones, and species, gender and tier come from each animal's
// visual variation seed matched against the save.
//
// # Need zone timetables
//
// A group's need zone path is ordered by the schedule slot the group is in:
// Group.NeedZoneGuids[i] is the zone the group is at during slot i of its
// species' daily timetable, and Group.Slots spells that out with the window of
// each slot. A zone is therefore used at one slot, not at every window of its
// activity type - three resting zones of one herd belong to three different
// rest periods, which is what the community zone-time pages describe.
//
// The windows themselves are not shipped, so they come from the best source
// that covers each slot in turn: Options.ZoneHours, this save's discoveries,
// an earlier save's, then the published fallback table. Save.Timetable exposes
// the merged result and GroupSlot.HourSource names the source per slot.
//
// # Save files
//
// One save folder holds animal_population_0..N (one per reserve), and usually
// found_need_zones_adf with the need zones the player has discovered. Each file
// is a 32 byte "SAVE"/"COMP" header around a zlib stream whose first five bytes
// are a compression descriptor; the rest is an ADF body (" FDA").
//
// # Game data
//
// Names are not stored in the save, only hashes of them. OpenGame extracts the
// lookup data from the game install (the archives_win64 folder next to the
// executable):
//
//   - StringLookup assets give hash -> localized text for every language; this
//     names need zone species and reserves.
//   - XLS data books give the species keys (one sheet per species) and the
//     wildlife codex ids, which are what the localization keys are built from.
//   - Population definitions such as "wild_boar_general" or "moose_yukon" are
//     not stored as strings anywhere, so they are recovered by hashing candidate
//     names built from the species keys and the words the game itself uses.
//
// The extracted result is cached in the user cache folder. The installed game
// version is checked on every OpenGame call, and the cache is rebuilt when it
// changed.
package cotw
