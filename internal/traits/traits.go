// Package traits is Layer 3 — the pet's slow-moving model of who you are
// (weeks-months). It accumulates lifetime distributions from the event stream
// and exposes "is this unusual for you?" judgments. Unlike Layer 2, traits gate
// their remarks on statistical readiness: a judgment fires only once enough
// samples back it (idea.md: "statistical readiness, not calendar time"). Heavy
// users reach a knowing pet in a week; light users when they've fed it enough.
package traits

import (
	"bufio"
	"encoding/json"
	"os"
	"time"

	"ccmagotchi/internal/events"
)

// readiness thresholds — how much evidence before a judgment is trustworthy.
const (
	hourReady     = 150 // tool/prompt events before the working-hours band is trusted
	dowReady      = 10  // distinct sessions before "rare day for you" is trusted
	sessionReady  = 5   // sessions before a pace baseline means anything
	fileReady     = 30  // cumulative file touches before "favorite" is meaningful
	breakReady    = 8   // observed breaks before "you usually break around now"
	repoReady     = 8   // repo visits before a "favorite repo" is meaningful
	toolKindReady = 200 // read+write tool calls before reads-vs-writes is trusted
	commitReady   = 5   // commits before the revert-after-commit quirk is trusted
)

// Traits is the Layer 3 store. Distributions are lifetime cumulative.
type Traits struct {
	HourHist     [24]int        `json:"hour_hist"`     // active events per hour-of-day
	DowHist      [7]int         `json:"dow_hist"`      // sessions per weekday (0=Sun)
	BreakHist    [24]int        `json:"break_hist"`    // breaks (active→idle) started per hour
	FileTotal    map[string]int `json:"file_total"`    // path -> lifetime touches
	RepoTotal    map[string]int `json:"repo_total"`    // repo (cwd basename) -> lifetime sessions
	Samples      int            `json:"samples"`       // total events folded (hour readiness)
	SessionCount int            `json:"session_count"` // lifetime sessions (pace/dow readiness)
	ToolTotal    int            `json:"tool_total"`    // lifetime tool calls
	ActiveMs     int64          `json:"active_ms"`     // lifetime active session time (pace base)
	Breaks       int            `json:"breaks"`        // lifetime breaks observed
	ReadTools    int            `json:"read_tools"`    // lifetime read-ish tool calls
	WriteTools   int            `json:"write_tools"`   // lifetime write-ish tool calls
	Commits      int            `json:"commits"`       // lifetime commits
	QuickReverts int            `json:"quick_reverts"` // reverts within 10min of a commit
	FirstSeenMs  int64          `json:"first_seen_ms"` // first-ever event (anniversaries)
}

func empty() Traits { return Traits{FileTotal: map[string]int{}, RepoTotal: map[string]int{}} }

// Load reads traits.json, returning an empty (initialized) store on any error.
func Load(path string) Traits {
	t := empty()
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &t)
		if t.FileTotal == nil {
			t.FileTotal = map[string]int{}
		}
		if t.RepoTotal == nil {
			t.RepoTotal = map[string]int{}
		}
	}
	return t
}

// Save writes traits.json atomically.
func (t *Traits) Save(path string) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ObserveEvent folds one classified event using its local hour.
func (t *Traits) ObserveEvent(evType string, localHour int) {
	switch evType {
	case "tool_call":
		t.ToolTotal++
		fallthrough
	case "prompt_submit":
		if localHour >= 0 && localHour < 24 {
			t.HourHist[localHour]++
		}
		t.Samples++
	}
}

// ObserveFile increments a path's lifetime touch count (called on first sight in
// a session — the daemon dedupes per session).
func (t *Traits) ObserveFile(path string) {
	if path != "" {
		t.FileTotal[path]++
	}
}

// EndSession records a finished session of the given length and start weekday.
func (t *Traits) EndSession(weekday time.Weekday, activeMs int64) {
	t.SessionCount++
	t.DowHist[int(weekday)]++
	if activeMs > 0 {
		t.ActiveMs += activeMs
	}
}

// SetFirstSeen records the first-ever event timestamp (once).
func (t *Traits) SetFirstSeen(ms int64) {
	if t.FirstSeenMs == 0 && ms > 0 {
		t.FirstSeenMs = ms
	}
}

