<!-- doc-class: normative -->
# GLM-5.3 价格表（2026-08-30 定稿版：原价 vs 实付价分离，SPA 截图+vision 实证）

> 溯源链：①2026-08-29 首版按 z.ai USD 换算（错——z.ai 是国际站独立定价）→ ②sogou 检索媒体转述拿 CNY（0.8/2.8/0.23，当时不知是原价还是实付价）→ ③**2026-08-30 HP playwright 截图 + gopi vision 读图 + DOM innerText 三重互证定稿**——0.8/2.8/0.23 是**划线原价**，五折窗口内实付 **0.4/1.4/0.115**。SPA 截图范式首战成功（pa-q-spa-screenshot-paradigm 落地实证）。

## GLM-5.3-Flash 价格表（bigmodel.cn，元/百万 tokens）

| 计费项 | 划线原价 | **五折实付（当前）** |
|--------|---------|-------------------|
| Input | 0.8 | **0.4** |
| Output | 2.8 | **1.4** |
| Cache hit | 0.23 | **0.115** |
| Cache storage | 限时免费 | 限时免费 |
| 高峰分时 | **无**（页面 DOM 全文 0 命中，Flash 低价靠直减非分时） | 同左 |
| 上下文 | 1M | 1M |
| 折扣徽章 | 「5折限时两周」（页面原文） | 至 2026-09-09 24:00 |

折扣算术自洽：0.8×0.5=0.4 ✓ 2.8×0.5=1.4 ✓ 0.23×0.5=0.115 ✓

其他活动：Batch API 五折、新品限时抢 4 折体验包。

## 真实成本对比（当前实付口径，元/百万 tokens）

| 模型 | Input | Cache hit | Output | 高峰 |
|------|-------|-----------|--------|------|
| glm-5.3-flash **实付** | **0.4** | **0.115** | **1.4** | 无 |
| deepseek-v4-flash（账单实证） | 1.5 | 0.05 | 4.5 | **9-12/14-18 翻倍**（3.0/9.0） |

**结论（经济性，五折窗口内）**：
- GLM vs DS 平价：input 省 73%、output 省 69%、**cache hit 仍贵 2.3×**（0.115 vs 0.05）
- GLM vs DS 高峰：input 省 87%、output 省 84%
- **9/9 五折结束后**：GLM 恢复原价 0.8/2.8/0.23 → 对 DS 平价（1.5/4.5）仍有优势（input 省 47%/output 省 38%），**策略翻转条件未触发**——GLM 在两个窗口期都是主力，DS 只剩"极重 cache 长会话闲时"niche（cache hit 0.05 vs 0.23 差 4.6×）
- 账单日核对：gopi 会话实测 cache 命中 90-98%，成本核算公式用**实付列**

## SPA 截图+vision 范式（本次实证，pa-q-spa-screenshot-paradigm 首战 ✅）

传统 curl/web_fetch/API 四路全堵的 JS SPA → **playwright 截图 + vision 读图**：
- HP playwright chromium-1228 缓存（~/.cache/ms-playwright，chrome-linux64/ 布局）headless 1440×2800 全页截图
- 1063KB PNG（1440×10171）——万级像素长图 gopi vision 直接读会截断 → **裁条带 + DOM innerText 互证**（三重验证：vision 读数=DOM 数字=peer1 参考价）
- 证据存档：devdoc/assets/bm-pricing-20260830.png + bm-pricing-cny-20260830.txt
- 配方在 HP memory #1136

## 工程修正记录

- **web_fetch geo 路由修复**（~/.gopi/plugins/websearch/scripts/web_fetch.py）：`--direct` 本地出口路由（CN 域直连 geo-correct，私网强制 ssh 侧 SSRF 门禁）——此前全走 remotebox 美国 IP 失真
- bigmodel.cn/pricing SPA 无 SSR（direct fetch chars 0-14）——截图范式补上这最后一块
- 抓取工具成本教训：SPA 反编译烧 token 无效——数据 API / 搜索引擎二手 / **截图+vision** 三条路按序试

## 落盘记录

