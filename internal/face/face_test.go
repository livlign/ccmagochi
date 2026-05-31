package face

import (
	"strings"
	"testing"

	"ccmagotchi/internal/state"
)

// The signature: the snout ᴥ and round parens ( ) are present in EVERY state,
// and square brackets / the kaomoji "_" mouth never appear.
func TestPick_SignatureAlwaysDog(t *testing.T) {
	states := []state.Now{
		{Activity: "idle", Mood: state.Mood{Energy: 0.6}},
		{Activity: "tool_running"},
		{Activity: "thinking"},
		{Tone: "error"},
		{Tone: "success", Mood: state.Mood{Energy: 0.9}},
		{Mood: state.Mood{Stress: 0.95}, Activity: "idle"},
		{Mood: state.Mood{Tiredness: 0.95}, Activity: "idle"},
		{EventFace: "skeptical"},
		{EventFace: "cheeky"},
	}
	for _, n := range states {
		for tk := int64(0); tk < 6; tk++ { // cover idle-overlay frames too
			out := Pick(n, tk, "")
			if !strings.Contains(out, snout) {
				t.Errorf("snout missing for %+v: %q", n, out)
			}
			if !strings.Contains(out, "(") || !strings.Contains(out, ")") {
				t.Errorf("parens missing for %+v: %q", n, out)
			}
			// NB: ANSI color codes contain '[', so we check ']' and '_', which
			// the old kaomoji used but neither the dog nor ANSI escapes do.
			if strings.Contains(out, "]") || strings.Contains(out, "_") {
				t.Errorf("dog must not use square brackets/underscore: %q", out)
			}
		}
	}
}

func TestDogMood_Priority(t *testing.T) {
	cases := []struct {
		name string
		n    state.Now
		want string
	}{
		{"error → alarmed", state.Now{Tone: "error", Activity: "tool_running"}, "alarmed"},
		{"working → focused", state.Now{Activity: "tool_running"}, "focused"},
		{"long working → determined", state.Now{Activity: "tool_running", StateHeldMs: 150000}, "determined"},
		{"very long → fixation", state.Now{Activity: "tool_running", StateHeldMs: 400000}, "fixation"},
		{"thinking", state.Now{Activity: "thinking"}, "thinking"},
		{"distressed", state.Now{Activity: "idle", Mood: state.Mood{Stress: 0.9}}, "distressed"},
		{"exhausted", state.Now{Activity: "idle", Mood: state.Mood{Tiredness: 0.95}}, "exhausted"},
		{"sleepy", state.Now{Activity: "idle", Mood: state.Mood{Tiredness: 0.75}}, "sleepy"},
		{"deep idle → dreaming", state.Now{Activity: "idle", Mood: state.Mood{Tiredness: 0.75}, StateHeldMs: 130000}, "dreaming"},
		{"curious", state.Now{Activity: "idle", Mood: state.Mood{Curiosity: 0.8, Tiredness: 0.1, Energy: 0.6}}, "curious"},
		{"neutral", state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6, Stress: 0.5}}, "neutral"},
	}
	for _, c := range cases {
		if g := dogMood(c.n); g != c.want {
			t.Errorf("%s: want %q got %q", c.name, c.want, g)
		}
	}
}

func TestEyes_MoodMapping(t *testing.T) {
	want := map[string][2]string{
		"neutral":    {"•", "•"},
		"alarmed":    {"O", "O"},
		"focused":    {"◉", "◉"},
		"distressed": {">", "<"}, // asymmetric distress gesture
		"determined": {"ò", "ó"}, // asymmetric determination
		"satisfied":  {"˘", "˘"},
		"skeptical":  {"¬", "¬"},
		"exhausted":  {"×", "×"},
		"cheeky":     {"◐", "•"}, // wink
	}
	for mood, w := range want {
		l, r := eyes(mood)
		if l != w[0] || r != w[1] {
			t.Errorf("%s eyes: want %s%s got %s%s", mood, w[0], w[1], l, r)
		}
	}
}

// Ears are BARE by default and appear only as gestures (pet-01-dog §4).
func TestEars_States(t *testing.T) {
	if l, r := ears(state.Now{}, "distressed"); l != "v" || r != "v" {
		t.Errorf("distress → flat vv, got %s%s", l, r)
	}
	if l, r := ears(state.Now{}, "skeptical"); l != "︶" || r != "︶" {
		t.Errorf("skeptical → drooped, got %s%s", l, r)
	}
	if l, r := ears(state.Now{}, "dreaming"); l != "⌒" || r != "⌒" {
		t.Errorf("dreaming → ⌒⌒, got %s%s", l, r)
	}
	if l, r := ears(state.Now{Mood: state.Mood{Energy: 0.5}}, "neutral"); l != "" || r != "" {
		t.Errorf("relaxed neutral → BARE (no ears), got %q%q", l, r)
	}
	if l, _ := ears(state.Now{Tone: "error"}, "alarmed"); l != "^" {
		t.Error("alarmed → perked ^^")
	}
	// a fresh event transiently perks an otherwise-bare face
	if l, _ := ears(state.Now{Activity: "tool_running", StateHeldMs: 1000}, "focused"); l != "^" {
		t.Error("a just-started tool perks the ears")
	}
}

