package cotw

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// Need zone geometry is far too large for the JSON cache: the install ships
// hundreds of thousands of areas. It is therefore kept in a compact binary
// sidecar next to the cache file, as fixed 32 byte records:
//
//	u32 id | u8 type | 3 pad | f32 x,y,z | f32 extent.x,extent.y,extent.z
//
// type 0 marks a spawn area; 1..3 are need zone types.
const (
	zoneSidecarSuffix = ".zones"
	zoneSidecarFormat = 1
	zoneRecordSize    = 32
)

var zoneSidecarMagic = [4]byte{'Z', 'O', 'N', 'E'}

// zoneSidecarPath is the geometry file that belongs to a cache file.
func zoneSidecarPath(cachePath string) string { return cachePath + zoneSidecarSuffix }

// writeZoneSidecar writes every geometry record, ordered by id so the file is
// deterministic.
func writeZoneSidecar(cachePath string, geoms []ZoneGeometry) error {
	path := zoneSidecarPath(cachePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	records := append([]ZoneGeometry(nil), geoms...)
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })

	buf := make([]byte, 0, 8+4+len(records)*zoneRecordSize)
	buf = append(buf, zoneSidecarMagic[:]...)
	buf = binary.LittleEndian.AppendUint32(buf, zoneSidecarFormat)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(records)))
	rec := make([]byte, zoneRecordSize)
	for _, geom := range records {
		binary.LittleEndian.PutUint32(rec[0:], geom.ID)
		rec[4] = byte(geom.Type)
		rec[5], rec[6], rec[7] = 0, 0, 0
		binary.LittleEndian.PutUint32(rec[8:], math.Float32bits(float32(geom.Position.X)))
		binary.LittleEndian.PutUint32(rec[12:], math.Float32bits(float32(geom.Position.Y)))
		binary.LittleEndian.PutUint32(rec[16:], math.Float32bits(float32(geom.Position.Z)))
		binary.LittleEndian.PutUint32(rec[20:], math.Float32bits(float32(geom.Extent.X)))
		binary.LittleEndian.PutUint32(rec[24:], math.Float32bits(float32(geom.Extent.Y)))
		binary.LittleEndian.PutUint32(rec[28:], math.Float32bits(float32(geom.Extent.Z)))
		buf = append(buf, rec...)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readZoneSidecar loads the geometry sidecar for a cache file.
func readZoneSidecar(cachePath string) ([]ZoneGeometry, error) {
	data, err := os.ReadFile(zoneSidecarPath(cachePath))
	if err != nil {
		return nil, err
	}
	if len(data) < 12 || string(data[0:4]) != string(zoneSidecarMagic[:]) {
		return nil, fmt.Errorf("cotw: %s is not a zone geometry file", filepath.Base(zoneSidecarPath(cachePath)))
	}
	if format := binary.LittleEndian.Uint32(data[4:]); format != zoneSidecarFormat {
		return nil, fmt.Errorf("cotw: zone geometry format %d, expected %d", format, zoneSidecarFormat)
	}
	count := int(binary.LittleEndian.Uint32(data[8:]))
	if count < 0 || len(data) < 12+count*zoneRecordSize {
		return nil, fmt.Errorf("cotw: zone geometry file is truncated")
	}
	out := make([]ZoneGeometry, 0, count)
	f32 := func(b []byte) float64 {
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
	}
	for i := 0; i < count; i++ {
		rec := data[12+i*zoneRecordSize : 12+(i+1)*zoneRecordSize]
		out = append(out, ZoneGeometry{
			ID:   binary.LittleEndian.Uint32(rec[0:]),
			Type: int32(rec[4]),
			Position: Vec3{
				X: f32(rec[8:]), Y: f32(rec[12:]), Z: f32(rec[16:]),
			},
			Extent: Vec3{
				X: f32(rec[20:]), Y: f32(rec[24:]), Z: f32(rec[28:]),
			},
		})
	}
	return out, nil
}

// setZoneGeometry fills the geometry maps from a flat record list.
//
// A zone id can appear more than once in the shipped bitmaps: the same id is
// used in several reserves' layouts, at genuinely different places. All records
// are therefore kept, and Save.Zones picks the candidate nearest the spawn
// areas of the groups that reference the zone.
func (g *Game) setZoneGeometry(geoms []ZoneGeometry) {
	g.zoneAll = append([]ZoneGeometry(nil), geoms...)
	g.zoneGeom = make(map[uint32]ZoneGeometry, len(geoms))
	g.spawnAreas = make(map[uint32]ZoneGeometry, 4096)
	g.zoneCandidates = make(map[uint32][]ZoneGeometry, 64)
	g.zoneCollisions = 0
	put := func(m map[uint32]ZoneGeometry, geom ZoneGeometry) {
		if old, ok := m[geom.ID]; ok {
			g.zoneCollisions++
			if g.zoneCandidates[geom.ID] == nil {
				g.zoneCandidates[geom.ID] = []ZoneGeometry{old}
			}
			g.zoneCandidates[geom.ID] = append(g.zoneCandidates[geom.ID], geom)
			// Keep the largest volume as the default answer.
			if geom.Extent.X+geom.Extent.Y+geom.Extent.Z <= old.Extent.X+old.Extent.Y+old.Extent.Z {
				return
			}
		}
		m[geom.ID] = geom
	}
	for _, geom := range geoms {
		if geom.Type == 0 {
			put(g.spawnAreas, geom)
		} else {
			put(g.zoneGeom, geom)
		}
	}
}

// allZoneGeometry returns every scanned record for writing to the sidecar,
// duplicates included so the candidate sets survive a cache round trip.
func (g *Game) allZoneGeometry() []ZoneGeometry { return g.zoneAll }
