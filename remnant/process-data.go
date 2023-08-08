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

type OffsetInfo struct {
	Names   uint32
	_       [8]byte
	Classes uint32
	_       [8]byte
}

type ClassData struct {
	ID         int32
	FName      ue.FName
	FNameValue string
}

type ClassEntry struct {
	PathName       string
	Data           ClassData
	AdditionalData []Property
}

type UObject struct {
	_          [9]byte `json:"-"`
	Offset     uint32  `json:"-"`
	Properties []Property
}

type PersistenceBlobHeader struct {
	Size          uint32
	_             [8]byte `json:"-"`
	NamesOffset   uint32
	_             [8]byte `json:"-"`
	ClassesOffset uint32
	_             [4]byte `json:"-"`
}

type PersistenceBlobObject struct {
	Name       string
	Size       uint32
	Properties []Property
}

type PersistenceBlob struct {
	Size        uint32 `json:"-"`
	NamesOffset uint32 `json:"-"`
	ClassOffset uint32 `json:"-"`
	BaseObject  PersistenceBlobObject
	Flag        uint8 `json:"-"`
	ObjectCount uint32
	Objects     []PersistenceBlobObject
}

type GuidData struct {
	A uint32
	B uint32
	C uint32
	D uint32
}

type Tables struct {
	Names   []string
	Classes []ClassEntry
}

type ArrayStructProperty struct {
	Size        uint32
	Count       uint32
	Items       []StructProperty
	ElementType string
	GUID        GuidData
}

type StructReference struct {
	GUID GuidData
}

const SKIP_CLASS_ID int32 = -0xFF

func readObject(r io.ReadSeeker, tables *Tables) (UObject, error) {
	_, err := r.Seek(9, io.SeekCurrent)
	if err != nil {
		return UObject{}, err
	}
	offset, err := memory.ReadInt[uint32](r)
	if err != nil {
		return UObject{}, err
	}

	properties, err := readProperties(r, tables)
	if err != nil {
		return UObject{}, err
	}

	return UObject{
		Offset:     offset,
		Properties: properties,
	}, nil
}

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

func readOffsets(r io.Reader) (OffsetInfo, error) {
	var offsets OffsetInfo
	err := binary.Read(r, binary.LittleEndian, &offsets)
	if err != nil {
		return offsets, err
	}

	return offsets, nil
}

func readHeader(r io.Reader) (DataHeader, error) {
	dataHeader := DataHeader{}

	err := binary.Read(r, binary.LittleEndian, &dataHeader)
	if err != nil {
		return dataHeader, err
	}

	return dataHeader, nil
}

func readNamesTable(r io.ReadSeeker, namesTableOffset uint32) ([]string, error) {
	_, err := r.Seek(int64(namesTableOffset), io.SeekStart)
	if err != nil {
		return nil, err
	}

	stringsNum, err := memory.ReadInt[int32](r)
	if err != nil {
		return nil, err
	}

	names := make([]string, stringsNum)

	for i := 0; i < int(stringsNum); i++ {
		stringData, err := ue.ReadFString(r)
		if err != nil {
			return nil, err
		}
		names[i] = stringData
	}

	return names, nil
}

