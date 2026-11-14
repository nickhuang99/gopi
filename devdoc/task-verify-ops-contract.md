<!-- doc-class: historical -->
# `gopi task verify` 的可运维性：有界默认 + 机器可读结果行 + 退出码契约

日期：2026-09-29　分支：`task/01a0ed373ea77a46a853ab398c7c0f43`

## 现象（两条缺陷，同一个根因）

素材批次（`docs/archive/demo/materials/06-verify/`）实测暴露：

```
$ gopi task verify 01a0ed25-… --timeout 10 ; echo rc=$?
task verify: FAIL: child gopi run: signal: killed (ctx: context deadline exceeded) (stderr tail: …)
rc=1
```

1. **默认无限**：`gopi task verify -h` 原文写 `--timeout caps the child in seconds
   (0 = unlimited)`，而默认值就是 `0`。不带 flag 的自动化/沙盒调用方会被一个
   永不返回的子进程**永久挂住**——批次里必须从**外面**包一层
   `timeout 40 gopi task verify …`（shell 的 exit 124）才能截断。
2. **超时路径不可解析**：整条信息是一句 prose。调用方读不到：
   - 子进程的**退出语义**（signal 名 `killed` ≠ 脚本能比较的 `128+N`）；
   - 到底有没有**写回 verdict**；
   - 有没有**遗留 pending 子行**（rework 是子行）；
   - 也没有**可编程的退出码**区分「已判定」与「未判定」——所有失败都是 `1`，
     和「FAIL 判定」共用一个码。

## 根因（5-why）

- 为什么默认无限？→ `taskVerifyTimeoutDefault = 0`，且解析处
  `if spec := fs.get("timeout"); spec != ""` 用**字符串非空**判断是否给了 flag：
  `--timeout 0` 与「没给 flag」在类型上不可分，于是「默认」实际被写死成 0
  （`cli_tasks.go` 旧行 1126）。
- 为什么超时信息读不出？→ `TaskVerify` 的所有失败路径都 `return "",
  fmt.Errorf(prose)`：`exec.CommandContext` 的 `ctx.Err()` 只被塞进一句话，
  子进程的 `ProcessState`（exit code / signal）**根本没被读**，行状态与子行计数
  也从没被回读。
- 为什么没有退出码契约？→ CLI 只有 `os.Exit(1)` 一条兜底分支。

## 修法

### 1. 有界默认，三通道单处解析（Rule 2）

`internal/config/config.go`：

- `DefaultTaskVerifyTimeoutSec = 30 * 60`（1800s）——**这是一个界，不是便利**；
- `TaskVerifyTimeoutEnv = "GOPI_TASK_VERIFY_TIMEOUT_SEC"`、
  `TaskVerifyTimeoutSettingsKey = "task_verify_timeout_sec"`；
- `ResolveTaskVerifyTimeout(cliVal int, cliSet bool) (int, error)`：
  **显式 CLI > env > settings > 默认 1800**；负数/非法值一律 **error**（不静默回退）；
- `cliSet` 这个第二参数就是缺陷本身的对策：它把「用户敲了 `--timeout 0`」
  与「没有这个 flag」分开，而 `0` 在这里是**有意义的值**（无限）。

`internal/config/settings.go` 加了 catalog 行（`gopi --help-env` 能渲染），
`CONFIG.md` 同 commit 加表 + `file:NNN` 锚点（`config_doc_gate_test.go` 双向检查）。

### 2. 机器可读结果行 + sentinel（`internal/tools/tasks.go`）

新增（全部命名常量，Rule 1）：

- `ErrNoVerdict`：**每次「没判定」的退出都 wrap 它**（超时 / 取消 / 子进程静默
  退出 / 子进程出错）。调用方只需 `errors.Is(err, ErrNoVerdict)` 一个布尔，
  不必再解析 prose。**判定出来的 FAIL 不是这个 sentinel**——FAIL 是判定结果，
  走 `nil` error + 渲染出的 verdict 行。
