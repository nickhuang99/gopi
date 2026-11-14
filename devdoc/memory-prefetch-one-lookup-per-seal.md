<!-- doc-class: historical -->
# memory prefetch: 一次 seal 内的重复检索 —— 裁定、合并后的实现与实测

task `01a115e6-7772-7af1-ac67-5a30e57111d9`（原始任务，系统层，peerbox）
重新落到 master 的任务：`01a11648-f6a4-7d66-9e28-040d34e559ad`
分支 `task/01a11648f6a47d669e28040d34e559ad`（worktree `/home/user/workspace/gopi-01a11648f6a47d669e28040d34e559ad`）

> **本文件是重写版。** 初版（在已归档的分支
> `task/01a115e677727af1ac675a30e57111d9`，tip `fa2864a9e`）的裁定是
> 「用**覆盖关系**（`Session.Covers`）判断可否复用」。落到 master 时发现
> **sibling 分支 `01a11601` 已经先落地了自己的修复**（master tip `9ea9d75b8` 含
> `8cbddb52a`），其判据是**两个 query 字符串相等**。两个判据对同一个契约不能共存
> （Rule 13/14），本文档改为**以 master 已落地的机制为准**，并把初版的记账词汇、
> 回归测试与实测数据重新对准它。**初版被判据否决的部分，以及否决的实测理由，都在
> §3 里留档**，因为那份推理是本任务的信息，不是垃圾（Rule 23）。

---

## 0. 修订史（为什么这份文档与初版不同）

| 阶段 | 判据 | 结论 | 状态 |
|---|---|---|---|
| 初版 `ac7c472a5` | 覆盖关系（`Covers`）+ 把复用降级成 `reused-typing` **事件**，section 留给调用方 fallback 采纳 | 一次 seal 一次检索 | **判据被 master 取代**；回归测试自身有缺陷（数 `"hits":` 键，数出的是事件数） |
| 初版修订 `00bd8f552` | 同上，只把计数谓词改成按 `outcome` 数 | 同上 | 判据仍被取代 |
| **本次（合并后）** | **query 字符串相等**（master `8cbddb52a` 的 `takeShared`，本任务不改动它） | 一次 seal 一次检索 | **落地** |

初版的 `Covers`/`hasSectionFor`/`pendingTypingQuery`/`reused-typing` **不合并进 master**
——理由在 §3.2，且不是「另一个更漂亮」，是**它会破坏 master 自己钉住的两条行为**
（实测，见 §3.3）。

---

## 1. 现状：一次 seal 上有两个各自独立的检索器

`gopi chat` 的一条 prompt 在 seal 之前会被**两个独立的状态机**各判一次「要不要检索」：

| | 打字路径（typing） | 轮边界（turn boundary） |
|---|---|---|
| 驱动 | `cmd/agent/chat_prefetch.go` 的 `loop()` 每 100ms 采样编辑器已发布快照 | `cmd/agent/cli_chat.go` 在 seal 处同步调用一次 |
| 状态 | `chatPrefetch.session`（`*memoryprefetch.Session`） | `chatPrefetch.turnState`（`*memoryprefetch.State`） |
| satisfied 集 | `Session.fetched`（`internal/memoryprefetch/session.go:119` 起） | `State.Satisfied`（`internal/memoryprefetch/state.go`） |
| 记账 | `Session.Claim`（`internal/memoryprefetch/session.go:119`）/ `Use`（`:146`）/ `Take`（`:164`） | `State.Step` / `Admit` |
| 采纳点 | `chatPrefetchAdopt`（`cmd/agent/chat_prefetch.go:1393`） | `chatPrefetchAdoptTurn`（`cmd/agent/chat_prefetch_turn.go:233`） |
| 调用序 | 先（loop 早于 seal） | 后（seal 处） |

两个机器**问的是不同问题、累积的是不同历史**——`Session` 与 `State` 是两个类型，
正是因为这个原因（`session.go` 头注释）。操作者**打的**那一行会同时喂给两者：
采样器按 buffer 建键并检索，seal 处轮边界按同一批词再建一次键。

## 2. 量级：核对 brief 里的数字

`~/.gopi/sessions/*/memory-prefetch.jsonl` 全量普查（**326 行**，2026-10-07 测得）：

