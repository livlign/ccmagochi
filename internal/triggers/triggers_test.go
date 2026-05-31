package triggers

import (
	"strings"
	"testing"

	"ccmagotchi/internal/persona"
)

func TestEval_FiresLongToolCall(t *testing.T) {
	e := NewEngine(persona.Default(), persona.DefaultVocab(), nil, nil, 1)
	cat, text := e.Eval(View{ToolMaxMs: 99999})
	if cat != "long_tool_call" || text == "" {
		t.Fatalf("want long_tool_call remark, got %q / %q", cat, text)
	}
}

func TestEval_SilentWhenNothingFires(t *testing.T) {
	e := NewEngine(persona.Default(), persona.DefaultVocab(), nil, nil, 1)
	if cat, text := e.Eval(View{LocalHour: 14}); text != "" {
		t.Fatalf("want silence, got %q / %q", cat, text)
	}
}

func TestEval_RespectsCap(t *testing.T) {
	p := persona.Default()
	p.RemarkCap = 1
	p.CooldownTurns = 0
	e := NewEngine(p, persona.DefaultVocab(), nil, nil, 1)
	if _, text := e.Eval(View{ToolMaxMs: 99999}); text == "" {
		t.Fatal("first remark should fire")
	}
	if _, text := e.Eval(View{LocalHour: 3}); text != "" {
		t.Fatalf("cap=1 should block the second remark, got %q", text)
	}
}

// Ambient remarks (weekend/late_hour) use the long AmbientCooldownTurns: once
// said, they stay quiet far longer than activity remarks.
func TestEval_AmbientCooldownIsLong(t *testing.T) {
	p := persona.Default()
	p.AmbientCooldownTurns = 100
	e := NewEngine(p, persona.DefaultVocab(), nil, nil, 1)
	if _, text := e.Eval(View{WeekendPhase: "now", LocalHour: 14}); text == "" {
		t.Fatal("first weekend remark should fire")
	}
	if _, text := e.Eval(View{WeekendPhase: "now", LocalHour: 14}); text != "" {
		t.Fatalf("weekend should be silenced by the ambient cooldown, got %q", text)
	}
}

// Activity remarks keep the short CooldownTurns and are unaffected by the
// (long) ambient cooldown — they can fire again right away.
func TestEval_ActivityCooldownStaysShort(t *testing.T) {
	p := persona.Default()
	p.CooldownTurns = 0
	p.AmbientCooldownTurns = 100000
	e := NewEngine(p, persona.DefaultVocab(), nil, nil, 1)
	if _, text := e.Eval(View{ToolMaxMs: 99999}); text == "" {
		t.Fatal("first long_tool_call should fire")
	}
	if _, text := e.Eval(View{ToolMaxMs: 99999}); text == "" {
		t.Fatal("long_tool_call should fire again — activity cooldown is short, not ambient")
	}
}

func TestEval_NoRepeatWithinRecency(t *testing.T) {
	p := persona.Default()
	p.CooldownTurns = 0
	v := persona.Vocab{"late_hour": {"late."}}
	e := NewEngine(p, v, nil, nil, 1)
	if _, text := e.Eval(View{LocalHour: 3}); text != "late." {
		t.Fatalf("first should say 'late.', got %q", text)
	}
	if _, text := e.Eval(View{LocalHour: 3}); text != "" {
		t.Fatalf("same phrasing should be suppressed by recency, got %q", text)
	}
}

// #3 fix: weekend is day-aware. Sunday ("now") must never produce "Friday".
func TestEval_WeekendIsDayAware(t *testing.T) {
	e := NewEngine(persona.Default(), persona.DefaultVocab(), persona.DefaultGrammar(), nil, 1)
	cat, text := e.Eval(View{WeekendPhase: "now", LocalHour: 14})
	if cat != "weekend_now" {
		t.Fatalf("Sat/Sun should fire weekend_now, got %q", cat)
	}
	if strings.Contains(strings.ToLower(text), "friday") {
		t.Errorf("a Sunday remark must not mention Friday: %q", text)
	}
}

// #2: improvisation — composing from the grammar yields many distinct lines
// from few fragments (not a fixed iteration of canned sentences).
func TestEval_ImprovisesVariety(t *testing.T) {
	p := persona.Default()
	p.CooldownTurns = 0
	p.AmbientCooldownTurns = 0 // late_hour is ambient; disable cooldown for the variety check
	p.RecencyWindow = 0        // don't let recency hide the variety
	p.RemarkCap = 1000
	e := NewEngine(p, persona.DefaultVocab(), persona.DefaultGrammar(), nil, 7)
	seen := map[string]bool{}
	for i := 0; i < 60; i++ {
		if _, text := e.Eval(View{LocalHour: 2}); text != "" {
			seen[text] = true
		}
	}
	if len(seen) < 6 {
		t.Errorf("grammar should improvise many late_hour variants, only saw %d: %v", len(seen), seen)
	}
}

// Hybrid voice: learned tone biases composition. Terse trims the tail flourish;
// exclaim prefers an emphatic ending.
func TestEngine_ToneMatch(t *testing.T) {
	p := persona.Default()
	p.CooldownTurns, p.AmbientCooldownTurns, p.RecencyWindow = 0, 0, 0
	g := persona.Grammar{"long_tool_call": {{"Still going"}, {"", " — on and on and on?", "!"}}}

	terse := NewEngine(p, persona.DefaultVocab(), g, nil, 1)
	terse.SetTone(true, false) // terse → shortest tail ("")
	if _, text := terse.Eval(View{ToolMaxMs: 99999}); text != "Still going" {
		t.Errorf("terse should drop the flourish, got %q", text)
	}

	loud := NewEngine(p, persona.DefaultVocab(), g, nil, 1)
	loud.SetTone(false, true) // exclaim → prefer the "!" tail
	if _, text := loud.Eval(View{ToolMaxMs: 99999}); !strings.HasSuffix(text, "!") {
		t.Errorf("exclaim should prefer an emphatic ending, got %q", text)
	}
}

// A Layer-3 judgment fires its own category and stays within the ambient bucket.
func TestEval_FiresTraitRemark(t *testing.T) {
	e := NewEngine(persona.Default(), persona.DefaultVocab(), persona.DefaultGrammar(), nil, 1)
	cat, text := e.Eval(View{LateByStandards: true, LocalHour: 14})
	if cat != "late_by_standards" || text == "" {
		t.Fatalf("want late_by_standards remark, got %q / %q", cat, text)
	}
}

// A tool error produces a spoken remark (not just the alarmed face/tone), and it
// outranks slow context.
func TestEval_ToolErrorSpoken(t *testing.T) {
	e := NewEngine(persona.Default(), persona.DefaultVocab(), persona.DefaultGrammar(), nil, 1)
	cat, text := e.Eval(View{JustError: true, WeekendPhase: "now", LocalHour: 14})
	if cat != "tool_error" || text == "" {
		t.Fatalf("want tool_error remark, got %q / %q", cat, text)
	}
}
