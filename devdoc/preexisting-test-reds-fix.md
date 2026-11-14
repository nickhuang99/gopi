<!-- doc-class: historical -->
# Task Report: master 预存 3 测试失败根因修复（task #288）

## 背景
master/feat 树上 3 个预存失败（干净树 stash 复现过，与 feat/task-id-collision 改动无关）：
1. internal/agent TestAgent_Run_ContextTrimReplaceSlimming
2. internal/agent TestAgent_Run_ToolResultPayloadOnce
3. internal/tools TestEnsureTagsRegeneratesForNewerTS
验收标准：三者转绿，或给出确证的环境性原因并记录。

## 根因（5-why，逐一定位）

### 1+2. 两个 agent Run 级测试 —— bash 子进程 locale 泄漏
- 测试链路：mock LLM 让 agent 真执行 `ls /nonexistent` → tools.BashInDir →
  断言 wire 上有 "cannot access '/nonexistent'"（bash 失败输出）。
- 5-why：断言找不到英文 → wire 上是中文「无法访问」→ coreutils 按宿主
  locale 本地化 → bash 子进程 exec.CommandContext 未设 cmd.Env，原样继承
  宿主 LANG=zh_CN.UTF-8 → go test 的测试进程本身继承宿主环境。
- 实证探针（最小样本）：独立 Go 程序同一命令，
  继承环境 → `无法访问`；显式 LC_ALL=C → `cannot access`。
- 与 memory #1152 的归因吻合（locale 钉扎），但该记录描述的
  testmain_test.go/TestMain 修复在代码中不存在（未落地或被丢弃）——本次
  以工具层（而非测试层）修复，覆盖所有调用方。

### 3. TestEnsureTagsRegeneratesForNewerTS —— exuberant-ctags 5.9 无 TS
- 本机 ctags = Exuberant 5.9~svn20110310：无 TypeScript/TypeScriptReact
  解析器、无 --map-*（--langmap 才是它的拼写）。
- ctagsCapabilities 探测正确过滤了未知语言、mapArgs 为空 → .ts/.tsx
  静默不进索引 → regenerate 后 tags 里没有 app.ts/comp.tsx 条目。
  （universal-ctags 的机器上本测试本来就绿——环境性预存红。）
- 5-why 到机制层：探测过滤只是"不崩"，不是"仍覆盖"——降级路径缺失。

## 修复（internal/tools，一处定义）

### bash.go — bashChildEnv() locale 钉扎
- bash 子进程统一 `cmd.Env = bashChildEnv()`：继承父环境但覆盖
  LANG/LC_ALL=C（并剔除 LANGUAGE/LC_MESSAGES 干扰项）。
- GOPI_BASH_LOCALE 可覆盖默认（Rule 2：唯一 env 读取点）。
- 选择工具层而非 TestMain 钉扎：生产 session 在 zh locale 宿主上同样会
  把本地化错误文本写进 context/session log，工具层修复覆盖所有调用方。

### symbol.go — exuberant langmap 降级
- ctagsCaps 新增 langmapArg；--map-* 探测失败时回退
  `--langmap=JavaScript:.js.cjs.mjs.ts.tsx`（实测 5.9 接受点分扩展名表，
  .ts/.tsx 内 function/var 可产出 tag，language: 字段标 JavaScript——
  文档化的降级而非静默缺口）。
- ensureTags 拼装处：langmapArg 非空则追加 `--langmap=`。

### symbol_test.go — fixture 稳定化
- `interface Shape {}` 在 exuberant 的 JS 解析器（kinds: f/c/m/p/v）下
  不产出任何 tag（它不认识 interface 语法）；且 `const NAME = 1` 单独
  也不产出（v kind 需 var）。fixture 改为 function tag（两种 ctags 变体
  都识别）——断言语义（.ts 必须进索引）不变。

## 回归测试
- TestBashChildLocalePinnedToC（bash_test.go）：`ls /nonexistent-path...`
  必须含英文诊断、不得含本地化文本；宿主 LANG=zh_CN.UTF-8 下直接复现原
  症状并通过。
- TestEnsureTagsRegeneratesForNewerTS 本身即为回归断言（恢复全绿）。

## 附带收尾
- 会话遗留的 M4.12 repo-guard 暂存改动（stash 恢复）：
  tasks_repo_guard_test.go 原先 seed 进共享默认库，留下 UNIQUE(task_id)
  地雷（t-repoguard-* 残留行让后续每轮必失败）——改为 t.TempDir() 隔离
  库 + GOPI_MEMORY_DB env 指向，清理了残留行。独立提交 ab13e71。

## Not Done
- universal-ctags 机器上 langmap 降级路径无真实二进制可测（探测分支不
  触发）；行为由 exuberant 实测覆盖，universal 主路径不受影响。
- TestMemorySearchNonBlocking 偶发（负载敏感首开竞态，memory #1152 已
  立案，memInitMu 修复不在本分支）——本轮全量 3 次中偶发 1 次，单跑
  与复跑均绿，与本 Epic 无关。

## Verification
- Build: `go build ./...` OK；gofmt 干净（改动文件）；go vet tools/agent 干净
- Tests: `go test ./internal/agent ./internal/tools ./cmd/agent -count=1`
  连续 3 轮全绿（之前 agent 包 2 FAIL / tools 1 FAIL）
  - 3 个目标测试 -count=1 单独跑全 PASS
- Commits（feat/task-id-collision，共享对象库）：
  - b17d654 fix(tools): close 3 pre-existing master test reds — bash locale pin + exuberant ctags langmap
  - ab13e71 feat(tasks): M4.12 repo guardrail（遗留暂存收尾）
