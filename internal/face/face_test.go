package face

import (
	"strings"
	"testing"

	"ccmagotchi/internal/state"
)

func TestSelectFace_PriorityTable(t *testing.T) {
	cases := []struct {
		name string
		n    state.Now
		want string
	}{
		{"stress wins over everything", state.Now{Mood: state.Mood{Stress: 0.9}, Activity: "tool_running"}, "[x_x]"},
		{"tired", state.Now{Mood: state.Mood{Tiredness: 0.9}}, "[-_-]"},
		{"delegating", state.Now{Activity: "delegating"}, "[◉_◉]→"},
		{"tool running", state.Now{Activity: "tool_running"}, "[◉_◉]9"},
		{"curious", state.Now{Mood: state.Mood{Curiosity: 0.8, Tiredness: 0.1}}, "[O_O]"},
		{"neutral default", state.Now{Activity: "idle"}, "[◉_◉]"},
	}
	for _, c := range cases {
		if g, _ := selectFace(c.n); g != c.want {
			t.Errorf("%s: want %q got %q", c.name, c.want, g)
		}
	}
}

func TestPick_WrapsColorAndReset(t *testing.T) {
	out := Pick(state.Now{Activity: "idle"})
	if !strings.HasSuffix(out, reset) {
		t.Errorf("face should end with reset code, got %q", out)
	}
	if !strings.Contains(out, "[◉_◉]") {
		t.Errorf("missing glyph, got %q", out)
	}
}
