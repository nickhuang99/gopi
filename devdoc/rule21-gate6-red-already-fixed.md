<!-- doc-class: historical -->
# Rule 21 Gate6 那条红是 ALREADY FIXED：修复 commit `cea1dc32c` 早于上报者的 base

Task `01a142db-d843-7440-93d7-6580ce006163`，branch
`task/01a142dbd843744093d76580ce006163`，本 ground 的 cut 点 `94210fe`（2026-10-16 18:37:40）。

## 上报的内容与本 run 的实测

上报行（`01a142db-d843…`，2026-10-16 11:57:46 创建）说：

> master's Rule-21 env-reader gate is RED at `internal/testutil/gitenv.go:158`
> (`pinSuiteGitRoutingEnvName` has no reader)，base `61b08468322b83c398ec617afeb4120f25692ef4`。

**上报本身没有错。** 在它点名的 base 上，那行红是真的，而且症状逐字对得上 ——
本 run 在独立 clone 里签出 `61b0846` 实跑，拿到：

```
--- FAIL: TestStandardGates (2.06s)
    --- FAIL: TestStandardGates/TestGate6EnvReaderRedProof (2.06s)
        Rule 21 (env-name constant must have a real reader): 1 total hits, 0 baselined,
            0 escaped, 1 new (unannotated), 0 baseline entries
        Rule 21: NEW violation (checklist 1b/env) at internal/testutil/gitenv.go:158
            [env constant pinSuiteGitRoutingEnvName (GOPI_TEST_GIT_ROUTING_ENV) has no
            environment reader — a hook with no consumer reads as a working control surface;
            wire it or delete it]
```

而 `61b0846` 的 `internal/testutil/gitenv.go:158` 正是那一行声明 ——
即上报的行号、常量名、变量名三项全中：

```
$ git show 61b08468322b83c398ec617afeb4120f25692ef4:internal/testutil/gitenv.go | grep -n pinSuiteGitRoutingEnvName
150:// pinSuiteGitRoutingEnvName is the TICKET that lets a witness undo the process
158:const pinSuiteGitRoutingEnvName = "GOPI_TEST_GIT_ROUTING_ENV"
173:// see RestoreGitRoutingEnv and pinSuiteGitRoutingEnvName.
```

## 结论：本 run 的正确交付是「不改代码」

**修复已经落地，且早于上报者的 base —— 是上报者落后，不是 master 落后。**

| 事实 | 读数 |
|---|---|
| 修复 commit | `cea1dc32c` *fix(testutil): delete the dead GOPI_TEST_GIT_ROUTING_ENV hook (Rule 21 Gate6)*，`internal/testutil/gitenv.go` +16/−10，2026-10-16 **12:43:17** |
| 上报者的 base | `61b0846`，2026-10-16 **11:23:35** |
| `cea1dc32c` 是 `61b0846` 的祖先？ | **NO** |
| `cea1dc32c` 是 `94210fe`（本 ground HEAD）的祖先？ | **YES** |
| `cea1dc32c` 是 `refs/heads/master` `7b3196d` 的祖先？ | **YES** |
| `cea1dc32c` 是 `refs/remotes/origin/master` `7b3196d` 的祖先？ | **YES** |
| `cea1dc32c` 是 `refs/remotes/gitPushcheck/master` `94210fe` 的祖先？ | **YES** |
| `61b0846` 是 `94210fe` 的祖先？ | **YES**（即上报的 base 是本 ground 的历史，修复发生在它之后） |

时间顺序即全部：上报 11:57 提行时读的是 11:23 的树，修复 12:43 落地。
**在三个 master 名的 ref 上，那条 gate 都是绿的** —— 所以这不是「修复落在某个合并基
不带的分支上」，而是纯粹的**时间差**。

修的内容也正是 epic 预设的形状（**删除**，不是接线）：`const` 声明没了，`PinSuiteGitEnv`
文档里那句 `see RestoreGitRoutingEnv and pinSuiteGitRoutingEnvName` 改成
`see RestoreGitRoutingEnv`，原处留下一段**墓碑注释**点名字符串，
使重新引入可被 grep 抓住、后来者知道这个接口为什么不存在。

### 本 run 触碰面：0 个 Go 文件

```
$ git diff --stat 94210fe..HEAD
                                    # 空 —— 未改任何生产字节（未提交 commit）
$ grep -rn "pinSuiteGitRoutingEnvName" --include='*.go' .
./internal/testutil/gitenv.go:152:// declared `const pinSuiteGitRoutingEnvName = ...` as a    ← 墓碑注释
$ grep -rc "^const pinSuiteGitRoutingEnvName" --include='*.go' . | grep -v ':0' | wc -l
0                                   # 全树 0 处可用声明
```

在本 ground 上再"修"一次等于**重做已落地的修复**，并把 `cea1dc32c` 刚删掉的死常量
**装回去** —— 一个没有消费者的 hook（Rule 21 / Rule 15 的静默失败面），是回归不是修复
（Rule 35：修法必须是删除；接一个只为了给名字找理由的消费者 = 新增依赖）。