func readClassesTable(r io.ReadSeeker, objectsTableOffset uint32, namesTable []string) ([]ClassEntry, error) {
	_, err := r.Seek(int64(objectsTableOffset), io.SeekStart)
	if err != nil {
		return nil, err
	}

	numUniqueObjects, err := memory.ReadInt[int32](r)
	if err != nil {
		return nil, fmt.Errorf("failed to read numUniqueClasses: %w", err)
	}

	objects := make([]ClassEntry, numUniqueObjects)

	// Read all objects/classes
	for i := 0; i < int(numUniqueObjects); i++ {
		wasLoadedByte, err := memory.ReadInt[uint8](r)
		if err != nil {
			return objects, fmt.Errorf("failed to read wasLoaded for object %d: %w", i, err)
		}
		wasLoaded := wasLoadedByte != 0

		classPathName, err := ue.ReadFString(r)
		if err != nil {
			return objects, fmt.Errorf("failed to read objectName for object %d: %w", i, err)
		}

		classData := ClassData{ID: SKIP_CLASS_ID}

		if !wasLoaded {
			objectFName, err := ue.ReadFName(r)
			if err != nil {
				return objects, fmt.Errorf("failed to read objectName for object %d (%s): %w", i, classPathName, err)
			}
			outerID2, err := memory.ReadInt[int32](r)
			if err != nil {
				return objects, fmt.Errorf("failed to read id for object %d (%s): %w", i, classPathName, err)
			}
			classData.ID = outerID2
			classData.FName = objectFName
			classData.FNameValue = namesTable[objectFName.Index]
		}

		objects[i] = ClassEntry{
			PathName: classPathName,
			Data:     classData,
		}
	}

	// for i := 0; i < len(objects); i++ {
	// 	objectID, err := memory.ReadInt[int32](r)
	// 	if err != nil {
	// 		return objects, fmt.Errorf("failed to read objectID for object %d: %w", i, err)
	// 	}
	// 	if objectID < 0 || objectID >= int32(len(objects)) {
	// 		return objects, fmt.Errorf("objectID %d is out of range", objectID)
	// 	}

	// 	objectLength, err := memory.ReadInt[uint32](r)
	// 	if err != nil {
	// 		return objects, fmt.Errorf("failed to read objectLength for object %d (%d): %w", i, objectID, err)
	// 	}

	// 	var object ue.UObject
	// 	if objectID >= 0 && objectID < int32(len(objects)) && objectLength > 0 {
	// 		object = objects[objectID]

	// 		objectStart, err := r.Seek(0, io.SeekCurrent)
	// 		if err != nil {
	// 			return objects, err
	// 		}

	// 		fmt.Printf("Reading object '%s'\n", object["name"])

	// 		if config.DEBUG_SAVE_BINARY {
	// 			objectBytes := make([]byte, objectLength)
	// 			_, err = r.Seek(objectStart, io.SeekStart)
	// 			if err != nil {
	// 				return objects, err
	// 			}
	// 			_, err = r.Read(objectBytes)
	// 			if err != nil {
	// 				return objects, err
	// 			}

	// 			utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, strconv.Itoa(i)+"_object_"+strings.Trim(object["name"].(string), "\x00"), "bin", objectBytes)
	// 			_, err = r.Seek(objectStart, io.SeekStart)
	// 			if err != nil {
	// 				return objects, err
	// 			}
	// 		}

	// 		// hack for reading BP header
	// 		if strings.HasPrefix(object["name"].(string), "BP_") {
	// 			_, err = r.Seek(4, io.SeekCurrent)
	// 			if err != nil {
	// 				return objects, err
	// 			}
	// 			dataSize, err := memory.ReadInt[int32](r)
	// 			if err != nil {
	// 				return objects, err
	// 			}
	// 			_, err = r.Seek(int64(dataSize+5), io.SeekCurrent)
	// 			if err != nil {
	// 				return objects, err
	// 			}
	// 		}
	// 		serializedObject, err := readObject(r, objectStart, objectLength, names, objects)
	// 		if err != nil {
	// 			return objects, fmt.Errorf("failed to serialize object %d (%d): %w", i, objectID, err)
	// 		}
	// 		utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, strconv.Itoa(i)+"_"+strings.Trim(object["name"].(string), "\x00"), "json", serializedObject)

	// 		// Check if we've read all the data
	// 		currentPos, err := r.Seek(0, io.SeekCurrent)
	// 		if err != nil {
	// 			return objects, err
	// 		}

	// 		if currentPos != objectStart+int64(objectLength) {
	// 			fmt.Printf("Warning: Object '%s' didn't read all its data (%d /%d; length: %d)\n", object["name"], currentPos, objectStart+int64(objectLength), objectLength)

	// 			// Correct the data position
	// 			_, err = r.Seek(objectStart+int64(objectLength), io.SeekStart)
	// 			if err != nil {
	// 				return objects, err
	// 			}
	// 		}
	// 	} else {
	// 		_, err = r.Seek(int64(objectLength), io.SeekCurrent)
	// 		if err != nil {
	// 			return objects, err
	// 		}
	// 	}

	// 	isActor, err := memory.ReadInt[uint8](r)
	// 	if err != nil {
	// 		return objects, fmt.Errorf("failed to read isActor for object %d (%d): %w", i, objectID, err)
	// 	}

	// 	if isActor != 0 {
	// 		// // Not sure about it
	// 		// // componentNameIndex, err := memory.ReadInt[int32](r)
	// 		// // if err != nil {
	// 		// // 	return objects, err
	// 		// // }
	// 		// // componentName := names[componentNameIndex]
	// 		// _, err = r.Seek(4, io.SeekCurrent)
	// 		// if err != nil {
	// 		// 	return objects, err
	// 		// }

	// 		// componentName, err := ue.ReadFString(r)
	// 		// if err != nil {
	// 		// 	return objects, fmt.Errorf("failed to read actor component name for object %d (%d): %w", i, objectID, err)
	// 		// }

	// 		// fmt.Println("Actor", componentName)

	// 		// Read Component Data
	// 		// int32 ComponentCount;
	// 		// *this << ComponentCount;

	// 		// TInlineComponentArray<UActorComponent*> ActorComponents;

	// 		// if (Actor)
	// 		// 	Actor->GetComponents(ActorComponents);

	// 		// for (int i = 0; i < ComponentCount; i++)
	// 		// {
	// 		// 	FString ComponentKey;
	// 		// 	*this << ComponentKey;

	// 		// 	uint32 ComponentLength;
	// 		// 	*this << ComponentLength;

	// 		// 	bool FoundComponent = false;

	// 		// 	for (UActorComponent* ActorComponent : ActorComponents)
	// 		// 	{
	// 		// 		if (ActorComponent->GetName() == ComponentKey)
	// 		// 		{
	// 		// 			FoundComponent = true;

	// 		// 			UE_LOG(LogGunfireSaveSystem, VeryVerbose, TEXT("  Reading component '%s' [%s]"), *ComponentKey, *ActorComponent->GetClass()->GetName());

	// 		// 			ActorComponent->Serialize(*this);

	// 		// 			break;
	// 		// 		}
	// 		// 	}

	// 		// 	// If we didn't find the named component (got renamed or removed),
	// 		// 	// just skip over the data.
	// 		// 	if (!FoundComponent)
	// 		// 	{
	// 		// 		UE_LOG(LogGunfireSaveSystem, Verbose, TEXT("  Missing component '%s', skipping %d bytes"), *ComponentKey, ComponentLength);

	// 		// 		Seek(Tell() + ComponentLength);
	// 		// 	}
	// 		// }

	// 		componentCount, err := memory.ReadInt[int32](r)
	// 		if err != nil {
	// 			return objects, fmt.Errorf("failed to read component count for object %d (%d): %w", i, objectID, err)
	// 		}

	// 		for j := 0; j < int(componentCount); j++ {
	// 			componentName, err := ue.ReadFString(r)
	// 			if err != nil {
	// 				return objects, fmt.Errorf("failed to read component name for object %d (%d): %w", i, objectID, err)
	// 			}

	// 			componentLength, err := memory.ReadInt[uint32](r)
	// 			if err != nil {
	// 				return objects, fmt.Errorf("failed to read component length for object %d (%d): %w", i, objectID, err)
	// 			}

	// 			fmt.Println("Component", componentName, componentLength)

	// 			_, err = r.Seek(int64(componentLength), io.SeekCurrent)
	// 			if err != nil {
	// 				return objects, err
	// 			}
	// 		}
	// 	}
	// }

	return objects, nil
}

