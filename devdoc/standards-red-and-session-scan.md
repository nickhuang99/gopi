<!-- doc-class: historical -->
# Task Report: master 上的三处 standards 红收口 + `taskFindSessionJSONL` 的有界化（Attack A）

**Task:** `01a11528-04a2-743b-b6b4-a05044d75c70` · **Worktree:** `/home/user/workspace/gopi-01a1152804a2743bb6b4a05044d75c70`
**Branch:** `task/01a1152804a2743bb6b4a05044d75c70`（本 run 从 briefing 给的那棵工作树起，未另建 worktree；Rule 6a）· **Date:** 2026-10-07 · **Host:** peerbox

## Summary

三处红彼此无关，逐条判定后各用**最小的一处改动**处置；`internal/standards` 从
`3 个 FAIL + TestStandardGateDataReport 的 NEW 行` → `ok`。第四件（`devdoc/tools-suite-hang-residual.md`
自己推荐但从未提交的 Attack A）落地为 `taskFindSessionJSONL` 的**寻址优先**：会话 id
自己能拼出的两种布局（bare `‹id›/session.jsonl`、legacy flat `‹id›.jsonl`）先用两条
stat 回答，无界树扫描降为回退，并带计数的回归测试钉住「命中时不扫描」。

**收益是真实的但只在 walk-bound 的行上**（见 §4：同一 profile 下 `before == after == ~0.02s`
的行，其 walk 本来就不发生，因为 `taskFindSessionJSONL` 在根上命中 / 或根本不会被调用）。

## 1. 三处红：逐条判定与处置

### §1a `TestStandardGates/TestGate5AccessorPrefixRedProof` — Rule 20，`cmd/agent/cli_tool_spelling_table.go:170`

**判定：真违规（新写代码引入的新违规），处置 = 改名，不入 baseline。**

`isUnderscoreException`（引入时的 `cmd/agent/cli_tool_spelling_table.go:170`）是**零参接收者方法**，名字以 `is` + 大写开头，正是 gate 5
（GOPI.md Rule 20 / checklist 1.4）的判据；同一族另两个命中（`internal/shot/x11.go` 的 `getRootImage` 等）早在 gate 落地时就被审阅并**就地**打了
`// name-ok:` 逃逸标记，baseline 是**空的**——
即这一族的设计意图是「有例外就就地写理由，不往 baseline 塞」。

- 引入者：`6685f7f83da1a1c6aef40516bbda1d2fa89b9042`「fix(cli): tool spellings from ONE
  table; every refusal points at a live door」（2026-10-07，本机），**该 commit 里这个方法是新写的**。
- 所以 §2a「新写的代码原则上改名」在这里就是**唯一**符合仓库惯例的选择：baseline 对该族
  是空的，且 gate 自己的注释写着「an escape documents the decision where the code is;
  a baseline only records that the checker was told to be quiet」。
- 改法（Rule 4，surgical）：`isUnderscoreException` → 按它回答的**值**命名。本 run 落的是
  `keepsUnderscoreSpelling`（这一行是否保留下划线拼写），只动定义处的注释 + 函数名，
  两个调用点：`cmd/agent/cli_completion_exceptions_test.go:58`（同文件内
  `projectedNamingExceptions`）。
- **交付点同步（2026-10-07，合并 master）时发现 master 已经用另一种写法修掉了同一个红**：
  `underscoreException`（同一处 `cli_tool_spelling_table.go`，同样的零参谓词，只去掉了 `is` 前缀）。
  冲突解决**采纳 master 的拼写**（`git checkout --theirs`），理由：两处改名语义与函数体完全相同，
  取 master 那一份可以避免「同一方法在本分支和 master 各有一个名字」的分叉；master 的写法同样是
  「按值命名」的合法解。**因此最终代码里的名字是 `underscoreException`**，上面描述的动作与判定
  （真违规 / 就地改名 / 不入 baseline / 函数体逐字不变）仍然逐条成立，变的只是最终名字。
- 未改动任何行为：函数体逐字不变（`return r.canonical == r.underscored`）。

```bash
# 跑前（master，本工作树 HEAD）：
$ go test ./internal/standards/ -count=1 -run TestStandardGates -v 2>&1 | grep -A2 'TestGate5'
    gate_test.go:101: Rule 20 (...): 2 total hits, 0 baselined, 1 escaped, 1 new (unannotated), 0 baseline entries
    gate_test.go:113: Rule 20 (...): NEW violation (checklist 1.4) at cmd/agent/cli_tool_spelling_table.go:170  [isUnderscoreException — drop the is prefix ...]
# 跑后：见 §3。
```

