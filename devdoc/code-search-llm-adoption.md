<!-- doc-class: historical -->
# Devdoc: code_search LLM adoption — diagnosis + persuasion + coverage fix

Epic: gopi rarely calls code_search during heavy code-review tasks, and never
uses it for definitions in languages beyond go/c/c++/go even though ctags
covers more. Investigated the tool's own prompt surfaces + dsh/codex
baselines, then changed both the backend coverage and the persuasion text.

## Diagnosis (measured, not assumed)

- Session audit (~26k tool_use across 792 sessions, ~/.gopi/sessions/*.jsonl):
  code_search = 121 calls total (~0.5%). In grep-heavy review sessions it
  appears 1–6 times against 20–100 bash calls (e.g. 1/83, 1/102, 3/67).
- The ALWAYS/NEVER anchors from the 2026-08-27 round (memory #1007) are
  present and survived — anchor presence alone did NOT move adoption.
- Root causes (5-why): (1) trained habit — the model's default pipeline for
  "understand code" is bash+read+grep; code_search has no trained analog, so
  routing must be supplied by prompt, and the old text only framed it as a
  faster grep substitute (efficiency argument), never as a REQUIRED STEP of
  the review workflow; (2) the C/C++ gap was real in the backend:
  indexedSourceExtensions deliberately excluded `.h` (and `.c`), so for
  header-centric codebases the tool was genuinely blind — a reviewer burned
  once reverts to grep forever.

## Baselines studied

- dsh (~/work/deepseek-harness, packages/fs/tool-fs-search/src/grep.ts,
  glob.ts): short functional descriptions; no definition-lookup tool at all.
- codex (~/work/codex, codex-rs/core/gpt_5_2_prompt.md:250): just
  "prefer rg because it is much faster" — again no ctags tool.
- Conclusion: no major harness has a definition-lookup tool, so the LLM has
  no trained habit for it; adoption is entirely prompt-supplied. The winning
  framing is therefore workflow-anchored ("when reviewing, resolve each
  unknown identifier FIRST via code_search"), not just efficiency-framed.

## Changes

1. internal/tools/symbol.go — indexedSourceExtensions += ".c", ".h";
   ctagsLanguages += ",C". Verified with universal-ctags 5.9.0 that .h
   parses under the C++ parser even when only C++ was listed; both langs now
   listed for clarity. Note: C++ must stay in the list or .h files yield
   nothing.
2. internal/agent/loop.go claudeToolDefs:
   - code_search description: added "macro" to the symbol kinds; added
     "C/C++ incl. .h headers" to the language list; added the workflow
     anchor "When reviewing or reading unfamiliar code, resolve EVERY
     unknown identifier here FIRST — before grep, before guessing from
     includes."
   - keyword_search description: "...use code_search, not grep."
   (grep.ts in dsh has a similar usage-steer ending — functional model.)
3. internal/agent/loop.go toolRoutingRules code_search line: added "Reviewing
   code? resolve each unknown identifier via code_search (C/C++ headers
   incl.)". To fit the 4500-char density gate, compressed the same line
   (dropped the "NewClient" example) and dropped the rm-rf/GOPI_ALLOW_DANGEROUS
   sentence from the bash routing line (it is duplicated in the bash tool
   description, which the tests already guard).

## Guard substrings preserved (tests would fail otherwise)

- "returns every definition in ONE structured call" (TestSystemPrompt_ToolRoutingAuditFixes)
- "Call code_search FIRST with the bare identifier" (loop_test.go)
- "not shell grep" (TestSystemPromptCounterExamples)
- description side: "NOT for searching code", "Faster and cleaner than bash
  grep", "For locating a symbol DEFINITION use code_search"
- TestCodeSearchScopeCoversIndexedLanguages (globs must match extensions —
  .c/.h globs now flow from the same list)
- TestSystemPromptDensity (< 4500 chars)

## Verification

- go build ./...: OK
- go test ./internal/agent/ -run TestSystemPrompt: ok (density gate passed
  after compression)
- go test ./internal/tools/ -run 'TestCodeSearch|TestTagsBinarySearch|TestEnsureTags': ok
- End-to-end (bin/gopi code-search): fixture src/t.h + src/src.c —
  `Node` → src/t.h:3 struct, `node_sum` → src/src.c:2 func. .h/.c now indexed.
- LLM adoption proof (Rule 11): two natural review tasks run through
  ./bin/gopi (session tool_use evidence appended below after runs).

## Second-order findings (from the live proof runs)

- The system prompt's own [BASH EFFICIENCY RULES] example — `grep -rn "x"
  --include="*.go" internal/` — was CAUGHT IN THE ACT: session
  b353b74c… quoted it verbatim in thinking to justify bash grep for a bulk
  definition inventory ("the BASH EFFICIENCY RULES explicitly show `grep -rn
  ...` as the example. I'll do that"). The prompt was feeding the habit it
  forbids. Fixed: the example is now `go build ./... && go test ./... -run
  TestX` (density re-balanced to 4499/4500).
- New-binary sessions (0cac8286…, e7b5b5a9…) open their first tool round
  with parallel code_search calls for the review targets (session jsonl
  evidence), i.e. the review→code_search reflex is forming.

## Rule 11 adoption evidence (session jsonl tool_use counts)

| session | binary | task | code_search | bash | notes |
|---|---|---|---|---|---|
| b353b74c-bc98 | OLD | review symbol.go | 0 | 8 | cited the old [BASH EFFICIENCY RULES] grep example verbatim to justify grep → example rewritten |
| 06a2e2c6-21cc | OLD | review cli_code_search.go | 3 | 6 | baseline; code_search only after memory priming |
| 0cac8286-9604 | NEW | review loop.go tool defs | 2 (first round, parallel) | 0 so far | first tool round = code_search×2 |
| e7b5b5a9-2744 | NEW | review memory.go schema | 1 (first round) | 4 | code_search first round; bash for runtime queries |

Old-binary evidence exposes the failure mode (grep habit + prompt
self-contradiction); new-binary evidence shows the intended first-reflex:
code_search resolves the review targets before anything else.

## Not done

- .pyi stub files: ctags returns nothing for them; skipped (marginal).
- Adoption rate is expected to improve but the 0.5% baseline was measured on
  mixed historical traffic; re-measure after more review sessions accumulate.
