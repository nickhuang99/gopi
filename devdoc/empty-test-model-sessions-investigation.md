<!-- doc-class: historical -->
# Investigation: Empty `test-model` Sessions in `gopi log`

## Problem
`gopi log --last 20` showed a large block of near-identical rows:
`test-model  0.0s  0 iter  0 tok  OK` — 16+ such rows in the tail alone.
Question: are these real (aborted) runs, a gopi parser/display bug, or test
pollution? The epic asked to (a) prove no raw log exists for them (i.e. NOT a
parser error), and (b) remove the empty-input session logs — but only those
100% certain to be empty test runs.

## Root cause — they are UNIT-TEST ARTIFACTS (not parser errors, not real runs)
The `test-model` string is used ONLY by Go unit tests: `internal/agent/*_test.go`
call `NewAgent("test-model")` / `NewSessionLogger("test-model", "")` across
dozens of worktrees/clones (gopi-align1-*, gopi-mem-*, gopi-sec-*, gopi-race-*,
GoPi, task-autoregister-glm, tmp/gopi-* …). Every impacted session_start has
`"workdir":"/home/user/work/<worktree>/internal/agent"`.

Older test code (pre `tempHome`/`newTestAgentEnv` HOME isolation) wrote
`NewSessionLogger`'s synchronous `session_start` straight into the real
`~/.gopi/sessions/<session-id>.jsonl`, then never ran a loop → the file contains
exactly one line (session_start) → **0 iterations, 0 tokens**.

### Hard evidence of "no raw log exists" (proves not a parser error)
1. A REAL gopi session's `session_start` ALWAYS carries the authoritative
   `"rawLogPath":"/home/user/.gopi/raw/gopi-raw-<ts>.log"` field
   (e.g. `c1d08222` → `gopi-raw-20260828-233104.log`, existing). See
   `internal/agent/session.go` NewSessionLogger docstring: the path is the
   session's own ground truth ("no timestamp heuristics").
2. EVERY empty `test-model` session_start contains **NO `rawLogPath` field at
   all** → no raw log was ever created → confirmed genuinely absent.
3. Cross-check: the newest empty-test-model batch (Aug 28 23:01–23:11) has ZERO
   entries in `~/.gopi/raw/` for that window (the only 23:0x files belong to the
   real deepseek sessions at 23:13/23:32). No hidden raw file exists.
→ The `gopi log` display is a faithful rendering of real (empty) session files —
**NOT a parser error**. The anomaly is that these files should never have been
written to the user's session store.

## Conservative deletion
- 797 session files matched the strict empty-test-model criteria:
  - `model == "test-model"`, `type == "session_start"`, single line (exactly 1),
    file < 300 bytes, **and no `rawLogPath` field**.
- Confirmed all 797 = 1 line, no raw events → 0 iter / 0 tok, backed up list to
  `/tmp/empty-test-sessions-backup-list.txt`.
- Deleted all 797 from `~/.gopi/sessions/` (verified gone; investigate.txt IDs all removed).
- Because none had a `rawLogPath`, **no raw log file needed deletion** (none existed).

## NOT deleted (kept intentionally — different category)
- 68 `test-model` session files that DO reference a (0-byte) raw log — they are
  not "no-raw-log" cases; a log file exists. Left for a separate decision.
- All real-model empty runs (`glm-5.3-flash`, `hy4-preview`, `deepseek-v4-flash`,
  e.g. `fb2c67d4`, `e3b39fcb`, `27233add`, `0cc3e8e2`) — real sessions with real
  raw logs + genuine task stdin → real (if empty) user runs, NOT test artifacts.

## Prevention note
Current `internal/agent` tests isolate HOME (`newTestAgentEnv`, `tempHome`), so
they no longer pollute `~/.gopi`. The 797 are a historical backlog from older
worktrees/commits before that isolation.

## Verification
- `gopi log --last 20`: empty `test-model` rows gone.
- All 16 investigate.txt test-model session IDs confirmed absent from disk.
- 797 deleted / 797 gone / 0 still-present (re-checked per file).
