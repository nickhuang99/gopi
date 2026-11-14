<!-- doc-class: historical -->
# Task Report: selfKnowledge prompt section — the `screen_capture` gap

Task `01a0d594677a702b81fb147e35b8d607`, worktree
`/home/user/workspace/gopi-01a0d594677a702b81fb147e35b8d607` on branch
`task/01a0d594677a702b81fb147e35b8d607`.

**This report is the FOLLOW-UP, not a duplicate.** The epic's primary deliverable
landed on master before this run closed: `f8604bf6 merge: feat/self-knowledge-
prompt-section — self-knowledge prompt section (task 01a0d590)`. That branch
created `internal/agent/loop.go`'s `selfKnowledge` section (Order 5),
`internal/cliverbs` (leaf constants), `internal/agent/self_knowledge_prompt_test.go`
and `devdoc/self-knowledge-prompt-section.md` (which is the report for IT, kept
verbatim here). This run's merge of master hit add/add conflicts on exactly those
five paths; the resolution took master's landed files verbatim (no duplication),
and **this branch's remaining delta is the one gap master's landed section has**:
it never names `screen_capture`, which the epic's acceptance requires. That gap,
its fix, and its evidence are what this document reports.

## Completed

1. **`cliverbs.ScreenCapture = "screen_capture"`** added next to `Capture` in
   `internal/cliverbs/cliverbs.go`, with the comment stating why the model needs
   the word: it is the CLI MIRROR of the `screen_capture` TOOL (`CliVerbScreenCapture`
   in `cmd/agent/cli_verbs.go`, same string, one dispatch), so the word reaches —
   through bash — the very engine the tool call uses.
2. **The section now names it**: `capture/screen_capture + act = see+drive the
   desktop`. The rendered section is
   `[SELF-KNOWLEDGE — you ARE gopi] \`gopi <verb> -h\` prints usage. Senses:
   ui_state = a live \`gopi ui\` screen as cells; log/raw/stats = your own
   sessions, traffic, cost; task = your ledger row; capture/screen_capture + act
   = see+drive the desktop; sandbox = a private profile.`
3. **Both guard suites extended to cover it** — `cliverbs_test.go`'s
   `senseVerbs` map (so the drift guard pins `ScreenCapture` to
   `CliVerbScreenCapture` in `cmd/agent`) and
   `self_knowledge_prompt_test.go`'s verb list (so emptying the section reddens).
4. **Density still held**: **4482 chars** (gate <4500), i.e. *below* master's
   4498 — the 3 chars of `screen_capture` (plus `/`) were paid for by dropping
   `(no pixels)` and the two words `a file's` from the read rule, which no test
   asserts.
5. **No new tool (Rule 3)**; `gopi ui_state -h` still exits 0.

## Not Done (honest)

