<!-- doc-class: normative -->
# A self-peer write leg is refused by name — the alias that resolves to THIS host

**Task:** `01a117cad9f5701ba5373dd04f227638` (branch `task/01a117cad9f5701ba5373dd04f227638`, 2026-10-08)
**Defect first measured at:** master `087f506df` (task `01a117c9-1111`)
**Fix:** `internal/tools/peer_self_host.go` (+ the two gates it serves)

## The defect, and why it was not a configuration slip

A review task running inside a private sandbox (Rule 34) ran

```bash
> **Retired mechanism (2026-11-05).** The cross-node sync family this page
> describes — `gopi task sync`, `diff`, `harvest`, `push`, `send`, `receive`,
> `pull`, `sync-pass` — is DELETED. No verb in this binary opens a session
> against another node's ledger; each of those spellings still dispatches to a
> removal sentence, and work that must run on another host is FILED there with
> `gopi task submit`. The text below is the record of the mechanism as it was,
> kept verbatim because it is the evidence the removal is judged against.

GOPI_HOME=$SB GOPI_SYNC_PEERS=peerbox gopi task sync --peer peerbox --next-eligible <id>
```

and **four fixture rows landed in the operator's real `~/.gopi/memory.db`**; two
of them were then picked up as real work. The copies carry the `from-peerbox`
provenance tag, which is written only at *import* — so they provably arrived
through the peer leg rather than through a local submit.

Everything the caller did was correct: its own profile was right, the peer was
declared, the verb was the documented one. The defect is entirely in the leg.

**Two halves, both measured:**

1. **The alias names this machine.** `~/.ssh/config` carries `Host peerbox` →
   `10.0.0.6`, and `10.0.0.6` *is* this host's own address. So
   `--peer peerbox` on peerbox is not a remote machine at all: it is an `ssh` to
   self, and the peer-side argv runs *here*.

   ```
   $ ssh -G peerbox | grep -i hostname
   hostname 10.0.0.6
   $ hostname -I
   10.0.0.9 10.0.0.6 …
   ```

2. **Nothing binds the profile.** `syncRowSSHExchange` / `syncTransferSignOver`
   / `SyncTransferPickRow` prefix the peer-side argv with `GOPI_HOME=… PATH=…`
   **only when `--remote-home` is non-empty**. Absent the flag, the peer's own
   login shell decides — and for an alias pointing at self, that is the
   operator's profile.

Either half alone is harmless. Together they make a leg whose entire purpose is
isolation write the one ledger isolation exists to protect.

## The criterion (one sentence)

> A peer leg must not run the peer process in a profile the caller did not
> choose; on a self-peer, "no `--remote-home`" leaves that choice to the local
> login shell, which is never a remote machine's profile.

## The fix

`internal/tools/peer_self_host.go` — one predicate and one verdict, referenced
by the legs rather than re-stated (Rule 14):

| symbol | job |
|---|---|
| `selfPeerLocalAddrs` | every address this host answers to (interfaces **and** what its own name resolves to) |
| `selfPeerResolvedHost` | the `HostName` the ssh client's own config gives the alias — read as **text** from `$HOME/.ssh/config`, `GOPI_HOME/.ssh/config` and the operator's `~/.ssh/config`, first `HostName` wins |
| `selfPeerAliasIsLocal` | the verdict, + the sentence naming the evidence (`resolves to 10.0.0.6, and it is one of THIS host's addresses`) |
| `selfPeerWriteRefusal` | **the gate**: called by every about-to-write leg before any contact |

**Gated (write) surfaces:**
- `SyncRowSendEx` — `gopi task send` / `task sync --push-only`
- `SyncTransferTakeOver` — `gopi task receive` / `receive --takeover`

**Deliberately NOT gated (read) surfaces:** `SyncTransferPickRow` (the OFFER)
and `SyncRowReceive`'s ssh+sqlite fetch. An unbound read is the only shape that
reaches a real remote ledger at all: `harvestRemoteDBPath` is `~/.gopi/memory.db`,
a **relative** path the peer's shell expands — binding the caller's `GOPI_HOME`
onto that ssh would make a sandboxed pull look for `<sandbox>/.gopi/memory.db`
in the *remote* filesystem and fail with "row not found" rather than with a
refusal. The read leg has its own isolation channel (`--remote-db`), which is
what the measured probe used. A write is different in kind: nobody can undo it.

