<!-- doc-class: historical -->
# Recovery ruling: `archive/feat/conflict-branch-rename` (12 commits) — the park mechanism is VOID, the graveyard half is LIVE WORK

Task ledger row: `01a0ab60-15a5-709f-91bb-1a9efe6331ee` (cancelled, tags
`merge-conflict,auto-dispatch`). Archive tags:
`archive/feat/conflict-branch-rename` and `archive/feat/conflict-branch-rename-20260917`,
both at **`abb4a062`** (2026-09-17). Cancel reason on the row: *"superseded: the
park/resolve-conflict mechanism is retired"* — which is true of the MECHANISM and
was never a measurement of the WORK.

## 1. Verdict

**Option 2 of the acceptance: the capability is deliberately dropped — with one
live exception, which is landed by this branch.** The tag stays the recovery
source and is named below. Nothing about the park mechanism is recovered.

## 2. Why the park half may not be recovered (measured, not asserted)

The branch's whole purpose was `git branch -m <branch> conflict/<branch>` — park
state carried in the branch NAME. That is VOID, three times over:

| evidence | what it says |
|---|---|
| `devdoc/branch-lifecycle-ruling.md` (master, commit `762bc8eb`, 2026-09-17 19:54) | the operator's ruling: the endpoint of a branch's lifecycle is **DELETION**; a state-carrying name is a **third state**; *"a branch is either in `refs/heads/` or it is deleted"*. §4 lists the artifacts it retired. |
| `briefings/brief-conflict-branch-rename.md` (master) | now opens with `# ⚠️ VOID — this brief's mechanism was WITHDRAWN (2026-09-17)`. It is the payload an agent would otherwise still follow. |
| `cmd/agent/merge_prompt_gate_test.go` + `cmd/agent/merger_takeover_prompt_gate_test.go` | **mechanical gates** that fail on the voided namespace appearing in the shipped merger prompts (`mrgVoidedStateNamespacePrefix`). |
| `internal/tools/branch_lifecycle_ruling_test.go` | the loop-proof: a worktree-held branch is protected, a terminal rework row does not dedup — delete, not a retained branch, ends a hand-off. |

Timeline: the tag's tip is `abb4a062`, 01:23; the ruling landed 19:54 the same
day. The branch is **superseded by construction**, and merging it would turn
master's own gates RED — the 12 commits are no longer merely unneeded, they are
actively wrong.

## 3. Per-commit triage of the 12 (none blind-merged)

`git log --oneline master..archive/feat/conflict-branch-rename` (12 commits):

| commit | subject | disposition |
|---|---|---|
| `c972cdd3` | park state lives in the branch NAME — `conflict/<name>` | **void** (the mechanism the ruling withdrew) |
| `35674141` | track the conflict-rename live-demo log | **void** + `.evid/` scratch (Rule 30: not a deliverable) |
| `7c881182` | correct two `task_merge.go` baseline re-keys | obsolete — baselines have been re-keyed many times since; the branch's line numbers are gone |
| `9fdeed46` `78f035a5` `8f9a37a9` `bb4d4992` `a03cf535` | devdoc edits for `devdoc/conflict-branch-rename.md` | **void** — they document the withdrawn carrier; the package-budget finding has its own home (`devdoc/ledger-dsn-cmd-agent-contract.md`, Rule 27) |
| `7e38e782` | mark `merger-ff-park-file.md` partially superseded | **void** (it supersedes the doc in favour of the withdrawn carrier) |
| `21e1cea3` | rewrite `buildGateRefuseFmt` elements 1/6 around the park namespace | **void** — the rework brief would teach a reworker to look for `refs/heads/conflict/<name>` and to revive a parked ref. Master's brief carries the archive-tag semantics instead |
| `ad1f80f7` | gofmt the two files this task edited | **void** (formatting of voided files; `internal/branchnames/` itself is not recovered) |
| `abb4a062` | test: cover the manual-merge refusal arm (BOTH namespaces) | **salvaged, graveyard half only** — see §4 |

