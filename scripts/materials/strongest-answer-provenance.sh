#!/usr/bin/env bash
# 逐格"判定块来源"取证（反作弊：把"产品真的答了"与"探测提示词的回显"分开）。
#
# 机理：每份探测输入的最后三行是形如 `M1_VERDICT=<NATIVE 或 NONE>` 的**占位符**，
# 产品被要求在自己的输出末尾写出**实值**行。本脚本对每个 .out 报告：
#   prompt_at  = 占位符行（`X_VERDICT=<...>`）出现的最后行号
#   answer_at  = 占位符**之后**才出现的实值行（`X_VERDICT=NATIVE|NONE`）行号
#   若 answer 缺失 → 该格**没有产出判定答案**（rc=137 被杀 / 空文件）
#
# 只读：不写四家任何目录。
# 用法：strongest-answer-provenance.sh <strongest-dir>
set -u
S=${1:?usage: strongest-answer-provenance.sh <strongest-dir>}

printf '%-32s %-6s %-10s %-12s %-8s %s\n' cell rc prompt_at answer_at lines provenance
for f in "$S"/raw/probe-m*.out; do
  b=$(basename "$f" .out)
  rc=$(cat "$S/raw/$b.rc" 2>/dev/null || echo "-")
  # the placeholder line the probe itself asks for
  pl=$(grep -n '^[A-Z0-9]*_VERDICT=<' "$f" 2>/dev/null | tail -1 | cut -d: -f1)
  # a REAL value line: verdict token, no angle brackets (the prompt says "若…直接输出")
  al=$(grep -nE '^[^<]*[A-Z0-9]+_VERDICT=(NATIVE|NONE)[[:space:]]*$' "$f" 2>/dev/null | tail -1 | cut -d: -f1)
  end=$(awk 'END{print NR}' "$f")
  if [ -z "$pl" ] && [ -z "$al" ] && [ "$end" = "0" ]; then
    prov="EMPTY-FILE (killed before any output)"
  elif [ -n "$al" ] && { [ -z "$pl" ] || [ "$al" -gt "${pl:-0}" ]; }; then
    prov="ANSWER (line $al of $end)"
  else
    prov="NO-ANSWER (only the probe's own placeholder echoed; truncated mid-run)"
  fi
  printf '%-32s %-6s %-10s %-12s %-8s %s\n' "$b" "$rc" "${pl:-none}" "${al:-none}" "$end" "$prov"
done
