<!-- doc-class: normative -->
# The completion-witness criterion — telling a `completed` claim apart from a claim that delivered nothing

Task `01a182bf-1814-7434-9c24-56f4948b3ffe` · cut point `9b028a7` · measured `2026-10-29`

## The question, and the incident that made it askable

After commit `20c15007c`, this ledger held 46 `completed` rows. The epic wanted to
know which of them could be told apart from a claim that delivered nothing — and
the immediate trigger was row `01a18253-f7c4-7cf7-8cb3-868647a25401`, which read
`completed` while **every address the ledger names for it was gone**:

```
$ git -C /home/user/work/gopi rev-parse --verify --quiet refs/heads/task/01a18253f7c47cf78cb3868647a25401 ; echo exit=$?
exit=1
$ git -C /home/user/work/gopi rev-parse --verify --quiet refs/remotes/origin/task/01a18253f7c47cf78cb3868647a25401 ; echo exit=$?
exit=1
$ ls -d /home/user/workspace/01a18253-f7c4-7cf7-8cb3-868647a25401
ls: cannot access '/home/user/workspace/01a18253-f7c4-7cf7-8cb3-868647a25401': No such file or directory
```

## The finding that reshaped the criterion: ref-absence is NORMAL, not a defect

The first version of the census asked three questions — does a ref name the recorded
branch, does a commit carry the row's `task:` trailer, does the ground directory
still exist — and it reported `witnessed 42 / WITNESSLESS 0` for 42 rows. That
answer was **almost right for the wrong reason**, and the second measurement said why:

```
# rows measured: 46
no ref carries the branch: 46
```

**All 46.** A delivered close-out legitimately ends in exactly this state: the clone
channel publishes `HEAD:master` and pushes, then finish-cleanup removes the worktree
and reclaims the address ref. So "no ref names the branch" is the ordinary shape after
a **successful** delivery, and a criterion built on it would have called every
delivered row a defect.

Two consequences followed, and both changed the code rather than the prose:

1. **The ground directory is not a witness.** A worktree that still exists says
   nothing about whether the run *produced* anything — an untouched ground is precisely
   the false positive this epic is about. The census had been counting it. It was
   removed from both the census and the production arm.
2. **The witness that survives everything is the commit's own trailer.** Row
   `01a18253` has one:

```
$ git -C /home/user/work/gopi show -s --format='%(trailers:key=task,valueonly)' 20e8328916328c3dc04e5fae6524ce14288c5882
01a18253-f7c4-7cf7-8cb3-868647a25401
$ git -C /home/user/work/gopi merge-base --is-ancestor 20e8328916328c3dc04e5fae6524ce14288c5882 refs/remotes/origin/master ; echo exit=$?
exit=0
```

So `01a18253-f7c4` is **not a lost row**: its ref and its ground are gone, its work is
on `origin/master` under a commit whose trailer names it. That is the class the epic
called "landed under a maintenance commit", and it is not the defect class.

## The criterion, as shipped

`internal/tools/task_completion_witness.go` asks three witnesses, in the order of how
directly each one knows the answer:

| arm | what it asks | why it is in the list |
|---|---|---|
| `ref` | does any ref (head or remote) name the recorded branch? | the ordinary mid-flight close-out; cheap, and it covers a row closed before cleanup ran |
| `landed` | does the row's trail record a clone-channel push naming a commit this repo still holds? | needs no directory and no trailer; it is the channel's own account of what it pushed |
| `commit` | does any commit reachable from a ref name this row, in its `task:` trailer or its subject? | **the survivor** — it outlives the ground's deletion *and* the address ref's removal |

Four verdicts, not two:

- **`witnessed`** — one of the three arms answered. Silent on the trail.
- **`concluded`** — none answered, **but** the row's own recorded reason is the clone
  channel's measured `nothing to deliver` exit. This completion rests on a
  *measurement* the channel took (`git rev-list --count HEAD --not origin/master` → 0),
  which is a different fact from an unbacked claim. Silent on the trail; reported on
  stderr.
