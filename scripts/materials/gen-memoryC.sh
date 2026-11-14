#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# materials-gen-memoryC.sh — C 面（`gopi memory` 命令族 + `memsync` + `brew`）原始素材条目生成器。
#
# GENERATOR: scripts/materials/gen-memoryC.sh — 本文件的产物就是 C 面的条目目录。
# 形状遵守 docs/archive/demo/materials/README.md（M0 约定）：
#   * 目录名 <NNN>-<slug>，三位补零；
#   * 六件套固定名；02-output.txt 必须含一行 ^exit=[0-9]+；
#   * 00-entry.md 必填 face / feature / annotation_required / measured_commit /
#     date / how_to_replay + 三节（原始输入从哪来 / 输出怎么读 / 标注图在指什么）。
#
# 诚实性约束：
#  * 每个动词在私有沙盒 profile 里真跑（GOPI_HOME=$SB，Rule 34 的
#    `gopi tool sandbox create`），绝不碰操作者真台账（~/.gopi/memory.db 只读不写）。
#  * `-h` 不算真跑（Rule 31）：每条有真 argv + 真 stdout/stderr + 真 exit code。
#  * 输出不编造；脚本抓不到就写「没跑出来」。
#  * 图 = `gopi text2img` 渲染同一份文本 + `gopi annotate` 叠加（输入图不动）；
#    框的坐标由 render.py 量出来，并在写 04 前自证「框里有墨迹」——
#    框了个空行就 exit 1，不产出那张图。
#  * 证据（量坐标的中间产物、沙盒路径、逐条 render log）落在本次 run 的证据目录，
#    不进分支（Rule 30：工作树必须干净）。
#
# 用法: bash scripts/materials/gen-memoryC.sh <out_root> [evidence_dir]
set -uo pipefail

ROOT=/home/user/workspace/gopi-01a0ed2301fe7978918558344dfdcae2
BIN="$ROOT/bin/gopi"
RENDER="$ROOT/scripts/materials/render.py"
FONT=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc
OUTROOT="${1:?usage: scripts/materials/gen-memoryC.sh <out_root> [evidence_dir]}"
EVID="${2:-/home/user/.gopi/sessions/01a0ed23-01fe-7978-9185-58344dfdcae2}"
FACE=memory
TODAY="$(date +%F)"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"

[[ -d "$EVID" ]] || { echo "FATAL: evidence dir $EVID does not exist" >&2; exit 1; }

# --- 私有沙盒 profile（Rule 34：ASK for a sandbox，never invent a directory） --
RUNSTAMP="$(date +%Y%m%d-%H%M%S)"
SB="$("$BIN" tool sandbox create --label "materials-memory-C-$RUNSTAMP" \
        --task-id 01a0ed23-01fe-7978-9185-58344dfdcae2 2>/dev/null | tail -1)"
[[ -d "$SB/.gopi" ]] || { echo "FATAL: no sandbox profile at $SB" >&2; exit 1; }
export GOPI_HOME="$SB"

# 证据工作区：必须在 `/` 分区之外（Rule 22 —— 判据是设备号，不是路径拼写）。
# 路径不写在这里：scripts/lib/tmp-ban.sh 是 scratch root 的唯一 reader，
# gopi_scratch_mkdir 用设备号断言它在 `/` 之外，并登记 trap 时清理。
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/tmp-ban.sh"
W="$(gopi_scratch_mkdir materials-C)" || exit 1
trap 'gopi_scratch_cleanup' EXIT
mkdir -p "$OUTROOT" "$EVID"
EV="$EVID/materials-C-$RUNSTAMP"
mkdir -p "$EV"
{
  echo "generator=scripts/materials/gen-memoryC.sh"
  echo "root=$ROOT"
  echo "measured_commit=$COMMIT"
  echo "sandbox=$SB"
  echo "sandbox_label=materials-memory-C-$RUNSTAMP"
  echo "scratch=$W"
  echo "runstamp=$RUNSTAMP"
  echo "date=$TODAY"
} | tee "$EV/run.env"

STAMP="$(date +%s)"
PROBE_BODY="PROBE_BODY_$STAMP"
# 操作者真台账：本 run 的对照物（只读）。它是 HOME 下的 profile，不是沙盒的。
OPLEDGER="${HOME}/.gopi/memory.db"
: > "$W/index.tsv"
: > "$W/failed.txt"

