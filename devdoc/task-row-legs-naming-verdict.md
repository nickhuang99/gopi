<!-- doc-class: historical -->
# 账本行推拉动词腿 — 命名裁决指引

**裁决：(a) 给账本行另取名 —— `send` / `receive` 维持不变，`gopi task push` 的 git 通道含义不动。**

本文件只交付判断，不实现（Rule 35：本行不写代码、不新增依赖）。
被裁决的对象是任务 `01a0feb6`（分支 `task/01a0feb6204b7652ba0d17bcb9a96f19`
@ `d7fdf1dfc86fe757dae3de7ec8453c414285738e`，下称 **rowlegs 分支**），
其 briefing 未给方向的那个二选一。测量基准 commit：
**`0be0e99b9671a5ad306a52fb8ff759bb52785398`**（master tip，本 worktree 的分叉点）。

---

## 1. 裁决 (a) 与三条理由

### 理由 1 — (b) 会要求改动「不可改的历史记录」，而约束 2 明令禁止

`task push` 的引用面里，有 **5 处是逐字运行记录**，不是描述、不是文档，是某一次真实
运行 stdout/stderr 的抄本；改动它们就是伪造历史（约束 2）：

生成器：`git grep -n 'task push' 0be0e99b9671a5ad306a52fb8ff759bb52785398 -- tasks/`

