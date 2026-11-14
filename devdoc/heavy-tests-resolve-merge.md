<!-- doc-class: historical -->
# Resolving a parked conflict branch so it is fast-forwardable (2026-09-17)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md — the operator owns this; an agent must never run it (submit the work instead: gopi task submit). -->

Two things this change is about: how the conflicts of `fix/heavy-tests-classification`
were reconciled as **unions**, and what happened when the merger fast-forwarded
master onto the resolved tip *while the verification run was still going* — the
part a future reader most needs, because it looks like data loss and is not.

## 1. The conflict set is not the set the briefing predicted

The predecessor is parked at `archive/fix/heavy-tests-classification` (tip
`5a159274`); rebasing it onto master `0f2cc45c` conflicted on **five** files:

| file | conflict |
|---|---|
| `GOPI.md` | add/add at end-of-Rule-27 |
| `cmd/agent/agent_spawn_guard_test.go` | add/add at the same statement position (3 sites) |
| `cmd/agent/agent_spawn_guard_env_isolation_test.go` | add/add (1 site) |
| `internal/tools/live_claim_worktree_veto_test.go` | add/add in the import block |
| `internal/signalrec/signalrec_test.go` | **no conflict — git auto-merged it** |

The briefing named `internal/signalrec/signalrec_test.go` as conflicted and did not
name `internal/tools/live_claim_worktree_veto_test.go`. Both are wrong for *this*
master tip: a briefing's conflict list is what the merge attempt saw when it was
written. **`git status --porcelain` (UU lines) is the ground truth** — trusting the
list here would have shipped a file with conflict markers still in it.

## 2. Why every conflict is a union, and how that was *proved*

All four real conflicts share one shape: master and the branch each inserted their
own text **at the same anchor**, so git has no basis to order them. Neither side
replaces the other, so the resolution keeps both:

- test bodies: `heavytest.Require(t, …)` **then** `SandboxRepoContext(t)`. The order
  is a choice (the skip decision before fixture work) and is stated rather than left
  implicit; both behaviours are present either way.
- import blocks: both imports.
- `GOPI.md`: the branch's "Where the rule is CHECKED" block completes **Rule 27**
  (it documents Rule 27's mechanical gate and the heavy-test classification), so it
  stays inside Rule 27 and master's `## Rule 28` + `## Rule 29` follow it unchanged.
  Putting it after Rule 29 would have moved Rule-27 text into Rule 29's section — a
  distortion, not a union.

**The proof that nothing was chosen away is mechanical, not a reading**:

```
git diff --numstat 0f2cc45c <resolved>  → 32/2/3/3/2 insertions, 0 deletions
python compare: git show master:GOPI.md from "## Rule 28" to EOF
                == resolved from "## Rule 28" to EOF            → True
                branch block occurrences in resolved            → 1
```

A union resolution of an add/add conflict must be **purely additive** against
master. A deletion in `--numstat` means a side lost text.

## 3. The merger raced the verification run (and this is not data loss)

Mid-run the branch **and** the worktree disappeared and master moved
`0f2cc45c → 17cec86b`. Measured sequence:

- `git cat-file -t 584b88f0…` → `commit` (the object was alive);
- `17cec86b`'s parent is `584b88f0` — master was **fast-forwarded onto the resolved
  tip**, then the branch ref and the worktree were recycled;
- `git merge-base --is-ancestor 584b88f0 master` → true.

Two habits made that recoverable instead of fatal:

1. **Pin the object immediately**: `git tag rescue/<task>-<short-sha> <sha>`. Without
   a ref the sha is reachable only from the deleted branch's reflog, which dies with
   the ref.
2. **Re-read master before concluding anything.** The whole diagnosis is
   `git log -1 --format='%H %P' master` — a fast-forward shows your commit as the
   parent of master's tip.

Verification then continues in a fresh worktree, because the resolved content is now
IN master: `git show master:<file>` hashes identically to `git show <tip>:<file>` for
all five files.

## 4. Baseline-vs-delta, and three ways to fake a delta

- **`git archive master | tar -x` into a plain directory is not a valid baseline.**
  It has no `.git`, so the repo-root junk gates fail
  (`TestRepoRootHasNoJunk`, `TestRepoRootJunkGateScansTheRealIndex`,
  `TestRepoRootJunkPathsAreGitignored`, `TestRepoRootJunkGateScansImmediately`). Use a
  real worktree (`git worktree add --detach … 0f2cc45c`).
- **The baseline must be the PRE-resolution master.** Once the fast-forward landed,
  master *contained* the change; a baseline at the new master compares the change
  against itself and reports a false zero delta.
- **Build `bin/gopi` in BOTH trees**: `testutil.GopiBin` SKIPS when the binary is
  missing, so a baseline without a binary reports a false green.
- **Beware a full scratch device.** `/dev/shm` (7.8G tmpfs) filled during these
  runs and a `go build` inside a test failed with `no space left on device`
  (`copying …/a.out to …/gopi-peak-removal-bin…/gopi`), adding spurious `cmd/agent`
  failures. That is an environmental delta, not a code delta — and it is exactly the
  kind of difference a baseline-vs-delta comparison exists to catch: read the failure
  TEXT, not just the count.
- **Run baseline and resolved SERIALLY, with the scratch root off the shared device.**
  The first pass ran both trees in parallel, both writing to `/dev/shm` — the same
  tmpfs the running merger/worker fleet uses. It saturated, and the resolved side ate
  the extra failures. The corrected pass sets `GOPI_TEST_SCRATCH_HOME` **and** `TMPDIR`
  to a directory on the `/home` device in both trees (verify with
  `stat -c %d <scratch>` vs `stat -c %d /` — the device ids must differ) and never
  overlaps the two runs. Under those conditions `cmd/agent` came out **9 == 9** and
  `internal/testutil` **6 == 6**, with the same package-level
  `panic: test timed out after 10m0s` on both sides. A parallel run of the same two
  suites is not a comparison, it is a race for the scratch device.

`gofmt -l .` likewise has **pre-existing** offenders on master (7 files, unchanged
here), so the criterion is the set delta, not an empty list.

## 5. Left alone deliberately

### One acceptance criterion is unattainable on this master, and saying so is the honest result

The epic's acceptance says `gofmt -l .` must print **nothing**. It prints **8** on the
branch head — and it is not achievable by this task, measured rather than argued:

- a clean worktree at the pre-resolution master `0f2cc45c` prints **7**;
- current master `bf9fbef8` prints **8** (its own new commit adds
  `internal/tools/task_finish_premerge.go`, unformatted);
- the branch head prints the **same 8** — delta attributable to this branch: **0**
  (the branch's diff against master is two `.md` files, so it touches no `.go` file
  at all).

Resolving the conflicts did not introduce a single offender, and `gofmt -w` on master's
pre-existing offenders would put an unrelated repo-wide reformat inside a
fast-forward-only branch — the thing the briefing forbids (`do NOT run make fmt`). So
the criterion is reported as **met in the only sense that is about this change**
(set delta 0) and **unmet literally** (8 ≠ 0), rather than restated as a pass. The
seven baseline offenders are the same seven named in the previous report; the eighth is
`bf9fbef8`'s.

`devdoc/repo-context-gate-reverify.md` carries literal `<<<<<<< HEAD` /
`>>>>>>> fix/heavy-tests-classification` lines **on master** (introduced by
`ce2a416c`) — a leftover of the very merge this task resolves. It is unrelated to
this change and touching it would add a hunk to a fast-forward-only branch; it is
reported instead of silently fixed.
