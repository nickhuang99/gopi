<!-- doc-class: normative -->
# Second-Opinion Review: gopi config/options layer (GLM, HP node)

- Reviewer: GLM (independent second pass — peer1's `review/cli-config-options`
  report was NOT read; findings below are from a fresh read of current master).
- Branch: `review2/cli-config-options`
- Date: 2026-09-02
- Scope: the top-level `gopi [options]` layer — `--settings`/`--home`/GOPI_*
  knobs, the env↔settings↔flag catalog and precedence rule, input resolution
  (readInput + FlagOverrides), settings.json schema.

## Executive summary

The config/options layer has matured a great deal since the first review:
**all five headline findings of the peer1 review are FIXED on current master**
(verified below). The catalog-driven help and the ApplyEnv two-pass precedence
are genuinely well-engineered (env.go's truthy-only EnvBool, the SetEnvVars
gating, drift-guard tests).

However, this second pass found **two P0-class bugs the first review missed**,
both in the same family: config values that main() injects into the process
environment at runtime can never reach consumers that read them at package
`init()` time. Four advertised knobs (`--tool-timeout`, `--bash-timeout`,
`--bash-idle`, `--allow-dangerous` and their settings.json keys) are
**silently dead** on the flag/settings channels.

---

## Part 1 — Known claims from first review (verify independently)

