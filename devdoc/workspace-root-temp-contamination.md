<!-- doc-class: historical -->
# Task Report: worktree 根目录临时污染——逐目录归因、根因、窄修法与清理记录

分支：`task/01a114edb83e72d8a88c01f08ddcb42a`（worktree：`/home/user/workspace/gopi-01a114edb83e72d8a88c01f08ddcb42a`）

## Completed

**1. 逐目录归因（对 `/home/user/workspace` 全部 198 个直接子项，零删除、零修改地分类）**

实测（本会话第一次 census，即清理前基线）：

```
scratch-gc role=cli-roots home=/home/user/workspace removed=0 stale=none disabled=false \
  root=/home/user/workspace children=198 worktree=19 stub=48 residue=23 foreign=108
```

| 类别 | 数 | 是什么 | 判据 |
|---|---|---|---|
| `worktree` | 19 | `.git` 是**目录**（仓库/主检出），或 `.git` 指针的 **gitdir 目标仍存在**（活 worktree 登记）；7 个在 `git worktree list` 里，另 12 个是**未登记但登记信息仍活**的目录 | `.git` 的形态 + 目标存在性（`/proc` 无关） |
| `stub` | 48 | 测试进程泄漏的**死指针**：`gopi-<32位十六进制>`，`.git` 指向 `/dev/shm/suite-*-run-*/…/.git/worktrees/…`，该路径已随 suite 运行消失 | 既有 `judgeWorktreeStub` 的全部 8 条条件 |
| `residue` | 23 | 本系统自己的命名铸出的**非 worktree** 目录：`gopi-attrib-evidence`/`gopi-scan-tip-evid`/`gopi-chat-ui-debug-evidence`（报告按名字引用的证据）、`sbxcheck-cfvY`/`sbxref-T7Zc`（Rule 34 sandbox 探针）、`tmp-m2-6Zgx`/`vocab.tNMt`（mktemp 工作目录） | `gopi-` 前缀 ∪ `IsBaseResidueName` ∪ `os.MkdirTemp` 随机尾 |
| `foreign` | 108 | 本系统不铸的名字：平铺文件、符号链接、人取的目录名（`backup`、`20210804`、`annex-fixture`、`fog`、`cplusplus`、`purec`、`homm3-m1`） | 三条阳性家族全不命中 |

**2. 四个诊断问题的实测答案**

- **Q1 谁建的**：三类，各有实测出处。
  `stub` 由**测试进程**建：`.git` 指针指向 `/dev/shm/suite-agent-run-*/TestReframeChildRowsSurviveTheParentGroundsDeath…/001/reframe-main/.git/worktrees/gopi-…`，即 `internal/agent` 的 reframe fixture 用真实 `git worktree add` 建 ground，进程被杀后没回收。`residue` 由**会话/探针**建：`sbxref-T7Zc` 等的 `GOPI_SANDBOX_DIR` 被显式指到 `/home/user/workspace`（会话记录里逐条可见：`root=/home/user/workspace/sbxref-T7Zc/sbx source=from GOPI_SANDBOX_DIR`），`gopi-*-evidence` 是会话把取证材料直接写到 worktree 根。
- **Q2 有无活进程 cwd**：**有**。Rule 24 判据（`/proc/<pid>/cwd`，本机 33 个命中）：`pid 1333966/1333967 → gopi-bin-01a0d9ab`、`pid 3785719/3785721 → gopi-chat-ui-debug-evidence`；`/proc/<pid>/fd` 还显示这两个目录里有**被打开的文件**（`fakellm-tg2.log`、`fakellm-tn.log`）。这正是"清理必须先判活"的实证：这两处**不是**惰性垃圾。
- **Q3 为什么落在 workspace 而不是 scratch 根**：`TMPDIR=/home/user/workspace/_tmp`（`GOPI_TMPDIR_SOURCE=from the caller's TMPDIR`），`GOPI_WORKTREE_ROOT` **未设**。所以两个独立通道都把 workspace 当根：(a) 未 pin 的临时目录默认落在 `$TMPDIR`，而 `$TMPDIR` 就在 workspace 下；(b) worktree 根解析回落到 `worktreeroots.DefaultRoot()` = `$HOME/workspace`。两者都在 `/home` 分区（dev `66309` ≠ `/` 的 `66307`），Rule 22 的"离开 `/` 分区"这一条**是满足的**——问题不在分区，在**命名与 GC 管辖**。
- **Q4 有无活跃产物混入**：**有，且分两种**。① 未登记但登记信息仍活的 12 个 `gopi-*`（如 `gopi-01a0db4231427aa79740ff9916dae51f`、`gopi-01a0ef0fc1567db9990d01b609789599`，gitdir 目标存在），census 判 `worktree`、**一律保留**——它们不是"死残留"，是别人（可能已死的会话）的工作。② 取证产物 `gopi-*-evidence`（`gopi-chat-ui-debug-evidence` 69 个文件，含 png 与日志；`gopi-bin-01a0d9ab` 的 `drive*.py`、`*.chunks`），报告按名字引用它们，删掉等于销毁证据。

