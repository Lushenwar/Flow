// Package schedule holds wall-clock recurring locks.
//
// This is the one place wall-clock time is legitimately authoritative: "bedtime"
// means 23:00 local, not "seven hours of monotonic ticks". That makes schedules
// defeatable by changing timezone, so the zone is pinned at creation and every
// evaluation happens in the pinned zone regardless of what the system says now.
package schedule

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Schedule struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	ListIDs []string `json:"listIds"`
	// Start and End are "HH:MM" in the pinned zone. End before Start means the
	// window crosses midnight, which is what "bedtime" always does.
	Start string `json:"start"`
	End   string `json:"end"`
	// Days the window may START on. Empty means every day.
	Days []time.Weekday `json:"days,omitempty"`
	// TZ is pinned at creation and signed with the row. Changing the system zone
	// does not move the window.
	TZ string `json:"tz"`
	// OffsetSeconds is the UTC offset captured at creation, and it is what
	// actually pins the window on Windows.
	//
	// time.Local.String() is the literal "Local", and LoadLocation("Local")
	// resolves to whatever the machine claims right now — so storing that name
	// alone pins nothing at all, which is exactly the attack this is supposed to
	// stop. Go has no stdlib Windows-to-IANA mapping, so the offset is the
	// portable anchor. Cost: a window pinned this way does not follow DST.
	OffsetSeconds int  `json:"offsetSeconds"`
	Enabled       bool `json:"enabled"`
}

// New pins the current zone at creation.
func New(id, name string, listIDs []string, start, end string, loc *time.Location) (Schedule, error) {
	_, offset := time.Now().In(loc).Zone()
	s := Schedule{
		ID: id, Name: name, ListIDs: listIDs,
		Start: start, End: end,
		TZ: loc.String(), OffsetSeconds: offset, Enabled: true,
	}
	if _, err := parseHHMM(start); err != nil {
		return Schedule{}, fmt.Errorf("start: %w", err)
	}
	if _, err := parseHHMM(end); err != nil {
		return Schedule{}, fmt.Errorf("end: %w", err)
	}
	if start == end {
		return Schedule{}, fmt.Errorf("start and end are the same; a zero-length window blocks nothing")
	}
	return s, nil
}

func parseHHMM(s string) (int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("want HH:MM, got %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("bad hour in %q", s)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("bad minute in %q", s)
	}
	return h*60 + m, nil
}

// Location resolves the pinned zone.
//
// "Local" and "" are refused outright: both resolve to the machine's current
// zone, which is the thing being defended against. A zone the OS no longer
// knows falls back the same way. In all those cases the captured offset is
// used, so the window stays where it was created.
func (s Schedule) Location() *time.Location {
	if s.TZ != "" && s.TZ != "Local" {
		if loc, err := time.LoadLocation(s.TZ); err == nil && loc != nil {
			return loc
		}
	}
	return time.FixedZone(s.pinnedName(), s.OffsetSeconds)
}

func (s Schedule) pinnedName() string {
	if s.OffsetSeconds == 0 {
		return "UTC"
	}
	sign, mins := "+", s.OffsetSeconds/60
	if mins < 0 {
		sign, mins = "-", -mins
	}
	return fmt.Sprintf("UTC%s%02d:%02d", sign, mins/60, mins%60)
}

// ActiveAt reports whether the window covers the given instant, evaluated in the
// pinned zone.
func (s Schedule) ActiveAt(t time.Time) bool {
	if !s.Enabled {
		return false
	}
	start, err := parseHHMM(s.Start)
	if err != nil {
		return false
	}
	end, err := parseHHMM(s.End)
	if err != nil {
		return false
	}

	local := t.In(s.Location())
	mins := local.Hour()*60 + local.Minute()

	if start < end {
		// Same-day window: 18:00-21:00.
		return s.dayAllowed(local.Weekday()) && mins >= start && mins < end
	}
	// Crosses midnight: 23:00-07:00. Before the end time belongs to the window
	// that started YESTERDAY, so that is the day whose schedule must allow it.
	if mins >= start {
		return s.dayAllowed(local.Weekday())
	}
	if mins < end {
		return s.dayAllowed(local.AddDate(0, 0, -1).Weekday())
	}
	return false
}

func (s Schedule) dayAllowed(d time.Weekday) bool {
	if len(s.Days) == 0 {
		return true
	}
	for _, allowed := range s.Days {
		if allowed == d {
			return true
		}
	}
	return false
}

// EndsAt returns the instant this window closes, in the pinned zone. Used to
// hold a lock for its full length when the system zone has been changed
// underneath it.
func (s Schedule) EndsAt(t time.Time) time.Time {
	end, err := parseHHMM(s.End)
	if err != nil {
		return t
	}
	local := t.In(s.Location())
	closing := time.Date(local.Year(), local.Month(), local.Day(), end/60, end%60, 0, 0, s.Location())
	if !closing.After(local) {
		closing = closing.AddDate(0, 0, 1)
	}
	return closing
}

// Set is the collection, kept sorted by id so rendering and signing are stable.
type Set struct {
	Schedules []Schedule `json:"schedules"`
}

func (s *Set) Add(sc Schedule) {
	for i, existing := range s.Schedules {
		if existing.ID == sc.ID {
			s.Schedules[i] = sc
			return
		}
	}
	s.Schedules = append(s.Schedules, sc)
	sort.Slice(s.Schedules, func(i, j int) bool { return s.Schedules[i].ID < s.Schedules[j].ID })
}

// ActiveListIDs is what the union consumes.
func (s *Set) ActiveListIDs(now time.Time) []string {
	seen := map[string]bool{}
	var out []string
	for _, sc := range s.Schedules {
		if !sc.ActiveAt(now) {
			continue
		}
		for _, id := range sc.ListIDs {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Active returns the schedules currently in force.
func (s *Set) Active(now time.Time) []Schedule {
	var out []Schedule
	for _, sc := range s.Schedules {
		if sc.ActiveAt(now) {
			out = append(out, sc)
		}
	}
	return out
}

// Defaults are the two schedules from the presets table.
func Defaults(loc *time.Location) *Set {
	set := &Set{}
	bedtime, err := New("sched.bedtime", "Bedtime", []string{"preset.bedtime"}, "23:00", "07:00", loc)
	if err == nil {
		bedtime.Enabled = false // seeded, not imposed
		set.Add(bedtime)
	}
	delivery, err := New("sched.delivery", "Food delivery", []string{"preset.delivery"}, "18:00", "21:00", loc)
	if err == nil {
		delivery.Enabled = false
		set.Add(delivery)
	}
	return set
}
