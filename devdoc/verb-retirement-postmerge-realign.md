<!-- doc-class: normative -->
# 顶层动词退役：合并后对齐清单（条件任务的取证交付）

**状态：触发条件已成立 —— 退役已落地 master，本文件转为「落地后的实测记录」。**
本文件仍是「合并后要做的事」的真值，但它的前提已经从「尚未合并」变成「已合并」：
第 1 节的三条谓词在**本树实测全部为真**，第 2 节因此降级为**带日期的历史论证**，
第 3 节的清单已在落地后的树上**逐条复测**（原 🔴 条目全部消失，只剩与新落地形态相关的残余项）。

任务：`01a10e7a-b4ee-71a4-bc9b-3f2d6a3790c8`（conditional followup of `01a10e03-9a5e-7a31-ad0b-a8455e7dc3ee`）
复测任务：`01a10e98-bcd1-7931-bcd5-c6ab6a2a3508`（2026-10-06；本树 HEAD = master = `79b29f5f1`）

## 1. 触发条件：已成立（实测）

简报给的原谓词是**死信**（这一条现在仍然正确，且已由实测确认）：

```
git merge-base --is-ancestor d3492e0a HEAD          # → exit 1（判否）
```

`d3492e0a`（`feat(cli): retire the top-level verb spellings behind family doors`）
**当时**只存在于分支 `task/01a10dc306ad78b7b65313efd18879bc`。退役的落地形态是
`rework/verb-retirement-complete`（leading commit `a4314fdf feat(cli): retire the flat tool/verb
spellings — one door per command`）。**两者现在都已是 master 的祖先**（复测 rc=0 / rc=0，
见下），所以「原谓词是死信」这一判断在落地后依然成立：真正的**落地 commit 是那个合并**，
不是简报里点名的 hash。

**修正后的触发谓词（三重判定）—— 在 HEAD=`79b29f5f1` 上实测，三条全部为真：**

```bash
# (1) 祖先判定 —— 退役的落地形态已进 master
git merge-base --is-ancestor 92622821d master    # exit 0 = 已落地
#   → 92622821d "Merge branch 'rework/verb-retirement-complete' into
#                task/01a10e23d2ef75bca03634e6b4b1e719"  (2026-10-06 07:40:10 +0800)
#     这就是**落地 commit**：它的第一个父提交 cd50f0439（同日 07:06）仍是三谓词 PRESENT +
#     旧横幅 "gopi proxy: " 的形态，而 92622821d 之后两者同时变为 ABSENT / "gopi tool proxy: "。
#   两个直接谓词同为止祖：git merge-base --is-ancestor d3492e0a3 master → 0
#                      git merge-base --is-ancestor a4314fdfa master → 0
#   rework/verb-retirement-complete 这个 ref 本身已不存在（git rev-parse --verify → exit 1）；
#   判据必须是「提交是否在 master 上」，不是「分支名还在不在」。

# (2) 语义判定 —— main.go 的派发链里不再有这三个谓词
grep -n -E 'isArchiveSubcommand|isCheckpointSubcommand|isProxySubcommand' cmd/agent/main.go
#   → rc=1，零命中（三条谓词实现与调用点都已随派发链一并删除）

# (3) 真二进制判定（Rule 31；grep 只作旁证，必须真跑）
CGO_ENABLED=0 go build -o "$S/gopi" ./cmd/agent/
for v in archive checkpoint proxy; do "$S/gopi" $v -h >/dev/null 2>&1; echo "$v -h -> exit $?"; done
#   → archive -h -> exit 1
#   → checkpoint -h -> exit 1
#   → proxy -h -> exit 1
#   每一条都打印逐词指路的退役通知，例如：
#     gopi: `archive` is RETIRED — use `gopi task archive` (`gopi task archive -h` for its help).
#     gopi: `proxy`   is RETIRED — use `gopi tool proxy`   (`gopi tool proxy -h` for its help).
#   三个家族门本身仍绿：gopi task archive -h / gopi session checkpoint -h / gopi tool proxy -h → 各 exit 0
```

**结论：三谓词已成立，落地形态见 commit `92622821d`（进入 master 的合并，2026-10-06 07:40 +0800）。**
本文件第 2 节描述的前提（「本树三谓词仍在派发 / 真二进制裸动词仍 exit 0」）**在本树已为假**。

## 2. 历史论证（2026-10-06 07:06 之前有效；保留为记录，不再是最新的处置依据）

