#!/usr/bin/env python3
"""rerender_entry.py — 只重画一条素材的 03/04（不重跑命令、不碰 01/02 一个字节）。

为什么能做（2026-09-30 实测）：`render.py` 的输入其实**全部可从已在仓库里的东西重建**：
  * 图顶解说 = `<dir>/00-entry.md` 的「## 标注图在指什么」正文（逐字）；
  * 三段分隔行 = 生成器脚本里的**三个 printf 字面量**（`gen-*.sh`，各面逐字相同）；
  * 正文 = `<dir>/01-input.txt` + `<dir>/02-output.txt`（证据，只读）；
  * `--mark-match` 规格 = 生成器 `entry …` 调用里含 `::` 的那个引号参数。
所以「渲染输入没随素材留档」不再阻塞重画 —— 生成器脚本本身就是那份存档。

faithful-reconstruction 门（唯一可信度来源）
    重画的 `03-raw.png` 必须与盘上现存的**逐字节相同**（sha256）。
    不同 = 输入重建没做对 ⇒ **拒绝写任何东西**，报 SKIP 退出非零，把这条留给人工。
    相同 ⇒ 03 不动（它已经是对的），只把 **04-annotated.png** 换成新渲染器的输出。

尺寸守卫：新 04 的宽高必须与盘上 03/04 一致（"图像尺寸不是缺陷"，改尺寸会让下游
坐标说明全错）。

用法
    python3 scripts/materials/rerender_entry.py --dir docs/archive/demo/materials/002-task-submit \\
        --generator scripts/materials/gen-taskA.sh \\
        --bin /usr/local/bin/gopi --font /usr/share/fonts/truetype/wqy/wqy-zenhei.ttc \\
        --scratch <off-/ 目录>
"""
import argparse
import codecs
import hashlib
import os
import re
import struct
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
RENDER = os.path.join(HERE, "render.py")
HEAD_KEY = "04-annotated.png 要说明什么"
IN_KEY = "01-input.txt（verbatim"
OUT_KEY = "02-output.txt（verbatim"


