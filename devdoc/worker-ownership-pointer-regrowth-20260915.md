<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Task Report: `TestWorkerOwnershipPointerPresent` red on master — six regrown files, not one

Branch: `fix/worker-ownership-pointer-missing`
Worktree: `/home/user/workspace/gopi-fix-worker-ownership-pointer` (off master `664b3784`)
Recovery: `git show fix/worker-ownership-pointer-missing:devdoc/worker-ownership-pointer-regrowth-20260915.md`
Commit: `2399825f` — "fix(briefs): add the operator-only worker-ownership pointer to the six uncovered docs"
(`git diff --stat master...fix/worker-ownership-pointer-missing` → 7 files, 99 insertions; worktree,
so the commit is already visible from the main repo — no push needed)

## Completed

- Reproduced the reported red on pristine master: `go test -count=1 -run
  TestWorkerOwnershipPointerPresent ./internal/testutil/` failed.
- **The briefing under-reported the gate's predicate set.** It named ONE file
  (`briefings/brief-worker-child-stdout-to-session-dir.md`); the gate on master
  reports **six**:
  - `briefings/brief-daemon-scope-unit-name-per-profile.md`
  - `briefings/brief-worker-child-stdout-to-session-dir.md`  ← the reported one
  - `devdoc/ledger-dsn-cmd-agent-contract.md`
  - `devdoc/nightshift-cgroup-worktree-resume-20260915.md`
  - `devdoc/tmpdir-default-and-workspace-decision.md`
  - `devdoc/worktree-residue-closeout.md`

  Root cause of the mis-report: the gate's own failure message is capped at
  `workerOwnershipMaxReportedHits = 20` and prints `… (truncated)`. Six real
  hits are below the cap, so the reporter's single-file list came from a
  partial read, not from a saturated report. The class is the documented
  "regrowth of an already-fixed class" (`devdoc/tmpdir-merge-resolution.md` §2):
  each file is a report written *about* a `gopi worker` / `gopi task run`
  failure, so every post-mortem re-introduces the launch shape.
- Fix: the ONE canonical pointer line at the head of each of the six files, in
  **markdown comment form** — `<!-- WORKER-OWNERSHIP: OPERATOR-ONLY —
  devdoc/worker-ownership.md -->` — exactly what the gate's own message and
  `devdoc/worker-ownership.md` §"canonical pointer line" prescribe.
- Chose option (a) (in-file annotation) over option (b)
  (`workerOwnershipDismissed`): these are not evidence-only quotes. They are
  documents that show a reader how a worker/task-run is launched, and the
  dismiss table is documented as a permit for files the audit did NOT name —
  adding six entries would have shrunk the gate to make a red go away
  (Rule 13/Rule 3: never weaken the check).

**Diff is markdown-only** — 6 files, 12 inserted lines, no Go file touched, so
the gate's own source and thresholds are untouched.

## Verification

- Gate: `go test -count=1 -run TestWorkerOwnershipPointerPresent ./internal/testutil/`
  → `ok github.com/nick/gopi/internal/testutil` (exit 0) in the branch worktree.
  All four ownership tests: `-run TestWorkerOwnership` → `ok … 1.408s`.
- Whole package (catches the dismiss-table honesty / canonical-block /
  scan-non-vacuity siblings): `go test -count=1 ./internal/testutil/` →
  `ok github.com/nick/gopi/internal/testutil 53.778s`.
- **Predicate-set differential (never trust the capped report):** reproduced the
  gate's needle set independently
  (`grep -rlE 'gopi worker|gopi task run|gopi-worker-up\.sh' --include=*.md --include=*.sh --include=*.txt devdoc briefings scripts`),
  then asserted every hit either carries the marker line or is named in
  `workerOwnershipDismissed`:

  ```
  predicate-set files: 204
  UNCOVERED: <none>
  ```

  Before the fix the same loop named exactly the six files above. The set is
  identical before and after my change (annotation only), so the green is a
  real predicate-set diff, not the report cap.
- Build: `CGO_ENABLED=0 go build ./...` → clean (`build+fmt ok`).
- `gofmt -l internal cmd` lists 4 files
  (`internal/tools/task_deliverable_worktree.go`,
  `cmd/agent/cli_tasks_advert_gate_test.go`, `cmd/agent/cli_worker_control.go`,
  `cmd/agent/usage_test.go`) — **pre-existing on master, unrelated**: the
  identical list comes back from the main repo checked out at master, and this
  branch touches no Go file.
- Main repo untouched: `git status` in `~/work/GoPi` shows no file from this
  change; all edits live in the branch worktree.

## Not Done (honest)

- **The underlying regrowth is not prevented, only repaired for the six current
  files.** Nothing yet makes a *new* devdoc/briefing fail at authoring time
  rather than at the next full `go test`. A durable guard (e.g. a pre-commit or
  report-writer hook that injects the pointer) is a separate piece of work and
  is NOT in this branch.
- The four pre-existing `gofmt` findings above are untouched (out of scope; no
  Go file is modified here).
- `go test ./...` full-suite was not run: the change is markdown-only, and the
  authorised gate (`-run TestWorkerOwnershipPointerPresent`) plus the whole
  `internal/testutil` package are green. No other package reads these docs.
