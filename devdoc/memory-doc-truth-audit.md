<!-- doc-class: historical -->
# Memory / Hippocampus / Ledger doc-truth audit (C3)

Audited: 8 docs named by the briefing + the sibling `devdoc/requirements/05-context-and-memory.md`.
Method: every claim below was checked against **code at HEAD `52090dc0`** (branch
`feat/mem-doc-truth-audit`, worktree `~/workspace/gopi-mem-doc-audit`, base = master `52090dc0`) or
against **live ledger readings taken 2026-09-19** — never against another doc's prose. Where a doc's
own line citations are checked, the base commit the doc names is used (it says so itself); the
verdict row records which.

Live ledger readings used throughout (`sqlite3 ~/.gopi/memory.db`, read-only, 2026-09-19):

| reading | value |
|---|---|
| `distilled_qa` rows | **4,044** |
| rows with non-empty `exam_questions` | **1,072** (26.5 %); **2,972 blank** |
| per-column `kw_*` fills | what 1,072 / why 963 / where 711 / who 698 / **when 230**; all five = **133** |
| rows distilled after 2026-08-30 with `exam_questions` | **0** |
| tier | cold 122 / warm 3,921 / hot 1 |
| `sources` | brew 6, `node:` 10 |
| `session_conclusions` | 5,949 |

---

## 1. Verdict rows