### 1.1 `--home` settings leak (peer1 P0-1) — **FIXED on master**
- Fix commit `335eb3f` ("fix(home): --home flag now redirects the settings.json
  read (P0-1)") + `feat/fix-home-settings-isolation` merge, both ancestors of
  master.
- `cmd/agent/main.go:151-159`: the `--home PATH` value is injected into
  `GOPI_HOME` (via `os.Setenv(config.GopiDirEnv, overrides.GopiHomePath)`)
  **before** `deepseek.SettingsPath()` runs, so the settings read resolves
  through the sandbox root. Regression tests exist and pass
  (`cmd/agent/home_settings_test.go`: `TestHomeFlagRedirectsSettingsRead`,
  `TestGopiHomeEnvStillWorks`, `TestHomeEqualsFormStillRedirects`).
- Verdict: **AGREE the original bug existed; CONFIRM the fix is real and
  tested.** Sandbox isolation now covers settings/sessions/raw-logs/memory.db.

### 1.2 Env-name-as-flag never implemented (peer1 P0-2) — **FIXED on master**
- Fix commit `afac5da` ("fix(config): env-named flags work —
  --gopi-timeout-sec ≡ --timeout (P0-2)").
- `internal/config/flags.go:57-75`: FlagOverrides builds an inverse
  `envFlag` map from the catalog (via `envNameAsFlag`, one function so help
  and parser agree), canonicalizes the env-named spelling to the short flag
  **before** the scalar/bool dispatch → identical parsing + precedence by
  construction.
- Verified empirically: `gopi --gopi-timeout-sec 10` parses (no longer
  "unknown flag"); help sentence "Use the full env var name as a flag" is
  backed by a parser test (`TestHelpEnvNameSentenceMatchesParser`).
- Also correct: an env var with no catalog CLIFlag (`GOPI_COMPUTE_NODES`)
  still errors as unknown — no fake support (`TestFlagOverridesEnvNameNoFlagInCatalog`).
- Verdict: **AGREE it was missing; CONFIRM implemented + regression-tested.**

### 1.3 Thinking-budget default drift 2048 vs 1024 (peer1 P1-1) — **FIXED on master**
- Catalog Help now says "(default 1024)" matching `deepseek.DefaultClaudeThinkingBudget
  = 1024` (`internal/deepseek/claude_client.go:111`).
- Drift-guard test `TestThinkingBudgetHelpMatchesCode` lives in `cmd/agent/usage_test.go`
  (must live there because internal/config cannot import internal/deepseek —
  import cycle). `go test ./cmd/agent/ -run TestThinkingBudgetHelpMatchesCode` passes.
- Verdict: **AGREE it was stale; CONFIRM fixed with a tripwire test.**

### 1.4 GOPI_PROMPT dead channel (peer1 P1-3) — **FIXED on master**
- `cmd/agent/main.go:295-306`: readInput now reads `GOPI_PROMPT` when
  `overrides.Prompt` is empty, honoring `@payload` file-refs; precedence
  flag > env > settings preserved (ApplyEnv seeds env from the settings
  "prompt" key). Regression tests exist (`TestReadInput_GOPIPromptEnv` etc.).
- Verdict: **AGREE it was dead; CONFIRM wired + tested.**

### 1.5 hideThinking/keepThinking inverted bool (peer1 P1-4) — **FIXED on master**
- `internal/agent/loop.go:2978-2984`: both now parse via `config.EnvBool`
  (truthy-only); `GOPI_HIDE_THINKING=0/off/false` no longer hides.
- Root cause of the original inversion (ApplyEnv force-writing "off" for
  unpassed switches) is additionally handled by the SetEnvVars gate:
  `ApplyEnv(fromFlags=true)` only writes a bool when the user actually passed
  the flag (`internal/config/env.go:26-34`), and truthy-only readers make even
  an explicit "off" parse as disabled.
- Verdict: **AGREE it was inverted; CONFIRM fixed at both the writer and
  reader ends.**

### 1.6 Catalog gaps (peer1 P1-5) — **FIXED on master**
- Re-swept all `os.Getenv/os.LookupEnv` of `GOPI_*|ANTHROPIC_*|DEEPSEEK_*`
  in non-test code vs the EnvVars catalog: **every remaining uncatalogued var
  is a test-only seam** (GOPI_TEST_BIN, GOPI_SEC_ENV_PROBE, GOPI_E2E,
  GOPI_M421_*, …) — correctly excluded. Catalog now carries 67 entries, all
  sectioned.
- Verdict: **AGREE gaps existed; CONFIRM catalog is now complete for
  user-facing knobs.**

---

## Part 2 — NEW findings this second pass (not in the peer1 report)

### P0-A. `--tool-timeout` / `--bash-timeout` / `--bash-idle` flags & settings keys are DEAD
- **Files:** `internal/tools/timeout.go:48-68` — `func init()` reads
  `GOPI_TOOL_TIMEOUT_SEC`, `GOPI_BASH_TIMEOUT_SEC`, `GOPI_BASH_IDLE_SEC`
  **once at package init** into `resolvedTimeout`, then
  `FileToolTimeout()`/`BashTimeout()`/`BashIdleTimeout()` return that frozen
  snapshot (callers: `internal/tools/{read,write,edit,bash}.go`).
- **Bug:** Go package `init()` runs before `main()`. main() only injects
  flags/settings into the process env via `config.ApplyEnv(overrides, true)`
  at `cmd/agent/main.go:171-172` — AFTER tools' init already read the env.
  → `gopi --tool-timeout 5`, `--bash-timeout 9`, `--bash-idle 2`, and
  settings.json `tool_timeout_sec/bash_timeout_sec/bash_idle_sec` are
  **silently ignored** on the top-level path. Only a shell-exported
  `GOPI_TOOL_TIMEOUT_SEC` at spawn time works.
- These knobs are fully advertised (`--help`, `--help-env`, settings keys) —
  the docs promise what the code cannot deliver.
- **Root cause (git blame):** `12f73061` (2026-08-04) added the init-time
  reads; `347331a` (2026-08-24) introduced the config-centralization with
  runtime ApplyEnv injection — the two designs were never reconciled. The
  codebase ALREADY documents this exact trap elsewhere:
  `internal/deepseek/retry.go:99-106` ("package vars evaluate BEFORE main
  runs config.ApplyEnv … Reading on every call fixes both") — tools/timeout.go
  never got the same lazy-read treatment.
- **Fix suggestion:** convert the three getters to lazy reads
  (`sync.Once` or per-call `os.Getenv` guarded by EnvBool-style parsing), as
  retry.go does for GOPI_DEBUG. Add a regression test that spawns the real
  binary with `--bash-idle 1` and asserts a `sleep 60` is killed quickly.

### P0-B. `--allow-dangerous` flag & `allow_dangerous` settings key are DEAD
- **File:** `internal/tools/bash.go:91-95` — `init()` reads
  `GOPI_ALLOW_DANGEROUS == "1"` into `escapeDangerous` once; the guard at
  bash.go:238 checks that frozen value.
- **Same bug:** main()'s ApplyEnv writes GOPI_ALLOW_DANGEROUS at runtime,
  after tools' init already decided. `--allow-dangerous 1` and settings
  `"allow_dangerous": true` therefore **cannot disable the dangerous-command
  guard**; only a spawn-time shell export works. This defeats the flag's own
  advertised purpose ("Set to 1 to allow dangerous commands") and the
  settings key.
- Notably, bash.go's own comment claims "Rule 2: exactly one env read site"
  — true, but the read happens too early in the process lifecycle.
- **Fix suggestion:** make the escape hatch lazy too (read at the guard site,
  or re-resolve in a function `dangerousAllowed()` called at bash.go:238);
  add a test that `gopi --allow-dangerous 1` actually permits a guarded
  command (or at minimum a unit test that applies the env then calls the
  guard).

### P1-A. Catalog default drift: compact trigger/target Help strings stale
- Catalog (`internal/config/settings.go:94-95`) advertises:
  - `GOPI_COMPACT_TRIGGER` "(default 0.7)"
  - `GOPI_COMPACT_TARGET` "(default 0.5)"
- Code defaults (`internal/agent/context.go:33`, `compact.go:38`):
  - `DefaultCompactTriggerRatio = 0.75` (raised "~75% matches the industry
    consensus")
  - `DefaultCompactTargetRatio = 0.35` ("0.35 (was 0.5) is the safe
    waterline")
- **Both Help strings drifted from code.** The drift-guard test only covers
  the thinking budget (P1-1), not these — free-text defaults in the catalog
  are otherwise unvalidated. Suggest extending the guard pattern to every
  numeric default in Help strings, or (better) rendering defaults from the
  constants so drift is impossible.
- Empirical check: `--help-env` renders "default 0.7"/"default 0.5" while the
  agent compacting at 0.75/0.35 — a user tuning from help would mis-set.

### P1-B. `ANTHROPIC_MODEL` help claims default `deepseek-v4-pro`; code default is `deepseek-v4-flash`
- Catalog (`internal/config/settings.go:59`): "Model name (default
  deepseek-v4-pro)".
- Code (`internal/deepseek/config.go:94`): `ResolveModel` falls back to
  `"deepseek-v4-flash"` (commit `b21e630` deliberately changed the
  settings-missing fallback v4-pro → v4-flash for billing safety; the
  catalog Help string was never updated — same drift class as P1-A).
- Note the real user settings.json on this machine sets `"model":
  "deepseek-v4-flash"` explicitly, so the drift is latent in practice — but
  for a fresh install with no settings.json the printed default lies.
- Also note the drift is only in the *default* claim: `gopi --model` and
  `ANTHROPIC_MODEL`/`DEEPSEEK_MODEL` override both work correctly.

### P2-A. GOPI_PROMPT + GOPI_NO_ENFORCE_MEMORY land in the "Other" help section
- `internal/config/help.go` Sections list omits `GOPI_PROMPT` and
  `GOPI_NO_ENFORCE_MEMORY`, so `formatEnvTable` dumps them into the trailing
  "Other" bucket (help.go:119-127). Cosmetic: they ARE listed, just not under
  a themed heading (GOPI_PROMPT arguably belongs under a new "Input framing"
  section; GOPI_NO_ENFORCE_MEMORY under "Harness tuning").

### P2-B. `--raw-log` is a scalar flag whose help says `ON|OFF` — inconsistent surface
- Catalog marks `raw_log` non-bool (`isBoolKey` list omits it), so
  `--raw-log` REQUIRES a value (`--raw-log off` works, `--raw-log` alone
  errors). But the help text styles it `--raw-log ON|OFF` alongside true bool
  switches (`--hide-thinking ON|OFF`). Minor doc/parse asymmetry; the
  `rawlog.go` reader itself accepts only literal "OFF" (case-insensitive),
  so `--raw-log 0` would set env "0", which does NOT disable logging — a
  value that looks boolean but isn't honored. Suggest either making it a real
  bool knob or documenting the exact accepted strings.

### P2-C. Env-read style inconsistencies (low risk, worth a cleanup pass)
- `internal/tools/bash.go:94` uses `== "1"` while `internal/agent/loop.go`
  `boolEnvTrue` and `internal/config/env.go` `EnvBool` accept the full truthy
  set — functionally equivalent for the true case, but three bool parse
  styles exist. PROJECT.md Rule 2 says env.go EnvBool is "the SINGLE SOURCE OF
  TRUTH for boolean env parsing"; bash.go and boolEnvTrue are parallel
  implementations that can drift.

---

## Part 3 — What is genuinely good (verified this pass)

1. **Catalog-driven help**: FormatUsage/FormatEnvTable render from EnvVars
   + Sections, so the env table cannot drift from the code's knob surface.
   `TestTopLevelUsageListsEveryEnvVar`, `TestHelpEnvListsEveryVar` etc.
   enforce coverage.
2. **Two-pass ApplyEnv precedence** (flag > env > settings > default) with
   SetEnvVars gating is the right architecture; the `--model` must override
   BOTH ANTHROPIC_MODEL and DEEPSEEK_MODEL handling (flags.go) is a subtle
   correct touch.
3. **Truthy-only bool parsing** + the reasoning documented in the comments
   shows the org learned the "off means on" regression twice and fixed it at
   the root.
4. **Init-vs-ApplyEnv trap is documented** in retry.go — it just wasn't
   applied to tools/timeout.go and tools/bash.go (the P0s above).
5. settings.json ↔ env catalog ↔ struct JSON keys cross-checked: no dead
   advertised keys (only "settings_path" is a doc artifact of GOPI_SETTINGS,
   correctly `json:"-"` on the struct).

---

## Part 4 — Prioritized issue list

| # | Sev | Issue | Evidence | Status |
|---|-----|-------|----------|--------|
| P0-A | P0 | `--tool-timeout`/`--bash-timeout`/`--bash-idle` flags+settings dead (init-time env read after main's ApplyEnv) | internal/tools/timeout.go:48-68; cmd/agent/main.go:171 | NEW |
| P0-B | P0 | `--allow-dangerous` flag+settings dead (same init-time read) | internal/tools/bash.go:91-95 | NEW |
| P1-A | P1 | compact trigger/target defaults drift in help (0.7/0.5 vs code 0.75/0.35) | internal/config/settings.go:94-95; context.go:33; compact.go:38 | NEW |
| P1-B | P1 | ANTHROPIC_MODEL default claim v4-pro vs code v4-flash | settings.go:59; deepseek/config.go:94 | NEW |
| P2-A | P2 | GOPI_PROMPT/GOPI_NO_ENFORCE_MEMORY ungrouped ("Other") | help.go Sections | NEW |
| P2-B | P2 | `--raw-log` scalar-vs-bool doc/parse asymmetry; "0" not honored | flags.go; rawlog.go:41 | NEW |
| P2-C | P2 | three bool-parse implementations (EnvBool / boolEnvTrue / == "1") | env.go; loop.go:1656; bash.go:94 | NEW |
| (peer1 P0-1) | — | --home settings leak | fixed 335eb3f + tests | FIXED |
| (peer1 P0-2) | — | env-name-as-flag unknown | fixed afac5da + tests | FIXED |
| (peer1 P1-1) | — | thinking-budget default drift | fixed + guard test | FIXED |
| (peer1 P1-3) | — | GOPI_PROMPT dead channel | fixed + tests | FIXED |
| (peer1 P1-4) | — | hideThinking inverted bool | fixed (EnvBool + SetEnvVars gate) | FIXED |
| (peer1 P1-5) | — | catalog gaps | fixed (67 entries, verified complete) | FIXED |

## Verification
- Build: `go vet ./internal/config/` clean; `go build ./...` OK (main repo's bin/gopi present).
- Tests: `go test ./internal/config/` PASS (cached); `go test ./internal/tools/` PASS.
  `go test ./cmd/agent/` has one failing test: `TestTaskRunSurvivesClosedStdoutPipe`
  (cli_tasks_test.go:450) — a real-binary e2e that needs `bin/gopi` in the
  worktree; a fresh review worktree has no bin/ (skips when run alone, fails in
  the full suite). A review-worktree environment artifact, NOT a config/options
  defect. Config-specific cmd/agent tests (TestThinkingBudgetHelpMatchesCode,
  TestReadInput_GOPIPromptEnv, home_settings) pass in isolation.
- Commit: see final message (worktree branch review2/cli-config-options)
