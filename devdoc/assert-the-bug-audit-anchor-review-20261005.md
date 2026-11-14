<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Anchor work: 1 new drift + 2 stale permits — the doc-anchor gate

**Task:** `01a10af8-523e-73ef-892b-4d5c3c993fb4` (branch
`task/01a10af8523e73ef892b4d5c3c993fb4`, worktree
`/home/user/workspace/gopi-01a10af8523e73ef892b4d5c3c993fb4`)
**Gate:** `internal/agent/docanchor_test.go` — `TestDocAnchorsResolveInNormativeDocs`,
manifest-scoped over the `normative` rows of `devdoc/DOC-INDEX.tsv` (636 markdown docs).
**Tree measured:** master tip `e02538fa` (this run's branch == master at start,
`git status --porcelain` empty).
**Epic verdict:** DOABLE — 1 new drift + 2 stale permits, all repairable inside one session.

## 1. The measured starting state (observed, not carried over from the briefing)

The briefing attributed the red to past-EOF anchors in
`devdoc/assert-the-bug-audit-20260917.md`. Measurement says otherwise: that document's
15 rows all fire as `BASELINE DRIFT` — logged, not errors — and are not in the count.

```
$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1     # BEFORE, exit=1
    docanchor_test.go:203: devdoc/requirements/test-infrastructure.md: anchor
        internal/standards/flake_registry_gate_test.go:146 points at a BLANK line of
        …/internal/standards/flake_registry_gate_test.go — the anchor drifted off its symbol
    docanchor_test.go:223: stale baseline entry
        "devdoc/requirements/cli.md|blank|cmd/agent/cli_contracts_test.go:363" (…) — the site was
        fixed or is no longer gated; delete the entry from internal/agent/testdata/docanchor-baseline.tsv
    docanchor_test.go:223: stale baseline entry
        "devdoc/requirements/cli.md|blank|cmd/agent/cli_completion.go:151" (…) — (the same sentence)
    docanchor_test.go:227: doc anchors verified: 2948 across 636 normative docs (2446 explicit
        + 502 bare continuations; 603 bare out of row/table/heading scope; 242 pre-existing drift
        entries permitted, 1 new, 2 stale permits)
```

The red is pre-existing on master (the worktree branched at the tip and nothing in the
tree was changed before this measurement).

## 2. Completed

- **Re-keyed the one NEW drift.** `devdoc/requirements/test-infrastructure.md` §7.5 cited
  `internal/standards/flake_registry_gate_test.go:146` for
  `TestFlakeRegistryMatchesTheTestsItNames`; that line is blank at the tip. The row's own
  text names the symbol, so the anchor is re-keyed to the symbol's real line, `:174`.
- **Re-keyed two further anchors of the same document that a same-file sweep re-measured** —
  the gate resolves them today, but both had drifted off their symbol, which is exactly the
  silent mis-citation the gate cannot see (its rule: the line is an accelerator, the symbol
  is the authority):
  - `:405` → `:438` for `TestFlakeCatchMechanismStaysOffTheSmokePath` (cited twice: §7.5's
    table row and the §8 "F3 — nothing schedules the off-peak run" row).
- **Deleted 2 stale permits** from `internal/agent/testdata/docanchor-baseline.tsv`:
  `devdoc/requirements/cli.md|blank|cmd/agent/cli_contracts_test.go:363` and
  `devdoc/requirements/cli.md|blank|cmd/agent/cli_completion.go:151`. Permit total
  **242 → 240** — the list only ratcheted down; nothing was added or re-pointed.
- **Decision on the briefing's named doc,** `devdoc/assert-the-bug-audit-20260917.md`:
  **stays permitted, not re-keyed** — it is a landed record whose line map is an artifact of
  the head it measured (§4 below).

## 3. The drift, at its cause

`internal/standards/flake_registry_gate_test.go` grew 832 → 1367 lines after the document's
own revision was read, and the function moved with it:

| rev | `TestFlakeRegistryMatchesTheTestsItNames` | the `:NNN` the row cites |
|---|---|---|
| `32932668` (the document's own `Revision read`) | `:144` | `:146` (`rows := flakeRegistryRows(t, root)`, inside the function) |
| `bd4c2bef` (the previous anchor repair) | `:146` | `:148` |
| `e02538fa` (this run) | `:174` | `:176` |

`bd4c2bef` re-keyed the row `:144` → `:146` — correct at its own tip, where `:146` WAS the
function — and the line went blank as the file grew further. The row's claim ("both
directions; the requirement (N2) adds the RACE class it deliberately refuses to hold") is a
standing claim about a live test, not a dated measurement, so the repair is the gate's own
designed one: **re-key by symbol**. The document's header (`Revision read: master 32932668`)
does not date-pin its anchors — the document says the opposite in the next sentence
(*"this document's review surface is `32932668..HEAD`, not `master..HEAD`"*) — and the gate's
own prior repair already re-keyed this very anchor, which settles the classification:
normative and re-keyable.

## 4. The 2 stale permits, at their cause

Both rows were added by `53635229` (master `2ec0c45b` era), when both cited lines WERE the
blank seams the permits excused:

| permit key | line at `53635229` | line at `e02538fa` |
|---|---|---|
| `cmd/agent/cli_contracts_test.go:363` | the drift that justified the permit (the block was later rewritten; the file went 706 → 851 lines, and `TestCLIContractEachSubcommandRoutesToItsHandler` now starts at `:491`) | `\t}` — a closing brace, not a blank line |
| `cmd/agent/cli_completion.go:151` | blank | `var memoryCompletionNames = memorySubcommandNames` (the file went 342 → 362 lines) |

The gate's blank test is `strings.TrimSpace(line) == ""`
(`internal/agent/docanchor_test.go:713`), so neither site can fire the finding any more.
Neither is a doc error: the *permits* were the residue. Per the gate's design a permit whose
finding no longer fires must be **deleted, never re-pointed** (Rule 13's ratchet) — done.
This is also why the deletion is not "re-baselining in reverse": nothing was written to
restore a permission.

## 5. The briefing's named doc — `devdoc/assert-the-bug-audit-20260917.md`: stays permitted

All 15 baseline rows for this document are `eof` (past-EOF), over two files:
`internal/tools/cov_finish_cleanup_test.go` (14 keys) and
`internal/tools/is_server_node_test.go` (1 key). Both were **created by that audit's own
epic** and have been rewritten since. Its anchors are correct-not-to-re-key, for a reason
the gate's own key proves:

- The row's numbers are anchors in one of the audit's **bare-continuation tables** (§2e,
  `| line | reason | tests naming it | verdict |`, headed `internal/tools/task_finish_cleanup.go`,
  26 sites). Verified at the head the audit measured (`69975030`):

  ```
  $ git show 69975030:internal/tools/task_finish_cleanup.go | wc -l
  2542
  $ git show 69975030:internal/tools/task_finish_cleanup.go | sed -n '767p;1938p;1975p'
  		rep.Skipped = true
  			rep.Skipped = true
  		rep.Skipped = true
  ```

  Those are the **skip sites the table is about**, at exactly the lines the table lists —
  the table is a correct measurement of `69975030`, and the file is 4684 lines today.
  Re-keying those numbers would assert that the audit measured today's tree, which is
  false: the row would be **falsified, not repaired**.
- `is_server_node_test.go:904` did not exist at `69975030` at all
  (`git show … → fatal: path … exists on disk, but not in '69975030'`) — it is the audit's
  own `t.Skip` cell (§4.2), a dated state of a file that epic introduced.
- The document IS normative (the manifest cites it from
  `internal/tools/is_server_node_test.go:15` for its retry finding), so it stays gated and
  stays permitted with its existing reason (*"a landed record whose anchors this change
  deliberately does NOT rewrite"*). **No permit row was added, changed or removed for it.**

Honest residue, for a future task if the operator wants it: those 15 rows could be refreshed
as a **dated appendix** (a new section recording the skip-site line numbers at each rev)
rather than by overwriting the table — an editorial change to a landed ruling, not an anchor
repair, and out of scope here.

## 6. Files changed

| file | change |
|---|---|
| `devdoc/requirements/test-infrastructure.md` | 3 anchors re-keyed by symbol (`:146`→`:174`; `:405`→`:438` twice) |
| `internal/agent/testdata/docanchor-baseline.tsv` | 2 stale permit rows deleted (none added) |
| `devdoc/assert-the-bug-audit-anchor-review-20261005.md` | this report |

## 7. Verification

- **Build:** `go build ./...` → `BUILD_ALL_OK` (exit 0);
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0.
- **Tests:** `go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1`
  → `ok github.com/nick/gopi/internal/agent 5.325s` (before the repair: exit 1, FAIL — §1).
  `go vet ./...` → exit 0.
  `make test-budget PKGS='./internal/agent/ ./internal/standards/'` → **exit 1**: `packages: 2
  finished, 1 with a failure` — `FAILED: github.com/nick/gopi/internal/standards`, whose three
  failures are all the ONE pre-existing Rule 18 hit at
  `internal/tools/chat_write_guard.go:839`
  (`TestStandardGates/TestGate3ElseAfterReturnRedProof`, `TestStandardGates` and
  `TestStandardGateDataReport`, the last two reporting it as `NEW=1`). **Master's own red,
  not this change's** — the evidence that it is not mine, and the one that it is master's:
  every file this change touches is a `devdoc/` markdown file or the permit `.tsv`
  (`git diff master --name-only` lists exactly those four), the offending file is not among
  them, and the gate run on the pristine tip produced the same failure in this session:
  `go test ./internal/standards/ -run 'TestStandardGates/TestGate3ElseAfterReturnRedProof'
  -count=1` in the worktree (`== e02538fa`, clean at start) → `exit 1`, same
  `NEW violation … chat_write_guard.go:839`. `git blame` puts the line at
  `a8bf8ce2 feat(chat): allow a chat to write and commit devdoc/` (2026-10-05), an ancestor of
  `e02538fa` — filed for its owner, not caused by this change. `internal/agent` (the package this change touches) finished clean in the same run
  (`4.27s` for the doc-anchor gate in the budget's slowest-test list, no failure reported).
  The scoped re-run of the standards gates this change could affect
  (`go test ./internal/standards/ -run 'Gate7WorktreeRootLiteralRealTreeIsClean|RepoHygiene'`)
  → `ok github.com/nick/gopi/internal/standards 3.781s`.
- **Ran:** `go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1 -v`
- **Result:** `--- PASS: TestDocAnchorsResolveInNormativeDocs (3.80s)`, and the gate's own
  tail line:

  ```
  docanchor_test.go:227: doc anchors verified: 2948 across 636 normative docs (2446 explicit
      + 502 bare continuations; 603 bare out of row/table/heading scope; 240 pre-existing
      drift entries permitted, 0 new, 0 stale permits)
  ```

  **0 new / 0 stale**, permit count 242 → 240. Transcripts:
  `/home/user/.gopi/sessions/01a10af8-523e-73ef-892b-4d5c3c993fb4/docanchor-base.txt` (before)
  and `…/docanchor-after-v.txt` (after).
- **Gate untouched:** `git diff master -- internal/agent/docanchor_test.go` → empty output;
  `docAnchorMinChecked` unedited.
- **Worktree clean:** `git status --porcelain` prints nothing after the commit.

## 8. Evidence (self-filed)

- NEW drift, at its cause: `git show bd4c2bef:internal/standards/flake_registry_gate_test.go | sed -n '146p'` → `func TestFlakeRegistryMatchesTheTestsItNames(t *testing.T) {`; `sed -n '146p'` at `e02538fa` → empty line; `sed -n '174p'` at `e02538fa` → the same `func` line. Blank rule: `internal/agent/docanchor_test.go:713` (`strings.TrimSpace(lines[lineNo-1]) == ""`).
- Stale permit 1: `git show 53635229:cmd/agent/cli_contracts_test.go | sed -n '363p' | cat -A` and the same command at `e02538fa` → `\t}` both times (not blank-empty, so the finding cannot fire); `cmd/agent/cli_contracts_test.go` 706 → 851 lines.
- Stale permit 2: `git show 53635229:cmd/agent/cli_completion.go | sed -n '151p'` → blank; same command at `e02538fa` → `var memoryCompletionNames = memorySubcommandNames`; the file 342 → 362 lines.
- Permit ratchet: `grep -vc '^#' internal/agent/testdata/docanchor-baseline.tsv` → 242 → 240; `git diff --stat` shows the tsv as `2 --` (two deletions, no additions).
- Dated-record decision: `git show 69975030:internal/tools/task_finish_cleanup.go | sed -n '767p;1938p;1975p'` → the three `rep.Skipped = true` skip sites the §2e table lists at those exact lines (file then 2542 lines, 4684 today); `git show 69975030:internal/tools/is_server_node_test.go` → `fatal: path … exists on disk, but not in '69975030'`.
- The re-keyed symbol's identity: `grep -n '^func TestFlakeRegistryMatchesTheTestsItNames' internal/standards/flake_registry_gate_test.go` → `:174`; `…TestFlakeCatchMechanismStaysOffTheSmokePath` → `:438`.
- Final post-commit re-run (transcript `…/01a10af8-523e-73ef-892b-4d5c3c993fb4/verify2.txt`): `go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1 -v | grep 'doc anchors verified'` → `… 240 pre-existing drift entries permitted, 0 new, 0 stale permits` and `--- PASS`; `git diff master --stat -- internal/tools/chat_write_guard.go` → empty; `git blame -L 838,840 --date=short internal/tools/chat_write_guard.go` → all three lines `a8bf8ce28 (gopi 2026-10-05)`; `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`.

## 9. Not Done (honest)

- **Master's `internal/standards` red is left to its owner** (out of this task's scope: a
  `devdoc/` change may not carry an `internal/tools` fix). Filed as its own bug row,
  **`01a10b17-3ec0-7366-955c-4cc9ca4e4479`** (`gopi task show 01a10b17-3ec0-7366-955c-4cc9ca4e4479`)
  with the one-line reproduction. The same row is the cause of two of the three failures in
  `make test-budget PKGS='./internal/agent/ ./internal/standards/'`: `TestStandardGates` and
  `TestStandardGateDataReport` both report Rule 18 `NEW=1` at `chat_write_guard.go:839`.
- The 15 permitted `eof` rows of `devdoc/assert-the-bug-audit-20260917.md` are **not**
  re-keyed and **not** removed; §5 is the deliverable for them (reason + measurement).
- This is a **doc-side** repair only: the gate is not taught to detect a same-file shift —
  by design, its rule is that the symbol in the row is the authority. The claim made here is
  that every anchor the gate resolves lands on a non-blank line and every permit is live;
  no claim is made that the repaired document is free of drifted anchors *inside* its other
  tables, nor that the sweep covered anchors the gate's bare-continuation rules leave out of
  scope (603 of them are reported as out of scope in the tail line).
- No attempt was made to repair drift in other normative docs: the briefing's scope is the
  1 new + 2 stale findings, and the gate reports no others.
