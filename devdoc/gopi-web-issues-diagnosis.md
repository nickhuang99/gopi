<!-- doc-class: historical -->
# gopi web 的四个问题 — 取证结论（epic 01a1b331，2026-11-07）

来源是操作员在 `gopi chat` 会话 `116dcdec-3027-49b5-9a6c-b4025543cb58`
（`session.jsonl:25`）里的实测抱怨。**本文只记录跑过的命令和它的原始输出**；
推断一律标 `推断`，被推翻的结论保留在 §6（删掉错误结论比留下它更贵）。

## 0. 「没有这个功能」被推翻（先验前提）

```
$ ls cmd/agent/cli_web_shot.go cmd/agent/web_voice*.go
cmd/agent/cli_web_shot.go
cmd/agent/web_voice.go
cmd/agent/web_voice_live.go
cmd/agent/web_voice_live_test.go
cmd/agent/web_voice_test.go
$ wc -l cmd/agent/cli_web.go
3879 cmd/agent/cli_web.go
$ grep -n "/api/shot" cmd/agent/cli_web.go
934:	// The page-capture route (cli_web_shot.go): POST → this instance's own page
```

📷 按钮、`/api/shot`、🎤 按钮、两条语音通道（start/stop 与 live 流式）都在树里。
四个抱怨全是**行为**问题，不是缺失功能问题。

## 1. 📷「截当前页」——/api/shot 的失败分支**随 bind 形态变化**（已实测）

三个实例，同一台机、同一份 revision（`da668af`），只改 `--addr`：

### A) `--addr 127.0.0.1:8091` → 成功

```
$ curl -sS -m 100 -o shot-body.json -w 'STATUS=%{http_code} TIME=%{time_total}s SIZE=%{size_download}\n' \
    -X POST http://127.0.0.1:8091/api/shot
STATUS=200 TIME=1.433592s SIZE=66267
```

解码后：PNG `49660` 字节，`1440 x 2800`，magic `89504e470d0a1a0a`；
`b64/binary = 1.333`（这条正好实测了操作员说的「base64 增大 1.3 倍」）。
`vision_analyze` 读这张图，确认它就是 gopi web 页（标题 `gopi web`、三个按钮、
输入框 placeholder `Ask gopi anything… (/ for commands, Enter to send, Shift+Enter for newline)`、`SEND`，中间大片空白 = 无消息的空会话）。

### B) `--addr 192.168.7.12:8092`（`gopi web -h` 明确宣传的写法之一）→ **500，确定失败**

```
$ gopi web --addr 192.168.7.12:8092
gopi web: listening (Ctrl+C to stop)
  open: http://192.168.7.12:8092?t=<per-run token>

$ curl -sS -m 30 -X POST http://192.168.7.12:8092/api/shot --noproxy '*'
STATUS=500 TIME=0.000525
{"error":"/api/shot: refusing to capture 192.168.7.12:8092 — only a loopback address can be this instance's own page"}

$ curl -sS -m 5 http://127.0.0.1:8092/
curl: (7) Failed to connect to 127.0.0.1 port 8092: Connection refused
```

**这是 /api/shot 的一个真实、可复现、带 status 的失败分支**：实例只绑在 LAN 地址上时，
loopback 上没有监听，`webShotIsOwnListener` 直接按「非 loopback」拒绝，
`capturePage()` 把这句话渲染进 `#imgstrip` 的 `.shoterr`
（`cli_web.go:3486-3487`），所以页面上看到的是
`capture failed: HTTP 500 — /api/shot: refusing to capture 192.168.7.12:8092 — only a loopback address can be this instance's own page`。
而那个 bind 形态恰恰是 `gopi web -h` 教用户从别的设备打开页面的写法
（`cli_web.go:40-41`、`:181`）。**推断**（未实测）：操作员当时若用 LAN 地址打开页面，
📷 必然 500 —— 这与「按钮不工作」的描述吻合，但**他当时的具体 bind 未取证**，
所以这只是最可能的解释之一，不是定论。

### C) `--addr 0.0.0.0:8093`（wildcard）→ 成功

