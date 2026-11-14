<!-- doc-class: historical -->
# Task Report: memory-prefetch — 前缀键(P)与轮边界完整键(Q)：判定为设计，并把它钉成测试（task 01a1163c）

**Branch**: `task/01a1163c10d37629b577260cc54e7878`
**Deliverable**: `cmd/agent/chat_prefetch_shared_test.go`（钉住设计 + 红证）+ 本报告。
**判定的两句话**: (1) 打字路径在**停顿点**对 P 检索，是在 P **就是整行**的时刻回答一个完整问题 —— 不是轮边界那次检索的副本，而且**无法被抑制**（判据只能是时间，而时间判据被本特性明令禁止）；(2) 因此**不**把共享判据放宽到"前缀关系" —— 轮键携带打字段从未持有的词，共享即注入一个不覆盖本轮词的表，这正是该特性存在的理由所要防的。

## Completed

### 1. 判定：设计，不是缺陷 —— 且缺陷/设计的边界由代码自己的判据划线

- **代码库的差分判据本身就是"前缀 ≠ 同一问题"**（`internal/memoryprefetch/session.go`）：`newKeywordCount` 的注释原文 —— "`alpha beta` against `alpha beta gamma` is a different one (1 new keyword) even though it contains the first completely"。这不是我为判定找的托辞，而是被测代码的定义。
- **两道闸门都建立在这条判据上**，一进一出：
  - `Claim`：行增长后**必须**再检索（`newKeywordCount(s.fetched, query) < DefaultMinQueryDelta` → 拒），注释明说是防"行长大了却被当作同一问题"。
  - `Take`：封印时**必须**拒绝前缀段（`newKeywordCount(s.sectionQuery, query) != 0` → 拒），注释原文 "A line the operator kept typing on is a different question, and the honest answer for it is 'no section', not a slightly older one"。
- **P 的那次检索为何不能抑制**：打字路径的触发条件就是"行在 `DefaultDebounceMS` 内未变"（`Observe`），即**停顿就是触发**。触发那一刻 P 是操作者已经打完的全部内容 —— 一个完整、可回答的问题；"行之后会不会再长"在当时不可知，而唯一能前瞻的判据（更长的窗口 / "仍在变"标志）是**时间判据**，本特性禁止（迟于停顿触发的检索已经错过 Enter）。
- **代价与收益对称**：P 的段在封印时被 `Take` 正当拒绝；`Session.Use` 的"同一台机器的记忆只保留最后一次 fetch 的答案"使 P 的段在打字路径上就被**取代**，因此它不会产生任何 adopted 记录 —— 这笔钱只花在"停顿后按 Enter"的那种输入上（那时 P=Q，轮边界直接共享，见下）。
- **与 01a11601 的关系是互补而正交的**：01a11601 移除的是**逐字相等键**上的重复检索（`takeShared` 要求 `PendingQuery() == turnQuery`）；本行的 P≠Q 形态不在其覆盖范围内，也**不应**被它覆盖 —— 覆盖它就是把"键相等"放宽成"键是前缀"。

### 2. 红/绿（Rule 10.5）+ 真跑取证（Rule 31）

`cmd/agent/chat_prefetch_shared_test.go` 6 个用例驱动**真实接缝**（`chatPrefetchAdoptTurn`、`chatPrefetchAdopt`、`chatPrefetchTurnTerms`、`Session.Claim/fetch/Take`、`memoryprefetch.Query`）：

| 用例 | 断言 | 状态 |
|---|---|---|
| `TestATypingPauseOnAPartialLineIsPaidForAtThatPause` | 停顿 + 打完整 + Enter → 2 条**不同键**的 `hits`，恰好 1 条 `shared`；P 的段从不出现 adopted | 绿 |
| `TestALiftedKeyNeverCoversTheSealedLine` | P 的段**不可**被封印行采纳（`Take` 拒），且轮边界仍跑**自己**的检索；记录里两把键按序出现、0 条 `shared` | 绿 |
| `TestAPrefixFetchedSectionNeverServesAFullerTurn` | 同一拒绝，及其对照：P 的段对 P 自己仍是答案（特性不是惰性） | 绿 |
| `TestASharedKeyNeverSuppressesADifferentQuestion` | 轮键含打字段没有的词 → **必须**自己检索（hits=2、shared=0）：保守方向 | 绿 |
| `TestTheSharedSectionIsNotAdoptedTwice` | 共享后 `chatPrefetchAdopt` 不能再采纳一次 | 绿 |
| `TestSharedPrefetchIsOffWhenTheSwitchIsOff` | OFF 路径不变 | 绿 |

