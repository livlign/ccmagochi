package world

import (
	"strings"
	"testing"
)

func TestTierAndUsable(t *testing.T) {
	cases := []struct {
		cols   int
		tier   string
		usable int
	}{
		{40, "compact", 10}, {60, "yard", 30}, {100, "garden", 70}, {130, "park", 100},
	}
	for _, c := range cases {
		if g := Tier(c.cols); g != c.tier {
			t.Errorf("Tier(%d)=%q want %q", c.cols, g, c.tier)
		}
		if g := Usable(c.cols); g != c.usable {
			t.Errorf("Usable(%d)=%d want %d", c.cols, g, c.usable)
		}
	}
}

func TestWidth(t *testing.T) {
	if w := Width("≡(•ᴥ•)≡"); w != 7 {
		t.Errorf("dog width want 7 got %d", w)
	}
	if w := Width("🌲"); w != 2 { // emoji = 2 columns
		t.Errorf("emoji width want 2 got %d", w)
	}
	if w := Width("\x1b[32mx\x1b[0m"); w != 1 { // ANSI escapes are zero-width
		t.Errorf("ansi-wrapped char want 1 got %d", w)
	}
}

func TestCompose_DogAtColumn(t *testing.T) {
	out := Compose(80, "(d)", 5, nil, nil, 0, "") // usable 50
	if !strings.HasPrefix(out, strings.Repeat(" ", 5)+"(d)") {
		t.Errorf("dog should sit at column 5, got %q", out)
	}
}

func TestCompose_Compact(t *testing.T) {
	// usable 10 (<12) → dog only
	if out := Compose(40, "(d)", 0, nil, nil, 0, ""); out != "(d)" {
		t.Errorf("compact should be dog-only, got %q", out)
	}
	// compact with robots → +N
	robots := []Item{{Col: 0, W: 5, S: "[r]"}}
	if out := Compose(40, "(d)", 0, nil, robots, 2, ""); !strings.Contains(out, "+3") {
		t.Errorf("compact should collapse robots to +N (1 shown-arg + 2 overflow = 3), got %q", out)
	}
}

func TestCompose_SceneryAndAmbient(t *testing.T) {
	out := Compose(120, "(d)", 2, []Scenery{{Glyph: "T", Pos: 40}}, nil, 0, "note") // usable 90
	if !strings.Contains(out, "T") {
		t.Errorf("scenery should render, got %q", out)
	}
	if !strings.Contains(out, "note") {
		t.Errorf("ambient text should render near the right edge, got %q", out)
	}
	// ambient is right-anchored: it appears after the scenery
	if strings.Index(out, "note") < strings.Index(out, "T") {
		t.Errorf("ambient should be right of scenery, got %q", out)
	}
}

func TestCompose_DogClampedIntoWorld(t *testing.T) {
	// pos beyond the edge clamps so the dog stays visible
	out := Compose(60, "(d)", 999, nil, nil, 0, "") // usable 30
	if !strings.Contains(out, "(d)") {
		t.Errorf("dog must stay on-screen even with an out-of-range pos, got %q", out)
	}
}
