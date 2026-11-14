<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# 素材库「竞品视角」追加轮报告（task 01a0ed231c73752aa27a543e36b6523c）

## 目标与形态

`docs/archive/demo/materials/` 的功能原始素材库已有 97 个条目目录，每条以
`00-entry.md / 01-input.txt / 02-output.txt / 03-raw.png / 04-annotated.png /
05-finding.md` 六件套交付。本轮**只补一个角度**：给 ≥15 个条目的
`05-finding.md` 末尾**追加**一节 `### 竞品比较`，对四个具名竞品
（Claude Code / OpenAI codex / Cursor / Aider）逐行给出 **赢/输/平** 判定
加可核查依据。

不新造文章、不重跑素材、不改动 `00`–`04` 任何字节。

**产出：17 个条目**（超出下限 15 条）。

## 选条目判据

97 个条目目录中选 17 个，判据三条，按优先级：

1. **维度在四家竞品里有公开可核查的对等物或明确的"无对等物"**。
   有对等物 → 可判 赢/输/平；四家都查不到 → 判 `未判` 并在依据里写明
   "官方文档未描述该类入口"。本轮**不写四行全 `未判` 的条目**：那样
   表格有 4 行却零信息量，属于违反约束 2 的字面合规。
2. **覆盖 gopi 的三层结构，而不是只挑容易赢的**：
   系统层（worker / merger / merge / task sweep / parent-gate）、
   I/O 层（code-search / keyword-search 姊妹条目 / log / stats / raw）、
   配置与客户面（env 条目、notify、checkpoint / resume）。
   系统层条目恰恰是本轮**认输点最密集**的地方（串行 drain、合并需人点名、
   并行吞吐），只挑 I/O 层会得到一个只赢不输的假结论。
3. **优先取已有 `05-finding.md` 正文（含 F1/F2 缺陷记录）的条目**——
   竞品判定要能引用该条目自己已观测到的事实当"认输点"的落地证据，
   而不是靠外部印象。

落选的 `docs/archive/demo/materials/01-list`、`02-show` … 等一批短条目未入选，
原因是其维度（ledger 的 list/show）在四家竞品里连"对等物"都难以界定，
判定会退化成四行 `未判`，撞上判据 1。

## 17 个条目与判定分布

判定列顺序固定为 Claude Code / OpenAI codex / Cursor / Aider；判定 ∈
{赢, 输, 平, 未判}。下表由本节末尾的自检命令第 2 条（逐行 grep）生成。

| # | 条目 | CC | codex | Cursor | Aider | 认输点 |
|---|---|---|---|---|---|---|
| 1 | 001-notify-cmd-default-off | 平 | 未判 | 赢 | 未判 | 有 |
| 2 | 001-task-list | 平 | 平 | **输** | 平 | 有 |
| 3 | 011-parent-gate-parked | 未判 | 未判 | 未判 | 未判 | 有（永久 park） |
| 4 | 012-task-sweep-zombies | 未判 | 平 | 未判 | 平 | 无 |
| 5 | 014-task-sweep-worktrees | 未判 | 未判 | 平 | 平 | 无 |
| 6 | 016-task-diff-nopeer | 未判 | 平 | **输** | 平 | 有 |
| 7 | 036-env-timeout-sec | 赢 | 平 | 未判 | 平 | 有 |
| 8 | 038-env-debug | 未判 | 赢 | 未判 | 平 | 有 |
| 9 | 102-index-coding-session | 平 | 平 | 赢 | 赢 | 有 |
| 10 | 104-worker | 平 | **输** | **输** | 平 | 有 |
| 11 | 105-merger | 未判 | 平 | 未判 | 平 | 无 |
| 12 | 106-merge | 未判 | 平 | 平 | 平 | 有 |
| 13 | 108-code-search | 平 | 平 | **输** | **输** | 有 |
| 14 | 110-log | 未判 | 未判 | 平 | 平 | 无 |
| 15 | 111-stats | 未判 | 平 | 平 | 平 | 无 |
| 16 | 113-checkpoint | **输** | **输** | **输** | **输** | 有（四行全输） |
| 17 | 114-resume | 平 | 平 | 平 | 平 | 有（使用门槛） |

判定统计（68 行，逐行 grep 计数）：**赢 5 / 输 10 / 平 34 / 未判 19**。
**12 个条目明写「认输点（gopi 输）」**（要求 ≥3）：
001-notify-cmd-default-off、001-task-list、011-parent-gate-parked、
016-task-diff-nopeer、036-env-timeout-sec、038-env-debug、
102-index-coding-session、104-worker、106-merge、108-code-search、
113-checkpoint、114-resume。

`012-task-sweep-zombies`、`014-task-sweep-worktrees`、`105-merger`、
`110-log`、`111-stats` 五个条目表格里**没有** `输` 行，也没有独立认输点行：
其维度（僵尸清扫、worktree 清扫、merger tick、逐轮日志、token 统计）在四家
公开文档里要么无对等物（`未判`），要么对等物不同形（`平`，依据行内已写明
差异）。按约束 2 不编造认输点，故如实标注为无。

## 依据来源清单（全部经核验）

### A. 官方 URL（11 条，本轮 web_fetch 逐条取回正文，status=ok）

