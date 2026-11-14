<!-- doc-class: normative -->
# Second-Opinion Review: gopi config/options layer (GLM, HP node)

- **Reviewer:** GoPi/GLM, HP node, 2026-09-03 — independent re-review of the
  `gopi [options]` config layer. This report was written WITHOUT reading the
  peer1 report during analysis; the peer1 report (`devdoc/review-cli-config-options.md`,
  branch `review/cli-config-options`) was opened only AFTER my own probing was
  complete, to map my findings against its claims. This merge-commit version
  also incorporates one factual correction found at cross-check time (§5).
- **Base reviewed:** master `f88cefc` (checked `git log --oneline -3` first;
  also contains the P1-batch fixes `86a955e`, `335eb3f`, `4b08400`, `afac5da`).
- **Method:** `--help`/`--help-env` capture from a freshly built binary →
  source read (`cmd/agent/main.go` 510 lines, `internal/config/*` — settings/
  env/flags/config/help/gopihome, `internal/deepseek/config.go`) → per-knob
  triple verification → live sandbox probes (6 full agent runs in
  `$GOPI_HOME` sandboxes) → git history cross-check.

---

## Part 1 — Known claims from first review, verified independently

| peer1 claim | My independent verdict | Evidence |
|---|---|---|
| P0-1 `--home` settings leak | **CONFIRMED FIXED on master.** `cmd/agent/main.go:171-173` sets `GOPI_HOME` via `os.Setenv(config.GopiDirEnv, …)` BEFORE `deepseek.SettingsPath()` runs → the settings read resolves under the sandbox root. Live probe: `--home <sandbox>` with sandbox settings `{model: homesettings-model, max_tokens: 777, api_key: dummy}` picked up BOTH keys (session_start recorded `homesettings-model`; the raw-log REQUEST carries `"max_tokens":777`). | `cmd/agent/main.go:166-183`; probe T13/T15 |
| P0-2 env-name-as-flag never implemented | **CONFIRMED FIXED.** `FlagOverrides` builds the inverse `envFlag` map from the catalog; `--gopi-timeout-sec` canonicalizes to `--timeout` before dispatch (`internal/config/flags.go:57-75`, `envNameAsFlag` at 100-103). Parser and help share one spelling function. | `flags.go:41,57-61` |
| P1-1 thinking-budget default drift 2048 vs 1024 | **CONFIRMED FIXED.** Catalog help now says "(default 1024)" matching `DefaultClaudeThinkingBudget = 1024` (`claude_client.go:111`); drift-guard test lives in `cmd/agent` (import-cycle avoidance) per commit `86a955e`. | `settings.go:82`; `86a955e` message |
| P1-3 GOPI_PROMPT dead channel | **CONFIRMED FIXED.** `readInput` falls back to `os.Getenv("GOPI_PROMPT")` with the same `@payload` handling; precedence `--prompt > GOPI_PROMPT > positional/stdin` as documented. | `cmd/agent/main.go:295-313` |
| P1-4 hideThinking inverted bool | **CONFIRMED FIXED at BOTH ends.** Writer: `ApplyEnv(fromFlags=true)` writes only `SetEnvVars` keys the user actually passed (`env.go:26-34`); reader: `internal/agent/loop.go:2978/2984` use `config.EnvBool` truthy-only parsing, so even a written `"off"` parses as disabled. | `env.go`, `internal/agent/loop.go:2978-2060` |
| (also: keep-thinking "unhandled flag") | **CONFIRMED FIXED.** `applyFlag` has `case "keep-thinking"` and `isBoolKey` contains `keep_thinking`; live `gopi --keep-thinking` / `--keep-thinking=false` both parse. Fix commit `4b08400`. | `flags.go:142-146`; probe T2/T3 |

**Summary: I AGREE with the peer1 review's headline verdicts — all five were
real bugs and all five are genuinely fixed and regression-tested on master.**

---

## Part 2 — NEW findings this pass

### P0-A. `--bash-timeout` / `--bash-idle` / `--tool-timeout` flags AND their settings keys are DEAD (init-time env read vs main()'s ApplyEnv)

- `internal/tools/timeout.go:42-69`: package `init()` reads
  `GOPI_TOOL_TIMEOUT_SEC` / `GOPI_BASH_TIMEOUT_SEC` / `GOPI_BASH_IDLE_SEC`
  ONCE into `resolvedTimeout`, frozen forever. Go runs package init BEFORE
  `main()`, and `main()` injects flags/settings into the env only at
  `cmd/agent/main.go:184-185` (`ApplyEnv` ×2). Therefore every value gopi
  injects at runtime arrives too late — only a shell-exported variable at
  spawn time works.
