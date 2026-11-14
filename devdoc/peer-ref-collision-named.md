<!-- doc-class: historical -->
# Peer-ref NAME COLLISION: the client push-back lane asks, and names, it

Epic: *a branch that passes the ff-onto-peer-master gate is pushed as a plain
`name:name` push; when the peer already owns `refs/heads/<name>` at a divergent
(or unrelated) tip, git's non-fast-forward refusal surfaces through one generic
line — so a NAME COLLISION, the one case needing a human decision, is
indistinguishable from a transport or permission failure.*

Branch: `task/01a0c53cbbcc7cbf8bf9d78a3f03e6d4` (worktree
`~/workspace/gopi-01a0c53cbbcc7cbf8bf9d78a3f03e6d4`, commit `fba3b141`, then
merged with master → `a5e42126`). An earlier name for this work
(`fix/peer-ref-collision-named`) was never committed to and holds only the base
commit — that ref is the lane's name, not the deliverable's.

## Completed

### 1. The lane asks the peer-ref question explicitly

`internal/tools/task_sync_git_pushback.go`, in `gitPushbackPlan`, between the
ff-onto-master gate and the push:

1. **Probe** — `gitPushbackPeerRefTip` runs `git ls-remote <target>
   refs/heads/<name>` and returns `(abbreviated tip, measured)`. Two states:
   `measured=true, tip=""` is a MEASUREMENT ("the peer does not own this name"),
   `measured=false` is the absence of one (unreachable peer, ls-remote failure).
   The bool is the point — a collision verdict may not stand on an unmeasured
   premise (Rule 15).
2. **Mirror** — when the peer does own the name,
   `gitPushbackFetchPeerRef` mirrors that one ref into
   `refs/remotes/gitPushcheck/peer/<name>` (the `gitPushcheck` namespace the
   pass already owns for the master mirror; the pass prunes the master mirror on
   the way out, and the mirror is force-updated by the next pass's fetch). A
   fetch — not an `update-ref` of the reported sha — because an ancestry test
   needs the COMMIT, and `ls-remote` only hands over the sha.
3. **Classify** — `gitPushbackClassifyPeerRef` answers with
   `merge-base --is-ancestor` in BOTH directions:
   * peer tip ⊂ ours → `gitPushbackPeerRefFastForward` (the peer's commits are
     kept; the push moves its ref forward along our history);
   * our tip ⊂ peer's → `gitPushbackPeerRefCollision` (the push still cannot be
     a fast-forward, and "someone else owns this name" is a human decision);
   * neither → `gitPushbackPeerRefCollision` (diverged / unrelated histories);
   * any other git status, a failed mirror fetch, or an unresolvable local ref →
     `gitPushbackPeerRefUnmeasured` — never a collision, never a refusal.

New shared primitive: `gitPushbackIsAncestorOf` returns `(true|false, error)`
so `merge-base` exit 0/1 is a measurement and ANY other status is an error —
one definition of "git said no" (`gitPushbackExitIsNo`) serves both directions
and the existing mirror gates.

### 2. Three distinct report lines, no guess left in them

| verdict | report |
|---|---|
| absent | `Pushed` gets the bare branch name (unchanged shape) |
| fast-forward | `Pushed` gets `"<name> (fast-forwarded the peer's ref of this name: <peer sha> → <our sha>)"`; `gitPushbackFlush.FastForwarded` carries the bare name beside it |
| collision | `Failed` gets `pushbackPeerRefCollisionReasonFmt`: **NAME COLLISION**, the branch, the peer's tip, our tip, "neither contains the other", *"nothing was pushed and the peer's ref is untouched"*, and the remedy (reconcile the two tips / merge the peer's tip in and re-deliver / deliver under a different name if the histories are unrelated) |
| unmeasured | the push is attempted and the refusal line (if any) is annotated `peer-ref probe UNMEASURED (<what failed>)` |

`pushbackPushRefusedReasonFmt` was rewritten to stop ASSERTING a cause: on
master it claimed *"its ref of this name has diverged"* for **every** push
failure — a diagnosis the lane never measured (the RED run below prints exactly
that, for a divergent ref, and would print it identically for a credential
failure). It is now an honest transport/permission-shaped line; the collision
cause has its own measured line. No force, no rename, no fork anywhere.

New constants (Rules 1/1b/13/14 — one definition each):
`gitPushbackPeerRefMirrorFmt`, `gitPushbackPeerRefHeadsFmt`,
`gitPushbackShortShaLen`, `pushbackPeerRefCollisionReasonFmt`,
`pushbackPeerRefFastForwardNoteFmt`, `pushbackPeerRefUnmeasuredNoteFmt`,
plus `masterBranchName`, `sshPeerAliasTargetSeparator` and the existing reason
fmts referenced, never re-spelled.

### 3. `GitPushbackReport`: one additive field

