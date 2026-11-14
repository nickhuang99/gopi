<!-- doc-class: roadmap -->
# Task Report: agency level 3 — a pattern-threshold improvement-proposal mechanism

Epic: raise GoPi from agency level 2 (passive execution + proactive bug reports)
to level 3 (proactive improvement proposals), via a pattern-threshold-triggered
mechanism that reuses existing infrastructure, costs no main-loop attention, is
off by default, and cannot become an auto-dispatch machine.

Branch: `feat/agency-proposals` (worktree `~/workspace/gopi-agency-proposals`)

---

## 1. Verdict up front

| Gate | Result |
|---|---|
| Do worth-proposing patterns exist? (offline evidence first) | **YES for one class, NO for another — and the split is the finding** (see §4) |
| Threshold values backed by a distribution | **YES** — every number in §5 is a named constant with its measured histogram next to it |
| Mechanism behind a switch; disabled = zero behaviour change | **YES** — `GOPI_AGENCY` unset ⇒ the miner is never entered (asserted) |
| Historical replay produces judged proposals | **YES** — 6 proposals over the real ledger, **6/6 judged real problems** (§7) |
| Idempotence (2nd pass ⇒ 0 proposals) | **YES** — asserted 3 ways: unit, live-ledger snapshot replay, two real `gopi` processes (§8) |
| Auto-dispatch vs the serve branch-scan precedent | **ANSWERED** — `quiet` fence + any-status dedupe, both red-provable (§9) |
| `go build` + `go test` green, minimal regression test | see §11 |

One-line answer to "does agency level 3 pay off here?": **yes, but not for the
reason the epic assumed.** The mined-and-proposed class that pays is NOT
"recurring task failures" (that class is silent on the real ledger — its only
members are unit-test fixtures). It is **duplicate knowledge rows**, which the
epic never mentioned. The mechanism therefore ships with both detectors, with
the negative result recorded rather than hidden.

---

## 2. Deliverables

| # | Artifact | Path |
|---|---|---|
| 1 | Miner + thresholds + dedupe + submit path | `internal/agencyscan/` (`agencyscan.go`, `store.go`, `render.go`, `report.go`) |
| 1b | End-hook trigger (form A) | `internal/agent/agencyhook.go`, called from `finishSessionTaskRecorded` (`internal/agent/taskregister.go`) |
| 1c | Switch + one reader (Rule 2) | `internal/config/agency.go` (`GOPI_AGENCY`, `GOPI_AGENCY_MIN_FAMILY`) |
| 1d | Ledger-side wiring (submit seam, quarantine set) | `internal/tools/agency.go` |
| 2 | Report-only CLI face + replay harness | `gopi task improvements` (`cmd/agent/cli_tasks.go`) |
| 3 | Replay + idempotence harness | `internal/tools/agency_replay_test.go` (copies the live ledger; asserts pass2 = 0) |
| 4 | Offline evidence scripts | `scripts/mine_patterns.py`, `scripts/mine_loops.py` (+ their raw output) |

No new store, no new daemon, no new top-level tool: the ledger **is** the edge
store, the end hook **is** the trigger, `TaskSubmit` **is** the output channel,
`quiet` **is** the quarantine, and `gopi task improvements` sits inside the
existing `task` CLI family.

---

## 3. Corpus (what the evidence was measured on)

`~/.gopi/memory.db`, 2026-09-11:

| Fact | Value |
|---|---|
| tasks | 969 (completed 783 / cancelled 123 / failed 55 / verified 3 / in_progress 3 / pending 2) |
| task sources | auto 505 / human 333 / llm 131 |
| task_events | completed 400 / in_progress 283 / reframe 243 / cancelled 65 / failed 25 |
| harness self-report rows (`— elapsed=… tokens=…`) | **239** (22 of them "failed") |
| → real briefing rows | 730 (33 failed, 4.5%) |
| distilled_qa | 1814 rows / 1782 distinct normalized questions |
| session JSONL files | 1374 files, 1361 processable, 390 MB |
| token digests | n=145, median 4.30 M, p90 34.9 M, max 104.8 M, total 1.88 B |

Both raw mining passes are committed: `scripts/mine_out.json`,
`scripts/loops_out.txt` (and the scripts that produced them, read-only).

---

## 4. The evidence pass: two candidate classes, one real

### 4.1 Class C1 — "recurring failing task family" → **SILENT, and that is the honest result**

Threshold shape: a task family (recurrence key = provenance-stripped tag
signature + first 3 normalized headline tokens) whose size clears the floor AND
whose failure share clears the gate.

