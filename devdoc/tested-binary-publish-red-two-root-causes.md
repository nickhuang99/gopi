<!-- doc-class: normative -->
# 「`GOPI_BIN` 已设时必红」有两条根因 —— 且其中一条的修复曾搁浅在未合并的分支上

task `01a12d2c-a3a6-7c5d-b2a1-fa03081027d5`（2026-10-12，host hostb-Laptop）·
分支 `task/01a12d2ca3a67c5db2a1fa03081027d5`

## 症状与它为什么骗人

一句话症状：`TestGopiBinPublishesTheTreeBuildToChildren`（`internal/testutil/gopibin_pin_test.go`）
在环境已设 `GOPI_BIN` 时**恒红**，同一棵树、同一个二进制，仅环境变量之差。

这个症状**有两条互相独立的根因**，且它们在不同的层：

| # | 根因 | 层 | 机制 | 修在 |
|---|---|---|---|---|
| 1 | 用例断言的契约与它自己没排除的状态矛盾 | 测试文件 | 该用例断言「**suite 构建臂**发布 `GOPI_TEST_BIN`」，但当 `$GOPI_BIN`（`testutil.BinaryPinEnv`）已设时，`resolveGopiBin`（`internal/testutil/run.go`）在**第 1 臂就 return**，suite 构建臂不可达 ⇒ 永不发布 ⇒ `before` 恒空 ⇒ FAIL。同文件的 `TestGopiBinPrefersTheSuitePinnedBinary` 恰恰断言 `GOPI_BIN` **优先** —— 两条用例对同一状态期望相反 | `b42e1b3fc`（row `01a12ceb`） |
| 2 | 构建系统静默覆盖了调用方的 pin | `Makefile` | `GOPI_BIN := $(BIN_DIR)/gopi` 用 `:=` **覆盖**调用方导出的值，而该变量从环境导入 ⇒ **保留 export 位** ⇒ 每个配方的每个子进程读到的是**相对**路径 `bin/gopi`。`go test` 给每个包自己的目录当 CWD ⇒ 相对路径不可解析 ⇒ `resolveGopiBin` 的 fatal 臂（设计上**无静默替换**）把调用方的 pin 变成红 suite | `cc6329a65`（row `01a12d14`） |

**同一句症状，两条根因**，所以只修一条时「必红」并不消失——这正是本 row 被提出来时的状态。

## 关键事实：修 #2 在 master 上不存在（曾经）

**file 号 `01a12d2c` 的文件时间 06:54:22；fix#1 于 06:43:25 合入 master（`dbfddcd04`）、fix#2 于 06:48:56 提交。** 但：

```
git merge-base --is-ancestor b42e1b3fc peer1/master   # exit 0 → fix#1 在 master
git merge-base --is-ancestor cc6329a65 peer1/master   # exit 1 → fix#2 不在 master
git branch -a --contains cc6329a65
#   remotes/peer1/task/01a12d14513a7b21baeed08e75acd312   ← 只此一个
```

**「修复存在」≠「修复在 master 上」。** 一个 row 被提为「老 bug 还活着」时，正确的第一步不是相信或怀疑这条断言，而是**量一下每个候选修复各在哪个 ref 上**（`--is-ancestor` + `--contains`）。本 row 的实测：fix#1 在 master，fix#2 只在一个从未合并的 task 分支上——所以现象是**半修好**，而它读起来像「完全没修」。

## 验收形态决定了看到哪条根因

**同一个 bug，两个入口，看到的根因不同：**

| 入口 | 谁先命中 | 结果 |
|---|---|---|
| `GOPI_BIN=<绝对> go test ./internal/testutil/` | 根因 1 | 修 #1 后 SKIP + exit 0 |
| `GOPI_BIN=<绝对> make test-budget PKGS='…'` | 根因 2（`Makefile` 先污染环境） | 修 #1 **不管用**；修 #2 后才 exit 0 |

修 #2 之前，`make` 形态的失败原文是 `GOPI_BIN=bin/gopi is set but not usable: stat bin/gopi: no such file or directory`
—— 注意 `bin/gopi` 是**相对**路径，与调用方传的绝对路径不同；**这个「值不一样」就是 Makefile 覆盖的直接指纹**，也是区分两条根因的最省事的判据。

## 判定 Makefile 覆盖的一行探针（不改任何文件）

