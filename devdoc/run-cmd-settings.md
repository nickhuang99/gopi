<!-- doc-class: normative -->
# Task Report: run_cmd must carry the launch --settings context

Branch: `feat/run-cmd-settings` (worktree `~/workspace/gopi-run-cmd-settings`, base 876090d). Commit `8510649`.

Epic goal: **task show run_cmd field must be accurate.** Reported: a manual run launched as

```
gopi --settings ~/.gopi/.do_settings.json task run t-1788497909
```

was recorded (task show) with

```
run_cmd:  gopi task run t-1788497909
```

— the `--settings .../.do_settings.json` parameter was silently dropped. That flag is operationally load-bearing: it names the settings file that carries provider identity (`api_key` / `base_url` / `model` / `reasoning_effort`). Here the `.do_settings.json` selects a *different* provider (do-ai.run, model `deepseek-v4-flash-0731`) than the default `settings.json` (DeepSeek, `deepseek-v4-flash`) — the run would resume under the WRONG provider.

## Root Cause

Two independent defects compounded:

1. **provenance accuracy** — `runMethodFor` (`cmd/agent/cli_tasks.go`) rebuilt run_cmd only from the parsed run-verb options (`target`, `--report-to`, `--repo`). Top-level process-global flags that precede the subcommand (`--settings`, `--home`) are NOT part of the sub-verb options, so they never made it onto the stamped command. The design doc (devdoc/run-method-provenance.md) wants run_cmd = "verbatim launch command / like /proc/<pid>/cmdline", truthful and re-runnable.

2. **the flag never reached the run at all** — `main()` runs the subcommand chain/shortcut BEFORE the agent-path config block that honours `--settings`/`--home`. Group verbs (`task run`, `worker`, `memory`, `web`…) `return` from main() before that block, so a leading `--settings X` was consumed by the commit's ssh-pass-through argv-skipper in `isTasksSubcommand` and dropped. The child gopi run (which inherits environment) then resolved the DEFAULT provider, silently diverging from the operator's intent.

## Fix

Two-part, per Rule 6 (engineered roots down, not symptom patches):

### ① Config helpers — `internal/config/settings.go` (new)
- **`SettingsEnv`** const (`GOPI_SETTINGS`), single name so env references cannot drift.
- **`BootstrapEnvFromArgs(args)`** — hoists a process-global `--settings PATH` / `--settings=PATH` and `--home PATH` / `--home=PATH` onto the environment (GOPI_SETTINGS / GOPI_HOME) BEFORE any subcommand dispatch. Only flags literally present fire (never clobbers a pre-exported value absent the flag); foreign flags (e.g. sub-verb `--model`) are never consumed. Idempotent with the agent-path config block (which re-derives the same value).
- **`LaunchSettingsArg()`** — returns a re-runnable `--settings <PATH>` token-pair when a non-default settings context is in effect, `""` otherwise. Resolution: argv `--settings` (highest, echoes even if it names the default — the operator typed it) > `GOPI_SETTINGS` env (only when ≠ default settings path) > `""` (default /proc cleanliness). This is what makes run_cmd truthful.
- **`flagSettingsPath()`**, **`shellQuote()`** helpers (argv scan `--settings X` / `--settings=X`; single-quote shell escaping for spaced/metachar paths).

### ② Wiring — `cmd/agent/main.go` + `cmd/agent/cli_tasks.go`
- `main()` calls `config.BootstrapEnvFromArgs(args)` right after the SIGPIPE guard, before the subcommand chain — so task-run children and all group verbs inherit the injected GOPI_SETTINGS/HOME. Fixes defect #2 (the run genuinely happens under `.do_settings`).
- `runMethodFor` prepends `config.LaunchSettingsArg()` to the manual run_cmd (when non-empty), behind which the existing `--report-to`/`--repo` still ride. Fixes defect #1.

### Result
The reported invocation now stamps (verified live against a throwaway settings file):

```
run_method: manual
run_cmd:   --settings /home/user/workspace/gopi-run-cmd-settings/.scratch-home/.do_settings.json gopi task run t-run-setting-e2e
```

## Not Done
- `dispatch` (center row) and `resume` stamp sites were intentionally left unchanged: their run_cmd is the exact remote command handed to a NODE (which uses the node's own default settings — echoing a center-side `--settings` would be misleading) / the resume recipe (correct as-is). Only the manual/common stamp needed the settings context.
- No schema migration — run_cmd column already exists; only its value is now more complete. Sync propagation (last-non-empty) already carries opaque cmdlines.

## Verification
- Build: `go build ./...` — OK (`bin/gopi` rebuilt).
- Config tests: `TestBootstrapEnvFromArgs_*` (flag, equals-form, no-clobber, foreign-arg-ignore), `TestLaunchSettingsArg_*` (env=default suppressed, env≠default echoed), `TestShellQuote` — all PASS.
- cmd/agent: `TestRunMethodFor` (unchanged, default settings→clean) + new `TestRunMethodFor_SettingsContext` (non-default GOPI_SETTINGS → `--settings <path> gopi task run …`) — PASS.
- Suites: `go test ./internal/config/` PASS; `go test ./cmd/agent/` (full, 73s) PASS; `go test ./internal/tools/ -run 'TestTaskShow|StampRun|RunProvenance'` PASS.
- Live E2E: real binary `task submit` + `task run --settings <non-default>` → `task show` run_cmd shows the settings prefix; child ran under the do-settings provider. (Run model-independent; a real model call needs a live endpoint, not part of this check.)
- Commit: `8510649` on `feat/run-cmd-settings`.

## Pitfalls Encountered (for future agents)
- **Worktree path discipline (Rule 6b):** several earlier edits were accidentally targeted at the MAIN repo (`~/work/GoPi`) because read+edit shared main-repo absolute paths — `git status` in main showed the files as modified. Reverted there with `git restore <files>` (never `git restore .`) and re-applied in the worktree. **Always edit via `~/workspace/gopi-run-cmd-settings/...`, never `~/work/GoPi/...`.**
- **Environment rollback:** the worktree content was rolled back mid-session once (files reverted to base). Commit promptly (logical unit per commit) so work survives a rollback.
- The bash workdir is the main repo by default; `rm -rf` of scratch dirs under the worktree is refused as "outside working directory" — clean scratch by deleting files/dirs explicitly, and don't leave untracked scratch: `git status` before commit.

## Correction (t-1788551138 follow-up)
The recorded form above — `--settings <PATH> gopi task run T` — was NOT re-runnable:
the settings token-pair sat BEFORE the binary, so a shell would try to exec
`--settings`. Correct form (fixed in `feat/fix-task-show-run-cmd`):
`gopi --settings <PATH> task run T` — the settings global flag slots AFTER the
binary, before the `task run` verb. `runMethodFor` now builds `gopi` first,
inserts `config.LaunchSettingsArg()` (when non-empty), then appends
` task run <target>` and the return-path options. `task show` now displays the
truthful, re-runnable command line.
