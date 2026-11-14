<!-- doc-class: historical -->
# Task Report: 判定 doccheck 的 `first-frozen` 断言 vs census `--check` 的集合冻结 — 哪一侧是 bug

task `01a12fb8-a821-70d1-9243-9df6af918c10`
worktree `/home/user/workspace/gopi-01a12fb8a82170d192439df6af918c10`，branch `task/01a12fb8a82170d192439df6af918c10`

## Completed

**判定：doccheck 的 `first-frozen` 断言是对的；错的是生成器/probe 侧。**
矛盾不是"同一棵树、同一张表、两侧代码相同"——**两侧读的是不同源码**：

- `gopi tool doccheck` 走 `/usr/local/bin/gopi -> bin/gopi`，那个二进制是 **master 侧代码**（Rule 12 的 rebuild 产物）。
- `go run internal/docclass/testdata/anchor-number-census.go` 读**当前工作树**的源码。

矛盾态只在 `bc10e8d78`（18:25:55）这一棵树上成立，且它在该 commit 里是**自洽的假绿**：doccheck（master 代码）rc=1 报 3 条 CLAIM NOT TRUE；census（bc10 代码 + bc10 表）rc=0 报 "the freeze is exact"。

**根因（三重独立确证）——key 含 class 导致冻结点人口被查错拼写：**

`bc10e8d78` 的 permit identity 是 4 字段 `doc|verdict|anchor-cell|text`，而 `verdict`（`number-drifted` vs `recorded-line-blank`）是**当前树**的算术函数。probe 拿这个带 class 的 key 去查 **0b3c1de29 的树**，于是查的是一个 freeze 树里根本不存在的拼写 → key 被判 ABSENT → `first-frozen` 被**真空确认为真** → 生成器照写 → census 自查用同一个 probe、同一口径 → rc=0 自洽。而 doccheck 用同一函数却拿到被上一版生成器写进表行的 claim，判定相反。

实测（原始输出见 `## Evidence`）：

| 行 | bc10 树上的 class | 探针在 0b3c1de29 的判定 | `first-frozen` |
|---|---|---|---|
| devdoc/cov-agent-cr.md | `recorded-line-blank` | **ABSENT**（人口 18） | 被判"真" |
| 同一行换成 freeze 树给的 class | `number-drifted` | **IN** | 应为"假" |
| devdoc/merge-backlog-terminal-guard.md | `number-drifted` | ABSENT | 真 |
| devdoc/task-notify-terminal-gap-20260912.md | `recorded-line-blank` | ABSENT | 被判"真" |

**这 6 行的真伪由"表的权威探针"独立裁决（同一 freeze commit，两组表各喂自己的 3 条 first-frozen）：**

- SHIPPED 表（master/HEAD）的 3 条 → 全部 **ABSENT** → claim **真** ✓
- bc10 表的 3 条（cov-agent-cr / nightshift / task-notify-terminal-gap）→ 全部 **IN** → claim **假** ✗

因此 **bc10 的表错了 3 行**（`devdoc/cov-agent-cr.md`、`devdoc/nightshift-20260911.md`、`devdoc/task-notify-terminal-gap-20260912.md` 被误标 `first-frozen`），**doccheck 的 3 条 CLAIM NOT TRUE 是正确的**。

**修错的一侧（已落地）：** 真正错的那半边（key 形状 + probe 口径）已由 `974a3f456` 修正（key 改为 `doc|path|text` 3 字段，`AnchorNumberBaselineKeyPathOf` 成为 key 的唯一读者），表由其后的 `c5e796b93` 重生成，master 上两条命令现在一致（均 rc=0）。

**本 run 补上缺失的回归测试**（Rule 15：静默失败）：`AnchorNumberProvenanceProbe` 对读不懂的 key 原本 `continue` 静默丢弃 —— 这正是上述真空确认的**残留机制**：一个无法解析的 key 不 materialise 任何文件，于是它"不在人口里"，而"不在人口里"恰是 `first-frozen` 所引用的全部证据。已改为**具名拒绝**：

