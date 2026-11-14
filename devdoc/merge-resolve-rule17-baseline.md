<!-- doc-class: normative -->
# Merge Resolution: fix/rule17-baseline-from-master ← master

## What conflicted
`git merge master --no-commit --no-ff` (branch `fix/rule17-baseline-from-master`,
worktree `/tmp/gopi-merge-resolve-1292415986/wt`) produced exactly ONE conflicted
file: `internal/standards/baseline.go`, one hunk (lines 111–135 of the conflicted
file) inside `testEnvBaseline` — the Rule 17 allow-list.

Both sides had *rewritten the same region* of the list (the disputed region is the
`internal/agent/*` entries around `rawlog_test.go` / `taskendstore_test.go` /
`taskstartsearch_test.go` / `testmain_isolation_test.go`), so git could not merge
them textually:

- **ours (HEAD, branch `6d12ffd`)**: re-pointed
  `testmain_isolation_test.go:62-65` → `:76-79` after master's own `6093c76`
  moved those test sites (4 line-keyed permits), and kept
  `taskendstore_test.go:83/84` + `taskstartsearch_test.go:93/94/125/126`. The
  `rawlog_test.go` entries were reformatted onto two per-line pairs.
- **theirs (master `9583fe9`)**: **deleted** the four
  `testmain_isolation_test.go:76-79` entries outright — it turned those four
  suite-pin env sites into reasoned inline `// env-leak-ok:` escapes (suite
  init before `m.Run`, so `t.Setenv` is structurally unavailable; spawned
  children read `GOPI_MEMORY_DB` at exec time), and one entry per line.

## Resolution
`testEnvBaseline` now holds **master's entry set**, in the one-entry-per-line
spelling (kept from the region's surrounding sorted style):

- The four `testmain_isolation_test.go:76-79` entries are **omitted on purpose** —
  taking master's accounting change (escape channel) is the whole point of this
  branch, and re-adding the line-keyed permits would re-introduce exactly the
  stale permits master retired in `9583fe9`.
- The entries this side carried that master lacks (`taskendstore_test.go:83/84`,
  `taskstartsearch_test.go:93/94/125/126`) are **not** silently dropped by the
  resolution: those env sites no longer exist in the merged tree at all (master's
  branch converted them to `t.Setenv` — `git grep` finds no `os.Setenv` left in
  either file), so keeping them would be a **STALE** entry, which the gate
  reports as an error.
- A `merge-note:` comment block records this reasoning at the conflict site, so a
  later reader does not "restore" the deleted lines as a lost side.

No other hunk was resolved by deletion; nothing else of ours/theirs was dropped.

## Verification (bounded — full suite deliberately not run)
- `grep -rn '^(<<<<<<<|=======|>>>>>>>)'` over the tree → **0 markers**
- `gofmt -l internal/standards/baseline.go` → clean; `git diff --check` → clean
- `CGO_ENABLED=0 go build ./...` → **exit 0**
- `go test ./internal/standards/ -run 'TestGate2|TestStandardGate|TestGate' -count=1`
  → **ok github.com/nick/gopi/internal/standards 14.162s** (Rule 17 gate green:
  no NEW, no STALE — the accounting of the resolved list is internally consistent)

## Commit
tip of branch `fix/rule17-baseline-from-master` — `resolve merge
fix/rule17-baseline-from-master into master` (this same commit; the trailing
`--amend` that added this devdoc is why no pinned hash is quoted)
(branch `fix/rule17-baseline-from-master`; worktree shares the main repo's object
store, so no push is needed).
