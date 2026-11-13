#!/usr/bin/env python3
"""Functional test for scripts/asr_score.py — no network, no models.

The scorer is what turns "the transcript looks bad" into a number we can
compare models with, so its arithmetic has to be checkable by hand. The
specific things that would silently mislead a model-selection decision:

  1. Normalisation must strip punctuation and whitespace, because whisper
     emits space-separated tokens while a human reference is punctuated.
     Without that, punctuation differences dominate and every model looks
     equally wrong.
  2. CER is edits/reference-length, not edits/longer-length. Using the wrong
     denominator biases short hypotheses favourably.
  3. Latin-term retention must count DISTINCT terms against the reference,
     so dropping RAG entirely is visible even if the word appears 10 times.
  4. NFKC folding matters: whisper sometimes emits full-width or composed
     forms that must not be scored as errors.
"""
import importlib.util
import io
import os
import sys
from contextlib import redirect_stdout

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "asr_score.py")


def load():
    spec = importlib.util.spec_from_file_location("asr_score", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def run(mod, hyp_text, ref_text):
    d = make_scratch_dir("asr-score-test")
    hp, rp = os.path.join(d, "h.txt"), os.path.join(d, "r.txt")
    open(hp, "w", encoding="utf-8").write(hyp_text)
    open(rp, "w", encoding="utf-8").write(ref_text)
    buf = io.StringIO()
    with redirect_stdout(buf):
        code = mod.main([hp, rp])
    return code, buf.getvalue()


def main():
    mod = load()
    checks = 0

    # --- 1. normalisation removes punctuation AND whitespace --------------
    assert mod.norm("你好，世界。") == "你好世界", mod.norm("你好，世界。")
    # whisper style: space-separated tokens, no punctuation
    assert mod.norm("你好 世界") == "你好世界", mod.norm("你好 世界")
    checks += 1

    # --- 2. NFKC folding: full-width latin folds to ascii -----------------
    assert mod.norm("ＡＧＥＮＴ") == "agent", mod.norm("ＡＧＥＮＴ")
    checks += 1

    # --- 3. levenshtein, hand-checked -------------------------------------
    assert mod.levenshtein("abc", "abc") == 0
    assert mod.levenshtein("abc", "abd") == 1
    assert mod.levenshtein("abc", "ab") == 1
    assert mod.levenshtein("", "abc") == 3
    assert mod.levenshtein("硅基", "规矩") == 2, "both chars differ"
    checks += 1

    # --- 4. identical files score 0% CER ----------------------------------
    code, out = run(mod, "你好世界", "你好世界")
    assert code == 0, code
    assert "0.00%" in out, out
    assert "100.00%" in out, out
    checks += 1

    # --- 5. punctuation-only difference must NOT count as error -----------
    # (this is the trap: whisper never emits the reference's punctuation)
    code, out = run(mod, "你好世界", "你好，世界。")
    assert "0.00%" in out, f"punctuation must not score as error:\n{out}"
    checks += 1

    # --- 6. CER denominator is the REFERENCE length -----------------------
    # ref=4 chars, 2 edits -> 50%, regardless of hypothesis length
    code, out = run(mod, "abcd", "abXY")
    assert "50.00%" in out, f"expected 50% CER:\n{out}"
    checks += 1

    # --- 7. latin retention: a dropped tech term is visible ---------------
    code, out = run(mod, "use Rug for context", "use RAG for context")
    assert "MISSING" in out and "rag" in out.lower(), out
    assert "SPURIOUS" in out and "rug" in out.lower(), out
    checks += 1

    # --- 8. latin retention counts distinct terms -------------------------
    # 'agent' appears 3x in ref and 3x in hyp -> fully kept
    code, out = run(mod, "agent agent agent", "agent agent agent")
    assert "3/3" in out, f"retention should be 3/3:\n{out}"
    checks += 1

    # --- 9. divergence list is produced when there ARE differences --------
    code, out = run(mod, "驯服了语言", "兴奋了语言")
    assert "replace" in out, f"divergences must be reported:\n{out}"
    checks += 1

    # --- 10. output names the accuracy explicitly (that is the decision
    #         number a human compares across models) ----------------------
    assert "accuracy" in out.lower(), out
    checks += 1

    print(f"test_asr_score: {checks} checks PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
