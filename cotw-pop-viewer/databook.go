package cotw

import (
	"cotw-pop-viewer/adf"
)

// dataBook is a decoded XLSBook: the game's spreadsheet data.
//
//	XLSBook  { Sheet: A[XLSSheet], Cell: A[XLSCell], StringData: A[String],
//	           ValueData: A[float], BoolData: A[uint8], DateData: A[String],
//	           ColorData: A[uint32], Attribute: A[XLSAttribute] }
//	XLSSheet { Cols u32, Rows u32, CellIndex A[uint32], Name string }
//	XLSCell  { Type u16, DataIndex u32, AttributeIndex u32 }
//
// CellIndex[row*Cols+col] indexes the book wide Cell array; Cell.Type selects
// the pool: 0 = ValueData, 1 = StringData, 2 = BoolData, 3 = ColorData.
type dataBook struct {
	sheets []*adf.Value
	cells  []*adf.Value
	text   []*adf.Value
	values []*adf.Value
	bools  []byte
	colors []*adf.Value
	names  map[*adf.Value]string
}

// XLSCell.Type values.
const (
	cellNumber = 0
	cellString = 1
	cellBool   = 2
	cellColor  = 3
)

func newDataBook(root *adf.Value) *dataBook {
	sheets := root.Field("Sheet")
	cells := root.Field("Cell")
	if sheets == nil || cells == nil {
		return nil
	}
	b := &dataBook{
		sheets: sheets.Elems,
		cells:  cells.Elems,
		names:  make(map[*adf.Value]string, len(sheets.Elems)),
	}
	if v := root.Field("StringData"); v != nil {
		b.text = v.Elems
	}
	if v := root.Field("ValueData"); v != nil {
		b.values = v.Elems
	}
	if v := root.Field("BoolData"); v != nil {
		b.bools = v.Bytes
	}
	if v := root.Field("ColorData"); v != nil {
		b.colors = v.Elems
	}
	return b
}

func (b *dataBook) sheetName(sheet *adf.Value) string {
	if name, ok := b.names[sheet]; ok {
		return name
	}
	name := ""
	if v := sheet.Field("Name"); v != nil {
		name = v.Str
	}
	b.names[sheet] = name
	return name
}

func (b *dataBook) sheet(name string) *adf.Value {
	for _, s := range b.sheets {
		if b.sheetName(s) == name {
			return s
		}
	}
	return nil
}

// dims returns a sheet's column and row count.
func (b *dataBook) dims(sheet *adf.Value) (cols, rows int) {
	if v := sheet.Field("Cols"); v != nil {
		if n, err := v.Int64(); err == nil {
			cols = int(n)
		}
	}
	if v := sheet.Field("Rows"); v != nil {
		if n, err := v.Int64(); err == nil {
			rows = int(n)
		}
	}
	return cols, rows
}

// cellAt resolves one cell to a string, float64 or bool.
func (b *dataBook) cellAt(sheet *adf.Value, row, col int) interface{} {
	cols, rows := b.dims(sheet)
	if row < 0 || col < 0 || row >= rows || col >= cols || cols == 0 {
		return nil
	}
	index := sheet.Field("CellIndex")
	if index == nil {
		return nil
	}
	pos := row*cols + col
	if pos >= len(index.Elems) {
		return nil
	}
	idx, err := index.Elems[pos].Int64()
	if err != nil || idx < 0 || int(idx) >= len(b.cells) {
		return nil
	}
	cell := b.cells[idx]
	typ, _ := cell.Field("Type").Int64()
	dataIdx, _ := cell.Field("DataIndex").Int64()
	if dataIdx < 0 {
		return nil
	}
	pick := func(pool []*adf.Value) interface{} {
		if int(dataIdx) >= len(pool) {
			return nil
		}
		v := pool[dataIdx]
		switch v.Kind {
		case adf.KindString:
			return v.Str
		case adf.KindFloat:
			return v.Float
		case adf.KindInt:
			return float64(v.Int)
		case adf.KindUint:
			return float64(v.Uint)
		}
		return nil
	}
	switch typ {
	case cellNumber:
		return pick(b.values)
	case cellString:
		return pick(b.text)
	case cellBool:
		if int(dataIdx) >= len(b.bools) {
			return nil
		}
		return b.bools[dataIdx] != 0
	case cellColor:
		return pick(b.colors)
	}
	return nil
}

// row returns the resolved cells of one row.
func (b *dataBook) row(sheet *adf.Value, row int) []interface{} {
	cols, _ := b.dims(sheet)
	out := make([]interface{}, cols)
	for c := 0; c < cols; c++ {
		out[c] = b.cellAt(sheet, row, c)
	}
	return out
}

// column finds a column by its header text, or -1.
func (b *dataBook) column(sheet *adf.Value, header string) int {
	cols, rows := b.dims(sheet)
	if rows == 0 {
		return -1
	}
	for c := 0; c < cols; c++ {
		if text, ok := b.cellAt(sheet, 0, c).(string); ok && text == header {
			return c
		}
	}
	return -1
}
