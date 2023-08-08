package remnant

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"revision-go/memory"
	"revision-go/ue"
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

type TextData struct {
	Data string
}

type TextProperty struct {
	Flags       uint32
	HistoryType uint8
	Data        interface{}
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

type PersistenceResult struct {
	ID       int32
	UniqueID uint64
	Data     interface{}
}

type Vector struct {
	X float64
	Y float64
	Z float64
}

type Number interface {
	memory.Int | float64 | float32
}

func readNumProperty[T Number](r io.ReadSeeker, raw bool) (T, error) {
	if !raw {
		// 1 unknown byte?
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

func readBoolProperty(r io.ReadSeeker) (bool, error) {
	varData, err := memory.ReadInt[uint8](r)
	if err != nil {
		return false, fmt.Errorf("readBoolProperty: %w", err)
	}
	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return false, fmt.Errorf("readBoolProperty: %w", err)
	}
	return varData == 1, nil
}

func readEnumProperty(r io.ReadSeeker, tables *Tables) (EnumProperty, error) {
	enumType, err := readName(r, tables)
	if err != nil {
		return EnumProperty{}, fmt.Errorf("readEnumProperty: %w", err)
	}

	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return EnumProperty{}, fmt.Errorf("readEnumProperty: %w", err)
	}

	enumValue, err := readName(r, tables)
	if err != nil {
		return EnumProperty{}, fmt.Errorf("readEnumProperty: %w", err)
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
		return result, fmt.Errorf("readMapProperty: %w", err)
	}

	result.ValueType, err = readName(r, tables)
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
		key, err := getPropertyValue(r, result.KeyType, 0, tables, true)
		if err != nil {
			return result, fmt.Errorf("readMapProperty: %w", err)
		}
		value, err := getPropertyValue(r, result.ValueType, 0, tables, true)
		if err != nil {
			return result, fmt.Errorf("readMapProperty: %w", err)
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

func readObjectProperty(r io.ReadSeeker, tables *Tables, raw bool) (ObjectProperty, error) {
	if !raw {
		// unknown 1 byte
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
		// no object
		return ObjectProperty{}, nil
	}

	return ObjectProperty{
		ClassName: tables.Classes[objectIndex].PathName,
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

	if name == "None" {
		// or the other way around?
		// currently for equipment level it reads 0xA (10)
		// and following byte is 0
		_, err = r.Seek(1, io.SeekCurrent)
		if err != nil {
			return 0, err
		}

		byteData, err := memory.ReadInt[uint8](r)
		if err != nil {
			return 0, err
		}

		return byteData, nil
	}

	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return 0, err
	}

	enumName, err := readName(r, tables)
	if err != nil {
		return 0, err
	}
	return enumName, nil
}

func readPersistenceBlobObject(r io.ReadSeeker, tables *Tables) (PersistenceBlobObject, error) {
	name, err := ue.ReadFString(r)
	if err != nil {
		return PersistenceBlobObject{}, err
	}
	size, err := memory.ReadInt[uint32](r)
	if err != nil {
		return PersistenceBlobObject{}, err
	}

	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return PersistenceBlobObject{}, err
	}

	properties, err := readProperties(r, tables)
	if err != nil {
		return PersistenceBlobObject{}, err
	}

	_, err = r.Seek(start+int64(size), io.SeekStart)
	if err != nil {
		return PersistenceBlobObject{}, err
	}

	return PersistenceBlobObject{
		Name:       name,
		Size:       size,
		Properties: properties,
	}, nil
}

func readStructProperty(r io.ReadSeeker, structName string, varSize uint32, tables *Tables) (interface{}, error) {
	if structName == "SoftClassPath" {
		return readStrProperty(r, true)
	}
	if structName == "SoftObjectPath" {
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

		version := binary.LittleEndian.Uint32(persistenceBytes[4:8])

		if version == 4 {
			return readPersistenceContainer(persistenceBytes)
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

func readPersistenceContainer(bytesData []byte) (interface{}, error) {
	persistenceContainer := PersistenceContainer{
		Header: PersistenceContainerHeader{
			Version:       binary.LittleEndian.Uint32(bytesData[4:8]),
			IndexOffset:   binary.LittleEndian.Uint32(bytesData[8:12]),
			DynamicOffset: binary.LittleEndian.Uint32(bytesData[12:16]),
		},
		Info: []PersistenceInfo{},
	}

	r := bytes.NewReader(bytesData)
	_, err := r.Seek(int64(persistenceContainer.Header.IndexOffset+4), io.SeekStart)
	if err != nil {
		return nil, err
	}

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
		// if version < 2 { // in our case version is 4, so this code is basically unreachable
		// 	_, err = ue.ReadFString(r) // or readFName if this wont work
		// 	if err != nil {
		// 		return nil, err
		// 	}
		// }
		persistenceContainer.Info[i].Offset, err = memory.ReadInt[uint32](r)
		if err != nil {
			return nil, err
		}
		persistenceContainer.Info[i].Length, err = memory.ReadInt[uint32](r)
		if err != nil {
			return nil, err
		}
	}

	result := []PersistenceResult{}
	for i := 0; i < len(persistenceContainer.Info); i++ {
		from := persistenceContainer.Info[i].Offset + 4
		to := from + persistenceContainer.Info[i].Length
		persistenceContainerReader := bytes.NewReader(bytesData[from:to])

		_, err = persistenceContainerReader.Seek(4, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		offsets, err := readOffsets(persistenceContainerReader)
		if err != nil {
			return nil, err
		}

		tables, err := readTables(persistenceContainerReader, offsets)
		if err != nil {
			return nil, err
		}

		// skip 4 bytes
		// version, err = memory.ReadInt[int32](r) // version
		_, err = persistenceContainerReader.Seek(4, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		persistenceData, err := readProperties(persistenceContainerReader, &tables)
		if err != nil {
			return nil, err
		}

		result = append(result, PersistenceResult{
			ID:       int32(i),
			UniqueID: persistenceContainer.Info[i].UniqueID,
			Data:     persistenceData,
		})
	}

	numDestroyed, err := memory.ReadInt[int32](r)
	if err != nil {
		return nil, err
	}

	destroyed := make([]uint64, numDestroyed)
	for i := 0; i < int(numDestroyed); i++ {
		destroyed[i], err = memory.ReadInt[uint64](r)
		if err != nil {
			return nil, err
		}
		// if version < 2 { // in our case version is 4, so this code is basically unreachable
		// 	_, err = ue.ReadFString(r)
		// 	if err != nil {
		// 		return nil, err
		// 	}
		// }
	}

	return result, nil
}
