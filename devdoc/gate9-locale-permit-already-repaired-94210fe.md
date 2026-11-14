<!-- doc-class: historical -->
# 报告：Gate9 git-env 红在 cut 点 94210fe 已不存在 —— 修复早已落地

Task `01a1430847747ec9b22d24636a6f9b75`，branch `task/01a1430847747ec9b22d24636a6f9b75`，
cut 点/HEAD `94210fe`，2026-10-16。

## 归因结论：先查，再决定要不要写代码

epic 说本 cut 点上 Gate9（Rule 13/15, git-env）是红的：一条 baseline permit 键在
`internal/tools/git_child_locale_test.go:243` 上「腐坏（+5）」，而活命中在 `:248`，
于是同一处缺陷被两条臂同时报出（NEW + STALE）。**在 `94210fe` 上不成立。**

| 判据（都在本 cut 点实测） | 读数 |
|---|---|
| `go test ./internal/standards/ -count=1 -run 'TestStandardGates/TestGate9GitEnvRedProof'` | `ok`（子测试真的跑了，见下） |
| 该臂的读数行 | `73 total hits, 72 baselined, 1 escaped, 0 new (unannotated), 72 baseline entries` |
| `go test ./internal/standards/ -count=1 -run 'TestStandardGates$'`（**全臂**，含 stale 检查） | `ok  … 50.637s` |
| `go test ./internal/standards/ -count=1 -run 'TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly'` | `ok` |
| `git merge-base --is-ancestor de5b3315c HEAD` | **YES** —— 重叠分支的修复已在我的祖先里 |
| `git show HEAD:internal/tools/git_child_locale_test.go \| grep -n 'control.Env'` | `258:	control.Env = testutil.GitEnv()` |
| `gitEnvBaseline` 条目里含 `git_child_locale` 的行 | **0 条**（`baseline.go` 里唯一那次 grep 命中是 `:995` 的**叙述注释**，不是条目） |

**STALE 不可能存在的判据是硬约束**：permit 与 `rel:line` 一一对应，`:243` 这条已经不在了 ——
没有条目就无所谓「条目腐坏」。epic 描述的 `:243` vs `:248` 是**修复之前**的状态。

## 落地的时刻与 commit

- 修复本身：`de5b3315c`（`fix(standards): clear Gate6 and Gate9 by deleting a dead env
  constant and repairing the git-env locale control`），2026-10-16 12:30。
- 并入 master：`cdc497090 Merge branch 'task/01a142df476f71188e4df1d246966d49'`，13:10。
- 本 ground 的 cut `94210fe`（`Merge branch 'task/01a142dadada71bfb5ecb259f59a8761'`）在其**之后**。

**棘轮读数（从对象存储的两个 rev 量，不手数）**：

```
14861c1   SIZE 73   locale ['internal/tools/git_child_locale_test.go:243']   ← 修复前，正是 epic 描述的状态
94210fe   SIZE 72   locale []                                                 ← 本 cut 点，条目已删、站点已修
```

`73 → 72`，条目**严格下降**，没有新增行，也没有被重键（`git log -S'control.Env =
append(os.Environ()' --all` 在本 ground 的祖先里 **0 命中**，说明当时是**真修**而不是改行号）。

## 因此本 run 的交付是「不改代码」

在 `94210fe` 上把 `control.Env = testutil.GitEnv()` 改成任何别的东西、或往
`gitEnvBaseline` 里加回一行，都是**重做已落地的修复**：加行违反 Rule 13 的棘轮方向，
"修"站点则会把 `testutil.GitEnv()` 这个唯一正确 helper 换掉（Rule 35：不要造第二套）。
本 run **触碰 0 个 Go 文件、0 条 baseline 条目、0 个 escape 注释**，只落这份记录与报告。

## 非空洞性：locale 控制组仍然测得到东西

`TestRepoSweepGitPinsTheLocale` 的意义全在它的负控制组 —— 若 git 在本机**不**本地化，
断言只会因为「没有翻译可看」而通过，等于什么都没测。所以该控制组不是空洞的，实测两层：

```
$ go test ./internal/tools/ -count=1 -v -run TestRepoSweepGitPinsTheLocale
--- PASS: TestRepoSweepGitPinsTheLocale (0.07s)
```

真跑一次 git 自己的探针（与测试同 argv：`git fsck --unreachable --no-reflogs`，同一临时仓库）：

```
$ LC_ALL=C        git fsck --unreachable --no-reflogs      →  dangling commit be0d43fb…
$ LANG=zh_CN.utf8 LC_ALL=zh_CN.utf8 git fsck --unreachable →  悬空 commit be0d43fb…
```

两条输出**确实不同**（`dangling` → `悬空`），即本机 git **真的会本地化** ——
这正是控制组赖以存在的条件；测试在 `!localeInstalled(localeLocalized)` 时会 `t.Skipf`，
而本机装了 `zh_CN.utf8`，所以它是**真跑**而不是被跳过的绿。

## 一个可复用配方：判「这处红是否早已修好」

1. **先跑真臂**：`-run 'TestStandardGates/TestGate9GitEnvRedProof'`。名字是**子测试**，
   不带 `/` 的正则只匹配顶层名 —— 无斜杠的 `-run` 是**空转**（memory #11899）。
   绿不等于跑过：用 `-v` 确认 `--- RUN/PASS .../TestGate9GitEnvRedProof` 出现过。
2. **再跑全臂**：STALE 的判定在 `TestStandardGates` 本体里，窄正则可能不报它。
3. **读条目要看字面量，不要 grep 整文件**：`baseline.go` 里对某个 `file:line` 的 grep 命中，
   很可能落在**解释它为何被删的注释**里（本例 `:995`）。判据是 `var gitEnvBaseline = []string{…}`
   内部有没有那一行。
4. **棘轮大小从两个 rev 量**：`git show <rev>:internal/standards/baseline.go` + 解析
   `[]string{…}` 字面量，比手数可靠（Rule 38）。

## 诚实范围

- epic brief 里的旧读数（`b305c9817` 上的 `hits=74 baselined=72 escaped=1 NEW=1`）在本 cut 点
  **不再复现**，本 run 以 `94210fe` 的实测为准。
- 重叠分支 `task/01a142df476f71188e4df1d246966d49` 与 epic 描述的归因问题**已被
  `86a6fc6ab`**（*docs(standards): the two gate reds … were already fixed before this
  ground's cut*，文件 `devdoc/standards-gate-reds-already-fixed.md`）记过一次同类结论；
  本文是**本 cut 点、本次实测**的一份新记录，不是那份的副本。
- `internal/standards` 包在 pristine 树上仍可能有个别与本 epic 无关的红（例如
  docclass banner 与清单不一致那一族），不被本 run 的 `-run` 集合匹配，本 run 未触碰。
