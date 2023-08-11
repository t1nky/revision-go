package remnant

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"revision-go/memory"
	"revision-go/ue"
)

type OffsetInfo struct {
	Names   uint64
	Version uint32
	Objects uint64
}

type UObject struct {
	ObjectID   uint32
	WasLoaded  bool
	ObjectPath string
	LoadedData *UObjectLoadedData
	Properties []Property
	Components []Component
}

type UObjectLoadedData struct {
	Name    string
	OuterID uint32
}

type Component struct {
	ComponentKey string
	Properties   []Property
}

type ArrayStructProperty struct {
	Size        uint32
	Count       uint32
	Items       []StructProperty
	ElementType string
	GUID        ue.FGuid
}

type StructReference struct {
	GUID ue.FGuid
}

type PackageVersion struct {
	UE4Version uint32
	UE5Version uint32
}

type SaveData struct {
	PackageVersion    *PackageVersion
	SaveGameClassPath *ue.FTopLevelAssetPath
	NameTableOffset   uint64
	NamesTable        []string
	ObjectsOffset     uint64
	Objects           []UObject
	Version           uint32
}

type SaveHeader struct {
	Crc                 uint32
	BytesWritten        uint32
	SaveGameFileVersion uint32 // version <= 8 -- uncompressed
	BuildNumber         uint32
}

type SaveArchive struct {
	Header SaveHeader
	Data   SaveData
}

type Variables struct {
	Name       string
	Properties []Property
}

const (
	VarTypeNone  = 0
	VarTypeBool  = 1
	VarTypeInt   = 2
	VarTypeFloat = 3
	VarTypeName  = 4
)

var VarTypeNames = map[uint8]string{
	VarTypeNone:  "None",
	VarTypeBool:  "BoolProperty",
	VarTypeInt:   "IntProperty",
	VarTypeFloat: "FloatProeprty",
	VarTypeName:  "NameProperty",
}

func readSaveHeader(r io.Reader) (SaveHeader, error) {
	dataHeader := SaveHeader{}

	err := binary.Read(r, binary.LittleEndian, &dataHeader)
	if err != nil {
		return dataHeader, err
	}

	return dataHeader, nil
}

func readPackageVersion(r io.Reader) (PackageVersion, error) {
	packageVersion := PackageVersion{}

	err := binary.Read(r, binary.LittleEndian, &packageVersion)
	if err != nil {
		return packageVersion, err
	}

	return packageVersion, nil
}

func readSaveData(r io.ReadSeeker, hasPackageVersion bool, hasTopLevelAssetPath bool) (SaveData, error) {
	result := SaveData{}
	var err error

	if hasPackageVersion {
		packageVersion, err := readPackageVersion(r)
		if err != nil {
			return result, fmt.Errorf("failed to read package version: %w", err)
		}
		result.PackageVersion = &packageVersion
	}
	if hasTopLevelAssetPath {
		saveGameClassPath, err := ue.ReadFTopLevelAssetPath(r)
		if err != nil {
			return result, fmt.Errorf("failed to read top level asset path: %w", err)
		}
		result.SaveGameClassPath = &saveGameClassPath
	}

	var offsets OffsetInfo
	err = binary.Read(r, binary.LittleEndian, &offsets)
	if err != nil {
		return result, err
	}
	objectsDataOffset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return result, err
	}

	result.NameTableOffset = offsets.Names
	result.ObjectsOffset = offsets.Objects
	result.Version = offsets.Version

	result.NamesTable, err = readNamesTable(r, offsets.Names)
	if err != nil {
		return result, fmt.Errorf("failed to read names table: %w", err)
	}

	err = readObjects(r, offsets.Objects, objectsDataOffset, &result)
	if err != nil {
		return result, fmt.Errorf("failed to read objects: %w", err)
	}

	return result, nil
}

func ReadSaveArchive(r io.ReadSeeker) (SaveArchive, error) {
	header, err := readSaveHeader(r)
	if err != nil {
		return SaveArchive{}, err
	}

	data, err := readSaveData(r, true, true)
	if err != nil {
		return SaveArchive{}, err
	}

	return SaveArchive{
		Header: header,
		Data:   data,
	}, nil
}

