<!-- doc-class: historical -->
# Task Report: `gopi worker` recycles the worktree it was launched in (the manual-run close-out)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Branch `task/01a0d6b1b30a792db2e4a18518b97bf3`. Base master `f4451b9d`.

## Completed

- Reproduced the red on pristine master (`EXIT=1`) and, from the run's own child
  output, found that the epic's mechanism claim was only half true — the withheld
  directory is one **no ledger row names** (see "The mechanism, measured" below).
- Fixed **five** withholding rungs, all of which vetoed the ground the run had
  just finished in, and introduced ONE owner for the fact that separates them:
  `SelfGroundRelease*` in `internal/tools/reclaim_own_ground.go`.
- The acceptance is **red → green** on the real E2E, with the two guarding tests
  (`…MainCheckoutSkipsRecycle`, `…KeepsWorktreeHeldByForeignProcess`) still green.
- Made the close-out **reportedly honest** about which directory it freed
  (`worktree-freed=<path>`, Rule 15), and made the acceptance assert the RELEASE
  rather than only the directory's absence.
- Regression tests for every gate touched, plus the admission conditions.

### What was actually wrong

| # | rung | why it vetoed the run's own ground |
|---|---|---|
| ① | `reclaimSelfOwnWhy` rung ① — the age floor (`reclaimSelfOwnMinAge`, 5m) | the close-out's ground is seconds old **by construction** (`younger than the 5m0s floor (3s) — a concurrent round may be creating it`), so the anti-race floor vetoed the row whose run had just completed |
| ② | `guestRowReleaseRefusal` (the receiver arm, `task_reclaim.go`) | a run's cwd IS a linked worktree of the row's repo, which the arm reads as "a guest releasing another row's worktree" — the manual-run contract makes that shape the DESIGNED state |
| ③ | `guestRowOwnCheckoutRefusal` (the ownership-independent pass) | same position, read as "a process must never free the checkout it stands in" (correct for the sweep; the manual-run pass runs before the cwd release by design) |
| ④ | the declared-root rung (`reclaimSelfOwnInRoot`) | the run's ground (`<tmp>/…/wt`, beside its repo) is inside NO declared worktree root, so even an otherwise-eligible registration was "not swept" |
| ⑤ | **the pass ORDER** | `ReclaimOwnFinishedWorktrees` armed its mark and **disarmed it on return**, so the pass that actually frees the run's ground (`ReclaimSelfOwnRegisteredWorktrees`) ran entirely unmarked — found by instrumenting the pass, not by reading it |

And the directory itself: a run started inside a task's worktree **does not work
there** — the worker CREATES its own ground (`gopi worker: child ground →
…/004/gopi-<taskid>`) and the row's `deliverable.worktree` records THAT. The
launch directory is therefore named by no row, so every ledger-keyed arm was blind
to it by design, and the fixture's residue survived all of the above until the
launch ground itself became part of the close-out's ground set.

### The design (one owner, Rules 13/14)

`reclaim_own_ground.go` publishes the ONE fact — *the ground of one of THIS
process's own rows is being released right now* — as a process-local mark:

