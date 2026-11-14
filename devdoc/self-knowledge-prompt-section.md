<!-- doc-class: historical -->
# Task Report: selfKnowledge prompt section — the senses the agent already had

Item [1/4] of the gopi self-knowledge epic, task `01a0d59065377e808244c2631e28643f`.
Branch `task/01a0d59065377e808244c2631e28643f` (worktree provided by the runner).

## Completed

- **`selfKnowledgeRules`** — one named const in `internal/agent/loop.go`, plus ONE
  registry entry `{Name: selfKnowledgeSectionName, Order: 5, Text: selfKnowledgeRules}`
  in `promptSections` (between the header, Order 0, and `toolRouting`, Order 10).
  It tells the model the two things nothing in the prompt said before: it **IS**
  gopi, running inside gopi's own source repo, and **bash calling gopi's own CLI
  is how it senses this machine** — then names the eight sense verbs.
- **Every verb comes from a named constant** (Rule 1/13/14), never a literal.
  `internal/cliverbs` (new leaf package: constants only, no imports) is the home,
  because `cmd/agent/cli_verbs.go` is `package main` and unimportable from
  `internal/agent` — the same reason `cmd/agent`'s `CliVerbMemory` already aliases
  `tools.CliVerbMemoryGroup`. Verbs named: `ui_state`, `log`, `raw`, `stats`,
  `task`, `capture`, `act`, `sandbox`.
  **Every verb was confirmed to exist BEFORE being named**: `ui_state`, `log`,
  `raw`, `stats`, `task`, `capture`, `sandbox` are `cli_verbs.go` constants;
  `act` is `CliVerbAct` in `cli_act.go`; all eight have a live `isXxxSubcommand`
  predicate and a `dispatcherOfToken` entry.
- **Static const, no per-turn interpolation.** The section is a Go CONSTANT
  EXPRESSION (`cliverbs.X + "…" + cliverbs.Y`), folded at compile time, riding
  `Text` (not `Render`) — so the rendered prompt is byte-identical per turn and
  the caching prefix is untouched. Guarded by `TestSelfKnowledgeSectionIsStatic`.
- **Guard test, red-proved.** `internal/agent/self_knowledge_prompt_test.go`
  (5 tests): section present in the assembled prompt, identity claims present,
  all eight verbs named by constant, capture+act joined as the eye/hand loop, the
  section is static and correctly ordered, and the verbs are spelled through
  constants inside the declaration (Rule 13). `internal/cliverbs/cliverbs_test.go`
  pins each leaf constant to the `cmd/agent` declaration that DISPATCHES it.
- **Density paid for by compression, not by overshoot**: 4488 → **4498** chars
  (gate: <4500). See the measured table under Verification.

## Not Done (honest)

- **The whole-package run `go test ./internal/agent/ -count=1` is RED, and the
  red test is `TestDocAnchorsResolveInNormativeDocs` — PRE-EXISTING master drift,
  not this change.** Evidence it is not mine: the gate's failure output names
  **0** occurrences of `self_knowledge_prompt_test.go` / `self-knowledge-prompt-section.md`
  / `cliverbs` (`grep -c` → `0`); it names ~200 landed-record devdocs
  (`devdoc/assert-the-bug-audit-20260917.md`, `devdoc/assertion-totality-gate-master-red.md`,
  `devdoc/cwd-and-lifetime-recovery.md`, …) whose `file:line` anchors a master
  refactor moved past EOF, which the gate itself classifies as "BASELINE DRIFT …
  a landed record whose anchors this change deliberately does NOT rewrite
  (re-keying a record falsifies it)". Every prompt guard — including all five of
  mine and `TestSystemPromptDensity` — PASSES in that same run.
- **My own report's anchors were written to survive that gate**: every `path:line`
  form I quote is live in this tree, and the one quoted *failing* line
  (`self_knowledge_prompt_test.go:38`) is prefixed `    ` (indented as gate
  output), so the gate's anchor pattern cannot mistake it for a claim — verified
  by the gate not naming my file at all.

