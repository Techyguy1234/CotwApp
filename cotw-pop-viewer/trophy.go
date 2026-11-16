package cotw

// Trophy tiers and the rating rule.
//
// The game rates a harvested animal's trophy (antlers, horns, tusks, skull or
// weight) as a "Trophy Rating" and turns that rating into a medal. The rule is
// documented by the game and confirmed against this save's own harvest history:
//
//	Trophy Rating < 20%  of the species maximum -> BRONZE
//	20%..60%             of the species maximum -> SILVER
//	60%..90%             of the species maximum -> GOLD
//	>= 90%               of the species maximum -> DIAMOND
//	a rating beyond the normal species maximum  -> GREAT ONE
//	no trophy organ (a female of an antlered species) -> NONE
//
// So a tier is a band of the *species' maximum trophy rating* - not of the
// animal's weight, and not of the population's current best score. All three
// fractions are used exactly as shipped by the game's rating code path; the
// per-species maxima are the one input the game does not ship as data (they are
// computed from each species' trophy model inside the executable), so they are
// supplied here from documented community values and may be overridden per
// species through Options.TrophyMaximums.
const (
	trophyBronzeMin = 0.20
	trophySilverMin = 0.60
	trophyGoldMin   = 0.90
)

// TrophyTier is an animal's medal class.
//
// The numeric order matches the value the game stores per harvest
// (hunting_log_adf TrophyScore), so a tier read from a save and a tier computed
// here compare directly.
type TrophyTier uint8

const (
	// TierUnknown means the species' maximum trophy rating is not known, so no
	// medal can be computed.
	TierUnknown TrophyTier = 255
	// TierDiamond, TierGold, TierSilver and TierBronze are the medal classes,
	// best first - matching the game's stored enum.
	TierDiamond TrophyTier = 0
	TierGold    TrophyTier = 1
	TierSilver  TrophyTier = 2
	TierBronze  TrophyTier = 3
	// TierNone is an animal with no trophy organ (for example a female of an
	// antlered species). The game stores 4 for these.
	TierNone TrophyTier = 4
	// TierGreatOne is a rating beyond the species' normal maximum. The game
	// stores 5 or higher; 5 is used here.
	TierGreatOne TrophyTier = 5
)

// String returns the tier name as the game displays it.
func (t TrophyTier) String() string {
	switch t {
	case TierDiamond:
		return "DIAMOND"
	case TierGold:
		return "GOLD"
	case TierSilver:
		return "SILVER"
	case TierBronze:
		return "BRONZE"
	case TierNone:
		return "NONE"
	case TierGreatOne:
		return "GREAT ONE"
	}
	return "UNKNOWN"
}

// SpeciesTrophy is how a species' trophy rating works.
type SpeciesTrophy struct {
	// Maximum is the species' maximum trophy rating - the denominator of the
	// rating rule. Zero means unknown, in which case no tier is computed.
	Maximum float64 `json:"maximum,omitempty"`
	// MaleOnly is true when only males carry the trophy organ, so a female is
	// rated NONE regardless of its score. Read from the game's own species
	// sheets (their "Trophy type" column is "male" for these species, "both"
	// otherwise).
	MaleOnly bool `json:"male_only,omitempty"`
	// Source records where Maximum came from: "option" (caller supplied),
	// "default" (built-in table), or "harvest-log" (derived from this save's
	// own harvest verdicts).
	Source string `json:"source,omitempty"`
}

// TierFor returns the medal an animal of the given species rates, given its
// trophy rating.
func (st SpeciesTrophy) TierFor(score float64, female bool) TrophyTier {
	if female && st.MaleOnly {
		return TierNone
	}
	if st.Maximum <= 0 {
		return TierUnknown
	}
	switch {
	case score >= st.Maximum:
		return TierGreatOne
	case score >= trophyGoldMin*st.Maximum:
		return TierDiamond
	case score >= trophySilverMin*st.Maximum:
		return TierGold
	case score >= trophyBronzeMin*st.Maximum:
		return TierSilver
	}
	return TierBronze
}

// Bronzes..GreatOne return the score at which each medal starts.
func (st SpeciesTrophy) Thresholds() (bronze, silver, gold, diamond, greatOne float64) {
	if st.Maximum <= 0 {
		return 0, 0, 0, 0, 0
	}
	m := st.Maximum
	return trophyBronzeMin * m, trophySilverMin * m, trophyGoldMin * m, m, m
}

