//go:build windows

package cotw

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// The entity decoder is pure arithmetic over a byte window, so it can be tested
// against a synthetic record built from the real field offsets. This is what
// catches an offset drifting out of step with the sentinel.

// synthRecord builds a whole materialised animal entity: the window the reader
// uses, plus the sentinel that anchors it.
func synthRecord(pos Vec3, weight, score float32, seed uint32) []byte {
	buf := make([]byte, entityPositionBack+len(animalEntitySentinel))
	put := func(off int, v float32) {
		binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(v))
	}
	put(0, float32(pos.X))
	put(4, float32(pos.Y))
	put(8, float32(pos.Z))
	put(entityWeightAt, weight)
	put(entityScoreAt, score)
	binary.LittleEndian.PutUint32(buf[entitySeedAt:], seed)
	copy(buf[entityPositionBack:], animalEntitySentinel)
	return buf
}

func TestDecodeEntityRoundTrip(t *testing.T) {
	want := Vec3{X: -5199.1689, Y: 1031.7935, Z: 10530.5703}
	buf := synthRecord(want, 1.044, 7.370, 2934616616)

	got, ok := decodeEntity(buf)
	if !ok {
		t.Fatal("decodeEntity reported the window too short")
	}
	if got.seed != 2934616616 {
		t.Errorf("seed = %d, want 2934616616", got.seed)
	}
	if got.weight != 1.044 {
		t.Errorf("weight = %v, want 1.044", got.weight)
	}
	if got.score != 7.370 {
		t.Errorf("score = %v, want 7.37", got.score)
	}
	for name, pair := range map[string][2]float64{
		"X": {got.position.X, want.X},
		"Y": {got.position.Y, want.Y},
		"Z": {got.position.Z, want.Z},
	} {
		if math.Abs(pair[0]-pair[1]) > 0.01 {
			t.Errorf("%s = %v, want %v", name, pair[0], pair[1])
		}
	}
}

func TestDecodeEntityShortWindow(t *testing.T) {
	if _, ok := decodeEntity(make([]byte, entityWindow-1)); ok {
		t.Fatal("a short window should be rejected")
	}
}

// The whole scheme rests on the sentinel sitting exactly entityPositionBack
// bytes after the X field, and on the seed field being inside the window the
// reader fetches. This fails loudly if any constant is edited alone.
func TestEntityOffsetsAgreeWithSentinel(t *testing.T) {
	if entityWindow < entitySeedAt+4 {
		t.Fatalf("window %d cannot hold the seed field at %d", entityWindow, entitySeedAt)
	}
	buf := synthRecord(Vec3{X: 1, Y: 2, Z: 3}, 4, 5, 6)
	if !bytes.Equal(buf[entityPositionBack:], animalEntitySentinel) {
		t.Fatal("the sentinel is not at entityPositionBack, so every offset is wrong")
	}
	// The sentinel must not fall inside the window, or a scan hit would be
	// mistaken for data.
	if entityWindow > entityPositionBack {
		t.Fatalf("window %d reaches the sentinel at %d", entityWindow, entityPositionBack)
	}
}

// The sentinel is an eight byte marker; a shorter one would match in unrelated
// data and a longer one would stop matching the engine.
func TestSentinelShape(t *testing.T) {
	if len(animalEntitySentinel) != 8 {
		t.Fatalf("sentinel is %d bytes, want 8", len(animalEntitySentinel))
	}
	if binary.LittleEndian.Uint32(animalEntitySentinel[0:]) != 0x7f7fffff {
		t.Error("sentinel does not start with FLT_MAX")
	}
	if binary.LittleEndian.Uint32(animalEntitySentinel[4:]) != 0xdeadbeef {
		t.Error("sentinel does not end with 0xDEADBEEF")
	}
}

// reserveworlddata_adf is the fallback Status and Positions use to name the
// loaded reserve; make sure the field name the reader expects is the one the
// save actually carries.
func TestReserveWorldDataParses(t *testing.T) {
	dirs := FindSaveDirs()
	if len(dirs) == 0 {
		t.Skip("no save folder on this machine")
	}
	save, err := ParseSave(dirs[0], Options{})
	if err != nil {
		t.Skipf("save not readable: %v", err)
	}
	live := &LiveGame{game: save.GameData()}
	reserve, ok := live.reserveFromSave()
	if !ok {
		t.Fatalf("reserveFromSave failed for %s", dirs[0])
	}
	if reserve < 0 || reserve > 64 {
		t.Fatalf("implausible reserve %d", reserve)
	}
}
