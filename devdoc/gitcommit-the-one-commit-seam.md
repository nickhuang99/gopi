<!-- doc-class: normative -->
# 一个提交抽象：internal/gitcommit

## 缺陷：签名机制坐在文本上，而内部的提交坐在 argv 上

提交必须携带两个事实——它由哪条账本行产出（`task: <id>`）、由哪台机器产出
（`host: <name>`）。这个机制当时只有一个门：bash 工具的**命令文本改写**
（`internal/tools/git_commit_task_trailer.go`），它扫描命令字符串，在每个
`git commit` 的词后面插入 `--trailer "<line>"`。

gopi 自己创建提交的三处调用点不经过命令文本——它们直接拼 argv：

| 站点 | 旧写法 |
|---|---|
| `internal/tools/task_adoption.go:433` | `adoptionGit(repo, "commit", "--no-verify", "-m", adoptionSummaryPrefix+br.Name+" ("+br.Tip+")")` |
| `internal/tools/task_merge_executor.go:353` | `mergeExecGit(mainRepo, "commit", "--no-verify", "-m", msg)` |
| `cmd/agent/merge_union.go:531` | `mergeGit(wt, "-c", "user.name="+mergeUnionCommitName, "-c", "user.email="+mergeUnionCommitEmail, "commit", "--no-edit", "-m", mergeUnionCommitMessage(artefact, rows))` |

（原文照抄，行号取自父提交 `f6288ed^`；取回完整上下文：
`git show f6288ed^:internal/tools/task_adoption.go | sed -n '433,437p'`、
`git show f6288ed^:cmd/agent/merge_union.go | sed -n '531,533p'`。）

没有文本可改写，所以这三处创建出来的提交**一个 trailer 都没有**。这不是谁不勤勉，
是位置的性质：**文本上的缝够不着 argv 上的缝。**

## 做法：把事实搬到 argv 被构造的地方

新 leaf 包 `internal/gitcommit`（只依赖标准库，**不 import internal/tools**，否则成环）：

```go
func Commit(ctx context.Context, repo string, msg Message, opts Options) (Result, error)
```

它做四件事：

1. **拼消息**：subject + 可选 body，再把 trailer 块作为**最后一段**用空行隔开追加
   ——只有被识别成 trailer 块，`%(trailers:key=<key>,valueonly)` 才看得见它。
2. **组 argv**：`-c` 配置对 → `commit` → 开关（`--no-verify` / `--no-edit` /
   `--amend` / `--allow-empty`）→ `-m <text>`。
3. **执行**：经一个可替换的后端（`Backend` 接口 + `UseBackend`）。
4. **读回自检（Rule 15）**：`git log -1 --format=%(trailers:key=<key>,valueonly)`
   把写进去的每个 key 从**提交对象**里读回来；空 → 报错，值不等 → 报错。
   自检问的是 commit object，不是我们刚拼的字符串——一个从未进入提交的消息，
   字符串比对恰恰看不见。

### 唯一定义搬了家

`TaskTrailerKey` / `TaskTrailerLineFmt` / `HostTrailerKey` / `HostTrailerLineFmt` /
`TaskTrailerLine` / `HostTrailerLine` / `LocalHostTrailerLine` / `Trailers` /
`Spellable` 的**定义**现在只在 `internal/gitcommit`。`internal/tools` 侧保留同名
标识符，但每一个都是**别名或薄封装**：

```go
const (
	TaskTrailerKey     = gitcommit.TaskTrailerKey
	TaskTrailerLineFmt = gitcommit.TaskTrailerLineFmt
	HostTrailerKey     = gitcommit.HostTrailerKey
	HostTrailerLineFmt = gitcommit.HostTrailerLineFmt
)

func TaskTrailerLine(taskID string) string { return gitcommit.TaskTrailerLine(taskID) }
```

于是**两个门共享一套拼写**：命令文本改写用 `gitcommit.Trailers(taskID)`，
Go 调用用 `gitcommit.Commit(ctx, repo, msg, opts)`，谁也不会漂移。
`TestGitCommitSeamAliasesAreNotCopies` 钉住这一条（别名 = 同值同行为，不是副本）。

### 三个站点的语义原样保留

每个站点抽成一个**具名函数**——不是风格，是可证性：内联在 sweep 循环里的调用
只能靠整套 fixture 才够得着，具名函数能被测试直接驱动、真跑、读回。