```
tasks/01a0dac544a97322bad872c5e30b6448-report.md:93:gopi task push → "fetch-prune: 2 pruned"
tasks/01a0dac544a97322bad872c5e30b6448-report.md:113:- **Ran:** `gopi task push` (real peer peerbox) → `fetch-prune: 2 pruned`;
tasks/01a0dac544a97322bad872c5e30b6448-report.md:141:- `gopi task push` → stderr line `fetch-prune: 2 pruned`.
tasks/task-row-sync-switch-report.md:45:`gopi task push`, `gopi memsync` are operator-typed and unaffected; `sync_peers
tasks/task-row-sync-switch-report.md:192:**Manual verbs are unaffected by the switch** — observed: `gopi task push` a
```

同类还有 **16 处只读记录面**（实测计数，见 §2）：`docs/demo/**` 的 3 个 TSV
（coverage 汇总，Rule 38 census 的产物，改数=改测量）、2 个 `.txt` 原始输出、
`.evid/orig-gopi.md`。这些不是"文档可以顺手重写"，是记录。

(b) 若成立，这一整类文件要么保持旧名（仓库里长期两名并存，读者无法判断哪个是现役）、
要么被改写（=伪造）。**(a) 让这 5+16 处记录保持真值。**

### 理由 2 — `push` 在同一个 family 里已经承载了「git 分支同步」，再加一个义项就是同名两物

实测的现行含义（master，`gopi task push -h` verbatim）：

```
  gopi task push
      The git sync channel's manual face (the SAME atom the serve tick
      runs): client — push local feat/* branches back to the server repo
      (idempotent) + pull master --ff-only; server — fetch the peers'
      branches, clean-merge + delete them (a conflict dispatches a
      merge-conflict task). Role-resolved via sync_peers (the server is the
      node whose name is NOT in its own peer list).
```

同一个 `task` family 下 `push` 已经至少三义：
1. `gopi task push` — git 分支通道（实现 `runTaskPushGit` / `GitSyncPushLocalBranches` / `GitSyncPushbackPlan`）；
2. `gopi task sync --push-only` — 把**行**广播给白名单 peer（走 claim broadcast）；
3. 口语里的 "push" 泛指"推东西出去"。

再让「写一行进对端账本」也叫 push，就把**两种不同的对象**（git ref 与 ledger row）
塞进同一个词。前序任务的裁决段自己给出的判据是正确的：

> `gopi task push` was unavailable: it is the family's **git** channel
> (`runTaskPushGit` pushes the task branch). Two objects — a branch and a ledger
> row — must not share one word inside one family.

**兄弟通道已有先例**：`gopi memsync` 面对同一个"定向传输"概念时，**没有**用 push/pull，
而是用了 `--export / --import`（`cmd/agent/cli_memsync.go`：
`const memsyncFlagNames = "--export / --import / --export-proc / --import-proc"`）。
即 family 的既有口味是**不给 push 加义项**。(a) 与这条口味一致，(b) 逆之。

### 理由 3 — 改名成本 vs 收益：成本是 60 文件 / 98 处（含 31 个 md 与 3 个 TSV），收益是零

见 §2 的实测清单。收益侧：`push` 作为 git 通道名**本来就准确**——那条通道干的正是
`git push` + `git fetch` + merge（synopsis 自己这么写），改名反而让「它对应 git push」
这层可读性损失掉，而 `push` 与 `git push` 的对应关系是操作者理解该动词的唯一线索。

**结论**：改 git 通道名要动 11 个 `.go`、31 个 `.md`、3 个 TSV、2 个 txt、1 个 sh，
其中 21 处是不可改记录；换来的只是"少一个同族旧词"。**成本高、收益负、(a) 成本为
两个新词 + 前序分支已完成的实现**。

### 独立复核：前序任务的 send/receive 裁决成立

我不采信 briefing 的转述，直接读了 rowlegs 分支的 `devdoc/task-row-legs-send-receive.md`
§"The naming verdict" 与 `internal/taskverbs/verbs.go`。复核结论：**成立**，且理由与我
上面的理由 2 同源（同族不得一词两物），但它当时**没有给引用面计数**——这正是本行的补足
（briefing 的「13+」是口述，实测为 98 处 / 60 文件，见 §2）。

复核中发现的、前序未言明的两点（供执行者注意，不改其结论）：
- rowlegs 分支同时引入了 peer-side flag `--row-remote`
  （`internal/tools.SyncRowRemoteFlagName = "row-remote"`，`cli_tasks.go:1604`），
  它是**第 4 个** "remote" 词（`--serve-remote` / `--claim-remote` / `--row-remote`）。
  命名自洽，但它也让 `task push` 之外多了一个不可回收的公开拼写。
- rowlegs 分支的 memsync 广告面仍**保留** `gopi task sync --push-only <task_id> --peer <alias>`
  的指向（见 §4 基线），这不是回退（约束 3 只禁 task 通道 synopsis 与退休拼写进
  `memsyncFlagNames`），但 (b) 若执行，这一句也得改。

---

## 2. 引用面清单（生成器 + commit，非口述）

**生成器（可复跑，只读对象库）：**

```bash
B=0be0e99b9671a5ad306a52fb8ff759bb52785398
git grep -c 'task push' $B            # per-file occurrence counts
git grep -l 'task push' $B            # file list
```

**实测结果（commit 0be0e99b，tracked 文件）：**

| 目录 | occ | files |
|---|---:|---:|
| cmd | 11 | 5 |
| internal | 6 | 6 |
| scripts | 1 | 1 |
| devdoc | 59 | 22 |
| tasks | 5 | 2 |
| 其余 12 文件（docs/demo、briefings、GOPI.md、CONFIG.md、.evid 等） | 16 | 12 |
| **合计** | **98** | **60** |

按扩展名：`31 md / 11 go / 3 tsv / 2 txt / 1 sh`（md 含 devdoc + tasks + briefings +
GOPI.md/CONFIG.md + docs/demo/v3）。

**生产代码 11 个 `.go`（其中非测试 9 个）：**

```
cmd/agent/cli_tasks_sync.go                      cmd/agent/cli_tasks_sync_git.go
cmd/agent/cli_tasks_sync_pass.go                 cmd/agent/cli_tasks_sync_surface_test.go
cmd/agent/cli_tasks_test.go                      internal/config/settings.go
internal/config/task_sync_switch.go              internal/taskverbs/verbs.go
internal/tools/task_sync_git.go                  internal/tools/task_sync_git_pushback.go
internal/tools/task_finish_cleanup_test.go
```

**briefing 的「13+」是低估**：光 `.go` 一处口径内是 11 occ/5 files（cmd），
全仓 tracked 是 98 occ/60 files。裁决若要引用数字，应引用本节。

---

## 3. 选名硬约束：候选名对既有 verb 面的查证

### 3.1 `taskverbs` 既有 verb（32 个常量，master @ 0be0e99b）

生成器：`grep -nE '^\s+[A-Z][A-Za-z]*\s*=\s*"' internal/taskverbs/verbs.go`

```
adopt archive audit-merges dedup-scan diff finish-cleanup for-branch harvest
improvements list merge merge-execute merged-cleanup merger new next push reopen
run search serve show status submit sweep-worktrees sweep-zombies sync sync-pass
tag-resumed update verdicts verify
```

补充查证：rowlegs 分支在该文件里**已加入** `Receive = "receive"` 与 `Send = "send"`
（该分支常量数 34）。**master 上是 32 个，`send`/`receive` 两个词都不在其中**：

```
$ grep -cE '^\s+(Send|Receive)\s*=' internal/taskverbs/verbs.go     # → 0
```

### 3.2 顶层 verb 面（`internal/cliverbs/cliverbs.go`，14 个词）

```
ui_state log raw stats task merge capture screen_capture act sandbox chat version ui
```

`send` / `receive` 均不在其中；`push` 也不作为顶层 verb 存在
（`git grep -n "'push'\|\"push\"" -- cmd/agent/main.go internal/cliverbs/` → 0 命中）。

### 3.3 其它 family 与 flag 面

- 全仓 `= "send"` / `= "receive"` 的**常量**：**0 处**（唯一命中是
  `cmd/agent/cli_web.go:2325` 的 HTML `<button id="send">`，非 verb 面）。
- `--push` / `push` 作为**选项**：不存在（`grep -nE '"push' cmd/agent/cli_tasks.go`
  的两个命中都是 `exec.Command(..., "git", ..., "push", ...)` 里的 git 子命令字面量，
  非 gopi 自己的 flag）。
- "send/receive" 在仓库里只作英文散文出现在注释中，未承担标识符语义。

**结论**：`send` / `receive` 在 `taskverbs`、顶层 cliverb 面、其它 family 的常量里
**均无碰撞**，且语义直白（一个 transfer 的两个方向）。

### 3.4 被淘汰的候选（过程留痕）

| 候选 | 淘汰理由 |
|---|---|
| `push-row` / `pull-row` | 仍把 `push` 留在同族（理由 2 未解决），且 `--push-only` 与 `--push-row` 在 help 里视觉难分 |
| `export` / `import` | 不是 taskverbs 碰撞，但**与兄弟通道 `memsync --export/--import` 的既有词汇重叠**——同一个仓库里 `task export` 与 `memsync --export` 会被读成同一件事（实际一个是 ledger row、一个是知识行 patch） |
| `deliver` / `collect` | 不碰撞，但 "deliver" 在 finish-cleanup 语义里已被占用（`deliverable.repo/worktree/branch`），会产生第三种"交付"含义 |
| `send` / `receive` | **选用**：零碰撞、双向词形对称、且正是 memsync 之外 family 里最中性的传输词 |

---

## 4. 三个面的名字与 help 对照（`-h` 输出 verbatim）

三个面分别是：(A) `task push` = git 分支同步；(B) `task sync --push-only` = claim 广播；
(C) `task send` = 落库（rowlegs 分支新增）。

**(A) `task push`（master 二进制，真跑 `go run ./cmd/agent task push -h`，exit=0）：**

```
  gopi task push
      The git sync channel's manual face (the SAME atom the serve tick
      runs): client — push local feat/* branches back to the server repo
      (idempotent) + pull master --ff-only; server — fetch the peers'
      branches, clean-merge + delete them (a conflict dispatches a
      merge-conflict task). Role-resolved via sync_peers (the server is the
      node whose name is NOT in its own peer list).
```

**(B) `task sync --push-only`（master，`go run ./cmd/agent task sync -h`，同上页 exit=0）：**

```
  gopi task sync --push-only [--task-id T] [--peer <alias> | --peers [a,b]]
                           [--db PATH]
      Manually PUSH a row to the whitelisted peers WITHOUT a pull
      session: with --task-id T exactly that row travels (the claim
      broadcast's manual face — idempotent). --peer narrows the push to
      that ONE peer (it must be declared in sync_peers); --peers narrows
      it to the DECLARED peers (the bare form) or to the listed ones
      (--peers a,b). Without either, every
      whitelisted peer is pushed to. Without --task-id the whole
      current delta window (updated_at > anchor) goes out — a deliberate
      choice, distinct from forgetting the flag: the peer's --claim-remote
      carries a CLAIM, so it accepts only a claim-sized patch (up to its
      small row cap) and refuses a window with a message that names its own
      row limit, hiding which side got the selection wrong.
```

**(C) `task send`（rowlegs 分支二进制 `d7fdf1df`，真跑，verbatim）：**

```
  gopi task send --task-id T [--peer <alias> | --peers [a,b]] [--db PATH]
                         [--remote-home DIR]
      Write ONE row into the peer's LEDGER (the row leg's push half): the
      row travels as a one-row delta patch and the peer applies it through
      the SAME merge authority a session uses (newer wins, a terminal row
      never regresses, content is never overwritten), then ANSWERS what its
      ledger did with it — stored / migrated / already current / REFUSED
      (with the reason). --peer narrows the send to that ONE peer (it must
      be declared in sync_peers); --peers narrows it to the DECLARED peers
      (the bare form) or to the listed ones (--peers a,b); without either,
      every whitelisted peer is sent to. --remote-home DIR binds the
      PEER-SIDE handler to an isolated profile …
      NOT the same leg as --push-only --task-id T: that one rides the CLAIM
      BROADCAST, whose peer handler mirrors the row's LOCK and answers a
      verdict — measured 2026-09-21 it printed "accepted (1 row(s))" while
      the peer's own "gopi task show T" reported no such task.
```

**(C') `task receive`（rowlegs 分支，verbatim）：**

```
  gopi task receive --task-id T --peer ALIAS [--remote-db PATH] [--db PATH]
      Pull ONE row off the peer's ledger into this one (the row leg's pull
      half): SELECT that row by id on the peer, then apply it here through
      the same merge authority (newer wins, a local TERMINAL row is never
      demoted, equal stamps must be identical or they count as a divergence
      warning). Reports the import's counts plus the row's state in THIS
      ledger afterwards. --remote-db points at a non-default peer-side
      ledger (the probe's isolated profile). No anchor moves: a single-row
      read is not a window, and advancing one would silently skip every row
      between it and the id you asked for.
```

**可区分性判定**：三者在 `synopsis 首行 + 对象名词 + help 正文首句` 三个层面各不相同 ——
(A) 首行无参数、对象是 **branches**；(B) 首行带 `--push-only`、对象是 **a row … the claim
broadcast's manual face**；(C) 首行独立 verb `send --task-id`、对象是 **the peer's LEDGER**，
且 (C) 的 help **显式点名** (B) 并说明为何不同（"NOT the same leg as `--push-only --task-id T`"）。
**这是 (a) 的直接收益：(B) 与 (C) 的差别现在写在 help 里，(b) 只会让 git 面 (A) 也变成
同一个词，三者更难分。**

### 本行的真跑取证（非 `-h`，Rule 31）

二进制的来源（Rule 29：`git archive`，绝不借别人的 worktree）：

```bash
# master（本 worktree）
CGO_ENABLED=0 go build -o /home/user/scratch/01a0febf-probe/gopi-master ./cmd/agent/
# rowlegs 分支（archive 展开到 off-/ 的 scratch，设备号已核：/ = 2051，/home/user/scratch = 2052）
git archive task/01a0feb6204b7652ba0d17bcb9a96f19 | tar -x -C /home/user/scratch/01a0febf-probe/src-rowlegs
CGO_ENABLED=0 go build -o /home/user/scratch/01a0febf-probe/gopi-rowlegs ./cmd/agent/
```

四次真跑（rowlegs 二进制，`GOPI_HOME` 指向一次性 profile），verbatim：

```
$ gopi-rowlegs task send
gopi task send: task send: --task-id is required (a row leg carries no delta window: exactly one row travels)
exit=1

$ gopi-rowlegs task receive
gopi task receive: task receive: --task-id is required (a row leg carries no delta window: exactly one row travels)
exit=1

$ gopi-rowlegs task sync --push-only --task-id deadbeef --peer no-such-alias
gopi task sync: memsync push: peer "no-such-alias" is not in the sync_peers whitelist (settings.json "sync_peers" / GOPI_SYNC_PEERS) — sync topology is declared, never discovered
exit=1

$ gopi-rowlegs task send --task-id deadbeef --peer no-such-alias
gopi task sync: row send: peer "no-such-alias" is not in the sync_peers whitelist (settings.json "sync_peers" / GOPI_SYNC_PEERS) — sync topology is declared, never discovered
exit=1
```

**观察到的缺陷（本行发现，交执行者处理，本行不改代码）**：第 2 条
（`task send` 的自检拒绝）被打印成 **`gopi task sync:`** 前缀，第 3 条
（`--push-only` 的 peer 解析）被打印成 **`memsync push:`** 前缀。两者都不是自己那条腿的
名字——同一个进程里消息前缀与命令面脱节。这不是命名裁决的阻塞项，但它**削弱 (a) 的一个
支撑点**（"三个面在名字上可区分"）：退出路径上的前缀目前并不可区分。**执行者若执行 (a)
的收尾，应把这两条前缀对齐到各自的面**（`row` / `--push-only` 的 face 名），否则
§4 的可区分性结论只在 help 上成立、在 stderr 上不成立。

---

## 5. 若执行者仍想选 (b)，必须满足的条件

(b) = 破坏性改 git 通道名（把 `gopi task push` 让给账本行，git 通道另起名）。
以下**全部**满足才允许，缺一不可：

1. **不得改动历史记录**。5 处 `tasks/*.md` 逐字运行输出 + 16 处只读记录面
   （`docs/demo/**` 的 3 TSV、2 txt、`.evid/orig-gopi.md`）必须原样保留旧名。
   于是仓库里旧名与新名并存——必须有一处**明确说明哪一个是现役**（否则读者无法判断，
   这正是理由 1 里 (b) 的代价）。**任何对这些行的改写 = 伪造（约束 2），直接否决。**
2. **必须处理 31 个 `.md` + `CONFIG.md` + `GOPI.md` + briefings 的叙述面**，
   并说明为何它们的旧名不构成"文档谎报现状"。
3. **必须给出旧拼写的可运行性判决**：`gopi task push` 是保留为 alias、还是硬拒绝。
   保留 alias = 两个名字永久并存（收益归零）；硬拒绝 = 破坏
   `scripts/demo-coverage.sh`、`scripts/` 与外部脚本（属破坏性变更，需操作者裁决）。
4. **必须按 Rule 1/13/14 落常量**：新 git 通道名进 `taskverbs`，flag 名进其所属常量
   （形状参照 `internal/tools.SyncRowRemoteFlagName`），不得出现第二处字面量。
5. **必须同步三处 gate 真值**：`syncSurfaceSynopsisFlags()`
   （cmd/agent/cli_tasks_sync_surface_test.go）、18 行下限的 `syncSurfaceFlagMatrix()`、
   以及 `cmd/agent/cli_tasks_test.go` 的 synopsis 断言。
6. **必须论证新名不与 git 词汇冲突**：该通道的语义**就是** `git push` + `git fetch` +
   merge（见 §4 (A) 的 synopsis）。新名若失去与 `git push` 的字面对应，
   要说明操作者如何仍能一眼看出"它跑 git push"。
7. **必须核 `memsync` 广告面**：rowlegs 分支的 `gopi memsync -h` 第 4 行与第 59–66 行
   仍指向 `gopi task sync --push-only`，改名后这一句随之失真，需一并处理
   （约束 3 只禁回退 task 通道 synopsis 与 `memsyncFlagNames` 里的退休拼写，
   本节不是允许回退，是允许**更新**）。
8. **必须给出成本账**：至少 §2 的 11 个 `.go` + 3 个 TSV 的逐文件清单与改动量，
   并与 (a) 的成本（= 0 个文件，前序分支已完成）并列。

**判据（一句话）**：若第 1 条做不到，(b) 在约束 2 下**不可执行**；(a) 是唯一同时满足
约束 2、3 与"三面可区分"的选项。

---

## 6. 本行给出的、面向 rowlegs 分支的结论

- **维持 `send` / `receive`**，`gopi task push` 的 git 通道含义**不动**。
- rowlegs 分支**不需要因命名而改动任何一行代码**（本裁决 = 零改动）。
- 合并 rowlegs 分支时，本文件即为该二选一的最终裁决记录。
