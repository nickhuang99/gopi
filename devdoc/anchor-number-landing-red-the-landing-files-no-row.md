<!-- doc-class: historical -->
# The landing that minted the anchor red — and the row it did not file

## What this records

Row `01a136ee-8817-7a61-8f3d-942bdbfa31d4` was filed 2026-10-14 04:22 with a
premise — *"`gopi tool doccheck` is RED on the wire tip `bbf8d97604ed5993886f43e28313ec6d0ae1e03d`
… STALE PERMIT"* — and sat `pending` for roughly two days. This document stamps
that premise against the LIVE tip on 2026-10-16, measures the merge that made the
red with the landing leg's OWN command, and locates the predicate that decides
whether a landing's fresh red is reported or lost.

Host: `hosta` (client — **Rule 28** puts the merge/close-out machinery on
peerbox; this run files, it does not implement). Generator and revisions are named
per Rule 38. All readings are from real checkouts
(`git worktree add --detach`), never from a diff.

## ① The premise, stamped at the live wire tip

Wire tip at measurement (2026-10-16, `git ls-remote origin refs/heads/master`,
and `git -C /home/user/work/GoPi rev-parse --short HEAD` on peerbox — both agree):
**`51b8a2d99eeeeae616c21114013bf7ce7351ec6b`**.

| command (in a detached checkout of `51b8a2d99`) | exit | what it said |
|---|---|---|
| `gopi tool doccheck` | **1** | `stored-cell drift` — 15 row(s) counted, 3 echoed; `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv` |
| `go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check` | **0** | `the freeze table is EMPTY and this scan accounts for it: 273 anchor row(s) scanned, 0 incoherent` · `table matches the tree — the freeze is exact` |

