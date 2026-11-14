<!-- doc-class: historical -->
# 洁净树上两道内部门自红：先在锚点复测，再判谁修的

任务前提是「master 上的 `internal/tools` 两道内部门自红」：
`TestAssertionTotalityAuditGate` 报 5 条 UNCLASSIFIED，`TestStylegateRatchetSuiteIsGreenOnThisTree`
报 3 个 stylegate 站点。briefing 的断言锚在 `9a7f1b0`，而 **master 已经前进 14 个提交**——
所以第一步不是分类，是**复测**。结论：**在 `dfcff49`（本 run 的树）上两道门都是绿的，
红在这 14 个提交里已经被两次提交各自修掉了**，本 run 不需要（也不应该）重新分类。

## 1. 复测：红在锚点，绿在 HEAD

```
# 本 run 的树（dfcff49）
$ go test ./internal/tools -run 'TestAssertionTotalityAuditGate|TestStylegateRatchetSuiteIsGreenOnThisTree' -count=1
ok  	github.com/nick/gopi/internal/tools	25.437s        # exit=0
$ go test ./internal/stylegate -count=1
ok  	github.com/nick/gopi/internal/stylegate	9.255s     # exit=0

# 同一对门，在 briefing 的锚点 9a7f1b0（/home/user/workspace/redproof-01a174f3，detached 复测）
$ go test ./internal/tools -run '...' -count=1
--- FAIL: TestAssertionTotalityAuditGate (0.83s)                # exit=1
--- FAIL: TestStylegateRatchetSuiteIsGreenOnThisTree (29.27s)
    cmd/agent/cli_web_shot_test.go:71 (error from ln.Close discarded)
    cmd/agent/cli_web_shot_test.go:80 (error from conn.Close discarded)
    cmd/agent/web_voice_test.go:164 (time.Sleep in a test — poll/wait instead…)
```

5 条 UNCLASSIFIED 在锚点上全部可复现（`run_ground_collect_clone_test.go` 的 :238 / :270 /
:310 / :341 / :375），**与 briefing 描述的 4 个 refusal 测试 + 1 个 gone 测试完全一致**。

## 2. 谁修的：两个提交，都真在 master 上

`git log --oneline 9a7f1b0..HEAD -- <那四个文件>` 给出两条，且文件分布是干净的：

| 提交 | 修掉了什么 | 改的文件 |
|---|---|---|
| `a16e81d` | 3 个 stylegate 站点 | `cmd/agent/cli_web_shot_test.go`、`cmd/agent/web_voice_test.go` |
| `b0d75f5` | 5 条 UNCLASSIFIED | `internal/tools/assertion_totality_allow_test.go`（+22 行） |

**门禁本身没被削弱**，这是两道门各自自己的门文件哈希说了算（不是提交信息说了算）：

```
$ git rev-parse HEAD:internal/tools/assertion_totality_audit_test.go   412b50f7…
$ git rev-parse 9a7f1b0:internal/tools/assertion_totality_audit_test.go 412b50f7…   # 逐字节相同
$ git rev-parse HEAD:internal/stylegate/gate_test.go                   54ed2252…
$ git rev-parse 9a7f1b0:internal/stylegate/gate_test.go                54ed2252…   # ceiling 未动
$ git rev-parse HEAD:internal/stylegate/stylegate.go                    835a974f…
$ git rev-parse 9a7f1b0:internal/stylegate/stylegate.go                 835a974f…
```

`b0d75f5` 只碰了 allow 文件一行都没碰断言（`--stat`: 1 file, +22 −0），`a16e81d` 只碰
`cmd/agent` 两个测试文件。

## 3. 5 条 UNCLASSIFIED 的判定：correct，两个半边都站得住（独立核对，不是回引）

allow 行写在 `assertion_totality_allow_test.go:236-256`，本 run **没有编辑它们**；下面是
我自己在代码里核对每一句的锚点，而不是把断言原文抄一遍。