```
$ curl -sS -m 60 -w 'STATUS=%{http_code} TIME=%{time_total}s SIZE=%{size_download}\n' \
    -X POST http://127.0.0.1:8093/api/shot --noproxy '*'
STATUS=200 TIME=1.483289s SIZE=68147
$ curl -sS -m 60 -o /dev/null -w 'LAN-IP STATUS=%{http_code}\n' \
    -X POST http://192.168.7.12:8093/api/shot --noproxy '*'
LAN-IP STATUS=200
```

wildcard 被解析成 `127.0.0.1`（`webShotSelfAddr` 的 `webShotWildcardHost4` 分支），
所以 0.0.0.0 的 bind 是好的。**坏的只有「恰好绑在自己 LAN IP 上」这一格。**

### D) 独立复核：`gopi tool shot --self` 端到端

```
$ /usr/local/bin/gopi tool shot --self --out /tmp/probe-self.png; echo exit=$?
/tmp/probe-self.png
exit=0
$ file /tmp/probe-self.png
/tmp/probe-self.png: PNG image data, 1440 x 2800, 8-bit/color RGB, non-interlaced
```

## 2. base64 → deepseek Files API：**已经实现并已在本树**（不是待办）

```
$ grep -rn "file_id\|Files API" --include=*.go cmd/agent/ internal/ | head
cmd/agent/cli_web_files.go:3:// cli_web_files.go — the web carrier's half of the Files API switchover: a turn
cmd/agent/cli_web_files.go:4:// that carries pictures uploads each one ONCE to DeepSeek's Files API and sends
cmd/agent/cli_web_files_test.go:3:// cli_web_files_test.go — the Files API transport's three non-negotiable
（完整首行用 `git show HEAD:cmd/agent/cli_web_files_test.go | head -12` 取；本行刻意截断并给出取回命令）
$ git log --oneline --all --grep="file_id"
d6e575b4 feat(web): 📷 sends a Files API file_id instead of inline base64
$ git merge-base --is-ancestor d6e575b4 HEAD && echo yes
d6e575b4 IS ancestor of HEAD — Files API 已在这棵树上
$ ls -l cmd/agent/cli_web_files.go cmd/agent/cli_web_files_test.go
-rw-rw-r-- 1 nick nick 6379 Nov  7 07:35 cmd/agent/cli_web_files.go
-rw-rw-r-- 1 nick nick 9918 Nov  7 07:35 cmd/agent/cli_web_files_test.go
```

账本里也有这条链（实测，`gopi task search base64`）：

```
01a1b288-a959-779e-8584-005c696754d1  [failed]     web 的 📷 截屏按钮改走 DeepSeek Files API：上传一次取 file_id，请求只带引用不带 base64 内联
01a1b2a5-71fc-79e7-a834-15efe48a7ab6  [completed]  clean-room copy of 01a1b288-a959-779e-8584-005c696754d1（177 loop passes, exit=0）
```

且那个 run 自己的 `output.log` 结尾是通道实证：
`clone-channel: attempt 1/3: push accepted`。
**⇒ 子任务 2（file API）的前提是错的：它不是「未实现」，是「已实现、待核实线上是否真的走这条腿」。**
真正剩下的开放问题是操作员担心的那点：**上传一次的文件在 agent 侧是否真的被读成图片**
（`cli_web_files.go` 的注释说浏览器仍发 `{media_type,data_b64}`，替换点只在
「browser image → model message」那一处）。这条要靠一次真实带图对话取证，
不是读代码能定的。

## 3. 语音终止键：Esc 在录音态**没有任何**语义（已定位到行）

```
$ grep -n "KEY_ESCAPE" cmd/agent/cli_web.go
2683:const KEY_ESCAPE='Escape';
2689:const MENU_NAV_KEYS=new Set([KEY_ARROW_DOWN,KEY_ARROW_UP,KEY_TAB,KEY_ESCAPE,KEY_ENTER]);
3833:    if(e.key===KEY_ESCAPE){e.preventDefault();menuClose();return;}
3835:  if(e.key===KEY_ESCAPE&&busy){e.preventDefault();stopTurn();return;}
$ grep -c "Escape" cmd/agent/web_voice_live.go
0
```

