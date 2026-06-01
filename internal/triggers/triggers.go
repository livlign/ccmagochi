// Package triggers decides when the pet speaks. Triggered, never generated: each
// fires a hand-written phrasing — or, preferably, an improvised one composed
// from a slot grammar (no LLM) — gated by cap / cooldown / recency. Most ticks:
// silence. Categories span Layer 1 (current session), Layer 2 (recent /
// cross-session) and Layer 3 (traits / who you are).
package triggers

import (
	"math/rand"
	"strings"

	"ccmagotchi/internal/persona"
)

// View is the read-only state a trigger inspects: session signals (Layer 1),
// recent signals (Layer 2), and trait judgments (Layer 3).
type View struct {
	// Layer 1 — current session
	ToolMaxMs        int64
	ThinkMaxMs       int64
	FilesCount       int
	MaxFileRepeat    int
	LocalHour        int
	WeekendPhase     string // "eve" (Fri) | "now" (Sat/Sun) | ""
	JustDelegated    bool
	JustCommitted    bool
	JustReverted     bool
	JustTestPass     bool
	JustTestFail     bool
	IsEditing        bool
	IsReading        bool
	JustFavoriteFile bool
	JustAversion     bool
	JustError        bool // a tool just errored (spoken reaction)
	HeavyBurn        bool // output tokens/min is high (Layer 1)
	IsWorking        bool // a tool is running that isn't an edit/read (Bash, build, git…)
	PaceAnomaly      bool // within-session: pace doubled vs earlier
	// Layer 2 — recent / cross-session
	FirstSessionToday bool
	FilesToday        int
	StreakDays        int
	RevisitHours      int  // gap since a just-reopened file was last seen (0 = none)
	SameAsYesterday   bool // a file touched yesterday was reopened now
	LongGapDays       int  // days since last active before this session (0 = none)
	// Layer 3 — traits
	LateByStandards bool
	RareDay         bool
	FavoriteMonth   bool   // the just-touched file is the lifetime favorite
	FavoriteRepo    bool   // currently in the lifetime favorite repo
	PaceVerdict     string // "faster" | "slower" | ""
	UsualBreak      bool   // currently active during your usual break hour
	AnniversaryDays int    // >0 only on a milestone day
	QuirkRevert     bool   // detected: you revert soon after committing
	QuirkReads      bool   // detected: you read more than you write
}

// ambientCats are slow-changing context remarks. Once said they don't need
// repeating for a long while — they use AmbientCooldownTurns instead of the
// short per-category CooldownTurns. Activity remarks stay real-time.
var ambientCats = map[string]bool{
	"weekend_eve": true, "weekend_now": true, "late_hour": true,
	"first_session_today": true, "busy_today": true, "streak": true,
	"file_revisit": true, "same_as_yesterday": true,
	"favorite_file_month": true, "late_by_standards": true, "rare_day": true,
	"vs_usual_pace_faster": true, "vs_usual_pace_slower": true,
	"pace_anomaly": true, "usual_break": true, "long_gap": true,
	"anniversary": true, "favorite_repo": true,
	"quirk_revert_commit": true, "quirk_reads_writes": true,
	// narration-grade signals: true facts, but they change tick-to-tick, so a
	// short cooldown turns them into spam. Ambient cooldown makes them occasional.
	"heavy_burn": true, "busy": true, "many_files": true, "working": true,
}

type Engine struct {
	p        persona.Persona
	vocab    persona.Vocab
	grammar  persona.Grammar
	recent   []string       // recent remark texts (repetition avoidance)
	lastTurn map[string]int // category -> turn it last fired
	turn     int
	rng      *rand.Rand
	// learned tone (from the lexicon) — biases composition toward your style.
	terse   bool
	exclaim bool
}

// SetTone updates the learned tone the composer matches: terse trims the
// optional flourish; exclaim prefers an emphatic ending. Called each tick.
func (e *Engine) SetTone(terse, exclaim bool) { e.terse, e.exclaim = terse, exclaim }

func NewEngine(p persona.Persona, vocab persona.Vocab, grammar persona.Grammar, recent []string, seed int64) *Engine {
	return &Engine{p: p, vocab: vocab, grammar: grammar, recent: recent, lastTurn: map[string]int{}, rng: rand.New(rand.NewSource(seed))}
}

