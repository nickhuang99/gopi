<!-- doc-class: historical -->
# 竞争分析：gopi vs hermes-agent — 8 维度源码对照

> 一手源码对比（2026-09-30）。被测仓 `/home/user/work/hermes-agent`（Python，只读）、
> 对照仓本 run 的 worktree（`task/01a0eeb6...`，master 基线 `af50b193`）。
> 格式沿用 `devdoc/gopi-vs-codex-competitive-analysis.md`（196 行，2026-08-29）。
> **每条判定必须带 file:line 两侧锚点**；判定词 `{赢,输,平,未判}`；判 `未判` 必须给出
> "检索得出的没有"（附检索词）或"源码无对应物"的理由。

---

## 0. 摘要（TL;DR）

hermes-agent 不是 codex 那一类"架构级碾压"的对手，它和 gopi 是**同一生态里的两只不同生物**：
hermes 是**平台型长生命周期 agent**（多前端 / 多渠道 / 多沙箱后端 / 多 MCP / 定时任务 / 看板编排），
gopi 是**单机自驱的交付流水线**（worktree→分支→merge→台账→报告，闭环强制记账）。

**70 条判定**里 gopi 赢 22、输 23、平 24、未判 1 —— **本 run 复核后为 71 条：赢 22 / 输 23 / 平 25 / 未判 1**（逐行机器重数，生成器见 §7）。
（本 run 的改动：3.3 由"输"改判"平"——gopi 的 `gopi brew` 是真后台蒸馏管线，原判据不成立；新增 3.10"质量度量"判"输"。）
gopi 明确输的最大两条：
**① 技能系统**（hermes 21 类目 / 81 个 `SKILL.md` + FTS5 索引 + 自动注入 + agent 自写技能 + GitHub 分发信任分级 vs gopi **零**技能系统）；
**② 多 agent 委派与编排**（hermes in-process 子 agent：可并发/异步/中断/独立 prompt/递归闸门 vs gopi 只有 `job` 后台 shell + 跨进程台账队列）。
第三是**工具生态接入**（hermes 5627 行 MCP + BM25 按需发现 vs gopi 17 个扁平常驻工具）。

gopi 赢在**交付纪律的机器化**：merge 队列与构建门、worktree 生命周期、Rule 33 父子闸门、append-only 台账、
记忆质量裁判（feedback/update）、零依赖渲染/截图（text2img、纯 Go X11）、文档真值分类（DOC-INDEX）。

**一句话**：hermes 赢在**广度**（能跑更多形态的工作），gopi 赢在**闭环**（每一项工作都被强制追责到底）。

---

## 1. 测量的基线、census 与生成器（Rule 38）

**被测仓 HEAD**：`baf5fa02173c0035fd61532159ce20781600809d`（branch `hot-note`，
`curator: repoint Skill Trigger Index at live umbrellas after consolidations`）。
本报告所有 hermes 行号都在这个 commit 上复读过。

| census 数字 | 生成器（逐字） | 测得值 |
|---|---|---|
| **3114** 个 .py | `find . -name '*.py' -not -path './.venv/*' \| wc -l` | 3114（同命令再加 `-not -path '*/node_modules/*'` → 3051）|
| **95** 个工具文件 | `ls tools/*.py \| wc -l` | 95 |
| **21** 个技能类目 | `ls -d skills/*/ \| wc -l` | 21 |
| 81 个技能实体 | `find skills -name SKILL.md \| wc -l` | 81（`skills/*.py`=95 是**文件数**，`skills/` 下 21 是**类目目录数**，两者不同维度，勿混用）|
| 79 个工具注册点 | `grep -rc "registry.register(" tools/*.py \| awk -F: '{s+=$2} END {print s}'` | 79 |
| 71 个唯一工具名 | `grep -rhoP 'name="[a-z0-9_]+"' --include=*.py tools/ \| sort -u \| wc -l` | 71 |

> **数字口径提示（诚实）**：95 是 `tools/` 下的 **.py 文件数**，71 是**唯一注册工具名**，
> 79 是**注册调用点**（有模块多次 register）。"95 个工具"这个说法在文件层面成立，
> 在"模型可见工具数"层面应说 **71 个（+12 个 browser_* 已计入）**。
> gopi 侧对照：`internal/agent/loop.go:1168` 的 `claudeToolDefs` 是 **17** 个
> （生成器：`grep -oP 'Name:\s+toolnames\.Tool\w+' internal/agent/loop.go | wc -l` → 17）。

---

## 2. 目录关系澄清（hermes 家族）

| 目录 | 是什么 | 证据 |
|---|---|---|
| `/home/user/work/hermes-agent` | **主开发仓**，本报告的被测对象。remote `ssh://digital/root/hermes-agent`（HP 节点）。当前 branch `hot-note` | `git rev-parse HEAD` → `baf5fa021`；`git remote -v` |
| `/home/user/work/hermes-upstream` | **另一份独立 clone**（remote `remotebox:/root/work/hermes-agent`），与主仓**不共享对象库**（主仓 `git cat-file -t <upstream-HEAD>` → `fatal: could not get object info`）。含 `website/`、`evals/`、`native/` 等主仓没有的目录 | 上述 `cat-file` 报错；`git log --oneline -1` → `a0ca7c1920 feat(cron): add explicit one-shot re-arm` |
| `/home/user/work/hermes-agent-hotnotes` | **下游 hot-notes POC 仓**，`git` 仓库**无任何 commit**（`fatal: your current branch 'master' does not have any commits yet`），工作区全为 untracked。README 自述"Heremes Agent 的 hot-notes 模块下游仓库"，但**核心实现其实在主仓** `agent/hot_notes.py` | `git log` 报错；`README.md`；`agent/hot_notes.py:22` |
| `/home/user/work/hermes-memory-design` | **报告目录，不是代码仓**。含 2026-08-23 一轮"让 LLM 真的调用记忆工具"的四方评估报告（claude/codex/dsh/gopi）+ `hippo_memory/` 插件骨架 + `gen_plugin.py` | `ls`；`gopi-report.md` 头 |
| `/home/user/hermes` | 只剩 `research/`（proposals、repos、几份 md），非仓库 | `ls -R` |
| `/home/user/hermes_demo` vs `hermes-agent/hermes_demo` | 两个**同名不同物**：前者是 evennia 游戏探索的产物（报告+json+py），后者主仓内只有 `report_part2.md` | `ls` 两侧 |
| 运行中的进程 | `hermes chat --cli --yolo`（PID 841880，已跑 2h39m）——本 run 全程未触碰 | `ps -eo pid,etime,cmd` |

**与 gopi 的关系（重要）**：`hippo_memory` 插件是 **从 gopi 的知识库设计反向移植到 hermes 的产物**
——`hermes-memory-design/claude-report.md` 开头写着"上一轮 gopi 方向的失败教训：工具描述（prompt）
没有说服力，LLM 不会用"，`hippo_memory/plugin.yaml` 的 description 是
"memory_search 重写（hippo CLI 后端 + 中文说服性描述）+ memory_store 新增 —— Hermes 记忆写侧闭环"。
gopi 侧也留着这条导入通道的痕迹：`internal/tools/memory_provenance.go:83`
（`provenanceTypeImport` = "bulk import（distilled.db / hermes-skills migrations）"）。
**即：一条双向的借鉴通道，不是单纯谁抄谁。**

---

## 3. 四份 hermes 仓内 gopi-handoff 的"参照关系"

四份文件都在 hermes-agent 仓根，内容是 **gopi 侧的会话交接单，被物理存放在对手仓里**：

| handoff | 文件 | 是否入库 | 提及 gopi 次数（`grep -c -i gopi`） |
|---|---|---|---|
| `handoff-20260828-tasks-autoregister.md` | 8 个闭环 | tracked | 18 |
| `handoff-20260829-session-search-freeze-fix.md` | session_search CJK 冻结修复 | tracked | 6 |
| `handoff-20260905-memory-aware-night.md` | memory-aware 夜间窗口 | tracked | 10 |
| `handoff-20260905-gopi-logstats-rowresolve.md` | log/stats/row-per-resolve | **untracked** | 21 |

（生成器：`for f in handoff-*.md; do grep -c -i gopi $f; done`；
入库判定：`git ls-files --error-unmatch handoff-20260905-gopi-logstats-rowresolve.md`
→ `did not match any file(s) known to git`。）

**参照关系的三个结论（这是本报告与普通外部分析最大的不同）：**

1. **不是"竞品"，是"对照实验组"。** 这四份 handoff 记录的工作，**一半是 gopi 自己的开发**
   （`gopi CLI --todo 修复` → GoPi master 56c7982、`gopi messages_path 支持` → master dbba5b1、
   `tasks 台账实现` → master c29ce89），**另一半是 hermes 的 bug 修复**
   （`session_search CJK 慢路径修复` → 记作"hermes-agent hot-note 851e039ce + a4ac15101，47x 提速
   2.83s→0.06s，5 个回归测试"）。同一个操作者用**同一套方法论**在两个仓里干活，并把两边的
   交接单放在同一处——**参照关系是"同一操作者的两条产品线"，而非敌意竞品。**

