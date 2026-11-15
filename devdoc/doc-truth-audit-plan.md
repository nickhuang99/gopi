<!-- doc-class: roadmap -->
# devdoc Truth Audit — method, taxonomy, and the partition

Status: **plan + method** (this branch adds this file only; no production code changed).
Base: master `5cdae25e`.

## 1. The problem, measured

The operator's complaint is real and the numbers say why it will not go away by
reading harder:

- `devdoc/` holds **1 010** top-level `.md` files (≈40 MB) plus 19 subdirectories
  (`assets/` 420, `evidence/` 134, `doubao-asr-eval/` 136, `requirements/` 20).
  `git log --all --diff-filter=A -- 'devdoc/*'` counts ≈**1 972** distinct paths
  ever added.
- Churn: by last-touch commit, **1 248** docs moved in the last 7 days, **689**
  in 7–30 days, **31** in 30–90 days. A doc is written by the run that changed the
  code, then frozen at that commit — the code keeps moving, the doc does not.
- **Nothing checks any of it.** The only doc-anchored gates in the tree are
  `internal/agent/docanchor_test.go` (hippocampus anchor resolution) and the
  screen-capture anchor test — both narrow, neither about devdoc truth.

So a devdoc's sentence is true **at the commit that wrote it** and unverified
afterwards. With ~1 000 of them, most written within the week, "read them all and
fix what is wrong" is not a task, it is a treadmill.

## 2. The reframe: devdoc has two classes of document, and only one is auditable

The single most important finding of this recon, and the thing that makes the epic
tractable: **the 1 010 files are not one corpus.** They split into two classes
with opposite truth semantics:

| class | what it is | its truth claim | audit verdict |
|---|---|---|---|
| **REPORT** | a run's own record: `*-report.md`, `*-audit*`, `*-review*`, `*-evaluation*`, `*-diagnosis*`, `*-sweep*`, incident/postmortem, evidence logs | *"this is what THIS RUN measured at commit X"* — a historical statement, true or false at X and never re-asserted | **not auditable as "current truth"** — auditing it means re-litigating a closed run. Wants a class banner, not a per-claim check. |
| **SPEC** | a durable normative document: a design, a spec, a roadmap, an architecture note, a contract | *"the code IS / SHOULD BE this"* — a standing claim about `master` | **auditable, and this is where the epic's value is.** These are exactly the "design docs telling the desired feature that code should fulfill" the operator named. |

The boundary is not decorative — it decides what a fix even is. A stale REPORT is
fixed by a status banner ("historical — superseded by X"); a false SPEC is fixed by
changing the doc (or filing the bug it exposes). Conflating them is why "audit the
devdocs" feels unbounded: most of the corpus has no standing claim to check.

### 2.0 A CORRECTION to this section (added after the reframe — read before §2.1)

The first pass of this doc reported the split as "REPORT ~957 / SPEC 53", derived
by a title regex over the 1 010 names. **That was an over-claim, and it is
corrected here.** Measured bucket counts (`ls devdoc/*.md`, two chained regexes):

```
total 1010 | report-named 280 | spec-named 53 | NEITHER 677
```

So the two regexes name 333 files; **677 names carry neither a report token nor a
spec token** — `GOLDEN-RULES.md`, `bash-efficiency.md`,
`01a092db-closedout-20260915.md`, `bug024-empty-thinking-400.md`,
`branch-scoped-smoke-gate.md`, … This is the corpus's LARGEST bucket, and the
first pass silently folded it into "REPORT-class (~957)". That was wrong: a
`*-fix*`/`*-closedout*`/`bug0NN-*` name does not mean the doc has no standing
claim, and **it demonstrably can** — see the §2.2 (D) finding, where a normative
architecture doc with a current authoritative table sits in exactly this bucket.

**The correction matters operationally, not pedantically.** The §4 partition was
built from the 53-name SPEC list, so an un-nameable normative doc is owned by **no
child**: `merger-self-driving.md` is in the 677 (verified: absent from the 53-name
list, named in zero C-slices) while it carries a live, currently-true normative
table. A name regex can *find candidates*; it can never *close the class*.

**Therefore the class rule the children apply is CONTENT-first, not name-first:**

- Admission test (what makes a doc SPEC-class) is **"does it make a standing claim
  about `master`?"** — a present-tense assertion about what the code is or must be.
- The 53-name list is a *seed*, not the population. Each child additionally sweeps
  the 677-name bucket for docs in its own family and admits any that passes the
  admission test. Where that is too open-ended to be exhaustive, the child reports
  its sweep's coverage boundary honestly rather than implying the family is closed.
- A doc whose claim is wholly past-tense ("this run measured X at commit Y") is
  REPORT-class whatever its name, and is out of scope for claim-checking.

