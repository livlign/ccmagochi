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
		LongToolCallMs: 30000, // kept high so warnings stay sane
		LongThinkingMs: 12000,
		ManyFiles:      4,
		SameFileRepeat: 3,
		IdleSeconds:    300, // fallback silence threshold when no hook heartbeat.
		// With the heartbeat (v1.5), turnActive handles in-turn silence and idle
		// becomes responsive (~60s) post-turn; this 300s only guards no-hook sessions.
		// v1.6 — talkative (overrides idea.md "silence is default"; tune in persona.json):
		RemarkCap:     40,
		CooldownTurns: 3,
		RecencyWindow: 20,
		RemarkHoldMs:  7000,
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
		"long_tool_call":   {"Hmm, that's taking a while…", "Still on that one…", "Patience — long call.", "This tool's deep in it.", "Hang tight…", "Chewing on that…"},
		"long_thinking":    {"Ooh, tough one — thinking…", "That's a hard question, let me think…", "Hmm, mulling this over…", "Deep thoughts here…", "Give me a sec…", "Thinking hard on this…"},
		"many_files":       {"Whew, that's a lot today!", "Lots of files in flight!", "Busy everywhere at once!", "So many things going on!", "Big session, huh?", "We're all over the place!"},
		"late_hour":        {"It's late — still going?", "The small hours… night owl.", "Past bedtime, by the clock!", "Burning the midnight oil!", "Late-night coding, I see.", "Should we call it soon?"},
		"same_file_repeat": {"Back in this file again!", "You and this file, huh?", "Round and round here…", "This one keeps calling you back.", "Third time's the charm?", "Can't quit this file!"},
		"delegating":       {"Sent a helper off!", "Delegating — smart.", "A subagent's on it!", "Outsourced that one.", "Help is on the way!", "Letting someone else dig in…"},
		"editing":          {"On it — editing %d files…", "Heads down, %d files in flight…", "Working through %d files…", "Editing away — %d so far…"},
		"busy":             {"Whew, that's a lot of work!", "Big day today, huh?", "We're cooking — lots going on!", "Lots of moving parts today!"},
		"weekend":          {"Weekend's almost here — any plans?", "Friday vibes! Wrapping up?", "Weekend's coming — got plans?", "Almost the weekend!"},
		// quirks (v1.7)
		"favorite_file": {"Ooh, a %s file — love these!", "%s, my favorite!", "Ah, a %s. nice.", "A %s? yes please."},
		"aversion":      {"Ugh, %s again.", "Not %s…", "%s? if we must.", "oh, %s. fine."},
		// dev events
		"commit":    {"Shipped! Nice one.", "Committed — clean!", "Another one in the books!", "Saved! git's happy.", "Locked it in!", "Boom — committed!"},
		"revert":    {"Oops — taking that back.", "Undo. It happens!", "Reverting — no harm.", "Scrapping that one.", "Rewind!", "That one didn't make it."},
		"test_pass": {"Yay — all green!", "Tests pass! Nice.", "All green, woo!", "Clean run!", "Passing — love it.", "Green across the board!"},
		"test_fail": {"Oops! Something broke.", "Tests are unhappy…", "Red. Back to it!", "Something's off…", "The suite's complaining.", "Uh oh — failing."},
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
