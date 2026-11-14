<!-- doc-class: historical -->
# Verification Report: session_start records the wire model for image auto-switch

Task: verify commit `77ec70b` (fix(log)) — confirm the feature's claims hold against the
actual repo state.

## Verdict
**VERIFIED — all claims in the commit message hold.** Build clean, full test suite green,
regression test present and passing, test-isolation fixes correctly scoped. No uncommitted
changes to any tracked feature file.

## Completed (what I confirmed)

1. **Commit exists & reaches HEAD.** `git cat-file -t 77ec70b` → commit; `git branch
   --contains 77ec70b` → master. It is the direct parent of HEAD `cedc7db` (the follow-up
   docs commit). No uncommitted changes to `cmd/agent` or `internal/agent`.

2. **Core fix — resolve vision model BEFORE constructing the agent.** In
   `cmd/agent/main.go`, `newAgent()` now takes a `forcedModel string`:
   - `main()` computes `modelOverride := ""`; when `len(input.images) > 0 &&
     overrides.Model == ""` it sets `modelOverride = deepseek.ResolveVisionModel(...)`
     and prints the stderr notice. This happens **before** the `ag, err := newAgent(modelOverride)`
     call, so `session_start` records the wire model.
   - `newAgent` overrides: `model := deepseek.ResolveModel(cfg); if forcedModel != "" { model =
     forcedModel }`.
   - Confirmed call sites: `main.go:102 newAgent(modelOverride)`, `cli_resume.go:131
     newAgent("")` (resume passes no override, per the fix intent).

3. **Resume passes no override.** `cmd/agent/cli_resume.go` calls `newAgent("")` with a comment
   explaining resume always runs the resolved model (checkpoint carries the conversation; the
   existing model-drift warning guards it).

4. **Test isolation — the three named tests call `newTestAgentEnv(t)`.**
   - `internal/agent/loop_test.go`: `TestSystemPrompt_IncludesBashEfficiencyRules` and
     `TestSystemPrompt_CounterExampleRoutingRules` each added `newTestAgentEnv(t)`.
   - `internal/agent/thinking_trim_test.go`: `TestNewAgentKeepThinkingEnv` added
     `newTestAgentEnv(t)`.
   - The commit diff touched only these 3 tests in the two files; `effort_test.go` (5 uses) and
     the `deepseek-v4-pro` loop tests (12 uses) already had the helper — commit scope is
     precise, no collateral edits. `newTestAgentEnv` sets HOME to a temp dir, GOPI_SETTINGS to
     a nonexistent file, DEEPSEEK_API_KEY, and GOPI_RAW_LOG=OFF — the HOME isolation is the
     real guard against writing to `~/.gopi/sessions`.

5. **Regression test present.** `cmd/agent/main_test.go` has
   `TestNewAgent_ImageInputRecordsVisionModel` which forces `GOPI_VISION_MODEL=my-vision-model`,
   calls `newAgent("my-vision-model")`, and asserts both `ag.Model` and the parsed
   `session_start.model` equal `my-vision-model` — directly guarding the bug.
   `testAgentEnv` (main_test.go variant) additionally clears `DEEPSEEK_MODEL`,
   `ANTHROPIC_MODEL`, `GOPI_VISION_MODEL`.

## Verification (measurements)
- Build: `go build ./...` → exit 0 (OK)
- Regression test: `go test ./cmd/agent/ -run TestNewAgent_ImageInputRecordsVisionModel -v`
  → PASS
- Full suite: `go test ./...` → ALL packages PASS
  (`cmd/agent`, `internal/agent`, `internal/checkpoint`, `internal/config`,
  `internal/deepseek`, `internal/gopilog`, `internal/tools`); exit 0.
- No uncommitted changes to tracked feature files (`git status --short -- cmd/agent
  internal/agent` empty).

## Not Done
- Did not re-run a live image-pipeline smoke test (the prior devdoc
  `session-model-banner-fix.md` already did the live smoke; this task is source-level
  verification of the committed fix, and the source + regression test fully cover the
  ordering contract).
- Did not rebuild the installed `/usr/local/bin/gopi` binary (out of scope; the fix is in
  recorded data, not display).

## Note
The follow-up commit `cedc7db` (docs) already committed the original task report
`devdoc/session-model-banner-fix.md` which documents root cause + fix. This report is the
independent **verification** pass confirming the committed code matches the described fix.
