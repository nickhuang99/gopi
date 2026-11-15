<!-- doc-class: normative -->
# Devdoc: DeepSeek pricing table repair — `deepseek-flash` (V4.1-Flash) 定价修复

**任务**：epic 01a08daf（`gopi stats --cost` 对当前唯一在用模型完全失效）
**分支**：`feat/pricing-deepseek-flash`（worktree `~/workspace/gopi-pricing`）
**日期**：2026-09-11
**性质**：**数据修复（data repair）** —— 只改价格表，复用既有 `Tier`/`Until`/`Peak` 机制，
没有任何逻辑改动。

---

## 一、根因（5-why）

| # | 现象 | 结论 |
|---|------|------|
| 1 | `gopi stats 055d468f --cost` 报 `cannot price model "deepseek-flash"` | 该模型无价格 tier |
| 2 | 表里只有 `deepseek-v4-flash`（`internal/pricing/pricing.go`） | 表停留在**旧模型名** |
| 3 | 2026-09-10 官方发布 **DeepSeek-V4.1-Flash**，模型名改为 `deepseek-flash`，旧名 V4-Flash **退役** | 官方改名（非我们改配置） |
| 4 | 改名同时**降价**：缓存命中 0.05 → 0.02（−60%），未命中 1.5 → 1，输出 4.5 → 4 | 旧表不仅缺名字，数字也过期 |
| 5 | 生产 settings 在改名当天已切到 `deepseek-flash` | **我们唯一的成本可见性工具对唯一在用的模型失效**；即使加别名也只解决报错、不解决 2.5x 高估 |

**机理性结论**：这不是"加个别名"，是**两张卡片的换代**。只补名字 = 命中项系统性高估 2.5 倍。

---

## 二、官方价格（已核实，非工单转述）

**主来源（实时抓取）**：`https://api-docs.deepseek.com/zh-cn/quick_start/pricing/`
**抓取时间**：**2026-09-11T08:39:59+08:00**（页面 23,602 bytes HTML→text，`模型细节` 段逐字解析）
**离线快照**：`~/docs/deepseek-api/quick_start/pricing.md`（EN 版，2026-09-11 归档）
**英文版同页**（USD 栏，用于交叉验证）：`$0.003/$0.006`、`$0.15/$0.3`、`$0.6/$1.2`（flash）
—— **CNY 数字只取自 zh-cn 页**，不与 USD 混算。

### 官方原文（zh-cn 页 `模型细节` 表，逐字）

```
模型                deepseek-flash (1)                deepseek-v4-pro (2)
模型版本            DeepSeek-V4.1-Flash               DeepSeek-V4-Pro-0813
价格 (3)
  百万tokens输入(缓存命中)  空闲 0.02元   高峰 0.04元     空闲 0.15元   高峰 0.30元
  百万tokens输入(缓存未命中) 空闲 1元      高峰 2元        空闲 4.5元    高峰 9.0元
  百万tokens输出            空闲 4元      高峰 8元        空闲 13.5元   高峰 27.0元
```

### 生效时间 / 路由规则（官方 change log `~/docs/deepseek-api/updates.md`，2026-09-10 条）

- **价格生效**：新价自 **2026-09-10 04:00 UTC = 12:00 +08** 生效（news260910.md）。
- **旧名仍在用**（脚注 1）：`deepseek-v4-flash` / `deepseek-v4-flash-vision-exp` 仍可调用，
  但对应模型已下线，请求由 V4.1-Flash 提供服务，**按 Flash 价格计费**。
- **V4 Pro 退役**（脚注 2 / change log）：**北京时间 2026-09-14 12:00 之后**，
  至未来 V4.1 Pro 上线之前，`deepseek-v4-pro` 请求**全部路由到 V4.1 Flash，按 Flash 价计费**。
  （英文 news 写 `04:00 UTC on Sept 14` —— 同一时刻，+08/UTC 互证。）

### 高峰时段规则（确认，无需改代码）

zh-cn 脚注 (3)：`空闲时段价格为高峰时段价格的一半。高峰时段为北京时间周一至周五 9:00-12:00、14:00-18:00。`
英文脚注 (3)：`Peak hours are 01:00-04:00 and 06:00-10:00 UTC, Monday through Friday.`
→ **同一时刻的两种写法**（01:00 UTC = 09:00 +08）。
→ 现有 `pricing.inPeakHours()`（**小时窗口，不含星期判断**）与之**逐条一致**：
`{9,12},{14,18}` 半开区间 + 空闲=高峰/2。**未改动**。（周末按官方应全为空闲，见"不确定性"。）

