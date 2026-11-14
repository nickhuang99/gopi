<!-- doc-class: historical -->
# The ALREADY-LANDED question reaches its third producer — the merge LLM child

<!-- devdoc-class: implementation -->

Task: `01a1315b-a35a-7316-82a8-b4919d3e63f0` (branch `task/01a1315ba35a731682a8b4919d3e63f0`).

## The gap this closes

`ProbeReworkLanding` (`internal/tools/rework_landing_probe.go`) answers one
question: *does the integration branch already carry a commit with this branch's
tip subject?* It was already wired into **two** Go filing lanes — the merge
ceiling arm (`merge_ceiling_rework.go`) and the hand-over escalation arm
(`merge_handover_escalate.go`) — both of which put
`ReworkLandingEvidence.Section()` into the payload of the rework row they file.

The **third** producer of that row shape is not Go at all: it is the merge run's
LLM child, which files branch-rework rows by hand from `cmd/agent/merge_prompt.md`
§3.6. That child is the producer the measured incident came from — a rework row
filed for a branch whose subject `master` already carried, discovered by hand at
the END of a run instead of at the start.

A prompt cannot put Go into a model's hands, so the prompt has to be able to say
**"run X"** and get back the ONE spelling of the answer.

## What landed

### 1. `gopi merge --already-landed NAME` — a read-only arm on an existing verb

`cmd/agent/merge_already_landed.go`. It resolves the repo set, runs the probe and
prints `ReworkLandingEvidence.Section()` **verbatim** — the same bytes the two Go
lanes write into their payloads.

- **A flag, not a new verb** (Rule 3): the already-landed question is a property
  of the merge face — it is about the merge command's own branch set — and
  `--status` is the existing precedent for a read-only arm on this verb.
- **It takes the BRANCH as its own value** (`--already-landed NAME`), and that is
  load-bearing: a positional at this verb is a merge SELECTION, so
  `gopi merge feat/x --already-landed` would read as *"merge this branch"* to
  every other line of the command. The value keeps the question and the act apart
  by construction. It lands in its own `opts.alreadyLanded` field, never in
  `opts.branches`.
- **Answered BEFORE the lock.** The child asking this question is running INSIDE
  a merge run that already holds the single-instance flock, so an arm that took
  the lock would deadlock on its own parent's guard. This is the same placement
  `--status` has.
- **Read-only** (Rule 35): no lock, no ref written, no row inserted, no child
  spawned, no artifact created. It runs `git rev-parse` / `git log` /
  `git merge-base` and prints.

The repo set is the settings.json `repos` table **plus the repository the process
was started in** when the table does not already hold it. That union matters: a
table that omits the repo the child is standing in would make the arm answer
"no repo holds the branch" about the one repo the question is about — the exact
false-negative shape this work exists to remove.

### 2. `cmd/agent/merge_prompt.md` §3.6.3 — the section

Anchored (`<!-- merge-section: §3.6.3 already-landed -->`) and listed in
`mrgRequiredSections`. It tells the child to run the arm, paste its answer into
the row's payload, and names:

- the **discriminator**: commit-**SUBJECT** equality;
- the probes that **invert the truth** on the measured pair — `git patch-id` and
  `git cherry` (the branch's fix and the landed fix were written twice, so their
  patch-ids differ and `git cherry` lists the tip patch-unique `+`: a
  patch-identity probe reports "not landed" for work that IS landed);
- why a **subject match is a SIGNAL, not a verdict** — hundreds of commits in
  this repo share the bookkeeping subjects, so the probe refuses to found an
  equivalence on one at all; the child reads the landed commit it names and says
  what it found;
- that **the match never changes whether the row is filed** — the branch still
  cannot merge, so the work still needs a home (§3.6.1 case ④). It changes what
  the payload SAYS.

§3.6's numbered recipe gains **step 0** (answer the question) and its closing
sentence now requires the answer in the payload, so the section is reachable from
the list the child actually follows.

### 3. One spelling (Rule 14)

The three answer headlines became **exported constants** in the probe
(`ReworkLandingSignalHeadline` / `ReworkLandingCleanHeadline` /
`ReworkLandingUnknownHeadline`), and `Section()` composes its text from them. The
prompt quotes the same three values, and the gate that pins the prompt reads them
**from the constants** — a rename moves both in one commit or the gate fails.

## Verification

See `## Verification` in the task report; the Rule 11 acceptance is the two real
merge runs recorded below.

## What this does NOT do

- It does not change `Section()`'s wording. Rule 14 asked for ONE spelling, so
  the probe's prose was preserved and only the three headline strings were
  lifted into constants.
- It does not make the child's answer authoritative. A subject match is a signal;
  the child's judgement (read the landed commit, say what it found) is still the
  decision, and the row is filed either way.
- It does not add a scheduled or resident anything (Rule 35) — the arm is a
  one-shot read.
