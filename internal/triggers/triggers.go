// Package triggers decides when the pet speaks. v1 = 5 current-session triggers
// + a delegating aside. Triggered, never generated: each fires a hand-written
// phrasing, gated by cap / cooldown / recency. Most ticks: silence.
package triggers

import (
	"math/rand"

	"ccmagotchi/internal/persona"
)

// View is the read-only session state a trigger inspects (current-session only).
type View struct {
	ToolMaxMs     int64
	ThinkMaxMs    int64
	FilesCount    int
	MaxFileRepeat int
	LocalHour     int
	JustDelegated bool
	// v1.6 dev events (instantaneous — true only on the tick they happen)
	JustCommitted bool
	JustReverted  bool
	JustTestPass  bool
	JustTestFail  bool
	IsEditing     bool // currently editing files → "on it, editing N files"
	IsWeekend     bool // Fri/Sat/Sun → "weekend's coming"
}

type Engine struct {
	p          persona.Persona
	vocab      persona.Vocab
	recent     []string       // recent remark texts (repetition avoidance)
	count      int            // remarks this session
	lastTurn   map[string]int // category -> turn it last fired
	turn       int
	rng        *rand.Rand
}

func NewEngine(p persona.Persona, vocab persona.Vocab, recent []string, seed int64) *Engine {
	return &Engine{p: p, vocab: vocab, recent: recent, lastTurn: map[string]int{}, rng: rand.New(rand.NewSource(seed))}
}

// Eval is called once per daemon tick. Returns (category, text) or ("","").
func (e *Engine) Eval(v View) (string, string) {
	e.turn++
	if e.count >= e.p.RemarkCap {
		return "", ""
	}
	// ordered candidates; first eligible wins
	cands := []struct {
		cat string
		ok  bool
	}{
		// dev events first — they're the most salient, react immediately
		{"test_fail", v.JustTestFail},
		{"commit", v.JustCommitted},
		{"test_pass", v.JustTestPass},
		{"revert", v.JustReverted},
		{"editing", v.IsEditing},
		{"long_thinking", v.ThinkMaxMs > e.p.LongThinkingMs},
		{"long_tool_call", v.ToolMaxMs > e.p.LongToolCallMs},
		{"same_file_repeat", v.MaxFileRepeat >= e.p.SameFileRepeat},
		{"busy", v.FilesCount >= 10},
		{"many_files", v.FilesCount >= e.p.ManyFiles},
		{"weekend", v.IsWeekend},
		{"late_hour", v.LocalHour < 5 || v.LocalHour >= 23},
		{"delegating", v.JustDelegated},
	}
	for _, c := range cands {
		if !c.ok {
			continue
		}
		if last, ok := e.lastTurn[c.cat]; ok && e.turn-last < e.p.CooldownTurns {
			continue
		}
		text := e.pick(c.cat)
		if text == "" || e.isRecent(text) {
			continue
		}
		e.lastTurn[c.cat] = e.turn
		e.count++
		e.recent = append(e.recent, text)
		if len(e.recent) > e.p.RecencyWindow {
			e.recent = e.recent[len(e.recent)-e.p.RecencyWindow:]
		}
		return c.cat, text
	}
	return "", ""
}

func (e *Engine) pick(cat string) string {
	ph := e.vocab[cat]
	if len(ph) == 0 {
		return ""
	}
	return ph[e.rng.Intn(len(ph))]
}

func (e *Engine) isRecent(text string) bool {
	for _, r := range e.recent {
		if r == text {
			return true
		}
	}
	return false
}