---

## 三、价差表（旧表 vs 官方现行，元 / 1M tokens）

| 维度 | 旧表 `deepseek-v4-flash`（空闲/高峰） | 官方现行 `deepseek-flash`（空闲/高峰） | 变化 |
|---|---|---|---|
| 缓存命中 | 0.05 / 0.10 | **0.02 / 0.04** | **−60%**（旧表高估 **2.5×**） |
| 输入未命中 | 1.5 / 3.0 | **1 / 2** | −33%（旧表高估 **1.5×**） |
| 输出 | 4.5 / 9.0 | **4 / 8** | −11%（旧表高估 **1.125×**） |
| 合计（1M 命中+1M 未命中+1M 输出，空闲） | 6.05 元 | **5.02 元** | −17% |

新增（旧表完全没有）：`deepseek-v4-pro` 卡 **0.15/4.5/13.5（空闲）、0.30/9/27（高峰）**。

---

## 四、改动（严格的表数据修复）

`internal/pricing/pricing.go`，`Default()` 表由 2 条 DS tier → **10 条**：

| 模型名 | 空闲/高峰 | 适用时刻 | 卡片 |
|---|---|---|---|
| `deepseek-v4-flash` | 空闲 / 高峰 | **< 2026-09-10 12:00 +08** | 旧卡 0.05/1.5/4.5（账单核实） |
| `deepseek-v4-flash` | 空闲 / 高峰 | ≥ 2026-09-10 12:00 +08 | **新卡**（旧名由 V4.1-Flash 服务） |
| `deepseek-flash` | 空闲 / 高峰 | 全程 | **新卡 0.02/1/4 + 0.04/2/8** |
| `deepseek-v4-pro` | 空闲 / 高峰 | **< 2026-09-14 12:00 +08** | Pro 卡 0.15/4.5/13.5 |
| `deepseek-v4-pro` | 空闲 / 高峰 | ≥ 2026-09-14 12:00 +08 | **Flash 卡**（路由后计费） |

配套（仍属"表数据"而非逻辑）：

- **命名常量**（PROJECT.md Rule 1b/13）：模型名 `modelDSFlash` / `modelDSV4Flash` / `modelDSV4Pro`、
  10 个 regime 标签、6 张卡（`dsFlashIdle`/`dsFlashPeak`/`dsV4FlashOldIdle`/…）
  全部为常量，**表里只引用，不重复写字面量**；两个截止时刻为 `dsPriceCutover` / `dsProRetire` 变量。
- **`Tier.Ref`（新增字段）**：卡片来源与 provider 默认来源不同时自带出处
  （V4.1-Flash 卡来自新的官方定价页，而非旧的账单导出）。
  `resolve()` 里 `match.Ref` 优先，其次才是 provider 级 `refDS`/`refGLM5`/`refGLM`
  —— 即 **新增 3 行取值逻辑**，不改变任何既有 tier 的解析结果（GLM/旧 DS 行为逐字节不变）。
- **标签策略**：`< 9/10 12:00` 的旧名 tier **完整保留原标签与原数字**（对该时段会话它就是真实 regime，
  且被 gopilog golden 钉住）；跨过截止点的 tier 用**新标签**（同名不同价，读者不会混淆）。
- 常量顺序整理：`Model 字段` 的注释补全为"三个模型名、两个截止点"。
- **GLM 段未动**（有意）：GLM tier 仍用字面量，但那是**本次修复之前**就存在的状态，
  与本次 DS 数据修复无关 —— 保持 diff 只含 DS 价格数据（约束 1），不去重排无关代码。
  若要做，应作为独立的 Rule 13 清账单。

---

## 五、验收证据

### 5.1 真实会话（验收项 3）——`055d468f` 从报错 → 出钱

```
$ ~/work/GoPi/bin/gopi stats 055d468f-d565-4050-b819-b4a354c5bc37 --cost      # 修复前 master 二进制
gopi stats: cannot price model "deepseek-flash" (unknown provider — no price table; see internal/pricing)
```

