<!-- doc-class: historical -->
# Six clone grounds, 104 dirty entries, and the gate that never looked at them

Measured 2026-10-27 on `hosta` by task `01a17b94-b1bd-706d-bdbd-41e4f73f36d3`.
This page records the population, the classification method, and the one mechanism
finding that outlives the inventory itself: **the Rule 30 close-out residue check
has no entry point on a clone ground.**

## 1. The population

`git status --porcelain` vs `git status --porcelain -uall`, per ground, with the
row's status read from the ledger (`tasks.status`):

| ground | porcelain | `-uall` | row status |
|---|---|---|---|
| `01a1623c-169b-71bb-9aca-d16889de83b8` | 50 | 50 | failed |
| `01a150f3-cb31-785e-bbc2-773813d06c1a` | 31 | 31 | **completed** |
| `01a16cba-80b6-7bfd-b8bd-fa264d796ce5` | 11 | 11 | failed |
| `01a16033-dd15-7ba0-9411-9401b400aebd` | 7 | **9** | failed |
| `01a14d7e-0863-7323-bae4-59640b84bb6f` | 2 | 2 | failed |
| `01a15bb9-b231-747b-ae57-7737658dc9e2-r1` | 1 | 1 | failed |
| **total** | **102** | **104** | 1 completed / 5 failed |

Two facts about counting, both worth keeping:

- **plain `--porcelain` folds a wholly-untracked directory into one line.** That is
  why `01a16033` reads 7 and is really 9: `?? devdoc/evidence/ellipsis-aggregate/`
  is three files. Any inventory that says "N dirty files" must say which spelling
  produced N.
- **`git diff HEAD` never carries untracked content**, so a patch-only archive of a
  ground silently loses every `??` entry. The archive recipe below copies them.

All six are CLONE grounds (`.git` is a DIRECTORY), and
`git -C /home/user/work/gopi worktree list --porcelain` reports exactly one
registration — the main checkout itself. Nothing here is a linked worktree.

## 2. Classification: compare END STATES, not patches

The six grounds sit **237–519 commits behind `origin/master`**, so no patch of theirs
applies to master (`git apply --check` fails on the first hunk of five of six). The
question "did this work land?" therefore cannot be answered by applying anything; it
is answered by comparing the ground's intended END STATE against master's bytes:

| entry shape | intended end state | test |
|---|---|---|
| ` M path` | the file's CURRENT bytes | `git hash-object path` vs `git rev-parse origin/master:path` |
| `D  path` | the file ABSENT | `git cat-file -e origin/master:path` |
| `?? path` | the file PRESENT with these bytes | same hash comparison, absence ⇒ new |

Result over 104 entries: **90 real work, 10 noise (already landed), 4 undecidable**.
The undecidable four are partial overlaps (25–53 % of the added lines already on
master) — a human's call, because part of the change landed by another route and part
did not.

A second, independent probe agreed: sample each ground's longest added lines and ask
`git log -S<line> origin/master`. `01a16cba` scored 6/6 present (the chat write
boundary turn carrier, `a968e5e`), `01a150f3` 4/6 (the `compute_nodes` /
`dispatch.go` deletion family, `bf87a5d`), and `01a15bb9-r1` 0/6 — that last one is
pure lost work: it introduces `reclaimGroundIsClone`, which the clone-shape question
ultimately answered a different way in `08e7022`.

## 3. The finding: a clone ground is invisible to the close-out residue check

`ReclaimRunResidueCheck` (`internal/tools/task_reclaim.go:2248`) is the Rule 30
measurement made at a `completed` writeback. Its two attribution arms are
`reclaimRunCodeWorktree` (:2120) and `reclaimRunUnattributableGround` (:2287). **Both
ask `mergedCleanupWorktrees(repo)`** (`internal/tools/task_merged_cleanup.go:898`),
which lists LINKED worktree registrations. A clone ground is not in that list, so
both arms return `""` and the check returns before it measures:

```go
	if wt := reclaimRunCodeWorktree(repo, branch, worktree); wt != "" { … }
	if wt := reclaimRunUnattributableGround(repo, worktree); wt != "" { … }
	return ReclaimRunResidueOutcome{}, nil      // ← a CLONE ground always ends here
```

Calling the real predicates on all six rows, with `GOPI_MEMORY_DB` pointing at a
COPY of the ledger (`ReclaimRunResidueCheck` is read-only; the copy is hygiene):

