#!/usr/bin/env bash
# =============================================================================
# safe-merge.sh — 竞争收尾守卫（Layer 2 兜底）
#
# 用法:  bash scripts/safe-merge.sh <branch>
#   merge 前守卫检查 —— 全部通过才允许 merge（exit 0 + ✅）
#   任一拦截项触发 —— 拒绝 merge（exit 1 + ❌）
#
# 检查项:
#   a. archive tag 检查 : git tag -l "archive/<branch>" 存在 = 该分支已存档(败者) -> 拒绝
#   b. 分支名黑名单     : loser/* 或 archive/* 前缀 -> 拒绝
#   c. worktree 占用    : git worktree list 仍挂着该分支 -> 拒绝(默认) / 可经
#                         SAFE_MERGE_WORKTREE=warn 降级为警告
#   d. 未 merge 提示    : git branch --merged master 对比 -> 信息输出
#   e. ref 存在性       : git rev-parse --verify --quiet refs/heads/<b> 无法解析
#                         -> 拒绝（GHOST 分支：一个已经不在的名字曾被 4 条 ✅
#                         认证为"可安全 merge"，派出一个求解者去 merge 一个
#                         不存在的 ref —— 这正是本项的由来）
#   f. 历史相关性       : 分支与 master 无共同祖先（孤儿根）-> 拒绝
#                         （git 只在 --allow-unrelated-histories 下才接受
#                         这种 merge，任何 merge 通道都永远落不了它）
#
# [e]/[f] 的拒绝词汇与 Go 镜像共用一份契约 —— scripts/lib/safe-merge-contract.sh
# （Go 侧: internal/tools/safe_merge_contract.go）。改动其中一侧必须同步另一侧。
#
# 环境变量:
#   SAFE_MERGE_WORKTREE=warn|deny   worktree 占用时行为 (默认 deny)
#   SAFE_MERGE_MASTER=main          对比用的主分支名 (默认 master)
# =============================================================================
set -u

# ---- 常量（Rule 1: 无魔法数字/字符串散落）----
readonly BAD_PREFIXES=("loser/" "archive/")
readonly DEFAULT_MASTER="master"
readonly WORKTREE_DEFAULT="deny"

# ---- 参数 ----
if [ "$#" -ne 1 ]; then
    echo "用法: bash scripts/safe-merge.sh <branch>" >&2
    exit 2
fi
BRANCH="$1"
MASTER="${SAFE_MERGE_MASTER:-$DEFAULT_MASTER}"
WORKTREE_POLICY="${SAFE_MERGE_WORKTREE:-$WORKTREE_DEFAULT}"

# ---- 共享拒绝词汇（唯一契约源）----
# MASTER 必须先定好再 source（库的渲染句子要用它）；库只声明字符串，不建文件。
SAFE_MERGE_CONTRACT_LIB="$(dirname "$0")/lib/safe-merge-contract.sh"
if [ ! -r "$SAFE_MERGE_CONTRACT_LIB" ]; then
    # Rule 15: 契约读不到就拒绝，绝不带着一份凭记忆拼出来的词汇继续 —— 一个
    # 认不出自己拒绝理由的守卫，正是这道闸失效的形状。
    echo "❌ 拒绝: safe-merge 契约库缺失/不可读: $SAFE_MERGE_CONTRACT_LIB —— 无法保证拒绝词汇与 Go 镜像一致，禁止 merge" >&2
    exit 1
fi
. "$SAFE_MERGE_CONTRACT_LIB"

FAIL=0
# REFUSAL_KINDS 收集本次命中的拒绝种类（共享词汇表的 kind token），结论行按名
# 列出 —— 机器可读的是 kind，人读的是 ❌ 句子。
REFUSAL_KINDS=""

echo "== safe-merge 守卫: $BRANCH =="

