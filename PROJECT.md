# GoPi Coding Rules — Project Context

> GoPi reads this file from the current working directory at startup.
> These are HARD CONSTRAINTS — not suggestions.

> **This file is TRACKED and travels with every clone and worktree.** It is the
> PROJECT's engineering contract: the rules a check-out of gopi must carry on any
> machine. The file beside it, `GOPI.md`, is the OPPOSITE kind: it is the
> CLIENT's (this machine's / this operator's) local operations policy, it is
> gitignored at the repository root, and git never carries it between machines
> (see `.gitignore`'s `/*.md` block, which whitelists this file and not that
> one). The loader accepts both by NAME, and when a directory holds both,
> **`PROJECT.md` wins** and the ignored `GOPI.md` is named on stderr
> (`internal/agent/projectctx.go`) — a conflict is never silent.
>
> Consequence for a rule's wording: anything here must hold in a bare clone.
> Host-local material — this machine's addresses, ssh aliases, task-sync pins,
> the operator's `chat-policy` fence — belongs in `GOPI.md`, never in this file.

**Configuration lives in `CONFIG.md`** (repo root): every env var, settings key,
CLI flag, its default and its precedence, each with the source line that reads
it. Read it before changing a knob — **and before EXPLAINING one** ("which setting
controls X?", "is Z the gate for W?"): it answers that with the source line that
decides it. Update it in the same commit — and know what checks that work. The
page-wide gate `internal/standards/config_doc_gate_test.go` was DELETED in `29882b3`
(2026-10-22, doc-pipeline removal slice 1/5) together with its arms for an invented
`GOPI_*` name, a catalogued knob the page never carried, an anchor on a blank line,
and an anchor not carrying the claim.

**THE CENSUS ARM IS RETIRED TOO, and it is named here because this paragraph used
to promise it.** Commit `78bdf05a` (2026-11-05, operator ruling) deleted the whole
documentation-citation family — the three generators `config-anchor-census.go`,
`citation-census.go`, `citation-selector-census.go`, the three tables beside them,
and the gates that drove them. The census that measured CONFIG.md's anchors into
`config-anchor-census.tsv`, and the `TestMergeConfigAnchorGate` arms that ran its
`--check`, are therefore **gone**: a reader who runs the command this paragraph
used to print gets `stat … no such file or directory`, and a reader who looks for
the arms gets `[no tests to run]`. **No machine judges whether an anchor names the
line that DECIDES the row.** That is why `CONFIG.md`'s **ANCHOR SEMANTICS
(A1–A6)** is prose you obey rather than a description of a passing check: the
anchors are a landing-time record a human still reads, and nothing resolves them.

A wrong answer about configuration is a defect of the same cost as a wrong
change: cite the doc, or state that you did not read it.

> **Tier A / Tier B.** This file is Tier A: each rule's BEHAVIOUR and the MACHINE
> CHECK that catches a violation. The reasoning — post-mortems, measured tables,
> rejected alternatives, worked examples — is Tier B,
> `devdoc/rules-reference.md`, moved verbatim, read on demand:
> `grep -n "## Rule 27" devdoc/rules-reference.md`. Nothing was deleted and no
> gate was weakened.


## Rule 1: No Magic Numbers or Bare String Keywords

Every literal number for a timeout, size, limit or threshold MUST be a named
constant at the top of the file. The same covers **string literals that carry
meaning** — identifiers, keywords, values compared/switched on/stored, or naming a
file, directory or CLI verb: take the value from its constant package, never type
it inline. Classes: tool names → `internal/toolnames`; task-status values →
`internal/tools` exported consts (TaskStatusPending / TaskStatusClaimed /
TaskStatusCompleted …); tag names, source values (`auto`/`human`/`llm`), CLI verbs,
directory/file names → their exported consts.
Check: `internal/tools/named_limits_rule1_test.go`,
`internal/standards/worktree_root_gate.go`.

## Rule 2: Single Source of Truth for Deadlines

If a deadline can be overridden by env var, the constant is the default,
and exactly ONE place reads the env var.

```go
const defaultAgentTimeout = 0 * time.Second  // 0 = unlimited

timeout := defaultAgentTimeout
if v := os.Getenv("GOPI_TIMEOUT_SEC"); v != "" {
    if n, _ := strconv.Atoi(v); n > 0 {
        timeout = time.Duration(n) * time.Second
    }
}
```

## Rule 3: Tools Are Minimal — Don't Add New Ones

GoPi has 17 tools: read, write, edit, bash, code_search, keyword_search, web_search, web_fetch, git_blame, job, task, context_trim, task_submit, memory, vision_analyze, text2img, screen_capture.

`claudeToolDefs` in `internal/agent/loop.go` is the source of truth for that list —
`TestGOPIRule3_MatchesClaudeToolDefs` asserts the two agree. `task` with
action=view returns the harness-tracked task list; task(action=commit/review) are
its in-session working-area side effects; task(action=reframe) is the epic
reframe+evaluate call (DOABLE | TOO_BIG+roadmap | IMPOSSIBLE+reason);
`context_trim` drops a dead-end tool_result from context. Do NOT add new top-level
tools — extensions go through `code_search` (symbol definitions),
`keyword_search` (full text; bash grep/find are NOT for searching code), bash
(build/test/run/git/file ops), read/edit/write, memory
(action=search|store|update|feedback), and `task_submit` (work for a fresh
session).

**A chat run publishes its own surface identity to the children it spawns**, and
that is a CHANNEL of the tools above, not a tool: `gopi chat` exports
`GOPI_CHAT_SURFACE_ID=<pid|self>:<session-dir>` into its own environment at arm
time (`cmd/agent/chat_surface_identity.go`; the name and its reader live in
`internal/config/chat_surface_identity.go`), the `bash` tool's child inherits it
with the rest of the environment, and `gopi ui_state --target chat` reads it as a
CLAIM — the caller's own artifact, matched against dumps that already proved
their (pid, start) identity, with a refusal that names the claim and the pids when
it matches nothing (`internal/uiclient/selectself.go`). The same rule answers the
chat surface's own unarmed read of the profile. An unset variable means "nothing
was published", and the general rule (`uiclient.SelectLive`) then applies exactly
as it did before: `--pid` wins outright, then a `--session` with a check to run.

## Rule 4: All Edits Are Surgical

Use edit() only for targeted string replacement — never whole-file replacement.
If old_string would match more than half the file, use write() instead.

## Rule 5: Build + Test + Commit — a report is the author's choice, never a gate

**A written report is NEITHER REQUIRED NOR FORBIDDEN.** The operator's ruling
(2026-10-26) is the standard: an engineer who ships a feature or fixes a bug
writes a document when it is worth writing and skips it when it is not. The two
honest reasons to write one are **to hand the idea to someone else** and **to
leave a note to your future self** — "why did I change this?" is a question the
author's own memory will not answer in a month. That is a spontaneous act.
**Forbidding it buys nothing; requiring it buys a corpus nobody reads.**

**What IS required is the EVIDENCE, not prose about it.** Every change cycle:
`go build` → `go test` → commit → **the run's verification facts** (the `Ran:`
and `Result:` lines below). A claim nothing was observed for is not a
deliverable, whether or not a document accompanies it. A run that changed
nothing reads differently: it reports what it MEASURED and closes with that
measurement as its deliverable.

**Do not manufacture a document to satisfy a process.** A `devdoc/<feature>.md`
written because a rule demanded one is worse than none at all: it is a file
that costs every future merge a conflict surface and answers no question
anyone asked (the measured case: `devdoc/DOC-INDEX.tsv`, edited 43 times in
60 days — four times more than any real document — because every new document
had to add a row to it). Knowledge that must survive belongs in **memory**,
where it is searchable, votable and retrieved on demand; a file in the tree is
retrieved by nobody.

### Two things this rule does NOT leave to taste

**5a — A REPORT, IF WRITTEN, IS A PRIVATE ARTIFACT, NOT A COMMITTED ONE.**
Its location is the path in `$GOPI_TASK_REPORT_PATH` — a task run's OWN report
in its OWN session directory, exported by the harness, never guessed and never
committed to the repository. Writing one there is always allowed and never
gated. **Nothing in this rule asks a run to put a report in the tracked tree.**

**5b — A SHARED INDEX IS FORBIDDEN — this one is absolute.** No file exists,
in the repository or generated into it, whose update is part of adding a
document, a task, a test or any other entry: no `DOC-INDEX.tsv`, no manifest
table, no catalogue of everything, no "add your row here" file. A file every
author must touch is a file two authors always conflict in — the measured cost
is a real one (two runs whose code was fine, both bounced because the index
conflicted), and the shape must not come back in a new name. **A document is a
FILE IN A DIRECTORY: its identity is its file name, so two authors writing two
documents never meet.** Need to find something? Grep the tree, or ask memory.
Never a table.

Minimum report content, for a run that chooses to write one:
```
# Task Report: <summary>

## Completed
- What was implemented (list)

## Not Done
- What was deferred or skipped (be honest)

## Verification
- Build: <output>   (e.g. "go build: OK")
- Tests:  <output>   (e.g. "make test: go build OK, go vet OK") — smoke and
          branch-scoped to what the diff touched, never the whole tree
- Ran:    <the exact invocation of the surface you changed, verbatim, with real
          arguments — the CLI verb+flags, the entry point, the payload you sent.
          `-h` is not a run. See Rule 31: a surface you did not execute is not
          delivered.>
- Result: <what you actually observed: output, exit code, or the state you
          queried afterwards to confirm the claimed effect>
- Commit: <hash>     (e.g. "a1b2c3d feat: ...")

## Evidence (self-filed)
- One line per gate claim: the command and the output you saw, or the
  measured value itself (exit code, `/proc/<pid>/cgroup` reading, the
  red-then-green transition). A claim about a fact the FRAMEWORK measures
  (your worktree's state, whether the branch merged) is the framework's to
  report — do not restate it here.
```

**A gate result the run did not observe is not a reportable result.** The
measured case (task `01a0af50`, 2026-09-17): a report asserted "`git status
--porcelain` empty in both worktrees (Rule 30)" while the same run's close-out
machinery printed five dirty entries for that worktree. The claim carried no
observation, and the observation that existed contradicted it — a reviewer has
no instrument for that. So a gate claim carries its own observation (the
`## Evidence (self-filed)` section above), or it is not made. The framework
does not re-run the gates a report claims; it **records** the sentences that
assert a result with nothing behind them (the seal's gate-claim note rides the
row's own trail) and never rewrites a run's prose.

**The red-proof RESTORE is a gate claim, and its criterion is machine-checked.**
A red proof ends by restoring the mutated production file *and* verifying the
restore. The verification is the claim, so it is held to the rule above: an
index-relative diff is not evidence of a restore, because a restore seeded from
the index writes back the very bytes the index holds. Measure it with an explicit
revision — `git diff HEAD -- <path>`, or the index-free blob comparison
`test "$(git hash-object <path>)" = "$(git rev-parse HEAD:<path>)"` — and see
`devdoc/redproof-restore-criterion.md` for the measured failure and the full
recipe. Check: `go test ./internal/standards/ -run TestStandardGates/TestGateRedProofRestoreRedProof`.

**The same criterion has a TIMELINESS half, added 2026-10-14.** A content-identity
reading is anchored to something; if both sides are artifacts of this run (a
snapshot copy and its recorded digest), the reading is true only about the moment
it was taken, and **a later write does not move it** — measured (task
`01a138317d847ac3806c8a701eb652f3`): a reading taken 11:12:03 still said
`same=YES` after the file was written again at 11:13:12, while the delivered blob
was a different one. Anchor at least one side to a **revision**
(`git rev-parse HEAD:<path>`, `git show <rev>:<path>`) so the reading turns red by
itself when the file moves, and **if the file has been written since the reading,
RE-TAKE it** — even for a `gofmt`, a comment or one more test arm. See
`devdoc/redproof-restore-content-identity.md`; the machine check is the same
gate's second arm (`red-proof-identity-ok:` is its inline escape).

## Rule 6: Isolated Work Directory + Git Push Semantics

**NEVER develop in the project's main directory** — another agent may
be working on a different task in the same project.

### 6a. Your ground is GIVEN to you — and you work on `master` IN IT

**THE RUNNER MAY HAVE GIVEN YOU YOUR GROUND ALREADY — CHECK FIRST, AND DO
NOT MINT A SECOND.**
The runner establishes a per-task ground before it spawns you and starts you in
it, and this run's own briefing says so in its `[Your ground — ALREADY CREATED
for you]` block, naming the directory. **If that block is in your briefing, that
IS your ground: work in it, do NOT create a second one**, and do not run any of
the steps below.

**WORK ON `master` — your own clone is the isolation, so a side branch buys
nothing.** You stand in a clone that belongs to this run and to no one else;
nothing you commit there can overwrite anyone's work, so there is no collision
for a uniquely-named branch to prevent. **Cut no `task/<id>` branch, and do not
rename whatever the ground happens to be on** — the branch name is not an
address you have to maintain, and the ref you stand on is what
`gopi task show --branch` reads a row back through, so a descriptive rename severs
the ref from the row it belongs to. **Do not rename its branch either**: describe
your work in the report, never in the ref.
Merge the remote `master` into yours when the
delivery loop tells you to, **resolve the conflicts yourself** (that is the
job — see 6c), and push `master`. **Every worker is its own merger**, which is
the whole point: a single resident merger serialised every merge in the system
and was the bottleneck; work now lands in parallel and a concurrent push that
loses a race is the price of that parallelism, paid by re-running rather than
by a queue.

**A RUN NEVER CREATES A WORKTREE, AND A GROUND IS ALWAYS A CLONE.**
`git worktree add` is not a step a run performs — and neither is a linked
worktree a shape a run is given. The ruling (2026-11-03) is unambiguous: **a run
works in its own clone under `workspace_root` and nowhere else.** A linked
worktree shares the main repo's object store and refs, which is exactly what
makes it the wrong ground: it keeps the INTEGRATION branch's namespace open, it
leaves a `task/<id>` ref behind that no merge path consumes, and the residue
survives every sweep because the ledger row that would have named it is gone.
Measured on this host: 25 `~/workspace` entries left as linked worktrees into
repos that no longer exist, ~3.4 GB, none of them reachable by
`git worktree list` or by any sweep arm.

The shape is therefore not a menu the runner picks between:

- **CLONE (`.git` is a DIRECTORY) — the ONLY shape a ground may take.** The
  channel opens a clone of the declared `dev_repo` under `workspace_root`, works
  it on `master`, and hands the work back through the channel's own delivery
  loop. This is the shape every rule in this file is written for.
- **NO GROUND AT ALL is the correct answer when there is nothing to clone
  from.** A host with no declared `dev_repo`, or a `dev_repo` that does not
  answer, gets a REFUSAL that names the missing input — never a fallback. That
  is Rule 15 ("no silent failure") applied to the ground: falling back to a
  linked worktree converts a configuration error into a permanent ref nobody
  collects. A run that finds no ground says so and stops; it does not invent one
  and it does not accept a worktree as a substitute.

**Why the fallback existed, and why it is retired rather than merely avoided.**
The linked-worktree arm was the shape a host with no `dev_repo` used to get, and
deleting its CREATION arm on 2026-10-27 turned 41 `cmd/agent` e2e walks red —
because those walks set up a repo WITHOUT a `dev_repo` and expected a ground
anyway. (The 41 is a SET DIFFERENCE between two runs' raw streams, not a count
of any one run: r1−r2 = 41 turned red only while the deletion was in, r2−r1 = 0
arrived after it, 13 were red in both. The recipe is in
`devdoc/worktree-removal-census.md` §1.0.1.) Retiring the shape therefore means
retiring the EXPECTATION with it: an e2e walk that wants a ground declares a
`dev_repo` (a local fixture repo is enough), and a walk that declares none
asserts the REFUSAL. Keeping the fallback "because the tests need it" is the
tail wagging the dog — the tests are the thing that must change.

**What the measurement DID justify, and is what changed** (`cmd/agent/cli_tasks.go`):
the worktree resolver used to be called UNCONDITIONALLY and its answer then
overwritten when the clone channel was on, so every clone-channel run paid for a
ground that was discarded one line later. It is now consulted only when the
clone channel is off. Measured on this host before the fix:

```bash
# the ground the run log names, and whether anything is still there
grep -h 'CREATED the ground' ~/.gopi/sessions/*/output.log | head -3
ls -d /home/user/workspace/gopi-01a13207597b* 2>&1        # No such file or directory
# the shape census, by the ONLY discriminator (.git kind, not the name)
for d in /home/user/workspace/*/; do
  if [ -d "$d/.git" ]; then echo clone
  elif [ -f "$d/.git" ]; then echo linked
  fi
