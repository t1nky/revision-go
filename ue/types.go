package ue

import (
	"bytes"
	"encoding/binary"
	"io"
	"revision-go/memory"
)

type FTopLevelAssetPath struct {
	Path string
	Name string
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
	return string(bytes.Trim(stringData, "\x00")), nil
}

type FName struct {
	Index  uint16
	Number int32
	Value  string
}

func ReadFName(r io.Reader) (FName, error) {
	const HAS_NUMBER = 1 << 15

	index, err := memory.ReadInt[uint16](r)
	if err != nil {
		return FName{}, err
	}

	hasNumber := index & HAS_NUMBER

	if hasNumber != 0 {
		index &^= HAS_NUMBER

		number, err := memory.ReadInt[int32](r)
		if err != nil {
			return FName{}, err
		}

		return FName{Index: index, Number: number}, nil
	}

	return FName{Index: index, Number: 0}, nil
}

type FGuid struct {
	A uint32
	B uint32
	C uint32
	D uint32
}

func ReadGuid(r io.Reader) (FGuid, error) {
	var guidData FGuid
	err := binary.Read(r, binary.LittleEndian, &guidData)
	if err != nil {
		return guidData, err
	}

	return guidData, nil
}

type FInfo struct {
	UniqueID uint64
	Offset   uint32
	Size     uint32
}

func ReadFInfo(r io.Reader) (FInfo, error) {
	var info FInfo
	err := binary.Read(r, binary.LittleEndian, &info)
	if err != nil {
		return info, err
	}

	return info, nil
}

type FVector struct {
	X float64
	Y float64
	Z float64
}

func ReadFVector(r io.Reader) (FVector, error) {
	var vector FVector
	err := binary.Read(r, binary.LittleEndian, &vector)
	if err != nil {
		return vector, err
	}

	return vector, nil
}

type FQuaternion struct {
	X float64
	Y float64
	Z float64
	W float64
}

func ReadFQuaternion(r io.Reader) (FQuaternion, error) {
	var quaternion FQuaternion
	err := binary.Read(r, binary.LittleEndian, &quaternion)
	if err != nil {
		return quaternion, err
	}

	return quaternion, nil
}

type FTransform struct {
	Rotation FQuaternion
	Position FVector
	Scale    FVector
}

func ReadFTransform(r io.Reader) (FTransform, error) {
	var transform FTransform
	err := binary.Read(r, binary.LittleEndian, &transform)
	if err != nil {
		return transform, err
	}

	return transform, nil
}

func ReadFTopLevelAssetPath(r io.Reader) (FTopLevelAssetPath, error) {
	topLevelAssetPath := FTopLevelAssetPath{}
	var err error

	topLevelAssetPath.Path, err = ReadFString(r)
	if err != nil {
		return topLevelAssetPath, err
	}

	topLevelAssetPath.Name, err = ReadFString(r)
	if err != nil {
		return topLevelAssetPath, err
	}

	return topLevelAssetPath, nil
}