Esc 只有两个消费者：关补全菜单（`:3833`）、**中止正在跑的回合**（`:3835`，`stopTurn`）。
录音中按 Esc：菜单没开、`busy` 为假（录音不是回合）⇒ 什么都不发生。
所以当前答案是**只有按钮**（`#mic`，`:3245-3248` 把 glyph 切成 `⏹`）。
这是**语义缺口而非 bug**：要不要给 Esc 补「取消本次录音（丢弃音频）」是产品决定，
而且必须与「按钮 = 停止并提交」分开（否则想撤销的人会误提交）。

## 4. 布局：`#input` 被 `#voicelive` 挤窄 —— 机制已定位，几何待测

```
$ grep -n "#form{\|#input{" cmd/agent/cli_web.go
2587:  #form{padding:14px 20px;border-top:1px solid #262b3a;display:flex;gap:10px;align-items:center;position:relative}
2603:  #input{flex:1;background:var(--panel);border:1px solid #2c3245;color:var(--fg);border-radius:10px;padding:11px 14px;font:inherit;resize:none}
$ sed -n '2581,2582p' cmd/agent/cli_web.go
  #voicelive{font-size:12px;color:#ffd479;white-space:nowrap;padding:0 4px}
  #voicelive[hidden]{display:none}
$ grep -n "voiceLiveEl" cmd/agent/cli_web.go
2650:const voiceLiveEl=document.getElementById('voicelive');
3305:  if(!voiceLiveEl)return;
3306:  if(!on){voiceLiveEl.hidden=true;voiceLiveEl.textContent='';return;}
3307:  voiceLiveEl.hidden=false;
3310:  const draw=()=>{voiceLiveEl.textContent='● recording '+name+' '+Math.floor((Date.now()-t0)/1000)+'s';};
```

`#form` 是 `display:flex`；`#voicelive` 是 `white-space:nowrap` 的**非 flex 子项**；
`#input` 是 `flex:1`。录音时 `micLiveState(true,device)` 取消 `hidden` 并写入
`● recording <设备名> <秒>s` —— 一段不换行的固定宽度文本，`flex:1` 的 textarea 只能让位。
**机制清楚了，「窄多少」必须量**：

```js
document.getElementById('input').getBoundingClientRect().width   // 录音前
// 点 🎤 -> 录音 -> 再读一次
```

本 run **没有**完成这一步：本机的 snap chromium 用 `--remote-debugging-port` 启动时
在 `--headless` 与 `--headless=new` 下**都核心转储**（见 §5），所以拿不到页面的
`getBoundingClientRect`。gopi 自己的 `internal/shot` 能截到图（走 `systemd-run --user`
+ RLIMIT 那条路，§1D），但它没有「执行任意 JS 并取回值」的 CLI 表面
（`gopi tool shot -h` 只有 `--viewport/--wait/--wait-selector/--url/--self`）。
→ 子任务 3 要补的正是这个测量，不是重读 CSS。

## 5. 被推翻的结论（保留，别重犯）

**本 run 第一版写的是：「本机没有可用的 headless Chromium，这是 /api/shot 的头号根因」。**
这是错的，而且是 Rule 41a 的典型翻车：我读了 `head -5 /usr/bin/chromium-browser`
看到 snap wrapper 脚本就下结论，**没有跑判定它的那条命令**。

```
$ snap list chromium
Name      Version        Rev   Tracking       Publisher    Notes
chromium  156.0.8078.79  3580  latest/stable  canonical**  -

$ timeout 20 /usr/bin/chromium-browser --headless --no-sandbox --disable-gpu \
    --remote-debugging-port=9333 --user-data-dir=<scratch> about:blank &
$ curl -s -m 1 http://127.0.0.1:9333/json/version
<DevTools 版本 JSON>   ← 第 1 秒就出来
```

⇒ snap chromium 是好的、CDP 可用，`gopi tool shot --self` 端到端成功（§1D）。
记忆 #3966 里「chromium 是坏 snap、DevTools 15s 不出来」是 **2026-09-19 在 peerbox 上**
的读数，套到本机是错的。

