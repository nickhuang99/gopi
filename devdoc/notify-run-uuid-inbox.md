<!-- doc-class: normative -->
# Task Report: `gopi notify --session <run-uuid>` delivered into a file no session reads

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a1330e-dfe3-7130-8ddf-f0aa73f5a274` · **Branch**: `task/01a1330edfe371308ddff0aa73f5a274`
**Date**: 2026-10-13 · **Host**: host-b

## Completed

- **The defect, fixed at its root**: `resolveNotifySessionDir`
  (`cmd/agent/cli_notify.go`) no longer decides the session's CONTAINER by
  guessing from the record's basename. It used to do

  ```go
  dir = filepath.Dir(path)
  if filepath.Base(path) != sessionlayout.RecordName {
      dir = filepath.Dir(dir)     // ✗ a guess that cannot tell two layouts apart
  }
  ```

  and that test is unable to distinguish the two layouts it means to separate:
  `filepath.Base(path) != "session.jsonl"` is true for BOTH the legacy flat
  record (`<root>/<id>.jsonl`) and the task-run record
  (`<root>/<task-id>/<run-uuid>.jsonl`). Only the flat case survives the extra
  peel (its dir IS the root), so a **run uuid** — the value `gopi task show`
  prints as `session_log` — resolved its session dir to the sessions ROOT and
  appended the message to `<root>/messages/inbox.jsonl`, which **no session
  reads** (the read side is `<session-dir>/messages/inbox.jsonl`,
  `internal/tools/session_message.go` `sessionInboxPath`). The verb exited **0**
  and printed `queued … (resolved as a session record with that id)` — Rule 15's
  silent failure, in the worst direction: the sender believes it was received.

- The fix is the deletion of the peel, plus the comment that records WHY the
  record's own directory is the container in all three layouts
  (`filepath.Dir(path)` is the answer for flat, bare AND task-run). It cites the
  measurement and the task id so a later reader cannot re-introduce it as a
  "missing" task-run branch.

- **Regression test** `cmd/agent/cli_notify_deliver_runlayout_test.go` (2 cases),
  driven through the REAL binary against a REAL profile, on a fixture profile
  built at the real layout `<root>/<task-id>/<run-uuid>.jsonl` with a genuine
  `session_start` event:
  - `TestNotifyDeliverRunUUIDLandsInTheContainerInbox` — the receipt names the
    container as the session dir, the inbox file EXISTS at
    `<root>/<task-id>/messages/inbox.jsonl` and carries the text, AND the stray
    `<root>/messages/inbox.jsonl` does **not** exist. The on-disk assertions are
    the acceptance; the receipt alone is what made the defect invisible.
  - `TestNotifyDeliverUnknownRunUUIDIsRefusedByName` — an unknown id over a tree
    that DOES hold records exits non-zero, the refusal says `unknown session`
    and names the sessions root it searched (Rule 15: a refusal names the
    place).

- **Red → green proof on the real surface**: the pre-fix binary was built from
  `HEAD:cmd/agent/cli_notify.go` (Rule 29 — read at a rev, never a second
  checkout) into the scratch dir, and both binaries were run against the SAME
  profile. Old: stray inbox, exit 0. New: container inbox (186 bytes), exit 0;
  unknown id exit 1. `## Evidence` carries the two transcripts.

## Files

| file | change |
|---|---|
| `cmd/agent/cli_notify.go` | `resolveNotifySessionDir`: the basename peel is gone; `dir = filepath.Dir(path)` is now final, with the comment that states why (three layouts, one answer) |
| `cmd/agent/cli_notify_deliver_runlayout_test.go` | new — the two regression cases above |
| `devdoc/notify-run-uuid-inbox.md` | this report (branch tip `111bd35be`; the fix itself is `169a1d101`) |
| `devdoc/DOC-INDEX.tsv` | one row classifying the report (Rule 36) |

`internal/sessionlayout` needed no change: it is the layout leaf and stays the
single truth (Rule 14). Nothing about the WRITE side
(`internal/tools/session_message.go`) changed either — the bug was the resolved
directory, never the append.

## Not Done — honest

- **The same defect exists on the `--peer` face and is NOT fixed here.**
  `internal/tools/notify_peer.go` `notifyPeerSessionDir` (line ~411) carries the
  identical peel:

  ```go
  dir = filepath.Dir(record)
  if filepath.Base(record) != sessionlayout.RecordName {
      dir = filepath.Dir(dir)          // same guess, same blindness
  }
  ```

  So `gopi notify --peer <alias> --session <run-uuid>` should write to the
  PEER's sessions root by the same route. **This is code reading, not a measured
  run** — I did not drive the cross-host hop (it needs the ssh shim, and the hop
  is a different surface from the epic's). Filed as bug row
  **`01a1337d-be08-7acf-906a-aa01015e2293`** rather than silently widened into
  this branch's diff (Rule 41 / Rule 26: a narrow fix that NAMES the remaining
  gap beats a fix that overstates its coverage). The bug row carries the exact
  `file:line` and the quoted code, and marks the claim as *reading*, not
  measurement — the same distinction Rule 41's third element demands.

