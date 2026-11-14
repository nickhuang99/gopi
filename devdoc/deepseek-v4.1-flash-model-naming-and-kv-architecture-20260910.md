<!-- doc-class: historical -->
# DeepSeek V4.1 Flash 拆解与模型命名真相（2026-09-10）

**来源**：nick 提供的官方定价页 + 新智元发布会截图（`~/work/claude/nick/deepseek-v4.1-flash.png`，1256×15224px）+ doubao 讨论（`~/work/claude/nick/KV-cache压缩与模型性能讨论.html`）
**结论级别**：权威（官方一手数据 + 服务端实测双重印证）

---

## 一、核心结论：deepseek-flash = V4.1-Flash，**一个模型**

### 服务端实测证据（`/tmp/ds_alias_probe.py`）

| 实验 | 结果 |
|---|---|
| A. 文本请求 × 3 别名 | 全部 `echo=deepseek-flash`，输出**逐字节相同**(sha 一致) |
| B. 视觉请求 × 3 别名 | 全部 `echo=deepseek-flash`，都正确读出图内文字 |
| D. 纯图片请求（无文字） | 两别名都返回完整描述 — 视觉是**原生内置** |
| E. `/models` | 只返回 `deepseek-flash` + `deepseek-v4-pro` |

**三个别名（`deepseek-flash` / `deepseek-v4-flash` / `deepseek-v4-flash-vision-exp`）全部归一到一个后端。**

### 官方定价页证据

模型细节表**只有两栏**：`deepseek-flash` / `deepseek-v4-pro`。
`deepseek-flash` 栏的"模型版本"写死为 **DeepSeek-V4.1-Flash**。

**判据：如果 V4-Flash 与 V4.1-Flash 是两个模型，价格表应有独立栏位。现在只有一行。**

### 命名演进的准确表述（修正常见误读）

```
V4-Flash (无视觉)          ──┐
                             ├──>  V4.1-Flash (原生多模态，单模型)
V4-Flash-Vision-Exp (实验)  ─┘       旧两个名字现在都路由到它
```

**不是"两个 flash 模型合并"，而是"一个升级后的新模型（V4.1）同时取代了旧的两个"。**
"-exp" 后缀是 experimental，那个专用视觉型号是过渡方案；V4.1 把视觉做成原生能力。

---

## 二、官方价格表（元 / 百万 tokens）

| | deepseek-flash | deepseek-v4-pro |
|---|---|---|
| **模型版本** | **DeepSeek-V4.1-Flash** | DeepSeek-V4-Pro-0813 |
| 输入·命中·空闲 | **0.02** | 0.15 |
| 输入·命中·高峰 | **0.04** | 0.30 |
| 输入·未命中·空闲 | **1** | 4.5 |
| 输入·未命中·高峰 | **2** | 9.0 |
| 输出·空闲 | **4** | 13.5 |
| 输出·高峰 | **8** | 27.0 |
| 上下文长度 | 1M | 1M |
| 输出长度 | 最大 384K | 最大 384K |
| 图像理解 | **支持** | **不支持** |
| 并发限制 | **2500** | 500 |

- 高峰时段：**工作日 9:00-12:00、14:00-18:00（北京时间）**，其余全为空闲（含周末）
- 空闲价 = 高峰价的一半
- flash 比 pro 便宜 **4.5~7.5 倍**，并发能力高 5 倍，且**独占视觉能力**

### V4 Pro 下线时间表
- **2026-09-14 12:00（北京时间）之后**：访问 `deepseek-v4-pro` 的请求全部路由到 V4.1-Flash，**按 Flash 价计费**
- 直到未来 V4.1 Pro 上线
- 旧名 `deepseek-v4-flash` / `deepseek-v4-flash-vision-exp`：对应模型已下线，请求由 V4.1-Flash 提供服务

### ⚠️ 我们配置的核对结果
- 全配置 grep `deepseek-v4-flash|deepseek-v4-pro` → 零残留
- `~/.gopi/settings.json`（生效）→ `deepseek-flash` ✅
- `~/.gopi/.ds_settings.json`（旧名）→ **2026-09-10 已统一为 `deepseek-flash`**（备份 `.bak-namefix-20260910`）
- `~/.hermes/config-deepseek.yaml` → `deepseek-flash` ✅

---

## 三、KV Cache 演进（本次最有工程价值的数据）

官方图表数据，Global KV Cache Per Token：

