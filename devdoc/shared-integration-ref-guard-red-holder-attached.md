<!-- doc-class: normative -->
# RED 半在本机 git 2.43 上被 git 自己拒绝：holder-attached 的 ref 写

**任务行**：`01a12e1e-16e6-730d-a566-c7940d5616b0`
**证据目录**：`/home/user/.gopi/sessions/01a12e1e-16e6-730d-a566-c7940d5616b0/`
（`rgprobe.sh` `rgprobe.out` `rgprobe2.sh` `rgprobe2.out` `rgprobe3.sh` `rgprobe3.out`
`rgprobe4.sh` `rgprobe4.out` `green-run.out`）

## 症状

`go test -count=1 -run 'TestSharedIntegrationRefGuard' ./internal/tools/` 在本机
（git 2.43.0）**确定性红**，红在 RED 半：

```
--- FAIL: TestSharedIntegrationRefGuard_VerbFamilyRedThroughRawGit/refuse/branch-f-onto-a-held-branch
    the command must SUCCEED when the guard is absent (...), got: exit status 128
    fatal: cannot force update the branch 'main' used by worktree at '.../trunk-checkout'
--- FAIL: TestSharedIntegrationRefGuard_ConfigFetchWithoutRefspec
    fatal: refusing to fetch into branch 'refs/heads/main' checked out at '.../trunk-checkout'
```

RED 半的前提是「无 guard 时裸 git 必须 exit 0 并改写 held ref」。在 fixture 形状下
（`trunk-checkout` **attached** 在 `main`）这个前提对一整类命令**不成立** —— 拒绝它的是
**git 自己**，不是 guard。

## 实测（git 2.43.0，`rgprobe2.out` / `rgprobe4.out`）

holder（`trunk-checkout`）**ATTACHED** 在 `main`，命令从 linked worktree 发出：

| 命令 | 裸 git exit | ref 是否移动 |
|---|---|---|
| `git branch -f main <sha>` | **128** `cannot force update the branch 'main' used by worktree at …` | 否 |
| `git branch --force main <sha>` | **128** 同上 | 否 |
| `git branch -M other main` | **128** 同上 | 否 |
| `git branch --move --force other main` | **128** 同上 | 否 |
| `git fetch . +refs/heads/src:refs/heads/main` | **128** `refusing to fetch into branch 'refs/heads/main' checked out at …` | 否 |
| `git fetch origin`（`remote.origin.fetch=+refs/heads/*:refs/heads/*`，无 refspec） | **128** 同上 | 否 |
| `git checkout -B main <sha>` | 0 | 是（staged 1） |
| `git update-ref refs/heads/main <sha>` | 0 | 是（staged 1） |

两个关键机理（都实测，不是推断）：

1. **拒绝键在「holder 的 HEAD」上，不在写入上。** `branch -f` / `-M` 的拒绝来自
   `cannot force update the branch … used by worktree`，fetch 的来自
   `refusing to fetch into branch … checked out at` —— 两者问的都是「**有没有别的
   worktree 站在这个分支上**」。
2. **把 holder 的 HEAD 从分支上摘下来（`git -C <holder> checkout --detach`：不动工作树、
   不动 index），同一串裸命令立刻 exit 0 并改写 ref。** 再把 holder 的 HEAD 按名字
   （`git -C <holder> symbolic-ref HEAD refs/heads/main`）指回被改成旧 sha 的 ref，
   它的 index 仍持有「拉下来之后」的树 → `git status` 报 **staged** 路径 —— 正是事故里
   被打出来的 Rule 30 park（`M  trunk.txt`，staged=1，unstaged=0，untracked=0）。

`update-ref` 那一行是**对照组**：它对 holder-attached 不做任何拒绝（`rgprobe4.out` 的
第 6 组 step1 就是 exit=0），所以事故（`01a12d86` 那行的
`git update-ref refs/heads/master …`）在本机没有任何东西拦它 —— 这也是为什么这条
guard 必须存在。

`rgprobe2.out` 的 J 组还记了一个附带事实：holder 的工作目录被**搬走**之后，从 linked
worktree 发的任何 git 命令先死在 `not a git repository: …/worktrees/linked`，与
ref 语义无关，故未引入测试。

## 修法（只改测试，guard 实现一字未动）

`internal/tools/shared_integration_ref_guard_verbs_test.go`：

- 新增 `refGuardRawWriteIsReachable(t, holderDir, holderBranch, ref, runDir, args...)`
  —— RED 半唯一的裸 git 入口。它**先按行里原样**跑一次（holder 仍 attached，读者会
  假设的那个状态），把 git 自己的拒绝**记进日志**；**再回读 ref**，确认这次被拒绝的尝试
  真的没动过 ref（被拒绝的尝试若顺手改了 ref，detach 就成了掩盖而不是协议）；然后摘
  HEAD、重跑同一串、按名字挂回去。**第一步就 exit 0 的 git**（holder check 不覆盖该
  form）直接返回，不做协议步 —— 两条分支都打日志，日志里能分辨。
- `TestSharedIntegrationRefGuard_ConfigFetchWithoutRefspec` 的裸 fetch 走同一函数。
- `refGuardDetachHolder` / `refGuardReattachHolder` 是协议的两半，常量
  `refGuardHolderDetachVerb/Flag`、`refGuardHolderReattachVerb`、`refGuardHeadRefName`
  在 `shared_integration_ref_guard_test.go` 具名（Rule 1/13）。
  `refGuardReattachHolder` **自带三条前置断言**（HEAD 真的挂在预期的 ref 上、无
  unstaged、无 untracked），否则「协议顺手弄脏的树」会被当成复现出来的 park。

**RED 语义没有被放宽**：断言仍是原来那三条 —— held ref 真的移动、trunk 真的变脏、
脏的是 staged（index-vs-ref），且 unstaged/untracked 必须为空。变的只是「让裸命令真的
到达那一步」的方式：与 git 2.34.1 上当初测量时的世界形状一致。

`internal/tools/shared_integration_ref_guard.go` 的**文件头实测表**补了一段：原表记的是
git 2.34.1（当时 holder 未被 git 拦），本机 2.43.0 加了 holder-head 检查，两个版本的
差别与「摘 HEAD」这一协议都写在那里，避免下一个读到该表的人以为 2.43 会放行。

## 结论 / 可复用的判据

- **「裸 git 一定放行」这个前提，在分支被别的 checkout 站着时只对部分 form 成立，且随
  git 版本变化。** 写这类 RED 证明时，要么用不被占用的 ref 形状，要么先真的跑一次、
  按 exit code 决定要不要先把 holder 的 HEAD 摘下来。
- 判据是**实测 exit code + ref 是否真的移动 + trunk staged 计数**，不是错误字符串。

## Verification

```
go build ./...                                                    : BUILD_OK
gofmt -l internal/tools/shared_integration_ref_guard*.go           : (无输出)
go test -count=1 -run 'TestSharedIntegrationRefGuard' ./internal/tools/ : ok 5.135s (6/6 PASS)
```
