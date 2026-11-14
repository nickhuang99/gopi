<!-- doc-class: normative -->
# Computer Use 冷静解剖：管线、玩家版图、5 个切口、不做清单

**Date**: 2026-09-10 · **性质**: 分析文档（无实现）· **作者**: gopi
**Branch**: `feat/cu-landscape-analysis` · **Task**: EPIC「Computer Use 全景 + 切口分析」

**阅读前提（诚实声明）**：
- 本文所有外部数字都带来源编号 `[S#]`，来源清单见 §8。**抓取失败的外部来源一律标 `未验证`**，不用"业内普遍认为"顶替。
- 本文所有本地数字来自本仓库已有的**实测**证据（§8 的 `[L#]`），不是回忆值。
- 本文**不写"我们要做 Computer Use"**。目标是：把一件大事拆成"已经很成熟的部分"和"真的新的那一个变量"，然后只在新变量上找一个 solo 能负担的最小切口。

---

## 0. 一句话结论（先说结论，后面全是证据）

| 问题 | 结论 | 支撑 |
|---|---|---|
| 这条管线难在哪？ | **不在"手眼"（capture/actuate/wait 已是几十年工业件），而在"判断"（state-understanding / verify-criteria / replan）** | OSWorld 2.0 原文：agent 的失败"rather than stumbling on basic GUI control or coding"，失在丢失约束、漏掉中途到达的信息、跳过验证、隐藏状态 [S4] |
| 瓶颈形态是什么？ | **不是精度不够，是单位动作成本与循环步数** —— 规划/反思/评判的模型调用占了绝大部分端到端延迟，且步数越多单步越慢（3×） | OSWorld-Human [S1] |
| 谁已经把成熟件做到极致？ | 硬件 capture/actuate：PiKVM（1920x1200@60Hz、35–50ms、免装软件、有 API、CE）[S11]；Web 域 wait/verify：Playwright auto-wait actionability 检查表 [S10] | — |
| 玩家版图有空格吗？ | **有，但空格很窄**：模型厂/云/legacy RPA/星标数万级的 OSS 全覆盖了"通用 CU + 浏览器 + 企业编排"。空格 = **本地优先、无云、非标准设备、长循环只读看护**。此判断是**推断**（依据 OSS→云的推送方向），不是穷尽枚举 → `未验证` | [S8][S9][S6][S12][S13] |
| solo 最优第一步？ | **niche A 的最小可证伪版本**：自有一台非标设备 + 确定性 diff 看护基线 + LLM 仅在 diff 命中时介入，跑 72h，只收 3 个数。成本：0 新硬件、≤2 人日、≤¥10 token | §7 |

---

## 1. 管线解剖：8 步，逐段贴标签（成熟 / 新 / 混合）

一条 Computer Use 操作的完整链路：**intent → capture → state → decision → actuate → wait → verify → recover**。

| # | 阶段 | 标签 | 成熟部分的证据（工业界已有的东西） | 新变量的部分 | 证据来源 |
|---|---|---|---|---|---|
| 1 | **intent**（人给任务） | 混合 | 参数化/表单化入参是 RPA 二十年常规件 | 自然语言 → 可执行意图的解析，全额依赖 LLM | [S12] Copilot Studio 用自然语言描述任务即配置 |
| 2 | **capture**（取世界状态） | **成熟** | ① HDMI/KVM 抓帧：PiKVM 1920x1200@60Hz、宣称 35–50ms 延迟、免装软件、纯 HTML5、有 API [S11]；② 操作系统截屏/取帧：X11/V4L2 等；③ 无障碍树：AT-SPI 等，语义化读出控件名与角色 | 无。这一层没有 LLM | [S11][L1][L2] |
| 3 | **state**（像素/控件 → 可决策状态） | **新变量** | 无障碍树能给"名字 + 角色 + 状态"（机器可读，无需推理）；Playwright 从 DOM 拿同一类东西 [S10] | **像素 → 语义**：没有 DOM/a11y 时，只能靠视觉模型 grounding。这是"新变量"最核心的一块 | OSWorld 2.0 把"implicit-state inference / hidden state"列为 agent 级挑战 [S4] |
| 4 | **decision**（规划/选择动作） | **新变量** | 无对应成熟件（规则引擎只能覆盖固定流程） | LLM 规划 + 反思 + 评判。**这是延迟与成本的主项** | OSWorld-Human：大模型调用（planning/reflection/judging）占端到端延迟绝大部分 [S1] |
| 5 | **actuate**（改变世界） | **成熟** | ① 软件注入：XTEST/xdotool 级键鼠事件；② 无障碍动作接口：`grab_focus` 0.15 ms、`do_action(0)` 0.08 ms；③ 硬件注入：USB-HID 芯片（CH9329 一类，~$15 级）免驱免装、OS 无关 | 无。这是纯粹的成熟件 | [L1][L4] |
| 6 | **wait**（等物理/界面延迟） | **成熟（策略层混合）** | Playwright 的 auto-wait：动作前自动等待 visible / stable / receives-events / enabled / editable，超时才报 TimeoutError [S10]；KVM/硬件侧帧率与延迟是已知常数 [S11][L2] | **"等多久"的策略**在非标准应用上只能靠观察与试错（无 DOM 可等） → 策略属新变量 | [S10][L2] |
| 7 | **verify**（确认动作生效） | 混合 | 有机器可读信号时是纯代码：返回码、DOM 断言、a11y 状态回读（本例 3.23 ms 一次）[L1] | **开放式屏幕任务的"成功判据是什么"**：判据要 LLM 设计、部分成功率要 LLM 判。OSWorld 2.0 明确把"skip verification"列为失败模式之一 [S4]；OSWorld-Human 把 judging 归入延迟主项 [S1] | [S1][S4][L1] |
| 8 | **recover**（失败后重规划） | **新** | 重试/退避/幂等是成熟工程件 | 长循环下的漂移恢复：单次 rollout 极脆、小错复利，多 rollout + 行为判优（BJudge）才把 OSWorld 推到 72.6%（≈人类 72.36%），代价是 N 倍 rollout | [S7][S4] |

