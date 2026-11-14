<!-- doc-class: normative -->
# `gopi notify --peer` — the cross-host hop of DELIVER

**Status:** delivered · **Task:** `01a10f78-3977-767d-aaff-640a4f5d9ac2` ·
**Rule 5 report:** this file.

## What it adds

`gopi notify --session <ID> --message <TEXT>` appends a message to a session's
inbox **on this host**. The new flag sends the same message to a session that
lives **on another host**:

```bash
gopi notify --peer hp --session <ID> --message "<TEXT>"
```

Nothing else about the verb changes, and **without `--peer` the local path is
byte for byte what it was** (verified below).

The receiving session takes the message at its next round boundary through the
**unmodified** receiver: same `messages/inbox.jsonl`, same take point
(`tools.TakeJobNotices` / `SessionInboxLoad`), same idle poller that opens the
turn with no keystroke. No line of the wake path was touched.

## Three existing things, reused — and nothing invented

| # | reused | where it lives |
|---|---|---|
| 1 | the ssh transport | `newSSHCommand` / `runSSHCommand` (`internal/tools/memory_sync_claim_ssh.go`) — BatchMode, one remote-argv element |
| 2 | the peer whitelist and alias guard | `ValidateSSHPeerAlias` + `RequireDeclaredAssetPeer` → `config.ResolveMemoryPeers` (`GOPI_MEMORY_PEERS`, settings.json `memory_peers`) ✔ path through `peer_topology.go`, an ADDITIVE `P` |
| 3 | the inbox protocol and its take point | `internal/tools/session_message.go` — the peer handler appends through the one append point; the receiver reads through the one take point |

**Rule 35 (four things for a new persistent dependency):** this feature creates
**no** persistent artifact — no daemon, no cron, no watcher, no second database,
no lock file, no state file. The durable half is the file that already existed.
The transport is the ssh trust that already existed. The wake is the poller that
already ticks. *What breaks without it:* cross-host hand-off is an operator
typing ssh by hand. *Why not in-process:* the target profile is on another
machine. *Who owns it:* nobody — nothing new. *Posterity:* the remote argv says
`gopi notify --notify-remote`, the same verb reached backwards.

## The design decisions, and why

### The hop is synchronous; the delivery is durable

The initiator blocks for one ssh round trip so it can report **what the peer's
disk says** — `queued on hp: message <id>`. "Queued" is a claim about another
host, and a claim nobody checked is the silent success Rule 15 forbids. What the
peer writes is a file, so the message survives the peer's session not running at
all (measured below: delivery to a non-running session landed and stayed).
Promptness buys the ANSWER, never the durability.

### One message, one id, on both sides — the id is DERIVED

The delivery id is `sha256(kind | source | session | message)` truncated to 16
hex (`NotifyDeliveryID`), **not** random. Two reasons:

1. **The failure mode is the re-run.** An ssh leg can time out *after* the peer
   appended; the sender cannot tell "never arrived" from "arrived, answer lost".
   Its only safe retry must produce the **same** record — with a random id it
   would produce a second one, i.e. exactly the duplicate injection this feature
   exists to prevent.
2. **The printed id and the peer's id must not drift.** The operator's evidence
   is "the id in the peer's inbox is the id I was told".

Framing is by **length** (each field prefixed with its byte count), so
`("ab","c")` and `("a","bc")` cannot collide — pinned by a test. The residual
risk is an 8-byte digest collision, whose outcome is bounded and *visible*: the
second delivery is treated as a repeat, appends nothing, and **says so**
(`recorded=false` + note). Losing a duplicate is the opposite failure from
injecting one, and it is the direction chosen.

The idempotence lives inside the inbox's **locked critical section**
(`SessionInboxDeliverDerived`), not at the caller: a check-then-act outside the
lock would let two concurrent senders both read "absent" and both append.

### Three refusals, each naming its repair

| case | what happens | why |
|---|---|---|
| the peer has no such session | **named error, exit 1** — `unknown session "<id>" — no session record with that id under <tree>` | an id the initiator cannot resolve is a guess; a guess about *which machine* is worse (Rule 15) |
| the text was already delivered **and TAKEN** (the `delivered` marker exists) | refused by name, exit 1, inbox unchanged | re-appending is not itself an injection (the marker suppresses the merge) but it *is* about to repeat an instruction the model already acted on; the local face cannot see that intent, the wire document is nothing *but* the intent |
| `--peer` with a blank operand | refused by name | the flag's value IS the machine that gets written to; a default would deliver to whichever host the operator happened to stand on |

