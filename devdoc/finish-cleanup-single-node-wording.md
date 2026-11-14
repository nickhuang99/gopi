<!-- doc-class: historical -->
# Task Report: finish-cleanup — the no-server reason names what it MEANS (single-node vs settings slip)

Branch: `fix/finish-cleanup-single-node-wording` (worktree `~/workspace/gopi-cleanup-singlenode`,
base `master` @ `6a105a48`).

## Completed

**The defect (wording, not behaviour).** On a host with `"sync_peers": []` (the single-node
shape) a row carrying a **deliverable branch** was closed out with the reason

```
no sync_peers resolved — no server to push to
```

Every word true, and the reader learns nothing that matters: a *deliverable branch* on a
single-node host is never handed off to a server **at all** — not "the push failed", not
"retry later". The sentence reads like a transient condition. The same blind spot produced the
second shape: a row whose recorded worktree was **removed by hand** (while the push was being
skipped) surfaced as a HYGIENE refusal —

```
git status: chdir <gone worktree>: no such file or directory
```

— i.e. "refused for dirtiness" standing in for "never delivered". A never-pushed branch was
therefore filed as tidy-up debt.

**The change (surgical, wording only).**

1. `internal/tools/task_finish_cleanup.go` — the empty-push-target arm now renders its reason
   through `noServerTargetSkipReason(base)`, which reads the **two causes apart** by asking the
   config whether ANY peer is declared (`config.HasSyncPeers()`, the single-node gate):

   | condition | appended note |
   |---|---|
   | no peer declared (single-node) | `single-node: sync_peers empty — this branch is NOT handed off to any server` |
   | peer declared, alias unusable (option-shaped/malformed) | `a sync peer IS declared but its alias is unusable (option-shaped/malformed ssh alias) — fix sync_peers` (the key text comes from `config.SyncPeersSettingsKey`, whose value is the settings key `sync_peers` — both notes compose the key from that constant, never a typed literal) |

   plus the constant `finishCleanupNoServerTargetBase` for the unchanged recognisable head
   (`no sync_peers resolved — no server to push to` — kept verbatim so existing readers/tests
   still match) and `finishCleanupReasonSeparator` for the joins. All new wording lives in named
   constants (Rules 1b/13); the settings key comes from `config.SyncPeersSettingsKey`, never a
   typed literal.

2. `finishCleanupGoneWorktreeNoPeersNote(diagnostic)` — appended to the `git status:` refusal at
   all **three** sites that report it (the client chain, the server/single-node recycle, and the
   injected-target seam). It fires **only** when the failure really is a missing directory
   (`finishCleanupMissingDirReason`) **and** no peer is declared, so the client arm is untouched
   (there the push report itself carries the delivery state).

3. `internal/tools/task_sync_git.go` — `GitSyncClientPass` reports the same condition through the
   same renderer (Rule 14: one renderer, both faces; its old text `no sync peer (no server to
   push to)` was a second spelling of the same fact).

**The refusal is unchanged — proven, not asserted.** Both arms still `Skipped = true`, still
return `nil` error, and the tests physically check that the worktree directory and the branch
still exist afterwards. Nothing was removed, no guard was relaxed, no code path was made to
proceed. `FinishCleanupReportSettled` classification is untouched and re-asserted (the
gone-worktree skip still reads *settled*, not *refused*).

## Not Done

- `GitSyncClientPass`'s head text changed from `no sync peer (no server to push to)` to the shared
  `no sync_peers resolved — no server to push to` + note. It was a duplicate spelling of the same
  condition; nothing asserted the old string (checked: no test matches it). Flagged here because
  it is a user-visible string change beyond the minimum.
- The malformed-peer arm reports *that* the alias is unusable, not *which* alias — the peer list
  is not carried through `finishCleanupServerTarget()` (it returns a string). Left alone: naming
  it would mean changing that function's contract, which is beyond a wording-only task.
- No live end-to-end run of the sweep was produced (it needs a populated ledger); verification is
  the package test suite plus the fixture-driven assertions in the new tests.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- Tests (new + the neighbouring behaviour locks):
  `go test ./internal/tools/ -count=1 -run '…' -v` →
  `TestFinishCleanupNoServerTargetNamesTheCondition` PASS,
  `TestFinishCleanupGoneWorktreeNamesTheUndeclaredPeer` PASS,
  `TestFinishCleanupGoneWorktreeNamesTheCause` PASS,
  `TestFinishCleanupSettledIsNotARefusal` PASS,
  `TestFinishCleanupChainCleanWorktree` PASS,
  `TestGitSyncClientPassNoPeerSkips` PASS,
  `TestWorktreeOwnershipServerRecycle*` (4) PASS → `ok github.com/nick/gopi/internal/tools`.
