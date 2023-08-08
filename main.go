package main

import (
	"log"
	"os"
	"remnant-save-edit/config"
	"remnant-save-edit/remnant"
	"remnant-save-edit/utils"
)

func main() {
	fileData, err := remnant.ReadData(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	result, err := remnant.ProcessData(fileData)
	if err != nil {
		log.Fatal(err)
	}

	utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, config.INPUT_FILE_NAME_WITHOUT_EXTENSION+"_processed", "json", result)
}
