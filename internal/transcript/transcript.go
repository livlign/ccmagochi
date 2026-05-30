// Package transcript locates, tails, and classifies the Claude Code session
// JSONL into events. Mapping is Phase-0 probe-confirmed (see PROBE.md):
// timestamps→deltas, subagent tool = "Agent", meta entry types ignored.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"time"

	"ccmagotchi/internal/events"
)

// --- raw entry shapes (only the fields we use) ---

type rawEntry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type      string         `json:"type"`
	Name      string         `json:"name"`        // tool_use
	Input     map[string]any `json:"input"`       // tool_use
	ID        string         `json:"id"`          // tool_use
	ToolUseID string         `json:"tool_use_id"` // tool_result
	IsError   bool           `json:"is_error"`    // tool_result
}

func parseTS(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// --- Classifier: streaming state across entries ---

type toolInfo struct {
	name string
	ts   int64
	cmd  string // Bash command (for commit/revert/test detection)
}

// dev-event detection from Bash command strings (v1.6)
func isCommit(cmd string) bool { return strings.Contains(cmd, "git commit") }
func isRevert(cmd string) bool {
	for _, p := range []string{"git revert", "git restore", "git reset", "git checkout -- ", "git checkout ."} {
		if strings.Contains(cmd, p) {
			return true
		}
	}
	return false
}
func isTestCmd(cmd string) bool {
	for _, p := range []string{"go test", "npm test", "npm run test", "yarn test", "pnpm test", "pytest", "jest", "vitest", "cargo test", "rspec", "phpunit", "dotnet test"} {
		if strings.Contains(cmd, p) {
			return true
		}
	}
	return false
}

type Classifier struct {
	prevTS    int64
	openTools map[string]toolInfo // tool_use id -> {name, spawn ts}
}

func NewClassifier() *Classifier {
	return &Classifier{openTools: map[string]toolInfo{}}
}

// Classify turns one JSONL line into zero or more events.
func (c *Classifier) Classify(line []byte) []events.Event {
	var e rawEntry
	if json.Unmarshal(line, &e) != nil {
		return nil
	}
	switch e.Type {
	case "user", "assistant", "system":
		// process
	default:
		return nil // ignore meta: mode, permission-mode, ai-title, last-prompt, file-history-snapshot, attachment
	}
	ts := parseTS(e.Timestamp)

	var blocks []block
	_ = json.Unmarshal(e.Message.Content, &blocks) // string content → blocks stays empty

	var out []events.Event
	emit := func(t string, d map[string]any) { out = append(out, events.Event{TS: ts, Type: t, Data: d}) }

	switch e.Type {
	case "assistant":
		for _, b := range blocks {
			switch b.Type {
			case "thinking":
				if c.prevTS > 0 && ts > c.prevTS {
					emit("thinking_turn", map[string]any{"duration_ms": ts - c.prevTS})
				}
			case "tool_use":
				if b.Name == "Agent" {
					c.openTools[b.ID] = toolInfo{"Agent", ts, ""}
					emit("subagent_spawn", map[string]any{"subagent_type": str(b.Input["subagent_type"])})
				} else {
					cmd := str(b.Input["command"]) // Bash
					c.openTools[b.ID] = toolInfo{b.Name, ts, cmd}
					d := map[string]any{"name": b.Name}
					if f := str(b.Input["file_path"]); f != "" {
						d["file"] = f
					}
					emit("tool_call", d)
					if b.Name == "Bash" { // dev events from the command itself
						if isCommit(cmd) {
							emit("commit", nil)
						} else if isRevert(cmd) {
							emit("revert", nil)
						}
					}
				}
			}
		}
	case "user":
		sawToolResult := false
		for _, b := range blocks {
			if b.Type != "tool_result" {
				continue
			}
			sawToolResult = true
			info, ok := c.openTools[b.ToolUseID]
			delete(c.openTools, b.ToolUseID)
			dur := int64(0)
			if ok && ts > info.ts {
				dur = ts - info.ts
			}
			switch {
			case b.IsError:
				emit("error", map[string]any{"name": info.name})
			case ok && info.name == "Agent":
				emit("subagent_done", map[string]any{"duration_ms": dur})
			default:
				emit("tool_done", map[string]any{"duration_ms": dur, "name": info.name})
			}
			// test pass/fail from a Bash test command (exit code ≈ is_error)
			if ok && info.name == "Bash" && isTestCmd(info.cmd) {
				if b.IsError {
					emit("test_fail", nil)
				} else {
					emit("test_pass", nil)
				}
			}
		}
		if !sawToolResult {
			emit("prompt_submit", nil) // a genuine user turn (content was text)
		}
	}

	if ts > 0 {
		c.prevTS = ts
	}
	return out
}

// OpenAgents / OpenTools drive the current activity (delegating / tool_running).
func (c *Classifier) OpenAgents() int {
	n := 0
	for _, t := range c.openTools {
		if t.name == "Agent" {
			n++
		}
	}
	return n
}
func (c *Classifier) OpenTools() int { return len(c.openTools) }

// OldestOpenToolStart returns the spawn ts of the longest-running open tool
// (0 if none) — drives the "warning: taking too long" tone.
func (c *Classifier) OldestOpenToolStart() int64 {
	var oldest int64
	for _, t := range c.openTools {
		if oldest == 0 || t.ts < oldest {
			oldest = t.ts
		}
	}
	return oldest
}

// --- Tailer: read only complete new lines, tracking byte offset ---

type Tailer struct {
	path   string
	offset int64
}

func NewTailer(path string) *Tailer { return &Tailer{path: path} }

// NewTailerAtEnd starts at the current end of file, so the daemon watches new
// activity from attach-time instead of replaying the whole session history
// (which would cold-spike the mood).
func NewTailerAtEnd(path string) *Tailer {
	t := &Tailer{path: path}
	if fi, err := os.Stat(path); err == nil {
		t.offset = fi.Size()
	}
	return t
}

func (t *Tailer) ReadNew() [][]byte {
	f, err := os.Open(t.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	if fi.Size() < t.offset { // file replaced/rotated
		t.offset = 0
	}
	if _, err := f.Seek(t.offset, 0); err != nil {
		return nil
	}
	data, _ := bufio.NewReader(f).ReadString(0) // read to EOF (no NUL in JSONL)
	if len(data) == 0 {
		return nil
	}
	nl := bytes.LastIndexByte([]byte(data), '\n')
	if nl < 0 {
		return nil // no complete line yet
	}
	complete := data[:nl+1]
	t.offset += int64(len(complete))
	var lines [][]byte
	for _, ln := range bytes.Split([]byte(complete), []byte{'\n'}) {
		if len(bytes.TrimSpace(ln)) > 0 {
			lines = append(lines, append([]byte(nil), ln...))
		}
	}
	return lines
}
