<!-- doc-class: historical -->
# 历史重写执行面：路径组、成本模型、失效面、冻结前置与善后（epic 01a0fd2e 第 [5/5] 步）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

本文是 epic `01a0fd2e`（「git 存代码不存 binary」）末步的**执行面说明**：一次
`filter-branch` 重写要删哪些路径、真实成本多少、哪些 SHA 会失效、动手前必须冻结什么、
怎么回滚、事后每个持有 clone/worktree 的人要做什么。

**本会话在主仓上不执行重写**（理由见 §6：冻结前提本次不成立）。全部数字来自**隔离 mirror**
上的彩排实测；执行器脚本在会话 artifacts 里（`run-history-rewrite.sh`），不是入库脚本
（Rule 35：一次性破坏性操作不新增常驻依赖）。

会话 `01a0fd2e-f01a-7be6-932f-afd9b755451a`，测量时刻 2026-10-03 00:16–00:55 (+08:00)，
主机 peerbox，git 2.34.1。原始证据：`~/.gopi/sessions/01a0fd2e-f01a-7be6-932f-afd9b755451a/artifacts/`
（`background.md`、`before.txt`、`after.txt`、`commit-map-old-to-new.tsv`、`refs-rewritten.tsv`、
`filter-branch.log`、`mirror.txt`）。

## 1. 要删的路径组（invert 列表）

只删这些；其它历史一个字不动（这就是简报里的「只 invert 指定路径」）。

| 路径组 | 是什么 | 为什么删 |
|---|---|---|
| `docs/archive/demo/**` | 演示渲染产物（重写基点 `56e17d98`：`docs/archive/demo` 共 1295 个跟踪文件，其中 **721 个是 PNG/GIF**；单个最大 `replay-film/chat-replay.gif` 15,416,176 B） | 可再生渲染产物；**recipe**（`*.py`/`*.sh` 渲染脚本）留在树里才是交付物 |
| `sandbox/**` | `go build -o` 的 ELF（`sandbox/gopi-bin` 19.7 MB）+ 伪造 `$HOME`（含操作者 profile 的副本） | Rule 34：sandbox 是执行环境，不是仓库内容；且是操作者真实数据的拷贝 |
| `devdoc/evidence/`、`devdoc/assets/`、`devdoc/webcam-capture-evidence/`、`devdoc/doubao-asr-eval/`、`devdoc/requirements/evidence/` | 截图、音频捕获、跑批日志、响应 dump | 一次运行的记录，不是 text of record；`.md` 与脚本另算 |
| `tests/fixtures/gopi-raw-20260905-024552.log` | 5.4 MB 跑批日志 | 同上（注意 `tests/fixtures/log-stats-raw/**` 是**测试读取**的 golden，**不在**本组） |
| `bin/gopi`、`bin/gopi-agent`、`agent`、`gopi-behind` | 9–26 MB 的构建产物 / 旧二进制 | `go build` 的产物，recipe 是构建命令 |
| `.leakprobe-agent.test` | 19.5 MB `go test -c` 产物 | 测试二进制 |

**边界（实测）**：历史中**每个出现路径都落在本组内**的 blob = **2403 个 / 263.4 MiB（未压缩）**；
与目标组共享、同时被非目标路径引用的 blob = **0 个** —— 路径组切得干净，没有「删了会伤到
别处」的对象。

## 2. 成本模型（两条路线，实测而非估算）