### 2.1 The 53 SPEC-class candidates

**merger/worker/task-control (8)** `cli-task-surface-restructure-20260915`,
`gopi-vs-codex-task-type-specialization`, `merger-single-owner-architecture-20260905`,
`merger-stop-takeover-contract`, `scheduler-integration-design`, `task-model-backfill`,
`task-model-db-column`, `task-submit-model`

**memory/hippocampus (8)** `context-snapshot-design`, `ledger-dsn-cmd-agent-contract`,
`memorization-effort-design-20260910`, `memroadmap-m1..m4`, `sync-tag-case-policy`

**chat/web/gui/voice (7)** `SPEC-voice-asr-engine-selection`, `SPEC-voice-input-phase23`,
`SPEC-voice-input-whisper`, `chat-table-design`, `gui-chat-roadmap`,
`voice-input-design-space`, `wf-contract-webfetch`

**standards/gates (3)** `cli-contract-testsuite`, `empty-test-model-sessions-investigation`,
`reflow-gate-contract`

**merge flow (3)** `merge-resolve-ledger-busy-dsn-test-contract`,
`merge-time-contract-gate-20260912`, `merge-time-contract-gate-20260913-postmerge-winner`

**tools (2)** `remote-tool-design`, `shot-design`  **crash/diag (1)** `crash-dump-design`
**release/build (1)** `rust-port-roadmap`

**cross-cutting / other (20)** `PRINCIPLE-lifecycle-storage-completeness`,
`attachment-index-contract`, `centralized-architecture-20260830`,
`cli-naming-convention`, `cmd-agent-contract-reds`,
`deepseek-v4.1-flash-model-naming-and-kv-architecture-20260910`,
`design-decision-process-group-20260903`, `game-agent-testbed-design`,
`gopi-md-design`, `m420-rank-model`, `path-policy-cd-base`, `perf-vs-claude-fix-plan`,
`roadmap`, `session-model-banner-fix`, `taskfindjsonl-layout-determinism`,
`taskfindjsonl-layout-fixture-isolation`, `taskid-vs-sessionid-architecture-20260909`,
`todo-list-design-evolution`, `todo-vs-writetaskarea-design-20260909`, `wf-contract`

(Plus `devdoc/requirements/` — 20 files, the repo's own reverse-requirement
series; auditable as a corpus, see §4.)

### 2.2 Worked examples — the failure modes the audit will look for

All verified against `master` this session; each is the shape the epic is about.

**(D) A live, currently-TRUE normative doc that no slice owns — `devdoc/merger-self-driving.md` (the §2.0-correction finding).**
This doc is an architecture/contract document: it carries a present-tense authority
table for `gopi merger`'s flags (`--every` admitted; `--daemon`, `--stop`,
`--restart`, `--start`, `--force`, `--wait` retired) and cites its own guard test
`TestMergerRetiredFlagsAreRefusedNotIgnored`. Verified this session, the code
agrees: `cmd/agent/merger_restart.go:104-105` declare `mergerStopFlag = "--stop"` /
`mergerRestartFlag = "--restart"`, and the doc's claim that the loop needs no
state/stop-request file matches the shipped loop. **So this is an *aligned* doc, not
a drifted one** — and that is exactly why it is the important example: it is a
standing claim, its claim is true, and it was **in no child's scope** (`grep` for
its name over the whole plan: 0 hits; it is absent from the 53-name seed). An
aligned doc that no slice reads is a future drift nobody is watching, and it is
the evidence for §2.0's content-first admission rule.
*Suggested fix:* none needed for the doc's content (it is true). It must be **added
to a slice's scope** — the merger/worker family (C2) takes its claims, and the §4
table below now names it.
*Bug-worthiness:* **not-a-bug** (doc and code align). The gap is coverage, not truth.

**(A) SPEC whose headline defect was fixed and whose body still asserts it — `devdoc/gui-chat-roadmap.md` (523 L).**
Line 25's TL;DR — the table an agent building a chat front-end reads first — says
*"What is actually broken today? `gopi chat`'s input path: cooked-mode
`bufio.ReadString('\n')` (cli_chat.go:424/440). No history, no cursor editing, no
multiline, no paste guard."* That is false on `master`: `cli_chat.go:408` reads
`input.ReadLine()` (a raw-mode editor), the `internal/lineedit/` package exists
with 26 files (editor, history, keys, menu, raw, render, value, winsize), and
`devdoc/chat-lineedit.md` records M1.1–M1.3 as shipped. The doc was later
maintenance-edited (`d145c847` marked M1.4/M1.5 SHIPPED) and the stale headline was
never revisited — while the file's §1 line-number citations (`cli_chat.go:424`,
`:437`, `:440`, `:469`) point at a file that has since shrunk below line 469.
This is *not* a claim that the doc is worthless: the reuse §1.1 borrow survey is
still the best front-end reference in the tree. It is the **partial-drift** shape
— correct survey, wrong status — and the fix is a status block at the top, not a
rewrite.

