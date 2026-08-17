package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	db := filepath.Join(dir, "state.db")
	s, err := Open(db, filepath.Join(dir, "key.bin"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, db
}

// edit is what a user with sqlite3.exe does.
func edit(t *testing.T, dbPath, stmt string) {
	t.Helper()
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(stmt); err != nil {
		t.Fatal(err)
	}
}

func TestKVRoundTrip(t *testing.T) {
	s, _ := open(t)
	if err := s.Put("mode", "commitment"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("mode")
	if err != nil || got != "commitment" {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := s.Put("mode", "pomodoro"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get("mode"); got != "pomodoro" {
		t.Fatalf("upsert did not take: %q", got)
	}
}

func TestKVTamperRejected(t *testing.T) {
	s, dbPath := open(t)
	if err := s.Put("targetAt", "2026-08-04T14:52:11Z"); err != nil {
		t.Fatal(err)
	}
	edit(t, dbPath, `UPDATE kv SET v='1970-01-01T00:00:00Z' WHERE k='targetAt'`)

	if _, err := s.Get("targetAt"); !errors.Is(err, ErrTampered) {
		t.Fatalf("edited row must not read back clean, got %v", err)
	}
	bad, err := s.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 1 || bad[0] != "kv/targetAt" {
		t.Fatalf("Verify missed the edit: %v", bad)
	}
}

func TestEventsSurviveAndDetectRenumbering(t *testing.T) {
	s, dbPath := open(t)
	for _, k := range []string{"service_start", "clock_drift"} {
		if _, err := s.Append(k, "{}"); err != nil {
			t.Fatal(err)
		}
	}
	evs, err := s.Events(0, 0)
	if err != nil || len(evs) != 2 {
		t.Fatalf("got %d events, %v", len(evs), err)
	}
	// Newest first. The limit is applied in SQL, so the ordering is what decides
	// which end a LIMIT keeps — ascending would cap the log at its oldest rows.
	if evs[0].Kind != "clock_drift" || evs[1].Kind != "service_start" {
		t.Fatalf("order or content wrong: %+v", evs)
	}
	if got, _ := s.Events(evs[1].ID, 0); len(got) != 1 || got[0].Kind != "clock_drift" {
		t.Fatalf("since filter ignored: %+v", got)
	}

	// The id is inside the signature, so moving a row breaks it.
	edit(t, dbPath, `UPDATE events SET id=99 WHERE kind='clock_drift'`)
	if _, err := s.Events(0, 0); !errors.Is(err, ErrTampered) {
		t.Fatalf("renumbered event must not verify, got %v", err)
	}
}

// The limit is applied in SQL because the cost that matters is one HMAC per
// returned row, not the bytes on the wire.
func TestEventsLimitIsAppliedAndKeepsTheNewest(t *testing.T) {
	s, _ := open(t)
	for i := 0; i < 10; i++ {
		if _, err := s.Append("session_commit", itoa(int64(i))); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Events(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("limit ignored: %d rows", len(got))
	}
	if got[0].Data != "9" {
		t.Fatalf("kept the wrong end: newest is %q", got[0].Data)
	}

	if all, _ := s.Events(0, 0); len(all) != 10 {
		t.Fatalf("a zero limit should mean the default, got %d", len(all))
	}
}

func TestKeyPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	db, key := filepath.Join(dir, "state.db"), filepath.Join(dir, "key.bin")

	s, err := Open(db, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("k", "v"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// A reboot must not invalidate every signed row written before it.
	s2, err := Open(db, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got, err := s2.Get("k"); err != nil || got != "v" {
		t.Fatalf("reopen lost the key: %q, %v", got, err)
	}
}
