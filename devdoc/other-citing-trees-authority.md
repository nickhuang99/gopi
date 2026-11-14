<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Which other citing trees are authority — the per-tree ruling on the citation corpus

**Status: RULING (b) — the four trees that feed the cite tier, ruled on one at a
time.** Landed 2026-10-14. Companion to `devdoc/run-record-is-not-authority.md`
(RULING (a), which ruled on `tasks/` alone). The criterion is still ONE constant,
`tools/docclass/classify.py`'s `AUTHORITY_EXCLUDE_DIRS`; the regression tests are
in `tools/docclass/classify_test.py`
(`test_every_run_record_tree_refuses_the_same_bytes` and
`test_the_same_bytes_outside_the_run_record_tree_still_cite`).

## The question, and why RULING (a) left it open

RULING (a) closed with an explicit admission, not a general principle:

> **Not Done / honest limits** — "Whether a briefing's mention is a *statement* (it
> is an assignment document, arguably a live one) or another run-record face is a
> separate question that was NOT asked here. Filing it as its own task is the
> correct next step."

The fence it drew was therefore a fence around one directory. But the cite tier
reads the WHOLE tree: `internal/`, `briefings/`, `cmd/`, `docs/`, `scripts/`,
`.evid/` and `reports/` all supply citation evidence today, and the same question
— *is this mention a STATEMENT, or is it a run recording what it did?* — was asked
of exactly one of them. This ruling asks it of each, and does not extend the
answer from one tree to the next by analogy: **each verdict below is a
measurement**, and the trees that survive are kept for a measured reason, not by
default.

## The measurement every verdict rests on

