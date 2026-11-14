<!-- doc-class: historical -->
# Task Report: pending 队列的顺序诚实性 — 显示序 = 认领序 (FIFO within tier)

分支：`fix/pending-queue-order`（worktree `/home/user/workspace/gopi-pending-order`，基于 master `f2daebad`）

## 报告的问题（operator, 2026-09-15）

> "I have observed the newly inserted task is displayed at top in FILO order which can
> starve the earliest task which might be the most urgently needed ones. Is it possible
> to make the order of execution of pending task as FIFO?" (附件：`task_pending_order.txt`)

## 调查结论（5-why，先定根因再改）

把报告拆成两个独立事实，**排除了"执行顺序是 LIFO"这个假设**：

| 面 | 代码位置 | 实测顺序 |
|---|---|---|
| **执行序**（谁被派发） | `TaskNextFiltered` — 唯一认领漏斗（worker round / serve dispatch / `task run next` 三路共用），`internal/tools/tasks.go` | `ORDER BY priority ASC, created_at ASC, id ASC` → **已是 FIFO（同 tier 内最老先跑）** |
| **显示序**（操作员看到什么） | `TaskList`，同一文件 | `ORDER BY created_at DESC, id DESC` → 最新在最上 |

实测（真实 ledger，改前）：

```
显示首行 = 01a0a429 (created 16:22:33, 最新)
执行首行 = 01a0a3a8 (created 14:02:18, 最老)
```

**根因不是调度器，而是"显示序与执行序相反"**：pending 视图按"流水账"惯例（新→旧）渲染，
而 pending 行本质是**队列**，队列新→旧读起来就是 FILO，于是屏幕上"最该先跑的行"沉在底部，
**看起来**被饿死。执行侧从来没有 LIFO。

残留的、真实的非 FIFO 例外是 **M4.11 静态 priority tier**（bug=1 → default=5 → docs=8，
`devdoc/task-priority-m411.md`）：这是刻意的、有文档的设计，本次**不动**——
但要诚实地标出来（见下）。

## Completed

1. **认领序成为唯一定义（Rule 14）**：新增 `taskClaimOrderBy`，`TaskNextFiltered`
   （`sel += taskClaimOrderBy`）与 `TaskList` 的 pending 视图（`query += taskClaimOrderBy`）
   **引用同一个常量**，两边不可能再漂移。原本 `COALESCE(priority, 5) …` 的 SQL 片段
   现在全树只有一处拼写（`TestClaimOrderHasOneDefinition` 用 grep-gate 钉死，Rule 13）。
2. **pending 视图改为按认领序渲染**：`task list --status pending` 的首行**就是**
   `task run next` 下一行会认领的行。其他视图（无过滤 / completed / failed…）
   保持原有"新→旧"流水账惯例——只有 pending 是队列，是刻意的例外。
   Header wire contract（`taskListCountSuffix` / `taskListStatusCountSuffix` 的前导整数）
   逐字节不变（`FilterArchivedRows` 与 CLI 解析者不受影响）。
3. **队列标记，让"看起来被饿死"无法再发生**：
   - `[next]` — 认领序里**第一个可认领**的行（`task run next` / worker 的下一单），每次渲染恰好一个；
   - `[queue-skip]` — **任何派发者都拿不到**的行（merger-owned resolve 行、无 payload 的 ledger 行），
     所以它排在前面不代表被饿死，而是不可认领。
4. **可认领性谓词抽成一处**：`taskRowClaimable(tags, payload, skipSet)` ——
   `TaskNextFiltered` 的 skip 梯子与 pending 视图的 `[queue-skip]` 标记共用同一判定，
   杜绝"标记说不可认领、漏斗却认领了"这种二次漂移。
   （list 侧 skipSet 传 nil 是刻意的：list 展示整个 pending 组，被本 worker skip 的 tag
   通常是别的派发者的活，不能标成不可认领。）

## Not Done（诚实）

- **静态 priority tier 未改**：bug(1) 仍会插到 feature(5)/docs(8) 之前——这不是 FIFO，是
  M4.11 的既定设计（`devdoc/task-priority-m411.md`，operator 当初要的就是"bug 单插队"）。
  本次的交付是"**同 tier 内 FIFO + 显示序不再说谎**"，不是"全队列纯 FIFO"。