// The tail rests (~) in calm/working states, wags in positive ones, and tucks
// (hidden) only when tense or in deep sleep.
func TestTail_VisibilityAndWag(t *testing.T) {
	if tail(state.Now{}, "focused", 0) != "~" {
		t.Error("focused dog rests its tail ~ (only tense states tuck)")
	}
	if tail(state.Now{}, "neutral", 0) != "~" {
		t.Error("neutral dog shows a resting tail ~")
	}
	if tail(state.Now{}, "distressed", 0) != "" {
		t.Error("distressed dog tucks its tail (hidden)")
	}
	if tail(state.Now{}, "alarmed", 0) != "" {
		t.Error("alarmed dog tucks its tail")
	}
	if tail(state.Now{}, "sleepy", 0) != "~" {
		t.Error("sleepy → tail at rest ~")
	}
	a := tail(state.Now{}, "joyful", 0)
	b := tail(state.Now{}, "joyful", 1)
	if a == b || (a != "~" && a != "-") {
		t.Errorf("joyful tail should wag (animate ~/-), got %q then %q", a, b)
	}
	if tail(state.Now{Tone: "success"}, "satisfied", 0) != "↗" {
		t.Error("a fresh success raises the tail ↗")
	}
}

// Walking turns the dog to a side profile (head leads travel, tail trails);
// standing still restores the front signature face (pet-01-dog §11).
func TestPick_WalkingSprite(t *testing.T) {
	base := state.Mood{Energy: 0.6, Stress: 0.5}
	if r := Pick(state.Now{Activity: "idle", Heading: "right", Mood: base}, 2, ""); !strings.Contains(r, spriteRight) {
		t.Errorf("walking right → right-facing sprite, got %q", r)
	}
	if l := Pick(state.Now{Activity: "idle", Heading: "left", Mood: base}, 2, ""); !strings.Contains(l, spriteLeft) {
		t.Errorf("walking left → left-facing sprite, got %q", l)
	}
	s := Pick(state.Now{Activity: "idle", Heading: "still", Mood: base}, 2, "")
	if !strings.Contains(s, snout) || strings.Contains(s, spriteLeft) {
		t.Errorf("standing still → front face with the snout, got %q", s)
	}
}

// The paw greeting suppresses the tail and faces the user.
func TestPick_GreetingPaw(t *testing.T) {
	out := Pick(state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6}, Greeting: true}, 1, "")
	if !strings.Contains(out, "/") {
		t.Errorf("greeting should show the paw /, got %q", out)
	}
}

// A bark renders beside the dog when set (and not while speaking a sentence).
func TestPick_Bark(t *testing.T) {
	out := Pick(state.Now{Tone: "error", Bark: "bork!"}, 1, "")
	if !strings.Contains(out, "bork!") {
		t.Errorf("bark should render beside the dog, got %q", out)
	}
	talk := Pick(state.Now{Tone: "error", Bark: "bork!"}, 1, "something")
	if strings.Contains(talk, "bork!") {
		t.Errorf("a spoken sentence suppresses the bark, got %q", talk)
	}
}

func TestPick_SpeaksBeside(t *testing.T) {
	out := Pick(state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6}}, 1, "hello there")
	if !strings.HasSuffix(out, "hello there"+reset) {
		t.Errorf("sentence should sit inside the color span, got %q", out)
	}
}

// The dog's anatomy is painted as the fixed white-bg / dark-fg signature badge
// (decorations §2), never a state-varying color.
func TestPick_SignatureBadge(t *testing.T) {
	for _, n := range []state.Now{
		{Activity: "idle", Mood: state.Mood{Energy: 0.6}},
		{Tone: "error"},
		{Activity: "tool_running"},
	} {
		out := Pick(n, 1, "")
		if !strings.Contains(out, badgeOpen) {
			t.Errorf("anatomy should be wrapped in the white-bg badge, got %q", out)
		}
		// the old state-color codes (cyan/green/red/gold/…) must be gone
		for _, c := range []string{"[36m", "[32m", "[2;31m", "[48;5;", "[38;5;"} {
			if strings.Contains(out, c) {
				t.Errorf("state color %q should be gone (badge only), got %q", c, out)
			}
		}
	}
}

// Baseline life: a calm idle dog blinks periodically (closed-eye frames appear),
// so it never reads as a frozen glyph between behaviors.
func TestPick_Blinks(t *testing.T) {
	n := state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6, Stress: 0.5}} // neutral
	open, closed := 0, 0
	for tk := int64(0); tk < 14; tk++ {
		if strings.Contains(Pick(n, tk, ""), "(-ᴥ-)") {
			closed++
		} else {
			open++
		}
	}
	if closed == 0 {
		t.Error("the dog should blink (a closed-eye frame) within a 14s span")
	}
	if open == 0 {
		t.Error("the dog should be open-eyed most of the time")
	}
}

