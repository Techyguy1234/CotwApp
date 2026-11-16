package adf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Errors returned by the parser.
var (
	ErrShortData   = errors.New("adf: not enough data")
	ErrBadMagic    = errors.New("adf: magic does not match (expected ' FDA')")
	ErrMissingType = errors.New("adf: missing typedef")
	ErrUnsupported = errors.New("adf: unsupported typedef")
)

// cursor is a bounds checked little endian reader over a byte slice.
type cursor struct {
	buf []byte
	pos int
}

func (c *cursor) remaining() int { return len(c.buf) - c.pos }

func (c *cursor) take(n int) ([]byte, error) {
	if n < 0 || c.pos+n > len(c.buf) {
		return nil, fmt.Errorf("%w: want %d bytes at %d of %d", ErrShortData, n, c.pos, len(c.buf))
	}
	b := c.buf[c.pos : c.pos+n]
	c.pos += n
	return b, nil
}

func (c *cursor) u8() (uint8, error) {
	b, err := c.take(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (c *cursor) u16() (uint16, error) {
	b, err := c.take(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b), nil
}

func (c *cursor) u32() (uint32, error) {
	b, err := c.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (c *cursor) u64() (uint64, error) {
	b, err := c.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}

// strz reads a NUL terminated string.
func (c *cursor) strz() (string, error) {
	start := c.pos
	for c.pos < len(c.buf) {
		if c.buf[c.pos] == 0 {
			s := string(c.buf[start:c.pos])
			c.pos++
			return s, nil
		}
		c.pos++
	}
	return "", fmt.Errorf("%w: unterminated string at %d", ErrShortData, start)
}

// Parse decodes a complete (already decompressed) ADF image.
// Parse decodes an ADF image: header, tables, and every instance value.
func Parse(data []byte) (*Adf, error) { return parseADF(data, true) }

// ParseTypes decodes only the header and tables — typedef names plus the
// instance (name, type) pairs — without touching instance data. It is used to
// classify archive entries that are too large to read whole, where the instance
// payload lies beyond the bytes held in memory.
func ParseTypes(data []byte) (*Adf, error) { return parseADF(data, false) }

func parseADF(data []byte, decodeValues bool) (*Adf, error) {
	if len(data) < 0x40 {
		return nil, fmt.Errorf("%w: file shorter than 0x40 bytes", ErrShortData)
	}

	a := &Adf{
		TypedefMap: make(map[uint32]*TypeDef),
		StringHash: make(map[uint64]string),
		Body:       data,
	}

	h := &cursor{buf: data}
	magic, err := h.take(4)
	if err != nil {
		return nil, err
	}
	if string(magic) != " FDA" {
		return nil, fmt.Errorf("%w: got %q", ErrBadMagic, string(magic))
	}
	a.Magic = string(magic)

	fields := []*uint32{
		&a.Version,
		&a.InstanceCount, &a.InstanceOffset,
		&a.TypedefCount, &a.TypedefOffset,
		&a.StringHashCount, &a.StringHashOffset,
		&a.NameTableCount, &a.NameTableOffset,
		&a.TotalSize,
	}
	for _, dst := range fields {
		v, err := h.u32()
		if err != nil {
			return nil, err
		}
		*dst = v
	}
	for i := range a.Unknown {
		v, err := h.u32()
		if err != nil {
			return nil, err
		}
		a.Unknown[i] = v
	}

	if a.Comment, err = h.strz(); err != nil {
		return nil, fmt.Errorf("adf: comment: %w", err)
	}
	if err := a.readNameTable(); err != nil {
		return nil, err
	}
	if err := a.readStringHashes(); err != nil {
		return nil, err
	}
	if err := a.readTypedefs(); err != nil {
		return nil, err
	}
	if err := a.readInstanceTable(); err != nil {
		return nil, err
	}
	if decodeValues {
		if err := a.readInstanceValues(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *Adf) readNameTable() error {
	count := int(a.NameTableCount)
	if count < 0 || count*2 > len(a.Body) {
		return fmt.Errorf("adf: implausible nametable count %d", count)
	}
	c := &cursor{buf: a.Body, pos: int(a.NameTableOffset)}

	lengths := make([]int, count)
	for i := 0; i < count; i++ {
		n, err := c.u8()
		if err != nil {
			return fmt.Errorf("adf: nametable length %d: %w", i, err)
		}
		lengths[i] = int(n)
	}

	a.Names = make([]string, count)
	for i := 0; i < count; i++ {
		// The reference reader consumes length+1 bytes and drops the last one
		// (the NUL terminator).
		b, err := c.take(lengths[i] + 1)
		if err != nil {
			return fmt.Errorf("adf: nametable entry %d: %w", i, err)
		}
		a.Names[i] = string(b[:len(b)-1])
	}
	return nil
}

func (a *Adf) name(idx uint64) string {
	if idx < uint64(len(a.Names)) {
		return a.Names[idx]
	}
	return fmt.Sprintf("<bad name index %d>", idx)
}

func (a *Adf) readStringHashes() error {
	count := int(a.StringHashCount)
	c := &cursor{buf: a.Body, pos: int(a.StringHashOffset)}
	for i := 0; i < count; i++ {
		value, err := c.strz()
		if err != nil {
			return fmt.Errorf("adf: string hash %d: %w", i, err)
		}
		hash, err := c.u64()
		if err != nil {
			return fmt.Errorf("adf: string hash %d: %w", i, err)
		}
		a.StringHash[hash] = value
	}
	return nil
}

func (a *Adf) readTypedefs() error {
	count := int(a.TypedefCount)
	c := &cursor{buf: a.Body, pos: int(a.TypedefOffset)}
	a.Typedefs = make([]*TypeDef, 0, count)

	for i := 0; i < count; i++ {
		td := &TypeDef{Position: c.pos}
		var err error
		if td.Metatype, err = c.u32(); err != nil {
			return fmt.Errorf("adf: typedef %d: %w", i, err)
		}
		if td.Size, err = c.u32(); err != nil {
			return fmt.Errorf("adf: typedef %d: %w", i, err)
		}
		if td.Alignment, err = c.u32(); err != nil {
			return fmt.Errorf("adf: typedef %d: %w", i, err)
		}
		if td.TypeHash, err = c.u32(); err != nil {
			return fmt.Errorf("adf: typedef %d: %w", i, err)
		}
		nameIdx, err := c.u64()
		if err != nil {
			return fmt.Errorf("adf: typedef %d: %w", i, err)
		}
		td.Name = a.name(nameIdx)
		if td.Flags, err = c.u32(); err != nil {
			return fmt.Errorf("adf: typedef %d: %w", i, err)
		}
		if td.ElementTypeHash, err = c.u32(); err != nil {
			return fmt.Errorf("adf: typedef %d: %w", i, err)
		}
		if td.ElementLength, err = c.u32(); err != nil {
			return fmt.Errorf("adf: typedef %d: %w", i, err)
		}

		switch td.Metatype {
		case MetaPrimitive, MetaString:
			// nothing further
		case MetaStructure:
			n, err := c.u32()
			if err != nil {
				return fmt.Errorf("adf: typedef %d members: %w", i, err)
			}
			if int(n) > c.remaining()/16 {
				return fmt.Errorf("adf: typedef %d implausible member count %d", i, n)
			}
			td.Members = make([]MemberDef, n)
			for j := range td.Members {
				m := &td.Members[j]
				idx, err := c.u64()
				if err != nil {
					return fmt.Errorf("adf: typedef %d member %d: %w", i, j, err)
				}
				m.Name = a.name(idx)
				if m.TypeHash, err = c.u32(); err != nil {
					return fmt.Errorf("adf: typedef %d member %d: %w", i, j, err)
				}
				if m.Size, err = c.u32(); err != nil {
					return fmt.Errorf("adf: typedef %d member %d: %w", i, j, err)
				}
				rawOffset, err := c.u32()
				if err != nil {
					return fmt.Errorf("adf: typedef %d member %d: %w", i, j, err)
				}
				m.Offset = rawOffset & 0x00FFFFFF
				m.BitOffset = uint8((rawOffset >> 24) & 0xFF)
				if m.DefaultType, err = c.u32(); err != nil {
					return fmt.Errorf("adf: typedef %d member %d: %w", i, j, err)
				}
				if m.DefaultValue, err = c.u64(); err != nil {
					return fmt.Errorf("adf: typedef %d member %d: %w", i, j, err)
				}
			}
		case MetaEnumeration:
			n, err := c.u32()
			if err != nil {
				return fmt.Errorf("adf: typedef %d enums: %w", i, err)
			}
			if int(n) > c.remaining()/12 {
				return fmt.Errorf("adf: typedef %d implausible enum count %d", i, n)
			}
			td.Enums = make([]EnumDef, n)
			for j := range td.Enums {
				idx, err := c.u64()
				if err != nil {
					return fmt.Errorf("adf: typedef %d enum %d: %w", i, j, err)
				}
				td.Enums[j].Name = a.name(idx)
				if td.Enums[j].Value, err = c.u32(); err != nil {
					return fmt.Errorf("adf: typedef %d enum %d: %w", i, j, err)
				}
			}
		case MetaPointer, MetaArray:
			// A non-zero count here means an unsupported extension; the value
			// is ignored (and no extra bytes consumed) as in the reference
			// reader.
			if _, err := c.u32(); err != nil {
				return fmt.Errorf("adf: typedef %d: %w", i, err)
			}
		case MetaInlineArray, MetaBitfield, MetaStringHash:
			n, err := c.u32()
			if err != nil {
				return fmt.Errorf("adf: typedef %d: %w", i, err)
			}
			if n != 0 {
				return fmt.Errorf("%w: typedef %d (%s) metatype %s with count %d",
					ErrUnsupported, i, td.Name, MetaName(td.Metatype), n)
			}
		default:
			return fmt.Errorf("%w: typedef %d (%s) has unknown metatype %d",
				ErrUnsupported, i, td.Name, td.Metatype)
		}

		a.Typedefs = append(a.Typedefs, td)
		if _, exists := a.TypedefMap[td.TypeHash]; !exists {
			a.TypedefMap[td.TypeHash] = td
		}
	}
	return nil
}

func (a *Adf) readInstanceTable() error {
	count := int(a.InstanceCount)
	c := &cursor{buf: a.Body, pos: int(a.InstanceOffset)}

	a.Instances = make([]InstanceEntry, count)
	for i := range a.Instances {
		ins := &a.Instances[i]
		ins.Position = c.pos
		var err error
		if ins.NameHash, err = c.u32(); err != nil {
			return fmt.Errorf("adf: instance %d: %w", i, err)
		}
		if ins.TypeHash, err = c.u32(); err != nil {
			return fmt.Errorf("adf: instance %d: %w", i, err)
		}
		if ins.Offset, err = c.u32(); err != nil {
			return fmt.Errorf("adf: instance %d: %w", i, err)
		}
		if ins.Size, err = c.u32(); err != nil {
			return fmt.Errorf("adf: instance %d: %w", i, err)
		}
		nameIdx, err := c.u64()
		if err != nil {
			return fmt.Errorf("adf: instance %d: %w", i, err)
		}
		ins.Name = a.name(nameIdx)
	}
	return nil
}

func (a *Adf) readInstanceValues() error {
	a.Values = make([]*Value, len(a.Instances))
	for i := range a.Instances {
		ins := &a.Instances[i]
		end := uint64(ins.Offset) + uint64(ins.Size)
		if end > uint64(len(a.Body)) {
			return fmt.Errorf("%w: instance %d spans %d..%d of %d",
				ErrShortData, i, ins.Offset, end, len(a.Body))
		}
		buf := a.Body[ins.Offset:end]
		v, _, err := a.readInstance(buf, 0, ins.TypeHash, ins.Offset, -1, 0)
		if err != nil {
			return fmt.Errorf("adf: instance %d (%s): %w", i, ins.Name, err)
		}
		a.Values[i] = v
	}
	return nil
}

// Instance returns the first top level instance whose name matches (usually "0").
func (a *Adf) Instance(name string) *Value {
	for i := range a.Instances {
		if a.Instances[i].Name == name {
			return a.Values[i]
		}
	}
	return nil
}

// InstanceOfType returns the first instance decoded as the given typedef name.
func (a *Adf) InstanceOfType(name string) *Value {
	for i := range a.Instances {
		if td, ok := a.TypedefMap[a.Instances[i].TypeHash]; ok && td.Name == name {
			return a.Values[i]
		}
	}
	return nil
}

// InstanceOfTypePrefix returns the first instance whose typedef name starts with
// prefix. ADF type names carry a version suffix ("AnimalPopulationReserveData_9",
// "NeedZoneSave_3"), so callers match on the stable part of the name.
func (a *Adf) InstanceOfTypePrefix(prefix string) *Value {
	for i := range a.Instances {
		if td, ok := a.TypedefMap[a.Instances[i].TypeHash]; ok && strings.HasPrefix(td.Name, prefix) {
			return a.Values[i]
		}
	}
	return nil
}

// TypedefPrefix returns the first typedef whose name starts with prefix.
func (a *Adf) TypedefPrefix(prefix string) *TypeDef {
	for _, td := range a.Typedefs {
		if strings.HasPrefix(td.Name, prefix) {
			return td
		}
	}
	return nil
}

// InstanceTypeName returns the typedef name of instance i ("" when unknown).
func (a *Adf) InstanceTypeName(i int) string {
	if i < 0 || i >= len(a.Instances) {
		return ""
	}
	if td, ok := a.TypedefMap[a.Instances[i].TypeHash]; ok {
		return td.Name
	}
	return ""
}

// float32From reassembles a float32 held in a uint32.
func float32From(v uint32) float64 {
	return float64(math.Float32frombits(v))
}
