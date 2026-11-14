<!-- doc-class: normative -->
# doubao 音频通道的端点与流式凭证收进 settings.json

task `01a11f9037977f12835d662d35a0e6ab` · 2026-10-09

## 为什么做

vision 侧早就有一个**成对**的形状：`vision_model` + `vision_base_url`（同一个
vendor，两个事实，都在 settings.json 里、都有 `--flag` 和 `$GOPI_*` 通道）。
doubao 音频侧不对称，而且是三处不对称：

| 通道 | 端点 | 凭证 |
|---|---|---|
| batch（`doubao`） | 两个 URL 是**编译期常量**，只有 `--doubao-submit-url` / `--doubao-query-url` 能改（settings/env 里没有面） | settings/env/flag 三级链齐全（2026-10-07 起） |
| stream（`doubao-stream`） | `doubaostream.DefaultEndpoint` 常量 —— **任何通道都改不了**（`Options.Endpoint` 只有 Go 调用者够得着） | 只能来自 `KEY=VALUE` 文件（`~/.doubao/v2_voice.env` 或 `--doubao-key-file`） |

结果：运维把 batch 的 key 放进 settings.json 之后，仍然要为流式通道在
vendor 自己的目录里保留一份文件；而"账号在另一个 region"这件事，batch 侧要
写两条 URL（还可能写歪，见下），stream 侧干脆要改代码。

## 做了什么

### 1. 一个 base 派生两个 URL（而不是两个可配 URL）

`internal/doubaoasr`：

```go
DefaultBaseURL   = "https://openspeech.bytedance.com/api/v3/auc/bigmodel"
SubmitURLPath    = "/submit"
QueryURLPath     = "/query"
DefaultSubmitURL = DefaultBaseURL + SubmitURLPath   // 仍是同一个字符串
DefaultQueryURL  = DefaultBaseURL + QueryURLPath
func SubmitURLFor(base string) string
func QueryURLFor(base string)  string
```

**为什么是"根"而不是两个 URL**：batch 协议是"提交拿 task id → 轮询那个 id"。
两条独立可配的 URL 允许出现"提交到 A 主机、轮询 B 主机"的配置，而那个组合
的错误信息是 `task id not found` —— 报错指向的东西（task id）与病因（两条 URL
不同源）无关。一个根让这个组合**不可表达**。

`DefaultSubmitURL`/`DefaultQueryURL` 仍然是导出常量且值不变（老调用者零改动），
只是现在**派生**自根 —— 既满足 Rule 13（值定义一次、处处引用），又让"根 ↔ 两个
URL"的关系被一个测试钉死（`internal/doubaoasr` 既有测试 + 新增断言）。

`--doubao-submit-url` / `--doubao-query-url` **仍然比根更具体**：各自只覆盖自己
那一条 URL，命名其中一条**不会**把另一条从根上摘下来（有断言）。
`SubmitURL: stringOrDefault(opts.doubaoSubmit, doubaoasr.SubmitURLFor(root))`。

### 2. 流式凭证有了 inline 面，文件仍然赢

`cmd/agent/cli_voice_stream.go` 的 `voiceStreamCredentialSource` 是**唯一**解算点
（Rule 2），顺序写在一处：

```
--doubao-key-file > $GOPI_DOUBAO_KEY_FILE > settings "doubao_key_file"
  > 该文件存在且可解析 → 用它（旧部署零感知）
  > $GOPI_DOUBAO_APP_ID / $GOPI_DOUBAO_TOKEN（可半来自 env、半来自 settings）
  > settings "doubao_app_id" / "doubao_token"
  > ~/.doubao/v2_voice.env（历史位置）
```

两条**非对称**是刻意的，各自有理由：

- **文件赢了内联**（batch 侧相反：显式命名的文件"赢或拒绝"）。batch 的 key 是
  单值、语义唯一；流式的两半可能一半在 env、一半在 settings，此时"文件存在就
  让文件整对胜出"是最少惊讶的规则。运行打印的 `credential:` 行会说清用的是谁。
- **文件不可用 + 内联成对 → 用内联，但播报替换**（`voiceStreamFileFellThroughNote`）。
  运维可能先配了内联、再指了个还没写的文件路径；两个通道独立，"能用哪个用哪个 +
  说清楚"优于"因为一个通道坏了就拒绝整个运行"。**绝不静默**（Rule 15）。
- **两者都没有 → 预检就拒绝**，报文点名全部五个 knob，从不打印值。

### 3. 端点三级链（batch 根 + stream URL）

