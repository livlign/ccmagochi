// Package daemon is the slow loop: tail the transcript, fold events into mood,
// evaluate triggers, and write now.json ~once a second. Single instance.
// It attaches at end-of-file (watches new activity), it does NOT replay history.
package daemon

import (
	"os"
	"strconv"
	"strings"
	"time"

	"ccmagotchi/internal/config"
	"ccmagotchi/internal/events"
	"ccmagotchi/internal/mood"
	"ccmagotchi/internal/persona"
	"ccmagotchi/internal/state"
	"ccmagotchi/internal/transcript"
	"ccmagotchi/internal/triggers"
)

// Daemon holds the streaming state. Tick() is the unit of work (one tick).
type Daemon struct {
	cfg          config.Config
	p            persona.Persona
	eng          *triggers.Engine
	cls          *transcript.Classifier
	tailer       *transcript.Tailer
	curPath      string
	m            state.Mood
	sessionStart int64
	seenFiles    map[string]bool
	fileRepeat   map[string]int
	toolMaxMs    int64
	thinkMaxMs   int64
	filesCount   int
	lastEventTS  int64
	curRemark     *state.Remark
	flashTone     string // "error"|"success" transient color, with expiry
	flashExpires  int64
	lastTool      string // last tool name seen (for the edit/read accessory)
	prevActivity  string
	activitySince int64 // ms when the current activity began (for habitat gating)
	eventFace     string // transient dev-event reaction (skeptical/disapproving/satisfied)
	eventFaceExp  int64
	lastCommitMs  int64 // for "revert soon after commit → skeptical"
	revertStreak  int   // consecutive reverts (no commit between) → disapproving
	lastTick      time.Time
}

func New(cfg config.Config) *Daemon {
	cfg.EnsureStateDir()
	p := persona.Load(cfg.PersonaPath())
	vocab := persona.LoadVocab(cfg.VocabPath())
	return &Daemon{
		cfg:         cfg,
		p:           p,
		eng:         triggers.NewEngine(p, vocab, state.RecentRemarks(cfg.RemarkedPath(), p.RecencyWindow), time.Now().UnixNano()),
		cls:         transcript.NewClassifier(),
		m:           state.Mood{Energy: 0.7},
		seenFiles:   map[string]bool{},
		fileRepeat:  map[string]int{},
		lastEventTS: time.Now().UnixMilli(),
		lastTick:    time.Now(),
	}
}

