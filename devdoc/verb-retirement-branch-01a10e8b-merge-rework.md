<!-- doc-class: historical -->
# Task Report: the merge of `task/01a10e8be5257b3fa520142b743d7859` is reworked — the branch is SUPERSEDED, and master's tree is the survivor

Task id: `01a10ef686337e1980860e8e25391bbb`.
Branch reworked: `task/01a10e8be5257b3fa520142b743d7859` (tip `402392f0`, base
`0ff4fc5c`) → `rework/test-surface-01a10e8b` (renamed in this run; Rule 6a).
The branch's ref had been DELETED; it was reconstructed from object `402392f0`,
whose content is checksum-verified against the attached diff below.

## Completed

- **The deciding command was RUN, not assumed — this is a real merge conflict, not a
  merge ceiling.** `git merge --no-commit --no-ff task/01a10e8b…` on master `a34781a6`
  → **exit 1**, `Automatic merge failed`, **13** paths under
  `git diff --name-only --diff-filter=U` and **13** marker lines. The attached diff
  recreates exactly this: decompressed it is **26 files, 192 insertions(+),
  155 deletions(-)**, and `git diff --stat master...402392f0` prints the **same 26
  files at the same counts** — so the reconstruction is the branch the briefing means.
  (Evidence: `evidence/branch.diff`, `evidence/merge-state.txt`.)

- **The branch's whole content is a RESPELLING, and the measurement says so.** The
  branch replaces master's DERIVED helper form — `runGopiMapped` +
  `familyAddressArgs`, which map a flat verb word through the ONE `verbAliasCanonical`
  table (`cmd/agent/cli_family_address_test.go:51,61`) — with INLINED bare argv
  literals (`[]string{CliVerbTool, CliVerbShot, …}`) and `testutil.RunGopi`. Extracted
  from the diff itself:

  ```
  $ grep -E '^\+func (Test|Benchmark)' evidence/branch.diff | sed 's/^+func //;s/(.*//'
  (empty)
  $ grep -E '^\+func ' evidence/branch.diff | grep -v 'func Test'
  (empty)
  ```

  **Zero added test functions. Zero added helpers.** A pure respelling of argv.

