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

// DefaultEventLimit bounds an unasked-for read. The history list renders eight
// rows; nothing in the app has ever needed the whole log in one response.
const DefaultEventLimit = 100

// Events returns the newest events with id > since, newest first.
//
// The limit is applied in SQL, not by the caller, because the cost that matters
// is the HMAC verification of every returned row. Without it this walked and
// re-verified the entire history on every call — and the UI called it every five
// seconds to draw eight lines.
//
// A limit <= 0 means DefaultEventLimit. There is deliberately no way to ask for
// everything over HTTP.
func (s *Store) Events(since int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = DefaultEventLimit
	}
	rows, err := s.db.Query(
		`SELECT id,ts,kind,data,sig FROM events WHERE id>? ORDER BY id DESC LIMIT ?`,
		since, limit)
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

// Retention bounds the event log.
//
// The log is a tamper-evidence record, so pruning has to be distinguishable
// from an attacker deleting rows: Prune writes a log_pruned event naming the
// count and the oldest surviving id. A gap in the ids with no log_pruned row
// before it is evidence; a gap with one is housekeeping. Without that, pruning
// destroys the property the signing exists to provide.
const RetentionDays = 90

// RetentionFloor is the number of newest rows kept whatever their age, so a
// quiet machine does not lose its whole history to the calendar.
//
// A var rather than a const so a test can lower it and exercise a real prune;
// nothing in the daemon writes to it.
var RetentionFloor = 10000

// Prune deletes events older than RetentionDays, keeping at least
// RetentionFloor of the newest rows. Returns how many were removed.
func (s *Store) Prune() (int64, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -RetentionDays).Format(time.RFC3339Nano)

	// The floor is expressed as "never touch the newest N", which is why this is
	// a subquery rather than a plain ts comparison.
	res, err := s.db.Exec(`
		DELETE FROM events
		WHERE ts < ?
		  AND id NOT IN (SELECT id FROM events ORDER BY id DESC LIMIT ?)`,
		cutoff, RetentionFloor)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return 0, err
	}

	var oldest int64
	s.db.QueryRow(`SELECT COALESCE(MIN(id), 0) FROM events`).Scan(&oldest)
	if _, err := s.Append("log_pruned", fmt.Sprintf(`{"removed":%d,"oldestId":%d}`, n, oldest)); err != nil {
		return n, err
	}
	return n, nil
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
