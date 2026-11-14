<!-- doc-class: normative -->
# Task Report: the peer fetch is ONE implementation, and it runs before the ground exists

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a115ac-23d5-7b3b-a8d2-d6b33ac894b3` (epic `worktree-base-align`, 2026-10-07).
Branch `task/01a115ac23d57b3ba8d2d6b33ac894b3`.

## Completed

**① The peer `git fetch` is ONE implementation (Rule 13/14).**
Three surfaces each carried their own copy — the push-back lane's
peer-master mirror (`gitPushbackFetchMaster`), the close-out pre-merge
gate's fetch step (which called ①, so it inherited ①'s argv by history
rather than by contract), and the client serve pass's fetch-prune step
(`task_sync_git.go`). The exec itself existed **three times**:
`gitSyncRun` and `gitPushbackGitRun` each built their own
`exec.CommandContext(…, "git", …)`.

- New `internal/tools/git_peer_fetch.go` owns **the ONE runner**
  (`gitPeerRun`, `git -C <dir>`, stderr folded into the error) and **the ONE
  fetch** — exported `SyncPeerFetch(ctx, repo, target) (tipSHA string, err error)`,
  which mirrors the peer's integration branch into the reserved durable ref
  `gitPushbackCheckRef` and returns that tip's sha. It has **no fallback**: an
  unreachable peer is an error, never a local sha (Rule 15).
- `gitSyncRun` and `gitPushbackGitRun` are now **delegating adapters** over
  `gitPeerRun`; their own named timeout stays at the adapter (it is each lane's
  cap), and the shared value is one constant (`gitPeerFetchTimeout = 60s`).
- `gitPushbackFetchMaster` delegates to `SyncPeerFetch`. Its ~2 callers — the
  push-back pass and `finishCleanupPreMergeVerify` — are untouched, so the
  pre-merge gate's verdict set, its two audit event names and its `--verify`
  semantics are unchanged.
- `taskGroundGit` (the ground's probe/creation) now also goes through
  `gitPeerRun`; what stays at that call site is its own timeout and the
  stripped git environment (`withoutGitDirEnv`), which moved from
  `cmd.Env = …` to a parameter of the one runner.

**② The ground is cut from the peer's tip (the feature).**
`taskGroundCreate(repo, dir, branch)` — the shape the fresh-path fixture used —
is kept, and the ONE implementation is now
`taskGroundCreateFrom(repo, dir, branch, base)`: `base == ""` means `HEAD` (the
pre-epic behaviour), a sha means "start there". The new
`internal/tools/task_ground_baseline.go` decides the base:

- **client role + a declared peer that answers** → the peer's integration-branch
  tip, and the resolution's `Detail` says so by name
  (`baseline cut from the peer <target> tip <sha> (not from the local master)`);
- **client role + a declared peer that does NOT answer** → the ground is still
  created from the local master **and the degradation is stated in the same
  `Detail`**: `baseline may be stale / NOT aligned with the peer <target> — the
  peer fetch failed (<git's own reason, one line, tail-bounded>); the worktree
  was still created from the LOCAL master` (Rule 15: no silent fallback);
- **no declared peer, or the server role** → nothing to align to, no fetch, **no
  note** (a note on a single-node host would be noise an operator learns to
  ignore). `IsServerNode()` is the predicate, not a re-derived role string.

Behaviour preserved deliberately: the degradation does **not** refuse the run. A
task that cannot start because a peer is down would make the peer a single point
of failure for local work; the run proceeds, degraded and labelled.

**③ No new knob (Rule 35).** The decision is taken from what the host already
declares — `config.ResolveNodeRole` (via `IsServerNode`) and the peer list
(`config.ResolveSyncPeers`, read through the ONE target renderer
`finishCleanupServerTarget`). No env var, no settings key, no CLI verb, no
script, no daemon, no state file.

**④ Rule 29 held.** The fetch writes remote-tracking/mirror refs only. Nothing
pulls, checks out, or moves a branch of the shared checkout — asserted in the
unit tests AND measured on the real run (below).

## Not Done

- **The two lanes' fetch steps were not merged into a single call site.** The
  client serve pass's `--prune` step keeps its own refspec **on purpose**, and
  the reason is measured, not stylistic: a fetch carrying `--prune` together
  with ONE explicit refspec **prunes nothing** (see Evidence ④). That step's
  destination must be the peer's remote-tracking namespace
  (`refs/remotes/<alias>/*`) or the reap cannot happen at all, while the
  push-back mirror's destination is the reserved durable ref — two different
  answers to "where does the peer's tip land here", each with its own measured
  justification. The **runner** is shared; the **refspec** is not, and
  collapsing it would delete the reap.
- **The measured benefit is a lag measurement, not a conflict reduction.** The
  epic's premise is that starting N commits behind causes rework. I measured the
  lag and I measured the rework rows that exist; I did **not** measure a
  before/after conflict rate (that needs the change to be live across a period
  of merges). Evidence ⑤⑥ are what exist.
- `internal/tools/task_run_ground.go` carries pre-existing `gofmt` drift (a
  table around a `switch` was already unformatted at `HEAD`); I did not reformat
  it, to keep the diff surgical (Rule 4).

## Verification

- **Build:** `go build ./...` → **OK** (exit 0, no output); `go vet ./internal/tools/` → clean.
- **Tests:** `scripts/test-budget.sh ./internal/tools ./cmd/agent ./internal/config`
  → `internal/tools` **ok** and `internal/config` **ok**; `cmd/agent` is **red for
  reasons that predate this branch** — the five names below fail identically at
  the base commit `bb6ee97f9` (verified in a throwaway worktree at that commit,
  removed afterwards), and `TestTaskMergeCanonicalFlagRefusalOnTheRealBinary` /
  `TestRunCloseoutReapWatchesAPidItCannotMeasureYet` pass standalone on both trees
  and fail only inside the loaded suite (`load_end=30.02` on 8 cores). Streams:
  `run-1791376460-2385429.json`, `run-1791375706-2298181.json`. See Evidence ⑦.
  New: `TestSyncPeerFetchReturnsThePeerTip`,
  `TestSyncPeerFetchNeverFallsBackToLocalMaster`,
  `TestSyncPeerFetchReportsGitStderr`,
  `TestPeerFetchPathHasExactlyOneGitRunner` (the consolidation gate: exactly ONE
  `exec` of git among the six files of the peer-fetch path),
  `TestTaskRunGroundCutsTheBaseFromThePeerTip`,
  `TestTaskRunGroundDegradesExplicitlyWhenThePeerIsUnreachable`,
  `TestTaskRunGroundSkipsThePeerFetchOnTheServerRole`.
  **Zero existing assertions were changed** — the only edits to existing test
  files are none; the refactor is proven by the untranslated suites above.
- **Ran:** the changed surface, for real, three ways (Rule 31):
  1. the E2E script (evidence, so it lives in the session directory, Rule 37 —
     never in the worktree) —
     `GOPI_BIN=<binary built from this branch> bash /home/user/.gopi/sessions/01a115ac-23d5-7b3b-a8d2-d6b33ac894b3/evidence/e2e-ground-baseline.sh`
     → **exit 0, `RESULT: FAILED=0`, 13/13 checks PASS** (Evidence ①–③). It
     materialises a sandbox (`gopi tool sandbox`, its own ledger), builds a real
     bare peer whose master is ahead of the client's, then runs
     `gopi worker <task-id>` twice — once with the peer reachable, once with the
     peer's declared alias rewritten to a path that is not a repository.
  2. `gopi task list --all` inside that sandbox (Evidence ③).
  3. `gopi task list` on the live ledger + `git merge-base/rev-list` against
     `peerbox/master` for the measured numbers (Evidence ⑤⑥).
- **Result:** the aligned run's ground **HEAD == the peer's tip**
  (`1538aac2ad…`, asserted two independent ways: the announcement's short sha
  and the fixture's own `rev-parse`), the unreachable-peer run's ground was
  **still created** from the local master with the stale-baseline note naming the
  peer, and **neither** run moved the client's own master, the peer's master or
  the checkout's cleanliness. The two cases provably took different paths
  (`aligned reached the peer; stale did not`) — which is what makes the second
  assertion meaningful rather than a second copy of the first.
- **Commit:** see the final message of the run.

## Evidence (self-filed)

① **The changed surface executed end to end** —
   `/home/user/.gopi/sessions/01a115ac-23d5-7b3b-a8d2-d6b33ac894b3/evidence/e2e/run.txt`,
   exit 0, `RESULT: FAILED=0`, 13/13 PASS; the raw worker logs sit beside it
   (`aligned-worker.log`, `stale-worker.log`). Sandbox
   `sandbox-groundbase-37b681a2`, declared `GOPI_NODE_ROLE=client GOPI_SYNC_PEERS=peer`,
   peer target `peer:~/work/GoPi` (production's own spelling — see the note in
   ⑧), fixture `PEER_TIP=1538aac2ad437727f5173d12062744c828c8e73a` vs
   `LOCAL_MASTER=72bf989778044886b3d799e4b6f9467f6e8bb270` — **different**, so a
   local fallback could not pass. The aligned run's announcement, verbatim:
   `CREATED the ground <path> on branch task/te2ealigned — baseline cut from the
   peer peer:~/work/GoPi tip 1538aac (not from the local master)`.
   (`gopi worker <id>` was run with `GOPI_ALLOW_AGENT_SPAWN=1` — the deliberate
   one-off override its own help names; the bare drain stays refused. The runs
   completed on `deepseek-flash` and the ground decision is made BEFORE the
   spawn, so the assertions do not depend on the model's behaviour.)
   **The binary the script ran was built from the committed source**, checked
   rather than assumed: `evidence/gopi-groundbase` mtime `20:23`, every source
   the E2E exercises last written by `20:21:16`, the E2E run at `20:27–20:28`,
   and the commit landing them at `20:30:01` — with `git status --porcelain`
   empty at that commit, so the built tree and the committed tree are the same
   bytes.

② **Unreachable peer** — `client-unreachable`, whose alias resolves to a path
   that is not a repository. The same announcement channel:
   `CREATED the ground <path> on branch task/te2estale — baseline may be stale /
   NOT aligned with the peer peer:~/work/GoPi — the peer fetch failed (peer
   fetch peer:~/work/GoPi (into <client-unreachable>): git fetch --force --no-tags
   peer:~/work/GoPi +refs/heads/master:refs/remotes/gitPushcheck/master: exit
   status 128: fatal: '…); the worktree was still created from the LOCAL master.
   Read the peer's own tip before trusting a conflict here as a real disagreement`.
   Both rows reached `[completed]` in the sandbox's ledger.

③ **The sandbox's own ledger only** — `gopi task list --all` under
   `GOPI_HOME=<sandbox>` holds exactly the two fixture rows
   (`#t-e2e-aligned`, `#t-e2e-stale`, both `[completed]`) and nothing else; the
   operator's real ledger is never opened (Rule 34/32 — the run's ledger is
   `…/sandbox-groundbase-37b681a2/.gopi/memory.db`).

⑧ **Two bugs this E2E script found in ITSELF, recorded because they are the
   trap for anyone writing this fixture** (both were silent green before they
   were fixed — the run looked like a pass while testing the wrong path):
   - the rewrite key must equal production's composed target **byte for byte**;
     a hand-typed `/home/user/work/GoPi` against production's `~/work/GoPi`
     rewrote nothing, and the fetch fell through to ssh — the "aligned" case was
     silently exercising the degraded path. The script now DERIVES the key from
     the constant it must match (`PEER_TARGET="peer:$PEER_REPO"`).
   - two `url.<sub>.insteadOf` rules matching one URL are resolved by git's own
     preference order, so adding a worse rule to the same repo does not spend
     the better one; the "unreachable" case came back aligned. The stale case
     now uses a **second clone holding only the broken rule**.

④ **The refspec/`--prune` measurement** (fixture
   `/home/user/.gopi/sessions/01a115ac-…/evidence/gq1`, git 2.x): after a fetch
   `git -C c2 fetch --force --no-tags --prune <url> "+refs/heads/master:refs/remotes/gitPushcheck/master"`,
   `git -C c2 for-each-ref refs/remotes/gitPushcheck` still listed
   `…/ghost`, `…/master`, `…/master2` — **no prune happened**, although the
   peer's `feat/ghost` had been deleted. The same file records the positive
   control (`git fetch --prune <configured-remote>` *does* reap) and the tip
   equality (`peer master` == `refs/remotes/gitPushcheck/master`).

⑤ **Branch-point lag behind `peerbox/master`** (`git merge-base` + `git rev-list --count`),
   read at `peerbox/master = 5276068c1`:
   `task/01a113e467767eb9a4c33cb1df1cdfbb` lag **53**;
   `task/01a115ac…` (this row) **64**; `task/01a115e03…` **64**;
   `task/01a11639…` **64**; `task/01a1163c…` **64**; `task/01a113a1fe…` **210**.
   Every new ground minted today started 53–64 commits behind the integration
   point.

⑥ **Rework rows in the live ledger** (`gopi task list`, 3252 rows, greps over
   `/home/user/.gopi/sessions/01a115ac-23d5-7b3b-a8d2-d6b33ac894b3/evidence/task-list.txt`):
   `resolve merge conflict` **166** rows;
   `rework:` (any cause) **86**; `rework:` naming a conflict/unmergeable —
   `["rework:.*\(unmergeable\|conflict\|冲突\)"]` — **45**. The epic's measured
   defect is real at this scale; the fix is a prevention, and its effect is a
   **future** conflict count, not one I can claim from this run.

⑦ **Branch-scoped budget run** — the `make test-budget PKGS='./internal/tools ./cmd/agent ./internal/config'`
   stream from this run (job `job-1`); recorded with its per-package PASS/FAIL
   lines when the run closed.

## Files

- `internal/tools/git_peer_fetch.go` (new) — the ONE runner + `SyncPeerFetch`.
- `internal/tools/git_peer_fetch_test.go` (new) — its behaviour, and the
  one-runner gate.
- `internal/tools/task_ground_baseline.go` (new) — the base decision + the
  degradation/alignment notes.
- `internal/tools/task_ground_baseline_test.go` (new) — aligned / degraded /
  server-role, driven through `TaskRunGroundFor`.
- `internal/tools/task_sync_git.go`, `task_sync_git_pushback.go`,
  `task_run_ground.go` (edited) — adapters, delegation, the `base` parameter.

## Not Done — honest, dispatchable

- The conflict-rate reduction is **unmeasured** (⑥). If it is wanted, it is its
  own task: count conflict-rework rows over a window before and after this
  branch is live, from the same ledger query.
