package adf

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Kind is the dynamic shape of a decoded ADF value.
type Kind int

// Value kinds.
const (
	KindNil Kind = iota
	KindInt
	KindUint
	KindFloat
	KindBytes
	KindString
	KindStruct
	KindArray
	KindPointer
)

// String renders the kind name.
func (k Kind) String() string {
	switch k {
	case KindNil:
		return "nil"
	case KindInt:
		return "int"
	case KindUint:
		return "uint"
	case KindFloat:
		return "float"
	case KindBytes:
		return "bytes"
	case KindString:
		return "string"
	case KindStruct:
		return "struct"
	case KindArray:
		return "array"
	case KindPointer:
		return "pointer"
	}
	return "unknown"
}

// Member is a named field of a decoded structure, kept in typedef order.
type Member struct {
	Name  string
	Value *Value
}

// Value is one decoded ADF value. Scalars live in Int/Uint/Float, strings in
// Str, byte blobs in Bytes, structures in Members, arrays in Elems.
//
// DataOffset is the absolute offset, within the decompressed ADF body, of the
// value's data; for arrays that is element zero, for structures the structure
// itself. InfoOffset is the offset of the value's descriptor when it differs.
type Value struct {
	TypeID       uint32
	InfoOffset   uint32
	DataOffset   uint32
	BitOffset    int // -1 when the value is not a bitfield member
	EnumString   string
	HashString   string
	Kind         Kind
	Int          int64
	Uint         uint64
	Float        float64
	Bytes        []byte
	Str          string
	Members      []Member
	Elems        []*Value
	IdentifiedAs string // typedef name, informational
}

// Field returns the named member of a structure, or nil.
func (v *Value) Field(name string) *Value {
	if v == nil || v.Kind != KindStruct {
		return nil
	}
	for i := range v.Members {
		if v.Members[i].Name == name {
			return v.Members[i].Value
		}
	}
	return nil
}

// Len reports the number of elements in an array (or bytes for a byte blob).
func (v *Value) Len() int {
	if v == nil {
		return 0
	}
	switch v.Kind {
	case KindArray:
		return len(v.Elems)
	case KindBytes:
		return len(v.Bytes)
	}
	return 0
}

// Float64 coerces a scalar value to float64.
func (v *Value) Float64() (float64, error) {
	if v == nil {
		return 0, fmt.Errorf("adf: nil value")
	}
	switch v.Kind {
	case KindFloat:
		return v.Float, nil
	case KindInt:
		return float64(v.Int), nil
	case KindUint:
		return float64(v.Uint), nil
	}
	return 0, fmt.Errorf("adf: value of kind %s is not numeric", v.Kind)
}

// Int64 coerces a scalar value to int64.
func (v *Value) Int64() (int64, error) {
	if v == nil {
		return 0, fmt.Errorf("adf: nil value")
	}
	switch v.Kind {
	case KindInt:
		return v.Int, nil
	case KindUint:
		return int64(v.Uint), nil
	case KindFloat:
		return int64(v.Float), nil
	}
	return 0, fmt.Errorf("adf: value of kind %s is not an integer", v.Kind)
}

// Uint64 coerces a scalar value to uint64.
func (v *Value) Uint64() (uint64, error) {
	if v == nil {
		return 0, fmt.Errorf("adf: nil value")
	}
	switch v.Kind {
	case KindUint:
		return v.Uint, nil
	case KindInt:
		return uint64(v.Int), nil
	case KindFloat:
		return uint64(v.Float), nil
	}
	return 0, fmt.Errorf("adf: value of kind %s is not an unsigned integer", v.Kind)
}

// Uint32 coerces a scalar value to uint32.
func (v *Value) Uint32() (uint32, error) {
	u, err := v.Uint64()
	if err != nil {
		return 0, err
	}
	return uint32(u), nil
}

// Int32 coerces a scalar value to int32.
func (v *Value) Int32() (int32, error) {
	i, err := v.Int64()
	if err != nil {
		return 0, err
	}
	return int32(i), nil
}

// Bool reports whether a numeric value is non-zero.
func (v *Value) Bool() bool {
	if v == nil {
		return false
	}
	switch v.Kind {
	case KindInt:
		return v.Int != 0
	case KindUint:
		return v.Uint != 0
	case KindFloat:
		return v.Float != 0
	}
	return false
}

