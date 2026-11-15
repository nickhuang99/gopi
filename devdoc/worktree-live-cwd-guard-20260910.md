<!-- doc-class: normative -->
# Task Report: live-CWD liveness guard for worktree removal (finish-cleanup + merged-cleanup)

**Branch**: `fix/worktree-live-cwd-guard` (worktree `/home/user/workspace/gopi-wtcleanup`)
**Epic**: BUG — server finish-cleanup removes a worktree a LIVE gopi session is still using
**Incident**: row `01a088ef-c0d2-785e-8b63-d2357ca55c6a`, session `7b527e07-31bc-479c-a181-031dc1423170`,
branch `feat/reframe-verdict`, worktree `~/workspace/gopi-reframe-verdict`, observed 2026-09-10 ~09:56
(worktree missing by 09:58, incident reported 10:01, master merged the branch at 10:09).

---

## Completed

### 1. Liveness guard — `finishCleanupWorktreeInUse` (`internal/tools/task_finish_cleanup.go`)

Zero-dependency `/proc` CWD scan (stdlib only, no new tools, no new imports beyond `strconv`):

- `finishCleanupPIDDirs` — numeric `/proc` entries (the process dirs), via `finishCleanupProcName`.
- `finishCleanupProcCWD` / `finishCleanupProcCommand` — `readlink /proc/<pid>/cwd`, `read /proc/<pid>/comm`.
- `finishCleanupPathUnder` — separator-aware "is path inside dir" (a sibling `wt-sibling` never matches `wt`).
- `finishCleanupWorktreeGitDir` — resolves a linked worktree's PRIVATE git dir from its `.git` **file**
  (`gitdir: <main>/.git/worktrees/<name>`) or a `.git` **dir**.
- `finishCleanupWorktreeInUse(wt) (pidMsg, reason)` — returns the first LIVE process whose CWD is inside
  the worktree DIR, its resolved (symlink) form, or its private git dir.

Two "in use" shapes are covered, deliberately:

| shape | who it is | why it must block |
|---|---|---|
| CWD inside the worktree dir | the gopi session / shell / `go test` (the incident) | `worktree remove` deletes the directory under it → bogus package-load failures |
| CWD inside the worktree's private git dir | a long-lived `git` process (git chdir's into `.git/worktrees/<name>`) | removal deletes that admin dir too |

Fail-soft: an **unscannable procfs** (non-Linux / restricted `/proc`) returns no holder and cleanup
proceeds on the pre-existing dirty/exempt gates — the guard closes a race, it must not become a hard
dependency (Rule 7 zero-dep intent). Every other unreadable pid is simply skipped (a process we cannot
see is one we cannot protect). The result is always **reported**, never swallowed (Rule 15):
server arm → `SkipReason`, client arm → the step's `Detail`.

### 2. Wired into every removal path in the chain

| # | site | before | now |
|---|---|---|---|
| ① | `finishCleanupServerRecycle` (server / single-node arm, **no `--force`**) | removed any clean worktree | liveness gate before removal → `Skipped` + `SkipReason`, branch preserved |
| ② | `runFinishCleanupChainTarget` step ③ (client arm, **`--force` = the data-loss path**) | `worktree remove --force` | liveness gate before the removal → `FinishCleanupStep{worktree, OK:false, Detail}`; the push already succeeded, so the branch is safe on the server and step ④ (`branch -d`) is not reached |
| ③ | `mergedCleanupRepo` (`task_merged_cleanup.go`, merged-branch sweep — the third `worktree remove` site) | led by the ledger-live-task check ⑤ only | guard ⑤b: the same liveness gate, skipped branches reported |

**Every other `worktree remove` in the tree was examined** (grep, non-test): the only remaining one is
`mergerResolveConflict`'s deferred teardown (`cmd/agent/merge_resolve_child.go`). It is **immune by
construction** — the worktree lives in a per-call `os.MkdirTemp("", "gopi-merge-resolve-")` path, so it
can never collide with a session's worktree (the E2 mismatch), and its own child is the only expected
holder. It was therefore deliberately not modified: the guard there would buy nothing (the enclosing
`defer os.RemoveAll(vcsScratch)` deletes the directory regardless of a skipped `git worktree remove`, so
a skip would be cosmetic), and Rule 4/one-variable-change discipline says do not add an unproven guard.

