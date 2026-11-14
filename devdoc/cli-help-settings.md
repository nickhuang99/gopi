<!-- doc-class: normative -->
# CLI Help & Unified Configuration (settings.json + env vars + CLI flags)

## Problem

GoPi previously had no coherent way to discover or set its many knobs:

- `gopi -h` listed only ~13 of the ~35 env vars the code actually reads, did
  **not** advertise the newly-integrated `gopi log` subcommand, and gave no
  help for the configuration file.
- `settings.json` supported only three keys (`api_key`, `base_url`, `model`).
  Every other tuning knob (`GOPI_THINKING`, `GOPI_TIMEOUT_SEC`,
  `GOPI_COMPACT_*`, tool timeouts, `GOPI_MEMORY_DB`, ...) could only be set
  via environment variables.
- There were no CLI flags for any of these knobs — only `--settings`.

## What was built

### 1. `internal/config` package — single source of truth for the knob surface

- `EnvVars []envKey` catalog: every tunable env var with its settings.json
  **key** and **CLI flag** and a one-line help. ~35 entries across six headings
  (endpoint, thinking & reasoning, agent runtime, memory compaction, logging,
  tools). The top-level help and `--help-env` render *from this catalog*, so
  the docs can never drift from the knobs the code honours.
- `Config` struct — type-checked view of every knob. JSON keys mirror the env
  var names in snake_case (`timeout`, `thinking`, `compact_keep_recent`,
  `memory_db`, `raw_log`, ...).
- `FromSettingsFile(path)` — loads settings.json into `Config`; missing file is
  not an error; unknown keys are silently ignored (backward compatible).
- `FlagOverrides(args)` — parses `--name value` / `--name=value` for every
  catalogued knob (plus `--settings`). Boolean switches accept `--flag`,
  `--flag=true`, or a following bare bool token (`--compact off`). Unknown or
  value-mismatched flags error (typos are caught, not silently ignored).
- `ApplyEnv(cfg, fromFlags)` — pushes a Config into the process environment so
  the existing per-file `os.Getenv` parses (Rule 2 single-source-of-truth) pick
  it up unchanged. Two-pass call in `main()` preserves precedence.

### 2. Precedence: flag > env var > settings.json > default

`main()` resolves config as:

```go
overrides, _ := config.FlagOverrides(os.Args[1:])   // CLI flags (highest)
base,    _ := config.FromSettingsFile(settingsPath)  // settings.json (lowest file source)
config.ApplyEnv(base, false)   // fill env vars NOT already set  (settings < env)
config.ApplyEnv(overrides, true) // flags ALWAYS win over env      (flag > env)
```

- Settings only fill env vars that are unset (or empty), so a developer's
  exported env var still wins.
- Flags always override — even a previously-exported env var (needed so
  `--timeout 0` = unlimited beats an ambient `GOPI_TIMEOUT_SEC`).
- Endpoint settings are mirrored onto both `ANTHROPIC_*` and `DEEPSEEK_*`
  fallbacks so a settings-only key reaches the reader's resolution chain.

### 3. Full `gopi -h` and `gopi --help-env`

The top-level help now shows all usage forms, the grouped env-var/`settings.json`
key/CLI-flag table, every subcommand (including `gopi log`), and a set of
use-case examples. `gopi --help-env` prints the complete table alone.
The integrated `gopi log` subcommand's usage is advertised at the top level
(regression-guarded by `usage_test.go`).

### 4. Tests

- `internal/config/config_test.go`: flag parsing (valid/unknown/missing-value/
  invalid-int), settings loading (full, missing-is-ok), and precedence
  (`settings-no-clobber-env`, `settings-fills-unset`, `flags-override-env`,
  `timeout-0-unlimited`, `settings-mirrors-deepseek-fallbacks`), plus help
  completeness (`FormatEnvTable` lists every env var/key/flag).
- `cmd/agent/usage_test.go` extended: top-level usage lists every subcommand
  incl. `gopi log`, every env var, and the settings-keys/flags/precedence
  wording.

## Not done

- No generated `--help-env-man`/JSON schema file; the settings.json format is
  documented only in the help text and this devdoc (acceptable for now).
- `formatEnvTable` renders the env var + settings key + CLI flag in one
  aligned column dump; a future `gopi init --settings` might scaffold a
  commented template.

## Verification

- Build: `go build ./...` OK.
- Tests: `go test ./...` — all packages pass (cmd/agent, internal/config,
  internal/agent, internal/tools, internal/deepseek, internal/gopilog).
- Manual: `/tmp/gopi -h` and `/tmp/gopi --help-env` render correctly;
  `gopi log -h`, `gopi memory_search -h`, `gopi web_search -h` still dispatch
  to their subcommand usage.
- Settings round-trip (regression): `timeout:300` in settings.json now yields
  `GOPI_TIMEOUT_SEC=300` (was broken — `time.Duration` JSON unmarshal treated
  `300` as 300ns). Fields are stored as integer seconds in `Config`.
