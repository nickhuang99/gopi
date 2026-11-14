<!-- doc-class: normative -->
# 03 — Worktree & cwd resource management

Module B3 of the reverse-requirement-analysis series.
Branch `feat/req-b3-worktree-cwd`, based on master `17cec86b`.

Evidence tokens follow `00-method-and-standards.md` §1 (E1 operator ledger row,
E2 GOPI.md rule, E3 commit message, E4 code, E5 devdoc, E6 test assertion,
I inference, O opinion). Every `file:line` below was re-read on `17cec86b`
before this document was committed.

---

## Front matter

**Boundary.** The ground a run stands on *on disk*: where a run's checkout
lives, who is allowed to be there, and how it stops existing when the run is
over — including the promise that a workspace never fills a disk the rest of the
machine needs.

**In scope.** The close-out chain that ends a client run's tenancy
(`internal/tools/task_finish_cleanup.go`); the worktree dirtiness split and the
refusal arms (`internal/tools/worktree_*`, `finishCleanupWorktreeDirtyReason`);
the single definition of every filesystem root a task may write in
(`internal/worktreeroots`); the guard that refuses a command reaching outside
those roots (`internal/tools/path_policy.go`); the released current directory
(`internal/worktreerelease`, `internal/daemoncwd`); the age-and-ownership sweep
of stale registrations (`internal/tools/worktree_sweep.go` and its resident
tick); and the `--force` / `-d` safety ordering that the chain's header states
as its own safety argument.

**Out of scope.** When a merge happens and what parks (§B2), except that B2's
merge path calls this module's close-out; the ledger's own semantics
(§B1). `internal/scratch` is in scope **as the mechanism the operator's
own resource question is answered by** (it holds the temp root and the child
resource ceilings this module is judged against); its own requirements belong to
no module document and this document says so rather than claiming them.

**Boundary why.** This responsibility is deliberately split from merge even
though much of the code sits in one Go package: the create/release need is
realizable and testable with no merge at all, and the operator asked about it
directly and separately [E1: task 01a0975a].

**Known boundary mismatch (stated, not hidden).** The module map assigned
worktree/cwd to M10 ("not stranding anything", `00-module-map.md:168`) and the
close-out chain's code lives in `internal/tools`, which the same map also gives
to M1/M2/M3. The measured line count (`internal/tools` production files,
2,542 lines in `task_finish_cleanup.go` alone) is the reason; the boundary here
follows **responsibility**, as the map's own principle requires
(`00-module-map.md:9-12`), and the mismatch is recorded rather than smoothed
over.

**Overlap with a sibling series.** Ledger row `01a0ab12-7bcc-7e9c-8b9a-e35eac417b48`
(`[12/15] MODULE: worktree / cwd resource management`) is still `pending` for
this same responsibility and asks a partly different question (it also claims
daemon lifetime/cgroup and tmpdir precedence). It has produced no file
(`git log --all --diff-filter=A -- 'devdoc/requirements/*'` → empty). This
document judges that scope split in §6 rather than redoing it.

---

## 1. What this is for

Work happens in a copy of the project, one copy per piece of work, and that copy
is the most dangerous thing the system owns: it is the only place where an
unfinished change can be lost, and it is a directory on someone's disk.

So the system has to answer four questions, and they are all the same question
asked at different moments. Where may a piece of work put its own copy? While
the work is in progress, is anybody else allowed to take that copy away? When
the work is finished, how does the copy disappear without taking unfinished work
with it? And who is responsible for the leftovers when a run ends badly —
because the answer has to be the system, never the human who happens to notice.

The user-visible promise is narrow and testable: **finished work reaches the
shared place and stops occupying my disk; anything not yet handed over is still
there when I look.** A leftover directory is not a condition to be tolerated — it
is the system's own debt, and the system must be able to say, per leftover, that
it is debt.

*If this stopped working, the operator would notice by his disk filling with
directories whose task is already marked finished, and — worse, and first — by
work he thought was delivered turning out to exist on only one machine.*

---

## 2. Who is served

**The operator (nick)** — he owns the disk and the machines. He needs: his work
survives; a workspace does not silently consume a device the rest of the machine
depends on; when something is left behind he is told, in his own terms, and the
leftover carries enough information to decide. His own words: *"is gopi clean up
after development? i.e. is this just normal worktree like other path? …
these are serious questions to ask"* [E1: task 01a0975a].

**The agent (a running session)** — it needs a directory nobody else will tear
away, and it needs to be *told* when it cannot have one, instead of having its
ground removed underneath a running command. Its distinct need is the opposite
of the operator's: the operator wants the directory gone, the running session
wants it kept.

**A server or client node (the same system on two machines)** — the two nodes
have genuinely different obligations and this is where most of the module's
complexity lives: on the client, work must leave the machine before the checkout
disappears; on the server, the branch is the merge input and must outlive the
checkout. Neither node may destroy work the other has not yet consumed.

**A future maintainer** — must be able to answer "is this leftover garbage?"
from the registration and the row, without archaeology. A leftover with no owner
attribution is indistinguishable from someone's live work.

---

## 3. The workflow it sits in

Entry: a task is claimed and a run begins. It needs a directory of its own
(`GOPI.md:144` Rule 6a — the linked worktree is the *standard*, and gopi creates
it itself; the operator's own words in Rule 6's rationale are that the copy is
cheap because it shares the object store).

