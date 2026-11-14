<!-- doc-class: historical -->
# Requirements — Build & Repo Declaration (`build-and-repos.md`)

> **Module B11/14** of the GoPi reverse-requirements series. This file is the
> deliverable named by task `01a0ab12-7dbe-7a19-86cf-33d7524c49c5`
> (series module 14/15), whose acceptance text asks for the user need behind
> **GOPI.md Rule 12** in an 8-section shape plus a C1–C5 constraint check.
>
> **This run is an AUDIT-AND-CLOSE-GAPS pass, not a from-scratch write.**
> A committed sibling document **`devdoc/requirements/11-build-and-repos.md`**
> (727 lines, commit `073eee99`, authored by the *other* series lane) already
> occupies this slot: it carries the same boundary, the same 8-section shape,
> the same C1–C5 check and the same governing evidence. Re-deriving it would
> produce a second, contradicting account of one requirement — which is the
> exact failure mode the operator's own series method warns about
> (`devdoc/requirements/00-method-and-standards.md`). So this document:
>
> 1. **states an audit verdict** on `11-build-and-repos.md` against this
>    briefing's acceptance list (what it satisfies, verified);
> 2. **closes the gaps this briefing names that `11` does not cover** —
>    Rule 15 is never applied in `11` (measured: the string `Rule 15` occurs
>    exactly once in that file, in a §7 *heading*, with no judgement under it),
>    and the literal `cross-repo` occurs **0 times**;
> 3. **stays consistent with `11`** — §9 below is a point-by-point
>    consistency table, and nothing here reverses a verdict `11` reached.
>
> Every claim is cited (`GOPI.md:line`, `file:line`, commit SHA, ledger id) or
> prefixed `[opinion]` / `[inferred]` with its weakest link named. **No code
> was changed and no test was written on this branch.** Every measurement below
> was read from the tree or produced by a command whose output is quoted in
> `## Evidence`.

---

## Front matter

**The need, in one sentence.** *The binary I am running follows the trunk.*

**Boundary — and what this run adds to it.** `11` §Front matter establishes the
boundary (a build contract in the `repos` table, the post-merge rebuild, the
pre-merge gate, `build-failed`, self-naming). This run accepts that boundary
and adds the two things the briefing requires and `11` leaves open:

- **the SILENCE of the undeclared-no-build path, judged against Rule 15** —
  the briefing's explicit ask, and the one place where `11` and the code
  disagree in a way `11` does not record (`11` §7's heading claims a "Rule-15
  judgement call" that its body never makes);
- **cross-repo task fields** — that by the time a build is a *contract for
  repos you did not write*, the hard part moves from "can we run a command" to
  "can one repo's build damage another's", and the answer is materialised in a
  cache-isolation invariant (`TestMergeBuildGate_GoCacheSeparatesRepos`).

**Relationship to `11` in one line.** `11` is the *requirement reconstruction*;
this file is its *audit*, its *Rule 15 finding*, and its *cross-repo half* — a
companion, not a replacement. A reader who wants the need itself should read
`11`; a reader who wants the silence and the cross-repo judgement should read
§5–§7 here.

Overlap disclosure, stated rather than hidden: the two documents cover the same
module, and this is deliberate — Rule 26 of `GOPI.md` ("an incomplete fix is
repaired by dispatching again — never by waiting") permits, and the briefing
requires, the duplicate dispatch. The overlap is closed by *reading* `11` and
judging it, not by redoing it.

---

## 1. What this is for

*(product-owner language — no code identifiers)*

When work lands, the thing I run must be the thing that was just built. `11` §1
states this need completely and I do not restate it; what follows is the part
of the need **this run is accountable for**, in the same register.

**The need has a silence in it, and the silence is load-bearing.** Point 2 of
`11` §1 is: *"a project that declares nothing is left alone — merged, not
built, and not complained about."* That is a **requirement for silence**, and it
is a good one: without it, every docs repo I sweep would page me nightly about
a build it was never going to have. But a requirement for silence is dangerous
in exactly the way Rule 15 (`GOPI.md:453`) was written about — a program that
says nothing is indistinguishable from a program that is not running. So the
need is not "be quiet"; the need is:

> **Be quiet about the repos I told you to leave alone, and loud about
> everything else — and never let me confuse the two.**

That distinction is the whole of §6 F1 here. The rule my work needs is not
Rule 12's "no entry = no build"; it is Rule 12 **as read through Rule 15**.

**The need gets harder the moment my work is not a repo.** While the only task
field is the tool I wrote, "declare your build" is bookkeeping. When any repo
becomes a task field, two of my repos are built by the same machine, in the
same close loop, on the same tick — and then the need is *scale-free isolation*:
one project's build must not be able to reach into another's caches, another's
tree, or another's timeout budget. `11` §4 N6 states the *existence* of this
need ("the same promise for any repo I make a task field"); §6 F3 here states
what it mechanically costs and whether the cost is paid.

**What I am buying.** Not speed. `11` §1 ends with the right phrase — *the right
to trust my own tools* — and I add only this: the trust survives a silence
**only because the silence is bounded and declared**. An unbounded silence is
not a quieter version of the same tool; it is a different tool, and it is the
one that lies.

## 2. Who is served

`11` §2 names four actors (operator / agent / second node / future maintainer)
and I adopt that list unchanged — I found no actor it missed. What this run
adds is **which actor each of my three findings actually serves**, because the
briefing's Rule 15 question ("does the silent no-build path conflict with Rule
15?") is answered differently for each:

| actor (from `11` §2) | the need they carry | which finding here serves them |
|---|---|---|
| **The operator (nick)** | never lose an evening to a stale binary; be told when catching up cannot be arranged | **F1-a** (pass-path silence), **F3** (cross-repo timeout) |
| **The agent (a Bond)** | work against the code the repo contains; a stale binary actively misleads a reader of source | **F1-a** indirect, **F2** (timeout, not version) |
| **The second node** | its installed copy is the same code (`01a0957a…`, verified) | unaffected here; `11` F3 owns it |
| **A future maintainer** | add a landing face without skipping the close loop (`cmd/agent/cli_tasks_rebuild.go:275-283`) | **F1-b** (comment/code mismatch on the gate path) |

**One actor `11` treats as a USER that this run treats as a CONTROL.** `11` §2
routes the second node through "the promise is per-host". I agree, and note for
the record that the *cross-repo* half of the same concern is not an actor's need
at all — it is the operator's **own** need not to have his repos interfere, and
it is served by a mechanism (cache isolation, `TestMergeBuildGate_GoCacheSeparatesRepos`)
rather than by a promise. [opinion] Splitting "per-host" (a promise to a node)
from "per-repo isolation" (a property of the machine) is what makes §6 F2/F3
below decidable rather than rhetorical.

## 3. The workflow it sits in

`11` §3 describes the entry, the pre-flight gate, the step and the four exits,
and I adopt that flow verbatim — including its sharpest correction, that the
**pre-merge gate is a different mechanism from the post-merge rebuild** and the
two are never conflated. This run does not re-describe the flow; it annotates
**the four exits with what each one OBSERVES**, because that is the axis Rule 15
turns on.

| exit (`11` §3) | what the operator sees | what is RECORDED | Rule 15 verdict |
|---|---|---|---|
| already current | `ok: … already current` | a log line | served |
| built and verified | `ok: … built in <d> (<version>)` | a log line | served |
| no declared build — **landing face** | `[merger] [rebuild] skip: no build contract for %s …` (`cmd/agent/cli_tasks_rebuild.go:317`) | stderr, per repo | **served** |
| no declared build — **merger pass** | **nothing** (`cmd/agent/cli_tasks_rebuild.go:250-252`) | **nothing** | **CONFLICTS — see §6 F1-a** |
| failed / mismatch | `FAILED: … exit=… err=…` **+ `build-failed`** (`internal/tools/rebuild.go:43`) | log + notify event | served |

**The asymmetry, named precisely.** There are **two** production call sites that
decide "this repo has no build contract, do nothing":

- `serveRebuild` (`cmd/agent/cli_tasks_rebuild.go:311-318`) — the per-repo build
  half. On `!found` it prints *"the deliberate silent skip … One quiet log line
  for the operator's audit trail"* (`:306-307`) at `:317`. **It keeps its
  promise.**
- `mergerPassCloseLoop` (`cmd/agent/cli_tasks_rebuild.go:239-252`) — the pass's
  own pre-filter over the configured repo list. On `!found` it **`continue`s with
  no output at all**, and the comment above it says so explicitly: *"no contract,
  nothing to rebuild, and no per-pass log line about it"* (`:247-249`).

So the module's documented behaviour ("one quiet log line") is true on one path
and false on the other, **and the path that is false is the one that runs on
every merger tick**. That is a Rule 15 conflict in the narrow, precise sense:
not "the program is silent", but *"the program documents a line it does not
always write, so an operator auditing for it sees a clean pass that may have
contained nothing."*

**Cross-repo enters here.** The pass pre-filter walks `mergerUniqueRepos(repos)`
— i.e. **every configured repo**, not one. That is where cross-repo cost and
cross-repo timeout semantics are decided (§6 F3), and it is why the pass path,
not the landing path, is the one that scales with my repo count.

## 4. Why it exists

`11` §4 reconstructs the why with the operator's verbatim row
(`t-1788383728`, 2026-09-02, `source=human`) and eight needs N1–N8. I adopt all
eight and add the **ninth**, which is the one this briefing sends me for.

**The governing evidence, re-cited and not re-derived.** `11` §4 §N2 quotes
Rule 12 (`GOPI.md:331`) settling the declaration question: *"must be declared,
never discovered"*, and — the sentence this whole document turns on —
*"**No entry = no build**: … skipped silently after a merge — no error, no
guess."* I read `GOPI.md:331`–`Rule 13` myself and it says exactly that.
**I do not re-quote it at length**; `11` §4 has the verbatim text and re-copying
it would be two sources for one sentence.

**N9 — a silence is only legitimate when it is BOUNDED and DECLARED.**
*Why-test:* remove this and the operator cannot tell "I declared nothing, so
nothing was built — correct" from "something went wrong before the build was
considered — and I was never told". Both print nothing; both look like a clean
tick. *Evidence:* the code itself states the bound on one path and loses it on
the other (§3 above). Rule 15 (`GOPI.md:453`) is the rule the operator already
wrote for exactly this shape — *"Errors must be handled or recorded — never
swallowed"* — and its 15b precedent is **the same failure geometry**:
*"a `>/dev/null` redirect swallowed every remote handoff error, so a failed
nohup handoff looked successful"* (`GOPI.md:487`-ish region, §15b). A skipped
rebuild that prints nothing is that precedent applied to a build.

**N9 is NOT in tension with Rule 12** — and the judgement the briefing asks for
is that this had to be *checked*, not assumed:

- Rule 12 demands silence **about the class "no contract declared"**: a
  docs-only repo must not alarm. That is a statement about a *declared,
  understood* condition.
- Rule 15 demands a record **about the class "an error was swallowed"**. Rule 12's
  own text separates these two classes in the same paragraph: an **unreadable
  settings file** is *not* the silent case, and the code reports it
  (`cmd/agent/cli_tasks_rebuild.go:241-245`, comment: *"Unreadable settings are
  NOT a silent skip … (Rule 15: no silent failure)"*).

So: **Rule 12 and Rule 15 are consistent, and the code already knows it** —
Rule 15 is even *named in a comment* on the error branch of the very same
function. The conflict is not rule-against-rule. It is **rule-against-code on a
third path**: the merger pass pre-filter (`:250-252`) is neither "error" (loud)
nor "audit line" (quiet-but-present); it is the hole between them. That is the
finding of this document, and §6 F1-a states it as a defect rather than as a
design choice, because the code contradicts itself about it: the same file
promises the line at `:306-307` and omits it at `:247-249`.

## 5. Acceptance in user terms

`11` §5 gives the observable acceptance sentences for N1–N8 and I adopt them
unchanged. Below are the sentences **this run adds**, in the same register —
each one something the operator could check himself without reading code.

**N9 — the silence is bounded (new).**
- A repo I never declared is merged and **not** built, and I am **not** alarmed.
  *Bug if:* it produces a repeating alarm (the false-positive half).
- A repo I never declared must **not** be able to absorb a whole pass silently
  when the pass had something to do. *Bug if:* the only trace of a tick that
  considered and rejected N repos is the absence of output — the operator cannot
  reconstruct what the pass looked at (F1-a).
- A repo I *did* declare, whose build did not run, must be reported. *Bug if:*
  the declared case and the undeclared case are indistinguishable in the log —
  which is what makes an operator unable to audit his own declaration.

**Cross-repo (new).**
- Two repos I have made task fields must not share a build cache keyed by a
  colliding name. *Bug if:* the same basename in two repos resolves to one
  cache and one repo's build reads the other's objects.
- The declared timeout is **per build**, not per round. *Bug if:* N declared
  repos on one tick can hold the tick for N × the declared bound with no
  statement that this is intended (`11` §5 N8 bounds *one* build; nothing in
  either document bounds a *round*).
- A repo whose build is not a Go build is not punished for not being one:
  the gate says so rather than aborting (`internal/tools/merge_build_gate.go:283-284`,
  *"no declared build and not a Go module … merging unchanged"*).

**A boundary the operator should NOT expect** (new, so "allowed" stays
separable from "broken"): the close loop does **not** verify that the operator's
`PATH` binary is the artifact it built — that is `11` §6 U1, still undecided,
and this document does not decide it either (§8).

## 6. Gap analysis

### 6.0 The audit verdict on `11-build-and-repos.md`

Briefing acceptance items, each **measured** against the committed file:

| briefing acceptance item | `11` status | evidence (measured this run) |
|---|---|---|
| 8-section shape complete | **YES** | sections at `11:64,98,131,183,277,323,567,658` |
| Rule 12 cited as requirement source | **YES** | `11:210,218,227,255,261,457`; `GOPI.md:331` verified |
| stale binary as user-visible consequence | **YES** | `11` §4 quotes `t-1788383728` verbatim incl. "stale-bin incidents" |
| 'no entry = no build, silently' judged vs Rule 15, named a finding if it conflicts | **NO — the gap this run closes** | `grep 'Rule.15\|Rule15' 11-…md` → **exactly one hit, `11:609`, a §7 HEADING**; the body under it (`11:610-625`) is about N2/N3 declaration semantics and never mentions Rule 15 |
| cross-repo task fields covered | **NO — the gap this run closes** | literal `cross-repo` occurs **0 times** in `11`; the concern rides only on F3, which is about a *host's* installed copy, not repos interfering |
| build-timeout + build-failed notification covered | **YES** | `11` N4 (alarm), N8 (bound), O2 (two defaults) |
| §7 names tests whose verdict the requirement changes | **YES** | `11:567-658` names `rebuild_tracked_guard_test.go`, `rebuild_pending_test.go`, `rebuild_tagged_version_test.go`, `config_repos_test.go`, `cli_tasks_rebuild_gate_test.go`, the `merge_build_gate_*` family |
| at least one finding new to the operator | **YES** | `11:549-566` ranks F1/F2/F4/C4-N3/C2-U1 |
| C1–C5 check present | **YES** | `11:475-566` |
| 'what I could not determine' present | **YES** | `11:694` |
| no tests written / no code changed | **YES** | `11` front matter: "this branch changes exactly one file" |

**Verdict: `11` is sound and load-bearing — 9 of 11 items satisfied, and I found
no claim in it that my own reading contradicts.** The two unmet items are exactly
the two the briefing predicted, and both are closed below. I record one
*internal* inconsistency in `11` that my closure repairs: `11` §7's heading
(*"N2/N3 — the declaration semantics, and one Rule-15 judgement call"*, `11:609`)
announces a judgement its body never makes. This document is that judgement.

### Where the code serves the need

Beyond `11` §6's list, three mechanisms measured here that serve the need well
and that `11` does not name:

- **The error branch of the skip decision is loud, and says why.** The comment
  at `cmd/agent/cli_tasks_rebuild.go:242-245` names Rule 15 explicitly and
  distinguishes "unanswerable" from "declared-nothing". This is the module
  already applying the rule this briefing sends me to check — the check
  therefore passes *in principle* and fails only on the third path (F1-a).
- **The pass pre-filter de-duplicates deliberately** (`mergerUniqueRepos`,
  `cli_tasks_rebuild.go:284-296`): *"one physical checkout is never built twice
  in one close loop"*. This is the cross-repo concern solved at the *cost* axis
  before it was ever named.
- **Non-Go repos are exempted from the module-cache requirement on purpose**
  (`internal/tools/merge_build_gate.go:304-307`): *"a repo that is not a Go
  module and whose declared build does not run the Go toolchain has no module
  set to resolve, so a host with no cache must not abstain on it (that would
  abstain every non-Go repo for an irrelevant reason)"*. This is the cross-repo
  half handled: the contract is language-agnostic (Rule 12's "anything their
  build needs") and the *gate* knows it must not impose Go's prerequisites on a
  foreign repo.

### Where it falls short

**F1-a — the merger pass writes NO line for a repo with no build contract, while
the same file promises one; this is the Rule 15 conflict the briefing asks
about.** *Measured, not inferred.* Two call sites, opposite behaviour:

- `serveRebuild` (`cmd/agent/cli_tasks_rebuild.go:311-318`) — `!found` ⇒
  `fmt.Printf("[merger] [rebuild] skip: no build contract for %s in settings
  repos table\n", repo)` at `:317`, under the comment *"the deliberate silent
  skip (docs-only repo / undeclared build) … One quiet log line for the
  operator's audit trail"* (`:306-307`).
- `mergerPassCloseLoop` (`cmd/agent/cli_tasks_rebuild.go:239-252`) — `!found` ⇒
  `continue` at `:251`, under the comment *"no contract, nothing to rebuild, and
  no per-pass log line about it (see ResolveRepoBuild)"* (`:247-249`).

The error half of the first check **is** printed, with Rule 15 named in the
comment (`:241-245`); the `!found` half of the pass filter is not printed at
all. *Consequence:* on a merger-host tick, a repo the operator declared but
which carries an **empty** entry (a PURE MERGE TARGET — a supported, documented
state, `internal/config/config.go:970-990`) and a repo where the close loop
never reached the build are **indistinguishable from the operator's side**,
because neither prints. *Consistency with Rule 12:* none broken — Rule 12
explicitly wants the skip. *Consistency with Rule 15:* **broken in the
documentation sense** — the module's own stated contract ("one quiet log line")
is not kept on the tick path, so an operator auditing for the line reads a pass
that swallowed N repos as a pass that had nothing to skip.

*Honest scope of the finding.* `[opinion]` The correct fix is a **one-line
count or per-repo line on the pass path**, not an alarm — Rule 12's silence
requirement is right and must not be traded away for Rule 15. [inferred] that
the omission was a deliberate cost decision at the pass level ("no per-pass log
line" reads as a choice, not an oversight); *weakest link:* nothing records who
made it or why, and `11` §6 F1 attributes the module's silence to the *dirty
skip*, not to this path — so the two documents name different silences and
**this one is the one that fires on every tick**.

**F1-b — a comment claims reporting that the code does not perform.**
*Measured.* `internal/tools/rebuild.go:115-118`:

```go
// same purposeful silence RebuildAfterMerge keeps). Reported, not swallowed:
// an unreadable settings file means the question is unanswerable, and the
// honest answer to "should this round be unwound?" is NO.
if _, found, err := config.ResolveRepoBuild(repo); err != nil || !found {
    return nil
}
```

The comment says *"Reported, not swallowed"*; the statement `return nil`s on
**both** `err != nil` and `!found`, and writes nothing. `RebuildAfterMergePrebuild`
has exactly one production caller (`cmd/agent/cli_tasks_rebuild.go:341`).
*Mitigation, stated so the finding is not overstated:* on the same loop,
`serveRebuild` re-resolves the contract at `:311` and **does** print the error
at `:313` — so the unreadable-settings case is reported *elsewhere in the same
tick*, and the net effect on the operator is not a swallowed error. What is left
is the weaker, still-real defect: a comment asserting a property the function
does not have, which a future reader will cite as evidence that this path
reports. [opinion] In a module whose opening premise is "silence is a bug", a
comment that mis-describes reporting is not cosmetic — it is the same class of
defect as `11` §7's heading.

**F2 — the timeout is per-build; the round is unbounded, and the cross-repo
limit is therefore N × the declared bound with no stated N.**
*Measured.* `repoBuildTimeoutDefaultSec = 300` (`internal/config/config.go:959`,
comment "5 min") and the gate's own `300` (`merge_build_gate.go:100`); the
operator's live settings declare `600` for GoPi; `RepoBuildTimeoutEnv`
(`GOPI_REPO_BUILD_TIMEOUT_SEC`, `config.go:955`) overrides **for ALL repos**
when an entry declares none — its own doc comment says so. `mergerPassCloseLoop`
iterates `mergerUniqueRepos(repos)` — **all configured repos**
(`cli_tasks_rebuild.go:239`). So a tick that lands in N repos can spend up to
N × timeout, serially, holding the merger's close loop. Rule 12 and `11` §5 N8
both bound *one build*; **neither bounds a round**, and `11` §8 does not list
the round bound among its undetermined items. *[opinion]* This is not a bug
today (the live table declares one build repo plus one pure merge target), but
it is the *cross-repo* limit the briefing asks be covered, and it is the one
that scales with the operator's stated intention to make arbitrary repos task
fields (`t-1788383728`: *"a general process — say our task is not only aimed at
the gopi project"*). *[inferred]* that serial-per-repo is intended (there is no
concurrency in the loop and no comment asking for any); weakest link: no row
prices the round, so "unintended" and "unpriced" are indistinguishable.

**F3 — Rule 15a applies literally to this module's own persisted contract, and
it is unguarded in the direction that matters (it is safe *because nothing
writes the file*, not because the tags are right).** *Measured.*
`RepoConfig.Build` is declared `json:"build,omitempty"` and `BuildTimeoutSec` is
`json:"build_timeout_sec,omitempty"` (`internal/config/config.go:398,401`).
Rule 15a (`GOPI.md:453` ff.) is explicit that `omitempty` on a
required-on-the-wire field silently erases it, and that the decision rule is
*"`omitempty` belongs on OPTIONAL outgoing fields only"*. For `BuildTimeoutSec`
this is the textbook hazard: **`0` means "use the default"** by the documented
resolution order (`config.go:988-996`), so if any code path ever round-trips a
settings file through this struct, a repo that had `0` and a repo that had the
key **absent** become the same document — silently, and exactly as designed-away.
*What saves it, measured:* nothing in production **ever marshals** the `repos`
table. `grep -rn 'json.Marshal' internal/config/*.go` (non-test) → **empty**;
`grep -rn 'os.WriteFile\|Create(' internal/config/*.go` (non-test) → **empty**;
the only file matching `settings` *writing* patterns is
`internal/config/config_repos_test.go`. So the defect is **latent and
structural**: the tags are optimistic, and the safety margin is "no writer
exists yet" — which is precisely the kind of margin Rule 15 was written to
remove, and which a future `gopi settings set repos.…` command would delete in
one commit. *[opinion]* The right closure is not to strip `omitempty` (the
current zero-value semantics are correct on the READ path) but to **state the
one-writer invariant**, or add the Rule 15a round-trip test the rule itself
prescribes (*"add a test that round-trips the zero-value case"*) — noted in §7,
not written, per this run's constraints.

### Where it over-builds

**O3 — the module carries two independent silences and documents only one.**
`11` O1/O2 judge the pre-merge gate. I add: the *skip* decision exists in **two
places with two different observability contracts** (F1-a) — `RebuildResult.Skipped`
is a first-class field whose doc says *"the server deliberately does nothing (no
error, no guess)"* (`internal/tools/rebuild.go:53-57`), yet the pass path never
constructs a `RebuildResult` at all and therefore never sets it. One concept,
two code paths, two behaviours. Under C3 (below) that is a branch whose cost has
not been paid for.

### Where no requirement was ever decided

`11` §6 U1–U3 stand (which binary is "the" binary; what a build may touch; what
happens to the artifact) and I add two:

**U4 — whether a round is bounded, or only a build (F2).** No row and no rule
states a ceiling for N repos on one tick. `11` §8 does not list this.

**U5 — whether a settings WRITER will ever exist, and therefore whether the
`omitempty` tags are a latent defect or a live one (F3).** The read path is
correct today; the write path is absent by fact, not by requirement.

### The C1–C5 constraint check

The operator's five binding constraints are `devdoc/requirements/00-method-and-standards.md:377-440`
(C1 one static binary/zero deps, C2 the operator controls everything, C3 daemons
are simple, C4 clean up after yourself, C5 small team/limited budget). `11`
§6 already runs this check and I **agree with every verdict it reaches**; the
additions below are the ones my three findings force, and none reverses `11`.

- **C1 — one static binary, zero dependencies, no plugins.** **SERVED**, and my
  additions keep it served. The `repos` table adds no dependency: it is
  stdlib-JSON config read through `ResolveRepoBuild` (`config.go:970`). The
  cross-repo mechanism that *could* have imported something (a per-repo build
  daemon, a cache service) does not exist — isolation is achieved by **naming
  caches per repo**, i.e. by strings and directories
  (`TestMergeBuildGate_GoCacheSeparatesRepos`, `internal/tools/merge_build_gate_gocache_test.go:218`),
  not by a process. **No conflict.**
- **C2 — the operator must control the whole thing; anything he cannot observe
  or reverse is a defect.** **FALLS SHORT — and this is where my finding lands
  hardest.** `11` §6 records C2 failing on the *dirty-skip* (its F1). I record a
  **second, independent** C2 failure, on a different path: the merger pass's
  `!found` continue (`cli_tasks_rebuild.go:250-252`) is **neither observable nor
  reversible** — it prints nothing, sets no field, and leaves no mark. Two
  independent silences in one module is not a coincidence of design; it is C2
  failing twice because Rule 15 was applied on one path and not the other.
  *Note the asymmetry that makes this decidable:* the operator has no way, from
  the log alone, to count how many of his declared repos a given tick skipped —
  which is exactly what C2 says must not happen.
- **C3 — a daemon must be simple; every branch multiplies the failure surface.**
  **SERVED, with my addition judged as the cost of an unpaid branch.** The
  post-merge step is mechanical (`11` §6 C3) and I agree. My addition: the skip
  decision is implemented **twice** (F1-a / O3). A single-concept/two-path shape
  is exactly the branch C3 asks one to notice, and here it has already produced
  the divergence C3 predicts — the two paths disagree about whether they print.
  *The C3 judgement this document records:* the module PASSES, and the one
  branch it should collapse is the duplicated skip predicate, not the gate.
- **C4 — the agent must clean up after itself.** **SERVED**, unchanged from
  `11`. Cross-repo strengthens it: per-repo cache *names* are what keep one
  repo's build residue out of another's reach, so C4 is served at the
  multi-repo scale by the same mechanism. *One interaction I record without
  raising it as a defect:* `--untracked-files=no` (`11` §6 C4/N3) is the
  predicate the skip guard uses, and my F1-a finding is about a *different*
  skip (no-contract), so the two do not compound.
- **C5 — small team, limited resources; cost is a requirement.** **SERVED, with
  one unpriced term.** The declared bound, the "already current" short-circuit,
  the PURE MERGE TARGET, and the de-duplication (`mergerUniqueRepos`) are all
  real cost discipline, and I agree with `11`. The unpriced term is my **F2**:
  N declared repos ⇒ up to N × timeout serially per tick, with no stated ceiling
  and no operator-facing statement that this is the intended cost. *[opinion]*
  This is the cheapest of my findings to close (one sentence of requirement, or
  one comment naming the serial bound) and the most expensive to leave, because
  C5 asks for the thing one person can **understand** — and an unbounded round
  is the sort of term that only shows up when it is already the problem.

### Findings that are new to the operator

Ranked by consequence. Rank 1 is the finding this briefing was dispatched for.

1. **F1-a — the merger PASS prints nothing for a repo with no build contract,
   although the same file promises "one quiet log line"; Rule 12's silence
   requirement is satisfied, Rule 15's record requirement is not, and the two
   are distinguishable in the code.** The conflict is real, narrow, and
   code-against-code (comment `:306-307` vs comment `:247-249`). *Not* a
   rule-against-rule conflict: Rule 12 and Rule 15 are consistent, and this
   module already applies Rule 15 by name on a sibling branch of the same
   decision (`:241-245`).
2. **F2 — the timeout bounds a BUILD, never a ROUND; N configured repos on one
   tick can cost up to N × the declared timeout with nothing stating so.** The
   cross-repo limit the briefing asks for, and it scales with the operator's own
   stated plan to make arbitrary repos task fields.
3. **F3 — Rule 15a applies literally to `RepoConfig`'s `omitempty` tags, and the
   only thing preventing the silent-erasure failure is that no settings writer
   exists.** `0` and absent already collide by documented semantics
   (`config.go:988-996`); a future write path makes the defect live in one
   commit.
4. **F1-b — a comment says "Reported, not swallowed" over a bare `return nil`
   (`internal/tools/rebuild.go:115-118`); the same condition is reported
   elsewhere on the same loop, so the operator is not currently misled, but the
   comment is false as written.**
5. **O3/U4 — one concept (`skipped`), two code paths, two observability
   contracts, and no stated round bound** — the C3 branch and the C5 term, named
   together because they are the same shape: an unmade decision expressed as an
   absent line.

## 7. Implications for tests

**No tests were written or changed by this document** (a constraint of this
run). What follows is which **existing** build/repos tests would change verdict
if the requirements above were taken literally. It extends `11` §7 rather than
replacing it — the tests `11` names keep its verdicts, and I neither re-judge
nor contradict them.

**F1-a first, because it names a line no test can see.**

- `internal/tools/rebuild_test.go` (`TestRebuildE2EMergeRoundBinFollowsMaster`
  and the no-contract / mismatch / failure cases) — **green, correct, and blind
  to F1-a by construction.** It exercises `RebuildAfterMerge`, which sets
  `RebuildResult.Skipped = true` (`internal/tools/rebuild.go:155`); it does
  **not** exercise `mergerPassCloseLoop`'s pre-filter, which never builds a
  `RebuildResult` at all. **Direction: keep; the requirement "a skipped repo is
  accounted for" has no assertion anywhere** — the pass path needs one.
- `internal/config/config_repos_test.go` — **green, and it is the requirement's
  executable form** (`11` §7 says keep; I agree). It pins that an empty build is
  not an error, which is the *input* side of F1-a. **Direction: keep.** The
  output side — that the skip is *counted* — is unpinned, and that is the gap.
- `cmd/agent/cli_tasks_rebuild_pass_test.go`
  (`TestMergerPassCloseLoopWiredAfterSweep`) — **green; it pins that the pass
  reaches the close loop, not what the close loop says.** **Direction: keep,
  and it is the natural home for an F1-a assertion**, because it already drives
  the pass path that produces the silence.

**F2 — the bound that no test protects.**

- `internal/tools/merge_build_gate_test.go` and the `rebuild_test.go` timeout
  cases — **green; they pin that ONE build is killed at the bound.** Nothing
  pins the round. **Direction: the verdict does not change**, and a test
  asserting "the round's cost is bounded, or the serial bound is stated" would
  be new coverage rather than a corrected verdict. Recorded as coverage-absent,
  not as a failing test.
- `internal/config/config_repos_test.go` — **its timeout matrix is the right
  place** for the `RepoBuildTimeoutEnv`-overrides-ALL-repos semantic
  (`config.go:955`), which is the cross-repo coupling F2 turns on. **Direction:
  verify the matrix covers the env-override-for-all case; if it does, F2's
  per-repo half is already pinned and only the round bound is unstated.**

**F3 — a Rule 15a test that Rule 15a itself prescribes.**

- `internal/config/config_repos_test.go` — **render/extract oriented today.**
  Rule 15a's decision rule (`GOPI.md:453` ff.) explicitly asks for *"a test that
  round-trips the zero-value case"*. `BuildTimeoutSec` with value `0` is exactly
  that case, and its collision with "key absent" is documented in
  `config.go:988-996`. **Direction: green today; the correct closure is a
  round-trip test — which is precisely what this run is forbidden from writing.**
  Named here so the next run does not have to rediscover it.

**Cross-repo — the invariant that already has the right pin.**

- `internal/tools/merge_build_gate_gocache_test.go`
  (`TestMergeBuildGate_GoCacheSeparatesRepos`, `:218`; the file's header states
  the arm as *"cross-repo isolation: the same basename in two repos never shares
  a cache"*, `:18`) — **green, and it is the cross-repo requirement's executable
  form.** This test is why the briefing's `cross-repo` item can be closed with a
  *cited existing guard* rather than a new one. **Direction: keep; it is
  load-bearing.** `11` §7 records this same file as **RED on master** for an
  unrelated placement defect (`TestMergeBuildGate_GoCacheLeavesNoScratchCache`,
  `:160`) — I did **not** re-measure that red in this run and do not adopt or
  contradict `11`'s reading of it; it is quoted as `11`'s finding.
- `internal/tools/merge_build_gate_unresolved_test.go`,
  `_modcache_test.go`, `_contained_test.go`, `_cleanup_test.go`,
  `_test.go` — **green; they pin ABSTAIN-not-FAIL per instance.** `11` §7
  correctly notes the *class* is unpinned. My cross-repo addition does not change
  those verdicts; it adds that the **non-Go exemption**
  (`merge_build_gate.go:304-307`) is the instance that keeps a foreign repo from
  inheriting Go's prerequisites — worth a comment-level pin, not a new verdict.
- `cmd/agent/cli_tasks_rebuild_gate_test.go` — **RED on master per `11` §7**
  (stale anchor after `126fad74`, fix on `fix/cmd-agent-red-reanchor`
  `63e86d64`). **Not re-measured this run; not contradicted.** It is named here
  only so the F1-a reader does not attribute that red to this finding.

**Consistency note for §7.** `11` §7 opens with *"No tests were written or
changed by this document"*; this §7 opens the same way, for the same reason, and
the two §7s **agree on every test they both name**.

## 8. Confidence and limits

**Per-claim confidence.**

| claim | confidence | basis |
|---|---|---|
| Audit verdict on `11` (9/11 items, 2 gaps) | **high** | each item grepped in the committed file; the two misses are literal zero-hit measurements |
| F1-a (pass path writes nothing) | **high — read, not inferred** | both call sites read end to end (`:239-252`, `:311-318`) with their own contradictory comments quoted |
| Rule 12 and Rule 15 are CONSISTENT (F1-a is code-vs-code, not rule-vs-rule) | **high** | Rule 12 separates the unreadable-settings case itself; the code names Rule 15 on that very branch (`:241-245`) |
| F2 (per-build bound, no round bound) | **high on mechanism, medium on intent** | the loop is serial over all configured repos, read; whether the round bound was ever considered is not recorded |
| F3 (`omitempty` latent, no writer) | **high that no writer exists; high that the tags are rule-textbook** | non-test `json.Marshal` / `os.WriteFile` greps in `internal/config` returned empty; `0`≡absent from `config.go:988-996` |
| F1-b (comment vs code) | **high on text; medium on consequence** | lines quoted; the mitigating report on the same loop is also read, so I claim a comment defect and not a swallowed error |
| Cross-repo coverage exists and is pinned | **high** | `TestMergeBuildGate_GoCacheSeparatesRepos` at `:218`, header arm at `:18`; non-Go exemption at `merge_build_gate.go:304-307` |
| `11`'s own red-test readings | **medium — NOT re-measured this run** | adopted as `11`'s findings, explicitly marked un-re-measured in §7 |

**Inferences, each with its weakest link named.**

1. `[inferred]` The pass path's missing line was a deliberate cost decision.
   *Weakest link:* the comment says "no per-pass log line" as a statement of
   fact, not as a decision; nothing records who chose it.
2. `[inferred]` The serial per-repo loop is intended rather than unconsidered.
   *Weakest link:* there is no comment asking for concurrency either, so
   "unintended" is not excluded — only unpriced.
3. `[inferred]` "Per-host promise" and "per-repo isolation" are two different
   requirement objects. *Weakest link:* no row splits them; the split is my
   reading of `11` §2 + F3 against the cache-isolation test.
4. `[inferred]` The `omitempty` tags are safe only because no writer exists.
   *Weakest link:* absent writer is measured; the *future* command that would
   break it is hypothetical.

**What I could not determine.**

- **Whether the operator was ever told that the merger pass skips silently.**
  `11` §8 asks the same question about the *dirty* skip and could not answer it.
  For F1-a I can add one measured fact and no more: the code's two comments
  disagree with each other, and neither cites a decision row. Whether the
  silence was accepted, overlooked, or simply never noticed is not recorded
  anywhere I could find.
- **Whether a settings WRITER is planned.** F3's severity is entirely
  determined by it: with no writer the `omitempty` tags are inert; with any
  writer (a `gopi settings set`-style surface) `BuildTimeoutSec: 0` vs absent
  becomes a silent semantic change. I found no row, no CLI verb and no devdoc
  proposing one.
- **The intended round bound (F2).** I can measure the bound per build and the
  serial loop; I cannot find a statement of what a *tick* is allowed to cost,
  and `11` §8 does not list this among its undetermined items either — so this
  gap is new to the series, not inherited.
- **Which of the two silences `11` §6 F1 actually found.** `11` attributes the
  module's silence to the *dirty-tree guard* (`:319-330`); I find an independent
  silence on the *no-contract* path (`:250-252`). Both are measured and both are
  real; whether the earlier author saw this one and chose the other, or did not
  see it, is not stated. I record the divergence rather than resolve it, because
  only the author knows.
- **Whether `internal/tools/rebuild_vet_gate.go` belongs in this boundary.**
  `11` §8 leaves it out for lack of an operator row; I reached the same
  conclusion independently (§3 notes it only as a neighbour of the exit table)
  and add nothing. Left out rather than invented.

**Verification of this document.** This branch adds exactly **one** file — this
one (the run report lives in the session directory, outside the worktree);
`git status --porcelain` is quoted in `## Evidence`.
`CGO_ENABLED=0 go build ./...` was run in this task's worktree and its exit code
is quoted there. **No test was written, no production file was touched, and no
existing document was edited** — the whole of this run is reading, measurement
and one new file.

## 9. Consistency with `11-build-and-repos.md` (no contradiction)

The briefing requires the two documents not contradict each other. Point by
point, where my verdicts sit relative to `11`'s:

| topic | `11` verdict | this document | relation |
|---|---|---|---|
| The need itself | the binary follows the trunk | **adopted verbatim** | agreement |
| Boundary | 4 mechanisms + `internal/version` | **adopted**; adds Rule 15 + cross-repo | extension |
| Actors (§2) | operator / agent / second node / maintainer | **adopted unchanged** | agreement |
| N1–N8 | stated with why-tests | **adopted**; adds N9 | extension |
| The pre-merge gate ≠ the post-merge rebuild | asserted, defended | **adopted verbatim** | agreement |
| Declaration semantics (N2/N3) | Rule 12 + `ResolveRepoBuild` | **adopted**; cites the same `config.go:970-990` | agreement |
| Failure semantics (N4) | alarm, never rollback | **adopted** | agreement |
| The module's silence | F1 — the **dirty-skip** (`:319-330`) | **not contradicted**; adds F1-a the **no-contract pass path** (`:250-252`) | independent addition |
| Rule 15 | *never applied* (one heading, `11:609`) | **applied**, judgement recorded in §4 N9 / §6 F1-a | the gap this run closes |
| `cross-repo` | literal **0 occurrences**; rides on F3 (a host's copy) | **covered** (F2 + the cache-isolation pin) | the gap this run closes |
| C1–C5 | all five judged | **agrees with every verdict**; adds a *second, independent* C2 failure (F1-a) and the C5 round term (F2) | agreement + extension |
| §7 tests | named per finding | **agrees**; extends F1-a, F2, F3 | agreement + extension |
| U1–U3 / §8 limits | three undecided; six unknowns | **adopted**; adds U4, U5 and three unknowns | extension |
| Findings new to operator | F1/F2/F4/C4-N3/C2-U1 | **adopted**; adds F1-a/F2/F3/F1-b/O3-U4 | extension |

No row above reverses a verdict `11` reached, and the two documents share one
governing citation (`t-1788383728`, quoted in `11` §4) that this document cites
rather than re-quotes.