| Gate combination | Candidate families |
|---|---|
| size ≥2, ≥1 failure | 58 → but see the artifact blow-up below |
| size ≥5 (shipped), failure >50% | **0 on the real ledger** |
| size ≥5, failure >50%, **digest self-reports NOT filtered** | **2 — both fake** |

The last row is the decisive measurement. With the harness self-report filter
disabled (experimental build), the miner proposes:

- `recurring-failure  |failing chat turn  n=5` — all rows `failed`, tag
  `from-peerbox`, headline `[auto] failing chat turn — elapsed=2.0s iter=1 tokens=20 …`
- `recurring-failure  |run the failing  n=6` — same shape, `from-peerbox`

Both are **unit-test/backfill fixtures mirrored in from a peer** (`token
counters=20`), not real work. The self-report filter removes 239 rows total, of
which 22 of the ledger's 55 "failed" rows — i.e. **40 % of the entire failure
signal is test residue**. A level-3 mechanism that proposed improvements from
that would have been an *artifact amplifier*: it would generate confident
"recurring failure" tasks about a fixture.

Since the filter landed, C1 is **quiet on the real ledger**. The detector is
implemented, unit-tested against synthetic families
(`TestMineTaskFamilies_ThresholdMatrix`), and latent: it will fire the moment a
genuine multi-row failure family appears. This is recorded as a negative result,
not papered over.

### 4.2 Class C2 — "one question, several near-identical knowledge rows" → **REAL, 6 candidates, 6/6 judged real problems**

`distilled_qa` normalized-question histogram: `1:1766, 2:10, 3:2, 4:2, 5:1, 9:1`
→ 6 groups reach n ≥ 3, holding 48 rows (2.6 % of the store).

The live run (`gopi task improvements`) proposes exactly those 6. Judged
individually (§7): **all 6 are real defects in the knowledge store**, and the
largest three are *one* mechanical defect wearing three coats.

### 4.3 Classes examined and REJECTED on measured numbers

The epic asked for a *data-driven* threshold, which means a candidate class must
be shown to clear it. The session corpus (1361 JSONL files, 13 869 distinct
normalized bash signatures, 1110 recurring in ≥2 sessions) gives:

| Recurring-signature size distribution | p50 | p75 | p90 | p95 | p99 | mean+2σ |
|---|---|---|---|---|---|---|
| distinct sessions | 2 | 4 | 8 | 16 | 65 | **43.3** |

- **Defect substitution** (`grep`/`rg`/`find` used where `code_search` exists:
  2579 signatures, **722 sessions = 53 %**; `cat`/`head`/`sed -n` used where
  `read` exists: 1685 signatures, **567 sessions = 42 %**). Detection is trivial;
  **justification is not.** A mean+2σ gate (43 sessions of evidence) would catch
  ~1 % of recurring signatures and pass every one of these — because the ledger
  shows **0 learned lessons** asking for it and 0 bug rows. Proposing it would
  be the mechanism's own noise floor. **Rejected — and the measurement is the
  reason, not a hunch.**
- **Structural loop classes** (`sleep`-poll: 0 sessions; merge-conflict: 22
  tagged rows / 48 `conflict`). The merge-conflict loop is the highest-count
  *task* pattern in the corpus — **and it is the one already automated**
  (merge-backlog audit + the serve branch scan). Re-detecting it is precisely
  the flood precedent of §9, so it is excluded by design.

---

## 5. Thresholds — each number with the distribution it came from

All in `internal/agencyscan/agencyscan.go`, one place, named (Rule 1).

