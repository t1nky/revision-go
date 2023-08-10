package remnant

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"revision-go/memory"
	"revision-go/ue"
)

const (
	REMNANT_SAVE_GAME_PROFILE = "/Game/_Core/Blueprints/Base/BP_RemnantSaveGameProfile"
	REMNANT_SAVE_GAME         = "/Game/_Core/Blueprints/Base/BP_RemnantSaveGame"
)

type Property struct {
	Name  string
	Index uint32
	Type  string
	Size  uint32
	Value interface{}
}

type ObjectProperty struct {
	ClassName string
}

func readObjectProperty(r io.ReadSeeker, saveData *SaveData, raw bool) (ObjectProperty, error) {
	if !raw {
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return ObjectProperty{}, err
		}
	}

	objectIndex, err := memory.ReadInt[int32](r)
	if err != nil {
		return ObjectProperty{}, err
	}

	if objectIndex == -1 {
		return ObjectProperty{}, nil
	}

	return ObjectProperty{
		ClassName: saveData.ObjectIndex[objectIndex].ObjectPath,
	}, nil
}

type ByteProperty interface {
	byte | string
}

func readByteProperty(r io.ReadSeeker, saveData *SaveData, raw bool) (interface{}, error) {
	if raw {
		value, err := memory.ReadInt[uint8](r)
		if err != nil {
			return 0, err
		}

		return value, nil
	}
	name, err := readName(r, saveData)
	if err != nil {
		return 0, err
	}
	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return 0, err
	}

	if name == "None" {
		byteData, err := memory.ReadInt[uint8](r)
		if err != nil {
			return 0, err
		}

		return byteData, nil
	}

	enumName, err := readName(r, saveData)
	if err != nil {
		return 0, err
	}
	return enumName, nil
}

type ArrayProperty struct {
	Count       uint32
	Items       []interface{}
	ElementType string
}

func readArrayProperty(r io.ReadSeeker, saveData *SaveData, varSize uint32) (interface{}, error) {
	elementsType, err := readName(r, saveData)
	if err != nil {
		return ArrayProperty{}, err
	}

	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return ArrayProperty{}, err
	}

	arrayLength, err := memory.ReadInt[uint32](r)
	if err != nil {
		return ArrayProperty{}, err
	}

	if elementsType == "StructProperty" {
		arrayStructProperty, err := readArrayStructHeader(r, saveData)
		if err != nil {
			return ArrayProperty{}, err
		}
		arrayStructProperty.Count = arrayLength

		items := make([]StructProperty, arrayLength)
		for i := 0; i < int(arrayLength); i++ {
			value, err := readStructPropertyData(r, arrayStructProperty.ElementType, saveData)
			if err != nil {
				return ArrayProperty{}, err
			}
			items[i] = StructProperty{
				Name:  arrayStructProperty.ElementType,
				Value: value,
				GUID:  arrayStructProperty.GUID,
				Size:  varSize,
			}

		}
		arrayStructProperty.Items = items
		return arrayStructProperty, nil
	}

	result := ArrayProperty{
		ElementType: elementsType,
		Count:       arrayLength,
		Items:       make([]interface{}, arrayLength),
	}
	for i := 0; i < int(arrayLength); i++ {
		elementValue, err := getPropertyValue(r, elementsType, varSize, saveData, true)
		if err != nil {
			return ArrayProperty{}, err
		}
		result.Items[i] = elementValue
	}

	return result, nil
}

func readArrayStructHeader(r io.ReadSeeker, saveData *SaveData) (ArrayStructProperty, error) {
	// skip first 2 bytes - variable name again
	_, err := r.Seek(2, io.SeekCurrent)
	if err != nil {
		return ArrayStructProperty{}, err
	}

	// skip 2 more bytes - type again (StructProperty)
	_, err = r.Seek(2, io.SeekCurrent)
	if err != nil {
		return ArrayStructProperty{}, err
	}

	// skip 4 bytes (array size in bytes)
	size, err := memory.ReadInt[uint32](r)
	if err != nil {
		return ArrayStructProperty{}, err
	}

	// skip 4 bytes - index
	_, err = r.Seek(4, io.SeekCurrent)
	if err != nil {
		return ArrayStructProperty{}, err
	}

	elementType, err := readName(r, saveData)
	if err != nil {
		return ArrayStructProperty{}, err
	}

	guid, err := ue.ReadGuid(r)
	if err != nil {
		return ArrayStructProperty{}, err
	}

	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return ArrayStructProperty{}, err
	}

	return ArrayStructProperty{
		ElementType: elementType,
		GUID:        guid,
		Size:        size,
	}, nil
}

