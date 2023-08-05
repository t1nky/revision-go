package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"remnant-save-edit/config"
	"remnant-save-edit/remnant"
	"strconv"
)

// -- Types

type Int interface {
	int | uint | int8 | uint8 | int16 | uint16 | int32 | uint32 | int64 | uint64
}

type UObject map[string]interface{}

type FSaveGameClassPath struct {
	PackageName string
	AssetName   string
}

type FName struct {
	Index  uint16
	Number int32
}

// -- Globals

var names []string
var objects []UObject

var typeSizes = map[string]int{
	"IntProperty":  4,
	"NameProperty": 2,
}

// -- Utils

func readInt[T Int](r io.Reader) (T, error) {
	var value T
	err := binary.Read(r, binary.LittleEndian, &value)
	if err != nil {
		return 0, err
	}
	return value, nil
}

// -- Rean UE specific types

func readFString(r io.Reader) (string, error) {
	stringSize, err := readInt[int32](r)
	if err != nil {
		return "", err
	}
	stringData := make([]byte, stringSize)
	err = binary.Read(r, binary.LittleEndian, &stringData)
	if err != nil {
		return "", err
	}
	return string(stringData[:stringSize-1]), nil
}

func readFName(r io.Reader, names []string) (FName, error) {
	var index uint16
	err := binary.Read(r, binary.LittleEndian, &index)
	if err != nil {
		return FName{}, err
	}

	hasNumber := index & (1 << 15)

	if hasNumber != 0 {
		index = index &^ (1 << 15) // clearing the 15th bit

		var number int32
		err := binary.Read(r, binary.LittleEndian, &number)
		if err != nil {
			return FName{}, err
		}

		// assuming that names are stored with format: "name" or "name_number"
		return FName{Index: index, Number: number}, nil
	}

	// if no number part, return the name based on the index
	return FName{Index: index, Number: 0}, nil
}

// -- Main code

func readSaveGameClassPath(r io.Reader) (FSaveGameClassPath, error) {
	saveGameClassPath := FSaveGameClassPath{}
	var err error

	saveGameClassPath.PackageName, err = readFString(r)
	if err != nil {
		return saveGameClassPath, err
	}

	saveGameClassPath.AssetName, err = readFString(r)
	if err != nil {
		return saveGameClassPath, err
	}

	return saveGameClassPath, nil
}

func readArchetype(r io.Reader) (string, error) {
	stringHeader := make([]byte, 13)
	_, err := r.Read(stringHeader)
	if err != nil {
		return "", err
	}
	if stringHeader[2] != 0xC {
		return "", errors.New("invalid string header")
	}

	return readFString(r)
}

func readBoolProperty(r *bytes.Reader) (bool, error) {
	stringHeader := make([]byte, 12)
	_, err := r.Read(stringHeader)
	if err != nil {
		return false, err
	}
	var value uint8
	err = binary.Read(r, binary.LittleEndian, &value)
	if err != nil {
		return false, err
	}
	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return false, err
	}

	return value == 1, nil
}

func readIntProperty(r *bytes.Reader, varSize int32) ([]byte, error) {
	varData := make([]byte, varSize)
	_, err := r.Seek(5, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.LittleEndian, &varData)
	if err != nil {
		return nil, err
	}

	return varData, nil
}

func readSoftObjectProperty(r *bytes.Reader, varSize int32) (string, error) {
	_, err := r.Seek(5, io.SeekCurrent)
	if err != nil {
		return "", err
	}

	varData, err := readFString(r)

	return varData, nil
}

