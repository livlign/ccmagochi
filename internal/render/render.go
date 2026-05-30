// Package render is the fast statusLine path (<50ms). It does NO transcript
// parsing or mood math — it reads now.json and formats one line, optionally
// below the user's existing status line(s).
package render

import (
	"bytes"
	"os/exec"
	"strings"

	"ccmagotchi/internal/config"
	"ccmagotchi/internal/face"
	"ccmagotchi/internal/state"
)

// Run composes the output Claude Code shows: the user's existing status line(s)
// (if a base command is configured) followed by the pet line. stdin is the raw
// statusLine JSON from Claude Code; it's piped through to the base command so
// the user's command sees exactly what it normally would.
func Run(stdin []byte, cfg config.Config) string {
	pet := petLine(cfg)
	base := baseLines(stdin, cfg)
	if base == "" {
		return pet // user has 0 existing lines — pet is the whole status line
	}
	return base + "\n" + pet
}

// petLine reads now.json and renders the face (+ remark). Never errors: if the
// daemon hasn't written state yet, it shows a neutral idle face.
func petLine(cfg config.Config) string {
	n, err := state.ReadNow(cfg.NowPath())
	if err != nil {
		n = state.Now{Activity: "idle"}
	}
	line := face.Pick(n)
	if n.Remark != nil && n.Remark.Text != "" {
		line += " " + n.Remark.Text
	}
	return line
}

// baseLines runs the user's existing statusLine command (if any), piping the
// same stdin, and returns its output as the upper line(s). On any failure it
// returns "" so the pet still shows — the base command must never break us.
func baseLines(stdin []byte, cfg config.Config) string {
	if strings.TrimSpace(cfg.BaseStatusCommand) == "" {
		return ""
	}
	cmd := exec.Command("sh", "-c", cfg.BaseStatusCommand) // Phase 5: Windows variant
	cmd.Stdin = bytes.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimRight(out.String(), "\n")
}