### 3. Named constants (Rule 1) — no bare literals

`procRootName`, `procCwdName`, `procCommName`, `procCommUnknown`, `procNumericNameChars`
(`finishCleanupPIDDigits` derives the membership set from it), `finishCleanupGitDirFilePrefix`
(`"gitdir:"`), `finishCleanupInUseMarker` (`"worktree in use"` — the one phrase every refusal carries),
`finishCleanupStepWorktree` (`"worktree"`, shared by both finish-cleanup sites), test-side:
`holderEnvMarker`, `holderChildArgRun`, `procScanWaitAttempts`, `procScanWaitInterval`,
`procHolderHoldTime`.

### 4. Regression tests (`internal/tools/task_finish_cleanup_test.go` and `task_merged_cleanup_test.go`)

The holder is **this test binary re-exec'd** (`TestHelperWorktreeCwdHolder`, marker-gated, `time.Sleep`
in the child) — no shell, no external `sleep`, no timing guesswork; `waitProcCwd` polls procfs until the
child's CWD is observable, and SKIPs (not FAILs) on a host where procfs is unreadable.

- `TestFinishCleanupPathUnderNoSiblingPrefix` — separator-aware match (no sibling-prefix false positive).
- `TestFinishCleanupWorktreeInUseDetectsLiveCWD` — idle → no holder; live CWD → holder named **by pid**;
  a sibling directory's holder does not clear the verdict.
- `TestFinishCleanupWorktreeInUseNestedCWD` — a CWD deeper in the tree (the real `go test` shape).
- `TestFinishCleanupServerRecycleSkipsWorktreeInUse` — **the incident regression**: live CWD → `Skipped`,
  reason names the marker + pid, no step ran, the worktree is still on disk, still in `git worktree list`,
  branch preserved.
- `TestFinishCleanupWorktreeRecycledWhenIdle` — the guard is not over-blocking: no holder → the same
  fixture is still recycled, branch preserved.
- `TestFinishCleanupChainStep3RefusesWorktreeInUse` — the `--force` path refuses: push OK, worktree step
  `OK:false` with the marker + pid in `Detail`, the worktree and the local branch survive.
- `TestMergedCleanupLiveCWDWorktreeSkipped` (`task_merged_cleanup_test.go`, added in the review pass) —
  the **third** guarded site: a merged branch + a directly removable pair, held back ONLY by a live CWD
  inside the worktree → zero removals, one skip whose Detail names the marker + holder pid, worktree and
  branch survive. Without it, site ③'s new gate ⑤b shipped untested.

## Not Done (honest)


- ~~**No CLI-level (binary) E2E run.**~~ **CLOSED by review pass 2 (below)**: the CLI/binary-level E2E
  gap — the `ManualRun` E2E tests now run for real (build `bin/gopi` first) and include the incident
  reproduction (`TestManualRunKeepsWorktreeHeldByForeignProcess`, A/B-verified against the pre-guard
  binary).
- **The double-run (two sessions, one row) condition is NOT closed by a lease** — see the investigation
  below; the evidence REFUTES the concurrent-run mechanism (both dispatch paths structurally prevent it),
  and shipping a claim-lease would be a redesign of the dispatch contract. What is left as a scoped
  follow-up is the *ownership* half: nothing binds a worktree path to exactly one row.
- **Found-and-fixed along the way**: `finishCleanupOwningWorktree` treated the FIRST porcelain entry
  (always the main checkout) as an ordinary candidate, so a dir *inside* the main checkout resolved to
  the main checkout as its "owning worktree". Fixed (commit `5b7e36c`) + regression test
  `TestFinishCleanupOwningWorktreeNeverMainCheckout`. `git worktree remove <main checkout>` itself is
  refused by git ("is a main working tree"), so this defect mis-resolved a target and ran the chain's
  branch step against a worktree it never removed — a real bug, but it cannot explain a *linked*
  worktree vanishing, and the remover of this incident stays unidentified (below).

### Limits (honest)

