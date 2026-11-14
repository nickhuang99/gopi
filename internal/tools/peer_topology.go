package tools

// peer_topology.go — the DECLARED-TOPOLOGY gate: one definition of "this peer
// alias is part of the topology this node declares", referenced by every
// peer-taking face (Rule 14: a shared rule lives in exactly one place;
// Rule 13: no face re-types the wording).
//
// WHY this exists: the rule "sync topology is DECLARED, never discovered"
// was implemented per face, and the copies had drifted — `task sync --peer`
// refused an undeclared alias with the full message (settings key + env
// override named), `task diff --peer` refused with NOTHING (it ran the
// read-only catalog query over ssh and reported ssh's own exit code), and
// `syncPeerDeclared` (the full-reconcile path) carried a third, SHORTER
// spelling that named neither the settings key nor the env override. A rule
// with three spellings is a rule the operator cannot rely on: the message
// they get depends on which verb they typed.
//
// The gate here is deliberately ONE function. It resolves the whitelist
// through its single reader (config.ResolveSyncPeers — Rule 2) and refuses
// with ONE message shape naming all three facts the operator needs: the
// offending alias, the settings.json key, and the env override.
//
// The alias SYNTAX guard (ValidateSSHPeerAlias, peer_alias.go) is a different
// rule with a different owner and stays there: a topology gate says "you did
// not declare this peer", a syntax guard says "this is not a host name at
// all". Callers keep both, in that order when both apply.

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/config"
)

// DeclaredPeerGate is the message TAIL every refusal below ends with — the
// clause the whole family shares (Rule 1b: the phrase lives here, no face
// re-types it; the sync-surface help page quotes the same words).
const DeclaredPeerGate = "sync topology is declared, never discovered"

// assetDeclaredPeerGate is the knowledge channel's clause: the same rule,
// named for the whitelist it is about (the two channels keep separate keys —
// see syncAssetsPeerWhitelisted — but one vocabulary).
const assetDeclaredPeerGate = "memory-asset " + DeclaredPeerGate

// undeclaredPeerError renders the ONE refusal sentence: the alias that is not
// declared, the whitelist it is not part of (settings.json key + env
// override, so the operator knows how to declare it) and the rule clause.
func undeclaredPeerError(peer, whitelistName, key, env, clause string) error {
	return fmt.Errorf("peer %q is not in the %s whitelist (settings.json %q / %s) — %s",
		peer, whitelistName, key, env, clause)
}

// declaredPeerError is the task-ledger channel's refusal (sync_peers).
func declaredPeerError(peer string) error {
	return undeclaredPeerError(peer, config.SyncPeersSettingsKey, config.SyncPeersSettingsKey,
		config.SyncPeersEnv, DeclaredPeerGate)
}

// assetDeclaredPeerError is the knowledge channel's refusal (memory_peers).
func assetDeclaredPeerError(peer string) error {
	return undeclaredPeerError(peer, config.MemoryPeersSettingsKey, config.MemoryPeersSettingsKey,
		config.MemoryPeersEnv, assetDeclaredPeerGate)
}

// PeerDeclared reports whether alias is part of the topology THIS node
// declares (settings.json "sync_peers" / GOPI_SYNC_PEERS). cliPeers supplies
// the highest-precedence source when a caller's own flags declare a topology
// for one run (the session verbs' --peers); nil means "consult the declared
// sources only". A resolution error is returned verbatim (the caller adds its
// own phase prefix): an unreadable topology must never be mistaken for an
// undeclared peer (Rule 15).
func PeerDeclared(peer string, cliPeers []string) (bool, error) {
	peers, err := config.ResolveSyncPeers(cliPeers)
	if err != nil {
		return false, err
	}
	for _, p := range peers {
		if p == peer {
			return true, nil
		}
	}
	return false, nil
}

// RequireDeclaredPeer is the gate itself: nil when alias is declared, else
// the single shared refusal naming the alias, the settings key and the env
// override. Every peer-taking face calls THIS — so a future verb cannot
// invent a fourth spelling, and a fix to the wording is a one-line change
// that every face inherits. The caller prefixes its own phase ("memsync
// sync: ", "diff: "), which is what keeps the message stable for the verbs
// that already printed it.
func RequireDeclaredPeer(peer string, cliPeers []string) error {
	ok, err := PeerDeclared(peer, cliPeers)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return declaredPeerError(peer)
}

// RequireDeclaredAssetPeer is RequireDeclaredPeer on the KNOWLEDGE channel's
// own whitelist (settings.json "memory_peers" / GOPI_MEMORY_PEERS). The two
// channels resolve different keys on purpose (a single-node task config clears
// sync_peers while memory-asset sync stays a kept cross-node feature), so the
// gate parameterizes the declaration source and nothing else.
func RequireDeclaredAssetPeer(peer string) error {
	peers, err := config.ResolveMemoryPeers()
	if err != nil {
		return err
	}
	for _, p := range peers {
		if p == peer {
			return nil
		}
	}
	return assetDeclaredPeerError(peer)
}
