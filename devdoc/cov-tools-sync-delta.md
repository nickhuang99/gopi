<!-- doc-class: historical -->
# Task Report: function-level coverage for the peer-sync delta faces (internal/tools)

Epic: `feat/cov-tools-sync`. This report covers the **delta** file's 5
functions.

New test file: `internal/tools/memory_sync_delta_cov_test.go`.

## Completed

| Function | Definition (code_search) | Tests added | Boundary / error cases named |
|---|---|---|---|
| `WorkerTaskTag` | internal/tools/memory_sync_delta.go:279 | `TestWorkerTaskTagHostPrefixAndStability` | prefix appears exactly once; equals `worker:<hostname>`; stable across calls |
| `TagWorkerClaim` | internal/tools/memory_sync_delta.go:291 | `TestTagWorkerClaimUnionIsIdempotent` | `""` and whitespace-only → exactly the tag (no leading comma); existing tags preserved + tag appended; re-tag → byte-identical; tag embedded mid-list → detected |
| `TaskRunWritebackTagged` | internal/tools/memory_sync_delta.go:305 | `TestTaskRunWritebackTaggedStampsWorkerProvenance`, `TestTaskRunWritebackTaggedErrorPath` | completed + worker tag + pre-existing tag kept, no leading separator; ghost task id → error, nothing written |
| `TaskRunWritebackTags` | internal/tools/memory_sync_delta.go:318 | `TestTaskRunWritebackTagsExplicitAndEmpty`, `TestTaskRunWritebackTagsRefusesIllegalStatusBeforeTagWrite` | explicit tags land with status; `""` tags → column untouched (before/after compared); illegal status (`in_progress`) → refused AND the tag UPDATE never ran |
| `SyncStateList` | internal/tools/memory_sync_delta.go:1035 | `TestSyncStateListOrderAndEmpty`, `TestSyncStateListRefusesForeignLedger` | empty ledger → empty list, no error; oldest-synced first; anchors verbatim; counter row key shape; foreign file → error + nil rows |

Callers (usages verified): `TaskRunWritebackTagged` →
`cmd/agent/cli_tasks.go:1545` (worker loop round);
`TaskRunWritebackTags` ← `internal/tools/memory_sync_delta.go:311`
(`TaskRunWritebackTagged` calls it); `TagWorkerClaim` ← same-line sibling.
`WorkerTaskTag`, `SyncStateList` and `TagWorkerClaim` currently have
in-package callers only (plus the CLI worker comment at
`cmd/agent/cli_tasks_worker.go:304`): they are exported so `cmd/agent` can
reach them, and the worker loop reaches `TaskRunWritebackTagged` through the
`tools` package. Asserted as the documented faces.

## Invariants asserted

* **Provenance is idempotent**: `TagWorkerClaim` is a set union — a retried
  writeback can never grow the tag column, and re-tagging an already-tagged
  row is a byte-for-byte no-op. This is what makes the worker tag safe to
  ride the sync channel (it re-converges instead of accumulating).
* **Provenance is additive, never destructive**: the worker tag is UNIONED
  onto the row's tags; the dispatcher's `module:*` / `keep:*` tags survive.
* **Half-writes are impossible**: the tags UPDATE in `TaskRunWritebackTags`
  rides a SUCCESSFUL terminal write. An refused status leaves both the
  status and the tags column exactly as they were (asserted, not assumed) —
  the tag write happening before the status write would be a silent
  corruption of sync provenance.
* **The sync-state listing is a truthful, ordered view**: oldest-synced
  first, anchors carried verbatim, and a broken/foreign ledger ERRORS
  instead of printing an empty topology (an operator debugging "why is
  nothing syncing" must not be told "no peers").

## Not Done / Deliberate gaps

1. **Full-counter rows are listed by `SyncStateList`** (asserted as a
   documented shape, not fixed). The per-peer incremental counter is stored
   in the same `sync_state` table under the `<peer>#full-counter` key, so
   `SyncStateList` returns it as if it were a peer's anchor row. A caller
   that renders `--status` from this list would print a bogus peer. Today the
   CLI uses `SyncPeerStatus(peer)` per declared peer, so there is no
   user-visible bug — but the coupling is a latent trap and is recorded here
   rather than silently asserted away. Not filed as a bug: no caller
   mis-uses it (a bug ticket needs a reproduction, and there is none).
2. No test drives `TaskRunWritebackTagged` through the actual worker loop
   (`cmd/agent/cli_tasks_worker.go`); the epic scope is `internal/tools`
   coverage, and the loop is a CLI shell around this call.

## Verification

- `go vet ./internal/tools/` → clean.
- Raw target run:
  `go test -count=1 -run 'TestWorkerTaskTag|TestTagWorkerClaim|TestTaskRunWritebackTagged|TestTaskRunWritebackTags|TestSyncStateList' ./internal/tools/`
  → `ok github.com/nick/gopi/internal/tools 1.220s`
- Coverage: none of the 5 names listed by
  `python3 scripts/func_coverage.py --pkg internal/tools --uncovered`.
  Package 148/181 = 81.8%.
- Two-run full-suite raw output: `devdoc/cov-tools-sync-summary.md`.

## Determinism

Isolated temp ledgers (`newDeltaTestDB` / `newDeltaTestDBClient` pin
`GOPI_MEMORY_DB` + CLIENT role and reset node identity). Fixture rows are
inserted with explicit timestamps (`deltaInsertRaw`), so ordering assertions
never depend on the wall clock; no sleeps, no network, no ssh. The tag tests
compare exact strings rather than "contains", so they cannot pass on a
partial write.

## Workflow feedback

* The tags-column tests are the ones that caught real semantics worth
  documenting: `TaskRunWritebackTags` returns BEFORE touching tags when the
  string is empty, and refuses the tag write when the status is illegal.
  Both are "no silent half-write" contracts (Rule 15) that a
  mention-only test would have missed.
* `SyncStateList`'s counter-row coupling is the kind of thing a coverage
  task surfaces for free — worth one devdoc paragraph instead of a
  speculative bug ticket.
