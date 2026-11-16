package cotw

import "testing"

func TestTrophyTierBands(t *testing.T) {
	// Moose: documented maximum trophy rating 305.4.
	st := SpeciesTrophy{Maximum: 305.4}
	cases := []struct {
		score float64
		want  TrophyTier
	}{
		{0.0, TierBronze},
		{61.0, TierBronze},    // just below 20%
		{61.09, TierSilver},   // 20%
		{183.2, TierSilver},   // just below 60%
		{183.24, TierGold},    // 60%
		{274.8, TierGold},     // just below 90%
		{274.86, TierDiamond}, // 90%
		{305.4, TierGreatOne}, // at the species maximum
		{310.0, TierGreatOne}, // beyond it
	}
	for _, c := range cases {
		if got := st.TierFor(c.score, false); got != c.want {
			t.Errorf("moose score %.2f: got %v, want %v", c.score, got, c.want)
		}
	}
}

func TestTrophyNoOrganAndUnknown(t *testing.T) {
	male := SpeciesTrophy{Maximum: 283.3, MaleOnly: true}
	if got := male.TierFor(280, true); got != TierNone {
		t.Errorf("female of a male-only species: got %v, want NONE", got)
	}
	if got := male.TierFor(280, false); got != TierDiamond {
		t.Errorf("male of a male-only species: got %v, want DIAMOND", got)
	}
	unknown := SpeciesTrophy{}
	if got := unknown.TierFor(100, false); got != TierUnknown {
		t.Errorf("unknown maximum: got %v, want UNKNOWN", got)
	}
	if got := unknown.TierFor(100, true); got != TierUnknown {
		t.Errorf("unknown maximum (female): got %v, want UNKNOWN", got)
	}
}

func TestTrophyThresholds(t *testing.T) {
	bronze, silver, gold, diamond, greatOne := SpeciesTrophy{Maximum: 100}.Thresholds()
	if bronze != 20 || silver != 60 || gold != 90 || diamond != 100 || greatOne != 100 {
		t.Errorf("thresholds = %v %v %v %v %v, want 20 60 90 100 100",
			bronze, silver, gold, diamond, greatOne)
	}
}

func TestTrophyOverridesWin(t *testing.T) {
	table := speciesTrophy(map[string]float64{"moose": 1.0, "nonesuch": 50}, nil)
	if table["moose"].Maximum != 1.0 || table["moose"].Source != "option" {
		t.Errorf("override not applied: %+v", table["moose"])
	}
	if !table["moose"].MaleOnly {
		t.Error("override lost the male-only flag")
	}
	if table["nonesuch"].Maximum != 50 || table["nonesuch"].MaleOnly {
		t.Errorf("new species not added: %+v", table["nonesuch"])
	}
	if table["whitetail_deer"].Source != "default" || !table["whitetail_deer"].MaleOnly {
		t.Errorf("default entry wrong: %+v", table["whitetail_deer"])
	}
}
