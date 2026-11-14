<!-- doc-class: historical -->
# Worktree & cwd resource management — requirement reconstruction

Module `[12/15]` of the reverse-requirement-analysis series. Ledger row
`01a0ab12` (`[12/15] MODULE: worktree / cwd resource management`).

**Revision read:** master `f7bd261658c2cc2b27ee084355b08f1f18832be5` — every
`file:line` below was re-read at that revision in this session; every measured
number was produced by a command quoted next to it.

Evidence tokens follow `00-method-and-standards.md` §1: **E1** operator ledger
row, **E2** `GOPI.md` rule, **E3** commit, **E4** code, **E5** devdoc,
**E6** test, **I** inference (with its weakest link), **O** opinion.

---

## Front matter

**Boundary.** The ground a run stands on *on disk and in its process tree*: which
directory a run and every long-lived process it spawns lives in, who else may be
there, where the temporary work goes, and how all of it stops existing when the
run is over — including after a crash.

**In scope.** The creation/protection/close-out of a run's checkout
(`internal/worktreeroots`, `internal/tools/task_finish_cleanup.go`,
`internal/tools/worktree_sweep.go`); the *cwd* half — the injected release
(`internal/worktreerelease`) and the start-directory rule for long-lived
children (`internal/daemoncwd`); the scratch/temp-root half — where temporary
work lives and which device it is on (`internal/scratch`, `internal/config`'s
`tmpdir` chain, `internal/config`'s child-resource ceilings); the borrow ban
(`internal/tools/branch_attribution.go`); and the close-out cleanliness
obligation (`internal/tools/task_reclaim.go`).

**Out of scope (analysed elsewhere).** The merge/park decision and the branch
lifecycle generally (`devdoc/requirements/02-merge-and-branches.md`); the
ledger's own record semantics (`01-task-ledger.md`); build/rebuild and the
`repos` table (`11-build-and-repos.md`), which this document touches only where
it inherits a temp root. Rule 27's device-load ban and the heavy-test
classification (`devdoc/heavy-tests-rule27-gate.md`) are *constraints on tests*,
not part of this boundary — §7 names them only where a test's verdict changes.

**Boundary why.** This is one user-visible responsibility and not a directory:
the operator's question is *"is gopi cleaning up after development, and is my
disk safe?"* — asked in those words [E1: task `01a0975a`] — and it is answered by
four mechanisms that must agree with each other (a checkout, a working directory,
a temp root, a process tree). Split them and each one answers a question nobody
asked. Directory-wise the code is scattered (`internal/tools`,
`internal/scratch`, `internal/worktreerelease`, `internal/daemoncwd`,
`internal/worktreeroots`); responsibility-wise it is one promise.

**Known boundary mismatch (stated, not hidden).** `devdoc/requirements/00-module-map.md:246` assigns
worktree/cwd to **M10**, and this briefing is `[12/15]` — the two id schemes of
`00-method-and-standards.md` §7.2 are both live. This document follows *this*
briefing's scope: it adds the **cgroup/daemon lifetime** and **temp-root** halves
that the sibling document `devdoc/requirements/03-worktree-and-cwd.md` (module
B3, a different, partly parallel series — its §Front matter says so itself)
declared **out of scope** and left to M10. That document owns the
create/protect/close-out chain in depth; this one owns the lifetime, cwd and
temp-resource faces. §6 states where the two disagree rather than smoothing it
over.

---

## 1. What this is for

A piece of work must stand somewhere. The system gives every piece of work its
own copy of the project so two pieces of work never stand in the same place, and
so a half-finished change is never lost to someone else's tidying.

That copy, and every background process the system leaves running, are the most
dangerous things the system owns — not because they are valuable in themselves,
but because they occupy the disk and the process tree long after the person who
started them has forgotten them. So the system makes four promises to the person
who owns the machine:

1. **Two pieces of work never share a working directory**, and work never
   happens in the place other work is happening.
2. **A copy that is finished disappears**, taking the disk back with it; and if
   it *cannot* disappear, the system says why rather than leaving it as a
   mystery.
3. **Everything a run starts, it also stops** — and when it dies badly, nothing
   it started survives to keep filling the disk.
4. **Temporary work never lands on the part of the disk the machine itself
   needs**, no matter which directory a tool happens to choose.

*If this stopped working, the person would notice first by work he believed was
delivered existing on only one machine, then by a machine that runs out of room
with no obvious cause, then by background processes nobody remembers starting —
the shape of all three historical incidents in §4.*

---

## 2. Who is served

**The operator (nick)** — he owns the disk. He needs his work to survive, and he
needs a workspace that cannot silently consume the partition his own system
lives on. His words, filed as work: *"is gopi clean up after development? i.e.
is this just normal worktree like other path? … these are serious questions to
ask"* [E1: task `01a0975a`, and its family `01a095cf`, `01a09c25`, `01a0a36a`].

**The agent (a running session)** — it needs a directory nobody will tear away
from under it, and it needs to be *told* when it cannot have one. Its need is
the exact opposite of the operator's: he wants the directory gone, the running
session needs it kept. Every guard in §3 exists because these two needs
collide.

**A long-lived background process (the merger, a worker)** — it needs a working
directory that is *not* anyone's active copy, so its own existence cannot keep a
finished copy alive forever. This actor is the one whose need was discovered
last, and §4.3 is the incident.

**A future maintainer** — must be able to answer "is this leftover garbage, and
whose is it?" from a record, without archaeology. A leftover with no owner is
indistinguishable from live work [E2: `GOPI.md:1078` Rule 29].

---

## 3. The workflow it sits in

**Entry:** a task is claimed and a run begins. It needs a copy and a working
directory. **Exit:** the copy is gone — or a named reason says why it is still
there — *and* no process the run started is still alive.

1. **Creation.** A linked copy of the repository is created in a declared root,
   on a branch named for the task; creating it is the system's own work, never
   something it waits for a person to do [E2: `GOPI.md:164` Rule 6a],
   [E4: `internal/worktreeroots/roots.go`]. A linked copy is cheap because it
   shares the repository's object store, which is why "one copy per piece of
   work" is affordable at all.
2. **Standing.** The run's working directory becomes that copy. A process that
   must outlive the round that started it is deliberately **not** started there:
   it is given a start directory that is provably off the system partition and
   provably outside every declared root [E4: `internal/daemoncwd/daemoncwd.go:21-23`].
3. **During the work.** The copy may not be taken: a live process holding it or
   standing in it refuses removal, and so does a copy another row has recorded
   as its own [E4: `internal/tools/task_finish_cleanup.go:1586`]. Other work may
   never be pointed at it [E2: `GOPI.md:1078` Rule 29].
4. **Close-out.** The order is the safety argument: guard the row and its
   cleanliness, hand the branch over, then remove the copy [E4: `internal/tools/task_finish_cleanup.go:11-23`].
   Before anything is removed the process is moved out of the directory
   [E4: `internal/worktreerelease/release.go:63`].
5. **Afterwards.** Anything the run failed to release is reaped by an
   age-and-ownership sweep that runs for real by default
   [E4: `internal/tools/worktree_sweep.go:78`, `:89`], and any dirt the run left
   in its own copy is recorded and — under one switch — refuses the run's own
   "I am done" claim [E4: `internal/tools/task_reclaim.go:138`, `:1268`].
6. **Temporary work, throughout.** Every temporary workspace the process or its
   children use is published once, into the process's own environment, and
   measured to be off the system partition [E2: `GOPI.md:717` Rule 22],
   [E4: `internal/scratch/tmproot.go:457`].

**The two arms are not the same operation.** On the machine that *sends* work,
the copy is pushed away and then removed; on the machine that *receives* it, the
branch is the deliverable and outlives the copy. The full chain-level treatment
is the sibling document's (§Front matter); this document's contribution is the
lifetime and resource faces around it.

---

## 4. Why it exists — cited evidence

### 4.1 Two pieces of work must not share a directory — and the main repository is not a place to experiment

The need: *a run's copy is the only place an unfinished change exists, so no
other actor may drive commands inside it.* The main repository hosts work in
progress — it is the shared floor every in-flight task stands on, not a bench —
so using another task's copy as a scratch directory, test target or reference
checkout is forbidden [E2: `GOPI.md:1078` Rule 29]. The mechanism that makes a
borrow **visible** is the attribution classifier, which names both the checkout a
process holds and the row the branch is attributed to, so two rows claiming one
checkout is *reported* rather than resolved [E4: `internal/tools/branch_attribution.go:342`, `:480`].

**The incident behind the rule** is recorded in `devdoc/branch-task-attribution.md`
— the file that both states Rule 29 (`:244`, `:385`) and records the classifier
that detects the borrow. Its own summary of the split is the honest half:
*"the classifier names the checkout a process holds and the ledger row it
attributes the branch to … a mismatch is reported, not resolved"*
[E5: `devdoc/branch-task-attribution.md:244`, `:385`].

*What would have gone wrong without it:* an experiment run in someone else's
checkout is indistinguishable, from the outside, from that task's own work — and
the attribution verdict the merger uses to decide whether a branch may be
recycled would be a lie.

### 4.2 A finished copy must disappear — and its dirtiness must not be confused with a live writer

The need: *the operator's disk is returned when the work is done.* The chain's
own header is the reasoning, and it is load-bearing rather than descriptive: the
row must be terminal and the copy clean, because a dirty copy means a live agent
is still writing there ("dirty > 0 = a live Bond is still writing there — skip,
never touch") [E4: `internal/tools/task_finish_cleanup.go:13-15`].

**The residue this exists to prevent is measured, not imagined.** The sweeper's
own header records the assessment: stale registrations whose tasks had finished
days earlier — *"was 265 MB"* — left by runs that ended without the close-out
step, and *"nothing ever revisits"* a registration whose owner is gone
[E4: `internal/tools/worktree_sweep.go:3-12`]. The sweep is therefore built on
three evidence axes — inside a declared root, no live process holding it, and
the registration's own creation time (default 24 h, [E4: `internal/tools/worktree_sweep.go:89`])
— and never on a name pattern.

**The leak shape the sweep had to grow an arm for** is the one this session
measured *live*. A registration whose directory is **gone** holds no work, and
the ordinary collection path **skips locked entries**, so that shape was
permanently uncollectable: *"prune SKIPS a locked entry, and nothing else ever
revisits it; measured: two entries from 2026-09-13 23:40 still present with
`locked: initializing` after their owners were SIGKILLed"*
[E4: `internal/tools/worktree_sweep.go:566-568`]. The fix is an arm that runs
*before* the lock arm [E4: `internal/tools/worktree_sweep.go:718`] plus a named
unlock repair [E4: `internal/tools/worktree_sweep.go:1870`].

**Observed in this session, one occurrence, mid-run:** the worktree
`/home/user/workspace/gopi-sc-redproof` was listed `locked` at the start of this
session and its registration was gone — with its directory already absent and
`git worktree prune --dry-run -v` printing nothing — by the time it was examined
minutes later. That is the §4.2 mechanism operating on the host: a locked,
directory-gone registration collected without operator action, while no branch
(and no row) was touched.

**And the guard is real on this host right now:** three registrations survive
that no close-out removed — two of them `detached HEAD` at master `9ce8e159`,
sitting in the declared root, each holding no branch of their own
(`git worktree list`, this session). None of the three appears as the deliverable
of any ledger row (three `select count(*) … deliverable like '%<dir>%'` queries
returned 0). **That is the honest state of the promise in §4.2 (see §6 FS3).**

### 4.3 A run's processes must die with the run, and a background process must not stand in a copy

Two needs that look opposite, both traced to incidents whose devdocs are named
by the rule itself.

**The need: independence is judged by the process's own cgroup, never by a proxy
signal.** `GOPI.md:796` Rule 24 states the criterion — the only evidence a
process has left its login session is that its cgroup is not a `session-*.scope`
— and names its own counterexample (`setsid` does **not** change the cgroup, so
the process still dies with the session that spawned it) and its sibling trap
(never probe liveness with a pattern-matching process search; it self-matches)
[E2: `GOPI.md:796`]. **The incidents:** two machine-wide kill incidents —
`devdoc/worker-cohort-kill-20260912.md` and
`devdoc/signal-provenance-20260913.md` — with the mechanism, the rejected
criteria and the choice of landing spot in `devdoc/detach-judgment-rule.md`
[E5: `devdoc/worker-cohort-kill-20260912.md`, `devdoc/signal-provenance-20260913.md`,
`devdoc/detach-judgment-rule.md`, all three named at `GOPI.md:822`].

**The need: a privilege is never the remedy.** `GOPI.md:825` Rule 25 forbids
prescribing elevated privilege as the fix for a resident process that cannot
leave its session — *"a privilege is not a missing ingredient; asking the
operator for one is itself the bug"* — and its table is measured, not inferred:
an elevated start **inherits the cgroup of the unprivileged shell that launched
it**, and the unprivileged migration returns `EACCES` [E2: `GOPI.md:825`].
**The incident devdoc is `devdoc/no-privilege-daemon.md`**, named in the rule's
own provenance line at `GOPI.md:861`; the operator-facing counterexample is the
dispatched task `01a09d00` ("escape cgroup requires super user") whose adopted
text taught the opposite, and the defect's operator-report origin is recorded in
`devdoc/daemon-scope-privilege-hint-20260914.md` [E5: `devdoc/no-privilege-daemon.md`,
`devdoc/daemon-scope-privilege-hint-20260914.md`]. The anti-regression half gates
the **text**: `cmd/agent/daemon_scope_no_privilege_test.go:88` fails a
production string literal that prescribes the privilege.

**The need: what must survive a crash must be enforced by the kernel, not by a
clean exit path.** `devdoc/daemon-e2e-leak.md` records the incident: a SIGKILLed
test binary left real daemons running with their (soon deleted) sandbox files
open, *"filled the tmpfs to 100%"*, and the lesson is stated plainly — *"anything
that must survive a SIGKILL has to be enforced by"* the kernel
[E5: `devdoc/daemon-e2e-leak.md:3`, `:16`, `:25`]. The producer-side counterpart
is the cwd rule: when a spawned process inherits the *launcher's* directory, and
the launcher stands in its own copy, the "is anyone in there?" guard can never
fire for that directory, so it becomes permanently un-recyclable —
*"THE FIX IS A PRODUCER OBLIGATION, NEVER A GUARD WEAKENING"*
[E4: `internal/daemoncwd/daemoncwd.go:7-21`].

*What would have gone wrong without either half:* copies that can never be
collected while an immortal process stands in them, and background processes
that outlive the machine's own health.

### 4.4 Temporary work must not land on the partition the machine needs

The need, in the operator's terms: *the boot partition is not a scratch pad.*
`GOPI.md:717` Rule 22 states it as a **criterion, not a value** — measure the
device of the resolved temp path against the device of the system root, never
trust a spelling — and records its incident and its numbers: *"/ is the 147G
boot partition the 2026-09-11 exhaustion took from 19G to 1.5G free before
another 23.5G of residue accumulated there"* [E2: `GOPI.md:717`]. **The incident
devdocs are `devdoc/peerbox-resource-exhaustion-20260911.md`** and the cleanup
research it produced (`devdoc/research-cleanup-safety-20260911.md`,
`devdoc/cgroup-limiting-not-cleanup-20260911.md`), with the residue audit in
`devdoc/scratch-residue-audit.md` and the ban's shell half in
`devdoc/tmp-ban-scratch-root.md` [E5: those four files].

The mechanism the rule names is the one in the tree: a measurement, not a
warning [E4: `internal/scratch/tmproot.go:457` `AssertTempRootOffRootFS`], with
its shell twin `scripts/lib/tmp-ban.sh` (`gopi_assert_off_root_fs`, `:53`) and
the suite-entry pin in `internal/testutil` [E6: `internal/testutil/tmproot_assert_test.go:75`, `:260`].

**The default was moved for the operator's own reason.** The built-in temp root
is an ordinary directory under the workspace, not the memory-backed tree, and the
code records why: *"an operator who wants the memory-backed scratch tree … sets
the parameter — that choice is theirs, not the default's"* [E2: `GOPI.md:717`, `:752`],
[E4: `internal/config/config.go:345-358`]. The operator's ruling that retired the
memory-backed root as a *creation* target is quoted at the constant itself
(2026-09-18: *"just for a few ms performance? no, no no! don't use tmpfs. change
settings to workspace. now"*), and the constant that carried the old role now
serves only to *recognise and clean* legacy artifacts [E4: `internal/worktreeroots/roots.go:62`, `:74`, `:80`].

### 4.5 "Clean up after yourself" is an obligation the system carries, not a favour

The need: *a leftover is the system's own debt.* `GOPI.md:1120` Rule 30 fixes it
at the level of the run's own ground — before a run reports ready, its worktree
must be clean, and the two shapes of dirt have **opposite** remedies: untracked
entries are litter to remove, tracked changes are an uncommitted deliverable to
commit [E2: `GOPI.md:1120`]. **The incident devdoc is the framework's own
document: the measured case is task `01a0af50` (2026-09-16/17)**, where ~1000
lines of finished, tested work sat uncommitted in an abandoned worktree while the
row read `completed` [E2: `GOPI.md:1160`] — and the rule's own citation pattern
(`:1149`) records that the framework does not re-run a report's gates, it
*records* the sentences that assert a result with nothing behind them.

In code, the obligation is three-valued and never destructive: untracked-only
dirt is residue with a marker [E4: `internal/tools/task_reclaim.go:138`], its
record is written once and never rolls the outcome back, and only one switch
(`GOPI_TASK_RUN_RESIDUE_ENFORCE`, [E4: `internal/tools/task_reclaim.go:1268`])
turns it into a refusal of the run's own `completed` claim; an operator who has
reviewed the tree can allow that one writeback
(`GOPI_TASK_DIRTY_CLOSEOUT_ALLOW`, [E4: `internal/tools/task_reclaim.go:1326`]).
The boundary of the mechanism is stated in the rule itself: *"the gate refuses
the STATUS claim — it never commits, stashes, discards or deletes anything"*
[E2: `GOPI.md:1173`].

---

## 5. Acceptance in user terms

Each is observable without reading code, and each separates a bug from allowed
behaviour.

- **A1 — one piece of work, one directory.** While a piece of work is running, no
  second piece of work stands in its copy, and no tool is ever pointed at a copy
  that is not its own. *Bug:* two rows' work observed in one directory, or a
  command run inside another row's copy.
- **A2 — a finished copy is gone.** When a run ends with its work handed over,
  its copy no longer occupies disk. *Bug:* the row is finished and the copy is
  still there, with no reason recorded.
- **A3 — a copy that cannot be removed names its reason.** Every surviving copy
  is either held by a live process, is a live row's own ground, contains
  uncommitted work, or is inside the age window — and it says which. *Bug:* a
  copy survives with no declared reason, or the reason names the wrong one.
- **A4 — dirt in a run's own ground is the run's debt.** A run that leaves litter
  in its own copy is recorded as having done so, and (under the enforce switch)
  is refused its own "I am done" claim. *Bug:* a run reports ready with
  untracked residue and nothing anywhere says so. **Bug, opposite direction:** a
  run's own uncommitted deliverable is destroyed, or discarded, by the cleanup
  mechanism.
- **A5 — temporary work is off the system partition, measured.** The device of
  the effective temp root is not the device of the system root, the value is
  published to every child, and an unmeasurable root is a loud failure — never a
  silent fallback. *Bug:* a child resolves its temp directory to the system
  root's `/tmp`, or the check answers "safe" when it could not measure.
- **A6 — a run's long-lived children do not keep a copy alive.** A process that
  outlives its round is not started inside any copy or declared root. *Bug:* a
  finished, delivered copy cannot be removed because a background process is
  standing in it.
- **A7 — a background process that dies leaves nothing running.** When the thing
  that started a daemon is killed without warning, the daemon stops too. *Bug:* a
  daemon survives its spawner's death and keeps a file open or a resource busy.
- **A8 — no privilege is ever required or requested.** A degraded detachment is
  announced; an operator who needs the guarantee gets a refusal, not a password
  prompt. *Bug:* the operator is told, anywhere in output or in a task summary,
  that the escape needs elevated privilege.

---

## 6. Gap analysis

### (a) Serves the reconstructed need

- **A5, and it is honestly built.** The criterion is measured with device ids
  and the explicit/implicit classification is carried into the error; "cannot
  tell" is never "safe" [E4: `internal/scratch/tmproot.go:457-470`]. The
  default moved to an ordinary workspace directory with the operator's own ruling
  at the constant [E4: `internal/worktreeroots/roots.go:62`]. The suite is pinned
  at its entry point, and its own tests prove the pin is measured, not asserted
  [E6: `internal/testutil/tmproot_assert_test.go:240`, `:260`].
- **A6, via a producer obligation rather than a weakened guard.** High-value and
  rare: the fix was placed on the *spawning* side, and the file states the
  invariant is never traded for a convenience path [E4: `internal/daemoncwd/daemoncwd.go:7-40`].
- **A1's visibility half.** A borrow is *reported* with both the held checkout
  and the attributed row [E4: `internal/tools/branch_attribution.go:342`, `:480`].
- **A4's non-destructive half.** The residue record is written once, never rolls
  back the outcome, and is switchable in both directions
  [E4: `internal/tools/task_reclaim.go:138`], [E6: `internal/tools/run_untracked_residue_test.go:179`].
- **A2's leak arm.** The locked-and-directory-gone registration is collected by a
  named arm before the lock check, with a named unlock repair
  [E4: `internal/tools/worktree_sweep.go:718`, `:1870`] — and this session
  observed one such registration collected on this host (§4.2).

### (b) Falls short

**FS1 — A5 is *satisfied* by a directory the system just declared retired, and
this is the module's sharpest user-visible hole.** `AssertTempRootOffRootFS`'s
own comment says the rule out loud: *"an EXPLICIT override is allowed exactly as
long as it is off `/` (an operator who pins TMPDIR at /dev/shm or the workspace
passes)"* [E4: `internal/scratch/tmproot.go:451-454`]. But the tree retired
creating under the memory-backed base on 2026-09-18 and now tells the shell rigs
to stop doing it [E5: commit `45c809a2` (2026-09-18, "fix(scratch):
operator-facing text and the shell rigs stop creating under /dev/shm"), with the
retirement rationale in `devdoc/tmpdir-param-not-banner.md`; the ruling itself is
quoted at E4 `internal/worktreeroots/roots.go:61`].

**Measured on this host, in this session:**

```
$ stat -c '%d %n' /dev/shm /home/user/workspace/_tmp
28 /dev/shm
2052 /home/user/workspace/_tmp
$ stat -f -c '%T %n' /dev/shm
tmpfs /dev/shm
$ GOPI_DEBUG=1 ./bin/gopi --version
[gopi] tmpdir: TMPDIR="/dev/shm/gopi-test-scratch/tmp" (from the caller's TMPDIR)
$ du -sh /dev/shm/gopi-test-scratch
436M	/dev/shm/gopi-test-scratch
$ grep tmpdir ~/.gopi/settings.json
  "tmpdir": "/dev/shm/gopi-test-scratch/tmp",
```

So: the operator's settings file still names the retired tmpfs, the effective
temp root is a 7.8 GB tmpfs of which 436 MB is already occupied, and the
soundness criterion reports **pass** because the device id differs from `/`.
This is **not** a Rule 22 violation — the rule's criterion is device identity and
it is correctly implemented, and `GOPI.md:752` explicitly permits a
memory-backed scratch when the operator *chooses* it. What is missing is a
**second criterion nobody wrote down**: the temp root must also be durable enough
to hold what it is asked to hold. The legacy-role comment names the quantity at
risk and the reason the constant survives — *"deleting the value would orphan
2.6 GB / 702 measured entries"* [E4: `internal/worktreeroots/roots.go:66-73`] —
and the same tmpfs has already been filled to 100 % once by a daemon leak
[E5: `devdoc/daemon-e2e-leak.md:16`]. **Direction of the gap:** either the
soundness check grows the second criterion (device *and* durability), or the
operator's settings value is corrected; leaving it makes A5 true and useless.

**FS2 — the "who is standing here?" decision is duplicated across many
independent implementations, which is the briefing's own (c) question answered
uncomfortably.** Enumerating the production functions that decide liveness or
ownership by name yields a long list in one package alone — `finishCleanupLiveOwnerGate`,
`finishCleanupForeignOwnerReason`, `finishCleanupOwnerRowLive`,
`finishCleanupProcessHolderLive`, `taskLiveHolderStatus`, `reclaimHolderVeto`,
`worktreeSweepRowSessionLive`, `branchAttributionRunnerLiveness`,
`taskRunRowIsLive`, `sweepOwnershipRoot`, and more. The *shared measurement* is
single-source (`internal/scratch/holder.go:276` `HeldByProcesses`), so this is
not drift in the primitive. It is **policy divergence**: each site applies its
own combination of liveness, ownership, dirtiness and age to the same question,
so "is this directory safe to collect?" has one answer per caller. Rule 25's own
lesson points the same way — *"every branch of logic it carries multiplies its
failure surface"* [E2: `GOPI.md:825`] — and this is the worktree face of it.
**Direction:** this is a *finding*, not a proposed refactor; naming the count is
the deliverable, and a future consolidation would have to be judged against C3
(§ below) before it is built.

**FS3 — the "no registration, no row" class stays invisible.** Every guard here
keys on a registration or a row's record; a directory created outside that path
is neither protected nor reapable. The sibling document reaches the same
conclusion from the sweep's side and marks it as this module's honest hole
[E5: `devdoc/requirements/03-worktree-and-cwd.md` §6(b) G3]. This session's
measurement supplies the quantity that matters more than the class: **three
registrations survive in the declared root right now, and at least one carries
no ledger owner at all** (three `deliverable` lookups → 0 rows). The sweep is
built to collect exactly this, so the honest statement is *"the mechanism exists;
the observed end state shows it had not run or had not yet aged them out"* —
which the 24 h floor plus a mid-session reconciliation (below) makes a
measurement, not a guess.

**FS4 — a registration that self-destructs mid-session is explained by a
mechanism but is not accounted for by the operator's model.** In this session a
`locked` registration present at start was gone (with `prune --dry-run` empty)
within minutes, unattributed to any close-out; the tree explains the mechanism
precisely [E4: `internal/tools/worktree_sweep.go:566-568`, `:718`, `:1870`].
The gap is not in the code — it is that a *live, unattributed* directory change
on the host has no operator-visible record here. This is the same "no
observation without a record" family as `GOPI.md:1149`.

### (c) Over-builds

**OB1 — the temp-root precedence chain has four branches where the operator
asked for one knob.** `--tmpdir` → a caller-exported `TMPDIR` → `GOPI_TMPDIR` →
the settings key, with the built-in default below all four
[E4: `internal/config/tmpdir.go:79`]. The chain is *reasoned*, not accidental —
the code justifies each level, and the flag doubles as the operator's escape
from a stale settings file [E4: `internal/config/tmpdir.go:60-77`]. But it is
also the clearest instance of the briefing's required finding shape ("a fallback
chain, say which branch could be deleted"): **the `GOPI_TMPDIR` level (branch 3)
is the deletable one.** It is the least specific deliberate channel, it exists
only because gopi needs a name that is not the variable it publishes, and the
ambient `TMPDIR` above it already outranks it — so the only configuration it can
express alone is one a caller could express with the ambient variable or the
settings key. Under C3 this branch buys a fourth precedence rule in exchange for
one more way to be wrong; deleting it removes a level, not a capability
`[inference — weakest link: the two channels differ in *who* sets them (a parent
process vs the operator's shell), which is a real distinction in a nested-gopi
run; the deletion is right under C3, and I cannot prove no operator relies on
branch 3.]`

**OB2 — the child resource ceilings interact with the temp root and are not part
of this promise.** `internal/scratch` carries per-child ceilings
[E6: `internal/scratch/limits_test.go`]. They are a *limiting* mechanism (the
operator's own preference: limit over clean up, [E1: `01a08da4`]) sitting in the
same package as the resource this module promises. Not a defect — recorded
because a reader will find them here and must know they belong to the C3
discussion, not to A5.

### (d) Never decided — decisions the code makes that no requirement fixed

**N1 — "off `/`" was decided; "durable enough" never was.** Rule 22 is explicit
that the criterion is device identity and explicitly permits a memory-backed
choice [E2: `GOPI.md:717`, `:733`]. Nothing anywhere states a *capacity* or
*durability* requirement on the temp root, so FS1's hole is not a violation of a
decision — it is the absence of one. **This is a `never decided`, and it is the
reason FS1 survives review:** every individual artifact is right, and the
composition is wrong. [I — weakest link: it is possible the operator intends
`/dev/shm` deliberately on this host for speed; if so the requirement is
"document the choice", not "change the default". Either way the requirement is
unwritten, and an agent cannot tell the two apart.]

**N2 — whether a run's own residue should ever *refuse* its completion was
decided by a switch default, not by a requirement.** The record is always
written; the refusal is opt-in (`GOPI_TASK_RUN_RESIDUE_ENFORCE`,
[E4: `internal/tools/task_reclaim.go:1268`], with its escape
`GOPI_TASK_DIRTY_CLOSEOUT_ALLOW`, `:1326`). Rule 30's own text describes both the
record and the refusal without choosing the default
[E2: `GOPI.md:1146-1173`]. **Consequence:** A4's "bug" half is currently
*allowed behaviour* — a run may report ready with untracked residue, as long as
the record is written. A test author cannot separate bug from allowed behaviour
here without deciding this first.

**N3 — the boundary between "a daemon must be simple" and "a run must clean up
after itself" was never drawn, and this is the module's central tension.**

The two constraints are both the operator's, both binding, and they pull in
opposite directions on this exact boundary [E2: `GOPI.md:825` Rule 25; E1: task
`01a0975a`]:

- **"A daemon must be simple"** — *"every branch of logic it carries multiplies
  its failure surface. The merger's logic itself is the defect — not any one bug
  in it"* [E2: `GOPI.md:825` quoting ledger `01a0a995`]. Under this, the correct
  move on a stranded checkout is to make the *producer* stop creating the
  problem, and to keep the daemon's decision surface small.
- **"The agent must clean up after itself"** — a leftover is the system's debt
  and "leave it for the human" is not an acceptable requirement
  [E2: `GOPI.md:717` Rule 22's own bounding sentence; E1: `01a0975a`]. Under
  this, the correct move is to collect the leftover — which *adds* logic, and
  which is precisely what the sweep did.

The tree made both moves at different times and never reconciled them. The
**producer** side is the C3-faithful one and is stated as such
(`internal/daemoncwd/daemoncwd.go:7-21`, "a producer obligation, never a guard
weakening"). The **collector** side is much larger: a sweep with an age default,
an env-tunable floor and cap, three evidence axes, an unlock repair, a
dirty-orphan escalation, a live-holder veto and its own tests
[E4: `internal/tools/worktree_sweep.go:74-190`]. **Where the tree actually
landed** — and this is the answer the tension needs — is: *every one of those
branches lives in a **resident tick's** bounded pass, not in a daemon's request
path, and the sweep is fail-open and switchable.* The daemon stays simple
because it does not decide the question per action; the pass decides it once, on
a schedule, over a bounded list. **So the reconciliation exists as an
architecture and not as a rule** — nothing writes it down, which is why N3 is a
`never decided` and not a `serves`. The measurable cost of leaving it unwritten
is FS2: with no stated rule for "who may collect what", each site wrote its own
combination, and that is the branch multiplication C3 warns about, reproduced
across callers instead of inside one daemon.
`[I — weakest link: "the daemon does not decide per action" is inferred from the
placement of the sweep in a tick rather than from a stated design rule; a reader
could argue the tick's pass IS the daemon's logic.]`

### The C-constraint sweep for this module (C1–C6, `00-method-and-standards.md` §5)

- **C1 (one static binary, zero deps)** — satisfied: the temp-root measurement,
  the scratch tree and the sweep are stdlib + `os/exec` to git; no new
  dependency enters through this boundary. **Conflict found:** OB1's chain is
  *cognitive* cost, not dependency cost, so it survives C1 and fails C3's
  spirit.
- **C2 (the operator controls the whole thing)** — satisfied by construction:
  the effective temp root names **both the value and the channel that supplied
  it** on one line under the pre-existing debug switch, so "why this directory?"
  is answerable without running anything else [E5: `devdoc/tmpdir-param-not-banner.md`].
  **Violation-shaped and named:** FS1 is a stale operator-owned value whose
  effect he cannot observe in ordinary runs — and that is a deliberate design
  choice (a run does not announce its parameters). Under C2 the *observability*
  exists but is off by default; that is acceptable, and FS1's fix must not
  become a banner (the epic that removed the banner is
  `devdoc/tmpdir-param-not-banner.md`).
- **C3 (a daemon must be simple)** — this is the module where C3 has teeth. OB1
  adds a branch; FS2 is branch multiplication across callers; N3 is the
  unresolved rule. §N3 is the required reconciliation.
- **C4 (the agent cleans up after itself; dirty is a violation)** — **satisfied
  and load-bearing.** A4's record + refusal switch is the mechanism, and §4.5's
  incident (`01a0af50`) is why it exists. The bounding is honest: the mechanism
  never deletes, so C4 is honoured without risking the deliverable.
- **C5 (a small team, one person can run it)** — **the strongest cost signal in
  this module.** The sweep carries an env-tunable age floor, a cap, a lock, a
  queue-subtree walker and its own test suite; the GC window is a hardcoded 6 h
  constant whose stated assumption the operator's own row already questioned
  [E1: task `01a0a4c4` — *"the stale window is a hardcoded 6h constant whose
  stated assumption is false"*; E4: `internal/scratch/gc.go:60`]. The
  reconciliation in N3 is what keeps this affordable: a bounded, scheduled,
  fail-open pass is something one person can hold; a per-action decision in a
  daemon is not.
- **C6 (`why` over `what`)** — **this document is the module's first written
  `why`.** §4.3's two rules are the exception: Rules 24/25 carry their own
  rationale and their incident devdocs, so their `why` is already recorded.

---

## 7. What this implies for tests

**No test is written here.** Each entry names an existing test whose *verdict*
the reconstructed requirement changes, and the direction.

**Already red against the requirement (the finding the next phase should
carry):**

1. `TestDefaultTmpDirIsAnOrdinaryDirectoryUnderTheWorkspace` —
   `internal/scratch/default_tmproot_test.go:59`. **Verdict unchanged, coverage
   incomplete.** It proves the *built-in default* is an ordinary workspace
   directory; nothing proves the *effective* root on a real host is not a
   tmpfs. Direction: a test that measures the effective root's filesystem type
   (not only its device) would have caught FS1 — and the existing
   `TestAssertTempRootAcceptsAnOffRootOverride`
   (`internal/testutil/tmproot_assert_test.go:145`) pins the *acceptance* of a
   `/dev/shm` override, i.e. today's suite **asserts the behaviour FS1 names as
   the hole**. That is the red.
2. `TestScratchHomeIsNeverTheAmbientTempRoot` —
   `internal/testutil/scratchroot_test.go:225`. **Verdict unchanged**; its name
   shows exactly the gap: "never the ambient root" is a weaker property than
   "never on a partition that cannot hold what it is given".

**Verdict changes under the lifetime half (A6/A7):**

3. `TestReleaseMovesCwdToProcessRoot` — `internal/worktreerelease/release_test.go:16`.
   **Verdict unchanged; scope narrow.** The release is exercised for the
   *existing* process. Under A6 the requirement is about the *spawn* side:
   direction — a test must observe `/proc/<pid>/cwd` of a **real spawned child**
   (the daemoncwd file says exactly this is the acceptance criterion:
   "never an assertion that a struct field was assigned",
   [E4: `internal/daemoncwd/daemoncwd.go:36-40`]).
4. `TestResolveRefusesTheScratchTreeInsideADeclaredRoot` and
   `TestResolveRefusesAScratchTreeOnRootFS` — `internal/daemoncwd/daemoncwd_test.go:114`, `:130`.
   **Verdict unchanged — these are the tests A6 is already built on.** Named
   because a reader of this module needs to know the requirement is *already
   covered* here, so a "missing test" claim about A6 would be false.

**Verdict changes under the cleanliness half (A4):**

5. `TestReclaimRunResidueEnforceSwitchRefusesTheWriteback` —
   `internal/tools/run_untracked_residue_test.go:179`. **Verdict unchanged;
   default undecided (N2).** The test pins the switch, not the policy. Direction:
   if N2 is decided toward "refuse by default", this test's assertion inverts —
   it would assert the refusal **without** the env var.
6. `TestReclaimDirtyReasonNamesUntrackedResidueApartFromWork` —
   `internal/tools/worktree_refusal_arms_test.go:43`. **Verdict unchanged; it is
   the evidence A4 is implemented as a three-valued classification** rather than
   a boolean.

**Verdict changes under the collection half (A2/A3):**

7. `TestSweepReclaimsStaleUnownedRegistration` — `internal/tools/worktree_sweep_test.go:40`;
   `TestSweepReportsDirtyWorktreeInsteadOfRemovingIt` — same file `:124`.
   **Verdict unchanged.** Named because A2/A3 are covered by *classification*
   tests, and the uncovered half is the operator-facing one: nothing observes
   that a survivor's reason ever reaches him (the sibling document makes the same
   point from its side).
8. `TestSweepDeadRegistrationOwnedByLiveTaskKeepsItsBranch` —
   `internal/tools/worktree_sweep_dead_registration_test.go:286`. **This is the
   test whose requirement the §4.2 live observation exercised**: it pins that the
   dead-registration arm keeps a live row's branch while collecting the
   registration — the exact split observed on this host (registration collected,
   no branch touched).
9. `TestSweepFixture_LiveHeldRootIsKept` — `internal/scratch/gc_test.go:98`, and
   `TestPermitsIsOneWayContainment` — `internal/worktreeroots/roots_test.go:112`.
   **Verdicts unchanged**; both are the containment/ownership half of A3 and A1.

**Verdict changes under the anti-privilege half (A8):**

10. `TestDaemonScopeNoPrivilege_PrescriptionsAreFlagged` —
    `cmd/agent/daemon_scope_no_privilege_test.go:88`. **Verdict unchanged and
    load-bearing:** it gates the *text*, not the code path, which is the only
    form that catches the Rule 25 defect class (a prescription in a usage line
    or a dispatched summary). Any future text in this boundary must survive it.

**A requirement with no test touching it (the strongest hand-off item):** FS1.
Nothing measures the *durability* of the effective temp root, and one existing
test positively asserts the acceptance of the shape FS1 names as the hole.

---

## 8. Confidence, weakest links, and the inference register

| # | Claim | Status | Weakest link |
|---|---|---|---|
| 1 | Four mechanisms answer one operator question (`01a0975a`) | **Evidenced** — E1 `01a0975a`; E2 Rules 6a/22/24/25/29/30 | none material |
| 2 | The close-out order is the safety argument | **Evidenced** — E4 `internal/tools/task_finish_cleanup.go:11-23` | none material |
| 3 | The sweep's three axes and its locked-dead arm | **Evidenced** — E4 `internal/tools/worktree_sweep.go:89`, `:566-568`, `:718`, `:1870` | none material |
| 4 | A locked, directory-gone registration was collected mid-session on this host | **Observed** — `git worktree list` at t+2s (`locked`) vs t+107s (absent, `prune --dry-run` empty) | the observation is of the *end state*: I did not catch the collecting process in the act, so "the sweep did it" is inference from the mechanism, not a trace |
| 5 | The effective temp root is a tmpfs and the operator's settings still name it | **Measured** — `stat -c %d`, `stat -f -c %T`, `GOPI_DEBUG=1 bin/gopi --version`, `grep tmpdir ~/.gopi/settings.json` | provenance of the settings value (whether the operator set it deliberately for speed) is unknown — this is FS1's own caveat |
| 6 | The soundness check permits any off-`/` root including a memory-backed one | **Evidenced** — E4 `internal/scratch/tmproot.go:451-454`; E2 `GOPI.md:752` | none material — this is the gap's premise, and it is written down |
| 7 | Three registrations survive with no row-level owner | **Measured** — `git worktree list`; three `deliverable like '%<dir>%'` counts → 0 | "no owner" means *no `deliverable` string*; an owner could be recorded elsewhere (a summary/payload mention returned 1–2 for two of the three, so the classifier's answer and my query's answer differ — that difference is itself FS3's point) |
| 8 | The liveness/ownership decision is spread over many independent sites | **Measured (enumeration)** — the production function names listed in FS2 | enumeration by name; I did not prove each site decides a *different* policy, only that each decides |
| 9 | N3's reconciliation ("the daemon does not decide per action; a bounded tick does") | **[inferred]** | weakest link: it is inferred from placement, not from a stated design rule; the tick's pass could equally be called the daemon's own logic |
| 10 | OB1's deletable branch is `GOPI_TMPDIR` | **[inferred]** | weakest link: the parent-vs-shell distinction is real for nested runs; I cannot prove no operator relies on branch 3 |
| 11 | Rule 24/25's incidents are the named devdocs | **Evidenced** — E2 `GOPI.md:822`, `:861`; E5 the four devdocs exist on this revision | none material |
| 12 | Rule 30's incident is task `01a0af50` | **Evidenced** — E2 `GOPI.md:1160` | none material |

**What would change my mind:** a written requirement (or an operator sentence)
stating that the temp root SHOULD be a memory-backed tree on this host. That
single sentence converts FS1 from "a hole" to "a documented choice whose
documentation lives only in a code comment" — and I say so in N1 rather than
guessing.

---

## C. Operator-vision constraints applied to this module

Short form; the full sweep is §6's C-constraint subsection.

- **C1 one static binary, zero deps** — satisfied; nothing in this boundary adds
  a dependency (the one external call is `git` by design, `os/exec`).
- **C2 the operator controls the whole thing** — satisfied; the temp root names
  its own value *and* its supplying channel on one opt-in line
  [E5: `devdoc/tmpdir-param-not-banner.md`]. **FS1 shows the limit of this
  control**: a stale operator-owned value is invisible in ordinary runs by
  deliberate design.
- **C3 a daemon must be simple** — **the binding constraint for this module.**
  It is why OB1 is flagged, why FS2 is a finding rather than a refactor
  proposal, and why N3 is the section the operator should read first.
- **C4 the agent cleans up after itself** — satisfied and load-bearing (§4.5).
- **C5 a small team** — the sweep's own surface is the cost the tension in N3
  buys; the reconciliation (bounded, scheduled, fail-open) is what keeps it
  affordable.
- **C6 `why` over `what`** — §4.3's two rules are the only part of this boundary
  whose `why` was already written; this document supplies the rest.

---

## D. What I could not determine

1. **Whether the operator intends the memory-backed temp root on this host.**
   The settings file says yes, the tree says the role is retired, and the
   soundness check says the value is fine. One sentence settles FS1 and N1.
   Cheapest way: ask; or `git log -S'/dev/shm/gopi-test-scratch/tmp' --
   ~/.gopi/` is not applicable (operator-owned, outside the repo), so only the
   operator or his session logs can say.
2. **Which process collected `gopi-sc-redproof`'s registration mid-session.** I
   observed the end state and can name the mechanism; I did not capture the
   collector. Cheapest way: the resident tick's log
   (`devdoc/`-documented lifecycle tick output) or a `worktree_sweep` finding
   event for that path.
3. **Whether the three surviving registrations will be collected by the sweep.**
   Two are `detached HEAD` with no branch; the sweep's age floor is 24 h and the
   holder/ownership gates apply. I measured the state, not a future pass.
   Cheapest way: `git worktree list` again after the next resident tick, and
   compare.
4. **How much of `/dev/shm`'s 436 MB is this module's responsibility** vs. the
   test suite's or another task's. `internal/scratch`'s home is the only
   component I can attribute; the rest of the tmpfs is unattributed here.
5. **The size of the FS2 population.** I enumerated production function names
   that decide liveness/ownership; I did not measure how many *behaviourally
   distinct* policies they encode. That measurement is a task of its own and
   should be judged against C3 before anything is merged
   (`du`-style enumeration is cheap; policy diffing is not).
6. **Whether the sibling document's id scheme (`B3`) and this one's (`M10`/`[12/15]`)
   will be reconciled by the series' summary.** `00-method-and-standards.md`
   §7.4 says contradictions between umbrella and module documents are recorded;
   the B3/M10 overlap is the largest one I found, and I record it rather than
   resolve it.

---

## The one finding that is new to the operator

**The temp-workspace soundness check is satisfied by a directory the system
itself retired four hours before this document was written — and the check's own
comment names that exact directory as a passing case.**
[E4: `internal/scratch/tmproot.go:451-454`; E5 commit `45c809a2`; E4
`internal/worktreeroots/roots.go:61`, whose comment quotes the 2026-09-18 ruling
"don't use tmpfs. change settings to workspace. now"]

Measured in this session: the effective temp root is
`/dev/shm/gopi-test-scratch/tmp` (`tmpfs`, device `28`), because the operator's
own settings file still names it — while 436 MB of it is already occupied, the
tmpfs is 7.8 GB in total, and the same tmpfs was filled to 100 % once by a
daemon-process leak [E5: `devdoc/daemon-e2e-leak.md:16`].

**Why it is new:** every artifact involved is individually correct and each is
defensible in isolation — the criterion is device identity (Rule 22 says so,
`:717` and permits the choice at `:733`), the retirement is of *creating* there,
and the operator's value is his own. What nobody has written down is the second
criterion: **the temp root must be able to hold what it is asked to hold.**
Adding it is the difference between A5 being true and A5 being useful — and the
cost of not adding it is that the *next* exhaustion report will look, to every
check in the tree, exactly like a healthy machine.

---

## Verification

**Build and tests are genuinely not-applicable for this change, and that is
measured rather than asserted.** The diff is one new Markdown file and touches
no Go source:

```
$ git diff --name-only master...HEAD
devdoc/requirements/worktree-and-cwd.md
$ git diff --stat master...HEAD
 devdoc/requirements/worktree-and-cwd.md | 860 ++++++++++++++++++++++++++++++++
 1 file changed, 860 insertions(+)
```

`go build`/`go test` could not exercise this diff — there is no compiled
artifact whose behaviour this document defines — so no build or test result is
claimed here. The checks that *are* meaningful for a cited document were run
against the committed revision, and each is quoted with its outcome:

- **Citation range-check.** Every `file:line` in this document was resolved
  against this revision: **61 unique citations checked, 61 in range, 0 bad**
  (a script that resolves each cited path — including the `devdoc/requirements/`
  relative forms — reads the file's true line count, and fails a citation whose
  line falls outside it, both ends of a `NNN-MMM` range included).
- **Test-name resolution.** **14 unique `Test*` names cited, 14 resolve** to a
  `func Test…` declaration in the tree, 0 unresolved. (This matters more than a
  grep: §7 and the diffusion paths name tests whose *verdict* the requirement
  changes, so an unresolvable name would be a fabricated hand-off item.)
- **Incident devdocs exist.** All nine named incident/rationale documents exist
  on this revision: `peerbox-resource-exhaustion-20260911`,
  `worker-cohort-kill-20260912`, `signal-provenance-20260913`,
  `detach-judgment-rule`, `no-privilege-daemon`, `daemon-e2e-leak`,
  `branch-task-attribution`, `daemon-scope-privilege-hint-20260914`,
  `tmpdir-param-not-banner`.
- **Section headings present.** All eight mandated headings are greppable
  (`## 1.` … `## 8.`), plus the operator-constraints section and the
  "what I could not determine" list.
- **The FS1 measurement reproduces after the document was written.**

```
$ stat -c '%d %n' /dev/shm /home/user/workspace/_tmp
28 /dev/shm
2052 /home/user/workspace/_tmp
$ stat -f -c '%T %n' /dev/shm
tmpfs /dev/shm
$ grep -n tmpdir ~/.gopi/settings.json
5:  "tmpdir": "/dev/shm/gopi-test-scratch/tmp",
$ du -sh /dev/shm/gopi-test-scratch
436M	/dev/shm/gopi-test-scratch
```

**One live corroboration added after the body was written.** The analysis
session that produced this document was itself running with
`TMPDIR=/dev/shm/gopi-test-scratch/tmp` in its environment — i.e. the retired
tmpfs was not merely named in a settings file, it was the effective temp root of
the very run doing the measuring. That is FS1 observed from *inside* the
mechanism rather than from a config read, and it strengthens the finding
without changing it.

**Worktree cleanliness (Rule 30).** `git status --porcelain` in this worktree
prints nothing; the deliverable is committed, not left as a tracked change.
