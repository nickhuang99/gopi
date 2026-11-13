#!/usr/bin/env python3
"""Produce the differential corpus the Go tokenizer test compares against.

The repo's test reads a JSON *array* of {"name","text","ids"} from the path in
$GOPI_TOKENIZER_CASELOAD, and compares the WHOLE id sequence (not the count).
The reference encoder is HuggingFace `tokenizers` loading the very same
`internal/tokenizer/tokenizer.json` the Go code embeds, so an id mismatch is a
real disagreement about the same artifact.

Classes required by TestCorpusCoversTheRequiredClasses: CJK, emoji (astral
plane), Go/JSON code, added special tokens, and a digit run that the \p{N}{1,3}
stage splits into more than one id.
"""
import json
import sys

from tokenizers import Tokenizer

TK = "internal/tokenizer/tokenizer.json"

CASES = [
    ("empty", ""),
    ("ascii-and-punct", "Hello, world! How are you?"),
    ("cjk-zh", "这是一个中文测试句子，用来验证分词是否与参考实现一致。"),
    ("cjk-mixed", "gopi 的 tokenizer 现在能精确计数了吗？可以。"),
    ("emoji-astral", "good job 🎉🚀🔥 well done"),
    ("go-code", "func (t *Tokenizer) Count(text string) int { return len(t.Encode(text)) }"),
    ("json-code", '{"model":"deepseek-flash","max_tokens":16384}'),
    ("digit-run", "1234567890"),
    ("long-digits", "2026-11-02 05:10:07 and 12043104 tokens"),
    ("special-bos", "<｜begin▁of▁sentence｜>hello"),
    ("special-eos", "hello<｜end▁of▁sentence｜>"),
    ("whitespace", "  \t\n  multiple   spaces\n\n"),
    ("newlines", "line one\nline two\r\nline three\n"),
    ("url", "https://github.com/nick/gopi/blob/master/internal/tokenizer/pre.go"),
    ("path", "/home/nick/.gopi/sessions/e1e22e9f/session.jsonl"),
    ("cyrillic", "Привет, мир! Тестирование токенизатора."),
    ("japanese", "これは日本語のテストです。"),
    ("korean", "이것은 한국어 테스트입니다."),
    ("accented", "café naïve résumé Größe"),
    ("math", "∑x² + √y − π ≈ 3.14159"),
    ("mixed-long", "The quick brown fox 跳过 the lazy dog 1234567890 🎉 func main() {}"),
]


def main():
    tk = Tokenizer.from_file(TK)
    out = []
    for name, text in CASES:
        ids = tk.encode(text, add_special_tokens=False).ids
        out.append({"name": name, "text": text, "ids": ids})
        print("%-18s %3d ids  %r" % (name, len(ids), text[:48]))
    path = sys.argv[1] if len(sys.argv) > 1 else "/tmp/tk_corpus.json"
    with open(path, "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False, indent=1)
    print("\nwrote %s (%d cases)" % (path, len(out)))


if __name__ == "__main__":
    main()
