#!/usr/bin/env python3
"""ASR 准确率评估 —— 对比 whisper 转录与人工正确答案。

为什么要自己写：nick 给的样本是中文有声书（含 AI/agent 技术词汇），
而 whisper 的中文失败模式与英文完全不同——不是"漏词"，而是**同音字
替换**（续章→叙章、硅基→规矩、尔虞我诈→耳语我炸）。所以必须看：
  1. 字符级准确率（CER）—— 整体质量
  2. 同音字错误占比 —— 中文 ASR 的特征失败模式
  3. 拉丁词保留率 —— AGENT/RAG/Prompt 这类技术词是否幸存
     （这是 nick 最关心的"技术词汇准确率"）

归一化：去标点/空白（whisper 输出用空格分词，正确答案用标点），
否则标点差异会淹没真实的识别错误。
"""
import argparse
import difflib
import re
import sys
import unicodedata
from collections import Counter

PUNCT = re.compile(r"[\s，。、！？；：""''（）《》〈〉「」『』…—─,.!?;:\"'()\[\]<>·~*#@$%^&_+={}|\\/]")
LATIN = re.compile(r"[A-Za-z][A-Za-z0-9]*")
CJK = re.compile(r"[\u4e00-\u9fff]")


def norm(s: str) -> str:
    s = unicodedata.normalize("NFKC", s)
    s = PUNCT.sub("", s)
    return s.lower()


def levenshtein(a: str, b: str) -> int:
    if len(a) < len(b):
        a, b = b, a
    if not b:
        return len(a)
    prev = list(range(len(b) + 1))
    for i, ca in enumerate(a, 1):
        cur = [i]
        for j, cb in enumerate(b, 1):
            cur.append(min(prev[j] + 1, cur[j - 1] + 1,
                           prev[j - 1] + (ca != cb)))
        prev = cur
    return prev[-1]


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description="Score a whisper transcript vs a reference")
    ap.add_argument("hyp", help="whisper output file")
    ap.add_argument("ref", help="reference (correct) transcript")
    args = ap.parse_args(argv)

    hyp_raw = open(args.hyp, encoding="utf-8", errors="replace").read()
    ref_raw = open(args.ref, encoding="utf-8", errors="replace").read()
    h, r = norm(hyp_raw), norm(ref_raw)

    dist = levenshtein(h, r)
    cer = dist / max(1, len(r))

    print("=" * 70)
    print(f"{'hypothesis':<16}{args.hyp}")
    print(f"{'reference':<16}{args.ref}")
    print("=" * 70)
    print(f"  chars (norm)      hyp={len(h):,}  ref={len(r):,}")
    print(f"  edit distance     {dist:,}")
    print(f"  CER (char error)  {cer*100:.2f}%")
    print(f"  accuracy          {(1-cer)*100:.2f}%")

    # --- 拉丁词（技术词汇）保留率 -----------------------------------------
    ref_lat = LATIN.findall(ref_raw)
    hyp_lat = LATIN.findall(hyp_raw)
    rc = Counter(w.lower() for w in ref_lat)
    hc = Counter(w.lower() for w in hyp_lat)
    kept = sum((rc & hc).values())
    print()
    print("  --- latin/tech terms ---")
    print(f"  in reference      {sum(rc.values())} ({len(rc)} distinct)")
    print(f"  in hypothesis     {sum(hc.values())} ({len(hc)} distinct)")
    print(f"  kept              {kept}/{sum(rc.values())} "
          f"({kept/max(1,sum(rc.values()))*100:.0f}%)")
    missing = [w for w in rc if w not in hc]
    if missing:
        print(f"  MISSING           {missing[:15]}")
    spurious = [w for w in hc if w not in rc]
    if spurious:
        print(f"  SPURIOUS          {spurious[:15]}")

    # --- 中文错误样本（difflib 对齐出的差异片段）--------------------------
    print()
    print("  --- sample divergences (first 20) ---")
    sm = difflib.SequenceMatcher(None, r, h, autojunk=False)
    shown = 0
    for tag, i1, i2, j1, j2 in sm.get_opcodes():
        if tag == "equal":
            continue
        print(f"    {tag:<8} REF{r[i1:i2]!r:<16} HYP{h[j1:j2]!r}")
        shown += 1
        if shown >= 20:
            break
    return 0


if __name__ == "__main__":
    sys.exit(main())
