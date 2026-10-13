package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreDirDefaultAndOverride(t *testing.T) {
	t.Setenv("WHATSAPP_STORE_DIR", "")
	if got := storePath("whatsapp.db"); got != filepath.Join("store", "whatsapp.db") {
		t.Fatalf("default storePath = %q", got)
	}

	dir := t.TempDir()
	t.Setenv("WHATSAPP_STORE_DIR", "  "+dir+"  ")
	if got := tokenFilePath(); got != filepath.Join(dir, ".bridge-token") {
		t.Fatalf("tokenFilePath = %q", got)
	}
}

func TestNewMessageStoreHonorsStoreDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "custom store")
	t.Setenv("WHATSAPP_STORE_DIR", dir)

	ms, err := NewMessageStore()
	if err != nil {
		t.Fatalf("NewMessageStore: %v", err)
	}
	t.Cleanup(func() { _ = ms.Close() })

	if _, err := os.Stat(filepath.Join(dir, "messages.db")); err != nil {
		t.Fatalf("messages.db not created in WHATSAPP_STORE_DIR: %v", err)
	}
}

// The Python MCP server parses timestamps with datetime.fromisoformat, so the
// DSN must keep writing time.Time in mattn's "YYYY-MM-DD HH:MM:SS[.f]+HH:MM"
// layout rather than modernc's default time.Time.String().
func TestSQLiteDSNWritesISOTimestamps(t *testing.T) {
	db, err := sql.Open(sqliteDriver, sqliteDSN(filepath.Join(t.TempDir(), "t.db"), ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ts := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	if _, err := db.Exec(`CREATE TABLE t (ts TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t (ts) VALUES (?)`, ts); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow(`SELECT CAST(ts AS TEXT) FROM t`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != "2026-03-01 10:00:00+00:00" {
		t.Fatalf("stored timestamp = %q", raw)
	}

	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, err %v", fk, err)
	}
	var busy int
	if err := db.QueryRow(`PRAGMA busy_timeout`).Scan(&busy); err != nil || busy != 5000 {
		t.Fatalf("busy_timeout = %d, err %v", busy, err)
	}
}
