# ccmagotchi

A terminal pet that lives in Claude Code's status line, reacts to how your session is going, and speaks rarely. Pure Go, no LLM, local-only. Requires Claude Code ≥ 2.1.97 and Go 1.22+.

## Build / test / run
```bash
go build -o bin/ccmagotchi ./cmd/ccmagotchi   # build
go test ./...                                 # all tests
go test ./... -cover                          # with coverage
./bin/ccmagotchi render                        # the statusLine command (reads CC JSON on stdin)
./bin/ccmagotchi daemon                        # the background watcher (auto-spawned by render)
```

## Use it (install as your status line)
In `~/.claude/settings.json`:
```jsonc
"statusLine": { "type": "command", "command": "/abs/path/bin/ccmagotchi render", "refreshInterval": 1 }
```
Keep your existing dashboard by setting it as the base command in `~/.ccmagotchi/config.json`:
```json
{ "base_status_command": "<your previous statusLine command>" }
```
The pet appends one line below your existing line(s).

## Layout
```
cmd/ccmagotchi/      main + ANSI setup (build-tagged unix/windows)
internal/
  render/    fast <50ms path: stdin → base command + pet line
  daemon/    slow loop: tail transcript → events → mood → triggers → now.json (+ spawn, lock)
  transcript/ locate/tail/classify the session JSONL (Phase-0 probe-confirmed mapping)
  events/    append-only events.log (replayable source of truth)
  mood/      fixed 5-var mood model + decay
  face/      mood/activity → kaomoji + color (data-driven registry)
  triggers/  when to speak: 5 triggers + rate limiting
  state/     now.json (atomic), session pointer, remarked log
  config/    JSON config (zero external deps)
  persona/   tunable thresholds + remark phrasings (defaults baked in)
```

## Tests
30 unit/integration tests. Pure logic (mood, triggers, face, transcript classifier) is unit-tested;
the daemon loop is integration-tested via a fixture transcript + a bounded run. State/events/persona
round-trips covered. See `*_test.go` co-located in each package.

## Status
v1 (MVP). Schema verified against a real CC session — see [`PROBE.md`](./PROBE.md).
Windows spawn + VT/UTF-8 are build-tagged and **not yet verified on real Windows**.
