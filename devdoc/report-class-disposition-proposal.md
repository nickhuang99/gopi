<!-- doc-class: historical -->
# Task Report: C8 — REPORT-class disposition (a PROPOSAL, not a mass edit)

**Base:** master `52090dc0`.
**Deliverable:** this file only. Nothing moved, nothing renamed, nothing deleted,
nothing submitted, no file outside this report touched.

---

## Completed

A SAMPLED pass over the REPORT-class devdocs answering the two questions the epic
asked, with every number measured on master and the command that produced it.

### 1. The population — and why the briefing's "~957" is not one

The briefing's "~957 report-class devdocs" is a **residual, not a predicate**, and
the parent epic's own instrument doc already retired it. `devdoc/doc-truth-audit-plan.md`
§2.0 ("A CORRECTION to this section") records the first pass as: *total 1010 |
report-named 280 | spec-named 53 | NEITHER 677*, and states plainly that folding the
677 into "REPORT-class (~957)" **was an over-claim**. 957 = 1010 − 53: the complement
of a name regex, which is not a class.

The name-regex population is far smaller than 957:

```
$ find devdoc -type f | grep -Ei -- '-(report|audit|review|evaluation|diagnosis|sweep)|\baudit|incident|postmortem|\.log$' | wc -l
239          # of which 160 are .md
```

**The authoritative, content-first classification already exists on master** — the
findings of the sibling doc-truth-classification work, not something this task
invents (Rule 13/14: reference it, do not re-derive it):

```
$ awk -F'\t' 'NR>9{print $2}' devdoc/DOC-INDEX.tsv | sort | uniq -c | sort -nr
   1595 historical        # <-- the REPORT class
    404 normative
     12 roadmap
      1 index-meta
```

`devdoc/DOC-INDEX.tsv` (2012 data rows — `wc -l` reads 2021, less 9 header/comment
lines — one per file under `devdoc/`) is the manifest;
`internal/docclass` is the single source of truth for the class vocabulary and the
banner; `internal/standards/docclass_gate_test.go` is the machine check. So **this
task's population = the manifest's 1595 `historical` rows**, split by what a marker
can even do to them:

| shape | count | can carry a markdown banner? |
|---|---|---|
| historical `.md` | **651** | yes |
| historical non-`.md` (`.txt` 227, `.err` 202, `.png` 165, `.json` 75, `.log` 63, `.sh` 51, `.wav` 32, …) | **944** | **no** |
| total | 1595 | |

### 2. Question 1 — what cheap, honest signal says "this is a historical record"?

**Measured: the signal already exists, and it is already 100 % applied to markdown.**

```
$ # first line of each historical .md, exact banner match
historical .md carrying `<!-- doc-class: historical -->` : 651 / 651
```

Not one historical markdown doc is missing it, and the convention is not decorative
— it is an HTML comment so it is invisible when rendered, it is line 1 so a reader
meets the class before the title, and `TestDocClassBannerMatchesManifest` fails the
build if a file and the manifest disagree in either direction. Cost of the banner
sweep: **0 — it is already done.**

**The real gap is the 944 non-markdown historical files.** A `.err` capture, a `.log`,
a `.png` screenshot or a `.wav` cannot open with an HTML comment; they have no
in-file channel at all. The honest answer for them is not "a second convention" but
**the manifest is the marker**: `devdoc/DOC-INDEX.tsv` already classifies every one
of them `historical`, is the single source of truth, and is what every class-driven
gate reads. A parallel marker for binaries would be a second spelling of the same
fact (Rule 14).

The other markers a reader already gets, for context (**not** a recommendation) — all
five measured over the same 651:

| organic marker, no enforcement | count |
|---|---|
| asserts a `status:` token anywhere in its text | 35 / 651 |
| asserts one as a line-leading `Status:` field | 14 / 651 |
| names a 40-hex commit sha (a DONE-AT signal) | 28 / 651 |
| names a short (≥7-hex) sha, any purpose | 577 / 651 |
| cites another `devdoc/*.md` path | 396 / 651 |

