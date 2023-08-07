package memory

import (
	"encoding/binary"
	"io"
)

type Int interface {
	int | uint | int8 | uint8 | int16 | uint16 | int32 | uint32 | int64 | uint64
}

func ReadInt[T Int](r io.Reader) (T, error) {
	var value T
	err := binary.Read(r, binary.LittleEndian, &value)
	if err != nil {
		return 0, err
	}
	return value, nil
}