`internal/docclass/anchor_number_provenance.go` — `AnchorNumberProvenanceProbe` 现在返回带 key 拼写与形状原因的 error，而不是空人口。

新增 `internal/docclass/anchor_number_provenance_test.go` 的 `TestAnchorNumberUnreadableKeyIsNeverConfirmedByDropping`：修复前**红**、修复后**绿**（见 Verification）。

## Not Done

- **未重生成 `bc10e8d78` 那棵树上的表**：该 commit 不在本 run 的分支祖先上，且其污染已由 master 侧路径覆盖；表是 DATA，只走生成器（Rule 36），本 run 不代它重生成。
- **未修 `947490f56`/`3ecc16d5d` 上的 key-shape 漂移**：那是同一 bug 家族在中间 commit 上的形态，已随 `974a3f456` 终结。
- **报告取数中探针人口报 28 而非表头的 153**：153 是**全表所有 permit 的锚文件并集**上的人口；探针只 materialise 它收到的 key 所命名的文件，故喂 6 个 key 时人口=28、喂 3 个 key 时人口=18。这是 probe 的文档化行为（"Only the anchored files some permit actually names"），不是缺陷；判定用逐 key 的 IN/ABSENT，不受人口大小影响。
- **未把"bc10 表 3 行误标"作为独立 bug 行提交**：该缺陷已由 `974a3f456`+`c5e796b93` 在 master 上修复，且本 run 已补回归；无新增活缺陷可提。

## Verification

- **Build**: `CGO_ENABLED=0 go build -o /tmp/gopi-fixed ./cmd/agent/` → OK；`go build ./...` → OK
- **Tests**:
  - 新回归红→绿：`go test ./internal/docclass/ -run TestAnchorNumberUnreadableKeyIsNeverConfirmedByDropping -count=1`
    - 修复前：`--- FAIL: ... the probe accepted the unreadable key "devdoc/stable.md\tsrc.go" and reported an empty population ...`（原样）
    - 修复后：`ok  github.com/nick/gopi/internal/docclass  1.622s`
  - 分支范围套餐：`make test-budget PKGS='./internal/docclass ./cmd/agent'` → 实测按包统计（原始 stream 取数）：
    ```
    packages seen: 2
      github.com/nick/gopi/cmd/agent             pass=  733 fail=   0 skip=   8
      github.com/nick/gopi/internal/docclass     pass=   95 fail=   0 skip=   0
    ```
    取数命令：`python3` 读 `/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791806608-2244770.json` 逐行 `Action` 聚合（同一次运行的 stream），**0 失败**。
- **Ran**（Rule 31，真跑被改的面，两条命令同树同表）:
  - `/tmp/gopi-fixed tool doccheck --repo .` → rc=0，`156 of 156 are frozen ...`
  - `go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check` → rc=0，`table matches the tree — the freeze is exact`
- **Result**: 同一棵树、同一张表上，两条命令**不再矛盾**（均 rc=0、均报冻结精确）。
- **Commit**: `7df189ee6 fix(docclass): an unreadable key is an ERROR, never a silent drop (the false `first-frozen` mark)`（分支 `task/01a12fb8a82170d192439df6af918c10`，base `8922d775a`）。本报告本身作为紧随其后的 docs commit 落在同一分支。

## Evidence (self-filed)

### 1. 两条命令在 bc10 树上相反（原始输出）

```
$ cd /home/user/workspace/gopi-wt-bc10e8d78      # git worktree add --detach ... bc10e8d78
$ gopi tool doccheck --repo .                    # the INSTALLED binary = master-side code
doccheck: anchor NUMBER coherence (recorded text still present, line number stale):
doccheck:   CLAIM NOT TRUE (regenerate the table): devdoc/cov-agent-cr.md	internal/agent/loop.go	// checkpoint (candidate C) so it fires exactly once per run, at the f is present in 0b3c1de29's incoherent population (153 key(s) measured there), so a `first-frozen` claim for this key is false
doccheck:   CLAIM NOT TRUE (regenerate the table): devdoc/nightshift-20260911.md	internal/agent/loop.go	// CompactTargetRatio is the fraction of the window the history is is present in 0b3c1de29's incoherent population (153 key(s) measured there), so a `first-frozen` claim for this key is false
doccheck:   CLAIM NOT TRUE (regenerate the table): devdoc/task-notify-terminal-gap-20260912.md	internal/tools/tasks.go	if rawLogPath == "" { is present in 0b3c1de29's incoherent population (153 key(s) measured there), so a `first-frozen` claim for this key is false
rc=1

$ go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check
anchor-number-census: scanned 237 anchor row(s), 156 incoherent; table carries 156 row(s)
anchor-number-census: table matches the tree — the freeze is exact
rc=0
```

