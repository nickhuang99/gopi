<!-- doc-class: historical -->
# Task Report: 空 `-test.run` 选择器（自噬链）—— 修复核实、全仓审计、回归门禁

Task: `01a12d4dedd278d0964244380f714979`
Branch: `task/01a12d4dedd278d0964244380f714979`
Worktree: `/home/user/workspace/gopi-01a12d4dedd278d0964244380f714979`

## Completed

### ① cmd/agent 侧：`chatSlotDeadPid` 夹具已不在任何 ref 上（经实测核实，未改代码）

**结论：本条无需修复，因为该夹具不存在。** 证据是两条独立的检索：

```
$ grep -rn 'chatSlotDeadPid' .                      → 0 命中
$ git log --all --oneline -S 'chatSlotDeadPid'      → 0 命中
$ git log --all --oneline -S 'ReclaimsOnlyAnEntryWhoseSubjectIsGone' → 0 命中
```

`chatSlotDeadPid` 与 `TestChatSessionSlotReclaimsOnlyAnEntryWhoseSubjectIsGone`
**从未出现在本仓的任何 ref、任何历史提交里** —— 它们是一次未提交的尝试，其产物
从未落库。master 的 `cmd/agent/chat_session_slots_test.go` 现状是三条纯内存测试
（`TestChatSessionSlotRegistrationIsIdempotentAndBounded` /
`...ValueIsTheSurfaceIdentityShape` / `...NamesMatchTheBound`），文件里**没有任何
`exec.` 调用**，因此不派生任何探针进程。

「跑该包相关测试能正常结束、无嵌套 probe 进程」已实测（见 Verification 的 `Ran:` 第 ② 条）。

### ② 全仓审计：55 个「自身测试二进制」夹具逐条判定

审计由**门禁自己产出的机器清单**驱动（不是人工阅读）：
`internal/testutil/fixture_selector_gate_test.go` 的 arm B 会为每个 spawn 站点打印
一行，含 file:line、函数名、解析出的选择器、以及四个判定位。完整 55 行见
`## 逐条判定` 表（机器生成）。判定族：

| 族 | 数量 | 判据 |
|---|---|---|
| **C 选择器已解析且非空** | 47 | 选择器字面量／包级常量／`t.Name()` 派生／argv builder 体内的字面量，解析结果非空 |
| **A 无 `-test.run` 元素，子进程体由 TestMain 的 env 标记选择** | 6 | 全部在 `internal/signalrec`：`TestMain` 在 `m.Run()` **之前**按 `os.Getenv(srChildEnv)` 等分支直接跑 victim 体并 return —— 子进程不进入 suite，因此不存在选择器为空的问题 |
| **D 无法解析（须登记豁免）** | 2 | 见下 |

**D 的两条**：

1. `cmd/agent/cli_checkpoint_test.go:83` `testHelperCommand` —— 选择器是函数**形参**
   `name`（`exec.Command(exe, "-test.run="+name)`）。**定论：非缺陷，安全。** 依据：
   该文件全部 4 个调用点都传**字面量且非空**的测试名
   （`TestResolveRawLog_MissingExits` 等），没有任何调用点传空值；且 `+` 拼接意味着
   元素本身不是「值为空的 `-test.run` 元素」（arm A 已独立判过它不是空选择器）。
   已登记进 `fixtureSelectorUnresolvedExempt`（键 = 文件|函数，理由写明判据）。
2. `internal/testutil/daemonlifetime_test.go:213` `runLifetimeHolder` —— 同样无法解析，
   但**同包有 TestMain env 派发**（门禁日志里 `envDispatched=true`），实测 PASS、
   未被报错。

**唯一仍在 shell 里以「拼接」形态出现的选择器**就是上面第 1 条；**没有任何站点的
选择器为空或缺失**。

### ③ 机器门禁：`internal/testutil/fixture_selector_gate_test.go`

两条**分层**的臂（硬臂不依赖软臂）：

- **arm A（raw text，不做任何解析）**：`-test.run` 元素值为空 → **直接 FAIL**。
  识别两种拼写：内联 `"-test.run="`（就是本行的事故拼写），以及裸标志
  `"-test.run", ""`（或后面没有元素 —— Go 的 flag 解析此时取空值）。判定受
  **元素级**约束，因此不误伤 `"-test.run=" + name`（`+` 表示拼接仍在继续）。
