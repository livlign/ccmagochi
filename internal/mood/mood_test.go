package mood

import (
	"testing"

	"ccmagotchi/internal/events"
	"ccmagotchi/internal/persona"
	"ccmagotchi/internal/state"
)

func TestApply_ErrorRaisesStress(t *testing.T) {
	m := state.Mood{}
	Apply(&m, events.Event{Type: "error"}, persona.Default())
	if m.Stress <= 0 {
		t.Fatalf("stress should rise on error, got %v", m.Stress)
	}
}

func TestApply_LongThinkingRaisesStress_ShortDoesnt(t *testing.T) {
	p := persona.Default()
	long := state.Mood{}
	Apply(&long, events.Event{Type: "thinking_turn", Data: map[string]any{"duration_ms": p.LongThinkingMs + 1}}, p)
	short := state.Mood{}
	Apply(&short, events.Event{Type: "thinking_turn", Data: map[string]any{"duration_ms": int64(10)}}, p)
	if !(long.Stress > short.Stress) {
		t.Fatalf("long thinking should raise stress more: long=%v short=%v", long.Stress, short.Stress)
	}
}

func TestDecay_OneHalfLife(t *testing.T) {
	m := state.Mood{Stress: 1}
	Decay(&m, 300) // stress half-life = 300s
	if m.Stress < 0.45 || m.Stress > 0.55 {
		t.Fatalf("want ~0.5 after one half-life, got %v", m.Stress)
	}
}

func TestTiredness_LateNightHigher(t *testing.T) {
	day := Tiredness(0, 3600000, 14)
	night := Tiredness(0, 3600000, 2)
	if night <= day {
		t.Fatalf("late night should be tireder: day=%v night=%v", day, night)
	}
}