- **`witnessless`** — none answered and there is no conclusion. **This** is the defect
  class the epic was about. ONE attributed trail entry
  (`completed-without-witness`) naming the branch, the three arms that were asked and
  found silent, and what the record does and does not mean.
- **`unknown`** — a read refused: the repository or the ledger could not be read, or
  the row names no repository at all. Reported, never recorded as the defect.

The measured result on the live population:

```
# rows measured: 46
witnessed:               39
concluded (measured):     7
WITNESSLESS:              0
```

## Two design decisions a later reader will otherwise try to "fix"

### `completed` is never turned back into `failed`

The arm RECORDS an absence and changes no status. An operator reading this will
predictably want it to refuse the claim, and that would re-open a defect this repository
already paid for: row `01a18244` fixed a case where an items=0 counter-proof run was
recorded as a *failed* delivery. A `completed` row with no witness is a fact worth
recording, not an error worth reverting — the ledger's job here is to stop being
*silent*, not to become stricter than the run's own conclusion.

### The `concluded` verdict exists because the epic demanded the distinction

Without it, all 7 of the measured concluded rows would read `witnessless` and the
epic's question ("which rows are unproven?") would answer 7 when the truth is 0. The
distinction is the deliverable.

The cost is honest and stated in the code: the conclusion is recognised by the
channel's own composed sentences (`carries no commit to deliver`,
`clone channel: nothing to deliver`). A third exit arm or a reworded one would make the
`concluded` count fall to 0 — and that 0 is **printed**, so the drift is visible rather
than silent. It is not a structured criterion, and it is not sold as one.

## Where the seam is, and why it is after the write

`cmd/agent/task_completion_witness_seam.go` is the one place a run tells the arm the
two facts it cannot derive: the status being claimed, and — via the `taskRunConclusion`
latch set at the nothing-to-deliver exit in `cmd/agent/cli_tasks.go:3547` — the
conclusion that completion rests on.

`recordCompletionWitness` is called from `writebackOutcome` **after** the terminal write
has landed (`cmd/agent/cli_tasks.go:4167`), never before. The arm annotates a completed
row; it does not gate one. That ordering is the whole reason the "never revert
`completed`" constraint is structurally true rather than merely promised.

## Regression tests, and the red proof

`internal/tools/task_completion_witness_test.go` covers the criterion (defect class,
trailer witness, concluded, live-ref silence, no-repo unknown, failed-not-judged).
`cmd/agent/task_completion_witness_seam_test.go` drives the **production door**
(`writebackOutcome`), because an arm that is correct in isolation and never called is
exactly the defect Rule 31 names.

The red proof mutated the arm's witnessless branch to `CompletionWitnessed` and re-ran
the defect-class test:

```
--- FAIL: TestCompletionWitnessClaimWithoutArtefactIsRecorded (0.07s)
    task_completion_witness_test.go:96: the row claims completed with NO artefact behind it
    (branch feat/dead-holder resolves to no ref, no reachable commit names it) — verdict "witnessed" ()
```

Restore was verified by **content identity** against the pre-mutation reading
(`sha256sum` unchanged, and `grep -c 'RED PROOF MUTATION'` → 0), not by an index-relative
diff.

## The census and the production arm must ask the same question

`internal/tools/testdata/completed-witness-census/main.go` is a separate `main` package
(read-only over the live ledger and the live repo; it deliberately does not import the
tools layer). Two drifts were caught during the work and both are recorded here because
they are the failure mode of a hand-mirrored census:

- A census without the `landed` arm called row `01a1836a` witnessless while the
  production arm would call it witnessed — its landed record names commit `c63f60ab`,
  which is reachable from `origin/master`, and it carries **no** `task:` trailer. The
  census was aligned to the production arm, not the other way round.
- The census counted the ground directory as a witness; the production arm never did.
  Removed.

Neither drift is detectable by a compiler. What makes them visible is that the census
PRINTS each arm's count — a source of witnesses going to 0 (the trailer index, the
landed index) is on the page rather than behind a verdict.