**3. 根因（5-why，到机制为止）**

1. 症状：worktree 根下 95 个 `gopi-*`，只有 7 个是真 worktree。
2. 机制一：**任何东西都不报告这个差集**。`internal/scratch` 的每一条 sweep 要么按命名空间限（`gopi-`/`gocache-`/`probe-`）、要么按根限（scratch home、声明的树、profile 根、sandbox 根），而 workspace 这个"与真 worktree 共用的目录"**没有任何一个 arm 认领**。
3. 机制二：产生侧**曾经**在测试里解析出真 workspace 根。`pinUnderTestWorktreeRoot`（`36efc343f`，2026-10-01）之前，`TaskRunGroundFor` 进程内解析 `DefaultRoot()` = `$HOME/workspace`，所以宿主测试真的在 workspace 里 `git worktree add`——48 个 stub 的 mtime 与 `/dev/shm` 的运行名字正是那批。
4. 机制三：**那条修法有效，但没有可见性**。实测：清理后 census 里 `stub=0`（48 个全是死指针），且当前只剩 6 个活 `gopi-*`（本次测试跑还新建/使用了 4 个，全部目标存在、会被 KEEP）。也就是说"新的死指针不再落在这里"**已经为真**，只是**没人能看见**，所以 48 个旧残留一年多没被清。
5. 根因：**缺少"这个共用目录里有什么、每一样是什么"的census**。不是缺少又一条删除规则——而是缺少把删除之外的 155 个条目**说出来**的通道。

**4. 修法（窄修 + 真实 surface）**

- `internal/scratch/worktree_root_residue.go`：**分类谓词**。四类（worktree / stub / residue / foreign），每类都带"决定它的那句话"。worktree 判据优先且是**正证据**（`.git` 目录，或指针目标存在）；stub 类**原样复用** `judgeWorktreeStub`（8 条条件、含 `/proc` 活体否决），不新增可删集合；residue/foreign 一律 `Removable=false`。
- `cmd/agent/cli_scratch_gc_roots.go` + `gopi tool scratch-gc roots`：**census arm**。裸形态只报告；`--yes` 时删除半边**委托给既有 `scratch.SweepWorktreeStubs`**（同一谓词、同一 sweep），并断言"census 说的可删数 = sweep 实删数"，不一致即 exit 1。`--explain` 打全部子项，`--root` 指定根（走既有 `WorktreeStubRootRefusal`，home/`/`/gopi profile/git 工作树一律拒绝）。
- **不是新依赖**（Rule 35）：无守护进程、无 cron、无状态文件，只是既有 verb 的一个 arm + 包内一个谓词；`internal/scratch` 的既有 sweep 挂在既有 tick 上。

**5. 回归测试**（`internal/scratch/worktree_root_residue_test.go`，6 个）
`TestRootCensusClassifiesEveryChildWithEvidence`（四类各一，且**删不删只看 Removable**）、`TestRealWorktreesAndOperatorDirsAreNeverRemovable`（真 worktree / 证据目录 / 人取的名字 / **同名不同族的死指针** 全部留下，只有 stub 走）、`TestDeadPointerInsideTheNameFamilyIsCollectableOutsideItIsNot`（名字族边界双向钉住）、`TestRootCensusIsReadOnlyOnAMissingRoot`、`TestRootCensusRefusesDangerousRoots`、`TestRandomSuffixPredicateSeparatesMintedNamesFromChosenOnes`（随机尾谓词双向），外加 2000 子项的一次遍历预算。

**6. 清理记录**

**删了（48 个，9167 字节）**：workspace 根下的 `stub` 类——`gopi-<32位十六进制>`、`.git` 死指针指向 `/dev/shm/suite-agent-run-*/…`（含 `TestReframeChildRowsSurviveTheParentGroundsDeath*` / `TestRepairRepoMovesARowOntoALiveWorkTree*` 的运行目录）。删除前后 children 198 → 150，`worktree=19`、`residue=23`、`foreign=108` **一个没动**。