Three arms per tree, all read off the shipped index in ONE process (the shipped
file untouched; the census script is kept beside the session's artifacts):

1. **cites / files** — how many of the corpus's docs take their citation evidence
   from this tree, and how many files supply it.
2. **class moves** — pop that tree's `CITED` entries and reclassify only the docs
   whose OWN cite came from it: how many docs' CLASS changes. This is the level
   RULING (a) used, and it is the only number that says whether the tree decides
   verdicts at all.
3. **the genre of the citing files** — what KIND of artifact the mentions stand in.
   This is the arm that actually decides the rulings: "does a line contain this
   filename" was never the question; the question is whether the line is a
   statement the authority makes.

```bash
# the census (one module instance per tree, tree-by-tree; writes nothing)
python3 <session>/artifacts/decide.py . internal/ briefings/ cmd/ docs/ scripts/ .evid/ reports/
python3 <session>/artifacts/genre.py  . internal/ briefings/ cmd/ docs/ scripts/ .evid/ reports/
```

Observed, base `b51a763` (2521 rows; 2520 docs):

| tree | cites | files | docs whose class moves | sole cite | genre of the citing files |
|---|---|---|---|---|---|
| `internal/` | 548 | 215 | **262** | 262 | go test 260, gate testdata 173, go source 115 |
| `briefings/` | 164 | 107 | **96** | 96 | brief-* 152, SPEC/PRINCIPLE 8, other 4 |
| `cmd/` | 160 | 112 | **78** | 78 | go test 111, go source 49 |
| `docs/` | 57 | 27 | **18** | 18 | **`docs/archive/` 47**, script 9, testdata 1 |
| `scripts/` | 27 | 19 | **5** | 5 | shell script 27 |
| `.evid/` | 21 | 2 | **6** | 6 | **`.evid/orig-gopi.md` 19**, script 2 |
| `reports/` | 24 | 17 | **1** | 1 | `reports/archive/` 24 |

## The rulings, one per tree

### EXCLUDED — `briefings/` (164 cites / 107 files / 96 docs move)

**What the mentions ARE.** 152 of the 164 stand in `brief-*.md` — a briefing is a
**dispatch document**: it is written BEFORE the work runs, to tell an agent what to
do. `briefings/brief-worker-resume-owns-the-ground.md:28` cites
`devdoc/PRINCIPLE-deterministic-lifecycle-ownership.md` as *"the … GUARANTEE"* —
i.e. the assignment quotes the rule it is asking the agent to implement. That is
the definition of a premise, not of a statement: the document has no claim of its
own to verify against master, and the devdoc it names is still exactly as
authoritative after the briefing is finished and deleted as it was before. The
same defect as `tasks/`, one layer up the pipeline: a run's *plan* is as much a
record of an action as a run's *report*.

**Why this is a ruling and not a preference.** 96 docs' classes rest on this tree
today and 96 of them take the tree as their ONLY cite — i.e. the briefing is not
one witness among several, it is the sole thing making those docs read
`normative`. When the briefing leaves the tree the class falls back.

### EXCLUDED — `docs/archive/` (57 cites / 27 files / 18 docs move)

**What the mentions ARE.** ALL 57 stand in files under `docs/archive/` — there is
no `docs/` left outside the archive (measured: `git ls-files docs | grep -v
'^docs/archive/'` prints nothing). And the tree states the criterion in its own
words, `docs/archive/README.md`:

> **这里放的是"产物"，不是"文档"。** 一个路径的全部价值在于**它记录了某一次运行的
> 输出**（截图、GIF、渲染页、录屏帧、逐字终端输出、可重跑的成片）⇒ 它是**产物**；
> … 所以这里的规则是 `git mv`，永不 `rm`。

A tree whose README defines its paths as *"valuable because they recorded one run's
output"* is a run-record tree by its own declaration, and its citations read like
it: 16 of the 57 stand in one generated replay README, 8 in one generated
`build.sh`, and the rest in per-stage `00-entry.md` files whose `**devdoc**:`
cell names the doc the demo material is *about*
(`docs/archive/demo/materials/03-status/00-entry.md:5`). Those are a material
generator's own record of which devdoc it illustrated.

**Honest scope note.** I did NOT examine all 27 files (18 docs move; the samples
above are 6 of them, covering 31 of the 57 cites). The verdict rests on the tree's
own declared criterion plus the genre census above — not on the unexamined
remainder, which is why the count and the reason are both stated.

### EXCLUDED — `.evid/` (21 cites / 2 files / 6 docs move)

**What the mentions ARE.** 19 of the 21 stand in `.evid/orig-gopi.md`, and that
file is a **snapshot: GOPI.md as it stood at commit `03a140023`** (1614 lines vs
GOPI.md's 1488; `git log -1 -- .evid/orig-gopi.md` → `03a140023 docs(rules): split
GOPI.md into Tier A … + Tier B`). A copy of an OLDER REVISION of a rule file is a
record of that revision, not a second statement of the rule: the live file
(`GOPI.md`) is the authority, and a frozen copy of it is evidence *about* the
corpus. The remaining 2 cites stand in a shell script
(`.evid/serve-lock-reland-acceptance-20260923.sh:29`) whose line is a `for f in …`
loop over two paths — the doc as an OPERAND, the shape the command-transcript arm
already refuses for the same reason.

**The control arm that made this decisive.** The 6 docs `.evid/` holds up were
first checked against the live tree: with `.evid/` removed, 6 of them have NO other
cite at all — and none of the 8 candidate docs appears in live `GOPI.md` or
`CONFIG.md` (measured: `grep -l` over both, 0 hits, while
`.evid/orig-gopi.md` carries each mention once). So the live rule file, which is
the file that states these rules today, does NOT cite them — the frozen copy is
the only thing that does. That is the same finding the `tasks/` ruling made by a
different route: **the class follows the PRESENCE of an artifact that is not the
authority.**

### KEPT — `internal/` (548 cites / 215 files / 262 docs move) — the largest tree, and live

Not excluded, and the 262 is not a defect signal: it is the size of a tree that
genuinely talks about documents. **433 of the 548 cites stand in test/gate
artifacts** (go test 260 + gate testdata 173), and a gate that names a doc is a
gate ASSERTING that doc's rule: `internal/testutil/worker_ownership_gate_test.go`
maps each doc to the claim it checks (`"devdoc/ab-control-run-cmd-settings.md":
"evidence: the --settings control's run_cmd stamps"`). A checked-in gate is the
opposite of a run's record — it is the machine-checkable statement of the rule,
and it is re-run on every merge. The remaining cites stand in shipped Go source
whose comment points a reader at the doc that explains the code beside it
(`internal/agent/loop.go`). **A code comment citing the doc for the code it sits
in is a statement.**

### KEPT — `cmd/` (160 cites / 112 files / 78 docs move)

Same criterion, same result: 111 of the 160 stand in `_test.go` files and 49 in
shipped source. The samples are the strongest form of a live citation in the
corpus: `cmd/agent/chat_turn_dump_grid_pty_test.go:92` cites
`devdoc/answer-row-cut-vocabulary.md` **as the rule the assertion checks**
("devdoc/answer-row-cut-vocabulary.md). A walk that compares the band against
the …"), and `cmd/agent/cli_annotate.go:19` cites
`devdoc/capability-annotate.md` in the help text a user reads. Both are artifacts
whose correctness depends on the doc being right — the definition of authority.

### KEPT — `scripts/` (27 cites / 19 files / 5 docs move)

All 27 stand in shell scripts, and the tree contains two kinds. One IS a run's
record — `scripts/gopi-stats.sh`, `scripts/replay-check.sh` — and on the genre arm
that is a reason to suspect the tree. But the ones that carry citation weight are
`scripts/materials/*.sh`, the documented material generators (their output is the
demo material under `docs/archive/`, whose own README calls those files 产物). A
generator naming the doc it renders is naming its subject; excluding them would
be excluding the recipe rather than the record. The tree is small (27 cites, 5
docs) and mixed, so it is kept WHOLE for the reason the corpus keeps `reports/`:
the boundary that decides is a tree's ROLE, and `scripts/` is shipped, runnable
tooling — not a record of one run. `go-toolchain-env-both-nodes-20260920.md`'s own
citing line is `# (History: devdoc/… set this per node.)` — a pointer from
runnable tooling at the doc that explains the setting, which is a statement.

### KEPT — `reports/` (24 cites / 17 files / 1 doc moves)

Already held to be live authority by RULING (a), and re-measured here rather than
inherited: `reports/archive/2026-08/report-gopi-r5r6-debug.md:41` is the citation
(`**机制**：DeepSeek 的 Anthropic 兼容端点接受 budget_tokens 但不执行
（devdoc/thinking-budget-fix.md 已证实）`) that holds `devdoc/thinking-budget-fix.md`
up today, and it is still the only cite for that doc. A ruling's own boundary must
survive the next ruling's measurement, and this one did.

## The criterion (Rule 13/14 — ONE definition, ONE predicate)

```python
AUTHORITY_EXCLUDE = {"scripts/docclass-red-proofs.sh"}
AUTHORITY_EXCLUDE_DIRS = ("tasks/", "briefings/", "docs/archive/", ".evid/")
```

One constant, consumed by the ONE existing predicate in `build_citation_index()`:
`any(r.startswith(d) for d in AUTHORITY_EXCLUDE_DIRS)` — the SAME seam the
per-file `AUTHORITY_EXCLUDE` uses. **No second predicate was added**, and the
four trees are entries in one list rather than four branches.

## What the ruling changed in the corpus (and the number that surprised)

**50 docs' classes rest on the four trees** (measured by classifying every doc
twice in ONE process with the constant swapped — `artifacts/effect.py`). Of those
50, **38 rows needed re-deriving** (33 whose stored class was `normative`, plus 5
whose class was ALREADY right but whose stored evidence CELL still named an
excluded tree — re-derivation moved the cell to the kept tree that cites the doc,
e.g. `devdoc/glm-5.3-pricing.md`: `cite briefings/brief-stats-cost-v2.md:22` →
`cite cmd/agent/cli_stats.go:96`). **Every class change is
`normative → historical`; not one row moved the other way.**

| | rows |
|---|---|
| docs whose class changes with the trees excluded | 50 |
| rows re-derived by this ruling (repeated `--only`) | **38** (33 class + 5 evidence cell) |
| of those, `normative → historical` | **38** |
| of those, `historical → normative` | **0** |
| residual `cite <excluded tree>` evidence cells afterwards | **0** (was 5) |
| stored distribution after | 1773 historical / 734 normative / 12 roadmap / 1 index-meta |

**The number that is smaller than the per-tree sums, and why — stated because it
looks like an arithmetic error and is not.** The per-tree census adds to 466
(262+96+78+18+5+6+1); the union effect is 50. `CITED` stores **ONE mention per doc**
(the first one the corpus walk finds, `CITED.setdefault`), so a doc cited by both
`briefings/` and `internal/` keeps its `internal/` mention when the briefing is
excluded and its class does not move. The per-tree figures answer "how much rests
on THIS tree"; the union figure answers "how many docs does the ruling move". They
are different questions, and the ruling acts on the second.

## The regression test (red-then-green, same bytes / two trees)

`test_every_run_record_tree_refuses_the_same_bytes` walks `RUN_RECORD_TREES` and
`LIVE_AUTHORITY_TREES` — spelled in the test as the trees it exercises — writing
`AUTHORITY_PROSE` at the SAME filename once per tree and reading the class back
through the shipped index. The variable is the TREE; the sentence and the
filename never change. Its first assertion compares the test's list with the
shipped constant, so a tree added to `classify.py` without an arm (or vice versa)
fails by assertion rather than by silence.

**A dead arm that the loop found, recorded because it is the test's own pitfall.**
The first version left each arm's file on disk; `CITED` keeps the FIRST mention
found by the sorted corpus walk, so the `kept='scripts/'` arm read
`cite cmd/01a136ee-decision-report.md:3` — a false GREEN for a tree that was never
consulted. The arms now remove their file before the next one is written, and the
evidence-cell assertion is what caught it.

**The red proof, on the shipped file.** `AUTHORITY_EXCLUDE_DIRS` is mutated in
place to the pre-ruling `("tasks/",)` → the test fails on the list-equality
assertion. Then — the stronger arm — the constant is restored and the PREDICATE
mutated instead (`AUTHORITY_EXCLUDE_DIRS[:1]`, i.e. the tree list intact but only
the first tree consumed) → 3 subTests fail with
`'normative' != 'historical' : a .evid/ file is a record of a past run, not a
statement: normative: … cited as the live statement by
.evid/01a136ee-decision-report.md:3`. That is the arm that proves the VERDICT
follows the predicate, not just the list comparison.

## Reproduce (each line is a run, not a claim)

```bash
# the per-tree census (cites / files / docs moved / sole cite / genre)
python3 <session>/artifacts/decide.py . internal/ briefings/ cmd/ docs/ scripts/ .evid/ reports/

# the ruling's own union effect: 50 docs, 45 rows re-derived, all norm → hist
python3 <session>/artifacts/effect.py . <flags> <drift-flags>

# the ruling's arms (fixture-sized, under a second):
python3 tools/docclass/classify_test.py TestCitationsAreStatementsNotQuotations

# the two check surfaces + the residue the predecessor ruling used as its metric
python3 tools/docclass/classify.py . --check          # 0 gap(s)
python3 tools/docclass/banner.py  . --check           # 0 disagreements
grep -c 'cite briefings/\|cite docs/archive/\|cite \.evid/\|cite tasks/' devdoc/DOC-INDEX.tsv   # 0
```

## Not Done / honest limits

- **`scripts/` is kept as a MIXED tree, and that is a judgement, not a
  measurement.** Its 27 cites split between a run's record (`gopi-stats.sh`,
  `replay-check.sh`) and shipped material generators. I kept the whole tree
  because the boundary that decides is a tree's ROLE and `scripts/` is runnable
  tooling; if a later ruling wants it excluded, the measurement is already above
  (5 docs move) and the change is one entry in the list.
- **671 rows carry stored text that is not what the classifier renders today.**
  Pre-existing, and measured to be so rather than asserted: at the baseline
  `b51a763` the same command reports **699** stale rows, at this commit **671**,
  and the set stale only at this commit is **empty** (`comm -13` on the two
  sorted STALE lists). NOT re-derived here: an edit that rewrites rows no ruling
  moved is exactly the churn the predecessor's method avoids.

  ```
  git worktree add --detach /home/user/workspace/gopi-basecheck-b51a763 b51a763
  (cd /home/user/workspace/gopi-basecheck-b51a763 && python3 tools/docclass/classify.py . --check)   # 699 row(s)
  python3 tools/docclass/classify.py . --check                                                     # 671 row(s)
  comm -13 <(…STALE at b51a763 | sort) <(…STALE here | sort) | wc -l                                # 0
  ```

- **The exclusion CHANGED 12 rows that the entry list alone does not name, and
  they are re-derived here.** `build_citation_index` is not only an input to the
  class decision — it also feeds the reason text (`… cited by <file>:<line>`).
  Dropping `briefings/` from the index therefore removed the citation a standing
  claim was resting on, and the classifier's answer for those docs moved from
  `cited by briefings/…` to no citation at all. Writing an exclusion list alone
  would have left them silently stale. Measured: 12 rows, all `normative`, all
  **unchanged files** (`git diff b51a763 HEAD -- <path>` empty) whose row text
  moved only because the index did — `branch-lifecycle-ruling.md`,
  `cli-task-surface-restructure-20260915.md`, `docanchor-blank-line-fix.md`,
  `materials-D-annotation-criterion.md`, `merge-time-contract-gate-20260912.md`,
  `merger-self-driving.md`, `rule31-ran-check.md`, `scratch-gc-stale-window.md`,
  `serve-lock-refusal-diagnosis.md`, `voice-hw-matrix.md`,
  `voice-mic-probe-privileged.md`, `worker-log-session-attribution-20260912.md`.
  They were re-derived in the SAME commit (`--only` ×12 → `updated: 12`), which
  is what moved the stale count from 683 to 671. **A verdict on a citing tree is
  a verdict on every row that tree was holding up** — an exclusion that does not
  re-derive them ships a corpus that disagrees with its own classifier.
- **`docs/` had no non-archive part left to test** — the ruling is on the tree as
  it exists (`docs/archive/`), not on a hypothetical live `docs/`.
- **The genre census classifies FILES, not lines.** It is the arm that decides
  here, and its samples were read by hand; but a file with one live sentence and
  fifty record lines is counted once. The four exclusion verdicts rest on their
  trees' own DECLARED nature (a dispatch document, a tree whose README defines its
  paths as 产物, a frozen snapshot of GOPI.md) as well as on the census.