type StructProperty struct {
	Name  string
	GUID  ue.FGuid
	Value interface{}
	Size  uint32
}

func readStructPropertyData(r io.ReadSeeker, structName string, saveData *SaveData) (interface{}, error) {
	switch structName {
	case "SoftClassPath":
		return readStrProperty(r, true)

	case "SoftObjectPath":
		return readStrProperty(r, true)

	case "Timespan":
		return memory.ReadInt[int64](r)

	case "Guid":
		guid, err := ue.ReadGuid(r)
		if err != nil {
			return nil, err
		}
		return guid, nil

	case "Vector":
		return ue.ReadFVector(r)

	case "DateTime":
		return memory.ReadInt[int64](r)

	case "PersistenceBlob":
		{
			persistenceSize, err := memory.ReadInt[uint32](r)
			if err != nil {
				return nil, err
			}

			persistenceBytes := make([]byte, persistenceSize)
			_, err = r.Read(persistenceBytes)
			if err != nil {
				return nil, err
			}
			persistenceReader := bytes.NewReader(persistenceBytes)

			if saveData.SaveGameClassPath.Path == REMNANT_SAVE_GAME_PROFILE {
				archive, err := readSaveData(persistenceReader, true, false)
				if err != nil {
					return nil, err
				}

				return PersistenceBlob{
					Archive: archive,
				}, nil
			}

			version, err := memory.ReadInt[uint32](persistenceReader)
			if err != nil {
				return nil, err
			}

			indexOffset, err := memory.ReadInt[uint32](persistenceReader)
			if err != nil {
				return nil, err
			}

			dynamicOffset, err := memory.ReadInt[uint32](persistenceReader)
			if err != nil {
				return nil, err
			}

			_, err = persistenceReader.Seek(int64(indexOffset), io.SeekStart)
			if err != nil {
				return nil, err
			}

			infoCount, err := memory.ReadInt[uint32](persistenceReader)
			if err != nil {
				return nil, err
			}

			actorInfo := make([]ue.FInfo, infoCount)
			for i := uint32(0); i < infoCount; i++ {
				actorInfo[i], err = ue.ReadFInfo(persistenceReader)
				if err != nil {
					return nil, err
				}
			}

			destroyedCount, err := memory.ReadInt[uint32](persistenceReader)
			if err != nil {
				return nil, err
			}

			destroyed := make([]uint64, destroyedCount)
			for i := uint32(0); i < destroyedCount; i++ {
				destroyed[i], err = memory.ReadInt[uint64](persistenceReader)
				if err != nil {
					return nil, err
				}
			}

			actors := make(map[uint64]Actor)
			for _, info := range actorInfo {
				_, err = persistenceReader.Seek(int64(info.Offset), io.SeekStart)
				if err != nil {
					return nil, err
				}

				actorBytes := make([]byte, info.Size)
				_, err = persistenceReader.Read(actorBytes)
				if err != nil {
					return nil, err
				}

				actorReader := bytes.NewReader(actorBytes)

				actors[info.UniqueID], err = readActor(actorReader)
				if err != nil {
					return nil, err
				}
			}

			_, err = persistenceReader.Seek(int64(dynamicOffset), io.SeekStart)
			if err != nil {
				return nil, err
			}

			dynamicCount, err := memory.ReadInt[uint32](persistenceReader)
			if err != nil {
				return nil, err
			}

			for i := uint32(0); i < dynamicCount; i++ {
				dynamicActor, err := readDynamicActor(persistenceReader)
				if err != nil {
					return nil, err
				}

				actor := actors[dynamicActor.UniqueID]
				actor.DynamicData = &dynamicActor
				actors[dynamicActor.UniqueID] = actor
			}

			return PersistenceContainer{
				Version:   version,
				Destroyed: destroyed,
				Actors:    actors,
			}, nil
		}

	default:
		return readProperties(r, saveData)
	}
}

