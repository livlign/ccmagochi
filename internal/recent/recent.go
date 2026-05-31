// Package recent is Layer 2 — rolling, cross-session memory (hours-days). Unlike
// Layer 1 (now.json, volatile) it persists across sessions: the daemon loads it
// on start, folds each new event into it, and saves it atomically. Everything
// here is derived from the event stream, so it can be rebuilt from events.log.
//
// Layer 2 is what lets the pet say things like "first session today", "back in
// auth.go again", "third day in a row" — observations that need yesterday's
// data, not just this tick's.
package recent

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"sort"
	"time"

	"ccmagotchi/internal/events"
)

const keepDays = 21                     // prune day stats older than this
const rollHalfLifeMs = 24 * 3600 * 1000 // rolling counts halve every 24h (no cliff)

// DayStat is one calendar day's activity (local date, "2006-01-02").
type DayStat struct {
	Tools    int `json:"tools"`
	Files    int `json:"files"`
	Errors   int `json:"errors"`
	Commits  int `json:"commits"`
	Sessions int `json:"sessions"`
}

// Recent is the Layer 2 store. Maps are always non-nil after Load.
type Recent struct {
	UpdatedMs int64              `json:"updated_ms"`
	Days      map[string]DayStat `json:"days"`      // local date -> counts (for streak/today)
	FileSeen  map[string]int64   `json:"file_seen"` // path -> last-seen unix ms
	// Exponentially-decayed rolling counts (half-life 24h). Unlike the calendar
	// Days buckets these have no midnight cliff — they answer "how busy lately?"
	// smoothly. Decayed on each Observe by elapsed time since UpdatedMs.
	RollFiles  float64 `json:"roll_files"`
	RollErrors float64 `json:"roll_errors"`
}

func empty() Recent {
	return Recent{Days: map[string]DayStat{}, FileSeen: map[string]int64{}}
}

// dateOf returns the local calendar date string for a unix-ms timestamp.
func dateOf(ms int64) string { return time.UnixMilli(ms).Format("2006-01-02") }

// Load reads recent.json, returning an empty (initialized) store on any error.
func Load(path string) Recent {
	r := empty()
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &r)
		if r.Days == nil {
			r.Days = map[string]DayStat{}
		}
		if r.FileSeen == nil {
			r.FileSeen = map[string]int64{}
		}
	}
	return r
}

// Save writes recent.json atomically (temp + rename).
func (r *Recent) Save(path string) error {
	r.prune()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// prune drops day buckets older than keepDays and caps FileSeen growth.
func (r *Recent) prune() {
	if len(r.Days) > keepDays {
		dates := make([]string, 0, len(r.Days))
		for d := range r.Days {
			dates = append(dates, d)
		}
		sort.Strings(dates)
		for _, d := range dates[:len(r.Days)-keepDays] {
			delete(r.Days, d)
		}
	}
	if len(r.FileSeen) > 500 { // keep the 500 most-recently-seen files
		type fs struct {
			p  string
			ms int64
		}
		all := make([]fs, 0, len(r.FileSeen))
		for p, ms := range r.FileSeen {
			all = append(all, fs{p, ms})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].ms > all[j].ms })
		r.FileSeen = map[string]int64{}
		for _, f := range all[:500] {
			r.FileSeen[f.p] = f.ms
		}
	}
}

// Observe folds one event into the day stats and file recency. nowMs is the
// daemon clock (events carry their own ts, but we bucket by event ts).
func (r *Recent) Observe(e events.Event) {
	if e.TS == 0 {
		return
	}
	r.decayTo(e.TS) // decay rolling counts to this event's time, then add
	d := dateOf(e.TS)
	ds := r.Days[d]
	switch e.Type {
	case "tool_call":
		ds.Tools++
		if f, ok := e.Data["file"].(string); ok && f != "" {
			if _, seen := r.FileSeen[f]; !seen {
				ds.Files++
				r.RollFiles++
			}
			r.FileSeen[f] = e.TS
		}
	case "error":
		ds.Errors++
		r.RollErrors++
	case "commit":
		ds.Commits++
	}
	r.Days[d] = ds
	r.UpdatedMs = e.TS
}

