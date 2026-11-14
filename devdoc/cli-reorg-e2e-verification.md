<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# CLI 重组 / 动词退役 — 端到端验收核验

**核验对象**：master 上「CLI 命令重组 / 顶层动词退役」重构（`internal/taskverbs/verbs.go` 退役表 + 家族门）。
**核验点**：worktree `/home/user/workspace/gopi-01a113a891d07182b49d555a8654357d`，分支 `task/01a113a891d07182b49d555a8654357d`，**HEAD `ef4211e96`**（Merge branch 'task/01a113a889d97a5da06ccc682dfcbac1'）。
**方法**：全部结论来自**真跑**（Rule 31）——构建产物 `bin/gopi` 的真实调用 + 真实 `go test`；无一条来自读代码推断。

## 0 基线与范围

```
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     → exit 0
```

**范围（Rule 27：不全树）** 只跑退役表/命令面所在包：
`./cmd/agent ./internal/standards ./internal/taskverbs ./internal/cliverbs ./internal/runcmd ./internal/toolnames`。
理由：这三个问题分别落在 (a) 命令派发与 help 面（`cmd/agent`）、(b) 语料/生成块一致性门（`internal/standards`）、(c) 退役表本身（`internal/taskverbs`）——其余包与本次重构无交集。

**语料归类**：本报告是**某一次核验跑**的记录，不是对 master 的长期主张，故归 `historical`（banner `<!-- doc-class: historical -->` + `DOC-INDEX.tsv` 行），并已用
`gopi tool doccheck`（`the devdoc corpus is consistent`）与 `python3 tools/docclass/classify.py . --check`（`0 gap(s)`）复核一致。

## 1 问题1：被点名的红测试，现况（绿 / 仍红 / 已删）

### 1.1 已经绿（抽查 defs=1，未见 FAIL）

`TestBashScriptStructure`、`TestVoiceHTTPServerEndToEnd`、`TestVoiceInputIsAContractToolMirror`、
`TestVoiceInputIsRegisteredInContractMirrors`、`TestProxyBusyPortNeverClaimsListening`、
`TestCLIContractInventoryMatchesDispatch` —— 这些在 01a1103b 的 14 红与 01a10ec5 的 13 红名单里，现在**在聚焦复现与全量跑中均未出现 FAIL**，且**定义仍在**（`grep -rn "func <Name>(" --include=*_test.go` → `defs=1`，全部 8 个抽查对象均非删除）。

### 1.2 仍然红 — `cmd/agent`（聚焦复现，`-count=1`，13 FAIL，证据 `q1-cmdagent-focused-full.txt`）

| # | 测试 | 实测原因（原样引用） |
|---|---|---|
| 1 | `TestAnnotateVerbRunsARealArgvEndToEnd/-h` | `-h did not print the verb's usage:` —— 实测 `-h` **确实打印了**完整 usage（`gopi tool annotate — draw marks onto an EXISTING PNG…`），即**测试期望与实现相反** |
| 2 | `TestChatBoundaryTenMegabyteInputStaysBounded` | `a 10485760-byte input produced a 10875-byte request body — the message grew with the input` |
| 3 | `TestChatStatus_LiveBlockKeepsTheStateWord` | 行内为 `resp\u00a0`，断言要求 `responding\u00a0` |
| 4 | `TestKeywordSearchHelpPageDiffersFromTopLevel` | `gopi keyword_search -h: exit error: exit status 1` |
| 5 | `TestKeywordSearchRunPrintsHitsAndNoMatches` | `gopi keyword_search EUAUDIT_NEEDLE_ZZZ: exit error: exit status 1` |
| 6 | `TestSandboxDirFlagDecidesTheRoot` | `gopi tool sandbox create …: exit status 1` → `gopi tool: unknown command "create" — this group knows: act\|annotate\|capture\|code-search\|doccheck\|gif\|keyword_search\|ledger-window\|proxy\|sandbox\|scratch-gc\|screen_capture\|shot\|text2img\|text2img-verify\|vision_analyze\|voice_input\|web` |
| 7 | `TestToolGroupWordsAreTheOnlyDoor` | `the group forwards to "web", which this binary ALSO dispatches at top level` / `"web" is refused NOWHERE at argv[0]` |
| 8 | `TestToolGroupForwardsToTheSameImplementation/web` | `` `gopi web -h` exited 0 — the bare spelling must refuse now that the group is its only door `` |
| 9 | `TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation` | `` `gopi tool voice_input` is advertised by the help (voice_input) but the inventory does not list it `` |
| 10 | `TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason` | `` "gopi tool voice_input" is advertised and NEITHER tested NOR declared blocked `` |
| 11 | `TestUndispatchedToolWordsNameADispatchingCanonical` | 8 行：`"code_search" refuses itself — a word that IS the canonical spelling must dispatch, not refuse (canonical "code-search")`；同型：`keyword-search`、`vision-analyze`、`screen-capture`、`ui-state`、`voice-input`、`web-search`、`web-fetch` |

