<!-- doc-class: normative -->
# Task Report: repo-hygiene — untrack scratch/junk from the GoPi repo root

Branch: `feat/repo-hygiene-tracked-scratch` (worktree `~/workspace/gopi-repo-hygiene`,
based on master `1c021f4c`).

Goal: the repo index carried 457 paths of scratch/junk that no code, test, script
or doc reads — a 19 MB compiled test binary, two fake-`$HOME` probe trees (one
holding a copied real `~/.gopi`), an ad-hoc e2e evidence tree and a pile of
merge-probe leftovers. Untrack them, extend `.gitignore` so they cannot come
back, and ship a machine gate that refuses the next one.

## Completed

- **Untracked 457 paths** with targeted `git rm -r --cached` (never `git add -A`,
  Rule 8) — `git ls-files | grep -cE '^(\.leakprobe|tmp-e2e|e2e-m420-rank)'` → 0.
- **`.gitignore` extended** with the per-class patterns; the existing `bin/`,
  `/agent`, `/gopi`, `.gopi-tags`, `__pycache__/`, `.e2e/` rules are untouched
  (`git diff HEAD~2 -- .gitignore | grep '^-'` shows only the four lines
  replaced by the two real typos this task made — see the ignore-coverage gate
  below).
- **Regression gate** `internal/standards/scratch_gate.go` +
  `scratch_gate_test.go`, five tests in two complementary halves:
  - **index half** — `TestRepoRootHasNoTrackedJunk` reads the real index
    (`git ls-files -z`) and fails on any tracked junk path, with a named-reason
    message per class; `TestRepoRootJunkGateScansImmediately` pins the
    pre-sweep shape; `TestRepoRootJunkGateScansTheRealIndex` fails rather than
    passing blind when the listing comes back empty.
  - **ignore half** — `TestRepoRootJunkPathsAreGitignored` asks git
    (`check-ignore --no-index -v`) whether every junk class is covered by
    `.gitignore`, and asserts legitimate same-basename paths are *not* swallowed.
    Complementary, not redundant: `.gitignore` never applies to a tracked path,
    and the index gate cannot see an untracked `??` path.
  - **non-vacuity** — `TestRepoRootJunkGateRedProof` (pure predicate, 15 poisoned
    + 12 legitimate paths) keeps the gate honest.
- **Four red-proofs, each observed red then green** (see Verification).
- **FOLLOW-UP, 2026-09-26 — a 6th class, index-half only**:
  `internal/standards/scratch_gate.go` gained the **repo-relative `scratch/`
  tree** (path-component anchored, so `internal/scratch/`,
  `reports/scratch/…` and `scratchpad-notes.txt` stay legitimate), and with it
  the last three paths the original sweep had LEFT TRACKED —
  `scratch/payload.md`, `scratch/expected.md`, `scratch/run-clean.log` (probe
  artefacts of `6cf7d1ef`, 2026-09-01) — were untracked, one `git rm --cached`
  per path (Rule 8). They are the causal precondition of the 135 MB untracked
  residue of 2026-09-26: an indexed `scratch/` is what made a run read
  `<repo>/scratch/` as an existing convention. Live red→green on the real index
  is recorded in `devdoc/scratch-repo-residue-01a0db54.md`.
  **This class is deliberately NOT in `.gitignore`** (so it is absent from
  `junkIgnoreSamples` and `TestRepoRootJunkPathsAreGitignored` does not — must
  not — cover it): an ignore line hides the writer and silences the `??` entry,
  which is the signal Rule 30's `completed-unclean` record and the whole residue
  attribution rest on. The two halves of this gate are complementary, not
  symmetric: the ignore half covers classes whose writers are gone and whose
  spelling is known, the index half covers the class whose visibility IS the
  diagnostic.
- **Red-proof #5 (live tree, both acceptance classes at once)**: on the finished
  branch, `touch .leakprobe2-redproof.test w.txt` + `git add -f` both →
  `TestRepoRootHasNoTrackedJunk` FAILED naming each path with its reason; after
  `git rm --cached` (one file per call, Rule 8) → PASS, `git status` clean.
- Two **real `.gitignore` typos were found by the new ignore-coverage gate**:
  `/w.txt` + `/y.txt` were missing entirely, and the two marker patterns were
  written without their leading dot (`/recycle-epic-marker`,
  `/worktree-owner`) — ignoring names that never existed while the real
  `.recycle-epic-marker` / `.worktree-owner` stayed visible in `git status`.

## Per-class reference proof

Method: `git grep` / `keyword_search` over the tracked tree, excluding the junk
paths themselves. "Refs" = files other than the junk that mention the class.

