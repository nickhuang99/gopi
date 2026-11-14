<!-- doc-class: normative -->
# /text2img 的产物现在随命令进入对话（R7：命令帧携带图片）

**症状（本次修的那条线）**：在 `gopi web` 里敲 `/text2img GOPI-P1`，页面渲染出
✅ 一行摘要 + 一个磁盘路径，然后**什么也没有发生**。下一轮模型收到的是纯文本，
它打不开那个路径；操作员看着缩略图区空白，只能自己去文件系统里把图找出来再
📎 手动上传一遍。也就是说：命令"渲染了"、页面"显示了路径"，而**对话里没有任何
一个环节真的把图交给了模型**。

## 为什么之前是这样（不是遗漏，是断在两处）

两道断口，一处一处接：

1. **命令注册表的签名 `(string, error)`**（`cmd/agent/commands.go`）。命令只能
   交回一段人类可读的文字。`runSlashText2img` 因此把 `tools.Text2Img` 的
   `Result.Outputs`（引擎本来就有每页路径）**丢了**，只留下摘要文字。
2. **`event: command` 帧的字段**（`cmd/agent/cli_web.go` 的 `serveSlashCommand`）。
   帧里只有 `{command, output}`，没有地方放图片；页面那边自然也无从 staging。

于是 R4（命令暴露）与 R5（图片上传）各自都是通的，**中间那一跳从来没接**：
R5 的 `images[]` / `renderStrip()` / `/api/chat` 管线全都在，只是没人往里塞东西。

## 怎么接的（三处，都很小）

- **`CommandContext{ Artifacts *CommandArtifacts }`** —— 给载体一个**产物收集口**，
  而不是把注册表签名加宽成 `(string, []string, error)`（那会让表里每个命令都背一个
  只有 text2img 用得上的返回值）。`Artifacts.PNGs` 按渲染顺序收路径，`addPNG` 的
  nil 接收者是 no-op —— **终端 REPL 传 nil，行为一个字没变**。
- **`webSlashCommandImages(paths)`** —— 把路径读成 `webChatImage{media_type,data_b64}`，
  也就是 `/api/shot` 返回的那个对象形状。三种拒绝各自点名文件：读不出来 / 超
  `webMaxChatImageBytes`（32 MiB，和 `/api/chat` 同一个上限）/ 不是 PNG。
  检查复用 `validateWebImage`，**没有再写第二份契约**。
- **页面 `attachCommandImages()`** —— 帧里的图进**已有的** `images[]`，走**已有的**
  `renderStrip()`，下一轮走**已有的** `/api/chat` 请求体。**没有第二条上传通路**，
  这是这个改法能小、能验证的原因：它复用的是 R5 的全部管线。

## 三个刻意的选择（每个都有反例子）

1. **产物走"收集口"而不是改返回值。** 改返回值 = 注册表里每个命令都被迫携带一个它
   不用的字段，help 表、别名表、所有既有测试全跟着动。
2. **读不回来就报错，不打印路径。** 渲染成功但图片读不回来时，帧变成 `error` 并说
   "图片已在磁盘上，但没有随命令进入对话"。把路径打印得像"已发送"是这个 bug 的
   原始形态（Rule 15：静默失败比报错贵）。
3. **页数超限要说出掉了几页。** 单轮上限 `webMaxImagesPerRequest=8`；一个 10 页的
   渲染必须说"2 页未附上"，否则"画了 5 页"看起来像"画了 3 页"——静默截断就是同一个
   bug 换了个地方住。页面这一侧同样拒绝静默丢（`attachCommandImages` 撞到
   `MAX_IMAGES` 时补一条错误节点）。

## 验证（都是实测，不是推断）

- `go test ./cmd/agent/ -run 'TestText2ImgCommand|TestPageCommandImages'` → ok
  - 帧形状：`frames 的 images` 里的字节**逐字节等于**渲染器写出的文件；
  - 端到端：把帧里的图按页面做法回传，**捕获到的模型请求体**里是
    `{"type":"image","source":{"type":"file","file_id":"file-api-…"}}`，
    且**不含内联 base64**，并带 `anthropic-beta` 头；
  - 三条拒绝各自命中（不可读 / 超限 / 非 PNG），且都点名那个文件；
  - 超页数：附满 8 页并给出"2 页未附上"的说明。
- 页面场景（node 跑的是 `cli_web.go` 里**页面自己的 JS**，不是 Go 复刻）：
  `node cmd/agent/cli_web_page_harness.js --only "R7 command images" --page cmd/agent/cli_web.go`
  → `PASS: 16/16 checks`。
- 真二进制 + 真 HTTP：`./bin/gopi web --addr 127.0.0.1:8155`，`curl POST /api/chat
  -d '{"message":"/text2img --out /tmp/gopi-e2e-slash.png GOPI-E2E-R7"}'` →
  命令帧带 `images[0].media_type=image/png`；把这一张回传后**真模型回答**了图上的字
  （`GOPI-E2E-R7`），而 session raw.log 里内联 base64 计数为 `0`。
  完整记录：`~/.gopi/sessions/01a1b818-…/e2e/E2E-EVIDENCE.md`。

## 留给后来的人

- **`Artifacts.PNGs` 是通用的**（路径列表），但目前只有 `/text2img` 往里写。
  别的命令有真正的"文件产物"时再加，别为了对称乱塞。
- **终端 REPL 仍然只打印路径**，这是设计：它没有图片载体，nil sink 就是这个意思。
- **`/api/shot`（整页截图）与命令产物是两条路**，共用的是 `webChatImage` 对象和
  `validateWebImage`。要合并它们得先回答"截图是不是也算命令产物"，今天不需要。