**红证（不是推断）**：把"共享判据"按本行提案**真的放宽**（`PendingQuery` 关系由相等改为前缀），或把 `Take` 的覆盖判据**真的弄成惰性**，测试即转红：

```
# 放宽为前缀（production 实改，再跑）：本行 3 个断言仍绿 —— 因为 takeShared 之外
#   还有 Take 这道闸门在拒绝前缀段；这本身是结论的一半：放宽共享**不会**让前缀段
#   进入提示，只会让轮边界静默跳过自己的检索、从而不注入任何段。
# Take 的覆盖判据惰性化（newKeywordCount(...) != 0 → < 0）：
--- FAIL: TestAPrefixFetchedSectionNeverServesAFullerTurn (0.02s)
    chat_prefetch_shared_test.go:250: a section fetched for the PREFIX was handed to the fuller line (237 bytes): the turn's own terms were never retrieved for it
--- FAIL: TestALiftedKeyNeverCoversTheSealedLine (0.01s)
    chat_prefetch_shared_test.go:361: the lifted key's section was adopted for the sealed line (237 bytes): the turn's own terms were never retrieved for it
FAIL	github.com/nick/gopi/cmd/agent
```

**真跑（Rule 31）**：真 `bin/gopi chat` 挂 pty，逐字打、中途真停顿（> 600ms 去抖）再打完 + 一次 Enter，见 `<session-dir>/memory-prefetch.jsonl`：基础树 `2 x outcome=hits + adopted:yes`（打字段）；本分支（带 01a11601 的逐字相等共享）同序列 `2 x outcome=hits + adopted:yes`，两把键 `worktree base tip master`（打字段，无 source）与 `worktree base tip master fetch`（轮边界，source=human）—— 即 P 与 Q **各检索一次**，且 P 的段**无** adopted 行。证据文件：`evidence/probe-PREFIX-MINE.jsonl`、`evidence/probe-BASE-1..3.jsonl`、`evidence/probe-BURST-MINE-1..5.jsonl`。

## Not Done

- **没有"让 hits 数下降"的修复** —— 这是本行的**结论**而非遗留：可下降的两种改法都被证伪（放宽共享 → 轮边界静默无段；抑制打字检索 → P 提问时无判据可依，只能靠时间判据）。已录 164 事件中"打字段严格前缀(T-Q)=0"说明真实形态里 P≠Q 的重复**尚未被观察到**，本次由真跑构造。若未来仍要减少这笔花销，唯一未探索的方向是**让 `Take`/adoption 对"前缀段的结局"更诚实**（现在它只是被 `Use` 静默取代）；那是记录语义的改动，不改变检索次数，本行未做。
- 未触动 `superseded` 机制、未改 `cli_chat.go` 的互斥分支（01a11601 的领地，且其契约在本行中被测试引用而非修改）。

## Verification

- Build: `go build ./cmd/agent/` → OK（含放宽/惰性化两次临时改动后的重建，均已复原）
- Tests: `go test ./cmd/agent/ -run 'TestATypingPauseOnAPartialLineIsPaidForAtThatPause|TestALiftedKeyNeverCoversTheSealedLine|TestAPrefixFetchedSectionNeverServesAFullerTurn|TestASharedKeyNeverSuppressesADifferentQuestion|TestTheSharedSectionIsNotAdoptedTwice|TestSharedPrefetchIsOffWhenTheSwitchIsOff' -v` → 6/6 PASS
- Ran: pty 真会话 `bin/gopi chat`（见上 `evidence/probe-PREFIX-MINE.jsonl` 等）
- Result: 打字段与轮边界各一次 `outcome=hits`、键分别为 P 与 Q；`outcome=shared` 仅在 P=Q 的序列出现
- Commit: `0173ade12`（等价的 01a11601 改动：一行只检索一次 / takeShared + 夹具）+ `5aab0d7f7`（本行的 6 个用例与判定报告）

## Evidence (self-filed)

- `go test ./cmd/agent/ -run TestALiftedKeyNeverCoversTheSealedLine`（Take 惰性化后）→ `--- FAIL ... the lifted key's section was adopted for the sealed line (237 bytes)`，复原后同一命令 → `ok`
- `git status --porcelain`（worktree）→ 空
- 记录文件读数：`probe-PREFIX-MINE.jsonl` 5 行 = 2×hits(不同键) + 2×adopted + 1×shared 之外无第七种 outcome