- **未实现 age-based aging（防跨 tier 饿死）**：实测当前 ledger 无此风险——
  12 条 pending 中 11 条是 tier 1，最老一条（tier 1, 1.5h）与被压住的唯一 tier-5 行同龄
  （都在 0.63h 量级）；历史认领延迟 tier 1 max 36.7h、tier 5 max 50.4h，
  跨 tier 的延迟差没有出现单调饿死形态。**没有测量证据就不引入 aging 参数**（Rule 10.3：
  先根因后参数）。若未来出现"低 tier 长期被高 tier 流压住"的实测形态，再单独开单，
  届时 `taskClaimOrderBy` 是唯一要改的落点。
- `cmd/agent` 全量套件未在本分支跑通——master 当前 `cmd/agent` 测试**编译不过**
  （`config.PeakHoursEnv` 等未定义，pending 行 01a0a3f9-8835 / 01a0a3db 记录在案）；
  这是 master 既有红灯，与本分支无关，本分支未触碰 `cmd/agent`。

## Verification

- **Red proof（master 上必须失败）**：`fix/pending-order-red-proof`（master `f2daebad` +
  仅一个公开 API 的探针测试）→
  `--- FAIL: TestRedProofPendingViewIsNewestFirstOnMaster … renders NEWEST first (pos oldest=373 > newest=91)`
  （同一测试文件在 fix 分支上 `ok`，红/绿对照由实跑给出，不是断言。）
- **Green proof（分支）**：`go build -o bin/gopi ./cmd/agent/` OK；
  `go test ./internal/tools/ -run 'TestPendingListRendersClaimOrder|TestNonPendingViewsKeepNewestFirst|TestClaimOrderHasOneDefinition' -count=1` → PASS；
  `go test ./internal/tools/ -count=1`、`./internal/standards/` → 见下方"最终验证"段（job 输出）。
- **真实 transcript（改后，真 ledger）**：`./bin/gopi task list --status pending` 首行 =
  `01a0a3db`（tier 1，14:57，最老）带 `[next]`，与 `TaskNextFiltered` 的
  `priority ASC, created_at ASC` 首行一致；改前首行是 16:22 的最新行。
- **新增回归测试**（Rule 10.5）：
  - `TestPendingListRendersClaimOrder` —— 逐行把渲染序当认领序走一遍 `TaskNext`，
    行序必须完全一致；同时断言恰好一个 `[next]`、无 payload 行带 `[queue-skip]`、
    最老行渲染在**最后**。
  - `TestNonPendingViewsKeepNewestFirst` —— 非 pending 视图与 header 形状不变。
  - `TestClaimOrderHasOneDefinition` —— 认领序 SQL 全树只许拼写一次。
- `gofmt -l internal/tools/` 干净。

## 已知边界（诚实标注）

- `[next]` = **无 skip-tag 的通用认领者**（`task run next` / worker 默认）的下一单。
  若某个派发者带 `--skip-tag`（例如 pa-queue 的专用 daemon），它可能跳过 `[next]` 行去拿更后面的行——
  这是该派发者的过滤语义，不是显示错误；list 侧刻意传 nil skipSet，见代码注释。
- 跨 tier 依然不 FIFO（见 Not Done）。

## 为什么这样改（给未来的 agent）

显示序是**契约**：操作员用 `task list --status pending` 判断"下一步跑什么"。
显示序与执行序不一致时，操作员只能得出错误结论（本次报告就是证据）。
真正的修法不是把调度器改成 LIFO（那会破坏 FIFO），而是让**显示忠于执行**，
并把不可认领的行显式标出——顺序问题与"看起来被饿死"是两件事，各自修。

## 最终验证（实跑输出）

### 构建 / 二进制
- `CGO_ENABLED=0 go build ./...` OK；`go vet ./internal/tools/` OK
- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **BIN_OK**

### 新增回归测试（分支上）
```
go test ./internal/tools/ -run 'TestPendingListRendersClaimOrder|TestNonPendingViewsKeepNewestFirst|TestClaimOrderHasOneDefinition' -count=1
→ PASS (0.465s)
```
- `TestTaskList*` / `TestTaskNext*` / `TestTaskPriority*` / `TestTaskArchive*` / `TestFilterArchived*` / `TestTaskShow*`
  定向回归 → `ok github.com/nick/gopi/internal/tools 0.877s`

### 红/绿对照（同一探针测试文件，公开 API，两棵树都能编译）
| 树 | 结果 |
|---|---|
| master `f2daebad`（`fix/pending-order-red-proof`） | **FAIL** — `renders NEWEST first (pos oldest=373 > newest=91)` |
| 本分支 `fix/pending-queue-order` | `ok` |

