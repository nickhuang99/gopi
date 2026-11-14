<!-- doc-class: historical -->
# Task Report: task tab completion follows help + help.go task-section alignment

## Epic
`gopi task <TAB>` must offer the same sub-verbs the help advertises (help =
the user-facing surface), never dispatch-only internal verbs or deprecated
aliases — and the help message itself must be aligned with the actual
commands/verbs.

## Root cause
Two unidirectional drifts, both on the "help" side:

1. **Top-level `gopi -h` task section was stale.** `internal/config/help.go`
   `FormatUsage()` documented only ~9 task verbs (list/show/archive/submit/
   run/update/verify + merger + worker), while the dispatcher accepts 21
   verbs. 12 real, user-runnable verbs — `tag-resumed`, `search`, `reopen`,
   `adopt`, `audit-merges`, `merge`, `merge-execute`, `sync`, `diff`,
   `harvest`, `push`, `sweep-zombies` — were simply absent from the top-level
   help (several existed only deep inside the family doc `tasksUsage`).
2. **`gopi task -h` (tasksUsage) itself omitted two verbs**: `adopt` and
   `audit-merges` (dispatch cases existed, family doc did not document them).
3. The tab-completion candidate set (`taskCompletionNames`,
   cli_completion.go) mirrored the DISPATCH superset (21 verbs incl. the
   deprecated `serve` alias), so `gopi task <TAB>` offered more — and
   differently — than the help advertised.

The user's symptom (`gopi task <TAB>` output ≠ help verbs) was the direct
consequence: tab followed dispatch, help followed an older ad-hoc subset.

Note: master advanced mid-task — the stranded `feat/help-task-serve-to-merger`
branch (serve→merger top-level help fix) was merged into master (0486257)
while this task was in recon; the worktree was re-based onto that fresh
master so the merged fix is included and extended.

## Completed
- **Single source of truth: tab follows help.** `taskCompletionNames`
  (cli_completion.go) now = the family surface `tasksUsage` documents.
  The deprecated `serve` alias is dropped from tab (help documents `merger`
  as canonical; dispatch still accepts `serve` for scripts).
- **Family help (tasksUsage, cli_tasks.go) now documents every verb**:
  added accurate `gopi task adopt` and `gopi task audit-merges` usage blocks
  (20 documented verbs; the full dispatch surface minus the serve alias).
- **Top-level `gopi -h` task section fully aligned**: added the
  `gopi task <subcommand> # …` verb enumeration head (all 20 verbs listed,
  mirroring the tab order) plus per-verb usage blocks for the 12 previously
  missing verbs (tag-resumed/search/reopen/adopt/audit-merges/merge/
  merge-execute/sync/diff/harvest/push/sweep-zombies), preserving the
  existing list/show/archive/submit/run/update/verify/merger/worker blocks
  and the append-only ledger note.
- **Regression guards**:
  - `TestTaskCompletionNamesMatchFamilyHelp` (cli_completion_test.go): every
    tab candidate must have a documented `gopi task X` usage line in
    tasksUsage, and every documented verb must be a tab candidate — the
    Epic's lock that tab==help and neither drifts alone.
  - `TestTaskCompletionNamesMatchDispatch` extended to exempt the `serve`
    alias (deprecated — dispatch-only, not tab/help surface).
  - `TestTopLevelUsageTaskVerbEnumMatchesCompletion` (usage_test.go): the
    top-level help enum region must carry every completable verb and never
    the serve alias.

## Not Done
- Runtime dispatch untouched: `gopi task serve` still executes (alias of
  merger) — scripts keep working; only the tab/advertised surface changed.
- The 4 pre-existing gofmt-cleanliness issues in other test files
  (cli_branch_scan_test.go etc.) were left untouched (not part of this task).

## Verification
- Build: `CGO_ENABLED=0 go build ./cmd/agent/` OK; `go build ./...` OK;
  `go vet ./cmd/agent/` clean.
- Tests: `go test ./cmd/agent/ -run 'TestCompletion|TestTaskCompletion|
  TestTopLevelUsage|TestTasksUsage' -count=1` → 14/14 PASS (incl. both new
  lock tests); `go test ./internal/config/ -count=1` OK.
- Full suite: `go test ./cmd/agent/ -count=1` (job_start, ~10 min) — result
  appended below.
- Live render: `gopi -h` task section shows the 20-verb enum + all usage
  blocks; `gopi completion bash` task arm = the same 20 verbs (`serve` gone).
- Commit: `9971620` on branch `feat/tab-help-align` (worktree
  ~/workspace/gopi-tab-help-align — commit lands in the shared main repo
  via worktree refs; no push needed).

## Deliverable
- Branch: `feat/tab-help-align`
- Recovery: `git show feat/tab-help-align:devdoc/tab-help-align-20260903.md`
- Commit is in the MAIN repo (worktree-shared refs).
