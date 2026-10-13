package main

import (
	"net/url"
	"strings"

	// Pure-Go SQLite driver (no cgo), so the bridge builds with CGO_ENABLED=0
	// into a single static binary.
	_ "modernc.org/sqlite"
)

// sqliteDriver is the database/sql (and whatsmeow dbutil) name modernc.org/sqlite
// registers. whatsmeow accepts any dialect string starting with "sqlite".
const sqliteDriver = "sqlite"

// sqliteDSN builds a SQLite URI for path that keeps the behavior the bridge had
// under mattn/go-sqlite3:
//   - _busy_timeout=5000: mattn defaults to 5s, modernc to none, and the bridge
//     plus MCP server share these files.
//   - _foreign_keys=on: required by whatsmeow's sqlstore and messages.db.
//   - _time_format=sqlite: write time.Time as "2006-01-02 15:04:05.999999999-07:00"
//     (mattn's format) rather than time.Time.String(). The Python MCP server
//     reads these columns with datetime.fromisoformat.
//
// extra is appended as-is, e.g. "mode=ro".
func sqliteDSN(path, extra string) string {
	params := []string{"_busy_timeout=5000", "_foreign_keys=on", "_time_format=sqlite"}
	if extra != "" {
		params = append(params, extra)
	}
	u := url.URL{Path: path}
	return "file:" + u.EscapedPath() + "?" + strings.Join(params, "&")
}