- Full package: `go test -count=1 ./internal/tools/` on the branch → **4 failures, all
  pre-existing host-geometry failures, none ours** (proven by baseline, below).
- Commit: on `fix/finish-cleanup-single-node-wording` (worktree `~/workspace/gopi-cleanup-singlenode`):
  - `5ee57565 fix(finish-cleanup): name the single-node no-peer condition in the skip reason`
  - `749c5732 docs(finish-cleanup): record the baseline-controlled full-package verification`
  - `2349e059 refactor(finish-cleanup): take the settings key from config.SyncPeersSettingsKey` —
    both no-server notes compose the key from `config.SyncPeersSettingsKey` instead of typing
    `sync_peers` (Rule 1b/13); rendered text unchanged, rebuild + the wording tests re-run green.
  - `0863c310 test(finish-cleanup): pin the same reason split on the git-sync face` — the second
    reporting face (`GitSyncClientPass`, the serve tick / `gopi task push`) is now covered too.

### Re-verified on the delivery head (`2349e059`)

`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK;
`go test ./internal/tools/ -count=1 -run 'TestFinishCleanup|TestGitSyncClientPassNoPeerSkips|TestWorktreeOwnershipServerRecycle'`
→ `ok github.com/nick/gopi/internal/tools`.

### Full-package failures: baseline-controlled (branch-introduced = 0)

`go test -count=1 ./internal/tools/` reports FAIL on this host both **with and without** this
change. A clean `master @ 6a105a48` worktree (`~/workspace/gopi-master-baseline`) was run through
the identical command, and the failure sets are BYTE-IDENTICAL (differing only in the scratch-run
directory name):

| failing test | master baseline | this branch | cause |
|---|---|---|---|
| `TestMergeBuildGate_GoCacheLeavesNoScratchCache` | FAIL | FAIL | not ours |
| `TestSuiteScratchRootIsMemoryBacked` | FAIL | FAIL | scratch root on ext4/3, not a memory fs |
| `TestScratchRootFilesystemClassification` | FAIL | FAIL | same |
| `TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy` | FAIL | FAIL | not ours |

So the branch introduces **zero** new failures: the four are host/environment facts (`/home` is
block-backed on this host, so the scratch-tree assertions cannot hold here) plus one pre-existing
worktree-sweep expectation, all present on unmodified master. Verdict therefore reads green in the
only sense that is honest: nothing this branch does turns a passing test red.

### Not-run caveat

`TestFinishCleanupNoServerTargetNamesTheCondition`, `TestFinishCleanupGoneWorktreeNamesTheUndeclaredPeer`
and the new literal gate were run *and passed* in isolation
(`go test -run 'TestFinishCleanup' -v` → all PASS, including the four neighbouring behaviour locks
`TestFinishCleanupGoneWorktreeNamesTheCause`, `TestFinishCleanupSettledIsNotARefusal`,
`TestFinishCleanupChainCleanWorktree`, `TestGitSyncClientPassNoPeerSkips`); the full-package run is
green-minus-baseline as tabulated above.

New coverage asserted (both arms of the wording, per the epic):

- **empty `sync_peers`** → the reason carries the *single-node … NOT handed off to any server*
  note, does NOT carry the settings-slip note, worktree + branch survive;
- **declared-but-unusable peer** (`-oProxyCommand=…`, the injection shape the peer-alias guard
  refuses) → the settings-slip note, NOT the single-node note, worktree survives;
- **declared usable peer** → `finishCleanupServerTarget()` still renders `peerbox:~/work/GoPi`, so
  the ordinary chain line is what the client arm reports (unchanged);
- **gone worktree + no peer** → the chdir refusal *and* the single-node note, still classified
  settled; **gone worktree + declared peer** → the note is absent.
- **the git-sync face** (`TestGitSyncClientPassNoServerNamesTheCondition`) → the same two arms on
  the second producer of the reason: driven through `GitSyncServerTarget()` (exactly what the two
  CLI call sites pass), no peer declared → the single-node note, declared-but-unusable alias →
  the settings-slip note.