**Out of scope, stated rather than implied away:** the claim broadcast
(`--claim-remote`) and the session leg (`--serve-remote`) also write on the peer.
They are guarded today only by a self-exclusion *filter* that compares the peer
to this node's identity **name** — which does not catch an alias that *resolves*
here (half ① above). The same predicate is now available to close them; that is
a separate change with its own measurements.

## Why a refusal and not a better default

The caller's intent is not inferable from the command line: with no
`--remote-home`, "the peer operator's own profile" is the **production shape**
for a real peer and the **leak** for a self-peer. Any default would either break
the ordinary deployment or keep writing the wrong ledger. So the leg says which
it is, and hands over the way out (`--remote-home DIR`, the flag that already
exists for exactly this).

A false "local" would refuse a working deployment, so the predicate answers
"not local" for anything it cannot prove — and the *verdict* is not weakened by
that, because in **both** sentences the profile the peer would use was never
chosen by the caller. The caller's ledger only selects which sentence is true:

- caller's ledger **is** the operator's → "this write would land in the profile
  THIS caller resolves";
- caller is already isolated (a sandbox, or an explicit `--db`) → "the peer side
  would run in the OPERATOR's own profile" — the measured incident.

Comparing the **ledger** rather than `GopiDir()` matters in one case: a caller
standing in the operator's profile while pointing the leg at a private `--db`.
`GopiDir()` would there say "the caller is the operator, so this is intended"
and pass the very write the gate exists to stop.

## Verification (Rule 5 / Rule 31)

### RED — before the fix, by assertion, not by compile error

```
### RED PROOF 1 — the send leg, gate disabled for one run
$ go test ./internal/tools/ -run TestZZRedProofShowsTheLeak -count=1
    RED-PROOF: contacted=true err=<nil> peerLedgerRows=1
    LEAK OBSERVED: the peer's unbound profile gained 1 row(s)
--- FAIL
```

The leak **observed**, not inferred: the peer-side handler ran, the leg returned
**no error at all** (exit 0 is exactly what made the production defect silent),
and the profile nobody chose gained the row.

```
### RED PROOF 2 — the takeover leg, gate disabled for one run
$ go test ./internal/tools/ -run TestSelfPeerTransferTakeOverIsRefusedBeforeTheOffer -count=1
--- FAIL: the round did not refuse by the self-peer gate …
    transfer: ssh gopi-selfpeer-probe: exit status 255
```

Pre-fix the round **reached ssh** (255 = it dialled). With the gate on it is
refused before any contact.

Raw capture: `~/.gopi/sessions/01a117ca-d9f5-701b-a537-3dd04f227638/artifacts/red-proof-before-fix.out`

### GREEN — the reply test set

```
$ go test ./internal/tools/ -run 'TestSelfPeer|TestRemotePeerSendIsNotTouched' -count=1 -v
--- PASS: TestSelfPeerSendIsRefusedAndTheOperatorResolvedLedgerGainsNoRow (0.05s)
--- PASS: TestSelfPeerSendStillWorksWithRemoteHome (0.07s)
--- PASS: TestRemotePeerSendIsNotTouchedByTheSelfPeerGate (0.04s)
--- PASS: TestSelfPeerGateDoesNotFireWhenTheLedgerIsAlreadyPrivate (0.03s)
--- PASS: TestSelfPeerTransferTakeOverIsRefusedBeforeTheOffer (0.00s)
ok  	github.com/nick/gopi/internal/tools
```

`TestRemotePeerSendIsNotTouchedByTheSelfPeerGate` is the no-collateral arm: an
alias resolving to `192.0.2.7` (TEST-NET-1, reserved and never on an interface)
sends exactly as before. `TestSelfPeerSendStillWorksWithRemoteHome` is the
designed same-host path: it reaches the peer, and the row is read back out of the
**bound** profile's ledger with the ledger's own opener — never inferred from the
receipt.

### Build + branch-scoped suite

```
$ go build ./...                              # OK
$ make test-budget PKGS='./internal/tools'    # 1 finished, 0 with a failure
                                              # 345s wall, exit=0, outcome=pass
                                              # stamp 2026-10-08T03:44:19+08:00 host=peerbox
$ go test ./internal/standards/ -count=1      # ok 72.505s
$ go test ./cmd/agent/ -run 'TestTaskSyncSurface|TestSync.*Surface|TestCliTasksSync'   # ok
```

Raw stream: `~/.gopi/sessions/01a117ca-d9f5-701b-a537-3dd04f227638/artifacts/tools-suite.measured.out`
(`run-1791402259-2026758`).

