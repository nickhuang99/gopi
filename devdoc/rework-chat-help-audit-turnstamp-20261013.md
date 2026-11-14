<!-- doc-class: historical -->
# Rework of the retired `task/01a12c4c…` — the help names its own audit, and a plain prompt's first request carries no turn stamp

Truth class: **historical** — this is one run's record (2026-10-13) of
re-applying an archived branch onto master. It is not a standing claim about
master; the standing claims live in the files below.

## What was reworked, and why it could not auto-merge

`task/01a12c4caf81780aa6b287195aee5583` (tip `a045fb7768e06164e4f7809e2488bca219a472f9`,
cut at `acc3fa3eb`) carried a 7-file intent: the `--dump-screen` help block names
the ONE call that audits the artifact, and a plain prompt's FIRST request carries
no harness turn stamp. Both halves had been **fixed independently on master** in
the meantime, in the same regions, so the branch's diff conflicted in three of
its seven files (`cmd/agent/cli_chat.go`, `cmd/agent/cli_lineedit_test.go`,
`internal/agent/clock.go`).

The archived tip is still in the object store (`git cat-file -t a045fb776` →
`commit`), so the rework follows Rule 23: a **merge of the archived tip into this
branch off master** (`691fb4180`), never an edit on the failed ref, so both
parents stay in history and `git log -p` still shows the original reasoning.

## The three conflicts, and what master's newer work decides

The decisive history (all 2026-10-12, all landed on master after the branch was
cut):

| master commit | what it does | effect on the archived intent |
|---|---|---|
| `f64c8507c` "the `--dump-screen` block names its reader again, out of the synopsis shape" | restores the reader paragraph AND fixes the census inflation the branch's own version caused (the audit command indented at 23 spaces, past the scanner's synopsis-head shape) | **the union, already landed**: the branch's `cli_chat.go` reader paragraph and its `45 → 47` census are the *same* edit as this commit, and master's form is the corrected one. Taken whole. |
| `cafe04e48` "a plain prompt's first request is the prompt alone" | lands `turnStampForRequest` in `internal/agent/clock.go` with a longer rationale + `internal/agent/plainprompt_turnstamp_test.go` | **the union, already landed**: `clock.go` is byte-identical outside comments (`diff` of both files with comments/blank lines stripped is empty), and the test is the same-named successor of the branch's case. |
| `222b42aa8` "the A4 census asserts it is reading the record, not a second filter" | `cli_lineedit_test.go` keeps the unfiltered `AllUserContents` read and adds the raw-census arm | master's form kept: the branch's variant reads `UserContents`/`HarnessUserContents` (which the `sessionevent` import would no longer be needed for). Both call the same predicate; master's arm is strictly stronger. |

## Accounting table — every hunk of the archived 7-file diff

`~/workspace/merge-run-20261012/branch3.diff`, sha256
`d47d546e4dfc48745292c178ef1e5e28424889bde825cac83802011dd4a8ee9e`
(`git diff acc3fa3eb a045fb776 --stat` at the time of the rework also reads
7 files, 167 insertions, 28 deletions).