- **I did not re-implement the section.** The obvious reading of the epic —
  write `internal/agent/loop.go`'s `selfKnowledge` section from scratch — became
  wrong the moment master landed `f8604bf6` with the same files. Re-adding them
  would have been an add/add duplicate of landed work, so this run contributes
  the delta over master instead. A reviewer wanting the section design rationale
  should read `devdoc/self-knowledge-prompt-section.md` (master's, kept).
- **Three reds in the tree are PRE-EXISTING and NOT this change** — each proven
  by substituting master's own file back in, not by inference:
  - `internal/agent` → `TestAgent_StuckLoop_TruncatedRunIsNotCompleted`
    (`truncated_run_test.go:124`) fails with MASTER's `internal/agent/loop.go`.
  - `cmd/agent` → `TestTopLevelUsageDelegatesEnvVarsToHelpEnv`
    (`usage_test.go:137`, "172 lines — over the 160-line budget"):
    `git diff --name-only master HEAD -- cmd/agent internal/config` prints
    **nothing**, so that suite runs byte-identical code to master's.
  - `internal/standards` → `TestDocAnchorsResolveInNormativeDocs` (naming
    `devdoc/agent-five-organs-ontology.md` / `hippocampus.md` anchors) and
    `TestScreenAssertionDoesNotSyncOnProducerCounters` (on
    `internal/uiclient/dump_test.go`, a file not in this diff).
- **The doc's own anchor drift is untouched.** `internal/agent/loop.go` grows by
  a few lines here, which can move a `path:line` anchor in a landed historical
  doc; that repair belongs to a doc-side change, not to this one (the landed
  sibling branch made the same call — `devdoc/self-knowledge-prompt-section.md`
  §Not Done names the same pre-existing doc-anchor red).
- **Adoption is taught, not habituated.** See the run below: the model used the
  verbs, but it also had to discover the sandbox `GOPI_HOME` pitfall itself.
  Prompt teaching is what the epic asked for; a stronger loop is separate work.

## Verification

- **Build**: `go build ./...` → **BUILD_OK** (exit 0);
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (used for the run).
- **Tests** (branch-scoped to what the diff touched):
  - `go test ./internal/agent/ ./internal/cliverbs/ -count=1 -run 'SelfKnowledge|Sense|SystemPromptDensity' -v`
    → `TestSystemPromptDensity` PASS · `TestSystemPrompt_SelfKnowledgeSection`
    PASS · `TestSelfKnowledgeSectionIsStatic` PASS ·
    `TestSenseVerbDeclarationUsesConstants` PASS ·
    `TestSenseVerbConstantsAreTheDispatchedWords` PASS ·
    `TestSenseVerbCatalogIsComplete` PASS · `TestSenseVerbsMatchCmdAgentDispatch`
    PASS · `TestNoSenseVerbIsEmptyOrBareLiteral` PASS.
  - `go test ./internal/agent/ -count=1 -run 'Prompt|Self|Sense|Clock|Budget|Density|ToolR'`
    → `ok  github.com/nick/gopi/internal/agent  38.560s`.
  - `go test ./internal/cliverbs/ -count=1` → `ok  0.762s`.
- **Ran** (exact invocations, verbatim):
  - `./bin/gopi ui_state -h` → `ui_state -h exit=0 OK` (prompt-only change).
  - `SB=/home/user/workspace/gopi-sandbox/sandbox-selfkn2; ./bin/gopi sandbox
    create --label selfkn2 --task-id 01a0d594` then
    `printf '%s\n' 'Without screenshots and without a vision model: which gopi CLI
    surfaces can read my own state, and quote one real number about my own recent
    runs? Use your own tools.' | GOPI_HOME="$SB" GOPI_SETTINGS="$SB/settings.json"
    NO_COLOR=1 timeout 420 ./bin/gopi chat --tools`
    → exit 0 (`RUN_EXIT=0`).
- **Result**: the model's answer names the senses BY THE VERBS THE SECTION
  TEACHES and it quoted measured numbers, e.g. `gopi stats --last 1` → "7 rounds,
  71,822 prompt tokens, 96.95 % cache hit"; `gopi task list` → "2430 task rows,
  1834 completed"; and it listed `gopi raw` / `gopi ui_state --json` /
  `gopi config show` / `gopi sandbox -h` among the surfaces. It also reported the
  sandbox pitfall it hit (`GOPI_HOME` makes the default home `$GOPI_HOME/.gopi`,
  so pass `--home`/`--db` to read real history) — i.e. it treated `gopi`'s own
  CLI as the instrument for reading its own state, which is the capability this
  section exists to teach.
- **Commit**: the branch tip.

### The RED proof (observed, then restored)

The section text was emptied **in a compiling way** — the registry entry pointed
at `Text: ""` — so the failure is the GUARD's, not the compiler's:

```
go test ./internal/agent/ ./internal/cliverbs/ -count=1 -run 'SelfKnowledge|Sense' -v
    self_knowledge_prompt_test.go:42: assembled system prompt does not carry the self-knowledge marker "[SELF-KNOWLEDGE"
--- FAIL: TestSystemPrompt_SelfKnowledgeSection (0.00s)
    self_knowledge_prompt_test.go:109: the selfKnowledge section text is not the selfKnowledgeRules constant
--- FAIL: TestSelfKnowledgeSectionIsStatic (0.00s)
    self_knowledge_prompt_test.go:176: promptSections has no selfKnowledge entry pointing at selfKnowledgeRules
--- FAIL: TestSenseVerbDeclarationUsesConstants (0.00s)
    self_knowledge_prompt_test.go:235: sense verb "ui_state" is in the catalogue but absent from the assembled prompt
    (… raw / stats / capture / sandbox alike …)
--- FAIL: TestSenseVerbCatalogIsComplete (0.00s)
```

Restored, then GREEN: `ok github.com/nick/gopi/internal/agent  0.218s` ·
`ok github.com/nick/gopi/internal/cliverbs  0.713s`.

### Density, measured

| | `len(assembleSystemPrompt())` |
|---|---|
| master (landed sibling section, without `screen_capture`) | 4498 |
| **this branch** | **4482** (gate: <4500) |
| the `selfKnowledge` section itself | 277 bytes rendered |

## Evidence (self-filed)

- `go build ./...` → exit 0; `./bin/gopi ui_state -h` → exit 0.
- Density measured with a throwaway in-package test
  (`t.Logf("DENSITY=%d", len(assembleSystemPrompt()))`, deleted before commit)
  → `DENSITY=4482`; the same harness logged `SECTION BYTES=277` for the
  `[SELF-KNOWLEDGE …` → `[TOOL ROUTING` region of the ASSEMBLED prompt, and
  those 277 bytes read:
  `` [SELF-KNOWLEDGE — you ARE gopi] `gopi <verb> -h` prints usage. Senses:
  ui_state = a live `gopi ui` screen as cells; log/raw/stats = your own sessions,
  traffic, cost; task = your ledger row; capture/screen_capture + act = see+drive
  the desktop; sandbox = a private profile. ``
- Verb counts in that rendered region (`grep -cw`): `ui_state 1 · capture 1 ·
  screen_capture 1 · log 1 · raw 1 · task 1 · stats 1`. In the `selfKnowledgeRules`
  DECLARATION those counts are all **0** — it spells `cliverbs.*` constants
  (Rule 13), and `internal/cliverbs/cliverbs.go` is the single definition site.
- The merge proof: `git merge master` reported add/add conflicts on the five paths
  both branches added; resolution was `git checkout --theirs` for those (master's
  landed text) — no duplicated file, no re-litigated design.
- `git status --porcelain` in this worktree after the commit prints nothing.
- Branch/commit: the deliverable is on `task/01a0d594677a702b81fb147e35b8d607`.
