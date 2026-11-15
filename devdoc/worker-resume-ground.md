<!-- doc-class: normative -->
# Devdoc: `gopi worker --resume` — the worker owns the ground

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Status: implemented, manually accepted (2026-09-24). Branch
`task/01a0d0ccfcff70868ee6ea2175c1e6a7`.
Briefing: `briefings/brief-worker-resume-owns-the-ground.md`.

## The problem this ends

The close-out chain removes a finished row's worktree — except when the chain's
own process is standing in it. That arm is
`internal/tools/guest_row_guard.go`'s `guestRowOwnCheckoutRefusal`, and it is
**correct**: the 2026-09-21 breach on row `01a0c574` removed a live run's ground
from under it, and nothing surfaced until a later step asked for its own cwd
(`getcwd: no such file or directory`).

The breach is structural, not an arm that needs weakening. `gopi resume
<task-id>` chdir's **into** the row's worktree — that is where the executor's
commits are — so the process that must remove the directory is the process
standing in it. Every successful resume therefore leaves its worktree
registered forever, and the operator's only tool for the job refuses by design.

## The fix: a face that is not standing there

`gopi worker --resume --branch <name>` / `--worktree <path>`:

1. **resolve** the EXISTING ground — never create one — and derive the ledger
   row from it (`internal/tools/task_resume_ground.go`; the resolver never
   chdir's, so the caller's cwd stays out of the worktree);
2. **run the existing resume path in a CHILD** (`gopi resume <task-id> --task
   <task-id>`, cwd = the resolved ground) — the resume is *not* reimplemented:
   the child is the operator's own command;
3. **run the standard close-out chain from here**
   (`tools.TaskFinishCleanupSessionID`), over a directory nobody is standing in.

The resumed session's own chain still correctly refuses (observed in the
acceptance run below, verbatim) — and the worker's chain, standing outside,
removes the directory. No arm was weakened; the arms were given the actor they
were implicitly waiting for.

## Design decisions worth keeping

- **Selector → row, not row → selector.** The dispatch path starts from a row;
  this mode starts from the GROUND. That inversion is what lets an operator
  resume what they can SEE (`git worktree list`) without knowing the row id.
  `--branch` derives the repo from the ledger rows naming the branch (the
  operator is not asked which host holds it); `--worktree` takes the repo from
  `git rev-parse --git-common-dir` at the path.
- **Resolve-only, enforced structurally.** The resolver returns a path and never
  enters or creates anything; `TestResumeGroundNeverCreatesGround` compares
  `git worktree list` and `git branch` byte-for-byte around four refusals. This
  is not politeness — silently minting an empty checkout when the operator asked
  to resume existing work is the worst possible outcome of the mode.
- **The main checkout is refused by name** (`taskResumeGroundMainCheckoutRefusalFmt`),
  in both spellings. The path is real, git is there, and what is wrong is
  something else entirely — so the sentence names the main checkout rather than
  reporting a missing worktree.
- **Plurality goes through the shared policy.** Several rows can name one
  ground; `TaskForBranchResolvedRow` picks the live owner, else the newest, and
  the resolver REPORTS the pass-over count (`resume ground: … 2 row(s) name
  it`) rather than batching silently.