**The premise is half-false, and the half that is false is the half that named the
work.** The STALE PERMIT arm is GREEN at the wire tip — the freeze table is empty
and that emptiness is accounted for by the scan (`frozen=0`), because the corpus
moved to the number-less render (`6bc71ab67` *"the anchor cell's owner is the
number-less render, and an empty freeze table is judged from the scan"*) and the
remaining permits were regenerated away (`fabcf3c96` *"regenerate the
anchor-number freeze table — 141 stale permits removed"*, both ancestors of the
tip). `gopi tool doccheck` does still exit 1, so *"master is red"* is true — but
by a **different arm** (`stored-cell drift`), which the row never named.

A two-day-old red is therefore dispatched as live work carrying a red that no
longer exists, and the red that DOES exist is one nobody dispatched. The
dispatched defect and the live defect are not the same defect.

## ② The landing's own leg, measured per revision

The devdoc-anchor leg's own question is `go test -count=1 ./internal/docclass
./internal/standards` (`mergesetDocclassRuns`, `internal/tools/mergeset_docclass.go`).
Run in a detached checkout of each revision:

| revision | role | result | which arm |
|---|---|---|---|
| `476e47ede` | integration branch | **FAIL** | `internal/standards` only — `TestDocClassBannerMatchesManifest`: *1437 markdown docs … (1 mislabelled)* |
| `77be22766` | the landed branch | **FAIL** | `internal/standards` only — same test, *0 mislabelled* (the DOC-INDEX half of the row↔banner pair) |
| `bbf8d97` | their merge | **FAIL** | **both**: `internal/docclass` — `TestAnchorNumberGateOnTheRealTree`, `TestAnchorNumberCensusShippedFaceIsExactOnTheRealTree`, `TestAnchorNumberNumberChangeOverTheRealFrozenPopulation` (the STALE PERMIT: `devdoc/merge-backlog-terminal-guard.md internal/tools/tasks.go }`) **and** `internal/standards` (`1 mislabelled`) |

The docclass arm exists **only in the merge**: neither parent was red there, and
`git merge-tree --write-tree 476e47ede 77be22766` is `bbf8d97^{tree}`, so the
union IS the merged tree and no revision-scoped lane holds that composition. The
briefing's mechanism is confirmed by re-measurement; its *current* consequence is
not (see ①).

## ③ The predicate that decides whether a fresh red survives

The leg's run set at `bbf8d97` was **`./internal/docclass` alone**:

```
$ git show bbf8d97:internal/tools/mergeset_docclass.go | grep -n PackageRel
(no match — the manifest's own gate package is not asked for)
$ git merge-base --is-ancestor 10217dc02 bbf8d97   # "the devdoc-anchor leg asks
                                                    # for the manifest's own gate package too"
not yet
```

so the arm that would have to speak for the second half of the invariant
(`TestDocClassBannerMatchesManifest`, which lives in `internal/standards`) was
not in the run set of that landing at all — and ② shows a fresh standards red did
exist there, in a test **neither parent had red**.

Both closes exist on master and are exercised:
`10217dc02` adds `docclass.ManifestGatePackageRel` to the run set unconditionally
(`refs := []string{ref}` + `manifestRef`), and `3b3682683` (*"a fresh finding on
an already-red gate is no longer swallowed by the base's red"*) is the
`mergesetGateFreshFindings` split — a fresh finding survives an already-red base
gate, which the whole-verdict downgrade in `mergesetGateAttribute` would
otherwise swallow. `go test ./internal/tools -run 'MergesetGate|MergesetDocclass|MergesetGendoc'`
→ **PASS** (16 tests, including `TestMergesetGateKeepsAFreshFindingOnAnAlreadyRedGate`
and `TestMergesetDocclassAsksTheManifestGatePackageForTheCleanMergeHalfState`).

## ④ The honest boundary

- **Did the landing file its row?** No row carrying the leg's own gate label
  exists: `ssh peerbox 'gopi task list --all | grep -ciE "devdoc-anchors"'` → `0`,
  and peerbox does not know this row at all (`gopi task show 01a136ee-…` →
  `no task with task_id=…`; it was filed on hosta's ledger, which is the
  client-side filing path).
- **Why it did not fire is NOT proven.** The merger's log for that landing is on
  peerbox and was not read here. What is proven is narrower and still load-bearing:
  the run set could not ask the standards half, and the standards half is where
  the fresh, parent-free red was.
- **Nothing in `internal/tools` was changed by this run** (Rule 28). The three
  files the sibling repair would have to touch — `mergeset_docclass.go`,
  `rebuild_vet_mergeset.go`, `merge_target_doc_gate.go` — were read only.

## ⑤ One incidental measurement, filed as a phenomenon (Rule 41)

This run's `GOPI_TASK_REPORT_PATH` names the session directory in the
**dash-less** spelling of the task id:

```
/home/user/.gopi/sessions/01a136ee-88177a618f3d942bdbfa31d4/task-report.md
```

while the run's own record reproduces (`task show` above) and the directory that
survives is the **hyphenated** id — the same spelling the `[CURRENT SESSION]`
line and the worktree carry:

```
/home/user/.gopi/sessions/01a136ee-8817-7a61-8f3d-942bdbfa31d4/
```

The path the variable names is **not** the session directory. Measured at 09:57:

```
$ mkdir -p /home/user/.gopi/sessions/01a136ee-88177a618f3d942bdbfa31d4   # inode 3580829 — EMPTY
$ stat -c %i /home/user/.gopi/sessions/01a136ee-8817-7a61-8f3d-942bdbfa31d4   # inode 4983109 — the session
$ ls /home/user/.gopi/sessions/01a136ee-88177a618f3d942bdbfa31d4         # (nothing)
```

Observed, with the clock as the only ordering (no deletion command was issued by
this run):

| time | what was run | what happened |
|---|---|---|
| 09:48–09:51 | `mkdir -p <dash-less>/artifacts`, `cp … <dash-less>/artifacts/leg-476e47ede.txt`, `ls -ld <dash-less>` | **exit 0** — the path resolved and accepted writes |
| 09:51–09:58 | a background job's `tee <dash-less>/artifacts/…`, and a `cp` back into it | **failed**: `tee: …: No such file or directory` |
| 09:58 | `stat` / `ls` / `cp` on `<dash-less>` | `No such file or directory`; `find ~/.gopi/sessions -maxdepth 1 -name '01a136ee*'` reports **only** the hyphenated directory |

The files written through the dash-less spelling at 09:51 are present in the
hyphenated directory (`ls -l` shows them with their 09:51 mtimes), so while they
were written the two spellings resolved to one directory; later the dash-less one
did not resolve at all, and a fresh `mkdir -p` of it produces an **empty**
directory that is not the session's. One captured `make test-budget` output
written to the dash-less path was lost and had to be recovered from the job
runner's own stdout copy.

**Measured**: `GOPI_TASK_REPORT_PATH` names a path that stopped resolving inside
the run, and re-creating it yields a directory that is NOT the session directory —
so the report can be written somewhere the session never sees. **Not measured**:
which component owns the dash-less spelling, and what reaps or re-creates it. The
report for this run is therefore written to the hyphenated session directory — the
one that exists — in addition to the path the variable names.

## Evidence

Session directory `~/.gopi/sessions/01a136ee-8817-7a61-8f3d-942bdbfa31d4/artifacts/`:
`wire-tip-premise.txt` (both commands, raw, 14 KB), `wire-tip-standards-arm.txt`,
`leg-wire-tip-51b8a2d99.txt`, `leg-476e47ede.txt`, `leg-77be22766.txt`,
`leg-bbf8d97.txt`, `attribution-predicate-tests.txt`, `make-test-budget.txt`.