### §1b `TestCommandsDocGeneratedBlockMatchesSource` + `TestCommandsDocGateRedProofStaleGeneratedBlock` — COMMANDS.md 生成块过期

**判定：真过期（两个 FAIL 是同一处漂移的两面），处置 = 用生成器重写，不手改、不放宽 gate。**

`COMMANDS.md` 的 verb index 是**生成块**，唯一的合法编辑是生成器：

```bash
go run internal/standards/testdata/commands-census.go . COMMANDS.md           # 重写
go run internal/standards/testdata/commands-census.go . COMMANDS.md --check   # 复算，exit 0 = 同步
```

跑前渲染与文档的**第一处差异**（`TestCommandsDocGeneratedBlockMatchesSource` 打印的）：

```
got:  | `gopi checkpoint` | routed only `gopi session checkpoint` | `cmd/agent/cli_checkpoint.go:167` | ...
want: | `gopi checkpoint` | routed only `gopi session checkpoint` | `cmd/agent/cli_checkpoint.go:185` | ...
```

第二个 FAIL 不是第二个 bug：`TestCommandsDocGateRedProofStaleGeneratedBlock` 的**前置条件**就是
「committed 文档的块已经过期」，文档一旦正确，这个前置条件就为假、它自己会红——「修文档，别修它」。

**谁改坏的（修没到位 vs 之后又改坏）——实测结论：之后又改坏。** 证据链：

- 锚点是**路由谓词**的行号（`cliindex` 记 `PredLine`，即 `func isCheckpointSubcommand(...)`
  的 1-based 行号）。`git show HEAD:cmd/agent/cli_checkpoint.go | grep -n 'func isCheckpointSubcommand'`
  → **185**；`git show 406e24860:...` → **167**。也就是说 `406e24860`
  「docs(COMMANDS.md): regenerate the verb index after the tool-group commit moved isToolSubcommand to :431」
  当时**是**正确的（167 == 167，块与渲染一致）。
- 之后 `3540c11be`「fix(checkpoint): bound the scan root by entry count and refuse oversized roots
  by name」在 `cmd/agent/cli_checkpoint.go` 上 **+53 行**（谓词上方加了 prose），把谓词从 167 推到 185，
  **同一个 commit 没有重跑生成器**（该 commit 的 stat：`cli_checkpoint.go`、两个新测试、
  DOC-INDEX、一篇 devdoc —— **没有 COMMANDS.md**）。
- 该族的前史也证明了这**正是**可复现的机制：`a4314fdfa` 之后有 `058faae11` 重生成；`dc30f3bda`
  的标题就是「regenerate the verb index — the session dispatch site moved 175->209」。
  即「谓词上方的 prose 一动，块就过期，必须重跑生成器」。
- 另一篇 2026-10-07 的 devdoc（`commands-doc-anchor-and-block-drift-20261007.md`，task
  `01a1139d` 作为 `01a112ce` 的 REISSUE）**修过同一族**，但它同样只在当时那次编辑后重生成；
  本次红的引入者 `3540c11be` 是**它之后**的 commit。这就是「01a112ce 那份修为什么没拦住这两条」的答案：
  **不是修没到位，是后来又改坏——修法本身正确，缺的是「同一 commit 内重生成」的约束，
  而那个约束没有任何 gate 能自动执行**（生成器不会自己跑）。

### §1c `TestStandardGateDataReport` — 报告行 NEW=1

**判定：不是独立的第三处红，是 §1a 的下游。** 该测试把每个 gate 的 hits 分成
`baselined / escaped / NEW` 并断言 `NEW == 0`（否则「这份报告在替一个红 gate 说绿」）。§1a 修掉
NEW 之后，这一行自动变绿——它**没有**被改过，也没有被跳过。

### §1d 交付点同步（2026-10-07）：合并 master 后的两件事

本 run 的收尾做了**交付点同步**（Rule 30a：它是最后一步，不是中途里程碑）：

