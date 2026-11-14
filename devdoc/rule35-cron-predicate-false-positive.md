<!-- doc-class: historical -->
# Rule35 依赖门：cron 判据被 awk 字段布局误报（修复记录）

**日期**：2026-09-27 · **分支**：`task/01a0ef5d9b89746ea5799fec4f7b22bf` · **提交**：`de2ac7eb`
**文件**：`internal/testutil/rule35_dependency_gate_test.go`（只改门；被扫脚本一行未动）

## 症状

任何工作树里 `go test ./internal/testutil/ -run Rule35` 都红：

```
rule35 audit: 278 script file(s), 6745 tracked path(s), 1 hit(s) map[self-scheduling: cron expression:1]
  self-scheduling: cron expression — scripts/materials-coverage.sh:242:
  "split(\"3 4 5 6 7 8\",cols,\" \");""
```

被命中的那行是 awk 的**字段布局串**（把 TSV 的 3/4/5/6/7/8 列命名成
card/input/output/raw/annotated/finding），不是任何调度。

## 根因（5-why）

1. 为什么命中？`rule35FiveFieldsThenCommand` 的两个条件同时成立。
2. 条件一「前 5 token 是合法 cron 字段」—— 为什么成立？该行**从 `3` 开始切分**时，
   `3 4 5 6 7 8` 的前五个 token 恰好是干净的单个数字，且 `rule35CronFieldOK`
   允许单个数字（这是刻意的：`0 3 * * 1` 合法）。
3. 为什么从 `3` 开始切？`rule35ScheduleStarts` 允许片段从**引号之后**开始 ——
   这条是为 `entry="*/10 * * * * cmd"` 设计的（首个空白分隔 token 是
   `entry="*/10`，不是字段），被扫行里 `split("` 的引号正好制造了这样一个起点。
4. 条件二「第 6 token 不是字段」—— 为什么成立？第 6 token 是 `8",cols,"`，
   含 `"` 故 `rule35CronFieldOK` 返回 false。**即：判据把「含引号」读成「这就是命令」。**
5. 因此根因是：判据缺一条 —— 「第 6 token 里残留的引号说明它是另一种语言的
   字符串字面量，不是一个命令词」。它把「不是时间字段」当成了「是命令」，
   而不是「某个东西可能是命令」。

## 修复

新增命名判据 `rule35ActionTokenIsCommand(action string) bool`（第 6 个 token 要算
COMMAND 必须同时满足）：

- 自身不是合法时间字段（原语义，保留 `for i in 0 1 2 3 4 5 6 7 8 9` 控制）；
- 去掉**一个尾引号**（成对收尾的 wrapper）并削掉尾随 shell 标点（`rule35CommandTailPunct = ");"`）
  之后，token 内**不得再残留引号**（`rule35CronQuoteChars`）。

只削尾、不削首：片段起点本就在引号之后，首引号不可达。这样
`entry="*/30 * * * * cmd"`（收尾引号）、`(crontab -l; echo "0 3 * * 1 /x.sh") | crontab -`
（收尾引号 + `)`）仍然命中，而 awk 布局里那个**幸存**的引号成为唯一的判据依据。
被测量的误报在函数注释里逐字记录 —— 下一个读到这段代码的人需要知道它为何存在。

## 回归守卫

`TestRule35CronExpressionPredicateTightened`：直调 `rule35CronExpressionIn`（**不经豁免表**，
所以没有任何文件能被「放行」进绿），双向断言 4+4：

| 反例（必须不命中） | 正例（必须仍命中） |
|---|---|
| `split("3 4 5 6 7 8",cols," ");` | `*/10 * * * * /home/user/bin/hp-worker-autostart.sh` |
| 单行 awk 程序里的同一布局 | `local entry="*/30 * * * * ${stable_watchdog} …"` |
| `for d in (0, 1, 7, 30, 90, 180, 365):` | `@reboot /home/user/bin/gopi serve` |
| `for i in 0 1 2 3 4 5 6 7 8 9; do …` | `(crontab -l; echo "0 3 * * 1 …") \| crontab -` |

**RED 证明**：`git archive HEAD` 物化修复前的 master 到 scratch（用 `gopi sandbox create`，
off `/`；不用 `git show > file`，那会丢 mode 与兄弟文件），拼 `package testutil` 头后放入该树运行
→ `--- FAIL`，逐字命名两条 awk 坏样本；同一测试在收紧后 PASS。

**非真空**：审计规模与报告行数前后逐字对比 —— `278 script file(s), 6745 tracked path(s)`，
`1 hit → 0 hit`；`rule35MinScannedFiles`（40）与 `rule35MaxReportedHits`（15）未动，
未新增任何豁免列表项。真阳性用例（含 `@reboot`）全部仍在命中。

## 局限（诚实）

判据读的仍是**形状**而非意图：其它语言里恰好长得像「五个字段 + 一个词」的构造仍可能
绕过或误报；`@reboot` 走的是另一条正则（本次未改）。本次只关闭了被测量的这一条路径。