留下的真实异常（**本轮未解释，按 Rule 41 记为待查**）：
我手工用 `--remote-debugging-port=9350/9360` 起同一个 snap chromium，
`--headless` 与 `--headless=new` 两种拼写都 `timeout: the monitored command dumped core`，
且 `--user-data-dir` 指到 session 证据目录与 /tmp 下结果相同。
而 gopi 的 `internal/shot` 起的是**同一份二进制**却成功 —— 差别在
`systemd-run --user --scope` 与 RLIMIT_AS（`internal/shot/launch.go:208`）。
⇒ 两者不是同一条件，不能拿手工那一半当「chromium 坏了」的证据。

## 6. 为什么拆四条而不合并

四个抱怨落在四个互不相同的代码面，验法也不同：HTTP 端点（curl + status/body，
本轮已做）、provider wire 形状（已实现，需一次真实带图对话）、页面 JS/CSS 几何
（CDP `getBoundingClientRect`，本机缺 JS-eval 表面）、证据落盘路径审计。
每条都必须真跑一次（Rule 31），浏览器驱动的两条还依赖同一个缺失的测量表面。

## 7. 本轮落成的任务行（各自带证据）

| 行 id | 内容 | 证据来源 |
|---|---|---|
| `01a1b343-b6ef-71d6-b8e8-1527980b1f94` | **bug**：绑本机 LAN IP 时 `/api/shot` 必然 500（`only a loopback address can be this instance's own page`），而 `gopi web -h` 把这种 bind 与 `0.0.0.0` 并列宣传 | §1 B/C 的两条 curl 原文 |
| `01a1b345-7163-74b1-8fd2-3383b040ddec` | **bug**：📷 失败时 `strip.innerHTML=''` 把**用户已附的缩略图**一起抹掉，但 `images[]` 没清，下次 Enter 仍会发出去 | `cli_web.go:3474` 与 `:3176-3188`、`:3484-3488` |
| `01a1b337-b029-…`（原 roadmap 第 2 项，payload 已改写） | Files API **已实现**（`d6e575b4` 在树上），改为核实线上腿真跑 + 模型真读到图 | §2 |
| `01a1b337-b038-…`（原 roadmap 第 3 项，payload 已改写） | Esc 语义已定位（录音态无 Esc 分支），布局机制已定位；剩「在活页面跑 JS 取 `getBoundingClientRect`」这一测量表面 + 几何数字 | §3 §4 |

## 8. 本节要留给他人的两个环境事实

1. **本机 snap chromium 可用**（`snap list chromium` → 156.0.8078.79 rev 3580；
   DevTools 端口 1 秒内可应答），`gopi tool shot --self` 端到端成功。记忆 #3966 的
   「坏 snap」是 **peerbox** 的读数，别套到本机。
2. **但手工 `--remote-debugging-port` 启动会 core dump**（`--headless` 与
   `--headless=new` 都试过），而 `internal/shot` 走 `systemd-run --user --scope` + RLIMIT_AS
   的路径成功。**要驱动活页面就跑 `internal/shot` 的 launch 路径，不要手工起。**
   这条是本轮拿不到 `getBoundingClientRect` 的直接原因。

## 4b. 布局几何：已量到（机器可读，不经 OCR）

§4 当时缺「在活页面跑 JS」的表面。**解法不需要改产品代码，也不需要能用的 CDP 客户端**：
把**真实页面 HTML**（`curl http://127.0.0.1:8095/` 拿到，69870 字节 / 1357 行）复制一份，
注入一段只在测量变体里存在的脚本 —— 它复现 `micLiveState(true,device)` 的两个动作
（`hidden=false` + 写入 `● recording microphone 0s`），在**切换前后**各读一次
`getBoundingClientRect()`，然后 `new Image().src='/REPORT?...'` 把这些数交回服务端。
**数字由 `python3 -m http.server` 的访问日志逐字落下**，截图与 OCR 都不在证据链上。

命令（逐字，三个视口各一次）：

