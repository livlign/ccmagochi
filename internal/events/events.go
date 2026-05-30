// Package events defines the append-only event stream — the replayable source
// of truth. Everything else (now.json, later Layers 2/3) is derived from it.
package events

import (
	"bufio"
	"encoding/json"
	"os"
)

// Event is one classified observation. Types: session_start, prompt_submit,
// thinking_turn, tool_call, tool_done, error, subagent_spawn, subagent_done, idle.
type Event struct {
	TS   int64          `json:"ts"` // unix millis
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

// Append writes events as JSON lines (O_APPEND, never rewritten).
func Append(path string, evs ...Event) error {
	if len(evs) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, e := range evs {
		b, _ := json.Marshal(e)
		w.Write(b)
		w.WriteByte('\n')
	}
	return w.Flush()
}

// Num coerces a Data value (int64/float64/int) to int64. JSON round-trips
// numbers as float64, so readers must tolerate both.
func Num(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	}
	return 0
}
