package daemon

import (
	"os"
	"strings"
	"testing"
	"time"

	"ccmagotchi/internal/config"
	"ccmagotchi/internal/state"
	"ccmagotchi/internal/world"
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

// L2/L3 wiring: a session with a file tool_call persists recent.json (daily
// counts, file recency) and traits.json (lifetime distributions).
func TestTick_PersistsLayer2And3(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	tr := tmp + "/t.jsonl"
	os.WriteFile(tr, []byte(`{"type":"user","timestamp":"2026-05-31T00:00:00.000Z","message":{"role":"user","content":"x"}}`+"\n"), 0o644)
	state.WriteSession(cfg.SessionPath(), state.Session{TranscriptPath: tr})

	d := New(cfg)
	d.Tick() // attach at end
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"assistant","timestamp":"2026-05-31T10:00:01.000Z","message":{"content":[{"type":"tool_use","id":"e","name":"Read","input":{"file_path":"/x/main.go"}}]}}` + "\n")
	f.Close()
	d.Tick()

	if _, err := os.Stat(cfg.RecentPath()); err != nil {
		t.Errorf("recent.json should be written: %v", err)
	}
	if _, err := os.Stat(cfg.TraitsPath()); err != nil {
		t.Errorf("traits.json should be written: %v", err)
	}
	if d.recent.Today(d.lastEventTS).Files != 1 {
		t.Errorf("Layer 2 should count 1 file today, got %d", d.recent.Today(d.lastEventTS).Files)
	}
	if d.traits.FileTotal["/x/main.go"] != 1 {
		t.Errorf("Layer 3 should count the file touch, got %+v", d.traits.FileTotal)
	}
	if d.recent.MarkSession(d.lastEventTS) { // already marked this session → not first now
		t.Error("session should have been marked on the first event")
	}
}

// Vocabulary evolution: a text prompt is harvested into the lexicon, and the
// raw text is STRIPPED from events.log (privacy).
func TestTick_HarvestsPromptIntoLexicon(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	tr := tmp + "/t.jsonl"
	os.WriteFile(tr, []byte(`{"type":"user","timestamp":"2026-05-31T00:00:00.000Z","message":{"role":"user","content":"seed"}}`+"\n"), 0o644)
	state.WriteSession(cfg.SessionPath(), state.Session{TranscriptPath: tr})

	d := New(cfg)
	d.Tick() // attach at end
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"user","timestamp":"2026-05-31T00:00:01.000Z","message":{"role":"user","content":"yeah lets ship /secret/path lol"}}` + "\n")
	f.Close()
	d.Tick()

	if d.lexicon.Prompts != 1 {
		t.Errorf("prompt should be harvested, got %d prompts", d.lexicon.Prompts)
	}
	if d.lexicon.Flavor["ship"] != 1 || d.lexicon.Flavor["lol"] != 1 {
		t.Errorf("flavor words should be counted, got %+v", d.lexicon.Flavor)
	}
	b, _ := os.ReadFile(cfg.EventsPath())
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "\"text\"") {
		t.Errorf("raw prompt text must be stripped from events.log:\n%s", b)
	}
}

// Token burn (Layer 1): a usage event surfaces as TokenBurn in now.json.
func TestTick_TokenBurn(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	tr := tmp + "/t.jsonl"
	os.WriteFile(tr, []byte(`{"type":"user","timestamp":"2026-05-31T00:00:00.000Z","message":{"role":"user","content":"x"}}`+"\n"), 0o644)
	state.WriteSession(cfg.SessionPath(), state.Session{TranscriptPath: tr})

	d := New(cfg)
	d.Tick()
	// burn is measured over the last 2 wall-clock minutes, so the event must be
	// ~now (in production the transcript is tailed in real time).
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"assistant","timestamp":"` + ts + `","message":{"role":"assistant","content":[{"type":"text"}],"usage":{"output_tokens":2000}}}` + "\n")
	f.Close()
	d.Tick()

	n, _ := state.ReadNow(cfg.NowPath())
	if n.TokenBurn <= 0 {
		t.Errorf("a usage event should produce a positive token burn, got %v", n.TokenBurn)
	}
}

// Decorations: a passing test fires a sparkle ✦ in now.json (pet-decorations).
func TestTick_DecorationSparkleOnTestPass(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	tr := tmp + "/t.jsonl"
	os.WriteFile(tr, []byte(`{"type":"user","timestamp":"2026-05-31T00:00:00.000Z","message":{"role":"user","content":"x"}}`+"\n"), 0o644)
	state.WriteSession(cfg.SessionPath(), state.Session{TranscriptPath: tr})

	d := New(cfg)
	d.Tick()
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"assistant","timestamp":"2026-05-31T00:00:01.000Z","message":{"content":[{"type":"tool_use","id":"tp","name":"Bash","input":{"command":"go test ./..."}}]}}` + "\n")
	f.WriteString(`{"type":"user","timestamp":"2026-05-31T00:00:02.000Z","message":{"content":[{"type":"tool_result","tool_use_id":"tp"}]}}` + "\n")
	f.Close()
	d.Tick()

	n, _ := state.ReadNow(cfg.NowPath())
	if n.Decor != "✦" {
		t.Errorf("a passing test should sparkle ✦, got Decor=%q", n.Decor)
	}
}

