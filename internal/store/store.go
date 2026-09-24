// Package store persists the counterexample archive in SQLite. Source
// bytes and their interpretation are stored separately; every identity is
// content-derived so export/import keeps identities, order and provenance
// intact. All mutations happen inside transactions: a failed import rolls
// back completely.
package store

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database.
type Store struct{ db *sql.DB }

// ID builds a stable, content-derived identifier.
func ID(prefix string, parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return prefix + "-" + hex.EncodeToString(h.Sum(nil))[:16]
}

// HashBytes hashes raw content for blob identity.
func HashBytes(b []byte) string {
	h := sha1.Sum(b)
	return "blob-" + hex.EncodeToString(h[:])[:16]
}

var migrations = []string{
	`CREATE TABLE blobs(hash TEXT PRIMARY KEY, content BLOB NOT NULL);
CREATE TABLE imports(id TEXT PRIMARY KEY, name TEXT NOT NULL, kind TEXT NOT NULL, blob TEXT NOT NULL);
CREATE TABLE signals(id TEXT PRIMARY KEY, import_id TEXT NOT NULL, name TEXT NOT NULL, ordinal INTEGER NOT NULL);
CREATE TABLE changes(id TEXT PRIMARY KEY, signal_id TEXT NOT NULL, t INTEGER NOT NULL, raw TEXT NOT NULL, value TEXT NOT NULL, ok INTEGER NOT NULL, ordinal INTEGER NOT NULL);
CREATE TABLE timescales(import_id TEXT PRIMARY KEY, raw TEXT NOT NULL, num INTEGER NOT NULL, unit TEXT NOT NULL, scale_fs INTEGER NOT NULL);
CREATE TABLE diagnostics(id TEXT PRIMARY KEY, import_id TEXT NOT NULL, where_ TEXT NOT NULL, raw TEXT NOT NULL, reason TEXT NOT NULL);
CREATE TABLE clocks(id TEXT PRIMARY KEY, import_id TEXT NOT NULL, name TEXT NOT NULL, signal TEXT NOT NULL, edge TEXT NOT NULL, period INTEGER NOT NULL, unit TEXT NOT NULL);
CREATE TABLE resets(id TEXT PRIMARY KEY, import_id TEXT NOT NULL, signal TEXT NOT NULL, from_t INTEGER NOT NULL, to_t INTEGER NOT NULL);
CREATE TABLE properties(id TEXT PRIMARY KEY, import_id TEXT NOT NULL, name TEXT NOT NULL, clock TEXT NOT NULL, ast TEXT NOT NULL, src TEXT NOT NULL, ordinal INTEGER NOT NULL);
CREATE TABLE param_versions(id TEXT PRIMARY KEY, parent_id TEXT, json TEXT NOT NULL, note TEXT NOT NULL);
CREATE TABLE branches(id TEXT PRIMARY KEY, name TEXT NOT NULL, parent_id TEXT, base_event_id TEXT, head_count INTEGER NOT NULL DEFAULT 0);
CREATE TABLE events(id TEXT PRIMARY KEY, branch_id TEXT NOT NULL, seq INTEGER NOT NULL, kind TEXT NOT NULL, payload TEXT NOT NULL, refs TEXT NOT NULL);
CREATE TABLE runs(id TEXT PRIMARY KEY, property_id TEXT NOT NULL, semantics TEXT NOT NULL, params_id TEXT NOT NULL, waveform_import_id TEXT NOT NULL, verdict TEXT NOT NULL, vacuous INTEGER NOT NULL, disabled INTEGER NOT NULL, input_hash TEXT NOT NULL);
CREATE TABLE branch_runs(branch_id TEXT NOT NULL, run_id TEXT NOT NULL, seq INTEGER NOT NULL, stale INTEGER NOT NULL DEFAULT 0, stale_reason TEXT NOT NULL DEFAULT '', PRIMARY KEY(branch_id, run_id));
CREATE TABLE threads(id TEXT PRIMARY KEY, run_id TEXT NOT NULL, start_tick INTEGER NOT NULL, status TEXT NOT NULL, verdict TEXT NOT NULL, vacuous INTEGER NOT NULL, disabled INTEGER NOT NULL);
CREATE TABLE steps(id TEXT PRIMARY KEY, thread_id TEXT NOT NULL, tick INTEGER NOT NULL, time INTEGER NOT NULL, region TEXT NOT NULL, detail TEXT NOT NULL, reads_json TEXT NOT NULL, ordinal INTEGER NOT NULL);
CREATE TABLE candidates(id TEXT PRIMARY KEY, run_id TEXT NOT NULL, kind TEXT NOT NULL, label TEXT NOT NULL, payload TEXT NOT NULL, verdict TEXT NOT NULL, ordinal INTEGER NOT NULL);
CREATE TABLE adjudications(id TEXT PRIMARY KEY, branch_id TEXT NOT NULL, seq INTEGER NOT NULL, candidate_id TEXT NOT NULL, decision TEXT NOT NULL, note TEXT NOT NULL, event_id TEXT NOT NULL, undone_seq INTEGER NOT NULL DEFAULT 0);
CREATE TABLE deps(run_id TEXT PRIMARY KEY, deps_json TEXT NOT NULL);
CREATE TABLE kv(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE INDEX idx_changes_signal ON changes(signal_id, t);
CREATE INDEX idx_events_branch ON events(branch_id, seq);
CREATE INDEX idx_branch_runs ON branch_runs(branch_id, seq);`,
}

// Open opens (or creates) the database and applies pending migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied TEXT NOT NULL DEFAULT '')`); err != nil {
		return err
	}
	var have int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&have); err != nil {
		return err
	}
	if have > len(migrations) {
		return fmt.Errorf("store: database has %d migrations, binary knows %d", have, len(migrations))
	}
	for i := have; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range strings.Split(migrations[i], ";\n") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("store: migration %d: %w", i+1, err)
			}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the raw handle for the export/import and query helpers.
func (s *Store) DB() *sql.DB { return s.db }
