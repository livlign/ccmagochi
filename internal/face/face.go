// Package face renders ccmagotchi's signature creature: a dog. Every state is
// the SAME dog — head is always round parens "( )", the snout is always "ᴥ"
// (the species signal, U+1D25). Mood lives in eyes/ears/tail/paw/bark; the
// renderer also runs a sparse idle/long-running micro-behavior overlay and
// places the decorations the daemon resolves (mood symbols, sounds).
// See docs/pet-01-dog.md and docs/pet-decorations.md.
//
// Signature rendering (decorations §2): the dog's anatomy is painted as a fixed
// white-background / dark-foreground badge — it NEVER varies by state. State is
// carried by eyes/ears/tail/decorations/barks, not by color. Decorations (tail,
// bark, mood symbol, sound) render OUTSIDE the badge in plain terminal style.
// (This replaced the earlier hue×brightness color system, which the doc now
// rules out — no mood hue, no energy brightness, no idle drift, no tints.)
//
// Transitions (pet-01-dog §14): the dog never snaps between states. A change is
// masked by one intermediate frame — a blink on a mood (eye) swap, a "˙" dot on
// ears emerging/retracting, a "·" dot on the tail appearing/tucking, a still
// frame while turning. Errors, alarms and greetings snap (no smoothing). Because
// the renderer is a fresh process each refresh, the previous frame's signature
// is persisted (frame.json) and compared on the next render via PickFrame.
package face

import (
	"unicode/utf8"

	"ccmagotchi/internal/state"
)

const reset = "\x1b[0m"
const snout = "ᴥ" // THE signature character — never substituted
const maxWidth = 14

// badgeOpen paints the dog's anatomy: bright-white background, black foreground
// (decorations §2). Fixed — it never changes with state. Closed with reset.
const badgeOpen = "\x1b[107;30m"

// Ascii swaps the less-common glyphs for fallbacks (§15) when a terminal can't
// render them. Off by default; the snout has no fallback by design.
var Ascii bool

func dogMood(n state.Now) string {
	m := n.Mood
	switch {
	case n.Tone == "error":
		return "alarmed"
	case n.EventFace == "amazed":
		return "amazed"
	case n.Tone == "success":
		if m.Energy > 0.7 {
			return "joyful"
		}
		return "satisfied"
	case n.EventFace == "cheeky":
		return "cheeky"
	case n.EventFace == "skeptical":
		return "skeptical"
	case n.EventFace == "disapproving":
		return "angry"
	case n.EventFace == "satisfied":
		return "satisfied"
	case m.Affection > 0.9:
		return "affectionate"
	case n.Activity == "delegating":
		return "focused"
	case n.Activity == "tool_running":
		switch {
		case n.StateHeldMs > 300000:
			return "fixation"
		case n.StateHeldMs > 120000:
			return "determined"
		default:
			return "focused"
		}
	case n.Activity == "thinking":
		return "thinking"
	case m.Stress > 0.85:
		return "distressed"
	case m.Tiredness > 0.9:
		return "exhausted"
	case n.Activity == "idle" && m.Tiredness > 0.7:
		if n.StateHeldMs > 120000 {
			return "dreaming"
		}
		return "sleepy"
	case m.Curiosity > 0.6 && m.Tiredness < 0.5:
		return "curious"
	case n.Activity == "idle" && m.Energy < 0.3:
		return "bored"
	case m.Stress < 0.3 && m.Energy > 0.4:
		return "content"
	default:
		return "neutral"
	}
}

func eyes(mood string) (string, string) {
	switch mood {
	case "sleepy", "dreaming":
		return "-", "-"
	case "content":
		return "^", "^"
	case "curious", "bored":
		return "◔", "◔"
	case "focused":
		return "◉", "◉"
	case "fixation":
		return "ʘ", "ʘ"
	case "determined":
		return "ò", "ó"
	case "thinking":
		return "◔", "◔" // looking up, pondering — darts to •ᴥ• between ticks (liveAnat)
	case "alarmed":
		return "O", "O"
	case "exhausted":
		return "×", "×"
	case "satisfied", "joyful", "excited":
		return "˘", "˘"
	case "skeptical", "angry":
		return "¬", "¬"
	case "distressed":
		return ">", "<"
	case "cheeky":
		return "◐", "•"
	case "affectionate":
		return "♥", "♥"
	case "amazed":
		return "★", "★"
	default:
		return "•", "•"
	}
}

