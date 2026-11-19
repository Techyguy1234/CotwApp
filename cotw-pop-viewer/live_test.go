package cotw

import "testing"

// applyWorld is the part of LiveStatus that does not need a running game: it
// turns a reserve number into the status fields a caller reads.
func TestApplyWorldReserve(t *testing.T) {
	st := LiveStatus{ReserveIndex: -1}
	applyWorld(&st, 0)
	if !st.InReserve {
		t.Fatal("reserve 0 should count as being in a reserve")
	}
	if st.ReserveIndex != 0 {
		t.Fatalf("ReserveIndex = %d, want 0", st.ReserveIndex)
	}
	if st.ReserveKey != "hirschfelden" {
		t.Fatalf("ReserveKey = %q, want hirschfelden", st.ReserveKey)
	}
	if st.TrophyLodge != "" {
		t.Fatalf("TrophyLodge = %q, want empty for a huntable reserve", st.TrophyLodge)
	}
}

func TestApplyWorldTrophyLodge(t *testing.T) {
	for _, index := range []int{5, 7, 15} {
		st := LiveStatus{ReserveIndex: -1}
		applyWorld(&st, index)
		if !st.InReserve {
			t.Fatalf("lodge %d should count as being in a world", index)
		}
		if st.TrophyLodge == "" {
			t.Fatalf("lodge %d: TrophyLodge not set", index)
		}
		if st.ReserveKey != "" {
			t.Fatalf("lodge %d: ReserveKey = %q, want empty", index, st.ReserveKey)
		}
	}
}

func TestApplyWorldUnknownReserve(t *testing.T) {
	// A reserve the player does not own has no key; the index is still
	// reported so a caller can look it up itself.
	st := LiveStatus{ReserveIndex: -1}
	applyWorld(&st, 21)
	if st.ReserveIndex != 21 {
		t.Fatalf("ReserveIndex = %d, want 21", st.ReserveIndex)
	}
	if st.ReserveKey != "" || st.TrophyLodge != "" {
		t.Fatalf("unexpected naming for unknown reserve: key=%q lodge=%q", st.ReserveKey, st.TrophyLodge)
	}
}

func TestApplyWorldNegativeStaysOutOfReserve(t *testing.T) {
	st := LiveStatus{ReserveIndex: -1}
	applyWorld(&st, -1)
	if st.InReserve || st.ReserveIndex != -1 {
		t.Fatalf("main menu should report InReserve=false, index -1; got %+v", st)
	}
}
