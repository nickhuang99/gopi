<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# CLI 工具组门收口 — 在基线 3255973cc 上落定「哪一侧对」

**任务**：`01a11461-db0b-7d30-b6ae-1995483c5bd3`（本 worktree `/home/user/workspace/gopi-01a11461db0b7d30b6ae1995483c5bd3`）
**基线**：`3255973cc`（本分支基点；master 三点相同）
**结论 commit**：`4fda65dcf`（`git cherry-pick -x 6a5ee1cee`，带 `(cherry picked from commit 6a5ee1ce…)` trailer）

## 0 判定摘要（先给结论）

「工具组（`gopi tool`）是 local tools 的唯一门」**是设计契约，但带一个具名例外**。三条红的定性如下，全部来自真跑（Rule 31），无一条来自读代码推断：

| # | 红 | 定性 | 依据 |
|---|---|---|---|
| 1 | `TestToolGroupWordsAreTheOnlyDoor` | **真缺陷，但缺的是「声明」不是「门」** | `toolSubcommandPredicates["web"]` **就是** `isWebSubcommand`（`cmd/agent/cli_tool.go:251`），而 `cmd/agent/main.go:553` 的顶层派发链调用的正是同一个谓词 —— 两门是**一个实现的两个拼写**，不是两个命令 |
| 2 | `TestToolGroupForwardsToTheSameImplementation/web` | 同上；断言在 `web` 上**过时**（它断言「裸词必须被拒」，而裸词是活的浏览器聊天服务） | 实测两门输出**逐字节相同**（md5 `b1c13957850f252763e35131f0bb42a0`） |
| 3 | `TestSandboxDirFlagDecidesTheRoot` | **测试探针缺陷，产品无缺陷** | 基线实测 `gopi tool sandbox -h` **rc=0 且已列出 create**，`gopi tool sandbox create --sandbox-dir <scratch> --label x` **rc=0 并打出真实路径**；失败由探针 argv 自造 |

**「web 双门哪一侧对」的答案：两侧都对。** 缺的是**声明**。两侧的关闭方案都实测过且都被否决：

- 让组成为唯一门（拒掉 `gopi web`）＝ **撤掉浏览器聊天 UI 服务**（`--addr`/`--tools`/`--thinking-live`、LAN token banner 是一张活的、有自己 help/contract inventory/测试的页面）。这不是重命名，是删除一个仍在服务中的命令 —— 正是 one-door 规则存在的理由「命令躲在自己的拒绝后面」那个缺陷。本 epic 未授权，也不该由一个收口任务顺手做掉。
- 把 `web` 当普通行留在表里而不声明 ＝ 正是被报告抓到的那件事：`gopi tool -h` 广告了一行，而那行的顶层拼写既不被拒也不被调和，读者拿到两条互相矛盾的线索。

**唯一站得住的第三形态**：例外**只在一处声明**、在**提出这个问题的页面上打印出来**、并由**断言真事的测试**钉住 —— 而不是把通例断言放松。

### 0.1 实测 one-door 矩阵（`bin/gopi`，本 worktree 构建，19 个组词全跑，不是抽样）

`gopi <word> -h` / `gopi tool <word> -h` 的 rc，词表由 `gopi tool -h` 页面本身派生：

```
WORD               TOP    GROUP        WORD               TOP    GROUP
act                1      0            shot               1      0
annotate           1      0            text2img           1      0
capture            1      0            text2img-verify    1      0
code-search        1      0            vision_analyze     1      0
doccheck           1      0            voice_input        1      0
gif                1      0            keyword_search     1      0
ledger-window      1      0            proxy              1      0
sandbox            1      0            scratch-gc         1      0
screen_capture     1      0            web                0      0   ← 唯一的例外
```

18 个词满足 one-door 契约（顶层按名拒绝 rc=1、组内可达 rc=0）；`web` 是唯一两侧都 rc=0 的**行**，也正是被声明为第二门的那个词。

矩阵里出现的 `only` 一行是页面散文（"the group is the only door …"）被粗派生器扫进词表的噪声，非组词。**并且 `TOP=0` 本身不构成「顶层派发」的证据** —— 实测一个对照的非命令词同样 rc=0：

```
$ gopi only -h                       → rc=0（顶层总览页）
$ gopi xyzzy-not-a-command -h        → rc=0（同一页 —— 对照）
$ gopi web -h                        → rc=0（gopi web — serve a local browser chat UI …，另一页）
```