- **Live proof (sandbox, isolated env):**
  - settings `bash_idle_sec: 1` (T17b): bash `sleep 60` ran past 40s and was
    never idle-killed at 1s (default 120s would apply).
  - flag `--bash-idle 1` (T17e): still alive at 30s → flag channel dead too.
  - control (T17c): spawn-time `GOPI_BASH_IDLE_SEC=1` killed `sleep 60`
    immediately ("killed after … no output for 1s" in the transcript) → the
    mechanism works when the env var is real at init.
- **Same class as the trap gopi already documented elsewhere:**
  `internal/deepseek/retry.go:99-106` says package vars evaluate BEFORE main
  runs `config.ApplyEnv`, and reads GOPI_DEBUG lazily for that reason —
  `tools/timeout.go` never got the same treatment. Root cause: `12f7306`
  (2026-08-04) added init-time reads; `347331a` (2026-08-25) centralized
  config with runtime ApplyEnv — the two designs were never reconciled.
- **Impact:** all three knobs are advertised in `--help`, `--help-env`, and
  settings schema; a user tuning them via flag/settings silently gets the
  default. This is exactly the "documented knob that is not honored" drift
  class the epic is hunting.
- **Fix (minimal):** make the three getters lazy (per-call `os.Getenv` or a
  `sync.Once` re-resolution invoked on first use), mirroring retry.go. Add a
  regression test: spawn `bin/gopi --bash-idle 1` and assert a silent
  `sleep 60` is killed in ≪60s.

### P0-B. `--allow-dangerous` flag and `allow_dangerous` settings key are DEAD (same root)

- `internal/tools/bash.go:91-96`: `init()` sets `escapeDangerous` from
  `os.Getenv("GOPI_ALLOW_DANGEROUS") == "1"` once; the guard at `bash.go:238`
  checks that frozen value.
- **Live proof:** sandbox settings `allow_dangerous: true` (T19) → bash tool
  still rejects: `dangerous command rejected: pattern "sudo "`; flag
  `--allow-dangerous` (T19b) → same rejection. Control (T19c): spawn-time
  `GOPI_ALLOW_DANGEROUS=1` → the SAME bash call executes (`exit=0`, output
  `snap`). So the flag/settings channels are dead; only spawn-env works.
