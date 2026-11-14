<!-- doc-class: historical -->
# 工具族审查：哪些适合 B 风格（command/action 合并）——逐个分析

日期 2026-09-09。触发：nick 问——若 B（单工具 action=view|commit）是好选择，
code_search/keyword_search 等是否也该 follow 成 command/param 风格？走一遍
所有 toolcall 审查。

## 23 个真工具全景
bash code_search context_trim edit git_blame job_kill job_output job_start
keyword_search memory_feedback memory_search memory_store memory_update read
task_evaluate task_submit text2img todo vision_analyze web_fetch web_search
write write_task_area

## 语义族分组 + 合并评估

### 族 1: 记忆族（memory_*）——天然 B 候选（5→1）
| 工具 | 职责 |
|---|---|
| memory_search | 查（query→结果）|
| memory_store | 写（content→存）|
| memory_update | 改（id+新内容→更新）|
| memory_feedback | 反馈（id+useful/unuseful→评分）|
| **潜在：memory_sync** | （CLI 已有——非工具）|

**评估：强合并候选**——同一对象（memory store）的 CRUD+检索，天然
`memory(action=search|store|update|feedback, ...)`。但注意：
- search 是纯读（并行安全）——与 store/update/feedback 写混合会拖累并行
- 四工具的 schema 差异大（search 要 query；store 要 content+tags；feedback
  要 id+score）——action 合并后参数变"按 action 条件必填"（复杂 schema）

### 族 2: 检索族（code_search/keyword_search）——你问的
| 工具 | 差异 |
|---|---|
| code_search | 代码符号定义查找（symbol.go——ctags 索引）|
| keyword_search | 关键词全文查找（FTS？）|

**评估：可合并但价值存疑**——两者都是"搜索"但**语义不同**（符号定义 vs
关键词）且**描述差异巨大**（code_search 有工作流锚定长描述——专门教 LLM
"review 时先查定义"）。合并成 `search(action=code|keyword)`：
- ✅ 少一个工具名
- ❌ code_search 的长描述（工作流锚定——feat/cs-llm-adoption 刚修的价值）
  在合并后要在 action=code 分支保留——schema 描述怎么按 action 变？OpenAI
  function calling **不支持按 action 分描述**——合并后描述要写通用版，工作流
  锚定稀释
- ❌ code_search 是 **parallelSafeTools**（并发读）——合并后同族问题

### 族 3: 文件族（read/write/edit）——**强烈不建议合并**
| 工具 | 用途 |
|---|---|
| read | 读文件（分页）|
| write | 写整文件（覆盖）|
| edit | 精准编辑（patch——替换唯一片段）|

**评估：绝不合**——这是四方竞争（2026-08-26）明确结论：read/write/edit
是**最高频三工具**，描述最裸（write 63 字符）但**每字符都关键**——合并会
造成：读（纯读并行）拖累写（串行）、edit 的"唯一匹配防错"语义稀释、
参数风暴（读要 offset/limit，写要 content，edit 要 old/new）。**安全关键**
工具保持独立最清晰。

### 族 4: 文件检索 vs 网络检索——可考虑但低优先
web_search（搜网页）/web_fetch（抓 URL）——语义不同（搜索 vs 抓取单页），
`web(action=search|fetch)` 可行但价值低（2 工具已是少数，描述都长）。

### 族 5: job 族（job_start/job_output/job_kill）——**建议合并**
| 工具 | 职责 |
|---|---|
| job_start | 起后台任务（command→job_id）|
| job_output | 取输出（job_id→结果）|
| job_kill | 杀任务（job_id→kill）|

**评估：好 B 候选**——同一对象（后台 job）三操作，天然
`job(action=start|output|kill, ...)`——且参数不冲突（start 要 command；
output/kill 要 job_id）——action 决定哪个参数必填，schema 清晰。job 族不是
parallelSafe（都碰进程），合并无损并行。

### 族 6: task 族（task_submit/task_evaluate/todo/write_task_area）——你的原始问题
- todo（只读自查）+ write_task_area（提交结论）= **B 候选**（你正在问的）
- task_submit（跨 session 提交）/task_evaluate（评估可做性）——不同对象
  （跨 session ledger vs 会话内）——不该混入

### 族 7: 单例工具（独立——不该合并）
| 工具 | 理由 |
|---|---|
| bash | 万能执行——独立 |
| context_trim | 特定机制（死胡同修剪）|
| git_blame | 只读 git 历史——独立语义 |
| text2img | 渲染——独立 |
| vision_analyze | 图像——独立 |
| read | 文件读——族 3 |

## 合并决策矩阵（按价值排序）
| 候选族 | B 合并后 | 价值 | 风险 | 建议 |
|---|---|---|---|---|
| job 族（3→1）| job(action) | 中高（对象清晰）| 低（参数天然互斥）| **做** |
| memory 族（4→1）| memory(action) | 高（对象清晰）| 中（search 并行 vs 写）| 做但保 search 并行 |
| todo+wta（2→1）| task_update(action=view|commit) | 中（你的问题）| 中（view 并行）| 你正在定 |
| code+keyword（2→1）| search(action) | 低（语义不同）| 高（描述稀释）| **不做**（你问的——code_search 工作流锚定会丢）|
| read/write/edit | —— | —— | 极高 | **绝不** |
| web（2→1）| web(action) | 低 | 低 | 可后置 |

## 核心原则（回答你的问题）
1. **B 风格适合"同一对象的多操作"**（job 对 job、memory 对 store、task 对
   task）——对象清晰时 action 枚举自然
2. **不适合"不同语义的并列工具"**（code_search=符号定义 vs keyword_search=
   关键词——不是同一对象的两个操作，是两个不同检索）——合并=人为凑对，
   且 OpenAI schema **不支持按 action 给不同描述**（长描述/工作流锚定会稀释）
3. **安全/高频工具绝不合并**（read/write/edit——四方竞争已验证价值）
4. **并行安全是硬约束**——只读子操作（view/search）混入可写工具会失去
   parallelSafeTools 资格

## 你的问题直接答案
- code_search/keyword_search **不该 follow B**——它们不是同一对象的两操作，
  是两种不同检索；合并后 code_search 的工作流锚定描述（cs-llm-adoption 刚
  修的——"reviewing → resolve EVERY unknown identifier FIRST"）会在通用
  search 描述里稀释——**采纳率会回退**（这是有 A/B 实证的教训：feat/
  cs-llm-adoption 修复 0.5%→高采纳，靠的就是独立工具+长描述）
- 真正该 follow B 的是 **job 族**（对象最清晰、参数天然互斥、无损并行）
- memory 族可做但要保 search 并行

## 待 nick 定
1. B 风格适用原则是否认可（对象级合并 vs 语义级不合并）？
2. job 族合并（job_start/output/kill → job(action)）做不做？
3. memory 族/todo+wta 的合并优先级？
