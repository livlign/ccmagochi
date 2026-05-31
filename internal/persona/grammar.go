package persona

import (
	"encoding/json"
	"os"
)

// Grammar is the pet's procedural improvisation (no LLM). Each category maps to
// an ordered list of "slots"; each slot is a pool of interchangeable fragments.
// Composing a remark draws one fragment per slot and concatenates them, so a
// category with slots of size a,b yields a*b distinct lines from a+b fragments.
// Fragments carry their own leading spacing/punctuation, so slot 2's " — still
// at it?" attaches cleanly after slot 1's "It's late". Variety comes from
// combination, not generation — which is why the pet stops sounding canned
// without ever phoning an LLM.
//
// %d / %s verbs in a fragment are filled by the daemon per category (file count,
// quirk ext, streak length, …). Any category here overrides the flat Vocab for
// that key; categories absent here fall back to Vocab.
type Grammar map[string][][]string

// DefaultGrammar covers the high-frequency categories with real combinatorial
// range. Tunable via grammar.json (LoadGrammar).
func DefaultGrammar() Grammar {
	return Grammar{
		"late_hour": {
			{"It's late", "The small hours", "Past midnight by the clock", "Burning the midnight oil", "Late one tonight", "Clock says it's late"},
			{"", " — still at it?", " — night owl mode", "…", " — wrap soon?", " huh"},
		},
		// #3 fix: day-aware weekend. Friday = "coming"; Sat/Sun = "here" (never
		// say "it's Friday" on a Sunday).
		"weekend_eve": {
			{"Friday", "Almost the weekend", "Weekend's nearly here", "Friday already"},
			{" — winding down?", " vibes", " — any plans?", "…", "!"},
		},
		"weekend_now": {
			{"Weekend coding", "It's the weekend", "Working through the weekend", "Weekend session"},
			{", huh?", " — dedication", "…", " — don't forget to rest", "!"},
		},
		"editing": {
			{"On it", "Heads down", "Editing away", "In the thick of it"},
			{" — editing %d files…", ", %d files in flight…", " — %d files so far…", " on %d files…"},
		},
		// #1: reading remark.
		"reading": {
			{"Reading", "Skimming", "Having a look", "Catching up"},
			{" through %d files…", " %d files…", " — %d so far…", " around, %d files…"},
		},
		"long_thinking": {
			{"Tough one", "Hmm", "A hard one", "Mulling this over", "Deep in thought"},
			{" — thinking…", ", give me a sec…", "…", " — chewing on it…"},
		},
		"long_tool_call": {
			{"Still going", "This one's deep", "Taking a while", "Patience", "Long call"},
			{"…", " on that one…", " — hang tight…", ", almost…"},
		},
		"same_file_repeat": {
			{"Back in this file", "You and this file", "Round and round here", "This one again"},
			{"…", " huh?", " — it keeps pulling you in", "!"},
		},
		"busy": {
			{"Lots of moving parts", "Plenty in flight", "Busy stretch", "Lots going on"},
			{" this session", " right now", "…", "!"},
		},
		"many_files": {
			{"That's a fair few files", "Lots of files", "Quite a few files"},
			{" this session", " open", "…"},
		},
		"commit": {
			{"Shipped", "Committed", "Locked it in", "Another one in the books", "Saved"},
			{"!", " — clean", " — nice one", ". git's happy."},
		},
		"revert": {
			{"Taking that back", "Undo", "Reverting", "Scrapping that", "Rewind"},
			{" — it happens", "…", "!", " — no harm"},
		},
		"test_pass": {
			{"All green", "Tests pass", "Clean run", "Passing"},
			{"!", " — love it", ", woo!", " across the board!"},
		},
		"test_fail": {
			{"Something broke", "Tests are unhappy", "Red", "Something's off"},
			{"…", " — back to it!", ".", " — the suite's complaining."},
		},
		"delegating": {
			{"Sent a helper off", "Delegating", "A subagent's on it", "Outsourced that", "Help's on the way"},
			{"!", " — smart", "…", "."},
		},
		"favorite_file": { // %s = favorite extension (quirk)
			{"Ooh, a %s file", "A %s, nice", "%s — my favorite", "A %s? yes please"},
			{"!", " — love these", ".", ""},
		},
		"aversion": { // %s = aversion tool (quirk)
			{"Ugh, %s again", "Not %s…", "%s? if we must", "Oh, %s. fine"},
			{"", "…", "."},
		},
		// --- Layer 2 (recent / cross-session) ---
		"first_session_today": {
			{"First session today", "Day's first run", "Morning's first dig", "Starting fresh today"},
			{"…", " — let's go", ".", " — here we go"},
		},
		"busy_today": { // %d = files touched today (real daily count, not session)
			{"Busy day", "Big day today", "Lots done today"},
			{" — %d files already", ", %d files in", " (%d files)"},
		},
		"streak": { // %d = consecutive active days
			{"%d days running", "Day %d in a row", "%d-day streak"},
			{"!", " — on a roll", "…", " — consistent"},
		},
		"file_revisit": { // %s = file, %d = hours since last seen
			{"First time back in %s in %dh", "%s again — been %dh", "Returning to %s after %dh"},
			{"…", "", "."},
		},
		"same_as_yesterday": { // %s = file
			{"%s — same as yesterday", "Back in %s from yesterday", "%s again, like yesterday"},
			{"…", "", "."},
		},
		// --- Layer 3 (traits / who you are) ---
		"favorite_file_month": { // %s = lifetime favorite file
			{"%s — your favorite lately", "Ah, %s, you live here", "%s again — the usual haunt"},
			{"…", "", "."},
		},
		"late_by_standards": {
			{"Later than you usually go", "Past your usual hours", "This is late for you", "Beyond your normal window"},
			{"…", " — everything ok?", ".", " — long day?"},
		},
		"rare_day": {
			{"Don't usually see you today", "A rare day for you to work", "Unusual day for a session"},
			{"…", ".", " — something up?"},
		},
		"vs_usual_pace_faster": {
			{"Flying today", "Faster than your usual", "Quick pace today", "Moving fast"},
			{"!", "…", " — in the zone", "."},
		},
		"vs_usual_pace_slower": {
			{"Taking it slow today", "Slower than your usual", "Easy pace today", "Measured today"},
			{"…", ".", " — careful work", ""},
		},
		// tool error (spoken reaction, distinct from the alarmed face/tone)
		"tool_error": {
			{"Hit a snag", "That errored", "Ran into trouble", "Something errored"},
			{"…", " — ugh", ".", " — let's see"},
		},
		// within-session pace anomaly ("doubled your pace from earlier")
		"pace_anomaly": {
			{"You've sped up", "Pace just doubled", "Picking up speed", "Faster all of a sudden"},
			{"!", "…", " — flow state?", "."},
		},
		// token burn rate (Layer 1)
		"heavy_burn": {
			{"Tokens flying", "Burning through tokens", "Lot of output right now", "Big generation"},
			{"…", "!", " — busy model", "."},
		},
		// Layer 3 — habit / anniversary / repo / detected quirks
		"usual_break": {
			{"You usually break around now", "This is your usual break time", "Normally you'd pause about now"},
			{"…", " — pushing through?", ".", " — still going"},
		},
		"long_gap": { // %d = days since last active
			{"Been a while — %d days", "%d days since you were last here", "Welcome back — %d days off"},
			{"…", "!", ".", " — missed this"},
		},
		"anniversary": { // %d = days since first session
			{"%d days since we started", "%d days together now", "We've been at this %d days"},
			{"!", " — wild", "…", "."},
		},
		"favorite_repo": { // %s = repo name
			{"%s — your home turf", "Back in %s, your usual", "%s again — where you live"},
			{"…", "", ".", " — comfy"},
		},
		"quirk_revert_commit": {
			{"You revert right after committing a lot", "Commit then undo — your pattern", "You and the post-commit revert"},
			{"…", " huh", ".", " — second thoughts?"},
		},
		"quirk_reads_writes": {
			{"You read more than you write", "Lots of reading, less writing — your style", "A reader more than a writer"},
			{"…", ".", " — measure twice", ""},
		},
	}
}

// LoadGrammar merges grammar.json over the defaults (present keys override).
func LoadGrammar(path string) Grammar {
	g := DefaultGrammar()
	if b, err := os.ReadFile(path); err == nil {
		var o Grammar
		if json.Unmarshal(b, &o) == nil {
			for k, v := range o {
				g[k] = v
			}
		}
	}
	return g
}
