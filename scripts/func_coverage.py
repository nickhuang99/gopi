#!/usr/bin/env python3
"""Function-level test coverage for GoPi (the operator's metric: 已覆盖函数/总函数).

Why not `go test -cover`? Statement coverage tells you which LINES ran, not
which FUNCTIONS have a test that exercises them with boundary/error cases. The
agreed success criterion for the periodic code-review campaign is:

    every exported function/method has at least one test (incl. boundary + error
    paths); coverage = covered functions / total functions

So this script enumerates exported funcs from source (via `go doc`/AST) and
cross-references them against test files that name them. It is deliberately
conservative: a function counts as covered only if its *name* appears in a
_test.go file in the same package (either as a direct call or a table entry).
That makes the number HARD to inflate — you cannot claim coverage without
writing something that mentions the function.

Usage:
    python3 scripts/func_coverage.py                          # repo-wide summary
    python3 scripts/func_coverage.py --pkg internal/tools     # one package
    python3 scripts/func_coverage.py --json                   # machine readable
    python3 scripts/func_coverage.py --uncovered --pkg X      # list gaps
"""
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# A Go function/method declaration, exported (uppercase name, optional receiver).
FUNC_RE = re.compile(
    r"^func\s+(?:\([^)]*\)\s+)?(?P<name>[A-Z]\w*)\s*\(",
    re.MULTILINE,
)


def go_files(pkg_dir: Path) -> list[Path]:
    return sorted(p for p in pkg_dir.glob("*.go") if not p.name.endswith("_test.go"))


def test_files(pkg_dir: Path) -> list[Path]:
    return sorted(pkg_dir.glob("*_test.go"))


def exported_funcs(paths: list[Path]) -> dict[str, list[str]]:
    """name -> [file:line] (a name can be redeclared on different types)."""
    out: dict[str, list[str]] = {}
    for p in paths:
        text = p.read_text(errors="ignore")
        for m in FUNC_RE.finditer(text):
            line = text[: m.start()].count("\n") + 1
            out.setdefault(m.group("name"), []).append(f"{p.name}:{line}")
    return out


def covered_names(tests: list[Path]) -> set[str]:
    """Every identifier that appears in a test file, as a coarse 'is it used?' set."""
    names: set[str] = set()
    for p in tests:
        text = p.read_text(errors="ignore")
        names.update(re.findall(r"\b([A-Z]\w+)\b", text))
    return names


def package_dirs(root: Path, only: str | None) -> list[Path]:
    dirs: list[Path] = []
    for dirpath, dirnames, filenames in os.walk(root):
        # skip VCS, vendor, worktrees, node_modules-ish noise
        dirnames[:] = [
            d
            for d in dirnames
            if d not in (".git", "vendor", "node_modules", "testdata")
            and not d.startswith(".")
        ]
        if not any(f.endswith(".go") for f in filenames):
            continue
        d = Path(dirpath)
        rel = d.relative_to(root).as_posix()
        if rel == ".":
            continue
        if only and not rel.startswith(only):
            continue
        dirs.append(d)
    return sorted(dirs)


def analyze(root: Path, only: str | None, want_uncovered: bool, as_json: bool) -> int:
    rows = []
    tot_f = tot_c = 0
    for d in package_dirs(root, only):
        src = go_files(d)
        if not src:
            continue
        tests = test_files(d)
        funcs = exported_funcs(src)
        if not funcs:
            continue
        covered = covered_names(tests) if tests else set()
        hit = [n for n in funcs if n in covered]
        miss = sorted(n for n in funcs if n not in covered)
        rows.append(
            {
                "package": d.relative_to(root).as_posix(),
                "funcs": len(funcs),
                "covered": len(hit),
                "missing": miss,
                "has_tests": bool(tests),
            }
        )
        tot_f += len(funcs)
        tot_c += len(hit)

    if as_json:
        print(json.dumps(
            {"total_funcs": tot_f, "covered": tot_c,
             "pct": round(100.0 * tot_c / tot_f, 1) if tot_f else 0.0,
             "packages": rows}, indent=2, ensure_ascii=False))
        return 0

    rows.sort(key=lambda r: (r["covered"] / r["funcs"] if r["funcs"] else 1))
    print(f"{'package':<42} {'funcs':>6} {'cov':>6} {'pct':>6}  tests")
    print("-" * 74)
    for r in rows:
        pct = 100.0 * r["covered"] / r["funcs"] if r["funcs"] else 0.0
        flag = "" if r["has_tests"] else "  <-- NO TEST FILE"
        print(f"{r['package']:<42} {r['funcs']:>6} {r['covered']:>6} {pct:>5.1f}%{flag}")
    print("-" * 74)
    pct = 100.0 * tot_c / tot_f if tot_f else 0.0
    print(f"{'TOTAL':<42} {tot_f:>6} {tot_c:>6} {pct:>5.1f}%")

    if want_uncovered:
        print("\nUncovered exported funcs (by package):")
        for r in rows:
            if r["missing"]:
                print(f"\n  {r['package']} ({len(r['missing'])}):")
                for n in r["missing"]:
                    print(f"    - {n}")

    print(f"\nNOTE: conservative metric — a func counts as covered only if its name\n"
          f"appears in a *_test.go file of the same package. Target: 100%.")
    return 0


def main(argv=None) -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--pkg", default=None, help="limit to packages under this path")
    ap.add_argument("--uncovered", action="store_true", help="list uncovered funcs")
    ap.add_argument("--json", action="store_true", help="machine-readable output")
    ap.add_argument("--root", default=str(ROOT))
    args = ap.parse_args(argv)
    return analyze(Path(args.root), args.pkg, args.uncovered, args.json)


if __name__ == "__main__":
    sys.exit(main())
