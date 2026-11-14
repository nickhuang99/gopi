<!-- doc-class: normative -->
# 99 — Summary: what gopi is for, which requirements are served, and where the code disagrees with the person who owns it

**Series:** `devdoc/requirements/` — reverse requirement analysis (the WHY reconstructed from the
built system, then gap-analysed). This is the closing document of 15.
**Base of record:** master `5cdae25e` ("adoption: merge docs/req-test-infrastructure (`65fd7d2f`)",
2026-09-18 20:44:56 +0800). The module documents pin *their own* revisions — `17cec86b`,
`812f9a60`, `9ce8e159` — and each is read here at the revision it pinned, never at master
(`devdoc/requirements/00-method-and-standards.md:571`–`:601`, the "pin the revision /
re-measure, never restate" rule).
**Method:** `devdoc/requirements/00-method-and-standards.md:359`–`:446` (the six constraints),
`:460`–`:571` (the evidence canon), and the 8-section shape at `:324`–`:353`. Module boundaries:
`devdoc/requirements/00-module-map.md`. Dispatch briefings:
`devdoc/requirements/01-per-module-briefings.md`.
**Acceptance instrument:** `devdoc/requirements/99-summary-gate.sh` — six checks (citation coverage
over every module document, citation resolution, the product-language predicate, the ranked gaps and
their citations, the disagreement instances and their citations, the presence of the unknowns list
and the pin list, and the six constraints). Run
`bash devdoc/requirements/99-summary-gate.sh devdoc/requirements/99-summary.md`; `--self-test`
mutates a copy and asserts each check goes red, because a green that cannot fail measures nothing
(the defect at `devdoc/requirements/09-chat-ui-verification-report.md:99`–`:118`).

**The 19 documents this summary reads and cites** — `00-method-and-standards.md`,
`00-module-map.md`, `01-per-module-briefings.md`, `01-task-ledger.md`, `02-merge-and-branches.md`,
`03-worktree-and-cwd.md`, `04-sync-and-nodes.md`, `05-context-and-memory.md`,
`06-agent-loop.md`, `07-tools-and-routing.md`, `09-chat-ui.md`,
`09-chat-ui-verification-report.md`, `11-build-and-repos.md`, `cli.md`, `merge-and-branches.md`,
`merge-branch-lifecycle.md`, `sync-and-nodes.md`, `test-infrastructure.md`, `README.md`.
(`devdoc/requirements/00-module-map.md:512` "What this map itself could not determine".)

**Honesty note, inherited and extended.** Everything in this series is *reconstructed*: gopi has no
requirements document, so a sentence here is an operator need only when it traces to an operator
row, a rule, a commit message or a read line. Where this summary reasons past its citations it says
`[inferred]` and names the weakest link; where it has no citation at all it says `[opinion]`
(`devdoc/requirements/00-method-and-standards.md:571`–`:601`, `devdoc/requirements/README.md:91`–`:97`).

---

## 1. What gopi is for

*(Product-owner language. No code identifiers, by construction — the series' own rule, restated at
`devdoc/requirements/00-method-and-standards.md:359`–`:446` and checked by the gate this document
ships with.)*

**The problem.** One person has more ideas, and more small pieces of work, than he has attention —
and he works on two machines, one of which he is usually not sitting at. A to-do note cannot carry a
piece of work to completion, a finished piece of work cannot reach the shared history by itself, and
the machine that did the work cannot tell him, later, why it did what it did. Existing tools answer
one of those questions each and refuse to talk to each other, so running them together costs more
attention than the work itself.

**What gopi is.** It is one program that this one person runs, and its subject is *the whole round
trip of his work*:

- **One durable list of everything he has handed over.** Filing an idea is cheap and requires no
  ceremony; the same entry is never picked up twice, including across his two machines; entries are
  never silently dropped; nothing is deleted, and a wrong entry is corrected by filing another one,
  so the trail of what he asked for survives every pass of the machinery
  (`devdoc/requirements/01-task-ledger.md:92`–`:130`, `:513`–`:540`).
- **Workers that act, rather than advise.** A piece of filed work is handed to a short-lived agent
  that can read files, write them, run commands, search the tree and the web, look at a picture of
  the screen and remember what it learned last time — and a run ends by *reporting what it did*,
  not by asking permission (`devdoc/requirements/06-agent-loop.md:368`–`:392`,
  `devdoc/requirements/07-tools-and-routing.md:662`–`:694`).
- **Unattended operation, honestly bounded.** The parts that must run while nobody is present are
  deliberately mechanical — they decide by asking whether a piece of work is already contained in
  the shared history, and when a decision needs judgement they hand it to a short-lived agent
  instead of growing a branch inside the watcher (`devdoc/requirements/02-merge-and-branches.md:123`–`:146`,
  `devdoc/requirements/merge-and-branches.md:78`–`:100`).
- **A finished piece of work arrives on the shared history by itself**, and a piece that *cannot*
  arrive is not left to rot: it is turned into a message he can act on, in the form of new work
  (`devdoc/requirements/merge-branch-lifecycle.md:68`–`:78`).
- **Two machines behave as one system, and each knows which one it is.** What he did on one shows
  up on the other; the finished article travels, not merely the note that says it is finished; and
  one machine is the centre that settles disagreements while the other reports to it
  (`devdoc/requirements/sync-and-nodes.md:68`–`:89`).
- **A memory that makes the next round cheaper than the last.** Distilled lessons from earlier work
  are retrieved when a later task needs them, so a solved problem is not re-solved; and inside a
  long run, the thread of the work survives even when the conversation is compressed
  (`devdoc/requirements/05-context-and-memory.md:392`–`:423`).
- **A promise that the writer cannot take back.** Rules the operator decided once — no unexplained
  numbers, no broken error chains, no leaked test state, a finished run leaving nothing behind —
  are re-checked by the machine on every build, so they survive the arrival of someone who has never
  spoken to him and has no memory of why
  (`devdoc/requirements/test-infrastructure.md:27`–`:54`).
- **Every project states how it is built, and the machine catches up by itself after work lands** —
  the alternative is the worst state a tool can be in, one that answers confidently from a past the
  repository has already left (`devdoc/requirements/11-build-and-repos.md:68`–`:86`).

**Why it is shaped this way, in the owner's own terms.** He wants to hold the whole thing, not to
integrate a federation of separate tools whose running-together is the cost
(`devdoc/requirements/00-method-and-standards.md:395`–`:412`, the constraint recorded as C2); the
unattended machinery must stay small, because every branch of logic it carries multiplies its
failure surface (`:414`–`:421`, C3); a finished run cleans up after itself, because leaving the
mess for the human is not an acceptable design (`:423`–`:434`, C4); this is a small team with
limited resources, so solutions a single person can run, cheap in attention and in compute, are the
requirement rather than the aspiration (`:436`–`:451`, C5); and the project must be able to say what
a thing is *for* (`:453`–`:465`, C6 — the premise of the very series that produced this document).

**And the first recorded need, one line, which is the best definition of the lot:** the work should
not care where it was filed or which machine runs it, and a finished result must reach the trunk the
other machine reads with nobody present — the operator's own framing is *"work finished on one
machine must land on the shared trunk with no human present"*
(`devdoc/requirements/merge-and-branches.md:125`–`:131`), with the row-level statement of it at
`devdoc/requirements/04-sync-and-nodes.md:225`–`:231` (R1: *"I file work on one machine and see it on
the other; a row must not be able to mean two different things on the two machines"*). Everything
above is an elaboration of those two sentences.

---

## 2. Which requirements are well served

Verdict per module, each traced to that module's own "serves" surface. A verdict here is a reading
of the module document's §6.1, not a new judgement.

| module doc | well served — the requirement, and the module's evidence for it |
|---|---|
| `devdoc/requirements/01-task-ledger.md:515`–`:540` | **Append-only by construction and immutability at the writer.** No delete path exists and a source-reading test forbids one; there is no summary SET clause on the update path (bug056's comment), and the queue's total order is a single ordering rule so two machines cannot double-run a row (`:523`–`:540`). |
| `devdoc/requirements/02-merge-and-branches.md:557`–`:581` | **The unattended merge decision is mechanical**, exactly as the requirement asks: landable or left alone, decided by ancestry, with judgement handed to a short-lived agent's prompt (`:123`–`:146`). |
| `devdoc/requirements/03-worktree-and-cwd.md:328`–`:342` | **Delivery before destruction, with three independent liveness judgements** — each reporting rather than acting — plus a machine-checked dirty classification table and a sweep that runs by default and escalates instead of destroying unowned leftovers. |
| `devdoc/requirements/04-sync-and-nodes.md:469`–`:493` | **Forward-only convergence with a data guard**: the rank ladder is data re-derived inside the update, so a race cannot clobber a newer state; role-gated behaviour is written end to end on the client. |
| `devdoc/requirements/05-context-and-memory.md:392`–`:423` | **The cross-run memory is served and actually used** — 3,774 distilled rows, 44,441 retrieval events, 77.9 % of rows returned to a model at least once — and the in-run half is defensive (trigger/target split, pinned-anchor refusal, cooldown, each with a regression test). |
| `devdoc/requirements/06-agent-loop.md:368`–`:393` | **A run that cannot progress stops itself**, and honest waiting is exempted by name rather than by luck; the run-level caps are environment-overridable with a documented unlimited value. |
| `devdoc/requirements/07-tools-and-routing.md:670`–`:694` | **The ability set is closed and countable**: an unknown name hits the dispatch switch's default, and the definition-to-dispatcher agreement is machine-checked. |
| `devdoc/requirements/09-chat-ui.md:442`–`:455` | **The conversation surface exists and is coherent**: one conversation is one session, one log and one index row; signals close a conversation instead of killing it; pasted blocks arrive as one message; piping degrades loudly instead of silently. |
| `devdoc/requirements/11-build-and-repos.md:327`–`:352` | **A repo that declares a build is built after a merge round, and a repo that declares nothing is left alone** — the declaration table is the contract, and no guess is made in its absence. |
| `devdoc/requirements/cli.md:520`–`:558` | **The command surface's engineering is sound**: the drift class is closed on both the test path and the merge path, the naming rule has machine-rechecked exceptions, and the retired-word channel works. |
| `devdoc/requirements/merge-and-branches.md:78`–`:100` | **Hand-over instead of heuristic**: where judgement is unavoidable it is put in a short-lived run's prompt, not in the daemon (R2 read as a requirement over the merger's shape). |
| `devdoc/requirements/merge-branch-lifecycle.md:190`–`:217` | **Cleanup is the machine's job, and it is attributable**: a park is a reference, never a delete, and every branch-related row is reachable from the branch's own name. |
| `devdoc/requirements/sync-and-nodes.md:378`–`:392` | **The three channels are separate because their units differ**, and the operator's on-demand correctness check exists as a verb rather than as a document. |
| `devdoc/requirements/test-infrastructure.md:267`–`:286` | **A decided standard survives the next agent's forgetfulness**: each gate lands green with a written, per-line debt list, and the allow-list can only shrink. |

**Two whole-project consequences, stated once.** (1) The *closure* properties are the strongest
thing this system has: closed ability set, closed status vocabulary, forward-only convergence,
append-only list, ancestry-only merge decision — and each is pinned by a test rather than by
intention (`devdoc/requirements/06-agent-loop.md:368`–`:393`,
`devdoc/requirements/01-task-ledger.md:515`–`:540`). (2) The *residue* requirement is where the
system is weakest across modules, and it fails in the same way in four independent places
(§3, gaps 1 and 5).

---

## 3. The biggest gaps, ranked

Ranked by (consequence for the operator) x (how plainly the module documents prove it). Each gap
carries the module evidence it rests on. Nothing here is a new finding; the ranking is this
document's contribution (`[opinion]`, the gap contents are cited).

### GAP-1 — Finished work does not reliably reach the shared history, and the run still says it did
*The single largest gap.* Three modules converge on it from three directions.

- The close-out chain's dirty gate refuses the **whole** sequence — including the harmless delivery
  — when the working copy is dirty, so a branch that never left the host looks exactly like a branch
  that had. The operator measured it: *"Three finished deliverables sat on HP for hours, unpushed,
  while their task rows said `completed`"*, with the dry run's own summary line `would-run=0`
  (`devdoc/requirements/03-worktree-and-cwd.md:345`–`:358`, ledger row `01a0aab8`).
- Merged into master since that reading, the pre-merge verification step now exists
  (`bf9fbef8`, 2026-09-17, "feat(client): pre-merge server master and verify the build before
  pushing a branch") — **but the entry gate still returns before the body runs**, and the body's
  copy of the same gate returns before the pre-merge step
  (`internal/tools/task_finish_cleanup.go:1130`–`:1149`, `:2151`–`:2168`, and the step at `:2204`–`:2237`).
  **This is this document's own verification, and it is new to the module documents**: the module
  doc's "G1b — the gate is duplicated, so the obvious repair does not repair it"
  (`devdoc/requirements/03-worktree-and-cwd.md:360`–`:373`) is still true of current master, *and*
  the repair branch it recommended is now an **archived loser**
  (`archive/fix/client-closeout-push-delivery`, the same branch the doc cites at `:370`–`:373`).
- The concurrency risk this creates is the other module's finding: delivery is *not one decision*
  but a sequence run by three components at close-out (the live process, the merger's tick, the
  reclaim path), and each reads status through its own gate
  (`devdoc/requirements/merge-and-branches.md:597`–`:638`, ND-1/§6.4).
- The same defect on the server arm did not exist: the dirty gate is the client arm's; the server
  arm's own comment says the chain refuses at the *entry* arm as well
  (`devdoc/requirements/03-worktree-and-cwd.md:360`).

**Why this is first.** It defeats C4 (*the agent cleans up after itself*) at the one moment that
matters — the end of the work — and it defeats it in the direction that loses work rather than
merely soiling a tree.

### GAP-2 — The centre of the two-machine topology does not know it is the centre
`devdoc/requirements/sync-and-nodes.md:393`–`:489`. Measured on the centre: its settings declare a
role nowhere, and the predicate that would infer it is dead code that can only return false, so the
server branch of the git channel is unreachable there — the centre takes the client path and then
skips it (`devdoc/requirements/04-sync-and-nodes.md:494`–`:537`). The omission is
self-concealing: the role banner is printed only when a role is declared, so the machine that
drifted prints *nothing* where the client prints an identity
(`devdoc/requirements/sync-and-nodes.md:395`–`:470`). Violates the module's R5 (identity declared,
not inferred) and R9 (the undeclared case must be visible). Second half, same module: the alarm the
requirement depends on (`ledger-conflict-human-task`) is not in the configured event allowlist on
either machine — re-measured here as
`{"events": "claimed,in_progress,completed,failed,stale,build-failed"}`
(`/home/user/.gopi/settings.json`), which matches what the doc measured, and the notify log
contains 0 occurrences (`devdoc/requirements/04-sync-and-nodes.md:536`–`:558`).

### GAP-3 — The word the operator reads as "it landed" means "the process stopped"
`devdoc/requirements/01-task-ledger.md:541`–`:570`. The decision is on record and deliberate, and
it is not a code bug: the terminal tier is one rank, and the row itself carries no information
about delivery — 529 completed rows with an empty reason and 782 with the default string, i.e.
1,311 of 1,376 completed rows say nothing (`:543`–`:570`). Three operator-reported incidents read it
as a delivery receipt; nobody ever decided which of the two the row is. This is the *same* failure
as GAP-1 seen from the reading side, which is why it ranks third rather than first: fixing the
channel without fixing the word leaves the operator unable to trust any row.

### GAP-4 — The advertised ability set and the runnable one were not the same set
`devdoc/requirements/06-agent-loop.md:607`–`:656`. The surface was closed and countable, and one of
its seventeen members had no dispatch arm: the model called it, the wire answered
`Unknown tool: screen_capture`, 30 times, and no test could see it because the definition-side gate
compared two lists that both contained the name (`:618`–`:634`). **Resolved on master during this
series**: commit `6f68a60c` (2026-09-18, "merge: fix/screen-capture-dispatch (screen_capture
dispatch arm + def↔dispatcher gate)") added the arm, and the case now exists
(`internal/agent/loop.go:5246`, read at `5cdae25e`). The gap is ranked anyway, because what it
proves is the *class*: a closure test that compares the definition against itself certifies a set
that is wrong by one (`devdoc/requirements/06-agent-loop.md:625`–`:634`).

### GAP-5 — Residue is unbounded, and each of the four families is invisible to its own sweeper
The C4 conflict, measured in three independent modules: the distilled store has no retention bound
(3,774 rows, 832 never retrieved, 22.0 %; the delete verb is per-record and nothing prunes —
`devdoc/requirements/05-context-and-memory.md:424`–`:475`, C4 row at `:842`); the run residue has a
deletion path and a refusal but no bound — 1,578 crash dumps, 1,522 orphaned, 378 structurally
unreachable by the sweeper whose candidate set comes from the session records themselves
(`devdoc/requirements/06-agent-loop.md:799`); the chat index carries rows nothing will ever close
(9 of 104 on this host; re-running the migration on a copy took 104 → 259 rows, 106 directory
pointers, 0 closed — `devdoc/requirements/09-chat-ui-verification-report.md:162`–`:167`); and
directories nobody registered are protected by nothing
(`devdoc/requirements/03-worktree-and-cwd.md:391`–`:397`).

### GAP-6 — The unattended machinery has grown work whose subject is not the merge
`devdoc/requirements/merge-and-branches.md:521`–`:596` (OB-1/OB-2): every tick runs four sweeps,
two of which the operator has already asked to leave the daemon. This is C3 (*every branch
multiplies the failure surface*) failing in exactly the place the operator wrote the rule about,
and the module's C3 check records it as the module's spine-conflict
(`devdoc/requirements/02-merge-and-branches.md:729`–`:755`).

**Two gaps the series deliberately does NOT put in this ranking**, because their evidence does not
yet decide them and overstating them would be worse than silence: **the chat surface has no
requirement statement anywhere** — its genesis is intact (a comparison with two other products, an
approval, a roadmap) and no row, rule or document says the operator *requires* it
(`devdoc/requirements/09-chat-ui.md:676`–`:700`, `:1146`–`:1155`); and **whether the in-session task
list is a user requirement or harness bookkeeping** (`devdoc/requirements/06-agent-loop.md:528`–`:565`,
`:749`–`:780`). Both are dispatchable questions, not rankings.

---

## 4. Where the operator's model and the code's model disagree

The operator's model, in his own words where the canon gives them, is the six constraints of
`devdoc/requirements/00-method-and-standards.md:359`–`:446` plus the stated positive vision each
module restates (`devdoc/requirements/merge-branch-lifecycle.md:581`–`:614`,
`devdoc/requirements/sync-and-nodes.md:538`–`:551`, `devdoc/requirements/cli.md:737`–`:800`,
`devdoc/requirements/test-infrastructure.md:545`–`:557`,
`devdoc/requirements/03-worktree-and-cwd.md:678`–`:692`). Concrete disagreements, each as
*(what he believes) vs (what the system does)*:

1. **"Clean up after yourself" vs "a dirty tree stops the delivery".** He asked whether the agent
   cleans up after itself (row `01a0975a`, quoted at
   `devdoc/requirements/00-method-and-standards.md:423`–`:430`); the system's answer is that the
   run's *own* residue refuses its *own* delivery
   (`devdoc/requirements/03-worktree-and-cwd.md:345`–`:358`, read again at master
   `internal/tools/task_finish_cleanup.go:1130`–`:1149`). Cleanup is a precondition because nobody
   bound it to the harmless steps.
2. **"Work should not care which machine" vs "the centre is a client by omission".** His line is
   that the worker does not care about machine A or machine B
   (`devdoc/requirements/03-worktree-and-cwd.md:462`–`:466`); the code's line is that a machine's
   role exists only as a declared string, so an undeclaring machine is permanently a client
   (`devdoc/requirements/sync-and-nodes.md:395`–`:470`).
3. **"One list I own and can re-read" vs "completed does not mean landed".** He reads the list as a
   record of what happened (`devdoc/requirements/01-task-ledger.md:513`–`:540`); the code's word for
   it is a stop-order (`:541`–`:570`).
4. **"I can see and reverse everything" vs two alarms he never receives.** C2 is on record
   (`devdoc/requirements/00-method-and-standards.md:395`–`:412`); the run-level alarms
   `loop-stuck` and `session-wallclock-exceeded` are emitted
   (`internal/agent/loop.go:312`, `:304`) and dropped by his own allowlist
   (`/home/user/.gopi/settings.json`, `events: claimed,in_progress,completed,failed,stale,build-failed`;
   `devdoc/requirements/06-agent-loop.md:797`).
5. **"Nothing lands by accident; the build gate decides" vs the gate abstains.** The requirement is
   that what lands works (`devdoc/requirements/merge-branch-lifecycle.md:255`–`:264`); the built
   gate merges unchanged when both master and branch fail to build
   (`devdoc/requirements/merge-branch-lifecycle.md:375`–`:386`).
6. **"Everything must be observable" vs a message delivered as a log line.** A delivery that never
   happened produced no durable record; the module found the requirement only in an *unmerged*
   repair branch, and the branch's own header names it
   (`devdoc/requirements/03-worktree-and-cwd.md:365`–`:378`, archived now at
   `archive/fix/client-closeout-push-delivery`).
7. **"A rule is a rule" vs "the gate abstains on a changed tree".** The operator's rules are
   enforced by machine referees (`devdoc/requirements/test-infrastructure.md:27`–`:54`); one gate
   must abstain in the face of correct behaviour, and the abstention is carried by a test rather
   than by the gate (`devdoc/requirements/test-infrastructure.md:458`–`:478`).
8. **"Measured, not remembered" vs a series whose earlier version was wrong about its own sizes.**
   The method requires re-measurement (`devdoc/requirements/00-method-and-standards.md:590`–`:601`),
   and the method's own author had to withdraw a figure — 47 command files was 45; 29 rules were 30;
   and one module's headline measurement had been taken from a substituted path rather than from the
   shipped function (`devdoc/requirements/09-chat-ui-verification-report.md:143`–`:152`).
9. **"A tool adoption standard" vs an unnamed zero-use tier.** Rule 11's standard is that a tool's
   value is that a model uses it (`devdoc/requirements/00-method-and-standards.md:436`–`:451`);
   re-measurement moved the tier rather than closing the question — four tools stand at zero in the
   sampled window (`devdoc/requirements/README.md:52`–`:63`).

**The single sharpest disagreement — stated as one sentence.** *The operator believes a finished run
delivers its work and reports honestly; the system's own gates can refuse the delivery on the run's
own residue, silently, while the durable record reads `completed` — so the one moment the whole
design exists for, work reaching the shared history while nobody watches, is the moment the model
and the code part company.* Evidence: `devdoc/requirements/03-worktree-and-cwd.md:345`–`:358`
(the operator's three deliverables stalled, `would-run=0`), `devdoc/requirements/01-task-ledger.md:541`–`:570`
(1,311 of 1,376 completed rows carry no delivery information), and the still-standing entry gate at
master `internal/tools/task_finish_cleanup.go:1130`–`:1149`.

---

## 5. What this series could not determine — consolidated

Every module doc ends with its own unknowns list; this is the union, deduplicated, grouped by the
kind of question it is. **A question here is a dispatchable item, not a gap this document filled by
guessing** (the series' rule, `devdoc/requirements/00-method-and-standards.md:107`–`:120`).

**A. About the operator's own intent (only he can close these).**
- Why the 2026-09-16 requirements series was cancelled — three cancelled rows, zero documents, no
  note anywhere (`devdoc/requirements/README.md:22`–`:27`, `:66`–`:80`).
- Whether the chat surface is a requirement he holds, or an experiment that worked
  (`devdoc/requirements/09-chat-ui.md:1146`–`:1155`; `:676`–`:700`).
- Whether the in-session task list is a user requirement or harness bookkeeping, and which of its
  two implementations he would keep if forced to one
  (`devdoc/requirements/06-agent-loop.md:749`–`:780`).
- Whether 25 top-level command words are "holdable" — he has never stated a number
  (`devdoc/requirements/cli.md:703`–`:712`).
- Whether `completed` is allowed to mean "the process stopped"
  (`devdoc/requirements/01-task-ledger.md:1049`–`:1060`).
- Whether the two close-out arms' destructive asymmetry is a decision or an accident
  (`devdoc/requirements/03-worktree-and-cwd.md:693`–`:700`).
- Whether the web search/fetch tools' Python tree is a sanctioned exception or a wart
  (`devdoc/requirements/07-tools-and-routing.md:1456`–`:1470`; the C1 conflict at
  `devdoc/requirements/00-method-and-standards.md:398`–`:412`).

**B. About the two machines (needs a two-host measurement).**
- What removed the centre's rows — a real removal with no removal path found
  (`devdoc/requirements/sync-and-nodes.md:780`–`:790`).
- Whether the duplicated anchor rows for one machine ever caused a *lost* delta
  (`devdoc/requirements/sync-and-nodes.md:782`–`:800`) — and whether the centre ever declared a role
  and lost it (`devdoc/requirements/04-sync-and-nodes.md:1050`–`:1060`).
- The intended peer cardinality: Rule 28 is written for two hosts, the code loops over N
  (`devdoc/requirements/04-sync-and-nodes.md:812`–`:830`).
- The cause of the chat index's per-host divergence (104 rows here, 116 there, re-running the
  migration 104 → 259) (`devdoc/requirements/09-chat-ui-verification-report.md:162`–`:167`).
- What the second declared repository in the live settings is for
  (`devdoc/requirements/sync-and-nodes.md:800`–`:815`).

**C. About the evidence canon itself (bounds every claim above).**
- Whether `source=human` rows are the operator's typed words or a transcription — no author field,
  empty model; named by two modules as *the largest interpretation risk*
  (`devdoc/requirements/merge-branch-lifecycle.md:806`–`:810`,
  `devdoc/requirements/02-merge-and-branches.md:959`–`:970`).
- Whether the operator's vision has a single canonical statement, or only six recovered rows
  (`devdoc/requirements/00-method-and-standards.md:859`–`:875`).

**D. About mechanisms whose effect is unknown.**
- Whether the memory organ is meant to supersede the tool's search path or coexist with it
  (`devdoc/requirements/05-context-and-memory.md:846`–`:860`).
- Whether the 5W1H/exam columns were retired deliberately or regressed silently
  (`devdoc/requirements/05-context-and-memory.md:877`–`:881`).
- Why the 2026-08-24 rows were demoted, and whether the cold-tier retrieval counts happened before
  or after demotion (`devdoc/requirements/05-context-and-memory.md:846`–`:890`).
- What consumes the heavy-skip ledger, and whether unit coverage is still thin
  (`devdoc/requirements/test-infrastructure.md:618`–`:625`).
- Whether the chat index's reader has ever surfaced its silent failure
  (`devdoc/requirements/09-chat-ui-verification-report.md:169`–`:175`).

**E. Two questions that no module owns.** Whether `web/` is a first-class user surface
(`devdoc/requirements/README.md:72`–`:75`), and how much of the session directory tree is live value
(`:76`–`:80`).

---

## 6. What the test-writing phase should pin first

**No test is written by this document** (the epic's constraint; the same restraint as
`devdoc/requirements/09-chat-ui.md:717`–`:760`). These are the highest-value pins, in order, each
with the module evidence that makes it the right first pin and with the direction the requirement
demands. A pin's value is that it makes a *silent* disagreement loud.

1. **The delivery decision is one decision, and residue cannot precede it.** Pin: on a client node,
   a terminal row whose branch is deliverable and whose working copy carries only *untracked*
   residue must still **push**; the pre-merge verification and the push are attempted before any
   removal gate can return; and the gate cannot be satisfied by deleting the entry-arm copy alone.
   Evidence and direction: `devdoc/requirements/03-worktree-and-cwd.md:345`–`:373`
   (G1 + G1b: "the entry-arm gate must be deleted or merged with the body's, or the fix is defeated
   at the door"), requirement R5 in `devdoc/requirements/merge-branch-lifecycle.md:199`–`:208`, and
   this document's own re-read of master (`internal/tools/task_finish_cleanup.go:1130`–`:1149`).
   Existing neighbours that must keep passing: `TestFinishCleanupChainPushFailKeepsBranch`,
   `TestFinishCleanupIdempotentPush` (`internal/tools/task_finish_cleanup_test.go`).
2. **The record does not lie about delivery.** Pin: a run that stops at an environment bound, and a
   run whose delivery was refused, are distinguishable *in the durable record* from a declared
   completion. Evidence: `devdoc/requirements/01-task-ledger.md:541`–`:570` (G1; 1,311 of 1,376),
   `devdoc/requirements/06-agent-loop.md:394`–`:410` (§6.2.1: the ceiling-stopped run is
   indistinguishable), requirement R11 at `devdoc/requirements/merge-and-branches.md:349`–`:357`.
3. **A run leaves its tree clean, and the gate that says so is checked, not trusted.** Pin the two
   opposite remedies — tracked changes refuse the `completed` writeback, untracked litter is
   reported (and refuses only under the opt-in) — and pin that a *foreign* live row or a
   non-attributable worktree is never the refusal. Evidence: `devdoc/requirements/03-worktree-and-cwd.md:345`–`:358`,
   Rule 30's own sentence (`GOPI.md` Rule 30; `devdoc/requirements/test-infrastructure.md:234`–`:241`),
   and the reality that this family already ships tests
   (`TestDirtyCloseout`, `internal/tools/dirty_closeout_binding_test.go`, added by `61b29de9`).
4. **Identity is declared, and the undeclared case is visible.** Pin: a node with no declared role
   must not silently take the client path; the missing declaration must be observable (the banner
   that is only printed when a role exists is the wrong shape for a fail-visible rule). Evidence:
   `devdoc/requirements/sync-and-nodes.md:395`–`:470`, `:472`–`:489` (G1/G2), R5/R9 at
   `devdoc/requirements/04-sync-and-nodes.md:295`–`:302`, `:362`–`:370`.
5. **The alarm that a requirement depends on is subscribed.** Pin: a contradiction that blocks a
   ruling reaches a human through the configured channel — the positive case, not the gate.
   Evidence: `devdoc/requirements/04-sync-and-nodes.md:536`–`:558`; the configured allowlist measured
   here (`/home/user/.gopi/settings.json`), and the same class already recorded in
   `internal/tools/tasknotify.go:158`–`:164`.
6. **A closure test must compare the definition against the dispatcher, not itself.** Pin: every
   advertised ability has a dispatch arm — the direction commit `6f68a60c` implemented
   ("def↔dispatcher gate"). Evidence: `devdoc/requirements/06-agent-loop.md:607`–`:656`, and Rule 3's
   own contract as the repo states it (`devdoc/requirements/00-method-and-standards.md:436`–`:451`
   on adoption standards; `TestGOPIRule3_MatchesClaudeToolDefs`,
   `internal/agent/gopidoc_sync_test.go`).
7. **Residue has a bound and a refusal, per family.** Pin the *bound* boundary, not the mechanism:
   the distilled store's pruning refusal, the crash-dump sweeper's candidate set (378 structurally
   unreachable dumps), the chat index rows that can never close, and unregistered directories.
   Evidence: `devdoc/requirements/05-context-and-memory.md:424`–`:475`,
   `devdoc/requirements/06-agent-loop.md:799`, `devdoc/requirements/09-chat-ui-verification-report.md:162`–`:167`,
   `devdoc/requirements/03-worktree-and-cwd.md:391`–`:397`.
8. **The judge of a *document's* claims is a gate that can go red.** Pin the citation gate, the
   code-identifier check for the product-language paragraph, and the negatives that prove them
   non-vacuous — because this series' own worst recorded defect is a proof that measured nothing:
   `grep -rn '"chats"' internal/sync/` printed `No such file or directory` and its "no hits" was
   vacuous; the conclusion had to be re-established positively
   (`devdoc/requirements/09-chat-ui-verification-report.md:99`–`:118`,
   `devdoc/requirements/09-chat-ui.md:900`–`:975`, corrected at
   `devdoc/requirements/09-chat-ui-verification-report.md:113`).

**Order of work, if only three land in the first pass:** 1, 2 and 4. The first is the only one that
loses finished work; the second is what makes the first visible; the fourth is the topology the
first two assume (`devdoc/requirements/sync-and-nodes.md:393`–`:489`, G1/G2).

---

## 7. The operator's stated positive vision, as constraints — how each fared across the series

| constraint (`devdoc/requirements/00-method-and-standards.md:359`–`:446`) | whole-project verdict |
|---|---|
| **C1** one static binary, zero dependencies, no plugin architecture | **Held where it was chosen, breached once.** One direct dependency, pure Go; the outward tools were hand-built — including an 8,746-line editor written rather than depended on, because the alternative could not be fetched (`devdoc/requirements/09-chat-ui.md:655`–`:675`). The breach is unresolved in mechanism, not intent: the web tools delegate to a deployed Python tree, deliberately sanctioned and deliberately excluded from the script gate (`devdoc/requirements/07-tools-and-routing.md:1168`–`:1200`). |
| **C2** the operator controls the whole thing, not a federation of silos | **Partly unmet.** The control faces exist and are read-only or bounded (`--status/--stop/--restart`), but the judgement the launcher delegates is not inspectable by him except through a log file (`devdoc/requirements/merge-and-branches.md:639`–`:660`), and two run-level alarms do not reach him (§4, item 4). |
| **C3** a daemon must stay simple; every branch multiplies the failure surface | **The most clearly violated.** The tick runs four sweeps, two asked to leave (`devdoc/requirements/merge-and-branches.md:521`–`:596`), and the merge/park lifecycle multiplies one user-visible concept into five namespaces (`devdoc/requirements/02-merge-and-branches.md:729`–`:755`, `devdoc/requirements/merge-branch-lifecycle.md:360`–`:374`). |
| **C4** the agent cleans up after itself | **The most consequential failure.** Four residue families, each unbounded and each partly invisible to its own sweeper (§3, GAP-5), plus the case where residue blocks delivery (§3, GAP-1). |
| **C5** small team, one person can run it | **Met in the small, open in the large.** The in-run cognitive budget is respected (2,866 `context_trim` calls, a 4,000-character anchor budget, a 3-row memory digest — `devdoc/requirements/05-context-and-memory.md:842`), while the command surface carries 25 accepted top-level words and 29 accepted verbs inside one family, 12 of them hidden (`devdoc/requirements/cli.md:434`–`:518`). |
| **C6** `why` over `what` | **This series is the remedy, and it also found the disease.** Three features ship with a written purpose the code does not implement or does not match: the demotion apparatus whose description promises automatic demotion (`devdoc/requirements/05-context-and-memory.md:844`), a purpose statement attached to a package identity that does not exist (`devdoc/requirements/05-context-and-memory.md:690`–`:700`), and a chat surface with genesis and no requirement statement (`devdoc/requirements/09-chat-ui.md:676`–`:700`). |

---

## 8. Confidence and evidence limits

- **The evidence ranking this document used**, inherited from the series canon: `source=human`
  ledger rows (the operator speaking) **>** a `GOPI.md` rule and the incident it names **>** a commit
  message stating a WHY **>** an intent-stating comment **>** a devdoc **>** a test assertion; a
  claim with none of these is `[opinion]` (`devdoc/requirements/00-method-and-standards.md:460`–`:570`).
- **Every module document was read, not summarised from memory.** Where this summary restates a
  module's verdict it cites the module's own line; where it goes further — GAP-1's "still true at
  master", GAP-2's allowlist re-measurement, GAP-4's resolution by `6f68a60c` — it says so and
  gives the read or the command.
- **The staleness of the series is real and must be read as such.** The module documents pin
  `17cec86b` (2026-09-11), `812f9a60` (2026-09-17) and `9ce8e159`; this summary reads master
  `5cdae25e` (2026-09-18). Two of the findings above moved inside that window (`6f68a60c` merged the
  screen-capture dispatch arm; `5153cf6c` gave the close-out chain its delivery-point sync), and the
  module documents' own rule is that a module document governs *its own revision*
  (`devdoc/requirements/sync-and-nodes.md:745`–`:760`). **A reader should treat every `file:line`
  here as a pointer to read, never as a number to trust** — the citation standard, and the reason
  each module re-anchored its citations at its own base
  (`devdoc/requirements/00-method-and-standards.md:571`–`:589`).
- **Three structural limits bound the whole series.** (1) The operator's words reach us through a
  ledger with no author field (`devdoc/requirements/merge-branch-lifecycle.md:806`–`:810`).
  (2) Several measurements are *samples*, not audits — the tool-adoption counts are a 300-log sample
  (`devdoc/requirements/README.md:52`–`:63`). (3) The ledger itself is a moving system: its counts
  *fall* as rows are archived, so every number is a snapshot with a timestamp, not a property
  (`devdoc/requirements/00-method-and-standards.md:460`–`:489`).
- **The series' own worst defect is recorded rather than hidden**, and it is the reason §6 pin 8
  exists: a proof that measured nothing because it grepped a directory that does not exist
  (`devdoc/requirements/09-chat-ui-verification-report.md:99`–`:118`), a headline number taken from
  a substituted path rather than the shipped function (`:143`–`:152`), and a series carrying two live
  id schemes at once, so that cross-references between documents resolve to the wrong module
  (`devdoc/requirements/00-method-and-standards.md:622`–`:640`, `:700`–`:712`). Each was repaired in
  place, with the old reading marked `[superseded]` rather than deleted
  (`devdoc/requirements/05-context-and-memory.md:26`–`:28`).

---

## What I could not determine

1. **Whether the close-out entry gate at master is a regression or an incomplete repair.** The
   module doc called the duplicated gate the reason the obvious repair fails
   (`devdoc/requirements/03-worktree-and-cwd.md:360`–`:373`); master still returns at the entry gate
   (`internal/tools/task_finish_cleanup.go:1130`–`:1149`) while carrying the pre-merge step in the
   body (`:2204`–`:2237`). `git_blame` on the entry gate and the two archived close-out branches
   (`archive/fix/client-closeout-push-delivery`; master-canonical merges `94666f7d`, `438770c1`,
   then the back-out `085811f8`) would decide it. I did not run that history read, and I will not
   guess which side of "half-applied by design" it falls on.
2. **Whether the delivery-point sync (`5153cf6c`, 2026-09-18) closes the class or only one member
   of it.** The commit lands hours before this summary's base; no module document measures it, and
   one reading of its subject cannot tell a single-instance fix from a family fix.
3. **Whether the two archived close-out branches should be revived or re-derived.** Rule 23 forbids
   continuing on a failed branch and encourages reading it; that is a dispatch decision, not a
   summary's, and the module doc's own recommendation was made against a revision that has since
   moved.
4. **Whether any of the nine operator-intent questions in §5-A has since been answered on the other
   host.** The ledger is replicated but its *recent* rows are not verified to be equally present on
   both machines (that is GAP-2's own subject), so a row answering one of these questions could
   exist and be invisible from here.
5. **[inferred] The strongest single sentence this series can support.** I have written it as §4's
   sharpest disagreement, and its weakest link is that the three evidence halves come from three
   revisions and two hosts: the operator's stall (`01a0aab8`) is a client-host incident, the
   `completed`-semantics count is a ledger snapshot, and the code half is a read at `5cdae25e`. A
   reader who wants it as one measurement would have to reproduce the stall on master.