1. **`git merge master`** → 2 处冲突，都在 §1a 那一处改名上 —— **master 自己已经用另一种写法修掉了
   同一个红**（`underscoreException`）。冲突取 master 的拼写（`--theirs`），理由见 §1a。
   `internal/tools/tasks.go` 自动合并成功（寻址优先的助手逐字保留）。
2. **合并后 `COMMANDS.md` 又过期了**（合并引入的 `cli_tasks.go` 行号移动：`468` → `475`），
   再跑一次生成器 → `--check` exit 0。这恰好是 §1b 那条「生成物必须在同一 commit 内重生成」约束的
   **又一次实例**：源码一动，生成物就得跟。commit `88885273a`。

同步后 `HEAD..master = 0`（本分支已含 master tip），`git status --porcelain` 空。

## 2. Attack A：`taskFindSessionJSONL` 寻址优先（`devdoc/tools-suite-hang-residual.md`「Not Done」第 1 条）

### §2a 为什么「精确路径」不是「内容前缀匹配」

那份 devdoc 把 Attack A 的代价写成「会把『resolve/not resolve』变成『可能没找到』，因为 id 匹配是
**对文件内容的前缀匹配**」——**这句话在今天的代码上已经过时**：`epic full-id-no-prefix`（2026-09-23）
已经把前缀分支删掉了，`taskFindSessionJSONL` 及其 `…DeepOnly` 都是**精确相等**匹配
（`taskSessionStartID(...) == id`），并有 `TestTaskFindSessionJSONLIsExactOnly` 钉住「截断 id 必须
解析为 nothing」。所以「前缀匹配会假命中」这个顾虑**不成立**。

真正需要论证的是**顺序**，以及它带来的唯一一处行为移动：

- `taskFindSessionJSONL` 的三条路径在新代码里是：**flat → bare → walk**。
  其中 flat 原本就在 walk **之前**（旧代码第一行就是 `findSessionJSONLFlat`），所以只有
  **bare 从「walk 里」前移到「walk 之前」**这一处移动。
- 移动的可见后果只有一种：**同一个 session id 同时存在 bare 目录与 task-run 目录**时，
  优先者由「walk 的 readdir 顺序」变成「id 自己的目录」。而 walk 的顺序**本来就不是一个语义**
  （实测 2026-10-07：一个根里放 `<task>/`、`<id>/`、`<id>.jsonl`，`os.ReadDir` 返回
  task-dir, bare-dir, flat-file —— 非字典序、也非 files-first），它是**目录名**的函数，
  与「哪个文件是这次会话的日志」无关。
- 更重要的是**这个形状在本机根本不存在**：实测 `/home/user/.gopi/sessions` 的 258 个 bare 会话目录
  与 308 个 task-run 目录内的 session 文件，**交集为 0**（623 条带 session_id 的账本行里
  bare=64 / flat=174 / ledger-hint=78 / 只有 walk 能找到=84，见 §4 分类）。
- 因此新行为**只影响一个未被观测到的形状**，且在那个形状上它与 `…ForRow`（行键路径，本来就是
  「自己的目录优先」）一致。这一处移动**写在测试里**（
  `TestTaskFindSessionJSONLPrefersTheIdsOwnDirOverAForeignTaskDir`，含它的理由与实测交集为 0）。

**为什么不去「修 walk 的顺序」**：那会改动 `taskFindSessionJSONLDeepOnly` 在**多候选**时的可见答案，
而不只是成本——比本任务的范围大，且与本任务无关（Rule 26：修法要窄）。本报告把它标为遗留项。

### §2b 实现

```go
func taskFindSessionJSONL(dir, id string) string {
	if p := taskFindSessionJSONLAddressablePath(dir, id); p != "" {   // flat → bare：两条 stat / 一次 root readdir
		return p
	}
	for _, r := range taskSessionTreeRecords(dir) { ... }             // walk：回退，语义不变
	return ""
}
```

新助手 `taskFindSessionJSONLAddressablePath` 是**顺序的唯一决定处**（flat 先、bare 后，与重构前的
顺序逐字一致），这样「便宜路径」与「walk」不可能对这两种可寻址布局的先后产生分歧。
`id == ""` 时整个寻址段被短路（避免把空 id stat 成根上的 `session.jsonl`——那是 walk 明确排除的
根记录）。

## 3. 逐条证据（跑前 / 跑后）