The `status:` token is the **worst** instrument of the set: 35 docs carry the *word*
and 14 carry it as a field — the other 21 mean "task status" in a body sentence. A
prose convention that lands at 5 % coverage and 40 % precision is not a convention;
it is what a reader gets *instead of* a convention. The commit-citing habit is the
opposite — it is dense (577/651 name some short sha) and it is what actually lets a
reader date a record, which is why the recommendation below extends that habit rather
than inventing a `Status:` line.

### 3. Question 2 — how many sampled docs name a superseding doc?

**Selection rule (stated so the sample can be reproduced):** over the 651 historical
`.md` paths — the population a cross-link could actually modify — take the paths in
lexicographic order and keep **every 17th** (`awk 'NR%17==1'` after `sort`), giving
**n = 39** (k = ceil(651/40)). The rule is deterministic, needs no seed, and spans
the corpus evenly rather than sampling one family.

```
SAMPLE: 39 docs read
```

Three predicates, weakest to strictest, all applied to the SAME sample. `T` below is
the one token set for all three (`supersed|replaced by|obsolet|no longer
(current|valid)|do not (use|read|follow)|now lives in`, case-insensitive):

| predicate | sample (n=39) | population (651) |
|---|---|---|
| (loose) file carries a token from `T` | 11 / 39 | 106 |
| (file-scope) carries a token **and** cites some other `.md` | 10 / 39 | 91 |
| **(strict) a single line names a `.md` AND carries a token** | **1 / 39** | **7** |

The three rows exist because the answer moves by an order of magnitude between them
(106 → 7), and only the strict row survives reading: the loose and file-scope counts
are counting the *word*, and in this corpus the word is about rows, commits and code
symbols far more often than about documents.

**Every one of the 11 loose sample hits was adjudicated by reading it, and none of
them is a devdoc-to-devdoc supersession.** They are: a *ledger row* closed as
`SUPERSEDED`; a scratch draft called "superseded" in its own title; a GATE **rule
sentence** replaced in `GOPI.md` (the single strict hit in the sample,
`dirty-closeout-binding.md:173` — Rule 30's "always recorded, never refused" sentence
replaced by the refusal design, i.e. a line about `GOPI.md`, not about a devdoc);
earlier **merge commits** called superseded; a branch state; a code **symbol**
(`scopeDirName`) replaced by another symbol; `[superseded]`-tagged **numbers**; a
**correction record** in the ledger; a count in `baseline.go`; a **binary** replaced by
a worker; a **reap reason**. The token is overwhelmingly about rows, commits, numbers
and symbols — not documents.

At population level the strict predicate's 7 hits were each opened and the matching
line read. Verdict per hit, with the line that matched:

| file | matching line is about | doc→doc? |
|---|---|---|
| `rust-port-reframe-v2.md:89` | *"`devdoc/rust-port-roadmap.md` (v1) is left in place for history; this file supersedes it"* | **YES** |
| `merge-time-contract-gate-20260913-postmerge-winner.md:209` | table row: *"`devdoc/merge-time-contract-gate-20260912.md` — cost section annotated as superseded"* | **YES** |
| `backup-peerbox-to-hp-task-report.md` | names `devdoc/memory-sync-assets-2026-09-02.md` §6 as `标记 SUPERSEDED` | **YES** |
| `dirty-closeout-binding.md:173` | `GOPI.md` Rule 30 sentence replaced (a RULE, not a doc) | no |
| `test-b-rule6-merge-report.md:6` | Rule 6 *gap* called obsolete (a rule) | no |
| `nested-report-path-clobber.md:29` | a `task-report.md` file *overwritten by a child probe* | no |
| `requirements/evidence/evidence-human-asks.md:30142,30188` | task ACCEPTANCE text — instructions that say "supersede it" | no |

So **3 / 651 (0.5 %)** are genuine doc→doc supersession statements.

**Finding: prose supersession is rare (3/651 ≈ 0.5 %), but STRUCTURAL supersession is
common and self-declaring.** A same-stem family (`X.md` + `X-v2` / `X-<date>` /
`X-verify`) IS a supersession relation. Stemming the 651 by stripping trailing
`-vN` / `-<8-digit date>` / `-stageN` / `-{report,task-report,verification,verify,verified,impl,final,winner,postmerge-winner,reframe,design,evidence,summary}`:

