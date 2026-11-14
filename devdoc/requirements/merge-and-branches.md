<!-- doc-class: normative -->
# Requirements (reverse analysis) — M2: merge, branch lifecycle & the merger daemon

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Series**: module **M2/14** of the reverse requirement analysis
(`devdoc/requirements/README.md`, `00-module-map.md` §M2, briefing
`01-per-module-briefings.md:87` — `[2/14] M2 — Delivering finished work to the
trunk`). Method, `why` standard, the 8-section shape and the operator's binding
constraints C1–C6 come from `00-method-and-standards.md` §1–§6. Row:
`01a0ab12-3115-70ff-973a-65fea6f16639` (this dispatch) — a **duplicate** of row
`01a0ab11-654f-7322-a6ee-53cb341f53b3`, which already produced two documents on
master. Date of this pass: 2026-09-18. Host: HP (`role: client`). Every
measurement below was taken on **master `812f9a60`** (2026-09-18 18:51:27 +0800)
inside the worktree `~/workspace/gopi-req-b2b`, branch
`docs/requirements-merge-b2b`.

---

## Front matter — what this document is, and how it relates to the two that exist

Rule 26 governs this row: *an incomplete fix is repaired by dispatching again,
never by waiting*, and *the briefing must carry the reason and the evidence*.
The reason this row exists is not recorded anywhere I can find; what *is*
recorded is its effect — **the module already has two documents on master**:

| document | produced by | size | base of record |
|---|---|---|---|
| `devdoc/requirements/merge-branch-lifecycle.md` | row `01a0ab11…` (`13d2bbcf`, 2026-09-17 01:33) | 836 L | master `17cec86b` |
| `devdoc/requirements/02-merge-and-branches.md` | same row, revision 2 (`0203ca5a`, 2026-09-17 04:33) | 1,002 L | master `17cec86b` |

Both are ancestors of this pass's base
(`git merge-base --is-ancestor 0203ca5a 812f9a60` → true), and both were written
against `17cec86b`. **Between `0203ca5a` and `812f9a60` master took 564 further
commits** (`git rev-list --count 0203ca5a..812f9a60` → `564`), and that window
contains a design replacement of the very subsystem both documents describe.
The increment this document owes is therefore **not a third copy of the same
analysis** — it is the analysis at the *current shape*, and where the two
predecessors' findings have been overtaken by history, it says so by name.

**I read both predecessors before writing** (`read`, at the base of record; no
`git show` of another worktree, Rule 29). Their §-shape is not reproduced: §1–§3
below are deliberately shorter than theirs and §4–§8 carry the new material.
The overlap is stated here rather than hidden, per Rule 26.

**What changed in the window, and is the spine of §4–§7:**

| # | commit | date | what it did |
|---|---|---|---|
| ① | `925194b0`, merged `5666b0b9` | 2026-09-17 19:34 | **removed the park mechanism** — a non-ff branch is REPORTED, never tagged (`internal/tools/repo_sweep.go:328-345`) |
| ② | `24ed9ad1` | 2026-09-17 19:44 | landed the **take-over**: the tick launches a fresh gopi whose prompt carries the judgement (`cmd/agent/merger_takeover_prompt.md`, 858 L) |
| ③ | `762bc8eb` | 2026-09-17 19:54 | the **branch-lifecycle ruling**: `conflict/` is void, *deletion is the endpoint* (`devdoc/branch-lifecycle-ruling.md`) |
| ④ | `af74`/`01a0af74` → `merger_loop.go` | 2026-09-18 | **restored `--every D`** as an in-process loop (epic merger-self-driving) |
| ⑤ | `128019af` | 2026-09-18 15:07 | the serial face legs ride the launching face too |
| ⑥ | `8459c6cf` | 2026-09-18 16:49 | arm the loop's stop handler before the lock publishes its pid |
| ⑦ | `5f0641a3` | 2026-09-18 18:46 | delivery-point sync: merge master into the delivered branch before close-out |

Consequence for the predecessors: `02-merge-and-branches.md` carries **87
occurrences of "park"**, three of its ten requirements (R3, R4, R10) are *about*
the park lane, and its §7.1 names the park tests
`TestRepoSweepParkedBranchIsNotReParked` / `TestRepoSweepParkBranchRowIsIdempotent`
— **neither file exists on master any more** (`ls internal/tools/*park*` → no
match; the tests went with `925194b0`). Its §6.3 over-build finding G-5 ("the
park and the hand-abandonment share one namespace") is **moot in shape and its
conclusion survives in a different form** (§6-OB-1 below). Measured live, this
pass: `sqlite3 ~/.gopi/memory.db "select count(*) from tasks where status='pending'
and summary like '%parked at archive/%'"` → **`0`**; pending rows in total → **33**.

**Boundary** (kept from the predecessors, because it is right): everything
between *"a branch exists carrying finished work"* and *"that work is on the
shared trunk, or the operator has been handed it back as a named piece of
work"*. Out of scope, and cited only where it decides a merge outcome: the
transport that carries a branch between hosts (M4), the ledger itself (M1), the
worktree lifecycle as a resource question (M3), the post-merge rebuild as a
build concern (M11).

---

## 0. The operator's binding constraints (C1–C6), applied to this module

Quoted from `00-method-and-standards.md:359-457` with the evidence each carries
there. They are **filters**, not background: a requirement that violates one is
not a gopi requirement.

- **C1 — one static binary, zero dependencies, no plugin architecture**
  (`00-method-and-standards.md:377`; `devdoc/roadmap.md:64`;
  `internal/agent/loop.go:895`; `briefings/brief-requirements-reverse-analysis.md:125`).
  For this module: the
  merger may not acquire a queue broker, a message bus, or a second daemon
  product to deliver a branch. `go.mod` has one direct dependency
  (`modernc.org/sqlite`), and the merge engine is stdlib + `git`.
- **C2 — the operator must be able to control the whole thing**
  (`00-method-and-standards.md:395`; `briefings/brief-requirements-reverse-analysis.md:128`). For this module:
  `gopi merger --status`, `--stop`, `--restart` are the operator's hands
  (`cmd/agent/main.go:560-600`), and "an action the operator cannot observe or
  reverse is a defect". This is the constraint §6-OB-2 is judged against.
- **C3 — a daemon must be simple: every branch of logic multiplies its failure
  surface.** Verbatim, row `01a0a995-8742-77fc-a164-53054ea652c5`
  (`00-method-and-standards.md:407-413`): *"A daemon
  must be simple. Every branch of logic it carries multiplies its failure
  surface. The merger's logic itself is the defect — not any one bug in it."*
  This is the strongest constraint on this module and the one §4-R2 and §6-OB-2
  are built on.
- **C4 — the agent must clean up after itself; dirty is a violation, not a
  condition to tolerate** (row `01a0975a-3e3f-7ae5-a848-a98cd24d6dc3`). Per
  `00-method-and-standards.md:415-427`: *"under C4 the residue was always gopi's
  own debt, which is exactly why the push (harmless) should have run and the
  removal (destructive) should have deferred."* **That sentence is this
  module's §7 verdict, decided at series level before this document existed.**
- **C5 — a small team with limited resources; solutions one person can run**
  (rows `01a08b9a-e5…`, `01a08b92-76…`). For this module: cost and cognitive
  budget are requirements, which is what §6-OB-1 and §6-OB-3 measure.
- **C6 — `why` over `what`** (`00-method-and-standards.md:446`). The premise of
  this whole series.

**Positive vision recorded as constraints (the operator's own words, from the
briefing set):** *single static binary, zero deps* (C1); *the operator controls
the whole thing* (C2); *a daemon must be simple because every branch multiplies
the failure surface* (C3); *the agent cleans up after itself* (C4); *solutions
one person can run* (C5); *why over what* (C6).

---

## 1. What this module is for, in product-owner language

**The module exists so that work finished on one machine lands on the shared
trunk with nobody present.** The operator files work before sleeping, on a laptop
that will be closed; the work finishes on whichever machine ran it; the result
must reach the trunk the other machine reads — *"work finished on one machine
must land on the shared trunk with no human present"* (this epic's own goal
sentence; the same need is stated in `merge-branch-lifecycle.md:11-30` and
`02-merge-and-branches.md:242-263`).

