<!-- doc-class: historical -->
# devdoc: 镜像必漂移 —— testsuite 拒绝词表改取 /help 真源

Task `01a0ef96-6373-76b8-97ae-e53fc8c64231` · 分支
`task/01a0ef96637376b897aee53fc8c64231` · commit `fa774f0`。

## 症状与根因

`testsuite/chat_once_slash_test.go` 的验收用例
`TestChatOnceSlash_RefusalVocabularyMatchesTheHelpPage` 红，且**红的是测试，不是生产者**：

```
got:  [/clear /code-search /exit /help /log /memory /new /queue /quit /task /text2img /tools]
want: [/clear /code-search /exit /help /log /memory /new          /quit /task /text2img /tools]
```

`want` 来自测试文件里手写的 `chatOnceSlashAcceptedWords()`；`got` 来自真实二进制。
二进制是对的（`/queue` 确在 `chatSessionWords`，`cmd/agent/lineedit_wire.go:853`）。

5-why：清单在 6bb0135c 写下 → `/queue` 在 ff76758c 才进命令表 → 清单是一份**镜像**（第二出处）
→ 没有任何机制能让它跟上 → 它必然落后。停因：**镜像**，不是「忘了改」。

更糟的是该用例的 (c) 段把真源比对与清单并列：
`if !helpWords[w] && !chatOnceSlashAcceptedWords(t)[w]` —— 这等于给手写清单开了豁免口，
所以 `/queue` 在修前**没有**被 (c) 段报出来，只有 (b) 段的集合相等碰到它。删清单时连豁免一起删。

## 修法（Rule 13/14）

一处定义，且定义就是**操作者被展示的那个 surface**：本 run 自己印出的 /help 页。
`chatOnceSlashHelpVocabulary(stdout)` 按操作者读页面的两种拼法收词：

1. 行首以 `/word` 起头的行（词表行）；
2. 行内 `(also /alias)` 的第二拼法（`(also /exit …)`、`(also /new)`）——它是同一命令的另一种
   被接受的拼法，因此也属于词表。

括号里没有斜杠词（`(also -h on any command)`）不收词：不猜（Rule 15）。
断言仍是**集合相等**（`sameWordSet`：长度 + 逐词），非包含；(c) 段双向报向
（refusal 窄了 / 宽了），失败信息指出漂移方向。

`len(helpWords) == 0` 从 `t.Fatalf` 改为 `t.Errorf`：抽取失败必须**报错而不是静默**——
若断言形式被改成「其它形状」，空集相等会给生产者免费通行证（一个空的期望集等于一个空的现实）。

## 变异验证：必须变异「最终集合」，不是某一处来源

red proof 用真实二进制 + 一次性变异实测（`GOPI_BIN=<变异二进制> go test …`）：

| 变异 | 结果 |
|---|---|
| `slashWordList`: `words = words[1:]`（少一个词） | **红**：`/help prints /clear but the refusal does not list it — the refusal is the narrower one` |
| `slashWordList`: `append(words, "extra-word")`（多一个词） | **红**：`the refusal lists /extra-word, which this run's /help page does not print — the refusal is the wider one` |

**第一次变异没抓到，值得记下**：初始变异改的是 `chatReplCommandNames()` 里删
`chatQueueCommandName`，测试**仍然绿** —— 因为 `chatSessionWords` 本来就含 `queue`，
并集把删掉的词补了回来。教训：真源是**并集**时，删一个来源不改变最终集合；
「我变异了，红了」这句话本身可能是假绿。变异要打在**渲染/最终集合**（`slashWordList`）上，
才证明断言真的看着被交付的词表。

## 边界：什么断言归谁

- **本 diff 不动 `cmd/agent/`**：生产端正确。持久化的「漏词」门禁已经在
  `cmd/agent/chat_slash_refusal_vocabulary_test.go` ——
  `TestChatReplVocabularyCoversBothSources` 直接用真源常量
  （`slashCommandNames` / `chatSessionWords` / `webClear*`）封住「词表漏掉任一来源」，
  不需要逐词清单来封。
- **testsuite 用例独有的价值**：它比的是**同一次运行产出的两个 surface 的两份渲染**
  （stdout 的 /help 页 与 stderr 的拒绝行，真实二进制）。那是 in-process 门禁看不到的。
- 手写清单的价值若确实需要（独立于生产者），位置应在 `cmd/agent/`，用常量而非字面量 ——
  不是在一个 testsuite 文件里抄一份词表。

## 复现命令

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/          # testsuite 缺 bin/gopi 会 SKIP（假绿）
go test ./testsuite/ -run 'TestChatOnceSlash' -count=1 -v
SB=$(mktemp -d /dev/shm/probe.XXXX)
printf '/frobnicate\n/help\n/quit\n' | GOPI_HOME=$SB ./bin/gopi chat --once   # exit=1，两侧词表可肉眼比对
```
