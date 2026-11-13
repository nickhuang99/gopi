#!/usr/bin/env bash
# demo-memory-loop-figs.sh — 把闭环真跑的输出渲染成配图（真实输出 + 机械标注）。
#
# 形状沿用 docs/archive/demo/materials 的约定：03-raw.png 是 text2img 渲染的同文本图，
# 04-annotated.png 是 gopi annotate 叠上去的框，框的几何由 scripts/materials/render.py
# 量出来并**自证**（框里有墨迹 / 标签没压住证据 / 真有像素被改）。自证不过就不产出。
#
# 处理顺序（前两次实现失败后定下来的形状，见 devdoc/demo-memory-loop.md）：
#   1. 先把「解说 + 各步骤 verbatim 输出」拼成一份 doc；
#   2. `demo-loop-prepad.py` 按 render.py **同一份 wrap()** 折行，在标注行上方补 2 个空行，
#      并同步累计偏移（升序），把结果写到 doc.prepad —— 这一步做完，render.py 自己的
#      腾空逻辑变成空操作，不会再把后面的 needle 行号顶偏；
#   3. render.py 只负责量几何、画框、自证。
#
# 用法: bash scripts/demo-memory-loop-figs.sh <loop-evidence-dir> <out-dir> <render-evidence-dir>
set -uo pipefail

ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
BIN="$ROOT/bin/gopi"
RENDER="$ROOT/scripts/materials/render.py"
PREPAD="$ROOT/scripts/demo-loop-prepad.py"
FONT=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc
LOOP="${1:?usage: bash scripts/demo-memory-loop-figs.sh <loop-evidence-dir> <out-dir> <render-evidence-dir>}"
OUT="${2:?out dir}"
EV="${3:?render evidence dir}"
[[ -x "$BIN" ]] || { echo "FATAL: no binary at $BIN" >&2; exit 1; }
[[ -d "$LOOP" ]] || { echo "FATAL: no loop evidence dir $LOOP" >&2; exit 1; }
mkdir -p "$OUT" "$EV"

SB="$(grep '^sandbox=' "$LOOP/run.env" | cut -d= -f2-)"
COMMIT="$(grep '^measured_commit=' "$LOOP/run.env" | cut -d= -f2-)"