> **本节是带日期的历史记录。** 它论证的是「退役**尚未落地**时，为什么不能提前把断言改到家族地址」。
> 该论证的前提是 master tip `993ce6e5`（收尾时前进到 `55a72f0c`）、三谓词仍在 `cmd/agent/main.go` 派发、
> 真二进制裸 `gopi archive|checkpoint|proxy -h` 仍 **exit 0** —— **这些前提现在都不成立**（见第 1 节实测）。
> 保留它的理由是：它记录了「两处拼写同源」这个**仍然有效的一般性结论**（见第 4 节），
> 以及一条至今成立的纪律 —— **不要为了让条件任务「有产出」而修改静默的顶层动词**。

当时的论证原文要点：

- 本树（master tip `993ce6e5` 派生）三个谓词仍在 `cmd/agent/main.go` 派发；
  真二进制裸 `gopi archive|checkpoint|proxy -h` 仍 **exit 0**。
  此刻把断言改到家族地址会打红当时**全绿**的基线（实测 **60 PASS / 0 FAIL / exit 0**）。
- 更硬的理由：`proxyBannerPrefix`（`cmd/agent/cli_proxy.go:21`）是受测试钉死的**运行时真值** ——
  在退役未落地时改成 `"gopi tool proxy: "`，`gopi proxy --upstream …` 就会打印与自身 argv 不符的横幅。
- 因此当时交付 = 取证 + 谓词修正 + 经变异证明的清单，**零代码改动**。

**该结论的现状**：`cmd/agent/cli_proxy.go:21` 现在就是 `proxyBannerPrefix = "gopi tool proxy: "`，
与家族地址一致 —— 当时的「不能改」已经由落地本身消解。

## 3. 清单（逐条复测于 HEAD `79b29f5f1`）

原清单的每一条都在落地后的树上重新核对。**复测方法**：`cliContractInventory` 由
`go test ./cmd/agent/ -run TestCliContracts` 驱动，代理面由
`go test ./cmd/agent/ -run 'TestProxy|TestG5|TestG4|TestCompletion'` 驱动，
真二进制判定见第 1 节 (3)。

| # | 位置（原清单的 MASTER 侧） | 复测结果 | 证据 |
|---|---|---|---|
| A1 | `cli_contracts_test.go:83` `argv: CliVerbArchive` 裸 | ✅ 已改（不再是裸 `archive`） | `TestCliContracts` PASS |
| A2 | `cli_contracts_test.go:134` `argv: "checkpoint"` 裸 | ✅ 已改 | 同上 |
| A3 | `cli_contracts_test.go:135` `argv: "proxy"` 裸 | ✅ 已改 | 同上 |
| A5 | `cli_contracts_test.go:250/261` `cliContractDispatchPredicates` 含 3 谓词 | ✅ 已删（该常量现带「IT SHRANK WITH THE CHAIN (epic verb-retirement, 2026-10-05)」注记） | 常量源文本 + `TestCliContracts` PASS |
| A4/B2/B3 | `dispatcherOfToken` / help 映射孤儿条目 | ✅ 无害，未构成红 | 全量复测 0 FAIL |
| **B4** | `cli_proxy_test.go:38` `proxyProbeArgs()` 首行 `CliVerbProxy` | 🟠 **一半（见 3.1-C4）**：`proxyProbeArgs` 仍以 `CliVerbProxy` 开头，`proxyStartProbe`（:152）已统一过 `familyAddressArgs`，但 `runProxyProbe`（:132）**没有** → 该文件仍有一个子测试确定性红 | `TestProxyBannerNamesBoundSocket` / `TestParseProxyArgs*` PASS，`TestProxyBusyPortNeverClaimsListening` FAIL×2 |
| B5 | `cli_chat_web_help_surface_basic_test.go:153/154` `Name:"gopi proxy"` + `Argv:[]string{CliVerbProxy}` | ✅ 已改：`Name: "gopi tool proxy"`（:153）、`Argv: familyAddressArgs([]string{CliVerbProxy})`（:157） | 文件源文本 |
| B6 | `cli_chat_web_help_surface_basic_test.go:874/908/912/917` `runLive/run(t, CliVerbProxy, …)` | ✅ 已无害：`g5Probe.run`/`runLive` 内部统一 `familyAddressArgs(args)`（:564/:588） | 源文本 + `TestG5ProxyRunsAsAdvertised` PASS |
| B7 | 期望串 `"gopi proxy: listening on "` | ✅ 已改 → 第 888 行断言 `"gopi tool proxy: listening on "` | 源文本 |
| B8 | `cli_proxy.go:21` `proxyBannerPrefix = "gopi proxy: "` | ✅ 已改 → `"gopi tool proxy: "` | 源文本（本文件第 2 节引用的那一行） |
| B1 | `cli_completion.go` `topLevelCommandNames` 含三词 | ✅ 已删（列表现为 chat/config/memory/merge/merger/notify/session/task/tool/ui/cli…，三词均不在） | 源文本 + `TestCompletionSuppressesTheSameVerbsAsHelp` PASS |
| **B9** | `TestCompletionSuppressesTheSameVerbsAsHelp` | ✅ **已绿**（原 🔴 已消解） | 该测试 PASS |

