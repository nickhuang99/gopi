# 长程运行是设计出来的，不是靠记忆插件

大多数 agent 把记忆做成插件，上下文用尽就开新会话。gopi 把连续性当成运行时的属性。下面每个数字都在这份代码树或真实运行会话里实测过——没有来自文档的断言。

## 1. 一个二进制；换新版只需一次重启

没有安装步骤，没有要更新的解释器。切到新构建就是 `/quit`、启动 `gopi chat`、恢复会话——秒级，不是一次迁移。

```console
$ stat -c '%s' bin/gopi
76432065
```

单个 76 MB 静态文件就是全部部署。（发布版二进制是 `statically linked`；见 innovation-showcase 第 7 条。）基于解释器的工具更新的是一棵依赖树；这里替换的是一个文件。

**边界：** 更新就是替换二进制并重启。一个正在进行的 turn 里执行中的工具调用不会热切换，它们会在旧构建上跑完。

## 2. 数百个会话，一条连续的历史

连续性是在数千个已记录的会话上度量的，不是一次聊天。

```console
$ find ~/.gopi/sessions -mindepth 1 -maxdepth 1 -type d | wc -l
4890
$ for f in ~/.gopi/sessions/*/session.jsonl; do wc -l < "$f"; done | sort -rn | head -1
25636
```

一份会话日志达到 25,636 行仍在正常工作。最大的那些运行不是例外——它们正是这个 harness 为之设计的常态。

## 3. 200K 窗口是部署选择，不是模型上限

这是最容易被误读的数字，所以完整给出。底层模型标称 1M token 上下文。gopi 刻意运行在约 200K 的窗口，因为约束是 prefill 成本，不是上下文大小。

```console
$ grep -nE 'DefaultContextWindow = |DefaultCompactTriggerRatio = ' internal/agent/context.go
18:const DefaultContextWindow = 200000
32:const DefaultCompactTriggerRatio = 0.75
```

源码写明了理由：一次 290K token 的 prefill 实测约 16 秒，所以单任务跑在约 200K 的有效窗口；在 0.75 处压缩在 150K 只运行一次，而不是让历史涨到约 810K、每个后期回合都重新 prefill 一大段尾部。

**这是我们做的判断，不是施加在我们身上的限制。** 只写"200K"会被误读成我们的天花板；模型支持更多。这个选择关乎交互式回合时延。

## 4. 记忆从 SQLite 查询，不从文件加载

没有要安装的记忆插件，也没有退出时要重写的 `memory.md`。记忆存在单个 SQLite 数据库里，通过全文检索触达。

```console
$ stat -c '%s' ~/.gopi/memory.db
1122205696
$ grep -nE 'EMBEDDED dictionary|go-ego/gse' internal/cjkseg/cjkseg.go
2:// wrapper around the pure-Go `github.com/go-ego/gse` engine, loaded from its
3:// EMBEDDED dictionary so no data file ever has to exist beside the binary.
31:	"github.com/go-ego/gse"
```

中文分词是 `gse` 加一份编译进二进制的词典——旁边没有数据文件，没有独立服务。同一个分词器同时供记忆写入方的索引列和查询方使用，因此索引与查询不会各说各话。

**会话续接，作为一种设计差异：** 有些 harness 通过在退出时写记忆文件（`memory.md`、"重要笔记"）再读回新上下文来延续会话。gopi 每轮把一行 self-record 写进 SQLite，在下一回合开始时用 FTS5 召回相关条目。这是两种不同的设计——这里不断言另一方有缺陷。

## 5. 每一轮都记录一个能挺过压缩的决定

```console
$ wc -l ~/.gopi/sessions/<SESSION-ID>/self-record.md
60
```

这个文件是模型自己写的：每个回合边界，agent 追加一行 `[IMPORTANT]`——一条值得保留的提炼结论。它不是由守护进程或模型外部的钩子生成的；正在工作的模型把它当作自己这一轮的一部分写下来。

恢复会话时，这个文件是真相源。内存里的笔记列表在初始化时从文件一次性重建（`internal/agent/selfrecord_reload.go` 的 `restoreSelfRecordNotes`），所以被不同进程续接的会话不会在文件还留着所有旧笔记时，却以空列表开始。当压缩丢弃原始对话时，这些笔记被带回上下文头部——这就是为什么一百轮之前做的决定现在仍在改变行为。
