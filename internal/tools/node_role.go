package tools

// Declared node role — is THIS node the sync server?
//
// WHY THIS FILE EXISTS (Rule 43: move the general tool out before deleting the
// mechanism that used to own it). IsServerNode lived in memory_sync_delta.go,
// a file the cross-node retirement plan deletes (devdoc/sync-retire-deletion-order.md
// §b3). A measured `go build -gcflags=-e` experiment removing that file shows
// its consumers are NOT sync code and no batch owns them:
//
//	internal/tools/finish_cleanup_survivors.go:1631  (the finish-cleanup gate)
//	internal/tools/task_ground_baseline.go:90        (the ground's baseline)
//
// Both are single-node paths: one decides whether a finishing run may rewrite
// the shared tree, the other records what a ground was cut from. Deleting the
// file that happens to declare the node role would make the plan compile only
// by deleting its consumers too — the failure this refactor exists to stop.
//
// NOTHING BELOW IS REWRITTEN (Rule 4: a MOVE). The two decision paths, the
// fail-safe default and the incident they encode are the shipped behaviour.

import (
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
)

// IsServerNode reports whether THIS node is the sync server — the only
// side allowed to rule on conflicts. A node names its peers in sync_peers;
// the server's name is NOT in its own peer list. Client detection (its own
// hostname IS among the peers) is best-effort: os.Hostname failing means
// "not server" — the legacy conflict-tag path stays (a tag never loses
// data). Exported for the finish-cleanup client gate (task_finish_cleanup.go).
func IsServerNode() bool {
	// ① Explicit config wins (bug044): ResolveNodeRole reads settings.json
	// role field — declared topology, never guessed. Primary source.
	if role, _ := config.ResolveNodeRole(); role != "" {
		return role == config.NodeRoleServer
	}
	// ② Legacy fallback: hostname check, FAIL-SAFE FIXED (N1, 2026-09-01
	// live incident): unknown hostname defaults to CLIENT — the old default
	// let any host missing from the alias-based peer list claim server
	// rights and merge the central repo. Client overreach is cheap.
	self, err := os.Hostname()
	if err != nil || strings.TrimSpace(self) == "" {
		return false
	}
	peers, err := config.ResolveSyncPeers(nil)
	if err != nil {
		return false
	}
	for _, p := range peers {
		if strings.EqualFold(strings.TrimSpace(p), self) {
			return false // my own name is in my peer list → I am the client
		}
	}
	return false // N1 fix: unknown host defaults to CLIENT (fail-safe)
}