**(B) SPEC for a feature that never landed — `devdoc/remote-tool-design.md`.**
A complete 5-tool SSH closure design (`remote` tool) written 2026-08-02. No
`remote` tool exists: `grep -rn 'remote_tool\|remoteTool' internal/ cmd/` returns
nothing, and `GOPI.md` Rule 3 lists 17 tools with no `remote` among them. The doc
carries no status line, so a reader today cannot tell a **planned** design from a
**shipped** one. The audit verdict for this shape is "planned-but-absent → the doc
needs a status header"; whether it is a *bug* to file is a separate, operator-level
question (see §3).

**(C) A corpus that promises a file it never grew — `devdoc/requirements/`.**
`README.md` and `00-module-map.md` both route the reader to `99-summary.md` as "the
whole-project answer"; `01-per-module-briefings.md` gives the `[15/15]` briefing for
it. It does not exist. Three module rows are likewise `open`
(M7 CLI surface, M11 notifications & observability, M13 environment adapters) or
`pending` (M9 gates). 13 of 15 module documents are present and were produced in
the last days.

### 2.3 What this reframe deliberately refuses

- **A grep proxy for staleness.** The obvious cheap check — "does every backticked
  path in a doc still exist?" — is invalid, and this recon proved it: devdoc
  citations are overwhelmingly *relative* (`loop.go:575`, `cli_chat.go:424`), so
  there is no path to resolve and the check would report noise as drift. Verifying
  a claim needs the claim's *intent* read by a model, not a regex.
- **Auditing a REPORT's claims as if they were standing.** It re-opens closed runs
  and produces "findings" that are just history. Report-class docs get a banner
  decision, not a claim-by-claim verdict.

## 3. The audit method (what every child task does, identically)

For **each SPEC-class doc**, in one pass, produce one row:

1. **Extract the standing claims.** Behavioral assertions about `master` as it is
   now (not "we measured Y on date X"). Skip borrow surveys, effort tables,
   historical narrative — mark them `informational`.
1b. **Select the docs by CONTENT, not by name** (§2.0). A child's family scope is
   the *seed* the §4 table lists; before auditing, sweep the family's docs in the
   677-name bucket (`ls devdoc/*.md` minus the report/spec regexes) and admit any
   that makes a standing claim about `master`. `merger-self-driving.md` in C2 is
   the worked case: a normative architecture doc with a live authority table and
   no `design|spec|contract` token in its filename. If a sweep cannot be made
   exhaustive, state the coverage boundary in the report — never imply closure.
2. **Test each claim against code**, with `code_search`/`read`/the test suite.
   Record the evidence as `file:line` in the row. A claim confirmed by a *passing
   test* is stronger evidence than a claim confirmed by reading — prefer it and
   name the test.
3. **Classify the claim**: `aligned` · `drifted` (code moved, doc left behind) ·
   `planned-absent` (design describes a feature the code does not have) ·
   `superseded` (a later doc/PR owns this) · `informational`.
4. **For `planned-absent` and `drifted`, give a bug-worthiness estimate** — never
   a submission, per the epic's instruction. Explicitly answer:
   - does anything *depend* on the missing feature (a failing test, a caller, a
     rule that presumes it)?
   - is it load-bearing for a user-visible surface, or a deferrable polish item?
   - is the doc the right fix (downgrade it to "planned") or is the code the right
     fix (file it)?
   Estimate: `bug` / `not-a-bug` / `needs-operator-decision`, each with the reason.
   **The default for `planned-absent` is NOT "bug"** — an unimplemented design is
   the normal state of a design doc. It becomes a candidate bug only when a
   *current* artifact (rule text, help output, a test, another doc) already asserts
   the feature exists.
5. **Report the doc's own staleness signals**: base commit it names, whether its
   `file:line` citations still land, whether a status line exists.

Output per family: one report under `devdoc/`, fixed shape
(`## Summary` / `## Doc-by-doc` / `## Findings ranked` / `## Bug-worthiness` /
`## Coverage and limits` / `## Verification`), every row carrying its code evidence.
**Honest coverage limits are mandatory** — "we audited N of M docs in this family
and here is why the rest were skipped" beats a claim of completeness.

## 4. The partition (the roadmap, and what each child owns)

Sized so that no child exceeds one working session; each is self-contained and
cites this file for the method.

