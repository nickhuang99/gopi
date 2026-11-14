<!-- doc-class: historical -->

# gopi 自身能力盘点 A —— 全 help 面枚举 + 源码交叉验证（工具/能力层）

阶段 2A 的交付物。用途：阶段 3 做逐家对比时，这张表就是"我方"那一列。
框架（D01–D17 维度定义、评分口径、证据规范）来自阶段 0 任务
`01a1aa6c-703d-7059-8e6a-4a6b5dced12c`，其产物为
`~/.gopi/sessions/01a1aa6c-703d-7059-8e6a-4a6b5dced12c/artifacts/dims.txt`
（16 行：D01 install … D16 orchestrate；D17 在该文件里未定义，故本片按 15 维工作，
另在 §5 说明）。

**唯一权威入口是 `gopi <verb> -h` 的原始输出**，源码只用来验证"help 上写的背后到底有没有实现"。

---

## §1 枚举方法与计数（可复算）

### 1.1 二进制与它对应的提交

| 项 | 值 |
|---|---|
| 构建命令 | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` |
| `gopi --version` 的 `revision` | `eafec12d6bfdd70df50d17644a3aec55d6a8ee06` |
| 本 ground 的 `git rev-parse HEAD` | `eafec12d6bfdd70df50d17644a3aec55d6a8ee06` |
| 版本 / 平台 | `v0.4.0` / `linux/amd64` / `go1.24.0` |

**两个 revision 相等**是这份文档可以被复核的前提：文件里每一条 `-h` 都是这棵树产出的二进制打的。
（环境里还有 `/usr/local/bin/gopi`，它是指向 `~/work/gopi/bin/gopi` 的符号链接、
revision 为 `4dcd8a431193d64efd2db5b6988678c1296aff21` —— **不是**本片的证据来源。）

### 1.2 取 `-h` 的命令总数：**105**，非零退出 **0**

| 批次 | 计数 | 落盘文件 |
|---|---|---|
| `all-exits.tsv` | 70 | `L0-top.txt`、`L0-env.txt`、`L1-<verb>.txt`×11、`L2-<verb>-<sub>.txt`、`R-task-<retired>.txt`×3、`L1-session-flag.txt` |
| `tool-exits.tsv` | 22 | `tool-<command>.txt`、`tool-sandbox-{create,run,show}.txt`、`tool-web-{search,fetch}.txt` |
| `third-exits.tsv` | 13 | `gopi task <sub> -h`，sub ∈ {merge, merge-execute, merged-cleanup, adopt, audit-merges, send, receive, pull, push, sync, sync-pass, diff, harvest} |
| **合计** | **105** | |

复算命令（`all-exits.tsv`/`third-exits.tsv` 每行为 `tag <TAB> 命令原文 <TAB> exit=W`，
`verb-exits.tsv`/`tool-exits.tsv` 只有 `命令 <TAB> exit=W` 两列 —— 所以下面的
awk 按"有第三列就取第三列、否则取第二列"取退出码，**不能只写 `$3!=0`**：
对两列文件 `$3` 恒为空，那个检查会静默地漏掉一个非零退出）：

```
$ cd /home/user/.gopi/sessions/01a1aa6c-e0b2-76e9-b07e-cfba9a16c1b0/artifacts/help
$ wc -l all-exits.tsv verb-exits.tsv tool-exits.tsv third-exits.tsv
   70 all-exits.tsv
   11 verb-exits.tsv
   22 tool-exits.tsv
   13 third-exits.tsv
  116 total          # 70 + 22 + 13 = 105 次取页；verb-exits.tsv 11 行是 L1 动词的
                     # 退出码记账，与 all-exits.tsv 的 L1 行是同一批命令，不重复计入 105
$ awk -F'\t' '{c=($3!=""?$3:$2); if(c+0!=0) print FILENAME": "$0}' \
    all-exits.tsv verb-exits.tsv tool-exits.tsv third-exits.tsv