- **arm B（解析棘轮）**：每个自身测试二进制的 spawn 必须把选择器解析成字面量
  （字面量／包级常量／`t.Name()` 派生／本包 helper 的 argv builder 体内）；解析不出
  的站点逐条报错，除非登记在 `fixtureSelectorUnresolvedExempt`（数据表，可复审）。
  站点清单每次运行**全量打日志**。

配套的两个 red-proof 与一个地板：`TestFixtureSelectorGateFindsTheEmptySelector`（字符串级
双向）、`TestFixtureSelectorScanFindsTheEmptySelectorInAFixture`（**真实目录**级双向）、
以及 `fixtureSelectorMinSites=25` 地板（扫描没读到仓库 → FAIL，而不是"绿"）。

### ④ 顺带修掉的一个真 bug：`fixtureFuncDeclRe` 把函数名读错

arm B 的第一版把**每个**站点都报成「函数名 = 一个字母」（`d` / `r` / `n`）。根因不在
新门禁，而在被复用的既有 helper：`internal/testutil/fixture_spawn_inventory_test.go` 的
`fixtureFuncDeclRe` 用**可选字符跑**匹配接收者，于是

```
old: "func reexecHolderCmd(t *testing.T) *exec.Cmd {" → 捕获 "d"   （吃掉了函数名的一部分）
old: "func (p *pkg) Method(a int) error {"            → 不匹配方法
new: 分别得到 "reexecHolderCmd" / "Method"
```

而**捕获到的名字是豁免表的键**（`fixtureSpawnExempt[file+"|"+fn]`）—— 名字错了，豁免就会
悄悄落在不存在的站点上、或让真泄漏站点逃过（当前 `fixtureSpawnExempt` 为空表故未爆）。
已改为显式交替 `^func (?:\(\w+ \*?\w+\) )?(\w+)\(`，并**随修复补上回归测试**
`TestFixtureFuncDeclReReadsPlainAndMethodNames`（Rule 5：每个修复带最小复现测试）。

### ⑤ 验证过程中暴露并修掉的第二个问题：本门禁的**测试数据**被兄弟门禁当成 spawn 站点

跑既有门禁 `TestFixtureSpawnInventoryIsAuditable` 时它报：

```
internal/testutil/fixture_selector_gate_test.go:707 fixtureSelectorElementClean
  starts a child that can outlive this test binary with nothing bounding it
```

**根因**：该门禁按**文本**扫描自身测试二进制的 spawn（`fixtureSpawnSelfRe` =
`exec\.Command\(\s*(os\.Args\[0\]|os\.Executable\(\)|exe\b|exe,)`）。我的 arm A 红证
把缺陷的**字面拼写**当作**测试数据**摆在 `map[string]string` 里
（`exec.Command(os.Args[0], "-test.run=", …)`），于是**数据被当成了站点**；
它的「函数名」还落在我已删除的 `fixtureSelectorElementClean` 上（残留行号 707）。

**修法**（两层，都不削弱任何一侧）：arm A 的判据是**元素**，不是整行调用 —— 所以
红证数据改成只写元素（`"-test.run=", "TestThisNameSelectsNoTestInTheProbe"`），
合成夹具源文件同理（`return []string{"-test.run=", …}`）；注释里不再出现 spawn 字面量。
**验证**：`inventory: 56 spawn sites, none with the leak's shape` +
`TestFixtureSpawnInventoryIsAuditable` PASS，**且 arm A 的红证依然双向可证**（见 Verification Ran ③）。

这条本身就是 Rule 41 的样本：一个「我的数据被别人的扫描器读错」的现象，先取证再改，
并且**没有**去动那个门禁的判据（它的判据是对的）。

### ⑥ 报告与审计数据

- 本报告：`devdoc/fixture-selector-autophagy-gate.md`
- 审计清单（机器生成，55 行）：本报告 `## 逐条判定` 表；原始门禁日志留在
  `~/.gopi/sessions/01a12d41-c711-7eec-af95-3fb0cc982c64/artifacts/selector-gate-inventory.txt`，
  并在本 run 的 session 目录留有副本
  `~/.gopi/sessions/01a12d4d-edd2-78d0-9642-44380f714979/artifacts/selector-gate-inventory.txt`
  （Rule 41：证据与它所描述的现象一起存放）。

## Files