### 门禁（Rule 16–21 + stylegate）：master 与分支逐字节一致
本分支只动 `internal/tools/tasks.go`（+ 新增测试/devdoc），未触碰 gate 扫描面里任何 red 文件。
实测对照（`grep -v PASS` 后 diff master vs branch）：

```
$ diff master-gates.txt branch-gates.txt
1c1
< --- FAIL: TestStyleGate (5.68s)
---
> --- FAIL: TestStyleGate (6.10s)
< FAIL	github.com/nick/gopi/internal/stylegate	9.276s
> FAIL	github.com/nick/gopi/internal/stylegate	8.162s
< --- FAIL: TestStandardGates (6.48s)
> --- FAIL: TestStandardGates (6.78s)
< FAIL	github.com/nick/gopi/internal/standards	14.049s
> FAIL	github.com/nick/gopi/internal/standards	16.474s
```
**差异只有耗时**：失败测试集合完全相同 → 这些红灯是 master 既有的，不是本分支引入。

**master 既有红灯（本分支无关，均为 pending 台账已记录的行）**：
- `internal/standards` `TestStandardGates` / `TestStandardGateDataReport` FAIL（Rule-16 baseline 漂移，pending 行 01a0a40b-1e77）
- `internal/stylegate` `TestStyleGate` FAIL（errorf-verb 10 > ceiling 9，pending 行 01a0a3f9-88d4）
- `cmd/agent` 测试树**编译不过**（`config.PeakHoursEnv` undefined，pending 行 01a0a3f9-8835 / 01a0a3db）
  → 实测：在 master worktree 跑 `go vet ./cmd/agent/` 得到同样的 undefined 错，与分支无关；
  故 `cmd/agent` 全量套件无法在本分支跑通，已如实记录。

### 真实 transcript（真 ledger，改后）
```
$ ./bin/gopi task list --status pending | sed -n 2p
#01a0a3db  01a0a3db-0aff-73ab-8875-00ccab0c3f3b  [pending]  master's test tree is red: …  (created 2026-09-15 14:57:16) [next]

# 独立算出的认领首行（SQL: ORDER BY COALESCE(priority,5), created_at, id LIMIT 1）:
01a0a3db-0aff-73ab-8875-00ccab0c3f3b   ← 与渲染首行同一个 task_id
```
改前：首行是 `#01a0a429`（created 16:22:33，最新行）—— 与认领首行相反。

### 红证明探针（master-only，可复现）

`internal/tools` 全量套件在本分支上只有**一个** FAIL：
`TestMergeBuildGate_SharedModCacheResolvesWithProxyOff` —— 在 master worktree 上单独跑同一条
同样 FAIL（`the cold-cache control did not report a download attempt … no reachable module proxy`），
即**依赖网络/模块代理的环境性失败，master 既有，与本分支无关**。

红证明用到的探针测试文件（两份 worktree 都能编译，只用公开 API）：

```go
package tools

import (
	"path/filepath"
	"strings"
	"testing"
)

// 在 master 上 FAIL（pending 视图 newest-first），在本分支上 PASS。
func TestRedProofPendingViewIsNewestFirstOnMaster(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	for _, summary := range []string{"oldest of the tier", "middle of the tier", "newest of the tier"} {
		if _, err := TaskSubmit(TaskSubmitOptions{
			Summary: summary, Payload: "briefing: " + summary,
			Tags: "feature", Source: "human",
		}); err != nil {
			t.Fatalf("submit %q: %v", summary, err)
		}
	}
	rendered, err := TaskList(TaskStatusPending)
	if err != nil {
		t.Fatalf("TaskList pending: %v", err)
	}
	posOldest := strings.Index(rendered, "oldest of the tier")
	posNewest := strings.Index(rendered, "newest of the tier")
	if posOldest < 0 || posNewest < 0 {
		t.Fatalf("both rows must render:\n%s", rendered)
	}
	if posOldest > posNewest {
		t.Fatalf("pending view renders NEWEST first (pos oldest=%d > newest=%d) — the claim funnel takes the OLDEST of the tier first, so the display contradicts the execution order:\n%s",
			posOldest, posNewest, rendered)
	}
}
```

复现（master = `git worktree add ../wt-red master` 后放入该文件）：
- master：`--- FAIL: TestRedProofPendingViewIsNewestFirstOnMaster … pos oldest=373 > newest=91`
- 本分支：`ok github.com/nick/gopi/internal/tools 0.443s`
