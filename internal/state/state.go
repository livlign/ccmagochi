// Package state holds the layered state shapes. v1 = Layer 1 (now.json) only.
package state

import (
	"encoding/json"
	"os"
	"strings"
)

// Mood is the fixed 5-variable model (continuous 0..1). The renderer maps it to
// a face; the daemon (Phase 3) updates and decays it. Adding a 6th var is a rare
// model revision, not routine growth.
type Mood struct {
	Energy    float64 `json:"energy"`
	Stress    float64 `json:"stress"`
	Affection float64 `json:"affection"`
	Curiosity float64 `json:"curiosity"`
	Tiredness float64 `json:"tiredness"`
}

// Remark is an optional spoken line, set by the daemon when a trigger fires.
type Remark struct {
	Text      string `json:"text"`
	ExpiresMs int64  `json:"expires_ms"`
}

// Now is Layer 1 — the only file the renderer reads. Overwritten ~1s by the daemon.
type Now struct {
	Mood        Mood    `json:"mood"`
	Activity    string  `json:"activity"`      // thinking|tool_running|delegating|idle
	Tone        string  `json:"tone"`          // transient color hint: error|warning|success|""
	StateHeldMs int64   `json:"state_held_ms"` // how long Activity has been stable (habitat gating)
	LastTool    string  `json:"last_tool"`     // last tool name (Edit/Read/Bash/Agent/...) for accessory
	OpenToolMs  int64   `json:"open_tool_ms"`  // age of oldest open tool (warning escalation)
	EventFace   string  `json:"event_face"`    // transient dev-event reaction: skeptical|disapproving|satisfied|""
	TokenBurn   float64 `json:"token_burn"`    // output tokens/min over a recent window (Layer 1)
	Greeting    bool    `json:"greeting"`      // paw-up greeting (after a prompt / session start)
	Bark        string  `json:"bark"`          // transient bark text beside the dog ("" = silent)
	Decor       string  `json:"decor"`         // the single active mood symbol (zZ/…/♥/✦/;/'…) ("" = none)
	Sound       string  `json:"sound"`         // italic sound emission (*huff* etc.) ("" = none)
	Remark      *Remark `json:"remark"`
}

// ReadNow loads now.json. Callers fall back to a neutral Now on error so the
// renderer never blocks or errors when the daemon hasn't written yet.
func ReadNow(path string) (Now, error) {
	var n Now
	b, err := os.ReadFile(path)
	if err != nil {
		return n, err
	}
	return n, json.Unmarshal(b, &n)
}

// WriteNow writes now.json atomically (temp + rename) so the renderer never
// reads a half-written file.
func WriteNow(path string, n Now) error {
	b, err := json.Marshal(n)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Session is the pointer the renderer drops (from CC's statusLine stdin) so the
// daemon knows which transcript to tail.
type Session struct {
	TranscriptPath string `json:"transcript_path"`
	SessionID      string `json:"session_id"`
	Cwd            string `json:"cwd"` // workspace dir (→ repo affinity, Layer 3)
}

// Subagent is one agent companion shown beside the dog while Claude delegates,
// written to subagents.json by the daemon.
type Subagent struct {
	ID      string `json:"id"`
	Status  string `json:"status"`  // running | done
	Variant int    `json:"variant"` // eye-variant index (stable per id)
	SinceMs int64  `json:"since_ms"`
}

func WriteSubagents(path string, subs []Subagent) error {
	b, _ := json.Marshal(subs)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func ReadSubagents(path string) []Subagent {
	var subs []Subagent
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &subs)
	}
	return subs
}

func WriteSession(path string, s Session) error {
	b, _ := json.Marshal(s)
	return os.WriteFile(path, b, 0o644)
}

func ReadSession(path string) (Session, error) {
	var s Session
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// Heartbeat is written by the `ccmagotchi hook` subcommand on Claude Code
// lifecycle events — a real-time active/idle signal the transcript can't give
// (a long thinking turn writes nothing until it completes).
type Heartbeat struct {
	TS    int64  `json:"ts"`
	Event string `json:"event"` // UserPromptSubmit | PreToolUse | PostToolUse | Stop | ...
	Tool  string `json:"tool"`  // tool name on PreToolUse (else "")
}

func WriteHeartbeat(path string, h Heartbeat) error {
	b, _ := json.Marshal(h)
	return os.WriteFile(path, b, 0o644)
}

func ReadHeartbeat(path string) (Heartbeat, error) {
	var h Heartbeat
	b, err := os.ReadFile(path)
	if err != nil {
		return h, err
	}
	return h, json.Unmarshal(b, &h)
}

// AppendRemarked logs an emitted remark (repetition avoidance + later analysis).
func AppendRemarked(path, trigger, text string, ts int64) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(map[string]any{"ts": ts, "trigger": trigger, "text": text})
	_, err = f.Write(append(b, '\n'))
	return err
}

// RecentRemarks returns the last n remark texts (for cross-session recency).
func RecentRemarks(path string, n int) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var texts []string
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		var r struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(ln), &r) == nil && r.Text != "" {
			texts = append(texts, r.Text)
		}
	}
	if len(texts) > n {
		texts = texts[len(texts)-n:]
	}
	return texts
}
