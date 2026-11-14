<!-- doc-class: historical -->
# Task Report: merger doc drift — `--stop`/`--restart` "retired", and the whole verb retired (2026-09-19)

Task: `01a0b1cd-fa6a-7bc9-81c0-1ccc54150c92` (filed from the audit finding G4 in
`devdoc/merger-design-alignment-audit.md`).
Branch: **`fix/merger-selfdriving-doc-drift`** (worktree `~/workspace/gopi-merger-doc-drift`, from master `90e664e0`).
Docs-only: **no Go file is touched** (`git diff --stat` — see Verification).

## What the drift actually was (measured, not read off the briefing)

The briefing said: `devdoc/merger-self-driving.md` calls `--stop`/`--restart`
retired while `mergerRetiredFlags` holds only `--daemon`/`--start`/`--wait`/`--force`.
That is true, and it is **the 2026-09-18 snapshot of a three-state arc**:

| when | what | evidence |
|---|---|---|
| 2026-09-17 | the resident merger's machinery **and `--every`** deleted (`daemon-machinery-removal`) | doc + epic |
| 2026-09-18 | `--stop`/`--restart` **RESTORED** as a real control face over the loop; `mergerRetiredFlags` = `--daemon`/`--start`/`--wait`/`--force` | `f234658c`, merged `e241b5ee`; the code comment at `f234658c:cmd/agent/main.go:426-432` says so in prose |
| 2026-09-18/19 | the whole **verb closed** (`f9ff1a89`: `isMergerSubcommand` refuses every spelling) and then the **implementation deleted** (`55c4d6f3`) | `merger_retired.go`, `merger_face_shared.go`, the two files' commit stats |

So the doc's `retired` cell for `--stop`/`--restart` was stale in two directions:
wrong for one day, and then overtaken by a retirement that makes **every** row of
its table non-live. A correction that only fixed the flag class would have been
true for one day and false the next, so the correction states the whole arc and
separates the two questions the table conflates.

## Completed