| 站点函数 | 保留的语义 |
|---|---|
| `adoptionCommitMerge` | `--no-verify`；消息 `adoptionSummaryPrefix+br.Name+" ("+br.Tip+")"` 不变；`adoptionGitTimeout`；**无 `task:` 行**（sweep 合的是别人的分支，没有自己的行，不编一个） |
| `mergeExecCommitMerge` | `--no-verify`；`mergeExecMergeMsg` 不变；`mergeExecGitTimeout`；`TaskID: rep.TaskID` → `task:` 行 |
| `mergeUnionCommitResolution` | `--no-edit`；`-c "user.name="+mergeUnionCommitName` / `-c "user.email="+mergeUnionCommitEmail` 身份覆盖（probe worktree 是 detached 的，没有自己的身份）；`mergeGitEnv()` 的 locale 钉死经 `Options.Env` 原样传下去；哈希由 seam 自己的 `rev-parse HEAD` 一次读回，调用点不再多跑一次 |

**输出文字一个字没动**：`rep.Steps = append(rep.Steps, MergeExecStep{Step: "commit", Detail: err.Error()})`
与 `rep.Steps = append(rep.Steps, MergeExecStep{Step: "commit", OK: true})`
还在原处，`counts.Failed = append(counts.Failed, br.Name+": commit: "+adoptionErrDetail(err))`
的前缀还在原处，`fmt.Errorf("union probe: commit the resolution: %w", err)` 还在原处。

## 门禁：三道，各管一件不同的事

`internal/tools/git_commit_seam_gate_test.go` + `cmd/agent/merge_union_commit_seam_test.go`：

1. **字面量门禁**：`internal/` 与 `cmd/` 下任何生产 Go 文件不得把裸串 `"commit"`
   当**实参**传。识别器判的是位置（前一个非空白字节是 `(` 或 `,`，后一个是 `,` 或 `)`），
   所以 `Step: "commit"`（字段）、`== "commit"`（比较）、`const x = "commit"`（赋值）、
   注释、`"commit --amend"`（更长的串）都不算。**红证明**
   （`TestGitCommitSeamGateRecognisesTheShapeItRefuses`）把三处被删掉的旧形状钉成 1、
   把五种同音形状钉成 0。非真空下限 `seamGateMinFiles = 500`。
   白名单是**收缩的**：`internal/tools/task_report.go`（`taskreport.FormatFact("commit", hash)`
   是报告列标签，不跑 git），且门禁要求识别器**仍能看见**这条——白名单变空要报错。
2. **三站点真跑**：真仓库、真 staged 改动，读 `git log -1 --format=%B`，用**整行相等**
   断言 `host: ` 存在（不用 `Contains`：测过的坑是两个 trailer 塌成一行
   `task: X host: Y`，`Contains` 会判绿）。站点 ① 还必须**没有** `task:` 行。
3. **假后端不改一行**：`gitcommit.UseBackend(fake)` 装上假后端，两个站点函数
   **在没有任何仓库**的 `t.TempDir()` 上跑到底，提交到达假后端，且消息里带着两条 trailer。
   这才是"抽象"而不是"三个包装改了个名"的判据。

## 没做的 / 明确不做的

- **不是 `gopi commit` 动词**。人用 git 本身；再加一个命令是同一件事的第二个界面。
- **不改历史**，不 amend 调用者没刚创建的东西（`Options.Amend` 是调用者明说要的）。
- **不替代 bash 工具的命令文本改写**：那条覆盖 agent 打出来的 `git commit`，
  这条覆盖 gopi 自己跑的 `git commit`。两条门，一套拼写。
- **不引入新的常驻依赖**（Rule 35）：包、测试、一行 devdoc，没有守护进程、
  没有 hook、没有配置键。

## 验证

```
Build:  CGO_ENABLED=0 go build ./...                                          → exit 0
Tests:  go test ./internal/gitcommit/ ./internal/tools/ ./cmd/agent/ \
          -run 'GitCommitSeam|CommitSeam' -count=1                            → 三个包 ok
        go test ./internal/tools/ -run 'TestGitCommitSeam' -v                 → 5 PASS
        go test ./cmd/agent/ -run 'CommitSeam' -v                             → 2 PASS
        go test ./internal/gitcommit/ -v                                      → 10 PASS
gofmt:  gofmt -l <changed files>                                              → 空
vet:    go vet ./internal/gitcommit/ ./internal/tools/ ./cmd/agent/            → 空
```

真跑的调用与输出见本次 session 报告 `$GOPI_TASK_REPORT_PATH`。