| 维度 | `git filter-repo` | `git filter-branch` |
|---|---|---|
| 本机可用性 | **不可用**（`command -v` 为空、python 模块缺失）；Rule 35 禁止为它安装依赖 | 可用：git 2.34.1，`/usr/lib/git-core/` |
| 本仓先例 | **有**：`.git/filter-repo/`（2026-09-07 14:44）—— 本仓 2026-09-07 就被 filter-repo 重写过一次 | 无 |
| 一次性吞吐 | —（无法实测） | 首次启动约 **15 s** 开销（`git fast-export --all > /dev/null` 15.4 s，流 1.67 GB），之后每提交约 **65 ms** |
| 全量 wall-clock（8693 提交，`--all` + `--tag-name-filter cat`） | — | **34–55 分钟**（实测上界：`3933/8693` 已耗 **1482 s** ⇒ 外推 ≈54 min；见 §2.2）。工具在 `650/8693` 处自报的剩余 **519 s（≈9–10 min）是线性乐观外推，已被 1482 s 那一轮推翻** |
| 产出的映射 | `commit-map` + `ref-map` + `changed-refs` + `first-changed-commits` + `suboptimal-issues` | **没有任何 map 文件**（git 2.34.1 实测：`filter-branch` 不写 `commit-map`/`ref-map`），映射只能自己从 `refs/original/*` 与变化后的 ref 对照算出 |
| 对 remote 的处理 | 默认要求干净 clone，`--force` 会**删掉 remote 配置** | **完全不碰** remote：那 33 条 `refs/remotes/*` 仍钉着旧 SHA，须人工处理 |
| 备份 | 自己负责 | 自己负责 |
| 结论 | 路线更安全（自带 ref-map/幂等标记/冲突提示），但**需装工具 = Rule 35 红线** | 现成可用，代价是 remote 与 `refs/original` 要人工收尾 |

**吞吐的关键事实**：`--index-filter` 是**每个提交各跑一次**，所以成本 ∝ 提交数（不是 blob 数）；
`-- --all` 在**本仓**是 **8693** 个提交（`git rev-list --all` 报 8222，加上 tags/remotes 可达集）。

**彩排实测（隔离 mirror，本会话）**：见 §2.1 与 `artifacts/rehearsal-evidence/`（保留的原始输出
`before--rehearsal-rc2.txt` / `after--rehearsal-rc2.txt` / `fb.log--rehearsal-rc2.txt` / `ref-map.tsv--rehearsal-rc2.txt`）。

### 2.1 有界全周期彩排的实测数（本轮，`rehearsal-rc2`）

在隔离 mirror 上对 `master~15..master`（**45 个提交**，含 merge）跑完整周期
（filter-branch → `reflog expire` → `gc --prune=now`）：

| 量 | 值 |
|---|---|
| **`du -sb .`** 前 → 后 | **563,035,516 B → 384,874,723 B**（`du -sh`：**563M → 368M**，即 **−178.16 MB / −31.6%**） |
| 耗时 | **35 s**（含 clone 之外的重写 + 两次 gc） |
| 新 HEAD | `27e1f5e1aa8dbb15a0f97047ec460cc8caf55668`（≠ 旧的 `c2a7ba7e…`） |
| `docs/archive/demo/replay` 跟踪文件 | **128 → 0** |
| `docs/archive/demo` 跟踪文件 | 1295 → 1167 |
| `refs/original/refs/heads/master` | `c2a7ba7e…` —— 旧 ref 被保留在这里 |
| 旧 SHA 可读性 | 删除 `refs/original` **之前**：`git cat-file -t` → `commit`；**删除并 gc 之后** → `fatal: git cat-file: could not get object info` |

**两条副产品结论**：
1. **删除 `refs/original/*` + `gc --prune=now` 才是「旧 SHA 失效」的那一刻**。只跑 filter-branch
   不改可达性 —— 这与删除路径的效果无关，是 filter-branch 的语义。
2. **git 2.34.1 的 `filter-branch` 不产出 `commit-map`**。本仓 `.git/filter-repo/` 里那份
   commit-map/ref-map/changed-refs 是 **filter-repo** 的格式与产物（mtime 2026-09-07 14:44），
   不是 filter-branch 的。选 filter-branch 就必须**自己**从
   `git for-each-ref refs/original` 与改写后的 ref 对照生成映射（本会话的 `refs-rewritten.tsv`
   就是这个动作；有界彩排的产物见 `rehearsal-rc2/ref-map.tsv`）。

## 3. SHA 失效面

- 唯一能证明「只影响目标路径」的办法是**范围**：`--index-filter` 在范围内每个提交都执行，
  凡内容变了的提交，其 hash 及其**全部后代**都改。
- 本仓实测：最早触碰目标组的提交是 `b425e88c`（2026-09-11 00:26），其**后代 = 8222 个提交**
  ⇒ 目标 blob 一旦进入某提交，该提交之后的一切都会改 hash。**所谓「只改 20 个提交」是错的**：
  20 是「触碰 `docs/archive/demo/replay` 路径」的提交数，不是失效面。
- 因此：**主分支线（master 及其全部后代）整体换 hash**，包括
  `refs/heads/*` 12 条、`refs/tags/*` 470 条、`refs/remotes/*` 33 条 —— 共 **526 条 ref**。
