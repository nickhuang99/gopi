<!-- doc-class: normative -->
# `gopi web` 的「📷 截当前页」— 服务端自截页面并作为附件

任务行 `01a16d28-38f5-78b6-9279-f9bdca21107c`。本文说明这个入口**为什么长成这样**，
以及三条以后动它之前必须知道的边界。

## 1. 它解决什么

在此之前，想把「界面现在这个样子」交给模型，只能三步绕路：切到终端跑
`gopi tool shot --self`（或 `screen_capture`）→ 得到 PNG 文件 → 切回浏览器点 `📎`
选文件。而 `📎` 本身只会选**已有文件**，不会生成任何东西。

现在页面输入框旁多了一个 `📷`：点击 → 服务端用 `internal/shot` 的 web surface
抓**这个实例自己的页面** → PNG 不落盘、base64 直接回来 → 进 `images[]` →
`renderStrip()` 画出缩略图 → Enter 就是一轮普通的 vision turn。

链路没有新机制：`images[]` → `renderStrip()` → `/api/chat` 的 `images` 字段是
R5 就有的上传通道，`📷` 只是换了一个**图片来源**。

## 2. 三条边界（动它之前先读）

### 2.1 只截页面 —— 是代码形状，不是开关

`cmd/agent/cli_web_shot.go` 里的捕获选项是常量：

```go
png, err := webShotRun(ctx, shot.Options{
    Self:      true,
    Addr:      addr,              // 本进程自己的 loopback 监听地址
    Surface:   shot.SurfaceWeb,   // 常量
    ViewportW: vp.W,              // 请求里唯一的旋钮，已钳制（§2.2）
    ViewportH: vp.H,
})
```

`internal/shot` 仍然认识终端（chat）和显示器（screen）两个 surface，它们在这个
端点**根本不可达**：没有分支、没有 fallback、没有 `--surface` 字符串经过子进程，
而且该文件里连它们的**名字**都不许出现 ——
`TestWebShotNeverNamesANonPageSurface` 扫全文（含注释）禁止
`SurfaceScreen` / `SurfaceChat` / `surface screen` / `surface chat` /
`--surface screen` / `--surface chat` / `screen_capture` / `ScreenCapture`，
并反向前置断言文件必须出现 `shot.SurfaceWeb`（否则说明扫错了文件，绿也是假的）。
把 surface 改成 `SurfaceScreen` 实测该测试与行为测试同时红。

### 2.2 不接受客户端目标 —— 请求类型只有一个字段，而且它不改变目标

```go
type webShotRequest struct {
    Viewport *webShotViewport `json:"viewport"`   // {"w":N,"h":N} —— 唯一的旋钮
}
```

**这个字段只改几何，不改目标。** 它经 `clampViewport` 落成 `shot.Options` 的
`ViewportW/H`，而 `Self` / `Surface` / `Addr` 都是常量或本进程自证的监听地址 ——
没有任何路径能让它影响「截哪个页面」「截哪个 surface」「连哪台机器」。
所以 HTTP 层仍然主动拒绝 `viewport` 以外的**任何**字段并点名 offending 字段
（`{"url":…}` → 400 `refusing url`，`{"surface":"screen"}` → 400 `refusing surface`），
因为「字段被静默忽略」会让调用方以为抓取去了它指定的地方 —— 那是 Rule 15 的静默失败，
只是换了个位置。

边界与回落（全部是命名常量，Rule 1）：

| 输入 | 结果 |
|---|---|
| 无 body / `{}` / `{"viewport":{}}` | 默认 `shot.DefaultViewportWidth/Height`（1440×2800，老调用方行为不变） |
| `w` 或 `h` 缺省或 ≤ 0 | 该维度用默认 |
| `w`/`h` ∈ [200, 4000] | 原样传给 `shot.Options` |
| `w`/`h` < 200 或 > 4000 | **钳制**到 `webShotMinViewport`/`webShotMaxViewport`，并把 `capture.clamped=true` + `asked_w/asked_h` 回写进响应，同时往 stderr 打一行 |
| `"viewport":"900x900"` / 小数 / 数组 | **400 拒绝并点名** `viewport`（不静默回落 —— 调用方明明给了几何，必须知道自己没被听懂） |
| 任何其他键 | 400 拒绝并点名该键 |

