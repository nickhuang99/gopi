<!-- doc-class: historical -->
# The sweep's ladder order, and the two arms that answer different questions

Task: **01a130ae-be52-76a9-bd74-584f08f78b37** (branch `task/01a130aebe5276a9bd74584f08f78b37`).
Surfaces: `internal/tools/worktree_sweep.go` (`worktreeSweepJudge`,
`worktreeSweepLiveSessionRow`, the `worktreeSweepRule` constants) and
`internal/tools/worktree_sweep_unowned_live_session_test.go`.

---

## 1. The defect, measured

Four one-shot **detached** arm worktrees carrying uncommitted mutation material sat
in `/home/user/workspace` (observed 2026-10-12 23:14, `gopi-01a12bfd-verify2`,
`gopi-01a13019-teeth{,2}`, `gopi-bfd-repro`; a fifth, `gopi-01a13019-base`, was
clean and was already gone before this run's first `git worktree list` at 23:48 —
not this run's doing, this run never ran `--apply`). No row names them: they carry
no branch (detached HEAD) and no `deliverable` points at them. The dry run said:

```
report      [dirty-worktree]  age=21h /home/user/workspace/gopi-01a12bfd-verify2 (dirty (uncommitted work) — left in place: M  …)
keep        [live-session]    age=0h  /home/user/workspace/gopi-01a13019-base   (… live task 01a13025-… still has a run standing … — its own session's ground)
report      [dirty-worktree]  age=2h  /home/user/workspace/gopi-01a13019-teeth  (dirty …)
```

Two coupling defects, in one ladder:

- **(a) the arm's own rule name.** The unowned escalation (`worktreeEscalateOrphan`)
  was stamped with `worktreeRuleDirty` — the same rule as the ordinary report of a
  dirty tree whose owner IS on the host. An operator reading the plan could not
  tell the two apart, and the two call for different actions (escalate: nobody will
  come back for this; report: its owner is mid-delivery).
- **(b) the ladder ORDER, the load-bearing half.** The `live-session` arm sat
  between the ownership rungs (③) and the dirty guard (③O/④), and returned the
  FIRST live row with a standing run — so **any** live row on the host (this host
  always has one: the resident worker's own) kept **any** registration, whatever
  that registration was. A registration no row names therefore never reached the
  dirty guard and never reached the escalation, and the sentence the operator read
  — *"still has a run standing … — its own session's ground"* — was false: that
  row's ground was a different directory. The "own session's ground" concept was
  implemented for chat grounds (①C/④C) only; nowhere else did the ladder ask
  whether the liveness it found had anything to do with **this** registration.

## 2. What the ladder asks, rung by rung

The rule the ordering encodes: **a rung may only decide on evidence about THIS
registration; evidence about the actor or about somebody else comes last, and may
only keep what its own evidence names.**

| rung | question it answers | whose fact |
|---|---|---|
| ① main / bare / ①D dead | is this a working tree this sweeper owns at all? | the registration (git's own bookkeeping) |
| ① locked | did git mark it do-not-touch? | git |
| ① root scope | is it inside a declared root? | policy |
| ①B self-checkout | would freeing it delete the ground under THIS process? | **the actor** |
| ①C chat ground (live holder / inert) | is this chat session's own ground still in use? | the registration (its own session) |
| ② live holder | is a live process inside it? | the registration (strongest presence evidence) |
| ③ ledger ownership / deliverable tie | does a live row's RECORD name it and still hold ground? | the ledger's own fields |
| ③O unowned escalation | dirty + no row anywhere owns it (+ aged) | the registration |
| ④ dirty report | uncommitted work — never destroyed | the registration |
| ④C chat ground | a chat's own version of ④ | the registration (its own session) |
| ⑤ age floor | is it younger than the threshold? | the clock |
| **⑤+ live-session** | **is there a standing run whose OWN BRANCH is this registration's branch?** | **somebody else — only via a name tie** |
| ⑤+ terminal-row reclaim / unowned reclaim | nobody owns it, nobody holds it, clean and old | the registration |

**⑤+ is the arm this task moved.** It used to sit directly after ③; it now runs
*after* the age floor, and it keeps a registration only on a tie that is about the
registration itself: a live row whose **branch** IS this registration's branch
(the 2026-09-17 concurrent-session shape, row `01a0af50`: one row, two sessions, the
deliverable naming only the other session's worktree). A branchless (detached)
registration has no such name and is therefore never read through this arm — which
is precisely how the measured residue reaches ③O and ④ instead of a false keep.

Two consequences worth stating, because they are choices rather than accidents:

- **A dirty registration is escalated before the session arm can keep it.** A tied
  dirty registration (the ordering pole in the test) escalates; the escalation
  never removes anything, and the branch stays. Destroying uncommitted work behind
  a live holder was never on the table.
- **The arm also judges the dead-registration rung** (`reg.Prunable || ho.DirGone`),
  where only the branch can implicate a live row. That call site passes `reg`
  too; there the tie is the only tie that can exist (there is no directory), and a
  keep there costs one git-bookkeeping entry.

## 3. Rule names

`worktreeRuleOrphan` (`"orphan-unowned"`) is the escalation's own rule, distinct
from `worktreeRuleDirty` (`"dirty-worktree"`), which stays what it always was: the
report of a dirty registration (owned or unmeasurable). Both are exported-shaped
constants beside every other rule name (Rules 1b/13/14 — no bare strings at a
comparison site, and the finding-root gate's arm inventory lists them, so a rule
that is never cited by a gate case fails the build).

## 3b. The naming clause: an escalation that will not say which tree it is about

The escalation's sentence opened `(branch detached, age 21h)` for every detached
registration — and every detached registration is detached, so the clause was true
of all of them and identified none. Measured on the four residue directories: four
identical openings, four different trees, and an operator told to "commit it, copy
it elsewhere, or remove it by hand" with nothing to recognise the tree by.

`worktreeSweepOrphanBranch` now renders the branch when there is one and, when
there is not, the commit the registration's HEAD points at —
`detached HEAD at commit da3f952b0391`, through the package's existing short-hash
helper (`branchDeleteShortHash`, 12 hex digits: one width for one hash, Rule 14).
`worktreeSweepDetachedLabel` (an alias of git's porcelain `detached` token, used at
exactly this one call site) was deleted with the clause; `worktreePorcelainDetachedToken`
stays, because the porcelain PARSER still needs it.

The `Head` field the clause reads was already there — `git worktree list --porcelain`
prints it on the `HEAD` line and the parser has always stored it — so this is a
renderer reading a fact the registration already carried, not a new measurement.

## 4. Verification

- Same fixture, both halves:
  `internal/tools/worktree_sweep_unowned_live_session_test.go` —
  `TestUnownedRegistrationIsNotDecidedByAnUnrelatedLiveSession` (four cases: the
  detached residue aged ⇒ escalate; young ⇒ dirty report; clean ⇒ plain reclaim;
  and the tied dirty case ⇒ the dirty guard decides first) plus
  `TestLiveSessionArmStillProtectsWhatItsOwnEvidenceNames` (the row's own
  deliverable, and the row's branch as the arm's own evidence — so the fix cannot
  have meant "delete the protection").
  That file names only PRE-EXISTING constants, so it compiles — and fails — on the
  pre-fix tree; the verbatim RED→GREEN pair is under
  `~/.gopi/sessions/01a130ae-be52-76a9-bd74-584f08f78b37/redegreen/`.
- Real verb (`gopi task sweep-worktrees --dry-run --min-age-h 1`, both binaries,
  never `--apply`): the four residue directories are already `escalate`; the only
  difference the fix makes to that output is the rule name —
  `[dirty-worktree]` → `[orphan-unowned]` — and the naming clause —
  `(branch detached, age 21h)` → `(detached HEAD at commit da3f952b0391, age 21h)`
  — which is defects (a)'s two halves exactly.
- `make test-budget PKGS='./internal/tools ./internal/testutil ./cmd/agent'`.

## 5. What this run did NOT do

No `--apply`, no `git worktree remove`, no `--force`, no branch deleted or archived:
the four residue directories are still on disk, byte for byte, and the judgement
that they are orphaned residue is now readable off the plan instead of being
hidden behind an unrelated row's liveness.
