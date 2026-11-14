<!-- doc-class: historical -->
# Task Report: gopi log banner shows wrong Model (vision auto-switch)

## Epic Question
`gopi log --chat` printed `Model: test-model` / `deepseek-v4-flash` for sessions that actually
ran `deepseek-v4-flash-vision-exp`. Was the banner hardcoded?

**Answer: No.** The banner is real data — the `model` field of the session JSONL's
`session_start` event (`internal/gopilog/formatter.go:48` reads `s.Model` from the event).
It was **stale** because of a construction-order bug.

## Root Cause
1. `cmd/agent/main.go` called `newAgent()` → `agent.NewAgent(model)` which immediately
   writes `session_start` with the resolved text model (`internal/agent/loop.go:959`,
   `internal/agent/session.go:47`).
2. AFTER the agent existed, `main.go` did the vision auto-switch:
   `if len(input.images) > 0 && overrides.Model == "" { ag.Model = deepseek.ResolveVisionModel(...) }`.
3. The wire (`loop.go:1376 BuildClaudeRequest(a.Model, ...)`) and `--raw` view (parsed from
   the actual HTTP payload) used the vision model — but the already-written `session_start`
   still said the text model.

Verified on real session `a037b7fd-d89c-487d-a7ef-24a6a80cca5c`: `session_start.model =
"deepseek-v4-flash"`, raw-log wire model = `deepseek-v4-flash-vision-exp`.

## Fix (commit 77ec70b)
- **`cmd/agent/main.go`**: resolve the vision model BEFORE constructing the agent.
  `newAgent(forcedModel string)` — forced model wins over env/settings resolution.
  `main()` computes `modelOverride` from image input + explicit `--model` first, passes it
  in, so `session_start` records the model that actually goes on the wire.
- **`cmd/agent/cli_resume.go`**: `newAgent("")` (resume always runs the resolved model; the
  existing checkpoint-model warning already guards drift).
- **`internal/agent/loop_test.go` + `thinking_trim_test.go`**: `TestSystemPrompt_*` and
  `TestNewAgentKeepThinkingEnv` called `NewAgent("test-model")` WITHOUT `newTestAgentEnv(t)`
  → wrote real `~/.gopi/sessions` junk. Added the isolation helper to all three tests.
  (This was the source of the `test-model` / workdir `internal/agent` sessions in the Epic —
  a second, independent misleading-banner contributor.)
- **`cmd/agent/main_test.go`**: `TestNewAgent_ImageInputRecordsVisionModel` regression test —
  forces a distinctive vision model and asserts `session_start.model` == `ag.Model`.

## Not Done
- Did NOT mass-delete the ~834 historical `test-model` junk sessions/raw logs accumulated in
  `~/.gopi` from past test runs (dangerous bulk delete, out of scope; tests no longer create
  new ones). The 4 newest junk sessions + 2 empty raw logs matching the Epic timestamps were
  removed after per-file `"test-model"` verification.
- Did not change the display code in `internal/gopilog/formatter.go` — it was correct; the
  data was wrong.
- `/usr/local/bin/gopi` (installed binary) not rebuilt — not needed, the fix is in recorded
  data, not display.

## Verification
- Build: `go build ./...` — OK
- Vet: `go vet ./cmd/agent/ ./internal/agent/` — clean
- Tests: `go test ./...` — ALL packages PASS (incl. new regression test)
- Live smoke (temp HOME): `gopi /tmp/smoke.png "describe this image"` →
  session_start `model: deepseek-v4-flash-vision-exp`; `gopi log --last --chat` banner:
  `Model: deepseek-v4-flash-vision-exp` ✅ (was `deepseek-v4-flash`)
- Commit: `77ec70b` (fix(log): session_start records the wire model for image auto-switch)