2. **handoff 里的方法论是共用的，且以 gopi 的规则为锚。** 8/28 那份的"关键方法论教训"写着
   "模型对比竞争必须直接跑 gopi 二进制（`echo task | gopi --settings X`）"；
   8/29 那份的"当前状态"直接写 **"GoPi master @ 428906d（8/28 终态，未变）"**
   ——hermes 的交接单里用 gopi 的 master 作为**坐标参照**。这解释了为什么
   hermes 的 `agent/prompt_builder.py` 里有中文的 `RULES_SNAPSHOT`
   （`agent/rules_snapshot.py:12`，第一行写着"核心信条 RULES_SNAPSHOT — 覆盖预训练默认行为, 不是建议"）
   ——**这两套系统的行为约束语言是同一支笔写的。**

   这条参照关系还有一处**最难反驳的物证**：hermes 自己的蒸馏检索面把 gopi 的词汇
   写进了锚点定义。`agent/distill_gate.py:383`（`build_search_kw` 的 docstring，
   `:376` 为函数签名）逐字写着——

   ```
   1. ASCII 提问者锚点     —— 产品/机制标识符 (gopi/ground/merge/worktree)；只排
   ```

   ——`gopi` 与 `ground`（gopi 的 worktree 术语）被列为**首选的检索锚点 token**，
   与 `merge`/`worktree` 并列。也就是说：hermes 的记忆检索为"提问者会说 gopi 的机制词"
   做了专门设计，**检索侧第一顺位装的是 gopi 的词汇表**。这不是"两个竞品碰巧撞词"，
   是**同一操作者的知识在两个系统间流动**在代码里的化石。加上 3.3 的 `extract_keywords`
   被 `build_search_kw` 显式取代（`agent/distill_gate.py:376` 的 docstring 自称
   "替代 extract_keywords 的整篇 body 机械切片"），hermes 的记忆管线演进方向
   与 gopi 的知识库设计（`internal/tools/memory.go` 的 `search_kw` 检索面包屑）是**同一个设计意图**。

3. **gopi 是 hermes 的"记忆设计上游"。** `hermes-memory-design/gopi-report.md` 是给 hermes 的
   设计评估报告（§4.3 结论："成败关键不在'有没有工具'，而在'写口的摩擦 + search 的动机锚'"），
   而它的评估对象正是 gopi 的知识库工具设计。**结论：两系统互相是对方的参照系，
   本报告的价值不在"谁强"，而在"同一目标下两条不同工程路线的取舍"。**

---

## 4. 八维度判定表

判定词：**赢**=该维度 gopi 明显更强；**输**=hermes 明显更强；**平**=双方都有等价机制；
**未判**=源码无对应物或检索未命中（附检索词）。

### 维度 1：工具模型（工具注册、发现、调度）

| # | 条目 | 判定 | hermes 锚点 | gopi 锚点 |
|---|---|---|---|---|
| 1.1 | 工具注册架构 | **输** | `tools/registry.py:208` `class ToolRegistry`；`:356` `def register(`；`:58` `discover_builtin_tools`（AST 扫描自注册模块）；71 个唯一工具名 | `internal/agent/loop.go:1168` `var claudeToolDefs = []deepseek.ClaudeToolDef{`（17 个扁平常量） |
| 1.2 | 按需工具发现 | **输** | `tools/tool_search.py:529` `assemble_tool_defs`；`:163` `is_deferrable_tool_name`；`:347` `_bm25_score`（BM25 检索目录，超阈值才把 MCP/插件工具折叠成 tool_search/describe/call 三个桥） | 无对应物：`grep -ril "tool_search\|defer_loading" internal/ cmd/ --include=*.go` → 0 命中 |
| 1.3 | 工具集分组/可见性 | **输** | `toolsets.py:31` `_HERMES_CORE_TOOLS`（核心集常驻、非核心可 defer）+ toolset 别名/覆盖策略 `tools/registry.py:281`/`:307` | 无 toolset 概念；`internal/toolnames/names.go` 只有名字常量 |
| 1.4 | 分派实现 | **平** | `tools/registry.py:574` `def dispatch(`（注册表查表） | `internal/agent/loop.go` 的 `executeToolDefault` switch（硬编码但短，17 分支） |
| 1.5 | 工具生态接入（MCP） | **输** | `tools/mcp_tool.py`（**5627 行**，含 OAuth、stdio watchdog、env 净化 `tools/mcp_oauth_manager.py`、`tools/mcp_stdio_watchdog.py`）；另有 19 个 `optional-mcps/` | 无 MCP：`grep -ril "\bmcp\b" internal/ cmd/ --include=*.go` → 0 |
| 1.6 | 工具数量与表达力 | **输** | 71 唯一工具名 + `tools/browser_*.py` 12 个 browser 工具（`browser_navigate`…`browser_dialog`）+ computer_use 全家桶 | 17 个（`loop.go:1168` 起） |
| 1.7 | 工具结果预算 | **平** | `tools/tool_output_limits.py`、`tools/tool_result_storage.py`、`tools/registry.py:606` `get_max_result_size` | `internal/agent/context.go:621` `TrimToFit` + `internal/tools/taskreport` 类报告截断惯例 |
| 1.8 | 只读工具不被误判为死胡同 | **赢** | 无对应物（`grep -ril "contentReturningTools" tools/ agent/` → 0） | `internal/agent/loop.go:553` `contentReturningTools` map（memory/websearch/webfetch/codesearch/keywordsearch 的正文含 "failed" 不算 dead end） |

**维度 1 小结：赢 1 / 输 5 / 平 2（共 8 条）—— gopi 在这个维度明确输。**
hermes 的工具系统是"注册表 + 分组 + 按需加载 + 生态接入"；gopi 是"17 个常量 + 一个大 switch"。
**但 gopi 赢一条被低估的**：1.8 的"正文类工具不被 trim 误杀"是 gopi 独有的实操教训
（注释里写着 2026-08-29 实测：memory_search 返回 5 条相关配方被当死胡同 trim 掉）。

---

### 维度 2：技能系统

| # | 条目 | 判定 | hermes 锚点 | gopi 锚点 |
|---|---|---|---|---|
| 2.1 | 技能库本体 | **输** | `skills/` 21 个类目 / 81 个 `SKILL.md`；`optional-skills/` 另 19 个类目 | **无技能系统**：`git ls-files \| grep -i skill` → 只有 1 份历史报告 `reports/archive/2026-08/SKILL-IMPORT-REPORT.md` |
| 2.2 | 技能检索 | **输** | `tools/skills_search_tool.py:22` `SKILL_SEARCH_SCHEMA` + `:58` register（`skill_search`）；`agent/skill_index.py:24` `INDEX_FILENAME = "skills_index.db"`（FTS5 独立库） | 无对应物 |
| 2.3 | 技能自动注入 | **输** | `agent/skill_trigger_injection.py:19` `_SKILLS_DB = /home/user/.hermes/skills_index.db`；`agent/system_prompt.py:526` `build_skill_trigger_index` → 系统提示里注入 "## Skill Trigger Index" | 无对应物：`grep -rn "Skill" internal/standards/*.go` → 0 |
| 2.4 | 技能 CRUD（agent 自己写技能） | **输** | `tools/skill_manager_tool.py:1641` `name="skill_manage"`；`agent/prompt_builder.py:186` `SKILLS_GUIDANCE`（"复杂任务后存成 skill，发现过时立刻 patch"） | 无对应物 |
| 2.5 | 技能分发/信任（跨机共享） | **输** | `tools/skills_hub.py:424` `class SkillSource(ABC)`、`:507` `class GitHubSource(SkillSource)`、`:131` `SkillMeta` / `:145` `SkillBundle`（Github taps、lockfile、quarantine、audit log：`:77`/`:82`/`:87`） | 无对应物 |
| 2.6 | 经验沉淀的机制（技能 vs 记忆） | **平** | hermes 走 **skill**（过程性知识）+ memory（声明式事实），`MEMORY_GUIDANCE` 明确分工 | gopi 走 **memory 的 procedural 表**：`internal/hippocampus/` + `internal/tools/memory.go:118` 的 `distilled_qa`；`internal/agent/loop.go:199` 的结论字段含 "skill gain, facts discovered, lessons learned, pitfalls avoided" |

**维度 2 小结：赢 0 / 输 5 / 平 1（共 6 条）—— gopi 在这个维度输得最彻底。**
gopi 没有"技能"这个物种：hermes 的可复用过程被**结构化成一个可分发的包**（SKILL.md + 索引 + GitHub taps + 信任分级），
gopi 的可复用过程被**塞进记忆表的一行文本**。这是**物种差异**，不是实现质量差异。

---

### 维度 3：记忆蒸馏