// ears returns the (left, right) ear glyphs. The default is BARE — no ears (the
// face stands alone). Ears appear only as gestures: flat on threat, drooped when
// low, floppy when joyful, dreaming asleep, perked transiently on a fresh event.
// (pet-01-dog §4: a deliberate move away from the always-on ≡(…)≡ look.)
func ears(n state.Now, mood string) (string, string) {
	switch mood {
	case "distressed", "angry":
		return "v", "v" // flat back (fear / irritation)
	case "alarmed", "amazed":
		return "^", "^" // peak attention/wonder — always perked
	case "skeptical", "exhausted", "bored":
		return "︶", "︶" // drooped
	case "dreaming":
		return "⌒", "⌒" // deep sleep
	case "joyful", "affectionate":
		return "ᶺ", "ᶺ" // floppy / playful
	case "cheeky":
		return "", "" // a wink is bare by default (§6: "no ears needed")
	}
	// transient perk on a fresh event (snaps in, decays back to bare)
	if mood != "sleepy" && (n.Tone != "" || n.EventFace != "" || (n.StateHeldMs > 0 && n.StateHeldMs < 3000 && n.Activity != "idle")) {
		return "^", "^"
	}
	return "", "" // bare — the default for neutral/content/curious/focused/thinking/…
}

func tail(n state.Now, mood string, tick int64) string {
	if n.Tone == "success" {
		return "↗"
	}
	switch mood {
	case "excited", "amazed":
		return "↗"
	case "joyful":
		if tick%2 == 0 {
			return "~"
		}
		return "-"
	case "satisfied", "affectionate":
		if (tick/2)%2 == 0 {
			return "~"
		}
		return "-"
	// Tucked (hidden) ONLY when tense or in deep sleep — a tucked tail is itself
	// a signal (fear / upset / out cold). (Deviation from the doc's §7 table,
	// which also tucked focused/thinking/neutral; that left the dog looking
	// tailless for most of an active session — Linh: "I see no tail".)
	case "distressed", "alarmed", "exhausted", "angry", "skeptical", "dreaming":
		return ""
	}
	return "~" // tail at rest (relaxed) — static ~; wag alternates ~/-
}

// --- idle / long-running micro-behaviors (Layer 2 overlay) -----------------

type overlay struct {
	eL, eR, aL, aR, pre, post string
	paw                       bool
	ok                        bool
}

// behaviorsFor returns the idle behaviors eligible for a mood, and the window
// length (ticks between behavior windows). Empty list → the dog holds still.
func behaviorsFor(n state.Now, mood string) ([]string, int64) {
	// long-running: the dog witnesses the process with sparse micro-behaviors.
	if n.Activity == "tool_running" {
		switch mood {
		case "focused", "determined":
			return []string{"headtilt", "earflick", "sniff"}, 53
		}
		return nil, 0
	}
	if mood == "thinking" {
		return []string{"lookaround", "daydream", "headtilt"}, 53
	}
	if n.Activity != "idle" {
		return nil, 0
	}
	switch mood {
	case "neutral":
		return []string{"lookaround", "sniff", "earflick", "daydream", "reposition", "scratch", "shakeoff"}, 17
	case "sleepy":
		return []string{"yawn", "settle", "daydream"}, 21
	case "content":
		return []string{"reposition", "settle", "lookup", "earflick"}, 17
	case "curious":
		return []string{"sniff", "lookaround", "earflick"}, 15
	case "bored":
		return []string{"lookaround", "daydream"}, 17
	case "exhausted":
		return []string{"yawn", "settle"}, 21
	case "satisfied":
		return []string{"reposition", "settle", "lookup"}, 17
	}
	return nil, 0 // focused/determined(idle)/distressed/alarmed: don't interrupt
}