- `--peer` and the local face now resolve the container by two independent
  code paths (`gopilog.FindSession` + `Dir`, and `taskFindSessionJSONL` + `Dir`).
  That split predates this task (the tools→gopilog edge is an import cycle and
  the peer file says so); this fix did not create it and does not widen it.
  Worth a row of its own if a third resolution is ever proposed.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK`
  (binary 66,563,661 bytes)
- **Tests**: `make test-budget PKGS='./cmd/agent'` — ran on this tip
  (`job-2`, 714s, `exit=1`). The package verdict is **FAIL**, and the failure is
  **entirely pre-existing and none of it is this diff's**: the 6 failing tests
  are the help-surface family
  (`TestManSourcesMatchTheirHelpPage/gopi-worker.md`,
  `TestPublishedHelpPagesStayWithinTheirUsageBudget/worker`,
  `TestToolsMaintenanceHelpSurface{EveryFormIsTestedOrBlockedWithAReason,InventoryMatchesDerivation}`),
  all four of which already failed in the **11:14** and **11:25** budget runs of
  this same package — before this fix's commit (12:20) — and are the subject of
  pending bug row **`01a1332a-fc41`**. Measured, from this run's own event stream
  (`run-1791865540-2152961.json`): **4323 tests pass, 6 fail**, and the two
  regression cases of this branch are among the passers:
  `TestNotifyDeliverRunUUIDLandsInTheContainerInbox` ✓,
  `TestNotifyDeliverUnknownRunUUIDIsRefusedByName` ✓.
  `gopi worker -h` (124 lines vs a 114-line budget) touches no file this branch
  changes — `git diff --stat gitPushcheck/master..HEAD` is four files, none of
  them `cli_usage_budget_test.go`, `man/gopi-worker.md` or the help surface.
  Plus the two new cases in isolation (below), which is the branch-scoped
  red→green evidence the epic asks for.
  `GOPI_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run 'TestNotifyDeliverRunUUID|TestNotifyDeliverUnknownRunUUID' -count=1`
  → `--- PASS: TestNotifyDeliverRunUUIDLandsInTheContainerInbox (0.01s)`,
  `--- PASS: TestNotifyDeliverUnknownRunUUIDIsRefusedByName (0.01s)`,
  `ok github.com/nick/gopi/cmd/agent 3.425s`
- **Ran** (the surface the diff changed, verbatim, with real arguments):

  ```
  # profile: <scratch>/home with <scratch>/home/.gopi/sessions/<task-id>/<run-uuid>.jsonl
  $ gopi notify --session 5f0c1a92-6b7d-4e51-8a3c-1d9b7e40c2aa --message "GREEN-PROOF fixed binary"
  queued: message 73c8a06d76ccf4b3 for session 5f0c1a92-6b7d-4e51-8a3c-1d9b7e40c2aa
  inbox:  <scratch>/home/.gopi/sessions/01a1330e-dfe3-7130-8ddf-f0aa73f5a274/messages/inbox.jsonl
  session dir: <scratch>/home/.gopi/sessions/01a1330e-dfe3-7130-8ddf-f0aa73f5a274 (resolved as a session record with that id)
  EXIT=0
  $ ls -la …/<task-id>/messages/inbox.jsonl   → 186 bytes
  $ wc -c  → 186
  $ gopi notify --session 01a1330e-dfe3-7130-8ddf-000000000000 --message "should be refused"
  gopi notify: unknown session "01a1330e-…-000000000000" — no session record with that id under <scratch>/home/.gopi/sessions (`gopi log --last` lists recent ones)
  EXIT=1
  ```
- **Result**: the session dir is now `<sessions>/<task-id>` (not the root), the
  container inbox holds the delivered record (186 bytes, one `kind:message`
  line), and the unknown id is refused with a non-zero exit that names the tree
  searched. Pre-fix, the same command against the same profile wrote
  `<sessions>/messages/inbox.jsonl` and exited 0.
- **Commit**: see the branch tip (`git log --oneline -1` on
  `task/01a1330edfe371308ddff0aa73f5a274`); the report is committed with it.

## Independent re-run (verifier, 2026-10-13)

The two transcripts above were reproduced from scratch by a second run, on two
FRESH profiles (`/dev/shm/nv-red-1`, `/dev/shm/nv-grn-1`) built from the same
one-record task-run fixture — the red binary is the one built from
`HEAD:cmd/agent/cli_notify.go`, the green binary is this branch's `bin/gopi`.
Both transcripts are re-saved as `artifacts/redproof-old.txt` and
`artifacts/greenproof.txt` (the earlier ones were never persisted — the run cited
`artifacts/` without creating it; that gap is closed here).

```
$ gopi-old notify --session 5f0c1a92-…-1d9b7e40c2aa --message "RED-PROOF pre-fix binary"
inbox:  /dev/shm/nv-red-1/.gopi/sessions/messages/inbox.jsonl          ← STRAY, no session reads it
session dir: /dev/shm/nv-red-1/.gopi/sessions (resolved as a session record with that id)
EXIT=0                                                        ← the silent failure, reproduced

