<!-- doc-class: historical -->
# `task harvest --peer` is whitelist-gated — the declared-topology gate's last missing face

**Task**: bug — EPIC "two accepted-and-unread surface defects in `gopi task`".
**Branch**: `feat/task-diff-peer-gate-guard` (worktree `~/workspace/gopi-diff-peer-guard`).

## The epic's two defects, as found

### Defect 1 — `task sync --peer X --task-id T` accepts `--task-id` and reads it nowhere

**ALREADY FIXED on master** (`1097152a`), confirmed rather than re-done. The
helper `syncSessionFaceSpelling` (`cmd/agent/cli_tasks_sync.go:164`) and the
refusal `syncTaskIDSessionRefusal` (`cmd/agent/cli_tasks.go:1242`) landed with
the sibling `b09afc6f`. Real-binary repro on master:

```
$ ./bin/gopi task sync --peer declared-peer --task-id t-123
gopi task sync: task sync: --task-id applies to --push-only only — --peer
declared-peer is a SESSION … to push ONE row use
`gopi task sync --push-only --task-id t-123`
[exit=1, no session spawned]
```

Regression coverage already on master:
`cmd/agent/cli_tasks_sync_taskid_surface_test.go` (4 funcs: the 9-face refuse
matrix, the accept-half, the spelling resolver, the real-binary leg). **The
briefing's named test file `cli_tasks_sync_surface_test.go` does not exist on
master** — a stale pointer in the briefing, not a missing test.

### Defect 2 — `task diff --peer <undeclared alias>` was un-gated