```
$ python3 -m http.server 8099 --bind 127.0.0.1 --directory <session证据目录> &
$ gopi tool shot --url http://127.0.0.1:8099/layout-report.html --wait 2 \
      --viewport 900x400 --out rep-w900.png
$ gopi tool shot --url http://127.0.0.1:8099/layout-report.html --wait 2 \
      --viewport 1200x400 --out rep-w1200.png
$ gopi tool shot --url http://127.0.0.1:8099/layout-report.html --wait 2 \
      --viewport 1600x400 --out rep-w1600.png
$ grep -o "GET /REPORT?[^ ]*" http8099.log
```

原始输出（服务端访问日志，逐字，**未改一个字符**）：

```
GET /REPORT?vw=900&vh=400&before_w=613.84&before_h=45.00&after_w=443.83&after_h=45.00&live_w=160.02&live_h=18.59&form_w=900.00&delta=170.02&pct=27.7&t=1794009331563
GET /REPORT?vw=1200&vh=400&before_w=913.84&before_h=45.00&after_w=743.83&after_h=45.00&live_w=160.02&live_h=18.59&form_w=1200.00&delta=170.02&pct=18.6&t=1794009334787
GET /REPORT?vw=1600&vh=400&before_w=1313.84&before_h=45.00&after_w=1143.83&after_h=45.00&live_w=160.02&live_h=18.59&form_w=1600.00&delta=170.02&t=1794009338095
```

**结论（三行数据指向同一件事）**：

| 视口 | `#input` 录音前 | 录音中 | 少了 | 占原宽 |
|---|---|---|---|---|
| 900 | 613.84 px | 443.83 px | **170.02 px** | 27.7% |
| 1200 | 913.84 px | 743.83 px | **170.02 px** | 18.6% |
| 1600 | 1313.84 px | 1143.83 px | **170.02 px** | 12.9% |

`delta` 三个视口**完全一样**（170.02 px），且 `170.02 = live_w 160.02 + #form 的 gap 10`
（`cli_web.go:2587` 的 `gap:10px`）—— 这不是相关，是**同一个数的分解**：
`#voicelive` 作为 `white-space:nowrap` 的非 flex 子项，把自己的 160.02 px 硬生生从
`flex:1` 的 textarea 里拿走，再加上一个 gap。

⇒ 机制推断被几何数据**证实**。操作员说「输入条不太好看」的量化含义是：
**窗口越窄，损失占比越狠**（900 宽时三成没了）。

## 4c. 一个可复用的测量配方（也是本轮的方法论收获）

**在 gopi web 里量 DOM 几何，不要走「截图 + vision/OCR」**：
本轮实测 OCR 转录的数字**与真实值不符**（同一个页面，OCR 读到 `613.84` 的同一张图里
`FORM` 行有时读不到、`delta` 有时读成 `170.01`），而日志里的是 `170.02`。
**差 0.01 px 不是噪声，是「这份证据不可用于验收」的信号。**

可靠路线两条，都不需要手工起 chromium（本机手工起会 core dump，§5）：

1. **页面自己回报（本文用的）**：复制页面 → 注入测量脚本 → 把数字作为 GET 打回
   同源服务器 → 读服务器的访问日志。零新依赖（`python3 -m http.server` 是标准库）。
2. **`--wait-selector` 当断言而非取数**：脚本在满足条件时才插入一个带**唯一 id** 的
   节点（id 里编码被验证的常量），`gopi tool shot --wait-selector '#<id>'` 命中 ⇒ exit 0，
   不命中 ⇒ `selector not found` + exit 1。已实测控制组确实 exit=1
   （`#never-matches` → `shot: selector "#never-matches" not found within 15s`），
   所以这个 exit 码是真断言，不是摆设。
   **局限（实测）**：id 里带精确小数时不能用它验「值是 X」，因为命中要求字符串**逐字**相等，
   而 OCR 给的近似值拼出的 id 打不中（本轮两条 such selector 都 exit=1 —— 恰好反证了
   OCR 的数不是真值）。⇒ 精确数值用路线 1，条件/存在性用路线 2。
