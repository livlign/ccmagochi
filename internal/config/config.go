// Package config loads ccmagotchi's JSON config (zero external deps).
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is read from ~/.ccmagotchi/config.json. All fields optional.
type Config struct {
	// BaseStatusCommand is the user's existing statusLine command. If set, the
	// renderer runs it (piping the same stdin) and prints its output above the
	// pet line — so the pet coexists with the user's 0/1/N existing lines.
	BaseStatusCommand string `json:"base_status_command"`
	// StateDir holds now.json, events.log, etc. Defaults to ~/.ccmagotchi/state.
	StateDir string `json:"state_dir"`
	// PetLine controls where the pet renders relative to the user's existing
	// status line(s): "first" (default — pet on top) or "last" (pet below).
	PetLine string `json:"pet_line"`
}

// Load reads config.json, applying defaults for anything missing. Never fails:
// a missing/broken config yields a usable default so the renderer always runs.
func Load() Config {
	c := Config{}
	home, _ := os.UserHomeDir()
	if b, err := os.ReadFile(filepath.Join(home, ".ccmagotchi", "config.json")); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.StateDir == "" {
		c.StateDir = filepath.Join(home, ".ccmagotchi", "state")
	}
	if c.PetLine == "" {
		c.PetLine = "first"
	}
	return c
}

// NowPath is the location of the Layer-1 state file the renderer reads.
func (c Config) NowPath() string { return filepath.Join(c.StateDir, "now.json") }

// dir is the config root (parent of StateDir), e.g. ~/.ccmagotchi.
func (c Config) dir() string { return filepath.Dir(c.StateDir) }

func (c Config) EventsPath() string    { return filepath.Join(c.StateDir, "events.log") }
func (c Config) SessionPath() string   { return filepath.Join(c.StateDir, "session.json") }
func (c Config) RemarkedPath() string  { return filepath.Join(c.StateDir, "remarked.log") }
func (c Config) LockPath() string      { return filepath.Join(c.StateDir, "daemon.lock") }
func (c Config) HeartbeatPath() string { return filepath.Join(c.StateDir, "heartbeat.json") }
func (c Config) FramePath() string     { return filepath.Join(c.StateDir, "frame.json") }
func (c Config) QuirksPath() string    { return filepath.Join(c.dir(), "quirks.json") }
func (c Config) PersonaPath() string   { return filepath.Join(c.dir(), "persona.json") }
func (c Config) VocabPath() string     { return filepath.Join(c.dir(), "vocab.json") }
func (c Config) GrammarPath() string   { return filepath.Join(c.dir(), "grammar.json") }
func (c Config) RecentPath() string    { return filepath.Join(c.StateDir, "recent.json") }
func (c Config) TraitsPath() string    { return filepath.Join(c.StateDir, "traits.json") }
func (c Config) LexiconPath() string   { return filepath.Join(c.StateDir, "lexicon.json") }
func (c Config) SubagentsPath() string { return filepath.Join(c.StateDir, "subagents.json") }

// EnsureStateDir creates the state dir if missing.
func (c Config) EnsureStateDir() error { return os.MkdirAll(c.StateDir, 0o755) }