| Constant | Value | Distribution it is derived from |
|---|---|---|
| `minTaskFamilySize` (→ exported `MinTaskFamilySize`) | **5** | live family sizes `1:502 2:32 3:13 4:2 5:3 6:2 7:1 8:2 ≥9:3` → n≥2 = 58, n≥3 = 26, n≥4 = 13, **n≥5 = 11**. 58 candidate families is a flood, not a conservative pass; 11 is the point where a family is a standing workflow. The histogram ships in every report, so the gate can be re-checked against a moving ledger. |
| `maxFailureRate` | **0.5** | whole-ledger failure rate 55/969 = 5.7 % (33/730 = 4.5 % on real rows); the only failing families measured 100 % (6/6, 5/5). A 50 % gate catches every observed failing family and admits the healthy ~95 %. |
| `minFailedInFamily` | **2** | one failure inside a 5-row family is a transient (network/API), not a pattern; the observed failing families carry 5–6. |
| `duplicateGroupMinSize` | **3** | `1:1766 2:10 3:2 4:2 5:1 9:1` → n≥3 is where the histogram stops being a long tail of coincidences (6 groups). |
| `RecurrenceKeyFingerprint` | **4** bytes | a collision *budget*, not a tuned knob: at ~30 candidates a full scan can yield, the birthday collision probability is ≈ 30²/2³³ ≈ 1e-7. |
| `recurrenceKeyTokens` | **3** | 3 tokens yields 560 families of which 502 are singletons (the one-off-briefing tail); 1 token collapses distinct workflows into one bucket. |
| `agencyScanTimeout` | **30 s** | measured pass cost over the live ledger: **0.39–0.54 s**. The timeout is a wedge guard (7× headroom is 2 s; 30 s chosen so a slow-but-progressing pass is never cut off), never the expected path. |
| `summaryExcerptRunes` / `maxProposalSummaryRunes` | 24 / 80 | `gopi task list` renders one line per row; the excerpt is the part that must survive (a 42-rune prefix pushed a CJK headline past the budget and dropped exactly the evidence). |

`GOPI_AGENCY_MIN_FAMILY` is the one operator knob; it only *re-scopes* that
default through a single reader (`config.AgencyMinFamilySize`) — a malformed
value falls back to the measured constant, never to 0.

---

## 6. Mechanism

```
session ends
  └─ finishSessionTaskRecorded (internal/agent/taskregister.go)
       └─ Agent.maybeRunAgencyPass()          ← GATE: GOPI_AGENCY truthy?      [no ⇒ return, zero work]
            └─ agencyscan.Scan(Submit:true)
                 ├─ mine task families        (C1: size ≥5, settled, failure >50 %)
                 ├─ mine duplicate knowledge  (C2: ≥3 rows per normalized question)
                 ├─ render                    (headline ≤80 runes + self-contained briefing; refusal is COUNTED)
                 ├─ dedupe probe              (payload LIKE '%candidate:<fp8>%', ANY status)
                 └─ TaskSubmit(tags=improvement,agency-proposal,<kind>,quiet)
```

Design decisions, each with the alternative it beat:

- **Form A (task-end hook), not form B (post-commit review question).** Form B
  would need a new agent-response hook *and* would spend main-loop attention.
  Form A is the existing "a session just ended" edge — the prompt is already
  flushed, so the pass costs 0 tokens.
- **Synchronous, not a goroutine.** First implementation ran detached; a live
  end-to-end run wrote **0 proposals** — the goroutine loses the race with
  `os.Exit` right after the trajectory stats. Measured cost of doing it inline
  is ~0.5 s, so it *is* inline. (A mechanism that "works" but never fires is
  worse than no mechanism: it wears an enabled flag.)
