<!-- doc-class: historical -->
# Task Report: HP→peerbox push blocked by `pushurl=DISABLED-no-push`, and the refusal that could not be read

Task `01a118aa-624b-7af4-8be6-8aa2996af7fb` (peerbox, 2026-10-08).

## Completed

### 1. The guardrail's lineage — and its installation, dated

- The key was **installed 2026-10-03 12:20:07 on HP** into
  `/home/user/work/glow-moon/.git/config` and `/home/user/work/hermes-agent/.git/config`,
  as part of a deliberate "peerbox is read-only" decision recorded in the
  operator's own skill tree (`~/.hermes/skills/devops/hermes-ops/SKILL.md:385-393`:
  *"their `pushurl` is now set to `DISABLED-no-push` on purpose … Preserve work
  with a local bundle, never a push"*). The blocking push was therefore **the
  design working**, not an accident.
- **The 2026-10-07 22:10 write on `/home/user/work/GoPi/.git/config` was the
  operator's own deletion of that guardrail**, not a re-installation.
  Evidence: `diff /home/user/backups/GoPi-git-config.bak-20261008
  /home/user/work/GoPi/.git/config` → one line, `10d9 < pushurl = DISABLED-no-push`;
  the backup (mtime 2026-10-08 07:25) still carries the key, the live file does
  not. The exact writer of the 22:10 mtime is **not** identified (rule below),
  but the *effect* of that write is measured.
- **No gopi code writes git config at all.** The whole tree contains no
  `git config` writer except one read (`git remote get-url`,
  `internal/tools/task_sync_git.go:116`); `promote` appears nowhere in any Go
  file or script, and `strings bin/gopi | grep -x promote` is empty. A
  `remote.<name>.promote` key that survives `git config` rewrites is therefore
  not from this repo — measured: a bare `git fetch`/`git maintenance run
  --auto`/`git worktree add` sequence changed the config's md5 **not at all**,
  so on git 2.34.1 those commands do not strip, preserve or write keys.

### 2. How HP's code reached peerbox WITH the guardrail stopping pushes

The guardrail blocks the **remote-NAME** spelling only. Measured on HP against
the live repo, three spellings of the same push:

| spelling | consults `pushurl`? | result |
|---|---|---|
| `git push probe master:master` | yes | `fatal: 'DISABLED-no-push' does not appear to be a git repository` |
| `git push probe:/path/to/repo …` | **no** | reached the peer (auth/refused at the peer, i.e. the wire worked) |
| `git push peerbox:/home/user/work/GoPi …` | **no** | reached the peer |

Gopi never uses the blocked spelling: both of its push targets are rendered as
`<alias>:<path>` — `GitSyncServerTarget()` → `peerbox:~/work/GoPi`
(`internal/tools/task_sync_git.go:462`) and `finishCleanupServerTarget()` → the
same shape (`internal/tools/task_finish_cleanup.go:2382`) — and the one
`--repo` value the dispatcher hands a node is spelled the same way
(`cmd/agent/dispatch.go:354`). So **the push chain was never actually severed**;
what the guardrail did achieve is a refusal that no reader could diagnose (§3).

The arrival channel is measured in peerbox's own reflogs for the peer refs:

```
hp/task/01a113a1fe28730c937c6f98186c723f@{2026-10-07 12:05:22} update by push
hp/task/01a113e467767eb9a4c33cb1df1cdfbb@{2026-10-07 20:49:05} fetch --append --no-auto-gc --no-write-commit-graph -q hp: storing head
```

`update by push` is the *receiving* side's reflog — i.e. `git push` **did**
reach peerbox's repo on 2026-10-07 for the first branch (and 2026-10-07 was
already after the guardrail's 10-03 installation). The second row is **not**
gopi: its argv is `git fetch --append --no-auto-gc --no-write-commit-graph -q
hp` — and no gopi in this tree runs those flags (whole-tree grep for
`--no-auto-gc` / `--no-write-commit-graph`: 0 hits outside an unrelated
`notifyfeed` constant; `SyncPeerFetch` uses `--force --no-tags`). It is a
human/diagnostic command that mirrored HP's whole ref namespace.
The 2026-09-16 wave used a different route (a `git push`
spelled with an explicit refspec into peerbox's `refs/remotes/hp/*`, recorded in
the then-current `devdoc/hp-push-diagnosis.md`, since deleted/archived and
recoverable from the pack); that route no longer exists in the current tree.
Honest limit: the 09-16 leg's *command* is evidence-by-doc, not reproduced this
run.

