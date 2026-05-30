package triggers

import (
	"testing"

	"ccmagotchi/internal/persona"
)

func TestEval_FiresLongToolCall(t *testing.T) {
	e := NewEngine(persona.Default(), persona.DefaultVocab(), nil, 1)
	cat, text := e.Eval(View{ToolMaxMs: 99999})
	if cat != "long_tool_call" || text == "" {
		t.Fatalf("want long_tool_call remark, got %q / %q", cat, text)
	}
}

func TestEval_SilentWhenNothingFires(t *testing.T) {
	e := NewEngine(persona.Default(), persona.DefaultVocab(), nil, 1)
	if cat, text := e.Eval(View{LocalHour: 14}); text != "" {
		t.Fatalf("want silence, got %q / %q", cat, text)
	}
}

func TestEval_RespectsCap(t *testing.T) {
	p := persona.Default()
	p.RemarkCap = 1
	p.CooldownTurns = 0
	e := NewEngine(p, persona.DefaultVocab(), nil, 1)
	if _, text := e.Eval(View{ToolMaxMs: 99999}); text == "" {
		t.Fatal("first remark should fire")
	}
	if _, text := e.Eval(View{LocalHour: 3}); text != "" {
		t.Fatalf("cap=1 should block the second remark, got %q", text)
	}
}

func TestEval_NoRepeatWithinRecency(t *testing.T) {
	p := persona.Default()
	p.CooldownTurns = 0
	// vocab with a single phrasing → second fire must be suppressed by recency
	v := persona.Vocab{"late_hour": {"late."}}
	e := NewEngine(p, v, nil, 1)
	if _, text := e.Eval(View{LocalHour: 3}); text != "late." {
		t.Fatalf("first should say 'late.', got %q", text)
	}
	if _, text := e.Eval(View{LocalHour: 3}); text != "" {
		t.Fatalf("same phrasing should be suppressed by recency, got %q", text)
	}
}
