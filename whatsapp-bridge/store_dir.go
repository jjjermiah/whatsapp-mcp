package main

import (
	"os"
	"path/filepath"
	"strings"
)

// storeDir returns the directory holding messages.db, whatsapp.db,
// .bridge-token and downloaded media. WHATSAPP_STORE_DIR overrides the default
// "store" (relative to the bridge's working directory). Read per call so tests
// can toggle it.
func storeDir() string {
	if v := strings.TrimSpace(os.Getenv("WHATSAPP_STORE_DIR")); v != "" {
		return v
	}
	return "store"
}

// storePath joins elem onto storeDir().
func storePath(elem ...string) string {
	return filepath.Join(append([]string{storeDir()}, elem...)...)
}
