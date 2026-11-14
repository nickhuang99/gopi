<!-- doc-class: normative -->

# `running tests:` 点名的测试不是肇事者 —— 读法与实测

**一句话**：`go test` 超时打印的那一行 `running tests:` 列出的是「超时那一刻仍在跑」的
测试集合，它的年龄被**四舍五入到整秒**、而且是**打印那一刻的年龄**。所以它既不是
「谁吃的墙钟」，也不足以证明「它刚开始跑，所以清白」。仓库里的正确读法是
`scripts/test-budget.sh` 的归因块（`scripts/lib/stream-attribution.sh`），它从
stream 的两个时间戳现算每个未结束测试的**在飞时长**。

## 1. Go 工具链那一行到底说什么（源码取证）

```bash
$ grep -n "func runningList" -A9 /usr/local/go/src/testing/testing.go
2490:func runningList() []string {
2491-	var list []string
2492-	running.Range(func(k, v any) bool {
2493-		list = append(list, fmt.Sprintf("%s (%v)", k.(string), highPrecisionTimeSince(v.(highPrecisionTime)).Round(time.Second)))
2494-		return true
2495-	})
2496-	slices.Sort(list)
2497-	return list
2498-}
```

三件事由此确定，不是推断：

1. 它读的是 `running` 这个 sync.Map —— **只有此刻在里面登记的测试**；
2. 年龄是 `Round(time.Second)` —— **整秒**，`0.4s` 和 `0.49s` 都印成 `(0s)`；
3. 年龄的基准是**打印那一刻**，不是包的起点。

⇒ 一个刚启动 0.2s 的测试和一个已经卡了 179s 的测试，在那一行里可能都以
`(0s)`/`(179s)` 的面目出现，而**包的预算**（例如 180s）不在这行里。读者要做的减法
（「它在飞多久 / 包跑了多久」）**这一行没有提供分子和分母的任何一方**。

## 2. 实测：被点名的那条，单独跑是好的（两次独立复现）

两次全量 `cmd/agent` 被预算截断（`GOPI_TEST_TIMEOUT` 分别为 180s 与 120s），
`running tests:` 各点名一条，两条在实跑里都不是耗时源：

| 运行 | 预算 | `running tests:` 点名的 | 它在飞多久 | 单独跑的结果 |
|---|---|---|---|---|
| run1 `run-1794113925-2882517` | 180s | `TestTaskRunChildReceivesSummaryAndPayloadE2E` | **1.8s**（占预算 1.0%） | `--- PASS: TestTaskRunChildReceivesSummaryAndPayloadE2E (1.33s)`，`ok  github.com/nick/gopi/cmd/agent  4.801s` |
| run2 `run-1794114463-3070648` | 120s | `TestRunPathTaskChildKeepsTheFraming` | **0.2s**（占预算 0.2%） | `--- PASS: TestRunPathTaskChildKeepsTheFraming (0.77s)`，`ok  github.com/nick/gopi/cmd/agent  5.031s` |

单独跑的命令（`GOPI_TEST_BIN` 必须给，否则这两条会 `SKIP`，见 §5）：

```bash
cd /home/user/workspace/01a1b971-5258-7433-98da-ffd434942074
B=/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1794114067-2920178-tested-bin/gopi
GOPI_TEST_BIN=$B go test -count=1 -vet=off -timeout 300s -v \
  -run '^TestTaskRunChildReceivesSummaryAndPayloadE2E$' ./cmd/agent
# --- PASS: TestTaskRunChildReceivesSummaryAndPayloadE2E (1.33s)
# ok  	github.com/nick/gopi/cmd/agent	4.801s
```

**这两条都不是性能问题**：全量跑里没有任何一条测试能吃下预算。run1 的 1639 条
顶层测试 `Elapsed` 合计 **178.0s**（预算 180s），**最大的一条 5.22s = 合计的 2.9%**，
一半的合计由 **42 条（2.6%）** 贡献。⇒ 墙钟不是某一条测试吃的，是「几百条各自
几毫秒的测试依次跑完」的累积，加上包级固定开销（进程启动、fixture、suite 收尾）。
追「肇事的那一条测试」这个问法本身就是错的。

## 3. 正确的读法：一条命令 + 它的原始输出

```bash
cd /home/user/workspace/01a1b971-5258-7433-98da-ffd434942074
GOPI_TEST_TIMEOUT=120s scripts/test-budget.sh ./cmd/agent
```

被截断时，块里这两行就是答案（run2 的原始输出）：

```
  github.com/nick/gopi/cmd/agent: ΣElapsed (LOWER BOUND) = 119.7s over 1204 finished arm(s)
  ^ 1 arm(s) started and never finished: TestRunPathTaskChildKeepsTheFraming
    TestRunPathTaskChildKeepsTheFraming: in flight 0.2s when the cap expired (its run event 13:09:49.103, the cap line 13:09:49.282)
```

