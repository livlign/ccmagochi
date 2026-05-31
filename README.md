# ccmagotchi

A terminal pet — a little dog — that lives in Claude Code's status line, reacts to how your
session is going, walks around, and speaks now and then. Pure Go, no LLM, local-only.

```
ϟ≡≡≡°⊃ …            🌲              4 files this session
```

> 🚧 **In active development — V1 coming soon.**
> This is a work in progress: the dog, its world, and the way it behaves are still being
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
session transcript → mood → behavior → state files). The dog has moods, ears, a tail, idle behaviors,
chatty barks, and a side-profile walk; it lives in a width-aware world with scenery, ambient captions,
and subagent companions. Three memory layers (session / rolling / lifetime) and a growing vocabulary sit
underneath, all gated on statistical readiness so the pet stays quiet until it has enough to say.

Everything is rule-based and local — no model in the loop, nothing leaves your machine.
