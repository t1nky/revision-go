package main

import (
	"bytes"
	"log"
	"os"
	"revision-go/config"
	"revision-go/remnant"
	"revision-go/utils"
)

func main() {
	fileData, err := remnant.ReadData(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	result, err := remnant.ReadSaveArchive(bytes.NewReader(fileData))
	if err != nil {
		log.Fatal(err)
	}

	utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, config.INPUT_FILE_NAME_WITHOUT_EXTENSION+"_processed", "json", result)
}
