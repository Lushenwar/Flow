// Package store is the signed SQLite state file. Every mutable row carries an
// HMAC over its own contents; reads fail loudly rather than trusting the disk.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db  *sql.DB
	key []byte
}

const schema = `
CREATE TABLE IF NOT EXISTS kv (
  k   TEXT PRIMARY KEY,
  v   TEXT NOT NULL,
  sig BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  id   INTEGER PRIMARY KEY AUTOINCREMENT,
  ts   TEXT NOT NULL,
  kind TEXT NOT NULL,
  data TEXT NOT NULL DEFAULT '',
  sig  BLOB NOT NULL
);
`

// Open loads (or creates) the key at keyPath and the database at dbPath.
func Open(dbPath, keyPath string) (*Store, error) {
	key, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("key: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db, key: key}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Put writes a signed key/value row.
func (s *Store) Put(k, v string) error {
	_, err := s.db.Exec(`INSERT INTO kv(k,v,sig) VALUES(?,?,?)
		ON CONFLICT(k) DO UPDATE SET v=excluded.v, sig=excluded.sig`,
		k, v, sign(s.key, "kv", k, v))
	return err
}

// Get returns a value, or ErrTampered if the row no longer matches its signature.
func (s *Store) Get(k string) (string, error) {
	var v string
	var sig []byte
	err := s.db.QueryRow(`SELECT v, sig FROM kv WHERE k=?`, k).Scan(&v, &sig)
	if err != nil {
		return "", err
	}
	if !verify(s.key, sig, "kv", k, v) {
		return "", ErrTampered
	}
	return v, nil
}

// Append writes an event. The row id is inside the signature, so rows cannot be
// reordered, duplicated, or renumbered without detection.
func (s *Store) Append(kind, data string) (int64, error) {
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO events(ts,kind,data,sig) VALUES(?,?,?,?)`, ts, kind, data, []byte{})
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE events SET sig=? WHERE id=?`,
		sign(s.key, "event", itoa(id), ts, kind, data), id); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

type Event struct {
	ID   int64  `json:"id"`
	TS   string `json:"ts"`
	Kind string `json:"kind"`
	Data string `json:"data"`
}

// Events returns events with id > since, oldest first.
func (s *Store) Events(since int64) ([]Event, error) {
	rows, err := s.db.Query(`SELECT id,ts,kind,data,sig FROM events WHERE id>? ORDER BY id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var sig []byte
		if err := rows.Scan(&e.ID, &e.TS, &e.Kind, &e.Data, &sig); err != nil {
			return nil, err
		}
		if !verify(s.key, sig, "event", itoa(e.ID), e.TS, e.Kind, e.Data) {
			return out, fmt.Errorf("event %d: %w", e.ID, ErrTampered)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Verify walks every signed row and returns a description of each bad one.
// This is what `flowctl verify` reports and what /api/health summarises.
func (s *Store) Verify() ([]string, error) {
	var bad []string

	rows, err := s.db.Query(`SELECT k,v,sig FROM kv`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k, v string
		var sig []byte
		if err := rows.Scan(&k, &v, &sig); err != nil {
			rows.Close()
			return nil, err
		}
		if !verify(s.key, sig, "kv", k, v) {
			bad = append(bad, "kv/"+k)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.db.Query(`SELECT id,ts,kind,data,sig FROM events`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var ts, kind, data string
		var sig []byte
		if err := rows.Scan(&id, &ts, &kind, &data, &sig); err != nil {
			return nil, err
		}
		if !verify(s.key, sig, "event", itoa(id), ts, kind, data) {
			bad = append(bad, "events/"+itoa(id))
		}
	}
	return bad, rows.Err()
}