// idleOverlay picks a sparse micro-behavior deterministically from the tick.
func idleOverlay(n state.Now, mood string, tick int64) overlay {
	behaviors, win := behaviorsFor(n, mood)
	if len(behaviors) == 0 {
		return overlay{}
	}
	phase := tick % win
	if phase >= 3 { // each behavior plays for ~3 ticks per window
		return overlay{}
	}
	bL, bR := eyes(mood)
	b := behaviors[(tick/win)%int64(len(behaviors))]
	o := overlay{eL: bL, eR: bR, aL: "", aR: "", ok: true} // bare ears by default
	switch b {
	case "lookaround":
		if phase == 0 {
			o.pre = " " // glance left
		} else {
			o.post = " " // glance right
		}
	case "sniff":
		o.eL, o.eR, o.aL, o.aR = "◔", "◔", "^", "^"
	case "earflick":
		o.aL = "^"
	case "daydream":
		o.eL, o.eR = "◔", "◔"
	case "yawn":
		if phase == 0 {
			o.eL, o.eR = "O", "O"
		} else {
			o.eL, o.eR = "˘", "˘"
		}
	case "settle":
		o.eL, o.eR = "-", "-"
	case "lookup":
		o.eL, o.eR, o.aL, o.aR = "◠", "◠", "^", "^"
	case "reposition":
		o.eL, o.eR = "^", "^"
	case "scratch":
		o.paw = true // raises a paw to its ear
	case "shakeoff":
		o.aL, o.aR = "~", "~" // ears flop sideways (motion)
	case "headtilt":
		o.aL, o.aR = "^", "︶" // quizzical tilt while working/thinking
	}
	return o
}

// --- anatomy & composition --------------------------------------------------

// anatomy is the resolved dog for one frame. tailMode lets a transition frame
// override the tail: auto (compute from mood), dot ("·"), or hidden. heading
// selects the body view — "still" shows the front face, "right"/"left" show the
// side-profile walking sprite.
type anatomy struct {
	mood                   string
	eyeL, eyeR, earL, earR string
	pre, post              string
	heading                string
	paw                    bool
	tailMode               int // 0=auto, 1=dot, 2=hidden
}

const (
	tailAuto = iota
	tailDot
	tailHidden
)

// The walking dog turns to a side profile (head leading, tail trailing) — a
// deliberate exception to the front-facing signature, shown only while moving.
// Head faces the direction of travel; the tail ϟ trails behind. (Front face,
// snout and parens return the moment it stops — see composeDog.)
const (
	spriteLeft  = "⊂°≡≡≡ϟ" // walking left: head ⊂° leads left, tail ϟ trails right
	spriteRight = "ϟ≡≡≡°⊃" // walking right: head °⊃ leads right, tail ϟ trails left
)

// baseAnat resolves the dog's Layer-1 look for the current state — no idle
// overlay, no heartbeat blink, no transition. This is the "target" the
// transition system compares against and smooths toward.
func baseAnat(n state.Now) anatomy {
	mood := dogMood(n)
	eL, eR := eyes(mood)
	aL, aR := ears(n, mood)
	return anatomy{mood: mood, eyeL: eL, eyeR: eR, earL: aL, earR: aR, paw: n.Greeting, heading: headingOf(n)}
}

// liveAnat is baseAnat plus the Layer-2 overlay (sparse idle/long-running
// micro-behavior) and the baseline heartbeat blink — the normal rendered dog.
func liveAnat(n state.Now, tick int64, talking bool) anatomy {
	a := baseAnat(n)
	if talking {
		return a
	}
	if o := idleOverlay(n, a.mood, tick); o.ok {
		a.eyeL, a.eyeR, a.earL, a.earR, a.pre, a.post = o.eL, o.eR, o.aL, o.aR, o.pre, o.post
		if o.paw {
			a.paw = true
		}
		return a // the overlay owns this frame — no heartbeat on top
	}
	// Baseline blink — the dog's heartbeat. A single closed-eye tick every ~7s
	// keeps it alive between the (sparse) idle behaviors, so it never reads as a
	// frozen glyph. Skipped for moods whose eyes are already closed / carry a
	// gesture (sleepy, distressed, determined, …).
	if canBlink(a.mood) && tick%7 == 0 {
		a.eyeL, a.eyeR = "-", "-"
		return a
	}
	// Thinking darts: the eyes flick between looking-up (◔, the base) and centered
	// (•) each tick, so the dog reads as actively churning while Claude processes
	// (pet-01-dog §5). Mood stays "thinking" — this is within-mood animation, not
	// a state change, so it doesn't trigger a transition.
	if a.mood == "thinking" && tick%2 == 1 {
		a.eyeL, a.eyeR = "•", "•"
	}
	return a
}

