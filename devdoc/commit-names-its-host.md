<!-- doc-class: normative -->
# 提交正文说得出「这台机器」是哪台

**问题。** `git log` 能说出提交属于哪一行账本（`task: <id>`，见
`commit-carries-its-ledger-row.md`），但**说不出它是哪台机器产出的**。三台机器的
fleet（`hosta` / `hp` / `peerbox`）里，review 和 triage 在这条轴上全是盲的：
一条提交看得见，它的产出机器看不见。于是"这条提交是不是在那台反复失败的机器上
跑的"没有机器答案，只能靠人回忆。

**机制。** `bash` tool 执行任何 `git commit` 时，把它改写成

```
git commit --trailer 'task: <本 run 的账本行 id>' --trailer 'host: <本机 hostname>' <调用方原本的其余参数原样保留>
```

（两个 `--trailer` 各占一个参数，所以正文里是**两行**；合并成一个参数的写法已实测否决，见下表。）

改写在**同一个缝**里：`internal/tools/git_commit_task_trailer.go`
（`gitCommitTaskTrailerRewrite`），由 `internal/tools/bash.go` 在命令落地前调用一次
（step 2f）。行 id 来自 `TaskRunIDEnv`（`GOPI_TASK_RUN_ID`，由 spawn 缝唯一写入，
Rule 2）；主机名来自 `os.Hostname()`，经 `localHostTrailerLine` 唯一读取。
常量 `TaskTrailerKey` / `HostTrailerKey` + 各自的行格式，都只有一处定义
（Rules 1/13/14）。

**于是索引只有一个，而且不用建**：两个轴各自可 grep。

```
git log --grep="task: 01a183ba-035b-7769-a94e-4469edc5a74b"   # 哪一行产出的
git log --grep="host: hosta"                             # 这台机器产出的
git log --grep="host: peerbox"                                  # 那台机器产出的
```

没有新表、没有索引文件、没有 hook、没有 config key、没有常驻进程、没有脚本 ——
进程退出后一无残留（Rule 35、Rule 5b）。

## 为什么要落在这个缝，而不是 `~/.gopi/GOPI.md`

操作者最初的直觉是写进系统策略 `~/.gopi/GOPI.md`（"在提交信息里报上本机名"）。
**这个落点被否决，理由是三条可测的事实：**

| 反对理由 | 具体后果 |
|---|---|
| **它不随仓库走** | `~/.gopi/GOPI.md` 是**某台机器**的本地文件。没配置过它的机器上，这条指令根本不存在 —— 而"提交无法归属"恰恰发生在这些机器上。 |
| **它只是劝告** | 散文化规则靠模型读到并照做。上下文一长、run 很机械、或者根本不是 gopi run（人手敲的 `git commit`、脚本、`make` 里的提交），规则就不生效。**没有东西观察它有没有被遵守，也就没有东西能报告它没被遵守**（Rule 15）。 |
| **事后不可复核** | 缺了这行的提交，与"这台机器报不出自己的名字"的提交，长得**一模一样**。没有见证者。 |

`bash` tool 反过来是**每个 run 的每条提交都要穿过的那一个点**（`git commit -m "<提交标题>"`
是每个 worker 的实际写法）：无论谁记不记得，行都会加上，在每台机器上都加，没有文件
要配置。而注入是**被播报的**（`[commit trailer added: task: <行 id> + host: <本机名>]` 到 stderr），
所以它唯一覆盖不了的那种情况是**可见的**而不是静默的。

这不是第二个机制：**同一次扫描、同一组豁免、同一个播报通道**（Rule 35 —— 不新增依赖）。

## 两个轴的独立性

`host:` 行**不依赖**账本缝。`GOPI_TASK_RUN_ID` 只发给被派发的 run，所以 chat、
手工跑的 gopi、`make` 里的提交都没有它 —— 而那些正是"机器不可知"的提交。
让 host 行依赖账本行，等于把归属能力挡在**最需要它的那些 run** 之外。所以：

- 有账本行 → 两行都加；
- 没有账本行 → 只加 `host:`，**绝不凭空造一个 `task:`**（实测：见下面真跑记录第三段）。

## 逐项实测（git 2.43.0，scratch 仓）

| 候选写法 | 实测结果 | 判定 |
|---|---|---|
| `--trailer "task: X" --trailer "host: Y"` | 正文**两行**：`task: X` / `host: Y` | **采用** |
| `--trailer "task: X host: Y"`（空格合并） | 正文**一行**：`task: X host: Y` —— 两个轴各自 grep 不到 | **否决**（实测踩到过） |
| `--trailer "task: X\nhost: Y"`（内嵌换行） | 两行，但把换行塞进 shell 单引号里，脆 | 否决（不如两个 flag 直白） |
| `--amend` 上也注入 | 同一个 trailer 会被**重复追加** | 否决 |
| 引号体 / heredoc 体里的 `git commit` | 那是**数据**，不是命令 | 否决（扫描本身是引号/heredoc 感知的） |
| 调用方自己已经传了 `--trailer` | 它的控制权被尊重，不重复注入 | 保持 |

