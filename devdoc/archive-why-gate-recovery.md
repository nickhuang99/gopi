<!-- doc-class: historical -->
# Task Report: archive-why gate — the WHY on an archive write, and the refusal to bury a finished task's branch

Branch: `feat/archive-why-gate-recover` (worktree `~/workspace/gopi-archive-why-gate`), based on
master tip `ece6fe8d`. Recovery of two cancelled rows (`01a0ab1e` = `feat/archive-why-gate`,
tip `eac3b475`; `01a0ab7c` = `feat/hand-archive-gate`, tip `c7569bcc`), both preserved under
`archive/feat/archive-why-gate*` / `archive/feat/hand-archive-gate*`.

## Completed

### 1. Which of the two attempts is authoritative, and why (read first, not re-derived)

The two branches are not two competing designs of the same thing — they are the SAME design at
two moments, and the later one is the correction of the earlier one:

| | `archive/feat/archive-why-gate` (tip `eac3b475`, 4 commits) | `archive/feat/hand-archive-gate` (tip `c7569bcc`, 8 commits) |
|---|---|---|
| where the WHY lives | the tag's **own message** (`git tag -a`) | a **companion ref** `refs/gopi/archive-reason/<branch>` |
| effect on the ref | `git rev-parse archive/<branch>` returns the **tag object** | **lightweight**, at the branch tip — unchanged |
| the human write path | none (documented, not built) | `gopi archive <branch> --reason <code>` |
| refusal gate | `repoSweepArchiveBranch` consults the owner | `ArchiveDecide` (hand face) + the mechanism writer |

The annotated-tag cut is defeated by a fact the later branch measured: four readers repo-wide treat
`refs/tags/archive/<branch>` as a **commit**, and the annotated tag silently re-pointed it at a tag
object (`TestRepoSweepParksNonFFBranchAndContinues` went red on that cut). A gate must never change
the shape of the thing it gates. The companion-ref design records the WHY while leaving the
decided-exclusion ref byte-identical, and it makes "was a reason recorded?" a mechanical question
(presence of a ref) instead of an inference from git's paragraph folding.

So this recovery carries the **hand-archive design** (its `archive_reason.go` / `cli_archive.go`
verbatim — they are the corrected generation) and folds in the archive-why census/visibility half.

### 2. What the drift was (the reason this was a port, not a cherry-pick)

Between the archived tips (2026-09-17 00:27 / 01:12) and master (`ece6fe8d`, 2026-09-19) the
surrounding machinery was retired:

- **the park lane is GONE** (`repoSweepParkBranch`, `repoSweepParkOutcome`, `repoSweepParkReason`)
  — epic merger-remove-park: a non-ff branch is reported and left alone, never tagged. So
  `repoSweepArchiveBranch` has exactly ONE caller on master (the merge-time compile gate) and the
  hand-archive diff's park-lane hunks had to be dropped, not merged.
- **the resident merger daemon is retired** (`devdoc/merger-retired.md`), so `MergerStatusReport`,
  `MergerRepoStatus`, `MergerSweepPreview.Archived` and the `merger --status` render the
  archive-why branch built are gone. `--status` survives only as the lock-liveness probe in
  `merger_status.go` — which is why the archived set is rendered on the surviving status face
  (`gopi merge --status`) and on the merger's own sweep log line instead.
- **`internal/branchnames` does not exist on master** (it was on the conflict-rename branch), so
  the archive namespace prefix stays `archiveBranchPrefix` in `internal/tools/tasks.go` — one
  definition, unchanged, as the epic requires.

### 3. The recovered capability (all on the fresh branch)

**① The WHY is a first-class record.** `internal/tools/archive_reason.go` (new, from the
hand-archive tip): the closed reason vocabulary (`not-ff-able-park` / `compile-failure` /
`failed-verify` / `superseded` / `hand-mistake` / `unowned`), `ArchiveTagReasoned` — the ONE writer,
which mints the **lightweight** tag `refs/tags/archive/<branch>` (shape unchanged, never forced, so
no historical ref is touched) and records
`branch` / `reason` / `repo` / `actor` / `task` / `at` / `detail` in
`refs/gopi/archive-reason/<branch>` — plus the readers (`ArchiveTagReason`, `ArchiveNamespace`,
`ArchiveReasonlessBacklog`, `ArchiveNamespaceOrphans`, `ArchiveReport`).

