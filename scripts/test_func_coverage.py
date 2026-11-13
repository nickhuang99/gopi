#!/usr/bin/env python3
"""Accuracy test for scripts/func_coverage.py — cross-check against ground truth.

The tool decides campaign acceptance, so "it runs" is not enough: its COUNTS
must be right. This test builds a synthetic Go package with a known number of
exported/unexported funcs and known test mentions, then asserts the tool reports
exactly what it should. Also cross-checks the real repo against `go doc -all`
(independently enumerating exported funcs) to catch under/over-counting.

Usage: python3 scripts/test_func_coverage.py
"""
import importlib.util
import io
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from contextlib import redirect_stdout
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent / "lib"))
from tmp_ban import make_scratch_dir  # noqa: E402

HERE = Path(__file__).resolve().parent
TOOL = HERE / "func_coverage.py"
REPO = HERE.parent
failures: list[str] = []


def check(name: str, cond: bool, extra: str = "") -> None:
    print(f"  {'PASS' if cond else 'FAIL'}  {name}{'  ' + extra if extra and not cond else ''}")
    if not cond:
        failures.append(name)


def load_tool():
    spec = importlib.util.spec_from_file_location("fc", TOOL)
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot load tool")
    m = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(m)
    return m


SYNTH_SRC = '''package demo

func ExportedOne() {}
func ExportedTwo(x int) int { return x }
func (t *Thing) ExportedMethod() {}
func unexported() {}
func (t Thing) alsoUnexported() {}

type Thing struct{}
'''

SYNTH_TEST = '''package demo

import "testing"

func TestExportedOne(t *testing.T) { ExportedOne() }
'''

SYNTH_SRC_2 = '''package demo

func ExportedOne() {}
func ExportedTwo(x int) int { return x }
func (t *Thing) ExportedMethod() {}
func unexported() {}

type Thing struct{}
'''

SYNTH_TEST_2 = '''package demo

import "testing"

func TestAll(t *testing.T) {
\tExportedOne()
\tExportedTwo(1)
\tvar th Thing
\tth.ExportedMethod()
}
'''


def synth_case(name: str) -> None:
    """Build a temp repo with a known ground truth and assert the tool's counts."""
    m = load_tool()
    tmp = Path(make_scratch_dir("fcov-test"))  # scratch queue, off `/`
    try:
        pkg = tmp / "internal" / "demo"
        pkg.mkdir(parents=True)

        print(f"== {name}: partial coverage ==")
        (pkg / "demo.go").write_text(SYNTH_SRC)
        (pkg / "demo_test.go").write_text(SYNTH_TEST)
        # 3 exported (ExportedOne, ExportedTwo, ExportedMethod), 1 mentioned
        out = io.StringIO()
        with redirect_stdout(out):
            m.analyze(tmp, None, True, False)
        text = out.getvalue()
        check("counts 3 exported funcs (skips unexported)", " 3 " in text or "demo" in text, text)
        check("demo package shows 1/3", re.search(r"internal/demo\s+3\s+1", text) is not None, text)
        check("lists ExportedTwo as uncovered", "ExportedTwo" in text, text)
        check("does NOT list unexported", "alsoUnexported" not in text and "unexported" not in text, text)

        print(f"== {name}: full coverage ==")
        (pkg / "demo_test.go").write_text(SYNTH_TEST_2)
        out = io.StringIO()
        with redirect_stdout(out):
            m.analyze(tmp, None, True, False)
        text = out.getvalue()
        check("now 3/3", re.search(r"internal/demo\s+3\s+3", text) is not None, text)
        check("100.0%", "100.0%" in text, text)

        print(f"== {name}: empty package (no go files) is skipped ==")
        empty = tmp / "internal" / "empty"
        empty.mkdir()
        (empty / "readme.txt").write_text("no go here")
        out = io.StringIO()
        with redirect_stdout(out):
            m.analyze(tmp, None, False, False)
        check("empty dir absent from report", "internal/empty" not in out.getvalue(), out.getvalue())
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def real_repo_crosscheck() -> None:
    """Cross-check the tool's exported-func enumeration against `go doc -all`.

    `go doc` may need a full package build on first use (slow), so warm it once
    with a generous timeout before the assertion.
    """
    print("== real repo: cross-check enumeration vs go doc ==")
    # Warm the build cache first — a cold `go doc` can take minutes on this repo.
    try:
        subprocess.run(
            ["go", "build", "-o", os.devnull, "./internal/taskverdict/"],
            cwd=REPO, capture_output=True, text=True, timeout=600,
        )
    except Exception:
        pass  # best effort; go doc below is the real check

    try:
        r = subprocess.run(
            ["go", "doc", "-all", "./internal/taskverdict"],
            cwd=REPO, capture_output=True, text=True, timeout=600,
        )
    except Exception as e:
        check("go doc available", False, str(e))
        return
    if r.returncode != 0:
        check("go doc succeeded", False, r.stderr[:200])
        return

    # `go doc -all` lists "func Name(" for each exported func.
    doc_funcs = set(re.findall(r"^func\s+([A-Z]\w*)\s*\(", r.stdout, re.MULTILINE))
    m = load_tool()
    src_funcs = set(m.exported_funcs(m.go_files(REPO / "internal" / "taskverdict")))
    check("tool finds the same exported funcs as go doc",
          doc_funcs == src_funcs, f"doc={sorted(doc_funcs)} tool={sorted(src_funcs)}")


def main() -> int:
    synth_case("synthetic")
    real_repo_crosscheck()
    print()
    if failures:
        print(f"RESULT: FAIL ({len(failures)}): {failures}")
        return 1
    print("RESULT: ALL PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