- 截图证据：devdoc/assets/bm-pricing-20260830.png（HP 采集 scp 回）+ bm-pricing-cny-20260830.txt
- HP 采集 session：~/.gopi/sessions/6d57ea58…jsonl（HP 侧）
- DS 单价来源：~/work/claude/nick/deepseek_usage_amount.txt 账单 price 字段
- 采集任务：HP 台账 t-1788041789（11m9s completed）

## 附录：截图脚本全文（原始脚本，非重建）

**原件出处与溯源**（2026-09-19 从 HP 主机取回，verbatim）：

| 项 | 值 |
|---|---|
| 主机 / 路径 | HP `hostb-Laptop` (192.0.2.5) `:/tmp/bmshot/bm-shot.js` |
| 文件 mtime | 2026-08-30 06:18（与截图证据 `bm-pricing-20260830.png` 06:28 同批） |
| 大小 / sha256 | 2216 B / `f65283009f18d9eadb03c4e5185772f03e3d73689d24eb5be5ff981460bfc6bb` |
| 依赖 | `playwright-core ^1.62.1`（`/tmp/bmshot/package.json`，node_modules 就地安装，不下载浏览器） |
| 浏览器 | `~/.cache/ms-playwright/chromium-1228/chrome-linux64/chrome`（HP 缓存版，脚本自动探测） |
| 产出 | `/tmp/bm-pricing.png`（1440×10171，1062960 B） |
| 所属采集会话 | HP `~/.gopi/sessions/6d57ea58-aea6-47c0-8f3d-9dc30465ac7b.jsonl`（42018 B，2026-08-30 06:24） |

> 本附录是**原件全文**，不是事后重建。取回方式：`scp 192.0.2.5:/tmp/bmshot/bm-shot.js`（HP 的 `/tmp` 这次没被清掉——这正是当初要求留底的原因：`/tmp` 易失，脚本随时可能消失，所以正文必须有全文）。校验：附录代码块抽取后 `sha256sum` = `f6528300…fc6bb`，与 HP 侧原件逐字节一致。

```javascript
// bigmodel.cn/pricing screenshot — playwright-core + HP cached chromium (no browser download)
const { chromium } = require('playwright-core');
const fs = require('fs');
const os = require('os');
const path = require('path');

// --- named constants (no magic numbers) ---
const OUT_PATH = '/tmp/bm-pricing.png';
const URL = 'https://bigmodel.cn/pricing';
const VIEWPORT_W = 1440;
const VIEWPORT_H = 2800;
const NETWORK_IDLE_TIMEOUT_MS = 60000;
const EXTRA_SPA_WAIT_MS = 3000;      // ① networkidle + extra 3s
const RETRY_SPA_WAIT_MS = 10000;     // ② retry waits 10s
const MIN_OK_BYTES = 50 * 1024;      // <50KB => SPA shell, not rendered

function findChromium() {
  const base = path.join(os.homedir(), '.cache', 'ms-playwright');
  const dirs = fs.readdirSync(base).filter(d => /^chromium(-headless-shell)?-/.test(d)).sort().reverse();
  const subs = ['chrome-linux64/chrome', 'chrome-linux/chrome', 'chrome-linux/headless_shell'];
  for (const d of dirs) {
    for (const sub of subs) {
      const p = path.join(base, d, sub);
      if (fs.existsSync(p)) return p;
    }
  }
  throw new Error('no chromium found in ' + base + ': ' + fs.readdirSync(base).join(','));
}

async function shoot(page, waitMs) {
  await page.goto(URL, { waitUntil: 'networkidle', timeout: NETWORK_IDLE_TIMEOUT_MS });
  await page.waitForTimeout(waitMs);
  await page.screenshot({ path: OUT_PATH, fullPage: true });
  return fs.statSync(OUT_PATH).size;
}

(async () => {
  const exe = findChromium();
  const browser = await chromium.launch({ headless: true, executablePath: exe, args: ['--no-sandbox'] });
  const page = await browser.newPage({ viewport: { width: VIEWPORT_W, height: VIEWPORT_H } });
  let size = await shoot(page, EXTRA_SPA_WAIT_MS);
  if (size < MIN_OK_BYTES) {
    console.error('first shot ' + size + 'B < 50KB — retrying with 10s wait');
    size = await shoot(page, RETRY_SPA_WAIT_MS);
  }
  await browser.close();
  console.log('exe=' + exe);
  console.log('size=' + size + ' bytes');
  if (size < MIN_OK_BYTES) { console.error('STILL_TOO_SMALL (SPA shell)'); process.exit(2); }
  console.log('OK ' + OUT_PATH);
})().catch(e => { console.error('FATAL ' + (e && e.message || e)); process.exit(1); });
```