func readStructProperty(r io.ReadSeeker, saveData *SaveData, varSize uint32, raw bool) (interface{}, error) {
	if raw {
		guid, err := ue.ReadGuid(r)
		if err != nil {
			return StructReference{}, err
		}

		return StructReference{
			GUID: guid,
		}, nil
	}

	structName, err := readName(r, saveData)
	if err != nil {
		return StructProperty{}, err
	}

	// 17 bytes, 16 GUID + padding?
	guid, err := ue.ReadGuid(r)
	if err != nil {
		return StructProperty{}, err
	}
	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return StructProperty{}, err
	}

	result, err := readStructPropertyData(r, structName, saveData)
	if err != nil {
		return StructProperty{}, err
	}

	return StructProperty{
		Name:  structName,
		GUID:  guid,
		Value: result,
		Size:  varSize,
	}, nil
}

type EnumProperty struct {
	EnumType  string
	EnumValue string
}

func readEnumProperty(r io.ReadSeeker, saveData *SaveData) (EnumProperty, error) {
	enumType, err := readName(r, saveData)
	if err != nil {
		return EnumProperty{}, fmt.Errorf("readEnumProperty: %w", err)
	}

	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return EnumProperty{}, fmt.Errorf("readEnumProperty: %w", err)
	}

	enumValue, err := readName(r, saveData)
	if err != nil {
		return EnumProperty{}, fmt.Errorf("readEnumProperty: %w", err)
	}

	return EnumProperty{
		EnumType:  enumType,
		EnumValue: enumValue,
	}, nil
}

type TextPropertyData struct {
	Namespace    string
	Key          string
	SourceString string
}

type TextData struct {
	Data string
}

type TextProperty struct {
	Flags       uint32
	HistoryType uint8
	Data        interface{}
}

func readTextProperty(r io.ReadSeeker, raw bool) (TextProperty, error) {
	if !raw {
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return TextProperty{}, err
		}
	}

	flags, err := memory.ReadInt[uint32](r)
	if err != nil {
		return TextProperty{}, err
	}

	historyType, err := memory.ReadInt[uint8](r)
	if err != nil {
		return TextProperty{}, err
	}

	var result interface{}
	switch historyType {
	case 0:
		namespace, err := ue.ReadFString(r)
		if err != nil {
			return TextProperty{}, err
		}

		key, err := ue.ReadFString(r)
		if err != nil {
			return TextProperty{}, err
		}

		sourceString, err := ue.ReadFString(r)
		if err != nil {
			return TextProperty{}, err
		}

		result = TextPropertyData{
			Namespace:    namespace,
			Key:          key,
			SourceString: sourceString,
		}
	case 255:
		flag, err := memory.ReadInt[uint32](r)
		if err != nil {
			return TextProperty{}, err
		}

		if flag != 0 {
			stringData, err := ue.ReadFString(r)
			if err != nil {
				return TextProperty{}, err
			}
			result = TextData{
				Data: stringData,
			}
		}
	default:
		result = TextPropertyData{
			Namespace:    "UNSUPPORTED",
			Key:          "UNSUPPORTED",
			SourceString: "UNSUPPORTED",
		}
	}

	return TextProperty{
		Data:        result,
		Flags:       flags,
		HistoryType: historyType,
	}, nil
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

