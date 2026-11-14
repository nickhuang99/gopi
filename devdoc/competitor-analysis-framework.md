<!-- doc-class: normative -->
# 竞争对比框架 — gopi 对 DeepSeek 官方 awesome-deepseek-agent 列表 23 家

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**本文件是规范（normative），不是对比结果。** 阶段 0 的唯一交付物：维度、评分口径、证据规范、
产物格式、23 家清单核对、以及阶段 1/2/3 各自的输入-输出契约。这里**不采集、不打分、不写对比表**。

- 对照语料：DeepSeek 官方 `awesome-deepseek-agent` 的 23 份集成指南（2026-11-05 HTTP 200 抓取），
  本地只读副本 `<session>/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/*.md`。
- 我方证据入口：`gopi <verb> -h` 与 `gopi --version` 的真实输出；源码只用于验证 help 背后是否真有实现。
- 本框架自身探针实测读数（census 性质）的生成器与实测点写在 §7.4，**任何引用这些数字的地方都必须带那两行**。

---

## 0. 三条铁律（贯穿全部阶段）

| # | 铁律 | 违反后果 |
|---|---|---|
| L1 | **无证据记 `unknown`，不猜。** `unknown` 与 `none` 是两件事：前者是"我没查/查不到"，后者是"查了，实现不存在"。 | 记成 `none` 会把"缺证据"说成"缺功能"，这是本框架最贵的错 |
| L2 | **每条声明带可复现命令或可定位 `file:line`。** 没有这两者之一的断言，不得写进任何表格。 | 一条无法复核的断言会污染后面每一个引用它的推理 |
| L3 | **help-only 是 bug，不是能力。** `gopi <verb> -h` 写了、源码里没有实现点 → 该维记 `none`，并按 Rule 41 单独提 `--tags bug` 行。 | 把 help 文本当能力，等于用自己的文档骗自己 |

**help-only 的判定顺序**（三步，缺一不可）：

1. 取 help 原文：`gopi <verb> -h`，存档到证据目录；
2. 在仓库里找实现点：`code_search <Symbol>` 或 `git grep -n '<help 里的关键串>' -- '*.go'`；
3. 命中数为 0 且没有旁证（配置键、测试、调用点）→ 判 `help-only`：该维按实现判 `none`，
   help 原文与零命中输出一起写进 `## Evidence`，并提一条 bug 行（Rule 41）。

---

## 1. 证据通道（E1 / E2 / E3）

每个维度都只在**这三条通道**上取证。通道决定证据类型，也决定证据等级上限。

| 通道 | 取证手段 | 证据类型 | 单独出现时的最高等级 |
|---|---|---|---|
| **E1 文档** | 对手：`dsa-docs/<tool>.md` 正文，`grep -n` 取行号。我方：`gopi <verb> -h` 输出存档 | `file:line`（文档）或命令 + 输出（help） | **partial**（doc-only，仅声明） |
| **E2 实现** | 对方：`git ls-tree` / `git grep -n` 在**固定 commit** 上（`owner/repo@sha`）。我方：`code_search` / `git grep -n -- '*.go'` | `file:line` + commit sha | **partial**（code-only，未被验证可运行） |
| **E3 运行** | 真的跑一次，记录命令与退出码。我方：`gopi tool sandbox run -- CMD` 里跑。对方：仅当该工具可零成本安装时，否则不出 E3 | 命令 + stdout/stderr + `exit=N` | **full** |

**升级规则（机械）**

- 只有 E1 → 上限 `partial`；
- 只有 E2 → 上限 `partial`；
- E1 + E2 一致 → 上限 `partial`（两条静态证据仍不等于能跑）；
- 任何一条 **E3 成功**（命令 `exit=0` 且输出含该维的正面证据）→ `full`；
- **E3 明确失败**（命令 `exit≠0` 且输出含拒绝/不存在语）→ `none`，且 E1/E2 的正面声明作废
  （"文档说有、实际跑不通"按 L3 记 `help-only` 类缺陷）；
- 三条通道全静默 → `unknown`。

**对方（23 家）的 E3 边界**：不得为了取证去安装/运行任意第三方 agent（Rule 27 与 Rule 35 同向）。
对方**只允许 E1 + E2**，因此**对方的最高分是 `partial`**。这不是妥协，是口径：
`partial` 对对方 = "有文档声明且有实现点"，`full` 那一档在本框架里**只对我方开放**，
记为 `full` 的对方案例必须是"文档 + 代码 + 一次真实安装运行的完整输出"三者齐备，
而本阶段不产出这类证据。→ 阶段 3 汇总时必须并列两个分数：`score_all`（含对方 partial）
与 `score_gopivs`（只在同为 E3 的维度上比），否则比较是拿满分的我方对上限为 partial 的对方。

> 补充口径（供阶段 3 使用，本阶段只定义）：若某对手在 E1 里明确写了安装命令且该命令是
> 单行 `npx`/`npm i -g`（可在私有 sandbox 内一次性执行、可回滚），阶段 3 允许对它做一次
> E3，并在证据里记 `sandbox=<path>` 与自动回收时机；未做就是 `partial`，不需解释。

---