| child | docs | notes |
|---|---|---|
| **C1 — cross-cutting specs + contracts** | the 20 "other" docs: `cli-naming-convention`, `cmd-agent-contract-reds`, `centralized-architecture-20260830`, `gopi-md-design`, `path-policy-cd-base`, `taskid-vs-sessionid-architecture-20260909`, `todo-vs-writetaskarea-design-20260909`, `todo-list-design-evolution`, `session-model-banner-fix`, `PRINCIPLE-lifecycle-storage-completeness`, `attachment-index-contract`, `wf-contract`, `perf-vs-claude-fix-plan`, `roadmap`, `m420-rank-model`, `taskfindjsonl-layout-determinism`, `taskfindjsonl-layout-fixture-isolation`, `deepseek-v4.1-flash-model-naming-and-kv-architecture-20260910`, `design-decision-process-group-20260903`, `game-agent-testbed-design` | highest value: naming conventions, id architecture, path policy and `GOPI.md` design are read as normative by every run |
| **C2 — task/merger/worker control specs** | `cli-task-surface-restructure-20260915`, `gopi-vs-codex-task-type-specialization`, `merger-single-owner-architecture-20260905`, `merger-stop-takeover-contract`, `merger-self-driving` **(added in the §2.0 correction — it sat in the 677-name bucket and was owned by no slice)**, `scheduler-integration-design`, `task-model-backfill`, `task-model-db-column`, `task-submit-model` | overlaps `devdoc/requirements/01-task-ledger.md` + `02-merge-and-branches.md` — read those first, they may already hold the verdict |
| **C3 — memory / hippocampus / ledger specs** | `context-snapshot-design`, `ledger-dsn-cmd-agent-contract`, `memorization-effort-design-20260910`, `memroadmap-m1..m4`, `sync-tag-case-policy` | `requirements/05-context-and-memory.md` is the sibling |
| **C4 — chat / web / voice specs** | `gui-chat-roadmap` (§2.2 A is already worked), `SPEC-voice-asr-engine-selection`, `SPEC-voice-input-phase23`, `SPEC-voice-input-whisper`, `voice-input-design-space`, `chat-table-design`, `wf-contract-webfetch`, plus the `devdoc/web-*.md` set and `chat-m2-streaming.md` | the voice SPECs are the sharpest case: they describe whisper.cpp / FunASR, the shipped path is `internal/doubaoasr` |
| **C5 — merge-flow contracts + gates/test-infra specs** | `merge-resolve-ledger-busy-dsn-test-contract`, `merge-time-contract-gate-20260912`, `merge-time-contract-gate-20260913-postmerge-winner`, `cli-contract-testsuite`, `reflow-gate-contract`, `empty-test-model-sessions-investigation` | `GOPI.md` Rules 16–21 are the code-side counterpart; check the gate names/predicates still match |
| **C6 — tools / crash / build specs** | `remote-tool-design` (§2.2 B is already worked), `shot-design`, `crash-dump-design`, `rust-port-roadmap` | small; `rust-port-roadmap` must be judged against whether any Rust port exists (none found: no `Cargo.toml`) |
| **C7 — `devdoc/requirements/` corpus** | the 20 files in `devdoc/requirements/` incl. the missing `99-summary.md` and the 3 `open` + 1 `pending` module rows (§2.2 C) | a *corpus*-level audit, not claim-level: does the series still describe today's master, and should `99-summary.md` land? |
| **C8 — REPORT-class disposition** | a sampled (not exhaustive) pass over `devdoc/*-report.md` / `*-audit*` / incident docs | NOT claim-checking. Question: what is the cheap, honest way to mark a doc as a historical record (banner? `devdoc/archive/`?) so a reader is not misled, and how many docs actually name a **superseding** doc that should be linked |

**Sequencing.** C1 first (highest fan-in: conventions reach every surface). C2–C6
in parallel — they touch disjoint code areas. C7 last (it is the repo's own
self-description, and C1–C6 findings will change what its summary should say). C8
is the meta-decision on REPORT-class docs and can run at any time, but its output
should be an *operator proposal*, since reorganising ~280 report-named (and up to
~600 historical-named) files is not an agent's call.

**Why not one task.** Each of C2–C6 is a read-the-doc, search-the-code,
test-the-claim loop over 3–20 documents; doing all six in one session means the
audit quality collapses exactly the way the operator warned ("do it carefully
because you may mis-guide project direction"). One child = one family = one report
with its own evidence.

## 5. Standing recommendation (for the operator, not a fix to apply now)

The durable fix is not this audit — it is a **class marker**. If SPEC-class docs
carried a machine-checkable header (`Status: current | planned | superseded` and
`Verified-at: <commit>`), a later drift would be visible instead of silent, and a
REPORT-class doc would be obviously historical. `internal/agent/docanchor_test.go`
is the existing precedent for a doc-facing check. This is a *proposal*; the epic
says report first, and it is reported here, not filed.
