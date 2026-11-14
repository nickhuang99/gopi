<!-- doc-class: normative -->
# Task Report: repo workspace hygiene — root report docs, the tracked/untracked split, and the "agent binary in git" premise

**Branch**: `chore/workspace-hygiene` (worktree `~/workspace/gopi-ws-hygiene`)
**Commit**: `83351795` → `6724c125` (4 commits) on top of `bf471665`
**Durability**: this file is a **kept design narrative** (it states a standing
rule others must follow), not a throwaway report — that is why it is a `devdoc/`
and not `reports/archive/<month>/`. See `reports/README.md` for the split.

---

## Completed

- **37 report `.md` files moved out of the repo root** (33 → `reports/archive/2026-08/`,
  4 → `reports/archive/2026-09/`, bucketed by the month they were written).
  `GOPI.md` and `CHANGELOG.md` stay: the first is read from the root at startup
  (`internal/agent/loop.go` `os.ReadFile("GOPI.md")`), the second is the release
  surface.
- **`text2img-ab.py` → `text2img-eval/`** — its only caller
  (`text2img-eval/TEXT2IMG-EVAL-codex.md`) already documented it as living there.
- **15 citing documents repaired** (12 `devdoc/`, 1 `tasks/`, 1 `briefings/`,
  1 `text2img-eval/`) to the new paths, plus one stale-prose fix in
  `devdoc/review-cli-search.md` that said "(repo root)".
- **`reports/README.md`** — the standing rule: a snapshot goes to
  `reports/archive/<YYYY-MM>/`, a narrative future agents must read goes to
  `devdoc/`.
- **`briefings/README.md`** — why that tree is deliberately untracked and where
  a brief's content belongs instead.
- **Gate + .gitignore ratchet**: `internal/standards` now refuses a tracked root
  `*.md` outside `rootDocAllowlist` and a tracked root build output
  (`agent`/`gopi`); `.gitignore` gained `/*.md` + `!/GOPI.md` + `!/CHANGELOG.md`
  so a new root report appears as `??` instead of sliding into the next commit.
- **A real bug in the existing gate helper fixed**: `ignoredPaths` treated *any*
  `git check-ignore -v` hit as "ignored", but that output includes the matching
  **negation** pattern. Before the fix a `!/GOPI.md` rule would have read as
  "GOPI.md is ignored" — the exact inverse. The negation fixtures are what
  exposed it.

## Answers to the briefing

### Are the root reports best kept in the root? What are they?

No. All 39 root `*.md` were **point-in-time task reports**, not living docs, and
no program reads one. The root's markdown surface should be exactly two files:
the agent's rule file and the changelog.

### Why were so many saved there — and is it hermes?

- **37 of 39 were committed by the human operator's own hand-run sessions**
  (`Qingzhe Huang <user@example.com>`, 2026-08-22 .. 2026-09-01). They are
  the pre-gopi era: e.g. `61fe9d09` "Round 3 report" carries
  `Co-Authored-By: Claude`. Each task ended by writing its report at the repo
  root — a habit, not a tool.
- **2 are gopi's**: `report-websearch-quality-fix.md` (`004bfa50`) and
  `report-memory.md` (`40ebdd91`), produced by the report path the harness
  exports (`GOPI_TASK_REPORT_PATH`) with a root-relative choice.
- **It is NOT hermes.** `hermes` is the PA role, which dispatches and never
  writes the development repo; `briefings/` authorship is gopi's.
- **The practice is already dead**: the last root report is 2026-09-12 and the
  live convention is `devdoc/` (643 docs there vs 0 new root reports). The 2026-09
  repo-hygiene task (`devdoc/repo-hygiene-tracked-scratch.md`) explicitly listed
  "remaining root clutter (report .md files)" as **out of its scope** — this task
  is that deferred half.

### Why do we keep a binary `agent` in git?

**We do not, and we never did — the briefing's premise is false.** Measured:
`git log --all -- agent` → 0 commits; `git ls-files agent` → 0 paths;
`.gitignore:6` already had `/agent`. What exists is a **19 MB untracked build
debris** at the root (`/home/user/work/GoPi/agent`, 2026-09-10 07:04), and it is
there because the project's own documented verification recipe —
`CGO_ENABLED=0 go build ./cmd/agent/` with **no `-o`** — writes `./agent` when
run from the repo root (a single main package). **Reproduced directly this
round**: `CGO_ENABLED=0 go build ./cmd/agent/` in the worktree root → exit 0 and
a fresh 21 MB `./agent`; the same command with `-o bin/gopi` (the `Makefile`
recipe) writes no root file. `make build` is correct; the *no-`-o` habit* in the
verification recipes (`devdoc/bug030-cli-shortcut.md`,
`devdoc/merger-service-restore-20260912.md`, …) is what keeps re-creating it. It is referenced by nothing, no
live process runs it, and `bin/gopi` (the declared build output) supersedes it.
The **tracked** files are text only; the largest tracked files are PNG/JPEG
evidence under `devdoc/` (≤1.6 MB). The gate now refuses a tracked root
`agent`/`gopi` so the debris can never be committed by accident.

