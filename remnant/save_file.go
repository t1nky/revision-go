package remnant

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"remnant-save-edit/config"
	"remnant-save-edit/memory"
	"remnant-save-edit/ue"
	"remnant-save-edit/utils"
	"strconv"
	"strings"
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

func readObject(r io.ReadSeeker, objectStart int64, maxLength uint32, names []string, objects []ue.UObject) (map[string]interface{}, error) {
	variables := map[string]interface{}{}

	for {
		variableNameIndex, err := memory.ReadInt[int16](r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return variables, err
		}
		variableName := names[variableNameIndex]

		varTypeIndex, err := memory.ReadInt[int16](r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return variables, err

		}
		varType := names[varTypeIndex]

		if variableName == "None" {
			_, err = r.Seek(2, io.SeekCurrent) // IT WAS 4 BEFORE PARSING PERSISTENCE BLOB
			if err != nil {
				return variables, err
			}
		} else {
			varSize, err := memory.ReadInt[int32](r)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return variables, err
			}

			// unknown data
			_, err = r.Seek(4, io.SeekCurrent)
			if err != nil {
				return variables, err
			}

			varData, err := ReadProperty(r, varType, varSize, names, objects, false)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return variables, err
			}

			variables[variableName] = varData
		}

		currentPos, err := r.Seek(0, io.SeekCurrent)
		if err != nil {
			return variables, err
		}

		if currentPos-objectStart >= int64(maxLength) {
			break
		}
	}

	return variables, nil
}

func readBaseObject(r io.ReadSeeker, names []string) error {
	var objects []ue.UObject

	objectIndexPos, err := memory.ReadInt[int64](r)
	if err != nil {
		return err
	}

	startPos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	_, err = r.Seek(objectIndexPos, io.SeekStart)
	if err != nil {
		return err
	}

	numUniqueObjects, err := memory.ReadInt[int32](r)
	if err != nil {
		return err
	}

	// Assuming baseObject is an empty object.
	baseObject := ue.UObject{"name": "BaseObject[SaveGame]"}

	objects = make([]ue.UObject, numUniqueObjects)

	// Read all objects/classes
	for i := 0; i < int(numUniqueObjects); i++ {
		wasLoaded, err := memory.ReadInt[uint8](r)
		if err != nil {
			return err
		}

		objectName, err := ue.ReadFString(r)
		if err != nil {
			return err
		}

		var object ue.UObject
		if wasLoaded != 0 && i == 0 {
			object = baseObject
		} else {
			// FindObject and LoadObject logic is replaced with loading from a predefined map or creating a new empty object
			object = ue.UObject{"name": objectName, "index": i}
		}

		if wasLoaded != 0 {
			objects[i] = object
		} else {
			objectName, err := ue.ReadFName(r)
			if err != nil {
				return err
			}
			outerID, err := memory.ReadInt[int32](r)
			if err != nil {
				return err
			}
			object = ue.UObject{"name": names[objectName.Index], "index": objectName.Index, "outerId": outerID}
			objects[i] = object
		}
	}

	_, err = r.Seek(startPos, io.SeekStart)
	if err != nil {
		return err
	}

	for i := 0; i < len(objects); i++ {
		objectID, err := memory.ReadInt[int32](r)
		if err != nil {
			return err
		}

		objectLength, err := memory.ReadInt[uint32](r)
		if err != nil {
			return err
		}

		var object ue.UObject
		if objectID >= 0 && objectID < int32(len(objects)) && objectLength > 0 {
			object = objects[objectID]

			objectStart, err := r.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}

			fmt.Printf("Reading object '%s'\n", object["name"])

			if config.DEBUG_SAVE_BINARY {
				objectBytes := make([]byte, objectLength)
				_, err = r.Seek(objectStart, io.SeekStart)
				if err != nil {
					return err
				}
				_, err = r.Read(objectBytes)
				if err != nil {
					return err
				}

				utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, strconv.Itoa(i)+"_object_"+strings.Trim(object["name"].(string), "\x00"), "bin", objectBytes)
				_, err = r.Seek(objectStart, io.SeekStart)
				if err != nil {
					return err
				}
			}

			// hack for reading BP header
			if strings.HasPrefix(object["name"].(string), "BP_") {
				_, err = r.Seek(4, io.SeekCurrent)
				if err != nil {
					return err
				}
				dataSize, err := memory.ReadInt[int32](r)
				if err != nil {
					return err
				}
				_, err = r.Seek(int64(dataSize+5), io.SeekCurrent)
				if err != nil {
					return err
				}
			}
			serializedObject, err := readObject(r, objectStart, objectLength, names, objects)
			if err != nil {
				return err
			}
			utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, strconv.Itoa(i)+"_"+strings.Trim(object["name"].(string), "\x00"), "json", serializedObject)

			// Check if we've read all the data
			currentPos, err := r.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}

			if currentPos != objectStart+int64(objectLength) {
				fmt.Printf("Warning: Object '%s' didn't read all its data (%d /%d; length: %d)\n", object["name"], currentPos, objectStart+int64(objectLength), objectLength)

				// Correct the data position
				_, err = r.Seek(objectStart+int64(objectLength), io.SeekStart)
				if err != nil {
					return err
				}
			}
		} else {
			_, err = r.Seek(int64(objectLength), io.SeekCurrent)
			if err != nil {
				return err
			}
		}

		isActor, err := memory.ReadInt[uint8](r)
		if err != nil {
			return err
		}

		if isActor != 0 {
			// Not sure about it
			// componentNameIndex, err := memory.ReadInt[int32](r)
			// if err != nil {
			// 	return err
			// }
			// componentName := names[componentNameIndex]
			_, err = r.Seek(4, io.SeekCurrent)
			if err != nil {
				return err
			}

			componentName, err := ue.ReadFString(r)
			if err != nil {
				return err
			}

			fmt.Println("Actor", componentName)
		}
	}

	return nil
}

func ProcessData(r io.ReadSeeker) error {
	var names []string

	// Read strings table
	stringsTableOffset, err := memory.ReadInt[int64](r)
	if err != nil {
		return err
	}

	startPos, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	_, err = r.Seek(stringsTableOffset, io.SeekStart)
	if err != nil {
		return err
	}

	stringsNum, err := memory.ReadInt[int32](r)
	if err != nil {
		return err
	}

	names = make([]string, stringsNum)

	for i := 0; i < int(stringsNum); i++ {
		stringData, err := ue.ReadFString(r)
		if err != nil {
			return err
		}
		names[i] = stringData
	}

	_, err = r.Seek(startPos, io.SeekStart)
	if err != nil {
		return err
	}

	// read version
	// version, err = memory.ReadInt[int32](r) // version
	_, err = r.Seek(4, io.SeekCurrent)
	if err != nil {
		return err
	}

	return readBaseObject(r, names)
}
