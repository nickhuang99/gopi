<!-- doc-class: historical -->
# Memory-consult enforcement: adoption telemetry, accountability checkpoint, historical case list, and the GO/NO-GO on the mid-task hook

Epic: make "consult memory BEFORE acting" harness-enforced, and decide by
MEASURED data whether the two remaining variants are worth building.
Branch: `feat/memory-consult-enforcement`. Commits: `821c239` (D1+D2 impl),
`76b7969` (tests), `36424f0` (docs anchors + audit/scenario scripts),
`cdc9371b` (bounded historical audit + measurement report), `ed06b907`
(the zero-hit population measurement + kill-switch live proof), `d1eefe28`
(D4 verdict contradiction resolved — both candidate-B forms NO-GO), `44f8906f`
+pinned verification outputs, `cbf17336` (what the checkpoint has produced so
far), `4745097a` (master line reference for candidate A), `6ff7c6d0`
(per-candidate verdict table with the number each verdict rests on),
`eace8282` (cost table corrected by a reproducible probe — the requests column
was 2× and the +199 belonged to the task-start leg), `963dbe1e` (raw-SSE
double-print risk + the corrected probe recorded), `354df3eb`
(`scripts/memconsult-checkpoint-scan.py` — D1 re-measured reproducibly: marker
count, marker→reply adjacency, fixture separation), `0a08321b` (the zero-hit
detector committed + marker counted as INJECTIONS not grep hits + D1 & cost
re-measured), `b6ed3ea4` + `943a15f2` (verdict row, six-defect ledger, tally).
The final verification pass over this branch found **six measurement defects,
every one in an instrument rather than in a mechanism** — see "Not Done" and the
risk register; the numbers below are the post-fix measurements.

## Completed

### Per-candidate verdict (acceptance requirement 6 — each claim with its number)

