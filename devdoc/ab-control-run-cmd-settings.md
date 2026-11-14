<!-- doc-class: historical -->
# Task Report: AB-CONTROL — run_cmd must include --settings

## Completed
- `runMethodFor` (cmd/agent/cli_tasks.go) now records an explicit `--settings PATH`
  into run_cmd when THIS process's argv carries it (standalone and `--settings=PATH`
  forms; flag position irrelevant). Default resolution (GOPI_SETTINGS env /
  ~/.gopi/settings.json default) is NEVER synthesized — the flag is echoed only
  when actually passed.
- New named constant `cliFlagSettings` + helper `settingsFlagFromArgs` (Rule 1/2:
  one flag-name constant, one argv scan).
- Regression test `TestRunMethodForSettingsFlag` pins: bare-argv no-echo,
  standalone flag, equals-form, flag-after-verb (the peerbox screenshot shape),
  and `--report-to`/`--settings` coexistence.

## Why (root cause)
run_cmd was reconstructed in runMethodFor from the options struct only —
`gopi task run <id> [--report-to A] [--repo B]` — but `--settings` is a
PROCESS-level flag (main.go FlagOverrides → GOPI_SETTINGS env; never reaches
parseTasksArgs/tasksOptions). The real peerbox invocation
`gopi --settings ~/.gopi/.do_settings.json task run t-…` therefore stamped a
bare run_cmd: an A/B run (HP baseline vs peerbox feature build) was
indistinguishable from a default-settings run — the settings file selects the
model provider + key config, which is exactly what run provenance must show.

## Not Done
- run_cmd of `worker` rounds still shows the fixed worker string (worker has
  no --settings concept by design — the worker env is its provenance).
- No schema change: run_cmd remains a single text column; --settings rides it.

## Verification
- Build: CGO_ENABLED=0 go build ./cmd/agent/ — OK (bin/gopi rebuilt)
- Tests: TestRunMethodFor + TestTaskStampRun + TestRunMethodForSettingsFlag
  PASS (go test ./cmd/agent/ -run 'TestRunMethodFor|TestTaskStampRun')
- go vet OK; gofmt clean
- Live smoke (isolated GOPI_HOME=.tmp-abctl):
  `gopi --settings <path> task run t-1788527934` → `task show` renders
  `run_cmd: gopi task run t-1788527934 --settings <path>`; stamp survives the
  transition to completed (writeback never clears it).
- Commit: ae39ded fix(tasks): run_cmd records an explicit --settings flag verbatim