### 1.1 关键区分（这是本文要交的作业）

> **成熟件** = capture（硬件与系统级）、actuate（软件注入 / 无障碍动作 / USB-HID）、wait（auto-wait 机制与硬件时序）、以及所有"有机器可读信号"时的 verify。
> **新变量** = 无 a11y/DOM 时的像素→状态理解、任务分解与动作选择、开放式任务的验证判据设计、长循环漂移后的重规划。

**这不是"整个都很难"，而是"两端硬、中间软"**：手（5）和眼（2）已经商品化到几十美元一块板子 [S11][L4]；卡住的是第 3/4/7/8 步，且卡住的方式是**成本与步数**，不是"看不清按钮"。

### 1.2 用证据反证"手眼不是瓶颈"

- OSWorld 2.0 的失因清单原文点名的四件事：**lose track of constraints / miss information that arrives mid-task / guess rather than ask the user / skip verification**，且**明确排除**了基础 GUI 控制与写代码 [S4]。
- OSWorld-Human 的量化：成功路径下 **最优 agent 也用了必要步数的 2.7–4.3 倍**，且**越到后面单步越慢（可达 3×）**——这是循环结构与上下文负担的性质，不是点击精度问题 [S1]。
- 原始 OSWorld：人类 72.36% vs 当时最好模型 12.24%（369 题）[S2]；到 OSWorld 2.0 长程 108 题、人类中位 1.6 小时、agent 平均约 250 步/318 次工具调用，最好成绩**二值完成率仅 20.6%**（500 步上限，Opus 4.8 最高思考 + 批量调用），GPT-5.5 约 14% [S4]。**两年过去，"眼睛没坏，判断还是不够"。**

---

## 2. 玩家版图（含空格位）

