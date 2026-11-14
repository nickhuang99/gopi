<!-- doc-class: historical -->
# 落地判据普查 — 四个读"交付是否落地"的站点

**测量基点**：`161f48a`（本 ground 的 HEAD 与 `origin/master` 同点，实测
`git rev-parse --short HEAD` = `git rev-parse --short origin/master` = `161f48a`）。
**读法**：判据一律从对象库读，不读工作树（epic 实例二）——
`git grep -n ... origin/master -- internal/tools`、`git show origin/master:<path>`。
工作树里只有本 run 改动的 `internal/tools/archive_landed.go` 与
`internal/tools/archive_backlog_classify.go` 与 HEAD 不同，其余站点的工作树副本
与 `origin/master` 逐字节相同（`git status --porcelain` 只列这两个文件 + 一个新测试）。

这是一份**普查**，不是规范：它数的是"今天有几个人在回答同一个问题、各自用什么"，
所以它带 base commit（Rule 38）。行号是**该 base 上的行号**，前移之后以符号名为准。

## 0. 问题是什么

"这个分支的交付落地了吗"这个问题，在四个地方被回答，四次各自实现。一晚四次误判
（哈希 / 标题 / 工作树 / 行状态）是同一个根：**只有内容指纹能给真答案**，而便宜的
替代判据（祖先关系、commit 主题、路径存在、`rev-list --count`）各自有各自的失效面。
四个站点的现状：

| # | 站点 | 判据 | 该 base 的 file:line |
|---|---|---|---|
| ① | `TaskDeliveryProof`（收尾链） | 三臂：ancestry → 内容归一化 → clone channel 的 landed-record | `internal/tools/task_delivery_proof.go:560`（臂1）、`:853`（臂2 调用点）、`:632`（臂3） |
| ② | `archive_landed.go`（`gopi task archive --check`） | ancestry → **路径存在** → 内容 | `internal/tools/archive_landed.go:326`（臂1）、`:333`（臂2）、`:348`（臂3，**本 run 新增**） |
| ③ | `worktree_sweep_undelivered.go`（清扫/交接） | `git rev-list --count` + 两条"等价"腿 | `internal/tools/worktree_sweep_undelivered.go:207`、`internal/tools/worktree_sweep_undelivered_equivalent.go:235`（内容腿）、`:350`（扣除） |
| ④ | `rework_landing_probe.go`（返工探针） | 祖先关系 + **commit 主题** + 内容佐证 | `internal/tools/rework_landing_probe.go:270`（Signal）、`:414`（内容臂调用点） |

## 1. 站点 ① — `TaskDeliveryProof`（收尾链，判据族的"真值"）

**它怎么判**（`task_delivery_proof.go:738` 起）：

1. 臂1 `deliveryProofTipContained`（`:560`）：tip 是不是目的 ref 的祖先 —— 便宜且
   无假阳性的方向；
2. 臂2（`:853`）调 `deliveryProofContentLanded`
   （`task_delivery_proof_content.go:436`）：本 run 新增的**行**是否在目的树里，
   经 `deliveryProofNormalizeLines`（`:217`）归一化（丢空行、纯注释、
   `measured-at:` 元数据），命中判定 `deliveryProofLinesMissingAt`（`:363`）；
3. 臂3 `deliveryProofLandedRecorded`（`:632`）：clone channel 自己记的"这次 push
   落地了、带的就是这个 tip"。

**谁调用它**：`deliveryProofRefusal`（`:1001`）—— 一个调用者，收尾链写回前的那道门。
`deliveryProofRefusal` 之外，`TaskDeliveryProof` 只被测试调用（
`task_delivery_proof_*_test.go`）。

**失效场景**：无（在本 base 上）。
- rebase 改哈希、内容不变 → 臂1 说不算、臂2 说算（`landing_criterion_probe_test.go`
  的 `TestLandingCriterion_AReplayedCommitIsLandedThoughItsHashIsNot` 实测打印
  `hash probe: NOT landed (false negative) — patch-id probe: landed`）；
- 同主题不同改动 → 臂2 拒绝（`TestLandingCriterion_ASharedSubjectIsNotAnIdentity`
  实测 `subject probe: LANDED (false positive)`）；
- 真没投递 → 三臂全否（`TestLandingCriterion_TrulyUndeliveredWorkFailsAllThreeArms`）。

