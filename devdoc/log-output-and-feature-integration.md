<!-- doc-class: normative -->
# Feature-Integration Testing + "gopi outputs the whole log" Root Cause

## TL;DR

The Epic reported "gopi now outputs full log to stdout instead of just the
response". Investigation found the true defect: **with the new help/config
integration, every ordinary run turned on DEBUG logging and dumped the raw
request/stream bodies to stderr** (52 KB of noise on a trivial "reply pong"
task), which in a user's terminal (where stderr+stdout merge) drowned the
actual response. Root cause + fix are below. The log-reader / help-config /
memory & web_search CLI integrations themselves were verified working and are
already well unit-tested.

## The root cause (this run's main deliverable)

Chain that produced the flood:

1. `config.FlagOverrides(args)` returns a `Config` whose un-set knobs are all
   zero values (`Debug=false`, `TimeoutSec=0`, `ContextWindow=0`, ...).
2. `main()` calls `config.ApplyEnv(overrides, fromFlags=true)`. The `setBool`
   / `setInt` / `setDur` helpers, with `fromFlags==true`, **force-write the
   value for every knob** — including zero/`off` for flags the user never
   passed. So a bare `gopi ...` run pushed `GOPI_DEBUG=off`, `GOPI_TIMEOUT_SEC=0`,
   `GOPI_CONTEXT_WINDOW=0`, `GOPI_THINKING_BUDGET=0`,
   `GOPI_COMPACT_MAX_SUMMARY_TOKENS=0`, ... into the process env.
3. `internal/agent/NewAgent` set `Debug: os.Getenv("GOPI_DEBUG") != ""`.
   A literal `"off"` is non-empty → **Debug = true** → every `if a.Debug`
   branch fired: the full `→ REQUEST [...]` body (with the whole system
   prompt / GOPI.md), `← STREAM [...]` content dumps, per-tool result sizes.
   Result: ~52 KB of debug junk on stderr for a one-word answer.
4. Separately, the injected `GOPI_*_SEC=0` / `=0` values triggered the
   "invalid GOPI_X=0, using default ..." warning lines on every start.

### The fix (two layers, defense in depth)

1. **Stop force-writing unset knobs** — `Config.SetEnvVars map[string]bool`
   (`json:"-"`). `FlagOverrides` records the env key of each flag that was
   ACTUALLY passed. `ApplyEnv(fromFlags=true)` now only force-writes keys in
   `SetEnvVars`. This kills the zero-value cascade (and the invalid-`=0`
   warnings) and restores the intended precedence — a flag wins over env only
   when the user actually passed it.
2. **Truthy-only Debug gate** — `NewAgent.Debug` now uses a new
   `boolEnvTrue("GOPI_DEBUG")` (accepts `1/on/yes/true/enabled`) instead of
   `!= ""`, matching the `== "1"` convention `tools/bash.go` uses for
   `GOPI_ALLOW_DANGEROUS` and `config.normalizedBoolValue`. So even if an
   `"off"` value reaches env by any path, it means off.

### Verification (before → after)

Control experiment, trivial "reply pong", stdout & stderr separated:

- stderr BEFORE: `52,376` bytes — `→ REQUEST` body dump, `← STREAM`, per-tool
  logs, plus 6 `invalid GOPI_*=0` warnings.
- stderr AFTER: `713` bytes — `[gopi] loaded GOPI.md`, raw-log/session paths,
  the live-streamed answer (by design, see below), and the timing summary.
- stdout unchanged both ways: `pong\n\n[TASK_LIST...]` (the returned `result`).

## stdout vs stderr contract (documented, not a bug)

- **stdout**: only the final `result` string (`cmd/agent/main.go:876 fmt.Print(result)`).
  Scriptable consumers should read stdout — it is the agent's answer. Note it
  currently includes any `[TASK_LIST_BEGIN]...[TASK_LIST_END]` block the model
  put in its final reply; that is part of the returned string, not diagnostic
  logging.
- **stderr**: all diagnostics — `[gopi]` warnings, raw-log & session paths, the
  LIVE-streamed answer (so the user sees progress), the `[thinking]` stream,
  the timing/trajectory summary. This is intentional (live UX); a call site
  that wants ONLY the answer should take stdout.

## Feature integrations verified (manual + existing unit tests)

### gopi log (log-reader subcommand) — PASS
- `gopi log <id>`, `<id> --response|--thinking|--chat|--todo|--memory|--json|--text`,
  `--request [--json]`, `--last [N]`, `--all --summary`, repeatable
  `--filter TOOL`. All exercised against real session files.
- Existing tests: `internal/gopilog` 49 funcs (reader/formatter/rawlog);
  `cmd/agent/cli_log_test.go` 17 funcs (arg parsing, filter resolution,
  negative-dispatch).

### Help / config (gopi -h, --help-env, settings.json, CLI flags) — PASS
- `gopi -h` renders the full usage incl. all subcommands + the env table.
- `gopi --help-env` prints the grouped env/settings-key/flag table (81 lines).
- `--settings <file>` loads settings.json; unknown keys ignored; precedence
  flag > env > settings > default.
- Unknown / value-mismatched flags error instead of silently ignoring.
- Existing tests: `internal/config` 16 funcs; `cmd/agent/usage_test.go`
  3 funcs. **Added** 3 regression tests for the new SetEnvVars behavior.

### memory_search / memory_store CLI — PASS
- `gopi memory_search "<q>" [--limit --intent --db]`, `gopi memory_store
  --topic --question --answer [--keywords --source --db]`. Store→search
  round-trip on an isolated `--db` verified (data round-trips; dedup signal
  correct; ASK-OR-fallback still recalls on rephrased queries).
- Existing tests: `cmd/agent/cli_memory_test.go` 12 funcs incl. a real
  store-then-search round-trip on a temp DB.

### web_search CLI — PASS
- `gopi web_search "<q>" [--method bing|google|arxiv|deepseek|github] [--count N]`;
  `--help` correct; unknown method / bad count error out.
- Existing tests: `cmd/agent/cli_websearch_test.go` 11 funcs.

## Other discovery

`cmd/agent/config_test.go` (the `TestApplyEnvSettingsMirrorsDeepseekFallbacks`
test) failed in dev shells because it did not unset the exported
`ANTHROPIC_AUTH_TOKEN`/`DEEPSEEK_API_KEY` credentials. `ApplyEnv(settings,false)`
correctly refuses to clobber an existing env var, so the assertion broke on an
environment quirk in any shell that has real keys exported. Fixed by setting the
six endpoint env vars empty first (hermetic). ~This was the only failing test
before the root-cause fix above.~

## New/changed files

- `internal/config/config.go` — `Config.SetEnvVars map[string]bool`.
- `internal/config/flags.go` — `FlagOverrides` populates `SetEnvVars` from a
  `flag→env` catalog; only passed flags marked.
- `internal/config/env.go` — `ApplyEnv(fromFlags=true)` only force-writes keys
  in `SetEnvVars`.
- `internal/config/config_test.go` — hermetic fallback test + 3 SetEnvVars
  regression tests.
- `internal/agent/loop.go` — `boolEnvTrue()` helper; `Debug` uses it.
- `internal/agent/loop_test.go` — `TestBoolEnvTrue` truth table.

## Test summary (post-fix)

`go build ./...` OK. `go test ./... -count=1`: **505 PASS / 0 FAIL** across
cmd/agent, internal/agent, internal/config, internal/deepseek,
internal/gopilog, internal/tools.
