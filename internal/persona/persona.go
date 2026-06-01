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
	CooldownTurns  int   `json:"cooldown_turns"` // per trigger category
	// AmbientCooldownTurns is the (much longer) cooldown for slow-changing
	// context remarks — weekend, late hour. These facts don't change minute to
	// minute, so the pet says them rarely. Activity remarks (editing, tool
	// calls, dev events) stay on CooldownTurns and can fire in real time.
	AmbientCooldownTurns int   `json:"ambient_cooldown_turns"`
	RecencyWindow        int   `json:"recency_window"` // avoid repeating last N remarks
	RemarkHoldMs         int64 `json:"remark_hold_ms"` // how long a remark stays visible
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
		CooldownTurns:        3,
		AmbientCooldownTurns: 1800, // ~30 min at ~1 tick/s — weekend/late-hour say rarely
		RecencyWindow:        20,
		RemarkHoldMs:         7000,
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
		"late_hour":        {"It's late — still going?", "The small hours… night owl.", "Past bedtime, by the clock!", "Burning the midnight oil!", "Late-night coding, I see.", "Should we call it soon?"},
		"same_file_repeat": {"Back in this file again!", "You and this file, huh?", "Round and round here…", "This one keeps calling you back.", "Third time's the charm?", "Can't quit this file!"},
		"delegating":       {"Sent a helper off!", "Delegating — smart.", "A subagent's on it!", "Outsourced that one.", "Help is on the way!", "Letting someone else dig in…"},
		"editing":          {"On it — editing %d files…", "Heads down, %d files in flight…", "Working through %d files…", "Editing away — %d so far…"},
		"reading":          {"Reading through %d files…", "Skimming %d files…", "Having a look — %d files…", "Catching up on %d files…"},
		"busy":             {"Lots of moving parts this session!", "Plenty in flight right now.", "Busy stretch — lots going on."},
		"working":          {"Heads down over here…", "Still at it…", "Plugging away…", "In the weeds…"},
		"many_files":       {"That's a fair few files this session.", "Lots of files open.", "Quite a few files this session."},
		// #3: day-aware weekend (never says "Friday" on a Sunday)
		"weekend_eve": {"Friday — winding down?", "Almost the weekend!", "Weekend's nearly here.", "Friday already, huh?"},
		"weekend_now": {"Weekend coding, huh?", "It's the weekend — dedication.", "Weekend session…", "Working through the weekend!"},
		// Layer 2 (recent) fallbacks
		"first_session_today": {"First session today.", "Day's first run — let's go.", "Starting fresh today."},
		"busy_today":          {"Busy day — %d files already.", "Big day today (%d files).", "Lots done today, %d files in."},
		"streak":              {"%d days running!", "Day %d in a row.", "%d-day streak — on a roll."},
		"file_revisit":        {"First time back in %s in %dh…", "%s again — been %dh.", "Returning to %s after %dh."},
		"same_as_yesterday":   {"%s — same as yesterday.", "Back in %s from yesterday.", "%s again, like yesterday."},
		// Layer 3 (traits) fallbacks
		"favorite_file_month":  {"%s — your favorite lately.", "Ah, %s, you live here.", "%s again — the usual haunt."},
		"late_by_standards":    {"Later than you usually go…", "Past your usual hours.", "This is late for you — long day?"},
		"rare_day":             {"Don't usually see you today…", "A rare day for you to work.", "Unusual day for a session."},
		"vs_usual_pace_faster": {"Flying today!", "Faster than your usual — in the zone.", "Quick pace today."},
		"vs_usual_pace_slower": {"Taking it slow today…", "Slower than your usual.", "Easy pace today — careful work."},
		"tool_error":           {"Hit a snag…", "That errored — ugh.", "Ran into trouble."},
		"pace_anomaly":         {"You've sped up!", "Pace just doubled…", "Picking up speed — flow state?"},
		"heavy_burn":           {"Tokens flying…", "Burning through tokens!", "Big generation right now."},
		"usual_break":          {"You usually break around now…", "This is your usual break time.", "Normally you'd pause about now — pushing through?"},
		"long_gap":             {"Been a while — %d days…", "%d days since you were last here!", "Welcome back — %d days off."},
		"anniversary":          {"%d days since we started!", "%d days together now.", "We've been at this %d days — wild."},
		"favorite_repo":        {"%s — your home turf.", "Back in %s, your usual.", "%s again — where you live."},
		"quirk_revert_commit":  {"You revert right after committing a lot…", "Commit then undo — your pattern.", "You and the post-commit revert, huh."},
		"quirk_reads_writes":   {"You read more than you write…", "Lots of reading, less writing — your style.", "A reader more than a writer."},
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