```
$ /tmp/gopi-pricing stats 055d468f-d565-4050-b819-b4a354c5bc37 --cost         # 修复后
model          deepseek-flash
rounds         187
prompt tokens  8,040,824   (avg 42,999/round)
cache          96.96% hit (7.80M) / 245K miss
completion     84,463
┌ COST — Session 055d468f-d565-4050-b819-b4a354c5bc37 ┐
│ total cost      0.7385 元 @ CNY                      │
│   cache hit     0.1559 元  (7.80M tokens)            │
│   input miss    0.2447 元  (245K tokens)             │
│   output        0.3379 元  (84K tokens)              │
│ money split      21.1% /  33.1% /  45.7%             │
│ regime          DS flash 空闲                        │
│ n² amp (money)  0.64x                                │
│ priced at       2026-09-11 07:27 +0800 Beijing       │
│ price ref       https://api-docs.deepseek.com/zh-cn/quick_start/pricing/ (官方定价页 zh-cn 元栏, fetched 2026-09-11T08:39:59+08:00; …) │
└──────────────────────────────────────────────────────┘
```

**非零总额 0.7385 元**，三维分解 + 占比 + regime + 出处齐全。
（`priced at` = 该会话 `session_start` 时间 07:27 +08，属**空闲时段**，与 regime 自洽。）

手工反推（用官方卡 0.02/1/4 逐维验算，与框内数字互相印证）：
- 命中 0.1559 元 ÷ 0.02 × 1e6 ≈ **7.795M** tokens（框内 `7.80M`，四舍五入一致）
- 未命中 0.2447 元 ÷ 1 × 1e6 ≈ **245K** tokens（框内 `245K miss` 一致）
- 输出 0.3379 元 ÷ 4 × 1e6 ≈ **84.5K** tokens（框内 `84,463` 一致）
- 三项相加 = **0.7385 元** = 框内 `total cost`（占比 21.1/33.1/45.7% 亦自洽）

即：**框内钱数可由官方现行价格表逐维反推出原始 token 量**，说明 regime/卡片选择正确，
不是"能出数字"而已。

### 5.2 测试（验收项 2、4）

```
$ go build ./...                                        # BUILD ok
$ go test -count=1 ./internal/pricing/ ./internal/gopilog/
ok      github.com/the operator/gopi/internal/pricing   0.077s
ok      github.com/the operator/gopi/internal/gopilog   2.259s   # golden 未改
```

新增 5 个测试（`internal/pricing/deepseek_v41_test.go`）全绿：

```
--- PASS: TestFlashModelPricesAtOfficialV41Rates     # 新名可定价 + 精确费率 + 高峰=2×空闲 + ref 指向官方页
--- PASS: TestLegacyV4FlashNameStillPrices           # 旧名仍可定价（截止前旧卡 / 截止后新卡），含 -0731/-vision-exp
--- PASS: TestNewAndLegacyFlashPricesDiffer          # 同名token不同钱 5.02 vs 6.05；命中 2.5×、输入 1.5×；截止后两名同价
--- PASS: TestV4ProBillingReroutesAtCutoff           # 9/14 12:00 前 Pro 卡 18.15 元 / 后 Flash 卡 5.02 元（命中项 15×）
--- PASS: TestUnpricedModelNamesStayUnpriced         # 拼写错名/未知名不猜价
```

**golden 未动**（约束 4）：`tests/fixtures/log-stats-raw/golden/stats-cost.txt` 与 `stats-bill.txt`
**零 diff** —— 旧名 + `<9/10 12:00` 时段解析到**同一张旧卡、同一个标签**，货币数字逐字节不变
（fixture 会话 `fc426fed` 时间是 `2026-09-04 20:40`，属旧卡窗口）。

---

## 六、未做 / 不确定性（诚实清单）

1. **会话跨截止点无法拆分（No-Join）**：`resolve()` 对**整场会话只解析一次**（单一 `at` =
   `session_start`），因此跨越 9/10 12:00 或 9/14 12:00 的会话**整场按一个 regime 计价**。
   要逐请求正确计价需要按每轮时间戳逐轮解析 = **真重构**，超出"数据修复"范围（约束 1）。
   *影响面*：跨截止点的会话很少（9/10、9/14 前后各一两天），但**确实存在**。
   建议后续单独立项（`gopilog.BuildStatsFromUsage` 已有逐轮记录，具备条件）。
