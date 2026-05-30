# Phase 0 — Probe findings (verified against a real Claude Code session)

Source: live transcript `~/.claude/projects/-Users-linh-Documents-Claude/<session-id>.jsonl` (1076 entries, this session). Throwaway probe; findings lock the daemon's event-mapping.

## Transcript: confirmed facts
- **Location:** `~/.claude/projects/<cwd with '/'→'-'>/<session-id>.jsonl`. One JSON object per line, append-only.
- **`timestamp`** (ISO-8601) is on entries → **durations = deltas between entries.** ✅ (matches the spec assumption)
- **`durationMs` is NOT reliable** — present on only 25/1076 entries, mostly `system`. Do **not** depend on it; compute from timestamps.
- **Entry `type`s:** `assistant`, `user`, `system` + meta types to **ignore**: `mode`, `permission-mode`, `ai-title`, `last-prompt`, `file-history-snapshot`, `attachment`.
- **assistant** → `message.content` = blocks `{type: thinking|text|tool_use}`; `message.usage` carries token counts (`input_tokens`, `output_tokens`, cache_*) → **burn rate is available** (bonus).
- **tool_use** lives in assistant content blocks: `{type:"tool_use", name, input, id}`. File ops (`Edit`/`Read`/`Write`) carry `input.file_path`. Names seen: Edit, Read, Write, Bash, AskUserQuestion, **Agent**.
- **tool_result** comes back in the *next* `user` message content (`{type:"tool_result", tool_use_id, ...}`) + a top-level `toolUseResult`. Pair to its `tool_use` via the id. Errors surface here.
- Top-level also: `sessionId`, `cwd`, `gitBranch`, `version`, `uuid`/`parentUuid`.

## Corrections to the spec
1. **Subagent tool is named `Agent`, not `Task`** — `input.subagent_type` gives the kind (e.g. `Explore`). Map `Agent` → `subagent_spawn` / its result → `subagent_done`.
2. **Durations from timestamp deltas** (not `durationMs`).

## Subagents — interior confirmed NOT observable from main transcript
- `isSidechain` exists as a field but is **False/None for all 1076 entries** — the 3 `Agent` spawns this session produced **zero** sidechain entries in this file.
- No separate per-subagent `.jsonl` in the project dir.
- → **v1's call stands and is now verified:** observe subagents at the **spawn + duration** level only (`Agent` tool_use → `delegating` activity/face); the interior isn't in reach. Defer any interior observation indefinitely unless a future CC version persists it.

## Residual unverified (one item)
- **The statusLine stdin JSON shape** (what CC pipes to the `render` command) is still assumed, not captured. → Capture next session by wiring a 3-line dump script as `statusLine` and triggering a refresh. Build `render` against the documented shape; adjust when captured. Low risk.

## Net: event-mapping is locked. Safe to build the daemon's classifier against fixture lines captured from this real transcript.