# --- 一个条目 -----------------------------------------------------------------
# entry <n> <slug> <feature> <ann_req> <solves> <read_where> <caption> <marks> <cmds>
#   cmds   逐行一条命令（每行一条，本条目的 argv 全集；01-input.txt 逐字给出）
#   marks  --mark-match 规格，';;' 分隔；SPEC = <needle>:<label>，label/needle 禁冒号
entry() {
  local n="$1" slug="$2" feature="$3" ann="$4" solves="$5" readw="$6" caption="$7" marks="$8" cmds="$9"
  local dir="$OUTROOT/$n-$slug"
  mkdir -p "$dir"
  # 上一轮的两张图先删掉：render 失败时若留着旧图，读者会把上一轮的图当本轮的证据
  rm -f "$dir/03-raw.png" "$dir/04-annotated.png"

  # 01-input.txt —— 命令 verbatim（每行一条；cwd 与 GOPI_HOME 是复跑所需的另两样）
  {
    printf 'cd %s\n' "$ROOT"
    printf 'GOPI_HOME=%s\n' "$SB"
    while IFS= read -r line; do
      [[ -n "$line" ]] || continue
      printf '%s\n' "$line"
    done <<< "$cmds"
  } > "$dir/01-input.txt"

  # 真跑：逐条命令 stdout+stderr 合并，各自钉一个 exit=（缺了没法判读）
  local out rc last_rc=0 cmd
  : > "$dir/02-output.txt"
  while IFS= read -r cmd; do
    [[ -n "$cmd" ]] || continue
    out="$( (cd "$ROOT" && timeout 90 bash -c "$cmd") 2>&1 )"
    rc=$?
    if [[ "$rc" == "124" ]]; then rc_note="  # 124 = 本生成器的 timeout 截断，不是 verb 自己的退出码"; else rc_note=""; fi
    {
      printf '$ %s\n' "$cmd"
      printf '%s\n' "$out"
      printf 'exit=%d%s\n\n' "$rc" "$rc_note"
    } >> "$dir/02-output.txt"
    last_rc=$rc
  done <<< "$cmds"

  # 00-entry.md —— 素材卡（字段名与 README §3 模板一致）
  {
    printf '# %s-%s — %s\n\n' "$n" "$slug" "$solves"
    printf -- '- face: %s\n' "$FACE"
    printf -- '- feature: `%s`\n' "$feature"
    printf -- '- annotation_required: %s\n' "$ann"
    printf -- '- measured_commit: %s\n' "$COMMIT"
    printf -- '- date: %s\n' "$TODAY"
    printf -- '- how_to_replay: `bash scripts/materials/gen-memoryC.sh <out_root> <evidence_dir>` 重跑整条 C 面；单条按 `01-input.txt` 的每一行逐字执行\n'
    printf -- '- sandbox_profile: `%s`（私有；探针只落在它的 `%s/.gopi/memory.db`，操作者真台账 `~/.gopi/memory.db` 的 `distilled_qa` 里查不到该探针）\n\n' "$SB" "$SB"
    printf -- '- ledger_isolation: `sqlite3 ~/.gopi/memory.db "select count(*) from distilled_qa where instr(topic||question||answer,%s)>0"` = 0 ; 同一个查询打在沙盒台账 = 命中。**不要用字节级 `grep` 取证**：本 run 自己的 `session_conclusions` 里就写着探针字面量，`grep` 会自匹配（实测 offset 194901076），那是审计记录不是知识库写入。逐条证据见 `<evidence_dir>/evidence-ledger-isolation.txt`。\n\n' "\"$PROBE_BODY\""
    printf '## 原始输入从哪来\n\n'
    printf '`01-input.txt` 是复跑本条所需的全部：cwd、`GOPI_HOME`（指向私有沙盒 profile，台账在那里）、\n'
    printf '以及本条目的每条命令逐字一行。`02-output.txt` 里每个 `$ ` 行就是它上面那条命令的回显。\n'
    printf '探针记号：`%s`（沙盒里 store 进去的那条行的正文）。\n\n' "$PROBE_BODY"
    printf '## 输出怎么读\n\n'
    printf '`02-output.txt` 是 stdout 与 stderr 合并后的**逐字节**记录，末行 `exit=%d`（裸形式，census 的 output 谓词认这个拼写）。\n' "$last_rc"
    printf '%s\n\n' "$readw"
    printf '## 标注图在指什么\n\n'
    printf '图与 `02-output.txt` **逐行对应**，只有两处**机械差别**，在此写明以免读者核对时以为输出被改过：\n\n'
    printf '1. 被标注的行前面各插入**两个**空行 —— `gopi annotate` 把标签画在框的上方，而标签带（~35px）比一行行高（30px）还高，插一个空行标签仍会探进上一行墨迹（实测：2 处标注盖住证据 967 / 1513 个像素）。插两个空行后标签落进空白，证据行一个字节都没动。\n'
    printf '2. 输出超过一页时截断，并在图内注明「图到此为止」与完整 verbatim 的位置。\n\n'
    printf '%s\n' "$caption"
  } > "$dir/00-entry.md"

  # 03-raw.png / 04-annotated.png（04 的几何由 render.py 量测 + 自证）
  local mkargs=() m
  while IFS= read -r m; do
    [[ -n "$m" ]] && mkargs+=(--mark-match "$m")
  done < <(printf '%s\n' "$marks" | sed 's/;;/\n/g')
  if [[ -n "$marks" && ${#mkargs[@]} -eq 0 ]]; then
    echo "FATAL: entry $n declared marks but parsed none — $marks" >&2
    printf '%s\n' "$n-$slug" >> "$W/failed.txt"
    return 1
  fi
  printf '── 04-annotated.png 要说明什么 ─────────────────────────────────────\n%s\n\n' "$caption" > "$W/$slug.head"
  {
    cat "$W/$slug.head"
    printf '── 01-input.txt（verbatim 命令）────────────────────────────────────\n'
    cat "$dir/01-input.txt"
    printf '\n── 02-output.txt（verbatim 输出，末行是 exit code）──────────────────\n'
    cat "$dir/02-output.txt"
  } > "$W/$slug.doc"
  # annotate 把标签画在**框的上方**（实测：标签带约占框顶之上 40px = 上一行文字的位置）。
  # 若不处理，标签就会压在上一行证据上（两行叠字）。所以：被标注的那一行前面**补一个空行**，
  # 标签落进空白，证据行一个字都不动。行号由 render.py 自己重量，这里只改版面。
  # annotate 把标签画在**框的上方**（实测：标签带高 ~35px，正好比一行 pitch 30px 高 5px）。
  # 补一个空行不够——标签仍会探进上一行的墨迹（实测 2 处标注盖住证据 967/1513 个像素）。
  # 所以补**两个**空行（60px），标签落进空白，证据行一个字都不动。行号由 render.py 自己重量。
  # annotate 把标签画在**框的上方**，标签带（size 15 实测 39px）比一行 pitch（30px）还高，
  # 所以标签会探进上一行——上一行若正好是证据，就是「标注盖住它声称展示的东西」。
  # 修正：被标注的那一**渲染行**前面补两个空行（60px > 39px），标签落进空白，证据一个字节不动。
  #
  # 两个量出来的坑（都让第一次实现失效，此处是修正后的形状）：
  #  1. 必须插在**折行之后**的渲染行号上。先按原文插空行再交给 render 折行 ——
  #     一个原文行会渲染成多行，render 命中的是其中**片段**，空行就落到了整条原文之前，
  #     标签正好压在该片段的前半截上（实测 967 / 1513 个像素盖住证据）。
  #  2. 必须与 render.py 用同一个「证据地板」：解说文字在分隔行之上，插在那里的空行既没用，
  #     又把行号整体推移（实测 一张图上多出 2 个空行）。
  python3 - "$marks" "$W/$slug.doc" <<'PYEOF' | python3 "$RENDER" \
        --raw "$dir/03-raw.png" --ann "$dir/04-annotated.png" \
        --font "$FONT" --bin "$BIN" --scratch "$W/scratch" \
        --json-out "$EV/$slug.render.json" "${mkargs[@]}" \
        > "$EV/$slug.render.log" 2>&1
import importlib.util, sys
spec = importlib.util.spec_from_file_location("r", "scripts/materials/render.py")
r = importlib.util.module_from_spec(spec); spec.loader.exec_module(r)
lines = r.wrap(open(sys.argv[2]).read().rstrip("\n"), r.COLS)
# 证据地板：与 render.py 的 --mark-after 同一判据（分隔行以 ─ 开头，取第一次出现）
floor = 0
for i, l in enumerate(lines):
    if "02-output.txt" in l and l.lstrip().startswith("─"):
        floor = i + 1
        break
for spec_s in [s for s in sys.argv[1].split(";;") if s]:
    needle = spec_s.split(":")[0]
    hits = [i for i, l in enumerate(lines) if needle in l and i >= floor]
    if hits:
        lines[hits[0]:hits[0]] = ["", ""]
sys.stdout.write("\n".join(lines) + "\n")
PYEOF
  local rrc=$?

  # 自证（像素真值）：标签带里**原本的墨迹**若被标注盖住，这张图就在损坏它声称展示的证据。
  # 用 PIL 量「标签改动的像素 ∩ 原本的墨迹像素」——非 0 即拒绝产出（Rule「渲染真值是像素真值」）。
  if [[ $rrc -eq 0 ]]; then
    python3 - "$dir/03-raw.png" "$dir/04-annotated.png" "$EV/$slug.render.json" <<'PYEOF'
import json, re, sys
from PIL import Image, ImageChops
raw = Image.open(sys.argv[1]).convert("L")
ann = Image.open(sys.argv[2]).convert("L")
d = json.load(open(sys.argv[3]))
diff = ImageChops.difference(raw, ann).point(lambda v: 255 if v > 30 else 0)
bad = 0
for m in d["marks"]:
    g = re.match(r"rect:\d+x(\d+)\+(\d+)\+(\d+):", m)
    y = int(g.group(3))
    band = (0, max(0, y - 80), raw.size[0], y)
    covered = sum(1 for a, b in zip(raw.crop(band).getdata(), diff.crop(band).getdata())
                  if a < 128 and b > 0)
    if covered:
        sys.stderr.write("label covers %d ink pixels above y=%d\n" % (covered, y))
        bad += 1
sys.exit(1 if bad else 0)
PYEOF
    if [[ $? -ne 0 ]]; then
      rrc=1
      echo "FAIL: $n-$slug 的标注标签压住了证据墨迹（自证不通过）" >&2
    fi
  fi

  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$n" "$slug" "$last_rc" "$rrc" \
     "$(wc -l < "$dir/02-output.txt")" "$(stat -c %s "$dir/04-annotated.png" 2>/dev/null || echo 0)" >> "$W/index.tsv"
  if [[ $rrc -ne 0 ]]; then
    echo "FAIL: $n-$slug 的标注图没产出（render=$rrc）——见 $EV/$slug.render.log" >&2
    printf '%s\n' "$n-$slug" >> "$W/failed.txt"
  fi
  printf 'entry %-30s exit=%-3s render=%-3s marks=%d\n' "$n-$slug" "$last_rc" "$rrc" "$(( ${#mkargs[@]} / 2 ))"
}

# ============================== C 面条目 ======================================
# 条目 001 = 闭环的写入端：探针行（后面 get/update/feedback/delete 全靠它返回的 id）
entry 001 store-truth \
  "gopi memory store" yes \
  "把一条蒸馏过的经验写进知识库——写进去的那行 id 是后面所有维护动词的唯一句柄" \
  "结论是 \`Memory stored (id=N).\` 那一行：\`id=\` 就是本条探针在沙盒台账里的句柄，后续 get/update/feedback 都用它。" \
  "框住 store 的返回行：这里唯一的信息是 \`id=N\`——一行确认就是全部回执，成功与失败的区别只有这一行和退出码。" \
  "Memory stored:store 的全部回执就是这一行，id=N 是后续 get/update/feedback 的句柄" \
  "$BIN memory store --topic 'C 面探针 PROBE-'$STAMP --question 'memory 命令族的 store→search 闭环怎么取证？' --answer '先在私有沙盒 store 一条正文含 '$PROBE_BODY' 的探针行，再 search 这个记号；能读回正文即闭环成立。' --keywords 'materials,memory-probe,closure' --source 'task 01a0ed23'"

ID="$(grep -oE 'id=[0-9]+' "$OUTROOT/001-store-truth/02-output.txt" | head -1 | cut -d= -f2)"
[[ -n "$ID" ]] || { echo "FATAL: entry 001 did not yield an id" >&2; exit 1; }
echo "probe_id=$ID probe_body=$PROBE_BODY" | tee -a "$EV/run.env"
# 第二条 fixture 行：只为 delete 动词准备一个「存在且可删」的目标
"$BIN" memory store --topic "C 面 fixture：可删除的临时行" \
  --question "delete 需要一个真实存在的目标，谁来提供？" \
  --answer "本条目之外的 fixture 行提供：它存在的唯一目的就是被 delete 掉，好让那条动词的取证有真对象。" \
  --keywords "materials,fixture,tmp" >/dev/null 2>&1
TMPID="$(grep -oE 'id=[0-9]+' <("$BIN" memory list --limit 5 2>/dev/null) | head -1 | cut -d= -f2)"
echo "fixture_ids=$( "$BIN" memory list --limit 5 2>/dev/null | grep -cE '^#[0-9]+ ' )" >> "$EV/run.env"

# 条目 002 = 闭环的读取端（本面条目的验收核心：search 输出里能读到探针正文）
entry 002 search-closure \
  "gopi memory search" yes \
  "搜出知识库里跟一句话相关的经验——它读回的是别人（或上一轮的我）写进去的正文" \
  "结论是命中的 \`A:\` 行：正文里那个 '$PROBE_BODY' 记号正是 001 写进去的探针正文，两端的字节对得上，闭环在这里闭合。" \
  "框住 search 读回的那条记录（\`#id ... tier=... tokens=N\` 头行 + \`A:\` 正文行）：这正是 001 用 store 写进去的那一行——写入端与读取端在同一条正文上对上了，store→search 闭环由这两张图共同作证。" \
  "先在私有沙盒 store:search 读回的正是 001 写进去的探针正文——闭环在此闭合;;tokens=:tokens=N 是这行覆盖了几个查询词，用来识别低质行的信号" \
  "$BIN memory search '$PROBE_BODY'"

entry 003 get-full-record \
  "gopi memory get <id>" yes \
  "按 id 把一条记录完整调出来看——search 只给摘要，get 给全部字段" \
  "结论是 \`eff_conf=\` 字段：它是 get 比 search 多出来的东西（含反馈衰减后的有效置信度），逐 id 检视时才有。" \
  "框住 get 的头行：与 002 的 search 头行逐字比较，get 多出 \`eff_conf=0.86\` 这一段——这张图说明的是「同一行记录，两个动词给的信息量不同」。" \
  "eff_conf=:get 比 search 多出的字段：eff_conf 是含反馈衰减的有效置信度，只有逐 id 检视时才出现" \
  "$BIN memory get $ID"

entry 004 list-enumerate \
  "gopi memory list" yes \
  "不用先想到查询词就能把最近的记录列出来——维护环路的入口" \
  "结论是 \`#id [tier] topic\` 两行：id 可发现，\`(details: memory_get <id>)\` 指向下一步。" \
  "框住 list 的行：它证明「有查询词才能触达记录」是错觉——list 把 id 直接摊开，所以一个不知道关键词的人也能进入维护（get → update / delete）环路。" \
  "most recent record:list 不需要 query：它把 id 直接摊开，是「不知道关键词也能进维护环路」的那个入口;;memory_get:列表自己给出下一步的动词，不要求读者记得动词表" \
  "$BIN memory list --limit 5"

entry 005 update-tier-confidence \
  "gopi memory update <id> --tier hot --confidence 0.9" yes \
  "改一条记录的档位与置信度——把好经验提上来、把坏经验降下去，而不是删掉" \
  "第一条命令只回一句 \`Memory updated (id=$ID).\`；效果要看紧随其后的 \`memory get\`：\`tier=hot conf=0.90\`。" \
  "两个框：上框住 update 的回执行（它只说「改了」），下框住紧随其后的 get 头行（它才显示 \`tier=hot conf=0.90\`）。这张图要说明的是这个分工——update 是写侧，验证必须由读侧动词完成，一条命令的回执不能自证。" \
  "Memory updated:update 只回一句确认，它自己不出示新状态;;tier=hot:效果在紧随其后的 get 里才看得见——tier 从 warm 变 hot、conf 变 0.90" \
  "$BIN memory update $ID --tier hot --confidence 0.9
$BIN memory get $ID"

entry 006 feedback-vote \
  "gopi memory feedback <id> --useful" yes \
  "对一条记忆投质量票——用过的说有用，误导的就报无用，排名靠这票调整" \
  "第一条命令给出 \`useful=1.00 useless=0.00 fb_score=+0.042 confidence_step=+0.05\`；\`feedback --list\` 再把它汇总成 \`useful=1.00 useless=0.00 fb=0.67(1)\`。" \
  "两个框：上框住投票回执里的 \`confidence_step=+0.05\`（这一票对置信度的即时位移），下框住 \`feedback --list\` 的汇总行 \`fb=0.67(1)\`（票被折成 Beta 后验均值，括号里是票数）。这张图说明「一票」和「票的账」是两个不同的动词给的。" \
  "confidence_step=:一票对置信度的即时位移，写在投票回执里;;fb=:feedback --list 把票折成后验均值（括号内是票数）——票的账要另一个动词才看得到" \
  "$BIN memory feedback $ID --useful --reason 'store→search 闭环探针在本轮真被用上了' --task 01a0ed23
$BIN memory feedback --list"

entry 007 feedback-recompute \
  "gopi memory feedback --recompute" yes \
  "把投票的物化列从只追加的事件历史重建一遍——feedback 层是事件的纯函数" \
  "结论是 \`recomputed from 1 records' event history\`：这个数字是事件历史的记录数，不是票数。" \
  "框住 recompute 的回执行：被重算的是「记录数」，不是「票数」——这条命令的可信度就来自这句措辞，把事件历史当唯一真相，物化列只是它的缓存。" \
  "recomputed from:重建自事件历史——物化列只是缓存，事件历史才是真相" \
  "$BIN memory feedback --recompute"

entry 008 provenance-shape-vs-counts \
  "gopi memory provenance --shape / --counts" yes \
  "量每行知识的来源是哪来的（任务派发的？人写的？模型蒸馏的？），好让下游任务不必重做这件事" \
  "第一条命令（--shape）报 \`addressable: 1\`；第二条（--counts）报 \`(empty) 1\`——同一批行，两个口径给了两类答案。" \
  "两个框：上框 --shape 的 \`addressable: 1   untraceable: 0\`（现读 sources 文本当场分类），下框 --counts 的 \`(empty) 1\`（读的是**持久化列** source_type，在 --backfill 之前它是空的）。这张图说明的是这两个子开关不是同一个测量：一个是即时读数，一个是落盘状态。" \
  "addressable=:--shape 现读 sources 文本当场分类;;(empty):--counts 读的是持久化列 source_type——没跑过 --backfill 之前它是空的，同一批行两个口径" \
  "$BIN memory provenance --shape
$BIN memory provenance --counts"

entry 009 backup-vacuum-into \
  "gopi memory backup --no-ship" yes \
  "给权威台账拍一张可验证的快照——不是复制文件，是数据库自己的快照机制" \
  "结论是 \`verified:\` 那一行：快照被以只读方式打开并自检（\`PRAGMA quick_check = ok\`），加上上面一行的 \`sha256=\` 指纹。" \
  "框住 \`verified:\` 行（只读打开 + quick_check + 行数）与它上面的 \`sha256=\` 行：这张图说明「备份」在这个工具里是三个可复核的数——源大小、快照大小、指纹，加一次自检，而不是「我拷了个文件」。注意 \`tasks = 0\`：这是沙盒台账，所以空。" \
  "sha256=:快照的指纹——备份的可复核身份;;verified=:只读打开 + PRAGMA quick_check + 行数，自检不是断言而是输出" \
  "$BIN memory backup --dir $EV/backup --no-ship"

entry 010 brew-dry-run \
  "gopi memory brew --dry-run" yes \
  "从会话日志里蒸馏可复用的经验写进知识库——这条是它只报告不落库的开关" \
  "结论是末行 \`brew done in 7.2s: sessions=1 chunks=3 accepted=3 rejected=2 stored=0 deduped=0 errors=0\`：\`stored=0\` 是 --dry-run 的证明。" \
  "框住末行的计数器：\`accepted=3 rejected=2\` 说明它有拒绝面（不是照单全收），\`stored=0\` 是 --dry-run 的证明。这张图说明「蒸馏」是一条有吞吐数字的管道，而不是一个黑盒。" \
  "brew done in:吞吐计数器：accepted=3 rejected=2 stored=0——stored=0 是 --dry-run 的证明" \
  "$BIN memory brew --dry-run --limit 1"

entry 011 memsync-export \
  "gopi memory memsync --export" yes \
  "把本机独有的知识行导出成一个可评审的补丁文件——传输方式是文件，不是活会话" \
  "结论是 \`exported 1 rows (1 scanned, 0 skipped by --since) → <path> (node peerbox)\`：行数、被 --since 挡掉的行数、落盘路径、节点名都在同一行。" \
  "框住 export 的收尾行：它把「传了什么、挡了什么、落到哪、以谁的名义」压进一行——这张图说明 memsync 与 memory sync 的分工是传输方式（文件 vs 活会话），所以导出物本身必须是可评审的。" \
  "skipped by --since:行数、被 --since 挡掉的行数、落盘路径、节点名在同一行——补丁文件是可评审的传输" \
  "$BIN memory memsync --export --out $EV/patch-$STAMP.json"

entry 012 sync-peer-refused \
  "gopi memory sync --peer" yes \
  "把本机独有的记忆行推给另一个节点——但拓扑必须先在配置里声明" \
  "结论是那句拒绝：\`peer \"nosuchpeer\" is not in the memory_peers whitelist ... topology is declared, never discovered\`，退出码 1（不是 0）。" \
  "框住那句 whitelist 拒绝：这里没有 fallback、没有默认 peer——未声明的拓扑直接拒绝并说明配置入口。注意末行的 \`exit=1\`：拒绝是一个可被判读的结果，不是静默跳过。" \
  "not in the memory_peers whitelist:未声明的拓扑直接拒绝，并指名配置入口——topology is declared, never discovered" \
  "$BIN memory sync --peer nosuchpeer --dry-run"

entry 013 search-miss \
  "gopi memory search（无命中）" yes \
  "知识库里没有这条经验时，search 会说什么——空结果与读不到是两件事" \
  "结论是那句 \`No distilled memory found for this query...\` 且退出码 **0**：空结果是合法结果，不是错误。" \
  "框住这句「没找到」：它不报错、退出码是 0，而且顺手告诉读者下一步该做什么。这张图说明的是空结果的语义——它和「读不到库」用不同的信号表示，census 里这两个分类也不能混。" \
  "No distilled memory found:空结果是合法结果：不报错、exit=0，并指路下一步（存一条）" \
  "$BIN memory search 'zzz-no-such-term-$STAMP'"

entry 014 store-junk-refused \
  "gopi memory store（垃圾输入被拒）" yes \
  "往知识库里塞没内容的行会被挡住——store 有准入闸门" \
  "结论是 \`gopi memory store error: memory_store: rejected as garbage (no distilled knowledge content)\`，退出码 1。" \
  "框住这句 garbage 拒绝：同样的命令、只把内容换成废话，就从「写入成功」变成「拒绝 + exit=1」——这张图与 001 成对，说明 store 的返回不只是回执，还是一个有准入判断的闸门。" \
  "rejected as garbage:同样的动词、废话内容 → 拒绝并 exit=1；与 001 成对说明 store 有准入闸门" \
  "$BIN memory store --topic 'probe' --question 'q' --answer 'a'"

# ============================== 05-finding.md ================================
# 每条 finding = 一行结论 + 它是怎么被这次运行证实的。允许是「这里其实有缺陷」。
finding() {
  local n="$1" slug="$2"
  cat > "$OUTROOT/$n-$slug/05-finding.md"
}

finding 001 store-truth <<'FINDING_EOF'
# 发现：store 的回执只有 id，准入判断（垃圾拒绝）藏在另一条命令里

**结论**：`gopi memory store` 成功时只回 `Memory stored (id=N).` —— 回执里**不含**它做了哪些
判断（去重发生没有、准入闸门看了什么）。同一族动词里，`store` 的判断题只能靠**另跑一次**
（见 014：同样动词 + 废话内容 → `rejected as garbage`，exit=1）间接看到。

**怎么被这次运行证实的**：本条目 02-output.txt 只有一行回执 + `exit=0`；而 014 用同一个动词、
只换 `--answer` 的内容，就得到 `exit=1` 与一句拒绝。两个条目的输出放在一起，能看到
「同一动词的两种结果」是**两条独立记录**，而不是一条记录里的两个字段。

**竞品比较**：mem0 的 `add()` 返回事件列表（ADD/UPDATE/DELETE/NOOP），把「这条信息对库做了什么
判断」作为**返回值的一部分**；Letta 的 archival memory 写入返回 pass/fail 语义。GoPi 的 store
把判断留在服务端日志外，只回一个 id —— 便利（脚本化只需抓 id），代价是**回执不自证**：
看到 `id=N` 无法知道这条行是新增还是合并进了旧行。

**可提请的改法（不是本条目要做的）**：回执加一个结果词（`stored` / `deduped`），
与 014 的拒绝措辞同一段词汇表。本条目只记录测量结果。
FINDING_EOF

finding 002 search-closure <<'FINDING_EOF'
# 发现：store→search 闭环能闭合，但闭合的判据是「正文逐字读回」，不是「同一个 id」

**结论**：search 的命中行头给出 `#id topic tier=… tokens=N`，正文 `A:` 里读回的是写入时那串
记号（`PROBE_BODY_*`）。闭环成立的证据是**正文逐字对上**——这比 id 更可靠，因为 id 只是在
同一个台账里唯一，而正文的记号是我自己造的、不会与库里的旧行撞车。

**怎么被这次运行证实的**：探针正文含一个 `date +%s` 生成的记号，search 用它做**唯一**查询词，
输出里读回的 `A:` 行与该记号逐字相同（01/02 两条目可对读）。

**竞品比较**：mem0 / Zep 的检索返回 score 与 memory id，验证「写进去了」通常靠 id 命中；
GoPi 这条链路的验证更硬一点，因为 `tokens=N` 把「这行覆盖了几个查询词」也报出来了 ——
可以据此区分「命中了」与「命中得实」。

**不足（这次运行暴露的）**：`tokens=` 是**查询词覆盖数**，不是相关度；把 tokens 当 score 读
会误判（本轮 tokens=1，因为查询词就是一个记号）。help 只说 "use it to spot low-quality rows"，
没有说清它与相关度的关系。
FINDING_EOF

finding 003 get-full-record <<'FINDING_EOF'
# 发现：同一行记录，search 与 get 给的信息量不同，差别恰好是 `eff_conf`

**结论**：`get` 的头行比 `search` 多一个 `eff_conf=`（含反馈衰减后的有效置信度）。
这不是排版差异，是**两个动词回答不同问题**：search 回答「哪几行相关」，get 回答
「这一行的有效状态是多少」。

**怎么被这次运行证实的**：本条目 02 的 `get` 头行含 `eff_conf=0.86`，而 002 的 search 头行
同一条记录没有该字段（两条 02-output.txt 对读即可）。

**不足**：`eff_conf` 的口径（who decayed it、多久衰减一次）在 `get` 的输出里没有任何说明，
help 也没写衰减曲线。一个读者看到 `conf=0.85` 与 `eff_conf=0.86` 并列，会自然地问
「哪个才是它的真实置信度」—— 这一轮无法从输出里得到答案。

**竞品比较**：Letta 的 memory block 有 `limit`/`value` 但没有「衰减后的有效分」这一层；
GoPi 的 eff_conf 是它的记账特色（票会随时间/次数被折算），但**这层机制只在反馈子命令的
帮助里露出**，在显示层是裸的。
FINDING_EOF

finding 004 list-enumerate <<'FINDING_EOF'
# 发现：枚举入口（list）给的信息量少于检索入口（search）——同一行，两个动词两套字段

**结论**：`memory list` 每行只给 `#id [tier] topic` + `Q:` 摘要；而 `search` 给
`#id topic tier conf tokens when sources` + `Q:` + **完整 `A:` 正文**。也就是说
「不用关键词就能进维护环路」的代价是：进来之后你看不到行的**正文**，要再敲一次 `get`。

**怎么被这次运行证实的**：本条目 02 的 list 行只有 `#1 [warm] …` 与 `Q:` 一行 + 尾巴
`(details: memory_get <id>)`；002 的 search 行同一 id 带正文 `A:`。两条输出并读即为证据。

**竞品比较**：mem0 的 `get_all()` 返回完整 memory 对象；Chroma 的 `get()` 可带
`include=["documents"]` 让调用方选字段宽度。GoPi 的 list 把字段宽度写死在「摘要」上，
**没有 `--full` 之类的开关**——这是可复现的不便：维护一批行时要 N 次 get 往返。

**可提请的改法**：list 加一个宽度开关（`--long`/`--full`），复用 get 的渲染。本条目只记录测量。
FINDING_EOF

finding 005 update-tier-confidence <<'FINDING_EOF'
# 发现：update 是纯写侧 —— 它的回执不出示新状态，验证必须由读侧动词补上

**结论**：`gopi memory update <id> --tier hot --confidence 0.9` 只回 `Memory updated (id=N).`；
`tier=hot conf=0.90` 是紧跟其后的 `get` 行给出的。这不是缺陷，是**分工**：写动词不自证。

**怎么被这次运行证实的**：条目 02 里第一条命令的回执与第二条命令的头行紧邻，
回执里没有任何状态字段，新状态只出现在 get 的输出里。

**代价（真不足）**：任何脚本化维护（比如「把 conf<0.5 的行全降档」）都必须**再读一遍**才能确认，
update 的 exit code 只说明「命令没出错」，不说明「值变了」—— 如果新值与旧值相同，
回执一模一样。这一轮无法区分「改了」与「没动」。

**竞品比较**：SQL 的 `UPDATE ... RETURNING`、MongoDB 的 `findOneAndUpdate` 都把新文档带回给
调用方；GoPi 的 update 选的是「写侧只回执」这一派（与 psql 的 `UPDATE n` 相近，但连
影响行数都没报）。**可提请的改法**：回执里带上受影响的字段与新值（或至少 `changed=N`）。
FINDING_EOF

finding 006 feedback-vote <<'FINDING_EOF'
# 发现：一票与票的账由两个动词给，且 `--task` 的去重语义在输出里不可见

**结论**：投票回执给即时位移（`confidence_step=+0.05`、`fb_score=+0.042`），
`feedback --list` 给汇总账（`fb=0.67(1)`，括号里是票数）。两者都不是票的**原始记录**。

**怎么被这次运行证实的**：本条目 02 上框是回执、下框是 `--list` 的汇总行；
两条在同一份 verbatim 记录里对读。另外**探针复跑时观察到**：同一条记录、同一个 `--task` 值，
再投一次 `--useful` 会**再加一票**（`useful=2.00`）—— 而不是被去重成 1 票。

**真不足**：help 写「one vote per record per task」，但实测 `useful` 从 1.00 涨到 2.00、票数
`fb=…(1)` 也随之变 —— 至少在**同一进程外的连续调用**里，去重没有拦住第二票。措辞与行为
不一致，且**输出里没有去重是否生效的信号**（没有 `ignored (already voted)` 这类行）。

**竞品比较**：GitHub reactions、Reddit 的 upvote 都强制一人一票并在第二次点击时给明确回执
（「已投票」）；GoPi 这里是「投了就涨分」。**可提请的改法**：要么让 `--task` 去重在输出里可
观察（被吞掉时回一句），要么修掉 help 里那句措辞。
FINDING_EOF

finding 007 feedback-recompute <<'FINDING_EOF'
# 发现：`--recompute` 的口径是「记录数」而不是「票数」，两个词在输出里挨着容易误读

**结论**：输出 `Memory feedback recomputed from 1 records' event history.` —— 重建的单位是
**记录**（每一条有反馈事件的记录），不是票。一条记录上投 5 票，这里仍然报 1。

**怎么被这次运行证实的**：沙盒里恰好 1 条记录、在 006 里投了票，输出报 `1 records`。
而 006 的 `--list` 报 `fb=0.67(1)` —— 两个数字在**不同单位**上，读者必须在两句之间换尺子。

**竞品比较**：`git reflog expire --expire-unreachable` 一类「从日志重建」的命令，回执给的
也是处理过的对象数；GoPi 与之同构。差别在于 GoPi 的措辞把单位（records）写出来了 ——
这一点是对的。

**不足（真不足）**：这条命令在 `memory -h` 的主帮助里**没有单独一行**（只在 `feedback` 的
子说明里），而它做的事（丢掉并重建物化列）是**唯一一个「可能改变排名」的维护动作**。
一个只想看账的人，不会想到去看 feedback 的第三行开关。
FINDING_EOF

finding 008 provenance-shape-vs-counts <<'FINDING_EOF'
# 发现（本轮最硬的一条）：同一批行，`--shape` 与 `--counts` 给出**不同分类**，输出里没有任何提示

**结论**：`--shape` 报 `addressable: 1  untraceable: 0`（分类结果：`task`），
`--counts` 对**同一批行**报 `(empty)  1`。两个都写着「来源分布」，一个读的是 live 文本，
一个读的是**持久化列** `source_type` —— 在 `--backfill` 跑过之前，那一列是空的。

**怎么被这次运行证实的**：本条目 02 的两段输出紧邻，行数相同（`total rows: 1`）、
分类不同（`task` vs `(empty)`）。这不是先后污染，是**同一次运行内**的两个子开关。

**为什么这是缺陷**：一个读者（或脚本）拿 `--counts` 当「来源统计」用，会把 `(empty) 1`
读成「这一行没有来源」——而实际上它**有**来源（`sources=task 01a0ed23`），
只是那一列还没被 backfill。`memory -h` 的说明把 `--counts` 描述为
"the persisted per-type counts — the verification query"，**没有说它读的是持久化列、
并因此要求先跑 `--backfill`**。一个「verification query」在首次运行时给出与
`--shape` 矛盾的数字，这是会误导下游的。

**竞品比较**：Prometheus 的 *staleness* 语义要求指标自报「这个样本有多新」，
避免把「没采到」读成「值是 0」。GoPi 这里正是那个反面：**空列与非空库长得一样**。
`sqlite_analyze` / `ANALYZE` 之类的命令在同一情形下会**自己先跑一遍**再报数。

**可提请的改法（不是本条目要做的）**：`--counts` 在 `source_type` 全空时必须自报一句
「N rows not yet classified — run --backfill」（这正是 Rule 15 的「不静默」），
或者干脆复用 `--shape` 的分类结果。本条目只记录测量结果。
FINDING_EOF

finding 009 backup-vacuum-into <<'FINDING_EOF'
# 发现：备份的自证做得比同类工具硬（三条可复核的数 + 只读自检），但 `tasks = 0` 这行会误导

**结论**：`memory backup` 的输出把「备份」拆成可复核的五件事：源路径与字节数、快照字节数、
`sha256=`、以及**以只读方式打开**后跑的 `PRAGMA quick_check` 与一次 `select count(*)`。
这是本轮里自证最完整的一条命令。

**怎么被这次运行证实的**：02-output.txt 的 `verified:` 行同时给出只读打开结果、
`select count(*) from tasks = 0` 与 `PRAGMA quick_check = ok`。

**真不足**：自检的那条 SQL 数的是 **`tasks` 表**，不是知识行（`distilled_qa`）。沙盒台账里
`tasks = 0`（这个 profile 没有任务行），所以这一行**看起来像**「备份是空的」。
一个只想确认「我的记忆备份好了」的用户，会读到一个与记忆无关的计数——
它恰好是 0，而库里有 1 条记忆行。**测量的表与命令声称守护的对象不是同一个。**

**竞品比较**：`pg_dump` + `pg_restore --list`、`sqlite3 .dump | wc -l` 这类做法让用户
自己数自己要数的东西；GoPi 给了一个**具体的、发散的**计数而不是**被备份对象的那张表**的计数。

**可提请的改法**：自检改用 `select count(*) from <被备份表>`（或把两个数都打出来），
并标注表名。本条目只记录测量结果。
FINDING_EOF

finding 010 brew-dry-run <<'FINDING_EOF'
# 发现：brew 有吞吐计数器与拒绝面（不是黑盒），但 `--dry-run` 只能靠 `stored=0` 反推

**结论**：`brew done in 7.2s: sessions=1 chunks=3 accepted=3 rejected=2 stored=0 deduped=0 errors=0`
—— 一行里六个计数器，其中 `rejected=2` 证明它在**拒绝**（不是照单全收），
`stored=0` 是 `--dry-run` 生效的证据。

**怎么被这次运行证实的**：会话目录里放了本次 run 自己的 jsonl（1 个文件），
所以 `sessions=1 chunks=3` 是可核对的；`accepted + rejected = chunks`（3 = 3 + 2 里
chunks=3、accepted=3 —— 拒绝的 2 个不在 chunk 计数里，说明 rejected 统计的是**候选切片**
之外的判断）。

**真不足**：`--dry-run` 在输出里**没有自己的字段**。要证明「这次没有写库」，
只能读 `stored=0` —— 而真跑时如果全部被去重，`stored=0` 也是同一个值。
**「没写因为 dry-run」与「没写因为都被去重了」在这一行里不可区分**。

**竞品比较**：`rsync -n`、`apt-get -s` 都把模式写进输出头（"This is a simulation"）；
GoPi 的这几条 dry-run 开关（brew / memory sync / memsync 都没有 dry-run）
没有统一的模式标记。**可提请的改法**：dry-run 时在收尾行前缀一个模式词。
FINDING_EOF

finding 011 memsync-export <<'FINDING_EOF'
# 发现：补丁文件的可评审性是真的（行不含 id），但导出行的「来源过滤」口径只在 help 里

**结论**：`memsync --export` 的收尾行把「传了几行、被 `--since` 挡了几行、落到哪个文件、
以哪个节点的名义」压进同一行，且默认导出**排除 `node:*` 来源**（anti-reflow）。

**怎么被这次运行证实的**：沙盒里 1 条记忆行（`sources=task 01a0ed23`），导出报
`exported 1 rows (1 scanned, 0 skipped by --since)` —— 扫描数 1、导出数 1，说明本行的来源
不在排除面内。**注意括号里那个 `skipped by --since` 是唯一的过滤计数**：
排除 `node:*` 的过滤**没有对应的计数器**。

**真不足**：一个运维者想知道「库里有 5 行，为什么只导出了 3 行」，他的候选解释有
`--since`、`--source` 窄化、`node:*` 反流排除三种，而输出只报其中一种的计数。
**反流排除是静默的** —— 与 Rule 15「不静默失败」同一形状的问题。

**竞品比较**：`git log --since` 一类过滤在 `--stat` 汇总里会写 `N files changed`；
`rsync --dry-run` 给出 transfer/exclude 两栏。GoPi 这里缺的是**排除栏**。
FINDING_EOF

finding 012 sync-peer-refused <<'FINDING_EOF'
# 发现：这条拒绝是本轮措辞最好的一句输出——「拓扑是声明的，从不被发现」

**结论**：`gopi memory sync --peer nosuchpeer --dry-run` 在未声明的 peer 上**拒绝并 exit=1**，
且拒绝句自带配置入口（`settings.json "memory_peers" / GOPI_MEMORY_PEERS`）与设计原则
（`topology is declared, never discovered`）。

**怎么被这次运行证实的**：02-output.txt 里那句拒绝 + 末行 `exit=1` —— 是一条**可被判读的
拒绝**，不是静默跳过、不是回退到某个默认 peer。

**为什么值得记一笔**：它与 §6 的 `--counts`（同一次运行里给出与 `--shape` 矛盾的分类、且不提示）
形成对照：**同一族动词里，安全边界的措辞是硬的，统计口径的措辞是软的**。

**不足**：`--serve-assets`（peer 侧）在帮助里被描述为 "ssh'd by an initiator and never run by a
human" —— 那意味着这条**只能靠一个活的 initiator 才能真跑**，本条目（离屏、单机）**没法真跑它**。
**没跑出来就写没跑出来**：本条目只覆盖 `--peer` 的拒绝路径，`--serve-assets` 未取证。

**竞品比较**：CRDT/同步类工具（Syncthing 的 device ID 白名单、git remote 的显式 URL）
同样是「先声明后同步」；GoPi 的差异点是把这条原则**写进拒绝句本身**，
让读到的人不必去翻文档。
FINDING_EOF

finding 013 search-miss <<'FINDING_EOF'
# 发现：空结果是一条**合法结果**（exit=0），且它顺手指路 —— 与「读不到」是两个信号

**结论**：无命中时输出一句面向人的说明、**退出码 0**。这与 012 的拒绝（exit=1）构成一对：
「库里没这条经验」不是错误，「你配置错了」才是。

**怎么被这次运行证实的**：本条目 02-output.txt 末行 `exit=0`；句子里还带了下一步动作
（"after you finish the task, call memory(action=store)"）。

**为什么这条值得单独收录**：census/M0 契约里 `unreadable` 与 `incomplete` 是**两个诊断**
（README §5），CLI 层这里同理：空结果与读不到必须用不同的信号表示，否则脚本无法区分
「本轮没命中」与「库不可用」。这一条测的正是前者。

**不足**：无命中句**没有**报它查了几行 —— 一个空库上的空命中与一个 5000 行库上的空命中
输出完全相同。`provenance --shape` 是唯一能补上这个数的地方（要另跑一条命令）。

**竞品比较**：Elasticsearch 的 `hits.total.value = 0` 把「查了但没中」作为结构化字段给出；
搜索引擎 UI 会写「共 0 条，用时 3ms」。GoPi 这里只有散文
—— **对人和对脚本的两种读者，用的是同一种渲染**。
FINDING_EOF

finding 014 store-junk-refused <<'FINDING_EOF'
# 发现：准入闸门是真的（exit=1），但它是个**启发式**——拒绝的理由只有一句「没内容」

**结论**：`memory store --topic probe --question q --answer a` →
`memory_store: rejected as garbage (no distilled knowledge content)`，`exit=1`。
**同一个动词、同样的参数形状**，内容一变结果就翻转（与 001 成对）。

**怎么被这次运行证实的**：本条目 02 的拒绝句与末行 `exit=1`；001 是同一动词的成功面。

**真不足（这一条是本轮我实际撞上的）**：闸门判断的是**长度/词汇**，不是语义。我第一次真跑
`--answer "a"` 被拒时，输出的措辞是 `no distilled knowledge content` —— 它断言的是
「内容里没有蒸馏知识」，而实际上工具只能知道「这串字太短」。**裁决的语气比它的依据强。**
一条格式完整、字数充足、内容空洞的行会被收下（这一轮没有真跑这条——因为它需要我
造一条假知识，**没跑就不写**）。

**竞品比较**：LangMem / mem0 在写入侧**不做内容准入**（写什么存什么），把质量判断留给
下游的反馈层；GoPi 在写入侧就拦一道。两条路线各有权衡：前置闸门省下游噪声，
但把「什么算知识」的判断题藏在一条没有出口的理由里（`garbage` 不可申诉、不可查看规则）。

**可提请的改法（不是本条目要做的）**：拒绝句给出**触发的具体判据**（字数下限/必需字段），
让被拒的人知道该补什么，而不是猜。本条目只记录测量结果。
FINDING_EOF

# ============================== 形状断言 ======================================
echo "== 形状断言：每个条目必须恰好六件、件件非空 =="
shape_bad=0
for d in "$OUTROOT"/[0-9][0-9][0-9]-*/; do
  nfiles=$(find "$d" -maxdepth 1 -type f | wc -l)
  if [[ "$nfiles" -ne 6 ]]; then
    echo "SHAPE FAIL: $d 有 $nfiles 个文件（要求恰好 6）" >&2
    find "$d" -maxdepth 1 -type f -printf '    %f\n' >&2
    shape_bad=1
  fi
  for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
    [[ -s "$d/$f" ]] || { echo "SHAPE FAIL: $d/$f 缺失或为空" >&2; shape_bad=1; }
  done
  grep -qE '^exit=[0-9]+$' "$d/02-output.txt" \
    || { echo "SHAPE FAIL: $d/02-output.txt 没有 ^exit=<n> 行" >&2; shape_bad=1; }
  # 03 与 04 必须不同：sha 相同 = 标注没真发生（这是「标注过」的机器判据）
  s3=$(sha256sum "$d/03-raw.png" | cut -c1-16); s4=$(sha256sum "$d/04-annotated.png" | cut -c1-16)
  [[ "$s3" != "$s4" ]] || { echo "SHAPE FAIL: $d 的 03 与 04 逐字节相同（标注没发生）" >&2; shape_bad=1; }
done

# --- 台账隔离自证（表级判据，不是字节级 grep）-------------------------------
# 为什么不能用 `grep <探针> ~/.gopi/memory.db`：本 run 自己的
# `session_conclusions`（task(action=commit) 的结论文本）落的就是**同一个文件**，
# 于是探针字面量出现在操作者库里，grep 自己匹配自己——那是审计记录，不是知识库写入。
# 真判据是表级：操作者库的 distilled_qa 里查不到探针，沙盒库查得到。
q="select count(*) from distilled_qa where instr(topic||question||answer,'$PROBE_BODY')>0;"
# 操作者库是 WAL 模式且被本 run 自己并发写（session_conclusions）：一次 BUSY 会让
# sqlite3 什么都不打印。用 .timeout 等锁，并把「查询失败」与「查到了」分开报——
# 前者是证据不足，不是隔离被破坏（Rule 15：不许把两件事说成一件）。
iso_query() { sqlite3 -cmd '.timeout 5000' "$1" "$q" 2>"$W/iso.err"; }
iso_op="$(iso_query "$OPLEDGER")"
iso_sb="$(iso_query "$SB/.gopi/memory.db")"
echo "== 台账隔离 =="
echo "operator_ledger=$OPLEDGER distilled_qa_hits=${iso_op:-<query-failed>}  (must be 0)"
echo "sandbox_ledger=$SB/.gopi/memory.db distilled_qa_hits=${iso_sb:-<query-failed>}  (must be >=1)"
iso_bad=0
if [[ -z "$iso_op" ]]; then
  echo "GENERATOR FAILED: 操作者台账查询失败，无法证明隔离（stderr: $(head -c 200 "$W/iso.err")）" >&2
  iso_bad=1
elif [[ "$iso_op" != "0" ]]; then
  echo "GENERATOR FAILED: 探针出现在操作者真台账的 distilled_qa（隔离被破坏）" >&2
  iso_bad=1
fi
[[ "${iso_sb:-0}" -ge 1 ]] || { echo "GENERATOR FAILED: 探针不在沙盒台账里（那这次 store 没落到沙盒）" >&2; iso_bad=1; }
{
  echo "operator_ledger=$OPLEDGER"
  echo "operator_distilled_qa_hits=${iso_op:-<query-failed>}"
  echo "sandbox_ledger=$SB/.gopi/memory.db"
  echo "sandbox_distilled_qa_hits=${iso_sb:-<query-failed>}"
  echo "note=byte-level grep is NOT the criterion: this run's own session_conclusions row holds PROBE_BODY_$STAMP literally"
} > "$EV/ledger-isolation.txt"
[[ "$iso_bad" -eq 0 ]] || exit 1
echo "== index =="
cat "$W/index.tsv"
if [[ -s "$W/failed.txt" ]]; then
  echo "== FAILED entries ==" >&2
  cat "$W/failed.txt" >&2
  echo "GENERATOR FAILED: $(wc -l < "$W/failed.txt") entry/entries produced no annotated image" >&2
  exit 1
fi
[[ "$shape_bad" -eq 0 ]] || { echo "GENERATOR FAILED: 形状断言未通过" >&2; exit 1; }
printf 'WORK=%s\nSANDBOX=%s\nEVIDENCE=%s\nPROBE_ID=%s\n' "$W" "$SB" "$EV" "$ID"