1. **Creation.** A directory is created from the declared worktree root for that
   repo, on a branch named for the task `[E2: GOPI.md:144 Rule 6a]`,
   `[E4: internal/worktreeroots/roots.go:76]`. The run then *stands* in it: its
   current directory is the run's ground, and a release moves the process out
   before anything is removed `[E4: internal/worktreerelease/release.go:63]`.
2. **During the work.** The directory may not be taken: a live process holding it
   or standing in it refuses the removal `[E4: internal/tools/task_finish_cleanup.go:2043]`,
   `[E4: internal/tools/task_finish_cleanup.go:912]`, and a directory another row
   has recorded as its own home is refused too
   `[E4: internal/tools/task_finish_cleanup.go:2054]`. Rule 29 makes the same
   point from the operator's side: never point work at a checkout that is not
   yours `[E2: GOPI.md:1047 Rule 29]`.
3. **Close-out — the four-step sequence, whose order is its safety argument.**
   Stated in the code's own header: guard the row and its dirtiness, then hand
   the branch to the peer, then remove the directory, then delete the local
   branch `[E4: internal/tools/task_finish_cleanup.go:11-23]`. The destructive
   half comes last because removal with an undelivered branch loses work; the
   local branch deletion is the *second* lock, refusing a branch the delivery did
   not deliver.
4. **Afterwards.** Anything the run failed to clean is a named refusal, and stale
   registrations are reaped by an age-and-ownership sweep — the resident
   lifecycle tick by default runs it for real, not as a rehearsal
   `[E4: cmd/agent/task_lifecycle_tick.go:128,280]`,
   `[E4: internal/tools/worktree_sweep.go:89]`.

Exit condition: the row is terminal **and** either the directory is gone or a
named reason says why it is not. Exit is per-directory, not per-task: a row can
end while its directory legitimately survives (a live holder, a dirty tree with
real work in it).

**The two arms are not the same operation, and this is the module's central
fact** (measured, §6): the client arm's removal is forced and its branch is
deleted after delivery; the server arm's removal is *not* forced and its branch
is preserved as the merger's input — "worktree remove only (no push — nothing to
push to; no branch -d — the branch is the deliverable)"
`[E4: internal/tools/task_finish_cleanup.go:818]`,
`[E4: internal/tools/task_finish_cleanup.go:951]` (no `--force`) versus
`[E4: internal/tools/task_finish_cleanup.go:2079]` (with `--force`), routed at
`[E4: internal/tools/task_finish_cleanup.go:807]`.

---

## 4. Why it exists — cited evidence

### 4.1 Work must not be lost, so destruction comes after delivery

The need: *the operator must never lose work to housekeeping.* The evidence is
the code's own stated ordering rationale, which is load-bearing rather than
descriptive: "ONLY after ② succeeded (a removed worktree with an unpushed branch
= work loss)" and "`-d` refuses unmerged branches (the double lock: a branch the
push did not deliver cannot be deleted)"
`[E4: internal/tools/task_finish_cleanup.go:19-23]`. The second lock is
implemented as a gate that consults whether *this sweep's* push landed
`[E4: internal/tools/task_finish_cleanup.go:2139]`,
`[E4: internal/tools/task_finish_cleanup.go:2195]` — i.e. the force-delete is
refused unless the delivery part of the same run succeeded.
*What would have gone wrong without it:* a finished branch deleted locally after
a failed push, with the row already saying `completed` — silent, total loss of a
deliverable.

### 4.2 A dirty directory is protected because it means a live writer

The need: *the system must not pull the ground out from under a running run.*
The code's rule: a non-empty status output is a refusal, and the reason names
dirtiness `[E4: internal/tools/task_finish_cleanup.go:992-1011]`. The clean
probe compares git's output *exactly* against a sentinel, so a real file whose
name happens to match it still reads as dirt
`[E4: internal/tools/task_finish_cleanup.go:581-587]`,
`[E4: internal/tools/task_finish_cleanup.go:586]` — a filename cannot defeat the
guard.
*What would have gone wrong:* a run whose CWD is inside the directory being
removed; the code names the observed failure of exactly that in the liveness gate
("bogus package-load failures, work loss on the `--force` path",
`[E4: internal/tools/task_finish_cleanup.go:909-911]`).

### 4.3 The operator's resource question — and the code answering it a day later

This is the module's governing evidence, quoted verbatim because paraphrasing an
operator's own words is how a requirement gets rewritten
[E1: task 01a0975a, `source=human`, `status=verified`, 2026-09-12T20:41:08Z]:

> "when seeing gopi creat worktree in /dev/shm (see attachment) really causes
> concern for anybody not understanding such purpose. is /dev/shm a shared
> memory? what is purpose of creating such worklist for git repo in shared
> memory? I guess this may consumes system resource, right? is gopi clean up
> after development? i.e. is this just normal worktree like other path? is
> shared memory causing any system process to process communication issue? is
> there any danger using this /dev/shm path for worktree? these are serious
> questions to ask and it poses another concern that for this sandbox
> environment, should we setup any blacklist/whitelist of system resource like
> bash command, system path, system call to avoid one mistake causing disaster?
> these are necessary for study"

Three separate needs are inside that paragraph, and they must not be merged:
(a) *tell me what this resource is and whether it is dangerous*; (b) *does the
system clean up after itself?*; (c) *may I bound the blast radius of a single
mistake?* The ruling-§6 below keeps them apart, because the code answered them
unequally.