// BytesValue returns the raw byte blob of a KindBytes value.
func (v *Value) BytesValue() []byte {
	if v == nil || v.Kind != KindBytes {
		return nil
	}
	return v.Bytes
}

// StringValue returns the string of a KindString value.
func (v *Value) StringValue() string {
	if v == nil || v.Kind != KindString {
		return ""
	}
	return v.Str
}

const maxDepth = 64

// readInstance decodes the value of the given type at buffer position pos.
// buf is the instance's slice of the decompressed file; abs is the absolute
// file offset of buf[0], used to report data offsets.
func (a *Adf) readInstance(buf []byte, pos int, typeID uint32, abs uint32, bitOffset int, depth int) (*Value, int, error) {
	if depth > maxDepth {
		return nil, pos, fmt.Errorf("adf: nesting deeper than %d at type 0x%08x", maxDepth, typeID)
	}
	if pos < 0 || pos > len(buf) {
		return nil, pos, fmt.Errorf("%w: read at %d of %d", ErrShortData, pos, len(buf))
	}

	// --- fixed size scalars ---------------------------------------------
	if IsPrimitive(typeID) {
		n := PrimitiveSize(typeID)
		if pos+n > len(buf) {
			return nil, pos, fmt.Errorf("%w: %s at %d of %d", ErrShortData, TypeName(typeID), pos, len(buf))
		}
		data := buf[pos : pos+n]
		v := &Value{TypeID: typeID, InfoOffset: abs + uint32(pos), DataOffset: abs + uint32(pos), BitOffset: -1, IdentifiedAs: TypeName(typeID)}
		switch typeID {
		case TypeS8:
			v.Kind, v.Int = KindInt, int64(int8(data[0]))
		case TypeU8:
			v.Kind, v.Uint = KindUint, uint64(data[0])
		case TypeS16:
			v.Kind, v.Int = KindInt, int64(int16(binary.LittleEndian.Uint16(data)))
		case TypeU16:
			v.Kind, v.Uint = KindUint, uint64(binary.LittleEndian.Uint16(data))
		case TypeS32:
			v.Kind, v.Int = KindInt, int64(int32(binary.LittleEndian.Uint32(data)))
		case TypeU32:
			v.Kind, v.Uint = KindUint, uint64(binary.LittleEndian.Uint32(data))
		case TypeS64:
			v.Kind, v.Int = KindInt, int64(binary.LittleEndian.Uint64(data))
		case TypeU64:
			v.Kind, v.Uint = KindUint, binary.LittleEndian.Uint64(data)
		case TypeF32:
			v.Kind, v.Float = KindFloat, float32From(binary.LittleEndian.Uint32(data))
		case TypeF64:
			v.Kind, v.Float = KindFloat, math.Float64frombits(binary.LittleEndian.Uint64(data))
		}
		return v, pos + n, nil
	}

	// --- inline string (offset + length, payload elsewhere in the buffer) ---
	if typeID == TypeString {
		if pos+8 > len(buf) {
			return nil, pos, fmt.Errorf("%w: string header at %d", ErrShortData, pos)
		}
		offset := int(binary.LittleEndian.Uint32(buf[pos:]))
		s, err := (&cursor{buf: buf, pos: offset}).strz()
		if err != nil {
			return nil, pos, fmt.Errorf("adf: string payload: %w", err)
		}
		v := &Value{
			TypeID: typeID, InfoOffset: abs + uint32(pos), DataOffset: abs + uint32(offset),
			BitOffset: -1, Kind: KindString, Str: s, IdentifiedAs: "string",
		}
		return v, pos + 8, nil
	}

	// --- deferred value (pointer to a typed value in the same buffer) ------
	if typeID == TypeDeferred {
		if pos+16 > len(buf) {
			return nil, pos, fmt.Errorf("%w: deferred header at %d", ErrShortData, pos)
		}
		var v0 [4]uint32
		for i := 0; i < 4; i++ {
			v0[i] = binary.LittleEndian.Uint32(buf[pos+4*i:])
		}
		if v0[0] == 0 || v0[2] == 0 {
			return &Value{TypeID: typeID, InfoOffset: abs + uint32(pos), DataOffset: abs + uint32(pos), BitOffset: -1, Kind: KindNil}, pos + 16, nil
		}
		sub, _, err := a.readInstance(buf, int(v0[0]), v0[2], abs, -1, depth+1)
		if err != nil {
			return nil, pos, fmt.Errorf("adf: deferred value: %w", err)
		}
		sub.InfoOffset = abs + uint32(pos)
		sub.DataOffset = abs + v0[0]
		sub.BitOffset = -1
		return sub, pos + 16, nil
	}

	if typeID == TypeGDC {
		return nil, pos, fmt.Errorf("%w: gdc archive instance (0x%08x)", ErrUnsupported, typeID)
	}

	// --- compound types --------------------------------------------------
	td, ok := a.TypedefMap[typeID]
	if !ok {
		return nil, pos, fmt.Errorf("%w: 0x%08x at %d", ErrMissingType, typeID, pos)
	}

	switch td.Metatype {
	case MetaStructure:
		v := &Value{
			TypeID: typeID, InfoOffset: abs + uint32(pos), DataOffset: abs + uint32(pos),
			BitOffset: -1, Kind: KindStruct, Members: make([]Member, 0, len(td.Members)),
			IdentifiedAs: td.Name,
		}
		for _, m := range td.Members {
			fv, _, err := a.readInstance(buf, pos+int(m.Offset), m.TypeHash, abs, int(m.BitOffset), depth+1)
			if err != nil {
				return nil, pos, fmt.Errorf("adf: field %s of %s: %w", m.Name, td.Name, err)
			}
			v.Members = append(v.Members, Member{Name: m.Name, Value: fv})
		}
		return v, pos + int(td.Size), nil

	case MetaPointer:
		if pos+8 > len(buf) {
			return nil, pos, fmt.Errorf("%w: pointer at %d", ErrShortData, pos)
		}
		raw := binary.LittleEndian.Uint64(buf[pos:])
		return &Value{
			TypeID: typeID, InfoOffset: abs + uint32(pos), DataOffset: abs + uint32(pos),
			BitOffset: -1, Kind: KindPointer, Uint: raw, IdentifiedAs: "pointer",
		}, pos + 8, nil

	case MetaArray, MetaInlineArray:
		var (
			offset int
			length int
			resume int
		)
		if td.Metatype == MetaArray {
			if pos+12 > len(buf) {
				return nil, pos, fmt.Errorf("%w: array header at %d", ErrShortData, pos)
			}
			offset = int(binary.LittleEndian.Uint32(buf[pos:]))
			// buf[pos+4:] is a flags field, ignored by the reference reader.
			length = int(binary.LittleEndian.Uint32(buf[pos+8:]))
			resume = pos + 12
		} else {
			offset = pos
			length = int(td.ElementLength)
			resume = pos
		}

		if length < 0 {
			return nil, pos, fmt.Errorf("adf: negative array length %d", length)
		}

		v := &Value{
			TypeID: typeID, InfoOffset: abs + uint32(pos), DataOffset: abs + uint32(offset),
			BitOffset: -1, Kind: KindArray, IdentifiedAs: td.Name,
		}

		if IsPrimitive(td.ElementTypeHash) {
			es := PrimitiveSize(td.ElementTypeHash)
			if offset+length*es > len(buf) {
				return nil, pos, fmt.Errorf("%w: %s[%d] at %d of %d",
					ErrShortData, TypeName(td.ElementTypeHash), length, offset, len(buf))
			}
			if es == 1 {
				// Byte blobs are kept as bytes, matching the reference reader.
				v.Kind = KindBytes
				v.Bytes = append([]byte(nil), buf[offset:offset+length]...)
				return v, resume, nil
			}
			v.Elems = make([]*Value, length)
			for i := 0; i < length; i++ {
				ev, _, err := a.readInstance(buf, offset+i*es, td.ElementTypeHash, abs, -1, depth+1)
				if err != nil {
					return nil, pos, fmt.Errorf("adf: element %d of %s: %w", i, td.Name, err)
				}
				v.Elems[i] = ev
			}
			return v, resume, nil
		}

		v.Elems = make([]*Value, length)
		p := offset
		for i := 0; i < length; i++ {
			ev, next, err := a.readInstance(buf, p, td.ElementTypeHash, abs, -1, depth+1)
			if err != nil {
				return nil, pos, fmt.Errorf("adf: element %d of %s: %w", i, td.Name, err)
			}
			v.Elems[i] = ev
			p = next
		}
		return v, resume, nil

	case MetaBitfield:
		var raw uint64
		switch td.Size {
		case 1:
			if pos+1 > len(buf) {
				return nil, pos, ErrShortData
			}
			raw = uint64(buf[pos])
		case 2:
			if pos+2 > len(buf) {
				return nil, pos, ErrShortData
			}
			raw = uint64(binary.LittleEndian.Uint16(buf[pos:]))
		case 4:
			if pos+4 > len(buf) {
				return nil, pos, ErrShortData
			}
			raw = uint64(binary.LittleEndian.Uint32(buf[pos:]))
		case 8:
			if pos+8 > len(buf) {
				return nil, pos, ErrShortData
			}
			raw = binary.LittleEndian.Uint64(buf[pos:])
		default:
			return nil, pos, fmt.Errorf("%w: bitfield size %d", ErrUnsupported, td.Size)
		}
		if bitOffset < 0 {
			bitOffset = 0
		}
		bit := (raw >> uint(bitOffset)) & 1
		return &Value{
			TypeID: typeID, InfoOffset: abs + uint32(pos), DataOffset: abs + uint32(pos),
			BitOffset: bitOffset, Kind: KindUint, Uint: bit, IdentifiedAs: td.Name,
		}, pos + int(td.Size), nil

	case MetaEnumeration:
		if td.Size != 4 {
			return nil, pos, fmt.Errorf("%w: enumeration size %d", ErrUnsupported, td.Size)
		}
		if pos+4 > len(buf) {
			return nil, pos, ErrShortData
		}
		raw := binary.LittleEndian.Uint32(buf[pos:])
		name := ""
		if int(raw) < len(td.Enums) {
			name = td.Enums[raw].Name
		}
		return &Value{
			TypeID: typeID, InfoOffset: abs + uint32(pos), DataOffset: abs + uint32(pos),
			BitOffset: -1, Kind: KindUint, Uint: uint64(raw), EnumString: name, IdentifiedAs: td.Name,
		}, pos + 4, nil

	case MetaStringHash:
		var (
			raw uint64
			n   int
		)
		switch td.Size {
		case 4:
			if pos+4 > len(buf) {
				return nil, pos, ErrShortData
			}
			raw = uint64(binary.LittleEndian.Uint32(buf[pos:]))
			n = 4
		case 6:
			if pos+6 > len(buf) {
				return nil, pos, ErrShortData
			}
			v0 := uint64(binary.LittleEndian.Uint16(buf[pos:]))
			v1 := uint64(binary.LittleEndian.Uint16(buf[pos+2:]))
			v2 := uint64(binary.LittleEndian.Uint16(buf[pos+4:]))
			raw = v0<<32 | v1<<16 | v2
			n = 6
		case 8:
			if pos+8 > len(buf) {
				return nil, pos, ErrShortData
			}
			raw = binary.LittleEndian.Uint64(buf[pos:])
			n = 8
		default:
			if pos+int(td.Size) > len(buf) {
				return nil, pos, ErrShortData
			}
			b := buf[pos : pos+int(td.Size)]
			var acc uint64
			for i := 0; i < len(b) && i < 8; i++ {
				acc |= uint64(b[i]) << (8 * uint(i))
			}
			raw = acc
			n = int(td.Size)
		}
		return &Value{
			TypeID: typeID, InfoOffset: abs + uint32(pos), DataOffset: abs + uint32(pos),
			BitOffset: -1, Kind: KindUint, Uint: raw, HashString: a.StringHash[raw], IdentifiedAs: td.Name,
		}, pos + n, nil
	}

	return nil, pos, fmt.Errorf("%w: typedef %s metatype %s", ErrUnsupported, td.Name, MetaName(td.Metatype))
}