func TestAmazed_Starry(t *testing.T) {
	if dogMood(state.Now{EventFace: "amazed"}) != "amazed" {
		t.Error("amazed eventFace → amazed mood")
	}
	if l, r := eyes("amazed"); l != "★" || r != "★" {
		t.Errorf("amazed → starry eyes, got %s%s", l, r)
	}
}

// Idle overlay fires a sparse micro-behavior (a glance) in calm idle.
func TestIdleOverlay_Fires(t *testing.T) {
	o := idleOverlay(state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6, Stress: 0.5}}, "neutral", 0)
	if !o.ok {
		t.Fatal("idle neutral should fire a behavior at the start of a window")
	}
	// focused work is never interrupted
	if idleOverlay(state.Now{Activity: "tool_running"}, "focused", 0).ok && idleOverlay(state.Now{Activity: "tool_running"}, "focused", 60).ok {
		// long-running has its OWN sparse window — fine if it occasionally fires,
		// but it must be silent most ticks
	}
	busy := 0
	for tk := int64(0); tk < 53; tk++ { // one long-running window
		if idleOverlay(state.Now{Activity: "tool_running"}, "focused", tk).ok {
			busy++
		}
	}
	if busy > 3 {
		t.Errorf("long-running micro-behaviors must stay sparse, fired %d in a 53-tick window", busy)
	}
}

// A mood symbol renders beside the dog; a bark suppresses it.
func TestPick_DecorAndBark(t *testing.T) {
	idle := state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6}}
	idle.Decor = "zZ"
	if !strings.Contains(Pick(idle, 1, ""), "zZ") {
		t.Error("a set mood symbol should render")
	}
	barked := idle
	barked.Bark = "woof!"
	out := Pick(barked, 1, "")
	if !strings.Contains(out, "woof!") || strings.Contains(out, "zZ") {
		t.Errorf("bark should win over the mood symbol, got %q", out)
	}
}

// The 14-char cap drops the sound emission before the symbol.
func TestPick_WidthCapDropsSound(t *testing.T) {
	n := state.Now{Activity: "idle", Mood: state.Mood{Tiredness: 0.8}} // sleepy → tail ~
	n.Decor = "zZ"
	n.Sound = "*snore*"
	out := Pick(n, 1, "")
	if !strings.Contains(out, "zZ") {
		t.Error("symbol should survive the cap")
	}
	if strings.Contains(out, "*snore*") {
		t.Error("sound should be dropped first when over the width cap")
	}
}

// A sentence-remark suppresses bark/symbol/sound (the sentence is the focus).
func TestPick_RemarkSuppressesDecor(t *testing.T) {
	n := state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6}, Decor: "zZ", Bark: "woof!"}
	out := Pick(n, 1, "hello")
	if strings.Contains(out, "zZ") || strings.Contains(out, "woof!") {
		t.Errorf("a spoken sentence suppresses decorations, got %q", out)
	}
}

// A mood (eye) change is masked by one blink frame, then lands on the target
// (pet-01-dog §14). Error/alarm/greeting snap instead.
func TestPickFrame_BlinkOnMoodChange(t *testing.T) {
	prev := Frame{Mood: "neutral", Heading: "still"}
	n := state.Now{Activity: "thinking"} // → mood "thinking" (eyes ◔), a smoothable swap
	frame1, sig1 := PickFrame(n, 4, "", prev)
	if !strings.Contains(frame1, "(-ᴥ-)") {
		t.Errorf("a mood change should blink first, got %q", frame1)
	}
	if !sig1.Pending {
		t.Error("the blink frame should mark Pending so the next render lands on target")
	}
	frame2, sig2 := PickFrame(n, 4, "", sig1) // even tick → looking-up frame of the dart
	if !strings.Contains(frame2, "(◔ᴥ◔)") {
		t.Errorf("the tick after the blink should show the target (thinking) mood, got %q", frame2)
	}
	if sig2.Pending || sig2.Mood != "thinking" {
		t.Errorf("after landing, frame should be the settled target, got %+v", sig2)
	}
	// an error snaps — no blink intermediate
	snapFrame, _ := PickFrame(state.Now{Tone: "error"}, 3, "", Frame{Mood: "neutral", Heading: "still"})
	if !strings.Contains(snapFrame, "(OᴥO)") {
		t.Errorf("an error should snap straight to alarmed, got %q", snapFrame)
	}
}

// ASCII fallback swaps the less-common ear glyphs (e.g. drooped ︶ → ,) but
// never the snout.
func TestAsciiFallback(t *testing.T) {
	Ascii = true
	defer func() { Ascii = false }()
	out := Pick(state.Now{EventFace: "skeptical"}, 3, "") // drooped ︶ ears
	if strings.Contains(out, "︶") {
		t.Errorf("ascii mode should drop the ︶ ear glyph, got %q", out)
	}
	if !strings.Contains(out, snout) {
		t.Errorf("the snout is never substituted, got %q", out)
	}
}