// decayTo applies exponential decay to the rolling counters up to ms.
func (r *Recent) decayTo(ms int64) {
	if r.UpdatedMs > 0 && ms > r.UpdatedMs {
		f := math.Pow(0.5, float64(ms-r.UpdatedMs)/rollHalfLifeMs)
		r.RollFiles *= f
		r.RollErrors *= f
	}
}

// RollingFiles returns the decayed recent-file count as of nowMs — a smooth
// "how busy lately" measure with no midnight cliff (unlike Today().Files).
func (r *Recent) RollingFiles(nowMs int64) float64 {
	if r.UpdatedMs > 0 && nowMs > r.UpdatedMs {
		return r.RollFiles * math.Pow(0.5, float64(nowMs-r.UpdatedMs)/rollHalfLifeMs)
	}
	return r.RollFiles
}

// DaysSinceActive returns the gap (in days) between today and the most recent
// active day strictly before today — for the "been a while" long-gap callback.
// 0 if there's no prior history.
func (r *Recent) DaysSinceActive(nowMs int64) int {
	todayStr := time.UnixMilli(nowMs).Format("2006-01-02")
	todayMid, _ := time.ParseInLocation("2006-01-02", todayStr, time.Local)
	var latest time.Time
	for d, ds := range r.Days {
		if d >= todayStr || (ds.Tools == 0 && ds.Sessions == 0) {
			continue
		}
		if t, err := time.ParseInLocation("2006-01-02", d, time.Local); err == nil && t.After(latest) {
			latest = t
		}
	}
	if latest.IsZero() {
		return 0
	}
	return int(todayMid.Sub(latest).Hours() / 24) // both local-midnight → clean day diff
}

// MarkSession records that a session started at nowMs and reports whether this
// is the first session of the local day (nothing recorded for today yet).
func (r *Recent) MarkSession(nowMs int64) (firstToday bool) {
	d := dateOf(nowMs)
	ds := r.Days[d]
	firstToday = ds.Sessions == 0
	ds.Sessions++
	r.Days[d] = ds
	if nowMs > r.UpdatedMs {
		r.UpdatedMs = nowMs
	}
	return firstToday
}

// --- derived signals the triggers read ---

// Today returns today's accumulated stats.
func (r *Recent) Today(nowMs int64) DayStat { return r.Days[dateOf(nowMs)] }

// FileLastSeen returns the last-seen unix-ms for a path (0 if never).
func (r *Recent) FileLastSeen(path string) int64 { return r.FileSeen[path] }

// SeenYesterday reports whether a path was touched on the prior local day.
func (r *Recent) SeenYesterday(path string, nowMs int64) bool {
	ms, ok := r.FileSeen[path]
	if !ok {
		return false
	}
	yday := time.UnixMilli(nowMs).AddDate(0, 0, -1).Format("2006-01-02")
	return dateOf(ms) == yday
}

// ConsecutiveDays counts the active-day streak ending today (today + each prior
// day that has any recorded activity). 0 if today has none yet.
func (r *Recent) ConsecutiveDays(nowMs int64) int {
	streak := 0
	day := time.UnixMilli(nowMs)
	for {
		d := day.Format("2006-01-02")
		ds, ok := r.Days[d]
		if !ok || (ds.Tools == 0 && ds.Sessions == 0) {
			break
		}
		streak++
		day = day.AddDate(0, 0, -1)
	}
	return streak
}

// Rebuild reconstructs Layer 2 from events.log (the replayable source of truth).
// Used to bootstrap when recent.json is missing so history isn't lost.
func Rebuild(eventsPath string) Recent {
	r := empty()
	f, err := os.Open(eventsPath)
	if err != nil {
		return r
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e events.Event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			r.Observe(e)
		}
	}
	return r
}