## 2. 评分口径（四级，逐级一句判定标准）

**四级：`full`(2) / `partial`(1) / `none`(0) / `unknown`(不计分)**

| 级 | 一句判定标准（可机械执行） | 判据落到哪 |
|---|---|---|
| `full` | 存在一条**本 run 亲自执行过**的命令，其 `exit=0` 且输出中出现该维 §3 表「full 证据串」列指定的字符串（大小写不敏感），并且该命令与证据串都已存档。 | E3 |
| `partial` | 没有 E3 证据，但存在 ≥1 条 E1 或 E2 证据：E1 是 `<file>:<line>` 命中 §3「doc 探针」正则，E2 是 `owner/repo@sha:path:line` 命中 §3「code 探针」正则。 | E1 或 E2 |
| `none` | 存在一条 E3 命令 `exit≠0` 且输出命中 §3「拒绝串」正则；或该维的 E1 与 E2 探针在**覆盖了该维的语料**上命中数均为 0（即"看过、没有"）。 | E3 反面，或 E1+E2 双零 |
| `unknown` | 该工具/我方该 subsytem 的 E1、E2、E3 **三条通道都没有被执行过**（语料里根本没有这个话题，且没有跑命令）。 | 无 |

**四条附加判据（把边界钉死）**

- **A1 不允许"部分覆盖"混进 `full`**：`full` 要求 §3「full 证据串」出现；只出现同义词不算。
  证据串必须是**命令输出里真实存在的字面串**，不接受解释。
- **A2 `none` 必须能指认拒绝**：光"我没找到"不配 `none`；要么给出 `exit≠0` 的拒绝输出，
  要么给出"在这份语料/这个 commit 上正则命中 0"的 `grep -c` 读数。两者都没有 → `unknown`。
- **A3 分母**：某维在 23 家上的均值只用 `full|partial|none` 计（0/1/2），`unknown` **从分母剔除**，
  并单独报 `unknown` 计数。报均值必须同时报分母（`n=`）。
- **A4 不可比就不比**：两个对象若在同一维上都是 `unknown`，该维在该对比较里**丢弃**，
  不写成"打平"。

**禁止措辞**（写进检查：产物中下面这条命令的输出必须为 0；正则用字符类拼写，等价于逐个列出禁用词）：

```bash
LC_ALL=C.UTF-8 grep -nE '主[观]|大[概]|差[不]多|看[情]况|感[觉]|较[好]|较[强]' \
    analysis/stage2/*.md analysis/stage3/*.md
# 期望：无输出（exit=1）
# LC_ALL=C.UTF-8 不可省：在 C locale 下 `主[观]` 按字节匹配，会误命中任何以 `主` 开头的词
# （本框架自检时实测：`主路径` 被误报一次）。
```

---

## 3. 十七个维度

格式：`id | 名称 | 一句话定义 | doc 探针（E1） | code 探针（E2） | full 证据串（E3） | 拒绝串 | 证据类型`

`doc` 探针对**对手文档**与**我方 help 输出**都适用；`code` 探针对**对手 repo（固定 sha）**与**我方仓库**都适用。
下方正则均已在语料上实测过命中分布（见 §7.4），命中数低**不是缺陷**——它表示该维在多数对手上是 `none` 或 `unknown`，
正是要区分开的东西。

### A 组：接入与运行面

| id | 名称 | 一句话定义 | doc 探针（E1） | code 探针（E2） | full 证据串（E3） | 拒绝串 |
|---|---|---|---|---|---|---|
| **D01** | 安装分发 | 从零到可执行的**一条**具名安装命令（包管理器或脚本），且该命令对 OS 有明确分支 | `npm (install\|i) -g\|pip3? install\|pipx install\|brew install\|irm .*\|go install\|uv tool install\|npx [a-z@/-]+` | `package.json` 的 `bin` 字段，或 `pyproject.toml` 的 `[project.scripts]` | 安装后 `<bin> --version` 输出中出现语义化版本号 | `command not found\|no such file` |
| **D02** | 零配置首跑 | 首次启动**不需要**先手写配置文件/key，靠向导或环境变量即可进入可用状态 | `wizard\|prompts for it\|API Key\|api_key\|env\|export ` | 首启读取 key 的代码分支（`os.Getenv` 同族 / 配置向导函数） | 首跑命令 `exit=0` 且输出出现 `ready\|welcome\|starting` 之类就绪行 | `missing.*(key\|token)\|not configured` |
| **D03** | 模型/供应商可换 | 能用自定义 `base_url` 或等价机制指向非默认供应商（含 DeepSeek 兼容端点） | `base_?url\|BASE_URL\|ANTHROPIC_BASE\|OPENAI_BASE\|api[_-]?key\|API Key\|custom model` | 读取 base_url 的代码点 | 环境变量改 base_url 后一次真实请求 `exit=0` | `unsupported provider\|invalid base` |
| **D04** | 配置可检查面 | 配置有**单一文档化载体**，且有一条命令能打印生效值（不是我读文件猜） | `settings\.json\|config\.(json\|toml\|yaml\|yml)\|~/\.([a-z0-9-]+)/` | 配置解析入口（一个函数） | `gopi config show` 同族命令 `exit=0` 且输出含键与值两列 | `no such command\|unknown key` |
| **D05** | 非交互/一次性执行 | 有一次调用即返回的 headless 模式（供脚本/CI 用），不需要先开 REPL | `--print\|--json\|--output-format\|non-?interactive\|headless\|print mode` | 非交互主路径的入口函数 | `echo "<prompt>" \| <bin> [flags]` 一次 `exit=0` 并打印答案 | `requires a terminal\|interactive only` |
| **D06** | 会话语义 | 会话可**列、可查、可续**（持久化到磁盘且能被命令读回） | `--resume\|--continue\|/resume\|session[s]? (list\|show)\|--session` | 会话文件写入点 + 读取点（各一个符号） | 续跑命令 `exit=0` 且输出含上一轮的上下文痕迹 | `no session\|session not found` |

