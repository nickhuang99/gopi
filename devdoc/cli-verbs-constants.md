<!-- doc-class: historical -->
# Task Report: cmd/agent 顶层 CLI 动词常量源 cliVerbs（Rule 1 SSOT）

Branch: `feat/cli-verbs` · Bug: [hardcoding] cmd/agent 24+ isXxxSubcommand
分派谓词动词字面量裸写，无 CLI 动词常量源 (t-1788906043, severity major)

## Completed

新建 `cmd/agent/cli_verbs.go` —— 顶层 `gopi <verb>` 用户面动词的
单一常量源（Rule 1），17 个常量（CliVerbTask / CliVerbWeb / CliVerbWorker /
CliVerbBrew / CliVerbChat / CliVerbCheckpoint / CliVerbCodeSearch /
CliVerbCompletion / CliVerbLog / CliVerbMemory / CliVerbMemsync /
CliVerbProxy / CliVerbRaw / CliVerbResume / CliVerbShot / CliVerbStats /
CliVerbVersion）。三命名空间边界在包头注释成文：

1. **LLM wire 名**（web_search / web_fetch / vision_analyze / text2img）→
   internal/toolnames（47ca58d 已合并处理，本分支不重碰）；
2. **顶层用户动词**（本文件）—— 工具改名不得改 CLI 词，故独立于 toolnames；
3. **group 子动词**（task list|submit… / memory get|list… / web search|fetch /
   chat list|backfill）→ family 内既有词表/switch，各带漂移守卫测试，
   单一顶层常量会把一个 family 打散到多文件，留在 family 文件。

逐谓词改引常量（19 个文件）：
- 16 个 `isXxxSubcommand` args[0] 裸串比较 → cliVerbs 常量（main.go
  worker 同；web / chat / log / brew / shot / proxy / resume / memsync /
  stats / raw / code-search / completion / version / memory / checkpoint
  双词 switch / memory_sync 的 `args[0] != CliVerbMemory`）。
- cli_tasks.go:389 `args[0] != "task"` → CliVerbTask；ssh-passthrough 词法
  扫描两处 `w == "task"`（413/428）→ CliVerbTask（简报点名的裸散落处）。
- cli_completion.go `topLevelCommandNames` 词表 15 个裸词 → cliVerbs 常量
  （text2img / vision_analyze 两条继续引用 toolnames，与谓词一致）。
- cli_checkpoint.go runCheckpointSubcommand 内 `cmd` 的 case "checkpoint"/
  "resume" → 常量（cmd 从谓词 case 传入，同值贯通）。

契约测试升级（cli_contracts_test.go）：
- `dispatcherOfToken` map 21 个 key 全改引常量（用户动词 → cliVerbs，
  wire 动词 → toolnames）—— 简报要求的"string map key 换常量引用"。
- 新守卫 `TestCLIUserVerbTokensAreAllFromCliVerbs`：双向断言
  dispatcherOfToken 用户动词 key ⊆ cliVerbs、cliVerbs 全量 ⊆
  dispatcherOfToken（wire 动词豁免，归 toolnames 契约管）。
  变异测试实证：从守卫清单删一个动词 → RED（"not a cliVerbs constant"），
  恢复 → GREEN。加谓词/删常量/忘契约 case 均会撞红。

## Not Done（登记豁免）

- **group 子动词**（task/memory/web/chat 二级、checkpoint list|export、
  completion bash|zsh|fish、memory sync）：family 内词表已有守卫
  （memorySubcommandNames↔memoryCompletionNames、task 词表↔dispatch、
  webCompletionNames），留在原处——见 cli_verbs.go 头注释。
- **cli_memory.go:131 `memory_feedback_list`**：memoryLegacyAliases KEY
  之一（merged toolnames 修复 47ca58d 的文档化豁免区）。
- **cli_log.go toolFamilies 过滤词（"memory"/"web" 等）**：log --<family>
  过滤词，47ca58d 已只常量化了与 wire 名相同的 VALUE，KEY 保持字面。
- **-h / --help / version 的 "--version"/"-v" 别名、help/help-env 词**：
  帮助通道标志/别名，非分派动词。

## Verification

- 动词裸串清零：`grep 'args[0] ==\|args[0] !=' cmd/agent/*.go`（非 test）
  无 `== "verb"` 裸词；`== "task|web|…"` 全库非 test 零命中（豁免注释内除外）。
- Build: `go build ./...` OK（全仓）。
- Tests:
  - `go test ./cmd/agent/ -run 'TestCLIContract|TestCompletion'` OK（2.5s）
  - 契约守卫 4 项（含新 TestCLIUserVerbTokensAreAllFromCliVerbs）PASS
  - 变异测试：删 CliVerbShot → RED，恢复 → GREEN
  - 全量 `go test ./cmd/agent/` OK 79.98s
- 冒烟（live 二进制，隔离 HOME）：`web --help` exit 0 + usage 指纹；
  `version` exit 0；`task -h` exit 0 + usage；`completion bash` exit 0；
  `task submit "…"` → "Task queued"（隔离队列，未污染真实队列）。
- Commit: 见 git log。

## 设计权衡（记录，供 PA 复核）

简报建议两案：新建 cliVerbs 常量 **或** 降级 minor + cleared-评估。
本任务选前者并落地为代码修复（非只报不改），理由：(a) sibling 发现
t-1788905960-3（toolnames 镜像）已按"修复而非报告"合并——同一审查 epic 的
另一半同值散落问题不该停在报告层；(b) 契约测试已存在（该面已被认为值得
守护）+ review-cli-* 事故史证明 CLI 文本漂移高发，与"低频事件"评估相悖；
(c) 改引常量的同时把 dispatcherOfToken 从手抄字符串 map 升级为常量引用 +
双向守卫——正是简报"契约测试遍历常量集断言完整覆盖"的落地。
顶层动词归属 cliVerbs 而非并入 toolnames：用户面词 ≠ wire 名（记忆 #1553/
#1556 的命名空间区分结论），并入会造成"工具改名误伤 CLI 词"的错误耦合。
