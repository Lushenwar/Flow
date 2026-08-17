package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func schedules(t *testing.T, h http.Handler) []scheduleView {
	t.Helper()
	var out []scheduleView
	if err := json.NewDecoder(do(t, h, "GET", "/api/schedules", nil).Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func find(rows []scheduleView, id string) *scheduleView {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

// Only the two seeded rows could be toggled before this. Creating one is the
// whole point of the third row type.
func TestScheduleCanBeCreatedAndDeleted(t *testing.T) {
	h, _ := sessionServer(t)

	rec := do(t, h, "POST", "/api/schedules", schedulePut{
		ID: "sched.mine", Name: "Evenings", ListIDs: []string{"preset.doomscroll"},
		Start: "20:00", End: "22:00", Enabled: true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create returned %d", rec.Code)
	}
	got := find(schedules(t, h), "sched.mine")
	if got == nil {
		t.Fatal("schedule not stored")
	}
	if got.Start != "20:00" || !got.Enabled {
		t.Fatalf("stored wrong: %+v", got)
	}

	if code := do(t, h, "DELETE", "/api/schedules/sched.mine", nil).Code; code != http.StatusOK {
		t.Fatalf("delete returned %d", code)
	}
	if find(schedules(t, h), "sched.mine") != nil {
		t.Fatal("still listed after delete")
	}
}

// Days existed on the model and not on the wire, so a schedule created over
// HTTP was every-day whatever the caller sent.
func TestScheduleDaysSurviveTheAPI(t *testing.T) {
	h, _ := sessionServer(t)

	if code := do(t, h, "POST", "/api/schedules", schedulePut{
		ID: "sched.weekdays", Name: "Weekdays", ListIDs: []string{"preset.doomscroll"},
		Start: "09:00", End: "17:00", Enabled: true,
		Days: []int{1, 2, 3, 4, 5},
	}).Code; code != http.StatusOK {
		t.Fatalf("create returned %d", code)
	}

	got := find(schedules(t, h), "sched.weekdays")
	if got == nil {
		t.Fatal("not stored")
	}
	if len(got.Days) != 5 || got.Days[0] != time.Monday {
		t.Fatalf("days lost in transit: %v", got.Days)
	}

	if code := do(t, h, "POST", "/api/schedules", schedulePut{
		ID: "sched.bad", Name: "Bad", Start: "09:00", End: "17:00", Days: []int{9},
	}).Code; code != http.StatusBadRequest {
		t.Fatalf("day 9 returned %d, want 400", code)
	}
}

// Editing or deleting a schedule whose window is live weakens enforcement on
// the same terms a baseline toggle would. There is no delay to invent: the
// window ends on its own.
func TestALiveScheduleCannotBeEditedOrDeleted(t *testing.T) {
	h, c := sessionServer(t)

	// Schedules pin the machine's zone at creation, so the test clock has to be
	// read in that zone too — a UTC instant is a different hour of the window.
	c.wall = time.Date(2026, 8, 4, 12, 0, 0, 0, time.Local)
	if code := do(t, h, "POST", "/api/schedules", schedulePut{
		ID: "sched.evening", Name: "Evening", ListIDs: []string{"preset.doomscroll"},
		Start: "18:00", End: "21:00", Enabled: true,
	}).Code; code != http.StatusOK {
		t.Fatalf("create returned %d", code)
	}

	// Now it is in force.
	c.wall = time.Date(2026, 8, 4, 19, 0, 0, 0, time.Local)
	if got := find(schedules(t, h), "sched.evening"); got == nil || !got.Active {
		t.Fatalf("setup: schedule should be live, got %+v", got)
	}

	rec := do(t, h, "POST", "/api/schedules", schedulePut{
		ID: "sched.evening", Name: "Evening", ListIDs: []string{"preset.doomscroll"},
		Start: "18:00", End: "18:30", Enabled: true, // shorten it to escape now
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("editing a live schedule returned %d, want 409", rec.Code)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "would_weaken" {
		t.Fatalf("error %q", body["error"])
	}

	if code := do(t, h, "DELETE", "/api/schedules/sched.evening", nil).Code; code != http.StatusConflict {
		t.Fatalf("deleting a live schedule returned %d, want 409", code)
	}
	if got := find(schedules(t, h), "sched.evening"); got == nil || got.End != "21:00" {
		t.Fatalf("a refused edit changed something: %+v", got)
	}

	// After the window closes, both are ordinary operations again.
	c.wall = time.Date(2026, 8, 4, 22, 0, 0, 0, time.Local)
	if code := do(t, h, "DELETE", "/api/schedules/sched.evening", nil).Code; code != http.StatusOK {
		t.Fatalf("delete after the window returned %d", code)
	}
}
