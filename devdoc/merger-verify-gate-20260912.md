<!-- doc-class: historical -->
# Post-merge verify gate (merger tick) — 2026-09-12

> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** Standing claim made
> before the change it describes: the merger TICK that ran this verify gate was
> deleted with the resident merger (`f9ff1a89`, `55c4d6f3`); every spelling of
> `gopi merger` now refuses and the one-tick merge is the bare `gopi merge`. The
> merge/verify judgement rules recorded here are the design the surviving face
> inherits. See `devdoc/merger-retired.md` and the dated correction in
> `devdoc/merger-self-driving.md`.

Epic: **merger-verify-gate**. The merger merges mechanically and never judges the
RESULT on master. Only the post-merge master is the truth (two branches each
green alone can break each other; a REGRESSED BUGFIX is worse than an unshipped
feature). This change gives the merger tick a judgement half: after a merge
lands on master, the merger **blocks** on an independent verify child that
judges the merged state on the real master, and **unwinds the merge** if the
verdict is FAIL.

Branch: `feat/merger-verify-gate` (worktree `~/workspace/gopi-mergeverify`).

## Flow (in order, all inside the merger tick)

```
mergerSweepRepo(repo)                       cmd/agent/cli_tasks.go
  ├─ capture := tools.CaptureFailedMergeRevert(repo)    ① pre-round snapshot
  │     PreTip = master's tip, BranchTips = every branch's tip
  ├─ gatedBranch := mergerVerifyGatedBranch(repo, capture)   ← predicate
  │     the alphabetically-first UNPROTECTED branch whose DELIVERING
  │     ledger row carries the `bug` tag (tools.MergeVerifyRecordForBranch)
  │     "" → the pass is untouched (no exclusion, no gate)
  ├─ tools.MergerRepoSweep(repo, 0, {Resolve, ExcludeBranches: [gated]})  ② sweep
  ├─ if merged/deleted > 0 → serveRebuildAfterMerge(repo, snapshot) (unchanged)
  └─ if gatedBranch != "" → mergerVerifyGateAtRepo(repo, capture, gatedBranch)
        ├─ cap check: tools.MergeVerifyFailCount(repo, branch) vs MergeVerifyFailCap
        │     ≥ cap → record a HUMAN-decision row + notify, no rework, no merge
        ├─ ③ merge the branch (tools.TaskMerge → taskMergeOne)
        │     conflict → run the merger's OWN Resolve closure, re-merge
        │     (the branch is EXCLUDED from the sweep, so without this a
        │      conflicting bugfixing branch could never be resolved/gated)
        │     beforeTip = the per-branch PreMergeTip taskMergeOne recorded
        │                 BEFORE `git merge` (fallback: capture.PreTip)
        ├─ ④ runMergeVerifyChild(rec)  — spawn + BLOCKING waitpid
        │     exec <gopi> task verify <task_id>, cwd = the delivering row's
        │     deliverable.repo, env GOPI_MERGE_VERIFY_TASK=<task_id>,
        │     GOPI_AUTO_TASK_REGISTER=0; own process GROUP (Setpgid)
        │     bounded by mergeVerifyChildWaitTimeout = 15m; at the cap the
        │     whole group is SIGKILLed (grandchildren included)
        ├─ ⑤ verdict read from the LEDGER, never the exit status:
        │     tools.TaskStatusOf(id) == "verified" → PASS
        │     == "failed"  → FAIL
        │     else          → child-dead (unverified ⇒ not kept)
        ├─ PASS → MergeVerifyRecordEvent + MergeVerifyNotify, keep the merge
        └─ FAIL → mergerVerifyUnwind:
             1. MergeVerifyPreserveBranch(branch, afterTip) — the landing's
                `git branch -d` already removed the ref; without this a
                reset back to the pre-merge tip leaves the work unreachable
             2. MergeVerifyMergeCommit(repo, beforeTip) — a merge commit whose
                first parent is beforeTip, else "" (a sweep landing usually
                FAST-FORWARDS: no merge commit at all)
             3. MergeVerifyRevertTo(repo, beforeTip, mergeCommit)
                  mergeCommit != "" → `git revert -m 1 <mc>` + assert
                    `git diff --quiet beforeTip HEAD` (tree-identical)
                  mergeCommit == "" → `git reset --hard beforeTip` + assert
                    `rev-parse HEAD == beforeTip`
                ASSERTED in both paths — a revert that silently leaves master
                 elsewhere is the failure mode this epic exists to prevent
             4. MergeVerifyFileRework → a NEW row referencing the old task id
             5. MergeVerifyRecordEvent + MergeVerifyNotify
             6. serveRebuildAfterMerge → the binary follows the restored master
```

## Key decisions (and why)

