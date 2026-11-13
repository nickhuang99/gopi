#!/usr/bin/env bash
# =============================================================================
# post-merge-cleanup.sh — merge 后分支自动清理（竞争收尾 3 步流程的第 2 步）
#
# 用法:  bash scripts/post-merge-cleanup.sh [--dry-run] <branch>...
#   或:  bash scripts/post-merge-cleanup.sh --auto    （无参数：扫描全部已合分支）
#
# 语义:
#   对每个传入分支，若已 merge 进主分支（git branch --merged），则:
#     1. 归档 + 回收 ref，**只走唯一写入者** `gopi task archive`：
#        它写 archive/<branch> 的同时把 WHY 记到 refs/gopi/archive-reason/<branch>，
#        并由自己的 reclaim 腿收回 head ref（记成一条 branch-delete 审计记录）。
#     2. 复查：ref 已不在 → 计入已回收；仍在 → 报告并拒绝自行删除
#        （本脚本过去跑裸 `git branch -d/-D`，那次删除没有任何留痕 —— 2026-10-15 实测）。
#   未 merge 的分支一律跳过（绝不删除未合工作）—— 本脚本只清理坟场，不做裁决。
#
# 检查项:
#   a. 主分支存在性    : SAFE_MERGE_MASTER（默认 master）必须存在
#   b. 已合判定        : git branch --merged <master> 白名单判定
#   c. worktree 占用   : 被占用的分支跳过（写入者的守卫也拒绝动别人的 tree，提前跳过更清晰）
#   d. master/main 本体: 永不删除自身
#   e. 当前 checkout   : 当前分支在被清理列表中 → 跳过
#
# 环境变量:
#   SAFE_MERGE_MASTER=master   对比用主分支名（与 safe-merge.sh 同一约定）
#   POST_MERGE_TAG=1|0         是否走唯一写入者（默认 1；0=只报告，绝不自行删 ref）
#   GOPI_BIN=<path>            gopi 可执行文件（默认按 PATH 查 gopi）
#
# 典型收尾三步（Rule 9）:
#   bash scripts/safe-merge.sh <branch>          # 前置守卫（拦截败者/占用）
#   git merge <branch>                            # 胜者入 master（PA 验收后）
#   bash scripts/post-merge-cleanup.sh <branch>   # 本脚本：归档 + 回收 ref（唯一写入者）
# =============================================================================
set -u

# ---- 常量（Rule 1: 命名，无魔法数字/字符串散落）----
readonly DEFAULT_MASTER="master"
# 归档原因（tools.ArchiveReasons 词表之一；由 gopi 自己校验，写错会被拒）。
# 本脚本只清理"已 merge 进 master"的分支 —— 工作已进 master 历史，分支 ref 本身用尽，
# 正是 superseded 的语义。--override-completed 是显式确认：分支已被证明合并，
# 所以"该行 completed 就不许归档"的拒绝在这里必须被有理由地越过。
readonly DEFAULT_ARCHIVE_REASON="superseded"
readonly GOPI_BIN="${GOPI_BIN:-gopi}"
readonly AUTO_SCAN_ALL="--auto"
readonly FLAG_DRY_RUN="--dry-run"

MASTER="${SAFE_MERGE_MASTER:-$DEFAULT_MASTER}"
TAG_ENABLED="${POST_MERGE_TAG:-1}"
DRY_RUN=0
BRANCHES=()

# ---- 参数解析: flags 任意位置, 分支为其余 positional ----
for arg in "$@"; do
    case "$arg" in
        "$FLAG_DRY_RUN") DRY_RUN=1 ;;
        "$AUTO_SCAN_ALL") AUTO=1 ;;
        -*) echo "post-merge-cleanup: unknown flag $arg" >&2; exit 2 ;;
        *)  BRANCHES+=("$arg") ;;
    esac
done

# ---- a. 主分支存在性 ----
if ! git show-ref --verify --quiet "refs/heads/$MASTER"; then
    echo "❌ 主分支 '$MASTER' 不存在（SAFE_MERGE_MASTER 可覆盖）" >&2
    exit 2
fi

CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"

# --auto: 扫描全部已合分支（排除主分支/当前分支）作为清理列表
if [ "${AUTO:-0}" -eq 1 ]; then
    mapfile -t BRANCHES < <(git branch --merged "$MASTER" --format='%(refname:short)' \
        | grep -vx "$MASTER" | grep -vx "$CURRENT_BRANCH" || true)
    if [ "${#BRANCHES[@]}" -eq 0 ]; then
        echo "✅ --auto: 没有已合入 $MASTER 的残留分支 —— 分支坟场为空"
        exit 0
    fi
fi

if [ "${#BRANCHES[@]}" -eq 0 ]; then
    echo "用法: bash scripts/post-merge-cleanup.sh [--dry-run] <branch>... | --auto" >&2
    exit 2
