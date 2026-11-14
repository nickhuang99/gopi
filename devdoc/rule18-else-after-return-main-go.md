<!-- doc-class: normative -->
# Rule 18 门禁命中：Go 的 if 语句作用域被误当成分支决策（cmd/agent/main.go）

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

<!--
记录（task 01a13378-768f-7cd0-b6a0-38dae981a518）。文中读数是**那一次测量**的快照，
不是对当前 master 的断言。
-->

## 现象（实测）

**本 run 的 base 线** `2e5b4b942` 上，`internal/standards` 的 Rule 18 门禁是红的；
**master（`60cf0a4d5`）是绿的** —— 同一份门禁代码、同一个 `cmd/agent/main.go`，
只是两个版本的这个分支链形态不同（差异见文末「归属更正」）：

```
go test ./internal/standards/ -count=1 -run 'TestStandardGates/TestGate3ElseAfterReturnRedProof'
base 2e5b4b942 的 main.go  → FAIL，NEW violation at cmd/agent/main.go:228
master 60cf0a4d5 的 main.go → PASS（2 total hits, 0 baselined, 2 escaped, 0 new）
```

同批的 `TestStandardGateDataReport` 在 base 上读出 `Rule 18 … hits=3 … NEW=1`（其余规则 NEW=0）。

## 根因：这不是分支决策，是 Go 的语句作用域

被点名的形状：

```go
if sentence, refuse, applied, deferred := workerControlSelectionGate(args[1:], opts.action); refuse {
        fmt.Fprint(os.Stderr, sentence)
        os.Exit(workerConflictExitCode)
} else if applied {
```

Go 的 `if` 初始化语句，其作用域**横跨 if/else 两个块**。`workerControlSelectionGate`
返回四个值，作者想让 `applied` / `deferred` 在后续分支里可见，于是只能把它们挂在
`else if` 上 —— `else` 在这里**不表达任何分支决策**，它只是变量的可见性容器。

所以正解不是给 `else` 找理由（`// else-ok:` 逃生）也不是往 `internal/standards/baseline.go`
加 permit（棘轮只许向下，新增 permit 本身即 error），而是**把调用提升（hoist）为具名语句** ——
值留在块作用域里，`else` 自然消失。

引入者（pickaxe 定位）：

```
git log --all --oneline -S 'sentence, refuse, applied, deferred :=' -- cmd/agent/main.go
→ 749d35a1c fix(worker): defer a bare resume-ground selector to the resume face
  (Tue Oct 13 10:57:44 2026 +0800)
```

`749d35a1c` 是**本命中唯一的引入者**（更正见文末「归属更正」一节：早先此处写
「749d35a1c 在 master 上只引入了 3 个返回值、第四个值随后落地」，**该论断已被实测推翻**）：