### Why are some files tracked and some not?

The repo has **0 modified tracked files** and 32 untracked paths, all of them
working-tree artefacts of in-flight or abandoned work — the split is
"deliverable vs envelope":

| Untracked set | Count | Status / proper place |
|---|---|---|
| `briefings/*.md` | 23 of 47 | Dispatch **envelopes** (per-run stamped, near-duplicates). Tooling already treats `briefings/` as untracked scratch (`internal/tools/rebuild.go:238`, `cmd/agent/cli_tasks_rebuild.go:23`). Policy documented; no action. |
| `devdoc/tab-order-actuate-poc.md` + `devdoc/tab-order-poc/` (5 files) | 6 | The **island case**: deliverables of the UNMERGED branch `feat/tab-order-actuate-poc` (`f17d3e37`). The branch holds them plus 4 files the working tree lacks, and the local doc is **newer** (`runC` added). Neither copy is complete — left untouched on purpose: the branch's own verify/merge round owns it. |
| `devdoc/verify-worker-writeback-01a09435.md` | 1 | Deliverable of the unmerged `verify/worker-writeback-01a09435` (`a8dd161a`) — **byte-identical** (sha256 `47107b7a…`), so nothing is at risk; left for that branch's merge. |
| `devdoc/verify-rebuild-guard-tracked-only.md` | 1 | No owning branch found; sole copy. Kept (not deleted — it is not mine to discard). |
| root `agent` (19 MB) | — | Build debris; **removed 2026-09-12** (`rm ~/work/GoPi/agent`) after re-verifying it was untracked, unreferenced and not running. The task worktree never had it (branch-made). |

## Not Done / deliberately out of scope

- ~~The 19 MB root `agent` in the main repo is NOT deleted by me.~~ **DONE
  2026-09-12 (follow-up round): `rm ~/work/GoPi/agent`, verified — the path is
  gone, `git status` in the main repo stays clean (it was never tracked), and
  the gate now refuses a re-created one.** The earlier entry explained why the
  worktree round did not do it (a worktree cannot dispose of a sibling
  checkout's untracked file, and `rm` into another agent's working tree is the
  interference Rule 6 forbids). Preconditions were re-checked before removal,
  not assumed: `git log --all -- agent` → 0 commits, `git ls-files agent` → 0
  paths, no live process had it open, and no script/config referenced it. Its
  sha256 `d9cf657a…` differed from the declared build output `bin/gopi`
  (`979ebc19…`), i.e. it was a stale binary, not an alias.
- **No history rewrite.** The 37 docs remain in history at their old root paths;
  the move is a forward rename (`R` entries). Rewriting shared history was out
  of scope (same call the 2026-09 hygiene task made).
- **`devdoc/` at 643 `.md` / 16 MB** is a real second-order volume problem, but
  `devdoc/` is the *correct* home; consolidating it is a different epic.
- **The 11 pre-existing gate failures** on this baseline (1 Rule-17 `os.Setenv`
  violation, 3 `internal/testutil` gate failures, GLM-night-watch sandbox) were
  **not** fixed — they are pre-existing and unrelated, proven below.

## Memory distillation — do the archived reports still carry lessons?

Question asked by the briefing: are these reports worth distilling into gopi memory?
Measured answer, not an opinion:

1. **The lessons are already in memory.** The archived reports are the *era* in
   which gopi built its memory subsystem, and each of them ended by storing what
   it learned. A targeted `memory(action=search)` for the reports' own subjects
   (context_trim / thinking-strip / memory_feedback / search-quality) returned
   the distilled form of the same facts — e.g. record `#896` ("context_trim ‑
   memory_feedback ‑ clock: implemented or just prompt prose?" → *all three are
   implemented and tested on master*, with the two mechanisms deliberately NOT
   conflated: `context_trim` removes a tool **pair**, thinking-strip is a
   separate `WireMessages(keepThinking)` path), and `#1073` (the dead-end nudge
   false-positive on content-returning tools). Verdict: **`useful`**, recorded
   via `memory(action=feedback)` — a duplicate store would only dilute the base.
