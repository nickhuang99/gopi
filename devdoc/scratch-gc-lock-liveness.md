<!-- doc-class: historical -->
# scratch-gc 的锁：放弃锁（持有者已死）必须当场打破，且每一次 defer 都要说话

分支 `task/01a1300b79da7224be8bb577ac655544`（worktree
`/home/user/workspace/gopi-01a1300b79da7224be8bb577ac655544`），off master `ff9a8b0e9`。
来源：bug row `01a1300b-79da-7224-be8b-b577ac655544`（由任务
`01a12fbd-c965-7205-85fa-0253312b6962` 的实测按 Rule 41 提行）。

## 症状（实测）

scratch home 里 `remove=0` 却「明明有可收项」：`--dry-run` 承诺
`would-remove <path> age=278h`，真跑 `removed=0`，**stderr 空**、exit 0。二者矛盾，
而操作者手上没有任何一行输出能把两者分开。

## 根因（两条，各自独立）

1. **放弃锁只按 mtime 判定。** `AcquireGCLock` 唯一的释放路径是「锁文件 mtime
   超过 `GCLockStaleAge`(10min)」，锁里写的持有者 pid 从不被读。一次**崩溃**的
   sweep 会留下一个**很新**的 `.gc.lock`（pid 写在里面、进程早已不存在），此后
   每一轮 `os.OpenFile(O_EXCL)` 都 EEXIST → `locked=false` → `SweepHome`
   `return 0, nil`。而「pid 是否还活着」是**当时就能判定**的（同宿主同用户的
   `/proc`）。
2. **没有句子（Rule 15）。** defer 之后没有任何对外输出，`removed=0` 同时是
   「无可收项」和「本轮根本没扫」的唯一表述。

## 修法

### `internal/scratch/gc_lock.go`（新文件，唯一判定点）

- `GCLockOutcome`（四值：`acquired` / `broke-abandoned` / `deferred-live-owner` /
  `deferred-unmeasurable`）与 `GCLockOwnerLiveness`（三值：`live` / `gone` /
  `unknown`）是**两组不同的词**，因为它们是两个问题：前者是本次调用拿到了什么，
  后者是锁里那个 pid 怎么样了（Rule 15a：`live` 与 `unknown` 绝不能共用一个值，
  只有其中一个可以授权打破）。
- `AcquireGCLockVerdict(home) (release, GCLockVerdict, error)` 是**唯一**的锁判定：
  创建失败（EEXIST）→ `gclockDisposition` 判持有者 → **GONE 立即打破并重试一次**；
  LIVE / UNKNOWN 保持 defer 并各自带句。
- `GCLockOwnerLivenessFor(pid)` 是 `/proc` 的唯一读法：目录不存在 ⇒ `gone`；
  目录在但 `stat` 读不到（无 procfs / 权限）⇒ `unknown`（**未知永不授权打破**）；
  状态位是 `Z`（僵尸）⇒ `gone`（僵尸会应 `kill(pid,0)` 却不做任何事，与
  `internal/procname` 对 runner 的裁决一致）。
- **mtime 阀门原样保留，且作用于每一条臂**：一个 pid **被回收**（号给了陌生人、
  它从没持有过这把锁）的锁永远不会被 pid 判定释放，mtime 是它唯一的出路；
  这也正是既有 stale-age 契约（`gc_test.go` 的 `os.Create`+`Chtimes` 那一段）所
  依赖的路径。**pid 检查只会让 sweep 更早跑，永远不会更晚。**
- `gclockOwnerLiveness` 是一个包级 seam（生产绑定是 `GCLockOwnerLivenessFor`），
  存在的唯一理由是让「判定依据是 liveness 而不是 mtime」这件事**可以被证伪**：
  红证用 seam 把答案固定成 three-valued，锁文件与 mtime 三条臂完全相同（见下）。

### `SweepHome` / `SweepSandboxes`：详细入口携带 verdict

- `SweepHomeDetailed(home, olderThan) (int, GCLockVerdict, error)` 与
  `SweepSandboxesDetailed(...)` 是报告面；既有的
  `SweepHome(home, olderThan) (int, error)` 与 `SweepSandboxes(root, olderThan)`
  保留为**薄包装**（20+ 既有契约测试测的是 sweep，不是锁证据，改签名等于用真契约
  换签名）。
- `AcquireGCLock` 同样保留为 `AcquireGCLockVerdict` 的薄包装。
- home 不存在时也返回一个 `acquired` verdict 并附句（`"scratch home does not
  exist: nothing to sweep"`），免得零值 verdict 被读成「拿到了锁」。

### `cmd/agent`：把 verdict 变对外句子

- `scratchGCLockLine(line, v)` 给每一条臂的计数行**追加字段**
  `lock=<outcome> owner=pid:<n>`（`scratch.GCLockFields`，唯一渲染器），于是
  `removed=0` 永远和一个判定一起出现。
- 当 outcome 不是 `acquired` 时，再打印**一行独立证据**
  `scratch-gc role=… home=… lock=… owner=… reason="…"`（`scratchGCLockLogFmt`
  + `scratch.GCLockEvidence`）。普通获取不打这一行：每轮都打的一行会把异常轮
  淹掉。
- 常驻 tick 走同一个渲染器，套自己的前缀
  （`[role] [scratch-gc] scratch-gc lock … tree=…`），CLI 与 tick 因此不可能漂移。

### 测试面

