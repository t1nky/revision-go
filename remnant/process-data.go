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
	Name    ue.FName
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
	ObjectIndexOffset uint64
	ObjectIndex       []UObject
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
	result.ObjectIndexOffset = offsets.Objects
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
		objectName, err := ue.ReadFName(r)
		if err != nil {
			return UObject{}, err
		}
		if int(objectName.Index) < len(saveData.NamesTable) {
			objectName.Value = saveData.NamesTable[objectName.Index]
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

		properties, err := readProperties(r, saveData)
		if err != nil {
			return nil, err
		}

		if _, err := r.Seek(startPos+int64(objectLength), io.SeekStart); err != nil {
			return nil, err
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

	saveData.ObjectIndex = make([]UObject, numUniqueObjects)
	for i := 0; i < int(numUniqueObjects); i++ {
		saveData.ObjectIndex[i], err = readObject(r, saveData, uint32(i))
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
		object := saveData.ObjectIndex[objectID]

		err = readObjectData(r, &object, saveData)
		if err != nil {
			return fmt.Errorf("failed to read object data: %w", err)
		}
		saveData.ObjectIndex[objectID] = object

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
		saveData.ObjectIndex[objectID] = object
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
			log.Println(
				"Did not read all object data", currentPos-startPos, length,
				"at", startPos, "for", object.ObjectPath,
			)
			_, err = r.Seek(startPos+int64(length), io.SeekStart)
			if err != nil {
				return err
			}
		}

		object.Properties = properties
	}

	return nil
}