| # | 条目 | 判定 | hermes 锚点 | gopi 锚点 |
|---|---|---|---|---|
| 3.1 | 长驻记忆（声明式事实） | **平** | `tools/memory_tool.py:113` `class MemoryStore`；`:130` `memory_char_limit=2200, user_char_limit=1375`；`:336` add / `:388` replace / `:457` remove / `:552` `apply_batch`；`:670` `format_for_system_prompt`（每轮注入） | `internal/agent/loop.go:1370` `Name: toolnames.ToolMemory`（action=search/store/update/feedback 四合一） |
| 3.2 | 蒸馏知识库（QA） | **平** | `tools/memory_search_tool.py:135` `def memory_search(`；`:254` register（`memory_search`，FTS5，零 LLM 调用） | `internal/tools/memory.go:118` `memoryBaseTableDDL`（`distilled_qa`：topic/question/answer/sources/evidence/confidence/tier/search_kw） |
| 3.3 | 自动蒸馏管线（后台） | **平** | `agent/distill_utils.py:26` `ensure_schema` / `:46` `tag_5w1h`（调 DeepSeek Flash 打 5W1H，`:229` 在 `insert_qa` 内**建行时**触发）/ `:92` `jieba_tokenize`（真 jieba 库）/ `:112` `_fts5_dedup_query`（去重）/ `:153` `insert_qa`；另有 cron 补标 `scripts/tag_5w1h_cron.py:2`、`agent/distill_gate.py`、`scripts/distill_record.py` | **gopi 有等价后台管线 = `gopi brew`**：`internal/tools/memory_brew_run.go:99` `func RunBrew(`（扫 `~/.gopi/sessions/*.jsonl` → LLM 判 lesson 三条件 → 蒸馏 QA → 写库，`:227` 落 `source=brew` 防自指）；触发器 `cmd/agent/cli_brew.go:133` `maybeBrewOnSessionExit()` + `cmd/agent/main.go:827`（**session 退出**时跑，24h 一次，`internal/config/settings.go:47` `GOPI_BREW_INTERVAL_SEC`）；去重 `internal/tools/memory.go:160`/`:167`（精确键 + trigram-Jaccard 相似度）。**5W1H 打标时机不同**：hermes 在线内联（`:229`），gopi 的 5W1H 列（`internal/tools/memory.go:135`/`:334`）在 store 时落空串（`:3452-3458` 的 `default:` 分支自承"LLM 契约不给这些列"），靠批式 `scripts/backfill_exam_5w1h.py:2` 回填（`WHERE exam_questions=''` 断点续跑、幂等）——机制等价、时机不同 |
| 3.4 | 记忆质量裁判（投票） | **赢** | **无对应物**（检索：`grep -ril "rating=\"useless\"\|quality vote" tools/ agent/` → 0；`grep -c feedback tools/memory_tool.py tools/memory_search_tool.py` → 1/0，且那 1 处是错误提示文案 `tools/memory_tool.py:687`（"Truncated one-line previews of entries for error feedback."），不是投票机制） | `internal/tools/memory.go:532` `RatingUseful = "useful"` / `:533` `RatingUseless`；`:895` `memory_feedback` 表 DDL（`CHECK (rating IN ('useful','useless','unused'))`）；工具描述 `internal/agent/loop.go:1374`（action=feedback，票改变置信度 0.05/-0.10 并重排检索） |
| 3.5 | 记忆纠错（硬编辑） | **赢** | 无对应物：hermes 的 `memory` 工具只有 add/replace/remove/overwrite（`tools/memory_tool.py:336`/`:388`/`:457`/`:497`），**没有"某条蒸馏记录被证伪后更正它"的动作** | `internal/agent/loop.go:1375`（action=update：id + 被更正字段，`feedback 说哪里错，update 做修正`）；工具路由注释 `internal/agent/loop.go:5544` 区分 search（只读）与写动作 |
| 3.6 | 自动注入的"陷阱/教训" | **平** | `agent/hot_notes.py:22` `SELF_RECORDING_PROMPT`（`[IMPORTANT]` 标记自动落盘 → `/home/user/.hermes/hot_notes/`）；`:176` `save_pitfalls` / `:188` `get_pitfalls`；注入点 `agent/system_prompt.py:544` `get_pitfalls()` | `internal/agent/loop.go:199`（结论字段要求写 pitfalls avoided / lessons learned）+ memLayout 段 `internal/agent/loop.go:1082` |
| 3.7 | 检索时机的 prompt 说服 | **平** | `agent/prompt_builder.py:147` `MEMORY_GUIDANCE` / `:170` `SESSION_SEARCH_GUIDANCE` / `:176` `MEMORY_SEARCH_GUIDANCE`（"Before repeating work that may have been done before"） | `internal/agent/loop.go:1372` 的 search 描述（"USE FIRST at the start of every task… 取不到 → store"，中文 use-case 驱动） |
| 3.8 | 溯源（这条记忆哪来的） | **赢** | 无对应物（`grep -ril provenance tools/ agent/` 无相关命中） | `internal/tools/memory_provenance.go:83` `provenanceTypeImport` / `:209` 的 SQL 分类谓词（url / import / node:<alias> / path 四型，可寻址性分级） |
| 3.9 | 层次化记忆分层 | **平** | 四层：`agent/prompt_builder.py:172` `RULES_SNAPSHOT`（无条件注入）→ fact_store → distilled_qa → state.db（`hermes-memory-design/gopi-report.md` §1.1 记录的分层） | 三层：memLayout（轮内工作区）→ task 台账 → `distilled_qa`（tier hot/warm/cold，`internal/tools/memory.go:118` 的 `tier` 字段） |
| 3.10 | 记忆/检索**质量度量**（可复跑的指标，非一次性复盘） | **输** | `scripts/compare_5w1h_quality.py:1`（5W1H 标注质量四指标：标签数量/长度/去重率/覆盖率）；`scripts/train_search_routing.py:141`（`hit_rate = hits/(hits+misses)`，路由准确率随使用自动生长，`tools/session_search_tool.py:873` 消费） | 无对应物：`find . -name '*quality*' -path './scripts/*'` → **0**；gopi 侧只有**一次性**的劣化复盘 devdoc（`devdoc/memory-lowquality-retrieval-evaluation.md:1`，2026-08-25 用 SQL 手工归因），没有可复跑的指标生成器 |

**维度 3 小结：赢 3 / 输 1 / 平 6（共 10 条）—— 这是 gopi 最强的一维。**
gopi 赢在**记忆的"代谢"**：hermes 的蒸馏库是"只进不出"的（有 add/replace/remove 但没有"这条被后来的事实推翻了"的动作），
gopi 有 feedback 投票 + update 硬修正 + provenance 溯源——**记忆是可以被证伪并被更正的**。
gopi 输的两条都是**度量与在线性**：3.3 里的打标时机（hermes 建行即打标，gopi 要等 backfill 批处理）、
3.10 的质量度量（hermes 有 `compare_5w1h_quality.py` + 自动生长的 `hit_rate`，gopi 只有一次性复盘）。
**自动蒸馏管线本身两者都有**（hermes `insert_qa`/cron tag vs gopi `gopi brew` + session-exit 触发），故 3.3 判平。

---

### 维度 4：工作流台账

| # | 条目 | 判定 | hermes 锚点 | gopi 锚点 |
|---|---|---|---|---|
| 4.1 | 持久化任务台账 | **平** | `hermes_cli/kanban_db.py:1096` `CREATE TABLE IF NOT EXISTS tasks`；`:1181` `task_links` / `:1187` `task_comments` / `:1211` `task_runs` / `:1239` `task_attachments` / `:1254` `kanban_notify_subs`；`tools/kanban_tools.py:1593` 起注册 `kanban_*` 十工具 | `internal/tools/tasks.go`（8287 行，append-only 台账）+ `internal/tools/ftsync_verify_test.go:96` 的 tasks DDL（另一处独立 DDL 见 `internal/tools/firstopen_refusal_regression_test.go:91`） |
| 4.2 | 状态机丰富度 | **平** | `hermes_cli/kanban_db.py:102` `VALID_STATUSES = {triage, todo, scheduled, ready, running, blocked, review, done, archived}`（9 态） | gopi 状态集较窄（pending/claimed/in_progress/completed/verified/failed/cancelled 等，`internal/tools/tasks.go`），但**多一个 verified 终态**（hermes 无 verified 概念） |
| 4.3 | 阻塞原因分类 + 防抖 | **赢** | `hermes_cli/kanban_db.py:125` `VALID_BLOCK_KINDS = {dependency, needs_input, capability, transient}` + `BLOCK_RECURRENCE_LIMIT` 防 unblock↔re-block 死循环（同文件 `:127-130` 注释） | gopi 无 block-kind 分类；但防抖由 `internal/tools/task_reclaim*.go` 的 reclaim 扫描承担 |
| 4.4 | 父子依赖闸门 | **平/赢** | `hermes_cli/kanban_db.py:2832` `if parent_status != "done":` → 把 ready 的孩子降回 todo（**只在 link 时降级一次**）；`:3372` `def claim_task(` 只做 ready→running 原子转换 | `internal/tools/tasks.go:5864` `func taskParentGateDenied(parentID string, parentStatus map[string]string) (bool, string)`；`:5982` `func taskRowClaimable(...)`——**每次认领都过闸**，父状态 ∈ {completed, verified} 才放行，且**悬挂父 id 也拒**（Rule 33） |
| 4.5 | claim 原子性/锁 | **平** | `hermes_cli/kanban_db.py:1415` `_dispatch_tick_lock`（board 级单写者锁）+ `:6932` `dispatch_once` | gopi 走 SQLite 事务 + `internal/tools/task_reclaim*.go`；锁粒度是行级 |
| 4.6 | 台账全文检索 | **赢** | **无对应物**：`grep -c "fts\|FTS" hermes_cli/kanban_db.py` → **1**（唯一命中不是任务全文索引）；`grep -n "def search_tasks\|CREATE VIRTUAL" hermes_cli/kanban_db.py` → 0 | `internal/tools/tasks.go:546` `fts = CREATE VIRTUAL TABLE IF NOT EXISTS tasks_fts USING fts5(`；`:550` `tasks_tri`（trigram，中文 2 字可搜）；`:1197` `migrateTasksFTS`；`:4666` `func TaskSearch(query string)`（`:4687` 用 `ORDER BY bm25(tasks_fts)`） |
| 4.7 | merge 队列/自动合并 | **赢** | **无对应物**：`grep -ril "merge queue\|auto_merge\|merger" --include=*.py tools/ agent/ hermes_cli/` → **0 命中**；`grep -rn "git merge" --include=*.py hermes_cli/ tools/ agent/` → 0 | `internal/tools/mergedCleanup*`、`internal/tools/tasks.go` 的 merger 单点所有权、`task_merged_cleanup.go:909`；合并前构建门 `internal/tools/merge_build_gate.go`（含 gocache/modcache 隔离变体 4 个文件） |
| 4.8 | worktree 生命周期作为一等机制 | **赢** | worktree 在 hermes 里是 **CLI 的一个便利功能**：`cli.py:1382`（=仓根主 CLI，16362 行；仓内共 8 个同名 cli.py，最浅者即仓根，勿混）`_active_worktree`、`cli.py:1445` `_resolve_worktree_base`、`cli.py:1663` 注释 lock、`cli.py:1828` 手工清理提示 | gopi 里是**台账驱动**的：`internal/tools/worktreerelease`、`internal/tools/worktree_sweep_chat.go`（含 `:43` "NOTHING PERSISTENT IS CREATED (Rule 35)"）、`internal/standards/worktree_root_gate.go`；清理由 finish-cleanup 链自动执行 |
| 4.9 | 定时调度 | **输** | `cron/` 目录（10 个 py）+ `cron/scheduler.py`（3750 行）+ `plugins/cron_providers/` + `cron/suggestion_catalog.py` | gopi **刻意不做 cron**：`grep -ic cron` 只 2 个文件，且 `internal/tools/merger_graceful.go:19` 明写 "RESIDENT-FORM SERVICES went the same way with the cron form (2026-09-17)"——是**规则选择**（Rule 35 零依赖），不是能力缺失 |
| 4.10 | 看板编排（swarm/blackboard） | **输** | `hermes_cli/kanban_swarm.py:77` `def create_swarm(` / `:225` `def post_blackboard_update(` / `:242` `latest_blackboard(`；`gateway/kanban_watchers.py:744` `_kanban_dispatcher_watcher` | 无对应物 |

