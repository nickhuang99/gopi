<!-- doc-class: normative -->
# sauc 端点来源可观测：LIVE 臂被一行旧配置静默覆盖（可观测性修复）

**任务** `01a1363f-5e6e-73d7-bba5-6ec828bdfa29` · 分支 `task/01a1363f5e6e73d7bba56ec828bdfa29` · 基线 `528501c`

## 一句话

`audio_engine_param.sauc.endpoint` 一旦有值，就**逐字**压过**两条臂**各自的默认端点；而 LIVE 臂
要的是 `bigmodel_async`（双向）、FILE 臂要的是 `bigmodel_nostream`（末尾只答一次）。于是一行
**为 FILE 臂写的配置**会静默废掉 LIVE 臂——已交付的实时修复（任务 `01a13303`）在本机从未生效，
而**代码是对的**。这次改的不是端点选择（配置优先是 operator 的权利），而是**让来源与语义不符
变成可观测量**：每次解析到 sauc 端点的运行，都说出这个 URL 是谁选的，并在它不满足 LIVE 臂契约时
明说不符。

## 缺陷（可复核，非推断）

`cmd/agent/cli_voice_sauc.go` 的 `voiceSaucLiveEndpoint()` 第一分支：

```go
if ep := strings.TrimSpace(voiceSaucSettingSource().Endpoint); ep != "" {
    return ep                                  // ← 配置值优先，且逐字生效
}
return doubaosauc.EndpointBidirectional        // ← 没配才是 bigmodel_async
```

**实测（2026-10-13，本机）**：`~/.gopi/settings.json` 的 `audio_engine_param.sauc.endpoint`
曾是 `…/bigmodel_nostream`，`SAUC` 落在 LIVE 臂（`voiceLiveEngineSelected` 对 `sauc` 返回 true），
端点却是文件语义的那个 → 症状「一段一段、不到实时」。运行**只说了它要去哪个 URL，从不说谁选的、
也从不说什么会坏**（Rule 15：该说的没说）。

## 设计

三个函数，一处决议（Rule 2/13/14），两处宣告（CLI 面 + chat 面）：

| 函数 | 职责 |
|---|---|
| `voiceSaucSettingEndpoint` | 设置文档里那个端点的**唯一**读者——「是配置的还是默认的」由它回答，不从 URL 反推 |
| `voiceSaucEndpointOrigin` / `…OriginNote` | 来源词汇（`settings` / `engine`），总是给出一句 |
| `voiceSaucLiveEndpointNote` | **只**在「来自 settings **且**不等于 `EndpointBidirectional`」时给出一句语义不符说明；否则为 `""` |
| `voiceSaucEndpointDecisionNote` | 把上面两句收成一个列表——宣告点不自己拼装句子 |

**为什么 `engine` 默认的 `bigmodel_nostream` 不报不符**（关键设计，非疏漏）：两条臂的默认**故意不同**
（FILE 臂开 nostream、LIVE 臂开 async），各自都正确。对没配的机器报警，等于对**工作正常的配置**说假话，
而一个动不动喊狼的行会让 operator 学会跳过真正要紧的那行。真正坏掉的是**一例**：operator 自己为
**一条臂**写的那行被逐字用到**两条臂**上，使另一条臂的默认再也到不了。

**只说不改**：配置值仍然逐字生效；不拒绝、不悄悄换回默认。跑照跑，只是说出来。

## 落点

- `cmd/agent/cli_voice_sauc.go`：上述函数；`voiceReportSaucPrivacy`（CLI 每个会**上传**的 sauc 运行
  都经过的那条披露路径）多打印决议行——复用既有宣告点，不新开调用点（Rule 3）。
- `cmd/agent/chat_voice.go`：`/voice` 的 sauc 引擎行加上来源（`chatVoiceSaucEngineLabel` 多一个 `%s`），
  并在 recorder 具备 LIVE 半（`voiceChatLiveRunner`）时打印语义不符行——不具备 LIVE 半的 recorder
  会被窗口循环接手，那一行就会描述一个这 run 没有的臂（Rule 15）。
- `cmd/agent/cli_voice.go`：help 里 sauc 段补上「每条运行都说出 URL 是谁给的；若不是双向端点，
  还会说明 LIVE 会话只会在你说完之后才答」。
