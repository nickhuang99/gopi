<!-- doc-class: historical -->
# Task Report: merge resolution — `fix/task-list-address-no-scan` ← `master`

Merge commit: `d447b61f` (parents `15d9d217` = branch tip, `6bc0dfeb` = master).
Worktree: the resolver's linked worktree (shared object store — no push needed).

## Completed

- Reproduced the merge with `git merge master --no-commit --no-ff` → two
  conflicts: `internal/tools/tasks.go` (UU, 8 hunks) and
  `devdoc/task-list-inprogress-scan-20260914.md` (AA).
- Read all three stages (`:1:` base / `:2:` ours / `:3:` theirs) before choosing
  any side, then re-keyed the resolution on the MERGED tree (memory #2013: a
  dropped hunk can delete identifiers referenced outside the markers).

### `internal/tools/tasks.go` — the deep-scan accessor

Both sides solved the same cost problem; the conflict is which accessor the
**deep scan** calls.

- `walkSessionTreeRecords` (HEAD) is **absent from master's tree** — HEAD is the
  side that diverged (it never received master's `54cdf94a`/`0618a2cd`, which
  renamed the accessor to `taskSessionRecordsInDirs`). Master's name is therefore
  the merged tree's binding, and the later master commits (`5552a631`,
  `c8934ca2`) build on it.
- HEAD is the side carrying the **new address** work: `taskSessionDirHints` +
  `internal/tools/taskledger.go` (ledger-named directories), the
  `taskFindSessionJSONLDeepOnly` split, `sessionBareRecordPath` /
  `taskRunRecordPath`, and the readdir-order (byte-sorted) precedence doc.
- Resolution: **HEAD's structure + master's accessor name** — every deep-pass
  accessor (`taskFindSessionJSONL`, `taskFindSessionJSONLDeepOnly`) now iterates
  `taskSessionRecordsInDirs(dir)`; the ledger-hint probe, the row-address
  helpers and HEAD's precedence comments are kept. Duplicate definitions are
  collapsed to one (the un-taken side's copies were discarded, not left behind).
- Why the deep pass keeps master's plain accessor rather than HEAD's memoised
  `taskSessionTreeRecords`: the deep pass is the SCAN fallback, defined by
  readdir (byte-sorted) order over the raw tree. HEAD's memo maps a session id
  **by value, not by directory name**, and can be re-keyed by a counting
  stand-in, so a fallback built on it can resolve a file the real scan would
  never reach — exactly the behaviour-identity the branch's own comments
  require. With it, all three of master's cost guards
  (`TestFindSessionJSONLFlatExactWinsInOnePass`,
  `…ForRowPrefersRowPathsOverScan`, `TestSessionTreeScanStaysTheFallbackAndIsMemoised`)
  pass, and memory #1970's cross-file rule holds: no identifier a dropped hunk
  referenced is left undefined (`go build ./...` proves it).
- HEAD's "resolved with TWO stats" comment is kept **literally true**: master's
  inlined deep pass is retained as written, so the wording still describes the
  code (a three-stat variant would have required the HEAD-only helper).
- `session.` `sync` import stays — HEAD's memo/seam/test hook remain in use.

### `devdoc/task-list-inprogress-scan-20260914.md` — add/add on the head

Master's copy carries the tool-added `WORKER-OWNERSHIP: OPERATOR-ONLY` banner
(a doubly appended copy); HEAD's copy carries the same banner-free report.
Resolution: HEAD's single title plus **one** banner copy — the banner's intent
survives, the duplication does not.

## Not Done

- Did not rename `taskSessionDirHints` (Rule 12.2 applies to *dispatched-row id*
  params; it is a session id). Reported rather than silently changed.
- No `go test ./...` — waived by the epic (known pre-existing full-suite hang);
  bounded checks only.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — OK (first compile check after resolution)
- `gofmt -l internal/tools/tasks.go` — clean; `git diff --check` — clean
- Conflict markers: `grep -c -E '^(<<<<<<<|>>>>>>>|=======)'` → 0 in both files;
  `git status --porcelain` — clean; no `MERGE_HEAD`
- `go test ./internal/standards/ -count=1` — **ok** (the machine gates are
  line-keyed, so this also proves no baseline key was invalidated by the merge)
- `go test ./internal/tools/ -run 'TestRowAddress|TestLedgerNamedDir|TestSessionDirHints|TestFindSessionJSONLFlat|TestTaskFindSessionJSONLForRow|TestSessionTreeScan' -count=1`
  — **ok** (1.04 s) — the merged cost/address contract is green

### Pre-existing redness (measured, NOT introduced by this resolution)

`TestTaskFindSessionJSONLStillHonoursEveryLayout` (master's `tasks_session_resolve_cost_test.go`)
is **flaky at the branch tip before this merge**: 6× `-run` against a pristine
`git archive 15d9d217` snapshot → 4 PASS / 2 FAIL; in the merged tree 4/8 FAIL.

Root cause (mechanism, not inference): the test writes the `bare` layout
(`<root>/<session-id>/session.jsonl`) and then the `flat` layout
(`<root>/<session-id>.jsonl`, a regular file at the root). The scan resolves by
readdir (byte-sorted) order, where `"<session-id>"` and `"<session-id>.jsonl"`
**tie** — the container sorts first in either order — so the current resolver can
return the bare file while the test expects the flat one. The test asserting a
deterministic `want` for two different layouts with the same sort key is hostile
to the raw readdir the resolver is defined by; HEAD's ledger-hint pass also
changes the path taken to the fallback. This is a resolver-input question, not a
merge artifact — fixing the flake is a follow-up for the branch owner, not part
of the resolution.

## Files

- `internal/tools/tasks.go`
- `devdoc/task-list-inprogress-scan-20260914.md`
- `devdoc/merge-resolve-task-list-address-no-scan.md` (this report)
