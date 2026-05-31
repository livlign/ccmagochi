// Package daemon is the slow loop: tail the transcript, fold events into mood,
// evaluate triggers, and write now.json ~once a second. Single instance.
// It attaches at end-of-file (watches new activity), it does NOT replay history.
package daemon

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"path/filepath"

	"ccmagotchi/internal/config"
	"ccmagotchi/internal/events"
	"ccmagotchi/internal/lexicon"
	"ccmagotchi/internal/mood"
	"ccmagotchi/internal/persona"
	"ccmagotchi/internal/quirks"
	"ccmagotchi/internal/recent"
	"ccmagotchi/internal/state"
	"ccmagotchi/internal/traits"
	"ccmagotchi/internal/transcript"
	"ccmagotchi/internal/triggers"
	"ccmagotchi/internal/world"
)

// debug logging — gated behind CCMAGOTCHI_DEBUG=1, never ships to users.
var debugOn = os.Getenv("CCMAGOTCHI_DEBUG") == "1"

func dbgf(format string, a ...any) {
	if debugOn {
		fmt.Fprintf(os.Stderr, "[ccmagotchi] "+format+"\n", a...)
	}
}

// Daemon holds the streaming state. Tick() is the unit of work (one tick).
type Daemon struct {
	tickN          int // monotonic tick counter (debug)
	cfg            config.Config
	p              persona.Persona
	eng            *triggers.Engine
	cls            *transcript.Classifier
	tailer         *transcript.Tailer
	curPath        string
	m              state.Mood
	sessionStart   int64
	seenFiles      map[string]bool
	fileRepeat     map[string]int
	toolMaxMs      int64
	thinkMaxMs     int64
	filesCount     int
	lastEventTS    int64
	curRemark      *state.Remark
	flashTone      string // "error"|"success" transient color, with expiry
	flashExpires   int64
	lastTool       string // last tool name seen (for the edit/read accessory)
	prevActivity   string
	activitySince  int64  // ms when the current activity began (for habitat gating)
	eventFace      string // transient dev-event reaction (skeptical/disapproving/satisfied)
	eventFaceExp   int64
	lastCommitMs   int64 // for "revert soon after commit → skeptical"
	lastTestFailMs int64 // for "test pass after failures → amazed" (pet-01-dog §6)
	revertStreak   int   // consecutive reverts (no commit between) → disapproving
	quirks         quirks.Quirks
	remarkCount    int // for the speech-tic cadence
	lastTick       time.Time
	// Layer 2 / Layer 3 — cross-session memory and lifetime traits
	recent                 recent.Recent
	traits                 traits.Traits
	lexicon                lexicon.Lexicon // growing vocabulary (your words + tone)
	flavorTick             int             // cadence for the flavor sprinkle
	toolCalls              int             // tool calls this session (pace numerator)
	sessionStartWallMs     int64           // wall ms of the first event this session
	sessionWeekday         time.Weekday
	firstSessionToday      bool
	firstSessionFreshUntil int64        // first-session remark only fresh for a few min
	burn                   []burnSample // recent usage events (token burn rate)
	recentToolTS           []int64      // tool-call times this session (pace anomaly)
	curRepo                string       // repo (cwd basename) for the active session
	sessionLongGap         int          // days since last active before this session
	activeSinceMs          int64        // when the current active run began (break detection)
	greetUntil             int64        // paw-up greeting active until this wall ms
	lastGreetMs            int64        // greeting cooldown
	curBark                string       // active bark text
	barkExpiresMs          int64        // bark fades after this
	lastBarkMs             int64        // bark cooldown (≥3min between barks)
	// decorations (pet-decorations.md) — daemon owns selection + cooldowns + caps
	curDecor                                                                string
	decorExpiry                                                             int64
	curSound                                                                string
	soundExpiry, lastSoundMs                                                int64
	curTint                                                                 string
	tintExpiry, lastTintMs                                                  int64
	lastAlertMs, lastConfuseMs, lastSparkleMs, lastAffectionMs, lastAlarmMs int64
	sparkleCount, affectionCount, alarmCount                                int // per-session caps
	// world (pet-world.md) — the dog's column among anchored scenery + robots
	worldRng     *rand.Rand
	cols         int    // terminal COLUMNS (from session.json, set by the renderer)
	pos, target  int    // dog column + wander target within usable width
	heading      string // right|left|still
	curFile      string // most-recent active file (→ 📄 scenery)
	moveCooldown int    // ticks until the dog may step again
	ambientIdx   int    // rotates the ambient caption
	ambientUntil int64  // when to rotate next
	subs         map[string]*state.Subagent
	sceneryM     map[string]*world.Scenery // by kind (sun/tree/stone/gear/file) — stable positions
}

// burnSample is one assistant message's output-token count at a moment.
type burnSample struct {
	ts  int64
	tok int
}