### B 组：能力面

| id | 名称 | 一句话定义 | doc 探针（E1） | code 探针（E2） | full 证据串（E3） | 拒绝串 |
|---|---|---|---|---|---|---|
| **D07** | 外部能力扩展 | 有第三方可写的扩展点（MCP server / plugin / hook / skill 之一），且有加载路径 | `\bMCP\b\|mcp server\|model context protocol\|plugin\|hook\|extension\|skill` | 扩展点目录或注册表符号 | 加载一个扩展后，命令输出出现该扩展名 | `no plugin support\|unknown extension` |
| **D08** | 命令审批门 | 对要执行的命令/写操作有**可配置的**放行策略（allow / ask / deny），而非全放行或全禁止 | `approv\|permission\|allowlist\|allow_list\|--yolo\|dangerously\|ask before` | 策略枚举 + 判定函数（各一个符号） | 同一命令在两种策略下得到两种结果（一次允许、一次被拒绝），两次输出都存档 | `not permitted\|refused` |
| **D09** | 执行隔离 | 有一次性、私有、可回收的执行环境（sandbox / 隔离 profile / worktree），而非在宿主目录直接跑 | `sandbox\|isolat\|container\|docker\|worktree` | sandbox 创建与回收各一个符号 | `sandbox create` 同族命令打印出路径，且该路径下存在私有 profile 文件 | `not supported\|no sandbox` |
| **D10** | 图像输入 | 能把图片作为输入交给模型（截图/OCR/图表），并有明确的入口参数 | `screenshot\|\.png\|image\|\bvision\b\|multimodal\|attach` | 图像输入参数或 MIME 处理分支 | 一次带图片的调用 `exit=0` 且输出描述了图内容 | `does not support images\|text only` |
| **D11** | 屏幕驱动 | 能把"看屏幕"与"操作鼠标键盘"接成闭环（capture → act），而非只能读图 | `browser\|playwright\|puppeteer\|chrome\|selenium\|click\|mouse` | 输入注入实现（X11/XTEST/无障碍 API）符号 | capture 一次 + act 一次，两次 `exit=0`，且第二张截图与第一张的 sha256 不同 | `no display\|X11 only.*unavailable` |
| **D12** | 本地服务面 | 能作为本地服务对外提供能力（HTTP UI / API / 协议代理），供别的程序接 | `serve\|--port\|localhost:\|HTTP (server\|API)\|API server` | `net/http` 服务启动点 | 起服务后本地请求得到 200 且响应体含页面或 JSON | `address already in use\|bind.*failed` |

### C 组：工程化面

| id | 名称 | 一句话定义 | doc 探针（E1） | code 探针（E2） | full 证据串（E3） | 拒绝串 |
|---|---|---|---|---|---|---|
| **D13** | 可审计痕迹 | 每次运行留下**结构化**记录（jsonl/DB/事件流），且有命令能按轮次/工具读回 | `jsonl\|transcript\|--verbose\|--debug\|log (file\|path)\|trace\|history` | 结构化写入点 + 读取器（各一个符号） | 读回命令 `exit=0` 且输出含至少一条带工具名的记录 | `no log\|nothing recorded` |
| **D14** | 计量面 | 能数 token 与/或成本，且数字来自与模型计费一致的编码器或服务端回报 | `token (count\|usage\|limit)\|--usage\|/cost\|cost\|credits\|billing` | tokenizer 或 usage 解析点 | 计量命令 `exit=0` 且对同一输入打印出确定的整数 | `unavailable\|not counted` |
| **D15** | 代码智能 | 有符号级定位（定义）与全文搜索两种检索，而非只能读整个文件 | `LSP\|language server\|symbol\|codebase index\|indexing\|grep` | 符号索引构建点 + 全文搜索后端（各一个） | 一次符号定义查询 `exit=0` 且返回 `file:line` | `symbol not found\|index unavailable` |
| **D16** | 跨会话记忆 | 有**持久**知识库（不是上下文窗口），可被检索、可写回，且与任务账本同源或可导出 | `memory\|memories\|knowledge\|learn\|CLAUDE\.md\|AGENTS\.md\|rules file` | 记忆存储读写符号（各一个） | 写一条后检索命中同一条，两次命令都存档 | `no memory\|empty store` |
| **D17** | 任务编排 | 有跨会话的任务队列/多 agent 派发（可认领、可回溯到行），而非只能单轮对话 | `sub-?agent\|multi-?agent\|Task tool\|delegat\|/-?agent\|worker\|queue` | 任务行写入点 + 认领/派发点（各一个） | 提交一条任务后能按 id 读回该行，两条命令均 `exit=0` | `no queue\|ledger unavailable` |