```bash
# ---- 跑前（把三处修复临时换回 HEAD 版本后实测，输出逐字，存 artifacts/pre-fix-standards.txt）----
$ go test ./internal/standards/ -count=1 -timeout 300s \
    -run 'TestCommandsDocGeneratedBlockMatchesSource|TestCommandsDocGateRedProofStaleGeneratedBlock|TestStandardGateDataReport|TestStandardGates' -v
        first difference:
          got:  line 10: | `gopi checkpoint` | routed only `gopi session checkpoint` | `cmd/agent/cli_checkpoint.go:167` | ...
          want: line 10: | `gopi checkpoint` | routed only `gopi session checkpoint` | `cmd/agent/cli_checkpoint.go:185` | ...
--- FAIL: TestCommandsDocGeneratedBlockMatchesSource (2.49s)
    commands_doc_gate_test.go:589: precondition: the committed document's block is already stale
--- FAIL: TestCommandsDocGateRedProofStaleGeneratedBlock (1.98s)
    gate_test.go:101: Rule 20 (no get/is/handle prefix on an accessor): 2 total hits, 0 baselined, 1 escaped, 1 new (unannotated), 0 baseline entries
    gate_test.go:113: Rule 20 (...): NEW violation (checklist 1.4) at cmd/agent/cli_tool_spelling_table.go:170  [isUnderscoreException — drop the is prefix (accessor) or take an argument (command)]
--- FAIL: TestStandardGates (22.13s)
    gate_test.go:126: Rule 20 (...): hits=2 baselined=0 escaped=1 NEW=1
    gate_test.go:132: Rule 20 (...): 1 hit(s) are neither baselined nor escaped — TestStandardGates fails them, so this report would be lying about a green gate
--- FAIL: TestStandardGateDataReport (14.49s)
FAIL    github.com/nick/gopi/internal/standards  44.043s

# ---- 跑后 ----
$ go build ./...                                        → OK
$ go test ./internal/standards/ -count=1 -timeout 300s  → ok  github.com/nick/gopi/internal/standards  109.963s
$ go run internal/standards/testdata/commands-census.go . COMMANDS.md --check  → exit 0
$ <bin> tool doccheck                                   → the devdoc corpus is consistent
# 同一个 TestStandardGateDataReport 的三行（跑后）：
    Rule 19 ...: hits=2 baselined=0 escaped=2 NEW=0
    Rule 20 (no get/is/handle prefix on an accessor) (checklist 1.4): hits=1 baselined=0 escaped=1 NEW=0   ← 2→1，NEW 1→0
    Rule 21 (env-name constant must have a real reader) (checklist 1b/env): hits=0 ... NEW=0
```

## 4. 收益数字（Rule 31：真跑输出）

### §4a 直接测量：目录枚举（`getdents64`）与墙钟

构造 **4,002 条** 的 sessions profile（`/home/user/workspace/gopi-test-scratch/prof-4k`），
账本 = 真实 `/home/user/.gopi/memory.db` 的拷贝，两类行各跑 `before/after`，`strace -f -c`
计数（输出逐字存 `artifacts/getdents-4k.txt`）：

```
### class BARE —— 该 session 的日志就在它自己的 sessions/<id>/session.jsonl（本改动探测的地址）
BEFORE  getdents64=24122  newfstatat=8020   total=32142   real=0.42 user=0.19 sys=0.24
AFTER   getdents64=   75  newfstatat=  19   total=   94   real=0.10 user=0.04 sys=0.06

### class SCAN-ONLY —— 日志在别人的 task-run 目录里，只有 walk 能找到（必须不变）
BEFORE  getdents64=24095  newfstatat=8070   total=32165   real=0.44 user=0.12 sys=0.33
AFTER   getdents64=24093  newfstatat=8072   total=32165   real=0.40 user=0.15 sys=0.24
```

**读法**：可寻址的一类 **目录枚举 24122 → 75 次（-99.7%）**，这是这一处改动的直接效果；
不可寻址的一类 **32165 → 32165（逐次相同）**，证明「回退没被动过」。
`before` 的 24122 次 `getdents64` / 4k 条目 ≈ 每个目录一次根枚举 + 遍历，
一个 54k 的 profile 同比例放大（§4b）——这正是 `devdoc/tools-suite-hang-residual.md`
量到的「线性于目录条目数」的那一项。

### §4b 墙钟（**生产表面，真实 profile**，Rule 31 的 `Ran:`）

