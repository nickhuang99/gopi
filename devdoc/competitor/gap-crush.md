<!-- doc-class: historical -->

# gopi × Crush 差距对比表（17 维，D01–D17）

<!-- meta -->
dimension_framework: devdoc/competitor-analysis-framework.md（§2 四级口径、§3 十七维探针、§4 证据规范、§7.3 等级/分桶）
our_side_sources: 本 run 在 ground 内自编 `bin/gopi` 的 E3 实跑（17 维逐条，输出落 `/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/`）；阶段 2 盘点 `devdoc/competitor/gopi-self-a.md` / `gopi-self-b.md` 只作交叉旁证，本次未改一字
opponent_source: devdoc/competitor/crush.md（阶段 2 采集快照，本次未改一字、未重采）
opponent_pin: charmbracelet/crush@b18531d6b0971f36d443d2569fb453cd61cfd090（short `b18531d`）—— 本文件每一处 `charmbracelet/crush@b18531d:path:NNN` 的行号都固定在该 sha 上
opponent_channel_cap: E1+E2 only —— 对方不产生 E3（不得为取证安装/运行第三方 agent，Rule 27/35），故对方等级上限 `partial`；本文件 17 行里对方无一行 `full`
our_measurement_anchor: ground HEAD `86bceb86f3c9e08a99c678e11cca1c79e97764f4`（`git rev-parse HEAD`）
installed_binary_anchor: 本 run 的 E3 全部由 **ground 内自编的 `./bin/gopi`** 执行 —— `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` 后 `./bin/gopi --version` 打印 `v0.4.0` / `go1.24.0` / `linux/amd64` / `revision: 86bceb86f3c9e08a99c678e11cca1c79e97764f4`，与该 ground 的 HEAD **逐字相同**（环境里的 `/usr/local/bin/gopi` 未被当成证据来源）
evidence_dir: `/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/`（不进 git 树）
counts_as_of: 2026-11-06T08:00Z（等级是快照读数，随对方 main 与我方 HEAD 变动而失效）

<!-- 为什么本文件不是「四张表 × 17 行 = 68 行」：派发简报沿用了旧措辞，而形态由已合入
     master 的门禁决定。门禁原文（internal/standards/gapdoc_contract_gate.go）：
     :7-8   被交付过的两份文档「written as FOUR tables each, and each of the four
            tables carried ALL SEVENTEEN dimensions — 68 rows = 17 × 4」
     :12-13 该缺陷的实质是「the two MUTUALLY EXCLUSIVE buckets both claimed the same
            dimension」——不是排版问题，是同一维在互斥两桶里重复
     :20    本契约「has SIX verdicts rather than the historic four」
     :40-53 五条判据：主表恰 17 行 / 判定列 == gapDocVerdictFor / 等级格是四级之一 /
            桶视图每个桶恰声明一次（空桶写 —）/ 每维只出现在它判定所指的桶里
     :126   gapDocDimCount = 17        :141 gapBucketEmptyMark = "—"
     :153   gapBucketViewHeadingCue = "桶视图"   :156 gapMainRowRE = `^\| *D([0-9]{2}) *\|`
     :201   func gapDocVerdictFor(our, opp string) (string, error)
     照「四张表」写会被判两条红：主表 68 行（判据 1）与每维出现 4 次（判据 5）。
     本文件按门禁写：一张 17 行主表 + 一节六桶视图。冲突与门禁原文同时写进任务报告。 -->

## 判定口径（按框架 §2，四级严格定义）

本表每行两个等级格都取自框架 §2 的四级，判定列**不是手写的**：它由
`gapDocVerdictFor(我方等级, 对方等级)`（`internal/standards/gapdoc_contract_gate.go:201`）
机械导出，规则如下（本 run 用 `./bin/gopi tool code-search gapDocVerdictFor --root .`
真跑确认该函数位置，输出 `internal/standards/gapdoc_contract_gate.go:201`）：

- `full` = 2、`partial` = 1、`none` = 0；`unknown` **不参与比较**，它表示「该通道从未执行过」，
  不是「比 `none` 更低的档」（§2 A4）。
- 任一侧为 `unknown` ⇒ `不可比`。**两个 `unknown` 也写 `不可比`，绝不写 `平手`**（§7.3 A3/A4）。
- 两侧都已知时比序数：我方高 ⇒ `我强敌弱`；对方高 ⇒ `敌强我弱`；相等 ⇒ `平手`；
  我方 0 且对方非 0 ⇒ `敌有我无`；对方 0 且我方非 0 ⇒ `我有敌无`。