func readObject(r io.Reader, saveData *SaveData, objectID uint32) (UObject, error) {
	wasLoadedByte, err := memory.ReadInt[uint8](r)
	if err != nil {
		return UObject{}, err
	}

	wasLoaded := wasLoadedByte != 0

	var objectPath string
	if wasLoaded && objectID == 0 {
		if saveData.SaveGameClassPath != nil {
			objectPath = saveData.SaveGameClassPath.Path
		} else {
			objectPath, err = ue.ReadFString(r)
			if err != nil {
				return UObject{}, err
			}
		}
	} else {
		objectPath, err = ue.ReadFString(r)
		if err != nil {
			return UObject{}, err
		}
	}

	var loadedData UObjectLoadedData
	if !wasLoaded {
		objectName, err := readName(r, saveData)
		if err != nil {
			return UObject{}, err
		}

		outerID, err := memory.ReadInt[uint32](r)
		if err != nil {
			return UObject{}, err
		}

		loadedData = UObjectLoadedData{
			Name:    objectName,
			OuterID: outerID,
		}
	}

	return UObject{
		ObjectID:   objectID,
		WasLoaded:  wasLoaded,
		ObjectPath: objectPath,
		LoadedData: &loadedData,
		Properties: make([]Property, 0),
		Components: nil,
	}, nil
}

func readNamesTable(r io.ReadSeeker, namesTableOffset uint64) ([]string, error) {
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

func readVariable(r io.ReadSeeker, saveData *SaveData) (*Property, error) {
	name, err := readName(r, saveData)
	if err != nil {
		return nil, fmt.Errorf("failed to read variable name index: %w", err)
	}

	if name == "None" {
		return nil, nil
	}

	varTypeEnumValue, err := memory.ReadInt[uint8](r)
	if err != nil {
		return nil, fmt.Errorf("failed to read variable type: %w", err)
	}

	varType := VarTypeNames[varTypeEnumValue]

	var varValue interface{}

	switch varTypeEnumValue {
	case VarTypeBool:
		value, err := memory.ReadInt[uint32](r)
		if err != nil {
			return nil, fmt.Errorf("failed to read variable value: %w", err)
		}

		varValue = value != 0

	case VarTypeInt:
		value, err := memory.ReadInt[uint32](r)
		if err != nil {
			return nil, fmt.Errorf("failed to read variable value: %w", err)
		}

		varValue = int32(value)

	case VarTypeFloat:
		value, err := memory.ReadInt[uint32](r)
		if err != nil {
			return nil, fmt.Errorf("failed to read variable value: %w", err)
		}

		varValue = float32(value)

	case VarTypeName:
		value, err := readName(r, saveData)
		if err != nil {
			return nil, fmt.Errorf("failed to read variable value: %w", err)
		}

		varValue = value

	default:
		return nil, fmt.Errorf("unknown variable type: %d", varTypeEnumValue)
	}

	return &Property{
		Name:  name,
		Type:  varType,
		Value: varValue,
	}, nil
}

func readVariables(r io.ReadSeeker, saveData *SaveData) (Variables, error) {
	name, err := readName(r, saveData)
	if err != nil {
		return Variables{}, fmt.Errorf("failed to read variable name index: %w", err)
	}

	_, err = memory.ReadInt[uint64](r)
	if err != nil {
		return Variables{}, fmt.Errorf("failed to read empty value: %w", err)
	}

	arrayLength, err := memory.ReadInt[uint32](r)
	if err != nil {
		return Variables{}, fmt.Errorf("failed to read array length: %w", err)
	}

	properties := make([]Property, 0, arrayLength)

	for i := 0; i < int(arrayLength); i++ {
		property, err := readVariable(r, saveData)
		if err != nil {
			return Variables{}, fmt.Errorf("failed to read property: %w", err)
		}
		properties = append(properties, *property)
	}

	return Variables{
		Name:       name,
		Properties: properties,
	}, nil
}

func readComponents(r io.ReadSeeker, saveData *SaveData) ([]Component, error) {
	componentCount, err := memory.ReadInt[uint32](r)
	if err != nil {
		return nil, err
	}

	components := make([]Component, componentCount)

	for i := 0; i < int(componentCount); i++ {
		componentKey, err := ue.ReadFString(r)
		if err != nil {
			return nil, err
		}

		objectLength, err := memory.ReadInt[uint32](r)
		if err != nil {
			return nil, err
		}

		startPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		properties := []Property{}
		switch componentKey {
		case "GlobalVariables":
			variables, err := readVariables(r, saveData)
			if err != nil {
				return nil, err
			}
			properties = append(properties, Property{
				Name:  componentKey,
				Type:  componentKey,
				Value: variables,
			})
		case "Variables":
			variables, err := readVariables(r, saveData)
			if err != nil {
				return nil, err
			}
			properties = append(properties, Property{
				Name:  componentKey,
				Type:  componentKey,
				Value: variables,
			})
		case "Variable":
			variables, err := readVariables(r, saveData)
			if err != nil {
				return nil, err
			}
			properties = append(properties, Property{
				Name:  componentKey,
				Type:  componentKey,
				Value: variables,
			})
		case "PersistenceKeys":
			variables, err := readVariables(r, saveData)
			if err != nil {
				return nil, err
			}
			properties = append(properties, Property{
				Name:  componentKey,
				Type:  componentKey,
				Value: variables,
			})
		case "PersistanceKeys1":
			variables, err := readVariables(r, saveData)
			if err != nil {
				return nil, err
			}
			properties = append(properties, Property{
				Name:  componentKey,
				Type:  componentKey,
				Value: variables,
			})
		case "PersistenceKeys1":
			variables, err := readVariables(r, saveData)
			if err != nil {
				return nil, err
			}
			properties = append(properties, Property{
				Name:  componentKey,
				Type:  componentKey,
				Value: variables,
			})
		default:
			properties, err = readProperties(r, saveData)
			if err != nil {
				return nil, err
			}
		}

		currentPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}

		if currentPos-startPos != int64(objectLength) {
			bytes := make([]byte, startPos+int64(objectLength)-currentPos)
			_, err := r.Read(bytes)
			if err != nil {
				return nil, err
			}
			log.Printf(
				"Did not read all component data. %d/%d bytes read at %d for %s (%v)\n",
				currentPos-startPos, objectLength, startPos, componentKey, bytes,
			)
		}

		components[i] = Component{
			ComponentKey: componentKey,
			Properties:   properties,
		}
	}

	return components, nil
}

