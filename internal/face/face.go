// Package face composes the pet's appearance from layered, render-only signals
// (mood + activity + tone, all already in now.json). Layers, in render order:
// expression → eye-char → posture → color(hue+brightness) → micro-animation →
// accessory. Animation is 1 frame/second (the Claude Code status-line ceiling).
//
// v1.3 scope: the layers that are derivable from current state. Features needing
// signals now.json doesn't carry (edit/read/revert/test-pass accessories;
// content/skeptical/satisfied faces; sustained-duration habitat; warning
// escalation; color interpolation) are intentionally NOT here — see worklog.
package face

import "ccmagotchi/internal/state"

const reset = "\x1b[0m"

// Expression: the base state key, from mood + activity + tone. Priority:
// transient distress → what you're doing → mood regions → neutral.
func Expression(n state.Now) string {
	m := n.Mood
	switch {
	case n.Tone == "error":
		return "alarmed"
	case n.EventFace != "": // dev-event reaction (skeptical/disapproving/satisfied)
		return n.EventFace
	case n.Activity == "delegating":
		return "delegating"
	case n.Activity == "tool_running":
		return "working"
	case n.Activity == "thinking":
		return "thinking"
	case m.Stress > 0.85:
		return "distressed"
	case m.Tiredness > 0.9:
		return "exhausted"
	case n.Activity == "idle" && m.Tiredness > 0.7:
		return "sleepy"
	case m.Curiosity > 0.6 && m.Tiredness < 0.5:
		return "curious" // engaged beats drained
	case n.Activity == "idle" && m.Energy < 0.3:
		return "bored"
	default:
		return "neutral"
	}
}

// hue maps state → color name (tone wins). Brightness comes from energy (sgr).
func hue(n state.Now) string {
	switch n.Tone {
	case "error":
		return "red"
	case "warning":
		return "yellow"
	case "success":
		return "blue"
	}
	switch Expression(n) {
	case "delegating":
		return "magenta"
	case "working":
		return "green"
	case "thinking":
		return "teal"
	case "curious":
		return "cyan"
	case "alarmed", "distressed":
		return "red"
	case "skeptical", "disapproving":
		return "yellow" // wary
	case "satisfied":
		return "green"
	case "cheeky":
		return "magenta" // playful
	default: // sleepy/exhausted/bored/neutral
		return "grey"
	}
}

// sgr returns the ANSI color for a hue at a brightness derived from energy
// (low → dim, mid → normal, high → bright). teal≈cyan in 16-color terminals.
func sgr(h string, energy float64) string {
	lvl := 1
	if energy < 0.3 {
		lvl = 0
	} else if energy > 0.75 {
		lvl = 2
	}
	table := map[string][3]string{
		"grey":    {"\x1b[2;37m", "\x1b[90m", "\x1b[37m"},
		"green":   {"\x1b[2;32m", "\x1b[32m", "\x1b[92m"},
		"teal":    {"\x1b[38;5;23m", "\x1b[38;5;30m", "\x1b[38;5;37m"}, // real teal, distinct from cyan
		"red":     {"\x1b[2;31m", "\x1b[31m", "\x1b[91m"},
		"yellow":  {"\x1b[2;33m", "\x1b[33m", "\x1b[93m"},
		"blue":    {"\x1b[2;34m", "\x1b[34m", "\x1b[94m"},
		"magenta": {"\x1b[2;35m", "\x1b[35m", "\x1b[95m"},
		"cyan":    {"\x1b[2;36m", "\x1b[36m", "\x1b[96m"},
	}
	if v, ok := table[h]; ok {
		return v[lvl]
	}
	return "\x1b[90m"
}

// eyeChar: engagement intensity from activity + energy.
func eyeChar(n state.Now) string {
	switch {
	case n.Activity == "tool_running" || n.Activity == "thinking":
		return "◉" // focused
	case n.Mood.Energy < 0.25:
		return "·" // drifting
	case n.Mood.Energy > 0.8:
		return "●" // locked in
	default:
		return "•" // normal
	}
}

// posture: brackets carry disposition. Breathe pulse on calm states.
func posture(n state.Now, tick int64) (string, string) {
	if tick%13 == 0 && n.Tone == "" && n.Mood.Stress < 0.5 {
		return "(", ")" // breathe
	}
	switch {
	case n.Tone == "error":
		return "/", "\\" // braced / defensive
	case n.Tone == "success":
		return "\\", "/" // arms up — celebrating
	case n.Mood.Stress > 0.6:
		return "{", "}" // tense
	case n.Mood.Curiosity > 0.6 && n.Mood.Tiredness < 0.4:
		return "<", ">" // leaning in
	default:
		return "[", "]"
	}
}