- 好消息（实测）：目标组与其它路径**零共享**，所以**没有任何提交会因「只是被删路径」以外的
  原因改变**：内容差异严格等于「目标路径从树里消失」。

## 4. 回滚点 = mirror 备份（本会话已实测大小与耗时）

```bash
# 目标必须在与 / 不同的设备上（Rule 22 的判据是 device id，不是路径写法）
stat -c '%d %n' / /home        # 2051 / 2052  →  /home 可放
git clone --mirror /home/user/work/GoPi <off-/>/GoPi-pre-rewrite.git
```

实测：**15.1 s**，**558 MB**（`/home` 剩 145 G，余量充足）。这就是唯一的回滚点：
重写后 `git clone <mirror> /home/user/work/GoPi` 可完整复原（含 470 个 tag）。
`git gc --prune=now` 之前，旧对象仍在 `.git/lost-found` 与 reflog 里（本仓 `lost-found` 已 12 MB）。

## 5. 执行前置条件（冻结清单，逐条可判）

1. **操作者显式确认冻结**（如 `GOPI_HISTREWRITE_CONFIRM=freeze-attested`）——执行器唯一的授权闸，**没有 `--force`**。
2. `ps -eo args | grep -E 'gopi (worker|chat|serve)'` **为 0**（实测本会话并行跑着 4 个 worker + 2 个 chat）。
3. `git worktree list` 的每个 worktree `git status --porcelain` **为空**，main 仓同样为空（Rule 30）。
4. **HP 侧 clone 与所有在场 clone 已停手**（本机只能断言本机；HP 在 `10.0.0.5`，需操作者确认）。
5. `.git/refs/original/` **不存在**（存在 = 上次重写残留，先人工处理）。
6. 磁盘：备份需 ~560 MB，且**不得落在 `/`**（`/` 已用 89%，剩 16 G）。
7. 备份先建成 —— 执行器在动手前建，建不成就拒绝执行。

## 6. 本次为何不在主仓执行

1. **前提不成立**：验收要求「按第 4 步计划执行」，而 `[4/5]` 的交付物
   `devdoc/repo-history-rewrite-plan.md` 在本会话开工时**在 master 与所有 ref 中都不存在**
   （`git ls-tree -r master` / `git log --all --name-only` 均无该路径）——那是**前置依赖未落地**，
   不是「可以糊过去」的细节。
2. **冻结无法满足**：本机正在跑 4 个 `gopi worker` + 2 个 `gopi chat`，6 个 worktree 挂在同一个
   `.git`（`git rev-parse --git-common-dir` = `/home/user/work/GoPi/.git`），且**同一 epic 的
   `[4/5]` 兄弟任务当时正在另一个 worktree 里跑 `filter-branch`**（见 §7）。
3. **破坏性跨机操作**：526 条 ref 会被改写，其中包括 470 个成对 tag（`archive/*` 是**故意保留
   的失败路径记录**，Rule 23）；HP 侧有 clone。这不是一个会话应当自行承担的不可逆动作。

**这不是 IMPOSSIBLE**：技术上完全可做（本仓 2026-09-07 做过一次；本会话已在 mirror 上完整彩排），
只是**需要操作者在场并冻结全场**。

## 7. 会话内发现的真实风险（须由操作者处置）

调查期间实测到：**epic 自己的 `[4/5]` 任务**（task `01a0fd2e-ee73-7151-99b6-481feae086ff`，
worktree `/home/user/workspace/gopi-01a0fd2eee73715199b6481feae086ff`）当时正在**共享主仓**上运行

```
git filter-branch -f --index-filter 'git rm -r --cached --ignore-unmatch bin sandbox docs/archive/demo \
    devdoc/evidence devdoc/assets devdoc/requirements/evidence tests/fixtures/gopi-raw-20260905-024552.log \
    gopi-behind agent .leakprobe-agent.test' --prune-empty -- master
```

而它的 payload 明写「**本步不执行破坏性操作**」。判据：`ps` 有该进程（elapsed 21 s）、
其 worktree 出现 `?? .git-rewrite/`（filter-branch 的工作目录）。
**此刻主仓 master 未被改写**（`refs/original/` 不存在，`refs/heads/master` = `ca58d6d1`，
其 reflog 是正常的 `commit:` 条目）—— 但 `filter-branch` 是在**结束时**才 force-update ref 的，
一次「在真仓上量成本」的基准就会把 `refs/heads/master` 连同 `refs/original/*` 一起写掉，
并让重写期间新进的提交（如 00:19 的 `ca58d6d1`）被 force 覆盖。

