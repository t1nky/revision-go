package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"remnant-save-edit/config"
	"remnant-save-edit/remnant"
	"remnant-save-edit/ue"
	"remnant-save-edit/utils"
)

// -- Globals

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

const PADDING_SIZE = 0x8

func prepareData(chunks [][]byte) io.ReadSeeker {
	// insert header padding in the beginning
	headerPadding := make([]byte, PADDING_SIZE)
	combined := bytes.Join(chunks, []byte{})
	// add header padding back to the beginning
	// because file offsets are used in the save file
	// and they are relative to the beginning of the file (including header)
	combined = append(headerPadding, combined...)

	utils.SaveToFile(config.INPUT_FILE_NAME_WITHOUT_EXTENSION, config.INPUT_FILE_NAME_WITHOUT_EXTENSION+"_decrypted", "bin", combined)

	r := bytes.NewReader(combined)

	return r
}

func readDataHeaderAndClassPath(r io.ReadSeeker) error {
	// skip crc, savedSize, SavegameFileVersion
	_, err := r.Seek(PADDING_SIZE, io.SeekStart)
	if err != nil {
		return err
	}

	// Read build number
	chunkHeader := remnant.ChunkHeader{}
	err = binary.Read(r, binary.LittleEndian, &chunkHeader)
	if err != nil {
		return err
	}

	// read SaveGameClassPath
	saveGameClassPath, err := readTopLevelAssetPath(r)
	if err != nil {
		return err
	}
	fmt.Println(saveGameClassPath)

	return nil
}

func main() {
	chunks, err := remnant.Decompress(os.Args[1])
	if err != nil {
		panic(err)
	}

	r := prepareData(chunks)

	err = readDataHeaderAndClassPath(r)
	if err != nil {
		panic(err)
	}
	err = remnant.ProcessData(r)
	if err != nil {
		panic(err)
	}
}
