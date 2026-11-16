package cotw

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"cotw-pop-viewer/adf"
)

// reserveIndexKeys maps the save file number (animal_population_N) to the game's
// reserve key.
//
// The order comes from the game's own data: the codex "Regions" sheet in
// game72.arc (offset 153419776) lists every reserve as a reserve_id in a fixed
// order — hirschfelden, layton_lake_district, siberia, africa, patagonia, yukon,
// iberia, rockies, nz, mexico, delta, finland, nh, australia, nepal, sh, alberta,
// scotland, peru — and the save numbers follow that order with the three trophy
// lodge worlds inserted at 5, 7 and 15 (see TrophyLodgeWorlds). Names, species and
// everything else come from the game files. Unlisted numbers (a reserve the player
// does not own, e.g. peru at 21 here) simply have no key; callers can extend the
// set through Options.ReserveIndexKeys.
var reserveIndexKeys = map[int]string{
	0:  "hirschfelden",
	1:  "layton_lake_district",
	2:  "siberia",
	3:  "africa",
	4:  "patagonia",
	6:  "yukon",
	8:  "iberia",
	9:  "rockies",
	10: "nz",
	11: "mexico",
	12: "delta",
	13: "finland",
	14: "nh",
	16: "australia",
	17: "nepal",
	18: "sh",
	19: "alberta",
	20: "scotland",
}

// TrophyLodgeWorlds names the save numbers that are not huntable reserves but the
// three trophy lodge worlds. They occupy the gaps at 5, 7 and 15, hold no animal
// populations and no need zones, and their names come from the game's own
// localization ("SPRING CREEK MANOR TROPHY LODGE", "SASEKA SAFARI TROPHY LODGE",
// "LAYTON LAKE TROPHY CABIN"); the game's WorldsListDefinition also defines exactly
// three non-reserve worlds named hp_trophyworld, hp_trophyworld_2 and
// hp_trophyworld_3. Reserves with these numbers deliberately have no reserve key.
var TrophyLodgeWorlds = map[int]string{
	5:  "Spring Creek Manor",
	7:  "Saseka Safari Lodge",
	15: "Layton Lakes Trophy Cabin",
}

// reserveKeyForIndex resolves a file number to a reserve key, honouring caller
// overrides.
func reserveKeyForIndex(index int, overrides map[int]string) string {
	if overrides != nil {
		if key, ok := overrides[index]; ok {
			return key
		}
	}
	return reserveIndexKeys[index]
}

// populationFiles lists the animal_population_N files in a save folder.
func populationFiles(dir string) (map[int]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[int]string, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "animal_population_") {
			continue
		}
		suffix := strings.TrimPrefix(name, "animal_population_")
		index, err := strconv.Atoi(suffix)
		if err != nil {
			continue
		}
		out[index] = filepath.Join(dir, name)
	}
	return out, nil
}

func sortedKeys(m map[int]string) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// parseReserve decodes one animal_population_N file. Species ids are left
// unresolved; ParseSave fills them in with one batch lookup.
func (g *Game) parseReserve(path string, index int, overrides map[int]string) *Reserve {
	r := &Reserve{
		Index:     index,
		File:      filepath.Base(path),
		Species:   []*SpeciesPopulation{},
		NeedZones: []*NeedZone{},
	}
	r.Key = reserveKeyForIndex(index, overrides)
	if r.Key != "" {
		r.Name = g.ReserveName(r.Key)
	}

	a, err := LoadADFFile(path)
	if err != nil {
		r.Err = err.Error()
		return r
	}
	root := a.InstanceOfTypePrefix("AnimalPopulationReserveData")
	if root == nil && len(a.Values) > 0 {
		root = a.Values[0]
	}
	if root == nil {
		r.Err = "no AnimalPopulationReserveData instance"
		return r
	}

	if f := root.Field("AlreadyConverted"); f != nil {
		r.AlreadyConverted = f.Bool()
	}
	if f := root.Field("ReserveSeed"); f != nil {
		r.ReserveSeed, _ = f.Uint32()
	}
	if f := root.Field("AtypicalsCooldownTime"); f != nil {
		if v, err := f.Uint64(); err == nil {
			r.AtypicalsCooldownTime = uint16(v)
		}
	}

	pops := root.Field("Populations")
	if pops == nil {
		r.Err = "no Populations array"
		return r
	}
	for i, pv := range pops.Elems {
		sp := decodeSpeciesPopulation(pv, i)
		if sp == nil {
			continue
		}
		r.Species = append(r.Species, sp)
		r.TotalAnimals += sp.AnimalCount
		r.TotalGroups += sp.GroupCount
	}
	return r
}

