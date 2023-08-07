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
	"strings"
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

type ArrayProperty struct {
	Count       int32
	Items       []interface{}
	ElementType string
}

type StructPropertyField struct {
	Name  string
	Type  string
	Value interface{}
}
type StructProperty map[string]StructPropertyField
type StructArrayProperty struct {
	Type   string
	Values []StructProperty
}

type EnumProperty struct {
	EnumType  string
	EnumValue string
}

type TextProperty struct {
	GUID []byte
	Text string
}

type MapPropertyValue struct {
	Key   interface{}
	Value interface{}
}
type MapProperty struct {
	KeyType   string
	ValueType string
	Values    []MapPropertyValue
}

type ObjectProperty UObject

type PersistenceInfo struct {
	// struct FInfo
	// {
	// 	uint64 UniqueID = 0;
	// 	uint32 Offset = 0;
	// 	uint32 Length = 0;
	// };
	UniqueID uint64
	Offset   uint32
	Length   uint32
}

type PersistenceContainerHeader struct {
	// struct FHeader
	// {
	// 	uint32 Version = 0;
	// 	uint32 IndexOffset = 0;
	// 	uint32 DynamicOffset = 0;
	// };
	Version       uint32
	IndexOffset   uint32
	DynamicOffset uint32
}

type PersistenceContainer struct {
	Header PersistenceContainerHeader
	Info   []PersistenceInfo
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

// -- Read UE specific types

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

func readFName(r io.Reader) (FName, error) {
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

func readIntProperty(r io.ReadSeeker, raw bool) (int32, error) {
	if !raw {
		// 1 unknown byte?
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return 0, err
		}
	}

	// is it always int32?
	var varData int32
	err := binary.Read(r, binary.LittleEndian, &varData)
	if err != nil {
		return 0, err
	}

	return varData, nil
}

func readSoftObjectProperty(r io.ReadSeeker, raw bool) (string, error) {
	if !raw {
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return "", err
		}
	}

	return readFString(r)
}

func readBoolProperty(r io.ReadSeeker) (bool, error) {
	varData := make([]byte, 1)
	err := binary.Read(r, binary.LittleEndian, &varData)
	if err != nil {
		return false, err
	}
	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return false, err
	}
	return varData[0] == 1, nil
}

func readEnumProperty(r io.ReadSeeker) (EnumProperty, error) {
	enumTypeIndex, err := readInt[int16](r)
	if err != nil {
		return EnumProperty{}, err
	}
	enumType := names[enumTypeIndex]

	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return EnumProperty{}, err
	}

	// is it always 2 bytes
	enumValueIndex, err := readInt[int16](r)
	if err != nil {
		return EnumProperty{}, err
	}
	enumValue := names[enumValueIndex]

	return EnumProperty{
		EnumType:  enumType,
		EnumValue: enumValue,
	}, nil
}

func readMapProperty(r io.ReadSeeker) (MapProperty, error) {
	result := MapProperty{}

	keyIndex, err := readInt[int16](r)
	if err != nil {
		return result, err
	}
	result.KeyType = names[keyIndex]

	valueIndex, err := readInt[int16](r)
	if err != nil {
		return result, err
	}
	result.ValueType = names[valueIndex]

	_, err = r.Seek(5, io.SeekCurrent)
	if err != nil {
		return result, err
	}

	mapLength, err := readInt[int32](r)
	if err != nil {
		return result, err
	}

	values := make([]MapPropertyValue, mapLength)
	for i := 0; i < int(mapLength); i++ {
		// -- something is off here, because map does not contain variable size, it is key:value pairs one after another
		key, err := readProperty(r, result.KeyType, 0, true)
		if err != nil {
			return result, err
		}
		value, err := readProperty(r, result.ValueType, 0, true)
		if err != nil {
			return result, err
		}

		values[i] = struct{ Key, Value interface{} }{key, value}
	}
	result.Values = values

	return result, nil
}

func readStrProperty(r io.ReadSeeker, raw bool) (string, error) {
	if !raw {
		// unknown 1 byte
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return "", err
		}
	}

	strLength, err := readInt[int32](r)
	if err != nil {
		return "", err
	}

	if strLength == 0 {
		return "", nil
	}
	strData := make([]byte, strLength)
	_, err = r.Read(strData)
	if err != nil {
		return "", err
	}

	return string(strData), nil
}

func readNameProperty(r io.ReadSeeker, raw bool) (string, error) {
	if !raw {
		// unknown 1 byte
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return "", err
		}
	}

	nameIndex, err := readInt[int16](r)
	if err != nil {
		return "", err
	}
	name := names[nameIndex]

	return name, nil
}