```bash
GOPI_BIN=/abs/path/gopi make -f Makefile --eval='probe: ; @echo "recipe shell sees GOPI_BIN=[$$GOPI_BIN]"' probe
# → recipe shell sees GOPI_BIN=[bin/gopi]     调用方的绝对路径被覆盖成相对路径
env -u GOPI_BIN make -f Makefile --eval='probe: ; @echo "recipe shell sees GOPI_BIN=[$$GOPI_BIN]"' probe
# → recipe shell sees GOPI_BIN=[]            对照：调用方没设时，配方内也没有
```

这一行同时证明了「环境值被覆盖」和「export 位保留」（否则子 shell 看到的是空）。

## 红证的做法：`-overlay`，不动工作树

「修前必红」不该靠嘴说，也不该靠改工作树再改回来。装回修前文件、**不落盘**：

```bash
git show b42e1b3fc^:internal/testutil/gopibin_pin_test.go > "$EV/prefix/gopibin_pin_test.go"
python3 -c "import json;print(json.dumps({'Replace':{'<abs worktree>/internal/testutil/gopibin_pin_test.go':'$EV/prefix/gopibin_pin_test.go'}}))" > "$EV/prefix/overlay.json"
GOPI_BIN="$EV/gopi" go test -overlay "$EV/prefix/overlay.json" ./internal/testutil/ -count=1 -run '^TestGopiBinPublishesTheTreeBuildToChildren$' -v
```

复现出的正是原始失败字样 `left GOPI_TEST_BIN unset`，且随后 `git status --porcelain` 为空 —— **`-overlay` 只影响编译期读取的字节，不改工作树**，所以红证与 Rule 30 不冲突。

## 修法的纪律：不是把红改成绿

- **#1 的修法**是加前置条件（skip 该状态），**不是**放宽断言：一个 PASS 在该状态下等于**声称一次可证明从未发生的构建**（Rule 15）。且 pin 指向**不可用**路径时**故意不跳过**——那是 `resolveGopiBin` 既有的 fatal 臂，无静默替换。
- 契约两侧自洽的复核：去掉 `GOPI_BIN` 时该用例仍 **PASS**（不是恒 skip）。skip 是**条件性**的。
- **#2 的修法**是**改名**（`GOPI_BIN` → `BIN_PATH`，构建产物用仓库私有名），不是给 `Makefile` 加 `?=` 之类的小聪明：`GOPI_BIN` 是 `internal/testutil` 发布的**调用方 pin** 的名字（`testutil.BinaryPinEnv`），把构建产物绑到这个名字上就是把「调用方命名被测二进制」的契约拿去复用了。门禁 `internal/standards/makefile_caller_env_gate_test.go` 让这两个名字不能再次合流。

## 复现 / 复核命令（本 run 实测）

```bash
EV=<this run's session dir>/artifacts
CGO_ENABLED=0 go build -o "$EV/gopi" ./cmd/agent/

# 根因 1（带 pin，修后：SKIP + exit 0；修前：FAIL + exit 1）
GOPI_BIN="$EV/gopi" go test ./internal/testutil/ -count=1 -run 'TestGopiBin' -v

# 契约另一侧（无 pin：必须 PASS，证明 skip 是条件性的）
env -u GOPI_BIN go test ./internal/testutil/ -count=1 -run '^TestGopiBinPublishesTheTreeBuildToChildren$' -v

# 根因 2 端到端（修 #2 前 exit 1；修 #2 后 exit 0）
GOPI_BIN="$EV/gopi" make test-budget PKGS='-run TestGopiBin ./internal/testutil'

# 根因 2 的修复自带的门禁
go test ./internal/standards/ -run 'Makefile|CallerEnv' -count=1 -v
```

## 未决 / 边界

- fix#2 在本 row 的分支上（`f924ec4a1`，cherry-pick of `cc6329a65`），**合并不由本 run 完成**。
- `internal/testutil` **整包**仍不是全绿，且与本主题无关：`TestEveryTempUsingSuiteIsPinned` 因 `internal/smokegate` 无 `TestMain` pin scratch root 而红（Rule 22）——已单独提行 `01a12d3f-be40-7604-97bd-3ab7394cd78a`。
- 本文件记录的是**本 run 实测到的**事实；`#9211`/`#9223` 两条记忆只用于**定位**（省掉重新发现两条修复各在何处），判据一律是本 run 自己的命令输出。
