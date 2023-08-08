package ue

import (
	"encoding/binary"
	"io"
	"revision-go/memory"
)

type FTopLevelAssetPath struct {
	PackageName string
	AssetName   string
}

type FName struct {
	Index  uint16
	Number int32
}

func ReadFString(r io.Reader) (string, error) {
	stringSize, err := memory.ReadInt[int32](r)
	if err != nil {
		return "", err
	}
	if stringSize <= 0 {
		return "", nil
	}
	stringData := make([]byte, stringSize)
	err = binary.Read(r, binary.LittleEndian, &stringData)
	if err != nil {
		return "", err
	}
	return string(stringData[:stringSize-1]), nil
}

func ReadFName(r io.Reader) (FName, error) {
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
