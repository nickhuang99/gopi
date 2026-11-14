<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# CLI 重组遗留红 — 实测基线（本分支收口任务的取证）

**性质**：某一次核验跑的记录，不是对 master 的长期主张。故归 `historical`。

**核验点**：worktree `/home/user/workspace/gopi-01a1144e688e717f9380b289fea79691`，
分支 `task/01a1144e688e717f9380b289fea79691`，**HEAD `3255973cc`**（= 当时的 master tip，
`git merge-base --is-ancestor master HEAD` 为真）。
**方法**：全部结论来自真跑 —— `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` 的真实产物
调用 + 真实 `go test`（Rule 31）；无一条来自读代码推断。

## 0 与上一份核验的差异（本文件存在的理由）

上一份核验 `devdoc/cli-reorg-e2e-verification.md` 记 `cmd/agent` **13 FAIL**、`internal/standards` **3 FAIL**。
在 HEAD `3255973cc` 上**重跑同一批聚焦测试名**，实测为 `cmd/agent` **10 条红 / 11 FAIL 行**、
`internal/standards` **1 FAIL**。**树的实际状态已经比那份记录好**，派发后代任务时必须用这里实测的数字，
不能用 13+3。

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → BUILD_OK
$ go test ./cmd/agent/ -count=1 -run '<11 个测试名>' → 10 条红（见 §1）
$ go test ./internal/standards/ -count=1 \
    -run 'TestDocClassBannerMatchesManifest|TestCommandsDocGeneratedBlockMatchesSource|TestCommandsDocGateRedProofStaleGeneratedBlock'
  → 仅 1 FAIL（TestDocClassBannerMatchesManifest）；两条 TestCommandsDoc* 已绿
```

## 1 `cmd/agent` 已转绿的（不要再去修）

| 测试 | 实测 |
|---|---|
| `TestUndispatchedToolWordsNameADispatchingCanonical` | `--- PASS`（0.08s）。上一份记的 8 行「规范拼写自拒」不再是红 |
| `TestKeywordSearchRunPrintsHitsAndNoMatches` 的**兄弟**退役面 | 旧拼写已按名拒绝：`./bin/gopi keyword_search X` → `gopi: 'keyword_search' is RETIRED — use 'gopi tool keyword_search'` |
| `TestSandboxDirFlagDecidesTheRoot` 的**兄弟**缺陷（缺 `tool sandbox create`） | 实测已实现：`./bin/gopi tool sandbox create --label x` → `rc=0`，打印 `sandbox …/sandbox-x-4bc59f65 created=true …` |

## 2 `cmd/agent` 仍然红（10 条）

| # | 测试 | 实测原文（截断） |
|---|---|---|
| 1 | `TestToolGroupWordsAreTheOnlyDoor` | `the group forwards to "web", which this binary ALSO dispatches at top level` / `"web" is refused NOWHERE at argv[0]` |
| 2 | `TestToolGroupForwardsToTheSameImplementation/web` | `` `gopi web -h` exited 0 — the bare spelling must refuse now that the group is its only door `` |
| 3 | `TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation` | `` `gopi tool voice_input` is advertised by the help (voice_input) but the inventory does not list it ``；同型 `--seconds 5` |
| 4 | `TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason` | `` "gopi tool voice_input" … advertised and NEITHER tested NOR declared blocked ``；同型 `--seconds 5` |
| 5 | `TestAnnotateVerbRunsARealArgvEndToEnd` | `-h` 子测红 |
| 6 | `TestChatBoundaryTenMegabyteInputStaysBounded` | `a 10485760-byte input produced a 10875-byte request body — the message grew with the input` |
| 7 | `TestChatStatus_LiveBlockKeepsTheStateWord` | 行内 `resp\u00a0`，断言要求 `responding\u00a0` |
| 8 | `TestKeywordSearchHelpPageDiffersFromTopLevel` | `gopi keyword_search -h: exit error: exit status 1` |
| 9 | `TestKeywordSearchRunPrintsHitsAndNoMatches` | `gopi keyword_search EUAUDIT_NEEDLE: exit error: exit status 1` |
| 10 | `TestSandboxDirFlagDecidesTheRoot` | `gopi tool sandbox ...: exit status 1` |

**#10 的重要更正（真跑证据）**：#1/#2 与 #10 所依赖的**底层缺陷有一半已经不在了** ——
`gopi tool sandbox create` 实测 `rc=0` 并真的建出 sandbox 目录；`gopi web -h` 与 `gopi tool web -h`
**实测都 `rc=0`**（双门仍在）。所以 #1/#2 是**活的真缺陷**（双门），#10 需要重新看它的失败原因
（不再是「`tool` 组不认识 `create`」）。

## 3 `internal/standards` 仍然红（1 条）

```
docclass_gate_test.go:212: devdoc/cli-help-user-audit.md: carries the "historical" banner but
  devdoc/DOC-INDEX.tsv classes it "normative" — only [historical] classes are bannered
docclass_gate_test.go:222: devdoc/DOC-INDEX.tsv: 1166 markdown docs of a bannered class carry
  their banner (1 mislabelled)
```

实测落点：`devdoc/cli-help-user-audit.md:1` 为 `<!-- doc-class: historical -->`，
而 `devdoc/DOC-INDEX.tsv:374` 记 `… cli-help-user-audit.md \t normative \t normative: standing claim …`。
两者矛盾，门按 banner 判定报红。

**注意 `gopi tool doccheck` 与 `python3 tools/docclass/classify.py . --check` 在这条红存在时都返回 0 gap**
—— 它们查的是「行是否存在且格式正确」，不查 banner/class 是否一致；**这条红只有那个 Go 门能看见**。

## 4 验证（Rule 5 / 31）

- **Build**：`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`
- **Tests**：
  - `go test ./cmd/agent/ -count=1 -run '<§2 的 10 个名字>'` → 10 红 / 11 FAIL 行，`FAIL github.com/nick/gopi/cmd/agent 9.142s`
  - `go test ./internal/standards/ -count=1 -run 'TestDocClassBannerMatchesManifest|TestCommandsDoc*'` → 1 FAIL，`FAIL …/internal/standards 4.486s`
- **Ran**（verbatim）：
  - `./bin/gopi tool sandbox create --label x` → `rc=0`，`sandbox … created=true reused=false …`
  - `./bin/gopi web -h` → `rc=0`；`./bin/gopi tool web -h` → `rc=0`（双门）
  - `./bin/gopi keyword_search X` → `rc=1`，`gopi: 'keyword_search' is RETIRED — use 'gopi tool keyword_search' …`
  - `./bin/gopi tool keyword_search -h` → `rc=0`（真门）
  - `./bin/gopi tool annotate -h` → `rc=0`，打印完整 usage 页
  - `gopi tool doccheck` → `rc=0`；`python3 tools/docclass/classify.py . --check` → `0 gap(s)`
