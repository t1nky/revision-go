package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"remnant-save-edit/config"
)

func saveJSON(name string, data []byte) error {
	return os.WriteFile("json/"+name+".json", data, 0644)
}

func saveBinary(name string, data []byte) error {
	return os.WriteFile("bin/"+name+".bin", data, 0644)
}

func createIfNotExist(name string) error {
	_, err := os.Stat(name)
	if errors.Is(err, os.ErrNotExist) {
		err := os.Mkdir(name, os.ModePerm)
		if err != nil {
			return err
		}
	}
	return err
}

func SaveToFile(name string, dataType string, data interface{}) error {
	switch dataType {
	case "json":
		if config.DEBUG_SAVE_JSON {
			err := createIfNotExist("json")
			if err != nil {
				return err
			}
			jsonObject, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				return err
			}
			return saveJSON(name, jsonObject)
		}
	case "bin":
		if config.DEBUG_SAVE_BINARY {
			err := createIfNotExist("binary")
			if err != nil {
				return err
			}
			return saveBinary(name, data.([]byte))
		}
	default:
		return fmt.Errorf("unknown file dataType: %s", dataType)
	}
	return nil
}
