<!-- doc-class: historical -->
# Merge review: `feat/zz-r31-orphan` — refused, orphan root (2026-09-27)

**Verdict: NOT MERGED.** The branch is an orphan root with no common ancestor to
master and is already archive-retired. master was left byte-identical.

Task: merge `feat/zz-r31-orphan` (tip `049e626c`) into master.
Run worktree: `/home/user/workspace/gopi-01a0e0a31f22738ab9b3bc27dbb2e3cd`,
branch `task/01a0e0a31f22738ab9b3bc27dbb2e3cd`.

## The measurement that decided it

`git diff --stat master...feat/zz-r31-orphan` **cannot run**:

```
fatal: no merge base
```

`git merge-base master feat/zz-r31-orphan` prints nothing and exits **1** — the
two refs share no commit. Git accepts such a merge only under
`--allow-unrelated-histories` (`cmd/agent/merge_run.go:146`), which
`briefings/brief-merge-unrelated-history-retirement.md` requirement 6 forbids.

| instrument | reading |
|---|---|
| `git merge-base master feat/zz-r31-orphan` | *(empty)*, `exit=1` |
| `git rev-list --parents -n1 feat/zz-r31-orphan` | `049e626cd05b…` — **no parent field** = root commit |
| `git ls-tree -r --name-only feat/zz-r31-orphan` | `orphan.txt` (1 file) |
| `git ls-tree -r --name-only master \| wc -l` | `5227` |
| `git diff --stat master 049e626c` | `5228 files changed, 1 insertion(+), 1101688 deletions(-)` |
| `git merge-base --is-ancestor 049e626c master` | NO |
| `git for-each-ref --contains 049e626c` | `refs/heads/feat/zz-r31-orphan`, `refs/tags/archive/feat/zz-r31-orphan` |

The branch's whole history is **one commit** (`049e626c an orphan root`, author
`t <t@t>`, `2026-09-27 10:12:58 +0800`) containing **one file**. There is no
payload to preserve and nothing to re-land. Merging it would delete 5226 of
master's 5227 tracked files — the entire Go tree — in exchange for one line.

## The gate the briefing asked for

```
$ bash scripts/safe-merge.sh feat/zz-r31-orphan
== safe-merge 守卫: feat/zz-r31-orphan ==
❌ 拒绝: 分支 'feat/zz-r31-orphan' 已有存档 tag 'archive/feat/zz-r31-orphan' —— 已归档(败者)，禁止 merge
✅ [b] 分支名黑名单: 'feat/zz-r31-orphan' 无违规前缀
✅ [c] worktree 占用: 无 worktree 挂着 'feat/zz-r31-orphan'
ℹ️  [d] 分支 'feat/zz-r31-orphan' 尚未 merge 进 master
✅ [e] ref 存在性: refs/heads/feat/zz-r31-orphan 可解析
❌ 拒绝: 与 master 无共同历史 (孤儿根 —— git merge 只在 --allow-unrelated-histories 下才接受) —— 禁止 merge
❌ safe-merge: 存在拦截项 —— 禁止 merge 'feat/zz-r31-orphan' (拒绝种类: archive-tag unrelated-history)
$ echo $?
1
```

Two independent predicates refuse: the archive tag and the unrelated history.
Note this is a **strengthening** of the guard worth recording — an earlier
version of `safe-merge.sh` had four predicates (archive-tag / loser|archive
prefix / worktree occupancy / merged status) and was blind to unrelated history;
this one carries `[e]` and an explicit orphan-root arm, so a green/red reading
here is decisive in a way it was not before.

## Already retired — nothing to add

The branch carries `refs/tags/archive/feat/zz-r31-orphan` at the **identical**
commit (`git rev-parse 'archive/feat/zz-r31-orphan^{commit}'` →
`049e626cd05b3833f4459be2074ccd1c2819172c`). That is the durable, idempotent
retirement implemented for exactly this verdict
(`cmd/agent/merge_run.go:813`, epic `unrelated-history-durable-retire`), and
`repoSweepArchiveExempt` (`internal/tools/repo_sweep.go:624`) is the predicate
every merge/cleanup path uses to honour it. Re-tagging would be a no-op; deleting
the branch would destroy the recoverable evidence the retirement deliberately
keeps.

## Why a row was filed anyway

The tip's author is synthetic (`t <t@t>`) and its timestamp (`10:12:58`) sits six
minutes before the archive tag recorded for it and nineteen minutes before
master's own `10:31:54` tip. The branch name carries the `zz-` sort-last marker
and `r31` (Rule 31): this is an **orphan-root verification fixture** from the
retirement work itself, not a contributor's branch.

The merger's own candidate scan honours `repoSweepArchiveExempt` and skips an
archive-tagged branch — a hole measured on 2026-09-19 and closed (see
`internal/tools/task_merge_graveyard_refusal_test.go`). Whatever filed **this**
row did not. That is the generator-side half of
`briefings/brief-merge-unrelated-history-retirement.md` requirement 7: *"whichever
code files `merge feat/<x> into master` rows must not file a row for a branch
that is already retired (`archive/`-tagged)"*. The residue is on the row-filer,
not on the merge path — which is why this run's deliverable is a refusal plus
this record, and not a code change.

## Residue dispatched

Two items belong to other surfaces and were not touched here: (1) the row-filer
honouring `repoSweepArchiveExempt` above; (2) whether the synthetic fixture
branch should exist in the real repository at all, since a live merge queue
rediscovers it every pass. Both are recorded in this run's report under
"Dispatchable residue".

## Verify

- `master` unchanged: `git rev-parse master` →
  `f8543d47fe7d9acf86e09ec24c5bb0523143d284` (reflog `master@{0}` is still the
  operator's earlier `Merge branch 'fix/removal-gate-arm-scope'`).
- `git status --porcelain` in the run worktree prints nothing.
- No half-merge: `.git/MERGE_HEAD` absent.
- The refused branch still resolves: `git rev-parse --short feat/zz-r31-orphan`
  → `049e626c`.