Two things follow from "nobody present", and they are the whole design:

1. **The part that runs unattended must be mechanical.** It cannot ask a
   question, because nobody is there to answer. So the merge decision is
   **pure git**: a branch whose tip is an ancestor's descendant of master is
   applied; one that is not is left exactly where it is and reported
   (`internal/tools/repo_sweep.go:16-25`, `:318-323`, decision at `:618`).
2. **Anything needing judgement must be handed over, not attempted.** Where the
   judgement is unavoidable — is this branch landable, is a peer still working,
   is this a dead holder — the design does not put a heuristic in the daemon. It
   puts the judgement **in a short-lived gopi's prompt**
   (`cmd/agent/merger_takeover_prompt.md`, §0 "You were started by `gopi merger`,
   a **cron launcher**"), and the launcher starts it and forgets it
   (`cmd/agent/merger_takeover.go:310-326`: *"⑤ Start, then RETURN. There is no
   step ⑥."*).

The module's *product* is not the merge. It is **the guarantee that the trunk
catches up by itself and that the machine never lies about it**.

---

## 2. Who is served

### 2.1 The operator (nick) — the one who must be able to leave

He needs: work he filed to land while he is asleep; a way to see whether the
trunk is catching up; and — when a branch cannot land — to be told, in a form he
can act on. Constraint C2 gives him `--status` / `--stop` / `--restart`
(`cmd/agent/main.go:562-572`). The row that most sharply states the need is
`01a0af74-bceb-7646-8f2e-8afdec306646`: *"The merger drives ITSELF — restore
`--every` as an in-process loop … Today nothing polls"*.

### 2.2 The agent (Bond) — a user of this module in two opposed roles

**Producer**: a run must leave a branch the merger can take — and it must not
leave *dirt* that withholds it (PROJECT.md Rule 30, `:1120`; row
`01a0aaa7-47a2-7c45-8272-6f051d489150`). **Consumer**: a run is also handed work
*by* this module, as a rework task
(`cmd/agent/merger_takeover_prompt.md` §3.6, `:399-437`).

### 2.3 The two nodes — HP (client) and peerbox (server)

The client is where work is finished; the server is where the trunk lives
(Rule 28, `GOPI.md:1035-1077`). The delivery obligation is **on the client**:
push the branch to the peer, then remove the worktree, then delete the local
branch — *in that order*, because the order is the safety argument
(`internal/tools/task_finish_cleanup.go:6-33`, steps ②③④).

### 2.4 The future maintainer — the actor C3 is really about

C3 is not about uptime; it is about **who can reason about the thing**. The
maintainer needs to be able to hold the merger's whole behaviour in his head.
§6-OB-2 measures how far the built system is from that.

---

## 3. The end-to-end workflow it serves

Measured on master `812f9a60`; every step cites where it is implemented.

```
  [a run finishes on HP, task row terminal, branch on the client]
        │
        │  ① CLIENT CLOSE-OUT (internal/tools/task_finish_cleanup.go)
        │     guard → ①a branch's own test run → ② push → ③ worktree remove --force → ④ branch -d
        │     (dirty probe, branch-check, push, worktree remove, branch -d — the step NAMES,
        │      not line numbers: the numbering below was pinned at 812f9a60 and the step set
        │      changed on 2026-10-12)
        ▼
  [branch arrives on the server's repo]
        │
        │  ② CRON TICK  `gopi merger`  (cmd/agent/merger_tick.go:149)
        │     a. master safety, ONCE, before anything else (:176, :407)
        │     b. resident sweeps: scratch GC + task lifecycle (:197-198)
        │     c. pre-spawn gate — a live take-over of THIS loop? skip (:229)
        │     d. mergerTakeoverRun → spawn ONE detached gopi, RETURN (:237)
        ▼
  [take-over gopi: cmd/agent/merger_takeover_prompt.md, 858 lines of prompt]
        │     judgement: is a peer working? is it dead? is this ff-able?
        ▼
  [per configured repo: pure-git sweep — internal/tools/repo_sweep.go:976]
        │     DISCOVER unprotected candidates → DECIDE with git alone
        │     ff-able  → merge, prune the ref
        │     not-ff   → LEAVE ALONE, report (never tag, never file, :328-345)
        │     then MergedCleanupSweep tidies already-merged ground (:1049)
        ▼
  [rebuild per the repo's declared contract, then the next tick]
```

Two faces of the same tick exist and share one body: the **bare cron tick**
(`gopi merger`, one tick, exits 0) and the **self-driving loop**
(`gopi merger --every D`, `cmd/agent/merger_loop.go:200`, tick → sleep → tick
under one flock held for the loop's whole life). `--once` runs the pass
synchronously in-process (the E2E seam, `cmd/agent/cli_tasks.go:4163` `mergerPassOnce`).

**The property that makes the workflow work unattended** is the one the
predecessors did not have: the tick **spawns but never waits**
(`cmd/agent/merger_tick_no_child_gate_test.go:14-16`: *"the merger SPAWNS, but it
never WAITS. It leaves no handle it could reap, no registry it could enumerate,
and no loop it could block in."*). The gate that pins it is a source-text
reading over six files, with a read floor **and** a presence control
(`:203-218`, `:24-27`) — an absence check is satisfied by deleting the feature,
so the spawn is required on the same file list (`mtgPresenceSpawn`, `:207`).

---

## 4. Why it exists — the requirement, traced

Each R is one clause of the need. `[INFERENCE]` marks a clause no operator
sentence states directly; the weakest link is named in the same breath.

### R1 — Finished work must reach the trunk with nobody present

**Citation.** Row `01a0af74-bceb-7646-8f2e-8afdec306646` (*"The merger drives
ITSELF … Today nothing polls"*, operator, verbatim summary);
`cmd/agent/merger_loop.go:11-20` («the merger is exactly the component whose job
is to run when nobody is watching»); `cmd/agent/merger_takeover_prompt.md:1-11`
(*"It **exited immediately** and is waiting on nothing — there is no supervisor,
no handshake and nobody to report to but this document and the ledger."*).
**Served.** `mergerResidentLoop` (`merger_loop.go:200`) + the cron form
(`merger_tick.go:237`).

### R2 — The unattended part must not carry judgement (C3, restated as a requirement)

**Citation.** Row `01a0a995-8742-77fc-a164-53054ea652c5`, operator verbatim:
*"A daemon must be simple. Every branch of logic it carries multiplies its
failure surface. The merger's logic itself is the defect — not any one bug in
it."* The built answer, verbatim in code: `repo_sweep.go:13-25` — *"ONE TICK =
PURE GIT, NO CHILD, NO LLM, NO WAIT"*; `:969-975` — *"NO CHILD, NO LLM, NO WAIT
… the whole tick is a sequence of git invocations and nothing else"*. And the
complementary half — where the judgement went — `merger_takeover_prompt.md:1-11`
(*"The launcher deliberately contains **no merge judgement**. Every question
below is yours to answer from machine facts."*).

**How the requirement is enforced, not merely asserted.** Three rings:
`merger_tick_no_child_gate_test.go:356` (no WAIT on the merger's own faces, with
the presence control at `:203-218`); `merger_takeover_gate.go:24-34` (the
pre-spawn gate — *"a take-over THIS LOOP STARTED is still running ⇒ the round is
SKIPPED"*, which closes exactly the *"alive but stopped reporting"* hole that
produced three concurrent take-overs and seventy zombies in one unattended night,
measured at `:9-10`); and `merger_takeover_prompt.md` — the prompt is
`//go:embed`-ed (`cmd/agent/merger_takeover.go:284`), *"`go build` fails if the
file is missing, so a binary can never carry an empty prompt"*.

### R3 — A branch that cannot land must not block, and must not be silently buried

**Citation.** `repo_sweep.go:325-345` (`repoSweepNotFFOutcome`) — the tick's
whole answer to a non-ff branch is *"REPORT it and change NOTHING"*, and the
comment names the two designs it replaced. Operator ruling on the endpoint:
`devdoc/branch-lifecycle-ruling.md:9-21` (row
`01a0af2f-b94d-76e3-8565-b35df602aa05`), quoting nick 2026-09-17: *"if we decide
to resolve it, then there is no need to rename it, so, just combine the task."*
and *"**The endpoint of a branch's lifecycle is DELETION.**"* Hand-over form:
`merger_takeover_prompt.md:399-437` (§3.6 — a **rework TASK**, *"never a tag"*).

### R4 — Conflict resolution is work, not a wait

**Citation.** `merger_takeover_prompt.md:429-431` — *"**Never**: force a merge, or rewrite a branch to make `--ff-only` succeed."*; and
`cmd/agent/main.go:598-599` — *"Conflict RESOLUTION is the worker path's job, not
this process's."* The proposal to keep a park ("bury the branch") was
withdrawn as *"the OLD merger algorithm"* (row
`01a0af0c-d865-7cd1-9dda-432d9fa21e09`, completed).

### R5 — The merge decision must be independent of the ledger

**Citation.** `internal/tools/repo_sweep.go:4-11`, verbatim: *"the merger is a
PURE git sweep driven by the settings.json "repos" table — NOT task-oriented,
NOT DB-driven … Whether a task row tracks the branch is irrelevant to the merge
DECISION — merging is fully decoupled from the task ledger."*; and `:39-41` —
*"The tick reads NO ledger state to decide anything."* Source of that design, in
the operator's words: row `t-1788468369`, verbatim: *"There is an even simpler
design to just sweep merging which is not task-oriented: in the config file we
specify the repo.path and the merger just periodically cd's to repo.path and
merges all branches (worktree-removed branches can be merged). This is even
simpler."*

### R6 — The merge trigger must be a git fact, not a proxy

**Citation.** `repo_sweep.go:16-24`: a checked-out branch is *"ACTIVE BOND
WORK"*, and the sweep's own occupancy guard is *"load-bearing"*. The operator's
own framing of the trigger (row `t-1788389175`): *"merge-trigger = NO worktree
protection (pure git fact)"*. **This requirement is the one the client close-out
chain must obey**: `task_finish_cleanup.go:861-868` — *"a finished run's parked
worktree starves the merger (its merge trigger = 'no worktree protection')"*.

### R7 — Cleanup is the machinery's job, and the ordering is the safety argument

**Citation.** `task_finish_cleanup.go:6-33` — *"Without this chain a delivered
branch lingers forever as a worktree + branch pair"*; the order ② push → ③
worktree remove → ④ branch -d, with each step's reason stated: ③ *"ONLY after ②
succeeded (a removed worktree with an unpushed branch = work loss)"*, ④ *"-d
refuses unmerged branches (the double lock …)"*. Constraint C4
(`00-method-and-standards.md:415`) makes this a requirement rather than a
tidiness rule.

### R8 — The operator must be able to see the state that matters, without asking a process

**Citation.** `internal/tools/merger_status.go:706-740` (`Render`) and the state
model at `:76-98`; the builder of that model, row
`01a0ac6b-2342-735d-a4eb-f1eda8200787` — *"merger `--status` must be trustworthy:
no swallowed errors, distinguish 'nothing to do' from 'stuck'"*. The operator's
own asks: `01a0a949-3683-771c-a6fa-c454b676b60f` (*"merger is losing track"*,
`verified`), `01a0a9b5-5494-758e-a37f-7b8ee4486de4` (*"merger status should show
next running time"*), and the refusal-shaped one `01a0b182-e62c-7e30-821f-0ff723459c17`
(*"merger status text misleading"*).

### R9 — What lands must actually work

**Citation.** PROJECT.md Rule 12, `:352-357` — *"Merge → cleanup → **REBUILD** is
the last step: without it the machine runs the OLD code against NEW master (the
recurring stale-bin incidents)."* Implemented as a tick-level invariant armed as
a defer *before any work*, `cmd/agent/cli_tasks.go:2726-2747` — *"the tick's very
first statement, before any work and before the repo list is even resolved"* —
and as a per-repo step after the sweep (`cmd/agent/cli_tasks.go:2824` →
`mergerSweepRepo`). Post-merge red is a live defect class here: row
`01a0af12-2a1a-7b23-8884-6e10b8d2b90f` (*"The post-merge VET gate only REPORTS —
a merge that breaks stays in master"*).

### R10 — A finished run must not withhold its own deliverable

**Citation.** PROJECT.md Rule 30 (`:1120-1135`) plus its mechanical half; row
`01a0aaa7-47a2-7c45-8272-6f051d489150` — *"A run must leave its worktree CLEAN:
dirty scratch permanently removes the deliverable from the merge queue"*; and
the operator's live incident, row `01a0aab8-ea14-7ad6-88ed-e0012f2a5555`,
verbatim summary: *"client push must not be gated by the worktree dirty guard —
a safe step is blocked by a guard for destructive steps"*. This requirement is
**the one the built system still violates** — §6-US-1 and §7.

### [INFERENCE] R11 — The delivery is one decision, not three

**The inference.** The three close-out steps are written, tested and reasoned
about as *one act with an internal order* (`task_finish_cleanup.go:6-33`), yet
they are independently guardable — and the guards are applied at the front door.
**Weakest link:** no operator sentence says "the chain is atomic"; I infer it
from the ordering argument being called *"the safety argument"* and from the
double-lock in step ④, which only makes sense if the steps are one claim. If the
inference is wrong, the fix for §6-US-1 is "reorder the steps"; if it is right,
the fix is "partition the step set by reversibility, then guard". The two
differ in what the tests must assert (§7).

---

## 5. Acceptance in user terms — so a test author can tell a bug from allowed behaviour

Written so that each line is decidable by reading one command's output.

**A1. The trunk catches up by itself.** With a fast-forwardable unprotected
branch present in a configured repo, a tick lands it with no operator action
(`repo_sweep.go:618` → `:1021`). *Not a bug:* a tick that lands nothing when
nothing is landable — `repoSweepNotFFOutcome` is a **correct, successful**
outcome (`merger_takeover_prompt.md:24`: *"Doing nothing is a correct,
successful outcome."*).

**A2. A branch that cannot land is still there afterwards, and is named.**
*Bug:* the branch is tagged, renamed, deleted, or its worktree torn down as a
side effect of the refusal (`repo_sweep.go:328-345` lists exactly these three as
removed). *Allowed:* the same branch reported again on every later tick — that
repeat is the signal that it is still waiting (`:343-344`).

**A3. A delivered branch reaches the peer even when its worktree is dirty.**
*Bug:* the push is skipped because the tree is dirty. *Allowed:* the worktree
removal and the branch deletion are skipped — those are destructive and a dirty
tree may hold a live writer's work (`task_finish_cleanup.go:962-963`).
**This is the acceptance criterion §7 turns into test verdicts.**

**A4. Cleanup never destroys work it cannot prove it delivered.** Steps ③ and ④
run only after ② succeeded (`task_finish_cleanup.go:2280-2290`, `:2332-2340`).
*Bug:* a `-D` on a ref this sweep's push did not land. *Allowed:* a branch kept
because the push failed.

**A5. The operator can tell "catching up" from "stuck" from one command.**
`gopi merger --status` prints the lock holder, the state, the take-overs and the
per-repo rows (`merger_status.go:706`). *Bug:* a live takeover rendered as
`not running` (row `01a0b182`, the defect this surface was rebuilt for), or a
swallowed probe error.

**A6. A land that breaks the build is visible.** Rule 12's close loop fires the
`build-failed` notify event (`GOPI.md:358-362`); the merge is *not* rolled back —
*"Build failure ≠ rollback"* is stated policy, not a bug.

**A7. The unattended part never waits on anything it started.** *Bug:* any
`Wait`/registry/handle on the merger's own faces — pinned by source text
(`merger_tick_no_child_gate_test.go:94-110`), with the presence control that
stops "delete the feature" from passing.

**A8. `--every` repeats the tick; it does not create a service.** Admitted as a
**sleep parameter**; `--daemon/--start/--wait/--force` stay refused
(`cmd/agent/main.go:513-515`), `--stop/--restart` are control faces
(`:444`). *Bug:* a flag honoured on one face and silently ignored on the other
(`:446-451` names that as the routing reason).

---

## 6. Gap analysis — where the built system is judged against R1–R11

### 6.1 Under-serve

**US-1 (HIGH, live). R10/R11 are violated: the safe step is still gated by the
destructive step's guard.**

The client close-out chain's dirty gate runs **before** the delivery target is
resolved and **before** the push:

| # | site | function | what it does |
|---|---|---|---|
| ① | `internal/tools/task_finish_cleanup.go:1145` (`:1129-1148`) | `taskFinishCleanupChain` | reads porcelain, on any dirt sets `Skipped` and **returns** — before the peer is even resolved (`:1156`) |
| ② | server arm (`finishCleanupServerRecycle`) | `finishCleanupServerRecycle` | same guard on the server arm (**that arm has no push step**, so it is correct there) |
| ③ | `:2126` (`:2151-2166`) | `runFinishCleanupChainTarget` | same guard again in the chain body |

The chain's own step set is otherwise ordered exactly as the requirement wants:
the branch's own test run (`finishCleanupBranchCheckStep`) ahead of the push,
`② push` at `:2232`, `③ worktree remove --force` at `:2283`, `④ branch -d` at
`:2335`.

> **CHANGED (2026-10-12, task `01a130a1`)**: the step this note used to call
> `①b pre-merge verify` is GONE — the client no longer fetches the peer's master,
> merges it into the delivered branch or rebuilds the composed tree. What runs
> there now is the branch's **own** test run, at the branch's **own** revision,
> recorded on the row's trail and **not blocking**. Whether the branch merges,
> and whether the composed tree is sound, is the merger's decision on the
> integration branch. The measurement and the reasoning:
> `devdoc/client-trial-merge-removal.md`. **The push is idempotent and moves
only a peer ref** (`git push <peer> <branch>:<branch>`, `:2232`); the two steps
the guard exists for are `--force` removal and `-d`.

**Measured live, this pass:** site ①'s guard is unreachable-by-repair from the
body — a maintainer who reorders inside `runFinishCleanupChainTarget` (site ③)
still ships the defect, because `taskFinishCleanupChain` returns at `:1148`
before the body is reached. **The entry-arm gate is a door, not a check.**

> **RESOLVED (2026-09-19, epic client-push-rebase / branch
> `feat/client-push-rebase` @1732dc81).** All three sites are closed. Sites ①
> and ③: the dirty probe is computed ONCE (`finishCleanupRemovalReason`) and
> consulted only at the removal, so the delivery (①a branch check → ② push) runs
> first and a dirty worktree no longer costs the branch its push. Site ①'s
> duplicate probe, its two returns and the empty-target return are DELETED —
> the outer `taskFinishCleanupChain` is now a resolver (branch + worktree) that
> hands over, so there is no longer a door in front of the check. Site ②
> (`finishCleanupServerRecycle`) is UNCHANGED and remains correct: that arm has
> no push step, so its own negation claim is legitimate (its test's wording was
> narrowed to say so — it had been the client arm's sentence word for word).
> The skipped cell this section points at,
> `TestFinishCleanupClientDirtyStillDelivers`, is un-skipped and green;
> `devdoc/client-push-rebase-20260919.md` carries the ordering contract, the
> new regression tests and the red proof.

**The requirement's verdict is already written down in this repo, and it is not
ambiguity.** `00-method-and-standards.md:415-427` (under C4): *"the residue was
always gopi's own debt, which is exactly why the push (harmless) should have run
and the removal (destructive) should have deferred."* And the test the fix owes
exists as a **skipped test carrying its own reason and owner**:
`internal/tools/is_server_node_test.go:210-233`, `TestFinishCleanupClientDirtyStillDelivers`,
`t.Skip("MISSING ROLE×STATE CELL (client+dirty): the destructive steps stay
refused but the idempotent push must still run — the chain returns Skipped
BEFORE the push, so this test is RED against master; owned by task 01a0aab8 …")`.
The red run is recorded: `devdoc/assert-the-bug-audit-20260917.md:345-362`
(§7.3 — *"the finished branch must be DELIVERED even from a dirty worktree"*,
with the actual report line
`reason="worktree dirty (live bond protected) — carries ONLY UNTRACKED files …
: ?? wip.txt"`).

**The row that owns it says `completed`.** `01a0aab8-ea14-7ad6-88ed-e0012f2a5555`
→ status `completed`, `ended_at` 1789577411 = 2026-09-17 00:50:11 +0800. Its
sibling `01a0aaef-5a90-7e02-9276-471d61b8f565` (row `01a0aab8`'s own
close-out reformulation: *"client close-out: deliver the branch independently of
worktree cleanup (push before the dirty guard …)"*) also says `completed`
(ended 2026-09-17 03:41). **The code at the tip says otherwise** — the guard is
still first. The history of what happened, measured (`git show 6e22d80b --stat`,
`085811f8`):

1. `6e22d80b` (2026-09-17 00:20) *"fix(closeout): deliver the branch before the
   dirty gate; make a stranded delivery observable"* — **the correct repair, and
   its commit message states exactly this requirement**: *"the guard exists to
   protect the destructive steps (`worktree remove --force`, `branch -d`), which
   stay behind it unchanged — only its REACH moved."* It is an ancestor of
   master (`git merge-base --is-ancestor 6e22d80b master` → true).
2. `94666f7d` (06:14) merged the branch; the merged amount was *one file*
   (`devdoc/client-closeout-delivery.md`).
3. `085811f8` (06:50) *"sweep: back out feat/client-closeout-delivery (its
   conflicted shared files were resolved master-side, half-applying it: 6
   merge-induced reds)"* — **deleted** `internal/tools/task_finish_delivery.go`
   (430 L) and `task_finish_delivery_test.go` (239 L), and reverted the reorder.

**Measured on master now:** `grep -rn 'FinishCleanupDeliveryPass' internal cmd`
→ **zero hits**; `ls internal/tools/task_finish_delivery*.go` → only
`task_finish_deliverysync.go` (a *different* later feature). So the repair was
written, merged in part, backed out in full, and the owning rows were closed —
**the defect is live and its ticket is closed.** That is the purest form of the
umbrella's own thesis (`README.md`, finding 1) and it is why this row exists.

**US-2 (MEDIUM, live). The over-broad assertions that pin the defect are on
master, and their own audit file says whose verdict they carry.**
`internal/tools/task_finish_cleanup_test.go:202` and `:923` assert *"no steps may
run on a dirty worktree"*; the audit classes them **over-broad**
(`devdoc/assert-the-bug-audit-20260917.md:57-58`, with the measurement: *"the
chain's step ② is `finishCleanupPushVerb` (`:2028`), an idempotent push of a
peer ref; the dirty guard exists for `worktree remove --force` (`:2065`) +
`branch -d` (`:2127`)"*, and the test's own comment at `:189` names the false
half — *"no push"*). The audit's allow-map entry
(`internal/tools/assertion_totality_allow_test.go:47`) still ends
*"OWNED BY 01a0aab8"* — an owner whose row is `completed`. **Direction:**
narrow the guarded set to the destructive steps, then the assertion becomes
"destructive steps refused, delivery ran" — §7.2.

**US-3 (MEDIUM). The "is there anything to do" half of delivery is invisible
when the peer is unreachable.** The client arm resolves its target from the sync
peers (`finishCleanupServerTarget`, `:1207`); an empty target is *"NO peer
declared (the single-node deployment) versus a declared-but-unusable alias"* —
one skip reason for two conditions, distinguished by text only
(`noServerTargetSkipReason`, `:1165-1180`). **This is the predecessor's G-2/G-3
in a new shape** (`merge-branch-lifecycle.md:332` *"the client never does the
pre-merge"* — that half is now built: `internal/tools/task_finish_premerge.go`,
599 L, landed `bf9fbef8`/`3c396080`/`5f0641a3`), and what remains is that a
**misspelt peer alias and a single-node host read the same on the row.** Under
C1 (no second product) the fix is a distinct reason, not a new mechanism.

### 6.2 Over-build

**OB-1 (HIGH, measured). The tick runs four sweeps whose subject is not the
merge, and the operator has already asked for them to leave.**

Per `cmd/agent/merger_tick.go:197-198`, **every** tick — before the launch, on
both faces — runs `scratchGCSweep(mergerRoleLabel)` and
`runTaskLifecycleTick(mergerRoleLabel)`. A third family arrived with
`128019af`/`b0f8ed16`: `mergerLaunchSurvivingLegs` (`merger_tick.go:332`)
carries ① the declared-rebuild drain, ②③ the dead-holder resume/reclaim ladder,
④⑤ the cross-node sweeps (`serveAdoptionSweep`, `serveGitSyncSweep` —
`cli_tasks.go:301`). The code's own justification is measured and clear
(`merger_tick.go:183-201`: the tick's E2E suite pins the zombie disposal on a
BARE tick), but the **criterion** it lacks is one the operator has already
stated and dispatched. Pending rows `01a0b31f-fc1f-7f28-86b3-8e8a2c6ca1cd`,
`01a0b31f-fdbb-720c-8492-6d69b6db0539`, `01a0b31f-ffdd-7651-9008-db8a84ad9b01`
(split from `01a0b19d-c616-790b-b40d-ba78fa524e9b`) ask for exactly the missing
thing, verbatim: *"the criterion (git-ref/worktree-scoped ⇒ merger;
ledger-row/task-scoped ⇒ not merger)"* and *"Slim the merger tick to its scope.
Remove from the merger execution path every leg the criterion does not keep."*
Given that criterion, `scratchGCSweep` (age-based scratch), `runTaskLifecycleTick`
(stale report + confirmed-zombie disposal + finish cleanup) and
`serveAdoptionSweep`/`serveGitSyncSweep` are **ledger/task-scoped** — they fail
the merger's own test. **Direction:** the legs move to a task-scoped trigger; the
merger keeps the git-ref/worktree-scoped legs. **The cost of not doing it is
C3's, in raw numbers**: the merge-domain production surface is **14,009 lines**
in `internal/tools` plus **3,874** in `cmd/agent` (recomputed this pass over the
named files; the predecessor measured 20,005 over a wider family on `17cec86b`)
and **74 merger tests** in `cmd/agent` alone — for a component whose requirement
is *"pure git, no branch of logic"*.

**OB-2 (MEDIUM). Four gate/retry-ish rings guard a spawn that the design says
must not be guarded.**

The launcher spawns one child and returns (`merger_takeover.go:310-326`). The
support that has grown around that spawn, measured by file:

| file | lines | what it is for |
|---|---|---|
| `merger_takeover_gate.go` | 218 | the pre-spawn gate + a `wait4(WNOHANG)` reap backstop |
| `merger_takeover_lock.go` | 479 | proving the flock travels as fd 3, and restamping/adopting a lock in the child |
| `merger_takeover_scope.go` | 392 | a three-rung cgroup-escape ladder with probes and three operator messages |
| `merger_takeover_progress.go` | 721 | the round's own progress record and its CLI face |
| `merger_restart.go` | 707 | the `--stop`/`--restart` control faces |
| `merger_loop.go` | 290 | the loop |
| **total (own faces only — the nine `merger_*.go` files)** | **3,874** | the merger’s whole process surface |

Each has a measured incident behind it (`merger_takeover_gate.go:5-14`: *"23:35
three take-overs alive at once … 05:33 seventy zombies"*; `merger_takeover_lock.go`
the hand-off proof; `merger_takeover_scope.go` the detach rungs). **I am not
calling these wrong.** I am recording that C3's test — *can one person hold the
daemon's behaviour in his head?* — is being answered by **adding a ring per
incident**, and the incidents are of a family (the loop spawns, the loop
forgets, the loop cannot see its child). The alternative C3 points at is the one
the operator chose once already, in `925194b0`: **remove the mechanism rather
than harden it** (the park lane's three side effects went from "guarded" to
"gone"). §6.4 lists what was never decided about which of the two it is.

**OB-3 (MEDIUM-LOW). A whole second merge path exists whose only callers are
operator-typed commands.**

`internal/tools/task_merge.go` (751 L), `task_merge_executor.go` (511 L),
`task_merge_backlog.go` (370 L), `task_merge_backlog_terminal_guard.go` (619 L)
— 2,251 lines — implement a ledger-row-mediated merge: scan branches → mint a
`merge <branch> into master` row → an executor consumes the row → a terminal
guard stops an unmerged branch reaching a success terminal. The tick itself has
**no path into any of it** (`merger_tick.go` contains no `Backlog`/`backlog`
token; `TaskMergeExecutor` has **zero production callers** on master —
`grep -rn 'TaskMergeExecutor|MergeBacklogExecutor' internal cmd` outside tests →
empty). `grep -c` on `repo_sweep.go` for park → only comments; the ledger-driven
lane's own comment in `cli_tasks.go:2601` says *"the merge-backlog row pipeline
(branch-scan/audit/executor) is retired"* — yet all four files remain, kept
compiling, tested and dead. Under C5 this is the exact cost class the operator
named, and the predecessor's §6.3 G-6 ("one requirement, two
non-interchangeable artefacts") is its earlier shape.

### 6.3 Never decided

**ND-1 (MEDIUM-HIGH). Is the take-over's judgement a daemon branch or a prompt?**

R2 says the unattended part carries no judgement. The built system satisfies R2
by moving the judgement **out of Go and into a 858-line prompt** that a model
reads (`merger_takeover_prompt.md`; `merger_takeover.go:284` embeds it). That is
a defensible reading — the daemon's logic did not grow — but it relocates the
failure surface rather than removing it, and C3's words are about *the thing
that runs unattended*, not about which language it is written in. **Nothing in
the repo states which reading is intended.** The pending three-row epic
(`01a0b31f-*`) implies the operator's answer is "the Go tick, not the prompt",
but those rows are `pending`, so it is not yet a decision. `[INFERENCE]` weakly
toward "the prompt is the accepted home" — evidence: `merger_takeover_prompt_gate_test.go`
**asserts on the prompt's content** (the shipped text is treated as a
contract-bearing artifact, `:160`), and `merger_takeover_prompt.md` is
`//go:embed`-ed so the binary cannot ship without it. **Weakest link:** the gate
proves the text must exist and say certain things; it does not prove the operator
endorsed the *placement*.

**ND-2 (MEDIUM). What happens to a branch the take-over cannot judge?** The
lifecycle ruling (`devdoc/branch-lifecycle-ruling.md:40-70`) gives three
cannot-delete dispositions (①ownership undecidable → touch nothing, report;
②peer holds it → touch nothing, report; ③the diff is not filed yet → file then
delete). Dispositions ① and ② are *stable* — they re-report every tick by
design (`repo_sweep.go:343`). **No row states when a stable re-report becomes an
operator obligation** — i.e. nothing says whether a branch re-reported for N
ticks should raise anything or is by definition fine. Measured: **21 local heads
are non-ff into master on this host** right now, 26 heads in total, of which 5
are worktree-held (`git worktree list --porcelain`). So the population that
lives in that undecided state is not hypothetical.

**ND-3 (LOW-MEDIUM). The not-ff *report* is emitted by the spawned child, so a gated tick emits none — and nothing states whether the silence is intended.** `repoSweepNotFFOutcome`
(`:325-345`) says the tick changes nothing and the take-over owns the hand-over.
But a tick reaches the launcher only when the pre-spawn gate lets it
(`merger_tick.go:229-235`), and a skipped tick runs NO sweep, so the not-ff
report the operator reads was written by a still-live previous round
(`repo_sweep.go:343`). On a host whose loop is skipped every tick, that loop's
tick reports nothing. It reads as coherent — a live round is the reporting round
— but nothing states it. Flagged as undecided, not as a defect: I did not run the
merger (§8 limit 1), so I cannot say whether the window is reachable.

### 6.4 Where the requirement is VIOLATED (C1–C6 check)

| constraint | check | verdict |
|---|---|---|
| C1 | one binary, no plugin; the merge engine is stdlib + `git` | **OK** (`repo_sweep.go:44-51` imports stdlib only) |
| C2 | operator controls it: `--status/--stop/--restart` exist and are read-only or bounded | **OK** — the control faces are live (`cmd/agent/main.go:562-572`) — **but see ND-1/OB-2: the judgement the launcher delegates is not inspectable by the operator except through a log file** |
| C3 | the unattended part carries no branch of logic | **VIOLATED IN EFFECT** (OB-1: four sweeps + five survival legs on the tick; OB-2: four rings around one spawn) |
| C4 | the agent cleans up after itself; dirty is a violation | **VIOLATED** (US-1: the debt blocks the delivery it must not block) |
| C5 | one person can run it | **AT RISK** (OB-1/OB-3: 14,009 L of merge-domain production code and 2,251 L of dead ledger-merge path) |
| C6 | why over what | **OK for this module** — `repo_sweep.go:1-41` is one of the best `why` headers in the tree; §6's findings are all *what* problems the *why* predicts |

---

## 7. What this implies for tests

A requirement changes a test's verdict when the test currently certifies
behaviour the requirement forbids (or forbids behaviour it requires). Below:
the named tests, the direction, and the two behaviours that must **not** be
"fixed".

### 7.1 The dirty-worktree test family — the verdict the requirement changes

**The requirement (R10 + C4, quoted at `00-method-and-standards.md:415-427`):
on a dirty tree the harmless step runs and the destructive step defers. The
correct behaviour is that the idempotent push executes; only the
`worktree remove --force` and `branch -d` are withheld.**

| test | site | asserts today | verdict the requirement gives it |
|---|---|---|---|
| `TestFinishCleanupChainDirtyWorktree` | `internal/tools/task_finish_cleanup_test.go:224` (assertion `:202`: *"no steps may run on a dirty worktree"*) | dirty ⇒ **no push and no removal** | **CORRECT ONLY AFTER THE SPLIT.** Its set is over-broad by its own file's audit (`devdoc/assert-the-bug-audit-20260917.md:57`): the push must run. After US-1 lands, this test's message and its step-set must be narrowed to the destructive steps, or it will pin the defect again. |
| `TestServerRecycleDirtyWorktreeSkipped` | `:912` (assertion `:923`, same message) | dirty ⇒ nothing at all | **HOLDS** — this arm is the server/single-node one and **has no push step** (`task_finish_cleanup.go:898-900`); the audit calls the negation *true of THAT arm* and asks for a reword (`:58`). The defect is not here; the *wording* is. |
| `TestFinishCleanupClientDirtyStillDelivers` | `internal/tools/is_server_node_test.go:221` — **`t.Skip`ped**, body written, owner named in the skip reason | *nothing may run* — the test is skipped precisely because the truth is the opposite | **NEVER RAN; IT IS THE REQUIREMENT.** Un-skipping it after US-1 lands is the green proof — `devdoc/assert-the-bug-audit-20260917.md:362`: *"The same body is stored in the skipped test, so un-skipping it after 01a0aab8 is the green proof."* Its body asserts the split in the correct order: worktree survives, local branch survives, **peer holds the branch**. |
| the dry-run twin | `task_finish_cleanup_test.go:1938` (*"the refused plan must leave the worktree alone"*) | refusal ⇒ nothing at all | **SCOPE WIDENS.** `planChain()`/`runChain()` differ by one bool (`:1705`), so the plan must predict the new set — including the predicted push line, which US-1's fix must emit in plan mode (`:2197-2199` already renders it when the gate is passed). The audit carries this row as *"the same bundle"* (`:59`). |
| `TestReclaimOwnFinishedWorktreesRefusesADirtyWorktree` | `internal/tools/task_reclaim_self_test.go:180` | dirty ⇒ the directory is refused **by name** (`reclaimSkipDirtyReason`, `task_reclaim.go:104`) | **HOLDS, and it is the control row.** The reclaim arm has no push step — its whole act is the removal (`devdoc/assert-the-bug-audit-20260917.md:69`: *"Same guard, different set, different verdict — this is the audit's **control row**"*). A future "just delete the dirty guard" repair breaks this one — which is exactly the discrimination the requirement demands. |
| `TestSalvageDirtyDeadHolder_LeavesTheWorktreeUntouched` | `internal/tools/dirty_holder_salvage_test.go:150` | a dirty dead-holder worktree is filed for a human and left alone | **HOLDS** — salvage acts on the *ground*, not on the ref; nothing to push (`dirty_holder_salvage.go`). |

**The single sentence a test author needs:** *a dirty worktree changes what this
run may **destroy**, never what it may **deliver**.* The push is a peer-ref
write and is idempotent (git's *"Everything up-to-date"* exit-0 no-op,
`task_finish_cleanup.go:27-29`); the removal and the deletion are the run's own
ground and are irreversible.

### 7.2 Behaviours that are bugs (no test asserts them, so nothing goes red)

- **B1.** Site ①'s guard returns at `task_finish_cleanup.go:1148` before the
  target is resolved — every existing test is green while a finished branch
  stays on the client host. Measured live: the owning rows say `completed`.
- **B2.** `TaskFinishCleanup`'s skip is **not recorded on the row's event
  trail.** The pre-merge refusal is (`finishCleanupPreMergeReason`,
  `:2223/:2230`), the dirty skip is not — so the one condition that strands a
  delivery is the one that leaves no trace beyond a stderr line. (Rule 15's
  spirit, violated in the arm that needed it most.)
- **B3.** A misspelt peer alias and a single-node host produce the same skip
  reason (US-3) — an operator debugging a stranded branch is told "no sync_peers
  resolved", the recognisable head of a run that *does* have a peer
  (`finishCleanupNoServerTargetBase`, `:559-563`).

### 7.3 Behaviours that must NOT be "fixed"

- **The merge decision ignoring the ledger** — explicitly required
  (`repo_sweep.go:9-11`, R5). Wiring the sweep to task rows is the regression.
- **The tick never waiting on its child** — R2, pinned by
  `TestMergerTickSpawnsButNeverWaits` (`merger_tick_no_child_gate_test.go:356`)
  plus the per-file forbidden-token sets (`:94-110`). Re-introducing a wait, a
  registry or a handle is the regression the operator's replacement row
  (`01a0a995`) exists to prevent.
- **A not-ff branch keeping its ref, tag-free, row-free, worktree untouched**
  (`repo_sweep.go:325-345`) — the parked alternative is withdrawn
  (`devdoc/branch-lifecycle-ruling.md:1-40`).
- **The `-d` double lock** (`:2335-2345`) and the ordering ② → ③ → ④
  (`task_finish_cleanup.go:6-33`).
- **ABSTAIN rather than PASS when a gate cannot measure**
  (`task_finish_premerge.go`; `finishCleanupDeliveryPremise` in
  `task_finish_cleanup.go`: an abstain
  *"pushes and says so"*) — the discipline is the requirement; a gate that
  guesses is the bug.

---

## 8. Confidence and evidence limits

| claim | confidence | evidence / weakest link |
|---|---|---|
| R1–R10 as stated | **high** | each carries an operator row or a code `why` header, cited inline in §4 |
| R11 ("the delivery is one decision") | **low-medium** | `[INFERENCE]`; no operator sentence says the chain is atomic — §4-R11 names the weakest link |
| US-1 is live at `812f9a60` | **high** | three dirty-gate sites read at `:1145`, `:977`, `:2164`; `t.Skip` still on `is_server_node_test.go:210`; the correct repair `6e22d80b` is an ancestor of master but its delivery file is gone (`grep FinishCleanupDeliveryPass` → 0 hits; backed out by `085811f8`) |
| the owning rows are closed while the defect is live | **high** | `01a0aab8…` `completed` (ended 1789577411 = 2026-09-17 00:50:11 +0800); `01a0aaef…` `completed` |
| OB-1 (four sweeps + five legs on the tick) | **high** | `merger_tick.go:197-198`, `:332`; the criterion the operator dispatched is quoted verbatim from the payload of `01a0b31f-fc1f-7f28-86b3-8e8a2c6ca1cd` |
| OB-2 is over-build rather than necessity | **medium** | each ring cites its own incident; the *judgement* that a ring per incident is over-build is mine, made against C3 — visible, and reversible by the operator disagreeing |
| OB-3 (2,251 L with no tick path in) | **high** | `TaskMergeExecutor` has zero production callers; `merger_tick.go` has no backlog token; `cli_tasks.go:2601` says the pipeline is retired |
| 14,009 L / 3,874 L / 74 tests | **high** | recomputed this pass with `wc -l` over the file list in §6.2 and the M2 boundary in `00-module-map.md:117-127`; differs from the predecessors' 20,005 because their family was wider |

### Structural evidence limits

1. **I did not run the merger.** No tick was executed; every claim about
   behaviour is read from source at `812f9a60` and from the tests/docs that pin
   it. A live tick could differ (settings-dependent: `config.ResolveMergerRepos`).
2. **The ledger is shared and moving.** Row statuses were read once
   (2026-09-18 ~19:00 +0800); `01a0aab8`'s `completed` is a snapshot of another
   session's writeback, and its *reason* field is empty — so I can state the
   status, not the intent behind it.
3. **Two documents already exist for this module** and I read them at their own
   base of record. Where I disagree with them (`02-merge-and-branches.md` R3/R4/R10
   and §7.1's park tests) I say so; where they are still right (§3 slice, the
   R6 trigger framing) I reuse the citation rather than re-deriving it.
4. **The predecessor's §7.1 will go stale in a way a reader must know about**:
   it names `TestRepoSweepParkedBranchIsNotReParked` and
   `TestRepoSweepParkBranchRowIsIdempotent` as verdicts the requirement changes.
   Both files are gone from master (`ls internal/tools/*park*` → no match,
   removed by `925194b0`). This is not a defect in that document — it was
   correct at `17cec86b` — it is the reason this document exists.

---

## 9. What I could not determine

1. **Why this row was dispatched.** `01a0ab11` produced two documents and its row
   is `completed`; `01a0aac4` (the cancelled `REQUIREMENTS [1/11]`) left no
   reason either. Cheapest route: `ls ~/.gopi/sessions/01a0ab12-3115*` and read
   the `*.jsonl`, or one sentence from the operator. `[E1: rows 01a0ab11,
   01a0aac4]`
2. **Why `01a0aab8` and `01a0aaef` were closed while their fix was backed out.**
   The backout commit `085811f8` gives its own reason (conflicted shared files,
   6 merge-induced reds) — that explains the *backout*, not the *closure*. Was
   the intent "the fix is not landing, close the row", or was the closure an
   accident of a sweep? The difference decides whether US-1 needs a new row or
   a reopen. `01a0aab8.reason` is empty.
3. **Whether the operator intends the tick to keep the ledger/task-scoped legs**
   (OB-1). The pending `01a0b31f-*` epic implies not, but `pending` is not a
   decision; `merger_tick.go:183-201` defends them with measured E2E evidence.
   Cheapest route: the `[1/3]` row's own deliverable (a duty-boundary devdoc).
4. **The true cost of `merger_takeover_progress.go` (721 L) and
   `merger_restart.go` (707 L)** — I measured their size and read their
   headers, but did not trace how often each is exercised on this host
   (`gopi merger --status` output over several days would answer it).
5. **Whether the 21 non-ff local heads are waiting work or abandoned work.**
   Measured as a count and a list; classifying them needs the ledger×git join
   the dead-holder ladder performs (`branch_attribution.go`), which I did not
   run — and running it is an action on someone else's branches (Rule 29).

---

## Verification

- Reviewed: this document read end-to-end against `00-method-and-standards.md`
  §4's 8-section shape — §1–§8 all present; §0 (constraints) and §9
  (could-not-determine) present.
- §6 carries both an under-serve finding (US-1, US-2, US-3) and an over-build
  finding (OB-1, OB-2, OB-3), plus never-decided (ND-1..ND-3).
- §7.1 names the dirty-worktree family by test name and site and states the
  correct behaviour: the harmless push runs, the destructive cleanup defers.
- §4: every R carries at least one citation token (commit SHA / GOPI.md rule /
  ledger row id / file:line); R11 is marked `[INFERENCE]` with its weakest link.
- Docs only: no code, no tests, no `make fmt`; `go build` / `go test` untouched.
