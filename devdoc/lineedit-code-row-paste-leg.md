<!-- doc-class: historical -->
# Task Report: 代码块的每一行不再带回合 gutter —— 抄下来的就是命令本身

> 任务：gopi chat 回答里可拷贝文本（命令、代码）默认字节干净，无开关；
> 屏幕上"连续回合可分辨"不得回退。
> 日期：2026-10-02 · 分支 `task/01a0fa44c3a97b7682c806330b0f4b89`

## Completed

### 1. 缺陷与受影响面（每条判决 + 判据）

判据统一为一条：**该产物是不是"读者要拿去执行/粘贴的文本"**。是 → 落在拷贝带上，
首格不得有任何非源文本字符；否（纯屏幕装饰，读者不会整行拷去执行）→ 允许装饰。

| 面 | 判决 | 判据与实测 |
|---|---|---|
| 答案 **代码块行**（fence 行、fence 内的代码行、diff 行） | **污染** | 读者的拷贝材料。实测（master，117 列真 pty）：网格第 011/012 行 = `▌ ```bash` / `▌ cd /tmp && go build ./...`，粘贴进 shell 第一行即报错 |
| 答案 **散文行**（普通段落） | 展示装饰，**保留** | `TurnMarker` 存在的理由是一个实测缺陷：相邻两回合的段落粘连、读者分不出边界（见 `answer.go` 中 TurnMarker 注释）。散文不是拷贝材料 |
| 工具活动行（`⚙ bash …`） | 早已干净 | `lineGutter()==""`（`r.noting`），从来就是第 0 列开——本次把代码行对齐到家族的另一半 |
| 屏幕 dump 的 row TEXT（`.dump` / `.dump.json`） | **同一份网格** | dump 是**同一个 cell grid 的回读**（`internal/uiclient`），不是第二份渲染。所以修网格即修 dump：实测 before/after 同一 dump 行 011/012 从 `▌ ```bash` 变为 ` ```bash ` |
| 会话 JSONL（`~/.gopi/sessions/<id>/*.jsonl`） | **不受影响** | 实测本会话 JSONL 中 `▌` 0 次来自答案正文；命中的是 raw.log 里的 SSE 原始流（`thinking_delta`），与渲染无关 |
| `ui_state` grid | 同一份网格 | 与 dump 同源；`ui_state --target chat` 需要活进程，本次未跑（见 Not Done） |

方向选择（三选一，判据优先于口味）：

- **(a) 导出时剥离** —— 否决，因为**根本不存在"导出"**。拷贝就是终端自身的选区读
  cell grid，grid 就是产物；剥离器得插进每一个读者里（操作者的选区、dump 的 row
  TEXT、粘回模型的文本）。
- **(b) gutter 画成不可拷贝** —— 否决，**一个 cell 就是一个字符**。留两个空 cell，
  行首仍是 `  `，而验收标准是**字节精确**（`拷贝文本 == 源命令文本`），选区无法把
  块的保留列和命令自己的前导空格区分开。
- **(c) 代码块行不画 gutter** —— **采纳**。行从第 0 列开始，文本逐字节就是源行。

### 2. 修复（默认生效，无开关、无 env）

- `answerRow.copied` + `answerRow.gutterGlyph()`（`internal/lineedit/answer.go`）：
  code/tool 层的行不画 gutter。散文行保持 glyph。
- 三处赋值点：`rowFor`（按 `layerForLine`）、`rowForPartial`（在 fence 内的在飞行）、
  `writeRow`/`commitLines` 的两处 `writeGutter` 调用加 `row.gutterGlyph()` 门。

### 3. 修复过程中由实测发现的**两个附带缺陷**（各自独立，均由真链路暴露）

- **1. 陈旧 cell 未擦净。** `commitLines` 擦除残留用的是「临时绘制文本**宽度** vs
  决定行宽度」，这只在两者**起始列相同**时成立。代码行从第 0 列起、临时绘制从第 2
  列起，于是 `▌ ```bash` 的尾巴 `sh` 留在网格上——实测输出 ```` ```bashsh ````。
  改为比较**两者各自到达的列**（新增 `rawCol`，`commitPrefix` 快照 `r.liveCol`）。
- **2. 分帧把 fence 判定打散。** `commitLines` 只在片段含粗体标记时才把它与停放的
  前半段重新拼起来。composer 是**有状态**的分类器（fence 开标记会翻转其后所有行的
  类别），于是分三帧到达的 ```` ```bash ```` 让 fence 从未打开，块内行被分类为
  **散文**、继续带 gutter。实测扫描 chunk=1..64：**2..16 全部残留 gutter，1 与 32+
  干净**——即"取决于载体分帧大小"的随机缺陷。改为无条件重拼停放片段。

### 4. 回归测试（修前红 → 修后绿）

`internal/lineedit/copied_text_test.go`（新增）：

- `TestCopiedText_CodeBlockRowsArePasteable` —— **红证**：整块（含 fence 行）与源行
  **逐字节**相等。
- `TestCopiedText_NoRowOfACodeBlockCarriesTheGutterGlyph` —— 块内任何行不得以
  `AnswerGutters()` 中任一 gutter 起头（含跨行折行）。
- `TestCopiedText_StreamedFenceLeavesNoStaleGraphics` —— chunk 1/2/3/5/8/16 全扫，
  既查缺行也查 `▌ ``` ` / `▌ cd /tmp` 残影。
- `TestCopiedText_TurnSeparabilityIsKept` —— **回退守卫**：连续两回合的散文行仍以
  `TurnMarker` 起头，且回合间仍有收尾规则行。

两个既有测试断言的是**缺陷本身**（"代码家族的行按正文被标记"），按新形状更新：
`layers_test.go`、`marker_column_narrow_test.go`（后者的散文列表改用块内行区间排除，
并按块整体回读做 round-trip）。

## Not Done（诚实）

- **`ui_state --target chat` 未跑。** 它需要活进程，实测会阻塞到超时。dump 的 row
  TEXT 与它读的是同一份网格，故证据由 dump 承担；这是**未执行**，不是"通过"。
- **两个预存在的红测试未修**（在**干净 master** 上同样红，见 Verification）：
  `TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow`（BeginTurn 提交 8 行、期望 5）
  与 `TestProbeMeasuresDepthFromRealAnswers`（flaky：3 次跑中 1 次 FAIL，DA1 探测）。
  二者与本改动无关，未纳入本次范围。
- **`TestMarkerColumn_FencedBlockRowsKeepTheColumn` 的语义被改写**（散文列表现在排除
  块内行）。若评审认为该测试的原意图是"块内行也必须开在块列"，那与本任务的验收标准
  直接冲突，需要一次显式的设计裁决——本次按任务的字节精确标准执行。

## Verification

- **Build:** `go build ./...` → OK；`CGO_ENABLED=0 go build -o bin/gopi-fixed ./cmd/agent/` → OK
- **Tests（红证，master 树 + 本次测试文件，`/dev/shm/gopi-red-3668339`）:**
  `go test ./internal/lineedit/ -run TestCopiedText` →
  `--- FAIL: TestCopiedText_CodeBlockRowsArePasteable`，
  `copied_text.go:131: code row 7 = "▌ cd /tmp && go build ./...", want the command verbatim`
  **且** `TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow` 在**同一干净 master 上亦红**
  （`BeginTurn committed 8 row(s), want 5`）→ 证明后者非本次引入。
- **Tests（修后）:** `go test ./internal/lineedit/ -run TestCopiedText` → 4/4 PASS；
  `go test ./internal/lineedit/` → 全包仅剩上述两个**预存在**红项。
- **Ran（Rule 31，真实链路，117×26 pty + 真实 `gopi chat` + SSE stub）:**
  - RED 二进制（master 构建）：
    `ANTHROPIC_BASE_URL=http://127.0.0.1:8791/anthropic ANTHROPIC_AUTH_TOKEN=stub python3 pty_paste_leg.py <master-gopi> before.dump 117 26 …`
  - GREEN 二进制（本次提交构建，md5 `00813b362f793b24d6eec69e5f3c88d4`）：
    `ANTHROPIC_BASE_URL=http://127.0.0.1:8821/anthropic ANTHROPIC_AUTH_TOKEN=stub python3 pty_paste_leg.py bin/gopi-fixed final.dump 117 26 …`
- **Result:** 同一 dump 行号、同一网格：

  | 行 | master（RED） | 本次（GREEN） |
  |---|---|---|
  | 011 | `▌ ```bash` | ` ```bash ` |
  | 012 | `▌ cd /tmp && go build ./...` | `cd /tmp && go build ./...` |
  | 014 | `▌ done.` | `▌ done.`（散文，按设计保留） |

  脚本裁决：RED `verdict=PREFIXED/EXTRA: '▌ cd /tmp && go build ./...'` →
  GREEN `verdict=EXACT`（行 012 文本逐字节等于命令）。
- **Commit:** `7e92b038 fix(lineedit): a code block's rows carry no turn gutter, so a copy of it is the command`

## Evidence (self-filed)

- 红证（master 树，本次测试文件）：`code row 7 = "▌ cd /tmp && go build ./..."` —— 命令与输出见上。
- 预存在红项：在 `/dev/shm/gopi-red-3668339`（`git archive master`，**不含**本次改动）
  上 `go test ./internal/lineedit/ -run TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow`
  → `FAIL … committed 8 row(s), want 5`。
- 分帧矩阵：chunk=1..64 扫描，修前 `chunk=2,3,5,8,16 bad=[STALE ▌ ``` , STALE ▌ cd /tmp]`，
  修后全 `bad=[]`。
- 真链路产物（`~/.gopi/sessions/01a0fa44-c3a9-7b76-82c8-06330b0f4b89/evidence/`）：
  `before.dump`（master）、`after.dump`（本次）、`pty-paste.txt`（裁决 + 整屏）、
  `pty-raw.bin`（pty 全字节）、`stubllm_block.py`、`pty_paste_leg.py`。
- `gofmt -l internal/lineedit/` → 仅 `colour.go`、`paste_envelope_test.go`（均非本次文件，
  改动前即未格式化）。
