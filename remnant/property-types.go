package remnant

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"remnant-save-edit/memory"
	"remnant-save-edit/ue"
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

type ArrayProperty struct {
	Count       uint32
	Items       []interface{}
	ElementType string
}

type StructProperty struct {
	Name  string
	GUID  GuidData
	Value interface{}
}

type EnumProperty struct {
	EnumType  string
	EnumValue string
}

type TextPropertyData struct {
	Namespace    string
	Key          string
	SourceString string
}

type TextProperty struct {
	Flags       uint32
	HistoryType uint8
	Data        TextPropertyData
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

type ByteProperty interface {
	byte | string
}

type Vector struct {
	X float64
	Y float64
	Z float64
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

func readBoolProperty(r io.ReadSeeker) (bool, error) {
	varData, err := memory.ReadInt[uint8](r)
	if err != nil {
		return false, err
	}
	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return false, err
	}
	return varData == 1, nil
}

func readEnumProperty(r io.ReadSeeker, tables *Tables) (EnumProperty, error) {
	enumType, err := readName(r, tables)
	if err != nil {
		return EnumProperty{}, err
	}

	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return EnumProperty{}, err
	}

	enumValue, err := readName(r, tables)
	if err != nil {
		return EnumProperty{}, err
	}

	return EnumProperty{
		EnumType:  enumType,
		EnumValue: enumValue,
	}, nil
}

