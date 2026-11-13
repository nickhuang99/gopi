#!/usr/bin/env python3
"""Mechanical check: every argparse-based script in scripts/ must accept argv.

Why this exists: I hit "main() takes 0 positional arguments" FOUR times while
writing tests for these scripts. Each time I fixed the one file in front of me,
which is exactly the fix that guarantees a fifth occurrence. The rule is not
"remember to do this" — it is a property you can grep for.

A script whose main() calls argparse.parse_args() without threading an argv
through cannot be driven from a test, so it cannot have a real test.
"""
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).resolve().parent


def main():
    offenders = []
    exempted = []
    for p in sorted(HERE.glob("*.py")):
        if p.name.startswith("test_") or p.name.startswith("_"):
            continue
        src = p.read_text()
        # 显式豁免：脚本头部用 "# test-drivable: no <reason>" 声明一次性的
        # 分析脚本。理由必须写出来——空豁免不接受。
        m = re.search(r"^# test-drivable: no\s+(\S.*)$", src, re.M)
        if m and len(m.group(1).strip()) >= 20:
            exempted.append(p.name)
            continue
        has_argparse = "argparse.ArgumentParser" in src
        # A script that reads sys.argv directly is WORSE than one with a
        # broken argparse signature: it cannot be driven from a test at all,
        # and it silently bypassed the first version of this check (which only
        # looked at argparse scripts). Catch both shapes.
        # A guarded fallback like `argv if argv is not None else sys.argv[1:]`
        # is CORRECT (server-style default) — only an UNGUARDED read at module
        # scope or inside main() is a defect.
        guarded = re.search(r"if argv is None else", src)
        reads_argv = (None if guarded
                      else re.search(r"^\s*\w+ = sys\.argv\[", src, re.M))

        if reads_argv:
            offenders.append(f"{p.name} (reads sys.argv — use argparse, or declare "
                            f"'# test-drivable: no <reason>')")
            continue
        if not has_argparse:
            continue
        declares = re.search(r"^def main\(argv=None\)", src, re.M)
        passes = re.search(r"parse_args\(argv\)", src)
        if not (declares and passes):
            offenders.append(p.name)

    if exempted:
        print(f"exempted by explicit declaration ({len(exempted)}): "
              + ", ".join(exempted))

    if offenders:
        print("FAIL: CLI scripts that cannot be driven from a test:")
        for o in offenders:
            print(f"  scripts/{o}  -> def main(argv=None) + parse_args(argv)")
        return 1
    n = sum(1 for p in HERE.glob("*.py")
            if ("argparse.ArgumentParser" in p.read_text()
                or re.search(r"^\s*\w+ = sys\.argv\[", p.read_text(), re.M))
            and not p.name.startswith("test_"))
    print(f"test_script_main_argv: {n} CLI script(s), all drivable from a test — PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
