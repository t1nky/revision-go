package config

import (
	"os"
	"path/filepath"
)

var (
	DEBUG             = os.Getenv("DEBUG") != ""
	DEBUG_SAVE_BINARY = os.Getenv("DEBUG_SAVE_BINARY") != ""
	DEBUG_SAVE_JSON   = os.Getenv("DEBUG_SAVE_JSON") != ""

	INPUT_FILE_NAME                   = filepath.Base(os.Args[1])
	INPUT_FILE_NAME_WITHOUT_EXTENSION = INPUT_FILE_NAME[:len(INPUT_FILE_NAME)-len(filepath.Ext(INPUT_FILE_NAME))]
)
