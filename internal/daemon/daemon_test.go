package daemon

import (
	"os"
	"strings"
	"testing"

	"ccmagotchi/internal/config"
	"ccmagotchi/internal/state"
)

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