### 3. The defect fixed: a refusal that named its lane but not its cause (Rule 15)

`gitPushbackPlan` reduced every failed push to one sentence —
`"%s: the peer rejected the push — this lane does not fork or force; reconcile
with the peer and re-deliver"` — which reads **identically** for a
non-fast-forward, a permission denial, and a push that never left the machine
because the target's config says so. Three causes, one sentence, and the third
one is unactionable.

Now the refusal carries git's own diagnostic, and — when the cause is
*measurable* — a plain finding naming git's config key, its value and the
command that clears it:

```
feat/blocked: the peer rejected the push — … [lane verdict, unchanged]
 — git said: error: failed to push some refs to 'DISABLED-no-push'.
   fatal: 'DISABLED-no-push' does not appear to be a git repository …
 — the target "peerbox" is blocked by its own git config:
   remote.peerbox.pushurl = "DISABLED-no-push" — git is not reading that remote
   for this push (…), so pushing is refused before anything leaves this machine;
   clear the block with `git config --unset remote.peerbox.pushurl` and re-deliver
```

The extra arm is driven by a **measured** probe (`git config --get
remote.<name>.pushurl`, read verbatim) and by git's own
`does not appear to be a git repository` sentence — never by the mere fact that
a push failed — so it cannot claim a block that does not exist. The probe
answers "no pushurl" for an unset key, for a path/URL target, and for a name
that is not a remote at all.

### 4. The four HP-local branches, each given a destination

All four are on **HP** (`/home/user/work/GoPi`); peerbox only ever saw the ones
that were pushed. Each was tagged `archive/<name>-20261008` at its exact tip
**before** the ref was removed, so the ref stays recoverable.

| branch | tip | `git cherry master` unmerged | destination |
|---|---|---|---|
| `feat/chat-voice-reland` | 579634d25 | **0** | archived + deleted (`-d`) |
| `task/01a113a1…c723f` | 7954eaa5f | **0** | archived + deleted (`-d`) |
| `test/archprobe` | 79b29f5f1 | **0** | archived + deleted (`-d`) |
| `task/01a113e4…1cdfbb` | 8dfd07b56 | **0** | archived + deleted (`-D`) |

`git cherry` is the right predicate here (patch-id per commit; `+` = no
equivalent in master) and it is what the first three passed. The fourth is the
one worth reading: its tip is **not an ancestor** of master (5 commits ahead,
and `git diff` between them is huge both ways — the branch is simply 233
commits *behind*), yet `git cherry` reports **0** unmerged commits: its two
non-merge commits are both patch-equivalent in master, and its three merges are
internal re-landing merges. The deliverable blobs confirm it:
`devdoc/voice-input-capture-rig.md`, `scripts/failed-reason-triage.sh` and
`devdoc/failed-reason-attribution.md` are byte-identical in master and in the
branch. That is why the housekeeping script's automated branch of the decision
**kept** it (its own predicate — ancestor-or-identical-tree — was too strict),
and it was deleted afterwards by hand on the same evidence. **Not Done /
honest:** that last deletion is not covered by the script's own guard; if the
same shape recurs the script will again keep the branch and a human must read
`git cherry`.

After: HP's GoPi carries exactly `master` (e5099c9f9) + the four archive tags,
`git worktree list` shows no worktree holding a deleted branch, and
`git status --porcelain` prints nothing.

### 5. The recurrence channel, named