```
"outcome":"hits"          228
"outcome":"adopted"        52
"outcome":"gave-up"         43
"outcome":"superseded"       3
"outcome":"shared"           0
```

**真实的浪费只有一件事：每条 prompt 多跑一次打字段检索**，而它的唯一可观测后果就是那
**3 行 `superseded`** —— 不是任何比例的分母（把三种事件合成一个计数才是「65%」的来源）。
3 次是**下界**：轮边界 2026-10-07 才落地，`source` 字段只在那之后出现，而记录了完整轮边界的
那个 session 里就已经有 2 次 supersession。**每当操作者在某行上停留到去抖窗口之外，
同一条 prompt 就会检索两次。**

（`shared` 为 0：`8cbddb52a` 落地于 2026-10-07 19:57，此语料尚未覆盖它生效后的真实会话。）

## 3. 裁定：**不共享 satisfied 集；共享 section，判据是「同一个 key」**

### 3.1 共享 satisfied 集 —— 否决（这一半始终没变）

把 `Session` 与 `State` 的记账合并是**被禁止的**，理由不是审美，而且是可测的：

- 轮边界一旦把词标进 `Session` 的世界，**操作者下一次停顿**就会因为「这些词已经检索过」
  而**不检索** —— 而那次检索属于**另一个问题**（操作者在打字，模型还没看过这一轮）。
- 反过来同样成立：打字路径的差分（`Session.Claim` 的 `newKeywordCount`）会把轮边界后续的
  轮次压掉。
- 两个机器的交集恰好是**最不该合并的那部分**。

**冗余的不在记账，在检索**：同一个 seal 向同一批关键词问了同一个问题两次。

### 3.2 判据：**query 字符串相等**（master `8cbddb52a` 已落地）

`takeShared`（`cmd/agent/chat_prefetch.go:566`）在轮边界入口、**早于 `TurnStep`** 处比较
`Session.PendingQuery()` 与**轮边界自己**用同一纯函数算出的 key（`p.turnQuery(in)`，
`chat_prefetch.go:506`）：

```go
if shared, ok := p.takeShared(out.Message, p.turnQuery(in), source); ok { … }   // turn.go:254
```

三条设计约束，全部由 master 的注释与测试陈述并被本任务沿用：

1. **判据是 key 的相等，不是 key 的前缀。** 轮边界的存在意义就是采样器**看不到的那四张脸**
   （`notify` 轮、整块 paste、`/queue` 的 prompt、模型自打的行），它们的 key **按构造就不同**。
   所以「这一轮已经在打字时检索过」**恰好等价于**两个 key 相等。
2. **相等之后由 `Session.Take` 消费并把 section 交给这一轮**（`takeShared` 里
   `p.session.Take(line, p.cfg)` + `out.Message += separator + shared`，记 `outcome=shared`）。
   section 被轮边界**取走**，调用方的 fallback 因此取不到第二次
   （master 钉死：`chat_prefetch_shared_test.go:TestTheSharedSectionIsNotAdoptedTwice`）。
3. **保守方向**：比较的是整串，所以只是**重叠**的 key 仍然自己发查询
   （`TestASharedKeyNeverSuppressesADifferentQuestion`）；被 lift 的（前缀）key 的 section
   对 sealed 行**不被采纳**（`TestALiftedKeyNeverCoversTheSealedLine`）。

### 3.3 为什么初版的「覆盖关系」判据**不能**合并进来（实测，不是偏好）

初版让轮边界**只探询、不消费**（`Session.Covers`），命中就记一行 `reused-typing` 并**返回
「未采纳」**，把 section 留给调用方的 fallback 去采纳。**这与 master 已经钉死的机制正面冲突**：

- master 的 `takeShared` **消费** section（否则 `Session` 会一直持有一份已经用过的 section，
  且后续轮次会拿到过期 section）。因此 master 的 fallback **取不到**它，
  `TestTheSharedSectionIsNotAdoptedTwice` 断言的正是「不能被采纳第二次」。
- 初版路径若保留为另一条分支，就会出现**两套机制处理同一个契约**：key 相等时到底走
  `takeShared`（消费、由轮边界采纳）还是 `hasSectionFor`（不消费、由 fallback 采纳）？
  两者对同一对 key 会给出**不同的记录与不同的消费语义** —— 这正是 Rule 13/14 禁止的
  「一个契约两个谓词」。

