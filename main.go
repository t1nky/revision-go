package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"remnant-save-edit/config"
	"remnant-save-edit/memory"
	"remnant-save-edit/remnant"
	"remnant-save-edit/ue"
	"remnant-save-edit/utils"
	"strconv"
	"strings"
)

// -- Globals

func readTopLevelAssetPath(r io.Reader) (ue.FTopLevelAssetPath, error) {
	topLevelAssetPath := ue.FTopLevelAssetPath{}
	var err error

	topLevelAssetPath.PackageName, err = ue.ReadFString(r)
	if err != nil {
		return topLevelAssetPath, err
	}

	topLevelAssetPath.AssetName, err = ue.ReadFString(r)
	if err != nil {
		return topLevelAssetPath, err
	}

	return topLevelAssetPath, nil
}

func readObject(r io.ReadSeeker, objectStart int64, maxLength uint32, names []string, objects []ue.UObject) (map[string]interface{}, error) {
	variables := map[string]interface{}{}

	for {
		variableNameIndex, err := memory.ReadInt[int16](r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return variables, err
		}
		variableName := names[variableNameIndex]

		varTypeIndex, err := memory.ReadInt[int16](r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return variables, err

		}
		varType := names[varTypeIndex]

		if variableName == "None" {
			_, err = r.Seek(2, io.SeekCurrent) // IT WAS 4 BEFORE PARSING PERSISTENCE BLOB
			if err != nil {
				return variables, err
			}
		} else {
			varSize, err := memory.ReadInt[int32](r)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return variables, err
			}

			// unknown data
			_, err = r.Seek(4, io.SeekCurrent)
			if err != nil {
				return variables, err
			}

			varData, err := remnant.ReadProperty(r, varType, varSize, names, objects, false)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return variables, err
			}

			variables[variableName] = varData
		}

		currentPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return variables, err
		}

		if currentPos-objectStart >= int64(maxLength) {
			break
		}
	}

	return variables, nil
}

func readBaseObject(r io.ReadSeeker, names []string) error {
	var objects []ue.UObject

	objectIndexPos, err := memory.ReadInt[int64](r)
	if err != nil {
		return err
	}

	startPos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	_, err = r.Seek(objectIndexPos, io.SeekStart)
	if err != nil {
		return err
	}

	numUniqueObjects, err := memory.ReadInt[int32](r)
	if err != nil {
		return err
	}

	// Assuming baseObject is an empty object.
	baseObject := ue.UObject{"name": "BaseObject[SaveGame]"}

	objects = make([]ue.UObject, numUniqueObjects)

	// Read all objects/classes
	for i := 0; i < int(numUniqueObjects); i++ {
		wasLoaded, err := memory.ReadInt[uint8](r)
		if err != nil {
			return err
		}

		objectName, err := ue.ReadFString(r)
		if err != nil {
			return err
		}

		var object ue.UObject
		if wasLoaded != 0 && i == 0 {
			object = baseObject
		} else {
			// FindObject and LoadObject logic is replaced with loading from a predefined map or creating a new empty object
			object = ue.UObject{"name": objectName, "index": i}
		}

		if wasLoaded != 0 {
			objects[i] = object
		} else {
			objectName, err := ue.ReadFName(r)
			if err != nil {
				return err
			}
			outerID, err := memory.ReadInt[int32](r)
			if err != nil {
				return err
			}
			object = ue.UObject{"name": names[objectName.Index], "index": objectName.Index, "outerId": outerID}
			objects[i] = object
		}
	}

	_, err = r.Seek(startPos, io.SeekStart)
	if err != nil {
		return err
	}

	for i := 0; i < len(objects); i++ {
		objectID, err := memory.ReadInt[int32](r)
		if err != nil {
			return err
		}

		objectLength, err := memory.ReadInt[uint32](r)
		if err != nil {
			return err
		}

		var object ue.UObject
		if objectID >= 0 && objectID < int32(len(objects)) && objectLength > 0 {
			object = objects[objectID]

			objectStart, err := r.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}

			fmt.Printf("Reading object '%s'\n", object["name"])

			if config.DEBUG_SAVE_BINARY {
				objectBytes := make([]byte, objectLength)
				_, err = r.Seek(objectStart, io.SeekStart)
				if err != nil {
					return err
				}
				_, err = r.Read(objectBytes)
				if err != nil {
					return err
				}

				utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, strconv.Itoa(i)+"_object_"+strings.Trim(object["name"].(string), "\x00"), "bin", objectBytes)
				_, err = r.Seek(objectStart, io.SeekStart)
				if err != nil {
					return err
				}
			}

			// hack for reading BP header
			if strings.HasPrefix(object["name"].(string), "BP_") {
				_, err = r.Seek(4, io.SeekCurrent)
				if err != nil {
					return err
				}
				dataSize, err := memory.ReadInt[int32](r)
				if err != nil {
					return err
				}
				_, err = r.Seek(int64(dataSize+5), io.SeekCurrent)
				if err != nil {
					return err
				}
			}
			serializedObject, err := readObject(r, objectStart, objectLength, names, objects)
			if err != nil {
				return err
			}
			utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, strconv.Itoa(i)+"_"+strings.Trim(object["name"].(string), "\x00"), "json", serializedObject)

			// Check if we've read all the data
			currentPos, err := r.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}

			if currentPos != objectStart+int64(objectLength) {
				fmt.Printf("Warning: Object '%s' didn't read all its data (%d /%d; length: %d)\n", object["name"], currentPos, objectStart+int64(objectLength), objectLength)

				// Correct the data position
				_, err = r.Seek(objectStart+int64(objectLength), io.SeekStart)
				if err != nil {
					return err
				}
			}
		} else {
			_, err = r.Seek(int64(objectLength), io.SeekCurrent)
			if err != nil {
				return err
			}
		}

		isActor, err := memory.ReadInt[uint8](r)
		if err != nil {
			return err
		}

		if isActor != 0 {
			// Not sure about it
			// componentNameIndex, err := memory.ReadInt[int32](r)
			// if err != nil {
			// 	return err
			// }
			// componentName := names[componentNameIndex]
			_, err = r.Seek(4, io.SeekCurrent)
			if err != nil {
				return err
			}

			componentName, err := ue.ReadFString(r)
			if err != nil {
				return err
			}

			fmt.Println("Actor", componentName)
		}
	}

	return nil
}

