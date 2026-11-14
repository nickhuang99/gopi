<!-- doc-class: historical -->
# Devdoc: the doc-anchor gate's master red — repaired by re-keying standing claims, permitting dated records

**Task:** `01a10a8f-9173-76b6-8f81-df1781fee7a9` (branch
`task/01a10a8f917376b68f81df1781fee7a9`, worktree
`/home/user/workspace/gopi-01a10a8f917376b68f81df1781fee7a9`)
**Gate:** `internal/agent/docanchor_test.go` — `TestDocAnchorsResolveInNormativeDocs`,
manifest-scoped over the `normative` rows of `devdoc/DOC-INDEX.tsv` (632 markdown docs).
**Tree measured:** master tip `156e2e69` (*merge: task/01a10a45f3b17cdb9d5db237dab80dc2 …*),
this run's own worktree, `git status --porcelain` empty at the start.
**Verdict of the epic:** DOABLE — 15 new drift entries and 0 stale permits, all
repairable inside one session.

## 1. The measured starting state (observed, not carried over from the briefing)

```
$ go test ./internal/agent/ -run '^TestDocAnchorsResolveInNormativeDocs$' -count=1     # BEFORE
--- FAIL: TestDocAnchorsResolveInNormativeDocs (3.16s)
    docanchor_test.go:227: doc anchors verified: 2945 across 632 normative docs (2444 explicit
        + 501 bare continuations; 592 bare out of row/table/heading scope; 231 pre-existing
        drift entries permitted, 15 new, 0 stale permits)
FAIL	github.com/nick/gopi/internal/agent	3.155s
```

15 failure lines (`docanchor_test.go:203`, the blank-line family) over 12 docs, and
**zero** stale permits — so one of the epic's three arms (delete stale permits) has no
work on this tree, and the report says so rather than manufacturing an entry for it.
The drift is entirely "a `path.go:NNN` anchor that now lands on a blank line", i.e. a
symbol moved across a paragraph break.

## 2. The two dispositions, applied by what each document IS

The classification comes from the document itself, not from convenience, and it is the
same line the gate's own header draws: a document that makes a **standing claim about
current master** is re-keyed; a document that **records a measurement of a dated tree**
keeps the numbers it wrote (re-keying it falsifies the measurement). Concretely, a
document is dated here iff its own header names a branch/worktree/`master @ <sha>` or a
task id — the criterion the manifest's own classifier uses (`RECORD_TITLE_RE`,
`DATE_IN_NAME_RE`, `standing N / record M`).

### 2a. RE-KEYED — standing-claim docs (4 entries in 3 docs)

Each new line was found by looking the **symbol** up in today's file, never by
arithmetic shift; each target was read and confirmed non-blank on this tree.

| doc | row | old anchor | new anchor | symbol the row names (measured line) |
|---|---|---|---|---|
| `devdoc/branch-task-linkage.md` | :109 | `tasks.go:5192` | `tasks.go:6719` | `const taskTagResumedPrefix = "resumed:"` (declaration; the region runs `:6717-6721`) |
| `devdoc/gopi-chat-clarify-landing.md` | :48 | `internal/lineedit/editor.go:838` | `internal/lineedit/editor.go:888` | `func (e *Editor) WriteBandRow(row string) bool` |
| `devdoc/gopi-chat-clarify.md` | :186 | `editor.go:838` | `editor.go:888` | same (`Editor.WriteBandRow`) — bare-continuation twin |
| `devdoc/gopi-chat-clarify.md` | :238 | `internal/lineedit/editor.go:838` | `internal/lineedit/editor.go:888` | same, explicit-path twin |
| `devdoc/merge-backlog-terminal-guard.md` | :37 | `internal/tools/tasks.go:2825` | `internal/tools/tasks.go:3643` | `var taskAllowedTransitions = map[string][]string{` |

`gopi-chat-clarify-landing.md` and `gopi-chat-clarify.md` are standing-claim docs on
the manifest's own terms (`standing 0 / record 0` and no dated header — they are the
design + landing checklist for a live feature), and the mechanism they describe is
alive: `Editor.WriteBandRow` exists at `editor.go:888` today. One edit covered **three**
anchors in `gopi-chat-clarify.md` because the row at `:186` carries `editor.go:838`
twice (an explicit and the explicit form of the same citation) — the second was matched
by the same substitution.

### 2b. PERMITTED — dated landed records, dated SPECs, dated re-measures (11 entries in 9 docs)

