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
		{"overwhelmed wins over activity", state.Now{Mood: state.Mood{Stress: 0.9}, Activity: "tool_running"}, "overwhelmed"},
		{"delegating", state.Now{Activity: "delegating", Mood: state.Mood{Tiredness: 0.9}}, "delegating"},
		{"BUG FIX: tired while thinking is NOT sleepy", state.Now{Activity: "thinking", Mood: state.Mood{Tiredness: 0.9}}, "thinking"},
		{"BUG FIX: tired while tool_running still works", state.Now{Activity: "tool_running", Mood: state.Mood{Tiredness: 0.9}}, "working"},
		{"sleepy only when idle + tired", state.Now{Activity: "idle", Mood: state.Mood{Tiredness: 0.9}}, "sleepy"},
		{"curious", state.Now{Activity: "idle", Mood: state.Mood{Curiosity: 0.8, Tiredness: 0.1}}, "curious"},
		{"neutral default", state.Now{Activity: "idle"}, "neutral"},
	}
	for _, c := range cases {
		if g := Expression(c.n); g != c.want {
			t.Errorf("%s: want %q got %q", c.name, c.want, g)
		}
	}
}

func TestFrameGlyph_Animates(t *testing.T) {
	if frameGlyph("working", 0, false) == frameGlyph("working", 1, false) {
		t.Error("working spinner should differ across frames")
	}
	// neutral has ambient motion somewhere in its 10s cycle
	steady := frameGlyph("neutral", 0, false)
	moved := false
	for f := int64(0); f < 10; f++ {
		if frameGlyph("neutral", f, false) != steady {
			moved = true
		}
	}
	if !moved {
		t.Error("neutral should show ambient motion (glance/blink) across a cycle")
	}
	// talking animates the mouth regardless of expression
	if frameGlyph("working", 0, true) == frameGlyph("working", 1, true) {
		t.Error("talking mouth should animate")
	}
}

func TestPick_ColorsAndWraps(t *testing.T) {
	out := Pick(state.Now{Activity: "idle"}, 0, false)
	if !strings.HasSuffix(out, reset) {
		t.Errorf("face should end with reset code, got %q", out)
	}
	if !strings.Contains(out, "[") || !strings.Contains(out, "]") {
		t.Errorf("missing bracketed face, got %q", out)
	}
}