**真实天花板**：它是**私有的、要一行 DB 才能跑**（`TaskDeliveryProof(taskID)` 自己去
解析 row 的 deliverable / ground / destination）。这就是 epic 缺口①的准确形状：不是
判据不对，是**判据不是一个可以被别人调用的东西**。

## 2. 站点 ② — `archive_landed.go`（本 run 修理的站点）

**它原来怎么判**（`HEAD` 的 `archive_landed.go`）：祖先 → **路径存在**
（`ArchiveLandProbe`，`:333` 的 `missing := probe(tip)`）→ 完。设计者自己在头注释里
记了这个选择的代价：`name test 42 → path presence 33 → blob check 6`，也就是说它
ship 了三个候选里**最弱**的那个，并把这个"弱"写成了"够用"。

**缺陷（实测，不是推断）**：路径存在对"**只改行、没加路径**"的分支是**构造性失明**。
`-return oldFlag` / `+return newFlag` 在一个两边都有的文件里：`missing` 空集，
verdict 直接读 LANDED。红证（`internal/tools/zzz_archland_redproof_test.go`，
临时文件，测得后即删，输出见 §5）：

```
RED PROOF: verdict = "landed", want "missing" — the branch's line is nowhere on
refs/heads/master (missing paths [])
```

新树对同一个 fixture 通过。两个必须同时成立的形状（`archive_landed_content_test.go`）：
- `feat/modified-only`：整条改动没落地 → MISSING（修前 LANDED）；
- `feat/other-change-same-path`：同一路径同一行是**另一个值** → MISSING（修前 LANDED）；
- `feat/rebase-same-content`：rebase 后哈希全变、内容不变 → **必须 LANDED**
  （防止修理把"非祖先"误判成"没落地"，这正是 2026-09-18 那 33 条假阳性的形状）；
- `feat/adds-path`：新增文件 → 仍由**路径臂**决定，verdict 里不得出现"缺失行"
  （否则就是错的仪器在说话）。

**修法（Rule 35：不新建机制）**：`archiveLandContentLanded`（`:394`）只**复用**
`deliveryProofCommitPaths` / `deliveryProofCommitAddedLines` /
`deliveryProofNormalizeLines` / `deliveryProofBlobText` / `deliveryProofLinesMissingAt`，
不写第二个比较器。arms 的分工按代价：一条 `merge-base --is-ancestor` 每分支；
新增路径一次批量 `git log --no-walk --diff-filter=A`（整个 miss-set 一次调用，不是
每次）；**逐 commit 的内容读只在路径臂 settle 不了的时候跑**，且被
`archiveLandContentScanCommits` 封顶（超出即 UNMEASURED，不猜）。
这就是它只能上显式报告面（`gopi task archive --check`）、**必须不接进
`gopi merge --status`** 的原因（Rule 27：后者每个 merger tick 都跑）。

**读不出 ≠ 没落地**：内容臂答不了（merge-base 读不出、commit 数超封顶、某 commit
的路径列不出来）时 verdict 是 `unknown`，不是 `missing` —— 路径臂已经说过"没有路径
缺失"，此时判 missing 就是凭空制造一次 re-dispatch，判 landed 就是发出本臂要消灭的
那个假判决。

## 3. 站点 ③ — `worktree_sweep_undelivered.go`（清扫 + 交接）

**它怎么判**：`git rev-list --count <branch> --not <每个能解析的投递点>`
（`worktree_sweep_undelivered.go:207`）。这个站点**已经被修过一次，而且修得对**：
`rev-list` 数出来的是"多少个 commit 不在投递点"，而 ground 里那份
`refs/remotes/origin/master` **从来没 fetch 过**（实测：208 个 clone ground，206 个
带着过期快照；相差 12h54m），于是"2 个 commit 谁都还没拿到"这句话对**已经在 master
上的工作**也成立，且**永不过期** —— 那条 keep 句子永久为真，ground 永远收不回来，
row 卡在 `in_progress`（bug 行 `01a17d1b`）。

**修法**：判据不动（"带着投递点没有的 commit 就必须 keep" 是对的），改的是**问题在
哪里问**：从"我站着的这个 repo 的主检出"读集成 tip，然后问两条等价腿 ——
① 主题相等（`reworkLandingSubjectCarriesSignal`）、② 内容包含
（`worktreeUndeliveredContentContained`，`:235`）。两条腿任一成立即算已投递。