**安全**：两个值都过 `trailerValueSpellable`（字母、数字、`-`、`_`、`.`）。一个
带单引号的值会**闭合引号**、把 trailer 变成后续**命令**，所以这种值那一行整条不加，
命令原样执行（值不是可拼的，就永远不进命令行）。**不可拼的值只丢它自己那一行**，
另一行照加 —— 实测：恶意行 id 不会带走 host 行。

**主机名报不出来时**（`os.Hostname()` 失败或空）：不加 `host:` 行。编一个机器名比
没有机器名更坏（Rule 15 / Rule 41：没量到的事实不当作量到了报出去）。

## 边界 —— 它不做什么

- **不改写历史。** 之前的提交一行未动，`git log --grep="host: <本机名>"` 对旧提交仍为 0 命中；
  归属只从今往后积累。
- **不带 `--amend`。** 见上表。
- **合并提交不带。** 合并提交不对应单独一行账本，也不对应一次"某台机器的编码"。
  （原文写的是 "`gopi merge` 的合并提交"；那个顶层动词已于 2026-11-01 的
  merge-removal epic 中删除，现由 `gopi task merge` 走同一个引擎。）
- **覆盖不到 `git merge` 的自动提交 / `git commit-tree` / `git cherry-pick`**：它们
  不接受 `--trailer`，覆盖它们意味着替 git 造提交信息。
- **覆盖不到命令替换里的 `git commit`（`x=$(git commit -m "<标题>")`）**：`$`、`(`、`)` 对这套
  扫描器是普通词字符，`$(` 与动词之间没有子句边界（与既有账本 trailer 的已知边界同源）。

## Rule 31 的真跑记录

单测之外，用**改动后的代码路径本身**（`tools.BashInDir`，即 bash tool 的入口）在一台
scratch 仓上跑了三次真提交。

**① 有账本行 —— 两个轴各自 grep 都命中 HEAD：**

```
$ GOPI_TASK_RUN_ID=01a183ba-035b-7769-a94e-4469edc5a74b go run ./cmd/zzhostproof_tmp \
    'echo work2 > w2.txt && git add w2.txt && git commit -m "proof: both trailers as separate lines"' \
    /dev/shm/zzhostproof-wcrwGQ/repo
[commit trailer added: task: 01a183ba-035b-7769-a94e-4469edc5a74b + host: hosta]
[master ff7ef46] proof: both trailers as separate lines
exit=0

$ git log -1 --format=%B | cat -A
proof: both trailers as separate lines$
$
task: 01a183ba-035b-7769-a94e-4469edc5a74b$
host: hosta$

$ git log -1 --format=%B | git interpret-trailers --parse
task: 01a183ba-035b-7769-a94e-4469edc5a74b
host: hosta

$ git log --format=%H --grep='task: 01a183ba-035b-7769-a94e-4469edc5a74b' | head -1
ff7ef46c3abf6faa5cfb2789d7bde79f481fb2aa
$ git log --format=%H --grep='host: hosta' | head -1
ff7ef46c3abf6faa5cfb2789d7bde79f481fb2aa
$ git rev-parse HEAD
ff7ef46c3abf6faa5cfb2789d7bde79f481fb2aa
```

**② 同一个 run，改法之前的那一次 —— 这就是"合并成一行"的实测踩坑证据：**

```
$ # 用改法前的注入（把两行空格合并进一个 --trailer）
$ git log -1 --format=%B
proof: both trailers reach the body

task: 01a183ba-035b-7769-a94e-4469edc5a74b host: hosta      ← 一行，两个轴都废了

$ git log --format=%H --grep='host: hosta'
(空)                                                              ← host 轴 grep 不到
```

单测因此改成**逐行比对**（`bodyLines[line]`），而不是 `strings.Contains` ——
子串断言在这条坑上会**绿着放过**。

**③ 没有账本行 —— host 行独立存活，且不凭空造 `task:`：**

```
$ env -u GOPI_TASK_RUN_ID go run ./cmd/zzhostproof_tmp \
    'echo work3 > w3.txt && git add w3.txt && git commit -m "proof: host line with no ledger row"' \
    /dev/shm/zzhostproof-wcrwGQ/repo
[commit trailer added: host: hosta]
[master 5d0d7cb] proof: host line with no ledger row
exit=0

$ git log -1 --format=%B | cat -A
proof: host line with no ledger row$
$
host: hosta$

$ git log -1 --format=%B | grep -c '^task:'
0                                                                 ← 没有凭空造
```

**一个诚实的坑，值得记下**：`/usr/local/bin/gopi` 是指向 `~/work/gopi/bin/gopi` 的
符号链接。机制在本 ground 的源码里时，**运行中的旧二进制仍不带这行** —— "机制存在"
与"机制在跑"是两件事，而 `git log --grep` 恰好能把它们区分开：查不到就是没在跑。

## 检查

```bash
go build ./...

go test ./internal/tools/ -run 'CommitTrailer|Trailer' -count=1

# 真正需要的索引，就是这两条命令
git log --grep="task: <某行 id>" --oneline
git log --grep="host: <某台机器>" --oneline
```
