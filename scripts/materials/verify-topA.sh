#!/usr/bin/env bash
# scripts/materials/verify-topA.sh — A 面（顶层命令，批 1）素材条目的**重放自证**工具。
#
# 它做什么：把 docs/archive/demo/materials/1[01][0-9]-*/01-input.txt **逐字当脚本执行**，
# 把这一次的 stdout+stderr+exit 与同目录 02-output.txt 逐字节比对，给出
# per-entry 的 identical/differs 判决 + 逐条 diff 文件。
#
# 为什么可信：01-input.txt 本身就是可执行的 bash（`cd <cwd>` / `GOPI_HOME=<sb> \` /
# 二进制绝对路径 + 逐字参数），所以"复跑"不是重新描述一遍命令，而是**执行那份记录**。
#
# 诚实性约束：
#  * READ-ONLY over the repo：只在 $EVID 下写比对证据，绝不改 docs/ 里的素材
#    （Rule 30/37：素材是交付物，重放只是证据）。
#  * 每条都用 timeout 兜底；输出不编造——差异原样保留（diff 文件就是证据）。
#  * 沙盒 profile 若已被回收（72h 后 Rule 34 的 GC），依赖它的条目会**如实报错**，
#    脚本把这记成 profile-missing，而不是"复跑通过"。
#
# 用法: scripts/materials/verify-topA.sh [<materials_root>] [<evidence_dir>]
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MAT="${1:-$ROOT/docs/archive/demo/materials}"
EVID="${2:-${GOPI_MATERIALS_EVIDENCE:-$HOME/.gopi/sessions/01a0ed23-1245-7e67-a8c2-cb827c03a4f1}/materials-topA-verify}"
mkdir -p "$EVID"
TSV="$EVID/verify.tsv"
: > "$TSV"
printf 'entry\tsha_recorded\tsha_replay\tbytes_recorded\tbytes_replay\texit_replay\tverdict\n' >> "$TSV"

n_ident=0; n_diff=0; n_missing=0; n_bad=0
for dir in "$MAT"/1[01][0-9]-*; do
  [[ -d "$dir" ]] || continue
  name="$(basename "$dir")"
  in="$dir/01-input.txt"; rec="$dir/02-output.txt"
  if [[ ! -s "$in" || ! -s "$rec" ]]; then
    printf '%s\t-\t-\t0\t0\t-\tmissing-input-or-output\n' "$name" >> "$TSV"
    n_bad=$((n_bad+1)); continue
  fi
  # 01 里写下的 GOPI_HOME（沙盒 profile）——被回收则本条目不可重放，如实报告。
  sb="$(sed -n 's/^GOPI_HOME=\([^ ]*\) *\\$/\1/p' "$in" | head -1)"
  if [[ -n "$sb" && ! -d "$sb/.gopi" ]]; then
    printf '%s\t%s\t-\t%s\t0\t-\tprofile-missing:%s\n' "$name" \
      "$(sha256sum "$rec" | cut -c1-16)" "$(stat -c %s "$rec")" "$sb" >> "$TSV"
    n_missing=$((n_missing+1)); continue
  fi
  # 逐字执行那份记录（它是 bash），取 stdout+stderr 与退出码。
  out="$EVID/$name.replay.txt"
  timeout 180 bash "$in" > "$out" 2>&1
  rc=$?
  printf 'exit=%d\n' "$rc" >> "$out"
  sha_r="$(sha256sum "$rec" | cut -c1-16)"
  sha_p="$(sha256sum "$out" | cut -c1-16)"
  br="$(stat -c %s "$rec")"; bp="$(stat -c %s "$out")"
  if diff -q "$rec" "$out" >/dev/null 2>&1; then
    printf '%s\t%s\t%s\t%s\t%s\t%s\tidentical\n' "$name" "$sha_r" "$sha_p" "$br" "$bp" "$rc" >> "$TSV"
    printf 'replay %-24s identical  exit=%s\n' "$name" "$rc"
    n_ident=$((n_ident+1))
  else
    diff -u "$rec" "$out" > "$EVID/$name.diff" 2>&1
    dl="$(wc -l < "$EVID/$name.diff")"
    printf '%s\t%s\t%s\t%s\t%s\t%s\tdiffers(%s diff lines)\n' "$name" "$sha_r" "$sha_p" "$br" "$bp" "$rc" "$dl" >> "$TSV"
    printf 'replay %-24s DIFFERS  exit=%s  diff=%s\n' "$name" "$rc" "$EVID/$name.diff"
    n_diff=$((n_diff+1))
  fi
done

echo "----"
echo "identical=$n_ident differs=$n_diff profile-missing=$n_missing bad=$n_bad"
echo "evidence: $TSV"
[[ $n_diff -eq 0 && $n_missing -eq 0 && $n_bad -eq 0 ]]
