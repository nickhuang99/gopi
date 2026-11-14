<!-- doc-class: normative -->
# Task List + Heartbeat + Kernel Prompt Integration

> Tasks: 02-tasklist-parser, 03-heartbeat-timer, 04-kernel-prompt-integration
> Date: 2025-08-03
> Branch: feat/tasklist-heartbeat-kernel

## Overview

Implemented structured task tracking with heartbeat-based stall detection for
the GoPi agent's ReAct loop. The LLM now produces parseable task lists in every
response, the framework validates them (no deletion, valid statuses), and a
background heartbeat goroutine detects stalled progress.

## Files Changed

| File | Change |
|------|--------|
| `internal/agent/tasklist.go` | NEW — parser, validator, task list utilities |
| `internal/agent/tasklist_test.go` | NEW — 36 tests covering all edge cases |
| ~~`internal/agent/heartbeat.go`~~ | NEW — stalled-progress detector, thread-safe. **REMOVED 2026-09-29** (task 01a0ed9f2e): it was never given a production constructor, and the live stalled-progress role is `maybeNudgeStaleTaskList` (`devdoc/heartbeat-knobs-removed.md`) |
| `internal/agent/loop.go` | EDITED — kernel prompt, Epic framing, heartbeat wiring |

## Architecture

```
LLM Response
    │
    ▼
ParseTaskList(response) ──→ []TaskItem or nil
    │
    ▼
ValidateTaskList(prev, curr) ──→ error if deleted/invalid
    │
    ▼
hb.UpdateTaskList(tasks)  ──→ increments unchanged counter
    │
    ▼
AllCompleted(tasks) && no tool calls? ──→ early termination
```

### Heartbeat State Machine

```
Round N: task list same as Round N-1 → unchangedCnt++
Round N: task list changed → unchangedCnt = 0 (or 1 for first round)

Every 30s (configurable):
  if !toolRunning && unchangedCnt >= staleRounds && tasks progressing:
    → inject HEARTBEAT REMINDER message
    → reset unchangedCnt = 0
```

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| ~~`GOPI_HEARTBEAT_SEC`~~ | 30 | Heartbeat tick interval (seconds). **REMOVED 2026-09-29** — the timer it configured had zero readers (`devdoc/heartbeat-knobs-removed.md`) |
| ~~`GOPI_HEARTBEAT_STALE`~~ | 3 | Consecutive unchanged rounds before firing. **REMOVED 2026-09-29** with the knob above |

All env var reading is in `NewAgent()` (Rule 2: Single Source of Truth).

## System Prompt Extension

The immutable task management rules are appended to the system prompt:

```
You are GoPi, a coding agent. Tools: read, write, edit, bash.
[TASK MANAGEMENT RULES]
1. First turn: generate an initial task list from the Epic goal...
2. Every response MUST end with [TASK_LIST_BEGIN]...[TASK_LIST_END]...
3. Only 2 task statuses: "progressing" and "completed"...
...
```

## Task List Format

```
[TASK_LIST_BEGIN]
[
  {"task_id":"t001","summary":"读取配置文件","create_at":"1780000000","status":"completed"},
  {"task_id":"t002","summary":"修改数据库schema","create_at":"1780000000","status":"progressing"}
]
[TASK_LIST_END]
```

### Validation Rules
1. No deletion: every previous task_id must appear in the new list
2. Only `progressing` or `completed` status allowed
3. No duplicate or empty task_ids
4. Parent decomposition: parent → completed, children → progressing

## Thread Safety

- `HeartbeatState` uses `sync.Mutex` on all public methods
- `CurrentTasks()` returns a copy (deep copy safe)
- Heartbeat goroutine only starts when `HeartbeatInterval > 0`
- Known limitation: `Ctx.AddMessage` is not mutex-protected, but the heartbeat
  fires infrequently and the main loop is single-threaded between API calls —
  race risk is minimal (documented with comment)

## Testing

- 36 tasklist tests: parsing, validation, all-completed, formatting, equality
- 14 heartbeat tests: trigger conditions, skip conditions, concurrency
- Race detector: clean (`go test -race`)
- All pre-existing tests continue to pass