11 of 12 are dismissed with a reason; 1 is salvaged in part.

## 4. The live defect the recovery found (and landed)

`abb4a062` guarded a hole that exists on master **today**. Measured 2026-09-19 on
master `ece6fe8d`, with a throwaway probe (written, run, deleted — never
committed):

```
PROBE branch="archive/feat/dead-loser" action="merged" detail="clean merge, branch deleted"  master_moved=true
PROBE manual-tagged branch="feat/tagged-loser" action="merged"                                  master_moved=true
PROBE --all branch="archive/feat/dead-loser" action="merged"  (Merged=2 alongside feat/ok)
```

**The exemption that every merge path claims was applied to no door the operator
can open.** `repoSweepClassify` drops a decided loser from the merger tick's
candidate set — via the `archive/` NAME bucket and the `refs/tags/archive/<name>`
tag — but:

1. a positional `gopi task merge <ref>` reaches neither discovery filter, and
2. `--all` builds its own list before `taskMergeOne` runs, so its pre-filter is
   the exemption's second door;

both let a branch named `archive/*`, or a branch carrying an archive TAG, land on
master and be **deleted** in the same call — the one state the graveyard
namespace exists to prevent (`GOPI.md` Rule 9).

**Fix** (this branch, `internal/tools/task_merge.go`):

- `taskMergeGraveyardExempt(branch)` — the ONE predicate for the graveyard
  namespace, derived from `archiveBranchPrefix` (Rule 1b/13/14: no bare
  `"archive/"` literal). Used by the decision tree's ⓪c arm **and** by the
  `--all` pre-filter.
- ⓪c's tag arm reuses `repoSweepArchiveExempt` — the predicate the sweep already
  trusts — so the tick and the manual face cannot disagree about who is archived.
- Both arms sit **before** every merge-time gate: master-safe by construction
  (nothing has touched master when they return), matching the file's existing
  arm design.

**Regression tests**: `internal/tools/task_merge_graveyard_refusal_test.go` —
three cases (hand-named NAME, hand-named TAG, `--all` sweep), each asserting
`Merged=0`, master's tip unchanged, the ref surviving, and a readable reason;
the `--all` case also asserts the ordinary branch still lands and that master
does not contain the graveyard tip. Each carries the red-proof output it was
written against in its doc comment.

## 5. Recovery recipe (the tag is the source, kept deliberately)

```bash
git worktree add ~/workspace/gopi-<task> -b <fresh-branch> master
cd ~/workspace/gopi-<task>
git fetch /home/user/work/GoPi refs/tags/archive/feat/conflict-branch-rename
git log --oneline master..FETCH_HEAD          # read before merging: 11 commits are void
git cherry-pick <the commit you actually want>
```

`git show archive/feat/conflict-branch-rename:<path>` reads any file without a
checkout (Rule 29: never develop in a foreign worktree). Note for the next
reader: the archive tags are swept periodically — this task's own worktree was
removed by the sweeper mid-session, so recover promptly or copy the tag's
content out early.

## Not Done (honest)

- **The park mechanism is not recovered**, by ruling (§2), not by neglect.
- **11 of the 12 commits are dismissed by reading, not by trial**: they build or
  document the withdrawn carrier. If a future reader wants the park design's
  *reasoning* (why a name was tried instead of a tag), it is in the branch's
  `devdoc/conflict-branch-rename.md` — readable with `git show`, and explicitly
  marked void here.
- **`internal/branchnames/`** (the voided helper package, `ConflictBranchPrefix`
  / `IsConflictBranch` / `ConflictName`) is deliberately absent from master and
  stays absent. The acceptance's first option (a `conflict/` naming helper with a
  regression test) is **refused by the operator's ruling**, so this task takes
  option 2 and lands the graveyard fix instead.
