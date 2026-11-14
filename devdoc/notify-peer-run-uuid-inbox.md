<!-- doc-class: historical -->
# Task Report: the cross-peer hop landed a run-uuid message in the peer's sessions root

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task**: `01a1337d-be08-7acf-906a-aa01015e2293` · **Branch**: `task/01a1337dbe087acf906aaa01015e2293`
**Base**: `3544b690c` · **Date**: 2026-10-13 · **Host**: hosta

## Completed

- **The `--peer` face of the run-uuid defect is fixed, by deletion.**
  `internal/tools/notify_peer.go` `notifyPeerSessionDir` peeled one directory off
  `filepath.Dir(record)` whenever `filepath.Base(record) != sessionlayout.RecordName`.
  That test cannot separate the two layouts it means to separate: the legacy flat
  record (`<sessions>/<id>.jsonl`) and the task-run record
  (`<sessions>/<task-id>/<run-uuid>.jsonl`) BOTH have a basename other than
  `session.jsonl`. So a **run uuid** — the value `gopi task show` prints as
  `session_log`, the easiest id to copy — resolved to the sessions ROOT and the
  message was appended to `<sessions>/messages/inbox.jsonl`, a directory no
  session reads (the reader is `<session-dir>/messages/inbox.jsonl`,
  `internal/tools/session_message.go`), while the peer's handler exited 0 and the
  initiator printed a successful hop. The peel is gone; the record's own
  directory is the container.

- **The container rule is now ONE definition (Rules 13/14).**
  `sessionlayout.RecordContainer(recordPath)` → `filepath.Dir(recordPath)`
  (`internal/sessionlayout/sessionlayout.go`), with the rule stated there: it is
  a property of the PATH, never of the basename, and the three layouts are
  enumerated with the task-run case marked as the only one where the container
  differs from the record's dir. Both faces call it:
  - the cross-peer handler — `internal/tools/notify_peer.go` (`notifyPeerSessionDir`);
  - the local face — `cmd/agent/cli_notify.go` (`resolveNotifySessionDir`, whose
    own peel was removed by `169a1d101`; it now references the shared definition
    instead of writing `filepath.Dir` inline).

  `filepath.Dir(<record>)` as the container rule now appears exactly once in
  production code (inside `RecordContainer`). Measured:
  `grep -rn 'RecordContainer' --include=*.go .` → the definition + the call
  sites; `grep -rn 'filepath.Dir' internal/tools/notify_peer.go cmd/agent/cli_notify.go`
  → no hit in either resolver.

