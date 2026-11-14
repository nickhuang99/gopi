<!-- doc-class: historical -->
# Task Report: register the merge-backlog ghost-ref alarm in `taskNotifyAlarmEvents`

Task `01a0d7672e84776f9154f6b208b6b84e` — pre-existing master red
`TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth`.

## Adjudication (which side was wrong)

The gate was right and the **registry** was the wrong side. Evidence, not assumption:

- The alarm is **fired**: `internal/tools/task_merge_backlog_dispatch_guard.go:317`
  calls `NotifySyntheticEvent(mergeBacklogGhostEvent, "", "", reason)` when the
  dispatch guard closes a ghost-branch row (a row whose branch ref does not
  resolve). So it is neither dead code nor a declaration awaiting wiring.
- The constant is **declared** in the same file (`mergeBacklogGhostEvent =
  "merge-backlog-ghost-ref"`, line 48) and the wire literal is spelled nowhere
  else in production code (the only other occurrence is
  `mergeBacklogGhostGuardHolder`, `"merge-backlog-ghost-ref-guard"`, a different
  value) — so Rule 1b/13 holds already; the correct fix is to register the
  existing constant, never to mint a new one.
- The registry `taskNotifyAlarmEvents` (`internal/tools/tasknotify.go`) is the
  SINGLE source of truth that `loadTaskNotifyConfig`'s `all`/`any` arm iterates,
  so its absence means `GOPI_TASK_NOTIFY_EVENTS=all` silently dropped the alarm
  for the subscriber who asked for everything — Rule 15's silent failure.

## Completed

- Added `mergeBacklogGhostEvent` to `taskNotifyAlarmEvents` in
  `internal/tools/tasknotify.go`, with a comment that explains why the entry
  exists and names the invariant, and that refers to the constant — the wire
  literal is **not** mirrored in the comment (the literal-mirror gate family
  scans production `.go` files, comments included).
- No gate, test, or predicate was touched: `internal/tools/tasknotify_alarms_test.go`
  is byte-identical to base, and the fix is one registry entry.
- No new test file was needed, and that is a finding, not a saving: the briefing
  predicted the required regression test did not exist. It **already exists and
  already pins the general invariant** — `TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth`
  derives its required set from the code (`covToolsScanNotifyCalls` parses every
  non-test file for `NotifySyntheticEvent`/`MergeVerifyNotify` call sites,
  `covToolsResolveAlarmConst` resolves alias chains) and names nothing. The red
  line itself is the proof: it reported
  `(fired as mergeBacklogGhostEvent)` — i.e. the fire site was derived, not listed.

## Not Done

- **The gate's blind spot was not closed** (out of scope, recorded honestly). Its
  scan is rooted at `internal/tools` only, so a future alarm fired from
  `internal/agent` with a *new* constant would be invisible to it. Checked as a
  live risk, not assumed: the only agent-side fire sites today are
  `internal/agent/loop.go` — `tools.NotifySyntheticEvent` call sites `:2468` (`notifyEventLoopStuck`), `:2802` (`notifyEventStuckObservation`), `:2815` (`notifyEventLoopStuckToolful`), `:3523` (`notifyEventWallclockExceeded`), all of them passing
  the already-registered tools-package session-alarm constants
  (`notifyEventLoopStuck`, `notifyEventStuckObservation`,
  `notifyEventLoopStuckToolful`, `notifyEventWallclockExceeded`). So no live
  alarm is missing; the coverage gap is future-only. Closing it means scanning
  `../agent` in `covToolsScanNotifyCalls` and keeping the agent-side constant
  aliases resolvable — a separate, larger change that the briefing did not ask
  for and that would widen this diff into the agent package.
- The remaining `internal/tools` package noise (e.g. the long-running
  `TestBidir2ServeTickAutoSync` over Go's 10m default) was **not** touched: the
  runs here were branch-scoped and focused (Rule 27), and nothing in this diff
  reaches that path.

## Verification

- Build: `go build ./...` → `BUILD OK` (exit 0)
- Vet: `go vet ./internal/tools/` → `VET OK` (exit 0)
- Tests: `go test ./internal/tools/ -count=1 -run
  'TestCovToolsNotify|TestCovToolsAlarmLiterals|TestMergeHandoverEventLiteralMirrorGate|TestMergeBacklogGhost'`
  → `ok github.com/nick/gopi/internal/tools 3.200s`; verbose family green:
  `TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth` PASS,
  `TestCovToolsNotifyAllFilterDeliversEveryAlarm` PASS (`/all` + `/any` subtests),
  `TestCovToolsNotifyAllFilterEnumeratesEveryAlarm` PASS,
  `TestCovToolsAlarmLiteralsHaveOneDefinitionSite` PASS.
- Ran: `go test ./internal/tools/ -run 'TestCovToolsNotifyAlarmRegistry' -count=1`
  (the briefing's acceptance invocation, verbatim)
- Result: pre-fix on this worktree —
  `--- FAIL: TestCovToolsNotifyAlarmRegistryIsTheSourceOfTruth (0.19s)`
  `tasknotify_alarms_test.go:221: the alarm "merge-backlog-ghost-ref" (fired as mergeBacklogGhostEvent) is NOT in taskNotifyAlarmEvents …`
  `FAIL github.com/nick/gopi/internal/tools 0.427s (exit 1)`; post-fix —
  `ok github.com/nick/gopi/internal/tools 0.788s`, `exit=0`.
- Commit: `7ff88acf` (`fix(notify): register the merge-backlog ghost-ref alarm in
  taskNotifyAlarmEvents`) — the code fix; this report lands in the commit on top
  of it on the same branch.

## Evidence (self-filed)

- **Registry now carries the alarm**: `git diff` on
  `internal/tools/tasknotify.go` shows exactly one added slice entry
  (`mergeBacklogGhostEvent`) plus its comment — `git status --porcelain` before
  the commit listed `M internal/tools/tasknotify.go` and nothing else.
- **The invariant actually fails red when violated** (controlled mutation, so the
  claim is a measurement and not a reading of the test): a temporary
  `internal/tools/zz_alarm_mutation_probe.go` declaring `zzProbeAlarmEvent` and
  calling `NotifySyntheticEvent(zzProbeAlarmEvent, …)` — a fired alarm that is
  NOT registered — made the gate fail with
  `tasknotify_alarms_test.go:221: the alarm "zz-probe-unregistered-alarm" (fired as zzProbeAlarmEvent) is NOT in taskNotifyAlarmEvents …`
  (`FAIL github.com/nick/gopi/internal/tools 0.808s`). The probe file was then
  deleted; `git status --porcelain` reprinted only `M internal/tools/tasknotify.go`.
  This is the red half of the red-then-green pair the fix requires.
- **The gate file was not touched**:
  `git diff --stat master...HEAD -- internal/tools/tasknotify_alarms_test.go`
  prints nothing (the file is identical to base), so the fix is on the code side,
  not the gate side.
- **Delivery, not just enablement**: `TestCovToolsNotifyAllFilterDeliversEveryAlarm`
  runs the real pipeline with `GOPI_TASK_NOTIFY_EVENTS=all` and `=any` and asserts
  a payload per registered alarm — it iterates the same registry, so the newly
  registered alarm is now delivered, and it passed for both spellings.
