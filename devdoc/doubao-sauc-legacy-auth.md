<!-- doc-class: normative -->
# Doubao SAUC v3: 旧版控制台鉴权 + 三方转写对照

Task: `01a132a1-b801-7398-a2c7-ae4e7ad089c9` (branch `task/01a132a1-b801-7398-a2c7-ae4e7ad089c9`)

## 1. 结论（最前面）

**探测器的 403 从来不是"服务未开通"，而是"鉴权形状用错了"。** 把
`devdoc/doubao-asr-eval/minclient` 的硬编码 `X-Api-Key: <UUID>` 换成旧版控制台
的应用凭据对，同一个 endpoint、同一个 `resource_id`
立刻拿到 `101 Switching Protocols`，并转出全文。

同一段真实录音（`~/Music/recording/Recording 1.mp3` → 16k mono wav，4.8327s）
三条通道的转写：

| 通道 | 转写 | 首字 | 最终 |
|---|---|---|---|
| v2 流式 `api/v2/asr`（`--engine streaming`） | 你能听到吗？不知道能不能听到声音**啊**？ | 1.24s（首部分） | 1.68s |
| v3 AUC 批量 `auc/bigmodel`（`--engine recording`） | 你能听到吗？不知道能不能听到声音**呢**？ | — | 2.5s |
| **v3 sauc 2.0** `sauc/bigmodel_nostream`（本探针，legacy） | 你能听到吗？不知道能不能听到声音**呢**？ | — | 5.52s |
| **v3 sauc 2.0** `sauc/bigmodel_async`（本探针，legacy） | 你能听到吗？不知道能不能听到声音**呢**？ | 2.86s | 5.22s |

**两派分歧只有一个字：v2 流式说「声音啊」，两条 v3 通道都说「声音呢」。**
并不是谁错——两个语气助词在这句话里都成立；这是**同一段音频上 v2 与 v3 的
模型差异**，且 v3 的两条通道（批量 / 流式）彼此一致，说明是版本差异而不是通道噪声。

## 2. 鉴权：两种形状，不可互换

| 形状 | 头 | 本账号对 `volc.seedasr.sauc.duration` 的结果 |
|---|---|---|
| 旧版控制台（**应用**凭据） | `X-Api-App-Key: <AppID>` + `X-Api-Access-Key: <Access Token>` | **101 Switching Protocols** ✅ |
| API Key（单键） | `X-Api-Key: <UUID>` | **403** `[resource_id=…] requested resource not granted` |

那个 UUID Key 属于**别的应用**，所以对这条产品线永远是 403。**403 读起来像
"没买这项服务"，实际是鉴权形状不匹配**——这次实测把两种形状在同一个 endpoint
上正面对照了一遍（run `sauc20-apikey-control`，见证据）。

探测器的改动：

- 新增 `-auth legacy|apikey`，**默认 `legacy`**（本账号唯一能通的那种）；
- legacy 从 `-app_key/-access_key` 取，缺则读 `-cred_file`（默认
  `~/.doubao/v2_voice.env` 的 `APPID=… / TOKEN=…` 形状，`ACCESS_TOKEN` 作别名）；
- `apikey` 保留原行为与 `-api_key_file`，**只读自己那一种来源**——一种形状的
  缺失不会让另一种跑不起来；
- 头名、鉴权方式值全部取常量（`HeaderAppKey` / `HeaderAccessKey` / `HeaderAPIKey` /
  `AuthLegacy` / `AuthAPIKey`），文件里没有裸字面量；
- 密钥**只经进程内存**：stderr 横幅与一切输出走 `redact()`（只印前 2 字符 + 长度），
  evidence 文件里没有任何明文（已 grep 复核，见 `## Verification`）。

## 3. 为什么这次不需要动帧

协议帧本来就对（`main.go` 的 `saucHeader` / 序号起始 2 / 末包取负）。上一轮
在 /tmp 手写的 Python 探针卡住的"帧头 bug"，正是仓库里这份实现早就写对的东西 ——
**缺的只有鉴权头**。所以本次改动是**外科手术式**的：只换鉴权构造，协议零改动。

一个反例记录，避免下次再踩：**设备不等于凭据**。`X-Api-Key` 与
`X-Api-App-Key`+`X-Api-Access-Key` 是两套并存的鉴权协议，服务端按形状分派；
"这个 Key 是这个账号的" 推不出 "这个 Key 对这条产品线有效"。

## 4. 延迟口径（别把两个数混起来）

三条通道的"延迟"不是同一个量：

- v2 流式：**首部分** 1.24s / **最终** 1.68s（边发边出字）；
- v3 AUC：提交 + 轮询，2.5s 出全文（**没有首字概念**）；
- v3 sauc `nostream`：文件语义，`paced=true` 时发包本身就耗时 5.06s，5.52s 出全文
  （RTF 1.14，`send_speed 0.95×` 实时）；
- v3 sauc `async`：2.86s 出首字（**比 `nostream` 早一倍**），5.22s 定稿。

即：**`async` 端点才是这次"最准且最早出字"的组合**（v3 的准确率 + 2.86s 首字），
`nostream` 适合"整段进、一次出"的文件语义。

## 5. 改动清单

