package main

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow"
)

func TestWriteQRPNGFileWritesDecodablePNG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "qr.png")

	if err := writeQRPNGFile(path, "2@pairing-code,key,secret"); err != nil {
		t.Fatalf("writeQRPNGFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read PNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a valid PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() < 100 || b.Dx() != b.Dy() {
		t.Fatalf("unexpected QR image size %v", b)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("PNG mode = %o, want 600", perm)
	}
}

func TestQRPNGPathDefaultsToStoreAndHonoursEnv(t *testing.T) {
	t.Setenv("WHATSAPP_QR_PNG_PATH", "")
	if got, want := qrPNGPath(), filepath.Join("store", "qr.png"); got != want {
		t.Fatalf("default path = %q, want %q", got, want)
	}
	t.Setenv("WHATSAPP_QR_PNG_PATH", "/srv/pair/qr.png")
	if got := qrPNGPath(); got != "/srv/pair/qr.png" {
		t.Fatalf("env path = %q", got)
	}
}

func TestQRRendererWritesPNGAndRemoveQRPNGDeletesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qr.png")
	t.Setenv("WHATSAPP_QR_PNG_PATH", path)
	opened := ""
	orig := openQRViewer
	openQRViewer = func(p string) { opened = p }
	t.Cleanup(func() { openQRViewer = orig })

	qrChan := make(chan whatsmeow.QRChannelItem, 2)
	qrChan <- whatsmeow.QRChannelItem{Event: "code", Code: "2@abc,def,ghi"}
	qrChan <- whatsmeow.QRChannelItem{Event: "success"}
	close(qrChan)

	if got := renderPairingQRCodes(qrChan, &bytes.Buffer{}, newQRRenderer()); got != pairingQRSucceeded {
		t.Fatalf("outcome = %v, want succeeded", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("QR PNG not written: %v", err)
	}
	if opened != path {
		t.Fatalf("viewer opened %q, want %q", opened, path)
	}

	removeQRPNG()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("QR PNG still present after removeQRPNG: %v", err)
	}
}