`internal/stylegate` reports hits in this tree, but **none in any file this
change touches** — `peer_self_host_test.go` was on its `test-setenv` list at
first and now carries the reviewed per-call escape
(`//stylegate:ignore the scope is ONE peer leg, not the test`), which is the
documented shape for a value that must be unwound when a *closure* returns
rather than at test cleanup. The remaining hits are pre-existing sites this
branch does not modify (`internal/tools/task_transfer.go:249`,
`chat_ops_authorization_test.go`, `cmd/agent/*_test.go`, `internal/lineedit/…`)
and are therefore a baseline fact of this tree, not a regression from this fix.

### Ran: — the REAL binary, in a private sandbox (Rule 31)

```bash
$ CGO_ENABLED=0 go build -o $ART/gopi ./cmd/agent/
$ gopi tool sandbox create --label selfpeer --task-id 01a117cad9f5701ba5373dd04f227638
sandbox /home/user/workspace/gopi-sandbox/sandbox-selfpeer-c03c16fa created=true …
$ GOPI_HOME=$SB GOPI_SYNC_PEERS=peerbox $gopi task submit "selfpeer live probe row (sandbox-local)" …
Task queued: 01a117e6-8028-7275-b731-8b6226a7aa6c (status=pending, source=agent)

# THE LEG — self-peer alias, NO --remote-home
$ GOPI_HOME=$SB GOPI_SYNC_PEERS=peerbox $gopi task send --task-id 01a117e6-… --peer peerbox
gopi task sync: row send: peer names this host: refusing a WRITE leg against a peer
that names THIS host: row send for peer peerbox: it names THIS host (the alias "peerbox"
resolves to 10.0.0.6, and it is one of THIS host's addresses (10.0.0.6)), and no
--remote-home was given — … Pass --remote-home DIR to BIND it: …
EXIT=1                                    ← the named refusal, non-zero

# CONTROL — the same leg WITH --remote-home (a second private sandbox)
$ GOPI_HOME=$SB GOPI_SYNC_PEERS=peerbox $gopi task send --task-id 01a117e6-… --peer peerbox \
      --remote-home /home/user/workspace/gopi-sandbox/sandbox-selfpeer-peer-be939738
send peerbox: stored (row 01a117e6-8028-7275-b731-8b6226a7aa6c, status=pending)
EXIT=0

# the row really landed in the BOUND profile (read back through that profile's own reader)
$ GOPI_HOME=<peer sandbox> $gopi task list | grep 01a117e6-…
#01a117e6-8028-7275-b731-8b6226a7aa6c  [pending]  selfpeer live probe row (sandbox-local)
```

**The operator ledger, counted read-only** (`modernc.org/sqlite`,
`file:…?mode=ro`, the same file the leak wrote):

```
BEFORE: 3777
AFTER : 3777
```

Unchanged across both legs. Raw capture:
`~/.gopi/sessions/01a117ca-d9f5-701b-a537-3dd04f227638/artifacts/selfpeer-live-refusal.out`

> **Why the operator ledger is measured here and not in the test.** A Go test
> that resolved, let alone wrote, `~/.gopi/memory.db` would be the very defect
> this task is about — and the ledger is **append-only**, so a test that "proved"
> the leak by reproducing it there would leave a permanent fixture row. The test
> fixture therefore reproduces the leak's **shape** (a private temp profile
> standing in for "whatever the login shell resolves") and asserts on counts; the
> real file is measured only here, read-only, with no write path in the run.

## What this does NOT change

- **A real remote peer is untouched**: the gate fires only when the alias is
  local, and it is a no-op before that (`--peer hp` from peerbox never reaches it).
- **`--remote-home` semantics are unchanged** — the prefix, the `:$PATH` tail and
  the "a sandbox with no `bin/` runs the peer's own gopi" behaviour are byte-for-byte
  as they were; the flag simply became mandatory for the self-peer case.
- **No new tool, no new dependency, no new daemon**: one file, one predicate, two
  call sites, and a constant (`SyncRowRemoteHomeFlagName`) moved to its single
  source so the refusal can name the flag without a mirrored literal (Rule 13).

## Files

- `internal/tools/peer_self_host.go` — the predicate, the verdict, the scope note
- `internal/tools/peer_self_host_test.go` — the repro + the four arms
- `internal/tools/memory_sync_row.go` — the send-leg gate (11 lines)
- `internal/tools/task_transfer.go` — the takeover gate (12 lines)
- `cmd/agent/cli_tasks.go` — `syncRemoteHomeFlag` now references the tools-layer constant
