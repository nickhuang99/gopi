<!-- doc-class: historical -->
# The chat prompt names THIS run's own transcript file

**Task:** `01a0e1da-0999-7b98-af5f-e342d02a5d8d` (epic: *re-implement on a fresh
branch the residual value of the deleted branch
`task/01a0e1d-e3a9-7d12-ba48-a90acdc4b827`, unmergeable because master's
`6dc8173b` landed the same ruling over the same three files first*).
**Branch:** `feat/chat-prompt-self-reference` (worktree
`/home/user/workspace/gopi-01a0e1da09997b98af5fe342d02a5d8d`), off
`a35916be`.
**Sibling of:** `devdoc/chat-context-self-knowledge.md` (task `01a0e167`, the
change this one extends — that doc covers the STATIC half, this one the per-RUN
half).

---

## What master already had, and the one thing it lacked

The recon is the finding, so it is recorded as a measurement rather than a
summary. `internal/agent/chat_context.go` on `a35916be` already shipped the
host · node · repo block, the arming/reading/compare loop, the operator's
no-pixel-snapshot rule and the two honest limbs (tools on / no tools). What none
of it did was name **this process's own transcript file**:

| what the prompt named | what it never named |
|---|---|
| `gopi log` / `gopi raw` / `gopi stats` — the verbs that READ a record | which FILE this run is writing, so "quote me the file that settles what you sent" had no answer but a guess |
| `gopi ui_state --compare "<line>" --session <id>` — the check that a line is on the grid AND in the transcript | the id of the session that check resolves against |

A chat asked for that file could only guess, and a chat that guessed **denied a
file that existed**. That is the surviving idea this branch carries forward.

## The second surviving idea was dropped, and here is why

The branch also carried a scoped *"do not mention internals unless asked"*
sentence. Master **deliberately deleted** that sentence, and the deletion is
frozen with its reasoning in `output_language_prompt_test.go`:

> the pre-task third sentence — *"Do not mention tools, task lists, or system
> internals unless asked"* — is the sentence that MADE the defect: it forbade the
> surface from saying what this same task now TELLS it.

The replacement is `"Honesty over fluency: never claim to have seen, run or
checked something you have not."` The prohibition is therefore **superseded by a
landed ruling**, and re-adding it would restore the exact instruction that made
the original defect: a surface forbidden to mention the very surfaces the block
now teaches. Its surviving half — *when asked, answer truthfully about your own
transcript; never deny a file that exists* — is what this branch implements, in
the runtime block's own words rather than as a prohibition. **Nothing was
recovered from the archived branch**: the branch
(`task/01a0e16d-e3a9-7d12-ba48-a90acdc4b827`) is present in **no** local ref,
remote ref or tag in this repo, so no claim in this document rests on having read
its diff. The deliverable was re-derived from master, which was the constraint
anyway.