// ObserveRepo records a session in a repo (cwd basename).
func (t *Traits) ObserveRepo(repo string) {
	if repo != "" {
		t.RepoTotal[repo]++
	}
}

// ObserveToolKind tallies read-ish vs write-ish tool calls (reads-vs-writes quirk).
func (t *Traits) ObserveToolKind(name string) {
	switch name {
	case "Read", "Grep", "Glob", "LS", "NotebookRead", "WebFetch", "WebSearch":
		t.ReadTools++
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		t.WriteTools++
	}
}

// ObserveCommit / ObserveQuickRevert track the revert-soon-after-commit quirk.
func (t *Traits) ObserveCommit()      { t.Commits++ }
func (t *Traits) ObserveQuickRevert() { t.QuickReverts++ }

// ObserveBreak records that a work break began at localHour.
func (t *Traits) ObserveBreak(localHour int) {
	if localHour >= 0 && localHour < 24 {
		t.BreakHist[localHour]++
		t.Breaks++
	}
}

// --- additional judgments ---

// UsualBreak reports whether `hour` is the user's most common break hour (so
// "you usually break around now" while still working). Needs enough breaks.
func (t *Traits) UsualBreak(hour int) bool {
	if t.Breaks < breakReady || hour < 0 || hour >= 24 {
		return false
	}
	max, at := 0, -1
	for h, n := range t.BreakHist {
		if n > max {
			max, at = n, h
		}
	}
	return at == hour && max >= 3
}

// AnniversaryDays returns days since first seen, and whether today hits a
// milestone (7 / 30 / 100 / 365 / 730 …). Only true exactly on the day.
func (t *Traits) AnniversaryDays(nowMs int64) (int, bool) {
	if t.FirstSeenMs == 0 {
		return 0, false
	}
	days := int((nowMs - t.FirstSeenMs) / (24 * 3600 * 1000))
	for _, m := range []int{7, 30, 100, 180, 365, 730, 1095} {
		if days == m {
			return days, true
		}
	}
	return days, false
}

// FavoriteRepo returns the most-visited repo once there's enough history and a
// clear leader.
func (t *Traits) FavoriteRepo() (string, bool) {
	total, bestN, secondN := 0, 0, 0
	var best string
	for r, n := range t.RepoTotal {
		total += n
		if n > bestN {
			secondN, bestN, best = bestN, n, r
		} else if n > secondN {
			secondN = n
		}
	}
	if total < repoReady || best == "" || bestN < secondN*2 {
		return "", false
	}
	return best, true
}

// ReadsMoreThanWrites reports the "reads more than writes" quirk once enough
// tool calls back it.
func (t *Traits) ReadsMoreThanWrites() bool {
	if t.ReadTools+t.WriteTools < toolKindReady {
		return false
	}
	return t.ReadTools > t.WriteTools*2
}

// RevertsAfterCommit reports the "you often revert right after committing"
// quirk once enough commits back it.
func (t *Traits) RevertsAfterCommit() bool {
	if t.Commits < commitReady {
		return false
	}
	return t.QuickReverts >= 3 && t.QuickReverts*3 >= t.Commits
}