| 层 | 玩家 | 他们卖什么 | 证据 |
|---|---|---|---|
| 模型厂 | Anthropic（Computer Use tool）、OpenAI（CUA 模型） | 把 CU 做成一个**工具**，由下游平台调用；Copilot Studio 里两者均为 Generally available | [S12] AI 抓取 Anthropic 官方 docs 两次超时 → Anthropic 侧细节 `未验证`；OpenAI Operator 页 HTTP 403 → `未验证` |
| 云 / 平台 | Microsoft Copilot Studio（computer use 工具，跑在 Windows 机器上，配虚拟鼠标键盘）、Skyvern Cloud（含反爬、代理、验证码）、Browser Use Cloud（宣称 stealth browser + 基础设施） | 把"能跑起来的基础设施"商品化：机器、代理、录制、数据策略 | [S12][S9][S8]；AWS Nova Act 两次抓取失败（404）→ `未验证` |
| legacy RPA | UiPath 等 | 企业编排 + 治理 + 既有 RPA 资产。**规模是真的**：FY2026（截至 2026-01-31）营收 $1.61B、+12.65%；FY2025 $1.43B、+9.30%；员工 3,981 | [S13]。**但增速从 FY2021 的 80.76%、FY2024 的 23.57% 降到 12.65%** — 同源 [S13] |
| CU 初创 / 框架商 | Simular（Agent-S 开源 12,260 stars / 1,440 forks / Apache-2.0；托管产品 Sai） | 开源框架拉开发票，托管版收钱：Sai 宣称 OSWorld 2.0 达 73%，"比 GPT 与 Opus 更低成本"（厂商自述） | [S5][S6] |
| OSS | browser-use（114k stars / 12.5k forks / 10,279 commits）、Skyvern、Agent-S | 免费层 + 云托管变现；browser-use README 直接把 "Path 1: Fully Hosted Cloud" 摆在第一 | [S8][S9][S5] |
| 硬件 / 通道 | PiKVM（工业级 KVM over IP）、CH9329 类 USB-HID 桥 | 卖"人类 I/O 通道"本身：免装软件、OS 无关、可进 BIOS | [S11][L4] |
| **空格位（本文判断）** | **solo/small team：本地优先、不依赖云、面向非标准/legacy 设备、长循环（小时~天）无人值守、且以"只读看护/低风险动作"起步** | 无人在抓取到的证据里以这个组合出现 | **推断**（依据：OSS 头部普遍向云托管收钱 [S8][S9]，云平台把 CU 包成企业工具 [S12]，legacy RPA 卖平台治理 [S13]）→ 标注 `未验证`：未做穷尽枚举，"没人做"只能作为假设用于下一步实验，不作为结论 |

**版图读法**：这张表里没有人缺"眼睛和手"。所有人挤在 (a) 更聪明的通用大脑、(b) 更便宜的执行基础设施、(c) 企业合规与编排。**solo 在这三条里都没有位置**——这也正是下面 §6 不做清单的来源。

---

## 3. 5 个候选切口 × 5 维度矩阵

**维度定义**（每格必须带来源或显式 `未验证`）：
- **D1 需求与付费方**：今天谁在为这件事付钱、付多少。
- **D2 新变量占比**：价值里有多大比例真的来自 LLM 那块新变量（占比低 = 成熟件就能解决 = solo 无优势）。
- **D3 竞品密度**：抓到的证据里已经有人在做吗。
- **D4 solo 最小验证成本**：能证伪的最小实验，需要多少钱/多少人日/什么设备。
- **D5 决定性风险**：这件事最致命的单点是什么（会因为哪一件事而死）。

