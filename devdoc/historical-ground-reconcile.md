<!-- doc-class: historical -->
# Historical owner-less grounds — reconstructed on the READ side, and the repo the ROW records

**Epic**: 01a0e074 · **Date**: 2026-09-27 · **Predecessor**: `6a4f556c` (devdoc/run-created-second-ground.md)
**Scope**: the two gaps the predecessor left in its own `## Not Done`.

## 1. Gap 1 — a historical ground with no recorded owner

The predecessor fixed the PRODUCER: a run that mints a branch/worktree now
records it in the row's `deliverable.grounds` axis. Rows minted before it still
record nothing, so every deliverable→row lookup answered *"no owner"* for a LIVE
ground. The measured shape (row `01a0ddc4-c410`, session `3592a5ef`): the run created
`/home/user/workspace/gopi-evi-pushback` on `fix/pushback-rule31-evidence` while its
row named its own ground, and 0 ledger rows named the created directory.

### Why READ side, and not a backfill verb

A backfill would WRITE a ground onto a row the ledger never recorded — for a
rebuilt skeleton, a RECONSTRUCTION. This axis has exactly ONE writer
(`taskBackfillDeliverableGrounds`), and it is deliberately restricted to the
moment a run CREATES a ground, in-process, where the taker's own identity is
observable. A historical ground has no taker left to ask, so the write would be
an inference — and **writing the inference DOWN freezes it**: a wrong row becomes
the ledger's own claim, un-appealable, while the evidence a later reader must be
able to re-check (a session's command line, git's registration, the row's life
window) is exactly what the read derives per query. That is also Rule 35: the
artifact such a verb needs — a written reconstruction plus a dry-run whose output
is itself a state a reader must trust — buys nothing the read cannot give, and it
creates a second writer of the ground axis. **The ledger row stays the store; the
answer is derived per query.**

### The evidence — all four pieces are required

`internal/tools/task_ground_reconcile.go`:

| # | piece | reader |
|---|---|---|
| ① | git registers the asked ground LIVE (directory still exists) | `taskRepoLinkedWorktrees` — the ONE reader |
| ② | the candidate row's `deliverable.repo` IS the registering repository | row reader |
| ③ | the row was ALIVE at the registration's birth (`created_at ≤ birth ≤ end`) | `branchAttributionCheckoutAge`'s admin-dir mtime |
| ④ | the row's OWN session ran `git worktree add … -b <branch>` inside that window | `taskWorktreeAddBranchFromCommand`, the ONE parser; session found by TASK ID |

Any one alone is a guess. ④ is the piece that makes the answer CHECKABLE: the
operator re-runs the same command line. A ground that fails any piece is reported
as **NOT reconciled**, never as "no owner" (Rule 15b) — the miss line names what
was looked for and where.

**Found en route**: a row's `session_id` column is the SUBMITTING session
(dispatcher), not the run's own; of 271 GoPi rows carrying one, 215 resolve only
through their own `sessions/<task_id>/<uuid>.jsonl` directory. Hence
`taskFindSessionJSONLByTask` — keyed by TASK ID, identity-checked against the
record's own `taskId`.

## 2. Gap 2 — `task show <id> --branch` read the CLI's CWD repo

The git half built its candidate repositories from the declared `repos` table
(Rule 12) and the process's working directory, IGNORING the row the caller had
already named. Measured: the row records `/home/user/work/GoPi`; invoked from a
different checkout the answer was about the WRONG repository — and, when the
declared table happened to hold a repo with the same branch name, was declared
"the SAME repository".

The row's own `deliverable.repo` now LEADS the candidate list
(`DeliverableSelector.Row` → `taskRowDeliverableRepo`, read through the ONE row
reader), and the answer says which repository it read
(`DeliverableRepoFromRowNoteFmt`), or — when the row records none —
states the fallback out loud (`DeliverableRepoUnrecordedNote`). The working
directory stays the LAST candidate: it is the weakest evidence, but it is still
what answers when there is no row.

## 3. Verification (all measured)

- **Gap 1, live ledger, real ground** — `/tmp/gopi-gap2 task for-branch
  fix/pushback-rule31-evidence` (cwd `/tmp`) → exit 0, `1 owner(s) … RECONSTRUCTED
  from evidence`, naming row `01a0ddc4`, the mint command
  (`git worktree add /home/user/workspace/gopi-evi-pushback -b
  fix/pushback-rule31-evidence` at 2026-09-26T18:59:17Z), the row's life
  (12:50:55Z–19:18:45Z) and the registration's tip `42fc17d6`. The control binary
  built from unmodified master: exit 4, `no owner`.
- **Gap 2, discriminating panel** (the row's repo is NOT in the declared table;
  CWD holds no such branch; isolate profile `GOPI_HOME` = sandbox): FIX → "branch
  EXISTS in /dev/shm/gap2-panel2/repo-row at 099ecc2" + "note: reading the
  repository the row records"; CONTROL → "does NOT exist in /home/user/work/GoPi
  — git has no such ref". Same invocation, different repository: the gap.
- **Red-first, both neutered at once** (`go test ./internal/tools/ -run
  'TestGroundReconcile|TestDeliverableGitHalf' -count=1`) → `FAIL` with
  `the reconciliation must name exactly one owner, got 0`,
  `the worktree spelling must reconcile to one owner (reconciled=false, owners=0)`,
  `the miss line must name what was looked for (…)`, and
  `the git half's FIRST candidate must be the row's recorded repo "…/repo-row",
  got […/repo-elsewhere …/internal/tools]` +
  `the reading answered about "…/repo-elsewhere", want the row's recorded
  repository "…/repo-row"`. Restore → `ok github.com/nick/gopi/internal/tools`.
- **Not mine, pre-existing**: `TestEveryWorktreeRemovalArmQuestionsTheGround`
  fails on a `git archive master` tree too (6 guards vs 7 expected), and only
  inside this worktree's suite (a pre-existing, root-branch-independent defect).

## 4. Not Done (honest)

- The reconciliation answers for grounds whose registration is LIVE; a ground
  whose directory was already removed cannot be reconciled from git and is
  reported as such. Backfilling those before they are swept is out of scope here.
