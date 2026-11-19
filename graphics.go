package main

import (
	cotw "cotw-pop-viewer"
	"fmt"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"strconv"

	"github.com/hajimehoshi/ebiten/v2/vector"
	_ "golang.org/x/image/webp"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

type needZoneIcon struct {
	image     *ebiten.Image
	zoneType  int
	positionX float64
	positionY float64
	id        int
	species   string
}

type groupArrow struct {
	startXY vec2
	endXY   vec2
}

type uiMode struct {
	showSpecificSpecies bool
	speciesName         string
	showGroupArrows     bool
}

var reserveImages = []string{
	"Images/Reserves/Hirschfelden.jpg",
	"Images/Reserves/Layton-Lake.jpg",
	"Images/Reserves/Medved-Taiga.jpg",
	"",
	"",
	"",
	"Images/Reserves/Yukon-Valley.jpg",
	"",
	"",
	"Images/Reserves/Silver-Ridge-Peaks.jpg",
	"",
	"",
	"Images/Reserves/Mississippi.jpg",
	"Images/Reserves/Revontuli-Coast.jpg",
	"Images/Reserves/New-England-Mountains.jpg",
	"",
	"Images/Reserves/Emerald-Coast-Australia.jpg",
	"Images/Reserves/Sundarpatan-Hunting-Reserve.jpg",
	"",
	"",
	"Images/Reserves/Torr-Nan Sithean-Hunting-Estate.jpg",
}

type Game struct {
	reserveImg       *ebiten.Image
	camera           *Camera
	zones            *[]needZoneIcon
	zonesTransform   ebiten.GeoM
	displayOptions   uiMode
	shownGroupArrows []groupArrow
}

func (g *Game) Update() error {
	g.camera.Update()
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	camMatrix := g.camera.Matrix()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Concat(camMatrix)
	screen.DrawImage(g.reserveImg, op)

	for _, zone := range *g.zones {

		if g.displayOptions.showSpecificSpecies == true && zone.species != g.displayOptions.speciesName {
			continue
		}

		op2 := &ebiten.DrawImageOptions{}

		// shrink image without moving it
		bounds := zone.image.Bounds()
		halfSize := vec2{float64(bounds.Dx()) / 2, float64(bounds.Dy()) / 2}
		op2.GeoM.Translate(-halfSize.x, -halfSize.y)
		op2.GeoM.Scale(0.2, 0.2)

		op2.GeoM.Translate(zone.positionX, zone.positionY)

		op2.GeoM.Concat(g.zonesTransform)
		op2.GeoM.Concat(camMatrix)

		screen.DrawImage(zone.image, op2)

	}

	drawArrowBetweenMapPoints(camMatrix, g.zonesTransform, g.shownGroupArrows[0].startXY, g.shownGroupArrows[0].endXY, screen)

}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return outsideWidth, outsideHeight
}

func runEBitEn(reserve int, save *cotw.Save, mode uiMode) {
	fmt.Println("Starting EBitEngine...")
	fmt.Println("Loading reserve: " + save.Reserves[reserve].Name)
	bgImg, _, err := ebitenutil.NewImageFromFile(reserveImages[reserve])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Loading " + strconv.FormatInt(int64(len(save.Zones(reserve))), 10) + " need zones")
	var needZones []needZoneIcon

	feedImage, _, err := ebitenutil.NewImageFromFile("Images/Icons/FeedingZoneIcon.webp")
	if err != nil {
		log.Fatal(err)
	}
	drinkImage, _, err := ebitenutil.NewImageFromFile("Images/Icons/DrinkingZoneIcon.webp")
	if err != nil {
		log.Fatal(err)
	}
	restImage, _, err := ebitenutil.NewImageFromFile("Images/Icons/RestingZoneIcon.webp")
	if err != nil {
		log.Fatal(err)
	}

	for i, savedZone := range save.Zones(reserve) {
		zone := needZoneIcon{}
		zone.positionX = savedZone.Position.X
		zone.positionY = savedZone.Position.Z
		zone.id = int(savedZone.ID)
		zone.zoneType = int(savedZone.Type)
		zone.species = savedZone.SpeciesName
		if zone.zoneType == 1 {
			zone.image = feedImage
		} else if zone.zoneType == 2 {
			zone.image = drinkImage
		} else if zone.zoneType == 3 {
			zone.image = restImage
		} else {
			println("Unknown zone type!")
			println(zone.zoneType)
			zone.image = restImage //, _, _ = ebitenutil.NewImageFromFile()   //("Images/Icons/Unknown.png")
		}

		needZones = append(needZones, zone)
		if i > 10000 {
			break
		}
	}

	//Group arrows
	groupArrowList := []groupArrow{}
	position1 := save.Reserves[reserve].Species[0].Groups[0].NeedZones[0].Position
	position2 := save.Reserves[reserve].Species[0].Groups[0].NeedZones[1].Position
	groupArrowList = append(groupArrowList, groupArrow{vec2{position1.X, position1.Z}, vec2{position2.X, position2.Z}})

	ebiten.SetWindowSize(1280, 720)

	//Window Title
	windowTitle := ""
	if mode.showSpecificSpecies == false {
		windowTitle = "All Species on " + save.Reserves[reserve].Name
	} else {
		windowTitle = "Showing " + mode.speciesName + " on " + save.Reserves[reserve].Name
	}
	ebiten.SetWindowTitle(windowTitle)

	if err := ebiten.RunGame(&Game{reserveImg: bgImg, camera: NewCamera(), zones: &needZones, zonesTransform: getOffset(reserve), displayOptions: mode, shownGroupArrows: groupArrowList}); err != nil {
		log.Fatal(err)
	}
}

func drawArrowBetweenMapPoints(cameraGeoM ebiten.GeoM, zonesGeoM ebiten.GeoM, mapStart vec2, mapFinish vec2, screen *ebiten.Image) {
	mapSpacePath := vector.Path{}
	mapSpacePath.MoveTo(float32(mapStart.x), float32(mapStart.y))
	mapSpacePath.LineTo(float32(mapFinish.x), float32(mapFinish.y))
	//mapSpacePath.LineTo(-float32(mapStart.y), float32(mapStart.x))

	combinedGeoM := ebiten.GeoM{}
	combinedGeoM.Concat(zonesGeoM)
	combinedGeoM.Concat(cameraGeoM)

	pixelSpacePath := vector.Path{}
	pixelSpacePath.AddPath(&mapSpacePath, &vector.AddPathOptions{GeoM: combinedGeoM})

	strokeOp := &vector.StrokeOptions{Width: 1}
	drawOp := &vector.DrawPathOptions{AntiAlias: true}
	drawOp.ColorScale.ScaleWithColor(color.RGBA{R: 255, G: 0, B: 0, A: 255})

	vector.StrokePath(screen, &pixelSpacePath, strokeOp, drawOp)
}