- **armed** by the close-out SEAM (`ReclaimOwnWorktreesAtCloseout`), which reads
  the rows the ledger says this pid claims (`reclaimSelfRows`) and arms **the
  launch directory alone** — and only when it is a **linked worktree of one of
  those rows' repos** (`inLinkedWorktreeRoot` + `reclaimSelfOwnRepoOf`, compared
  canonically). The rows are read for their REPOS, never to include their recorded
  grounds: a ground some row NAMES must keep refusing (the pre-existing criterion
  `TestReclaimSelfRefusesItsOwnCheckout` covers the 2026-09-21 breach), and the
  machinery that resolves a row already removes its recorded ground (the measured
  run's own `removal-audit: worktree-removal record for …/004/gopi-<taskid>`);
- **read** by each of the five rungs above through `SelfGroundReleasePendingFor`,
  which only ever *narrows* a rung: a foreign holder, a dirty tree, a live row, a
  registration outside the set, and every non-close-out caller are untouched. The
  permission lives at the SEAM, never inside `ReclaimSelfOwnRegisteredWorktrees`
  itself — that function is also the sweep's pass, and arming inside it handed the
  close-out's permission to the sweep and turned
  `TestGuestRowRefusesToReconcileFromAGuestWorktree` red (measured);
- **nested by composition, not by replacement**: the seam arms both halves, and
  the ledger-keyed half arms (and disarms) its own mark inside it. A disarm
  therefore SAVES and RESTORES what it replaced — a disarm that cleared took the
  outer mark with it, which is exactly how the launch ground came back to the age
  floor a second time (measured);
- **disarmed** when the pass returns, and **armed by each pass for itself** (④/⑤
  above — a mark and its pass must not be separable);
- deliberately **not an env knob**: a knob an operator could set would let a sweep
  free somebody else's ground; this fact is only ever true inside the pass that
  publishes it.

The launch directory is admitted on its own merits, never on precedence: the
recorded candidates are where THIS process stood and where its **launcher** stood,
and the fixture's own run proved the handoff can name a foreign directory (this
session's worktree), which condition ② then rejects — measured, not assumed.

## Not Done

- **A cost I introduced, named rather than hidden**: admitting the launch
  candidates runs one `git worktree list` per candidate per pass (up to two
  candidates × two producer passes per round). The previous design read no cwd at
  all. It is bounded and memoizable (the candidates cannot change during a
  process's life), and I left it un-memoized deliberately — a cache here would be
  new state to reason about for a cost measured in single `git` invocations beside
  passes that already run one per repo.
- **The launch-directory policy is a judgement call I made, not one the epic
  specified.** A close-out now may remove a linked worktree of its own row's repo
  that it was launched in, inside no declared root. It is guarded five ways (own
  row, linked worktree only, foreign holder vetoes, dirty vetoes, live row vetoes)
  and it is what makes the acceptance green — but it widens what a close-out can
  remove, and a reviewer may prefer to gate it behind an explicit operator
  declaration instead. Named here so the decision is visible rather than buried in
  a diff.
- **A live-session veto for a dead claim was not investigated.** The launch
  directory is released under the row's own claim; the row is read from the ledger
  by claim stamp, and I did not add a "the row's session is still alive" guard
  beyond the live-holder scan (which reads the *directory's* holders, not the
  *row's* session). If a slow-settling session holds the reserved name without a
  process cwd, the release resorts to a name-level path — unchanged from master's
  behaviour for the ledger-keyed arm, but new for the launch directory.
- **The E2E's stale premise was left as found**: its comment predicts the branch
  backfill yields `feat/recycle-e2e` while the run produces `task/trecyclee2e`
  (the worker's own branch). Harmless for these assertions (the name comes from
  the fixture) but misleading; correcting it is a separate, cosmetic change.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK;
  `go vet ./internal/tools/ ./cmd/agent/` → clean.
- Tests:
  `go test -count=1 -timeout 900s -run 'TestManualRunRecyclesItsWorktree|TestManualRunMainCheckoutSkipsRecycle|TestManualRunKeepsWorktreeHeldByForeignProcess' ./cmd/agent/`
  → `--- PASS: TestManualRunRecyclesItsWorktree (6.52s)`,
  `--- PASS: TestManualRunMainCheckoutSkipsRecycle (4.69s)`,
  `--- PASS: TestManualRunKeepsWorktreeHeldByForeignProcess (6.85s)`,
  `ok github.com/nick/gopi/cmd/agent 19.201s`, `EXIT=0` (`/tmp/acc5.log`, HEAD
  `30b2155a`, `bin/gopi` rebuilt from that same tree — `sha256
  02f5bd20d57eeb95…`, the fixture binary the suite requires, since without it the
  E2E SKIPS green); red before the change (§ Evidence).
  `go test -count=1 -run 'TestReclaimSelf.*|TestSelfGround.*|TestGuestRow.*|TestFinishCleanup.*|TestReclaimGroundChecklist.*|TestWorktreeRefusal.*' ./internal/tools/`
  → `100 PASS / 0 FAIL`, `ok … 84.148s`, `EXIT=0` (`/tmp/rev2.log`).
- Ran: the E2E is itself the live invocation, and the run it drives is the epic's
  own reproduction — `gopi worker <task-id>` whose CWD is a linked worktree of the
  row's repo. The close-out's own line, captured from that run's child output:

  ```
  task t-recycle-e2e: own-worktree-release: own registered worktree freed: /home/user/workspace/gopi-test-scratch/suite-cmd-agent-run-561806890/…/001/wt (a worktree of this run's repo that no ledger row owned; branch task/trecyclee2e untouched)
  ```

  `gopi task run` was tried first and is **retired** by design —
  `gopi task run: removed (epic worker-run-entry) — running a task is the worker's
  job now` — so the CLI surface exercised is `gopi worker <task-id>`.
- Result: green; the child's close-out output is quoted above and in § Evidence.
- Commit: `30b2155a fix(tools): the close-out mark covers the LAUNCH ground only,
  and composes` — the tip of the branch, whose three commits are the landed fix:
  `9bad5b42` (the close-out releases the worktree it was launched in), `3328ccf5`
  (the cost named, not hidden) and `30b2155a` (the mark narrowed to the launch
  ground, moved to the seam, and made to compose). Report history: `6627c21e`,
  `7987b09f`, this section final.

## Evidence (self-filed)

- **Pre-change red** (pristine master `f4451b9d`):
  `--- FAIL: TestManualRunRecyclesItsWorktree (2.90s)`, `EXIT=1`, with the child's
  own lines:
  `own-worktree-release: own worktree release refused: … guest-row-reconcile
  refusal (arm guestRowOwnReleaseRefusal)` and
  `own registered worktree kept: … — younger than the 5m0s floor (3s)`.
- **Post-change green** (`/tmp/acc-final2.log`): the three PASS lines and
  `EXIT=0` quoted above; each run also carries the `… freed: …/001/wt` line, which
  the test now ASSERTS (`tools.ReclaimSelfOwnFreedPrefix+wt`) rather than merely
  observing the directory's absence.
- **The two guarding directions, measured by real subprocesses**:
  - `TestManualRunKeepsWorktreeHeldByForeignProcess` drives the worker with its
    CWD inside `wt` WHILE a foreign holder process sits in that directory — the
    armed mark is present and the live-holder scan still vetoes (PASS).
  - `TestManualRunMainCheckoutSkipsRecycle` drives a run on the main checkout —
    the no-op guard (PASS).
- **Unit-level red proof**: `TestSelfGroundReleaseKeepsEveryForeignRegistration`
  asserts both directions — UNARMED, the age floor still keeps a fresh
  registration of the asker's own row; ARMED, another row's ground is not covered
  (and a parent-directory spelling never matches).
- **Instrumented evidence for finding ⑤** (the pass order): a temporary print in
  the ownership-independent pass showed `armed=[] launchDirs=[]` for both
  producers — the mark was empty because the sibling pass had already disarmed it.
- **Skip trap measured** (worth remembering): the E2E SKIPS without the fixture's
  `bin/gopi` — `cli_tasks_recycle_e2e_test.go: gopi binary not found at …/bin/gopi
  — run scripts/e2e_smoke.sh first`, `ok … 1.086s`, `EXIT=0`. The first run of this
  task came back green-by-skip; the build is a prerequisite of the acceptance.
- **Gate attribution**: the ONE new `internal/standards` violation
  (Rule 17, `internal/agent/rawlog_test.go:309`) and the two doc-manifest reds for
  `devdoc/internal-tools-master-reds-20260925.md` / `devdoc/shot-capture-closeout-wipe.md`
  are OTHER agents' uncommitted files from the same day, present before this run's
  work; the gate reports no violation in any file this change touches. This run's
  own devdoc was classified in `devdoc/DOC-INDEX.tsv` (row appended) and carries
  its `<!-- doc-class: historical -->` banner, so it adds no gate debt.
- **Rule 30 (self-filed)**: `cd <worktree> && git status --porcelain` printed
  NOTHING at HEAD `30b2155a` — measured, not asserted: the two commands were
  `git status --porcelain` (empty output, exit 0) and `git rev-parse HEAD`
  (`30b2155a96ec5bdcaaf29ae59f35fda03399d6fc`). `bin/gopi` is untracked-but-ignored
  (`.gitignore:2:bin/`), so the built fixture does not dirty the tree.


## Files changed

- `internal/tools/reclaim_own_ground.go` (new) — the mark: arm/read/disarm, the
  close-out ground set, the launch-directory admission conditions.
- `internal/tools/reclaim_own_ground_test.go` (new) — the two admission directions
  (^ armed releases / v unarmed keeps), canonical matching, disarm, admission.
- `internal/tools/task_reclaim_self.go` — rungs ①②③⑤ and the pass-order fix.
- `internal/tools/task_reclaim.go` — rung ② (the receiver arm).
- `internal/tools/reclaim_ground_checklist.go` — rung ③ at the shared checklist.
- `internal/tools/task_finish_cleanup.go` — `worktree-freed=<path>` (Rule 15).
- `cmd/agent/task_session_cwd.go` — publish the launch candidates (one owner).
- `cmd/agent/cli_tasks.go`, `cmd/agent/cli_tasks_worker.go` — the pass order's doc.
- `internal/tools/cov_finish_cleanup_test.go`, `cmd/agent/cli_tasks_recycle_e2e_test.go`
  — regression tests.
