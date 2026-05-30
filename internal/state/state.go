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
	Mood     Mood    `json:"mood"`
	Activity string  `json:"activity"` // thinking|tool_running|delegating|idle
	Remark   *Remark `json:"remark"`
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
