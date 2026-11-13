package config

import (
	"fmt"
	"os"
	"strings"
)

// NodeRole is the settings.json "role" value: this machine's declared
// identity in the multi-node topology ("server" = the dispatcher/center;
// "client" = a compute node that reports back to sync_to). Declared as
// DATA, never guessed from hostname — same philosophy as compute_nodes /
// sync_peers (t-bug041: Bond guessed its node identity from the remotes
// list because neither side's settings.json carried a role field; the
// guess was right, but only by luck).
type NodeRole string

const (
	NodeRoleServer NodeRole = "server"
	NodeRoleClient NodeRole = "client"
)

// NodeRoleSettingsKey / SyncToSettingsKey are the settings.json keys
// carrying this node's declared identity ("role": "server"|"client",
// "sync_to": "<ssh alias of the node this client reports/syncs to>").
const (
	NodeRoleSettingsKey = "role"
	SyncToSettingsKey   = "sync_to"
)

// RoleEnv is the env override for the node role (Rule 2: read in exactly
// one place — ResolveNodeRole).
const RoleEnv = "GOPI_NODE_ROLE"

// SyncToEnv is the env override for the sync_to alias.
const SyncToEnv = "GOPI_SYNC_TO"

// nodeIdentity is the resolved, process-wide node identity (resolved once
// per process by ResolveNodeRole; empty role = undeclared).
var nodeIdentity struct {
	role   NodeRole
	syncTo string
}

// ResolveNodeRole merges the role sources, highest precedence first:
// env (GOPI_NODE_ROLE) > settings.json "role". The result is cached —
// the identity never changes within one process. An empty role means
// "undeclared" (single-machine default; every role-driven branch treats
// it as the plain local case). The ResetNodeIdentityForTest seam lives at
// the file's tail next to NodeIdentityBriefing (ONE seam — the 5f70dbd
// merge briefly duplicated it here).
func ResolveNodeRole() (NodeRole, string) {
	if nodeIdentity.role != "" || nodeIdentity.syncTo != "" {
		return nodeIdentity.role, nodeIdentity.syncTo
	}
	role := NodeRole(strings.ToLower(strings.TrimSpace(os.Getenv(RoleEnv))))
	syncTo := strings.TrimSpace(os.Getenv(SyncToEnv))
	cfg, err := FromSettingsFile(ResolveSettingsPath())
	if err == nil {
		if role == "" {
			role = NodeRole(strings.ToLower(strings.TrimSpace(string(cfg.Role))))
		}
		if syncTo == "" {
			syncTo = strings.TrimSpace(cfg.SyncTo)
		}
	}
	switch role {
	case NodeRoleServer, NodeRoleClient:
	default:
		role = "" // unknown spelling (e.g. a typo) = undeclared, never guessed
	}
	nodeIdentity.role, nodeIdentity.syncTo = role, syncTo
	return role, syncTo
}

// NodeIdentityLine renders the one-line banner identity:
// "node: hp (client→zbook)" / "node: zbook (server)". Empty when the role
// is undeclared (no banner on a machine that never opted into the
// topology — keeps single-machine output clean).
func NodeIdentityLine() string {
	role, syncTo := ResolveNodeRole()
	if role == "" {
		return ""
	}
	host, _ := os.Hostname() //stylegate:ignore host name is a display label; a failure leaves it empty and nothing downstream breaks
	if host == "" {
		host = "unknown"
	}
	if syncTo != "" {
		return fmt.Sprintf("node: %s (%s→%s)", host, role, syncTo)
	}
	return fmt.Sprintf("node: %s (%s)", host, role)
}

// NodeIdentityBriefing is the node-identity line appended to every
// `task run` briefing so a dispatched child knows from its FIRST message
// which node it is executing on (bug041: the briefing said "check role =
// client" but no data existed to read — now the fact ships with the task).
// Empty (no line at all) when the role is undeclared.
func NodeIdentityBriefing() string {
	line := NodeIdentityLine()
	if line == "" {
		return ""
	}
	return "[node] " + line
}

// ResetNodeIdentityForTest clears the memoised node identity (test seam —
// ResolveNodeRole caches process-wide; tests flipping roles need a reset).
func ResetNodeIdentityForTest() { nodeIdentity.role, nodeIdentity.syncTo = "", "" }
