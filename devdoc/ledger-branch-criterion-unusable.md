<!-- doc-class: normative -->
# 账本判据实测：`deliverable.branch` + `merge-base --is-ancestor` 在本机账本上可用行数为 0

**结论先行：这条判据不可用。** 不是脚本写错，是问句本身在本机账本上退化了。
下面每个数字都是在本机 `~/.gopi/memory.db` 上**只读**测得（`mode=ro&_pragma=query_only(true)`），
一条行的状态都没有改。

## 实测（2026-10-26，229 条 `failed` 行）

```
failed_rows=229  deliverable_absent_or_bad=41  branch_empty=67
                 branch_is_master=114  ref_gone=7  usable=0
```

| 情形 | 行数 | 判据为什么不成立 |
|---|---|---|
| `deliverable` 缺失或不可解析 | **41** | 没有可读的分支字段 |
| `deliverable.branch` 为空 | **67** | 没有可判的 ref |
| `deliverable.branch` 就是 `master` | **114** | `merge-base --is-ancestor master HEAD` **恒真** —— 无论该行产出在不在，都答"已落地" |
| ref 已不存在（`git rev-parse --verify --quiet` 无输出） | **7** | 判据无法执行：`merge-base` 会报 `Not a valid object name` |
| **可判** | **0** | —— |

**根因。** `deliverable.branch` 在大量行里记的是**集成分支**（`master`）而不是该行自己的
工作分支 —— clone 通道的 ground 本身就站在 `master` 上，写回时如实记了它。于是
"这个分支是不是 HEAD 的祖先"这个问句退化成"master 是不是 master 的祖先"，恒真。

**这不是一个能靠改查询修好的 bug**：信息不在账本里。要回答"某条 failed 行的产出在不在
master"，需要的是**提交与行之间的关联**，而不是分支字段。

## 因此 trailer 才是那条判据

`task: <id>` trailer（见 `devdoc/commit-carries-its-ledger-row.md`）把"这条提交属于哪一行"
变成提交正文里的事实，于是

```bash
git log --grep="task: <某行 id>" --oneline
```

成为唯一需要的索引 —— 不读账本字段、不需要任何新表或索引文件（Rule 35 / Rule 5b）。
它同时修掉了上表 41 + 67 + 7 = 115 行"根本没有可判 ref"的那半个问题：
提交一旦带上 trailer，即使它的分支早就被归档删除，命中依然能查出来。

## 复现（只读，安全）

```bash
# 在 gopi 仓库内放一个临时测试（读 ~/.gopi/memory.db，只读），跑完即删
go test ./internal/tools/ -run TestTmpLedgerBranchCensus -count=1 -v
```

测法：`SELECT id, status, deliverable FROM tasks WHERE status='failed'`，
对每个 `branch` 先分类（空 / `master` / 其它），再对"其它"逐个
`git rev-parse --verify --quiet refs/heads/<b>`，只把成功的计入 usable。

## 诚实边界

- 只统计了 `failed`；其它状态的行没有测，本文件不对它们下结论。
- 行数随账本增长而变；本文件记录的是**测量时刻 2026-10-26** 的值，重跑会得到新数
  （Rule 38：数字带它的生成方式与时刻）。
- epic 简报里写的是 "202 条 failed 行"，实测是 **229** 条 —— 简报的数字偏旧，
  本文件以实测为准。
