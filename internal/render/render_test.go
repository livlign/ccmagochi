package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ccmagotchi/internal/config"
)

func writeNow(t *testing.T, dir, json string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "now.json"), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Pet line appended below the user's existing line, in order.
// (Glyph is animated, so assert the expression class, not an exact frame.)
func TestRun_ComposesBaseThenPet(t *testing.T) {
	dir := t.TempDir()
	writeNow(t, dir, `{"mood":{"curiosity":0.8,"tiredness":0.1,"energy":0.6},"activity":"idle"}`)
	cfg := config.Config{StateDir: dir, BaseStatusCommand: `echo "DASH | Ctx: 50%"`}

	out := Run([]byte(`{"model":{"display_name":"x"}}`), cfg)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), out)
	}
	if lines[0] != "DASH | Ctx: 50%" {
		t.Errorf("base line wrong: %q", lines[0])
	}
	if !strings.Contains(lines[1], "[36m") { // curious → cyan (stable; posture animates)
		t.Errorf("want curious cyan on line 2, got %q", lines[1])
	}
}

// stdin is piped through to the base command.
func TestRun_PipesStdinToBase(t *testing.T) {
	dir := t.TempDir()
	writeNow(t, dir, `{"activity":"idle"}`)
	cfg := config.Config{StateDir: dir, BaseStatusCommand: `cat | sed 's/.*"m":"//;s/".*//'`}
	out := Run([]byte(`{"m":"hello"}`), cfg)
	if !strings.HasPrefix(out, "hello\n") {
		t.Errorf("base command did not receive stdin; got %q", out)
	}
}

// No base command → pet is the only line (a bracketed face, whatever frame).
func TestRun_NoBase_OnlyPet(t *testing.T) {
	dir := t.TempDir()
	writeNow(t, dir, `{"activity":"idle"}`)
	out := Run(nil, config.Config{StateDir: dir})
	if strings.Contains(out, "\n") {
		t.Errorf("want single line, got %q", out)
	}
	if !strings.Contains(out, "_") { // a face is present (mouth char)
		t.Errorf("want a face, got %q", out)
	}
}

// Daemon hasn't written now.json yet → never errors, still renders a face.
func TestRun_MissingNow_NeverErrors(t *testing.T) {
	out := Run(nil, config.Config{StateDir: t.TempDir()})
	if !strings.Contains(out, "_") {
		t.Errorf("want a fallback face, got %q", out)
	}
}

// High stress (idle) renders the distressed face.
func TestRun_DistressedFace(t *testing.T) {
	dir := t.TempDir()
	writeNow(t, dir, `{"mood":{"stress":0.95},"activity":"idle"}`)
	out := Run(nil, config.Config{StateDir: dir})
	if !strings.Contains(out, ">_<") {
		t.Errorf("want distressed face, got %q", out)
	}
}
