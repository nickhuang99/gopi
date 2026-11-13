#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# gen-flagF-013.sh —「013 已修正条目」的取证与初稿生成器。
#
# 背景：作者发现自己在 203/206 两条里写的注释是**错的**（--model 的失败并不是
# 「一个不存在的模型会被替换成默认值」；206 这一条本身跑挂了），于是**不覆盖原件**，
# 另立 013 把修正与两个实证（dumps 落宿主目录、--db 接受任意路径）一起留证。
#
# 用法: bash scripts/materials/gen-flagF-013.sh <binary> <sandbox> <out_dir>
set -uo pipefail
BIN="${1:?binary}"
SB="${2:?sandbox}"
DIR="${3:?out dir}"
mkdir -p "$DIR"

echo "== 013: dumps 落在宿主 home（隔离的反例）=="
( cd "$SB" && "$BIN" --model zz-no-such-model-xyz --prompt "Reply with exactly: ok" --timeout 25 2>&1 ) \
  | grep -oE '/home/[^ ]*dump\.json' | head -2
echo "== 013: 宿主 dumps 目录当前的条目数 =="
ls /home/nick/.gopi/dumps | wc -l
echo "== 013: 该通道在 CONFIG.md 里的登记行 =="
grep -n 'GOPI_ERROR_DUMP_DIR' /home/nick/workspace/gopi-01a0ed23194577f7bad0962ec576bcb9/CONFIG.md | head -2
echo "== 013: 沙盒 profile 里同名的 dumps 目录 =="
ls -d "$SB/.gopi/dumps" 2>&1 | head -1
echo "OK out=$DIR"