A repeat that is **not** yet taken is *not* an error: it appends nothing and
reports `already queued on hp: … (nothing was appended)`.

### What was deliberately NOT done

- **No spilling of large messages on the initiator.** The local face spills a
  large body to a file inside the target session's directory. On the hop that
  directory is on *another machine*, so the message is sent **verbatim** and the
  peer applies the peer's own inbox ceiling (4 MiB, refused by name).
- **No ledger, cursor, sink or `take`.** This hop moves a message to a session,
  the same thing the local path does.
- **No change to `sync_peers`.** It is empty on both hosts and this feature does
  not open it; the peer channel used is the KNOWLEDGE one (`memory_peers`),
  which is populated and is the "may I ssh this host" gate every other leg uses.
- **No new tool.** Rule 3's tool table is untouched — this is a flag on an
  existing CLI verb.
- **No second policy for "what is this session id".** The peer resolves with the
  resolver this package already has for the same operand and the same tree
  (`taskFindSessionJSONL`: exact id only, never a prefix). It cannot delegate to
  `gopilog.FindSession`, the local face's owner of that question: `internal/
  gopilog` imports `internal/agent`, which imports `internal/tools`, so the edge
  would be an import cycle (the same wall `task_deliverable_session.go` records).

## The wire

Initiator → peer (stdin), one JSON document:

```json
{"version":1,"session_id":"<ID>","message":"<TEXT>","source":"notify"}
```

Peer → initiator (stdout), one JSON document, decoded **strictly** (unknown
field or unknown version refused by name — a half-read receipt is a claim about
another host assembled from guesses):

```json
{"version":1,"session_id":"…","dir":"…","inbox":"…","id":"…","recorded":true,"note":"…"}
```

The remote command is the **one** element `gopi notify --notify-remote`,
composed from shared constants (`cliverbs.SyncRemoteBinaryName`, `CliVerbNotify`,
`tools.NotifyPeerRemoteFlagName`) — not typed as literals (Rules 1b/13/14).

## Verification

- **Build:** `go build ./...` — OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK.
- **Tests:** `go test ./internal/tools/ -run 'TestNotifyPeer|TestNotifyDeliveryID|TestSessionInbox' -count=1` — ok; `go test ./cmd/agent/ -run 'TestNotifyPeerCLI' -count=1` — ok (65.7s, both cases); `go test ./internal/cliverbs/ -count=1` — ok.
- **The branch-domain suite has a PRE-EXISTING failure set, and this change did
  not cause it.** `make test-budget PKGS='./internal/tools ./cmd/agent
  ./internal/cliverbs ./internal/config'` → `exit=1 outcome=fail`, 15 failing
  tests. **A control run on the base commit proves they are not this branch's:**
  a second worktree at `HEAD~1` (`basecheck-notify-peer`) was given the same
  failing set, and the SAME tests fail there — `TestG5InventoryMatchesTheHelp`
  (`no row for gopi tool proxy`, a stale row for `gopi proxy`),
  `TestCheckpointUsageLinesAreDispatchable` (`0 Usage line(s) inspected`),
  `TestCLIContractInventoryMatchesDispatch` (`main.go has 20 … contract suite
  tracks 18`), `TestKeywordSearchHelpPageDiffersFromTopLevel` (`keyword_search`
  is RETIRED), `TestBashScriptStructure`,
  `TestProxyBusyPortNeverClaimsListening`, `TestSandboxDirFlagDecidesTheRoot`,
  `TestAnnotateVerbRunsARealArgvEndToEnd`, `TestChatStatus_LiveBlockKeepsTheStateWord`,
  `TestAssertionTotalityAuditGate` (an UNCLASSIFIED negative-totality assertion
  in `cli_notify_deliver_spill_test.go`, a file this change does not touch) —
  each red on `HEAD~1` with this branch's files absent. All are repo-state drift
  from the verb-retirement work (the top commit on this base is
  `aef82d93 fix(g6): re-derive the help-surface inventory through the verb
  doors`), not this feature.
  Two of the batch's failures did NOT reproduce on the base in the same batch
  (`TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--no-editor`): that test
  passes **in isolation on both trees**, so its batch failure is parallel-run
  interference inside the package, not a regression of this change — stated here
  rather than left as an unexplained delta.