The measured assessment is `devdoc/shm-worktree-risk-assessment.md` (2026-09-13):
RAM not disk, no POSIX shm objects on that mount, interference channel is memory
pressure not name collision, and the real dangers are the OOM blast radius,
volatility and an unquota'd shared cap
[E5: devdoc/shm-worktree-risk-assessment.md:17]. (a) is therefore **served by
documentation**, and the code side of (b)/(c) landed the next day.

### 4.4 Bounding the blast radius: the path policy, and what was refused on purpose

The need (c) was implemented as a **coarse, cheap** guard rather than a syscall
filter, and the choice is recorded with its reason: seccomp "is the wrong layer
for agent mistakes (high effort, fragile against the Go runtime) — it contains
exploits, not slip-ups" [E5: devdoc/shm-worktree-risk-assessment.md:17],
`[E5: devdoc/shm-worktree-risk-assessment.md:171]`.

The implementation is one predicate consulting the declared roots
`[E4: internal/tools/path_policy.go:210]`,
`[E4: internal/tools/path_policy.go:324]`, rooted in a single definition of every
root a task may use `[E4: internal/worktreeroots/roots.go:63,76,102]`, with the
per-repo declaration resolved in one place
`[E4: internal/config/config.go:1010]`. It is pinned by two acceptance tests
that name the exact operations the operator worried about: creating a worktree
under the shared-memory mount is refused *and the refusal names the target*
`[E6: internal/tools/bash_path_policy_test.go:36-42]`, and an out-of-root
recursive delete is refused `[E6: internal/tools/bash_path_policy_test.go:54-58]`.

### 4.5 The child ceiling, and the alternative that was accepted

The same task added ceilings on what a child may consume
`[E4: internal/scratch/limits.go:54,57]`, resolved in one place
`[E4: internal/scratch/limits.go:123]` with measured defaults
`[E4: internal/scratch/limits.go:68,76]`. It is a **count-and-memory ceiling**,
not a syscall whitelist — the operator's phrase "system call" is deliberately not
implemented as such, with the reason recorded above. Under C2 this is the
operator's own stated preference in practice: a limit he can read, raise and
reverse beats a filter he cannot see. *This is a partial answer to (c)*
[INFERENCE — weakest link: the assessor's engineering judgement is quoted in an
E5 document, and no operator row states "a process ceiling is enough"; the
operator's verdict on 01a0975a was `verified`, which is evidence he accepted the
answer at the time, not evidence that he rates ceiling-versus-filter the same
way now].

### 4.6 "Clean up after yourself" is the module's obligation, not a courtesy

Rule 22 is not a spelling rule: it is a criterion measured by device id, with the
incident that produced it (the boot partition driven to 1.5 G free)
`[E2: GOPI.md:697 Rule 22]`, and Rule 24 judges process independence the same
way — by measurement, not by appearance `[E2: GOPI.md:776 Rule 24]`. Rule 29
forbids borrowing another task's checkout at all `[E2: GOPI.md:1047 Rule 29]`.
The close-out chain's own residue note states the obligation in the direction
this module needs: the untracked residue "is the run's own hygiene debt",
"a run must clean up after itself"
`[E4: internal/tools/task_finish_cleanup.go:568-573]`,
`[E4: internal/tools/task_finish_cleanup.go:614]`.

---

## 5. Acceptance in user terms

A1. **Delivered means delivered.** When a run on my laptop finishes, the branch
is on the server before the local checkout disappears. Observable: after a
finish, the server holds the branch; after a finish whose upload failed, my
directory *and* my branch are still here.
*(Currently met for the failure half; see A2 for the half that is not.)*

A2. **Nothing harmless is blocked by something dirty.** A leftover scratch file
or build output in my checkout may not prevent finished work from reaching the
server. Observable: with such a file present, the branch is on the server
afterwards, and the *directory is still here* — the system does not destroy
anything to achieve this.
*(Not met — §6 G1. This is the acceptance statement the two tests in §7
contradict.)*