// decodeSpeciesPopulation reads one AnimalPopulationData entry.
func decodeSpeciesPopulation(v *adf.Value, index int) *SpeciesPopulation {
	if v == nil || v.Kind != adf.KindStruct {
		return nil
	}
	sp := &SpeciesPopulation{Index: index, Groups: []*Group{}}

	if f := v.Field("NameHashId"); f != nil {
		sp.NameHashID, _ = f.Uint32()
	}
	if f := v.Field("InitialTotalScore"); f != nil {
		sp.InitialTotalScore, _ = f.Float64()
	}
	if f := v.Field("InitialNumAnimals"); f != nil {
		if n, err := f.Int64(); err == nil {
			sp.InitialNumAnimals = int32(n)
		}
	}
	if f := v.Field("Revision"); f != nil {
		sp.Revision, _ = f.Uint32()
	}
	if f := v.Field("RepopulationSkipCounter"); f != nil {
		if n, err := f.Uint64(); err == nil {
			sp.RepopulationSkipCounter = uint16(n)
		}
	}

	if groups := v.Field("Groups"); groups != nil {
		for gi, gv := range groups.Elems {
			group := decodeGroup(gv, gi)
			if group == nil {
				continue
			}
			sp.Groups = append(sp.Groups, group)
			sp.AnimalCount += len(group.Animals)
			for _, animal := range group.Animals {
				switch animal.Gender {
				case GenderMale:
					sp.MaleCount++
				case GenderFemale:
					sp.FemaleCount++
				}
				if animal.Weight > sp.MaxWeight {
					sp.MaxWeight = animal.Weight
				}
				if animal.Score > sp.MaxScore {
					sp.MaxScore = animal.Score
				}
			}
		}
	}
	sp.GroupCount = len(sp.Groups)
	return sp
}

// decodeGroup reads one AnimalGroupData entry.
func decodeGroup(v *adf.Value, index int) *Group {
	if v == nil || v.Kind != adf.KindStruct {
		return nil
	}
	g := &Group{Index: index, NeedZoneGuids: []uint32{}, Animals: []*Animal{}}
	if f := v.Field("SpawnAreadId"); f != nil {
		g.SpawnAreaID, _ = f.Int32()
	}
	if f := v.Field("Id"); f != nil {
		g.ID, _ = f.Uint32()
	}
	if f := v.Field("NeedZonePathGuids"); f != nil {
		for _, e := range f.Elems {
			if id, err := e.Uint32(); err == nil {
				g.NeedZoneGuids = append(g.NeedZoneGuids, id)
			}
		}
	}
	if f := v.Field("Animals"); f != nil {
		for _, av := range f.Elems {
			if animal := decodeAnimal(av); animal != nil {
				g.Animals = append(g.Animals, animal)
			}
		}
	}
	return g
}

// decodeAnimal reads one AnimalData entry.
//
// Current saves store: Gender u8 @0, Weight f32 @4, Score f32 @8,
// FeatureModifiers @12 (a one byte Flags struct), IsScripted u8 @13,
// VisualVariationSeed u32 @16, Id u32 @20, MapPosition Vector2 @24. There is no
// Great One flag in this version.
func decodeAnimal(v *adf.Value) *Animal {
	if v == nil || v.Kind != adf.KindStruct {
		return nil
	}
	a := &Animal{Gender: GenderUnknown}
	if f := v.Field("Gender"); f != nil {
		if raw, err := f.Uint64(); err == nil {
			a.GenderRaw = uint8(raw)
		}
	}
	switch a.GenderRaw {
	case 1:
		a.Gender = GenderMale
	case 2:
		a.Gender = GenderFemale
	}
	if f := v.Field("Weight"); f != nil {
		a.Weight, _ = f.Float64()
	}
	if f := v.Field("Score"); f != nil {
		a.Score, _ = f.Float64()
	}
	if f := v.Field("IsScripted"); f != nil {
		a.IsScripted = f.Bool()
	}
	if f := v.Field("VisualVariationSeed"); f != nil {
		a.VisualVariationSeed, _ = f.Uint32()
	}
	if f := v.Field("Id"); f != nil {
		a.ID, _ = f.Uint32()
	}
	if fm := v.Field("FeatureModifiers"); fm != nil {
		if f := fm.Field("Flags"); f != nil {
			if raw, err := f.Uint64(); err == nil {
				a.FeatureModifiers = uint8(raw)
			}
		}
	}
	if mp := v.Field("MapPosition"); mp != nil {
		if f := mp.Field("X"); f != nil {
			a.MapX, _ = f.Float64()
		}
		if f := mp.Field("Y"); f != nil {
			a.MapY, _ = f.Float64()
		}
	}
	return a
}

// assignSpecies fills in species key, name and definition for the populations of
// a reserve.
func assignSpecies(reserves []*Reserve, defs map[uint32]PopDef, g *Game) {
	for _, r := range reserves {
		for _, sp := range r.Species {
			def, ok := defs[sp.NameHashID]
			if !ok || def.Species < 0 || def.Species >= len(g.species) {
				sp.Known = false
				continue
			}
			record := g.species[def.Species]
			sp.Known = true
			sp.SpeciesKey = record.Key
			sp.SpeciesName = record.Name
			sp.Definition = def.Name
		}
	}
}