The guardrail's *installation recipe* is still live in the operator's skill
tree: `~/.hermes/skills/devops/hermes-ops/SKILL.md:385-390` and
`.../references/delivery-landing-audit.md:160` both instruct an agent to set
`pushurl = DISABLED-no-push` on every remote pointing at peerbox. A future
hermes-side session following those pages can re-install it. This task did
**not** edit them (they are the operator's files, outside this repo, and Part b
of the briefing was skipped by the operator's own instruction — see `## Not Done`).

## Not Done

- **The exact writer of the 22:10 config mtime is not identified.** What is
  established is the *content delta* (the guardrail was removed) and the
  *timing*; the process itself left no trace in the candidates checked —
  HP's `~/.bash_history` has no `git config` line, the 22:09 session's transcript
  contains the string `DISABLED-no-push` **once**, inside a `git remote -v`
  output block, and no gopi code writes config. Naming a culprit from that
  would be a guess.
- **Part b of the briefing (add a GUI control for the operator to approve or
  reject the push-blocking guardrail, i.e. a feature to temporarily lift it)
  was deliberately not built**, because the operator asked for Part a first and
  then ruled the feature itself out in the live chat session of 2026-10-08:
  *"这是历史遗留问题，现在push的问题已经不存在了…都不需要再出现，那是一个自以为是的
  东西…我在本地chat中也已经和gopi说了，你不需要这个功能，不需要去设计这样的东西。"*
  Building it would have added a knob for a guardrail the operator has decided
  should not exist. The brief for this row is unchanged and still carries
  verbatim `Part b: 请增加一个可以在GUI中由operator批准或拒绝的GUI控件来控制以及push
  的阻断护栏` — the discrepancy is recorded here rather than silently resolved.
- **No code path was added to suppress or defeat the guardrail** (Rule 35): the
  operator's own `git config --unset` is the fix, and the code change teaches
  the *report* to say so. Nothing persistent was created — one Go file changed
  and one test file added, no script, hook, unit or state file.

## Verification

- **Build**: `go build ./...` → OK (exit 0, no output).
- **Tests** (branch-scoped smoke, the packages the diff touches):
  - red→green, the new repro: `go test ./internal/tools/ -run
    'TestPushbackBlockedPushURLIsNamedInTheRefusal' -count=1` — **red** before
    the fix, printing the pre-fix line
    `"feat/blocked: the peer rejected the push — this lane does not fork or
    force; reconcile with the peer and re-deliver"` and failing on 3 of 6
    assertions (no URL, no key, no remedy); **green** after.
  - the false-positive guard: `go test ./internal/tools/ -run
    'TestPushbackPushURLProbeIsSilentWhenUnset' -count=1` → PASS.
  - the touched surface as a whole: `go test ./internal/tools/ -run
    'Pushback|GitSyncClientPass|FinishCleanup' -count=1` → `ok
    github.com/nick/gopi/internal/tools  12.430s`.
- **Ran** (Rule 31 — the surface itself, on HP, with the real binary):
  ```
  scp bin/gopi-01a118aa hp:/tmp/gopi-fix-01a118aa
  ssh hp 'cd <fixture> && GOPI_SYNC_PEERS=peerbox GOPI_NODE_ROLE=client \
          HOME=/home/user /tmp/gopi-fix-01a118aa task push'
  ```
  on a fixture whose peer-NAME `peerbox` carries
  `pushurl = DISABLED-no-push` and whose branch fast-forwards the peer's master.
- **Result** — the OLD binary (HP's installed `/home/user/work/GoPi/bin/gopi`)
  and the NEW binary, same fixture, same env:

  | binary | printed |
  |---|---|
  | OLD | `[FAIL] push: 1 branch(es) failed: feat/blocked: the peer rejected the push — this lane does not fork or force; reconcile with the peer and re-deliver` |
  | NEW | the same line **plus** `git said: … 'DISABLED-no-push' does not appear to be a git repository` **plus** `the target "peerbox" is blocked by its own git config: remote.peerbox.pushurl = "DISABLED-no-push" … clear the block with `git config --unset remote.peerbox.pushurl` and re-deliver` |

  and in both runs `git ls-remote peerbox 'refs/heads/feat/blocked'` stayed
  **empty** — nothing left the machine, which is what makes this a report fix
  and not a bypass.
  Honest note on the fixture: a first hand-built fixture never reached the push
  at all (`GIT_TRACE` showed the lane stopping at the ancestry gate, because the
  branch's history and the peer's `origin/master` were unrelated — the peer was
  an empty bare repo at fetch time). The measurement above is from the corrected
  fixture (`git remote rename origin peerbox` after a real seed push), where
  `SyncPeerFetch`'s own argv admits the branch and the push is the step that
  decides.
- **HP branch housekeeping**: `bash /tmp/hp-branch-housekeeping.sh` (the script
  in the session's `artifacts/`), whose before/after ref listings and per-branch
  `git cherry` counts are quoted in §4; the fourth branch was then removed by
  hand on the same evidence.
- **Commit**: see the close-out.

### Added 2026-10-08 (peerbox side): the same lane, refused at the WIRE

The HP measurement above is a push that never left HP. This is its complement —
the same code path, a push that REACHES the peer's git and is refused there, so
every gate ahead of the push passes and the sentence this change renders is the
one git's receive side produced.

- **Ran** (the surface itself, verbatim; a throwaway clone plus a peer reached
  through a test-only ssh shim, so nothing crosses the network and
  `/home/user/work/GoPi` is only ever read as the clone's source):
  ```
  GOPI_HOME=<sandbox> GOPI_NODE_ROLE=client GIT_SSH_COMMAND=<shim> \
    bin/gopi task push            # cwd = the clone, branch feat/pb-live
  ```
  The peer is a NON-BARE repo whose HEAD sits on `feat/pb-live` (so git's own
  `receive.denyCurrentBranch` refuses the update) whose tip is an ANCESTOR of
  ours (so the push IS a fast-forward by every gate the lane measures).
- **Result**: `git-client: push_failed=[feat/pb-live: the peer rejected the push
  — this lane does not fork or force; reconcile with the peer and re-deliver —
  git said: remote: error: refusing to update checked out branch:
  refs/heads/feat/pb-live]`, and the peer's `refs/heads/feat/pb-live` was
  **unmoved** across the run (`3c5b2157c` before and after) — nothing landed.
  Before this change that sentence stopped at "re-deliver".
- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (exit 0).
- **Tests**: `go test ./internal/tools/ -run
  'TestAssertionTotalityAuditGate|TestPushbackBlockedPushURL|TestPushbackPushURLProbe'
  -count=1` → `ok github.com/nick/gopi/internal/tools 4.562s`.
- **Honest note (the budget run)**: `make test-budget PKGS='./internal/tools'`
  reported 2 failures. `TestAssertionTotalityAuditGate` was MINE and is fixed —
  the new negative-totality assertion needed its `why:` classification (the
  audit gate is total over the test tree). `TestBashWithTimeout_PositiveKills`
  passes in isolation (`-run TestBashWithTimeout_PositiveKills` → `ok`) and is
  timing-sensitive: that run measured load 11.08 with 4 cores. It is not a
  claim about this diff; it is reported because the budget run printed it.

## Evidence (self-filed)

- `go build ./...` → exit 0, no output. (observed this run)
- `go test ./internal/tools/ -run 'TestPushbackBlockedPushURLIsNamedInTheRefusal' -count=1`
  → `--- FAIL: … the refusal must quote the refused push URL "DISABLED-no-push":
  "feat/blocked: the peer rejected the push — …"` (red, before the fix);
  `ok github.com/nick/gopi/internal/tools 0.400s` (green, after).
- `go test ./internal/tools/ -run 'Pushback|GitSyncClientPass|FinishCleanup' -count=1`
  → `ok github.com/nick/gopi/internal/tools 12.430s`.
- HP, spelled-push measurement: `git push --dry-run probe master:master` →
  `fatal: 'DISABLED-no-push' does not appear to be a git repository` (exit 128);
  `git push --dry-run probe:/home/user/work/GoPi master:master` →
  `! [rejected] master -> master (fetch first)` — i.e. it reached the peer, which
  is the proof that `pushurl` does not govern the spelling gopi uses.
- peerbox reflog: `hp/task/01a113a1…@{2026-10-07 12:05:22} update by push` —
  the receiving side's own log of a push that arrived four days AFTER the
  guardrail was installed.
- Rule 30: `git status --porcelain` in this run's worktree → see the close-out;
  HP's `/home/user/work/GoPi` → prints nothing (observed).
- **Not measured** (stated so it is not read as a claim): the writer of the
  22:10 mtime; the 2026-09-16 leg's command reproduced live.