`TestWebShotRequestTakesOnlyTheViewport` 是**改写后**的形状门（原名
`TestWebShotEndpointTakesNoCaptureOption`，它断言字段体为空 —— 那是「没有旋钮」时代的
形状）。改写不是关掉一条门，而是把断言从「空」换成「**恰好这一个**」：
`webShotRequest` 的字段体必须逐字等于 `Viewport *webShotViewport` 加它的 json tag，
且 `webShotViewport` 的字段体必须恰好是 `W`/`H`。有人加 `URL`、`Addr`、`Surface`，
或者往几何对象里塞第三个成员，它一样红；行为面（`{"url":…}`/`{"surface":…}` 仍 400
且点名）由 `TestWebShotRefusesAClientSuppliedTarget` 钉住。

目标侧的自证是**一次真实 dial**，判据是 **custody（这个进程持有那个套接字）**，不是
「必须是 loopback」这个**更窄**的陈述：`webShotSelfAddr(s.addr)` 只把通配绑定
（`0.0.0.0` / `::` / 空 host）归一到 `127.0.0.1`；一个具体的 host 原样交给
`webShotIsOwnListener`，它先要求该 host 是**本机地址**（`webHostIsLocal`：loopback
或本机某个网卡地址），再真连一次并要求 `conn.LocalAddr()` **等于**目标 host:port ——
内核自己回答「这个连接没有离开本机、监听就在 bind 放的位置」。于是**绑在 LAN IP、
绑在具体地址的实例都能截自己的页面**，而一台远程主机、或者一个探测目标在 dial 之前
就被拒，**浏览器一步都不启动**（否则拒绝本身就是一个可用来探测主机的 oracle）。
（这段措辞曾经写成「非 loopback → 拒绝」，与 `282dfad3` 之后的代码不符，本行按实测改正。）

### 2.3 门是同一扇门

`/api/shot` 注册在 `webChatHandler()` 的同一个 mux 上，所以 `runWeb` 已有的
`webTokenGate`（非 loopback 绑定时装上的那个）**自动**覆盖它 —— 不是复刻一份校验，
是走同一条装配线。`TestWebShotRefusesWithoutTheToken` 双向钉住：无 token → 401 且 0 次
capture；带 `Authorization: Bearer` → 200 且 1 次 capture（门是门，不是墙）。

## 3. 实测的 Chromium 路径（别在别的环境里假设）

`gopi web` 通常起在 gopi 的 bash 子进程里，于是带着 scratch 的地址空间钳制：

```
$ grep 'Max address space' /proc/<web-pid>/limits
Max address space  12884901888  12884901888  bytes      ← 12 GiB，soft == hard
```

`internal/shot` 的 `addressSpaceClamped()` 因此走 `launchChromeUserUnit`，
实测抓到瞬时用户单元：

```
$ systemctl --user list-units --all --no-legend 'gopi*'
gopi-shot-<pid>-<suffix>.service loaded active running /usr/bin/chromium-browser --headless=new …
```

要点：走的是 **service 单元**（`systemd-run --user`），不是 `--scope` —— scope 会**继承**
调用方的进程限制，看起来像修好其实没有；退出即收（`--collect`），没有常驻进程。
没有 user manager 时 `internal/shot` 会在 stderr 明说降级并退回 attached 启动。

## 4. 一次 capture 的成本 = 一次浏览器启动

所以页面侧：抓取期间按钮 `disabled` + `.busy`（秒级操作必须可见）；已经到
`MAX_IMAGES` 上限时**本地直接拒绝**，不发请求（不花掉一次启动）；失败把服务端**原文**
渲染在 strip 里（`.shoterr`），不做静默的 no-op，也不弹 alert 打断页面。

## 5. 验证入口

```bash
go test ./cmd/agent/ -run 'TestWebShot' -count=1        # 端点：行为 + 隐私 + SSRF + 门 + 失败
node cmd/agent/cli_web_page_harness.js | tail -1        # 页面：驱动真实页面，含 R6 三场景
```

真跑（Rule 31）的完整记录在任务报告里：起 `gopi web --addr 127.0.0.1:8097 --tools` →
`POST /api/shot` → `HTTP 200`、46151 字节 PNG / 1440×2800 → `vision_analyze` 判读为页面、
无桌面元素 → 把该图塞进 `/api/chat` 的 `images[]` → SSE `delta`…`done`，模型只看图就答对。

## 6. 端到端取证：按钮那一下，字节没被改过（行 `01a1b337`，2026-11-07）