### 2. 表头自证 bc10 的 6 条 first-frozen（原始输出）

```
$ sed -n '33p' /home/user/workspace/gopi-wt-bc10e8d78/internal/docclass/testdata/anchor-number-baseline.tsv
# provenance: base 0b3c1de29; 153 incoherent key(s) measured in that tree; 150 permit(s) claim `pre-existing`, 6 state `first-frozen`
$ git branch containing bc10e8d78 / 974a3f456 timestamps
bc10e8d78 2026-10-12 18:25:55 +0800  (not an ancestor of master)
974a3f456 2026-10-12 18:27:35 +0800  (IS an ancestor of master)
```

### 3. 机制决定性证据：bc10 自己的 probe，同一行两种 class（原始输出）

```
$ printf 'devdoc/cov-agent-cr.md\trecorded-line-blank\tinternal/agent/loop.go:1814\t// checkpoint (candidate C) so it fires exactly once per run, at the f\n' > /tmp/bc10-4f.txt
$ go run ./internal/docclass/testdata/tmppop . 0b3c1de29 /tmp/bc10-4f.txt      # bc10 tree's own probe
population measured at 0b3c1de29: 18 key(s)
  ABSENT from the population devdoc/cov-agent-cr.md | recorded-line-blank | internal/agent/loop.go:1814 | // checkpoint (candidate C) so it ...

$ printf 'devdoc/cov-agent-cr.md\tnumber-drifted\tinternal/agent/loop.go:1814\t// checkpoint (candidate C) so it fires exactly once per run, at the f\n' > /tmp/bc10-4f-b.txt
$ go run ./internal/docclass/testdata/tmppop . 0b3c1de29 /tmp/bc10-4f-b.txt
population measured at 0b3c1de29: 18 key(s)
  IN the population devdoc/cov-agent-cr.md | number-drifted | internal/agent/loop.go:1814 | // checkpoint (candidate C) so it fires exactly ...
```

→ 同一行、同一 freeze commit：查**当前树**的 class 得 ABSENT（`first-frozen` 真空确认为真），查 **freeze 树**的 class 得 IN。这就是 bc10 表 3 行误标的机制。

### 4. 权威探针在 freeze commit 上的裁决（原始输出，报告取数命令）

```
$ go run ./internal/docclass/testdata/tmploop . 0b3c1de29
freeze population measured at 0b3c1de29: 28 key(s)

  ABSENT  devdoc/archive-mint-outside-writer.md      class@HEAD=number-drifted       -> `first-frozen` claim is true
  ABSENT  devdoc/merge-backlog-terminal-guard.md     class@HEAD=number-drifted       -> `first-frozen` claim is true
  ABSENT  devdoc/task-render-pct-extra-determination.md class@HEAD=number-drifted       -> `first-frozen` claim is true
  IN      devdoc/cov-agent-cr.md                     class@HEAD=recorded-line-blank  -> `first-frozen` claim is FALSE
  IN      devdoc/nightshift-20260911.md              class@HEAD=number-drifted       -> `first-frozen` claim is FALSE
  IN      devdoc/task-notify-terminal-gap-20260912.md class@HEAD=recorded-line-blank  -> `first-frozen` claim is FALSE
```

前 3 行 = SHIPPED 表（master/HEAD）的 3 条 first-frozen → 全 ABSENT → claim 真。
后 3 行 = bc10 表额外的 3 条 → 全 IN → claim 假。