func readMapProperty(r io.ReadSeeker, tables *Tables) (MapProperty, error) {
	result := MapProperty{}

	var err error

	result.KeyType, err = readName(r, tables)
	if err != nil {
		return result, err
	}

	result.ValueType, err = readName(r, tables)
	if err != nil {
		return result, err
	}

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
		// map does not contain variable size, it is key:value pairs one after another
		// we might do something else rather than ReadProperty
		// maybe something like FromBytes
		key, err := getPropertyValue(r, result.KeyType, 0, tables, true)
		if err != nil {
			return result, err
		}
		value, err := getPropertyValue(r, result.ValueType, 0, tables, true)
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

func readNameProperty(r io.ReadSeeker, tables *Tables, raw bool) (string, error) {
	if !raw {
		// unknown 1 byte
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return "", err
		}
	}

	name, err := readName(r, tables)
	if err != nil {
		return "", err
	}

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

func readTextProperty(r io.ReadSeeker, raw bool) (TextProperty, error) {
	if !raw {
		// unknown 1 byte
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

	var result TextPropertyData
	if historyType == 0 {
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
	} else {
		result = TextPropertyData{
			Namespace:    "UNSUPPORTED",
			Key:          "UNSUPPORTED",
			SourceString: "UNSUPPORTED",
		}
	}

	// // 10 unknown bytes
	// _, err := r.Seek(10, io.SeekCurrent)
	// if err != nil {
	// 	return TextProperty{}, err
	// }

	// guidLength, err := memory.ReadInt[int32](r)
	// if err != nil {
	// 	return TextProperty{}, err
	// }

	// // it is string, but I'm not sure where it's used, so read it as bytes
	// guidData := make([]byte, guidLength)
	// _, err = r.Read(guidData)
	// if err != nil {
	// 	return TextProperty{}, err
	// }

	// textLength, err := memory.ReadInt[int32](r)
	// if err != nil {
	// 	return TextProperty{}, err
	// }

	// textData := make([]byte, textLength)
	// _, err = r.Read(textData)
	// if err != nil {
	// 	return TextProperty{}, err
	// }

	return TextProperty{
		Data:        result,
		Flags:       flags,
		HistoryType: historyType,
	}, nil
}

func readObjectProperty(r io.ReadSeeker, tables *Tables, raw bool) (ObjectProperty, error) {
	if !raw {
		// unknown 1 byte
		_, err := r.Seek(1, io.SeekCurrent)
		if err != nil {
			return ObjectProperty{}, err
		}
	}

	// I think for ObjectProperty objectIndex is 4bytes
	// but I'd like to keep it consistent with other properties
	objectIndex, err := memory.ReadInt[int32](r)
	if err != nil {
		return ObjectProperty{}, err
	}

	if objectIndex == -1 {
		// no object
		return ObjectProperty{}, nil
	}

	return ObjectProperty{
		ClassName: tables.Classes[objectIndex].Name,
	}, nil
}

func readName(r io.Reader, tables *Tables) (string, error) {
	nameIndex, err := memory.ReadInt[uint16](r)
	if err != nil {
		return "", err
	}
	if nameIndex >= uint16(len(tables.Names)) {
		return "", fmt.Errorf("nameIndex is out of range: %d", nameIndex)
	}
	name := tables.Names[nameIndex]

	return name, nil
}

func readByteProperty(r io.ReadSeeker, tables *Tables, raw bool) (interface{}, error) {
	// not sure why is it 8 bytes
	// i don't know what to return here
	// returning 0 for now
	// _, err := r.Seek(4, io.SeekCurrent)
	// if err != nil {
	// 	return 0, err
	// }

	if raw {
		value, err := memory.ReadInt[uint8](r)
		if err != nil {
			return 0, err
		}

		return value, nil
	}
	name, err := readName(r, tables)
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

	enumName, err := readName(r, tables)
	if err != nil {
		return 0, err
	}
	return enumName, nil
}

func readStructProperty(r io.ReadSeeker, structName string, varSize uint32, tables *Tables) (interface{}, error) {
	if structName == "SoftClassPath" {
		return readStrProperty(r, true)
	}
	if structName == "Timespan" {
		return memory.ReadInt[int64](r)
	}
	if structName == "PersistenceBlob" {
		// read all the data
		// create new reader just for persistence blob
		// pass new reader to readPersistenceBlob
		persistenceBytes := make([]byte, varSize)
		_, err := r.Read(persistenceBytes)
		if err != nil {
			return nil, err
		}
		persistenceReader := bytes.NewReader(persistenceBytes)
		var persistenceBlobHeader PersistenceBlobHeader
		err = binary.Read(persistenceReader, binary.LittleEndian, &persistenceBlobHeader)
		if err != nil {
			return nil, err
		}

		persistenceTables, err := readTables(persistenceReader, OffsetInfo{
			Names:   persistenceBlobHeader.NamesOffset + 4,
			Classes: persistenceBlobHeader.ClassesOffset + 4,
		})
		if err != nil {
			return nil, err
		}

		baseObject, err := readPersistenceBlobObject(persistenceReader, &persistenceTables)
		if err != nil {
			return nil, err
		}

		flag, err := memory.ReadInt[uint8](persistenceReader)
		if err != nil {
			return nil, err
		}

		objectCount, err := memory.ReadInt[uint32](persistenceReader)
		if err != nil {
			return nil, err
		}

		objects := make([]PersistenceBlobObject, objectCount)
		objects = append(objects, baseObject)
		for i := 0; i < int(objectCount); i++ {
			object, err := readPersistenceBlobObject(persistenceReader, &persistenceTables)
			if err != nil {
				return nil, err
			}
			objects[i] = object
		}

		err = readClassAdditionalData(persistenceReader, &persistenceTables)
		if err != nil {
			return nil, err
		}

		return PersistenceBlob{
			Size:        persistenceBlobHeader.Size,
			NamesOffset: persistenceBlobHeader.NamesOffset,
			ClassOffset: persistenceBlobHeader.ClassesOffset,
			BaseObject:  baseObject,
			Flag:        flag,
			ObjectCount: objectCount,
			Objects:     objects,
		}, nil

		// var persistenceBlobHeader PersistenceBlobHeader

		// utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, fmt.Sprintf("%d_persistence", persistenceCounter), "bin", persistenceBytes)
		// persistenceCounter++

		// version := binary.LittleEndian.Uint32(persistenceBytes[0:4])

		// if version == 4 {
		// 	return readPersistenceContainer(persistenceBytes)
		// }
		// persistenceReader := bytes.NewReader(persistenceBytes)
		// return readPersistenceBlob(persistenceReader)
	}
	if structName == "Guid" {
		var guidData GuidData
		err := binary.Read(r, binary.LittleEndian, &guidData)
		if err != nil {
			return nil, err
		}
		return guidData, nil
	}
	if structName == "Vector" {
		var vector Vector
		err := binary.Read(r, binary.LittleEndian, &vector)
		if err != nil {
			return nil, err
		}
		return vector, nil
	}

	return readProperties(r, tables)
}

// func readPersistenceBlob(r io.ReadSeeker) (interface{}, error) {
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
// 	return "PersistenceBlob", nil
// }

// type PersistenceResult struct {
// 	ID       int32
// 	UniqueID uint64
// 	Data     interface{}
// }

// func readPersistenceContainer(bytesData []byte) (interface{}, error) {
// 	persistenceContainer := PersistenceContainer{
// 		Header: PersistenceContainerHeader{
// 			Version:       binary.LittleEndian.Uint32(bytesData[0:4]),
// 			IndexOffset:   binary.LittleEndian.Uint32(bytesData[4:8]),
// 			DynamicOffset: binary.LittleEndian.Uint32(bytesData[8:12]),
// 		},
// 		Info: []PersistenceInfo{},
// 	}

// 	r := bytes.NewReader(bytesData)
// 	_, err := r.Seek(int64(persistenceContainer.Header.IndexOffset), io.SeekStart) // +4?
// 	if err != nil {
// 		return nil, err
// 	}

// 	numInfos, err := memory.ReadInt[int32](r)
// 	if err != nil {
// 		return nil, err
// 	}

// 	persistenceContainer.Info = make([]PersistenceInfo, numInfos)

// 	for i := 0; i < int(numInfos); i++ {
// 		persistenceContainer.Info[i] = PersistenceInfo{}
// 		persistenceContainer.Info[i].UniqueID, err = memory.ReadInt[uint64](r)
// 		if err != nil {
// 			return nil, err
// 		}
// 		// if version < 2 { // in our case version is 4, so this code is basically unreachable
// 		// 	_, err = ue.ReadFString(r) // or readFName if this wont work
// 		// 	if err != nil {
// 		// 		return nil, err
// 		// 	}
// 		// }
// 		persistenceContainer.Info[i].Offset, err = memory.ReadInt[uint32](r)
// 		if err != nil {
// 			return nil, err
// 		}
// 		persistenceContainer.Info[i].Length, err = memory.ReadInt[uint32](r)
// 		if err != nil {
// 			return nil, err
// 		}
// 	}

// 	result := []PersistenceResult{}
// 	for i := 0; i < len(persistenceContainer.Info); i++ {
// 		from := persistenceContainer.Info[i].Offset
// 		to := from + persistenceContainer.Info[i].Length
// 		persistenceContainerReader := bytes.NewReader(bytesData[from:to])

// 		_, err = persistenceContainerReader.Seek(4, io.SeekCurrent)
// 		if err != nil {
// 			return nil, err
// 		}

// 		var names []string

// 		// Read strings table
// 		stringsTableOffset, err := memory.ReadInt[int64](persistenceContainerReader)
// 		if err != nil {
// 			return nil, err
// 		}

// 		startPos, err := persistenceContainerReader.Seek(0, io.SeekCurrent)
// 		if err != nil {
// 			return nil, err
// 		}

// 		_, err = persistenceContainerReader.Seek(stringsTableOffset, io.SeekStart)
// 		if err != nil {
// 			return nil, err
// 		}

// 		stringsNum, err := memory.ReadInt[int32](persistenceContainerReader)
// 		if err != nil {
// 			return nil, err
// 		}

// 		names = make([]string, stringsNum)

// 		for i := 0; i < int(stringsNum); i++ {
// 			stringData, err := ue.ReadFString(persistenceContainerReader)
// 			if err != nil {
// 				return nil, err
// 			}
// 			names[i] = stringData
// 		}

// 		_, err = persistenceContainerReader.Seek(startPos, io.SeekStart)
// 		if err != nil {
// 			return nil, err
// 		}

// 		// read version
// 		// version, err = memory.ReadInt[int32](r) // version
// 		_, err = persistenceContainerReader.Seek(4, io.SeekCurrent)
// 		if err != nil {
// 			return nil, err
// 		}

// 		persistenceData, err := readBaseObject(persistenceContainerReader, names)
// 		if err != nil {
// 			return nil, err
// 		}

// 		result = append(result, PersistenceResult{
// 			ID:       int32(i),
// 			UniqueID: persistenceContainer.Info[i].UniqueID,
// 			Data:     persistenceData,
// 		})
// 	}

// 	numDestroyed, err := memory.ReadInt[int32](r)
// 	if err != nil {
// 		return nil, err
// 	}

// 	destroyed := make([]uint64, numDestroyed)
// 	for i := 0; i < int(numDestroyed); i++ {
// 		destroyed[i], err = memory.ReadInt[uint64](r)
// 		if err != nil {
// 			return nil, err
// 		}
// 		// if version < 2 { // in our case version is 4, so this code is basically unreachable
// 		// 	_, err = ue.ReadFString(r)
// 		// 	if err != nil {
// 		// 		return nil, err
// 		// 	}
// 		// }
// 	}

// 	return result, nil
// }
