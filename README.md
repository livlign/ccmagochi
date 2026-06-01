# ccmagotchi

A terminal pet — a little dog — that lives in Claude Code's status line, reacts to how your
session is going, and speaks now and then. Pure Go, no LLM, local-only.

```
(• ᴥ •) ~                         idle, tail at rest
(◉ ᴥ ◉) woof                      heads-down on a tool, a bark
(• ᴥ •) 4 files today — nice pace.  speaking up
(◔ ᴥ ◔) … [◉_◉] ·1                thinking while one agent works
```

> 🚧 **In active development — V1 coming soon.**
> This is a work in progress: the dog and the way it behaves are still being
> shaped and tuned. Expect rough edges and changing behavior. Not yet released.

## Try it (dev build)

Requires Go 1.22+ and Claude Code ≥ 2.1.97.

```bash
go build -o bin/ccmagotchi ./cmd/ccmagotchi
go test ./...
```

Add it as your status line in `~/.claude/settings.json`:

```jsonc
"statusLine": { "type": "command", "command": "/abs/path/bin/ccmagotchi render", "refreshInterval": 1 }
```

To keep your existing status line, set it as the base command in `~/.ccmagotchi/config.json`:

```json
{ "base_status_command": "<your previous statusLine command>" }
```

The pet renders one line above (or below) your existing line(s).

## What's inside

A fast renderer (`render`, <50ms, paints the pet line) and a background watcher (`daemon`, tails the
session transcript → mood → behavior → state files). The dog has moods, ears, a tail, idle
micro-behaviors (blink, sniff, yawn, head-tilt), chatty barks, and it speaks up now and then with
useful context — files touched, day streaks, the repo, a late night. It always faces front at the
start of the line (no positioning, so it shows identically in every session), and agent companions
appear beside it while Claude delegates. Three memory layers (session / rolling / lifetime) and a
growing vocabulary sit underneath, all gated on statistical readiness so the pet stays quiet until it
has enough to say.

Everything is rule-based and local — no model in the loop, nothing leaves your machine.