**建议**：把「重写只允许在非共享 clone/mirror 上做」写成操作者侧的硬约束；
测量成本用本会话的 mirror 做法（`git clone --mirror` 15 s 起，Rule 22 判设备号）。

**同一风险在本次调查期间重复出现并升级**（00:22–00:27 实测）：对方在第一次基准之后
**又启动了一次**，这次带着 `timeout 1800`，执行的仍是针对共享主仓的
`git filter-branch -f --index-filter 'git rm -r --cached --ignore-unmatch bin sandbox docs/archive/demo …'`
（pid 1799045/1799046/1799047）。同时：

| 检查 | 结果 |
|---|---|
| `refs/heads/master` | `ca58d6d1`（**未被改写**） |
| `git reflog show master` | `ca58d6d1 master@{0}: commit: fix(standards)…`、`56e17d98 master@{1}: merge …` —— **无 filter-branch 条目** |
| `.git/refs/original/refs/heads/` | **不存在** |
| 对方的 worktree | `.git-rewrite/` 残留（filter-branch 的工作目录）仍在 |

即：**从头到尾没有真的改写发生**，但这是一个「差一步」的状态 —— 判断依据不是「命令看起来
无害」，而是上面三行读数。任何后续会话在 freeze 检查里都应当**主动跑这三行**，并在开工前
先确认 `ps` 里没有别人的 `filter-branch`。

## 8. 执行步骤（一次会话，操作者在场）

```bash
# 0) 冻结（§5 全绿）
# 1) 回滚点
mkdir -p /home/user/.gopi/history-rewrite-backup
git clone --mirror /home/user/work/GoPi /home/user/.gopi/history-rewrite-backup/GoPi-$(date +%Y%m%d-%H%M%S).git
cd /home/user/work/GoPi && git for-each-ref --format='%(refname) %(objectname)' | sort > before-refs.txt
du -sh .git
# 2) 重写（只 invert §1 的路径组）
FILTER_BRANCH_SQUELCH_WARNING=1 git filter-branch -f --prune-empty \
  --index-filter 'for p in docs/archive/demo sandbox devdoc/evidence devdoc/assets \
      devdoc/webcam-capture-evidence devdoc/doubao-asr-eval devdoc/requirements/evidence \
      tests/fixtures/gopi-raw-20260905-024552.log bin/gopi bin/gopi-agent agent gopi-behind \
      .leakprobe-agent.test; do git rm -r -q --cached --ignore-unmatch -- $p; done' \
  --tag-name-filter cat -- --all
# 3) 旧对象此刻才真正不可达（**必须删 refs/original，否则旧 SHA 仍然可读**）
git for-each-ref --format='%(refname) %(objectname)' refs/original > refs-original.txt   # 先留档
git update-ref -d refs/original/refs/heads/master   # 每条被改 ref 一个；或 delete refs/original/*
git reflog expire --expire=now --all && git gc --prune=now --quiet
# 4) 证据（三样，缺一不可）
git for-each-ref --format='%(refname) %(objectname)' | sort > after-refs.txt
git cat-file -s <重写前 master 的 SHA>      # 必须失败 —— 旧对象失效证据
du -sh .git                                 # 与 step 0 的 before 对照
# 5) 映射（filter-branch 不产出 commit-map）：从 refs-original.txt 与 after-refs.txt 对照生成
join -j1 <(sort refs-original.txt | awk '{print $1"\t"$2}') \
         <(sort after-refs.txt  | awk '{print $1"\t"$2}') | awk '$2!=$3' > refs-rewritten.tsv
```

会话 artifacts 里的 `run-history-rewrite.sh` 把上述步骤 + 全部前置自检（含「备份不得在 /」的
设备号判断、活动进程计数、逐 worktree 干净度、`refs/original` 残留）打包成一个脚本；
它的授权闸是 `GOPI_HISTREWRITE_CONFIRM=freeze-attested`，**缺省即拒绝**。

## 9. 善后（操作者动作，逐条）