所以矩阵只说明「哪些词在顶层**按名拒绝**」（rc=1，18 个），不说明 `web` 之外还有谁在顶层活着。`web` 构成第二门的两条独立证据是：(a) 它打印的是**另一张页**（上面的对照区分了「顶层总览页」与「聊天 UI 页」），(b) `cmd/agent/main.go:553` 的项链调用 `isWebSubcommand`，而 `toolSubcommandPredicates["web"]` 是**同一个谓词**。矩阵与对照均为本次真跑（`<session>/round2/one-door-matrix.txt`、`<session>/round2/discriminator.txt`）。

## 1 Completed

1. **复核兄弟行的判定，并在本基线上承接它**。兄弟行 `01a1144e-17f1-7219-a9cc-786ed8246982` 在 peer1 master 上状态为 **`cancelled`**（不是本行简报所记的 `completed`），其修复 `6a5ee1cee` 确实并入 `3ff9ed361`。其判定的每一句都在**本 worktree 的本基线上重测过**（§0 表 + §2），全部成立，因此**不造第二个修**。
2. **落地**：`git cherry-pick -x 6a5ee1cee` → `4fda65dcf`。先验证「干净承接」：该 commit 触碰的 4 个文件在本 HEAD 与 `6a5ee1cee^` 之间 `git diff --stat` **为空**（上下文完全一致），所以是无冲突的承接，不是二次实现。
3. **三条红转绿**（基线 3 FAIL → 修复后 0 FAIL，`-v` 输出见 §2）。
4. **真跑取证**（Rule 31）：`gopi web -h` / `gopi tool web -h` / `gopi tool -h` / `gopi tool sandbox -h` / `gopi tool sandbox create --sandbox-dir` 全部用**本分支新建的 `bin/gopi`** 在**隔离 profile**（`GOPI_HOME=<session>/round2/home`）里跑过，rc + 输出见 §2。
5. **补上承接的另一半**：`406e24860` 用 gate 自己点名的生成器重生成 `COMMANDS.md`（`cli_tool.go:374 → :431`），把这次承接**自带的**两条 `TestCommandsDoc*` 红修成绿（§4 #4）。只 cherry-pick 代码会把这个 diff 变成「必须两半一起落地」的原子单元 —— 上游 master 正是因为只承接了一半而仍红。

## 2 Verification

**Build**

```
$ CGO_ENABLED=0 go build -o <session>/round2/gopi ./cmd/agent/
BUILD OK (post-fix)
（基线同法构建 <session>/round1/gopi-base，亦 exit 0）
```

**Tests（聚焦复现，`-count=1 -v`）**

基线（`3255973cc` + 无修复）—— 证据 `<session>/round1/base-red.txt`：

```
--- FAIL: TestSandboxDirFlagDecidesTheRoot (5.97s)
    cli_sandbox_ledger_test.go:256: gopi tool sandbox create --sandbox-dir …: exit status 1
        gopi tool: unknown command "create" — this group knows: act|annotate|capture|code-search|…
--- FAIL: TestToolGroupWordsAreTheOnlyDoor (0.00s)
    cli_tool_test.go:118: the group forwards to "web", which this binary ALSO dispatches at top level — …
    cli_tool_test.go:121: the group forwards to "web", but "web" is refused NOWHERE at argv[0] — …
--- FAIL: TestToolGroupForwardsToTheSameImplementation (0.38s)
    cli_tool_test.go:153: `gopi web -h` exited 0 — the bare spelling must refuse now that the group is its only door
FAIL	github.com/the operator/gopi/cmd/agent	8.115s   EXIT=1
```

修复后（`4fda65dcf`）—— 证据 `<session>/round2/green-focused.txt`：
```
--- PASS: TestSandboxDirFlagDecidesTheRoot (5.36s)
--- PASS: TestToolGroupVocabularyMatchesHelp (0.00s)
--- PASS: TestToolGroupWordsAreTheOnlyDoor (0.00s)
--- PASS: TestToolGroupForwardsToTheSameImplementation (0.42s)   [19/19 子测试，含 /web]
--- PASS: TestToolGroupDeclaredSecondDoorIsPrintedAndPinned (0.03s)
ok  	github.com/the operator/gopi/cmd/agent	8.652s   EXIT=0
```

