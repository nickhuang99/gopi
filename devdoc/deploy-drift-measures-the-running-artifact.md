<!-- doc-class: normative -->
**Subject**: `gopi version --drift`, the read-only report that answers "how far
is the deployed gopi behind the branch it should have been built from?" — and
the two false-clean readings it shipped with.

Motivating row: `01a1827b-429e-7c76-baee-b78483124606`. The deployed
`/usr/local/bin/gopi` sat at revision `84beb309ce3c` while the main checkout's
`refs/heads/master` had moved 67 commits and the task ground's `origin/master`
had moved on too. The run that filed the row blamed a *different* defect — a
ground line rendering a false `branch … (NOT FOUND now — a stale reading)` — and
that one is real but was already **fixed upstream** (`f64c703`,
`internal/tools/task_ground_branch_probe.go`). This document is about the second,
larger defect the row named: **the build leg of Rule 12 let "the deployment is
behind" stay invisible**, so nobody could see the distance without running
`git rev-list --count` by hand.

## 1. What already existed, and why that is the first finding

`gopi version --drift` is **not new**. `internal/tools/deploy_drift.go` and the
`--drift` flag on `cmd/agent/cli_version.go` were already in the deployed
revision `84beb30`, and the deployed binary answers with real numbers:

```
$ /usr/local/bin/gopi version --drift
v0.3.0
go: go1.24.0
platform: linux/amd64
revision: 84beb309ce3c90875e85142eb12769564778ea45
milestone: 2026-10-25T06:16:00Z
drift-repo: /home/user/work/gopi
drift-artifact: /home/user/work/gopi/bin/gopi
drift-revision: 84beb309ce3c90875e85142eb12769564778ea45
refs/heads/master:  67 behind (this checkout would build from 5c51bf53d64b2ed1c69a0da5780e162e55c45f42)
refs/remotes/gitPushcheck/master:  0 behind (this checkout would build from b7a1ff886776665d31684385de57b63f0bdec5ce)
STALE: …
```

So the epic's deliverable ① ("a read-only command that prints the distance") was
**already shipped**. The honest answer to "build it" was therefore not to build
it again but to **measure what it actually reports** — and that measurement found
two defects, one of them a false-clean that would have misled exactly the
operator this row was filed for.

Evidence: `~/.gopi/sessions/01a1827b-…/evidence/deploy-drift-false-clean.txt`.

## 2. Defect A — "0 behind" is not a clean verdict

`deployDriftCommitsBehind` measured ONE distance:
`git rev-list --count <artifact>..<ref>`. On the deployed artifact against the
peer mirror that number was **0**, and the report printed `0 behind` — a true
number that reads as "clean" and was completely wrong:

```
$ git merge-base --is-ancestor 84beb30 refs/remotes/gitPushcheck/master; echo rc=$?
rc=1                                    # NOT an ancestor — the artifact is from another line
$ git rev-list --count 84beb30..refs/remotes/gitPushcheck/master
0                                       # …which is why "0 behind" was printed
$ git rev-list --count refs/remotes/gitPushcheck/master..84beb30
284                                     # the artifact is 284 commits AHEAD of that ref
```

`behind == 0` cannot distinguish *follows the ref* from *diverged from it*, so
the report now measures **both** directions (`DeployDriftRef.Ahead` +
`.Behind`) and derives a **verdict** (`DeployDriftRef.Verdict()`):
`follows the ref` / `behind` / `ahead` / `diverged`, plus the stated reason when
the measurement could not be taken. Four named constants
(`DeployDriftVerdict*`), so the print site and the tests read the same decision
instead of each re-deriving "behind == 0 means fine".

## 3. Defect B — the report measured the wrong file

`printDeployDrift` probed `os.Getwd()`'s `bin/gopi`. Started from a task ground,
that is **that ground's own fresh build**, not the deployed binary:

```
$ cd /home/user/workspace/<task-id> && gopi version --drift   # pre-fix
drift-artifact: /home/user/workspace/<task-id>/bin/gopi
refs/heads/master:  follows the ref — the artifact was built from it
```

Every word of that was true about the ground's build and a false answer to the
question asked, because `/usr/local/bin/gopi` is a **symlink** into
`/home/user/work/gopi/bin/gopi` — the file the machine actually executes.