func readObjects(r io.ReadSeeker, objectsTableOffset uint64, objectsDataOffset int64, saveData *SaveData) error {
	_, err := r.Seek(int64(objectsTableOffset), io.SeekStart)
	if err != nil {
		return err
	}

	numUniqueObjects, err := memory.ReadInt[int32](r)
	if err != nil {
		return fmt.Errorf("failed to read numUniqueClasses: %w", err)
	}

	saveData.Objects = make([]UObject, numUniqueObjects)
	for i := 0; i < int(numUniqueObjects); i++ {
		saveData.Objects[i], err = readObject(r, saveData, uint32(i))
		if err != nil {
			return fmt.Errorf("failed to read object %d: %w", i, err)
		}
	}

	_, err = r.Seek(objectsDataOffset, io.SeekStart)
	if err != nil {
		return err
	}

	for i := 0; i < int(numUniqueObjects); i++ {
		objectID, err := memory.ReadInt[uint32](r)
		if err != nil {
			return fmt.Errorf("failed to read object id: %w", err)
		}
		object := saveData.Objects[objectID]

		err = readObjectData(r, &object, saveData)
		if err != nil {
			return fmt.Errorf("failed to read object data: %w", err)
		}
		saveData.Objects[objectID] = object

		isActor, err := memory.ReadInt[uint8](r)
		if err != nil {
			return fmt.Errorf("failed to read isActor: %w", err)
		}
		if isActor != 0 {
			object.Components, err = readComponents(r, saveData)
			if err != nil {
				return fmt.Errorf("failed to read components: %w", err)
			}
		}
		saveData.Objects[objectID] = object
	}

	return nil
}

func readObjectData(r io.ReadSeeker, object *UObject, saveData *SaveData) error {
	length, err := memory.ReadInt[uint32](r)
	if err != nil {
		return err
	}

	startPos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	if length > 0 {
		properties, err := readProperties(r, saveData)
		if err != nil {
			return err
		}

		currentPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}

		if currentPos-startPos != int64(length) {
			bytes := make([]byte, startPos+int64(length)-currentPos)
			_, err = r.Read(bytes)
			if err != nil {
				return err
			}
			log.Printf(
				"Did not read all object data. %d/%d bytes read at %d for %s (%v)\n",
				currentPos-startPos, length, startPos, object.ObjectPath, bytes,
			)
		}

		object.Properties = properties
	}

	return nil
}
