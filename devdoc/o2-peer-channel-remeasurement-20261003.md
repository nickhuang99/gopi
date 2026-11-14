<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Devdoc: the O2 peer-channel finding re-measured — every drifted citation re-keyed by symbol, one permit deleted

**Task:** 01a10200-3bda-759a-b6ab-346a208daee0 (branch `task/01a102003bda759ab6ac346a208daee0`)
**Tree measured:** master `fd126c45`, this run's worktree, host `peerbox`, 2026-10-03.
**Doc under measurement:** `devdoc/requirements/04-sync-and-nodes.md` — a **dated
record** (`**Measured:** 2026-09-17`, host of record `hostb-Laptop`).

## 1. What the briefing claimed, and what the content actually is

The briefing named one wrong citation. Reading the CONTENT of every line this
document cites (not just its existence) found **fifty-four drifted anchors** — 21 the
first two passes corrected, 33 more the whole-set sweep found — **one of which pointed
at the wrong FILE, and one sentence citing a constant that has never existed**, across
§0/C1–C4, §R1–R11, §6.1, §6.3/§6.4, §7.1–§7.3 and §6.2's risk table. Every row below
is a `sed -n '<N>p' <file>` reading taken on `fd126c45`; the third pass added the
bottom block, found by sweeping the WHOLE citation set (a script that resolves each
`.go:NNN` token, prints the line and flags ambiguous basenames / past-EOF / comment- or
brace-shaped targets).