| 文件 | 改动 |
|---|---|
| `devdoc/doubao-asr-eval/minclient/main.go` | `+205/-17`：鉴权常量 + `credentials`/`authHeaders`/`buildHandshake`/`parseCredFile`/`resolveCredentials`/`redact`/`authAnnounce`；新增 `-auth/-app_key/-access_key/-cred_file`；握手结果显式打印（`[ws] upgrade HTTP 101`） |
| `devdoc/doubao-asr-eval/minclient/auth_test.go` | 新增，8 个回归测试（见下） |

`go.mod` **未动**：仍是 `module doubao-minclient`，`go list -deps` 里没有任何
非标准库依赖（零依赖约束成立）。

## 6. 回归测试（钉住"两种形状各自发哪些头"）

```
TestAuthHeadersLegacyEmitsExactlyTheAppKeyPair      legacy 恰好 2 个头 = AppKey 对，且绝不含 X-Api-Key
TestAuthHeadersAPIKeyEmitsExactlyOneHeader          apikey 恰好 1 个头，且绝不含旧版那两个
TestBuildHandshakeCarriesAuthOnceAndKeepsTheUpgrade 握手文本里每个头恰好出现 1 次 + resource id 显式 + 空行结尾
TestAuthAnnounceNeverCarriesTheSecretWhole          横幅不含明文 token/key
TestRedactShortensAndNeverEchoesWhole               脱敏函数不整文回显、空值不 panic
TestParseCredFileReadsAppIDAndToken                 KEY=VALUE 解析（跳过 # 注释/空行）
TestParseCredFileAcceptsAccessTokenAliasAndRefusesPartial  ACCESS_TOKEN 别名；缺 token 必须报错而不是半读
TestResolveCredentialsPrefersFlags…/ReadsOnlyTheSourceItsModeNeeds  旗标优先；各形状只读自己的来源
```

## 7. 证据（本轮 session 目录，不随仓库提交）

`~/.gopi/sessions/01a132a1-b801-7398-a2c7-ae4e7ad089c9/artifacts/`

| 文件 | 内容 |
|---|---|
| `rec1-16k-mono.wav` | ffmpeg 从 `Recording 1.mp3` 转的 16k/mono/s16le |
| `sauc20-nostream.{txt,metrics.json,raw.txt,stderr.txt}` | v3 sauc nostream 全量原始证据 |
| `sauc20-async.{txt,metrics.json,raw.txt}` | v3 sauc async |
| `sauc20-apikey-control.*`（stderr 内） | **反证**：`X-Api-Key` → 403 原文 |
| `gopi-v2-streaming.txt` / `gopi-v3-auc.txt` | 另两条通道的完整 CLI 输出 |

## 8. 顺带发现（已按 Rule 41 当场提行，各带实测证据）

跑 `make test-budget PKGS='./internal/standards/ ./internal/docindex/ ./internal/doubaoasr/ ./internal/doubaostream/'`
时 `./internal/standards` 红两条 —— **两条都与本次改动无关，base `cf15c989d` 上就已存在**
（本 run 的 diff 只含 4 个 devdoc 文件，未触碰 `cmd/`、`COMMANDS.md`、`session-record-view-refilter-gate.md`）：

1. `TestCommandsDocGeneratedBlockMatchesSource` + `TestCommandsDocGateRedProofStaleGeneratedBlock`：
   `COMMANDS.md` 的生成块 stale（`cli_archive.go:179` vs 源码 `:188`），
   **连带让那条红证明失效**（它的 precondition 是"块是新鲜的"，块坏了它直接 FAIL 而非跳过，
   于是不再能区分"门禁抓到篡改"与"块本来就坏"）。→ 行 `01a132a8-c9e7-7f33-9536-9b7db9b16ae6`
2. `gopi tool doccheck` exit 1（anchor NUMBER drift，`devdoc/session-record-view-refilter-gate.md:1724`
   记 `cmd/agent/cli_notify_deliver_idle_test.go:200`，文本实际在 `:195`）。→ 行 `01a132a8-3e64-705a-b3e1-2a9909fd6f7a`

两者的原始命令 + 输出落在本 run 的 session 目录
（`artifacts/commands-md-stale-block-evidence.txt`、`artifacts/doccheck-anchor-drift-evidence.txt`），
已作为附件随行提交。**发现者不修**（Rule 28：系统层归 peerbox）。

## Not Done

- **没有把 v3 sauc 接进 `gopi tool voice_input`**。本次交付范围是仓库自带**探针**
  的鉴权修复 + 三方对照；把 sauc 做成 `--engine` 的第三条通道是产品改动
  (`internal/doubaoasr` / `internal/doubaostream` 同级的第三个包 + 配置键 +
  路由)，不在本 epics 的 deliverables 内，且超出单 session 预算。残留问题：
  现在要跑 v3 sauc 需要手工 `go run ./devdoc/doubao-asr-eval/minclient`。
- **只测了一条 4.83s 样本**。三方对照的结论只对这一条音频成立；"v3 两通道
  一致、v2 差一个字"的推广需要更多样本。
- **`enable_nonstream` 与 hotwords 未做 A/B**：本次全用默认 flag 跑通链路，
  参数调优未做（也没有对照基线）。
