<!-- doc-class: historical -->
> **CORRECTION-ANCHOR (2026-09-19).** The `--stop` / `--restart` mentions below are
> about the PARSER's rule (two actions in one argv are refused), not about the flags
> being retired — and the face itself is gone: the resident merger was closed
> (`f9ff1a89`) and deleted (`55c4d6f3`), so every spelling of `gopi merger` now
> refuses and the one-tick merge is the bare `gopi merge`.

# Task Report: `--force` is a MODIFIER — the `merger --stop --force` refusal (P0 on master)

Branch: `fix/merger-stop-force-modifier` (worktree `~/workspace/gopi-stopforce-modifier`),
rebased onto master `464164d3` (commit `e8c6d8e5`). Baseline red was reproduced on a pristine
`356db1c9` worktree (routing note: `464164d3` = adoption of `fix/stylegate-test-sleep-annot`,
open a FRESH branch from master).

## Completed

### 1. The reported defect, root-caused

`gopi merger --stop --force` exited **1** with `gopi merger: --stop and force — pick ONE
action`, while the same spelling is printed in the face's own usage head
(`gopi merger --stop [--wait 25m] [--force]`, `cli_merger_stop.go`) and asserted by the
e2e contract (`merger_graceful_restart_e2e_test.go:456`, "must exit 0").

Baseline red reproduced on a pristine master worktree (`git worktree add --detach 356db1c9`)
with no other test running — pre-existing, not branch-local.

**Root cause (5-why):** D2 (`c897390c`, "dedupe contract") made the parsers keep the FULL
distinct action list so a refusal could NAME every word the operator typed — including
`--force`. The shared decider `controlConflictMessage` then counted that list:
`if len(p.distinct) > 1 { refuse }`. `--force` is BOTH a modifier of `--stop`/`--restart`
and an action of its own (a bare `--force` folds to a force-stop), so the documented
modifier spelling became a two-entry list and was refused as a collision. The refusal had
also been given the modifier's *token* at the same time (`mergerActionForce =
"force"` deriving the flag), which is what let the collision count see it as a peer action.

### 2. The fix — two questions, one input

The decider now separates the two readings of the SAME list, and derives both from ONE
field (`controlConflictParse.distinct`):

- **READBACK** (what was typed): the refusal keeps naming every distinct word, `--force`
  included — the D2 contract is intact, `--start --force --stop` still reads
  `start and force and stop`.
- **COLLISION** (what will run): `controlRequests(distinct)` drops the modifier; the refusal
  fires only on `len(...) > 1`. `--stop --force` is ONE request with a modifier.

`controlRequests` is a leaf function called *inside* `controlConflictMessage`, not a second
struct field: a face can therefore never hand over a list the decider reads as the wrong
one (an earlier draft did exactly that and the review caught it).

Spelling single-sourced (Rule 13/14): `controlModifierActionWord = "force"` is the ONE
word, and both faces' flag constants derive from it (`mergerControlForce = "--" +
controlModifierActionWord`, same for the worker) — the flag, the action constant and the
modifier predicate are one value, so a rename cannot leave the comparison looking for a
word that is no longer in the list.

### 3. Regression test — the minimal repro without the e2e rig

`cmd/agent/merger_stop_force_modifier_test.go`:

- ① `TestModifierForceIsNotASecondAction` — unit decider, both faces: `--stop --force` is
  NOT refused, while `--start --force --stop` still refuses AND still names `force`.
- ②③ `TestMergerStopForceAcceptedByCLI` — the real binary: `merger --stop --force` exits 0
  with no "pick ONE action" line and no daemon running (no restart rig needed), and
  `merger --stop --restart` is still refused — so ② cannot be satisfied by disabling the
  check.

Red-proof on unfixed master (`~/workspace/gopi-sf-red`, detach `356db1c9` + only this test
file): both fail with the reported symptom —
`\`merger --stop --force\` is the documented spelling and must not be refused, got "stop and force — pick ONE action"`
and `\`merger --stop --force\` must be accepted (exit 0), got 1`. ③ passes on master, i.e.
it is a guard, not a repro.

### 4. Two MORE master reds found by the same acceptance run (clock- and gate-dependent)

The briefing asked for `go test ./cmd/agent/ -run TestMergerRestart_ -count=1` green. That
run surfaced two failures that are NOT this epic's subject but block its acceptance gate —
both reproduced identically on unfixed master, so both are pre-existing:

**(a) The peak-hour start gate makes the merger rigs red four hours a day.** At
2026-09-14 14:00:07 CST (inside DeepSeek's 高峰时段, Beijing 14:00-18:00) the gate
(`cmd/agent/peak_guard.go`, before any subcommand dispatch) refused the child with exit
`config.PeakGuardExitCode` (4):

```
--- FAIL: TestMergerRestart_BadWaitIsRefused
        gopi merger: peak-hour cost guard: refusing to start — … inside 14:00-18:00
--- FAIL: TestMergerRestart_NoAlarmWordingAndTheCgroupIsMeasured
        `gopi merger --restart` must exit 0, got 4
```

Fix: ONE pin in the suite's `TestMain` (`cmd/agent/testmain_test.go` →
`unpinPeakHourGate`), not per-rig — the cause is a suite-level ambient condition, and
pinning one rig would leave the next rig to rediscover the same four-hour hole. Every rig
env (`mgrEnv`/`mdEnv`/`dcEnv`) derives from `os.Environ()`, so one pin covers all. The
gate's own behaviour is untouched and stays asserted where it belongs
(`peak_guard_test.go`, `internal/config`); a test that wants the ARMED gate can still undo
the pin in its own child env (an explicit extra entry wins).

**(b) `internal/stylegate` was RED on master, two rules:**
`test-sleep: 1 test hit(s), ceiling 0` at `cmd/agent/cli_worker_status_test.go:91`
(a bounded comm-poll of a freshly exec'd child — the *test* had no marker; verified
pre-existing: on a clean master worktree the same run reports it), and
`ignored-error: 187 test hit(s), ceiling 182`.

Fix scoped to what this epic's gate needs: the sleep site is now annotated with its
argument (`//stylegate:allow-sleep …`). The `ignored-error` ceiling (187 > 182, repo-wide,
not touched by this branch) is **NOT** fixed here — it is a separate sweep (see Not Done).

## Not Done

- **`internal/stylegate` `ignored-error` ceiling is still exceeded on master (187 > 182).**
  Repo-wide `_ = <error-typed call>` discards in test files, unrelated to this epic
  (no file this branch touches is among them). Master has since fixed the two stylegate
  reds this package carried (`4ed0550a` test-sleep annotation + the `TestGate2` annotation),
  so this branch reverted its own redundant annotation and was rebased onto `464164d3`.
- **Sibling red `TestWorkerDaemon_StatusReportsTheLiveDaemon`
  (`worker_daemon_singleton_e2e_test.go:144`) is pre-existing and NOT this epic's subject.**
  The full-package run (`e8c6d8e5`, 188.2s) is red on exactly this one test. It asserts that a
  daemonless `worker --status` must not print this home's worker STATE dir, while the binary
  prints `lock: <home>/.gopi/worker.lock` — a lock-file path, not `workerStateDirName`
  (the rig's substring check is over-broad). Confirmed pre-existing: it fails identically on
  the pristine unfixed master worktree `356db1c9` (`--detach`, peak gate disarmed), so it is
  not caused by this change. Per the briefing NOTE it is being fixed on
  `fix/daemon-scope-privilege-hint`; deliberately not duplicated here (Rule 23).

## Verification

- Build: `go build ./...` — OK.
- Vet: `go vet ./cmd/agent/` — OK (after two dead-ends: an unused `os` import and an
  `append` with no values in the new test file).
- **Acceptance (the briefing's command):** `go test ./cmd/agent/ -run TestMergerRestart_
  -count=1` — **ok, 59.2s** (was FAIL: `TestMergerRestart_ForceInterruptsAndReportsAndKeepsMasterBuildable`
  `merger_graceful_restart_e2e_test.go:456`).
- Repro, focused: `go test ./cmd/agent/ -run 'TestMergerStopForce|TestModifierForce|TestControlConflictMessage|TestDedupeActions' -count=1 -v`
  — all PASS.
- **Red-proof on unfixed master:** `~/workspace/gopi-sf-red` (`--detach 356db1c9` + the new
  test file only) — both new tests FAIL with the reported symptom (1 and 2 assertions).
- Style/standards gates: `go test ./internal/standards/` — ok; `go test ./internal/stylegate/`
  — green on the `test-sleep` rule, still FAIL on the pre-existing `ignored-error` ceiling.
- Full package: `go test ./cmd/agent/ -count=1` on the rebased tip — **188.2s, red on exactly
  ONE test**: the pre-existing sibling `TestWorkerDaemon_StatusReportsTheLiveDaemon` (see
  Not Done; fails identically on unfixed master `356db1c9`). No other test failed.

## Files

- `cmd/agent/cli_merger_stop.go` — modifier-derived flag spelling.
- `cmd/agent/cli_worker_control.go` — `controlModifierActionWord`, `controlRequests`, the
  two-view decider, worker flag spelling.
- `cmd/agent/merger_stop_force_modifier_test.go` — new: the minimal repro (unit + binary).
- `cmd/agent/testmain_test.go` — `unpinPeakHourGate` (clock-independent suite).
- `devdoc/merger-stop-force-modifier.md` — this report.

## Lesson (for memory)

A refusal that *names* the words an operator typed and a refusal that *counts* the actions
they requested are two different predicates over ONE list. Sharing the list is right;
sharing the count is the bug — a MODIFIER rides the naming list and must be filtered
before the count.