**Ran（Rule 31：每条改动面的真实调用，隔离 profile，verbatim）**

| 命令（verbatim） | rc | 观察到的输出/状态 |
|---|---|---|
| `GOPI_HOME=<iso> <bin> web -h` | **0** | 聊天 UI 页；md5 `b1c13957850f252763e35131f0bb42a0` |
| `GOPI_HOME=<iso> <bin> tool web -h` | **0** | **同一页**，md5 相同（`md5sum` 两文件逐字节相等） |
| `GOPI_HOME=<iso> <bin> tool -h` | **0** | 含 `ONE DECLARED EXCEPTION` 段落（`grep -c` = 1，基线 = 0） |
| `GOPI_HOME=<iso> <bin> tool sandbox -h` | **0** | Usage 段列出 `create` / `run` / `show` 三行 |
| `GOPI_HOME=<iso> <bin> tool sandbox create --sandbox-dir <iso>/sbx --label flag-root` | **0** | 末行 = `<iso>/sbx/sandbox-flag-root-432507af`（真实路径，落在 `--sandbox-dir` 下）；摘要行 `created=true reused=false root=… source=from --sandbox-dir` |

**Result（真跑的直接后果，不是推断）**

- `--sandbox-dir` 真的赢：沙箱落在 flag 根下，`source=from --sandbox-dir` 明说了哪条通道决定的（`config.ResolveSandboxRoot` 的优先级被真跑确认）。
- 两个门真的是一次实现：两文件 md5 相等是**逐字节**证据，比原断言「裸词必须被拒」更强 —— 通例规则保护的是「一个实现」，这个断言在例外词上直接测那个东西。
- `gopi tool -h` 真的印出理由：`grep -c "ONE DECLARED EXCEPTION"` 基线 `0` → 修复后 `1`。

## 3 Not Done（honest）

1. **本分支不含全部 cmd/agent 稳定红**。本任务范围只是「tool 组门这一族」三条。`cmd/agent` 全量里**仍有红**（见 §4.1 的实测清单），其中若干属 CLI 重构族，由兄弟/后续任务收口。本分支**没有**去碰它们。
2. **§4 的发现没有在本分支修**，只做了点名（Rule 26：另派新行，不顺手大改）。其中 §4.2 的两条**在 peer1 master `3ff9ed361` 上实测同样红**，所以不是本分支的遗漏，是本族之外的既有缺陷；§4.1 的逐条归因见该节的**证据强度声明**。
3. **`devdoc/cli-reorg-red-baseline-20261007.md` 在本基线与本分支都不存在**（本行简报引用过它）。本报告 §2 的基线数字是**自己真跑采的**，不依赖那份文件（Rule 5 的「gate 结果必须是本次跑观察到的」）。
4. **未做整树测试**（Rule 27）：范围是 `cmd/agent` + `internal/standards`。
5. **全量按名集合差（`round3/fail-diff.txt`）没有完成** —— 起它的后台跑在写文件前被终止，`round3/baseline-full.txt` 与 `round3/mine-full.txt` 都是 0 字节。**已完成**的对照是**同一份 `-run` 名单**在纯净快照与本分支上的各自真跑（§4 #2：9 条逐条同名、条数相同），以及 §4 #4 的 standards 聚焦对照。因此本报告对「哪些红由本 diff 引进」的结论**只到这 9 条 + standards 那两条**，不覆盖 `cmd/agent` 的全量集合。
6. **声明式入口 `make test-budget PKGS='./cmd/agent ./internal/standards'` 起了两次、两次都在写结论前被终止**（该 host 实测 25m/包 的预算，两次都没跑完，`round3/test-budget.txt` 只留下 `packages:`/`budget:`/`host:` 表头）。被它拦掉的是**同类红的全量集合**，不是本 diff 的任何一条断言 —— 上面那些聚焦跑与快照对照都完成并落盘了。**不**据此声称「包级预算已通过」。

## 4 事实性修正与点名（发现，未修）

**#1 `gopi tool <wire_name> -h` 的拒绝是死胡同（Rule 15），且活到 master。** 本基线与 peer1 master `3ff9ed361` **都**如此：

