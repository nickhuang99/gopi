<!-- doc-class: historical -->
# doccheck 的进程退出码：一条被管道伪造的读数

**页面性质**：一次运行（task `01a142ed23d37901971d1dc464421232`，2026-10-16）的实测记录
—— 一个 bug 行的裁决过程，以及它暴露的覆盖缺口。

## 结论（先给结论）

`gopi tool doccheck` 在**存有 stored-cell drift** 的树上：stdout 打印
`verdict: drift`，**进程退出码 = 1**。声称「打印 drift 却 rc=0」的那条 bug 行，
来自一次**管道内的读数**，不是工具的行为。

```text
$ gopi tool doccheck >/tmp/dc2.txt 2>&1; echo "rc=$?"     # 管道之外
rc=1
$ grep -n "verdict:\|stored-cell drift: [0-9]" /tmp/dc2.txt
51:  doccheck: stored-cell drift: 1 of 2588 rendered row(s) whose stored text is not what the classifier renders today
57:verdict: drift

$ gopi tool doccheck 2>&1 | tail -3; echo "rc=$?"         # 管道之内（原行的形状）
rc=0                                                       # ← tail 的状态
```

`$?` 在管道之后取的是**最后一个元素**（`tail`）的状态，与 `doccheck` 无关。
原始 bug 行的记录正是 `... | tail -15; echo "doccheck-rc=$?"`，它自己在正文里
把这一读数称为「管道之外单独测得」——**读数与它的描述相反**，而没有任何测试
能反驳它，因为（见下）退出码在当时没有任何一处被断言。

## 代码路径（从 class 到 os.Exit）

| 位置 | 事实 |
| --- | --- |
| `cmd/agent/cli_doccheck.go:608` | report 臂：`return docCheckVerdict(docCheckStoredDriftVerdictClass)` |
| `cmd/agent/cli_doccheck.go:641-645` | `docCheckVerdict` 由 class 解析 `v := docCheckVerdictForClass(class)`，打印 `verdict: <name>` 后 `return v.Code` |
| `internal/docclass/docclass_exit_codes.go:111-119` | **唯一的映射**：`""`→clean/0，`AnchorNumberNotMeasuredVerdictClass`→NOT MEASURED/4，其余→drift/1 |
| `cmd/agent/cli_doccheck.go:513` | `runDocCheckSubcommand` 收尾 `os.Exit(code)` |

存有 drift 时的 class 是 `docCheckStoredDriftVerdictClassDrifted`（不在映射的两个
特例里）→ `drift` / `DocclassExitFinding`(1)。三个通道（`verdict:` 行、进程状态、
臂的返回值）由**同一个 class** 驱动，因此在结构上不可能各自为政。

## 覆盖缺口（本行真正的缺陷）

这个包里的 doccheck 测试**全部**走 `runDocCheckInProcess`（`cli_doccheck_test.go:414`），
它调用 `docCheckReport`/`docCheckFix` 并**取返回值**。于是契约的最后一跳
——`runDocCheckSubcommand` 把 code 交给 `os.Exit` ——**在整棵树里没有任何断言**。
一个「打印 drift、返回 clean」的臂可以做到：所有 `TestDocCheck*` 全绿，
而操作者读到的进程状态是 0。

这是同一条腿第二次以同一形状出问题：`5b5f97903` 修的就是
「臂打印了实测 drift 然后 return 空串（= clean）」。当时的修复补上了**臂的返回值**
测试，却没有补**进程状态**的那一跳。`cmd/agent/doccheck_process_exit_test.go`
补的就是这一跳：

- 用仓库自己的生成器（`tools/docclass/classify.py` + `banner.py`）在 `t.TempDir()`
  里建一个真实夹具语料；
- 用**构建出来的真二进制**跑 `gopi tool doccheck --repo <fixture>`，
  从 `exec.ExitError.ExitCode()` 读**进程自己的**状态（不经过 shell，因此没有管道）；
- 断言两条通道一致：`verdict: clean` ↔ 0、`verdict: drift` ↔ 非零（且等于
  `docclass.DocclassExitFinding`），并排除 abstention 的状态；
- 绿色臂先行（干净语料 0/clean），否则「一律失败」也能让等式成立。

## 红证明（先红后绿，两次都实测）

在 `cli_doccheck.go` 的 report 臂里把这一格改回该行声称的形状
（打印 `verdict: drift` 之后 `return docclass.DocclassExitClean`），重建后：

```text
--- FAIL: TestDocCheckProcessExitStatusAgreesWithTheVerdictLine (2.82s)
    doccheck_process_exit_test.go:80: a corpus with a MEASURED stored-cell drift exited 0 while printing:
        doccheck: stored-cell drift: 1 of 3 rendered row(s) ...
        doccheck: this run exits 1 on stored-cell drift (...) (see the section above), independently of the 1 gap(s)
        verdict: drift
        — a caller gating on the status reads this as a pass (Rule 15).
```

被测文件哈希：`69fb8de221f327f50920ee8d82c726200d7435b9a80fe8b36aebfa22d47a9518`（红），
恢复后 `git hash-object cmd/agent/cli_doccheck.go` ==
`git rev-parse HEAD:cmd/agent/cli_doccheck.go` ==
`455d6e07885b5a451fc09dd949c74b1090518ce8`（内容同一，以 **revision** 为锚，不是 index 相对的 diff）。

## 值得记住的一条（下次取证）

**测一个进程的退出码，就不要把它放进管道。** 一个 `cmd | tail` 之后读到的 `$?`
是 `tail` 的；`${PIPESTATUS[0]}`（bash）才是第一个元素的状态，而它对 POSIX `sh`
不存在。在 Go 里读 `exec.ExitError.ExitCode()` 是唯一不经解释器的读数。
当一条 bug 行的全部证据就是一个退出码时，**先看它是怎么被读出来的**，
再看工具做了什么。