func readTables(r io.ReadSeeker, offsets OffsetInfo) (Tables, error) {
	startPos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return Tables{}, err
	}

	namesTable, err := readNamesTable(r, offsets.Names)
	if err != nil {
		return Tables{}, fmt.Errorf("failed to read names table: %w", err)
	}
	classesTable, err := readClassesTable(r, offsets.Classes, namesTable)
	if err != nil {
		return Tables{}, fmt.Errorf("failed to read classes table: %w", err)
	}

	_, err = r.Seek(startPos, io.SeekStart)
	if err != nil {
		return Tables{}, err
	}
	return Tables{
		Names:   namesTable,
		Classes: classesTable,
	}, nil
}

func readArrayStructHeader(r io.ReadSeeker, tables *Tables) (ArrayStructProperty, error) {
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

	elementType, err := readName(r, tables)
	if err != nil {
		return ArrayStructProperty{}, err
	}

	var guidData GuidData
	err = binary.Read(r, binary.LittleEndian, &guidData)
	if err != nil {
		return ArrayStructProperty{}, err
	}

	_, err = r.Seek(1, io.SeekCurrent)
	if err != nil {
		return ArrayStructProperty{}, err
	}

	return ArrayStructProperty{
		ElementType: elementType,
		GUID:        guidData,
		Size:        size,
	}, nil
}

