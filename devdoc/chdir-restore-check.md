<!-- doc-class: normative -->
# Chdir-restore check — no test silently discards a cwd change

Source ticket: test-suite rigor audit `devdoc/test-code-discipline-audit.md` §2.2
(8 unchecked `os.Chdir` restores). Severity: low, cascade-shaped.

## The defect

`os.Chdir` returns an error. In **statement position** — `defer os.Chdir(oldwd)`,
`t.Cleanup(func() { os.Chdir(oldwd) })`, or a bare `os.Chdir(dir)` — there is
no `err :=` and no visible `_ =`, so the result is discarded with nobody's
consent (Rule 15b).

Why this shape is worse than an ordinary swallowed error: `os.Chdir` mutates
PROCESS state that every later test in the same binary inherits. If the
**restore** back to the original directory fails, the rest of the run executes
inside a `t.TempDir()` the framework has already removed — the cascade that
follows (relative-path opens, `go build .` in the wrong tree) names everything
except the real cause.

## Sites fixed (8)

| Site | Before | After |
| --- | --- | --- |
| `internal/tools/search_test.go:26-27` (`chdirTemp` helper) | `os.Chdir(dir)` + `t.Cleanup(func() { os.Chdir(oldwd) })` | `t.Chdir(dir)` (helper already takes `*testing.T`) |
| `internal/agent/loop_test.go:519` | `defer os.Chdir(oldwd)` | `t.Chdir(oldwd)` (the in-test `os.Chdir(dir)` keeps its explicit `t.Fatalf` check) |
| `cmd/agent/cli_tasks_merged_cleanup_test.go:134` | `defer os.Chdir(wd)` | `t.Chdir(wd)` |
| `cmd/agent/cli_resume_worktree_test.go:99,117,134,160,183` (5 sites) | `defer os.Chdir(startWd)` | `t.Chdir(startWd)` |

Every site had a usable `*testing.T` in scope, so `t.Chdir` was applicable at
all 8 — the smallest correct change. `t.Chdir` registers its own restore and,
per the stdlib, **panics** if the process cannot get back to the original
directory (`testing.Chdir: <err>`), so a restore failure can never be silent.
It also marks the test non-parallel-safe, which was already true for all of
them (`t.Parallel` is used nowhere in these four files).

The two sibling sites the audit named as the correct model were deliberately
**not touched**:
`internal/tools/read_test.go:48-51` (visible `_ = ` discard) and
`internal/tools/memory_sync_task_gitpushback_cov_test.go:73-76` (checked
`t.Cleanup`).

## The gate

The repo already runs its style sweep inside `go test` as a ratchet
(`internal/stylegate`, `TestStyleGate` with per-rule ceilings). The new rule
`test-chdir` lives there — same machinery, no new tool:

- **Rule**: `stylegate.RuleTestChdir = "test-chdir"`.
- **Predicate** (`(*visitor).checkChdirStmt`, AST, no regex): a call to
  `os.Chdir` (resolved through `types.Info` + the `os` package path, so an
  unrelated `chdir` helper is not a hit) that sits in statement position —
  `ExprStmt`, `DeferStmt` or `GoStmt` — in a `_test.go` file. Assignments
  (`err := os.Chdir(d)`, `_ = os.Chdir(d)`) are handled elsewhere in the same
  scanner (`ignored-error`), so the two rules do not double-report.
- **Ceiling**: `{true: 0}` — no baseline. All 8 known sites were fixed in this
  same change, so the rule is red on any new silent cwd restore.
- **Escape**: none needed. The legitimate forms are `t.Chdir` and the explicit
  checked call; a statement-position restore cannot be justified by a comment
  (the point of the rule is that the error is unobservable there).

Red-proof (negative + positive discrimination, one synthetic fixture in
`TestChdirRuleFlagsStatementPositionChdir`):

- flagged (3): `defer os.Chdir(oldwd)`, `t.Cleanup(func() { os.Chdir(dir) })`,
  bare `os.Chdir(t.TempDir())`.
- spared (4): `t.Chdir(...)`, `if err := os.Chdir(...); err != nil`, the
  non-test helper `func(d string) error { return os.Chdir(d) }`, and
  production code.

## Not done / pre-existing (not caused by this change)

- **`unchecked-close` is at 33 hits against ceiling 32 on master itself.** The
  stylegate ratchet was already red before this change (reproduced in an
  untouched `~/work/GoPi` checkout: same 33/32 message), so
  `go test ./internal/stylegate/` fails on the baseline, not on anything here.
  The 33rd site arrived with a merge and its owner has not re-baselined; this
  task's diff adds no bare `Close()`. Recorded in the report rather than
  silently raised — a ceiling is a reviewed number, not a rubber stamp.
- Production `os.Chdir` is out of scope: its only non-test site
  (`cmd/agent/cli_resume.go:310`) already uses the checked `err :=` form.
- No conversion of `t.Chdir`-adjacent `t.Setenv` sites; unrelated (that is the
  `test-setenv-scope` rule's territory).

## Verification

```
gofmt -l internal/stylegate/                              : clean
go vet ./internal/stylegate/                              : OK
go build ./...                                            : OK
go test -count=1 ./internal/stylegate/                     : ok (TestStyleGate ratchet green at ceiling 0)
go test -count=1 -run TestChdirRuleFlags ./internal/stylegate/ -v : PASS
go test -count=1 ./internal/tools/ ./internal/agent/ ./cmd/agent/ : see task report
```

Commit: see `tasks/` report / `git log fix/chdir-restore-check`.