```
$ gopi tool code_search -h ; echo rc=$?
gopi tool: unknown command "code_search" — this group knows: act|…|code-search|…
Run `gopi tool -h` for what each one does (or `gopi code_search` for the command's own page).
rc=1
$ gopi code_search -h        # ← 上一句建议的那个地址
gopi: "code_search" is not a gopi command — this one is spelled `gopi tool code-search` …
```

拒绝把 `argv[1]` 原样回显成 `gopi <word>`，而那个拼写恰恰**在另一条 arm 里被拒** —— 建议的地址不是活门，是另一次拒绝。正确地址已被**同一条消息的上一句**写出来了（`gopi tool code-search -h`），所以这是「有正确答案却回显输入」的缺陷，不是缺信息。

- 注：**`keyword_search` 是部分反例** —— 它既是**具名**的 underscore 例外（`devdoc/cli-naming-convention.md` 的例外表，`toolnames.ToolKeywordSearch`），所以 `gopi tool keyword_search -h` rc=0；但消息里的 `(or \`gopi keyword_search\` …)` 那一半仍指向 `gopi keyword_search` 这个顶层裸词。逐词规则不是「下划线一律」也不是「一律被拒」。
- **根因**：`cmd/agent/cli_tool.go:84` 的 hint 模板 `(or \`gopi %s\` for the command's own page)` 拿到的 `%s` 是**被拒的拼写**，而检索/规范化的单一真源（内存里记录的 `cmd/agent/cli_tool_spelling_table.go` + `toolRefusalPageWord`）**不在本分支，也不在 master**（`grep -rln "toolRefusalPageWord" . → 空`），其分支 `task/01a1144e3f6e7b05b42b0ca941f0f78b` 的修复未被合并（该行在 peer1 上状态 `cancelled`）。可引用配方：memory #8407（CLI 拼写表单一真源 + 拒绝地址可执行 rc 门禁）。
- 建议：另派一行，简报要点 = 拒绝地址必须**真跑得到 rc=0**（Rule 31 的自洽判据），并带最小回归测试。
- **已派**：`01a11494-9f6f-7591-86bc-63eaaf18e154`（peer1，`tags=bug`，`Task queued:` 已回读确认）。本行**不做**它，只点名（Rule 26）。

**#2 §4.1 本分支 `cmd/agent` 全量仍有 9 条红（本任务范围之外）。** 全部**在本分支实测为红**，与本次 diff 的因果关系按下面的「证据强度」读：

```
$ go test ./cmd/agent/ -count=1 -run '<this list>'     （本分支，真跑）
--- FAIL: TestAnnotateVerbRunsARealArgvEndToEnd
--- FAIL: TestChatBoundaryTenMegabyteInputStaysBounded
--- FAIL: TestChatStatus_LiveBlockKeepsTheStateWord
--- FAIL: TestKeywordSearchHelpPageDiffersFromTopLevel
--- FAIL: TestKeywordSearchRunPrintsHitsAndNoMatches
--- FAIL: TestMergePromptCoversEveryRequiredSection
--- FAIL: TestTaskMergeCanonicalFlagRefusalOnTheRealBinary
--- FAIL: TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation
--- FAIL: TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason
FAIL	github.com/the operator/gopi/cmd/agent	26.358s   MINE_EXIT=1
（证据 round3/suspects-mine.txt）
```

**证据强度（先读这一句）**：这 9 条**在纯净的 3255973cc 快照上同样红** —— 用 `git archive 3255973cc` 展开的独立快照（`<session>/baseline-archive/`）跑**同一份** `-run` 名单，结果逐条同名、条数相同：

```
$ cd <session>/baseline-archive && go test ./cmd/agent/ -count=1 -run '<同一名单>'
--- FAIL: TestChatBoundaryTenMegabyteInputStaysBounded (0.93s)
--- FAIL: TestChatStatus_LiveBlockKeepsTheStateWord (0.00s)
--- FAIL: TestAnnotateVerbRunsARealArgvEndToEnd (0.08s)
--- FAIL: TestKeywordSearchHelpPageDiffersFromTopLevel (7.18s)
--- FAIL: TestKeywordSearchRunPrintsHitsAndNoMatches (0.01s)
--- FAIL: TestTaskMergeCanonicalFlagRefusalOnTheRealBinary (0.23s)
--- FAIL: TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation (0.00s)
--- FAIL: TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason (0.00s)
--- FAIL: TestMergePromptCoversEveryRequiredSection (0.00s)
FAIL	github.com/the operator/gopi/cmd/agent	34.172s   BASE_EXIT=1
（证据 round3/suspects-base.txt）
```