# fig <slug> <title> <assertion> <needle::label>... -- <step-dir>...
fig() {
  local slug="$1"; local title="$2"; local ack="$3"; shift 3
  local specs=() dirs=() needles=()
  local mode=spec a
  for a in "$@"; do
    if [[ "$a" == "--" ]]; then mode=dir; continue; fi
    if [[ "$mode" == spec ]]; then specs+=("$a"); else dirs+=("$a"); fi
  done
  local dir="$OUT/$slug"
  mkdir -p "$dir"
  local doc="$EV/$slug.doc"
  {
    printf '%s\n' "$title"
    printf '\n%s\n' "$ack"
    printf '\nGOPI_HOME=%s\n' "$SB"
    printf 'measured_commit=%s   渲染自证中间产物: %s\n' "$COMMIT" "$EV"
    printf '\n── 02-output.txt（verbatim；每条的最后一行是它自己的 exit code）──\n'
    for d in "${dirs[@]}"; do
      cat "$LOOP/$d/02-output.txt"
      printf '\n'
    done
  } > "$doc"
  # needle::label —— render.py 用单冒号切 `needle:label`，所以这里还原成单冒号，
  # 并断言 needle 里没有冒号（真踩过：`A: 实测纠正` 被切成 `A` + 一段乱标签）。
  local mkargs=() s n l
  for s in "${specs[@]:-}"; do
    [[ -z "$s" ]] && continue
    n="${s%%::*}"; l="${s#*::}"
    if [[ "$n" == *:* ]]; then echo "FATAL: needle 含冒号会被 render.py 切错: $n" >&2; exit 1; fi
    mkargs+=(--mark-match "$n:$l"); needles+=("$n")
  done
  if [[ ${#mkargs[@]} -eq 0 ]]; then echo "FATAL: fig $slug 没有标注规格" >&2; exit 1; fi

  if ! python3 "$PREPAD" "$doc" "${needles[@]}" > "$doc.prepad" 2> "$EV/$slug.prepad.log"; then
    echo "FAIL fig $slug (prepad rc=$?) —— 见 $EV/$slug.prepad.log" >&2
    tail -3 "$EV/$slug.prepad.log" >&2
    return 1
  fi
  python3 "$RENDER" --raw "$dir/03-raw.png" --ann "$dir/04-annotated.png" \
    --font "$FONT" --bin "$BIN" --scratch "$EV/scratch-$slug" \
    --mark-after "02-output.txt" --max-lines 60 \
    --json-out "$EV/$slug.render.json" "${mkargs[@]}" \
    < "$doc.prepad" > "$EV/$slug.render.log" 2>&1
  local rc=$?
  if [[ $rc -ne 0 ]]; then
    echo "FAIL fig $slug (render rc=$rc) —— 见 $EV/$slug.render.log" >&2
    tail -3 "$EV/$slug.render.log" >&2
    return 1
  fi
  cp "$doc" "$dir/02-output.txt"
  {
    printf '# %s\n\n- face: memory\n- feature: 记忆闭环九步\n- measured_commit: %s\n- sandbox_profile: %s\n- how_to_replay: `bash scripts/demo-memory-loop.sh <evidence_dir>` 重跑闭环，再 `bash scripts/demo-memory-loop-figs.sh <loop-dir> docs/archive/demo/loop <render-evidence>` 重画图\n\n## 这张图在指什么\n\n' "$title" "$COMMIT" "$SB"
    printf '%s\n\n' "$ack"
    printf '## 图与文本的机械差别\n\n图与 `02-output.txt` 逐行对应，只有两处**机械差别**，在此写明以免读者核对时以为输出被改过：\n\n'
    printf '1. 被标注的行上方各插入 **2 个空行** —— `gopi tool annotate` 把标签画在框上方（实测标签带 ~39px），不腾空就压住上一行证据；插空行只动版面，证据行一个字节没动（render.py 的自证项 `label_covered_ink` 量了这个，必须为 0 才产出图）。\n'
    printf '2. 输出超过 60 行时截断，图内注明被截断、完整 verbatim 在同目录 `02-output.txt`。\n\n'
    printf '## 图怎么被证明不是"看起来对"\n\n'
    printf '框的几何由 `scripts/materials/render.py` 用 PIL 量出的行距（`pitch`）算出，并在写出 04 之前自证三项，任一不过就 exit 1 不产出：框内原有墨迹 > 0（框没框空行）、标签带压住的证据像素 = 0、annotate 确实改了像素。量出来的数字在 `<render-evidence>/%s.render.json`。\n' "$slug"
  } > "$dir/00-entry.md"
  echo "fig $slug ok"
}

# ④ 投票前后：同一行、同一个查询，账目怎么走
fig 04-vote-conf-vs-effconf \
  '图 ④ 投票前后：同一条记录、同一个查询，算账的那几个数怎么走' \
  '断言：一张 useful 票真的改了账 —— conf 0.85→0.90（反馈是同一条记录上唯一能动 conf 的自动通道），eff_conf 0.91→0.92 是它的派生层（clamp(conf×e^(−Δt/τ)+检索复验奖励)）；搜索行随之多出 fb=0.67(1)。同一行既是知识也是账本，两个数不能互相替代。' \
  'conf=0.85 lesson=recipe::① get（投票前）：只有 conf=0.85，eff_conf 这一栏还不出现' \
  'eff_conf=0.92::② get（投票后）：conf 0.85→0.90，eff_conf=0.92 冒出来' \
  'tokens=1 fb=0.67(1)::③ search（投票后）：搜索行也带上了反馈账 fb=0.67(1)' \
  -- 02-get-correct-baseline 04-feedback-correct-useful 05-get-correct-after-vote 06-search-correct-after-vote

# ⑤→⑨ 出问题那一幕：一条听起来有理、实为假的记忆在库里活着的样子
fig 05-wrong-memory-dies \
  '图 ⑤→⑨ 出问题那一幕：一条假记忆在库里活着的样子，以及它被投票压低的过程' \
  '断言：错的记忆不是"写错就被拦"——它 store 成功（id=2）、search 命中（tokens=1）、以 lesson 身份被当作可用知识；系统只在被真跑推翻时投票把它压低。这句假话本身取自知识库 #6893 的真记录断言（"conf 保持 0.85 不变，变的是 eff_conf"），本 run 在 ④ 用一个 useful 票把它推翻（conf 真的动了）。' \
  '不会变。投票只改::① 假记忆的正文：听起来成套、可检索、带 5W1H 面包屑' \
  'useless=1.00 fb_score=-0.042::② 一张 useless 票记进 append-only 账，步长 −0.10' \
  'conf=0.75 lesson=lesson::③ 投票之后：conf 0.85→0.75，eff_conf=0.77' \
  -- 07-store-wrong 08-search-wrong 09-get-wrong-baseline 10-feedback-wrong-useless 11-get-wrong-after-vote 12-search-wrong-after-vote

# ⑬→⑯ 纠错生效：正文被改写（不是删行），改后的文本可被搜回
fig 13-correction-takes-effect \
  '图 ⑬→⑯ 纠错生效：改的是正文，不是删行；改后的文本能被搜回来' \
  '断言：memory update 是人工通道，它改的是同一条记录的 answer——id 不变、票不变（fb=0.33(1) 还在）、tier 不变，只有正文换了；新正文里的记号 FIXED9-… 能被 search 读回。诚实的一格：旧的 WRONG9-… 记号因为还留在 topic 里，也仍然命中同一条记录——改的是答案，不是主题。' \
  'Memory updated (id=2).::① 纠错回执：update 同一条记录，不是新开一行' \
  '实测纠正：投票确实会改::② get 逐字：正文段落已被改写（id、票、tier 都不变）' \
  'fb=0.33(1)::③ search 读回修正后的正文（正文里的记号可被检索）' \
  -- 13-update-wrong-corrected 14-get-wrong-after-fix 15-search-fixed 16-search-stale-wrong

# ⑰→⑲ 蒸馏端：brew 的输入、判定、落库
fig 17-brew-distill \
  '图 ⑰→⑲ 蒸馏端：brew 从真实会话里判定/蒸馏，--dry-run 与真跑只差一个写入' \
  '断言：brew 的输入是**真实会话 JSONL**（沙盒 sessions/demo 里那份就是本机一条真会话）；收尾一行同时给出吞吐（sessions/chunks/accepted/rejected）与写入（stored/deduped/errors）。--dry-run 与真跑的唯一差别是 stored=0 而台账行数不变，真跑则把蒸馏出的行写进沙盒台账（distilled_qa 与 procedural 两张表都长）。' \
  'rejected=4 stored=0::① --dry-run：三条判据判完，一个字节没写' \
  'stored=6::② 真跑：同一份输入，蒸馏出 6 条并落库' \
  '二进制路径用已构建会话目录::③ 台账里多了 brew 蒸馏出来的行（source=brew）' \
  -- 17-brew-dry-run 18-brew-live 19-ledger-list
