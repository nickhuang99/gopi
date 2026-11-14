<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# 两条 standards 门禁红是 ALREADY FIXED：修复已在 cut 点 cdc4970 之内

Task `01a141993d6d77148535c61b76d5471b`，branch `task/01a141993d6d77148535c61b76d5471b`，
cut 点 `cdc4970`（本 run 的 HEAD），2026-10-16。

## 发现

epic 的内容是「在 `internal/standards` 上消掉两条红」：

1. **Gate6**（Rule 21：env 常量必须有真实 reader）在 `internal/testutil/gitenv.go` 的
   `pinSuiteGitRoutingEnvName` / `GOPI_TEST_GIT_ROUTING_ENV` 上报 NEW 命中；
2. **Gate9**（git-env 门禁）baseline 含 stale 条目 `cmd/agent/cli_tasks_help_surface_basic_test.go:1365`，
   门禁要求 ratchet down。

**两条红在 epic 点名的 commit `9faef4015` 上都是真的**（执行级取证，见文末），前提本身没有错。
但**两处修复早已落在 master 上，并且已含于本 ground 的 cut 点**：

| 红 | 修复 commit | 内容 | `merge-base --is-ancestor <c> HEAD` |
|---|---|---|---|
| Gate6 | `cea1dc32c` | *fix(testutil): delete the dead GOPI_TEST_GIT_ROUTING_ENV hook (Rule 21 Gate6)* | **YES** |
| Gate9 | `c8348de04` | *test(standards/gate9): rename the coverage arm…*（`--stat` 含 `internal/standards/baseline.go \| 32 +++-`） | **YES** |

`9faef4015` 本身也是本 ground HEAD 的祖先（YES）—— 即两处修复都发生在它之后、cut 点之前。

## 结论：本 run 的正确交付是「不改代码」

在本 ground 上"修"这两处等于**重做已落地的修复**：Gate6 那条还会把 `cea1dc32c` 刚删掉的死常量
**重新装回去**（一个没有消费者的 hook —— Rule 21 / Rule 15 的静默失败面），是回归而不是修复。

本 run 因此**触碰 0 个 Go 文件、0 个 gate 文件、0 条 baseline 条目、0 处 escape 注释**，
只落一份记录（本文）与一份报告。

## 一个可复用的取证配方

判断「一条门禁红是否已经被修好」不要靠读代码或靠 `git log -S` 的命名，要在**对象存储上对照两个 rev**：

```bash
# 1) 红的样子：从对象存储读 epic 点名的 rev（不签出 —— Rule 29）
git show 9faef4015:internal/testutil/gitenv.go        | grep -nE 'pinSuiteGitRoutingEnvName|GOPI_TEST_GIT_ROUTING_ENV'
git show 9faef4015:internal/standards/baseline.go     | grep -n 'cli_tasks_help_surface_basic_test.go:1365'
#    → 150:const pinSuiteGitRoutingEnvName = "GOPI_TEST_GIT_ROUTING_ENV"
#    → 964:\t"cmd/agent/cli_tasks_help_surface_basic_test.go:1365",     ← 真条目

# 2) 现在（cut 点）的样子
git show HEAD:internal/testutil/gitenv.go             | grep -nE 'pinSuiteGitRoutingEnvName|GOPI_TEST_GIT_ROUTING_ENV'
#    → 只有 151/152 的墓碑注释
grep -nE '^[[:space:]]*(const|var)[[:space:]].*pinSuiteGitRoutingEnvName' internal/testutil/gitenv.go ; echo $?
#    → exit=1（本树不存在可用声明）
git show HEAD:internal/standards/baseline.go          | grep -n 'cli_tasks_help_surface_basic_test.go:1365'
#    → 只剩第 967 行注释里的叙述，不是 gitEnvBaseline 条目
```

**两个坑，本 run 都踩到并绕开了**：

- **`git log -S` 会点错作者**。`git log -S'"cmd/agent/cli_tasks_help_surface_basic_test.go:1365"' --
  internal/standards/baseline.go` 把该行的移除记在 `6b6a4fe3f` 名下，但
  `git show --stat 6b6a4fe3f` 的 diff **不含** `internal/standards/baseline.go`，且该 commit 的父上这行已是 0 ——
  它只是把修复带下来的**祖先/合并**。判「谁改的」要看 `--stat`/`--format=` 的 diff，不要信 `-S` 的命名。
- **`grep` 命中的是注释，不是代码**。HEAD 的 `baseline.go` 里 `cli_tasks_help_surface_basic_test.go:1365`
  仍然出现一次 —— 在第 967 行的**解释性注释**里（说明它当初为何被删）。判「条目还在不在」必须看
  `gitEnvBaseline` 那个 `[]string` 字面量内部，而不是整文件的 grep 计数。

## 验收（本 ground 实测）

```
$ go build ./...                                                    → exit 0
$ go test ./internal/standards/ -run 'TestGate' -count=1
ok  github.com/nick/gopi/internal/standards  21.540s                 → exit 0
$ go test ./internal/standards/ -run 'TestGate6EnvReaderFindsTheRealTree' -count=1 -v
    gate_test.go:720: Rule 21 (env-name constant must have a real reader): hits=0 baselined=0 escaped=0 NEW=0
--- PASS: TestGate6EnvReaderFindsTheRealTree (1.50s)                 → exit 0
$ go test ./internal/standards/ -run 'TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly' -count=1 -v
    git-env gate: 1913 test files scanned, 73 hit(s), 72 baseline entries
--- PASS: TestGate9GitEnvCoverageArmFloorAndStaleEntriesOnly (0.48s) → exit 0
```

`73 hit(s) / 72 baseline entries` 两半相等 = **既无 stale 条目，也无未基线化的新命中**（两个方向同时判）。

## 诚实范围（与本 epic 无关、但同包的一处红）

`internal/standards` 包在 pristine 树上仍有一处红，**不被 `-run 'TestGate'` 匹配**，本 run 未触碰其文件：

```
docclass_gate_test.go:214: devdoc/removal-audit-shell-census.md: carries the "historical" banner
    but devdoc/DOC-INDEX.tsv classes it "normative"
FAIL: TestDocClassBannerMatchesManifest
```

它已在 2026-10-16 13:27:48 被另一 run 提为 pending 行 `01a1432e-4441-71a6-bd4c-f94fce6cbe11`
（Rule 41 下不重复提行）。同类的「banner 与清单打架」历史上有过一串（`01a136be` / `01a136b6` /
`01a12eed` …），修复形状是**先 `python3 tools/docclass/classify.py <repo> --only <path>` 重生成行，
再同步文件首行的 banner**，两个半面在同一个 commit 落地。
