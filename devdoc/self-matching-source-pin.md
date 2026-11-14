<!-- doc-class: normative -->
# The self-matching source pin — a guard that reads its own file can never fail

**Class**: a structural guard that reads its OWN source file and asks
`strings.Contains(src, needle)` is **self-matching by construction**. The guard's
own string literal IS the needle, so the guard is green on every tree, including
the tree whose defect it exists to refuse. This is a RECORD of three measured
instances and their repairs (2026-10-12, tasks `01a13019` and `01a13025`).

## The instances

| # | file | needle | repaired in |
|---|---|---|---|
| 1 | `cmd/agent/merge_every_test.go` (`TestMergeResidueWalkHasOneDefinition`) | a 78-char delegation call | `4ebb9113f` |
| 2 | `internal/tools/task_main_checkout_deposit_test.go` (`TestMainCheckoutDepositTestNamesTheMeasuredReproduction`) | four evidence literals | this task |
| 3 | *(scan below)* | — | — |

Instance 1 was falsified first: in a throwaway detached worktree whose
`merge_every_test.go` had the delegation replaced by a weaker hand-rolled walk —
the exact shape the pin refuses — the pin reported `ok`. Instance 2 is the same
defect shape: all four needles already occur in the file's own comments
(lines 4/13/14), its constants and its failure messages.

## Why a comment scan is NOT the repair

A "strip the comments first" fix does not close the class: a MENTION inside a
comment or a **string literal** still matches, and the guard's own `want` list is
a string literal. The distinction that matters is not comment-vs-code, it is
**spelling vs use**. `go/parser` + `go/ast` answer exactly that question.

## The repair shape (reuse, don't invent)

Both repairs are the same three parts, and each part closes a different hole:

1. **PRODUCER half** — the name must be DEFINED in the file with the measured
   value (instance 1: the walk's helper is defined exactly once in the file that
   owns the walk; instance 2: each citation constant must hold its measured
   value). A use of a name nobody defines is not a citation.
2. **CONSUMER half** — the name must be SPENT by the code:
   `ast.Inspect` + a count of `*ast.CallExpr` (instance 1, via the family's
   existing `probeCallName`) or of `*ast.Ident` uses (instance 2).
3. **NON-VACUITY floor** — assert something else the parse must have seen
   (a known sibling call, a declaration count, a constant count, and **the total
   citation count**). Without it a parser that silently returned an empty tree
   makes every count zero-on-zero and the guard passes on ANY file.

### The two traps the repair itself walked into (both measured here)

- **`id.Obj.Kind == ast.Con` does not identify a definition.** On a *use* of a
  package-level const, `id.Obj` is that same const object, so the predicate skips
  every real use and the gate goes vacuous in the opposite direction. Definitions
  are excluded **by position** (`name.Pos()`), never by `Obj.Kind`.
- **The gate can satisfy itself through its own failure messages.** The
  consumer half counts identifier uses; the gate's own `t.Errorf` arguments spend
  the very identifiers it counts. The gate's own `FuncDecl` must be EXCLUDED from
  the scan — and its presence ASSERTED, so a rename cannot silently turn the
  exclusion into a no-op. The same applies to the gate's vocabulary constants
  (`depositMeasuredRowName = "depositMeasuredRow"`), which are strings, not uses.

## Proof, not argument (the only way to know a pin has teeth)

Two arms, each a **throwaway detached worktree**; the worktree under test is
never mutated. Run the OLD body and the NEW body in the SAME falsified tree —
that is what isolates the defect to the guard rather than to the fixture.

```
# arm A — the pre-fix guard, tree whose reproduction cites nothing
cd <detached @pre-fix-tip> && go test ./internal/tools/ -run '^TestMainCheckoutDepositTestNamesTheMeasuredReproduction$'
--- PASS     ← VACUOUS: no evidence cited anywhere, guard still green

# isolation — the SAME falsified tree, the PRE-FIX body spliced over the new one
--- PASS     ← the assertion, and only the assertion, differs

# arm B — the repaired guard, tree whose reproduction stopped SPENDING its constants
--- FAIL: TestMainCheckoutDepositTestNamesTheMeasuredReproduction
    the reproduction spends depositMeasuredRow 0 times, want at least 1
    this gate's scan saw 0 citation ident(s) across the 4 subjects, want at least 10
```

