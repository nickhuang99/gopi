<!-- doc-class: historical -->
# Task Report: gopi shot — 截图子命令（raw CDP, 零依赖）

## Completed

- **`gopi shot` 子命令**（commit `032e9f1`，branch `feat/gopi-shot`）：
  - `gopi shot --url URL` — 任意外部页面截图
  - `gopi shot --self` — 截 gopi 自身 web chat UI（内嵌 webChatPage SPA）
  - `--out PATH`（默认 shot-<unix>.png）、`--viewport WxH`（默认 1440x2800）、
    `--wait N`（SPA 渲染等待）、`--wait-selector CSS`（轮询 DOM 元素）、
    `--addr HOST:PORT`（--self 连已在跑的 gopi web 实例）
- **实现路线 c（设计论证后选定）**：纯 Go 裸 CDP 客户端——crypto/sha1 WebSocket
  握手 + RFC6455 掩码帧 + JSON id 匹配调用，零 go.mod 依赖（chromedp/rod 弃：
  违反 ZERO-DEP；playwright 二进制直调弃：仍需 node）。probe 先行实证可行。
- **internal/shot 包**：cdp.go（ws 拨号/帧/调用 + /json/list 发现）、
  chrome.go（探测链：GOPI_SHOT_CHROME > playwright 缓存 chromium-*/chrome-linux*/
  chrome 数字排序取新 > PATH 四名 > 报错带修复提示）、shot.go（Capture 引擎）。
- **--self 架构**：临时起仅静态页 server（同一 webChatPage 常量经
  shot.SelfPageProvider 注入，internal 不 import main）——截图只需页面渲染，
  绕开 gopi web 的 agent/session 机器（newAgent 失败也不影响）。
- **CLI 分发**：main.go isShotSubcommand 挂入；help.go 顶层 usage 广告行；
  discoverability 测试（TestUsageAdvertisesShot）+ usage 默认值守卫 + 9 例
  flags 解析表驱动测试 + fake-CDP 端到端管线测试（含非 PNG 拒绝、大帧 64bit
  长度路径、帧 roundtrip）。
- **E2E 闭环验证（HP 真跑）**：
  - `gopi shot --self --out /tmp/gopi-self-shot.png --wait 3` → 1440x2800 PNG
    33990 bytes（file 确认非 PSD）→ `vision_analyze` 正确读出 UI 全布局
    （顶栏 "gopi web · pure-Go browser chat · SSE streaming"、📎 附件、输入框
    placeholder、蓝色 Send 按钮）✓
  - `gopi shot --wait-selector "textarea"` → PNG ✓
  - data: URL 截图 ✓（修了一个 bare-hostname 判断把 data:URL 误加 http:// 的 bug）

## Not Done

- **--node hp 远程节点截图**（ssh+scp 封装）：本机即 HP 验证环境，未做；Capture
  返回字节，CLI 层可后补。
- **--fullpage 长页截图**：默认 viewport 1440x2800 刻意避开 #1136 的 10000px+
  vision token 截断坑；fullpage + 裁条带配方留后。
- 内嵌 webChatPage 通过 Go string 常量跨包注入（SelfPageProvider var）而非
  embed——cli_web.go 的常量在 package main，internal/shot 不能 import main；
  若后续要彻底解耦可把 SPA 挪到 internal/webui 包。

## Verification

- Build: `CGO_ENABLED=0 go build ./...` OK
- Tests: `go test ./cmd/agent/ ./internal/shot/ ./internal/config/ ./testsuite/`
  全部 ok（internal/shot 1.017s 含端到端；cmd/agent 2.2s 含 3 个新测试组）。
  `go test ./...` 13 包 ok；4 个既有 FAIL（internal/agent x2、internal/tools x2）
  **stash 验证与本分支无关**（master 基线同样红，非回归）。
- Commit: `032e9f1 feat(shot): ...`（10 files, +1434）
- E2E: HP 真跑 --self → PNG → vision_analyze 读图验证（见上）。

## Key learnings (mid-task)

- chrome DevTools 端口行在 stderr：`DevTools listening on ws://127.0.0.1:PORT/...`；
  io.ReadCloser 无 deadline——Scanner goroutine + select timeout 才是正解
  （Buffered()==0 轮询对 os.Pipe 死等会饿死）。
- chrome 启动即打 vaInitialize 报错行（无害）——不要因 stderr 有内容就断定失败。
- memoryFlagSet 语义：裸 flag 会吞下一个非 `--` token 当值（`--self extra` →
  self="extra"）；布尔 flag 需显式检查值非空报"unexpected positional"。
