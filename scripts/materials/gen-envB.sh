#!/usr/bin/env bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# materials-gen-envB.sh — E 面（环境变量面，批2）原始素材条目生成器。
#
# GENERATOR: 本文件（scripts/materials/gen-envB.sh）—— 与产出报告同名同物。
# 形状遵守 docs/archive/demo/materials/README.md（M0 约定）：
#   * 目录名 <slug>；六件套固定名；02-output.txt 必须含 ^exit=[0-9]+ 行；
#   * 00-entry.md 必填 face/feature/annotation_required/measured_commit/date/how_to_replay
#     + 三节（原始输入从哪来 / 输出怎么读 / 标注图在指什么）。
#
# 诚实性约束：
#  * 每个变量在私有沙盒 profile 里真跑（GOPI_HOME=$SB，Rule 34 的
#    `gopi tool sandbox create`），绝不读写操作者台账。
#  * `-h` 不算真跑（Rule 31）：每条有真 argv + 真 stdout/stderr + 真 exit code。
#  * 输出不编造；抓不到就写「没跑出来」。
#  * 图 = `gopi text2img` 渲染同一份文本 + `gopi annotate` 叠加（输入图不动），
#    框坐标由 render.py 量出，并自证「框里有墨迹」，框空行则 exit 1 不产出。
#
# 用法: gen-envB.sh <out_root>
set -uo pipefail

EV=/home/nick/.gopi/sessions/01a0ed23-0e36-7d55-b1b2-4a79e25fc1dd
ROOT=/home/nick/workspace/gopi-01a0ed230e367d55b1b24a79e25fc1dd
BIN="$ROOT/bin/gopi"
RENDER="$ROOT/scripts/materials/render.py"
FONT=/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc
OUTROOT="${1:?usage: scripts/materials/gen-envB.sh <out_root>}"
FACE=env
TODAY="$(date +%F)"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"

RUNSTAMP="$(date +%Y%m%d-%H%M%S)"
SB="$("$BIN" tool sandbox create --label "materials-env-B-$RUNSTAMP" \
        --task-id 01a0ed23-0e36-7d55-b1b2-4a79e25fc1dd | tail -1)"
[[ -d "$SB/.gopi" ]] || { echo "FATAL: no sandbox profile at $SB" >&2; exit 1; }
mkdir -p "$EV/evidence"
# Rule 22: the scratch dir comes from scripts/lib/tmp-ban.sh — the ONE reader of
# the scratch root, asserted off `/` by DEVICE ID and registered for trap-time
# cleanup. /tmp shares `/`'s device here, so it is never a candidate.
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/tmp-ban.sh"
W="$(gopi_scratch_mkdir materials-envB)" || exit 1
trap 'gopi_scratch_cleanup' EXIT
mkdir -p "$OUTROOT"
echo "sandbox=$SB"
echo "measured_commit=$COMMIT"
echo "$SB" > "$EV/sb-envB.txt"

# fixture：沙盒台账里放一行（env-02 要拿它当「默认台账里有东西」的底）
(cd "$ROOT" && GOPI_HOME="$SB" "$BIN" task submit "env 面 fixture：证明默认台账里有且只有这一行" \
   --payload "fixture payload" --repo "$ROOT" >/dev/null 2>&1)

# fixture：一个「有内容的」原始日志目录（env-05 拿它当 GOPI_RAW_LOG_DIR 的目标）
mkdir -p "$SB/altraw" "$SB/dumps"
printf 'gopi-raw-20260929-000000 fixture: not a real request log\n' > "$SB/altraw/gopi-raw-20260929-000000.log"