**保留（150 个）及理由**：
- 19 个 `worktree`：6 个 `git worktree list` 登记项 + 13 个未登记但登记信息仍活（含 `gopi-01a112ce…`、`gopi-01a114df…`、`gopi-probe-mc01a0db25`）——未知=KEEP（Rule 30/6）。`git worktree list` 计数与删除前一致。
- 23 个 `residue`：证据目录（`gopi-chat-ui-debug-evidence` 等，其中**有活进程持有**）、sandbox 探针残留、mktemp 目录——报告按名字引用，删除是 operator 的判断，不是 sweep 的。
- 108 个 `foreign`：平铺文件与人取的名字。

## Not Done (honest)

- **未清理 23 个 residue + 108 个 foreign**：这是刻意的范围决定，不是遗漏。其中 `gopi-chat-ui-debug-evidence` **正被活进程 pid 3785719/3785721 占据并打开着日志文件**，`gopi-bin-01a0d9ab` 同样有活 cwd；删它们既越权也可能破坏正在跑的东西。census 的价值就是把这批**命名出来**交给 operator，本轮到此为止。
- **未修 `$TMPDIR` 指向 workspace 的配置**（`TMPDIR=/home/user/workspace/_tmp`）：这是调用方导出的环境，`_tmp` 是**声明过的**临时根、GC 正常管辖（本次 census 里它被算作一个普通 foreign 子项，`_tmp` 内部由既有 trees arm 管），不属于"污染"。改它属于另一个 epic（配置层的 operator 决定）。
- **未修 12 个"未登记但登记信息仍活"的 `gopi-*`**：它们的登记还在、内容可能是别人的工作；判定归属需要读账本与分支，不在本 epic 范围。
- **stub 的"不再产生"这一条只有本机实测证据**（`stub=0`，且旧 stub 的 mtime 全部早于 `36efc343f`），没有做跨主机的长期观察。

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK；`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Tests: `go test ./internal/scratch/ -run 'RootCensus|RandomSuffix|RealWorktrees|DeadPointer' -count=1` → `ok github.com/nick/gopi/internal/scratch 8.526s`
- Tests: `go test ./cmd/agent/ -run 'TestToolsMaintenanceHelpSurface' -count=1` → `ok github.com/nick/gopi/cmd/agent 36.707s`（含 inventory 一致性、每形态有测或声明 blocked、页面来自其自身常量三个 gate）
- Tests: `make test-budget PKGS='./internal/scratch ./cmd/agent'` → `internal/scratch` **PASS**（`Elapsed: 289.658`，含本 epic 新增的 `TestRootCensus*` 全部子测试）；`cmd/agent` 有 2 个失败，**attribution 见下，均非本 epic 引入**。
- **cmd/agent 失败归因（自查，非结论式声明）**：
  - 失败是 `TestPrefetchBusyIsRetriedWithinTheSameBudget`（`context deadline exceeded`，1500ms 预算在 load 24 下超时）与 `TestCLINamingExceptionsAreStillDispatchable`（`"web_search"/"web_fetch" has no contract-inventory entry`）。
  - **本分支与这两个失败面零差异**：`git diff --stat master...HEAD -- cmd/agent/cli_contracts_test.go cmd/agent/cli_completion_exceptions_test.go cmd/agent/chat_prefetch_segments_test.go internal/agent/` → **空**；`git diff --name-only master...HEAD` 的全部 8 个文件里没有它们。
  - **更早的独立运行已含同两个失败**：`/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791364386-1218732.json`（时间戳 **17:13**，早于本 commit 17:53；发起进程 pid 1370797，属于**另一个 worktree** `gopi-01a114df436175c3b71ed508914410c1`，scope `./cmd/agent/ ./internal/standards`）在该包有 **9 个**失败，**完整包含**上面这 2 个（其余为 `TestLedgerOpenersUseProductionDSNContract`、`TestMergePromptCoversEveryRequiredSection`、`TestMergeUnionRowCheckOnTheRealManifest`、`TestSpawnersPinTheRepoContext`、`TestSubmitSpawnersGiveTheChildAResolvableRepo` 等）。
  - **读法**：这是**既有、跨 worktree、环境级**的 `cmd/agent` 失败，不是本分支的；`test-budget` 的 stamps 显示同期 load_start 15–18、load_end 达 24.8（8 核），`TestPrefetch*` 的 1500ms 预算在这个负载下必然超时——**这正是本 epic 所测的共用根目录同址争抢的一种后果**，记录在此供 operator 另立 epic。
- Ran（真 surface，verbatim）:
  - 清理前：`gopi tool scratch-gc roots --root /home/user/workspace` → `children=198 worktree=19 stub=48 residue=23 foreign=108 dry-run=true`
  - 清理后：`gopi tool scratch-gc roots --root /home/user/workspace --yes` → 逐子项 `root-child class=stub … removable=true reason=a .git pointer to /dev/shm/suite-agent-run-1981446858/…, which no longer exists (the suite run that created it is gone), beside 1 leftover file(s)/8 bytes`；汇总 `scratch-gc role=cli-roots … children=198 worktree=19 stub=48 residue=23 foreign=108` 后接 `scratch-gc role=cli-roots home=/home/user/workspace removed=48 stale=none disabled=false bytes=9167`
  - `--yes` 的"census 与 sweep 必须一致"断言通过（`removed=48` = `stub=48`；不一致会 exit 1）
- Result: 再次 census → `children=150 worktree=19 stub=0 residue=23 foreign=108`；`git worktree list` 登记数删除前后同为 6，全部登记目录 `ls` 存在（`missing=0`）；未登记但登记信息仍活的 `gopi-01a0db4231427aa79740ff9916dae51f`、`gopi-probe-mc01a0db25`、`gopi-01a0ef0fc1567db9990d01b609789599` 原样存在；自身写作的 worktree 未被本 arm 触碰（其子项按 census 判 `worktree`/`foreign`）。
- Ran（活体复测，本 epic 自己跑 `make test-budget` 的同时，两小时后的第二次真跑）：
  `./bin/gopi tool scratch-gc roots --root /home/user/workspace` →
  `scratch-gc role=cli-roots home=/home/user/workspace removed=0 stale=none disabled=false root=/home/user/workspace children=151 worktree=20 stub=0 residue=48 foreign=83 dry-run=true`
  逐子项行 `48` 条、`class=` 计数 `48 residue`（默认形态只逐行打印可报告的类，worktree/foreign 只进汇总——与 arm 的设计一致）。
  **读法**：`stub` 仍为 **0**（旧泄漏源未复现）；`residue` 23 → 48 是**本 epic 自己的测试运行在实时增生的**（每次 `make test-budget` 都会 mint sandbox/scratch 探针目录），`removed=0` 且 `dry-run=true` 证明**默认形态永不删**——正是"只报告，删除留给 `--yes`"的设计在真实运行下被观察到。
- Commit: 见 `## Evidence (self-filed)` 的 commit 行

