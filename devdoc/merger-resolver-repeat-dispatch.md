<!-- doc-class: historical -->
# Merger repeat-dispatch of resolvers — root cause + fix

Bug: **01a089b7** (LLM-filed, source=llm, 2026-09-10) — *"merger repeatedly
dispatches resolvers for the SAME branch — duplicate work + zombie rows"*.
Branch: `feat/merger-resolver-dedup`.

## Symptom (as filed)

`feat/safe-merge-guard-answer` got **two sequential resolvers**. The first
resolved the single `devdoc/safe-merge-guard-answer.md` add/add conflict
(commit `19a71da`, 12:51:15) with full verification; the merger reportedly did
not recognise the resolved state and spawned a **second** resolver
(pid 3128439, `/tmp/gopi-merge-resolve-1827224490/wt`) that re-did the same
conflict for 15+ minutes. The same pattern hit `feat/hippocampus-host`
(resolver 20-min timeout → retry → orphan `in_progress` rows).

## CORRECTION (2026-09-10, later evidence) — there was only ONE resolver

The filing above is **wrong on the second resolver and on Defect C**, and the
corrected reading makes the fix narrower and the cause clearer. Evidence:

- `pid 3128439` is **the resolve child of the first resolver**, not a second
  merger. Ledger row `01a089a7-26cb…` (the `[auto] … conflict resolver` row
  that pid stamped at 12:50:28) carries `session_id =
  98d7ba16-8f83-424c-aa0d-d354995b2184` — the session whose worktree is
  `/tmp/gopi-merge-resolve-1827224490/wt` and whose own log shows
  **`git commit … 19a71da resolve merge feat/safe-merge-guard-answer into
  master`** at 12:51:15. It is that child's *own* `TaskStampSelfClaim`
  (`claimed_by = hostname:os.Getpid()`) that wrote `peerbox:3128439`, three
  seconds after the merger pid 3128385 stamped the parent row. One merger, one
  resolver, one branch.
- **No two merger processes.** The child was spawned at 12:50:27 and the
  `[auto]` row is its *auto-registration* row (Defect A) — the same
  auto-registration that the fix turns off. `~/.gopi/memory.db` shows a single
  merger pid (3128385) for this branch, and that process is still alive and is
  the only merger running.
- **Defect C is therefore refuted** (bug `01a089ce` should be closed as
  invalid): it was inferred from a pid that belongs to the resolver child, so
  the "two merger processes alive at once" claim has no supporting evidence.

What actually happened, per the resolver's own session log: it committed the
resolution at 12:51:15, then started `go test ./...` as a **background job**
and spent 12:51–12:59 in `sleep 115` polling loops (the package's known
pre-existing full-suite hang), then at 13:03 found `panic: test timed out
after 10m0s` in `internal/tools`, re-verified, and re-committed the merge at
13:04:01 (`6761cad`) — the merger's 20-minute cap fired at 13:08 and killed
the by-then-finished resolver, which the merger recorded as *"child exited
signal: killed (branch left untouched, retried next tick)"*.

So the bug is **not** "the merger ignored a resolution commit" in the sense of
double-dispatch: it is that (A) an error return from the resolver was trusted
as proof that **nothing landed**, so the branch was re-resolved from scratch
even though the resolution was already committed, and (B) the briefing itself
told the resolver to verify with the **full suite** before committing — the
resolver died waiting on that suite, which is precisely the ordering that
produces a committed-then-killed resolver. Both are fixed on this branch;
Defect C is withdrawn.

## Investigation — what the timeline actually says

Pull the ledger, not the narrative (the evidence below is from
`~/.gopi/memory.db` + the session JSONLs, both preserved).