**它与站点 ① 的关系**：站点 ③ 的"内容包含"是**自己实现的**（自己的 diff 解析
`worktreeUndeliveredParseDiff`:292、自己的逐行 trim 比较），不是复用
`deliveryProofLinesMissingAt`。这是**一个仍在的判据副本** —— 它今天没有失效证据，
但它是"同一个问题两份实现"的现存实例，属 peerbox（系统层的清扫/交接），本 run 不动它。

**方向性**：所有读不出来的输入（git 失败、diff 解析不了、超 cap、fresh tip 没有这个
文件）都让腿**拒绝点火**，于是 commit 继续被计数，于是 ground 被 keep。这个"错的方向
只能是保守"是它的安全论证，也是它不能顺手改成 agressive 的原因。

## 4. 站点 ④ — `rework_landing_probe.go`（返工探针）

**它怎么判**：祖先关系（`reworkLandingAncestryProof`:430）+ **commit 主题**相等 +
内容佐证（`Signal()`:270）。内容佐证**复用**站点 ① 的
`deliveryProofContentLanded`（`:414`），不写第四个包含测试。

**失效场景与被处理的形状**：主题是**名字**，两个 commit 可以同名不同改动 ——
于是"主题匹配"单独成立时会误报"master 上可能已经有这个改动"。修法是
`Signal()` 里的 `return !(e.ContentJudged && !e.ContentLanded)`：内容臂答了且说
"不是这个改动"→ 不算信号（给出 refuted 段，不冒充 signal）。内容臂**答不了**时信号
保留 —— 名字匹配是真证据，读不出的问题不是反驳（Rule 15）。
`reworkLandingContentUnjudgedLine` 把这件事写进渲染文本：读者必须能区分
"内容佐证过"和"名字对上了但没人看字节"。

**它与站点 ① 的关系**：**已经是对的**，也是本 epic 里"复用而非重造"的既有样板 ——
本 run 修站点 ② 时照的就是这个先例。

## 5. 逐条对齐 epic 的四项建议

| # | 建议 | 状态 | 证据（本 base 实测） |
|---|---|---|---|
| ① | 判据必须是**一个可调用**的东西，被每个读落地状态的地方复用 | **本 run 补了半边；整条仍开放** | 内容归一化臂 `deliveryProofContentLanded` / `deliveryProofNormalizeLines` / `deliveryProofLinesMissingAt` 是可复用的**函数**，现有三个消费者（①:853、②:348、④:414）。但**整条判据**仍然要一行 DB（`TaskDeliveryProof(taskID)`），所以站点 ③ 的内容腿是自己实现的副本。把它做成"无 DB 的 ONE callable"要动 row/deliverable 解析面 = 系统层 → **peerbox** |
| ② | 收尾链必须用它、且只有一处用它 | **已达成** | `deliveryProofRefusal`（`internal/tools/task_delivery_proof.go:1001`）是 `TaskDeliveryProof` 的唯一生产调用者，而它本身只有一个生产调用点 `internal/tools/tasks.go:5951`（`git grep -n "deliveryProofRefusal(" origin/master -- internal/tools cmd | grep -v _test` 实测：生产代码 2 行 = 定义 + 这一个调用） |
| ③ | rebase 改哈希 / 内容不变 → 仍然算落地，且有 fixture 钉死两个方向 | **已达成**（本 run 又加了一组） | 已有：`landing_criterion_probe_test.go`（replayed commit / 同主题不同改动 / archive tag / 重导出的 rebase / 真没投递）与 `task_delivery_proof_rebase_content_test.go`。本 run 新增 `archive_landed_content_test.go`：`_RebasedBranchStaysLanded`（先断言"确实不是祖先"，再断言 LANDED）+ `_ModifiedOnlyBranchIsNotLanded`（两个 MISSING 形状） |
| ④ | "没内容可投" 与 "pull 被拒" 必须是两个 kind，各带实测计数 | **已达成** | 两个 kind 是分开的常量：`BlockerKindEmpty`（`internal/devworkspace/delivery_anchor.go:55`）、`BlockerKindPull`（`internal/devworkspace/devworkspace.go:302`），且各有自己的 decisive-lines 渲染（`internal/devworkspace/decisive_lines.go:88` 起）。"各带实测计数"本 run 未逐个复核其计数渲染，标为**未测**，不冒充已验证 |

