<!-- doc-class: historical -->
# The two faces of the delivery target — one subject, one answer

**Class:** landing record (what changed, why, and what was measured).

**Measured** 2026-10-16 on `hosta`, task `01a142e9-fc64-761e-aa35-0301fba80a25`,
worktree `/home/user/workspace/gopi-01a142e9fc64761eaa350301fba80a25`, base `9943ffb`.

## The defect, as measured

`internal/tools` had **two faces** onto the delivery target:

| face | subject |
|---|---|
| `finishCleanupServerTarget()` (niladic) | *this process's* checkout, via `finishCleanupServerTargetRepo()` |
| `finishCleanupRowTargetResolution(repoDir)` | *the row's* repository |

Since 2026-10-12 the **path/name half is a per-repository fact**
(`peerRemoteTargetFor`: the declared alias is asked of that repo's own git
remotes; a repo that configures a remote of that name delivers through the
remote NAME, otherwise the leg falls back to `<alias>:<path>`). Two faces whose
only difference is their SUBJECT can therefore answer differently about one
repository — and two of the production legs read the wrong one.

Both were observed on the baseline, in one run
(`artifacts/red-A-prechange.log`):

```
RED-PROBE row-face="delivery-faces-peer" chain-target="" skipped=true reason="in-process test fence:
  a test process must not push a deliverable to a real node (target=delivery-faces-peer:~/work/GoPi) …"
  → the chain delivered through "" while the row's OWN repository answers "delivery-faces-peer"
    (its own git remote) — the target was resolved from a different repository.

RED-PROBE no-repo niladic target="delivery-faces-peer:~/work/GoPi"
  → fixture: `git rev-parse --is-inside-work-tree` in that directory: exit 128,
    "fatal: not a git repository (or any parent up to mount point /dev)"
```

So, on the unchanged baseline:

1. **The close-out chain resolved its push target from the PROCESS, not the row.**
   A row whose own repository *did* configure the declared alias as a git remote
   was still addressed as `<alias>:~/work/GoPi` — a real ssh destination chosen
   for a repository nobody had been asked about.
2. **With no repository to ask at all, the niladic face composed a real target
   anyway.** `git remote` fails there exactly as it does in a repo with no
   remotes, so the empty list was read as "the repo has no such remote" and the
   fallback arm answered `<alias>:~/work/GoPi` — for a repository that does not
   exist. Two conditions, one answer (Rule 15).

## The change

- **`finishCleanupServerTargetForRepo(repo string) peerRemoteTarget` is the ONE
  entry** (Rule 14). The niladic face is now *this function with an empty
  argument*: one resolution, two subjects, no second implementation to drift.
  `finishCleanupRowTargetResolution(repo)` delegates to it too.
- **`peerRemoteTarget` carries `Dir`** — the repository the answer was asked of.
  A resolution whose subject is invisible cannot be compared with another
  resolution, and after a failed delivery "which repo did you ask?" is the first
  question.
- **A third condition, and a third sentence.** Before reading a remote list the
  resolution asks `git rev-parse --git-dir` (`peerRemoteRepoIsEstablishable`).
  An unanswerable repository is now `Rendered == ""` **with the peer named**:
  `peerRemoteTargetNoRepoFmt` says which directory was asked, what the probe
  answered, and that this is *not* the fallback condition. **`--git-dir`, not
  `--is-inside-work-tree`** — a BARE repository is a repository with its own
  remotes and no work tree, and the delivery path is legitimately handed one
  (measured mid-change: the fixture arm `TestPeerRemoteTargetNamesTheArmAndThisReposRemotes` builds exactly that, and a work-tree probe called it "no repository").
- **`finishCleanupServerTargetRepo()` answers `""` when this process is in no
  repository** — the condition is no longer masked into `.`. The checkout-less
  *resolution* still asks about `.` (git's own spelling for "the directory you
  are in", the subject that face claims), which is where the honest answer is
  produced.
- **Both production legs read the per-repository face with their own repo**:
  `taskFinishCleanupChain` → `finishCleanupServerTargetForRepo(d.Repo)`;
  `taskGroundPeerBaseline(repo)` → `finishCleanupServerTargetForRepo(repo)`.

**Kept, deliberately:** the niladic face (the arm predicate
`finishCleanupClientArmApplies`, wording fixtures, "this host has a peer at all",
no row in hand) and the `<alias>:~/work/GoPi` fallback arm (a real client —
hosta: `sync_peers ["peerbox"]`, remotes `origin`, `task_sync: false` — takes it).
`gitSyncPeerRepo` still holds that spelling for the git-sync legs; `rule 13`'s
enumerated source check in `task_finish_cleanup_localside_test.go` still passes.

## Verification

| check | result |
|---|---|
| red A (baseline production + behavioural probe) | FAIL, both arms, `artifacts/red-A-prechange.log` |
| red B (per-repo face mutated to ignore its argument) | `TestDeliveryTargetFacesAgreeForOneRepo` FAIL, `artifacts/red-B-masked.log` |
| green (`DeliveryTargetFaces\|TaskRunGround\|PeerAlias\|PeerRemoteTarget\|FinishCleanup`) | ok |
| `make test-budget PKGS='./internal/tools ./internal/standards'` | `artifacts/full-tools-standards.log` |

The new test file is `internal/tools/delivery_target_faces_test.go`:
① one repository, both faces, one answer (plus `Dir`), on the arm where a face
asking the wrong repository shows up; ② no repository — both faces EMPTY and the
refusal named; ③ both production bodies read `finishCleanupServerTargetForRepo(…)`
with `d.Repo` / `repo`, read from their source.

## Not done

- `GitSyncServerTarget()` (`task_sync_git.go`, peer_alias_test.go) still composes
  its own `<alias>:~/work/GoPi` from `gitSyncPeerRepo`, in two files, and is read
  by two test sites rather than by a production leg. It is the same one-subject
  face for the git-sync pass, but folding it in touches the alias-guard suite —
  a change of its own, not this one's.
- `finishCleanupClientArmApplies` answers "does this host have a peer at all"
  through the checkout-less face. It is the right face for that question (no row
  in hand); nothing was changed there.