- **The OTHER writers of the same rule in this family were folded in too (same
  sweep, Rules 13/14).** `filepath.Dir(<record>)`-as-container was written inline
  in three more production places and in the four notify test fixtures' own
  record→dir derivation; all six now call the shared definition (behaviour
  unchanged — `RecordContainer` IS `filepath.Dir`):
  - `internal/replay/session_ground.go` — `resolveSessionGround`'s id route and
    `recordForPath`'s file route (the replay film's session directory);
  - `internal/agent/session_inbox.go` — `sessionInboxDir`, the directory this
    run's own durable inbox lives in (the READER side of the same rule the cross-peer
    hop writes against — the two sides of one inbox now derive it from one place);
  - `cmd/agent/cli_notify_deliver_idle_test.go`, `cli_notify_duplicate_test.go`,
    `cli_notify_deliver_test.go`, `cli_notify_peer_test.go` — the fixtures stop
    re-deriving the container their assertions already name.

  **Honest boundary.** This is NOT "every container derivation in the repo is now
  single-sourced". The chat/console-log family still writes
  `filepath.Dir(<the session log path>)` inline at 16 production sites (14 from
  one measured grep, plus `cmd/agent/ui_state_self.go:91` and the body of
  `cmd/agent/lineedit_wire.go`'s `chatSessionDir`) — the same rule, unchanged
  behaviour today, but a Rule 13/14 mirror this epic did not take on. It is filed
  as its own row (`01a13440-3188-7561-8efa-ead0bf351f06`) with the measured site
  list rather than folded in late and unverified.

- **Regression test** `cmd/agent/cli_notify_peer_test.go`
  `TestNotifyPeerCLITaskRunRecordKeepsItsContainer`: the EXISTING ssh shim drives
  a real process boundary, the peer profile's session is
  `<peer-home>/.gopi/sessions/<task-id>/<run-uuid>.jsonl`, the delivery addresses
  the **run uuid**, and the assertion is two-sided — the container's inbox holds
  the message AND the stray `<sessions>/messages/` does not exist (one-sided
  would pass on a resolver that wrote both).

- **The one new fact about a session record the local face only *mentioned* now
  has a definition to cite, not a rule to copy.** Both faces still find the
  record with their own resolver (`gopilog.FindSession` local,
  `taskFindSessionJSONL` peer — the tools→gopilog edge is an import cycle, stated
  in the peer file); what was duplicated was the container DERIVATION, and that
  is what is single-sourced. A third resolution algorithm is still the drift
  Rules 13/14 forbid.

## Not Done

- **Nothing in this epic's scope was skipped.** Two adjacent observations are
  recorded rather than acted on:
  - the two faces still resolve a session OPERAND through two different
    algorithms (by design, per the import cycle); a future third one would be a
    rule violation, and the doc is the place that says so;
  - `internal/tools/notify_peer.go` still imports `sessionlayout` only for the
    container rule and `RecordName` (the latter for `DirHasRecord`-adjacent
    classification in the same function) — no change to that split was needed.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./... && go vet ./internal/tools ./cmd/agent ./internal/sessionlayout` → exit 0, no output.
- **Tests**: `make test-budget PKGS='./internal/tools ./cmd/agent ./internal/sessionlayout ./internal/replay ./internal/agent'`
  — the branch-scoped budget run (Rule 27), `elapsed=719s`, `exit=1`
  (`EXIT=2` = make wrapping it), stream
  `/home/user/workspace/gopi-test-scratch/queue/test-budget/run-1791877070-3891517.json`,
  transcript `testbudget2.txt` under this run's session directory. Measured from
  the stream, per package (pass / fail of named tests):
  `cmd/agent` **3001 / 0**, `internal/tools` **3375 / 1**,
  `internal/agent` **1084 / 1**, `internal/replay` **11 / 0**,
  `internal/sessionlayout` **14 / 0** — package verdicts: replay + sessionlayout
  **pass**; the other three FAIL (see below).
  This branch's own cases in that very run:
  `TestNotifyPeerCLITaskRunRecordKeepsItsContainer` **pass**,
  `TestNotifyPeerCLIIdempotentAndNamedRefusals` **pass**,
  `TestNotifyPeerCLIWakesTheFarSessionWithNoInput` **pass**,
  `TestNotifyDeliverRunUUIDLandsInTheContainerInbox` **pass** (the local face's own run-uuid case, from the already-merged fix),
  `TestNotifyDeliverReachesARunningChatSession` **pass**,
  `TestNotifyDeliverStatesTheMessageOnceInTheSession` **pass**,
  `TestNotifyDeliverWakesAnIdleChatSessionWithNoInput` **pass**,
  `TestRecordContainerIsTheRecordsOwnDir` **pass**.
  `internal/sessionlayout` green on its own: `make test-budget
  PKGS='./internal/sessionlayout'` → `exit=0 outcome=pass elapsed=4s`
  (stream `run-1791876811-3872421.json`).

  **The packages this diff touches, run directly on the final tree** (the
  branch-scoped check that separates "the budget run is red for three unrelated
  reasons" from "this diff is red"):

  ```
  $ go test ./cmd/agent -run 'TestNotify(Peer|Deliver)' -count=1
  ok  	github.com/nick/gopi/cmd/agent	10.112s
  $ go test ./internal/sessionlayout -count=1
  ok  	github.com/nick/gopi/internal/sessionlayout	2.762s
  $ go test ./internal/replay -count=1
  ok  	github.com/nick/gopi/internal/replay	2.888s
  $ go test ./internal/agent -run 'Inbox' -count=1
  ok  	github.com/nick/gopi/internal/agent	4.828s
  ```

  Whole notify family, the shared definition and every consumer of it: green.

  The three failures, each measured on the untouched tree at rev `3544b690` in
  this run and each already a filed row — none this diff's:
  - `internal/tools/TestAssertionTotalityAuditGate` — **pre-existing**:
    `cd <rev3544b690 tree> && go test ./internal/tools -run TestAssertionTotalityAuditGate`
    → `FAIL github.com/nick/gopi/internal/tools` (4.5s). Filed:
    `01a133d3-b9f3-7e6f-8453-8640d8f82b25`.
  - `internal/agent/TestDocAnchorsResolveInNormativeDocs` — **pre-existing**:
    same command at the same rev → FAIL. The run reports 26 new drift entries,
    **none of them in this diff's devdoc**, measured:
    `go test ./internal/agent -run TestDocAnchorsResolveInNormativeDocs 2>&1 | grep -c 'notify-peer-run-uuid-inbox'`
    → `0` (the doc is classified `historical` in `devdoc/DOC-INDEX.tsv`, and
    historical docs are deliberately not anchor-gated); the drifts it does name are
    in `devdoc/assert-the-bug-audit-20260917.md` and
    `devdoc/agent-five-organs-ontology.md`, neither touched here. Filed (completed
    rows `01a13100-8ab8-7d23-bb19-cd7675001a96`, `01a12f20-4b9e-7eb7-a8be-819587839f93`,
    `01a12bdd-0673-7e51-a16b-9a24399867da`).
  - `cmd/agent` **package-level FAIL with 0 failed tests**: the run's own close-out
    prints `suite goroutine sweep: parked repository goroutines: 6`, every frame
    `cmd/agent.workerDaemonCtx.installFileWatcher.func1` (`cli_worker_control.go:2164`)
    — pre-existing, already filed (`01a133f4-82e9-79a3-9a03-311b7924d6c8`,
    `01a133c3-8008-7c5f-b311-77e7147f3a18`), on a file this diff does not touch.

  The new case in isolation, against BOTH binaries (the red/green transition):
  - `GOPI_BIN=<old binary built from rev 3544b690> go test ./cmd/agent -run TestNotifyPeerCLITaskRunRecordKeepsItsContainer`
    → **FAIL**, `inbox: …/.gopi/sessions/messages/inbox.jsonl`, and the container
    inbox is missing (`no such file or directory`);
  - `GOPI_BIN=<this branch's binary> go test ./cmd/agent -run TestNotifyPeerCLITaskRunRecordKeepsItsContainer`
    → **ok github.com/nick/gopi/cmd/agent** (same package, same fixture).
- **Ran** (the surface the diff changed, verbatim, with real arguments): the
  cross-peer hop, driven end to end by two real `gopi` processes and a real `ssh`
  shim, over ONE peer profile shape
  (`<peer-home>/.gopi/sessions/<task-id>/<run-uuid>.jsonl`), once with the tree
  at rev `3544b690` as the PEER and once with this branch as the peer —
  transcripts in `## Evidence`. The initiator is the OLD binary in both arms on
  purpose (the defect is on the receiving side).
- **Result**: old peer → `PRESENT <peer>/.gopi/sessions/messages/inbox.jsonl`
  (stray) and the container inbox ABSENT, with the initiator exiting 0 and
  printing a successful hop; new peer → the container inbox
  `<peer>/.gopi/sessions/<task-id>/messages/inbox.jsonl` PRESENT and the stray
  ABSENT, same exit 0. Same command, same profile shape, opposite landings.
- **Commit**: the branch tip (`git log --oneline -1` on
  `task/01a1337dbe087acf906aaa01015e2293`).

## Files

| file | change |
|---|---|
| `internal/sessionlayout/sessionlayout.go` | `RecordContainer` — the ONE definition of "which directory holds this record", with the three layouts and the measured defect |
| `internal/sessionlayout/sessionlayout_test.go` | `TestRecordContainerIsTheRecordsOwnDir` — the cheap in-process half of the same guard |
| `internal/tools/notify_peer.go` | the basename peel is deleted; the container comes from `sessionlayout.RecordContainer` |
| `cmd/agent/cli_notify.go` | `filepath.Dir(path)` replaced by the shared definition (behaviour unchanged; the rule now has one home) |
| `internal/replay/session_ground.go` | its two record→container derivations now call `sessionlayout.RecordContainer` (Rules 13/14 sweep) |
| `internal/agent/session_inbox.go` | `sessionInboxDir` — the inbox READER side — now derives the container from the same definition as the writer |
| `cmd/agent/cli_notify_deliver_idle_test.go`, `cli_notify_duplicate_test.go`, `cli_notify_deliver_test.go` | notify fixtures' record→dir derivation folded into the shared definition |
| `cmd/agent/cli_notify_peer_test.go` | `TestNotifyPeerCLITaskRunRecordKeepsItsContainer` + the `writePeerTaskRunSession` fixture |
| `devdoc/notify-peer-run-uuid-inbox.md` | this report |
| `devdoc/DOC-INDEX.tsv` | one row classifying it (Rule 36) |

## Evidence (self-filed)

Every line below is a command this run executed and the output it saw; the
transcripts live under this run's session directory
(`~/.gopi/sessions/01a1337d-be08-7acf-906a-aa01015e2293/`).

- **Red → green, on the real hop.** Both binaries were built from THIS worktree:
  the old one from `git archive 3544b690c` (a rev read out of the object store
  into a scratch dir — Rule 29, no second checkout of the working repo), the new
  one from the branch tree. Transcripts: `redproof/old.transcript.txt`,
  `redproof/new.transcript.txt` (driver `redproof-run.sh`).

  ```
  ########## red-vs-green: where the same message landed ##########
  --- arm old ---
  (initiator exit: 0)
  ABSENT  …/redproof/old/peer/.gopi/sessions/01a10f79-1111-…-444444444444/messages/inbox.jsonl
  PRESENT …/redproof/old/peer/.gopi/sessions/messages/inbox.jsonl
           {"kind":"message","id":"6c99cc354dfbd5bf","session_id":"01a10f79-5555-6666-7777-888888888888",
            "message":"NOTIFY-PEER-REDPROOF-old: addressed by the run uuid","source":"notify",
            "created_at":"2026-10-13T07:19:32Z"}
  --- arm new ---
  (initiator exit: 0)
  PRESENT …/redproof/new/peer/.gopi/sessions/01a10f79-1111-…-444444444444/messages/inbox.jsonl
           {"kind":"message","id":"ad8d6f9966312523","session_id":"01a10f79-5555-6666-7777-888888888888",
            "message":"NOTIFY-PEER-REDPROOF-new: addressed by the run uuid","source":"notify",
            "created_at":"2026-10-13T07:19:32Z"}
  ABSENT  …/redproof/new/peer/.gopi/sessions/messages/inbox.jsonl
  ```

  Both arms' initiator stdout carried `queued on hp: message … for session
  <run-uuid>` — i.e. **the red arm reported success while writing to a file no
  session reads**, which is the defect's whole shape. Each arm's shim log records
  the argv it was handed (`shim peer=hp argv=gopi notify --notify-remote`), so
  the transport really ran rather than being assumed.

- **The LITERAL same-profile control** (`redproof/c_crosscheck.transcript.txt`):
  arm `old`'s own peer profile was addressed a SECOND time, with the NEW binary
  as the peer. Both landings then coexist in ONE profile, which is the tightest
  form of the comparison — same tree of files, same run, no fresh fixture:

  ```
  $ gopi-old notify --peer hp --session <run-uuid> --message "…c1…"      # peer = NEW
  (initiator exit: 0)
  PRESENT <redproof/old/peer>/.gopi/sessions/01a10f79-…-444444444444/messages/inbox.jsonl
     {"kind":"message","id":"21c86aa68a39f218","…":"NOTIFY-PEER-REDPROOF-c1:
      the fix, on the very profile the old peer polluted","source":"notify"}
  PRESENT <redproof/old/peer>/.gopi/sessions/messages/inbox.jsonl          ← the OLD peer's stray, still there
     {"kind":"message","id":"6c99cc354dfbd5bf","…":"NOTIFY-PEER-REDPROOF-old:
      addressed by the run uuid","source":"notify"}
  ```

  One profile, one command, two peer binaries: the old peer's copy sits in the
  root (a file no session reads), the new peer's copy sits in the container under
  the run uuid.

- **Regression case, red then green (same fixture, same package).**

  ```
  $ GOPI_BIN=<old binary> go test ./cmd/agent -run TestNotifyPeerCLITaskRunRecordKeepsItsContainer
  --- FAIL: TestNotifyPeerCLITaskRunRecordKeepsItsContainer
      cli_notify_peer_test.go:451: the receipt does not name the container's inbox (…/002/.gopi/sessions/
          01a10f79-1111-2222-3333-444444444444/messages/inbox.jsonl):
          queued on hp: message 3a9098dc09deb78c for session 01a10f79-5555-…-888888888888
                  inbox:  …/002/.gopi/sessions/messages/inbox.jsonl on hp   ← the stray landing
      cli_notify_peer_test.go:455: read the peer inbox at …/01a10f79-…-444444444444/messages/inbox.jsonl:
          no such file or directory
  FAIL	github.com/nick/gopi/cmd/agent	3.266s

  $ GOPI_BIN=<this branch's binary> go test ./cmd/agent -run TestNotifyPeerCLITaskRunRecordKeepsItsContainer
  ok  	github.com/nick/gopi/cmd/agent	3.369s
  ```

  (Saved as `redproof-test-old.txt` / `redproof-test-new.txt`.)

- **The single container definition**, measured (after the fold-in):

  ```
  $ grep -rn 'RecordContainer' --include=*.go . | grep -v _test.go
  ./internal/replay/session_ground.go:168:	dir := sessionlayout.RecordContainer(record)
  ./internal/replay/session_ground.go:202:		dir := sessionlayout.RecordContainer(p)
  ./internal/agent/session_inbox.go:90:	return sessionlayout.RecordContainer(p), true
  ./internal/sessionlayout/sessionlayout.go:354:// RecordContainer returns the directory that CONTAINS a session record — the
  ./internal/sessionlayout/sessionlayout.go:379:func RecordContainer(recordPath string) string { return filepath.Dir(recordPath) }
  ./internal/tools/notify_peer.go:414:	// <sessions>/messages/, which no session reads (see RecordContainer).
  ./internal/tools/notify_peer.go:415:	dir = sessionlayout.RecordContainer(record)
  ./cmd/agent/cli_notify.go:818:	// the answer is taken from the ONE definition (sessionlayout.RecordContainer),
  ./cmd/agent/cli_notify.go:820:	dir = sessionlayout.RecordContainer(path)
  $ grep -n 'filepath.Dir' internal/tools/notify_peer.go cmd/agent/cli_notify.go
  (no output)
  ```

  One definition, five call sites, and `filepath.Dir(<record>)` written as the
  container rule inside the notify/replay/inbox family: nowhere. The chat/
  console-log family's own copies are listed in `## Not Done`.

- **`git status --porcelain`** in the worktree after the commit: prints nothing
  (Rule 30). Build scratch (`bin-gopi-old`, `bin-gopi-new`) and the extracted rev
  live under `/home/user/workspace/gopi-scratch-01a1337d/` — outside the worktree
  and off `/` (Rule 22).