The report now measures **the running artifact**: `cmd/agent` passes
`os.Executable()` and `tools.DeployDriftRepoOf` resolves the repo from that
path's own `<repo>/bin/gopi` shape — **and requires the `<repo>` level to
actually carry a `.git` entry** (directory or the linked-worktree file; the name
comes from `devworkspace.GroundGitEntryName`, the project's one spelling).

That `.git` requirement is there because the first version of this function got
it wrong and its own test caught it: `/usr/local/bin/gopi` has a `bin` level like
any build output, so a **shape-only** rule answered `/usr/local` — a directory
that holds no repository. The fallback (the current directory) is still
available, but it is now **stated in the output** as
`drift-repo-source: … the CURRENT DIRECTORY was used …`, never passed off as the
artifact's own repo (Rule 15).

`origin/master` joined the measured refs (`DeployDriftRefs`) — it was the ref
the row's own `27 commits` number came from and it was absent from the report.

## 4. The red proofs, and the one that initially survived

Two mutations, both applied to the fixed tree and reverted by **content
identity** (`sha256sum` against a snapshot, not `git checkout`):

1. **Drop the `ahead` measurement** (the shipped shape) →
   `TestDeployDrift_AheadIsNotReportedAsZeroBehind` and
   `TestDeployDrift_BothDistancesNonZeroIsDiverged` fail with
   `Ahead = 0, want 1 … 'behind == 0' is NOT a clean verdict`.
2. **Restore the shipped print shape** (`<N> behind`, nothing else) →
   **SURVIVED** `./internal/tools/`, because the tool layer was tested and the
   print site was not.

Mutation 2 surviving is the useful result, and it is why
`cmd/agent/cli_version_drift_test.go` grew
`TestDeployDriftRefLine_StatesEveryVerdict`: it drives the print function across
all five verdicts and **denies** the string `0 behind` for the ahead and diverged
cases. With it in place mutation 2 fails with

```
line "0 behind" must NOT say "0 behind" — that is the shipped false-clean
```

Lesson (Rule 31's shape, applied to a red proof): **a mutation that survives
tells you which layer you forgot to test.** Asserting the computed value and not
the sentence an operator reads leaves the sentence free to reproduce the defect.

## 5. Two states, really run

`--drift` needs a binary whose report *attests a revision*, and a plain build
from this tree attests none (`modified: true` — the tree was dirty during the
work, so `binaryVersion` drops the revision deliberately). The two states were
therefore demonstrated with two real binaries in an evidence directory, each
reporting the state the epic names:

```
# N=0 — the artifact attests exactly the refs' tip
$ ./bin/gopi version --drift
drift-revision: 5c51bf53d64b2ed1c69a0da5780e162e55c45f42
refs/heads/master:  follows the ref — the artifact was built from it — 5c51bf53…

# N behind — the artifact attests an ancestor revision
refs/heads/master:  3 behind (the ref is at <tip>)
```

Artifacts and transcripts:
`~/.gopi/sessions/01a1827b-…/evidence/state-N0.txt` and `state-Nbehind.txt`.
The build recipe is the one `gopi version`'s own usage text documents
(`-ldflags "-X github.com/nick/gopi/internal/version.Revision=<sha>"`), so a
linked-worktree-shaped build can still attest a revision — that is a
**declaration**, and the report labels it as such.

## 6. What this does NOT do

- **It does not rebuild anything.** Rule 35: the machinery that rebuilds after a
  landing already ticks (`mergerPassCloseLoop → serveRebuildAfterMerge`). This
  adds no daemon, no cron line, no script and no new file on disk — one flag on
  an existing verb, read-only, which creates no session and no ledger row.
- **It does not rebuild the deployed binary.** That is the operator's action
  (`/home/user/work/gopi/bin/gopi` lives in the main checkout, not in this
  task's ground). This row's deliverable is that the distance is now *visible*
  and *stated correctly*; the rebuild itself is Rule 12's close loop, run by
  whoever owns that checkout.
- **It does not decide whether a ref is itself current.** A ref can be behind its
  own upstream while the artifact follows it exactly; the report says so in its
  own remedy line rather than pretending to answer it.

## 7. Reproduce

```bash
go build ./...
go test ./internal/tools/ -run 'TestDeployDrift|TestDeployDriftRepoOf' -count=1
go test ./cmd/agent/ -run 'TestVersion|TestDeployDriftRefLine' -count=1
go run ./cmd/agent/ version --drift     # the report, about the artifact you just built
```

To reproduce the false-clean on the **deployed** binary directly (the historical
reading, pre-fix):

```bash
cd /home/user/work/gopi
git rev-list --count 84beb309ce3c90875e85142eb12769564778ea45..refs/remotes/gitPushcheck/master  # 0
git rev-list --count refs/remotes/gitPushcheck/master..84beb309ce3c90875e85142eb12769564778ea45  # 284
git merge-base --is-ancestor 84beb309ce3c90875e85142eb12769564778ea45 refs/remotes/gitPushcheck/master; echo "rc=$?"  # rc=1
```

## 8. Cost of getting this wrong

The row exists because a stale deployment produced a **false diagnostic** on a
delivered ground, and the operator's next move (re-dispatching work against a
ground that looked broken) would have been wasted. A report that says "0 behind"
for an artifact 284 commits off the ref has the same cost in the other
direction: it certifies the thing it was built to catch.
