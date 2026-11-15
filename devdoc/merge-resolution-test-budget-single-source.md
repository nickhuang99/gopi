<!-- doc-class: normative -->
# Merge Resolution: fix/test-budget-single-source into master

Branch: `fix/test-budget-single-source` (worktree under the shared scratch
queue, never the main checkout). Merge commit `1fb2be7a`, parents
`3e9f0a19` (branch tip) + `d70dec88` (master tip).
Merge base: `07499f15`.

## The conflict, root cause

Six files conflicted, and NONE of them is a textual accident: both sides
independently migrated the SAME three acceptance scripts off `/tmp` onto the
`scripts/lib/tmp-ban.sh` scratch helpers, and both sides annotated the same
test-env sites for the Rule 17 gate. The branch's own change (`scripts/
test-budget.sh`, `Makefile`, the budget devdoc + gate test) touches only
`scripts/`, `Makefile`, `devdoc/` and adds new files — it does NOT touch the
`cmd/agent` tests. So every Go-test conflict is master-vs-merge-base:
master's wording against the branch side's inherited (base) line, i.e. the
conflict was purely COMMENT TEXT. This is why the resolution keeps master's
more precise wording everywhere and folds the branch's extra pointer into it.

## Resolution (one implementation per site, no intent dropped)

| File | Resolution |
| --- | --- |
| `cmd/agent/cli_tasks_worker_test.go` | master's wording (`testutil.SuiteTempRoot()` + "never an ambient os.TempDir()") plus the branch's `devdoc/tmp-ban-scratch-root.md` pointer. |
| `cmd/agent/cli_voice_review_test.go` | kept the branch's full comment block AND master's `env-leak-ok` escape on the `os.Unsetenv` line — the gate reads the contiguous comment block above the statement (`internal/standards/gate.go` `linesAbove`), while stylegate reads the line itself, so the escape is carried on both. |
| `cmd/agent/worktree_release_accept_test.go` | union of both reasons: master's per-call undo-scope argument (t.Setenv's undo is TEST-scoped) and the branch's SnapshotEnv-restores-absence argument, with both `stylegate:ignore` + `env-leak-ok` markers on the `os.Setenv` line. |
| `scripts/acceptance-self-release-stop.sh` | the branch's SELF-CONTAINED form (`HERE` + sourced helper + `mkdir`-failure guard + parent trap). Master's bare call was dropped here because it had no failure guard, and without the `HERE`-sourced helper the guard is what actually protects the run. |
| `scripts/acceptance-self-release.sh` | union: master's usage text and its guarded source line, branch's `HERE` form, and MASTER's ledger allocation (`gopi_scratch_mkdir self-release-ledger` / `self-release`) for both arms — the helper registers AND creates in one call, so the branch's `mktemp -d "$LEDGER_ROOT/…"` pair is redundant. |
| `scripts/acceptance-zombie-auto-dispose.sh` | master's `gopi_scratch_mkdir zombie-phase` over the branch's `mktemp` + `gopi_scratch_register` pair — same guarantee (off-`/` assert, registration, age-GC-able name), one call. |

### The `LEDGER_ROOT` subtlety (the only non-obvious hunk)

The branch's `LEDGER_ROOT`/`cleanup_sandbox`/`REUSE` block is OUTSIDE the
conflict hunks, and its three `mktemp -d "$LEDGER_ROOT/…"` users were the
exact lines master replaces. Taking master's side would therefore have left
`LEDGER_ROOT=…` assigned and never read. Resolution keeps master's allocation
AND keeps the `LEDGER_ROOT` producer, whose real job is the reuse-vs-self-built
cleanup policy (`cleanup_sandbox` returns early for a REUSE run so the
operator's sandbox survives). Its comment was rewritten to say that, instead of
claiming the value names an SB arm it no longer names.

## Verification

- `git diff --name-only --diff-filter=U` → empty; zero conflict markers in
  `cmd/`/`scripts/`/`internal/`; `grep -c '^trap '` = 1 trap per script
  (no shadowed second trap).
- `bash -n` on all three scripts: OK. `gofmt -l` on the three touched Go
  files: no output.
- `go build ./...`: exit 0. `go vet ./cmd/agent/`: exit 0.
- Touched tests: `go test ./cmd/agent/ -run
  'TestUserHomeDirOrDefaultFallsBack|TestWorkerHelpDoesNotRun|TestWorkerReleaseOwnWorktree_Acceptance|TestOwnRelease_RefusesLiveSibling'
  -count=1 -v` → all four PASS (`ok github.com/the operator/gopi/cmd/agent 1.193s`).
- `go test ./internal/standards/` → ok (the env-escape window the resolution
  targets is the gate this check exercises).

### Attribution of the two failures seen while verifying (NOT this resolution)

1. `go test ./internal/stylegate/` fails with ONE production hit:
   `internal/version/git.go:25 (ignored error-typed value (cmd.Process.Kill()))`.
   `git diff master HEAD -- internal/version/` is EMPTY and this resolution
   touched no production file — the hit is on master's own tip.
2. A first, wider `cmd/agent` run aborted with
   `fatal error: runtime: netpoll failed / epollwait on fd 4 failed with 22`
   right after `gopi worker: --daemon: write GOPI_DAEMON_READY_FD: invalid
   argument`. That is the known full-suite/daemon-spawn hang class this task
   forbids as an exit gate; re-running narrowed to the tests in the touched
   files passes cleanly. The touched lines are comments only — they cannot
   change runtime behaviour.

No full `go test ./...` was run (forbidden here: known pre-existing hang).
