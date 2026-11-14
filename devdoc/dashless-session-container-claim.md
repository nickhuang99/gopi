<!-- doc-class: historical -->
# 错拼孪生目录的认领规则 — dash-less 32-hex 根级目录归入哪个命名空间

**日期**: 2026-10-17 · **host**: hosta · **task**: 01a144001e4b7086b97d257497a832ed
**代码**: `internal/sessionlayout/families.go`（`IsSessionContainerName` /
`IsRootTaskContainerName` / `isNonCanonicalUUIDSpellingName` / `isDashlessUUIDName`）
**单测**: `internal/sessionlayout/dashless_container_claim_test.go` ·
`internal/sessionlayout/container_name_grouping_test.go`（既有，扩到两个拼写）

## 现象

`~/.gopi/sessions` 下有一批根级目录，名字是**32 个 hex、无连字符**（如
`01a12161de4075a487774f0042b63d6a`）。它们是 run 用错拼写 mint 出来的 canonical
容器的孪生：

- 实测 25 个，**25/25 都在同一根下有一个 canonical 孪生**（`01a12161-de40-75a4-8777-4f0042b63d6a`）；
- 其中一个孪生比它晚 **40 小时**才被写 —— 所以 dash-less 那个不是"旧拼写被取代"，是
  run 给自己 mint 的**第二个**目录；
- 25/25 都不含 `metadata.json`、不含 `*.jsonl`（账本描述的是 canonical 那个）；
- 合计 1.93 GB，里面有两个整 checkout。

## 根因（5-why 到机制为止）

1. 为什么这些目录"没有归属"？→ 因为**判它是不是容器**的那条谓词，和**判它属于哪个家族**
   的那条谓词，对同一个名字给出了不同答案：
   - `IsSessionContainerName`：`ShapeOf` 把任何 identifier run 都映射成 `<id>`，**拼写被抹掉**，
     于是 bare 32-hex 与 canonical uuid 形状相同 ⇒ `true`（是容器）；
   - `IsRootTaskContainerName`：以 `IsSessionContainerName` 为第一句短路
     （"id 命名的容器归容器家族，永不归我"）⇒ `false`；
   - stray 家族是**精确形状表**，不含它 ⇒ `false`。
2. 为什么两面不一致？→ 因为 registry 的 Match 用 `e.Shape` 判、顶层目录谓词用 `name` 判，
   而两者的差集恰好是"形状相同、拼写不同"的那一类 —— 不是任意一类，是**一个被构造出来的**空洞。
3. 后果：它既是容器、又不被任何 legacy 谓词认领 ⇒ **没有任何声明树按命名空间收它**，
   `gopi tool scratch-gc trees` 的 `tree-summary` 也数不出它（它落在 tree 的
   `not_ours` 那一堆里，和 2000 多个别的东西混在一起）。
4. 机制（norm）：**一条谓词的形状化（shape-erasure）会让两种拼写合并成一类；只要下游还有
   一条以"名字"为判据的谓词，合并处就必然出现一个谁都不认领的空档。** 修复不是再加一条
   谓词，而是把"这个名字是不是容器"这一个问题，在**两侧引用同一个导出谓词**。

## 判词（改了什么）

| 名字 | 改前 container / task | 改后 container / task |
|---|---|---|
| `01a12161-de40-75a4-8777-4f0042b63d6a`（canonical） | true / false | **true / false（不动）** |
| `01a12161de4075a487774f0042b63d6a`（dash-less） | true / **false** ← 空档 | **false / true** |
| `01a13645-66c3-79ec-962a5f8776e1f216`（错分组） | false / **false** ← 同类空档 | **false / true** |
| `t-…` / `process-…` | false / true | false / true（不动） |
| `01a11548-tracked`、`0000-emptydir`、`01a0a392`、`01a0a3926b1c`、`plain-name` | 不变 | 不变 |

- **判归哪个家族，理由必须硬**：`IsRootTaskContainerName` 是 session-legacy 树根级目录行的
  namespace 谓词；它自己的文档早就写着这个家族收 "a 32-hex row id"。dash-less 名字正是 run
  的 id（`taskGroundIDStem` 剥掉所有非字母数字字符后的拼写），所以它归 task-container 家族
  **不是权宜之计，而是这条家族自己声明的成员**。
- **零新增谓词**：新增的只有 `isDashlessUUIDName`（判"32 个小写 hex、无分隔符"）与它的并集
  `isNonCanonicalUUIDSpellingName`（dash-less ∨ 非 canonical 分组）。registry 的 Match 与
  树谓词仍然只引用那两个导出函数（Rule 13/14）。