**维度数量：17**（落在 12–18 的区间内）。计数用一行命令复核：

```bash
grep -cE '^\| \*\*D[0-9]{2}\*\* \|' devdoc/competitor-analysis-framework.md
```

---

## 4. 证据规范

### 4.1 证据目录（唯一合法位置）

```
~/.gopi/sessions/<this-run-session-id>/artifacts/
  stage2/<tool_id>/                 # 每个被比对对象的原始证据
    e1-guide.txt                    # guide 的 grep -n 命中行
    e2-repo.txt                     # owner/repo@sha 的 git grep 命中行
    e3-run.log                      # 命令 + exit=N + 输出原文
    help.txt                        # 我方的 `<verb> -h` 全量存档
  stage3/matrix.md                  # 汇总表（阶段 3 产物）
  stage3/unknowns.md                # 全部 unknown 的清单与原因
```

**证据不进 git 树。** 一条证据如果只能靠"我记得当时跑了"复述，就不是证据：
必须落成文件，路径写进表格的 `evidence` 列。

### 4.2 一条证据的最小形状

```
# 维 D09 — 我方 — E3
$ gopi tool sandbox create --label probe --task-id 01a1aa6c
/home/user/workspace/gopi-sandbox/sandbox-20261105-141233
exit=0
```

**四行，缺一不可**：维度 id、原始命令（可直接粘贴）、输出原文（可截断但需标注行数）、`exit=N`。
截断必须写清省了多少行与怎么取回（`GOPI.md` 2.6），例如
`输出共 214 行，此处保留前 3 行，完整内容用 gopi tool sandbox create --label probe 重跑取得`。

### 4.3 file:line 的形状

- 我方：`internal/tools/<file>.go:NNN`（仓库相对路径，行号以当前 `HEAD` 为准，附 `git rev-parse HEAD` 短 sha）；
- 对方：`owner/repo@<sha7>:path:NNN`（**必须带 sha**，否则 main 一动，行号就失效）。

### 4.4 unknown 的记法

```markdown
| D08 | unknown | — | 语料无审批话题，且未运行该工具；E1/E2/E3 三通道均未执行 |
```

**`unknown` 必须写原因**，原因只有三种合法取值：`语料无此话题` / `未运行（成本/权限）` /
`命令不可用`。写别的推断性说法（例如"确实没有，虽然没查"）即违约。

---

## 5. 产物格式与命名约定

### 5.1 目录与文件

```
analysis/
  stage2/                      # 阶段 2 产物：一 agent 一文件
    <tool_id>.md
  stage3/
    matrix.md                  # 阶段 3 产物：一张汇总表
    unknowns.md
```

- `tool_id` = 23 家清单里的工具名，**小写、空格与点替换为 `-`**，其余字符不动
  （例：`deepseek-tui`、`oh-my-pi`、`github-copilot`、`cherry-studio`）。
- 我方固定 `tool_id = gopi`。
- **文件名即身份**：一 agent 一文件，两个采集者永远不会撞同一个文件（Rule 5b：没有共享索引表）。

### 5.2 stage2 单文件格式（照抄即可）

```markdown
# <tool 显示名>（<owner/repo@sha7>）

<!-- meta -->
repo: <owner/repo>
sha: <sha7>
guide: <dsa-docs 里的文件名，或 "none">
url_in_guide: <guide 里写的 URL，或 "absent">
pinned_sha_verified_at: <UTC 时间戳>

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1+E2 | dsa-docs/claude_code.md:23 + anthropics/claude-code@<sha>:package.json:8 | 装的是 npm 包 |
| D02 | unknown | — | — | 语料无此话题，且未运行 |
```

**列头规则**：`stage2` 用**长格式**（一行一维），因为要写证据；`stage3/matrix.md` 用**宽格式**，
列头**直接用维度 id**：

```markdown
| tool_id | D01 | D02 | D03 | D04 | D05 | D06 | D07 | D08 | D09 | D10 | D11 | D12 | D13 | D14 | D15 | D16 | D17 | score | n |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| gopi | full | full | partial | full | full | full | none | full | full | full | full | full | full | full | full | full | full | 30 | 17 |
```

`score` = 把 `full=2 / partial=1 / none=0` 相加（`unknown` 记 `-` 且不计入 `n`），`n` = 参与计分的维数（A3）。

### 5.3 一票否决的格式规则

- 列头**必须是维度 id 本身**（`D01`），不允许写成 `安装分发` 之类自然语言列名——阶段 3 要能 `awk` 出来。
- 表格里**不允许出现合并单元格、脚注符号、颜色标记**（Markdown 方言差异会让机械解析崩掉）。
- 每个文件必须能被 `grep -c '^| D[0-9]'` 数出**恰好 17 行**。

---

## 6. 23 家清单核对表

