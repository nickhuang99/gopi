<!-- doc-class: historical -->
# Verification report — task 01a095cf "gopi development workspace should be cleaned up"

**Verifier role**: independent verification instance (no fixes made).
**Deliverable under test**: branch `chore/workspace-hygiene`, merged into master as
`9cc034bc` (parents `5e57ef57` pre-fix / `e8be1e1f` fix tip). Fix commits:
`83351795`, `52e8ca21`, `9b31890f`, `33240fac`, `6724c125`, `e8be1e1f`.
**Verdict**: **PASS** — recorded once on the ledger.

## Premise check (the briefing asserted a binary `agent` in git)

**False, and correctly disproven by the deliverable.** `git log --all -- agent` → 0
commits; `git ls-files agent` → nothing; root `agent` is gone; `.gitignore:6` had
`/agent`/`/gopi` before this task. The 19 MB file was untracked build debris produced
by `CGO_ENABLED=0 go build ./cmd/agent/` with no `-o` (writes `./agent`); the declared
build `-o bin/gopi` never does.

## Acceptance criteria vs evidence

| Criterion | Evidence (measured on master `9cc034bc`) |
|---|---|
| Reports out of the repo root | `git ls-tree --name-only bf471665 \| grep -c '\.md$'` → **39**; at `9cc034bc` → **2** (`GOPI.md`, `CHANGELOG.md`). Tracked root files are exactly `.gitignore CHANGELOG.md GOPI.md Makefile go.mod go.sum`. |
| Moved to an appropriate report dir | `reports/archive/2026-08/` = **33** files, `reports/archive/2026-09/` = **4** → 37 total, matching the report's claim. Moves are pure renames (`R`, 0 line changes), so no content was lost. |
| Path for the report deliberately chosen | `devdoc/repo-workspace-hygiene-2026-09-12.md` (17 KB, tracked, committed in `e8be1e1f`) states the rule: a snapshot → `reports/archive/<YYYY-MM>/`, a standing narrative → `devdoc/`. `reports/README.md` + `briefings/README.md` (both tracked) carry the standing rules. Justified and consistent. |
| Tracked vs untracked split explained | `git status --porcelain` → 0 modified tracked files; 32 untracked paths, all classified in the report by class (23 `briefings/*.md` dispatch envelopes already treated as untracked by `internal/tools/rebuild.go`, 6 island devdocs of unmerged branches, 1 byte-identical duplicate, 1 orphan kept). |
| Drift cannot recur (ratchet) | `internal/standards/scratch_gate{,_test}.go` on master; `.gitignore` `/*.md` + `!/GOPI.md` + `!/CHANGELOG.md` (lines 70–72). |
| Root cited docs repaired | 15 citing documents rewritten in `83351795` (`git show --stat -M` → 57 files, 207+/24−); a scan for the 37 moved basenames outside `reports/` finds **no bare path citation** — the only hits are the hygiene doc quoting old names as history. |
| Memory-distillation question answered | Empirically, not as opinion: the report records a `memory(action=search)` whose hits already carried the distilled form, and new record **#2174** exists in `~/.gopi/memory.db` (`distilled_qa`, topic = "Repo root doc/binary hygiene…", confidence 0.85, warm). A `useful` feedback vote on `#896` from this session is on record (`memory_feedback` row 2148, task_id `1b730399-…`). |

## Build + tests (run by this verifier, fresh detached worktree at `9cc034bc`)

```
$ CGO_ENABLED=0 go build ./...          → exit 0
$ go test -run TestRepoRoot -v ./internal/standards/
  PASS TestRepoRootHasNoTrackedJunk / …JunkGateRedProof /
       …GateScansTheRealIndex / …JunkPathsAreGitignored / …GateScansImmediately
  ok  github.com/nick/gopi/internal/standards  0.396s
```

Live red-proof reproduced independently: `touch ROOT-REDPROOF-REPORT.md agent; git add -f`
→ `TestRepoRootHasNoTrackedJunk` FAILS naming both paths; `git rm --cached` + `rm` → PASS.
The full `./...` suite was **not** re-run: the branch touches 0 files under
`internal/tools`, whose 600s default-timeout failure under host load is pre-existing and
proven so in the report (raised-budget run green, 1123.8s; same tree green 12h earlier);
it is filed as follow-up `01a09650-de8c-7514-935a-4b020d3e3e01` (status `pending`, tags
bug/flaky-test), which exists as claimed.

## Caveats (non-blocking, do not change the verdict)

1. Report authorship attribution is slightly off: it lists the two gopi-authored root
   docs as `report-websearch-quality-fix.md` + `report-memory.md`. Measured additions at
   the old root path: the two gopi ones are `report-websearch-quality-fix.md`
   (`004bfa50`) and `brief-reportA.md` (`6657af4a`) — `report-memory.md` was *added* by
   Qingzhe Huang (`f0e64e8e`) and only *edited* later by gopi (`40ebdd91`). Cosmetic.
2. The report's own leftover-sweep section leaves operator decisions open on purpose
   (21 degenerate `~/workspace/gopi-*` dirs, 9.5 GB live `GOTMPDIR`, 9.9 GB `/tmp`) —
   declared out of scope with reasons, not silently skipped.
3. Verifier-declared state: this report file is **untracked** in the main repo
   (`devdoc/verify-*.md` is the established verifier-deliverable convention); no other
   file in the main repo was touched, and no fix was applied.

## Where the deliverable lives (recovery)

- Branch: `chore/workspace-hygiene` — merged; master tip is `9cc034bc`.
- Recover any artifact: `git show 9cc034bc:devdoc/repo-workspace-hygiene-2026-09-12.md`
