<!-- doc-class: normative -->
# gopi shot 设计 — 裸 CDP 截图子命令

## 目标

`gopi shot` 对 gopi 自身 web chat UI（cmd/agent/cli_web.go 内嵌 SPA）或任意 URL 截图，
产出 PNG，供 vision 调试闭环：shot → vision_analyze → UI bug 修复。
截图描述远优于文字描述（P1 已有图片上传+vision 读图管线，本单补"产生截图"能力）。

## 实现路线论证（三选一）

| 路线 | 依赖 | 论证 |
|------|------|------|
| a) chromedp/rod (Go CDP 库) | go.mod 新依赖 | 违反 ZERO-DEP 默认（Rule 10.7）；gopi 主仓至今 stdlib-only。**弃** |
| b) 调 playwright headless_shell 二进制 | npm 缓存结构知识 + node | 1228 缓存布局是 chrome-linux64/chrome（不是 headless_shell），绕开 playwright 协议层意义不大——playwright-core 本身就是 CDP 客户端，直调它=把 node 变成必须依赖。**弃** |
| c) 纯 Go 裸 CDP 协议 | **零依赖** | CDP = HTTP /json/list 拿 ws url + WebSocket JSON 消息。WebSocket 握手/帧编码 stdlib 可写（crypto/sha1 + encoding/binary），probe 已验证可行。**选 c** |

### Probe 实证（2026-08-31, HP, chromium-1228 = Chrome for Testing 149.0.7827.55）

`/tmp/cdp_probe.sh` 手工实现：
1. `chrome --headless=new --no-sandbox --remote-debugging-port=0 --user-data-dir=TMP`
2. stdout 解析 `ws://127.0.0.1:PORT/devtools/browser/UUID`
3. python 裸 socket WebSocket：握手（Sec-WebSocket-Key/Accept SHA1）→ 帧（FIN|opcode 0x1, MASK 位, varint 长度）→ JSON
4. `Page.enable` → `Emulation.setDeviceMetricsOverride 1440x900` → `Page.navigate data:URL` → sleep → `Page.captureScreenshot {"format":"png"}`
5. 产出 1440x900 PNG 17.6KB，`file` 确认 PNG，vision_analyze 读出 "GOPI-CDP-PROBE-OK" ✓

坑（probe 中踩掉）：
- chrome 启动到日志出现 ws url 需轮询（首次 probe sleep 不足导致空日志）。
- WebSocket 客户端→服务端帧**必须带 MASK 位**（服务器拒绝无掩码帧）；服务端→客户端不带。
- `--remote-debugging-port=0` 端口写 stdout `DevTools listening on ws://...`；node 版监听 /json/list 时要等 HTTP ready。

## 架构

```
cmd/agent/cli_shot.go          CLI 解析+分发（照 cli_text2img.go 模式：isShotSubcommand → runShot）
internal/shot/shot.go          引擎：Capture(opts) (pngBytes, error)
internal/shot/chrome.go        chrome 二进制定位（探测链）+ 进程生命周期
internal/shot/cdp.go           裸 CDP：ws 拨号、握手、帧收发、JSON cmd/resp 关联
internal/shot/shot_test.go     单测（帧编码/解析、flags 解析、chrome 探测链降级）
```

CLI 形态（对齐 Epic）：

```
gopi shot [--url URL | --self] [--out PATH] [--viewport WxH] [--wait N | --wait-selector CSS]
          [--addr HOST:PORT]  # --self 时连已在跑的 gopi web 实例；缺省起临时 server
```

- `--url`：任意外部页面（CNY 截图配方复用——--wait 等 JS 渲染）。
- `--self`：截图 gopi web chat UI。两段式：若 --addr 可连通直接用；否则起临时
  `gopi web` 子进程（GOPI_WEB_ADDR 挑空闲端口），等 / 可达后截图，defer kill。
  临时 server 模式下 agent 构造失败也无妨——截图只需要 GET / 返回 HTML（handleRoot
  不依赖 agent 路径）；但 gopi web 的 runWeb 在 newAgent 失败时退出。规避：--self
  临时 server 直接用 net/http 起**仅静态页**的 server（embed 同一 webChatPage），
  完全绕开 agent/session 机器——截图只需页面渲染，不需要能 chat。
- `--wait N`：navigate 后 sleep N 秒（SPA 渲染等待，默认 shotWaitDefault）。
- `--wait-selector CSS`：轮询 Runtime.evaluate `document.querySelector(CSS) !== null`，
  超时（shotWaitTimeout）报错。
- viewport：默认 1440x2800（Epic 建议，chat UI 长页）；Emulation.setDeviceMetricsOverride。
- 输出：默认 `shot-<timestamp>.png`（cwd）；打印绝对路径供下一步 vision_analyze。

## chrome 定位链（降级顺序）

1. `GOPI_SHOT_CHROME` env（显式指定）
2. `~/.cache/ms-playwright/chromium-*/chrome-linux*/chrome`（glob 取最高版本号——HP 1228 实测布局）
3. `which chromium-browser / chromium / google-chrome / google-chrome-stable`（PATH 查找；peerbox snap 坏时 snap 版会失败，靠后续 CDP 握手报错兜底，错误信息提示设 GOPI_SHOT_CHROME）
4. 找不到 → 报错并列出尝试过的路径 + 提示 `npx playwright install chromium` 或设 GOPI_SHOT_CHROME

## 与 vision 管线闭环

shot 输出 PNG → `gopi vision_analyze <png>` 或 chat 里 positional 图片直接喂。
大图（10000px+ 长页）已知 token 截断坑——shot 默认 viewport 1440x2800 避开；
需要长页时用户自定 viewport 并按 #1136 配方裁条带（future：--fullpage）。

## 远程节点（--node hp 等）

本单**不做**（HP 即本机验证环境；peerbox 跑 gopi shot 时走本仓同代码）。CNY 任务
模式的 ssh+scp 封装留 TODO，接口已预留（Capture 返回字节，远程传输是 CLI 层事）。

## 测试策略

- cdp 帧编码/解析：单测（无 chrome）。
- flags 解析：单测（照 parseText2imgArgs 模式）。
- chrome 探测链：env 注入假路径单测。
- 端到端：CI 环境无 chrome 则 skip（`testing.Short()` 或 chrome 缺失 skip）；HP 手动
  `gopi shot --self` 真跑验证（PNG magic bytes + file + vision_analyze）。
- 回归：usage_test 补 `gopi shot` 广告行（discoverability 硬门）。