func readMapProperty(r io.ReadSeeker, saveData *SaveData) (MapProperty, error) {
	result := MapProperty{}

	var err error

	result.KeyType, err = readName(r, saveData)
	if err != nil {
		return result, fmt.Errorf("readMapProperty: %w", err)
	}

	result.ValueType, err = readName(r, saveData)
	if err != nil {
		return result, fmt.Errorf("readMapProperty: %w", err)
	}

	_, err = r.Seek(5, io.SeekCurrent)
	if err != nil {
		return result, fmt.Errorf("readMapProperty: %w", err)
	}

	mapLength, err := memory.ReadInt[int32](r)
	if err != nil {
		return result, fmt.Errorf("readMapProperty: %w", err)
	}

	values := make([]MapPropertyValue, mapLength)
	for i := 0; i < int(mapLength); i++ {
		key, err := getPropertyValue(r, result.KeyType, 0, saveData, true)
		if err != nil {
			return result, fmt.Errorf("readMapProperty: %w", err)
		}
		value, err := getPropertyValue(r, result.ValueType, 0, saveData, true)
		if err != nil {
			return result, fmt.Errorf("readMapProperty: %w", err)
		}

		values[i] = struct{ Key, Value interface{} }{key, value}
	}
	result.Values = values

	return result, nil
}

type PersistenceBlob struct {
	Archive SaveData
}

type PersistenceContainer struct {
	Version   uint32
	Destroyed []uint64
	Actors    map[uint64]Actor
}

type Actor struct {
	Transform   *ue.FTransform
	Archive     SaveData
	DynamicData *DynamicActor
}

func readActor(r io.ReadSeeker) (Actor, error) {
	hasTransform, err := memory.ReadInt[uint32](r)
	if err != nil {
		return Actor{}, fmt.Errorf("readActor: %w", err)
	}

	var transform ue.FTransform
	if hasTransform != 0 {
		transform, err = ue.ReadFTransform(r)
		if err != nil {
			return Actor{}, fmt.Errorf("readActor: %w", err)
		}
	}

	archive, err := readSaveData(r, false, false)
	if err != nil {
		return Actor{}, fmt.Errorf("readActor: %w", err)
	}

	return Actor{
		Transform: &transform,
		Archive:   archive,
	}, nil
}

type DynamicActor struct {
	UniqueID  uint64
	Transform *ue.FTransform
	ClassPath ue.FTopLevelAssetPath
}

func readDynamicActor(r io.Reader) (DynamicActor, error) {
	uniqueID, err := memory.ReadInt[uint64](r)
	if err != nil {
		return DynamicActor{}, fmt.Errorf("readDynamicActor: %w", err)
	}

	transform, err := ue.ReadFTransform(r)
	if err != nil {
		return DynamicActor{}, fmt.Errorf("readDynamicActor: %w", err)
	}

	classPath, err := ue.ReadFTopLevelAssetPath(r)
	if err != nil {
		return DynamicActor{}, fmt.Errorf("readDynamicActor: %w", err)
	}

	return DynamicActor{
		UniqueID:  uniqueID,
		Transform: &transform,
		ClassPath: classPath,
	}, nil
}

type Number interface {
	memory.Int | float64 | float32
}

func readNumProperty[T Number](r io.ReadSeeker, raw bool) (T, error) {
	if !raw {
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return 0, fmt.Errorf("readIntProperty: %w", err)
		}
	}

	var varData T
	err := binary.Read(r, binary.LittleEndian, &varData)
	if err != nil {
		return 0, fmt.Errorf("readIntProperty: %w", err)
	}

	return varData, nil
}

func readName(r io.Reader, saveData *SaveData) (string, error) {
	fName, err := ue.ReadFName(r)
	if err != nil {
		return "", err
	}

	if int(fName.Index) >= len(saveData.NamesTable) {
		return "", fmt.Errorf("readNameProperty: invalid index %d", fName.Index)
	}

	return saveData.NamesTable[fName.Index], nil
}

func readBoolProperty(r io.ReadSeeker, raw bool) (bool, error) {
	varData, err := memory.ReadInt[uint8](r)
	if err != nil {
		return false, fmt.Errorf("readBoolProperty: %w", err)
	}
	if !raw {
		_, err = r.Seek(1, io.SeekCurrent)
		if err != nil {
			return false, fmt.Errorf("readBoolProperty: %w", err)
		}
	}
	return varData == 1, nil
}