核对方法（可复现）：对每家取 guide 中出现的 GitHub URL，再解析其真实仓库（`git ls-remote` / GitHub API 的
`full_name` + `archived`），HTTP 非 200 记缺失。核对结果落
`artifacts/urls/final.tsv`（24 行 = 1 行表头 + 23 行）。

| # | 工具名 | GitHub 地址（核对后） | 本地 guide 文件 | URL 缺失 / 异常 |
|---|---|---|---|---|
| 1 | astrbot | `https://github.com/AstrBotDevs/AstrBot` | `astrbot.md` | — |
| 2 | cherry_studio | `https://github.com/CherryHQ/cherry-studio` | `cherry_studio.md` | — |
| 3 | claude_code | `https://github.com/anthropics/claude-code` | `claude_code.md` | **guide 内无 URL**，仓库由工具名核对得出 |
| 4 | cline | `https://github.com/cline/cline` | `cline.md` | **guide 内无 URL** |
| 5 | codex | `https://github.com/openai/codex` | `codex.md` | — |
| 6 | crush | `https://github.com/charmbracelet/crush` | `crush.md` | **guide 内无 URL** |
| 7 | deepcode | `https://github.com/lessweb/deepcode-cli` | `deepcode.md` | 仓库名与工具名不同（`deepcode-cli`） |
| 8 | deepseek-tui | `https://github.com/codewhale-hq/Codewhale` | `deepseek-tui.md` | **已改名**：guide 写的是 `deepseek-tui`，真实仓为 `Codewhale`（重定向 200） |
| 9 | github_copilot | `https://github.com/microsoft/vscode-copilot-chat` | `github_copilot.md` | **guide 给的是第三方扩展**；官方仓 `archived=true` |
| 10 | copilot_cli | `https://github.com/github/copilot-cli` | `copilot_cli.md` | **guide 内无 URL** |
| 11 | hermes | `https://github.com/NousResearch/hermes-agent` | `hermes.md` | **guide 内无 URL** |
| 12 | kilo_code | `https://github.com/Kilo-Org/kilocode` | `kilo_code.md` | **guide 内无 URL** |
| 13 | langcli | `https://github.com/LangcliTeam/langcli` | `langcli.md` | **guide 内无 URL**；星标 238，最小仓之一 |
| 14 | lobehub | `https://github.com/lobehub/lobehub` | `lobehub.md` | — |
| 15 | nanobot | `https://github.com/HKUDS/nanobot` | `nanobot.md` | **guide 内无 URL** |
| 16 | oh-my-pi | `https://github.com/can1357/oh-my-pi` | `oh-my-pi.md` | — |
| 17 | openclaw | `https://github.com/openclaw/openclaw` | `openclaw.md` | — |
| 18 | opencode | `https://github.com/anomalyco/opencode` | `opencode.md` | **guide 内无 URL**；仓库现属 `anomalyco`（`sst/opencode` 重定向） |
| 19 | pi_mono | `https://github.com/earendil-works/pi` | `pi_mono.md` | **已改名**：`pi_mono` → `pi` |
| 20 | qoder | **（无官方 GitHub 仓库）** | `qoder.md` | **缺失**：`QoderAI` org 仅有 changelog 仓；产品是闭源 IDE/CLI，分发走 `https://qoder.com/install` |
| 21 | qwen_code | `https://github.com/QwenLM/qwen-code` | `qwen_code.md` | — |
| 22 | reasonix | `https://github.com/esengine/DeepSeek-Reasonix` | `reasonix.md` | **guide 内无 URL**；仓库名 `DeepSeek-Reasonix` 与工具名不同 |
| 23 | workbuddy | **（无官方 GitHub 仓库）** | `workbuddy.md` | **缺失**：`Tencent` org 无 CodeBuddy/WorkBuddy 仓，404 |

**缺失 2 家，显式列出**：`qoder`、`workbuddy`。这两家**没有可 pin 的 sha**，
因此阶段 2 对它们**只能走 E1**，E2 通道结构性不可用，其等级上限为 `partial`；
若 guide 也缺该维话题，则**必须记 `unknown`**（原因：`语料无此话题` + `E2 通道不存在`），
不得因为"闭源"就记 `none`。

**guide 内无 URL、但仓库能核对到的 10 家**（逐条列出，不多不少）：
`claude_code`、`cline`、`crush`、`copilot_cli`、`hermes`、`kilo_code`、`langcli`、
`nanobot`、`opencode`、`reasonix`。
它们的仓库地址由工具名 + 官方域名 + `git ls-remote` 核对得出，`url_in_guide` 列一律填 `absent`，
**不得**把核对结果写成"guide 给了 URL"。
另加 `qoder`、`workbuddy` 两家 guide 内同样无 URL 且**仓库根本不存在**，共 12 家 `guide_has_github = no`
（`grep -c 'no$\|no\t' artifacts/urls/final.tsv` 之外，用 `awk -F'\t' '$3 ~ /^no/ {n++} END {print n}' artifacts/urls/final.tsv` 复核应为 12）。

---

## 7. 阶段契约（输入 → 输出）

三个阶段的边界是**契约**，不是建议。每一阶段的产物必须能被下一阶段**机械消费**。

