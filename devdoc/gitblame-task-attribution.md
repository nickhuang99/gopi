<!-- doc-class: normative -->
# git_blame 说出「这段代码属于哪个任务」

## 问题：author 对 agent 是零信息

人类公司里「谁提交的」是一个判断依据：资深工程师写的代码不轻易动，实习生写的可以直接改。
**agent 没有这种区分** —— 每一次 run 都以 `gopi <gopi@gopi>` 提交。实测本仓库一个提交：

```
$ git blame -L 1,1 --porcelain -- internal/tools/gitblame.go
964b2404b4539768367fe00a87aa3a5e01ecb26b 1 1 3
author gopi
author-mail <gopi@gopi>
```

`author gopi` 等于没提供任何区分人的信息。

**真正有信息量的是：这一段改动属于哪个 task，那个 task 想干什么。**

理由是它回答了一个操作性的问题 —— 「我的改动和这一行的目的冲不冲突，我该不该让步」。
而那个问题需要一个**意图**，不是一个人名。

## 数据链路

`git_blame` 的每一行拿到它所属 commit 的 `task:` trailer，再拿这个 id 去账本里取那一行的
summary / status。join key 是现成的：bash 工具的提交缝
（`internal/tools/git_commit_task_trailer.go`）给每个被派发 run 的提交都追加 `task: <row id>`。

三个真实样本（本机实测）：

```
0cfa412cd  → task 01a19602-b631-…  → status in_progress + 真实 summary
11d5513d   → task 01a195bb-4287-…  → host host-b
964b2404b  → （空，天然没有 trailer —— 仓库的 initial commit）
```

## 三层 fallback，每一层都不许报错

`git_blame` 在任何 git 仓库都必须能用，而「这个仓库没有 task trailer」是最普通的情况
（别的项目、手写的提交、本仓库自己的 initial commit）。

| 层 | 情况 | 结果 |
|---|---|---|
| ① | commit 没有 `task:` trailer | `task_*` 全空，blame 照常 |
| ② | 有 trailer，但账本里没有这行 | `task_id` 有值（来自 git），其余空 |
| ③ | 本机根本没有账本 | `task_*` 全空，blame 照常 |

顶层 `task_enrichment: none\|partial\|full` + `task_enrichment_note` 说明走了哪一层。
**为什么要有 note**：字段空着不说明原因，agent 会读成「这个 commit 不属于任何 task」，
而真相可能是「有 task，但本机账本看不见它」。这两种情况对「该不该让步」的判断完全不同。

## 四个非显然的实现决定

### 1. 「本机有没有账本」必须在 `storeBlameMemory` **之前**采样

`storeBlameMemory` 是既有的 best-effort 记忆联动，它会调 `MemoryStore`，而 `MemoryStore`
会**创建**账本。如果存在性检查放在它之后，note 描述的就不是「我读的时候有没有账本」，
而是「我自己的副作用留下了什么」。

实测（`TestGitBlameEnrichNoLedgerOnThisHost` 首版）：检查放在后面时，note 打印的是
`no such task row exists in <path>`（第二层的措辞），而且那个路径是这次调用刚刚建出来的。
现在检查在 `gitblame.go` 的调用点采样一次，作为参数传进来。

### 2. 一次 blame 只开一次账本、只查 distinct 的 task id

一个 200 行窗口常常只来自少数几个 commit。实测本仓库 `internal/tools/tasks.go` 1..200 行：

```
access: {Commits:6 TrailerLookups:6 LedgerOpens:1 LedgerQueries:4}
```

**6 个 distinct commit / 4 个 distinct task id → 1 次打开 + 4 次查询**，不是 200 次。
这个数字是**就地报告**的（`ledger_access` 字段），不是让人相信注释。

### 3. 一次 `git log` 取全部 trailer，三重分隔符

每条 blamed line 单独问一次 git 会把 200 行放大成 200 次进程 spawn。做法是
`git log --no-walk=unsorted --stdin -z`，把去重后的 hash 列表从 stdin 喂进去。

