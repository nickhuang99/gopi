<!-- doc-class: normative -->
# Devdoc: GOPI_HOME / --home data-root isolation + empty-input guard

Branch: `feat/session-home-isolation` (worktree `~/workspace/gopi-session-home`)
Date: 2026-08-29
Epic: (a) SessionLogger should be isolated in tests — the 8/26 code comment
claimed HOME redirection but tests still wrote throwaway session logs into the
real `~/.gopi/sessions/`; (b) empty input must print help, never start a run.

## Root cause (5-why)

1. *Empty `test-model` sessions pollute `~/.gopi/sessions`* — why?
   `NewAgent()` eagerly calls `NewSessionLogger()` **before any input exists**,
   so merely constructing an agent in a test creates a session file.
2. Why did the comment lie? The isolation helpers (`newTestAgentEnv`,
   `testAgentEnv`) set `HOME`, but **`internal/agent/taskregister_test.go`'
   `newAutoTaskAgent` set nothing** — its comment claimed a "Home override"
   that was never applied. Measured: **+4 real sessions per test run**.
3. Why is there no `--home`-style switch? Session dir, raw log dir, settings
   path, memory.db and plugin scripts each called `os.UserHomeDir()`
   independently (8 call sites) — no single data-root knob existed.
4. Why can empty input "run"? At the CLI, `readInput` + the
   `No input provided` guard already reject empty stdin/args (verified on
   master: exit=1, no session). But **the library level had no guard**: any
   other caller of `Agent.Run("")` would log a user message into the eagerly
   created session and start the loop.
5. Root causes: (a) no single data-root override, (b) eager session creation
   leaks side effects into construction, (c) no loop-entry empty-input guard.

## Fix

### 1. `config.GopiDir()` — one data root, one place

`internal/config/gopihome.go` (new):

- `GOPI_HOME` env var (the only place it is read) → data root is
  `$GOPI_HOME/.gopi`; else `$HOME/.gopi` via `os.UserHomeDir()`.
- `config.GopiHome()` exposes the root itself (without `.gopi`) for
  tilde/sandbox-style consumers.
- All 8 gopi-data call sites now resolve through `GopiDir()`:
  session.go, agent/rawlog.go, deepseek SettingsPath, config
  DefaultSettingsPath, gopilog reader+rawlog, tools/memory.go,
  tools/websearch.go, cmd/agent cli_checkpoint.go.
- Bug found by E2E while wiring: `gopilog`'s `sessionsDir = ".gopi/sessions"`
  constant double-prefixed (`.gopi/.gopi/sessions`) — fixed to bare
  `sessions`/`raw` segments.

### 2. `--home PATH` CLI flag

- `Config.GopiHomePath` (`gopi_home` settings key) + `--home` flag
  (flags.go scalar) + `ApplyEnv` injects it as `GOPI_HOME` **first**, before
  any reader runs. Precedence stays: flag > env > settings > default.
- Listed in the env catalog + `--help` options block; unknown flags still
  rejected (`gopi: unknown flag --home2`).

### 3. Library-level empty-input guard

`Agent.runEngine` (loop.go), before any message is logged:

```go
if !resume && strings.TrimSpace(userInput) == "" && len(images) == 0 {
    return "", fmt.Errorf("empty task input: ...")
}
```

- Resume is exempt (empty continuation is legitimate there).
- Image-only input passes (`len(images) > 0`) — native vision tasks.
- The CLI keeps its own earlier guard (prints usage); the library guard
  protects every other caller. Both were needed: construction happens before
  input is known, so the guard must sit at the loop entry.

### 4. Test-isolation fixes

- `taskregister_test.go`: helper now does `t.Setenv("GOPI_HOME", t.TempDir())`;
  the lying comment is replaced with the truth (8/26 regression documented).
- `loop_test.go newTestAgentEnv` / `cmd/agent main_test.go testAgentEnv`:
  additionally set `GOPI_HOME` (gopi data root isolation, same sandbox as
  HOME) — belt-and-braces; sessions also can't escape if a future reader
  bypasses HOME.

## Verification

- `go build ./...`: OK
- New tests (all PASS):
  - `internal/config/gopihome_test.go`: GOPI_DIR precedence, fallback,
    whitespace-unset, `--home` parsing + ApplyEnv injection, `=` form.
  - `internal/agent/session_gopihome_test.go`: Run("") errors "empty task
    input" for empty/whitespace input and logs NO user message;
    NewSessionLogger lands in `$GOPI_HOME/.gopi/sessions` (HOME unset!);
    taskregister session stays out of the real profile.
- Measured pollution: targeted `go test` run created **0** sessions in the
  real profile (was +4 before; remaining +3 in one measurement came from a
  DIFFERENT agent's worktree `gopi-taskmodel-db` running old code in
  parallel — confirmed by the session's `workdir` field, disappears when
  that branch rebases).
- E2E (`/tmp/gopi-fix`): empty stdin / `echo ""` / `--prompt ""` / `""` all
  exit=1 with `No input provided`, no session created; `/dev/null` stdin is
  a char device → usage branch (also safe); `gopi log` reader honours
  GOPI_HOME both ways (finds sandbox session, still falls back to real
  profile without it).
- Full `go test ./... -count=1`: all packages pass except a pre-existing
  flaky `TestMemorySearchNonBlocking` (SQLITE_BUSY; fails with the changes
  stashed too — unrelated to this branch).

## Not Done / Deferred

- Eager session creation in `NewAgent` kept (an empty `<id>.jsonl` may still
  be created before Run rejects input when callers construct agents without
  running them). Making it lazy touches resume path + taskregister + CLI
  printLogPaths — follow-up if empty-file residue matters.
- `tools/bash.go` tilde expansion (`~/`, workdir sandbox) intentionally still
  uses the OS home — `~` in shell commands is the USER's home, not gopi's
  data root.
- Parallel-agent worktrees running old code still leak until they rebase.

## Usage

```bash
# isolate a whole gopi run (or a test) from the real profile:
GOPI_HOME=/tmp/gopi-sandbox gopi "task"
gopi --home /tmp/gopi-sandbox "task"
# in Go tests:
t.Setenv("GOPI_HOME", t.TempDir())
```
