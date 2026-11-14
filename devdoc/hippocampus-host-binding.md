<!-- doc-class: historical -->
# Task Report: the hippocampus host binding is now the LIVE organ

**Epic**: bind the live organ to `internal/hippocampus.NewHostEngine` (option a) so the
host floors + the reply-shape retry actually apply in production.
**Branch**: `feat/hippocampus-host-binding` (worktree `~/workspace/gopi-hippo-bind`).
**Status**: implemented, verified; committed (see Verification).

## Completed

### 1. The defect that was fixed

`internal/hippocampus/host.go` declared the package's **production binding** —
`NewHostEngine` + `ClientAdapter` + `HostEngineClientTimeout = 90s` /
`HostEngineCycleTimeout = 4min` — and **no production code called it**. The live
organ was built one line inside `internal/agent/hippocampus_agent.go`:

```go
engine := hippocampus.New(&hippoClaudeClient{c: c, model: model}, hippocampus.ToolsMemory{})
```

so every production session ran:

| | declared host binding | what production actually ran |
|---|---|---|
| per-LLM-call timeout | `HostEngineClientTimeout` = 90s | `DefaultClientTimeout` = **10s** (the batch-pipeline value the 90s floor's own comment says it exists to override) |
| whole-cycle timeout | `HostEngineCycleTimeout` = 4min | `DefaultTimeout` = **20s** |
| JSON-less-reply retry | `ClientAdapter` (bounded, `ClientAdapterDefaultRetries`) | **none** |

The unused half compiled, had doc comments asserting intent, and passed every
test that named it — only a caller-graph check reveals it.

### 2. The fix (option a — the live binder *is* the host binding)

- `internal/agent/hippocampus_agent.go` gains one named seam,
  **`newLiveHippocampusEngine(c *deepseek.Client, model string) (*hippocampus.Engine, bool)`**,
  which delegates to `hippocampus.NewHostEngine(&hippoClaudeClient{…})`. The raw
  `hippocampus.New` call is gone from the live path, so the floors and the
  `ClientAdapter` retry are the single source of truth again.
- The floors stay in `internal/hippocampus/host.go` (one definition, Rule 14) and
  the live path *reads* them through `NewHostEngine` — nothing is re-declared on
  the agent side.
- **Rule 15 / `ok=false`**: `NewHostEngine`'s `ok=false` (and a nil transport) is
  handled explicitly. `newHippocampusRecallFunc` now **returns nil** when no organ
  could be built and logs the reason; `bindHippocampusRecall` treats a nil recall
  function as "leave the host unbound" (the run degrades to the gate-off
  behaviour) instead of installing an organ that archives a
  retrieval-failure marker every round. A nil engine can no longer reach
  `Engine.Run` and panic.
- **Rule 2 unchanged**: `NewHostEngine` reads the knobs through the organ
  package's own `LoadConfig` — still the single env reader.
- **Rule 4**: surgical edits only (one new function, one call site swapped, the
  nil path made explicit).

### 3. Observability (deliverable 2)

`(*hippocampus.Engine).Config()` — the exact mirror of the existing
`Context()` accessor:

```go
func (e *Engine) Config() Config { if e == nil { return Config{} }; return e.cfg }
```

It returns a **copy**, so a caller cannot re-budget a running engine through it
(asserted by a test). Without it, an engine's settled budgets are unobservable —
there is no other way to assert *which* timeouts the live path runs with.

### 4. Regression tests

- `internal/hippocampus/host_test.go` (new) — the host binding's own contract:
  nil guard (`NewHostEngine(nil)` → `(nil,false)`; a bound-but-failing transport
  is *recorded*, never a silent success), the floors reach the engine config
  (raised when tiny, **preserved when larger**, read through the new
  `Config()`), `Config()` is a copy and nil-safe, and the retry additions not
  already pinned by `cov_hippocampus_test.go` (an explicit positive budget, a
  cancelled context stops the loop, a usable reply costs exactly one attempt).
- `internal/agent/hippocampus_binding_test.go` (new) — the **LIVE** path:
  * `TestHippocampus_LiveBinderMeetsHostFloors` — with
    `GOPI_HIPPOCAMPUS_CLIENT_TIMEOUT_SEC=1`, the live engine's
    `Config().ClientTimeout >= hippocampus.HostEngineClientTimeout` and
    `Config().Timeout >= HostEngineCycleTimeout`.
  * `TestHippocampus_LiveBinderRetriesJSONLessReply` — a **real cycle** over the
    live binder against an `httptest` Claude endpoint: reply #1 is prose with no
    JSON object, reply #2 is a valid query list. The live transport must see
    **≥2** HTTP requests (i.e. the retry fired) and the cycle must return a brief.
  * `TestHippocampus_LiveBinderWithoutTransportStaysUnbound` — Rule 15: no
    transport ⇒ unbound host + a log line, never a silent stub.
  * `TestHippocampus_LivePathConstructsThroughHostBinding` — Rule 13-style
    exhaustive literal gate: no production `.go` file in `internal/agent` may
    contain `hippocampus.New(` (the bypassing constructor); the only legal live
    construction is `NewHostEngine` behind `newLiveHippocampusEngine`.

**Pre-fix evidence (the tests fail before the fix).** With the live binder
temporarily restored to the raw `hippocampus.New(...)`:

```
--- FAIL: TestHippocampus_LiveBinderMeetsHostFloors (0.00s)
    hippocampus_binding_test.go:66: live ClientTimeout = 1s, want at least the host floor 1m30s —
      the live path is running the batch default (it must build through hippocampus.NewHostEngine)
    hippocampus_binding_test.go:71: live cycle Timeout = 20s, want at least the host floor 4m0s
--- FAIL: TestHippocampus_LiveBinderRetriesJSONLessReply (0.00s)
    hippocampus_binding_test.go:142: the JSON-less reply earned 1 HTTP request(s) — the reply-shape
      retry is NOT on the live path (want at least 2: the rejected prose + the retry)
--- FAIL: TestHippocampus_LivePathConstructsThroughHostBinding (0.00s)
    hippocampus_binding_test.go:208: hippocampus_agent.go builds the organ with "hippocampus.New("
      — the live path must go through newLiveHippocampusEngine (hippocampus.NewHostEngine) ...
```

Restoring `NewHostEngine` makes all of them pass.

## Not Done

- **A typed-nil transport is not rejected by `NewHostEngine`** (an interface
  holding a nil `*T` passes its `c == nil` check). No caller can produce one —
  the live binder allocates its transport — so this is recorded as a known
  limitation rather than papered over with reflection; `host_test.go` says so in
  the guard test's comment.
- **No real-LLM (Rule 11) adoption run.** This change adds no tool and changes no
  prompt, so the LLM-adoption gate does not apply; the transport-level proof is
  done with a deterministic `httptest` stub (no network, no key), per the
  constraint "tests must be deterministic/no network".
- **The `hippoClaudeClient` effort-mode fallback was not re-tested end-to-end**
  (it is unchanged and already covered by its own behaviour); the new cycle test
  exercises the plain thinking-disabled path only.
- **The gate-on end-to-end session test still injects a stub organ** via
  `SetHippocampusRecall`; wiring the *real* organ into that scripted run was out
  of scope (it would need a live memory DB and a scripted multi-call transport
  that the run's own SSE endpoint serves).
- No change to the brew pipeline or to any sibling organ behaviour; nothing was
  merged into master (Rule 6/9 — the branch is the deliverable).

## Verification

```
$ gofmt -l internal/agent internal/hippocampus          # no output
$ go build ./...                                        # BUILD_OK
$ go vet ./internal/hippocampus/ ./internal/agent/      # VET_OK
$ go test -count=1 ./internal/hippocampus/ ./internal/agent/   # exit=0
ok  	github.com/nick/gopi/internal/hippocampus	57.793s
ok  	github.com/nick/gopi/internal/agent	    308.653s
```

`internal/agent` was also run alone three times (`256.4s ok`, `318.6s ok`,
`464.3s FAIL`) — the one FAIL was **not** this change:

```
--- FAIL: TestT2_E3_ResumeCarriesForwardCommittedConclusions (5.90s)
    taskarea_takeover_test.go:213: commit original: write_task_area: schema: context deadline exceeded
```

That test is a timing-sensitive takeover/resume test (`taskarea_takeover_test.go`,
untouched by this task). **Reproduced on pristine master** with the same failure —
`cd /home/user/work/GoPi && go test -count=1 -run TestT2_E3_Resume… ./internal/agent/`
gave `FAIL / FAIL / ok` across three runs (`schema: context deadline exceeded`
again), i.e. a pre-existing flake, filed here for the owner rather than
"fixed" inside this task's scope. Every run of the touched package
(`internal/hippocampus`, always green: 54.3s / 57.8s) and the focused
live-path selection is deterministic.

Focused live-path run (green after the fix):

```
$ go test -count=1 -run TestHippocampus_Live -v ./internal/agent/
--- PASS: TestHippocampus_LiveBinderMeetsHostFloors (0.00s)
--- PASS: TestHippocampus_LiveBinderKeepsDeliberateLargerKnobs (0.00s)
--- PASS: TestHippocampus_LiveBinderRetriesJSONLessReply (0.79s)
--- PASS: TestHippocampus_LiveBinderWithoutTransportStaysUnbound (0.08s)
--- PASS: TestHippocampus_LivePathConstructsThroughHostBinding (0.00s)
ok  	github.com/nick/gopi/internal/agent	0.902s
```

The live caller exists (the acceptance grep):

```
$ grep -rn 'NewHostEngine' --include='*.go' . | grep -v _test
./internal/agent/hippocampus_agent.go:175:	return hippocampus.NewHostEngine(&hippoClaudeClient{c: c, model: model})
./internal/hippocampus/host.go:109:func NewHostEngine(c HostClient) (*Engine, bool) {
```

Commit: `1f68439 fix(hippocampus): bind the live organ to NewHostEngine (host floors + reply-shape retry)` —
branch `feat/hippocampus-host-binding`, worktree `~/workspace/gopi-hippo-bind`
(shared refs with the main repo, so the commit IS on the branch — no push needed, Rule 6c).

## Files touched

| file | change |
|---|---|
| `internal/agent/hippocampus_agent.go` | live binder → `newLiveHippocampusEngine` → `NewHostEngine`; nil-organ path explicit + logged |
| `internal/hippocampus/engine.go` | `(*Engine).Config()` accessor (copy) |
| `internal/hippocampus/host.go` | doc comment corrected (the binding names `hippocampus_agent.go`, not the never-created `hippocampus_binding.go`) |
| `internal/agent/hippocampus_binding_test.go` | new: live-path floors + retry + nil guard + construction gate |
| `internal/hippocampus/host_test.go` | new: host binding contract tests |
| `devdoc/hippocampus-host-binding.md` | this report |