`voiceDoubaoSettingSource` 里按 settings → env → flag 覆盖式写入（最后一个写的
赢），新增 `baseURL` 与 `streamEndpoint` 两个字段；flag 常量
`--doubao-base-url` / `--doubao-stream-endpoint`。**终值不在 resolver 里落字面量**：
空值由消费方经 `doubaoasr.SubmitURLFor` / `doubaostream.DefaultEndpoint` 落地，
resolver 不会成为"已发布默认值"的第二个来源（Rule 13）。

stream 端点在预检里做**形状**检查（必须 `wss://`），不合格就点名
`--doubao-stream-endpoint` / `$GOPI_DOUBAO_STREAM_ENDPOINT` —— 在开麦之前，而不是
在 dialer 里。

### 4. 两个坑（都实测命中）

1. **resolver 在一次真实运行里被调用两次**：先 `voiceDepsReady`（预检）、后
   `transcribeFileStream`（干活）。把 note 写在 `load()` 里 → 同一句话连打两行
   （实测 D2 第一次跑出来的样子）。改法：resolver 只返回**值**
   （`path` / `fellThroughNote()`），由真正开 session 的那一层
   （`voiceStreamCredentials`）打印一次。`fellThroughNote()` 重算而非缓存状态，
   这样预检与运行不会对"是否发生替换"各说各话。
   这与 help 的重复行是**同一缺陷类**（同一句话出现两次），所以 help 也拿到了
   一个结构门禁（见下）。
2. **`os.UserHomeDir()` 不读 `$HOME`**：想让测试把默认凭证路径指到临时目录，
   必须先让 `userHomeDirOrDefault()` 优先读 `$HOME`（否则测试要么碰操作者真实的
   `~/.doubao`，要么根本测不到默认档）。

### 5. help 的重复行

`gopi tool voice_input -h` 里有一处跨行重复：

```
A missing key is refused BEFORE any audio is sent, and the
refuses BEFORE any audio is sent, and the              ← 重复段
refusal names the way back to --engine local.
```

**门禁的形状是这次的设计要点**：整行查重/整页查重都是错的仪器 —— 这一页**合法地**
重复短语（两个 engine 段落各写一遍同一个 flag 的三个通道、"CER comparison" 的散文
也在两处出现）。缺陷的签名窄得多：一段**跨相邻两行连续重复**。所以门禁是
`sharedWordRun(相邻两行) >= 6 词`，并且**自带红证**：把当初那两行原样作为 fixture
喂给检测器，检测器不响就 fail —— 免得它对着任何 help 文本都读绿
（`TestVoiceHelpHasNoSplitDuplicatedFragment`）。

### 6. 配置文档与门禁同步

CONFIG.md §2c 新增 4 行（`doubao_base_url` / `doubao_stream_endpoint` /
`doubao_app_id` / `doubao_token`），每行带决定它的源码行；
`internal/standards/config_doc_flag_gate_test.go` 的 `configDocFlagRowCount`
63 → 67，并在常量上方写了**为什么是 4 行**（审计理由，不是记账）。
顺带把 `doubao_resource_id` / `doubao_hotwords` 两行的 flag 列从 `—` 改成真实
flag：它们**本来就有** `--doubao-resource` / `--doubao-hotwords`（`cli_voice.go`
的 flag 表里），文档此前低报了自己的面 —— 改的是行，不是行为。

## 没做 / 边界

- **`ApplyEnv` 不发布凭证**（`doubao_api_key` / `doubao_app_id` / `doubao_token`）：
  那个环境会被 gopi 派生的每个子进程继承，发布等于把凭证复制一份更宽的副本。
  三个 key 的 env 通道**照常被读**（手动 export 有效，catalog 行里说明），
  但 gopi 自己不去设它 —— `internal/config/env.go` 里那段注释就是这条规则。
- **零新增依赖**（Rule 35）：没有新脚本、新配置文件、新常驻进程、新数据库。
  一个设置键 + 一条环境变量 + 一个 CLI flag，就是全部的持久化面。
- **未实测真实厂商端点**：本机没有可用的 doubao 账号，所以所有实测都打在"配置
  被采用 / 默认被采用 / 凭证被用在哪条通道"这一层（用 `.invalid` 域名 + 无音频
  与有音频两种），没有一次真实转写。见报告 `## Not Done`。
- **未改动**任何既有 key 名、未改引擎默认值（`voice_engine` 默认仍是 local）。

## 验证

见 `~/.gopi/sessions/01a11f90-3797-7f12-835d-662d35a0e6ab/task-report.md`：
`Build` / `Tests` / `Ran`（逐条真实调用与输出）/ `Evidence`。
