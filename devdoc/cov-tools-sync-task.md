<!-- doc-class: historical -->
# Task Report: function-level coverage for the peer-sync task face (DefaultGitPushback)

Epic: `feat/cov-tools-sync`. This report covers `memory_sync_task.go`'s one
target function: the exported default git push-back.

New test file: `internal/tools/memory_sync_task_gitpushback_cov_test.go`.

## Completed

| Function | Definition (code_search) | Tests added | Boundary / error cases named |
|---|---|---|---|
| `DefaultGitPushback` | internal/tools/memory_sync_task.go:1448 | `TestDefaultGitPushbackSkipsIntegratedPushesUnmerged`, `TestDefaultGitPushbackNothingUnmergedPushesNothing`, `TestDefaultGitPushbackRefusesInvalidPeer`, `TestDefaultGitPushbackUnresolvableRepoErrors`, `TestDefaultGitPushbackUnreachablePeerFailsSoft` | already-integrated branch SKIPPED (never re-pushed) while the unmerged deliverable crosses; server master tip unchanged; nothing-unmerged flush pushes nothing and reports no failure; `""`/blank/metacharacter aliases refused (`invalid peer`, nil report) while a padded valid alias is trimmed and accepted; non-repo cwd → `resolve repo` error, nil report; unreachable peer → nothing pushed, failure reported |

Caller (usage verified): `cmd/agent/cli_tasks.go:2833` —
`return tools.DefaultGitPushback(opts.peer)` (the harvest git push-back
phase's default pusher; tests inject their own via
`HarvestOptions.GitPushback`, which is why the exported face had no test).

## Invariants asserted

* **Never resurrect a deleted ref (2026-09-02 stale-flood incident)**: a
  branch whose tip is already an ancestor of the peer's master is SKIPPED —
  re-pushing it would recreate a ref the server deliberately deleted after
  adopting it into master. Asserted end to end against a real bare repo: the
  integrated branch appears in `Skipped`, never in `BranchesPushed`, and the
  peer's `refs/heads/master` is byte-identical before and after the pass.
* **Truly-unmerged work still crosses**: the deliverable branch lands on the
  peer with a matching tip (the guard must not be so strict that it blocks
  the actual delivery).
* **Nothing-unmerged is a NO-OP, not an error** (requirement (d)): an empty
  flush pushes nothing, reports no failure, and leaves the server untouched.
* **Blind push is forbidden**: if the server-master fetch fails the flush
  must not push anything — unprovable merge-ness must never degrade into
  "push everything and hope".
* **master is asymmetric**: the client never pushes master (excluded from the
  plan entirely), so a client can never force-rewrite the integration branch.
* **The report is print-safe**: `BranchesPushed`/`PushFailed` are non-nil
  even when empty (the CLI printer contract).

## Not Done / Deliberate gaps

1. **The collision-rename branch (`gitPushbackPushOne` retry under
   `<name>-<HHMMSS>`) is not exercised.** Reaching it requires the peer to
   hold a DIFFERENT branch of the same name whose tip is not an ancestor —
   constructible, but the rename path then depends on `time.Now()`'s
   `pushbackRenameSuffixLayout` and would assert a wall-clock-derived name.
   I chose determinism over touching that branch (it is also already covered
   at the plan level by `task_sync_git_pushback_test.go`'s injection seam,
   per its own doc). Recorded here rather than faked.
2. **Real ssh transport is not exercised** (epic rule: no network tests). The
   peer target is the literal `<path>:~/work/GoPi` string the product
   composes (`harvestPeerRepo`), with `<path>` pointing at a LOCAL bare repo:
   git resolves it as a path and the whole plan (list → fetch master →
   per-branch `merge-base --is-ancestor` → push) runs for real. The ssh hop
   itself is `TestClaimSSHExchangeUnreachableFailsSoft`'s territory.
3. `gitPushbackLoadBranches`' cap (`gitSyncMaxBranches`) is not driven — a
   20-branch fixture would add seconds for a constant that is already named
   (Rule 1) and asserted nowhere else. Low value, recorded.

## Verification

- `go vet ./internal/tools/` → clean.
- Raw target run:
  `go test -count=1 -run 'TestDefaultGitPushback' ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools 0.434s`
- Coverage: `DefaultGitPushback` gone from
  `python3 scripts/func_coverage.py --pkg internal/tools --uncovered`.
  Package 148/181 = 81.8%.
- Two-run full-suite raw output: `devdoc/cov-tools-sync-summary.md`.

## Determinism

No network, no ssh, no sleeps: two LOCAL git repos (a working client and a
bare peer) plus the literal target-path form the product composes. Every
fixture commit is made with pinned `user.email`/`user.name` and
`commit.gpgsign=false`, so no ambient git config can change object ids. The
test chdirs into the client repo (the push-back resolves the repo as the
process CWD by contract, `gitPushbackDefault`) and restores the previous
directory via `t.Cleanup`. Timestamps are never asserted; the two
tip-equality checks compare `git rev-parse` outputs, which are content
derived, not clock derived.

## Workflow feedback

* Building the peer as a literal `"<path>:/home/user/work/GoPi"` directory
  was the key trick: it exercises `gitPushbackDefault`'s REAL target
  composition (the `fmt.Sprintf("%s:%s", peer, harvestPeerRepo)` line) with
  git's own local-path parsing, so the test would fail if the target format
  changed — a fake injected pusher would not.
* First draft of the fixture pushed to the bare alias path while the product
  fetches/pushes through the colon-suffixed target, so every fixture push
  failed with "does not appear to be a git repository". Fixing the FIXTURE
  (push to the same literal target the product uses) rather than relaxing the
  assertion kept the test honest.