**And the ruling is now pinned by a test, so a future "recovery" fails red.** The
epic's item 4 asked for the scoped sentence *"on the plain path only, if master
still lacks master's own version of it"* — re-verified against `a35916be`, master
has its own version, in the same slot (the persona body's third sentence), so the
condition resolves to *master has one; do not add a second*. What was missing is
that nothing failed if someone re-added the prohibition anyway, so
`TestChatPromptCarriesMastersHonestySentenceNotTheDeletedProhibition`
(`internal/agent/chat_session_hint_test.go`) asserts on the **assembled plain-path
prompt** both halves: master's honesty sentence is present, and every fragment of
the deleted prohibition (`"…unless asked"` included) is absent — while also
asserting that the two blocks that prohibition would forbid (`[CHAT SURFACE …]`,
`[CURRENT SESSION …]`) really are on that path, so the test's premise cannot
silently rot. A future agent handed "recover the branch's sentence" now has a red
gate to read instead of a prose note to trust.

## The design decision worth recording

**The runtime half rides the ASSEMBLY, not the section registry.** The static
half is a `PromptSection` whose text is a process property; the runtime half is
**per-Agent state** (`a.RawLog.Path()`, `a.SessionLog.Path()`), which a
`func() string` `Render` cannot see — it takes no argument. So the split follows
the data:

- static → `chatContextSection` (`Render: chatContextRules`), unchanged, still
  invisible to `TestSystemPromptDensity` because it is **not in
  `promptSections`** (the task registry) — the block is appended by the two chat
  composition points, exactly as master already did;
- runtime → `chatSessionBlock(a *Agent)`, appended by the SAME two points.

To append it, both composition points had to **receive the run**:
`chatSystemPromptServed()` → `chatSystemPromptServed(a *Agent)` and
`chatOpsSystemPrompt(base string)` → `chatOpsSystemPrompt(a *Agent)` (the base is
now read from `a.SystemPrompt` in one function instead of at the call site). This
is the change most likely to bite a reviewer, so it is stated plainly: **a
signature changed on both chat paths, and the only call sites are the request
builders plus the tests.** The alternative — a package-level "current agent"
variable — was rejected because it would make two concurrently-lived Agents
render each other's paths, which is the very confusion the block exists to end.

**The prompt stays byte-stable turn to turn.** The runtime half re-reads live
state per render, but the state does not move between consecutive renders of one
turn, so the appended bytes are identical — `prompt cache` survives. Asserted,
not assumed: `TestChatPromptSessionBlockByteStableTurnToTurn`.

**Absence is REPORTED, never left to inference** (Rule 15). Each half is rendered
on its own, because gating one on the other made the commonest real state — the
wire log alone — fall through to a sentence about a transcript that does not
exist. The three arms and what each says:

| arm | condition | what the prompt says |
|---|---|---|
| 1 | both logs live | both paths named, plus `gopi raw --file <path>` for the wire log; the session id |
| 2 | session JSONL only | the session path, **and** `GOPI_RAW_LOG=OFF` named as the reason the wire log is absent |
| 3 | neither | *"No transcript of this run is on disk … say so rather than naming a file"* |

Arm 3 is the one that matters most: its opposite — a model naming a file it
cannot know — is the failure the block was written to end.

**Every spelling comes from a constant (Rule 1/13/14).** `gopi raw` and the
`--file` flag are read from `internal/cliverbs`, never typed. That flag needed
care: `cmd/agent`'s raw parser matches BARE words (`rawFlagMember`) and prepends
`--` at the boundary, so the dispatch side holds the word and the prompt renders
the flag. Rather than two literals, `cliverbs.RawFileWord` is the ONE definition
and `FileFlag = "--" + RawFileWord` is **derived** from it; `cmd/agent`'s
`rawFlagFile` is an **alias** of it. The drift guard was extended to resolve
through an alias (a value comparison, so a rename on either side still fails) and
a second test pins the *shape* — `rawFlagFile` must not be a bare literal again —
because "the alias arm is accepted" would otherwise be a licence to re-introduce
the mirror-literal defect Rules 1b/13 forbid. Both halves were red-proved.

**Rule 21 needed the env names to become real constants.** Arm 2 has to name the
switch that caused the absence, which means `internal/agent` now spells
`GOPI_RAW_LOG`/`GOPI_RAW_LOG_DIR`/`OFF`. They were bare literals in `rawlog.go`,
so they became `rawLogEnableEnv` / `rawLogDirEnv` / `rawLogDisabledValue` and
the three readers reference them — the same change Rule 13 asks for, made because
a second surface needed to name the same value. **This shifted the file's line
numbers, so `CONFIG.md`'s two `rawlog.go:NNN` anchors were re-pointed in the
same commit** (`config_doc_gate_test.go` fails on a stale anchor). The anchors
for `raw_log_dir`'s reader point at `newRawLoggerInDir`, the ONE function that
actually reads it (the two `NewRawLogger*` functions just route to it) — which is
also what `CONFIG.md`'s own prose already claimed.

## Verification

- Build: `go build ./...` → exit 0; `go vet ./...` → exit 0 (no output).
- Binary: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0.
- Tests: `go test ./internal/agent/ -run 'ChatPrompt' -count=1` → **ok** (11
  tests: `-run ChatPrompt` selects an 11-test superset of the 6 in
  `chat_session_hint_test.go`) on both chat paths across all three arms;
  `go test ./internal/cliverbs/ -count=1` → ok; `go test ./internal/cliverbs/
  ./cmd/agent/ -count=1` → ok; `go test ./internal/standards/ -count=1` → **ok
  (30.6s)** — the whole standards gate package is GREEN, so this change adds no
  gate violation. The one red in the tree is
  `TestDocAnchorsResolveInNormativeDocs` in `./internal/agent/`, a standing master
  condition measured below. See the report (`$GOPI_TASK_REPORT_PATH`) for the
  exact invocations and their output.
- **The live surface (Rule 31), not just the unit tests.** `gopi chat --once` was
  run inside a sandbox profile (`bin/gopi sandbox run --label chatlive --task-id
  $TID -- bin/gopi chat --once -p …`) — the run being reported, on this branch's
  binary:
  - plain path: the answer quoted
    `/home/user/workspace/gopi-sandbox/sandbox-chatlive-43eafe45/.gopi/sessions/
    02c8d28d-…/02c8d28d-….raw.log` and `…/session.jsonl` — **both halves named,
    arm 1** — and that raw path was then read back out of the **wire log the turn
    itself wrote** (25,964 bytes; `grep -c 'CURRENT SESSION'` → 1), with the block
    appearing there verbatim: `[CURRENT SESSION — 02c8d28d-…] Your own transcript,
    written while you answer: raw HTTP log … so it is what settles "what did I
    actually send"`. The block provably went **on the wire**, not merely into a
    string.
  - **The invocation the block teaches, run by hand against that very file**:
    `bin/gopi raw --file …/02c8d28d-….raw.log` printed the log's header line
    (`# gopi raw HTTP log — created 2026-09-27T08:22:51Z pid=3432147
    session=02c8d28d-…`) followed by `→ REQUEST` — so the command the prompt puts
    in front of the model is executable as spelled.
- **Merge probe**: `git merge --no-commit --no-ff feat/chat-prompt-self-reference`
  in a detached worktree on pristine `a35916be` → rc=0, zero unmerged paths, 14
  files staged.
- **Rule 36/DOC-INDEX**: `gopi doccheck` → "the devdoc corpus is consistent —
  every file under devdoc/ has a well-formed row" (exit 0), the row for this
  document written by the classifier, not by hand.
- **The arms are asserted on the ASSEMBLED prompt** —
  `chatSystemPromptServed(...)` / `chatOpsSystemPrompt(...)`, the very expressions
  the request builders pass to `BuildClaudeRequest` — never on the block function
  and never on a copy of the text. Both paths are driven in every arm, because a
  half-served pair is the shape of the defect.
- **Mutate-and-rerun (red proof)**: deleting the `chatSessionBlock(a)` append from
  `assembleChatSystemPrompt` and from `chatOpsSystemPrompt` turned **6 tests red**
  (`TestChatPromptArmBothLogs`, `…ArmSessionOnly`, `…ArmNeitherLog`,
  `…SessionBlockIsRemovable`, `…NamesThisProcesssOwnPaths`, and
  `…CarriesMastersHonestySentenceNotTheDeletedProhibition` — the last through its
  premise clause, which is exactly what the premise clause is for); restoring the
  two lines returned the package to green with `git status --porcelain` empty. A
  test that cannot be made red is not measuring anything.
- **The line-count discipline (a measurement that changed the design).** The first
  version of the `chatSystemPromptServed` comment was six lines longer than the
  one it replaced, and `internal/agent/loop.go` is the most heavily anchored file
  in the corpus: the shift dragged **15 pre-existing doc anchors** off their
  symbols, growing the gate's diagnostic listing. The comment was rewritten to the
  same length. The verification is a **comparison, not a reading**, and the
  command is the evidence: `TestDocAnchorsResolveInNormativeDocs` was run on both
  trees (branch worktree, and a throwaway `--detach a35916be` worktree), each run's
  `file:line` tokens extracted with
  `grep -oE '[a-z0-9_./-]+\.(go|md):[0-9]+' | sort -u` — **235 lines each, and
  `diff` between the two sets is empty (identical drift sets)**; `loop.go` is
  **7001 lines on master and 7001 here**. Master is red on this gate
  independently of this branch; those entries are the baseline this change is
  measured against, not something it fixes.
- `git status --porcelain` in the worktree → empty (Rule 30).

## Not done / honest limits

- **The archived branch was never read.** Checked with `git for-each-ref`, `git
  tag -l` and `git branch -a`: the exact branch
  `task/01a0e16d-e3a9-7d12-ba48-a90acdc4b827` is in no local ref, remote ref or
  tag. (A *different* child of the same epic, `task/01a0e16de562714484d5265ea340e598`,
  does exist — the prefix is what makes them look alike; it is not the branch
  this task was asked to recover.) The deliverable is a re-derivation from
  master, not a port, and no claim here rests on the archived branch's contents.
- **`TestDocAnchorsResolveInNormativeDocs` is RED on master and stays red here.**
  It is a whole-corpus gate over the normative docs and its red set is a standing
  master condition, not a result of this change (the two runs' `file:line`
  diagnostics are identical, 235 each, `diff` empty). The branch's obligation —
  introduce no NEW drift — is met and measured above. The line counts, not a
  drift-entry count, are what was measured: the earlier "21 vs 36" figures were
  taken from memory of an intermediate run and are **not** reproduced by the
  commands in this document, so they are withdrawn here rather than left standing.
- **The `..` path in a raw log's directory is not resolved** for display; the
  block prints the path the logger resolved, verbatim. Symlinked homes are the
  only place this reads oddly, and rewriting a path the operator must be able to
  paste into a shell was judged worse than the oddity.
- **Arm 2 in production is rarer than arm 1 or 3** (it needs `GOPI_RAW_LOG=OFF`
  while a session logger still attaches). It is covered because it is the arm
  whose absence-sentence is most likely to be wrong, not because it is common.
