<!-- doc-class: normative -->

一次 merge run 之后留下 11 条"已决定但没回收"的分支名。任务前提是"跑归档动词把它们删掉"；
实测是"**已经被删了，缺的是 WHY**"。这份记录留下的是**判定顺序**，因为下一次遇到同一形状的
残留列表，第一个问题不是"怎么删"，而是"**它还在不在，谁动过它**"。

## 一、先问 ref 在不在（一条命令，别再往下读之前）

```bash
git rev-parse -q --verify refs/heads/task/<id>   # 空 + rc=1 = 已消失
```

`residual-refs.txt` 这类清单是**某一时刻的快照**。两小时之后，另一条 merge/回收臂可能已经
把它落地了。清单本身不是现状。

## 二、再问"谁动过它"—— `removal-audit.log`，一个文件，五个事实

```bash
grep '"kind":"branch-delete"' ~/.gopi/logs/removal-audit.log | grep -E '<ids>'
```

每条记录带 `arm / repo / target / branch / pid / ts / sha / evidence`。
**这是"ref 消失"唯一可归因的仪器**：`git reflog` 在 `git update-ref -d`（plumbing）
路径上不写、`git branch -d` 以外的删除姿势一个都没留痕。本项目里 ref 的删除只有一个合法
写入者（Rule 9），它必定留下这一行；**没有这一行的消失就是没有留痕的删除**，那不是"回收完成了"，
那是"有人绕过写入者了"。

判别式（很好用）：`archive-reclaim-branch` 这条臂的 `evidence` 会引用
"tag 仍然指着 head 自己的 commit"。三者一致（tag→tip、head 曾=tip、reason 记录）
只可能是这一条臂干的，外部删除产生不了这种一致性。

## 三、最后才问"WHY 在不在"，而且用仓库自己的计数，不要自己数

```bash
git for-each-ref refs/gopi/archive-reason/ | grep -cE '<ids>'   # 逐条：有没有
gopi task archive --check | grep 'reason-less archive(s)'        # 全仓：补写前后的差就是你的动作量
```

后一条是关键技巧：`--check` 的 `reason-less archive(s)` 是**仓库级数字**，
所以"你的动作有没有真的落地"可以用**差值**验证，不需要逐条肉眼核对：

```
补写前 239  → 补写后 229      # 差值 10 = 本 run 写下的 10 条，正好
```

这条差值是"写入真的发生了"的独立证据，和动词自己的 stdout 互相印证
（动词的回执可以被信任，但**被验证过的回执**才是证据）。

## 四、补写 WHY 是幂等写，不是重写

`ArchiveTagReasoned` 的规则是：

- tag **已存在** → 不重写（`tag -l` 命中即跳过）；
- reason 记录**已存在** → 不重写（"reviewer 可能已经读过这条记录"），于是
  `at:` 时间戳不会被重试改掉；
- reason 记录**不存在** → 现在写。

所以对一条"tag 在、reason 缺"的分支再跑一次归档动词是**安全的补写**，
而不是对既有决定的改写。验证方式是 blob sha：
`git for-each-ref refs/gopi/archive-reason/` 前后 diff，已有的那条 sha 必须**逐字节不变**
（本次实测 `UNCHANGED_SHA_OK`），新增条数必须等于你打算补的条数（本次 10/10）。

## 五、`--reason` 被强制改写时，不要当成失败

无 ledger 行追踪的分支，`--reason superseded` 会回读成 `reason: unowned`
（`ArchiveDecide` 案例 ③：记录事实优于接受调用者编的故事）。调用者的意图保留在
`detail:` 自由文本里。**看到回读与你传入的不一样，先查是不是这条规则**，不要当场"修"它。

## 六、环境的并行性：`git worktree list` 的漂移不是你的 bug

本 run 期间 `git worktree list` 少了一个条目 —— 另一个任务的 `task-self-reclaim`
臂（不同 pid）回收了它自己的 ground。同机并行是常态：
**归因先看 `removal-audit.log` 的 pid/arm，再决定要不要把它写进报告的"异常"里**。
把别人的回收记成自己的缺陷，会把一条正常的生命周期说成 regression。

## 相关

- `devdoc/archive-ref-reclaim.md` —— 回收臂本身的设计（tag 是决定、head 是引用）
- `devdoc/archive-reclaim-removal-audit-20261012.md` —— 这条臂"删了却无记录"的那次事故
- 本 run 提的两个 bug：`01a14a8c-a9f0`（回收臂不写 reason）、`01a14a8c-c72c`（reason 的 `repo:` 写成调用者工作目录）
