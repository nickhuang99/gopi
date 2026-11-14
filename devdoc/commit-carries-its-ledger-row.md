<!-- doc-class: normative -->
# 提交正文携带它自己的账本行：`task: <id>`

**问题。** 提交信息和账本行之间没有机器可用的关联 —— 实测本仓库里带 task id 的提交
**0 条**，于是 202 条 `failed` 行里"哪些产出其实已经在 master 上"无法机器判定，
只能靠人读 diff 猜。

**机制。** `bash` tool 执行任何 `git commit` 时，把它改写成

```
git commit --trailer "task: <本 run 的账本行 id>" …
```

改写点是 `internal/tools/git_commit_task_trailer.go`（`gitCommitTaskTrailerRewrite`），
由 `internal/tools/bash.go` 在命令落地前调用一次。行 id 来自 `TaskRunIDEnv`
（`GOPI_TASK_RUN_ID`，由 spawn 缝唯一写入，Rule 2），不是对话里猜的。

**于是索引只有一个，而且不用建**：`git log --grep="task: <id>"`。没有新表、没有索引文件、
没有 hook、没有 config key、没有常驻进程、没有脚本 —— 进程退出后一无残留（Rule 35、Rule 5b）。

## 为什么是 `--trailer`（git 2.43.0 逐项实测否决其余写法）

| 候选 | 实测结果 |
|---|---|
| settings `trailer.task.key` | 单独设置**不追加任何东西** —— 只有 `--trailer` 才写 |
| alias / `GIT_CONFIG` 注入 | git 自身拒绝或不生效 |
| 在 `--amend` 上也注入 | **否决**：会重复追加同一个 trailer |
| 引号体 / heredoc 体里的 `git commit` | **否决**：那是数据，不是要执行的命令 |
| 行 id 直接拼进命令行 | 先过白名单（十六进制 + `-`），防引号逃逸 |

## 边界 —— 它不做什么

- **不改写历史。** 本次之前的所有提交正文一行未动，所以 `git log --grep` 对旧提交仍为 0 命中；
  关联只从今往后积累。
- **不带 `--amend`。** 见上表。
- **合并提交不带。** 合并提交不对应单独一行账本。（原文写的是 "`gopi merge` 的合并
  提交"，指当时那个顶层一次性合并命令；该动词已于 2026-11-01 的 merge-removal epic
  中删除，现由 `gopi task merge` 走同一个引擎。这条判断本身没变。）
- **它不是"哪些行已经落地"的完整答案。** 它只把"这条提交属于哪一行"变成事实；
  "这一行的产出是否已进 master"仍要 `merge-base --is-ancestor` 之类来判断 —— 而那条判据
  **在本机账本上不可用**（下一个文件 `devdoc/ledger-branch-criterion-unusable.md` 记录了数字）。

## Rule 31 的真跑记录

单测之外，用**改动后的代码路径本身**（`tools.BashInDir`，即 bash tool 的入口）跑了两次真提交：

```
$ GOPI_TASK_RUN_ID=01a17857-537c-7636-a6be-fb94413d7f54 go run ./cmd/zzproof <scratch-repo> \
    'git commit -m "proof: the row reaches the commit body"'
[commit trailer added: task: 01a17857-537c-7636-a6be-fb94413d7f54 + host: host-b]
exit=0
[master (root-commit) 91c33f0] proof: the row reaches the commit body
```

读回：

```
$ git log -1 --format='%B'
proof: the row reaches the commit body

task: 01a17857-537c-7636-a6be-fb94413d7f54

$ git log --grep='task: 01a17857-…' --oneline
91c33f0 proof: the row reaches the commit body      ← 命中

$ git log -1 --format='%B' | git interpret-trailers --parse
task: 01a17857-537c-7636-a6be-fb94413d7f54          ← git 自己也认它是 trailer
```

**一个诚实的坑，值得记下**：本次提交 `ee57adf` / `a08c53b` / `e98a5e3` 时，
运行中的 `/usr/local/bin/gopi` 还是**主仓库的旧二进制**（符号链接指向
`~/work/gopi/bin/gopi`），所以由它执行的 `git commit` **没有**带 trailer —— 机制当时只在
本 ground 的源码里，不在运行中的进程里。只有 ground 的代码被 merge 并由服务器重建之后，
worker 的提交通道才会带上这一行。这也说明"机制存在"与"机制在跑"是两件事，
而 `git log --grep` 恰好能把它们区分开：查不到就是没在跑。

## 检查

```bash
go test ./internal/tools/ -run 'TestGitCommitTaskTrailer|TestBashCommitCarriesTheLedgerReference'
git log --grep="task: <某行 id>" --oneline    # 真正需要的索引，就是这一条命令
```