（无输出 —— 105 次取页全部 exit=0）
```

**一条必须留痕的更正（2026-11-06 00:27 的自查）**：`all-exits.tsv` 里
`L1-session-flag` 那一行原先记的是 **exit=127**，不是 0。原因不是命令不存在，
而是那一行的调用写成了相对路径 `./bin/gopi`，而当时的工作目录不是 ground ——
文件内容就是证据：`bash: line 2: ./bin/gopi: No such file or directory`。
用绝对路径重跑后 **exit=0**，页面 41 行已存进 `L1-session-flag.txt`：

```
$ /home/user/workspace/01a1aa6c-e0b2-76e9-b07e-cfba9a16c1b0/bin/gopi --session -h
$ echo $? ; wc -l L1-session-flag.txt
0
41 L1-session-flag.txt
```

⇒ 教训（已写进本节而不是删掉了事）：**"全部 exit=0"这句话当时是错的**，
而错它的方式正是本片要防的那一种 —— 计数脚本用了不会命中的列。一个 127
（command not found）与一个"版本不对/参数错"在读表的人眼里长得一样，
所以这里同时记下**命令原文**，让下一个人能看出它是路径问题，不是接口问题。

### 1.3 层级结构

- **L0**：`gopi -h`（`L0-top.txt`）、`gopi --help-env`（`L0-env.txt`）
- **L1**：11 个顶层动词 —— chat config memory notify session task tool ui_state version web worker，外加 `gopi --session -h`
- **L2**：子命令页 —— task **24** 个（`ls L2-task-*.txt | wc -l` = 24）、memory 15、session 10、notify 3、config 4、worker 1
- **tool**：`gopi tool -h` + 22 个工具页（`sandbox`、`web` 是分组，各自再取二级）
- **R**：三条已退役拼写的页面（`R-task-merger.txt` / `R-task-serve.txt` / `R-task-merge.txt`）

### 1.4 枚举脚本

`artifacts/help/enum.sh.txt` —— 原样可重跑，产出上面全部文件与两张 `*-exits.tsv`。

### 1.5 E1b：help 面关键词记账

`e1b.sh.txt`（可重跑，正则在里面）+ `e1b-hits.tsv`，覆盖本目录 105 份原始输出：

| 维度 | files_hit | lines_hit |
|---|---|---|
| D01 install | **0** | **0** |
| D02 firstrun | 54 | 780 |
| D04 configfile | 51 | 194 |
| D05 headless | 49 | 131 |
| D06 resume | 46 | 218 |
| D07 audit | 34 | 122 |
| D08 mcp | **0** | **0** |
| D09 approval | 5 | 15 |
| D12 guiauto | 16 | 72 |
| D15 cost | 20 | 58 |

两个 **0** 是有意义的 0，不是正则写错的 0：§3 会用源码第二次确认。

**这里必须写明一个我一开始写错、随后被实测推翻的地方**（留着，因为它是本片方法论的证据）：
原先这句写的是「`grep -rli mcp --include=*.go` 在全仓 0 命中」——**那是假的**。实测：

```
$ cd /home/user/workspace/01a1aa6c-e0b2-76e9-b07e-cfba9a16c1b0
$ grep -rli "mcp" --include=*.go . | wc -l
6
$ grep -rliE "(^|[^a-zA-Z0-9_])mcp([^a-zA-Z0-9_]|$)" --include=*.go . | wc -l
0
$ grep -rli "model context protocol\|\bmcp\b" --include=*.go internal/ cmd/
（无输出，exit=1）
```

那 6 个文件命中的全是**更长的标识符**里的一段：`memcpy`（`devdoc/voice-zerodep-poc/model_embed*.go`
四份）与 `pmcPassPkgCount` / `branch_check` 测试里的 `pmc` 片段 —— **没有一处是 MCP**。
所以 D08 的结论（**none**）不变，但**支撑它的那句话原来的写法是错的**：
不带词边界的 `mcp` 在 Go 源码里 6 命中，带词边界 0 命中。**结论对 ≠ 那句话对。**

---

## §2 gopi 能力 × 维度表（本片负责的 10 维）

评分口径沿用阶段 0：`full` = help 面有专门入口且 E3 实跑产生真实效果；
`partial` = 有入口但边界受限、或需要外部组件才能用；
`none` = help 面 + 源码双面都没有；`help-only` = help 说有、源码没有实现（单独列 §4）。

| # | 维度 | 判定 | 出面证据（help 行） | 源码交叉验证 |
|---|---|---|---|---|
| D01 | install | **none** | 105 份 `-h` 全文对 `npm i\|pip install\|go install\|brew install\|make build\|download` **0 命中**（§1.5）。`gopi config setup` 是**装完之后**把 profile 变成 run-ready，不是获取二进制 | 仓库根**无** `README*`、`package.json`、`pyproject.toml`、`setup.py`；只有 `go.mod`。`man/` 下 8 个页面无一含 INSTALLATION 章节。构建路径只有 `Makefile:35` 的 `build:` 目标 |
| D02 | firstrun | **full** | `L1-config.txt:10-11` "gopi config setup … Provision a FRESH host to a run-ready state (idempotent)"；`L0-top.txt:5-11` 三种一次性调用形态（prompt 参数 / 文件 / stdin） | `cmd/agent/cli_setup.go:87 runSetupSubcommand`、`:117 runSetup`。**E3 实测**（`e3/D02.txt`）：空目录 `GOPI_HOME=/tmp/emptyhome-2224509` → 4 行 `created`/`initialized`，`find` 证实真的落了 `settings.json`、`sessions/`、`memory.db`。二次运行输出 `nothing to do — the profile is run-ready`、exit 0（幂等，实测于 `e3/D02-idempotent.txt`：同一个 `/tmp/emptyhome-2224509` 再跑一次，4 项全 `present`）。**E3 实测**（`e3/D02b.txt`）：同一个空 home 直接 `gopi chat --once -p "say the single word READY"` **不再需要 setup** —— 它自动建了 `.gopi/memory.db`、`sessions/<id>/`、`chat_session_id_0.txt`，且答案 `READY` 出现在 JSONL 第 15 行。⇒ 首次运行比 help 写的更宽松：setup 是**加固**，不是**前置条件** |
| D04 | configfile | **full** | `L1-config.txt:22-25` "config show INSPECTS … config setup PROVISIONS from the same dimension"；`L0-env.txt` 是一张 **480 行**的完整环境变量表（`wc -l` 实测）；`--help-env` 是一个独立顶层开关 | `cmd/agent/cli_config.go:137 parseConfigShowFlags`、`:167 runConfigShow`、`:235 resolveConfigShowSettingsPath`。**E3 实测**（`e3/D04.txt`）：`gopi config show` 打出 `settings.json:` 真值段（`api_key <set: 35 chars>`、`repos {...}`、`notify {...}`）。解析优先级在 `--help-env` + `config show` 输出里都能读到 |
| D05 | headless | **full** | `L1-chat.txt` 的 `--once` 段；`--tools` 段；`gopi web` 也有一体型 headless 用法 | `cmd/agent/chat_once.go:604 runChatOnce`。**E3 实测**（`e3/D05.txt`）：`gopi chat --once -p "reply with exactly: OK"` → exit 0，输出第 15 行为 `OK`。**E3 实测**（`e3/D05pipe.txt`）：`echo 'say the single word READY' \| gopi chat --once`（stdin 形态）→ exit 0，第 15 行 `READY`，末行 `you> bye.` 说明 REPL 外壳仍在但**非交互**跑完即退 |
| D06 | resume | **full** | `L1-session.txt` 的 `session list/log/show/stats/resume`；`L1-chat.txt` 的 `--resume SESSION`、`--session <id>` | `cmd/agent/cli_session.go:421 runSessionList`、`:560 runSessionShow`；`cmd/agent/chat_once.go` 的 resume 分支。**E3 实测**（`e3/D06.txt`）：`gopi session list --limit 3` 打印 3 条 + `[hidden] 157` + `[limit] 2378 more` + `[scanned] 1645 of 3866`。**E3 实测**（`e3/D06resume.txt`）：`gopi --session c97d834f-… "what did I ask you to reply with?"` → 回答 **`OK`** 且第 14 行自报 `resume: loaded 1 history message(s)` —— 跨进程真的恢复了上一轮上下文 |
| D07 | audit | **full** | `L1-session.txt` 的 `session log` / `session raw` / `session stats --rounds/--tools/--bytes/--cost`；`--verbose`、`--debug` 出现在多处 L1 | `internal/tools/search.go` 之外，`cmd/agent/cli_session.go` 的 log/raw 腿。**E3 实测**（`e3/D07.txt`）：`gopi session stats c97d834f-… --cost` 打出真金白银的分项表（`prompt tokens 167,094`、`cache 72.01% hit (120K) / 47K miss`、`completion 58`、`turns 2`）。**E3 实测**：`ls ~/.gopi/sessions/c97d834f-…/*.jsonl` → `session.jsonl` 4205 字节 / 15 行，`memory-prefetch.jsonl` 0 行 —— 会话正文是可读 JSONL |
| D08 | mcp | **none** | 105 份 `-h` 对 `\bMCP\b\|mcp server\|model context protocol` **0 命中**（§1.5） | `grep -rli "model context protocol\|\bmcp\b" --include=*.go internal/ cmd/` → **无输出、exit=1（0 文件）**；不带词边界的 `grep -rli mcp --include=*.go .` → 6 文件，逐个打开全是 `memcpy` / `pmcPassPkgCount` 这类更长的标识符，**没有一处是 MCP**（实测见 §1.5 末）。help 面与实现面一致地没有 MCP。**要注记的边界**：repo 里有一处 `internal/tools/` 下的扩展机制（`plugins/websearch/`）——但那是**内置插件**，不是 MCP 客户端，见 D14 行外的 §4 |
| D09 | approval | **full（已接线，但 help 自述"未接线"—— 见 §4.2）** | `L1-chat.txt:330-342` `--policy POSTURE` 段，末三行（`:340-342`）写 "NOT ENFORCED YET: this run reports the resolved posture and the write boundary is unchanged." 同一句也出现在 `man/gopi-chat.md:205` | `internal/tools/chat_write_guard.go:202` → `return chatBashPolicyForbiddenRefusal(command, m)`，拒绝文案由 `internal/tools/chat_policy_gate.go:159` 渲染。**E3 双向实测**：`e3/D08a.txt` `--policy=forbidden` → bash 调用的 `tool_result` 是 `Refused by the CHAT WRITE BOUNDARY … the startup posture is 'forbidden'`；`e3/D08b.txt` `--policy=allow` → 同一句话的命令 `bash` 两次 `exit=0`，正文第 41-42 行 `$ echo POLICY-PROBE-A` / `POLICY-PROBE-A`。**⇒ 姿态是真的在执行层判的，help 那句"未接线"已过期**（见 §4 help-only 的反向一例） |
| D12 | guiauto | **partial** | `tool-act.txt:1` "drive the LIVE X11 desktop: move the pointer, click, type"；`tool-capture.txt:1` "capture the LIVE X11 desktop to a PNG"；`tool-screen_capture.txt`；`tool-shot.txt:3-4` "Drives a local headless Chromium over the raw Chrome DevTools Protocol (zero Go dependencies; finds chrome via GOPI_SHOT_CHROME, the playwright …)" | X11 侧：`cmd/agent/cli_act.go:490 runActSubcommand`、`:511 runAct`（真 XTEST 注入，`tool-act.txt` 第 3 行自述 hand-rolled XTEST）。浏览器侧：`internal/shot/cdp.go:2` "Chrome DevTools Protocol (CDP) client that drives a local headless Chromium"、`:43` 是手写 RFC 6455 WebSocket。**实测边界**：`tool-shot.txt:4` 明说 chrome 靠 `GOPI_SHOT_CHROME` / playwright 目录**找到**，二进制不在仓库里 ⇒ 判 partial 而非 full。X11 侧另有硬边界：`screen_capture` 在 Wayland 上**按设计拒跑** |
| D15 | cost | **full** | `tool-tokens.txt:1-4` "GoPi tool tokens — count tokens, and put our count beside the server's / Usage: gopi tool tokens --file <path>"；`L1-session.txt` 的 `stats --cost` | `cmd/agent/cli_tokens.go:36 isTokensSubcommand`（`CliVerbTokens` 在 `cli_tool.go:204` 注册）。**E3 实测**（`e3/impl-probe.txt`）：`gopi tool tokens --file …/PROJECT.md` → `37529 tokens (143728 bytes, 2234 lines, deepseek-v4.1-bpe)`，exit 0。**E3 实测**（`e3/D15.txt`）：`session stats … --cost` 给出 model/source/turns/requests/prompt tokens/cache/completion 逐项真值 |

### 2.1 一句话汇总

本片 10 维里 **full = 7**（D02 D04 D05 D06 D07 D09 D15）、**partial = 1**（D12）、
**none = 2**（D01 D08）。两个 none 都不是遗漏，是**双面一致地没有**：
gopi 不分发二进制（`D01`），也不实现 MCP（`D08`）。
唯一的 partial（D12）是因为它的浏览器腿要**外部** Chromium 才成立，仓库里没有那个二进制。

---

## §3 源码交叉验证（只对本片判 full/partial 的格子做）

| 维度 | 符号 | 位置 |
|---|---|---|
| D02 | `runSetupSubcommand` / `runSetup` | `cmd/agent/cli_setup.go:87` / `:117` |
| D04 | `parseConfigShowFlags` / `runConfigShow` / `resolveConfigShowSettingsPath` | `cmd/agent/cli_config.go:137` / `:167` / `:235` |
| D04 | `ResolveSandboxRoot(settingsCfg, flagCfg *Config) (root, source string)` | `internal/config/sandbox.go:228`（`gopi tool code-search ResolveSandboxRoot` 实测命中，见 `e3/D04-symbol.txt`）。**注**：sandbox 本身是 D10（阶段 2C），此处只借它当"配置项由单一函数解析"的**机制样板**，不构成对 D10 的判定 |
| D05 | `runChatOnce` | `cmd/agent/chat_once.go:604` |
| D06 | `runSessionList` / `runSessionShow` | `cmd/agent/cli_session.go:421` / `:560` |
| D07 | `gopi session stats` 四腿（`--rounds/--tools/--bytes/--cost`） | `L1-session.txt` 声明；`e3/D07.txt` 实跑 `--cost` 与 `--tools` 两腿 |
| D09 | `chatBashPolicyForbiddenRefusal`（调用点 + 渲染点） | `internal/tools/chat_write_guard.go:202` → `internal/tools/chat_policy_gate.go:159` |
| D09 | `ChatWritePathRefusal` / `ChatWritePathRefusalAuthorized` | `internal/tools/chat_write_guard.go:529` / `:539` |
| D12 | `runActSubcommand` / `runAct` | `cmd/agent/cli_act.go:490` / `:511` |
| D12 | CDP 客户端 | `internal/shot/cdp.go:2`（注释）/ `:43`（`cdpConn`） |
| D12 | `handleShot`（web 侧截图入口） | `cmd/agent/cli_web_shot.go:105` |
| D15 | `isTokensSubcommand` | `cmd/agent/cli_tokens.go:36` |
| D15 | `TaskMerge`（跨片引用，§4 用） | `internal/tools/task_merge.go:570` |
| D01 | 构建目标 | `Makefile:35` `build: CGO_ENABLED=0 go build -o $(BIN_PATH) ./cmd/agent/` |
| D09 | 配置映射（`chat_op_auto` → posture） | `e3/D12.txt` 的 server log 第 1 行是运行期实证：`settings.json "chat_op_auto"=true is mapped to the chat posture "allow"` |

**验证方法说明**：`code_search` 只用于找**定义**（Rule 3 的路由规则），
`keyword_search`/`grep` 用于"某符号还在不在别处"的分布题 —— 两者不是可互换的。

---

## §4 help-only 一节（help 说有、实际不成立）

本节是 Rule 41 要求的产物：**它包含两条，方向相反**。

### 4.1 正向 help-only：help 承诺的 flag，引擎不认

```
$ cd /tmp && gopi task merge --dry-run
gopi task merge: unknown flag --dry-run
GoPi task subcommands — the task-as-memory ledger (跨会话追溯/审计).
Usage:
  gopi task <sub> [args]
exit=1
```

`R-task-merge.txt`（`gopi task merge -h` 的 34 行页面）第 8-18 行列了 7 种调用形态：
不带参数全扫、`<branch>...`、`-h|--help`、`--all`、`--repo PATH`、`--submit`、`--db PATH`。
`--dry-run` **不在**这 7 种里 —— 所以这一条本身**不是** help 的错，是我先猜了一个 flag。
**保留在这里的价值是反向的**：`--dry-run` 被拒这件事证明了 `-h` 页的 flag 清单是**真的被读的**，
不是装饰。同一批探测里 `--all` / `--repo` / `--submit` 三个都被真接受（各自给出各自的错误或行为）：

```
$ gopi task merge --repo /nonexistent
gopi task merge error: task merge: repo "/nonexistent" is not in the settings.json "repos" table (1 configured) — a full sweep covers the table; add the repo to it (GOPI_SETTINGS names the file), or name the branches to merge
```

⇒ **本小节最终判定：无正向 help-only。** `task merge` 的 `-h` 页与 `tools.TaskMerge`
（`internal/tools/task_merge.go:570`）确实对得上，页面第 5 行自述的 "same engine (tools.TaskMerge)" 成立。

### 4.2 反向 help-only：help 自述"未接线"，实际已经接线 —— **这一条要提 bug**

这是本片最有价值的发现，因为它**方向相反**：不是"help 吹了没实现"，而是
**"help 说自己没实现，实现已经做了"**。对读者的危害一样大 —— 读者会据此以为
`--policy` 不生效，从而不去用（或者更糟：以为它不生效而去依赖它不管事）。

| 面 | 原文 | 位置 |
|---|---|---|
| help | `NOT ENFORCED YET: this run reports the resolved posture and the write boundary is unchanged.` | `L1-chat.txt:340-342` |
| man | `NOT ENFORCED YET: this run resolves, reports and freezes the posture, and the write boundary (chat_write_guard.go) is unchanged — so the posture governs nothing until the wiring item lands. Nothing here is a claim that yolo runs your commands; it is the channel a wired judgement will read.` | `man/gopi-chat.md:205` |
| 源码 | `return chatBashPolicyForbiddenRefusal(command, m)` | `internal/tools/chat_write_guard.go:202` |
| 实测 A | `Refused by the CHAT WRITE BOUNDARY (echo POLICY-PROBE-A): the startup posture is 'forbidden' and no rule allows it…` | `e3/D08a.txt`（`--policy=forbidden`） |
| 实测 B | `⚙ call bash echo POLICY-PROBE-A` → `↳ result bash exit=0`，正文 `$ echo POLICY-PROBE-A` / `POLICY-PROBE-A` | `e3/D08b.txt`（`--policy=allow`） |

**Why it matters**：一句 `NOT ENFORCED YET` 落在 man 页里，是**官方承诺**级别的文本。
它现在把一个已经工作的安全栅栏描述成尚未工作的。两个方向的读者都会做错决定。

**为什么两处都对不上 —— 用 git 把时间线查出来了，不是猜的**（这条是本片自查后才补上的，
原先这里写的是"需要先确认 man 页是手写还是生成的"，现在有答案了）：

```
$ git log --reverse --format='%h %ad' --date=iso -S "ENFORCED YET: this run reports" -- cmd/agent/cli_chat.go
1de01bf6 2026-10-19 20:45:07 +0800      # help 文案在这里落地
$ git log --reverse --format='%h %ad' --date=iso -S "NOT ENFORCED YET" -- man/gopi-chat.md
3c6dad33 2026-10-19 20:53:57 +0800      # man 文案在这里落地
$ git log -1 --format='%h %ad %s' --date=iso 8d9a35ec
8d9a35ec 2026-10-19 21:58:15 +0800 chat policy 3/6: the bash arm decides from the operator's rules; the verb reader is gone
```

⇒ **两段文本都比接线早，接线提交没有回头改它们。** 20:45 与 20:53 写下的"未接线"是
当时的事实；21:58 的 `chat policy 3/6` 把 bash 臂接上之后，这两句话就变成了过期陈述，
而没有任何一步要求改它们（它们不在同一次提交里，也不是生成物）。

**两处文本是各自独立的源，不是一份源生成两份** —— 所以修它需要两处编辑：

| 面 | 源文件 | 性质 |
|---|---|---|
| help | `cmd/agent/cli_chat.go:303-306`（`-h` 页是一个 Go 字符串常量） | 人工编辑 |
| man | `man/gopi-chat.md:205` | 人工编辑（`Makefile:109` 写明 "man/*.md is the source a human edits"） |

这也解释了为什么 `grep -rn "NOT ENFORCED" --include=*.go .` 在**改之前**输出为空：
Go 里那句被源码字符串拼接**断行**成 `NOT\nENFORCED YET`（`cli_chat.go:304` 行尾是 `NOT`），
所以按整句 grep Go 源码会得到一个假阴性。**按整句 grep 找不到，不等于不存在**
（Rule 43 的同音陷阱，反向的一种）。

**已按 Rule 41 提行**：`01a1ac91-de51-77e0-83ad-8cf86c1d76bb`（`tags: bug`，
`priority: 1`，`status: pending`，实测 `gopi task show` 读到）。**本片不修它** ——
它落在 §4 之外（`chat` 面的文档准确性），且修法需要同时动 Go 常量与 man 页两个源，
属于另一条独立的改动。

### 4.3 已退役拼写的三条页面（不是 help-only，是存活证明）

```
$ gopi task merger
merger: retired — the resident merger daemon was retired for re-development: `--stop` is only answered at a TICK BOUNDARY, so an in-flight tick cannot be interrupted — a merge the operator cannot stop is not patched in place (see devdoc/merger-retired.md)
  merging itself is NOT retired: it is reached through the family spelling `gopi task merge` (same engine, same lock)
exit=1
```

- `gopi task merger -h` / `gopi task serve -h`：**exit=0**，打印的是这条拒绝串（`R-task-merger.txt` / `R-task-serve.txt`）。
  注意 `-h` 也返回 0 —— 判"这个动词还在不在"**不能只看退出码**，要读正文。
- `gopi task merger`（无 `-h`）：**exit=1**。⇒ 这条拒绝是**真的**，不只是在 help 里写着。

---

## §5 不属于本片（越界声明）

| 维度 | 归属 | 依据 |
|---|---|---|
| D03 provider | 阶段 2C | 模型接入 |
| D10 sandbox | 阶段 2C | 隔离/容器 |
| D11 image | 阶段 2C | 多模态 |
| D13 server | 阶段 2C | 服务端 |
| D14 extend | 阶段 2C | 插件/钩子 |
| D16 orchestrate | 阶段 2B | 多 agent 调度 |
| D17 | **未定义** | 阶段 0 的 `dims.txt` 只有 D01–D16，共 16 行（`wc -l` 实测 16）。本片在 §2 只表 10 维，剩余 6 维按上表分流，**D17 无人认领** —— 这是阶段 0 的口径缺口，不是本片漏做 |

**给阶段 2B/2C 的一行提示**（不展开，否则越界）：`gopi config show` 里
`repos {...}` 表既是构建声明也是合并扫描表（Rule 12），`notify.events: all`
订阅了全部事件 —— 这两处在 D13/D16 上都会是关键证据，别从零开始。

---

## §6 复核清单（每一条都能重跑）

```
# 1. 枚举总量与非零退出
cd /home/user/.gopi/sessions/01a1aa6c-e0b2-76e9-b07e-cfba9a16c1b0/artifacts/help
wc -l all-exits.tsv verb-exits.tsv tool-exits.tsv third-exits.tsv   # 70 / 11 / 22 / 13（取页 105）
awk -F'\t' '{c=($3!=""?$3:$2); if(c+0!=0) print FILENAME": "$0}' \
    all-exits.tsv verb-exits.tsv tool-exits.tsv third-exits.tsv     # 无输出

# 2. 关键词记账
bash e1b.sh.txt                                          # 重算 §1.5 的表

# 3. 逐维实跑（文件见 §2 表格右列）
cat e3/D02.txt e3/D02-idempotent.txt e3/D02b.txt e3/D04.txt e3/D04-symbol.txt \
    e3/D05.txt e3/D05pipe.txt e3/D06.txt e3/D06resume.txt e3/D07.txt \
    e3/D08a.txt e3/D08b.txt e3/D12.txt e3/D15.txt e3/impl-probe.txt

# 4. §4.2 的矛盾（help 说未接线 / 源码已接线 / 实测两向）
grep -n "NOT ENFORCED" /home/user/workspace/01a1aa6c-e0b2-76e9-b07e-cfba9a16c1b0/man/gopi-chat.md
sed -n '202p' /home/user/workspace/01a1aa6c-e0b2-76e9-b07e-cfba9a16c1b0/internal/tools/chat_write_guard.go
cat e3/D08a.txt e3/D08b.txt
```

证据根目录：`/home/user/.gopi/sessions/01a1aa6c-e0b2-76e9-b07e-cfba9a16c1b0/artifacts/help/`
（`MANIFEST.md` 是它的索引，`e3/` 是本 run 亲自执行的实跑输出）。