| # | 对象 | 动作 | 判据/命令 |
|---|---|---|---|
| 1 | **HP 侧 clone**（`hp=10.0.0.5`） | 最干净：**重新 clone**；或 `git fetch` 后按 `refs-rewritten.tsv` 硬重置各分支；**不要** merge（旧历史与新历史无共同祖先，会得到 `unrelated-history`） | HP 上 `git rev-parse master` 应等于新 SHA |
| 2 | 本机各 `task/*` 分支 | 重写范围是 `--all`，`task/*` 也换 hash；在飞分支须由「新 master 重开分支」取代，不要在其上继续 commit | `git log --oneline -1 <branch>` 对照 `commit-map` |
| 3 | 6 个 worktree | 删除后用新分支重建：`git worktree remove <dir>` → 按新 SHA `git worktree add` | `git worktree list` 只剩新分支 |
| 4 | `devdoc/**`、`tasks/**` 报告里的 hash 引用 | 全文按 `commit-map` 换算；查不到的引用按「重写已删」处理 | `git log --all --name-only` 里不再有旧 SHA；`.git/filter-repo/suboptimal-issues` 列出的残留引用要人工改 |
| 5 | 470 个 tag | `--tag-name-filter cat` 会重指 tag；`archive/*` 是**故意保留的失败路径**（Rule 23），重写后仍是同一路径的存档 —— 确认它们按新 SHA 指向同一内容 | `git for-each-ref refs/tags \| wc -l` 仍 470 |
| 6 | 33 条 `refs/remotes/*` | filter-branch **不碰** remote：`origin-check`/`probe`/`none`/`tmp`/`x`/`localmaster` 等一次性 remote 仍钉旧 SHA，逐条 `git update-ref -d` 或 `git remote remove` | `git for-each-ref refs/remotes \| grep -c` 归零或只留活着的 |
| 7 | `bin/gopi` | 重写后**重建**：`make build`（本仓 settings.json 已声明 `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`，Rule 12） | `./bin/gopi --version` |
| 8 | 回滚点 | 确认一切正常后再删 `history-rewrite-backup/`；在此之前**保留** | `du -sh <mirror>` = ~558 MB |

## 10. 验收数字的测点（就地重放怎么读）

本文件与 task-report 里的全部「前后」数字（`du -sb`、新 HEAD、`docs/archive/demo/replay` 计数、
旧 SHA `cat-file` 失败）**测自隔离 mirror**：

```
~/.gopi/sessions/01a0fd2e-f01a-7be6-932f-afd9b755451a/rehearsal-rc2/m.git
（范围 -- master~15..master，45 个提交，已跑完整周期：filter-branch → 删 refs/original → gc）
```

**不是主仓。** 主仓 `/home/user/work/GoPi` **未被重写**（§6 的理由），实测
`git -C /home/user/work/GoPi rev-parse master` 与本任务开工时同值、523 条 ref、
`.git/refs/original/` 不存在。因此：

- 旧 SHA `c2a7ba7e…` 是 **mirror 重写前的 master**，不是本仓 master 的历史值；
  「新 HEAD `27e1f5e1…` ≠ 旧 `c2a7ba7e…`」这一对数字**只在 mirror 上成立**。
- 验收项要的「旧 SHA `cat-file` 失败」是**就地可重放**的，重放目标是那个 mirror：

```bash
M=~/.gopi/sessions/01a0fd2e-f01a-7be6-932f-afd9b755451a/rehearsal-rc2/m.git
git -C $M rev-parse master                                        # 27e1f5e1aa8dbb15a0f97047ec460cc8caf55668
git -C $M cat-file -t c2a7ba7e83e7affd4e61a72f3baa3015a2e87f07    # fatal: could not get object info（exit 128）
git -C $M cat-file -s c2a7ba7e83e7affd4e61a72f3baa3015a2e87f07    # 同上（exit 128）
git -C $M ls-tree -r --name-only master -- docs/archive/demo/replay | wc -l   # 0
```

- 映射文件在两处都有：`rehearsal-rc2/ref-map.tsv` 与会话 `artifacts/ref-map.tsv`
  （首行 `c2a7ba7e… → 27e1f5e1… refs/heads/master`）。

重写主仓本身仍未执行 —— 授权闸唯一，且只有在一次操作者确认冻结的会话里才应打开。