```
$ /usr/bin/time -f "BEFORE row=… real=%e" ~/.gopi/…/artifacts/gopi-before task show <row>
$ /usr/bin/time -f "AFTER  row=… real=%e" ~/.gopi/…/artifacts/gopi-after  task show <row>
BEFORE row=01a11579-da6e-70f3-ac6e-961ff141a731 real=4.97     ← 日志在它自己的 sessions/<id>/session.jsonl
AFTER  row=01a11579-da6e-70f3-ac6e-961ff141a731 real=0.30     ← 16.6×
BEFORE row=01a11561-a965-7ed0-afb5-20e5537ba991 real=0.11     ← 日志在别人的 task-run 目录（只有 walk 能找到）
AFTER  row=01a11561-a965-7ed0-afb5-20e5537ba991 real=0.10     ← 不变（回退路径未被改动）
$ diff <(gopi-before task show <row>) <(gopi-after task show <row>)   → 无差异（84 行渲染逐字相同）
```

真实 profile（本机 `~/.gopi/sessions`）上的增益是 **4.97s → 0.30s**，且**渲染结果逐字相同**——
这正是这一处改动要的：**同样的答案，少一次整树扫描**。
放大到 54k 条目（`devdoc/tools-suite-hang-residual.md` 量到的规模）时，被消掉的那棵 walk
本身就是 ~55s 量级（该 devdoc 的线性律：0→18.75s、4k→84.34s、16k→272.93s、
54,847→77.41s at load~20）。

**54k profile 上没取到干净差值的那一类行（诚实）**：可寻址的 walk 被消掉了，但
`taskFindSessionJSONL` 对**索引 0** 执行的就是 `findSessionJSONLFlat(dir, id)`，那是**一次
`os.ReadDir(sessions)`（54k 条目）+ 1 次 basename 比较**——「flat 记录不存在」这件事，
改前的第一趟就已经付了一次 54k 目录枚举。所以对**日志在别处**的行，本改动**不减少**那一次枚举
（它只是把 bare 的两条 stat 插在同一位置之前），54k 上这类行的墙钟在噪声内。

要连 §4a「枚举仍在」的那一半也消掉，必须让 flat 的读取从「枚举根 + 比较 basename」变成
「**一条 stat**」——
根上还存在**没有 sessionId 的裸 `‹uuid›.jsonl` 记录**，无内容读无法把「无身份的 flat 记录」与
「任务的 flat 记录」分开，
所以那是**独立的下一步**（见「Not Done」第 3 条），本 run 不动。

**本次回归测试钉住的是「属性」而不是「墙钟」**（Rule 27，不制造负载）：

```bash
$ go test ./internal/tools/ -count=1 -run 'TestTaskFindSessionJSONLAddressesTheIdBeforeWalkingTheTree' -v
--- PASS  (bare_session_dir, legacy_flat_record)
# RED 证（把寻址段短接 return "" 后再跑，同一测试）：
    tasks_session_resolve_cost_test.go:76: resolving the bare session dir layout walked the sessions tree 1 time(s); ...
    tasks_session_resolve_cost_test.go:76: resolving the legacy flat record layout walked the sessions tree 1 time(s); ...
--- FAIL: TestTaskFindSessionJSONLAddressesTheIdBeforeWalkingTheTree
```

**为什么墙钟没能在 54k profile 上复现 devdoc 里那条 「73.61s → 0.14s」的数量级**（诚实）：
那份 devdoc 的 73.61s 是**测试内**的 `TestTaskStampRun`，而本次量的是**进程整体**
（`gopi task show` 启动 + 读 184MB 账本 + 渲染），本机的启动/账本成本本身就把它淹掉了；
而 §4 那三行的目录枚举量（`getdents64`）才是这一处改动的直接测量——见 §4 末尾的 syscall 计数。
本次没有用 `-short`、没有调超时、没有制造负载（Rule 27）。

**红证的范围（诚实）**：短接寻址段会让 4 个测试红——
`…AddressesTheIdBeforeWalkingTheTree`（**以 walk 计数为判据**，正是验收要求的那条）、
`…AddressingPreservesTheScanPrecedence`、`…PrefersTheIdsOwnDirOverAForeignTaskDir`（以值为判据）、
`…AddressesTheIdBeforeWalkingTheTree/legacy_flat_record`。它们全部属于**这一处改动**，
不是「整个 package 变红」；`…KeepsTheWalkAsTheFallback` 在红态下**仍然 PASS**（walk 未被破坏）。

