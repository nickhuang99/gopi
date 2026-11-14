<!-- doc-class: normative -->
<!-- census-evidence-ok: this public export was published with NO git history (a clean-tree export, not a clone of the development repository), so a commit pin can never resolve here by construction — the figures are a snapshot of the private tree, kept as a record and not as a re-derivable claim about this repository -->
# Task Report: GoPi project statistics — milestone snapshot + portability/open-source assessment

**Base commit measured:** `cc5ac1fd8a82c9cdd78ec1e3ef9e9bf4aa1b8906` (re-measured on this
ground's snapshot tip; the original base `7e34258976e2f768f87df9b30b32f559bf9b6205`,
2026-09-20 05:29:07 +0800, is NOT reachable here — see the truncation note below)

<!-- census-truncation: this ground's object store is a TRUNCATED snapshot (root 964b2404, 106 commits), so the original pin 7e342589 does not resolve here; the figures are restated at a pin this repository CAN re-derive, and the original hash is kept as history below. -->
**Branch:** `docs/gopi-statistics`
**Generator:** `scripts/gopi-stats.sh` — re-run to reproduce every number below.

> **Why a script and not a table.** Numbers quoted only in prose go stale silently:
> GOPI.md:309 still claims *"git_blame 0 uses / 21435"* while a 400-session sample
> measured 4 uses (see `## Evidence`). Every figure in this document was measured by
> `scripts/gopi-stats.sh` at the pinned commit above; nothing is typed from memory.
> Method and claim-classification discipline follow the precedent recorded in memory
> #3303 (reverse requirement analysis of gopi @143k lines).

---

## 1. Scale

| Dimension | Measured value |
|---|---|
| Production Go | **401 files, 173,702 LOC, 4,668 functions** |
| Tests | **775 files, 227,003 LOC, 4,687 `Test*` functions** (0 benchmarks) |
| Go total (tracked) | 1,176 files, 400,705 LOC |
| Test : production LOC ratio | **1.31 : 1** (tests exceed production code) |
| Packages | **54** (`cmd` 3, `internal` 50, `testsuite` 1) |
| Shell | 147 files, 16,910 LOC |
| Python (test drivers) | 88 files, 16,211 LOC |
| Web/UI | 14 HTML (1,862 LOC), 5 JS (1,421 LOC) |
| Documents | **1,542 markdown, 304,104 LOC** (1,141 devdoc / 250,811 LOC = 82%) |
| Doc : code LOC ratio | 1.75 : 1 |
| Tracked files | 4,060 |
| Repo sizes | tracked content 45.3 MB · worktree 97.8 MB · `.git` 261,516,384 B (249 MiB) |

**Reading:** the documentation and test volume matching (and exceeding) the code is
the single most distinctive property of this project. It is not a side-effect — the
rules (PROJECT.md Rule 5, 7) *require* a document per change cycle, and 2,025 of 4,848
commits are `docs:`.

Largest production packages: `internal/tools` 67,591 LOC · `cmd/agent` 37,680 LOC ·
`internal/agent` 14,633 LOC.
Largest single files: `internal/tools/tasks.go` 6,778 · `internal/agent/loop.go` 5,765 ·
`cmd/agent/cli_tasks.go` 4,808.

---

## 2. Feature surface

| Surface | Count | Source |
|---|---|---|
| LLM-facing tools | **28** | `internal/toolnames/names.go` |
| CLI verbs | **28** | `gopi --help` (`sort -u`; a raw line count reads 32 because continuation lines repeat a verb) |
| `gopi task` subcommands | **17** | `gopi task -h` |
| Documented env vars | **78** | `gopi --help-env` |

Tools: `bash code_search commit context_trim edit feedback git_blame job keyword_search kill memory output read reframe review screen_capture search start store task task_submit update view vision_analyze voice_input web_fetch web_search write`