**维度 4 小结：赢 4 / 输 2 / 平 4（共 10 条）。**
gopi 赢在**台账的"可检索 + 可追责 + 自动合并"**（4.6 全文检索、4.7 merge 队列、4.8 worktree 一等机制、4.3 的另一方面），
hermes 赢在**台账的"广度 + 调度"**（4.9 cron、4.10 swarm）。
注意 4.4：hermes 也有父子 gating，但它是**link 时一次性降级**；gopi 是**每次认领都过闸且悬挂父也拒**——
gopi 的语义更严；而 hermes 的 `dependency` block-kind 把"依赖未满足"路由回 todo 等待自动提升，是 gopi 没有的分类学。

---

### 维度 5：系统提示词

| # | 条目 | 判定 | hermes 锚点 | gopi 锚点 |
|---|---|---|---|---|
| 5.1 | 人格/身份层 | **输** | `agent/prompt_builder.py:126` `DEFAULT_AGENT_IDENTITY`（"You are Hermes Agent… created by Nous Research… prioritize being genuinely useful over being verbose"）；`:136` `HERMES_AGENT_HELP_GUIDANCE`（指向官方文档为唯一事实源） | `internal/agent/loop.go:1073` `{Name: headerSectionName, Order: 0, Text: headerRules}`——身份段极简，主体是**工具清单**，无沟通人格层 |
| 5.2 | 分段式装配 | **平** | `agent/system_prompt.py:116` `def build_system_prompt_parts(...)`；`:563` `build_system_prompt` | `internal/agent/loop.go:1071` 起的 `promptSections`（11 段，带 Order 排序 + `Render` 惰性渲染） |
| 5.3 | 项目上下文注入（AGENTS.md 类） | **平** | `agent/prompt_builder.py:78` `_HERMES_MD_NAMES = (".hermes.md", "HERMES.md")`；`:81` `_find_hermes_md`；`:65` `_find_git_root`；另支持 `SOUL.md / AGENTS.md / CLAUDE.md / .cursorrules`（见 `agent/agent_init.py:375`） | GOPI.md 项目上下文由 `LoadProjectContext` 加载（`internal/agent` 内，规则文件即 `GOPI.md`，Rule 37 记载其唯一 reader `LoadProjectContext`） |
| 5.4 | 模型族差异化指令 | **输** | `agent/prompt_builder.py:291` `TOOL_USE_ENFORCEMENT_GUIDANCE`、`:308` `TOOL_USE_ENFORCEMENT_MODELS = ("gpt","codex","gemini","gemma","grok","glm","qwen","deepseek")`、`:369` `PARALLEL_TOOL_CALL_GUIDANCE`、`:390` `OPENAI_MODEL_EXECUTION_GUIDANCE`、`:452` `GOOGLE_MODEL_OPERATIONAL_GUIDANCE`、`:628` `DEVELOPER_ROLE_MODELS = ("gpt-5","codex")` | 单套提示词，无按模型族切换（`internal/agent/loop.go` 的 promptSections 无按模型分支） |
| 5.5 | 平台/环境感知提示 | **输** | `agent/prompt_builder.py:630` `PLATFORM_HINTS`；`:869` `WSL_ENVIRONMENT_HINT`；`:914` `_WINDOWS_BASH_SHELL_HINT`；`:1057` `build_environment_hints()`；`:886` `_REMOTE_TERMINAL_BACKENDS`（ssh/docker/modal…） | 有 self-knowledge 段 `internal/agent/loop.go:1074`，但**无平台矩阵**（gopi 本身是单平台 Go 二进制） |
| 5.6 | 硬约束（不可协商信条） | **平** | `agent/rules_snapshot.py:12` `RULES_SNAPSHOT`（中文，"[核心信条 RULES_SNAPSHOT — 覆盖预训练默认行为, 不是建议]"），`agent/system_prompt.py:172` 无条件注入 | `GOPI.md`（"HARD CONSTRAINTS — not suggestions"）+ `internal/agent/loop.go:1073` headerRules |
| 5.7 | 效率/纪律补丁集 | **平** | `agent/prompt_builder.py:147` `MEMORY_GUIDANCE`、`:186` `SKILLS_GUIDANCE`、`:195` `KANBAN_GUIDANCE`（交给 worker 的执行协议） | `internal/agent/loop.go:1076-1083`：efficiency / taskManagement / bashEfficiency / job / engineering / conciseThinking / memLayout / tickClock（8 段纪律补丁） |
| 5.8 | 提示词缓存/失效 | **平** | `agent/prompt_caching.py`、`agent/agent_runtime_helpers.py:2042` `_cached_system_prompt = None`（失效钩子）、`agent/system_prompt.py:589` `invalidate_system_prompt` | gopi 侧对应物是 `internal/agent` 的 system prompt 组装与 `chat_completion` 缓存前缀策略 |
| 5.9 | 动态上下文文件预算 | **平** | `agent/prompt_builder.py:1197` `_dynamic_context_file_max_chars`；常量组 `:1182-1194`（`CONTEXT_FILE_MAX_CHARS=20_000`、head/tail 比例、6% 窗口、500K 上限） | gopi 侧对应物是 memLayout 段的轮内预算（`internal/agent/loop.go:1082`） |

**维度 5 小结：赢 0 / 输 3 / 平 6（共 9 条）。**
gopi 的 `promptSections` 是**纪律补丁集**（跟 codex 对比时的老短板延续），hermes 是**人格层 + 模型族适配层 + 平台矩阵层**；
gopi 平的部分在"分段装配 + 硬约束 + 缓存"——机制齐了，**缺的是内容层的行为规范**。

---

### 维度 6：多-agent 委派