- **`gopi ui_state` returned a `frames=0` (all-blank) grid in my own sandbox run.**
  A sibling session's pty-driven surface (in an adjacent sandbox/scratch dir) is
  live and NON-blank, and that is the target the adoption run actually read; my
  own `--dump-screen` surface never painted a frame under a plain pty. The model
  handled it exactly as the tool intends (it called `ui_state`, saw the tool's own
  "this is NOT evidence that the screen is blank" refusal, and went around it via
  the surface's own pty bytes). Not a defect of this change — the section teaches
  the verb and the verb was used — but the report should not imply my own surface
  reproduced a painted frame.
- **`gopi stats`/`gopi raw` were not the model's first choice in the log run** —
  it reached for `gopi log`/`gopi stats` and then `ls`/`find` over `~/.gopi`. All
  eight verbs are named in the prompt; two of them (`ui_state`, `log`) are proven
  by real tool_use records. The epic's acceptance asks for exactly those two.
- The one log-run chat that ran long hit the model's real context ceiling
  (`1 048 576` tokens) and exited non-zero — an unrelated session-length artifact
  of that particular investigation, not of this change.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./cmd/agent/` → **BUILD_OK**
  (acceptance spelling; also built `-o /tmp/gopi-sk`, 26 725 906 bytes).
- **Tests** (branch-scoped smoke, what the diff touched):
  - `go test ./internal/agent/ -run 'TestSystemPrompt|TestAssembleSystemPrompt|TestPromptSections|TestClockRule|TestUnlimitedBudget|TestTaskToolView|TestSenseVerb|TestSelfKnowledge|TestPrompt' -count=1`
    → **ok  github.com/nick/gopi/internal/agent  3.844s**
  - `go test ./internal/agent/ -run TestSystemPromptDensity -count=1` → **ok**
  - `go test ./internal/cliverbs/ -count=1` → **ok  0.568s**
- **Ran** (exact invocations, verbatim):
  - `printf '%s\n' 'A gopi ui terminal chat is running on this box. Without screenshots and without a vision model, find out what that screen currently shows and quote a few cells from it. Use your own tools.' | NO_COLOR=1 GOPI_HOME=<sandbox> /tmp/gopi-sk chat --tools -v`
  - `printf '%s\n' 'Audit my own recent activity: which gopi sessions ran lately, how many rounds they took and roughly what they cost. Use your own tools to look this up.' | NO_COLOR=1 GOPI_HOME=<sandbox> /tmp/gopi-sk chat --tools -v`
- **Result**:
  - Run A (screen read), session `8ad636fe-748e-4ab7-9f1f-bb5d25ad2f2a`, exit 0,
    50.4 s — the model's own bash tool_use:
    `gopi ui_state -h 2>&1 | head -40`,
    `gopi ui_state --list 2>&1 | head -60`,
    `gopi ui_state --pid 793663 2>&1 | head -60`
  - Run B (self audit), session `0de445d3-732b-4955-ac1d-8be2d2eac7f8` — the
    model's own bash tool_use:
    `gopi stats -h 2>&1 | head -40; echo ---; gopi log -h 2>&1 | head -40`,
    `gopi log --last 10 2>&1 | tail -60`,
    `gopi log --all --summary 2>&1 | tail -30; echo === AGGREGATE ===; gopi stats --all 2>&1 | tail -40`,
    `gopi stats <session-id> --cost 2>&1 | grep -E "total cost|cache hit|…"`
- **Commit**: see the branch tip (this report is committed with it).

### Density BEFORE / AFTER

| | assembled prompt chars | vs 4500 gate |
|---|---|---|
| BEFORE (pristine tree, measured) | **4488** | −12 |
| AFTER (this branch) | **4498** | −2 |
| …the new section itself | 288 | |
| …header growth (identity folded in) | 81 | |
| …recovered by compressing existing blocks | 359 | |

Compression touched only prose with no test asserting it verbatim; five
phrases that a red test DID assert were restored after the first green run went
red (`memory feedback enum`, `git_blame` audit line, `Call code_search FIRST`,
`Report outcomes faithfully`, the rule-5 budget sentence) — the suite is what
told me, not my judgement.

### RED proof (both outputs recorded)

Registry entry `{Name: selfKnowledgeSectionName, Order: 5, …}` removed, then:

```
go test ./internal/agent/ -run 'TestSystemPrompt_SelfKnowledgeSection|TestSelfKnowledgeSectionIsStatic|TestSenseVerbDeclarationUsesConstants|TestSenseVerbCatalogIsComplete' -count=1 -v
=== RUN   TestSystemPrompt_SelfKnowledgeSection
    self_knowledge_prompt_test.go:38: promptSections has no "selfKnowledge" entry — the sense verbs are invisible to the model again
--- FAIL: TestSystemPrompt_SelfKnowledgeSection (0.00s)
=== RUN   TestSelfKnowledgeSectionIsStatic
--- PASS: TestSelfKnowledgeSectionIsStatic (0.00s)
=== RUN   TestSenseVerbDeclarationUsesConstants
    self_knowledge_prompt_test.go:174: promptSections has no selfKnowledge entry pointing at selfKnowledgeRules
--- FAIL: TestSenseVerbDeclarationUsesConstants (0.00s)
=== RUN   TestSenseVerbCatalogIsComplete
    self_knowledge_prompt_test.go:233: sense verb "ui_state" is in the catalogue but absent from the assembled prompt
    (… raw / stats / capture / sandbox alike …)
--- FAIL: TestSenseVerbCatalogIsComplete (0.00s)
```

Restored (entry back, `grep -c` = 1), then GREEN:

```
--- PASS: TestSystemPromptDensity (0.00s)
--- PASS: TestSystemPrompt_SelfKnowledgeSection (0.00s)
--- PASS: TestSelfKnowledgeSectionIsStatic (0.00s)
--- PASS: TestSenseVerbDeclarationUsesConstants (0.00s)
--- PASS: TestSenseVerbCatalogIsComplete (0.00s)
--- PASS: TestSystemPromptRendersTheBudget (0.00s)
ok  	github.com/nick/gopi/internal/agent	1.332s
```

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./cmd/agent/` → `BUILD_OK` (observed in job-10 output).
- `md5sum /tmp/gopi-sk /tmp/gopi-final` → both
  `54733a30ec774d29ab4e81f605e3feb1` → **IDENTICAL**: the binary used for the two
  adoption runs carries the shipped prompt.
- `strings /tmp/gopi-final | grep -a "you ARE gopi"` →
  ``you ARE gopi] `gopi <verb> -h` prints usage. Senses: ui_state = a live `gopi ui` screen as cells (no pixels); log/raw/stats = your own sessions, traffic, cost; task = your ledger row; capture + act = see+drive the desktop; sandbox = a private profile.``
  — the section is compiled INTO the shipped binary.
- Density measured with a throwaway in-package test
  (`t.Logf("TOTAL=%d", len(assembleSystemPrompt()))`, deleted before commit):
  BEFORE `TOTAL=4488`; AFTER `TOTAL=4498`; `TestSystemPromptDensity` → ok.
- Session tool_use records extracted from
  `<sandbox>/.gopi/sessions/<id>/session.jsonl` by JSON parse
  (`"name":"bash"` → `"command"`), quoted verbatim above.
- `git status --porcelain` in this worktree after commit: **clean**.