| # | doc | kind | verdict | code evidence (HEAD unless stated) |
|---|---|---|---|---|
| 1 | `devdoc/memroadmap-m1.md` | plan + acceptance | **DONE, one specified item NOT done** | Cols + FTS present: `internal/tools/memory.go:133-138` (`exam_questions`,`kw_what..kw_who`), `:211-219` (`m1FTSCols` = the 12-column set, `m1TriCols`). Routes: `:1856` `likeCJKTargets`, `:2278-2279` coverage haystack. Backfill shipped: `scripts/backfill_exam_5w1h.py`, live rows dated 2026-08-24..08-29. §3.4 **store side NOT implemented** — see F1. Tests: `memory_m1_test.go:51,75,141,165,190` (5). |
| 2 | `devdoc/memroadmap-m2.md` | plan + acceptance | **DONE — every specified symbol exists** | Consts `memory.go:281,287,294,301,313-314,320,329-330,332`; `m2EffectiveConfidence` `:3095` (+reward `:3112`); wiring `:1981` / `:1975`; feedback writeback `:3132` (`MemoryFeedback`) with steps at `:3215-3217`, cooldown `:3242`; `hotRecordStale` `:2090`; `MemoryHotResident` `:3514`; agent injection `internal/agent/loop.go:3629,3631` + logged at `:3638-3640`; tests `memory_m2_test.go` 8/8 named `TestM2*`. |
| 3 | `devdoc/memroadmap-m3.md` | plan + acceptance | **DONE; two "future agent" claims now FALSE** | `memory_brew.go:42,69,73,78,103,123`; `memory_brew_run.go:262` (`brewGuardBlocked`); `memory_brew_llm.go:164` (effort-mode max_tokens); `cmd/agent/cli_brew.go:22-35` (usage lists all five flags), `:119` `maybeBrewOnSessionExit`, wired at `cmd/agent/main.go:1088`; `memory_brew_test.go` + `cli_brew_test.go`. Stale claims = F5. |
| 4 | `devdoc/memroadmap-m4.md` | plan + acceptance | **DONE; Not-Done list still accurate** | `memory_sync.go:163` `SyncExport`, `:285` `SyncImport`, `:381` `SetMemoryDBPath`, `:38` `SyncNodeTagPrefix`, `:252-280` refused-vs-rolled-back semantics; `cmd/agent/cli_memsync.go`; dedup-key fix `memory.go:2506` (`substr(question,1,?)`); tests `memory_sync_test.go:25,84,122,149,182,204,324,349,366`. Still absent, as the doc says: import `--dry-run` (0 hits in `cli_memsync.go`/`memory_sync.go`), automatic transfer, reverse channel. |
| 5 | `devdoc/sync-tag-case-policy.md` | task report | **ACCURATE** (line numbers drift by 1 in one file) | `memory_sync_delta.go:109` `syncTagTokenPattern`, `:153` `syncTagTokenMatchSQL` = `(',' || COALESCE(<col>,'') || ',') GLOB ?` (GLOB, comma anchor, no space-strip); `syncTagColumn` `memory_sync_task.go:65`; four call sites `memory_sync_task.go:316,339` and `memory_sync_delta.go:207,230`. Doc's "was :310/:333/:156/:180" matches `git show 98876783:…` exactly; the *new* numbers are 316/339 (doc wrote 315/338) — see F4. Test file present: `memory_sync_tagcase_test.go`. |
| 6 | `devdoc/ledger-dsn-cmd-agent-contract.md` | task report | **ACCURATE**; gate's own comment self-contradicts | `internal/tools/ledger_dsn.go:37` `LedgerDSN`, `:49` `LedgerReadOnlyDSN`, `:71` `ledgerPragmas` rendering `dbBusyTimeoutMSec` (`memory.go:84` = 15000); named symbols `cmd/agent/cli_daemon_scope.go:411,418,432`, `cli_daemon_spawn.go:557`; gate file `cmd/agent/ledger_dsn_contract_test.go`; doc's own "Not Done: agencyscan keeps one bare opener" is **TRUE** — `internal/agencyscan/store.go:68` `sql.Open("sqlite", path+"?_txlock=immediate")` and the permit is live (the gate passes). Its one defect is F3. |
| 7 | `devdoc/context-snapshot-design.md` | design roadmap | **PHASE 1 PARTIALLY LANDED UNDER DIFFERENT NAMES; Phases 2-5 not landed** | Landed: `internal/checkpoint/index.go:38` `IndexDir`, `:119` `CountRequests`, `parse.go:105` `LastRequest`; CLI `cmd/agent/cli_checkpoint.go:43,60,112,136` (`gopi checkpoint list/export`, `gopi resume`). NOT landed: `GOPI_CHECKPOINT_DIR` (0 hits repo-wide), `~/.gopi/checkpoints` (absent), `gopi clone` (`isCloneSubcommand` 0 hits in `cmd/agent`), `internal/agent/bootstrap.go`, `AgentFromSnapshot`, the no-op clone invariant test. Doc proposed names `checkpoint.Parse`/`checkpoint.Index` do not exist. All Phase boxes still `- [ ]` — see F7. |
| 8 | `devdoc/memorization-effort-design-20260910.md` | discussion, saved ideas | **NOT IMPLEMENTED — and the doc says so** | Header: "想法保存，未实施". Code: `memorization_effort` / `memory_effort` / `MemorizationEffort` / `GOPI_MEMORY_EFFORT` — **0 hits** in `internal/`, `cmd/`. The page is explicitly `<!-- doc-class: historical -->` and closes "下一步（未定——待再设计）", so a reader is not misled. Its listed "已有基础设施" are real: `feedback` dedup M4 (`memory.go:2506`), `context_trim`, effort-mode (`memory_brew.go:98`). Verdict: honest parking lot, no truth defect. |
| 9 | `devdoc/requirements/05-context-and-memory.md` | requirements | **STRONGEST DOC AUDITED — accurate; a few line anchors rotted** | Spot-checked at its own base `812f9a60`: `internal/agent/loop.go:1051` still the memory tool def, `:1055` still the `auto-demote` sentence (verbatim as quoted); `internal/agent/context.go:19/:33` = 200000 / 0.75; `hippocampus_config.go:13` = default-off; `memory.go:2849` at base = `sets = append(sets, "tier=?")`; `FeedbackSuggestCold` at base `:3286`. Rot: the doc writes `FeedbackRecompute` at `memory.go:3160`–`:3176`, but at its own base (`812f9a60`) the function is at **`:3162`** and the end is `:3176`-ish; at HEAD it is at `:3269`. (`compact.go:298` checks out: at both base and HEAD `:298` is `const memorySummaryMarker`, the label heading the injected summary message, and `:314` at base is `referenceOnlyPrefix` — the two definition sites of the injected text, exactly as cited.) Its §6.5 numbers survive re-measurement in shape (exam fill flat at 1,072 while rows grew; hot = 1). Its own finding #7 ("0 of the newest rows carry exam_questions … cause I cannot bound") is **explained** by this audit — see F1. |