| 模型 | 时间 | 每 token KV Cache | 相对上一代 |
|---|---|---|---|
| DeepSeek-V1 | 2023.11 | 389,120 B | — |
| DeepSeek-V3.2 | 2025.12 | 48,068 B | — |
| DeepSeek-V4-Flash | 2026.04 | 3,514 B | **8.1× smaller** |
| **DeepSeek-V4.1-Flash** | **2026.09** | **890 B** | **13.7× smaller**（图中另标 3.9×） |

**相对初代 V1：KV Cache 缩小 437 倍**（官方原文："KV Cache 已经缩小了 437 倍"）

官方补充：HBM 需求降到 1/4，SSD 需求降到 1/8（vs 上一代）。

### 架构要点（来自官方 + doubao 分析）
- **550B 总参数 MoE**，非对称 **CED（Causal-Encoder-Decoder）** 架构
  - 输入激活 **8B**，输出激活 **16B**
- **CSA2**（Compressed Sparse Attention 2）：混合策略，部分上下文 4× 压缩 + 稀疏 Top-K 保留关键 token 原始 KV；跨层复用全局 KV 索引
- **HCA**（Heavy Compressed Attention）：最高 128× 压缩比，把连续上下文投影聚合为少量隐向量
- **Global KV Cache 的真正含义**：**同一会话内跨 Transformer 层共享一套 KV 池**（传统 80 层模型要存 80 份独立 KV，大量重复）；**不是跨会话/跨用户共享**（那会造成信息泄露）
- **不存在客户端压缩** — 客户端只能发原始文本/图片，压缩发生在服务端 prefill 阶段
- 压缩是**预训练学到的**（嵌入注意力层），非事后外挂

### 对我们的实际意义
gopi 长会话（如 7168c24d：463 轮、峰值 106K prompt tokens）服务端 KV 占用：
- V1 时代：106K × 389,120 B ≈ **41 GB**
- V4.1-Flash：106K × 890 B ≈ **94 MB**

**437 倍差距** — 这是"1M 上下文"今天能商业可行的底层原因，也解释了为什么缓存命中价能定到 0.02 元/百万。

**叠加效应**：gopi 应用层 compaction（trim 把峰值从 ~300K 压到 106K ≈ 3×）× 模型层 KV 压缩（3.9×）≈ **10× 综合成本改善**。这是 memory-aware 项目价值的外部佐证。

---

## 四、Agentic Benchmark 数据（V4.1-Flash vs 前沿模型）

来源：新智元截图 band-03/04 表格

| Benchmark | **V4.1-Flash** | V4-Pro-0813 | V4-Flash-0731 | GLM-5.3 | Kimi-K3 | GPT-5.6-Sol | Claude Opus 5 |
|---|---|---|---|---|---|---|---|
| GPQA Diamond | 90.9 | 92.4 | 89.9 | 88.1 | 92.9 | 94.1 | 93.4 |
| HLE | 36.8 | 42.7* | 37.8* | 42.0* | 43.5 | 44.5 | **56.3** |
| Codeforces (Rating) | **3471** | 3348 | 3289 | — | — | — | — |
| MathArena Apex | 65.6 | 65.3 | 58.6 | — | 65.6 | — | — |
| Terminal-Bench 2.1 | 90.6 | 87.9 | 82.7 | 88.2 | 88.3 | 88.8 | 89.1 |
| Terminal-Bench 3.0 | 30.0 | 11.8 | 7.6 | 28.3 | 17.7 | 34.4 | **43.3** |
| Terminal-Bench 4.0 | 31.2 | 12.4 | 7.0 | 37.9 | 12.6 | 39.9 | **51.8** |
| DeepSWE v1.1 | 74.2 | 62.7 | 54.4 | 66.9 | 67.5 | 73.0 | 74.0 |
| ProgramBench | 20.3 | 15.5 | — | 19.0 | 17.5 | 23.0 | **37.0** |
| NL2Repo-Bench | 65.4 | 61.5 | 54.2 | 58.0 | 58.0 | 56.8 | **75.3** |
| CyberGym | **88.1** | 83.3 | 76.7 | 84.5 | 80.0 | 84.5 | — |
| SEC-Bench Pro | 62.8 | 56.4 | 30.9 | — | — | 74.3 | — |
| ExploitGym | 15.3 | 5.4 | 1.8 | 15.0 | — | 33.7 | 22.1 |
| HLE (w/tools) | **63.9** | 60.0 | 51.5 | 62.5 | 59.8 | — | 63.6 |
| Automation-Bench | **54.8** | 43.2 | 37.7 | 48.8 | 46.7 | 45.8 | 50.3 |
| Agents' Last Exam | **31.8** | 25.7 | 25.2 | 28.5 | 27.6 | 26.7 | 28.6 |
| Chartography (w/tools) | 78.9 | — | — | — | 68.1 | 79.9 | **84.0** |
| BabyVision (w/tools) | 89.6 | — | — | — | 85.7 | 88.9 | **94.1** |
| ZeroBench-main (w/tools) | 49.0 | — | — | — | 41.0 | 53.0 | 52.0 |

