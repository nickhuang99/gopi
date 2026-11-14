<!-- doc-class: normative -->
# The smoke gate on the merge/verify path — and the retirement of the whole-tree prescription (2026-09-18)

Epic `smoke gate` subtask **[2/4]**, branch `feat/wire-smoke-gate-merge-path`.

## The defect this closes

The heavy/smoke classification had already landed (`internal/heavytest` +
`testdata/heavy-tests.tsv` + the Makefile's `test` / `test-heavy` targets,
2026-09-16). What had NOT landed is the other half: the merge/verify path's
**LLM-facing text** still told its children to run the whole module.

Three sites, all on the path where a model acts on them:

| site | text (before) | why it matters |
|---|---|---|
| `internal/tools/verify_gate.go:565` (rework child) | "run the project's own verification there (`go build ./...` and the tests the original fix touched — `` `go test ./...` `` **for the whole tree**)" | the child redevelops a reverted branch under a bounded wait cap |
| `internal/tools/verify_gate.go:789` (verify-retry child) | "SMOKE-ONLY PATH: `` `go test ./...` `` here is the SMOKE set" | it called the WHOLE TREE "the smoke set" — the mislabel that made the whole-tree run look sanctioned |
| `cmd/agent/merger_takeover_prompt.md` §3.8 | "**Do not run the test suite.** … `build` + `vet` is your gate" | a *negative* instruction: it forbade the whole tree but gave the take-over NO positive smoke command, so a tick either ran nothing behavioural or improvised |

The middle row is the one worth keeping in mind: the text was not merely
permissive, it was **wrong about what smoke means** — the whole module was
labelled the smoke set while heavy tests were excluded from it. Two different
scopes shared one name.

## What landed

**One constant, one leaf package** (`internal/smokegate`, Rule 14 — no project
imports, so `internal/tools`, `cmd/agent` and `internal/standards` can all speak
it):

- `Command` = `make test` — this repo's declared smoke target, which routes
  through `scripts/test-budget.sh` (one declared per-package budget; a timeout
  reported AS a timeout) with the smoke classification in force and the skipped
  set written to the ledger.
- `ScopedCommand` = ``make test-budget PKGS='<the Go packages your diff touched>'``
  — the bounded form. The slot is deliberate: the caller maps branch→packages
  (the touched-package primitive, epic subtask [1/4]), and a briefing that
  invented package names would be wrong for every branch but one.
- `WholeTreeNeedle` = `` `go test ./...` `` — the shape the path must not
  prescribe.
- `Doc` = *"the merge/verify path runs SMOKE tests, BRANCH-SCOPED — never the
  whole tree"* — the one sentence the briefings, the prompt and the docs quote.

**The three sites rewritten** to reference it — no bare literal at any of them
(Rule 1b/13):

- `verify_gate.go` rework child: build + the scoped smoke, with the rule stated.
- `verify_gate.go` verify-retry child: the mislabel is gone; the smoke command,
  the scoped form and the rule replace it.
- `tasks.go` — two spots: `TaskVerify`'s briefing ("run the SMOKE tests … scoped
  to what this merge touched") and `taskVerifyBaselineClause`, whose doc now
  names the whole-tree needle **as the baseline sweep's command** (built from
  `smokegate.WholeTreeNeedle`, so the one place that legitimately runs the whole
  tree still says so).
- `merger_takeover_prompt.md` §3.8: the blanket prohibition became a positive,
  scoped instruction (smoke command + `PKGS='…'` form + how to derive the package
  list from `git diff --name-only master...<branch>`).

**The gate** (`internal/standards/smoke_path_gate_test.go`,
`TestSmokePathPrescribesNoWholeTreeSuite`). It reports the whole-tree command
**as a quoted command span** in any merge/verify-path briefing, from two
carriers:

1. Go **string literals** under `internal/tools` (the merger gate + the briefing
   renderers) and `cmd/agent` (the take-over launcher);
2. the shipped artifact `cmd/agent/merger_takeover_prompt.md`.

The gate lands with an **EMPTY permit list** — no baseline, so a future re-inline
fails on the commit that introduces it. It also carries a non-vacuity floor
(`smokePathMinGoFiles = 60`; the tree has 104 + 76 production files under those
two roots): a broken root would otherwise report a clean green while reading
nothing.

### Why LITERALS and not comments — the precision decision

A comment **documents** the repository; a string literal **renders** into a
child's briefing. That distinction is what keeps the gate at zero false
positives on a repo that talks about `go test ./...` constantly:

- `internal/tools/verify_gate.go` names the whole-tree command in a comment to
  say it is NOT what the merge path runs — a comment, not reported. (The
  `MergeVerifyBaselineFailures` parser this bullet used to cite was retired
  2026-10-30 with the `verify` verb; see devdoc/worktree-removal-forensics.md
  §4 for the same retirement on the arm axis.)