```
$ git merge-base --is-ancestor 749d35a1c master      → NO  (exit 1)
$ git merge-base --is-ancestor 749d35a1c 2e5b4b942   → YES (exit 0)
$ git show master:cmd/agent/main.go | grep -n 'workerControlSelectionGate\|} else if'
  207:  if sentence, refuse, applied := workerControlSelectionGate(args[1:], opts.action); refuse {
  210:  } else if applied {
$ git show 749d35a1c:cmd/agent/main.go | grep -n ...   → 216 / 219 / 228（含 `} else if !deferred {`）
$ git show 749d35a1c -- cmd/agent/main.go   → 同一个 hunk 同时：+`deferred` 第四值、+`} else if !deferred {`
```

也就是说：`749d35a1c` 这一个 commit **同时**引入第四个返回值与 `} else if !deferred {`，
它 **不是** master 的祖先（只在本任务分支与兄弟分支上）。master `60cf0a4d5` 上
`cmd/agent/main.go` 停在 **3 值 + `} else if applied {`** 的旧形式 —— 该旧形式
**不是**命中（换进去实测：`2 total hits, 0 baselined, 2 escaped, 0 new` → PASS），
机理见文末「归属更正」。**master 是绿的；红只在本 run 的 base 线上。**

## 修复形状（de-indent，零语义变更）

```go
sentence, refuse, applied, deferred := workerControlSelectionGate(args[1:], opts.action)
if refuse {
        fmt.Fprint(os.Stderr, sentence)
        os.Exit(workerConflictExitCode)
}
if applied {
        … workerControlCommand(args[1:]); return true
}
if !deferred {
        …
}
```

三条平铺 guard clause，控制流逐条等价：

- `refuse` → stderr 那句 + `os.Exit(workerConflictExitCode)`（哨兵：`os.Exit` 不返回）；
- 否则 `applied` → `workerControlCommand` + `return true`；
- 否则 `!deferred` → status 路由或 `workerControlCommand` + `return true`；
- 再否则（deferred）落到 `workerControlCommand` 之外的扫描器，与 `else` 链的落点相同。

消息字符串、注释、缩进后的相对结构逐字未动；改动仅 `cmd/agent/main.go` 一个小 hunk
（`-2 / +4` 行净）。
`internal/standards/baseline.go` 未改（`git diff 2e5b4b9..HEAD -- internal/standards/baseline.go` 为空），
也没有新增 `// else-ok:` 逃生。

## 验证读数

红 → 绿（两条命令都在本 worktree 实跑，读数原样抄录）。

**修前**（`git show HEAD:cmd/agent/main.go > cmd/agent/main.go` 把该文件退回 base `2e5b4b9`，
measure 完立刻用备份还原；还原判据 = `diff` 逐字节相同）：

```
go test ./internal/standards/ -count=1 -run 'TestStandardGates/TestGate3ElseAfterReturnRedProof' -v
  gate_test.go:103: Rule 18 (no else after terminating if): 3 total hits, 0 baselined, 2 escaped, 1 new (unannotated), 0 baseline entries
  gate_test.go:115: Rule 18 (no else after terminating if): NEW violation (checklist 4.3) at cmd/agent/main.go:228
                        [else after a terminating if-body — drop the else and de-indent (early return)]
  --- FAIL: TestStandardGates (1.31s)
  FAIL
```

**修后**：

```
go test ./internal/standards/ -count=1 -run 'TestStandardGates|TestStandardGateDataReport'
→ PASS  ok  github.com/nick/gopi/internal/standards  47.574s
  gate_test.go:128: Rule 18 (no else after terminating if) (checklist 4.3): hits=2 baselined=0 escaped=2 NEW=0
  （escape 的两处是既有的 internal/lineedit/editor.go:1703 与 internal/tools/memory.go:3761；
    main.go 已不在命中里，NEW 由 1 → 0，baselined 恒为 0）

CGO_ENABLED=0 go build ./cmd/agent/   → BUILD OK
```

`internal/standards/baseline.go` 未被修改：

```
git diff 2e5b4b9..HEAD -- internal/standards/baseline.go   → 空
```

## 运行环境注记（与本修复无关）

本 worktree 的 base `2e5b4b9` 上，`./cmd/agent` 已有 3 条 pre-existing 红
（`TestPublishedHelpPagesStayWithinTheirUsageBudget/worker`、`TestToolsMaintenanceHelpSurface*`），
所以该包整包读数是 FAIL —— 与 `main.go` 这一处无关。归属用 A/B 证明：把 `main.go` 退回 base
再跑同一 `-run` 选择，红点逐字相同（`artifacts/cmd-agent-precheck-{post,base}.txt`）。
`TestBareRunKeepsAllItsArtifactsInOneSessionDir` 是测量时序的假阳性：它比较测试二进制与
`cmd/agent/main.go` 的 mtime，而上面的退回/还原恰好改写了该文件的 mtime；单独跑它是绿的。



## 行为不变的实测证据（Rule 31）

`cmd/agent/main.go` 是 `gopi worker` 控制面的路由入口，所以真跑被改面：
同一 worktree 分别构建 **post** 二进制与 **pre** 二进制（`cp` 备份 `cmd/agent/main.go`
→ `git show HEAD:cmd/agent/main.go > cmd/agent/main.go` 退回 pre 状态后构建
→ `cp` 备份还原、`diff` 逐字节验证），对同一组真实调用逐字节对比
stdout+stderr+退出码。**注意：本环境用不了 `git stash`** —— 该仓库有多个 linked worktree
共享同一个 `refs/stash`，环境的危险命令守卫会直接拒绝（`rule18-ab.sh` 里仍留着 stash 版本，
实际执行的是 cp 版本）。

| 场景 | 覆盖的 arm |
|---|---|
| `gopi worker --status` | `!deferred` → status 枚举 |
| `gopi worker --status --stop` | `refuse`（读+act 冲突，F1 缺陷的拒绝面） |
| `gopi worker --pid 999999 --status` | `applied` → `workerControlCommand` |
| `gopi worker --branch` | `deferred` → 落到扫描器（resume 面自己的拒绝） |
| `gopi worker --branch master --status` | `applied` + 选不中的 target |
| `gopi worker -h` | usage |

`diff post.txt pre.txt` 为空、两份 sha256 相同 —— 见
`~/.gopi/sessions/01a13378-768f-7cd0-b6a0-38dae981a518/artifacts/`（`ab-post.txt` / `ab-pre.txt`）。

## 教训

Rule 18 的命中**通常不是**"作者想写 else"，而是"作者的变量需要跨 if/else 可见"。
诊断顺序：先看 `if` 的 init 语句里有没有函数调用、它的返回值有几个被后续分支用到 ——
有，就是作用域问题，hoist 即可，不必论证 else 的必要性。
动手前先 `git log --all -S '<改动后的调用形式>' -- <file>` 查 master 是否已独立修过同一处
（本仓库已发生过两次逐字节重复修复）。

## 归属更正（Rule 41：用错的依据必须当场撤回）

本文件早先写过两句**推断**，两次实测都把它们的**细节**推翻了（结论不变，但依据全错）。
原文与更正并列如下，不删原文：

| 早先的论断 | 实测（命令 + 原始输出见 `artifacts/attribution-remeasure.txt`、`artifacts/rule18-gate-vs-master-main-go.txt`） | 判定 |
|---|---|---|
| 「`749d35a1c` 在 **master** 上只引入了 3 个返回值；`deferred` 第四值与 `} else if !deferred {` **随后落地**」 | `git show 749d35a1c -- cmd/agent/main.go` 的**同一个 hunk** 里同时出现 `+deferred` 与 `+} else if !deferred {`；且 `git merge-base --is-ancestor 749d35a1c master` → **exit 1（非祖先）** | **撤回**：单一引入者就是 `749d35a1c`，不存在「随后落地」的第二个 commit |
| 「master `c5306a2` 上 Rule 18 门禁是红的（与本 run 无关的 pre-existing 违规）」 | 把 `cmd/agent/main.go` 换成 **master `60cf0a4d5`** 的那份再跑 `-run TestStandardGates/TestGate3ElseAfterReturnRedProof`：`2 total hits, 0 baselined, 2 escaped, 0 new` → **PASS**。同一条命令换回 **base `2e5b4b942`** 的那份：`3 total hits, … 1 new (unannotated)` + `NEW violation at cmd/agent/main.go:228` → **FAIL** | **整条撤回**：master 是**绿的**。红只存在于本 run 的 base 线上 |

**为什么会红、master 为什么不红**（同一份门禁代码，`internal/standards/gate.go` 的
`elseAfterReturnFind`：只有「else 前面的 if-body **以终止语句收尾**」才算命中）：

```go
// master 60cf0a4d5 — 链到此为止：else-if 体虽以 return true 收尾，但链后面没有 else
} else if applied {
        workerControlCommand(args[1:])
        return true
}                                    ← 链结束，没有后继 else ⇒ 不构成 else-after-terminating
if workerStatusRequested(args[1:]) { … }

// base 2e5b4b942（= 749d35a1c 的形态）— 同一个 return true 后面挂上了 else
} else if applied {
        workerControlCommand(args[1:])
        return true
} else if !deferred {                ← ← 这就是 :228 的命中行
```

所以：**master 从未红过**；红是本 run base 线上 `749d35a1c` 的引入造成的。
`c5306a2` 只是本 run **base 线**上的一个 merge commit（早先误写为 "master `c5306a2`"）：
`git rev-parse --short master` → `60cf0a4d5`，本 run 的 base `2e5b4b942` 与它
**不是同一条线**（`git rev-list --count master..2e5b4b942` → `74`，方向是 base **领先**本地 master 引用）。

结论不变（这处就是要 de-indent），但**依据只能是上表的实测行**，不能是那两句推断
（Rule 41 第 3 要件：推断不得充当判据）。