**ALSO ALREADY FIXED on master** (`70a4b2b9`, "fix(task-diff): gate `task diff
--peer` on the declared sync topology before the transport"). Verified by
reading the landed commit and by exercising the current binary:

```
$ gopi task diff --peer not-declared              # sync_peers == ["a-peer"]
gopi task sync: diff: peer "not-declared" is not in the sync_peers whitelist
(settings.json "sync_peers" / GOPI_SYNC_PEERS) — sync topology is declared,
never discovered
[exit=1, ssh leg count 0]
```

Identical to `sync --peer not-declared` after its own phase prefix. The gate is
`RequireDeclaredPeer` (`internal/tools/peer_topology.go`), the single shared
definition the epic names.

### …which exposed the REAL remaining defect: `task harvest --peer` is not gated

Auditing every CLI face that takes a peer alias against the gate's callers
(`grep RequireDeclaredPeer` → only `sync` + `diff` + the knowledge channel's
asset gate) found ONE task-ledger peer face still outside the family:

| verb | peer face | gated? |
|---|---|---|
| `task sync`/`--peers`/`--full`/`--push-only` | `syncPeerDeclared` | ✅ `RequireDeclaredPeer` |
| `task diff` | `TaskDiffPeer` | ✅ `RequireDeclaredPeer` (70a4b2b9) |
| **`task harvest`** | **`TaskHarvest`** | ❌ **only `ValidateSSHPeerAlias` (syntax)** |
| `memory backup --ship` | `LedgerBackupShip` | ✅ `RequireDeclaredAssetPeer` (its own key) |

`task harvest` is the family's most expensive face — it **writes** the peer's
rows into this ledger in one transaction and then pushes every local branch
back — and it was the one face where a topology typo ran a real pull against a
host nobody declared, reported as ssh's exit code:

```
$ gopi task harvest --peer not-declared --no-push   # PRE-FIX
gopi task harvest error: harvest: ssh not-declared: exit status 2
[exit=1, ssh leg count 1]
```

The epic's defect 2 (message-shape inconsistency) therefore had a live
instance: the operator learned the rule on `sync`/`diff` and met ssh's exit
code on `harvest`.

## What changed

1. **`TaskHarvest` gates before the transport** (`internal/tools/memory_sync_task.go`):
   `RequireDeclaredPeer(peer, nil)` immediately after the required-`--peer`
   check, so the refusal precedes the ssh leg, the local DB open (harvest
   commits) and the git push-back. The comment records the measured pre-fix
   invocation.
2. **Help and behaviour agree** (`cmd/agent/cli_tasks_sync.go`): the
   `gopi task harvest` section of `syncUsageHead` (the sync surface's single
   help home) now states the rule, naming `sync_peers` / `GOPI_SYNC_PEERS` and
   the clause.
3. **No new wording, no new mechanism** — the fix is one call to the shared
   gate. Rule 14 holds: `peer_topology.go` still owns the sentence, and the
   existing `TestDeclaredTopologyWordingHasOneDefinition` (Rule 13 literal gate)
   keeps it that way.
4. **The pre-existing harvest fixtures now DECLARE their peer.** 18 transport
   tests pulled from `hp`/`testpeer`/… without ever declaring it; with the gate
   in place they would have been answered by the contract, i.e. testing the
   gate while their names promise the pull. `harvestTestDB` now declares
   `harvestFixturePeer`, and the git-pushback / notify-band / relabel fixtures
   got the same (naming their alias through a constant, Rule 1b). This is the
   identical adjustment `70a4b2b9` made for `TestTaskDiffPeerUnreachablePeerErrors`.

## Tests (RED before the fix, green after)

- `internal/tools/peer_topology_test.go`
  - `TestHarvestRefusesUndeclaredAliasBeforeTransport` — nil counts, the
    transport seam NEVER entered, no ledger created at the pinned path, and the
    message carries alias + settings key + env override + `DeclaredPeerGate`
    and does **not** contain `ssh` (the pre-fix signature).
  - `TestHarvestDeclaredAliasStillReachesTransport` — no over-refusal.
  - `TestHarvestCallSitesReferenceAPeerConstant` — Rule 1b/13 gate: no
    `TaskHarvest(… Peer: "<literal>" …)` call site may exist in this package's
    tests, so a fixture cannot drift from the topology it declares.
- `cmd/agent/cli_tasks_harvest_whitelist_test.go` (REAL binary, Rule 31)
  - `TestTaskHarvestUndeclaredAliasRefusedByTheRealBinary` — exit ≠ 0, the
    whitelist sentence on the child's output, and the argv-recording `ssh` shim
    was never entered (leg count 0).
  - `TestTaskHarvestDeclaredAliasStillRunsThePull` — a declared alias reaches
    the transport and the pull reports its result.
  - `TestUndeclaredPeerRefusalShapeIsTheSameOnEveryVerb` — **the shape
    requirement mechanized**: `sync` / `diff` / `harvest` must answer an
    undeclared alias with the byte-identical contract sentence (only the phase
    prefix differs).
  - `TestSyncUsageHeadStatesTheDeclaredPeerRuleForHarvest` — help == behaviour.

### RED proof (revert only the fix)

`internal/tools/memory_sync_task.go` was restored from its pre-fix content on
master (tests untouched) and both levels were run:

```
--- FAIL: TestHarvestRefusesUndeclaredAliasBeforeTransport (0.81s)
    an undeclared alias must be refused, got &{Harvested:0 …}

--- FAIL: TestTaskHarvestUndeclaredAliasRefusedByTheRealBinary (2.50s)
    refusal must carry "sync_peers" …
        gopi task harvest error: harvest: ssh undeclared-peer: exit status 2
    the ssh leg ran for an undeclared alias — the gate must precede the transport:
        gopi task harvest error: harvest: ssh undeclared-peer: exit status 2
    the failure is reported by the transport, not by the contract (the pre-fix defect):
        gopi task harvest error: harvest: ssh undeclared-peer: exit status 2

--- FAIL: TestUndeclaredPeerRefusalShapeIsTheSameOnEveryVerb (3.06s)
    [task harvest --peer undeclared-peer --no-push]: refusal names no alias in the shared shape:
        gopi task harvest error: harvest: ssh undeclared-peer: exit status 2
```

Un-fixed, the child prints exactly the reported symptom, with a recorded ssh
leg. Fix restored → all green.

## Not Done (honest)

- **The epic's defect 2 deliverable as literally written** (gate `task diff
  --peer`, or amend its help page) required **no code change**: it landed on
  master before this run. What this branch delivers instead is the *finding*
  that the same defect class had one more live instance (`task harvest`) plus
  the regression guard, and the shape-consistency test the epic asked for —
  which now pins all three verbs, not just `diff`.
- `gopi task push` (git channel) takes no `--peer`: its targets come from the
  declared topology, so there is nothing to gate (verified in
  `cmd/agent/cli_tasks_sync_git.go`).
- The `docs`/help page for `memory backup --peer` was left alone: its ship leg
  gates correctly on `memory_peers` (`RequireDeclaredAssetPeer`), a deliberate
  separate channel (`CONFIG.md` line 254), and it already names that key in the
  refusal.

## Decision recorded (the briefing's either/or)

`task diff --peer` (and now `task harvest --peer`) are **gated**, not
exempted: a one-off read-only probe of an undeclared alias is not a supported
shape on this surface. Rationale, in the operator's own terms: the whitelist is
what makes an alias a *peer* rather than a host, `harvest` writes and pushes as
well as reads, and one rule with three spellings is a rule nobody can rely on.
The help pages say so, in the same words the refusal uses.