- `internal/scratch/gc_lock_test.go`（7 个测试 / 12 子用例）：死锁打破 + 真扫、
  活锁 defer 且点名 pid、四种不可判定形状（空 / 非数字 / 0 / 负）一律 defer 且零删除、
  红证、mtime 阀门仍能打破不可解析的旧锁、打破后重试一次、pid 解析的非测即未知。
- `cmd/agent/cli_scratch_gc_test.go` 新增两个 **E2E**（exec 真二进制）：
  `TestScratchGCCLI_BrokenAbandonedLockIsReportedE2E` 与
  `TestScratchGCCLI_DeferredLiveLockNamesTheHolderE2E` —— 断言的是**用户可见的那一行**
  （`lock=` / `owner=pid:` / `reason=`），不是内部返回值。

## 真跑证据（Rule 31，自造 home，未触碰 operator 真实 scratch home）

evidence 目录：
`/home/user/.gopi/sessions/01a1300b-79da-7224-be8b-b577ac655544/evidence/scratchgc-run/`

**(a) 死 pid 锁 + 一个超龄可收目录**（`.gc.lock` 内容 `4194300`，
`/proc/4194300` 实测不存在；`suite-cmd-agent-run-999001` mtime −7h）：

```
$ GOPI_HOME=…/home GOPI_SETTINGS=…/home/settings.json \
  GOPI_TEST_SCRATCH_HOME=…/scratch ./bin/gopi tool scratch-gc
scratch-gc role=cli home=…/scratch lock=broke-abandoned owner=pid:4194300 \
  reason="lock …/.gc.lock records pid 4194300, which is dead (pid 4194300 has no /proc/4194300 entry: the holder is gone) — abandoned lock broken and the lock retaken"
scratch-gc role=cli home=…/scratch removed=1 stale=6h0m0s disabled=false lock=broke-abandoned owner=pid:4194300
scratch-gc role=cli shot-units-renewed=0
stderr: scratch: collected …/suite-cmd-agent-run-999001 outcome=unregistered (no .holder record) (collectable: …)
```
事后 `ls` 该 home：`suite-cmd-agent-run-999001` **确实消失**（只剩 `queue/`）。

**(b) 活 pid 锁**（`.gc.lock` = 一个 `sleep 300 &` 的 pid，实测 `/proc/<pid>` 在）：

```
scratch-gc role=cli home=…/scratch lock=deferred-live-owner owner=pid:2748190 \
  reason="lock …/.gc.lock is held by live pid 2748190 (pid 2748190 is alive) — this pass deferred, another GC is running"
scratch-gc role=cli home=…/scratch removed=0 stale=6h0m0s disabled=false lock=deferred-live-owner owner=pid:2748190
stderr: (empty)
```
该 pass **零删除**，`suite-cmd-agent-run-999002` 原样存活；锁文件也原样存在。

## 红证（Rule 10.5）

把 `GCLockOwnerLivenessFor` 的调用换成恒返回 `live`（= 退回 mtime-only 判定）：

```
$ perl -0pi -e 's/\tliveness, why := gclockOwnerLiveness\(pid\)/\tliveness, why := GCLockOwnerLive, "red-proof: mtime-only rule"/' internal/scratch/gc_lock.go
$ go test ./internal/scratch/ -run 'TestGCLock_DeadHolderIsBrokenAndTheSweepRuns|TestGCLock_DecisionRestsOnLivenessNotMtimeAlone'
--- FAIL: TestGCLock_DeadHolderIsBrokenAndTheSweepRuns (0.00s)
    gc_lock_test.go:79: a dead holder's lock must not park the sweep: removed=0 rootExists=true
--- FAIL: TestGCLock_DecisionRestsOnLivenessNotMtimeAlone (0.00s)
    --- FAIL: …/holder_is_gone_—_a_fresh_lock_is_still_abandoned (0.00s)
        gc_lock_test.go:225: outcome="deferred-live-owner" want "broke-abandoned"
FAIL
$ cp /tmp/gc_lock.go.bak internal/scratch/gc_lock.go && diff -q … && echo RESTORED
RESTORED
```

## 保守面（一条都没削弱）

| 情形 | 判定 | 结果 |
|---|---|---|
| 活 pid 持有 | `deferred-live-owner` | defer，零删除，句子点名 pid |
| 空 / 非数字 / 0 / 负 锁文件（**不可判定**） | `deferred-unmeasurable` | defer，零删除，`owner=none` |
| `/proc/<pid>` 目录在但 `stat` 读不到（无 procfs / 权限） | `deferred-unmeasurable` | defer，零删除 |
| 上述不可判定 + 锁 mtime > `GCLockStaleAge` | `broke-abandoned` | 打破（**既有阀门不变**） |
| 持有者已死 / 僵尸 | `broke-abandoned` | 打破并重试一次 |

## Rejected alternatives

- **「锁里写 pid + starttime，用 `procname.Match` 判」** —— 更"正确"，但会把
  `internal/scratch` 的锁从 advisory 变成有身份的：pid 回收后 `recycled` 与
  `gone` 会给出不同的句子，而**授权是一样的**（都不打破），且它让一个 10 分钟
  mtime 阀门之外的第二条释放路径变得难以解释。垃圾锁的判据只需要回答「这个号
  现在有没有进程在做 GC」，不需要回答「是不是同一个进程」。
- **defer 时返回 error** —— 派生的「另一个 GC 正在跑」是**成立的判断**，把它报成
  错误会让每一个 watchdog 把正常并发叫成故障。改在输出里可辨（本文件的方案）。
- **给 tick 新加常驻组件/脚本** —— Rule 35：零新依赖。证据搭在既有的 CLI 与
  既有 tick 上。