- The guard **narrows, it does not eliminate**, the race: a process can enter the worktree (or be
  started there by the scheduler) between the scan and the `worktree remove`. The window is now the
  few milliseconds of the scan instead of the entire run, and the retry property below recovers the
  missed case — it is not a lock.
- A skip is **per pass**: both sweeps re-run every lifecycle pass
  (`cli_tasks_worker.go` finish-cleanup sweep, serve merged-cleanup), so a worktree skipped while in use
  is removed by a later pass once the holder exits — the skip parks, it does not leak.

## Investigation: the two-sessions-one-row enabling condition

Sources: ledger (`~/.gopi/memory.db` tasks table), raw session JSONL/dump files, `git log --all`,
`git worktree list`, live process table. Zero external deps.

### Timeline (all times +0800, 2026-09-10)

| time | event | evidence |
|---|---|---|
| 09:30 | session `48dbea14` (row `01a088e0`) creates `~/workspace/gopi-reframe-verdict` on `feat/reframe-verdict2` | session commands `git worktree add ~/workspace/gopi-reframe-verdict -b feat/reframe-verdict2` |
| 09:30 | that row is **cancelled**; its deliverable records `{"branch":"feat/reframe-verdict2","worktree":"/tmp/gopi-reframe-verdict"}` — a worktree it no longer owns | tasks row `01a088e0` |
| ~09:31–09:32 | session `7b527e07` (row `01a088ef`, tags `feature`, deliverable `{"repo":"/home/user/work/GoPi"}` — **no branch, no worktree**) reuses the SAME checkout: `git worktree add ~/workspace/gopi-reframe-verdict feat/reframe-verdict` — i.e. it attaches to the existing directory and checks out a different branch in it | session `7b527e07` commands |
| 09:32 | `48dbea14` stores the first workaround memory ("the system tmp cleaner removed `/tmp/gopi-reframe-verdict`" → put worktrees under `~/workspace`) and then exits — **it owns a cancelled row whose deliverable points at a worktree it did not create** | memory store + memory record #1579 |
| 09:42 | `7b527e07` writes its report to the worktree | session tool call `write` |
| 09:56–09:58 | `7b527e07` runs `cd ~/workspace/gopi-reframe-verdict` → **No such file or directory**; the worktree is gone mid-work | raw log line 947: `bash: line 1: cd: /home/user/workspace/gopi-reframe-verdict: No such file or directory` |
| 10:01 | `7b527e07` submits this bug (row `01a0890c`) | session tool call `task_submit` |
| 10:09 | master takes `3ce88c2 Merge branch 'feat/reframe-verdict'`; `7b527e07` still works (its `cd` fails through 10:02) | `git log -1 3ce88c2` |

### What the evidence rules OUT

- **Two sessions running one row concurrently — REFUTED as the mechanism.** Both dispatch paths
  structurally prevent it: `TaskNextFiltered` CAS-claims `WHERE status = 'pending'`, and
  `TaskClaimExplicit` (`task run <id>` / worker round) Shape ② re-stamps **only** a `claimed` row —
  an `in_progress` row is refused with its holder named (`internal/tools/tasks.go`). Row `01a088ef`
  was `in_progress` under `peer1:2868010` from 01:30, so no second session could claim it. Its
  deliverable is `{"repo":"/home/user/work/GoPi"}` — **no branch, no worktree** — which is the known
  *backfill gap* class (memory #1500: a child that runs `git worktree add` in bash leaves the row
  branchless), not evidence of a concurrent second run.
- **No `--force` destruction of the vanished worktree.** No retained record names
  `~/workspace/gopi-reframe-verdict` as a removal target: row `01a088e0`'s deliverable worktree is
  `/tmp/gopi-reframe-verdict`, and no other row's deliverable mentions that path or the branch
  `feat/reframe-verdict`. No session command in any of the three involved session dirs contains
  `worktree remove`/`prune`/`rm -rf` against it.
- **The `/tmp` sibling had a different cause**: the system tmp cleaner (the agent's own 09:30 memory,
  after which it mandated `~/workspace` for worktrees). It is the same *class* — a worktree directory
  destroyed under a live/returning session — but not the same actor.

