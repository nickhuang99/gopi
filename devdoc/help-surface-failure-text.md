<!-- doc-class: normative -->
# 失败文本分清「期望 / 作者注 / 实测输出」— bug 01a13207

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

<!-- DOC-CLASS: reference -->
<!-- 本文件由 bug 01a13207 的修复引入（task task/01a13207597b7bb6aa37a28db49744d4）。 -->

## 问题：一条失败消息里混着三种来源不同的文字

help-surface 探针（`cmd/agent/cli_tools_maintenance_help_surface_cases_test.go`）
的 token 断言失败时，旧文本是一行连写的：

```
`gopi worker --stop`: none of ["no worker daemon is running"] appeared in the output — note: no daemon holds the sandbox lock, … and it must NOT signal anything
    gopi worker: no DETACHED worker daemon is running (no live daemon state under …)
```

第一行里有两个**都不是被调用命令输出**的东西：

| 片段 | 真实来源 |
|---|---|
| `%q`（`["no worker daemon is running"]`） | case 的 `wantAny` —— **作者的期望** |
| `— note:` 之后 | `g6Case.note` —— **作者对这条 case 的描述** |
| 下一行（`g6Head(out)`） | 命令**实际打印**的东西 |

读者会（实测：bug 01a13207 的提交者就这样花掉一整轮）把 `%q` 里的字符串当成命令的
stdout 去找，而它是作者写的期望，永远不可能在输出里出现 —— 于是失败消息本身在**引导
误读**。

## 修法（外科手术式，只动渲染）

新增 `g6FailureText(expected, note, out)` 与三个常量，把三段按固定顺序、**各自带标签**
地印出来：

```
`gopi tool probe`: EXPECTATION (this case's wantAny, OR-semantics — at least one of these tokens must appear): none of ["…"] appeared
AUTHOR'S NOTE (a description of the CASE, NOT this command's stdout): …
ACTUAL OUTPUT of the command driven above: …
```

- 三个调用点（`wantExit` 不符 / `wantAll` 缺 token / `wantAny` 全缺）统一走它；
- `note` 为空时印 `AUTHOR'S NOTE: (this case declares none)`，**不静默省略**（Rule 15）；
- `g6AssertTokens` 的形参由 `*testing.T` 放宽为 `testing.TB`，使新守卫能用
  `recordingTB` 直接驱动它并读回消息（不必真的让测试失败）。

## 被拒绝的替代方案：整文件回退到 HEAD 做红证

把 `cli_tools_maintenance_help_surface_cases_test.go` 整体还原成 HEAD 版**不是**这条
守卫的合法红证：守卫引用的是本次修复引入的常量（`g6ExpectationLabel` 等）并且用
`testing.TB` 驱动 `g6AssertTokens`，HEAD 版**根本编译不过**（`undefined:
g6ExpectationLabel` / `cannot use tb … as *testing.T`），红的是「编译失败」而不是
「形状错了」。合法的红证是**只把三个调用点写回旧的连写形态**（helper 留着），
所以 `artifacts/redproof-B-callsites-prefix.txt` 才是那条证据，
`artifacts/redproof-B-wholefile-revert-BUILD-FAILED.txt` 是失败尝试的留痕。

## 机器判据（此前只靠作者自觉）

`cmd/agent/cli_tools_maintenance_help_surface_failure_text_test.go`：

1. 运行时两臂（OR / AND）：驱动真实的 `g6AssertTokens`，断言**输出标签之后不得出现
   作者的 note、也不得出现 case 的期望 token** —— 这正是本 bug 的机理；
2. 无 note 那条：必须印出「未声明 note」，而不是空白；
3. 源码级：cases 文件里不得再出现无标签的 `— note: ` 形态（`3 occurrences` 即红），
   且必须仍在调用 `g6FailureText(` —— 抓到「新写的另一个 helper 又用旧形态」。

三条红证/还原实测见任务报告（`GOPI_TASK_REPORT_PATH`）与
`~/.gopi/sessions/01a13207-597b-7bb6-aa37-a28db49744d4/artifacts/`。

## 关于「引文 0 命中」那半

原 bug 行引用的失败文本 `none of ["the exact text as the page prints it is required"] …`
**在树里 0 命中**（实测：`grep -rF` 计数 0）。引文是提交者自己拼的，属已撤回的一半；
它被记住的价值是**它为什么会写错**：旧的失败文本把作者的文字印得和命令输出难以区分。
本修复消除的正是这个歧义。