def sha(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()


def png_size(path):
    with open(path, "rb") as f:
        return struct.unpack(">II", f.read(24)[16:24])


def _unesc(s):
    """只还原生成器 printf 里真的用到的转义。

    不用 codecs.decode(s,"unicode_escape")：它会把非 ASCII（这里是中文）按 latin-1
    重新编码，中文字面量会被打乱（第一次实现踩到，全部面报 SKIP）。
    """
    return s.replace("\\n", "\n").replace("\\t", "\t").replace("\\'", "'")


def printf_literals(gen):
    """生成器里三个分隔行字面量（逐字），按内容定位；`\\n` 还原成真换行。"""
    out = {}
    for line in open(gen, encoding="utf-8").read().split("\n"):
        m = re.search(r"printf ['\"](.*?)(?:['\"]|$)", line)
        if not m:
            continue
        body = _unesc(m.group(1))
        for key, tag in ((HEAD_KEY, "head"), (IN_KEY, "in"), (OUT_KEY, "out")):
            if key in body:
                out[tag] = body.replace("%s", "")
    missing = [k for k in ("head", "in", "out") if k not in out]
    if missing:
        return None, "生成器里找不到分隔行字面量 %s（%s）" % (missing, gen)
    return out, None


def caption(d):
    t = open(os.path.join(d, "00-entry.md"), encoding="utf-8").read()
    marker = "## 标注图在指什么"
    if marker not in t:
        return None, "00-entry.md 里没有「## 标注图在指什么」"
    rest = t[t.index(marker) + len(marker):]
    j = rest.find("\n## ")
    return (rest[:j] if j >= 0 else rest).strip("\n"), None


def _tokenize(s):
    """按 shell 引号规则切一个逻辑行（拼接了续航行之后）。"""
    toks, cur, q = [], "", None
    i = 0
    while i < len(s):
        c = s[i]
        if q:
            if c == q:
                q = None
            elif c == "\\" and q == '"' and i + 1 < len(s):
                cur += s[i + 1]
                i += 1
            else:
                cur += c
        elif c in "'\"":
            q = c
        elif c.isspace():
            if cur:
                toks.append(cur)
                cur = ""
        else:
            cur += c
        i += 1
    if cur:
        toks.append(cur)
    return toks


def marks_for(gen, slug):
    """生成器里 slug 那一条的 marks 规格（`entry` 调用的第 8 个参数）。

    语法面：续航行先拼成逻辑行，再按引号切词——marks 可能是字面量（gen-taskA 形），
    也可能写成 `"$MARKS_TABLE"`（gen-envB 形），后者去同一文件里把变量取回来。
    """
    text = open(gen, encoding="utf-8").read()
    # 续航行拼接
    logical, buf = [], ""
    for line in text.split("\n"):
        buf += line
        if buf.rstrip().endswith("\\"):
            buf = buf.rstrip()[:-1]
            continue
        logical.append(buf)
        buf = ""
    if buf:
        logical.append(buf)

    vars_ = dict(re.findall(r"^([A-Z_][A-Z0-9_]*)=\"?([^\"\n]*)\"?\s*$", text, re.M))
    for line in logical:
        toks = _tokenize(line)
        if not toks or toks[0] not in ("entry", "entry_t", "entry_env", "mk"):
            continue
        if len(toks) < 9 or toks[2] != slug:
            continue
        spec = toks[8]
        if spec.startswith("$"):
            spec = vars_.get(spec[1:], "")
        if "::" in spec or ":" in spec:
            pairs = []
            for one in spec.split(";;"):
                needle, _, label = one.partition(":")
                pairs.append((needle, label))
            return pairs, None
    return None, "生成器里找不到 slug=%s 的 marks 规格" % slug


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", required=True)
    ap.add_argument("--generator", required=True)
    ap.add_argument("--bin", required=True)
    ap.add_argument("--font", required=True)
    ap.add_argument("--scratch", required=True)
    ap.add_argument("--apply", action="store_true",
                    help="通过 reconstruction 门后**真的**换掉 04（缺省只演练，不写盘）")
    a = ap.parse_args()

    d = a.dir.rstrip("/")
    slug = os.path.basename(d).split("-", 1)[-1] if os.path.basename(d)[:1].isdigit() \
        else os.path.basename(d)
    raw, ann = os.path.join(d, "03-raw.png"), os.path.join(d, "04-annotated.png")
    if not (os.path.exists(raw) and os.path.exists(ann)):
        sys.stderr.write("SKIP %s: 盘上没有 03/04 成对（这是新条目，不是重画）\n" % d)
        return 3

    lits, why = printf_literals(a.generator)
    if why:
        sys.stderr.write("SKIP %s: %s\n" % (d, why))
        return 3
    cap, why = caption(d)
    if why:
        sys.stderr.write("SKIP %s: %s\n" % (d, why))
        return 3
    pairs, why = marks_for(a.generator, slug)
    if why:
        sys.stderr.write("SKIP %s: %s\n" % (d, why))
        return 3

    body = (lits["head"] + cap + "\n\n" + lits["in"]
            + open(os.path.join(d, "01-input.txt"), encoding="utf-8").read()
            + lits["out"]
            + open(os.path.join(d, "02-output.txt"), encoding="utf-8").read())

    os.makedirs(a.scratch, exist_ok=True)
    tmp_raw = os.path.join(a.scratch, "_check.03.png")
    tmp_ann = os.path.join(a.scratch, "_check.04.png")
    # --legacy-layout：老素材的图**没有**标签净空、没有文末补行，版式与 render.py 现在
    # 的默认版式不同；原样重建才可能让 03 逐字节相同（这正是"输入重建"过去失败的原因）。
    cmd = [sys.executable, RENDER, "--raw", tmp_raw, "--ann", tmp_ann,
           "--font", a.font, "--bin", a.bin, "--scratch", a.scratch,
           "--legacy-layout", "--json-out", os.path.join(a.scratch, "_check.json")]
    for needle, label in pairs:
        cmd += ["--mark-match", "%s:%s" % (needle, label)]
    p = subprocess.run(cmd, input=body, capture_output=True, text=True)
    if p.returncode != 0 or not os.path.exists(tmp_ann):
        sys.stderr.write("FAIL %s: render rc=%d\n%s\n" % (d, p.returncode, p.stderr[-500:]))
        return 2

    # --- faithful-reconstruction 门：03 必须逐字节相同，否则一律不写 ---------
    if sha(tmp_raw) != sha(raw):
        sys.stderr.write("SKIP %s: 重建的 03-raw 与盘上不同（%s vs %s）——重建没做对，"
                         "拒绝写盘\n" % (d, sha(tmp_raw)[:16], sha(raw)[:16]))
        return 3
    if png_size(tmp_ann) != png_size(ann):
        sys.stderr.write("SKIP %s: 新 04 尺寸 %s != 盘上 %s（尺寸不是缺陷）\n"
                         % (d, png_size(tmp_ann), png_size(ann)))
        return 3

    print("OK %s  03-raw 重建逐字节相同 (%s); 04-annotated 新 sha=%s"
          % (d, sha(raw)[:16], sha(tmp_ann)[:16]))
    if a.apply:
        os.replace(tmp_ann, ann)
        print("   applied: %s 已替换 (sha=%s)" % (ann, sha(ann)[:16]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