- 对方没有 E3 通道（框架禁止为取证安装/运行第三方 agent），所以对方等级**上限
  `partial`**；`full` 只有在我方一侧出现。这不是「我们处处更强」，是口径的直接产物。

证据两侧的通道是分开写的：**我方 = E3**（本 run 在 ground 内真跑的命令 + 原始输出，
落 `/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/`）；**对方 = E1/E2**（`crush.md` 里已钉在该 sha 上的 guide 行号与
`path:line` 锚点，本次未重采、未改一字）。

## 逐维判定（17 维，一维一行）

| 维度 | 我方等级 | 对方等级 | 判定 | 证据 |
|---|---|---|---|---|
| D01 | partial | unknown | 不可比 | 我方：`grep -nE '^install:' Makefile` → `129:install: build`（rc=0）；`grep -n 'make install' README.md` → `138:make install`、`141:`make install` 会做两件事：构建 `bin/gopi`…`、`154:make install PREFIX=/usr/local/bin`；`ls package.json pyproject.toml setup.py` → 三者皆 `No such file or directory`；`grep -cE 'npm (install\|i) -g\|pip3? install\|pipx install\|brew install\|go install\|install\.sh' README.md` → `0` ⇒ 有具名安装目标（`make install` + `PREFIX=`），无包管理器通道、无 OS 分支，记 `partial`（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d01.txt`）。对方：阶段 2 已作双零探针——guide 全文 0 命中 + 该 sha 全树 1398 条 blob 路径 0 命中 + 抽样内容 0 命中 ⇒ `unknown`（探针未取得该维输出，不是「看过、没有」的 `none`，见 `devdoc/competitor/crush.md` D01 行的「试过的 URL」清单） |
| D02 | full | partial | 我强敌弱 | 我方：`./bin/gopi config setup` → `rc=0`，逐行打印 `profile data root  present      /home/user/.gopi`、`sessions  present`、`settings  present`、`task ledger  present`，末行 `nothing to do — the profile is run-ready`（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d02.txt`）—— 幂等 provisioning，一条命令把宿主带到可跑状态，且已有 profile 时不重写。对方：E1 `dsa-docs/crush.md:38` 原文「`"api_key": "$DEEPSEEK_API_KEY",`」（key 走环境变量注入，注册表即配置文件形态）+ E2 文档(非实现) `charmbracelet/crush@b18531d:docs/config/FUTURE.md:25` 原文「``export` or `alias` at a live prompt.`」⇒ 零配置首跑有一条环境变量路径，但只有 E1+E2、无 E3 ⇒ `partial` |
| D03 | full | partial | 我强敌弱 | 我方：`./bin/gopi -h` 打印 `--model NAME  model (DEEPSEEK_MODEL / ANTHROPIC_MODEL)`、`--base-url URL  API base URL (DEEPSEEK_BASE_URL / ANTHROPIC_BASE_URL)`、`--vision-model NAME`、`--vision-base-url URL`（`grep -cE -- '--model\|--base-url\|--api-key\|--vision-model'` → `4`）；`./bin/gopi config show` 打印生效值 `base_url  "https://api.deepseek.com/anthropic"`、`model  "deepseek-flash"`、`vision_model  "deepseek-flash"`；`./bin/gopi --help-env` 里带 `BASE_URL`/`_MODEL` 的行 `10` 条（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d03.txt`）⇒ 端点与模型都能换，旗标/环境变量/配置文件三层都通。对方：E1 `dsa-docs/crush.md:37` 原文「`"base_url": "https://api.deepseek.com",`」+ E2 文档(非实现) `charmbracelet/crush@b18531d:docs/config/FUTURE.md:46` 原文「`model-switch dialog and API-key entry:`」⇒ 自定义 `base_url` 在配置面成立，无 E3 ⇒ `partial` |
| D04 | full | partial | 我强敌弱 | 我方：`./bin/gopi config show` → `rc=0`，共 `51` 行（身份段 + 生效键值两列，敏感项脱敏成 `api_key  <set: 35 chars>`）；`./bin/gopi --help-env \| wc -l` → `455` 行环境变量总表（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d04.txt`）⇒ 「打印生效值」与「枚举旋钮」两条腿都真跑。对方：E1 `dsa-docs/crush.md:28` 原文「`- **Linux / macOS**: `~/.config/crush/crush.json``」+ E2 文档(非实现) `charmbracelet/crush@b18531d:docs/config/FUTURE.md:121` 原文「`~/.local/share/crush/crush.json`」⇒ 有单一文档化配置载体，但「打印生效值」的命令面无 E2 命中、无 E3 ⇒ `partial` |
| D05 | full | partial | 我强敌弱 | 我方：`./bin/gopi chat --once -p 'reply with exactly: GAP-OK'` → 输出 `GAP-OK`、`rc=0`，全程无 TTY、一次调用即退出（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d05.txt`；同一通道另有一条 `./bin/gopi chat --once --tools --policy=allow -p 'run the bash command: echo hi'` → `⚙ call bash echo hi` / `↳ result bash exit=0`，见 `e3-d08.txt`）⇒ headless 一次性执行是真入口。对方：E2 源码 `charmbracelet/crush@b18531d:internal/cmd/projects.go:23` 原文「`crush projects --json`」（`--json` 是机器可读输出，属一次性命令，但不是「一次调用跑完一个 prompt」的 headless 主路径）；guide 全文无该维话题 ⇒ `partial` |
| D06 | full | none | 我有敌无 | 我方：`./bin/gopi session list --limit 3` → `rc=0`，打印 `3 session(s):`，逐行带 `[run]`/`[chat]` 种类、pid 与存活态、branch、起止时刻、`turns`/`total tok in=217631 out=3449 cache=217631`，并交代未展示部分：`[hidden] 157 session(s) hidden — their session log is not on this machine; --all shows them`、`[limit] 2688 more session(s) matched; --limit N raises the cut`、`[scanned] 1645 of 4079 session director(ies) held no run log`（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d06.txt`）⇒ 会话可列、可查、可续（`session log`/`session resume` 同族）。对方：阶段 2 的双零探针 —— guide 全文 0 命中 + 该 sha 全树 1398 条 blob 路径 0 命中 + 抽样内容 0 命中 ⇒ `none`（**看过、没有**；该读数适用范围是 guide 语料与该 sha 的路径/抽样，不等于运行时一定不具备，`devdoc/competitor/crush.md` D06 行原文如此） |
| D07 | partial | partial | 平手 | 我方：`./bin/gopi -h \| grep -ci mcp` → `0`；`ls ~/.gopi/plugins/` → `websearch`；`ls ~/.gopi/plugins/websearch/scripts \| wc -l` → `8`（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d07.txt`）⇒ 有插件目录与 8 个真实脚本**实现**，但帮助面无扩展点声明、无 MCP ⇒ 有实现、无声明，记 `partial`。对方：E1 `dsa-docs/crush.md:5` 原文「`Crush is a glamorous open-source AI coding agent that runs in your terminal, built by Charm. It supports multi`」+ E2 文档(非实现) `charmbracelet/crush@b18531d:docs/hooks/FUTURE.md:1` 原文「`# Hooks: Future Work`」⇒ hook 面被明确标成 future work，扩展点存在但按文档形态记账，无 E3 ⇒ `partial`。两侧同为 `partial`、机制不同（我方是磁盘插件目录 + 无声明，对方是文档里已规划的 hook 面 + 键位/服务器面），等级口径分不出这个差，不给高下（§2 A4） |
| D08 | full | unknown | 不可比 | 我方：同一条命令在两种 posture 下得到两种结果，两次输出都落盘 —— `./bin/gopi chat --once --tools --policy=forbidden -p 'run the bash command: echo hi'` → `rc=0`，回复内容为被 `CHAT COMMAND BOUNDARY` 拒绝的说明（「本项目 chat-policy 栅栏无任何一行提到 echo，本次 run 姿态为 --policy 给的 forbidden，故无规则覆盖的命令一律不执行」），未执行任何命令；`./bin/gopi chat --once --tools --policy=allow -p 'run the bash command: echo hi'` → `rc=0`，输出 `⚙ call bash echo hi` / `↳ result bash exit=0` / `$ echo hi` / `hi`（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d08.txt`）⇒ 门是运行期可切换的，两种结果都可观察。对方：阶段 2 只拿到路径级旁证 —— 该 sha 全树路径命中 14 个（`internal/agent/tools/write_permission_test.go`），**但抽样未取到其内容**，`crush.md` D08 行的证据格是 `—`、等级 `unknown`（「看过路径、未读到正文」不是 `none`），故记 `unknown`、判定 `不可比` |
| D09 | full | unknown | 不可比 | 我方：`./bin/gopi tool sandbox create --label gap-crush --task-id 01a1ad3b-6592-75c1-a5b6-c962a60a25ec` → `rc=0`，打印 `sandbox /home/user/workspace/gopi-sandbox/sandbox-gap-crush-deb82a3d created=true reused=false root=/home/user/workspace/gopi-sandbox source=from the built-in default ledger=/home/user/workspace/gopi-sandbox/sandbox-gap-crush-deb82a3d/.gopi/memory.db owner=task=01a1ad3b-… pid=3469899 plugins=/home/user/.gopi/plugins`，末行是那个目录本身（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d09.txt`）⇒ 一次性、私有、带 owner 标记与自有账本的隔离 profile 是真建出来的目录，`GOPI_HOME` 指过去即成隔离执行环境，回收走 `gopi tool scratch-gc sandboxes`。对方：阶段 2 路径级命中 5 个（`internal/agent/isolate_test.go`），正文未取到 ⇒ `unknown`（`devdoc/competitor/crush.md` D09 行） |
| D10 | full | unknown | 不可比 | 我方：`./bin/gopi tool vision_analyze <capA.png>` → `rc=0`，输出首行 `【vision 分析结果】(模型 deepseek-flash, 输入 1010 tokens / 输出 2048 tokens, 图片 339150 bytes)`，其后是对该 1920×1080 截图的语义描述与分段文字识别（识别出 `api-docs.deepseek.com/zh-cn/guides/thinking_mode`、群聊窗口标题等）（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d10.txt`；capA.png 是本 run 在 D11 里自己截的整屏图）⇒ 图进来、结构化读数与语义结果出去。对方：阶段 2 路径级命中 8 个（`internal/message/attachment.go`），正文未取到 ⇒ `unknown`（`devdoc/competitor/crush.md` D10 行） |
| D11 | full | unknown | 不可比 | 我方：capture → act → capture 闭环，`./bin/gopi tool capture --screen --out capA.png` → `"sha256_16":"a4343afcfa9aec6f"`、`"bytes":339150`；`./bin/gopi tool act move 800 500` → `{"action":"move","display":":0","dry_run":false,"pointer":"800,500","focus":"0x2400011","root":"1920x1080","detail":"moved pointer to 800,500"}`；**独立客户端读回**：`DISPLAY=:0 xdotool getmouselocation` → `x:800 y:500`（注入真的落在 X 服务器上，不是只改了自家状态）；再 `./bin/gopi tool capture --screen --out capB.png` → `"sha256_16":"59b306173616704b"`、`"bytes":339290`，三条 `rc=0`，两张 PNG 整文件 sha256 分别为 `a4343afcfa9aec6ff5ff4b916444f44ebff98fca08ab6e475928543508d57736` 与 `59b306173616704b61dcf052d6f7bb902661cca1a940f9eccae3d062a85e091d`（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d11.txt`）⇒ 屏幕读取与指针注入两条腿都在，且改动被第二次截图与第三方客户端双向证明。对方：阶段 2 路径级命中 4 个（`internal/ui/dialog/quit_mouse_test.go`），正文未取到 ⇒ `unknown`（`devdoc/competitor/crush.md` D11 行） |
| D12 | full | partial | 我强敌弱 | 我方：`./bin/gopi web --addr 127.0.0.1:18097` 起服务后 `curl -s -m 10 -o web-body.html -w 'http=%{http_code} bytes=%{size_download}' http://127.0.0.1:18097/` → `http=200 bytes=69870`，`grep -o '<title>[^<]*</title>' web-body.html` → `<title>gopi web chat</title>`（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d12.txt` 与 `web-body.html`）⇒ 本地服务面真起、真有客户端拿到 200 与页面；起停各一次，端口用后释放（`ss -ltn` 复查）。对方：E1 `dsa-docs/crush.md:5` 原文「`Crush is a glamorous open-source AI coding agent that runs in your terminal, built by Charm. It supports multi`」+ E2 源码 `charmbracelet/crush@b18531d:internal/cmd/server.go:16` 原文「`"github.com/charmbracelet/crush/internal/server"`」（有独立 server 包与启动命令）⇒ 服务面在，无一次本地请求的 E3 ⇒ `partial` |
| D13 | full | partial | 我强敌弱 | 我方：`./bin/gopi session log 01a1ad3b-6592-75c1-a5b6-c962a60a25ec 1` → `rc=0`，打印 `SESSION 01a1ad3b-… — ROUND 1 of 17  (turn 1 of 1)`、`this session: 17 rounds (the denominator above); 17 content-carrying rounds`、`18 raw HTTP requests in the paired raw log`、`Usage:   prompt 63,674 tok (cache hit 58,880 / miss 4,794)  completion 116 tok`、`Tools:   memory`，其后逐轮给出工具名与参数（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d13.txt`）⇒ 轮次级 / 请求级 / 计费级三档都能按轮次读回。对方：E2 源码 `charmbracelet/crush@b18531d:internal/cmd/logs.go:85` 原文「`return fmt.Errorf("failed to tail log file: %v", err)`」⇒ 有日志文件的 tail 通道（`crush logs`），但「按轮次/按工具读回结构化记录」的读回面只有 E2 一行、无 E3 ⇒ `partial` |
| D14 | full | partial | 我强敌弱 | 我方：`printf 'hello world\n' > probe-tokens.txt; ./bin/gopi tool tokens --file probe-tokens.txt` → `/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/probe-tokens.txt: 3 tokens (12 bytes, 2 lines, deepseek-v4.1-bpe)`、`rc=0`（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d14.txt`）—— 同一输入给出确定整数并自带编码器名，可复算。对方：E2 源码 `charmbracelet/crush@b18531d:internal/cmd/stats.go:49` 原文「`Long:  "Generate and display usage statistics including token usage, costs, and activity patterns",`」⇒ 有 usage/cost 统计命令（`crush stats`），但其数字来自事后聚合的 E2 声明、无计量命令的 E3 ⇒ `partial` |
| D15 | full | partial | 我强敌弱 | 我方：`./bin/gopi tool code-search gapDocVerdictFor --root .` → `rc=0`，输出 `gapDocVerdictFor  internal/standards/gapdoc_contract_gate.go:201  func  func gapDocVerdictFor(our, opp string) (string, error) {`（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d15.txt`）—— 符号级定义定位一次调用即返回 `file:line` 与签名，且正打在本 ground 的源码上；全文搜索另有 `keyword_search` 通道。对方：E2 源码 `charmbracelet/crush@b18531d:internal/lsp/client.go:1` 原文「`package lsp`」（自建 LSP 客户端 = 符号级定位的实现面）+ E1 `dsa-docs/crush.md:5` 原文「`…built by Charm. It supports multi`」⇒ 符号级通道是真实现，全文检索侧未取到专门实现点、无 E3 ⇒ `partial` |
| D16 | full | unknown | 不可比 | 我方：`./bin/gopi memory search "gapdoc 六桶 差距文档" --limit 2` → `rc=0`，首位命中 `#28525 gap 文档（competitor 差距对比）的契约形态：17 行主表 + 六桶视图，不是四张表 tier=warm conf=1.00 lesson=lesson when=2026-11-05 23:15:18 sources=gopi repo,internal/standards/gapdoc_contract_gate.go + 9 份 devdoc/competitor/gap-*.md; task 01a1acc7-7c1d-75c0-841f-3012ec049ff0`，带 Q/A 正文与 keywords；第二条 `#28582`（`tier=warm conf=1.00`）（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d16.txt`）⇒ 跨会话记忆是可检索、带置信度与投票分数的持久库，不是静态提示文件。对方：阶段 2 路径级命中 4 个（`AGENTS.md`），正文未取到 ⇒ `unknown`（`devdoc/competitor/crush.md` D16 行） |
| D17 | full | partial | 我强敌弱 | 我方：`./bin/gopi task show 01a1ad3b-6592-75c1-a5b6-c962a60a25ec` → `rc=0`，打印 `status: in_progress`、`session:  01a1ad3b-… state=running crash=none turns=16 requests=18`、`runs: 1 run(s) — 0 completed, 1 with a raw-log checkpoint`、`run: 01a1ad3b-… 2026-11-06 07:53:23 unfinished 16 loop passes ← resume target`，以及 `session_log:` / `raw_log:` / `task_output:` / `run_dir:` 四条落盘路径与 `parent_task_id`、`tags`、`priority`（`/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/e3-d17.txt`）⇒ 任务行 + 断点续跑目标 + 父子依赖三项都能按 id 读回。对方：E2 文档(非实现) `charmbracelet/crush@b18531d:AGENTS.md:44` 原文「`ui/                              Bubble Tea v2 TUI (see internal/ui/AGENTS.md)`」（仓库内已有面向子 agent 的 `AGENTS.md` 分层约定，说明多 agent 协作面在文档层被组织），但认领/派发/回溯到行的编排面未取到 E2 正文、无 E3 ⇒ `partial` |

## 桶视图

| 桶 | 维度清单 |
|---|---|
| 敌有我无 | — |
| 敌强我弱 | — |
| 我有敌无 | D06 |
| 我强敌弱 | D02, D03, D04, D05, D12, D13, D14, D15, D17 |
| 平手 | D07 |
| 不可比 | D01, D08, D09, D10, D11, D16 |

六个桶互斥且并集恰为 17 维：`敌有我无` 0、`敌强我弱` 0、`我有敌无` 1（D06）、
`我强敌弱` 9（D02/D03/D04/D05/D12/D13/D14/D15/D17）、`平手` 1（D07）、`不可比` 6
（D01/D08/D09/D10/D11/D16）。空桶写 `—`，不用别的桶的内容填。

**两个空桶是口径的直接产物，不是实力结论**：对方上限 `partial`（不得为取证安装/运行
第三方 agent），我方 15 维 `full`、2 维 `partial`，`partial` 不严格高于 `partial`、
也追不上 `full`，所以 `敌有我无` 与 `敌强我弱` 在本口径下**结构性为空**。把这两个空集
读成「我们处处更强」是本表最典型的误读。

**六个 `不可比` 是取证缺口，不是对方弱**：D01 是对方双零探针、D08/D09/D10/D11/D16
是「路径命中但正文未取回」——`crush.md` 把这几维记成 `unknown` 而不是 `none`，正是
框架 §2 A4 要求的「没读到 ≠ 没有」。要收窄这六格，唯一合法途径是给它补 E2 正文取证
（`charmbracelet/crush@b18531d` 上重新取回对应文件），而不是在这里改等级。

## 结论

1. **形态**：本文件按 `internal/standards/gapdoc_contract_gate.go` 写 —— 一张 17 行主表
   + 一节六桶视图 + 本节。简报的「四张表各 17 行共 68 行」是门禁文件头部（`:7-8`）
   明确记为**已修复缺陷**的旧措辞（同一维在互斥两桶里重复），照写会同时判红判据 1 与
   判据 5；冲突与门禁原文已写进任务报告的 `## Not Done` 一节，不在文档里两套口径并存。
2. **判定的方向性**：17 维里 `我强敌弱` 9、`平手` 1、`我有敌无` 1、`不可比` 6，
   `敌有我无`/`敌强我弱` 结构性为 0。**没有一个 `我强敌弱` 是靠对方缺实现得出的** ——
   对方每一维都有 E1/E2 证据；差距来自通道等级（我方有 E3 实跑，对方口径上限 `partial`），
   这条差别是本对比表的形状本身，不是任何一维的发现。
3. **唯一一个 `我有敌无` 是 D06（会话语义）**，且它是 `none` 而非 `unknown`：`crush.md`
   的 D06 是**双零探针**（guide 0 命中 + 该 sha 1398 条路径 0 命中 + 抽样 0 命中）——
   这是「看过、没有」，与 D01 的「未取得输出」在口径上不同，两者不可互换。
4. **`平手` 只有 D07，且两侧机制不同**：我方是磁盘插件目录（8 个真实脚本、帮助面无声明、
   无 MCP），对方是文档里已规划的 hook 面（`docs/hooks/FUTURE.md:1` = `# Hooks: Future Work`）。
   等级口径分不出这个差，本表不给高下排序（§2 A4）。
5. **等级是快照**：对方列钉在 `charmbracelet/crush@b18531d`，我方列钉在 ground HEAD
   `86bceb86`（自编 `bin/gopi` 的 `revision` 与该 HEAD 逐字相同）。任一侧移动后本表的
   等级读数即过期，重跑 `/home/user/.gopi/sessions/01a1ad3b-6592-75c1-a5b6-c962a60a25ec/artifacts/gap/e3/` 下的命令即可复算我方 17 格。