func readStrProperty(r io.ReadSeeker, raw bool) (string, error) {
	if !raw {
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return "", fmt.Errorf("readStrProperty: %w", err)
		}
	}

	strLength, err := memory.ReadInt[int32](r)
	if err != nil {
		return "", fmt.Errorf("readStrProperty: %w", err)
	}

	if strLength == 0 {
		return "", nil
	}
	strData := make([]byte, strLength)
	_, err = r.Read(strData)
	if err != nil {
		return "", fmt.Errorf("readStrProperty: %w", err)
	}

	return string(bytes.Trim(strData, "\x00")), nil
}

func readNameProperty(r io.ReadSeeker, saveData *SaveData, raw bool) (string, error) {
	if !raw {
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return "", err
		}
	}

	return readName(r, saveData)
}

func getPropertyValue(r io.ReadSeeker, varType string, varSize uint32, saveData *SaveData, raw bool) (interface{}, error) {
	switch varType {
	case "IntProperty":
		return readNumProperty[int32](r, raw)

	case "Int16Property":
		return readNumProperty[int16](r, raw)

	case "Int64Property":
		return readNumProperty[int64](r, raw)

	case "UInt64Property":
		return readNumProperty[uint64](r, raw)

	case "FloatProperty":
		return readNumProperty[float32](r, raw)

	case "DoubleProperty":
		return readNumProperty[float64](r, raw)

	case "UInt16Property":
		return readNumProperty[uint16](r, raw)

	case "UInt32Property":
		return readNumProperty[uint32](r, raw)

	case "SoftClassPath":
		if !raw {
			_, err := r.Seek(1, io.SeekCurrent)
			if err != nil {
				return "", err
			}
		}
		return ue.ReadFString(r)

	case "SoftObjectProperty":
		if !raw {
			_, err := r.Seek(1, io.SeekCurrent)
			if err != nil {
				return "", err
			}
		}
		return ue.ReadFString(r)

	case "BoolProperty":
		return readBoolProperty(r, raw)

	case "MapProperty":
		if raw {
			log.Fatal("Raw map property is not supported yet")
		}
		return readMapProperty(r, saveData)

	case "EnumProperty":
		return readEnumProperty(r, saveData)

	case "StrProperty":
		return readStrProperty(r, raw)

	case "TextProperty":
		return readTextProperty(r, raw)

	case "NameProperty":
		return readNameProperty(r, saveData, raw)

	case "ArrayProperty":
		return readArrayProperty(r, saveData, varSize)

	case "StructProperty":
		return readStructProperty(r, saveData, varSize, raw)

	case "ObjectProperty":
		return readObjectProperty(r, saveData, raw)

	case "ByteProperty":
		return readByteProperty(r, saveData, raw)

	case "None":
		return nil, nil

	default:
		return nil, fmt.Errorf("property type is not supported yet: %s", varType)
	}
}

func readProperty(r io.ReadSeeker, saveData *SaveData) (*Property, error) {
	varName, err := readName(r, saveData)
	if err != nil {
		return nil, fmt.Errorf("failed to read variable name index: %w", err)
	}

	if varName == "None" {
		return nil, nil
	}

	varType, err := readName(r, saveData)
	if err != nil {
		return nil, fmt.Errorf("failed to read variable type index: %w", err)
	}

	varSize, err := memory.ReadInt[uint32](r)
	if err != nil {
		return nil, fmt.Errorf("failed to read variable size: %w", err)
	}

	index, err := memory.ReadInt[uint32](r)
	if err != nil {
		return nil, err
	}

	var value interface{}
	if varName == "FowVisitedCoordinates" {
		value = make([]byte, varSize+19)
		_, err := r.Read(value.([]byte))
		if err != nil {
			return nil, err
		}
	} else {
		value, err = getPropertyValue(r, varType, varSize, saveData, false)
		if err != nil {
			return nil, fmt.Errorf("failed to read variable data (%s %s %d): %w", varName, varType, varSize, err)
		}
	}

	return &Property{
		Name:  varName,
		Type:  varType,
		Index: index,
		Size:  varSize,
		Value: value,
	}, nil
}

func readProperties(r io.ReadSeeker, saveData *SaveData) ([]Property, error) {
	result := []Property{}
	for {
		property, err := readProperty(r, saveData)
		if err != nil {
			return nil, err
		}
		if property == nil {
			break
		}
		result = append(result, *property)
	}

	return result, nil
}