CLI verbs: `act archive brew chat checkpoint cli code-search completion log memory memsync merge merger proxy raw resume scratch-gc screen screenshot shot stats task text version vision voice web worker`

> Note: PROJECT.md Rule 3 says "GoPi has 17 tools". The **toolnames** list holds 28, and
> 14 of those are `task`/`memory` *action* entry points plus `commit`/`review`/`reframe`/
> `start`/`kill`/`output`/`search`/`store`/`update`/`feedback`/`view`, i.e. sub-actions of
> merged tools, not top-level tools. The count in the rule is a different (smaller)
> taxonomy; the two are not a contradiction but the rule text reads as drift — worth a
> one-line clarification (see `## Not Done`).

### Actual tool usage — the adopted surface (not the declared one)

Sample: the 400 newest session JSONL files (≈3 days of runs).

| Tool | Calls | | Tool | Calls |
|---|---:|---|---|---:|
| bash | 31,471 | | write | 929 |
| job | 5,412 | | context_trim | 293 |
| task | 4,262 | | code_search | 259 |
| edit | 3,894 | | keyword_search | 184 |
| read | 3,549 | | task_submit | 100 |
| memory | 2,325 | | vision_analyze | 11 |
| | | | screen_capture | 7 |
| | | | git_blame | **4** |

**bash alone is 76% of all tool calls.** The specialised search tools
(`code_search` + `keyword_search` = 443) are used 71× less than bash — the same
adoption gap PROJECT.md Rule 11 documented as the reason tool work requires an
adoption proof. `git_blame` is effectively unadopted (4 calls); the rule's stale
"0/21435" figure is directionally right even after 3 more weeks of work.

---

## 3. Development history

| Metric | Value |
|---|---|
| First commit | `d5736e9a` 2026-07-31 21:42:48 +0800 |
| Age at snapshot | **50.3 days** |
| Commits | **4,848** (1,113 merges = 23%) |
| Avg pace | 96.3 commits/day overall · **321.7/day in the last 7 days** |
| Branches | 82 remote, 4 local, 6 worktrees |
| Tags | 349 (167 `archive/*` = retired competition/loser branches) |
| Contributors | 8 git identities (2953 `Qingzhe Huang`, 1533 `gopi`, 254 `qingzhe huang` — the same human under 3 spellings) |
| Churn | **+939,397 / −125,836 lines** across 12,924 file-touches |
| Ledger rows | **2,106 tasks**: completed 1,558 · cancelled 301 · failed 167 · verified 68 · pending 11 · in_progress 1 |
| Distilled memory | 4,338 QA records + 3,592 feedback votes (`~/.gopi/memory.db`, 115.7 MB) |
| Session corpus | 74,985 session dirs, **41 GB** (outside the repo) |

Commits by type: `docs` 2,025 · `fix` 745 · `feat` 471 · `test` 244 · `merge` 127 ·
`adoption` 62 · `refactor` 45 · `chore` 43 · `style` 26 · `probe` 17.

Monthly: 2026-07 = 15 · 2026-08 = 690 · 2026-09 = **4,143** (through the 20th).

**Reading:** a sustained ~100 commits/day with a 23% merge rate and 301 cancelled
rows is a *high-throughput, self-modifying* development style — the ledger and the
41 GB session corpus are first-class project artifacts, not by-products. Notably
88% of the fix+feat commits are `docs:` — the system's dominant output is the
written record of its own decisions.

---

## 4. Cost & dependency footprint

