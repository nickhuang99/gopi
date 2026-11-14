<!-- doc-class: historical -->
# Close-out ④: 删分支用 `-D`,不再先试 `-d`(`-d` 的判据按构造必然拒绝)

Task `01a131bb-98b4-78f0-8b29-0dd6c6e857b4`(system layer, Rule 28)。

## 结论先说

`internal/tools/task_finish_cleanup.go` 的收尾链第 ④ 步原来长这样:

```go
line, err := finishCleanupGit(ctx, branchDir, finishCleanupBranchVerb, finishCleanupBranchDeleteFlag, branch) // -d
if err != nil {
    // ……两道守卫……
    line2, err2 := finishCleanupGit(…, finishCleanupBranchForceDeleteFlag, branch) // -D 兜底
}
```

现在 ④ **只有一个动词**:守卫先问,问过了就直接 `git branch -D <branch>`。
**改动只涉及"用哪个 flag"** —— "该不该删"的判据一个都没动。

## 为什么 `-d` 是纯浪费(不是"保险")

`-d` 问的是 git 自己的问题:**这个分支合并进它的 upstream / HEAD 了吗?**

而 ④ 站的位置让这个答案**按构造就是 NO**:

1. ② 刚把这个 ref push 到**交付点**(peer 自己的 checkout)—— tip 在对面;
2. 本仓 HEAD(master)**并没有** merge 这个分支 —— `gitMergeBaseAncestor` 为 false。

于是"未合并"这个拒绝不是一次发现,而是**每次收尾都要付一次的固定成本**。审计流里
它留下的签名就是这对形影不离的记录:

```
branch-delete finish-cleanup-branch
branch-delete finish-cleanup-branch-force   ← 同一次删除,前一条只是被拒的那次尝试
```

HP 上实测:`finish-cleanup-branch` 与 `finish-cleanup-branch-force` 是 **393/393 成对**
—— 没有任何一次 `-d` 单独站住过。

## 什么在替它把关(两道,都没动)

`-D` 覆写的是 git 那句"not fully merged"——正是 ④ 不该替**没交付的分支**绕开的判据。
所以 `-D` 仍然要在**删之前**过同样两道门,两道门原封不动:

| 门 | 函数 | 问什么 |
|---|---|---|
| axis 2 | `finishCleanupBranchForceDeleteReason` | ② 必须真的把**这个 ref** 交付了(否则 fail-soft:报告 + **保留**) |
| orphan | `branchDeleteOrphanRefusalAt` | tip 必须还能在别的 ref 里找到(**交付点也算**),否则拒绝 |

`internal/tools/branch_delete_guard.go` **一个字没改**:没绕过、没削弱、没加例外。

## 失败必须出声(Rule 15)

`-D` 现在没有"第二次尝试"了,所以它失败就是失败:

```go
rep.Steps = append(rep.Steps, FinishCleanupStep{Step: finishCleanupBranchVerb, Target: branch,
    Detail: finishCleanupBranchVerb + " " + finishCleanupBranchForceDeleteFlag +
        " failed after the guards granted it: " + strings.TrimSpace(delOut) + ": " + delErr.Error()})
```

即:step **不是 OK**、带上 **git 自己的那句话**、**分支留在原地**(没删成功就不该报成功)。
不 fetch,不重试同一个动词,不静默。

## 另外两处 `-d` 引用点的逐处判定(改 / 不改,理由)

`finishCleanupBranchDeleteFlag = "-d"` 在树里还有两个读者。**它们都保留 `-d`,因为
它们问的正好就是 git 那个问题**:

1. **`internal/tools/worktree_sweep.go:2659/2665`(`worktreeSweepDeleteBranch`)**
   —— sweep 删的是**死掉登记的**分支 / **惰性 chat ground** 的分支。它**没有**
   push 到交付点这个动作,也就没有"tip 在对面"这个前提;`-d` 在这里是真的闸:
   "带着 master 没有的提交就不删"。**保留 `-d`,不改。**
   (同一函数里那句 `// …… so -d, never -D ……` 的注释与实现一致。)