```
15 same-stem families covering 33 historical docs (5.1 % of 651)
12 of those 33 already declare supersession in their own text (36 %)
```

The families and their sizes: `ledger-treasure-hunt` (5), then fourteen 2-member
families — `worktree-refusal-arms`, `worktree-combo-cleanup`, `tasklist-audit`,
`session-model-banner-fix`, `screen-capture-llm-shot`, `rust-port`, `resume`,
`nightshift`, `merger-status`, `merge-time-contract-gate`, `image-input`,
`game-agent-testbed`, `finish-cleanup-single-node-wording-merge`, `09-chat-ui`.

(Stem definition is the sensitive part, and it is stated here so the number can be
reproduced or contested: a looser stem that stops after one suffix-strip finds 9
families / 18 docs, because `-20260917-stage0` and `-postmerge-winner` survive one
pass. The 15/33 figure is the fully-stripped one. Either way the conclusion holds and
is an order of magnitude away from the 3 prose hits.)

So the answer to Q2 is not a number of scattered docs to link: it is **33 docs in 15
families**, and a grep for the word "superseded" is the wrong instrument for finding
them.

### 4. Recommendation, with cost

**(A) Marker — keep the existing banner; add nothing.**
The convention exists, is 100 % applied to the 651 markdown historical docs, is
machine-gated, and is invisible when rendered. A new banner or a `Status:` convention
would be a second spelling of a value that already has one source of truth
(`internal/docclass`). For the 944 non-markdown records the marker is the manifest
row, not a file edit.
*Cost: 0. Do nothing.*

**(B) `devdoc/archive/` move — recommend AGAINST, on measured cost.**
The signal the move would buy is already delivered on line 1 by the banner, and the
move is expensive and mechanically destructive:

```
historical .md citing another devdoc/*.md path : 396 / 651   (61 %)
distinct devdoc paths cited                    : 551
manifest rows keyed by path                    : 2012
```

A move rewrites the target of 551 distinct citations across 396 docs, invalidates
2013 manifest rows (the coverage gate asserts manifest↔tree agreement **exactly**),
and re-points the anchor gate's scope, all to re-express a class the banner already
states at a cost of zero. (For scale: `devdoc/archive/` already exists and holds
**1** file.)

**(C) Supersession cross-linking — do the cheap deterministic slice, skip the sweep.**
A family-linker (15 families / 33 docs) is a single deterministic pass that finds
**100 %** of the structural cases, at roughly one minute of work.
A full read-sweep of all 651 — 8.4 MiB, 142,680 lines, ~13.6 KB mean and a 2 MB
outlier — is **~2–3 agent sessions (~1.8 M tokens)** and its marginal yield over the
cheap pass is the **3** explicit prose statements found by a single grep.

**One defect found and left to a dispatch, not fixed here** (Rule 26): the manifest's
own coverage gate is **RED on master, independently of this run.** The manifest's
rows and `devdoc/`'s files are meant to agree *exactly*; measured (uncached,
`--count=1`), the difference is **29 files on master, 30 on this branch** — this
report being the 30th, a consequence of the acceptance criterion "no file outside the
report touched":

```
$ comm -23 <(git ls-tree -r --name-only master devdoc | sort) \
           <(git show master:devdoc/DOC-INDEX.tsv | awk -F'\t' '!/^#/{print $1}' | sort) | wc -l
29
```