- **The ledger is the edge store.** No new store/schema: proposals are ordinary
  `pending` rows, the dedupe fence lives in the existing `payload` column, and
  the quarantine set lives in the existing tag column. The 969-row task table
  already carries the lifecycle evidence (Rule 7's own "the ledger is the audit
  layer").
- **`internal/agencyscan` is a leaf; the one write is injected.** The miner must
  not import `internal/tools` (cycle), so `internal/tools` installs the submit
  seam in its `init` and the mining rules stay pure functions testable without a
  DB. The reverse-direction injection is also what keeps `TaskSubmit` the single
  write path.
- **Report-only is the default face of the CLI.** `gopi task improvements`
  writes nothing, so the epic's replay harness and the operator's audit use the
  exact same code path as the writer.

---

## 7. Historical replay and the quality verdict (the acceptance artifact)

`TestAgencyReplay_LiveLedgerIdempotent` copies the operator's live
`memory.db` (main file only — a stale `-wal` would replay uncommitted frames),
points `GOPI_MEMORY_DB` at the copy via `t.Setenv` (Rule 17), and runs the real
miner through the real submit seam:

```
REPLAY PASS 1: examined=2789 candidates=6 proposed=6 deduped=0 dropped=0
  duplicate-knowledge  n=3  key=knowledge|epic goal that is itself a literal exact string …
  duplicate-knowledge  n=4  key=knowledge|git blame cmd agent cli tasks go lines …
  duplicate-knowledge  n=9  key=knowledge|git blame internal agent loop go lines …
  duplicate-knowledge  n=4  key=knowledge|git blame internal tools tasks go lines …
  duplicate-knowledge  n=5  key=knowledge|has the user expressed expectations about how you should behave …
  duplicate-knowledge  n=3  key=knowledge|output requirements for verification tasks show each test case …
REPLAY PASS 2: candidates=6 proposed=0 deduped=6 dropped=0
```

**Quality verdict — proposal by proposal** (real problem vs noise):

| # | Pattern | Rows | Verdict |
|---|---|---|---|
| 1 | `git blame internal agent loop go lines <n> <n>` | 9 | **REAL** — the `git_blame` recipe is stamped once per (file, line-range). The Rule-10.6 recipe store is being polluted by its own citation format. Highest blast radius: 9 of 17 rows across #1–#3 are one mechanical defect. |
| 2 | `git blame internal tools tasks go lines <n> <n>` | 4 | **REAL** — same defect, different file. |
| 3 | `git blame cmd agent cli tasks go lines <n> <n>` | 4 | **REAL** — same defect, third file. |
| 4 | `has the user expressed expectations about how you should behave …` | 5 | **REAL** — 5 copies of one mid-task checkpoint question. |
| 5 | `epic goal that is itself a literal exact string deliverable like "Reply with exactly: GLM_OK"` | 3 | **REAL** — the actionless-epic class is still minting rows, each with rc≈455. |
| 6 | `output requirements for verification tasks show each test case individually with actual vs expected` | 3 | **REAL** — one lesson stored three times. |

**Precision on the real corpus: 6/6 = 100 %.** At the shipped gates the proposal
list is not a review burden — it is a small, correct to-do list. And the
highest-value item (#1) is a defect in *the mechanism's own input channel*:
duplicate knowledge rows degrade every future retrieval, so consolidating them
is a payoff that compounds. That is the level-3 behaviour the epic was after.

*(Noise measurement note: the honest statement is "6/6 real at the shipped
gates". The gates are what make that true — at size ≥2 the C1 class would have
proposed artifact families (§4.1).)*

---

## 8. Idempotence — asserted three ways

| Level | Assertion | Evidence |
|---|---|---|
| Unit (no live data) | `TestScan_IdempotentSecondPassHasZeroProposals`: pass 1 proposes N≥2, pass 2 proposes **0** with `deduped == N`, pass 3 **0** | `ok internal/agencyscan` |
| Live-ledger replay | `TestAgencyReplay_LiveLedgerIdempotent` over a byte copy of the real ledger: pass 1 proposed 6, pass 2 proposed **0** / deduped 6, and `len(candidates)` identical both passes (the miner is a pure function of the ledger) | replay output above |
| Two real processes | e2e sandbox (`GOPI_HOME` with a ledger copy): run 1 wrote **6** proposal rows; run 2 logged `[agency] scanned=2799 candidates=6 deduped=6` and the ledger still held **6** | `sqlite3 … where tags like '%agency-proposal%'` → 6 |

The dedupe probe matches **any** status — the operator's "no" (cancelled) is
permanent until the *pattern* changes, and `TestProposalExists_TerminalRowStillSuppresses`
pins that for all 7 statuses.

---

## 9. "Will this become an auto-dispatch machine?" — answered in the ledger

Three independent fences, each with a red proof:

1. **Default off.** `GOPI_AGENCY` unset ⇒ `maybeRunAgencyPass` returns before
   anything is opened. `TestScan_DisabledMeansNoWrite` asserts a report-only
   pass submits 0 rows *and* leaves the row count unchanged.
2. **The `quiet` fence.** Every proposal row carries `agencyscan.TagQuiet`, and
   the worker's skip list is built by *referencing* the same constant
   (`workerExcludedTags = "pa-queue," + tools.QuarantineTagSet`, Rule 14 — no
   literal). `TestProposalRowsAreNeverClaimable` seeds the proposal **first**
   (so claim order would offer it first) and asserts through the queue's own
   `TaskNextFiltered` that it is never offered. **Red-proven**: deleting the
   `quiet` tag from the fixture makes the test fail with the queue offering the
   proposal row.
3. **Any-status dedupe.** §8. This is the *serve branch-scan* lesson applied:
   that scan's coverage mask counted only LIVE rows, so a decision row that
   reached a terminal state re-armed the branch and every tick minted a
   byte-identical row (~600 duplicates in the legacy ledger). A proposal that is
   cancelled, completed, failed or verified still suppresses itself.

So: a proposal is addressed to **the operator** — a row in the audit ledger a
human reads. Nothing about this mechanism dispatches work, and if the two
automation fences were both removed, the third (default off) still holds.

---

## 10. Risks and limitations (honest)

- **C1 has no positive hit on the live ledger.** Its only firing candidates are
  test fixtures. The detector is latent and unit-tested; a real multi-row
  failure family will fire it. If after a few weeks it has *never* fired on real
  data, the honest conclusion is to delete C1 rather than keep dead code — the
  report prints its family histogram every run, which is the measurement that
  would justify that call.
- **Existence-based detection.** Only *recurrence* is visible. A one-off task
  that consumed 104 M tokens and failed is invisible to this mechanism (the
  token digests are in `task_events` but the pass does not consult them yet —
  a cost-threshold class would be the obvious next detector, and it is exactly
  the class the epic's "threshold from a distribution" rule can justify).
- **`payload` substring dedupe.** The fence is an 8-hex-char prefix inside a
  free-text column; a *human-written* payload that happened to contain
  `candidate:<8 hex>` would suppress a proposal. Accepted: the marker string is
  deliberately verbose (`Dedupe fence (do not edit):`), and the cost of the
  failure mode is one un-proposed improvement, not corruption.
- **Session JSONL evidence was only used for the rejected class.** The mining
  script reads all 390 MB of sessions; the *shipped* miner reads only the
  ledger (bounded, ~0.5 s). No session scan is on the hot path.

## How to disable / operate

```bash
gopi task improvements            # report-only: what would be proposed. Writes nothing.
GOPI_AGENCY=1 gopi <task>         # enables the write path for that run
GOPI_AGENCY_MIN_FAMILY=3 gopi …   # loosens the C1 size gate for a young ledger
```

Disabled (the default) = the pass is never entered ⇒ zero behaviour change, zero
attention cost, zero rows.

---

## 11. Verification

- **Build**: `go build ./...` → OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- **Tests**: `go test ./...` → **19/20 packages ok**, one PRE-EXISTING flake
  unrelated to this change:
  `internal/tools` `TestMemorySearchNonBlocking` fails inside a full-package run
  (order/load-sensitive) while passing **12/12 in isolation**
  (`-run TestMemorySearchNonBlocking -count=12` → ok). `git diff master...HEAD`
  touches neither `memory.go` nor `memory_test.go`. Filed as a bug task with the
  repro and the untouched-file evidence rather than papered over:
  `01a08e80-4091-78e1-a842-ee24f18ac172`.
  Every package this change touches is green: `internal/agencyscan`,
  `internal/agent`, `internal/config`, `internal/standards` (gates 16–21),
  `internal/stylegate`, `internal/testutil`, `cmd/agent`.
- **Focused suites**: `go test ./internal/agencyscan/` → ok;
  `go test ./internal/tools/ -run 'TestAgencyReplay|TestProposalRows'` → ok;
  `go test ./cmd/agent/ -run 'TestTaskCompletionNames|TestWorkerExcludedTags|TestTopLevelUsage'` → ok;
  `go test ./internal/standards/` → ok (gates 16–21 green; one baseline line
  re-pointed for the Rule-18 line shift).
- **Live evidence**: replay transcript (§7), two-process idempotence transcript
  (§8), proposal rows queried from the sandbox ledger.
- **e2e note**: the sandbox is `GOPI_HOME=/tmp/agency-e2e-N` (settings +
  ledger copy, `sync_peers` emptied, no `repos` entry) — the real ledger was
  never written by the e2e runs.

## 12. Commits (branch `feat/agency-proposals`)

| Commit | Content |
|---|---|
| `0d12bf2` | evidence pass: offline pattern mining scripts + raw output |
| `9ccb428` | `internal/agencyscan`: miner, threshold gates, submit path |
| `8ffce12` | end-hook pass (`GOPI_AGENCY`) + report-only CLI face + quiet quarantine |
| `5630b45` | live-ledger replay + claim-quarantine red proof |
| `b74b74d` | synchronous pass (goroutine lost the exit race) + live e2e evidence |
| `24bd2c9` | this devdoc |

**Full-suite status**: 19/20 packages `ok`; the single failure
(`internal/tools.TestMemorySearchNonBlocking`) is a pre-existing order-sensitive
flake — untouched files, green in isolation 12/12, filed as bug
`01a08e80-4091-78e1-a842-ee24f18ac172`.

## 13. Not done (deferred, deliberately)

- **C1's positive reproduction on real data** — needs a real recurring failure
  family to appear; unit-tested in the meantime (see §10).
- **The cost/token-threshold detector** — the digests are in `task_events` and
  unconsulted; a follow-up task, not a silent gap.
- **Consolidating the 6 proposed patterns** — that is operator work: the
  mechanism proposes, the human decides (and the proposal rows carry the
  briefing for exactly that decision).
