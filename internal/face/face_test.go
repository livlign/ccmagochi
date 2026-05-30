package face

import (
	"strings"
	"testing"

	"ccmagotchi/internal/state"
)

func TestExpression_Priority(t *testing.T) {
	cases := []struct {
		name string
		n    state.Now
		want string
	}{
		{"error tone → alarmed", state.Now{Tone: "error", Activity: "tool_running"}, "alarmed"},
		{"delegating", state.Now{Activity: "delegating"}, "delegating"},
		{"working", state.Now{Activity: "tool_running"}, "working"},
		{"thinking", state.Now{Activity: "thinking"}, "thinking"},
		{"BUG FIX: thinking while very tired is NOT sleepy/exhausted", state.Now{Activity: "thinking", Mood: state.Mood{Tiredness: 0.95}}, "thinking"},
		{"distressed (high stress)", state.Now{Activity: "idle", Mood: state.Mood{Stress: 0.9}}, "distressed"},
		{"exhausted (very tired, idle)", state.Now{Activity: "idle", Mood: state.Mood{Tiredness: 0.95}}, "exhausted"},
		{"sleepy (idle + tired)", state.Now{Activity: "idle", Mood: state.Mood{Tiredness: 0.75}}, "sleepy"},
		{"bored (idle + drained)", state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.1}}, "bored"},
		{"curious", state.Now{Activity: "idle", Mood: state.Mood{Curiosity: 0.8, Tiredness: 0.1, Energy: 0.6}}, "curious"},
		{"neutral", state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6}}, "neutral"},
	}
	for _, c := range cases {
		if g := Expression(c.n); g != c.want {
			t.Errorf("%s: want %q got %q", c.name, c.want, g)
		}
	}
}

func TestHue_ToneWinsThenActivity(t *testing.T) {
	checks := map[string]state.Now{
		"red":     {Tone: "error"},
		"yellow":  {Tone: "warning"},
		"blue":    {Tone: "success"},
		"green":   {Activity: "tool_running"},
		"teal":    {Activity: "thinking"},
		"magenta": {Activity: "delegating"},
		"grey":    {Activity: "idle", Mood: state.Mood{Energy: 0.6}},
	}
	for want, n := range checks {
		if g := hue(n); g != want {
			t.Errorf("hue(%+v): want %q got %q", n, want, g)
		}
	}
}

func TestSgr_BrightnessFromEnergy(t *testing.T) {
	if sgr("green", 0.1) != "\x1b[2;32m" {
		t.Error("low energy should be dim green")
	}
	if sgr("green", 0.9) != "\x1b[92m" {
		t.Error("high energy should be bright green")
	}
	if sgr("green", 0.5) != "\x1b[32m" {
		t.Error("mid energy should be normal green")
	}
}

func TestEyeChar(t *testing.T) {
	if eyeChar(state.Now{Activity: "tool_running"}) != "◉" {
		t.Error("active → focused eye ◉")
	}
	if eyeChar(state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.1}}) != "·" {
		t.Error("drained → faint ·")
	}
	if eyeChar(state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.9}}) != "●" {
		t.Error("high energy → ●")
	}
}

func TestPosture(t *testing.T) {
	if l, _ := posture(state.Now{Mood: state.Mood{Stress: 0.7}}, 1); l != "{" {
		t.Error("stress → tense {}")
	}
	if l, _ := posture(state.Now{Mood: state.Mood{Curiosity: 0.7, Tiredness: 0.1}}, 1); l != "<" {
		t.Error("curious → leaning <>")
	}
	if l, _ := posture(state.Now{Tone: "error"}, 1); l != "/" {
		t.Error("error → braced /\\")
	}
	if l, _ := posture(state.Now{Mood: state.Mood{Energy: 0.5}}, 1); l != "[" {
		t.Error("default → []")
	}
}

func TestAccessory(t *testing.T) {
	want := map[string]state.Now{
		"☂":  {Tone: "error"},
		"⚙":  {Activity: "tool_running"},
		"…":  {Activity: "thinking"},
		"↯":  {Activity: "delegating"},
		"zZ": {Activity: "idle", Mood: state.Mood{Tiredness: 0.8}},
		"":   {Activity: "idle", Mood: state.Mood{Energy: 0.6}},
	}
	for w, n := range want {
		if g := accessory(n); g != w {
			t.Errorf("accessory(%+v): want %q got %q", n, w, g)
		}
	}
}

func TestFacialFrame_SpecialAndAnimated(t *testing.T) {
	l, _, r := facialFrame(state.Now{Tone: "error"}, "alarmed", 1, false)
	if l != "O" || r != "O" {
		t.Errorf("alarmed should be O_O, got %s_%s", l, r)
	}
	// open face animates across a 12s window (blink/glance differ from steady)
	steady, _, _ := facialFrame(state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6}}, "neutral", 1, false)
	moved := false
	for tk := int64(0); tk < 12; tk++ {
		if l, _, _ := facialFrame(state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6}}, "neutral", tk, false); l != steady {
			moved = true
		}
	}
	if !moved {
		t.Error("neutral face should show ambient motion across a cycle")
	}
	// talking animates the mouth
	if _, m0, _ := facialFrame(state.Now{}, "neutral", 0, true); m0 == "" {
		t.Error("talking should produce a mouth char")
	}
}