分隔符的选择是有约束的：

- `-z` = NUL 分**记录**。提交正文不能含 NUL，所以正文伪造不了记录边界。
- `0x1f` 分**字段**（hash / task / host）。
- `0x1e` 分**同一个 key 的多个值**。

### 4. trailer 必须 `%(trailers:key=task,valueonly)`

实测提交 `90927414a` 把 trailer 写了两遍 —— 一次 `Task:`、一次 `task:`：

```
$ git log -1 --format='%(trailers:key=task)' 90927414 | od -c | head -2
0000000   T   a   s   k   :       0   1   a   1   9   6   0   2   -   b
...
```

不加 `valueonly` 拿到的是 git 的**渲染文本**（`Task: <v>\ntask: <v>`），要自己再解析一遍；
加了 `valueonly` 得到两行原值，取第一个。重复值不是错误，但必须**确定性地**取一个 ——
把两个值拼起来会得到一个不匹配任何行的字符串。

## 读行走 taskByID，不解析渲染文本

summary / status 从 `taskByID` 拿 `*Task` 结构体，**不** grep `taskShowWithDB` 的输出。
解析渲染文本在本仓库已经翻车过两次：渲染是给人看的，列宽、缩进、多行折叠都会变，
而结构体的字段名是编译期保证的。

summary 还必须过 `sanitizeTaskSummary` —— **同一个函数**，不是第二份实现。账本自己的
写入路径会清洗，但历史行（写入守卫存在之前写的、从别的节点同步进来的）可能带控制字节；
工具输出如果原样透出，就等于绕过了那道守卫。实测 `TestGitBlameEnrichSanitizesSummary`：
塞进 `"resolve conflict\x00 for fix/x — steps\x01two\nnext line"`，输出与
`sanitizeTaskSummary(dirty)` **逐字符相等**，且保留 `[bin …]` 标记（Rule 15：不静默丢弃证据）。

## A 方案：只提供信息，不自动裁决

工具**不**判断「两个 task 的目的是否相容」。那个判断无法机械化 —— 硬做只会产出一个
看起来权威、实际上错误的裁决，而 agent 会信它。

工具做的是：把 intent 放到 agent 眼前，让它自己决定。

系统提示里的实测例子（`internal/agent/loop.go` 的 `ToolGitBlame` Description）：

```
git_blame(internal/tools/clone_ground_branch_create_guard_test.go, 1, 6)
→ commit 11d5513d
  task_id:      01a195bb-4287-7ad5-a764-b6df563895dc
  task_status:  completed
  task_host:    host-b
  task_summary: run 01a1957a 在自己的 clone 之外还进入了共享主目录 /home/user/work/gopi
                并建 task/<id> 分支写入半成品，导致所有人 pull 被挡、make build 报
                undefined: scratchGCConsentGiven（/usr/local/bin/gopi 停在旧版）
```

**这段数据为什么值钱**：拿到它之后，agent 才知道那几行守卫是为了堵一条**真实的**越界路径
存在的 —— 于是「我为了跑通我的测试临时放宽这个守卫」这个念头会被自己否掉，而
「我沿着同一个方向补一条漏掉的子命令」会被放行。**只看 `author gopi` 这两种判断都做不出来。**

## 验收怎么跑

```bash
go test ./internal/tools/ -run 'TestGitBlameEnrich|TestGitBlameUniqueCommits' -count=1 -v
```

七个测试，每一个都建**真实**的 temp git 仓库 + **真实**的 temp 账本
（`testutil.NewTestDBPath`，Rule 32）—— 这个 join 发生在两个存储之间，mock 掉任何一边
都证明不了那三层 fallback。

真实数据的那一份（本机账本，只读）：

```bash
GOPI_MEMORY_DB=/home/user/.gopi/memory.db go test ./internal/tools/ -run <证据收集器> -v
# → task_enrichment: full; access: {Commits:1 TrailerLookups:1 LedgerOpens:1 LedgerQueries:1}
```
