<!-- doc-class: historical -->
# 客户机上的两类环境性全红：suite build-cache sandbox 与外部二进制 skip

**日期**：2026-10-11 · **主机**：hosta（client） · **分支**：`task/01a12b54e636796fa97e5cfccd3ebb06`

## 症状

```
$ make test-budget PKGS='./internal/tools ./cmd/agent'
```
baseline（改动前）：`internal/tools` 14 例红 + `cmd/agent` 3 例红（同主机另一次全量 run 35 例）。
两类原因，**都不是回归**：

| 类 | 现象 | 计数 |
|---|---|---|
| A | `exec: "ctags": executable file not found in $PATH` / 同形 rg | 7/17（35 例 run 中 ~11） |
| B | merger BUILD gate `ABSTAINING (no usable shared Go build cache)` | 8/17（35 例 run 中 21） |
| C | `TestChatCookedFallback_NonTTY` 等存量红（与本任务无关） | 1+ |

逐条归因与逐字原始输出：`~/.gopi/sessions/01a12b54-…/attribution.md`。

## 根因

### B — 两个候选被同一套规则的**两个不同半边**拒

`internal/tools/testmain_test.go` 的 `toolsPinBuildCacheSandbox()` 按顺序试两个候选：

1. `<scratchRunRoot>/gopi-tools-suite-build-cache` —— 本机 scratch run root 落在 **/dev/shm（tmpfs）**，命中 **ban 2（off-RAM，2026-09-15 事故：per-process 缓存写 726M–1.14G/次，把 /dev/shm 打到 100%）**；
2. `$HOME/.cache/gopi-tools-suite-build-cache-<pid>` —— 本机 **`/home/user` 与 `/` 同为 device `66308`**（单块盘装整个系统），命中 **ban 1（off-`/`，2026-09-11 耗竭）**；
3. 不 pin 时，gate 自己解析的默认根是 `<worktree root>/gopi-build-cache/...`，而 `testutil` 把测试进程的 worktree root pin 进了 **scratch 树**（`pinUnderTestWorktreeRoot`）→ 命中 **ban 3（collected scratch tree）**。

无路可走 ⇒ `TestSuiteBuildCacheSandboxIsPinned` 红 + gate ABSTAIN ⇒ merge/mergeset/delivery-point 家族全红。

**本机几何（实测 `stat -c %d`）**

```
66308 /                     nvme1n1p2  1.8T 94%
66308 /home/user
66308 /home/user/.cache
66308 /tmp
   27 /dev/shm              tmpfs
66305 /home/user/workspace  nvme0n1p1  3.6T 84%   ← 唯一 off-/ 且非 scratch 的树
```

### A — 缺 ctags / rg 的测试没有与仓库既有约定对齐

仓库里已有 25+ 处 `t.Skip("ctags not installed")` / `t.Skip("rg not installed")`，
但一整族测试**从别的路径**摸到同一个外部二进制而没加守卫，于是把「主机缺包」报成「本仓回归」。

## 改动

### A. `internal/tools/testmain_test.go` —— 第三候选 + 改用 gate 自己的谓词

- 新增 `toolsBuildCacheSandboxCandidates()`，候选顺序：
  1. `<scratchRunRoot>/gopi-tools-suite-build-cache`（历史第一候选，保留）
  2. **新增** `<workspace home>/workspace/gopi-build-cache/gopi-tools-suite-build-cache-<pid>`
     —— 即 **production gate 的默认根那一层**，但带 suite 自己的名字与 pid 后缀，
     `toolsRemoveBuildCacheSandbox` 负责删除；operator 真实的 per-repo 缓存目录在旁边**不动**。
     这是本机唯一同时满足「off-`/`」与「不在 collected scratch tree」的树，**gate 也接受它**——
     这正是关键性质：sandbox 若被 gate 拒，suite 只是把自己的弃权证明了。
  3. `$HOME/.cache/...`（保留，但降到**最后**：它在 `$HOME` 与 `/` 同盘的机器上被 ban 1 拒，
     把「gate 拒绝的根」排在「gate 接受的根」前面，正是两半当初分歧的来路。）
- `toolsDirIsDiskSafe()` 改为直接调用 gate 的同一组谓词
  `scratch.AssertGoCacheOffRootFS` / `AssertGoCacheOffShm` / `scratch.IsInScratchTree`
  （Rule 14：一套规则一个来源），弃用原先在本文件里**重新拼写**的
  `MemoryFilesystem + SharesRootFS(dir 的 parent)` —— 那个拼写有两个自伤：
  量 parent 使尚未创建的候选测不到，而 `sharesRootFS` 单独不知道 scratch 树里的一切都会被 age GC 收走。
  两个设备谓词都会把**尚不存在**的路径解析到最近的既有祖先，所以「先判后建」仍然成立。

### B. 缺外部二进制的**具名、可审计** skip

- `internal/tools/toolpath_test.go`：`requireCtags(t)` / `requireRipgrep(t)`，
  skip 文案沿用仓库既有拼写（`ctags not installed` / `rg not installed`），
  在 stream JSON 里是 `"Action":"skip"` —— **可审计**，不是静默通过。
- `cmd/agent/toolpath_test.go`：同形的一份（本包没有 `ctagsAvailable()`，
  以 `exec.LookPath` 直接判）。
- 加守卫的测试（9 例 / 含 3 子测共 11 个断言点，覆盖**不删**、断言**不减**）：

