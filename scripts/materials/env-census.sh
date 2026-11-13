#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# env-census.sh — 「环境变量面」的现场解析 census（Rule 38：数字必须自报生成器与 measured_commit）。
#
# 它做什么（全部只读）：
#   1. 从**活的二进制** `--help-env` 现场解析：分组数、每组的变量、变量总数；
#   2. 对每个变量在**本 commit 的源码**里找它的字面定义点（file:line，优先非 _test 的命中）；
#   3. 打印分组→变量清单，并标注每个变量是否已被「真改值→观察到差异」的六件套条目取证；
#   4. 末尾汇总：分组数 / 已取证变量数 / 仅表格式变量数 / 已取证组数。
# 它不做什么：不写素材树、不跑任何 verb、不改任何文件（中间件只落临时目录）。
#
# 用法: scripts/materials/env-census.sh [BIN] [REPO]
#   BIN  默认 $GOPI_MATERIALS_BIN，否则 PATH 上的 gopi
#   REPO 默认本脚本所在仓
set -u

# Rule 22: the scratch directory comes from scripts/lib/tmp-ban.sh — the ONE
# reader of the scratch root (GOPI_TEST_SCRATCH_HOME, else GOPI_TEST_HOME's
# workspace, the tree the Go suite GC sweeps). The helper asserts by DEVICE ID
# that it is off `/` and registers it for trap-time cleanup; /tmp shares `/`'s
# device here, so it is never a candidate and there is no fallback to it.
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/tmp-ban.sh"

BIN="${1:-${GOPI_MATERIALS_BIN:-gopi}}"
REPO="${2:-$(cd "$(dirname "$0")/../.." && pwd)}"
MAT="$REPO/docs/archive/demo/materials"

command -v "$BIN" >/dev/null 2>&1 || BIN="$REPO/bin/gopi"
[ -x "$BIN" ] || { echo "no gopi binary: pass one as \$1 or set GOPI_MATERIALS_BIN" >&2; exit 1; }
REPO_TOP="$(cd "$REPO" && git rev-parse --show-toplevel 2>/dev/null || echo "$REPO")"

SCRATCH="$(gopi_scratch_mkdir env-census)" || exit 1
trap 'gopi_scratch_cleanup' EXIT
ENVDUMP="$SCRATCH/help-env.txt"
BODY="$SCRATCH/body.txt"

"$BIN" --help-env >"$ENVDUMP" 2>/dev/null || { echo "cannot read --help-env from $BIN" >&2; exit 1; }
total=$(grep -cE '^  (GOPI_|ANTHROPIC_|DEEPSEEK_)[A-Z0-9_]+' "$ENVDUMP")
gopi_total=$(grep -cE '^  GOPI_[A-Z0-9_]+' "$ENVDUMP")

# 一个变量的字面定义点：优先**非 _test** 的第一次出现，输出 file:line
defsite() {
  local v="$1" hit
  hit="$(cd "$REPO_TOP" && git grep -n -e "\"$v\"" -- '*.go' 2>/dev/null | grep -v '_test\.go' | head -1)"
  [ -z "$hit" ] && hit="$(cd "$REPO_TOP" && git grep -n -e "\"$v\"" 2>/dev/null | head -1)"
  printf '%s' "$hit" | awk -F: 'NF>=2 {print $1":"$2}'
}

# 一个变量是否已被取证：某条目 00-entry.md 的 feature/group 字段点到了它，且该条目六件齐全
probed_by() {
  local v="$1" d n f
  for d in "$MAT"/*/; do
    [ -s "$d/00-entry.md" ] || continue
    grep -q "GOPI_$v" "$d/00-entry.md" 2>/dev/null || continue
    n=0
    for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
      [ -s "$d/$f" ] && n=$((n+1))
    done
    [ "$n" -eq 6 ] && { basename "$d"; return 0; }
  done
}

# --- 主体：分组标题（顶格）→ 变量（缩进两格） --------------------------------
awk '
  /^Precedence:/ { armed=1; next }
  !armed { next }
  /^[A-Za-z][A-Za-z0-9 &()\/._-]*$/ { if (grp!="") printf "## %s\t%d\n%s", grp, n, vars; grp=$0; n=0; vars=""; next }
  /^  [A-Z][A-Z0-9_]+[ ]/ { n++; vars = vars "  " $1 "\n" }
  END { if (grp!="") printf "## %s\t%d\n%s", grp, n, vars }
' "$ENVDUMP" > "$SCRATCH/groups.txt"
groups_parsed=$(grep -c '^## ' "$SCRATCH/groups.txt")

{
  while IFS= read -r line; do
    case "$line" in
      '## '*)
        gname="${line%$'\t'*}"; gcount="${line##*$'\t'}"
        printf '\n[group] %s  (vars=%s)\n' "$gname" "$gcount"
        ;;
      '  '*)
        v="${line#  }"
        ds="$(defsite "$v")"
        pv="$(probed_by "${v#GOPI_}")"
        if [ -n "$pv" ]; then pstat="probed:$pv"; else pstat="table-only"; fi
        printf '  %-32s %-46s %s\n' "$v" "${ds:-<no literal definition site>}" "$pstat"
        ;;
    esac
  done < "$SCRATCH/groups.txt"
} > "$BODY"

# 已产出的 env 面条目数：00-entry.md 声明 face: env 且六件齐全
env_entries=0
for d in "$MAT"/*/; do
  [ -s "$d/00-entry.md" ] || continue
  grep -qE '^- face: env' "$d/00-entry.md" || continue
  n=0
  for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
    [ -s "$d/$f" ] && n=$((n+1))
  done
  [ "$n" -eq 6 ] && env_entries=$((env_entries+1))
done

prob_n=$(grep -c 'probed:' "$BODY" || true)
table_n=$(grep -c 'table-only' "$BODY" || true)
groups_probed=$(awk '/^\[group\]/{g=$0; hit=0} /probed:/{if(g!="" && hit==0){print g; hit=1}}' "$BODY" | wc -l)

cat <<EOF
# generator: scripts/materials/env-census.sh
# generator_version: 1 (2026-09-29, epic [6/12] E 面批2)
# measured_commit: $(cd "$REPO_TOP" && git rev-parse --short HEAD)
# measured_branch: $(cd "$REPO_TOP" && git branch --show-current)
# binary: $BIN ($("$BIN" version 2>/dev/null | head -1))
# dirty_worktree: $( [ -n "$(cd "$REPO_TOP" && git status --porcelain)" ] && echo yes || echo no )
# env_names_all_prefixes: $total
# env_names_GOPI_: $gopi_total
# groups_parsed: $groups_parsed
# groups_with_probe: $groups_probed
# vars_probed: $prob_n
# vars_table_only: $table_n
# env_entries_complete: $env_entries  (六件齐全且 face: env 的条目 = 本面已产出数)
#
# 判据（可复算）：vars_probed = 某条目 00-entry.md 点到了该变量名、且该条目六件齐全；
# 未命中的变量是 table-only（只登记变量/默认/reader file:line，未逐条真跑）。

GROUP / VAR / reader file:line / 取证状态 — 全部现场解析，不手抄
EOF
cat "$BODY"