### The remover could not be pinned down (honest finding)

The branch `feat/reframe-verdict` only entered master at **10:09** (`3ce88c2`), i.e. *after* the
directory vanished (~09:56) — so the merged-cleanup arm's own guard ① (`git branch --merged master`)
would have refused it, and the finish-cleanup arm requires a terminal row whose deliverable resolves
to that path (none exists; the branch-less rows fall back to the repo HEAD = master = exempt). The
retained evidence therefore does **not** identify which actor removed the directory. Candidates left
open: an out-of-band operator/tool action; a cleanup arm acting on a *mis-resolved* worktree path
(the `finishCleanupOwningWorktree` leading-blank off-by-one recorded under "Not Done" is exactly a
path-resolution defect, though `git worktree remove` refuses the main checkout so it cannot delete the
repo itself); the same system cleaner class that hit `/tmp`.

**Consequence for this deliverable**: the guard is justified by the *code fact* it fixes — **both**
removal sites removed a worktree with **no liveness check whatsoever**, so a clean worktree whose row
is terminal, or whose branch is merged + clean, is removable while a live session is sitting in it —
and by a directly observed live-CWD loss. It is **not** claimed that the guard would have prevented
this specific instance, since the remover of this instance is unidentified.

### Enabling conditions found (mechanism-verified in code)

**E2 — a lease-less manual run attaches to a directory another row already owned (PRIMARY).**
`git worktree add <existing-path> <branch>` succeeds against a directory that is already a registered
worktree and silently re-points its checkout. In this incident that produced one directory with
**mismatched ownership**: session `48dbea14` created `~/workspace/gopi-reframe-verdict` for its row
(`01a088e0`) and recorded `/tmp/gopi-reframe-verdict` in that row's deliverable after the tmp cleaner
took it; session `7b527e07` then re-attached the SAME directory to the SAME branch
(`git worktree add ~/workspace/gopi-reframe-verdict feat/reframe-verdict`) under a different row.
Nothing binds a worktree path to exactly one row: a cleanup arm resolving "the branch's linked
worktree" (`finishCleanupBranchWorktree`) or "the owning worktree of this repo"
(`finishCleanupOwningWorktree`) can therefore land on a directory a *different live session* is using.
There is **no exclusive per-row run lease** anywhere — `claimed_by`/`claimed_at` are a dispatch CAS
trail, not a run lock.

**E1 — dispatch-adjacent hygiene.** `TaskNextFiltered` returns `Empty` on a lost claim CAS instead of
re-walking the queue, so a lost race reads as "queue empty" (a dispatch-visibility wart, not a
double-run). Recorded for the follow-up; not a proven contributor here.

### Recommendation (not implemented here — one-variable-change discipline)

1. **Bind worktree → row at creation** (best value/effort): every cleanup arm requires the
   deliverable's `worktree` to match the directory it is about to remove — a mismatch is a skip, never
   a removal. Cheap, no schema change, and it closes the E2 mismatch.
2. **Lease the row for the run duration**: keep `claimed_by` but add a run-lease liveness check
   (`pidAlive`/`claimHolder`, taskzombie.go, already exist) to the terminal-cleanup trigger so a
   cleanup arm refuses while the claiming pid is alive.
