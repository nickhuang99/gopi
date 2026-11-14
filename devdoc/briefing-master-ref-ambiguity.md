<!-- doc-class: historical -->
# "取 master 版本" 有两个互不相同的读法 —— 简报缝此前只说了分支名，没有说它指哪个 ref

**Scope:** the dispatch-time briefing-append seam (`cmd/agent/cli_tasks.go`,
beside `tools.TaskGroundCutPointClause`) and the clause added there
(`internal/tools/task_briefing_master_reading.go`). **Verdict: the seam was
correct about everything else and silent about the ONE thing a criterion
phrased against "master" cannot do without — which ref that word denotes.**

## The measured case (epic `01a13af7`, 2026-10-14, host hostb-Laptop = client)

Row `01a13524-4c53-7849-bf94-3ff4ef870bec` was closed against the acceptance
criterion «`devdoc/DOC-INDEX.tsv` 与 anchor-number baseline 用 **master 的版本**».
Read in ONE tree, on ONE day, three different blobs each answered to "master":

| ref | `devdoc/DOC-INDEX.tsv` blob | lines | what it is |
|---|---|---|---|
| `refs/heads/master` | `578ada5ab` | 2502 | the LOCAL integration branch, **183 commits behind** the peer |
| `refs/remotes/gitPushcheck/master` | `4bc6b73d6` | 2533 | the PEER's integration tip, mirrored by the sync lane |
| the ground's cut commit `f2824ff` | `735712e52` | 2540 | the population the ground INHERITED |

The criterion's verdict flips between rows 1 and 2. Re-read on the delivering
run's own ground (`HEAD f2824ff`, 2026-10-15) the same two names, the same two
blobs, and the same flip — only the lag has moved on, because the local ref keeps
stopping while the mirror moves: `refs/heads/master 578ada5ab/2502 lines` vs
`refs/remotes/gitPushcheck/master 735712e52/2540 lines`, **249 commits behind**.
The table above is the day's record; the clause reads whichever pair the tree
holds at dispatch, so the number it prints is that tree's reading, never a
constant.

The closing run disclosed the
ambiguity in its own report (`artifacts/docindex-two-master-readings.txt`) rather
than resolving it, because nothing in the system told it which ref the word meant:
the seam rendered the integration branch as the BARE NAME
`IntegrationBranchName` (`internal/tools/merger_graceful.go:83`, `"master"`) —
the cut-point clause's own sentence reads "The integration branch in this tree is
**master**", which is exactly the ambiguous spelling.

## Why this is a gopi defect and not a briefing-writing one

A payload's prose is the submitter's own and has no generator to mend (the seam's
own header records that: `task_ground_cutpoint.go`). What gopi OWNS is the clause
it appends, and that clause named a branch where the ambiguity lives in **which
ref the branch name resolves to**. Measured: `git rev-parse --short
master:devdoc/DOC-INDEX.tsv` alone is not even a well-formed question without
naming the namespace.

## What was delivered

One additive clause, `[Which \`master\` — the ref a criterion phrased against the
integration branch is measured on]`, appended at the SAME seam and carried on the
SAME record channel (`execRunFacts`) as the four clauses already there. It names,
from named constants: both full ref names, each tip, and the commit distance
between them — and it requires the deliverable to name the ref + sha it took,
because "a branch name is not enough".

Three states, and the third is the one that matters:

- **lagging** (the measured case) — both refs, both tips, `N` behind, and the
  sentence "They are NOT the same commit".
- **aligned** (positive control) — "`refs/heads/master` is AT
  `refs/remotes/gitPushcheck/master` — 0 commit(s) behind", so the clause cannot
  cry lag on a tree that has none.
- **unmeasured** — it names what could not be read **and explicitly forbids the
  fallback** ("do NOT fall back on refs/heads/master as though it were the only
  master there is"), which is the substitution that produced the epic (Rule 15).

## Zero new dependencies (Rule 35)

No new ref, file, env var, setting or CLI verb. Both refs already exist and are
already maintained: the local integration ref is whatever a checkout is on, and
the mirror ref is the ONE the delivery lane's ancestry gate decides on
(`gitPushbackCheckRef`, `internal/tools/task_sync_git_pushback.go:67`) — read here
through its own constant rather than minted a second time. The count reuses the
package's ONE relative-refspec spelling (`integrationClaimRelativeRefspecSeparator`)
and its ONE "unmeasurable" verdict (`gitSyncLagUnknown`), so the alarm
`gopi task push` already prints and this clause cannot disagree about what
"behind" means. The read goes through the package's ONE bounded git runner
(`mergeVerifyStaleGit`), never a second prober.

## Where the honest boundary is

- The clause NAMES the ambiguity; it does not resolve it, and it must not — only
  the criterion's author knows which ref they measured. That is why the clause
  prescribes naming a ref+sha rather than picking one.
- The peer mirror is a *mirror*: a host that has not fetched recently reads a
  mirror that is itself behind the peer's live tip. The clause therefore speaks
  about the two refs IN THIS TREE (the same pair the delivery lane decides on),
  never about a tip nobody here has read.

## Verification

`go build ./...` OK, `go vet ./internal/tools ./cmd/agent` OK,
`go test ./internal/tools/ -run TestMasterReadingClause` 3/3 PASS (RED /
POSITIVE-CONTROL / UNMEASURED), and the clause was read out of a REAL worker
child's first model request (`cmd/agent/task_ground_announcement_e2e_test.go`).
The report with the `Ran:` lines is under `$GOPI_TASK_REPORT_PATH`.