# ---- a. archive tag 检查 ----
if git tag -l "archive/$BRANCH" | grep -q .; then
    echo "❌ 拒绝: 分支 '$BRANCH' 已有存档 tag 'archive/$BRANCH' —— 已归档(败者)，禁止 merge" >&2
    FAIL=1
    REFUSAL_KINDS="$REFUSAL_KINDS $SAFE_MERGE_REFUSAL_ARCHIVE_TAG"
else
    echo "✅ [a] archive tag: 无 archive/$BRANCH —— 未存档"
fi

# ---- b. 分支名黑名单 ----
BLACKLISTED=0
for prefix in "${BAD_PREFIXES[@]}"; do
    case "$BRANCH" in
        "$prefix"*)
            echo "❌ 拒绝: 分支名 '$BRANCH' 命中黑名单前缀 '$prefix'" >&2
            BLACKLISTED=1
            FAIL=1
            REFUSAL_KINDS="$REFUSAL_KINDS $SAFE_MERGE_REFUSAL_BLACKLIST_PREFIX"
            ;;
    esac
done
[ "$BLACKLISTED" -eq 0 ] && echo "✅ [b] 分支名黑名单: '$BRANCH' 无违规前缀"

# ---- c. worktree 占用检查 ----
if git worktree list | grep -q "\[$BRANCH\]"; then
    echo "⚠️  [c] 分支 '$BRANCH' 仍被 worktree 占用（未清理）" >&2
    if [ "$WORKTREE_POLICY" = "deny" ]; then
        echo "❌ 拒绝: worktree 占用默认拒绝 —— 请先: git worktree remove <路径>" >&2
        FAIL=1
        REFUSAL_KINDS="$REFUSAL_KINDS $SAFE_MERGE_REFUSAL_WORKTREE_OCCUPIED"
    else
        echo "   （SAFE_MERGE_WORKTREE=warn —— 仅警告，继续）"
    fi
else
    echo "✅ [c] worktree 占用: 无 worktree 挂着 '$BRANCH'"
fi

# ---- d. 未 merge 提示（信息输出，不拦截）----
if git branch --merged "$MASTER" --list "$BRANCH" | grep -q .; then
    echo "ℹ️  [d] 分支 '$BRANCH' 已 merge 进 $MASTER（历史分支？）"
else
    echo "ℹ️  [d] 分支 '$BRANCH' 尚未 merge 进 $MASTER"
fi

# ---- e. ref 存在性检查（拒绝）----
# 一个已经把 ref 删掉的分支（e2e 探针、已 merge 后被 `git branch -d`、拼错的
# 名字）：rev-parse --verify --quiet 退出 1 且**不打印任何东西**。这正是
# `--quiet` 与 `^{commit}` 这对参数的意义（与 Go 侧 repoSweepRefPresent 同一契约）——
#   ref 缺失   = 退出 1，**无输出**
#   仓库级故障 = 退出 128 / **带** 文字（"not a git repository" …）
# 这两种绝不能塌缩成同一个判决（Rule 15）：缺失点名缺的那个名字，故障说
# "我回答不了"，都不允许通过。用一个子 shell 捕获 (rc, 输出) 两半。
REF_EXISTS=1
REF_RC=0
REF_OUT=""
REF_OUT=$(git rev-parse --verify --quiet "refs/heads/$BRANCH^{commit}" 2>&1) || REF_RC=$?
if [ "$REF_RC" -ne 0 ]; then
    REF_EXISTS=0
    if [ -n "$REF_OUT" ]; then
        # 非空输出 = git 报的是**它自己的故障**，不是"这个 ref 不存在"。
        # shellcheck disable=SC2059  # 格式串来自共享契约库（唯一源）
        printf '❌ 拒绝: %s (rev-parse: %s)\n' "$(printf "$SAFE_MERGE_MSG_PROBE_FAILED" "$REF_OUT")" "$REF_OUT" >&2
        FAIL=1
        REFUSAL_KINDS="$REFUSAL_KINDS $SAFE_MERGE_REFUSAL_PROBE_FAILED"
    else
        # shellcheck disable=SC2059  # 格式串来自共享契约库（唯一源），非自由文本
        printf '❌ 拒绝: %s (rev-parse: %s)\n' "$(printf "$SAFE_MERGE_MSG_MISSING_REF" "$BRANCH")" "$REF_OUT" >&2
        FAIL=1
        REFUSAL_KINDS="$REFUSAL_KINDS $SAFE_MERGE_REFUSAL_MISSING_REF"
    fi