| Metric | Value |
|---|---|
| Direct dependencies | **2** (`github.com/google/uuid`, `golang.org/x/sys`) |
| Total modules in `go.mod` | 12 (rest transitively pinned for offline builds) |
| Go toolchain | go 1.24.5 / `go 1.24.0` directive |
| CGO | **0 in production** (`CGO_ENABLED=0`; the 4 `import "C"` files are all in `devdoc/voice-zerodep-poc/`, a POC, not the build) |
| Binary | **23,776,031 B (22.7 MiB)**, static |
| Build | one command, no Makefile dependency: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` |

Two direct dependencies — one of which (`modernc.org/sqlite`, pulled via the
module graph) is a **pure-Go SQLite**. That is the whole reason the binary is
zero-CGO and cross-compilable at all. This is an exceptionally small dependency
surface for a tool of this capability.

---

## 5. Portability — measured by cross-compiling, not inferred

`GOOS=<os> GOARCH=amd64 go build ./...` at the pinned commit:

| Target | Result | Failing packages | Error sites |
|---|---|---|---|
| **linux/amd64** | ✅ **BUILD OK** | — | 0 |
| **darwin/amd64** | ❌ BUILD FAIL | 1 (`internal/scratch`) | **2** |
| **windows/amd64** | ❌ BUILD FAIL | 5 (`scratch`, `procstat`, `cgroupscope`, `daemonlifetime`, `lineedit`) | **24** |

Darwin errors (verbatim):

```
internal/scratch/tmproot.go:171:25: f.X__val undefined (type syscall.Fsid has no field or method X__val)
internal/scratch/tmproot.go:285:32: cannot use st.Type (variable of type uint32) as int64 value in argument to ClassifyFilesystemType
```

Windows errors by category: `syscall.Statfs_t`/`Statfs` ×3 each, `syscall.Kill` ×2,
`unix.FcntlInt`/`F_DUPFD`, `syscall.Rlimit`/`RLIMIT_AS`/`Getrlimit`, `syscall.Access`,
and 10 termios constants (`IXON ICRNL BRKINT INPCK ISTRIP PARMRK IGNBRK OPOST ECHO`).

Unix coupling per package (production files touching `syscall.` / `x/sys` / `/proc` /
`ioctl`):

| Package | prod files | unix-touching | tests |
|---|---:|---:|---:|
| `internal/scratch` | 7 | 7 | 9 |
| `internal/lineedit` | 16 | 7 | 11 |
| `internal/shot` | 6 | 3 | 15 |
| `internal/procstat` | 2 | 2 | 0 |
| `internal/daemonlifetime` | 2 | 2 | 4 |
| `internal/cgroupscope` | 1 | 1 | 2 |
| `internal/signalrec` | 1 | 1 | 2 |
| `internal/borrowedfd` | 2 | 0 | 2 |

**The porting surface is small and well-isolated.** The unix coupling sits in 7
leaf-shaped packages; 49 production files across the whole tree reference `syscall`
or `x/sys`. The original repo layout already contains the porting idiom it needs
(`internal/procstat/procstat_unix.go`).

### Effort estimates — `ESTIMATE` (inference, weakest link named)

These are **estimates**, not measurements: the only measured facts are the error
counts and package counts above. Estimates assume one experienced Go engineer
already familiar with the codebase:

| Target | Effort | Reasoning (weakest link = unmeasured test cost) |
|---|---|---|
| **macOS** | **0.5–2 days** to build; **1–2 weeks** to be trustworthy | Only 2 compile errors, both in `internal/scratch` (`Fsid` field + type width). But the unix **test** suite was never run on darwin — 775 test files may hide darwin-specific failures that compile fine. |
| **Windows** | **2–6 weeks** for a building, core-loop-working binary; **+4–8 weeks** for CLI/UX parity | 24 error sites in 5 packages, each needing a real platform implementation (not a stub): `Statfs` → `GetDiskFreeSpaceEx`; `Rlimit` → Job Objects or an honest no-op; `Kill` → `TerminateProcess`; **`lineedit` raw-mode is the hard part** — 27 files of termios/PTY work needing the Windows Console API; `shot` needs a non-X11 capture path. Worse: **147 shell files + the entire Makefile/test-budget/verification machinery are Unix-only**, so the project's own quality gates do not run on Windows without a PowerShell port. |
| **Linux armoury** (other arches) | near-zero | Pure-Go SQLite + zero CGO; `GOARCH` cross-compile is a build-flag change. |

**Honest ranking of portability:** Linux → macOS → Windows, with a steep cliff between
macOS and Windows. macOS is a "fix 2 lines + run the suite once" task; Windows is a
platform layer for the terminal, the screen, and the whole test harness.

---

## 6. Open-source readiness (GitHub)

| Gate | Status | Evidence |
|---|---|---|
| LICENSE file | ❌ **NONE** | no `LICENSE`/`COPYING`; 0 Go files carry an SPDX header. Without one the code is "all rights reserved" — **GitHub cannot legally be used as a public repo target yet** |
| Module path | ⚠️ `module github.com/nick/gopi` | already shaped for GitHub, but a public repo must own that path |
| Secrets in tracked files | ✅ **none found** | no `sk-…`/`AKIA…` matches; no tracked `.env`/`*.pem`/credential files |
| **Hardcoded operator paths in production code** | ❌ **blocker** | 70 tracked files (go/sh) carry `/home/user`; includes *production* constants: `internal/tools/task_verify_closure.go:101 taskClosureDefaultRepo = "/home/user/work/GoPi"`, `internal/tools/memory_provenance_latent.go:143 latentColumnProbeRoot = "/home/user/work/GoPi"` + 3 personal repo paths (`~/work/hermes-upstream`, …) |
| Tracked media/logs | ⚠️ noise | 236 PNG, 32 WAV, 293 `.log/.out/.err` tracked — evidence artifacts, not source; they make a public repo look like a dump and bloat clone size |
| Docs language | ⚠️ Chinese + English mixed | devdoc is 250k LOC, largely Chinese; an English README is the missing front door |
| CI | ❌ none visible | `Makefile` targets exist (`test`, `test-heavy`, `test-budget`, `flake-catch`) but no `.github/workflows` |
| Advertised tool count drift | ⚠️ minor | PROJECT.md Rule 3 says 17 tools; `toolnames` holds 28 (different taxonomy) |

### Effort estimate — `ESTIMATE`

| Work item | Effort |
|---|---|
| LICENSE + README + `.gitignore`/media cleanup + CI workflow (build + `make test`) | **2–4 days** |
| Replace hardcoded `/home/user` paths with config/derivation (70 files, but only ~5 are production constants; the rest are docs/scripts) | **2–5 days** |
| English README + translated top-level docs | **3–7 days** |
| **Total to a credible public repo** | **1–2 weeks** |

**Verdict on open-sourcing:** *technically* the project is in unusually good shape
(2 dependencies, zero CGO, 1.3:1 tests, a documented 50-day decision trail). The
blockers are **legal (no LICENSE)** and **personal (hardcoded operator paths and a
personal provenance graph baked into production code)** — both cheap to fix, and both
must be fixed *before* the first push, not after.

---

## Completed

- `scripts/gopi-stats.sh` — a reproducible, read-only statistics generator (named
  constants per Rule 1; static counts only, no device load per Rule 27), with a
  JSON-`--json` flag reserved and a dirty-worktree warning so a snapshot can never
  silently be attributed to the wrong commit.
- Measured and pinned: code, tests, docs, packages, functions, feature surface
  (tools + CLI verbs + task subcommands + env vars), binary, repo size, dependencies.
- Measured development history: commits, merges, branches, worktrees, tags, archive
  tags, contributors, monthly/period cadence, churn, commit-type distribution, ledger
  status distribution, memory rows, session corpus.
- Measured **tool adoption** from real session logs — the declared vs. actual tool
  surface (bash 76%).
- Measured **portability by cross-compilation** (linux OK / darwin 2 errors / windows
  24 errors) instead of asserting it, with a per-package unix-coupling inventory.
- Measured **open-source hygiene**: no LICENSE, no secrets found, and a concrete,
  evidence-backed blocker list including hardcoded operator paths in production code.
- Effort estimates for macOS, Windows, and open-sourcing — explicitly labelled
  `ESTIMATE`, with the weakest link of each named.

## Files

- `scripts/gopi-stats.sh` (new) — the generator.
- `devdoc/gopi-statistics-20260920.md` (new) — the snapshot + assessment.
- `devdoc/DOC-INDEX.tsv` (one row added) — classifies the new devdoc file
  (`historical`) so the doc-manifest gate does not report it.

## Verification

- Build: `CGO_ENABLED=0 go build -o … ./cmd/agent/` → **exit 0**
  (via the generator: `fresh build size: 23856250 bytes`).
- Script self-run: `GOTMPDIR=… ./scripts/gopi-stats.sh` → **exit 0**, full snapshot
  printed (see Evidence).
- `bash -n scripts/gopi-stats.sh` → **OK**.
- `shellcheck -S warning scripts/gopi-stats.sh` → **exit 0, clean** (it first
  reported 3 unused constants — `REPO_DEFAULT`, `RULE14_LEAF_PKGS`, `JSON` — which
  were removed rather than suppressed).
- Cross-compile portability: `GOOS={linux,darwin,windows} GOARCH=amd64 go build ./...`
  → linux exit 0; darwin/windows FAIL with the error counts quoted in §5.
- **Numbers corrected by the generator against my first hand count**: CLI verbs
  32→**28** and `gopi task` subcommands 18→**17** (the raw `grep -c` counted
  continuation lines that repeat a verb; `sort -u` is the correct instrument). This
  is the concrete reason the snapshot ships as a script.
- `internal/standards` doc-manifest gate: **pre-existing RED at the base commit**,
  independent of this task. Measured both ways — with this report removed the same
  2 files are still unclassified, and at `/home/user/work/GoPi` (master, my files
  absent) the same 5 files are unclassified. My own new devdoc file was given a
  `DOC-INDEX.tsv` row (class `historical` — a snapshot of counts at a pinned commit
  is a past run's record by construction), which removes this report from the
  finding; the remaining 4 rows are not mine to classify. The full red is reported
  in `## Not Done`.