2. **周末高峰未判定**：官方写"周一至周五"，现有 `inPeakHours()` 只看小时、不看星期。
   **本次未改**（属逻辑改动，且 9/10-9/11 恰为周四/周五）。周末会话的命中/输入/输出会
   被按高峰价高估 2×——已有的老问题，非本次引入。
3. **旧名截止时刻取 9/10 12:00 而非 9/9**：官方只给"新价自 2026-09-10 12:00 生效"，
   未给"实际切换发生的分钟"。若实际在 9/10 12:00 之前的某分钟切换，则该小窗口的会话会被
   按旧卡（略高 17%）计价。**保守取官方公布时刻**，不臆测。
4. **`deepseek-v4-flash-0731`** 是修订后缀名，按前缀匹配走旧名规则（官方未单独列价，
   若它其实按另一张卡计费，我们的数字会偏——**无官方依据可查，按同族处理**）。
5. **`deepseek-4-flash`**（真实会话里出现过 4 次，明显是 `deepseek-v4-flash` 的笔误）
   **仍然不可定价**——不猜价（Rule：宁可说不知道也不编数字）。若需要，应单独确认。
6. **余额对账未做**：`scripts/ds_balance_probe.py` 可查余额（2 位小数），本次未用；
   若要以"实测扣费"锚定 0.02/1/4，需批次级对账（余额精度不足以支撑单会话）。
7. **未改** `internal/gopilog`、`cmd/agent`、任何 golden、任何 CLI 文案 —— 唯一改动文件为
   `internal/pricing/pricing.go` + 新增测试文件（`git show --stat` 可证）。

---

## 七、交付

| 项 | 位置 |
|---|---|
| 价格表修复 | `internal/pricing/pricing.go`（commit `b0df0ce`） |
| 回归测试 | `internal/pricing/deepseek_v41_test.go`（commit `0dd3784`） |
| 本报告 | `devdoc/deepseek-v41-flash-pricing-table-repair-20260911.md` |

**分支**：`feat/pricing-deepseek-flash`（worktree `~/workspace/gopi-pricing`，worktree 共享 refs，
已可直接合并）。**未合并 master**（Rule 6e）。

**复现命令**：
```bash
git show feat/pricing-deepseek-flash:devdoc/deepseek-v41-flash-pricing-table-repair-20260911.md
cd ~/workspace/gopi-pricing && go test -count=1 ./internal/pricing/ ./internal/gopilog/
CGO_ENABLED=0 go build -o /tmp/gopi-pricing ./cmd/agent/ && /tmp/gopi-pricing stats 055d468f-d565-4050-b819-b4a354c5bc37 --cost
```

---

## 附录 A：本次任务暴露的 `read` 工具缺陷（超出本 epic 范围，已另行报单）

验收过程中 `read` 对本报告文件报 `is a binary file（检测到无效 UTF-8）`，但该文件是**合法 UTF-8**：
`file` 报 `Unicode text, UTF-8 text`，`iconv` 无错，零 NUL 字节，Python `decode('utf-8')` 成功。
根因（代码级确认，非猜测）：`internal/tools/read.go` 的 `checkBinary()` 对**原始 8192 字节采样**
直接调 `utf8.Valid()`，而 8192 这个字节偏移**不落在 rune 边界上** —— 本文件第 8192 字节
（`0xe5`）正是一个 3 字节 CJK 字符的首字节，采样在字符中间被截断 ⇒ 合法前缀被判"无效"。

CJK 每字 3 字节 ⇒ 约 1/3 的中文文档（长度 mod 3 ≠ 0）必然命中；纯 ASCII 文件（1 字节/字符）
**永不命中** ⇒ 该缺陷系统性偏向中文文档，即 gopi 的主要文档形态。
同类现象此前已在 `devdoc/code-review-2026-08-28.md:190`、`reports/archive/2026-08/PYTHON-KERNEL-RESEARCH.md:169`
被记录过（当时 agent 被迫退回 `bash sed` 读文件），但从未修复、也无测试覆盖 CJK 文件。

**本次未修**：约束 1 限定本 epic 唯一改动为 DS 价格表数据，修 read 属逻辑改动。
已按流程单独报 bug 单（含最小复现 + 字节序列 + 修复方向 + 验收），交由独立任务处理。