**这一轮还纠正了 sibling 测试的一个错误 oracle**：`TestRowAddressResolutionMatchesTheScan`
（行键路径的既有回归）原本拿 `taskFindSessionJSONL` 当作「walk」的替身来比对
`taskFindSessionJSONLForRow`——本轮实测发现**它本来就是绿的**（5 次连跑全 ok），
但这是**巧合**：两者在 `session_dir_sorts_first` 这个形状上恰好一致，而 `os.ReadDir` 的顺序
无保证（§2a）。所以把 oracle 换成**真正的 walk**（`taskFindSessionJSONLDeepOnly`，
即 `…ForRow` 回退时实际调用的那一个），使这条断言从「碰巧」变成「定义上」成立。
`TestRowAddressResolutionMatchesTheScan` 的**五条子断言全部未改**，只换了 oracle。

## 5. Verification

- **Build:** `go build ./...` → OK（跑前/跑后各一次）
- **Tests:**
  - `go test ./internal/standards/ -count=1 -timeout 300s` → 跑前 4 个 FAIL（§3）；跑后 `ok`（§3）。
  - `go test ./internal/tools/ -count=1 -run 'TestTaskFindSessionJSONL|TestRowAddressResolution|TestFindSessionJSONLFlat|TestSessionLayoutPrecedence|TestSessionTreeScanStaysTheFallback|TestLedgerNamedDirOutranksTheScan'` → `ok`
  - 红证：短接寻址段 → 上述测试红、以 walk 计数为判据；还原 → 绿。
- **Ran（Rule 31，verbatim）：**
  - `go run internal/standards/testdata/commands-census.go . COMMANDS.md` → `commands-census: wrote COMMANDS.md (44 verbs)`
  - `go run internal/standards/testdata/commands-census.go . COMMANDS.md --check` → exit 0
  - `<bin> task show 01a11579-da6e-70f3-ac6e-961ff141a731`（真实 profile，无 `GOPI_HOME` 覆盖）→
    `BEFORE real=4.97` / `AFTER real=0.30`，两次渲染 `diff` 为空（84 行逐字相同）
  - `<bin> task show 01a11561-a965-7ed0-afb5-20e5537ba991` → `BEFORE real=0.11` / `AFTER real=0.10`
  - `GOPI_HOME=<4k profile> GOPI_SETTINGS=/home/user/.gopi/settings.json strace -f -qq -c -e trace=getdents64,newfstatat <bin> task show <row>` → §4a
  - `<bin> tool doccheck` → `doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row`
- **Result:** standards 三处红 → 全绿（跑前 4 个 FAIL 的输出逐字存 `artifacts/pre-fix-standards.txt`）；
  `taskFindSessionJSONL` 对两种可寻址布局走 0 次 walk（红证按 walk 计数复现）；
  真实 profile 上 `task show` 4.97s → 0.30s 且渲染逐字相同。
- **Commit:** `d763d822c`（分支 `task/01a1152804a2743bb6b4a05044d75c70`，worktree `/home/user/workspace/gopi-01a1152804a2743bb6b4a05044d75c70`；Rule 6c/6d — worktree 与主仓共享 refs，无需 push）

## Evidence (self-filed)

- **三处红的 `running/listening` 量**：把三个文件（`COMMANDS.md`、`cmd/agent/cli_tool_spelling_table.go`、
  `cmd/agent/cli_completion_exceptions_test.go`）临时换回 HEAD 版本后跑
  `go test ./internal/standards/ -count=1 -run '…四个测试…' -v`（输出逐字存 `artifacts/pre-fix-standards.txt`），
  观察到 4 个 FAIL：`TestCommandsDocGeneratedBlockMatchesSource`（first difference 逐字：
  `cli_checkpoint.go:167` vs `:185`）、`TestCommandsDocGateRedProofStaleGeneratedBlock`
  （`precondition: the committed document's block is already stale`）、`TestStandardGates`
  （`Rule 20 …: hits=2 baselined=0 escaped=1 NEW=1` + `NEW violation (checklist 1.4) at
  cmd/agent/cli_tool_spelling_table.go:170`）、`TestStandardGateDataReport`
  （`Rule 20 …: 1 hit(s) are neither baselined nor escaped — … so this report would be lying
  about a green gate`）；还原后同一命令 `ok`。
