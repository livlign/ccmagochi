// ccmagotchi — a terminal pet that lives in Claude Code's status line.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"ccmagotchi/internal/config"
	"ccmagotchi/internal/daemon"
	"ccmagotchi/internal/render"
	"ccmagotchi/internal/state"
)

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "render":
		enableANSI() // no-op on unix; enables VT + UTF-8 on Windows
		stdin, _ := io.ReadAll(os.Stdin)
		cfg := config.Load()
		prepare(stdin, cfg)
		fmt.Print(render.Run(stdin, cfg))
	case "daemon":
		if err := daemon.Run(config.Load(), nil); err != nil {
			fmt.Fprintln(os.Stderr, "daemon:", err)
			os.Exit(1)
		}
	case "hook":
		// Called by Claude Code hooks: `ccmagotchi hook <EventName>`. Writes a
		// real-time heartbeat. Must be fast and never fail (runs on every event).
		ev := ""
		if len(os.Args) > 2 {
			ev = os.Args[2]
		}
		stdin, _ := io.ReadAll(os.Stdin)
		var in struct {
			ToolName string `json:"tool_name"`
		}
		_ = json.Unmarshal(stdin, &in)
		cfg := config.Load()
		cfg.EnsureStateDir()
		_ = state.WriteHeartbeat(cfg.HeartbeatPath(), state.Heartbeat{
			TS: time.Now().UnixMilli(), Event: ev, Tool: in.ToolName,
		})
	case "probe":
		fmt.Fprintln(os.Stderr, "ccmagotchi probe: see PROBE.md (Phase 0 done)")
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "usage: ccmagotchi render|daemon|probe")
		os.Exit(2)
	}
}

// prepare records the active session for the daemon and makes sure a daemon is
// running. Best-effort: any failure here must not stop the render.
func prepare(stdin []byte, cfg config.Config) {
	cfg.EnsureStateDir()
	var in struct {
		SessionID      string `json:"session_id"`
		TranscriptPath string `json:"transcript_path"`
	}
	_ = json.Unmarshal(stdin, &in)
	if in.TranscriptPath != "" {
		_ = state.WriteSession(cfg.SessionPath(), state.Session{TranscriptPath: in.TranscriptPath, SessionID: in.SessionID})
	}
	daemon.EnsureRunning(cfg)
}
