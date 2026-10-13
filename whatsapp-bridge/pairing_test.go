package main

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
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

func TestResolvePairPhone(t *testing.T) {
	cases := []struct {
		name, flagValue, envValue string
		want                      string
		wantErr                   bool
	}{
		{name: "unset", want: ""},
		{name: "flag digits", flagValue: "14155550123", want: "14155550123"},
		{name: "separators dropped", flagValue: "+1 (415) 555-0123", want: "14155550123"},
		{name: "env used when flag empty", envValue: "447911123456", want: "447911123456"},
		{name: "flag beats env", flagValue: "14155550123", envValue: "447911123456", want: "14155550123"},
		{name: "letters rejected", flagValue: "1415abc0123", wantErr: true},
		{name: "leading zero rejected", flagValue: "07911123456", wantErr: true},
		{name: "too short", flagValue: "123456", wantErr: true},
		{name: "too long", flagValue: "1234567890123456", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolvePairPhone(c.flagValue, c.envValue)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

type fakePairPhoner struct {
	calls   int
	phone   string
	display string
	errs    []error // returned in order for the first len(errs) calls
}

func (f *fakePairPhoner) PairPhone(_ context.Context, phone string, _ bool, _ whatsmeow.PairClientType, display string) (string, error) {
	f.calls++
	f.phone, f.display = phone, display
	if f.calls <= len(f.errs) {
		return "", f.errs[f.calls-1]
	}
	return "ABCD-1234", nil
}

func TestRunPairingWithoutPhoneUsesQRNotPairPhone(t *testing.T) {
	t.Setenv("WHATSAPP_QR_PNG_PATH", filepath.Join(t.TempDir(), "qr.png"))
	orig := openQRViewer
	openQRViewer = func(string) {}
	t.Cleanup(func() { openQRViewer = orig })

	qrChan := make(chan whatsmeow.QRChannelItem, 2)
	qrChan <- whatsmeow.QRChannelItem{Event: "code", Code: "2@abc,def,ghi"}
	qrChan <- whatsmeow.QRChannelItem{Event: "success"}
	close(qrChan)

	phoner := &fakePairPhoner{}
	if got := runPairing(context.Background(), phoner, qrChan, "", &bytes.Buffer{}); got != pairingQRSucceeded {
		t.Fatalf("outcome = %v, want succeeded", got)
	}
	if phoner.calls != 0 {
		t.Fatalf("PairPhone called %d times in QR mode", phoner.calls)
	}
}

func TestWaitForPairingCodePrintsCodeOnceAndIgnoresRotation(t *testing.T) {
	qrChan := make(chan whatsmeow.QRChannelItem, 4)
	qrChan <- whatsmeow.QRChannelItem{Event: "code", Code: "qr-1"}
	qrChan <- whatsmeow.QRChannelItem{Event: "code", Code: "qr-2"}
	qrChan <- whatsmeow.QRChannelItem{Event: "success"}
	close(qrChan)

	phoner := &fakePairPhoner{}
	var out bytes.Buffer
	got := runPairing(context.Background(), phoner, qrChan, "14155550123", &out)

	if got != pairingQRSucceeded {
		t.Fatalf("outcome = %v, want succeeded", got)
	}
	if phoner.calls != 1 || phoner.phone != "14155550123" || phoner.display != pairingCodeDisplayName {
		t.Fatalf("PairPhone calls=%d phone=%q display=%q", phoner.calls, phoner.phone, phoner.display)
	}
	if !strings.Contains(out.String(), "Pairing code: ABCD-1234") {
		t.Fatalf("code not printed: %q", out.String())
	}
	if strings.Contains(out.String(), "qr-1") || strings.Contains(out.String(), "qr-2") {
		t.Fatalf("QR data leaked into code-pairing output: %q", out.String())
	}
}

func TestWaitForPairingCodeRetriesOnNextCodeEventAfterError(t *testing.T) {
	qrChan := make(chan whatsmeow.QRChannelItem, 4)
	qrChan <- whatsmeow.QRChannelItem{Event: "code", Code: "qr-1"}
	qrChan <- whatsmeow.QRChannelItem{Event: "code", Code: "qr-2"}
	qrChan <- whatsmeow.QRChannelItem{Event: "timeout"}
	close(qrChan)

	phoner := &fakePairPhoner{errs: []error{errors.New("iq timeout")}}
	var out bytes.Buffer
	got := runPairing(context.Background(), phoner, qrChan, "14155550123", &out)

	if got != pairingQRTimedOut {
		t.Fatalf("outcome = %v, want timed out", got)
	}
	if phoner.calls != 2 {
		t.Fatalf("PairPhone calls = %d, want 2", phoner.calls)
	}
	if !strings.Contains(out.String(), "Failed to request a pairing code: iq timeout") || !strings.Contains(out.String(), "Pairing code: ABCD-1234") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}