上面 §5 记录的是**服务端两跳**；这一节把**页面自己的那条 JS 路径**也真跑了，并把
「缩略图里那张图」与「模型收到的那张图」按字节对齐。

驱动方式：`jsdom` 提供 DOM（页面脚本是服务端**真实下发**的那份 HTML，不是复刻），
`fetch` 走**真网络**打真实例。这一点必须写清楚 —— **jsdom 没有布局引擎，CSS 几何不是
真的**，所以这条通道**只能**用来判「点击 → 请求 → 暂存 → 提交」这条路走不走通，
**不能**用来判布局。

```
[net] POST http://127.0.0.1:8091/api/shot
[A] thumbnails after the click = 1 (shoterr=0)
[net] POST http://127.0.0.1:8091/api/chat
[B] /api/shot returned  b64len=199392 sha256(png)=62c47429c79d595794ff90db0dfc98e2108019876309e1c45ecc67a4ef3f14af
[C] /api/chat received  b64len=199392 sha256(png)=62c47429c79d595794ff90db0dfc98e2108019876309e1c45ecc67a4ef3f14af
[D] media_type=image/png images=1
[E] IDENTICAL_BYTES=true
```

`[E]` 是这条链的验收条件：**按钮拿到的字节就是模型收到的字节**，中间没有重新编码、
没有裁剪、没有转格式。配上 `/api/chat` 的 done 帧逐字答出页面顶部那行标题，vision 腿
（`{"type":"file","file_id":…}`，见 Files API 那一行）就是真的读到了图，而不是模型在猜。

失败分支同样真跑（只把 `/api/shot` 的请求体换成 `{"url":…}`，让**服务端真实的 400**
原样回来），实测结果与 §4 的承诺一致：

```
[A] thumbnails staged by the page BEFORE the failed capture = 1
[B] thumbnails AFTER the failed capture = 1
[C] .shoterr lines = 1
    shoterr[0] = "capture failed: HTTP 400 — /api/shot takes no parameter but \"viewport\" — refusing url; the capture target is always THIS server's own page"
[D] strip children = 2
```

即 `.shoterr` 只删失败行、**不动**已经暂存的缩略图（`e1c344e2`），一行红字带状态码 + 服务端原文。

## 7. 两个已知缺口（都不在本文件描述的「链路」里，但会让用户以为链路坏了）

1. ~~**视口是固定的 1440×2800，不是用户窗口**~~ —— **已修（任务行
   `01a1b3b8-5328-7044-be17-4530cb36c2a2`）**。`capturePage()` 现在把
   `window.innerWidth/innerHeight` 放进 `{"viewport":{"w":…,"h":…}}` 发给
   `/api/shot`（§2.2 是新的请求契约），所以图与用户窗口同几何。
   **`devicePixelRatio` 不参与**：CDP 以 `deviceScaleFactor: 1` 跑，页面送的是 CSS
   像素且**不缩放**，因此在 2x 屏上 PNG 的物理像素是窗口的一半 —— 匹配的是**布局**，
   也就是用户眼睛看到的排版。真跑实测（`curl` + 解 PNG IHDR）：

   ```
   无 body                        → 1440×2800（默认，老行为不变）
   {"viewport":{"w":900,"h":900}} → 900×900
   {"viewport":{"w":1440,"h":900}}→ 1440×900
   {"viewport":{"w":99999,"h":99999}} → 4000×4000（钳到 webShotMaxViewport）
   {"viewport":{"w":10,"h":10}}   → 200×200（钳到 webShotMinViewport）
   ```

   页面侧不是读代码得出的：用项目自己的 `cli_web_page_harness.js` 驱动**真实的**
   `capturePage()`，它实际交给 `fetch` 的 body 是
   `{"viewport":{"w":1280,"h":720}}`（harness 的窗口几何），改窗口后再点一次即送新值。
2. **Files API 的「同一张图只上传一次」只在进程内成立**。`webFilesCache` 是纯内存
   （缓存键 `mediaType+sha256(bytes)` 本身跨进程稳定，坏在它不落盘），而回收不实现
   （没有 `DeleteFile`/`MethodDelete`），对象永久有效、账号上限 25 GiB / 10000 个文件。
   实测：同一份 49835 字节 PNG，重启后重新上传拿到**新** `file_id`
   （`file-api-2fc2bed7-…` → `file-api-82d27704-…`）。任务行 `01a1b3b8-5afd-7904-a0f5-4eae6f099031`。
