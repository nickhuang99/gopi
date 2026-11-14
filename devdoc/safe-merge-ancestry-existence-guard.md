<!-- doc-class: historical -->
# Safe-Merge Ancestry/Existence Guard — two defects, one contract

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a0cca2-fd71-7c96-bbc0-8be0232cb41a` — *"scripts/safe-merge.sh is
ancestry- and existence-blind — it prints ✅ exit 0 for a branch ref that does
not exist, so a merge row is dispatched onto a ghost branch"*.
**Branch**: `fix/safemerge-ancestry-existence`.

## The two defects, as measured

1. **Existence-blind.** `scripts/safe-merge.sh` (GOPI.md Rule 9's pre-merge
   guard) ran its four checks `[a]`–`[d]` — archive tag, name blacklist,
   worktree occupancy, "already merged?" — and on a branch name that resolves
   to **no ref at all** printed the four ✅ lines and exited **0**, i.e.
   *certified safe to merge*. Its Go mirror `mergeExecSafeMergeBlock`
   (`internal/tools/task_merge_executor.go`, executor step ③) had the same
   blindness: an unresolvable branch produced either a bare pass or the
   misleading ledger reason *"merge conflict"*.
2. **Ancestry-blind, and dispatch-blind.** A branch with an **unrelated
   history** (an orphan root) was likewise certified. And because the guard
   runs *after* a session was claimed, the failure surfaced only once an agent
   had already been paid for: measured row `01a0c614-00aa-7e17-8be5-b37ef5a0fcec`
   (`merge feat/e2e-orphan-probe1 into master`) — an e2e fixture pushed the
   branch, the audit filed the row, the fixture's cleanup deleted the ref, and
   the pending row stayed in the queue waiting to be dispatched onto nothing.

## What was built

### The shared contract (one vocabulary, two languages)

`scripts/lib/safe-merge-contract.sh` (shell half) and
`internal/tools/safe_merge_contract.go` (Go half) hold the **same** refusal
kinds (`missing-ref`, `unrelated-history`, plus the pre-existing
`archive-tag` / `probe-failed`), the same rendered sentences, and the same
probe-exit table. Cross-language parity is not a promise: it is a **gate**
(`TestSafeMergeContractShellVocabularyParity`), with its own drift red-proof
(`TestSafeMergeContractParityDetectsDrift`) and a pairing gate
(`TestSafeMergeGuardPairingIsDocumented`) that requires each half to *name* the
other by path and by function, so a reader landing on one half finds the other
and the rule that they move together.

Why a shared library at all (Rule 14): the shell script and the Go executor
must refuse with the same words, because the ledger `reason` column is what an
operator reads and what a later sweep matches on. Two independently typed
vocabularies drift the day one is edited.

### Deliverable 1–2: `scripts/safe-merge.sh`

Checks `[a]`–`[d]` are **byte-unchanged**; the change is purely additive:

- `[e]` **ref existence** — `git rev-parse --verify --quiet refs/heads/<b>^{commit}`
  (then `refs/remotes/<b>`, then the bare name). Exit 1 **with empty output** is
  a *measured absence* → ❌ `missing-ref`, exit 1. Any other non-zero exit, or
  output on an unexpected exit, is `probe-failed` (exit 1) — never a silent
  pass.
- `[f]` **common ancestor** — `git merge-base --all master <b>`. The MEASURED
  orphan signature is the **pair** (exit 1, empty output) → ❌ `unrelated-history`,
  exit 1. Exit 0 with empty output is *neither* a pass *nor* a named refusal, so
  it refuses as `probe-failed`.

  **This check first used `merge-base --is-ancestor`, and that was a real bug**:
  "one is an ancestor of the other" is a STRICTER question than "they share
  history", and it answers 1 in **both** directions for the ordinary case — two
  branches descending from a common base, each with its own commit. That is
  every real feature branch, so the guard refused exactly the merges it exists to
  let through (measured 2026-09-23: it refused `TestMergeExecConflict`'s plain
  `master`/`feat/e2e-merge` fixture, whose branch is one commit on top of
  master's tip). The parity gate now pins the probe **command** and the refusal
  **exit**, not only the rendered sentences, so the two halves cannot come to
  describe different questions again.

The Rule 15 split matters and is the reason `[e]` is not one `if`: *"git could
not answer"* is a different fact from *"the ref is gone"*, and only the second
is a refusal on the merits. Both refuse — but they refuse **under different
kinds**, so the operator sees which one happened.

### Deliverable 3: the Go mirror, `mergeExecSafeMergeBlock`

The function now **returns an `error`** instead of a discardable string, so the
executor's step ③ cannot lose the verdict. `[e]` reuses the package's existing
ref probe `repoSweepRefPresent` (Rule 14 — no second `rev-parse` reader);
`[f]` goes through `mergeExecAncestryProbe`, which returns the exit status as a
three-valued answer (`safeMergeAncestryPass` / `safeMergeAncestryRefusal`, plus
the unmeasured case) extracted with `errors.As`. Step ③ branches on
`guardErr != nil`, so the ledger reason is now
`safe-merge [missing-ref]: 分支不存在 … refs/heads/<b>^{commit}` instead of
"merge conflict" — the reason names the *measured* fact.

### Deliverable 4–5: refuse at claim time, not after the session

`internal/tools/task_merge_backlog_dispatch_guard.go`, called from the shared
claim funnel `TaskNextFiltered`. A **pending merge-backlog family row** whose
branch ref is **measurably absent** (all three spellings probed, exit 1 with
empty output) is **cancelled by name** with the probe evidence baked into the
`reason` column, and a `merge-backlog-ghost-ref` notify event fires; the row is
never handed to a solver.

Lane discipline (this is where the guard could have gone wrong, so it is
explicit):

- `taskRowClaimable` was **NOT widened**. It stays a pure predicate over row
  fields (Rule 14, no I/O). Whether a *branch* resolves is a fact about a
  *repository*, not about a row — folding it in would make the pending view's
  `[queue-skip]` marker depend on the ledger reader's cwd.
- **Unmeasured is never a refusal.** A repo the row does not name, or a probe
  that could not answer, leaves the row alone (Rule 15).
- **A landed branch is never a ghost.** The merger deletes a branch precisely
  when the merge succeeded, so the guard defers to the D2 terminal guard's own
  landing probe rather than duplicating the ladder.

## Verification (all runs on this branch, 2026-09-23)

| surface | command | observed |
|---|---|---|
| `[e]` shell | `MASTER=master bash scripts/safe-merge.sh feat/e2e-orphan-probe1` | rc=1, `拒绝种类: missing-ref`, ✅`[a]`-`[d]` intact |
| `[f]` shell | orphan-root fixture (real second `git init` + fetch) | rc=1, `拒绝种类: unrelated-history`, "无共同历史" |
| control | clean related branch | rc=0, all ✅ `[a]`–`[f]` present |
| pre-fix proof | `TestSafeMergePreFixScriptCertifiesTheGhost` | the pre-fix script (both sections reverted to a symlinked-contract copy) certifies the ghost **rc 0 "可安全 merge"** — the defect, reproduced |
| Go mirror | `TestMergeExecSafeMergeMissingRef` / `…UnrelatedHistory` / `…PassesForARelatedBranch` / `…ExistingRefusalsIntact` | executor step ③ renders the named reason; `[a]`–`[c]` intact |
| dispatch | ghost row + real `GOPI_HOME=<scratch> gopi worker --once` | row `01a0ccda-…` **cancelled** by name with probe evidence, `queue drained — exiting`, **no session dir created** |
| RED-proof | guard call site removed from `TaskNextFiltered` | the funnel offers the ghost row again (the guard is what refuses it) |

Full build + smoke output: `tasks/01a0cca2-report.md` (session report) and the
`## Verification` block of the task report.

## Not done / deferred (honest)

- The `[e]`/`[f]` checks are *pre-merge* guards. A ref that exists when the
  guard runs and is deleted between guard and `git merge` is still a race; the
  window is now milliseconds instead of "one whole solver session", and closing
  it entirely needs the merge itself to be the probe (`git merge` already
  refuses by name, which is what now reaches the ledger as the reason).
- The dispatch guard governs the **merge-backlog family** only. A hand-filed
  row that merely *mentions* a branch (no family tag, no audit verb) is not
  governed — expanding the lane means deciding what "names a branch" means for
  arbitrary prose, which would make the guard guess.