| | A 长循环无人值守看护 | B 非标准/legacy 最后一公里设备 | C 自托管/隐私 GUI 自动化 | D 记忆层作为中间件 | E LLM 原生视觉操作技能库 |
|---|---|---|---|---|---|
| **D1 需求与付费方** | 需求存在但付费方分散：`未验证`（未找到任何"值守型 CU"的采购或市场规模数据；OSWorld 2.0 明说其 108 题含"日常与专业流程"，却全部是**有人在场**的 benchmark [S4]） | legacy 存量是硬证据：RPA 厂商 FY2026 营收 $1.61B 且仍有 12.65% 增长 [S13]；CU 的通用价值主张就是"没有 API 时也能做"（Copilot Studio 原文：complete tasks even when there's no API）[S12] | 企业侧需求方向有证据但**价格未知**：云 CU 明确卖基础设施与"profiles/recordings/data policies" [S8]、Skyvern 云卖反爬/代理/验证码 [S9] ⇒ "不想上云"是一个可推断的对偶需求 → `未验证`（无企业采购证据） | 中间件的付费方只能是上游 agent 商或框架商；`未验证`（未找到任何人为此单独付费的证据） | `未验证`（无采购证据）。学术侧只有"知识→执行"缺口：UI-Evol 报告**即使 90% 正确的知识，执行成功率也只有 41%** [S14] |
| **D2 新变量占比** | **高**：判据设计与"看到变化后该不该叫人"属第 3/7 步，无成熟件 [S4] | **中**：actuate/capture 全是成熟件（硬件通道 [S11][L4]），新变量集中在"无 a11y 时的状态理解" [S4] | **低**：核心难点是部署形态（本地/离线），不是模型能力；成熟件（截屏、HID、auto-wait [S10][L4]）占比高 | **高**但被判据锁死：记忆的收益要在长循环里才显现，而长循环本身是 OSWorld 2.0 里最差的部分（20.6% / 500 步）[S4] | **中**：与 UI-Evol 同向（把执行轨迹回灌成知识 [S14]），但"技能库"的检索-执行落差已被量化（90%→41% [S14]） |
| **D3 竞品密度** | **低**：抓到的证据里没有以"小时~天级无人值守看护"为主张的产品；这是从"所有头部 OSS 都把 CU 讲成一次任务/一次 workflow"反推的（browser-use 的 demo 叙事是"订一次驾照考试" [S8]，Skyvern 是 workflow 自动化 [S9]）→ 反推部分 `未验证` | **中高**：legacy 是 RPA 主战场（营收 $1.61B [S13]），GUI-agent 化的最后一公里有云 CU 覆盖（"没有 API 也能做" [S12]） | **低**：OSS 侧有自托管能力（Agent-S Apache-2.0 可本地跑 [S5]；Skyvern `pip install` 本地 SQLite [S9]），但**没有以"隐私/离线"为卖点的商品** → 密度低 = 也说明无人验证过付费意愿 | **高**：Agent-S 仓库 topics 直接含 `memory` [S5]；Simular 自己就把记忆/规划当特性写进框架 [S5][S6] ⇒ 中间件位置正被上游吃掉 | **中**：UI-Evol 是 plug-and-play 模块 [S14]，Agent-S 系论文（S2/S3）自带经验/技能机制 [S6][S7] |
| **D4 solo 最小验证成本** | **最低**：0 新硬件（用自有设备/本仓库已有的 capture 与 HID 套件 [L2][L4]）+ 确定性 diff 基线 + LLM 仅在变化时调用；≤2 人日，≤¥10 token（按本仓库 flash 视觉实测每图 ≤384 tokens [L3] 量级估算） | **中**：需要一台真实 legacy 设备/系统（非本机可造）；本仓库已测过"无 a11y 的应用"这条通道（Xaw 应用在 AT-SPI 里完全不出现 [L1]）但仍需一台真设备 → `未验证`：成本取决于能否借到设备 | **低-中**：软件形态、无硬件；但"隐私"这个卖点的验证需要企业访谈，非工程实验能证 → `未验证` | **高**：中间件的收益只能在长循环里测，而长循环单次实验就要 250+ 步 [S4]、且延迟是"数十分钟级" [S1] | **高**：技能库需要大量成功轨迹；单次 rollout 脆弱需多 rollout [S7]，solo 无力批量产生轨迹 |
| **D5 决定性风险** | 需求侧：看护场景的错误代价与"漏报"惩罚不对称，误报会迅速杀死信任 → `未验证`（无实测误报率） | 供给侧：legacy 设备**不可复制**，每个客户一套现场，规模不经济（RPA 厂商靠平台与治理覆盖这一点 [S13]，solo 无此杠杆） | 付费意愿：`未验证`（无人做过"隐私溢价"的定价证据；反而云平台在把基础设施做得更便宜 [S8][S9]） | 被上游吞并：已有框架把 memory 列为自带特性 [S5] ⇒ 独立中间件的窗口取决于上游是否补齐 | 经济性：多 rollout 才能稳 [S7]，成本×N 与"低时薪场景成本权重最高"直接冲突 |

**矩阵读出的一件事**：
- D2 高的（A/D/E）恰好是 **D4 成本高的**（长循环 → 步数多 → 成本×N）；
- D4 最便宜的（A/C）恰好是 **D5 里证据最薄的**（无付费证据）；
- **唯一同时"新变量占比高"且"最小验证成本最低"的格子是 A**，代价是它的 D5（误报杀死信任）必须用实验先证。

---

## 4. 不做清单（do-not-touch）与理由