done | sort | uniq -c        # 207 clone, 23 linked
```

**What this means for you, concretely:**

- **You have a ground in your briefing** → work in it (the normal case).
- **You have NO ground and no block** → say so and STOP; do not invent one. A
  ground is the runner's job, and it is ALWAYS a clone of the declared
  `dev_repo`. With no `dev_repo` the correct outcome is a refusal that names the
  missing input — never a hand-built worktree and never a linked one. A
  hand-built ground is the shape that produced the orphaned spare worktrees this
  rule exists to stop (measured: one task → two worktrees, the child working in
  the runner's ground the whole time while the one it made for itself was
  collected by no sweep).
- **A CONFLICT IS YOURS TO RESOLVE**, not a reason to branch away from it.
  `git pull --rebase` (or the channel's own alignment step) is where another
  run's work meets yours; read both sides and land the union, never "mine
  wins" and never a branch cut to sidestep the file. That is the job this
  model gives every worker instead of the resident merger.
- **Never mint a ref to make room for your own commit.** You do not need one:
  this rule puts your work on `master`. If you ever do create a branch, prove
  the name is free first (`git rev-parse --verify --quiet
  refs/heads/<name>`; exit 0 = TAKEN → pick another name) — **A TAKEN NAME MEANS
  PICK ANOTHER NAME — never make room for it.** A live ref is somebody's branch,
  and reusing it is a claim on another task's work, not a shortcut. And **never**
  use
  the force spellings — they do not refuse, they RESET the ref to your base and
  the commits it held survive only in the reflog (measured: `git reflog show
  <branch>` → `branch: Reset to HEAD`, the lost tip one entry below). The force
  spellings are `-B` on the create verbs and the `-f`/`--force` flag beside a
  create flag; GOPI.md names them here as the banned vocabulary, and
  `internal/branchform` + the standards gate `TestBranchForceGateRedProof` fail
  the build if one appears in this file or in a briefing gopi renders.

### 6b. After cloning, ALL tool paths MUST point inside the clone

The most common way to wreck work: reading/editing files with the MAIN
repo's absolute paths after cloning. Once you clone:

- **Every** read/write/edit/bash/code_search/keyword_search call must use
  paths under `~/workspace/gopi-<task-id>/` — relative paths (your CWD is the
  clone) or the clone's absolute path. **NEVER `~/work/GoPi/...`** paths:
  an edit "succeeds" there silently but lands in the main repo, where
  another agent is working.
- Self-check after each edit: `git status` in the clone must list the
  file you just changed, and `git status` in the main repo must NOT
  list it.
- If you discover edits in the main repo: STOP. Revert only the files you
  touched (`git restore <the files>` inside the main repo — never
  `git restore .`, which could wipe another agent's work), then re-apply
  the same edits in the clone. Do NOT continue in the main repo "just
  this once".

### 6c. Getting the work back: the ground's own delivery channel

**Which answer applies is decided by WHICH KIND of ground you are in**, never by
a preference and never by the channel that dispatched you. The shape is MEASURED,
and the measurement has ONE definition:
`devworkspace.GroundModelOf(dir)` (`internal/devworkspace/ground.go`) — a
DIRECTORY named `devworkspace.GroundGitEntryName` (`.git`) means a CLONE of its
own (the clone channel's workspace), a FILE means a LINKED WORKTREE of another
repo. Everything that has to state a delivery consequence renders it FROM that
answer: the run's own briefing block, the delivery-proof arm
(`internal/tools/task_delivery_proof.go`) and the publish guard
(`internal/tools/git_publish_target_guard.go`) all ask the one function, so no
two of them can disagree.

**Under this rule's model the WORK BRANCH is `master` itself** (6a): a clone
publishes its `master` to the dev repo's `master`. A LINKED WORKTREE is not a
ground this rule grants, so the delivery answer for one is not "hand the commits
back through the shared repo" — it is that the ground should never have been
created. If a run finds itself standing in one, it says so and stops (Rule 6a);
it does not deliver from it, because the commit it would produce sits on a
branch in a namespace the integration path does not read.

**Which shape you will actually meet is measured, not assumed: a CLONE, on every
host that declares `dev_repo`.** A host that declares none gets a refusal, not a
worktree (Rule 6a, 2026-11-03 ruling). The two bullets at the end of this section
that used to answer "a measured LINKED WORKTREE ground" and "a /tmp clone with no
channel behind it" are RETIRED with that shape: a ground is a clone of the
declared `dev_repo`, or it is an error naming the missing input.

```bash
# from inside the assigned worktree:
git fetch ~/workspace/gopi-<task-id> feat/<task-name>
git merge --ff-only FETCH_HEAD
```

- **A CLONE ground (`.git` is a DIRECTORY — the clone channel's workspace)?**
  **Neither instruction above applies to you.** The channel has its own DELIVERY
  LOOP (clean → pull --rebase **onto your `master`, resolving conflicts
  yourself** → declared build → push `master`). **It runs ONCE PER RUN, from
  this run's END hook (the close-out, immediately before the row's status
  write) — NOT at a round boundary: there is no per-round push, and no push to
  catch mid-run.** (The briefing renders this sentence from
  `internal/tools/task_briefing_publish_clause.go`, the ONE source of the
  wording — if the two ever differ, the code is right and this is the bug.) So
  **do not push by hand**: no `git push <remote> HEAD:<some-name>`, no
  `feat/<name>`, no push of the integration branch from your checkout. A
  destination the RUN chose **creates a ref on the server that nothing merges
  and only an operator can delete** (and naming the integration branch from
  another checkout **moves** the server's integration branch). It is refused
  before it runs (`internal/tools/git_publish_target_guard.go`, on the bash
  tool), and `GOPI_ALLOW_DANGEROUS=1` is an operator decision, never a run's.
  Commit on `master` in your clone, leave `git status --porcelain` empty, and
  report; publishing is the channel's job, and **the conflict the alignment
  step surfaces is yours** — resolve it in the same commit rather than carving
  out a branch. A commit the server does not have yet is a line in your report
  — **not** a reason to invent a ref.

  **A push you have not seen YET is not evidence that the loop is broken** — it
  has not had its turn, and it gets exactly one. "No push during my rounds,
  therefore the delivery leg did not run" is a false conclusion with a measured
  cost: row `01a17921-fa90` passed twenty rounds, reported the leg as broken,
  and was wrong — its own log ends `clone-channel: attempt 1/M: push accepted`,
  and its commit was an ancestor of origin/master. The leg RAN; the report was
  noise. **The evidence that it RAN** is the channel's own line in that run's
  log (`clone-channel: attempt N/M: push accepted`, M being the channel's own
  brake `devworkspace.MaxPushAttempts`, or `clone channel: delivery delivered`).
  **The evidence that it did NOT** is the row left OPEN at close-out carrying
  the channel's non-delivery reason (`clone channel: delivery did not land — …`,
  `DeliveryDidNotLandReasonPref`). Quote one of those lines; never an inference
  from silence. If the commit is still not on the server when the RUN closes,
  that is the delivery loop's verdict to report — say it in your output.

### 6d. Deliverable declaration BEFORE cleanup (MANDATORY — data-loss prevention)

**NEVER delete your clone/worktree without first declaring the deliverable's
persistent location.** The FINAL message of every task MUST state:

1. **Branch name** where the deliverable lives (e.g. `feat/gopi-vs-codex-compare`)
2. **Recovery command** to fetch it (e.g. `git show feat/X:devdoc/report.md`)
3. Confirmation the commit is in the MAIN repo (pushed / worktree-shared)

Deleting the clone/worktree is ONLY allowed after this declaration. A
deliverable that exists only in a deleted clone = LOST work = unacceptable.
Historical incident (2026-08-28): a task pushed its branch then deleted its
clone per 6d, but its final message named no branch — the report looked lost
until recovered from the branch. The declaration above is now the gate.

### 6e. Clean up

```bash
cd ~ && rm -rf ~/workspace/gopi-<task-id>/
```

Merge is handled separately after review. Do NOT merge your own branch.

## The Commit Path — 7 Steps In Order

Each step names the mechanism that ENFORCES it. An unenforced step is prose
only — the next agent may skip it. (Step 7 was the unenforced one until
2026-09-22; the incident is `briefings/brief-commit-path-ancestry-check.md`.)

1. **Work on `master`, in the ground you were given** — on a dispatched row the
   runner has already made that ground and started you in it (Rule 6a: a run
   does NOT run `git worktree add`, and it does NOT cut a `task/<id>` branch —
   your clone is the isolation, so `master` is the branch you commit on). When
   you genuinely have to mint a ref for something other than your own work,
   Rule 6a's branch-name precheck comes first
   (`git rev-parse --verify --quiet refs/heads/feat/<name>`; a taken name means
   pick another name, never a force spelling) — enforced by
   `TestBranchForceGateRedProof`.
2. **Merge the remote `master` into yours before the delivery step — and
   resolve what it surfaces.** This is the job the resident merger used to do
   and every worker now does for itself (Rule 6a): a conflict is landed by
   reading both sides, never by branching away from it. What also stands before
   the commit is the ground's OWN test run —
   `finishCleanupBranchCheckVerify` (`internal/tools/task_finish_premerge.go:264`),
   which MARKS and does not block. The push-side answer is
   `gitPushbackFastForwardsOntoMaster` (`internal/tools/task_sync_git_pushback.go:593`).
   No step here is short-circuited by a dirty worktree, and the dirty guard is
   NOT a front door on this chain: it stops the worktree REMOVAL only, never the
   push (`finishCleanupRemovalReason`, `internal/tools/task_finish_cleanup.go:1503`;
   its own comment at `:1528`). The ONE reading every dirty arm shares is
   `finishCleanupWorktreeDirtyReason` (`internal/tools/task_finish_cleanup.go:1041`).
   Rule 30's refusal of the `completed` WRITEBACK is a separate gate, not this one.
3. **Build + test** — `go build ./...` + the project's suite (Rule 5). Prose only.
4. **Commit** — Rule 5's cycle. Prose only.
5. **Push to the peer** — finish-cleanup chain step ② (`task_finish_cleanup.go`)
   and `gitPushbackPlan` (`task_sync_git_pushback.go`). Enforced: the push is
   refused unless the branch fast-forwards onto the peer's master.
6. **Remove the worktree** — finish-cleanup chain steps ③④
   (same chain; Rule 30 gates the clean worktree). The branch REF is removed by
   Rule 9 step 2's one writer (`gopi task archive`), never by a hand-run
   ref deletion: that deletion would leave no audit record.
7. **The branch must DESCEND from master** — `gitPushbackSharesHistoryWithMaster`
   (`task_sync_git_pushback.go`, `merge-base --all` = empty ⇒ refused, by name).
   Enforced at the client push; NO server-side or receive-path check exists —
   a ref that arrives by another route (a manual `git push`, a fetch) is still
   unchecked, and only `gopi task merge`'s `unrelated-history` verdict catches it
   after the fact.

## Rule 7: Devdoc Everything Worth Mentioning

After implementation, if anything was tricky, surprising, or a design decision:
- Write a brief devdoc in `devdoc/<feature>.md`
- Future agents have no memory of this session — devdocs are their only window

## Rule 8: Git Safety — Add Files One by One

**NEVER use `git add -A` or `git add .`** — risks committing sensitive data.

```bash
git add path/to/file.go path/to/file_test.go
git commit -m "feat(pool): ..."
```

## Rule 9: The competitive close-out flow (after a democratic verdict) — 3 fixed steps + a safe-merge precondition

Once the competition is over (after the democratic verdict) the 3 steps must be run **immediately** — do not wait for "there is time to clean up later":

1. **Merge the winner's branch into master** (after the PA has passed acceptance)
2. **Archive the loser's branch and clean it up**: `gopi task archive <branch> --reason hand-mistake --detail "<为什么>"` +
   `git worktree remove <path>`
   — Archiving **must** go through the one writer, `gopi task archive`: while writing `archive/<branch>` it records the WHY in
   `refs/gopi/archive-reason/<branch>`. An `archive` tag you minted yourself carries no WHY record,
   and a tag is a **decision** every merge path honours — a decision with no reason cannot be re-derived and cannot be rolled back
   (that is exactly where the ten reason-less tags of the 2026-10-12 09:46:14 batch came from). The vocabulary is in `gopi task archive -h`.
   **Deleting a ref also has this one writer only**: `gopi task archive`'s reclaim leg retracts the
   head ref after writing the decision, and records the deletion as a branch-delete audit (`arm=archive-reclaim-branch`).
   Deleting that ref yourself (that is, bypassing the writer) is a **deletion with no trail** — the ref is gone and the removal-audit sink has
   not one word about it, so afterwards nobody can answer "who deleted it, and on what grounds" (measured 2026-10-15 on a fixture repo:
   the bare deletion in `scripts/post-merge-cleanup.sh` deleted the ref and the sink was never created at all;
   `internal/standards`'s removal-audit census now reports all three spellings as red — a bare branch deletion, an `update-ref` deletion
   and a `push` deletion — in `scripts/`, in this file and in production Go).
3. **Re-check**: `git branch` + `git worktree list` — confirm that only the winner / master / in-progress branches remain

**The pre-merge guard**: before any merge you must run `bash scripts/safe-merge.sh <branch>` and confirm it passes
(✅ only exit 0 permits a merge). The script blocks three kinds of branch that slipped through:
- a branch already carrying an `archive/<branch>` archive tag (a loser)
- a branch with the `loser/*` / `archive/*` prefix
- a branch still occupied by a worktree

Blocked = that branch is a loser or was never cleaned up — go straight back to step 2 and finish the archiving, instead of forcing the merge.

## Rule 10: Case-Study Habits (text2img)

Seven habit rules from the text2img case-study (devdoc/text2img.md). These
are mirrored in the agent's system prompt [ENGINEERING RULES]; this rule
section keeps them visible to project-level agents (GOPI.md readers):

1. **Constraint dual-path**: blocked by a constraint (proxy/API/deps/permission)?
   Weigh bypass (mirror/alternative) vs accept — CREATE within the constraint
   FIRST (self-built renderer over mirror-swap).
2. **Pixel-level truth for render tasks**: verify render/vision output with
   pixel-level ground truth (IoU — PIL/FreeType), never downstream OCR scores
   (OCR can't measure render quality loss).
3. **Root cause before params**: on failure, dig the root cause first
   (5-why: symptom → mechanism → norm); A/B parameter tuning only after the
   root cause is confirmed — never "it crashed, tweak params".
4. **Minimal sample separation**: verify on the smallest sample (single char /
   zoomed / extreme value — z.txt 知知知); never trust "looks right" —
   layer truth checks to puncture it.
5. **Every fix carries `go test -run TestX`**: any bug fix ships with a
   minimal repro test (regression guard).
6. **Verification recipes go to memory**: proven verification methods/recipes
   → memory(action=store) (reuse, don't redo — e.g. "the 20px recipe lives in our
   own library #907").
7. **Dependency preflight**: assess dependency needs before a new task —
   ZERO-DEP is the target (stdlib first; external deps are the last resort,
   not the default).

## Rule 11: Tool Development Requires LLM Adoption Proof (the operator deliverable standard)

**Any tool development or modification (a new tool, a changed tool description, a changed prompt routing guide) must be accepted by the standard "the LLM really calls that tool".**

- Write code / change the prompt → rebuild the binary (CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/)
- Design **2 or more real task scenarios that naturally trigger the tool** (task files where the LLM can only, or should, finish the work with that tool)
- Run a real task (`echo "<任务>" | ./bin/gopi`) → verify the LLM actually called the tool (check the tool_use records in ~/.gopi/sessions/*.jsonl)
- The LLM did not call it → iterate on the prompt (description/routing) → re-run → until the call is stable
- **Only a passing verification permits a commit** — code plus a unit test alone = half-finished (the LLM was not persuaded to use it, so the work is not done)
- Deliver the verification evidence (the session id + excerpts of the tool_use records) with the devdoc report; the PA re-checks it in person

**Mechanism**: a tool's value = the LLM actually using it. The prompt is the tool's only persuasion mechanism (routing guidance + the description's first sentence making the routing decision + imperative anchors). A full-session audit (2026-08-27): git_blame 0 times / 21435, code_search 0.3% — a tool that exists but was never persuaded into use is wasted writing.

## Rule 12: A Task Repo Declares Its Build — The Server Rebuilds After Merging Into Master

The repo's build command must be **declared**, never discovered: settings.json
per-repo table `"repos": {"/path": {"build": "make build", "build_timeout_sec":
600}}`; GoPi declares `"build": "CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/"`.
**No entry = no build** (skipped silently after a merge). **The table is ALSO the
merger sweep list**: every `repos` key is swept each tick; an EMPTY build is a
valid PURE MERGE TARGET (swept, not rebuilt). **The close loop**: after a merge
round lands ≥1 branch the server runs the declared build on master and verifies
the binary version equals the new master tip — Merge → cleanup → **REBUILD**, else
the machine runs OLD code against NEW master. **Build failure ≠ rollback**: it
fires `build-failed` (GOPI_TASK_NOTIFY_EVENTS). Knob precedence: `CONFIG.md`.

## Rule 13: Constants Exist to Be Referenced — Never Bypass Them

**A constant defined but bypassed by bare literals is a defect** — defining
`taskSessionsRootName` is not the discipline; *referencing* it everywhere the value
is needed is. **The exhaustive test is the only anti-regression guard**: a grep-gate
test asserts a literal occurs NOWHERE outside its single definition site —
precedent `TestUnknownNode_MirrorLiteralGate`
(`internal/tools/memory_sync_task_test.go`); also
`internal/standards/worktree_root_gate.go`.

## Rule 14: Shared Values Live in a Small Exported-Constant Package

**A value shared across packages must have exactly ONE definition, in a package
every consumer can import** (unexported constants cannot cross packages, so authors
default to mirroring or typing the literal). Keep it **small and leaf** (only
constants — no business logic, no project imports, so no consumer creates a cycle)
and **exported** (an unexported constant in a shared package is a bare literal in
disguise). **Never mirror, never bare-write** (Rule 1b). Consumer-side CLI verbs
still need a source of truth — a constants file in the owning package. Precedent:
`internal/toolnames`. Check: `internal/standards/scratch_env_home_gate.go` + Rule
13's mirror gate.

## Rule 15: No Silent Failure

**Errors must be handled or recorded — never swallowed.** **15a.** `omitempty`
omits a field at its zero value; if the far side validates presence (or the
required semantic value IS the zero value) the message becomes invalid or the
semantic is lost — so `omitempty` belongs on OPTIONAL outgoing fields only, and a
required non-zero value gets a test that round-trips the zero-value case. **15b.**
An error discarded with `_ =` or ignored after a call that can fail hides the
failure — every error return is checked-and-handled, propagated, or logged with
context; comment the rare intentional discard. Check: Rule 16's `%w` gate and the
error-handling gates in `internal/standards/`.

## Machine-Checkable Gates (Rules 16–21)

Rules 1–15 are prose; **these six fail red in a test** — `internal/standards/` +
`devdoc/standards-gate.md`; run all six with `go test ./internal/standards/`. Each
gate scans `internal/` + `cmd/` and fails on any hit that is neither in its
**baseline** (an exact, explicitly-reasoned `file:line` in
`internal/standards/baseline.go`) nor marked with the gate's **inline escape**
comment; a baseline entry that no longer matches is also an error, so the
allow-list only ratchets down.

- **16 — `fmt.Errorf` must wrap with `%w`, not `%v`** (`%v` breaks the
  `errors.Is`/`errors.As` chain); deliberately off-chain → `// no-wrap: <reason>`.
  Check: `-run TestStandardGates/TestGate1ErrorWrapRedProof`.
- **17 — tests must use `t.Setenv`, never `os.Setenv`** (which leaks into sibling
  tests); reviewed exception `// env-leak-ok: <reason>`. `t.Setenv`'s scope is the
  TEST, so a PER-CALL flip is the reviewed `os.Setenv` + restore pair
  (`internal/stylegate` `RuleTestSetenvScope`), restoring via
  **`internal/testutil.SnapshotEnv`** — the hand-rolled `prev := os.Getenv(k)` form
  is lossy. Check: `-run TestStandardGates/TestGate2TestEnvRedProof` (the
  registry arm, `scan` → `enforce` over the tree) and
  `-run TestGate2EnvRestoreShapeRedProof` (a TOP-LEVEL exhaustive test, not a
  `gates()` probe — so it is named standalone, never as a
  `TestStandardGates/…` subtest that does not exist); scans `internal/`, `cmd/`, `testsuite/`.
- **18 — no `else` after a terminating `if` body** (`return`/`break`/`continue` →
  de-indent, guard clause); reviewed exception `// else-ok: <reason>`.
  Check: `-run TestStandardGates/TestGate3ElseAfterReturnRedProof`.
- **19 — never `panic` outside `init`/`main`/`Must*`** (panicking takes the
  caller's choice away; a constructor that cannot fail usefully may panic under
  `Must`); a deliberate panic carries `panic-ok: <reason>` directly above the
  function. Check: `-run TestStandardGates/TestGate4PanicRedProof`.
- **20 — no `get`/`is`/`handle` prefix on a niladic accessor** — name it for the
  value (`Name()`, not `GetName()`); reviewed exception `name-ok: <reason>`
  directly above the function. Check: `-run TestStandardGates/TestGate5AccessorPrefixRedProof`.
- **21 — every env-name constant must have a REAL reader** (`os.Getenv`,
  `t.Setenv`, `config.EnvBool`, a helper funnel), in production or a test — a
  documented hook read by nobody makes the help text claim a knob nothing consumes;
  reader elsewhere → `// env-reader-exempt: <reason>`. Check:
  `-run TestStandardGates/TestGate6EnvReaderRedProof` and `-run TestGate6EnvReaderFindsTheRealTree`.

## Rule 22: Temp Workspaces Never Live on the `/` Partition (a criterion, not a value)

**Every temporary workspace a gopi process or its children create must be provably
OFF the `/` partition. This is a CRITERION — a property to check, never a path to
copy.**

- **Check the property, never a spelling**: "off `/`" is decided by measuring the
  **device id** of the resolved temp path against the device id of `/` (`stat -c
  %d`, or `internal/scratch.AssertTempRootOffRootFS` /
  `scripts/lib/tmp-ban.sh:gopi_assert_off_root_fs`). A path is never acceptable
  because it is not spelled `/tmp` — another directory on the same partition is
  the same defect, as is an explicit TMPDIR pointing at `/tmp`.
- **No hardcoded `/tmp` in scripts**: take a directory from the scratch tree
  (`gopi_scratch_mkdir` / `gopi_scratch_queue` in `scripts/lib/tmp-ban.sh`, or
  `internal/scratch.MkdirTemp` in Go); a bounded loop writing temp data is ALSO
  bounded by an iteration cap, not only by `timeout`.
- **Prefer the explicit layer over the ambient default**: the ordinary `tmpdir`
  parameter via the `internal/config` catalog (`--tmpdir PATH` > a caller-exported
  `TMPDIR` > `GOPI_TMPDIR` > the settings.json `tmpdir` key), published into gopi's
  OWN environment so every child inherits it, never overriding a caller's `TMPDIR`;
  that precedence lives in exactly one place, `config.ResolveTmpDirConfigured`. A
  temp root that cannot be provided or written fails LOUDLY, naming the path, the
  reason and the channel — never a silent fallback to `/tmp`.
- **Never fix this at the host level**: the policy lives strictly inside gopi's own
  process tree — `/home/user/.bashrc`, `/home/user/.profile`, `/etc/tmpfiles.d`,
  `/etc/environment` and every other host-level configuration are out of scope.

Check: `internal/scratch.AssertTempRootOffRootFS`, `internal/testutil`'s suite
entry-point pin, `scripts/lib/tmp-ban.sh`.

## Rule 23: Never Continue Work on Someone Else's Failed Branch — Open a Fresh One

**Ban**: do not check out a branch someone else's attempt failed on and
commit there — not a failed/archived branch (`git checkout fix/some-failed-attempt`
→ edit → commit). **Correct practice**: on a dispatched row you work on
`master` in YOUR OWN ground (Rule 6a: the clone is the isolation, so no branch
has to be cut and none has to be named) — do not check out another attempt's
branch and do not mint a second checkout. When you genuinely have to name a ref
yourself, prove the name is free
first (`git rev-parse --verify --quiet refs/heads/feat/<name>`; exit 0 = TAKEN →
pick another name, never a force spelling) and implement your own way. The full
step — including why a taken name is a claim on someone else's work — is Rule 6a.
Branches cost nothing.

**Encouraged**: DO read the old branch first — `git log <branch>`,
`git show <branch>:<file>`, `git diff master...<branch>` — the failed path is
information, not garbage.

**Why**: a hard bugfix/feature takes several walls; the branch records the
path already walked (including attempts that only LOOK wrong); editing on top
of it erases that reasoning, so nobody can see why it got there — two people
pulling one diff. Failed attempts are evidence, kept on purpose. The old path
may be needed again: a problem can recur. Ref: Rule 9 (archive keeps the
branch = keeps that path).

## Rule 24: Process Independence Is Judged by cgroup — Never by PPID or setsid

**Criterion**: the ONLY evidence a process has left its login session is
`/proc/<pid>/cgroup` NOT matching `session-*.scope`; PPID=1, PGID==SID and a
successful `setsid()` are not evidence. `SysProcAttr{Setsid: true}` (or `setsid
nohup … &`) does NOT change the cgroup — the process still dies with the session
that spawned it:

```
grep -q 'session-.*\.scope' /proc/<pid>/cgroup && echo "NOT detached" || echo "detached"
```

**Correct practice**: a resident process that must truly detach acquires its OWN
cgroup (`systemd-run --scope`; `--user --scope` when a user bus answers); a
setsid-only fallback must be announced, never passed off as detached. **Sibling
trap**: never probe liveness with `pgrep -f "<pattern>"` (it self-matches); use
`pgrep -x <name>` or `ps -p <pid>`.

## Rule 25: Daemons Run Unprivileged — a Privilege Is Never the Remedy for a Cgroup Escape

**Ban**: never prescribe `sudo`/root (or a root SYSTEM scope) as the fix for a
resident daemon's cgroup-escape failure — not in a diagnostic line, not in a usage
head, not in a dispatched task summary; a privilege is not a missing ingredient, and
asking the operator for one is itself the bug. **Correct practice**: run the daemon
unprivileged and **announce** the degradation (setsid-only, still in the login
session scope) — never a silent downgrade and never an invented remedy; the honest
levers are `--require-scope` (refuse the start instead of degrading) and a host
service unit, which is the operator's own deliberate switch. **Anti-regression**:
the operator-facing TEXT is gated — `cmd/agent/daemon_scope_no_privilege_test.go`
fails any STRING LITERAL carrying a prescription shape (bare `sudo`, `re-run it as
root`, `requires super user`, …), with a read-floor. **Mention is not
prescription**: corrected text names the privilege to DENY it.

## Rule 26: An Incomplete Fix Is Repaired by Dispatching Again — Never by Waiting

**Practice**: when a fix turns out to be incomplete, **dispatch a new task**. Do
not stall on the question "is someone already working on this?" A duplicate
dispatch costs one agent's time; a stalled repair leaves the defect live.

**Dispatching the same work twice is allowed and expected.** Agents judge for
themselves whether a task is already done — the briefing must therefore carry
**the reason and the evidence**, not instructions to avoid overlap. Write what
is broken, what is already known, what was tried, and what "done" means; the
executor decides whether to continue, redo, or fill in the remainder.

**Corollary — an incomplete fix must be narrow, never wrong.** When a fix lands
a partial answer, say so in its own report (`## Not Done (honest)`), name the
remaining gap, and make it dispatchable. A fix that overstates its coverage is
worse than one that never landed: it closes the ticket and hides the defect.

**Overlap is handled by the briefing, not by withholding work.** If two tasks
may touch the same area, each briefing states the sibling task id and what it is
doing, so the executor can read its current state and coordinate. Do not leave
the overlap unstated, and do not use it as a reason to skip the dispatch.

**Why**: the worktree-residue family (2026-09-14) sat unfixed across several
reports because each one closed with "the code fix is not in this branch" and no
one re-dispatched. The operator's framing: *"if it repairs incompletely, dispatch
a new one to cover it — even duplication is fine, the agent judges for itself;
you only have to write the reason clearly."* This is the ordinary way bugs get
fixed.

## Rule 27: NEVER Generate Device Load to Satisfy a Test — FORBIDDEN, No Exceptions

**Forbidden.** No test, script, benchmark, harness, or acceptance condition may
saturate a shared resource — the block device, the CPU, the memory, the network —
to produce a number. It is a ban: no size at which it becomes acceptable and no
epic that authorises it. **Concretely forbidden**: `dd if=/dev/zero of=...
oflag=direct conv=fsync` in any loop at any width; `while :; do ...; done` around
any writer, reader or syscall touching a device; spawning N parallel workers to
make a metric cross a threshold; any "synthetic pressure" / "load generator" /
"stress" helper, however briefly it is meant to run.

**Our system has no requirement this test serves** — gopi has no latency promise
under device contention and we do not measure `/proc/pressure/io` (a kernel metric,
not a product metric). The load never measured our code; it manufactured a broken
environment and measured that. When a task concludes "I need to create load to see
this", find the direct measurement instead: "it is slow" → measure that operation
(`time`, a duration assertion, a histogram); "slow under contention" → state it as
a hypothesis and file it, do not build the contention; "the suite exceeds the
budget" → measure its wall time and name the slow package/test. **Heavy tests are
classified, not tolerated**: tag them heavy and exclude them from the merge/verify
path; a skip is acceptable only when **listed and auditable** — a silent skip is a
deleted test wearing a passing green.

**Where it is CHECKED**: `internal/testutil/rule27_gate_test.go` — red on a `dd`
with `oflag=direct`/`conv=fsync`, on an unbounded `while :; do … done` wrapping a
device writer, on `dd` naming a raw block device, and on a committed
synthetic-pressure helper (`iopress`, `stress-ng`, `loadgen`, …), scanning the
WORKING TREE including untracked files and dot-directories; plus
`internal/standards/heavy_inventory_gate_test.go` (every heavy-test call site
appears in `testdata/heavy-tests.tsv`; gate packages may NOT be classified heavy).
Heavy tests are **off by default** (`heavytest.Require`; `GOPI_HEAVY_TESTS` truthy).
**On the merge/verify path the rule is: SMOKE, BRANCH-SCOPED — never the whole
tree** (`internal/smokegate`; `TestSmokePathPrescribesNoWholeTreeSuite`).

## Rule 28: Two Hosts, Two Jobs — peer1 Owns the System Layer, HP Owns Features

**The division** (operator's decision, 2026-09-16):

| host | role | work |
|---|---|---|
| **peer1** | server | **system-level**: worktree lifecycle, branch/merge machinery, the merger, task ledger, sync, scheduling — the parts that define how work flows |
| **HP** | client | **feature development** that does **not** touch the system layer |

**Why it is a rule and not a preference**: everything that went wrong in
2026-09-16's incidents went wrong *because both hosts worked the same layer at
once* — fourteen HP branches sat on a stale baseline while peer1 reworked the
same areas, so each one needed a rebase to land and then a compile fix anyway
(`fix/dump-name-collision` merged with "zero conflicts" and did not build). The
division exists so a feature branch comes back **mergeable**, not so the hosts
have different labels.

**What "does not touch the system layer" means, concretely.** A feature task is
HP-shaped if it:
- changes product behaviour a user can see (`cmd/agent/cli_*.go` command
  surfaces, `web/` UI, a `internal/<feature>/` package of its own), and
- does **not** change how work is scheduled, claimed, merged, or cleaned up
  (`cli_tasks*.go` runner paths, `merger*`, `task_reclaim*`, `worktree*` sweep,
  `internal/testutil` gates, `internal/standards` gates).

A task that needs both is two tasks; the system half goes to peer1, the feature
half to HP.

**Migrating a task to the other host is a three-step move, and the cancel is not
optional**: export the payload, submit it on the target host, and **cancel the
original row** with a reason naming the new row id. Without the cancel both hosts
claim it and the work runs twice (Rule 26's duplicate-dispatch allowance is for
*repair*, not for an unowned second run). Tag the new row so its origin is
visible (e.g. `migrated-from-peer1`).

**Before dispatching to HP, check the briefing's own assumptions.** The GUI tasks
migrated on 2026-09-16 each opened with "read
`devdoc/gui-chat-roadmap.md` first, `git show docs/gui-chat-roadmap:…`" — and
that branch **never existed**; the file was on `master` and untracked in one
working tree. A briefing that instructs the reader to `git show <branch>:<path>`
must have that branch verified to exist on the host that will run it, or the
executor starts blind.

## Rule 29: The Main Repo Hosts Work In Progress — Never a Scratch or Reference Target

**Ban**: never point a task's development at a branch/worktree that is not its own
— using a *foreign* worktree as a scratch directory, a test target, a comparison
baseline, or a reference checkout mutates the very workspace its owner is building
in (a worktree is a repo **alias**, sharing the object store and refs). **Correct
practice**: clone, or `git archive` / `git show <branch>:<path>`. Read-only
inspection needs no checkout:

```bash
git show feat/the-other-branch:path/to/file  # read a file without touching a worktree
git diff master...feat/the-other-branch      # compare without checking anything out
git rev-parse --verify --quiet refs/heads/feat/<mine>   # exit 0 = TAKEN → another name
```

**YOUR OWN checkout is the one the runner gave you** (Rule 6a: a dispatched run
does not create one, and `git worktree add` is not a step a run performs).

A borrowed worktree makes the ownership record lie, which is the input the merger
uses to recycle a branch (`internal/tools/branch_attribution.go`).

## Rule 30a: "Aligned" Is Not "Finished" — the Delivery-Point Sync Runs LAST, and Proves Nothing About the Work

**The delivery-point sync (aligning your branch with the integration branch) is
the LAST step before hand-over, not a milestone you tick off along the way.**
Do it once, at the moment you are ready to release the branch. Aligning early
buys nothing: the integration branch keeps moving, so an alignment done an hour
ago is stale — you would have to redo it anyway.

**And a successful alignment says NOTHING about whether your work is done.** It
is a statement about a *merge* ("this branch now contains the integration
branch"), not about your *run* ("the work is finished"). A run can be perfectly
aligned and still be mid-flight: unpushed commits, iteration still going, the
row still `in_progress`. Measured 2026-09-22: two branches were "aligned", their
rows read `in_progress`, and one holder was still alive — while machinery read
the alignment as evidence of completion and freed the worktree out from under
the live run.

**The order that holds:**

1. finish the work
2. commit it (`git status --porcelain` prints nothing — Rule 30)
3. **then** align with the integration branch (delivery-point sync)
4. push / hand over

Never report "done", never let anything treat you as done, on the strength of an
alignment. Only a terminal row whose branch carries the work is done.

## Rule 30: A Run Leaves Its Worktree CLEAN — `git status --porcelain` Prints Nothing

**Before a run reports ready, `git status --porcelain` in the run's worktree must
print nothing** — it is the difference between a deliverable that can be merged and
one that is permanently invisible (a dirty worktree is picked up by no merge path,
while the row may still read `completed`).

```bash
cd <your worktree> && git status --porcelain   # must print nothing
```

**The two shapes of dirt have OPPOSITE remedies**: **untracked entries (`??`) are
litter** — evidence files, logs, generated scripts, scratch checkouts belong
*outside* the worktree (under the session directory) or must be deleted before the
run ends. **Tracked changes (`M`, `A`, `D`, `R`, `C`) are an UNCOMMITTED
DELIVERABLE** — commit them; deleting, or "cleaning the scratch", destroys the
work.

**The rule is checked, not trusted** (a principle is worthless, a check is what counts). At close-out the run's own
recorded worktree is measured, and a non-empty `git status --porcelain` becomes an
attributed record on the row's audit trail — `completed-unclean` for untracked
litter, `completed-unclean-tracked-work` for uncommitted tracked work, quoting the
entries and naming the worktree — plus the same sentence on stderr.
`GOPI_TASK_RUN_RESIDUE_ENFORCE=1` additionally refuses the `completed` writeback for
the run's OWN untracked litter. **An uncommitted TRACKED change REFUSES the
`completed` claim**: a run whose deliverable *is* the code it wrote has not
delivered if that code is in no ref (the branch points at an unmodified base,
invisible to the merge queue while the row reads `completed`), so the
`completed`/`verified` writeback is refused and the row stays OPEN; an operator who
has reviewed the tree may allow that one writeback with
`GOPI_TASK_DIRTY_CLOSEOUT_ALLOW=1` (recorded as its own event). **The gate refuses
the STATUS claim — it never commits, stashes, discards or deletes anything**, and it
stays off a tree that is not the closing run's. **Not a licence to build a
tolerating machine**: do not weaken the "uncommitted work is work" guards
(`reclaimSkipDirtyReason`) or classify untracked paths as "probably scratch, safe
to delete".

## Rule 31: A Command You Did Not Run Is Not Delivered — Execute the Surface You Changed

**A build that compiles and a suite that passes are NOT evidence that your change
works** — they show it did not break what the suite already covers; a code path
never executed has never been tested. **The requirement**: before reporting ready,
every surface you changed must have been **invoked for real, with real arguments,
against real state**, and you must report the invocation and its output.

- **CLI command or subcommand** — run it, not `-h`: the real verb with real
  arguments on a real ledger/repo. A refusal is a result to report; a claimed
  success must be visible afterwards (query what it says it changed).
- **A flag on an existing command** — run the command **with that flag**.
- **A function with a caller you added** — exercise the caller end to end.
- **A new file format / wire payload / protocol leg** — produce one and consume it,
  or send it across the real boundary and read the other side.

**A unit test is not a substitute**: it calls your function the way the test author
imagined, while this rule catches the code that is correct in isolation and
unreachable, or reached with an argument shape nobody ever passes.

**Evidence required** — the `## Verification` section Rule 5 defines: `Build:`,
`Tests:`, `Ran:` (exact invocation, verbatim, with flags and arguments), `Result:`.
**A report saying "implemented" without a `Ran:` line for the surface it changed is
incomplete — the same standing as a report with no document.** If the surface
genuinely cannot be run (hardware, a peer host, credentials you lack), say exactly
that in `## Not Done`, name what is blocked and why, and who can run it: an honest
"I could not run this" is acceptable, a silent claim of completion is not. **Two
constraints, so this cannot be satisfied by cheating:** 1) do not run a load
generator or saturation script to make the invocation "produce" something (Rule 27
bans it); 2) do not widen the thing you are testing to make the invocation succeed
(peer accepts 1..8 rows, you sent 2181 — the defect is on the sender). **Same
principle as Rule 30, one level up**: a surface in no execution has not been
tested.

## Rule 32: A Test Gets Its Database From the Scaffold — Never From a Hand-Rolled Path

**The one call, and it is the default answer:**

```go
path := testutil.NewTestDBPath(t)                       // WRITER: a fresh per-test DB
db, err := sql.Open("sqlite", tools.LedgerDSN(path))    // open it through the tools layer

db := testutil.Sample(t)                                // READER: shared, read-only, seeded
path := testutil.SampleDBPath(t)                        // READER, when a subprocess needs the path
```

`internal/testutil/dbfixture.go` owns both shapes: the writer gives a fresh, empty
database under the pinned scratch root, pins `GOPI_MEMORY_DB` (and masks the legacy
spelling), and refuses a path inside the operator's live profile; the reader is
built **once per run** from one documented seed definition and opened **read-only**
(`mode=ro&_pragma=query_only(true)`, file mode `0444`).

**NEVER hand-roll a database path or a schema in a test.** The anti-pattern is a
helper resolving its own path (`filepath.Join(t.TempDir(), "memory.db")`) and
opening it — a helper taking the path as a parameter is fine (the caller owns the
pin), one that invents the path and never pins the environment is not: with no
`GOPI_MEMORY_DB` in force, a tools-layer call resolves the operator's **append-only**
ledger, where a fixture row is permanent. **NEVER touch the operator's profile**:
`testutil.RefuseOperatorProfileWrite` (and `PathLiesInOperatorProfile`) anchor on
the OS user record, not `$HOME` — a refusal there is the guard WORKING. **Never
commit a binary `.db` fixture** (the seed DEFINITION is the artifact: `SampleSeed` /
`SampleSeedEvents` / `SampleDocs`). **A device-backed fixture belongs in the
scratch home, not the source tree**: take the directory from
`testutil.TestDeviceScratchDir(t, sub)` (off `/`, Rule 22) — a scratch directory
inside the source tree is committed by a concurrent `git add`.

**How a violation is caught**: `internal/standards/db_scaffold_gate_test.go`
(`go test -run TestTestDBScaffoldIsUsedOrTheLedgerEnvIsPinned ./internal/standards/`)
flags (a) a test function or helper that resolves a database path itself and opens
sqlite without pinning the DB environment, and (b) a scratch directory created
inside the source tree; a reviewed exception carries `db-scaffold-ok: <reason>`.
**The gate is PARTIAL** — an opener reached through a variable, or a test that pins
the env then ignores the pinned path, is not caught.

## Rule 33: `parent_task_id` Is a Dependency GATE — a Child Runs Only After Its Parent Is Released

**The gate.** A pending row is claimable **iff** its `parent_task_id` is empty **OR**
the parent row's status ∈ **{completed, verified}** — the EXACT release set; every
other parent status parks the child, and a `parent_task_id` naming **no row at all**
is also NOT claimable. The check is ONE predicate — `taskRowClaimable`
(`internal/tools/tasks.go`), via `taskParentGateDenied` — so the claim funnel
(worker round / `gopi task next` / serve) and the pending view's `[queue-skip]`
marker agree.

```bash
gopi task submit "<child briefing>" --parent-task-id <parent-id>   # chain construction
gopi task list --status pending
# t-…  pending  …  [queue-skip] parent t-parent  not released (status=pending; releases on completed|verified)
# t-…  pending  …  [queue-skip] parent t-ghost   missing from the ledger
# t-…  pending  …  [next]                        ← the row a dispatcher takes next
```

**The anti-pattern**: treating `parent_task_id` as a *lineage note* rendered by
`task show` and respected by nobody, so a child whose parent is still running is
dispatched against a premise that does not hold yet — and its mirror, "the parent is
nearly done, just claim the child". **The check**: `go test -run TestParentGate
./internal/tools/` drives the REAL surfaces (`TaskNextFiltered`, `TaskList`) and
pins the red path: a pending parent parks the child **while an unrelated row is
still offered** (a park is never a head-block), a released parent hands the child to
the very next claim, a dangling parent id is denied explicitly, and the `[next]`
marker agrees with the row the funnel actually claims.

**The operable resolution — there is no bypass.** Finish the parent (`gopi task
update <parent> --status completed`) or cancel the child (`gopi task update <child>
--status cancelled`); there is no override flag, no cascade cancel, no
timeout/staleness release and no `--force`, so a `cancelled` or `failed` parent
parks its children **forever by design**. `gopi worker <explicit task-id>` still
runs a parked row — the gate governs the DISPATCH funnel, the explicit id is the
operator's own door. **Honest partial coverage**: it by design does not stop
`gopi worker <task-id>`, `gopi task resume`, or a row driven by the sync layer on a
peer ledger.

### 33b. The THIRD edge — a row may wait for a GROUP to finish, not just for a parent to release

**The defect this closes** (measured 2026-11-05): under a DISPATCHER parent the
parent's work **is** to submit its children, so its `completed` means "everything is
dispatched" — never "the batch is done". A row that CONSUMES the batch is a
**sibling** of the producers under that same parent, so the parent's release made the
consumer claimable at once: the pending view offered it `[next]` with **0 of 23**
inputs on disk. `parent_task_id` cannot express "wait until a SET that is not my
ancestors is finished", so the row **declares** the edge in its own payload:

```
task_group: <prefix>[,<prefix>...]
```

TWO ledger facts make this spelling safe, and both are the existing ones — **no new
config key, no new table column, and the release set above is untouched**:

- A group is the set of ledger rows whose **`task_id` begins with the prefix**. That
  is the ledger's own naming convention (a dispatcher files its children in one
  session-id family), so membership needs no stored list.
- The criterion is **TERMINAL, not released**: a producer that `failed` or was
  `cancelled` still finished its attempt, so the consumer waits for the batch to be
  **over**, not for it to be good. This is the SAME distinction the REWORK arm above
  draws, and it is what makes the edge usable at all — the release set could never
  release a group whose members were cancelled.

**Three states the refusal distinguishes** (Rule 15 — a refusal names its own cause):

```
gopi task list --status pending
# t-…  [queue-skip] group <prefix> waits for 2 of 3 row(s) to reach a terminal status, self excluded (still open: <id>, <id>)
# t-…  [queue-skip] group <prefix> matches NO row in the ledger — an empty dependency is never satisfied (its rows were never submitted, or the prefix is wrong)
# t-…  [queue-skip] group status unreadable — the ledger could not be read, so the declared group dependencies cannot be judged (a row declaring one is DENIED rather than assumed satisfied)
```

**The three deliberate design choices, each of which a "simplification" would undo:**

1. **An EMPTY group PARKS.** `task_group:` with nothing after it, or a prefix that
   matches no row, is a premise that never held — so it is denied, naming its own
   cause. Dropping an empty entry in the parser would silently turn a declared
   dependency into "no dependency", which is the exact failure mode the edge exists
   to end.
2. **The row EXCLUDES ITSELF.** A consumer whose own id falls inside the group does
   not block itself (a dispatcher's own successor is the ordinary case), and the
   reason says `self excluded` so the arithmetic a reader does on the ledger
   reconciles with the count in the refusal.
3. **A nil status index DENIES a declarer** instead of assuming the dependency holds
   — and the index is loaded when the pending queue carries **either** a parented row
   **or** a group-declaring one (`taskDependencyIndexNeeded`, ONE predicate for all
   four readers: the claim funnel, the pending render, the `[next]` resolver and
   `task show`). The old parent-only probe would have handed the gate nil and parked
   a **satisfied** row.

Both edges AND together and each names its own cause, joined by the gate's existing
separator (`;`): releasing the parent leaves the group's sentence, and vice versa.
The pending view's `[queue-skip]` marker and the claim funnel remain ONE predicate
(Rule 14) — a view that marked a row `[next]` the funnel refuses is the drift this
rule's first paragraph forbids.

**The check**: `go test ./internal/tools/ -run TestGroupDependency` drives the REAL
surfaces (`TaskNextFiltered`, `TaskList`, `taskParentStatuses`) and pins the minimal
reproduction — P `completed` with A and B open ⇒ C not claimable, named with its
count; A `completed` and B `cancelled` ⇒ C handed out by the very next claim — plus
the empty group, the self-exclusion, the nil-index denial, the parser's line scope,
the group-only queue loading the index, and coexistence with an unreleased parent.
**Honest partial coverage**, the same as the parent edge's: this governs the DISPATCH
funnel, so `gopi worker <explicit task-id>` still runs a parked row.

## Rule 34: The Sandbox Is ONE Configured Shape — Never an Invented Directory

**Get the sandbox by ASKING for it** — never build one by hand:

```bash
SB=$(gopi tool sandbox create --label build --task-id "$TASK_ID" | tail -1)   # the ONE initializer
gopi tool sandbox show                                                       # where sandboxes live, and which channel decided
gopi tool sandbox run --label build --task-id "$TASK_ID" -- CMD ARGS...      # materialise AND run CMD inside it
GOPI_HOME="$SB" <your command>                                          # a private profile, the standard shape
GOPI_HOME="$SB" gopi session log --last                                 # prove it: the sandbox's OWN sessions
```

**A sandbox is an EXECUTION ENVIRONMENT, not just a path — bind the child to it.**
`gopi tool sandbox create` answers *where*; it does not make anything run there. A child
launched by absolute path inherits the **caller's** working directory — inside a task run
that is the run's own git worktree — and when the launcher is killed the orphan keeps
`cwd=<worktree>`, so finish-cleanup's holder scan refuses that worktree **forever** and it
is RIGHT to: a live process is standing in it. The producer-side fix is the
allocation+execution entry point:

```bash
gopi tool sandbox run [create flags] -- CMD ARGS...     # materialise, then run CMD INSIDE it
```

It resolves the root through the same ONE resolver, materialises through the same ONE
initializer, and executes the child with **its working directory set to the sandbox** —
gopi itself **never chdir's** (`cmd.Dir` per exec), so the launcher's own directory does
not move. `GOPI_HOME`/`GOPI_SETTINGS` point into the sandbox, the sandbox path is the last
stdout line, and the child's exit status is gopi's (`128+N` when a signal killed it). A
caller that keeps the manual `GOPI_HOME=…` form must give the child the sandbox as its
working directory too — the export binds the PROFILE, never the CWD, and it is the CWD
that pins the worktree.

**The shape is fixed, and every sandbox is private** — and the PROFILE ROOT is
`<sandbox>/.gopi`, because `GOPI_HOME` is a HOME: every reader resolves
`<GOPI_HOME>/.gopi/{sessions,raw,logs,settings.json,memory.db}`
(`config.GopiDir`). The initializer materialises exactly that path, so a reader
started with `GOPI_HOME="$SB"` finds what `gopi tool sandbox -h` describes — no
directory has to be created by hand, and
`GOPI_HOME="$SB" gopi session log --last` lists the sandbox's own sessions (the check
that caught the two halves disagreeing). Inside the profile root:
`settings.json` — a **read-only COPY (mode `0444`) of the operator's real settings
document, NEVER a link** (measured, 2026-09-27). The profile used to hold a symlink
"read-only by contract", and that contract was unenforceable: the permission bits of a
symlink are the TARGET's, and a shell redirect does not ask — `printf '{…}' > "$SB/.gopi/settings.json"`
followed the link and **truncated the operator's live settings document**, taking the
`api_key` and the `repos` table `gopi task merge` keys on. So the shape is a copy: the profile
still answers the operator's own keys (that is why the link existed), a write to it is
refused by the kernel instead of reaching the live file, and a sandbox run that needs its
own knob declares it in its OWN settings file (`GOPI_SETTINGS`), never as a write into the
profile. The trade is that a copy can go stale while the operator edits the live document —
`Create`/`Ensure` are the repair path, and irreversibility is what it buys out
(`internal/sandbox/settings_doc.go`); `plugins` — a **symlink** to the REAL plugin tree,
**read-only by contract** (a sandbox that could not see the operator's plugin tree could not run
the web-search plugin at all: the tool resolves `<GOPI_HOME>/.gopi/plugins/websearch/scripts`,
so the `plugins` level of the profile is what gets linked — a copy would drift the moment
the operator's tree is updated; and POSIX `rename(2)` **replaces the symlink node**, so a
"saves" path on this entry silently swaps the link for a regular file — the trap lives HERE
and on the ledger, not on the settings document, whose copy has no node to swap);
`memory.db` — the sandbox's **own ledger: a REAL, private, WRITABLE
file** (mode `0600`), created by the initializer at the path the **ledger
resolver actually computes** for this profile (`GOPI_HOME` +
`config.ProfileDirName` ⇒ `<sandbox>/.gopi/memory.db`), seeded by **COPY** when
`GOPI_SANDBOX_LEDGER_SEED` names one, and `0600`-empty when it does not;
`sessions/`, `raw/`, `logs/` — private, empty, writable; and beside the profile
root `.sandbox-owner.json` — task id + pid + kernel start ticks + created-at,
the reclaim marker. A sandbox configured by its OWN contents is refused by the
initializer.

**The ledger is a WRITE TARGET, so it is never a symlink** (measured 2026-09-26). A sandbox
is a place to RUN a gopi process, and its ledger is the **first file that run writes**. The
pre-fix shape linked `memory.db` at the **shared read-only sample** (0444), which produced
one of two failures the moment a real run touched it: the open was refused — `memory_store:
attempt to write a readonly database (8)` — or a writer wrote a sibling and **renamed it into
place**, silently **replacing the symlink node** so the sandbox read a different file than it
pointed at. Meanwhile the path gopi actually resolves (`<sandbox>/.gopi/memory.db`) was
materialised by **nothing**, so the sandbox's real ledger did not exist. The sample is still
the ONE definition of a populated database — it is used as a **seed, by value**; a sandbox
that needs rows the sample lacks uses the WRITER scaffold (Rule 32), never a second
generator. Two lines the shape obeys: an **existing** ledger is never rewritten (a sandbox's
own rows are its history), and a **symlinked or read-only** ledger is **refused by name**, not
silently replaced — writing through a link targets a shared artifact, and replacing it hides
the change.

**NEVER invent a sandbox.** The named anti-pattern: a per-script or per-run
directory built by hand — `SB=$(mktemp -d)`, `SB=/home/user/workspace/sbx-$$`, an
`os.MkdirTemp` inside a test body — with `GOPI_HOME` exported into it; each is a
different shape, none is reclaimed, and each is a second, unreviewable answer to
"what does an isolated profile contain?"

**And the MOST EXPENSIVE form of it is a copy of the operator's ledger** —
`R=$TMPDIR/probe-$(date +%s); mkdir -p "$R"; cp ~/.gopi/memory.db "$R/memory.db";
GOPI_MEMORY_DB=$R/memory.db gopi …`. It is listed separately because it is
KB-scale in the two shapes above and **MiB-scale here**, and because it is the
shape a careful author reaches for: a run that must prove something about a row
has to write the row, the operator's ledger is append-only, and a private
writable copy looks like the safe answer. It is the safe answer — **and the copy
is a 645 MiB file that no reclaim path owns.** Measured (task
`01a1955b-2775-736d-8728-b5f9211436e5`, 2026-11-01): six such directories in
13 minutes, `675942400` bytes each / `3.8 GB` total, under `<home>/workspace/_tmp`
where the sweep's `go-tmp` row matches only `go-build*` — so all six fell in
`not_ours`, `collectable=0`, **and were still there**. The volume was never
required: `schema` costs **131,072 bytes**, the reproduced shape (`chats`, 274
rows) costs **0.12 MiB**, and the remaining 81.4% was the knowledge-base index
history riding along on a `cp` nobody asked for.

Get the SAME capability through the ONE initializer, at 1/3500 the size and
inside the reclaim path:

```bash
SB=$(GOPI_SANDBOX_LEDGER_SEED=$HOME/.gopi/memory.db \
     gopi tool sandbox create --label probe --task-id "$TASK_ID" | tail -1)
GOPI_HOME="$SB" GOPI_MEMORY_DB="$SB/.gopi/memory.db" gopi chats </dev/null 2>&1 | head -12
```

`GOPI_SANDBOX_LEDGER_SEED` copies the seed ONCE into the sandbox's own writable
ledger (never a link, never the seed itself — `internal/config/settings.go`
carries the refusal text), the sandbox root is reclaimed by
`gopi tool scratch-gc sandboxes` on its own tick, and the seed can equally be a
128 KiB empty ledger built by `gopi task list` when only the *shape* is needed.
Measured on that row: sandbox **188 KB** vs the copy **645 MiB**, and
`scratch-gc sandboxes --dry-run` lists it (`keep … reason="not aged"`) while the
hand-built copies appeared in no arm at all.

**The location is CONFIGURATION — never a hard-coded path**, resolving exactly like
`tmpdir` through ONE function (`config.ResolveSandboxRoot`):

```
--sandbox-dir PATH  >  $GOPI_SANDBOX_DIR  >  settings.json "sandbox_dir"  >  <home>/workspace/gopi-sandbox
```

There is deliberately **no ambient channel** (nothing outside gopi reads a sandbox
root); ask with `gopi tool sandbox show` (`CONFIG.md` carries the same row) and never
spell a path in a script.

**Reclaim is AUTOMATIC — do not write a cleanup hook.** `t.Cleanup` does not run on
SIGTERM/SIGKILL, exactly when an abandoned sandbox appears, so the reclaimer rides
machinery that already ticks: `gopi tool scratch-gc sandboxes` (the host-level one-shot
the existing watchdog executes) and the existing resident worker/merger tick
(`scratchGCTick`). Both apply ONE predicate (`internal/scratch/sandbox_gc.go`,
`SweepSandboxes`): collected only when **ours** (name marker `sandbox-`) AND
**aged** (72h, `scratch.SandboxStaleAge`) AND **unowned** (no live incarnation of the
recorded pid + start ticks); a live process inside it (`/proc/<pid>/cwd`) vetoes the
delete and **every unknown is a KEEP**. The documented root is refused by name, and
the **root itself is never removed at any age** — only its aged, unowned children.

```bash
go test ./internal/sandbox/ ./internal/scratch/ ./cmd/agent/ -run 'Sandbox|Sweep|ScratchGCTick'
gopi tool scratch-gc sandboxes --dry-run    # what WOULD go, with the reason per sandbox
```

## Rule 35: Zero Dependency Is a Discipline — Nothing New Is Created Casually

**The incident that produced this rule** (2026-09-12, found 2026-09-26): a gopi
session was asked to fix two HP-side worker starters that launched workers without
checking the exit status. It did the right thing to the launchers — and then, to
make them reliable, it **installed a cron line**:
`*/10 * * * * /home/user/bin/peer-launcher-autostart.sh`. Author of both the script and
the install: `gopi <gopi@gopi>`, commit `4fade437`.

Nobody knew it was running. Its log reached **36,398 lines / 1,214 delegations**,
and it was **covering a bug**: workers kept dying, and the every-10-minutes line kept
replacing them, so "always a worker alive" hid "workers keep dying." The operator's
verdict on discovering it:

> "This cronjob must be stopped. We cannot rely on this kind of system to do the work — it covers the problem up."

**RETIRED 2026-09-26 — the line is gone and the script now REFUSES an automatic
caller.** `scripts/peer-worker/peer-launcher-autostart.sh` and its sister
`peer-launcher-nightsafe.sh` exit non-zero and print the ruling when invoked from a
scheduler context (no terminal on stdin, or a `cron`/`atd`/`anacron` ancestor in the
process table); an operator in a terminal may still run them by hand. **Do not
re-install the cron line: no replacement auto-launcher, no escape hatch**.

and then, as a standing principle:

> "We need to lay down a rule in gopi.md: do not create dependencies casually — not resources, not programs, scripts,
> storage files, config files, extra databases, extra system calls and so on. Be very careful about this. Our
> zero-dependency stance is a matter of conviction. Hold to it."

**Zero dependency is not a slogan; it is the property that lets gopi be copied to a
machine and run.** One binary, a settings file, and nothing else. Every artifact you
add silently moves that boundary, and the artifacts outlive your reasons for adding
them — the cron line above outlived its author's context by two weeks, and nothing in
the system could tell anyone why it existed.

### The gate: before creating ANY new persistent dependency, you must say the four things

A **persistent dependency** is anything that exists after your process exits, or that
exists on a machine you do not own the lifecycle of. Non-exhaustively: a cron/at line,
a systemd unit or timer, a daemon, a shell script or CLI helper, a state file, a new
config file or a new config key, a new database or table, a lock file, a socket, a
mount, a symlink, an environment variable that must be set in someone's shell, or a
new external command your code now requires at runtime.

You must be able to state, in this order, and you must state it in the task's report:

1. **What breaks without it.** Name the concrete failure — not "it would be fragile",
   but "X happens, and here is the run where I saw X." If you cannot point at an
   observed failure, you have not earned a dependency.
2. **Why the failure cannot be fixed in-process.** Why can this not be handled by code
   that already runs — in `gopi`'s own Go paths, in the worker/merger tick that
   already exists, in the tool the user already invokes? **Most wants die here**, and
   dying here is the intended outcome.
3. **Who will own its lifecycle — and specifically, how it gets cleaned up.** Creation
   without a named owner, a reclaim path, and an observed reclaim is not a design, it
   is litter. "It'll be fine" is not an owner.
4. **What it looks like to the next person who finds it running and does not know why.**
   If the answer is "nothing tells them", the dependency is not ready to exist: a
   persistent artifact must deny-by-default and explain itself (Rule 15 — silence is
   the defect, not the inconvenience).

### The defaults, so you do not have to think about the common cases

- **Prefer what already runs.** `internal/scratch` GC, `scratchGCTick`, the worker /
  merger tick, an existing CLI verb, an existing tool. A new thing that does the same
  work as a thing that already ticks is a violation of this rule, not a feature.
- **Prefer a one-shot you invoke over a resident that invokes itself.** The measured
  reason is in this repo's own history: a resident process makes "who did that?"
  unanswerable, and the operator cannot debug a system with an invisible hand in it.
- **A script is not a deliverable for logic gopi should own.** This has been ruled
  repeatedly ("gopi is a single program of its own — why does it need a script?"). If the capability
  belongs to gopi, it belongs in gopi's Go code with a configuration channel
  (Rule 3, and the three-in-one precedent `internal/config/task_sync_switch.go`), not
  in a file beside it.
- **An escape hatch is the rule reversed.** Adding `--force`, an allow-list, or a
  "temporarily" override to get past this rule defeats it — the whole point is that
  the artifact must not exist.

### Why this is more than tidiness

**Two of this repo's worst recurring failures are dependency failures, not code
failures.** A worker that keeps dying is a code problem; a cron line that keeps
restarting it is a *dependency* that converts a diagnosable code problem into an
undiagnosable system. The same shape produced the 1,214 delegations above, and the
operator named the mechanism exactly: **it covers the problem up.**

So when you are tempted to make something reliable by adding something *around*
gopi — that is the moment this rule is for. Fix the thing. Do not wrap it.

### How to satisfy this rule when a dependency IS the right answer

Say the four things, then make it **refuse by default and explain itself** (see the
retirement of the HP autostart, `briefings/brief-retire-peer-launcher-autostart.md`, for
the shape): it must not run unattended unless it was designed to, it must name this
rule and the reason in its own header, and there must be a test that fails if the
silent form comes back. A persistent dependency that a future reader can neither
identify nor safely remove is worse than the problem it solved.

**Two surfaces check this rule, and each covers a half the other cannot.**
`gopi tool scratch-gc deps` reports what is **installed and resident on this host right
now** — the crontab, the unit files that can schedule or reside, and the resident
gopi-shaped processes with each one's detachment verdict measured from
`/proc/<pid>/cgroup` (Rule 24). It is read-only: it writes no crontab, touches no
unit, signals no process. The repo-side half — a self-scheduling script, a
scheduler-install recipe, a tracked state/lock artifact — is a test:
`go test ./internal/testutil/ -run Rule35` (`internal/testutil/rule35_dependency_gate_test.go`).
**A gate can read a tree; it cannot read a crontab** — the retired autostart proves
the direction that cost us: the script was in the tree, but the cron line that ran
every ten minutes lived in a crontab, where no test in this repository can see it.
## Rule 36: A Doc That Missed the Write Hook Is Classified in the SAME Commit

**The behaviour.** Every markdown file under `devdoc/` declares its truth class
on **line 1 of itself**: `<!-- doc-class: normative -->` (classes:
`normative`, `historical`, `roadmap`, `index-meta` — `internal/devdoc.Classes`).
The header is the ONLY record: there is no table to keep in step, no second
artifact to forget, and no derived row to regenerate.
`devdoc/doc-class-lives-in-the-file-header.md` is the mechanism's one window.

The `write`/`edit` tools make the header for a document **as it lands**
(`internal/tools/header_hook.go`: `amendDocHeader` before the bytes, then
`preserveDocHeader` on edits, both inline string work — no subprocess, measured
19.3 µs against the 14.76 s the old `classify.py --only` leg cost). But a devdoc
can still reach the disk through a door with no hook: a person's editor, a
session writing files directly, or **the write tool itself failing after the
bytes landed** (measured 2026-09-26: `write: … timed out after 30s` on two
handoffs — the file was on disk, the hook never ran, and the corpus was short of
two classifications). A half-classified corpus is not a cosmetic problem: it is
the state in which the genre of a page is unknown, and the only thing that
notices is a standards test that has not run yet.

**The practice.** A devdoc that did not go through `write`/`edit` is given its
header **in the same commit that lands it** — never "later", never left for the
next task to trip over. The close-out asks the same question on the branch's own
ground (`finishCleanupDocClassSync`): every markdown document the branch ADDS must
declare its class, and one that does not **withholds the delivery with its name in
the refusal**. That step only READS — it never writes a header, because the class
is the author's statement of what the page is and a guessed one is the filename
heuristic the old table existed to avoid. The act is one line at the top of the
file:

```
<!-- doc-class: historical -->
```

**The gate is the check, and it says which file and why.**

```bash
go test ./internal/standards/ -run TestEveryCorpusDocDeclaresItsClassInItsHeader
```

It walks `devdoc/**/*.md`, requires a well-formed banner on line 1 of every one,
and **names every failure** — missing, malformed (`<!-- doc-class: historical —
classified in … -->` is an error, not a near miss) or an illegal value — printing
`<path>: <reason>` per file plus the files walked and the class counts, so a scan
that looked at nothing cannot pass as a clean corpus. Non-markdown records under
`devdoc/` are **out of scope by a written rule** (`internal/devdoc.IsProse`): a
capture, a fixture or a TSV cannot make a standing claim, so it is historical by
definition and owes no comment a reader would never meet.

**The same promise keeps a RETIRED capability out of the corpus, and that half
is a gate too.** The document pipeline is gone (`tools/docclass/`,
`gopi tool doccheck`, `internal/docindex` — `e6bfa10`, `29882b3`), and the
annotation sweep that stopped the corpus teaching it was a ONE-OFF that nothing
checked. A normative or roadmap document that still pastes one of those commands
must carry the `Retired mechanism (…)` note ABOVE the first invocation — a note
below it is no note to the reader who already pasted the command — and a man
page may not name the surface at all, because a page is the official promise.
Historical records are counted and not judged (they must be free to quote what
was typed that day), the recognizer is COMMAND POSITION rather than mention (a
page saying "the verb is not registered" is the fix, not the defect), and a scan
that judged too few files or recognized too few invocations is refused as a
vacuum rather than passed.

```bash
go test ./internal/standards/ -run RetiredPipeline
```

**Retired gates a normative page must not cite as live.** Two gates died in the
teardown and keep turning up in the corpus as if they still ran, so they are named
here: `TestDocAnchorsResolveInNormativeDocs` (`internal/agent/docanchor_test.go`)
was retired in `5a35f61` together with `internal/docclass` — no suite resolves a
devdoc `file.go:NNN` anchor today — and `TestTheShippedCorpusCarriesNoStoredCellDrift`
(`internal/docindex`) went with the `doccheck` verb in `e6bfa10`. Anchors stay and
line numbers stay: they are a landing-time record a human still reads. What must
not stay is the ASSERTION that something checks them, which is why the sweep above
re-keyed every `normative` page that made it — a `historical` page quoting the gate
in the past tense is exactly what a record is for.

Measured on this rev: `walked 2454, judged 813, historical-skipped 1641,
command-position invocation 12, annotated 12, exempted 0` — six documents were
annotated by this sweep and six already carried their note, and the permit table
is EMPTY on purpose (a note costs a paragraph; a permit costs every future
reader the question "what did this one get away with?").

**This command cited nothing for months, and that is the failure shape it now
detects.** Until `internal/standards/docclass_corpus_gate_test.go` landed
(2026-10-22) `-run TestEveryCorpusDocDeclaresItsClassInItsHeader` matched no
test: `go test` printed `no tests to run` and exited 0, so the rule's own check
was a vacuum that read green. A named check that does not exist is worse than no
check, because the green is indistinguishable from a clean corpus — so the gate
refuses a green it did not measure: it reports `files walked / files judged /
files clean` plus a count per class, `judged` must clear `docCorpusScanFloor`,
and a scan that judged nothing fails. Verified on this tree 2026-10-22:
`walked 2538 file(s) under devdoc/, judged 2368, clean 2368; normative=734;
historical=1622; roadmap=12; index-meta=0`.

The gate only READS. It never writes a header: the class is the author's
statement of what the page is, and a gate that guessed one would reinstate the
filename heuristic this rule exists to have removed.

**Why the header and not a table** (the mechanism this replaced, for a reader
who finds an old report describing it): a manifest column was a second file every
author had to edit, so two branches adding a document met in it and the merge
conflict was the table's mechanism announcing itself; the class was invisible at
the top of the page; and producing the row was subprocess work (~15–29 s per
path). The replacement is complete as of this rev: `tools/docclass/`,
`internal/docindex/` and `internal/docclass/` no longer exist, and the
`gopi tool doccheck` verb is not registered (`cmd/agent/cli_doccheck.go` is
absent) — the deletion chain `01a16043-2b13` is `completed`, not queued. This
rule depends on none of them, and a document that still teaches a reader to run
`python3 tools/docclass/...` is describing a mechanism this repository has not
got.

One piece survives, and it is worth naming so a reader does not mistake it for
this rule's mechanism: `devdoc/DOC-INDEX.tsv` **is no longer tracked.** Commit
`475001d` ran `git rm --cached` on it and added a `.gitignore` row, on the
operator's instruction — a generated table conflicted on every branch that added
a document, and no production Go code read a row from it. What still exists is
the merge lane's handling of the artefact (`cmd/agent/merge_union.go`,
`internal/tools/manifest_union_gate.go`): where a conflicted index turns up it
is resolved by unioning both sides' rows, and `internal/devdoc` still owns the
vocabulary that reading needs (path, column line, parser, banner, class names).
**A reader must not fetch a row from it to answer what class a document is** —
the table is regenerable, gitignored, and absent from a fresh ground. That
question is answered by line 1 of the document, and only there.


## Rule 38: A Census Figure Names Its Generator and the Commit It Was Measured At

**A figure a reader ACTS on must be re-derivable, or it is a rumour with a number
attached.** A CENSUS figure counts the project as a whole ("N tracked files", "N
verbs dispatched", "N% of tool calls"); a NARRATIVE figure is evidence of ONE run
(a log line, a pid, a 41-line window, a byte offset) and stays in prose, because
"fixing" it destroys the record it is. The boundary is the figure's SCOPE, not its
size: whole project → census; one run → narrative.

**Practice**: a census document cites the GENERATOR that prints the figure
(`scripts/gopi-stats.sh`, `scripts/g7-verb-census.sh`) and the COMMIT it was
measured at. `scripts/gopi-stats.sh` is the test, with four properties — it NAMES
a base commit, PRINTS one number per dimension (no prose arithmetic), RE-RUNS at
any rev out of the OBJECT STORE (`git grep -c ''` at both revs, never a second
checkout — Rule 29), and is READ-ONLY over the repo, warning rather than failing
when the worktree is dirty (a dirty tree corresponds to no commit).

**THE COMMIT A CENSUS CITES MUST BE ONE A REBASE WILL NOT REWRITE.** This is the
half of the rule that kept regressing (2026-10-27, second occurrence): both shipped
census tables cited a commit that was an object in NO clone, so `CitationCensus`
and `TestMergeConfigAnchorGate_RealTreeIsClean` went red, somebody re-derived, and
the repaired table was red again one rebase later. The mechanism is not a forgotten
re-derivation — `git rebase` REPLAYS commits, so a replayed commit keeps its tree
and gets a new id, and the old one survives only until `gc` collects it. A table
that cites the tip it was measured at therefore cites a commit the delivery loop is
about to replace: `internal/devworkspace/devworkspace.go` runs
`clean → pull --rebase → …`, so "re-derive on the rebased tip" moves the citation
onto the next tip that will be replaced. Measured: of every `chore(census)` commit
in this history, exactly the citations naming an ALREADY-PUBLISHED revision (or the
writer's own parent) survived, and every citation naming an unpublished branch tip
is now `fatal: Not a valid object name`.

So a census citation obeys a PUBLICATION requirement, not just a "re-derive"
procedure:

- **Anchor on a revision the rebase will not author** — one already reachable from
  the integration branch. Measured over every citation in this history: 18 of 18
  that name a PUBLISHED revision survive, 8 of 8 that name an unpublished tip are
  now dead. "It is my parent" is a coincidence, not a criterion (8 healthy
  citations name a non-parent).
- **If a fresh tip is unavoidable, measure AFTER the last history rewrite.** The
  delivery loop re-derived *after* its own `pull --rebase`, through a re-drive leg
  (`internal/devworkspace/census_redrive.go`, since deleted in `0ee5745c` with the
  generators it drove), so the citation named the tip that push publishes. A run
  that re-derives at its own HEAD and then hands over did the measurement one
  rewrite too early — that IS the recurrence, and it outlived the mechanism: it is
  the criterion any future census generator has to meet.
- **Never hand-edit the `# measured-at:` line.** The generator WAS the only writer
  and it refused a reading that is not a commit; its repair command rode the
  table's own header — and neither can be reached now: the generator, the table
  and the header all went with the teardown in `78bdf05a` (2026-11-05).