// composeDog paints the badge-framed dog plus the plain decorations around it.
// The badge (white bg / dark fg) wraps ONLY the body; bark, mood symbol and
// sound render unframed outside it (decorations §2).
//
// Two body views: standing still (or talking/greeting) shows the front FACE
// — the signature ears(eyeᴥeye)ears — with decorations on the right (a bark
// taking the tail's slot, legacy). Walking shows the side-profile SPRITE
// (pet-01-dog §11): head leads the direction of travel, the tail ϟ is built in,
// and any bark/symbol emits in front. Visible width (badge escapes excluded) is
// capped at maxWidth — emissions drop once it's hit. A sentence is uncapped.
func composeDog(n state.Now, a anatomy, tick int64, remark string) string {
	walking := remark == "" && !a.paw && (a.heading == "right" || a.heading == "left")

	var visBody string
	if walking {
		if a.heading == "left" {
			visBody = fb(spriteLeft)
		} else {
			visBody = fb(spriteRight)
		}
	} else {
		visBody = fb(a.earL) + "(" + a.pre + fb(a.eyeL) + snout + fb(a.eyeR) + a.post + ")" + fb(a.earR)
	}
	body := badgeOpen + visBody + reset

	if remark != "" {
		return body + " " + remark + reset // the sentence is the whole right side
	}
	if a.paw {
		return body + " /" + reset // greeting faces the user; tail suppressed (§9)
	}

	w := vis(visBody)
	left, right := "", ""
	addRight := func(s string) bool {
		if s == "" || w+1+vis(s) > maxWidth {
			return false
		}
		right += " " + s
		w += 1 + vis(s)
		return true
	}
	addLeft := func(s string) bool {
		if s == "" || w+1+vis(s) > maxWidth {
			return false
		}
		left = s + " " + left
		w += 1 + vis(s)
		return true
	}

	switch {
	case walking && a.heading == "left": // emissions lead left (the sprite's tail is built in)
		placeFront(n, addLeft)
	case walking: // heading right — emissions lead right
		placeFront(n, addRight)
	default: // still front face: everything on the right; a bark takes the tail's slot
		var tl string
		switch a.tailMode {
		case tailDot:
			tl = "·"
		case tailHidden:
			tl = ""
		default:
			tl = fb(tail(n, a.mood, tick))
		}
		if n.Bark != "" {
			addRight(n.Bark)
		} else {
			addRight(tl)
		}
		if n.Decor != "" && n.Bark == "" { // bark wins over a mood symbol
			addRight(fb(n.Decor))
		}
		addRight(n.Sound)
	}

	out := left + body + right
	if left != "" || right != "" {
		out += reset // decorations are plain; guard the line end with a reset
	}
	return out
}

// placeFront emits the "front" decorations onto one side, in priority order: a
// bark wins over a mood symbol (§7), then a sound. Stops at the width cap.
func placeFront(n state.Now, add func(string) bool) {
	if n.Bark != "" {
		add(n.Bark)
	} else if n.Decor != "" {
		add(fb(n.Decor))
	}
	add(n.Sound)
}

// canBlink reports whether a mood's eyes should blink. Excludes already-closed
// eyes (sleepy/dreaming/exhausted) and gesture eyes that a blink would erase
// (distressed >ᴥ<, determined òᴥó, amazed ★, cheeky wink, affectionate ♥).
func canBlink(mood string) bool {
	switch mood {
	case "sleepy", "dreaming", "exhausted", "distressed", "determined", "amazed", "cheeky", "affectionate":
		return false
	}
	return true
}

// --- transitions (pet-01-dog §14) ------------------------------------------

// Frame is the transition-relevant signature of a rendered dog. The renderer
// persists it (frame.json) and hands the previous one back to PickFrame so a
// state change can be masked by one intermediate frame. Pending marks that a
// transition frame was just shown (so the next render lands on the target).
type Frame struct {
	Mood    string `json:"mood"`
	Ears    bool   `json:"ears"`
	Tail    bool   `json:"tail"`
	Heading string `json:"heading"`
	Pending bool   `json:"pending"`
}

