<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# 「Rule 18 的 NEW 违规只在这个 branch 上」是一个 ref-relative 前提 —— 同一个 ref 名的三种读法

**Class**: historical (this run's record; the manifest row and the banner agree —
`python3 tools/docclass/classify.py . --add-missing --only <this path>`).
**Measured on** `dc1743222` (= this branch's HEAD = `refs/remotes/origin/master`
mirror = the peer's own `master`, all three compared in one command set) at 2026-10-13 14:28–14:33 +0800.
**Generator**: `~/.gopi/sessions/01a1336c-a732-7682-bf6f-2485d60df7c5/ref-inventory.txt`
(`git rev-parse` / `git ls-remote` / `git merge-base --is-ancestor` per ref / `git show <rev>:FILE | sed -n 228p`),
re-runnable as-is; landing analysis in `landing-merges.txt` + `red-window-on-master.txt`; the three
gate runs in `red-c5306a23e.txt`, `green-dc1743222.txt`, `frozen-local-master-64b8a316a.txt`.

## The premise adjudicated

The correction request carried this premise, and the epic's job was to decide its truth
value on explicit revisions (never a bare `master`):

> `749d35a1c` 只在 ground `task/01a1330b65ca7…` 的 hand-over merge `c5306a23e` 上，
> master 無此字节；该 branch 一旦合并就会让 master 的 `internal/standards` 变红。

It is **FALSE as stated**, in the direction that matters: it concluded "not on master" from a ref
that was two hours stale and 56 commits behind — a ref in which the commit that introduced the
violation does not exist yet. The byte it was looking for **was on master the whole time**.

## The three readings of one name

One checkout holds three refs that all read as "master". They disagreed by 56 commits, and only the
first line of this table is the integration branch:

| ref | commit | authored | `git merge-base --is-ancestor 749d35a1c <ref>` | `main.go:228` |
|---|---|---|---|---|
| `refs/heads/master` (local branch) | `64b8a316a` | 2026-10-13 10:22:49 | **rc=1** (absent) | `workerControlCommand(args[1:])` |
| `refs/remotes/origin/master` (fetch mirror) | `2e5b4b942` | 2026-10-13 12:01:26 | rc=0 (present) | **`} else if !deferred {`** |
| **true master tip** — `git ls-remote origin refs/heads/master` = `ssh peerbox 'git rev-parse master'` = `HEAD` | `dc1743222` | 2026-10-13 14:19:03 | rc=0 (present) | `workerControlCommand(args[1:])` (b02c95337 landed) |

```
$ git rev-parse HEAD refs/heads/master refs/remotes/origin/master
dc174322286d97dcc7fc3fce0c5d521c4f754b9f      <- HEAD == the peer's master, checked below
64b8a316af16c3ab6333ec06d65e868000815652      <- the FROZEN local branch
2e5b4b942072352ea80e385cd51a6fe0c1a714f6      <- the fetch mirror
$ git ls-remote origin refs/heads/master
dc174322286d97dcc7fc3fce0c5d521c4f754b9f	refs/heads/master
$ ssh peerbox 'cd /home/user/work/GoPi && git rev-parse master'
dc174322286d97dcc7fc3fce0c5d521c4f754b9f
$ git rev-list --count refs/heads/master..refs/remotes/origin/master
56
```

`HEAD` and the peer's `master` are the **same commit**, so every reading below is a reading of the
real integration branch, not of a local checkout's opinion.

## Why the premise concluded the opposite

The violation is a one-line fact on master's **first-parent** line, and the frozen ref predates its
introduction — so the read is not merely stale, it is stale in the direction that inverts the verdict:

| event | commit | time | first-parent index on true master |
|---|---|---|---|
| the `else if` enters the worker control chain (violation created) | `749d35a1c` (`fix(worker): defer a bare resume-ground selector to the resume face`) | 10:57:44 | — |
| that commit reaches **master** (merge `b99fc60f2`) | — | 11:30:04 | 21 |
| the branch the premise named lands (merge `c5306a23e`) | `c5306a23e` | 11:49:49 | 18 |
| the violation is **fixed** (`b02c95337`, merge `153c0942e`) | — | 13:42:48 | 4 |
| this run's baseline | `dc1743222` | 14:19:03 | 0 (tip) |

`git blame -L 226,229 c5306a23e -- cmd/agent/main.go` attributes line 228 to `749d35a1c8`, and
`git merge-base --is-ancestor 749d35a1c refs/remotes/origin/master` → **rc=0**: the violating byte was
already master history **20 minutes before** the branch the premise blamed. The premise's causal
arrow ("this branch, if merged, will make master red") is the reverse of the measurement: master went
red **at 11:30:04**, was red across **14 consecutive first-parent merges** (11:49:49 → 13:42:48,
1 h 53 m — `red-window-on-master.txt`), and that window **closed at 13:42:48**, before this run began.

## The gate, run for real on each ref (Rule 31)

Each ref was materialised with `git archive <rev> | tar -x -C /dev/shm/…` (Rule 29: no checkout of
anyone's worktree; Rule 22: the temp root is on tmpfs, a different device from `/`) and the gate run
in place:

| ref | command | result | exit |
|---|---|---|---|
| `c5306a23e` (has `749d35a1c`, lacks `b02c95337`) | `go test -count=1 -run TestStandardGates ./internal/standards/` | `Rule 18 …: 1 new (unannotated)` / `NEW violation (checklist 4.3) at cmd/agent/main.go:228` — **RED** | **1** |
| `dc1743222` (true master tip) | same | `ok  github.com/nick/gopi/internal/standards  8.356s` — **GREEN** | **0** |
| `refs/heads/master` = `64b8a316a` (the *other* ref the premise cites) | same | `ok  github.com/nick/gopi/internal/standards  9.230s` — **GREEN** | **0** |

## The verdict on the row, by reference

`01a13367-6ea6-7c8b-83b6-82833bcaf5b6` itself reported: *"a NEW unbaselined Rule 18 violation at
`cmd/agent/main.go:228`; `internal/standards` red on a pristine tree"*, measured on `c5306a23e`, and
it correctly labelled the violation **pre-existing** (`749d35a1c` in that ground's base). **That
statement is TRUE** — the first row of the gate table reproduces it exactly, including the line
number. Its one unowned qualifier is *"on a pristine tree"*: `c5306a23e` is not an arbitrary pristine
tree, it is **commit 18 of master's own first-parent line** — a **master** state, red, mid-window.

The **false** premise is the correction's own restatement, adding *"only on …, master 無此字节 … this
branch, if merged, turns master red"*:

- "`749d35a1c` is only on `c5306a23e`" — **FALSE**. rc=0 against `refs/remotes/origin/master`
  (`2e5b4b942`) and against the peer's `master` (`dc1743222`); it landed via `b99fc60f2` (11:30:04).
- "master has no such byte" — **FALSE for the integration branch** (the byte is at
  `origin/master:cmd/agent/main.go:228`), **TRUE for `refs/heads/master`** (`64b8a316a`, 56 commits
  behind, authored before `749d35a1c` existed). The premise was measured, correctly, on a ref that
  could not answer the question.
- "merging that branch will make master red" — **FALSE as a forecast**. It was merged (first-parent
  index 18) and master **was** red — for 1 h 53 m, ending at 13:42:48. Today's master tip is green
  (exit 0, table above).

## Not a new defect — and deliberately no new bug row

The mechanism is already filed and closed: **`01a12e64-23c3-7c10-a07c-b85aef54924d`** (*"客户端 sync
pass 的「pull master」无 refspec —— fetch 腿推新 refs/remotes/origin/master，merge 腿只快进 cwd 当前
分支，本机 refs/heads/master 永不前进"*, `completed` 2026-10-12 13:41:10) — the refspec-less client-sync `git pull --ff-only` step of
`GitSyncClientPass` (step name const `gitSyncStepPullBranchName = "pull-master"`, `--ff-only` const
`gitSyncPullFFOnlyFlag`, both in `internal/tools/task_sync_git.go`). The same artifact is already
distilled in
memory records #9146 / #9151 / #10289, and the measurement here is a fourth instance of the same shape,
not a distinct failure. Per **Rule 41** a duplicate would cost one agent's time and buy nothing; per
**Rule 28** the fix is system-layer and belongs to the server host. **No new bug row was filed for it.**

## Measured vs inferred (Rule 41 §3)

**Measured** (commands + raw output in the evidence files named above): every ref/ancestor/line-text
reading; all three gate runs with their exit codes; the first-parent landing indices and the red window
(14 commits, 11:49:49 → 13:42:48).

**Inferred, and marked as such** — the *cause* of `refs/heads/master` being frozen is attributed to the
already-filed `01a12e64` mechanism (refspec-less `pull --ff-only` merging the cwd's branch). This run did
**not** re-derive that mechanism; it cites the filed row and the memory records. What this run measured is
only the *effect*: the local branch is 56 commits behind the peer's master and does not contain a commit
authored at 10:57:44.

## The habit this run is the fourth instance of

A claim whose subject is "master", "the integration branch", or "the branch as delivered" is
**ref-relative**, so it is unverifiable until it names a revision. The operable rule: state the ref, and
prefer the one the delivery lane actually decides on — `git ls-remote <peer> refs/heads/master` (or a
fetched `refs/remotes/origin/master`), never the local `refs/heads/master` on a client host. The
ancestry question is then one call per candidate: `git merge-base --is-ancestor <commit> <ref>; echo $?`.