**读法**：
1. **V4.1-Flash 全面超越 V4-Pro** — 官方"全面超越"有数据支撑（Terminal-Bench 3.0：30.0 vs 11.8；DeepSWE：74.2 vs 62.7；CyberGym：88.1 vs 83.3）
2. **Agentic 任务上逼近甚至反超 Claude Opus 5** — CyberGym 88.1 领先、Automation-Bench 54.8 领先、Agents' Last Exam 31.8 领先、HLE w/tools 63.9 微胜 63.6、DeepSWE 74.2 vs 74.0 打平
3. **纯深度推理上仍有明显差距** — HLE 36.8 vs 56.3、Terminal-Bench 4.0 31.2 vs 51.8、ProgramBench 20.3 vs 37.0、NL2Repo 65.4 vs 75.3

**这支持"同硬件/同成本约束下竞争才有意义"的判断**：Agentic 类任务吃上下文管理与工具编排，恰是 MoE + KV 压缩路线的强项；纯推理类吃参数密度，稠密模型仍占优。benchmark 只看能力不限算力，天然偏向稠密大模型。

---

## 五、风险观察点（HCA 压缩损失）

官方明确警告：**逐字回溯、精确引用原文细节**的场景，HCA 重度压缩会丢细粒度信息。

**对 gopi 的潜在影响**：write_task_area 结论链是精确文本（commit hash、`文件:行号`、错误码）。
**验证思路**：若未来在超长会话（>200K）中观察到 gopi 引用了**错误的 hash/行号**，应优先怀疑 KV 压缩损失，而非模型能力退化。

---

## 六、其它事实

- **模型已开源**：`https://huggingface.co/deepseek-ai/DeepSeek-V4.1-Flash`，另有技术报告 `DeepSeek_V41_Tech_Report.pdf`
- **官方合作伙伴**：腾讯（WorkBuddy、CodeBuddy）、OpenCode 已全量接入
- **新价格 2026-09-10 12:00 起生效**

---

## 七、纠错记录

**doubao 讨论（`KV-cache压缩与模型性能讨论.html`）中的错误**：
1. 价格写成"输入 0.02 元 / **千** token，输出 1.0 元 / **千** token" — 官方是**每百万**，且它的"输出"列实际是"未命中输入"价。**差 1000 倍 + 列串位**，不可用于成本核算。
2. 称"部署门槛高，本地跑不了，只能 API 调用" — 模型已开源（550B MoE 确实本地跑不动，但"只能 API"不准确）
3. "双重压缩"表述前后不一致（前半说"不存在客户端+服务端双重压缩"，后半说"双重压缩叠加"，实际后者指权重压缩+KV压缩两个层级）

---

## 八、工具与后续

- **价格监控脚本**：`~/work/GoPi/scripts/ds_pricing_snapshot.py` — 每次运行存快照 + 与上次 diff，表格变化会显著报告
- **快照目录**：`~/.gopi/pricing-snapshots/`（首个 baseline 20260910-195949.json）
- **9/14 监控 cron**：`ds-pricing-v4pro-retirement-check`（job c094702fce9f，9/14 12:10 触发，deliver=local 需手动查）
  - 目的：看官方在 V4 Pro 退役后如何重写价格表 — 是删掉 v4-pro 栏、改成镜像价、还是新增 V4.1 Pro
- **实测脚本**：`/tmp/ds_alias_probe.py`（别名归一验证）

---

## 附：踩坑记录

**vision 读万级像素长图会退化**：1256×15224px 直接喂给 vision_analyze → 模型陷入重复循环（同一表格行重复数百次）。
**正解**：按 ~1600px 高度裁条带（`/tmp/dsv41/band-00..09.png`）逐段读，内容准确无退化。
（与既有 memory 记录一致：HP 万级像素长图截断问题的同族陷阱。）