func headingOf(n state.Now) string {
	if n.Heading == "" {
		return "still"
	}
	return n.Heading
}

// sigOf is the target signature for the current state (ignores overlay/blink,
// which don't change the underlying mood/ears/tail/heading).
func sigOf(n state.Now, a anatomy) Frame {
	return Frame{
		Mood:    a.mood,
		Ears:    a.earL != "" || a.earR != "",
		Tail:    tail(n, a.mood, 0) != "",
		Heading: headingOf(n),
	}
}

func sameSig(a, b Frame) bool {
	return a.Mood == b.Mood && a.Ears == b.Ears && a.Tail == b.Tail && a.Heading == b.Heading
}

// snaps reports state changes that must NOT be smoothed — errors, alarms and
// greetings react instantly (§14: "Errors deserve instant attention").
func snaps(n state.Now, a anatomy) bool {
	return n.Tone == "error" || a.mood == "alarmed" || n.Greeting
}

// transitionAnat returns the masked intermediate anatomy for a smoothable
// change prev→target, and true if one applies. Priority per §14: a combined
// blink+ear frame for an event (eyes and ears both change), else a blink, an
// ear dot, a tail dot, or a still frame (turning).
func transitionAnat(prev Frame, n state.Now, target anatomy) (anatomy, bool) {
	cur := sigOf(n, target)
	moodCh := prev.Mood != cur.Mood && canBlink(cur.Mood)
	earCh := prev.Ears != cur.Ears
	tailCh := prev.Tail != cur.Tail
	headCh := prev.Heading != cur.Heading
	a := target
	switch {
	case headCh:
		// Turn through the front face for one frame (§14): bridges still→walk and
		// left↔right swaps so the body doesn't snap between views.
		a.heading, a.tailMode = "still", tailHidden
	case moodCh && earCh:
		a.eyeL, a.eyeR, a.earL, a.earR = "-", "-", "˙", "˙" // ˙(-ᴥ-)˙ event combo
	case moodCh:
		a.eyeL, a.eyeR = "-", "-" // blink masks the eye swap
	case earCh:
		a.earL, a.earR = "˙", "˙" // ears emerging / settling
	case tailCh:
		a.tailMode = tailDot // tail emerging / tucking
	default:
		return target, false
	}
	return a, true
}

// Pick composes the full dog + decorations for the current state at tick. This
// is the pure, transition-free renderer (used by the fallback path and tests).
func Pick(n state.Now, tick int64, remark string) string {
	return composeDog(n, liveAnat(n, tick, remark != ""), tick, remark)
}

// PickFrame is the transition-aware renderer. Given the previous frame's
// signature it either renders the live dog (when nothing smoothable changed, on
// a snap event, or right after a transition frame) or one masking intermediate
// frame. It returns the rendered line and the frame to persist for next time.
func PickFrame(n state.Now, tick int64, remark string, prev Frame) (string, Frame) {
	target := baseAnat(n)
	cur := sigOf(n, target)
	if prev.Mood == "" || prev.Pending || snaps(n, target) || sameSig(prev, cur) {
		return composeDog(n, liveAnat(n, tick, remark != ""), tick, remark), cur
	}
	if ta, ok := transitionAnat(prev, n, target); ok {
		kept := prev
		kept.Pending = true // hold the previous signature; land on target next tick
		return composeDog(n, ta, tick, remark), kept
	}
	return composeDog(n, liveAnat(n, tick, remark != ""), tick, remark), cur
}

// --- ascii fallback --------------------------------------------------------

var fallbackPairs = [][2]string{
	{"≡", "="}, {"︶", ","}, {"ᶺ", "ʌ"}, {"⌒", "~"}, {"ʘ", "◉"}, {"★", "*"}, {"♥", "<3"},
	// walking-sprite glyphs
	{"⊂", "<"}, {"⊃", ">"}, {"°", "o"}, {"ϟ", "~"},
}

func fb(s string) string {
	if !Ascii {
		return s
	}
	for _, p := range fallbackPairs {
		for {
			i := indexOf(s, p[0])
			if i < 0 {
				break
			}
			s = s[:i] + p[1] + s[i+len(p[0]):]
		}
	}
	return s
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func vis(s string) int { return utf8.RuneCountInString(s) }