func TestHabitat_SustainedAndAlarm(t *testing.T) {
	if _, r := habitat(state.Now{Tone: "error"}, "alarmed"); r != " !!" {
		t.Errorf("alarm habitat should be ' !!', got %q", r)
	}
	if l, r := habitat(state.Now{StateHeldMs: 1000}, "neutral"); l != "" || r != "" {
		t.Errorf("under 30s should have no habitat, got %q/%q", l, r)
	}
	if l, _ := habitat(state.Now{StateHeldMs: 40000}, "sleepy"); l != "· " {
		t.Errorf("sustained sleepy → '· ', got %q", l)
	}
	if l, _ := habitat(state.Now{StateHeldMs: 40000, Mood: state.Mood{Stress: 0.1}}, "neutral"); l != "~ " {
		t.Errorf("sustained calm → calm waters '~ ', got %q", l)
	}
	if l, _ := habitat(state.Now{StateHeldMs: 40000}, "working"); l != "› " {
		t.Errorf("sustained working → focus '› ', got %q", l)
	}
}

func TestAccessory_EditVsTool(t *testing.T) {
	if accessory(state.Now{Activity: "tool_running", LastTool: "Edit"}) != "✎" {
		t.Error("edit tool → ✎")
	}
	if accessory(state.Now{Activity: "tool_running", LastTool: "Bash"}) != "⚙" {
		t.Error("non-edit tool → ⚙")
	}
}

func TestWarnColor_Escalates(t *testing.T) {
	soft := warnColor(longToolMs * 3)
	bright := warnColor(longToolMs * 5)
	orange := warnColor(longToolMs * 9)
	red := warnColor(longToolMs * 20)
	if soft == bright || bright == orange || orange == red {
		t.Error("warning color should escalate distinctly with time")
	}
	if red != "\x1b[91m" {
		t.Errorf("16× threshold → red, got %q", red)
	}
}

func TestFacialFrame_DistinctiveEyes(t *testing.T) {
	// tick=1 avoids the blink (%6) and glance (%11) overlay frames
	if l, _, _ := facialFrame(state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.1}}, "bored", 1, false); l != "◔" {
		t.Errorf("bored → glazed ◔, got %q", l)
	}
	if l, _, _ := facialFrame(state.Now{Activity: "idle", Mood: state.Mood{Curiosity: 0.8, Energy: 0.6}}, "curious", 1, false); l != "⊙" {
		t.Errorf("curious → wide-eyed ⊙, got %q", l)
	}
	if l, _, _ := facialFrame(state.Now{Tone: "success"}, "neutral", 1, false); l != "˘" {
		t.Errorf("success → satisfied ˘, got %q", l)
	}
	if _, m, _ := facialFrame(state.Now{Activity: "idle", Mood: state.Mood{Stress: 0.1, Energy: 0.6}}, "neutral", 1, false); m != "‿" {
		t.Errorf("calm neutral → content smile ‿, got %q", m)
	}
}

func TestEventFace_DevReactions(t *testing.T) {
	// EventFace overrides activity/mood (a salient dev reaction)
	if Expression(state.Now{EventFace: "skeptical", Activity: "tool_running"}) != "skeptical" {
		t.Error("EventFace should win over activity")
	}
	if l, _, _ := facialFrame(state.Now{}, "skeptical", 1, false); l != "¬" {
		t.Error("skeptical → ¬_¬")
	}
	if l, _, _ := facialFrame(state.Now{}, "disapproving", 1, false); l != "ಠ" {
		t.Error("disapproving → ಠ_ಠ")
	}
	if l, _, _ := facialFrame(state.Now{}, "satisfied", 1, false); l != "˘" {
		t.Error("satisfied → ˘_˘")
	}
	// satisfied gets the milestone habitat
	if _, r := habitat(state.Now{EventFace: "satisfied"}, "satisfied"); r != " ✦" {
		t.Errorf("satisfied → milestone ✦, got %q", r)
	}
}

func TestPosture_Celebrating(t *testing.T) {
	if l, r := posture(state.Now{Tone: "success"}, 1); l != "\\" || r != "/" {
		t.Errorf("success → celebrating \\ /, got %q %q", l, r)
	}
}

func TestPick_WrapsColorReset(t *testing.T) {
	out := Pick(state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6}}, 1, "")
	if !strings.HasSuffix(out, reset) || !strings.Contains(out, "•") {
		t.Errorf("expected colored face ending in reset, got %q", out)
	}
	// a remark is rendered INSIDE the color span (before reset) → same color
	withR := Pick(state.Now{Activity: "idle", Mood: state.Mood{Energy: 0.6}}, 1, "hello there")
	if !strings.HasSuffix(withR, "hello there"+reset) {
		t.Errorf("remark should sit inside the color span, got %q", withR)
	}
}
