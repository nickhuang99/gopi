<!-- doc-class: historical -->
# C7 — the `devdoc/requirements/` corpus, audited as a corpus

Task `01a0b49f` (`[7/8] C7`). Audit of `devdoc/requirements/` — 22 `.md` files plus
`99-summary-gate.sh` and `evidence/` — against three questions: (1) does each file
still describe the system it claims to describe, (2) is each `open`/`pending` module
row still needed, and (3) should the missing `99-summary.md` land.

**Base of record: master `52090dc0`** (`merge verify/branch-lifecycle-ruling-already-landed`),
read in worktree `/home/user/workspace/gopi-c7-req-audit` on branch `docs/c7-req-truth-audit`,
created off that SHA (Rule 6a). All measurements below were taken in this session at that
revision. Docs-only change (Rule 5/7): this file and nothing else.

**Headline.** The corpus is a **three-series artifact** whose two routing documents
are wrong about their own contents. `99-summary.md` **already exists** (510 L, commit
`363c5457`) while `00-module-map.md:342` still routes the reader to it as
`(does not exist yet)`; `README.md:22` states the earlier series "produced nothing"
while 11 sibling documents from that lineage sit in the same directory; and the
series' own acceptance instrument, `99-summary-gate.sh`, **is red on master** — its
`--self-test` mode is doubly broken and never ran once. The 15-row series is
`completed`, not partial: 13 of 15 module documents exist and the two that do not
(M13, M14) were never briefed into the file path their own briefing names.

---

## 1. The corpus inventory, with a verdict per file

**Verdict legend.** `CURRENT` = claims verified to still hold at the base SHA of the
file's own pin. `STALE-ROUTING` = the document's own claims are fine, but it routes
the reader to a file/state that no longer exists. `STALE-CLAIM` = a measured claim in
the file is false today. `SUPERSEDED` = a later document owns the same boundary.
`DUPLICATE` = overlapping boundary from a parallel series, deliberately kept as the
audit record.