**因此本次合并的裁定是**：判据只有一处（`takeShared` / `p.turnQuery`），记账词汇只有一处
（`prefetchOutcomeShared`）；初版的 `Covers`/`hasSectionFor`/`pendingTypingQuery`/
`prefetchOutcomeReusedTyping` **不引入**——`prefetchOutcomeReusedTyping` 与
`prefetchOutcomeShared` 会是**同一事件的第二个名字**。

### 3.4 一个诚实的边界情形（初版 §3.3 保留）

**打字 lookup 在 seal 时仍在飞行中**：`Session.section` 尚为空，`PendingQuery()` 可能已有
`fetched`（`Claim` 先写 `s.fetched`）但 `Covers`/`Take` 要求的 `section != ""` 不成立 →
轮边界**自己检索**。对「操作者那一行」而言至少一次检索发生过，契约（同词集一次）仍成立；
代价是这一种竞态下仍可能两次。**这是 master 判据的既有边界，本任务不改**，如实记录。

---

## 4. 实现（合并后 master 的形态，本任务未改生产代码）

| 文件 | 承载 |
|---|---|
| `internal/memoryprefetch/session.go:193` | `PendingQuery`：section 的 key（`sectionQuery`，未取时回落到 `fetched`） |
| `internal/memoryprefetch/session.go:164` | `Take`：采纳谓词（存在 section 且 sealed key 无新词），**消费** |
| `cmd/agent/chat_prefetch.go:506` | `turnQuery`：轮边界自己的 key，与 lookup 用同一个纯入口 |
| `cmd/agent/chat_prefetch.go:566` | `takeShared`：`PendingQuery()==query` 才交 section；记 `outcome=shared` + `adopted=yes` + `section_bytes` |
| `cmd/agent/chat_prefetch.go:147` | `prefetchOutcomeShared = "shared"`（Rule 1，唯一词表） |
| `cmd/agent/chat_prefetch_turn.go:233` | `chatPrefetchAdoptTurn`：入口先判共享，命中即采纳并返回 |
| `cmd/agent/chat_prefetch_turn.go:344` | `discardTypingSection`：**master 仍保留**（key 不等的路径上，打字 section 确实被顶掉，记 `superseded`，Rule 15）。因此本任务的可见性代价只发生在**key 相等**的路径上——而那正是本任务要消掉重复的那些 seal |
| 本任务新增 | `cmd/agent/chat_prefetch_reuse_test.go`（回归；见 §6） |

**改动列表为空是结论，不是遗漏**：master tip 已含 `8cbddb52a` 的完整修复，本任务在
生产代码上没有**必须**改的东西（§3.3 解释了为什么初版的生产改动是**回归**而非补齐）。
`internal/memoryprefetch/session.go` 因此**不新增 `Covers`**——一个没有真实调用方的
记账方法违反 Rule 3/35 的依赖预算（deliverable 3 的要求：「只在有真实调用方时保留」，
这里没有）。

## 5. 实测（Rule 31）

**真跑的是本文件的回归测试所驱动的生产接线**（与真实 run 同一条缝：`chatPrefetchAdoptTurn`
→ `takeShared` → `turnQuery` → `Session.Claim/fetch/Take`），计数读的是 **run 自己的记录
文件** `memory-prefetch.jsonl`。记录形状，两条 run：

**改动前**（把 `8cbddb52a` 的**生产 hunk 反向应用** = master 的 `chat_prefetch.go` /
`chat_prefetch_turn.go` 退回 `8cbddb52a^`，测试文件**原样不动**）：

```
--- FAIL: TestOneSealCostsExactlyOneLookup
    chat_prefetch_reuse_test.go:118: the same seal performed 2 lookup(s) — the turn boundary fetched for keywords the typing path already had
--- FAIL: TestTheTwoKeysAreOneQuestionHere
    chat_prefetch_reuse_test.go:178: the record's one retrieval key
    ["notify delivered retrieval key reaches turn boundary" "notify delivered retrieval key reaches turn boundary"]
    is not the pause's own "…" — the seal asked a second, different question
```

→ **同一个 key 被检索两次**（两条 `outcome=hits`，query 逐字相同），这正是本任务的缺陷。