| cited as | line's actual content on `fd126c45` | true symbol + line |
|---|---|---|
| `internal/config/config.go:311` ("merges all three") | `// `ignore_peak_hours` keys keep: a settings.json written while the tick existed` | `ResolveSyncPeers` `internal/config/config.go:763` |
| `internal/config/config.go:612` ("merges all three") | `// (GOPI_TASK_NOTIFY_EVENTS); "all" = every status plus every synthetic` | `ResolveMemoryPeers` `internal/config/config.go:870` |
| `internal/config/config.go:362` (§6.1 S6, "two whitelist keys") | `// TaskSync is the TASK-ROW SYNCHRONIZATION switch (settings key` | `SyncPeersSettingsKey` `internal/config/config.go:750` |
| `internal/config/config.go:404` (§6.1 S6, "two whitelist keys") | `// "chat_ideal_geometry"; env GOPI_CHAT_IDEAL_GEOMETRY; CLI` | `MemoryPeersSettingsKey` `internal/config/config.go:807` |
| `internal/config/config.go:584` (R11, "the predicate is `HasSyncPeers()`") | `// ArchiveBuryOverrideFor returns the explicit archive-bury permission recorded` | `HasSyncPeers` `internal/config/config.go:787` |
| `internal/tools/memory_sync_assets.go:188` (R6, "the gate is its own key") | `PushedRows int` | `syncAssetsPeerWhitelisted` `internal/tools/memory_sync_assets.go:524` |
| `internal/tools/memory_sync_delta.go:186` (R1/S1, "export: rows newer than the anchor") | `// (one SELECT, no policy).` | `SyncTaskExportDelta` `internal/tools/memory_sync_delta.go:187` |
| `internal/tools/memory_sync_delta.go:429` (R1/S1, "import: one transactional apply") | `// SyncTaskImportDelta applies a v2 patch through the merge. patchPath is` | `SyncTaskImportDelta` `internal/tools/memory_sync_delta.go:431` |
| `internal/tools/tasks.go:866`/`:878` (R1, "the only reader and the only writer") | `}` / `if !strings.Contains(ddl, "'"+s+"'") {` — an unrelated table-rebuild predicate | `syncStateGet` `internal/tools/tasks.go:1068` / `syncStatePut` `:1082` |
| `internal/tools/memory_sync_delta.go:586-600` (R3/S7, the bug053 fix) | a comment block, not the function | `syncDeltaMergeRow` `internal/tools/memory_sync_delta.go:540` (blind-ruling block `:584-604`) |
| `internal/tools/memory_sync_delta.go:697` (R3) | `Tags: syncConflictTag,` in a payload literal | `notifyLegacyConflict` `internal/tools/memory_sync_delta.go:688` |
| `internal/tools/tasknotify.go:81-86` (R3/S7, "may never be auto-completed") | the `notify.events` default + the synonym constant's comment | `notifyLedgerConflictHumanTask` `internal/tools/memory_sync_delta.go:763` + `taskNotifyEventLedgerConflictHumanTask` `internal/tools/tasknotify.go:103` |
| `internal/tools/memory_sync_delta.go:796-809` (S5, the fail-safe default) | the `syncDeltaServerResolve` SQL/text — the wrong function | `IsServerNode` `internal/tools/memory_sync_delta.go:815`, fail-safe `return` `:838` |
| `internal/tools/memory_sync_reconcile.go:505` (§R7/R11/§6.1 S3/§7.3 — cited 4×) | `// session: returns true when the NEXT session must be full (count+1 >= N,` | `syncAnchorGuard` `internal/tools/memory_sync_reconcile.go:361`, wrapper `guardSyncStateCommit` `:542` |
| `memory_sync_session.go:75`/`:88-90`, `memory_sync_assets.go:195-196`, `node_role.go:18-19` (R8, the refusal doctrine) | struct-field comments / a `const (` opener | the doctrine's doc comment `internal/config/node_role.go:12-14`; the gate `syncPeerDeclared` `internal/tools/memory_sync_reconcile.go:332` |
| `memory_sync_reconcile.go:302` (R8, "consulted on every session path") | `staged.Close() //stylegate:ignore …` | `syncPeerSession` `internal/tools/memory_sync_session.go:72` |
| `task_sync_git.go:257`/`:292` (R8, "two server-side paths") | `}` / `return flush.Pushed, flush.Failed` | `GitSyncRepoRoot` `internal/tools/task_sync_git.go:253` / `GitSyncPushLocalBranches` `:275` |
| `task_finish_cleanup.go:1046` (R8) | `finishCleanupOwnerMismatchMarker,` | `finishCleanupServerTarget` `internal/tools/task_finish_cleanup.go:2333` |
| `tasknotify.go:76`/`:86` (R11, "alarms that must be reachable") | the default-events comment / the terminal-push constant's comment | `taskNotifyAlarmEvents` `internal/tools/tasknotify.go:161` (+ constants `:87`/`:93`/`:103`) |
| role gate at `task_finish_premerge.go:820` | past EOF (the file is **636** lines today) | `IsServerNode() \|\| finishCleanupServerTarget() == ""` `internal/tools/task_finish_cleanup.go:1232` |
| trunk exemption "`:818` region" | — | `finishCleanupExemptBranchesResolved` `internal/tools/task_finish_cleanup.go:583` (trunk set `:584`) |
| **third pass — whole-set sweep** | | |
| `internal/agent/loop.go:875` (§0 C1, "ZERO-DEP default") | `// job(action=output). Framework-level, not user-configurable.` | `internal/agent/loop.go:895` |
| `memory_sync_session.go:55`/`:73` (C1, the ledger transport) | `// local delta patch on stdin; the remote merges…` / a comment | `SyncPeerSession` `internal/tools/memory_sync_session.go:57`, `syncSSHExchange` `:173` |
| `memory_sync_assets.go:118` (C1, "no conflict resolution at all") | `// bounded and small — one pause per batch boundary…` (a SQLITE_BUSY comment) | `MemoryAssetsImportPatch` `internal/tools/memory_sync_assets.go:755`, `syncAssetsImportRows` `:898` |
| `task_finish_cleanup.go:2020-2080` (R4, the chain's ordering) | `finishCleanupMainRepoReason`'s body — the MAIN-CHECKOUT gate, not the chain | the chain's own doc comment `internal/tools/task_finish_cleanup.go:3-30`; the client/server branch it describes at `:1173-1180` |
| `node_role.go:100`, `cli_tasks.go:1573`, `main.go:715` (R5, identity delivery) | `NodeIdentityBriefing` is right; the other two are unrelated comment lines | `NodeIdentityBriefing` `internal/config/node_role.go:100` wired at `cmd/agent/cli_tasks.go:2303`; `NodeIdentityLine` `:80` printed at `cmd/agent/main.go:1198` |
| `memory_sync_delta.go:804`, `task_sync_git.go:307` (R8/G4, "the self-check exists in three places" ×2) | `bus.observeIDs(…)` / `// (no peers = no server to sync with).` | `IsServerNode`'s peer loop `internal/tools/memory_sync_delta.go:831-836`; `GitSyncServerTarget`'s own-name skip `internal/tools/task_sync_git.go:455-458` |
| `memory_sync_delta.go:796-800` (R9, the trade-off comment) | the `syncDeltaServerResolve` SQL/text | `IsServerNode` `internal/tools/memory_sync_delta.go:815`, trade stated at `:824` |
| `settings.go:196-203` (R10, "the knobs are enumerated in one place") | `// with its settings.json key, CLI flag and a one-line help string…` | the `EnvVars` catalog `internal/config/settings.go:201`, topology/pacing rows `:351-359` |
| `memory_sync_delta.go:787-791` (R11, "the same first arm") | `// tags += sync-resolved (union with BOTH sides' own tags)…` | `IsServerNode` `:815`, declared-role arm `:817-821` |
| `memory_sync_delta.go:734-737` (G2, the alarm) | `syncRowIsMirrorNoise`'s tail (`:734` `return true`, `:737` `}`) | `notifyLedgerConflictHumanTask` `internal/tools/memory_sync_delta.go:763` |
| `tasknotify.go:63`/`:86` (G2, the event sets) | `// with — EXACTLY one, whatever the caller handed us` / `// Operators subscribe via events (like "stale").` | `taskNotifyEventsDefault` `internal/tools/tasknotify.go:77`; **`:86` cites a constant that does not exist** — the six-event list is the operator's subscribed `notify.events` value (read on `peerbox` 2026-10-03; also `CONFIG.md:534`, `internal/config/settings.go:351-359`'s sibling rows), and the sentence was corrected rather than re-keyed |
| `:735` (G2, the stderr fallback) | a `tasknotify.go` line about the terminal-push alarm | `memsyncWarnf` `internal/tools/tasknotify.go:606`, fired at `internal/tools/memory_sync_delta.go:764` |
| `tasknotify.go:120-126` (G2, "the same class as the 2026-09-02 incident") | the wallclock alarm's constants | the incident comment `internal/tools/tasknotify.go:261-262` |
| `cli_tasks.go:4788` (G3/G6, "one call site" ×2) | `// filesystem's coincidentally-equal inode would be reported as…` (the serve-lock table) | the dispatch `tools.TaskDiffPeer` `cmd/agent/cli_tasks_sync.go:547`, symbol `internal/tools/memory_sync_reconcile.go:92` |
| `memory_sync_task.go:1399`, `task_sync_git.go:47`, `task_finish_cleanup.go:55` (G7, three constants for one path) | three unrelated comment/data lines | `harvestPeerRepo` `internal/tools/memory_sync_task.go:1692`, `gitSyncPeerRepo` `internal/tools/task_sync_git.go:48`, `finishCleanupPeerRepo` `internal/tools/task_finish_cleanup.go:75` |
| `memory_sync_assets.go:186` (G10 and again in §6.4) | a `PushedRows` field comment | `syncAssetsPeerWhitelisted` `internal/tools/memory_sync_assets.go:524` over `RequireDeclaredAssetPeer` `internal/tools/peer_topology.go:107` |
| `memory_sync_claim.go:723` (G11, the claim broadcast) | `}` (a branch close of the revert path) | `RunClaimLockProtocol` `internal/tools/memory_sync_claim.go:855`, `StartClaimLockBroadcast` `:220`, `syncClaimRemoteAcquire` `:604` |
| `cli_tasks_sync.go:182`, `memory_sync_assets.go:190` (N1, "the code loops over it") | help text / a doc-comment line | `normalizeSyncPeers` `internal/config/config.go:794`; `MemoryAssetsPeerSessionMode` `internal/tools/memory_sync_assets.go:328`; the `--peer` rule at `cmd/agent/cli_tasks_sync.go:175-186` |
| `task_finish_cleanup.go:1046` (N1, "takes `peers[0]`"), `task_sync_git.go:291` (N1) | `finishCleanupOwnerMismatchMarker,` / `return flush.Pushed, flush.Failed` | `finishCleanupServerTarget` `internal/tools/task_finish_cleanup.go:2333` (`peers[0]` at `:2340`); `GitSyncServerTarget` `internal/tools/task_sync_git.go:440` |
| `memory_sync_session.go:190-205` (C1 row) | a comment inside the ssh exec | `syncSSHExchange` `internal/tools/memory_sync_session.go:173`, exec at `:183-195` |
| `memory_sync_delta.go:809` (G1), `node_role.go:99-104` (G1) | the `IsServerNode` doc comment / an unrelated comment | `IsServerNode` `:815` (fail-safe `:838`); `NodeIdentityLine` `internal/config/node_role.go:80` / `NodeIdentityBriefing` `:100` |
| `cmd/agent/cli_tasks_sync.go:33`/`:315` (R4 steps, the daemon's pacing) | the push-only flag's comment / `syncPushOnlyArgv` | `syncIntervalEnv` `cmd/agent/cli_tasks_sync.go:99`, reader `syncIntervalFromEnv` `:667`, tick `runTaskSyncTick` `:697` |
| `cli_tasks_sync.go:78-86` (G6/§R11) | an optional-value-flag comment | `syncErrorPrefix`'s own comment `cmd/agent/cli_tasks_sync.go:80-92` |
| `memory_sync_task.go:1422` (R4 step ③) | `if n, err := res.RowsAffected(); err != nil \|\| n == 0 {` | `harvestPeerRepo` `:1692` |
| `memory_sync_delta.go:1089` (R4 step ④, "epic ⑥") | `}` (inside `syncEnvAnchor`) | `syncStateCommit` `internal/tools/memory_sync_delta.go:1118` |
| `cli_tasks.go:1551-1552` (R4 risk table + §7.3) | (right file, and it is the `task diff --peer` refusal — kept, path spelled) | unchanged: `cmd/agent/cli_tasks.go:1551-1552` |
| `node_role.go:81` (R4, the startup banner) | `role, syncTo := ResolveNodeRole()` inside `NodeIdentityLine` | `NodeIdentityLine` `internal/config/node_role.go:80` |
| `task_finish_cleanup.go:584` (R4, the trunk exemption) | `exempt := map[string]bool{"master": true, "main": true}` | `finishCleanupExemptBranchesResolved` `:583` |
| `task_sync_git.go:212-213` (G12) | `parts = append(parts, fmt.Sprintf("dispatched=[%s]"…` | `internal/tools/task_sync_git.go:213` |
| `path_policy.go:210` (§0 C4, "the blast-radius guard") | `// names a target outside every declared root ("" = the command is permitted).` | `rejectOutsideDeclaredRoots` `internal/tools/path_policy.go:215` |
| `memory_sync_assets.go:180` (§G4, "no identity check") | `// MemoryAssetSyncStats reports one bidirectional asset session.` | `MemoryAssetSyncStats` `internal/tools/memory_sync_assets.go:181` |
| §7.1–§7.3 test rows: `memory_sync_delta_test.go:1027`/`:416`/`:525`/`:576`, `memory_sync_bidir2_e2e_test.go:158`, `singlenode_serve_test.go:52`, `peer_alias_guard_test.go:64` | Fatalf bodies, braces, `ids := map[string]bool{}` and a comment | the NAMED test functions: `TestSyncDeltaServerRulingBlocksHumanTicket` `:1100`, `TestSyncDeltaEqualAnchorEcho` `:431` (+ its client counterpart described at `:243`), `TestSyncDeltaTerminalNeverRegresses` `:540`, `TestSyncStateForwardOnlyAnchors` `:591`, `TestBidir2LiveConflictNeverClobbered` `:160`, `TestServeGitSyncSweepWithPeerAdvancesClock` `:55`, `TestDispatchRefusesOptionShapedNode` `:99` |
| §6.1 S2/S9 rows' anchors (the third pass read them too) | the chain citation and the claim-lock citation | `internal/tools/task_finish_cleanup.go:3-30`; `RunClaimLockProtocol` `internal/tools/memory_sync_claim.go:855` |

The last two are the sharpest: the role gate is **not in `task_finish_premerge.go`
at all**, so re-keying the anchor by arithmetic would have written a line number
into a file that never carried the symbol. The permit's own reason ("no symbol in
the row resolves") was true — the FIX was to name the symbol in the row's text, in
the right file, which is what this change does.

## 2. The O2 finding, re-measured per channel

O2's claim was "three ways to configure one fact". It is **correct**, and the merge
it named is one function per channel. The three sources, in the precedence order
`ResolveSyncPeers` encodes (`internal/config/config.go:763-777`, arms at `:764-766`
CLI, `:767-770` env, `:771-776` settings):

1. **CLI flag** (`cliPeers`, `internal/config/config.go:764-766`) — a one-off
   session's own topology; the only arm that widens the topology for one run
   without editing a file. The CLI funnels its flag through `syncResolvePeers`
   (`cmd/agent/cli_tasks_sync.go:641`, the split value passed at `:650`; the bare
   `--peers` spelling arrives as `""` and resolves to the declared list on purpose).
   The 2026-09-17 text cited `cli_tasks_sync.go:181`/`:252` for this — `:181` is
   help text ("omitted: the check then runs against every peer THIS node declares
   in") and `:252` is a doc-comment line, so both are corrected here.
2. **env `GOPI_SYNC_PEERS`** (`SyncPeersEnv`, `internal/config/config.go:755`; read
   at `:767`) — the test seam and the per-shell re-point; read here and nowhere else
   (Rule 2's single reader).
3. **settings.json `sync_peers`** (`SyncPeersSettingsKey`,
   `internal/config/config.go:750`; read at `:771-776`) — the declared topology the
   daemon, the worker and the merger read.

**The new half of the finding** (not in the 2026-09-17 text): the asset channel is
**asymmetric**. `ResolveMemoryPeers` (`internal/config/config.go:870`) takes **no
`cliPeers` parameter** — its merge has two arms (env `GOPI_MEMORY_PEERS` at `:871`,
settings `memory_peers` at `:875-878`; the env arms' guards are byte-identical between
the two resolvers, `:767` vs `:871`, so this is an ABSENT ARM and not a differently
written one), so the knowledge channel exposes two of the
three sources while the *refusal vocabulary* is shared
(`RequireDeclaredPeer` `internal/tools/peer_topology.go:91` /
`RequireDeclaredAssetPeer` `:107`, both over one `undeclaredPeerError`). The
operator's error message therefore names three facts on either channel, but only the
task channel lets a flag supply the third for one run.

## 3. Completed

- O2 rewritten: each channel justified individually with its arm's current line;
  the merge cited as a SYMBOL (`ResolveSyncPeers` + `ResolveMemoryPeers`) with
  current lines; the 2026-09-17 wrong citations named in the text as retracted.
- **Third pass — the WHOLE citation set swept mechanically**: a script resolved every
  `.go:NNN` token in the document, printed the cited line's content, and flagged
  ambiguous basenames, past-EOF targets and comment-/brace-shaped targets; each hit
  was then read by hand and re-keyed to its symbol (the third-pass block of §1's
  table, and the two claims corrected rather than re-keyed: the `tasknotify.go:86`
  constant that does not exist, and the `ResolveMemoryPeers` comment that misread
  identical guards as divergent ones).
- **Second pass over the WHOLE citation set** (the epic asked for the §6.1 rows and
  the O2-adjacent text in the same pass; reading them by content found more than the
  ones the briefing named). Every drifted anchor below was re-keyed to its SYMBOL:
  - §R1: `memory_sync_delta.go:186`/`:429` (comment lines above the functions) →
    `SyncTaskExportDelta` `:187` / `SyncTaskImportDelta` `:431`; `tasks.go:866`/`:878`
    (inside an unrelated table-rebuild predicate, NOT `sync_state`) → `syncStateGet`
    `internal/tools/tasks.go:1068` / `syncStatePut` `:1082`.
  - §R3: `memory_sync_delta.go:586-600` → `syncDeltaMergeRow`
    `internal/tools/memory_sync_delta.go:540` (blind-ruling block `:584-604`);
    `:697` → `notifyLegacyConflict` `:688`; `tasknotify.go:81-86` (the `notify.events`
    default + synonym comments) → `notifyLedgerConflictHumanTask`
    `internal/tools/memory_sync_delta.go:763` + `taskNotifyEventLedgerConflictHumanTask`
    `internal/tools/tasknotify.go:103`.
  - §R6: `memory_sync_assets.go:188` → `syncAssetsPeerWhitelisted` `:524`;
    `config.go:362`/`:404` → the key constants `:750`/`:807`.
  - §R8: `memory_sync_session.go:88-90` and `memory_sync_assets.go:195-196`
    (struct-field comments) → the doctrine's own doc comment
    `internal/config/node_role.go:12-14`; `memory_sync_session.go:75` →
    `syncPeerSession` `:72` + `syncPeerDeclared`
    `internal/tools/memory_sync_reconcile.go:332`; `memory_sync_reconcile.go:302`
    (a `staged.Close()` line) → `syncPeerDeclared` `:332`; `task_sync_git.go:257`/`:292`
    (a `}` and a `return`) → `GitSyncRepoRoot` `:253` / `GitSyncPushLocalBranches`
    `:275`; `task_finish_cleanup.go:1046` (a skip-marker) → `finishCleanupServerTarget`
    `:2333`.
  - §R11: `memory_sync_reconcile.go:505` (a `syncTickShouldRunFull` comment) →
    `syncAnchorGuard` `internal/tools/memory_sync_reconcile.go:361` /
    `guardSyncStateCommit` `:542`; `tasknotify.go:76`/`:86` → `taskNotifyAlarmEvents`
    `internal/tools/tasknotify.go:161` + the alarm constants `:87`/`:93`/`:103`.
  - §6.1 table: **S1/S3/S5/S7** rows carried the same drifted anchors and were fixed
    with them (`memory_sync_delta.go:186`/`:429`; `memory_sync_reconcile.go:505`;
    `memory_sync_delta.go:796-809` → `IsServerNode` `internal/tools/memory_sync_delta.go:815`
    with the fail-safe `return` at `:838`; `memory_sync_delta.go:586-600` +
    `tasknotify.go:81-86` → `syncRowIsMirrorNoise` `:728` + the alarm pair above).
  - §7.3: `memory_sync_reconcile.go:505` → `guardSyncStateCommit` `:542` (the SAME
    single-line-comment drift, twice more).
  - §R11's `config.go:584` → `HasSyncPeers` `internal/config/config.go:787`.
  - §R4's role-gate and trunk-exemption references corrected, including the wrong FILE
    (the role gate is in `task_finish_cleanup.go`, not `task_finish_premerge.go`).
- Header: the dated `**Measured:** 2026-09-17` record is KEPT (it is the record) and
  a re-measurement line is added beside it (`peerbox`, `fd126c45`, 2026-10-03).
- Permit `devdoc/requirements/04-sync-and-nodes.md eof (bare)
  task_finish_premerge.go:820` **DELETED** — re-measuring the row made it fire
  again, so its site no longer drifts; the file's anchor set is now green with no
  permit. 231 → 230 permits. **No new permit, no re-baseline, no gate edit.**
- Report: this file.

## 4. Not Done (honest)

- **`§6.1`'s S2/S4/S8/S9 rows.** S2/S4/S8/S9 were re-read in the third pass (the
  whole-citation sweep, §6's second bullet): each row's own `.go` anchors were checked
  by content and re-keyed where they had drifted (S2's chain citation, S9's claim-lock
  citation).
  What remains untouched in §6.1 is the *live* half of the rows — the field
  observations taken on the client on 2026-09-17.
- **The doc's live field measurements were NOT re-run.** The client-side observations
  (claim counts, anchor values, `task sync --status` output, the `hp`/`peerbox` refusal
  matrix in R6) are host-state readings taken on `hostb-Laptop` on 2026-09-17; this
  run has no client to observe (it measured on `peerbox`). They remain as the dated
  record says, and the header marks which parts are the re-measurement.
- **`ResolveMemoryPeers`' missing CLI arm is reported, not changed.** Whether the
  asset channel *should* get a per-run flag is a design decision for the operator,
  not a doc-repair.

## 5. Verification

- **Build:** `CGO_ENABLED=0 go build ./...` — exit 0, no output.
- **Tests:** `CGO_ENABLED=0 go test ./internal/agent/ -run 'TestDocAnchor' -count=1`
  — `ok github.com/nick/gopi/internal/agent`;
  `doc anchors verified: 2912 across 620 normative docs (2418 explicit + 494 bare
  continuations; 588 bare out of row/table/heading scope; **230 pre-existing drift
  entries permitted, 0 new, 0 stale permits**)` (the earlier passes of this run
  printed 2901/2907/2910 — the count rises with each re-keyed citation). Before this change the same command
  printed `231 ... permitted, 0 new, 0 stale` (the permit was still excusing a row
  already fixed upstream); the intermediate state after the first round of doc edits
  but before the permit deletion printed `4 new, 1 stale` — the 4 new were the
  retraction prose itself leaving bare `config.go:NNN` anchors (AMBIGUOUS across 5
  `config.go` files), fixed by spelling the full path. **This run's second pass
  (the §6.1-table sweep) went red ONCE on two of its own prose anchors** (`config.go:362`/`:404`
  left as bare basenames in the new §6.1 note — AMBIGUOUS across 5 `config.go` files);
  spelling the full path fixed it and the next invocation printed `0 new, 0 stale`.
  The lesson repeats the first pass's: the gate re-checks the correction prose itself.
- **Ran:**
  - `gopi doccheck` → `doccheck: the devdoc corpus is consistent — every file under
    devdoc/ has a well-formed row`, exit 0.
  - `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and
    devdoc/DOC-INDEX.tsv`, exit 0.
  - `CGO_ENABLED=0 go build -o <session>/gopi ./cmd/agent/` → `BUILD OK`.
  - `CGO_ENABLED=0 go build ./...` → exit 0 (third pass).
  - `CGO_ENABLED=0 go test ./internal/agent/ -run 'TestDocAnchor' -count=1 -v` (the
    gate, verbatim above): the final invocation printed `2912 across 620 normative
    docs (2418 explicit + 494 bare continuations; 588 bare out of row/table/heading
    scope; 230 pre-existing drift entries permitted, 0 new, 0 stale permits)` +
    `--- PASS: TestDocAnchorsResolveInNormativeDocs`.
  - the two scripts this run wrote and ran (kept in the session directory):
    `check_cites.sh` (resolve each `.go:NNN`, print the line's content, flag
    UNRESOLVED/PAST-EOF) and `flag_cites.sh` (flag comment-/brace-/blank-shaped
    targets). Their outputs are the measurement behind §1's table.
- **Result:** the manifest diff was verified after classification — `git diff
  devdoc/DOC-INDEX.tsv` adds exactly ONE line (this report) and removes/changes
  nothing else; `git diff internal/agent/testdata/docanchor-baseline.tsv` is exactly
  one deletion (`1 file changed, 1 deletion(-)`), the stale row named in §3.
  `git status --porcelain` in the worktree prints nothing after the commit.
- **Commit:** `8f483962` — `fix(docs): re-measure the O2 peer-channel finding and
  re-key its drifted anchors by symbol` (2 files: the doc + the baseline);
  `6c340399` — `docs(devdoc): the O2 peer-channel re-measurement report with its gate
  evidence`; and the third pass's own commit (`docs(devdoc): sweep the whole citation
  set of 04-sync-and-nodes …`, `6bef2da9`, 2 files). All three were taken on base
  `fd126c45` (`git merge-base --all master HEAD` → `fd126c45` at that point, i.e. the
  measurements above are readings on `fd126c45`). **Delivery point (Rule 30a — the
  alignment is the LAST step, done after the work and after these measurements):** the
  branch was then merged with `master` at `e9a5cb2c` (`git merge --no-edit master` →
  clean; the six master commits touch a disjoint file set — none of the files this
  document cites, including `internal/config/config.go`,
  `internal/tools/memory_sync_assets.go` and `internal/tools/task_finish_cleanup.go`),
  so the branch was handed over **descending from the integration branch**: the
  delivery-point merge commit `d0fe173d` (parents `6bef2da9` + master `e9a5cb2c`) is
  the shape the merge queue receives, `git log --oneline --merges -1` names it, and
  `git merge-base --is-ancestor master HEAD` exits 0. The gates were re-run on the
  post-merge tree and report the same numbers as before the merge: `TestDocAnchor` →
  `2912 across 620 normative docs (2418 explicit + 494 bare continuations; 588 bare out
  of row/table/heading scope; 230 pre-existing drift entries permitted, 0 new, 0 stale
  permits)` + `--- PASS`.

## 6. Evidence (self-filed)

- Re-measured this run, on the centre: `grep -c 'ledger-conflict'
  /home/user/.gopi/notify/task-events.jsonl` → **0** on a 52,525,993-byte log
  (`2026-10-03`), independently reproducing §G2's "the alarm has never been
  delivered" on the machine that HOLDS the log (the doc's own reading was the
  client's). The subscribed value behind that check was read from the live profile:
  `notify.events = claimed,in_progress,completed,failed,stale,build-failed`.
- The env-arm behaviour §O2's note records was read from the source, not inferred:
  `sed -n '767p;871p' internal/config/config.go` → two byte-identical
  `if raw := strings.TrimSpace(os.Getenv(...)); raw != "" {` guards.
- The whole-set sweep's own output (third pass): `check_cites.sh
  devdoc/requirements/04-sync-and-nodes.md .` printed 134 resolved tokens with **no
  UNRESOLVED / PAST-EOF** entry (`cite-content-final3.txt`), and `flag_cites.sh`
  printed 5 comment-shaped targets — each one a line the document cites *as* a
  comment on purpose (the bug041 quote, the chain's own header, the notify incident,
  the asset-decoupling reason, the refusal-doctrine doc comment). A citation that was
  a comment WITHOUT the sentence saying so is exactly the drift class the sweep was
  written to find; the `tasknotify.go:86` row above is the one it caught in prose.
- Every pair written by this run's three rewrite scripts was applied only after the
  script refused a non-unique match (`REFUSED (n)`), so no replacement landed twice
  and no anchor was re-keyed by arithmetic: `apply_cites.py` (34), `apply_cites2.py`
  (8), `apply_report.py` (3). The scripts and their raw outputs live in this run's
  session directory (`/home/user/.gopi/sessions/01a10200-3bda-759a-b6ab-346a208daee0/`),
  outside the branch (Rule 30/37).
- The bare basenames this report quotes (`config.go:362`, `config.go:584`,
  `main.go:715`, `task_finish_premerge.go:820`, …) are **verbatim quotations of the
  retracted citations** in the "cited as" column — the finding is precisely that they
  were ambiguous or past EOF. They are not anchors this report relies on; the
  replacements beside them carry full paths.
- Gate red→green, observed in this run (first pass): the intermediate invocation
  printed `231 pre-existing drift entries permitted, 4 new, 1 stale permits`
  followed by `FAIL`; the post-fix invocation printed `230 pre-existing drift entries
  permitted, 0 new, 0 stale permits` followed by `ok` — the same command, same tree.
- Gate on the §6.1 second pass, observed in this run:
  `go test ./internal/agent/ -run TestDocAnchor -count=1` → `ok
  github.com/nick/gopi/internal/agent 2.352s`, and the `-v` run printed
  `2901 across 620 normative docs … 230 pre-existing drift entries permitted, 0 new,
  0 stale permits` + `--- PASS: TestDocAnchorsResolveInNormativeDocs (1.19s)`; the
  red intermediate of the same pass printed `2901 … 230 permitted, 2 new, 0 stale`
  with the two AMBIGUOUS `config.go` basenames named, then went green after the fix.
- Stale permit deletion observed: `git diff internal/agent/testdata/docanchor-baseline.tsv`
  → `1 file changed, 1 deletion(-)`, and `grep -c 04-sync-and-nodes` on that file →
  0.
- Every citation in §1's table was read by `sed -n '<N>p'` on `fd126c45`; the
  content strings quoted are those readings. The second pass swept the doc's whole
  `.go:NNN` citation set with a script that resolves each token's file, prints the
  line's content and flags UNRESOLVED (ambiguous basename) / PAST EOF entries — the
  raw output is the measurement behind §3.
- Host and date: `date -Iseconds` → `2026-10-03T22:04:45+08:00`; `git rev-parse
  --short HEAD` → `fd126c45` at measurement time (the commits above are later hashes
  on the same branch); `hostname` → `peerbox`.