2. **The reports are snapshots; the durable half already lives elsewhere.** A
   report's *finding* is a snapshot (date-stamped, superseded by the next round);
   its *narrative* is what a future agent must read, and that was already moved to
   `devdoc/` by the practice that replaced the root-doc habit. Re-storing report
   prose would be storing raw logs — exactly what the store contract forbids.
3. **What IS worth distilling is the new lesson of THIS task** (not a report):
   the root-doc drift mechanism and its ratchet. Stored as its own record — see
   the verification block.
4. **But the archived reports are not worthless.** Four of them are the *sole*
   written account of a design decision the code still lives with
   (`WEBSEARCH-CLI` tool→CLI mirroring, `WEBSEARCH-AUDIT`'s live-network script
   audit, `MEMORY-CLI` debug-interface decision, `PYTHON-KERNEL-RESEARCH`'s
   rejected-alternative study). That is precisely why the move was
   `git mv`→`reports/archive/<month>/` and **not** deletion: archive, never
   discard — the reason is recorded in `reports/README.md`.

## Workspace-level findings (measured beyond the repo)

The briefing said "gopi development workspace". The repo is clean now; the
surrounding machine is not:

| Finding | Measured | Assessment |
|---|---|---|
| `~/workspace` total | **16 GB** | Mostly churn, not deliverables. |
| Degenerate worktree dirs | **21 dirs** whose `.git` link is gone (dir exists, no gitdir) — e.g. `gopi-chat-web-align`, `gopi-reframe-stats`, `gopi-claim-e2e` | `git worktree prune -n -v` reports **nothing** because these were never registered worktrees — they are leftovers of a *clone*-based (or manually-copied) workflow (`gopi-prov-clone`, `gopi-latprov*` carry full 39-doc root trees = old checkouts). Safe to sweep, but it is an operator decision, not mine: several are < 2 days touched and one agent's scratch convention may still point there. **Not removed.** |
| `~/workspace/gopi-go-tmp` | **9.5 GB**, 237 `go-build*` dirs | **LIVE — must not be deleted.** It is the configured `GOTMPDIR` (`go env GOTMPDIR` → this path; `~/.config/go/env`) and is *actively* in use by other agents' test runs. It is the intended target of the scratch-GC watchdog (`crontab` */30 `gopi-scratch-gc-watchdog.sh`) — the GC's `home` resolves to `/dev/shm/gopi-test-scratch`, i.e. the missing piece is the watchdog's **scope**, not the tree's right to exist. Recorded as a finding; changing a shared env var or GC scope from a hygiene branch would be out of scope. |
| `/tmp` | **9.9 GB**; root fs **90% full (15 GB free)** | Out-of-repo scratch from other sessions (incl. many `/tmp/wta-*` logs). Not touched. |

## Verification

Commands run in `~/workspace/gopi-ws-hygiene` on `chore/workspace-hygiene`.

- **Build**: `CGO_ENABLED=0 go build ./...` → OK (exit 0), before and after.
- **Pre-existing-failure baseline** (clean detached worktree `/tmp/hyg-baseline`
  @ `bf471665`): `go test ./internal/standards/ ./internal/testutil/` → same
  failing tests as my branch (`TestStandardGates` Rule 17 NEW at
  `cmd/agent/cli_voice_review_test.go:1301` + `TestStandardGateDataReport`
  Rule 20 `internal/shot/x11.go:513`, and `internal/testutil`'s
  `TestGlmNightWatchOperatorPathStartsWorkersUnattended`,
  `TestScriptsAreBoundedAndOffTmp`, `TestWorkerOwnershipPointerPresent`). **My
  branch adds zero new failures.**
- **Worker-ownership surface unchanged**: the gate's listed file set diffed
  baseline vs branch → **identical** (21 vs 21 paths, `diff` empty), i.e. adding
  `reports/README.md` and `briefings/README.md` does not add an un-annotated
  launch-shape doc.
- **Targeted gate**: `go test -run 'TestRepoRoot' ./internal/standards/` → **ok**
  (all five: `TestRepoRootHasNoTrackedJunk`, `…JunkGateRedProof`,
  `…GateScansTheRealIndex`, `…JunkPathsAreGitignored`, `…GateScansImmediately`);
  `gofmt -l internal/standards/` → clean.
