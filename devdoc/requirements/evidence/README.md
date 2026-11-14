<!-- doc-class: historical -->
# devdoc/requirements/evidence/ — the series' evidence index (entry point + task report)

Item **[2/15]** of the reverse-requirement-analysis series: the shared
requirement-evidence index every later module worker mines. Read the files in this
order:

| file | what it is |
|---|---|
| `evidence-index.md` | **the curated index** — the five evidence classes (E1–E5) with the greppable citation handle for each, and the "what is NOT recorded" accounting (14 categories, with counts) |
| `evidence-human-asks.md` | **E1, machine-generated**: all 608 `source=human` ledger rows (task id, ISO + epoch created time, status/tags/parent, summary and payload verbatim) |
| `extract-human-asks.sh` | the read-only extractor that produces the file above |
| `evidence-counts.sh` | the read-only counter that produces every number quoted in the index |

Self-contained by construction: a worker that receives only this directory plus
its own briefing can cite a requirement, or see that no artifact exists for it.

## Completed

- **E1 evidence file** — 608 rows, one per `source=human` task row, machine-generated
  (never hand-written): `<!--row: <task_id>-->` marker per row (the count handle),
  `### <task_id>` heading, ISO-8601 + epoch `created_at`, status/tags/parent, then
  summary and payload verbatim.
- **Curated index** (`evidence-index.md`) — E1 sub-sets A/B/C/D with counts; E2
  `GOPI.md` rules mapped to the user need each encodes, with `GOPI.md:<line>`
  handles; E3 the devdocs that state a design decision (with discovery greps); E4
  why-not-what commits (11 cited by SHA); E5 intent-stating production comments
  (12 cited by `path:line`).
- **"What is NOT recorded"** — 14 categories with measured counts, including the
  E1 authorship contamination (max 214 of 608 rows are human-attributable).
- **Two read-only scripts**, committed next to the docs, plus the re-run section
  stating exactly what a re-run may differ in.

## Not Done (honest)