**② The refusal — a finished task's branch is not buried.** Two faces, ONE predicate
(`archiveOwnerDecided`):

- the **human face**: `gopi archive <branch> --reason <code>` (`cmd/agent/cli_archive.go`, new;
  `CliVerbArchive` in `cli_verbs.go`, dispatched from `main.go`, completion entry, contract
  inventory, config help row). A `completed`/`verified` owner is REFUSED unless `--reason <code>`
  **and** `--override-completed` are both given; a **live** owner (pending/claimed/in_progress) is
  refused too (the safe reading of the case the epic leaves open); `failed`/`cancelled` and
  unowned branches proceed (unowned is forced to `--reason unowned`). The refusal runs BEFORE any
  ref exists, so a refused archive leaves no trace.
- the **mechanism face** (`repoSweepArchiveBranch`, `internal/tools/repo_resolve_row.go`) — the
  writer the merge-time compile gate calls, i.e. the only path that fires **unattended**. It now
  requires a non-empty reason and consults the same owner census; a finished/in-flight owner is
  refused unless the repo's settings entry names an explicit, recorded permission
  (`repos.<path>.archive_bury_override`, new key in `internal/config` + `ArchiveBuryOverrideFor`).
  This is the half the two archived attempts left open — `archive_why.go` gated the mechanism and
  had no human verb; `archive_reason.go` gated the verb and left the mechanism minting bare tags.
  Without it the epic is only half-closed: the 21:04 defect reachable by another door.

**③ The set is visible.** `RepoSweepPreview.Archived` / `DescribeArchived()` (`repo_sweep.go`)
render `archived=N (BURIED: k [branch — task (status), …])` into the merger's own sweep log line
via the existing `DescribeProtected` composition, and `tools.ArchiveReport` (`archive_reason.go`)
is the full report, exposed as the read-only `gopi merge --status` arm (`merge_run.go`):
no lock, no sweep, no child. An unreadable listing is reported in words — never rendered as
"nothing is archived".

**④ The backlog check** (`ArchiveReasonlessBacklog` + `ArchiveBacklogReport`, `gopi archive
--check`): every archive tag over a `completed`/`verified` row carrying no reason — exit 1 on
findings.

### 4. Interface change (deliberate, documented)

`repoSweepArchiveBranch`'s Go signature gained its `reason` parameter, and `ArchiveNamespace`
returns `[]ArchiveBacklogRow` (owner + reason) rather than a status-only struct — the status
struct it used to return was deleted with the retired daemon. Both are named-in-the-report
interface changes on a branch that has not landed; nothing external consumes them.

## Not Done (honest)