### 7.1 阶段 1 — 维度冻结（本阶段已产出，后续阶段只读）

| | |
|---|---|
| **输入** | 本文件（`devdoc/competitor-analysis-framework.md`）、23 份 guide、`artifacts/urls/final.tsv` |
| **输出** | 本文件本身。**冻结标记**：§3 的 17 行维度表 + §2 四级口径 + §4 证据规范，三者任一被改动即视为阶段 1 重开 |
| **完成判据** | `grep -cE '^\| \*\*D[0-9]{2}\*\* \|'` = 17；`grep -cE '^\| [0-9]+ \| '` 的清单段 = 23；正文 `grep -cE '主[观]|大[概]|差[不]多|看[情]况'` = 0 |
| **不做** | 不采集、不打分、不写任何 stage2 文件 |

### 7.2 阶段 2 — 单对象采集（一 agent 一文件）

| | |
|---|---|
| **输入** | 冻结后的本文件（维度定义与探针正则）、一个 `tool_id`、该 tool 的 `owner/repo@sha`（从 `final.tsv` 取；`NO_OFFICIAL_REPO` 的走纯 E1） |
| **输出** | `analysis/stage2/<tool_id>.md`，格式见 §5.2；证据落 `artifacts/stage2/<tool_id>/` |
| **每条行的产出** | `维度 id / 等级(full\|partial\|none\|unknown) / 证据等级(E1\|E2\|E3\|组合) / 证据(命令 或 file:line) / 备注` |
| **完成判据** | 该文件能被 `grep -c '^\| D[0-9]'` 数出**恰好 17** 行；每行 `等级 ∈ {full,partial,none,unknown}`；`partial`/`full` 行的证据列非空；`unknown` 行的备注列命中 §4.4 的三种合法原因 |
| **禁止** | 一个文件写多个 tool；表格里出现自然语言列名；把 `unknown` 折算成 `none`；为一维写两行 |
| **单文件预算** | 一个 tool 的采集必须能在**一次** run 内完成（≤ 60 min），否则按 §7.4 拆成两个 tool 的文件，不拆维度 |

### 7.3 阶段 3 — 汇总与差距

| | |
|---|---|
| **输入** | 全部 `analysis/stage2/*.md`（至少含 `gopi.md` 与 23 家）、本文件 §2 口径 |
| **输出** | `analysis/stage3/matrix.md`（宽表，列头 = 维度 id，见 §5.2 的下半张表）+ `analysis/stage3/unknowns.md`（全部 `unknown` 的 `tool_id × 维度` 与原因）+ 一段**逐维差距叙述**（每个维度一行：`D0x — 我方 <级> / 对方众数 <级> / 差距一句话 / 证据指向的 stage2 文件`）+ 每个对手一份 `devdoc/competitor/gap-<tool_id>.md`，形态见 **§7.3.1 的契约**（17 行一维一行、判定列机械算出、桶视图互斥且覆盖全部 17 维） |
| **完成判据** | 行数 = 24（`gopi` + 23）+ 1 行表头；列数 = 1（`tool_id`）+ 17（维度）+ 2（`score`,`n`）= 20；`unknowns.md` 的条目数 ≥ matrix 中 `unknown` 单元格数（少一条即漏记） |
| **禁止** | 用 `unknown` 参与均值（A3）；把两个 `unknown` 写成"打平"（A4）；改动 stage2 的等级（stage3 只读，发现矛盾就回阶段 2 重采，并在 `unknowns.md` 里记一次）；把同一张 17 维全表复制进多个桶（§7.3.1 ①） |

#### 7.3.1 每个对手的差距文档契约（`devdoc/competitor/gap-<tool_id>.md`）

> **与冻结面的关系**：§7.1 的冻结标记只覆盖 §3 的 17 行维度表、§2 的四级口径、§4 的
> 证据规范三处。**本节不在冻结集内** —— 修改本节不重开阶段 1，也不改动 §2/§3/§4
> 任何一个字。这是本节能被写成契约而不必走重采流程的原因，写在这里以免后人误判。

本契约是**可机械检查**的：`go test ./internal/standards/ -run TestGapDoc` 判它，判定
列的对错不是人说了算。三条要求，缺一即红。

**① 主表恰好 17 行，一维一行。** 一维在全文中只有**一行完整内容**；不允许同一维写两行，
也不允许把同一张全表复制多份。"视图"不是"副本"。

| 维度 | 我方等级 | 对方等级 | 判定 | 证据（命令 或 file:line:行号） |
|---|---|---|---|---|
| `D01` 到 `D17`（逐个列出，共 17 行） | §2 四级之一 | §2 四级之一 | 由 ② 机械算出 | 该维的原文证据，逐字保留 |

**② 判定列不手写，它由 `(我方级, 对方级)` 唯一决定。** 该函数是**全函数**且**互斥**：
十六种等级组合每一种都恰好落进一个桶，所以作者手上不存在"没有家"的行。