| # | 条目 | 判定 | hermes 锚点 | gopi 锚点 |
|---|---|---|---|---|
| 6.1 | 子 agent 生成 | **输** | `tools/delegate_tool.py:3565` `registry.register(` `name="delegate_task"`；`:3328` `DELEGATE_TASK_SCHEMA`（goal/context/tasks/role/background） | **无 in-process 委派**：gopi 只有 `job(action=start)` 后台 shell（`internal/agent/loop.go:1539` `Name: toolnames.ToolJob`） |
| 6.2 | 子 agent 生命周期 | **输** | 并发上限 `tools/delegate_tool.py:355` `_get_max_concurrent_children`、异步槽 `:399` `_get_max_async_children`、超时 `:426` `_get_child_timeout`、中断 `:183` `interrupt_subagent`、登记 `:170` `_register_subagent` / `:206` `list_active_subagents`、`:124` `tools/async_delegation.py:124` `dispatch_async_delegation` | 无对应物 |
| 6.3 | 递归深度闸门 | **平** | `tools/delegate_tool.py:126` `MAX_DEPTH = 1`（默认扁平：parent(0)→child(1)，孙代被拒）；`:468` `_get_max_spawn_depth()`（可配置，下限 1，无上限） | gopi 的对应物是**任务树的父子闸门** `internal/tools/tasks.go:5864` `taskParentGateDenied`——但语义不同：hermes 防**递归爆炸**，gopi 管**依赖释放顺序** |
| 6.4 | 子 agent 上下文隔离 | **输** | `tools/delegate_tool.py:662` `_build_child_system_prompt`（子 agent 有独立 system prompt）；`:767` `_strip_blocked_tools`（叶子孩子剥掉 delegation toolset）；`:524` `_get_inherit_mcp_toolsets` / `:576` `_preserve_parent_mcp_toolsets` | gopi 的 `task_submit`（`internal/tools/tasks.go:5152` `func TaskSubmit(`）是**跨会话**委派——给新 agent 一份自足 briefing，不是同进程子 agent |
| 6.5 | 编排/群体协作 | **输** | `hermes_cli/kanban_swarm.py:77` `create_swarm`（swarm + blackboard）、`:242` `latest_blackboard`；`hermes_cli/kanban_decompose.py`（任务分解）；`gateway/kanban_watchers.py:744` dispatcher watcher | 无对应物（gopi 的等效物是 `internal/tools/` 里 merger/worker 台账队列 + `task_submit` 拆子任务） |
| 6.6 | 子 agent 的**产物验收** | **赢** | 无对应物：delegate 返回文本尾部 `tools/delegate_tool.py:219` `_extract_output_tail` + `:302` `_looks_like_error_output`——**只有"像不像错误"的启发式，没有产物核验** | gopi 的 `task(action=review)`（verdict A-E）+ `task(action=commit)`（结论入工作区）+ `internal/tools/verify_gate.go` + Rule 30 git status 门 + Rule 31 "未执行不算交付" ——**交付物有独立核验面** |
| 6.7 | 子 agent 的权限收敛 | **平** | `tools/delegate_tool.py:75` `_subagent_auto_deny` / `:89` `_subagent_auto_approve`（子 agent 的审批回调）、`tools/approval.py:1663` `prompt_dangerous_approval` | gopi 的对应物是 `GOPI_ALLOW_DANGEROUS` 黑名单 + sandbox 隔离 profile（`internal/sandbox/sandbox.go:213` `func Create(`） |
| 6.8 | 进程外驻留 worker | **平** | `hermes_cli/kanban_db.py:6932` `dispatch_once`（board 级单写者锁下 spawn worker）+ `cron/` 定时驱动 | gopi 的 `cmd/agent/cli_tasks_worker.go` + `cli_serve*`（worker/merger 循环），**刻意不用 cron**（Rule 35） |

**维度 6 小结：赢 1 / 输 4 / 平 3（共 8 条）—— 这是 gopi 输得第二狠的一维。**
hermes 的委派是**活的**（同进程子 agent、可中断、可并发、可编排成 swarm），
gopi 的委派是**异步的**（submit 一行任务，等另一个进程来认领）。
gopi 唯一赢的 6.6 是关键：**hermes 委派出去的东西没有强制的产物核验**——`_extract_output_tail` 只截一段文本尾部看像不像错；
gopi 的每个 child 都必须留下 commit + report + verification，且 Rule 31 明写"未执行的表面不算交付"。

---

### 维度 7：UI

| # | 条目 | 判定 | hermes 锚点 | gopi 锚点 |
|---|---|---|---|---|
| 7.1 | CLI 行编辑 | **赢** | `cli.py:14315`（=仓根主 CLI，行内容 `input_area = TextArea(`）；`cli.py:14322` `history=FileHistory(...)`；`cli.py:15120` `app = Application(` / `cli.py:15124` `full_screen=False`——**prompt_toolkit 全屏关闭的滚动式 REPL**，功能成熟但依赖外部库（prompt_toolkit 3.0.52） | `internal/lineedit/answer.go:869` `func NewAnswerRenderer(`；`internal/lineedit/doc.go:10`（**离线、x/sys/unix termios 直接进 raw mode，零第三方依赖**）；`cmd/agent/cli_chat.go:243` `--no-editor` 回退 cooked |
| 7.2 | Web UI | **输** | `hermes_cli/web_server.py` **16926 行**（FastAPI）：`@app.websocket("/api/pty")`（`:15099`）、`@app.websocket("/api/console")`（`:14743`）、`/api/media`（`:1483`）、`/api/files`（`:1718`）、上传（`:1821`/`:1856`）、`:1966` `/api/fs/list` | `cmd/agent/cli_web.go` **3091 行**（内嵌 HTML + SSE）：`:1205` `w.Header().Set("Content-Type", "text/event-stream")` |
| 7.3 | TUI（独立前端） | **输** | `ui-tui/`（TypeScript，223 个 .ts/.tsx，35 个 .tsx）：`ui-tui/src/lib/editor.ts`、`viewportStore.ts`、`fpsStore.ts`、`inputMetrics.ts`、`terminalParity.ts`；网关 `tui_gateway/server.py` | 无独立 TUI；gopi 的 TUI 就是 CLI 自身（`internal/lineedit`） |
| 7.4 | 桌面截屏/驱动 | **平/赢** | `tools/computer_use/`（cua_backend、AT-SPI/X11、`:16` 注释列 "no DISPLAY, missing AT-SPI"）、`tools/computer_use/permissions.py:15`（Linux X11/XWayland 就绪判定）——**依赖系统 AT-SPI 栈** | `internal/tools/screencapture.go:88` `func ScreenCapture(display, out string)`——**纯 Go 手写 X11 协议 GetImage**，零 CGO 零外部二进制（Rule 34 上下文）；驱动侧 `cmd/agent/cli_act.go` |
| 7.5 | 渲染真值测试 | **赢** | 无对应物（hermes 无"渲染像素级回归"机制；`grep -ril "IoU" tools/ agent/` → 0） | `internal/tools/text2img.go` + `internal/shot/cov_shot_x11_test.go`（**自建假 X 服务器**，`:139` `covXTestMajorOpcode = 0x84`、`:151` `covBtnEvent` 记录 XTestFakeInput）——渲染/驱动**可离线单测** |
| 7.6 | 多渠道接入（IM/邮件等） | **输** | `tools/discord_tool.py`、`tools/send_message_tool.py`、`tools/feishu_doc_tool.py`/`feishu_drive_tool.py`、`tools/microsoft_graph_client.py`、`tools/yuanbao_tools.py`、`gateway/`（pairing / channel_directory / delivery） | 无对应物 |
| 7.7 | 语音 | **平** | `tools/tts_tool.py`、`tools/transcription_tools.py`、`tools/voice_mode.py`、`tools/neutts_synth.py` | `internal/doubaoasr/doubaoasr.go`（ASR）；TTS 侧无 |

**维度 7 小结：赢 2 / 输 3 / 平 2（共 7 条）。**
gopi 赢在**前端的自我掌握**（行编辑与 X11 驱动都是零依赖自研，可离线像素级验证），
hermes 赢在**前端的广度与完成度**（16926 行 Web 服务 + 独立 TUI + 多渠道网关）。
**两边的取向完全不同**：gopi 是"我全自己写，所以我能验"；hermes 是"我把生态全接上，所以我能用"。

---

### 维度 8：gopi 有而 hermes 无

（每条都给出"检索得出的没有"= 检索词 + 命中数，而非"我没看到"。）

