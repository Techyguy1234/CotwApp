package adf

import "testing"

// Values produced with deca/hashes.py from the apc project.
func TestHash32(t *testing.T) {
	cases := []struct {
		in   string
		want uint32
	}{
		{"", 0xdeadbeef},
		{"a", 0x58d68708},
		{"eu_rabbit_population", 0x1a5d1561},
		{"wild_boar_general_population", 0x08cbeb4a},
		{"moose_yukon_population", 0x6c0365e2},
		{"animal_pheasant_name", 0xa2e717d8},
		{"reserve_hirschfelden_name", 0x0e8a39e1},
	}
	for _, c := range cases {
		if got := Hash32String(c.in); got != c.want {
			t.Errorf("Hash32String(%q) = 0x%08x, want 0x%08x", c.in, got, c.want)
		}
	}
}

func TestPrimitiveSize(t *testing.T) {
	cases := map[uint32]int{
		TypeS8: 1, TypeU8: 1, TypeS16: 2, TypeU16: 2, TypeS32: 4, TypeU32: 4,
		TypeF32: 4, TypeS64: 8, TypeU64: 8, TypeF64: 8,
	}
	for typ, want := range cases {
		if got := PrimitiveSize(typ); got != want {
			t.Errorf("PrimitiveSize(%s) = %d, want %d", TypeName(typ), got, want)
		}
		if !IsPrimitive(typ) {
			t.Errorf("IsPrimitive(%s) = false", TypeName(typ))
		}
	}
	if IsPrimitive(TypeString) {
		t.Error("IsPrimitive(string) = true, want false")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse(make([]byte, 0x40)); err == nil {
		t.Fatal("Parse of a zeroed header should fail the magic check")
	}
	if _, err := Parse([]byte("short")); err == nil {
		t.Fatal("Parse of a short buffer should fail")
	}
}
