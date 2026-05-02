package repository

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

// NewLibSQLRepo opens a connection to a remote libSQL/Turso database.
//
// The libsql client driver registers itself under the name "libsql". The DSN
// is the database URL (typically "libsql://<db-name>-<org>.turso.io"); the
// auth token is appended as an `authToken=` query parameter.
//
// Unlike the local SQLite path, no `journal_mode`, `busy_timeout`, or
// `_txlock` URI pragmas are applied — those are either server-controlled on
// Turso or unsupported by this driver. Transactions begin as deferred; the
// `(word, status)` unique constraint remains the source of truth for
// collision resolution.
//
// The schema and SQL are SQLite-compatible, so we reuse SQLiteRepo for all
// query/scanning logic and the embedded `migrations/sqlite/*.sql` migrations.
func NewLibSQLRepo(dbURL, authToken string) (*SQLiteRepo, error) {
	dsn, err := buildLibSQLDSN(dbURL, authToken)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("libsql", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening libsql: %w", err)
	}

	// libSQL HTTP/WS connections are cheap to multiplex but each holds an
	// outbound socket; cap the pool conservatively. Writes are serialized
	// server-side regardless of pool depth.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxIdleTime(5 * time.Minute)

	return &SQLiteRepo{db: db}, nil
}

// buildLibSQLDSN normalizes the URL and merges the auth token into the query
// string. Accepts libsql://, wss://, ws://, https://, http://, or file: URLs
// (the last is mostly useful for local libsql-server testing).
func buildLibSQLDSN(dbURL, authToken string) (string, error) {
	if dbURL == "" {
		return "", fmt.Errorf("libsql url is empty")
	}

	if strings.HasPrefix(dbURL, "file:") || dbURL == ":memory:" {
		return dbURL, nil
	}

	u, err := url.Parse(dbURL)
	if err != nil {
		return "", fmt.Errorf("parsing libsql url: %w", err)
	}

	if authToken != "" {
		q := u.Query()
		if q.Get("authToken") == "" {
			q.Set("authToken", authToken)
		}
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}
