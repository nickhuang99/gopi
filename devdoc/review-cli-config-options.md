<!-- doc-class: normative -->
# Task Report: Config/Options Layer Code Review

**Date:** 2026-09-02
**Epic:** CLI code-review epic — child task (config/options layer)
**Branch (deliverable):** `review/cli-config-options`
**Reviewer method:** `--help` + `--help-env` capture → source read (cmd/agent/main.go, internal/config/*, deepseek.SettingsPath) → per-knob verification → docs (GOPI.md, devdoc) → git history/blame → empirical CLI probes.

---

## 0. Scope Covered

`gopi [options]` layer as shown by `./bin/gopi --help` (278 lines) and `./bin/gopi --help-env` (101 lines): data-root redirection (--settings/--home/GOPI_SETTINGS/GOPI_HOME), the env↔settings.json↔CLI-flag catalog (FormatUsage/FormatEnvTable), precedence rule, all flag knobs, and input resolution (@payload / @- / \@ escape / image positional / readInput + FlagOverrides).

---

## 1. Architecture Assessment (what works)

### 1.1 Catalog-driven help — the drift-resistant core (GOOD)

`internal/config/settings.go` `EnvVars` is the single catalog: each entry carries `{Env, Key(settings.json), CLIFlag, Help}`. `FormatEnvTable` (--help-env) and the env-table section of `FormatUsage` (--help) render **directly from this catalog** — verified line-by-line: every env var in the code's readers appears in both help outputs, and `TestTopLevelUsageListsEveryEnvVar` guards it. The `Sections` grouping with "Other" fallback is clean. This is the correct design and largely fulfills its "docs can't drift" promise — **for the env table**.

### 1.2 Precedence implementation (GOOD)

`cmd/agent/main.go` two-pass `ApplyEnv`: `ApplyEnv(settingsCfg, false)` (fill unset env only, settings < env) then `ApplyEnv(flagCfg, true)` (flags always win). `SetEnvVars` map ensures only flags the user actually passed are force-written (fix 62dbb27: unpassed `--debug` no longer floods stderr with `GOPI_DEBUG=off`). `--model`/`--api-key`/`--base-url` correctly force-both Anthropic_* and DeepSeek_* fallback pairs (ResolveModel checks ANTHROPIC_* first — a shell-exported ANTHROPIC_MODEL must not beat the flag). Duration knobs stored as int seconds (Config `TimeoutSec`), avoiding the JSON-duration-nanoseconds trap.

### 1.3 Input resolution (GOOD)

`readInput` classification (literal prompt / text file / image file) is flag-value-aware — a `--model deepseek-v4-flash-vision-exp` value can never leak into positionals. `@payload` / `@-` stdin / `\@` escape / `@@` double-escape all implemented with clear error paths (missing file = hard error, never silent prompt text). Native image positional detection (stat first — a prompt mentioning "a.png" is not treated as an image) is correct. `ExpandFileRef`'s 8 MiB cap is a named constant (Rule 1).

### 1.4 Effective boolean normalization (GOOD)

`EnvBool` truthy-only parsing is a **documented, tested regression fix** (agent debug dump 08-29, deepseek retry logging 08-30): `GOPI_DEBUG=off` must never enable debug. `normalizedBoolValue` for flags accepts `1/true/on/yes/enabled` and `0/false/off/no/disabled`. Flags parse `--flag`, `--flag=val`, and a following bare bool token (`--debug off`).

---

## 2. Per-Item Findings (issues)

### P0-1: `--home` flag does NOT redirect the settings.json read — data-root isolation is incomplete

**Files:** `cmd/agent/main.go:83-99`, `internal/deepseek/config.go:29-57`, `internal/config/gopihome.go`

**Mechanism:** settings path resolution happens BEFORE `ApplyEnv` injects `GOPI_HOME`:
```go
settingsPath := overrides.SettingsPath          // --settings flag only
if settingsPath == "" {
    settingsPath = deepseek.SettingsPath()      // ← GOPI_SETTINGS env + GopiDir()
}
base, sErr := config.FromSettingsFile(settingsPath)
config.ApplyEnv(base, false)                    // GOPI_HOME injected HERE
config.ApplyEnv(overrides, true)
```
`deepseek.SettingsPath()` resolves the default via `config.GopiDir()`, which reads `GOPI_HOME` **from the process env** — but the `--home` flag value only lands in the env *after* this call. `--home X` therefore reads `~/.gopi/settings.json` (or whatever GOPI_SETTINGS says) instead of `X/.gopi/settings.json`.

**Impact:** the flagship sandbox-isolation knob (`--home`, help text: "redirects sessions, raw logs, memory.db and settings away from the profile") silently leaks the REAL profile's settings into the sandbox — including `api_key`, `model`, `compute_nodes`. Sessions/raw-logs/memory.db are redirected (they resolve via GopiDir() at use time), but settings are not. The help text explicitly claims settings redirection. Empirical probe confirmed: `--home <sandbox>` with a marker `{"model":"SANDBOX-MODEL-MARKER"}` in the sandbox settings does not pick up the marker (the env-var `GOPI_HOME=...` form does).

**Fix:** resolve the settings path AFTER applying GOPI_HOME, or make `cmd/agent/main.go` inject the flag's GopiHomePath into the env (or into `deepseek.SettingsPath()`'s resolution) before the settings read. One-line reorder in main.go + a regression test.

**Severity:** P0 — the primary isolation knob is documented as doing something it does not do; a sandboxed run (test suite, CI, A/B) silently inherits production credentials and topology.

### P0-2: Help advertises "Use the full env var name as a flag" — the feature does not exist

**Files:** `internal/config/help.go:179`, `internal/config/flags.go:20-92`
<!-- docanchor drift permitted 2026-09-27 (task 01a0e2ea91f37e9185e4f99317484247):
     help.go's constant block at :179 has moved; this dated review row is a
     landed record of the review it describes, so its citation is listed rather
     than rewritten — repair = re-key by symbol. -->

**Evidence:** help text: "Use the full env var name as a flag for any knob (e.g. `--gopi-timeout-sec`)." `FlagOverrides` builds its accepted-flag set **only** from `e.CLIFlag` in the catalog — there is no code path that accepts `--gopi-timeout-sec`, `--gopi-max-tokens`, etc. Empirical probe:
```
$ echo t | ./bin/gopi --gopi-timeout-sec 10
gopi: unknown flag --gopi-timeout-sec
```
The advertised escape hatch for knobs without a short flag (e.g. `--compact-trigger` exists, but `--gopi-compact-max-summary-tokens`-style would be needed for the long ones) simply errors. **A user following the help text is told the flag is a typo.**

**Fix:** either (a) implement it — in `FlagOverrides`, also accept `--`+env-name when the env name is in `flagEnv`-inverse (map env→flag), mapping `--gopi-timeout-sec` to the same applyFlag case; or (b) delete the sentence from help. (a) is the documented promise; (b) is the honest minimum. Note: this claim shipped in 347331a (the original config-centralization commit) and has never worked.

**Severity:** P0 for doc-truth (documented feature that errors), P2 if fixed by deleting the sentence.

### P1-1: GOPI_THINKING_BUDGET default drift — catalog says 2048, code says 1024

**Files:** `internal/config/settings.go:79` ("default 2048"), `internal/deepseek/claude_client.go:111` (`DefaultClaudeThinkingBudget = 1024`), `internal/agent/loop.go:2108-1966`

**Evidence:** commit d3ef449 "feat(perf): halve the thinking budget (2048 → 1024)" changed the code default but **never touched the EnvVars catalog help string**. `--help` and `--help-env` both print "Thinking budget in tokens per round (default 2048)". This is exactly the drift the catalog design was meant to prevent — but the *defaults* live inside the free-text `Help` strings, which nothing validates against the code constants.

**Fix:** a test asserting every `(default NNN)` in `EnvVars` help matches its code constant would have caught this. Suggest `TestHelpDefaultsMatchCode`.

### P1-2: `--settings` path is ALSO not GOPI_HOME-aware for the flag form — same root as P0-1, worse interplay

**Files:** `cmd/agent/main.go:83-99`

`--settings PATH` is honored (it IS the settings path), but the ordering bug in P0-1 means a user passing BOTH `--home X --settings` (or just `--home X`) gets settings resolved against the wrong root. Also: after resolution, `os.Setenv("GOPI_SETTINGS", settingsPath)` is written — which is correct for downstream readers but happens after the `ApplyEnv` passes; a settings.json `gopi_home` key would be injected into env by ApplyEnv but already consumed the wrong default path. Fold into P0-1's fix.

### P1-3: GOPI_PROMPT env var is catalogued, documented, injected — but never READ

**Files:** `internal/config/settings.go:65` (catalog), `internal/config/env.go:133` (injection), `cmd/agent/main.go:191-262` (readInput), `usage_test.go:70`

**Evidence:** `readInput` reads `overrides.Prompt` (from `--prompt` flag only) and never `os.Getenv("GOPI_PROMPT")`. `ApplyEnv` writes `GOPI_PROMPT` from the settings `prompt` key, so the *settings→env→reader* chain for this key is: settings `prompt` → GOPI_PROMPT env → **nothing reads it**. The three-channel promise ("flag > env > settings.json") is broken for this knob: the env channel is dead and the settings channel only works because ApplyEnv feeds... nothing. The help and usage tests advertise GOPI_PROMPT. The image-input devdoc (devdoc/image-input-report.md:51) explicitly claims "GOPI_PROMPT env and settings `prompt` key" work.

**Empirical:** `GOPI_PROMPT="..." gopi` — the env var is ignored (prompt comes from positional/stdin).

**Fix:** in `readInput`, fall back to `os.Getenv("GOPI_PROMPT")` when no positional and no `--prompt` (mirroring the documented precedence), or remove GOPI_PROMPT from the catalog/help if deliberately env-only-disabled. The precedence contract says flag > env > settings — currently env is silently skipped.

### P1-4: `hideThinking`/`keepThinking` use `!= ""` — inverted vs the truthy-only convention and the "Set to 1" doc

**Files:** `internal/agent/loop.go:3223-3214`

```go
hideThinking := os.Getenv("GOPI_HIDE_THINKING") != ""
keepThinking := os.Getenv("GOPI_KEEP_THINKING") != ""
```
Catalog/help say "Set to 1 to hide live reasoning display" / "Set to 1 to replay...". `!= ""` treats **any non-empty value as true** — so `GOPI_HIDE_THINKING=0`, `=off`, `=false` all ENABLE hiding. This is the exact regression class `EnvBool` was built to prevent (env.go:10-27 documents the "seen twice" history), and it's inconsistent: `boolEnvTrue` (GOPI_DEBUG, GOPI_NO_ENFORCE_MEMORY) and `EnvBool` both parse truthy-only, but these two readers predate the fix and were left on `!= ""`.

Note the subtle asymmetry: `ApplyEnv` emits `GOPI_HIDE_THINKING=off` when `--hide-thinking off` is passed (setBool false branch) — and the reader then treats `"off"` as TRUE. So `--hide-thinking off` **enables** hiding. Tested logic confirmed (loop_test only tests `=1` and unset; no `=0`/`=off` case).

**Fix:** `hideThinking := config.EnvBool("GOPI_HIDE_THINKING")` and same for KEEP_THINKING. Add `=0`/`=off` regression tests. The comment "Set to 1" in the catalog is then accurate.

### P1-5: EnvVars catalog omits documented-in-code env vars — help surface incomplete

**Files:** `internal/config/settings.go` EnvVars, cross-checked against every `os.Getenv` reader.

> **Correction (2026-11-05).** This finding was itself wrong about three of the
> names below. `GOPI_SYNC_INTERVAL`, `GOPI_SYNC_GIT_INTERVAL` and
> `GOPI_SYNC_FULL_EVERY` were **never read by code** — they were listed here as
> "missing from the catalog" on the assumption that an advertised variable has a
> reader, and the assumption held for the rest of the list but not for these
> three (Rule 21's defect shape in the opposite direction: a knob with zero
> production readers). Their catalog rows survived until 2026-11-05 precisely
> because this page vouched for them. They are now removed, together with
> `GOPI_TASK_SYNC_FULL_TIMEOUT_SEC` and the `SyncFullReconcile` mechanism the
> last one paced — see `CONFIG.md`'s removal notes and the anti-re-advertisement
> gate `internal/config/sync_knob_removal_gate_test.go`. **Do not re-list them.**

**Missing from the catalog/help (all read by code, some documented in GOPI.md/devdocs):**
- `GOPI_SYNC_PEERS`, `GOPI_SYNC_NODE`, `GOPI_ADOPTION_INTERVAL`, `GOPI_MERGE_AUDIT_INTERVAL`, `GOPI_TASK_NOTIFY_*`, `GOPI_WORKER_MAX_ROUNDS`, `GOPI_NODE_ROLE`, `GOPI_SYNC_TO` (already flagged in devdoc/cli-command-audit.md §3.1 as EnvVars-catalog gaps)
- ~~`GOPI_SYNC_INTERVAL`, `GOPI_SYNC_FULL_EVERY`, `GOPI_SYNC_GIT_INTERVAL`~~ — **struck: zero readers, removed 2026-11-05** (see the correction above)
- `GOPI_EFFORT_VALUES` (injected by ApplyEnv, read by loop.go; undocumented in help — a settings-only channel surfaced as env)
- `GOPI_NUDGE_ROUNDS`, `GOPI_NUDGE_TOOLS` (loop.go:2140-2004 — env-overridable for tests, undocumented)
- `GOPI_HIPPO_DB` (legacy alias read by memoryDBPath, memory.go:52 — intentionally legacy, fine to keep undocumented but should be commented as such)
- `GOPI_BASH_LOCALE` (bash.go:77)
- `GOPI_SHOT_CHROME` (advertised in `gopi shot` help but not the env table)
- ~~`GOPI_ANCHOR_GUARD`, `GOPI_SYNC_FULL_EVERY` etc. (memsync internals)~~ — `GOPI_ANCHOR_GUARD` is live and advertised (`internal/tools/memory_sync_reconcile.go`); `GOPI_SYNC_FULL_EVERY` had **zero production readers** and was removed 2026-11-05 with the `SyncFullReconcile` mechanism (see the correction above)

The design intent (help.go comment: "help can never drift from what the code actually honours") is violated in the *other* direction: knobs honored but not advertised. `TestTopLevelUsageListsEveryEnvVar` only checks catalog→help, never code→catalog.

### P2-1: FormatUsage "Options" block is hand-written, NOT catalog-driven — the drift that P0-2/P1-1 demonstrate

`FormatUsage`'s `Options (each overrides...)` section (help.go:139-179) is a hardcoded string listing ~25 flags, while the env table below is catalog-driven. Two sources of truth for the flag surface = the exact drift class this epic exists to find. The `Options` block omits flags the catalog HAS (`--thinking-budget`, `--compact-trigger`, `--compact-target`, `--compact-keep-recent`, `--compact-max-summary-tokens`, `--compact-cooldown`, `--compact-reference-only`, `--heartbeat-sec`, `--heartbeat-stale`, `--no-enforce-memory`, `--allow-dangerous`, `--websearch-scripts-dir`, `--vision-base-url` etc.) and lists `--raw-log ON|OFF` while the catalog's flag is `--raw-log` (scalar `raw_log` string). A user scanning the Options list sees a smaller surface than the table below it.

**Fix:** generate the Options block from EnvVars (dedupe the Anthropic/DeepSeek pairs, list short flag + help), so one catalog drives both.

### P2-2: Default model strings duplicated across help, ResolveModel, and vision resolution

`deepseek-v4-pro` appears in settings.go:59 (catalog help "default deepseek-v4-pro") — but `ResolveModel` (deepseek/config.go:94) defaults to `deepseek-v4-flash` after commit b21e630 "fix(deepseek): ResolveModel default v4-pro -> v4-flash". The catalog help still says the old default. Same class as P1-1: free-text defaults in Help strings are unvalidated against code constants. Also `deepseek-v4-flash-vision-exp` appears in catalog help, claude_client.go:130 (`DefaultVisionModel`), and FormatUsage's image-input section — three copies of one constant.

### P2-3: `--raw-log` is a scalar (string "on"/"off") but the Options block documents `--raw-log ON|OFF`; inconsistent bool typing

Catalog: `GOPI_RAW_LOG` → `raw_log` string, `CLIFlag raw-log` — NOT in `isBoolKey`. So `--raw-log off` works (scalar consumes next token) but `--raw-log` alone errors "requires a value". Meanwhile `--compact`/`--debug`/`--thinking` are bool switches. The reader (rawlog.go:41) only checks `== "OFF"` (case-insensitive upper) — `GOPI_RAW_LOG=false`/`=0` does NOT disable. Minor inconsistency between the "on (default) or off" help and the actual single-value semantics.

### P2-4: `--db` flag → `memory_db` key → `GOPI_MEMORY_DB` env — legacy `GOPI_HIPPO_DB` still read but not in catalog (intentional, undocumented)

Noted for completeness; low priority.

### P2-5: GOPI.md documents none of the config knobs

GOPI.md (248 lines) contains exactly ONE env-var reference: `GOPI_TIMEOUT_SEC` (in the Rule 2 example). None of --settings/--home/GOPI_SETTINGS/GOPI_HOME/--model/--api-key/--vision-* / thinking knobs / compaction / --db / --timeout / --debug / GOPI_ALLOW_DANGEROUS are documented in the project context file that gopi loads into every session as a system message. The full knob surface lives only in `gopi -h`/`--help-env`. For a project whose agents are expected to self-configure, a GOPI.md "Configuration" section (or pointer to `gopi --help-env`) is warranted.

---

## 3. Design-Reversal / Accumulated-Patch History (git_blame evidence)

- **keep-thinking dead flag**: shipped in 347331a (catalog declared `--keep-thinking`, isBoolKey omitted it, applyFlag had no case) → fixed 2026-08-28 by 4b08400 (isBoolKey + applyFlag + FormatUsage + regression test). **Already fixed in current master** — this review confirms memory #1015's finding is stale (updated in the knowledge base).
- **Unpassed-flag env flooding**: 347331a force-wrote every zero-value knob (`GOPI_DEBUG=off`, `GOPI_TIMEOUT_SEC=0`) → regression (raw request log flooding stderr) → fixed 62dbb27 (SetEnvVars gating). The SetEnvVars machinery and its test coverage (config_test.go:429-475) are the mature form.
- **Bool env truthiness**: `os.Getenv != ""` → truthy-only `EnvBool` (two separate regressions: agent debug dump 08-29, deepseek retry logging 08-30). **But the hide/keep-thinking readers (P1-4) predate this fix and were never migrated** — an incomplete sweep, the classic accumulated-patch pattern.
- **Thinking budget 2048→1024** (d3ef449): code constant halved, catalog help string not updated — P1-1, direct evidence that free-text defaults in the catalog rot.
- **Default model v4-pro→v4-flash** (b21e630): billing-risk fix; catalog help still says v4-pro — P2-2.
- **Role/SyncTo merge collisions** (5f70dbd/e3e3988/32450a1/4bcacbb): three commits to deduplicate a NodeRole field pair across a merge — resolved with the typed single pair; not in this task's core scope but shows the config struct is merge-collision-prone (config.go holds 30+ fields across 8 feature families).

---

## 4. Prioritized Issue List

| ID | Priority | Issue | Fix scope |
|---|---|---|---|
| P0-1 | **P0** | `--home` flag does not redirect settings.json read (main.go resolves settings path before ApplyEnv injects GOPI_HOME) — sandbox inherits real profile's api_key/model/compute_nodes | main.go reorder + regression test |
| P0-2 | **P0** | Help advertises "use the full env var name as a flag" (`--gopi-timeout-sec`) — FlagOverrides rejects it ("unknown flag") | implement env-name→flag mapping OR delete the sentence |
| P1-1 | P1 | GOPI_THINKING_BUDGET help says default 2048, code default 1024 (d3ef449) | fix Help string + add defaults-match-code test |
| P1-2 | P1 | `--settings` interplay with `--home`/GOPI_HOME (same root as P0-1) | fold into P0-1 fix |
| P1-3 | P1 | GOPI_PROMPT env var catalogued/injected/advertised but never read (env channel dead; precedence contract broken) | readInput fallback or remove from catalog |
| P1-4 | P1 | `hideThinking`/`keepThinking` use `!= ""` — `=0`/`=off`/`=false` ENABLE them; `--hide-thinking off` enables hiding; contradicts "Set to 1" doc and EnvBool convention | migrate to config.EnvBool + `=0`/`=off` tests |
| P1-5 | P1 | ~15 env vars read by code but missing from EnvVars catalog/help (sync/worker/role/nudge/effort) | add to catalog or comment legacy |
| P2-1 | P2 | FormatUsage Options block hand-written, not catalog-driven (omits 13 catalog flags, lists --raw-log as ON|OFF) | generate from EnvVars |
| P2-2 | P2 | Default model strings duplicated + stale (catalog "v4-pro" vs code v4-flash; vision model 3 copies) | single constants + catalog reference |
| P2-3 | P2 | `--raw-log` scalar semantics vs "ON|OFF" doc; `GOPI_RAW_LOG=false` doesn't disable | doc-align or bool-ify |
| P2-4 | P2 | Legacy `GOPI_HIPPO_DB` undocumented (intentional) | comment only |
| P2-5 | P2 | GOPI.md documents zero config knobs (only GOPI_TIMEOUT_SEC in a Rule-2 example) | add Configuration section |

---

## 5. Verification

- Build: `go build ./...` OK on master (no code changes made — review-only task).
- Empirically verified: `--gopi-timeout-sec` → "unknown flag" (P0-2); `GOPI_HOME=...` redirects settings, `--home` does not (P0-1); `--home` redirects raw logs/sessions (both forms).
- Tests reviewed: config_test.go precedence/SetEnvVars coverage, usage_test.go catalog-coverage, thinking_trim_test.go (only `=1`/unset — missing `=0`/`=off` cases).
- No code was changed in this review — report only.

## 6. Not Done / Deferred

- No code fixes (epic constraint: review-only child task). P0/P1 items are filed with fix scope for a follow-up task.
- `ResolveComputeNodes`/`ResolveSyncPeers`/`ResolveNodeRole` (three-source merges) read settings via their own `FromSettingsFile(ResolveSettingsPath())` calls — the same P0-1 root (they resolve settings AFTER ApplyEnv in main, so `--home`-injected GOPI_HOME is visible; but `--settings`-only flag form passes no env for them — they re-read GOPI_SETTINGS env which main sets post-hoc; ordering worth a dedicated look in the merge fix).