## Evidence (self-filed)

- Rule 30（写 worktree 干净）：`cd /home/user/workspace/gopi-01a114edb83e72d8a88c01f08ddcb42a && git status --porcelain` → **无输出**（提交 5fdd912dc 之后实测，本报告即在其中）；事后复核仍为空。
- Rule 24（活体再生证据）：本 epic 自身跑 `make test-budget` 期间重测 census，`children=146`、`stub` 型 14 个全部**指针存活**（gitdir 指向存在的仓库）——即当下 workspace 里**死指针 stub = 0**；实测脚本对全部 `*/` 逐项 `[ -e "$tgt" ]` → `dead_pointer_count=0`。这说明旧 stub 的**产生源已停**（mtime 全部早于 `36efc343f`），残留是一次性历史。
- Rule 6（只在自家 worktree 写）：`git status --porcelain` 在 `/home/user/work/GoPi`（主仓库）为**空**；本次两个新文件曾一度误写入主仓库，已 `mv` 回 worktree 并确认主仓库干净。
- Rule 22（临时区不在 `/`）：`stat -c %d` → `/home/user/workspace` = `66309`，`/` = `66307`；本 arm 的测试用 `t.TempDir()`（该包不能 import `internal/testutil`：testutil 反向 import 它，会构成测试循环）。
- Rule 24（活体判据）：上表 pid/cwd 全部来自 `readlink /proc/<pid>/cwd`，未用 `pgrep -f`。
- 3 处自身缺陷在本轮被测试抓住并修掉（记录以免后人重犯）：① census 的计数器曾被塞进共享格式的 `disabled=` 字段（语义错误，已改为 `dry-run` 后缀约定）；② `hasRandomSuffix` 原先要求"同时含数字和字母"，会把 `vocab.tNMt`/`sbxcheck-cfvY` 这类纯字母随机尾判成 foreign（已改为"混合大小写或含数字"）；③ 初版测试把 `live-repo`（`.git` 是目录）期望为 foreign，实际正确行为是 `worktree`——是断言错了，不是实现错了。
- `gopi-wtrecycle` 这个名字**落在既有的 stub 名字族**（`gopi-` + `[0-9a-zA-Z]{8,64}`）内，若其指针已死则**会被既有谓词收集**——这是**既有**行为，本轮不扩大也不缩小，已由 `TestDeadPointerInsideTheNameFamilyIsCollectableOutsideItIsNot` 双向钉住；测试 fixture 因此改用族外名字，而不是把这条真相"改掉"。