**改动后**（master tip）：四条全绿（`ok github.com/nick/gopi/cmd/agent 6.710s`），
`TestOneSealCostsExactlyOneLookup` 断言同一序列**只有 1 条 lookup 行**，
外加 1 条 `outcome=shared`（`adopted=yes`），**0 条 `superseded`**。

反向应用的机制与可重复性：`git diff 8cbddb52a^ 8cbddb52a -- cmd/agent/chat_prefetch.go
cmd/agent/chat_prefetch_turn.go | git apply -R`（`--check` 先过）。反向补丁删掉
`prefetchOutcomeShared` 常量，而 `chat_prefetch_shared_test.go`（master 的既有测试）仍引用
它，故实验期临时放了一个**测试文件内**的常量垫片（`zzz_redproof_shim_test.go`），**跑完即删**、
未提交——它只补编译，不改变任何被测行为（两条红都发生在 `takeShared` 存在的意义上：
它被删掉了，所以没有任何共享）。

## 6. 回归测试：`cmd/agent/chat_prefetch_reuse_test.go`

| 测试 | 钉住的行为 |
|---|---|
| `TestOneSealCostsExactlyOneLookup` | 一次 seal（打的整行 + 停顿 + 回车）**只有 1 次检索**；记 1 条 `shared`；**0 条 `superseded`**；轮边界**消费**掉 section 后调用方 fallback 取不到第二次（section 不会重复进入 prompt） |
| `TestTheTwoKeysAreOneQuestionHere` | 判据本身：记录里那一条 retrieval key **就是** pause 自己的 key（相等，不是重叠） |
| `TestANotifySealWithNoTypingSectionStillPaysItsOwnLookup` | 采样器看不到的那张脸（`notify`）**照旧自己检索一次**，且**不出现** `shared` |
| `TestTheLookupCountCountsLookupsNotEvents` | 计数谓词自身的红证明：只带 1 次检索 + 1 条 shared + 1 条 superseded 的记录必须数出 **1**（数 `"hits":` 键会数出事件数——初版就是这么红的） |

**计数谓词**：`prefetchLookupOutcomes = {hits, empty, gave-up, error}`——**只有这四个 outcome
表示「真跑了查询」**；`adopted` / `shared` / `superseded` 是**关于**一次检索的事件，不计数
（Rule 1/13：命名常量，不是散落的字面量）。这是**按 outcome 计数**，与 master
`chat_prefetch_shared_test.go` 的 `countOutcome` 同一读法。

**改前红 / 改后绿**：红 = 上节的两条 FAIL（只反向应用**生产**文件，测试原样不动）；
绿 = master tip 全绿。**测试不依赖另一个分支的交付**（初版曾依赖
`task/01a115ce420973bfa159f631a3a2cfaa` 的 `c41a94056` 把 `hits` 改成 `*int`——本版改成按
outcome 计数后，与该任务合并前后**得到同一个数**）。

**未重复 master 已钉的行为**：`chat_prefetch_shared_test.go` 的 6 条（前缀 section 不服务更长的
turn、共享键不压制不同问题、section 不被采纳两次、被 lift 的 key 不覆盖 sealed 行、prefix key
的 verdict、开关关闭时惰性）**保持原样不动**，本文件不重述——重复会把一份契约变成两处判据
（Rule 13/14）。本文件补的只有那一条它们只能**间接**覆盖的读数：**key 相等时的 lookup 计数**。

## 7. 未做（诚实）

- **未引入覆盖关系判据**（§3.3：它与 master 已落地的相等判据是同一契约的第二个谓词，
  且会破坏 master 钉住的消费语义）。
- **未删除 `discardTypingSection`**（§4：key 不等的路径上它仍在如实记账；删掉会让
  `superseded` 的语义漂移，但那是另一个行为，不在本任务范围内）。
- **未改 `hits`/`ms` 的记录形态**：已由 `task/01a115ce420973bfa159f631a3a2cfaa`
  （`c41a94056`，`*int` + `omitempty`）落地在 master，本任务不重复做。
- **§3.4 的竞态未做额外处理**，已如实标注。
- 语料样本仍极小（`superseded` 3 行），§2 据此只声称「下界」。
- 未做真实 pty 端到端 run：本任务的验收口径是 `make test-budget` + 回归测试 + 构建，
  真 pty run 属原始任务（`01a115e6`）的交付，已在归档分支的报告中留档。