func readProperty(r *bytes.Reader, varType string, varSize int32) (interface{}, error) {
	// 0x1 = IntProperty
	if varType == "IntProperty" {
		varData, err := readIntProperty(r, varSize)
		if err != nil {
			return nil, err
		}
		return varData, nil
	}
	// 0xC = SoftObjectProperty
	if varType == "SoftObjectProperty" {
		varData, err := readSoftObjectProperty(r, varSize)
		if err != nil {
			return nil, err
		}
		return varData, nil
	}
	// 0x7 = BoolProperty
	if varType == "BoolProperty" {
		_, err := r.Seek(4, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		varData := make([]byte, 1)
		err = binary.Read(r, binary.LittleEndian, &varData)
		if err != nil {
			return nil, err
		}
		_, err = r.Seek(1, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		return varData[0] == 1, nil
	}
	// 0x3 = ArrayProperty
	if varType == "ArrayProperty" {
		_, err := r.Seek(4, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		// elementType is stringIndex of type
		elementTypeIndex, err := readInt[int16](r)
		if err != nil {
			return nil, err
		}
		elementType := names[elementTypeIndex]

		_, err = r.Seek(1, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		// arrayLength, err := readInt[int32](r)
		// if err != nil {
		// 	return nil, err
		// }

		// fmt.Println("array", elementType, arrayLength)
		// TODO: read each element based on elementType
		_, err = r.Seek(int64(varSize), io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		return map[string]interface{}{
			"elementType": elementType,
		}, nil

		// for i := 0; i < int(arrayLength); i++ {
		// 	_, err := readProperty(r, elementTypeIndex, varSize)
		// 	if err != nil {
		// 		return nil, err
		// 	}
		// }
	}
	// 0x11 = StructProperty
	if varType == "StructProperty" {
		// 10 00 - unknown
		// 11 00 - maybe stringIndex of StructProperty
		_, err := r.Seek(4, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		propertyTypeIndex, err := readInt[int16](r)
		if err != nil {
			return nil, err
		}
		propertyType := names[propertyTypeIndex]

		// 15 bytes, not sure what they are
		_, err = r.Seek(17, io.SeekCurrent)

		_, err = r.Seek(int64(varSize), io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		// _, err = r.Seek(17, io.SeekCurrent)
		// if err != nil {
		// 	return nil, err
		// }

		// structSize, err := readInt[int32](r)
		// if err != nil {
		// 	return nil, err
		// }

		// _, err = r.Seek(4, io.SeekCurrent)
		// if err != nil {
		// 	return nil, err
		// }
		// _, err = r.Seek(int64(structSize), io.SeekCurrent)
		// if err != nil {
		// 	return nil, err
		// }

		return propertyType, nil
	}
	// 0x47 = EnumProperty
	if varType == "EnumProperty" {
		_, err := r.Seek(4, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		enumTypeIndex, err := readInt[int16](r)
		if err != nil {
			return nil, err
		}
		enumType := names[enumTypeIndex]

		_, err = r.Seek(1, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		varData := make([]byte, varSize)
		enumValueIndex, err := r.Read(varData)
		if err != nil {
			return nil, err
		}
		enumValue := names[int16(enumValueIndex)]

		return map[string]interface{}{
			"enumType":  enumType,
			"enumValue": enumValue,
		}, nil
	}
	// 0xC4 = MapProperty
	if varType == "MapProperty" {
		_, err := r.Seek(4, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		keyIndex, err := readInt[int16](r)
		if err != nil {
			return nil, err
		}
		keyType := names[keyIndex]

		valueIndex, err := readInt[int16](r)
		if err != nil {
			return nil, err
		}
		valueType := names[valueIndex]

		_, err = r.Seek(1, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		_, err = r.Seek(int64(varSize), io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		// // unknown 5 bytes
		// _, err = r.Seek(5, io.SeekCurrent)
		// if err != nil {
		// 	return nil, err
		// }

		// mapLength, err := readInt[int32](r)
		// if err != nil {
		// 	return nil, err
		// }

		// for i := 0; i < int(mapLength); i++ {
		// -- something is off here, because map does not contain variable size, it is key:value pairs one after another
		// 	key, err := readProperty(r, keyType, keySize)
		// 	value, err := readProperty(r, valueIndex, valueSize)
		// }
		return map[string]interface{}{
			"keyType":   keyType,
			"valueType": valueType,
		}, nil
	}
	if varType == "NameProperty" {
		nameIndex, err := readInt[int16](r)
		if err != nil {
			return nil, err
		}
		name := names[nameIndex]

		return name, nil
	}
	if varType == "StrProperty" {
		_, err := r.Seek(5, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		strLength, err := readInt[int32](r)
		if err != nil {
			return nil, err
		}

		strData := make([]byte, strLength)
		_, err = r.Read(strData)
		if err != nil {
			return nil, err
		}

		return string(strData), nil
	}

	varData := make([]byte, varSize)

	_, err := r.Seek(5, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	err = binary.Read(r, binary.LittleEndian, &varData)
	if err != nil {
		return nil, err
	}

	return varData, nil
}

func readUSavedCharacter(r *bytes.Reader, objectStart int64, maxLength int64) error {
	variables := map[string]struct {
		varType string
		varData interface{}
	}{}

	for {
		stringIndex, err := readInt[int16](r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			} else {
				return err
			}
		}

		if names[stringIndex] == "None" {
			_, err = r.Seek(4, io.SeekCurrent)
			if err != nil {
				return err
			}
		} else {
			varTypeIndex, err := readInt[int16](r)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				} else {
					return err
				}
			}
			varType := names[varTypeIndex]

			varSize, err := readInt[int32](r)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				} else {
					return err
				}
			}

			varData, err := readProperty(r, varType, varSize)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				} else {
					return err
				}
			}

			// TODO: Add parsing based on varType

			varName := names[stringIndex]
			variables[varName] = struct {
				varType string
				varData interface{}
			}{varType: varType, varData: varData}
		}

		currentPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}

		if currentPos-objectStart >= maxLength {
			break
		}
	}

	fmt.Println(variables)

	return nil
}

func DeserializeObject(r *bytes.Reader, name string, objectStart int64, objectLength int64) error {
	if name == "SavedCharacter" {
		err := readUSavedCharacter(r, objectStart, objectLength)
		if err != nil {
			return err
		}
	}

	_, err := r.Seek(objectStart+objectLength, io.SeekStart)
	if err != nil {
		return err
	}

	return nil
}

func readBaseObject(r *bytes.Reader) error {
	objectIndexPos, err := readInt[int64](r)
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

	numUniqueObjects, err := readInt[int32](r)
	if err != nil {
		return err
	}

	// Assuming baseObject is an empty object.
	baseObject := UObject{"name": "BaseObject[SaveGame]"}

	objects = make([]UObject, numUniqueObjects)

	// Read all objects
	for i := 0; i < int(numUniqueObjects); i++ {
		wasLoaded, err := readInt[uint8](r)
		if err != nil {
			return err
		}

		objectName, err := readFString(r)
		if err != nil {
			return err
		}

		var object UObject
		if wasLoaded != 0 && i == 0 {
			object = baseObject
		} else {
			// FindObject and LoadObject logic is replaced with loading from a predefined map or creating a new empty object
			object = UObject{"name": objectName, "index": i}
		}

		if wasLoaded != 0 {
			objects[i] = object
		} else {
			objectName, err := readFName(r, names)
			if err != nil {
				return err
			}
			outerID, err := readInt[int32](r)
			if err != nil {
				return err
			}
			object = UObject{"name": names[objectName.Index], "index": objectName.Index, "outerId": outerID}
			objects[i] = object
		}
	}

	_, err = r.Seek(startPos, io.SeekStart)
	if err != nil {
		return err
	}

	for i := 0; i < len(objects); i++ {
		objectID, err := readInt[int32](r)
		if err != nil {
			return err
		}

		objectLength, err := readInt[uint32](r)
		if err != nil {
			return err
		}

		var object UObject
		if objectID >= 0 && objectID < int32(len(objects)) && objectLength > 0 {
			object = objects[objectID]

			objectStart, err := r.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}

			// Log information about the object (replace with your own logging function)
			fmt.Printf("Reading object '%s'\n", object["name"])

			// err = object.Deserialize(r)
			// if err != nil {
			// 	return err
			// }
			// TODO: Temporary solution for reading objects (code above)
			err = DeserializeObject(r, object["name"].(string), objectStart, int64(objectLength))
			if err != nil {
				return err
			}

			// Check if we've read all the data
			currentPos, err := r.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}

			if currentPos != objectStart+int64(objectLength) {
				// Log a warning (replace with your own logging function)
				fmt.Printf("Warning: Object '%s' didn't read all its data\n", object["name"])

				// Correct the data position
				_, err = r.Seek(objectStart+int64(objectLength), io.SeekStart)
				if err != nil {
					return err
				}
			}

			// object["bytes"] = objectBytes

			// if config.DEBUG_SAVE_DECRYPTED {
			// 	os.WriteFile(strconv.Itoa(i)+"_"+strings.Trim(object["name"].(string), "\x00")+".bin", objectBytes, 0644)
			// }
		} else {
			_, err = r.Seek(int64(objectLength), io.SeekCurrent)
			if err != nil {
				return err
			}
		}

		isActor, err := readInt[uint8](r)
		if err != nil {
			return err
		}

		if isActor != 0 {
			fmt.Println("Actor")
			return errors.New("Actor, not implemented yet")
			// actor := object.ToActor() // You'll have to define how this conversion works
			// err = readComponents(r, actor)
			// if err != nil {
			// 	return err
			// }
		}
	}

	return nil
}

const PADDING_SIZE = 0x8

func readProfileFile(r *bytes.Reader) error {
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
	saveGameClassPath, err := readSaveGameClassPath(r)
	if err != nil {
		return err
	}
	fmt.Println(saveGameClassPath)

	// Read strings table
	stringsTableOffset, err := readInt[int64](r)
	if err != nil {
		return err
	}

	startPos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	_, err = r.Seek(int64(stringsTableOffset), io.SeekStart)
	if err != nil {
		return err
	}

	stringsNum, err := readInt[int32](r)
	if err != nil {
		return err
	}

	names = make([]string, stringsNum)

	for i := 0; i < int(stringsNum); i++ {
		stringData, err := readFString(r)
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
	// version, err = readInt[int32](r) // version
	_, err = r.Seek(4, io.SeekCurrent)
	if err != nil {
		return err
	}

	// here comes get classes to load (in PreloadSave)
	// we ignore it, since it is not needed for our purposes

	// instead, we do:
	// USaveGame* SaveGame = NewObject<USaveGame>(GetTransientPackage(), SaveGameClass)
	// FSaveGameArchive Ar(MemoryReader);
	// Ar.ReadBaseObject(SaveGame); <-- this part

	return readBaseObject(r)
}

// TODO:
// 1. Create struct for each type
// 2. Create ToString method for each struct
// 3. Read into struct
// 4. Fix array, map and struct properties reading
// 5. Read BP_RemnantSaveGameProfile_C ?
// 6. Read save file
func main() {
	chunks, err := remnant.ReadSaveFile(os.Args[1])
	if err != nil {
		panic(err)
	}

	// insert header padding in the beginning
	headerPadding := make([]byte, PADDING_SIZE)
	combined := bytes.Join(chunks, []byte{})
	// add header padding back to the beginning
	// because file offsets are used in the save file
	// and they are relative to the beginning of the file (including header)
	combined = append(headerPadding, combined...)
	r := bytes.NewReader(combined)

	err = readProfileFile(r)
	if err != nil {
		panic(err)
	}

	if config.DEBUG_SAVE_DECRYPTED {
		filename := filepath.Base(os.Args[1])
		extension := filepath.Ext(filename)
		filenameWithoutExt := filename[0 : len(filename)-len(extension)]
		os.WriteFile(filenameWithoutExt+"_decrypted.bin", []byte(combined), 0644)

		for i, chunk := range chunks {
			os.WriteFile(filenameWithoutExt+"_decrypted_"+strconv.Itoa(i)+".bin", chunk, 0644)
		}
	}
}
