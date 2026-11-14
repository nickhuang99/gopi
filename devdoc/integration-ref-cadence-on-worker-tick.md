<!-- doc-class: normative -->

> **Retired mechanism (2026-11-05).** The `cmd/agent/cli_tasks_sync_pass.go` call sites this page
> names were DELETED with the cross-node sync family. The ref cadence itself survives and is what
> the page is about only as a CLOCK: `serveGitSyncSweep` itself is GONE (its legs were the row sync
> tick and the delta switch, both deleted — `cmd/agent/ref_lag_tick_clock.go:20`), and the constant
> `refLagTickDefault` is what survived. Read the call-site list below as history.

# 本机集成分支的推进节奏：worker 回合上的 git 交付腿

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**任务** `01a13440-1e63-7be2-a1bd-7e199f2b66df`（epic：`devdoc/bare-master-is-not-a-revision.md` 的**节奏/面**半边）
**归属**：系统层缺陷，**应归 peerbox**（Rule 28）。本条在 hosta 上执行，改动落在 `internal/tools/task_sync_git.go` 与
`cmd/agent/` 的 tick 接线处 —— **属跨层，如实标注**。

## 问题（实测，非推断）

hosta（`role: "client"`）上 `refs/heads/master` 与对端的差：

```
$ git -C /home/user/work/GoPi rev-parse refs/heads/master refs/remotes/gitPushcheck/master origin/master
64b8a316af16c3ab6333ec06d65e868000815652   # refs/heads/master  —— 本地集成分支
fd8423be1a8d7ae43d3509b42042593b062cf809   # refs/remotes/gitPushcheck/master —— 对端 tip 的本地镜像
e7f3bf2d19673cc6efa03425cf708d9032c164f2   # origin/master
$ git -C /home/user/work/GoPi rev-list --count refs/heads/master..refs/remotes/gitPushcheck/master
169
$ git -C /home/user/work/GoPi merge-base --is-ancestor 64b8a31 fd8423b && echo ances
ances
```

即：该 ref 是**对端 tip 的纯祖先**，落后 169 commit（briefing 记的 141 是更早的一次取样）。

**为什么没人推进它**：`serveGitSyncSweep` 在改动前只有两个触发点 ——

- serve tick（`cmd/agent/cli_tasks_sync_pass.go:141`，`gopi task sync-pass --once` 的 pass）
- merger pass（`cmd/agent/cli_tasks.go:5874`，`mergerSerialFaceLegs` ④⑤）

而 hosta 常驻的是 **worker**（`ps`：`gopi worker`，pid 209851 / 449811），**无 serve、无 merger**。
更进一步：这两个进程的 cwd 在 `/home/user/.gopi/sessions/<id>`，而 `GitSyncRepoRoot()`
（`internal/tools/task_sync_git.go:383`）按 cwd 解析 `git rev-parse --show-toplevel` ——
**即使在 hosta 上跑 merger，它也拿不到 repo**，整条 git 腿在 `repo == ""` 处 skip。

结论：**这条 ref 在 hosta 上没有任何常驻载体**，只有「偶然站在主 work tree 里的人手工跑一次」才会动。

## 修法（Rule 35 形状：既有 tick 上再搭一条腿）

### 1. worker 回合加一条腿

`cmd/agent/cli_tasks_worker.go` 的回合顶部，在既有的三条 tick 之后（task-lifecycle ①、
scratch-gc ①′、memory-sync ①‴）加 ①⁗：

```go
if !once {
    serveGitSyncSweep(workerRoleLabel)
}
```

- **零新机制**：不加 cron、不加 systemd timer、不加脚本、不加状态文件、不加守护进程 ——
  就是一个已经在跑的组件的一次函数调用，形状与 `task_lifecycle_tick.go` / `scratch_gc_tick.go` 一致。
- ~~**节拍复用既有 clock**：`serveGitSyncSweep` 自己的 `serveGitSyncLastTick` 节流，
  读点唯一（`serveGitSyncInterval()` ← `GOPI_SYNC_GIT_INTERVAL`，默认 15min，Rule 2）~~
  —— **这一条是错的，别照抄。** `serveGitSyncInterval()` 在任何修订里都不存在，
  `GOPI_SYNC_GIT_INTERVAL` 没有任何生产读者（它只被目录表和 `gopi --help-env` 广告），
  两者都在 2026-11-05 随跨节点 sync 面删除。节拍今天由**克隆通道自己的交付循环**持有，
  没有操作员旋钮。反广告门：`internal/config/sync_knob_removal_gate_test.go`。
- `--once` 上跳过，和它上面三条 tick 完全一致：`--once` 是单测/E2E 接缝，测试 worker 不许
  碰真 ref（Rule 6）。
