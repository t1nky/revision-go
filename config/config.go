package config

import "os"

var (
	DEBUG                = os.Getenv("DEBUG") != ""
	DEBUG_SAVE_DECRYPTED = os.Getenv("DEBUG_SAVE_DECRYPTED") != ""
)