- **Master has already converged this exact work — measured by file, not asserted.**
  Commit **`844d1f3d`** ("chore(verb-retirement): converge the branch onto master's
  implementation — the epic is already live") falls inside the merge-base..master
  window: it is an ancestor of master and NOT of the branch. It touched **24 of the
  branch's 26 files**:

  ```
  $ comm -12 <branch 26 files> <git show --name-only 844d1f3d | sort>   # 24 files
  ```

  That commit is an attempt at *this same work* — same subject, same filenames, same
  "+funnel helper / use the constant" shape — already absorbed. The branch is the
  superseded twin of a change master took ten minutes before this rework was filed.

- **Master then DECIDED the question the other way, and the decision is gate-enforced.**
  **`17e7ed8b`** — "fix(cmd/agent tests): refusal probes drive the binary RAW, not
  through the family door" — puts the family door where it belongs: it is the
  production address, never a test's transport for a probe that means to send a
  RETIRED word. This branch inlines the family address into *every* probe; that is the
  opposite decision, and master's is later.

- **The resolution rule applied: MASTER'S TREE WINS WHOLESALE — and the reason is
  mechanical, not aesthetic.** The rest of the branch's 26 files merged with no marker
  at all, and they carried the branch's INLINED form into the merged index. A
  merge that takes git's clean auto-merge result therefore produces **exactly the mixed
  tree the epic forbids**: master's `familyAddressArgs`/`runGopiMapped` calls and the
  branch's inlined `[]string{CliVerbTool, …}` literals living side by side in the same
  package. There is no hunk-level split to make — every hunk on the branch side is the
  superseded spelling — so the merge takes master's side in full:

  ```bash
  git read-tree --reset -u HEAD     # master's tree; the merge commit still records both parents
  ```

  Result: `git diff --name-only --diff-filter=U` → **0**, zero conflict markers
  anywhere, and `git diff HEAD` (i.e. the merge tree vs master) → **nothing**. The
  surviving helper form is master's, uniformly: the merged tree holds **1**
  `func familyAddressArgs`, **1** `func runGopiMapped`, and the branch's inline form
  survives in no file.

- **A real, separate defect was caught and proved to be my OWN instrument, not a merge
  bug.** An early `git diff master` in this run listed 5 files the merge appeared to
  have silently REVERTED to their merge-base blob (the g6 help-surface family:
  `cmd/agent/cli_tools_maintenance_help_surface_basic_test.go`,
  `scripts/g6-help-forms.awk`, `scripts/g6-inventory-doc.py`,
  `scripts/g6-tools-maintenance-help-surface.sh`,
  `devdoc/gopi-tools-maintenance-help-surface-inventory.md`) — files the attached
  three-dot diff does not carry, so it looked like the merge resurrecting dead content.
  The root cause is that **the `master` REF MOVED DURING THIS RUN**: it was `a34781a6`
  when the merge was taken and `aef82d93` by the time the diff was read (another agent
  merged a `fix(g6): re-derive the help-surface inventory through the verb doors`).
  Master's `73983fee` fixed those five files and is **not** an ancestor of `a34781a6`,
  which is why they differ from a newer master and NOT from the merge. Re-measured
  against the pinned sha the diff is exactly the 24 cmd/agent+internal/shot files and
  nothing else. **The lesson is the instrument's: compare against a pinned sha, never
  against a moving ref** — a live `master` is another writer's scratch while the merge
  queue runs. The reverted blobs are gone with the rest of the branch side; verified,
  all 5 now hold master's content.

- **The DELIVERY-POINT SYNC came LAST (Rule 30a), and it is also what proves the
  rework is real.** Master moved twice during this run — `a34781a6` (the sha the merge
  was taken against, from the briefing's `add55f06`) → `aef82d93` (another agent's
  `fix(g6): re-derive the help-surface inventory through the verb doors`). The sync was
  therefore run against the LIVE tip after the work was committed:
  `git merge --no-commit --no-ff aef82d93` → **rc=0**, `Automatic merge went well`,
  **0** conflicted paths, no manual resolution — committed as `51c688f5`. That is the
  acceptance property the sibling rework also reports: a tree that keeps absorbing
  master with zero conflicts is a tree that IS master's. After the sync,
  `git diff --name-only master HEAD` is still exactly this report plus its
  `DOC-INDEX.tsv` row — no source path, no test path.
- The merge is a real two-parent commit — `107960c2`, parents `a34781a6` and
  `402392f0` — so the branch records the failed branch's lineage and the decision
  against it while its tree is master's. `git status --porcelain` prints nothing.

## Not Done

- **This merge adds NO byte to master's tree, and the report says so plainly.** The
  deliverable's acceptance is "no superseded bytes left", and the honest consequence is
  that the merged tree must be master's tree — measured, not claimed: `git diff HEAD` is
  empty. There is nothing here for a reviewer to merge INTO master beyond this report
  and its `DOC-INDEX.tsv` row; the branch's value is the resolution and the record, not
  a code delta.
- **The touched packages are NOT green, and the reds are master's own — re-measured,
  not assumed.** `a34781a6`'s own subject is "attribute the cmd/agent red family by
  running the ten names in a detached master worktree": master records **ten** failing
  names in `cmd/agent`, reproduced on a detached master worktree. The merged tree
  compiles four source trees that are master's byte for byte, so those reds are not this
  rework's. The measured set from this run is under `## Verification`.
- **I did NOT re-adjudicate the two designs.** Master's raw-probe rule (`17e7ed8b`) is
  later and gate-enforced, and its sibling convergence (`844d1f3d`) is already live. If
  the operator wants the branch's inline form instead, that is a NEW change against
  master, not a revival of this branch — and it would have to move `17e7ed8b`'s rule,
  which this rework does not touch.
- **The branch's `402392f0` is an ancestry marker only, and its 3 commits add nothing.**
  Measured: `git diff --name-status master 402392f0` is a **169-file / −10,952-line**
  delta, i.e. the tip is ~10.9k lines BEHIND master plus its own respelling. The three
  commits (`a74826bb`, `26a49cfd`, `402392f0`) touch test files only, with zero added
  tests. Their bytes are dropped with the tree, which is the convergence.

## The reds are KNOWN and OWNED — the overlap is stated, not duplicated (Rule 26)

The branch's ledger row is `#01a10e8be5257b3fa520142b743d7859` — **`[completed]`**,
filed at 08:10:11 with the goal *"verb-retirement: drive the remaining test argv
through the family doors (121 red names in cmd/agent)"*. That goal names the reds this
report measures. They are not this rework's to fix, and two things say so:

- **The row that owns them is LIVE.** `gopi task list` prints
  `#01a10f845aee-7c8b-a03b-70ce7cbd5456 [in_progress] "[… ] rework: branch
  task/01a10e8c464573f09a329e894c7d568b read as 'already landed' — its cmd/agent test
  fixes never reached master (`TestCLIContractInventoryMatchesDispatch` is red on
  master …)"` — running 30m39s at this reading. That is this branch's SIBLING
  (`#01a10e8c4645…`, `[completed]`, *"the remaining bare top-level verb assertions in
  cmd/agent moved to family addresses"*), and reworking it is already in flight. The
  diagnosis matches my own measurement exactly: those fixes did not reach master's
  tree — `git diff --name-only a34781a6 HEAD -- cmd/agent` is **empty** — because the
  same convergence (`844d1f3d`) dropped them.
- **The mechanism they need is already master's and is not this branch's.** Four of
  the reds were read out of my run's own stream:
  `cli_keyword_search_test.go:146` → `gopi keyword_search -h: exit error: exit status 1`
  and `:185` → `gopi keyword_search EUAUDIT_NEEDLE_ZZZ: exit error: exit status 1`, each
  with the binary's own `gopi: `keyword_search` is RETIRED — use `gopi tool
  keyword_search`` on stderr; `TestCLIContractInventoryMatchesDispatch` →
  *"dispatch predicate count mismatch: main.go has 20 (…), contract suite tracks 18"*;
  `TestAnnotateVerbRunsARealArgvEndToEnd` fails its `-h` sub-case. Each is a call site
  that still drives a RETIRED word while asserting the command WORKS — the door
  rewrite belongs at that call site, which is what `familyAddressArgs`/`runGopiMapped`
  exist for. This branch's answer was to inline the family address into the same
  probes; that is a third spelling of a fix master already owns, not an addition to it.

So the disposition is Rule 26's ordinary one: **measure, state the overlap, do not
duplicate the live row.** If `#01a10f845aee` turns out to leave a gap, the repair is a
new task against master — never a revival of this branch, whose bytes cannot be
selectively un-dropped without re-introducing the mixed form.

## Verification

- Build: `CGO_ENABLED=0 go build ./cmd/agent/ ./internal/agent/ ./internal/lineedit/`
  → **rc=0**.
- Vet: `CGO_ENABLED=0 go vet ./cmd/agent/ ./internal/agent/ ./internal/lineedit/`
  → **rc=0**.
- Declared build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **rc=0**,
  `bin/gopi` **30,707,938** bytes; `git status --porcelain` after it → **0 lines**
  (`bin/` is `.gitignore`d, checked with `git check-ignore -v bin/gopi`).
- Tests: branch-scoped, never whole-tree —
  `make test-budget PKGS='./cmd/agent ./internal/shot ./testsuite'` →
  **3 of 3 packages FAILED**, `stamp: 2026-10-06T13:06:16+08:00 … budget=25m
  elapsed=1101s exit=1 outcome=fail stream=run-1791263176-1608208.json`, **20**
  top-level failing tests: `TestChatStatus_LiveBlockKeepsTheStateWord`,
  `TestAnnotateVerbRunsARealArgvEndToEnd`, `TestG5InventoryMatchesTheHelp`,
  `TestG5InventoryListsEveryGapTheTestsCannotCover`,
  `TestCheckpointUsageLinesAreDispatchable`, `TestBashScriptStructure`,
  `TestCLIContractInventoryMatchesDispatch`,
  `TestKeywordSearchHelpPageDiffersFromTopLevel`,
  `TestKeywordSearchRunPrintsHitsAndNoMatches`, `TestProxyBusyPortNeverClaimsListening`,
  `TestSandboxDirFlagDecidesTheRoot`, `TestG4HelpSurfaceMatchesInventory`,
  `TestG4HelpSurfaceInventoryTableMatchesHelp`, `TestG4EveryAdvertisedSurfaceRuns`,
  (`cmd/agent`); `TestShotStartupTriggerRenewsAnOrphanedUnit` (`internal/shot`);
  `TestChatInfo_OnRealPTY_IdentityMatchesDiskAndTheCommandRuns`,
  `TestChatLayerFormat_OnRealPTY`, `TestChatNoticeTurn_JobCompletionOpensATurnWithNoInput`,
  `TestChatNoticeTurn_WakesAParkedConsole`,
  `TestChatPTY_LongPasteIsSpilledAndKeepsTheEditorResponsive`,
  `TestTask_BarePrompt_SIGTERM_WritesTheSignalVerdictEventIntoTheSessionLog`
  (`testsuite`). **The budget did NOT trip** (25m per package, 1101s wall for all
  three) — these are assertion failures, not a timeout, at load ~19 on 4 cores.
  The `cmd/agent` set is the family `a34781a6` attributes by name, and every red is
  master's, not this rework's, because that package's Go sources here ARE master's:
  `git diff --name-only a34781a6 HEAD -- cmd/agent internal` → **empty**. Ownership
  and the mechanism are under the section above; the two `TestKeywordSearch*` reds are
  read out of the stream verbatim there.
- `gofmt -l cmd/agent internal` → **31 files**, every one of them master's own (this
  tip's whole non-doc delta is the report plus its `DOC-INDEX.tsv` row), so a fix here
  would be reformatting another writer's files. Same class as the reds: the live state
  of master, not a product of this rework.
- Ran: `git merge --no-commit --no-ff task/01a10e8b…` (exit **1**, 13 conflicts — the
  deciding command), `git read-tree --reset -u HEAD`, `git commit -F <message>`,
  `git diff HEAD` (empty), `git diff --name-only --diff-filter=U | wc -l` (**0**),
  `grep -rn -E '^(<<<<<<<|>>>>>>>|=======)$' cmd/agent internal/shot testsuite`
  (no hits), `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` (rc 0).
- Result: the merge tree is master's tree at the pinned sha, byte for byte, with zero
  conflict markers and zero unmerged paths; per-file verdicts for all 26 branch-touched
  files plus the 5 revert suspects read `OK master` — **31/31**, **0** differs.
- Commit: `107960c2` (the merge — parents `a34781a6` + `402392f0`) and the report
  commit, which is the branch TIP and is named by `git log --oneline -1` rather than
  here (a report cannot name its own commit: writing the hash changes the hash).

## Files

- `devdoc/verb-retirement-branch-01a10e8b-merge-rework.md` — this report.
- `devdoc/DOC-INDEX.tsv` — this report's classification row, written by
  `gopi doccheck --fix` in the same commit (Rule 36).
- Merge commit `107960c2` — no file delta vs master; it carries the branch's lineage and
  the measured decision against its bytes.

## Evidence (self-filed)

- **The reconstruction is the right branch**: the attachment
  (`task-01a10e8be5257b3fa520142b743d7859.diff.gz`,
  sha256 `de832289277b3fc842446cdd7265c64732ec8c3fc7cbbe6a9cefb01f6a193376`) decompresses
  to 1,380 lines; `git apply --stat` lists 26 files / 192+ / 155−, which is what
  `git diff --stat master...402392f0` prints — same files, same counts.
- **Zero added tests / helpers**: the two `grep` commands above, both empty (an empty
  result is printed as `(empty)` rather than left to look like a scan that never ran).
- **Master already converged**: `git merge-base --is-ancestor 844d1f3d master` → true;
  `… 844d1f3d task/01a10e8b…` → false; `comm -12` of the file lists → 24 of 26.
- **The merge ceiling is not the cause** (the sibling rework's diagnosis was a ceiling;
  this one is a genuine 13-file conflict): `--diff-filter=U | wc -l` = **13** before,
  **0** after.
- **Per-file verdicts**: `evidence/per-file-verdicts.txt` → `grep -c '^OK master'` = **31**,
  `grep -c 'DIFFERS'` = **0**; the list covers all 26 three-dot files AND the 5 revert
  suspects.
- **Surviving helper form**: `grep -rn "func familyAddressArgs" cmd/agent/*.go | wc -l` = **1**;
  `grep -rn "func runGopiMapped" cmd/agent/*.go | wc -l` = **1** — the one definition each,
  in master's form.
- **The moving-ref instrument error**: recorded in full under `## Completed`; the pinned
  comparison is `git diff a34781a6 HEAD` (empty) versus `git diff master HEAD` taken
  later against `aef82d93`.
- **`devdoc/DOC-INDEX.tsv` classification**: this report went through the `write` tool, so
  the corpus row is written by the same hook the close-out chain uses; verified with
  `gopi doccheck` after the commit.