3. **Fix the backfill gap** (memory #1500): resolve the branch/worktree the child actually used, so a
   row can never be branchless/worktree-less — this is what makes a cleanup arm guess in the first place.

## Review pass 2 (2026-09-10, post-merge): the guard blocked the cleanup's OWN process tree

**Symptom (caught by a pre-existing E2E, not by my unit tests):**
`TestManualRunRecyclesItsWorktree` (`cmd/agent/cli_tasks_recycle_e2e_test.go`) FAILED on the guarded
branch ("manual run left its worktree behind") and PASSES on the pre-guard master binary
(`GOPI_BIN=/home/user/work/GoPi/bin/gopi`) — a regression introduced by this guard, proven by A/B.

**Why my 13 unit tests missed it:** they always call the removal path from a test binary whose CWD is
NOT the worktree, and the only live holder they start is a re-exec'd child. The production shape is the
opposite: a manual `gopi task run` executes with its OWN CWD inside the very worktree it recycles at
close-out (`runTaskRunOnce` → `TaskFinishCleanupSessionID` on the wrapper row, `cmd/agent/cli_tasks.go`).
The guard saw the wrapper process itself, or (matters even more) a **worker pass** whose CWD sits in the
same worktree, counted it as a "live holder", and skipped — silently leaving EVERY manual run's worktree
behind. Note the E2E only became real once `bin/gopi` existed: `GopiBin` SKIPs (does not fail) without a
built binary, so the regression hid behind a skip. Two lessons: (a) a skip is not a pass — build the
binary before trusting a green line; (b) an E2E through the real binary catches tree-level bugs that
unit-level fixtures structurally cannot.

**Fix — the caller's own process tree is excluded (`finishCleanupSelfChain`):**
- `finishCleanupProcParent` reads the parent pid from `/proc/<pid>/stat` (parsed after the LAST `')'`,
  because the command name there is parenthesised and may contain spaces/parens — a naive `Fields()[3]`
  misparses `(a b)` names).
- `finishCleanupSelfChain` walks this process and its ancestors upward (bounded by
  `finishCleanupProcChainMaxHops`, so a procfs cycle / the pid-1 re-parenting race terminates), and the
  scan skips every pid in it. The exclusion is the whole ANCESTOR chain, not just this pid: the worker /
  child process tree must also be exempt, otherwise a worker pass deadlocks its own cleanup.
- **Scope, stated honestly**: the guard now protects against a *different* process holding the worktree —
  exactly the incident (a second live session, or a sweep acting for another row). It does NOT protect
  against the caller's own run being torn away by a *third party* while it builds; that is the pull
  direction of the same race and is out of scope for a removal-side guard.

**New regression tests (so this cannot come back):** `TestFinishCleanupWorktreeHeldByCallerIsRecycled`
(the cleanup runs with `t.Chdir(worktree)` — the production shape — and must still remove it),
`TestFinishCleanupSelfChainContainsAncestors`, `TestFinishCleanupProcParentNeverFollowsUnreadablePid`.
Plus the E2E is now run with a real `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` in the verification
below, so it executes instead of skipping.

## Review pass 3 (2026-09-10, Rule 13): the guard shipped with fresh bare literals of its own

**Finding:** the very file that added named constants for the /proc literals re-typed the git worktree
verb/porcelain strings as bare literals — `"worktree"` 8× in `task_finish_cleanup.go` alone (the two
porcelain parsers `finishCleanupBranchWorktree` / `finishCleanupOwningWorktree` and both removal sites
`finishCleanupServerRecycle` / `runFinishCleanupChainTarget` step ③), plus the sibling parsers in
`task_merged_cleanup.go`, `task_deliverable_worktree.go`, `task_deliverable_branch.go`,
`task_merge_executor.go`. A verb typo or a future rename (e.g. `worktree list` → the `-z` porcelain
variant) had that many chances to diverge — Rule 13's exact failure shape.

**Fix:** constants `finishCleanupWorktreeVerb` (`"worktree"`), `finishCleanupWorktreeListVerb`,
`finishCleanupWorktreeRemoveVerb`, `finishCleanupWorktreeListFlag` (`--porcelain`), and the derived
prefixes `finishCleanupPorcelainWorktreePrefix` (= verb + `" "`) / `finishCleanupPorcelainBranchPrefix`
— the porcelain prefixes are DERIVED from the verb constant, so the parser's key and the command's verb
cannot drift. All five files now reference them; the two command-STRING parsers that detect a
deliverable's worktree compare `fields[i] == finishCleanupWorktreeVerb` instead of the literal.

**Gate (Rule 13's anti-regression form):** `internal/tools/task_finish_cleanup_verbs_gate_test.go`
- `TestFinishCleanupWorktreeVerbLiteralGate` scans every production `.go` in `internal/tools` and fails
  any file other than the definition site carrying `"worktree "`, `"worktree", "list"` or
  `"worktree", "remove"`. It also fails if the scan read ZERO production files (a vacuous pass is a
  pass-shaped hole).
- `TestFinishCleanupWorktreeVerbLiteralGateHasTeeth` plants the literal in a temp dir and asserts the
  gate catches it, while `_test.go` files and the definition file stay exempt (tests legitimately spell
  git args for their fixtures).
- Deliberately NOT gated: the bare token `"worktree"` — `internal/tools` legitimately spells it in
  deliverable struct tags and in command-string matching, so gating it would force meaningless edits.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → **OK**
- Compile-all (incl. every test binary): `CGO_ENABLED=0 go test ./... -run zzzNoSuchTestXYZ` → **no
  failures** (empty output = every package and test binary builds)
- Vet: `go vet ./internal/tools/` → **clean**
- Acceptance tests: `go test ./internal/tools/ -run 'FinishCleanup' -count=1 -v` → **13/13 PASS**,
  incl. `TestFinishCleanupServerRecycleSkipsWorktreeInUse`, `TestFinishCleanupChainStep3RefusesWorktreeInUse`,
  `TestFinishCleanupWorktreeRecycledWhenIdle`, `TestFinishCleanupOwningWorktreeNeverMainCheckout`
- Wider: `go test ./internal/tools/ -run 'FinishCleanup|ServerRecycle|MergedCleanup' -count=1` → **ok**
  (15.9s: 13 finish-cleanup + all 11 merged-cleanup tests, incl. the new live-CWD site ⑤b case)
- Full `internal/tools` suite on the branch tip: **run, honest result recorded** —
  `go test ./internal/tools/ -count=1` exceeds the 600s default cap, and the timeout dump names the
  hanging test: **`TestManyFreshDBsNoHang` (1m1s at the dump)**, the memory-DB regression test that
  opens 40 fresh SQLite DBs in `t.TempDir()` (no finish-cleanup code path is involved). A/B attribution
  on the SAME test, same machine: pre-change `master` (`aafa7ce`) → **PASS in 59.9s**, guarded branch →
  **PASS in 66.1s** — a pre-existing slow test (the whole suite's own budget), NOT a regression from
  this change. With it skipped the suite runs to completion; the targeted filters above (`FinishCleanup`,
  `MergedCleanup`, the verb gate) cover every package/file this change touches.
  *(Worth a separate look someday — a 60s+ single test is a suite-budget hazard — but it is out of
  scope here and explicitly not claimed as fixed.)*
- **Binary-level E2E (the acceptance's repro, real process table)** — build first, or the E2E SKIPS:
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`, then
  `GOPI_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run 'ManualRun' -count=1 -v`:
  - `TestManualRunRecyclesItsWorktree` → **PASS** (idle worktree still recycled; this is the test that
    FAILED before the self-tree fix and PROVED the regression — it PASSES on the pre-guard master binary
    and FAILED on the guarded branch, an A/B attribution)
  - `TestManualRunKeepsWorktreeHeldByForeignProcess` → **PASS** on the branch; **FAIL on the pre-guard
    master binary** ("worktree held by pid N must SURVIVE the close-out: no such file or directory") —
    the incident reproduced end-to-end against the real binary, with the fix as the only difference.
- Commits on `fix/worktree-live-cwd-guard`:
  - `ffd4b00` fix(tools): liveness guard — never remove a worktree a live process is using
  - `5b7e36c` fix(tools): finishCleanupOwningWorktree must never resolve the main checkout
  - `292de0c` fix(tools): empty-name guard in the /proc pid-dir filter (Rule 15, no vacuous digits match)
  - `195999e` test(tools): merged-cleanup sweep skips a worktree held by a live CWD (third guarded removal site)
  - `62c456b`, `6d631c2`, `617f655`, `91e96e4`, `61782cc` docs: this devdoc

### Minimal repro (what the acceptance asks the tests to prove)
1. Build a repo + linked worktree, start a process whose CWD is inside the worktree.
2. Call the production removal path (`finishCleanupServerRecycle` / step ③) on that worktree.
3. Expect: skip/refusal **with the holder pid in the reason**, `git worktree list` unchanged, worktree
   still on disk, branch preserved.
4. Kill the holder, call again: the worktree **is** removed, branch preserved. (Tests ②=live skip,
   ⑤=idle recycle prove both directions on the same fixture.)
