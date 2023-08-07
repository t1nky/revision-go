package remnant

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"remnant-save-edit/config"
	"remnant-save-edit/memory"
	"remnant-save-edit/ue"
	"remnant-save-edit/utils"
)

type ObjectProperty map[string]interface{}

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

	return ue.ReadFString(r)
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

func readEnumProperty(r io.ReadSeeker, names []string) (EnumProperty, error) {
	enumTypeIndex, err := memory.ReadInt[int16](r)
	if err != nil {
		return EnumProperty{}, err
	}
	enumType := names[enumTypeIndex]

	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return EnumProperty{}, err
	}

	// is it always 2 bytes
	enumValueIndex, err := memory.ReadInt[int16](r)
	if err != nil {
		return EnumProperty{}, err
	}
	enumValue := names[enumValueIndex]

	return EnumProperty{
		EnumType:  enumType,
		EnumValue: enumValue,
	}, nil
}

func readMapProperty(r io.ReadSeeker, names []string, objects []ue.UObject) (MapProperty, error) {
	result := MapProperty{}

	keyIndex, err := memory.ReadInt[int16](r)
	if err != nil {
		return result, err
	}
	result.KeyType = names[keyIndex]

	valueIndex, err := memory.ReadInt[int16](r)
	if err != nil {
		return result, err
	}
	result.ValueType = names[valueIndex]

	_, err = r.Seek(5, io.SeekCurrent)
	if err != nil {
		return result, err
	}

	mapLength, err := memory.ReadInt[int32](r)
	if err != nil {
		return result, err
	}

	values := make([]MapPropertyValue, mapLength)
	for i := 0; i < int(mapLength); i++ {
		// -- something is off here, because map does not contain variable size, it is key:value pairs one after another
		key, err := ReadProperty(r, result.KeyType, 0, names, objects, true)
		if err != nil {
			return result, err
		}
		value, err := ReadProperty(r, result.ValueType, 0, names, objects, true)
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

	strLength, err := memory.ReadInt[int32](r)
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

func readNameProperty(r io.ReadSeeker, names []string, raw bool) (string, error) {
	if !raw {
		// unknown 1 byte
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return "", err
		}
	}

	nameIndex, err := memory.ReadInt[int16](r)
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

	guidLength, err := memory.ReadInt[int32](r)
	if err != nil {
		return TextProperty{}, err
	}

	// it is string, but I'm not sure where it's used, so read it as bytes
	guidData := make([]byte, guidLength)
	_, err = r.Read(guidData)
	if err != nil {
		return TextProperty{}, err
	}

	textLength, err := memory.ReadInt[int32](r)
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

func readObjectProperty(r io.ReadSeeker, objects []ue.UObject, raw bool) (ObjectProperty, error) {
	if !raw {
		// unknown 1 byte
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return ObjectProperty{}, err
		}
	}

	// I think for ObjectProperty objectIndex is 4bytes
	// but I'd like to keep it consistent with other properties
	objectIndex, err := memory.ReadInt[int16](r)
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

var persistenceCounter = 0

func readStructArrayProperty(r io.ReadSeeker, arrayLength int32, names []string, objects []ue.UObject) (StructArrayProperty, error) {
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

	innerTypeIndex, err := memory.ReadInt[int16](r)
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
			variableNameIndex, err := memory.ReadInt[int16](r)
			if err != nil {
				return result, err
			}
			variableName := names[variableNameIndex]

			if variableName == "None" {
				// end of struct
				break
			}

			varTypeIndex, err := memory.ReadInt[int16](r)
			if err != nil {
				return result, err
			}
			varType := names[varTypeIndex]

			varSize, err := memory.ReadInt[int32](r)
			if err != nil {
				return result, err
			}

			_, err = r.Seek(4, io.SeekCurrent)
			if err != nil {
				return result, err
			}

			value, err := ReadProperty(r, varType, varSize, names, objects, false)
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
	version, err := memory.ReadInt[uint32](r)
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
		persistenceContainer.Header.IndexOffset, err = memory.ReadInt[uint32](r)
		if err != nil {
			return nil, err
		}
		persistenceContainer.Header.DynamicOffset, err = memory.ReadInt[uint32](r)
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

		numInfos, err := memory.ReadInt[int32](r)
		if err != nil {
			return nil, err
		}

		persistenceContainer.Info = make([]PersistenceInfo, numInfos)

		for i := 0; i < int(numInfos); i++ {
			persistenceContainer.Info[i] = PersistenceInfo{}
			persistenceContainer.Info[i].UniqueID, err = memory.ReadInt[uint64](r)
			if err != nil {
				return nil, err
			}
			if version < 2 { // in our case version is 4, so this code is basically unreachable
				_, err = ue.ReadFString(r) // or readFName if this wont work
				if err != nil {
					return nil, err
				}
			}
			persistenceContainer.Info[i].Offset, err = memory.ReadInt[uint32](r)
			if err != nil {
				return nil, err
			}
			persistenceContainer.Info[i].Length, err = memory.ReadInt[uint32](r)
			if err != nil {
				return nil, err
			}
		}

		return persistenceContainer, nil
	}

	// else (for our case this is used in Profile)

	// _, err = r.Seek(4, io.SeekCurrent)
	// if err != nil {
	// 	return nil, err
	// }

	// // Read strings table
	// stringsTableOffset, err := memory.ReadInt[int64](r)
	// if err != nil {
	// 	return nil, err
	// }

	// startPos, err := r.Seek(0, io.SeekCurrent)
	// if err != nil {
	// 	return nil, err
	// }

	// _, err = r.Seek(stringsTableOffset, io.SeekStart)
	// if err != nil {
	// 	return nil, err
	// }

	// stringsNum, err := memory.ReadInt[int32](r)
	// if err != nil {
	// 	return nil, err
	// }

	// names = make([]string, stringsNum)

	// for i := 0; i < int(stringsNum); i++ {
	// 	stringData, err := ue.ReadFString(r)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	names[i] = stringData
	// }

	// _, err = r.Seek(startPos, io.SeekStart)
	// if err != nil {
	// 	return nil, err
	// }

	// // skip 4 bytes - unknown, version (GUNFIRE_SAVEGAME_ARCHIVE_VERSION)
	// _, err = r.Seek(4, io.SeekCurrent)
	// if err != nil {
	// 	return nil, err
	// }

	// // base object
	// err = readBaseObject(r)
	// if err != nil {
	// 	return nil, err
	// }

	// return fmt.Sprintf("%s with size %d", "PersistenceBlob", varSize), nil
	return "PersistenceBlob", nil
}

func ReadProperty(r io.ReadSeeker, varType string, varSize int32, names []string, objects []ue.UObject, raw bool) (interface{}, error) {
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
			return readMapProperty(r, names, objects)
		}
	case "EnumProperty":
		{
			return readEnumProperty(r, names)
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
			return readNameProperty(r, names, raw)
		}

	case "ArrayProperty":
		{
			elementPropertyTypeIndex, err := memory.ReadInt[int16](r)
			if err != nil {
				return nil, err
			}
			elementPropertyType := names[elementPropertyTypeIndex]

			_, err = r.Seek(1, io.SeekCurrent)
			if err != nil {
				return nil, err
			}

			arrayLength, err := memory.ReadInt[int32](r)
			if err != nil {
				return nil, err
			}

			if elementPropertyType == "StructProperty" {
				return readStructArrayProperty(r, arrayLength, names, objects)
			}

			result := ArrayProperty{
				ElementType: elementPropertyType,
				Count:       arrayLength,
				Items:       make([]interface{}, arrayLength),
			}
			for i := 0; i < int(arrayLength); i++ {
				elementValue, err := ReadProperty(r, elementPropertyType, varSize, names, objects, raw)
				if err != nil {
					return nil, err
				}
				result.Items[i] = elementValue
			}

			return result, nil
		}
	case "StructProperty":
		{
			propertyTypeIndex, err := memory.ReadInt[int16](r)
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
				persistenceSize, err := memory.ReadInt[int32](r)
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

				utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, fmt.Sprintf("%d_persistence", persistenceCounter), "bin", persistenceBytes)
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
			return readObjectProperty(r, objects, raw)
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
