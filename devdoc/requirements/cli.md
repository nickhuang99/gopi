<!-- doc-class: normative -->
# Requirements (reverse analysis) — the CLI surface: the operator's cockpit

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Module [9/15] of the reverse requirement analysis.** The direction is inverted:
the built system is the evidence, the need is *inferred* from it, and the value of
this document is §6 — the gap between the inferred need and the code.

| | |
|---|---|
| **Boundary** | *The operator can dispatch, watch, verify and inspect the whole system from one place, and that place still fits in one person's head.* The surface's **shape and size**, not any subsystem's behaviour. |
| **Base read** | master `812f9a60` (the SHA this worktree was created at, `812f9a606b1814bf093f2189d70165828d59bbfe`, dated 2026-09-18 18:51:27 +0800). Every `file:line` below is that revision. |
| **Branch / worktree** | `docs/req-cli-surface` @ `/home/user/workspace/gopi-req-cli` (Rule 6a) |
| **Deliverable** | this file, and nothing else (`git diff --name-only master..HEAD`) |
| **Method** | `devdoc/requirements/00-method-and-standards.md` — the mandatory 8-section shape (`:199`), the binding constraints C1–C6 (`:377`–`:446`), the honesty rules |
| **Measured** | every number below was produced **in this session** on `812f9a60` by the script printed in §4.0; the ledger was re-queried live at write time |
| **Sibling** | ledger row `01a0ab12-5b52-7404-8b27-9e0bdac8bdd2` (`source=llm`, status `in_progress`) names the same deliverable path; row `01a0ab3e-94f9-7bb4-bf34-767ae30add9e` (`source=llm`, `completed`) was the earlier task that commissioned it. **Neither is an operator row** — see §4.0 and §8.5: no `08-cli.md` and no `cli.md` was ever produced (`git log --all --oneline --diff-filter=A -- 'devdoc/requirements/cli.md'` → empty), so this document is the first. |