| 我方 \ 对方 | `full` | `partial` | `none` | `unknown` |
|---|---|---|---|---|
| `full` | 平手 | 我强敌弱 | 我有敌无 | 不可比 |
| `partial` | 敌强我弱 | 平手 | 我有敌无 | 不可比 |
| `none` | 敌有我无 | 敌有我无 | 平手 | 不可比 |
| `unknown` | 不可比 | 不可比 | 不可比 | 不可比 |

**这张表为什么有六个桶而不是四个。** 历史上只用了 `敌有我无 / 敌强我弱 / 我有敌无 /
平手` 四个名字，而它们**不是全函数**：`full` 对 `partial`（两份已交付文档里都是 15/17 行）
在四桶下**没有落点**。于是作者只能把整张表复制进每个桶，四张表各自 17 行、合计 68 格，
互相矛盾的行随即出现 —— `devdoc/competitor/gap-astrbot.md` 与 `gap-cline.md` 都曾把
`我方=none` 的 D01 列进「我有敌无（我方有、对方 `none`）」，而该桶的入表条件就是
「我方有」。**补齐 `我强敌弱` 与 `不可比` 不是一个新口径，它是把已有口径写全**：
`不可比` 正是 §2 A4 明令不许写成"打平"的那种情形。

**③ 桶视图的并集必须等于 `D01..D17`，每维恰好一次，且每个桶都要被声明。**
空桶写 `—`：一个**被省略**的桶与一个**被忘记**的桶在机器眼里无法区分，所以省略即违例。

```markdown
## 桶视图

| 桶 | 维度清单 |
|---|---|
| 敌有我无 | D01 |
| 敌强我弱 | — |
| 我有敌无 | — |
| 我强敌弱 | D02, D03, D04, D05, D06, D08, D09, D10, D11, D12, D13, D14, D15, D16, D17 |
| 平手 | D07 |
| 不可比 | — |
```

**可复制的自检命令**（在仓库根跑；第一条必须每条都打印 `1`，否则就是副本形态）：

```bash
# ① 主表行数 = 17，且每维全文恰好出现一次
grep -cE '^\| D[0-9]{2} \|' devdoc/competitor/gap-<tool_id>.md
for d in $(seq -w 1 17); do
  printf "D%s %s\n" "$d" "$(grep -cE "^\| D$d \|" devdoc/competitor/gap-<tool_id>.md)"
done | awk '$2!=1{print "VIOLATION: "$0; bad=1} END{exit bad}'

# ② 桶视图的并集 == 17 且每维一次
#    表头行与分隔行以「桶」/「---」开头，用 grep -v 去掉；用 tail 去会连第一条数据一起丢，
#    而丢掉的那条恰好是第一个桶（实测：少算 D01，dims=16 却仍报 OK）。
awk '/^## 桶视图/{f=1;next} f&&/^\| /{print}' devdoc/competitor/gap-<tool_id>.md \
  | grep -vE '^\| *(桶|---)' | cut -d'|' -f3 | tr ',' '\n' | tr -d ' `' \
  | grep -E '^D[0-9]{2}$' | sort | uniq -c \
  | awk '$1!=1{print "VIOLATION: "$0; bad=1} END{printf "dims=%d\n", NR; if(NR!=17||bad) exit 1}'

# ③ 判定列与 (我方,对方) 一致 —— 交给门禁，不要肉眼比
go test ./internal/standards/ -run TestGapDocContractOnTheRealCorpus -count=1 -v
```

**门禁在判什么、不在判什么。** 它判**形态**与**入表逻辑**：行数、唯一性、判定列的
机械来源、桶的互斥与覆盖。它**不判**任何一格的等级或证据对不对 —— 那是阶段 2/3 的
采集判断，一个正则无法重判已采集的证据，硬判只会用错误去改正确的读数。所以
一条等级有争议时，改的是采集，不是让文档迁就门禁。

### 7.4 本框架自身的探针读数（census，带生成器与实测点）

**Rule 38 要求**：本节每个数字都可由下面两条命令重跑得到，且实测点明确。

- 实测点：本 ground 基线 `f43cd82`（`git rev-parse HEAD` 当时的 head）；
  我方二进制 `gopi --version` → `v0.4.0`，`revision: 43c6724c7b362122c616792898d1ea7fb98cdf28`。
- 生成器（两个，均在 evidence 目录里已存）：

```bash
# G1：17 维 doc 探针在 23 份 guide 上的命中数 —— 探针取自本文件 §3 表格第 3 列，
# 文档是探针的唯一真源（不存在第二份探针表），生成器脚本落在
# <session>/artifacts/gen-e1.sh
bash <session>/artifacts/gen-e1.sh \
     devdoc/competitor-analysis-framework.md \
     <session>/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs \
     <session>/artifacts/e1-hits-from-doc.tsv

# G2：我方 17 维 E3 探针的退出码
for c in "gopi -h" "gopi config show" "gopi chat -h" "gopi tool code-search -h" \
         "gopi tool sandbox show" "gopi session resume -h" "gopi memory search -h" \
         "gopi task submit -h" "gopi web -h" "gopi tool tokens -h" "gopi tool capture -h" "gopi worker -h"; do
  $c >/dev/null 2>&1; echo "$c rc=$?"; done
