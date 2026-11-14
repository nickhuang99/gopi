<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The DOC-INDEX anchor tier: number and text are coupled at generation — and the two rungs nobody watches

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Base commit: **`3db654fb7`** (`merge: task/01a12d00e4b0787db7fcce75a8ed966c`, master tip at run
start of task `01a12d21-6193-7cc2-b0e4-d4925cd84d14`). Every figure below is re-derivable from
the scripts named beside it, run against this worktree at that commit.

**Supersedes** `devdoc/merge-union-row-check-anchor-drift-determination.md` §4 and the bug row
`01a12d1e-045d-70e8-9186-c548bf7acaee`. That row's measured half stands (re-measured here);
its diagnosis — a two-thirds drift presented as a defect with a corpus-wide re-derivation as the
implied durable repair — does not. **This document decides no gate behaviour**: the gate is
system-layer (§7, Rule 28) and the fork is handed to its owner.

## 1. What `01a12d1e` got right, re-measured here

```
$ python3 <session>/artifacts/anchor-tiers.py .        # Q1/Q2/Q3 + L1
Q0 anchor rows parsed                 : 231
   of which recorded text            : 231
   of which number only (legacy)     : 0
Q1 recorded TEXT gone from the file   : 0   <- the gate's subject
Q2 text alive, at a DIFFERENT line    : 153
Q3 cell line does not carry the text  : 153
   gate's line == cell number         : 78
   gate's line != cell number         : 153
```

The count is **153 of 231** at this commit (the bug row and its row-title say 153/230; the
manifest carries **231** anchor rows today). The one-row difference is not a disagreement about
the corpus — it is the corpus moving: `01a12d1e` measured `0b3c1de29`, this run measures
`3db654fb7`. **Q1 = 0** is the load-bearing half: every anchor row's recorded text is still
somewhere in its file, and every one of the 153 is GREEN under all three shipped gates
(`TestMergeUnionRowCheckOnTheRealManifest`, `classify.py --check`, `gopi tool doccheck` — §6).

## 2. The mechanism: the number comes from the doc's prose, the text from the code

This is the correction, and it is readable in two source lines.

The **generator** (`tools/docclass/classify.py`) builds the cell inside `code_spotcheck`
(**lines 524–537**): it iterates `ANCHOR_RE` (**line 489**) over the **document's own body** —

```python
ANCHOR_RE = re.compile(r"`([A-Za-z0-9_][A-Za-z0-9_./-]*\.go):(\d+)`")   # line 489
...
for m in ANCHOR_RE.finditer(text):                                     # line 528 — `text` is the DOC
    ref, n = m.group(1), int(m.group(2))
    ...
    if 1 <= n <= len(lines) and lines[n - 1].strip():                  # line 536
        return "anchor %s:%d at %s%s" % (cands[0], n, lines[n - 1].strip()[:70], drift_token(text))
```

So **the number is the document's own prose anchor** (whatever `` `path.go:NNN` `` the prose
names) and **the text is whatever stood at that number the moment the cell was written**. The
pair is consistent *at generation* and inconsistent from the first commit that moves a line —
which is the ordinary steady state of a line-numbered citation, not corruption.

The **gate** (`internal/docclass/evidence.go`) reads the recorded text as the authority and the
number as a hint — `Check` (line 133) goes through `anchoredTextLine` (**line 243**), the number
is "preferred, but only to report WHICH line matched". Both halves are by design and both are
documented in the code.

**Measured — the number really is the prose's number.** Over the 153 drifted rows, comparing the
cell's `(basename, number)` against the doc body's own `ANCHOR_RE` hits
(`artifacts/anchor-drift-shape.py` §1):

| provenance | rows | share |
|---|---|---|
| `NAMED-EXACT` — the doc's prose names that same `path.go:NNN` | 132 | 86% |
| `NAMED-OTHER` — the doc names that basename at a *different* number | 16 | 10% |
| `NOT-NAMED` / `NO-BODY-HIT` — the prose names no such anchor | 5 | 3% |

**Measured — the doc's own first hit is what the generator emits.** A whole-corpus generation of
the sample docs reproduces the cell from the prose, not from a search for the text
(`artifacts/classify-only-dryrun.txt`; `--dry-run`, writes nothing):

```
--- devdoc/IMMEDIATE-FEEDBACK-REGRESSION.md
anchor internal/agent/loop.go:6417 at // executeToolDefault is the real per-tool dispatch (executeTool witho
```

`loop.go:6417` is the **first** `ANCHOR_RE` hit in that doc's body
(`artifacts/doc-body-scan.py`: hits `6417, 547, 6417, 6417, 1694`; the prose mentions `547`
second). Regeneration therefore does not merely move a number: **it re-couples the number to the
text today**, and the next code move decouples them again.

## 3. What a re-derivation would actually cost — the case against dispatching it

`01a12d1e`'s implied durable repair is `python3 tools/docclass/classify.py .` (660 rows,
1320 lines). Measured three ways.