| candidate | verdict | number it rests on |
| --- | --- | --- |
| A — forced pre-action retrieval | **DONE (on master, not rebuilt here)** | report-time run: injection present in 290 scanned logs / **284 of 288 work runs**, of which **282 issued a real memory search**; the classes partition the 288 work runs exactly — `started-with-search` 281 + 1 `stored-unconsulted` (searched late) = 282 searched, and the 6 that never searched are 4 enforcement-off legacy runs + 2 忘記先查 |
| C — review-time accountability checkpoint (D1) | **DONE** | 14/14 real marker runs replied at **marker line +1**; **injection** ×1 in 36/36 marker logs (one-shot — measured injection-only, the run's own quotes counted separately); **+0 prompt tokens at request 1** (the message enters after the first commit and is cache-read thereafter; the 586-char injection is one line, once per run — see the corrected cost probe) |
| D2 — adoption telemetry | **DONE** | session `893660e9`: `rows_returned=5 rows_consumed=1`; session `c67df866`: `feedback id=1867` counted as consumed |
| D3 — historical 忘記先查 audit | **DONE** | 忘記先查 = **2 runs / 41 396 tokens** in a 400-log / 14-day window (after fixing a detector defect that had invented 341 runs / 5.1 B tokens) |
| B — mid-task déjà-vu hook | **NOT DONE — 不做, by measurement** | round-count trigger fires on **119 of 288 work runs (41%)** at `stuck@3` (186, 65%, at `stuck@2`); its "covering row" rate **50–69%** is topic overlap (hand-audited false positives); the narrower zero-hit form: **6 runs, 1 with a streak, 0 with a covering row** — and that one run's own informed re-query one round later did not prevent its 145-round tail |

### Candidate A — already DONE on master; NOT rebuilt here (as instructed)

`maybeInjectTaskStartSearch()` (internal/agent/loop.go:3503 on master, called on every
start-of-run path, default ON, off via `GOPI_NO_ENFORCE_MEMORY`) injects
`taskStartSearchMessage` plus the hot-tier digest without any LLM volition.
Measured in the live log window (report-time, post-fix): **290 of 400 scanned
logs carry the injection**, and **282 of 288 work runs** (281 classified
`started-with-search` + 1 `stored-unconsulted`, which searched late) issued a
real memory search; of the 284 work runs that carried the injection, 282
searched — the 2 that did not are the 忘記先查 class itself. This is the
baseline the report's numbers are compared against; candidate A was not touched.

### D1 — Review-time accountability checkpoint (candidate C, the data instrument)

`maybeInjectConsultCheckpoint()` (internal/agent/loop.go) asks the one-shot
question `consultCheckpointMessage` when the run's **first item commits**:

> `[MEMORY-CONSULT CHECKPOINT] This round: which work could a memory search have
> saved? Answer in ONE line (or "none — prior knowledge was already
> sufficient") naming (a) what you recomputed, re-explored or re-derived that
> distilled experience already contained, and (b) roughly what it cost … do NOT
> start new work, do NOT re-plan the task, and do NOT search again for the sake
> of the answer.`

Why the first commit is the injection point: that is the moment the run holds
real evidence about what it would have needed — earlier there is nothing to
report, later the run is already unwinding. Shape follows the proven one-shot
siblings (`maybeInjectTaskStartSearch`, `maybeEnforceCompletionStore`):

* **armed before the loop** (`armConsultCheckpoint()` on the start-of-run path)
  and released on the first call — an LLM cannot skip it, because it is not a
  prompt the model may decline, it is a context event the harness injects;
* an agent built directly (unit tests, hand-built agents) never goes through
  start-of-run, so `consultArmed` stays false and no unrelated test sees it;
* it carries its own default-conservative switch
  `GOPI_NO_ENFORCE_MEMORY_CONSULT` / `--no-enforce-memory-consult`, separate
  from the parent `GOPI_NO_ENFORCE_MEMORY`, so the instrument can be dropped for
  an A/B baseline without touching the start-of-run enforcement;
* it writes `consultCheckpointMarker` (`[MEMORY-CONSULT CHECKPOINT]`) into the
  session JSONL, so the injection is auditable after the fact — a mechanism that
  leaves no trace is unmeasurable.

**What the checkpoint has produced so far — re-measured 2026-09-11 with
`scripts/memconsult-checkpoint-scan.py`** (the reproducible form of the
paragraph that used to sit here; the script is committed so the number can be
argued with instead of believed):

```
$ python3 scripts/memconsult-checkpoint-scan.py --sessions 400 --window-days 14
scanned_logs=281  window_days=14.0
marker_logs=37  fixture_logs=23  real_runs=14
marker_occurrences min/median/max=1/1/1   (one-shot = every run carries it exactly once;
                                           an injection only — the run's own quotes of the
                                           marker are counted separately)
marker_QUOTES min/median/max=0/0/2        (grep -c counts injection+quotes; that is why the
                                           cost probe's numbers disagreed with grep)
real_runs_with_a_reply_after_the_marker=14/14   (a skipped checkpoint would be marker-with-no-reply)
marker->reply line delta: [1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1]
```

Three facts, each one a claim the acceptance criteria ask for:

1. **It cannot be skipped.** Every one of the 14 real marker runs produced an
   assistant turn at **marker line + 1** — the run replies immediately, in the
   turn the harness opened; there is no marker-with-no-reply and no marker whose
   reply came rounds later.
2. **It is one-shot.** `marker_count` is exactly 1 in 37 of 37 marker logs
   (min = median = max = 1). This is measured on **injections only**: an
   assistant event carrying the marker is the run *quoting* its own prompt back
   (arm `43888439` quotes it twice), and counting those would have claimed a run
   received the checkpoint 3× — the sixth instrument defect this report caught,
   and the reason `marker_QUOTES` is printed beside `marker_occurrences`.
3. **It fires broadly, not only on the probe tasks.** The 14 real runs are
   ordinary work runs (the A/B arm pairs, the cost-probe arms, the two Rule 11
   scenarios and their originals, plus this work's own sessions) — the marker is
   present wherever the run reached a first commit with the switch on, and absent
   exactly where the switch was off (see the kill-switch table).

**The yield question is answered by READING, not by a ratio — deliberately.**
A first version of the scan classified each reply `EXPLICIT-NONE` vs
`EXPLICIT-WORK` by substring and reported `4 NONE / 7 no-answer`; reading those
four replies falsified it — their `none` was about the TASK LIST ("none are
progressing"), not the checkpoint. Free prose classified by substring is the
"plausible number that is not the quantity it claims" this report has now caught
four times, so the script drops the classification and the yield is reported
here as the reading it is:

* **`893660e9`** — the reply audits the run's OWN missing first-step search,
  unprompted: *"this session, I did not run a memory search first (I should have
  per the routing rules) … I referenced memory #1690 in my conclusion without a
  search"*.
* **`ac391c0f`** — and four siblings — answer the checkpoint and the run's
  remaining bookkeeping in the same turn, e.g. *"Memory checkpoint: none —
  writing + byte-verifying a one-line file needs no distilled prior knowledge; no
  rounds were wasted re-deriving anything"*.
* The remaining replies answer it inside the turn's thinking block while naming
  the task's own state; none of the 11 declined to answer.

That is the honest shape: **the checkpoint is reliably delivered and reliably
answered once, and its output is auditable prose that occasionally names a real
miss (`893660e9`) and usually reports "none" for a small mechanical task** —
which is why it is priced as a case-list generator, never as a savings number.
The count is also window-dependent (marker dirs accumulate; the raw-SSE-only
ones GC), so the stable statements are the three above, not the run count.

### D2 — Adoption telemetry (the 查了不用 detector) in internal/agent/trajectory.go

New fields on `TrajectoryStats`, all derived from the session JSONL in one
ordered pass:

| field | meaning |
| --- | --- |
| `FirstToolRound` | round of the run's first tool_use |
| `MemoryFirstToolRound` | that first tool was a memory search |
| `FirstMemorySearchRound` | round of the first memory search (-1 = never) |
| `MemorySearchRounds` | distinct rounds that searched |
| `MemoryRowsReturned` / `MemoryRowsConsumed` | hits handed back vs hits a later `memory(feedback/update)` named by id |
| `MemoryEmptySearches` | searches that handed back no distilled row |
| `ConsultCheckpointInjected` | the D1 checkpoint is in the log |

The adoption signal is deliberately NARROW: prose that merely *mentions* a row
number is not consumption, only a later `feedback`/`update` naming that same id
is — a lower bound on adoption, chosen so "rows_consumed=0" can never be an
artifact of generous matching. Two search results, one process log line each:

```
memory: first_tool_round=0 memory_first=true search_rounds=1 first_search_round=0
memory adoption: rows_returned=5 rows_consumed=1 (20% consumed) empty_searches=0 search_completion_tokens=…
memory consult checkpoint injected: true
```

### D3 — Bounded historical audit: the 忘記先查 case list

`scripts/memconsult-audit.py` (stdlib only, ~600 lines) scans the newest N
session JSONLs inside a time window, streams each exactly once, and opens the
LIVE WAL `memory.db` read-only (`file:…?mode=ro` — never copied). It classifies
every run and prints the case list.

**Two measurement defects were found and fixed by re-measuring, and both would
have flipped the GO/NO-GO while looking perfectly plausible:**

1. The "newest 300 session directories" bound was not recent: the sessions root
   holds 24k directories whose *own* mtime does not move when a nested log is
   appended, so "newest 300 dirs" reached back to 2026-08-02 while 24681 of the
   picked dirs contained no log at all. Replaced with a glob over the logs
   themselves plus a `--window-days` mtime bound.
2. The detector knew only the CURRENT wire spelling of the memory search
   (`memory` + `action=search`). The memory family was merged — historical logs
   carry `memory_search` as well — so every pre-merge run scored as "never
   consulted memory" and the audit reported **341 runs / 5.1 B tokens** of
   invented 忘記先查. With both spellings matched (one `is_memory_search`
   predicate) the same window reports **2 runs / 41 396 tokens**. The artefact
   was 170× the truth; publish nothing before re-running after a detector change.

Measured result (400 newest logs, 14-day window, **288 work runs** — the
report-time run, post-fix; the classification is printed by the script as
`cases_work_runs`, whose classes sum to the work-run count):

| class | runs | note |
| --- | --- | --- |
| `started-with-search` | 281 | + 1 `stored-unconsulted` = **282 of 288** work runs that searched at all; the other 6 are the two classes below |
| `never-consulted-with-a-row-waiting` (存了没想起来查) | 2 | 41 396 tokens total, 3 and 11 rounds |
| `no-checkpoint-no-search` (enforcement off in that run) | 4 | legacy/dev runs, no injection in the log |
| `stored-unconsulted` (searched, but only after exploring) | 1 | 2 tool calls / 755 completion tokens before the search |

The two 忘記先查 runs, in full (the class is real, it is just rare in this
window — nothing was laundered into an estimate):

| session | rounds | tool calls | tokens | rows that were waiting |
| --- | --- | --- | --- | --- |
| `1bb51189-8f8d-4413-889f-0378a4037d04` | 3 | 1 | 38 398 | #1107, #999, #1006, #1040 |
| `0d77eecc-641c-4314-b3d7-1de33cae40b7` | 11 | 8 | 2 998 | #3, #413, #1006, #1066 |

**The 忘記先查 class is real but vanishingly small in the current window** — the
start-of-run leg already covers it (281/288). Its total token cost across the
window is 41 k, i.e. ~0.001% of the window's spend. The Bond-style
"re-explores our own memory-sync code" case belongs to a DIFFERENT class, below.

Reproducibility note: the 14-day window ROLLS, so the run counts drift by a few
per day as old sessions age out. The figure that repeats exactly across runs is
the 忘記先查 token cost — `41 396` on every run of this window, including the
report-time one after the defect-7 fix; the run counts (`work_runs` 288–304) and
the candidate-B rate (`hit_rate` 0.54–0.56) move with the window, and the
`fired` count also moved once for a non-drift reason (the marker-shape fix, see
`fired` 124 → 119 below). Quote the token cost, not the run counts.

**Final re-run (2026-09-11, report time, `--sessions 400 --window-days 14 --stuck 3`,
the run whose JSON is persisted):** `sessions_scanned=400`, `work_runs=288`,
`started-with-search=281`, `no-checkpoint-no-search=4`, `never_consulted_with_row_waiting=2` /
`never_consulted_token_cost=41396` (identical to the first measurement),
`stored_unconsulted=1` / `wasted_tool_calls_before_search=2` /
`wasted_completion_tokens_before_search=755`, `candidate_b_threshold_rounds=3`,
`candidate_b_fired=119`, `candidate_b_fired_with_unconsulted_row=67`,
`candidate_b_hit_rate=0.563`, `candidate_b_residual_runs=66`,
`candidate_b_residual_hit_rate=0.555`,
`candidate_b_post_stuck_completion_tokens=12 579 864`
(post-fix run: `checkpoint_fired=290`, `consult_checkpoint_events=37` over
`consult_checkpoint_runs=37` — events and runs now agree, where the substring
count had reported 42 events over 37 runs),
`candidate_b_fired_never_searched=1` (that single fire is a cost-probe session
whose "area" is `cost-probe.txt` and whose covering rows are the cost-probe rows
`#1873/#1874/#1875/#1877` — topic overlap again, so it does not rescue the
round-count trigger; the zero-hit replacement is measured separately below).
The token figure repeats the first measurement to within the window's drift —
`post_stuck` 12 978 978 → 12 579 864 (3% below; the population is nearly five
hundred runs deep, so a 3% wobble is the window's own noise, not a finding).
The `fired`/`hit_rate` movements are NOT drift but the defect-7 fix (124 → 119,
0.548 → 0.563) — the pre-fix run had counted the reader's own words as
checkpoints, which is exactly why the two runs are reported side by side rather
than averaged.

Reproduce: `python3 scripts/memconsult-audit.py --sessions 400 --window-days 14 --stuck 3 --out $SCRATCH/audit.json`
(the report-time JSON is persisted at `/dev/shm/memconsult-audit/final.json`)
Reproduce D1's own table: `python3 scripts/memconsult-checkpoint-scan.py --sessions 400 --window-days 14`

### D4 — GO/NO-GO on candidate B (mid-task stuck / déjà-vu retrieval hook)

The instrument for this decision is the deja-vu signature: `--stuck` consecutive
exploration rounds in the SAME area with no memory search in between — the exact
moment a mid-task nudge could fire. The population that matters is the
**residual**: the run had ALREADY used the start-of-run leg (it searched before
the streak) and still re-derived an area a stored, unconsulted row covered.

Post-fix measurement (2026-09-11, the same 400-log / 14-day window, all three
thresholds re-run through the committed script — `--stuck 2/3/5`, the JSONs kept
at `/dev/shm/memconsult-audit/stuck{2,3,5}.json`):

| threshold | fired | covering row existed (unconsulted) | residual runs | post-streak completion tokens (paid after the nudge point) |
| --- | --- | --- | --- | --- |
| 2 rounds | 186 (65%) | 93 (50%) | 89 | 19.9 M |
| **3 rounds** | **119 (41%)** | **67 (56%)** | **66** | **12.6 M** |
| 5 rounds | 26 (9%) | 18 (69%) | 18 | 4.1 M |

(The first table this section carried — `153/136/21` fired, `77/74/15` covering,
`50/73/3` residual, `17.4/13.0/3.7` M — was measured before the marker-shape fix
of defect 7 and is superseded; the *shape* of the conclusion is unchanged, which
is the point: the trigger fires on 41–65% of work runs at every threshold that
has any population, and its covering-row rate never exceeds 69%.)

**Verdict: NO-GO on candidate B — both of its forms.** The specified
round-count trigger is rejected below on two independent, data-driven reasons;
the narrower zero-hit replacement hypothesis is then measured directly (also
below) and rejected too. The epic's hard constraint 4 ("<50% useful ⇒ 不做") is
satisfied by reporting it, not by shipping a noise generator.

1. **The trigger does not discriminate.** `stuck@3` fires on **119 of 288** work
   runs (41%); at `stuck@2` it is 186 of 288 (65%). A hook that fires on two
   runs in five — or two in three — is not a "stuck" detector, it is a
   per-round tax. The strict variant (`stuck@5`) drops to 26 runs (9%) and its
   covering-row rate rises to 69%, but that is exactly the trade the epic
   forbids making blindly: the population that remains is 26 runs, and the
   hand-audit below is what decides whether their rows are useful.
2. **The "covering row" evidence is not evidence of waste.** Hand-auditing the
   top residual runs shows the matched area is the run's WORKING TEXT, not its
   question. Example (session `c28bde25`, 899 rounds / 528 k tokens after the
   streak, rows `#1765/#1786/#1153` matched on `%v→%w` / gate-ceiling terms):
   the run's 11 memory searches were on *that* subject and returned nothing
   usable, because the run was doing the conversion while the rows describe it
   abstractly. The row's overlap is real, its utility for the specific round is
   not established. Same shape for `452f71a0` (standards-gate accounting) and
   `a965c2b0` (hippocampus host wiring): the rows are *about* the area, they do
   not answer the round's question.

By the epic's hard constraint 4 (<50% useful ⇒ report "不做"), the honest
classification of the 50–69% figure is: **not a measured usefulness rate but a
topic-overlap rate**, and topic overlap is not usefulness — the three hand-audited
cases above are all false positives at the level that decides the question. No
injection threshold may be designed from it (the epic forbids exactly that).

**What the data DOES support as the next step** — measured, not guessed. I took
the replacement hypothesis seriously and measured its population **with a
committed detector** (`scripts/memconsult-audit.py`, the `zero_hit_*` summary
keys added 2026-09-11): the run's FIRST memory search returned zero distilled
rows, then the run went N≥3 rounds in the same area.

| quantity | measured (report-time run, `--sessions 400 --window-days 14 --stuck 3`) |
| --- | --- |
| work runs in window | 288 |
| first search returned ZERO hits | 6 (2.1%) |
| …and then hit a ≥3-round same-area streak | 1 |
| …of those, with a covering distilled row waiting | **0** |
| post-streak tokens in that population | **75 162** |
| first search returned rows (the contrast population) | 276 |
| first search issued but its result unrecognised | **0** |

The `first_search_result_unknown=0` row matters: the detector distinguishes
"the result carried no hit line" from "the reader could not parse the result",
and only the former is counted — an unparsed result would otherwise inflate the
zero-hit population with the instrument's own blindness (the defect class this
report has now caught seven times).

**Hand-audit of that single run (the honest form of the 0-row claim) — and it
falsifies the trigger more strongly than the count does.** Session `1a2b67b0`
(154 rounds) searched three times: round 0 (zero hits — the population's entry
condition), **round 1 — five rows, `#1400/#1228/#1021`, squarely about its subject
(write_task_area / code-review)**, and round 131 (five rows about `loop.go` /
trim / carryForward). Its ≥3-round streak begins at round 9. So the run did
exactly what a zero-hit trigger would try to force — a second, informed query one
round later — and it still went on to a 145-round post-streak tail
(75 162 completion tokens). The trigger's premise is therefore not merely
unpopulated in this window: **the one run that satisfies its entry condition is
positive evidence that an informed re-query does not prevent the streak.** That
is a stronger 不做 than "the population is small", and it is the reason the
post-streak token figure above must NOT be read as recoverable spend.

So the zero-hit trigger is ALSO 不做, for two independent reasons: its population
is one run whose covering-row rate is 0% (below even the 56% topic-overlap figure
the round-count trigger produced), and that run's own trajectory shows an informed
re-query arriving one round after the empty search without preventing the streak.
Both candidate-B forms are rejected by measurement.

That result is itself the useful finding: the residual 12.6 M post-streak tokens
are NOT a missing-trigger problem. 114 of 119 fires had `searches_before_stuck >
0` — the runs searched, got hits, and the hits were about the wrong thing (e.g.
`#1765 %v→%w` returned in a style sweep, `#1611` returned while wiring the
hippocampus host). A second injected question cannot fix a search that already
returned hits; that is a RETRIEVAL QUALITY problem (query formulation and row
relevance), which is audits #1862/#1864 territory and a different epic. The
follow-up task filed from the original epic
(`01a08e8e-c6f8-7776-9f3b-1aaf2fe4f70a`) carries this instruction: confirm the
zero-hit population figure above first (its step 2 requires the measurement
before any build) and report 不做 with these numbers if it reproduces — the
epic allows exactly that outcome, and the honest next investigation is search
relevance, not another trigger.

### D5 / report

This document. Design, measured data, risks, kill switches, and why this shape
beat the alternatives.

## Not Done

* **Candidate B itself is NOT implemented** — deliberately: see D4. The epic
  allows exactly this outcome ("不做 + reason"); the reason is above, with the
  numbers, and the follow-up is filed as a task.
* **No injection threshold was designed or tuned** — forbidden by the epic until
  data justifies it, and the data does not.
* The audit's "residual" class is a HEURISTIC (token overlap between a memory
  row's subject and the re-explored tool-input area). It is a candidate
  generator, not a verdict: every run in it still needs a human/LLM read before
  it can be called waste. The report treats it that way — the top 3 were read by
  hand and all 3 were false positives, and that reading is what the NO-GO rests on.
* Pre-existing cluster details (28% ever-returned, 1.29 rows/search, 8.8%
  error-signal trigger) were taken from audits #1864/#1862 and NOT re-derived.
* **Seven measurement defects were found and fixed during this work** (all in
  the instruments, none in the mechanisms): (1) the "newest N dirs" bound was
  not recent; (2) the detector knew only one memory wire spelling (170× invented
  忘記先查); (3) `grep -c message_start` double-counted requests; (4) free prose
  was machine-classified by substring; (5) the zero-hit table had no committed
  instrument; (6) `grep -c` counted the run's own marker quotes as injections;
  (7) a marker substring test counted the READER's own report text and the
  harness's work-area echo as injections (42 events over 37 one-shot runs).
  Each is recorded in the risk register — the audit's own output is only as good
  as these fixes, and every mechanism verdict above rests on the post-fix
  numbers.

## Verification

| check | command | result |
| --- | --- | --- |
| Build | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | `BUILD_OK` |
| D1/D2 unit suite | `go test ./internal/agent/ -run 'Consult\|Trajectory' -count=1` | `ok … 0.295s` (16 tests, incl. every kill switch) |
| Machine-checkable gates | `go test ./internal/standards/ -count=1` | `ok … 4.8s` |
| Full suite | `CGO_ENABLED=0 go build … && go test ./...` (final pass, job-31, 272.7s) | `BUILD_OK` + **exit 0**, 22 pkg `ok` (`cmd/agent` 111.9s, `internal/agent` 86.3s, `testsuite` 2.7s), 5 `no test files`; `gofmt -l internal/ cmd/` empty |
| D1 scan (yield / one-shot / cannot-skip) | `python3 scripts/memconsult-checkpoint-scan.py --sessions 400 --window-days 14` | 14 real runs, **injection**×1 in 37/37 marker logs, 14/14 replied at marker+1, quotes 0/0/2 |
| D3 audit | `python3 scripts/memconsult-audit.py --sessions 400 --window-days 14 --stuck 3 --out …` | see tables above (report-time re-run: 2 runs / 41 396 tokens / fired 119 / hit_rate 0.563) |
| Kill switch, live | `memconsult-checkpoint-scan.py` injection counts on the A/B arms | ON `e0c6e747`/`173d01f3`/`c2c34a84`/`99a25f76`/`80ddf0e8`/`43888439` = 1 each; consult-OFF `0f93a3bf`=0, `7b02809c`=0; all-OFF `4111e41d`=0, `00b90205`=0, `183ad5ea`=0, `ef94b904`=0 (raw-log arms) |
| Cost probe | `bash scripts/memconsult-cost-probe.sh` | 3 arms, request-1 prompt `10 591` (both ON) / `10 591` (consult OFF) / `10 392` (all OFF) |

### Rule 11 — real-LLM adoption proof (quoted from the session JSONL)

Six real tasks (two scenarios × three runs — the original, the re-run after the
scenario script's log-path defect was fixed, and a report-time re-run) were run
through the rebuilt `bin/gopi` against the live harness
(`scripts/memconsult-scenario.sh`), plus the two three-arm cost probes and the
checkpoint-off A/B pairs.

* **Report-time re-run (2026-09-11, the freshest evidence)** — Scenario A
  session `c2c34a84-63fa-4aa9-a3a0-7983138bab01`: one `[MEMORY-CONSULT
  CHECKPOINT]` **injection** at session.jsonl line 29 (injection count = 1,
  quote count = 0), the run's trajectory prints `memory consult checkpoint
  injected: true`, and its memory tool_use blocks are
  `"search" · "search" · "feedback #1885" · "store"` with adoption
  `rows_returned=4 rows_consumed=2 (50% consumed) empty_searches=0`. Scenario B
  session `99a25f76-c81a-4f04-b455-4bc2d3175bc3`: marker at line 30, blocks
  `"search" · "feedback #1870" · "store"`, adoption `rows_returned=8
  rows_consumed=1`, `memory consult checkpoint injected: true`.

* **Scenario A re-run** (fresh, after the script fix) — session
  `e0c6e747-e4d6-421d-a775-870f67d2e084`: JSONL line **31** is the
  `[MEMORY-CONSULT CHECKPOINT] This round: which work could a memory search have
  saved? …` **user** event (`grep -c` = 1), the run's own trajectory prints
  `memory consult checkpoint injected: true`, and its memory tool_use blocks are
  `search · feedback · search · feedback #1869 · store` — the checkpoint's answer
  arrived with real adoption behind it (`rows_returned=3 rows_consumed=2`, 67%).
* **Scenario B re-run** — session `173d01f3-4b43-4d90-b697-8d983af329e9`: marker
  at JSONL line **39** (`grep -c` = 1), tool_use blocks
  `search · search · feedback #1870 · store`, adoption `rows_returned=10
  rows_consumed=2` — D2 measuring a real run, not a fixture.
* **Original pair** (same scenarios, earlier binary): Scenario A session
  `893660e9-1d89-4fec-869f-71d1ccacc4fb` (marker line 30, `rows_returned=5
  rows_consumed=1`); Scenario B session `c67df866-a41a-46aa-b147-4c7cc8b8b642`
  (marker line 33, `feedback id=1867`).

Excerpts (verbatim shapes, lengths trimmed):

```
$ grep -c 'MEMORY-CONSULT CHECKPOINT' ~/.gopi/sessions/e0c6e747-…/session.jsonl
1
$ python3 -c "…print the memory tool_use inputs…"   # e0c6e747
  line  5 {"action": "search", …}
  line 11 {"action": "feedback", "rating": "useful", …}
  line 57 {"action": "feedback", "id": 1869, "rating": "useful", "tool_use_id": "call_00_tMXtRqN2KAAyAx62WTfF1367"}
$ grep 'memory adoption' <run>.err            # e0c6e747
memory adoption: rows_returned=3 rows_consumed=2 (67% consumed) empty_searches=0 search_completion_tokens=1126
memory consult checkpoint injected: true
```

Every marker session in the window was re-scanned post hoc
(`grep -rl 'MEMORY-CONSULT CHECKPOINT' ~/.gopi/sessions`): 33 session dirs carry
the marker, of which the unit-test fixtures (model `test-model`, 0 rounds) are
excluded; the remaining real runs all carry exactly the injected user event and
answer in the following assistant turn.

### Kill switches (each demonstrated OFF in a test)

| Kill switch | effect | proof |
| --- | --- | --- |
| `GOPI_NO_ENFORCE_MEMORY_CONSULT` / `--no-enforce-memory-consult` | drops ONLY the review-time checkpoint | `TestConsultCheckpoint_DisabledByOwnSwitch` + the live A/B pair above (`ON` timestamps 1×/2×, `OFF` 0×) |
| `GOPI_NO_ENFORCE_MEMORY` / `--no-enforce-memory` | drops the checkpoint too (parent) | `TestConsultCheckpoint_DisabledByNoEnforceMemory` |
| (never armed) | a hand-built agent injects nothing | `TestConsultCheckpoint_UnarmedInjectsNothing` |
| (one-shot) | it fires exactly once and cannot be re-triggered | `TestConsultCheckpoint_ArmedFiresOnceAtFirstCommit` |
| D2 telemetry | adds no switch — pure offline log reading, zero tokens | by construction (session-exit computation) |
| D3 audit | offline script; `--window-days 0`/`--sessions 0` narrows it to nothing | CLI flags |

### Cost added per task by each mechanism

**Re-measured with `scripts/memconsult-cost-probe.sh` (added 2026-09-11 after a
second measurement defect was found in this very table).** The probe runs the
SAME two-item task (create + read back + commit both items) through the rebuilt
binary in three arms differing ONLY in the switches, and reads the cost from the
session log's own `request` events — `prompt_tokens` / `cache_hit` /
`cache_miss` / `completion_tokens` **per API request**, emitted by the harness
itself.

| arm | switches | session | requests | prompt tokens first / total | completion tokens |
| --- | --- | --- | --- | --- | --- |
| both ON | (default) | `80ddf0e8` | 7 | 10 591 / 80 966 | 5 318 |
| consult OFF | `GOPI_NO_ENFORCE_MEMORY_CONSULT=1` | `0f93a3bf` | 9 | 10 591 / 105 310 | 4 208 |
| all OFF | `GOPI_NO_ENFORCE_MEMORY=1` | `4111e41d` | 6 | 10 392 / 66 111 | 2 311 |
| both ON (re-run) | (default) | `43888439` | 9 | 10 691 / 105 716 | 7 517 |
| consult OFF (re-run) | `GOPI_NO_ENFORCE_MEMORY_CONSULT=1` | `7b02809c` | 8 | 10 691 / 93 310 | 4 501 |
| all OFF (re-run) | `GOPI_NO_ENFORCE_MEMORY=1` | `00b90205` | 12 | 10 392 / 139 295 | 4 027 |

**What the numbers say, per mechanism:**

* **D1 (review-time consult checkpoint) adds ZERO prompt tokens of its own at
  request 1** — its arms' first request is byte-identical to the consult-OFF arm
  (10 591 both). That is not an accident of this run: the checkpoint fires at the
  FIRST COMMIT, so its 586-char message enters the window at the request AFTER
  that commit and is cache-read from then on. The probe reports the payload index
  at which the marker first appears (`first_payload_after_request=3` on
  `80ddf0e8`) instead of assuming request 1 — the previous table's "+199 tokens
  ≈ the injected 563-char message" attributed to D1 was WRONG twice over: 199 is
  the **task-start** message's cost (visible in the all-OFF arm: 10 392 vs
  10 591), and the injected length is **586 chars, not 563**.
* **D1's honest footprint is therefore ~0 net prompt tokens** (one 586-char
  message, cache-read thereafter) and its cost is the ANSWER it elicits — one
  line, capped by the message's own "do NOT start new work" wording.
* **The task-start leg (candidate A, pre-existing on master)** is the one that
  costs prompt tokens at request 1: `10 591 − 10 392` = 199 in the first probe,
  `10 691 − 10 392` = 299 in the re-run. The two probes bracket the figure
  because the injected task-start block carries the **hot-tier memory digest**,
  whose length varies with what is in the hippocampus — the stable claim is
  "D1 costs 0 at request 1, the task-start leg costs the digest", not the
  constant 199. (Both probe runs agree on the part that decides the question:
  ON and consult-OFF have byte-identical request-1 prompts.)
* **The request-count column in the old table was 2× the truth.** The raw SSE log
  prints the token `message_start` twice per request (`event: message_start` and
  `"type":"message_start"` inside the data JSON), so `grep -c message_start` —
  the method that produced `16/18/22` — double-counts. The `request` event read
  back from the session log gives 7/9/6 here. This is why the probe parses the
  session log and not the raw log.
* **Run-length variance dominates any cost claim from a single pair.** The three
  arms differ in request count (6–9) and total prompt tokens (66 k–105 k) while
  doing identical work — a trivial task's request count is LLM-behaviour noise.
  Quote the request-1 figure and the marker's payload index (both deterministic);
  never quote a task-total delta as a mechanism's cost.

Kill switch re-proven live on all six arms, counted as **injections**
(`memconsult-checkpoint-scan.py`'s rule, not `grep -c`): `80ddf0e8`=1,
`43888439`=1 (both ON), `0f93a3bf`=0, `7b02809c`=0 (consult OFF), `4111e41d`=0,
`00b90205`=0 (all OFF). `grep -c` on `43888439` returns 3 — one injection plus
two quotes of the marker by the run itself — which is exactly the defect the
scan's injection-only rule removes. The task-start marker is present in the ON
and consult-OFF arms and absent in the all-OFF arm, i.e. the two switches are
independent, as designed.

Persisted probe output: `/dev/shm/memconsult-audit/cost-probe.log` (reproduce:
`bash scripts/memconsult-cost-probe.sh`).

### The Bond-class case: found, and it is the residual — with its honest size

The epic's motivating example (a run re-exploring our own memory-sync code) is
reproducible in the window. Session `a965c2b0` spent **459 rounds / 305 536
completion tokens after the déjà-vu point**, re-exploring an area covered by
`#1611` (hippocampus 备忘每轮重复投递 — root cause + the two gates),
`#1618`, `#1622`, `#1623` — all "hippocampus / memory-sync" rows, all with
`retrieval_count > 0` (i.e. retrieved before, then forgotten). That is the class
verbatim. Sibling cases: `c8e4d67f` (claim-broadcast rows `#1173/#1174/#1194/#1196`),
`bc66ae14` (memory-asset sync rows `#1278/#1309/#1311`), `95c3db5b`(peer-sync rows
`#1686/#1695/#1696/#1700`).

Their shared property is the finding that kills the round-count trigger: in each
one the run HAD searched (114 of the 119 `stuck@3` fires have `searches_before_stuck > 0`) —
it searched on a *nearby* subject and got results that were about the wrong
thing, then kept working the area. So the missed hook is neither "the run forgot
to search" nor "the search came back empty" — the second reading was tested and
its whole population is 6 runs, of which 1 reached a streak and **0** had a
covering row waiting (D4 above). The Bond run's searches returned rows; those
rows were about its working text rather than its question. An injected re-query
cannot repair that, which is why both candidate-B forms are NO-GO and the real
residual is search RELEVANCE (query formulation + row scoring), a different epic
that audits #1862/#1864 already opened.

### Why the 1.1 M-token headline is deflated by hand-audit

The candidate-B table's largest residual run (`a9873ebc`, 3175 rounds / 1.15 M
completion tokens after the streak) matched rows `#1028/#1029/#1360/#1361` —
HOMM3/VCMI map toolchain notes — while the run was doing `cd … && python3 -c
"from PIL import Image…"`, i.e. image work on the same project. Project-level
topic overlap, not "we re-derived a stored answer". Quoting this number as
"savings" would be exactly the noise-generator failure mode constraint 4 forbids.

## Risk register

* **The D1 checkpoint is an LLM-reported measurement.** It is harness-enforced
  in that it cannot be skipped, but the ANSWER is prose and may be "none" by
  default. It is priced as such (one line, no work) — it is a case-list
  generator, never a number to quote as savings. **Do not machine-classify its
  replies by substring** (defect 4 below).
* **Free prose is not a metric — the fourth measurement defect.** A first version
  of `memconsult-checkpoint-scan.py` classified replies `EXPLICIT-NONE` vs
  `EXPLICIT-WORK` by substring and reported `4 NONE / 7 no-answer`; reading the
  four showed their `none` referred to the TASK LIST ("none are progressing").
  The classifier is gone; the script now measures only what is deterministic
  (marker count, marker→reply adjacency, fixture separation) and the yield is
  reported as the hand reading it is. Same family as defects 1–3 (the measuring
  instrument, not the mechanism) and the reason the D1 section quotes replies
  instead of a rate.
* **A session dir can lose its `session.jsonl` while its `.raw.log` survives**
  (GC/rotation) — the four checkpoint-off A/B arms are raw-only now. A scan that
  only globs `session.jsonl` silently drops them, so the kill-switch evidence is
  stated against the raw logs (`96f22803`=1, `cc5c8a1d`=2 consult markers;
  `183ad5ea`/`ef94b904`=0) and the script says `scanned_logs=274` for a
  sessions root that holds far more.
* **The adoption detector is a lower bound.** Feedback/update-by-id is the only
  observable consumption. A run that used a hit silently (no vote) is counted
  unconsumed. Reading `rows_consumed=0` as "the search was useless" is wrong —
  and this is precisely why the D4 verdict above was made from hand-audited
  cases and not from the ratio.
* **`rows_returned` can double-count a row** returned by two searches in one run
  (observed in scenario B: one hit id, two feedback votes) — the reason the rate
  is reported alongside, never instead of, the raw counts.
* **Wire-spelling drift** is the standing hazard for any log reader (defect 2
  above). New `is_memory_*` predicates must be extended for every future tool
  merge; the audit's constants are the single place.
* **A substring test counts the reader's own words — defect 7, and it misled a
  published number.** The audit matched the consult marker with `in`, so the
  harness's WRITE_TASK_AREA echo and this report's own prose counted as
  checkpoints: `consult_checkpoint_events=42` over `consult_checkpoint_runs=37`
  in a mechanism that is one-shot by construction. Both instruments now match
  the injection SHAPE (the marker must BEGIN the user message); events collapse
  to 37, exactly one per run. The tell was the arithmetic — more events than
  possible — and it is the same family as the other six: the two markers are
  also quoted inside `tool_result` events by the runs that report on them.
* **`grep -c` cannot count injections — the sixth measurement defect.** The
  checkpoint marker is quoted back by the run when it answers, so a substring
  count over a session log returns injection + quotes: arm `43888439` measures 3
  by `grep -c` and exactly 1 by the scan's injection rule. Every "the marker
  fired N times" claim in this report is an injection count; the scan prints
  `marker_QUOTES` beside it so the two can never be silently confused again.
* **The report's zero-hit table had no committed instrument until now.** The
  7-runs/302 figure and the 75 162-token figure were measured in an ad-hoc probe
  and quoted in the report; the detector only landed in
  `scripts/memconsult-audit.py` (`zero_hit_*`) during the final verification
  pass, and it reproduces the token figure exactly (75 162) with 6 runs / 289
  work runs in the report-time window. The lesson generalises: a number in a
  report whose producing script is not committed is not reproducible, and this
  one very nearly slipped through as a bare claim.
* **The raw SSE log double-prints every event token.** `grep -c message_start`
  returns 2× the request count (the event name line plus the `type` field in the
  data JSON). Any cost/request count must come from the session log's `request`
  events — the reason `scripts/memconsult-cost-probe.sh` exists and why the
  epic's first cost table was wrong. Same family as the wire-spelling defect:
  the measuring instrument, not the mechanism, produced the wrong number.

## Why this shape beat the alternatives

* **Why a review-time checkpoint (D1) instead of a prompt rule?** 原则无效，检查才有效.
  A prompt rule is skippable by construction; a harness context event is not.
  The epic's own history proves it: candidate A only moved the first-action
  number once it was injected by the harness rather than asked for in the prompt.
* **Why the first commit, not the run's end?** The end is too late to name what
  was wasted (the run is summarizing, not reporting) and too early is guessing.
  First commit is where the run has evidence and still has context.
* **Why measure adoption in trajectory.go (D2) instead of a new counter in the
  memory tool?** The tool would only see its own calls; the question is whether
  the run USED what came back, which lives in the run's whole trajectory.
  Rule 3 also forbids a new tool, and the stats are computed at session exit
  from data already on disk — zero-cost, zero-token.
* **Why a bounded log audit (D3) instead of reasoning about the design?** The
  first two numbers this audit produced were both artifacts of the reader, one
  of them 170× off. Data that has not survived a re-measurement is not data.
* **Why NO-GO (D4)?** Because the only trigger the epic proposed fires on 41%
  of runs (65% at the loosest threshold) and the evidence behind its 50–69%
  covering-row rate is topic overlap — hand-audited false positives. Shipping
  it would have produced a noise generator, the one outcome constraint 4 names
  explicitly. The measured replacement (zero-hit) fails twice over: its
  population is one run in 288, and that run's own trajectory contains the
  experiment the trigger would run — an informed second query one round after
  the empty one — followed by the same 145-round streak. A trigger whose one
  natural experiment already happened and did not work is 不做 with evidence,
  not 不做 by absence.
* **Why re-measure the whole report at the end?** Because the final pass found
  seven instrument defects, and four of the published numbers moved when they
  were fixed. A report whose numbers were produced by an instrument that has
  since changed is a history of the instrument, not a measurement of the
  mechanism. The pre-fix figures are kept where they differ, with the fix named
  beside them, so a reader can see exactly which claim each number supports.

## Deliverable declaration (Rule 6d)

* Branch: `feat/memory-consult-enforcement` (worktree `~/workspace/gopi-memconsult`)
* Recovery: `git show feat/memory-consult-enforcement:devdoc/memory-consult.md`
* The commits live in the shared object store — no push needed, the branch is
  visible from the main repo.