| # | 不做 | 理由（证据） |
|---|---|---|
| 1 | 通用桌面 CU 基座 / 全栈 agent 框架 | 对标物是 12,260 stars 的 Agent-S（论文级方法：BJudge 多 rollout 才到 72.6% [S5][S7]）与 114k stars 的 browser-use [S8]。差距不在代码量，在**模型 + rollout 预算 + 评测集** [S4][S7] |
| 2 | 浏览器网页自动化（DOM/Playwright 域） | 这一域最成熟的 wait/verify 已被 Playwright auto-wait 做成默认行为 [S10]，LLM 增益点已被 Skyvern（WebVoyager 85.8% 自述 [S9]）与 browser-use 占满；solo 的边际贡献接近零 |
| 3 | 自研 KVM / HDMI 抓帧硬件 | PiKVM 已是工业级：1920x1200@60Hz、35–50ms、免装软件、CE、有 API [S11]。自制等于重做成熟件——违反"成熟件复用"原则 |
| 4 | GUI 数据集 / 标注 / 训练 GUI 专用模型 | 资本与数据密集（MagicalGUI / GUI-World 一类数据集工作 [S17]）；solo 无算力与采集面 |
| 5 | 企业级 RPA 平台 / 编排 / 合规 | 付费方要的是治理、审计、服务与集成；对照物员工 3,981 人、人均营收 $432,268 [S13]。solo 交付不了这颗信任 |
| 6 | 面向消费者的"通用电脑助手"订阅 | 与四个模型厂 + 三个云 + 数个 10 万星开源项目正面同质竞争 [S8][S9][S12]；无差异化支点 |
| 7 | 对抗鲁棒性 / 提示注入防御做成独立产品 | 研究阶段就够深（WebInject 用像素扰动诱导截图型 web agent，EMNLP 2025 [S15]；弹出窗注入攻击 + CVPR-26 级防御 LaSM [S16]），但**买家是谁 `未验证`**。做能力可以内嵌，做产品没有证据 |
| 8 | 全自动无人值守**写操作**（改数据 / 提交表单 / 付款） | 误操作代价不对称，且当前 agent 在长程任务上二值完成率 20.6%（500 步）[S4]；OSWorld 2.0 专门另附 safety 审计报告也说明这类执行需要单独的安全面 [S4]。solo 无兜底能力 |
| 9 | 把 CH9329/HID 通道本身当产品 | 本仓库结论：它只是**一条 Actuate 通道**（~$15 级、免驱、OS 无关），独立成产品无价值 [L4] |
| 10 | 移动端 App 自动化（Android/iOS） | 平台条款与封控决定了这是合规问题而非技术问题；本次**未取得任何可引用证据** → `未验证`，故不入候选 |

---

## 5. gopi 编码 vs Computer Use：4 原语同构分析

原语骨架（本仓库既有结论 [L5]，非本文新造）：代码域 `Read / Write+Edit+Bash / sleep / 校验返回码` ↔ 屏幕域 `Capture / Actuate / Wait / Verify`。

| 原语 | 编码域（gopi 今天） | 屏幕域 | 同构度 | 关键差异（这才是成本来源） |
|---|---|---|---|---|
| **Capture** | `read`：读文件内容。状态是**文本、可寻址、可 diff** | 截屏 / a11y 树。像素是**非寻址**的，a11y 树是半寻址（名字可寻址） | 结构同构，代价不同 | 本地实测：读 a11y 状态 **3.23 ms**（中位），读像素 **195.99 ms**（中位）——**60×** [L1] |
| **Actuate** | `write`/`edit`/`bash`：按**路径/符号名**操作 | 键鼠注入 / a11y 动作 / USB-HID | 结构同构 | 本地实测：按名字的 `do_action` **0.08 ms**、`grab_focus` **0.15 ms**，与目标距离无关；坐标/键盘走位是 O(N)（CH9329 串口路径推算 17–40 ms/Tab，属协议推算 [L4]）[L1] |
| **Wait** | `sleep`，以及进程/命令的自然同步 | Playwright 式 auto-wait（visible/stable/receives-events/enabled/editable）[S10]；硬件时序常数 [S11][L2] | **同构度最高**：都是"回到可动作状态再继续" | 屏幕域多一层：无 DOM 时"稳定"要靠**观察定义**，属新变量 [S4] |
| **Verify** | 返回码 / 单测 / 编译——**免费且确定性** | 有机器可读信号时同样免费（a11y 回读 3.23 ms [L1]）；无信号时要设计判据 | **同构，但前提不同** | 本地核心结论：**验证不花 LLM 调用**——a11y/像素 diff 是确定性代码，一次任务里坐标瞄准方案 ≈2S 次 LLM 调用，a11y-first 方案 ≈1–2 次 [L1] |

**三条可迁移的结论（不是"我们要做 CU"，而是"这套骨架怎么复用"）**：
1. **同构是真的**：四个原语在两边一一对应，所以不需要新范式，只需要**补齐通道**。
2. **"按名字寻址"是编码域的全部红利，也是屏幕域唯一真正稀缺的东西**：有 a11y/DOM 时验证与动作都退化成代码（0.08 ms / 3.23 ms [L1]，auto-wait [S10]）；没有时，一切退回像素，成本与不确定性同时上升（195.99 ms + identity-blind [L1]）。
3. **长循环成本问题两边同源**：屏幕域步数多（OSWorld 2.0 平均约 250 步 [S4]），编码域是历史累积与上下文膨胀；本仓库已有的上下文管理（trim / 任务区提交 / 结论前移）面对的是同一类问题 [L3][L5]。**但这一步只能算"结构相同"，不能算"已经解决"** —— 没有任何证据表明编码域的压缩策略在像素观测流上同样有效 → `未验证`。

