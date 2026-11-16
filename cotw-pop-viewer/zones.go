package cotw

import (
	"cotw-pop-viewer/adf"
)

// Need zone and spawn-area geometry, read from the installed game.
//
// The game does not keep need zones in a need-zone asset; it lays them out in
// its spawn bitmaps (bitmap_data.adf assets). Each such asset holds layers of
// rectangular "bitmap areas":
//
//	BitmapArea: key u32, X u32, Y u32, Size u32, Tiles[], AabbMin[3]f32, AabbMax[3]f32
//
// Layer 0 of a reserve's bitmap holds the spawn areas that population groups
// reference by id (Group.SpawnAreaID). Layers 1..3 hold the need zones, one
// layer per need zone type, and each area's key is the need zone id the save
// uses in Group.NeedZonePathGuids. The area's bounding box is the zone's volume,
// so the box centre is the zone position the save stores for discovered zones.
//
// Verified against the save: for every discovered zone that appears in these
// bitmaps, the box centre equals the saved position and the layer index equals
// the saved NeedType.

// ZoneGeometry is the shipped geometry of one need zone or spawn area.
type ZoneGeometry struct {
	// ID is the need zone id (layers 1..3) or the spawn area id (layer 0).
	ID uint32 `json:"id"`
	// Type is the need zone type, i.e. the bitmap layer the area came from
	// (1..3). Zero for spawn areas.
	Type int32 `json:"type,omitempty"`
	// Position is the centre of the area's bounding box, in reserve world
	// metres (Y is elevation).
	Position Vec3 `json:"position"`
	// Extent is the size of the bounding box along each axis, in metres.
	Extent Vec3 `json:"extent"`
}

// extractBitmapZones reads one BitmapData instance and records the geometry of
// its areas. Layer 0 is spawn areas; later layers are need zones of that type.
func extractBitmapZones(v *adf.Value, out *scanResult) {
	layers := v.Field("Layers")
	if layers == nil {
		return
	}
	for layer, layerValue := range layers.Elems {
		if layerValue == nil {
			continue
		}
		areas := layerValue.Field("Areas")
		if areas == nil || areas.Len() == 0 {
			continue
		}
		for _, area := range areas.Elems {
			if area == nil || area.Kind != adf.KindStruct {
				continue
			}
			geom, ok := areaGeometry(area)
			if !ok {
				continue
			}
			if layer == 0 {
				out.spawnAreas = append(out.spawnAreas, geom)
			} else {
				geom.Type = int32(layer)
				out.zoneGeom = append(out.zoneGeom, geom)
			}
		}
	}
}

// areaGeometry reads the id and bounding box of one BitmapArea.
func areaGeometry(area *adf.Value) (ZoneGeometry, bool) {
	var geom ZoneGeometry
	key := area.Field("key")
	if key == nil {
		return geom, false
	}
	id, err := key.Uint32()
	if err != nil {
		return geom, false
	}
	min := floatTriple(area.Field("AabbMin"))
	max := floatTriple(area.Field("AabbMax"))
	// Reject degenerate boxes: every zone has a real volume in the world.
	if max.X == 0 && max.Y == 0 && max.Z == 0 {
		return geom, false
	}
	geom.ID = id
	geom.Position = Vec3{
		X: (min.X + max.X) / 2,
		Y: (min.Y + max.Y) / 2,
		Z: (min.Z + max.Z) / 2,
	}
	geom.Extent = Vec3{
		X: max.X - min.X,
		Y: max.Y - min.Y,
		Z: max.Z - min.Z,
	}
	return geom, true
}

// floatTriple reads a fixed array of three floats.
func floatTriple(v *adf.Value) Vec3 {
	var out Vec3
	if v == nil {
		return out
	}
	get := func(i int) float64 {
		if i >= len(v.Elems) {
			return 0
		}
		e := v.Elems[i]
		if e == nil {
			return 0
		}
		f, err := e.Float64()
		if err != nil {
			return 0
		}
		return f
	}
	out.X = get(0)
	out.Y = get(1)
	out.Z = get(2)
	return out
}

// --- game accessors -------------------------------------------------------

// ZoneGeometry returns the shipped geometry of a need zone id, whether or not
// the player has discovered that zone.
func (g *Game) ZoneGeometry(id uint32) (ZoneGeometry, bool) {
	geom, ok := g.zoneGeom[id]
	return geom, ok
}

// ZoneGeometries returns every need zone the game ships geometry for.
func (g *Game) ZoneGeometries() []ZoneGeometry {
	out := make([]ZoneGeometry, 0, len(g.zoneGeom))
	for _, geom := range g.zoneGeom {
		out = append(out, geom)
	}
	return out
}

// SpawnAreaGeometry returns the shipped geometry of a group spawn area id (the
// value a Group carries in SpawnAreaID).
func (g *Game) SpawnAreaGeometry(id int32) (ZoneGeometry, bool) {
	geom, ok := g.spawnAreas[uint32(id)]
	return geom, ok
}

// ZoneGeometryCollisions reports how many zone ids had more than one record in
// the shipped bitmaps.
func (g *Game) ZoneGeometryCollisions() int { return g.zoneCollisions }

// ZoneCandidates returns every shipped record for a zone id. Most ids have
// exactly one; ids that appear in several reserves' layouts have several.
func (g *Game) ZoneCandidates(id uint32) []ZoneGeometry {
	if list := g.zoneCandidates[id]; len(list) > 1 {
		return list
	}
	if geom, ok := g.zoneGeom[id]; ok {
		return []ZoneGeometry{geom}
	}
	return nil
}