- **claim 的真实性（half a）**：记录的写入点在
  `internal/tools/run_ground_anchor.go:706`（clone 腿）与 `:727`（linked 腿），**都在移除之前**；
  每条 refusal 是 `skipped = append(...)` + `continue`，所以被拒绝的 ground **到不了这两个写入点**。
  于是「没有记录」是「没有动作」的**缺席证据**，不是一条缺失的记录。
- **集合里没有会被误排除的成员（half b）**：被否定的集合是**记录集**（一个 sink 过滤到
  `KindWorktreeRemoval` + 本 fixture 路径），不是步骤序列——没有「本该执行的幂等步骤」能被
  这条过滤吞掉；唯一必须不发生的步骤（移除）恰好就是记录要宣告的那件事。
- **每一处的具体拒绝理由各不相同，我逐一看着输出核过**（`five-sites-behavioural.txt`）：
  - `…CarryingUndeliveredWork` — 计划步就拒；测试同处**正向**断言 ground 在 `kept` 且理由含
    "no delivery point has"（`run_ground_anchor.go:278` 的 `worktreeSweepUndeliveredRefusal`，
    在 `RunGroundCollectableGrounds` 的 `:520` 调用处把 ground 判成 kept 而不是 collectable）。
  - `…ADirtyCloneGround` — skip 行实测为 `the clone ground is not clean — worktree is DIRTY (uncommitted work)…seed.txt`
    （`run_ground_anchor.go:771-779` 的 `reclaimDirtyVerdict` 分支）。
  - `…WithScratchInIt` — skip 行实测为 `the clone ground is not clean — …untracked…scratch.log`。
  - `…ALiveCloneGround` — 计划步 kept：`kept: the launch context t…`（read-out 实测），apply 拿到空选择。
  - `…SkipsAGoneCloneGround` — skip 行实测为 `the directory is already gone — nothing to remove…`
    （`run_ground_anchor.go:652` 的 `os.Stat`/`os.IsNotExist` 分支，且测试同时**正向**断言
    不含 git 的 "is not a working tree"）。
- **残留（行里也写了，我复核同意）**：过滤器是 (kind, target-path)，所以「同一 sink 里为**别的路径**
  写的记录」看不见；只读一个 sink。

## 4. 3 个 stylegate 站点：一个真修，两个「这行有注释会不会变成免死牌」的独立验证

- `cmd/agent/web_voice_test.go:164`（原 `time.Sleep`）—— **真修**，不是 allow：
  `waitForRounds` 改成等 recorder 自己的唤醒 channel（`Round` 在计数变化时 close+换新），
  并保留 5s deadline。两个调用点（`:315`、`:451`）都走同一把等待，实测通过。
- `cmd/agent/cli_web_shot_test.go:71/:80` —— `_ = ln.Close()` / `_ = conn.Close()` 带
  `//stylegate:ignore <reason>`。这条规则**只认标记 + 理由**（`IgnoreMarker`，理由 ≥
  `MinReasonLen`=10），没有更「真」的写法：`t.Cleanup(func(){ _ = ln.Close() })` 里没有
  可断言的失败路径，`conn` 的关闭在 accept 循环的退出分支上。

**为了避免「有注释 = 门禁被削弱」，我做了两次变异探针（在一次性 worktree 里，不进树）：**

| 探针 | 结果 | 说明 |
|---|---|---|
| 把两行的 marker 换成普通注释、`Close()` 去掉 `_ =` | `stylegate unchecked-close: 2 test hit(s), ceiling 0` → `cmd/agent/cli_web_shot_test.go:71 / :80` **红** | 站点没有被「静音」，撤掉 marker 立刻红 |
| 新建 `cmd/agent/zz_probe_sleep_test.go`（`package main`，一行 `time.Sleep`） | `stylegate test-sleep: 1 test hit(s), ceiling 0` → 指名该文件 **红** | 该 arm 的探测与指名都还活着 |
| 同探针第一次写成 `package agent` | 门报 `type information failed for 1 package(s)` | **探针本身写错**（cmd/agent 是 `package main`），不是门禁失效——记在这里免得下一个人误判 |

