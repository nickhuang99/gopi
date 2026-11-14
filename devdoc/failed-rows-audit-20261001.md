<!-- doc-class: historical -->
# Recent FAILED rows — what happened to them: solved elsewhere, or nobody ever re-dispatched?

**Task:** `01a0f3fc-13d3-7103-8185-5a46823147dd` (epic: *audit the recent failed rows — is the work solved
elsewhere, or is nobody dispatched?*), from `briefings/brief-audit-recent-failed-rows.md`.
**Instrument:** `scripts/gopi-failed-audit.sh` (read-only, re-runnable; this report's numbers are that
script's output — see `## Reproduction`).
**Ledger measured:** `/home/user/.gopi/memory.db` (md5 `6a5647522cd1970029e7134fcfff4ece`, 212 217 856 bytes —
read with `sqlite3 -readonly` only; not one row was written).
**Git measured:** `/home/user/work/GoPi` at master `e9040a09` (2026-10-01).

---

## 1. The window, and why it is 7 days and not the 3 the operator asked for

```
$ sqlite3 -readonly ~/.gopi/memory.db "select source,count(*) from tasks \
    where status='failed' and created_at > strftime('%s','now')-3*86400 group by source;"
agent=1
server=7
$ ... source='human'
0
```

**Zero `source=human` rows in 3 days.** The operator's question was *"我看看我提的任务为什么失败"* —
a 3-day window cannot answer it, because in that window he filed none that failed. Widened to **7 days**,
which is also the recent *cluster* the briefing measured (09-26..09-27: 8 human rows; the next human
cluster back is 09-22).

### Counts per source, 7-day window (all four sources present in it)

| source | rows | note |
|---|---:|---|
| `server` | 22 | **21 of 22 are `[auto]` probe/verb rows**, not work items (§6) |
| `agent` | 19 | the bulk: 6 rework rows + 13 defect reports |
| `merger` | 9 | automatic merge-handover failures, no work identity of their own (§4.3) |
| `human` | 8 | **the operator's own rows — all 8 below, full ids** |
| **total** | **58** | |

Raw failed baseline for reconciliation (the filtered CLI view hides `resolved`-tagged rows — the
known header-miscount defect, memory #2128): `select count(*) from tasks where status='failed'` = **289**.

---

## 2. Every `source=human` row — why it failed, and what happened next

**The mechanism, measured for the whole set (7 of the 8):** the worker child died with a non-zero
exit inside **5–6 seconds**, wrote the 1113-byte *"the model wrote no report"* skeleton, and the
harness recorded the row `failed`:

```
$ sqlite3 -readonly ~/.gopi/memory.db "select event||' :: '||substr(detail,1,120) from task_events \
    where task_id='01a0de59-b406-72b0-8699-59e5bd4c602b' order by created_at desc limit 1;"
failed :: gopi run failed: exit status 1 — task run failed (abnormal child exit) — run: elapsed=6s
          — task-report: …/01a0de59-…/task-report.md (1113 bytes)
$ ls -l ~/.gopi/sessions/01a0de59-b406-…/task-report.md   → 1113 bytes (the "not recorded" skeleton)
```

So: **these rows did not fail at the work — they failed at the model call.** No reasoning, no diff,
no diagnosis exists for any of them. That is why "why it failed" below is the *routing* failure
(worker could not run), and "what happened next" is the only informative half.

| # | task id (full) | filed | why it failed | what happened next |
|---|---|---|---|---|
| 1 | `01a0de59-b406-72b0-8699-59e5bd4c602b` | 09-26 23:33 | worker child exit 1 after 6 s; no report written | Rework of `fix/chat-table-row-head-and-double-marker` (tip `3676d3bb`, parent `46926a2e` **is** on master ⇒ the tip's own commit never landed). Ref now **gone**; archived patch path in the row is **absent**. Its subject matter *did* land — `9c6b8da8` (09-27 03:01) *"stop carrying a partial paint's markers onto a committed row"*. Content in master, delivered by another task. |
| 2 | `01a0de59-b842-7099-bb24-0fbded4f78c0` | 09-26 23:33 | same | Rework of `task/01a0dcfad9bd…` (tip `753bbb92` = *"docs(euaudit): slice 4 — honest ledger-count footnote"*, parent not on master). `devdoc/end-user-audit-protocol.md` on master today carries slices 2,1,3 and **no slice 4** — so slice 4 never landed. The epic it belonged to is finished and documented elsewhere. → UNCLEAR/MOOT (§5). |
| 3 | `01a0de59-ba90-7621-be43-4d997a8df74b` | 09-26 23:33 | same | Rework of `task/01a0dd878315…` (tip `fc923e5f` = *"fix(ui_state): the named --session is settled before any artifact answer"*, three files). **Solved-elsewhere:** `55c8f5f4` (09-27 09:24) *"fix(ui_state): one honest exit contract for --compare, and select a surface by its session"* — same defect, delivered by another task. The tip's own files still differ from master (no blob overlap). |
| 4 | `01a0de60-5f66-7d04-a00b-f85e37cf30a9` | 09-26 23:40 | same | Rework of `task/01a0ddbdfb9f…` (tip `f2cd329b` = *"feat(ui,chat): report the emphasis fidelity event instead of falling back silently"*, **parent `933c9a8c` is on master ⇒ the tip's commit never landed**). No emphasis-"fidelity" contract exists in master's `internal/lineedit/emphasis.go` / `internal/uiclient/dump.go`; the added test `internal/lineedit/emphasis_fidelity_test.go` is absent. → **live, not delivered as specified** (§3). |
| 5 | `01a0de60-61cd-77c6-bd13-033625e3004c` | 09-26 23:40 | same | Rework of `task/01a0dde884ad…` (tip `53ff7533`, `devdoc/DOC-INDEX.tsv`). **Solved-elsewhere:** the tip is an **ancestor of master**; the sibling DOC-INDEX union landed as `369c3e5d` (09-26 23:56) *"merge: union the append-only rows of devdoc/DOC-INDEX.tsv (2467 rows, mechanical, gopi merge union lane)"*. |
| 6 | `01a0de60-649e-74dc-b97a-edf1f85b3388` | 09-26 23:40 | same | Rework of `task/01a0dde954de…` (tip `dc80cd25` = *"fix(ui_state): --session is consulted or refused by name — one conflict table at parse time"*, three files). **Solved-elsewhere:** the area was re-fixed by `55c8f5f4` and `842e5f34`; the tip's files differ from master (no overlap). |
| 7 | `01a0df07-b744-7e04-bc78-a0852c384e86` | 09-27 02:43 | same (`merge-sweep` tag) | Rework of `task/01a0deb741d4…` (tip `3a750a68`, 7 commits, archived `…-rework.diff` — **that path is absent now**). Its three asks: (1) `doccheck` verb registered → **on master** (`master:cmd/agent/cli_verbs.go:84: CliVerbDocCheck = "doccheck"`); (2) "one master-reds doc" → master carries **11** `master-red` docs, but **not** the one this diff added (`devdoc/cmd-agent-master-reds-20260927.md` = absent); (3) "shot startup-bound export" → **UNCLEAR**, the phrase does not resolve on master. 2 of 3 discharged, 1 undetermined. → partially solved (§3). |
| 8 | `01a0dfd9-98ff-72be-ad47-cee8024fbdfa` | 09-27 06:32 | same — **this one has a verbatim payload and no successor** | Payload: *"why cannot merge? why /home/user/workspace/gopi-test-scratch/e2merge-01a0df2d is mentioned instead of master? what is checkout branch now? master? findout and fix"*. It is the operator's own diagnosis request. **Live evidence that nobody answered it:** the scratch path it names **still exists** (`/home/user/workspace/gopi-test-scratch/e2merge-01a0df2d`) weeks later; `git branch --list '*01a0df2d*'` is empty; the session wrote no report (same 1113-byte skeleton). → **STILL-NEEDED** (§3, item 1). |

**One line per row as asked. The honest summary:** six of the eight are the *same* failure — the worker
could not run at all, three of them inside a single merge-run rework burst at 23:33/23:40 on 09-26 —
and their subject matter was mostly re-derived by other tasks in the following 48 h. **Two carry a
question the operator asked that nothing in the ledger ever answered** (#4's "report it instead of
failing silently", #8's "why cannot merge"). He is right to want this list: the rows are
indistinguishable from success once nobody re-reads them.

---

## 3. STILL-NEEDED — live problems, nobody dispatched

**Tally: 4 directly verifiable + 2 UNCLEAR-live, plus the 2 unmade merge-run reworks in §5.**
Items 3 and 4 below were *reclassified while this audit ran* — they are kept in the list because the
ledger row still reads `failed` and would mislead the next reader, but they are **not** still-needed.
Nothing here was re-dispatched: read-only audit, the operator decides.

Ordered by how directly the operator asked for them. Every line carries the command that shows it.

1. **`01a0dfd9-98ff-72be-ad47-cee8024fbdfa`** *(human)* — "why cannot merge, and why does the
   merge-run name a test-scratch worktree instead of master?" **Why it failed:** worker child exit 1,
   5 s, no report. **Live evidence:** the directory the row names is still on disk —
   `ls -d /home/user/workspace/gopi-test-scratch/e2merge-01a0df2d` → present; the branch it asks
   about is gone (`git branch --list '*01a0df2d*'` → empty); `grep -n '01a0dfd9' <master>` → nothing
   but this task's own row. Nobody answered it.
2. **`01a0de60-5f66-7d04-a00b-f85e37cf30a9`** *(human)* — the emphasis-fidelity event: a renderer
   that falls back to plain text **and reports nothing** (Rule 15). **Why it failed:** same worker
   burst. **Live evidence:** the tip's parent `933c9a8c` **is** on master, so the tip's commit is
   exactly what is missing: `git cat-file -e master:internal/lineedit/emphasis_fidelity_test.go` →
   absent; `git grep -ln 'fidelity' master -- internal/lineedit/emphasis.go internal/uiclient/dump.go`
   → no hits. The defect the row describes (silent degradation) remains observable by inspection.
3. **`01a0df53-2464-7ed6-be5d-bc827d3cabda`** *(agent, 09-27)* — `TestWorktreeSweepRefusesEvictionOfAPeerCheckout`
   fails on master, all three subtests; **nobody re-dispatched it.** **Evidence, measured here:**
   `go test ./internal/tools/ -run TestWorktreeSweepRefusesEvictionOfAPeerCheckout -count=1` →
   `ok github.com/nick/gopi/internal/tools 3.457s`. → **This one has since gone green** (its area was
   reworked by `19fb90bc`). Reclassified as *solved-elsewhere while this audit ran*; listed here
   because the ledger row still reads `failed` and would mislead the next reader. **No ledger write
   was made — read-only.**
4. **`01a0df52-fc39-7d47-9df6-ac69b88403e3`** *(agent)* — its sibling. `internal/tools/worktree_removal_ground_gate_test.go`
   (`TestEveryWorktreeRemovalArmQuestionsTheGround`) was red on master. **Evidence:**
   `go test ./internal/tools/ -run TestEveryWorktreeRemovalArmQuestionsTheGround -count=1` → **ok**.
   Solved-elsewhere too; same note stands.
5. **`01a0e1c9-5713-70e4-b3db-16f835275ead`** *(agent)* — a scripted pty paints `gopi chat`'s typed
   line and the dump is written, but the **transcript** never receives the line. **Evidence of live
   status:** the script's own measurement — the only file the row names,
   `devdoc/chat-context-self-knowledge.md`, was last touched **2026-09-28** (2 commits since the row),
   and no commit message names the defect. `git log --since=2026-09-27 master -- cmd/agent/cli_chat.go`
   shows later commits, none about the transcript half. → **UNCLEAR-live**: the instrument moved
   (`b77e1115`, `af371e41`), the reported defect was never addressed by name. Honest verdict: nobody
   was dispatched.
6. **`01a0e313-ac65-7eac-af96-141404f159f6`** *(agent)* — `ui_state --all` reports `paint_loss` for a
   95-col ASCII line in a 117-col grid. **Evidence (measured by the script):** the row's own files —
   `internal/uiclient/audit.go` last touched 09-28, `internal/uiclient/compare.go` last touched 09-29,
   7 commits each since the row — moved repeatedly *after* the report without any of them claiming
   this case (`e167bbb9`, `842e5f34`, `782afbec`). → **UNCLEAR-live**: many later fixes in the same
   files, none of them this defect. Nobody was dispatched to it by name.
7. **`01a0ef8a-ec07-7ef0-8eb9-86fed6994677`** *(agent, 09-30)* — the asset re-render batch
   ("re-paint the remaining 142 items' 04 with `rerender_entry.py`; hard gate = 03-raw byte-identical").
   **Evidence:** the tool it names **is on master** (`scripts/materials/rerender_entry.py`); the
   script measures the row's own files (`devdoc/materials-box-geometry-audit-20260930.md`,
   `scripts/materials/box_audit.py`) as last touched **2026-09-30**, i.e. after the row, and no later
   row references it. → **STILL-NEEDED** if the material set is still in use; **UNCLEAR** from the
   ledger alone whether the batch still matters. Flagged for the operator.
8. **`01a0ddb1-7bc2-7694-84d8-a43d4f4f130b`** *(agent, 09-26)* — *"gopi chat: copy/paste loses the gold
   colour — decide whether it is fixable in gopi (OSC 52 …)"*. The task's deliverable was **an answer,
   not a guess**. **Evidence:** no successor row references it (`select … where payload like
   '%01a0ddb1-7bc2%'` → itself only); no `OSC 52`/`osc52` implementation on master; the one file its
   row names, `devdoc/chat-tool-activity-band-row.md`, was last touched 09-27 (2 commits since) and
   neither is about copy/paste colour. → **STILL-NEEDED as a decision**: the operator-reported symptom
   has no recorded answer anywhere.

**Also live, non-human:** the merger rows (§4.3) are a *system* defect, not work items — the row that
names it belongs to the sibling task, not this one.

---

## 4. SOLVED-ELSEWHERE — with the commit or file that solved it

### 4.1 The rework rows whose content IS in master (per-file blob equality)

`git merge-base --is-ancestor <tip> master` is the strongest single test; blob equality is the
strongest per-file one.

| tip | what it was | verdict |
|---|---|---|
| `53ff7533` (`task/01a0dde884ad…`) | DOC-INDEX.tsv rows | **ancestor of master**; the union landed as `369c3e5d` (09-26 23:56) |
| `40035f03` (`01a0dd5e-…`) | archived diff of a pruned branch | **ancestor of master** |
| `99f6bbf3` (`01a0d31e-…`, master-red `TestAgent_*`) | truncated-run writeback | **ancestor of master**; the named tests exist on master (`internal/agent/truncated_run_test.go`, `internal/agent/loop_stuck_test.go`) and their parent task `01a0d64f-05ce-…` is `completed` |
| `5710f683` (`01a0dd66-…`, SUPERSEDED-NOOP) | sandbox-ledger merge | **ancestor of master** — the row itself says so, and the object agrees |
| `c183aab6` | master at the time of the 23:33/23:40 rework burst | **ancestor** (it *was* master) |

### 4.2 The judge-resolves-work rows

| failed row | solved by |
|---|---|
| `01a0d366-ab33-…` (7 worktree-residue e2e arms) | successor `01a0f3cf-7d3c-…` **completed** 10-01 (the failed-rows-silent-forever task, whose briefing catalogues this id) |
| `01a0d533-7577-…` (bare-prompt SIGTERM) | successor `01a0d64f-090c-…` **completed** 09-25, titled exactly that fix |
| `01a0d31e-6d56-…` (master RED) | successor `01a0d649-c2b1-…` **completed** 09-25 |
| `01a0df19/01a0df1b-…` (wrapped line / buffered render markdown) | successor `01a0f3cf-7d3c-…` **completed** 10-01 (catalogue), plus the lineedit commits `9c6b8da8` (09-27) and `18db7f2f` (09-29) |
| `01a0df41-7eae-…` (Ctrl+C at the claim boundary) | **the ruling landed as a briefing**: `f5a40f56` 09-27 03:46 `docs(brief): Ctrl+C is a flag the worker reads at the claim boundary…`; the mechanism was already in `cli_tasks_worker_stop.go` (`consumed at the claim boundary`) |
| `01a0ddb1-…` — *not* solved; see §3.8 | — |

> ⚠️ **Catalogue ≠ fix (a trap this audit had to defuse).** The successor query's most frequent hit
> is `01a0f3cf-7d3c-7b9f-b177-44228c1365c8` — *"a failed row is SILENT forever"*. It is a briefing
> that **lists** failed row ids; it fixes none of them. Counted as "handled" it would have made this
> whole audit read as *mostly solved*. Verified: `git show master:briefings/brief-failed-rows-are-silent-forever.md`
> contains **7** `01a0…` ids and does **not** contain `01a0dfd9` (the operator's row).

### 4.3 Merger rows — MOOT (the instrument, not the work)

All nine `source=merger` rows are `merge handover failed: task/<id> (1/3)`. Their target branches are
**all gone** (`git rev-parse --verify refs/heads/task/<id>` → nothing, for all 7 distinct targets and
`feat/rule31-scope`), and every one of their source rows is **`completed`**
(`01a0d500-…`, `01a0d513-…`, `01a0d51b-…`, `01a0d530-…`, `01a0d99a-…`, `01a0d9d4-…`). Rule 31 itself
is on master (`GOPI.md:713`). **The work landed; the handover row failed.** Nothing to re-dispatch.

---

## 5. MOOT / UNCLEAR — one line each

- `01a0de59-b842-…` *(human)* — **MOOT**: its branch body was a "slice 4" footnote to an epic
  (`end-user-audit-protocol.md`) that master carries through slices 2/1/3 and closed elsewhere.
- `01a0de59-b406-…` *(human)* — **SOLVED-ELSEWHERE** (`9c6b8da8`); listed under §2 because the
  operator asked for the row, not the class.
- `01a0df20-ecf5-…` *(agent)* — **UNCLEAR-live**: `auditBaselineStyle` still defines the row's plain
  style as *"the style covering the most cells"* (`master:internal/uiclient/audit.go:728`), i.e. the
  heuristic the row blames is unchanged — but master's `audit.go` was rewritten twice since
  (`e167bbb9`, `782afbec`) and neither commit claims this case, and **no test pins it**
  (`git grep -n emphasis_unpainted master -- '*_test.go'` → none). Not verifiably fixed; not
  verifiably broken. **Reported, not guessed.**
- `01a0df4c-6ce3-…` *(agent)* — **PARTLY solved**: the dump/geometry half moved (`af371e41` 09-27
  00:44 *"the dump sink sees the paint stream in the terminal's order"*, plus `b77e1115`,
  `04a540ed`, `09861dfc` 09-28); the **mid-paint freeze-gate half** is not settled by any commit
  merely about it (`chatDumpFinalize` still "freezes the sink on its last PAINTED frame",
  `master:cmd/agent/chat_dump.go:395`). **UNCLEAR.**
- `01a0ddd9-0715-…` *(agent)* — **SOLVED-ELSEWHERE**: `internal/docindex/generator.go` on master
  carries the re-adjudication guard the row asked for (lines 131, 220).
- `01a0de72-cbf2-…`, `01a0de72-df3e-…` *(agent, 00:01)* — **STILL-NEEDED**: tips `67d19cbf`, `1a4f3c7d`
  are **not** ancestors of master and add files master lacks
  (`devdoc/merge-git-locale-pin.md`, `devdoc/task-next-removal-advice-live-verbs.md`,
  `cmd/agent/task_removed_advice_live_verbs_test.go`); **no successor**. But the *area* moved on
  (`b93301d6`'s sibling landed; master's `merge_locale_test.go` carries the locale pin the row was
  about). → the merge-run left two reworks unmade; whether they are still wanted is the operator's call.
- `01a0d5bb-*`, `01a0da21-*`, `01a0dd6f-ad9e-…`, `01a0df17-db21-…` *(merger)* — **MOOT** (§4.3).
- `01a0df17-db21-…` *(merger)* — **MOOT**, but note the instrument defect: the row's own
  `source` is `merger` and its target `feat/rule31-scope` no longer exists; Rule 31 is on master.
- `01a0dda3`, `01a0dda4`(×2), `01a0d590`, `01a0d591`, `01a0dea2`, `01a0df06`, `01a0e0b1`, `01a0e17e`,
  `01a0e1f6`, `01a0e1f7`, `01a0e331`, `01a0e361`(×2), `01a0edee`, `01a0eedf`, `01a0eee5`, `01a0eeed`,
  `01a0ef60`(×2), `01a0f235` *(server)* — **MOOT/NO-VALUE by shape**; see §6.

---

## 6. `server` rows: 21 of 22 are instrument noise, and one of them is a real symptom

Every `server` failed row in the window has the `[auto] ` preface and a body that is a *verb* or a
*probe*, not work: `frobnicate`, `frobnicate9`, `revision`, `repos list`, `ps`, `status`,
`settings show` (×5), `branch list` (×2), `session <id>`, `Reply with exactly: ok`, two
"cross-harness capability probe" rows, and one operator-pasted Chinese prompt. Two carry the
diagnosis already:

```
01a0dda3-…  reason: "The epic body is the single word \"revision\" and the ledger row proves it
             carries no requirement: payload length 0, run_c…"
01a0dda4-c8c1-…  reason: "PROVABLY unattainable in principle: \"frobnicate9\" carries no
             specification at all (no object, behaviour, file, or accept…"
```

These are **not failures of work**; they are rows that should never have been queued as work.
They are safe to treat as MOOT — but the *pattern* is a finding: a `[auto]` row is indistinguishable
from a work row in every list the operator reads.

**The one that is not noise:** `01a0f235-0041-7d3a-9589-1e3bd17f384c` (09-30 20:05) —
*"拍一张截图，证明你现在跑在哪个窗口里 —— 必须是承载你这个进程的那个窗口…"*, the `screen_capture self:true`
acceptance test. It is the newest failed row in the ledger (≈30 min before this run started), it is a
**capability probe with a real pass/fail meaning**, and its own log shows the session reasoning about
it without reaching a verdict. **It is UNCLEAR, and it is worth the operator's eye** — if `self:true`
cannot resolve on this host, that is a live product gap, not a probe artefact.
(`~/.gopi/sessions/987acc95-cd98-43b6-add3-a9f9f6635c40/signal-provenance.jsonl` exists — 613 bytes —
which at least shows the session tried to attribute a signal source.)

---

## 7. What this audit did NOT determine (and what was tried)

- **Whether `01a0df07`'s third ask ("shot startup-bound export") is on master.** Tried:
  `git grep -n 'startup.bound\|StartupBound\|STARTUP_BOUND' master -- cmd/agent internal/shot` →
  only `TestWorkerOnceFaceKeepsTheStartupBoundary`, a worker test, unrelated name. Not resolved →
  **UNCLEAR** rather than guessed.
- **Whether the removed archived diffs still exist anywhere.** Tried:
  `ls /home/user/workspace/ | grep merge-run-archive` → 0; `ls /home/user/workspace/gopi-branch-01a0deb741d4…-rework.diff` → absent;
  `find /home/user -maxdepth 4 -name 'merge-run-archive-*'` → timed out at 2 min (abandoned, not
  reported). The script prints the row's own sentence and stops — it does not read the operator's
  workspace.
- **Whether a branch was merged, pruned, or never pushed.** Ref absence is ref absence. Only per-file
  blob truth answers "is the content in master", and it is only available while the object survives.
- **Why the worker children exited 1 in 5–6 s.** The rows carry no reason beyond
  *"abnormal child exit"*; the session files for those runs hold no model output. Root cause not
  established here. (The sibling epic `01a0f3cf-…` is "why nobody hears about these"; this task is
  "what is the state of these".)
- **`01a0df41`'s** exact landing commit: the *ruling* is on master as a briefing (`f5a40f56`); whether
  the implementation was changed *because of* the ruling or already matched it was not resolved.
  `git grep -n 'claim boundary' master -- cmd/agent/cli_tasks_worker_stop.go` shows the phrase in
  place, so the behaviour the ruling demands is present. Stated as *MOOT-with-UNCLEAR-provenance*.

---

## 8. Reproduction

```bash
bash scripts/gopi-failed-audit.sh --repo /home/user/work/GoPi        # 7 days (default)
bash scripts/gopi-failed-audit.sh --days 3                           # the window the operator asked for
```

Read-only in the strongest available form: the ledger is opened `sqlite3 -readonly` and every
statement is a `SELECT`; git is invoked only through `rev-parse / merge-base / rev-list / cat-file /
grep / log`; **no file is written** (stdout only), so there is no `/tmp` and no scratch directory.
The script prints, rather than inventing: the 3-day zero-human measurement, the per-source counts,
the full-id row table, the successor of every row, each named branch's ref state, per-file blob truth
for every surviving tip, the payload tip commits, an area-activity measurement, and a closing
*"what this script did not determine"*. Its counts are the ones quoted in §1.

**Verification of no-mutation (this run):**
```
$ grep -inE 'insert|update |delete |drop |alter ' scripts/gopi-failed-audit.sh   # → no hits
$ grep -n sqlite3 scripts/gopi-failed-audit.sh
61:readonly SQL_BACKEND="sqlite3 -readonly"        # the only handle this task opens
$ git status --porcelain            # in the worktree, after the work
(clean)
```

**An honest correction, measured.** An earlier draft of this section claimed the ledger file was
byte-identical across the task. It is **not**, and the claim is retracted:

```
$ md5sum ~/.gopi/memory.db      # at the start of the audit:  6a5647522cd1970029e7134fcfff4ece
$ md5sum ~/.gopi/memory.db      # at the end:                 b413a2e6194f2170e8c2ad8cc9b5167d
$ sqlite3 -readonly ~/.gopi/memory.db "select count(*) from tasks"     # still reads (no lock taken)
```

The bytes changed because **the ledger is live** — the resident worker/merger on this host is writing
rows while the audit reads them. That is a property of the ledger, not an effect of this task: every
statement this task issued went through `sqlite3 -readonly` (a SELECT-only handle, which cannot write
even by accident), no `UPDATE`/`INSERT`/`DELETE`/`ALTER`/`DROP` appears in the script, and no row was
created, closed, cancelled or archived. What this section can prove is the **absence of a write path**
in the instrument; it cannot prove the file's bytes are stable, and it should not pretend to. This is
also why the report quotes counts with the instant they were taken — they drift under the reader.

**The script's own per-file measurement** (the last-touch table, abridged to the rows §3/§5 lean on —
the full table is the script's `named-area` section):

```
01a0dfd9-98ff-…  (human)  cannot determine: the row names no file path
01a0de60-5f66-…  (human)  internal/lineedit/answer.go           last=2026-09-29 since=40
01a0df20-ecf5-…  (agent)  internal/uiclient/audit.go            last=2026-09-28 since=7
01a0e1c9-5713-…  (agent)  devdoc/chat-context-self-knowledge.md last=2026-09-28 since=2
01a0e313-ac65-…  (agent)  internal/uiclient/audit.go,compare.go last=2026-09-28/29 since=7/7
01a0ef8a-ec07-…  (agent)  scripts/materials/box_audit.py        last=2026-09-30 since=1
```
Read it as *"the file was touched, but the touches are `since=` neighbourhood activity"*: the count
is commits in the file since the row's own day, **not** evidence about the row. Only the last column
paired with a commit message that names the defect would be a fix; none of these does.