// World: with a known terminal width, the daemon places the dog at a column and
// spawns at least the edge-anchored sun/moon scenery.
func TestTick_World(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	tr := tmp + "/t.jsonl"
	os.WriteFile(tr, []byte(`{"type":"user","timestamp":"2026-05-31T00:00:00.000Z","message":{"role":"user","content":"x"}}`+"\n"), 0o644)
	state.WriteSession(cfg.SessionPath(), state.Session{TranscriptPath: tr, Cols: 120})

	d := New(cfg)
	d.Tick()
	d.Tick()
	n, _ := state.ReadNow(cfg.NowPath())
	if len(n.Scenery) == 0 {
		t.Error("a wide terminal should have scenery (at least the sun/moon)")
	}
	if n.Heading == "" {
		t.Error("heading should be set in the world")
	}
	if n.Pos < 0 || n.Pos > 90 {
		t.Errorf("dog position should be within usable width, got %d", n.Pos)
	}
}

// World: a running subagent becomes a robot in subagents.json.
func TestTick_SubagentRobot(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	tr := tmp + "/t.jsonl"
	os.WriteFile(tr, []byte(`{"type":"user","timestamp":"2026-05-31T00:00:00.000Z","message":{"role":"user","content":"x"}}`+"\n"), 0o644)
	state.WriteSession(cfg.SessionPath(), state.Session{TranscriptPath: tr, Cols: 120})

	d := New(cfg)
	d.Tick()
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(`{"type":"assistant","timestamp":"2026-05-31T00:00:01.000Z","message":{"content":[{"type":"tool_use","id":"ag1","name":"Agent","input":{"subagent_type":"Explore"}}]}}` + "\n")
	f.Close()
	d.Tick()

	subs := state.ReadSubagents(cfg.SubagentsPath())
	if len(subs) != 1 || subs[0].Status != "running" {
		t.Fatalf("an open Agent should be one running robot, got %+v", subs)
	}
}

// #2: a daemon only "owns" the lock when the file holds its own PID — the basis
// for an orphaned instance standing down instead of racing now.json.
func TestOwnsLock(t *testing.T) {
	p := t.TempDir() + "/daemon.lock"
	os.WriteFile(p, []byte(itoa(os.Getpid())), 0o644)
	if !ownsLock(p) {
		t.Error("our own PID → owns the lock")
	}
	os.WriteFile(p, []byte("999999999"), 0o644)
	if ownsLock(p) {
		t.Error("a different PID → does not own the lock")
	}
	os.Remove(p)
	if ownsLock(p) {
		t.Error("no lock file → does not own the lock")
	}
}

// pet-world §3: the dog drifts in SHORT local hops within a central band (never
// to the walls), and biases toward the ⚙ while a tool runs.
func TestPickTarget_LocalDriftAndGear(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{StateDir: tmp + "/state"}
	cfg.EnsureStateDir()
	d := New(cfg)
	// idle drift from mid-band: short hops (±10) that never hit a wall.
	for i := 0; i < 60; i++ {
		got := d.pickTarget(60, 30, "idle") // band [10,50]
		if got == 0 || got == 60 {
			t.Errorf("idle drift must avoid the walls, got %d", got)
		}
		if got < 20 || got > 40 {
			t.Errorf("an idle hop from 30 should be local (±10), got %d", got)
		}
	}
	// while a tool runs, hops bias toward the ⚙ (here to the right of the dog).
	d.sceneryM["gear"] = &world.Scenery{Glyph: "⚙", Pos: 50}
	for i := 0; i < 30; i++ {
		got := d.pickTarget(60, 20, "tool_running")
		if got <= 20 {
			t.Errorf("during a tool the dog should drift toward the ⚙ (right of 20), got %d", got)
		}
		if got-20 > 10 {
			t.Errorf("a hop should stay short (≤10), got %d from 20", got)
		}
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