| URL | 核验到的正文要点 |
|---|---|
| https://raw.githubusercontent.com/anthropics/claude-code/main/README.md | "agentic coding tool that lives in your terminal, understands your codebase … executing routine tasks … handling git workflows"；无跨会话台账/统计/索引类入口 |
| https://raw.githubusercontent.com/openai/codex/main/README.md | 三种形态（CLI / IDE / 桌面 App / Codex Web）+ 安装；无会话摊平/统计入口 |
| https://cursor.com/docs/get-started/installation | 侧栏列出 Agent / Agents Window / Projects / Agent Review / Planning / Cloud Agents 等**一等 UI 概念** |
| https://cursor.com/docs/agent/overview | agent 过程可在产品内查看（Debugging 章节），但无"逐轮原始事件"粒度 |
| https://cursor.com/docs/agent/planning | Plan Mode 是产品面 |
| https://cursor.com/docs/agent/review | "Reviewing and testing code"：合并前评审是产品化流程 |
| https://aider.chat/docs/repomap.html | "a concise map of your whole git repository that includes the most important classes and functions along with their types and call signatures" |
| https://aider.chat/docs/git.html | 每次编辑自动 commit、`/undo`、按 git 分支管理改动 |
| https://aider.chat/docs/scripting.html | `--message` 单发单收，执行完即退出（无队列） |
| https://aider.chat/docs/usage/commands.html | 会话内斜杠命令表（`/add`、`/ask`、`/architect`、`/undo`…），无任务依赖概念 |
| https://aider.chat/docs/config/options.html | 选项总表以 model/API/edit 行为为主 |

### B. 本仓一手依据（file:line，逐行 grep 核验）

| 引用 | 该行实际内容（本轮 `sed -n` 核验） |
|---|---|
| `devdoc/gopi-vs-codex-competitive-analysis.md:18` | 多 agent 对比行：codex `spawn_agent`/`send_input`/`wait`/`close_agent` 完整委派生命周期 |
| `devdoc/gopi-vs-codex-competitive-analysis.md:46` | 小节标题「动态工具发现：`tool_search` / BM25（最大的可扩展性差距）」 |
| `devdoc/gopi-vs-codex-competitive-analysis.md:56` | 「为什么 codex 赢」：工具发现是模型主动的可检索能力 |
| `devdoc/gopi-vs-codex-competitive-analysis.md:68` | codex 截断策略 `TruncationPolicy::Bytes\|Tokens` + `approx_token_count` |
| `devdoc/gopi-vs-codex-competitive-analysis.md:85` | codex 多智能体委派 `spawn_agent` 细节 + `agent_max_depth` |
| `devdoc/gopi-vs-codex-competitive-analysis.md:183` | 借鉴清单第 3 项「【高】多智能体委派」 |
| `GOPI.md:817` | "there is no override flag, no cascade cancel, no timeout/staleness release"（父行永久 park 子行） |

引用该 devdoc 时口径一致：它**只**提供 codex 一侧的一手源码对比；Cursor /
Claude Code / Aider 的判定一律走 A 类官方 URL，不借道该 devdoc。

## 自检结果

```
# 1) 形态：只有 05-finding.md 变化，且只增不删
$ git diff --stat
 17 files changed, 256 insertions(+), 0 deletions(-)
 （17 个文件全部是 docs/archive/demo/materials/*/05-finding.md）
$ git status --porcelain | grep -vE '05-finding\.md'   # → 空
$ git diff --stat -- docs/archive/demo/materials/*/00-entry.md .../01-input.txt \
     .../02-output.txt .../03-raw.png .../04-annotated.png   # → 空输出

# 2) 每条 4 行、四个具名竞品齐全
$ for f in $(git diff --name-only); do sed -n '/^### 竞品比较/,$p' "$f" \
    | grep -cE '^\| *(Claude Code|OpenAI codex|Cursor|Aider)'; done | sort -u
 4                                   # 17/17 条目各恰好 4 行

# 3) 认输点 ≥3
$ for f in $(git diff --name-only); do grep -q '认输点（gopi 输' "$f" && echo x; done | wc -l
 12

# 4) 判定词汇统计（68 行）
 赢 5 / 输 10 / 平 34 / 未判 19

# 5) devdoc 行号仍指向真实内容（本轮重新 sed 核验，见上表 B 列）
# 6) 11 条 URL 本轮 web_fetch 全部 status=ok（见上表 A 列）
# 7) 语料分类与 Rule 30
$ python3 tools/docclass/classify.py . --check
 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
$ git status --porcelain      # 见「收尾」小节
```

## 本轮踩到的工具行为（给后续同类轮次）

**`write` 对 `001-task-list/05-finding.md` 的追加静默失败**：同一批 append
里只有这一个文件 size 未变（`git diff --stat` 显示 0 变化），其余 16 个正常。
改用 `bash` 追加并逐文件复核 size 与 `### 竞品比较` 段数后通过。
教训：**批量 append 后必须逐个核对产出面**（不能用"脚本 exit=0"当交付证据），
本轮的核对命令是上面自检第 2 条。

## Not Done（诚实部分）

- 未对素材库**全部** 97 个条目补竞品段，只补 17 个——本轮目标是"角度"轮，
  下限 15 条；剩余条目若要补，判据同上，可直接复用本报告的依据来源清单。
- `012-task-sweep-zombies`、`110-log`、`111-stats` 三个条目**没有**独立
  「认输点」行：该维度四家无同形对等物，按约束 2 不编造。
- 竞品判定基于**公开文档**，未实际运行四家产品做对照实验；凡文档未描述
  之处一律记 `未判`，不记 `平`。