**两侧逐条同名、条数相同（9 = 9）⇒ 这 9 条没有一条是由本次 diff 引进的。** 注意这正是**先证明再断言**的价值：`TestKeywordSearchHelpPageDiffersFromTopLevel` / `...RunPrintsHitsAndNoMatches` 的定义域正是「组的门 vs 顶层」，与本次 diff 同族，**看名字一定会怀疑是本 diff 造成的** —— 真跑对照把它们排除了（基线 7.18s 与分支 1.65s 是同一类的 CLI 探测超时/拒绝差异，两侧都红）。

边界：这条对照用**同一份 `-run` 名单**，因此它证明的是「这 9 条不是新增红」；它**不**证明两侧的全量红集合相同（那次全量按名求差的后台跑被终止，`round3/baseline-full.txt` / `round3/mine-full.txt` 均为 0 字节 —— 见 §3.5）。

**#3 更新版复核未跑通（方法学，诚实记录）。** 本机无法从 peer1 读到本 cherry-pick 的目标分支（`git ls-remote peer1 | grep 01a11461` 无输出；`ssh peer1 'gopi task list'` 也不含本行 —— 本 as-of 时刻两机账本尚未同步本行）。因此「master 上已有一版修复」**未经真跑确认**，只确认了：本分支的 `4fda65dcf` 与上游 `6a5ee1cee` 同源（`-x` trailer）。若 master 后来已有同源版本，本分支的合并在内容层会退化/no-op —— 这是**预期行为，不是缺陷**。

**#4 peer1 master `3ff9ed361` 上 `internal/standards` 仍 3 红，且成因已定位。** 在 peer1 上真跑（`ssh peer1 … go test ./internal/standards/ -run 'TestCommandsDoc|TestDocClassBannerMatchesManifest'`）：

```
--- FAIL: TestCommandsDocGeneratedBlockMatchesSource (0.72s)
    COMMANDS.md's generated verb index disagrees with the source (44 verbs).
      first difference:
        got:  line 38: | `gopi tool` | argv[0] | `cmd/agent/cli_tool.go:374` |
        want: line 38: | `gopi tool` | argv[0] | `cmd/agent/cli_tool.go:431` |
--- FAIL: TestCommandsDocGateRedProofStaleGeneratedBlock (0.71s)
    precondition: the committed document's block is already stale
--- FAIL: TestDocClassBannerMatchesManifest (0.02s)
    devdoc/cli-help-user-audit.md: carries the "historical" banner but devdoc/DOC-INDEX.tsv classes it "normative"
（FAIL github.com/the operator/gopi/internal/standards 5.438s）
```

且**本基线（`3255973cc`，`toolUsage` 在 `cli_tool.go:258`）的 `internal/standards` 只有 1 条 `TestDocClassBannerMatchesManifest` 红**，`TestCommandsDoc*` 两条是**绿的** —— 真跑于 `git archive 3255973cc` 展开的纯净快照（`<session>/baseline-archive/`），证据 `round3/baseline-standards.txt`：

```
$ go test ./internal/standards/ -count=1 -run 'TestCommandsDoc|TestDocClassBannerMatchesManifest'
--- FAIL: TestDocClassBannerMatchesManifest (0.05s)
    docclass_gate_test.go:212: devdoc/cli-help-user-audit.md: carries the "historical" banner but devdoc/DOC-INDEX.tsv classes it "normative" …
    docclass_gate_test.go:222: devdoc/DOC-INDEX.tsv: 1166 markdown docs of a bannered class carry their banner (1 mislabelled)
FAIL	github.com/the operator/gopi/internal/standards	10.327s
（`TestCommandsDoc*` 未出现在 FAIL 行中 = 基线绿）
```