A3. **"Dirty" says which kind of dirty.** A refusal must distinguish *work not
yet committed* (a human must look) from *my own leftovers* (the system's debt).
Observable: the operator-facing line names which of the two it is, in words, not
as a bare "dirty".
*(Met — §4.6, §6 G4.)*

A4. **A finished task's tenancy ends, and a leftover is attributable.** Every
directory the system created belongs to a task row, and when the row is finished
the directory either goes or a named reason says why it stays; a leftover can be
traced to the row that owns it without guessing.
*(Met for registrations with an owner; **not** met for directories nobody
registered — §6 G3.)*

A5. **A workspace may not fill a device the machine needs.** No temporary
workspace lives on the boot partition, judged by the measured device — the
spelling of a path is not evidence `[E2: GOPI.md:697 Rule 22]`. Observable: the
resolved temp root's device differs from the root filesystem's.
*(Met by policy; measured live for this checkout in §6 G5.)*

A6. **A single mistake is bounded, readably.** A command reaching outside the
roots declared for this task is refused and the refusal names the target; a child
process has a ceiling on memory and process count. Observable: two named
refusals and one visible ceiling.
*(Met in the coarse form the operator asked for §4.4/4.5; a syscall whitelist is
NOT met and §6 G6 says why that is a decision, not an overshoot.)*

A7. **Two runs never stand in one directory.** A directory recorded by one row
is refused to another, and a live process inside one makes it unremovable.
*(Met — §3 step 2.)*

---

## 6. Gap analysis

Four surfaces, each with entries or an explicit "none found, because …".

### (a) Serves the reconstructed need

- The ordering safety argument itself: delivery before destruction, with a
  second lock on the local branch `[E4: internal/tools/task_finish_cleanup.go:19-23,2139]`.
- Live-holder protection on both arms (liveness, foreign-owner and live-ledger-row
  gates) — three independent judgements, each reporting rather than acting
  `[E4: internal/tools/task_finish_cleanup.go:2043,2054,2066,912,1329]`.
- The genuine dirty split, with a machine-checked classification table
  `[E6: internal/tools/worktree_dirty_split_test.go:32-49]`.
- The path guard, with the operator's own scenario as a named acceptance test
  `[E6: internal/tools/bash_path_policy_test.go:36,54]`.
- The age-and-ownership sweep, running for real by default
  `[E4: cmd/agent/task_lifecycle_tick.go:128,280]`, escalating rather than
  destroying unowned-but-dirty directories
  `[E6: internal/tools/worktree_sweep_memory_base_test.go:277]`.
- Cwd release before removal `[E4: internal/worktreerelease/release.go:63]`.

### (b) Falls short

**G1 — the dirty gate is applied to the harmless step as well as the destructive
ones (THE module's defect).** The client chain refuses the whole sequence —
including the delivery — on a dirty tree, at the entry arm *and* again in the
chain body `[E4: internal/tools/task_finish_cleanup.go:1007-1011]`,
`[E4: internal/tools/task_finish_cleanup.go:1993-1997]`. The operator hit this in
production and filed it, with the measurement:

> "Three finished deliverables sat on HP for hours, unpushed, while their task
> rows said `completed`" — and the dry run's own summary line: `would-run=0`,
> *"A single untracked file — an image — blocked the entire delivery chain of a
> 19-commit branch"* [E1: task 01a0aab8, verbatim from the operator-filed row's
> payload].

The user-visible consequence, in the operator's own frame: work he believed was
delivered existed on one machine only, and the one file that stopped it was the
run's own residue — which §4.6 says was gopi's debt to clean, not a reason to
stop delivering.

**G1b — the gate is duplicated, so the obvious repair does not repair it.**
There are two dirty gates on the client path, the first *before* the push target
is even resolved `[E4: internal/tools/task_finish_cleanup.go:1007]` and the
second inside the body `[E4: internal/tools/task_finish_cleanup.go:1993]`. A
maintainer who reads the chain body — where the ordering header and every step
live — and reorders *there* still ships the defect, because the entry gate
returns before the body is reached. Evidence that this is the real shape and not
a worry: the unmerged repair branch does exactly this correctly — its version of
the entry function resolves the delivery target first and carries **no** dirty
gate at all
[E4: `remotes/peer1/fix/client-closeout-push-delivery:internal/tools/task_finish_cleanup.go:1183-1225`
vs master `:963-1021`]. **Direction: the entry-arm gate must be deleted or merged
with the body's, or the fix is defeated at the door.**

**G2 — the operator is told the outcome of the only step that matters as a log
line.** Before the repair branch, a delivery that never happened produced no
durable record; the repair branch's own header names this (*"a branch that never
left the host looked exactly like a branch that had"*,
`[E4: remotes/peer1/fix/client-closeout-push-delivery:internal/tools/task_finish_cleanup.go:548-550]`).
*Status: found in an **unmerged** branch — `6e22d80b` plus five docs commits, 6
commits ahead of master, `NOT` an ancestor of master, parked at
`archive/fix/client-closeout-push-delivery` with a pending merge-resolution row
`01a0ab2c`. So the requirement is reconstructed from this module's code **and**
a repair that the trunk does not yet have. The deliverable is not "already
fixed".*

**G3 — directories nobody registered are invisible to every mechanism here.**
Every guard in this module keys on a *registration* or a row's record. A
directory created outside that path is unprotected and unreapable: the risk
assessment measured 11 stale fixture directories still registered and 3.0 GB of
test scratch persisting `[E5: devdoc/shm-worktree-risk-assessment.md:14]`. Under
A4 this is the honest hole: "who owns this directory?" has an answer for
registered worktrees and a shrug otherwise.

### (c) Over-builds

**None found in this module's own machinery, because the machinery is unusually
plain**: every step is a git subcommand with a named reason and an explicit
refusal, and the one place a *derived* mechanism was considered (a syscall
whitelist) was rejected with a recorded reason rather than built
[E5: devdoc/shm-worktree-risk-assessment.md:171]. The nearest thing to an
over-build is the **legacy second entry point** into the same guard
(`runFinishCleanupChainForTest`/
`runFinishCleanupChainTarget`'s injected-target seam,
`[E4: internal/tools/task_finish_cleanup.go:1918-1929]`), which exists for tests
and duplicates the whole prelude — target resolution, branch resolution,
exemption, ownership — around 60 lines of a 2,542-line file
`[E4: internal/tools/task_finish_cleanup.go:963-1021]` versus
`[E4: internal/tools/task_finish_cleanup.go:1929-1978]`. It is a test seam, not a
requirement, and G1b shows the cost: **the same rule is now implemented twice and
can drift.** [OPINION] the seam is the right next consolidation target, but it is
not a user-facing gap and must not be cited as one.

**One naming oddity that is deliberate, recorded so a future reader does not
"fix" it:** the executed client removal is built from the *plan-renderer's*
`--force` constant, not from a step constant
`[E4: internal/tools/task_finish_cleanup.go:2079]` using
`[E4: internal/tools/task_finish_cleanup.go:300]`. The comment above the
constant says this is intentional — one spelling for the predicted and the
executed argv, "so a plan can never advertise an argv the executor does not run"
`[E4: internal/tools/task_finish_cleanup.go:295-299]`. The name lies about its
scope; the choice is documented and correct.