// Tick does one observation cycle: read new transcript lines, update mood,
// decay, evaluate triggers, write now.json.
func (d *Daemon) Tick() {
	// (re)attach to the active session, watching from NOW (skip backlog).
	if s, err := state.ReadSession(d.cfg.SessionPath()); err == nil && s.TranscriptPath != "" && s.TranscriptPath != d.curPath {
		d.curPath = s.TranscriptPath
		d.tailer = transcript.NewTailerAtEnd(d.curPath)
		d.cls = transcript.NewClassifier()
		d.sessionStart = 0
	}

	justDelegated, errFlash, doneFlash := false, false, false
	justCommitted, justReverted, justTestPass, justTestFail := false, false, false, false
	newEventFace := ""
	if d.tailer != nil {
		for _, line := range d.tailer.ReadNew() {
			for _, e := range d.cls.Classify(line) {
				events.Append(d.cfg.EventsPath(), e)
				d.lastEventTS = e.TS
				if d.sessionStart == 0 {
					d.sessionStart = e.TS
				}
				mood.Apply(&d.m, e, d.p)
				switch e.Type {
				case "error":
					errFlash = true
				case "tool_done":
					v := events.Num(e.Data["duration_ms"])
					if v > d.toolMaxMs {
						d.toolMaxMs = v
					}
					if v > d.p.LongToolCallMs {
						doneFlash = true // a long-awaited task finished → success
					}
				case "thinking_turn":
					if v := events.Num(e.Data["duration_ms"]); v > d.thinkMaxMs {
						d.thinkMaxMs = v
					}
				case "tool_call":
					if name, ok := e.Data["name"].(string); ok {
						d.lastTool = name
					}
					if f, ok := e.Data["file"].(string); ok && f != "" {
						d.fileRepeat[f]++
						if !d.seenFiles[f] {
							d.seenFiles[f] = true
							d.filesCount++
							mood.BumpCuriosity(&d.m, 0.2)
						}
					}
				case "subagent_spawn":
					d.lastTool = "Agent"
					justDelegated = true
				case "commit":
					justCommitted = true
					d.lastCommitMs = e.TS
					d.revertStreak = 0
					d.m.Affection = clampUnit(d.m.Affection + 0.4)
				case "revert":
					justReverted = true
					d.revertStreak++
					if d.revertStreak >= 2 {
						newEventFace = "disapproving"
					} else {
						newEventFace = "skeptical"
					}
				case "test_pass":
					justTestPass = true
					newEventFace = "satisfied"
				case "test_fail":
					justTestFail = true
					d.m.Stress = clampUnit(d.m.Stress + 0.3)
				}
			}
		}
	}

	now := time.Now()
	mood.Decay(&d.m, now.Sub(d.lastTick).Seconds())
	d.lastTick = now
	nowMs := now.UnixMilli()
	if d.sessionStart > 0 {
		d.m.Tiredness = mood.Tiredness(d.sessionStart, nowMs, now.Hour())
	}

	// real-time turn signal from the hook heartbeat (v1.5): a turn can be active
	// while the transcript is silent (long thinking) — don't call that idle.
	hb, _ := state.ReadHeartbeat(d.cfg.HeartbeatPath())
	hbFresh := hb.Event != "" && nowMs-hb.TS < 15*60*1000
	turnActive := hbFresh && isActiveEvent(hb.Event)
	if hb.Tool != "" && nowMs-hb.TS < 10000 {
		d.lastTool = hb.Tool // freshest tool, straight from the hook
	}
	idleMs := d.p.IdleSeconds * 1000 // fallback when no hooks
	if hbFresh {
		idleMs = 60 * 1000 // hooks present → idle responsive post-turn
	}

	act := "idle"
	switch {
	case d.cls.OpenAgents() > 0:
		act = "delegating"
	case d.cls.OpenTools() > 0:
		act = "tool_running"
	case turnActive:
		act = "thinking" // a turn is in progress — never sleep here
	case nowMs-d.lastEventTS > idleMs:
		act = "idle"
	default:
		act = "thinking"
	}

	if d.curRemark != nil && nowMs > d.curRemark.ExpiresMs {
		d.curRemark = nil
	}
	maxRepeat := 0
	for _, r := range d.fileRepeat {
		if r > maxRepeat {
			maxRepeat = r
		}
	}
	if d.curRemark == nil {
		if cat, text := d.eng.Eval(triggers.View{
			ToolMaxMs: d.toolMaxMs, ThinkMaxMs: d.thinkMaxMs,
			FilesCount: d.filesCount, MaxFileRepeat: maxRepeat,
			LocalHour: now.Hour(), JustDelegated: justDelegated,
				JustCommitted: justCommitted, JustReverted: justReverted,
				JustTestPass: justTestPass, JustTestFail: justTestFail,
		}); text != "" {
			d.curRemark = &state.Remark{Text: text, ExpiresMs: nowMs + d.p.RemarkHoldMs}
			state.AppendRemarked(d.cfg.RemarkedPath(), cat, text, nowMs)
			d.toolMaxMs, d.thinkMaxMs = 0, 0 // one-shot maxes don't re-trip
		}
	}

	// transient color tone: error flash > success flash > live "warning" (too long)
	if d.flashTone != "" && nowMs > d.flashExpires {
		d.flashTone = ""
	}
	if errFlash {
		d.flashTone, d.flashExpires = "error", nowMs+5000
	} else if (doneFlash || justCommitted || justTestPass) && d.flashTone != "error" {
		d.flashTone, d.flashExpires = "success", nowMs+3000
	}
	// transient dev-event face (skeptical/disapproving/satisfied)
	if newEventFace != "" {
		d.eventFace, d.eventFaceExp = newEventFace, nowMs+8000
	}
	if d.eventFace != "" && nowMs > d.eventFaceExp {
		d.eventFace = ""
	}
	tone := d.flashTone
	if tone == "" {
		if oldest := d.cls.OldestOpenToolStart(); oldest > 0 && nowMs-oldest > d.p.LongToolCallMs {
			tone = "warning"
		}
	}

	// how long we've held this activity (for sustained-state habitat gating)
	if act != d.prevActivity {
		d.prevActivity = act
		d.activitySince = nowMs
	}
	stateHeld := int64(0)
	if d.activitySince > 0 {
		stateHeld = nowMs - d.activitySince
	}
	openToolMs := int64(0)
	if om := d.cls.OldestOpenToolStart(); om > 0 {
		openToolMs = nowMs - om
	}

	state.WriteNow(d.cfg.NowPath(), state.Now{
		Mood: d.m, Activity: act, Tone: tone,
		StateHeldMs: stateHeld, LastTool: d.lastTool, OpenToolMs: openToolMs,
		EventFace: d.eventFace, Remark: d.curRemark,
	})
}

func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Run is the daemon entrypoint. Returns nil immediately if another instance holds
// the lock. The stop channel bounds the loop (used by tests).
func Run(cfg config.Config, stop <-chan struct{}) error {
	cfg.EnsureStateDir()
	if !acquireLock(cfg.LockPath()) {
		return nil
	}
	defer os.Remove(cfg.LockPath())
	d := New(cfg)
	for {
		d.Tick()
		select {
		case <-stop:
			return nil
		case <-time.After(time.Second):
		}
	}
}

// --- single-instance lock (processAlive is OS-specific, see spawn_*.go) ---

func acquireLock(path string) bool {
	if b, err := os.ReadFile(path); err == nil {
		if pid, e := strconv.Atoi(strings.TrimSpace(string(b))); e == nil && processAlive(pid) {
			return false
		}
	}
	_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
	return true
}

// isActiveEvent reports whether a hook event means a turn is in progress
// (vs. Stop/Notification/SessionEnd which mean the turn ended / awaiting user).
func isActiveEvent(e string) bool {
	switch e {
	case "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "SubagentStart":
		return true
	}
	return false
}

// EnsureRunning is called by the renderer: spawn a detached daemon if none is alive.
func EnsureRunning(cfg config.Config) {
	if b, err := os.ReadFile(cfg.LockPath()); err == nil {
		if pid, e := strconv.Atoi(strings.TrimSpace(string(b))); e == nil && processAlive(pid) {
			return
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = spawnDetached(exe, "daemon")
}