三条门的 ceiling 都是 0（`internal/stylegate/gate_test.go:130-135`），所以上面任何一条红都
不可能被基线吸收。

## 5. 顺带发现并修掉的缺陷：生产注释与它下面 30 行的代码相反

`run_ground_anchor.go:745-749` 的 doc 块写着：

> untracked-only residue is a run's own scratch, … which this leg therefore removes WITH the
> quote in the audit evidence, rather than parking the directory forever.

而 `:771` 的代码（同一个提交链里的 `6c394e5`「the clone leg refuses ANY dirt」）对
**untracked-only 也拒绝**。两份文档只能有一份真，实测判据是下面这条命令在最小的
两仓 fixture 上跑出来的：

```
$ git worktree remove linked-wt          # 树里只有未跟踪的 scratch.log
fatal: 'linked-wt' contains modified or untracked files, use --force to delete it
exit 1
$ git worktree remove linked-wt2         # 树里只有一处已跟踪文件的改动
fatal: 'linked-wt2' contains modified or untracked files, use --force to delete it
exit 1
```

git 对两种脏都拒绝，所以 clone 腿（`os.RemoveAll`，什么也不问）跟着拒绝才是**parity**——
注释是 `6c394e5` 之前的旧文本，那次提交只改代码和测试，没改这段 doc。本 run 把 ② 的句子
改成与代码一致（并写进上面的实测命令与输出）。**只改注释，不改任何判据**：
`git rev-parse HEAD:internal/tools/run_ground_collect_clone_test.go` 的 5 条 allow 行、
两个拒绝分支和门文件一个字节都没动。

## 6. 另一条与本任务无关的发现（已按 Rule 41 取好证）

`go test ./cmd/agent -race`（本 run 树的 HEAD）在 `-run 'TestVoice|TestWebShot'` 下报
DATA RACE，**在锚点 9a7f1b0 上同样报 22 条**，帧全部落在
`cmd/agent/suite_stall_watchdog_test.go:208/:223/:510`（`suiteStallBudgetDuration` 在
goroutine 里读 `flag.durationValue`，而 `TestMain` 的 `flag.Parse` 已写过它），
**没有一帧落在本任务涉及的两个文件**。这不是本次改动引入的，是预存的测试基础设施竞态，
已单独立 bug 行（见任务报告）。

## 7. 复现清单（全部命令可粘）

```bash
W=/home/user/workspace/01a174f3-f5af-71d5-a20a-61172074f008     # 本 run 的 clone ground
E=$HOME/.gopi/sessions/01a174f3-f5af-71d5-a20a-61172074f008/evidence

cd "$W" && go test ./internal/tools -run 'TestAssertionTotalityAuditGate|TestStylegateRatchetSuiteIsGreenOnThisTree' -count=1 -v
cd "$W" && go test ./internal/stylegate -count=1
cd "$W" && go test ./cmd/agent -run 'TestVoice|TestWebShot' -count=1
cd "$W" && go test ./internal/tools -run 'TestRunGroundCollect' -count=1 -v
cd "$W" && git status --porcelain        # 必须为空（Rule 30）

# 锚点复测（git worktree add --detach 到声明根下）
git -C "$W" worktree add --detach /home/user/workspace/redproof-01a174f3 9a7f1b0
cd /home/user/workspace/redproof-01a174f3 && go test ./internal/tools -run 'TestAssertionTotalityAuditGate|TestStylegateRatchetSuiteIsGreenOnThisTree' -count=1
```

证据文件（本 run 的 session 目录，Rule 37）：`red-before-anchor-9a7f1b0.txt`、
`retest-verbose.txt`、`stylegate-suite.txt`、`provenance.txt`、`five-sites-behavioural.txt`、
`cmd-agent-behavioral.txt`、`cmd-agent-race.txt`、`cmd-agent-race-at-9a7f1b0.txt`、
`mutation-probe-red.txt`、`stylegate-mutation-probe-cmdagent.txt`、`stylegate-probe-bare-close.txt`、
`stylegate-probe-marker-removed.txt`、`tools-package-full.txt`。