- **The nine-tag 21:04 remediation is NOT done here.** The epic ('Do NOT delete any existing
  archive tag') puts sweeping them in a separate task (`01a0aa9e`); `gopi archive --check` reports
  the backlog and `ArchiveNamespaceOrphans` reports records outliving their tag, but no existing
  tag was rewritten, forced or deleted by this change.
- **The annotated-tag variant was not resurrected.** The spec allows either design; the companion
  ref is the one this branch carries, with the shape argument in §1 of this devdoc and pinned by
  `TestArchiveWriterRecordsAReadableReason` (lightweight, `rev-parse <tag>` == branch tip).
- **No live end-to-end merger tick was run** (a merger tick writes refs; the acceptance asks for
  the gate, the tests and the report). The mechanism refusal is proven by test
  (`TestRepoResolveRowRedProof`, incl. the neutered-tree red observation) and the human refusal by
  the CLI test plus a live binary run.
- `git status --porcelain` in this worktree must be empty at close-out (Rule 30); if any entry is
  listed in the session log, it is residue and was removed before the final message.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` — OK (no output)
- Vet: `CGO_ENABLED=0 go vet ./internal/tools/ ./cmd/agent/ ./internal/config/` — OK (no output)
- gofmt: clean on every file this branch touches (the three `gofmt -l` hits in
  `internal/tools` — `task_finish_premerge.go`, `task_staleness_guard_test.go`,
  `taskledger_cost_test.go` — are pre-existing and also listed by `gofmt -l` in the
  MAIN repo checkout).
- Scoped tests, one run, `-count=1` (log `/home/user/workspace/archive-final-gates.log`):
  - `./cmd/agent/` (`-run 'TestArchive|TestCompletion|TestCLI|TestTopLevelUsage|TestTaskCLI|TestMergeStatus|TestMergeHelp|TestMergeRun|TestHelpCapability'`) → `ok github.com/nick/gopi/cmd/agent 20.018s`, `CMD_EXIT=0`
  - `./internal/tools/` (`-run 'TestArchive|TestRepoSweep|TestMergeBuild|TestMergeUnion|TestMergerStatus|TestTaskMerge|TestRepoResolveRow|TestBranchAttribution|TestMergedCleanup'`) → `ok github.com/nick/gopi/internal/tools 254.212s`, `TOOLS_EXIT=0`
  - `./internal/config/` → `ok`, `./internal/standards/` → FAIL, `STD_EXIT=1`
- The standards failure is PRE-EXISTING, reproduced in the main repo on master
  (`cd /home/user/work/GoPi && go test -run TestDocClass ./internal/standards/` → the same
  two failures): (a) ~40 devdoc files carry no `DOC-INDEX.tsv` row, and (b)
  `TestDocClassBannerMatchesManifest: devdoc/daemon-death-pipe-fd-shape.md: class
  "historical" but the file does not open with its banner`. This branch's own new devdoc IS
  classified and bannered (I added its row and its `<!-- doc-class: historical -->` line),
  which is why the banner count moved 656 → 657 with `0 mislabelled`.
- Worktree: `git status --porcelain` printed nothing after the commit (Rule 30).

## Evidence (self-filed)

- Gate — the mechanism refuses a FINISHED owner and an IN-FLIGHT owner and a call with NO
  reason, and writes NOTHING in all three cases:
  `CGO_ENABLED=0 go test -timeout 300s -run TestRepoResolveRowRedProof ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools 3.720s`.
- Gate — the RED observation (the test fails when the gate is neutered), observed, not
  argued: with the two checks in `repoSweepArchiveBranch` disabled, the same command printed
  `--- FAIL: TestRepoResolveRowRedProof (6.42s) archive_reason_test.go:414: RED PROOF: the
  mechanism archived a FINISHED task's branch — that is the burial this gate exists to stop`
  → `FAIL`; the file was restored immediately and the test re-ran green.
- Gate — the human refusal, the override and the backlog reproduction:
  `… -run 'TestArchive|TestRepoResolveRow' ./internal/tools/` → `ok … 47.670s`.
- Gate — the report arm on the real command line (render, positional-arg refusal,
  unreadable listing → non-zero): `ok` in the cmd/agent scoped run above
  (`-run …TestMergeStatus…`, `CMD_EXIT=0`).
- Gate — the old archive readers still hold: the scoped `internal/tools` run above includes
  `TestMergedCleanup…` (the `merge-base --is-ancestor <archive ref> <commit>` reader) and
  `TestRepoSweep…`; the ref SHAPE is pinned by `TestArchiveWriterRecordsAReadableReason`
  (object type `commit`, `rev-parse archive/<branch>` == branch tip).
- Live run (built binary, temp repo, `GOPI_MEMORY_DB`): `gopi archive -h` prints the
  verb usage; a branch owned by a `completed` row is REFUSED with exit 1 and `git tag -l`
  stays empty; the same call with `--reason … --override-completed` writes the lightweight
  tag plus the reason record, and
  `git cat-file blob refs/gopi/archive-reason/<branch>` reads the reason back; the tag is
  `commit`-typed and `rev-parse` equals the branch tip.
- Commit: `f5007adb feat(archive): the WHY on an archive write, and the refusal to bury a
  finished task's branch` on `feat/archive-why-gate-recover`.