$ gopi notify --session 5f0c1a92-…-1d9b7e40c2aa --message "GREEN-PROOF fixed binary"
inbox:  /dev/shm/nv-grn-1/.gopi/sessions/01a1330e-dfe3-7130-8ddf-f0aa73f5a274/messages/inbox.jsonl
session dir: /dev/shm/nv-grn-1/.gopi/sessions/01a1330e-dfe3-7130-8ddf-f0aa73f5a274
EXIT=0 ; container inbox 186 bytes, one {"kind":"message",…} line
         (the stray <sessions>/messages/ was NOT created)

$ gopi notify --session 01a1330e-dfe3-7130-8ddf-000000000000 --message "should be refused"
gopi notify: unknown session "01a1330e-…-000000000000" — no session record with that id under
/dev/shm/nv-grn-1/.gopi/sessions (`gopi log --last` lists recent ones)
UNKNOWN_EXIT=1
```

**Restore criterion, measured against HEAD** (Rule 5's index-free form):

```
$ git diff HEAD -- cmd/agent/cli_notify.go | wc -l    →  0
$ test "$(git hash-object cmd/agent/cli_notify.go)" = "$(git rev-parse HEAD:cmd/agent/cli_notify.go)"
RESTORED: worktree blob == HEAD blob
$ git status --porcelain | wc -l                      →  0     (Rule 30)
```

**Regression cases, run against the real binary**:

```
$ GOPI_BIN=$PWD/bin/gopi go test ./cmd/agent/ -run 'TestNotifyDeliverRunUUID|TestNotifyDeliverUnknownRunUUID' -count=1 -v
--- PASS: TestNotifyDeliverRunUUIDLandsInTheContainerInbox (0.01s)
--- PASS: TestNotifyDeliverUnknownRunUUIDIsRefusedByName (0.01s)
ok  github.com/nick/gopi/cmd/agent  4.271s
```

## Evidence (self-filed)

Every line below is a command I ran and the output I saw; the transcripts are
kept under this run's session directory
(`~/.gopi/sessions/01a1330edfe371308ddff0aa73f5a274/artifacts/`).

- **Red proof (pre-fix binary, built from `HEAD`)** —
  `artifacts/redproof-old.txt`:

  ```
  === OLD BINARY (HEAD, bug present) ===
  queued: message 24bf48162f7bed00 for session 5f0c1a92-6b7d-4e51-8a3c-1d9b7e40c2aa
  inbox:  /dev/shm/notify-red-RgdY/home/.gopi/sessions/messages/inbox.jsonl
  session dir: /dev/shm/notify-red-RgdY/home/.gopi/sessions (resolved as a session record with that id)
  EXIT=0
  --- sessions root stray inbox ---
  -rw-r--r-- 1 nick nick 182 Oct 13 12:19 inbox.jsonl        ← <sessions>/messages/inbox.jsonl
  --- container inbox ---
  ls: cannot access '/dev/shm/notify-red-RgdY/home/.gopi/sessions/01a1330e-…/messages/': No such file or directory
  ```

- **Green proof (fixed binary, same profile)** — `artifacts/greenproof.txt`:
  container inbox 186 bytes, `cat` shows
  `{"kind":"message","id":"73c8a06d76ccf4b3","session_id":"5f0c1a92-…",…}`;
  unknown id → `EXIT=1` with `unknown session … under <sessions root>`.

- **Restore criterion (the mutated file is back, measured against a REVISION,
  not the index)** — Rule 5 / `devdoc/redproof-restore-criterion.md`:

  ```
  $ sha256sum cmd/agent/cli_notify.go
  c3f2a150f3fac516325db75753a8c1f2057e8bcd8d45b2b32dc73327990a2789  cmd/agent/cli_notify.go
  ```

  This is the hash of the FIXED file taken BEFORE the red-proof swap
  (`artifacts/fixed.sha256`) and printed again after the swap was undone — the
  two agree, and the hash is the post-edit tree's, not `HEAD`'s (whose hash
  differs because the fix is in it).

- **`git status --porcelain`** in the worktree: printed nothing after the commit
  (Rule 30) — the worktree holds no litter; the scratch profile and both
  binaries live under `/dev/shm/notify-red-RgdY/` (off `/`, Rule 22), and the
  transcripts under the session directory (Rule 37).

- **`gopi tool doccheck` on this branch tip** — exit **1**, but **not from this
  branch's diff**. The two rows it names
  (`devdoc/doubaostream-session-test-coverage.md:717` → `chat_voice_live.go:249`,
  `devdoc/merge-resolve-peak-hour-guard.md:1226` → `main.go:982`) are carried by
  no commit of mine and are already the subject of pending bug rows
  `01a13320-4981` and `01a1331a-d077`. Its own DOC-INDEX half is green — `0 gap(s)
  between devdoc/ and devdoc/DOC-INDEX.tsv` — which is the part Rule 36 asks this
  commit to keep true. Transcript: `artifacts/doccheck-branch-tip.txt`. Measured,
  not inferred: `git diff --name-only HEAD~2..HEAD` is exactly the four files of
  this branch, and neither drifting source file is among them.

- **The base this branch is measured against** is the integration master the
  push-back path uses, `gitPushcheck/master` = `2e5b4b942` (the local `master`
  ref is older, `64b8a316a`, and is not what this branch descends from):

  ```
  $ git merge-base --is-ancestor gitPushcheck/master HEAD   →  YES (ancestor)
  $ git rev-list --count gitPushcheck/master..HEAD           →  2      (mine)
  $ git rev-list --count HEAD..gitPushcheck/master           →  0      (not behind)
  $ git diff --stat gitPushcheck/master..HEAD
   cmd/agent/cli_notify.go                        |  28 ++-
   cmd/agent/cli_notify_deliver_runlayout_test.go | 170 +++++++++++++++++++
   devdoc/DOC-INDEX.tsv                           |   1 +
   devdoc/notify-run-uuid-inbox.md                | 225 +++++++++++++++++++++++++
   4 files changed, 419 insertions(+), 5 deletions(-)
  ```

- **Test gate**: `make test-budget PKGS='./cmd/agent'` at this tip — `exit=1`,
  714s, stream `run-1791865540-2152961.json`; **4323 pass / 6 fail**, and all 6
  are the help-surface family already red at this package in the 11:14 and 11:25
  runs of the same day (before this fix's 12:20 commit) and filed as pending bug
  row `01a1332a-fc41`. The branch's own two cases PASS in that run.

## Evidence the budget run's 6 failures are not this diff's

Claim: the 6 failing tests are master-side, and the branch's two cases pass.

```
$ python3 - run-1791865540-2152961.json     # this run's own events, Package==cmd/agent
FAILED (cmd/agent):
    TestManSourcesMatchTheirHelpPage
    TestManSourcesMatchTheirHelpPage/gopi-worker.md
    TestPublishedHelpPagesStayWithinTheirUsageBudget
    TestPublishedHelpPagesStayWithinTheirUsageBudget/worker
    TestToolsMaintenanceHelpSurfaceEveryFormIsTestedOrBlockedWithAReason
    TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation
total FAIL: 6  total PASS: 4323
my two tests present and PASSING: True True

$ grep 'pkg=./cmd/agent ' stamps.log | tail -4
2026-10-13T11:14:09  ... exit=1 outcome=fail stream=run-1791861249-1505015.json   ← same 4, -2h before my commit
2026-10-13T11:14:06  ... exit=1 outcome=fail stream=run-1791861246-1503115.json
2026-10-13T11:25:58  ... exit=1 outcome=fail stream=run-1791861958-1610430.json
2026-10-13T12:25:40  ... exit=1 outcome=fail stream=run-1791865540-2152961.json   ← this run

$ gopi task list | grep -i 'usage budget|help surface'
#01a1332a-fc41-7b49-aedf-6445422982fc  [pending]  bug: 帮助面两条 gate 在 master 上就红
        —— worker -h 广告的两条形式不在 devdoc 清单里，且 worker 页 124 行超 114 行预算
```

This is stated because a **FAIL package verdict must not be silently rounded to
green**: the report says what the gate printed, and then measures which part of
it belongs to this branch. It belongs to none of it.
