// Package store opens the SQLite database and applies the schema and the demo seed.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var Schema string

//go:embed seed.sql
var Seed string

// busy_timeout matters: without it a second writer fails with SQLITE_BUSY instead of waiting,
// and a lost race looks like a lock error.
const params = "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"

// Open applies the schema to path. It applies the demo seed too when seed is true.
func Open(path string, seed bool) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+params)
	if err != nil {
		return nil, err
	}
	for _, script := range []string{Schema, seedOrEmpty(seed)} {
		if script == "" {
			continue
		}
		if _, err := db.Exec(script); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply sql: %w", err)
		}
	}
	return db, nil
}

func seedOrEmpty(seed bool) string {
	if seed {
		return Seed
	}
	return ""
}
