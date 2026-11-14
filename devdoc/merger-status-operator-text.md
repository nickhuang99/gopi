<!-- doc-class: historical -->
# Merger status text — the answer, in the operator's own model

> **CORRECTION (2026-09-19, epic `merger-delete-dead-impl`).** Standing claim
> made before the change it describes: the resident merger — loop, tick, take-over
> launcher, `--stop`/`--restart` control face AND the `--status` surface this
> document is about — was closed (`f9ff1a89`) and deleted (`55c4d6f3`). Every
> spelling of `gopi merger` now refuses with one recorded sentence
> (`mergerRetiredCause`, `cmd/agent/merger_retired.go`), and the one-tick merge is
> the bare `gopi merge` (`cmd/agent/merge_run.go`); the merger WORK LOCK survives
> (`cmd/agent/merger_work_lock.go`). `devdoc/merger-retired.md` is the record, and
> `devdoc/merger-self-driving.md` carries the flag-classification table with the
> same dated correction.

Epic: *merger status text misleading* (task `01a0b182-e62c-7e30-821f-0ff723459c17`),
branch `fix/merger-status-operator-text`.

## Completed

- **Root cause**: `(live-holder)` is a LEDGER verdict about a task ROW's recorded
  runner, welded to a claim about the CHECKOUT DIRECTORY ("a live run is working
  in its checkout") that no probe had made — while the same surface said
  elsewhere that holder liveness was not measured. Measured: both flagged
  branches had zero processes inside their checkouts; one row was already
  `completed`.
- **The measured half already existed and was dropped**: `branchAttributionCheckout`
  computes `d.Held` from the shared `scratch.HeldByProcesses`/`LiveHolders` /proc
  scan, but `BranchAttributionLine` never carried it, so no render could state a
  measurement. Now carried out, with the checkout path. **No second probe was
  added** (the destructive reclaim arms consult that one matcher).
- **The operator's own model is now the render** (`internal/tools/merger_status_candidate.go`):
  per branch, `candidate: no checkout holds it` / `NOT a candidate: a checkout
  holds it (<path>) and a live process IS / NO live process is inside that
  checkout — task <id>`, plus a `monitor:` occupancy tally and a named
  branch↔task registration gap when no row names a branch.
- **Wire tags retired from operator text** via one `verdictOperatorWords` table;
  the live-holder withheld reason split into *runner alive + measured process
  inside* / *runner alive but nobody inside* / *not measurable*, so the honest
  disclaimer can no longer contradict the confident sentence.
- **The same defect, found live on the lock line** (`d573e81a`): the surface
  declared one pid gone and running-for-a-minute simultaneously; `LockHolderNote`
  now measures the pid and the lock as separate claims.
- Regression tests asserting the operator model AND the retired phrase's absence.

## Files

- `internal/tools/merger_status_candidate.go` (new) — the operator model render
- `internal/tools/merger_status_disposition.go`, `merger_status.go` — verdict words, split withheld reasons
- `internal/tools/branch_attribution.go`, `repo_sweep.go` — carry the measured `Held` + worktree path
- `internal/tools/merger_status_candidate_test.go`, `merger_status_trust_test.go`, `merger_status_holder_note_test.go` (new/changed) — behaviour + retired-phrase guards
- `internal/tools/merger_takeover_state_test.go` — stale zombie assertion repaired (render unchanged)
- `cmd/agent/cli_worker_control.go` — worker lock-note passes the pid it names
- `devdoc/merger-status-operator-text.md`, `tasks/01a0b182-report.md` — this report

## The complaint, and what was actually wrong

One line carried it:

```
repo:        /home/user/work/GoPi
  pending:   3 branch(es) the next sweep would act on (1 already in master → the ref is pruned)
    ...
  disposed:  ...
    fix/merger-tick-takeover-gate (live-holder) — a live run is working in its checkout
```

The operator's questions were "what does `live-holder` mean to me?" and "what
does *a live run is working in its checkout* mean?" — and the second question is
the diagnostic, because **that sentence was not a measurement of the checkout at
all**. It was two different probes welded into one claim:

| half | what it is | measured how |
|---|---|---|
| `(live-holder)` | a **ledger verdict** — some task ROW's recorded runner resolves to a live process | `scratch`/`branch_attribution` three-factor identity (`pid`+`starttime`+`cmdline`) |
| "a live run is working in its checkout" | a claim about the **checkout directory** | **nothing** — no probe had looked inside the directory |

And the same surface printed, elsewhere, that holder liveness was *not* measured
here. Both sentences were in the output at once.

**Measured on the live repo while writing the fix**: both branches the line
flagged had **zero** processes with a working directory inside their checkouts,
and one of the two flagged rows was already `completed`. So the confident
sentence and the honest disclaimer were describing different axes, and the
confident one was wrong on the evidence.

## The operator's model IS the contract

The epic states it, and it is now what the render computes:

> a branch either (a) is not protected by a worktree, so it **IS** a candidate
> for the ff-merge trial, or (b) is under a worktree, so it is **NOT** a
> candidate. That is the takeover's whole duty.

Plus the second duty the epic asks of the resident merger — *"judging a little
beyond a simple worktree management … verify if a worktree is still under
development"* — which is a question about **processes inside the checkout**, not
about ledger rows.

## What landed

### 1. Carry the measured ground to the render (`79d79119`)

`branchAttributionCheckout` already computed the holder scan (the shared
`scratch.HeldByProcesses` /proc sweep the worktree arm uses for safety) and then
**dropped it before any render saw it** — that drop is the whole bug. `d.Held`
and the checkout path now ride out on `BranchAttributionLine`.

No second probe was added. One measurement, one source (Rule 13/14).

### 2. Answer the merge question per branch (`79d79119`)

New `internal/tools/merger_status_candidate.go` renders, for every attributed
branch:

```
  candidates: 3 of 8 branch(es) answer the merge question — a candidate has no checkout holding it, and the next sweep tries its ff-merge:
    - feat/merger-takeover-identity-stats — candidate: no checkout holds it — the next sweep tries the ff-merge against master
    - feat/branch-scoped-smoke-gate — NOT a candidate: a checkout holds it (/home/user/workspace/gopi-smoke-scope) and NO live process is inside that checkout — no ledger row names this branch (a gap: the run holding this checkout should record it on its task); the merger can merge it only after that checkout is released
    - fix/merger-status-operator-text — NOT a candidate: a checkout holds it (/home/user/workspace/gopi-merger-status-text) and a live process IS working in that checkout — task 01a0b182-…; the merger can merge it only after that checkout is released
  monitor:   4 of 6 head(s) sit in a checkout: 1 with no process inside (those runs are over — each branch's remedy is named above) and 3 being worked in right now
```

Three things to notice, because each is the fix for one clause of the complaint:

- **the answer is yes/no first** — the operator's question, answered before any
  reason;
- **the occupancy clause is a measurement** (`IS working in that checkout` /
  `NO live process is inside that checkout`) and it *varies with reality* —
  verified on the live repo both ways, in one run (3 held-with-process,
  1 held-with-nobody);
- **`monitor:`** is the "little branch-development monitoring" asked for: counts
  of measurements, not opinions.

### 3. Retire the wire vocabulary (`79d79119`)

`live-holder` is a **wire value**: it is compared, counted and persisted, so it
may not be renamed — but a status surface's whole job is to be readable. So the
verdicts map through ONE table (`verdictOperatorWords`):

| wire verdict | operator words |
|---|---|
| `live-holder` | an unfinished run still owns it |
| `owner-ended` | its run is over, the checkout is still there |
| `owner-record-only` | only a ledger row remembers it (no checkout) |
| `unowned` | no ledger row names it |
| `ungoverned` | could not be determined |

An unknown verdict prints *"unclassified verdict"*, never the raw tag.

The withheld-reason for a live-holder was **split into three**, because the two
halves of the old sentence can disagree:

- the row's runner is alive **and** a process is measured inside the checkout →
  *"a live process IS working in its checkout (measured) and its task row's run is still alive — the work is genuinely in progress"*;
- the row's runner is alive but **nobody** is inside the checkout → *"NO process is inside its checkout (measured) — the checkout registration is what withholds the branch"*;
- the checkout could not be measured → names the LEDGER as the source and says
  the checkout was not measured.

### 4. The same defect, found live on the *other* line (`d573e81a`)

While verifying, the surface printed this about itself:

```
lock holder: pid 396084  (stale: the process is gone; the lock is FREE)
takeover:    pid 396084 running for 1m0s; …
```

The **same pid**, declared gone on one line and running for a minute on the next,
by one command about one machine. Measured: `/proc/396084/stat` read state `S`,
and `flock -n /home/user/.gopi/serve.lock` **succeeded** — the flock really was
free while the pid really was alive.

Root cause is identical in shape: `LockProbe`'s `live=false` is a measurement of
**the lock** ("the kernel's lock table names no live holder for this inode"); the
one-boolean note turned it into a claim about **the pid** ("the process is gone")
that no probe had made. `LockHolderNote` now takes the pid it names and probes it
with the package's one zombie-aware predicate (`pidAlive`):

```
(stale: the pid named here is gone; the lock is FREE)                       [pid gone]
(the lock is FREE — no live process holds the flock; the pid named here is
 STILL ALIVE, so it is not the holder)                                      [pid alive]
```

Both branches were observed live: `410497` genuinely gone, `396084` genuinely
alive with a free lock.

### 5. The branch↔task registration gap, named as a gap

The epic proposes that claiming a task and creating its worktree is a milestone
to record on the task row (so the merger can join the two instead of guessing).
The render now says, when no row names a branch:

> *no ledger row names this branch (a gap: the run holding this checkout should
> record it on its task)*

— reported, not silently attributed to nobody. Live example above
(`feat/branch-scoped-smoke-gate`).

## Verification

| gate | command | result |
|---|---|---|
| build | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | `BUILD OK` |
| vet | `go vet ./internal/tools/ ./cmd/agent/` | clean |
| standards gates | `go test ./internal/standards/ -count=1` | `ok` (after syncing master) |
| tools package | `go test ./internal/tools/ -count=1` | see report |
| live render | `./bin/gopi merger --status` | both occupancy directions observed in one run |
| live lock line | same run, pid `410497` gone / `396084` alive | both clauses observed |
| retired phrase absent | `./bin/gopi merger --status \| grep -c live-holder` | `0` |
| same render, both directions | one run named `candidate: no checkout holds it` ×3, `NOT a candidate: … a live process IS working in that checkout` ×1, `NOT a candidate: … NO live process is inside that checkout` ×3 | observed |
| the render tracks reality on the MEASURED axis (the epic's "monitor" duty) | three live runs: (1) `docs/merge-resolve-effort-audit` read "NO live process is inside that checkout", (2) minutes later "a live process IS working in that checkout" (an agent began working in `/home/user/workspace/gopi-resolve-audit` in between), (3) later again "candidate: no checkout holds it" (its worktree was released) | observed — three states on one branch across the session, two of them in ANOTHER run's checkout, so the measurement is not merely detecting this session's own shell |
| this run's own branch flipped both ways | `fix/merger-status-operator-text` read "NO live process is inside that checkout" while nobody worked in it, then "a live process IS working in that checkout" while this run's shell held that cwd | observed, no source change between readings |
| branch↔task gap line | NOT observed live in this run — every checkout-held head happened to be ledger-attributed at both readings, so the held-and-unowned branch the clause needs was absent | covered by `TestMergerStatusCandidateAnswerIsTheOperatorModel`, which feeds a held `ungoverned` line and asserts `candidateNoRowNote` — PASS (see job-11 | job-12). Stated as not-live-observed rather than implied observed. |
| regression suite | `go test ./internal/tools/ -run 'MergerStatus\|Candidate\|Verdict\|Disposition\|BranchAttribution\|RepoPreview\|Zombie\|LockHolder\|HolderNote' -count=1` | `ok github.com/nick/gopi/internal/tools 39.782s` on the MERGED tip (365e697d) |
| `go build ./...` / `go vet ./internal/tools/ ./cmd/agent/` / `go test ./internal/standards/` | — | all clean/ok on the merged tip |
| master merged in | `git merge master` | clean (master had only docs/briefings since the branch point); branch now at master's tip |
| full package `internal/tools` (unrestricted) | `go test ./internal/tools/ -count=1` | **NOT green — and NOT this branch's.** Two failures reproduce identically on a clean detached worktree of master's tip `8b0011a3`: `TestFirstOpenSyncModeDecisionTable` ("= NORMAL, want OFF" — it still asserts the retired tmpfs scratch world) and `TestMergeBuildGate_GoCacheLeavesNoScratchCache` (the merge build gate's cache lives inside the collected scratch tree). Both files are untouched by this branch's diff. Dispatched as bug `01a0b1bf-5170-71b1-b4dc-5891f20c3f60`. A loaded host additionally pushes the whole package past Go's 10m budget (`panic: test timed out after 10m0s`) — that is row `01a0aba7`'s known issue, not a merge-status defect. |

## Not Done

- **A shared `task process is live` helper.** The epic suggests one common
  function over the three-factor identity, since several callers need it
  (`branch_attribution`, `taskproc`, `taskrunguard`, `finish_cleanup`). Those
  callers each have a *different* degradation ladder today, and unifying them is
  its own task with its own red proofs — not a drive-by edit in a status-text
  fix. The epic explicitly allows this: *"this can be an extra task, if worker
  feel this is too big to fulfill."*
- **The branch↔task milestone write.** The epic's proposal — the run records its
  worktree/branch on the task row at claim/creation time — is a **writer-side**
  change in the task lifecycle, and this branch is the **reader-side** answer
  (the gap is now *visible* instead of guessed). Named above; worth dispatching.
- **`TestMergerStatusDoesNotCallAZombieRunning` was red at master**,
  independently of this branch: the identity phrase merged by `8ef4ad54` sits
  between the pid and the state word its assertion expected. Reproduced in a
  clean detached worktree of master.
  **Repair state (be precise, two things happened):** this branch repairs the
  stale assertion in `84df4f02` (assert the pid AND the measured state word
  separately; the render is unchanged and was never wrong), *and* the same
  finding was dispatched as bug task `01a0b19d-46bc-7cec-9a03-2802131eb1d8`
  (Rule 26) because the red lives at master and reaches it whether or not this
  branch merges. If `84df4f02` lands first, that row is a duplicate — the
  executor should confirm the assertion is already repaired and close it.

## Dispatched follow-ups (Rule 26 — this branch does not own them)

Both proposals the epic floats beyond the surface text are now their own rows, so
this branch's report does not have to carry an open-ended "TODO":

| row | what it closes |
|---|---|
| `01a0b1b1-2a7e-7c17-8e06-11ae80462a30` | the branch↔task registration gap: a run writes its branch/worktree onto **its own task row** at claim/creation time, so `branchAttribution*` resolves every worktree-held head. Measured gap this branch reports but cannot close: one live render named 4 `attributed:` heads *and* 2 `unowned:` heads in the same repo. |
| `01a0b1b1-2c57-73f7-a031-abcc58776a2d` | one common "is this task's recorded process live?" helper (`pid`+`starttime`+`cmdline`, via `taskIdentityVerdict`) so call sites stop re-deriving it — with the Rule-15b requirement that "not measured" never collapses into "dead", the exact conflation this whole fix was about. |

Both are system-layer (claim path / `internal/tools` internals) — the peerbox side
of Rule 28, which is why they are dispatched rather than landed here.