- **错分组那一个**（`01a13645-…-962a5f8776e1f216`）在这一批里一并补上：它和 dash-less 是
  **同一个事实**的两种拼写 —— "这是 uuid 的字节，但不是本树使用的拼写"，而
  `internal/sessionid.Refuse` 对两者给的是**同一句拒绝理由**（"joining it mints a SECOND
  session directory beside the row's own"）。分开写两条谓词，就会让两半漂移到两个家族，
  而 run mint 的第二个目录会落进被遗忘的那一半。

## 为什么不复用 `internal/sessionid`

`internal/sessionid.IsAdmissible` 问的是同一件事，但**问的对象不同**：它问的是"调用者交上来的
**row id** 能不能用"（用了就拒绝写入），本包问的是"磁盘上这个**目录名**属于谁"。
两条边界的动作相反：segments 是**拒绝**，本包是**收编**（拒绝完了还要有人认领，否则就是空档）。
import 过来也不会更省：`sessionid` 会去 parse uuid，而本包只需要"长度 + hex"这个更窄的判据
（Rule 35：能不加依赖就不加）。

## 取证（本 run 实测）

```bash
# 改前
$ gopi tool scratch-gc trees --dry-run | grep -E 'tree=(session-raw|session-legacy) '
tree=session-raw     present=true entries=0  holding=0        holding_n=0
tree=session-legacy  present=true entries=22 holding=1931700  holding_n=22
tree-summary tree=session-raw    examined=10197 not_ours=1 collectable=8 kept=10189
tree-summary tree=session-legacy examined=22    not_ours=2089 collectable=0 kept=22

# 改后（同一命令，新二进制）
tree=session-raw     present=true entries=0  holding=0         holding_n=0     ← 原样
tree=session-legacy  present=true entries=52 holding=1104978284 holding_n=52   ← +30
tree-summary tree=session-raw    examined=10197 not_ours=1 collectable=8 kept=10189  ← 原样
tree-summary tree=session-legacy examined=52    not_ours=2059 collectable=0 kept=52

# 25 个 dash-less 是否真的被 legacy 行认领（逐条比对 /tmp/dl.txt，25 个名字）
$ gopi tool scratch-gc trees --dry-run --tree session-legacy --explain > /tmp/legacy_explain.txt
claimed_dashless=25 of 25
# canonical 孪生没有跑到 legacy 命名空间里来
$ grep -c '01a12161-de40-75a4-8777-4f0042b63d6a' /tmp/legacy_explain.txt
0
```

`entries=`（树按 namespace 匹配到的条数）是本次改动唯一移动的计数：22 → 52，差额 30 = 25 个
dash-less 目录 + 5 个此前也在 `not_ours` 里、同样"是容器但无人认领"的目录（非 canonical 分组
那一个即在其中）。`collectable=0` 两侧都不变 —— legacy 窗口是 180 天，而这批目录最新的是
2026-10-16，一个都不会被收走（Rule 22：删除策略是 operator 的决定，本行不删任何目录）。

## 红证

把 `isDashlessUUIDName` 的第一句改成 `if true { return false }`（规则关掉），
`go test ./internal/sessionlayout/ -run Dashless` 必红，逐条指出：

```
--- FAIL: TestDashlessContainerIsRefusedAndClaimed (0.00s)
    IsSessionContainerName("01a12161de4075a487774f0042b63d6a") = true — …must read the name itself
    IsRootTaskContainerName("01a12161de4075a487774f0042b63d6a") = false — …in the gap between two predicates
```

恢复用显式修订核对（不是 index 相对 diff）：

```
SNAP_before = git hash-object internal/sessionlayout/families.go
            = 9ae36050c480de81ac7ab0fd67bc0bbe220bff82
恢复后 now = 9ae36050c480de81ac7ab0fd67bc0bbe220bff82  → RESTORE_VERIFIED=YES
```

## 本 run 顺带发现、未在本行修的两处（Rule 41）

1. `internal/scratch/sessions_scratch_dir.go` 的 `containerHasNoLedger` 只承认 canonical 拼写：
   一个**已经没有 canonical 孪生**的 dash-less 容器（不是错拼，而是唯一载体）永远拿不到
   `session-scratch-dir` 家族，其内部目录永远不 collectable。本主机上 25/25 都有孪生，
   所以今天观察不到，但判词确实分叉了。
2. `internal/scratch` 的容器臂对容器自身给出 keep 的判据是"这一遍里还有 keptFiles>0 的兄弟条目"
   （`containerEntries` / `containerVerdicts`），也就是说**容器自身的归属判词是整遍 pass 的函数**，
   不是容器自身的函数。本行**未实测**跨 pass 的差异（标注：这是读代码得到的推断，不是观测），
   仅记录为待证。
