#!/usr/bin/env bash
# =============================================================================
# safe-merge-contract.sh — safe-merge 守卫的**共享词汇表**（单一契约源）
#
# 为什么有这个文件（Rule 1/1b/14 的跨语言半边）:
#   守卫有两个家 —— bash 的 scripts/safe-merge.sh（GOPI.md Rule 9 要求
#   merge 前由人或 PA 跑的那道闸）与它的 Go 镜像
#   mergeExecSafeMergeBlock（internal/tools/task_merge_executor.go 的
#   执行器步骤 ③）。两个家必须用**同一套拒绝词汇**：同一种事实、同一种
#   拼写。这个文件就是那套词汇的唯一定义处，Go 侧对应
#   internal/tools/safe_merge_contract.go 的同名常量（Go 无法 import
#   shell，所以两侧各有一份定义，而"必须一致"由
#   internal/tools/safe_merge_contract_test.go 与
#   internal/tools/task_merge_executor_safemerge_test.go 机械守卫，见下）。
#
# 它**不是** temp-root 通道，所以不 source scripts/lib/tmp-ban.sh：这个库
# 只声明字符串常量、不创建任何文件，走 tmp-ban 只会向 stderr 打一条 temp
# 根信息，污染一道要被人读的守卫输出。
#
# 拒绝种类（kind）—— Go 侧同名常量:
#   safeMergeRefusalArchiveTag        (检查 [a])
#   safeMergeRefusalBlacklistPrefix   (检查 [b])
#   safeMergeRefusalWorktreeOccupied  (检查 [c])
#   safeMergeRefusalMissingRef        (检查 [e])
#   safeMergeRefusalUnrelatedHistory  (检查 [f])
#   safeMergeRefusalProbeFailed       (检查 [f] 的"无法回答"臂 —— bash 侧即时
#    exit，Go 侧是要带回调用者的 error)
#
# 被 source 的脚本必须已声明 MASTER（默认集成分支名）。允许先定义后
# source：用法是 `MASTER="$(...)" . <this file>`。
# =============================================================================

# shellcheck disable=SC2034  # 常量由 source 它的脚本读取
{
    # ---- 拒绝种类：唯一契约 ----
    readonly SAFE_MERGE_REFUSAL_ARCHIVE_TAG="archive-tag"
    readonly SAFE_MERGE_REFUSAL_BLACKLIST_PREFIX="blacklist-prefix"
    readonly SAFE_MERGE_REFUSAL_WORKTREE_OCCUPIED="worktree-occupied"
    readonly SAFE_MERGE_REFUSAL_MISSING_REF="missing-ref"
    readonly SAFE_MERGE_REFUSAL_UNRELATED_HISTORY="unrelated-history"
    readonly SAFE_MERGE_REFUSAL_PROBE_FAILED="probe-failed"

    # ---- 渲染句子：两个家逐字一致 ----
    # %s = 分支名 / %s = 集成分支名
    readonly SAFE_MERGE_MSG_MISSING_REF="分支不存在 —— 禁止 merge (git rev-parse --verify --quiet refs/heads/%s 无法解析该 ref)"
    readonly SAFE_MERGE_MSG_UNRELATED_HISTORY="与 %s 无共同历史 (孤儿根 —— git merge 只在 --allow-unrelated-histories 下才接受) —— 禁止 merge"
    readonly SAFE_MERGE_MSG_PROBE_FAILED="ref 存在性探测无法回答 (git rev-parse 报: %s) —— 不确定既不是缺失也不是安全，禁止 merge"
    readonly SAFE_MERGE_MSG_ANCESTRY_PROBE_FAILED="历史相关性探测无法回答 (git merge-base --all 报: %s) —— 不确定不等于有共同历史，禁止 merge"

    # ---- 探测面：`git merge-base --all` 的退出码 + 输出一起才是答案 ----
    # git merge-base --all <master> <branch>:
    #   退出 0 且输出非空 = 有共同祖先 → 通过
    #   退出 1 且输出为空 = 无共同祖先（孤儿根）→ 拒绝 unrelated-history  ← 实测形状
    #   其它（含退出 0 却无输出）= git 无法回答 —— **不确定，禁止通过**（Rule 15）
    #
    # 不要换成 --is-ancestor：那是更严的问题，对普通分叉分支（两个方向都不是祖先）
    # 都答 1，会把每一个正常 feature 分支都拒掉。
    # 探测命令本身是共享词汇的一部分：两个家跑同一条命令，答案表才不会分叉。
    readonly SAFE_MERGE_ANCESTRY_PROBE="merge-base --all"
    # "无共同祖先"的实测签名是**成对**的：(退出码, 输出) = (1, 空)。
    readonly SAFE_MERGE_ANCESTRY_REFUSAL="1"
}