| # | file | hunk | disposition | reason |
|---|---|---|---|---|
| 1 | `cmd/agent/cli_chat.go` | import `internal/toolnames` | **dropped** | master already imports it (added by `f64c8507c`); keeping the branch's line is a no-op, and the union is master's import block. |
| 2 | `cmd/agent/cli_chat.go` | `--dump-screen` DESTINATION sentence gains `(`gopi chat --dump-screen` is how a run is ARMED)` | **re-applied, adapted** | **This is the only surviving product hunk of the archive.** Master re-worded the *rest* of the paragraph's neighbourhood but not this sentence. Re-applied with `chatDumpFlag` in place of the branch's literal `--dump-screen` (Rule 13: the constant owns the spelling; the branch typed the literal). |
| 3 | `cmd/agent/cli_chat.go` | blanket replacement of the "Reading the artifact it writes" block with the `WHO READS IT` paragraph | **dropped (superseded)** | master's `f64c8507c` paragraph says strictly more (per-action tool contract, the ledger words, `ui_state -h` as the vocabulary home, the 23-space command indent that keeps the census at 45). Re-applying the branch's version would regress that commit and re-inflate the census. |
| 4 | `cmd/agent/cli_chat.go` | `statusLiveIdleTurn` comment: `turnLabel` → `iterLabel` | **dropped (archived hunk is stale/wrong)** | master's function is `turnLabel` (`cmd/agent/chat_stream.go:3888`); no `iterLabel` exists anywhere in the tree, and `git log -S "is how a run is ARMED" --all` is empty while the commit body cites it. The branch's line would point readers at a function that does not exist. |
| 5 | `cmd/agent/cli_chat_web_help_surface_basic_test.go` | `g5ExpectedFlags` 45 → 47, with a Rule-38 note | **dropped (superseded, and the number is now wrong)** | The inflation was a *defect* of the branch's own paragraph; `f64c8507c` removed the cause. Re-derived on the merged binary: `scripts/g5-chat-web-help-surface.sh` reports **22 flags for `gopi chat` and 45 for the group** — so master's pin (45) is the derived one and 47 would be a false pin. Live command and output: `artifacts/g5-census-merged.tsv`. |
| 6 | `cmd/agent/cli_lineedit_test.go` | drop the `sessionevent` import; replace the split loop with `sess.UserContents()` / `sess.HarnessUserContents()`; replace the comment block | **dropped (superseded)** | `222b42aa8` reached the same measurement from the other side and kept the unfiltered read + a stronger arm (the raw census must see every row). The branch's variant is equivalent in effect (`testutil.Session.UserContents` filters with the same `sessionevent.IsHarnessInjected`) but weaker. Its rationale is recorded as a comment at the loop, so the archive's reasoning is not lost. |
| 7 | `cmd/agent/cli_lineedit_test.go` | delete the "REQUEST COUNT IS CHECKED NONZERO FIRST" comment | **dropped (superseded)** | `222b42aa8` states why that arm exists twice over. |
| 8 | `cmd/agent/cli_lineedit_test.go` | delete the `raw := sess.Counts()["user"]` arm | **dropped (superseded)** | That arm is master's; deleting it is the regression `222b42aa8` was written to prevent. |
| 9 | `cmd/agent/cli_runpath_boundary_rp1_test.go` | "THE THIRD ADDITION — the per-request turn stamp" history comment | **re-applied** | Master's `cafe04e48` reworked the *message* half of this region but kept no account of why the stamp was the third injector. Re-applied verbatim: it is the measured history behind the exemption and points at the in-process repro. |
| 10 | `devdoc/gopi-chat-web-help-surface-inventory.md` | row 1 gains `all,target` and Totals 45 → 47 | **dropped (superseded, and the numbers are wrong)** | Same cause as #5. On the merged binary the census shows 22 flags on row 1 and a group total of **45**; master's row and Totals line are the ones the script derives. |
| 11 | `internal/agent/clock.go` | `Degradations, both silent` → `all silent` + the exemption clause; new `turnStampForRequest`; `requestMessages` calls it | **dropped (already on master, comment for comment re-worded)** | `cafe04e48` landed the same code. Proof it is the same code, not merely similar: `diff <(git show master:internal/agent/clock.go \| grep -v '^\s*//' \| grep -v '^\s*$') <(git show a045fb776:internal/agent/clock.go \| …)` prints nothing (`CLOCK_CODE_IDENTICAL`). Only the prose differs, and master's prose is the longer one. Re-applying the branch's would shorten a rationale. |
| 12 | `internal/agent/plainprompt_firstmessage_test.go` | +70 lines: `TestPlainPrompt_FirstRequestCarriesNoTurnStamp`, three arms | **dropped (same-named duplicate)** | `cafe04e48` landed `internal/agent/plainprompt_turnstamp_test.go` with a function of the **same name** (`go build` → `TestPlainPrompt_FirstRequestCarriesNoTurnStamp redeclared in this block`). The successor has three arms too, and asserts the gap suffix rather than "a stamp is present". Restoring the fix would require **renaming master's** test — a change to landed work for no coverage gain. |

