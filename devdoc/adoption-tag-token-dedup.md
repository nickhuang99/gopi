<!-- doc-class: historical -->
# Task Report: ⑤ same-branch-once dedup matched a tag SUBSTRING (mid-token false positive)

Branch: `feat/adoption-tag-token-dedup` (renamed from `fix/…` to match the
epic's `feat/` deliverable; the commits below predate the rename) ·
worktree `~/workspace/gopi-adopt-token`
Base: master `51323ed5`

## Problem

`internal/tools/task_adoption.go:adoptionConflictTaskLive` — the ⑤
same-branch-once guard of the branch-adoption chain (③b must dispatch exactly
ONE conflict-resolution task per branch) — selected its candidate rows with a
raw substring match:

```sql
WHERE tags LIKE '%merge-conflict%'
  AND payload LIKE '%branch <name> (tip %'
  AND status IN ('pending','claimed','in_progress')
```

Every other tag consumer in the package matches at TOKEN level (comma-anchored
`(','||tags||',') LIKE '%,tag,%'`, memory #1961). A tag that merely CONTAINS
`merge-conflict` mid-token (`merge-conflictz`, `xmerge-conflict`) therefore made
the guard report a LIVE conflict task for a branch that had none — and ③b then
SKIPPED dispatching the conflict task the branch actually needed, so a
conflicted branch could never be resolved and never be adopted. The payload half
carried the same class of defect as a duplicated literal: `'%branch <name>
(tip %'` was hand-written in the query while the writers rendered `"branch %s
(tip %s)"` from `taskMergePayloadBranchLine` — a needle/text pair with nothing
forcing them to agree.

**Latent today, filed for the record.** Measured divergence on the live ledger
(`~/.gopi/memory.db`, 2026-09-12): raw LIKE 27 rows, GLOB token 27 rows → 0
extra rows today, because nothing in the tree writes a mid-token variant. The
bug activates the moment any producer writes a tag like `merge-conflict-<x>` (a
one-character rename, a namespaced tag) — and it then fails SILENTLY (a skipped
dispatch, no error, no log line).

## Completed

- **One shared definition of "this row's tags carry this tag as a whole token"**
  — `taskTagTokenSQLTest(tag)` in `internal/tools/task_adoption.go`:

  ```go
  func taskTagTokenSQLTest(tag string) string {
      return "(','||COALESCE(tags,'')||',') GLOB '*," + tag + ",*'"
  }
  ```

  Comma-padding tests MEMBERSHIP instead of CONTAINMENT. **GLOB, not LIKE**:
  SQLite's `LIKE` is case-INSENSITIVE, so a `%,tag,%` LIKE would match
  `Merge-Conflict` — which the package's Go token readers
  (`resolveRowIsMergerOwned`, `isTaskResolved`) reject by exact comparison. GLOB
  is case-sensitive, i.e. it agrees with Go on case as well as on boundaries.
  It closes over the caller's existing SQL text, so each conversion site stays a
  one-line change.
- **`adoptionConflictTaskLive` rewired** onto that fragment. The branch name is
  now a bound PARAMETER (`payload LIKE ?`) instead of interpolated text — caller
  text belongs in the value channel. The payload needle is derived from the
  shared writer const.
- **Payload marker has one definition** — `internal/tools/task_merge.go`:

  ```go
  taskMergePayloadBranchPrefix = "branch %s (tip "     // the TIP-OPEN head
  taskMergePayloadBranchLine   = taskMergePayloadBranchPrefix + "%s)"  // full line
  ```

  The two briefing WRITERS now render from `taskMergePayloadBranchLine`
  (`adoptionDispatchConflict`, `taskMergeConflictPayload`) and the ⑤ reader
  derives its needle from `taskMergePayloadBranchPrefix`, so needle and written
  text cannot drift. `taskMergePayloadBranchLine` is unchanged in value
  (`"branch %s (tip %s)"`) — the prefix split is what makes it shareable.
- **`taskLiveStatusSQLList()`** in `internal/tools/tasks.go`: the non-terminal
  status set as an SQL literal list, built from `taskStatusPending/Claimed/
  InProgress` — the inline `'pending','claimed','in_progress'` is gone (Rule 1b,
  same rendering shape as the existing `taskSchemaStatusCHECK`).
- **Regression tests** (`internal/tools/task_adoption_dedup_token_test.go`,
  new file, table-driven):
  - `TestAdoptionConflictDedupMatchesTagTokenNotSubstring` — 11 tag shapes
    through the REAL gate (`adoptionConflictTaskLive`), cross-checked against
    the package's existing Go token reader: real token alone / first / last in a
    comma list DO satisfy the dedup; `merge-conflictz`, `xmerge-conflict`,
    `auto-dispatch,merge-conflictzz` do NOT; `Merge-Conflict` does NOT (the
    GLOB choice); empty tags do NOT.
  - `TestAdoptionConflictDedupIgnoresOtherBranches` — payload half, using the
    writer-rendered line.
  - `TestAdoptionConflictDedupIgnoresTerminalRows` — only non-terminal rows are
    "live" (both directions asserted).
  - `TestTaskTagTokenSQLTestIsTokenScoped` — the shared fragment asked directly.
  - `TestMergeConflictTagHasOneLiteralAndOneTokenMatcher` — the Rule 13
    anti-regression gate: `"merge-conflict"`, `tags LIKE '%` and `payload LIKE '%`
    may occur in NO production `.go` file of the package except the one holder.
- **Test helpers moved onto the shared definitions**
  (`internal/tools/task_adoption_test.go`): the two ad-hoc
  `tags LIKE '%merge-conflict%' AND payload LIKE '%branch …'` queries were the
  copy-paste carriers of the same idiom; without this they would have pinned the
  OLD shape. Also switched to `?` binding.

## Not Done

- `internal/tools/verify_gate.go:208` (`merge_verify_fail_count`) still uses
  `tags LIKE ?` with `"%"+MergeVerifyEvent+"%"`. That is a deliberately loose
  probe over an EVENT name the same package writes (its own mixed-token
  convention), NOT the merge-conflict token, and re-scoping it changes merge
  retry/threshold behaviour — out of this epic's blast radius, filed here for the
  record as the remaining member of the class.
- The same `tags LIKE '%…%'` shape in reporting surfaces
  (`internal/tools/tasks.go` bug-population queries) is the subject of the
  separate fix already on `fix/bug-tag-population-token-match`; not touched.
- `task_merge.go`'s own live-row dedup keys on an exact `summary = ?` match
  (already token-exact) — left as is.
- The payload needle is still a `LIKE ?` pattern, so a branch name carrying LIKE
  metacharacters (`_` or `%` — `*` is not a wildcard for LIKE) could in principle
  match a DIFFERENT branch's rendered line: `feat/a_b` matches a row whose
  payload says `branch feat/axb (tip `. Pre-existing (the old query interpolated
  the same text into `payload LIKE '%branch …'`), two branches differing only by
  such a character away from a live conflict row, and NOT what this epic is
  about — the tag half is the false-positive that skips a dispatch. Making the
  payload half exact would mean matching on a `substr()`/equality of the
  rendered line, which changes the ⑤ guard's contract, so it stays filed here
  rather than smuggled into this fix.

## Verification

- **Build**: `go build ./...` → OK (`gofmt -l internal/tools/` empty, `go vet
  ./internal/tools/` clean).
- **RED PROOF** (the regression test can actually fail; gate temporarily
  reverted to `tags LIKE '%merge-conflict%'`, run, then restored):

  ```
  --- FAIL: TestAdoptionConflictDedupMatchesTagTokenNotSubstring (0.71s)
      --- FAIL: .../mid-token_suffix_must_not_match (0.08s)
          task_adoption_dedup_token_test.go:149: tags "merge-conflictz": adoptionConflictTaskLive = true, want false
      --- FAIL: .../mid-token_prefix_must_not_match (0.03s)
          task_adoption_dedup_token_test.go:149: tags "xmerge-conflict": adoptionConflictTaskLive = true, want false
      --- FAIL: .../mid-token_embedded_in_a_list_must_not_match (0.06s)
          task_adoption_dedup_token_test.go:149: tags "auto-dispatch,merge-conflictzz": adoptionConflictTaskLive = true, want false
      --- FAIL: .../different_case_must_not_match_(GLOB,_not_LIKE) (0.05s)
          task_adoption_dedup_token_test.go:149: tags "Merge-Conflict": adoptionConflictTaskLive = true, want false
  FAIL	github.com/nick/gopi/internal/tools	1.003s
  ```
  The revert + restore was re-run at closing (not only recorded once):
  after restore `git diff --stat internal/tools/task_adoption.go` is EMPTY
  (byte-identical to the commit) and the focused run is green — `ok
  github.com/nick/gopi/internal/tools 1.784s`. Note the RED run also confirms
  the four pre-existing adoption-chain acceptance tests are NOT what catches
  this: they pass under both queries; only the new mid-token table does.
- **Tests**: `go test ./internal/tools/ -run 'TestAdoption|TestTaskTagTokenSQLTest|TestMergeConflictTagHasOneLiteral' -count=1 -v`
  → all PASS (11 sub-cases in the token table + the payload/terminal/fragment
  tests + the six pre-existing adoption-chain tests: clean adopt / conflict
  dispatch / re-tick idempotency / orphan skip / hunk extraction / sync gate).

  The Rule 13 gate was red-proved too: appending `var probeMidTokenTag =
  "merge-conflict"` to `internal/tools/task_adoption_conflict.go` fails it with
  `task_adoption_conflict.go carries "\"merge-conflict\"" — reference
  taskTagMergeConflict (defined in task_adoption.go)`; removing the probe makes
  it green again.
- **Full package**: `go test ./internal/tools/ ./internal/standards/ ./internal/stylegate/ -count=1`
  (background job — the `internal/tools` suite alone is ~10 min, memory #1973):

  ```
  ok  	github.com/nick/gopi/internal/tools	586.935s
  ok  	github.com/nick/gopi/internal/stylegate	6.923s
  FAIL	github.com/nick/gopi/internal/standards	12.292s   ← first run, see below
  ```
  The standards failure was NOT a new violation: the fix inserted lines in two
  files (`task_adoption.go` +25 above the ⑤ dedup call, `task_merge.go` +7 in the
  const block) and that gate keys its baseline on `file:LINE`, so three
  PRE-EXISTING legacy entries reported as "stale" while their sites reported as
  "new" — same sites, same reasons, moved keys. Re-keyed in the follow-up commit
  `a09fed48` (no entry added, no gate weakened, reason comments on each). After
  it: `go test ./internal/standards/ -count=1` → `ok … 18.189s`.
- **Ledger divergence probe** (`sqlite3 -readonly ~/.gopi/memory.db`):

  ```
  LIKE_substring|27
  GLOB_token|27
  ```
  → divergence 0 today: the defect is latent, exactly as filed.

### Closing re-verification (post-rename branch, HEAD `b2228bba`)

Run in one pass at the end of the task, after every commit landed and the
branch was renamed `fix/…` → `feat/adoption-tag-token-dedup`:

```
git log --oneline -1                      b2228bba docs(adoption): record the full-suite result, …
CGO_ENABLED=0 go build ./...              build OK
go test ./internal/tools/ -run 'TestAdoptionConflictDedup|TestTaskTagTokenSQLTest|TestMergeConflictTagHasOneLiteral|TestAdoption' -count=1
                                          ok  github.com/nick/gopi/internal/tools  4.896s
go test ./internal/standards/ -count=1    ok  github.com/nick/gopi/internal/standards  12.046s
go test ./internal/stylegate/ -count=1    ok  github.com/nick/gopi/internal/stylegate  8.587s
```

The branch rename is refs-only (same commits), so the verification above
covers the delivered SHA. Deliverable recovery from the MAIN repo:
`git -C ~/work/GoPi show feat/adoption-tag-token-dedup:devdoc/adoption-tag-token-dedup.md`.

## Recipe (reusable)

1. The tag column is comma-separated free text; the ONLY correct membership
   test is comma-padding — `(','||COALESCE(tags,'')||',') GLOB '*,tag,*'`.
2. Use GLOB, never LIKE, whenever an SQL tag test must agree with a Go token
   test: SQLite's LIKE case-folds, Go's `==` does not (memory #1961 step 2).
3. When a query needs a substring of a string another function RENDERS, do
   not copy-paste the substring: split the write format into a PREFIX const and
   derive both the rendered line and the reader's needle from it.
4. Prove the fix by reverting it: the table-driven test must go RED on the old
   query (4 sub-cases above) and GREEN on the new one.
