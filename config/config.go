package config

import "os"

var (
	DEBUG                = os.Getenv("DEBUG") != ""
	DEBUG_SAVE_DECRYPTED = os.Getenv("DEBUG_SAVE_DECRYPTED") != ""
	DEBUG_SAVE_BINARY    = os.Getenv("DEBUG_SAVE_BINARY") != ""
	DEBUG_SAVE_JSON      = os.Getenv("DEBUG_SAVE_JSON") != ""
)