- `GOPI.md`, `devdoc/`, `briefings/`, `tasks/` and `bugs/` discuss the command
  everywhere and are **out of scope on purpose**: the ledger's baseline sweep
  genuinely runs the whole tree (a baseline must enumerate every package's
  verdict), `make test-heavy` and a developer's own `go test ./...` are
  legitimate (Rule 27's deliberate off-peak opt-in), and a report must stay able
  to QUOTE the command this rule retired — a record that cannot quote its
  subject is not a record.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK; `go vet` on the touched packages → OK.
- Gate green on the fixed tree: `go test -run TestSmokePath ./internal/standards/` → ok.
- **Red-proof against the REAL pre-change text** (not a synthetic snippet only):
  `git stash push -- internal/tools/verify_gate.go internal/tools/tasks.go cmd/agent/merger_takeover_prompt.md`
  left the new gate in place against master's text, and the gate failed on
  exactly the three named sites —
  `internal/tools/tasks.go:6086`, `internal/tools/verify_gate.go:565`,
  `internal/tools/verify_gate.go:789` (raw output:
  `../gopi-smoke-gate-evid/redproof-gotext.txt`). The markdown carrier was
  red-proved the same way: appending the pre-change span to the shipped prompt
  made the gate report `merger_takeover_prompt.md`.
- **Rule 11 (LLM adoption) — a real session, evidence inline here** (this is a
  TEXT change to an LLM-facing prompt, so "the model actually followed it" is
  the acceptance standard, not `go test`):

  - setup: a scratch repo `~/workspace/smoke-e2e/repo` (master + one
    fast-forwardable branch `feat/greeting`, packages `./greet`, `./util`), its
    own profile, launched as a real merger take-over:
    `GOPI_MERGER_TAKEOVER_REPOS=<repo> GOPI_MERGER_TAKEOVER_MODE=setsid-only
    exec -a gopi-merger-takeover bin/gopi`;
  - **the session** (acceptance-facing): `2fa0a50e-e235-4cc3-9128-4293bb2807f3`,
    ledger row `01a0b197-f66c-731f-8df7-2aa557cf34fc`, launcher log
    `~/workspace/gopi-smoke-gate-evid/takeover-run2.log`;
  - **the instruction was DELIVERED**: the §3.8 clause this branch wrote appears
    verbatim in that session's `session.jsonl` — "**Run the SMOKE tests, SCOPED
    to what the merge touched — never the whole tree.** The merge/verify path's
    smoke command is `make test` … `make test-budget PKGS='<the Go packages the
    diff touched>'`";
  - **the model ISSUED it**: that session's `bash` tool_use
    `call_00_QdoaMldNcuoL0kC5Qh9V0704` runs
    `make test-budget PKGS=./greet` — the scoped form, filled in from
    `git diff --name-only HEAD~1 HEAD` — i.e. the LLM obeyed the new clause as
    written and did NOT fall back to a whole-tree command;
  - **the counterfactual is on record**: at the time of that run `master`'s
    prompt still carried the OLD §3.8 (`git show master:cmd/agent/merger_takeover_prompt.md`
    → line 643 "Do not run the test suite"), so the delivered text could only
    have come from this branch's rebuilt binary — the adoption is attributable
    to the change, not to ambient text.

  Note on a first-run detail worth a future reader's time: the earlier take-over
  session `0c1b4b8c` was spawned by the gopi binary built at that moment, which
  was the **stale** one (the old clause); after `CGO_ENABLED=0 go build -o
  bin/gopi ./cmd/agent/` the same harness produced `2fa0a50e` carrying the new
  clause. Rebuild before you trust a prompt-change experiment — the same
  stale-binary failure mode PROJECT.md Rule 12 describes for merges.

## The make-form trap (measured, and now pinned by a test)

`make test-budget PKGS=./a/ ./b/` is **broken**: make parses the second path as a
TARGET, so the script receives only `./a/` and the run silently covers a subset —
measured in a sandbox repo (see the report's evidence). The single-quoted
assignment is therefore load-bearing, and
`internal/smokegate` pins it (`TestScopedCommandStaysOneSingleQuotedArgument`)
rather than trusting an editor to keep the quotes.

## Addendum — the stale-base trap (why this branch merged master)

Cut from `bd621dc0`, this branch predated master's Rule 16 (`%w`) fixes, so the
tree's own standard gates were RED on it — `Rule 16 NEW violation at
cmd/agent/main.go:1223` + `cmd/agent/merger_restart.go:229`, plus a `STALE
baseline entry cmd/agent/main.go:1158`. The redness is inherited, and it was
proven inherited rather than assumed: `git diff master...HEAD` over those files
is EMPTY (this change touches none of them), `git show master:cmd/agent/merger_restart.go`
already reads `— %w`, and the BASE reads `— %v`. Master's fix also shifted
main.go's site 1223 → 1239, which is why the permit's line number is a moving
target in `internal/standards/baseline.go`.

**Habit worth keeping**: before reading a gate failure as your own, diff the
spot against `master` (`git show master:<file>` vs `git show <merge-base>:<file>`)
— a baseline keyed by `file:line` goes stale the moment any branch above that
line shifts, and `git merge master` is the repair, not a baseline edit.
