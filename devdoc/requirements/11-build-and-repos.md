<!-- doc-class: normative -->
# Requirements 11 — Build & Repo Declaration

> **Module B11/12** of the GoPi reverse-requirements series. This is not a
> design document and not a spec written from imagination: it reconstructs the
> **user need** behind an existing surface by reading the built system, then
> measures the gap between that need and the code.
>
> Every claim is cited (ledger task id / `file:line` / commit SHA / GOPI.md
> rule) or prefixed `[opinion]`. Every inference is marked `[inferred]` with its
> weakest link named. Nothing here was written into code: this branch changes
> exactly one file.

## Front matter

**Boundary.**
The promise this module carries is one sentence: *the binary I am running
follows the trunk.* Three user-visible things make it true or false — a repo
that is a task field says how it is built; after work lands the machine runs
the new code; and the binary can name its own revision well enough that
"is it the new one?" is answerable without trusting anybody's memory.

**In scope.**

- `internal/version` — the single source of truth for how a gopi binary NAMES
  itself, including the `-dirty` hazard.
- The settings `repos` table as a per-repo build CONTRACT
  (`build`, `build_timeout_sec`), including the deliberately EMPTY entry.
- The post-merge rebuild step in the merge close loop, and its durable mark.
- The PRE-merge build gate — a different mechanism from the post-merge
  rebuild; the two are never conflated here.
- The `build-failed` notify path (the event is this module's requirement even
  though the transport belongs to B10).
- The script-refresh step that rides the same close loop.

**Out of scope.**

- The merge decision itself (branch selection, ff-or-park, conflict routing) —
  module B2. This document touches it only where it is the rebuild's trigger.
- The notify TRANSPORT (delivery, subscribers, transport failures) — B10.
- The `repos` table's OTHER key (`worktree_root`) — it is the worktree
  policy's business, not the build's. It appears here only as a measured
  interaction (§5, §6 N2).
- The scratch/temp-root placement policy — Rule 22's own module. Cited, judged
  only where a build artifact is its subject.

**Boundary why.**
Every other module in this series can be located in the tree; this one cannot.
The need "the machine runs the code that just landed" has **no directory**: it
is spread across `internal/version` (a name), `settings.json` (a contract),
the merger's close loop (a trigger) and the CLI (an entry point). No package
boundary names it and no type owns it. That is precisely why a reverse
requirement document is worth writing here and why the front matter has to say
so: the reader who goes looking for the module in the tree will not find it,
and would otherwise conclude the need is nobody's.

Overlap note: the sibling series task `01a0ab12-7dbe-7a19-86cf-33d7524c49c5`
(series module 14/15) is PENDING and asks for a document of the same boundary
(`devdoc/requirements/build-and-repos.md`). Its acceptance text is read and
judged here rather than redone; where it asks for a judgement this document
does not make, §8 says so.

---

## 1. What this is for

*(product-owner language — zero code identifiers on purpose)*

When work lands, the thing I run must be the thing that was just built. If it
is not, I do not merely have an old program: I have a **liar**. It accepts my
instructions and answers from a past the repo has already left, and it is at
its most convincing exactly when it is most wrong — an option it does not know, a
path it will not take, a failure mode the new code fixed. Every hour of that
is an hour I spend reasoning about code that is not running.

So the need is not "rebuild after merging". The need is:

1. **After work lands, the machine catches up by itself.** Nobody should have
   to remember to rebuild; the close of a merge round is where the machine's
   world is brought level with the repo's.
2. **A project says how it is built, and the machine does not guess.** My work
   is a repo, not a language. The project I am working on declares its build
   the same way it declares anything else about itself, and a project that
   declares nothing is left alone — merged, not built, and not complained
   about.
3. **A build that cannot be done is announced, not papered over — and it never
   undoes the merge.** The merge is history and history is fine. What is not
   fine is discovering hours later, from a strange failure, that the binary
   never followed.
4. **The binary can name itself.** "Which revision are you?" must have an
   answer the machine can check, so catching up is verifiable rather than
   asserted.
5. **The same promise holds for any repo I make a task field** — not only for
   the one this tool is written in.

What this buys is not speed; it is **the right to trust my own tools**. It
costs a build per merge round, and that cost is paid deliberately.

## 2. Who is served

Four actors, four distinct needs. They are not interchangeable, and two of
them are served only indirectly.

**The operator (the operator).** *Need:* to never lose an evening to a stale binary,
and to be told when catching up cannot be arranged. His own words for the
symptom are in the governing row (§4). He is the one who pays the trust cost
when the promise quietly fails, because he is the one who debugs the resulting
nonsense.

