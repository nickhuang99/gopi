<!-- doc-class: normative -->
# 聊天页的 DOM 顺序 = SSE 到达顺序（方向 A：气泡延迟挂载）

## 症状与根因

`gopi web` 的聊天页曾把 log 的空间顺序排成了 SSE 时间顺序的**镜像**：

```
用户看到：  user
            assistant（回答）
            toolcard（这张卡是回答的原因）
线上顺序：  tool(call) → tool(result) → delta… → done
```

根因只有一行：`runTurn` 在 `fetch(API_CHAT)` **之前**就 `addMsg('assistant','')` 把空气泡
append 到日志末尾占位，并立刻挂上 `.caret` 闪烁光标。此后每个 `tool` 帧都
`addToolCard(...)` → `logEl.appendChild(card)`，于是卡片只能排在**已经存在**的空气泡
之后。气泡是「先到的」，卡片是「后到的」，空间顺序就与时间顺序相反 —— 读者无法判断
哪张卡对应哪段回答（TUI 里不会这样，TUI 是边到边打印）。

## 修法（方向 A：延迟创建）

气泡的**创建**从「请求前」移到「第一个必须往里画的帧」：

```js
let bubble=null,caret=null;
function openBubble(){
  if(bubble||message.startsWith('/'))return bubble;   // 幂等；斜杠轮直接 null（R4）
  bubble=addMsg('assistant','',true);                 // quiet: B3 门，不强制滚动
  caret=document.createElement('span');
  caret.className='caret';bubble.appendChild(caret);
  return bubble;
}
```

- `delta` 分支：`openBubble()` → 移除 caret → 渲染。气泡在**此刻**才落位到日志末尾，
  因此它一定排在之前到达的卡片之后。
- `tool` 分支：只 append 卡片，**不**开气泡。卡片按到达顺序落位。
- `queued` 分支（B2 等待提示）：只画提示，不开气泡（回答还没开始，不能让占位抢先占行）；
  服务端的 `done` 帧移除提示 —— `done` 在回答的 delta 之前到，所以提示不会夹在卡片与回答之间。
- `!res.ok` 与**非** abort 的 `catch`：也走 `openBubble()`，失败信息仍落在它在时间线上应得的位置。
  abort（用户按 Esc 停）若一个字都没流过，则什么都不挂 —— 没有内容可展示。

`addMsg(role,text,quiet)` 新增的第三个参数只影响滚动：`quiet` 走 `follow(false)`，
其余调用点仍是 `follow(true)`。**B3 follow-bottom 门原样保留**（读屏者向上翻时不被拽走）。

## 不变量逐条对照

| 不变量 | 状态 |
|---|---|
| 单会话合并 `s.mu` 每轮互斥 | 未触碰（只改客户端渲染） |
| B1 reset（`RESET_NOTICE` → 清屏） | 未触碰 |
| B2 queued 提示 | 保留；且**不再**顺带开气泡（更严格） |
| B3 follow-bottom 门 | 保留；延迟挂载节点用 `follow(false)`，不强制滚动 |
| B12 steering / 队列 | 未触碰 |
| R4 斜杠轮不开气泡 | 保留（`openBubble()` 对 `/` 前缀直接返回 null） |
| SSE 服务端发送顺序 | 未改 |

## 门禁

1. **驱动测试** `TestWebDomOrder_ToolCardPrecedesItsAnswer`
   （`cmd/agent/cli_web_domorder_test.go`，走已有 `cli_web_page_harness.js --only`）——
   跑的是**发布的页面本身**，不是副本。对应场景是
   `cli_web_page_scenarios.js` 的「DOM order equals ARRIVAL order: …」，它用**分块可暂停**的流
   在「tool call 已到、回答还没有一个字」这一刻停下观察 DOM（harness 自带的 `sseResponse`
   一次把整块 body 交出去，看不到决策发生在哪一步），然后放行后续帧。
2. **静态测试** `TestWebDomOrder_BubbleIsNotOpenedBeforeTheRequest`——
   钉住修复形状：`runTurn` 内 `addMsg('assistant'` 只允许出现在 `openBubble` 里；
   `delta` 分支必须调 `openBubble()`；`tool` 分支不得调。

## red proof（实测）

把 `cmd/agent/cli_web.go` 回退到 HEAD 后：

```
FAIL NO empty assistant bubble is mounted before the answer streams (got 1, want 0)
FAIL DOM order = arrival order: the card is BEFORE the answer (got ["user","assistant","tool"])
FAIL: 11/13 checks
```

恢复后 `PASS: 13/13 checks`，全量场景 `PASS: 156/156`。

用**真实**服务端的 SSE 回放同一份数据到两个版本的页面（`evidence/dom-order-check.js`）：

```
修复后： #1 toolcard → #2 assistant   VERDICT: DOM order matches arrival order
修复前： #1 assistant → #2 toolcard   VERDICT: DOM order is REVERSED
```

## 一个踩过的坑

页面 JS 活在 Go 的 **raw string** 里（`const webChatPage = \`…\``），所以
**注释里也不能出现反引号** —— 我在注释里写了 `` `done` ``，直接截断了 raw string，
`go build` 报 `syntax error: unexpected name done after top level declaration`。
在这个文件里引用事件名就裸写 `done` / `delta` / `tool`。
