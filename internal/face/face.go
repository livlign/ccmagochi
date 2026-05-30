// Package face maps mood + activity to a colored, animated kaomoji.
// Two layers: Expression() is the stable base state (priority logic lives here);
// frameGlyph() adds per-second ambient motion (blink, glance, spinner, talk).
// Animation is 1 frame/second — that's the Claude Code status-line ceiling.
package face

import "ccmagotchi/internal/state"

const reset = "\x1b[0m"

const (
	grey      = "\x1b[90m"
	dim       = "\x1b[2;37m"
	brightCyn = "\x1b[96m"
	cyan      = "\x1b[36m"
	amber     = "\x1b[33m"
	warm      = "\x1b[38;5;215m"
)

// Expression returns the stable base state key. Priority is deliberate:
// genuine distress wins; then what you're DOING; sleepy only when actually idle
// (fixes the bug where high tiredness pinned the face to [-_-] while working).
func Expression(n state.Now) string {
	m := n.Mood
	switch {
	case m.Stress > 0.85:
		return "overwhelmed"
	case n.Activity == "delegating":
		return "delegating"
	case n.Activity == "tool_running":
		return "working"
	case n.Activity == "thinking":
		return "thinking"
	case n.Activity == "idle" && m.Tiredness > 0.7:
		return "sleepy"
	case m.Curiosity > 0.6 && m.Tiredness < 0.5:
		return "curious"
	default:
		return "neutral"
	}
}

func colorFor(n state.Now, expr string) string {
	switch expr {
	case "overwhelmed":
		return amber
	case "sleepy":
		return dim
	case "curious":
		return brightCyn
	case "delegating":
		return cyan
	}
	// neutral/working/thinking: tint by mood so the same face reads differently
	switch {
	case n.Mood.Tiredness > 0.6:
		return dim
	case n.Mood.Stress > 0.5:
		return warm
	default:
		return grey
	}
}

// animation frame sets (cycled by wall-clock second)
var (
	spinner   = []string{"◐", "◓", "◑", "◒"}      // working
	talkMouth = []string{"_", "o", "O", "o"}      // speaking
	delegArr  = []string{"→", "➤", "→", "➤"}      // delegating
	thinkDots = []string{"   ", ".  ", ".. ", "..."}
	sleepZ    = []string{" ", "z", "zz", "z"}
)

func frameGlyph(expr string, frame int64, talking bool) string {
	f := int(frame)
	if talking { // the pet talks while it talks — mouth moves on any face
		return "[◉" + talkMouth[f%len(talkMouth)] + "◉]"
	}
	switch expr {
	case "overwhelmed":
		return "[x_x]"
	case "delegating":
		return "[◉_◉]" + delegArr[f%len(delegArr)]
	case "working":
		return "[◉_◉]" + spinner[f%len(spinner)]
	case "thinking":
		return "[•_•]" + thinkDots[f%len(thinkDots)]
	case "sleepy":
		return "[-_-]" + sleepZ[f%len(sleepZ)]
	case "curious":
		return []string{"[O_O]", "[O_O]", "[o_o]", "[O_O]"}[f%4]
	default: // neutral: mostly steady, an occasional glance and a 1-frame blink = ambient life
		switch f % 10 {
		case 3:
			return "[◔_◔]" // glance aside
		case 7:
			return "[-_-]" // blink
		default:
			return "[◉_◉]"
		}
	}
}

// Pick renders the animated, colored face for the current state at frame
// (wall-clock seconds). talking = a remark is currently shown.
func Pick(n state.Now, frame int64, talking bool) string {
	expr := Expression(n)
	return colorFor(n, expr) + frameGlyph(expr, frame, talking) + reset
}
