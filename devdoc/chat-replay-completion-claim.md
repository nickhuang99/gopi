<!-- doc-class: normative -->
# `chat --replay` 的完成声明必须是一次比较

## 现象（实测，2026-10-27）

录音 `80c07a7d` 有 152 个 model request、134 个工具结果。重放跑完只服务了 93 / 82，
收尾却写：

```
[gopi replay] tool results served from the recording: 82 (unrecorded: 0),
model requests: 93 (script-exhausted: 0), turns typed: 11/11
```

这句话每个数字都是真的，整句是假的：它不写录音里有多少，所以"服务了 61%"和
"服务了全部"读起来一模一样。`turns typed: 11/11` 是关于**键盘**的陈述 —— 十一句
prompt 确实都打进去了 —— 不是关于**脚本**的陈述。

## 两个成因，这一个数字分不开

1. **驱动提前推进。** `waitTurn` 把"计数器静默 700ms"当成"turn 结束"。录音的
   实时屏幕上有 `50s · sent` 这种状态行：一轮工具调用还在跑，计数器只是两轮之间
   安静了一下。把下一句 prompt 打进这一轮，编辑器会收下这些键。
2. **构造性不可达。** 录音的实时屏幕 dump（`chat-screen.dump.json`）里
   `⚙call` 共 **134** 行，其中 **80** 行在操作员自己的轮下、**54** 行在七个
   `[system] Background job … finished` 轮下。重放的 `job` 工具是**从录音回答**的，
   所以没有后台进程在跑，也就没有"完成通知"被注入 —— 那七轮的脚本任何重放都取不到。
   实测这一轮服务了 82 个结果，缺口 52，与这七轮的量级一致。

两种原因的处置完全不同：第一种该修驱动，第二种该让摘要**说清楚构造边界**。把两者
混在同一句话里，会让后来的人去追一个不存在的竞态。

## 改法

- `cmd/agent/chat_replay_account.go`（新）持有全部结论句与唯一判据 `Short()`
  —— 摘要行和退出状态读同一个谓词，所以"打印了缺口"和"承认了缺口"不可能互相矛盾。
- 收尾行永远是**比较**：`served: 93 of 152 recorded model request(s), 82 of 134
  recorded tool result(s), turns typed: 11/11`，然后 `SHORTFALL:` 点名缺口、列出
  每一个落后的 turn 及其自己的计数、并写出两种成因。
  两侧相同时才印 `COMPLETE`，边界是精确的（少一个响应就撤不回）。
- 驱动侧：`closeTurn` 把"这一轮消费了多少"按**该轮自己的份额**记账
  （`before := count()` 之差），静默臂不再算成功；`waitTurnSettled` 把"可以打下一句"
  的门槛改成 `chatReplaySettleQuiescence`（3s，`chatReplayQuiescence` 的倍数），
  等不到就带着原因停下，而不是把 prompt 打进还在干活的 turn。
- `replaydrive.waitTurn` 的注释改掉了旧结论（"静默 = 这一轮结束了，是成功"）。

## 回归测试

`go test ./cmd/agent/ -run 'TestChatReplaySummary|TestCloseTurnRecords|TestWaitTurnSettled|TestAccountingReads'`

红证明：把同一份测量形状（152/134，服务 93/82）喂给 **HEAD 的**
`reportReplayResults`，输出里搜不到 `152`，测试失败；换到修好的树，输出带上
两侧计数与 `SHORTFALL`，测试通过。两侧的原始输出都在本 run 的证据目录
（`redproof-before.txt` / `redproof-after.txt`）。

## 这条规则的边界

`turns typed: N/M` 本身没有错，错在**只有它**。任何"完成了"的断言都必须同时给出
分母，并且分母来自录音而不是来自这次运行。