| 文件 | 变更 |
|---|---|
| `internal/testutil/fixture_selector_gate_test.go` | **新增**（865 行）：arm A 裸文本门禁 + arm B 选择器解析 + D 类豁免表 + 字符串级/真实目录级双向红证 + `min-sites` 地板 |
| `internal/testutil/fixture_spawn_inventory_test.go` | 修正：本门禁测试数据不再写出兄弟门禁会认成 spawn 站点的完整调用前缀；顺带修 `fixtureFuncDeclRe`（函数名捕获 bug，含方法形） |
| `devdoc/fixture-selector-autophagy-gate.md` | **新增**：本报告（含 55 站点逐条判定表） |
| `devdoc/DOC-INDEX.tsv` | 新增本报告的一行（Rule 36，随文件同一次提交落地） |

提交：`025d83378`（分支 `task/01a12d4dedd278d0964244380f714979`，worktree
`/home/user/workspace/gopi-01a12d4dedd278d0964244380f714979`）。

## Not Done

- **未修 `internal/testutil/daemonlifetime_test.go:213` 的不可解析选择器**：它是
  `envDispatched=true`（同包 TestMain 派发）且实测 PASS，把它登记进豁免表只是噪声，
  故只做记录。**若日后该包去掉 TestMain 派发，arm B 会立刻报它** —— 这正是棘轮的作用。
- **门禁只覆盖三个源码根**（`internal/`、`cmd/`、`testsuite/`，复用既有 inventory 的
  常量）。仓外的测试树不在扫描范围。
- **arm B 是正则+索引的静态分析，不是 Go 类型检查**：选择器经由结构体字段、
  接口调用或跨包函数构造的站点会落入 D（无法解析）→ 须登记豁免。这是**刻意的**：
  宁可报出来让人判，也不静默当安全。
- **未使用 `go/parser`**：为避免在测试里引入一整套解析基础设施（Rule 35 零依赖取向），
  选择复用既有的文本扫描机制并把它做准；代价是上面那条静态分析上限。

## Verification

- Build: `go build ./...` → exit 0（无输出）
- Vet: `go vet ./internal/testutil/` → exit 0（无输出）
- Tests（分支范围 smoke）：
  `timeout 300 go test ./internal/testutil/ -run 'TestFixtureSelector|TestFixtureFuncDeclRe|TestFixtureSpawn' -count=1 -v`
  → **7/7 PASS**（`ok github.com/nick/gopi/internal/testutil 3.479s`）：
  `TestFixtureSelectorNeverRunsTheWholeSuite`（2.87s）、
  `...GateFindsTheEmptySelector`、`...ScanFindsTheEmptySelectorInAFixture`、
  `...ResolutionTellsALiteralFromAnUnknown`、`TestFixtureSpawnInventoryIsAuditable`
  （日志 `inventory: 56 spawn sites, none with the leak's shape`）、
  `TestFixtureSpawnClassifierFindsTheLeakItself`、
  `TestFixtureFuncDeclReReadsPlainAndMethodNames`
- Tests（声明入口）：`make test-budget PKGS='./internal/testutil ./cmd/agent'`（2026-10-12T08:39:02+08:00 host=hosta）
  → `packages: 2 finished, 0 with a failure`，`executed: 4545 test(s)`，`elapsed=922s`，
  `exit=0 outcome=pass`，stream `run-1791765542-2438242.json`。
