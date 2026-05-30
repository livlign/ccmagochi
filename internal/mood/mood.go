// Package mood updates and decays the fixed 5-variable mood model.
// Deltas/rates are first-pass guesses — tune during the dogfood soak.
package mood

import (
	"math"

	"ccmagotchi/internal/events"
	"ccmagotchi/internal/persona"
	"ccmagotchi/internal/state"
)

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Apply nudges the mood for a single event.
func Apply(m *state.Mood, e events.Event, p persona.Persona) {
	switch e.Type {
	case "error":
		m.Stress = clamp(m.Stress + 0.25)
	case "thinking_turn":
		if events.Num(e.Data["duration_ms"]) > p.LongThinkingMs {
			m.Stress = clamp(m.Stress + 0.15)
		}
	case "tool_call":
		m.Energy = clamp(m.Energy - 0.02)
	case "idle":
		m.Energy = clamp(m.Energy + 0.05)
		m.Stress = clamp(m.Stress - 0.05)
	case "subagent_spawn":
		m.Curiosity = clamp(m.Curiosity + 0.05)
	}
}

// Curiosity is bumped by the daemon when a genuinely new file is opened.
func BumpCuriosity(m *state.Mood, by float64) { m.Curiosity = clamp(m.Curiosity + by) }

// Decay moves volatile variables toward rest over dt seconds (exponential, no cliffs).
func Decay(m *state.Mood, dt float64) {
	m.Stress *= math.Pow(0.5, dt/300)    // half-life 5 min
	m.Curiosity *= math.Pow(0.5, dt/120) // half-life 2 min
	// energy restores toward a 0.7 baseline
	m.Energy += (0.7 - m.Energy) * (1 - math.Pow(0.5, dt/600))
	m.Energy = clamp(m.Energy)
}

// Tiredness is a function of session length + local hour (not decayed; recomputed).
func Tiredness(sessionStartMs, nowMs int64, localHour int) float64 {
	hrs := float64(nowMs-sessionStartMs) / 3600000.0
	t := hrs / 6.0
	if localHour < 5 || localHour >= 23 {
		t += 0.3
	}
	return clamp(t)
}