- **pre-existing 红的同一性**：`git show HEAD:cmd/agent/cli_tool_spelling_table.go` /
  `…cli_completion_exceptions_test.go` 写回后跑
  `go test ./cmd/agent/ -count=1 -run TestCLINamingExceptionsAreStillDispatchable`，
  仍以逐字相同的两行 `"web_search"/"web_fetch" has no contract-inventory entry` FAIL；
  还原本 diff 后同样 FAIL ⇒ 本 diff 无关。
- `git show 406e24860:cmd/agent/cli_checkpoint.go | grep -n 'func isCheckpointSubcommand'` → `167`；
  同一命令在 `HEAD` → `185`；`git show --stat 3540c11be` 的 stat 里**没有** `COMMANDS.md`。
- 4k profile syscall 计数（`strace -f -qq -c -e trace=getdents64,newfstatat`）逐字存
  `artifacts/getdents-4k.txt`：可寻址类 `24122 → 75` 次 `getdents64`，不可寻址类 `24095 → 24093`。
- 交集判定：`/home/user/.gopi/sessions` 的 bare 目录 258 个、task-run 目录内 session 文件 308 个，
  交集 **0**（脚本逐目录 `os.listdir`）。
- 红证：短接 `taskFindSessionJSONLAddressablePath` → `--- FAIL: TestTaskFindSessionJSONLAddressesTheIdBeforeWalkingTheTree`
  且失败行是 walk 计数（`walked the sessions tree 1 time(s)`）；`cp artifacts/tasks.go.mine2` 还原后同一命令 `ok`。

## Not Done（诚实）

0. **`cmd/agent` 里有一条与本 diff 无关的红：「`TestCLINamingExceptionsAreStillDispatchable`」**
   （`"web_search" / "web_fetch" has no contract-inventory entry`）。**已验证是 pre-existing**：
   把 `cli_tool_spelling_table.go` + `cli_completion_exceptions_test.go` 两个文件换回 **HEAD
   版本**后，同一条测试以**逐字相同的两行**失败（§Evidence）。本 diff 未碰契约清单，
   也不是它的成因；本报告只点名，不修（Rule 26）。
1. **本 run 的工作树是 briefing 给的那一棵**（`/home/user/workspace/gopi-…-01a1152804a2743bb6b4a05044d75c70`，
   分支 `task/01a1152804a2743bb6b4a05044d75c70`），未另建 worktree（Rule 6a）。
   交付物 = 该分支上的 commit：**`d763d822c`**，恢复命令 `git show d763d822c:devdoc/standards-red-and-session-scan.md`（在 `/home/user/work/GoPi` 或本 worktree 里均可）——已在主仓 refs 中。
2. **`taskFindSessionJSONLDeepOnly` 的多候选顺序仍是 `os.ReadDir` 的顺序**（非字典序，实测见 §2a）。
   本 run 只保证「不再让 walk 决定**可寻址**布局的优先者」，没有重排 walk 自身的可见答案——那是独立工作。
3. **flat 布局仍要一次 54k `os.ReadDir`**（§4 第 2 类收益没有拿满）。要拿满需要把 flat 的读取变成
   「一条 stat」，而根上存在**无任何身份标记**的裸 `‹uuid›.jsonl`（chat 日志），所以必须先在写入侧
   给这些文件留标记——属于**独立、可单独验证**的工作，未在本 run 顺手扩大（Rule 26）。
4. 54k profile 与 `/home/user/.gopi/memory.db` 的只读拷贝留在
   `/home/user/workspace/gopi-test-scratch/prof-54k`（scratch 区，不在仓库内）；两个测量用二进制留在
   session 目录 `artifacts/`。

## Deliverable declaration（Rule 6d）

1. **Branch:** `task/01a1152804a2743bb6b4a05044d75c70`（worktree `/home/user/workspace/gopi-01a1152804a2743bb6b4a05044d75c70`）
2. **Commit:** `d763d822c`
3. **Recovery:** `git show d763d822c:devdoc/standards-red-and-session-scan.md`，
   或 `git log --oneline -1 task/01a1152804a2743bb6b4a05044d75c70`（worktree 与主仓共享对象库/refs，
   所以该 commit **已经在主仓里**，无需 push；Rule 6c）。
4. worktree 保留（不自行删除，merge 由审阅方处理）。