## 可复用配方：判「一条门禁红是否已被修好」

不要读代码、不要读 diff、不要信 `git log -S` 的命名 —— **在对象存储上对照两个 rev，各自实跑一次门禁**：

```bash
# 1) 红臂：在独立 clone 里签出上报点名的 rev（Rule 29：绝不为了看代码去动别人的 worktree）
R=$(mktemp -d /home/user/workspace/tmp-redproof-XXXX)
git clone -q --no-hardlinks --shared . "$R/gopi" && cd "$R/gopi"
git checkout -q <report-base-sha>
go test ./internal/standards/ -run 'TestGate6EnvReaderFindsTheRealTree' -count=1   # 期望 FAIL + 命中行
go test ./internal/standards/ -run 'TestStandardGates/TestGate6EnvReaderRedProof' -v -count=1

# 2) 绿臂：本 ground 的 HEAD 上同一条命令
cd <this ground> && go test ./internal/standards/ -run 'TestStandardGates/TestGate6EnvReaderRedProof' -v -count=1

# 3) 祖先关系：每一个 master 名的 ref 都要单独读数（本树有三个）
for r in refs/heads/master refs/remotes/origin/master refs/remotes/gitPushcheck/master; do
  printf '%s %s fix-ancestor=%s\n' "$r" "$(git rev-parse --short $r)" \
    "$(git merge-base --is-ancestor <fix-sha> $r && echo YES || echo NO)"
done
```

### 两个坑

- **`go test ./internal/standards/` 的包级 `FAIL` 不等于你的红还在。** 本 ground 上该包仍然
  `FAIL`，但红的是 `TestCommandsDocGeneratedBlockMatchesSource` 与
  `TestDocClassBannerMatchesManifest` 两条**与 Rule 21 无关**的测试（已各自有 pending 行进队）。
  判据必须是 **failing-subtest 集合的对照**，不是退出码 —— 并且最好用 `-run` 精确打到你那一条。
  绿的 gate 一行命中都不打印，所以「没有那条 Rule 21 命中行」本身就是通过信号。
- **`bash` 会在 2 分钟无输出时杀掉一个前台调用，而 `make test-budget` 是缓存的** ——
  用 `job(action=start)` 起、`job(action=output)` 收一次。本 run 实测该包一次完整跑 97s。

## 验收（本 ground 实测）

```
$ go test ./internal/standards/ -run 'TestStandardGates/TestGate6EnvReaderRedProof' -v -count=1
    gate_test.go:105: Rule 21 (env-name constant must have a real reader):
                      0 total hits, 0 baselined, 0 escaped, 0 new (unannotated), 0 baseline entries
--- PASS: TestStandardGates (1.99s)
    --- PASS: TestStandardGates/TestGate6EnvReaderRedProof (1.99s)
PASS  ok  github.com/nick/gopi/internal/standards  2.376s

$ go test ./internal/standards/ -run 'TestGate6EnvReaderFindsTheRealTree' -v -count=1
    gate_test.go:720: Rule 21: hits=0 baselined=0 escaped=0 NEW=0
--- PASS: TestGate6EnvReaderFindsTheRealTree (2.18s)

$ git merge-base --is-ancestor cea1dc32c 94210fe   → exit 0 (YES)
$ git merge-base --is-ancestor cea1dc32c 61b0846    → exit 1 (NO)
```

## 诚实范围

- 本 run **没有**修 `internal/standards` 包剩下的两条红（COMMANDS.md 生成块行漂移、
  `removal-audit-shell-census.md` 的 banner/manifest 冲突）。它们与 Rule 21 无关，
  且已在队列里（`01a1432e-4416` / `01a1432e-4441` / `01a14412` / `01a14384`），
  Rule 41 下不重复提行。
- 本文与另一份同结论的记录 `devdoc/standards-gate-reds-already-fixed.md`（task
  `01a141993d6d77148535c61b76d5471b`，cut `cdc4970`）**同结论、不同取证方式** —— 两份都保留，
  因为它们证明的强度不同，而这一点值得被后来者看见：
  - **那一份**：cut `cdc4970`，它点名的 rev 是 `9faef4015`，红臂是**静态读**（`git show <rev>:file | grep`
    看见声明还在）。结论正确，但「红」是从文件内容**推断**出来的。
  - **本 run**：cut `94210fe`，点名的 rev 是**上报者自己的 base `61b0846`**，红臂是**执行级** ——
    在独立 clone 里 `git checkout 61b0846` 后**实跑门禁**，拿到逐字失败输出；绿臂同理在 HEAD 实跑。
    并为**本 ground 的钉死入口** `make test-budget PKGS='./internal/standards'` 留了一份完整 run stream
    （200 arms，`gitenv.go:158` 命中 0 次）。
  - 差别不是措辞：Rule 31 的立场是「没被执行的面不算交付」。一份**静态读出的红**和一份**跑出来的红**，
    在前者是假红（例如旧行号只是注释）时结论会分叉。两份并存，后来者可以选强度更高的那一份引用。