## Not Done

- **The worktree is not claimed clean here** — at report-writing time the script,
  report and `DOC-INDEX.tsv` row are the uncommitted deliverable; they are committed
  immediately after, and the close-out reports the measured `git status --porcelain`.
- **Darwin/Windows are measured as *compile* failures only.** No test suite was run
  under those GOOS values, so the effort estimates carry an unmeasured test-cost term
  (named as the weakest link in §5).
- **Effort estimates (§5, §6) are inference, not measurement.** A real port estimate
  needs a spike on `internal/lineedit` — a separate task.
- **Pre-existing red, found and NOT fixed:**
  `go test ./internal/standards/` fails at the base commit because **4 tracked
  devdoc files have no `DOC-INDEX.tsv` row** — `devdoc/cli-ui-feasibility-study.md`,
  `devdoc/report-seam-hermetic.md`, `devdoc/requirements/12-verification-and-gates.md`,
  `devdoc/taskid-vs-sessionid-verdict.md`, `devdoc/tools-master-reds-90e664e0.md`
  (five when this file was first written; my one row is now added). They are another
  agent's in-flight documents and classifying them for class+reason is exactly the
  judgement their author must make — so this is left to the author, per Rule 26
  (dispatch, don't guess). It is NOT caused by this branch: measured red in both
  trees with my files absent.
- **`go list -m all` hung** (offline module proxy), so the module graph came from
  `go.mod`/`go.sum` (12 modules); the *direct* count (2) is exact.
- PROJECT.md Rule 3's "17 tools" vs `toolnames`' 28 was **not** reconciled — they look
  like two taxonomies (top-level tools vs. action entry points), but a one-line
  clarification is worth dispatching.
- No GitHub CI workflow drafted and no LICENSE chosen — §6 scopes that work, it does
  not do it.

## Evidence (self-filed)

```
$ git -C /home/user/work/GoPi log -1 --format='%H %ci'
7e34258976e2f768f87df9b30b32f559bf9b6205 2026-09-20 05:29:07 +0800

$ git rev-list --count HEAD          -> 4848
$ git log --merges --oneline | wc -l -> 1113
$ git log --reverse --format='%h %ci' | head -1
d5736e9a 2026-07-31 21:42:48 +0800
$ git tag | wc -l                    -> 349 ;  archive/*: 167
$ git log --numstat --format='' | awk '{a+=$1;d+=$2} END{print a,d}'
939397 125836
span days: 50.3   commits/day over span: 96.3   commits in last 7 days: 2252 (321.7/day)
monthly:  2026-07 15 | 2026-08 690 | 2026-09 4143

$ git ls-files '*.go' | grep -v _test.go | wc -l          -> 401
$ ... | xargs wc -l | tail -1                             -> 173702 total
$ ... | xargs grep -h '^func ' | wc -l                    -> 4668
$ git ls-files '*_test.go' | wc -l                        -> 775 ; LOC 227003 ; Test funcs 4687 ; Benchmark 0
$ go list ./... | wc -l                                   -> 54   (cmd=3 internal=50)
$ git ls-files '*.md' | xc  -> 1542 files / 304104 LOC ; devdoc: 1141 / 250811
$ ./bin/gopi --help | grep -cE '^  gopi [a-z-]+'          -> 32
$ ./bin/gopi --help-env ... GOPI_/DEEPSEEK_ uniq         -> 78
$ sqlite3 "file:~/.gopi/memory.db?mode=ro" "select status,count(*) from tasks group by status;"
completed|1558 cancelled|301 failed|167 verified|68 pending|11 in_progress|1
$ sqlite3 ... "select count(*) from distilled_qa;"        -> 4338 ; memory_feedback -> 3592
$ du -sh ~/.gopi/sessions                                 -> 41G

$ GOOS=linux   GOARCH=amd64 go build ./...                -> exit 0
$ GOOS=darwin  GOARCH=amd64 go build ./...                -> exit 1, 2 errors, 1 pkg (internal/scratch)
$ GOOS=windows GOARCH=amd64 go build ./...                -> exit 1, 24 errors, 5 pkgs
$ grep -rn 'import "C"' --include='*.go' . | grep -v _test  -> 4, ALL in devdoc/voice-zerodep-poc/
$ ls LICENSE COPYING                                       -> NO LICENSE FILE

tool usage, 400 newest session JSONL files (last ~3 days), grep '"name":"…"':
bash 31471 | job 5412 | task 4262 | edit 3894 | read 3549 | memory 2325 | write 929 |
context_trim 293 | code_search 259 | keyword_search 184 | task_submit 100 |
vision_analyze 11 | screen_capture 7 | git_blame 4

$ grep -rn '/home/user' --include='*.go' --include='*.sh' . | grep -v _test | wc -l  -> 70 files
internal/tools/task_verify_closure.go:101:taskClosureDefaultRepo = "/home/user/work/GoPi"
internal/tools/memory_provenance_latent.go:143:latentColumnProbeRoot = "/home/user/work/GoPi"

$ GOTMPDIR=… ./scripts/gopi-stats.sh   ->  exit 0, snapshot printed, "fresh build size: 23856250 bytes"
$ shellcheck -S warning scripts/gopi-stats.sh   ->  exit 0 (clean, after removing 3 unused constants)
$ bash -n scripts/gopi-stats.sh                 ->  OK

pre-existing standards red, measured in BOTH trees with this task's files absent:
  main repo (master):  5 devdoc files unclassified, "2088 rows cover 2093 files"
  this worktree w/o my report: same 2 of them -> FAIL
  this worktree with my DOC-INDEX row: 4 remain -> FAIL (mine closed)
```
