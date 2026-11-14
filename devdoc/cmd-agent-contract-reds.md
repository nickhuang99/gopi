<!-- doc-class: historical -->

> **NOTE 2026-10-26 — the verb this page is about NO LONGER EXISTS.**
> `gopi task sweep-zombies` and the automatic zombie disposer were deleted whole; a stuck
> row is now REPORTED and left where it is. The defect analysis below is kept as history
> (the Rule 15 lesson is still live in other verbs), but no command here can be re-run.
> Current behaviour: `devdoc/zombie-sweep-removed.md`.

# Task Report: the two inherited cmd/agent contract reds

Both reds were **production bugs**, not stale tests. Neither assertion was
weakened: the test's basis was correct in both cases and the production side
was fixed.

Repro (clean tree off master `e51b554c`):
```
env -u GOPI_WORKER_DAEMON_CHILD -u GOPI_DAEMON_READY_FD \
  go test -count=1 -run 'TestParseTasksSweepZombiesArgs|TestEveryHelpConstantFitsTheWrapBudget' ./cmd/agent/
```

## Red 1 — `TestParseTasksSweepZombiesArgs`: flags accepted, then silently dropped

**Symptom:** `stallSec=0 maxRunSec=0, want 900/5400`; bad and valueless
`--stall-sec` not refused.

**Root cause (not the defaults the briefing guessed at):** the whole
`--stall-sec` / `--max-run` parse body of `case taskverbs.SweepZombies:` in
`parseTasksArgs` (`cmd/agent/cli_tasks.go`) was mis-nested at the **tail of the
`case taskverbs.SweepWorktrees:` arm** — while still printing
`"task sweep-zombies: …"` error text. `taskFlagMember` admits `stall-sec` and
`max-run` for `sweep-zombies` **only**, so under the sweep-worktrees arm that
body was unreachable: the flags passed validation, then fell through
unread. That is a Rule 15 silent failure — and also a Rule 1b symptom (the
error text carried the wrong verb's identity, the tell that the block was
pasted into the wrong arm).

**Live A/B against the real binary (pre-fix):**
```
$ gopi task sweep-zombies --stall-sec 900 --max-run=5400
  … stall grace 1800s, max-run 0s      # requested 900/5400; accepted + ignored
$ gopi task sweep-zombies --stall-sec abc ; echo rc=$?
rc=0                                    # invalid value silently accepted
```

**Adjudication:** the test is right. `900/5400` is the documented override
semantics (`tasksUsage`), and `cmd/agent/cli_tasks_sweep_zombies_test.go:338`
already asserts the same contract through the resolution path.

**Fix:** moved the block (parse + both refusals + the `--dry-run` forcing
comment) back under `case taskverbs.SweepZombies:`. Post-fix live check:
`900s/5400s`, and `--stall-sec abc` exits 1 with the error.

## Red 2 — `TestEveryHelpConstantFitsTheWrapBudget`: one over-budget usage line

**Symptom:** the briefing's "~20 usage constants over budget" reading is wrong —
those `maxWidth=7x/8x` lines are `t.Logf` telemetry. The gate reports exactly
**one** error:

```
cli_tasks.go: tasksUsage line 102 is 96 display cols (> 88):
  "  gopi task sweep-worktrees [--dry-run] [--apply] [--min-age-h N] [--max N] [--json] [--db PATH]"
```

**Adjudication:** the test is right — 96 > `config.HelpWrapCol` (88), measured
with `config.DisplayWidth` (the CJK-aware measure; this line is ASCII, so the
count is unambiguous). `2b6586e7` wrapped *most* usage rows but missed this one.

**Fix:** wrapped the synopsis at its natural flag-group break; the continuation
is indented to align under `sweep-worktrees`.

Cross-check on the same measure (its other consumer): `cli_brew.go`'s
`brewUsage` line `      against the three lesson conditions (可重复模式 / 可复现闭环 / 收益达标),`
is 93 **bytes** but only 77 **display cols** (8 CJK glyphs × 2), so it is under
budget — which is why the gate reported one error and not two. The byte count
is exactly the trap `config.DisplayWidth` exists to avoid.

## Why neither was a stale test

Both contracts describe behaviour the surrounding code and docs already
promise (`tasksUsage`, the sibling sweep-zombies resolution tests,
`HelpWrapCol`). The tests were the last place the promise was still enforced.

## Not Done

- No sweep of the other help constants: the gate is discovery-based and green,
  so nothing else is over budget. No further audit attempted.
- The `--dry-run` forcing comment travelled with the moved block; no behaviour
  change intended or checked beyond the moved code compiling and the contract
  tests passing.
- Did not touch the `fix/worker-status-stop-conflict` owner branch mentioned in
  the briefing — per Rule 23 the fix was made on a fresh branch off master.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK
- Targeted: `go test -count=1 -run 'TestParseTasksSweepZombiesArgs|TestEveryHelpConstantFitsTheWrapBudget' ./cmd/agent/` — PASS
- Package: `go test -count=1 ./cmd/agent/` — `ok github.com/nick/gopi/cmd/agent 348.088s` RC=0
  (full log `/home/user/scratch/contract-reds-pkg.log`)
- Assertions unweakened: `git diff e51b554c..aa147684 -- cmd/agent/cli_tasks_test.go`
  is additions only; lines 821/838/841 are byte-identical to master
- `gofmt -l cmd/agent/` empty; `go vet ./cmd/agent/` clean
- Live CLI A/B: pre-fix drop reproduced, post-fix override honoured + invalid
  value refused (rc=1)

## Deliverable location

- Branch: `fix/cmd-agent-inherited-contract-reds` (worktree `/home/user/workspace/gopi-contract-reds`)
- Recovery: `git show fix/cmd-agent-inherited-contract-reds:devdoc/cmd-agent-contract-reds.md`
