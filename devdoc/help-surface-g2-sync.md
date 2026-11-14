<!-- doc-class: normative -->

> **Retired mechanism (2026-11-05).** This page documents the help surface of the cross-node sync
> family (`task sync` / `sync-pass` / `diff` / `harvest` / `push` / `send` / `receive` / `pull`, and
> `gopi memsync`). **Every one of them is DELETED** — each spelling still dispatches to a removal
> sentence and exits 1, and `gopi task -h` carries the note that replaces them. What this page is
> still good for is the METHOD it applies (gate every advertised spelling against a real run of its
> page); its inventory is history.

# Help-Surface Inventory — the sync group (G2)

Task: G2 of help-surface hardening (the sync family). Scope: the group's
cross-node command words — `gopi task sync` (all eight faces), `gopi task
diff`, `gopi task harvest`, `gopi task push`, and `gopi memsync`. Ledger,
memory, log/stats/resume, chat/web and tools-from-shell verbs are other
groups' words and were deliberately NOT touched.

## The generating command

The inventory is **derived from the help text**, not maintained beside it:

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
./bin/gopi task sync -h        # the ONE page sync/diff/harvest/push share
./bin/gopi memsync -h          # the patch-FILE channel's page
```

`TestSyncSurfaceHelpIsTheMatrixSource`, `TestSyncSurfaceEveryHelpFlagIsInTheMatrix`,
`TestMemsyncHelpPrintsEveryChannel` re-run that derivation inside the suite:
a matrix row whose flag the help no longer prints fails, and a flag the help
prints with no row fails. The table below cannot rot away from the page.

## The inventory

| # | surface (as the help prints it) | test | has test | notes |
|---|---|---|---|---|
| 1 | `task sync --peer <alias> [--tags TAG] [--db PATH]` | `TestSyncPeerSessionDrivesThePeerHandler`, `TestSyncTagsPartitionTravelsInThePatch`, `TestSyncPeerSessionUndeclaredPeerIsRefusedByName`, `TestSyncStatusDBSelectsTheLedger` | ✅ new | argv + stdin asserted on the real transport |
| 2 | `task sync --peers a,b [--db PATH]` | `TestSyncPeersRunsEveryListedPeer`, `TestSyncPeersListOutsideTheWhitelistIsRefused` | ✅ new | one failure does not stop the list |
| 3 | `task sync --status [--db PATH]` | `TestSyncStatusOneLinePerDeclaredPeer`, `TestSyncStatusWithNoPeerIsASilentEmptyAnswer`, `TestSyncStatusDBSelectsTheLedger` | ✅ new | empty whitelist = empty answer, exit 0 |
| 4 | `task sync --serve-remote` (peer side, stdin = protocol) | `TestServeRemoteRefusesNonProtocolStdin`, `TestServeRemoteAnswersAnEnvelope` | ✅ new | driven AS the peer command, real ledger write |
| 5 | `task sync --claim-remote` (peer side, stdin = protocol) | `TestClaimRemoteRefusesNonProtocolStdin`, `TestClaimRemoteMirrorsOneRowAndAnswers` | ✅ new | same |
| 6 | `task sync --push-only [--task-id T] [--peer <alias> \| --peers a,b] [--db PATH]` | `TestPushOnlyReachesExactlyTheOperatorChosenPeer`, `TestPushOnlyInferredPeerListIsTheWholeWhitelist`, `TestPushOnlyPeerMustBeDeclared`, `TestPushOnlyWithNoPeerIsRefusedByName`, `TestSyncPushOnlyRefusesTags` + the pre-existing `cli_tasks_sync_pushonly_rowfilter_test.go` (row selection) | ✅ new (+1 FIX) | see Defect 1 — the sent-to-everyone bug |
| 7 | `task sync --peer <alias> --full [--db PATH]` | `TestSyncFullBypassesTheAnchorsOnTheWire`, `TestSyncFullComposesOnlyWithASession` | ✅ new | `full:true` asserted on the wire after an anchor exists |
| 8 | `task diff --peer <alias> [--db PATH]` | `TestTaskDiffIdenticalPairIsACleanZero`, `TestTaskDiffNamesTheBreakAndTheRepair`, `TestTaskDiffRequiresAValidPeer` | ✅ new | exit 0 / exit 1 both covered |
| 9 | `task harvest --peer ALIAS [--since UNIX] [--remote-db PATH] [--db PATH] [--no-push]` | `TestTaskHarvestPullAppliesRemoteRows`, `TestTaskHarvestRemoteDBReachesThePeerCommand`, `TestTaskHarvestSinceMustBeUnix`, `TestTaskHarvestRequiresPeer` | ✅ new | the pulled row is then read back from this ledger |
| 10 | `task push` | `TestTaskPushIsRoleResolvedAndTakesNoPositional` (+ pre-existing `TestParseTasksArgsPushAcceptsBare`, `TestRunTaskPushGitRejectsPositionals`, `TestServeGitSyncSweepThrottles`) | ✅ new | role-resolved client pass driven in a scratch checkout |
| 10b | `task send --task-id T [--peer <alias> \| --peers a,b] [--db PATH] [--remote-home DIR] [--cancel-source]` | `TestTaskSendCancelSourceMovesTheRowOnce`, `TestTaskSendRefusesANonPendingRow/{in_progress,cancelled}`, `TestTaskSendCancelSourceLeavesTheSourceWhenNoPeerPersisted`, `TestTaskSendWithoutCancelSourceLeavesTheSourceAlone`, `TestTaskSendCancelSourceIsAdvertisedAndAdmitted` (+ the leg's own `internal/tools` gate tests) | ✅ new | the row leg's STATUS GATE and its opt-in `--cancel-source`; the peer side is a REAL second ledger (the shim executes the `--row-remote` handler against the armed peer `GOPI_HOME`), so "the row landed" is proven by the peer's own `task show`, not by the sender's success line |
| 11 | flag admission for every flag the page prints on verbs 1–10 | `TestSyncSurfaceEveryAdvertisedFlagIsAdmitted` (19 rows) | ✅ new | the drift gate between help and `taskFlagMember` |
| 12 | `memsync --export --out F [--since] [--source] [--db]` / `--import F [--db]` | `TestMemsyncBrewAndProceduralChannelsRunEndToEnd`, `TestMemsyncUnreadablePatchIsRefusedByName`, `TestMemsyncExportRequiresOut` | ✅ new | |
| 13 | `memsync --export-tasks --out F [--status] [--tags] [--since] [--limit] [--db]` / `--import-tasks F [--db]` | `TestMemsyncTaskChannelRoundTripIsThePatchContract` | ✅ new | export from ledger A → import into ledger B → the row is found in B (proves `--db` is obeyed) |
| 14 | `memsync --export-proc --out F …` / `--import-proc F` | `TestMemsyncBrewAndProceduralChannelsRunEndToEnd` | ✅ new | |
| 15 | bare `gopi memsync` (no channel) | `TestMemsyncNoChannelIsRefusedByName` | ✅ new | refusal lists every channel |
| 16 | `memsync --dedup-scan` (retired spelling, pointer preserved) | `TestMemsyncDedupScanPointsToItsNewHome` | ✅ new | must fail WITH the pointer to `task dedup-scan` |

### Advertised but NOT covered here — with the reason

| surface | why not |
|---|---|
| A second physical host (a real `ssh` peer, a real `peer1`/`HP` pair) | both syncing nodes must exist; this rig has ONE host. The transport is the PATH-shadowed shim, which is an argv recorder, not a mocked peer: the peer-SIDE protocols are exercised as real commands (rows 4–5), so nothing about the peer's behaviour is faked — only the byte pipe is. |
| ~~`GOPI_SYNC_INTERVAL` / `GOPI_SYNC_GIT_INTERVAL` serve ticks~~ | **Both knobs and both tests this row named are GONE.** `GOPI_SYNC_INTERVAL` / `GOPI_SYNC_GIT_INTERVAL` were advertised by the catalog and `gopi --help-env` while no production path read them, and were removed 2026-11-05 with the cross-node sync surface; the two tests cited here (`TestServeGitSyncInterval`, `TestServeGitSyncSweepThrottles`) no longer exist anywhere in the tree — a normative row that asserted live coverage by a deleted test is the Rule 41a defect this page was about. The git-pass atom itself is still covered by row 10. |
| ~~The `--full` periodic backstop (`GOPI_SYNC_FULL_EVERY`)~~ | **Gone with the mechanism it paced.** `SyncFullReconcile`, the per-peer counter and the `MustSyncFullEvery` report face had no production caller left after the sync surface went, and were deleted 2026-11-05 together with this knob (`internal/tools/memory_sync_full_bound.go` went with them). Nothing here is left uncovered, because nothing here is left. |
| `task harvest`'s push-back half (without `--no-push`) | it pushes branches to the peer repo; on a single-host rig the "peer repo" is a path that does not exist, so the leg would assert the shim's answer rather than the behaviour. `--no-push` (the pull half, which is what the verb is for) is covered. |

## Contained fixes landed by this group

### Defect 1 — `task sync --push-only --peer X` pushed to EVERY declared peer (FIXED)

Measured (Rule 31, one invocation):

```bash
$ GOPI_SYNC_PEERS=first-peer,second-peer gopi task sync --push-only --task-id t-surface-push --peer first-peer
# before the fix: TWO ssh legs (first-peer AND second-peer)
# after  the fix: ONE leg, to first-peer
```

`--peer` and `--peers` were parsed, admitted by `taskFlagMember`, printed by
the help — and read by nobody on the push-only branch, which walked the whole
resolved whitelist. The fix resolves the peer list the way the sibling session
branches already do (`syncResolvePeers`, narrowed by `--peer`) and hands it to
a new `tools.SyncTaskPushOnlyPeers`, which enforces the declared-whitelist
membership contract (`sync topology is declared, never discovered`) for the
push face too. `SyncTaskPushOnly(taskID)` keeps its signature and behaviour
(whitelist default) for the serve tick.

Regression: `TestPushOnlyReachesExactlyTheOperatorChosenPeer` +
`TestPushOnlyPeerMustBeDeclared` — both RED with the fix stashed (verified,
see the report's Verification section).

### Defect 2 — the help page's push-only synopsis did not print `--peer`/`--peers` (FIXED)

The synopsis line said `[--peer <alias>]` while the surrounding prose already
described both flags. Corrected to `[--peer <alias> | --peers a,b]` — a help
statement made to match the (now enforced) behaviour, never the reverse.

## Behaviour observed while probing (findings, no code change)

1. **`task diff --peer <undeclared>` is not whitelist-gated** (unlike
   `sync --peer`): it runs the read-only catalog query over ssh and reports
   whatever ssh says (`ssh hp: exit status 2`). It writes nothing, so this is
   a message-quality gap, not a correctness one — a diff of an alias the
   topology does not declare is arguably a legitimate one-off. Left as
   observed; a task is filed rather than a guess landing here.
2. **`task sync --peer <alias> --task-id T` silently ignores `--task-id`**:
   `--task-id` is the push-only row selector by design (its one documented
   meaning), and a session branch simply never reads it. Same class as
   Defect 2 (accepted-and-unread) but with a documented reason for existing in
   the shared flag set; a usage refusal for the combination would be the fix —
   filed as a bug task, not done here because it is a surface-contract
   decision (refuse vs. ignore-for-session) rather than a contained patch.