2. **`internal/tools/task_merged_cleanup.go:728`(orphan lane)**
   —— 这条道删之前刚刚**复检过**"它仍是 master 的祖先"(`mergedCleanupStillMergedReason`),
   即 `-d` 的判据在这里**由构造为真** —— 与 ④ 恰好相反。这里 `-d` 不是浪费而是
   冗余确认,而且它是**唯一**的 flag,没有 `-D` 兜底,也没有"必拒再退"的浪费。
   **保留 `-d`,不改。**

> Rule 13(常量必须有真读者):`finishCleanupBranchDeleteFlag` **不是死常量** —— 上面
> 两个包外读者 + 本文件 plan 渲染路径之外的真实调用点撑着它。本文件里那个
> `finishCleanupPlanDeleteFlag`(plan 渲染 `run: git branch <flag> <branch>`)已经从
> 引用 `-d` 改成引用 `-D`:plan 必须预报**执行器真的会跑**的 argv,否则 plan 就在说谎。

## 审计记录(arms)为什么还是两条

`removalaudit` 有两个 arm:`finish-cleanup-branch` 与 `finish-cleanup-branch-force`。
④ 现在**两条都写**:删除前写 `finish-cleanup-branch`(记"branch-liveness 门没拒绝"),
再过两道门、写 `finish-cleanup-branch-force`(记"本链自己的 push 交付了它,且 orphan 门
没拒绝"),然后才删。理由有二:

1. **`finish-cleanup-branch` 不能变成死 arm**:`TestRemovalAuditGateEveryRemovalSiteEmits`
   会因为"声明了却没有任何生产 emit"直接红(实测过这条规则真的会 fire)。
2. **两条记录在审计流上仍然可分辨**:单独一条 plain 记录 = 走到删除但没经过 force
   站立判定;force 记录 = 那次删除是被本链自己的交付授权过的。

## 那个门自己改了什么(诚实说明)

`internal/standards/removal_audit_gate_test.go` 的
`TestRemovalAuditGateSameKindSiblingIsLoadBearing` 在改动后**红了** —— 这是**预期的**,
因为它的契约里写死了拓扑:

- 它要求 `task_finish_cleanup.go` 里有 **2 个 branch-delete site**(`-d` 一个、`-D` 一个),
  并按序号把两条记录 1:1 配到两个 site 上;现在只剩 **1 个 site**(唯一的 `-D`),
  按序号配对在计数上就不成立了。
- 修法是**按树的现状重指**,而不是放宽断言(Rule 31):改为
  ①site 数按**下限 + 上限**断言(下限保证不空转),②"记录覆盖删除"的方向改为
  **把两条记录一起删掉**,再要求谓词报出**那个 site**;③"兄弟改名 → 报出每一个未记录的
  branch-delete site"从"等于 emit 调用数"改成**从调用方文件里数出真实的 site 数**。
- **被有意放弃的断言**:"删掉单独一条记录 ⇒ 该 site 变红"(bug row `01a12be8`,
  2026-10-12 的假绿)。它是**两个 site** 的性质;现在只有一个 site、一条记录就能覆盖它,
  这条性质在树上**不再成立**。硬留着它就是让测试断言一件树不做的行为。
  第二条记录的价值在**审计流**(读者能分辨删除的站立),不在这个门的覆盖上 ——
  这一点写在测试文件的注释里,免得下一个读者把它当漏洞去"修"。

## 回归测试

`internal/tools/task_finish_cleanup_branch_flag_test.go`(新增),在**真实执行的 argv**
上取证(Rule 31:跑被改的表面,不是读它的形状):用 PATH shim 记录子进程真正跑的
`git branch …` argv,再委派给真 git —— 夹具仍是真仓库(裸 server + client + linked worktree),
删除也真的发生。

- **(A)** 成功的收尾:**跑了 `git branch -D <branch>` 恰好一次,`git branch -d` 一次都没有**;
  并且之后 ref 真的不在(`branch --list` 空)。
- **(B)** `-D` 失败(故障注入 shim):step ④ **不是 OK**、detail 带 **git 原话**、
  **没有**任何一次退回去的 `-d`、**ref 仍在**(失败道保留工作)。

## 参考

- 前情:`devdoc/closeout-branch-delete-evidence-20261011.md`(D1/D2:空 ref 也被记成"删过"、
  记录不带 tip / 交付点)。
- 孤儿守卫的由来:2026-09-17 事故(`git branch -D` 是唯一能让一个 commit **从所有 ref 上消失**
  的路径)。
