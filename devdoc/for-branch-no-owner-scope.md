<!-- doc-class: historical -->
# `gopi task for-branch`: a scoped denial, and the git channel labelled


**Class: historical.** This is the record of ONE run (task
`01a0ed38-fecc-70e9-9c2b-14bdef7887af`, 2026-09-29) that repaired a
self-contradicting answer. It describes the measured defect, the change, and the
evidence — not a standing contract. The standing contract lives in the verb's own
help (`gopi task -h`, the `for-branch` entry's EXIT CODES table) and in the two
tests the change ships.

## 1. The defect, measured

`docs/archive/demo/materials/09-for-branch/02-output.txt` is the operator's own capture
(gate epic `01a0ecf5ec917c23b454765e46482661`) of the verb answering a branch
that git registers and the ledger did not:

```
no owner: no ledger row names branch task/01a0ecf5ec917c23b454765e46482661
  (0 rows carry a branch; none names this one)
  reconciled against the git side: repository /home/user/work/GoPi registers the branch
  task/01a0ecf5ec917c23b454765e46482661 at /home/user/workspace/gopi-01a0ecf5… (50f70a6c…), and …
```

Two findings that contradict each other **in one block**:

1. a **flat absolute denial** — "no owner: no ledger row names branch X", and its
   count line "0 rows carry a branch; none names this one";
2. immediately under it, a sentence proving **git registers the branch as a live
   ground**.

Only the second is scoped, and it arrives as a footnote under a verdict the
reader has already formed. `docs/archive/demo/materials/COVERAGE.md` records the same
finding as `F9.1`, and `09-for-branch/05-finding.md` spells out why the reader
cannot recover from it: the first sentence has already landed as a claim about
the machine; the second reads as an explanation of "why git has something the
ledger does not", not as a correction.

Both readings were individually true. What was missing was **scope**: "no ledger
row names X" is a statement about ONE database, and this host carries one ledger
per profile — the operator's profile, and every sandbox profile
(`Rule 34`). The 50f70a6c capture was taken from a **sandbox profile** whose own
ledger had never been dispatched into, while the branch it asked about was
registered by the runner in the shared repository. One of the two channels was
about the ledger it opened; the other was about the host.

**The measured window** (the regression test reproduces it): the runner has
created the worktree and the branch (`git worktree add … -b task/<id>`, so git
registers the ground), while no row of the ledger under test names that branch —
either because the row is still **repo-only** (the claim-time stamp
`taskClaimGroundStamp` reaches a row only on the dispatch path, so an ordinary
row carries `{"repo":…}` and nothing else until completion), or because the row
lives on **another profile's ledger entirely**.

## 2. What changed

### 2a. The claim is scoped to the ledger that answered

`TaskForBranchLookup` now carries `LedgerPath` — the ONE resolved path the reader
opened (the caller's `--db`, else `memoryDBPath()`), assigned once and rendered
once. `TaskForBranchReport` prints it directly under the verdict:

```
no owner: no ledger row names branch feat/nobody-owns-this
  ledger: /home/user/.gopi/memory.db — this LEDGER only (a host carries one per profile; --db names another)
```

### 2b. "Empty ledger" is distinguished from "populated, none names it"

`LedgerRowsTotal` is the ledger's **own** row count (every row, not only the
deliverable-carrying ones), read on the handle the scan already opened
(`taskForBranchCountRows`) — never a second opener, because opening has a cost and
a write effect (`ledger_read_guard.go`). Three cases now render differently:

| measured | rendered |
|---|---|
| no ledger file / 0 rows | `(that ledger holds no row at all — a fresh profile: nothing has been dispatched into it, so no row of it can name anything)` |
| rows exist, none carries a branch | `(that ledger holds 2 row(s), and not one of them carries a branch — nothing here can name a selector at all)` |
| rows carry branches | `(1486 rows carry a branch; none names this one)` |
| the count could not be read | `(that ledger's own row count could not be read, so whether it holds rows at all is UNKNOWN)` |

The last row is `taskForBranchRowsTotalUnknown` (`-1`) — a sentinel, not 0, for
Rule 15's reason: **"the ledger is empty" is itself a claim**, and an
unmeasurable total may not make it.

### 2c. The git registration is a SECOND, LABELLED channel

The reconciliation answer now populates `TaskForBranchLookup.Ground` — a
`TaskGroundRegistration{Kind, Repo, Worktree, Branch, Tip}`, read out of the pass
that already measured it (no second `git worktree list`). The renderer prints it
as its own channel, under its own label:

```
no owner: no ledger row names branch task/01a0ed38…
  ledger: /home/user/workspace/gopi-sandbox/sandbox-for-branch-1a5131fb/.gopi/memory.db — this LEDGER only (…)
  (that ledger holds no row at all — a fresh profile: …)
git registers this branch — a ground that NO row of that ledger owns (read from git NOW, never recorded in the ledger):
  /home/user/work/GoPi registers branch "task/01a0ed38…" at /home/user/workspace/gopi-01a0ed38… (tip 014a6f5a…)
  reconciled against the git side: … the 0 row(s) of that repository alive when the registration was created record no session …
```

`TaskGroundChannelLabel` states **the instrument** (git, read now) and **the
finding** (a ground no row of *that* ledger owns) in one line, before the facts —
so it cannot be read as a footnote, and a script can take the channel from JSON
(`"ground":{…}`) without parsing the note.

The channel is emitted only when a **LIVE registration was actually measured**
(`ans.GroundKnown`); a lookup that measured nothing grows no hedge. And when the
reconciliation names a row, the channel is **dropped** (`taskGroundApplyReconcile`)
— a reconstructed owner *is* an owner, and leaving the unowned-ground channel
standing beside an owner block would be the same contradiction in the other
direction.

### 2d. Why the field, not a sentence

The readings come from two **instruments** — the ledger that was opened, and
`git worktree list` run right now — answering two different questions. Carrying
them as one prose block is what produced the defect; carrying them as two fields
lets each be labelled, rendered and consumed on its own. The reconciliation
**note** stays what it was (Rule 15b: what was looked for, and where) — it is the
reading, not the verdict.

### 2e. The worktree sibling, folded the same way

`TaskForWorktree` (the `--worktree` spelling) shares the answer shape, so it
shares the fix — through ONE fold, `taskGroundApplyReconcile`, rather than a
second copy free to diverge (Rule 14). It also gained the **read guard** the
branch sibling has had since `ledger_read_guard.go`: it was the last member of
the family that would build the ledger it was reading. Its no-owner claim is
scoped and sized the same, and the worktree selector's channel value is the
**directory** (`taskGroundRegistrationValue`), never the branch — the answer must
name the thing that was asked about (Rule 15).

## 3. The exit-code table, measured

The help said only "the exit code stays non-zero", while the verb really answers
`4`. `docs/archive/demo/materials/09-for-branch/05-finding.md` filed that as `F9.2`: a
caller cannot write a script against an undocumented code.

The measured algebra, run on the built binary:

| code | meaning | measured |
|---|---|---|
| `0` | the branch has an owner (recorded or RECONSTRUCTED) | `exit=0` on this run's own branch |
| `4` | the ledger WAS read and no row of it names the branch | `exit=4`, three separate runs |
| `1` | usage error: no positional, two positionals, unknown flag | `exit=1`, three separate runs, refusal on stderr |
| `1` | the ledger could not be READ at all (path on stderr) | `exit=1` (`TestTaskVerbForBranchRunsAsAdvertised`, blocker fixture) |
| `2`, `3` | **not used by this verb** | asserted absent in the help |

`2` and `3` are taken by the surrounding conventions (`workerUnexpectedArgExitCode`
= 2, the agent guard refusal = 3), and a collision there would make a script
unable to tell "no owner" from "I was not allowed to look" (Rule 15). The help now
carries the table, and `TestTaskVerbForBranchRunsAsAdvertised` asserts **each line
by name, inside the `for-branch` entry** (not merely somewhere in the family
help), plus the two codes the verb really returns for usage — so a table that
drifted from the binary fails the build.

## 4. Verification

- `go build ./...` — clean.
- `go test ./internal/tools/ -run 'TaskForBranch|GroundReconcile|DeliverableSelector'` — ok.
- `go test ./cmd/agent/ -run 'TestTaskVerbForBranchRunsAsAdvertised|TaskHelpSurface'` — ok.
- Real runs: §5 of the task report (`/home/user/.gopi/sessions/01a0ed38-fecc-70e9-9c2b-14bdef7887af/task-report.md`).
- The regression test `TestGroundReconcileScopesTheNoOwnerClaimAndLabelsTheGitChannel`
  reproduces the measured window (repo-only rows + a git-registered branch);
  `TestGroundReconcileEmptyLedgerSaysItIsEmpty` pins the empty-ledger sentence.

## 5. Not done

- The **row-side** write is not touched: `TaskForBranch` still reads, and only
  reads. The measured window's other half — a row whose ground is stamped only on
  the dispatch path — is a producer-side question (when `taskClaimGroundStamp`
  runs, and whether a non-dispatched run should record a ground at all) and is
  deliberately out of this verb's contract. This change makes the VERB honest
  about what it read; it does not make the ledger record more.
- `--repo` narrowing still compares repo strings exactly (`taskForBranchNoOwnerFmt`
  prints "in repo R — drop --repo to look across repos"). Left as found.
- The channel reports the registration's repository, ground, branch and tip. It
  does NOT report whether that worktree is dirty or how old the registration is —
  both are covered by the `gopi task show --branch` face's own git block
  (`task_deliverable_git.go`), and duplicating them here would create a second
  reader of the same facts (Rule 14).