`memory_sync_task.go` — `BranchesFastForwarded []string
`json:"branches_fast_forwarded,omitempty"``, populated from
`flush.FastForwarded` in `gitPushbackDefault`. Existing fields
(`BranchesPushed`, `PushFailed`, `Skipped`) keep their shape and meaning, and
`BranchesPushed` still holds the branch name the peer received, so every
existing reader/test is untouched. The addition is what lets a CALLER tell
"the peer already owned this name" from "the name was new at the peer" without
parsing prose — the epic asked for the ff case to be *said* in the report, and a
machine-readable half is the only form a caller can act on. `cmd/agent/cli_tasks.go`
prints it as `fast_forwarded=[…]` on the existing harvest line.

## Not Done (honest)

- **The master mirror's `Skipped`/ff gates were not re-plumbed** (out of scope
  by the epic's constraints) and no behaviour change was needed in them.
- **A peer whose `refs/heads/<name>` cannot be READ leaves the question
  unmeasured at the branch level** — the per-branch `ls-remote` failure is
  reported, not retried per branch.
- **The 60s cap of `gitPushbackFetchTimeout` now covers one extra pair of git
  calls per candidate branch** (ls-remote + a ref fetch). On a large branch set
  that is more round-trips than before, by design (the named ref is learned from
  the remote's advertisement rather than inferred from a mirror that was already
  stale when the pass began). No batching was attempted.
- **A local filesystem path cannot be pushed by this lane**: the ssh half of
  the composed target is validated by `ValidateSSHPeerAlias`
  (`internal/tools/peer_alias.go`), which refuses a `:` — so a
  `/path/peer.git` server target is rejected as a malformed alias (measured
  while building the Rule 31 fixture). This was true before this change and is
  unchanged by it; the devdoc records it because the first two fixture attempts
  spent their time on it. The Rule 31 run therefore went through git's own
  transport hook (`GIT_SSH_COMMAND`) against a local bare repo — the recipe in
  memory #4695.

## Verification

- Build: `go build ./...` → OK (exit 0)
- Vet: `go vet ./...` → OK (exit 0); `gofmt -l internal/tools cmd/agent` lists
  only the five files that are already unformatted on master
  (`memory_sync_assets.go`, `task_staleness_guard_test.go`,
  `taskledger_cost_test.go`, `cli_config.go`, `serve_lock_replacement_test.go`)
  — the two files this branch touched are clean.
- Tests (new): `go test -run 'TestPushbackPeerRef|TestClassifyPeerRef|TestPeerRefTip|TestPushedEntry|TestShortSha' -count=1 ./internal/tools/` → `ok 5.270s`
- Tests (regression, existing pushback/ff/ancestry/resurrection set):
  `go test -run 'TestPushback|TestGitPushback|TestTaskHarvestGitPushback' -count=1 ./internal/tools/` → `ok 8.079s`
- Smoke / scoped gate: `go test -count=1 ./internal/standards/ ./internal/tools/ ./cmd/agent/` — see `## Evidence` (job `job-1`, `.evid/gates.txt`).

### RED before the change

A scratch worktree at the parent commit (`bf36bf69`, `~/workspace/gopi-peer-red`)
carrying ONLY a self-contained probe (no helper from this branch, so it
compiles at the parent) — `internal/tools/zz_peeref_redproof_test.go`, whose
fixture is the epic's shape (client `feat/dup` = master+ours; peer `feat/dup` at
its own divergent tip):

```
$ go test -run TestZZRedPeerRefCollisionNamed -count=1 ./internal/tools/
--- FAIL: TestZZRedPeerRefCollisionNamed (0.16s)
    zz_peeref_redproof_test.go:82: RED: a divergent peer ref was NOT refused by name.
          report line: "feat/dup: the peer rejected the push (its ref of this name has diverged) — this lane does not fork; reconcile the two tips and re-deliver"
          pushed: []
          (peer tip db3aae9, ours fc0d65d — neither tip is named, so a NAME COLLISION reads exactly like a transport/permission failure)
FAIL
```

The same probe copied into THIS branch passes (green after red): `ok 0.837s`.
The probe file was deleted from the branch afterwards (it was executed; it is
not a deliverable, and the durable tests below cover the same assertion).

### GREEN, durable (`internal/tools/task_sync_git_pushback_peeref_test.go`)

Real local git only (bare peer + clone, no ssh, no network):
`TestPushbackPeerRefCollisionRefusedByName`,
`TestPushbackPeerRefAncestorFastForwardedNotCollision`,
`TestPushbackPeerRefAbsentPushesPlainName`,
`TestPushbackPeerRefUnrelatedHistoryUnderSameNameIsCollision`,
`TestClassifyPeerRefThreeWay`, `TestPeerRefTipMeasuredVsUnmeasured`,
`TestPushedEntryUnmeasuredNote`, `TestShortSha`.

Each asserts on git's OWN refs, not on the report alone: the collision case
compares a full `for-each-ref` snapshot of the peer's `refs/heads` before and
after the run (byte-identical), and checks the peer's tip is unchanged; the
divergence precondition is MEASURED in a scratch clone that holds both tips
(`merge-base --is-ancestor` both directions must exit 1) rather than assumed.

### Rule 31 — the real surface, run once

