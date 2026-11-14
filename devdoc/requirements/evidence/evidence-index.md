<!-- doc-class: historical -->
# Evidence index — where a gopi requirement can be cited from

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**What this is.** The companion to `evidence-human-asks.md` (class **E1**). Every
document in the reverse-requirement series must trace each "the operator needs…"
to a citation (background: `devdoc/requirements/00-method-and-standards.md` §6.3,
§8). This file is the *catalogue* of the citation kinds that actually exist in
this repository, with the greppable handle for each, and an honest statement of
what is **not** recorded anywhere.

**How to use it.** Find the class, take the handle, quote the artifact. A claim
with no handle from E1–E5 is `[opinion]`; an inference is `[inferred]` **and
names its weakest link** (§8 honesty rules — the series' own standard).

**Every number below is measured, not claimed.** Counters:
`bash devdoc/requirements/evidence/evidence-counts.sh` (re-runnable, read-only).
A stale number is a visible diff, not a silent error. Measured on the snapshot
described in §Re-run below.

| class | artifact | size on this snapshot | citation handle |
|---|---|---|---|
| **E1** | `source=human` ledger rows | **608** rows | `E1 task <task_id>` |
| **E2** | `GOPI.md` intent-stating rules | 24 rules + 6 gates, 1187 lines | `GOPI.md:<line> (Rule <n>)` |
| **E3** | devdoc documents that state a design decision | 1029 `.md` on master at snapshot `9ce8e159` (1033 on master `812f9a60`); 595 carry a rationale phrase; 9 name-matched | `devdoc/<file>.md` |
| **E4** | commit messages that explain a why | 753 / 4597 commits match the rationale grep (`756` / `4607` on master `812f9a60`); 5 quote the operator | `<sha>` |
| **E5** | intent-stating comments in production Go | 57 lines / 40 files | `path:line` |
| — | **not recorded anywhere** | see §"What is NOT recorded" | — |

---

## E1 — the operator's own asks (primary source)

**File**: `devdoc/requirements/evidence/evidence-human-asks.md` — every
`source=human` row, verbatim (summary + payload/spec).
**Row count**: 608. **Count handle** (never count by eye):
`grep -c '^<!--row: ' devdoc/requirements/evidence/evidence-human-asks.md`

**Cite as** `E1 task <task_id>` — e.g. `E1 task 01a0aac3-fcca-7272-a423-f744dcf74d69`.
The task id is unique and stable, and joins to `task_events`, tags and
`parent_task_id`.

**Sub-sets** (queries copied from the method doc §6.1, which also explains why
payload-length alone loses the one-line briefs):

| set | selection | rows |
|---|---|---|
| A — substantive payloads | `length(payload) > 200` | 556 |
| B — one-line briefs (the *final* decisions) | non-telemetry, `length(payload)<=200 OR payload=''` | 48 |
| C — verbatim operator statements | the `nick 原话/终审/拍板/…` marker family | 29 |
| D — vocabulary search | `tasks_fts MATCH '<module keywords>'` (fts5 live) | — |

**Both id shapes exist in one table** — `t-<unix>` (284 rows, before
2026-09-09) and `<uuid>` (324 rows, after). A grep for one silently misses the
other; `evidence-human-asks.md` carries both.

### The E1 caveat that changes how this class may be used (measured)

E1 is the series' **primary source**, and it is **contaminated by construction** —
`source='human'` is a *stamp*, not an observation of authorship:

| evidence that an agent, not the operator, wrote the row | rows |
|---|---|
| `session_id` AND `model` both set (an LLM session was the submitter) | **314** |
| of those, `model like 'deepseek-flash%'` — the exact signature named in `e10d9958` | **174** |
| `session_id` set but `model` empty | 31 |
| neither set (attributable to a human hand) | 214 |

The mechanism is recorded in commit **`e10d9958`** (`fix(task-source): derive the
submit source — human is reserved`): the one writer of the CLI submit row defaulted
`if source == "" { source = "human" }`, so *every* invocation that did not pass
`--source` — wherever it came from — was stamped human. The commit measured 250
such rows then and derived the source instead of assuming it.

**That fix is NOT on master** (it lives on branch `fix/task-source-worker-derivation`;
`git ls-tree master -- internal/tasksource` → empty). So for the whole E1 corpus
in this snapshot:

> **608 rows are stamped `source=human`; at most 214 are attributable to a human
> hand, and 174 carry the `deepseek-flash` signature of an agent-submitted row.**
> A row is *evidence of an ask* only after its text is read; the stamp alone is not
> evidence of authorship. Prefer rows that also carry `nick 原话/终审/拍板/裁决`
> (set C, 29 rows) when an operator quote is what the claim needs.

---

## E2 — `GOPI.md`: rules that encode a decided need

`GOPI.md` is 1187 lines: **24** `## Rule N:` headings plus **6** `### Rule N —`
gates (Rules 16–21). A rule is a *decided constraint* — the operator (or an agent
that measured a failure and was ratified) settled a question here, which is why
the method doc §6.2 lists it as secondary source #2.

**Handle**: `GOPI.md:<line> (Rule <n>)` — line numbers are from this snapshot.

### E2a — rules that encode a *user-visible product* need

| rule | line | the need it encodes (short) |
|---|---|---|
| Rule 3 — tools are minimal | `GOPI.md:85` | the operator controls the tool surface; 17 tools, build-enforced by `TestGOPIRule3_MatchesClaudeToolDefs` |
| Rule 5 — build+test+commit+**document** | `GOPI.md:114` | no task is complete without a written deliverable; the report's `## Evidence (self-filed)` is the only window a future agent has |
| Rule 6 — isolated work dir + push semantics | `GOPI.md:159` | parallel work must not collide; a deliverable must never be lost in a deleted clone |
| Rule 7 — devdoc everything worth mentioning | `GOPI.md:260` | future agents have no memory of this session — devdocs are their only window |
| Rule 10 — case-study habits | `GOPI.md:291` | constraint dual-path, pixel-level truth for renders, root-cause before params, minimal-sample separation, recipes to memory, dependency preflight (ZERO-DEP default) |
| Rule 11 — LLM adoption proof for tool work | `GOPI.md:318` | a tool's value = the LLM actually calls it; written after an audit found git_blame 0/21435 and code_search 0.3% |
| Rule 12 — a repo declares its build | `GOPI.md:331` | the machine must run the NEW binary after a merge; an empty build = a pure merge target, never a guess |
| Rule 15 — no silent failure | `GOPI.md:453` | `omitempty` dropping a required field and swallowed errors are both ruled defects |
| Rule 22 — temp never on `/` | `GOPI.md:717` | the 2026-09-11 exhaustion (19G→1.5G free); stated as a *criterion* (measure device ids), never a path to copy |
| Rule 28 — two hosts, two jobs | `GOPI.md:1035` | peerbox owns the system layer, HP owns features — 14 branches on a stale baseline was the cost of not dividing |
| Rule 29 — main repo hosts work in progress | `GOPI.md:1078` | one task, one worktree, one branch; a foreign worktree makes the ownership record lie |
| Rule 30 — a run leaves its worktree clean | `GOPI.md:1120` | untracked litter vs an uncommitted deliverable are OPPOSITE defects; the `completed` writeback is refused on dirty tracked work |

### E2b — rules that encode *how work is done* (still a need: the operator's cost model)

| rule | line | the need it encodes |
|---|---|---|
| Rule 24 — cgroup is the only evidence of detachment | `GOPI.md:796` | two machine-wide kill incidents; PPID/setsid are not evidence (`**Why**` at `:820`) |
| Rule 25 — daemons run unprivileged | `GOPI.md:825` | a privilege is never the remedy for a cgroup escape; the operator's principle is quoted at `:847` — `**Why**` at `:872` |
| Rule 26 — an incomplete fix is repaired by dispatching again | `GOPI.md:877` | **operator verbatim** at `:901`: *"if it repairs incompletely, dispatch a new one to cover it — even duplication is fine…"* |
| Rule 23 — never continue someone else's failed branch | `GOPI.md:777` | a failed path is evidence kept on purpose (`**Why**` at `:789`) |
| Rule 27 — never generate device load | `GOPI.md:906` | three incidents, same excuse; heavy tests are classified and off the merge path |
| Rule 1 / 2 / 13 / 14 | `:6` `:69` `:365` `:410` | one source of truth for a value; a constant exists to be *referenced*; shared values live in a small exported-constant package |
| Rules 16–21 (gates) | `:538`–`:668` | prose did not hold — the discipline is turned into six machine checks that ratchet: `%w` wrapping, `t.Setenv`, no `else` after return, no stray `panic`, no accessor prefixes, every env constant has a real reader |

**Recorded-intent gap (measured)**: only **5** rules carry an explicit
`**Why**` block (`GOPI.md:789, 820, 872, 899, 1099`). The other 25 state a rule
with no recorded motivation — an author mining E2 for a *need* must reconstruct it
from the rule text plus the incident it cites. Report that as a gap, never invent
the motivation.

---

## E3 — devdocs that state a design decision

1029 `.md` under `devdoc/` on master at the snapshot commit `9ce8e159` (1033 on
master `812f9a60` — the tree moves, so re-run `evidence-counts.sh`); 989 in this
worktree; 595 carry a rationale
phrase (`because|so that|instead of|root cause`); **9** are name-matched as
decisions. **Handle**: `devdoc/<file>.md` (quote the heading, which carries the
verdict and often the date).

| devdoc | the decision it records |
|---|---|
| `devdoc/design-decision-process-group-20260903.md` | process-group isolation **DEFERRED** — a decided *non*-requirement |
| `devdoc/tmpdir-default-and-workspace-decision.md` | the temp-root default and the parameter's three channels |
| `devdoc/detach-judgment-rule.md` | process independence judged by cgroup (Rule 24's source) |
| `devdoc/no-privilege-daemon.md` | "no privileged solution for daemon running" — the measured table that closed the question |
| `devdoc/why-worktree-residue.md` | source-level diagnosis of why worktrees are left behind |
| `devdoc/branch-lifecycle-ruling.md` | `conflict/` is void; what to do when a branch CANNOT be deleted |
| `devdoc/heavy-tests-rule27-gate.md` | heavy-test classification + the rule-27 gate's three incidents |
| `devdoc/standards-gate.md` | how Rules 16–21 became six lock-in checks (`internal/standards`) |
| `devdoc/branch-scoped-smoke-gate.md` | smoke is ONE branch-scoped command; the whole-tree prescription retired |
| `devdoc/task-source-provenance` → `devdoc/run-method-provenance.md`, `devdoc/distilled-provenance-governance.md` | where a row's provenance comes from (E1's contamination caveat) |
| `devdoc/hp-master-merge-decision.md`, `devdoc/hosta-unreachable-decision-20260913.md`, `devdoc/terminal-conflict-ruling.md`, `devdoc/bug053-m416-human-ruling.md` | closed rulings: what was decided and on what measurement |
| `devdoc/signal-provenance-20260913.md`, `devdoc/worker-cohort-kill-20260912.md` | the two kill incidents that produced Rules 24/25 |
| `devdoc/roadmap.md` | **the founding document** (2026-07-31): the goal and the five design constraints (`## 设计约束`) — the earliest requirement text in the repo |
| `devdoc/GOLDEN-RULES.md` | quotable judgements distilled from rulings |
| `briefings/*.md` (154 files) | the operator's own dispatch text; 19 carry a verbatim operator marker |

Discovery greps (both work, either handle is fine):
`ls devdoc/ | grep -icE 'decision|ruling|why'` → 9 ·
`grep -lriE 'because|so that|instead of|root cause' devdoc/*.md | wc -l` → 595.

---

## E4 — commit messages that explain a *why* rather than a *what*

753 of 4597 commits (`756` of `4607` reachable on master `812f9a60`) match the
rationale grep; the **body** is where the intent
lives (the subject alone is a *what*). **Handle**: the short SHA.

Grep: `git log --all -i -E --grep='because|so that|instead of|to avoid|root cause|rather than|the reason|the real' --format='%h %s'`

| commit | why it is cited |
|---|---|
| `e10d9958` | `fix(task-source): derive the submit source — human is reserved` — the E1 contamination mechanism, with its own measurement |
| `5204f653` | the smoke gate: one branch-scoped command; the whole-tree prescription retired (Rule 27's merge path) |
| `8bb18ecc` | `the first-open refusal is a fact about the FILE, not a wall-clock guess` — a failure signature + root cause |
| `974523d4` | `--status` must not count its own take-over chain as a peer — a self-reference defect |
| `79d79119` | measure checkout occupancy instead of inferring it |
| `6f988037` | a gate whose footprint did not match its own description (Rule 13's gate, measured) |
| `524156af` | **quotes the operator**: *"just for a few ms performance? no, no no! don't use tmpfs. change settings to workspace. now"* |
| `b790c046` | **quotes the operator**: *"no gopi is allowed to run heavy duty testing"* → the escape var must not be inheritable |
| `273d0de0` | two live-worktree-removal incidents; the removal was mechanically unattributable |
| `f427db72` | `refuse agent-spawned gopi worker / gopi task run (operator ruling 2026-09-12)` |
| `d5736e9a` | `init: GoPi project structure + devdoc roadmap` — the project's first commit |

5 commits quote the operator verbatim
(`git log --all -i -E --grep='operator ruling|operator.s rule|nick .*终审|原话'`).

---

## E5 — intent-stating comments in production Go (bonus class)

The method doc §6.2 lists this last; it is the *only* evidence class that sits
next to the code it explains. 57 lines / 40 production files in `internal/`+`cmd/`.

Grep: `grep -rn --include=*.go '^[[:space:]]*// \(Why\|Because\|So that\|Instead of\|Deliberately\|NOTE:\)' internal cmd | grep -v _test.go`

| handle | what it records |
|---|---|
| `internal/tools/gitblame.go:18` | why a dedicated tool instead of a bash subcommand (design doc §判断) |
| `internal/sessionevent/events.go:5` | why a leaf package and not a const in the owning package (Rule 14) |
| `internal/thinkmode/thinkmode.go:4` | why a leaf package (Rule 14) |
| `internal/tools/task_verify_closure.go:107,164` | why the epoch and not a backfill sweep; why a prefix rule and not a format parser |
| `internal/tools/task_reclaim.go:791` | why a second scan (the arm above is keyed on TERMINAL rows) |
| `internal/tools/task_deliverable_branch.go:27,56` | deliberately ENV-GATED; the session-scan fast path |
| `internal/tools/memory.go:1434` | deliberately small (max term) |
| `internal/tools/memory_provenance_latent.go:204` | "Why is the measured justification, echoed in the report" |
| `internal/tools/task_finish_cleanup.go:2674` | why a separate disposition |
| `internal/tools/merger_schedule.go:172` | why the schedule is unknown — one word per measured cause |

---

## What is NOT recorded — requirement evidence that exists nowhere

Counts from `evidence-counts.sh` on the E1 snapshot (608 human rows). Each row is
a **category of need that no artifact can be cited for**; they are the series'
honest scoreboard, not defects to be silently rounded off.

| # | category | measured | why it is unrecoverable |
|---|---|---|---|
| 1 | **Authorship of E1 rows** | 314 rows carry an agent session+model; 174 the `deepseek-flash` signature; only 214 attributable to a human | the stamp was defaulted, not observed; the fix (`e10d9958`) is not on master |
| 2 | **Why a cancelled row was cancelled** | 21 cancelled rows with an EMPTY `reason` | the ledger has the field, nothing wrote it |
| 3 | **Why a failed row failed** | 9 failed rows with an EMPTY `reason` | same |
| 4 | **What was delivered** | 81 rows with an EMPTY `deliverable` | no branch/doc pointer survives |
| 5 | **How completion was verified** | 576 rows with an EMPTY `verify_evidence` | the verification cannot be re-checked later |
| 6 | **Any lifecycle trace at all** | 201 human rows have ZERO `task_events` rows | 1303 events exist for 608 rows; of those, 427 carry an empty `detail` |
| 7 | **The pre-ledger requirement history** | ledger starts 2026-08-29; 518 commits predate it, `d5736e9a` is 2026-07-31 | needs from the first month exist only as commits/devdocs, if at all |
| 8 | **Duplicate asks are not linked** | 12 groups of rows sharing one summary text (17 extra rows) | no parent/duplicate edge is written; the same ask is filed twice and neither row says so |
| 9 | **Requirement-vs-telemetry in one table** | 70 rows are harness telemetry (`summary LIKE 'gopi run%'`) | no separate table or flag; the class must be subtracted by hand |
| 10 | **Where a need came from** | no row records "which conversation/incident produced this ask" | `session_id` names a session dir, but 74250 session dirs exist against 104 rows in the `chats` table — the transcripts are unindexed |
| 11 | **Why the 2026-09-16 requirements series died** | 3 rows, all `cancelled`, reason = `migrated to HP: …` | the migration is recorded; the *reason for the migration* is not |
| 12 | **A need that never became a task** | unmeasurable by construction | the ledger can only count asks that were filed — the denominator is unknown |
| 13 | **Motivation for 25 of 30 GOPI.md rules** | 5 rules carry a `**Why**` block | the rule text survives, its reason does not |
| 14 | **Operator intent behind HP/peerbox splits** | recorded only in dispatcher reasoning, not in the ledger | Rule 28 states the division; the per-task decision trail is not queryable |

**The honest summary line for every module document in this series**:

> Of the requirement evidence this project kept, E1 (608 rows, ≤214
> human-attributable) is the only class that records a need *in the asker's own
> words*. Everything else — rules, devdocs, commits, comments — records a
> **decision made in response to** a need, so it can prove a requirement existed
> but not how it was phrased. And the categories above (6 through 14) are needs
> for which no artifact exists at all; a module document may only name them as
> gaps, never as requirements.

---

## Re-run / provenance

| what | command |
|---|---|
| regenerate E1 (byte-for-byte on an unchanged ledger) | `bash devdoc/requirements/evidence/extract-human-asks.sh` |
| re-measure every count in this file | `bash devdoc/requirements/evidence/evidence-counts.sh` |
| count rows in E1 | `grep -c '^<!--row: ' devdoc/requirements/evidence/evidence-human-asks.md` |
| the acceptance identity | `sqlite3 ~/.gopi/memory.db "select count(*) from tasks where source='human'"` |

- **Ledger**: `~/.gopi/memory.db`, table `tasks`, opened **read-only**
  (`file:…?mode=ro`) by both scripts. This work never writes to it (Rule 22-safe:
  no temp files, no device load).
- **Snapshot boundary**: `max(created_at)` = **2026-09-18 04:42:51** (epoch
  1789706571.0); first E1 row 2026-08-29 14:09:46.
- **What a re-run may differ in**: (a) rows added after that boundary — plus,
  when such rows arrive, the `Row count` / `Snapshot boundary` lines of
  `evidence-human-asks.md` and the fingerprint counts derived from them; nothing
  else shifts there (no wall-clock value is emitted, so an unchanged ledger
  reproduces the file byte-for-byte); (b) the **E2/E3/E4/E5 counts and the
  "NOT recorded" table are snapshot measurements of a moving tree** — `GOPI.md`
  gains rules, `devdoc/` gains files and master gains commits between runs, so
  `evidence-counts.sh` on a later master legitimately prints slightly different
  numbers (measured: E3 `devdoc/*.md` on master 1029 → 1033, E4 commits
  4597 → 4607 / rationale hits 753 → 756, `GOPI.md` unchanged at 1187 lines).
  Treat those as *the count on the day it was taken*, and re-run the script
  rather than quoting this file after a merge; the **E1 identity** (row count ==
  `select count(*) … where source='human'`) and every handle must still hold.
- **Repo state of this measurement**: worktree `feat/req-evidence-index` off
  master `9ce8e159`; the six-line counts above were taken in that tree.
- **Series contract**: no tests written, no production code changed (method doc §8
  rule 7). This directory contains documents and two read-only scripts.