**The agent (a Bond, as the machine's user).** *Need:* to work against the code
the repo actually contains. This is the sharper, less visible need: an agent
that reads the source and then runs a stale binary gets a result that CONTRADICTS
what it just read, and has no way to tell which of the two is wrong. The agent
does not merely suffer from a stale binary — it is actively misled by one, and
it then writes reports, commits and reviews on top of the misdirection.

**The second node (a worker host that is not the integrating host).** *Need:*
for its installed copy to be the same code. Evidence: `01a0957a-a135-7eff-b6e0-fefe4f6c60df`
(verified) records HP running a stale checkout AND binary with two
contract-critical features absent from its binary. This actor needs the
promise *and* needs to know the promise is per-host — see §6 F3.

**A future maintainer.** *Need:* to add a landing face without silently
skipping the rebuild. Evidence: the code's own comment states the intent —
the manual and executor faces "reach the SAME builder rather than each writing
its own rebuild: this is what keeps a landing face from being added without a
close loop" (`cmd/agent/cli_tasks_rebuild.go:275-283`). [inferred] that this
actor was foreseen as a requirement rather than discovered as a refactor
concern; weakest link: the comment is a design statement, not a need
statement.

## 3. The workflow it sits in

Entry, exit and who triggers what.

**Entry — work is about to land.** Two doors, one rule:

- *The merger's own tick.* A repo is a configured task field; its branches are
  swept; ff-able ones land. Exit into this module: work landed (or was
  pruned), so the close loop runs once, after every sweep
  (`cmd/agent/cli_tasks.go:2612` — deferred by the pass; the durable marks are
  drained first).
- *An explicit landing face.* `gopi task merge` and the merge-execute face land
  on request, with no merger sweep. Exit into this module:
  `mergerCloseLoopAfterLanding` (`cmd/agent/cli_tasks_rebuild.go:275-289`) —
  the configured repo set UNION the repo actually landed in, de-duplicated so
  one checkout is never built twice.

**Pre-flight — a compile gate (a DIFFERENT mechanism).** Before an FF or park
is decided, the branch is compiled in isolation
(`internal/tools/repo_sweep.go:723`, `internal/tools/task_merge.go:280`). It
refuses the branch into the rework lane; it never blocks the merge at the git
level, and it is not the rebuild. Its default for a Go repo with no declared
contract is `go build ./...` (`internal/tools/merge_build_gate.go:90`).

**The step itself.** For each repo with a declared build: resolve the
contract; compare what the tree currently produces against the tip; if already
current, do nothing (`internal/tools/rebuild.go:129`); else run the command in
the repo with the declared timeout (`:136`); then assert the result NAMES the
tip (`:156`, `:207`).

**Exit — one of four outcomes**, and the user-visible difference between them
is the whole point:

| outcome | what the operator sees | is the merge affected? |
|---|---|---|
| already current | `ok: … already current` — a probe, no build | no |
| built and verified | `ok: … built in <d> (<version>)` | no |
| no declared build | `skip: no build contract …` — quiet, per repo | no |
| failed / mismatch | `FAILED: … exit=… err=…` **+ the `build-failed` alarm** | no |

The exit condition of this module is therefore never "the build succeeded". It
is **the machine's world is level with the repo again, or the operator has been
told why not** — which is why the failure row does not end the round.

**A neighbour on the same close loop.** The repo's installed operator scripts
are refreshed in the same step (`cmd/agent/cli_tasks_rebuild.go:395`), on the
same reasoning and with the same contract (never fatal; silent when nothing was
ever installed). It is the same need — an installed copy drifted from the repo
— for a second kind of artifact. [opinion] It earns its place in this module
rather than a module of its own because the drift mechanism is identical and
splitting it would reproduce the trigger logic.

## 4. Why it exists

The why-test: *if this were removed, what would the user notice?* Each need
below is stated, tested, and cited.

**The governing evidence — the operator's own words.** Ledger
`t-1788383728` (2026-09-02, `source=human`, status completed), verbatim:

> "gopi server must add one more task: when it successfully merges one branch
> or a list of branches, at the end it should rebuild gopi. This would be a
> general process — say our task is not only aimed at the gopi project; in
> future other git repos are a field of task, then the building is for that
> project, which might be a little risky to require all projects having a
> default make to build. But this is worth and necessary — this is the logic."

The same row states the gap it answers, verbatim:

> "the merge round … merges branches into master and cleans up … but NEVER
> rebuilds bin/gopi. Result: master advances but the deployed binary is stale
> (the recurring stale-bin incidents — 2026-08-31 through 09-02, dozens of
> 'unknown flag' / 'falls through to agent loop' failures traced to a bin built
> hours before the merged commit). The close loop is incomplete: merge without
> rebuild = the machine runs the OLD code against NEW master."

**N1 — after work lands, the machine catches up by itself.**
*Why-test:* remove it and the operator is again the scheduler for his own
toolchain, and the symptom returns verbatim. *Evidence:* `t-1788383728` above;
its settled form **PROJECT.md Rule 12** (`GOPI.md:311`), which fixes the step
order — "Merge → cleanup → **REBUILD** is the last step".

**N2 — a project declares how it is built; undeclared means untouched.**
*Why-test:* remove the declaration and the machine must guess — a `make`, an
inference from file extensions, a language detector — and guessing wrong is a
false alarm or a false silence on a repo the operator did not ask about.
*Evidence:* `t-1788383728` states the risk itself ("might be a little risky to
require all projects having a default make to build"); Rule 12 settles it
("must be **declared**, never discovered"; "**No entry = no build**: … skipped
silently after a merge — no error, no guess"). Code:
`internal/config/config.go:1609` (`ReposSettingsKey`), `:970`
(`ResolveRepoBuild`), `:984` (empty build ⇒ not found).

**N3 — an empty declaration is a legitimate, meaningful entry.**
*Why-test:* the operator needs a repo to be a task FIELD without being a build
target (a docs repo, a data repo); if an empty entry were an error, the sweep
list could not be the same list as the build contract. *Evidence:* Rule 12
("**The table is ALSO the merger sweep list** … An entry with an EMPTY build is
a valid PURE MERGE TARGET — swept for branches but not rebuilt … never a config
error"). This is not theory: the live settings file declares
`"/home/user/Downloads/games/Maps": {}` beside the GoPi entry.

**N4 — failure is announced, never fatal, never a rollback.**
*Why-test:* remove the alarm and the operator learns hours later from a strange
symptom — the exact failure mode the module exists to end. Remove the
"never fatal" half and a build failure destroys good history. *Evidence:* Rule
12 ("**Build failure ≠ rollback**: the merge is done and good … fires the
`build-failed` notify event … Never a crash, never a rollback"); code:
`internal/tools/rebuild.go:43` (`BuildFailedEvent = "build-failed"`), `:269`
(the notify), `cmd/agent/cli_tasks_rebuild.go:354-360` (the failure path stays
log + notify when there is nothing of its own to unwind).

**N5 — the binary can name itself, and the name is checkable.**
*Why-test:* without it, "did it catch up?" is unanswerable, so the promise in
N1 cannot be verified at all — it degrades into a hope. *Evidence:*
`t-1788383728` names the verification as part of the design ("Verify: after
rebuild, the binary's version string equals …"); code:
`internal/version/version.go:150` (the display), `:201` (describe from the
tree), `internal/tools/rebuild.go:156` / `:207` (the tip assertion).

**N6 — the same promise for any repo made a task field.**
*Why-test:* remove it and the operator can only ever automate the tool he
wrote, which contradicts the point of making arbitrary repos task fields.
*Evidence:* `t-1788383728` ("a general process — say our task is not only aimed
at the gopi project"); Rule 12's heading and closing line ("**To be a gopi task
repo you declare your build**").

**N7 — the build must not depend on a build system existing.**
*Why-test:* if the contract required `make`, every task repo would have to
adopt one, which is a tool the operator did not ask for and another thing to
maintain. *Evidence:* Rule 12 — GoPi declares its own build and "no Makefile
dependency for itself; OTHER repos may declare `make` or anything their build
needs"; the declared command is the trust boundary, and it is arbitrary text by
design.

**N8 — taking the build cost is a deliberate, bounded decision.**
*Why-test:* remove the bound and a hung build holds the tick; remove the
decision and the module either rebuilds everything always (paying for nothing)
or never (the stale binary). *Evidence:* the declared `build_timeout_sec: 600`
and the gate's own `300` (`internal/tools/merge_build_gate.go:100` area), both
uniformly bounded subprocesses; the "already current ⇒ no build"
short-circuit; the deliberate omission of a build for the pure merge target.
Rule 1/2's single-source discipline applies to both numbers — the rebuild and
the gate are documented as one place each (`merge_build_gate.go:100`,
"the rebuild never disagree about how long a build may take").

## 5. Acceptance in user terms

Observable statements, concrete enough that a test author can separate a bug
from allowed behaviour. Each is a sentence the operator could check himself.

**N1/N5 — catching up is checkable.**
- After a merge round that landed work, asking the binary which revision it is
  must answer with the new tip. **Bug if:** it answers the pre-merge tip, or
  answers nothing.
- If the tree already produces the tip, no build runs (`ok: … already
  current`). **Allowed:** the fast path. **Not allowed:** reporting "built" for
  a build that did not run.

**N2/N3/N6 — declaration semantics.**
- A repo with no entry: merged if it is swept, never built, **no error**. A
  repo with an empty entry: exactly the same, and it is not a config error.
  *Bug if:* an unbuildable-looking repo produces a repeating alarm.
- A repo with a declared build: it is built. *Bug if:* the build silently does
  not run.
- An unreadable settings file is NOT the silent case — it is reported
  (`cmd/agent/cli_tasks_rebuild.go:241`, "Unreadable settings are NOT a silent
  skip"). *Bug if:* a broken settings file produces no line at all.

**N4 — failure semantics.**
- A build failure fires the alarm AND leaves the merge in place.
- A version mismatch fires the alarm. *Bug if:* a build whose artifact does not
  name the tip is reported as success — the "quietly wrong" failure.
- A binary reporting `-dirty` **never** matches a tip
  (`internal/tools/rebuild.go:207-212`): a binary built from a modified tree
  does not represent the committed tip. *Bug if:* it is accepted.

**N8 — cost semantics.**
- A build that exceeds the declared bound is killed, with its children, and
  reported as a failure rather than holding the round.
- The trigger is a LANDING, not a tick: a tick that landed nothing does not
  rebuild.

**Boundaries the operator should NOT expect** (stated so "allowed" is
separable from "broken"):
- The rebuild does **not** roll back a merge. Only the round's own landing
  failure path may unwind, and only when the round itself landed the work
  (`cli_tasks_rebuild.go:354-380`).
- The rebuild does **not** install or publish the artifact anywhere beyond the
  repo's declared output location.
- The rebuild does **not** run tests. Compiling is not the same as passing.

## 6. Gap analysis

Four surfaces, plus the C1–C5 constraint check. §6 and §8 are load-bearing:
this is the only section that can contradict the code, and §8 is what keeps it
honest.

### Where the code serves the need

- The declaration contract is exactly as stated: `repos` is both the sweep list
  and the build contract, empty means merged-not-rebuilt, and the resolution
  distinguishes "no contract" from "unreadable settings" — the silent case and
  the loud case are separated on purpose (`internal/config/config.go:970-990`).
- All four landing faces reach ONE builder (pass close loop, manual merge,
  merge-execute, and the durable-mark drain), so a new face cannot skip the
  rebuild silently (`cmd/agent/cli_tasks_rebuild.go:275-300`).
- Failure never rolls back except where the round itself did the landing, and
  the alarm is the SAME event the vet gate reuses
  (`internal/tools/rebuild_vet_gate.go:90`, `VetFailedEvent = BuildFailedEvent`)
  — one alarm for "the code on disk does not do what the repo says".
- `-dirty` is refused as a match, everywhere the comparison happens. This is
  the identity rule done right: a name that cannot be trusted is not accepted
  because it looks close.
- The gate's abstention is honest: a timeout, a repo with no contract, a repo
  that is not a Go module root, a branch already contained in the target, and a
  module cache it cannot see all produce ABSTAIN with a reason rather than a
  refusal (`internal/tools/merge_build_gate.go:182` and the scratch/modcache
  files). An abstention lets the round stand — the right default.

### Where it falls short

**F1 — a skipped rebuild is a silent no-op, and the "durable mark" design makes
that a claim rather than an accident.** *Measured, not inferred.* The
dirty-tree guard is not a deferral: `serveRebuild` returns before calling the
builder at all, so no mark is consulted and no mark is refreshed
(`cmd/agent/cli_tasks_rebuild.go:319-330`); the durable mark is cleared only on
success (`:345`). Its log line is described in the code as "the deliberate
silent skip … One quiet log line for the operator's audit trail"
(`:308-310`, cf. `:385-387`) — but the comment above `rebuild_pending.go` records the opposite intent, that "a rebuild
that a `return` can step over" is the defect the mark exists to prevent
(`cmd/agent/cli_tasks_rebuild.go:208-223`). So a tree that is persistently
dirty (i.e. a task host, which is dirty by nature)
never rebuilds AND produces no alarm: the one silent hole left in a module
whose whole purpose is that silence is a bug.
*Live observation, same host, this session:* the deployed binary
(`/home/user/work/GoPi/bin/gopi`, resolved from `PATH`) reports
`17cec86bd32e-dirty` while master is 40 commits ahead at `bf4d1941…`; the
binary's mtime is 2026-09-17 00:28 and master's tip landed 04:55. Nothing was
paged. `[inferred]` that the cause was this skip path; weakest link: no
`[rebuild]` log line was captured (this host runs no resident merger — the only
`merger` processes on it belong to the test suite), so the *mechanism* is
inferred from the code while the *staleness* is measured. The compensating
mechanism (auto-revert of an unbuildable landing) does not cover it either: the
deployed binary's own build is not slow or broken, it is old.

**F2 — the verification half of the promise is unverifiable by construction in
any linked worktree.** *Measured.* Go's VCS stamping does not fire when the
build runs in a linked worktree (`.git` is a file there, so no `vcs.*` build
settings are recorded). I built the same clean revision twice — once in the
main clone (stamped `vcs.revision=bf4d1941…`) and once in this task's linked
worktree (no `vcs.*` at all) — and the worktree build answers plain `dev`,
which `versionMatches` refuses and which the rebuild would therefore report as
a version mismatch every pass. The project already knows the fact: it is
written into a test comment (`internal/tools/rebuild_tagged_version_test.go:19-21`,
"Go's vcs stamping does not fire in a linked worktree … measured") and its
protection is that the rebuild tests synthesise the stamp with a fixture script
instead of doing a real build. The finding is the production consequence: for
any repo whose build runs in a linked worktree — `worktree_root` is a supported
`repos.<path>` key (`internal/config/config.go:1034`, with its own settings key
in `internal/worktreeroots`) — the promise's verification step cannot succeed,
and the failure mode is the WORST one this document identifies: a correct build
reported as a mismatch, i.e. a false alarm every pass (the same shape as the
2026-09-13 epic the code's own comment records, where a report-parsing defect
"auto-revert[ed] every tagged merge",
`internal/version/version.go:355-362`). Today GoPi escapes it only because its
declared build names the main clone's output path. The suite is green over this
because the tests do not build for real.

**F3 — the promise is per-repo, and the second node's installed copy is not a
repo.** `01a0957a-a135-7eff-b6e0-fefe4f6c60df` (verified) records HP running a
stale checkout AND binary. The module rebuilds the artifact *inside the repo
that landed*; a peer host's installed copy is a different thing, and nothing in
this module's boundary covers it. `[inferred]` that the operator's "the machine
runs the new code" was meant per-machine; weakest link: `t-1788383728` says
"gopi server", which reads as the integrating host, and I found no operator row
declaring the peer's copy a requirement of THIS module.

**F4 — the durable-mark half of the close loop has no production writer since
the merger was simplified.** *Measured.* `MarkRebuildPending` — the function
that records "master advanced, the binary is behind" so a successor pass can
recover it — is called only from TESTS. Its sole production caller was the
post-merge verify child (`cmd/agent/merger_verify_child.go`), which was deleted
whole by `126fad74` ("refactor(merger): ff-only or park-and-file — no child, no
wait, no state"); `grep -rn MarkRebuildPending --include=*.go` then returns the
definition (`internal/tools/rebuild_pending.go:119`) and test call sites only.
The DRAIN is still armed on every pass (`cmd/agent/cli_tasks_rebuild.go:227-238`
walks `RebuildPendingRepos()`), so the drain reads a directory nothing writes.
`[inferred]` that this is a DELIBERATE casualty rather than an oversight: the
mark existed for an interruption window a blocking verify child created
(measured 56 minutes, `rebuild_pending.go:9-31`), and "no child, no wait, no
state" is exactly C3 — so deleting the writer follows the operator's own
judgement. What did NOT follow is the cleanup: the drain, the mark type, its
tests and a now-red gate test all remain as a monument to the removed design.
`[opinion]` This is the C3 rule applied to a REFACTOR: doing less in the daemon
is right, and leaving the deleted mechanism's scaffolding (with a red test
guarding the wrong file) is how a future reader concludes the mark is live.
*Weakest link:* I could not find the row that decided the deletion — `126fad74`
is a refactor commit, and whether the author noticed the mark's writer was in
the deleted file is not recorded.

### Where it over-builds

**O1 — the pre-merge gate carries the most judgement in the module.** Judged
against C3 below. It materialises a detached worktree of the branch tip,
redirects two Go caches, and classifies failures — it owns a signature for "the
build environment is unbuildable" (`mergeBuildGateUnresolvedModules`,
`merge_build_gate.go:773`). That classifier is judgement that crept back in,
and the safeguard is that its only output is an ABSTAIN. `[opinion]` The honest
reading is "acceptable, with a named risk": the gate earns its keep (a branch
that does not compile must not reach master — see the operator's own words in
`01a0989c-bd1d-7bac-9bc6-e44bdad67a12`), but every future addition to the
abstain list must be an abstain, never a refuse.

**O2 — two different defaults for "how long may a build take".** The rebuild
contract's default is `600`; the gate's Go default is `300`. Both are named
constants, so Rule 1 is satisfied — but the same physical action has two
answers depending on which mechanism is asking. `[opinion]` This is a
deliberate split (a pre-merge probe may be stricter than a landing build) that
is documented but not justified in one place.

### Where no requirement was ever decided

**U1 — what "the binary the operator runs" means.** The module verifies an
artifact at a fixed location inside the repo and probes it. Which binary is on
the operator's `PATH`, whether it is the same file, and whether it should be —
nothing in the ledger or in Rule 12 decides this. [inferred] that it is assumed
to be a symlink or a copy the operator arranges; weakest link: on this host the
`PATH` binary is a symlink to the repo's output, and I found no requirement row
saying that arrangement is the contract rather than a habit. This is the
unstated premise that F1's live staleness rests on.

**U2 — what a build is allowed to touch.** The contract is arbitrary text.
Rule 22 constrains the temp root; the gate redirects its caches; but there is
no stated requirement about network access, registry writes or wall-clock
externality during a landing build. The gate has handling for a build that
would FETCH a module (it announces the unbuildable environment instead), which
is a shield, not a requirement.

**U3 — what happens to the artifact.** No requirement says whether the newly
built binary should replace a running one, be versioned, be kept, or be
garbage-collected. The output location is a convention inside the declared
command, and the `-dirty` marker is the only trace of the tree it came from.

### The C1–C5 constraint check

Every requirement must violate none of the operator's five stated constraints;
a conflict is a finding.

- **C1 — one static binary, zero dependencies, no plugins.** **SERVED, doubly.**
  The declared command itself is `CGO_ENABLED=0` (`settings.json`, and the
  oldest ancestor of this need is the founding constraint 5,
  `devdoc/roadmap.md:64`: "CGO_ENABLED=0：始终编译为静态二进制文件"). Rule 12's
  refusal to require a build system is that constraint stated as policy. The
  module's own imports add nothing new — no plugin mechanism, no build-system
  integration, and a build is a bounded subprocess, not an extension point.
  *Conflict filed as U2:* the zero-dep constraint is on the ARTIFACT, while the
  declared command is arbitrary text; the requirement that a landing build must
  not depend on the network was never decided.
- **C2 — the operator must control the whole thing; anything he cannot observe
  or reverse is a defect.** **FALLS SHORT — this is the module's C2 finding.**
  The rebuild's outcome is observable only as a log line at the moment it runs,
  and the silent-skip path (F1) is by construction neither observed nor
  reversed. `[opinion]` The gap is narrow and fixable — the status surface
  exists and says nothing about builds — but it is exactly the shape C2 calls a
  defect. Note that `t-1788383728` is BOTH this module's governing requirement
  AND C2's citation; that doubling is a finding in itself, because it means the
  same sentence is being asked to justify a feature and to constrain it, so a
  misreading of it cannot be caught by either role. (The brief's paraphrase "one
  place he understands" is the agent's wording, never the operator's — the row
  itself is quoted in §4.)
- **C3 — a daemon must be simple; every branch of logic multiplies its failure
  surface.** **SERVED, with the two surviving branches judged as asked.**
  The post-merge rebuild is mechanical: resolve → compare → run → compare. It
  holds no state, makes no judgement, and its only branch (dirty? already
  current?) is the one D2 requires. The PRE-merge gate is the heavier branch
  (O1): it owns a workspace, two caches and a failure classifier — and the
  reason it is nonetheless on the right side of C3 is that its only refusal is
  the one D2 demands and everything else ABSTAINS. Rule 12's own reasoning is
  C3 applied to failure handling and is quoted in §4 N4: "build failure ≠
  rollback … never a crash, never a rollback" — the operator's answer to "what
  does a daemon do when a step fails?" is "keep going and say so". *The C3
  judgement this document records:* the rebuild PASSES; the gate PASSES ONLY
  BECAUSE of its abstain list, and that list is a load-bearing invariant a
  future change can quietly weaken.
- **C4 — the agent must clean up after itself; dirty is a violation, not a
  condition to tolerate.** **SERVED, and the module is unusually disciplined
  about it.** It removes what it materialises: the gate's scratch worktree and
  registrations are reaped (`merge_build_gate.go:430-480`), and its caches live
  outside the collected scratch tree on purpose. It never writes to the tree it
  inspects: the artifact probe runs the binary to ask its NAME
  (`internal/tools/rebuild.go:237-244`) and the dirty predicate reads status,
  never modifies it. The `-dirty` marker is treated as the trace of an unclean
  tree and is refused rather than explained away. The blast-radius half of this
  constraint is CLOSED elsewhere (`internal/tools/path_policy.go`) and is not
  re-raised here. *Where it touches C4 as a finding:* the guard's scope
  question. The rebuild and `internal/version` both judge dirt with
  `--untracked-files=no` (`internal/tools/task_adoption.go:158`,
  `internal/version/version.go:211`). *Measured:* that predicate is the right
  one for "should I defer?", but it is NOT the right one for "can an untracked
  file change my artifact?" — adding an untracked `.go` source inside the built
  package flips the toolchain's `vcs.modified` from `true` to `false` under the
  rebuild's environment, and `--untracked-files=no` also overrides an
  operator's own `status.showUntrackedFiles` setting for that build
  (`internal/tools/rebuild.go:367-400`). Two predicates, one name, two
  questions.
- **C5 — small team, limited resources; cost is a requirement.** **SERVED, and
  priced deliberately.** Declared `build_timeout_sec: 600` and the gate's `300`
  bound the cost; "already current" skips it entirely; the PURE MERGE TARGET
  entry means a non-build repo costs nothing; the shared disk-backed caches
  replaced per-process tmpfs caches whose cost was measured in hundreds of MB
  per build and a 2026-09-15 disk exhaustion (the reasoning lives in
  `merge_build_gate_gocache.go`, and the placement criterion in Rule 22). The
  module's cost is proportional to LANDINGS, not to ticks. *A C5 observation
  that belongs with F1:* the dirty-skip is cheap in CPU and expensive in trust,
  which is the wrong way round — the constraint asks for the cheapest thing one
  person can RUN and UNDERSTAND, and an unrebuilt binary costs understanding.

### Findings that are new to the operator

Ranked by consequence:

1. **F1** — the dirty-tree skip is an unconditional no-op, so a persistently
   dirty task host never rebuilds and is never told; the live binary on this
   host is 40 commits stale with no alarm.
2. **F2** — the promise's verification step cannot succeed in a linked
   worktree, and the suite is green over that because the tests synthesise the
   stamp instead of building.
3. **F4** — the durable-mark drain reads a directory nothing has written since
   the verify child was deleted, and a red test guarding the deleted file is
   still in the suite.
4. **C4/N3** — `--untracked-files=no` answers two different questions with one
   predicate, and an untracked source file measurably changes the artifact.
5. **C2/U1** — the module has no observable statement of "which binary is the
   one that matters", which is the premise its only live failure rests on.

## 7. Implications for tests

No tests were written or changed by this document. What follows is which
EXISTING tests would change verdict if the requirements above were taken
literally — the direction each would move, and why.

**F1 first, because it is the one that inverts a verdict.**

- `internal/tools/rebuild_tracked_guard_test.go` (the tracked-only guard, and
  the `buildVCSStatusEnv` pin at `:82-97`) — its subject is right and stays
  green. What is missing is its COMPANION: a test asserting that a SKIPPED
  rebuild is not silently forgotten. Today the only "skip" assertions are that
  no build ran; if the requirement is "skip and remember", the suite has no
  assertion for the remember half. **Direction: add (not fix) — the existing
  pins are correct and incomplete.**
- `cmd/agent/cli_tasks_rebuild_pending_test.go` — the durable-mark drain. Its
  verdict changes if the skip path is made to defer: a dirty repo would have to
  leave a mark that a later clean pass drains, so a test that today passes with
  "no build attempted" would have to assert "a mark exists, and is drained on
  the next clean pass". **Direction: would need the new assertion or it becomes
  false-green.**
- `internal/tools/rebuild_pending_test.go` (`ClearRebuildPending` on the
  success path only) — the clear rule is what makes F1 permanent. **Direction:
  the clear must be paired with a documented "what discharges a mark when the
  build is refused", or this test pins the defect.** Note also that under F4
  this file tests a path whose writer no longer exists in production: it is
  green, and it is green about a code path no production caller reaches.

**F2 — a test that passes for the wrong reason.**

- `internal/tools/rebuild_tagged_version_test.go` — it documents the linked-
  worktree fact in a comment and then sidesteps it with a synthetic display
  script (`:19-21`). **Direction: the comment is the requirement and the
  fixture is the loophole.** A test that builds for real in a linked worktree,
  or an explicit statement that builds only ever happen in a main clone, would
  change this from "documented obstacle" to a pinned invariant.
- `internal/tools/rebuild_versionprobe_test.go` and
  `internal/version/version_report_probe_test.go` — the report/probe shapes.
  They are green and stay green; they do not cover the "no vcs stamp at all"
  answer, which is the shape a worktree build produces. **Direction: one
  fixture (a `dev`-answering artifact) would cover F2's production case.**

**N2/N3 — the declaration semantics, and one Rule-15 judgement call.**

- `internal/config/config_repos_test.go` — the entry/empty/missing matrix.
  Green, and it already pins that an empty build is not an error. **Direction:
  keep; it is the requirement's executable form.**
- `cmd/agent/cli_tasks_rebuild_gate_test.go` — **RED on master, for an
  unrelated reason, and honesty requires saying so.** It reads a filename for a
  post-merge verify child that was deleted as part of the sharp simplification
  of the merger (`cmd/agent/cli_tasks_rebuild_gate_test.go:38` reads
  `merger_verify_child.go`; the file no longer exists — deleted whole by
  `126fad74`, and a fix is sitting unmerged on a sibling host's branch
  `fix/cmd-agent-red-reanchor`, commit `63e86d64`, which re-anchors this very
  test). This is not a defect of the build module; it is a REQUIREMENT VERDICT
  CHANGE caused by B2's redesign — i.e. exactly what §7 exists to name. **Direction:
  the test is right to be red; the verdict it pins moved, and the fix belongs to
  the module that moved it.** It is listed here because a reader of this document
  who runs the module's tests will hit it, and must not conclude the build
  requirements are broken.

**The pre-merge gate's abstain list (O1) — the invariant with no pin.**

- `internal/tools/merge_build_gate_test.go`, `_contained_test.go`,
  `_gocache_test.go`, `_modcache_test.go`, `_unresolved_test.go`,
  `_cleanup_test.go` — collectively they pin ABSTAIN-not-FAIL for a timeout, a
  missing contract, a contained branch, an unreadable cache. **Direction: green
  and correct.** The gap is that nothing pins the CLASS ("every unrecognised
  condition abstains") — only the instances. Under C3 that class IS the
  requirement.
- `internal/tools/merge_build_gate_gocache_test.go`
  (`TestMergeBuildGate_GoCacheLeavesNoScratchCache`) — **RED on master
  `bf4d1941`, measured on a pristine tree as well as this branch** (the gate's
  cache resolves under the scratch tree, so the test's own placement criterion
  `internal/tools/merge_build_gate_gocache_test.go:160` fails). Unrelated to
  this document, recorded so the reader does not attribute it to the build
  requirements; it is a C4/Rule-22 placement defect in a test-support fixture.

**The close-loop wiring.**

- `cmd/agent/cli_tasks_rebuild_pass_test.go`
  (`TestMergerPassCloseLoopWiredAfterSweep`) — green; it pins that the pass
  reaches the close loop after the sweep. **Direction: keep.** Note the red
  described above sits on `TestCloseLoopWiringIsComplete`, in the same package
  and the same file family, and is the same stale-anchor defect.
- `internal/tools/rebuild_test.go` (`TestRebuildE2EMergeRoundBinFollowsMaster`
  and the no-contract / mismatch / failure cases) — these ARE the acceptance
  test of §5 in code, and they are green. **Direction: keep; they are the
  document's executable form.** Their one limitation is the same as F2's: they
  synthesise the version report instead of producing a really-stamped binary.

## 8. Confidence and limits

**Per-claim confidence.** High unless stated otherwise.

| claim | confidence | basis |
|---|---|---|
| The governing need and its settled form | **high** | operator verbatim `t-1788383728` + Rule 12, quoted in §4 |
| Declaration semantics (N2/N3) | **high** | both the rule and the code, plus the live settings file |
| Failure semantics (N4) | **high** | Rule 12 + code paths read end to end |
| Naming/verification exists (N5) | **high** | code read; the live binary answers the shape |
| F1 (skip is a no-op) | **high on mechanism, medium on attribution** | mechanism read in code; the stale binary is measured; the CAUSE is [inferred] because no rebuild log line was captured on a host with no resident merger |
| F2 (worktree cannot be stamped) | **high** | measured twice on this host; the repo records the same fact independently |
| C4/N3 (untracked source flips the stamp) | **high** | measured: `vcs.modified` true → false, reproduced |
| U1/U2/U3 | **medium** | absence of evidence — these are gaps I could not find a decision for, not decided gaps |
| Actor list (§2) | **medium** | the agent-as-user and the second node are [inferred] from consequences and one verified row, not from a need statement |

**Inferences, each with its weakest link named.**

1. `[inferred]` The application/toolchain "catching up" is worth a build per
   merge round rather than per tick. *Weakest link:* no operator row prices
   this; the bound is read from the code's own choices.
2. `[inferred]` The pre-merge gate and the post-merge rebuild are one
   requirement seen twice. *Weakest link:* they were built separately, at
   different times, with different defaults (O2); the operator's rows address
   the post-merge half directly and the gate only through the "a branch that
   does not compile must not land" principle.
3. `[inferred]` The agent (as opposed to the operator) is a real user of this
   module. *Weakest link:* no row says "an agent was misled by a stale binary";
   the argument is mechanistic.
4. `[inferred]` F1's live staleness came from the dirty-skip. *Weakest link:*
   the skip is one of several reasons a rebuild may not have run; I captured no
   evidence line. The staleness itself is measured and is not in doubt.
5. `[inferred]` The script-refresh neighbour is the same requirement for a
   second artifact kind. *Weakest link:* its own comment justifies it; no
   operator row does.

**What I could not determine.**

- Which binary is "the" binary (§6 U1). I measured that the `PATH` binary on
  this host is a symlink into the main repo's output and that it is 40 commits
  stale — but I found no requirement that says the symlink arrangement IS the
  contract. Without that, "the machine runs the new code" has an unstated
  premise, and F1 rests on it.
- Whether the operator was ever TOLD about the dirty-skip's silence. Rule 12's
  text does not mention a dirty guard at all; the guard is a later addition,
  and whether its silence was accepted deliberately or overlooked is not
  recorded anywhere I could find. [opinion] The evidence points to
  overlooked: the guard's own comment calls its skip "deliberate", yet the
  durable-mark design immediately beside it was written to prevent exactly
  "a rebuild that a `return` can step over".
- Whether the sibling module `01a0ab12-7dbe-7a19-86cf-33d7524c49c5` reached
  the same findings. It is PENDING; I read its briefing and judged rather than
  redid it, and its acceptance list and this document's overlap on every
  point except F2, F3 and the C4/N3 predicate split — so those three are the
  places where a duplicate run would add information rather than noise.
- Whether a repo whose build legitimately lives in a linked worktree exists
  today outside GoPi. `worktree_root` is supported and unset for GoPi, so the
  question is open and F2 is a latent defect rather than an active one.
- What `internal/tools/rebuild_vet_gate.go`'s "post-merge vet" is FOR, in user
  terms. It is in this module's blast radius (it reuses `BuildFailedEvent` and
  is named after a merge), it abstains on a non-Go repo, and I found no
  operator row that states the need it serves. I left it out of the needs list
  rather than invent one.

**Verification of this document.** `git diff --name-only master...HEAD` on this
branch lists exactly one file (this one); `CGO_ENABLED=0 go build ./...` exits
0 on the branch. Nothing in the tree was modified to produce any measurement
quoted above: the Go-VCS experiments ran in a throwaway clone under the
workspace temp root and its worktree, and every probe left no tracked change
behind (`git status --porcelain` empty at the end of each).
