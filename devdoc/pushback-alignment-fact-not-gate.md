<!-- doc-class: historical -->
# Push-back alignment: a fact, not a gate (the third gate stands down)

**Task:** `01a12a546ff373d191ed39b5890f4dd6` · **Lane:** `internal/tools/task_sync_git_pushback.go` (`gitPushbackPlan`)

## What changed

`gitPushbackPlan` asked three questions per local branch, and the third was a
GATE:

| # | question | predicate | before | after |
|---|---|---|---|---|
| 1 | is it already integrated? | `merge-base --is-ancestor <branch> <mirror>` | skip (do not resurrect) | unchanged |
| 2 | does it share history with the peer's trunk? | `merge-base --all <mirror> <branch>` | **refuse** | **refuse** (unchanged) |
| 3 | would it fast-forward onto the peer's master? | `merge-base --is-ancestor <mirror> <branch>` | **refuse** | **report a fact, push anyway** |

The gate's refusal line was

```
%s: not fast-forwardable onto master — push refused; merge master into it (or rebase it onto master) and re-deliver
```

It was removed, not softened. Nothing "failed" survives: the branch is pushed,
appears in `Pushed`, and its entry states the alignment fact.

That line still appears in `devdoc/commit-path-ancestry-gate.md` — correctly, as
the quoted output of a PRE-CHANGE control run. It is a record of what that binary
printed, not a description of this lane's current behaviour; this document is the
live one.

## Why (5-why, in order)

1. *Symptom*: a branch carrying real work and cut from a base the peer has since
   moved past is reported as a failure of this lane and never crosses.
2. *Mechanism*: gate 3 answers "can the peer's `merge --ff-only` land this?" —
   which is a TRUE and USEFUL fact — and then converts a "no" into a refusal of
   the hand-over itself.
3. *Norm (the violation)*: whether a hand-over is worth making is the RECEIVER's
   decision. The receiving side already applies its own rule (`repo_sweep.go`
   lands with `merge --ff-only`), so a branch it cannot land yet is a fact it is
   entitled to see, not one the sender may hide. This lane's job is to move
   bytes and report truthfully.
4. *Aggravator*: the refusal line named a remedy in a loop — "merge master into
   it … and re-deliver" — which the peer's own landing path does not need; the
   operator follows it, and the same line is printed again.
5. *Root*: an observational question was wired to a control-flow decision. The
   fix keeps the observation and drops the decision.

## What did NOT change (the two real refusals)

Both stay refusals, and both keep their own red-proof tests
(`task_sync_git_pushback_ancestry_test.go`, `task_sync_git_pushback_peeref_test.go`):

- **unrelated history** — no common commit with the peer's trunk. Such a ref
  cannot be landed by anyone (`git merge` exits 128, `fatal: refusing to merge
  unrelated histories`), and the remedy is terminating: discard the ref.
- **peer-ref NAME COLLISION** — the peer owns `refs/heads/<name>` at a tip that
  neither contains nor is contained by ours. That one needs a human decision,
  and this lane must never resolve it itself: no force, no rename-fork.

The peer-ref gate is now asked on **every** pushed branch (aligned or not) — it
answers a question about the peer's ref *namespace*, not about our alignment.

## The report (machine-readable halves)

The alignment fact now has one machine-readable representation, in both the
flush and the public report:

```
gitPushbackFlush.FastForwardable / NotFastForwardable
    → GitPushbackReport.BranchesFastForwardable   `branches_fast_forwardable,omitempty`
      GitPushbackReport.BranchesNotFastForwardable `branches_not_fast_forwardable,omitempty`
    → GitPassReport.BranchesFastForwardable / BranchesNotFastForwardable   (client pass)
    → CLI faces: `ffable=[…] not_ffable=[…]` (harvest line) and the pass line
```

A pushed branch appears in exactly one of the two; a branch in neither was not
pushed by the flush. They are **not** `BranchesFastForwarded`, which answers the
different question "did the push update a peer ref the peer already owned" — a
branch the peer has never seen is pushed *and* fast-forwardable.

Prose side: the `Pushed` entry carries the fact

```
feat/behind (does not contain the peer's master at 05e9192 — the peer's own landing
            call decides it: the landing operation is `merge --ff-only`, so an
            unaligned branch parks there until it is aligned)
feat/aligned (contains the peer's master at 05e9192 — the peer's `merge --ff-only`
            can land it as delivered)
```