All 11 newly permitted entries below are the **same class the baseline already carries**
(a landed record whose quoted line map master has since moved) and each carries a
per-row reason naming which measurement the record is.

| doc | anchor (as written) | the record it is |
|---|---|---|
| `audit-ab3-peerbox-2026-09-04.md` | `internal/tools/tasks.go:1230` | dated landed audit, header `Base: master 7098ce5`; its D3 defect row quotes `TaskList`'s header render at that head (`TaskList` itself starts at `:1928` today) |
| `branch-task-linkage.md` | `tasks.go:5192` | the doc's own header: *"Baseline: `master` @ `69975030` … This is the evidence report for epic `01a0a964` item [1/5]"*, read in worktree `~/workspace/gopi-branch-linkage` |
| `salvage-20260912-0412-incident.md` | `cmd/agent/cli_tasks.go:3149` | dated incident record (2026-09-12 04:12, salvage task `01a0924c`); it names the refusal as `da3c2960` (05:04:47) left it — that refusal is at `cli_tasks.go:6229` today |
| `task-ledger-hygiene-20260912.md` | `tasks.go:3663` | dated landed report (2026-09-12, branch `chore/task-ledger-hygiene`); `FilterArchivedRows` starts at `:7104` today |
| `task-reparent.md` | `cli_tasks.go:734`, `cli_session.go:178`, `cmd/agent/cli_session.go:178` | landed record of its own run (task `01a10a18…`, its own worktree); §6 records the re-points **it** made (`cli_tasks.go:734`→`:312`, `cli_session.go:178`→`:193`, the latter already stale at its pristine `HEAD` per its `## Not Done (honest) 1`) |
| `taskid-vs-sessionid-verdict.md` | `tasks.go:2284` | dated landed verdict (*"branch `docs/taskid-vs-sessionid` … off `master` @ `e3195ca6`. Investigation only"*); its id table quotes `taskSessionLogPath` at `:2284` (`:2720` today) |
| `tmpdir-merge-resolution.md` | `cmd/agent/cli_daemon_spawn.go:75` | dated landed merge record (2026-09-13, `fix/tmpdir-settings`); the constant `daemonReadyEnv` is declared at `:153` today |
| `verify-responsibility-spec-20260912.md` | `internal/tools/tasks.go:2224` | dated SPEC (2026-09-12, task `01a09242`, *"Deliverable B"*); the anchor names `TaskUpdateReason` where that tree had it (`:3981` today) |
| `web-m2-b5b8b9b12-remeasure.md` | `cmd/agent/commands.go:704` | dated re-measure (subject tag `archive/feat/web-m2-b5b8b9b12` tip `98fb2942`, *"Measured against: master `fa12d97f`"*); its per-unit table quotes the **tag's** line map (`webClearCommandName` is at `:807` today) — the same doc already carries the sibling `eof` permit for this reason |

Two of those docs are cited by a current artifact (`audit-ab3-peerbox-2026-09-04.md` by
`internal/deepseek/error_display_limit.go:13`, `branch-task-linkage.md` by
`briefings/brief-req-2-task-ledger.md:144`) and are therefore classed `normative` — but
the **cited finding** is not the **drifted row**. `error_display_limit.go:13` cites the
audit's retry-line-number finding; the anchor the gate reds is the D3 record's, at
`tasks.go:1230`. That entry's reason states exactly this, so the permit excuses a
recorded measurement and never a standing claim.

## 3. What deliberately was NOT done

- **No gate-logic change.** `internal/agent/docanchor_test.go` is untouched: no regex,
  no `docAnchorMinChecked` floor, no permit-matching rule. `git diff --stat` over
  `internal/` shows the baseline file only.
- **No permit for a standing claim.** Every anchor whose doc is a live claim was
  re-keyed by symbol; the permit form was used only where re-keying would falsify a
  record. (An earlier pass of this run *did* re-key two dated records —
  `branch-task-linkage.md` and `taskid-vs-sessionid-verdict.md` — which the epic's
  constraint forbids; that edit was reverted with `git restore` and both are permitted
  above instead.)
- **No blind baseline rewrite.** No line was inserted or deleted from the permit list;
  11 rows were appended, each with its own reason, and one pre-existing reason
  (`audit-ab3…`) was sharpened to say the drifted row is the dated record, not the cited
  finding.

## 4. Verification