- **Red-proof (predicate)**: `TestRepoRootJunkGateRedProof` now carries 19
  poisoned paths (incl. `report-r3.md`, `FINAL-VERIFICATION-REPORT.md`,
  `agent`, `gopi`) → all reported; 19 legitimate paths (`GOPI.md`,
  `CHANGELOG.md`, `reports/…`, `devdoc/…`, `bin/agent`, nested basenames) → 0
  reported. *This is what caught two fixture collisions (`tmp-e2e.md`,
  `e2e-m420-rank-notes.md` — root basenames that are now correctly in the
  class).*
- **Red-proof (live index)**: `touch ROOT-REDPROOF-REPORT.md agent; git add -f`
  both → `TestRepoRootHasNoTrackedJunk` **FAILED**, naming 2 paths with their
  reasons; `git rm --cached` both + `rm` → **PASS**, and `git status` shows no
  leftover.
- **Negation semantics proven by the gate itself**: `.gitignore` `/*.md` +
  `!/GOPI.md` `!/CHANGELOG.md` → `TestRepoRootJunkPathsAreGitignored` fails if
  either negation is removed, and (after fixing `ignoredPaths`) passes with them
  in place. This is the assertion that keeps the agent's own rule file from
  being silently un-tracked.
- **Ref-check after the move**: a grep for every moved basename outside
  `reports/` shows **every remaining mention already carries its
  `reports/archive/…` path** (the 15 citing docs rewritten); no bare
  `report-*.md` citation remains as a live path claim.
- **Full suite (load-sensitive — read this before calling the suite red)**:
  `CGO_ENABLED=0 go test -count=1 ./...` → everything green **except**
  `FAIL github.com/nick/gopi/internal/tools 600.3xx s`, which is Go's
  **default per-package timeout** (600s), not an assertion failure: the dump
  shows a `panic: test timed out` with ~20 idle `database/sql.…
  connectionOpener` goroutines. Proven environmental, twice over:
  1. **Same tree, same branch, 12 hours apart**: on the quiet host it was
     `ok internal/tools 189.0s` (09:28) and `ok … 266.5s` (09:38); the two
     FAILs (23:26, 23:40) ran at `uptime` load average **42 → 63** with 30+
     concurrent `go test` processes from other agents.
  2. **Raised budget → green**: `go test -count=1 -timeout 25m
     ./internal/tools/` → **ok (1123.8s)**, i.e. the package needs ~1.1–2× the
     default budget under load and has no failing assertion at all.
  `git diff --name-only bf471665..HEAD | grep -c internal/tools` → **0** files.
  Every other package (incl. `cmd/agent`, `testsuite`) is green in the same
  run. **Conclusion: the branch adds zero failures**; the 600s cliff is a
  property of the loaded host, worth its own task (raise the budget per package
  or shrink the suite), not of this change.
- **Workspace-level**: root `agent` removed (`rm ~/work/GoPi/agent`, sha256
  `d9cf657a…` ≠ `bin/gopi` `979ebc19…`); main-repo `git status` still lists no
  tracked change. `go env GOTMPDIR` = `/home/user/workspace/gopi-go-tmp`
  (9.5 GB, 237 `go-build*`) — read-only finding, not touched.
- **Memory**: one `useful` vote (`#896`, the search hit that already answered
  this task's subject) + one new distilled record (`#2174` — the root-doc drift
  mechanism, its ratchet, `git check-ignore` negation gotcha, and the
  false-binary-in-git verification recipe). See the section above for why the
  archived reports themselves were **not** re-stored.
- **Commit**: `83351795` — `git show --stat` → **57 files changed, 207
  insertions(+), 24 deletions(-)**; `git status` after the commit → clean.

## Follow-ups (not done here)

- **Filed as a task (`01a09650-de8c-7514-935a-4b020d3e3e01`, tags bug/flaky-test)**:
  the `internal/tools` 600s default-timeout cliff under host load (see the
  Verification section for the proof) — the package is not broken, the budget
  and/or the package's concurrency is.
- ~~`rm ~/work/GoPi/agent`~~ **done this round** (see the completed entry).
- The two unmerged branches (`feat/tab-order-actuate-poc`,
  `verify/worker-writeback-01a09435`) should land their untracked devdocs when
  they merge, otherwise those files stay island copies forever.
- `devdoc/` consolidation (647 `.md` files / 16 MB) is a separate epic.
- **Operator decision (not a hygiene-branch change):** sweep the 21 degenerate
  `~/workspace/gopi-*` dirs + the 9.9 GB of `/tmp` scratch, and point the
  scratch-GC watchdog's scope at the live `GOTMPDIR` tree
  (`~/workspace/gopi-go-tmp`, 9.5 GB / 237 `go-build*` dirs) — that tree is
  in-use and must be *swept by age*, never deleted.