- **The planned three classes are five.** `01-per-module-briefings.md` names E2 =
  `briefings/*.md` + `GOPI.md`, E3 = `devdoc/roadmap.md`, E4 = commit bodies. This
  index adopts that vocabulary but **folds the briefings and `roadmap.md` into E3**
  (documents that state a decision) and adds **E5** (intent-stating comments, §6.2
  source #5). A worker must read `evidence-index.md`'s legend, not assume the
  briefing's numbering.
- **The E1 contamination fix is not on master.** `e10d9958` (derive the submit
  source; `human` is reserved) lives on branch `fix/task-source-worker-derivation`.
  This snapshot therefore still carries rows stamped `human` that an agent
  submitted; the index quantifies them rather than filtering them out.
- **Session transcripts are not indexed.** 74,250 session dirs exist against 104
  rows in the `chats` table; no greppable index of them was produced (it is not an
  evidence file, it is a corpus).
- **No live session logs were read.** The count queries the method doc §6.1 and
  the E1 file cover the ledger; the `*.jsonl` transcripts were not searched for
  operator verbatims — so E1's set C (29 marker rows) is a lower bound.
- **No tests written, no production code changed** — the series forbids both.

## Verification

| gate | command | result |
|---|---|---|
| Build | `go build ./...` | not run — **no `.go` file in this change** (`git status --porcelain` lists only `devdoc/requirements/evidence/`) |
| Tests | `go test ./internal/standards/` style gates | not run — same reason; no Go file, no gate scans `devdoc/` content |
| Row-count identity | `sqlite3 ~/.gopi/memory.db "select count(*) from tasks where source='human'"` vs `grep -c '^<!--row: ' …evidence-human-asks.md` | **608 = 608** |
| Determinism | `bash …/extract-human-asks.sh "$HOME/workspace/_tmp/regr.X/evidence-human-asks.md" && cmp …/evidence-human-asks.md "$HOME/workspace/_tmp/regr.X/evidence-human-asks.md"` | **`BYTE-IDENTICAL`** |
| Row structure | per-marker check: 608 markers, `### <id>` heading + `created`/`epoch`/`status` lines present | **0 structurally bad rows**, 608/608 distinct ids = `count(distinct task_id)` |
| Cited handles resolve | each `GOPI.md:<line>`, `devdoc/<file>`, `<sha>`, `path:line` in the index checked against the tree | all resolve (3 off-by-one GOPI line numbers found and fixed) |

## Evidence (self-filed)

- **Row-count identity** — `sqlite3 "file:$HOME/.gopi/memory.db?mode=ro" "select count(*) from tasks where source='human';"` → `608`; `grep -c '^<!--row: '` on the file → `608`. Both printed in the same command, output `608` then `608`.
- **Determinism** — E1 regenerated into the review scratch dir (`$HOME/workspace/_tmp/regr.*`, Rule 22: never `/tmp`) and `cmp`-ed against the committed file → `BYTE-IDENTICAL`; the script reprints `rows in ledger : 608` / `rows in file : 608`.
- **Ledger opened read-only** — every query used the DSN `file:/home/user/.gopi/memory.db?mode=ro`; no write statement was issued against it. The scripts write only to the repo (`Rule 22`-safe: no temp files, no device load, no `/tmp` default).
- **E1 contamination (the number the index leans on)** — `select sum(session_id<>'' and model<>''), sum(session_id<>'' and model like 'deepseek-flash%'), sum(session_id='' and model='') from tasks where source='human'` → `314|174|214`; task-`354824` mechanism quoted from commit `e10d9958`'s own body, whose presence on master was tested with `git ls-tree -r --name-only master | grep -c internal/tasksource` → `0`.
- **Gap counts** — one consolidated `select` printed `human_rows|wrapper_rows|no_payload|no_deliverable|no_verify|canc_noreason|fail_noreason|reopened|no_events|dup_extra_rows|dup_groups` = `608|70|10|81|576|21|9|0|201|17|12`; the `task_events` companion query printed `1303|427` (events / empty `detail`); `ls ~/.gopi/sessions | wc -l` → `74250` against `select count(*) from chats` → `104`.
- **Class sizes** — `grep -c '^## Rule [0-9]' GOPI.md` → `24`, `'^### Rule [0-9]'` → `6`, `'^\*\*Why\*\*'` → `5`; `git ls-tree -r --name-only master -- devdoc | grep -c '\.md$'` → `1029`; `ls devdoc/ | grep -icE 'decision|ruling|why'` → `9`; `grep -lriE 'because|so that|instead of|root cause' devdoc/*.md | wc -l` → `595`; the E4 grep → `754` of `4601` commits, operator-quoting subset → `5`.
- **Handle resolution** — `sed -n "${L}p" GOPI.md` for 29 line handles, `test -f` for 19 devdoc paths, `git cat-file -e <sha>^{commit}` for 11 SHAs, `sed -n "${n}p" "$f"` for 12 `path:line` comments; all printed `OK` after the three GOPI line numbers were corrected (`109→85` Rule 3, `159→114` Rule 5).
- **Not run, and why** — `go build`/`go test` are *not* claimed: the change contains zero `.go` files, verified by `git status --porcelain` listing only `?? devdoc/requirements/evidence/` before the commit.

## Rules

- **Rule 22** — no temp path is named (`extract-human-asks.sh` writes only to its
  `OUT_PATH`, defaulting into the repo); no load was generated (Rule 27).
- **Rule 27** — nothing here measures a device; the two slow-ish commands are
  `sqlite3` count queries and `git log` greps.
- **Rules 6a/6b/23/29** — work done in `~/workspace/gopi-req-evidence-index`
  (branch `feat/req-evidence-index` off master `9ce8e159`); the live worktree
  `~/workspace/gopi-req-umbrella-verify` was read from only, never entered.
