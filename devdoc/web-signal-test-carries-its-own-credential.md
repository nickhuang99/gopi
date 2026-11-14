<!-- doc-class: historical -->
# `TestWebToolsSignalShutdownWritesSessionEnd` carries its own provider, not the operator's key

Task `01a134d4-2117-7610-ad6a-f509a5d21644` (ticket C of three pre-existing reds observed
during a `gopi merge` hand-over run, all three reproduced at `58e8f4681`).

Class: `test-infrastructure`. Status: fixed on this branch.

## The symptom, measured

```
$ env -u ANTHROPIC_AUTH_TOKEN -u DEEPSEEK_API_KEY -u ANTHROPIC_BASE_URL -u DEEPSEEK_BASE_URL \
      go test ./cmd/agent/ -run TestWebToolsSignalShutdownWritesSessionEnd -count=1
--- FAIL: TestWebToolsSignalShutdownWritesSessionEnd (2.75s)
    cli_web_align_test.go:529: gopi [web --addr=127.0.0.1:0]: timed out waiting for the listening banner
        --- stdout ---
        --- stderr ---
        gopi web: failed to create agent: failed to create DeepSeek Claude client: deepseek: ANTHROPIC_AUTH_TOKEN not set (env, DEEPSEEK_API_KEY, or ~/.gopi/settings.json)
FAIL
```

Two facts in one output: the failure message points at the **banner** (a readiness
timeout), while the child's stderr says why — the server **exited 1 before serving**.

## Root cause (5-why)

1. Why did the JSONL never appear? — `gopi web` never got as far as constructing the agent.
2. Why not? — `runWeb` (`cmd/agent/cli_web.go:393-406`) builds the ONE persistent agent
   **before** `Serve`, and `return 1` when construction fails.
3. Why does construction fail? — `deepseek.ResolveClaudeAPIKey` (`claude_client.go:647-656`)
   resolves `ANTHROPIC_AUTH_TOKEN` > `DEEPSEEK_API_KEY` > settings `api_key`, and errors with
   the message above when none is present.
4. Why was that not anticipated? — the test carried an environment assumption in prose:
   *"the key is NOT a precondition: `gopi web` boots, writes its session JSONL and finalizes
   the session on SIGTERM with no key at all"* (`cli_web_align_test.go`, commit `f6694fc89`,
   2026-09-11). **That sentence was false when it was written**: `9fe3d48cf` — the same
   web-align series that added the test — put `newWebAgent()` in front of `Serve`, so the
   no-key path already exited 1.
5. Why did nobody see it? — the red is environment-dependent (a host with a key exports one
   and the case passes silently), and its message blames a **timing** deadline rather than the
   missing credential, so it read as a flake. It is recorded as exactly that in two past runs'
   memory: "环境类假红，ANTHROPIC_AUTH_TOKEN 未设".

## The fix

The credential is now **supplied by the test**, not borrowed from the ambient environment:

```go
llm := testutil.NewMockLLM(t, testutil.Resp{Text: webSignalTurnReplyText})
defer llm.Close()
env := testutil.MockEnv(t, home, llm.URL())
```

`testutil.MockEnv` is the ONE script that decides a mocked run's environment
(`internal/testutil/run.go:311`): it strips every ambient `ANTHROPIC_*`/`DEEPSEEK_*`/`GOPI_*`
variable and injects `ANTHROPIC_AUTH_TOKEN=test-key` + `ANTHROPIC_BASE_URL=<mock>`. The driven
turn therefore hits the scripted provider — hermetic, no real model call, no operator token
either required or able to mask the absence.

Three consequences carried in the same commit:

- The turn assertion **stopped tolerating** a turn that never reaches `event: done`
  (`t.Logf` → `t.Errorf`). The tolerance existed only because the model call was real and
  optional; with a scripted provider the same miss is a genuine failure.
- The false prose was deleted and replaced by the measured history (Rule 15: a comment that
  contradicts the code is the defect, not a convenience).
- `webSignalTurnReplyText` is a named constant (Rule 1a), so the script and the assertion
  cannot drift.

## Verification (measured in this run)

| command | before | after |
|---|---|---|
| `env -u ANTHROPIC_AUTH_TOKEN … go test ./cmd/agent/ -run TestWebToolsSignalShutdownWritesSessionEnd -count=1` | `FAIL (2.75s)` | **`PASS (1.62s)`** |
| `go test ./cmd/agent/ -run TestWebToolsSignalShutdownWritesSessionEnd -count=2` (operator token present) | `PASS` | `ok 6.706s` |

`-v` run: `--- PASS: TestWebToolsSignalShutdownWritesSessionEnd (1.62s)`.

## Not done / honest limits

- Older runs on this repo used a different workaround
  (`ANTHROPIC_AUTH_TOKEN=dummy ANTHROPIC_BASE_URL=http://127.0.0.1:9`, recorded in the merge-run
  memory). That is a **caller-supplied** export, so it reinstates exactly the environment
  dependence this change removes; it is superseded, not preserved.
- The readiness wait is still the session JSONL; the fix does not change the artifact the test
  asserts, and the boot-timeout constant (`webSignalBootTimeout = 20s`) is untouched.
