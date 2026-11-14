<!-- doc-class: normative -->
# devdoc/requirements/ — reverse requirement analysis of gopi

Entry point for the series. Read in this order:

| file | what it is |
|---|---|
| `00-method-and-standards.md` | the method, the why-standard, the evidence classes, the mandatory 8-section shape, the six operator-vision constraints, the honesty rule. **Self-contained** — this is what a per-module worker needs. |
| `00-module-map.md` | the 14 module boundaries, each with its reason, its packages, and the coverage proof that nothing is unassigned. |
| `01-per-module-briefings.md` | the dispatch-ready briefings `[1/N]..[15/N]` (14 modules + summary), each carrying its evidence seeds and the measurement it must make itself. |
| `<module>.md` ×14 | the module documents (produced by the per-module tasks). |
| `99-summary.md` | the whole-project answer: what gopi is for, what is well served, the ranked gaps, where the operator's model and the code's model disagree. |

## What is already established before any module document exists

Measured on master `17cec86b` (re-runnable commands in the map and briefings):

- **143,015** lines of production Go, **179,917** lines of test Go;
  **40** `internal/` packages; **47** `cmd/agent/cli_*.go` surfaces;
  **932** `devdoc/` files; `GOPI.md` = 1,085 lines, **29** numbered rules.
- The ledger holds **1,607** rows — **557** `human`, 686 `auto`, 348 `llm` —
  plus **3,251** distilled memory rows and **73,559** session directories.
- **A prior requirements series existed and produced nothing.** Three rows
  (`01a0aac3`, `01a0aac4` ×2) filed 2026-09-16 named `REQUIREMENTS [0/11]` /
  `[1/11]` / `[2/11]`; all three are `cancelled`; `git log --all --oneline
  --diff-filter=A -- 'devdoc/requirements/*'` returns **empty**. The reason is
  not recorded anywhere the umbrella could find — the summary must try again.

## The three findings the umbrella itself contributes

These are the umbrella's own "something the operator may not have noticed", per
the epic's acceptance. Each is re-runnable.

1. **The earlier requirements series died silently and left no marker.** Three
   cancelled rows, zero documents, no explanation on any branch
   (`git branch -a --list '*requirement*'` → only this branch). Consequence: any
   future attempt restarts from zero, and no one can say whether the earlier one
   died because the *framing* was wrong or because the work was simply never
   picked up. Cost of not knowing: the same 11 tasks can be filed again and
   abandoned again.

2. **`internal/testutil` looks like a production dependency and is not.** Five
   production files under `internal/` / `cmd/` match `internal/testutil` by
   simple grep — but every match is **in a comment**, and the count of real
   imports in production code is **zero**
   (`grep -rn 'gopi/internal/testutil"' --include=*.go internal cmd | grep -v _test | wc -l` → `0`).
   Why it matters: a naive dependency-direction audit reports a cycle that does
   not exist, and the *comments* that create the illusion
   (`internal/tools/write.go:58`, `internal/tools/script_install_sync.go:8`) are
   load-bearing design rationale — deleting them to satisfy a tool would lose the
   reason. Recorded here so no later agent "fixes" a non-problem.

3. **Tool adoption has moved, and the surface has a new zero-use tier.**
   `GOPI.md:309` records the 2026-08-27 audit: git_blame **0** calls in 21,435
   events, code_search **0.3%**. Measured now over the **300 newest session
   logs**: bash 15,665 | job 3,292 | task 1,942 | read 1,930 | edit 1,874 |
   memory 995 | write 552 | code_search 142 | context_trim 123 |
   keyword_search 111 | task_submit 59 | vision_analyze 20 | git_blame 6 —
   and **text2img 0, screen_capture 0, web_search 0, web_fetch 0**.
   So the two tools Rule 11 was written about are no longer the problem
   (git_blame went 0 → 6, code_search 0.3% → ~1.6% of the sampled call volume),
   while **four other tools are now at zero** in the same window. Rule 11's
   acceptance standard is a *living* check, and it currently fails for a
   different set than the rule names. (Sample, not audit — the module worker
   re-measures; the point is the tier moved.)

## What I could not determine (umbrella level)

1. **Why the 2026-09-16 requirements series was cancelled.** No document, no
   note on any branch, not recoverable from branch names. Cheapest way to find
   out: the session logs of those runs —
   `ls ~/.gopi/sessions/01a0aac3* ~/.gopi/sessions/01a0aac4*` and read the
   `*.jsonl`; or ask the operator. `[E1: tasks 01a0aac3, 01a0aac4]`
2. **Whether the operator intends `web/` to be a first-class user surface.**
   The boundary depends on an unwritten need; the sample shows no `web` traffic
   as a *tool* but a live worktree `gopi-web-m2-rework` exists. Cheapest way:
   one sentence from the operator, or the M8 doc.
3. **How much of the 73,559 session directories is live value.** Counted, not
   sampled — the umbrella did not measure disk cost, retention, or how many are
   ever read. Cheapest way: `du -sh ~/.gopi/sessions` plus an mtime histogram;
   belongs to M11 (observability) and M10 (cleanup).
4. **Whether `internal/deepseek` is a module or part of the loop.** Placed in M5
   by inference; flagged in the map as unresolved.
5. **The true total of the ledger's own history** (1,607 rows counted, but the
   umbrella did not measure how many carry a `deliverable` or a `reason`, which
   would be the sharper number for M1).
6. **Whether `GOPI.md` is the whole rule set** — the file has 29 rules and runs
   to 1,085 lines, but rules may also live in per-repo `goose.md`/`GOPI.md`
   variants the umbrella did not enumerate. A `find ~ -name 'GOPI.md'` sweep
   would settle it.

## Honesty note

Everything in this series is **reconstructed**. gopi has no requirements
document, so every "the operator needs…" in a module document is either traced
to a citation or marked `[INFERENCE]` with its weakest link. Any sentence in any
document that carries no citation token is, by the series' own standard, an
opinion — including sentences in this README.