## 6. 仍在开放、且属 peerbox 的部分（如实）

1. **站点 ③ 的内容腿是第二份实现**。它今天没有失效证据，但它是"一个问题两份判据"的
   现存实例，和 epic 的缺口①是同一件事的另一个面。归并它要动清扫/交接的判定面
   （系统层）。
2. **"ONE callable 落地判据"** 需要把 row/deliverable/ground 的解析从判据里剥出来，
   成为"给定 (tip, 目的 ref, 两侧目录) → 三臂答案"的纯函数，并让四个站点全部调它。
   那是 worktree/ledger/收尾链的机制面 —— Rule 28 归 peerbox。本 run 只把**可复用的
   那一层**（内容归一化 + 行包含）做到了"三个消费者共用一个实现"，并在站点 ② 上
   证明了这条路走得通。
3. **代价约束不是口味**：内容臂读的是 commit 历史，只能上显式报告面。任何把它接进
   tick 的改动都要先回答"每 tick 每个 ground 多读几次 git"（Rule 27）。

## 7. 复现方式

### 7.0 真跑证据（Rule 31：改动的入口真的执行过，不是只过编译）

`gopi task archive --check` 就是这个判据的**唯一生产入口**，用真参数真仓库跑过两次
（sandbox `sandbox-r31-ca6c66f9`，仓库是本次生成的一次性 fixture，不碰任何真实账本）：

```
$ gopi task archive --check --repo <sandbox>/demo-repo          # 形状 A：只改行、没落地
  2 branch(es): 2 reason-less, 0 decided-owner, 1 not-landed, 0 decided-and-not-landed
    feat/mod-only  — no reason recorded — (none) — NOT LANDED
      work absent from refs/heads/master in flag.go: func Flag() bool { return true }
    feat/rebase    — no reason recorded — (none) — landed
```

（第一轮只有 `feat/mod-only` 一个 tag 时，同一命令的输出是
`1 branch(es): … 1 not-landed …` + `feat/mod-only … NOT LANDED` +
`work absent from refs/heads/master in flag.go: func Flag() bool { return true }`。）

判决与证据都来自被修的这条腿：**同一路径、只改了行、master 上没有那个值** → NOT LANDED，
并且把缺失的**那一行**引出来（修前这里读 `landed`，见 §2 的红证）。

```
$ gopi task archive --check --repo <sandbox>/demo-repo          # 形状 B：rebase，内容不变
  branch tip=0af8fc95a79c…  master=50e422c7704c…（not an ancestor —— 祖先臂答不了）
  git show <tip>:extra.go | sha256sum  ==  git show master:extra.go | sha256sum
  2 branch(es): 2 reason-less, 0 decided-owner, 0 not-landed, 0 decided-and-not-landed
    feat/mod-only  … landed
    feat/rebase    … landed
```

两条方向在**同一个 CLI 入口**上同时成立：非祖先 + 内容在 → landed；非祖先 + 内容不在 →
NOT LANDED 并点名缺失的那一行。

```bash
# 站点 ② 的两个方向（branch-scoped，不跑全树）
go test ./internal/tools/ -run 'TestArchiveLandContent_' -count=1 -v

# 判据族的既有回归
go test ./internal/tools/ -run 'TestLandingCriterion_|TestDeliveryProofContent_' -count=1

# 本普查自己的行号（从对象库读，不读工作树）
git grep -n "func TaskDeliveryProof\|func deliveryProofTipContained\|func deliveryProofLandedRecorded" origin/master -- internal/tools/task_delivery_proof.go
git grep -n "func deliveryProofContentLanded\|func deliveryProofNormalizeLines\|func deliveryProofLinesMissingAt" origin/master -- internal/tools/task_delivery_proof_content.go
git grep -n "func worktreeUndeliveredContentContained\|func worktreeUndeliveredSubtractEquivalent" origin/master -- internal/tools/worktree_sweep_undelivered_equivalent.go
git grep -n "func (e ReworkLandingEvidence) Signal\|deliveryProofContentLanded(" origin/master -- internal/tools/rework_landing_probe.go
```
