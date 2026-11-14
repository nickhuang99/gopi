<!-- doc-class: historical -->
# Rule 18 门禁命中：run_ground_anchor.go 的三处 `} else if`（task 01a17582-944a-77d2-8e5b-798efce67ea2）

<!--
记录（task 01a17582-944a-77d2-8e5b-798efce67ea2，ground = clone，branch master，
cut point `dee5c41`）。文中读数是**那一次测量**的快照，不是对当前 master 的断言。
-->

## 现象（实测）

干净 master（本 ground 的 cut point）上 Rule 18 门禁本来就是红的，与任何分支无关：

```
$ go test ./internal/standards/ -run 'TestStandardGates/TestGate3ElseAfterReturnRedProof' -count=1
--- FAIL: TestStandardGates/TestGate3ElseAfterReturnRedProof
    gate_test.go:107: Rule 18 (no else after terminating if): 5 total hits, 0 baselined, 2 escaped, 3 new (unannotated), 0 baseline entries
    gate_test.go:119: NEW violation ... internal/tools/run_ground_anchor.go  } else if !ok {  (run_ground_anchor.go:710)
    gate_test.go:119: NEW violation ... internal/tools/run_ground_anchor.go  } else if root != "" {  (run_ground_anchor.go:762)
    gate_test.go:119: NEW violation ... internal/tools/run_ground_anchor.go  } else if why := reclaimDirtyVerdict(porcelain); ...  (run_ground_anchor.go:772)
```

## 根因（不是逻辑，是 Go 的语句作用域）

三处 `else` 都不是分支决策，而是**为了让 init 语句里的变量在 `else` 体里可见**：

| 位置 | 写法 | `else` 存在的唯一原因 |
|---|---|---|
| 710 | `if ok, rerr := devworkspace.Reclaim(dir); rerr != nil { … } else if !ok { … }` | `ok` 绑在 if 语句上，else 体才看得见 |
| 762 | `if root, source, err := config.WorkspaceRootConfigured(); err != nil { … } else if root != "" { … }` | `root`/`source` 同理 |
| 772 | `if porcelain, derr := reclaimGitStatus(dir); derr != nil { … } else if why := reclaimDirtyVerdict(porcelain); … { … }` | `porcelain` 同理 |

两个 body 本身都在末尾终止（`return` / `continue`），所以 else 是纯语法产物。
正解是**把调用提升为具名语句**，变量留在函数作用域，`else` 消失，两条平铺的
guard clause 语义逐字等价 —— 这是本仓库一贯的修法（Rule 18 的检查项就是它）。

## 修法（零语义变更）

```
-			if ok, rerr := devworkspace.Reclaim(dir); rerr != nil {
+			ok, rerr := devworkspace.Reclaim(dir)
+			if rerr != nil {
 				skipped = append(...)
 				continue
-			} else if !ok {
+			}
+			if !ok {
```

`runGroundCollectCloneVerdict` 里两处同形：

- `root, source, rerr := config.WorkspaceRootConfigured()` + `if rerr != nil { return … }` + `if root != "" { … }`
- `porcelain, derr := reclaimGitStatus(dir)` + `if derr != nil { return … }` + `if why := reclaimDirtyVerdict(porcelain); why != reclaimStatusClean { … }`

命名约定跟文件里既有的走（同族函数用 `rerr` / `derr`），错误消息字符串、
注释文本、控制流一律未改。**`internal/standards/baseline.go` 一个字都没动** ——
permit 只能 ratchet down，新增 permit 本身就是 error，而且这里根本不需要它：
else 是可消除的语法产物，不是真实的控制流依赖，所以也没有用 `// else-ok:` 逃生。

## 验证（实测）

```
$ go test ./internal/standards/ -run 'TestStandardGates/TestGate3ElseAfterReturnRedProof' -count=1
ok  	github.com/nick/gopi/internal/standards	1.996s

$ go test ./internal/standards/ -count=1          # 整包，见 task-report 的 Tests:
$ go test ./internal/tools/ -run 'RunGround|GroundAnchor|CloneVerdict' -count=1
$ CGO_ENABLED=0 go build ./cmd/agent/            # exit 0
$ go vet ./internal/tools/                        # exit 0
$ gofmt -l internal/tools/run_ground_anchor.go    # 空
```

## 为什么值得记

1. **`else if` 的第一嫌疑永远是作用域，不是逻辑。** 看到 `if x, err := f(); err != nil { … } else if …`
   这种形状，先问"`x` 在 else 里被用到吗"，用到就提升调用 —— 一步就消掉。
2. **master 上的红灯要先归因再动手。** 本 run 在干净 ground 上重测过，红是
   预先存在的，与本次改动无关；报告里必须写这一点，否则会被读成"这次改动弄红的"。
3. **门禁的行号会随编辑漂移。** 修完必须重跑门禁拿新读数，不要拿旧的 710/762/772
   去对照新文件的行号。