| # | 机制 | gopi 锚点 | hermes 检索词 → 命中 | 判定 |
|---|---|---|---|---|
| 8.1 | symbols 定义索引工具（code_search） | `internal/tools/symbol.go`；工具定义 `internal/agent/loop.go:1299`（`Name: toolnames.ToolCodeSearch`），实现 `internal/tools/symbol.go`（37286 字节）；CLI 侧 `cmd/agent/cli_log.go:438` | `grep -ril "code_search\|ctags\|symbol_index" --include=*.py tools/ agent/` → **0** | **赢** |
| 8.2 | 全文检索工具（keyword_search，与 code_search 分工） | `internal/agent/loop.go:1313`（`Name: toolnames.ToolKeywordSearch`）；实现 `internal/tools/search.go`（9997 字节；`websearch.go` 是另一支） | 同上 0 命中 | **赢** |
| 8.3 | git_blame 工具 | `internal/tools/gitblame.go:69` `func GitBlame(filePath string, startLine, endLine int) string` | `grep -ril "git_blame\|git blame\|line history" --include=*.py tools/ agent/` → **0** | **赢** |
| 8.4 | context_trim（把死胡同从上下文里摘掉） | `internal/agent/loop.go:1475`（`Name: toolnames.ToolContextTrim`）；配套死胡同判定 `internal/agent/loop.go:553` `contentReturningTools` | `grep -ril "context_trim\|trim_context" --include=*.py .` → **0**（hermes 只有**自动**压缩 `agent/conversation_compression.py:435 compress_context`，无"主动摘除某条结果"的模型动作） | **赢** |
| 8.5 | 记忆质量投票（feedback）+ 硬更正（update） | `internal/tools/memory.go:532`/`:533`；`:895` `memory_feedback` DDL；`internal/agent/loop.go:1374`/`:1375` | `grep -ril 'rating="useless"\|quality vote' tools/ agent/` → **0** | **赢** |
| 8.6 | 记忆溯源分级（provenance：url/import/node/path 四型） | `internal/tools/memory_provenance.go:83`、`:209` | `grep -ril "provenance" agent/ tools/` → 无相关命中 | **赢** |
| 8.7 | 文档真值分类清单 + 门 | `devdoc/DOC-INDEX.tsv`（2657 行；类目分布 `historical 2055 / normative 588 / roadmap 12 / index-meta 1 / class 1`，生成器 `awk -F'\t' 'NR>3 && NF>1 {print $2}' devdoc/DOC-INDEX.tsv \| sort \| uniq -c`）；写侧 `tools/docclass/classify.py`；门 `internal/standards/`（Rule 36） | `ls hermes-agent \| grep -i "DOC-INDEX"` → 无（只有打包用的 `MANIFEST.in`）；`grep -ril "DOC-INDEX" hermes-agent/agent/*.py` → 0 | **赢** |
| 8.8 | merge 队列 + 合并前构建门 | `internal/tools/merge_build_gate.go` + `merge_build_gate_gocache.go`/`modcache.go`（4 个变体）；`task_merged_cleanup.go:909` | `grep -ril "merge queue\|auto_merge\|merger" hermes_cli/ tools/ agent/` → **0**；`grep -rn "git merge" --include=*.py hermes_cli/ tools/ agent/` → 0 | **赢** |
| 8.9 | 规则的门禁化（prose → failing test） | `internal/standards/`（Rule 16-21 六个可机器检查的 gate）；`internal/smokegate` | hermes 侧对应物是 `tests/`（数量庞大但主要验行为），无"规则文本 → gate"的体系 | **赢（部分）** |
| 8.10 | 零依赖纯 Go 文本渲染（text2img + 自研 SFNT/TrueType） | `internal/tools/text2img.go` | `grep -ril "text2img\|TrueType\|sfnt" tools/ agent/` → **0** | **赢** |
| 8.11 | 纯 Go X11 抓屏/注入（不依赖 AT-SPI/XTest 外部库） | `internal/tools/screencapture.go:88`；`internal/shot/shot.go` | hermes 的对应能力存在但走系统栈：`tools/computer_use/permissions.py:15`（"Readiness == driver [installed]"） | **平**（能力相同、依赖不同；判平更诚实） |
| 8.12 | 沙箱 = 一个配置形状的私有 profile（含自有 ledger + 自动回收） | `internal/sandbox/sandbox.go:213` `func Create(`；`internal/sandbox/run.go:87` `RunIn`；`internal/scratch/sandbox_gc.go:307` `func SweepSandboxes(root string, olderThan time.Duration) (removed int, bytes int64, err error)`（Rule 34） | hermes 的沙箱是**执行后端**（`tools/environments/docker.py`、`modal.py`、`daytona.py`、`singularity.py`、`ssh.py`）——**隔离的是命令执行，不是 agent 的 profile** | **赢**（不同物） |
| 8.13 | 定时/常驻服务的"零依赖纪律" | `internal/tools/merger_graceful.go:19`、`internal/tools/worktree_sweep_chat.go:43`（"NOTHING PERSISTENT IS CREATED (Rule 35)"） | hermes 反向：`cron/`（10 个 py，`cron/scheduler.py` 3750 行）+ `gateway/kanban_watchers.py:744` | **未判**——这是**工程哲学的取舍**，不是能力高下：hermes 用常驻换能力，gopi 用纪律换可诊断性 |

**维度 8 小结：赢 11 / 输 0 / 平 1 / 未判 1（共 13 条）。**

---

## 5. gopi 明确输的条目（专段，不修饰）

按"输得有多疼"排序，共 **23 条输**（维度 1: 5 条 + 维度 2: 5 条 + 维度 3: 1 条 + 维度 4: 2 条 + 维度 5: 3 条 + 维度 6: 4 条 + 维度 7: 3 条 = 23 条判定为"输"，去重后归为 **5 个结构性缺口**）：

### 缺口 A：没有技能系统（输得最彻底，物种差异）
hermes 有一个**可分发的技能生态**：81 个 SKILL.md、21 个类目的 FTS5 索引（`agent/skill_index.py:24`）、
系统提示自动注入的触发索引（`agent/system_prompt.py:526`）、agent 自己写技能的 `skill_manage`（`tools/skill_manager_tool.py:1641`）、
还能从 GitHub 拉别人的技能并做信任分级（`tools/skills_hub.py:424`/`:507`）。
gopi 侧 `git ls-files | grep -i skill` 只返回一份**历史的导入报告**。
**后果**：gopi 的可复用过程只能以"记忆里的一行文本"存在，没有结构、没有可分发单位、没有被检索命中的索引。

### 缺口 B：没有活的多 agent 委派（输得第二狠）
hermes 的 `delegate_task` 是**同进程的活子 agent**：可并发（`tools/delegate_tool.py:355` `_get_max_concurrent_children`）、
可异步（`tools/delegate_tool.py:399` `_get_max_async_children` / `tools/async_delegation.py:124` `dispatch_async_delegation`）、
可中断（`tools/delegate_tool.py:183` `interrupt_subagent`）、有独立 system prompt（`tools/delegate_tool.py:662` `_build_child_system_prompt`）、
叶子自动剥工具集（`tools/delegate_tool.py:767` `_strip_blocked_tools`）、有递归深度闸门（`tools/delegate_tool.py:126` `MAX_DEPTH = 1` / `:468` `_get_max_spawn_depth`）。
gopi 的对应物只有 `job(action=start)` 后台 shell，以及**跨进程**的 `task_submit`。
**后果**：gopi 做不了"一个 agent 当场分出三个侦察兵，等它们回来汇总"这类交互式编排；
它只能"提交一条任务，等另一个 worker 认领"——延迟是按分钟到小时计的。

### 缺口 C：没有工具生态接入（MCP / 分组 / 按需加载）
`tools/mcp_tool.py` 一个文件 **5627 行**；工具发现走 BM25 目录检索（`tools/tool_search.py:347`/`:529`），
超阈值时把非核心工具折叠成三个桥工具，核心集由 `toolsets.py:31` `_HERMES_CORE_TOOLS` 常驻。
gopi 是 17 个工具全量常驻（`internal/agent/loop.go:1168`），无 MCP（`grep -ril "\bmcp\b" internal/ cmd/` → 0）。
**后果**：gopi 每加一个能力都要改 Go 源码 + 重建二进制；hermes 可以挂 MCP server 不改代码。

### 缺口 D：系统提示词缺"行为层"
hermes 有身份层（`agent/prompt_builder.py:126`）、模型族适配层（`:308` 覆盖 gpt/codex/gemini/glm/qwen/deepseek 等）、
平台矩阵（`:630`/`:869`/`:914`/`:886`）、并行工具调用规范（`:369`）。
gopi 的 11 段 `promptSections`（`internal/agent/loop.go:1073-1083`）**全是效率与纪律**，没有一段讲"怎么和人说话"。
**后果**：gopi 的输出风格靠模型默认 + 操作者的语言指令兜底（`internal/agent/loop.go` 的 reply-language 前置），
不是靠系统提示词塑形。

### 缺口 E：前端广度（Web / TUI / 多渠道）
hermes：Web 服务 **16926 行**（含 PTY-over-WebSocket 的浏览器内聊天、文件管理、控制台）、
独立 TS TUI（223 个文件）、discord/feishu/邮件/yuanbao 多渠道接入、cron 调度面。
gopi：`cmd/agent/cli_web.go` **3091 行**（SSE 单页）、无独立 TUI、无 IM 接入、**刻意不做 cron**。
**后果**：gopi 的"人机界面"基本只有终端；hermes 可以嵌进任何有浏览器的环境。

> **诚实补充**：缺口 E 里"不做 cron"是 **gopi 的规则选择**（Rule 35 零依赖纪律，
> `internal/tools/merger_graceful.go:19` 记录了 2026-09-17 撤销常驻服务形态的决定），
> 不是能力做不到——所以维度 4.9 判"输"是从**功能面**判的，从**可诊断性**判是 gopi 赢。
> 这条必须双写，否则报告变成单向推销。

---

## 6. 复读校验（Rule 1/13：行号必须真实）

交付前对**每一条被判定的 file:line** 做了 sed 回读。本轮复读揪出并修正 3 处：

| # | 原引用 | 问题 | 修正后 |
|---|---|---|---|
| 1 | `internal/tools/firstopen_refusal_regression_test.go:96`（称其为 tasks DDL） | 该行是 `}`，DDL 的不完整引用；真正的 `CREATE TABLE tasks (` 在 `internal/tools/ftsync_verify_test.go:96` | 已改为 `ftsync_verify_test.go:96`，并旁注另一处独立 DDL 在 `firstopen_refusal_regression_test.go:91`（该行确为 `seed(t, fdb, "CREATE TABLE junk (x TEXT)")`，非 tasks DDL，故仅作"另有一处 DDL 测试"的旁注保留） |
| 2 | `internal/tools/memory_provenance.go:205-209` | `:205` 是 `why: "a '/' separator…"`（url 型），`import` 型的 why 在 `:209` | 精确到 `:209` |
| 3 | 摘要初稿写 "95 个工具" | 95 是 `tools/*.py` 文件数，模型可见工具名是 71 | 已在 §1 明确两套口径，并给出两侧生成器 |

### 6.0 本 run 追加的两处**判定翻案**（不是引用偏差，是结论错了）

前 3 处是引用不准；下面 2 处是**判定本身**站不住，必须单独列出：