func getPropertyValue(r io.ReadSeeker, varType string, varSize uint32, tables *Tables, raw bool) (interface{}, error) {
	switch varType {
	case "IntProperty":
		{
			return readNumProperty[int32](r, raw)
		}
	case "Int16Property":
		{
			return readNumProperty[int16](r, raw)
		}
	case "Int64Property":
		{
			return readNumProperty[int64](r, raw)
		}
	case "UInt64Property":
		{
			return readNumProperty[uint64](r, raw)
		}
	case "FloatProperty":
		{
			return readNumProperty[float32](r, raw)
		}
	case "DoubleProperty":
		{
			return readNumProperty[float64](r, raw)
		}
	case "UInt16Property":
		{
			return readNumProperty[uint16](r, raw)
		}
	case "UInt32Property":
		{
			return readNumProperty[uint32](r, raw)
		}
	case "SoftClassPath":
		{
			if !raw {
				_, err := r.Seek(1, io.SeekCurrent)
				if err != nil {
					return "", err
				}
			}
			return ue.ReadFString(r)
		}
	case "SoftObjectProperty":
		{
			if !raw {
				_, err := r.Seek(1, io.SeekCurrent)
				if err != nil {
					return "", err
				}
			}
			return ue.ReadFString(r)
		}
	case "BoolProperty":
		{
			return readBoolProperty(r)
		}
	case "MapProperty":
		{
			if raw {
				log.Fatal("Raw map property is not supported yet")
			}
			return readMapProperty(r, tables)
		}
	case "EnumProperty":
		{
			return readEnumProperty(r, tables)
		}
	case "StrProperty":
		{
			return readStrProperty(r, raw)
		}
	case "TextProperty":
		{
			return readTextProperty(r, raw)
		}
	case "NameProperty":
		{
			return readNameProperty(r, tables, raw)
		}
	case "ArrayProperty":
		{
			elementsType, err := readName(r, tables)
			if err != nil {
				return nil, err
			}

			_, err = r.Seek(1, io.SeekCurrent)
			if err != nil {
				return nil, err
			}

			arrayLength, err := memory.ReadInt[uint32](r)
			if err != nil {
				return nil, err
			}

			if elementsType == "StructProperty" {
				arrayStructProperty, err := readArrayStructHeader(r, tables)
				if err != nil {
					return nil, err
				}

				items := make([]StructProperty, arrayLength)
				for i := 0; i < int(arrayLength); i++ {
					value, err := readStructProperty(r, arrayStructProperty.ElementType, arrayStructProperty.Count, tables)
					if err != nil {
						return nil, err
					}
					items[i] = StructProperty{
						Name:  arrayStructProperty.ElementType,
						Value: value,
						GUID:  arrayStructProperty.GUID,
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
				elementValue, err := getPropertyValue(r, elementsType, varSize, tables, true)
				if err != nil {
					return nil, err
				}
				result.Items[i] = elementValue
			}

			return result, nil
		}
	case "StructProperty":
		{
			if raw {
				// not sure about it
				var value GuidData
				err := binary.Read(r, binary.LittleEndian, &value)
				if err != nil {
					return nil, err
				}

				return StructReference{
					GUID: value,
				}, nil
			}

			structName, err := readName(r, tables)
			if err != nil {
				return nil, err
			}

			// 17 bytes, 16 GUID + padding?
			var guid GuidData
			err = binary.Read(r, binary.LittleEndian, &guid)
			if err != nil {
				return nil, err
			}
			_, err = r.Seek(1, io.SeekCurrent)
			if err != nil {
				return nil, err
			}

			result, err := readStructProperty(r, structName, varSize, tables)
			if err != nil {
				return nil, err
			}

			return StructProperty{
				Name:  structName,
				GUID:  guid,
				Value: result,
			}, nil
		}
	case "ObjectProperty":
		{
			return readObjectProperty(r, tables, raw)
		}
	case "ByteProperty":
		{
			return readByteProperty(r, tables, raw)
		}
	default:
		{
			log.Fatal("Raw map property is not supported yet")
		}
	}

	return nil, nil
}

func readProperty(r io.ReadSeeker, tables *Tables) (*Property, error) {
	variableName, err := readName(r, tables)
	if err != nil {
		return nil, fmt.Errorf("failed to read variable name index: %w", err)
	}

	if variableName == "None" {
		return nil, nil
	}

	varType, err := readName(r, tables)
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

	value, err := getPropertyValue(r, varType, varSize, tables, false)
	if err != nil {
		return nil, fmt.Errorf("failed to read variable data (%s %s %d): %w", variableName, varType, varSize, err)
	}

	return &Property{
		Name:  variableName,
		Type:  varType,
		Index: index,
		Size:  varSize,
		Value: value,
	}, nil
}

func readProperties(r io.ReadSeeker, tables *Tables) ([]Property, error) {
	result := []Property{}
	for {
		property, err := readProperty(r, tables)
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

func readClassAdditionalData(r io.ReadSeeker, tables *Tables) error {
	trimOffset := 0
	for i := 0; i < len(tables.Classes); i++ {
		if tables.Classes[i].Data.ID == SKIP_CLASS_ID {
			break
		}
		trimOffset++
	}

	for i := 0; i < len(tables.Classes)-trimOffset; i++ {
		id, err := memory.ReadInt[uint32](r)
		if err != nil {
			return err
		}
		length, err := memory.ReadInt[uint32](r)
		if err != nil {
			return err
		}

		if length > 0 {
			properties, err := readProperties(r, tables)
			if err != nil {
				return err
			}
			_, err = r.Seek(4, io.SeekCurrent)
			if err != nil {
				return err
			}
			tables.Classes[id].AdditionalData = properties
		}

		_, err = r.Seek(1, io.SeekCurrent)
		if err != nil {
			return err
		}
	}

	return nil
}

func ProcessData(data *[]byte) ([]UObject, error) {
	r := bytes.NewReader(*data)

	_, err := readHeader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}
	topLevelAssetPath, err := readTopLevelAssetPath(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read top level asset path: %w", err)
	}
	fmt.Println("topLevelAssetPath", topLevelAssetPath)

	offsets, err := readOffsets(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read offsets: %w", err)
	}

	tables, err := readTables(r, OffsetInfo{
		Names:   offsets.Names - 8,
		Classes: offsets.Classes - 8,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read tables: %w", err)
	}

	// baseObjectSize
	_, err = memory.ReadInt[int32](r)
	if err != nil {
		return nil, err
	}

	baseObjectProperties, err := readProperties(r, &tables)
	if err != nil {
		return nil, err
	}

	objects := []UObject{}
	objects = append(objects, UObject{
		Properties: baseObjectProperties,
	})

	for i := 0; i < 2; i++ {
		object, err := readObject(r, &tables)
		if err != nil {
			return nil, err
		}

		objects = append(objects, object)
	}

	_, err = r.Seek(5, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	err = readClassAdditionalData(r, &tables)
	if err != nil {
		return nil, err
	}

	return objects, nil
}