### (d) Never decided — decisions the code makes that no requirement fixed

**N1 — "no step may run on a dirty worktree" was never a requirement; it is an
over-broad reading that got pinned.** This is the series' worked example, and on
this module's evidence it is a **classification, not merely a bug**:

- The chain's own header states the rule as *clean → push → remove → branch*
  `[E4: internal/tools/task_finish_cleanup.go:13-15]` — i.e. the author of the
  ordering header never separated "the dirty guard" from "the push".
- The stronger rule ("no steps at all") is asserted **only in tests**, and the
  git history shows it was written in the same commit as the chain itself
  (`8f7e824a`, 2026-08-31, "client finish-cleanup 4-step chain
  (push→worktree remove→branch -d)") — the test was typed alongside the code, so
  no requirement decision ever happened between them
  [E3: commit `8f7e824a`, via `git_blame` on `task_finish_cleanup_test.go:190-201`].
- The later dirty-split work explicitly *narrowed* the assertion, and left a
  comment saying so: the older "no steps at all" assertion is narrowed to "no
  step RAN (OK:true) — a prediction/note is not an action"
  `[E4: internal/tools/task_finish_cleanup_test.go:173-178]`, commit `bab6be54`
  (2026-09-15, merge of `feat/resume-finish-cleanup`).
- The split work's own header records the rediscovery, with a number: *"this
  epic's case 1: 242 live refusals, ~58 of them this state, all rendered
  identically"* `[E4: internal/tools/task_finish_cleanup.go:572-573]`, commit
  `80f721f0` (2026-09-15, "three refusal arms — named untracked residue …").

**Verdict: never decided.** Nobody ever wrote down what the dirt is protecting,
so the guard's *reach* was set by whoever typed the test. The operator's own
words after the incident are the requirement arriving late: the residue was
gopi's debt, therefore a harmless delivery step must not be stopped by it
[E1: task 01a0aab8].

**The split is itself the evidence of rediscovery** (§N1): the code had to
re-derive, from 242 live refusals, a distinction a requirement would have fixed
in a sentence — *uncommitted work is somebody's business; leftover build output
is the system's debt*. Two constants and a shared verdict function now carry that
distinction, and both arms consult the same one reading
`[E4: internal/tools/task_finish_cleanup.go:609-610]`,
`[E4: internal/tools/task_finish_cleanup.go:563-566]`,
`[E4: internal/tools/task_reclaim.go:1038-1042]`.

**N2 — the two arms' destructive power differs, and nothing decided it.**
Measured on master: the **client** removes with `--force`
`[E4: internal/tools/task_finish_cleanup.go:2079]`; the **server** removes
without it `[E4: internal/tools/task_finish_cleanup.go:951]`. Yet the same
dirty gate guards both, and the server arm's comment calls it "the same guard as
the client chain" `[E4: internal/tools/task_finish_cleanup.go:818-820]`,
`[E4: internal/tools/task_finish_cleanup_test.go:904-905]`.

**This is a finding, and it contradicts the expectation recorded for this module
in the series briefing — so it is stated with its arithmetic.** The briefing
expected *both* pinned assertions (§7, master `:201` and `:917`) to encode the
wrong requirement. On this evidence they do not:

- **`:201` (client arm) is the wrong requirement.** The client chain contains a
  harmless delivery step, so "no steps may run" forbids a step no user need
  protects — and the operator's incident proved the cost
  [E1: task 01a0aab8].
- **`:917` (server arm) is right by construction, wrong by reasoning.** The
  server arm has *no delivery step at all* — "no push — nothing to push to; no
  branch `-d`" `[E4: internal/tools/task_finish_cleanup.go:818]` — so on that
  path there is nothing harmless for a dirty tree to block, and skipping the
  whole arm is the correct outcome. What is wrong is the **stated reason**: the
  comment claims the guard is "the same guard as the client chain", when the two
  arms' removals are not the same operation (forced vs not). A plain
  `git worktree remove` already refuses a tree with untracked files, so on the
  server the gate's marginal effect is a *better message*, not a protection.
  **Direction: `:917`'s verdict should not change; its justification should.**
  Independent corroboration that this reading is the right one: the unmerged
  repair branch reorders the **client** path and deliberately leaves the server
  arm's assertion in place, unchanged
  [E4: `remotes/peer1/fix/client-closeout-push-delivery:internal/tools/task_finish_cleanup_test.go:1011-1022`,
  and its server removal still unforced at `:1171`].

**N3 — the blast-radius answer stopped at coarse guards; the syscall half of the
operator's question was decided *against*, not left open.** The decision is
recorded [E5: devdoc/shm-worktree-risk-assessment.md:171]
and the coarse half shipped [E4: internal/tools/path_policy.go:210],
`[E4: internal/scratch/limits.go:54,57]`. It was never re-submitted to the
operator for a verdict as a *requirement*, only as an engineering recommendation
inside an assessment document — the task that carried it forward was filed by an
agent, not by the operator [E1: task 01a09765-6b0e-7918-86b2-80f36d0c77a3,
`source=llm`, `status=completed`, 2026-09-12T20:53:21Z, dispatched 12 minutes
after his question]. **That asymmetry is the finding: the operator asked a
three-part question; two parts were answered in code and one was answered by an
agent's engineering judgement, and no ledger row records him accepting that
trade.** The honest form is not a re-raise of the closed `/dev/shm` item (§N4) —
it is: *if you want a command/syscall whitelist, it is still only a
recommendation, not a decision.*

**N4 — `/dev/shm` path policy: CLOSED (do not re-raise).** The operator's
original worry — creating worktrees under the shared-memory mount
[E1: task 01a0975a] — is answered in code: the guard refuses a worktree creation
outside the declared roots and names the target
`[E4: internal/tools/path_policy.go:210]`,
`[E6: internal/tools/bash_path_policy_test.go:39]`, with the companion refusal
for an out-of-root recursive delete
`[E6: internal/tools/bash_path_policy_test.go:56]`. Measured live on this host
while writing this document: **9 worktree registrations, ZERO of them under
`/dev/shm`**. There is no open gap here.

**N5 — the "clean up after yourself" bound was never stated as a bound.** Rule
22 states a criterion; the sweep has an age floor (24 h,
`[E4: internal/tools/worktree_sweep.go:89]`) and escalates rather than destroys
unowned dirty directories
`[E6: internal/tools/worktree_sweep_memory_base_test.go:277]`. Nobody ever wrote
down *by when* a leftover must be gone, or what the operator is owed when it is
not. `[INFERENCE — weakest link: the age floor is a constant in the reaper, and
a constant is not a requirement; the operator's rows ask whether cleanup happens
at all, never how long is acceptable]`.

**N6 — the temp root and the workspace default are decided in one place, but the
module's documents of record for it are five separate devdocs.** The decision
(flag > caller's env > gopi's env > settings, defaulting to a directory under
the workspace, measured by device id rather than spelling) lives in one resolver
`[E4: internal/config/tmpdir.go:79]` and in Rule 22
`[E2: GOPI.md:697]`; its rationale is spread across
`devdoc/tmpdir-default-and-workspace-decision.md`, `tmpdir-param-not-banner.md`,
`tmpdir-settings-value.md`, `tmpdir-settings-injection.md` and
`tmpdir-merge-resolution.md` [OPINION] — a requirement this load-bearing deserves
one document of record, and this is a documentation gap, not a code gap.

### The C-constraint sweep for this module (C1–C5)

- **C1 single binary, zero deps** — **honoured, and load-bearing here.** Every
  mechanism in this module is git plus the filesystem plus Go's own process
  facts (`/proc` reads in the liveness gate); no external service, no daemon
  dependency, `go.mod` carries exactly one direct dependency (pure-Go SQLite).
  [INFERENCE — weakest link: the scan was `go.mod`'s require block plus the
  modules named above; a transitive CGO dependency would only show at build
  time, and this document did not run a full `CGO_ENABLED=0` link]
- **C2 the operator must control the whole thing** — **honoured by construction:
  every root, ceiling and refusal is a readable value.** Declared roots
  `[E4: internal/worktreeroots/roots.go:76]`, one env override for all repos
  `[E4: internal/worktreeroots/roots.go:102]`, child ceilings
  `[E4: internal/scratch/limits.go:54,57]`, sweep age floor
  `[E4: internal/tools/worktree_sweep.go:89]`. Nothing here is hidden state.
- **C3 a daemon must be simple** — **strained, and this is a real §6 entry, not a
  preference.** The close-out logic carries 25 separate skip/return sites in one
  file (`grep -c 'rep.Skipped = true' internal/tools/task_finish_cleanup.go` →
  25) and the client path implements its prelude twice
  (`:963-1021` and `:1929-1978`). Every one of those branches is a way the
  operator can be told "nothing happened", which is exactly the failure surface
  C3 warns about. It is the module's clearest C3 strain.
- **C4 the agent must clean up after itself** — **the governing constraint, and
  G1 is its violation**: the residue was gopi's own debt, so a harmless delivery
  must not be blocked by it (A2). Where the code does name the debt as debt
  (§4.6, N1) it honours C4 precisely.
- **C5 small team** — **honoured in spirit, strained in surface**: the sweep, the
  refusals and the escalation are all operable by one person at night; what is
  *not* one-person-shaped is the number of gates to hold in mind when reasoning
  about "what happens when this finishes on a dirty tree" (C3's strain, same
  cause).

---

## 7. What this implies for tests

**Named existing tests whose verdict this requirement changes (no test is
written here).**

1. `TestFinishCleanupChainDirtyWorktree` — `internal/tools/task_finish_cleanup_test.go:190`,
   with the pinned assertion at **`:201`**
   (`t.Errorf("no steps may run on a dirty worktree: %+v", rep.Steps)`). **This
   assertion must go red under the requirement.** Direction: for a dirty tree on
   the client arm, (i) the delivery step **must have run and succeeded** — the
   branch is on the peer; (ii) the removal step **must NOT have run** — the
   directory survives; (iii) the local branch **must survive**; (iv) the report
   still reports the refusal, with the reason naming which kind of dirt it was
   (A3). The repair branch's own rewrite is the precedent shape: `assertStep(rep,
   "push", true)` plus a peer-holds-branch check, with the "no destructive step
   ran" half retained
   [E4: `remotes/peer1/fix/client-closeout-push-delivery:internal/tools/task_finish_cleanup_test.go:206-243`].
   **Note the trap this document opened in §6 (N2): under this requirement the
   current test *cannot* go red from the client arm alone** — the client arm is
   gated twice (`:1007`, `:1993`), so a reorder at either site alone leaves the
   test passing while the defect lives. A test that only exercises the chain body
   cannot distinguish the two gates. The test (or a sibling) must therefore drive
   the **entry point the production caller uses**, so that deleting one gate and
   not the other is caught.

2. `TestServerRecycleDirtyWorktreeSkipped` — `internal/tools/task_finish_cleanup_test.go:906`,
   assertion at **`:917`**. **Verdict unchanged, justification changed**
   (§6 N2): the server arm has no harmless step to run first (`:818`), so
   "skip everything" is correct there. What must change is the *stated reason* —
   the comment at `:906-907` ("same guard as the client chain") claims an
   identity between two different operations, and a reader who believes it will
   port the client's ordering fix to the server and find nothing to fix. A test
   *could* pin the distinction rather than the outcome: assert that the server
   arm's removal is the unforced form, which is what makes the skip correct.

3. `TestFinishCleanupPlanKeepsEveryGuard` — `internal/tools/task_finish_cleanup_test.go:1762`
   (assertion `:1769`, "a dirty worktree must make the PLAN skip too").
   **Verdict unchanged** — a plan may not be more permissive than the run.
   What a *new* assertion must observe, and does not today: the plan's step list
   for a dirty client tree must **predict the delivery** (the harmless act) while
   predicting **no** removal
   [E4: master `:2018-2026` already renders the push prediction, and the plan
   gate at `:2070-2075` renders the removal prediction — the requirement is that
   the first must appear on a dirty tree and the second must not].

**Packages to look in:** `internal/tools` (the chain and its refusal arms),
`internal/worktreeroots` (root resolution), `internal/scratch` (ceilings),
`cmd/agent` (the resident tick that runs the sweep).

**What a new test would have to observe, per requirement:**
A1/A2 — end state on the *peer* (the branch exists there) **and** on the local
machine (directory and branch intact) after a dirty-tree finish; A3 — the
operator-facing line for both dirt kinds; A4 — that a finished row's directory is
gone or names its reason, and that a leftover resolves to its owning row;
A5 — device ids measured, not path spellings; A6 — the two named refusals;
A7 — the foreign-owner and live-holder refusals.

**A requirement with no test touching it at all — the strongest finding for the
next phase:** **A4's second half.** Nothing observes the *disappearance* of a
directory that has no registration and no row (`§6 G3`), and nothing observes
that a leftover's escalation actually reaches the operator. The sweep has tests
for its classification table, not for the operator receiving anything.

---

## 8. Confidence, weakest links, and the inference register

| # | Claim | Status | Weakest link |
|---|---|---|---|
| 1 | The four-step order and its rationale are the module's safety argument | **Evidenced** — E4 header `:11-23`, E4 `:2139` | none material |
| 2 | The client dirty gate blocks the delivery (G1) | **Evidenced** — E4 `:1007`, `:1993`; E1 `01a0aab8` measured `would-run=0` | none material |
| 3 | The gate is duplicated, so a single-site reorder is defeated (G1b) | **Evidenced** — two sites read on master; the repair branch removed the entry gate | Line numbers are of *this* master `17cec86b`; a rebase of the repair branch shifts them |
| 4 | The two arms differ in destructiveness while sharing the gate (N2) | **Evidenced** — E4 `:951` (unforced) vs `:2079` (forced); E6 `:906-907` vs `:917` | none material; corroborated by the repair branch's own split |
| 5 | `:917` is right by construction, `:201` is the wrong requirement | **Evidenced** — the server arm contains no delivery step (E4 `:818`) | If a server-side delivery step is ever added, this flips; the claim is arm-specific, not timeless |
| 6 | "Never decided" for the over-broad rule (N1) | **Evidenced** — E3 `8f7e824a` (test typed with the chain), narrowing comment `bab6be54` E4 `:173-178`, rediscovery count E4 `:572-573` | git_blame proves *when*, not *why*; the "why" is inferred from a test written in the same commit |
| 7 | The operator's answer to the blast-radius question was partly an agent's judgement (N3) | **Evidenced** for provenance (E1 `01a09765` is `source=llm`, 12 min later); **inferred** for "he never accepted the trade" | Absence of an accepting row is not proof of absence of acceptance |
| 8 | `/dev/shm` path policy is closed (N4) | **Evidenced** — E4 `path_policy.go:210`, E6 `:39`/`:56`, live measurement 9 registrations / 0 under that mount | none material |
| 9 | Rule 22's criterion is honoured by the temp-root resolver | **Evidenced** — E4 `internal/config/tmpdir.go:79`, E2 `GOPI.md:697` | This document did not measure the running process's resolved temp root, only read the resolver and the filesystem layout |
| 10 | Child ceilings are the accepted alternative to a syscall whitelist | **Evidenced** for the decision (E5 `:171`); **inferred** that the operator accepted it | §8 row 7's same weakest link |
| 11 | C1's dependency claim | **Inferred** — `go.mod` require block read; no full `CGO_ENABLED=0` link run in this task | A transitive CGO requirement would only appear at link time |
| 12 | "Over-builds: none found" | **Evidenced as a sweep, not as a proof** — the module's mechanisms were read; a cleverer reading could find one | The two entry-point preludes are the nearest candidate and are classified as a test seam, [OPINION] |

**The module's single weakest link.** Row 5 — the claim that the server arm's
pinned assertion is correct-by-construction. It is the claim that makes this
document *disagree with its own briefing*, and everything in §7 rests on it. If
the server arm acquires a delivery step, or if the arm's unforced removal is
itself judged the defect (a plausible reading: the two arms should be one
operation), then row 5 inverts and `:917` becomes a second instance of the
wrong-requirement pattern. **Cheapest way to settle it:** ask whether the
deliberate asymmetry between the two arms (forced client removal, unforced
server removal) is a decision the operator made or an artifact of two people
writing two arms; no ledger row found in this module's evidence states it.

---

## C. Operator-vision constraints applied to this module

Recorded again here because §6's C-sweep is the version the reader must grade;
this section is the short form. **C4 governs this module** — the operator's own
words ("is gopi clean up after development?") are the requirement, and G1 is
their violation: residue that is the system's own debt was allowed to stop the
system from delivering finished work. **C1 is honoured** with no external
dependency anywhere in the mechanism. **C2 is honoured** — every root, ceiling
and age floor is a readable, reversible value. **C3 is strained** by 25 refusal
sites and a duplicated prelude; that strain is the module's highest-value
consolidation target. **C5 is honoured for operation and strained for
comprehension**, for the same reason.

---

## D. What I could not determine

1. **Whether the two arms' destructive asymmetry is a decision or an accident**
   (weakest link, §8 row 5). Cheapest: ask the operator; or `git_blame` the two
   removal calls and read the commit messages of the two arms' first commits
   (client `8f7e824a`, server arm's introduction) side by side.
2. **What the operator was owed, and by when, for a leftover** (N5). The age
   floor exists; no requirement states a bound. Cheapest: one question — "if a
   finished task's checkout is still on disk after a day, do you want to be told,
   and do you want the system to act?"
3. **Whether he accepted the ceiling-instead-of-syscall trade** (N3). The
   evidence is an agent-filed row, not his. Cheapest: show him the two halves
   (`path_policy.go:210`'s refusal and `limits.go:54,57`'s ceilings) and ask
   whether that closes his "blacklist/whitelist of … system call" question.
4. **Which of the 242 refusals the split comment counts were on which arm**
   [E4: `:572-573`]. The number is recorded; the breakdown is not, and it is
   what would say whether the server arm was ever actually harmed by the
   over-broad rule. Cheapest: re-run the same censuses per node and group by
   node — each command's own summary line already counts candidates and refusals
   [E1: `01a0aab8`'s pasted dry-run output].
   > **Retired mechanism (worktree lifecycle, `58b5618b`, 2026-10-30).** This
   > paragraph used to name `gopi task finish-cleanup --dry-run` as the census to
   > re-run. That spelling is retired and answers exit **1** with one sentence
   > naming what replaced it. What survives is split across the machinery that
   > still runs: `gopi task sweep-worktrees` (the ownership-independent worktree
   > reap, `--dry-run` by default), `gopi task list` (the stale read-out — a
   > row's holder is resolved per row and REPORTED, never acted on; the
   > `--sweep` trigger this row used to name was deleted end to end on
   > 2026-11-05, commit e4521fad),
   > `gopi task merge` (the merge sweep, which is what actually reclaims a
   > ground — this row read `gopi merge` until 2026-11-01, when the top-level
   > verb was removed by the merge-removal epic),
   > and `gopi task status <id>` (a row's own record). Read the ref facts off
   > their owners: `git -C <repo> for-each-ref refs/heads` and
   > `git worktree list`.
5. **Whether any *unregistered* directory currently exists** (G3). The
   registration list is readable (`git worktree list` → 9, measured); the
   complement is not enumerable from the repo alone. Cheapest: a filesystem
   scan of the declared roots for `.git`-file directories absent from the
   registration list.
6. **The exact state of the parked repair** — `6e22d80b` plus five docs commits,
   6 ahead of master and not an ancestor of it, parked at
   `archive/fix/client-closeout-push-delivery`, with merge-resolution row
   `01a0ab2c` still `pending`. This document read the branch's diff and its
   test rewrite; it did not run it, so "the fix works" is **not** a claim made
   here. Cheapest: the pending row.

---

## The one finding that is new to the operator

**The two close-out arms are not the same operation, and the tests say they
are.** The client removes a finished checkout with `--force` and deletes the
branch after delivery; the server removes the *same* checkout **without** force
and keeps the branch as merge input
`[E4: internal/tools/task_finish_cleanup.go:951]` vs `:2079`,
`[E4: internal/tools/task_finish_cleanup.go:818]`. Both are guarded by the same
dirty check, and both tests confirm the same sentence — "no steps may run on a
dirty worktree" — including the server test's comment that it is "the same guard
as the client chain" `[E6: internal/tools/task_finish_cleanup_test.go:906-907,917]`.

Two consequences the operator can act on, neither of which appears in the code's
own comments:

1. On the **server** the over-broad rule is *correct* — there is no harmless step
   it can block — so the series' worked example is **half** right: one test
   encodes the wrong requirement (client, `:201`) and the other encodes the right
   outcome for a reason its own comment gets wrong (server, `:917`). A repair
   that "fixes both assertions" would change a correct verdict.
2. On the **client** the rule is gated **twice** (`:1007` before the delivery
   target is resolved, `:1993` inside the chain body), so a maintainer who
   reorders the gate where the ordering header lives still ships the defect —
   which is the exact shape the unmerged repair branch had to solve
   [E4: `remotes/peer1/fix/client-closeout-push-delivery:…:1183-1225`].