**(a) Simulated, per row** (`artifacts/anchor-regen-cost.py` §A — a faithful mirror of
`code_spotcheck`: first resolvable, non-blank `ANCHOR_RE` hit of the doc's own body):

| outcome | rows | share |
|---|---|---|
| `IDENTICAL` — nothing would change | 69 | 30% |
| `COSMETIC` — truncation/whitespace only | 3 | 1% |
| `TEXT-FOSSIL` — same number, the text at it differs | 122 | 53% |
| `WHOLE-CELL-MOVES` — the fresh number itself differs | 31 | 13% |
| `NO-FRESH-CELL` — the prose names no resolvable anchor | 6 | 3% |

**(b) Real, whole-corpus** (`artifacts/whole-corpus-write.diff`, `artifacts/real-write-anchor-delta.txt`).
`python3 tools/docclass/classify.py .` was run against this worktree, its output captured, and the
manifest **restored from git in the same command** (`cmp` against the pre-run copy: identical;
`git status --porcelain` empty afterwards):

```
666 666  devdoc/DOC-INDEX.tsv          # git diff --numstat: 666 changed lines
   CELL-UNCHANGED                          60
   NUMBER MOVED                            30
   NUMBER-SAME, TEXT REWRITTEN            132
   TIER-CHANGED (no longer an anchor)       9
   anchor rows before                     231
```

**(c) The tier's information content does not improve** (`artifacts/information-content-before-after.txt`):

| | anchor rows | 1-word-or-empty text | cells carrying `stale=N` |
|---|---|---|---|
| committed manifest | 231 | 27 (12%) | 67 |
| after a whole-corpus generation | 308 | 33 (11%) | 83 |

A regenerated cell re-records **today's** text on the **same rung**. The rung that makes a claim
weak — a one-word `text` — is not retired by re-deriving; it is re-issued. (The row count rises
231 → 308 because a fresh pass finds an anchor cell for docs the committed manifest had classified
with a lower tier — i.e. a rewrite is also a corpus-wide **re-classification**, not a number
tidy-up.)

**Conclusion**: the rewrite buys a *temporarily consistent* corpus, at the price of ~132 cells
whose recorded text is replaced, ~30 whose number moves, 9 rows whose tier changes, a 666-line
diff of a DATA file, and no change to either residual below. **Recommendation: do not dispatch it**
(§6).

## 4. Residual (i) — the number is never validated (measured size: 153 rows)

`anchoredTextLine` returns *a* line that satisfies the text; it never compares that line with the
number the cell recorded. A cell may therefore point two thousand lines away and stay green.
Measured shape over the 153 drifted rows (`artifacts/anchor-drift-shape.py` §2):

```
rows measured: 153   min=1  median=49  p90=706  max=2380
>=100 lines away: 54   >=1000 lines away: 10
```

Row 16 of the manifest (`devdoc/IMMEDIATE-FEEDBACK-REGRESSION.md`) claims
`internal/agent/loop.go:547 at if contentReturning {`; the text lives at `:582` — 35 lines off.
The worst observed is 2380 lines. **This is the sharper of the two residuals for a reader**,
because the number is the human's only navigation handle: following it lands on an unrelated
sentence, which is exactly the shape that produced the red this whole thread started from.

`anchoredTextLine`'s own comment says the looseness "costs no discrimination the tier needs",
resting the argument on "a 70-character prefix of a real source line is not a plausible random
collision". **Measured, that argument holds for the corpus as it stands** (§5), but the premise
that the recorded text is a ~70-character line is false for a measured minority.

## 5. Residual (ii) — the text rung is looser than it reads (measured size: 38 rows)

The match is a **whitespace-normalised substring anywhere in the file** (`anchorTextKey`, line
264: `strings.Join(strings.Fields(s), " ")`). Two measured facts.

**Satisfying lines per row** (`artifacts/anchor-tiers.py`, L1): 193 rows are pinned by exactly one
line, and **38 (16%) are satisfied by two or more** — up to 1150 distinct lines for a recorded
text of `}`. Those rows are *underdetermined*: several lines keep them green, and the harness
picks one.

**Mutation proof** (`artifacts/anchor-false-green-probe.py`): blank the single line the gate
currently reports, re-apply the gate's rule — **38 of 38 such rows still resolve**, on a different
line, with nothing reported. So a row survives the deletion of the very line its cell recorded.
(Scope, measured: this mutates the harness's own computation in memory; it is not a claim that a
file edit would be caught or missed — the rule that matters is the next paragraph.)

**Two limits on the size of this hole, both measured, so nobody overstates it.**
1. An earlier pass of this probe blanked *every* matching line and reported **0 survivors** — the
   mutation was self-defeating (with only matches blanked, no other line can match). The count is
   38 (one line at a time), not 0 and not 231.
2. Requiring a **start-of-line prefix** (the cheapest tightening, and closer to what the code's
   comment describes) would tighten **1 row of 38**: for 17 of them the text is a start-of-line
   prefix of *every* satisfying line anyway (`}` and `//` carry no column), so start-anchoring
   buys nothing there. Measured, `artifacts/anchor-drift-shape.py` §3.

**Where the weakness is concentrated** (`artifacts/anchor-tiers.py`, L1b): of the 38 multi-line
rows, **26 (68%) have a one-word recorded text** (`}`, `//`, `return nil`); of the 193 pinned rows
only 1 (1%) is one word. The over-broad rung and the degenerate rung are the same rows.

## 6. The fork — with each option's measured cost

| | option | measured cost | measured benefit |
|---|---|---|---|
| **(a)** | **accept as is** | 0 | 0 — the 153 numbers stay wrong-as-navigation; the 38 underdetermined rows stay underdetermined |
| **(b)** | **whole-corpus re-derivation** (`classify.py .`) | 666 changed lines of a DATA file; 132 recorded texts rewritten, 30 numbers moved, 9 rows re-classed; numbers consistent for days-to-weeks | 0 on either residual (§3c); it re-couples today and drifts again |
| **(c)** | **tighten the text rung** (start-of-line prefix) | does not fix the biggest measured class — 17 of the 38 multi-line rows are start-of-line prefixes of every satisfying line, and 26 are one-word texts | would tighten 1 of 38 rows unless the rung is changed to a *token-count floor* |
| **(c′)** | the same option, stated so it is decidable: a **minimum normalised word count** (e.g. ≥3) for a recorded text | a floor of ≥3 words would make **26 of 231 rows (11%)** non-resolvable as written → those cells must be **re-derived**, and the generator's `[:70]` truncation must stop being able to emit `}` / `//` at all (a generator change, i.e. option (b)'s machinery, scoped to 231 rows instead of 2295) | retires the rung that makes the tier falsifiable-by-accident; the measured concentration (68% of weak rows are one-word) says the floor is aimed at the real weakness |
| **(d)** | **gate the number** (fail when the cell's line does not carry the text) | **153 rows red on the day it lands** — every one of them healthy today (Q1 = 0) — so it cannot land without (b) first, and (b) drifts again | closes residual (i) honestly |

**Recommendation (this run's, recorded as an opinion — not a decision):** take **(a) now**;
treat **(b) as unattached** to any repair — it is a re-classification of a DATA file that buys no
gateable property; and if the owner wants the tier to mean something, the decidable change is
**(c′) with (d) after it**, in that order, because (d) without (c′) lands 153 red rows for no
gain and (b) without (c′/d) changes 666 lines and strengthens nothing.

**Why (c′) and (d) are not this run's to take** (Rule 28): both change behaviour of
`internal/docclass/evidence.go` and `cmd/agent/merge_union_rows.go` — the merge/verify gate, i.e.
the system layer. This run measures; the owner decides. Filed as
`01a12d4e-…` (§8) for the system-layer owner (**peerbox**), carrying these evidence paths.

## 7. Measured vs inferred

**Measured** (each with its command and raw output in `artifacts/`): the Q1/Q2/Q3 counts and the
231-row inventory; the number's provenance (132/153 `NAMED-EXACT`); the fresh-generation delta,
both simulated (70% of cells) and real (666 lines / 132 texts / 30 numbers / 9 tiers); the
post-generation tier census; the multi-line counts (38 rows, k up to 1150) and the one-line
mutation survivors (38/38); the word-count concentration (26 of 38 vs 1 of 193); the distance
distribution (max 2380); the prefix analysis.
**Inferred** (labelled, not load-bearing): that a re-derivation's benefit decays with the next
code move — that follows from the §2 mechanism, and the mechanism is measured, but the decay
itself was not timed.
**Not measured / open**: the `16 NAMED-OTHER` rows (§2) — a doc naming the *same basename at a
different number* is either a stale cell or an evolved prose anchor, and this run did not decide
which; and the 9 `TIER-CHANGED` rows of §3b, whose new tier was not inspected row by row.
**Earlier figure now corrected in place**: `01a12d1e`'s "generated by SEARCH" reading of the
generator is wrong — see `classify.py:528` and `artifacts/doc-body-scan.py`.

## 8. Acceptance checks (this run, at `3db654fb7`)

Run for real, raw output in `artifacts/acceptance-gates.txt` (`git status --porcelain` empty
immediately after):

```
$ go test ./cmd/agent/ -count=1 -run TestMergeUnionRowCheckOnTheRealManifest
    merge_union_rows_test.go:138: devdoc/DOC-INDEX.tsv: 730 normative row(s) resolved cleanly against the repository
ok  github.com/nick/gopi/cmd/agent
$ python3 tools/docclass/classify.py . --check
0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv          # exit 0, writes nothing
$ gopi tool doccheck
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
```

Evidence bundle (scripts + raw output, re-runnable, each verified deterministic by a second run
into a separate file and `cmp`): `/home/user/.gopi/sessions/01a12d21-6193-7cc2-b0e4-d4925cd84d14/artifacts/`
— `anchor-tiers.py|.txt`, `anchor-provenance.py|.txt`, `anchor-drift-shape.py|.txt`,
`anchor-false-green-probe.py|.txt`, `anchor-regen-cost.py|.txt`, `anchor-recouple.py|.txt`,
`doc-body-scan.py|.txt`, `classify-only-dryrun.txt`, `whole-corpus-write.diff`,
`real-write-anchor-delta.txt`, `information-content-before-after.txt`, `acceptance-gates.txt`.