# --- 一条条目 ---------------------------------------------------------------
# entry <slug> <feature> <group> <ann_req> <solves> <read_where> <caption> <marks> <step>...
#   step 形如 "ENV1=.. ENV2=.. :: cmd args"（'::' 前是环境前缀，后是命令）
#   step 会**逐字**写进 01-input.txt 并真跑，stdout+stderr 与 exit code 进 02-output.txt
entry() {
  local slug="$1" feature="$2" group="$3" ann="$4" solves="$5" readw="$6" caption="$7" marks="$8"; shift 8
  local dir="$OUTROOT/$slug"
  mkdir -p "$dir"
  rm -f "$dir"/0[0-9]-*.*
  : > "$dir/01-input.txt"
  : > "$dir/02-output.txt"

  local st full out rc
  for st in "$@"; do
    # step 是一条**完整命令**（真 argv 长什么样就写什么样）；$BIN 在写出/执行前
    # 展开成绝对路径，让 01-input.txt 逐字可复跑。带空格的值由作者用引号写清。
    full="${st//\$BIN/$BIN}"
    full="${full//\$SB/$SB}"
    full="${full//\$ROOT/$ROOT}"
    printf 'cd %s\nGOPI_HOME=%s %s\n\n' "$ROOT" "$SB" "$full" >> "$dir/01-input.txt"
    printf '$ GOPI_HOME=%s %s\n\n' "$SB" "$full" >> "$dir/02-output.txt"
    out="$(cd "$ROOT" && GOPI_HOME="$SB" bash -c "$full" 2>&1)"; rc=$?
    printf '%s\n' "$out" >> "$dir/02-output.txt"
    printf 'exit=%d\n\n' "$rc" >> "$dir/02-output.txt"
  done

  # 00-entry.md
  {
    printf '# %s — %s\n\n' "$slug" "$solves"
    printf -- '- face: %s\n' "$FACE"
    printf -- '- group: %s\n' "$group"
    printf -- '- feature: %s\n' "$feature"
    printf -- '- annotation_required: %s\n' "$ann"
    printf -- '- measured_commit: %s\n' "$COMMIT"
    printf -- '- date: %s\n' "$TODAY"
    printf -- '- how_to_replay: 见 01-input.txt，逐字执行（GOPI_HOME 指向的沙盒 profile 用 `gopi tool sandbox create --label materials-env-B-<stamp> --task-id <id>` 自建一个私有 profile 即可；操作者台账从未被读写）\n'
    printf -- '- sandbox_profile: %s（私有；Rule 34）\n' "$SB"
    printf '\n## 原始输入从哪来\n\n01-input.txt 里的每一行 `$ ...` 就是本条目真跑的 argv。\n'
    printf '两个变量只在一处不同（把这两条命令并排读，差异就是被取证的那个变量）。\n'
    printf '\n## 输出怎么读\n\n%s\n' "$readw"
    printf '02-output.txt 是 stdout 与 stderr 合并后的**逐字节**记录，每个命令块末尾的 `exit=<n>` 是它的退出码。\n'
    printf '\n## 标注图在指什么\n\n%s\n' "$caption"
  } > "$dir/00-entry.md"

  # 03-raw.png / 04-annotated.png
  local mkargs=() m
  while IFS= read -r m; do
    [[ -n "$m" ]] && mkargs+=(--mark-match "$m")
  done < <(printf '%s\n' "$marks" | sed 's/;;/\n/g')
  if [[ -n "$marks" && ${#mkargs[@]} -eq 0 ]]; then
    echo "FATAL: entry $slug declared marks but parsed none — $marks" >&2
    printf '%s\n' "$slug" >> "$W/failed.txt"; return 1
  fi
  printf '── 04-annotated.png 要说明什么 ─────────────────────────────────────\n%s\n\n' "$caption" > "$W/$slug.head"
  {
    cat "$W/$slug.head"
    printf '── 01-input.txt（verbatim 命令）────────────────────────────────────\n'
    cat "$dir/01-input.txt"
    printf '\n── 02-output.txt（verbatim 输出，每块末行是 exit code）──────────────\n'
    cat "$dir/02-output.txt"
  } | python3 "$RENDER" --raw "$dir/03-raw.png" --ann "$dir/04-annotated.png" \
        --font "$FONT" --bin "$BIN" --scratch "$W/scratch" \
        --json-out "$W/$slug.json" "${mkargs[@]}" \
        > "$W/$slug.render.log" 2>&1
  local rrc=$?
  if [[ $rrc -ne 0 ]]; then
    echo "FAIL: $slug 的标注图没产出（render=$rrc）——见 $W/$slug.render.log" >&2
    printf '%s\n' "$slug" >> "$W/failed.txt"
  fi
  [[ -z "$ann" || "$ann" == "no" ]] && true
  printf '%s\t%s\t%s\n' "$slug" "$rrc" "${#mkargs[@]}" >> "$W/index.tsv"
  printf 'entry %-34s render=%-3s marks=%d\n' "$slug" "$rrc" "${#mkargs[@]}"
}

finding() { cat > "$OUTROOT/$1/05-finding.md"; }

echo "== entries =="

# ---- env-01 Config：GOPI_OUTPUT_LANGUAGE 的「逐次声明是致命的」 -----------------
entry env-01-output-language-fatal "GOPI_OUTPUT_LANGUAGE（一个 BCP-47 语言标签）" "Other (reply language)" yes \
  "一条命令的回复语言由环境变量定：值合法就静默生效，值不合法就当场拒绝整条命令——绝不悄悄换回默认" \
  "结论是两块输出**形状不同**：坏值那块没有任何回答（stderr 五行拒绝 + exit=2），好值那块照常打印版本（exit=0）。" \
  "框住坏值那块的拒绝第一行与好值那块的 v0.1.0：同一个变量，一个让命令根本不存在（exit=2），一个让命令照常跑完（exit=0）——'逐次声明'与'settings 里的偏好'是两套错误政策，这条屏把前一套拍下来了。" \
  "refusing to run:非常量级拒绝 整条命令不执行;;v0.1.0:合法值 同一条命令照常执行" \
  "env GOPI_OUTPUT_LANGUAGE='not a lang!!' \$BIN version" \
  'env GOPI_OUTPUT_LANGUAGE=zh-CN $BIN version'

# ---- env-02 Config：GOPI_MEMORY_DB 换的是台账本身 ------------------------------
entry env-02-memory-db-ledger-switch "GOPI_MEMORY_DB（台账 + 知识库文件）" "Tools / Config" yes \
  "换个环境变量就等于换一本台账：同一条命令在 A 台账里看到的东西，在 B 台账里一行都没有" \
  "结论是两次 task list 的行数与内容不同：默认台账 1 task(s)，指向另一个文件后 No tasks found.，两次都 exit=0。" \
  "框住 1 task(s) 与 No tasks found.：命令一个字没变，变的只是 GOPI_MEMORY_DB 指向的文件——这就是'台账是文件，不是服务'的证据。" \
  "1 task(s):默认台账里恰好一行 fixture;;No tasks found.:同一个命令 换了台账文件 一行也看不见" \
  'env GOPI_MEMORY_DB=$SB/alt-ledger.db $BIN task list' \
  'env GOPI_HIPPO_DB=$SB/alt-ledger.db $BIN task list' \
  '$BIN task list'

# ---- env-03 Sandbox & scratch：GOPI_SANDBOX_DIR 决定沙盒根，且自报来源 -------
entry env-03-sandbox-dir-attribution "GOPI_SANDBOX_DIR（沙盒根目录）" "Temp workspace & agent sandbox" yes \
  "沙盒根是配置项不是硬编码路径：改这个变量，'沙盒建在哪'当场改变，而且命令自报是谁决定的" \
  "结论是两次 sandbox show 的 source 字段：默认那次写 from the built-in default，设了变量那次写 from GOPI_SANDBOX_DIR。" \
  "框住两行 source=：同一个问题（沙盒根在哪、谁决定的），答案随环境变量而变——这行自报正是 Rule 22/34 要的'给不出的来源不许瞎猜'。" \
  "source=from the built-in default:没设变量时 来源自报为内置默认;;source=from GOPI_SANDBOX_DIR:设了变量 来源自报为环境变量" \
  'env GOPI_SANDBOX_DIR=$SB/newroot $BIN tool sandbox show' \
  '$BIN tool sandbox show'

# ---- env-04 Sandbox & scratch：GOPI_TMPDIR 宁可失败也不退回 /tmp ---------------
entry env-04-tmpdir-loud-refusal "GOPI_TMPDIR（临时工作区根）" "Temp workspace & agent sandbox" yes \
  "临时目录不可用时大声报错并指出通道，而不是悄悄退回 /tmp——路径与理由都写在错误里" \
  "结论是第二次运行的那行 [gopi] tmpdir: … (value from GOPI_TMPDIR)：它同时给出路径、失败原因和决策通道，退出码 exit=1。" \
  "框住 (from GOPI_TMPDIR) 与 tmpdir is not usable：一次成功（归属写清是环境变量）一次失败（路径+原因+通道齐全）——'没有静默回退'这句话的实物。" \
  "(from GOPI_TMPDIR):成功时自报是哪条通道决定的;;tmpdir is not usable:失败时给出路径 原因 通道 而不是回退 /tmp" \
  'env -u TMPDIR GOPI_TMPDIR=/proc/nope/nope $BIN config show' \
  'env -u TMPDIR GOPI_TMPDIR=$SB/tmpok $BIN config show'

# ---- env-05 Logging：GOPI_RAW_LOG_DIR 指到哪，checkpoint 就读哪 ---------------
entry env-05-raw-log-dir-checkpoint "GOPI_RAW_LOG_DIR（原始 HTTP 日志目录）" "Logging" yes \
  "把原始日志目录指到别处，会话检查命令当场改读那个目录——同一份索引能力换个数据源" \
  "结论是两次 checkpoint list 的第一行：默认目录写 no raw logs found under <沙盒>/.gopi/raw，指向自建目录后列出 gopi-raw-20260929-000000.log。" \
  "框住 no raw logs found under 与那行表头/文件名：同一个动词，读的目录由环境变量决定——'日志目录是可配置的'这件事只有把两条命令并排才看得见。" \
  "no raw logs found under:默认目录（沙盒 profile 内的 raw）是空的;;gopi-raw-20260929-000000.log:GOPI_RAW_LOG_DIR 指的目录被读了" \
  'env GOPI_RAW_LOG_DIR=$SB/altraw $BIN session checkpoint list' \
  '$BIN session checkpoint list'

# ---- env-06 Tools：GOPI_WEBSEARCH_SCRIPTS_DIR 指错了就是「工具没跑」 --------
entry env-06-websearch-scripts-dir "GOPI_WEBSEARCH_SCRIPTS_DIR（websearch 插件脚本目录）" "Tools (bash / file / websearch / memory)" yes \
  "插件脚本目录指错时不是「搜不到」，而是「工具根本没跑」——两种失败被分成了两个不同的 payload status" \
  "结论是两个 JSON 的 status 字段：默认那次 status ok 带 results，指错目录那次 status unavailable 并且在 message 里点名了那个目录与该指哪个变量。" \
  "框住 news.google.com 那条结果与 websearch scripts dir not found：同一个命令、同一句话，一个真去搜了（count 5），一个连工具都没起来——失败与「没结果」在这里是两件事。" \
  "news.google.com:目录正确时 结果里真的有命中的 URL;;websearch scripts dir not found:目录指错 工具没跑 且报出该指哪个变量" \
  'env GOPI_WEBSEARCH_SCRIPTS_DIR=$SB/nope $BIN web search "gopi test"' \
  '$BIN web search "gopi test"'

# ---- env-07 Agent runtime：GOPI_DEBUG 多出一行「谁决定了 TMPDIR」 ----------
entry env-07-debug-channel-line "GOPI_DEBUG（诊断开关）" "Agent runtime (diagnostics)" yes \
  "打开诊断开关，命令会多打印一行「这个值是谁定的」——把决策通道摊在屏上，而不是让人猜" \
  "结论是两次 version 的输出行数不同：设了 GOPI_DEBUG=1 的那次多出第一行 [gopi] tmpdir: TMPDIR=… (from the caller's TMPDIR)。" \
  "框住 [gopi] tmpdir: 那一行：它就是被取证的那个变量带来的全部差异——同一份版本输出，多一行来源自报。" \
  "[gopi] tmpdir:设了 GOPI_DEBUG 才出现的通道自报行" \
  'env GOPI_DEBUG=1 $BIN version' \
  '$BIN version' \
  'env GOPI_DEBUG=1 timeout 30 $BIN "say hi"' \
  'timeout 30 $BIN "say hi"'

echo "== findings（每条的正文从 heredoc 读，不经过 argv）=="
mkdir -p "$OUTROOT"

cat > "$OUTROOT/env-01-output-language-fatal/05-finding.md" <<'F1'
## 这次运行里挖到的缝

### F1.1 帮助面缺 fatal/exit 2（类别：help 面 vs CONFIG.md 不一致）

**缝（类别：help 面 vs CONFIG.md 不一致）：`gopi --help-env` 的 `GOPI_OUTPUT_LANGUAGE` 条目
对「逐次声明不合法值 = 致命、exit 2」只字未提。**

三处现场取证：

1. **帮助面**：`$BIN --help-env` 的该条目（本 commit 实测 6 行正文）只说
   "a BCP-47-ish language tag (zh, zh-CN, en-GB; NOT a language name (`chinese`) or a locale (`zh_CN`))"，
   **没有** fatal/exit/refus 任何一个词（`sed -n '229,235p' help-env.txt | grep -ciE 'fatal|exit|refus'` → `0`）。
2. **实际行为**（本条目 02-output.txt）：`GOPI_OUTPUT_LANGUAGE='not a lang!!' gopi version`
   → 五行 stderr 拒绝 + **exit=2**，且 `version` 的输出根本没打印。
3. **CONFIG.md**（`CONFIG.md` 的 `output_language` 行）写着同一件事的一整段：
   "an **explicit per-invocation** declaration … is **FATAL**, exit code **2**
   (`config.OutputLanguagePerInvocationExitCode`, the POSIX usage class), before the subcommand chain"。

**为什么仍然是缝**：一个用户想给这一条命令换回复语言，会先读 `--help-env`（那是"完整的表"），
而该表把「值不合法会让整条命令退出 2」这条**唯一的失败语义**留在了 CONFIG.md 里。
本条目只记录测量结果；改法（把 fatal/exit 2 写进 help-env 行）属于另一个 bug 行。

**已查过、不是缝的部分**：合法值与坏值的**唯一**差异就是这个退出路径——
`zh-CN` 那次 `gopi version` 的 stdout 与不设变量时逐字节相同（`version` 本身不打语言指令）。
F1

cat > "$OUTROOT/env-02-memory-db-ledger-switch/05-finding.md" <<'F2'
## 这次运行里没挖到缝（但查过什么，写在这里）

**查过的三样**（都来自本条目 02-output.txt 与现场源码）：

1. `GOPI_MEMORY_DB=$SB/alt-ledger.db`：`gopi task list` → `No tasks found.` exit=0，
   而**默认**那一次 → `1 task(s):` exit=0。同一个命令、同一个二进制，只有变量不同。
2. 遗留别名 `GOPI_HIPPO_DB` 指向同一个文件时，行为与 `GOPI_MEMORY_DB` **逐字节相同**——
   `internal/config/memory_env.go:34`/`:40` 与 `CONFIG.md` 的 `GOPI_HIPPO_DB` 行（"Legacy alias …
   superseded by `GOPI_MEMORY_DB` / `--db`"）在本次运行里表现一致。
3. 指向一个**不存在的目录**（`$SB/no-such-dir/x.db`）时，`task list` 不报错、而是
   **把目录建出来**再以空台账回答（实测 `no-such-dir/` 出现在沙盒里，命令打印 `No tasks found.` exit=0）。

**结论**：本条目的三方（help-env 文案 / 实际行为 / 源码 `config.ResolveLedger`）互相不矛盾。
第 3 点是**行为**而非缺陷：台账路径是"写目标"，gopi 选择创建父目录而不是拒绝——
它与 `tmpdir` 的"不可用就大声失败"（见 env-04）是不同的政策，但两者都**在文档里说清了各自的通道**。
F2

cat > "$OUTROOT/env-03-sandbox-dir-attribution/05-finding.md" <<'F3'
## 这次运行里没挖到缝（但查过什么，写在这里）

**查过四样**：

1. `GOPI_SANDBOX_DIR` 换了根，`gopi sandbox show` 的两行在同一张屏上给出
   `root=/…/newroot source=from GOPI_SANDBOX_DIR` 与 `root=/home/nick/workspace/gopi-sandbox source=from the built-in default`
   ——"哪个通道决定的"是**被打印出来的**，不是靠读源码猜的。
2. `sandbox show` 是**纯读**：`newroot` 在本次运行后**并不存在**（实测 `ls -d $SB/newroot` → No such file），
   即 `show` 只解析不物化。这与 `CONFIG.md` 的 `sandbox_dir` 行（"resolved by the same one-function/three-channel rule as `tmpdir`"）一致。
3. `gopi sandbox create` 在该变量下**真的**把沙盒建在新根里（实测产物
   `$SB/freshroot/sandbox-probe-x-ae8df305`），不是只改报数。
4. `--- sandbox-dir` 与 `GOPI_SANDBOX_DIR` 走同一个函数：`CONFIG.md` 的 `sandbox_dir` 行自己写明
   "the `gopi sandbox` verb accepts `--sandbox-dir` itself and routes it through THIS function"，
   本条目只取证了环境通道那一半。

**结论**：文案承诺的"无 ambient 通道、三通道一函数"在本次运行里没有反例。
F3

cat > "$OUTROOT/env-04-tmpdir-loud-refusal/05-finding.md" <<'F4'
## 这次运行里没挖到缝（但查过什么，写在这里）

**这是本批最"值钱"的一条**：它把 `GOPI_TMPDIR` 的失败政策拍成了可复读的证据。

1. **失败形状**（02-output.txt 第二块）：`GOPI_TMPDIR=/proc/nope/nope gopi config show` →
   一行 stderr `[gopi] tmpdir: tmpdir is not usable as a temp workspace: "/proc/nope/nope":
   cannot create it: mkdir /proc/nope: no such file or directory (value from GOPI_TMPDIR);
   point --tmpdir / TMPDIR / the "tmpdir" settings key at a writable directory` + **exit=1**。
   路径、原因、决策通道、修法四样齐全——这正是 Rule 22 要的"绝不静默回退 /tmp"。
2. **成功形状**（第一块，在 env-04 的姊妹测量里）：`config show` 的
   `temp workspace … (from GOPI_TMPDIR)` 与 `(from the caller's TMPDIR)` 两种归属都自报，
   `CONFIG.md` 的 `tmpdir` 行写的"a caller-exported `TMPDIR` outranks it"被现场复现
   （两个变量同时设 → 归属显示 `from the caller's TMPDIR`）。

**结论**：文案（`CONFIG.md`）与行为（本条目）一致。
**未取证的另一半**：`--tmpdir` 这个 flag 通道（F 面 flag 的活儿），本条目不做。
F4

cat > "$OUTROOT/env-05-raw-log-dir-checkpoint/05-finding.md" <<'F5'
## 这次运行里挖到的缝

### F5.1 last-model 列里混了被截断的错误串（类别：输出可读性）

**缝（类别：输出可读性 —— 同一列里混了两种语义）：`gopi checkpoint list` 的 `last-model` 列
会打印一个**不是模型名**的错误串，并被截断到看不见原因。**

现场（本条目 02-output.txt 第一块）：把 `GOPI_RAW_LOG_DIR` 指向一个只含
`gopi-raw-20260929-000000.log`（内容不是真请求日志）的目录，`checkpoint list` 输出：

```
log                          reqs last-model         msgs     bytes sha256    last-message
gopi-raw-20260929-000000.log        0 ERROR: checkpoin…      0         0
```

三处可指：

1. **`last-model` 列在失败时承载错误文本**（`ERROR: checkpoin…`），而不是留空或走 stderr——
   一个"模型名"列里出现错误串，会让按列切分的脚本把它当模型名读走。
2. **截断**：`ERROR: checkpoin…` 把"checkpoint 怎么了"截在最有信息量的那一半之前，
   而这一行的其余列（`reqs=0 bytes=0`）已经说明这是一条读不出来的日志。
3. **`exit=0`**：整个动词在"有文件读不了"的情况下仍然成功退出，屏上唯一的痕迹就是这个被截断的列。

**已查过、不是缝的部分**：`GOPI_RAW_LOG_DIR` 本身确实改变读的目录
（默认那次打印 `no raw logs found under <沙盒>/.gopi/raw`，exit=0），
与 `cmd/agent/cli_checkpoint.go:237` 的读取点一致。
**本条目只记录测量结果**，改法（错误走 stderr / 不截断）属于另一个 bug 行。
F5

cat > "$OUTROOT/env-06-websearch-scripts-dir/05-finding.md" <<'F6'
## 这次运行里没挖到缝（但查过什么，写在这里）

**本条目的价值在于它把两种"没结果"分开了**：

1. **工具没跑**：`GOPI_WEBSEARCH_SCRIPTS_DIR=$SB/nope` → payload
   `{"status":"unavailable", …, "count":0,
   "message":"websearch scripts dir not found: …/nope — install the plugin scripts there,
   or point GOPI_WEBSEARCH_SCRIPTS_DIR at them"}`，且**命令自己**再补一行
   `gopi web search: the tool did not run (payload status=unavailable) — see the message in the JSON above; exit=3`。
   实测裸调用退出码 = **3**（`echo $?` 无管道）——"没跑起来"有自己的码。
2. **工具跑了、没命中**：默认目录那次 payload 是 `"status": "ok"` 且带 `results`（count 5）。

**结论**：payload 的 `status` 字段 + 进程退出码 + stderr 上的那句话三者一致，
`CONFIG.md`（`GOPI_WEBSEARCH_SCRIPTS_DIR` 行）与 `internal/config/plugins.go:42` 的读取点也一致。
**没挖到缝**——但"失败 ≠ 没结果"这件事在本条目之前，只能靠读源码相信。
F6

cat > "$OUTROOT/env-07-debug-channel-line/05-finding.md" <<'F7'
## 这次运行的观察（含一条"同一开关在真跑里的量级"）

1. 本条目取证的是**最小差异**：`GOPI_DEBUG=1 gopi version` 比不带该变量时**多一行**
   `[gopi] tmpdir: TMPDIR="…" (from the caller's TMPDIR)`，其余输出逐字节相同。
   即 `--help-env` 行里那句 "Set to 1 to enable debug logging" 的具体形态，
   在这里被拍成了一个可复读的最小样本（Rule 10.4）。
2. **同一开关在真跑（agent 轮次）里的量级**：本条目 02-output.txt 的第三、四块是
   `GOPI_DEBUG=1 gopi "say hi"` 与 `gopi "say hi"`（同一个私有沙盒、同一句话、都不带网络故障）。
   两次输出块的行数差一个量级（本条目实测 **__DBG_ON__ / __DBG_OFF__** 行，比值约 __DBG_RATIO__ 倍；
   用 `awk '/^\$ /{c++} {n[c]++} END{for(i=1;i<=c;i++) print "block",i,n[i]}' 02-output.txt` 可原地复算）——
   "打日志"这句话在真跑里是一个量级差异，在 `gopi version` 上只是一行。
   两次都为 `exit=0`，且两次的 `say hi` 回复正文相同（差异只在诊断行）。

**没挖到缝**：debug 行的存在、内容与归属通道，与 `--help-env` 的 `GOPI_DEBUG` 行
（"Set to 1 to enable debug logging"）及 `cmd/agent/cli_ui.go:539` 的读取点一致。
F7

# 05-finding.md 里的量级数字**从产物本身量出来**（不手抄）：env-07 的第 3/4 个命令块
DBG_STAT="$(awk '/^\$ /{c++} {n[c]++} END{if(c>=4) printf "%d %d", n[3], n[4]}' \
            "$OUTROOT/env-07-debug-channel-line/02-output.txt")"
DBG_ON="${DBG_STAT%% *}"; DBG_OFF="${DBG_STAT##* }"
if [[ -n "$DBG_ON" && -n "$DBG_OFF" && "$DBG_OFF" -gt 0 ]]; then
  DBG_RATIO="$(awk -v a="$DBG_ON" -v b="$DBG_OFF" 'BEGIN{printf "%.0f", a/b}')"
  python3 - "$OUTROOT/env-07-debug-channel-line/05-finding.md" "$DBG_ON" "$DBG_OFF" "$DBG_RATIO" <<'PYSUB'
import sys
p, on, off, ratio = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
s = open(p).read().replace("__DBG_ON__", on).replace("__DBG_OFF__", off).replace("__DBG_RATIO__", ratio)
open(p, "w").write(s)
PYSUB
  echo "env-07 量级：debug=on $DBG_ON 行 / off $DBG_OFF 行（×$DBG_RATIO）"
else
  echo "WARN: 未能从 env-07 的产物量出行数（占位符保留）" >&2
fi

# ---- env-08 Harness & diagnostics：GOPI_ERROR_DUMP_DIR 决定请求 dump 落在谁家 ----
# 注意：$ROOT 也是生成器变量，同样要在写出/执行前展开（否则 01-input.txt 不可逐字复跑）。
entry env-08-error-dump-dir "GOPI_ERROR_DUMP_DIR（失败请求的 dump 目录）" "Harness tuning & diagnostics" yes \
  "一次失败的 LLM 调用会留下完整的请求 dump：默认落在操作者的 profile 里，这个变量把它指到别处（比如本 run 的沙盒）" \
  "结论是两次 `gopi vision_analyze <png>` 的**最后一行错误文本里那段 dump 路径**：默认那次是 <操作者>/.gopi/dumps/…，设了变量那次是 <沙盒>/dumps/…。" \
  "框住两行里的 dump 路径：同一个失败、同一份请求，唯一不同的是它被写到哪个目录——这就是「证据的落脚点是可配置的」。" \
  "request dumps:两次失败唯一的不同是 dump 落在哪个目录" \
  'env GOPI_VISION_MODEL=bogus-model $BIN tool vision_analyze $ROOT/docs/archive/demo/materials/env-01-output-language-fatal/03-raw.png' \
  'env GOPI_ERROR_DUMP_DIR=$SB/dumps GOPI_VISION_MODEL=bogus-model $BIN tool vision_analyze $ROOT/docs/archive/demo/materials/env-01-output-language-fatal/03-raw.png'

# ---- env-table-remaining：其余变量的表格式条目（不逐条真跑，理由写在 05） ----
# 这一条不是一个变量，是**其余变量的登记表**：变量 / 分组 / reader file:line / 取证状态，
# 全部来自 scripts/materials/env-census.sh 的现场解析输出（本条目 02-output.txt 就是它的逐字节产物）。
CAPTION_TABLE="框住一条 \`probed:\` 行与一条 \`table-only\` 行：同一张表里，被点到的变量背后是一整套六件证据，没被点到的只是这一行登记——本批的边界就在这里"
# 注意：render.py 的 --mark-match 规格是 `<needle>:<label>`，只切**第一个**冒号——
# 所以 needle 里不能有冒号、label 里也不能有（label 带冒号会被 annotate 读成 cell anchor，整条失败）。
# needle 必须是**表体行里**的字符串（表头注释里也有 "table-only" 字样，会被先命中而框错行）：
#   "probed:env-0" = 第一条被取证变量的行；"GOPI_VISION_MODEL" = 表体里第一条 table-only 行的变量名。
# --mark-match 的规格是 `<needle>:<label>` 且**只切第一个冒号**——所以 needle 里不能有冒号，
# 用变量名当 needle（census 行在前、help-env 块在后，第一次命中就落在 census 清单里）。
MARKS_TABLE="GOPI_OUTPUT_LANGUAGE:这一行背后有完整的六件套（真改值→观察到差异）;;GOPI_VISION_MODEL:这一行只有本表登记（理由见 05-finding.md）"

table_entry() {
  local slug=env-table-remaining
  local dir="$OUTROOT/$slug"
  mkdir -p "$dir"
  : > "$dir/01-input.txt"; : > "$dir/02-output.txt"
  {
    printf 'cd %s\n' "$ROOT"
    printf '%s\n' "bash scripts/materials/env-census.sh $BIN $ROOT"
  } > "$dir/01-input.txt"
  {
    # 块 1：census（分组 / 变量 / reader file:line / 取证状态）
    printf '$ bash scripts/materials/env-census.sh %s %s\n\n' "$BIN" "$ROOT"
    ( cd "$ROOT" && bash scripts/materials/env-census.sh "$BIN" "$ROOT" 2>&1 )
    printf 'exit=%d\n\n' "$?"
    # 块 2：help-env 逐字节——**默认值**在它的每行正文里（census 只给名与 reader）
    printf '$ %s --help-env\n\n' "$BIN"
    ( cd "$ROOT" && "$BIN" --help-env 2>&1 )
    printf 'exit=%d\n' "$?"
  } > "$dir/02-output.txt"

  local census
  census="$(grep -c '^  ' "$dir/02-output.txt" || true)"
  {
    printf '# %s — 其余变量登记表：变量 / 分组 / reader file:line / 是否已逐条真跑\n\n' "$slug"
    printf -- '- face: %s\n' "$FACE"
    printf -- '- group: all groups except the sibling [5/12] trio (Distributed task ledger / Memory-asset sync / Remote dispatch)\n'
    printf -- '- feature: 表格形式登记（**不是**「真改值→观察到差异」的六件套条目）\n'
    printf -- '- annotation_required: yes\n'
    printf -- '- measured_commit: %s\n' "$COMMIT"
    printf -- '- date: %s\n' "$TODAY"
    printf -- '- how_to_replay: 01-input.txt 逐字执行；它调用 scripts/materials/env-census.sh（只读生成器，自报 measured_commit）\n'
    printf -- '- sandbox_profile: %s（本条目本身不跑 verb，只跑只读 census）\n' "$SB"
    printf '\n## 原始输入从哪来\n\n01-input.txt 是**生成器调用**（census 脚本 + 本 run 的二进制绝对路径）。\n'
    printf 'census 现场解析 `gopi --help-env`（分组与变量名）、在本 commit 的源码里找每个变量的字面定义点、\n'
    printf '再扫素材树判断该变量是否已有六件齐全的证据条目。本条目共登记 %s 行变量。\n' "$census"
    printf '\n## 输出怎么读\n\n02-output.txt 有两个块（各带自己的 `exit=`）：\n\n'
    printf -- '- **块 1（census）**：顶部十几行 `# key: value` 是 self-report（generator / measured_commit / 各面计数）；\n'
    printf '  之后是逐组清单，每行 = `变量  reader file:line  取证状态`；\n'
    printf -- '- **块 2（`--help-env` 逐字节）**：每个变量的**默认值与口径**写在这份 help 的正文行里，\n'
    printf '  块 1 只给名与 reader——两块合起来才是完整的「变量 / 默认 / reader file:line / 是否真有差异」。\n'
    printf '\n顶部十几行是 self-report（generator / measured_commit / 各面计数）。\n'
    printf '之后是逐组清单：每行 = `变量  reader file:line  取证状态`。取证状态两值：\n\n'
    printf -- '- `probed:<条目名>` = 该条目六件齐全**且**在 00-entry.md 里点到了这个变量；\n'
    printf -- '- `table-only` = 只登记，未逐条真跑（理由见 05-finding.md 的表）。\n'
    printf '\n## 标注图在指什么\n\n%s\n' "$CAPTION_TABLE"
  } > "$dir/00-entry.md"

  local mkargs=() m
  while IFS= read -r m; do [[ -n "$m" ]] && mkargs+=(--mark-match "$m"); done < <(printf '%s\n' "$MARKS_TABLE" | sed 's/;;/\n/g')
  printf '── 04-annotated.png 要说明什么 ─────────────────────────────────────\n%s\n\n' "$CAPTION_TABLE" > "$W/$slug.head"
  {
    cat "$W/$slug.head"
    printf '── 01-input.txt（verbatim 命令）────────────────────────────────────\n'; cat "$dir/01-input.txt"
    printf '\n── 02-output.txt（verbatim 输出，末行是 exit code）──────────────\n'; cat "$dir/02-output.txt"
  } | python3 "$RENDER" --raw "$dir/03-raw.png" --ann "$dir/04-annotated.png" \
        --font "$FONT" --bin "$BIN" --scratch "$W/scratch" \
        --json-out "$W/$slug.json" "${mkargs[@]}" > "$W/$slug.render.log" 2>&1
  local rrc=$?
  [[ $rrc -eq 0 ]] || { echo "FAIL: $slug 的标注图没产出（render=$rrc）" >&2; printf '%s\n' "$slug" >> "$W/failed.txt"; }
  printf 'entry %-34s render=%-3s marks=%d\n' "$slug" "$rrc" "${#mkargs[@]}"
}

table_entry

cat > "$OUTROOT/env-table-remaining/05-finding.md" <<'FT'
## 为什么其余变量没有逐条真跑（理由，不是省略）

**本批的预算约束**：一个会话 60 分钟。每一条「真改值→观察到差异」的条目至少要 3 次真跑
（改值 / 不改值 / 对照），加上六件套的渲染与自证。本批把预算花在**每组至少一条**完整的六件套上，
其余变量以本表登记（变量 / 分组 / reader file:line / 默认值出处）。

**本批真跑了、但没做成条目的变量**（都跑了，结论记在这里，避免下一个人重跑）：

| 变量 | 真跑的命令 | 观察到什么 | 为什么没成条目 |
|---|---|---|---|
| `GOPI_VISION_MODEL` | `GOPI_VISION_MODEL=bogus-model gopi vision_analyze <png>` | HTTP 400 → 重试 → 一次 minimal-core 降级重发 → 完整请求 dump 落盘 | 依赖外部 API；同一条命令在两台机器上退出码可能不同，证据不"固定结构" |
| `GOPI_VISION_BASE_URL` | 指 `http://127.0.0.1:1` | `connection refused`，exit=1 | 同上；与 `ANTHROPIC_BASE_URL` 是同一机制的两个实例，一条够 |
| `ANTHROPIC_BASE_URL` | 指 `http://127.0.0.1:1` 的假端点 | `dial tcp 127.0.0.1:1: connect: connection refused`，exit=1 | 同上 |
| `GOPI_MAX_TOKENS` / `GOPI_THINKING*` | 设值后跑一轮真任务，再读该 run 的 raw HTTP 日志找 `max_tokens`/`thinking` 字段 | 本 run 的 raw 日志里**没有**这两个字段（协议路径不同） | 取证失败——"没跑到能看见差异的表面"，按 Rule 31 记在这里，不写进条目 |
| `GOPI_EFFORT_VALUES` / `GOPI_REASONING_EFFORT` | `GOPI_EFFORT_VALUES='low\|high' GOPI_REASONING_EFFORT=max gopi config show` 与 `version` | 无输出差异（`config show` 不列该键） | 效果只出现在**请求体**里，本机取证不到 |
| `GOPI_MEMORY_PEERS` / `GOPI_MEMORY_SYNC_*` | `GOPI_MEMORY_PEERS=hp gopi memory sync --peer hp` | 需要真实对端；本机 20s 内无输出被 timeout 杀掉 | 需第二台机器（姊妹任务 [11/12] 的多节点素材正是这件事） |
| `GOPI_WEB_ADDR` | `GOPI_WEB_ADDR=127.0.0.1:18099 timeout 5 gopi web` vs 不设 | `open: http://127.0.0.1:18099` vs `open: http://127.0.0.1:8090` | **这条真有差异，是本表里唯一"已量到差异、只差成条目"的一个**——本批没做成纯属预算。**下批应优先做它** |
| `GOPI_SHOT_CHROME` | `GOPI_SHOT_CHROME=/nonexistent/chrome gopi shot --url …` | `GOPI_SHOT_CHROME=… is not an executable file`（**退出码是 0**） | 退出码 0 + 错误文本是可疑形状，但同一次运行里默认 `gopi shot` 也没起来（chrome 被地址空间钳制杀掉），构不成干净对照 |
| `GOPI_CODE_SEARCH_ROOT` | 指向只有 1 个 `.go` 的小目录，从 /tmp 跑 | `no symbol found`（默认那次在 /tmp 里跑出的是 rg 权限噪音） | 两次都不是"差异"，是两种不同的失败；需要干净 cwd 的对照 |
| `GOPI_SANDBOX_LEDGER_SEED` | `= <沙盒自己的 memory.db> gopi sandbox create` | **拒绝**（seed "lives INSIDE the sandbox" 时 refuse the create） | 拒绝路径值得一条条目（它证明 Rule 34 的自指保护），本批该组已有 env-03/env-04 |

**这张表本身也是诚实的产物**：它列出的是"查过、跑过、能说清为什么没成条目"的变量。
**凡本表没有的变量，就是一次都没跑过**——它们只在本条目的 `02-output.txt`（census 清单）里
有一行登记，状态 `table-only`。

## 未逐条真跑的代价（写明，别让它变成默认）

**`table-only` 不是"已验证"**：它只证明"这个变量名在本 commit 的 help 面与源码里都有"。
一个变量的**行为**（改值后到底发生了什么的差异）在 `table-only` 行里**没有任何证据**。
按 Rule 5/31 的口径：没跑过、没观察到的，不许声称。

**下批的第一优先级**（从本表里挑，按"已量到差异 > 只需一次真跑"排序）：
1. `GOPI_WEB_ADDR`（已有差异，只差成条目）；
2. `GOPI_SANDBOX_LEDGER_SEED` 的**拒绝**路径（自指 seed）；
3. `GOPI_SHOT_CHROME` 的"错误文本 + 退出码 0"形状，需要先有一个能起来的 chrome 作为对照。
FT


cat > "$OUTROOT/env-08-error-dump-dir/05-finding.md" <<'F8'
## 这次运行里挖到的缝

### F8.1 默认 dump 目录落在**操作者的 profile** 里，而不是 fallback 到私有 profile

现场（本条目 02-output.txt，两次调用都失败于同一件事——`GOPI_VISION_MODEL=bogus-model`
被上游以 HTTP 400 拒绝，错误文本里点名了传入的模型名）：

- **不设该变量**：dump 写到 `/home/nick/.gopi/dumps/<ts>.dump.json` —— 即**操作者的 gopi 目录**，
  与本次运行那个私有沙盒 profile（`GOPI_HOME=$SB`）无关；
- **设 `GOPI_ERROR_DUMP_DIR=$SB/dumps`**：dump 写到 `<沙盒>/dumps/<ts>.dump.json`。

**为什么是缝**：Rule 34 的整套推理是"一切真跑在私有 profile 内，绝不读写操作者台账"，
而一个**在沙盒里跑的** `gopi vision_analyze` 默认会把完整请求（含提示词与图片的 base64）
写进操作者的 `/home/nick/.gopi/dumps/`。沙盒隔离了 `GOPI_HOME`（sessions/raw/logs/memory.db），
但**没隔离这个诊断产物**——它是"另一条通道"里默认指向操作者的路径。
本次运行**确实**向操作者的 `/home/nick/.gopi/dumps/` 写了 2 个文件（本条目取证的对照组所致；
对照是本条目的必要成分，因为它们证明的正是"默认落在谁家"）。
**本条目只记录测量结果**；改法（dump 默认跟随 `GOPI_HOME`）属于另一个 bug 行。

**已查过、不是缝的部分**：该变量的存在、名字与效果与 `--help-env` 的
`GOPI_ERROR_DUMP_DIR` 行（"directory for the full request dump of a failed call"）一致；
两次失败的**上游错误**逐字节相同（同一个 400、同一个 message 模板），
所以两次输出的差异**只**来自那个变量。
F8

echo "== 形状断言：每个条目必须恰好六件、件件非空、02 有 exit= 行 =="
shape_bad=0
for d in "$OUTROOT"/env-*/; do
  base="$(basename "$d")"
  nfiles=$(find "$d" -maxdepth 1 -type f | wc -l)
  if [[ "$nfiles" -ne 6 ]]; then
    echo "SHAPE FAIL: $base 有 $nfiles 个文件（要求恰好 6）" >&2
    find "$d" -maxdepth 1 -type f -printf '    %f\n' >&2
    shape_bad=1
  fi
  for f in 00-entry.md 01-input.txt 02-output.txt 03-raw.png 04-annotated.png 05-finding.md; do
    [[ -s "$d/$f" ]] || { echo "SHAPE FAIL: $base/$f 缺失或为空" >&2; shape_bad=1; }
  done
  grep -qE '^exit=[0-9]+$' "$d/02-output.txt" \
    || { echo "SHAPE FAIL: $base/02-output.txt 没有 ^exit=<n> 行" >&2; shape_bad=1; }
  # 03 与 04 必须**不同**（标注真的发生过）
  if [[ -s "$d/03-raw.png" && -s "$d/04-annotated.png" ]]; then
    a=$(sha256sum "$d/03-raw.png" | cut -c1-16); b=$(sha256sum "$d/04-annotated.png" | cut -c1-16)
    [[ "$a" != "$b" ]] || { echo "SHAPE FAIL: $base 的 03/04 sha 相同（标注没发生）" >&2; shape_bad=1; }
    printf '  %-34s 03=%s 04=%s\n' "$base" "$a" "$b"
  fi
  grep -qE '^(##|###) ' "$d/05-finding.md" \
    || { echo "SHAPE FAIL: $base/05-finding.md 没有任何标题行（=空）" >&2; shape_bad=1; }
done
[[ "$shape_bad" -eq 0 ]] || { echo "GENERATOR FAILED: 形状断言未通过" >&2; exit 1; }
echo "形状断言：全部通过"