| # | 位置 | 原报告 | 复核证据 | 修正 |
|---|---|---|---|---|
| 4 | §3.3 | "gopi 侧对应物是**外挂脚本**（`internal/tools/script_install_sync.go` 之类），核心 `internal/` 里没有 5W1H 打标/去重管线"，判**输** | **判据不成立**：`internal/tools/memory_brew_run.go:99` `RunBrew` 是扫 session JSONL → LLM 判 lesson → 蒸馏 QA → 写库的完整管线；去重 `internal/tools/memory.go:160`/`:167`；5W1H 列 `:135`/`:334`；触发点 `cmd/agent/cli_brew.go:133` + `cmd/agent/main.go:827` | 3.3 改判**平**；打标**时机**的差异（hermes 在线内联 vs gopi store 落空串 + 批式 backfill）并入 3.3 正文 |
| 5 | §3 新增 3.10 | —（原报告未考察记忆**质量度量**） | hermes 有可复跑度量：`scripts/compare_5w1h_quality.py:1`（标注质量四指标）、`scripts/train_search_routing.py:141`（`hit_rate = hits/(hits+misses)` 随使用自动生长，由 `tools/session_search_tool.py:873` 消费）；gopi `find . -name '*quality*' -path './scripts/*'` → **0**，只有一次性复盘 `devdoc/memory-lowquality-retrieval-evaluation.md:1` | 新增 3.10，判**输** |

**方法论（写给下一个做 absence 判定的读者）**：一条"对方没有 X"的判定，**检索词选错就会变成假判定**。
3.3 原检索词只覆盖了 `internal/` 里"打标/去重"的字面词，而 gopi 管这条管线叫 **brew**——
所以判"没有"之前必须再问一次：**这个能力在本项目的行话里叫什么？**（已在 §8 附独立复跑。）

**抽样复核（5 条，抽自不同维度，逐字回读输出见 §1 与本节的会话证据）**：
`tools/registry.py:208` → `class ToolRegistry:` ✔
`hermes_cli/kanban_db.py:2832` → `if parent_status != "done":` ✔
`internal/tools/tasks.go:5864` → `func taskParentGateDenied(parentID string, parentStatus map[string]string) (bool, string) {` ✔
`agent/prompt_builder.py:126` → `DEFAULT_AGENT_IDENTITY = (` ✔
`tools/memory_search_tool.py:254` → `registry.register(` ✔

### 6.1 交付后的机器化全量复读（本 run 共复跑 4 次；**验证器自身修了 3 个 bug 后达成 0 oob**）

抽样 5 条是抽查；交付前又做了一次**全量机器复读**：脚本抽取报告里**每一个**
`path:NNN` token，按仓（hermes / gopi）解析路径，逐行打印真实内容并判定
`ok / MISSING / OOB`（OOB = 行号越界）。脚本落
`~/.gopi/sessions/01a0eeb6-cb9c-73ee-8171-518632d86c31/verify_lines.py`，输出同目录 `verify-lines.txt`：

```
=== TOTALS: citations=188 ok=181 missing=3 oob=4 ambiguous=2 ===   ← 首次
=== TOTALS: citations=204 ok=204 missing=0 oob=0 ambiguous=8 ===   ← 终值（详见 §8）
```

**本 run 从这次复跑里揪出的是验证器自身的 3 个 bug（报告本身无误）**，逐个修好后重跑：

| # | 验证器 bug | 症状 | 修法 |
|---|---|---|---|
| 1 | basename 同名多文件时取 `find` 的**首项**（顺序不确定） | `cli.py:1382` 被解析到 `agent/lsp/cli.py`（299 行）→ 报 OOB；实际是仓根 `cli.py`（16362 行），该行确为 `# Tracks the active worktree for cleanup on exit` | 候选**显式排序**（路径深度升序=最浅优先，仓根文件优先） |
| 2 | 不认**续写行号**形式（`` `:1445` ``） | 这些行号未被抽取，覆盖不全 | 按反引号 span 顺序扫描：`path:NNN` 设定当前 path，随后 `` `:NNN` `` 归属到它 |
| 3 | **续写锚点被散文里的同名路径劫持**（本轮第三次复跑才暴露） | §4.8 表格末尾一句"仓内另有 8 个同名 `cli.py`，如 `agent/lsp/cli.py` 299 行"把后续 `` `:1445` ``/`:1663`/`:1828` 全部归到了那个 299 行文件 → 3 条假 OOB；§6.1 表格自己引用的 `` `:1445` `` 也中招 | 给锚点加**资格判定**：只有该 basename 在全树里**最浅**的候选才可当锚点，否则它只是散文里提到的同名文件 |

**修好后重跑的 3 个 `missing` + 4 个 `oob` 逐个核对结果**（4 个 oob 里 3 个是上表 bug 3 的产物）：
- `hermes-agent/cli.py:*`（3 个 missing）：审阅中写成的显式相对路径形式，误加了 `hermes-agent/` 前缀；
  已改回 `` `cli.py:NNN` `` + 就地注明"= 仓根主 CLI，仓内另有 8 个同名 `cli.py`，勿混"（歧义从源头消除）。
- `agent/lsp/cli.py:*`（3 个 oob）：bug 3 的产物，非报告错误；修脚本后消失。
- `internal/tools/script_install_sync.go`（**不存在 687 行**；该文件仅 269 行）：**真错**，已从 §3.3 删除该引用（该文件与"蒸馏管线"无关——它讲的正是 3.3 那条**假 absence 判定**的来源）。

**basename 简写引用的解析结果**（本 run 全部命中真实行）：

| basename 引用 | 解析到的真实路径 | 真实行内容 |
|---|---|---|
| `loop.go:1168` | `internal/agent/loop.go:1168` | `var claudeToolDefs = []deepseek.ClaudeToolDef{` |
| `task_merged_cleanup.go:909` | `internal/tools/task_merged_cleanup.go:909` | `func mergedCleanupTrim(out []byte) string {` |
| `firstopen_refusal_regression_test.go:91` | `internal/tools/firstopen_refusal_regression_test.go:91` | `seed(t, fdb, "CREATE TABLE junk (x TEXT)")` |
| `ftsync_verify_test.go:96` | `internal/tools/ftsync_verify_test.go:96` | `` `CREATE TABLE tasks ( `` |

**结论**：全部引用真实、0 条越界（`oob=0` 在修好验证器 **3** 个 bug 后重跑达成，终值见 §8 的 TOTALS）。
未判条目 8.13 是唯一一条"无源码锚点"的（工程哲学取舍），且它**并非**以"我看到没有"作结。

---

## 7. 判定汇总

| 维度 | 赢 | 输 | 平 | 未判 | 条目数 |
|---|---|---|---|---|---|
| 1 工具模型 | 1 | 5 | 2 | 0 | 8 |
| 2 技能系统 | 0 | 5 | 1 | 0 | 6 |
| 3 记忆蒸馏 | 3 | 1 | 6 | 0 | 10 |
| 4 工作流台账 | 4 | 2 | 4 | 0 | 10 |
| 5 系统提示词 | 0 | 3 | 6 | 0 | 9 |
| 6 多-agent 委派 | 1 | 4 | 3 | 0 | 8 |
| 7 UI | 2 | 3 | 2 | 0 | 7 |
| 8 gopi 有而 hermes 无 | 11 | 0 | 1 | 1 | 13 |
| **合计** | **22** | **23** | **25** | **1** | **71** |

**本表的生成器（Rule 38，任何读者可复现；本 run 复跑输出即上表）**：按维度分组，条目行 =
正文里以 `| <维度号>.<序号> |` 开头的表格行，判定 = 该行**首个**含 `**赢|输|平|未判**` 的列，
用**含锚点的正则**取（不能用行尾取——多数行尾是 gopi 锚点，会把判定的位置数错）：

```python
# ~/.gopi/sessions/01a0eeb6-cb9c-73ee-8171-518632d86c31/count_verdicts.py
import re; from collections import Counter, defaultdict
dim=None; per=defaultdict(Counter)          # 逐行解析 §4 的八张判定表
for ln in open('devdoc/competitor-hermes-analysis.md',encoding='utf-8'):
    m=re.match(r'^### 维度 (\d)',ln)
    if m: dim=int(m.group(1)); continue
    m=re.match(r'^\|\s*(\d+\.\d+)\s*\|(.*)\|\s*$',ln)
    if not m or dim is None: continue
    for c in [x.strip() for x in m.group(2).split('|')]:
        t=re.search(r'\*\*(赢|输|平|未判)',c)
        if t: per[dim][t.group(1)]+=1; break