- 一行固定结构（字段顺序固定、**每个字段恒存在**，未知写 `n/a` 而不是省略）：

```
task verify: result: outcome=no-verdict; task=ID; timeout_sec=N; elapsed_sec=N;
child_exit=128+N|N|n/a; child_signal=N|n/a; cause=timeout|cancelled|child-error|child-exited-without-verdict;
verdict=none|recorded (STATUS); row_status=STATUS; pending_children=N|n/a; stderr_tail=…
```

  实现要点：
  - `taskVerifyChildOutcome(ps)` 读 `syscall.WaitStatus`：signal 死亡报
    `128+N`（SIGKILL→137）+ **裸信号号**（不发散地报名字——名字是本地化，
    数字才是事实）；正常退出报 `ExitCode()`。
  - `taskVerifyRowFacts` 回读行状态、是否已写回 verdict、**pending 子行计数**。
  - 关键坑（自测抓到并修掉）：**死后取证不能复用刚过期的 ctx**。第一版用调用方
    ctx 去读行 → 全部字段渲染成 `n/a`（deadline 就是它刚触发的）。现在取证走
    一个**新 ctx**（`taskVerifyFactReadTimeout = 5s`），并且 `row_status=pending`、
    `pending_children=0` 在超时路径上被实测观察到。
  - `TaskResultLineOf(err)` 导出：CLI 把这一行打到 **stdout**，错误正文留在
    stderr，脚本读任一通道都拿到同样字段。

### 3. 退出码契约（`cmd/agent/cli_tasks.go`）

```
0 = judged        （verified / failed 都是「已判定」，verdict 在行里）
1 = invalid       （调用/引擎失败，历史语义不变）
4 = timeout       （cap 到期，行 UNJUDGED）
5 = no-verdict    （其它未判定退出，行 UNJUDGED）
```

`taskVerifyExitCode(err)` 是纯映射；`4` 与 `5` 的区分让自动化能「加长 cap 重试」
而不是把每次失败当同一类。两条 `-h` 说明同步更新（flag 行标注默认值 1800、两个
override 通道、整张退出码表）。

另外：**`--timeout 0` 会在 stderr 上被显式承认**（"the child is UNBOUNDED …"），
`run dir:` 行在**子进程启动前**打印（否则 ground 被回收后这个事实就不可知了）。

## 反例与回归测试

| 测试 | 断言 |
|---|---|
| `internal/tools/taskverify_noverdict_test.go` `TestTaskVerifyTimeoutNoVerdictContract` | `--timeout 1` 跑一个必然跑不完的假 deliverable（沿用 `taskverify_ctx_test.go` 的 TestMain 子进程模拟器）：① `errors.Is(err, ErrNoVerdict)`；② 恰好一条结构行、字段齐全、`cause=timeout`、`timeout_sec=1`、`verdict=none`；③ **行状态仍是 pending**（RED 前置：改动前无法断言）；④ **pending 子行 = 0** |
| 同文件 `TestTaskVerifyCapArmsAfterGround` | **cap 起算位置的顺序门**（见下节「第二个根因」）：源码顺序必须是 ground → cap → 用 capped ctx 构 child；并带**反向 red proof**——把修复前的形状喂给同一个 checker，必须被判 fail，否则这道门什么都没守 |
| 同文件 `TestTaskVerifyJudgedVerdictIsNotANoVerdictExit` | 反向钉住：`TaskResultLineOf(nil)`、invalid 错误都不带结果行；生产构造器 wrap sentinel 且回出同一行 |
| `cmd/agent/cli_tasks_verify_test.go` `TestTaskVerifyExitCodeContract` | 0/1/4/5 纯映射，且 4≠5、1 与二者不撞码 |
| 同文件 `TestTaskVerifyDefaultIsBounded` | CLI 默认常量 = 解析器默认 = 1800（>0），且 `-h` 的 verify 块**印出 1800 与 `GOPI_TASK_VERIFY_TIMEOUT_SEC`** |
| `internal/config/task_verify_timeout_test.go`（6 个） | 默认有界 / 显式 0 = 无限 / CLI>env>settings 优先级 / 负数与非法值报错 |