- **`TestCommandsDoc*` 的成因已被证实是那条「落错行」的提交**：生成块记的是 `cmd/agent/cli_tool.go:374`，而 374 是**`3255973cc` 上** `isToolSubcommand` 之前的行号（`3255973cc` 上 `toolUsage` 在 258、predicate 更靠前）；`6a5ee1cee` 在 `cli_tool.go` 顶部插入了第二道门那 57 行，把 predicate 推到 **431**，而该 commit **没有同 commit 重新生成 `COMMANDS.md`**。于是「代码动了、生成块没动」= 这两条红。判据不是猜的：`--check` 与 gate 在**两条独立路径**上给出同一个 delta（分支上 `go run internal/standards/testdata/commands-census.go . COMMANDS.md --check` → `commands-census: COMMANDS.md disagrees with the source`，exit 1；master 上 gate 报同一对 374/431），且**基线绿 / 上游红**的对照把成因锁在这次修改上。
- **`TestDocClassBannerMatchesManifest` 是另一族**（`devdoc/cli-help-user-audit.md` banner=historical vs 索引记 normative），**基线上就已红**，与本 diff 无关；peer1 上它已有一条 pending 行 `01a1144e-8981`（`[queue-skip]`，parent `01a1144e-688e` 已 `cancelled` ⇒ 该行**永远不会被派发**，Rule 33）。
- **处理**：**本分支已随带修好**。`6a5ee1cee` 把 `isToolSubcommand` 推到 `cli_tool.go:431` 却没在同一 commit 重生成 `COMMANDS.md`，所以我这条承接形成了一个**必须一起落地的原子单元**：只 cherry-pick 代码会留下 `TestCommandsDocGeneratedBlockMatchesSource` + `TestCommandsDocGateRedProofStaleGeneratedBlock` 两条红（peer1 master `3ff9ed361` 上实测仍红，正因为它只承接了那一半）。本分支补的是另一半 —— 用 gate 自己点名的生成器重生成、**不手改**（Rule 38）：

```
$ go run internal/standards/testdata/commands-census.go . COMMANDS.md
commands-census: wrote COMMANDS.md (44 verbs)          # 唯一变更：cli_tool.go:374 -> :431
$ go test ./internal/standards/ -count=1 -run 'TestCommandsDoc'
ok  	github.com/the operator/gopi/internal/standards	11.910s   # 两条红 -> 绿
```

  证据：commit `406e24860`（`git show --stat` = `COMMANDS.md | 2 +-`，一行改），`round3/commands-census-gen.txt` 的生成回读。**之所以能这么确定是「补另一半」而不是「顺手改」**：`3255973cc` 上这两条是**绿**的（`round3/baseline-standards.txt`：该名单里只冒出 `TestDocClassBannerMatchesManifest` 一条 FAIL），变红只发生在插入 57 行的那一刻 —— 是本 diff 自身的义务，不是既有缺陷。若上游已有同源重生成，本分支的合并在内容层退化/no-op，仍无冲突。
- **`TestDocClassBannerMatchesManifest` 是另一族**（`devdoc/cli-help-user-audit.md` banner=historical vs 索引记 normative），**基线上就已红**，与本 diff 无关；peer1 上它已有一条 pending 行 `01a1144e-8981`（`[queue-skip]`，parent `01a1144e-688e` 已 `cancelled` ⇒ 该行**永远不会被派发**，Rule 33）。**不并入本分支**：它与 tool 组门无关，且会改动 1166 篇 bannered doc 的语料面，属独立审阅的问题。**本报告只点名、不派新行**（peer1 `01a112ce-0fd7` 已逐字覆盖同一族：stale COMMANDS.md census + DOC-INDEX banner 不匹配；其 parent `01a10efc-760d` 亦为 `cancelled`）。

## 5 Evidence (self-filed)

每条 gate 断言都带自己的观察。全部证据文件在 `~/.gopi/sessions/01a11461-db0b-7d30-b6ae-1995483c5bd3/`：

