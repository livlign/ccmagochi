// Package render is the fast statusLine path (<50ms). It does NO transcript
// parsing or mood math — it reads now.json and formats one line, optionally
// below the user's existing status line(s).
package render

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"ccmagotchi/internal/config"
	"ccmagotchi/internal/face"
	"ccmagotchi/internal/state"
)

// Run composes the output Claude Code shows: the pet line and the user's
// existing status line(s) (if a base command is configured). cfg.PetLine
// decides the order — "first" (default) puts the pet on top, "last" puts it
// below. stdin is the raw statusLine JSON from Claude Code; it's piped through
// to the base command so the user's command sees exactly what it normally would.
func Run(stdin []byte, cfg config.Config) string {
	pet := petLine(cfg)
	base := baseLines(stdin, cfg)
	if base == "" {
		return pet // user has 0 existing lines — pet is the whole status line
	}
	if cfg.PetLine == "last" {
		return base + "\n" + pet // pet below the user's existing line(s)
	}
	return pet + "\n" + base // default: pet on the first line, base below
}

// petLine reads now.json, renders the dog (+ remark), and appends the active
// subagent companions when Claude is delegating. The dog always renders at the
// start of the line — no positioning, so it shows identically in every session
// regardless of terminal width. Never errors: a missing state file shows a
// neutral idle dog.
func petLine(cfg config.Config) string {
	n, err := state.ReadNow(cfg.NowPath())
	if err != nil {
		n = state.Now{Activity: "idle"}
	}
	remark := ""
	if n.Remark != nil {
		remark = n.Remark.Text
	}
	tick := time.Now().Unix()
	// Transition smoothing (pet-01-dog §14): the renderer is a fresh process each
	// refresh, so the previous frame's signature is persisted and handed back so a
	// state change is masked by one intermediate frame (blink / dot).
	prev := readFrame(cfg.FramePath())
	dog, cur := face.PickFrame(n, tick, remark, prev)
	writeFrame(cfg.FramePath(), cur)
	if remark != "" {
		return dog // the spoken sentence is the whole line; no companions beside it
	}
	return dog + subagentTag(cfg)
}

const (
	maxRobots = 3 // agent faces shown inline before the count carries the rest
	dimAnsi   = "\x1b[2;37m"
	reset     = "\x1b[0m"
)

// subagentTag renders the agent companions beside the dog while Claude is
// delegating: up to maxRobots agent faces plus a dim count of how many are
// active ("[◉_◉] [◦_◦] ·2"). Empty when nothing is delegating. The whole tag is
// dimmed so the dog stays the focus.
func subagentTag(cfg config.Config) string {
	subs := state.ReadSubagents(cfg.SubagentsPath())
	if len(subs) == 0 {
		return ""
	}
	sort.Slice(subs, func(i, j int) bool { return subs[i].SinceMs < subs[j].SinceMs }) // stable: oldest first
	var faces strings.Builder
	for i, s := range subs {
		if i >= maxRobots {
			break
		}
		faces.WriteString(robotFace(s) + " ")
	}
	return " " + dimAnsi + faces.String() + "·" + strconv.Itoa(len(subs)) + reset
}

// robotFace renders a subagent companion — mechanical contrast to the dog:
// square brackets, dot eyes, no snout/tail/bark. The contrast between the framed
// white-bg dog and these unframed brackets reads as biological vs mechanical.
var robotEyes = []string{"◉", "◦", "·"} // variants so concurrent robots differ

func robotFace(s state.Subagent) string {
	switch s.Status {
	case "done":
		return "[-_-]" // completed, fading
	case "errored":
		return "[×_×]" // stuck (daemon error tracking pending)
	case "alarmed":
		return "[O_O]" // something went wrong
	default:
		e := robotEyes[s.Variant%len(robotEyes)]
		return "[" + e + "_" + e + "]"
	}
}

// readFrame loads the previous render's transition signature. A missing or
// unreadable file yields the zero Frame (Mood ""), which PickFrame treats as
// "no history" and snaps to the target.
func readFrame(path string) face.Frame {
	var f face.Frame
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	return f
}

// writeFrame persists the current frame signature (best-effort, atomic).
func writeFrame(path string, f face.Frame) {
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// baseLines runs the user's existing statusLine command (if any), piping the
// same stdin, and returns its output as the upper line(s). On any failure it
// returns "" so the pet still shows — the base command must never break us.
func baseLines(stdin []byte, cfg config.Config) string {
	if strings.TrimSpace(cfg.BaseStatusCommand) == "" {
		return ""
	}
	shell, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, flag = "cmd", "/c"
	}
	cmd := exec.Command(shell, flag, cfg.BaseStatusCommand)
	cmd.Stdin = bytes.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimRight(out.String(), "\n")
}
