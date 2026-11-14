<!-- doc-class: historical -->
# Task Report — the peer-ref collision lane, reworked (task/01a0c53c's content restored)

**Epic:** REWORK of `task/01a0c53c`. That run fixed a real bug — a client branch
whose name the peer already owns at a DIVERGENT tip was reported as an anonymous
push failure (`pushbackPushRefusedReasonFmt`) instead of a NAME COLLISION needing
a human decision — in commit `fba3b141`. The branch never landed because it left
master with a NEWLY-RED gate: its own new test
`internal/tools/task_sync_git_pushback_peeref_test.go:174` carries a
negative-totality assertion (`"a refused collision must not push anything"`) that
`TestAssertionTotalityAuditGate` reports as UNCLASSIFIED.
**Branch:** `fix/peeref-collision-audit-row` (worktree
`~/workspace/gopi-peeref-audit`, off master `ccdd5f1e`).
**Verdict at reframe:** DOABLE.

## Completed

1. **The archived content is restored on a fresh branch off the CURRENT master
   tip.** The old ref `task/01a0c53cbbcc7cbf8bf9d78a3f03e6d4` is free but its
   commit is unreachable — `git branch -a --contains fba3b141` prints nothing —
   so the content could only come from the attachment diff
   (`01a0c53c-branch-vs-master.diff`, 1508 lines). `git apply --check --verbose`
   listed the targets, then `git apply` applied all of them with ZERO conflicts
   (the diff is `master...branch`, so a fresh worktree off master accepts it
   whole):
   - `internal/tools/task_sync_git_pushback.go` — the peer-ref probe
     (`gitPushbackPeerRefTip`, `git ls-remote`), the mirror fetch, the three-way
     classifier (`gitPushbackClassifyPeerRef`, `merge-base --is-ancestor` in both
     directions) and the by-name collision refusal
     (`pushbackPeerRefCollisionReasonFmt`).
   - `internal/tools/task_sync_git_pushback_peeref_test.go` (new, 442 lines) —
     the regression fixture and the four tests, all against REAL git.
   - `internal/tools/memory_sync_task.go` — the flush plumbing
     (`FastForwarded` / the pushed entry).
   - `cmd/agent/cli_tasks.go` — the harvest surface gains
     `fast_forwarded=[…]` beside `branches_pushed=`/`push_failed=`.
   - `devdoc/peer-ref-collision-named.md` (the lane's own standing doc) and its
     `devdoc/DOC-INDEX.tsv` row (normative — it names the code it routes through).

2. **Exactly ONE judged row added to `auditTotalityAllow`**
   (`internal/tools/assertion_totality_allow_test.go`), keyed
   `internal/tools/task_sync_git_pushback_peeref_test.go:TestPushbackPeerRefCollisionRefusedByName`:
   **verdict `correct`, half (b)** — the set legitimately HAS no idempotent member
   for a colliding name, because the collision arm is the exclusion the claim
   names. Evidence chain, as written in the row:
   - production: `gitPushbackPlan` appends the refusal and `continue`s
     (`internal/tools/task_sync_git_pushback.go:634-636`), while the loop's only
     act past that point is `gitPushbackPushOne` + the `flush.Pushed` append
     (`:649-652`) — a colliding name reaches `Pushed` only if the collision arm
     is deleted, which is the defect.
   - **the carrier assertion that pins the opposite direction** (the claim is
     NOT "this lane never pushes") lives in the SAME file, on live fixtures:
     `TestPushbackPeerRefAncestorFastForwardedNotCollision` fetches the peer's
     CONTAINING tip and asserts `len(flush.Pushed)==1` with `Pushed[0]` prefixed
     `feat/ff` (`task_sync_git_pushback_peeref_test.go:220-223`), and
     `TestPushbackPeerRefAbsentPushesPlainName` asserts `Pushed[0]=="feat/new"`
     (`:251-254`).
   - same-function positive carriers: `len(flush.Failed)==1` (`:141`) and the
     line naming BOTH tips + the remedy in place of the anonymous push failure
     (`:150-170`); the refusal is measured on the peer's OWN refs
     (`pbForEachRef` before/after byte-identical, `pbRefSha(w,"feat/dup")==peerTip`
     `:177-181`, our own tip unmoved `:184-187`).
   - **what the proof still cannot separate** (stated in the row): the fixture
     gives the client exactly ONE branch of this name and no other pushable local
     branch, so within that test "nothing was pushed" is not separated from
     "nothing was selected to push" — that reachability half is carried by the
     two sibling tests above, not here.

3. **The gate itself and every other row are untouched.** No predicate
   loosening, no row deleted, no inline `// audit-allow:` escape.
   `git diff master..HEAD -- internal/tools/assertion_totality_audit_test.go`
   prints nothing.

4. **This rework's own report** — `devdoc/peer-ref-collision-audit-rework.md`
   (this file), with its hand-written `devdoc/DOC-INDEX.tsv` row (class
   `historical`: a past run's record; the standing claim it restores lives in
   `devdoc/peer-ref-collision-named.md` and in the code). Written BY HAND on
   purpose: re-running `tools/docclass/classify.py` would rewrite 447 unrelated
   rows of the committed manifest (measured — the manifest is hand-maintained
   beyond its generation, e.g. rows 16/19/31/32/38/41 carry hand-written
   reasons and spot-checks the generator does not reproduce).

## Not Done (honest)

- **The original run's `.evid/` scratch is NOT carried onto this branch** —
  `.evid/peeref-surface-fixture.sh`, `.evid/peeref-surface-rerun.sh`, the
  `zz_peeref_redproof_test.go` RED-proof (a deliberately FAILING test), and the
  captured `rerun-*.txt` / `surface-run*.txt` transcripts. They are the original
  run's Rule 31 evidence, they hardcode the OLD worktree path, and committing
  them would (a) inert a red-proof test into the suite and (b) violate Rule 30
  if left untracked. This rework re-ran the surface instead and quotes its own
  transcript below; the fixture script was reproduced locally (outside the repo)
  from `devdoc/peer-ref-collision-named.md` exactly as delivered.
- **`gitPushbackSharesHistoryWithMaster`'s unrelated-history arm** is left as the
  restored content has it. The collision FMT covers the unrelated case in its
  text; whether a separate ancestry gate is redundant there is the lane owner's
  judgement, not a gate question, and no acceptance command in this epic touches
  it.
- **The full-repo smoke (`scripts/test-budget.sh ./internal/tools ./cmd/agent`)
  was NOT run** — this rework's acceptance is scoped to the audit gate, the
  restored regression family and a full `./internal/tools/` package run. Running
  the whole-tree smoke belongs to the merge path (Rule 27's smoke, branch-scoped).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `build OK: 25116207 bytes`, exit 0.
- Vet: `GOPI_HEAVY_TESTS=0 go vet ./...` → `VET_EXIT=0`, no diagnostics.
- Tests:
  - `GOPI_HEAVY_TESTS=0 go test ./internal/tools/ -run 'TestAssertionTotalityAuditGate$' -count=1`
    → RED before the row (`FAIL … 1.647s`, exit 1, exactly one UNCLASSIFIED line),
    GREEN after (`ok github.com/nick/gopi/internal/tools 1.009s`, exit 0).
  - `GOPI_HEAVY_TESTS=0 go test ./internal/tools/ -run 'Pushback|PeerRef|Collision' -count=1`
    → `ok github.com/nick/gopi/internal/tools 14.036s` (the restored regression family).
  - Whole package `GOPI_HEAVY_TESTS=0 go test ./internal/tools/ -count=1 -timeout 20m`
    → see Result below.
  - Doc-manifest gate for this report's row:
    `GOPI_HEAVY_TESTS=0 go test ./internal/standards/ -count=1 -run 'TestDocManifestCoversEveryDevdocFile'`
    → `2212 rows cover 2213 files` — the same one-file deficit
    (`devdoc/merge-live-owner-fence.md` has no row) the CLEAN master baseline
    reports, i.e. pre-existing and not attributable to this branch.
- Byte-identity of the gate file: `git diff master..HEAD -- internal/tools/assertion_totality_audit_test.go` → prints nothing.
- Ran (1): `bash /home/user/workspace/_tmp/peeref-rerun.sh` — built this worktree's
  `bin/gopi`, stood up a local bare peer with a HEAD relay as `GIT_SSH_COMMAND`
  (a `:` in the alias is refused by `ValidateSSHPeerAlias`, so a local path cannot
  travel through `GOPI_SYNC_PEERS`), then ran the REAL `gopi task push` twice.
- Result (1) (verbatim, this run):
  - ① COLLISION: `git-client: push_failed=[feat/dup: NAME COLLISION — the peer's
    refs/heads/feat/dup is at 909c995 while ours is at 1f27dfd and neither
    contains the other, ... nothing was pushed and the peer's ref is untouched.
    This lane never forces ... and never forks into a new name ...: reconcile the
    two tips deliberately — merge the peer's tip into yours and re-deliver — ...]`
    → `EXIT=1`; peer refs before `909c995 feat/dup|4c3d75a master` == after.
  - ② FAST-FORWARD: `git-client: pushed=[feat/dup (fast-forwarded the peer's ref
    of this name: 909c995 → 3cf4834)]` → `EXIT=0`, peer `feat/dup` moved to our
    tip (`3cf4834`). So both halves of the three-way answer are live on the real
    CLI, not only in the unit fixture.
- Ran (2), the surface `cmd/agent/cli_tasks.go` actually gained: a two-node
  fixture whose relay serves the bare peer for git services AND answers a
  non-git command by running the REMOTE gopi (`sh -c`), with both nodes on this
  branch's `gopi` and their own `HOME`/ledger. `gopi task harvest --peer hp` was
  run in both peer-ref states.
- Result (2) (verbatim, this run):
  - COLLISION state —
    `git push-back to hp: branches_pushed=[] push_failed=[feat/dup: NAME COLLISION
    — the peer's refs/heads/feat/dup is at 044f95d while ours is at 78b9d94 and
    neither contains the other, so this push cannot be a fast-forward; nothing
    was pushed and the peer's ref is untouched. ...]` and the peer's `refs/heads`
    unchanged (`044f95d feat/dup`, `af0c1e1 master`).
  - FAST-FORWARD state (the client merged the peer's tip in) —
    `git push-back to hp: branches_pushed=[feat/dup (fast-forwarded the peer's ref
    of this name: 044f95d → 242c694)] push_failed=[] fast_forwarded=[feat/dup]`,
    peer `feat/dup` now `242c694` == the client's tip. The new
    `fast_forwarded=[…]` leg of the harvest line is therefore reached on a real
    run, which is what Rule 31 asks of the one line this branch added to
    `cmd/agent/cli_tasks.go`.
- Commit: the branch's tip commit on `fix/peeref-collision-audit-row`
  (`git log --oneline -1 fix/peeref-collision-audit-row` in `~/work/GoPi`), message
  `fix(pushback): restore the peer-ref NAME COLLISION lane and classify its one audit site`.
  The hash is deliberately NOT written here: writing it into a file that the same
  commit carries would invalidate itself (the doc would name a predecessor).

## Evidence (self-filed)

- **RED (before the row, restored content only):**
  `GOPI_HEAVY_TESTS=0 go test ./internal/tools/ -run 'TestAssertionTotalityAuditGate$' -count=1`
  → exit 1, and the gate named the key verbatim:
  `assertion_totality_audit_test.go:118: internal/tools/task_sync_git_pushback_peeref_test.go:TestPushbackPeerRefCollisionRefusedByName (internal/tools/task_sync_git_pushback_peeref_test.go:174): negative-totality assertion "a refused collision must not push anything: %v" is UNCLASSIFIED.`
- **GREEN (after the single row):** same command → `ok github.com/nick/gopi/internal/tools 1.009s`, exit 0
  — covers both halves: no UNCLASSIFIED line and no STALE-key error.
- **Restored regression family:** `GOPI_HEAVY_TESTS=0 go test ./internal/tools/ -run 'Pushback|PeerRef|Collision' -count=1`
  → `ok github.com/nick/gopi/internal/tools 14.036s`.
- **Gate file byte-identical:** `git diff master..HEAD -- internal/tools/assertion_totality_audit_test.go` prints nothing (empty output measured).
- **Baseline is the current tip:** `git log --oneline -1` in the worktree at branch creation → `ccdd5f1e Merge master into the live-run completion-fact branch`, i.e. the tip the epic named (ccdd5f1e or later), not a stale baseline.
- **Restoration was conflict-free:** `git apply --check --verbose <diff>` listed all targets, then `git apply <diff>` exited 0 with `git status --porcelain` listing exactly the expected 5 modified + 9 untracked paths (the untracked ones then removed / committed per the tables above).
- **Peer-ref surface (Rule 31), this run:** collision `EXIT=1` with the NAME COLLISION line naming peer tip `909c995` and our tip `1f27dfd`, peer `refs/heads` unchanged across the refused push; ff case `EXIT=0` with `pushed=[feat/dup (fast-forwarded the peer's ref of this name: 909c995 → 3cf4834)]`.