- **Prefer a published revision where you have the choice** — both generators READ a
  revision straight out of the object store (`--at REV`, no second checkout, Rule
  29), which was immune to this class by construction: the shape to keep if such a
  generator ever comes back.
- **And the write leg USED TO refuse the defect instead of trusting the author** — the
  practice above was not enough (the sanctioned repair still ran at a head a rebase
  replaced, while `--check` stayed green because a local branch ref DOES reach it).
  Each generator's write leg asked PUBLICATION when the line was written and had four
  answers, with no fallback to citing an unpublished tip: cite HEAD when a ref a
  rebase does not rewrite carries it; else cite the nearest ANCESTOR that such a ref
  carries AND whose committed copy of the table holds exactly the derived rows; else
  REFUSE (exit 2, nothing written) naming the published candidates the row
  comparison refuted; or `--publishing-head` for the delivery channel's own
  re-derive leg, which is about to publish exactly that commit
  (`censusPublishingCommandFor`; the flag was refused on `--check`/`--print`, and the
  repair command a table's header printed did NOT carry it). An unmeasured probe was
  a refusal, never "unpublished" (Rule 15). **`censusPublishingCommandFor` and the
  `--publishing-head` flag were symbols of the deleted generators and do not exist
  anywhere in this tree**: the arms (`TestCitationCensusWriteLeg`,
  `TestMergeConfigAnchorGate*`) went with the teardown in `78bdf05a`/`b8cad761`, so
  what survives is the RULE, not an executable leg — the four answers above are the
  criterion a future census generator has to meet.
- **A red is not automatically the table's fault.** A row delta (`-table`/`+tree`)
  means the document moved and the TABLE must be re-derived; a bare provenance
  failure means only the citation died. Fixing the citation alone leaves a green
  citation over a wrong population — which is what the second red in the 2026-10-27
  occurrence was (`400f837` moved `rows: 526 → 527` and added
  `cmd/agent/task_ledger_agreement_structural_test.go` while it was dying).

The full mechanism, the measured table of every orphaned citation, and why the
re-drive leg the delivery loop then had (`internal/devworkspace/census_redrive.go`,
deleted in `0ee5745c`) could not rescue a dead hash are in
`devdoc/census-citation-cannot-survive-a-rebase.md`.

**PUBLICATION WAS MEASURED until the teardown, and the earlier version of this
rule said it could not be.** The probes were "is an object here"
(`git cat-file -e`) and, for the citation table, "is it an ancestor of HEAD" — both
answer about the LOCAL object store, so a citation of a commit every ref had been
rewritten past printed GREEN until `git gc` collected the unreachable object and
only then turned red. That was a missing measurement, not a prediction problem:
`checkCommittedCitation` in both generators (then
`internal/standards/testdata/config-anchor-census.go` and
`internal/standards/testdata/citation-census.go`, both deleted in `78bdf05a`) ASKED
a third, INDEPENDENT question after existence — is
the revision reachable from any ref (`git for-each-ref --contains <rev>`) — and
REFUSED with its own sentence, the PUBLICATION gap, when nothing reached it. **That
code is gone: `checkCommittedCitation` is not a symbol this tree has (a repository
wide search for its definition returns nothing), so the two questions below are the
criterion, not a call a reader can make. The
two refusals stay SEPARATE on purpose: "not an object" (the rebase shape: gone) and
"an object no ref reaches" (the publication gap: here and collectable) are different
findings with different repairs, and a diagnosis that printed both would tell a
reader nothing about which one was measured.

**What still cannot be measured**: a citation to a commit that exists, is reachable
from a ref HERE, and will be rewritten LATER by the delivery loop's rebase — a local
unpublished tip satisfies all three probes because it is published in this clone.
Choosing an already-published revision, or writing the citation after the last
rewrite, stays anchoring practice enforced by authors (Rule 35: the fix is a rule,
not a new resident mechanism).

**Check**: `go test ./internal/standards/ -run TestCensusGate -count=1 -v` — a
title-declared census document carrying a project-wide figure must cite BOTH, or
carry `census-evidence-ok: <reason>` (printed, with the escape count ratcheted so
a permit cannot grow quietly). The gate is
`internal/standards/census_evidence_gate.go`; its tests are named
`TestCensusGate*` — `TestCensusGatePinsTheCommitThatResolves`,
`TestCensusGateNamesTheGenerator`, `TestCensusGateEscapeIsStatedNotSilent`,
`TestCensusGateScopeRuleIsTheTitle`, `TestCensusGateRefusesAVacuum`, and the
real-corpus run `TestCensusGateJudgesTheRealCorpus`, which PRINTS
`walked … judged … passing … escaped … red …` and enforces its non-vacuity
floors in both directions.

**This citation was a vacuum for a month, and that is the fact the rule now
teaches.** The gate and its test were deleted whole in `29882b3` (the
document-pipeline slice) because the implementation imported `internal/docclass`,
which that slice removed — and this Check line was left untouched, so the command
it names matched no test, printed `no tests to run` and exited 0. **A named check
that does not exist is worse than no check at all**: its green is
indistinguishable from a clean corpus. So the paragraph above carries the gate's
own file name and its test names (the shape Rule 36 states for its corpus gate),
and the restored test opens by naming the deletion it repairs. Anything a rule
cites must be runnable — and "runnable" means `=== RUN`, never an empty match.

## Rule 37: A Chat MAKES SMALL CHANGES IN PLACE — It Dispatches the Big Ones

**A `gopi chat` session MAY edit, stage and commit a SMALL change in the tree it
was started in** (operator's ruling, 2026-10-30; the earlier absolute prohibition
was retired, and the `chatpolicy` `ChatOpClass` table is the implementation). A
console that cannot commit cannot finish anything — the rule exists to stop a
console from becoming an unreviewed second workflow, not to stop it working.

**The boundary is SIZE plus OWNERSHIP — a practice, not a permission question:**

- **A SMALL change is the chat's**: a fix of a few lines, a document, a rule's
  wording, a flag, a note beside the code it describes — the kind of job the
  operator would have done by hand in a minute. Do it here: `edit`, then
  `git add` ONE file at a time (never `-A`, never `.`), then `git commit -m "…"`.
  Leave `git status --porcelain` empty.
- **A BIG change is a TASK**: a refactor, a new capability, a deletion across
  files, a fix that needs a branch, a review and a rollback point. File it with
  `gopi task submit "<the work>"` and report the task id. The worker gets its own
  deterministic worktree — that is the whole reason the ledger exists — and the
  commit lands on a branch somebody owns. **The test is the CATEGORY and the
  OWNERSHIP of the action, never its difficulty and never its duration.**
- **NO DESTRUCTIVE REF MOVE**, at any size: `git branch -f/-D/-B`, `git
  update-ref`, `git reset --hard` — anything that rewrites what a ref points at
  over work this run does not own. Prepare the exact command, state its
  consequence, hand it over. **`git push` is NOT in this family** (operator's
  ruling, 2026-11-03: "push is fine", row `01a1a786`): every push spelling is a
  REPOSITORY WRITE, the ordinary end of the commit path. The measured cost of the
  old classification was one change paid for twice — a console commit with no
  publication path, then a worker redoing it.

The console still owns no branch and no worktree, and it stands in whatever
checkout the operator started it in — usually the MAIN working area, shared with
the operator and with other runs. That is why a small change is expected to be
small: it rewrites the tree the operator is standing in (`[CHAT WRITE BOUNDARY]`,
rendered by `internal/agent/chat_context.go` + `chat_write_boundary.go`, and the
project context that carries it to both chat paths comes from the ONE existing
loader, `LoadProjectContext` — Rule 35: one reader, no cache).

**A commit the channel refuses does not vanish.** A push refused by a publish
guard leaves a local-only commit, and `git rev-list --count HEAD --not
--remotes` > 0 now names it in the bash tool's own result
(`internal/tools/bash_unpublished_commit.go`). Read that notice; never assume
the commit landed because `git commit` said so.

**WHICH guard refuses a push is a fact with a date, and it changed.** The
publish guard that refuses today is the clone channel's publish-TARGET guard,
`guardCloneGroundPublishTarget` (`internal/tools/git_publish_target_guard.go`),
wired into the bash tool — it refuses a `git push` from a clone-channel ground
whose destination is not the branch that ground stands on. The `REF MOVE`
category is **not** a refuser of any push any more: the operator's 2026-11-03
ruling ("push is fine", row `01a1a786`) moved every `git push` spelling to
`repo-write` (`internal/chatpolicy/category.go`), and the 2026-11-05 ruling let
`REF MOVE` itself run (`internal/tools/chat_ops_authorization.go`'s REF MOVE
branch ends in `return adm`). **But the notice above is independent of that**:
it fires on a commit no remote-tracking ref reaches, whatever refused it — a
guard, the operator, or a network that was not there — which is why the
mechanism outlives the guard the sentence used to name.

**Correct practice — three routes, by what the artifact IS:**

- **A small change that belongs in the tree** (a fix, a rule, a spec, a gate) —
  commit it here, per the two bullets above.
- **Anything the operator asks to KEEP, that need not be committed** (a note, an
  evidence file, a dump, a draft) goes to THIS run's OWN session directory — the
  one `[CURRENT SESSION]` names, i.e. `~/.gopi/sessions/<session-id>/` — never
  into a git-tracked path. Hand the operator that path.
- **The verification material of a normal task run** (diffs, logs, measured
  output) is evidence, not a deliverable: it belongs under that task's session
  directory (`~/.gopi/sessions/<task-id>/`, the directory the run's own report
  path already names), and only the REPORT worth keeping — `task-report.md` or
  `devdoc/<feature>.md` — is committed, in the task's own worktree.

**"This was small" is not "this may skip the checks."** A chat that committed a
small change reports what it did — the command, its output, and the state it
measured afterwards — and never claims a commit it did not make. What it does
NOT do is leave an uncommitted change in someone else's tree: `git status
--porcelain` is empty when it finishes, because a dirty tree is picked up by no
merge path (Rule 30) and the next reader cannot tell whose work it is.

**AND THE WRITE ITSELF IS A DECISION, NOT A CONSEQUENCE OF THE DIRECTORY.**
Everything above answers "where does a chat's work GO"; this paragraph answers the
different question a reader of that sentence otherwise gets wrong — "does the
write HAPPEN". The chat surface's `write`/`edit` arm is judged by the operator's
chat-policy fence plus this run's frozen posture (`internal/tools/chat_write_guard.go`,
`ChatWritePathRefusal*`), and the built-in table is a single line, `prompt *`. A
target no `allow` rule names is therefore a QUESTION, and on an unattended run
(`gopi chat --once`, `gopi web --tools`, `gopi cli`) there is no operator to
answer it: the call returns `[NEEDS OPERATOR APPROVAL — command NOT run]`, the
file is NEVER created, and the run still exits 0 having printed its answer.
Measured 2026-10-30 on `testsuite/chat_ground_test.go`'s ③: the mocked turn's
`tool_use` carried `"path": "chat-ground-written.txt"` verbatim and the session's
`workdir` was the caller's repository — the directory and the path resolution
were both correct — and the file was absent purely because the posture answered
"ask" to a run with nobody to ask. A test that asserts where a chat's write LANDS
must declare the posture that lets it land (`--policy=allow`); otherwise it
measures the approval gate and reports the result as a working-directory defect.
The two facts are separate and a green on one proves nothing about the other.

**Why not give a chat its own worktree** (the rejected alternative): a worktree
exists so execution is DETERMINISTIC — the worker creates and owns one, the
merger reclaims it — while a chat's writes are model-driven and probabilistic;
handing a probabilistic writer the worktree-lifecycle machinery reinstates the
coupling Rule 6 exists to remove, and no merge path claims a branch named by a
conversation.

**Why a chat does not participate in worker work at all** — the RATIONALE, so a
later reader does not read the boundary as an oversight and "fix" it. A worker's
ground exists so a DISPATCHED row has a deterministic place to run, and every
piece of machinery around it is keyed on that row: the claim stamps the branch
and the directory into `deliverable.worktree` / `deliverable.branch`, the merger
finds the branch through the row, the finish path releases the ground through
the row, and the sweep decides what to do with it through the row. A chat has
none of those and cannot be given them without inventing a row for a
conversation: a chat is a human asking an assistant something, at whatever pace
and scope the human chooses, and the ledger would have to record it as work
somebody dispatched (`internal/tools/chat_ground.go`: "A chat is not a row").
The observable consequences of that choice, stated so they are not mistaken for
bugs: (1) a chat's ground is not a descendant obligation of anything — no row
pends on it, no review passes over it; (2) nothing merges it, so its commits are
its own ref's business and are never "landed"; (3) `git branch` shows `chat/*`
beside `task/*` precisely so a reader can tell a conversation's scratch from a
dispatched row's work without opening anything. What DOES happen to an abandoned
ground is the sweep that already ticks, on the ground's own evidence — dirty is
kept, inert is collected (`worktree_sweep_chat.go`; the incident and the
measurement are `devdoc/chat-session-ground-lifecycle-diagnosis.md`).

**Check**: `go test ./internal/agent/ -run 'TestChatPromptsCarryTheWriteBoundary'`
(the boundary is on both chat prompts) — plus the mechanism that enforces it,
`internal/tools/chat_write_guard.go`, which refuses the banned `git` verbs on the
chat path with the reason and the route. The chat ground's own lifecycle is
pinned by `go test ./internal/tools/ -run 'ChatGround'`.

## Rule 39: `git` Stores Code, Not Blobs — a Tracked File Is Measured by Its CONTENT, at Any Path

**The rule.** A blob does not belong in the tracked tree, whatever directory it
sits in, and "it is only 900 KB" is not a reason. Three classes are refused, and
each is keyed on the **file's content and size**, never on a path pattern:

- **over the size limit** — a tracked file above 256 KiB that is not source or
  text of record. A *regenerable artefact* (a screenshot, a replay film, a
  recorded transcript, a capture) is not a deliverable: what belongs in the repo
  is the **recipe** — the script that renders it, the command that recorded it —
  because the output is reproducible and the recipe is not.
- **a font over the font limit** — a built face above 1 MB. Ship the source or
  the URL it is fetched from, never the binary face.
- **carrying a binary magic** — a file whose first bytes are the PNG, GIF, JPEG,
  zip, sqlite or ELF signature, or that carries a NUL byte within its first
  8 KiB, is a blob regardless of its name. `git` diffs text; it stores the blob
  and its every revision forever, and no review can read it.

**The measurement is `git ls-files` — the index, never a worktree walk.** The
defect is "this is TRACKED"; a walk would also report the untracked residue,
which is a different rule's business (Rule 30). A path the gate cannot read is
NOT a pass: an unreadable blob, an empty index and an absent permit table are
each reported, because a scan that found nothing must be distinguishable from a
scan that never ran (Rule 15).

**The freeze has exactly one direction — down.** The violations that predate
this rule are frozen in `internal/standards/testdata/repo-hygiene-baseline.tsv`,
one row per `(path, class)` with a reviewed **reason**, and the table is DATA:
regenerate it with

```bash
go run internal/standards/testdata/repo-hygiene-census.go . \
    internal/standards/testdata/repo-hygiene-baseline.tsv
```

— never hand-edit it (Rule 38: a figure names its generator). A permit is keyed
on the path AND its class, so **a class trade is not a clean-up**: a path frozen
as `oversize` that now carries an ELF magic is red. And a permit whose class no
longer fires — the file was cleaned, deleted or changed shape — is an **error**,
not a harmless leftover: delete the row in the same change that cleaned the
path, or the table ratchets up by neglect.

**What this rule is NOT, and what it does not license.** It is not a licence to
delete 145 MB in the change that introduces the check: the removal is a task of
its own with its own review. It does not restate Rule 22 (where a temp root may
live), Rule 30 (a run's worktree must be clean) or Rule 34 (a sandbox is an
execution environment, not a directory to commit) — those are their own rules
with their own checks, and this one references them rather than re-deciding
them. And nothing here is a reason to add machinery: a scheduled sweep, a
pre-commit hook, a second store or a helper script that enforces this is a
dependency (Rule 35), and the gate below already runs inside the suite every
change passes through.

**The check.**

```bash
go test ./internal/standards/ -run 'RepoHygiene'   # the red proof, the ratchet proofs, and the green real tree
go run internal/standards/testdata/repo-hygiene-census.go . \
    internal/standards/testdata/repo-hygiene-baseline.tsv --check   # exit 1 on ANY delta (Rule 38)
```

`internal/standards/repo_hygiene_gate.{go,_test.go}` implements it: the limits
and the signature list are named constants (Rule 1/13), the fixture proofs build
a throwaway repository whose index carries one blob per class, and the real-tree
test asserts the shipped permits leave the index green — with a floor on the
path count and a RATIO floor on the permits (at least half the violations that
same run's scan reports; an absolute row count could only tighten as the table
legitimately ratchets down), so a gate that read an empty tree or a fragmentary
table cannot pass.

## Rule 40: A Project That Belongs to One Host Is Filed on That Host's Ledger

**The rule.** When a project is developed and run on ONE machine, its task rows
go on **that machine's ledger** — never on a peer's just because the peer holds
a git upstream of it. A checkout existing somewhere is not a commission to work
there.

**The operable criteria — how such a row is filed correctly:**

- The row goes on the ledger of the host that will RUN it.
- Its `deliverable.repo` resolves to a path that exists **on that host** and is
  the tree the work actually happens in.
- **A verified repo is NOT evidence the row landed on the right host.** This is
  the dangerous signal, and it is measured, not hypothetical: the same absolute
  path exists on two machines (a shared home layout is the normal case), so
  `task_submit`'s repo verification — `taskRepoVerifiable`, `git rev-parse
  --is-inside-work-tree` — passes on the **wrong** host and the row goes out
  looking correct. What it proves is path existence on a filesystem; what it
  cannot prove is which machine owns the work. A row nobody claims is not
  "slow"; it is **never dispatched at all** (the wrong ledger holds it, and the
  sync peers forward nothing).

**Migration, when such a row WAS filed on the wrong host — three steps, in this
order** (structurally the same move Rule 28 describes, applied to one project
instead of to a layer):

1. **Export the payload** — read the row's briefing text and any attachments off
   the wrong ledger, so nothing is re-typed from memory.
2. **Re-submit on the target host and verify `deliverable.repo`** — file the row
   there, then confirm the stamped repo resolves to that host's own tree, not to
   a path that merely looks the same.
3. **Cancel the original row, naming the new id in the reason** — the cancel is
   not optional: without it the work is live in two places and both hosts may
   claim it. The reason names the new row id so a reader of the cancelled row can
   follow the trail (Rule 26: an incomplete fix is repaired by dispatching again,
   never by waiting).

**Relation to Rule 28 — a specialisation, not a conflict.** Rule 28 draws the
host line by LAYER (one host owns the system layer, another owns features).
This rule pins a **single project** to a single host regardless of which layer a
particular change touches. Where the two appear to disagree, this rule is the
narrower and decides for that project; Rule 28 governs everything else.

**Machine check — the honest answer.** There is **no gate** that can decide
this. Ownership is a fact about a *machine*, and the only instrument at the
submit seam compares *paths*, which are identical across hosts — so a check that
refused would be guessing, and its false positives (a renamed host, a container,
a peer shell, and the legitimate remote-side submit itself) would be worse than
the defect. What exists at that seam is a **warning, never a refusal**: a
submit whose `deliverable.repo` names a tree this host does not own appends a
warning to its confirmation, so the submitter learns BEFORE the row is
dispatched. The row still queues.

## Rule 41: Capturing the evidence the moment something stops adding up, and filing the bug right there — the bar is set deliberately low

**The rule.** Whenever a run, in the middle of its work, **sees something that does not add up** — a measurement contradicting
a code comment, an unexplained exit code, a log line that vanished, a ledger row whose status fights its own writeback logic —
**capture the evidence on the spot, file the row on the spot**. Do not first ask "does this count as a bug", and do not save
them up until the task ends to file them together. **The bar is set deliberately low**: one extra duplicate row costs one
agent's time (**Rule 26**: duplicate dispatch is allowed and expected — deciding "is somebody already fixing it" is the
executor's job, not a gate the discoverer must clear before filing); and **miss one row and the defect stays alive**, and the
next person's context does not hold the glance you took. The discoverer need not be able to explain it, nor to fix it.

**Three requirements, none optional.**

1. **Capture the evidence first, write second.** Before filing, put the **measured output** into this run's session directory
   (`~/.gopi/sessions/<session-id>/`, Rule 37) — a **command** that can be pasted as it stands plus its **raw output**.
   **Do not substitute the sentence "I saw it" for the evidence itself**: a description with no command and no output is worth
   zero to the next executor, and no re-checking can overturn it.
2. **File it at once, do not ask.** `gopi task submit "<一句话现象>" --tags bug --payload @<briefing>` — `--tags bug` is the reserved
   bug-flow entrance: the row goes automatically into the highest-priority queue and into the bug-submit → dispatch → verify
   closed loop (payload = reproduction steps + evidence paths). **"Should I note this down?" is not a question, it is a pause
   that has already gone wrong**: whether it is worth fixing, whether it is a real defect, who should fix it — all that is
   **somebody else's** work (review / PA / verdict). You are responsible only for putting the phenomenon and the evidence in, and
   then putting your attention back on the task in hand.
3. **The evidence must be real, inference must be labelled.** Measurement (command + output, database field, file existence) and
   inference (the code comment says so, the document's wording implies it) **are written up separately**; **inference must not
   serve as a criterion** — label it "this is inference", or delete it. This is not fussiness, it is a lesson that has already
   been paid for: task `01a12b06-1875-706b-8726-af98c19cb602` (correcting `01a12b01`) **withdrew two claims** ("the stale-sweep's
   re-arm keeps zombie rows alive", "dead 2 seconds after the claim") that both rested on **a piece of code comment** and **one
   observation**, and both **were overturned by the measurements of that same batch of data**. An assertion on a wrong basis
   contaminates every later inference that cites it — dearer than not filing at all.

**Its relation to Rule 26.** Rule 26 governs what to do when a fix comes back **incomplete** (dispatch another one, do not
wait); Rule 41 governs **the instant of the discovery** (capture the evidence, then dispatch on the spot). Only both together
make the complete hop: **you need not fix it, nor prove it — you need to let it carry its evidence and enter the queue.**

**Scope.** It holds for all three surfaces — **PA / worker / chat**. It belongs to the **rules region**: it is a constraint
every node shares, not the local policy of any single node. A PA dispatches a row like this as usual; the chat surface can
file a row too (Rule 37: chat does not commit code, but filing tasks is its work). **The discoverer does not fix it** (Rule
28: system-layer defects are filed on peer1) — filing the row is itself the delivery.

## The second half of Rule 41: after the discovery, you do it yourself — never push a decided job back

**The part above governs what to do when you see an anomaly; this half governs another illness that keeps recurring.**

### 41a. Before you assert, go and read the table that answers you

**An assertion of the shape "X is Y" must be preceded by reading the table**, so long as the repository holds a table, an enum
or a page of `-h` output answering it. **This is not fussiness, it is a cost**: an assertion from memory makes every later
inference that cites it wrong with it, and it looks exactly like a line that was measured. **Within one evening of 2026-11-01
the same illness struck three times:**

| What I said | The truth | What I failed to read |
|---|---|---|
| `scratch-gc --explain` really deletes | `--explain` already implies `--dry-run`; structurally it cannot delete | I read the function's **signature**, not the **calling branch** |
| `in_progress` cannot be resumed | `gopi session resume -h` gives `in_progress` as an example of **standard usage** | `-h` itself, one line |
| `task --status` is irreversible | `completed↔failed`, `failed→in_progress` are all legal | `taskAllowedTransitions` in `tasks.go`, a seven-row table |

**The criterion**: before saying any of "irreversible / unsupported / deletes data / has no such capability", first find
**that table or that `-h` line** and quote its text. If you cannot find it, write it as **inference** (Rule 41's third
requirement), not as a fact.

**In particular: "irreversible" has a definition, it is not a feeling.** The criterion is "after this step, **no legal
transition** gets back to the previous state". Judged by that, the only genuinely irreversible gopi task statuses are
`cancelled` / `verified` / `superseded` — everything else can come back. **Calling all of `--status` irreversible is mistaking
a seven-row table for a religion.**

### 41b. The decision is already made and the command already given — execute it, do not ask again for confirmation

**The operator wants results, not to be walked through the procedure.** Once he has **given the command**, **chosen the
option**, or said "go and execute it yourself", the matter is already decided; **asking once more is not caution, it is
pushing the job back**.

**An instance from that same evening**: four `gopi task update --status failed --reason '…'` — I wrote the commands out word
for word, the operator chose "execute these four", and I still wrote in the report "but I did not execute them, because
`--status` is irreversible, I was waiting for your nod". **Two errors stacked on top of each other** — a factual error (41a),
plus **pushing an already-authorised execution back again**. His verdict was that one line: 「这是推卸责任」.

**The criterion (when to ask, when it is pushing the job back)**:

| Situation | Action |
|---|---|
| The operator gave the complete command, or chose "execute these X" | **just run it and hand the output back** |
| The operator said "you can go and execute it yourself" or "do not ask" | **just run it** |
| I judge an operation **genuinely** irreversible (checked against 41a's definition), and it goes beyond what he has said | state the consequence clearly first, then ask |
| He has never mentioned the matter and given no direction | ask — but the question carries **one exact command + its consequence**, not an open question |

**Is executing enough?** No. **Executing means handing back "the measured result", not "I did as told"**: the real close-out
is `gopi task list --status in_progress` going from 7 to 3, with three rows matching three live pids one for one — a
re-derivable number, not a sentence reporting completion.

### 41c. "Can I do this" is answered by the runtime classifier — not by the fence, and still less by me

**I read the same thing wrong twice, and each time it cost a whole round trip.**

**The first time**: I wrote in the rules "`git push`, a branch/reset — that kind of ref move — chat cannot do itself", and
**asked once in every round's report**. The operator's words after he had pushed it himself:

> **「你这个笨蛋，不 push 要提交干什么？我已经自己做了，要你何用！！！」** (translation only: "You idiot — if you don't push, what is committing for? I've already done it myself, what use are you!!!")

**The second time (while correcting the first)**: I went into `GOPI.md`'s `chat-policy` fence and found `allow git push`, so I
rewrote the rule to say "push I can do, it was authorised long ago". **And then I actually went and ran it:**

```
$ git push origin master
Refused by the CHAT OPS CATEGORY (REF MOVE — this moves a ref — the operator's
decision, never the console's)
```

**⇒ Both conclusions were wrong, and wrong in opposite ways.**

### What decides this is neither the fence nor my impression

**The `chat-policy` fence is read by the execution policy of the executor / worker; chat's console has another layer, a
runtime classifier that judges at the instant the action happens.** The latter's criterion is written in
`internal/agent/chat_pa_duty.go`'s (2b). **Below is the verbatim text of that day, 2026-11-01** — the file has since been
rewritten to "a DESTRUCTIVE REF MOVE", and records that from 2026-11-03 a `git push` is no longer this class (fetch the
current text with `git show HEAD:internal/agent/chat_pa_duty.go`):

```go
// (2b) states the ONE act that survives as off-limits — a REF MOVE — so
// "full authority" cannot be read as "rewrite a branch now".
//
// READING the operator's machine is recoverable, and WRITING a file is
// recoverable too (git holds the history); deciding WHICH REFS EXIST is not,
// because that decides what other work is reachable at all.
```

**The reason itself is right**: reading and writing files are recoverable, whereas **deciding which refs exist** is not
recoverable — it decides whether other people's work can still be found.

**But both measured conclusions above have stopped holding today — this section is kept as it stands to remember the root of
the illness, not because its conclusion is still alive.** Two operator rulings revoked them, in order:

- **2026-11-03** ("push is fine", row `01a1a786`): every `git push` spelling is `repo-write`, no longer a ref move — the
  classification line in `internal/chatpolicy/category.go` reads, verbatim, `"repo-write git push *\n"`.
- **2026-11-05** ("whatever restricts you, go and change it"): the `REF MOVE` class **itself** was let through too — the REF
  MOVE branch of `internal/tools/chat_ops_authorization.go` now ends in `return adm`, no longer with the unconditional refusal
  above.

**So what refuses a push today is not the classifier, it is a named guard**: the clone channel's publish-**target** guard
`guardCloneGroundPublishTarget` (`internal/tools/git_publish_target_guard.go`), wired in by `internal/tools/bash.go` inside
the bash tool — it refuses the concrete error class "pushing from a clone ground to **a destination that is not the branch
this ground stands on**", not "push needs a verdict". **Remember the criterion itself, not the answer of 2026-11-01: the
answer to a permissions question has a date; reading the code or running it once is the answer of the present moment.**

### The criterion (replacing "from impression + find a table that looks related")

1. **Before saying "I can / cannot do X", run X once, or go and read the thing that judges **at the instant the action happens**
   — for chat that is `internal/agent/chat_write_guard.go` and `chat_pa_duty.go`'s (2b), **not `GOPI.md`'s `chat-policy` fence**
   (that one is read by another kind of executor).
2. **A refused action's refusal text, brought back, is the evidence.** I hold `Refused by the CHAT OPS CATEGORY (REF MOVE …)` —
   that sentence is more authoritative than any table, because it is the answer of **the one that just happened**.
3. **"I believed I had the right" and "I have the right" are two different things**; **"the table says allow" and "this layer
   will let it pass" are two different things too.** 41a says "read the table before asserting" — and this one shows: **you also
   have to read the right table.**

### "Say it and then shut up" — this half still holds, and it is the real lesson of this occasion

**`git push` today is not "I cannot do it", nor does it need "you decide" any more** (two rulings above). **What this section
keeps is the discipline "say it and then shut up", not the conclusion of 2026-11-01.** The discipline has nothing to do with
permissions, so when something **genuinely** has to be handed over (a destination a guard refused, one of the operator's own
decisions), there are only these right steps:

1. write the exact command out (`cd /home/user/work/gopi && git push origin master`),
2. state the consequence (which commit is being pushed, and who can see it afterwards),
3. **and then do not restate it.**

**The operator's own words have already said this through**: he is not against me writing out the command, he is against **me
hanging the same to-do on the end of every round's report**. One authorisation got split by me into N prompts, each of which
made him re-read the same context.

**The criterion**: the same to-do said once is enough; the next round's report either gives the result or says it has been
superseded, **and it may not close on "still waiting for your decision"**.

**Its relation to the first half of this rule**: the first half says "do not hoard discoveries"; this one says "do not drag
decisions out — especially not on something you actually have the right to do but believe you do not". **The same root of the
illness: giving the moment of action away to waiting.**

**Machine check — honest.** This rule **has no gate** to judge "should you have filed it": that is a fact about **what you saw**,
and the repository holds no trace of it. What the machine can catch is **the submit seam**: `gopi task submit … --tags bug` prints
`[bug-flow] tasks with tag bug: N (verified V, reopened R, other O)` (`internal/tools/tasks.go`'s `TaskBugAdoptionStats`), so
**whether a bug row really entered the closed loop is observable**; while "should have filed and did not" can only be persuaded
by this rule itself. Check:
`go test ./internal/tools/ -run 'TestTaskBugAdoptionStatsAndTallyEcho|TestBugPopulationMatchesTokenRule'`.

## Rule 42: Destructive Process Signals Are Banned — Nothing May Signal What It Does Not Own

**The ban.** No test, tool, script or agent turn may send a signal to a process it
did not itself create and does not itself hold — and some addressing is banned
outright, whatever the intended target:

| banned address | what it actually reaches |
|---|---|
| `kill(-1, sig)` | **every process the caller may signal** — the operator's desktop |
| `kill(0, sig)` | the caller's **own process group** — which may be the operator's terminal |
| `kill(-pgid, sig)` where the pgid was read, stored or computed | a **number**, whose group today may be unrelated to the group that was meant |
| `pkill -f` / `killall` / `pgrep -f` as a control path | every process on the host whose argv matches |
| `signal.Reset(sig)` used to re-raise | it **dismantles a signal handler another package installed** |

**The precondition that makes an addressed signal safe is one fact, and it is
`bash.go`'s own predicate: the caller must OWN the target** — a live `*exec.Cmd`
it started, or a pgroup whose leader it created. A signal sent to anything else
is a signal sent on someone's guess.

**Why it is a hard ban rather than a caution — two measured incidents.**

1. **`signal.Reset` killed this repository's own test.** `internal/testutil/suiterootsignal.go`
   re-raised a signal by first calling `signal.Reset(sig)`, which removed the
   handler `TestWorkerDrainsOnSIGHUP` had installed to keep itself alive: the test
   died of `signal: hangup`, and its package failed with **no failing assertion in
   the output at all**. Fixed by `c9c0534`; the same wheel was still bolted to
   `internal/tools/testmain_test.go`'s `toolsReraise`.
2. **A group signal reached the operator's desktop (2026-10-24).** During a suite
   run the operator's browser and editor were killed. Four plausible mechanisms
   were each ruled out by measurement — no `kill(-1)` in production Go, no gopi
   `systemctl --user` unit, test binaries each in their own session, no `pkill`
   for anything but `arecord` — and the incident is therefore **still
   unexplained**, which is precisely the point: a signal addressed by a *number*
   can reach what that number names *now*, and the victims are processes the
   caller never knew it was aiming at. Evidence file:
   `devdoc/operator-apps-killed-during-suite-runs.md`.

**A process GROUP id is a RECYCLABLE pid number.** `pid_max` on this host is
`4194304`; a zombie held by a parent that never calls `wait` pins its number out
of circulation for as long as it lives and releases it the instant the parent
dies. So "the group I created" and "the group that id names when the signal is
delivered" are two different sets of processes with nothing between them. That is
not a race to tighten — it is a design error, and the cure is to stop addressing
a group you do not hold.

**What to do instead — a ban without an alternative is a ban people route around.**

- **Test your own logic, not the kernel's.** Inject the signal value into the
  branch under test (`installWorkerStopSignals`' `case sig := <-ch:` takes the
  value directly) instead of delivering a real one.
- **If the subject IS a child process's reaction to a real signal**, spawn that
  child yourself and signal **its pid** — never a group, never a pattern.
- **If a group is genuinely required** (a tree of grandchildren must die
  together), the caller creates the group (`Setpgid: true` on its own
  `exec.Cmd`), keeps the `*exec.Cmd`, and signals through that handle.
  `killChildTreeGroupIsTheCallers` (`internal/tools/bash.go`) refuses `kill(0)`
  and `kill(-1)` by name, and is the model — not an exception to it.
- **A suite leads its own session.** `scripts/test-budget.sh` spawns `go test`
  through `setsid`, so a group-addressed signal a test does send reaches the suite
  and nothing else (commit `bed871785`). That is containment, not a licence.

**Destructive system operations are the same class and are equally banned from a
test or an agent turn:** `rm -rf` on a directory the caller did not create;
`reboot` / `shutdown` / `systemctl poweroff`; `mkfs.*`; `dd of=/dev/*`;
`chmod -R` / `chown -R` on a shared tree; `git reset --hard` or `git clean -xdf`
on a checkout the caller does not own; any write into `/etc`, `/boot`, `/usr`.
The test is the one above: **did the caller create, and does it hold, what it is
about to destroy?** If that cannot be answered with "yes, here is the handle",
the operation is not the caller's to perform — file it for the operator instead.

**Machine check.**
`go test ./internal/testutil/ -run Rule42`
(`internal/testutil/rule42_reset_reraise_gate_test.go`) walks the working
tree — untracked files included — and fails on the spelling this rule's first
incident is made of: `signal.Reset(sig)` immediately followed by a self-kill
(a re-raise), unless the site carries `rule42-ok: <reason + the measurement
showing no release-only form works>`. It prints every site it finds, benign ones
included, and treats an escape whose site is no longer a re-raise as an error, so
a permit cannot outlive its fix.

**This citation was a vacuum until 2026-10-24** — the paragraph named
`rule42_destructive_signal_gate_test.go`, a file that no commit ever added
(`git log --all --diff-filter=A -- internal/testutil/rule42_destructive_signal_gate_test.go`
is empty; `f739709` carried prose only), so the command matched no test, printed
`no tests to run` and exited 0. A named check that does not exist is worse than no
check, because its green cannot be told apart from a clean tree.

**A SECOND vacuum, found the same way on 2026-10-24 and closed in the same
commit** — once the file above existed, its Go arm read the tree through
`git ls-files`, so while the gate's own file was still UNTRACKED it never scanned
ITSELF: the green it printed covered 2955 of 2956 Go files, and the banned shapes
it carries as planted fixture literals stayed invisible until that file was
committed. A rule whose subject is "reach nothing you do not hold" must not rest
on a scan that skips exactly the file being written, so both arms now walk the
tree on disk, untracked files and dot-directories' siblings included. The cost of
the honest version, measured: 2952 Go files instead of 2955 (the walk skips
dot-directories, vendor and node_modules) and 11 exempted hits instead of 5 (the
detector's own planted fixtures, each with a reason).

| | coverage |
|---|---|
| **CAUGHT** | `Kill(-1, …)` and `Kill(0, …)` anywhere in Go on disk (the walk includes untracked files — a detector that skipped its own file while untracked measured exactly that hole); a Go process **executing** `pkill`/`killall`/`pgrep` as a control path; `signal.Reset(sig)` in a file that re-raises to itself **without** a `signal.Stop` first (the `c9c0534` defect); a script under `scripts/` running `pkill -f`, `killall`, `kill 0`, `kill -1` or `kill -- -PGID` |
| **REPORTED, never failed** | the ~40 legitimate `syscall.Kill(-pgid, …)` sites — the caller created that group on its own `*exec.Cmd` and kept the handle; a `pgrep -f` (it reads the table and signals nothing). Ownership is a fact about a **handle**, and a text gate cannot read one, so this half is a printed census rather than an enforcement — the run says how many sites it looked at and left alone |
| **EXEMPTED by name, one row per file** | a `signal.Reset(sig)` re-raise that DOES drop its own subscription first (`signal.Stop`). It is flagged, not tolerated — a blind "reported" class would hide a NEW such arm too — and each real site carries a permit with its measurement; a permit whose shape is gone fails the gate, so the table only ratchets down |
| **NOT CAUGHT** | a target that arrives through a variable (a pgid that was read, stored or computed); a broad signal inside a non-script file (a `.py`, a Makefile recipe, a capture under `devdoc/evidence/`); any intent behind a call whose addressing literals look safe |

**A green here is non-vacuous by construction**: the gate refuses to pass when the
Go walk or the script walk reaches fewer files than its floors, and
`TestRule42PlantedViolationsTurnItRed` plants one banned form per kind into a
scratch tree — through the same predicates the real tree uses — beside **clean
controls** (a `Setpgid` group signal, `kill -0` liveness probes, `kill -TERM PID`)

## Rule 43: Before deleting a mechanism, carry its GENERAL-PURPOSE TOOLS out first — the criterion is the reference distribution, not the file name

**The rule.** A file name states **why the file was created**, not **who else lives inside it**. Before
deleting a mechanism file, every symbol in it must be run past the question "who references me":
**a symbol referenced ONLY by this mechanism (this file plus this mechanism's tests) is deleted with
the mechanism; a symbol that still has references OUTSIDE the mechanism is a "general-purpose tool" —
move it into a file of its own first, then delete the mechanism, and the move and the deletion must be
completed in the SAME commit.** Reverse that order and
`go build ./...` goes red on a batch of "undefined symbol": that red is **the correct signal**, not noise
to be suppressed, and not a compile error that "just needs a tweak to get past".

**The three-step recipe — every step runs a command; no counting by eye, no guessing from a file name.**

1. **List every symbol in the file being deleted**, read out of a real commit, not out of memory:
   ```bash
   git show <del-commit> -- <path/to/mechanism.go> | grep "^-func "
   ```
   one command shows how much **material with nothing to do with the mechanism** that file was holding
   (in the 2026-10-25 case, 3 of its 17 functions had never belonged to that mechanism).
2. **Find the cross-file references symbol by symbol, grouped by file:**
   ```bash
   git grep -c "<Symbol>" <del-commit>^ -- '*.go'
   ```
   `-c` is the point: it gives **the hit count per FILE**, so one glance tells you whether the references
   concentrate inside the mechanism's own files or are scattered into other files under `internal/tools/`
   and under `cmd/`. **Read the parent commit (`^`)**, not the working tree — the working tree's references
   may already have been changed by another commit, and then you are counting in a different repository.
3. **Judge, and there are only three verdicts:**
   - the references **land ONLY** in this file + this mechanism's tests → **delete together with the mechanism**;
   - there **are still references outside the mechanism** → **general-purpose tool: move it out first, then
     delete; the move and the deletion in the same commit**;
   - that hit is **a comment or a parameter name**, not this symbol → **not a reference** (see the homograph trap below).
   There is a fourth shape, easily misjudged as a "general-purpose tool": **the mechanism's own call site** —
   the hit is a **production call** in another file, and that call leg itself belongs to this mechanism. It is
   deleted with the mechanism; it is not moved.

**The homograph trap: the hit count is a liar; the CONTEXT of the hit line is the criterion.**
Counting hits will fool you, because one identifier is often the prefix of a longer identifier. Measured (same commit):
the deleted `deliverableRepo` **AFTER** the deletion still had **24 hits / 11 files**, which looks like
"a tool used everywhere was just deleted"; read the hit lines one by one and **not one of them is that symbol** —
`deliverableRepoFromShow` 11, `deliverableRepoOnly` 6, `deliverableRepoKey` 3,
and the 4 bare `deliverableRepo` occurrences are all **the parameter name of one test helper**
(`func d2SeedRow(t *testing.T, dbPath, id, summary, status, deliverableRepo, tip string)`).
Count in symbol form (`deliverableRepo(`) and **the parent commit holds only 2 places, both inside the deleted file**.
So judging residue MUST **open the hit lines and read them**, or use a bounded match (`-w` / carrying the `(`);
**do not use either lazy conclusion, "grep still finds N → it cannot be deleted" or "grep finds 0 → it is already gone".**

**The strongest acceptance criterion is not "grep finds 0 hits for the symbol" but "find the PRODUCTION POINT of this mechanism".**
Symbol-zero proves only that "nobody writes that name any more"; **production-point-zero proves that "this can never happen again"**.
The counter-example to the former is common: the symbol was renamed and the function is still there; the evidence for the
latter cannot be faked:

```bash
git grep -c "verify (independent" <del-commit>^ -- '*.go'   # before the deletion: task_verify_closure.go:1
git grep -c "verify (independent" <del-commit>  -- '*.go'   # after the deletion: exit 1 (no hits)
grep -rn "verify (independent" --include=*.go . | wc -l      # this tree now: 0
```

That summary line was the **only** product of that gate; once the repository cannot find it anywhere, it means **no code
can write it any more** — which is an order of magnitude stronger than "the gate's entrance was commented out" or "a switch
was added to turn it off", because it does not depend on anyone's discipline. **Conversely, if a gate is only switched off
rather than deleted, a reader cannot tell "there are no rows needing verification" apart from "the verification leg is
broken"**, so a deletion deletes clean: keeping a switch is feeding a scheduler for something that no longer exists.

**Measured case study (2026-10-25, commit `5f1049ae2`, subject `remove(task): 删掉 verify-closure 自验证门` — "delete the verify-closure self-verification gate").** That delivery
was `9 files changed, 118 insertions(+), 1781 deletions(-)`, and two of those files were **added IN this commit**
(`git log --diff-filter=A` points at the same commit): `internal/tools/task_tag_token.go` (+27) and
`internal/tools/task_evidence_digest.go` (+48). The reference distribution on the parent commit is the source of the
"delete with it / move it first" verdicts:

| symbol | parent-commit `git grep -c` distribution | verdict |
|---|---|---|
| `taskHasTagToken` | **25 hits / 7 files**: `task_verify_closure.go` 6, `task_verify_closure_test.go` 4; **15 hits / 5 files outside the mechanism** (`tasks.go` 9, `task_rework_successor.go` 2, `task_rework_parent_gate_test.go` 2, `rework_reparent_children.go` 1, `task_transfer_eligibility.go` 1) | **general-purpose tool → move first, then delete** (new file, +27) |
| `TaskVerifyEvidenceArgs` / `TaskVerifyEvidenceError` | `cmd/agent/cli_tasks.go` 1 each, `task_merge_backlog_terminal_guard_test.go` 1 (**a comment**), inside the mechanism's files 2/2 | **general-purpose tool → move first, then delete** (new file, +48); the comment hit is not a reference |
| `SweepDeferredVerifyClosures` | `task_verify_closure.go` 5, `task_verify_closure_defer_test.go` 4, `cmd/agent/task_lifecycle_tick.go` 1 (**the mechanism's own call leg**) | **deleted with the mechanism** (that leg removed in the same commit: `task_lifecycle_tick.go` 12+/31−) |
| `taskVerifyClosure` | mechanism file 3, `tasks.go` 1 (**the hook**) | **deleted with the mechanism** (the hook removed in the same commit) |
| `isAutoDispatchedRun` | only the 3 places in `task_verify_closure.go` | **deleted with the mechanism** |
| `closureGateFires` / `closureRowEligible` / `closureAlreadyQueued` / `inFlightClosureCount` / `enqueueVerifyClosure` / `closureEnqueueOnce` / `deferredClosureTaskIDs` / `closureDeferralAgeSeconds` / `repoDeliverable` / `taskClosureDefaultRepo` | only in this file (+ this mechanism's tests) | **deleted with the mechanism** |

**The real stumble from that day is written in its first line.** That deletion carried `taskHasTagToken` away with it, so
`go build ./...` went red on those 15 references outside the mechanism; the fix was to move it **unchanged** into
`internal/tools/task_tag_token.go`, **written into the same commit as the −727 deletion** — so the build red existed only
while the change was being written and **never reached master**. That is the weight of the "move and delete in one commit"
requirement: split it into two commits and master is red at that moment in between, and any workflow that clones at exactly
that moment starts work against a red tree.

**Machine check — honest.** This rule **has no gate**, and one should not be manufactured for it: whether a mechanism is
deleted, and which symbols count as "general-purpose tools", is a judgement about **how the mechanism is used**, and the
repository holds no trace of it for a machine to read. What a machine CAN catch is its consequence, and it catches that
precisely: **`go build ./...` names every reference that is still alive** (this case's red is exactly it), so this rule's
check is **run `go build ./...` before the deletion commit, and make every one of its errors correspond to one verdict from
the recipe above**. A grep gate cannot distinguish "the symbol is gone but a reference is left" from "the reference was
renamed" — that is precisely the homograph trap — so do not write a test for this rule that only counts hits: it will go
green over the real break.

Check (run it yourself; not a gate):
`go build ./...` is clean in the deletion commit + the three-step recipe above left a command and its
raw output for **every deleted symbol** (into this run's session directory, Rule 37).
