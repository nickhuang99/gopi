<!-- doc-class: historical -->
# Task Report: '@file' payload references for CLI arguments

Epic: submitting multi-line text with special characters on the command line
is difficult — shell quoting mangles characters and destroys formatting that
is essential to task execution. A value starting with `@` followed by a file
path must be replaced by the file's contents.

## Completed

- **`internal/config/fileref.go`** — `ExpandFileRef(value) (string, error)`:
  the ONE shared definition of the `@path` convention:
  - `@PATH` (PATH = single token, no whitespace) → file contents, verbatim
    (no trimming — a briefing's trailing newline can be significant).
  - `@-` → piped standard input (refuses with a clear message when stdin is
    a TTY, so an interactive `--payload @-` cannot hang).
  - `@@x` → literal `@x` (escape hatch for values that genuinely start with
    a single at-sign).
  - Passthrough (NOT a ref): bare `@`, empty string, `@`-not-at-start
    (`user@host`, `see @briefing.md`), and `@<text containing whitespace>`
    (prose like `@nick mentioned this in #chan` — without this guard such
    prose would fail with a confusing `open <words>` error).
  - Oversized refs (> 8 MiB, `maxFileRefBytes`) refuse with a clear error —
    accidental `@/var/log/huge.log` must fail fast, not OOM.
  - Missing/unreadable file → error naming the original `@` reference.
- **Wiring (3 consumption points, chosen as the payload surfaces where
  special characters/formatting matter most):**
  - `internal/config/flags.go` — `--prompt` (generic flag path; env
    `GOPI_PROMPT` unaffected — the expansion happens at flag-parse time).
  - `cmd/agent/cli_tasks.go` `parseTasksArgs` — `task submit --payload` and
    `task reopen --reason` (the memoryFlagSet hand-rolled parser path;
    expansion happens in the pure, unit-testable front-end BEFORE the
    engine, so `tools.TaskSubmit`/`TaskVerifyToReopen` need no change).
  - cmd/agent re-exports `var ExpandFileRef = config.ExpandFileRef` — one
    name at both call sites.
- **Usage texts**: `internal/config/help.go` (new "'@' file reference"
  section with examples), `internal/config/settings.go` GOPI_PROMPT catalog
  line, `cmd/agent/cli_tasks.go` tasksUsage submit/reopen entries.
- **Tests** `internal/config/fileref_test.go`: verbatim read (quotes,
  `$`, backticks, tab, blank lines), passthrough table, `@@` escape,
  missing-file error naming the ref, oversize refusal, `IsFileRef` truth
  table.

## Not Done

- Positional-argument file refs (a positional starting with `@`) — positionals
  already support "existing file → contents become prompt" natively in
  `readInput`, so the `@` prefix adds nothing there.
- `task_submit` TOOL payload (the LLM-side tool): its payload arrives via
  tool JSON args, not the command line — the shell-quoting problem does not
  exist there.
- Subcommand flag values other than the three wired ones (e.g. `--tags`)
  are short/character-safe by design.

## Verification

- Build: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin ./cmd/agent/` OK
- Tests: `go test ./internal/config/ ./cmd/agent/ ./testsuite/` PASS.
  `internal/agent` shows exactly 2 failures — stash-verified PRE-EXISTING on
  clean master (TestAgent_Run_ContextTrimReplaceSlimming,
  TestAgent_Run_ToolResultPayloadOnce); `internal/tools` 1 pre-existing
  (TestEnsureTagsRegeneratesForNewerTS). None touched by this change.
- Commit: `73a7f86 feat(cli): '@file' payload references for
  --payload/--reason/--prompt` on branch `feat/file-payload-arg`
  (worktree ~/workspace/gopi-argfile).
- E2E smoke (fresh binary, real ledger): `task submit --payload @/tmp/b.md`
  → `task show` briefing byte-exact incl. `` ` `` and `"quotes"` and
  `a@b.c` and blank lines; `--payload '@@literal-at-sign'` → briefing
  `@literal-at-sign`; `--payload '@nick mentioned this in #chan'` →
  briefing unchanged; `--prompt @/nonexistent` →
  `gopi: @file reference "@/nonexistent": open /nonexistent: no such file`.