else
    echo "✅ [e] ref 存在性: refs/heads/$BRANCH 可解析"
fi
# ---- f. 历史相关性检查（拒绝：孤儿根 / 无共同历史）----
# 仅在 ref 存在时才有意义：ref 不存在时 [e] 已经点名了缺的那个名字，再问
# `merge-base master <missing>` 只会重复一句 git 的抱怨。
#
# 探测是 `git merge-base --all $MASTER $BRANCH`：**退出码 + 输出**一起才是答案 ——
#   退出 0 且输出非空 = 有共同祖先 → 通过
#   退出 1 且输出为空 = 无共同祖先（孤儿根）→ 拒绝 unrelated-history   ← 实测形状
#   其它（含退出 0 却无输出）= git 无法回答 → **不确定，禁止通过**（inconclusive 绝不是"通过"）
#
# 为何不用 `git merge-base --is-ancestor`：那是比"有无共同历史"**更严**的问题，
# 对**普通分叉分支**（都从同一 base 出发、之后各自前进 —— 每个真实 feature
# 分支的形状）两个方向都答 1，等于把本守卫存在的目的（放行可 merge 的分支）
# 反过来拒掉。实测 2026-09-23：它拒了 TestMergeExecConflict 的普通 master/
# feat/e2e-merge 夹具。
if [ "$REF_EXISTS" -eq 1 ]; then
    ANCESTRY_OUT=$(git merge-base --all "$MASTER" "$BRANCH" 2>/dev/null)
    ANCESTRY_RC=$?
    if [ "$ANCESTRY_RC" -eq 0 ] && [ -n "$ANCESTRY_OUT" ]; then
        echo "✅ [f] 历史相关性: '$BRANCH' 与 $MASTER 有共同祖先"
    elif [ "$ANCESTRY_RC" = "$SAFE_MERGE_ANCESTRY_REFUSAL" ] && [ -z "$ANCESTRY_OUT" ]; then
        # shellcheck disable=SC2059  # 格式串来自共享契约库（唯一源）
        printf '❌ 拒绝: %s (git merge-base %s %s 报无共同祖先)\n' "$(printf "$SAFE_MERGE_MSG_UNRELATED_HISTORY" "$MASTER")" "$MASTER" "$BRANCH" >&2
        FAIL=1
        REFUSAL_KINDS="$REFUSAL_KINDS $SAFE_MERGE_REFUSAL_UNRELATED_HISTORY"
    else
        # shellcheck disable=SC2059  # 格式串来自共享契约库（唯一源）
        printf '❌ 拒绝: %s (git merge-base --all %s %s 退出 %s)\n' "$(printf "$SAFE_MERGE_MSG_ANCESTRY_PROBE_FAILED" "exit $ANCESTRY_RC")" "$MASTER" "$BRANCH" "$ANCESTRY_RC" >&2
        FAIL=1
        REFUSAL_KINDS="$REFUSAL_KINDS $SAFE_MERGE_REFUSAL_PROBE_FAILED"
    fi
fi

# ---- 结论 ----
if [ "$FAIL" -ne 0 ]; then
    echo ""
    echo "❌ safe-merge: 存在拦截项 —— 禁止 merge '$BRANCH' (拒绝种类:${REFUSAL_KINDS})"
    exit 1
fi

echo ""
echo "✅ safe-merge: 全部通过 —— 可安全 merge '$BRANCH'"
exit 0