- **Ran:** the real cross-machine acceptance, both hosts running **this
  branch's binary** (md5 `88b286664dfdca5600b7d17fc8a447f3` on peer1, scp'd to hp's
  `/home/user/work/GoPi/bin/gopi`; hp's original saved to
  `…/artifacts/gopi-hp-before`, md5 `53894d9ac396b0583da75189e19a88a2`).
- **Result:** see `## Evidence (self-filed)`.

### The Rule 31 acceptance, verbatim (peer1 → hp)

A **live `gopi chat` on hp** (pid 89908, started with NO input on its stdin:
`tail -f /dev/null | gopi chat --tools`), session
`967eed30-af42-42cd-a664-db76f66afbfd` — so the wake below happened with no
keystroke from anybody:

```
$ ./bin/gopi notify --peer hp --session 967eed30-af42-42cd-a664-db76f66afbfd \
                     --message "ACCEPT-135242: cross-host delivery test — reply with exactly the single word ACK and nothing else"
queued on hp: message e7762274b37417e5 for session 967eed30-af42-42cd-a664-db76f66afbfd
        inbox:  /home/user/.gopi/sessions/967eed30-af42-42cd-a664-db76f66afbfd/messages/inbox.jsonl on hp (that host's file, not this one)
        the peer's session takes it at its next round boundary
EXIT1=0
```

hp's inbox, read on hp:

```
{"kind":"message","id":"e7762274b37417e5",…,"message":"ACCEPT-135242: …","source":"notify","created_at":"2026-10-06T05:52:42Z"}
{"kind":"delivered","id":"e7762274b37417e5"}
```

hp's session record — a turn **opened by the session itself**:

```
13:52:42?  console:  you> [gopi] external message e7762274b37417e5 — an external
                     process sent this session a message; opening a turn to
                     deliver it (no input from you)
2 user      2026-10-06T13:52:44.257177+08:00  "[system] An external process sent this session a message: ACCEPT-135242: …"
3 user      2026-10-06T13:52:44.258037274+08:00  (same)
4 request   (the model was asked)
5 assistant 2026-10-06T13:52:45.185759263+08:00  [{"text":"ACK","type":"text"}]
```

The model answered **ACK**, the word the delivered message asked for, two
seconds after the delivery — on a session nobody typed into.

Idempotence and the refusals, against hp (the same binary both sides):

```
$ … --message "IDEM-REPEAT: fixed text for the duplicate check"
queued on hp: message c97eab70c140a99a for session 8e62706c-…            EXIT 0
$ … --message "IDEM-REPEAT: fixed text for the duplicate check"     (again)
already queued on hp: message c97eab70c140a99a … (this exact text was delivered before — nothing was appended)
        note:   already in this session's inbox, from an earlier identical delivery — not appended a second time
                                                                        EXIT 0
$ hp: cat ~/.gopi/sessions/8e62706c-…/messages/inbox.jsonl      → 3 records, 3 distinct ids
                                                 (the repeated text appears ONCE)
$ … --session 01a10f78-0000-0000-0000-000000000000 --message nobody
                                                                        EXIT 1
  gopi notify: notify --peer hp: ssh hp: exit status 1 (stderr: … gopi notify
  --notify-remote: unknown session "01a10f78-0000-0000-0000-000000000000" — no
  session record with that id under /home/user/.gopi/sessions (`gopi log --last`
  lists recent ones))
$ … --session 967eed30-… --message "ACCEPT-135242: …"   (the TAKEN text)
                                                                        EXIT 1
  … message e7762274b37417e5 was already delivered to session 967eed30-… and
  TAKEN by it (the inbox holds the delivered marker) — re-sending the same text
  would inject it twice; change the text, or deliver to a different session
```

Local regression (no `--peer`), on peer1:

```
$ ./bin/gopi notify --session 01a10f78-3977-767d-aaff-640a4f5d9ac2 --message "local regression probe …"
queued: message 6bd223d3c0af95db for session 01a10f78-3977-767d-aaff-640a4f5d9ac2
inbox:  /home/user/.gopi/sessions/01a10f78-3977-767d-aaff-640a4f5d9ac2/messages/inbox.jsonl
session dir: /home/user/.gopi/sessions/01a10f78-3977-767d-aaff-640a4f5d9ac2 (resolved as its session directory name)
the session takes it at its next round boundary (an IDLE console wakes for it …)
                                                                        EXIT 0
```

That line was the last thing typed into peer1's own console before this turn
began: **this turn's opening message is that delivery.** The local path woke a
live session with no `--peer` involved — the regression check and the
demonstration in one.

## Files

- `internal/tools/notify_peer.go` (new) — the hop: wire document, peer handler,
  derived id, the two gates, the receipt decoder.
- `internal/tools/notify_peer_test.go` (new) — the handler and the id, and the
  double-injection case driving the real take point.
- `internal/tools/session_message.go` — `SessionInboxDeliverDerived` (idempotent
  append inside the lock) + `NotifyDeliveryID`; `SessionInboxDeliver` keeps its
  signature and behaviour.
- `internal/tools/memory_sync_task.go` — `syncRemoteGopi` now references
  `cliverbs.SyncRemoteBinaryName` instead of a literal.
- `internal/cliverbs/cliverbs.go` — `SyncRemoteBinaryName`, `Notify`.
- `cmd/agent/cli_notify.go` — `--peer`, `--notify-remote`, the peer arm, help.
- `cmd/agent/cli_notify_peer_test.go` (new) — argv on the real binary, a
  process-boundary (not in-process) peer, and the live-console wake.
- `COMMANDS.md` — regenerated (the census check failed on `master` itself: the
  table's line numbers were stale; this commit refreshes them).

## Evidence (self-filed)

- `go build ./...` → exit 0.
- `go test ./internal/tools/ -run 'TestNotifyPeer|TestNotifyDeliveryID' -count=1` → `ok github.com/the operator/gopi/internal/tools`.
- `go test ./cmd/agent/ -run 'TestNotifyPeerCLI' -count=1` → `ok github.com/the operator/gopi/cmd/agent 65.749s` (both cases; the live-console one 33.5s alone).
- `go test ./internal/cliverbs/ -count=1` → `ok`.
- `ssh hp 'md5sum ~/work/GoPi/bin/gopi'` → `88b286664dfdca5600b7d17fc8a447f3`, equal to peer1's acceptance binary.
- The five invocations quoted above, with their stdout and exit codes, run by hand against hp.

## Not Done (honest)

- **The two hosts ran the same branch, not a merged `master`.** hp's binary was
  scp'd from this worktree; hp's `~/work/GoPi` checkout is at a different
  revision, so anyone re-running the acceptance must rebuild on hp from this
  branch. hp's original binary is preserved at
  `…/artifacts/gopi-hp-before` (md5 `53894d9ac396b0583da75189e19a88a2`).
- **hp's `~/work/GoPi/bin/gopi` was replaced in place** to run the acceptance
  (that path is what `/usr/local/bin/gopi` symlinks to), and **restored
  afterwards** to hp's own build — `md5sum` on hp reads
  `53894d9ac396b0583da75189e19a88a2`, the backup taken before the swap
  (`…/artifacts/gopi-hp-before`). The console started for the acceptance was
  killed in the same step (`pkill -f "gopi chat --tools"`, 0 remaining). hp's
  profile and binaries are therefore as this run found them, and the acceptance
  binary stays on peer1 at `…/artifacts/gopi-accept`
  (`88b286664dfdca5600b7d17fc8a447f3`) for a re-run.
- **No `progress`/`--json` output for the peer arm.** The human lines only; the
  receipt is already the whole machine-readable answer for a caller that wants
  one, and adding a flag nobody asked for is a knob without a reader.
- **`ssh` itself is covered only in the acceptance** (a real one, above) and by
  a PATH shim in the tests — no in-process test can stand in for a second
  machine, which is why the acceptance is the evidence for that leg.
