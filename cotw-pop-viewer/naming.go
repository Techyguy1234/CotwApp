package cotw

import (
	"strings"

	"cotw-pop-viewer/adf"
)

// SpeciesRecord is one animal species as the game knows it.
type SpeciesRecord struct {
	// Key is the game's internal species key, e.g. "wild_boar".
	Key string `json:"key"`
	// CodexID is the wildlife-codex id without its "animal_" prefix, e.g.
	// "wildboar". It is what the localization keys are built from.
	CodexID string `json:"codex_id,omitempty"`
	// Name is the localized display name, e.g. "Wild Boar".
	Name string `json:"name,omitempty"`
}

// PopDef is one population definition: the data asset a reserve's population
// entry refers to. A species normally has several (a plain one, a "general" one
// and sometimes regional ones such as "moose_yukon").
type PopDef struct {
	// Name is the definition name without the "_population" suffix, so
	// "wild_boar_general" or "moose_yukon".
	Name string `json:"name"`
	// Species is the index into Game.Species.
	Species int `json:"species"`
}

// regionQualifiers are leading words a species key may carry while the game's
// codex name omits them ("siberian_musk_deer" -> "muskdeer",
// "eurasian_brown_bear" -> "brownbear").
var regionQualifiers = map[string]bool{
	"siberian": true, "eurasian": true, "american": true, "european": true,
	"eastern": true, "western": true, "northern": true, "southern": true,
	"rockymountain": true, "ii": true,
}

// speciesNameCandidates lists the spellings the game may use for a species in
// localization keys ("animal_<spelling>_name"). It tries the key itself, its
// compact form, and the key with a leading region qualifier dropped or expanded
// ("eu_rabbit" -> "european_rabbit").
func speciesNameCandidates(key string) []string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return nil
	}
	set := map[string]bool{key: true, strings.ReplaceAll(key, "_", ""): true}
	words := strings.Split(key, "_")
	if len(words) > 1 {
		first, rest := words[0], words[1:]
		if first == "eu" || first == "eur" {
			expanded := append([]string{"european"}, rest...)
			set[strings.Join(expanded, "_")] = true
			set[strings.Join(expanded, "")] = true
		}
		if regionQualifiers[first] {
			set[strings.Join(rest, "_")] = true
			set[strings.Join(rest, "")] = true
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		if len(s) > 1 {
			out = append(out, s)
		}
	}
	return out
}

// populationSuffix is what every definition name ends with.
const populationSuffix = "_population"

// genericTags are the extra words a definition name can carry between the
// species and the suffix. "general" is what most older reserves use; the rest
// are regional definition names observed in the wild.
var genericTags = []string{"general", "default", "base", "shared", "common"}

// suffixWords are trailing words a species key may drop in a legacy definition
// name, e.g. "blacktail_deer" -> "blacktail".
var suffixWords = map[string]bool{
	"deer": true, "goose": true, "duck": true, "bear": true, "elk": true,
	"fox": true, "hare": true, "bison": true, "goat": true, "sheep": true,
}

// speciesStems lists the species-name shapes that appear at the front of a
// population definition name: the full key, its compact form, its last word,
// its last two words, and the key with a generic trailing word dropped.
func speciesStems(key string) []string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return nil
	}
	words := strings.FieldsFunc(key, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	if len(words) == 0 {
		return nil
	}
	set := map[string]bool{key: true, strings.Join(words, ""): true}
	if len(words) > 1 {
		set[words[len(words)-1]] = true
		set[strings.Join(words[len(words)-2:], "_")] = true
		if suffixWords[words[len(words)-1]] {
			set[strings.Join(words[:len(words)-1], "_")] = true
			set[strings.Join(words[:len(words)-1], "")] = true
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		if len(s) > 1 {
			out = append(out, s)
		}
	}
	return out
}

// cheapPopDefs builds the definitions that do not need a word list: every
// species stem on its own, plus the generic tags ("_general_population" etc).
func cheapPopDefs(species []SpeciesRecord) map[uint32]PopDef {
	out := make(map[uint32]PopDef, len(species)*8)
	for i, sp := range species {
		for _, stem := range speciesStems(sp.Key) {
			name := stem + populationSuffix
			out[adf.Hash32String(name)] = PopDef{Name: stem, Species: i}
			for _, tag := range genericTags {
				tagged := stem + "_" + tag
				out[adf.Hash32String(tagged+populationSuffix)] = PopDef{Name: tagged, Species: i}
			}
		}
	}
	return out
}

// deepDefinitionSearch looks for regional definition names
// ("<species>_<token>_population") among the population ids it is given.
//
// Definitions are not stored anywhere as strings, so the only way to name them
// is to try the words the game itself uses. Callers pass the handful of ids they
// could not resolve; the scan is over the species keys crossed with the game's
// word list (a couple of million hashes, about a second).
func deepDefinitionSearch(want map[uint32]bool, species []SpeciesRecord, tokens []string) map[uint32]PopDef {
	out := make(map[uint32]PopDef)
	if len(want) == 0 || len(tokens) == 0 {
		return out
	}
	remaining := len(want)
	for i, sp := range species {
		if remaining == 0 {
			break
		}
		key := strings.ToLower(sp.Key)
		if key == "" {
			continue
		}
		for _, token := range tokens {
			name := key + "_" + token
			h := adf.Hash32String(name + populationSuffix)
			if !want[h] {
				continue
			}
			if _, seen := out[h]; seen {
				continue
			}
			out[h] = PopDef{Name: name, Species: i}
			remaining--
			if remaining == 0 {
				break
			}
		}
	}
	return out
}

// localizationTokens extracts the candidate words used for the deep definition
// search from the game's localized strings: lowercase words, 3..20 characters.
func localizationTokens(stringsByHash map[uint32]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 4096)
	for _, text := range stringsByHash {
		for _, word := range splitWords(text) {
			w := strings.ToLower(word)
			if len(w) < 3 || len(w) > 20 || seen[w] {
				continue
			}
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

// splitWords splits on anything that is not a letter, digit or underscore.
func splitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			return false
		}
		return true
	})
}