| Class | Tracked | Refs in `*.go`/`*.sh`/`*.py`/`Makefile`/`*.yml` | Where refs live | Verdict |
|---|---|---|---|---|
| `.leakprobe-agent.test` (19 MB, `go1.24.5`, `path github.com/nick/gopi/internal/agent.test` — a `go test -c` output) | 1 | **0** | doc prose in `devdoc/crash-dump-startup-backfill.md` | junk; no legitimate `*.test` path is tracked anywhere else |
| `.leakprobe/`, `.leakprobe2/` (fake `$HOME`: copied `.gopi/memory.db`, 3 `sessions/*.jsonl`, `raw/*.log`; `.leakprobe2` = a whole `go-build` cache) | 411 | **0** | doc prose only | junk |
| `.leakprobe-full.out` (2 636-line probe stdout dump) | 1 | **0** | — | junk |
| `tmp-e2e/` (hand-written e2e evidence: `*.log`, `evidence-run*.jsonl`, `session-logs/*.err`) | 22 | **0** | doc/report prose: `tasks/01a08919-9399-report.md`, `tasks/memory-lesson-hippocampus-convergence-report.md`, `devdoc/hippocampus-repeat-memory-env-leak.md` (narrative only — these describe the experiment and never read the files) | junk |
| `e2e-m420-rank/` (`a/.gopi/memory.db`, `b/.gopi/memory.db`, `seed/delta/back.json`) | 5 | **0** as a repo path — `scripts/e2e-m420-rank.sh:16` mentions the NAME as a dry-run label while writing to `$ROOT=$(gopi_scratch_mkdir m420-rank)` | the script's scratch root, never the repo | junk |
| `w.txt` (`x`), `y.txt` (`y`) | 2 | **0** | — | junk |
| Merge/leak-probe root leftovers: `base.txt`, `div.txt`, `ff.txt`, `ff3.txt`, `shared.txt`, `master-side.txt`, `ort-client.txt`, `ort-master.txt`, `probe-ff-note.txt`, `result.json`, `s6.png` | 11 | **0 at the repo root** — every hit is a *temp-dir basename*: `filepath.Join(t.TempDir(), "base.txt")` (`cmd/agent/*_test.go`, `internal/tools/*_test.go`), `mrsCommitFile(t, repo, "shared.txt", …)`, and `scripts/e2e-task-merge.sh`'s sandbox clone (`WORK=$(gopi_scratch_mkdir task-merge)`) | same names, different directories | junk (hence the `.gitignore` patterns are root-anchored: `/base.txt`, not `base.txt`) |
| `memory.db`, `gopi.db` (0 bytes each — a probe's relative-path sqlite) | 2 | **0** — `cmd/agent/cli_memory.go:114` documents the *default* `~/.gopi/memory.db`; every test sets `GOPI_MEMORY_DB=filepath.Join(t.TempDir(), "memory.db")` | — | junk |
| `.recycle-epic-marker` (`recycle epic work`), `.worktree-owner` (`# uuid-taskid worktree marker …`) | 2 | **0** — no code reads either; only `devdoc/crash-dump-startup-backfill.md:71` lists the marker as debris | — | junk |
| `text2img` (root bash render script) | 1 | name appears only in `text2img-eval/TEXT2IMG-EVAL-codex.md` as a usage example; the live implementation is the `internal/text2img` package + the `text2img` tool | — | junk (superseded prototype) |

Total untracked: **457 paths** (`458 files changed, 43 insertions(+), 1947 deletions(-)`
in the hygiene commit — the extra file is `.gitignore`).

### Root cause: why did a live writer put them there?

- `git grep` for `\.leakprobe` / `tmp-e2e` over all production code, scripts,
  Makefile and CI files → **0 hits**. The probe scripts that produced the trees
  (`.leakprobe*`, `.leakprobe-full.out`, `tmp-e2e/`) are **gone from the repo** —
  they were one-off, ad-hoc invocations; there is nothing to fix, and no code
  re-creates these paths.
- No live writer holds the files (`.leakprobe/home/.gopi/memory.db-shm` is a dead
  sqlite sidecar left by the probe; `ps` shows no process using them).
- The **class-level** root cause is fixed where it can be: the scripted e2e suites
  already write to the pinned scratch root via `scripts/lib/tmp-ban.sh`
  (`gopi_scratch_mkdir`), and the guard test now makes a re-commit impossible to
  land silently. The `*.test` writer (`go test -c` with a default output name in
  the repo root) is covered by the same gate plus the `.gitignore` pattern.

### Honest note on doc references

Five docs mention the untracked paths (`devdoc/crash-dump-startup-backfill.md`,
`devdoc/hippocampus-repeat-memory-env-leak.md`, two `tasks/*.md` reports, one
briefing). They are **narrative**: they describe the debris/experiment and do not
read the files, so untracking breaks nothing. One of them
(`devdoc/crash-dump-startup-backfill.md:126`) schedules exactly this cleanup.
They are left untouched on purpose — rewriting history in prose is out of scope.

## Secret / user-data implication (recommendation, NOT executed)

The tracked `.leakprobe/home/.gopi/memory.db` (128 KB, 26 tables, one
`distilled_qa` row: `smoke topic / smoke question`) is a **schema copy of a real
knowledge base**, and the same tree carried **three session JSONL files** and a
`raw/*.log` under a fake `$HOME`. They are part of commit `db379f68` and stay in
git history after untracking:

- A blob history audit found the junk at commit `db379f68` is **19.7 MB** across
  657 objects (dominated by the 19 MB test binary); the **largest blob in the
  whole repo history** (19 497 922 B) is that test binary, and the next largest
  17.9 MB / 9.0 MB blobs are unrelated pre-existing artifacts.
- No real user knowledge base (`~/.gopi/memory.db` is 83 MB) is tracked: an
  all-objects size scan found no blob over 19.5 MB, and `memory.db`/`gopi.db` at
  the root are 0 bytes. So the exposure is *schema + a smoke row + session
  metadata*, not the distilled memory content.

**Recommendation (do NOT rewrite history here):**

1. Treat `db379f68` (and anything reachable from it) as carrying probe-session
   metadata. If any of those session JSONL files contained prompts worth
   protecting, rotate anything they exposed; otherwise no action is required —
   the copied DB is a schema-only smoke fixture.
2. If the operators decide the probe data must leave history, the only real fix
   is a history rewrite (`git filter-repo --path .leakprobe --path .leakprobe2
   --path .leakprobe-agent.test --path .leakprobe-full.out --path tmp-e2e
   --path e2e-m420-rank --invert-paths`), which rewrites every downstream commit
   SHA and breaks every existing clone/worktree. That is an explicit operator
   decision, deliberately **not** taken by this task.
3. Cheap prevention already landed: the gate fires before a path like this can
   be committed again, and `.gitignore` keeps them out of `git status` so they
   are far less likely to be swept in by a careless `git add`.

## Not Done

- **Two pre-existing test failures were NOT fixed** (out of scope, both proven on
  clean master @ `1c021f4c`): `internal/stylegate TestStyleGate` (4 error-discard
  hits in `cmd/agent/cli_voice*.go`) and `cmd/agent
  TestCLIContractInventoryMatchesDispatch` (`cmd/agent/main.go` has 26 dispatch predicates,
  the contract suite tracks 25 — `VoiceSubcommand` landed without its contract
  case) — filed as follow-up work rather than smuggled into this hygiene branch:
  - `01a091fb-1cff-7b83-9af3-47cfb2670865` — bug/test: CLI contract inventory 26 vs 25.
  - `01a091fb-1e7d-7293-a493-22d3748d2f59` — bug/style: the 4 stylegate sites.
- **History rewrite** — deliberately excluded (constraint: do not rewrite git
  history); analysed and recommended above instead.
- **Remaining root clutter** (report `.md` files, `gopi.db`-adjacent probe
  artifacts already untracked, `scripts/loops_out.txt`, `scripts/mine_out.json`)
  is out of the epic's scope: the epic named the `.leakprobe*` / `tmp-e2e` /
  `*.test` classes plus the probe leftovers, all of which are handled.
- The `.leakprobe*` trees still exist **on disk** (untracked, now ignored). They
  are ~3.3 MB of dead evidence, deletable at will; deleting them is not part of
  the deliverable and was left to the operator so nothing else is lost.

## Verification

Commands run in `~/workspace/gopi-repo-hygiene` on `feat/repo-hygiene-tracked-scratch`.

- **Build**: `CGO_ENABLED=0 go build ./...` → OK (exit 0).
- **Tests**: `CGO_ENABLED=0 go test -count=1 ./...` → **18 packages ok, 2 FAIL** —
  both proven **pre-existing on master**, not caused by this branch. The branch's
  whole non-deletion diff vs master is 4 files
  (`git diff master HEAD --name-status --diff-filter=d`):
  `M .gitignore`, `A devdoc/repo-hygiene-tracked-scratch.md`,
  `A internal/standards/scratch_gate.go`, `A internal/standards/scratch_gate_test.go`
  — zero diff inside `cmd/agent/` or `internal/stylegate/` (`git diff --stat master HEAD -- cmd/agent internal/stylegate` → empty).
  Reproduced independently on a **clean detached master worktree**
  (`git worktree add --detach /tmp/hygiene-baseline master` @ `1c021f4c`):
  - `TestStyleGate` — identical 4 production hits (`cmd/agent/cli_voice.go:124`,
    `cli_voice_server.go:207/162/166`), same message → pre-existing.
  - `TestCLIContractInventoryMatchesDispatch` — same "main.go has 26 … contract
    suite tracks 25" mismatch (a `VoiceSubcommand` dispatch predicate landed
    without a contract case) → pre-existing.
  `internal/standards` (this branch's package) → **ok, 9.56 s**, all five gate
  tests PASS.
- **Targeted gate**: `go test -run 'TestRepoRoot' -v ./internal/standards/` → all
  five PASS (`TestRepoRootHasNoTrackedJunk`, `TestRepoRootJunkGateRedProof`,
  `TestRepoRootJunkGateScansTheRealIndex`, `TestRepoRootJunkPathsAreGitignored`,
  `TestRepoRootJunkGateScansImmediately`).
- **Red-proof #1 (predicate)**: 15 poisoned paths → all 15 reported; 12 legitimate
  paths → 0 reported. *This is what caught a real hole*: the first version checked
  the leak-probe class as an exact directory (`.leakprobe/`), which silently
  missed the whole `.leakprobe2/` tree (405 of the 457 paths). The class is now a
  root-anchored prefix and the proof is what forced the fix.
- **Red-proof #2 (real index)**: `git add -f w.txt` + a fresh
  `.leakprobe2-redproof.test` → `TestRepoRootHasNoTrackedJunk` FAILED naming both
  paths with their reasons; after `git rm --cached` → PASS, `git ls-files | grep -c`
  → 0.
- **Red-proof #3 (ignore coverage)**: deleting the `/w.txt` line from
  `.gitignore` → `TestRepoRootJunkPathsAreGitignored` FAILED naming `w.txt`
  ("NOT covered by .gitignore"); restoring the line → PASS.
- **Red-proof #4 (`.gitignore` diff is additive)**: the two marker patterns were
  reported by the ignore gate as unmatched after being written without their
  leading dot; `/.recycle-epic-marker` + `/.worktree-owner` + `/w.txt` + `/y.txt`
  were added, and `git status` went from listing four `??` junk paths to listing
  only the three files this task edited.

  - `git ls-files | grep -cE '^(\.leakprobe|tmp-e2e|e2e-m420-rank)'` → **0**
  - `git ls-files | grep -cE '\.test$'` → **0**
  - `git ls-files w.txt y.txt` → **empty**
  - `git diff --cached --name-only --diff-filter=A` before the commit → **0** (no
    file was accidentally added while untracking)
- **`.gitignore`**: the only removed lines are the two typo'd patterns this task
  wrote itself (`/recycle-epic-marker`, `/worktree-owner`); `bin/`, `/agent`,
  `/gopi` are still present (`grep -c '^bin/$' .gitignore` → 1).
- **Commits**: `b5ddd85c` (untrack + .gitignore), plus the guard commit recorded
  below.

### Commit hashes

```
b5ddd85c chore(hygiene): untrack repo-root scratch junk + extend .gitignore
6d8cb0e2 test(standards): repo-root tracked-scratch gate (red-proofed)
e7a35dbd fix(hygiene): real .gitignore patterns for w.txt/y.txt + dot markers; add ignore-coverage gate
28d2cef7 docs(hygiene): report — per-class reference proof, history note, verification
f457304c docs(hygiene): record commit list + red-proof recipe
c5816b4d docs(hygiene): verification — 2 pre-existing failures proven on clean master, follow-ups filed
```


trees still sit on disk, ignored (`git check-ignore -v .leakprobe` →
`.gitignore:25:.leakprobe*`), so they no longer appear as `??`.

## How to reproduce the red-proof

```bash
cd ~/workspace/gopi-repo-hygiene
printf 'junk\n' > w.txt && git add -f w.txt && git commit -qm repro
go test -run TestRepoRootHasNoTrackedJunk ./internal/standards/   # FAILS naming w.txt
```

Recovery (deliverable lives on the branch in the shared repo, Rule 6d):

```bash
cd /home/user/work/GoPi && git fetch . feat/repo-hygiene-tracked-scratch  # no fetch needed: worktree-shared
git show feat/repo-hygiene-tracked-scratch:devdoc/repo-hygiene-tracked-scratch.md
git show feat/repo-hygiene-tracked-scratch:internal/standards/scratch_gate.go
```