// accessory: the single attention glyph. v1.4 distinguishes edit vs other tools
// via LastTool.
func accessory(n state.Now) string {
	switch {
	case n.Tone == "error":
		return "☂"
	case n.Activity == "delegating":
		return "↯"
	case n.Activity == "tool_running":
		switch n.LastTool {
		case "Edit", "Write", "MultiEdit", "NotebookEdit":
			return "✎"
		default:
			return "⚙" // Read/Bash/Grep/etc. (read-eye glyph deferred — emoji width risk)
		}
	case n.Activity == "thinking":
		return "…"
	case n.Activity == "idle" && n.Mood.Tiredness > 0.7:
		return "zZ"
	default:
		return ""
	}
}

const longToolMs = 30000 // display threshold for warning escalation (≈ persona LongToolCallMs)

// warnColor escalates yellow→orange→red by how long a tool has been running.
func warnColor(openMs int64) string {
	switch {
	case openMs > 16*longToolMs:
		return "\x1b[91m" // red — effective failure
	case openMs > 8*longToolMs:
		return "\x1b[38;5;208m" // orange
	case openMs > 4*longToolMs:
		return "\x1b[93m" // bright yellow
	default:
		return "\x1b[33m" // soft yellow
	}
}

// habitat returns flanking decoration for sustained states (held >30s) or the
// alarm event. Empty most of the time — decoration is the exception. Suppressed
// during a remark (the text fills the line).
func habitat(n state.Now, expr string) (left, right string) {
	if n.Tone == "error" {
		return "", " !!" // alarm (event-driven, no sustain needed)
	}
	if expr == "satisfied" {
		return "✦ ", " ✦" // milestone — a test passed / shipped
	}
	if n.StateHeldMs < 30000 {
		return "", "" // sustained-only
	}
	switch expr {
	case "sleepy", "exhausted":
		return "· ", " ·" // sleeping
	case "working", "thinking", "delegating":
		return "› ", " ‹" // intense focus
	case "neutral", "bored":
		if n.Mood.Stress < 0.4 {
			return "~ ", " ~" // calm waters
		}
		return "· ", " ·" // quiet
	case "curious":
		return "· ", " ·"
	}
	return "", ""
}

var talkMouth = []string{"_", "o", "O", "o"}

// facialFrame returns the eyes+mouth core, applying special faces, the talk
// mouth, and micro-animation (yawn > look > blink) for open-eyed faces.
func facialFrame(n state.Now, expr string, tick int64, talking bool) (l, mouth, r string) {
	switch expr { // fixed special faces
	case "alarmed":
		return "O", "_", "O"
	case "distressed":
		return ">", "_", "<"
	case "exhausted":
		return "x", "_", "x"
	case "skeptical":
		return "¬", "_", "¬"
	case "disapproving":
		return "ಠ", "_", "ಠ"
	case "satisfied":
		return "˘", "_", "˘"
	case "cheeky":
		return "ᗒ", "_", "ᗕ"
	case "sleepy":
		if tick%90 < 2 { // occasional yawn
			return "O", "_", "o"
		}
		return "-", "_", "-"
	}
	if talking { // the pet talks while it talks
		return "◉", talkMouth[int(tick)%len(talkMouth)], "◉"
	}
	// distinctive base eyes + mouth per expression / tone
	mouth = "_"
	switch {
	case n.Tone == "success":
		l, r = "˘", "˘" // satisfied (closed-happy)
	case expr == "bored":
		l, r = "◔", "◔" // glazed
	case expr == "curious":
		l, r = "⊙", "⊙" // wide-eyed
	case expr == "neutral" && n.Mood.Stress < 0.3 && n.Mood.Energy > 0.4:
		e := eyeChar(n)
		l, r, mouth = e, e, "‿" // content — subtle smile
	default: // working / thinking / delegating / plain neutral
		e := eyeChar(n)
		l, r = e, e
	}
	switch { // micro-animation overlay (look > blink)
	case tick%11 == 0 && expr != "bored":
		l, r = "◔", "◔" // glance aside
	case tick%6 == 0:
		l, r = "-", "-" // blink
	}
	return l, mouth, r
}

// Pick composes the full animated, colored pet for the current state at tick
// (wall-clock seconds). remark != "" means the pet is speaking; the text is
// rendered INSIDE the color span so it matches the pet's color.
func Pick(n state.Now, tick int64, remark string) string {
	talking := remark != ""
	expr := Expression(n)
	lb, rb := posture(n, tick)
	l, mouth, r := facialFrame(n, expr, tick, talking)
	body := lb + l + mouth + r + rb
	if acc := accessory(n); acc != "" {
		body += " " + acc
	}
	if talking {
		body += " " + remark // inside the color span → same color as the pet
	} else {
		hl, hr := habitat(n, expr) // habitat suppressed during a remark
		body = hl + body + hr
	}
	col := sgr(hue(n), n.Mood.Energy)
	if n.Tone == "warning" {
		col = warnColor(n.OpenToolMs) // escalates with time
	}
	return col + body + reset
}
