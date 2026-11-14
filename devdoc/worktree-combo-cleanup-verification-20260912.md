<!-- doc-class: historical -->
# Task Report: independent verification — abandoned worktree `~/workspace/gopi-combo` (ledger row `01a09464-f2d9-7bef-a898-db515c79e3fd`)

Verifier session: read-only. The judged row stays terminal; the deliverable
(`docs/gopi-combo-worktree-cleanup` @ `b48e20db`, merge `875816fd`) was **not**
edited. Report branch: `docs/worktree-combo-cleanup-verification`.

**VERDICT: PASS** — re-derived from the shared repo, not from the deliverable's
own report.

---

## 1. Re-derived evidence table

| # | Claim to re-derive | Command | Observed | Verdict |
|---|---|---|---|---|
| a1 | HEAD side `fcbb4742` is already a master ancestor | `git merge-base --is-ancestor fcbb4742 master` / `git rev-list --count ^master fcbb4742` | exit 0 / count **0** | TRUE |
| a2 | MERGE_HEAD side `58314968` is already a master ancestor | `git merge-base --is-ancestor 58314968 master` / `git rev-list --count ^master 58314968` | exit 0 / count **0** | TRUE |
| a3 | Neither side is an ancestor of the other (a real two-sided merge, not a ff) | `git merge-base fcbb4742 58314968` | `79d6fe52` (both sides branched there) | TRUE |
| b1 | master carries the merge's target files | `git cat-file -e master:<path>` | `crashdump_orphanwindow_test.go` IN, `devdoc/crash-dump-orphan-false-positive.md` IN | TRUE |
| b2 | The cleanup devdoc is in master | `git rev-parse master:devdoc/worktree-combo-cleanup.md` | blob `2ba6ad1f…` (authored `b48e20db`, merged `875816fd`) | TRUE |
| b3 | The 8-file index residue's paths all exist in master | `git cat-file -e master:<p>` ×8 | 7/7 tracked paths IN (the 8th, `?? tmpbench/`, is untracked scratch) | TRUE |
| c1 | worktree path gone | `ls -ld /home/user/workspace/gopi-combo` | No such file or directory | TRUE |
| c2 | gitdir registration gone | `ls .git/worktrees/` | `gopi-mergerverify, gopi-verify-01a09435, gopi-verify-glmwatch, gopi-verify-prefix, gopi-verify-rpt, gopi-zombie-dispose, red, repo` — **no `gopi-combo`** | TRUE |
| c3 | No dangling/prunable entry | `git worktree list --porcelain`, `git worktree prune -n -v` | 10 entries, diff vs expected set **empty**; prune dry-run prints **nothing** | TRUE |
| c4 | Other live worktrees untouched | HEAD + index mtime of the 6 listed worktrees + `/tmp/gwv/*` | HEADs `a0bc3e11 / a8dd161a / 141a6aa8 / cf99e644 / 999ae086 / c0211ad4`, `/tmp/gwv/{red,repo}=55296ffb`; index mtimes 07:55–17:40 (all predate this session's 17:4x work); **no `*.lock`** in `.git/worktrees` | TRUE |
| c5 | No live holder of the removed path | `/proc/[0-9]*/cwd` readlink scan + `/proc/[0-9]*/fd/*` symlink grep | **0** cwd holders, **0** open fds | TRUE |
| c6 | Refs not disturbed | `git for-each-ref | wc -l`, `git rev-parse master` | 64 refs, master = `875816fd` (the cleanup merge is the current tip; `git reflog master` shows `875816fd` ← `f970c08c` ← `c0211ad4`) | TRUE |

## 2. Honest assessment of the "8-file reverse diff / nothing unique" claim

**What is verifiable after removal (and re-derived here):**

- Both sides' objects are still reachable **from master's own history** — the
  merge was a *replay of already-landed work*: `git log master` contains
  `fcbb4742` ("sweep orphan sessions at most once per UTC day") and `58314968`
  ("accept the legacy session_id spelling"), and every index-residue path
  resolves in master (`git cat-file -e master:<p>`) with the merged content
  present: e.g. `sessionTailWindowBytes` is in `master:internal/agent/session.go`,
  and the orphan-sweep gate lives in master's `crashdump_sweep_gate.go`.
- The **"reverse diff" reading is confirmed** directionally: `git diff --shortstat
  master fcbb4742` = `689 files changed, 2630 insertions(+), 42006 deletions(-)`
  — the residue is dominated by *content master has that the parked checkout
  lacks*, i.e. diffing the stale tree against master deletes far more than it adds.
  Master is the strictly newer superset: for the residue files master has
  1–12 later commits (e.g. `cmd/agent/main.go` 12, `session.go` 4), and the
  forward per-file merge of `base←HEAD,MERGE_HEAD` reproduces master exactly for
  `internal/agent/envparse.go` (`git merge-file` output byte-identical to master).
- The residue's manifest is **already frozen on master** and independently
  reachable: `89457bbb` (round-14 salvage, an ancestor of master, and an ancestor
  of the cleanup commit `b48e20db`) carries
  `salvage/crash-tail-combo/status.txt` — the exact `UU cmd/agent/main.go` +
  `A/M` list — plus `stale-vs-master.diff` (837 lines) and a frozen copy of the
  one file that master had *superseded*,
  `salvage/crash-tail-combo/sessionend_bounded_read_test.go.txt`, whose blob
  hash `f43e6db2…` is byte-identical to `fcbb4742:internal/agent/sessionend_bounded_read_test.go`.
  So even the single artifact the stale checkout held that master no longer has
  (it was superseded by master's equivalence-oracle test) survived in the repo
  *before* the removal.
- Untracked `tmpbench/` was the only non-git artifact; the deliverable's report
  documents its backup and that its measurements were already published in
  master's devdocs. Post-removal there is no git-side way to re-inspect it —
  **that part is accepted on the deliverable's record, not re-derived here.**

**Honest gaps / what is NOT provable after the fact:**

1. The `UU` file's *working-tree* text at removal time cannot be re-inspected
   (the index/worktree are gone). It is inferable only indirectly: the frozen
   `stale-vs-master.diff` and the ancestry of both sides show the merge was a
   replay, and `git diff master fcbb4742` shows the direction of the residue.
2. `tmpbench/`'s content is only attested by the deliverable's devdoc + the
   `salvage/…/tmpbench-combo-vs-frozen.diff` (21 lines) artifact.
3. The deliverable's `git for-each-ref` before/after ref comparison cannot be
   replayed (its "before" snapshot is gone). What IS re-derived: the working tree
   and live ledger are consistent now, and no other worktree was touched (§1 c4).

## 3. Build + the project's own tests

| Gate | Command | Result |
|---|---|---|
| Build | `CGO_ENABLED=0 go build ./...` | **exit 0** (`BUILD_OK`) |
| Vet | `go vet ./internal/agent/` | clean |
| Standards gates (Rules 16–21) | `go test ./internal/standards/ -count=1 -timeout 10m` | `ok github.com/nick/gopi/internal/standards 3.469s` (20 gate tests) |
| Worktree/task-maintenance selectors | `go test ./internal/tools/ ./cmd/agent/ -run 'Worktree|Combo|Stranded|Recycle|MergedCleanup|FinishCleanup|StaleOwner|LiveHolder' -count=1 -timeout 15m` | `ok internal/tools 5.644s`, `ok cmd/agent 6.000s` — 89 + 21 test funcs actually matched (`-list`) |
| Deliverable-area tests | `go test ./internal/agent/ -run 'Crashdump|Orphan|SessionEnd|SessionTail' -count=1 -timeout 10m` | `ok internal/agent 1.789s` — 44 test funcs matched |

## 4. A/B probe of the DECISION RULE (synthetic twins)

The rule under verification (*the preflight*): an abandoned worktree parked in an
in-progress merge is abort+removable **only if both merge sides are already
ancestors of the trunk**. Because the delivered artifact is a one-shot
doc+operation (nothing in the repo executes this rule), the probe re-implements
the rule (`ab/preflight.sh`, verifier-authored, outside the repo) and runs it on
synthetic twins built by `ab/build_twins.sh` + `ab/run_ab.sh`:

| Probe | Twin | Preflight | Action taken | Outcome |
|---|---|---|---|---|
| P1 | `stale`: HEAD `94fbfcc` and MERGE_HEAD `19b1b945` both ancestors of master; `UU f.txt` (the gopi-combo shape) | `VERDICT=SAFE`, exit 0 | `git merge --abort` + `git worktree remove --force` | abort+remove exit 0; **`git rev-list --all --not master | wc -l` = 0** (nothing unreachable was lost); worktree dir gone, `.git/worktrees/stale-wt` gone, `git worktree prune -n -v` empty, `fsck` reports no corruption → **removal loses nothing** |
| P2 | `unique`: parked revision is a **non-ancestor** commit (`719f2d9`, 1 commit unreachable from master) | `UNSAFE: … HEAD 719f2d9… is NOT an ancestor of master` → `VERDICT=UNSAFE`, **exit 2** | **REFUSED — nothing removed** | worktree + gitdir still registered, HEAD preserved at `719f2d9` |
| P2b (sharp twin) | worktree mid-merge with HEAD an ancestor but **MERGE_HEAD `c2c68329` NOT an ancestor** (`UU f.txt`) | `UNSAFE: MERGE_HEAD c2c68329… is NOT an ancestor of master` → exit 2 | **REFUSED** | MERGE_HEAD still reachable from the worktree, 1 unique commit intact |
| P3 (residual risk, documented) | stale merge with HEAD+MERGE_HEAD ancestors, but a **hand-written** novel line typed into the `UU` file | `VERDICT=SAFE` | would be removed | **the rule cannot see working-tree-only text no object holds** — the probe proves the novel text exists in **no blob** in the object store, and the rule still says SAFE. Filename/showcase evidence of the claim's boundary: the rule is safe for *this* incident (where the index residue is a replay of landed work and the frozen diff is on master) but an ancestry-only rule is **blind to uncommitted hand edits in a conflicted file**. This is a stated limit, not a pass. |

**The safety twin is the point of the exercise**: a rule that deletes anything is
not verified by its own success — P2/P2b show the preflight *refuses* a
unique-work twin (exit 2, artefact untouched), so the delivered decision is
narrowed, not reckless.

## 5. Verdict record

Recorded **exactly once**:

```
gopi task update 01a09464-f2d9-7bef-a898-db515c79e3fd --status verified \
  --result "evidence: …"     → see §6 (the row remains terminal)
```

## 6. Not Done / Deliberately Not Done

- **No fix, no edit to the deliverable** — `git worktree list` / `master` unchanged
  (`875816fd`); the only repo write is this report on its own docs branch.
- **No re-issue of the verdict** (one `gopi task update`, atomic; re-issuing would
  be a second unauthorized ledger mutation).
- **No `pkill`**, no touch of `gopi-mergerverify`, `gopi-verify-01a09435`,
  `gopi-verify-glmwatch`, `gopi-verify-prefix`, `gopi-verify-rpt`,
  `gopi-zombie-dispose`, `/tmp/gwv/*` (HEADs + mtimes recorded before/after, §1 c4).
- **No full `go test ./...`** — this repo's full suite has pre-existing reds/hangs;
  the bounded selectors above are the relevant gate and all matched real tests.
- Probes P3's limitation is reported as a **gap**, not smoothed over.

## Verification (Rule 5 summary)

- Build: `CGO_ENABLED=0 go build ./...` → exit 0
- Tests: `internal/standards` ok (3.47s); `internal/tools` ok (5.64s); `cmd/agent` ok (6.00s); `internal/agent` ok (1.79s)
- Structure: worktree set = expected 10 entries, no prunable entry, `.git/worktrees/gopi-combo` absent, 0 `/proc` holders
- A/B: P1 SAFE→removed with 0 commits lost; P2/P2b UNSAFE→refused (exit 2, artifacts intact)
- Verdict: one `gopi task update … --status verified --result "evidence: …"`