fi

# ---- 逐分支清理 ----
REMOVED=0
SKIPPED=0
# PLANNED 是 --dry-run 的计数：dry-run 不改变任何东西，所以它**不能**和 REMOVED 共用
# 一个计数器 —— 否则总结行会报告一次没有发生的删除（实测：`--dry-run` 打印"已删除 1"
# 而 ref 还在）。两个事实分开记（Rule 1b）。
PLANNED=0
declare -a REMOVED_NAMES=()
declare -a SKIPPED_REASONS=()
declare -a PLANNED_NAMES=()

for BRANCH in "${BRANCHES[@]}"; do
    # d. 永不删除主分支本体 / 当前 checkout 分支
    if [ "$BRANCH" = "$MASTER" ] || [ "$BRANCH" = "$CURRENT_BRANCH" ]; then
        SKIPPED=$((SKIPPED+1))
        SKIPPED_REASONS+=("$BRANCH: 自身/当前 checkout — 永不删除")
        continue
    fi
    # 分支必须存在
    if ! git show-ref --verify --quiet "refs/heads/$BRANCH"; then
        SKIPPED=$((SKIPPED+1))
        SKIPPED_REASONS+=("$BRANCH: 分支不存在")
        continue
    fi
    # b. 已合判定 —— 未合分支一律跳过（本工具不做裁决）
    if ! git branch --merged "$MASTER" --list "$BRANCH" | grep -q .; then
        SKIPPED=$((SKIPPED+1))
        SKIPPED_REASONS+=("$BRANCH: 未 merge 进 $MASTER — 跳过（绝不删未合工作）")
        continue
    fi
    # c. worktree 占用 — branch -d 对 checked-out 分支会拒绝，提前跳过
    if git worktree list 2>/dev/null | grep -q "\[$BRANCH\]"; then
        SKIPPED=$((SKIPPED+1))
        SKIPPED_REASONS+=("$BRANCH: 仍被 worktree 占用 — 先 git worktree remove")
        continue
    fi
    # 独立提交数（tag 命名与留痕用）
    AHEAD=$(git rev-list --count "$MASTER.."$BRANCH 2>/dev/null || echo 0)

    if [ "$DRY_RUN" -eq 1 ]; then
        echo "[dry-run] 将归档 + 回收: $BRANCH (ahead=$AHEAD, 写入者=$([ "$TAG_ENABLED" = "1" ] && echo "${GOPI_BIN} task archive" || echo "关闭（POST_MERGE_TAG=0）"))"
        PLANNED=$((PLANNED+1))
        PLANNED_NAMES+=("$BRANCH")
        continue
    fi

    # 1. archive tag — 仅有独立提交时打（纯 fast-forward 无内容可归档）
    #
    #    只走唯一写入者 `gopi task archive`：它写 archive/<branch> 的同时把 WHY
    #    记到 refs/gopi/archive-reason/<branch>。本脚本过去自己 mint
    #    "${TAG_PREFIX}/${BRANCH}-$(date +%Y%m%d)" —— 与 GOPI.md Rule 9 教给每个
    #    agent 的 `git tag archive/<name>-<date>` 是同一个形状，2026-10-12 09:46:14
    #    那批十枚无 reason 的 tag 就是这么来的（-YYYYMMDD 后缀是识别标志——没有任何
    #    gopi 写入者产出该形状）。注意：本脚本里那条 mint 分支其实够不到——below
    #    只在 AHEAD>0 时归档，而本脚本只清理"已合进 master"的分支，AHEAD 恒为 0；
    #    所以那批 tag 不是本脚本打的，是 agent 照 Rule 9 手打的。本脚本只是同一套
    #    命名约定的携带者，改它是为了让约定不再被传播。tag 是每条 merge 路径都认的
    #    DECISION，没有 WHY 的决定无法复核也无法回退。
    #    回归守卫 internal/standards/archive_mint_gate.go 会在任何本树脚本或指令
    #    再次渲染裸 mint 时让构建变红。
    #
    #    gopi 缺失时不退化成裸 git tag（那正是要消除的行为）：警告并继续，而 ref 删除
    #    也一并由写入者负责 —— 失败时下一步会拒绝删除并把分支报成 SKIPPED，绝不退化成
    #    自己动手。
    #
    #    **AHEAD 不再是前置条件**：AHEAD>0 只可能出现在"有独立提交"的已合分支上，
    #    而本脚本只清理"已 merge 进 master"的分支 —— 旧注释据此认为归档腿够不到；
    #    但归档腿还有第二个职责，也正是本脚本第 2 步要的那件事：**收回 head ref**
    #    （archive-ref-reclaim，2026-10-01）。对 ahead=0 的已合分支来说，"内容已在
    #    master、ref 已用尽、decision 是 superseded"恰恰是归档要记录的东西，而
    #    `gopi task archive` 实测会写下 archive/<branch> + reason ref 并删掉 head
    #    （见第 2 步注释）。所以归档腿对每个已合分支都调用一次。
    if [ "$TAG_ENABLED" = "1" ]; then
        ARCH_DETAIL="post-merge-cleanup.sh: ${BRANCH} 已 merge 进 ${MASTER} (ahead=${AHEAD})，随后回收 refs/heads/${BRANCH}"
        if "${GOPI_BIN}" task archive "$BRANCH" --reason "$DEFAULT_ARCHIVE_REASON" --detail "$ARCH_DETAIL" --override-completed; then
            echo "   🏷  已归档 $BRANCH (reason=${DEFAULT_ARCHIVE_REASON})"
        else
            echo "   ⚠️  gopi task archive $BRANCH 失败（不自行删除 ref —— 见第 2 步）"
        fi
    fi

    # 1b. backfill 的 archive tag 与回收归 archive 腿；若分支 ref 已不在，跳过删除
    if ! git show-ref --verify --quiet "refs/heads/$BRANCH"; then
        echo "   🗑  分支 $BRANCH 已由 archive 收回 (ahead=$AHEAD)"
        REMOVED=$((REMOVED+1))
        REMOVED_NAMES+=("$BRANCH")
        continue
    fi

    # 2. 回收分支 ref —— **只能**走唯一写入者 `gopi task archive`（见上方 1.）
    #
    #    这里过去跑 `git branch $DEL_FLAG "$BRANCH"`（--force 把 -d 升级成 -D）。
    #    那是一次**没有留痕的删除**：ref 消失，而 removal-audit sink 一个字都没有
    #    —— 2026-10-15 在 fixture repo 上实测到的形状（sink 根本没被创建）。
    #    同一个"唯一写入者"已经会做这件事：`gopi task archive <branch> --reason …`
    #    的 reclaim 腿（internal/tools/archive_ref_reclaim.go）在写下 archive/<branch>
    #    + refs/gopi/archive-reason/<branch> 之后，用 `git update-ref -d` 收掉 head，
    #    并把这一次删除记成 `arm=archive-reclaim-branch` 的一条 branch-delete 记录。
    #    所以本脚本不再自己删 ref：上面第 1 步的归档成功时它已经删好了，这里只需
    #    确认结果（ref 是否还在），而不是再补一刀。
    #
    #    两个安全性质由写入者自己的五道守卫提供（tag 必须存在、tag 与 head 必须同
    #    一个 commit、被 worktree 占用的分支不动、master/main 永不动），比 `branch -d`
    #    的"只看 upstream 是否合并"更贴切：本脚本只清理已合分支，但"已合"不等于
    #    "可以不留痕地删掉"。
    #
    #    归档被禁用（POST_MERGE_TAG=0）时这里**拒绝删除**并报告：那正是"要删 ref 却没
    #    有写入者"的状态，静默 `git branch -d` 就是本脚本被修的缺陷本身。
    if git show-ref --verify --quiet "refs/heads/$BRANCH"; then
        if [ "$TAG_ENABLED" = "1" ]; then
            SKIPPED=$((SKIPPED+1))
            SKIPPED_REASONS+=("$BRANCH: 仍存在 — 唯一写入者未收回 ref（gopi task archive 失败？见上方 ⚠️）")
        else
            SKIPPED=$((SKIPPED+1))
            SKIPPED_REASONS+=("$BRANCH: POST_MERGE_TAG=0 时本脚本不删 ref —— ref 删除只能走唯一写入者 gopi task archive")
        fi
    else
        echo "   🗑  分支 $BRANCH 已回收 (ahead=$AHEAD)"
        REMOVED=$((REMOVED+1))
        REMOVED_NAMES+=("$BRANCH")
    fi
done

# ---- 结论 ----
echo ""
echo "== post-merge-cleanup 结论 =="
if [ "$REMOVED" -gt 0 ]; then
    echo "🗑  已回收 ${REMOVED}: ${REMOVED_NAMES[*]}"
fi
if [ "$PLANNED" -gt 0 ]; then
    echo "🧪 dry-run 计划 ${PLANNED}: ${PLANNED_NAMES[*]}（什么都没改）"
fi
if [ "$SKIPPED" -gt 0 ]; then
    echo "⏭  跳过 ${SKIPPED}:"
    for r in "${SKIPPED_REASONS[@]}"; do echo "   - $r"; done
fi
if [ "$REMOVED" -eq 0 ] && [ "$SKIPPED" -eq 0 ]; then
    echo "（无事可做）"
fi

# 回归验收（Rule 9 第 3 步）: 清理后应只剩 胜者/master/进行中分支
echo ""
echo "== 复查: 剩余分支 =="
git branch --format='%(refname:short)' | sed 's/^/   /'
[ "$DRY_RUN" -eq 1 ] && exit 0
exit 0
