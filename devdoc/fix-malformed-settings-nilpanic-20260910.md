<!-- doc-class: normative -->
# Task Report: malformed settings.json must never nil-panic (P1-D fix)

Fixes devdoc/review2-cli-config-options-variant-20260903.md finding **P1-D**
(verified live on master before this fix).

## Completed

- **Chosen behavior (single source of truth): hard-exit.** A malformed
  settings.json now exits with code 1 and a clean, actionable error naming
  the settings path AND the JSON syntax problem:
  ```
  [gopi] config: config: failed to parse /tmp/bad-settings.json: invalid character '}' ...
  [gopi] fix the settings file and re-run, or pass --settings PATH to use a different one
  ```
  Rationale: settings.json is the provider surface (api_key / base_url /
  model). Silently continuing on empty defaults would only fail later at the
  API call with an untraceable auth/network error — a hard exit is the honest
  behavior. The subcommand chain (`gopi task run` etc.) returns from main
  BEFORE this config block, so the hard exit only affects the agent path.

- **Root cause (why the panic happened):** `config.FromSettingsFile` returns
  `(nil, error)` on a JSON parse error (only the missing-file path returns a
  non-nil empty Config). main.go printed the error and CONTINUED, passing the
  nil `*Config` into `config.ApplyEnv(nil, false)`, which dereferences
  `cfg.SetEnvVars`/fields at `internal/config/env.go:117` → nil-pointer
  panic. main.go was the **only** caller of FromSettingsFile that ignored the
  error — every other call site (`config.go` peer/node/task resolvers,
  `node_role.go`) correctly returns the error or fail-softs on it.

- **Fix (surgical, cmd/agent/main.go only):** on `sErr != nil`, print the
  error + the actionable hint, then `os.Exit(1)`. `ApplyEnv` is never reached
  with a nil base.

- **Regression test** `cmd/agent/settings_nilpanic_test.go`
  `TestMalformedSettingsNoPanic`: drives the REAL gopi binary through main's
  config path (`--settings <malformed> "say hi"`) with a scrubbed env
  (ambient GOPI_*/ANTHROPIC_*/DEEPSEEK_* dropped). Asserts: no "panic:" on
  stderr, non-zero exit, and the error names the settings path + parse
  problem. Shares the package-level `gopiBinOnce`/`gopiBinErr` binary cache
  with the other binary-driving tests.

## Not Done

- Did NOT change `FromSettingsFile`'s `(nil, error)` parse-error contract.
  The review doc suggested that as layer (1) of a two-layer fix, but it is
  unnecessary: main.go was the sole misbehaving caller, and keeping the
  explicit error contract lets every other caller keep its fail-soft
  semantics. One layer (the main.go guard) fully closes the crash.
- No change to the settings-channel semantics elsewhere (env/flag readers
  untouched).

## Verification

- Pre-fix repro (git stash): `panic: runtime error: invalid memory address or
  nil pointer dereference` at `internal/config/env.go:117`, exit 2. Confirms
  the bug and that the regression test would catch it (red).
- Post-fix: `[gopi] config: config: failed to parse … invalid character '}'`
  + hint line, exit 1. No panic (green).
- Test: `go test ./cmd/agent/ -run TestMalformedSettingsNoPanic -count=1`
  → PASS (0.87s).
- Build: `go build ./...` OK; `go vet ./cmd/agent/` clean.
- Full suite: `go test ./...` — see job output (this repo's suite runs
  several minutes).

## Report

- Branch: `feat/fix-malformed-settings-nilpanic`
- Recovery: `git show feat/fix-malformed-settings-nilpanic:devdoc/fix-malformed-settings-nilpanic-20260910.md`
- Commit: `cd8f9e2` (feat/fix-malformed-settings-nilpanic)
