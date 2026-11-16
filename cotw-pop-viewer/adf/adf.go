// Package adf implements a read-only parser for Avalanche Studios ADF files
// (the ".adf" containers used by theHunter: Call of the Wild, including the
// animal_population_N save files and the assets inside the game's .arc
// archives).
//
// The format is documented in deca/ff_adf.py from the apc project; this package
// mirrors that reader: header -> name table -> string hashes -> typedefs ->
// instances, followed by a recursive decode of each instance value.
//
// Nothing here writes to a file.
package adf

import "strings"

// Header is the 0x40 byte ADF header that precedes the comment string.
type Header struct {
	Magic            string
	Version          uint32
	InstanceCount    uint32
	InstanceOffset   uint32
	TypedefCount     uint32
	TypedefOffset    uint32
	StringHashCount  uint32
	StringHashOffset uint32
	NameTableCount   uint32
	NameTableOffset  uint32
	TotalSize        uint32
	Unknown          [5]uint32
}

// MemberDef describes one field of a structure typedef.
type MemberDef struct {
	Name         string
	TypeHash     uint32
	Size         uint32
	Offset       uint32
	BitOffset    uint8
	DefaultType  uint32
	DefaultValue uint64
}

// EnumDef is one named value of an enumeration typedef.
type EnumDef struct {
	Name  string
	Value uint32
}

// TypeDef is a typedef entry: a primitive, structure, array, enum, ...
type TypeDef struct {
	Position        int // file offset of the typedef record
	Metatype        uint32
	Size            uint32
	Alignment       uint32
	TypeHash        uint32
	Name            string
	Flags           uint32
	ElementTypeHash uint32
	ElementLength   uint32
	Members         []MemberDef // metatype 1
	Enums           []EnumDef   // metatype 8
}

// InstanceEntry is one top level instance in the file.
type InstanceEntry struct {
	Position int
	NameHash uint32
	TypeHash uint32
	Offset   uint32
	Size     uint32
	Name     string
}

// Adf is a parsed ADF file.
type Adf struct {
	Header
	Comment    string
	Names      []string
	StringHash map[uint64]string
	Typedefs   []*TypeDef
	TypedefMap map[uint32]*TypeDef
	Instances  []InstanceEntry
	// Values holds the decoded value of every instance, in file order.
	Values []*Value
	// Body is the decompressed ADF image these values were decoded from.
	Body []byte
}

// Metatype constants.
const (
	MetaPrimitive   uint32 = 0
	MetaStructure   uint32 = 1
	MetaPointer     uint32 = 2
	MetaArray       uint32 = 3
	MetaInlineArray uint32 = 4
	MetaString      uint32 = 5
	MetaType6       uint32 = 6
	MetaBitfield    uint32 = 7
	MetaEnumeration uint32 = 8
	MetaStringHash  uint32 = 9
)

// Primitive and special type hashes.
const (
	TypeS8       uint32 = 0x580D0A62
	TypeU8       uint32 = 0x0CA2821D
	TypeS16      uint32 = 0xD13FCF93
	TypeU16      uint32 = 0x86D152BD
	TypeS32      uint32 = 0x192FE633
	TypeU32      uint32 = 0x075E4E4F
	TypeS64      uint32 = 0xAF41354F
	TypeU64      uint32 = 0xA139E01F
	TypeF32      uint32 = 0x7515A207
	TypeF64      uint32 = 0xC609F663
	TypeString   uint32 = 0x8955583E
	TypeDeferred uint32 = 0xDEFE88ED
	TypeGDC      uint32 = 0x178842FE
)

// IsPrimitive reports whether the type hash is a fixed size scalar type.
func IsPrimitive(typeHash uint32) bool {
	switch typeHash {
	case TypeS8, TypeU8, TypeS16, TypeU16, TypeS32, TypeU32, TypeS64, TypeU64, TypeF32, TypeF64:
		return true
	}
	return false
}

// PrimitiveSize returns the byte width of a primitive type hash.
func PrimitiveSize(typeHash uint32) int {
	switch typeHash {
	case TypeS8, TypeU8:
		return 1
	case TypeS16, TypeU16:
		return 2
	case TypeS32, TypeU32, TypeF32:
		return 4
	case TypeS64, TypeU64, TypeF64:
		return 8
	}
	return 0
}

// MetaName gives the readable name of a metatype value.
func MetaName(meta uint32) string {
	switch meta {
	case MetaPrimitive:
		return "Primitive"
	case MetaStructure:
		return "Structure"
	case MetaPointer:
		return "Pointer"
	case MetaArray:
		return "Array"
	case MetaInlineArray:
		return "InlineArray"
	case MetaString:
		return "String"
	case MetaBitfield:
		return "Bitfield"
	case MetaEnumeration:
		return "Enumeration"
	case MetaStringHash:
		return "StringHash"
	}
	return "Unknown"
}

// TypeName renders a type hash for diagnostics.
func TypeName(typeHash uint32) string {
	switch typeHash {
	case TypeS8:
		return "sint08"
	case TypeU8:
		return "uint08"
	case TypeS16:
		return "sint16"
	case TypeU16:
		return "uint16"
	case TypeS32:
		return "sint32"
	case TypeU32:
		return "uint32"
	case TypeS64:
		return "sint64"
	case TypeU64:
		return "uint64"
	case TypeF32:
		return "float"
	case TypeF64:
		return "double"
	case TypeString:
		return "string"
	case TypeDeferred:
		return "DEFERRED"
	case TypeGDC:
		return "gdc"
	}
	return "0x" + strings.ToUpper(hex32(typeHash))
}

func hex32(v uint32) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		out[i] = digits[v&0xf]
		v >>= 4
	}
	return string(out)
}