- **Ran:（Rule 31 —— 改了的面真跑，并观察它【结束】）**
  1. **空选择器语义的实测**（最小样本，独立 scratch 树 `/home/user/workspace/gopi-selprobe-*`，
     设备 66305 ≠ `/` 的 66308，Rule 22）：
     ```
     $ go test -c -o probe.test .          # 两个 trivial 测试 TestA / TestB
     $ ./probe.test -test.run= -test.v     → --- PASS: TestA / --- PASS: TestB   ← 空选择器跑了【全套】
     $ ./probe.test "-test.run=" -test.v   → 2 个 PASS                            ← 同样跑全套
     $ ./probe.test "-test.run=^$" -test.v → PASS（没有任何测试运行）
     $ ./probe.test -test.run "" -test.v   → 2 个 PASS                            ← 分离空参数也跑全套
     ```
     这四条就是门禁存在的理由，也是 arm A 判定「`^$` 才是 no-test 拼写」的依据。
  2. **cmd/agent slot 夹具真跑并【正常结束】**（deliverable ①）：
     ```
     $ go test ./cmd/agent/ -run 'TestChatSessionSlot|TestResolveRawLog' -count=1 -v
     --- PASS: TestChatSessionSlotRegistrationIsIdempotentAndBounded (0.01s)
     --- PASS: TestChatSessionSlotValueIsTheSurfaceIdentityShape (0.00s)
     --- PASS: TestChatSessionSlotNamesMatchTheBound (0.00s)
     --- PASS: TestResolveRawLog_MissingExits (1.10s)
     ok  	github.com/nick/gopi/cmd/agent	2.309s   exit=0
     elapsed_sec=4
     ```
     反自噬的正面证据（跑完后对证据文件计数）：
     `grep -c '=== RUN   TestChatSessionSlotRegistrationIsIdempotentAndBounded'` → **1**
     （自噬链会是 26+）；`suite child sweep: live child processes: 0`；无残留进程。
     注：`ps` 中同时存在的 `agent.test` 属于**其他会话**的 go test，不是本 run 派生的。
  3. **门禁红证（真实树注入）**：写入 `cmd/agent/zz_selector_red_proof_test.go`
     （`exec.Command(os.Args[0], "-test.run=", "TestThisNameSelectsNoTestInTheProbe")`）→
     `go test ./internal/testutil/ -run TestFixtureSelectorNeverRunsTheWholeSuite` →
     `--- FAIL`，报文**指名该 file:line**（arm A 行 + arm B 行各一条）；删除该文件 → 立刻
     `ok`。红→绿在同一命令内闭合。
  4. **门禁绿证（真实树）**：`selector gate: 55 spawn sites, 49 carrying a -test.run element,
     1 unresolved (recorded exemptions)` → PASS；兄弟门禁
     `TestFixtureSpawnInventoryIsAuditable` → PASS（`inventory: 56 spawn sites, none with
     the leak's shape`），证明我给它的修法不是屏蔽而是不误报。
- **Result**：`go build ./...` 通过；门禁红/绿双向可证；cmd/agent slot 测试 4 秒内正常结束、
  无嵌套 probe 族、无残留；审计 55 条逐条判定；两个既有门禁（spawn inventory、selector）
  全绿。

## Evidence (self-filed)

- 空选择器跑全套：`./probe.test -test.run= -test.v` → `--- PASS: TestA` + `--- PASS: TestB`
  （2 个测试都跑了）；`./probe.test "-test.run=^$" -test.v` → 只 `PASS`，0 个测试。
- `chatSlotDeadPid` 不存在：`git log --all --oneline -S 'chatSlotDeadPid'` → 空输出，
  0 命中（`grep -rn` 同样 0 命中）。
- slot 测试真的结束：`elapsed_sec=4`，`ok github.com/nick/gopi/cmd/agent 2.309s`，`exit=0`；
  自噬计数 `grep -c '=== RUN   TestChatSessionSlotRegistrationIsIdempotentAndBounded'` → `1`。
- 门禁红证：注入 `cmd/agent/zz_selector_red_proof_test.go` 后
  `--- FAIL: TestFixtureSelectorNeverRunsTheWholeSuite`，报
  `cmd/agent/zz_selector_red_proof_test.go:9 carries an EMPTY -test.run value`；删除后 `ok`。
- 门禁绿证：`selector gate: 55 spawn sites, 49 carrying a -test.run element, 1 unresolved`。
- 函数名 bug（Rule 41 取证）：`old=[func reexecHolderCmd( d] new=[func reexecHolderCmd( reexecHolderCmd]`
  —— 同一行同一正则，修复前捕获 `d`、修复后捕获全名（另附方法形：old 不匹配 / new `Method`）。
- 审计数据落盘：门禁日志 `~/.gopi/sessions/01a12d41-c711-7eec-af95-3fb0cc982c64/artifacts/
  selector-gate-inventory.txt`（55 行站点清单），逐条判定转录为本报告的表格。

## 逐条判定（机器生成 —— 55 个自身测试二进制 spawn 站点）

生成方式：`go test ./internal/testutil/ -run TestFixtureSelectorNeverRunsTheWholeSuite -count=1 -v`
的站点日志（arm B 每站点一行），不做人工转录。字段：`站点 — 解析出的选择器`、`spells`
（argv 是否带 `-test.run` 元素）、`resolved`（是否解析成字面量）、`env`（同包 TestMain 是否
env 派发 child 体）。