1. **`devdoc/merger-self-driving.md`** — the atomic target:
   - a dated `## CORRECTION (2026-09-19, epic merger-delete-dead-impl) — dated, originals kept`
     block at the top, in three numbered parts: (1) the `f234658c` restoration of
     `--stop`/`--restart`, (2) the full retirement of the verb, (3) a **test-name
     truth table** resolving each name the doc cites against master `90e664e0`.
   - the **table row itself** now carries the correction (`--stop`/`--restart`
     split out of the `--start` row, its `now` cell marked *"restored the NEXT DAY
     by epic `merger-loop-restart` (`f234658c`)"*, plus a `2026-09-19` column);
   - each affected narrative section got a one-paragraph dated note naming what
     was deleted: `## Shape`, `## --status reports a LIVE loop`,
     `## Ownership details`, `## Gates updated`, `## Not done`, `## Verification`.
   - **originals are unedited** — a landed decision is evidence (GOPI.md Rule 23's
     spirit), and the re-development this doc is the design record for needs the
     reasoning. Nothing was erased; corrections sit beside the claims.
2. **The test-pinning claims, verified against tests that exist.** Every test and
   file name the doc cites was resolved by hand: `grep -rn "func <Name>(" --include=*.go`
   today, and `git grep -l "func <Name>(" f9ff1a89^ -- '*.go'` for where it lived.
   Result: **all 6 dead**. `merger_loop_test.go`, `merger_loop_e2e_test.go` and
   `merger_retired_flags_test.go` were deleted by `f9ff1a89`
   (205/297/139 lines); `serve_worker_split_test.go` (holding
   `TestMergerOnceLeavesPendingTaskUntouched`) by `55c4d6f3`. The **live** pins are
   `cmd/agent/merger_retired_face_test.go`'s `TestRetiredMergerFacesRefuseLoudly`
   and `TestRetiredMergerRefusalIsNotTheOldMessage`, plus
   `merger_work_lock_e2e_test.go`'s two cases for the surviving lock. All four
   were confirmed present **and** confirmed to be the ones that pin what the doc
   claimed (read, not assumed).
3. **Sweep of the sibling docs** — 21 more files carried the same class of claim
   and got a dated correction note naming the same commits; the normative ones
   that stated the retirement/reached-for face as current, and the historical ones
   whose subject was deleted. Every touched file is listed in `git diff --stat`
   and was inspected before editing. Two non-`merger-*` docs were included because
   they make the same claim: `devdoc/daemon-machinery-removal.md` (a normative
   doc whose "PARTIALLY SUPERSEDED" note still described `--every` as live) and
   `devdoc/taskcontrol-doc-truth-audit.md` (whose finding F1 listed the stale docs).

   Non-`merger-*`/non-stranded survivor checked and left alone, with the reason:
   `briefings/brief-devdoc-truth-classification.md:16` cites the drift as the
   MOTIVATION for the classification epic — it is the record of the discovery, and
   rewriting it would erase what this task is the fix for. It is rewritten by
   nobody in this change.

4. **No Go file modified**, and no attempt to change `mergerRetiredFlags` or any
   flag behaviour (constraint; the retirement makes the flag-class question moot).

## Not Done (honest)

- **`devdoc/DOC-INDEX.tsv` — ONE row added, for this report only.** The manifest
  gate requires every file under `devdoc/` to be classified, so my own report
  (`devdoc/merger-doc-drift-correction-20260919.md`, `historical`, banner added)
  got the row it needs. The **five** files the gate was already missing
  (`pending-queue-obsolescence-review-20260919.md`, `proc-identity-single-predicate.md`,
  `rule13-gate-self-red.md`, `task-update-status-message.md`,
  `web-page-token-fetch-fix.md`) are deliberately NOT touched: they are master's
  pre-existing rot (the first landed at `36e0f0bc`, in the main repo's working tree
  after `90e664e0`), and fixing them is the dispatched task, not this diff. My
  change therefore adds **zero** new manifest failures and closes the one it opened:
  2079 rows / 2084 files now (base was 2078/2083).
- **The two gates below are red ON PRISTINE MASTER** (`90e664e0`) with byte-identical
  output — they are not mine and not this branch's to fix. Filed below rather than
  worked around.
- **Two rows of the audit table I did not re-measure:** `merger-design-alignment-audit.md`
  G1/G2 named `merger_takeover_gate.go` and `merger_tick.go`, both deleted since —
  moot rather than open, and recorded as such in the doc (their G4 row now carries
  a RESOLVED note pointing at this task).
- **The three `Not done` items inside `merger-self-driving.md`** (the 22-minute
  hang, `/proc/locks` silence, the take-over re-probe) are all moot — their
  subjects are deleted. Recorded inside the doc, not fixed.

## Verification

**Build** — `go build ./...` → exit 0 (whole module, from the worktree; no Go file
is in the diff, so this is a sanity check that the branch is a clean checkout).

**The acceptance grep** (no surviving claim that `--stop`/`--restart` are retired
without a dated context):

```
$ grep -rn -- "--stop\|--restart" devdoc/merger-*.md | grep -i "retir\|refus"   # 38 hits, 14 files
$ ... | sed 's/:.*//' | sort | uniq -c | sort -rn
      7 devdoc/merger-retired.md
      7 devdoc/merger-doc-drift-correction-20260919.md      <- this very report
      6 devdoc/merger-readact-force-refusal.md
      5 devdoc/merger-stop-force-modifier.md
      2 devdoc/merger-stop-immediate.md
      2 devdoc/merger-self-driving.md
      2 devdoc/merger-restart-observed-superseded.md
      1 each: merger-stop-takeover-contract, merger-loop-restart, merger-help-truth,
              merger-help-detach-vocabulary, merger-graceful-restart-20260912,
              merger-design-alignment-audit, merger-busy-recycled-pid
```

Accounted for, per hit (this is the real acceptance evidence — a raw count is not):

- **A quotation of the refusal itself** (the retirement is the subject, the flags
  are content of a message): `merger-retired.md` (the retirement record — 7),
  `merger-help-detach-vocabulary.md` (1), `merger-restart-observed-superseded.md`
  (1 — quoted inside its own new correction anchor), `merger-help-truth.md` (1).
- **The mechanism being discussed is the PARSER's rule** — "two actions in one
  argv are refused", `--stop --restart` — NOT the flags' retirement class:
  `merger-readact-force-refusal.md` (6), `merger-stop-force-modifier.md` (5),
  `merger-stop-immediate.md` (2, of which one is *its own title's* "`--force`
  retired" — a different flag, still true), `merger-stop-takeover-contract.md` (1:
  "`--restart` while a round runs → REFUSE before signalling").
  **Every one of these files now opens with a dated CORRECTION/CORRECTION-ANCHOR
  note** naming `f9ff1a89`/`55c4d6f3`, so the survivor is explicitly
  contextualised, which is the acceptance rule's other allowed form.
- **The corrected docs themselves:** `merger-self-driving.md` (2 — its own
  corrected row + the retirement sentence) and `merger-loop-restart.md` (1) and
  `merger-graceful-restart-20260912.md` (1) and `merger-busy-recycled-pid.md` (1),
  each with the dated note.
- `merger-design-alignment-audit.md` (1) — its G4 row now carries the RESOLVED
  note.

**Zero hits present `--stop`/`--restart` as retired-and-current without a dated
context in the same file.** 23 of the 24 `.md` files under `devdoc/merger-*.md`
that mention the pair were inspected; the 24th (`merger-retired.md`) is the
retirement record itself, whose whole subject is that the flags are refused.

**The doc gates** (`go test ./internal/standards/ -run TestDoc`,
`go test ./internal/agent/ -run TestDocAnchor`):

- `TestDocManifestCoversEveryDevdocFile` FAILS — 2079 rows cover 2084 files
  (`pending-queue-obsolescence-review-20260919.md`, `proc-identity-single-predicate.md`,
  `rule13-gate-self-red.md`, `task-update-status-message.md`,
  `web-page-token-fetch-fix.md`); **same failure, same five files, on a pristine
  `90e664e0` worktree** (`~/workspace/gopi-mdrift-base`). On base the count is
  2078/2083 — the branch adds exactly ONE row (its own report) and ONE file, so the
  delta is zero: no new unclassified file, no new failure.
- `TestDocAnchorsResolveInNormativeDocs` FAILS — `doc anchors verified: 1970 across
  413 normative docs (1595 explicit + 375 bare continuations; 392 bare out of
  row/table/heading scope; 201 pre-existing drift entries permitted, 108 new, 30
  stale permits)`. The **counters are identical on base and on this branch**, and
  `diff` of the full failure output (path-normalised) is empty → my 23 markdown
  edits add **zero** anchors and zero failures. (My added text uses no `path.go:NNN`
  anchor form; the one `merger_loop.go:283` string is a quotation of a test's
  failure output inside prose.)
- Both are pre-existing master rot; a fresh bug row is filed for them: **(see
  `## Filed`)**.

**Rule 30** — `git status --porcelain` in the worktree: empty at the end (below).

## Evidence (self-filed)

- Build: `go build ./...` → `exit=0` (no output).
- Dead tests: `for t in <11 names>; do grep -rl "func $t(" --include=*.go .; done` →
  all empty for the 6 the doc cites; `git grep -l "func TestMergerEveryIsASleepParameterNotAControlFace(" f9ff1a89^ -- '*.go'`
  → `cmd/agent/merger_loop_test.go` (i.e. it existed, and only before `f9ff1a89`).
- Ancestry: `git merge-base --is-ancestor f234658c f9ff1a89` → true (restore first,
  retirement second); all five commits are ancestors of `master`.
- Gates: base-vs-branch `diff /tmp/b2.txt /tmp/m2.txt` → **empty** after
  normalising the worktree path out; both files 327 lines.
- The refusal is real, not paraphrased: `cmd/agent/merger_retired_face_test.go`
  drives 8 spellings and asserts non-zero exit + `mergerRetiredCause` + an
  untouched profile.
- Diff scope: `git diff --stat` → 23 `.md` files, `322 insertions(+), 16 deletions(-)`;
  **zero `.go`**, zero `DOC-INDEX.tsv`, zero baseline files.

## Filed (Rule 26 — dispatched, not deferred)

`internal/docclass`'s manifest gate and `internal/agent`'s doc-anchor gate are both
red on master `90e664e0` for reasons that predate this branch (see Verification).
They are a docs-breadth job, not a merger-doc job, so they are submitted as their
own task rather than smuggled into this diff: **task `01a0ba14-7d18-7550-9468-e58fd6de2907`**
(tags `bug`), whose payload carries the two exact commands, the five unclassified
files, the counters, the iterate-in-this-order note (manifest rows can extend the
anchor gate's scope), and the pointer back to this report.

## Deliverable (Rule 6d)

- **Branch:** `fix/merger-selfdriving-doc-drift` (in the shared repo — the worktree
  shares `~/work/GoPi`'s object store, so the commit IS the deliverable; no push needed).
- **Recovery command:** `git show fix/merger-selfdriving-doc-drift:devdoc/merger-doc-drift-correction-20260919.md`
  (this report), or `git show fix/merger-selfdriving-doc-drift:devdoc/merger-self-driving.md`
  (the corrected target). Verify the commit landed with
  `git log --oneline fix/merger-selfdriving-doc-drift -1` → `2d0ad878`.
- The worktree is removed after close-out; the branch is not merged by this run.
