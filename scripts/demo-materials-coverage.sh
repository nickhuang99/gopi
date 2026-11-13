#!/bin/bash
# demo-materials-coverage.sh — 素材库覆盖率仪器（Rule 38：census 数字必须能被重新推导）
#
# 它做什么：现场解析 gopi 的动词表现场（不手抄），数出各"面"的分母，
#           再数 docs/archive/demo/materials/ 下已完成的素材条目，打印覆盖率。
# 它不做什么：不修改任何东西（纯读），不跑任何 verb。
#
# 用法: scripts/demo-materials-coverage.sh [BINARY]
# 默认 BINARY 取 $GOPI_MATERIALS_BIN，否则 PATH 上的 gopi。
set -u

BIN="${1:-${GOPI_MATERIALS_BIN:-gopi}}"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
MAT="$REPO/docs/archive/demo/materials"

command -v "$BIN" >/dev/null 2>&1 || BIN="$REPO/bin/gopi"
[ -x "$BIN" ] || { echo "no gopi binary: pass one as \$1 or set GOPI_MATERIALS_BIN" >&2; exit 1; }

# --- 分母：动词表现场解析，不手抄（Rule 38 + #6791 的口径教训）---
# 族门拼写（Rule 1/13：门词只在这里写一次）。2026-10-05 的 verb-retirement epic
# 把 `sandbox` / `checkpoint` 从顶层摘除，改成 family 门的叶子
# （cmd/agent/cli_verb_aliases.go：`gopi sandbox` → `gopi tool sandbox`，
# `gopi checkpoint` → `gopi session checkpoint`）。旧拼写如今**指名拒绝且 exit 1**，
# 不再是「没有子命令」—— 继续用旧词会让这里静默数出 0（本任务修的正是这个）。
readonly DOOR_SANDBOX="tool sandbox"
readonly DOOR_CHECKPOINT="session checkpoint"
top_commands=$("$BIN" -h 2>/dev/null | sed -n '/^Commands/,/^Examples/p' | grep -cE '^  gopi ')
sub_task=$("$BIN" task -h 2>/dev/null | grep -cE '^  gopi task [a-z0-9-]+')
sub_memory=$("$BIN" memory -h 2>/dev/null | grep -cE '^  gopi memory [a-z0-9-]+')
sub_sandbox=$("$BIN" $DOOR_SANDBOX -h 2>/dev/null | grep -cE "^  gopi $DOOR_SANDBOX [a-z0-9-]+")
sub_checkpoint=$("$BIN" $DOOR_CHECKPOINT -h 2>/dev/null | grep -cE "^  gopi $DOOR_CHECKPOINT [a-z0-9-]+")
env_vars=$("$BIN" --help-env 2>/dev/null | grep -cE '^  GOPI_[A-Z0-9_]+')
# 一个族门探到 0 个子命令 = 要么拼写退役了、要么帮助面变了 —— 两种都必须响，
# 不能以 0 计入分母（Rule 15：静默失败就是缺陷）。
for probe in "task:$sub_task" "memory:$sub_memory" "$DOOR_SANDBOX:$sub_sandbox" "$DOOR_CHECKPOINT:$sub_checkpoint"; do
  case "$probe" in
    *:0) echo "demo-materials-coverage: family door '${probe%%:*}' parsed 0 subcommands — the door spelling or the help shape changed; refusing to count a silent zero" >&2; exit 1;;
  esac
done
# 展开成子命令的族头，不能再按「一个顶层命令」计一次（否则双重计数）。
# 族头在 verb-retirement 之后是 task / memory / tool / session（sandbox /
# checkpoint 已退役为族叶，见上面的 DOOR_* 常量），所以这里按「族头」的**形状**
# 匹配（`gopi <head> <subcommand>| <command>`），而不是按名字猜。
fam=0
for c in task memory tool session; do fam=$((fam + $("$BIN" -h 2>/dev/null | sed -n '/^Commands/,/^Examples/p' | grep -cE "^  gopi $c (<subcommand>|<command>)"))); done
top_exclusive=$((top_commands - fam))
total=$((top_exclusive + sub_task + sub_memory + sub_sandbox + sub_checkpoint + env_vars))

# --- 分子：已完成的素材条目（6 文件齐全才算一条）---
entries=0
for d in "$MAT"/*/; do
  [ -d "$d" ] || continue
  n=0
  for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
    [ -s "$d/$f" ] && n=$((n + 1))
  done
  [ "$n" -eq 6 ] && entries=$((entries + 1))
done

# --- 有真实不足的条目（05-finding.md 的"## 不足"节里出现了缺陷编号 F<num>.<num>）---
findings=$(grep -lE '^### F[0-9]+\.[0-9]+' "$MAT"/*/05-finding.md 2>/dev/null | wc -l)

# 按面计数：六件齐全 **且** 00-entry.md 声明了该 face 的条目（env 面判据与
# scripts/materials/env-census.sh 一致——同一判据两处实现，改动必须同时改）
face_count() {
  local want="$1" d n cnt=0 f
  for d in "$MAT"/*/; do
    [ -s "$d/00-entry.md" ] || continue
    grep -qE "^- face: $want\$" "$d/00-entry.md" || continue
    n=0
    for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
      [ -s "$d/$f" ] && n=$((n + 1))
    done
    [ "$n" -eq 6 ] && cnt=$((cnt + 1))
  done
  printf '%s' "$cnt"
}
task_entries="$(face_count task)"
env_entries="$(face_count env)"
env_probed="$(bash "$(dirname "$0")/materials/env-census.sh" "$BIN" "$REPO" 2>/dev/null | awk -F': ' '/^# vars_probed/{print $2}')"
env_probed="${env_probed:-?}"

cat <<EOF
generator        = scripts/demo-materials-coverage.sh
generator_ver    = 2 (2026-09-29, +env 面一行；env 面明细见 scripts/materials/env-census.sh)
measured_commit  = $(cd "$REPO" && git rev-parse --short HEAD)
measured_branch  = $(cd "$REPO" && git branch --show-current)
binary           = $BIN ($("$BIN" version 2>/dev/null | head -1))
materials_dir    = docs/archive/demo/materials
dirty_worktree   = $(cd "$REPO" && [ -n "$(git status --porcelain)" ] && echo yes || echo no)

dimension              denominator      covered
gopi task subcommands  $sub_task               $task_entries
GOPI_* vars probed     $env_vars                $env_probed   (generator: scripts/materials/env-census.sh；分子=被六件齐全条目点到的变量数)
EOF
printf '%-22s %-16s %s\n' "ALL (tree, all faces)" "$total" "$entries"
cat <<EOF

breakdown (分母构成，全部现场解析自 -h)
  top-level commands   $top_commands (其中 $fam 个展开为子命令族，不重复计)
  top-exclusive        $top_exclusive
  gopi task            $sub_task
  gopi memory          $sub_memory
  gopi $DOOR_SANDBOX    $sub_sandbox
  gopi $DOOR_CHECKPOINT $sub_checkpoint
  env vars (--help-env) $env_vars
  ------------------------------------
  total                $total

findings: $findings / $entries 条目带真实不足编号（"### F<n>.<m>"）
coverage: $((task_entries + env_entries))/$total 项功能点（已产出的面：task $task_entries、env $env_entries）
faces covered: task（条目 $task_entries）+ env（条目 $env_entries；姊妹任务覆盖的 env 三组不计入本数）
EOF