func New(cfg config.Config) *Daemon {
	cfg.EnsureStateDir()
	p := persona.Load(cfg.PersonaPath())
	vocab := persona.LoadVocab(cfg.VocabPath())
	grammar := persona.LoadGrammar(cfg.GrammarPath())
	// Layer 2: load recent.json, or bootstrap it from events.log (the replayable
	// source of truth) the first time so cross-session history isn't lost.
	rec := recent.Load(cfg.RecentPath())
	if len(rec.Days) == 0 {
		if rebuilt := recent.Rebuild(cfg.EventsPath()); len(rebuilt.Days) > 0 {
			rec = rebuilt
		}
	}
	// Layer 3: same — rebuild traits from events.log on first run ("get smarter
	// retroactively"). cwd/repo isn't in the log, so repo affinity starts fresh.
	tr := traits.Load(cfg.TraitsPath())
	if tr.Samples == 0 && tr.SessionCount == 0 {
		if rebuilt := traits.Rebuild(cfg.EventsPath()); rebuilt.Samples > 0 {
			tr = rebuilt
		}
	}
	return &Daemon{
		cfg:         cfg,
		p:           p,
		eng:         triggers.NewEngine(p, vocab, grammar, state.RecentRemarks(cfg.RemarkedPath(), p.RecencyWindow), time.Now().UnixNano()),
		cls:         transcript.NewClassifier(),
		quirks:      quirks.Load(cfg.QuirksPath()),
		m:           state.Mood{Energy: 0.7},
		seenFiles:   map[string]bool{},
		fileRepeat:  map[string]int{},
		lastEventTS: time.Now().UnixMilli(),
		lastTick:    time.Now(),
		recent:      rec,
		traits:      tr,
		lexicon:     lexicon.Load(cfg.LexiconPath()),
		worldRng:    rand.New(rand.NewSource(time.Now().UnixNano())),
		heading:     "still",
		subs:        map[string]*state.Subagent{},
		sceneryM:    map[string]*world.Scenery{},
	}
}