| # | file | L | series / row | pinned at | verdict |
|---|---|---|---|---|---|
| 1 | `README.md` | 97 | umbrella `01a0ab12` | `17cec86b` | **STALE-CLAIM + STALE-ROUTING** (F2, F3, F9) |
| 2 | `00-method-and-standards.md` | 930 | umbrella `01a0ab12` | `9ce8e159` | **CURRENT** — the standard the series is judged by; no measured claim found false |
| 3 | `00-module-map.md` | 575 | umbrella `01a0ab12` | `9ce8e159` | **STALE-CLAIM** (F1, F4) — scorecard table false at 6 rows |
| 4 | `01-per-module-briefings.md` | 561 | umbrella `01a0ab12` | `9ce8e159` | **CURRENT-AS-DESIGN, STALE-AS-ROUTE** — `[3/14]`–`[12/14]` delivered under other filenames (F5) |
| 5 | `01-task-ledger.md` | 1193 | B1/12 (`01a0ab3d`) | `17cec86b` | **CURRENT** (42 citations resolve; superseded in scope by same-named doc — see §1a) |
| 6 | `02-merge-and-branches.md` | 1002 | B2/12 | `17cec86b` | **CURRENT, 2 dead citations** (F8) |
| 7 | `03-worktree-and-cwd.md` | 752 | B3/12 | `17cec86b` | **CURRENT**; later doc `worktree-and-cwd.md` is the repair (§1a) |
| 8 | `04-sync-and-nodes.md` | 1080 | B4/12 | `17cec86b` | **CURRENT**; `sync-and-nodes.md` is the `812f9a60` repair |
| 9 | `05-context-and-memory.md` | 924 | B5/12 | `17cec86b` | **CURRENT** — carries its own `[superseded]` markers; repaired in place |
| 10 | `06-agent-loop.md` | 909 | B6/12 | `17cec86b` | **CURRENT** (5 citations; anchored to `812f9a60` in the rebuild) |
| 11 | `07-tools-and-routing.md` | 1492 | B7/12 | `812f9a60` | **CURRENT — NOT AUDITED HERE** (C6 owns tools/build; see §5) |
| 12 | `09-chat-ui.md` | 1327 | B9/12 | `812f9a60` | **CURRENT** — 100 citations, all resolve; the corpus' best-instrumented doc |
| 13 | `09-chat-ui-verification-report.md` | 252 | B9 re-anchor | `812f9a60` | **CURRENT, and load-bearing**: it records a *vacuous green* (`:99`–`:118`) that `99-summary.md` then quotes as the reason the gate needs a self-test — and the gate's own self-test is vacuous too (F1) |
| 14 | `11-build-and-repos.md` | 728 | B11/12 | (Rule 12) | **CURRENT — NOT AUDITED HERE** (C6) |
| 15 | `99-summary.md` | 510 | `[15/15]` | `5cdae25e` | **EXISTS, GREEN at its own base, RED on master** (F1, F6) |
| 16 | `build-and-repos.md` | 683 | B11/14 audit | `17cec86b`/`812f9a60` | **DUPLICATE by design** — explicit audit-and-close-gaps of #14; §9 consistency table |
| 17 | `cli.md` | 805 | `[9/15]`, row `01a0ab12-5b52` | `812f9a60` | **CURRENT** → closes M7 (F4) |
| 18 | `merge-and-branches.md` | 791 | M2/14, row `01a0ab12-3115` | `812f9a60` | **CURRENT** — a *second* M2 doc, self-declared duplicate; the series' own briefings do not list it (F5) |
| 19 | `merge-branch-lifecycle.md` | 836 | `[1/11]` | `17cec86b` | **STALE-CLAIM: 3 dead citations** (F8); boundary absorbed by #6/#18 |
| 20 | `notifications.md` | 878 | M11, host peerbox | `0ac1f70e` | **CURRENT** → closes M11 (F4) |
| 21 | `sync-and-nodes.md` | 808 | B4 repair, row `01a0ab3d-d541` | `812f9a60` | **CURRENT** — its Appendix A audits #8 without overwriting it (the §1a pattern working) |
| 22 | `test-infrastructure.md` | 682 | `[11/15]` | `32932668` | **CURRENT** — 105 citations, 1 dead (F8) → closes M9 |
| 23 | `worktree-and-cwd.md` | 861 | `[12/15]` | `f7bd2616` | **CURRENT** → the `03` repair; the `[12/15]` briefing's boundary, delivered |
| — | `99-summary-gate.sh` | 184 | `[15/15]` instrument | `363c5457` | **BROKEN** (F1) — red on master; `--self-test` can never run |
| — | `evidence/` | 4 files | umbrella evidence index | — | **CURRENT** — `README.md`, `evidence-index.md`, `evidence-human-asks.md`, `evidence-counts.sh`, `extract-human-asks.sh`; consulted by `00-method-and-standards.md` §6 |

### 1a. The three-series structure (why the directory holds 22 docs for 14 modules)

Three series ran over the same corpus, and the corpus is their union:

| series | rows | outcome | documents |
|---|---|---|---|
| **A** — `REQUIREMENTS [0/11]` | `01a0aac3`, `01a0aac4`×2 | all **cancelled** | none (planning only) |
| **B** — `B1/12..B12/12` | `01a0ab3d`/`3e`/`3f` | 11 `completed`, `B12` **`pending`** | `01-task-ledger.md` … `11-build-and-repos.md` + the 5 named-only repairs/duplicates |
| **C** — `[1/15]..[15/15]` | `01a0ab12` | **all 15 `completed`** | `00-*`, `01-per-module-briefings.md`, `cli.md`, `notifications.md`, `test-infrastructure.md`, `worktree-and-cwd.md`, `99-summary.md` |

Series **C** delivered its per-module documents under the *filename the briefing
named*, not the `NN-<module>.md` convention of series B — and the two series'
filenames collide in spirit (both have a `worktree` doc, both have a `merge` doc,
both have a `sync` doc) without colliding on disk. The later document of each pair
**audits the earlier one in an appendix instead of overwriting it**
(`sync-and-nodes.md` §Appendix A, `build-and-repos.md` §9) — the pattern is
deliberate and it is the corpus' best property. The cost is that a reader cannot
tell from the filenames which document is authoritative; only the front matter says.

---

## 2. Per-row verdict for every `open`/`pending` module row

The map's scorecard (`00-module-map.md:321`–`:349`) is the thing to audit. Verdicts:

| module | map says | truth at `52090dc0` | verdict |
|---|---|---|---|
| **M7** CLI surface | `none` — **open** | `cli.md` (805 L, `8ce4c674`, row `01a0ab12-5b52` completed) | **CLOSED — map is stale.** The `[9/15]` row ran. |
| **M9** gates & test infra | `none` — **pending** (`01a0ab3f-7069`) | `test-infrastructure.md` (682 L, `8ba10484`, row `[11/15]`) | **CLOSED — row stale.** B12 `01a0ab3f-7069` should be closed as superseded, not re-run. |
| **M11** notifications | `none` — **open** | `notifications.md` (878 L, `beac38c5`) | **CLOSED — map is stale.** |
| **M13** environment adapters | `none` — **open** | nothing, on any branch (`git log --all -- environment-adapters.md` → empty) | **STILL NEEDED — briefing exists at `01-per-module-briefings.md:457`, deliverable path named at `:495`. Genuinely unwritten.** |
| **M14** cross-cutting | `none` — **open** | nothing, on any branch | **STILL NEEDED — briefing at `:499`, deliverable `cross-cutting.md` named at `:535`. Genuinely unwritten.** |
| **M15** summary | `99-summary.md (does not exist yet)` — pending | exists, 510 L, committed `363c5457` | **CLOSED — the central stale claim (F1/F3).** |
| **B12** verification & gates | `pending` (row `01a0ab3f-7069`) | delivered as `test-infrastructure.md` | **CLOSE AS SUPERSEDED.** |
| M1–M6, M8, M10, M12 | completed | confirmed by document + ledger row | **CLOSED** — 9 of 9 verified present with a completed row. |

**So the honest count is 13 of 15, not 9 of 15** — and of the two that remain, only
M13 and M14 are real work. Both are `[13/14]` and `[14/14]` in the briefings file,
which means the *briefings* already exist and are dispatch-ready; what is missing is
the two documents. This is the corpus's own gap, correctly diagnosed by the map and
then mis-reported by the map's own table.

---

## 3. `99-summary.md` — the recommendation, and the experiment behind it

**Recommendation: WRITE NOTHING NEW. `99-summary.md` has landed — keep it; fix the two
documents that route to it as absent, and fix its gate.**

The task's question ("should the missing `99-summary.md` land?") presumes the file is
missing. It is not; it was committed `363c5457` (`[15/15] 99-summary — what gopi is
for, ranked gaps, model disagreements, unknowns, first pins`, 2026-09-18 21:10) and
is on master. The reference the task quotes — `00-module-map.md:342` — is a **stale
cell in a table whose other eight cells are also stale** (`:334`, `:336`, `:338`,
`:340`, `:341`).

**The experiment that decides *how* to treat it** (a replay, because "does the summary
still hold?" and "was it ever good?" are different questions):

```bash
# rebuild the corpus as it stood at the summary's OWN base, plus the summary
git show 5cdae25e:devdoc/requirements/<each of the 19 files>   # → a scratch dir
git show 363c5457:devdoc/requirements/99-summary.md           # ditto
bash <scratch>/99-summary-gate.sh <scratch>/99-summary.md      # run from the REPO ROOT
```

- **At its own base `5cdae25e`: `GATE GREEN` — 10/10 checks pass, exit 0.** The
  summary was a genuine, verified closing document.
- **On master `52090dc0`: `GATE RED` — 1 check fails, exit 1**, naming
  `build-and-repos.md notifications.md worktree-and-cwd.md` as *module docs not cited
  by path*. All three arrived on master **after** the summary's base (measured:
  `beac38c5`, `c8732ecd`, `0df34680` are each *not* ancestors of `5cdae25e`), so the
  summary could not have cited them. The failure is entirely downstream of the summary.

An earlier replay from inside the scratch dir reported `110 of 110 do not resolve` —
a **false alarm from my own harness**, not a finding: the citation-resolution check is
CWD-relative, so it must be run from the repo root. Recorded here so the next auditor
does not mis-file it.

**Therefore: the summary is accurate-as-of-its-base and its remaining rot is
mechanical (3 uncited files). Do not rewrite it; the repair is two one-line
corrections plus a gate fix (§4).**

---

## 4. Findings, ranked

