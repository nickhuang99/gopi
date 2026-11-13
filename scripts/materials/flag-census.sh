#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# flag-census.sh — F 面（**顶层 flag**）的分母生成器（Rule 38：census 数字必须
# 点名生成器命令 + measured_commit）。
#
# 用途：一条命令打印「旧口径（错）→ 新口径（对）」的对照，并逐字打印本轮的分母。
# 只读：不写台账、不建沙盒、不改任何文件（除了 stdout）。
#
#   bash scripts/materials/flag-census.sh            # 用仓库内 bin/gopi（没有就先用 GOPI_BIN 指定）
#   GOPI_BIN=/path/to/gopi bash scripts/materials/flag-census.sh
set -uo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${GOPI_BIN:-$ROOT/bin/gopi}"
[[ -x "$BIN" ]] || { echo "no binary at $BIN (build: cd $ROOT && CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/, or set GOPI_BIN)" >&2; exit 1; }

echo "# generator: scripts/materials/flag-census.sh"
echo "# measured_commit: $(git -C "$ROOT" rev-parse --short HEAD)"
echo "# binary: $BIN"
echo "# binary_sha256_16: $(sha256sum "$BIN" | cut -c1-16)"
echo "# built_with: CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/"
echo

echo "## 旧口径（本 run 证伪）—— 数的是另一个 CLI 的版式，恒为 0"
echo '$ gopi -h | sed -n '"'"'/Flags:/,$p'"'"' | grep -cE '"'"'^\s+--'"'"''
OLD="$("$BIN" -h 2>&1 | sed -n '/Flags:/,$p' | grep -cE '^\s+--' || true)"
echo "$OLD"
echo "# 该段在 gopi -h 里根本不存在（gopi 的段名是 Options），sed 打印空段 ⇒ grep -c 得 0；"
echo "# 而 grep -c 的退出码在 0 命中时是 1——旧口径连'失败'和'零'都分不开。"
echo

echo "## 新口径（正确）—— Options 段 + 两空格缩进的 --flag"
echo '$ gopi -h | sed -n '"'"'/^Options /,$p'"'"' | grep -cE '"'"'^  --[a-z]'"'"''
NEW="$("$BIN" -h 2>&1 | sed -n '/^Options /,$p' | grep -cE '^  --[a-z]')"
echo "$NEW"
echo

echo "## 去重后（本面分母；本 run 实测与上式相等，即无别名重复）"
echo '$ gopi -h | sed -n '"'"'/^Options /,$p'"'"' | grep -oE '"'"'^  --[a-z][a-z-]*'"'"' | sort -u | wc -l'
UNIQ="$("$BIN" -h 2>&1 | sed -n '/^Options /,$p' | grep -oE '^  --[a-z][a-z-]*' | sort -u | wc -l)"
echo "$UNIQ"
echo

echo "## 逐项（分母的成员，逐字）"
"$BIN" -h 2>&1 | sed -n '/^Options /,$p' | grep -oE '^  --[a-z][a-z-]*' | sed 's/^  //' | sort -u | tr '\n' ' '
echo
echo
echo "# 对照（本 run 一并实测，证明口径只差'段名'一处）："
echo "#   宽松口径（任意缩进的 --flag，含正文里的举例，如 --prompt -z、--help-env）: $("$BIN" -h 2>&1 | grep -oE '(^|[[:space:]])--[a-z][a-z-]*' | grep -oE '\-\-[a-z][a-z-]*' | sort -u | wc -l)"
echo "# 结论：gopi -h 的顶层 flag 面 = ${UNIQ} 项（Options 段每行一项，无别名）。"
