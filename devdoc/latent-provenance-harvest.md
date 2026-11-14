<!-- doc-class: normative -->
# Latent Provenance Harvest — Rule 16 Follow-up (2026-09-11)

**Branch:** `feat/latent-provenance` · **Work tree:** clone at `~/workspace/gopi-latprov`
(base: master `fb1b6887`; master moved to `c9e37b53` mid-task — it merged an earlier
latent-prov state and a stylegate ceiling audit, neither of which touches the
findings below) · **Branch tip:** `e24284a5` → **revision 2:** `fix/latent-provenance-measured`

**Recovery:** `git show feat/latent-provenance:devdoc/latent-provenance-harvest.md`
(also pushed: `git push origin feat/latent-provenance`).

> **Revision 2 (the corrections below are already folded in).** A re-verification
> pass re-measured every number in this report and found FOUR that did not
> reproduce — the residual decomposition, the content cross-check total, three
> spot-check cells, and the cited backup filename. All four had the same root
> cause: a figure a human typed where the program should have computed it. Both
> numeric classes are now computed and printed by the binary on every run, and
> the bases are pinned by a regression test. See *Revision 2* under Verification.
> The honest headline is unchanged: **90 rows newly recoverable, 412 untraceable.**

## The question, and the answer

The prior provenance pass (Rule 16, `internal/tools/memory_provenance.go`,
`devdoc/distilled-provenance-governance.md`) added `source_type` and measured that
**495 of the 500** rows whose free-text `sources` is empty nonetheless name a
path-shaped token in their own topic/question/answer. It left the semantic
question open and did NOT act on the number.

This pass decided that question first:

> Is an in-text path the row's **ORIGIN** (where the knowledge came from) or only
> its **SUBJECT** (what the row is about)?

**Answer: it is sometimes the origin and sometimes not, and the two are separable
by a measurable shape — so the harvest is real, but it is much smaller than 495.**

The discriminator is the **declared subject heading**:

| | shape | verdict |
|---|---|---|
| **ORIGIN** | the path *heads the topic* as a declaration — `hermes_cli/main.py — CLI dispatcher & update/TUI machinery` — **and** the row's own question or answer repeats it | harvest |
| **SUBJECT** | the path is *mentioned inside a sentence* — `补法（已在 gopi internal/tools/memory.go 实现并测试）` (the row is about CJK retrieval; memory.go is merely where the fix landed) | refuse — recording it would be a fabrication |

## The measurement that justifies the rule

A claim about provenance is exactly the kind that gets fabricated, so the rule was
**calibrated against the rows whose origin is already recorded** — the
`source_type='local_path'` population. The rule's signature was applied to both
populations and the resolve rate measured (`gopi memory provenance --latent-calibrate`):

| population | rows | path resolves to a real file on disk |
|---|---|---|
| **RECORDED** (`source_type='local_path'`) | 567 | **370 → 65 %** |
| **HARVESTED** (empty sources, declared heading) | 90 | **90 → 100 %** |

A signal that resolves **more** often than the human-recorded column is not a
weaker guess than the recorded data — it is a stronger one. (The recorded column
is not a gold standard: rows like `~/work/GoPi/TASK-END-STORE-REPORT2.md`,
`docs/developers/LLM_AI_Roadmap.md`, `AI/Nullkiller2/AIGateway` name artifacts that
have since moved or never existed.)

Content spot-check on the harvested rows (not just path existence — do the
identifiers the answer names actually live in that file?). This is a PROGRAM,
not a hand count: `latentContentAgreement` tokenizes the row's own answer,
keeps the symbol-shaped words (snake_case / camelCase), and counts how many
occur verbatim in the harvested file:

| row | harvested path | identifiers named by the answer that exist in the file (measured) |
|---|---|---|
| 17 | `hermes_cli/main.py` | 10/10 (`cmd_chat`, `cmd_dashboard`, `Startup`, `Termux`, …) |
| 924 | `internal/agent/clock.go` | 4/4 (`SessionClock`, `NewSessionClock`, `Elapsed`, `TickLabel`) |
| 969 | `cmd/agent/main.go` | 32/36 (`FlagOverrides`, `ApplyEnv`, `FromSettingsFile`, …) |
| 22 | `hermes_cli/models.py` | 33/33 (`ProviderEntry`, `detect_provider_for_model`, …) |