| 断言 | 命令 | 本次观察到的 |
|---|---|---|
| 基线三条红、且失败原因逐字如此 | `go test ./cmd/agent/ -count=1 -v -run '<三条>'` | `round1/base-red.txt`：3×`--- FAIL`，末行 `EXIT=1` |
| 基线红名单**不含**「组缺 create」 | 同上 base-red 里的 `unknown command "create"` 行 | 该消息是 `gopi tool create …` 的组拒绝 —— 即探针 argv 自造；同基线的 `gopi tool sandbox -h` rc=0 且列出 create |
| 修复后三条绿 | 同命令，`4fda65dcf` | `round2/green-focused.txt`：5 个测试 + 19 子测试全部 `--- PASS`，`EXIT=0` |
| 两门同一实现 | `GOPI_HOME=<iso> <bin> web -h` / `… tool web -h` + `md5sum` | `round2/cli-probes.txt`：rc=0/rc=0，两文件 md5 均 `b1c13957850f252763e35131f0bb42a0` |
| 页面上真有声明 | `grep -c "ONE DECLARED EXCEPTION" round2/tool-h.txt` | `1`（基线 `round1/cli-probes.txt` 同计数 = `0`） |
| one-door 矩阵 | 19 词逐个 `gopi <w> -h` / `gopi tool <w> -h` | `round2/one-door-matrix.txt`：18 词 `1/0`，`web` `0/0` |
| `web` 的第二门是真的（不是 rc=0 的巧合） | `gopi web -h` vs `gopi xyzzy-not-a-command -h` | `round2/discriminator.txt`：前者打印 `gopi web — serve a local browser chat UI …`，后者打印顶层总览页 —— 两个不同的页，故 `web` 是**派发**而非落到总览 |
| `--sandbox-dir` 真赢 | `gopi tool sandbox create --sandbox-dir <iso>/sbx --label flag-root` | rc=0，末行 `<iso>/sbx/sandbox-flag-root-432507af`，`created=true reused=false source=from --sandbox-dir` |
| 死胡同（#1）在 master 上也在 | `ssh peer1 'gopi tool code_search -h'` + `gopi code_search -h` | `round2/wire-name-refusal.txt`（本机）+ §4 #1 的 peer1 真跑输出 |
| **9 条红不是本 diff 引进的** | 同一份 `-run` 名单，在 `git archive 3255973cc` 快照与本 worktree 上各跑一次 | `round3/suspects-base.txt` 与 `round3/suspects-mine.txt`：**9 = 9，逐条同名**（含两条名字很像本 diff 造成的 keyword_search 红） |
| **standards：基线 1 红 / 上游 3 红** | `go test ./internal/standards/ -count=1 -run 'TestCommandsDoc\|TestDocClassBannerMatchesManifest'`，在快照上跑；master 上同名单跑 | `round3/baseline-standards.txt`：快照只冒出 `TestDocClassBannerMatchesManifest` 一条 FAIL（`TestCommandsDoc*` 绿）；peer1 master 上三条全红（§4 #4） |
| **生成块与本源自洽** | `go run internal/standards/testdata/commands-census.go . COMMANDS.md` → `go test ./internal/standards/ -count=1 -run 'TestCommandsDoc'` | `round3/commands-census-gen.txt`：`wrote COMMANDS.md (44 verbs)`，唯一变更 `cli_tool.go:374 → :431`，随后 `ok … 11.910s` |
| 语料归类一致 | `gopi tool doccheck`；`python3 tools/docclass/classify.py . --check` | `consistent — every file under devdoc/ has a well-formed row`（rc=0）；`0 gap(s) …`（rc=0），均在最后一次 commit 后真跑 |
| 语料归类一致 | `gopi tool doccheck` | 见 §6 |

## 6 语料归类（Rule 36）

本报告由 `write` 落盘时经写入钩子**当场归类**，同 commit 内写入 `devdoc/DOC-INDEX.tsv`（`DOC-INDEX.tsv` 是数据，**从未手改**）：

```
devdoc/cli-tool-group-door-closure.md
  → normative: standing claim about master that names code that resolves (anchor);
    standing 4 / record 0
    anchor cmd/agent/cli_tool.go:251 at const toolGroupSecondDoorWord = CliVerbWeb
```

分类器给 `normative` 而非 `historical` 是对的：本报告的主体是对 master 的**长期主张**（one-door 契约 + 一个具名例外），并且锚定了能解析的代码（`toolGroupSecondDoorWord` 常量所在行）；§2 的基线数字是**该行自身的记录**（record），与主张分列。

复核：`gopi tool doccheck` → `the devdoc corpus is consistent — every file under devdoc/ has a well-formed row`（rc=0）；`python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv`（rc=0），两者均在本 commit 落盘后真跑。

## 7 交付位置（Rule 6d）

- 分支：`task/01a11461db0b7d30b6ae1995483c5bd3`（本 worktree，commit `4fda65dcf`）
- 取回：`git show task/01a11461db0b7d30b6ae1995483c5bd3:devdoc/cli-tool-group-door-closure.md`
- 与上游同源：`git log --format='%(trailers)' -1 4fda65dcf` → `cherry picked from commit 6a5ee1ceeca48d164678adcdaa792f5d75d499fb`