Evidence: `~/.gopi/sessions/01a13025-5297-7df6-8602-a9727f97aa27/artifacts/`
(`armA-prefix-guard-vacuous.txt`, `armB-ast-guard-has-teeth.txt`,
`armB-falsified.diff`, `armB-falsified-file.go`).

Non-vacuity floor proven separately (a third throwaway detached worktree): point
the repaired gate at a WRONG subject file — `mainCheckoutDepositTestFile =
"bash_test.go"`, a file that EXISTS, so the parse succeeds and every count is a
plausible-looking zero — and it FAILS by name
(`depositMeasuredRow must be DEFINED in bash_test.go`), rather than passing on a
wrong parse. The same run also trips the gate's own-funcdecl assertion
(`bash_test.go no longer declares TestMainCheckoutDepositTestNamesTheMeasured-
Reproduction`), which is the guard against the exclusion silently becoming a
no-op. Capture: `armC-wrong-subject-nonvacuity.txt`.

## The scan — find the next instance

A test that reads its OWN source is the population at risk; the defect is the
ones whose verdict is a bare substring. Detector (follows the constant/alias
indirection this codebase uses — the file usually names itself through a `const`,
not a literal):

```python
import re, subprocess, os
files = subprocess.run(["git","ls-files","*_test.go"],capture_output=True,text=True).stdout.split()
hits = []
for f in files:
    b = os.path.basename(f)
    src = open(f, errors="ignore").read()
    names = {n for n, v in re.findall(r'(\w+)\s*(?::=|=)\s*"([^"]+)"', src) if os.path.basename(v) == b}
    for _ in range(3):                      # alias chains: const pen = <name>
        for a, rhs in re.findall(r'(?:const\s+|var\s+)?(\w+)\s*(?::=|=)\s*([A-Za-z_]\w*)\s*$', src, re.M):
            if rhs in names: names.add(a)
    for n in names:
        if re.search(r'os\.ReadFile\(\s*%s\s*\)' % re.escape(n), src):
            hits.append((f, n, "go/parser" in src)); break
for f, n, ast in sorted(hits):
    print(("AST     " if ast else "NOT-AST "), f, "via", n)
```

**Measured at this change's tip** (the generator is the snippet above; it prints
both figures, so re-run it rather than trusting this line):

```
SELF-READING TESTS (os.ReadFile on own basename, const/var alias chain followed): 2
AST      cmd/agent/merge_every_test.go via pen
AST      internal/tools/task_main_checkout_deposit_test.go via pen
```

Both are now AST-based — instance 1 repaired in `4ebb9113f`, instance 2 by this
change. A NARROWER detector (`ReadFile("<own basename>")` only) finds **1**, and
a looser one ("the file names its own basename anywhere") finds dozens, nearly
all of them a file merely mentioning a sibling by name. Which detector you run
decides the number, so the number is meaningless without it (Rule 38).

### The contrast that confirms instance 2 was the outlier

The two in-package siblings that scan sources are **not** instances:
`task_display_env_guard_test.go:257` and `bash_test.go:838` both walk SIBLING
files and explicitly EXCLUDE their own (`name == taskDisplayEnvGuardFile` →
`continue`; the sibling table scans `bash_test.go` + two others with a per-file
READ FLOOR). Excluding yourself is one correct fix; reading yourself and counting
USES is the other. What is never correct is reading yourself and counting
SPELLINGS — which is what both repaired instances did.

## The rule this leaves behind

A guard's verdict must not be reachable by the guard's own text. If a pin's
subject is a file, pin a **use** inside it — never a spelling, because the
spelling is what the pin is written in.
