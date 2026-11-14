<!-- doc-class: historical -->
# Epic verification — settings.json `"compact": "off"` string parse failure (P1-C)

Date: 2026-09-10 · Repo: ~/work/GoPi @ master (28b21b5)

## Verdict: ALREADY FIXED on master — no code change needed

The epic asked for a tolerant `Compact` unmarshal so a string value (`"compact": "off"`)
does not fail the whole settings.json parse. This was verified as **already implemented
and merged** before any new code was written.

## Evidence

- Merge **fe81c18** (`feat/fix-compact-settings-string`) is an ancestor of master HEAD
  (verified with `git merge-base --is-ancestor fe81c18 HEAD`).
  Its chain: `9ea3f6b` fix · `f71c79b` regression test · `993442f` docs.
- `internal/config/config.go`: `type CompactEnabled bool` with tolerant `UnmarshalJSON`
  — accepts bool verbatim plus strings `on/off/true/false/1/0/yes/no/enabled/disabled`
  (case-insensitive, via `normalizedBoolValue`); invalid value returns a clear error
  naming the field (documented in the type comment). `Config.Compact` is `*CompactEnabled`.
- Regression test `TestCompactStringAccept` present and covering: bool true/false,
  `"on"`/`"off"` strings, invalid-value error path, sibling-setting survival (no other
  settings lost), nil/unset default. Env-bridge covered by `TestApplyEnv_CompactStringPropagates`.

## Acceptance gate results (this session, master)

- `go build ./...` — green.
- `go test ./internal/config/ -run TestCompactStringAccept` — PASS (incl. bridge test).
- `go test ./...` — 5 failures, **proven pre-existing** at fix baseline `9ea3f6b~1` via a
  clean-worktree run (`git worktree add /tmp/wt 9ea3f6b~1`):
  - 3 × code_search tests: require `rg` binary, absent on this host.
  - `TestJobNoNoticeOnKill`: flaky only under full-suite parallelism; passes in isolation
    and x5 repeated.
  None touch the compact/config path. Worktree removed after the run.

## Working notes

- Untracked at time of writing (unrelated to this epic): devdoc notes
  `task-submit-param-position-pass4-20260909.md`, `write-task-area-introduction-tracking.md`, `sandbox/`.
- Nothing to commit for this epic; no repo changes were made.