- **Build:** `go build ./...` → OK (exit 0); `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- **Tests (observed, on the committed tree):**
  - `go test ./internal/agent/ -run '^TestDocAnchorsResolveInNormativeDocs$' -count=1` → `ok`
    (exit 0), `--- PASS` , with the gate's own summary line:
    `doc anchors verified: 2945 across 632 normative docs (2444 explicit + 501 bare
    continuations; 592 bare out of row/table/heading scope; 242 pre-existing drift
    entries permitted, 0 new, 0 stale permits)`.
  - `go test ./internal/agent/ -run '^TestDocAnchor' -count=1` → the whole docanchor family
    PASS: `TestDocAnchorsResolveInNormativeDocs`, `TestDocAnchorBaselineHasNoDuplicateKeys`
    (242 permit rows, no duplicate key, no duplicate line),
    `TestDocAnchorBaselineDuplicateGuardCatchesDuplicates`, `TestDocAnchorBareLineRules`,
    `TestDocAnchorScopeComesFromTheManifest` (exit 0).
- **Ran:** the gate is the surface this change touches, and it was run for real with real
  arguments on the real corpus (above) — both before (exit 1, `15 new`) and after (exit 0,
  `0 new, 0 stale permits`). The before/after pair is the deliverable's own proof, and the
  corpus is the live tree: no fixture, no synthetic input.
- **Result:** 15 red lines → 0. The permit list grew 231 → 242 rows, all newly added rows
  matched by this run's own invocation (a permit that matched nothing is red as *stale*,
  and the run reports 0 stale).
- **Manifest consequence (this report is itself a new `devdoc/` file):**
  `./bin/gopi doccheck` → `exit=1`, `doccheck: file-with-no-row
  devdoc/docanchor-drift-repair-20261005.md — every file under devdoc/ must be
  classified (class + reason)`; then `./bin/gopi doccheck --fix` → `exit=0`,
  `doccheck: repaired 1 path(s)`, writing the row
  `devdoc/docanchor-drift-repair-20261005.md	historical	historical (5 standing / 5
  record): the doc's own title/name declares a past run's record, and no current
  artifact cites it as authority` into `devdoc/DOC-INDEX.tsv` (line 474). It is
  classified in the same commit that lands it (Rule 36) — the `write` call that
  created this file reported `timed out after 30s` while the bytes did land, which
  is exactly the door `gopi doccheck` exists to close.
  - `go test ./internal/standards/ -run 'DocManifest|DocClass' -count=1` → `ok` (exit 0)
  - `go test ./internal/tools/ -run 'Manifest' -count=1` → `ok` (exit 0)
- **Commit:** see the branch tip; the worktree is clean (`git status --porcelain`
  prints nothing).

## 5. Evidence (self-filed)

- `go test ./internal/agent/ -run '^TestDocAnchorsResolveInNormativeDocs$' -count=1`
  (before the change) → `exit=1`; tail: `231 pre-existing drift entries permitted, 15 new,
  0 stale permits`, `FAIL github.com/nick/gopi/internal/agent 3.155s`.
- Same command (after the change) → `exit=0`; `--- PASS: TestDocAnchorsResolveInNormativeDocs
  (3.72s)`, `ok github.com/nick/gopi/internal/agent 4.914s`, and the summary line quoted
  in §4.
- `go build ./...` → `BUILD_ALL_OK` (exit 0).

## 6. Not Done (honest)

- **The anchor line numbers that land on a NON-blank line but no longer on the named
  symbol are not audited.** That set is strictly larger than the 2945 anchors the gate
  resolves (the gate checks range and blankness, never content) and it is not what made
  the gate red. `devdoc/taskid-vs-sessionid-verdict.md:65` is a live example: its
  `:2368` is inside a table row, so it is not even a bare anchor the gate scans — while
  `taskRawLogPathFromSession` starts at `:2804` today. Fixing that class is a corpus
  audit of its own.
- **`0 stale permits` was the starting state, so no stale entry was deleted.** The epic
  lists deletion as an arm; this tree had nothing to delete, and the report records that
  as a measurement rather than inventing a deletion.
- **The 231 pre-existing permits were not re-verified as a whole** — only the ones this
  run's gate invocation proved still match (all 242, since 0 came back stale) and the
  reasons touched in §2b.
- `bin/gopi` was built into the worktree (gitignored) so the CLI guards on the branch-scoped
  suite run rather than skip; it is not a deliverable.
- This report itself is a new `devdoc/` file, so it needs a manifest row and a class
  banner (`<!-- doc-class: historical -->` above) — added through the repo's own
  classifier, not by hand.