const PADDING_SIZE = 0x8

func readDataHeaderAndClassPath(r io.ReadSeeker) error {
	// skip crc, savedSize, SavegameFileVersion
	_, err := r.Seek(PADDING_SIZE, io.SeekStart)
	if err != nil {
		return err
	}

	// Read build number
	chunkHeader := remnant.ChunkHeader{}
	err = binary.Read(r, binary.LittleEndian, &chunkHeader)
	if err != nil {
		return err
	}

	// read SaveGameClassPath
	saveGameClassPath, err := readTopLevelAssetPath(r)
	if err != nil {
		return err
	}
	fmt.Println(saveGameClassPath)

	return nil
}

func processData(r io.ReadSeeker) error {
	var names []string

	// Read strings table
	stringsTableOffset, err := memory.ReadInt[int64](r)
	if err != nil {
		return err
	}

	startPos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	_, err = r.Seek(stringsTableOffset, io.SeekStart)
	if err != nil {
		return err
	}

	stringsNum, err := memory.ReadInt[int32](r)
	if err != nil {
		return err
	}

	names = make([]string, stringsNum)

	for i := 0; i < int(stringsNum); i++ {
		stringData, err := ue.ReadFString(r)
		if err != nil {
			return err
		}
		names[i] = stringData
	}

	_, err = r.Seek(startPos, io.SeekStart)
	if err != nil {
		return err
	}

	// read version
	// version, err = memory.ReadInt[int32](r) // version
	_, err = r.Seek(4, io.SeekCurrent)
	if err != nil {
		return err
	}

	return readBaseObject(r, names)
}

func prepareData(chunks [][]byte) io.ReadSeeker {
	// insert header padding in the beginning
	headerPadding := make([]byte, PADDING_SIZE)
	combined := bytes.Join(chunks, []byte{})
	// add header padding back to the beginning
	// because file offsets are used in the save file
	// and they are relative to the beginning of the file (including header)
	combined = append(headerPadding, combined...)

	utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, config.INPUT_FILE_NAME_WITHOUT_EXTENSION+"_decrypted", "bin", combined)

	r := bytes.NewReader(combined)

	return r
}

func main() {
	chunks, err := remnant.Decompress(os.Args[1])
	if err != nil {
		panic(err)
	}

	r := prepareData(chunks)

	err = readDataHeaderAndClassPath(r)
	if err != nil {
		panic(err)
	}
	err = processData(r)
	if err != nil {
		panic(err)
	}
}