> **Correction (this revision).** This table's first cut showed 11/11, 16/19 and
> 24/25 — three of four figures that do not reproduce under the now-stated rule
> (measured: 10/10, 32/36, 33/33). Same root cause as the residual split: a
> number chosen by hand rather than computed. `latentContentAgreement` is the
> fix, and the full 10-row table below is regenerated from its output.

## What was written (additive only)

Three **new** columns on `distilled_qa`, additive, never overwriting anything:

| column | meaning |
|---|---|
| `source_type_latent` | the harvested classification (same `local_path` vocabulary as `source_type`) |
| `provenance` | the **provenance marker**: `harvested` / `recorded` / `none` |
| `provenance_evidence` | for `harvested`, the exact path that earned the label — so a reader can re-check the derivation from the row alone |

`provenance` is what makes the harvested-vs-recorded distinction **explicit**:

- `harvested` — the label was DERIVED from the row's own text. Never a recorded origin.
- `recorded` — the row already had an addressable `sources`; the latent column
  **restates** `source_type` so the two are comparable side by side (it never
  competes with it).
- `none` — no recorded origin **and** no defensible signal. The honest untraceable bucket.

### The confidence marker, and the calibration behind it

The marker IS the confidence, and it is backed by a measurement rather than an
assertion — the signal was calibrated against the only ground truth available,
the 567 rows whose origin a human recorded (`source_type='local_path'`):

| marker | what it means | confidence, on what basis |
|---|---|---|
| `recorded` | the row's `sources` names an artifact | **recording confidence** — but measured only 370/567 (**65 %**) of those paths still resolve on disk, so "recorded" means "a human recorded it", NOT "it is verifiable". That is exactly why nothing here overwrites it or treats it as ground truth. |
| `harvested` | the origin was DERIVED: the topic declares a file-shaped path heading and the row's own question/answer repeats it verbatim | **high** — 90/90 (**100 %**) of the harvested paths resolve to a real file, and the row's own answer agrees on content (159/163 identifiers, 97.5 %). A signal that resolves *more* often than the human-recorded column is not a weaker guess. |
| `none` | no recorded origin and no defensible signal | **explicitly unknown** — 412 rows. Not "probably nothing": the row's origin is not recoverable from its own text by any rule this work could defend, and 195 of the 412 demonstrably DO name an artifact (as a subject, not as an origin). |

