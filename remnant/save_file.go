package remnant

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

type CompressedChunkInfo struct {
	CompressedSize   int64
	UncompressedSize int64
}

type ChunkHeader struct {
	BuildNumber uint32
	UEVersion   [8]byte // int32 x2
	_           [4]byte // maybe related to SaveGameClassPath, struct type? F1 03 00 00
}

type CompressedChunkHeader struct {
	PackageFileTag              uint64
	LoadingCompressionChunkSize uint64
	Compressor                  byte
	Size                        uint64

	LoadingCompressionChunkSize2 uint64
	Size2                        uint64
	LoadingCompressionChunkSize3 uint64
}

type SaveHeader struct {
	Crc                 uint32
	BytesWritten        uint32
	SaveGameFileVersion int32 // version <= 8 -- uncompressed
}

const (
	PACKAGE_FILE_TAG               = 0x9E2A83C1
	PACKAGE_FILE_TAG_SWAPPED       = 0xC1832A9E
	ARCHIVE_V2_HEADER_TAG          = PACKAGE_FILE_TAG | (uint64(0x22222222) << 32)
	LOADING_COMPRESSION_CHUNK_SIZE = 131072
)

func decompressData(data []byte) ([]byte, error) {
	const maxCompressedSize = 10 * 1024 * 1024   // 10 MB
	const maxDecompressedSize = 20 * 1024 * 1024 // 20 MB

	if len(data) > maxCompressedSize {
		return nil, fmt.Errorf("compressed data is too large")
	}

	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	defer zr.Close()

	lr := io.LimitReader(zr, maxDecompressedSize)

	var buf bytes.Buffer
	_, err = io.Copy(&buf, lr)
	if err != nil {
		return nil, fmt.Errorf("failed to copy: %w", err)
	}

	return buf.Bytes(), nil
}

func Decompress(filePath string) ([][]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	saveHeader := SaveHeader{}
	err = binary.Read(file, binary.LittleEndian, &saveHeader)
	if err != nil {
		return nil, fmt.Errorf("failed to seek: %w", err)
	}

	result := [][]byte{}

	for {
		chunkHeader := CompressedChunkHeader{}
		err = binary.Read(file, binary.LittleEndian, &chunkHeader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		data := make([]byte, chunkHeader.Size)
		err = binary.Read(file, binary.LittleEndian, &data)
		if err != nil {
			return nil, err
		}

		buf, err := decompressData(data)
		if err != nil {
			return nil, fmt.Errorf("failed to read header: %w", err)
		}

		result = append(result, buf)
	}

	return result, nil
}