---

## 6. 诚实不确定清单（uncertainty list）

**外部证据缺口（抓取失败或未取得）**：
1. Anthropic Computer Use 官方文档：两次抓取超时 → 其分辨率/计费/限制条款 `未验证`。**后果：无法计算云 CU 的单位动作成本，因此本文任何 ROI 判断都不成立。**
2. OpenAI Operator / Nova Act：分别 HTTP 403 与 HTTP 404 → 产品状态与定价 `未验证`。
3. Google Gemini computer use：搜索命中全是无关噪声，无有效来源 → `未验证`。
4. UiPath 等 RPA 厂商的 **AI/CU 收入占比**：只拿到总营收与增速 [S13] → "RPA 正被 CU 替代还是被 CU 加强" `未验证`。
5. 误报率 / 漏报率 / 真实生产成功率：**没有任何一个**（benchmark 分数是沙箱内的执行式判定 [S2][S4]，人类基线 72.36% 是标注者基线 [S2]）→ 生产可用性 `未验证`。

**方法学弱点（我自己的）**：
6. "空位"是**推断**（从头部 OSS 向云收钱、云平台把 CU 工具化的方向反推 [S8][S9][S12]），不是穷尽枚举 → 只能当假设。
7. 本地延迟数字有已知偏差方向：webcam 抓帧实测 5.7–5.9 fps（60 帧 / 10.55 s）跑在 **2015 年 i7-5500U 单线程**上，JPEG 解码 23.64 ms@2304x1296 [L2] → **对现代机器是保守偏差**（真实值更快），但没有反向证据说这一层会慢。
8. a11y 通道的**跨 OS 对应物未实测**：本文只用 Linux/AT-SPI 实测过（且实测到 Xaw 类应用完全不出现 [L1]）；Windows UIA / macOS AX 的覆盖与延迟 `未验证`。
9. CH9329 的 17–40 ms/Tab 是**协议推算**（9600 8N1 + HID 轮询，硬件当时未接入 [L4]）→ `未验证`。
10. "知识→执行"缺口的量级来自单篇论文的单模型实验（90%→41% [S14]）→ 外推需谨慎。

**关于方向本身**：
11. 我**没有**任何证据表明"更低单价的多模态模型会让上述瓶颈消失"。能证明的只有：延迟主项是**模型调用次数与步数** [S1]，而步数是 2.7–4.3× 冗余 [S1]、长程二值成功率 20.6% [S4] → 这两项是结构与判断问题，不是单价问题。
12. 所以本文不给时间表预测。§7 给的是一个**可被证伪的最小实验**，不是一个路线图。

---

## 7. 推荐的第一步（niche A 的最小可证伪版本）+ 最小验证成本

**只推荐一个切口：A（长循环无人值守看护），且必须用"只读 + 确定性基线先行"的方式做。**

### 7.1 为什么是 A，且为什么必须这样切
- A 是矩阵里唯一"新变量占比高 + 最小验证成本最低"的组合（§3）；
- A 的 D5 风险（误报杀死信任）**正好可以用一个便宜实验直接测量**，不需要先解决任何模型问题；
- 只读意味着第 8 步 recover 的风险被移除（不做写操作 = 不做 §4 第 8 条），这是 solo 唯一能承担的形态。

### 7.2 实验（一次性，可证伪）
**目标**：在一台**自有的、非标准的**目标上（一台老系统/老设备/无 a11y 的界面），回答"LLM 这块新变量在这里到底有没有增量"。

**步骤**（0 新硬件）：
1. 确定性基线：capture + 帧差/区域变化检测 + 阈值告警，**不带任何 LLM**，连续跑 72 小时。
2. 同一 72 小时内，仅在 diff 命中时调用一次视觉模型做"这是什么变化 / 该不该叫人"的判断（本仓库 flash 视觉实测每图 ≤384 tokens [L3]，因此调用率 = 成本），记录它的判断。
3. 人工在最后复核这 72 小时的**全部**命中事件，作为 ground truth。

**只收 3 个数**（这就是"最小验证成本"的全部产出）：
- **N1**：确定性基线的误报/漏报（precision/recall）。
- **N2**：LLM 调用次数/小时 + 折算 token 成本/小时。
- **N3**：LLM 判断**改变了**结论的事件数（LLM 与确定性基线不一致，且 LLM 对）。