| time | event |
|---|---|
| 12:44:14 | row `t-1789015454` (pid **2209903**) — hippocampus-host, cancelled ("branch archived") |
| 12:44:16 | resolver session starts, `wt=/tmp/gopi-merge-resolve-403816628/wt` (hippocampus-host) |
| **12:50:24** | **merger pid 3128385 starts** (`task merger --every 5m`) — the freshly restarted merger |
| 12:50:25 | row `01a089a7-1e32…` (pid **3128385**) in_progress — safe-merge-guard-answer ⬅ the merger's OWN row |
| 12:50:27 | resolver session `98d7ba16` starts, `wt=/tmp/gopi-merge-resolve-1827224490/wt` ⬅ the ONE resolver |
| 12:50:28 | row `01a089a7-26cb…` (pid **3128439**, session_id **98d7ba16**) — the resolver child's own auto-registration row (Defect A) |
| 12:51:15 | **`19a71da` committed** by session `98d7ba16` — the resolution (DONE) |
| 12:51:11–12:59 | the same session polls `go test ./...` (background job + `sleep 115` loops) — the full-suite hang |
| 13:03:11 | `panic: test timed out after 10m0s` in `internal/tools` (the pre-existing hang) |
| 13:04:01 | `6761cad` — the same session re-commits after master advanced |
| 13:08:38 | `d75818e` — the branch is merged (PA) |
| 13:08:40 | row `…-1e32` → **failed**, *"child exited signal: killed (branch left untouched, retried next tick)"* — the 20-min cap fired on an already-finished resolve |

What the records actually show:

1. **One merger (3128385), one resolver (session 98d7ba16), one branch.** The
   `[auto]` row `01a089a7-26cb…` at 12:50:28 is the resolver **child's own**
   ledger row, minted by its auto-registration hook: its `session_id` is
   `98d7ba16` — the resolver session itself, the one that committed `19a71da`.
   `TaskStampSelfClaim` writes `claimed_by = hostname:os.Getpid()`, so
   `peerbox:3128439` is *that child's* pid. Nothing else stamped it.