**原清单的四个 🔴 中，B5/B6/B9 在本树已修好，B4 只修了一半（剩下的那一半就是 3.1 的 C4）。**
第 4 节「合并会踩 B4/B5/B6/B9」的警告已大部分失效，但第 4 节的**机理**仍然成立 —— 且 C4 正是它的又一个实例。

### 3.1 复测新发现（不在原清单里；都源自同一次落地）

原清单是在「合并前」的树上做的，复测在「合并后」的树上跑，因此暴露出**原清单未覆盖的残余项**。
复测命令与计数（本树 HEAD `79b29f5f1`）：

```bash
go test ./cmd/agent/ -count=1 -v -run 'TestCliContracts|TestCliProxy|TestCompletionSuppressesTheSameVerbsAsHelp|TestGroupPagePrintsTheSamePageAsItsOwnVerb|TestG4EveryAdvertisedSurfaceRuns|TestG5|TestHelpSurface|TestG4UnknownFlagRefusals|TestG4CheckpointPositionalToleranceIsDeliberate'
# → 102 PASS 子测试 / 1 FAIL 子测试；4 个顶层测试 FAIL：
#    TestG4EveryAdvertisedSurfaceRuns, TestG5InventoryMatchesTheHelp,
#    TestG5InventoryListsEveryGapTheTestsCannotCover, TestProxyBusyPortNeverClaimsListening
```

| # | 症状（实测输出） | 判定 |
|---|---|---|
| **C1** | `TestG4EveryAdvertisedSurfaceRuns/checkpoint_Usage_gopi_session_checkpoint_listexport_--dir_DIR` — 帮助页里 `gopi session checkpoint list\|export [--dir DIR]` 这一行被测试当成**一条可执行用法**，真跑 `gopi session checkpoint 'list\|export' --dir DIR` → `exit=1`，`want 0`。子测试名里的 `listexport` 正是被打平的那一行 | 🔴 **真红，需修**：或让帮助页把两种用法分行列举，或让 G4 的用法抽取器不把 `a\|b` 当选一条命令 |
| **C2** | `TestG5InventoryMatchesTheHelp` — `the inventory has no row for gopi tool proxy` **且** `the inventory has a row for "gopi proxy", which is not a surface of this group — a stale row is a claim about a surface that no longer exists` | 🔴 **真红，需修**：G5 inventory 表的行名与 gap 名没跟着 B5 的改名走（B5 改了 `Name`，**没有同步表内行名 / gap 字符串**）—— 与第 4 节「只改一半 = 双向红」是同一个机理 |
| **C3** | `TestG5InventoryListsEveryGapTheTestsCannotCover` — `the inventory does not list the gap "gopi tool proxy — the Anthropic→OpenAI translation against a real upstream provider"` | 🔴 **同一根因（C2）**：gap 串在测试里已是 `gopi tool proxy`，被断言的文档/清单里仍是 `gopi proxy` |
| **C4** | `TestProxyBusyPortNeverClaimsListening` — `cli_proxy_test.go:86: a refused start must report the bind failure, got: gopi: \`proxy\` is RETIRED — use \`gopi tool proxy\`…` | 🔴 **真红，需修（B4 的漏网兄弟）**：本文件有**两个**驱动点，`proxyStartProbe`（`:152`）过了 `familyAddressArgs`，而 `runProxyProbe` 这一条（`:132` `exec.CommandContext(ctx, bin, proxyProbeArgs(listen)...)`）**没过** —— 子进程拿到裸 `proxy`，于是打印退役通知而不是绑定失败。`-count=2` 两次都红（2.87s / 0.02s），**确定性失败，不是 flake** |

**C1/C2/C3 是同一类：** 落地把「动词地址」从裸词改成家族地址时，**断言所依据的字符串**在同一文件里有多个副本，
改了一部分、漏了一部分。这不是「清单过期」，而是「清单之外还有面」。

### 3.2 仍为真的事实（复测确认）

- `proxyBannerPrefix`（`cmd/agent/cli_proxy.go:21`）= `"gopi tool proxy: "` —— 与家族地址一致，
  原本「运行时真值 vs 断言」的对立已经消失。
- 三个家族门可用：`gopi task archive -h` / `gopi session checkpoint -h` / `gopi tool proxy -h` → 各 exit 0。
- 裸动词被**按名拒绝**，并打印指路通知（不是静默落到 agent）。

## 4. 「两处拼写同源」的真正含义（本任务的核心结论；机理至今成立）

简报说「两处拼写同源」—— 实测把它说清楚了：**同源点不止两处，而是一组四元组**：