// Tick does one observation cycle: read new transcript lines, update mood,
// decay, evaluate triggers, write now.json.
func (d *Daemon) Tick() {
	d.tickN++
	// (re)attach to the active session, watching from NOW (skip backlog).
	if s, err := state.ReadSession(d.cfg.SessionPath()); err == nil && s.TranscriptPath != "" && s.TranscriptPath != d.curPath {
		// close out the previous session into Layer 3 (length → pace baseline).
		if d.sessionStart > 0 {
			d.traits.EndSession(d.sessionWeekday, d.lastEventTS-d.sessionStart)
			_ = d.traits.Save(d.cfg.TraitsPath())
		}
		d.curPath = s.TranscriptPath
		d.tailer = transcript.NewTailerAtEnd(d.curPath)
		d.cls = transcript.NewClassifier()
		d.sessionStart = 0
		// reset session-scoped counters so pace/file stats are per-session.
		d.seenFiles = map[string]bool{}
		d.fileRepeat = map[string]int{}
		d.filesCount, d.toolCalls = 0, 0
		d.toolMaxMs, d.thinkMaxMs = 0, 0
		d.firstSessionToday = false
		d.recentToolTS = nil
		d.activeSinceMs = 0
		d.sparkleCount, d.affectionCount, d.alarmCount = 0, 0, 0 // decoration caps reset per session
		d.curRepo = ""
		if s.Cwd != "" {
			d.curRepo = filepath.Base(s.Cwd)
		}
	}

	justDelegated, errFlash, doneFlash := false, false, false
	justCommitted, justReverted, justTestPass, justTestFail := false, false, false, false
	favoriteFile, aversionHit := false, false
	newFile, justPrompt, kindWords := false, false, false
	newEventFace := ""
	barkCandidate := "" // a bark this tick's events would warrant (gated below)
	// Layer 2/3 transient signals discovered while folding this tick's events.
	var revisitFile, sameYesterdayFile, favMonthFile string
	var revisitHours int
	if d.tailer != nil {
		for _, line := range d.tailer.ReadNew() {
			for _, e := range d.cls.Classify(line) {
				// Harvest your vocabulary/tone from the prompt, then STRIP the raw
				// text so events.log never stores it (privacy).
				if e.Type == "prompt_submit" {
					if txt, ok := e.Data["text"].(string); ok && txt != "" {
						d.lexicon.Observe(txt)
						kindWords = kindWords || hasKindWords(txt) // "thanks/good dog" → wink
						delete(e.Data, "text")
					}
					justPrompt = true // returning/greeting → casual bark
					// paw-up greeting (min 30s between greetings)
					if e.TS-d.lastGreetMs > 30000 {
						d.greetUntil, d.lastGreetMs = e.TS+3000, e.TS
					}
				}
				events.Append(d.cfg.EventsPath(), e)
				d.lastEventTS = e.TS
				if d.sessionStart == 0 {
					d.sessionStart = e.TS
					d.sessionStartWallMs = e.TS
					d.sessionWeekday = time.UnixMilli(e.TS).Weekday()
					d.sessionLongGap = d.recent.DaysSinceActive(e.TS) // before MarkSession
					d.firstSessionToday = d.recent.MarkSession(e.TS)
					d.firstSessionFreshUntil = e.TS + 5*60*1000
					d.traits.SetFirstSeen(e.TS)
					d.traits.ObserveRepo(d.curRepo) // Layer 3 repo affinity
				}
				mood.Apply(&d.m, e, d.p)
				// Layer 3: lifetime hour/tool distributions.
				d.traits.ObserveEvent(e.Type, time.UnixMilli(e.TS).Hour())
				if e.Type == "usage" { // token burn (Layer 1)
					d.burn = append(d.burn, burnSample{e.TS, int(events.Num(e.Data["output_tokens"]))})
				}
				switch e.Type {
				case "error":
					errFlash = true
					barkCandidate = "bork!"
				case "tool_done":
					v := events.Num(e.Data["duration_ms"])
					if v > d.toolMaxMs {
						d.toolMaxMs = v
					}
					if v > d.p.LongToolCallMs {
						doneFlash = true // a long-awaited task finished → success
					}
					if v > 5*d.p.LongToolCallMs {
						newEventFace = "amazed" // a very long task finally finished — wonder
					}
				case "thinking_turn":
					if v := events.Num(e.Data["duration_ms"]); v > d.thinkMaxMs {
						d.thinkMaxMs = v
					}
				case "tool_call":
					d.toolCalls++
					d.recentToolTS = append(d.recentToolTS, e.TS) // pace anomaly
					if name, ok := e.Data["name"].(string); ok {
						d.lastTool = name
						d.traits.ObserveToolKind(name) // reads-vs-writes quirk
						if name == d.quirks.Aversion {
							aversionHit = true
							d.m.Stress = clampUnit(d.m.Stress + 0.1)
						}
					}
					if f, ok := e.Data["file"].(string); ok && f != "" {
						d.curFile = f // most-recent active file → 📄 world scenery
						// Layer 2: file recency BEFORE recent.Observe updates it.
						if prev := d.recent.FileLastSeen(f); prev > 0 {
							if gapH := int((e.TS - prev) / 3600000); gapH >= 4 {
								revisitFile, revisitHours = f, gapH
							}
							if d.recent.SeenYesterday(f, e.TS) {
								sameYesterdayFile = f
							}
						}
						// Layer 3: lifetime favorite-file detection.
						if fav, ok := d.traits.FavoriteFile(); ok && f == fav {
							favMonthFile = f
						}
						d.fileRepeat[f]++
						if !d.seenFiles[f] {
							d.seenFiles[f] = true
							d.filesCount++
							newFile = true          // a fresh file → sniff
							d.traits.ObserveFile(f) // lifetime touch count
							mood.BumpCuriosity(&d.m, 0.2)
							if strings.HasSuffix(f, d.quirks.FavoriteExt) {
								favoriteFile = true
							}
						}
					}
				case "subagent_spawn":
					d.lastTool = "Agent"
					justDelegated = true
					barkCandidate = "woof"
				case "commit":
					justCommitted = true
					d.lastCommitMs = e.TS
					d.revertStreak = 0
					d.traits.ObserveCommit() // revert-after-commit quirk
					d.m.Affection = clampUnit(d.m.Affection + 0.4)
					barkCandidate = "woof!"
				case "revert":
					justReverted = true
					d.revertStreak++
					// detected quirk: a revert within 10min of a commit
					if d.lastCommitMs > 0 && e.TS-d.lastCommitMs < 10*60*1000 {
						d.traits.ObserveQuickRevert()
					}
					if d.revertStreak >= 2 {
						newEventFace = "disapproving"
					} else {
						newEventFace = "skeptical"
					}
					barkCandidate = "grrr"
				case "test_pass":
					justTestPass = true
					if d.lastTestFailMs > 0 && e.TS-d.lastTestFailMs < 5*60*1000 {
						newEventFace = "amazed" // green after red — genuine wonder (§6 starry)
					} else {
						newEventFace = "satisfied"
					}
					barkCandidate = "woof! woof!"
				case "test_fail":
					justTestFail = true
					d.lastTestFailMs = e.TS
					d.m.Stress = clampUnit(d.m.Stress + 0.3)
					barkCandidate = "whine"
				}
				// Layer 2: fold into daily counts + file recency (after the gap
				// check above, which reads FileLastSeen before this updates it).
				d.recent.Observe(e)
			}
		}
		_ = d.recent.Save(d.cfg.RecentPath())
		_ = d.traits.Save(d.cfg.TraitsPath())
		_ = d.lexicon.Save(d.cfg.LexiconPath())
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

	// Break detection (Layer 3): when a ≥10min active run ends, record the hour —
	// over time this learns when you usually stop ("you usually break around now").
	if act != "idle" {
		if d.activeSinceMs == 0 {
			d.activeSinceMs = nowMs
		}
	} else if d.activeSinceMs > 0 {
		if nowMs-d.activeSinceMs > 10*60*1000 {
			d.traits.ObserveBreak(now.Hour())
		}
		d.activeSinceMs = 0
	}

	// Bark: chatty by default (pet-01-dog §9) — the dog comments on what's
	// happening. ~1 every 60-90s. Event barks (set above) take priority; below
	// them come casual barks and sustained sounds, then state restrictions.
	if d.curBark != "" && nowMs > d.barkExpiresMs {
		d.curBark = ""
	}
	heldMs := int64(0)
	if d.activitySince > 0 {
		heldMs = nowMs - d.activitySince
	}
	sleeping := act == "idle" && d.m.Tiredness > 0.7
	if barkCandidate == "" { // no event bark → maybe a casual one or a sustained sound
		switch {
		case justPrompt:
			barkCandidate = "arf" // returning / acknowledging you
		case newFile:
			barkCandidate = "sniff" // investigating a fresh file
		case sleeping:
			barkCandidate = "*sigh*" // the only sound a sleeping dog makes
		case act == "idle" && nowMs-d.lastEventTS > 5*60*1000:
			barkCandidate = "*sigh*" // long idle
		case act == "tool_running" && heldMs > 4*longToolDisplayMs:
			barkCandidate = "*pant*" // tired but focused (long stretch)
		case d.revertStreak >= 2:
			barkCandidate = "*hmph*" // unimpressed by the revert pattern
		case act == "tool_running" && d.worldRng.Intn(45) == 0:
			barkCandidate = []string{"ruff", "woof", "arf", "huff"}[d.worldRng.Intn(4)] // casual mid-work
		}
	}
	// state restrictions
	distressed := d.m.Stress > 0.85
	focusedLong := act == "tool_running" && heldMs > 120000
	casual := map[string]bool{"woof": true, "arf": true, "ruff": true, "huff": true, "sniff": true, "yip": true, "woof!": true}
	switch {
	case sleeping && barkCandidate != "*sigh*":
		barkCandidate = "" // a sleeping dog only sighs
	case distressed && barkCandidate != "":
		barkCandidate = "whine" // a distressed dog only whines
	case focusedLong && casual[barkCandidate]:
		barkCandidate = "" // don't interrupt deep focus with casual chatter
	}
	if barkCandidate != "" && nowMs-d.lastBarkMs > 60000 {
		d.curBark, d.barkExpiresMs, d.lastBarkMs = barkCandidate, nowMs+4000, nowMs
	}

	d.resolveDecor(nowMs, act, errFlash, doneFlash, justTestPass, justCommitted, justReverted)

	if d.curRemark != nil && nowMs > d.curRemark.ExpiresMs {
		d.curRemark = nil
	}
	maxRepeat := 0
	for _, r := range d.fileRepeat {
		if r > maxRepeat {
			maxRepeat = r
		}
	}
	todayFiles := d.recent.Today(nowMs).Files
	burn := d.tokenBurn(nowMs)
	favRepo := false
	if fav, ok := d.traits.FavoriteRepo(); ok && fav == d.curRepo {
		favRepo = true
	}
	annDays, annOK := d.traits.AnniversaryDays(nowMs)
	d.eng.SetTone(d.lexicon.Terse(), d.lexicon.Exclaim()) // match your style
	if d.curRemark == nil {
		if cat, text := d.eng.Eval(triggers.View{
			ToolMaxMs: d.toolMaxMs, ThinkMaxMs: d.thinkMaxMs,
			FilesCount: d.filesCount, MaxFileRepeat: maxRepeat,
			LocalHour: now.Hour(), JustDelegated: justDelegated,
			JustCommitted: justCommitted, JustReverted: justReverted,
			JustTestPass: justTestPass, JustTestFail: justTestFail,
			IsEditing:        act == "tool_running" && isEditTool(d.lastTool),
			IsReading:        act == "tool_running" && isReadTool(d.lastTool),
			WeekendPhase:     weekendPhase(now.Weekday()),
			JustFavoriteFile: favoriteFile,
			JustAversion:     aversionHit,
			JustError:        errFlash,
			HeavyBurn:        burn > 5000,
			PaceAnomaly:      d.paceAnomaly(nowMs),
			// Layer 2
			FirstSessionToday: d.firstSessionToday && nowMs < d.firstSessionFreshUntil,
			FilesToday:        todayFiles,
			StreakDays:        d.recent.ConsecutiveDays(nowMs),
			RevisitHours:      revisitHours,
			SameAsYesterday:   sameYesterdayFile != "",
			LongGapDays:       d.sessionLongGap,
			// Layer 3
			LateByStandards: d.traits.LateByStandards(now.Hour()),
			RareDay:         d.traits.RareDayOfWeek(now.Weekday()),
			FavoriteMonth:   favMonthFile != "",
			FavoriteRepo:    favRepo,
			PaceVerdict:     d.traits.PaceVerdict(d.toolCalls, nowMs-d.sessionStartWallMs),
			UsualBreak:      act != "idle" && d.traits.UsualBreak(now.Hour()),
			AnniversaryDays: annDaysIf(annOK, annDays),
			QuirkRevert:     d.traits.RevertsAfterCommit(),
			QuirkReads:      d.traits.ReadsMoreThanWrites(),
		}); text != "" {
			text = d.fill(cat, text, todayFiles, revisitFile, revisitHours, sameYesterdayFile, favMonthFile, d.sessionLongGap, annDays)
			// Hybrid voice: sprinkle one of YOUR favored interjections into ~1-in-4
			// remarks (grows as you use more flavor words). Takes the slot the
			// speech-tic would use, so they don't both pile on.
			sprinkled := false
			if flavors := d.lexicon.FlavorWords(5); len(flavors) > 0 {
				d.flavorTick++
				if d.flavorTick%4 == 0 {
					text = text + " " + flavors[d.flavorTick/4%len(flavors)]
					sprinkled = true
				}
			}
			d.remarkCount++ // speech tic ~1 in 3 remarks (its verbal fingerprint)
			if !sprinkled && d.quirks.SpeechTic != "" && d.remarkCount%3 == 0 {
				text += d.quirks.SpeechTic
			}
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
	if (favoriteFile || kindWords) && newEventFace == "" {
		newEventFace = "cheeky" // a favorite-kind file, or you said something kind → wink
	}
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

	scenery, ambient := d.worldTick(nowMs, act, now.Hour())

	state.WriteNow(d.cfg.NowPath(), state.Now{
		Mood: d.m, Activity: act, Tone: tone,
		StateHeldMs: stateHeld, LastTool: d.lastTool, OpenToolMs: openToolMs,
		EventFace: d.eventFace, Remark: d.curRemark,
		TokenBurn: burn,
		Favored:   d.quirks.PreferredHour == now.Hour(),
		Greeting:  nowMs < d.greetUntil,
		Bark:      d.curBark,
		Decor:     d.decorOut(act, nowMs),
		Sound:     d.curSound,
		Tint:      d.curTint,
		Pos:       d.pos,
		Heading:   d.heading,
		Scenery:   scenery,
		Ambient:   ambient,
	})
}

// tokenBurn returns output tokens/min over the last 2 minutes (Layer 1), pruning
// older samples in place.
func (d *Daemon) tokenBurn(nowMs int64) float64 {
	cut := nowMs - 120000
	keep := d.burn[:0]
	sum := 0
	for _, s := range d.burn {
		if s.ts >= cut {
			keep = append(keep, s)
			sum += s.tok
		}
	}
	d.burn = keep
	return float64(sum) / 2.0
}

// paceAnomaly reports a within-session surge: recent tool-call pace more than
// double the session-so-far average (needs ≥10min of session and enough calls).
func (d *Daemon) paceAnomaly(nowMs int64) bool {
	if d.sessionStartWallMs == 0 {
		return false
	}
	sessMin := float64(nowMs-d.sessionStartWallMs) / 60000
	if sessMin < 10 || d.toolCalls < 12 {
		return false
	}
	cut := nowMs - 120000
	keep := d.recentToolTS[:0]
	for _, ts := range d.recentToolTS {
		if ts >= cut {
			keep = append(keep, ts)
		}
	}
	d.recentToolTS = keep
	recentPace := float64(len(keep)) / 2.0      // calls/min, last 2 min
	earlyPace := float64(d.toolCalls) / sessMin // calls/min, session avg
	return len(keep) >= 6 && earlyPace > 0 && recentPace > 2*earlyPace
}

// annDaysIf returns days when it's a milestone, else 0 (View gate).
func annDaysIf(ok bool, days int) int {
	if ok {
		return days
	}
	return 0
}

// resolveDecor picks the transient mood symbol + sound + rare background tint
// (pet-decorations.md), enforcing per-category cooldowns and per-session caps.
// Silence is the default — most ticks nothing fires. Sustained symbols (zZ/…/♪)
// are computed in decorOut; this handles the punctuated, capped ones.
func (d *Daemon) resolveDecor(nowMs int64, act string, errFlash, doneFlash, testPass, committed, reverted bool) {
	if d.curDecor != "" && nowMs > d.decorExpiry {
		d.curDecor = ""
	}
	if d.curSound != "" && nowMs > d.soundExpiry {
		d.curSound = ""
	}
	if d.curTint != "" && nowMs > d.tintExpiry {
		d.curTint = ""
	}
	set := func(sym string, dur int64) { d.curDecor, d.decorExpiry = sym, nowMs+dur }
	_, anniversary := d.traits.AnniversaryDays(nowMs)
	greeting := nowMs < d.greetUntil

	// transient symbol — first eligible wins (recent/severe over routine)
	switch {
	case errFlash && d.m.Stress > 0.7 && nowMs-d.lastAlarmMs > 30000 && d.alarmCount < 3:
		set("‼", 4000)
		d.lastAlarmMs, d.alarmCount = nowMs, d.alarmCount+1
	case anniversary && nowMs-d.lastSparkleMs > 300000 && d.sparkleCount < 5:
		set("✨", 4000)
		d.lastSparkleMs, d.sparkleCount = nowMs, d.sparkleCount+1
		if nowMs-d.lastTintMs > 1800000 { // gold milestone tint, ≤1/30min
			d.curTint, d.tintExpiry, d.lastTintMs = "gold", nowMs+4000, nowMs
		}
	case (testPass || committed) && nowMs-d.lastSparkleMs > 300000 && d.sparkleCount < 5:
		set("✦", 4000)
		d.lastSparkleMs, d.sparkleCount = nowMs, d.sparkleCount+1
	case errFlash && nowMs-d.lastAlertMs > 30000:
		set("!", 4000)
		d.lastAlertMs = nowMs
	case greeting && d.m.Affection > 0.5 && nowMs-d.lastAffectionMs > 3600000 && d.affectionCount < 2:
		set("♥", 5000)
		d.lastAffectionMs, d.affectionCount = nowMs, d.affectionCount+1
	case reverted && nowMs-d.lastConfuseMs > 60000:
		set("?", 4000)
		d.lastConfuseMs = nowMs
	}

	// sound emission — own cooldown (≥3min), silence is default
	sound := ""
	switch {
	case doneFlash:
		sound = "*huff*" // panting after a long task
	case reverted:
		sound = "*humph*"
	case act == "idle" && d.m.Tiredness > 0.7:
		sound = "*snore*" // sleeping
	case act == "idle" && d.m.Stress < 0.3 && d.m.Energy > 0.4:
		sound = "*hmm*" // contented hum
	}
	if sound != "" && nowMs-d.lastSoundMs > 180000 {
		d.curSound, d.soundExpiry, d.lastSoundMs = sound, nowMs+3000, nowMs
	}
}

// decorOut returns the symbol to display: the active transient one, else a
// state-derived sustained symbol (sleeping zZ, thinking …, sweat, tear, etc.).
func (d *Daemon) decorOut(act string, nowMs int64) string {
	if d.curDecor != "" {
		return d.curDecor
	}
	openToolMs := int64(0)
	if om := d.cls.OldestOpenToolStart(); om > 0 {
		openToolMs = nowMs - om
	}
	switch {
	case act == "idle" && d.m.Tiredness > 0.9:
		return "ᶻᶻ" // deep sleep
	case act == "idle" && d.m.Tiredness > 0.7:
		return "zZ" // sleeping
	case act == "thinking":
		return "…"
	case d.m.Stress > 0.85:
		return "'" // tear — distressed
	case act == "tool_running" && openToolMs > 4*longToolDisplayMs:
		return ";" // sweat — long-running effort
	case act == "idle" && d.m.Stress < 0.3 && d.m.Energy > 0.4 && nowMs-d.sessionStartWallMs > 20*60*1000:
		return "♪" // humming during a long content session
	case act == "idle" && d.m.Energy < 0.3:
		return "···" // bored, drifting
	}
	return ""
}

const longToolDisplayMs = 30000 // ≈ persona LongToolCallMs (display threshold)

// worldTick advances the world (pet-world.md): refreshes terminal width from the
// renderer's session pointer, wanders the dog one step (mostly still), maintains
// stable-positioned scenery, rotates ambient text, and writes subagents.json.
// Returns the scenery + ambient caption for now.json.
func (d *Daemon) worldTick(nowMs int64, act string, hour int) ([]world.Scenery, string) {
	if s, err := state.ReadSession(d.cfg.SessionPath()); err == nil && s.Cols > 0 {
		d.cols = s.Cols // COLUMNS lives in the renderer; it hands it to us here
	}
	usable := world.Usable(d.cols)
	d.trackSubagents(nowMs, usable) // robots tracked even in compact (→ +N)
	if usable < 12 {
		d.pos, d.heading = 0, "still"
		d.sceneryM = map[string]*world.Scenery{}
		return nil, ""
	}

	const dogW = 9 // ≈ "≡(•ᴥ•)≡ ⌒" — enough margin to keep the dog on-screen
	maxPos := usable - dogW
	if maxPos < 0 {
		maxPos = 0
	}
	if d.pos > maxPos {
		d.pos = maxPos
	}
	dbgf("worldTick t=%d act=%q cols=%d usable=%d maxPos=%d pos=%d target=%d cd=%d", d.tickN, act, d.cols, usable, maxPos, d.pos, d.target, d.moveCooldown)

	// Wander (pet-world §3): the dog walks visibly to a target — one column per
	// tick while traveling — then rests when it arrives. Stillness lives between
	// excursions (a 4-11 tick pause), not between steps, so the dog reads as
	// walking over to scenery / the ⚙ rather than creeping a column every few
	// seconds. Movement spans idle, thinking and active periods alike.
	prevPos := d.pos
	if d.moveCooldown > 0 {
		d.moveCooldown--
	}
	if maxPos > 0 {
		if d.pos == d.target {
			// Arrived (or idle at rest): pause, then pick the next destination.
			if d.moveCooldown == 0 {
				d.target = d.pickTarget(maxPos, d.pos, act)
				if d.target == d.pos {
					d.moveCooldown = 4 + d.worldRng.Intn(8) // nowhere new — rest before reconsidering
				} else {
					dbgf("  target ASSIGN t=%d target=%d pos=%d", d.tickN, d.target, d.pos)
				}
			}
		} else {
			// Traveling: step one column toward the target every tick.
			oldPos := d.pos
			if d.target > d.pos {
				d.pos++
			} else {
				d.pos--
			}
			dbgf("  pos MOVE t=%d %d -> %d (target=%d)", d.tickN, oldPos, d.pos, d.target)
			if d.pos == d.target {
				d.moveCooldown = 4 + d.worldRng.Intn(8) // arrived — rest before the next excursion
			}
		}
	}
	// Heading reflects the step ACTUALLY taken this tick, not the target — so a dog
	// that isn't moving (resting, arrived, or boxed in) reads as "still" (front
	// face) rather than a frozen walking pose. The side-profile sprite only shows
	// while the dog is genuinely advancing.
	switch {
	case d.pos > prevPos:
		d.heading = "right"
	case d.pos < prevPos:
		d.heading = "left"
	default:
		d.heading = "still"
	}

	// Scenery — stable positions, spawned once and kept while their condition holds.
	moon := "🌙"
	if hour >= 6 && hour < 18 {
		moon = "🌞"
	}
	d.sceneryM["sun"] = &world.Scenery{Glyph: moon, Pos: usable - 3} // edge-anchored, re-set each tick
	d.setScenery("tree", "🌲", usable, d.sessionStartWallMs > 0 && nowMs-d.sessionStartWallMs > 20*60*1000)
	d.setScenery("stone", "🪨", usable, d.curRepo != "")
	d.setScenery("gear", "⚙", usable, act == "tool_running")
	d.setScenery("file", "📄", usable, act == "tool_running" && d.curFile != "")
	sc := make([]world.Scenery, 0, len(d.sceneryM))
	for _, s := range d.sceneryM {
		if s.Pos >= 0 && s.Pos < usable {
			sc = append(sc, *s)
		}
	}

	// Ambient caption (yard+), rotated every ~15s.
	ambient := ""
	if usable >= 24 {
		if nowMs > d.ambientUntil {
			d.ambientIdx++
			d.ambientUntil = nowMs + 15000
		}
		ambient = d.ambientText(hour)
	}
	return sc, ambient
}

// pickTarget chooses the dog's next destination (pet-world §3): a SHORT local
// hop — a few columns from where it stands — within a central band that keeps it
// off the walls. Short hops are the point: a target picked anywhere on the line
// makes the dog traverse the whole width at 1 col/sec, which reads as the world
// scrolling past and ends with the dog parked at an edge. Local drift reads as
// the dog pottering/sniffing around one area. While a tool runs, the hop is
// biased toward the ⚙ so the dog drifts over to watch the work.
func (d *Daemon) pickTarget(maxPos, pos int, act string) int {
	margin := maxPos / 6
	if margin < 2 {
		margin = 2
	}
	lo, hi := margin, maxPos-margin
	if lo >= hi {
		return maxPos / 2 // world too narrow for a band — just sit centre
	}
	dir := 1
	if d.worldRng.Intn(2) == 0 {
		dir = -1
	}
	if act == "tool_running" { // drift toward the tool the dog is watching
		if g, ok := d.sceneryM["gear"]; ok && g.Pos != pos {
			if g.Pos < pos {
				dir = -1
			} else {
				dir = 1
			}
		}
	}
	t := pos + dir*(3+d.worldRng.Intn(8)) // a 3-10 column hop
	if t < lo {
		t = lo
	}
	if t > hi {
		t = hi
	}
	return t
}

func clampTo(pos, maxPos int) int {
	if pos < 0 {
		return 0
	}
	if pos > maxPos {
		return maxPos
	}
	return pos
}

// setScenery spawns a kind once (at a free column) when wanted, removes it when not.
func (d *Daemon) setScenery(kind, glyph string, usable int, want bool) {
	if !want {
		if _, ok := d.sceneryM[kind]; ok {
			dbgf("  scenery DESPAWN t=%d kind=%s", d.tickN, kind)
		}
		delete(d.sceneryM, kind)
		return
	}
	if _, ok := d.sceneryM[kind]; !ok {
		pos := d.freeCol(usable)
		d.sceneryM[kind] = &world.Scenery{Glyph: glyph, Pos: pos}
		dbgf("  scenery SPAWN t=%d kind=%s glyph=%s pos=%d dogPos=%d", d.tickN, kind, glyph, pos, d.pos)
	} else {
		d.sceneryM[kind].Glyph = glyph
	}
}

// freeCol picks a column biased away from the dog and other scenery.
func (d *Daemon) freeCol(usable int) int {
	for try := 0; try < 8; try++ {
		c := d.worldRng.Intn(usable)
		if abs(c-d.pos) < 4 {
			continue
		}
		clash := false
		for _, s := range d.sceneryM {
			if abs(c-s.Pos) < 2 {
				clash = true
				break
			}
		}
		if !clash {
			return c
		}
	}
	return d.worldRng.Intn(usable)
}

// ambientText returns the current rotating caption (factual, lowercase).
func (d *Daemon) ambientText(hour int) string {
	var opts []string
	if d.filesCount > 0 {
		opts = append(opts, fmt.Sprintf("%d files this session", d.filesCount))
	}
	if streak := d.recent.ConsecutiveDays(d.lastEventTS); streak >= 2 {
		opts = append(opts, fmt.Sprintf("%d-day streak", streak))
	}
	if hour < 5 || hour >= 23 {
		opts = append(opts, "late one")
	}
	if d.curRepo != "" {
		opts = append(opts, "in "+d.curRepo)
	}
	if len(opts) == 0 {
		return ""
	}
	return opts[d.ambientIdx%len(opts)]
}

// trackSubagents maintains the robot companions from open Agent calls, writing
// subagents.json. New ones spawn to the right of the dog; finished ones show a
// fading face for a few seconds, then leave.
func (d *Daemon) trackSubagents(nowMs int64, usable int) {
	open := map[string]bool{}
	for _, id := range d.cls.OpenAgentIDs() {
		open[id] = true
	}
	n := 0
	for id := range open {
		if _, ok := d.subs[id]; !ok {
			d.subs[id] = &state.Subagent{
				ID: id, Status: "running", Variant: variantOf(id),
				Pos: d.pos + 9 + n*4, SinceMs: nowMs,
			}
		}
		n++
	}
	for id, s := range d.subs {
		if !open[id] {
			if s.Status != "done" {
				s.Status, s.SinceMs = "done", nowMs
			} else if nowMs-s.SinceMs > 3000 {
				delete(d.subs, id)
			}
		}
	}
	subs := make([]state.Subagent, 0, len(d.subs))
	for _, s := range d.subs {
		if usable > 0 && s.Pos >= usable {
			s.Pos = usable - 1
		}
		subs = append(subs, *s)
	}
	_ = state.WriteSubagents(d.cfg.SubagentsPath(), subs)
}

func variantOf(id string) int {
	sum := 0
	for i := 0; i < len(id); i++ {
		sum += int(id[i])
	}
	return sum % 3
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// hasKindWords reports whether a prompt contains praise/thanks aimed at the pet
// (or the work) — triggers a playful wink (pet-01-dog §6).
func hasKindWords(text string) bool {
	t := strings.ToLower(text)
	for _, w := range []string{"thank", "good dog", "good boy", "good girl", "well done", "nice work", "good job", "love you", "you're the best", "cute"} {
		if strings.Contains(t, w) {
			return true
		}
	}
	return false
}

func isEditTool(name string) bool {
	switch name {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return true
	}
	return false
}

func isReadTool(name string) bool {
	switch name {
	case "Read", "Grep", "Glob", "LS", "NotebookRead":
		return true
	}
	return false
}

// weekendPhase distinguishes the run-up to the weekend (Friday) from the
// weekend itself (Sat/Sun) so the pet never says "it's Friday" on a Sunday.
func weekendPhase(d time.Weekday) string {
	switch d {
	case time.Friday:
		return "eve"
	case time.Saturday, time.Sunday:
		return "now"
	}
	return ""
}

// fill substitutes the %-verbs in a chosen remark for the given category. Only
// categories with verbs are handled; others pass through unchanged.
func (d *Daemon) fill(cat, text string, todayFiles int, revisitFile string, revisitHours int, sameYesterdayFile, favMonthFile string, longGapDays, annDays int) string {
	switch cat {
	case "favorite_file":
		return fmt.Sprintf(text, d.quirks.FavoriteExt)
	case "aversion":
		return fmt.Sprintf(text, d.quirks.Aversion)
	case "editing", "reading":
		return fmt.Sprintf(text, d.filesCount)
	case "busy_today":
		return fmt.Sprintf(text, todayFiles)
	case "streak":
		return fmt.Sprintf(text, d.recent.ConsecutiveDays(d.lastEventTS))
	case "file_revisit":
		return fmt.Sprintf(text, filepath.Base(revisitFile), revisitHours)
	case "same_as_yesterday":
		return fmt.Sprintf(text, filepath.Base(sameYesterdayFile))
	case "favorite_file_month":
		return fmt.Sprintf(text, filepath.Base(favMonthFile))
	case "long_gap":
		return fmt.Sprintf(text, longGapDays)
	case "anniversary":
		return fmt.Sprintf(text, annDays)
	case "favorite_repo":
		return fmt.Sprintf(text, d.curRepo)
	}
	return text
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
	// Only clean up the lock if we still own it (a newer instance may have taken
	// over — don't delete its lock).
	defer func() {
		if ownsLock(cfg.LockPath()) {
			os.Remove(cfg.LockPath())
		}
	}()
	d := New(cfg)
	for {
		d.Tick()
		// If another instance has taken the lock (e.g. the lock file was removed
		// and the renderer respawned a daemon), stand down instead of racing it to
		// write now.json. This stops orphaned daemons from a rename / manual lock
		// removal — the bug that pinned the dog at column 0.
		if !ownsLock(cfg.LockPath()) {
			dbgf("lock lost (owner is no longer pid %d) — exiting", os.Getpid())
			return nil
		}
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

// ownsLock reports whether the lock file's PID is this process.
func ownsLock(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return err == nil && pid == os.Getpid()
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
