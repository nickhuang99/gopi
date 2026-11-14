<!-- doc-class: historical -->
# Task Report: M4.13 memsync ssh-argv E2E — shim failure root-caused & fixed

## Completed
- Root-caused the E2E shim failure (`ssh bnode "gopi task sync --serve-remote"` →
  peer gopi entered the AGENT LOOP instead of serve-remote): ssh-forwarded argv
  shapes are not recognized by `isTasksSubcommand`. Two proven shapes:
  1. **Full ssh argv**: shim/ssh forwards options+host verbatim → gopi argv =
     `[-o, BatchMode=yes, -o, ConnectTimeout=10, bnode, gopi task sync --serve-remote]`
     (strace-proven: the shim's `shift` dropped `-o`, leaving `BatchMode=yes` as args[0]).
  2. **Single quoted positional**: `ssh host "gopi task sync --serve-remote"` →
     one positional string.
  Both fell through to the agent loop (LLM session burned on a handshake).
- Fix `cmd/agent/cli_tasks.go` `isTasksSubcommand`: skip ssh option pairs (`-o X`)
  and lone flags, drop the host token, word-split the remote command, dispatch to
  `runTasksSubcommand`. Handles both shapes above plus the split form.
- `syncSSHExchange` (memory_sync_session.go): remote command stays ONE quoted ssh
  element; hazard documented at `syncRemoteGopi` (memory_sync_task.go).
- `SyncTaskExportDelta` (memory_sync_delta.go): `GOPI_SYNC_NODE` env overrides the
  hostname for patch.Node — same-host two-node sims need distinct node identities
  (provenance `from-<node>` + anti-reflow echo rule depend on it).
- `devdoc/memsync-e2e-sim.sh`: full E2E sim — alias-aware ssh shim (bnode→B,
  anode→A; options/alias parsing mirrors real ssh), two nodes on one host.

## E2E sim results (4/4 green)
- ① A creates 3 rows → session → B sees 3 (pushed=3, anchors commit)
- ② B claims→in_progress→completes one + self-submits → session → A shows the
  row migrated to completed + `delta-from-b` imported
- ③ A's view: `#3 … [completed] charlie` + delta-from-b present (1)
- ④ idempotent re-run: `already=4`, anchor frozen (1788106869)

## Not Done
- serve-tick auto-sync (`GOPI_SYNC_INTERVAL`) smoke-tested only via code path
  (`runTaskSyncTick` wired at cli_tasks.go:730) — no dedicated E2E.
- devdoc/memsync-timestamp-delta.md was referenced in b3b71b6's commit message
  but never written — M4.13 protocol doc still missing (pre-existing gap).

## Verification
- Build: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Tests: `go test ./internal/tools -run TestSync -count=1` ok (34s);
  `go test ./cmd/agent/ ./internal/config/ -count=1` ok with
  ANTHROPIC_AUTH_TOKEN=x (the bare `cmd/agent` failure is pre-existing on clean
  master e820c2e — env-gated brew test, verified via git stash).
- E2E: sim 4/4 stages green (above).
- Commit: 53a965b fix(memsync): ssh argv shapes + GOPI_SYNC_NODE identity — E2E shim green
