package events

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppend_WritesJSONLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "e.log")
	if err := Append(p, Event{TS: 1, Type: "tool_call"}, Event{TS: 2, Type: "error"}); err != nil {
		t.Fatal(err)
	}
	// appends, doesn't overwrite
	if err := Append(p, Event{TS: 3, Type: "idle"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if n := strings.Count(string(b), "\n"); n != 3 {
		t.Fatalf("want 3 lines, got %d:\n%s", n, b)
	}
	if !strings.Contains(string(b), `"type":"error"`) {
		t.Errorf("missing error event:\n%s", b)
	}
}

func TestNum_Coercion(t *testing.T) {
	cases := []struct {
		in   any
		want int64
	}{
		{float64(5), 5}, // JSON round-trips numbers as float64
		{int64(7), 7},
		{int(9), 9},
		{"nope", 0},
		{nil, 0},
	}
	for _, c := range cases {
		if got := Num(c.in); got != c.want {
			t.Errorf("Num(%v): want %d got %d", c.in, c.want, got)
		}
	}
}