// maleOnlyTrophy is a fallback for the species whose trophy organ is carried by
// males only. The library reads this from the installed game on every cache
// rebuild (the "Trophy type" column of its per-species organ sheets), so an
// updated install refreshes it; this copy is only used when no game data is
// available.
var maleOnlyTrophy = map[string]bool{
	"axis_deer": true, "barasingha": true, "blackbuck": true,
	"blacktail_deer": true, "fallow_deer": true, "hog_deer": true,
	"iberian_mouflon": true, "javan_rusa": true, "manitoban_elk": true,
	"moose": true, "mule_deer": true, "nilgai": true,
	"northern_red_muntjac": true, "red_deer": true, "rockymountain_elk": true,
	"roe_deer": true, "roosevelt_elk": true, "sambar": true,
	"siberian_musk_deer": true, "sika_deer": true, "whitetail_deer": true,
}

// defaultTrophyMaximums holds species maximum trophy ratings. The game does not
// ship these (they are computed from each species' trophy model), so they come
// from the community-documented values on the game's wiki. They are validated
// against this save's own harvest history: for every harvested species the
// implied band is consistent with the recorded medal.
//
// Callers can override or extend any entry through Options.TrophyMaximums.
var defaultTrophyMaximums = map[string]float64{
	"canada_goose": 9.4, "mallard": 2.1, "scrub_hare": 5.9,
	"antelope_jackrabbit": 7.0, "sidestriped_jackal": 32.3, "red_fox": 15.6,
	"eurasian_lynx": 30.7, "coyote": 63.1, "eu_hare": 7.2,
	"harlequin_duck": 8.0, "cinnamon_teal": 5.1, "siberian_musk_deer": 276.7,
	"roe_deer": 99.2, "springbok": 122.9, "fallow_deer": 279.7,
	"blacktail_deer": 197.2, "whitetail_deer": 283.3, "red_deer": 278.9,
	"blue_wildebeest": 41.8, "roosevelt_elk": 423.1, "moose": 305.4,
	"reindeer": 478.0, "caribou": 478.0, "mule_deer": 349.8,
	"axis_deer": 241.3, "lesser_kudu": 35.8, "warthog": 64.6,
	"wild_boar": 160.2, "black_bear": 25.2, "eurasian_brown_bear": 30.8,
	"grizzly_bear": 74.3, "puma": 43.3, "lion": 53.9, "gray_wolf": 43.3,
	"iberian_wolf": 43.3, "eu_bison": 300.3, "plains_bison": 245.7,
	"cape_buffalo": 168.1, "water_buffalo": 186.1, "blackbuck": 146.9,
	"iberian_mouflon": 199.4, "gredos_ibex": 112.1, "beceite_ibex": 212.9,
	"ronda_ibex": 119.9, "southeastern_ibex": 99.6,
	"eastern_grey_kangaroo": 530.0, "saltwater_crocodile": 1100.0,
}

// A failed harvest check downgrades the medal by exactly one level, so a
// harvested animal's stored tier can be one worse than its rating implies.
// Nothing in the save records whether the check passed, so a tier computed from
// Score is the *best* tier the animal could have earned.

// speciesTrophy builds the resolved trophy table: caller overrides win over the
// built-in documented values. maleOnly comes from the installed game when it is
// known, and falls back to the embedded list otherwise.
func speciesTrophy(overrides map[string]float64, fromInstall map[string]bool) map[string]SpeciesTrophy {
	out := make(map[string]SpeciesTrophy, len(defaultTrophyMaximums)+len(overrides))
	for key, max := range defaultTrophyMaximums {
		out[key] = SpeciesTrophy{Maximum: max, Source: "default"}
	}
	for key, max := range overrides {
		st := out[key]
		st.Maximum = max
		st.Source = "option"
		out[key] = st
	}
	maleOnly := fromInstall
	if len(maleOnly) == 0 {
		maleOnly = maleOnlyTrophy
	}
	for key := range maleOnly {
		if _, ok := out[key]; !ok {
			out[key] = SpeciesTrophy{}
		}
	}
	for key, st := range out {
		st.MaleOnly = maleOnly[key]
		out[key] = st
	}
	return out
}

// applyTrophyTiers sets the tier fields on every animal of the save.
func applyTrophyTiers(save *Save, opts Options, game *Game) {
	var fromInstall map[string]bool
	if game != nil {
		fromInstall = game.MaleOnlyTrophySpecies()
	}
	trophies := speciesTrophy(opts.TrophyMaximums, fromInstall)
	save.Trophies = trophies
	for _, r := range save.Reserves {
		for _, sp := range r.Species {
			st := trophies[sp.SpeciesKey]
			for _, g := range sp.Groups {
				for _, a := range g.Animals {
					setTier(a, st)
				}
			}
		}
	}
}

func setTier(a *Animal, st SpeciesTrophy) {
	if a == nil {
		return
	}
	t := st.TierFor(a.Score, a.Gender == GenderFemale)
	a.Tier = t
	a.IsDiamond = t == TierDiamond
	a.IsGold = t == TierGold
	a.IsSilver = t == TierSilver
	a.IsBronze = t == TierBronze
	a.IsGreatOne = t == TierGreatOne
	a.IsNone = t == TierNone
}