**Where the gate lives: the merger tick, not the VCS cycle.** `vcsLoopPass`
runs in its own goroutine concurrently with the merger pass, so a verify gated
inside a cycle pass would not be *the merger blocking on it*, and two master
writers could interleave under the gate's feet. Inside the flock-guarded
merger tick the pass is single-threaded and owns master for the gate's
duration. This is why `MergerRepoSweep` grew `ExcludeBranches` instead of the
gate being bolted onto a cycle: the sweep must not consume the branch the gate
is about to judge.

**Verdict = the ledger, not the exit status.** An agent can exit 0 without a
verdict and non-zero after writing one. The child therefore writes its verdict
through the EXISTING `gopi task verify` contract (completed/failed →
`verified`/`failed` on the delivering row) and the merger reads
`TaskStatusOf(id)`. No new verdict channel, no new protocol, no new tool.

**No new process/daemon/agent.** The child is a normal `gopi task verify`
spawn — the same shape as the resolve child (`merge_resolve_child.go`), with
the same env-override seam (`GOPI_MERGE_VERIFY_BINARY`) so tests drive the
real spawn+waitpid path with a mechanical stub.

**Machine-decidable bugfix predicate, fail-open.** "Bugfixing branch" = the
delivering ledger row carries the `bug` tag (`hasBugTag`, the SAME matcher the
queue's priority and the bug-flow tally use). Detection parses the deliverable
JSON and matches BOTH repo and branch exactly (never a substring match — a
branch name is a prefix of another: `fix/x` inside `fix/x-v2`), newest ended
row wins. If the ledger is missing/unreadable, or no row delivers the branch,
the predicate is UNPROVABLE and the gate does not fire — a gate that cannot
prove its predicate must not block or revert merges.

> **Open question deliberately NOT settled here** (recorded per the epic's
> "bugfix predicate ruling, incl. wider untagged-branch question"): the epic
> asked whether the predicate should also cover branches that touch existing
> code without a `bug` tag. The tag is what makes the predicate decidable, so
> this change ships tag-only; a wider predicate needs a different
> machine-readable signal (e.g. a `merge-risk` tag the submitter sets) and is
> its own task.

**Append-only ledger.** The judged row's TERMINAL status is never flipped back
by the gate; the rework is a NEW row whose summary, payload and deliverable
reference the old task id. One nuance surfaced by the tests and stated
honestly: the MANDATED verdict writer (`gopi task verify`) does move
`completed → failed` on the judged row — that is the verifier's write, it is
how the verdict becomes readable, and it is not the gate re-running anything.
The gate's own re-file half is proven byte-identical where no verify child
runs (`TestVerifyGateFileReworkReferencesOldTask`).

**Anti-loop cap.** `MergeVerifyFailCap = 3`; the count is durable and
crash-safe (it counts this gate's terminal-FAILED rows for the (repo,branch),
the same ledger-as-counter model as `resolveRowFailedCount`). At the cap the
machine stops filing rework and records a human-decision row + notify — a loop
that cannot terminate must hand over.

## Honest limits

- **One gated branch per pass.** The gate blocks the merger tick for minutes,
  so exactly one gated merge per pass keeps the tick's latency predictable
  (selection is deterministic: alphabetically first). A batch of bugfixing
  branches is gated one per tick.
- **A FAIL costs a revert commit or a reset.** The unwind restores the
  pre-merge state but the branch's work is preserved via a re-created ref
  (`MergeVerifyPreserveBranch`) — never lost, but it IS a new master-state
  event that other readers can observe mid-tick (all inside the merger's
  flock, so no concurrent merger pass).
- **The verify child's own cost is unbounded in content.** The gate bounds the
  WAIT (15m) but the child's `gopi task verify` spawns a full agent; measured
  per-gated-merge cost is therefore the child's agent run, not the git work.
  The cap makes the worst case a killed child + revert, never a wedged merger.
- **The rework row does not carry the resolver worktree.** The rework brief
  names the branch and repo; the rework agent re-derives the worktree itself.
- **In-tick gating of a conflict resolved in the SAME pass** was the epic's
  more ambitious reading. This change instead makes the GATE resolve the
  conflict itself (via the merger's own Resolve closure) and judge the
  resolved state — same requirement met ("the resolved state is what gets
  verified"), no cross-module handoff of the sweep's internals.

## Verification

Built and tested inside `~/workspace/gopi-mergeverify` (worktree on
`feat/merger-verify-gate`).

Verified on the final tree (`/home/user/workspace/gopi-mergeverify`, branch
`feat/merger-verify-gate`):

| check | command | result |
|---|---|---|
| format | `gofmt -l cmd internal` | empty (no unformatted file) |
| build | `CGO_ENABLED=0 go build ./...` | exit 0 |
| gate E2E | `go test -count=1 -run 'TestMergeVerifyGate' ./cmd/agent/` | `ok` (5 tests) |
| gate core | `go test -count=1 -run 'TestVerifyGate' ./internal/tools/` | `ok` (7 tests) |
| standards | `go test -count=1 ./internal/standards/` | `ok` (all six gates green) |
| full touched pkgs | `go test -count=1 ./internal/tools/ ./cmd/agent/ ./internal/standards/` | `internal/standards` ok; **2 failures, both pre-existing/environmental** — see below |

Two failures in the full run, NEITHER caused by this change (proven, not
assumed):

- `cmd/agent TestCLIContractInventoryMatchesDispatch` — **pre-existing on this
  branch's base**: the same test fails with every change of mine stashed
  (`git stash -u` → run → fail), reporting `main.go has 26 … contract suite
  tracks 25`. Untouched by this work.
- `internal/tools TestMemorySearchNonBlocking` — a load-sensitive flake: passes
  in isolation both with my change and on the stashed tree, and failed only
  under the concurrent full-package run.

### A note on Rule 11 (LLM adoption proof)

Rule 11 binds changes to the TOOL SURFACE (a new tool, a tool description, or
prompt routing). This change adds none: it reuses the existing
`gopi task verify` contract as the verify child and the existing
`NotifySyntheticEvent` side-channel. No prompt text, no tool description and no
`claudeToolDefs` entry changed, so the "LLM actually calls it" gate does not
apply here — stated explicitly rather than silently skipped.

- Gate tests (tools layer, `internal/tools/verify_gate_test.go`), real temp repos + a real ledger (`GOPI_MEMORY_DB` → temp file):
  - `TestVerifyGateDetectsBugfixRow` — predicate + prefix-confusion + no-row.
  - `TestVerifyGateRevertRestoresRecordedTip` — **negative case 1**: after a fast-forward landing, the revert leaves `rev-parse HEAD` EXACTLY at the recorded pre-merge tip, the branch's work survives at its merged tip, and the fix content is gone from master.
  - `TestVerifyGateRevertMergeCommitPath` — the `git revert -m 1 <mc>` path with a real divergent merge commit; master's own side work survives.
  - `TestVerifyGateRevertRefusesWithoutTip` — no recorded tip ⇒ refuse (never reset to an arbitrary point).
  - `TestVerifyGateFileReworkReferencesOldTask` — **negative case 2**: a FAILED verify files a NEW row referencing the old task id, carrying the gate vocabulary and the branch deliverable, while the judged row's terminal record is BYTE-IDENTICAL (status + tags + summary + deliverable + reason + source).
  - `TestVerifyGateFailCapStopsRefiling` — the anti-loop counter.
  - `TestVerifyGateRecordsVerdictEvent` — the append-only verdict event (`task_events` row) and the detail the operator reads via `gopi task show`.
- Gate tests (cmd layer, `cmd/agent/merger_verify_e2e_test.go`), the real spawn+waitpid path with a stub child that writes through the real ledger:
  - `TestMergeVerifyGatePassKeepsMerge` — PASS keeps the merge (content still on master, branch consumed, judged row `verified`, event records the master tips).
  - `TestMergeVerifyGateFailRevertsAndRefiles` — **negative case 3**: FAIL ⇒ master EXACTLY at the recorded pre-merge tip, branch work preserved, one NEW rework row referencing the old task id, judged row never re-run, event names the mechanism.
  - `TestMergeVerifyGateJudgesConflictResolutionFlagship` — a REAL conflicting bugfixing branch: the gate resolves it via the merger's own resolver child, re-merges, and judges the RESOLVED state; FAIL restores the recorded tip and files the rework.
  - `TestMergeVerifyGateNonBugBranchIsNotGated` — the predicate is narrow (feature branch / orphan branch ⇒ no gate).
  - `TestMergeVerifyGateExcludedFromSweep` — the gated branch is excluded from the sweep turn (master untouched, branch survives).

### Red-proof (pre-fix behaviour)

The gate is new code, so "proven red pre-fix" is demonstrated by disarming the
mechanism and watching the decisive assertions fail. Command:

```bash
GOPI_MERGE_VERIFY=0 CGO_ENABLED=0 go test -count=1 \
  -run 'TestMergeVerifyGateFailRevertsAndRefiles|TestMergeVerifyGatePassKeepsMerge' ./cmd/agent/
```

Observed (gate disarmed — the merger never lands the branch, so nothing is
judged):

```
--- FAIL: TestMergeVerifyGatePassKeepsMerge (14.13s)
    merger_verify_e2e_test.go:158: master = 64ec94ee…, want the merged branch tip 9cf9ad2f… kept
--- FAIL: TestMergeVerifyGateFailRevertsAndRefiles (7.61s)
    merger_verify_e2e_test.go:191: a FAILING verify must revert the merge
FAIL
```

Both failures are AT the gate's own assertions (the keep and the revert), not
at fixture setup — the pre-fix behaviour is exactly "no gate runs". With the
gate armed the same two tests pass (`ok`), and the tools-layer negative cases
(`TestVerifyGateRevertRestoresRecordedTip`,
`TestVerifyGateFileReworkReferencesOldTask`) hold the same property at the
git/ledger level.