The 29 (`git ls-tree -r --name-only master devdoc` vs the manifest's paths):

```
archive-tag-audit-20260918.md            ledger-treasure-hunt-20260917-stage5.md
archive-tag-audit-20260918.tsv           ledger-treasure-hunt-20260918-consolidate.py
attachment-claim-marker.md               ledger-treasure-hunt-20260918-final.md
conflict-marker-gate.md                  ledger-treasure-hunt-20260918-rows-final.csv
fd-finalizer-leak.md                     merger-help-detach-vocabulary.md
heavytest-scratch-pin-fresh.md           merger-help-truth.md
ledger-treasure-hunt-20260917-rows-stage0-residual.csv   merger-loop-stop-signal-window-rework.md
ledger-treasure-hunt-20260917-rows-stage0-residual.py    merger-loop-stop-signal-window.md
ledger-treasure-hunt-20260917-rows-stage5-resolved.csv   merger-restart-observed-superseded.md
ledger-treasure-hunt-20260917-stage5.md  merger-tick-slim.md
orphan-guard-delivery-point.md           scratch-home-block-backed-red.md
procstat-parse-single-source.md          smoke-fixture-script-closure.md
remote-submit-central-ledger.md          task-process-liveness-predicate.md
repo-context-gate-recognition.md         web-menu-keyboard-fix.md
requirements/99-summary-gate.sh          requirements/99-summary.md
```

That the missing set overlaps the family set of §3 (`merger-loop-stop-signal-window`
+ `-rework`, `ledger-treasure-hunt` ×5, `merger-restart-observed-superseded`) is not a
coincidence: **new family members are exactly the docs that arrive without a row** —
the same work-stream that produces a v2 tends to skip the manifest step. The
`docclassMinRows=1900` floor still passes, so it is the coverage assertion — the
design's own rule, "a new doc without a class is a red build" — that fails.

---

## Not Done

- **Deliberately not implemented** (the task is a proposal): no banner added, no file
  moved, no cross-link written, no manifest row added, nothing submitted.
- **A manifest row for this report was NOT added**, although the docclass design
  requires one and sibling C-children added theirs (`ed1be6df docs(index): classify
  devdoc/gates-doc-truth-audit.md (historical)`). The acceptance criterion says "no
  file outside the report touched", so I respected it literally. **Consequence,
  stated plainly: merging this branch takes the unclassified-file count from 29 to
  30 and leaves `TestDocManifestCoversEveryDevdocFile` red.** The one-line fix is a
  `historical` row in `devdoc/DOC-INDEX.tsv` (class + reason + empty evidence), which
  belongs in the dispatch below rather than in a proposal branch.
- **The 944 non-markdown historical files were not individually inspected** — only
  counted by extension. They carry no in-file marker channel, which is the finding;
  whether any deserves promotion to markdown was out of scope.
- **No claim-by-claim verification of any report-class doc**: that is the one thing
  §2.3 of the plan doc forbids for this class, and this proposal does not do it.

---

## Verification

- **Build:** `go build ./...` — OK (no production code changed).
- **Tests:**
  - `go test -count=1 -run 'TestDocClassBannerMatchesManifest' ./internal/standards/` →
    `ok github.com/nick/gopi/internal/standards 2.567s` (green — the banner/manifest
    agreement this proposal relies on holds).
  - `go test -count=1 -run 'TestDocManifestCoversEveryDevdocFile' ./internal/standards/` →
    **FAIL**, `devdoc/DOC-INDEX.tsv: 2012 rows cover 2042 files under devdoc/` and the
    gate's `... and 5 more unclassified files` — the pre-existing master defect of §4
    (29 files) plus this report (30). `-count=1` is not decoration: the gate reads
    devdoc **data files**, which Go's test cache does not track, so a cached `ok`
    would be a false green here.
  - Both runs were made with `git status --porcelain` empty except this one tracked
    file, so the tree under test is the branch's own.
- **Commit:** the tip of `feat/report-class-disposition`, subject `docs(c8): REPORT-class
  disposition proposal — sampled (39/651), banner already exhaustive, archive move
  rejected on measured cost`. Read it with `git log --oneline -1` in the worktree (a
  commit cannot cite its own hash — amending to insert it changes it again).

## Evidence (self-filed)

One line per claim: the command and the value it printed. Every line was re-run or
re-derived uncached in worktree `/home/user/workspace/gopi-c8-report-class` (master
`52090dc0`) during this pass — where a number in §2/§3 above differs from this list,
**this list is the later measurement and the body was corrected to match it.**

- Population derivation: `find devdoc -type f | grep -Ei -- '-(report|audit|review|evaluation|diagnosis|sweep)|\baudit|incident|postmortem|\.log$' | wc -l` → `239` (160 `.md`); the 957 figure is traced to `devdoc/doc-truth-audit-plan.md` §2.0.
- Class counts: `awk -F'\t' 'NR>9{print $2}' devdoc/DOC-INDEX.tsv | sort | uniq -c` → `1595 historical`, `404 normative`, `12 roadmap`, `1 index-meta`. (`wc -l` = 2021 lines = 2012 data rows + 9 header/comment lines; the earlier `NR>3` variant was wrong because the header block is 9 lines long.)
- Historical split: `historical .md = 651`, `historical non-.md = 944` (extensions `.txt` 227 / `.err` 202 / `.png` 165 / `.json` 75 / `.log` 63 / `.sh` 51 / `.wav` 32).
- Marker coverage: loop over the 651 historical `.md` rows (`head -1 | grep -q '^<!-- doc-class: historical -->'`) → `bannered=651/651`.
- Sample: the 651 historical `.md` paths sorted, `awk 'NR%17==1'` → `39` docs.
- Q2 loose predicate: `grep -liE 'supersed|replaced by|obsolet'` over the sample → `11/39`.
- Q2 strict predicate (one line naming a `.md` AND a supersession token): `awk -f /tmp/strict.awk` (`match($0,/[A-Za-z0-9._\/-]+\.md/) && tolower($0) ~ /supersed|replaced by|obsolet|no longer (current|valid)|do not (use|read|follow)|now lives in/`) → sample `1/39` (`devdoc/dirty-closeout-binding.md:173`), population `7/651`; all 7 lines read and adjudicated → **3** genuine doc→doc supersessions.
- Families: full suffix-strip stem (`awk` `gensub` loop over `-vN`, `-<8 digits>`, `-stageN`, and the word suffixes) then `uniq -c | awk '$1>1'` → `15` families, `33` docs; supersession token inside those 33 → `12`.
- Organic markers: `grep -lEi '^[[:space:]>#*-]*[[:space:]]*status:'` → `14/651`; `grep -liE 'status:'` → `35/651`; `grep -lE '\b[0-9a-f]{40}\b'` → `28/651`; `grep -lE '\b[0-9a-f]{7,40}\b'` → `577/651`.
- Move-cost inputs: `grep -lE 'devdoc/[A-Za-z0-9._/-]+\.md'` over the 651 → `396`; `grep -ohE … | sort -u` → `551` distinct cited paths; `ls devdoc/archive | wc -l` → `1`.
- Volume: `xargs wc -lc` over the 651 → `142680 8834365` (8.4 MiB).
- Pre-existing red gate: `go test --count=1 -run TestDocManifestCoversEveryDevdocFile ./internal/standards/` → `FAIL`, `2012 rows cover 2042 files`; independent path check `comm -23 <(git ls-tree -r --name-only master devdoc|sort) <(git show master:devdoc/DOC-INDEX.tsv|awk -F'\t' '!/^#/{print $1}'|sort) | wc -l` → `29` on master, `30` on this branch.
- Worktree cleanliness at close-out: `git status --porcelain` in the worktree prints nothing after the final commit.

## Proposed dispatch (NOT submitted — Rule 26 names the work, the operator decides)

This run submits nothing (acceptance: "nothing submitted"). The two items the
proposal leaves actionable, written so they can be dispatched verbatim:

1. **Classify the 29 unclassified devdoc files** — add one `historical` row
   (`class` + `reason` + empty `evidence`) to `devdoc/DOC-INDEX.tsv` for each, taking
   `TestDocManifestCoversEveryDevdocFile` from red to green. Acceptance: gate green
   with `-count=1`, and the *reason* column filled per the manifest header (not left
   blank — the gate checks presence, a reader checks the sentence).
2. **Family linker** — for each of the 15 same-stem families, add a one-line pointer
   from every member to the family's live head (or record the family as
   all-historical). Acceptance: the 33 docs each name their successor inline; no file
   moved; the deterministic stem rule stated in the report is the one used.

## Deliverable location (Rule 6d)

- **Branch:** `feat/report-class-disposition` (worktree `/home/user/workspace/gopi-c8-report-class`), based on master `52090dc0`.
- **Recovery:** `git show feat/report-class-disposition:devdoc/report-class-disposition-proposal.md`
- The commit is in the main repo (`~/work/GoPi`) via the shared worktree refs — no push needed.
