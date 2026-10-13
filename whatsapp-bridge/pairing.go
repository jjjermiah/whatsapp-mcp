package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mdp/qrterminal"
	"go.mau.fi/whatsmeow"
	"rsc.io/qr"
)

// CLI flag: pair by typing an 8-character code on the phone instead of
// scanning a QR code. Meant for headless hosts where nobody can see the
// terminal QR. WHATSAPP_PAIR_PHONE is the env equivalent; the flag wins.
var pairPhoneFlag = flag.String("pair-phone", "",
	"Pair with a code entered on the phone instead of a QR scan; value is the phone's number in international format, digits only (env: WHATSAPP_PAIR_PHONE). Only used when no session exists yet")

// pairingCodeDisplayName is shown on the phone next to the pairing code. The
// server validates it as `Browser (OS)` and rejects uncommon values with 400.
const pairingCodeDisplayName = "Chrome (Linux)"

// resolvePairPhone returns the digits-only phone number to pair with, or ""
// when code pairing was not requested. The flag value takes precedence over the
// env value. Separators people commonly type ("+", spaces, dashes, parentheses)
// are dropped; anything else is an error.
func resolvePairPhone(flagValue, envValue string) (string, error) {
	raw := strings.TrimSpace(flagValue)
	if raw == "" {
		raw = strings.TrimSpace(envValue)
	}
	if raw == "" {
		return "", nil
	}
	digits := strings.NewReplacer("+", "", " ", "", "-", "", "(", "", ")", "").Replace(raw)
	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("invalid pair phone %q: use digits only, in international format", raw)
		}
	}
	switch {
	case strings.HasPrefix(digits, "0"):
		return "", fmt.Errorf("invalid pair phone %q: use international format without a leading 0 (country code first)", raw)
	case len(digits) < 7 || len(digits) > 15:
		return "", fmt.Errorf("invalid pair phone %q: expected 7-15 digits", raw)
	}
	return digits, nil
}

// pairPhoner is the part of *whatsmeow.Client that code pairing needs.
type pairPhoner interface {
	PairPhone(ctx context.Context, phone string, showPushNotification bool, clientType whatsmeow.PairClientType, clientDisplayName string) (string, error)
}

// runPairing drains the pairing channel until pairing ends. With no phone it
// renders QR codes; with a phone it requests a pairing code instead.
func runPairing(ctx context.Context, client pairPhoner, qrChan <-chan whatsmeow.QRChannelItem, phone string, w io.Writer) pairingQROutcome {
	if phone != "" {
		return waitForPairingCode(ctx, client, qrChan, phone, w)
	}
	return renderPairingQRCodes(qrChan, w, newQRRenderer())
}

// waitForPairingCode requests a pairing code for phone once the connection is
// up (whatsmeow's docs: wait for the first QR channel item) and prints it.
// The QR codes that keep arriving are ignored, except that a failed request is
// retried on the next one. A code is valid for the lifetime of the pairing
// socket (about 160 s); after a timeout the caller's next attempt prints a new one.
func waitForPairingCode(ctx context.Context, client pairPhoner, qrChan <-chan whatsmeow.QRChannelItem, phone string, w io.Writer) pairingQROutcome {
	requested := false
	for evt := range qrChan {
		switch evt.Event {
		case "code":
			if requested {
				continue
			}
			code, err := client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, pairingCodeDisplayName)
			if err != nil {
				_, _ = fmt.Fprintf(w, "\nFailed to request a pairing code: %v\n", err)
				continue
			}
			requested = true
			_, _ = fmt.Fprintf(w, "\nPairing code: %s\n", code)
			_, _ = fmt.Fprintln(w, "On the phone: WhatsApp > Settings > Linked devices > Link a device > Link with phone number instead, then enter the code.")
			_, _ = fmt.Fprintln(w, "\nWaiting for the pairing code to be entered...")
		case "success":
			return pairingQRSucceeded
		case "timeout":
			return pairingQRTimedOut
		}
	}
	return pairingQRChannelClosed
}

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
	return storePath("qr.png")
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