The positive half is rendered **only when the same flush pushed an unaligned
sibling**. Rationale: an all-aligned flush (the ordinary case) keeps the bare
branch-name entry that existing readers and tests rely on, and when the flush
does mix the two, EVERY entry states the fact — so one report answers "can the
peer land this as delivered?" for every branch it delivered, without a note
written over the ordinary case. When a fact's own value (the peer tip) could not
be read, the note is withheld and the machine-readable half still carries the
name — Rule 15: an unmeasured premise is never printed as a finding.

## Tests

- **New**: `internal/tools/task_sync_git_pushback_fffact_test.go`
  - `TestPushbackBehindMasterBranchIsPushedNotRefused` — the branch reaches the
    peer at our tip, its entry states the fact with the peer tip, the not-ff-able
    half names it, the ff-able half does not, no fork is minted, and after
    aligning, the same flush flips the fact to ff-able.
  - `TestPushbackFfableBranchIsPushedWithFfFinding` — the positive control: an
    aligned branch is delivered and lands in the ff-able half.
- **Rewritten spec**: `task_sync_git_pushback_test.go`'s
  `TestPushbackRefusesBranchBehindMasterRatherThanDeliveringIt` became
  `…IsDeliveredWithTheFfFact` (the old name is gone so nobody greps a contract
  that no longer exists).
- **Unchanged red proofs**: ancestry refusal, peer-ref collision, test fence.

Red-then-green was produced the honest way — the new assertions were run against
the UNCHANGED production code (a temporary test-only shim supplied the two new
symbols so the file compiled) and failed on exactly the old behaviour:

```
--- FAIL: TestPushbackBehindMasterBranchIsPushedNotRefused
    a behind-master branch must be delivered, not refused: pushed=[] failed=[rework/behind:
    not fast-forwardable onto master — push refused; merge master into it (or rebase it
    onto master) and re-deliver]
--- FAIL: TestPushbackFfableBranchIsPushedWithFfFinding
    an aligned branch must be reported in the fast-forwardable half: []
```

## Rule 31 — the surface was really run

`gopi task push` (real binary built from this branch, private sandbox profile
via `gopi tool sandbox create`, the client role resolved from that profile),
against a fixture bare peer served by a local `GIT_SSH_COMMAND` relay. The relay
is Rule 29-safe by construction: it rewrites the peer repo path — **both**
spellings — in the trailing `git-upload-pack`/`git-receive-pack` argv element and
runs it LOCALLY, so the fixture is served in-process and the real peer repo is
never addressed. The argv was measured before it was trusted:

```
RELAY argv: -G -o SendEnv=GIT_PROTOCOL peerbox
RELAY argv: -o SendEnv=GIT_PROTOCOL peerbox git-upload-pack '/home/user/work/GoPi'
```

i.e. the remote command is ONE argv element carrying the **expanded** path (not
the literal `~/work/GoPi` the lane composes) — a relay that rewrote only one
spelling addresses the live repo. An early run did exactly that and read the real
peer's refs; it was verified afterwards that nothing was written there
(`refs/heads` = master only, both branch names ABSENT, reflog with no push
entry), and the relay was fixed before any push was attempted.

Observed output (verbatim, `task-report.md` carries the raw run):

```
git-client: pushed=[feat/aligned (contains the peer's master at 05e9192 — …) feat/behind
(does not contain the peer's master at 05e9192 — …)] ffable=[feat/aligned]
not_ffable=[feat/behind] pull:Already up to date.
  [ok] push: 2 pushed (idempotent)
EXIT=0
```

and in the same fixture with a collision + an orphan present, the two refusals
still refuse and the peer's own ref is untouched:

```
push_failed=[feat/collide: NAME COLLISION — the peer's refs/heads/feat/collide is at
c3536c7 while ours is at 0f682c8 … | feat/orphan: shares no common ancestor with master …]
  [FAIL] push: 2 branch(es) failed: …
EXIT=1
peer refs after: feat/collide c3536c7   (the peer's own tip, unchanged)
```

The old line's unperformable advice (`merge master into it … and re-deliver`) does
not appear anywhere in either run.

## Not done / honest limits

- The peer-ref gate runs on branches it previously never saw only because gate 3
  no longer filters them first; its own refusal set is unchanged.
- The unaligned branch parks at the peer until someone aligns it. That is the
  point of the fact (the peer reports it), but an operator who never reads the
  report sees only a branch parked — the previous design sent the same branch
  back to the sender instead, which is what this epic removes.
- `BranchesFastForwardable` is computed per flush while the mirror exists; a
  caller reading the JSON long after the pass must not re-derive it from git.