### 5. 独立于 docclass 代码的原始 git 复核（在 0b3c1de29 上）

```
$ for each row: doc in freeze manifest? recorded cell? text off the recorded line?
  devdoc/cov-agent-cr.md -> doc present, text in file (1 hit(s)) but NOT on line 1814 => INCOHERENT at the freeze => first-frozen FALSE
  devdoc/nightshift-20260911.md -> doc present, text in file (1 hit(s)) but NOT on line 1904 => INCOHERENT at the freeze => first-frozen FALSE
  devdoc/task-notify-terminal-gap-20260912.md -> doc present, text in file (1 hit(s)) but NOT on line 2395 => INCOHERENT at the freeze => first-frozen FALSE
  devdoc/archive-mint-outside-writer.md -> doc ABSENT from the freeze manifest => key cannot exist => first-frozen HONEST
  devdoc/merge-backlog-terminal-guard.md -> doc present, text in file (1150 hit(s)) but NOT on line 2825 => INCOHERENT at the freeze => first-frozen FALSE   ← 见下
  devdoc/task-render-pct-extra-determination.md -> doc ABSENT from the freeze manifest => key cannot exist => first-frozen HONEST
```

**方法自证（诚实标注）**：第 5 步中 `merge-backlog-terminal-guard.md` 这一行是我用**两个字段**（doc path）去匹配的简化口径，它给出的 INCOHERENT 与探针的 ABSENT **不一致**；原因是该行的 recorded text 是 `}`（在 `internal/tools/tasks.go` 里 1150 处命中），我的简化口径无法表达"recorded cell 具体是哪一列"。**探针口径（第 4 节）是权威判据** —— 它以 `Evidence.Check` 的门为前置条件，第 5 节只作旁证。第 5 节中三行 "doc ABSENT from the freeze manifest" 的结论与探针一致，是可靠的独立复核。

### 6. 修复后两条命令同树同表一致（原始输出）

```
$ CGO_ENABLED=0 go build -o /tmp/gopi-fixed ./cmd/agent/ && /tmp/gopi-fixed tool doccheck --repo .
doccheck:   156 of 156 are frozen in internal/docclass/testdata/anchor-number-baseline.tsv (...)
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
rc=0

$ go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check
anchor-number-census: scanned 238 anchor row(s), 156 incoherent; table carries 156 row(s)
anchor-number-census: table matches the tree — the freeze is exact
rc=0
```

### 7. 新回归的红→绿（原始输出）

```
before fix:
--- FAIL: TestAnchorNumberUnreadableKeyIsNeverConfirmedByDropping (0.04s)
    anchor_number_provenance_test.go:327: the probe accepted the unreadable key "devdoc/stable.md\tsrc.go" and reported an empty population — a key it cannot read is silently dropped, and a dropped key is CONFIRMED as absent from the base, i.e. blessed `first-frozen`, whatever that tree says
FAIL	github.com/nick/gopi/internal/docclass	0.893s

after fix:
ok  	github.com/nick/gopi/internal/docclass	1.622s
```

### 8. 分支范围测试套餐

`make test-budget PKGS='./internal/docclass ./cmd/agent'` → 按包实测：`cmd/agent` 733 pass / 0 fail / 8 skip，`internal/docclass` 95 pass / 0 fail（取数：同一次运行的 stream `/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791806608-2244770.json` 逐行 `Action` 聚合；原始 stdout 全文在 `/tmp/docclass-budget.log`）。

### 9. 收尾状态

- `git status --porcelain` in the run's worktree — 见 commit 前的实测（探针目录已删除）。
- 旁证 worktree（`gopi-wt-bc10e8d78`、`gopi-wt-947490f56`）为取证临时创建，收尾已 `git worktree remove`。
- 证据文件落本 run 的 session 目录：`~/.gopi/sessions/01a12fb8-a821-70d1-9243-9df6af918c10/evidence/freeze-population.txt`

**Commit**: 见本文件所在 commit（`git log -1 --format=%H`）。