**Boundary statement, expanded (the umbrella's M4 mismatch).** `devdoc/requirements/00-module-map.md:186`
names this boundary *"M7 — the operator's cockpit (CLI surface)"* and states the
responsibility: *"one person can dispatch, watch, inspect and repair the whole system
from places he understands"*. The CLI is **not a module** — the verbs belong to the
modules whose needs they expose. A directory-shaped decomposition would give you
`cmd/agent` and lose the requirement entirely, because the user-visible thing is not
the directory but *the words he types*. So this document owns the **vocabulary, its
size, its discoverability and its drift guards**, and it deliberately does **not**
own what any verb *does*: the ledger's semantics are document 01's, the merger's are
02's, sync's are 04's, memory's are 05's. §5 and §6 below judge **the shape and the
size of the surface**, never a subsystem's behaviour.

---

## 1. What this is for (product-owner language)

**This is the place I run the machine from.**

I do not want a pile of excellent separate tools that each solve one problem and
refuse to talk to each other — I have those, and the cost of running them together
is the whole problem. I want **one command I type** that can start work, watch work,
decide about work, and show me what happened to work, without me leaving it to open
something else.

So the thing this module is for is *the surface itself*: the set of words I say to
the machine, and the promise that the set is:

1. **Complete for my actual work** — dispatch, watch, merge, verify, inspect the
   ledger, inspect a session, inspect a log, decide what is ready. When I need to do
   one of those things, the word for it exists.
2. **Discoverable without reading code** — I can ask the machine what it can do and
   get an answer I can read in one sitting; the names tell me what they do and are
   not a private dialect.
3. **Self-consistent** — one concept, one word. If the machine internally calls a
   thing by two names, I should not have to learn that; and if a word used to mean
   something and now means something else, the machine must tell me where it went
   rather than silently doing the old thing or silently doing nothing.
4. **Still small enough to hold in my head** — this is the part I am actually
   worried about. The surface's growth is the cost of every capability I added, and
   nobody is charged for that growth except the person who has to remember it.

**The test I would personally apply** (in the product owner's seat, no code open):
I sit down at a terminal, I type the machine's bare name, and I get a list of what
it can do. I pick up one line, read it, and I can do my whole day: hand work in, see
it go, see what came back, decide whether it is good, and look up anything that went
wrong. If I ever have to open a source file to find out how to do one of those
things, **this module has failed** — that is not a slogan, it is the exact failure
mode the two drift guards described in §4 exist to prevent.

---

## 2. Who is served

**The operator (nick) — the primary user, and the only one who can accept this
module.** His need is stated above. Everything in §5 and §6 is judged against *his*
ability to hold the surface, because he is the one who pays for its size. He is also
the one who has been **shrinking** it: ledger `t-1788037892` (`source=human`,
cancelled), `t-1788039140` (`source=human`, completed) and `t-1788039618`
(`source=human`, completed) are three operator rulings that each *removed* a verb
from his own cockpit because keeping it cost more than it bought (§4.4).

**The agent (Bond, the harness user) — a second user with a contradictory need.**
The agent also runs these words, but through the shell tool, and it reads them
through a *different* channel: the tool-call record. Two consequences that shape the
whole module:

- the agent **needs the words it emits to be stable identifiers**, because its own
  transcript is the evidence of what it did — a renamed word breaks the audit trail,
  not just a habit (`internal/toolnames` is the constant source for exactly that,
  and `cmd/agent/cli_toolnames_contract_test.go:88` is the gate that keeps the CLI's
  spelling byte-identical to the wire name);
- the agent **must not be able to start the never-returning verbs** — ledger
  `01a09260-f5c8-72a6-98b0-ba1f6846a08f` (`source=human`, completed) is the operator
  ruling: the agent is forbidden from running the task-runner in any form, including
  background and including through a script, because it stalls the agent's own
  session. This is the one place where the two users' needs are in direct conflict,
  and the resolution is enforced in the surface, not in discipline (`guardVerbOrExit`
  at `cmd/agent/main.go:196`).

**Server and client nodes — the same surface, remotely.** A peer is addressed with
the same words, and one dispatch path tolerates being invoked through an ssh
passthrough where the argv carries the host and the ssh options in front of the verb
(`cmd/agent/cli_tasks.go:705`). The need here is *the words survive a hop* — a
surface that only works when typed locally is not the cockpit for a system that
runs on two hosts.

**Future maintainers — the person who must not re-learn this.** Two guards exist
purely for them (§4.5): one that fails the merge when the dispatch chain and its
mirror drift, and one that fails the build when a verb is added to dispatch but not
to the index. Both encode a *rule about how to change the surface*, which is this
module's real long-term deliverable.

---

## 3. The workflow it sits in

**Entry condition.** The operator has a need to change the machine's state or to see
what the machine did. He has a terminal, the machine's single binary, and the
expectation that a bare invocation tells him what is available. Nothing else has to
be set up first — no environment, no config file, no daemon.

**The path.**

1. **Learn the surface.** A bare invocation prints the command index; appending the
   help flag to any word prints *that word's* page (`internal/config/help.go:413`).
   This is a zero-side-effect path by contract — asking what a word does must not
   start a session, spend a model call, or claim work.
2. **Hand work in.** Work is submitted rather than executed. The agent's version of
   this is a tool call; the operator's version is a typed word. Both land in one
   queue, and the queue is the handoff — no process spawns another process as a
   "request".
3. **Run it.** A resident runner drains the queue, or one named row is run in the
   foreground by naming it. These are the *never-returning* words, which is exactly
   why the surface has to distinguish who may start them (§2).
4. **Watch.** A read-only query answers "what is running right now", and a second
   answer reports the health of what *should* be running.
5. **Decide.** Merging, archiving, verifying, reclaiming — the words that change a
   piece of work's status after it exists.
6. **Inspect.** The ledger listing, one row's detail, a session's statistics and its
   byte-level log. This is the reverse direction of the whole loop: it is how the
   operator reconstructs what happened, and it is the only reason the surface can be
   trusted.
7. **Recover.** A word that used to exist and no longer does must say so and name its
   replacement, rather than falling into a generic error — because a script, a
   deployment, or a log entry from last week may still type the old word.

**Exit condition.** The operator has either changed the machine's state or answered
his question, **without opening a source file and without having learned a dialect**.
The invariant he must be able to rely on at exit: whatever he typed, the machine
either did it or told him in his own vocabulary why it did not.

**What sits before and after.** Before: the agent loop and the task ledger (a row is
what a verb acts on). After: the merge-and-branch lifecycle and, once a round lands,
rebuilding the project so the binary matches the repository — that last step is
itself a requirement the operator filed (`t-1788383728`, §4.4) and it closes the loop
this surface opens.

---

## 4. Why it exists — the requirement per need, with citations

### 4.0 The measurement, first (the method, and why the counts in circulation are not usable)

The number *is* the finding here, so the method is stated before the numbers.
**All counts below were produced in this session, on base `812f9a60`, 2026-09-18, by
running this script from the repository root of a built tree:**

```bash
# ~/workspace/_tmp/cli-verb-census.sh — run from the repo root; bin/gopi must exist.
# A. the REGISTRY of top-level verb words (constant values, not string literals)
grep -rhoE '\bCliVerb[A-Za-z]+ = "[a-z0-9_-]+"' $(ls cmd/agent/*.go | grep -v _test) \
  | sed 's/.*= "//;s/"//' | sort -u | wc -l
# B. the dispatch-chain predicates on main.go's single `if` line
grep -oE 'is[A-Za-z0-9]+Subcommand\(dispatchArgs\)' cmd/agent/main.go | sort -u | wc -l
# C. the words the chain ACCEPTS: predicate -> dispatcherOfToken -> that constant's VALUE
#    (python: reads cli_contracts_test.go's dispatcherOfToken, then each constant's definition)
# D. the words ADVERTISED in the Commands index of the built binary's own -h
./bin/gopi -h | sed -n '/^Commands (each accepts -h/,/^Examples:/p' \
  | grep -oE '^  gopi [^ ]+' | sed 's/^  gopi //' | sort -u | wc -l
# E/F/G. the `gopi task` family: DispatchVerbs = union of internal/taskverbs' 5 groups
```

**Binary provenance, because rows C and D must not be read off a foreign build.** Row
C is computed from **source at `812f9a60`** (the dispatch chain and the constant
definitions); row D was read from a binary whose own `version` output is
`v20260918-1114-812f9a60 (812f9a606b18)` — i.e. **built from this same revision**, not
a newer or older one. Rows A, B and E–G are source-derived and reproduce from the
printed script alone; a rerun of the script in this session reproduced A = 22,
B = 28, E = 29, F = 17, G = 12 exactly (D needs a built `bin/gopi` and is therefore
the one row whose reproduction depends on a build step, which is why its provenance is
stated here).

**Measured, in this session:**

| # | What is counted | How | Value |
|---|---|---|---|
| A | top-level **verb words registered** (`CliVerb*` constant values) | registry, `cmd/agent/*.go` | **22** |
| B | **dispatch-chain predicates** on `cmd/agent/main.go`'s one `if` line | `cmd/agent/main.go:554` | **28** |
| C | distinct **words the chain accepts** | predicate → token → constant value | **25** |
| D | words **advertised** in the `Commands` index of `gopi -h` | the built binary's own help | **25** (≡ C as sets) |
| E | `gopi task` verbs **accepted** (`taskverbs.DispatchVerbs`, union of 5 groups) | `internal/taskverbs/verbs.go:302` | **29** |
| F | `gopi task` verbs **advertised** (`taskverbs.FamilyVerbs`) | `internal/taskverbs/verbs.go:193` | **17** |
| G | `gopi task` verbs **accepted but not advertised** (E−F) | derived | **12** |
| — | `gopi memory` sub-verbs | `cmd/agent/cli_memory.go:392` | **9** |
| — | size: `cmd/agent/cli_*.go` | production | **45 files / 26,527 lines** |
| — | size: all of `cmd/agent` | production | **80 files / 38,519 lines** |
| — | size: CLI test files | `cmd/agent/cli_*_test.go` | **104 files** |
| — | size: the drift-guard package | `internal/clicontract/clicontract.go` | **330 lines** |
| — | ledger at write time | live query | **1,910 task rows / 609 `source=human`** (1,909 at the §4.0 run — the ledger is live and gained one row mid-session) |

**Two counts circulated before this document, and neither survives.** The earlier
commissioning row asserts *"65 verbs"* and records that it counted
`grep -rhoE '"[a-z-]+"'` over `cmd/agent/*.go`; the umbrella document
(`devdoc/requirements/00-method-and-standards.md:428`, constraint C5) already
re-measured that method and rejected it. Re-run here on `812f9a60`, the same pattern
returns **539 distinct literals** over the **80** non-test files in `cmd/agent`
(**432** over the **45** `cli_*.go` files alone) — the count is sensitive to how
`*_test.go` files are excluded and to the file set, which is itself the objection:
the pattern is mostly catching flag names and configuration values. The other figure,
**53**, came from counting `case "…"` arms. **Both measure the wrong object**: a verb
is what the *registry* declares and the *predicate chain* honours, not what a
string-pattern happens to catch. That is why the table above counts A from the
constant registry and C from the predicate chain, and why the method is printed here
rather than summarised — 22 registered words, 25 accepted words, 29 accepted family
verbs. **The disagreement between the old methods was the finding; the new method is
the answer.**

### 4.1 Need: I can drive and inspect the whole system from one place

C2 names this the module's spine: the operator must be able to control the whole
thing (`devdoc/requirements/00-method-and-standards.md:395`), and the positive half
in his own words is *"我想能够掌控这一切，而不是一个个独立的工具，虽然每个工具都很棒，但集成运行有障碍有成本"*
(`briefings/brief-requirements-reverse-analysis.md:128`). The evidence of the
requirement in the built system is the **existence of one dispatch chain** — a single
`if` on one line decides every top-level word
(`cmd/agent/main.go:554`, 28 predicates, **measured**), and the words are declared
once in a registry (`cmd/agent/cli_verbs.go:7`, *"cliVerbs is the single source of
<!-- docanchor drift permitted 2026-09-27 (task 01a0e2ea91f37e9185e4f99317484247):
     `cliVerbs`'s doc comment opens at :9 today (see the sibling rows in this
     file that were re-keyed the same day); this row is permitted, not rewritten,
     because the module's own measured citation is preserved verbatim. -->
truth for the TOP-LEVEL gopi CLI verb words"*). Every capability the operator added
over the project's life arrived as an entry in that chain: the act words
(`cmd/agent/cli_act.go:38`), the screen-capture word, the text-to-image word, the
voice word — all of them are **faces of agent tools**, and each one became a
top-level word at the moment the operator wanted to run that capability himself
rather than through the model (§4.3 is where that pattern becomes a cost).

**The requirement, stated so the owner can accept it:** *when I acquire a new
capability, I can reach it by typing a word at the same place as everything else;
there is a single front door and I am not expected to remember which binary, script
or module owns a given capability.*

### 4.2 Need: the words are discoverable without reading code, and the machine tells me when they are not

The surface's discoverability is **machine-checked**, which is unusual enough to be
the citation. One test asserts that **every dispatched word appears in the top-level
index**, and its failure message names the incident:
*"`gopi %s` is dispatchable (%s in main.go) but missing from the top-level command
index — add it to config.commandIndex (memory #1252: an undiscoverable subcommand is
a P0)"* (`cmd/agent/usage_test.go:58`). The index it checks is the hand-maintained
table at `internal/config/help.go:457`, rendered by `FormatUsage`
(`internal/config/help.go:393`) and sectioned at `:413`.

That guard is the machine half of the need "I can find out what this does". The
human half is a **shape contract**: the help must lead with *what it can do* and put
mechanism last — `cmd/agent/help_capability_first_test.go:106`
(`TestServiceHelpIsCapabilityFirst`) and `:125`
(`TestServiceHelpDetailStaysAtTheEnd`) — and must be an aligned, terse index rather
than a ragged essay, with a column budget
(`internal/config/help.go:99`, `commandIndexMinCol = 28`, checked by
`cmd/agent/usage_test.go:92`). The reason those two tests exist is a recorded
complaint class: the operator's own rows about help that misleads him
(`01a0b182-e62c-7e30-821f-0ff723459c17`, `source=human`: *"--status gives out
misleading text … what does live-holder mean to user understanding? … it should only
tell what user is interested to know, not misleading terms"*).

**The requirement:** *the machine can always tell me what it can do, in words I can
read in one sitting, and it is a defect — caught by the build, not by my patience —
if a thing it can do is not on that list.*

### 4.3 Need: one concept has one word — and the surface must prefer *fewer* words

This is the module's most-repeated requirement and the one with the most evidence,
because the operator enforced it by hand, three times, against his own surface.

The rule is written down: a tool-derived word keeps the **tool's own spelling**
(underscore, byte-identical to the wire name, because the word is a *lookup key* into
the session log), while a word the machine itself names is **dash-cased**
(`devdoc/cli-naming-convention.md:10`, the four-kinds table). The three underscore
words are **deliberate exceptions, each with its reason and its test**:
`cmd/agent/cli_completion_exceptions_test.go:94` requires each one to still (a)
dispatch, (b) still not have a dash twin that also dispatches, and (c) still carry the
tool constant that makes the underscore meaningful
(`.reason` fields at `cmd/agent/cli_completion_exceptions_test.go:36`). An exception
nobody re-checks silently becomes either a stale exemption or an unreachable
command — that is what the gate is for.

The enforcement, from the operator, each one *removing* something:

- `t-1788037892` (`source=human`, **cancelled**) — "task new 彻底移除（nick 终审：
  shell 层不接受此命令参数整体删除"): one word removed from the external surface
  because a second legitimate path to the same act already existed internally.
- `t-1788039140` (`source=human`, **completed**) — "task update 收窄为 status-only
  （nick 终审：payload/summary 全不可变 —— a job is just a job）": the *flags* of an
  existing word were cut, and the reason is a principle about what a word may do, not
  a bug.
- `t-1788039618` (`source=human`, **completed**) — "task next CLI 收缩 + claimed
  降级为瞬时态（nick 终审：claim-and-run 原子化，无停留中间态）": a word removed and
  its state hidden, with the explicit instruction to sync the help page.
- `01a0927b-fdea-7341-ad34-8db9c9f62841` (`source=human`, completed) — the operator
  moving a word **up** to the top level because its nesting *lied* about what it is:
  *"current gopi merger is under 'task' sub command which is implying it is a sub
  command of task, actually merger is a service and run independently of any task
  just like worker which is first command of gopi, this is like a regression bug"*.

The last one is the module's clearest statement of the need: **a word's position in
the surface is a claim about what the thing is**, and a false claim in the surface is
a bug of the same seriousness as a false claim in the behaviour.

**The requirement:** *every capability has exactly one word; a word's spelling and
its place in the hierarchy both tell me the truth about what it is; and when a word
must keep existing for old callers, it says so and names its replacement rather than
quietly doing something else.*

### 4.4 Need: the surface serves the operator's actual loop — and the loop includes the step after the merge

The operator's own rows show the surface being extended for a workflow, not for a
feature list. `t-1788383728` (`source=human`, completed) is the close loop in his
words: *"when it successfully merges one branch or a list of branches, at the end it
should rebuild gopi … this is worth and necessary — this is the logic"*. And
`01a0b1c5-8674-79d1-bb19-c79574379f23` (`source=human`, completed) is the surface
being corrected against his purpose: *"go through both devdoc and code to make sure
it is back to my purpose design"*. The words for this loop exist and are
discoverable: `gopi merge` (the foreground one-shot, added `06438272`, 2026-09-18
— **later removed**; its surviving spelling is `gopi task merge`),
`gopi merger` (the resident form), `gopi worker` (the runner), `gopi task merge`
and `gopi task verify` (the ledger-side decision words).

**The requirement:** *the surface is organised around the loop I actually run —
hand work in, let it run, decide on it, land it, and rebuild what I am running — and
a step of that loop does not become reachable only from within another step.*

### 4.5 Need: the surface does not drift, because nobody notices drift

The module's two machine-checked guards exist for one failure: **a word wired into
dispatch but absent from the list the user can see**. Both cite the same incident
class rather than a principle:

- **Test-time half.** `cmd/agent/cli_contracts_test.go:290`
  (`TestCLIContractInventoryMatchesDispatch`) plus the 31-case inventory at `:67`,
  the predicate↔token map at `:149`, per-subcommand routing at `:363`, the
  legacy-spelling check at `:620`, and the one-line-alias check at `:588`.
- **Merge-time half.** `internal/clicontract/clicontract.go:7` records the incident
  verbatim: *"2026-09-12 voice_input incident: bd3d34a5 landed on master with
  `|| isVoiceSubcommand(dispatchArgs)` added to the main.go dispatch chain while the
  contract suite's mirror was NOT updated … went red on master (26 wired vs 25
  tracked) and stayed red until someone ran the suite."* The package therefore asks
  the same question **on the merge path**, from `git show` text alone — no checkout,
  no build, no test run, no model call — and it **abstains rather than refuses** when
  the two files move (`:43`, `:46`: the two paths are a contract with the suite, and a
  caller that cannot read them says so in the sweep output instead of blocking).
  `TestCompare_VoiceInputIncidentIsCaught`
  (`internal/clicontract/clicontract_test.go:147`) replays the incident against the
  two real chain shapes, and `TestCompare_FixedBranchIsClean` (`:176`) proves the
  corrected branch passes.

**The requirement:** *a new capability cannot reach the operator's surface only
half-way; the build or the merge refuses the half-wired state, so nobody has to
remember to check.*

### 4.6 Need: after a word changes meaning, the old word must still explain itself

Retirement is a **first-class surface operation**, not a deletion: the retired
spellings keep a dispatch case so that a script, a deployment or last week's log
receives a pointed message rather than the generic usage dump.
`internal/taskverbs/verbs.go:223` (`RemovedVerbs`) and `:193` (`FamilyVerbs`) are the
two halves, `TestRemovedVerbsAreNotAdvertised`
(`internal/taskverbs/verbs_test.go:223`) pins the split, and
`TestCLIContractLegacySpellingsStillDispatch`
(`cmd/agent/cli_contracts_test.go:776`) proves the old words still *reach their
handler* — deliberately exit-code tolerant, because some legacy spellings now
*refuse* with a pointer home, and *that refusal is the contract*. The deprecation
policy and its worked cases are `devdoc/cli-help-dedup.md:1` (case A `:63`, case B
`:87`, case C `:141`).

**The requirement:** *an old word keeps working long enough to tell me where the
function went, and then never lies to me by silence.*

---

## 5. Acceptance in user terms

Each statement is observable by the operator himself, and each is meant to let a
test author separate *a bug* from *allowed behaviour*.

1. **Front door.** Typing the machine's bare name in a terminal prints an index of
   what it can do, one line per word, aligned into columns, and reading it does not
   start anything, spend anything or change anything. *(§4.1, §4.2.)*
2. **Nothing hidden.** For every word the machine will act on, that word appears in
   the index. The check is mechanical: a word that dispatches but is not listed is a
   defect, and one that is listed but not dispatchable is also a defect. *(§4.2.)*
3. **One concept, one word.** No two words in the index mean the same thing, and no
   word's position implies a relationship that does not exist. If the machine
   internally uses two names for one thing, the user sees one. *(§4.3.)*
4. **The hierarchy tells the truth.** A word that runs independently of any other
   work is at the top level, not nested under a word it is unrelated to. The recorded
   wrong answer is a service word nested under the work word. *(§4.3, `01a0927b`.)*
5. **Old words explain themselves.** Typing a word the machine used to accept
   produces a message that names where the function went; it does not produce a
   generic error, and it does not silently do something else. *(§4.6.)*
6. **Help is capability-first.** A word's own help page answers *what it can do*
   before *how it works*, and the mechanism explanation, if present at all, is at the
   end. *(§4.2.)*
7. **Size is measurable and judged.** The operator can obtain, in one command, the
   count of top-level words and of the family's sub-words — and he accepts the number
   or he shrinks it. The number is not a matter of opinion; the command is in §4.0.
   *(§4.0, §6.1.)*
8. **The runner words are not startable by the agent.** The agent's attempts to start
   the never-returning words are refused *in the surface*, with a message, not by
   discipline. *(§2, `01a09260`.)*
9. **A word that acts says what it acted on**, and a word that only reads does not
   write anything (no claim, no status move, no notification) — the read/write split
   is part of the surface's contract, not an implementation nicety.
   *(§4.4; the read-only family word's own doc, `internal/taskverbs/verbs.go:193`
   group comments.)*
10. **Every word answers the help flag** with its own page and no side effect — so
    "can I run this?" is answerable without running it. *(§3 step 1, `§4.5` guards.)*

---

## 6. Gap analysis — the code against the need

**The headline.** The surface's *engineering* is in good shape: the drift class is
closed on both the test path and the merge path, the naming rule is documented with
its exceptions machine-rechecked, and the retired-word channel works. The gap is
where it always was for this module, and it is the one the operator named himself:
**the surface has grown to a size that one person can no longer hold, and the growth
is invisible because each addition was individually justified.**

### 6.1 `falls short` / `over-builds` — the same finding, and it is new

**Measured (this session, method in §4.0): 25 command words the dispatch chain
accepts, 22 of them registered as top-level verb constants, 45 CLI source files
carrying 26,527 production lines, 104 CLI test files, and — inside the single family
word `task` — 29 accepted verbs of which only 17 are advertised.**

Read against C5's question (*"can one person hold this surface?"*,
`devdoc/requirements/00-method-and-standards.md:428`) the arithmetic is not close:
**25 + 29 + 9 + 2 + 2 = 67 distinct words** a user may type (top-level; the `task`
family; the `memory` family measured at 9; `web` at 2; `checkpoint` at 2), spread
over a 3-level hierarchy and 104 test files. Nothing here is individually wrong —
§4.1–§4.6 show each word was added for a reason, and the operator's own shrink
rulings (t-1788037892/9140/9618) are the proof that this surface is *maintained*, not
abandoned. But maintenance by removal has not kept pace with addition, and the
clearest single measurement of that is **§6.2's asymmetry**.

**The finding that is new to the operator** — this one is not a restatement of any
ledger row, and it is the one I would put first:

> **The surface has three answers to "what can I type?", and the two a user meets
> are not the same.**
>
> Measured on the delivered binary and by set difference (not by count equality):
>
> - the **dispatch chain** accepts **25** words, and the **help index** advertises
>   exactly those **25** — a member-by-member difference comes back **empty in both
>   directions**, so the index is faithful to dispatch and this half is *served*;
> - the **Tab-completion candidate set** offers **22**
>   (`cmd/agent/cli_completion.go:97`, `topLevelCommandNames`, 22 entries) —
>   **three dispatched and advertised words are not completable at all: `act`,
>   `raw`, `stats`** (set difference, this session).
>
> So a user who has learned the surface the way shells are learned — by pressing Tab
> — has never seen three words the machine will act on and does show in its help.
> The disagreement is **never checked**, and the sharpest way to say it is that the
> completion gate **states the requirement and then checks something weaker**: its
> own opening comment reads *"Every verb the dispatch chain recognizes must be
> completable"* (`cmd/agent/cli_completion_test.go:21`), while the list it checks is a
> hand-written 21 words (`want`, `cmd/agent/cli_completion_test.go:22`) that is **not
> derived from the dispatch chain or the registry** — it is compared
> **bidirectionally** against `topLevelCommandNames`
> (`cmd/agent/cli_completion.go:97`, 22 entries = those 21 plus `completion` itself),
> so the gate is genuinely strict *about a pair of lists that are both beside the
> registry*. `act`, `raw` and `stats` are absent from **both** sides, so the gate
> passes while its own stated rule is violated. **The gap is not a missing word; it is
> that the surface has three self-descriptions of itself (chain, index, completion),
> each maintained beside the others, and no single check that all three agree.**
>
> **Weakest link, stated up front:** the completion set is a separate table, and I read
> its 22 entries from the source list. The delivered script *is* built from that same
> table — the test asserts the emitted script contains
> `compgen -W "<join of topLevelCommandNames>"` (`cmd/agent/cli_completion_test.go:217`)
> — so the three omissions propagate to the delivered completions and this is a
> user-visible hole, not a bookkeeping one. What I did **not** do is run the generated
> script in a live shell and watch Tab fail to offer them (see "what I could not determine", item 2).

### 6.2 `falls short` — the `task` family teaches less than it accepts, and the asymmetry is deliberate but unmeasured

Measured (§4.0 rows E–G): **29 `gopi task` verbs are accepted; 17 are advertised; 12
are accepted but hidden** — `adopt`, `audit-merges`, `diff`, `harvest`,
`merge`, `merge-execute`, `merged-cleanup`, `merger`, `push`, `run`, `serve`,
`sync`. The hiding is *designed*: `internal/taskverbs/verbs.go:223` (`RemovedVerbs`)
and the group comments at `:242`/`:260`/`:278` explain that the service atoms and the
sync words have their documentation home on another page (`gopi merger -h`,
`gopi task sync -h`), that their **spelling is not deprecated — only its
documentation home moved**, and `TestNoServiceOrSyncVerbInLedgerGroup`
(`cmd/agent/cli_tasks_advert_gate_test.go:148`) is the gate that keeps them out of
the ledger page.

**The gap is not the design, it is the ratio and the *absence of an entry point*.** A
user who wants the sync half must know that `gopi task sync -h` is the page that
teaches `diff`/`harvest`/`push`/`sync`; nothing on the `task` page need tell him,
because the rule is that the ledger page teaches no service/sync word. **Hidden but
live is the worst pairing for a one-person surface: the word works, so the machine
never corrects him, and the word is undocumented, so he cannot discover it.**
`TestSyncVerbsHaveExactlyOneHeadHome` (`cmd/agent/cli_tasks_advert_gate_test.go:318`)
guarantees there *is* a home — it does not guarantee a user can *reach* it from where
he is standing.

**What would close it (stated as a need, not a design):** *when I type the group
word, the machine's answer should let me discover every word the group accepts, even
if the detailed page for some of them lives elsewhere — a pointer is enough, silence
is not.* This is the same principle the operator already applied to *semantics*
("a removal must explain itself", §4.6) applied to *discovery*.

### 6.3 `serves` — the drift guards, and the one asymmetry in them

The two halves of the guard (§4.5) are the module's strongest evidence that the need
is real and met. What they cover, precisely, so the coverage is not overstated: the
merge-time guard catches **exactly one** failure mode — the dispatch chain and the
contract suite's predicate mirror disagreeing — by its own words (*"WHAT IT IS NOT: a
build or a test run … Compile errors, unrelated red tests, and inventory-fingerprint
drift need the full build/test gate this package deliberately does not implement"*,
`internal/clicontract/clicontract.go:20`). It reads two files by `git show`, computes
from text alone, and **abstains rather than refuses** when a file moves. That is a
good, honest boundary — and it is a *narrow* one: the completion set (22) and the
help index (25) are **not** in its scope, which is precisely §6.1's unnamed hole.

