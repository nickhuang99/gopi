<!-- doc-class: normative -->
# Merge resolution: fix/rule17-env-escape-annotations → master

Resolution merge commit on branch `fix/rule17-env-escape-annotations`
(parents: branch tip `62902c12` + master `0cfb618c`).

## Conflict hunks (2 files, both `_test.go`, both the SAME shape)

The branch rewrote two `os.Setenv`/`os.Unsetenv` sites into prose-comment-block
escapes (`// env-leak-ok:` on the comment block ABOVE the statement); master
landed the same sites with the marker INLINE on the statement line.

| file | branch side | master side | resolution |
|---|---|---|---|
| `cmd/agent/cli_voice_review_test.go` (`os.Unsetenv("HOME")`) | 4-line reasoning block above | inline `// env-leak-ok: …` | **both** — block kept, master's inline marker appended to the statement |
| `cmd/agent/worktree_release_accept_test.go` (`os.Setenv(tools.TaskWrapperEnv, row)`) | 5-line reasoning block above | inline `//stylegate:ignore … // env-leak-ok: …` | **both** — block kept, master's inline markers appended |

Both sides are meaningful and neither is dropped: `internal/standards` (gate 2)
accepts `// env-leak-ok:` on the offending line OR at the end of the comment
block above (`linesAbove`), and `internal/stylegate` Rule `test-setenv` requires
the marker anywhere in the offending statement's span (`reasonOverSpan`). The
combined form satisfies both — verified by running both gates (below).

## The resolution's own tail: line-keyed baseline re-key (gate 2)

`git merge` auto-merged `internal/standards/baseline.go`, but that file is a
**line-keyed permit list** and master's keys were stale for the merged tree:
master shifted `internal/tools/task_deliverable_branch_test.go` and
`internal/tools/tasks_test.go` above the `os.Unsetenv` pairs and landed the keys
**without** the shift (138/139/1103), so after the merge the real sites are
144/145/1133 → 3 `NEW` + 3 `STALE`. Keys were **re-derived from the merged tree**
(`grep -n 'os.Unsetenv'`), not copied from either side, and the re-key carries a
`// merge-note:` explaining it at the list (memory #1970's line-key class).

Two pre-existing `unchecked-close` hits in `cli_voice_review_test.go` (lines
`wildcard.Close()` / `conn.Close()` — the branch's file, absent from master)
were also reported by `internal/stylegate`'s ceiling 0; both are probe
handles closed right after the accept/dial they test, so they gained the
required reasoned `//stylegate:ignore <reason>` escapes.

## Not Done

- Nothing from either side was dropped: the branch's other annotated Rule-17
  sites (`internal/agent/testmain_isolation_test.go` etc.) live in files the
  merge auto-merged, and they still carry their escapes (gate 2 reports 13
  escaped sites, unchanged from the branch's green state).
- No full-suite run (`go test ./...`) — waived by the epic (known pre-existing
  hang).

## Verification

- `go build ./...` — OK (exit 0)
- `go vet ./cmd/agent/ ./internal/standards/ ./internal/stylegate/` — OK
- `go test ./internal/standards/ -run 'TestStandardGates|TestStandardGateDataReport' -count=1` — ok (was FAIL before the re-key)
- `go test ./internal/stylegate/ -count=1` — ok (was FAIL before the two reasons)
- `go test ./cmd/agent/ -run 'TestUserHomeDirOrDefaultFallsBack|TestReleaseOwnWorktree|TestOwnWorktree' -count=1` — ok
- `grep -rn '^<<<<<<<\|^>>>>>>>\|^=======$'` — 0 hits; `git diff --diff-filter=U` — 0 unmerged; MERGE_HEAD — absent; `gofmt -l` — clean