// Rebuild reconstructs Layer 3 from events.log (the replayable source of truth),
// so improved trait logic can "get smarter retroactively". Sessions are
// segmented by activity gaps (>30min) since events.log has no session markers;
// repos can't be rebuilt (cwd isn't in the log) and accumulate forward only.
func Rebuild(eventsPath string) Traits {
	t := empty()
	f, err := os.Open(eventsPath)
	if err != nil {
		return t
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	const gapMs = 30 * 60 * 1000
	var prevTS, sessStart, lastCommitMs int64
	var sessWeekday time.Weekday
	seenFiles := map[string]bool{}
	for sc.Scan() {
		var e events.Event
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.TS == 0 {
			continue
		}
		t.SetFirstSeen(e.TS)
		// session segmentation by gap
		if prevTS == 0 {
			sessStart, sessWeekday = e.TS, time.UnixMilli(e.TS).Weekday()
		} else if e.TS-prevTS > gapMs {
			t.EndSession(sessWeekday, prevTS-sessStart)
			t.ObserveBreak(time.UnixMilli(prevTS).Hour()) // break began at the gap
			sessStart, sessWeekday = e.TS, time.UnixMilli(e.TS).Weekday()
			seenFiles = map[string]bool{}
		}
		prevTS = e.TS
		h := time.UnixMilli(e.TS).Hour()
		t.ObserveEvent(e.Type, h)
		switch e.Type {
		case "tool_call":
			if name, ok := e.Data["name"].(string); ok {
				t.ObserveToolKind(name)
			}
			if fp, ok := e.Data["file"].(string); ok && fp != "" && !seenFiles[fp] {
				seenFiles[fp] = true
				t.ObserveFile(fp)
			}
		case "commit":
			t.ObserveCommit()
			lastCommitMs = e.TS
		case "revert":
			if lastCommitMs > 0 && e.TS-lastCommitMs < 10*60*1000 {
				t.ObserveQuickRevert()
			}
		}
	}
	if prevTS > 0 { // close the final session
		t.EndSession(sessWeekday, prevTS-sessStart)
	}
	return t
}

// --- judgments (each guarded by its own readiness gate) ---

// pos maps an hour to a "working-day position" where 5am is the start of the
// day, so late-night hours (0-4) sort after the evening rather than before dawn.
func pos(hour int) int { return (hour - 5 + 24) % 24 }

// LateByStandards reports whether `hour` is later than the user usually works —
// past the 90th percentile of their active-hour distribution. Only meaningful
// once HourHist has enough samples (else returns false).
func (t *Traits) LateByStandards(hour int) bool {
	if t.Samples < hourReady {
		return false
	}
	// 90th-percentile working-day position across all active events.
	type hp struct{ p, n int }
	bins := make([]hp, 0, 24)
	for h := 0; h < 24; h++ {
		if t.HourHist[h] > 0 {
			bins = append(bins, hp{pos(h), t.HourHist[h]})
		}
	}
	// sort by position ascending (insertion — only 24 bins)
	for i := 1; i < len(bins); i++ {
		for j := i; j > 0 && bins[j].p < bins[j-1].p; j-- {
			bins[j], bins[j-1] = bins[j-1], bins[j]
		}
	}
	cutoff := int(float64(t.Samples) * 0.9)
	cum, p90 := 0, 0
	for _, b := range bins {
		cum += b.n
		if cum >= cutoff {
			p90 = b.p
			break
		}
	}
	return pos(hour) > p90
}

// RareDayOfWeek reports whether today is an unusually low-activity weekday for
// the user (this weekday has under half the average session share). Needs
// enough sessions across the week first.
func (t *Traits) RareDayOfWeek(wd time.Weekday) bool {
	if t.SessionCount < dowReady {
		return false
	}
	avg := float64(t.SessionCount) / 7.0
	return float64(t.DowHist[int(wd)]) < avg*0.5
}

// FavoriteFile returns the most-touched file (and true) once there's enough
// history and it's clearly ahead of the runner-up. Empty/false otherwise.
func (t *Traits) FavoriteFile() (string, bool) {
	total := 0
	var best string
	var bestN, secondN int
	for p, n := range t.FileTotal {
		total += n
		if n > bestN {
			secondN = bestN
			best, bestN = p, n
		} else if n > secondN {
			secondN = n
		}
	}
	if total < fileReady || best == "" || bestN < secondN*2 {
		return "", false
	}
	return best, true
}

// Pace reports the user's typical tool-calls-per-active-hour baseline, and
// whether it's trustworthy yet.
func (t *Traits) Pace() (perHour float64, ready bool) {
	if t.SessionCount < sessionReady || t.ActiveMs <= 0 {
		return 0, false
	}
	hours := float64(t.ActiveMs) / 3600000.0
	if hours <= 0 {
		return 0, false
	}
	return float64(t.ToolTotal) / hours, true
}

// PaceVerdict compares a current session's rate (tool calls / active hours) to
// the lifetime baseline: "faster", "slower", or "" (normal / not enough data).
func (t *Traits) PaceVerdict(sessionTools int, sessionActiveMs int64) string {
	base, ready := t.Pace()
	if !ready || sessionActiveMs < 5*60*1000 { // need ≥5min of session to judge
		return ""
	}
	cur := float64(sessionTools) / (float64(sessionActiveMs) / 3600000.0)
	switch {
	case cur > base*1.6:
		return "faster"
	case cur < base*0.5:
		return "slower"
	}
	return ""
}