### 1.3 仍然红 — `internal/standards`（3 FAIL，**聚焦跑也红**）

```
go test ./internal/standards/ -count=1 -run 'TestDocClassBannerMatchesManifest|TestCommandsDocGeneratedBlockMatchesSource|TestCommandsDocGateRedProofStaleGeneratedBlock'
→ rc=1, 3 FAIL   (job-22, 29.8s)
```

**这三条是稳定的红，不是全量跑的顺序/环境偶发。** 具体内容：

```
docclass_gate_test.go:212: devdoc/cli-help-user-audit.md: carries the "historical" banner but
  devdoc/DOC-INDEX.tsv classes it "normative" — only [historical] classes are bannered
docclass_gate_test.go:222: devdoc/DOC-INDEX.tsv: 1163 markdown docs of a bannered class carry
  their banner (1 mislabelled)
```

实测落点：`devdoc/cli-help-user-audit.md` 首行为 `<!-- doc-class: historical -->`，而 `devdoc/DOC-INDEX.tsv:374` 记的是
`devdoc/cli-help-user-audit.md  normative  normative: standing claim about master, re-runnable…`。**两者矛盾**（banner 说 historical、清单说 normative），门按 banner 判定并报红。

### 1.4 根因（5-why，Q1）

1. **退役表不认「规范拼写」**：`code_search/keyword_search/vision_analyze/screen_capture/voice_input` 是工具的**规范**拼写，退役表却把它们当"非规范词"自拒；测试断言"规范拼写必须派发"→ 直接对撞（#11）。
2. **`ui_state` 的连字符形式**：`ui_state` 是 argv[0] 顶层词，其常量是 `ui_state`，而 `ui-state` 被当成"拼错"提示 → 与 `TestUndispatchedToolWordsNameADispatchingCanonical` 声明的 canonical `ui_state` 不一致（#11）。
3. **`tool sandbox` 组缺 `create`**，而 `gopi tool -h` 把 sandbox 组当已实现面广告（#6）。
4. **`web` 有两个门**（顶层 `gopi web` + `tool` 组转发 `web`），组门测试要求「工具组是唯一门」（#7/#8）。
5. **`voice_input` 在 tool 组 help 中被广告，但 g6 帮助面清单未收录** → "advertised but never typed"（#9/#10）。
6. **`cli-help-user-audit.md` 的 banner 与清单 class 不一致**（#1.3）——这是本次重构遗留的语料半成品。

## 2 问题2：用户面自洽 — 见同次会话的独立核验结论

退役旧拼写 29 个全部 `rc=1` 按名拒绝；退役表内 28 行 canonical 地址逐条 `-h` 全部 `rc=0`（无死胡同）；
新拼写抽样真跑全部工作（`version`、`session log|stats`、`tool text2img` 产 PNG、`tool text2img-verify` 产 `z_20px.png`、
`tool ledger-window`、`tool sandbox show`、`tool code-search`、`tool keyword_search`、`tool doccheck`、`tool scratch-gc --dry-run`、`tool voice_input`）。
拒绝措辞两类，实测原文：

```
gopi: `keyword_search` is RETIRED — use `gopi tool keyword_search` (`gopi tool keyword_search -h` for its help).
      To send the word as a prompt instead, say so explicitly: `--prompt keyword_search`.
```

