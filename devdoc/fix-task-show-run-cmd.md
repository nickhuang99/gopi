<!-- doc-class: normative -->
# Task Report: task show run_cmd param ordering bug (t-1788551138 follow-up)

## Completed
- Root-caused the display bug: `runMethodFor` (`cmd/agent/cli_tasks.go`) stamped
  the manual run_cmd as `--settings <PATH> gopi task run T` — the settings
  token-pair was prepended BEFORE the `gopi` binary. That string is not
  re-runnable (a shell would try to exec `--settings`), and it did not match
  the verbatim launch `gopi --settings <PATH> task run T` that the operator
  actually typed.
- Fixed `runMethodFor`: builds `gopi` first, inserts
  `config.LaunchSettingsArg()` (when non-empty), then appends
  ` task run <target>` and the `--report-to`/`--repo` options. `task show`
  now displays the truthful, re-runnable `gopi --settings X task run T`.
- Updated regression test `TestRunMethodFor_SettingsContext` to assert the
  corrected order (settings AFTER the binary), plus its doc-comment.
- Appended a Correction note to `devdoc/run-cmd-settings.md` (the old report
  recorded the buggy order as its verified Result).

## Not Done
- Existing DB rows already stamped with the wrong run_cmd are NOT rewritten —
  run_cmd is stamped ONCE at launch; the fix applies to future runs.
- `dispatch` and `resume` stamp sites untouched (their run_cmd strings were
  already correct / intentionally settings-free).
- No live model E2E re-run (would need a live endpoint); the construction
  order is fully covered by the unit regression.

## Verification
- Build: `go build ./...` — OK
- Tests:
  - `go test ./cmd/agent/ -run TestRunMethodFor` — PASS (both cases)
  - `go test ./cmd/agent/` (full suite) — PASS (72.7s)
  - `go test ./internal/config/` — PASS
  - `go test ./internal/tools/ -run 'TestTaskShow|StampRun|RunProvenance|Mirror'` — PASS
- Commit: `f5a5aee` on branch `feat/fix-task-show-run-cmd` (worktree `~/workspace/gopi-task-show-run-cmd`, shares main repo refs — no push needed)