## 第二个根因：cap 在「进入函数」时就开始跑（2026-09-29 实测补修）

第一个根因修完后，**在真实 ledger 上跑 verb**（Rule 31）暴露了第二个、独立的缺陷：

```
$ gopi task verify <id> --timeout 1     # 修复前
task verify: result: outcome=no-verdict; timeout_sec=1; elapsed_sec=1;
child_exit=n/a; child_signal=n/a; cause=timeout; row_status=pending; …   exit 4
```

`n/a` 出现在**它本来就是为了描述的那次 kill 上**。插桩实测（打印三个时刻）：

```
DBGGROUND secs=1.10 preCtxErr=context deadline exceeded   ← ground 准备耗时 1.10s
DBGRUN    pre=context deadline exceeded err=context.deadlineExceededError ps_nil=true
```

**cap 是在函数入口创建的**，而 ground 准备（解析/临时签出那个私有 detached checkout）
单独就要 ~1.1s。于是 `--timeout 1` 在**子进程还不存在**时就到期了：没有进程状态可读，
`child_exit` 只能写 `n/a`——结果行描述了一次没发生过的 kill。同一行、同一个二进制：

```
--timeout 1 → child_exit=n/a;  child_signal=n/a   （child 从未启动）
--timeout 6 → child_exit=137; child_signal=9      （SIGKILL，与 -h 的承诺一致）
```

入口注释早就声称 cap「bounds the child」，而代码把它武装得更早。**修法**：
ground 先解析 → 再武装 cap → 然后**用这个 capped ctx 构造 child**
（`exec.CommandContext`，deadline 到点即 `Process.Kill()`，关掉的是子进程的**生命周期**，
而不只是它的等待）；`startedAt` 与 cap 同刻打点，`elapsed_sec` 因此度量 cap 所描述的那段运行。

**为什么单测看不见**：模拟器子进程**瞬间**启动，ground 成本为零。这正是「跑真的 verb」
（Rule 31）的意义——缺陷是执行时发现的，回归门就是这次教训的产物。门做成
**源码顺序断言**而非计时测试：计时测试会和宿主的 ground 耗时赛跑，而三个锚点的**先后**
才是那个属性本身。

## 未做（诚实）

- **超时子进程的 `child_exit`/`child_signal` 已修好并双重实测**：初版为 `n/a` 的原因有**两个**，
  且互相独立——①（已修）取了 `cmd.ProcessState`，`Run()` 走 error 分支时它是 nil，
  改取 `*exec.ExitError.ProcessState`；②（本次已修）cap 起算得太早，child 根本没启动。
  只修 ① 仍会得到 `n/a`：**这就是为什么单测绿了而真实 verb 仍然错**。
  实测 `child_exit=137; child_signal=9`（SIGKILL），并有断言钉住（`n/a` 直接判 fail）。
- `verdict=recorded (STATUS)` 分支（子进程在被杀前已写回 verdict 的竞态）有实现，
  但未构造出稳定复现的测试（需要精确控制写回与杀进程的先后）。
- `cmd/agent` 包里三个 `TestDocCheck*` 在本分支上 FAIL，但**与本改动无关**：
  在 `master`（`0b453c18`）上同样 FAIL，根因是 `5c7d5f5f`
  （classify.py 改为用 `git ls-files` 枚举语料）后 fixture 目录不是 git 仓库，
  已在 master worktree 上复现确认。
- cap 现在**只**约束验证运行；ground 准备（~1s）不受它约束。这是有意的语义选择
  （flag 承诺的是「cap 子进程」），但意味着一个极小的 `--timeout` 不再是「整个 verb 的墙钟上界」
  ——需要后者的调用方仍应在外层用 `timeout(1)` 包。

## 证据

见 `tasks/` 报告中的 `## Verification` 段（verbatim 命令 + 输出 + exit code）。