- **fail-soft**：pass 自己把失败打到 stderr，永远不能让一个回合失败。

### 2. `role` 参数化

```go
serveGitSyncSweep(role string)     // 三个面：serveGitSyncRolePass / mergerRoleLabel / workerRoleLabel
```

role 只用于**一个**用途：让那条唯一的告警行说清**是哪个面**发现 ref 旧的。
调用点同步：`cli_tasks_sync_pass.go`、`cli_tasks.go`（merger）、`cli_tasks_worker.go`（新）。
三个测试文件里的调用点同步带 `workerRoleLabel`。

### 3. 陈旧可见（不是静默状态）

新增 `gitSyncIntegrationLag(repo)` —— 取本地集成分支与**对端镜像 ref**（`gitPushbackCheckRef`，
交付通道自己在维护的那个 reservations ref）之间的距离，**两个操作数都是本地 ref**，
测量本身不碰网络、不会失败：

| 取值 | 含义 |
|---|---|
| `"0"` | 已在对端 tip（或领先 —— 那不算陈旧） |
| `"<n>"` | 落后 n commit |
| `unmeasurable` | 取不到数（还没有镜像 ref / repo 读不了） |

接着落到**三个**可见面：

1. **步骤**：report 里记一条 `integration-ref-stale` 步骤。落后 ≥1 时该步骤 **FAIL** ——
   于是它自动走了「每个失败步骤打一行 stderr」那条**既有**通道，不需要新通道。
   `aligned` 与 `unmeasurable` 都不 FAIL（Rule 15a：取不到数不是缺陷，是一个还没人问的问题）。
2. **摘要行**：`GitPassReport.Line()` 在有落后时追加 `integration_lag=<n>` ——
   一眼可 grep；aligned 时**不打**（否则每次健康 pass 都带噪声，训练人忽略它）。
3. **告警行**（`GitSyncStaleAlarm`）：stderr 上一行 `WARNING: stale integration ref — …`，
   句子**取自那条步骤的 Detail**（一次渲染，多个读点，Rule 14），并附上唯一能关掉这个缺口的命令
   （`gopi task push`，也就是同一条 pass 的手工面）。

server 臂（`gitSyncServerLag`）**如实报 unmeasurable 并说明理由**，且该步骤不 FAIL：
客户端比对的 `refs/remotes/gitPushcheck/master` 是「对端 tip 的持久镜像」，
而服务端同名 ref 是每次 pass 私有的 reservation —— **同名不同事实**。
在服务端编一个数，正是这个 epic 要终结的「静默成功」形状。

### 4. 测试

`internal/tools/task_sync_git_lag_test.go`（新）—— **真 git、真仓库**（判定是一个 ref 的值和一个计数，
文本 fixture 证明不了任何事），成对：

- `TestGitSyncClientPassClosesTheGapAndTheAlarmStaysSilent`（绿腿 + 控制组：关了缺口就闭嘴，
  包括摘要行不出现 `integration_lag`）
- `TestGitSyncClientPassAlarmFiresWhenTheGapSurvives`（红腿：本地多出 commit 造成 diverged，
  pass 关不掉缺口 → 步骤 FAIL、告警行带数字、摘要行带 `integration_lag=3`）
- `TestGitSyncIntegrationLagUnmeasurableIsNotAWarning`（第三态：删掉镜像 ref →
  `unmeasurable`，**不**编 0，也**不**告警）
- `TestGitSyncServerArmDoesNotInventTheLag`（诚实性守卫：服务端不编数）

## 一个测量上的坑（值得记下来）

第一次跑 fixture 时 `lag = "0"`，而 fixture 明明是落后的。原因：`git clone` 会把
**clone 那一刻**的 peer tip 作为本地 `refs/heads/master`，所以「先让 peer 前进、再 clone」
造出来的 client **一开始就是 aligned 的** —— 测的不是缺陷形状。

正确的造法：**先 clone，再让 peer 前进**（对象在 client 里缺，因此 lag 可算）。

## 验证（Rule 31）

见 `## Verification` 与 `## Evidence`（本 run 的 session 目录 `~/.gopi/sessions/01a13440-1e63-7be2-a1bd-7e199f2b66df/`：
`artifacts/e2e-final.log` + `artifacts/e2e-worker-cadence.sh`）。

## Not Done / 诚实边界

- **服务端（peerbox）的同一问题未验证**：本条只证明 client 面。peerbox 的 `refs/heads/master`
  由它自己的 merge 推进，本轮没有在 peerbox 上取证。
- **`gopi worker --once` 不触发该腿**（有意）：`--once` 测试/E2E 接缝。
- **只测了一个「落后且纯祖先」与一个「diverged」形状**；merge commit 之后的多路形状未单独构造。
