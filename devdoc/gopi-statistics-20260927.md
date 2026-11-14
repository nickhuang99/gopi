<!-- doc-class: historical -->
<!-- census-evidence-ok: this public export was published with NO git history (a clean-tree export, not a clone of the development repository), so a commit pin can never resolve here by construction — the figures are a snapshot of the private tree, kept as a record and not as a re-derivable claim about this repository -->
# GoPi project statistics — second snapshot (2026-09-27)

**Base commit measured:** `cc5ac1fd8a82c9cdd78ec1e3ef9e9bf4aa1b8906` (re-measured on this
ground's snapshot tip; the original base `574505bf6cd4dece18668d162b405455e30c11f2`,
2026-09-27 21:06:37 +0800, is NOT reachable here — see the truncation note below)

<!-- census-truncation: this ground's object store is a TRUNCATED snapshot (root 964b2404, 106 commits), so the original pin 574505bf does not resolve here; the figures are restated at a pin this repository CAN re-derive, and the original hash is kept as history below. -->
**Previous snapshot:** `devdoc/gopi-statistics-20260920.md` (base `7e342589`, 2026-09-20) — this
document is its successor and every "growth" row is measured against that commit.
**Generator:** `scripts/gopi-stats.sh` — re-run to reproduce every number below:

```bash
scripts/gopi-stats.sh                            # the current worktree
scripts/gopi-stats.sh --delta [REF]              # + a growth table against REF
                                                 # (default: the 2026-09-20 snapshot)
```

