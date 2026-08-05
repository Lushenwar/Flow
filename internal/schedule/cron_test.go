package schedule

import (
	"slices"
	"testing"
	"time"
)

var nyc = mustLoad("America/New_York")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func at(t *testing.T, loc *time.Location, day int, hour, min int) time.Time {
	t.Helper()
	return time.Date(2026, 8, day, hour, min, 0, 0, loc)
}

func TestSameDayWindow(t *testing.T) {
	s, err := New("d", "Delivery", []string{"preset.delivery"}, "18:00", "21:00", nyc)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		hour, min int
		want      bool
	}{
		{17, 59, false},
		{18, 0, true},
		{20, 59, true},
		{21, 0, false}, // end is exclusive, or two adjacent windows would overlap
		{23, 0, false},
	}
	for _, c := range cases {
		if got := s.ActiveAt(at(t, nyc, 4, c.hour, c.min)); got != c.want {
			t.Errorf("%02d:%02d active=%v, want %v", c.hour, c.min, got, c.want)
		}
	}
}

// Bedtime is the case a naive start<end comparison gets wrong.
func TestWindowCrossingMidnight(t *testing.T) {
	s, _ := New("b", "Bedtime", []string{"preset.bedtime"}, "23:00", "07:00", nyc)

	cases := []struct {
		day, hour int
		want      bool
	}{
		{4, 22, false},
		{4, 23, true},
		{5, 0, true},
		{5, 3, true},
		{5, 6, true},
		{5, 7, false},
		{5, 12, false},
	}
	for _, c := range cases {
		if got := s.ActiveAt(at(t, nyc, c.day, c.hour, 0)); got != c.want {
			t.Errorf("day %d %02d:00 active=%v, want %v", c.day, c.hour, got, c.want)
		}
	}
}

// Threat model: change the timezone to slip out of a scheduled lock early.
func TestTimezoneChangeDoesNotMoveTheWindow(t *testing.T) {
	s, _ := New("b", "Bedtime", []string{"preset.bedtime"}, "23:00", "07:00", nyc)

	// 01:00 in New York is inside the window.
	instant := at(t, nyc, 5, 1, 0)
	if !s.ActiveAt(instant) {
		t.Fatal("setup: should be locked")
	}

	// The same instant, read in Tokyo, is 14:00 — outside a naive local check.
	// Evaluation happens in the pinned zone, so the answer must not change.
	tokyo := mustLoad("Asia/Tokyo")
	if !s.ActiveAt(instant.In(tokyo)) {
		t.Fatal("a timezone change must not end a scheduled lock early")
	}
	if s.TZ != nyc.String() {
		t.Fatalf("zone drifted to %q", s.TZ)
	}
}

// The zone must never resolve to "whatever the machine says now" — that is the
// thing being defended against.
func TestUnresolvableZonesUseTheCapturedOffsetNotLocal(t *testing.T) {
	// "Local" is what time.Local.String() returns on Windows, and
	// LoadLocation("Local") follows the machine. Pinning that name pins nothing.
	for _, tz := range []string{"Local", "", "Mars/Olympus"} {
		s := Schedule{
			ID: "x", Start: "23:00", End: "07:00",
			TZ: tz, OffsetSeconds: -5 * 3600, Enabled: true,
		}
		loc := s.Location()
		if loc == time.Local {
			t.Fatalf("tz %q resolved to the machine's current zone", tz)
		}
		probe := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC).In(loc)
		if _, off := probe.Zone(); off != -5*3600 {
			t.Fatalf("tz %q gave offset %d, want the captured -18000", tz, off)
		}
	}
}

// The whole point: a window created at UTC-5 stays at UTC-5 after the machine moves.
func TestPinnedOffsetHoldsTheWindowWhenTheMachineMoves(t *testing.T) {
	s := Schedule{
		ID: "b", Start: "23:00", End: "07:00",
		TZ: "Local", OffsetSeconds: -5 * 3600, Enabled: true,
	}
	// 03:00 at UTC-5 is 08:00 UTC, inside a 23:00-07:00 window.
	instant := time.Date(2026, 8, 5, 8, 0, 0, 0, time.UTC)
	if !s.ActiveAt(instant) {
		t.Fatal("setup: should be locked")
	}
	// The same instant read anywhere else must not change the answer.
	if !s.ActiveAt(instant.In(mustLoad("Asia/Tokyo"))) {
		t.Fatal("a timezone change must not end a scheduled lock early")
	}
}

