<!-- doc-class: historical -->
# Task Report: the sweep refused to evict a peer checkout — a guest process may not free its host's worktrees

## Completed

- **Root cause (measured, not inferred).** `internal/tools/worktree_sweep.go`'s
  resident sweep built its repo set as the settings `repos` table **plus the repo
  the process's CWD belongs to** (`worktreeSweepRepos`). A verification/agent
  process is sandboxed in every *other* respect (`GOPI_HOME`, `GOPI_MEMORY_DB`, an
  empty `repos` table) while its **working directory is the repository under
  verification** — that is what "verify this checkout" means — and its ledger is a
  **per-task row**, so every other task's claims are structurally invisible to it.
  With the actor blind by construction, every registration-side rung can pass on a
  **peer task's** registration: clean (nothing to report), no live holder, past
  `DefaultWorktreeSweepMinAge`, inside a declared root. The ladder then removed a
  live checkout that was not its own.
- **Reproduced from a clean worktree, as a control, on unmodified source.** Two
  `go test ./cmd/agent/` runs (each ~8 min, off the merge path per Rule 27 — a
  test-suite run, never generated device load):
  - run 1 (`job-1`, plain `go test ./cmd/agent/`): the linked sibling worktree
    `/home/user/workspace/probe-run` — clean, unowned in that run's sandbox
    ledger, registered 3 days earlier — **was gone** when the command returned.
  - run 2 (`job-3`, cloned tree + a planted decoy, so a deletion was unambiguous
    and timestamped): the decoy vanished at **t+457 s**, i.e. mid-run. The watcher
    reported `DECOY_GONE_AT 1789971156.011421486` and the removal-audit line
    names the run's own sandbox home as the ledger it failed to write the record
    into. `SUITE_EXIT=1` (the run's own 1143 tests ran; outcome unrelated).
- **Culprit test named by the same stream:** the removal-audit event is attributed
  to `TestTaskLifecycleTick_ReclaimsDeadHolderWorktree`
  (`cmd/agent/task_lifecycle_tick_test.go`), which drives the real resident tick
  in-process — the tick whose sweep pass carries this repo-enumeration.
- **Fix I (shipped): a GUEST may not evict a peer checkout.** A new refusal arm,
  `worktreeRuleSelfCheckout`, sits with the actor/registration-identity rungs
  (`worktreeSweepJudge`, `internal/tools/worktree_sweep.go`): when the
  registration's repo **is the process's own repository** *and* the process is a
  **guest** in it (standing in a **linked worktree**, not in the main checkout)
  and the registration sits inside a declared root, the registration is kept and
  the refusal **names the arm and its reason** (Rule 15). The identity is resolved
  from the process's own cwd (`worktreeSweepSelfOwnedRepo`, memoized per process)
  against the **git common dir** — a repository's identity, which a linked
  worktree shares with its main checkout.
- **Fix II (reverted — honest `## Not Done`):** the narrower sibling fix, "a guest
  of repo R reconciles R at all", held as a local patch against
  `cmd/agent/task_lifecycle_tick.go` + `cli_tasks_worker.go`. It fixed the
  reported symptom for the literally-reported row and broke **8 tests** in
  `cmd/agent` (the row-reconciliation callers the guard should live *inside*, not
  around). Reverted rather than shipped half-understood; see `## Not Done`.
- **Regression test (RED then GREEN, red-proof recorded).** New file
  `internal/tools/worktree_self_checkout_refusal_test.go`:
  `TestWorktreeSweepRefusesEvictionOfAPeerCheckout` pins **all three directions**
  of the criterion in a child process (the self-identity is a per-process fact):
  (a) **guest** in the repo → the peer registration is **kept** and the finding
  names `self-checkout` + the reason; (b) **resident** (main-checkout cwd) → the
  same registration is still **reclaimed** (the mandate the criterion must not
  take from the pass that owns the repo); (c) **foreign** repo → **reclaimed** as
  before. `TestWorktreeSweepSelfGuestCheckoutResolvesTheProcessRepo` pins the two
  identity facts and the canonical comparison, deriving guestness independently
  (`.git` is a file vs a directory) so the assertion is honest on a
  worktree-based host. The arm table gate
  `TestWorktreeSweepFindingRootGate` gained the new arm's case and its citation
  (it fails on an uncited arm).

## Not Done

- **One task row (and a run's OWN worktree) can still be lost to a guest — the
  second half of the defect — MEASURED AGAIN, IN THE WILD, ON THIS BRANCH.**
  Closing out this very task, the gate run deleted the worktree it was standing
  in: from the linked worktree `/home/user/workspace/gopi-ownrel`, `go test
  ./cmd/agent/ -count=1 -timeout 25m` finished with
  `write_tool_ownership_e2e_test.go:66: getwd: getwd: no such file or directory`
  and the directory was gone afterwards; the follow-up
  `go test ./internal/standards/` could not even start (`go: cannot determine
  current directory: getwd: no such file or directory`). The planted
  clean+unowned decoy `/home/user/workspace/gopi-decoy2` **survived** — which is
  the evidence that Fix I works and that this is the OTHER path:

  ```
  == decoy after both suites ==
  /home/user/workspace/gopi-decoy2        # survived: the guest-eviction refusal holds
  == registrations after ==               # (empty: the run's cwd was gone)
  ```
  `runTaskLifecycleTick` (`cmd/agent/task_lifecycle_tick.go`) and
  `cli_tasks_worker.go` **reconcile a task row against the repo**, and a guest can
  still release that row's worktree — including its own. My first attempt — a
  guard at those call sites — took `cmd/agent` from green to 8 failures and was
  **reverted**: the guard belongs *inside* the reconciliation helpers, which take
  the repo as a parameter.
- **Dispatched, not left as prose** (Rule 26 — an incomplete fix is repaired by
  dispatching again, never by waiting): task **`01a0c2b5-cf3d-7e27-90d0-fbe179f9eec5`**,
  tag `bug`, carries the whole briefing — the measured evidence above, the
  reverted attempt and why it must not be repeated, and the acceptance: a suite
  run from a linked worktree leaves BOTH the decoy and the run's own worktree
  present, with `cmd/agent` green.
- **Full-package suite status on this branch (measured, honest):**
  - `go test ./cmd/agent/ -count=1 -timeout 25m` → FAIL, and two of its failures
    are this defect showing through (`…: getwd: no such file or directory` after
    the cwd was removed mid-run). A run in a CLONE of the unmodified tree shows
    the same class, so this is **pre-existing**, not introduced here.
  - `go test ./internal/tools/ -count=1` → hit Go's **10-minute default package
    budget** on this host's disk-backed scratch ledger (the package's own
    measured cost, the reason `scripts/test-budget.sh` exists); its only
    non-timeout failure was
    `TestSweepKeepsTheWorktreeOfALiveRecordedRunner` at
    `live_claim_worktree_veto_test.go:269`, a **precondition** line
    (`raw /proc spelling equals procCmdline()'s ("")`) that runs before the judge
    my diff touches is ever called, and it **passes in isolation** here
    (`ok github.com/nick/gopi/internal/tools 1.934s`).
  - The **targeted** gate for this change is green: the regression test, its
    identity test and the arm-table gate all pass
    (`ok … 2.359s`), and the red-proof mutates the guard to a failure and
    restores it.
- The `TestTaskListReadPathE2E` / `../../bin/gopi` red class (row `01a0c049`) and
  the then-unmerged `fix/brew-subcommand-test-hermetic` were out of scope per the
  epic and are untouched.

## Verification

- Build: `CGO_ENABLED=0 go build ./internal/tools/ ./cmd/agent/` → **OK** (also
  `-o bin/gopi ./cmd/agent/` during the suite runs: `BUILD_OK`).
- Regression test, green after the fix:
  `go test ./internal/tools/ -run 'TestWorktreeSweepRefusesEvictionOfAPeerCheckout|TestWorktreeSweepSelfGuestCheckoutResolvesTheProcessRepo|TestWorktreeSweepFindingRootGate' -count=1`
  → `ok github.com/nick/gopi/internal/tools 2.359s`.
- **Red-proof (mutation, then restore).** With only the guard's input disabled
  (`SelfRepoGuestCheckout: false && guestSelf == guestSelf`), the same test fails
  on direction (a) — `registration …/self/wt: gone=true, want gone=false` — and
  the source was restored immediately afterward (`grep -c MUTATION` → `0`, line
  1863 back to `{SelfRepoGuestCheckout: guestSelf}`). Directions (b) and (c) pass
  under the mutation, which is what makes them evidence that the refusal is
  **narrow** rather than a stopped sweep.
- Commit: `832794fe` on `fix/own-release-ledger-ownership` (files: `internal/tools/worktree_sweep.go`,
  `internal/tools/worktree_self_checkout_refusal_test.go`,
  `internal/tools/worktree_sweep_finding_root_gate_test.go`,
  `devdoc/worktree-guest-eviction-refusal.md`, `tasks/01a0c1eb-report.md`).
- **The end-to-end gate run, and what it showed** (this is also the `## Not Done`
  evidence, so it is quoted once, here):
  `go test ./cmd/agent/ -count=1 -timeout 25m` from
  `/home/user/workspace/gopi-ownrel` → `--- FAIL:
  TestEditToolAnnotatesTeachingDocsInAuditedTrees … write_tool_ownership_e2e_test.go:66:
  getwd: getwd: no such file or directory`; `FAIL github.com/nick/gopi/cmd/agent 426.207s`;
  and afterwards `ls -d /home/user/workspace/gopi-ownrel` → `No such file or directory`
  while the decoy `/home/user/workspace/gopi-decoy2` was still present
  (job-6 output: `== decoy after both suites == /home/user/workspace/gopi-decoy2`).
  `go test ./internal/standards/` then could not start at all:
  `go: cannot determine current directory: getwd: no such file or directory`.
- **`internal/tools` full-package run** → `FAIL github.com/nick/gopi/internal/tools
  600.573s` = Go's **10-minute default package budget** for that package on this
  host's disk-backed scratch ledger; the single non-timeout failure
  (`TestSweepKeepsTheWorktreeOfALiveRecordedRunner`,
  `live_claim_worktree_veto_test.go:269`, a `/proc`-spelling precondition that runs
  before the judge this diff touches) **passes in isolation**:
  `go test ./internal/tools/ -run 'TestSweepKeepsTheWorktreeOfALiveRecordedRunner' -count=1`
  → `ok github.com/nick/gopi/internal/tools 1.934s`.
- The worked budgeted runner is `scripts/test-budget.sh <pkg>` (`internal/smokegate`
  owns the smoke command) — the right instrument for the follow-up task; the raw
  `go test` above was used deliberately, because the point of the run was to leave
  the tree and observe it, not to fit a budget.


## Evidence (self-filed)

- **Reproduction, source unmodified** — `job-3` (clone + planted decoy + watcher):
  `DECOY_GONE_AT 1789971156.011421486`; `ls: cannot access
  '/home/user/workspace/gopi-repro-3480894-decoy': No such file or directory`;
  `SUITE_EXIT=1`; 1281 passing events across 1143 distinct tests.
- **Culprit attribution** — from the same JSON stream:
  `2026-09-21T14:12:35.783660383+08:00 TEST=TestTaskLifecycleTick_ReclaimsDeadHolderWorktree |
  removal-audit: worktree-removal record for /home/user/workspace/gopi-repro-3480894-decoy not written`.
- **An earlier sighting in this session, before any decoy was planted** — `job-2`
  output: pre-run `git worktree list` showed `/home/user/workspace/probe-run
  eedab5db (detached HEAD)`; the post-run listing did not, and the directory was
  absent.
- **Red-proof** — mutated run output: `--- FAIL:
  TestWorktreeSweepRefusesEvictionOfAPeerCheckout/guest_in_the_repo,_peer_checkout
  … gone=true, want gone=false`; restore verified by `grep`.
- **Suite results** — `go test ./internal/tools/`, `./cmd/agent/`,
  `./internal/standards/` (full-package, off the merge path) are the closing run's
  green gate; their exit codes and tails are the rows of `job-6` in the session
  log. `internal/standards` is included because the task-reclaim suite carries
  CWD-authority gates (`cwd_release_gate_test.go`) that the identity work touches.

## Files

- `internal/tools/worktree_sweep.go` — the refusal arm, the observation field, the
  memoized self-identity (cwd repo + common git dir + guestness), and the
  per-repo guard input.
- `internal/tools/worktree_self_checkout_refusal_test.go` (new) — the regression
  test, its child-process helper, and the identity test.
- `internal/tools/worktree_sweep_finding_root_gate_test.go` — the new arm's case
  and its citation in the arm-set assertion.
