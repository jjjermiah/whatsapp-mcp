package main

import (
	"os"
	"path/filepath"
	"strings"
)

// storeDir returns the directory holding messages.db, whatsapp.db,
// .bridge-token and downloaded media. WHATSAPP_STORE_DIR overrides the default
// "store" (relative to the bridge's working directory). An override is made
// absolute so the bridge does not depend on its working directory; the MCP
// server resolves WHATSAPP_STORE_DIR against its own working directory, so set
// the same absolute path for both. Read per call so tests can toggle it.
func storeDir() string {
	if v := strings.TrimSpace(os.Getenv("WHATSAPP_STORE_DIR")); v != "" {
		if abs, err := filepath.Abs(v); err == nil {
			return abs
		}
		return v
	}
	return "store"
}

// storePath joins elem onto storeDir().
func storePath(elem ...string) string {
	return filepath.Join(append([]string{storeDir()}, elem...)...)
}