2. **There was no second resolver.** The PA saw the `[auto]` row and cancelled
   it ("duplicate resolver: first resolver already committed 19a71da … this run
   redundant") — the right call on the row, and the origin of the "two
   resolvers" narrative, but the row was never a second resolve.
3. **Defect C (two merger processes) is refuted** and must not be carried
   forward: it rested entirely on reading 3128439 as a merger. The ledger shows
   no second merger for this repo; bug `01a089ce` should be closed invalid.

So two real defects produce the reported waste:

1. **The resolver's error return was read as "nothing landed."** The resolver
   committed at 12:51:15, then burned 12:51→13:08 verifying with the full
   suite; the merger's 20-minute cap killed it at 13:08 and recorded *"branch
   left untouched, retried next tick"* — discarding a finished resolution, so
   the branch was resolved again from scratch. Git was the arbiter all along:
   the branch tip had advanced. (Fix 3: `repoSweepBranchTipAdvanced`.)
2. **The briefing made that outcome likely by asking for the full suite before
   the commit.** The resolver was told to verify with the project's test
   commands and that acceptance included "the test suite passes"; it committed
   only after starting that (hanging) suite and kept polling it. COMMIT-FIRST +
   a bounded check removes the window in which a finished resolve can be
   killed. (Briefing rewrite + `repo_resolve_briefing_test.go`.)

The zombie rows are Defect A below, which is also why the PA's "duplicate
resolver" reading was possible at all.

### Defect A — the auto-registration kill switch never worked (`GOPI_AUTO_TASK_REGISTER=1`)

`internal/agent/taskregister.go` defines the switch as **default-ON**:

```go
func autoTaskRegisterEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(autoTaskRegisterEnv))) {
	case "0", "off", "no", "false", "disabled":
		return false
	}
	return true
}
```

`cmd/agent/merge_resolve_child.go` (since its first commit `bbc7503`,
2026-09-05 — `git_blame` confirms, so it never worked) wrote the **opposite**:

```go
child.Env = append(os.Environ(),
	tools.AutoRegisterOffEnv+"=1",   // "1" ⇒ autoTaskRegisterEnabled() == TRUE
	...
)
```

Consequence, exactly as predicted by requirement #1 ("merger never submits,
the queue gets **zero** new merge-conflict rows"): **every resolve child
minted its own ledger row** — the `[auto] You are the merger's conflict
resolver for branch "…"` rows, created in_progress and never closed as a
duplicate (the merger only ever finishes the row *it* created). The ledger
shows one such row per resolver **attempt**: `01a0898a` (12:19, pid 3086757),
`01a089a1` (12:44, pid 3116997), `01a089a7-26cb` (12:50, pid 3128439), plus
`01a0882c` (05:56, closed as *"zombie: resolver claimer pid … dead"*). That is
the "zombie rows" half of the report.

Amplifier: the shared `append(os.Environ(), …)` pattern cannot override an
inherited key either — `os/exec` keeps the **first** occurrence of a
duplicated key, so an inherited `GOPI_AUTO_TASK_REGISTER=1` would win over the
appended value even if it were spelled correctly.

### Defect B — nothing recorded "a resolver is already in flight for branch X"

`internal/tools/repo_sweep.go`'s resolve gate only consulted
`resolveRowHasOpenRework` (branch-rework rows are open) and
`resolveRowFailedCount` (≥ N failures). Neither asks *"is a merger-owned
resolve row for this branch still in_progress with a live owner?"*, so with
two merger processes (or a stuck sweep) each one happily spawns its own
resolver and mints its own row — the mechanism behind the "two sequential
resolvers" observation and the wasted 15 minutes of re-doing an
already-committed resolution.

Note the double standard that already existed: `MergerResolveRecoverScan`
**does** honour liveness ("a live pid → the resolve is genuinely still in
flight … never false-fail a slow live resolve"), but the *dispatch* path did
not.

### Defect C (WITHDRAWN — refuted by later evidence) — no second merger process

The earlier reading of this section claimed "two merger processes were alive at
once" from a pid (3128439) that stamped rows 3 s after the merger. **That pid
is the resolver child's own pid** (`TaskStampSelfClaim` = `os.Getpid()`) — see
the CORRECTION section at the top of this document. There is no second merger
in the ledger for this repo: one merger (3128385, still running) spawned one
resolver (session 98d7ba16), and the extra rows came from the resolver child's
auto-registration (Defect A), not from a second daemon. Bug `01a089ce` was
filed from this mistaken reading and should be **closed as invalid**; the
`acquireServeLock()` singleton question remains worth a direct test but has no
supporting production incident here.

## Fix (minimal, works within the existing architecture)

**A. `cmd/agent/merge_resolve_child.go`**
- New named constant `mergeResolveAutoRegisterOff = "0"` (Rule 1) with a
  comment recording the trap.
- New `mergeResolveChildEnv(branch)` builds the child env by **replacing**
  `GOPI_AUTO_TASK_REGISTER` / `GOPI_MERGE_RESOLVE_BRANCH` in place instead of
  blind-appending, closing the duplicate-key hole.

**B. `internal/tools/repo_sweep.go` + `repo_resolve_row.go`**
- New `resolveRowHasLiveResolve(repo, branch)`: true when a merger-owned
  (`merge-conflict,auto-spawn`) resolve row for that repo+branch is
  `in_progress` **and** its owner is a live pid — or the owner cannot be
  trusted/parsed (unknown ≠ dead, conservative). A **dead** owner returns
  false, so a crashed merger never deadlocks a branch (the recovery scan
  closes the row and the branch returns to the dispatch gate next tick).
- New first case in the sweep's resolve `switch`: skip + count
  `ResolveInFlight` + emit a `resolve-in-flight` outcome instead of spawning.
- `RepoSweepResult.ResolveInFlight` + the merger's summary line
  (`resolve-in-flight=N`).

## Verification

- `go build ./...` — OK; `go vet ./internal/tools ./cmd/agent ./internal/agent` — clean.
- New tests (all green):
  - `internal/agent`: `TestAutoTaskRegisterOffValueVocabulary` — pins the OFF
    vocabulary; `"1"` is **not** OFF.
  - `cmd/agent`: `TestResolveChildAutoRegisterValueIsOff` (the spawner's
    constant is OFF per the reader's own predicate) and
    `TestMergerResolveChildEnvDisablesAutoRegister` — runs the **real spawn
    path** with the stub resolver, which dumps `env` to
    `GOPI_MERGE_RESOLVE_ENV_OUT`; asserts the child's actual
    `GOPI_AUTO_TASK_REGISTER` is OFF, the role marker is the right branch, and
    the key appears **exactly once** (first-wins safety).
  - `internal/tools`: `TestMergerRepoSweep_SkipsBranchWithLiveInFlightResolve`
    (two consecutive sweeps over one branch: the in-flight one dispatches 0
    resolvers, leaves the row `in_progress` and the branch intact; once the row
    goes terminal the next sweep resolves + merges normally — the counter-case
    that proves no deadlock) and
    `TestResolveRowHasLiveResolve_DeadOwnerIsNotInFlight`.
- **Negative controls** (each fix reverted in isolation, test must fail):
  - `mergeResolveAutoRegisterOff = "1"` → both cmd tests FAIL with
    `resolve child GOPI_AUTO_TASK_REGISTER = "1" — auto-registration stays ON …`.
  - in-flight guard short-circuited to `false` → the sweep test FAILS with
    `sweep dispatched 1 resolver(s) for a branch with a live in-flight resolve, want 0`.
- Pre-existing suite state (honest): the targeted runs above are the gate.
  `go test ./internal/tools/ ./internal/agent/` as a **whole package** does not
  complete — `internal/tools` has a long-documented pre-existing full-suite
  hang (bug `t-1788361069`; memory #1296/#1059/#1332: suite-order interaction
  leaves a shared `sql.DB` pool opener blocked; the same package block is
  aggravated by concurrent `memory` tool activity). It produced **zero output
  in 11+ minutes** here, so that run was killed and the fix was exonerated
  with targeted runs instead — which is exactly what those memories
  prescribe. `go test ./internal/tools/ -run
  'TestMergerRepoSweep|TestResolveRow|TestTaskNextFiltered_NeverDispatches|TestMergeConflictTag'`
  → `ok 36.3s`; `internal/agent` and `cmd/agent` targeted runs → `ok`.

## Re-verification (independent, post-commit)

Both negative controls were re-run against the committed tree, each fix
reverted **in isolation** and restored afterwards (`git status --short` empty
before and after):

| control | command | result |
|---|---|---|
| `mergeResolveAutoRegisterOff = "1"` | `go test ./cmd/agent/ -run 'AutoRegister\|ResolveChild\|ChildEnv' -count=1` | **FAIL** — `mergeResolveAutoRegisterOff = "1" — the auto-registration reader treats that as ON …` + `resolve child GOPI_AUTO_TASK_REGISTER = "1" — auto-registration stays ON …` |
| in-flight guard short-circuited (`case false && resolveRowHasLiveResolve(...)`) | `go test ./internal/tools/ -run TestMergerRepoSweep_SkipsBranchWithLiveInFlightResolve` | **FAIL** — `sweep dispatched 1 resolver(s) for a branch with a live in-flight resolve, want 0` |

Restored tree: `cmd/agent` `ok 6.6s`, `internal/agent` `ok 14.1s`, `internal/tools`
(targeted) `ok 4.8s`; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK;
`go vet ./internal/tools ./cmd/agent ./internal/agent` clean. Full
`go test ./internal/agent/ -count=1 -timeout 15m` → `ok 156.0s`.

## Not done

- **Defect C (two merger processes / serve-lock enforcement)** is **withdrawn** —
  later evidence refuted it (pid 3128439 was the resolver child's own pid, not
  a second merger; see the CORRECTION section). Bug `01a089ce` has no
  supporting incident and should be closed as invalid. `acquireServeLock()`'s
  singleton guarantee is still worth a direct test, but as a hardening item,
  not as this bug's cause.
- The `feat/hippocampus-host` timeout-then-retry path was **not** changed: on
  the evidence above, retrying after a genuinely dead child is correct
  behaviour; what was missing is only the in-flight bookkeeping (A/B above).
- Not addressed: a resolver is killed at the 20-min cap even if it is making
  progress (`mergeResolveChildWaitTimeout`); no liveness/progress signal.

## Defect D (found while re-verifying the merge state) — a LANDED resolve was recorded as a FAILURE, feeding the escalation gates

The landed-resolve arbitration (fix 3) is correct in its dispatch decision —
it keeps the committed resolution and refuses to spawn a second resolver. Its
**row bookkeeping** was wrong in the other direction: the landed row was
finished `taskStatusFailed` (rationale at the time: the resolver *process* did
not exit cleanly). But `resolveRowFailedCount` — the persistent per-branch
counter the sweep's escalation gates read — is implemented as exactly
"merger-owned resolve rows with status = `failed`":

```
SELECT status, tags, deliverable FROM tasks WHERE status = 'failed'
  → count rows whose tags are merge-conflict+auto-spawn and whose deliverable
    is (repo,branch)
```

so **every landed resolve was counted as a resolve failure**. Three landed
resolves for one branch push the counter to
`resolveRowMaxFailedBeforeRework = 3`, which is precisely the trigger for:

- `resolveRowSubmitRework` (submit a branch-rework task for a developer), and
- `repoSweepArchiveBranch` (tag `archive/<branch>` and drop the branch from the
  merge queue permanently, once a rework has also concluded).

A branch that keeps regaining a conflict as master moves (each time faithfully
resolved and committed — the healthy path, exactly what fix 3 preserves) would
therefore be escalated and finally **archived as a "decided conflict loser"
while every one of its resolves had succeeded**. That is the same bureaucracy
loop the epic is about (wasted work, stalled pipeline), entered from the
SUCCESS side — and it is the failure mode that turns "keep the resolution" into
"the branch gets thrown away".

**Fix**: the landed row is finished `taskStatusCompleted` (the resolution
SUCCEEDED — the branch carries it) with the unchanged, distinct
`resolveRowReasonResolveLanded` reason, so the audit trail still distinguishes
"landed but the process died" from "landed and merged in the same tick"
(`resolveRowReasonResolved`). Only rows from a resolver that failed **without**
landing keep status `failed` and feed the counter. The constant's doc comment
records the trap.

**Regression tests** (`internal/tools/repo_resolve_landed_escalation_test.go`):

- `TestMergerRepoSweep_LandedResolvesNeverEscalate` — drives three real
  landed-resolve cycles through `MergerRepoSweep` (re-conflicting the branch
  between cycles) and asserts `ReworkSubmitted == 0`, `Archived == 0` and
  `resolveRowFailedCount == 0`. Pre-fix this fails with
  `resolveRowFailedCount = 3 after 3 LANDED resolves, want 0`.
- `TestMergerRepoSweep_LandedResolveRowCountsAsSuccess` — the row-level pin
  (status `completed`, landed reason, counter 0) **plus an in-test negative
  control**: a resolver that fails without committing still yields
  `resolveRowFailedCount == 1`, so the zero-counter assertion cannot be
  vacuous.

**Negative control** (fix reverted in isolation, `taskStatusCompleted` →
`taskStatusFailed`, restored afterwards): both tests FAIL with
`resolve row … status = "failed", want "completed"` and
`resolveRowFailedCount = 3 after 3 LANDED resolves, want 0`; restored tree
green (`internal/tools -run 'LandedResolve|LandedResolves|Rework|Briefing|InFlight'`
→ `ok 27.0s`, `go build ./...` OK, `go vet` clean).

The pre-existing threshold tests in `repo_rework_test.go` (which use a resolver
that never commits) remain the positive control for the counter itself and were
re-run green: the counter still sees real failures.
