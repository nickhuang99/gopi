<!-- doc-class: historical -->
# The submit refusal's advertised door could not be walked — `task update --repo` now stamps the ground

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a131f5e6a870f989eb4ded659e11f5` · 2026-10-13 · branch `task/01a131f5e6a870f989eb4ded659e11f5`

## The defect, measured

`gopi task submit` refuses a non-uuid row id when the row is repo-aware, and it is
RIGHT to: `TaskRunGroundFor` derives a ground directory as
`gopi-` + the row's uuid with its dashes removed (32 hex characters), so a slug id
can name no ground. The refusal is not the bug — the refusal's second remedy was.

```
$ gopi task submit "r31" --task-id 01a131f2r31row --source agent --repo <repo>
gopi task submit error: task_submit: cannot resolve the ground for task 01a131f2r31row in repo …:
the task id is not a full uuid, so no ground name can be derived from it …;
resubmit the row under a uuid id, or name the ground explicitly with
gopi task update <task_id> --repo <path-to-live-work-tree>
```

Pasting a path out of `git worktree list` — the natural reading of that sentence —
produced this, on a row the ledger already held with `{"repo":…}` only:

```
$ gopi task update 01a131f2-0000-7000-8000-00000000ab03 --repo <repo>/wt3
task 01a131f2-0000-7000-8000-00000000ab03 already names <repo> — nothing to repair
$ # stored deliverable AFTER: {"repo":"<repo>"}   ← still no worktree key
```

Exit 0, and `deliverable.worktree` unstamped — so the row never becomes a candidate
of the reclaim arm, which is the outcome the refusal promised was reachable.

### Root cause (5-why)

1. The operator pastes a live linked work tree of the row's repo; the verb says "nothing to repair".
2. `TaskRepointDeliverableRepo` resolves the argument with `DurableRepoForSubmit`, which maps a **linked worktree to its main checkout** (that is its job — a repo-aware row must not store an ephemeral directory).
3. The main checkout IS the repo the row already stores, so `SameDir(oldRepo, durable)` fires and the function returns early — before any ground question is asked.
4. So the verb repaired ONE axis (repo) while the refusal had asked about ANOTHER (ground = `deliverable.worktree`); and by construction no argument to a `--repo` naming a worktree of the row's own repo can escape that early return.
5. Two different facts were rendered by one sentence: *"the REPO axis is already right"* was being reported as *"there is nothing here to repair"*. Only the first was true.

`deliverable.worktree` is the field the reclaim arm scans (`task_reclaim_self.go`),
so the row stayed invisible to it forever.

## The fix — make the advertised remedy performable

`--repo` may now answer the GROUND question as well as the repo question. When the
argument resolves to the repo the row already names, the verb asks the next
question instead of stopping:

| state of the argument / row | answer |
|---|---|
| the argument **is** the repo (main checkout, or DurableRepo collapsing it) | the honest same-repo note (unchanged) |
| the row **records** exactly this worktree | satisfied, reported (`already names the worktree …`) |
| the row records a **different** worktree | REFUSED by name — never re-pointed |
| the argument is not a **live registered linked worktree** of that repo | REFUSED, naming the path and the listing command |
| the argument is a **detached** worktree | REFUSED — a ground is the worktree **+ branch** pair |
| another row already names the **worktree** | REFUSED, naming the owning row |
| another row already names the **branch** | REFUSED, naming the owning row |
| otherwise | the ground pair is stamped through the ONE writer |

```
$ gopi task update <id> --repo <repo>/wt4
[task-notify] task …: deliverable.worktree backfilled to "<repo>/wt4" (branch "task/01a131f5-second") at repair
task … deliverable.worktree: <repo>/wt4 (branch task/01a131f5-second) — the row now records its ground
$ # stored deliverable AFTER: repo + branch + worktree — read back from the ledger
```

### The design decisions, and why

- **The door sentence is qualified, not weakened.** The refusal still names the same
  verb; it now adds *"(run it from a linked work tree of the row's repo — the
  directory itself is the ground)"*. That is the part an operator pasting a random
  path would otherwise get wrong, and the honest place to say it is where the
  instruction is given.
- **The argument is EVIDENCE, not a copy of argv.** The path must be a **live
  registered linked worktree** of the row's own repo — read through the EXISTING
  `taskRepoLinkedWorktrees` (the reader the claim-time seam and `resume` already
  trust), compared by directory identity (`SameDir`). This is what keeps the ground
  stamp an evidence claim: the directory is one git lists *and* one a run can start
  in. A bare directory inside the repo, or a path that is not a work tree, is refused.
- **The operator is not guessing; he is selecting.** Unlike gap A (claim-time
  binding), where registry-only evidence was rejected because the *row's own session*
  had to be the proof, here the OPERATOR names the directory explicitly — the same
  standing evidence `gopi worker --resume --worktree P` already accepts. The guards
  that carry the correctness load are the ONE-OWNER scans, and they are armed.
- **ONE writer, ONE-OWNER scans, never-rebind guard — all reused.** The write is
  `taskBackfillDeliverableWorktreeAt(taskID, wt, branch, &durable, "repair")`: the
  same CAS + parse/rebuild + attachment-preserving blob writer every other stamp
  uses; `taskWorktreeOwnerScan` (the rebindable seam, so its fail-closed branch stays
  observable) and `taskBranchRowOwner` both refuse on error, not just on a hit. The
  2026-09-12 foreign-owner hijack guard is untouched: a row that records a worktree
  is **never** re-pointed, and the refusal says so.
- **A third stamp moment.** The writer's outcome line renders *when* the byte was
  written, and the two existing spellings were `claim` / `creation` — neither is true
  of an operator's repair. `taskWorktreeStampMomentRepair` ("repair") was added for
  the same measured reason the claim/creation split was introduced: a log line that
  attributes the write to the wrong moment is a wrong claim, not a near-miss.

### Honest scope of the fix

The refusal itself is unchanged and still correct — a non-uuid id with a repo and no
ground is refused at submit. What changed is that its second door now performs the
repair it names. The FIRST remedy (`resubmit under a uuid id`) was already sound.

## Verification (green)

- `go build ./...` — clean.
- `go test ./internal/tools/ -run TestRepairRepo` — 6 tests PASS (green).
- RED proof: the same test file against the pre-fix source of the three touched
  files (restored with `cp`, then re-restored and hash-verified) FAILS with the
  verbatim pre-fix message `already names <repo> — nothing to repair` and
  `deliverable.worktree = ""`. Transcript: session
  `artifacts/redproof/RED-pre-fix.txt`.
- Real-surface run (Rule 31): `pre-fix binary → dead end`, `post-fix binary →
  exit 0 + ledger read back`. Transcripts: `artifacts/prefix-run/remedy-prefix.txt`,
  `artifacts/postfix-run/remedy-postfix.txt`, `artifacts/postfix-run/refusals-postfix.txt`.

## Files

- `internal/tools/task_repo_repair.go` — the ground arm (`taskRepairSameRepo`,
  `taskRepairLinkedEntry`) and the new named constants. The registration question is
  ONE reading of `taskRepoLinkedWorktrees` (existence and branch answered from the
  same list, so they cannot disagree; its error is reported, never swallowed).
- `internal/tools/task_deliverable_worktree.go` — the `repair` stamp moment.
- `internal/tools/task_run_ground.go` — the refusal's door note.
- `internal/tools/task_repo_repair_ground_test.go` — the regression test (red → green).

## An inherited red, landed in the same branch

`TestManSourcesMatchTheirHelpPage` failed for `man/gopi-merge.md` on this branch
before this run's change was reachable at all: `3217781bc` — the sibling
`feat(archive): read side` branch, merged into this branch's base by `5be1220b2` —
rewrote the `gopi merge --status` help text and did not regenerate the man source.
`./cmd/agent` is a touched package here, so the suite could not be green without it;
commit `25885bb65` carries the mechanical regeneration by the ONE generator
(`internal/standards/testdata/mancensus.go`) and nothing else. The sibling branch
`task/01a131fcb8337c1b9e1ff879920b59ae` still carries the defect on its own tip.

## Follow-up (2026-10-13): the sibling E2E's second call still asserted the PRE-ARM semantics

`TestRepairRepoMovesARowOntoALiveWorkTree` (`internal/agent/reframe_durable_repo_resolver_test.go`,
written by `5052246db`) was red **3/3** on a clean master, deterministically, with:

```text
reframe_durable_repo_resolver_test.go:109: re-repairing the same repo must say so;
got "task <id> deliverable.worktree: <ground> (branch task/fixture-ground) — the row now records its ground"
```

The two calls in that fixture do NOT mean the same thing here, and that is the whole
root cause — measured, by printing the raw blob around each call:

| step | `deliverable.repo` | `deliverable.worktree` |
|---|---|---|
| after `TaskSubmit(Repo: seedRepo)` | `<seed>` | — |
| **call 1** `TaskRepointDeliverableRepo(id, ground)` | `<seed>` → **durable main** | — (pair DROPPED, it belonged to the seed repo) |
| **call 2** same argument | durable main | **`<ground>`** (+ branch) — the GROUND ARM |
| call 3 same argument | durable main | `<ground>` → "already names the worktree …" |

Call 1 is a **repo MOVE**: `TaskRepointDeliverableRepo`'s change branch deliberately
drops `branch`/`worktree` (they belonged to the old repository), so the row comes out
of call 1 ground-less — the exact state this epic's door exists for. Call 2 therefore
reaches `taskRepairSameRepo` (repo already right) and performs the **ground stamp**.
The expectation `"already names"` describes the pre-`22c38dbd5` verb, whose early
return made call 2 a no-op; the arm changed that answer for the `recordedWorktree == ""`
state, and the sibling tests pin the new one
(`TestRepairRepoStampsTheGroundPromisedByTheSubmitRefusal`).

**Verdict: the test was the stale side, not the product.** The behaviour that reddens
it is pinned by its own passing tests, and no production arm was weakened. The
assertion was re-pointed at the three states the fixture actually walks:

- call 1 → the repo move is reported and the stale pair is gone (`task show` renders
  no `deliverable.worktree:` line at all), which is the `clearGroundForNewRepo` clause
  it was silently covering;
- call 2 → the stamped line, **read back from the row** (`deliverable.worktree` ==
  the argument, `deliverable.branch` == the registration's branch) — a green that
  cannot be vacuous;
- call 3 → `already names the worktree …` — the idempotence the fixture was about,
  now asserted on the state where it is the true answer.

The two state phrases are exported constants (`TaskRepairGroundStampedNote`,
`TaskRepairGroundPresentPhrase`) that COMPOSE `TaskRepairGroundStampedFmt` /
`TaskRepairGroundPresentFmt`, so the sentences and their readers share ONE spelling
(Rule 1b/13); `task_repo_repair_ground_test.go` now reads its phrase from the
constant too.

Red proof (temporary prefix mutation — `taskRepairSameRepo`'s early return restored
verbatim; restore verified against `HEAD` by blob hash): the restated test fails on
the two new arms with the pre-arm answer quoted, and is green on the real source.
Rule 31 run on the real CLI (`gopi task update <id> --repo <linked-wt>`, three calls
with the ledger read back) reproduces all three lines verbatim.