1. `proxyBannerPrefix`（`cmd/agent/cli_proxy.go:21`）—— 运行时**产生的**串
2. G5 的期望串（`cli_chat_web_help_surface_basic_test.go`）—— 断言**读到的**串
3. G5 的 argv 入口（`g5Probe.run` / `runLive`）—— **驱动**
4. `cli_proxy_test.go` 的 `proxyProbeArgs` —— **驱动**

**只改 1、2 而不同步 3、4 = 双向红**。落地形态最后是**用 `familyAddressArgs` 把两个驱动统一**，
而不是逐处改写裸词 —— 这就是第 3 节 B4/B5/B6 全部转绿的办法，
也是 3.1 的 C2/C3 **仍然在红**的原因：**清单里的字符串副本**（inventory 行名、gap 串）
没有被 `familyAddressArgs` 这类统一入口覆盖，得单独清。

> **给后人的一般化**：一次「地址迁移」的爆炸半径 =
> **产生串 + 断言串 + 全部驱动点 + 全部清单/文档里的字符串副本**。
> 前四者可以靠一个统一函数收口；**第五类（清单、文档、注册表里的字面副本）没有编译期保护，必须手工 grep 清点**。

## 5. 超出简报警戒范围的实测发现

简报只点名了 `checkpoint` / `proxy` / `archive` 三处（+ 邻接）。复测确认：**这 14 个词不是遗留面** ——
它们都是**家族动词**或**已按名拒绝的退役词**，在帮助页里的出现是正确形态：

`completion`、`doccheck`、`vision_analyze`、`text2img`、`screen_capture`、`capture`、`act`、
`scratch-gc`、`sandbox`、`setup`、`ledger-window`、`text2img-verify`、`code-search`、`keyword_search`。

**判定方法（可重跑，逐词实测于本树）**：把每个词当裸顶层动词跑一次，
`exit 1` + 退役通知里点名的**实际家族地址** = 已退役（正确）；`exit 0` = 仍活着（需处置）。

```bash
for v in completion doccheck vision_analyze text2img screen_capture capture act \
         scratch-gc sandbox setup ledger-window text2img-verify code-search keyword_search; do
  "$S/gopi" $v -h >/dev/null 2>&1; echo "$v -> exit $?"
done   # → 14/14 exit=1
```

通知里点名的家族地址（原文，逐词实测）：

| 裸词 | 家族地址 |
|---|---|
| `completion` | `gopi config completion` |
| `setup` | `gopi config setup` |
| `doccheck` | `gopi tool doccheck` |
| `vision_analyze` | `gopi tool vision_analyze` |
| `text2img` / `text2img-verify` | `gopi tool text2img` / `gopi tool text2img-verify` |
| `screen_capture` | `gopi tool screen_capture` |
| `capture` / `act` | `gopi tool capture` / `gopi tool act` |
| `sandbox` / `scratch-gc` / `ledger-window` | `gopi tool sandbox` / `gopi tool scratch-gc` / `gopi tool ledger-window` |
| `code-search` / `keyword_search` | `gopi tool code-search` / `gopi tool keyword_search` |

即：这 14 个词**全部**已退役，`gopi tool <verb>` 与 `gopi config <verb>` 是它们共同的家族门
（`gopi tool` 承载本地工具面，`gopi config` 承载配置面）。

原清单提到的邻接测试，复测结果：
`TestGroupPagePrintsTheSamePageAsItsOwnVerb` ✅ PASS、
`TestG4EveryAdvertisedSurfaceRuns` ❌（仅 C1 一行；其余 92 条子测试 PASS）、
`TestG4UnknownFlagRefusals` ✅ PASS、
`TestG4CheckpointPositionalToleranceIsDeliberate` ✅ PASS。

## 6. 交接

- 原清单的完整逐行对照版（含每行两侧原文）产出于任务 `01a10e7a-b4ee-71a4-bc9b-3f2d6a3790c8` 的
  session 目录；**该目录已不在本机**（`~/.gopi/sessions/01a10e7a-…/` 不存在），
  本文件第 3 节的表格即其现存最完整的摘要。变异实测日志（`baseline-filter.log` 等）同已消失。
- 本次复测的原始证据（真二进制、三个退役通知的原文、测试日志）在
  `~/.gopi/sessions/01a10e98-bcd1-7931-bcd5-c6ab6a2a3508/`：`gopi`、`notice-archive.txt`、
  `notice-checkpoint.txt`、`notice-proxy.txt`、`baseline-recheck.log`。
- 触发后要做的事（第 3 节原 🔴 项）**已经全部落地，该后续行已无事可做**。
- 复测新发现（3.1 的 C1/C2/C3/C4）**不在本任务范围内**（本任务零代码改动），
  已作为独立 bug 任务提交。