| 包 | 测试 | 加的函数 |
|---|---|---|
| internal/tools | TestCodeSearchSurvivesEOFProbe | requireCtags |
| internal/tools | TestQueryDefinitionHealsMidEntryTornIndex | requireCtags |
| internal/tools | TestQueryDefinitionDoesNotReadAMissOffATornIndex | requireCtags |
| internal/tools | TestQueryDefinitionSelfHealsUnsearchableIndex | requireCtags |
| internal/tools | TestCodeSearchWithTimeout_ShortTimeoutNormal | requireRipgrep |
| internal/tools | TestSearchWithTimeout_NoMatchReturnsEmptyNotError | requireRipgrep |
| cmd/agent | TestCodeSearchBackendNoSymbol | requireCtags |
| cmd/agent | TestKeywordSearchRunPrintsHitsAndNoMatches | requireRipgrep |
| cmd/agent | TestSearchWrongGroundExitCodeOnTheRealBinary（3 子测） | requireCtags / requireRipgrep |

  已有 shim 路径**原样保留**（`testCtagsShim` / `testTornCtagsShim` 安装替身 ctags，**不 skip**）——
  对工具路径的覆盖没有被本次改动碰过一行。

## red-proof（先红后绿，双向）

守卫必须**双向**成立：缺二进制时**必须 skip**；有二进制时**必须不 skip**（否则等于把测试删了还报绿）。
`testing` 不让父测试询问子测试是否 skip，所以守卫用**子进程**测量：
把本测试二进制以受控 `PATH` 重新执行，读 `go test` runner 自己打印的 `--- SKIP:` 行。

```
# 绿（正常实现）
--- PASS: TestToolSkipIsConditionalOnPath (1.33s)      internal/tools
--- PASS: TestToolPathSkipIsConditionalOnPath (2.27s)  cmd/agent

# 红证：把 requireCtags 改成无条件 t.Skip（临时，仅用于取红，已还原）
--- FAIL: TestToolSkipIsConditionalOnPath (1.63s)      cmd/agent
    toolpath_test.go:103: with a fixture "ctags" on PATH the probe child SKIPPED — the guard would
        drop this test's coverage on every host:
            toolpath_test.go:69: ctags not installed
--- FAIL: TestToolSkipIsConditionalOnPath (1.52s)      internal/tools
    toolpath_test.go:140: with a fixture "ctags" on PATH the probe child SKIPPED — …
# 还原后再次全绿
```

## 不做的事（如实）

- **不装 ctags / rg**：主机包，需要 sudo；Rule 25（不得把权限当作缺的那味药）+ 本 run 的 bash guard 直接拒绝 `sudo ` 模式。
  operator 一条命令见报告 `## Not Done`。
- **不 stub / mock 掉任何被测路径**：跳过的只是「本机没有这个二进制」这一事实，
  断言与覆盖留在原处。
- **不动 `TestChatCookedFallback_NonTTY` 一类存量红**：与本任务无关（独立缺陷，Rule 41 的取证另记）。

## 顺带发现的一条既存红（Rule 41：取证即提行）

基线复跑对比时，`TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow` 表现为「新增红」。
逐项排查后**与本任务两处改动无关**（`git show --name-only ebaa80382` 不含该测试、也不含
`taskzombie_extinct.go` / `cli_tasks_worker.go`），它是对端 master 上的既存红：

| 工作树 | HEAD | 含 `71577959e`? | 隔离单跑 ×3 |
|---|---|---|---|
| 本任务分支 | `ebaa80382` | 是（delivery-point sync 合入对端 master） | FAIL FAIL FAIL |
| 本机 master | `7fb3ccd5d` | 否 | ok ok ok |
| origin/master | `a3a31da42` | 是 | FAIL FAIL FAIL |

```
go test -count=1 -run '^TestTaskLifecycleTick_DisposesTheNeverStartedAutoRow$' ./cmd/agent
--- FAIL: … task_lifecycle_litter_test.go:95: the never-started auto row is still "in_progress" after the tick
    [worker] [lifecycle] stale reported=2 disposed=0
```

**归因是推断不是断言**：只做了「含它红 / 不含它绿」的相关性对比，逐行机理未验。
`71577959e` 改的正是本测试所测的 disposal 链（`taskzombie_extinct.go` +373 / `cli_tasks_worker.go` +20）。
已按 Rule 41 取证并提行（peerbox，`--tags bug`）：
**任务 `01a12b79-24cb-7eee-995d-3125dd372de4`**，
证据 `~/.gopi/sessions/01a12b54-…/anomaly-peer-master-71577959e.md`。
**任何按交付点同步过对端 master 的分支都会带上它** —— 这是本机 master 看不到它的原因。

## 常见坑（供后来者）

1. **`$HOME` 与 `/` 同设备**不是异常主机——单块盘装系统就是常态；**不要**用
   `workspace` 之外的 home 级目录当「磁盘候选」，本机只有 `/home/user/workspace` 在另一块盘。
2. 判断候选合法性要用 **gate 的谓词**，不要在本文件里重新拼一遍规则：
   suite 与 gate 一旦对「缓存可以住在哪」有各自的说法，两边就会分歧（这次就是这样坏掉的）。
3. `Makefile` 的 `PKGS=` 是 **shell 分隔的一次**展开：写 `PKGS='./a ./b'`；写成 `PKGS='./a' './b'` 会让
   b 变成第二个目标（实测）。
4. 长跑要用 `job(action=start)`：`make test-budget` 两个包约 10–25 分钟，前台会被空闲输出网掐死。