```

实测读数（G1，口径 = 命中份数 / 23；原始输出 `artifacts/e1-hits-from-doc.tsv`）：

| 维度 | 命中 | 维度 | 命中 | 维度 | 命中 |
|---|---|---|---|---|---|
| D01 | 16 | D07 | 15 | D13 | 2 |
| D02 | 23 | D08 | 2 | D14 | 4 |
| D03 | 23 | D09 | 2 | D15 | 1 |
| D04 | 10 | D10 | 8 | D16 | 2 |
| D05 | 1 | D11 | 7 | D17 | 3 |
| D06 | 1 | D12 | 7 | | |

实测读数（G2）：12 条探针全部 `rc=0`。

**这两组数字的用途只有一个**：证明每个维度的探针**在真实语料上可执行且能区分**。
D05、D06、D15 只在 1 份 guide 上命中，D08/D09/D13/D16 各 2 份，D17 只 3 份 —— 这六维在多数对手上
会是 `none` 或 `unknown`，正是阶段 2 要逐家分开的东西，**不是**"这六维没用"。
反过来 D02/D03 命中 23/23：这两维在语料上**没有区分度**，阶段 2 对它们的结果会几乎全是
`partial`，因此它们的差距信息量最低 —— 这一点必须在阶段 3 的叙述里写明，不能用它们撑结论。
**这条读数本身是推论，不是结论**：它只说"探针在这份语料上命中多少份"，不说"该工具没有这个能力"。

---

## 8. 己方（gopi）取证的固定落点

我方 17 维的 E3 探针，命令与退出码都固定，便于阶段 2 逐维照抄：

| 维度 | 探针命令（我方） |
|---|---|
| D01 | `gopi --version`（含 `revision:` 行） |
| D02 | `gopi config show` |
| D03 | `gopi config show`（键 `model` / `vision_model` / `api_key` 三行） |
| D04 | `gopi config show` + `gopi --help-env` |
| D05 | `echo "<prompt>" \| gopi --output-language zh-CN "<prompt>"` |
| D06 | `gopi session -h`、`gopi session resume -h`、`gopi session log -h` |
| D07 | 全 help 面 `grep -ci mcp`（当前为 0）+ `ls ~/.gopi/plugins/` |
| D08 | `gopi chat -h`（`--policy POSTURE` 段） |
| D09 | `gopi tool sandbox show`、`gopi tool sandbox -h` |
| D10 | `gopi tool vision_analyze -h` |
| D11 | `gopi tool capture -h` + `gopi tool act -h` |
| D12 | `gopi web -h`、`gopi tool proxy -h` |
| D13 | `gopi session log -h`、`gopi session stats -h` |
| D14 | `gopi tool tokens -h` |
| D15 | `gopi tool code-search -h`、`gopi tool keyword_search -h` |
| D16 | `gopi memory -h`、`gopi memory search -h` |
| D17 | `gopi task -h`、`gopi worker -h` |

**D07 的现状必须如实写进阶段 2**（三行实测，各自可重跑）：

| 观测 | 命令 | 读数 |
|---|---|---|
| help 面无 MCP 声明 | `for c in "gopi -h" "gopi tool -h" "gopi chat -h" "gopi config -h" "gopi task -h" "gopi session -h"; do $c; done \| grep -ci mcp` | `0` |
| help 面对 `plugin` 的唯一出现在一段散文里，不是扩展点声明 | `gopi --help-env 2>&1 \| grep -in plugin` | 1 行（第 366 行，讲 profile 可见性的散文） |
| 扩展点确有实现 | `git ls-files internal/config/plugins.go cmd/agent/cli_websearch.go` 与 `ls /home/user/.gopi/plugins/websearch/scripts/` | 两个实现文件在索引里；插件树有 `arxiv_search.py`、`bing_search.py`、`deepseek_ask.py`、`github_search.py` |

按 §1 升级规则，这一维在我方是 `E2 有实现点（plugin 树解析 + web 工具接线）+ E1 无扩展点声明`
→ **上限 `partial`**，不得记 `full`（没有 E3 运行证据），也不得因为"没有 MCP 字样"就记 `none`
——那是把"用另一种机制实现"判成"没有扩展点"，正是 L3 的反面：拿 help 缺声明去否定代码里的实现。

---

## 9. 本框架的已知边界（写清，不掩盖）

1. **对方拿不到 `full`**：§1 的 E3 边界决定了对方上限 `partial`。阶段 3 必须并列
   `score_all` 与 `score_gopivs` 两个数，否则是拿满档对半档。
2. **两家无仓库**（`qoder`、`workbuddy`）结构性缺 E2，等级上限 `partial`，且更常见的结局是 `unknown`。
3. **`doc` 探针是正则，不是语义理解**：它只判"这个话题在文档里出现过"，不判"说得好不好"。
   这是刻意的——可机械判定优先于覆盖率。
4. **本框架不覆盖**：性能、稳定性、安全审计、许可证、社区活跃度。这些维度要么不可机械判定，
   要么需要本阶段无法获得的数据；把它们硬塞进来会让"可判定"这条塌掉。
5. **17 维的权重一律相等**（A3 的简单相加）。加权需要依据，本阶段没有依据，所以不做。