**怎么读**：

- `ΣElapsed (LOWER BOUND)` —— 已收口测试的 `Elapsed` 之和，是**下界**（未收口的不在内）；
- `^ N arm(s) started and never finished` —— **每一个**被截断时仍在飞的测试，不只一条；
- `in flight X.Xs when the cap expired (its run event T1, the cap line T2)` ——
  打印的是 **T2 − T1**，毫秒精度，两个时间戳都印出来，读者可以从 stream 自己再算一遍。
  T2 是 go 自己那行 `running tests:` 的时刻（stream 里唯一说得出「预算何时用完」的时间戳）。
- stream 里没有 `running tests:` 时间戳时（例如 panic 文本被截断），这一行改印
  `in flight for an UNMEASURED time` —— **缺测量就报缺测量，绝不编一个数**。

这条 `in flight` 是从 run1/run2 之前**不存在**的：改之前块里只有名字，没有时长，
而名字恰恰是「读者已经会看错」的那一半。

## 4. 机制（`scripts/lib/stream-attribution.sh`）

- 读 `go test -json` 流：`Action:"run"` 记每个顶层测试的 `Time`；`pass/fail/skip`
  记 `Elapsed`（子测试 `TestX/arm` 全部排除，否则父测试被重复计算）；
- `running tests:` 那一行落下的 `Time` 就是**截断瞬间**，按 `Package` 记；
- 差集 = 起了步没有终态的那些手臂，逐条印年龄；
- 时间戳是 RFC3339Nano（纳秒），`datetime.fromisoformat` 只吃 6 位小数，所以先裁到
  微秒再解析 —— 直接喂会 `ValueError: Invalid isoformat string`（本行第一版就踩了，
  见 §6）。

## 5. 两个会让人误判成「测试很慢」的陷阱

1. **这两条 E2E 不给 `GOPI_TEST_BIN` 就 `SKIP`，用时 0.01s**：
   ```
   cli_tests_test.go:457: gopi binary not found and no test binary pinned — run scripts/e2e_smoke.sh first (it builds bin/gopi)
   --- SKIP: TestTaskRunChildReceivesSummaryAndPayloadE2E (0.01s)
   ```
   于是「单独跑 0.01s 就过了」可以被读成「它一点不慢，所以一定有问题」—— 实际是**根本没跑**。
   单独验证必须给 pin（§2 的命令）。
2. **`t.Parallel()` 的测试在等待期不在 `running` 里**。`testing` 在 `t.Run` 的
   `t.isParallel` 分支里先 `running.Delete(name)` 再等顺序阶段结束，所以一个
   **将来会吃掉整段墙钟**的并行测试，在等待期间**根本不会出现在 `running tests:`**。
   实测（合成最小样本，`go test -v -timeout 3s`）：
   ```
   === RUN   TestSerialHolds
   panic: test timed out after 1s
   	running tests:
   		TestSerialHolds (1s)
   ```
   `TestParallelWaits`（`t.Parallel()` + `Sleep(60s)`）在同一个包里，**一行都没出现**。
   ⇒ `running tests:` 同时会**漏报**并行测试，这个方向的错和 §1 的「多报/误报」是
   两个独立的错。

## 6. 本行踩过的坑（给下一个人）

- **`datetime.fromisoformat('2026-11-08T13:04:27.906435375+08:00')` 直接抛
  `ValueError: Invalid isoformat string`** —— 纳秒超过 6 位小数。先
  `re.match(r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(\.\d+)?(.*)", s)` 拆开再拼（前三组分别是
  日期时间、小数部分、时区）。
- **bash 的 `local age_fmt="${10:-    %s: in flight %.1fs when the cap expired (its run event %s, the cap line %s)}"`
  这种默认值里不能有落单的单引号**（我第一版写的是 `the cap's line`），
  它会让整个 `<<'PY'` heredoc 的引号配平错位，报的却是 python 行号的
  `syntax error near unexpected token ')'` —— 症状离病因两屏远。
- **对照 §1 之前我写过「截断到整秒」**：源码是 `Round`，不是截断。`grep` 一次
  `runningList` 就能定，别把「看起来像截断」当成事实写进注释。

## 7. 与既有机制的关系

`scripts/test-budget.sh` 的归因块本来就在每次运行（不只超时）打印，本行只加了两样：
**每条未收口测试的在飞时长**，以及**缺测量时的显式拒绝**。没有新增任何常驻进程、
定时任务、脚本或配置键（Rule 35）。守卫是
`go test ./internal/testutil -run TestBudgetRunAttributesTheWallClock`，它驱动**发布的
那个 lib**（不是它的改写版），并用**合成 stream** 把 1.839s 这个减法钉死：
13:04:29.739 − 13:04:27.900 = 1.8s。预修版 lib 喂同一个 fixture 会红（red proof 实测）。