**F1 — `99-summary-gate.sh` is red on master, and its `--self-test` mode is doubly
broken so nobody ever saw it. (severity: highest — the corpus's only acceptance
instrument is dead and silent.)**
- `bash devdoc/requirements/99-summary-gate.sh devdoc/requirements/99-summary.md`
  → `GATE RED — 1 failing check(s)`, **exit 1** (`FAIL module docs not cited by path:
  build-and-repos.md notifications.md worktree-and-cwd.md`).
- `bash devdoc/requirements/99-summary-gate.sh --self-test` → **exit 1**, and it never
  reaches the self-test block. Two independent defects:
  1. **Argument form.** The documented invocation calls for a *positional* `--self-test`
     (`#   bash … [doc] [--self-test]`, `:4`), but the script reads `DOC=${1:-…}`,
     `SELF=${2:-}` (`:16`–`:17`). `SELF` stays empty and `DOC="--self-test"`, so
     `DIR=$(dirname "$DOC")` fails, `grep -q 'requirements/*.md' --self-test` errors,
     and the whole run measures a nonexistent document — hence the nonsensical
     `FAIL constraints absent from the summary: C1 C2 C3 C4 C5 C6`, when the summary
     *does* carry all six (`:437`–`:448`) and the real run passes that check.
     (The block itself is correct — it calls `bash "$0" "$t"`; only the entry parse is wrong.)
  2. **Rule 22 violation in the self-test's scratch dir.** `sd=$(mktemp -d)` (`:159`)
     resolves through `TMPDIR`. On this host `TMPDIR=/dev/shm/gopi-test-scratch/tmp`
     — device `28`; `/` is device `2051`. Measured, so it is clean *right now*, but the
     script does not measure it: the criterion in Rule 22 is what must be checked,
     not the value that happens to hold. Under the default environment (`TMPDIR` unset
     → `/tmp` → device `2051`, same device as `/`) the same line writes to `/`.
- **The irony is the finding's sharpest edge**: `99-summary.md:20` cites
  `09-chat-ui-verification-report.md:99`–`:118` — *"a green that cannot fail measures
  nothing"* — as the reason the gate needs a self-test, and the gate's self-test has
  never executed once. The instrument was written to prevent exactly this defect and
  suffers it.
- **Recommendation:** fix the entry parse (`SELF=${1:-}` when `DOC` is absent, or
  `case "$1" in --self-test) …`), take the scratch dir from `internal/scratch`'s rule
  (measure device id against `/`), and re-run both modes; then update the coverage
  expectation to `>=19` so the next orphan is reported instead of failing the gate.

**F2 — `README.md:22`–`:27` claims the earlier series "produced nothing". Its own
`git log` contradicts it. (severity: high — a reader believes a whole lineage vanished.)**
The edit at `be4a2326` repairs exactly this claim **inside `00-module-map.md`** (`:56`:
*"This entry's original text is now wrong and is corrected here rather than deleted"*)
but never propagated the correction to the README, where the identical sentence still
stands. Measured: `git log --all --oneline --diff-filter=A -- 'devdoc/requirements/*'`
returns 13 commits, not empty. The README is the corpus' entry point, so this is the
first thing a reader is told, and it is false.

**F3 — `00-module-map.md:342` routes the reader to `99-summary.md (does not exist yet)`,
and `:344` says "9 of 15 have a document". (severity: high — the map's own scorecard is
its least accurate section.)**
Measured: the file exists (510 L, `363c5457`); 13 of 15 boundaries have a document;
`open` appears for M7/M11/M13/M14 and `pending` for M9/M15 while four of those six are
delivered. The map is otherwise the corpus' most carefully re-measured document (its
`.md` table at `:321`–`:342` carries **9 uncorrected line counts**: `01-task-ledger.md`
920→**1193**, `04-sync-and-nodes.md` 1079→**1080**, `05-context-and-memory.md`
523→**924**, `06-agent-loop.md` 581→**909**, `07-tools-and-routing.md` 1116→**1492**,
`09-chat-ui.md` 937→**1327**, `11-build-and-repos.md` 727→**728**,
`02-merge-and-branches.md` 1002 L ✓, `03-worktree-and-cwd.md` 752 L ✓). Note the
asymmetry that caused it: the map re-measures *its own* numbers meticulously and never
re-measured the documents it is a map **of**.

**F4 — M7, M9, M11 are closed by documents the map does not know about. (severity: high.)**
`cli.md` (M7), `test-infrastructure.md` (M9), `notifications.md` (M11) each exist with
a completed ledger row; the map's scorecard lists all three as `none`. Consequence:
the 15-row series can be re-dispatched for work already done — the same failure mode as
F1/F3, and the reason the scorecard is the finding rather than the missing summary.

