<!-- doc-class: historical -->
# Task Report: 三段式分支收编链 (branch adoption chain)

Branch: `feat/branch-adoption-chain` (worktree ~/workspace/gopi-adoption-chain)
Commit: `git log feat/branch-adoption-chain -1` (feat(adoption): three-stage branch adoption chain on the serve tick)
Recovery: `git show feat/branch-adoption-chain:devdoc/branch-adoption-chain.md`

## Completed
- **引擎** `internal/tools/task_adoption.go` + `task_adoption_conflict.go`：
  - ①发现：`git for-each-ref refs/heads refs/remotes` 双通道（本地路径 remote 的 push 直达 refs/heads、harvest 通道走 refs/remotes/origin/*），排除 master/HEAD，同分支双通道 dedup（seen map）。
  - ①门2：台账配对 —— 只有 deliverable.branch 指向该分支且 status='completed' 的任务才参与收编（无台账行的孤儿分支 Skipped，绝不动）。
  - ②试探：`git merge --no-commit --no-ff`（TryMerge seam 注入测试，同 HarvestOptions.Fetch 模式）。
  - ③a 干净：`git commit --no-verify -m "adoption: merge <branch> (<tip>)"` + `branch [-r] -d` —— token-free 收编。
  - ③b 冲突：`git merge --abort` 保存现场 → `TaskSubmit` 自足 briefing：冲突文件清单（git diff --name-only --diff-filter=U）+ 逐文件冲突块（<<<<<<< … >>>>>>>，40 行/块截断）+ `git diff master...branch --stat` + 分支意图（台账 summary+deliverable）+ 验收标准。source=auto, tags=merge-conflict,auto-dispatch, repo=server repo。
  - ④架构红线：Go 只做发现/试探/派发/收编，无任何语义判断/LLM 调用；解冲突在 Bond 自己的 session（P2.5 t-1788063202 先例）。
  - ⑤防风暴：adoptionConflictTaskLive payload-keyed 查重（pending/claimed/in_progress 中同分支 merge-conflict 行已存在 → Duped 不重派）+ ⑤门在 ②之前跑（幂等不依赖 merge 状态）；merge-conflict tag 经 taskTagPriority 自动高优先（TaskSubmit priority<=0 时推导）。
- **serve tick 集成** `cmd/agent/cli_tasks_adopt.go`：serveAdoptionSweep() 作为 runTaskServe 的 pass step（serveStaleSweep 之后、dispatch 之前）；GOPI_ADOPTION_INTERVAL 节流（默认 15min，0/负值/junk=禁用，Rule 2 单点读取）；脏工作树拒绝本 pass（adoption 不与手工工作纠缠）。
- **CLI 面** `gopi task adopt [--repo PATH] [--master BR] [--submit]`：默认 report-only，--submit 才派发（serve 面恒 submit）。
- **测试** 9 个新测试全绿：干净收编+远端 ref 删除+二次 pass 零扫描、冲突派发+现场恢复（abort 后 worktree 干净）+ briefing 自足断言、重复 tick 幂等（3 次 sweep 仅 1 行）、孤儿分支 skip、hunk 截断、deliverable.branch 解码（坏 JSON/无 branch）、serve 间隔解析/节流/脏树跳过。
- E2E（真 git 临时仓库 + GOPI_MEMORY_DB 注入）：epic 三条实测 = ①干净分支→收编+删②冲突→abort+派行（含冲突块）③重 tick 幂等。

## Not Done
- push-back→refs/remotes 的真实 ssh 通道 E2E（双节点）：本机用本地路径 remote 验证 refs/heads 通道 + for-each-ref 双通道代码路径覆盖 refs/remotes/*；ssh 真机可后补。
- Rule 11（LLM 真实调用验证）：gopi task adopt 是 CLI face，serve face 是全自动 pass step，无 task_submit 工具注册——LLM adoption proof 不适用于本 epic（epic 明确"Go 代码只做派发"）。
- 冲突 task 被 Bond 解完 push back 后"下一 tick 走 a 路径"的整环 E2E：依赖 Bond session 真实解冲突，单测覆盖到派发+幂等；整环靠 serve 常驻后自然验证。

## Verification
- Build: `go build ./...` OK；`go vet ./cmd/agent/ ./internal/tools/` clean
- Tests: 新增 9/9 PASS + CLI face 真跑（gopi task adopt 扫描 12 分支 report-only OK；nil ctx segfault 已修 @16cdd25 —— CLI 传 nil ctx= nil interface，database/sql 解引用即 panic，AdoptionSweep 入口兜底 context.Background）。
- Commit: 0c183cc (feat) + 16cdd25 (fix) — feat/branch-adoption-chain（worktree 共享主仓 refs，无需 push）