Two hunks survive (#2, #9) plus one comment (#6), and the branch's own new test
(#12) is accounted for as a duplicate rather than lost. Net delta of this branch
against master, measured after the merge:

```
 cmd/agent/cli_chat.go                      |   6 +-      ← hunk #2
 cmd/agent/cli_lineedit_test.go             |   9 ++      ← hunk #6's rationale comment
 cmd/agent/cli_runpath_boundary_rp1_test.go |  15 ++      ← hunk #9
```

(The three files master landed *behind* `8356a4925` — the `notify` family — are
gone from the delta because the delivery-point sync brought master forward.)

## Surprises worth a note

1. **The archive contained a hunk that its own parent tree contradicts.** #4's
   `iterLabel` rename: the commit body the archive shipped cites `iterLabel`, and
   no such symbol has ever existed in this repo (the file's `turnLabel` is at
   `cmd/agent/chat_stream.go:3888` in both `acc3fa3eb` and `master`). An
   auto-merge would have carried it into master as a comment pointing at a
   missing function — nobody reads a comment in a conflict-free merge.
2. **A "conflict" that is the same measurement from the other side.** #6 is not
   two features colliding: the branch and `222b42aa8` are two resolutions of one
   2026-10-12 fixture bug, taken hours apart. Master's is the stronger arm; the
   archived one is the same read through a pair of accessors that master added
   for the purpose. Recording that in a comment is cheaper than choosing.
3. **`git merge` of this branch claims three conflicts and hides two more
   problems** (#5, #7, #8, #10, #11, #12 are not conflicts — they auto-merge or
   are already identical). The census pin (#5) is the dangerous class: git would
   have let `47` merge silently, and it is only wrong once the *paragraph* it
   counts is also taken from the other side. Union resolution is not
   file-by-file — it is measurement-by-measurement.

## Addendum (2026-10-13): two corrections the scoped suite forced

The first scoped run of this branch was quoted in the task report as "4 FAIL
arms with no failure body". A **larger** whole-package run (`gates-final.log`,
HEAD `7925740a0`, `./internal/agent/ ./cmd/agent/`, 1241s, 5423 tests) reported
**15**, so the number in the first draft was an undercount produced by reading a
truncated stream, not by measuring the suite. Re-run one arm at a time (`-p 1`,
no parallelism) on the final HEAD `9b7cab361`: **13 pass, 2 fail** — and both
failures are **pre-existing on pristine master**, which is the only reason they
are not this branch's regression:

| arm | who it is red for |
|---|---|
| `TestRepairRepoMovesARowOntoALiveWorkTree` | master `cf15c989d`, **3/3**, `-p 1` — the fixture back-fills `deliverable.worktree` and then the assertion that a second repair "must say so" reads the *first* repair's success string |
| `TestDocAnchorsResolveInNormativeDocs` | master — `8 new, 2 stale permits`; one stale permit names `cli_tasks_worker_output.go:145`, a file master has since renamed |

Measured transcripts: `/home/user/.gopi/sessions/01a12c66-…/artifacts/ctrl-master-repairrepo-3x.txt`
and `…/final-2-red-arms.txt`.

**The lesson worth keeping** (the reason this addendum exists rather than a
quiet edit): an earlier draft of the report asserted that
`TestRepairRepoMovesARowOntoALiveWorkTree` "passes in a pristine detached
worktree of master" on the strength of an `ok … 18.161s` line **from a run that
never selected that arm**. A package-level `ok` is not an observation of a test.
The correction moved the conclusion in the *less* flattering direction — the
branch's scoped suite is not green — which is exactly why the wrong first
observation had to stay visible in the artifact tree (`rerun-4-tests.txt`) while
the measured one was filed beside it.

Consequence for hand-over: the delivery-point sync and the ff-merge probe stand
unchanged (both re-run clean), but the merge decision now carries this caveat —
the suite this diff lands into is **already red in two arms on master**, and
neither red is this branch's.

## Late addendum (2026-10-13, post-report)

master moved two commits after the report (39138e8a4 -> 0da655900 -> cf15c989d). Delivery-point sync re-run: git merge master merged cleanly (Auto-merging devdoc/DOC-INDEX.tsv; 0 unmerged paths), HEAD 194ea699f descends from master (merge-base --is-ancestor rc=0). Re-verified at that HEAD: go build ./... rc=0, git status --porcelain empty, the surviving hunk present in source (cli_chat.go) and in the freshly built binary, devdoc/DOC-INDEX.tsv auto-merge added the row for master's new devdoc/help-surface-failure-text.md, and go test ./internal/standards/ -run 'CommandsDoc|DocCheck' ok 16.3s.
