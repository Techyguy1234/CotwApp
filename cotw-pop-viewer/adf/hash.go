package adf

// Jenkins lookup3 "hashlittle2" / "hash32", the string hash function used by
// Avalanche's engine. Ported from deca/hashes.py in the apc project.
//
// COTW uses it for, among other things:
//   - population definition ids:   hash32("<definition name>_population")
//   - need zone species:           hash32("animal_<codex id>_name")
//   - reserve names:               hash32("reserve_<key>_name[_uppercase]")
//   - ADF string-hash tables and archive path hashes.

func rot32(x uint32, k uint) uint32 {
	return (x << k) | (x >> (32 - k))
}

func mix(a, b, c uint32) (uint32, uint32, uint32) {
	a -= c
	a ^= rot32(c, 4)
	c += b
	b -= a
	b ^= rot32(a, 6)
	a += c
	c -= b
	c ^= rot32(b, 8)
	b += a
	a -= c
	a ^= rot32(c, 16)
	c += b
	b -= a
	b ^= rot32(a, 19)
	a += c
	c -= b
	c ^= rot32(b, 4)
	b += a
	return a, b, c
}

func final(a, b, c uint32) (uint32, uint32, uint32) {
	c ^= b
	c -= rot32(b, 14)
	a ^= c
	a -= rot32(c, 11)
	b ^= a
	b -= rot32(a, 25)
	c ^= b
	c -= rot32(b, 16)
	a ^= c
	a -= rot32(c, 4)
	b ^= a
	b -= rot32(a, 14)
	c ^= b
	c -= rot32(b, 24)
	return a, b, c
}

// HashLittle2 returns both 32 bit halves of the lookup3 hash.
func HashLittle2(data []byte, initval, initval2 uint32) (uint32, uint32) {
	length := uint32(len(data))
	a := uint32(0xdeadbeef) + length + initval
	b := a
	c := a + initval2

	remaining := len(data)
	p := 0
	for remaining > 12 {
		a += uint32(data[p]) + uint32(data[p+1])<<8 + uint32(data[p+2])<<16 + uint32(data[p+3])<<24
		b += uint32(data[p+4]) + uint32(data[p+5])<<8 + uint32(data[p+6])<<16 + uint32(data[p+7])<<24
		c += uint32(data[p+8]) + uint32(data[p+9])<<8 + uint32(data[p+10])<<16 + uint32(data[p+11])<<24
		a, b, c = mix(a, b, c)
		p += 12
		remaining -= 12
	}

	// Tail (little endian, partial words).
	switch remaining {
	case 12:
		c += uint32(data[p+8]) + uint32(data[p+9])<<8 + uint32(data[p+10])<<16 + uint32(data[p+11])<<24
		b += uint32(data[p+4]) + uint32(data[p+5])<<8 + uint32(data[p+6])<<16 + uint32(data[p+7])<<24
		a += uint32(data[p]) + uint32(data[p+1])<<8 + uint32(data[p+2])<<16 + uint32(data[p+3])<<24
	case 11:
		c += uint32(data[p+8]) + uint32(data[p+9])<<8 + uint32(data[p+10])<<16
		b += uint32(data[p+4]) + uint32(data[p+5])<<8 + uint32(data[p+6])<<16 + uint32(data[p+7])<<24
		a += uint32(data[p]) + uint32(data[p+1])<<8 + uint32(data[p+2])<<16 + uint32(data[p+3])<<24
	case 10:
		c += uint32(data[p+8]) + uint32(data[p+9])<<8
		b += uint32(data[p+4]) + uint32(data[p+5])<<8 + uint32(data[p+6])<<16 + uint32(data[p+7])<<24
		a += uint32(data[p]) + uint32(data[p+1])<<8 + uint32(data[p+2])<<16 + uint32(data[p+3])<<24
	case 9:
		c += uint32(data[p+8])
		b += uint32(data[p+4]) + uint32(data[p+5])<<8 + uint32(data[p+6])<<16 + uint32(data[p+7])<<24
		a += uint32(data[p]) + uint32(data[p+1])<<8 + uint32(data[p+2])<<16 + uint32(data[p+3])<<24
	case 8:
		b += uint32(data[p+4]) + uint32(data[p+5])<<8 + uint32(data[p+6])<<16 + uint32(data[p+7])<<24
		a += uint32(data[p]) + uint32(data[p+1])<<8 + uint32(data[p+2])<<16 + uint32(data[p+3])<<24
	case 7:
		b += uint32(data[p+4]) + uint32(data[p+5])<<8 + uint32(data[p+6])<<16
		a += uint32(data[p]) + uint32(data[p+1])<<8 + uint32(data[p+2])<<16 + uint32(data[p+3])<<24
	case 6:
		b += uint32(data[p+4]) + uint32(data[p+5])<<8
		a += uint32(data[p]) + uint32(data[p+1])<<8 + uint32(data[p+2])<<16 + uint32(data[p+3])<<24
	case 5:
		b += uint32(data[p+4])
		a += uint32(data[p]) + uint32(data[p+1])<<8 + uint32(data[p+2])<<16 + uint32(data[p+3])<<24
	case 4:
		a += uint32(data[p]) + uint32(data[p+1])<<8 + uint32(data[p+2])<<16 + uint32(data[p+3])<<24
	case 3:
		a += uint32(data[p]) + uint32(data[p+1])<<8 + uint32(data[p+2])<<16
	case 2:
		a += uint32(data[p]) + uint32(data[p+1])<<8
	case 1:
		a += uint32(data[p])
	case 0:
		return c, b
	}

	a, b, c = final(a, b, c)
	return c, b
}

// Hash32 is the primary 32 bit lookup3 hash (the "c" half).
func Hash32(data []byte, initval uint32) uint32 {
	c, _ := HashLittle2(data, initval, 0)
	return c
}

// Hash32String is Hash32 over the bytes of s with a zero initial value.
func Hash32String(s string) uint32 { return Hash32([]byte(s), 0) }
