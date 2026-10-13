package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// run's exit code is what systemd's Restart=on-failure keys on: every fatal
// startup path must return non-zero.

func TestRun_InvalidPairPhoneIsNonZero(t *testing.T) {
	t.Setenv("WHATSAPP_PAIR_PHONE", "not-a-number")
	if got := run(); got != 1 {
		t.Fatalf("run() = %d, want 1", got)
	}
}

func TestRun_SendIntervalAboveCapIsNonZero(t *testing.T) {
	t.Setenv("WHATSAPP_SEND_MIN_INTERVAL_MS", "40000")
	if got := run(); got != 1 {
		t.Fatalf("run() = %d, want 1", got)
	}
}

func TestRun_UnusableStoreDirIsNonZero(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// A directory cannot be created below a regular file.
	t.Setenv("WHATSAPP_STORE_DIR", filepath.Join(blocker, "store"))
	if got := run(); got != 1 {
		t.Fatalf("run() = %d, want 1", got)
	}
}

func TestSleepCtx(t *testing.T) {
	if !sleepCtx(context.Background(), time.Millisecond) {
		t.Error("sleepCtx should report true after a full wait")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if sleepCtx(ctx, time.Minute) {
		t.Error("sleepCtx should report false for a cancelled context")
	}
	if time.Since(start) > 5*time.Second {
		t.Error("sleepCtx did not return promptly on cancellation")
	}
}
