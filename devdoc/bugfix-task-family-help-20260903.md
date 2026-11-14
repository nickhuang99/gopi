<!-- doc-class: historical -->
# Bugfix Report: Task Family Help — per-verb -h + dead syncUsageHead (P0-2)

Date: 2026-09-03
Branch: `fix/task-family-help`
Origin: P0-2 in `devdoc/review-cli-task-sync.md` (review branch `review/cli-task-sync`, aeb70ff)

## The bug

`runTasksSubcommand` scanned `args` for `-h`/`--help` BEFORE extracting the
verb, and always printed the whole ~80-line `tasksUsage` family blob:

1. `gopi task sync|harvest|diff|push -h` all printed the identical family
   help — no per-verb detail, and `--full` (M4.24's most important sync
   flag) was missing entirely.
2. `cli_tasks_sync.go:38`'s `syncUsageHead` — a detailed, up-to-date
   per-verb head covering `--full`, `--serve-remote` protocol,
   `--claim-remote`, `--push-only`, `--task-id`, `--tags` — was dead code
   (nothing referenced it; orphaned by the family-usage refactor).
3. The M4.24 `task diff` verb was documented NOWHERE in live help.
4. The unknown-subcommand want-list still listed the RETIRED `ingest`.

## The fix (help/wiring only — no sync behavior change)

`cmd/agent/cli_tasks.go`:
- `runTasksSubcommand` now extracts the verb FIRST, then scans the REST for
  `-h`. For verbs `sync|diff|harvest|push` it prints `syncUsageHead`
  (the per-verb head) followed by the family `tasksUsage`; other verbs keep
  the family blob.
- `tasksUsage` sync line: added `--full` (with the M4.24 full-reconciliation
  semantics) and `--claim-remote`/`--push-only [--task-id T]` to the flag
  list; added a NEW `gopi task diff --peer ALIAS` section (read-only
  anchor-premise self-check, exit 1 on difference, repair = `--full`).
- Unknown-subcommand want-list: `ingest` (retired) → `diff`.

`cmd/agent/cli_tasks_sync.go`:
- `syncUsageHead` comment updated — no longer dead code; it is now the live
  per-verb head source for the sync family.

## Verification

- `go build ./cmd/agent/` OK; `go vet ./cmd/agent/` clean.
- Manual (fresh binary):
  - `gopi task sync -h` → syncUsageHead leads; `--full` visible (12 matches
    incl. the `--full` line, `--claim-remote`, `--push-only [--task-id T]`).
  - `gopi task diff -h` / `harvest -h` / `push -h` → same per-verb head
    (accepted: sync-family head leads for all four).
  - `gopi task -h` → family usage now contains `gopi task diff` + the
    `--full (M4.24)` sentence.
  - `gopi task bogus` → `unknown subcommand "bogus" (want list|show|...
    archive|diff|...` — `diff` in, `ingest` gone.
- `go test ./cmd/agent/` : PASS (59.0s).
- `go test ./internal/tools/` : PASS (detached job, ~13min suite).

## Not Done

- No change to sync/harvest/diff/push execution logic — help surface only.
- Per-verb heads for NON-sync verbs (submit/run/serve/...) still fall back
  to the family blob (out of scope for P0-2).

## Commit

See git log on branch `fix/task-family-help` (single surgical commit).