**F5 — the briefings file names 15 deliverable paths; 4 of them were delivered under a
different filename, and 1 was delivered twice. (severity: medium.)**
`01-per-module-briefings.md` names `context-and-memory.md` (`:200`), `agent-loop.md`
(`:232`), `chat-ui.md` (`:323`), `environment-adapters.md` (`:495`), `cross-cutting.md`
(`:535`). On disk the first three are `05-context-and-memory.md`, `06-agent-loop.md`,
`09-chat-ui.md` (series B's numbering convention), and no `context-and-memory.md` or
`agent-loop.md` or `chat-ui.md` exists — while `merge-and-branches.md:10` self-declares
as *"a duplicate"* of a row that already produced two documents. A reader following the
briefings literally looks for files that do not exist and misses the ones that do.

**F6 — the summary is a snapshot at `5cdae25e` and is now 1 document short + 3 documents
blind. (severity: medium — bounded, mechanical, verified by replay.)**
Coverage: 19 cited vs 22 on disk today. The three it cannot cite are the three that
landed after its base (`beac38c5`, `c8732ecd`, `0df34680`). Its other 9 checks still
pass on master, including `110 distinct citations resolve`.

**F7 — the corpus cites file paths that no longer exist. (severity: medium, sharply
bounded: 6 dead citations over 815 distinct file:line citations = 0.74%, and the other
three checks below pass.)**
| path cited | cited in | status |
|---|---|---|
| `internal/tools/repo_sweep_park_test.go` | `02-merge-and-branches.md:255,284`; `merge-branch-lifecycle.md:255` | **file does not exist** |
| `cmd/agent/cli_merger_daemon.go` | `merge-branch-lifecycle.md:3` | **file does not exist** |
| `cmd/agent/cli_merger_stop.go` | `merge-branch-lifecycle.md:12` | **file does not exist** |
| `cmd/agent/merger_restart_e2e_test.go:677` | `test-infrastructure.md` | file exists, **line out of range** |
None of the three files exists **anywhere** in today's tree (`git ls-files '*/<name>'` → empty
for each), and near-name successors are visible for two of them
(`internal/tools/repo_sweep_test.go`, `repo_sweep_notff_test.go`;
`cmd/agent/merger_stop_arm_order_gate_test.go`) — i.e. a rename/move the
`17cec86b`-pinned documents never learned about.
**Correction to my own first draft:** I had written "2 stale counts in `README.md:96`".
That claim was wrong and I could not observe it — `README.md:96` is the honesty note,
not a count. The README's stale counts are the block at `:15`–`:21` (F9 below); the one
other forward claim in the README that has moved is `:59` ("the two tools Rule 11 was
written about are no longer the problem"): measured in the 300-newest-log sample,
`git_blame` = **5** and `code_search` = **225**, against `bash` = **21,298** — "no
longer zero" is measured; "no longer the problem" is a judgement the numbers do not
carry. Left as a judgement, attributed.

Note what did *not*
happen: 809 of 815 citations still resolve (99.3%), and the three most heavily cited
documents (99-summary 110, test-infrastructure 105, chat-ui 100) are at 0/0/0 dead.
The dead citations concentrate in the two oldest and least-cited documents
(`merge-branch-lifecycle.md`: 3 of 6!). **That is the corpus-level result: the series
aged well; its routing aged badly.**

**F8 — M13 and M14 are genuinely unwritten, and their briefings are ready. (severity:
medium — this is the corpus' only real remaining work.)**
`git log --all -- devdoc/requirements/environment-adapters.md devdoc/requirements/cross-cutting.md`
→ empty: no branch ever carried them. Both briefings exist (`:457`, `:499`) with their
deliverable paths. The 14-package coverage proof in the map (§2) assigns every one of
today's **45** `internal/` packages, so M13's `proxy`/`doubaoasr`/`shot`/`text2img`/
`textguard` and M14's `config`/`clicontract`/`daemonlifetime`/`sessionevent`/
`sessionlayout`/`runcmd`/`procname` are *assigned* but *undocumented*.

**F9 — `README.md`'s headline numbers are all pre-`17cec86b`-era, and one of its tool
adoption claims has moved again. (severity: low — the README pins its SHA openly, which
is the honest half of the defect.)**
README says "Measured on master `17cec86b`"; re-measured at the base SHA:

| claim (README) | `17cec86b` | `52090dc0` | Δ |
|---|---|---|---|
| production Go lines | 143,015 | **168,125** | +17.6% |
| test Go lines | 179,917 | **213,352** | +18.6% |
| `internal/` packages | 40 | **45** | +5 |
| `cmd/agent/cli_*.go` | 47 | **45** | −2 |
| `devdoc/` files | 932 | **1,089** | +157 |
| `GOPI.md` lines / rules | 1,085 / 29 | **1,187 / 30** | +102 / +1 |
| ledger rows (human/auto/llm) | 1,607 (557/686/348) | **2,036 (652/867/517)** | +429 |
| distilled memory rows | 3,251 | **4,050 (+65 procedural)** | +799 |
| session directories | 73,559 | **74,608** | +1,049 |
| tool-adoption sample | git_blame 0, code_search 0.3% | **git_blame 5, code_search 225** (300 newest logs: bash 21,298 / job 3,772 / task 2,924 / read 2,622 / edit 2,603 / memory 1,676 / … / vision_analyze **2** / screen_capture **7**; **web_search, web_fetch, text2img still 0**) | — |

The README states its numbers as a snapshot and its sample as a sample, so this is rot,
not dishonesty — but it is what a reader will quote. Note the README's own finding #3
was *right and is now wrong in a new way*: the zero-adoption tier moved from
`git_blame`/`code_search` (now 5 and 225) to `web_search`/`web_fetch`/`text2img` plus a
near-zero `vision_analyze` (2) — exactly the "living check" the README said Rule 11's
acceptance standard needed.

**F10 — the corpus auto-loads its own audit question set. (severity: low, but the
recursion is worth recording.)**
`01-task-ledger.md` leads with `<!-- doc-class: historical -->`; the map cites
`09-chat-ui-verification-report.md`; both are read by later C-series tasks as
*instructions*, and `00-method-and-standards.md` §4.2 is the text workers are told to
paste whole. A defect in this corpus propagates into every future corpus task —
which is why the two routing errors (F2, F3) matter more than their size suggests and
why they must be fixed in the corpus, not in a report like this one.

---

## 5. What I deliberately did not audit (and why)

This task's scope is the corpus **as a corpus** — routing, currency, and the open
rows. Three adjacent areas belong to other work and are left alone, named here so the
next reader does not mistake silence for approval:

- **The tools and build documents** (`07-tools-and-routing.md`, `11-build-and-repos.md`,
  `build-and-repos.md`) — sibling task **C6** owns them (`feat/c6-tools-build-doc-audit`
  @ `d9df443d`). Their citations and claims were counted for the corpus-level tables
  above (all resolve; `11`'s 32/0, `build-and-repos.md` 19/0, `07` 65/0) but not judged.
- **The module documents' *content*** — whether each reconstructs the right need is the
  per-module audit's job, done when each was written (each carries its own §6 gap
  analysis and §8 confidence register). I verified their currency (citations, pins,
  filenames), not their judgement.
- **`README.md`'s three "here's what the umbrella contributes" findings** — finding #2
  (`internal/testutil` is not a production dependency) is the kind of claim that needs
  a live re-run to retire, and it is outside this task's acceptance. Flagged, not judged.

---

## 6. What I could not determine

1. **Why series A was cancelled.** Still unrecorded on any branch. The session logs
   are the cheapest route (`ls ~/.gopi/sessions/01a0aac3* ~/.gopi/sessions/01a0aac4*`);
   I did not read 3 sessions' worth of jsonl for a question this task does not ask.
   The map (`:74`) and the README (`:70`) both flag it as the unknown the summary should
   have carried — and the summary does carry it (§5, consolidated unknowns).
2. **Whether M13/M14 should be dispatched, or the boundaries redrawn.** Both are
   `[13/14]`/`[14/14]` in the briefings and both are genuinely unwritten — but the map's
   own §4 says the *number* of modules is `[INFERENCE]` and "an operator may merge two
   of these in his head (most plausibly M14 with M10, or M13 with M6)". Whether to write
   two more documents or to fold M14 into M10 and M13 into M6 is an operator call, not
   an audit finding. **I did not submit anything** (the acceptance forbids it).
3. **Whether the 4 dead paths in F7 were renamed or the documents were wrong when
   written.** Settled in the direction of *renamed*: none of the three missing
   filenames exists anywhere in the tree (no branch carries them), and near-name
   successors exist (`internal/tools/repo_sweep_test.go`,
   `cmd/agent/merger_stop_arm_order_gate_test.go`). Identifying the exact rename
   commit would not change the verdict (`CURRENT` with 3 dead citations) or the fix
   (re-point or strike).
4. **How many of the corpus' [inferred]-marked resolutions are still the right call.**
   Counting them is mechanical; judging them is the operator's, and the map's §4 lists
   the four it left open on purpose.

---

## 7. Verification (self-filed, per Rule 5)

- **Branch/worktree:** `docs/c7-req-truth-audit` at `/home/user/workspace/gopi-c7-req-audit`,
  created from master `52090dc0` (Rule 6a; no sibling's branch used — Rule 23).
- **Files changed:** this file only (`git diff --name-only 52090dc0..HEAD`).

### Evidence — one measured line per gate claim

| claim | command | observed |
|---|---|---|
| gate red on master | `bash devdoc/requirements/99-summary-gate.sh devdoc/requirements/99-summary.md` | `FAIL module docs not cited by path: build-and-repos.md notifications.md worktree-and-cwd.md`; `GATE RED — 1 failing check(s)`; **exit 1** |
| gate `--self-test` broken | `bash devdoc/requirements/99-summary-gate.sh --self-test` | `grep: unrecognized option '--self-test'`; `FAIL constraints absent from the summary: C1 C2 C3 C4 C5 C6`; `GATE RED — 9 failing check(s)`; **exit 1** |
| argument-parse root cause | `bash -x devdoc/requirements/99-summary-gate.sh --self-test` | `+ DOC=--self-test`, `+ SELF=` before `dirname --self-test` errors |
| summary green at its own base | replay at `5cdae25e` + `363c5457`, run from repo root | `GATE GREEN — …`; **exit 0**; `ok citation coverage: 19 module docs cited by path` |
| 3 uncited docs postdate the base | `git merge-base --is-ancestor <c> 5cdae25e` | `beac38c5`/`c8732ecd`/`0df34680` → NOT ancestors |
| summary exists | `git log -1 -- devdoc/requirements/99-summary.md` | `363c5457` · 510 L · 42,760 bytes |
| earlier series did not produce nothing | `git log --all --oneline --diff-filter=A -- 'devdoc/requirements/*'` | 13 commits, first `12e1ac74` |
| 13 of 15 boundaries documented | ledger + `ls` | `sqlite3 …tasks where summary like '%REQUIREMENT%'` → 15 rows `01a0ab12`, status `completed`; M13/M14 paths absent from `git log --all` |
| dead citations | corpus-wide `file:line` extraction (815 distinct) vs `test -f` + `wc -l` | 6 dead: `repo_sweep_park_test.go` ×3, `cli_merger_daemon.go`, `cli_merger_stop.go`, `merger_restart_e2e_test.go:677`; 809 resolve |
| headline numbers | one `find`/`ls`/`wc`/`sqlite3` batch at `52090dc0` | 168,125 / 213,352 / 45 / 45 / 1,089 / 1,187 / 30 / 2,036 / 4,050 / 74,608 |
| adoption sample | `ls -t ~/.gopi/sessions/*/*.jsonl \| head -300 \| xargs cat \| grep -oE '"name": *"[a-z_]+"' \| sort \| uniq -c` | bash 21,298 … code_search 225, git_blame 5, vision_analyze 2, screen_capture 7; web_search/web_fetch/text2img absent |
| Rule 22 measurement | `stat -c %d /`; `stat -c %d /dev/shm`; `echo $TMPDIR` | `/`=2051, `/dev/shm`=28, `TMPDIR=/dev/shm/gopi-test-scratch/tmp` (so `mktemp -d` at gate `:159` is currently off `/` but unmeasured by the script) |
| worktree clean | `git status --porcelain` | empty (Rule 30) |
| nothing submitted | — | no `task_submit` call made in this session |
| Rule 27 | — | no device load generated; every measurement above is a read, a grep, or a git query |