Fixture `~/workspace/peeref-live` (script `.evid/peeref-surface-fixture.sh`): a
bare peer holding `feat/dup` at its own divergent tip, a client clone whose
`feat/dup` is master+ours, and a `GIT_SSH_COMMAND` relay serving the fixture
locally. The real binary was rebuilt from this branch
(`CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`) and the real CLI run:

```
$ cd ~/workspace/peeref-live/client
$ GOPI_SETTINGS=…/settings.json GOPI_SYNC_PEERS=hp GOPI_SYNC_NODE=clientnode \
  GOPI_ROLE=client GIT_SSH_COMMAND=…/relay/ssh gopi task push
git-client: push_failed=[feat/dup: NAME COLLISION — the peer's refs/heads/feat/dup is at 37a3955 while ours is at 3065f13 and neither contains the other, so this push cannot be a fast-forward; nothing was pushed and the peer's ref is untouched. This lane never forces (that would rewrite the peer's history) and never forks into a new name (the peer cannot land that either): reconcile the two tips deliberately — merge the peer's tip into yours and re-deliver — or, if the two histories are unrelated, deliver this work under a different branch name] pull:Already up to date.
  [FAIL] push: 1 branch(es) failed: feat/dup: NAME COLLISION — …
  [ok] pull-master: Already up to date.
EXIT=1
--- peer refs after:
37a3955 feat/dup      (unchanged — same sha as before the run)
2502584 master
```

Positive control, same fixture and same command, after aligning the client
branch with the peer's tip (`git merge FETCH_HEAD`):

```
$ … gopi task push
git-client: pushed=[feat/dup (fast-forwarded the peer's ref of this name: 37a3955 → 99890ad)] pull:Already up to date.
  [ok] push: 1 pushed (idempotent)
  [ok] pull-master: Already up to date.
EXIT=0
--- peer refs after: 99890ad feat/dup   (moved forward to our tip), 2502584 master
```

So the collision is refused by name with both tips, the peer's ref is untouched
by the refusal, and the SAME surface delivers the branch (under its own name, a
fast-forward) once the tips are reconciled.

### Re-verified after merging master in (`a5e42126`)

- `go build ./...` → exit 0; `gofmt -l` on the touched files → no output;
  `git status --porcelain` → no output.
- Pushback set (scoped) → `ok github.com/nick/gopi/internal/tools 13.396s`,
  including all eight new peer-ref tests and the ff/ancestry/`Skipped` set.
- `go test ./internal/standards/` → the SAME single pre-existing failure
  (`TestDocClassBannerMatchesManifest`, the two unrelated un-bannered docs),
  re-measured at the parent in a detached worktree before this merge; no new
  gate failure from this branch.
- **RED re-proved independently after the merge:** the probe at
  `.evid/zz_peeref_redproof_test.go` copied into a fresh worktree at the base
  commit `bf36bf69` →
  `--- FAIL: TestZZRedPeerRefCollisionNamed (0.16s) … report line: "feat/dup: the peer rejected the push (its ref of this name has diverged) …" (peer tip 4a10347, ours 135bf97)`
  — the parent prints the old generic line with neither tip named, and the probe
  file was removed from this worktree afterwards (`git status --porcelain` →
  nothing).
- The ff-onto-master gate is byte-identical, measured not asserted:
  `awk '/^func gitPushbackSharesHistoryWithMaster/,/^}/'` on `bf36bf69:` vs this
  commit → the same md5 (`269ba4fe484790ce6a424578c9dc41da`).
- Rule 31 re-run on the merged tree (`bash .evid/peeref-surface-rerun.sh`):
  collision refused by name (peer `3728048` / ours `bb029e8`, EXIT=1, peer refs
  unchanged) and then the same command pushed the fast-forward (`3728048 →
  0ed622e`, EXIT=0, peer `feat/dup` moved to our tip).

## Evidence (self-filed)

- `gofmt -w` on the branch file was needed after the edits (a comment
  re-indent): `gofmt -l internal/tools cmd/agent` afterwards lists only the five
  master-unformatted files, and `internal/tools/task_sync_git_pushback.go` is no
  longer among them (observed output above).
- RED: `.evid` copy of the probe run is quoted verbatim in the RED section; the
  worktree it ran in is `~/workspace/gopi-peer-red` at `bf36bf69`.
- The Rule 31 pair of runs is pasted verbatim from `.evid/surface-run.txt` and
  `.evid/surface-run-ff.txt`; the peer's `for-each-ref` before/after lines are
  the observation the "untouched" claim rests on.
- `go test -run … ` results quoted above were the command's own `ok`/`FAIL`
  lines.

## Files

- `internal/tools/task_sync_git_pushback.go` — probe, classifier, three-way gate,
  collision/ff/unmeasured lines, namespace note.
- `internal/tools/task_sync_git_pushback_peeref_test.go` — the durable tests.
- `internal/tools/memory_sync_task.go` — `BranchesFastForwarded` (+ its
  population in `gitPushbackDefault`).
- `cmd/agent/cli_tasks.go` — `fast_forwarded=[…]` on the harvest report line.
- `devdoc/peer-ref-collision-named.md` — this report.
