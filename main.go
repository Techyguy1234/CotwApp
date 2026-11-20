package main

import (
	cotw "cotw-pop-viewer"
	"fmt"
	"log"
	"strconv"

	"github.com/AlecAivazis/survey/v2"
)

var save *cotw.Save
var err error

func main() {
	fmt.Println("Starting... (This may take a few minutes on the first time.)")
	save, err = cotw.ParseSave("", cotw.Options{})
	if err != nil {
		panic(err)
	}

	live, err := (*cotw.Game).OpenLive(&*save.GameData())
	defer live.Close()
	if err != nil {
		panic(err)
	}

	gameIsLive := live.Running()

	fmt.Println(save.Reserves[0].Species[0].Groups[0].NeedZones)

	live.Close()

	uiOptions := uiMode{}

	var reserveList []string
	var reserveListIndices []int

	for i := range save.Reserves {
		if save.Reserves[i].TotalAnimals != 0 {
			reserveList = append(reserveList, save.Reserves[i].Name+": "+strconv.FormatInt(int64(save.Reserves[i].TotalAnimals), 10)+" (Index: "+strconv.FormatInt(int64(save.Reserves[i].Index), 10)+")")
			reserveListIndices = append(reserveListIndices, i)
		}
	}

	var promptReserveResult int
	prompt := &survey.Select{
		Message: "Select reserve:",
		Options: reserveList,
	}
	err = survey.AskOne(prompt, &promptReserveResult)
	if err != nil {
		log.Fatal(err)
	}
	selectedReserve := reserveListIndices[promptReserveResult]

	showLiveAnimalPositions := false
	if gameIsLive {
		fmt.Println("Game is running")
		promptLiveAnimals := &survey.Confirm{
			Message: "Do you want live animal positions on the map?",
		}
		err = survey.AskOne(promptLiveAnimals, &showLiveAnimalPositions)
		if err != nil {
			log.Fatal(err)
		}
	}
	uiOptions.showLiveAnimalPositions = showLiveAnimalPositions

	var selectedReserveSpecies []string
	for i := range save.Reserves[selectedReserve].Species {
		selectedReserveSpecies = append(selectedReserveSpecies, save.Reserves[selectedReserve].Species[i].SpeciesName)
	}

	selectedReserveSpecies = append(selectedReserveSpecies, "Show All Species")

	var promptSpeciesResult int
	promptSpecies := &survey.Select{
		Message: "Select species:",
		Options: selectedReserveSpecies,
	}
	err = survey.AskOne(promptSpecies, &promptSpeciesResult)
	if err != nil {
		log.Fatal(err)
	}

	if promptSpeciesResult == len(selectedReserveSpecies)-1 {
		fmt.Println("Showing all species")
		uiOptions.showSpecificSpecies = false
	} else {
		uiOptions.showSpecificSpecies = true
		uiOptions.speciesName = selectedReserveSpecies[promptSpeciesResult]
	}

	var promptShowArrowsResult bool
	var promptShowArrowsResultInt int
	if uiOptions.showSpecificSpecies {
		promptArrows := &survey.Select{
			Message: "Show Group Movement Arrows",
			Options: []string{"No", "Yes"},
		}
		err := survey.AskOne(promptArrows, &promptShowArrowsResultInt)
		if err != nil {
			log.Fatal(err)
		}

		promptShowArrowsResult = false
		if promptShowArrowsResultInt == 1 {
			fmt.Println("Showing group movement arrows")
			promptShowArrowsResult = true
		}
	} else {
		promptShowArrowsResult = false
	}
	uiOptions.showGroupArrows = promptShowArrowsResult

	runEBitEn(selectedReserve, save, uiOptions)
}
