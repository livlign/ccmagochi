package persona

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_DefaultsWhenMissing(t *testing.T) {
	p := Load(filepath.Join(t.TempDir(), "none.json"))
	if p.CooldownTurns != Default().CooldownTurns {
		t.Fatalf("missing file should yield defaults, got cooldown=%d", p.CooldownTurns)
	}
}

func TestLoad_OverrideKeepsOtherDefaults(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "persona.json")
	os.WriteFile(fp, []byte(`{"cooldown_turns":2}`), 0o644)
	p := Load(fp)
	if p.CooldownTurns != 2 {
		t.Fatalf("override failed: %d", p.CooldownTurns)
	}
	if p.ManyFiles != Default().ManyFiles {
		t.Fatalf("unspecified key should keep default, got %d", p.ManyFiles)
	}
}

func TestLoadVocab_MergesOverDefaults(t *testing.T) {
	fp := filepath.Join(t.TempDir(), "vocab.json")
	os.WriteFile(fp, []byte(`{"late_hour":["zzz"]}`), 0o644)
	v := LoadVocab(fp)
	if len(v["late_hour"]) != 1 || v["late_hour"][0] != "zzz" {
		t.Errorf("override not applied: %v", v["late_hour"])
	}
	if len(v["long_tool_call"]) == 0 {
		t.Error("default categories should remain after merge")
	}
}