---

## 2. memroadmap M1–M4, item by item (against symbols, not prose)

### M1 — "exam_questions + 5W1H parallel recall"

| item (doc §) | status | symbol evidence |
|---|---|---|
| ① schema, add-only (6 cols) | **DONE** | `memory.go:133-138`; heal path `:1002`, `:1029`, `:1256`, `:1313`; `m1KwCols` `:340` |
| ① FTS column set → 12 | **DONE** | `m1FTSCols` `:211-214`; rebuild-on-column-mismatch `:1062`, `:1095`; triggers `m1FTSTriggers` `:825` |
| ① "M1 先不动 tri" | **CHANGED in code (undocumented)** | `m1TriCols` `:219` adds `exam_questions` to the trigram set — F8 |
| ② backfill script | **DONE** | `scripts/backfill_exam_5w1h.py` (`WHERE exam_questions=''` resume, FTS rebuild at `:131-132`); live cohorts 2026-08-24..29 |
| ③ routes: FTS / coverage / LIKE | **DONE** | `:1856` `likeCJKTargets` = `question, topic, exam_questions, kw_what, kw_why` (exactly the doc's list); haystack `:2278-2279` |
| ③.4 **store side**: "LLM 可传 5W1H keywords 时**分维写入**" | **NOT DONE** | `:2555-2561` writes `search_kw` only; the `kw_*`/`exam_questions` branch is `default:` → `""`, commented `:2570-2575` "start empty on store: the LLM contract does not provide them yet" — while the tool schema (`loop.go:1059-1100`) offers **no** per-dimension field, only the flat `keywords` array |
| ④ 12+2 acceptance | **DONE (recorded)** | `scripts/m1_acceptance.py` present; `memory_m1_test.go` 5 tests |

### M2 — "tier semantics + confidence lifecycle"

| item | status | evidence |
|---|---|---|
| ① hot resident injection | **DONE** | `hotResidentInjectLimit=3` `:329`, `hotResidentMaxChars=600` `:330`, `MemoryHotResident` `:3514`, consumed `loop.go:3629-3631`, audited `:3638-3640` |
| ① hot_stale signal | **DONE** | `hotStaleDays=7` `:320`, `hotRecordStale` `:2090`, rendered `:3555` |
| ② lazy decay, no job | **DONE** | `m2EffectiveConfidence` `:3095`, `confDecayTauSeconds=fbDecaySeconds` `:281`, floor `:287`, reward cap/saturate `:294,301`, applied `:1975-1984` |
| ③ feedback → confidence (asymmetric, cooldown) | **DONE** | `:3215-3217` (+0.05 / −0.10), clamp `:3227-3231`, cold suggestion `:3242` in the return text |
| ③ "FeedbackRecompute 不碰 confidence" | **DONE** | comment at `:3207`, `FeedbackRecompute` `:3269` only rebuilds counters |
| ④ FTS sync after UPDATE | **DONE** | `TestM2FeedbackUpdateKeepsFTSInSync`; legacy heal `TestM2EnsureFeedbackSchemaHealsLegacyTierConfidence` |
| ⑤ acceptance (data-driven) | **DONE (recorded)** | `scripts/m2_acceptance.py`; live tier distribution now hot=1 (the doc's table had hot=0 — later, expected drift) |

### M3 — brew pipeline

| item | status | evidence |
|---|---|---|
| CLI `gopi brew` + 5 flags | **DONE** | `cli_brew.go:22-35` usage; `brewUsage` names `--dry-run/--limit/--session/--dir/--json` |
| slice / judge / distill / store 4 stages | **DONE** | `memory_brew.go`, `memory_brew_llm.go`, `memory_brew_run.go` |
| ③ anti-self-reference (tag + no-overwrite) | **DONE** | `memory_brew_run.go:262` `brewGuardBlocked` |
| ④ budget (64 sessions / 12K chunk / ≤3 lessons / 180 s / 4096 tok) | **DONE** | `:69` 64, `:73` 3, `:42` 4096, `:103` 180 s, `:123` 8192 effort-mode; doc's "90s 超时" is the pre-`cfe590a` value |
| trigger: session exit, 24 h, `GOPI_BREW_INTERVAL_SEC` | **DONE** | `BrewIntervalEnv` `memory_brew.go:90`, `maybeBrewOnSessionExit` `cli_brew.go:119`, `main.go:1088` |
| **Not Done**: cron/periodic trigger, chat-mode exit hook | **STILL OPEN** | only the one-shot call site at `main.go:1088`; no hook at `internal/agent/chat_ops.go:417` |
| **Not Done**: `lesson_type` classification | **STILL OPEN** | live: `lesson_type` non-empty on **0 of 4,044** rows |
| **Not Done**: full 842-session sweep | **STILL OPEN** (ops action) | — |
| known-pit #1 `.gitignore` bare `agent` swallows `cmd/agent/*` | **FIXED since — doc now misleading** | `.gitignore:4-6` is anchored (`/agent`) with the fix explained in a comment — F5 |

### M4 — cross-node sync

| item | status | evidence |
|---|---|---|
| `SyncExport` / `SyncImport` / `SetMemoryDBPath` | **DONE** | `memory_sync.go:163,285,381` |
| `gopi memsync --export/--import` | **DONE** | `cmd/agent/cli_memsync.go`; usage cross-references the sibling channel |
| patch is versioned, id-free, node-stamped | **DONE** | `SyncNodeTagPrefix` `:38`; version guards covered by `TestSyncImportNewerFormatPatchRefused`, `TestSyncImportVersionlessPatchRefused` |
| export double gate (tag + `NOT node:*`) | **DONE** | live: 6 brew + 10 `node:` rows; `TestSyncExportLocalNonNode` |
| human rows always win; refusals ≠ rollback | **DONE** | `:252-280` semantics comment; `TestSyncImportConflictHumanWins`, `TestSyncImportGarbageRefused` |
| dedup key-width bug fixed | **DONE** | `memory.go:2506` + `TestMemoryStoreLongQuestionDedup` (`memory_sync_test.go:182`) |
| **Not Done**: import `--dry-run`, auto transfer, reverse channel, tier weighting | **STILL OPEN, as stated** | 0 hits for `dry-run` in `cli_memsync.go` / `memory_sync.go` |

---

## 3. Findings, ranked (with suggested fix + bug-worthiness)

Bug-worthiness = how likely this is to cause wrong behaviour or wasted work, 1 (cosmetic) to 10 (live defect).

### F1 — M1's store-side implementation was never built; the recall surface is frozen at 2026-08-29 — **bug-worthiness 8/10**

**Evidence.** `memory.go:2555-2561` writes `search_kw`; `:2570-2575` (the `default:` branch) writes `""` for
`exam_questions` and `kw_what..who` with the comment "the LLM contract does not provide them yet, and the
backfill fills them for existing rows". The tool the LLM actually reads (`loop.go:1059-1100`) exposes
`action/query/limit/intent/topic/question/answer/source/keywords/type/name/trigger/preconditions/steps/tools_used/id/rating/reason/tool_use_id`
— **no** per-dimension field and no `exam_questions` field. So no caller *can* supply them.

**Measured effect.** Live: 1,072 filled of 4,044 (26.5 %), all distilled 2026-08-24..08-29 (the backfill
window); **2,972 blank**; **0 of the rows distilled after 2026-08-30 carry any** `exam_questions`. The
per-column shape (what 1,072 / why 963 / where 711 / who 698 / when 230, all-five 133) is exactly what a
one-shot backfill produces — a store path would have been incomplete per row, not per column.

**Consequence.** Every row written by gopi since 2026-08-29 sits outside the M1 recall surface: not in
`m1FTSCols` MATCH, not in `likeCJKTargets`, not in the coverage haystack. M1's measured +2 top1 / +3 top3
gain applies only to the 1,072 pre-existing rows while 73.5 % of the store has none of it. `requirements/05`
saw the symptom and could not bound the cause (its "What I could not determine" #7: "0 of the newest 399 rows
carry `exam_questions` … the one finding whose cause I cannot even bound"). This audit bounds it.

**Suggested fix (two honest options, one of them cheap).**
- *(A) Implement it* — the doc's own §3.4. Add optional typed fields to the memory tool schema
  (`5w1h_what/why/when/where/who` or a nested object), persist them at `memory.go:2541-2575`, and generalise
  `scripts/backfill_exam_5w1h.py` from `WHERE exam_questions=''` to `WHERE exam_questions=''` **OR all five
  `kw_*` blank** (its filter already skips rows that have one without the other). Cost: schema/prompt change +
  the §4 acceptance re-run. Risk: recall behaviour changes for the whole store.
- *(B) Retire the columns* — delete the "分维写入" clause from `memroadmap-m1.md` §3.4, record the decision in
  `requirements/05` §6.2, and stop presenting the M1 columns as a live recall feature. Cheapest, and Rule 5's
  "document what you did not do" is satisfied.
- Either way the **tool prompt line stays a defect until it is fixed**: `loop.go:1054` tells the model "2-3 mixed
  中英 keywords **per 5W1H dimension** (WHAT 主题 / WHY 问题 …)" and `memory_brew_llm.go:44,49` repeats it — that
  is a written purpose for typed dimensions the wire contract does not offer, the same class as F2.

### F2 — the memory tool tells the model that ≥2 useless votes auto-demote, which no code path does — **bug-worthiness 6/10**

**Evidence.** `loop.go:1055` (verbatim, still at HEAD): "≥2 useless votes auto-demote to cold (reversible)".
M2's contract is `only-up, never auto-down`: the only tier writer is `memory_update`
(`memory.go:2849` at 812f9a60 = `sets = append(sets, "tier=?")`), and the vote path only *suggests*
(`:3242` renders "COOLDOWN … consider `memory_update <id> --tier cold` (suggestion only)"; `FeedbackSuggestCold`
`:3393`). `requirements/05` W6 flagged this and it is still unfixed.

**Consequence.** The one consumer of that sentence is the LLM: it is told a mechanism exists and may therefore
skip the manual demotion it is actually responsible for. A cheap, high-leverage correctness fix.

**Suggested fix.** Reword to what the code does — "≥2 decayed useless votes surface a cold *suggestion*; demotion
is a manual `memory_update`". One string; no behaviour change. (If instead the machine should demote, that is a
policy decision and belongs in `requirements/05` first — do not fix the text by building automation.)

### F3 — the ledger gate's own comment contradicts its own scan roots — **bug-worthiness 2/10**

**Evidence.** `cmd/agent/ledger_dsn_contract_test.go:39-43`: "The gate deliberately does NOT scan
internal/agencyscan … an agencyscan TEST importing the helper would close an import cycle." But
`:167-170` `ledgerScanRoots` **includes** `internal/agencyscan` (with its own read-floor
`ledgerMinAgencyscanGoFiles = 5` at `:243`), and `:56-59` carries the three name-based permits including
`agencyscan_test.go`. So the tree *is* scanned and its two bare openers are permitted by name, with the
cycle reason attached to the permit — the correct design. The comment is a leftover from the earlier shape.

**Consequence.** A future agent reading only the comment believes the tree is unscanned and may "restore" the
permit-free state, or may re-add the tree blindly as the comment fears. The report's `## Not Done` entry is
still accurate (one bare opener remains, `internal/agencyscan/store.go:68`) — only the *reason given in code*
is wrong.

**Suggested fix.** Replace the comment with the permit-based statement; no test change.

### F4 — `sync-tag-case-policy.md`'s post-fix line numbers are 1 off in one file — **bug-worthiness 2/10**

**Evidence.** The doc cites the two new `memory_sync_task.go` sites as `:315` / `:338`; HEAD has them at
`:316` / `:339` (`memory_sync_delta.go` `:207`/`:230` are exact). The "was :310/:333/:156/:180" column is
verified correct against `git show 98876783:internal/tools/memory_sync_task.go` (LIKE form at 310) and
`…memory_sync_delta.go` (LIKE at 156/180). The *substance* of the doc — GLOB replaces LIKE at all four gates,
comma anchor, no space-strip, metacharacters unescaped — is exactly what `memory_sync_delta.go:153` implements.

**Suggested fix.** Correct the two numbers, or (better) make the doc cite the symbol (`syncTagTokenMatchSQL`
call sites) instead of a line, which is what rots.

### F5 — `memroadmap-m3.md`'s "known pitfalls for future agents" now contain two false statements — **bug-worthiness 4/10**

**Evidence.**
1. Pit #1 says `.gitignore`'s bare `agent` pattern swallows `cmd/agent/*` and demands `git add -f` — **true at
   m3's base `621a60e`** (`git merge-base --is-ancestor 4b06759b 621a60e` → no, i.e. the fix was not in that base).
   `.gitignore:4-6` is now **anchored** (`/agent`) with a comment naming exactly that bug ("unanchored "agent"/"gopi"
   accidentally ignored cmd/agent/* and internal/agent/*"), landed in `4b06759b` (2026-08-29, an ancestor of HEAD).
   The note is a stale habit, not a wrong observation at the time it was written.
2. The report lists `TestAutoTaskRegisterFailedStatus` / `TestChatTaskRegistration_WrapperFailedStatus` as
   pre-existing failures. Measured now: `go test ./internal/agent/ -run 'TestAutoTaskRegisterFailedStatus|TestChatTaskRegistration_WrapperFailedStatus' -count=1` → **`ok github.com/nick/gopi/internal/agent 5.627s`**. The bug row (`t-1788021020`) has been fixed.
3. Smaller drift in the same doc: "每次调用 90s 超时" vs `brewClientTimeout = 180 * time.Second`
   (`memory_brew.go:103`, raised in `cfe590a`), and "808.3s 预算 3-8 分钟区间内" which is the same sentence's
   own contradiction.

**Consequence.** A second-hand "the agent suite fails on master" belief propagates to new agents (this exact
family of stale-claim cost real work in earlier tasks). The pitfall about `git add -f` causes an unnecessary habit.

**Suggested fix.** Strike the two resolved pitfalls (or mark them "fixed — the anchor is `4b06759b`,
2026-08-29, itself already an ancestor of master"); align the timeout number with the constant. Do not silently
delete the history — annotate it, as the doc already does elsewhere.

### F6 — citation rot in `requirements/05` (the doc whose value *is* its citations) — **bug-worthiness 3/10**

**Evidence.** The doc's method is explicit: all 57 `path:line` citations re-read at base `812f9a60`. At that
commit `FeedbackRecompute` is at `:3162`, not `:3160` (the doc both asserts `:3160`–`:3176` and notes the
`:3283/:3286` correction for its sibling — an inconsistency inside one paragraph). At HEAD the function moved
to `:3269`. The anchors that matter most survived: `loop.go:1051` and `:1055` are still exact at HEAD, and
`compact.go:298`/`:314` (the injected summary's marker + reference-only prefix) are still exact at base.

**Consequence.** Low today (the substantive claims check out), high over time: this is the doc that other docs
and agents cite. The project already has the machinery to stop it — a test that resolves every `path:line`
citation in `devdoc/requirements/*.md` and fails on a miss is ~30 lines of AST-free file+line checking and is
the same shape as the Rule 13 literal gate.

**Suggested fix.** Prefer symbol citations (`FeedbackRecompute` in `internal/tools/memory.go`) for anything
that moves; add the resolver test if the citation density here is going to keep growing.

### F7 — `context-snapshot-design.md` is a roadmap whose landed half does not use its names — **bug-worthiness 3/10**

**Evidence.** Phase 1's checklist items (`- [ ]` at `:389-394`) name `checkpoint.Parse`, `checkpoint.Index`,
`GOPI_CHECKPOINT_DIR`, snapshot manifest + `sha256`. What exists: `internal/checkpoint/index.go:38` `IndexDir`,
`:119` `CountRequests`, `parse.go:105` `LastRequest`, `:69` `findMarker`, `index.go:24` sha256 of the payload —
and the CLI is `cli_checkpoint.go:43` `list` / `:60` `export` / `resume`. `GOPI_CHECKPOINT_DIR` has 0 hits
repo-wide; `~/.gopi/checkpoints/` does not exist; `gopi clone`, `AgentFromSnapshot`, `internal/agent/bootstrap.go`
have 0 hits. Phases 2-5 entirely unlanded. The doc is `<!-- doc-class: historical -->` and never claims "done".

**Consequence.** A reader cannot tell "planned" from "planned with different names, half built". The names
matter because the doc's §4.9 file layout is a promise about where conversation-bearing files (`0600`) live.

**Suggested fix.** Add a one-paragraph status banner at the top: what landed (and under which symbol), what did
not, and that Phase 2 (clone) is a design aspiration — no code. Cheap and it is the only thing keeping this doc
from being a trap.

### F8 — M1 §1 said "leave the trigram index alone"; the code extended it — **bug-worthiness 1/10**

**Evidence.** `memroadmap-m1.md` §1: "`distilled_qa_tri`（trigram）：列集保持 question/answer/topic 不动 …
**M1 先不动 tri**". `memory.go:219` `m1TriCols = "question, answer, topic, exam_questions"`; the column-set
resize path is `:1256,1313`. The change is defensible (alternate phrasings are whole CJK sentences, so a
substring of a phrasing recalls the row) and is documented in the code comment — it is simply not in the doc
that a future implementer reads.

**Suggested fix.** One line in the doc noting the implemented decision. No code change.

**Carried forward, not a finding (audit boundary).** `requirements/05` "What I could not determine" #8 —
whether candidate A's injection hit-rate gate (<50 % → report "不做") was ever answered. Still no ledger row
reporting that number; the instrument is `internal/agent/memconsult_test.go`'s `TestMemoryAdoption_*` family,
which this documents-only task did not run.

---

## 4. Not Done (honest)

- **The 10 `devdoc/hippocampus*.md` docs were not audited** — outside the briefing's named 8 + sibling. The
  verdict rows cover only what was assigned.
- **The 05 doc's session-JSONL adoption counts were not re-measured** (knowledge-base call counts, per-file
  first-call ordering, `context_trim`/`job`/`task` tallies). Only the ledger-side numbers were re-read. Those
  JSONL numbers are ~3,000 files of scanning and are not what any verdict row rests on.
- **No doc was edited.** This task produces the audit only; every suggested fix above is dispatchable, and the
  two I would dispatch first are F1 (needs a decision: implement or retire) and F2 (one string). **Nothing was
  submitted to the task queue.**
- The `scripts/m1_acceptance.py` / `m2_acceptance.py` numbers were not re-executed; they are quoted from the
  docs as *recorded* results, and the verdict rows never rest on them.
- `memroadmap-m2.md`'s claim "hot 未填充（M2 提供机制，hot 化由使用积累）" is now **hot = 1** in the live
  store, i.e. one row was promoted and — per `hotResidentInjectLimit` — is being injected at task start. Not a
  defect; noted so the number is not read as current.

## 5. Verification

- Deliverable: this file, branch `feat/mem-doc-truth-audit`, worktree `~/workspace/gopi-mem-doc-audit`, base
  `52090dc0`. Worktree removed mid-session by another process's sweep and re-materialised
  (`git worktree add ~/workspace/gopi-mem-doc-audit feat/mem-doc-truth-audit`) — the branch was never at risk.
- **Build/tests** (no production code changed; run to make sure the audit's claims sit on a green tree):
  - `CGO_ENABLED=0 go build ./...` → exit 0.
  - `go test ./internal/tools/ -run 'TestM1|TestM2|TestSyncTag|TestMemoryStoreLongQuestionDedup|TestLedgerDSN' -count=1`
    → `ok github.com/nick/gopi/internal/tools 51.956s`.
  - `go test ./cmd/agent/ -run 'TestLedgerDSN|TestLedgerBare' -count=1` → `ok … 1.355s`;
    `go test ./internal/tools/ -run 'TestLedgerDSN' -count=1` → `ok … 1.742s`.
  - `go test ./internal/agent/ -run 'TestAutoTaskRegisterFailedStatus|TestChatTaskRegistration_WrapperFailedStatus' -count=1`
    → `ok … 5.627s` (the F5 evidence that the m3 "pre-existing failures" claim is stale).
- **Code evidence** is `file:line` at HEAD `52090dc0` as cited inline; doc-base comparisons use
  `git show 812f9a60:…` and `git show 98876783:…`.
- **Live ledger** read with `sqlite3 ~/.gopi/memory.db` in read-only queries; the readings are stamped
  2026-09-19 and the store is live (its row count moved during the session's own work).
- Rule 30: `git status --porcelain` in the worktree prints nothing after this commit.
