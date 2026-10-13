package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mdp/qrterminal"
	"rsc.io/qr"
)

// newQRRenderer returns a renderer that draws each pairing code in the terminal and also writes
// it to a PNG. On the first code it announces the PNG path.
func newQRRenderer() func(code string, w io.Writer) {
	announced := false
	return func(code string, w io.Writer) {
		qrterminal.GenerateHalfBlock(code, qrterminal.L, w)
		writeQRPNG(code, !announced)
		announced = true
	}
}

// qrPNGPath is where the pairing QR is written. WHATSAPP_QR_PNG_PATH overrides
// the default inside the store directory.
func qrPNGPath() string {
	if p := strings.TrimSpace(os.Getenv("WHATSAPP_QR_PNG_PATH")); p != "" {
		return p
	}
	return filepath.Join("store", "qr.png")
}

// writeQRPNGFile encodes code as a PNG at path (owner-only, parent dir created).
func writeQRPNGFile(path, code string) error {
	encoded, err := qr.Encode(code, qr.M)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, encoded.PNG(), 0o600)
}

// removeQRPNG deletes the pairing QR once pairing is over: it is a one-time
// secret and must not linger on disk.
func removeQRPNG() {
	if err := os.Remove(qrPNGPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("Could not remove %s: %v\n", qrPNGPath(), err)
	}
}

// writeQRPNG renders the pairing code to a PNG as a fallback for terminals where
// the half-block rendering cannot be scanned. Several terminal emulators (macOS
// Terminal.app among them) add line spacing between rows, which separates the
// QR modules and makes the code unreadable to a phone camera even though it
// still looks correct to a human. On a headless host the PNG is the only way to
// see the code at all: copy it off the machine and scan it.
//
// On the first code it also offers to open the file in the system viewer.
// Every failure here is non-fatal: the terminal rendering stays the primary
// path and the bridge keeps working exactly as before.
func writeQRPNG(code string, announce bool) {
	path := qrPNGPath()
	if err := writeQRPNGFile(path, code); err != nil {
		return
	}
	if !announce {
		return
	}
	fmt.Printf("If the code above will not scan, open: %s\n", path)
	openQRViewer(path)
}

// openQRViewer starts the system image viewer on path. A variable so tests do
// not pop a viewer window; on a headless host the command is simply absent.
var openQRViewer = func(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	_ = cmd.Start()
}
