<!-- doc-class: historical -->
# checkpoint 索引器：两层 raw-log 布局 + 零结果自证

**任务**：01a0ed99-b05a-7721-8222-8fe93fd2e3b0（branch `task/01a0ed2312457e67a8c2cb827c03a4f1`）

## 缺陷（实测，非推断）

`session-dir-unify S1`（3a816775）把 raw HTTP 日志从平铺目录 `<gopi>/raw/gopi-raw-YYYYMMDD-HHMMSS.log`
搬到了与 run 共址的 `<sessions>/<run-dir>/<run-uuid>.raw.log`。而
`internal/checkpoint/index.go` 的 `IndexDir` 仍只读**一层**并要求
`gopi-raw-` 前缀 + `.log` 后缀 —— 于是搬迁后写下的日志一条也索引不到。

本机测量（`find ~/.gopi/sessions -maxdepth 2 -name '*.raw.log'`）：

| 布局 | 数量 | 体积 |
|---|---|---|
| `sessions/<run-dir>/<run-uuid>.raw.log`（新） | 1265 | 41278.6 MB |
| `<gopi>/raw/gopi-raw-*.log`（旧） | 6 | — |

**旧 binary（master）实测**：`gopi checkpoint list --dir ~/.gopi/sessions` →
`no raw logs found under …`，exit 0 —— 41 GB 证据被报成"没有"。
默认 `--dir` 走 `rawLogDir()`（`~/.gopi/raw`），连这 1265 条都不看一眼。

第二个缺陷是**可诊断性**：零结果只打印一句 `no raw logs found under <dir>`，
既不说看过哪些目录，也不说判定规则，操作者无法区分"目录写错了"、"文件名规则
跟不上写入方了"、"布局还没学"。

## 修法

1. **`internal/checkpoint/index.go`**
   - `IndexTree(dir)`：一次有界扫描，产出 `[]LogIndex` + `ScanReport`。
     - 平铺布局：`gopi-raw-*` + `.log`（行为不变）。
     - 共址布局：`<run-dir>/<run-uuid>.raw.log`，下沉**仅当 dir 自己的名字
       是 `config.SessionsDirName`**（`isSessionsRoot`，按名不按路径 ——
       `GOPI_HOME`/sandbox/`--dir` 都能重定位 profile 而名字不变）。
     - 下沉深度 = `sessionScanDepth = 1`，命名常量。**为什么必须浅**：本机
       sessions 树里嵌套着整份私有 profile
       （`<run-dir>/evidence/**/.gopi/sessions/…`，1298 条、32 MB/条），
       深走就是把任务自己的 scratch 树重读一遍 —— 正是 #1313 的挂起形状。
     - 命名常量取自写入方自己的定义（`scratch.RawLogNamePrefix` /
       `RawLogFileSuffix` / `SessionRawLogFileSuffix`，Rules 1/13/14），
       `IsRawLogName` 是两种布局唯一的判定谓词。
   - `ScanReport{ScanRoot, Roots[], Skips[], Files, Skipped}` + `Describe()`：
     逐目录（条目数 / 命中数 / 不可读原因）+ 逐条拒绝理由 + 唯一判据文本。
     `IndexDir(dir)` 保留为"不下沉"的薄壳，旧调用者与旧测试不动。
   - `countScanBudgetBytes`（16 MB 头部采样）原样保留 —— 边界扫描不动。

2. **`cmd/agent/cli_checkpoint.go`**
   - `checkpointDir()`：`--dir` > `GOPI_RAW_LOG_DIR` > `<gopi>/sessions`。
     `rawLogDir()`（`<gopi>/raw`）保留给 dispatch 错误日志等既有调用者，
     两者职责分开，互不偷改。
   - `list`：**零结果与非零结果都**在 stderr 打印 `ScanReport.Describe()`
     （看过的目录 + 判据）；零结果时 stdout 明确指向该报告。
   - `resolveRawLog`：候选阶梯扩展到 `<dir>/<run-uuid>/<run-uuid>.raw.log`
     等共址形态；未命中时打印同一份 `ScanReport`（**同一次扫描**同时供给
     查找与解释，避免"解释用的读取"与"查找用的读取"不一致）。
   - `-h` 更正：`~/.gopi/raw/` 的过时描述改为"两种布局 + 默认 sessions 根 +
     list 会自证"。

## 验证（自建 binary + 私有 sandbox profile 真跑）

见 `task-report.md` 的 `## Verification`。关键红→绿证据：

- **旧 binary**（master，从本分支暂存前的源码构建）：`checkpoint list --dir <sessions root>` → `no raw logs found …`（0 条），且完全不打印判据。
- **新 binary**：同一条命令列出 `01a0ed99-…-000000000001.raw.log | reqs=1 | m-helper`，
  并在 stderr 打印"看了 2 个目录 + 判据"两行。
- 该 raw 日志由**写入方自己的构造器**产生（`agent.newRawLoggerInDir`），
  不是手搓文件 —— 文件名与布局都是系统真值。
- legacy 平铺回归：`--dir <profile>/.gopi/raw` 与 `GOPI_RAW_LOG_DIR=…` 都仍列出
  `gopi-raw-20260101-000000.log`。
- `--dir` 指向非 sessions 根（profile 根目录）时按平铺列举，不外溢。
- `export <裸 run-uuid>` 在共址布局下解析成功并打印 checkpoint JSON。
- 私有 profile：`GOPI_HOME=<…>/evidence/profile`，从不触碰 `~/.gopi`。

## 未做（诚实）

- 会话根以下**更深**的嵌套（例如 `<run-dir>/evidence/…/sessions/<run>/`）按设计
  不索引（Rule 27 的有界扫描 + 避免重读任务 scratch）。若将来真实运行目录需要
  更深，改的是 `sessionScanDepth` 这一个常量，并必须同时补一条"证据树不外溢"
  的反向测试。
- `gopi log --raw` / `gopi stats` 走的 `gopilog.ResolveRawLogForSession` 解析路径
  **未改**（任务约束明确排除）。