func readFloatProperty(r io.ReadSeeker, raw bool) (float32, error) {
	if !raw {
		// unknown 1 byte
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return 0, err
		}
	}

	// is it always float32?
	var floatData float32
	err := binary.Read(r, binary.LittleEndian, &floatData)
	if err != nil {
		return 0, err
	}

	return floatData, nil
}

func readUInt64Property(r io.ReadSeeker, raw bool) (uint64, error) {
	if !raw {
		// unknown 1 byte
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return 0, err
		}
	}

	var uint64Data uint64
	err := binary.Read(r, binary.LittleEndian, &uint64Data)
	if err != nil {
		return 0, err
	}

	return uint64Data, nil
}

func readTextProperty(r io.ReadSeeker) (TextProperty, error) {
	// 10 unknown bytes
	_, err := r.Seek(10, io.SeekCurrent)
	if err != nil {
		return TextProperty{}, err
	}

	guidLength, err := readInt[int32](r)
	if err != nil {
		return TextProperty{}, err
	}

	// it is string, but I'm not sure where it's used, so read it as bytes
	guidData := make([]byte, guidLength)
	_, err = r.Read(guidData)
	if err != nil {
		return TextProperty{}, err
	}

	textLength, err := readInt[int32](r)
	if err != nil {
		return TextProperty{}, err
	}

	textData := make([]byte, textLength)
	_, err = r.Read(textData)
	if err != nil {
		return TextProperty{}, err
	}

	return TextProperty{Text: string(textData), GUID: guidData}, nil
}

func readObjectProperty(r io.ReadSeeker, raw bool) (ObjectProperty, error) {
	if !raw {
		// unknown 1 byte
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return ObjectProperty{}, err
		}
	}

	// I think for ObjectProperty objectIndex is 4bytes
	// but I'd like to keep it consistent with other properties
	objectIndex, err := readInt[int16](r)
	if err != nil {
		return ObjectProperty{}, err
	}
	_, err = r.Seek(2, io.SeekCurrent)
	if err != nil {
		return ObjectProperty{}, err
	}

	if objectIndex == -1 {
		// no object
		return ObjectProperty{}, nil
	}

	return ObjectProperty(objects[objectIndex]), nil
}

func readByteProperty(r io.Seeker) (byte, error) {
	// not sure why is it 8 bytes
	// i don't know what to return here
	// returning 0 for now
	_, err := r.Seek(4, io.SeekCurrent)
	if err != nil {
		return 0, err
	}

	return 0, nil
}

func readStructArrayProperty(r io.ReadSeeker, arrayLength int32) (StructArrayProperty, error) {
	result := StructArrayProperty{}

	// skip first 2 bytes - variable name again
	_, err := r.Seek(2, io.SeekCurrent)
	if err != nil {
		return result, err
	}

	// skip 2 more bytes - type again (StructProperty)
	_, err = r.Seek(2, io.SeekCurrent)
	if err != nil {
		return result, err
	}

	// skip 4 bytes (array size in bytes)
	_, err = r.Seek(4, io.SeekCurrent)
	if err != nil {
		return result, err
	}

	// 4 unknown bytes
	_, err = r.Seek(4, io.SeekCurrent)
	if err != nil {
		return result, err
	}

	innerTypeIndex, err := readInt[int16](r)
	if err != nil {
		return result, err
	}
	result.Type = names[innerTypeIndex]

	_, err = r.Seek(17, io.SeekCurrent)
	if err != nil {
		return result, err
	}

	values := make([]StructProperty, arrayLength)
	for i := 0; i < int(arrayLength); i++ {
		arrayElement := StructProperty{}
		for {
			variableNameIndex, err := readInt[int16](r)
			if err != nil {
				return result, err
			}
			variableName := names[variableNameIndex]

			if variableName == "None" {
				// end of struct
				break
			}

			varTypeIndex, err := readInt[int16](r)
			if err != nil {
				return result, err
			}
			varType := names[varTypeIndex]

			varSize, err := readInt[int32](r)
			if err != nil {
				return result, err
			}

			_, err = r.Seek(4, io.SeekCurrent)
			if err != nil {
				return result, err
			}

			value, err := readProperty(r, varType, varSize, false)
			if err != nil {
				return result, err
			}

			arrayElement[variableName] = StructPropertyField{
				Name:  variableName,
				Type:  varType,
				Value: value,
			}
		}
		values[i] = arrayElement
	}

	result.Values = values

	return result, nil
}