- **Impact:** the security escape hatch documented in the tool description
  ("blocked unless GOPI_ALLOW_DANGEROUS=1"), in `--help-env` ("Set to 1 to
  allow dangerous commands"), and in settings.json cannot be enabled by the
  documented channels. A user who trusts the flag runs commands they expect
  to pass and gets rejections; worse, an operator auditing "how was
  allow-dangerous enabled" cannot answer from the process env.
- **Fix:** read the variable at the guard site (lazy, like P0-A), or export a
  `config.DangerousAllowed()` helper as the single read point. Test:
  `gopi --allow-dangerous 1` must execute a pattern-guarded command.

### P1-C. `settings.json` `"compact"` is typed bool-only — the env/flag channel accepts strings, the settings channel rejects the documented value

- Catalog help (`settings.go:90`): `GOPI_COMPACT` = `Memory compaction:
  "off" disables (default on)`. The env reader (`internal/agent/loop.go:2256-2109`) accepts
  `off/0/false` (case-insensitive) and the flag is a bool
  (`--compact off` works). But `Config.Compact` is `*bool`
  (`config.go:179`) — a settings file with `"compact": "off"` fails the
  WHOLE settings parse: `config: failed to parse … settings.json: json:
  cannot unmarshal string into Go struct field Config.compact of type bool`.
- **Live proof:** sandbox with `"compact": "off"` → hard parse error, then a
  nil-config PANIC in the same run (see P1-D) — the run dies at startup.
- **Impact:** the documented value `off` works in env/flag form and fails in
  settings form — a triple inconsistency inside one knob. Either accept the
  same normalizedBoolValue spellings in settings (custom UnmarshalJSON, like
  `ThinkingMode` already does) or document `"compact": false` only.
- **Fix:** give the bool knobs used with string values a `UnmarshalJSON`
  that reuses `normalizedBoolValue` (the ThinkingMode pattern already in the
  package).

### P1-D. A malformed settings.json can nil-panic in ApplyEnv — error path is not hardened

- `cmd/agent/main.go:181-186`: on parse error it prints
  `[gopi] config: <err>` but `base` is `nil`, then calls
  `config.ApplyEnv(base, false)` immediately. `ApplyEnv` dereferences
  `cfg.SetEnvVars`/fields without a nil check →
  `panic: runtime error: invalid memory address or nil pointer dereference`
  at `internal/config/env.go:117` (reproduced live with the `"compact":
  "off"` file above).
- **Impact:** a user typo in settings.json turns a warning into a crash with
  a raw Go panic trace. Two-layer fix: (1) `FromSettingsFile` should return
  a non-nil empty Config with the error (it already does that for
  missing-file; make the parse-error path do the same), and/or (2) `cmd/agent/main.go`
  must not call `ApplyEnv(base, …)` when `sErr != nil && base == nil`.
- This is a real crash-on-bad-input bug — P1 (not P0) only because the
  trigger is a malformed user file.

### P1-E. Catalog default-drift audit (same class as peer1 P1-1, beyond the thinking budget)

Free-text "(default …)" strings in the catalog vs code constants:

| Catalog help (`internal/config/settings.go`) | Says | Code default | Verdict |
|---|---|---|---|
| `GOPI_COMPACT_TRIGGER` :95 | 0.7 | `DefaultCompactTriggerRatio = 0.75` (`internal/agent/context.go:33`) | **DRIFT** |
| `GOPI_COMPACT_TARGET` :96 | 0.5 | `DefaultCompactTargetRatio = 0.35` (`internal/agent/compact.go:38`) | **DRIFT** |
| `ANTHROPIC_MODEL` :59 | v4-pro | `ResolveModel` → `deepseek-v4-flash` (`internal/deepseek/config.go:94`, commit `b21e630`) | **DRIFT** |
| `GOPI_MAX_TOKENS` :83 | 16384 | `DefaultClaudeMaxTokens = 16384` (`claude_client.go:94`) | ok |
| `GOPI_HEARTBEAT_SEC` / `STALE` :88-89 | 30s / 3 | `internal/agent/heartbeat.go` 的两处常量 30s/3（**旋钮与文件已于 2026-09-29 移除** — 配置的死代码，`devdoc/heartbeat-knobs-removed.md`） | ok |
| `GOPI_NUDGE_ROUNDS`/`TOOLS` :122-123 | 5 / 3 | `internal/agent/loop.go:93-94` 5/3 | ok |
| `GOPI_COMPACT_COOLDOWN` :99 | 600 | `DefaultCompactCooldown = 600s` | ok |

The drift-guard test pattern (budget) exists but covers one knob. Suggest
extending it to every numeric default in Help strings (table-driven test in
`cmd/agent`, where the deepseek import is allowed), or rendering the numeric
defaults from constants at help-build time so drift is structurally
impossible.

### P2-A. Help `Options` block claims `--raw-log ON|OFF`, but `raw_log` is a non-bool scalar

`isBoolKey` omits `raw_log` → `--raw-log` requires a value; `--raw-log 0`
sets env `GOPI_RAW_LOG=0`, and the reader (`internal/agent/rawlog.go:41`)
only disables on literal `OFF` (case-insensitive) → `--raw-log 0` leaves raw
logging ON while looking boolean. Either add `raw_log`-style normalization
at the reader or document the exact strings. (`rawlog.go`'s read is the
single source; the value alphabet is the drift.)

### P2-B. Duplicate env-var names pointing at one key make `--model`-style SetEnvVars special-cases necessary

`ANTHROPIC_MODEL` and `DEEPSEEK_MODEL` both map to key `model` (same for
api-key/base-url pairs). `FlagOverrides` hard-codes the "mark BOTH env keys
as flag-set" special case (`flags.go:131-141`) — correct but open to drift if
a future pair is added (the comment admits the map only keeps the LAST
catalog entry per flag). Suggest deriving the fallback-pair from the catalog
(group by Key) instead of hard-coding three switch arms.

### P2-C. Bool-parse styles: `EnvBool` is declared the single source, but a full truthy set and a bare `== "1"` coexist

`config.EnvBool` (env.go:11-19) vs `internal/agent/loop.go`'s boolEnvTrue-style truthy list
vs `bash.go:94` `== "1"` (moot after P0-B fix lands, but pick one parse).
PROJECT.md Rule 2 calls EnvBool THE single source of truth for boolean env
parsing — the remaining parallel implementations should delegate to it.

### P2-D. `--help` Options block is hand-written prose next to the catalog table (residual drift surface)

The env table IS catalog-rendered, but the `Options (each overrides …)`
list is a separate hand-maintained literal in `FormatUsage`
(`internal/config/help.go:148-190`): flag names, defaults, and the `--timeout 0 = unlimited`
claim must be kept in sync manually (e.g. it already omitted
`--heartbeat-sec/--heartbeat-stale` until added; `--allow-dangerous` has no
Options line at all). Low priority since the table below it is
authoritative, but it duplicates the surface the catalog was built to
eliminate.

---

## Part 3 — What is genuinely good (verified)

1. **Catalog-driven help**: `--help-env` and the env section of `-h` render
   from `EnvVars`+`Sections`; coverage tests (`TestTopLevelUsageListsEveryEnvVar`,
   `TestHelpEnvListsEveryVar`) enforce completeness. The 24-var catalog gap
   from the first review is closed.
2. **Two-pass ApplyEnv** with `SetEnvVars` gating gives true
   flag > env > settings > default precedence without refactoring every
   reader — and the `--model`/`--api-key`/`--base-url` dual-pair marking
   handles the Anthropic_*/Deepseek_* fallback ordering correctly.
3. **Truthy-only bool parsing** (`EnvBool`) with the regression history
   documented in comments — the "off means on" class is dead for the
   readers that use it.
4. **The init-vs-ApplyEnv trap is understood in-tree** (retry.go comment) —
   the P0s above are the same bug in files the sweep missed, not a novel
   design flaw.
5. `@payload` / `@-` / `\@` input resolution: stat-first classification,
   hard errors for missing payload files, escape hatch, stdin-override —
   all match the help text verbatim (probed).

## Part 4 — Prioritized issue list

| # | Sev | Issue | Evidence |
|---|-----|-------|----------|
| P0-A | **P0** | `--bash-timeout`/`--bash-idle`/`--tool-timeout` flags+settings dead (init-time env read before main's ApplyEnv) | `internal/tools/timeout.go:42-69`; probes T17b/T17e/T17c |
| P0-B | **P0** | `--allow-dangerous` flag+settings dead (same root) | `internal/tools/bash.go:91-96,238`; probes T19/T19b/T19c |
| P1-C | P1 | settings `"compact": "off"` (documented spelling) hard-fails settings parse | `config.go:179` `*bool`; probe T19-pre |
| P1-D | P1 | malformed settings.json → nil-config PANIC in ApplyEnv (env.go:117) | `cmd/agent/main.go:181-186`; live repro |
| P1-E | P1 | catalog default drift: compact trigger 0.7→0.75, target 0.5→0.35, model v4-pro→v4-flash | `settings.go:59,95-96` vs `context.go:33`, `compact.go:38`, `deepseek/config.go:94` |
| P2-A | P2 | `--raw-log` scalar vs bool-looking docs; `0` not honored | `flags.go` isBoolKey; `rawlog.go:41` |
| P2-B | P2 | fallback-pair SetEnvVars marking is hard-coded, not catalog-derived | `flags.go:131-141` |
| P2-C | P2 | three bool-parse styles vs EnvBool "single source" claim | `env.go`, `internal/agent/loop.go`, `bash.go:94` |
| P2-D | P2 | hand-written Options block duplicates catalog surface (drift-prone) | `internal/config/help.go:148-190` |

## Part 5 — Cross-check note (post-probe)

After my probes were complete I opened the peer1 report to map claims. Its
five headline findings all match my independent verification (Part 1). One
disagreement in emphasis: the peer1 report's P1-2 ("--settings not
GOPI_HOME-aware for the flag form") overstates the residual issue — with
`--home` fixed (main.go:171-173) the default settings path resolves through
the sandbox root correctly, and an explicit `--settings PATH` is by
definition absolute; the interplay bug it describes no longer exists at
f88cefc. The peer1 report also missed the P0-A/P0-B init-time dead-knob
family (the only P0s still open on master as of this review).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK (worktree
  and main checkout).
- Tests: `go build ./...` OK. No code changed by this review (docs only).
- Live probes: 6 sandboxed agent runs (`--home` isolation, api_key/model/
  max_tokens settings pickup, bash-idle ×3 channels, allow-dangerous ×3
  channels, malformed-settings panic) — outputs quoted above.
- Commits: see final message (branch `review2/cli-config-options`).
