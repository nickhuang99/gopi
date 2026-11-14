<!-- doc-class: historical -->
# Task Report: merger singleton — two `gopi task merger` processes (bug 01a089b7 companion)

Branch: `fix/merger-singleton-lock` · worktree `~/workspace/gopi-merger-singleton`

## The reported symptom

Ledger evidence preserved from the 2026-09-10 restart window:

| row | created | claimed_by | shape |
|---|---|---|---|
| `t-1789015454` | 12:44:14 | `peerbox:2209903` | `resolve merge conflict: feat/hippocampus-host`, tags `merge-conflict,auto-spawn`, session_id **""** |
| `01a089a7-1e32…` | 12:50:25 | `peerbox:3128385` | `resolve merge conflict: feat/safe-merge-guard-answer`, tags `merge-conflict,auto-spawn`, session_id **""** |
| `01a089a7-26cb…` | 12:50:28 | `peerbox:3128439` | `[auto] You are the merger's conflict resolver for branch …`, tags **""**, session_id **98d7ba16…** |

Three pids stamped rows inside one restart window ⇒ read as "two merger daemons are sweeping the same repo".

## Root cause — the premise was wrong, and the real defect was found

**(a) The singleton guard was never breached.** `acquireServeLock` (cmd/agent/cli_tasks.go) takes
`flock(LOCK_EX|LOCK_NB)` on exactly one global file, `<GopiDir()>/serve.lock`
(`GopiDir()` = `GOPI_HOME/.gopi`, else `~/.gopi`). Path, symlink-target (`config.GopiDir`
ignores `GOPI_SETTINGS`) and lock scope are unchanged since `c84d9f0` — that is, byte-for-byte the
code that was running at 12:50. Live confirmation: `~/.gopi/serve.lock` holds `3128385`, the ONE
live merger (`./bin/gopi task merger --every 5m`); the other pids in the window (`3128439`,
`3116997`) are resolve children, not daemons. Two merger processes CANNOT both sweep: the second
`acquireServeLock` fails and `runTaskMerger` exits 1. Mechanisms ruled out, one by one: no second
lock file anywhere under the profile; `cmd.Stdout = os.Stderr` does not leak the lock fd — fd-level
flock survives `dup`; `runTaskMerger` never spawns a child merger — the only spawn is the
test-only `GOPI_MERGE_RESOLVE_BINARY` hook, read by `mergeResolveBinary()`; no `GOPI_MERGE_RESOLVE_*`
in the live merger's `/proc/3128385/environ`.

**(a′) The lock is a LIFETIME lock, which answers the briefing's question (c).**
`runTaskMerger` calls `acquireServeLock()` exactly ONCE, *before* the pre-loop drain pass and before
the poll loop (`defer lock.Close()` — released only at process exit). So it is held across every
pass and across a blocking resolve inside `MergerRepoSweep` (the 20-minute `mergeResolveChildWaitTimeout`):
while the first merger sleeps on a child, the flock is still held, so a second process entering
"because the lock was only taken once at startup" is exactly what CANNOT happen — the once-at-startup
take is what makes it a lifetime singleton. The kernel releases the flock on process death (no
heartbeat needed), which is why the post-kill negative case starts cleanly.

**(b) The row that looked like a second merger was a resolve CHILD auto-registering itself.**
`01a089a7-26cb` is not a merger tracking row: its summary is the child's *briefing*
(`[auto] You are the merger's conflict resolver for branch "feat/safe-merge-guard-answer" …`), it
carries a real `session_id`, and it has none of the merger's `merge-conflict,auto-spawn` tags —
i.e. it is an **auto-registered child session**, not a `MergerRepoSweep` row (whose shape is
summary `resolve merge conflict: <branch>`, tags `merge-conflict,auto-spawn`, empty `session_id`,
`claimed_by` = the merger pid).

Why did the child auto-register? `cmd/agent/merge_resolve_child.go:124` spawned it with

```go
tools.AutoRegisterOffEnv+"=1",   // GOPI_AUTO_TASK_REGISTER=1  ← the OFF NAME, the ON VALUE
```

introduced by `bbc7503` (2026-09-05). `autoTaskRegisterEnabled()` only treats
`0/off/no/false/disabled` as OFF, so `=1` — the ON value behind an `Off` name — turned
auto-registration ON for every resolve child. The child is a full LLM agent loop, so it can
outlive the merger that spawned it, and each resolve minted a duplicate ledger row exactly like
`01a089a1` (12:44:17) and `01a089a7-26cb`. That is the entire "several merger pids" appearance — and
it is a Rule 15 silent-failure specimen: an env latch whose *name* promised one thing and whose
*value* did the opposite, invisible because the env pair was assembled inline and never read by
any test.