- `cmd/agent/cli_voice_sauc_endpoint_origin_test.go`（新增）：4 个回归用例。

## 实测（真跑该 surface，Rule 31）

**配置有值（nostream，即本机事故配置）**：

```
$ GOPI_SETTINGS=…/settings-configured.json ./bin/gopi tool voice_input --engine sauc …/jfk.wav
[voice] PRIVACY — engine sauc UPLOADS the audio to …/bigmodel_nostream …
[voice] sauc endpoint from settings "audio_engine_param.sauc.endpoint" (a configured endpoint wins verbatim, and it overrides BOTH arms)
[voice] sauc endpoint "…/bigmodel_nostream" comes from settings and is not the bidirectional "…/bigmodel_async": it answers ONCE, when the audio has ended. That is what the FILE arm wants and NOT what the LIVE arm wants — … A configured endpoint wins on BOTH arms (settings "audio_engine_param.sauc.endpoint"), so the LIVE arm's own default is unreachable until that line changes
[voice] sauc channel → …/bigmodel_nostream (resource volc.seedasr.sauc.duration), credential file /home/user/.doubao/v2_voice.env
[voice] sauc: 108 chars, 11000 ms audio, 55 packets, first text 1.206s, logid 2026101606254946FF6D52A252A1CAC82F
And so, my fellow Americans, ask not what your country can do for you, ask what you can do for your country.
EXIT=0
```

**配置为空（引擎默认）**：

```
$ GOPI_SETTINGS=…/settings-engine-default.json ./bin/gopi tool voice_input --engine sauc …/jfk.wav
[voice] sauc endpoint from the engine default (nothing is configured at audio_engine_param.sauc.endpoint)
[voice] sauc channel → …/bigmodel_nostream (resource volc.seedasr.sauc.duration), credential file /home/user/.doubao/v2_voice.env
[voice] sauc: 108 chars, 11000 ms audio, 55 packets, first text 1.385s, logid 202610160625526E5C5EA61B754EBBA53C
EXIT=0
```

两半都对上了：**有值 → 明示「来自 settings」+ 语义不符提示；为空 → 明示「引擎默认」+ 无提示。**
（两次都真连了 bigmodel 端点、真拿到了 108 字符转写与 vendor logid，所以这不是 dry-run。）

## 先红后绿

| 红肢 | 手法 | 观察到的失败 |
|---|---|---|
| CLI 面 | 从 `voiceReportSaucPrivacy` 摘掉决议行（只留 `voiceReportSaucPrivacy` 原样） | `--- FAIL: TestVoiceSaucEndpointOriginIsStated … the run did not say the endpoint came from settings`；恢复后绿 |
| chat 面 | 把 `m.notice(…voiceSaucLiveEndpointNote…)` 换成 `_ = voiceSaucLiveEndpointNote` | `--- FAIL: TestVoiceSaucEndpointOriginOnTheChatLiveArm … the live arm did not state that a file-semantics endpoint contradicts it`；恢复后绿 |

两肢都**只摘掉了新句子**，端点行本身仍然打印——所以用例不是因为「行没了」而红，而是因为缺了那句话。

## 诚实缺口

- **LIVE 臂本身的观感未实测**：本机 `~/.gopi/settings.json` 现已指向 `.sauc-no_endpoint.json`
  （`endpoint = bigmodel_async`），且 `/voice` 拒绝管道输入，所以这次**没有**再跑一次真麦克风的
  `/voice`。新增的是**宣告**，它的证据是 chat 面的 pty 用例（生产载体、真读循环）加上 CLI 面真跑，
  未经的只有「人对着麦克风说话」这一段。
- 上表红肢的原始输出在报告里逐字引用；两条 fix 之后的文件与 `HEAD` 的差异可由
  `git diff HEAD -- cmd/agent/cli_voice_sauc.go cmd/agent/chat_voice.go` 复核。

## 复核方式

```bash
go build ./... && go test ./cmd/agent/ -run 'VoiceSauc' -count=1
git show <branch>:cmd/agent/cli_voice_sauc_endpoint_origin_test.go
git show <branch>:devdoc/sauc-endpoint-origin-observable.md
```