**运行方式（HP 侧）**：`cd /tmp/bmshot && node bm-shot.js`（`playwright-core` 装在 `/tmp/bmshot/node_modules`；从别处跑需 `NODE_PATH=/tmp/bmshot/node_modules`）。脚本自带 SPA 壳检测：产出 <50KB 判为未渲染壳，重试一次 10s 等待，仍小则 `exit 2`。

### 复现校验：2026-09-19 在 HP 重跑（同一脚本参数）

按本文件早前的约定"重跑输出需与 `bm-pricing-cny-20260830.txt` 对差后再采信"，2026-09-19 于 HP 原机重跑一次（单次无头页面加载，非压测）：

```
exe=/home/user/.cache/ms-playwright/chromium-1228/chrome-linux64/chrome
size=434027 bytes   sha256=b8c2eb48af6bc325dc6b4ce52480c287f385ccc43d41803da7b4cfb7e55fc5f3
dom_innertext_bytes=5469   dom_sha256=1f17c5995fb7b40084f1b4605b02908f5216868e4b64e1d0ba762463799417a5
NODE_RC=0
```

**价格对差（DOM innerText，全部逐字来自页面）**：

| 项 | 2026-08-30 存档 (`bm-pricing-cny-20260830.txt`) | 2026-09-19 重跑 | 一致 |
|---|---|---|---|
| GLM-5.3-Flash 输入 | 划线原价 0.8 | 卡片 `输入单价 0.8元 / M` | ✓ |
| GLM-5.3-Flash 输出 | 划线原价 2.8 | 卡片 `输出单价 2.8元 / M` | ✓ |
| GLM-5.3-Flash 缓存命中 | 划线原价 0.23 | 卡片 `缓存命中 0.23元 / M` | ✓ |
| 五折实付 | 0.4 / 1.4 / 0.115 | FAQ 原文 `限时五折输入 0.4 元、缓存命中 0.115 元、输出 1.4 元；标准价输入 0.8 元、缓存命中 0.23 元、输出 2.8 元` | ✓ |
| 上下文 / 缓存存储 | 1M / 限时免费 | 1M / 限时免费 | ✓ |
| 分时计价 | 高峰/低峰/分时 0 命中 | 仍 0 命中 | ✓ |
| 同表旗舰价 | GLM-5.3 8/28/2，GLM-5.2 8/28/2 | 同（8/28/2） | ✓ |

**结论：价格零差异**，2026-08-30 存档表格在当前页面仍逐项成立，无需修订。

**一处需要修订的是本文件的时效性表述**（不是价格）：正文"折扣徽章 …至 2026-09-09 24:00"以及"9/9 五折结束后 GLM 恢复原价"——**截至 2026-09-19，五折窗口仍在生效**（重跑页面 FAQ 原文仍是"限时五折 …输入 0.4 元"），故"策略翻转条件"至今仍未触发，正文该判断继续有效、只是结束日期需按实况更新。

**产出差异说明**：重跑 PNG 434027 B（2026-08-30 为 1062960 B）。尺寸/字节差异来自渲染细节（字体缓存、懒加载图片是否就位），**不影响价格读数**——本次判定以 DOM innerText 逐字比对为准，PNG 仅作旁证。重跑脚本（含 innerText 落盘与 sha256）与产物在 HP `/tmp/bm-shot-verify.js`、`/tmp/bm-pricing-verify.png/.txt`，未纳入本仓库。
