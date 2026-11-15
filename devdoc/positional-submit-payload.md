<!-- doc-class: historical -->
# Task Report: positional `gopi task submit` no longer creates an un-claimable row

Branch: `fix/positional-submit-payload`
Worktree: `~/workspace/gopi-positional-payload`

## Completed

- **CLI fix** (`cmd/agent/cli_tasks.go`, `case "submit"` in `parseTasksArgs`):
  when no `--payload` is given, the positional summary is **mirrored into the
  payload column** (summary = payload, the "the summary IS the briefing"
  model the code already documented). `tasksOptions.payloadMirrored` records
  the decision; `runTaskSubmit` prints the named constant
  `taskSubmitPositionalPayloadNote` on stderr (never a silent semantic
  change). `--payload` keeps its exact meaning (summary = one-line headline,
  payload = full briefing; no mirror, no note).
- **`--help` text** for `task submit` updated to state the mirror.
- **Regression pins** (`cmd/agent/cli_tasks_submit_payload_test.go`, new):
  - `TestCLIPositionalSubmitIsDispatchable` — real binary positional submit →
    the payload column carries the text (raw SQL check, so the assertion does
    not go through the same composer it tests) → `tools.TaskNextFiltered`
    (the exact call `worker`/`serve`/`dispatch` use) returns the row.
  - `TestCLIPositionalSubmitRunsThroughDispatcherE2E` — positional submit →
    `gopi task run next` claims it (never "no pending task") → the row ends
    `completed` and the CHILD session's user message carries the briefing.
  - `TestCLISubmitExplicitPayloadWins` — an explicit `--payload` is not
    overwritten and no mirror note is printed.
- **code_search corrupt-index half: verified, no new code needed.** The
  2026-09-10 NUL-filled `.gopi-tags` self-heal already landed in master as
  `72e9932` (confirmed ancestor of master):

  ```
  $ git merge-base --is-ancestor 72e9932 master && echo "72e9932 IN MASTER"
  72e9932 IN MASTER
  ```

  Guards present and green: `TestCodeSearchSelfHealsCorruptIndex` (NUL-filled
  / truncated-header / foreign-text / empty, each asserts a ctags DEFINITION
  hit — not the ripgrep fallback — plus exactly one self-heal, index valid on
  disk, and no re-heal on the second call), `TestTagsIndexValid`,
  `TestEnsureTagsSelfHealsCorruptIndex`. This branch ADDS the CLI-level pin
  `TestCLICodeSearchSelfHealsCorruptIndex`
  (`cmd/agent/cli_code_search_corrupt_test.go`): the real binary over a
  1 MiB NUL index must exit 0 with a definition hit and leave the artifact
  rebuilt in place. Extra manual E2E proof:

  ```
  $ python3 -c "open('$T/.gopi-tags','wb').write(b'\x00'*(1<<21))"
  $ bin/gopi code-search CsHealProof --root $T
  CsHealProof  src/a.go:3  func  func CsHealProof() {}
  exit=0
  $ head -c 40 $T/.gopi-tags
  !_TAG_FILE_FORMAT	2	/extended format; --      <- index rebuilt in place
  ```

## Not Done

- The engine (`tools.TaskSubmit`) was deliberately NOT changed: auto
  registration rows and bridge/mirror inserts legitimately create payload-less
  ledger rows, and `TaskNextFiltered`'s bug028 filter must keep skipping them.
  Only the CLI (the only positional-submit producer) mirrors.
- Pre-existing payload-less pending rows already sitting in a live ledger
  (e.g. the peer1 `01a089fe` row in the incident) are NOT backfilled by this
  change. They are legacy registration rows; they need an explicit
  `gopi task run <id>` or a cancel+resubmit. No bulk UPDATE was attempted —
  rewriting another ledger's history is an operator decision, and the old
  rows' emptiness is the only signal that they are ledger entries.
- The mirror makes `summary == payload` for the positional form. `taskRunBriefing`
  now DEDUPES that case (a payload identical to the summary is not emitted as a
  second `[Briefing payload]` section — the child must not read the same
  one-line text twice; distinct texts still produce both sections), pinned by
  `TestTaskRunBriefingDedupesMirroredPayload`. The payload column stays
  populated for dispatchers that read it directly.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Vet: `go vet ./cmd/agent/ ./internal/tools/` → clean
- Mutation proof (the pin is a real red line): forcing the mirror condition
  to `false` reproduced the reported defect verbatim —
  `stderr: ""`, `payload = ""`, and
  `queue empty — the positionally submitted row is un-claimable (payload filter skipped it)`.
  Restoring the fix → PASS.
- Tests: see "Test output" below.
- Commit: see the branch tip (`git log --oneline -1` on
  `fix/positional-submit-payload`).

## Notes for future agents

- Two surfaces, one rule: **a row with an empty payload is not dispatchable**
  (`TaskNextFiltered`, bug028). CLI `submit` therefore must never leave the
  payload empty; engine callers may, when the row is a ledger entry.
- If you ever add another positional submit entry point, it must mirror too —
  the pin here only covers `gopi task submit`.
- `worker --once` printing `queue drained` while `task list --status pending`
  shows rows is the signature of this defect (or of `--skip-tag`/merger-owned
  rows); check the payload column first.