The three markers are mutually exclusive and exhaustive (`provenance` is set on
every row; the harvest's second pass writes 0). The confidence of a `harvested`
label is deliberately NOT a per-row score: a score would invite treating 0.51 as
"use it" and 0.49 as "ignore it" — the rule either defends the row or refuses it,
and `provenance_evidence` names the exact token that earned the label so a reader
can re-check the derivation from the row alone.

**`sources`, `source_type`, topic, question, answer and `search_kw` are untouched** —
proven by a SHA-256 fingerprint over `id|hex(topic)|hex(question)|hex(answer)|sources|search_kw|source_type`
for every row, taken before and after the write, and by the unit test's
column-by-column diff.

> The fingerprint is a before/after COMPARISON of one run, not a global constant:
> the live table grows under concurrent writers, so its absolute value changes
> between sessions (measured on this box: `ac57ecdb…` on the 1861-row pre-write
> snapshot, `c3404795…` on the live table a few hundred rows of churn later).
> Only the equality of the
> before/after pair within a single run is meaningful, which is what
> `scripts/latent-provenance-harvest.sh` asserts
> (`protected-column fingerprint before == after`). A fixed value quoted in prose
> would be a sixth un-reproducible number, so none is quoted.

**Row-by-row additivity, verified against the true pre-write snapshot.** The
fingerprint proves one run did not move a protected byte; this proves it for the
PRODUCTION table as it now stands. Comparing
`~/.gopi/memory.db.bak-latprov-20260911-125436` (1861 rows, no latent columns)
with the live DB, on the 1861 rows the two share:

```
pre rows=1861  live rows=1869  common=1861
PROTECTED COLUMNS CHANGED on pre-existing rows: 0 []
rows added by other sessions since: 8
```

Zero protected-column changes — `topic`, `question`, `answer`, `sources`,
`search_kw` and `source_type` are byte-identical on every row that existed before
the harvest. (The 8 new rows are other sessions' inserts, and the harvest labels
them on its next run; that is the intended behaviour, not a rewrite.)

## How many rows are newly recoverable — and how many are not

Production DB. The harvest ran at **1861 rows**; other agent sessions kept
inserting throughout, so the live table reads **1869** by the end of this revision.
Both views, since the corpus grows under the work: the harvested population is
fixed at **90** (a row's topic text does not change), while `recorded` absorbs
every new row — which is why `recorded` is a moving number and the two columns of
the table below are a snapshot pair, not a constant.

| | at the harvest | live (end of this revision) |
|---|---|---|
| total | 1861 | 1869 |
| recorded origin (`sources` non-empty) | 1359 | 1367 |
| **empty `sources`** | **502** | **502** |
| — **newly recoverable** (`provenance='harvested'`) | **90** | **90** |
| — untraceable (`provenance='none'`) | 412 | 412 |
| unlabelled (no verdict yet) | 0 | 0 |

Harvested detail (measured on the live table): 45 `internal/*` (GoPi), 11 `cmd/*`,
31 `hermes_cli/*`, 3 `gateway/*`; 56 resolve into the GoPi tree, 34 into the hermes
trees.

### The 412 that remain untraceable — stated plainly, and MEASURED

The split below is not eyeballed: it is computed with the harvest rule's OWN
predicate over the live corpus (`latentHarvestRow` + `latentPathTokens`, the
same functions the write uses) AND PRINTED BY THE BINARY ITSELF
(`gopi memory provenance --latent-candidates`, lines 4-5):

```
  unharvestable(no origin, no defensible signal)=412
    = no path token at all=10 + token not file-shaped=193 + mentioned only (subject, not origin)=195 + declared but not repeated=0 + bare filename declared+repeated=14
```

Of the 502 empty-`sources` rows:

- **90** declare a file-shaped path as their topic heading AND repeat it in their
  own question/answer → harvested.
- **412** do not — and every one of them is `provenance='none'`, deliberately,
  for one of five MEASURED reasons:
  - **195** name a file-shaped path *somewhere* in their text but only as a
    mid-sentence MENTION (`补法（已在 gopi internal/tools/memory.go 实现并测试）`).
    The row is about a technique, not about that file — filling it would be a
    fabrication.
  - **193** name path-shaped tokens, but none carries a file extension — a
    slash-joined attribute list like `env_int/env_float/env_bool` is
    path-shaped by the character class yet names no file.
  - **14** declare a BARE FILENAME heading and repeat it (`hermes_time.py —
    timezone-aware clock`). The rung was considered and refused: a basename is
    not ADDRESSABLE (utils.py exists 178 times in one tree, cli.py 30 times,
    memory.db 4 times), so it fails Rule 16's "能被下一个任务检索到" test.
  - **10** name no token of the path class at all (e.g. `smoke topic`,
    `HOMM3 wine 自动化窗口焦点与热键`).
  - **0** declare a file-shaped heading and then fail to repeat it (measured 0
    in the live corpus; `TestLatentHarvestRequiresRepeat` pins this branch as
    the anti-fabrication guard).

**Honest summary: 90 of 502 empty-origin rows (17.9 %) were newly made
traceable. 412 remain untraceable by any defensible signal — 195 of them name an
artifact but never DECLARE it as their origin, and this report does not claim
otherwise.** (Note the difference from the previous pass's headline: that pass
counted "names a path somewhere" by a loose substring test and reported 495 of
500; the stricter heading test this work actually implements fires on 90.)

> **Correction (this revision).** The first cut of this section carried a
> hand-typed decomposition (401 mention-only + 11 no-path) that does NOT survive
> re-measurement — the rule's own predicate yields 195 + 193 + 14 + 10, which is
> what the binary now prints on every run. The root cause was the same in both
> places it appeared: a number a human typed instead of a number the program
> computed. The fix is structural, not editorial — the residual is now computed
> by `tallyLatentResidual` and pinned by `TestLatentResidualBasesSum`, which
> fails if the bases stop summing to `Unharvestable`.

The motivation for saying no so often: a fabricated origin is worse than an absent
one — it is *retrievable*, so a downstream task would act on it, and the wrong
artifact would look exactly like the right one.

## The re-runnable query (deliverable a)

```bash
./bin/gopi memory provenance --latent-candidates | sed -n '/^WITH/,$p' | sqlite3 -readonly ~/.gopi/memory.db
# → 90   (the same 90 the Go rule defends)
```

The printed SQL is **rendered from the same Go constants** the harvest uses
(`latentFileExtensions`, `latentTokenTrim`) and is pinned by
`TestLatentCandidateSQLMatchesTheRule`, which runs the printed SQL against the
seeded DB and fails if it disagrees with the Go predicate.

The query sums to the table by construction — the report prints every population
and asserts `recorded + empty_sources = total` and
`harvestable + unharvestable = empty_sources`.

## Idempotency (acceptance c)

Run by `scripts/latent-provenance-harvest.sh` on a copy of the pre-harvest
production DB (the script runs the harvest TWICE and fails if the 2nd pass writes
anything). Full transcript: the run below is on
`~/.gopi/memory.db.bak-latprov-20260911-125436` (1861 rows, no latent columns —
a genuine pre-write snapshot: it answers `no such column: provenance`):

> Naming note (this revision): the snapshot's suffix is `.bak-latprov`, while the
> current script writes `.bak-latent-provenance` (line 35) — the suffix was
> lengthened mid-task for readability and this citation was not updated with it,
> so the report named a file that does not exist. The path is corrected here; a
> fresh run of the script produces the longer name.

```
== pass 1   rows=1861 … harvested(written this pass)=90    rows written this pass=1861  already labelled before this pass=0
== pass 2   rows=1861 … harvested(written this pass)=0     rows written this pass=0     already labelled before this pass=1861
```

**Second pass writes 0 rows.** The anchor is the `provenance` marker being unset —
deliberately NOT the latent *type*, because a `none` row legitimately has an empty
type; anchoring on the type re-wrote 426 rows on every pass (measured, and fixed
before commit). The script also **aborts + restores the backup** if the second
pass writes anything or if the protected-column fingerprint moves (the fingerprint
was byte-identical across the run).

### Idempotency on a LIVE corpus (the honest caveat)

`/home/user/.gopi/memory.db` is production: other agent sessions write to it *while* this
work runs. The corpus was **1861 rows** at the harvest and **1869** by the end of
this revision — other sessions keep inserting while this is written, which is
evidence in itself that the harvest must be re-runnable.
Re-running the harvest against the grown corpus reported `rows written this pass=3`
— exactly the three rows inserted since the previous pass, which had no latent
verdict yet:

```
harvest-latent (live, after a backup to .bak-latprov-20260911-134025)
  rows=1868  recorded=1366  empty_sources=502  heading_shaped=90   (1869 / 1367 moments later)
  harvested(written this pass)=0    rows written this pass=3    already labelled=1865
next run
  rows written this pass=0          already labelled=1868
live state
  provenance: harvested=90  none=412  recorded=1366      unlabelled=0
  (the totals keep climbing as other sessions insert — 1869 rows / recorded=1367
   by the time this line was written; the invariant is unlabelled=0)
```

Every row now carries a verdict and a further pass is a provable no-op
(`rows written this pass=0`).

So the idempotency claim is precise, and worth stating in its precise form:
**a row is written at most once; a re-run writes 0 rows for every row that already
carries a verdict.** A re-run after the table grew writes exactly the new rows —
which is not a violation but the intended behaviour (a new row deserves a verdict),
and the operator script's `rows written this pass=0` assertion must therefore be
read as "0 rows that already had a verdict". The test proves the strict per-row
form on a private DB (`TestLatentHarvestIsAdditiveAndIdempotent`: second pass
`Written=0`).

**Why the live path is filled by the binary and not by the script:** the script is
correct for a private copy (backup → pass 1 → pass 2 → restore on failure), but on a
LIVE corpus it can lose a concurrent writer's row — pass 2 legitimately writes a row
another session inserted between the passes, the script reads that as an idempotency
failure and restores the backup over it. On the live DB the harvest was therefore run
directly (backup taken first, `.bak-latprov-20260911-134025`), where the honest
per-row form holds and no write is ever overwritten. The script remains the
re-runnable operator path for a copy.

## Where the work lives

| artifact | path |
|---|---|
| harvest implementation | `internal/tools/memory_provenance_latent.go` |
| regression suite (7 tests) | `internal/tools/memory_provenance_latent_test.go` |
| CLI modes | `cmd/agent/cli_memory.go` — `--latent-candidates`, `--latent-calibrate`, `--harvest-latent`, `--latent-crosscheck` |
| operator script (double pass + backup/restore) | `scripts/latent-provenance-harvest.sh` |
| this report | `devdoc/latent-provenance-harvest.md` |

## 10-row manual cross-check of harvested vs recorded

The sample is the first 10 harvested rows by id, printed by
`gopi memory provenance --latent-crosscheck`. Every figure below comes from that
output (path resolution AND the content agreement the binary now computes), so
the table can be regenerated rather than re-trusted:

```
  id=17    harvested local_path:hermes_cli/main.py      recorded=""  on-disk
      content: 10/10 identifiers of the answer found in the file
  ...
```

| # | row | harvested path | resolves to | content check (answer identifiers found in that file) | recorded `sources` | verdict |
|---|---|---|---|---|---|---|
| 1 | 17 | `hermes_cli/main.py` | hermes-upstream, 14268 lines | 10/10 | `""` | ORIGIN |
| 2 | 18 | `hermes_cli/config.py` | 6072 lines | 22/23 | `""` | ORIGIN |
| 3 | 19 | `hermes_cli/auth.py` | 9459 lines | 12/12 | `""` | ORIGIN |
| 4 | 20 | `hermes_cli/gateway.py` | 8428 lines | 15/15 | `""` | ORIGIN |
| 5 | 21 | `hermes_cli/tools_config.py` | 5973 lines | 21/21 | `""` | ORIGIN |
| 6 | 22 | `hermes_cli/models.py` | 6981 lines | 33/33 | `""` | ORIGIN |
| 7 | 23 | `hermes_cli/setup.py` | 3565 lines | 22/22 | `""` | ORIGIN |
| 8 | 24 | `hermes_cli/kanban_db.py` | 12139 lines | 15/15 | `""` | ORIGIN |
| 9 | 25 | `hermes_cli/cli_commands_mixin.py` | 3919 lines | 4/5 | `""` | ORIGIN |
| 10 | 26 | `hermes_cli/cli_agent_setup_mixin.py` | 933 lines | 5/7 | `""` | ORIGIN |

Every one of the ten was additionally verified by hand against the tree
(`~/work/hermes-upstream`), one row at a time — not merely "the path exists":
the identifiers the answer NAMES were re-looked-up inside the harvested file, so
the check measures *content* provenance, not just path existence. The line counts
above were verified with `wc -l` (71737 lines in total across the ten files).

**Content totals: 159 of 163 identifiers (97.5 %) named by these ten answers
exist verbatim in the harvested file.** The 4 misses are the module basename
itself (`cli_commands_mixin` / `cli_agent_setup_mixin` — a filename rather than
a symbol) and one genuinely absent symbol from each of rows 18 and 26
(`is_uv_tool_install`, `run_agent`): a row describing a file's surface will
always carry a stale or cross-file reference, which is why the standard is
content agreement, not perfection.

> **Correction (this revision).** The first cut of this section stated a headline
> of "199 of 203 identifiers (98 %)" — a number that does NOT reproduce: the
> table's own per-row counts sum to **159/163 (97.5 %)**, and no stated rule
> (backticked spans, 3-char or 4-char symbol-shaped words) yields 199/203 from
> these answers. The per-row figures were right; the aggregate was hand-added
> wrong. The programmatic form above is the durable fix — the reader can re-run
> the check instead of trusting a total.

## The rung that was considered and REFUSED: bare filenames

The deliverable asks for the honest negative result, so this is one: **14 rows
declare a BARE FILENAME as their heading and repeat it** — `hermes_time.py —
timezone-aware clock`, `cli.py — TUI front-end…`, `GOPI.md …`. Structurally these
rows look like the 90 harvested ones: the topic leads with a filename, the row's
own text repeats it. They are still refused, because the token is not
ADDRESSABLE — it does not tell the next task WHICH file to read:

| declared basename | files of that name in the probed trees |
|---|---|
| `utils.py` | 178 |
| `cli.py` | 30 |
| `memory.db` | 4 |
| `hermes_time.py`, `run_agent.py`, `hermes_state.py`, … | 1 in hermes-upstream, 1 in hermes-agent (two different files) |
| `settings.json` | 0 (the artifact no longer exists) |

Rule 16's provenance contract is that the recorded origin must be retrievable
("能被下一个任务检索到") — a basename satisfied by 178 candidates does not
satisfy it, and picking one would be a guess dressed as a record. The binary
therefore reports the rung (`residual_bare_filename=14`) instead of acting on it,
and the count is part of the 412.

Every row in the table is an ORIGIN by the rule: the path heads the topic as a
declaration, the row's own question or answer repeats it, `sources` is empty
(nothing overwritten), and the file is on disk.

Reproduce the sample: `gopi memory provenance --latent-crosscheck`.

### Counter-samples that were deliberately REFUSED (mention, not origin)

- row 5 — `SQLite FTS5 trigram CJK retrieval在 Go memory 引擎`: the answer says
  `已在 gopi internal/tools/memory.go 实现并测试`. The row is about a *technique*;
  `memory.go` is where it happened to be implemented. → `none`.
- row 1825 — `Merge-conflict resolution in a gate allow-list (internal/standards/baseline.go): …`:
  the path is in the topic but not as a heading, so the loose LIKE mirror counted
  it (91 vs the rule's 90) while the rule correctly refuses it. → `none`.
- row 1041 — `codex tool_search BM25 dynamic tool discovery vs gopi full context`:
  mentions `codex-rs/core/src/tools/handlers/tool_search.rs`, an artifact of a
  *different* project that is not on this box. → `none`.

## Verification

```
go build ./...            : OK
go test ./internal/tools/ -run TestLatent -count=1
  TestLatentHeadingPredicate          PASS   (11 heading fixtures, 5 refusal cases)
  TestLatentHarvestRequiresRepeat     PASS   (the anti-fabrication test)
  TestLatentHarvestIsAdditiveAndIdempotent PASS (protected-column diff + 2nd pass writes 0)
  TestLatentCandidateSQLMatchesTheRule PASS  (printed SQL == Go predicate; populations sum)
  TestLatentReadModeNeverWrites       PASS   (a report never ALTERs the DB)
  TestLatentVerdictsAreDocumented     PASS
  TestLatentResidualBasesSum          PASS   (the five refusal bases partition Unharvestable)
go test ./...             : GREEN — every package ok, including internal/stylegate
go test ./internal/stylegate/ : ok
```

### Revision 2 (this commit) — the numbers are now computed, not typed

The re-verification that produced the corrections above:

```
go build ./...                                    : OK
go test ./internal/tools/ -run TestLatent -count=1 : ok
  TestLatentResidualBasesSum  PASS  (NEW — the five refusal bases must partition
                                     Unharvestable; each fixture lands in its base)
go test ./...                                     : GREEN — 20+ packages ok, no FAIL line
go vet ./...                                      : clean

scripts/latent-provenance-harvest.sh on the pre-write snapshot:
  pass 1  harvested(written this pass)=90   rows written this pass=1861
  pass 2  harvested(written this pass)=0    rows written this pass=0
  protected-column fingerprint before == after (identical)

  unharvestable=412 = no path token 10 + token not file-shaped 193
                    + mentioned only 195 + declared not repeated 0
                    + bare filename 14            (what the binary now prints)
```

The four shipped numbers this revision corrects, and where each was wrong:

| claim | claimed | measured | root cause |
|---|---|---|---|
| residual decomposition | 401 mention-only + 11 no-path | 195 + 193 + 14 + 10 | hand-typed |
| content cross-check total | 199/203 (98 %) | 159/163 (97.5 %) | hand-added; the per-row cells were right |
| spot-check rows | 11/11, 16/19, 24/25 | 10/10, 32/36, 33/33 | hand-typed |
| backup filename | `.bak-latent-provenance-…` | `.bak-latprov-…` | the script's suffix was lengthened mid-task and the citation was not |

Both numeric classes are now computed by the program (`tallyLatentResidual`,
`latentContentAgreement`) and printed on every run, so a future drift shows up in
the output instead of in a re-read of the prose.

### The stylegate package — found red, left green

The acceptance criterion is `go test ./...` green, and `internal/stylegate` was
**red on master**. Checked honestly in a detached worktree at master
(`~/workspace/gopi-mastercheck`) rather than trusted:

```
master c9e37b53  : internal/stylegate FAIL
  stylegate errorf-verb: 10 production hit(s), ceiling 9
    … including internal/tools/memory_provenance_latent.go:747 (%v wraps an error)
```

The one site in THAT list belonging to this work was fixed for real (not
suppressed): the calibration probe's scan-error path wrapped a `cerr` with `%v`.
It now uses `errors.Join(err, cerr)` inside a single `%w` — BOTH errors stay in
the chain and `errors.Is` finds either, which is what the `%v` form silently
lost. That is a genuine correctness improvement in a path that previously
reported only one of two failures.

The two test sites this branch had added to the unchecked-close overflow
(`internal/tools/memory_provenance_test.go` rows.Close() / rows2.Close()) now
check and report their error (`t.Fatalf("… close: %v")`) instead of discarding
it — again a silent-failure fix, not a raised ceiling. The merge that landed in
master mid-task (`c9e37b53`) audited the remaining legacy test sites separately,
so the final comparison is:

```
master   c9e37b53 : internal/stylegate FAIL  (errorf-verb 10 > ceiling 9)
branch e24284a5 : internal/stylegate ok
                   go test ./... : GREEN — every package ok, no FAIL line
```

This work ADDS zero findings (the diff of the gate's site list between master and
this branch was byte-identical before the fixes) and REMOVES three.

Production write (backup `~/.gopi/memory.db.bak-latprov-20260911-125436`,
sha256 verified identical to the pre-write DB — the `provenance` column does not
exist in it, so it genuinely predates the write).

## Design decisions worth remembering

1. **A separate column, not a backfilled `source_type`.** Inferring an origin into
   the same column that holds measured origins would destroy the very distinction
   this task exists to create.
2. **The rule is deliberately PARTIAL.** Unlike the recorded-source ladder (which
   must partition every row), this rule refuses what it cannot defend; the refusal
   is the feature.
3. **`provenance='recorded'` restates `source_type`** rather than leaving the
   latent column empty, so "harvested vs recorded" is readable without a join.
4. **Classification never touches the filesystem.** Path resolution is used for
   *reporting* the resolve rate only — a provenance label must be a property of the
   row, not of today's checkout.
5. **Read modes never heal the schema.** The first cut called the column heal from
   the read-only candidate report and died with `attempt to write a readonly
   database` on the production DB; a report must never `ALTER` the knowledge base.
