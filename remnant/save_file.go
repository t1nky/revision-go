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

type SaveFile struct {
	Crc32       uint32
	ContentSize uint32
	Version     uint32
	Chunks      []CompressedSaveChunk
}

const (
	PACKAGE_FILE_TAG               = 0x9E2A83C1
	PACKAGE_FILE_TAG_SWAPPED       = 0xC1832A9E
	ARCHIVE_V2_HEADER_TAG          = PACKAGE_FILE_TAG | (uint64(0x22222222) << 32)
	LOADING_COMPRESSION_CHUNK_SIZE = 131072
)

func decompressData(data []byte) ([]byte, error) {
	const maxCompressedSize = 20 * 1024 * 1024   // 20 MB
	const maxDecompressedSize = 40 * 1024 * 1024 // 40 MB

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

func readSave(filePath string) (*SaveFile, error) {
	file, err := os.Open(filePath)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	var crc32 uint32
	err = binary.Read(file, binary.LittleEndian, &crc32)
	if err != nil {
		return nil, err
	}

	var contentSize uint32
	err = binary.Read(file, binary.LittleEndian, &contentSize)
	if err != nil {
		return nil, err
	}

	var version uint32
	err = binary.Read(file, binary.LittleEndian, &version)
	if err != nil {
		return nil, err
	}

	chunks := []CompressedSaveChunk{}

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

		chunks = append(chunks, CompressedSaveChunk{
			Header: compressedChunkHeader,
			Data:   data,
		})
	}

	return &SaveFile{
		Crc32:       crc32,
		ContentSize: contentSize,
		Version:     version,
		Chunks:      chunks,
	}, nil
}

func decompressChunks(saveFile *SaveFile) ([]byte, error) {
	var result bytes.Buffer

	err := binary.Write(&result, binary.LittleEndian, saveFile.Crc32)
	if err != nil {
		return nil, err
	}

	err = binary.Write(&result, binary.LittleEndian, saveFile.ContentSize)
	if err != nil {
		return nil, err
	}

	for _, chunk := range saveFile.Chunks {
		buf, err := decompressData(chunk.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress chunk: %w", err)
		}

		result.Write(buf)
	}

	data := result.Bytes()
	binary.LittleEndian.PutUint32(data[8:], uint32(saveFile.Version))

	return data, nil
}

func ReadData(filePath string) ([]byte, error) {
	saveFile, err := readSave(filePath)
	if err != nil {
		return nil, err
	}

	return decompressChunks(saveFile)
}