文档侧 `COMMANDS.md` 内 23 处 `gopi <old>` 全部位于「§2 生成的动词索引」表内、由生成器产出，并自带
"routed only `gopi tool X`" 解释列——属于**陈述而非误导**。

## 3 问题3：半成品清单（逐项证据）

| # | 半成品 | 证据 | 判定 |
|---|---|---|---|
| 1 | **`gopi session checkpoint list` 默认路径不可用** | 默认（`~/.gopi/sessions`，一级 `83949` 个条目）跑 **4m40.127s 后被 KILL**（`rc=137`，**零输出**）；**而 `user 0m0.002s`** —— 几乎不吃 CPU，即**阻塞在 I/O/系统调用上**，不是计算型无界扫描。`--dir <空目录>` 立刻 `rc=0` 并打印自解释扫描报告 | **全损项**：动词在默认输入上不可用 |
| 2 | `tool sandbox create` 缺失 | §1.2 #6 原文 | 部分退役 |
| 3 | `web` 双门 | §1.2 #7/#8；实测 `gopi web -h rc=0` 且 `gopi tool web -h rc=0` **两者同时可用** | 半成品 |
| 4 | `voice_input` 被广告但未登记/未测 | §1.2 #9/#10 | 半成品 |
| 5 | 8 个规范拼写自拒 | §1.2 #11 | 半成品 |
| 6 | `cli-help-user-audit.md` banner/class 不一致 | §1.3 + `DOC-INDEX.tsv:374` | 语料半成品 |

**"同一动词两种拼写是别名还是泄漏"**：逐条实测（`-h` 派发与否）后确有唯一一类例外——工具的**下划线规范拼写**在 `tool` 面成立
（`tool code_search`❌/`tool code-search`✅、`tool keyword_search`✅/`tool keyword-search`❌、`tool vision_analyze`✅、
`tool screen_capture`✅、`tool voice_input`✅），而 `ui_state` 是**顶层**词（`ui_state`✅/`ui-state`❌/`tool ui_state`❌）。
这与 `devdoc/cli-naming-convention.md` 声明的 rule1/rule2 **一致**，属**刻意例外而非泄漏**——但正是这条例外与 #11 的测试断言对撞。

## 4 结论：**本次重构不能称为已完成**

- 用户面**基本自洽**（旧拼写全部按名拒绝、建议指向真实命令、新拼写可用、无死胡同）。
- 但**验收门是红的**：`cmd/agent` 13 FAIL + `internal/standards` 3 FAIL，且 3 条 standards 红在**聚焦跑中同样复现**（非偶发）。
- 红的原因是**重组自身的半成品**（工具组缺子命令、双门、广告未登记、规范拼写自拒、banner/class 不一致），而非环境噪声。
- 另有一项**全损**：`gopi session checkpoint list` 在默认路径上不可用。

**缺少的部分**：上述 §1.2 (6)(7)(8)(9)(10)(11) 与 §1.3 的修复，以及 §3 #1 的无界扫描修复。

## 5 Not Done（诚实）

- **未修任何一条红**：全部修复都越过本核验任务的边界（改动退役表/命令面属系统层，Rule 28 归 peer1 的系统层改动，且 Rule 26 要求以"新任务"而非本会话修补）。本报告只做取证与判定。
- **未跑全树**（Rule 27）；只在 §0 列出的包上取证。
- **`TestCommandsDocGeneratedBlockMatchesSource` / `...RedProofStaleGeneratedBlock` 的"第一处差异行"未逐字摘出**——只采到门的红/绿判定，未采到它打印的 `first difference:` 文本。
- **§3 #1 的机制只下钻到一半**：观测到 `real 4m40.127s / user 0m0.002s / rc=137`，可断定**不是计算型扫描**而是**阻塞在 I/O**；但**未定位到具体是哪一个文件/哪一个 syscall**（`~/.gopi/sessions` 一级 83949 个条目、含 82M 级 run 目录；`find`/`du` 在其上同样 20–25s 超时而**无输出**，与 `user≈0` 一致）。
- **未复现这 3 条 standards 红在**「全量 `make test-budget` 跑」与「聚焦跑」之间的**差异来源**（本次两处都采到红，故不影响结论；差异本身未查）。