**成本**：0 新硬件；≤2 人日；≤¥10 token（按 N2 量级）；72 小时挂机。
（设备：本仓库已有 capture 与 HID 套件的实测基础 [L1][L2][L4]，无需新买。）

### 7.3 预先写好的判决规则（避免事后解释）
- **N3 ≥ 5 且 N1 的 precision ≥ 0.95** → A 成立：新变量有增量，且基线不会用误报淹没信任 → 进入第二步（把"看护"扩到更多自有目标，仍只读）。
- **N1 precision < 0.95** → 先修第 3/7 步（状态表示与判据），**不要**动模型。
- **N3 ≤ 1** → **A 对 LLM 价值为零**，切口死亡，记录并转向 B（同一套 harness，只换目标为"非标准设备的最后一公里"）。
- 任何一个数拿不到（例如 72h 内 0 次命中）→ 目标选择失败，换目标重跑，而不是调参数。

**这个实验的独立价值**：无论结果如何，N1/N2/N3 都是外部找不到的数（§6 第 5 条：生产误报率 `未验证`），是唯一能拿在手里的东西。

---

## 8. 来源清单

**外部（本次通过 web 工具实际抓取，URL 为所引内容所在页）**

| 编号 | 来源 | URL |
|---|---|---|
| S1 | OSWorld-Human: Benchmarking the Efficiency of Computer-Use Agents（延迟与步数冗余） | https://arxiv.org/abs/2506.16042 |
| S2 | OSWorld 官方站（人类 72.36% vs 最好模型 12.24%，369 题） | https://os-world.github.io/ |
| S3 | OSWorld 仓库 README（OSWorld-Verified，2025-07-28） | https://github.com/xlang-ai/OSWorld |
| S4 | OSWorld 2.0（108 长程题、人类中位 1.6h、约 250 步/318 次工具调用、20.6% 二值完成、失因清单） | https://osworld-v2.xlang.ai/ |
| S5 | Agent-S 仓库元数据（12,260 stars / 1,440 forks / Apache-2.0 / topics 含 memory、planning） | https://api.github.com/repos/simular-ai/Agent-S |
| S6 | Agent-S README（S3 72.60% 超人类 72.36%；Sai 自述 OSWorld 2.0 73%） | https://raw.githubusercontent.com/simular-ai/Agent-S/main/README.md |
| S7 | Scaling Agents for Computer Use / BJudge（单 rollout 脆弱、多 rollout 到 72.6%） | https://arxiv.org/abs/2510.02250 |
| S8 | browser-use 仓库页（114k stars / 12.5k forks / 云托管为第一路径 / stealth + 基础设施） | https://github.com/browser-use/browser-use |
| S9 | Skyvern README（LLM+CV 浏览器自动化、云含反爬代理验证码、本地 SQLite、WebVoyager 85.8% 自述） | https://raw.githubusercontent.com/Skyvern-AI/skyvern/main/README.md |
| S10 | Playwright Auto-waiting / actionability 检查表 | https://playwright.dev/docs/actionability |
| S11 | PiKVM 官方站（1920x1200@60Hz、35–50ms、免装软件、API、CE） | https://pikvm.org/ |
| S12 | Microsoft Copilot Studio — computer use（无 API 时也能做；模型与 GA/Experimental 状态） | https://learn.microsoft.com/en-us/microsoft-copilot-studio/computer-use |
| S13 | UiPath 营收史（FY2026 $1.61B +12.65%；FY2025 $1.43B；员工 3,981；人均营收 $432,268） | https://stockanalysis.com/stocks/path/revenue/ |
| S14 | UI-Evol（90% 正确知识 → 41% 执行成功） | https://arxiv.org/abs/2505.21964 |
| S15 | WebInject（像素扰动提示注入，EMNLP 2025） | https://arxiv.org/abs/2505.11717 |
| S16 | LaSM（弹出窗环境注入攻击与防御，CVPR-26） | https://arxiv.org/abs/2507.10610 |
| S17 | Large Language Model-Brained GUI Agents: A Survey | https://arxiv.org/abs/2411.18279 |

**抓取失败 / 未验证的外部来源**：Anthropic Computer Use docs（超时 ×2）、OpenAI Operator（403）、AWS Nova Act（404×2）、Google Gemini computer use（无有效命中）。

**本地实测证据（`[L#]`，均在主仓库 `/home/user/work/GoPi`，为**未提交**的他人工作产物，仅作只读证据引用）**

