package main

import (
	"github.com/hajimehoshi/ebiten/v2"
)

type vec2 struct {
	x float64
	y float64
}

func getOffset(reserveIndex int) ebiten.GeoM {
	var outpost1PixelPosition vec2
	//var outpost2PixelPosition vec2
	var outpost1MapPosition vec2
	//var outpost2MapPosition vec2

	if reserveIndex == 0 { // Hirschfelden Hunting Reserve
		outpost1MapPosition = vec2{-4636.700, 12596.158}
		//outpost2MapPosition = vec2{-4463.089, 10971.438}
		outpost1PixelPosition = vec2{1649, 2014}
		//outpost2PixelPosition = vec2{1694, 1607}
	} else if reserveIndex == 1 { //Layton Lake District
		outpost1MapPosition = vec2{11228.529, 10107.859}
		outpost1PixelPosition = vec2{1525, 1754}
	} else if reserveIndex == 2 { //Medved-Taiga National Park
		outpost1MapPosition = vec2{-4723.573, -5763.791}
		outpost1PixelPosition = vec2{1952, 1691}
	} else if reserveIndex == 6 { //Yukon Valley Nature Reserve
		outpost1MapPosition = vec2{-5495.146, -5964.524}
		outpost1PixelPosition = vec2{1800, 1836}
	} else if reserveIndex == 9 { //Silver Ridge Peaks
		outpost1MapPosition = vec2{-1107.703, -5618.033}
		outpost1PixelPosition = vec2{2004, 2033}
	} else if reserveIndex == 12 { //Mississippi Acres Preserve
		outpost1MapPosition = vec2{6569.068, 11567.906}
		outpost1PixelPosition = vec2{644, 1941}
	} else if reserveIndex == 13 { //Revontuli Coast
		outpost1MapPosition = vec2{-5870.802, 12543.796}
		outpost1PixelPosition = vec2{1581, 2078}
	} else if reserveIndex == 14 { //New England Mountains
		outpost1MapPosition = vec2{11046.005, 10092.304}
		outpost1PixelPosition = vec2{1831, 1874}
	} else if reserveIndex == 16 { //Emerald Coast Australia
		outpost1MapPosition = vec2{-6431.029, -4883.352}
		outpost1PixelPosition = vec2{1592, 1978}
	} else if reserveIndex == 17 { //Sundarpatan Hunting Reserve
		outpost1MapPosition = vec2{-6135.571, 7754.018}
		outpost1PixelPosition = vec2{1610, 2065}
	} else if reserveIndex == 20 { //Torr Nan Sithean
		outpost1MapPosition = vec2{5579.706, -4347.336}
		outpost1PixelPosition = vec2{465, 2083}
	} else {
		panic("invalid reserveIndex offset calibration")
	}

	//pixelSpaceScale := math.Abs(outpost1PixelPosition.y - outpost2PixelPosition.y)
	//gameSpaceScale := math.Abs(outpost1MapPosition.y - outpost2MapPosition.y)
	//
	//fmt.Println(pixelSpaceScale)
	//fmt.Println(gameSpaceScale)

	t := ebiten.GeoM{}

	//scaleFactor := pixelSpaceScale / gameSpaceScale
	//fmt.Println(scaleFactor)

	//Scale factor should be same across maps
	scaleFactor := 0.250

	t.Scale(scaleFactor, scaleFactor)

	newOutpostScaledCoords := vec2{scaleFactor * outpost1MapPosition.x, scaleFactor * outpost1MapPosition.y}
	t.Translate(outpost1PixelPosition.x-newOutpostScaledCoords.x, outpost1PixelPosition.y-newOutpostScaledCoords.y)

	return t
}