## 6 范围外、应各自开新任务的问题

1. `gopi session checkpoint list` 默认路径无界扫描 → 系统层缺陷（peer1）。
2. `cmd/agent` 13 条红：其中 `TestChatBoundaryTenMegabyteInputStaysBounded`、`TestChatStatus_LiveBlockKeepsTheStateWord` 与 CLI 重组**无关**（chat 边界/状态行），应各自成任务。
3. `TestAnnotateVerbRunsARealArgvEndToEnd/-h` 的**测试期望与实现相反**：实现打印 usage，测试要求不打印——需判定哪一侧是对的（涉 `internal/text2img`/annotate 语义）。

## 7 验证（Rule 5）

- **Build**：`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `exit 0`。
- **Tests**（Rule 27：SMOKE、按分支改动范围，不全树）：
  - `go test ./cmd/agent/ -count=1 -run '<13 个测试名>'` → `rc=1`，**13 FAIL**（`q1-cmdagent-focused-full.txt`）。
  - `go test ./internal/standards/ -count=1 -run 'TestDocClassBannerMatchesManifest|TestCommandsDocGeneratedBlockMatchesSource|TestCommandsDocGateRedProofStaleGeneratedBlock'` → `rc=1`，**3 FAIL**（`q1-standards-focused.txt`）。
  - 全量 `make test-budget PKGS='./cmd/agent ./internal/standards ./internal/taskverbs ./internal/cliverbs ./internal/runcmd ./internal/toolnames'`：`taskverbs` PASS、`runcmd` PASS、`cliverbs` PASS、`toolnames` SKIP、`standards` FAIL、`cmd/agent` FAIL。
- **Ran**（改了哪个面就跑哪个面，verbatim）：
  - `./bin/gopi tool doccheck` → `doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row`，`rc=0`
  - `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`，`rc=0`
  - `./bin/gopi keyword_search -h` → ``gopi: `keyword_search` is RETIRED — use `gopi tool keyword_search` …``（旧拼写按名拒绝）
  - `./bin/gopi tool code-search -h` → `rc=0`；`./bin/gopi tool code_search -h` → `rc=1`
  - `./bin/gopi web -h` → `rc=0`；`./bin/gopi tool web -h` → `rc=0`
  - `timeout -s KILL 280 ./bin/gopi session checkpoint list` → `rc=137`，`real 4m40.127s / user 0m0.002s`，**零输出**
  - `./bin/gopi session checkpoint list --dir <空目录>` → `rc=0`，打印自解释扫描报告
- **Result**：报告落盘为本文件并入库（`DOC-INDEX.tsv`），语料门绿；核验判定「重构未完成」，红门与全损项见 §1–§4。

## 8 Evidence（自填）

| 断言 | 观测 |
|---|---|
| 构建 OK | `CGO_ENABLED=0 go build …` → `exit 0` |
| cmd/agent 13 红 | `grep -c -- '--- FAIL' q1-cmdagent-focused-full.txt` → `13`；`FAIL github.com/the operator/gopi/cmd/agent 18.177s` |
| standards 3 红（聚焦亦红） | job-22：`rc=1`, `grep -c -- '--- FAIL'` → `3`，`FAIL … internal/standards 32.166s` |
| standards 红的落点 | `docclass_gate_test.go:212` 引 `devdoc/cli-help-user-audit.md`（banner `historical` vs `DOC-INDEX.tsv:374` `normative`） |
| checkpoint 全损 | job-19：`rc=137`、`real 4m40.127s`、`user 0m0.002s`、stdout 空 |
| checkpoint 空目录可用 | `rc=0`，输出 `scanned 1 directory(ies) for raw logs; criteria: …` + `no raw logs found under /tmp/cp-empty-dir` |
| 双门 | `gopi web -h` → `rc=0` 且 `gopi tool web -h` → `rc=0`（测试要求后者为唯一门） |
| 规范拼写自拒 | `tool code_search` → `rc=1`；`tool keyword_search` → `rc=0`（两组拼写并存） |
| 语料一致 | `gopi tool doccheck` → `the devdoc corpus is consistent`，`rc=0`；`classify.py . --check` → `0 gap(s)`，`rc=0` |