## Completed

- **Fixed the latch** (`internal/tools/tasks.go`, `cmd/agent/merge_resolve_child.go`,
  `internal/agent/taskregister.go`): the OFF pair now has one source of truth —
  `tools.AutoRegisterOffEnv = "GOPI_AUTO_TASK_REGISTER"` + `tools.AutoRegisterOffValue = "0"`,
  with the canonical value pinned by `agent.AutoTaskRegisterOff` (`"GOPI_AUTO_TASK_REGISTER=0"`,
  single exported pair, guarded by `autoTaskRegisterEnabledWith` — the same semantics function the
  real reader uses). The spawn site now builds the env through one writer, `mergeResolveEnv(branch)`.
- **Daemon-level repro test** `TestMergerSingleton_TwoConcurrentInvocations`
  (`cmd/agent/merger_singleton_e2e_test.go`): two REAL `task merger` invocations against one
  sandboxed profile (the worker-help binary-build precedent). The holder daemon
  (`task merger --every 5m`) stamps its pid into `serve.lock`; the second (`--once`) **exits
  non-zero** with `already running … refusing double-serve`, names the live holder pid, and never
  prints `single pass done` (it did not sweep); the holder still lands its seeded branch (the flock
  serialises daemons, it does not freeze sweeps); after the holder is killed, a fresh merger runs
  normally and lands a newly seeded branch — **no stale-lock deadlock**.
- **Latch gate** `TestMergeResolveEnv_DisablesAutoRegister` (`cmd/agent/merge_resolve_env_test.go`)
  + `TestAutoTaskRegisterOff_IsOff` (`internal/agent/taskregister_test.go`) — both falsified against
  the defect (see Verification) so they guard the regression, not just describe it.
- The E2E is hermetic: the seeded branches are clean (`--allow-empty` divergence), so the sweep
  never discovers a conflict and never spawns an LLM child; `GOPI_MERGE_RESOLVE_BINARY` is seeded
  to the mechanical resolver as a second belt, and a `t.Cleanup` kills the holder daemon so a
  failing assertion can never leave an orphan sweeping temp repos (the very shape under
  investigation).

## Not Done

- **No change to `acquireServeLock` semantics.** The investigation found no hole to close: the
  flock is global, non-blocking, pid-stamped, verified at process level. Deliberately NOT added: a
  heartbeat/pid-liveness layer — `flock` is already released by the kernel on process death, which
  is *why* the negative case passes; an added layer would only introduce a stale-lock risk.
- **Not reproduced:** a live resolve child under `GOPI_AUTO_TASK_REGISTER` (needs a conflicting
  branch + a model call). The latch is therefore proven at the env-contract level (constant →
  spawn env → reader semantics), not by observing a real child's ledger row. The mechanism is
  unambiguous (one reader, one value), and the ledger rows `01a089a1` / `01a089a7-26cb` already
  are the observed-after-the-fact evidence.
- The briefing's premise ("two merger daemons") is corrected rather than satisfied; the
  daemon-level singleton test requested by the acceptance criteria is shipped anyway, and the
  companion `feat/merger-resolver-dedup` in-flight guard is unaffected by this fix.

## Verification

- Build: `CGO_ENABLED=0 go build -o /tmp/gopi-singleton ./cmd/agent/` → OK
- Focused: `go test ./cmd/agent/ -run TestMergerSingleton_TwoConcurrentInvocations -v` →
  `--- PASS: TestMergerSingleton_TwoConcurrentInvocations (2.94s)`; no orphan process after the run.
- Latch gates: `TestMergeResolveEnv_DisablesAutoRegister` PASS; `TestAutoTaskRegisterOff_IsOff` PASS.
- **Falsification (the tests fail on the defect, not just pass on the fix):**
  - `autoTaskRegisterOffValue = "1"` → `FAIL … AutoTaskRegisterOff = "GOPI_AUTO_TASK_REGISTER=1", want … "0"`.
  - spawn site reverted to `tools.AutoRegisterOffEnv + "=1"` →
    `FAIL … resolve-child latch = "GOPI_AUTO_TASK_REGISTER=1", want the OFF pair "GOPI_AUTO_TASK_REGISTER=0"`.
  - both restored → PASS.
- Suites: `go test ./cmd/agent/... ./internal/agent/... ./internal/tools/...` → see below.
- Commit: see `git log --oneline -1` (hash recorded in the session ledger / final message).

## Operator note

An already-deployed merger keeps the old behaviour only until it is restarted — the latch fix lives
in the resolve-child spawn path, so `./bin/gopi task merger --every 5m` must be restarted on the
new binary for resolve children to stop minting ledger rows.
