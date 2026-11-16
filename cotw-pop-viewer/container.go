package cotw

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"cotw-pop-viewer/adf"
)

// DecompressContainer unwraps COTW's "SAVE"/"COMP" container and returns the ADF
// body it holds. Both the save files and most of the game's assets use it:
//
//	0x00  "SAVE"
//	0x08  u32 decompressed size
//	0x10  "COMP"
//	0x14  u16/u16 version
//	0x18  u32 decompressed size
//	0x20  zlib stream
//
// The zlib stream's first 5 bytes are a compression descriptor; the ADF body
// (" FDA" ...) follows them.
func DecompressContainer(raw []byte) ([]byte, error) {
	if len(raw) < 32 {
		return nil, fmt.Errorf("cotw: container shorter than the 32 byte header")
	}
	head := raw[:32]
	if string(head[0:4]) != "SAVE" {
		return nil, fmt.Errorf("cotw: unexpected container magic %q (expected SAVE)", string(head[0:4]))
	}
	if string(head[16:20]) != "COMP" {
		return nil, fmt.Errorf("cotw: unsupported container %q (expected COMP)", string(head[16:20]))
	}
	return decompressZlibBody(raw[32:])
}

func decompressZlibBody(compressed []byte) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("cotw: zlib: %w", err)
	}
	defer zr.Close()

	body, readErr := io.ReadAll(zr)
	if readErr != nil && len(body) == 0 {
		return nil, fmt.Errorf("cotw: zlib: %w", readErr)
	}
	return stripCompressionPrefix(body)
}

// stripCompressionPrefix drops the 5 byte descriptor in front of the ADF body.
func stripCompressionPrefix(body []byte) ([]byte, error) {
	switch {
	case len(body) >= 9 && string(body[5:9]) == " FDA":
		return body[5:], nil
	case len(body) >= 4 && string(body[0:4]) == " FDA":
		return body, nil
	}
	return nil, fmt.Errorf("cotw: payload is not an ADF file")
}

// LoadADFFile reads a save file (or archive entry written to disk) and parses it.
func LoadADFFile(path string) (*adf.Adf, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cotw: read %s: %w", path, err)
	}
	body, err := DecompressContainer(raw)
	if err != nil {
		return nil, fmt.Errorf("cotw: %s: %w", path, err)
	}
	a, err := adf.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("cotw: %s: %w", path, err)
	}
	return a, nil
}

// parseADFImage parses a bare or zlib wrapped ADF image held in memory, the way
// archive entries appear.
func parseADFImage(data []byte) (*adf.Adf, error) {
	switch {
	case len(data) >= 4 && string(data[0:4]) == " FDA":
		return adf.Parse(data)
	case len(data) >= 32 && string(data[0:4]) == "SAVE" && string(data[16:20]) == "COMP":
		body, err := DecompressContainer(data)
		if err != nil {
			return nil, err
		}
		return adf.Parse(body)
	}
	return nil, fmt.Errorf("cotw: not an ADF image")
}

// containerSize reports the decompressed size recorded in a SAVE header.
func containerSize(head []byte) uint32 {
	if len(head) < 12 {
		return 0
	}
	return binary.LittleEndian.Uint32(head[8:12])
}