| 编号 | 位置 | 本文用到的数字 |
|---|---|---|
| L1 | `devdoc/tab-order-actuate-poc.md`（+ `devdoc/tab-order-poc/`，66 张 PNG 证据） | AT-SPI 读状态中位 3.23 ms；X11 10.63 ms；像素 195.99 ms；键注入 15.59 ms；`grab_focus` 0.15 ms、`do_action(0)` 0.08 ms；Xaw 应用（xmessage）在 a11y 总线完全不出现；坐标瞄准 ≈2S 次 LLM 调用 vs a11y-first ≈1–2 次；两独立进程逐步像素 md5 全等 |
| L2 | `devdoc/webcam-capture-evidence/`（`hw-inventory-raw.txt`、`capture-latency-raw.txt`、`closed-loop-metrics.json`） | 真实 host webcam 05c8:0374：60 帧 10.55 s（=5.7 fps，1280x720 MJPEG）、640x480 YUYV 10.21 s（5.9 fps）；JPEG 解码 23.64 ms@2304x1296（单线程 i7-5500U）；闭环伺服 9–14 次迭代收敛，末端真实误差 10.6–27.7 px，均命中 widget |
| L3 | `devdoc/computer-use-second-blue-ocean-4-primitives-20260910.md` | flash 视觉模型每图 ≤384 tokens、实测约 56 tokens；上下文命中/新增 ≈22.4×/22.9× |
| L4 | `devdoc/ch9329-hid-actuator-evaluation.md` | CH9329 = 免驱免装 OS 无关的 USB-HID Actuate 通道，~$15 级；本文引用时**硬件未接入**，开销为协议推算（17–40 ms/Tab）→ 标 `未验证` |
| L5 | 同上 L3 文档（4 原语同构框架） | 原语对照骨架（本文 §5 沿用并加了本地量化） |

---

## 9. Rule 5 任务报告（Completed / Not Done / Verification / Commit）

### Completed
- 8 阶段管线解剖，逐段贴 `成熟 / 新变量 / 混合` 标签，并给出反证（§1、§1.1、§1.2）。
- 玩家版图表（模型厂 / 云 / legacy RPA / CU 初创 / OSS / 硬件通道）+ 明确的空格位与其推断性质（§2）。
- 5 切口 × 5 维度矩阵，25 格逐格带来源编号或显式 `未验证`（§3）。
- 不做清单 10 条，每条带理由与证据（§4）。
- gopi 编码 vs CU 的 4 原语同构分析，含本地量化差异（§5）。
- 诚实不确定清单 12 条，含抓取失败清单与自身方法学弱点（§6）。
- 单一推荐切口 + 可证伪实验 + 预算 + 预设判决规则（§7）。
- 来源清单：17 个外部来源 URL（其中 ≥3 个为本次 web 工具**实际抓取**正文）+ 5 组本地实测证据（§8）。

### Not Done（诚实列出）
- 未取得 Anthropic / OpenAI / AWS / Google 的一手产品文档与定价 → 因此**没有做任何 ROI 计算**（§6 第 1–3 条）。
- 未做"空位"的穷尽枚举 → 空格位按**假设**对待，不作为结论（§6 第 6 条）。
- 未实测 Windows UIA / macOS AX 通道，未实测 CH9329（硬件未接入）→ 相应数字标 `未验证`（§6 第 8–9 条）。
- 未运行 §7 的实验（本次任务范围是分析；实验是推荐的下一步）。

### Verification
- 文档产物：`devdoc/computer-use-landscape-and-niches.md`（本文件）。
- 抽查式核对：文中每个数字均可在 §8 的 URL 或 `[L#]` 路径上复核；每个矩阵格以 `[S#]`/`[L#]` 或 `未验证` 收尾。
- Web 工具抓取证据：S1/S10/S11/S12/S13/S14/S15/S16/S17（arXiv 摘要页与官方站正文）+ S3/S4/S6/S8/S9（README/站点正文）+ S5（GitHub API JSON）。
- 构建/测试：本任务为纯文档，无代码改动 → 无 build/test 步骤（`git show --stat` 见 §9 提交行）。

### Commit
- 分支 `feat/cu-landscape-analysis`（工作树 `~/workspace/gopi-cu-landscape`，Rule 6）。
- 只提交本文档 + 报告文件（Rule 8：逐文件 `git add`，不用 `git add -A`）。
- 提交哈希与 `git show --stat` 输出见同分支的 `devdoc/computer-use-landscape-analysis-report.md`。