| 站点 | 解析出的选择器 | spells | resolved | env 派发 |
|---|---|---|---|---|
| `internal/daemoncwd/daemoncwd_env_holder_test.go:80 envHolderChild` | `-test.run=TestHelperEnvHolder` | true | true | false |
| `internal/daemoncwd/daemoncwd_env_holder_test.go:229 TestIsTestScratchHolderGatesOnTheDaemonShape` | `-test.run=TestHelperEnvHolder` | true | true | false |
| `internal/signalrec/signalrec_sessionlog_test.go:112 TestSignalRecVerdictEventReachesTheSessionLogFromTheSetSessionDirSeam` | `` | false | false | true |
| `internal/signalrec/signalrec_test.go:886 TestSignalRecOffIsHonored` | `` | false | false | true |
| `internal/signalrec/signalrec_test.go:932 TestSignalRecChildVictimRecordsEndToEnd` | `` | false | false | true |
| `internal/signalrec/signalrec_test.go:1168 TestSignalRecForwardingPreservesDefaultDeath` | `` | false | false | true |
| `internal/signalrec/signalrec_test.go:1302 TestSignalRecClaimedHandlerIsNotReRaised` | `` | false | false | true |
| `internal/signalrec/signalrec_unsourced_test.go:61 srSignalVictimAndReadVerdict` | `` | false | false | true |
| `internal/testutil/daemonlifetime_test.go:213 runLifetimeHolder` | `` | true | false | true |
| `internal/testutil/fixturelifetime_test.go:142 runFixtureHolder` | `^$` | true | true | true |
| `internal/testutil/fixturelifetime_test.go:289 TestFixtureChildIsReapedByItsOwnGroup` | `^$` | true | true | true |
| `internal/testutil/fixturelifetime_test.go:328 TestBindFixtureChildRefusesWithoutAnArmedSuite` | `^$` | true | true | true |
| `internal/testutil/fixturelifetime_test.go:392 startFixtureHolder` | `^$` | true | true | true |
| `internal/testutil/gopibin_pin_test.go:132 TestGopiBinPublishCaseSkipsWhenACasePinAlreadyResolved` | `^TestGopiBinPublishesTheTreeBuildToChildren$` | true | true | true |
| `internal/testutil/gopibin_pin_test.go:163 TestGopiBinFailsLoudlyOnAnUnusablePin` | `^TestGopiBinFailsLoudlyOnAnUnusablePin$` | true | true | true |
| `internal/testutil/ledgerpin_test.go:81 TestSuiteLedgerPinRawSuiteFails` | `^TestSuiteLedgerAnchorProbe$` | true | true | true |
| `internal/testutil/ledgerpin_test.go:114 TestSuiteLedgerPinFixedShapePasses` | `^TestSuiteLedgerAnchorProbe$` | true | true | true |
| `internal/testutil/mockproc_test.go:69 reexecHolderCmd` | `-test.run=^TestHelperMockProcessHolder$` | true | true | true |
| `internal/testutil/mockproc_test.go:334 TestStartMockProcessCmdRefusesAPresetPgid` | `^TestHelperMockProcessPresetPgidProbe$` | true | true | true |
| `internal/testutil/mockproc_test.go:434 TestMockProcessReapedOnPanicPath` | `^TestHelperMockProcessSpawnThenPanic$` | true | true | true |
| `internal/testutil/mockproc_test.go:504 TestAllowStartFailureSkipsVisiblyAndRunsTheHook` | `^TestHelperMockProcessOptionalSkipProbe$` | true | true | true |
| `internal/testutil/scratchroot_entrypoint_test.go:83 TestSuitePinFallbackToBootDeviceFails` | `^TestScratchReexecTrivial$` | true | true | true |
| `internal/testutil/subreaper_chain_test.go:181 newSubreaperLayer` | `<runtime test name>` | true | true | true |
| `internal/testutil/suiteenv_test.go:65 TestSuiteNotifyEnvRawSuiteFails` | `^TestSuiteNotifyEnvAnchor$` | true | true | true |
| `internal/tools/bash_test.go:625 TestGOPI_ALLOW_DANGEROUS_EnvDisablesGuard` | `^TestGOPI_ALLOW_DANGEROUS_EnvDisablesGuard$` | true | true | false |
| `internal/tools/guest_row_guard_test.go:247 runGuestRowChild` | `^TestGuestRowGuardHelper$` | true | true | false |
| `internal/tools/live_claim_worktree_veto_test.go:685 TestEmptyCmdlineProcTreeFailsLoudlyNamingTheFixture` | `<runtime test name>` | true | true | false |
| `internal/tools/memory_sync_txn_test.go:79 TestSyncImportTwoProcessConcurrent` | `<runtime test name>` | true | true | false |
| `internal/tools/task_finish_cleanup_holder_shared_session_test.go:94 sharedSessionSpawnOwnedHolder` | `-test.run=TestHelperSharedSessionCwdHolder` | true | true | false |
| `internal/tools/task_finish_cleanup_holder_shared_session_test.go:341 TestHelperSharedSessionLauncher` | `-test.run=TestHelperSharedSessionCwdHolder` | true | true | false |
| `internal/tools/task_finish_cleanup_holder_shared_session_test.go:546 newSharedSessionOrphanHolderMode` | `-test.run=TestHelperSharedSessionLauncher` | true | true | false |
| `internal/tools/task_finish_cleanup_orphan_test.go:200 deadPidOnThisHost` | `-test.run=TestHelperNeverRuns` | true | true | false |
| `internal/tools/task_finish_cleanup_run_identity_test.go:46 holderCommand` | `-test.run=TestHelperOrphanCwdHolder` | true | true | false |
| `internal/tools/task_finish_cleanup_test.go:1078 daemonCwdHolder` | `TestHelperDaemonWorktreeHolder` | true | true | false |
| `internal/tools/task_finish_cleanup_test.go:1175 liveCwdHolder` | `-test.run=TestHelperWorktreeCwdHolder` | true | true | false |
| `internal/tools/task_reclaim_own_ground_test.go:147 runReclaimOwnGroundChild` | `^TestReclaimOwnGroundHelper$` | true | true | false |
| `internal/tools/toolpath_test.go:158 toolPathRunProbe` | `^TestToolPathProbeChild$` | true | true | false |
| `internal/tools/worktree_self_checkout_refusal_test.go:292 runSelfCheckoutChild` | `^TestSelfCheckoutSweepHelper$` | true | true | false |
| `internal/tools/worktree_self_position_test.go:154 TestSelfPositionFollowsACwdChange` | `^TestSelfPositionChild$` | true | true | false |
| `cmd/agent/cli_capture_test.go:552 captureProbeChild` | `^TestCaptureVerbFailureArmsExitNonZero$` | true | true | false |
| `cmd/agent/cli_checkpoint_test.go:83 testHelperCommand` | `` | true | false | false |
| `cmd/agent/cli_serve_test.go:96 TestAcquireServeLock_RefusesAfterUnlinkUnderneath` | `TestServeLockHolderHelperProcess` | true | true | false |
| `cmd/agent/cli_tasks_recycle_e2e_test.go:69 startRecycleHolder` | `-test.run=TestHelperRecycleCwdHolder` | true | true | false |
| `cmd/agent/cli_voice_review_test.go:530 runVoiceChild` | `^$--` | true | true | false |
| `cmd/agent/cwd_release_rig_test.go:141 startCwdReleaseHolder` | `-test.run=TestHelperCwdReleaseHolder` | true | true | false |
| `cmd/agent/merger_work_lock_e2e_test.go:143 mwlHolder` | `TestServeLockHolderHelperProcess` | true | true | false |
| `cmd/agent/serve_lock_diagnosis_liveness_test.go:132 diagnosisOwnerPids` | `TestServeLockDiagnosisHelperProcess` | true | true | false |
| `cmd/agent/serve_lock_diagnosis_liveness_test.go:427 TestServeLockLivenessNoteReportsADeadHolder` | `TestServeLockDiagnosisHelperProcess` | true | true | false |
| `cmd/agent/serve_lock_diagnosis_liveness_test.go:484 TestServeLockDiagnosisHelperProcess` | `TestServeLockDiagnosisHelperProcess` | true | true | false |
| `cmd/agent/serve_lock_refusal_diagnosis_test.go:52 deadReapedPid` | `TestServeLockRefusalDiagHelperProcess` | true | true | false |
| `cmd/agent/serve_lock_refusal_diagnosis_test.go:345 TestServeLockRefusalReportsTheRefusedInodeAcrossProcesses` | `TestServeLockRefusalDiagCrossProcessHelperProcess` | true | true | false |
| `cmd/agent/serve_lock_replacement_test.go:248 TestServeLockFlockOutlivesItsInheritedCopy` | `TestServeLockRaceHelperProcess` | true | true | false |
| `cmd/agent/serve_lock_replacement_test.go:331 TestServeLockRaceHelperProcess` | `TestServeLockRaceHelperProcess` | true | true | false |
| `cmd/agent/toolpath_test.go:120 toolPathRunProbe` | `^TestToolPathProbeChild$` | true | true | false |
| `cmd/agent/worker_lock_guard_test.go:221 workerGuardStartDaemon` | `TestWorkerDaemonGuardHelperProcess` | true | true | false |

