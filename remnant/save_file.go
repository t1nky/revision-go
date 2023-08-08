package remnant

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"remnant-save-edit/ue"
)

type SaveHeader struct {
	Crc                 uint32
	BytesWritten        uint32
	SaveGameFileVersion int32 // version <= 8 -- uncompressed
}

type CompressedChunkHeader struct {
	PackageFileTag              uint64
	LoadingCompressionChunkSize uint64
	Compressor                  byte
	CompressedSize              uint64

	LoadingCompressionChunkSize2 uint64
	Size2                        uint64
	LoadingCompressionChunkSize3 uint64
}

type CompressedSaveChunk struct {
	Header CompressedChunkHeader
	Data   []byte
}

type DataHeader struct {
	UncompressedSize uint32

	BuildNumber uint32
	UE4Version  uint32
	UE5Version  uint32
}

type ProcessedData struct {
	Header            DataHeader
	SaveGameClassPath ue.FTopLevelAssetPath

	NamesOffset uint32   `json:"-"`
	NamesTable  []string `json:"-"`

	ObjectsOffset uint32 `json:"-"`
	Objects       []ClassEntry
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

func readSave(filePath string) ([]CompressedSaveChunk, error) {
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

	result := []CompressedSaveChunk{}

	for {
		compressedChunkHeader := CompressedChunkHeader{}
		err = binary.Read(file, binary.LittleEndian, &compressedChunkHeader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		data := make([]byte, compressedChunkHeader.CompressedSize)
		err = binary.Read(file, binary.LittleEndian, &data)
		if err != nil {
			return nil, err
		}

		result = append(result, CompressedSaveChunk{
			Header: compressedChunkHeader,
			Data:   data,
		})
	}

	return result, nil
}

func decompressChunks(chunks []CompressedSaveChunk) (*[]byte, error) {
	var result bytes.Buffer

	for _, chunk := range chunks {
		buf, err := decompressData(chunk.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress chunk: %w", err)
		}

		result.Write(buf)
	}

	resultBytes := result.Bytes()

	// utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, config.INPUT_FILE_NAME_WITHOUT_EXTENSION+"_decompressed", "bin", resultBytes)

	return &resultBytes, nil
}

func ReadData(filePath string) (*[]byte, error) {
	chunks, err := readSave(filePath)
	if err != nil {
		return nil, err
	}

	return decompressChunks(chunks)
}
