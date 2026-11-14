<!-- doc-class: historical -->
# Task Report: Node identity — role/sync_to config fields + startup banner (bug041)

## Completed
- `internal/config/config.go`: `Config` struct gains `Role string json:"role,omitempty"`
  ("server" = sync hub e.g. peerbox / "client" = satellite e.g. hp) and
  `SyncTo string json:"sync_to,omitempty"` (client's push destination).
  Documented as the DECLARED-topology source for bug041's four-step chain,
  M4.19 tick git channel, M4.18 claim broadcast — data-driven, never
  `if hostname`.
- `internal/config/role.go` (new): `NodeBanner(role, syncTo)` — one-line
  `[gopi] node: <host> (<role>→<sync_to>)` string; empty role → empty string
  (undeclared topology renders NO banner, never a hostname guess).
  `nodeName()` is a DISPLAY label (lowercased hostname) only.
- `cmd/agent/main.go`: after `printLogPaths`, print the banner line to stderr
  when `base.Role != ""` (settings already parsed at that point — zero extra
  file reads).
- Settings values (written manually per epic, verified present):
  hp `~/.gopi/settings.json` = `role: client, sync_to: peerbox`;
  peerbox = `role: server`. Config parsing now surfaces them.
- Tests `internal/config/role_test.go`: `TestRoleSyncToParsing` (server form,
  client form, absent-fields → zero values) + `TestBannerNodeRole` (server,
  client→, and empty-role renders nothing).

## Not Done
- Epic items ④ (task_submit/task run briefing auto-append identity line) —
  epic marked it optional ("banner 已够"); not implemented.
- Role-driven behavior branches (bug041 chain / M4.19 / M4.18) — this task
  delivers the DATA + visibility; semantic branching is follow-up work
  reading `cfg.Role`.
- peerbox live banner not observed from here (ssh-verified settings only);
  HP banner verified live.

## Verification
- Build: `go build ./...` OK; `go vet ./internal/config ./cmd/agent` clean
- Tests: `go test ./internal/config ./cmd/agent -count=1` — ok (all pass)
- Live banner (HP, real binary + real settings):
  `[gopi] node: hostb (client→peerbox)`
- Commit: dfc82f1 on branch feat/node-role-identity (worktree
  ~/workspace/gopi-node-role, shared with main repo)

Recovery: `git show feat/node-role-identity:devdoc/node-role-identity.md`