func TestNewCapturesTheOffset(t *testing.T) {
	s, err := New("b", "Bedtime", nil, "23:00", "07:00", nyc)
	if err != nil {
		t.Fatal(err)
	}
	_, want := time.Now().In(nyc).Zone()
	if s.OffsetSeconds != want {
		t.Fatalf("offset %d, want %d", s.OffsetSeconds, want)
	}
}

func TestDayFiltering(t *testing.T) {
	s, _ := New("w", "Weekdays", []string{"preset.doomscroll"}, "09:00", "17:00", nyc)
	s.Days = []time.Weekday{time.Monday, time.Tuesday}

	// 2026-08-03 is a Monday.
	if !s.ActiveAt(at(t, nyc, 3, 10, 0)) {
		t.Error("Monday should be active")
	}
	if s.ActiveAt(at(t, nyc, 5, 10, 0)) {
		t.Error("Wednesday should not be active")
	}
}

// A midnight-crossing window belongs to the day it STARTED on.
func TestOvernightWindowUsesTheStartingDay(t *testing.T) {
	s, _ := New("f", "Friday night", []string{"preset.bedtime"}, "23:00", "07:00", nyc)
	s.Days = []time.Weekday{time.Friday}

	// 2026-08-07 is a Friday.
	if !s.ActiveAt(at(t, nyc, 7, 23, 30)) {
		t.Error("Friday 23:30 should be active")
	}
	if !s.ActiveAt(at(t, nyc, 8, 2, 0)) {
		t.Error("Saturday 02:00 belongs to the window that started Friday")
	}
	if s.ActiveAt(at(t, nyc, 8, 23, 30)) {
		t.Error("Saturday 23:30 starts a window Saturday, which is not allowed")
	}
}

func TestDisabledScheduleNeverFires(t *testing.T) {
	s, _ := New("b", "Bedtime", []string{"preset.bedtime"}, "23:00", "07:00", nyc)
	s.Enabled = false
	if s.ActiveAt(at(t, nyc, 5, 1, 0)) {
		t.Fatal("a disabled schedule must not lock")
	}
}

func TestMalformedTimesAreRejectedAtCreation(t *testing.T) {
	for _, c := range [][2]string{{"25:00", "07:00"}, {"23:00", "7:99"}, {"nope", "07:00"}, {"23:00", "23:00"}} {
		if _, err := New("x", "x", nil, c[0], c[1], nyc); err == nil {
			t.Errorf("accepted %v", c)
		}
	}
}

func TestSetCollectsActiveLists(t *testing.T) {
	set := &Set{}
	bed, _ := New("b", "Bedtime", []string{"preset.bedtime"}, "23:00", "07:00", nyc)
	del, _ := New("d", "Delivery", []string{"preset.delivery"}, "18:00", "21:00", nyc)
	set.Add(bed)
	set.Add(del)

	if got := set.ActiveListIDs(at(t, nyc, 4, 19, 0)); !slices.Equal(got, []string{"preset.delivery"}) {
		t.Fatalf("19:00 got %v", got)
	}
	if got := set.ActiveListIDs(at(t, nyc, 5, 2, 0)); !slices.Equal(got, []string{"preset.bedtime"}) {
		t.Fatalf("02:00 got %v", got)
	}
	if got := set.ActiveListIDs(at(t, nyc, 4, 12, 0)); len(got) != 0 {
		t.Fatalf("midday got %v", got)
	}
}

func TestAddReplacesByID(t *testing.T) {
	set := &Set{}
	a, _ := New("b", "Bedtime", []string{"preset.bedtime"}, "23:00", "07:00", nyc)
	set.Add(a)
	b, _ := New("b", "Bedtime", []string{"preset.bedtime"}, "22:00", "06:00", nyc)
	set.Add(b)

	if len(set.Schedules) != 1 {
		t.Fatalf("got %d schedules", len(set.Schedules))
	}
	if set.Schedules[0].Start != "22:00" {
		t.Fatalf("not replaced: %v", set.Schedules[0])
	}
}

func TestEndsAtIsInThePinnedZone(t *testing.T) {
	s, _ := New("b", "Bedtime", []string{"preset.bedtime"}, "23:00", "07:00", nyc)
	got := s.EndsAt(at(t, nyc, 5, 1, 0)).In(nyc)

	if got.Hour() != 7 || got.Day() != 5 {
		t.Fatalf("ends at %v, want 07:00 the same morning", got)
	}
}

func TestDefaultsAreSeededOff(t *testing.T) {
	set := Defaults(nyc)
	if len(set.Schedules) != 2 {
		t.Fatalf("got %d", len(set.Schedules))
	}
	for _, s := range set.Schedules {
		if s.Enabled {
			t.Fatalf("%s ships enabled; a blocker that starts locking without being asked is a trap", s.ID)
		}
	}
}