```
PROBE mergedCleanupWorktrees(/home/user/work/gopi): err=<nil> mainDir="/home/user/work/gopi" linked=0
PROBE row=01a150f3-…6c1a repo="/home/user/work/gopi" branch="master" worktree="/home/user/workspace/01a150f3-…6c1a"
      reclaimRunCodeWorktree="" reclaimRunUnattributableGround=""
      ReclaimRunResidueCheck: err=<nil> attributed=false blocked=false worktree="" porcelainLines=0 note=""
```

`linked=0` is the whole mechanism. Consequences, in order:

1. no `completed-unclean*` event is written for a clone-通道 run, so the rule's
   "record it" half is silent;
2. `GOPI_TASK_RUN_RESIDUE_ENFORCE=1` cannot refuse anything there, because the refusal
   path reads the same `Note == ""`;
3. the ledger agrees: 9831 `task_events` rows, and
   `completed-unclean-tracked-work-refused` appears **once**, on a row whose ground
   used the older `gopi-<id>` naming (a linked worktree, since removed).

**This is not "the gate was bypassed". It is a gate with no entry point on the
channel the product now uses.** Filed as `01a17b9d-f074-7ba9-adf6-b19aec0d1a90`.

### The live instance

`01a150f3-cb31-785e-bbc2-773813d06c1a` is `completed` with 31 dirty entries, and the
dirt predates the claim — the newest dirty file is 33 s older than the first
`completed` writeback:

```
ended_at            1792370411  2026-10-19 08:40:11
newest dirty file   2026-10-19 08:39:38.503560959 +0800  internal/standards/testdata/config-anchor-census.tsv
delta              -33 s
```

Its trail, complete:

```
in_progress           1792368295
reframe               1792368315  verdict=DOABLE
completed             1792370411  elapsed=35m15s, task-report 13117 bytes
failed                1792370412  ← one second later
completed             1792407528  ← 10 h later, no run between (ledger-side writeback)
reclaim-unreachable   1793020244  ← the false "ANOTHER repo's linked worktree" sentence
```

So: the gate could not fire (code above), the dirt was there when the claim was made
(time above), and the final `completed` has no execution behind it at all — a
separate question this page does not answer.

## 4. Recovery recipe — the work survives WITHOUT the ground

Per ground the session evidence directory holds `combined.patch` (`git diff HEAD`),
`head-blobs/` (the `HEAD` blob of every touched path), `worktree-files/` (current
bytes of every touched path that still exists) and `touched-paths.txt`.

```
EV=/home/user/.gopi/sessions/01a17b94-b1bd-706d-bdbd-41e4f73f36d3/evidence
d=01a1623c-169b-71bb-9aca-d16889de83b8        # any of the six
mkdir -p /tmp/recon && cp -a "$EV/$d/head-blobs/." /tmp/recon/
cd /tmp/recon && git apply --whitespace=nowarn "$EV/$d/combined.patch"
cp -a "$EV/$d/worktree-files/." .              # then the untracked files
```

Verified byte-for-byte: `reproduced_identical=82 differ=10 deleted_in_worktree=12`
across the six, where every `differ` is exactly one of that ground's untracked files
(they are not in `combined.patch`) and every `deleted_in_worktree` is a path the
ground removes (recovered from `head-blobs/` + the patch). So a teardown that removes
the directories does not destroy the work, provided this directory is kept.

## 5. What a teardown must not conclude from this

- **The 90 real-work entries are not "safe to drop" because the rows are `failed`.**
  `failed` is a run outcome, not a verdict on the work; `01a15bb9-r1`'s
  `reclaimGroundIsClone` is a complete, coherent implementation that nothing else
  replaced.
- **The 113 legacy `reclaim-unreachable` markers are HISTORY.** `08e7022` fixed the
  wording for future passes only; it migrates no marker, and
  `reclaimRefusalIsPermanent` + `reclaimUnreachableRecorded`
  (`task_reclaim.go:564`) make a permanent refusal permanently silent. The six
  grounds therefore wear a sentence that is now known to be false, and no sweep will
  revisit them.
- **Do not measure "is there work here" with a patch apply.** Five of six fail
  `git apply --check` purely because of the 237–519-commit gap.

## 6. Files

Evidence (session directory, not tracked):
`task-report.md`, `evidence/FINAL-CLASSIFICATION.tsv`, `evidence/event-trails.txt`,
`evidence/ledger-rows.txt`, `evidence/probe-residue.txt`,
`evidence/probe-test-withdrawn-from-tree.go.txt`, `evidence/recon-proof.txt`,
`evidence/endstate-01a150f3.txt`, and one subdirectory per ground.