### 6.4 `never decided` — who owns the surface's size?

There is **no requirement anywhere that bounds the surface**, and no owner for its
growth. The evidence is the asymmetry between add and remove:

- **Addition has a procedure** — a word arrives with a registry constant, a
  predicate, an index row, a completion entry, and (since 2026-09-13) a derivation
  from one source; four commits in two days encode it
  (`b243ce4a`, `657046b3`, `9a81e398`, `a5429a3b`; see the log of
  `cmd/agent/cli_verbs.go` and `internal/taskverbs/verbs.go`).
- **Removal has a procedure too** — it must keep a dispatch case that explains
  itself (§4.6). But **nothing states a budget**, and nothing asks, at the moment a
  capability is added, whether the *surface* can afford it. The operator has done
  this by hand, three times, reactively (`t-1788037892`, `t-1788039140`,
  `t-1788039618`).
- The nearest thing to a ceiling is a **line-width** budget, not a word budget:
  `commandIndexMinCol = 28` and the 88-column wrap
  (`internal/config/help.go:99`, `cmd/agent/usage_test.go:92`). The surface is
  bounded in *columns*, never in *concepts*.

**This is the module's `never decided` finding**, and it is the direct consequence
of C6's premise: the project can now say what each word is FOR, and it has never said
**how many words it can afford**. A missing requirement is not a missing feature —
it is the reason a one-person surface grows until it hurts, and the reason the
evidence of the hurt (§6.1's lagging third view of the surface) was never noticed.

### 6.5 Conflicts with the binding constraints, recorded

Each requirement above was checked against C1–C6
(`devdoc/requirements/00-method-and-standards.md:377`–`:446`); two conflicts are
real and belong here rather than in a footnote.

- **A verb-per-capability surface fights C5 (small team, one person can run it).**
  §4.1's need — "I can reach the new capability by typing a word" — is what C2 asks
  for, and each instance is cheap; the *sum* is what C5 forbids. This is not
  resolvable by judging any single word. It is resolvable only by §6.4's missing
  requirement: a named budget, held by one person, enforced by a check. **Today the
  only enforcement is the operator noticing.**
- **A discovery word count conflicts with C1 (one static binary, zero deps) only in
  the direction of *opportunity*, not violation.** The surface is the one place where
  C1 pays off: all 67 words come from one binary — and, measured, its module graph
  carries **three** direct requires (`go mod edit -json`: `modernc.org/sqlite`,
  `github.com/google/uuid`, `golang.org/x/sys`), not one. The
  consequence worth recording is the *inverse* of a violation: because there is no
  plugin architecture, a capability that exists cannot be "an extension" — **it must
  become another word**, which is the mechanism by which §6.1's count grew. The
  constraint and the defect share a root.

### 6.6 `serves`, and the shrink side is real

Stated so the picture is not one-sided: the requirement *is* met on the loop's
critical path. The words for submitting, running, watching, deciding, merging,
verifying, listing, showing and inspecting all exist and are all in the index
(`§4.0` rows C and D — **measured equal as sets, difference empty in both
directions**, so every word the chain accepts is advertised and nothing advertised is
dead); the runner's two faces are separated by *who may start them*
(`cmd/agent/main.go:196`), which is the operator's own ruling
(`01a09260-f5c8-72a6-98b0-ba1f6846a08f`); the retired-spelling channel works
(`internal/taskverbs/verbs.go:223`, the family's own `run`); and the naming rule's
exceptions are contractual rather than accidental
(`cmd/agent/cli_completion_exceptions_test.go:94`). **The gap is not competence; it
is the absence of a size owner (§6.4) and of one check that all the surface's
self-describing views agree (§6.1).**

---

## 7. Implications for tests (naming existing tests; writing none)

**No test is written by this document** (C6's discipline: the doc is the deliverable).
What follows is the verdict a requirement above passes down on an existing test —
which test, and in which direction its verdict moves. Several of the verdicts are
*unchanged* on purpose: a doctest that survives a requirement is evidence the
requirement was already implemented.

| # | Existing test (`file:line`) | Verdict under this requirement |
|---|---|---|
| 1 | `cmd/agent/usage_test.go:58` `TestTopLevelUsageAdvertisesEveryDispatchedVerb` | **Unchanged, and load-bearing.** It is the machine half of §4.2 and §5.2. §6.1 does **not** weaken it — it shows it guards *one* of the three views. |
| 2 | `cmd/agent/cli_completion_test.go:20` `TestCompletionCandidateSetsCoverTopLevel` | **Should be strengthened.** Its own comment states *"Every verb the dispatch chain recognizes must be completable"*, but it checks a hand-written 21-word list (`:22`) bidirectionally against the completion table — so both sides omit `act`/`raw`/`stats` and the gate passes while its stated rule is broken. Under §6.1, the list must be **derived from the dispatch chain or the registry** (`cli_verbs.go`), which is what makes those three a red test instead of an unnoticed hole. |
| 3 | `cmd/agent/usage_test.go:19` `TestTopLevelUsageListsSubcommands` | **Verdict narrows:** it asserts the index *lists* subcommands; it does not assert the index agrees with completion or with the registry. Under §6.1 it is insufficient on its own. |
| 4 | `cmd/agent/usage_test.go:92` `TestTopLevelUsageIsAlignedAndTerse` | **Unchanged, and it is the *only* budget in the surface** (§6.4). Its scope is columns (`internal/config/help.go:99`), not concepts — the reason §6.4 is `never decided`. |
| 5 | `cmd/agent/cli_contracts_test.go:290` `TestCLIContractInventoryMatchesDispatch` | **Unchanged.** §4.5's test-time half; it is what caught the `voice_input` incident. |
| 6 | `cmd/agent/cli_contracts_test.go:363` `…EachSubcommandRoutesToItsHandler` | **Unchanged.** §5.9/§5.10 — the contract that a word reaches its own handler. |
| 7 | `cmd/agent/cli_contracts_test.go:776` `TestCLIContractLegacySpellingsStillDispatch` | **Unchanged, and its exit-code tolerance is the requirement, not a weakness.** §4.6: a retired word that *refuses with a pointer* is passing. |
| 8 | `cmd/agent/cli_contracts_test.go:651` `…TaskServeAliasPrintsOneLinePointer` | **Unchanged.** §4.3 — one concept, and the alias says where the real page is. |
| 9 | `internal/clicontract/clicontract_test.go:147` `TestCompare_VoiceInputIncidentIsCaught` | **Unchanged, and it is the module's precedent test**: it replays the real incident against two real chain shapes. §6.1's hole is exactly what this test *cannot* see (it parses two files, not the completion table). |
| 10 | `internal/clicontract/clicontract_test.go:176` `TestCompare_FixedBranchIsClean` | **Unchanged.** The negative half — the corrected branch must pass, or the gate would be unusable. |
| 11 | `cmd/agent/cli_tasks_advert_gate_test.go:148` `TestNoServiceOrSyncVerbInLedgerGroup` | **Unchanged for its own rule; the requirement it serves is incomplete.** It enforces §6.2's hiding; §6.2 asks for a *pointer* from the ledger page for reachability, which no test asserts. A test asserting the pointer would be the §6.2 deliverable. |
| 12 | `cmd/agent/cli_tasks_advert_gate_test.go:318` `TestSyncVerbsHaveExactlyOneHeadHome` | **Unchanged** — and note what it guarantees: one *home*, not one *discoverable path* (§6.2). |
| 13 | `cmd/agent/cli_tasks_advert_gate_test.go:353` `TestNoHelpTextIsDuplicatedAcrossPages` | **Unchanged.** It is the mechanism of §4.3's "one home", and it is why §6.2's pointer must be a *pointer*, not a copy. |
| 14 | `cmd/agent/cli_completion_exceptions_test.go:94` `TestCLINamingExceptionsDocumentedInTopLevelHelp` | **Unchanged, and it is §4.3's enforcement**: three underscore words, each with a reason, a dash-twin that must NOT dispatch, and a constant behind it. |
| 15 | `cmd/agent/cli_toolnames_contract_test.go:88` `TestCLIToolMirrorVerbsMatchConstants` | **Unchanged.** §2's agent-facing need: the CLI spelling **is** the wire name, which is what keeps a session log a lookup key. |
| 16 | `cmd/agent/cli_toolnames_contract_test.go:134` `TestCLINonToolSurfaceTokensDoNotOverlapTools` | **Unchanged** — the guard against a machine word colliding with a tool name, which would make §4.3's two-kind table unenforceable. |
| 17 | `internal/taskverbs/verbs_test.go:186` `TestDispatchVerbsAreDerived` | **Unchanged, and it is the size's single source**: §4.0 row E (29) is a *derived* number, so it cannot drift from the groups. |
| 18 | `internal/taskverbs/verbs_test.go:223` `TestRemovedVerbsAreNotAdvertised` | **Unchanged.** §4.6's split. |
| 19 | `internal/taskverbs/verbs_test.go:257` `TestServeAliasIsNotFamilySurface` | **Unchanged.** §4.3 at the sub-verb level. |
| 20 | `cmd/agent/cli_completion_test.go:82` `TestTaskCompletionNamesMatchDispatch` | **Unchanged** — the family's completion set is derived from `taskverbs.FamilyVerbs` (`cmd/agent/cli_completion.go:151`), which is the *correct* pattern and the contrast to row 2's hand-written list. |
| 21 | `cmd/agent/cli_completion_test.go:316` `TestTaskCompletionNamesAreTheSharedFamilySurface` | **Unchanged.** §4.3: the family surface is one object, shared. |
| 22 | `cmd/agent/help_capability_first_test.go:106` / `:125` / `:485` | **Unchanged, and they are §5.6's enforcement** — capability first, detail last, with a non-blindness proof for the predicate. |
| 23 | `cmd/agent/worker_run_entry_test.go:58` `TestTaskRunIsRetiredAndPointsAtTheWorker` | **Unchanged** — the retired runner word names its replacement (§4.6). |

**The one test this document would *change the verdict of*, if the requirement were
adopted:** row 2. Under §6.1's requirement (all three self-describing views agree, and
each is derived from the registry), `TestCompletionCandidateSetsCoverTopLevel`'s
hand-written `want` list becomes the defect it is meant to catch, and `act`, `raw`
and `stats` become **red** until they are either completed or consciously excluded
with a reason — the same choice the naming rule already makes explicit for its three
exceptions.

---

## 8. Confidence and limits

**Confidence and weakest link per claim.** Everything in §4.0 is **measured in this
session** and reproducible from the script printed there; the ledger rows quoted in
§4.3–§4.4 were re-queried live at write time (1,909 rows at the census, 1,910
at close-out, 609 with
`source='human'` — the operator's own voice). What follows is what is *not* that
solid.

1. **The three-way comparison in §6.1 is measured member-by-member, and the propagation
   to the delivered script is read, not run.** *Confidence in the measurement: high*
   (chain 25 = index 25, difference empty in both directions; completion 22; the three
   missing words `act` / `raw` / `stats` by set difference — all obtained this session
   on the delivered binary; and the emitted `compgen -W` line is assembled from
   `topLevelCommandNames`, `cmd/agent/cli_completion_test.go:217`). *Weakest link:*
   **I did not source the generated completion script in a live shell** and watch the
   three words fail to complete — the propagation is read off the generator's source,
   which is why it is high-confidence rather than proven. The check for *whether a
   fourth view exists* (any other place that enumerates the surface) also did not
   happen.
2. **The 67-word total in §6.1 is a sum I performed, not a number the code prints.**
   *Confidence: high for each part, medium for the sum's meaning.* *Weakest link:*
   whether a user "holds" words or *concepts* is a judgement; 25 top-level words plus
   29 family words is not the same cognitive load as 67 peers, because the family
   words sit behind one word. The sum is stated as a measurement, and the *judgement*
   against C5 is explicitly mine — an operator reading it should adjust it, not adopt
   it.
3. **The claim that every accepted word is advertised (§6.6) is now a set difference,
   not a count coincidence.** *Confidence: high (after correction).* *Weakest link:*
   my **first** check compared the two numbers (both 25) instead of comparing the two
   sets — a same-size, different-membership pair would have passed it, and I flagged
   that risk in the draft rather than fixing it. It is now fixed: the comparison was
   re-run member by member and the two directions come back **empty**
   (`accepted − index = ∅`, `index − accepted = ∅`). Recording the near-miss is
   deliberate — it is the same class of error this whole module is about, and the
   first answer to §8.3 was "medium confidence", which was the honest reading of the
   weaker check.
4. **§6.4 ("the surface's size is unowned") is an inference from absence.**
   *Confidence: medium-high.* *Weakest link:* proving a requirement was never decided
   requires searching for it, and I searched the places a budget would live (the help
   renderer's column budgets, the naming rule, the family's group comments, the
   regression tests) rather than the whole corpus. A budget stated in a briefing or a
   devdoc I did not open would falsify it. The absence is consistent with the
   operator's three reactive shrink rulings, which is corroboration, not proof.
5. **The sibling-row situation (§4.0's front matter).** Two ledger rows name this
   deliverable. Rows `01a0ab12-5b52-7404-8b27-9e0bdac8bdd2` (`in_progress`) and
   `01a0ab3e-94f9-7bb4-bf34-767ae30add9e` (`completed`) are **both `source=llm`** —
   dispatch briefings, not operator rows — so neither is evidence of a *user* need,
   and I have not cited either as such. `git log --all --oneline --diff-filter=A --
   'devdoc/requirements/cli.md'` is empty (no branch ever produced it). **The
   consequence for a reader:** this document is the first to exist, so it is not a
   revision of anything, and where it disagrees with the earlier briefing (which
   asserted "65 verbs") the disagreement is recorded in §4.0 with both methods named.
6. **The retired-word channel is verified by reading, not by running.** *Confidence:
   high.* The tests are cited (`cmd/agent/cli_contracts_test.go:776`) and their shape
   read, but **this session did not run the CLI contract suite** — the deliverable is
   docs-only and this document makes no claim that a suite is green. Where it says a
   guard *exists*, the citation is the guard's own source, which is what "exists"
   requires.

### What I could not determine

1. **Whether the operator would accept 25 top-level words as "holdable".** He has
   never stated a number, and the only thresholds I found are column budgets. §6.1's
   judgement is mine; **only he can close it**, which is why it is stated as a
   question with a command attached rather than as a verdict.
2. **Whether the three missing completion words are an oversight or a decision.**
   Three words (`act`, `raw`, `stats`) dispatch, appear in the index, and are absent
   from the completion candidate set. Nothing in the code, the tests, or the ledger
   I read records a reason — and the completion test's `want` list is hand-written,
   so an omission there is invisible by construction. **It is equally consistent with
   "forgotten" and "deliberately not completed"**, and I will not guess.
3. **What the surface actually costs in *use*, as opposed to in size.** The ledger
   has ~1,910 rows and 609 operator rows, and the session logs are on disk, but
   attributing "the operator used word X N times" needs a parse of the bash
   invocations inside session logs, not the ledger. I ran a **100-file sample** (not
   an audit) which found `gopi task` in 54 of 100 logs and, among the `task` verbs,
   only 10 distinct ones used (`list` 99, `show` 74, `for-branch` 18, `submit` 16,
   then a long tail) — that sample says the *heavily used* surface is much smaller
   than 67, but a 100-file sample of a live, mixed host is **not** a measurement of
   his habits, and I have not presented it as one. A real usage audit is its own
   task.
4. **Whether the merge-time guard is wired into every merge path.** The package's own
   doc states its purpose (`internal/clicontract/clicontract.go:1`) and I read its
   callers' contract, but I did not trace the merger's pass to confirm every merge
   route consults it. A guard that exists and is not called is the exact scenario the
   other requirements in this document exist to prevent.
5. **Whether any of §6.2's 12 hidden-but-live `task` words has a *second*
   documentation home** that I did not find. I read the three group comment blocks and
   the two advert gates; I did not read every help page in `internal/config/help.go`
   line by line to prove a pointer exists *somewhere* for each of the 12.

---

## C. Constraint section — the operator's stated positive vision applied to this module

The vision is binding, not background (`devdoc/requirements/00-method-and-standards.md:359`,
constraints C1–C6). Applied to the CLI surface, each one has teeth here:

- **C1 — one static binary, zero dependencies, no plugins.** The surface *is* the
  extension mechanism, by construction: with no plugin architecture, a capability
  that cannot be a plugin must become **a word**. Measured consequence: `cmd/agent`
  carries **80** production files and **38,519** lines, of which **45 files /
  26,527 lines** are the CLI surface — 69% of the command package's production code is
  the surface, and it all compiles into the one binary. **This constraint is not
  violated by the gap in §6.1; it is the reason for it** (§6.5).
- **C2 — the operator must control the whole thing.** This is the module's spine and
  the source of §4.1's requirement. The citations are the operator's own words in
  `briefings/brief-requirements-reverse-analysis.md:128` and the close-loop row
  `t-1788383728`; the *engineered* half is one dispatch chain (`cmd/agent/main.go:554`)
  plus the registry (`cmd/agent/cli_verbs.go:7`). **Note the honesty requirement the
  method attaches to this constraint** (`devdoc/requirements/00-method-and-standards.md:395`):
  the phrase *"one place he understands"* is an **agent's paraphrase inside a briefing**,
  not the operator's words. It appears in this document only inside the umbrella's own
  boundary line (§ front matter) and is never cited as evidence.
- **C3 — a daemon must be simple; every branch multiplies its failure surface.** The
  surface's shape obeys this twice. First, the never-returning words are *separated
  from* the read/write words instead of folding loop behaviour into them
  (`cmd/agent/cli_tasks_worker.go:254`: the explicit-row face is the *same* round body
  and drops only the loop — the comment names the retired duplicate handling
  explicitly). Second, this is the operator's stated reason for keeping the runner
  un-daemonised (row `01a09260`: *"worker 我就不想让它跑 daemon"*). **The gap §6.1 must
  not be closed by adding words to a daemon.**
- **C4 — the agent must clean up after itself.** In this module the constraint appears
  in the *command* contract rather than in a sweep: a word that writes is
  distinguishable from a word that reads (§5.9), and the surface's own naming does not
  pretend otherwise. The behavioural half is other modules' (§3's exit condition names
  the rebuild step the merge surface opens — spelled `gopi merge` when this was
  written, removed as a top-level verb on 2026-11-01; the face is `gopi task
  merge` today).
- **C5 — a small team; solutions one person can run.** The named measurable symptom is
  the CLI surface, and this is where this document is *supposed* to hurt. Measured:
  25 top-level words, 29 family words, 9 memory sub-words, 104 test files, 26,527
  surface lines. The umbrella's own warning applies verbatim
  (`devdoc/requirements/00-method-and-standards.md:428`): **a count of verbs must count
  the registry and the predicates, not string literals** — which is exactly why §4.0
  prints its method and rejects the 65/53 figures in circulation.
- **C6 — `why` over `what`.** This is the constraint that makes the module's *own*
  §7 load-bearing: `TestTopLevelUsageAdvertisesEveryDispatchedVerb`
  (`cmd/agent/usage_test.go:58`) and
  `TestNoServiceOrSyncVerbInLedgerGroup`
  (`cmd/agent/cli_tasks_advert_gate_test.go:148`) are not merely consistency checks —
  they assert that the machine's vocabulary tells the truth about what each word is
  for. §4.3's ruling (`01a0927b`) — *a word's position is a claim about what the thing
  is* — is the operator's own statement of C6 applied to a surface, and it is why §6.2
  (hidden but live) is framed as a `falls short` rather than as tidiness.

---

## Verification of this document (run as the last step)

- **Docs-only:** `git diff --name-only master..HEAD` must be exactly
  `devdoc/requirements/cli.md`. No code, no tests, no formatting.
- **Citations:** every `file:line` above was re-read on base `812f9a60` after
  drafting, and corrected where the draft was off. Every measured number in §4.0 was
  produced in this session by the printed script; the ledger counts were re-queried at
  write time.
- **Deliverable statement (Rule 6d):** branch `docs/req-cli-surface`, worktree
  `/home/user/workspace/gopi-req-cli`, base master `812f9a60`
  (`812f9a606b1814bf093f2189d70165828d59bbfe`). A worktree shares the main repo's
  refs, so the commit is already in the main repo; recover this file with
  `git show docs/req-cli-surface:devdoc/requirements/cli.md`.
- **Conflicts recorded, not resolved:** §6.5 (C2's verb-per-capability habit against
  C5's one-person bound) is left as a stated conflict with its owner named — the
  operator — rather than decided here.