func readPersistenceBlob(r io.ReadSeeker) (interface{}, error) {
	// 0x4 size, 0x4 crc?, 0x4 version?
	version, err := readInt[uint32](r)
	if err != nil {
		return nil, err
	}
	if version == 4 {
		// PersistenceBlob inside PersistenceContainer
		// struct FHeader
		// {
		// 	uint32 Version = 0;
		// 	uint32 IndexOffset = 0;
		// 	uint32 DynamicOffset = 0;
		// };
		// We've already read version, so skip first 4 bytes and read offsets
		persistenceContainer := PersistenceContainer{
			Header: PersistenceContainerHeader{
				Version: version,
			},
			Info: []PersistenceInfo{},
		}
		persistenceContainer.Header.IndexOffset, err = readInt[uint32](r)
		if err != nil {
			return nil, err
		}
		persistenceContainer.Header.DynamicOffset, err = readInt[uint32](r)
		if err != nil {
			return nil, err
		}

		_, err = r.Seek(int64(persistenceContainer.Header.IndexOffset), io.SeekStart)
		if err != nil {
			return nil, err
		}

		// uint32 NumInfos;
		// Ar << NumInfos;
		// Info.SetNumUninitialized(NumInfos);

		// for (FInfo& CurInfo : Info)
		// {
		// 	Ar << CurInfo.UniqueID;
		// 	if (Header.Version < 2)
		// 	{
		// 		FName Unused;
		// 		Ar << Unused;
		// 	}
		// 	Ar << CurInfo.Offset;
		// 	Ar << CurInfo.Length;
		// }

		// uint32 NumDestroyed;
		// Ar << NumDestroyed;
		// Destroyed.SetNumUninitialized(NumDestroyed);

		// for (uint64& DestroyedID : Destroyed)
		// {
		// 	Ar << DestroyedID;
		// 	if (Header.Version < 2)
		// 	{
		// 		FName Unused;
		// 		Ar << Unused;
		// 	}
		// }

		numInfos, err := readInt[int32](r)
		if err != nil {
			return nil, err
		}

		persistenceContainer.Info = make([]PersistenceInfo, numInfos)

		for i := 0; i < int(numInfos); i++ {
			persistenceContainer.Info[i] = PersistenceInfo{}
			persistenceContainer.Info[i].UniqueID, err = readInt[uint64](r)
			if err != nil {
				return nil, err
			}
			if version < 2 { // in our case version is 4, so this code is basically unreachable
				_, err = readFString(r) // or readFName if this wont work
				if err != nil {
					return nil, err
				}
			}
			persistenceContainer.Info[i].Offset, err = readInt[uint32](r)
			if err != nil {
				return nil, err
			}
			persistenceContainer.Info[i].Length, err = readInt[uint32](r)
			if err != nil {
				return nil, err
			}
		}

		return persistenceContainer, nil
	}

	// else (for our case this is used in Profile)
	_, err = r.Seek(4, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	// Read strings table
	stringsTableOffset, err := readInt[int64](r)
	if err != nil {
		return nil, err
	}

	startPos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	_, err = r.Seek(stringsTableOffset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	stringsNum, err := readInt[int32](r)
	if err != nil {
		return nil, err
	}

	names = make([]string, stringsNum)

	for i := 0; i < int(stringsNum); i++ {
		stringData, err := readFString(r)
		if err != nil {
			return nil, err
		}
		names[i] = stringData
	}

	_, err = r.Seek(startPos, io.SeekStart)
	if err != nil {
		return nil, err
	}

	// skip 4 bytes - unknown, version (GUNFIRE_SAVEGAME_ARCHIVE_VERSION)
	_, err = r.Seek(4, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	// // base object
	// err = readBaseObject(r)
	// if err != nil {
	// 	return nil, err
	// }

	// return fmt.Sprintf("%s with size %d", "PersistenceBlob", varSize), nil
	return "PersistenceBlob", nil
}

var persistenceCounter = 0

func readProperty(r io.ReadSeeker, varType string, varSize int32, raw bool) (interface{}, error) {
	switch varType {
	case "IntProperty":
		{
			return readIntProperty(r, raw)
		}
	case "SoftObjectProperty":
		{
			return readSoftObjectProperty(r, raw)
		}
	case "BoolProperty":
		{
			return readBoolProperty(r)
		}
	case "MapProperty":
		{
			return readMapProperty(r)
		}
	case "EnumProperty":
		{
			return readEnumProperty(r)
		}
	case "StrProperty":
		{
			return readStrProperty(r, raw)
		}
	case "TextProperty":
		{
			return readTextProperty(r)
		}
	case "UInt64Property":
		{
			return readUInt64Property(r, raw)
		}
	case "FloatProperty":
		{
			return readFloatProperty(r, raw)
		}
	case "NameProperty":
		{
			return readNameProperty(r, raw)
		}

	case "ArrayProperty":
		{
			elementPropertyTypeIndex, err := readInt[int16](r)
			if err != nil {
				return nil, err
			}
			elementPropertyType := names[elementPropertyTypeIndex]

			_, err = r.Seek(1, io.SeekCurrent)
			if err != nil {
				return nil, err
			}

			arrayLength, err := readInt[int32](r)
			if err != nil {
				return nil, err
			}

			if elementPropertyType == "StructProperty" {
				return readStructArrayProperty(r, arrayLength)
			}

			result := ArrayProperty{
				ElementType: elementPropertyType,
				Count:       arrayLength,
				Items:       make([]interface{}, arrayLength),
			}
			for i := 0; i < int(arrayLength); i++ {
				elementValue, err := readProperty(r, elementPropertyType, varSize, true)
				if err != nil {
					return nil, err
				}
				result.Items[i] = elementValue
			}

			return result, nil
		}
	case "StructProperty":
		{
			propertyTypeIndex, err := readInt[int16](r)
			if err != nil {
				return nil, err
			}
			propertyType := names[propertyTypeIndex]
			// 17 bytes, not sure what they are
			_, err = r.Seek(17, io.SeekCurrent)
			if err != nil {
				return nil, err
			}

			if propertyType == "PersistenceBlob" {
				persistenceSize, err := readInt[int32](r)
				if err != nil {
					return nil, err
				}
				// read all the data
				// create new reader just for persistence blob
				// pass new reader to readPersistenceBlob
				persistenceBytes := make([]byte, persistenceSize)
				_, err = r.Read(persistenceBytes)
				if err != nil {
					return nil, err
				}

				SaveToFile(fmt.Sprintf("%d_persistence", persistenceCounter), "bin", persistenceBytes)
				persistenceCounter++

				persistenceReader := bytes.NewReader(persistenceBytes)

				return readPersistenceBlob(persistenceReader)
			}
			if propertyType == "SoftClassPath" {
				return readStrProperty(r, true)
			}

			fmt.Println("StructProperty", propertyType, varSize)
			return StructProperty{}, nil
		}
	case "ObjectProperty":
		{
			return readObjectProperty(r, raw)
		}
	case "ByteProperty":
		{
			return readByteProperty(r)
		}
	default:
		{
			fmt.Println("unknown varType", varType)
			varData := make([]byte, varSize)

			err := binary.Read(r, binary.LittleEndian, &varData)
			if err != nil {
				return nil, err
			}

			return varData, nil
		}
	}
}

func readObject(r io.ReadSeeker, objectStart int64, maxLength uint32) (map[string]interface{}, error) {
	variables := map[string]interface{}{}

	for {
		variableNameIndex, err := readInt[int16](r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return variables, err
		}
		variableName := names[variableNameIndex]

		varTypeIndex, err := readInt[int16](r)
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
			varSize, err := readInt[int32](r)
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

			varData, err := readProperty(r, varType, varSize, false)
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

func readBaseObject(r io.ReadSeeker) error {
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

	// Read all objects/classes
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
			objectName, err := readFName(r)
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

			fmt.Printf("Reading object '%s'\n", object["name"])

			if config.DEBUG_SAVE_DECRYPTED {
				objectBytes := make([]byte, objectLength)
				_, err = r.Seek(objectStart, io.SeekStart)
				if err != nil {
					return err
				}
				_, err = r.Read(objectBytes)
				if err != nil {
					return err
				}

				SaveToFile(strconv.Itoa(i)+"_"+strings.Trim(object["name"].(string), "\x00"), "bin", objectBytes)
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
				dataSize, err := readInt[int32](r)
				if err != nil {
					return err
				}
				_, err = r.Seek(int64(dataSize+5), io.SeekCurrent)
				if err != nil {
					return err
				}
			}
			serializedObject, err := readObject(r, objectStart, objectLength)
			if err != nil {
				return err
			}
			SaveToFile(strconv.Itoa(i)+"_"+strings.Trim(object["name"].(string), "\x00"), "json", serializedObject)

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

		isActor, err := readInt[uint8](r)
		if err != nil {
			return err
		}

		if isActor != 0 {
			// Not sure about it
			// componentNameIndex, err := readInt[int32](r)
			// if err != nil {
			// 	return err
			// }
			// componentName := names[componentNameIndex]
			_, err = r.Seek(4, io.SeekCurrent)
			if err != nil {
				return err
			}

			componentName, err := readFString(r)
			if err != nil {
				return err
			}

			fmt.Println("Actor", componentName)
		}
	}

	return nil
}

const PADDING_SIZE = 0x8

func readProfileFile(r io.ReadSeeker) error {
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

	_, err = r.Seek(stringsTableOffset, io.SeekStart)
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

	return readBaseObject(r)
}

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
