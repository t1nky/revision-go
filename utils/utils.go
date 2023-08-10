package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"revision-go/config"
)

func createIfNotExist(name string) error {
	_, err := os.Stat(name)
	if err != nil && os.IsNotExist(err) {
		return os.Mkdir(name, os.ModePerm)
	}
	return err
}

func saveJSON(foldername string, name string, data []byte) error {
	combinedPath := path.Join("json", foldername)
	err := createIfNotExist(combinedPath)
	if err != nil {
		return err
	}
	return os.WriteFile(path.Join(combinedPath, name+".json"), data, 0644)
}

func saveBinary(foldername string, name string, data []byte) error {
	combinedPath := path.Join("binary", foldername)
	err := createIfNotExist(combinedPath)
	if err != nil {
		return err
	}
	return os.WriteFile(path.Join(combinedPath, name+".bin"), data, 0644)
}

func SaveToFile(foldername string, name string, dataType string, data interface{}) error {
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
			return saveJSON(foldername, name, jsonObject)
		}
	case "bin":
		if config.DEBUG_SAVE_BINARY {
			err := createIfNotExist("binary")
			if err != nil {
				return err
			}
			return saveBinary(foldername, name, data.([]byte))
		}
	default:
		return fmt.Errorf("unknown file dataType: %s", dataType)
	}
	return nil
}
