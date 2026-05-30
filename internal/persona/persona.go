// Package persona holds the (user-tunable) thresholds and the remark phrasings.
// Defaults are baked in so ccmagotchi works with no config files; ~/.ccmagotchi/
// persona.json and vocab.json override them. These numbers are SOAK-TUNABLE.
package persona

import (
	"encoding/json"
	"os"
)

type Persona struct {
	LongToolCallMs int64 `json:"long_tool_call_ms"`
	LongThinkingMs int64 `json:"long_thinking_ms"`
	ManyFiles      int   `json:"many_files"`
	SameFileRepeat int   `json:"same_file_repeat"`
	IdleSeconds    int64 `json:"idle_seconds"`
	RemarkCap      int   `json:"remark_cap"`     // per session
	CooldownTurns  int   `json:"cooldown_turns"` // per trigger category
	RecencyWindow  int   `json:"recency_window"` // avoid repeating last N remarks
	RemarkHoldMs   int64 `json:"remark_hold_ms"` // how long a remark stays visible
}

func Default() Persona {
	return Persona{
		LongToolCallMs: 30000,
		LongThinkingMs: 20000,
		ManyFiles:      8,
		SameFileRepeat: 5,
		IdleSeconds:    300, // fallback silence threshold when no hook heartbeat.
		// With the heartbeat (v1.5), turnActive handles in-turn silence and idle
		// becomes responsive (~60s) post-turn; this 300s only guards no-hook sessions.
		RemarkCap:      5,
		CooldownTurns:  8,
		RecencyWindow:  20,
		RemarkHoldMs:   15000,
	}
}

func Load(path string) Persona {
	p := Default()
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &p) // present keys override; missing keep defaults
	}
	return p
}

// Vocab maps a trigger category to hand-written phrasings (~observational, deadpan).
type Vocab map[string][]string

func DefaultVocab() Vocab {
	return Vocab{
		"long_tool_call":   {"that one's taking its time.", "still chewing on that call.", "long call — patience."},
		"long_thinking":    {"deep in thought.", "a long pause. weighing something.", "quiet for a while there."},
		"many_files":       {"a lot of files open today.", "you've touched a dozen things this session.", "casting a wide net."},
		"late_hour":        {"late.", "the small hours, still here.", "past your usual, by the clock."},
		"same_file_repeat": {"back in this file again.", "third pass on the same spot.", "this one keeps pulling you back."},
		"delegating":       {"sent a helper off.", "delegating — nice.", "handed that one off."},
	}
}

func LoadVocab(path string) Vocab {
	v := DefaultVocab()
	if b, err := os.ReadFile(path); err == nil {
		var o Vocab
		if json.Unmarshal(b, &o) == nil {
			for k, val := range o {
				v[k] = val
			}
		}
	}
	return v
}