- **The agent-spawn guard is skipped, deliberately.** `--resume` starts no loop
  and claims nothing (the guard's ruling is "an agent session must never START
  the worker LOOP"), and applying it here would refuse the very repair path the
  mode gives an operator inside a session.
- **A no-row refusal MEASURES the ground before it names it** (added
  2026-09-27, `taskResumeGroundNoRowRefusal`). The original no-row sentence —
  `no ground to resume — no ledger row names --branch B` — derived a fact about
  the FILESYSTEM from a fact about the LEDGER. Measured disagreement on branch
  `fix/pushback-rule31-evidence`: it was live in
  `/home/user/workspace/gopi-evi-pushback`, a registered linked worktree of
  `/home/user/work/GoPi` at tip `42fc17d6`, and the verb told the operator there
  was no ground. Two independent drivers produced such a ground: a branch a run
  created AFTER its claim (neither `TaskSubmit` nor the claim-time backfill
  stamps a run-created branch onto a row — measured: 0 rows named it), and a
  branch checked out in the repository's MAIN checkout, which the branch arm's
  `taskGroundResolveList` (linked registrations only) can never reach.
  The repair keeps the ledger walk FIRST and, only when no row names the branch,
  reads the ground through the SAME git reader `gopi task show --branch` uses
  (`TaskGitProvenanceFor`), then renders one sentence per measured shape: live
  linked worktree (names dir + repo + tip + the reason a row-less ground is not
  resumable), main checkout, stale registration, ref absent, and inconclusive
  (never rendered as absence). The `--branch` arm deliberately does NOT use
  `taskGroundResolveList` for the no-row case: one no-row fixture in
  `TestResumeGroundNeverCreatesGround` hands the resolver a registered worktree
  while git holds no such ref, and that path's contract is "tried and failed",
  which the provenance rendering keeps. Making a row-less ground RESUMABLE stays
  out of scope and is impossible without a row: the resume checkpoint resolves
  from the row's own run directory, and inventing a row is dependency
  invention (Rule 35).

## Verification

- Build: `go build ./...` → exit 0; `go vet ./cmd/agent/ ./internal/tools/` → clean;
  `gofmt -l` on the touched files → clean.
- Tests: `go test ./internal/tools/ -run 'TestResumeGround'` → ok (7 tests, incl.
  four negative shapes); `go test ./cmd/agent/ -run 'TestWorkerResume'` → ok
  (5 grammar/contradiction tests + 4 end-to-end tests: both spellings against a
  real repo, real-binary refusals, and the dirty-worktree guard).
- Ran: `testsuite/resume_acceptance.sh <scratch> /tmp/gopi-new` (real binary,
  real repos, real ledger; SSE stub in place of the API) — both spellings
  `EXIT=0`, ground `REMOVED`, branch tip unchanged, `git worktree list` down to
  the repo alone; five refusals `EXIT=1`/`EXIT=2` with named reasons and nothing
  created.
- Pre-existing at the base (measured on a detached worktree at `3dd59e7b`, NOT
  caused by this change): `TestTopLevelUsageDelegatesEnvVarsToHelpEnv` (167 > 160
  line budget), `TestWorkerReleaseOwnWorktree_Acceptance` ("the own directory
  must be released"), and `TestParentGateListingAgreesWithFunnel` ("child missing
  from the pending view") — the second is precisely the defect this mode exists
  to route around.

## Evidence (self-filed)

- `go build ./...` → exit 0 (no output).
- `go vet ./cmd/agent/ ./internal/tools/` → exit 0 (no output).
- `gofmt -l cmd/agent/cli_worker_resume.go internal/tools/task_resume_ground.go
  cmd/agent/cli_worker_daemon.go cmd/agent/main.go` → no output.
- `go test ./internal/tools/ -run 'TestResumeGround' -count=1` → `ok
  github.com/the operator/gopi/internal/tools 23.462s` (7 tests).
- `go test ./cmd/agent/ -run 'TestWorkerResume' -v -count=1` → all PASS, incl.
  `TestWorkerResumeOwnsTheGround` (`--- PASS: … (5.63s)`).
- `testsuite/resume_acceptance.sh` on `/tmp/gopi-new`:
  `EXIT=0`, `AFTER … the ground directory: REMOVED (as required)`,
  `AFTER … branch tip` identical to `BEFORE` (`cf1bb91d…`, `d127e808…`),
  and `git worktree list` after = the repo line alone.
- Red-proof (base `3dd59e7b`, detached worktree): `TestTopLevelUsage…`,
  `TestWorkerReleaseOwnWorktree_Acceptance` and `TestParentGateListingAgrees…`
  all FAIL there too — the three suite reds are not this branch's.

## Not done (honest)

- The 160-line top-level usage budget is now exceeded by 7 lines at the branch
  base and by this mode's extra usage block; the fix (moving worker detail to
  `gopi worker -h`) is a separate mechanical task, left untouched here.
- `--resume` does not accept `--takeover/--as/--db/--repo/--report-to`: it
  refuses them by name instead of implementing them (they describe the
  claim-and-run path this mode does not take).
