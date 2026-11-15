<!-- doc-class: normative -->
# `task diff --peer` is whitelist-gated: declared topology, before the transport

> **Retired mechanism (2026-11-05).** The cross-node sync family this page
> describes — `gopi task sync`, `diff`, `harvest`, `push`, `send`, `receive`,
> `pull`, `sync-pass` — is DELETED. No verb in this binary opens a session
> against another node's ledger; each of those spellings still dispatches to a
> removal sentence, and work that must run on another host is FILED there with
> `gopi task submit`. The text below is the record of the mechanism as it was,
> kept verbatim because it is the evidence the removal is judged against.

**Task**: bug — `gopi task diff --peer <undeclared alias>` refuses with a raw ssh
error instead of naming the declared topology.
**Branch**: `fix/task-diff-peer-whitelist` (worktree `~/workspace/gopi-diff-whitelist`).

## The defect (measured, one invocation each)

```
$ gopi task diff --peer hp                  # "hp" not in sync_peers
gopi task sync: diff: ssh hp: exit status 2                 ← PRE-FIX
[exit=1]

$ gopi task sync --peer hp                  # same undeclared alias
gopi task sync: peer "hp": memsync sync: peer "hp" is not in the sync_peers
whitelist (settings.json "sync_peers" / GOPI_SYNC_PEERS) — sync topology is
declared, never discovered
[exit=1]
```

Every other peer-taking verb on `gopi task sync -h` enforced "topology is
declared, never discovered" BEFORE touching the transport. `task diff --peer`
did not: it ran the read-only catalog query over ssh and reported whatever ssh
said. The direction of the failure was correct (exit 1) but the message told the
operator nothing about the real condition, and an undeclared alias was silently
allowed to make an outbound connection attempt.

## What changed

1. **ONE gate, ONE wording** — new `internal/tools/peer_topology.go`:
   - `RequireDeclaredPeer(peer, cliPeers)` / `PeerDeclared(...)` — resolve the
     whitelist through its single reader (`config.ResolveSyncPeers`, Rule 2) and
     answer membership;
   - `RequireDeclaredAssetPeer(peer)` — the same gate on the knowledge
     channel's own key (`memory_peers`), whose refusal now shares the shape;
   - `DeclaredPeerGate` — the clause constant, referenced (never re-typed).
2. **The three drifted copies are gone.** The wording used to exist in three
   slightly different spellings: `syncPeerSession` (full message),
   `syncPeerDeclared` (shorter — named neither the settings key nor the env
   override), and `syncAssetsPeerWhitelisted`. All three now route through the
   one gate; `syncPeerSession` and `syncPeerDeclared` return the FULL message.
3. **`TaskDiffPeer` is gated before the transport**: alias syntax
   (`ValidateSSHPeerAlias` — a different rule, unchanged) then
   `RequireDeclaredPeer`, then the ssh leg.
4. **A transport seam for the first-refusal proof**:
   `SetReconRemoteRowSetsForTest` (the `reconRemotePendingSetHook` pattern),
   installed AFTER both guards so a refusal test can observe that the transport
   was never entered.
5. **Help and behaviour agree** — the `gopi task diff --peer <alias>` section of
   `syncUsageHead` (the sync surface's single help home) now states the rule,
   names the key and the env override, and says explicitly that a one-off diff
   of an undeclared alias is NOT a supported shape (the briefing's either/or,
   decided: gated, and documented as gated).

## Tests

- `internal/tools/peer_topology_test.go`
  - `TestTaskDiffPeerRefusesUndeclaredAliasBeforeTransport` — the refusal carries
    alias + settings key + env override + the clause, and the transport hook was
    NEVER entered; the message must not contain `ssh` (the pre-fix signature).
  - `TestTaskDiffPeerDeclaredAliasStillReachesTransport` — no over-refusal.
  - `TestRequireDeclaredPeerNamesTheDeclarationSources` — the gate's contract and
    the `--peers` precedence.
  - `TestDeclaredTopologyWordingHasOneDefinition` — Rule 13 literal gate: the
    refusal shape may appear only in `peer_topology.go`.
- `cmd/agent/cli_tasks_diff_whitelist_test.go` (REAL binary, Rule 31)
  - `TestTaskDiffPeerUndeclaredAliasRefusedByTheRealBinary` — exit ≠ 0, the
    whitelist sentence on the child's output, and the recording `ssh` shim was
    never entered.
  - `TestTaskDiffPeerDeclaredAliasStillRunsTheDiff` — a declared alias reaches
    the transport (the shim records the ssh leg naming the alias) and the diff
    reports `identical`.
  - `TestSyncUsageHeadStatesTheDeclaredPeerRuleForDiff` — the help states it.
- `internal/tools/memory_sync_reconcile_cov_test.go`:
  `TestTaskDiffPeerUnreachablePeerErrors` now DECLARES its alias (it is a
  transport test — otherwise the gate would answer first and the test would pass
  for the wrong reason).

## RED proof (revert only the fix)

`internal/tools/memory_sync_reconcile.go` was restored from a backup with the
two gate lines deleted (the new package and all tests untouched), and both the
unit and the binary test were run on that tree:

```
--- FAIL: TestTaskDiffPeerRefusesUndeclaredAliasBeforeTransport (0.31s)

--- FAIL: TestTaskDiffPeerUndeclaredAliasRefusedByTheRealBinary (1.22s)
    refusal must carry "sync_peers" ...
        gopi task sync: diff: ssh undeclared-peer: exit status 2
    the ssh leg ran for an undeclared alias — the gate must precede the transport:
        gopi task sync: diff: ssh undeclared-peer: exit status 2
    the failure is reported by the transport, not by the contract (the pre-fix defect):
        gopi task sync: diff: ssh undeclared-peer: exit status 2
```

Un-fixed, the child prints exactly the symptom the ticket reported. Restored →
green.

## Notes / decisions

- `TaskDiffPeer` is gated internally, so BOTH faces inherit it: the CLI
  (`runTaskDiff --peer`) and `LedgerAgreementReport` (which reaches the same
  read path through `peerRowSets`, and is only ever called with declared peers —
  `task diff` without `--peer`, which iterates the node's own declared topology).
  The retired per-dispatch announcement that used to be its second caller is
  gone (see `devdoc/ledger-agreement-precondition.md`); the gate's reach is
  narrower by exactly that one caller, and no other face was widened.
- The `task diff` (no `--peer`) branch is unchanged: it already iterates the
  node's OWN declared topology, which is the rule in its most direct form.
- Topology contract untouched: no limit was widened, no whitelist relaxed
  (Rule 31.2). The gate only refuses EARLIER and more legibly.
