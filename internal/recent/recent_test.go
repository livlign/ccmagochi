package recent

import (
	"path/filepath"
	"testing"
	"time"

	"ccmagotchi/internal/events"
)

func ms(date string, hour int) int64 {
	t, _ := time.ParseInLocation("2006-01-02", date, time.Local)
	return t.Add(time.Duration(hour) * time.Hour).UnixMilli()
}

func TestObserve_CountsByDayAndFile(t *testing.T) {
	r := empty()
	r.Observe(events.Event{TS: ms("2026-05-31", 10), Type: "tool_call", Data: map[string]any{"file": "a.go"}})
	r.Observe(events.Event{TS: ms("2026-05-31", 11), Type: "tool_call", Data: map[string]any{"file": "a.go"}}) // same file again
	r.Observe(events.Event{TS: ms("2026-05-31", 12), Type: "error"})
	d := r.Today(ms("2026-05-31", 13))
	if d.Tools != 2 {
		t.Errorf("want 2 tools, got %d", d.Tools)
	}
	if d.Files != 1 {
		t.Errorf("distinct files should be 1 (a.go twice), got %d", d.Files)
	}
	if d.Errors != 1 {
		t.Errorf("want 1 error, got %d", d.Errors)
	}
}

func TestMarkSession_FirstToday(t *testing.T) {
	r := empty()
	if !r.MarkSession(ms("2026-05-31", 9)) {
		t.Error("first session of the day should report firstToday=true")
	}
	if r.MarkSession(ms("2026-05-31", 14)) {
		t.Error("second session same day should report firstToday=false")
	}
	if !r.MarkSession(ms("2026-06-01", 9)) {
		t.Error("first session of a new day should report firstToday=true")
	}
}

func TestConsecutiveDays_Streak(t *testing.T) {
	r := empty()
	for _, d := range []string{"2026-05-29", "2026-05-30", "2026-05-31"} {
		r.Observe(events.Event{TS: ms(d, 10), Type: "tool_call"})
	}
	if got := r.ConsecutiveDays(ms("2026-05-31", 12)); got != 3 {
		t.Errorf("want 3-day streak, got %d", got)
	}
	// a gap breaks it
	r2 := empty()
	r2.Observe(events.Event{TS: ms("2026-05-28", 10), Type: "tool_call"})
	r2.Observe(events.Event{TS: ms("2026-05-31", 10), Type: "tool_call"})
	if got := r2.ConsecutiveDays(ms("2026-05-31", 12)); got != 1 {
		t.Errorf("gap should reset streak to 1, got %d", got)
	}
}

func TestSeenYesterday(t *testing.T) {
	r := empty()
	r.Observe(events.Event{TS: ms("2026-05-30", 15), Type: "tool_call", Data: map[string]any{"file": "auth.go"}})
	if !r.SeenYesterday("auth.go", ms("2026-05-31", 9)) {
		t.Error("auth.go seen yesterday should be true")
	}
	if r.SeenYesterday("auth.go", ms("2026-06-02", 9)) {
		t.Error("two days later is not 'yesterday'")
	}
}

// Rolling counts decay exponentially (no midnight cliff): a file touched today
// is "heavier" than one from 2 days ago.
func TestRollingFiles_Decays(t *testing.T) {
	r := empty()
	r.Observe(events.Event{TS: ms("2026-05-29", 10), Type: "tool_call", Data: map[string]any{"file": "old.go"}})
	r.Observe(events.Event{TS: ms("2026-05-31", 10), Type: "tool_call", Data: map[string]any{"file": "new.go"}})
	// two distinct files, but the older one has decayed (~2 days, half-life 24h →
	// ~0.25 weight), so the rolling total is well under 2.
	roll := r.RollingFiles(ms("2026-05-31", 10))
	if roll >= 2.0 || roll <= 1.0 {
		t.Errorf("decayed rolling files should be between 1 and 2, got %v", roll)
	}
}

func TestDaysSinceActive(t *testing.T) {
	r := empty()
	r.Observe(events.Event{TS: ms("2026-05-25", 10), Type: "tool_call"})
	if got := r.DaysSinceActive(ms("2026-05-31", 9)); got != 6 {
		t.Errorf("want 6-day gap, got %d", got)
	}
	e := empty()
	if got := e.DaysSinceActive(ms("2026-05-31", 9)); got != 0 {
		t.Errorf("no history → 0 gap, got %d", got)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "recent.json")
	r := empty()
	r.Observe(events.Event{TS: ms("2026-05-31", 10), Type: "commit"})
	if err := r.Save(p); err != nil {
		t.Fatal(err)
	}
	got := Load(p)
	if got.Today(ms("2026-05-31", 11)).Commits != 1 {
		t.Errorf("commit count did not survive round-trip: %+v", got)
	}
}