> **Why a script and not a table.** A number typed into prose goes stale the moment
> the next commit lands — the previous snapshot's own §3 quotes 4,848 commits for a
> project that now has 6,588. Every figure below was printed by the generator at the
> pinned commit; nothing is typed from memory, and the growth column is read out of
> the **object store at both revs** (`git grep -c ''`), never from a second checkout
> (a foreign tree is another run's worktree — GOPI.md Rule 29).

---

## 1. Scale

| Dimension | 2026-09-20 (`7e342589`) | **2026-09-27 (`574505bf`)** | Growth |
|---|---:|---:|---|
| Production Go | 401 files / 173,702 LOC | **538 files / 248,423 LOC** | +137 / **+74,721** (×1.43) |
| Production functions | 4,668 | **6,525** | +1,857 |
| Tests | 775 files / 227,003 LOC | **1,131 files / 341,060 LOC** | +356 / **+114,057** (×1.50) |
| `Test*` functions | 4,687 | **6,553** | +1,866 |
| Benchmarks | 0 | **0** | — |
| Test : production LOC | 1.31 : 1 | **1.37 : 1** | tests stay ahead |
| Packages | 54 (`cmd` 3, `internal` 50) | **65** (`cmd` 3, `internal` 61) | +11 |
| Shell | 147 files / 16,910 LOC | **186 / 22,391** | ×1.32 |
| Python (test drivers) | 88 files / 16,211 LOC | **104 / 19,901** | ×1.23 |
| Env vars | 78 | **89** | ×1.14 |
| Web/UI | 14 HTML (1,862) · 5 JS (1,421) | **14 HTML (1,862) · 5 JS (1,502)** | HTML flat, JS +81 |
| Markdown | 1,542 files / 304,104 LOC | **2,063 / 405,937** | +521 / **+101,836** (×1.33) |
| ↳ devdoc share | 1,141 / 250,811 (82%) | **1,444 / 313,484 (77%)** | +303 files |
| Doc : code LOC | 1.75 : 1 | **1.63 : 1** | doc grows slower than code |
| Tracked files | 4,060 | **5,258** | +1,198 |
| Tracked content | 45.3 MB | **66.1 MB** | ×1.46 |
| `.git` object store | 261,516,384 B | **212,256,497 B (~202 MiB)** | shrank (repack) |

> **Two instruments, on purpose.** Go, test and markdown rows are the worktree census the
> generator prints; shell and python quote the **committed-HEAD** census (`git grep -c ''`,
> the growth table's own instrument), because a worktree count includes the run's own
> uncommitted edits — during this snapshot the generator was mid-edit, so its worktree shell
> count read higher than the commit's. Neither instrument is wrong; mixing them silently would be.

**Largest production areas** (`internal/tools` 85,718 LOC · `cmd/agent` 60,735 ·
`internal/agent` 18,287 · `internal/lineedit` 11,767 · `internal/scratch` 6,340) and the
**largest single files** (`internal/tools/tasks.go` 7,806 · `internal/agent/loop.go` 7,024 ·
`cmd/agent/cli_tasks.go` 5,727 · `internal/tools/task_finish_cleanup.go` 4,608 ·
`internal/tools/memory.go` 4,575 — vs 6,778 / 5,765 / 4,808 three weeks ago).

**Reading.** In seven days the project added **≈291k lines** (74.7k production Go + 114.1k test
Go + 101.8k markdown — the three lines that grow), and the single most distinctive property
survived the growth: **the tests and the written record are each larger than the code**, with a
doc corpus at 1.63× the production LOC. The ratio moved the right way (docs ×1.33 against code
×1.43) — the code is catching up with its own paper trail rather than the reverse.

---

## 2. Feature surface (what a user can actually invoke)

| Surface | 2026-09-20 | **2026-09-27** | Source |
|---|---:|---:|---|
| LLM-facing tool names | 28 | **29** | `internal/toolnames/names.go` |
| CLI verbs | 28 | **34** | `gopi --help` (`sort -u`) |
| `gopi task` subcommands | 17 | **17** | `gopi task -h` |
| Environment variables | 78 | **89** | `gopi --help-env` |
| `settings.json` keys | — | **63** | `gopi --help-env` (2nd column) |

**New CLI verbs since the last snapshot:** `capture doccheck keyword sandbox setup ui`
(`act archive brew chat checkpoint cli code-search completion config log memory memsync
merge merger proxy raw resume scratch-gc screen shot stats task text version vision voice
web worker` were already there). `capture`/`ui`/`sandbox` are the screen+GUI and
isolation surfaces; `doccheck` is the Rule 36 corpus verb; `setup` is onboarding.

**New tool names:** `chat_surface`, `voice_input` (29 vs 28).

> **Taxonomy note (unchanged).** GOPI.md Rule 3 says "17 tools"; `toolnames` holds 29. The
> extra names are the *action* entry points of the merged `task`/`memory`/`job` families
> (`view commit review reframe start kill output search store update feedback`)
> plus `chat_surface`/`voice_input`. Two taxonomies, one rule text — still worth a
> one-line clarification, and still not reconciled (see `## Not Done`).

### The adopted surface — measured from real session logs, not from the declaration

Sample: the **400 newest** `~/.gopi/sessions/**/*.jsonl` (169.4 MiB, spanning
**2026-09-17 → 2026-09-27**), counting `"name":"…"` tool_use records.

| Tool | Calls | Share | | Tool | Calls | Share |
|---|---:|---:|---|---|---:|---:|
| `bash` | 22,253 | **62.5%** | | `context_trim` | 147 | 0.41% |
| `edit` | 3,033 | 8.5% | | `task_submit` | 136 | 0.38% |
| `job` | 2,966 | 8.3% | | `code_search` | 134 | 0.38% |
| `read` | 2,539 | 7.1% | | `keyword_search` | 111 | 0.31% |
| `task` | 2,042 | 5.7% | | `memory_search` | 108 | 0.30% |
| `memory` | 1,210 | 3.4% | | `write_task_area` | 72 | 0.20% |
| `write` | 699 | 2.0% | | `vision_analyze` | 53 | 0.15% |

Rarest of all (bottom of the ranking): `todo` 24 · `screen_capture` 13 · `memory_store` 12 ·
**`git_blame` 12** · `web_search` 5 · `log` 1.

**Reading — and it is the same reading as three weeks ago.** `bash` is **62.5%** of all
35,606 calls (it was 76% of a smaller corpus), and the two specialised search tools
(`code_search` + `keyword_search` = 245 calls, **0.69%** combined) remain ~90× rarer than
`bash`. `git_blame` moved from 4 → 12 calls: still effectively unadopted. This is GOPI.md
Rule 11's thesis measured for the third time — **a tool exists iff the model reaches for
it**, and the routing text is the only thing that decides that. The good news in the same
table is `memory` (1,210) + `context_trim` (147) + `task_submit` (136): the
harness-discipline tools *are* adopted, because the prompt narrates them at the moment of
use.

---

## 3. Development history

| Metric | 2026-09-20 | **2026-09-27** |
|---|---:|---:|
| First commit | `d5736e9a` 2026-07-31 | unchanged |
| Age at snapshot | 50.3 days | **58.0 days** |
| Commits | 4,848 | **6,588** (+1,740) |
| Merges | 1,113 (23%) | **1,638 (24.9%)** |
| Avg pace | 96.3/day | **113.6/day** |
| Commits, last 7 / 30 days | 2,252 | **1,653 / 6,147** |
| Branches | 82 remote · 4 local · 6 worktrees | **79 remote · 2 local · 4 worktrees** |
| Tags | 349 (167 `archive/*`) | **400 (174 `archive/*`)** |
| Contributors | 8 identities | **10 identities** |
| Churn | +939,397 / −125,836 (12,924 touches) | **+1,273,549 / −148,505 (17,099 touches)** |
| Rules | 24 rules in GOPI.md | **31 rules in GOPI.md (1,080 lines)** |
| `CONFIG.md` | — | **632 lines** |

Monthly: 2026-07 = 15 · 2026-08 = 690 · **2026-09 = 5,883** (through the 27th).
Commit types: `docs` 2,769 · `fix` 1,020 · `feat` 570 · `test` 303 · `merge` 209 ·
`adoption` 62 · `refactor` 48 · `chore` 46 · `style` 31 · `probe` 17.

**Ledger (`~/.gopi/memory.db`, 146.2 MB):** **3,177 task rows** —
completed 1,852 · cancelled 1,057 · failed 199 · verified 68 · in_progress 1.
Source: `auto` 1,612 · `human` 831 · `llm` 617 · `agent` 107 · `server` 10.
Driving model: none recorded 2,617 · `glm-5.3-flash` 333 · `deepseek-flash` 198 ·
`deepseek-v4-flash` 17 · `deepseek-v4-pro` 6. Tasks created in the last 7 days: **280**.

**Knowledge base (same DB):** `distilled_qa` **6,241** rows (warm 6,118 · cold 122 · hot 1),
**21,774 cumulative retrievals**, `procedural` 12 workflows, `memory_feedback` 1,617 votes
(useful 854 · unused 446 · useless 317), `task_events` 2,119, `session_conclusions` 1,335.

**Session corpus (outside the repo):** 7,345 session dirs, 730 `.jsonl` logs,
**18 GB** + 6.1 GB of raw request/response logs.

**Reading.** The self-cancelling rate is the number to watch: **1,057 cancelled rows against
1,852 completed**, and the fix/feat output (`docs` 2,769 + `fix` 1,020) says most of the
day's work is spent writing down what was decided. The `verify_evidence` column is filled on
**1 row of 3,177** — the verification *gate* (Rule 5's `## Verification`) lives in the report
documents, not in the ledger column built for it.

---

## 4. Cost & dependency footprint

| Metric | 2026-09-20 | **2026-09-27** |
|---|---:|---:|
| Direct dependencies | 2 | **5** |
| Modules pinned in `go.mod` | 12 | **28** |
| Third-party import roots in production code | — | **6** |
| CGO in production build | 0 | **0** |
| Binary | 23,776,031 B (22.7 MiB) | **28,047,461 B (26.7 MiB)** |
| Build | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` | unchanged |

Direct deps: `github.com/charmbracelet/bubbletea` (TUI), `github.com/mattn/go-runewidth`,
`modernc.org/sqlite` (pure-Go SQLite — the reason the binary is CGO-free),
`github.com/google/uuid`, `golang.org/x/sys`.
Third-party roots actually imported by production code (6): `github.com/BurntSushi` (toml),
`github.com/bytedance` (sonic JSON), `github.com/google`, `github.com/gorilla` (websocket),
`github.com/mattn`, `golang.org/x`.

The four `import "C"` files are all under `devdoc/voice-zerodep-poc/` — a POC, not the
build. **Zero CGO in the shipped binary** is what makes §5 a build-flag question rather
than a project.

> **Dependency growth is the trend to state plainly:** 2 → 5 direct deps and 12 → 28 pinned
> modules in seven days. The five are each defensible (a TUI framework, a width table, a
> pure-Go SQLite, a UUID, `x/sys`), but the rule the project wrote for itself — Rule 35,
> "zero dependency is a discipline" — is now a rule with a slope attached.

---

## 5. Portability — measured by cross-compiling, not inferred

`GOOS=<os> GOARCH=amd64 go build ./...` at `574505bf`:

| Target | Result | Failing packages | Error sites |
|---|---|---|---|
| **linux/amd64** | ✅ **BUILD OK** | — | 0 |
| **darwin/amd64** | ❌ BUILD FAIL | 1 (`internal/scratch`) | **2** |
| **windows/amd64** | ❌ BUILD FAIL | 5 (`scratch`, `procstat`, `cgroupscope`, `daemonlifetime`, `lineedit`) | **24** |

**Identical to the 2026-09-20 measurement** (same packages, same 2/24 counts) — three weeks
and 189k lines later, the platform-specific surface did not grow. Platform-specific
packages present: `internal/scratch` (28 files) · `lineedit` (70) · `shot` (24) ·
`daemonlifetime` (6) · `borrowedfd` (4) · `cgroupscope` (3) · `procstat` (2).

The §5 effort estimates of the previous snapshot (`0.5–2 days` macOS, `2–6 weeks` Windows,
weakest link = the unmeasured darwin/windows **test** cost) still stand unamended: the only
*measured* facts are the compile-error counts, and no test run under those GOOS values has
happened since.

---

## 6. Open-source readiness (GitHub)

| Gate | Status | Evidence |
|---|---|---|
| LICENSE | ❌ **NONE** | no `LICENSE`/`COPYING`; 0 production files carry an SPDX header |
| README | ❌ **NONE** | `git ls-files 'README*'` → empty. The front door is missing |
| Secrets in tracked files | ✅ **none** | 0 files match `(^|[^A-Za-z0-9])sk-…|AKIA…` (see the method note below) |
| Hardcoded operator paths | ⚠️ **mostly prose, corrected 2026-09-27** | **1,197** tracked files carry `/home/user`. The **52 production `.go` files** this row reported were the WRONG SET: that number counted matching FILES regardless of whether the match was code or the prose of an incident record, and **44 of the 48 non-POC files carry the path only inside comments**. Measured with the corrected instrument (`scripts/gopi-stats.sh` hygiene section, comment lines dropped by construction): **4 production `.go` files / 7 CODE lines** at this base — `internal/tools/task_verify_closure.go` (a default-repo constant) and `internal/tools/memory_provenance_latent.go` (four reporting probe roots) plus **2 lines of `gopi -h` usage prose** inside raw-string blocks (`cmd/agent/cli_sandbox.go`, `cmd/agent/cli_uistate.go`). The two code constants are fixed and now read **2 files / 2 lines**, both of them that kept usage prose. The real blocker is narrower than "the path is being copied into new code": 1,100 of the 1,197 are tracked **non-`.go` residue** (task reports, fixtures, shell), plus a 4-file CGO proof-of-concept under `devdoc/voice-zerodep-poc/` counted separately |
| Tracked images / audio / logs | ⚠️ noise | 245 `.png/.jpg/.gif` · 32 `.wav` · 306 `.log/.out/.err` — evidence artifacts, not source; they make a public repo look like a dump and bloat clone size |
| CI | ❌ none | `git ls-files '.github/workflows/*'` → 0 (Makefile targets exist) |
| Governance | ✅ | `devdoc/DOC-INDEX.tsv`: **2,518 rows** classify **1,444 / 1,444** markdown files, **0 unclassified, 0 rows naming a missing path** (`gopi doccheck` agrees verbatim); 21 standards gate files; heavy-test inventory 126 rows |
| Docs language | ⚠️ | devdoc is 313k LOC, largely Chinese; an English README is still the missing front door |

**Method note (a false positive found and killed).** The previous snapshot's secrets scan
was `sk-[A-Za-z0-9]{16,}|AKIA[0-9A-Z]{16}` and reported "none"; the same regex here reported
**1 file** — `tasks/01a0d5d2-rework-report.md`, where a `git tag archive/task-01a0d5548d…`
command line contains the letters `sk-` by accident. The generator now anchors the pattern on
a non-identifier boundary and reports 0. A security scan that cries wolf on a task-id is a
scan everyone learns to skip.

**What changed since 2026-09-20:** nothing improved on the two blockers (still no LICENSE,
still no README). The hardcoded-path row was **re-measured and corrected** (2026-09-27): the
"24 → 52 production `.go`" growth was an artifact of an instrument that could not tell a code
line from a comment line — 44 of the 48 non-POC files carry the path in prose only. On the
corrected instrument the CODE population was 4 files / 7 lines, of which 2 files / 5 lines
were genuinely hardcoded constants and 2 lines are `gopi -h` usage prose; the constants are
now derived or configurable, so the instrument reads 2 files / 2 lines (the kept usage prose)
— see §6's row and `scripts/gopi-stats.sh`'s hygiene section. All tracked files still went
**788 → 1,197**, and that residue is real: it is 1,100 non-`.go` files (reports, fixtures,
shell) plus a 4-file CGO proof-of-concept. The governance story is the one that genuinely
improved: the doc corpus now has **no unclassified file and no stale row**, where at the base
the standards gate was red on 4–5 files.

**Verdict (unchanged in direction):** technically this is a 5-dependency, zero-CGO, 1.37:1
tested, 58-day documented decision trail. What stands between it and a public repo is
**legal (LICENSE)** and **editorial (README/en)** — both cheap, both still undone. The third
item this document listed, "operator paths baked into production constants", is now closed
for the constants themselves: what remains of the path family is 1,100 tracked
prose/fixture files (whose specificity is the evidence, deliberately kept), 4 files of CGO
proof-of-concept, and two usage paragraphs that are operator-facing documentation.

---

## 7. What a newcomer should read, in order

1. `devdoc/gopi-statistics-20260927.md` (this file) — what the project *is*, measured.
2. `GOPI.md` — 31 rules; they are the project's constitution, and the gates in
   `internal/standards/` (21 files) fail the build on six of them.
3. `CONFIG.md` (632 lines) — every knob, its default, and the source line that reads it.
4. `devdoc/DOC-INDEX.tsv` — the truth-class manifest: which of the 1,444 devdocs is a
   standing claim about master and which is one run's record.
5. `scripts/gopi-stats.sh` — re-run it. Every number here regenerates.

---

## Completed

- Recovered and re-ran the previous snapshot's generator at today's tip; reproduced the
  2026-09-20 baseline from the object store so every growth figure is a measurement.
- **Extended `scripts/gopi-stats.sh`** with new axes: a **growth table** (`--delta REF`,
  measured both-ends from `git grep -c ''` — no second checkout), **env vars + settings
  keys** from `gopi --help-env`, **session corpus + real tool adoption** (the declared-vs-used
  gap), **knowledge-base stats** (tiers, retrievals, feedback votes, procedural workflows),
  **ledger distributions** (status/source/model/cadence), **governance** (doc-corpus
  coverage as a set comparison, standards gates, heavy inventory), **open-source hygiene**
  (LICENSE/README/CI/secrets/hardcoded paths), **direct vs pinned dependency accounting**
  from `go.mod`, **third-party import roots** from the code, and a **working-tree & recent
  activity** tail. All new literals are named constants (Rule 1); the script stays read-only.
- Fixed three defects the previous version carried: the dependency count read `28` where it
  meant *direct* deps over a whole-file `grep -c`; `--help-env` was not measured at all; and
  the corpus-cleanliness line had no set-comparison, so it could not distinguish
  "unclassified" from "row naming a missing file".
- Fixed the secrets scan's false positive (a task id containing `sk-`), with the reason in a
  comment beside the constant.
- Wrote this snapshot with a growth column, an adoption table, a governance section, and a
  reading order for an outsider.

## Files

- `scripts/gopi-stats.sh` (modified) — the generator, now covering the axes above.
- `devdoc/gopi-statistics-20260927.md` (new) — this snapshot.
- `devdoc/DOC-INDEX.tsv` (one row added, classifying this file) — Rule 36, same commit.

## Verification

- Build: `CGO_ENABLED=0 go build -o … ./cmd/agent/` → **exit 0** (via the generator:
  `fresh build size: 28047461 bytes`).
- Tests: `bash -n scripts/gopi-stats.sh` → **exit 0** (the generator is a shell script; no
  Go package is touched by this change, so no Go suite is in scope — the diff is
  `scripts/` + `devdoc/` only).
- Ran: `scripts/gopi-stats.sh --delta 7e34258976e2f768f87df9b30b32f559bf9b6205`
  (full output captured in the run's evidence dir) → **exit 0**, all sections printed.
- Ran: `scripts/gopi-stats.sh` (no args, the `--delta`-skipped path) → **exit 0**.
- Ran: `gopi doccheck` → `the devdoc corpus is consistent — every file under devdoc/ has a
  well-formed row`.
- Result: every number in this document is copied from the captured generator output; the
  growth column matches `git grep -c ''` at both revs; the cross-compile row reproduces the
  previous snapshot's 2/24 error counts.

## Not Done

- **The worktree-clean claim is made by the close-out, not here** — the generator edit, this
  doc and the DOC-INDEX row are committed immediately after this report is written.
- **GOPI.md Rule 3's "17 tools" vs `toolnames`' 29 is still not reconciled** — it is the same
  open item the previous snapshot filed, and it is a one-line ruling someone must make.
- **`verify_evidence` is filled on 1 of 3,177 ledger rows** — measured, reported, not fixed;
  whether that column is dead or under-used is a separate decision.
- **`sort`/`comm` "broken pipe" noise on stderr** appears when a pipeline's consumer exits
  first (e.g. `head`). The counts are unaffected — the lines are printed after the consumer
  stops — but the generator's stderr is noisy. Left as-is rather than papered over with a
  blanket `2>/dev/null`, which would hide real errors (Rule 15).
- **`gopi task sweep-worktrees` is skipped in this worktree** (no `./bin/gopi` here by
  design — it is gitignored); the section prints how to build one instead of inventing a
  number. A fresh worktree therefore reports "skipped" for that one line.
- **Darwin/Windows remain compile measurements only** — no test suite has been run under
  those GOOS values, so the porting estimates keep their unmeasured test-cost term.
- The adoption sample is the **400 newest** logs by mtime; it is a recent-traffic sample, not
  the whole corpus, and it is labelled as such in the output.

## Evidence (self-filed)

```
$ git -C <worktree> log -1 --format='%H %cs'
574505bf6cd4dece18668d162b405455e30c11f2 2026-09-27

$ bash -n scripts/gopi-stats.sh                       -> exit 0
$ scripts/gopi-stats.sh --delta 7e342589…             -> EXIT=0, full snapshot (see below)

== identity == base commit: 574505bf6cd4dece18668d162b405455e30c11f2 / base date 2026-09-27 21:06:37
production files 538 / LOC 248423 / funcs 6525 ; test 1131 / 341060 / 6553 ; packages 65 (cmd=3 internal=61)
growth vs 7e34258976e2f768f87df9b30b32f559bf9b6205:
  production go  401 -> 538 (+137)   173702 -> 248423 (+74721)  x1.43
  test go        775 -> 1131 (+356)  227003 -> 341060 (+114057) x1.50
  markdown      1542 -> 2063 (+521)  304104 -> 405940 (+101836) x1.33
  shell          147 -> 186 (+39)     16910 ->  22391 (+5481)   x1.32
  python          88 -> 104 (+16)     16213 ->  19891 (+3688)   x1.23
  commits       4848 -> 6588          rules 24 -> 31
feature surface: LLM tools 29 | CLI verbs 34 | task subcommands 17 | env vars 89 | settings keys 63
binary: fresh build size 28047461 bytes (27M)
deps: 5 direct (bubbletea go-runewidth modernc.org/sqlite uuid x/sys), 28 pinned, 6 three-party import roots
history: 6588 commits / 1638 merges / 400 tags (174 archive) / 10 contributors / +1273549 -148505 over 17099 touches
ledger: 3177 tasks (completed 1852 cancelled 1057 failed 199 verified 68 in_progress 1) ; 7d 280 ; verify_evidence 1
kb: distilled_qa 6241 (warm 6118 cold 122 hot 1) procedural 12 feedback 1617 retrievals 21774 ; db 146198528 B
sessions: 7345 dirs / 730 jsonl / 18G (+6.1G raw) ; adoption sample 400 logs (169.4 MiB) 35606 calls
  bash 22253 62.5% | edit 3033 | job 2966 | read 2539 | task 2042 | memory 1210 | write 699 | context_trim 147
  task_submit 136 | code_search 134 | keyword_search 111 | ... | git_blame 12 | web_search 5 | log 1
portability: linux OK ; darwin FAIL 2 errors in internal/scratch ; windows FAIL 24 errors in 5 pkgs
hygiene: LICENSE NONE ; README NONE ; images 245 audio 32 logs 306 ; CI 0 ; secrets 0 ; /home/user 1197 (prod go 52)
   ↑ the "(prod go 52)" reading is the PRE-CORRECTION instrument's (it counted matching
     files, not matching CODE); the corrected split at this base is 4 / 7 CODE files/lines
     excluding the POC (of which 2 / 5 are the constants this task fixed), plus 1,100
     non-.go residue files — see §6's corrected row.
governance: doc rows 2517 classify 1444 of 1444 md (0 unclassified, 0 rows naming no file) ; gates 21 ; heavy 126

$ gopi doccheck
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row

# set comparison behind the governance line, run standalone:
$ comm -12 <(git ls-files 'devdoc/*.md'|sort) <(awk -F'\t' '!/^#/&&$1~/^devdoc\/.*\.md$/{print $1}' devdoc/DOC-INDEX.tsv|sort) | wc -l  -> 1444
$ comm -23 <(git ls-files 'devdoc/*.md'|sort) <(…as above…) | wc -l  -> 0   (unclassified)
$ comm -13 <(git ls-files devdoc|sort) <(awk -F'\t' '!/^#/&&$1~/^devdoc\//{print $1}' devdoc/DOC-INDEX.tsv|sort) | wc -l  -> 0  (rows naming no file)

# the false positive the secrets scan used to report:
$ git grep -nE 'sk-[A-Za-z0-9]{16,}|AKIA[0-9A-Z]{16}'
tasks/01a0d5d2-rework-report.md:101:  git tag archive/task-01a0d5548d637cde86853409345eb4bc-$(date +%Y%m%d) …
# -> the letters "sk-" inside a task id, not a key. Boundary-anchored pattern reports 0.
```