// Eval is called once per daemon tick. Returns (category, text) or ("","").
func (e *Engine) Eval(v View) (string, string) {
	e.turn++
	// ordered candidates; first eligible wins. Dev events and live activity rank
	// above slow context, so the pet reacts to what you're doing now first.
	cands := []struct {
		cat string
		ok  bool
	}{
		{"test_fail", v.JustTestFail},
		{"tool_error", v.JustError},
		{"commit", v.JustCommitted},
		{"test_pass", v.JustTestPass},
		{"revert", v.JustReverted},
		{"favorite_file", v.JustFavoriteFile},
		{"aversion", v.JustAversion},
		{"editing", v.IsEditing},
		{"reading", v.IsReading},
		{"long_thinking", v.ThinkMaxMs > e.p.LongThinkingMs},
		{"long_tool_call", v.ToolMaxMs > e.p.LongToolCallMs},
		{"same_file_repeat", v.MaxFileRepeat >= e.p.SameFileRepeat},
		{"pace_anomaly", v.PaceAnomaly},
		// Layer 2 / Layer 3 context (ambient cooldown)
		{"first_session_today", v.FirstSessionToday},
		{"vs_usual_pace_faster", v.PaceVerdict == "faster"},
		{"vs_usual_pace_slower", v.PaceVerdict == "slower"},
		{"file_revisit", v.RevisitHours > 0},
		{"same_as_yesterday", v.SameAsYesterday},
		{"favorite_file_month", v.FavoriteMonth},
		{"favorite_repo", v.FavoriteRepo},
		{"streak", v.StreakDays >= 3},
		{"late_by_standards", v.LateByStandards},
		{"rare_day", v.RareDay},
		{"usual_break", v.UsualBreak},
		{"long_gap", v.LongGapDays >= 3},
		{"anniversary", v.AnniversaryDays > 0},
		{"quirk_revert_commit", v.QuirkRevert},
		{"quirk_reads_writes", v.QuirkReads},
		{"busy_today", v.FilesToday >= 25},
		{"busy", v.FilesCount >= 10},
		{"many_files", v.FilesCount >= e.p.ManyFiles},
		{"weekend_eve", v.WeekendPhase == "eve"},
		{"weekend_now", v.WeekendPhase == "now"},
		{"late_hour", v.LocalHour < 5 || v.LocalHour >= 23},
		{"delegating", v.JustDelegated},
		// lowest priority: narration fillers. They only speak when nothing more
		// interesting is eligible, and (being ambient) rarely even then.
		{"heavy_burn", v.HeavyBurn},
		{"working", v.IsWorking},
	}
	for _, c := range cands {
		if !c.ok {
			continue
		}
		cooldown := e.p.CooldownTurns
		if ambientCats[c.cat] {
			cooldown = e.p.AmbientCooldownTurns
		}
		if last, ok := e.lastTurn[c.cat]; ok && e.turn-last < cooldown {
			continue
		}
		text := e.pick(c.cat)
		if text == "" || e.isRecent(text) {
			continue
		}
		e.lastTurn[c.cat] = e.turn
		e.recent = append(e.recent, text)
		if len(e.recent) > e.p.RecencyWindow {
			e.recent = e.recent[len(e.recent)-e.p.RecencyWindow:]
		}
		return c.cat, text
	}
	return "", ""
}

// pick prefers improvisation (compose from the slot grammar) and falls back to a
// flat vocab phrasing when the category has no grammar.
func (e *Engine) pick(cat string) string {
	if slots, ok := e.grammar[cat]; ok && len(slots) > 0 {
		return e.compose(slots)
	}
	ph := e.vocab[cat]
	if len(ph) == 0 {
		return ""
	}
	return ph[e.rng.Intn(len(ph))]
}

// compose draws one fragment from each slot and concatenates them. Fragments
// carry their own spacing, so the join is a plain concat. %-verbs pass through
// untouched for the daemon to fill. The final slot is the flourish/tail, where
// learned tone applies: terse → shortest fragment (often ""), exclaim → an
// emphatic one if available.
func (e *Engine) compose(slots [][]string) string {
	var b strings.Builder
	for i, pool := range slots {
		if len(pool) == 0 {
			continue
		}
		last := i == len(slots)-1
		b.WriteString(e.chooseFragment(pool, last))
	}
	return b.String()
}

func (e *Engine) chooseFragment(pool []string, last bool) string {
	if last && len(pool) > 1 {
		switch {
		case e.terse: // prefer the shortest tail (drops the flourish)
			short := pool[0]
			for _, f := range pool[1:] {
				if len(f) < len(short) {
					short = f
				}
			}
			return short
		case e.exclaim: // prefer an emphatic tail if one exists
			var bangs []string
			for _, f := range pool {
				if strings.Contains(f, "!") {
					bangs = append(bangs, f)
				}
			}
			if len(bangs) > 0 {
				return bangs[e.rng.Intn(len(bangs))]
			}
		}
	}
	return pool[e.rng.Intn(len(pool))]
}

func (e *Engine) isRecent(text string) bool {
	for _, r := range e.recent {
		if r == text {
			return true
		}
	}
	return false
}
