// Package face maps the current mood + activity to a colored kaomoji.
// v1 = ~5 distinguishable states. Faces are intended to grow in later reps;
// keeping selection in one small table makes adding states a local change.
package face

import "ccmagotchi/internal/state"

const reset = "\x1b[0m"

// ANSI colors (kept minimal; Phase 5 handles Windows VT enabling).
const (
	grey      = "\x1b[90m"
	dim       = "\x1b[2;37m"
	brightCyn = "\x1b[96m"
	cyan      = "\x1b[36m"
	amber     = "\x1b[33m"
)

// Pick returns the colored face string for the current state.
func Pick(n state.Now) string {
	glyph, color := selectFace(n)
	return color + glyph + reset
}

// selectFace is the v1 mood/activity → (glyph, color) table. Order = priority.
func selectFace(n state.Now) (glyph, color string) {
	m := n.Mood
	switch {
	case m.Stress > 0.7:
		return "[x_x]", amber // overwhelmed / recent error
	case m.Tiredness > 0.7:
		return "[-_-]", dim // sleepy
	case n.Activity == "delegating":
		return "[◉_◉]→", cyan // a subagent is running
	case n.Activity == "tool_running":
		return "[◉_◉]9", grey // working
	case m.Curiosity > 0.6 && m.Tiredness < 0.4:
		return "[O_O]", brightCyn // alert / exploring
	default:
		return "[◉_◉]", grey // neutral / watching
	}
}
