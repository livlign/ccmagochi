package daemon

import (
	"os"
	"strings"
	"testing"
	"time"

	"ccmagotchi/internal/config"
	"ccmagotchi/internal/state"
)

func TestIsActiveEvent(t *testing.T) {
	for _, e := range []string{"UserPromptSubmit", "PreToolUse", "PostToolUse"} {
		if !isActiveEvent(e) {
			t.Errorf("%s should be active", e)
		}
	}
	for _, e := range []string{"Stop", "Notification", "SessionEnd", ""} {
		if isActiveEvent(e) {
			t.Errorf("%s should NOT be active", e)
		}
	}
}

// The hook heartbeat keeps the pet "thinking" during transcript silence
// (the long-thinking-turn sleep bug, fixed for real).
func TestTick_HeartbeatBeatsSilence(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	stale := time.Now().UnixMilli() - 10*60*1000 // 10 min of transcript silence

	// no heartbeat → silence reads as idle
	d := New(cfg)
	d.lastEventTS = stale
	d.Tick()
	if n, _ := state.ReadNow(cfg.NowPath()); n.Activity != "idle" {
		t.Fatalf("without heartbeat, long silence should be idle, got %q", n.Activity)
	}

	// active heartbeat → same silence is "thinking", not idle
	state.WriteHeartbeat(cfg.HeartbeatPath(), state.Heartbeat{TS: time.Now().UnixMilli(), Event: "UserPromptSubmit"})
	d2 := New(cfg)
	d2.lastEventTS = stale
	d2.Tick()
	if n, _ := state.ReadNow(cfg.NowPath()); n.Activity != "thinking" {
		t.Fatalf("active heartbeat should keep thinking during silence, got %q", n.Activity)
	}
}

// The daemon attaches at end-of-file: it must SKIP pre-existing backlog, then
// react to NEW activity appended after attach. Tick() is the testable unit.
func TestTick_SkipsBacklog_ThenReadsNew(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()

	tr := tmp + "/t.jsonl"
	// pre-existing history the daemon must NOT replay
	os.WriteFile(tr, []byte(`{"type":"user","timestamp":"2026-05-30T00:00:00.000Z","message":{"role":"user","content":"old"}}`+"\n"), 0o644)
	state.WriteSession(cfg.SessionPath(), state.Session{TranscriptPath: tr})

	d := New(cfg)
	d.Tick() // attaches at EOF — ignores the old line
	if _, err := os.Stat(cfg.EventsPath()); err == nil {
		t.Error("backlog should be skipped — no events from pre-existing lines")
	}

	// new activity arrives after attach
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"assistant","timestamp":"2026-05-30T01:00:00.000Z","message":{"content":[{"type":"tool_use","id":"a","name":"Agent","input":{"subagent_type":"Explore"}}]}}` + "\n")
	f.Close()

	d.Tick() // reads the new line
	n, err := state.ReadNow(cfg.NowPath())
	if err != nil {
		t.Fatalf("now.json not written: %v", err)
	}
	if n.Activity != "delegating" {
		t.Errorf("want activity=delegating (open Agent), got %q", n.Activity)
	}
	b, _ := os.ReadFile(cfg.EventsPath())
	if !strings.Contains(string(b), "subagent_spawn") {
		t.Errorf("events.log missing subagent_spawn:\n%s", b)
	}
}

// A tool error sets the transient "error" tone (drives the red color).
func TestTick_ErrorTone(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	tr := tmp + "/t.jsonl"
	os.WriteFile(tr, []byte(`{"type":"user","timestamp":"2026-05-31T00:00:00.000Z","message":{"role":"user","content":"x"}}`+"\n"), 0o644)
	state.WriteSession(cfg.SessionPath(), state.Session{TranscriptPath: tr})

	d := New(cfg)
	d.Tick() // attach at end
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"assistant","timestamp":"2026-05-31T00:00:01.000Z","message":{"content":[{"type":"tool_use","id":"b","name":"Bash"}]}}` + "\n")
	f.WriteString(`{"type":"user","timestamp":"2026-05-31T00:00:02.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"b","is_error":true}]}}` + "\n")
	f.Close()

	d.Tick()
	n, _ := state.ReadNow(cfg.NowPath())
	if n.Tone != "error" {
		t.Errorf("want error tone after a tool error, got %q", n.Tone)
	}
}

// The daemon surfaces the last tool name (drives the edit/read accessory).
func TestTick_SurfacesLastTool(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	tr := tmp + "/t.jsonl"
	os.WriteFile(tr, []byte(`{"type":"user","timestamp":"2026-05-31T00:00:00.000Z","message":{"role":"user","content":"x"}}`+"\n"), 0o644)
	state.WriteSession(cfg.SessionPath(), state.Session{TranscriptPath: tr})

	d := New(cfg)
	d.Tick() // attach at end
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"assistant","timestamp":"2026-05-31T00:00:01.000Z","message":{"content":[{"type":"tool_use","id":"e","name":"Edit","input":{"file_path":"/a.go"}}]}}` + "\n")
	f.Close()

	d.Tick()
	n, _ := state.ReadNow(cfg.NowPath())
	if n.LastTool != "Edit" {
		t.Errorf("want LastTool=Edit, got %q", n.LastTool)
	}
}

// Touching a new file of the pet's favorite extension → cheeky reaction.
func TestTick_FavoriteFileCheeky(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	os.WriteFile(cfg.QuirksPath(), []byte(`{"favorite_ext":".go","speech_tic":"!","aversion":"Grep","seeded":true}`), 0o644)
	tr := tmp + "/t.jsonl"
	os.WriteFile(tr, []byte(`{"type":"user","timestamp":"2026-05-31T00:00:00.000Z","message":{"role":"user","content":"x"}}`+"\n"), 0o644)
	state.WriteSession(cfg.SessionPath(), state.Session{TranscriptPath: tr})

	d := New(cfg)
	d.Tick() // attach at end
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"assistant","timestamp":"2026-05-31T00:00:01.000Z","message":{"content":[{"type":"tool_use","id":"e","name":"Edit","input":{"file_path":"/x/main.go"}}]}}` + "\n")
	f.Close()

	d.Tick()
	n, _ := state.ReadNow(cfg.NowPath())
	if n.EventFace != "cheeky" {
		t.Errorf("new .go file (the favorite) → cheeky, got %q", n.EventFace)
	}
}

// Mood starts neutral on attach (no cold-replay spike).
func TestTick_StartsNeutral(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	d := New(cfg)
	d.Tick() // no session pointed yet
	n, _ := state.ReadNow(cfg.NowPath())
	if n.Mood.Stress > 0.1 || n.Mood.Tiredness > 0.1 {
		t.Errorf("fresh attach should be calm, got stress=%v tiredness=%v", n.Mood.Stress, n.Mood.Tiredness)
	}
}

// A second Run while the first holds the lock returns nil without writing state.
func TestRun_SingleInstanceLock(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	os.WriteFile(cfg.LockPath(), []byte(itoa(os.Getpid())), 0o644) // live lock (this process)

	stop := make(chan struct{})
	close(stop)
	if err := Run(cfg, stop); err != nil {
		t.Fatalf("locked Run should no-op, got %v", err)
	}
	if _, err := os.Stat(cfg.NowPath()); err == nil {
		t.Error("locked Run should not have written now.json")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