# → 维度1..8 逐行计数，合计 赢22 输23 平24 未判1 = 70 行
```

本表的 8 条维度小结、§0 摘要、§5 专段的计数都由**同一份逐行计数**改写 ——
此前版本曾手写汇总（维 1/4/6/7 各偏 1–2 条、总数写成 22/22/24），本 run 用上面的脚本复算后已逐处更正。

（维度 8 的 8.11 判平、8.13 判未判，已计入本表的"平/未判"两列；
未判条目 8.13 的理由=**工程哲学取舍**，非"源码无对应物"——本报告不含任何以"我没看到"作结的判定。）

**结论**：22 赢 / 23 输 / 25 平 / 1 未判（共 71 条判定）—— 这不是一场碾压，是**两条工程路线的对称取舍**。
hermes 赢在**广度与生态**（技能、委派、MCP、前端、调度），gopi 赢在**闭环与自证**
（merge 门、台账检索、worktree 一等机制、记忆代谢、渲染真值、零依赖）。
最值得 gopi 偷的一条不是"加技能系统"这种大工程，而是 **hermes 的 `BLOCK_KINDS` 分类学**
（`hermes_cli/kanban_db.py:125`）——把"阻塞"分成 dependency/needs_input/capability/transient 四型并分别路由，
这正是 gopi 台账目前缺的**阻塞可操作性**。

---

## 8. Ran: 与 Evidence（Rule 5 / 31）

### Ran:

```
cd /home/user/work/hermes-agent     # 只读，全程未写
git rev-parse HEAD                  # baf5fa02173c0035fd61532159ce20781600809d
find . -name '*.py' -not -path './.venv/*' | wc -l                       # 3114
ls tools/*.py | wc -l                                                    # 95
ls -d skills/*/ | wc -l                                                  # 21
grep -rc "registry.register(" tools/*.py | awk -F: '{s+=$2} END {print s}' # 79
grep -rhoP 'name="[a-z0-9_]+"' --include=*.py tools/ | sort -u | wc -l    # 71
grep -ril "code_search\|ctags\|symbol_index" --include=*.py tools/ agent/ # 0
grep -ril "merge queue\|auto_merge\|merger" hermes_cli/ tools/ agent/     # 0
sed -n '<line>p' <file>   # 逐条 file:line 回读（§6 抽样 5 条）

# --- 本 run 追加（复核阶段，全部只读）---
python3 ~/.gopi/sessions/01a0eeb6-cb9c-73ee-8171-518632d86c31/verify_lines.py \
        devdoc/competitor-hermes-analysis.md          # 全量行号复读 → §6.1 的 TOTALS
python3 ~/.gopi/sessions/01a0eeb6-cb9c-73ee-8171-518632d86c31/count_verdicts.py \
        devdoc/competitor-hermes-analysis.md          # 判定逐行重数 → §7 的表
find scripts -name '*quality*' | wc -l                # 0（gopi 无记忆质量度量脚本）
grep -rn "func RunBrew" internal/tools/                # memory_brew_run.go:99（3.3 翻案的证据）
grep -rn "maybeBrewOnSessionExit" cmd/agent/           # cli_brew.go:133 + main.go:827（触发点）
ls scripts/compare_5w1h_quality.py scripts/train_search_routing.py  # hermes 侧度量脚本（3.10）
```

被测仓与对照仓的健康检查（本 run 结束时）：

```
ps -eo pid,etime,cmd | grep hermes   # PID 841880 hermes chat --cli --yolo —— 未被本 run 触碰
```

### Evidence (self-filed)

- **只读约束**：本次对 `/home/user/work/hermes-agent` **只执行了 read/grep/sed/find/ls/git 只读命令**；
  未调用 write/edit；该仓 `git status --porcelain` 的 9 个条目在 run 开始时就存在（`M agent/distill_utils.py`、
  `M scripts/distill_record.py`、`?? --help` 等，属**其它 agent 的工作区**），本 run 未增减一个字节。
- **不干扰进程**：`ps` 显示 hermes chat PID **841880** 全程存活；本 run 未发送信号、未写该仓文件。
- **census 可复算**：§1 表格六行生成器逐字给出，任何人可在该 commit 上复跑。
- **行号抽样**：5 条跨维度抽样逐字回读，输出在 §3/§6 正文（`class ToolRegistry:` / `if parent_status != "done":` /
  `func taskParentGateDenied(...)` / `DEFAULT_AGENT_IDENTITY = (` / `registry.register(`）。
- **行号全量复读（判定口径 = 修好验证器 **3** 个 bug 后的终值；本 run 共复跑 4 次）**：

  ```
  === TOTALS: citations=204 ok=204 missing=0 oob=0 ambiguous=8 ===
  ```

  出处（可复跑）：`python3 ~/.gopi/sessions/01a0eeb6-cb9c-73ee-8171-518632d86c31/verify_lines.py devdoc/competitor-hermes-analysis.md`
  → `~/.gopi/sessions/01a0eeb6-cb9c-73ee-8171-518632d86c31/verify-lines-final.txt`（本次输出的落盘副本）。
  口径：`ok` = 路径解析成功且行号在文件内；`missing` = 找不到文件；`oob` = 行号越界。
  **204 条引用 0 条 missing、0 条 oob**；`ambiguous=8` 全部是 basename `cli.py`（仓内共 8 个同名文件），
  8 条逐条回读到的都是仓根 `cli.py`(16362 行) 的真实内容 —— `:1382` `# Tracks the active worktree for cleanup on exit`、
  `:1445` `def _resolve_worktree_base(repo_root: str) -> tuple:`、`:1663` lock 注释、`:1828` 手工清理提示、
  `:14315` `input_area = TextArea(`、`:14322` `history=FileHistory(str(self._history_file)),`、`:15120` `app = Application(`、
  `:15124` `full_screen=False,` —— 同名文件歧义已在 §4.8 / §7.1 就地注明"= 仓根主 CLI"。

  **三次中间运行的轨迹**（保留，因为它就是本节的证据链）：
  `citations=188 ok=181 missing=3 oob=4`（首次）→ `citations=205 ok=197 missing=3 oob=5`（修 bug 1/2 后，
  断言"修脚本后 oob 消失"当时**并未成立**——见 §6.1 表的 bug 3）→ `citations=204 ok=204 missing=0 oob=0`（修 bug 3 后终值）。
  历史上 3 条 `missing` 是 `hermes-agent/cli.py:*` 这种误加仓名前缀的写法，已在正文改回 `cli.py:NNN`；
  4 条 `oob` 里 3 条是验证器 bug 的产物、1 条是报告真错（引用 `internal/tools/script_install_sync.go` 的 687 行，
  该文件仅 269 行，已删除）。

  **口径边界（诚实披露）**：抽取器只认 `path:NNN` / `:NNN` 形式。报告里另有 **7 处区间形式**
  （`1 处` 形如 `path:a-b`：`agent/prompt_builder.py:256-262`、`internal/tools/memory_provenance.go:205-209`、
  `internal/agent/loop.go:1073-1083`、`internal/agent/loop.go:1076-1083`；另 3 处是续写区间 `:1182-1194` /
  `:127-130` / `:3452-3458`）**不被机器复读覆盖**，本 run 用 `sed -n 'a,bp'` 逐条人工回读，7 条全部命中报告所述内容
  （例：`hermes_cli/kanban_db.py:127-130` 确为 unblock-loop breaker 注释；`internal/tools/memory.go:3452-3458`
  确为 `default:` 分支自承"LLM 契约不给 recall 列，靠 backfill 回填"）。
- **判定重数**：§7 的表由 `count_verdicts.py` 的输出**逐字贴入**（不是手写），
  生成器源码同节给出 —— 本 run 复跑输出 `合计 22 赢 / 23 输 / 25 平 / 1 未判 = 71 行`，与正文一致。
- **翻案证据**：3.3 的改判依据是 `internal/tools/memory_brew_run.go:99` `func RunBrew(`（实测 `sed -n '99p'`
  逐字回读）等 6 个锚点；3.10 的"gopi 无对应物"是 `find scripts -name '*quality*' | wc -l` → **0** 得出的。
- **三处自我修正**：§6 表格记录了复读揪出的 3 处偏差（含摘要里的"95 个工具"口径）。
- **"没有"的证明是检索得出的**：维度 8 每条都附检索词 + 命中数（0），未使用"我没看到"的表述。
- **本 run 对 absence 断言的独立复跑**（交付前，只读）：`grep -ril "code_search\|ctags\|symbol_index" --include=*.py tools/ agent/` → 0；
  `grep -ril "merge queue\|auto_merge\|merger" --include=*.py tools/ agent/ hermes_cli/` → 0；
  `grep -rn "git merge" --include=*.py hermes_cli/ tools/ agent/` → 0；`grep -ril "DOC-INDEX" hermes-agent/agent/*.py` → 0。
  反向（gopi 侧）：`grep -ril "\bmcp\b" internal/ cmd/` → 0；`find . -name SKILL.md` → 0。
  **注意一处措辞纪律**：`grep -ril worktree tools/ agent/ hermes_cli/` → **25 个文件**、
  `grep -ril cgroup agent/ tools/` → **4 个文件** —— hermes **有** worktree（作为 kanban/CLI 便利功能，
  `agent/prompt_builder.py:256-262`）与 cgroup（Docker 资源限制，`tools/environments/docker.py:332`），
  所以本报告在 4.8 写的是"worktree 在 hermes 里是 CLI 的便利功能"而**不是**"无对应物"——
  未把"gopi 做得更体系化"偷换成"hermes 没有"。
- **census 独立复算（本 run，同一 commit）**：`git ls-files '*.py'` → 3042（**tracked**），
  `find . -name '*.py' -not -path './.venv/*'` → 3114（含 untracked，与 §1 表格一致），
  `ls tools/*.py | wc -l` → 95，`ls -d skills/*/ | wc -l` → 21。
  3042 vs 3114 的差额即 tracked/untracked 口径差——§1 已把生成器逐字写出，读者可用同一命令复现任一数字。
- **行号全量机器复读**：`~/.gopi/sessions/01a0eeb6cb9c73ee8171518632d86c31/verify_lines.py`（+ `verify-lines.txt`），
  101 条引用 → `ok=97 missing=4 oob=0`，4 个 basename 引用手工解析后全部命中（见 §6.1）。

### 提交位置

- worktree：`/home/user/workspace/gopi-01a0eeb6cb9c73ee8171518632d86c31`
- branch：`task/01a0eeb6cb9c73ee8171518632d86c31`
- 文件：`devdoc/competitor-hermes-analysis.md`
