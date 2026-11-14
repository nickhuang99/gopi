package tools

// The task-row-sync switch and the CLOSE-OUT CHAIN'S branch push-back.
//
// This file answers the one question that decides whether the switch is safe
// to default OFF: does a client node that exchanges NO task rows still HAND ITS
// BRANCH BACK? The chain's push-back is the DELIVERY axis
// (now finish_cleanup_survivors.go: 58b5618 retired the mechanism and moved the
// resolver entry point here, where the push-back legs still read it), not the
// collaboration axis: it is how a finished
// run's work reaches the repository that can merge it. A switch that silently
// stopped it would strand every finished branch on the node that produced it.
//
// The mechanism that makes this a MEANINGFUL test rather than a tautology:
// finishCleanupServerTarget() (the chain's push-target resolver) and the
// submit forward's taskSubmitForwardPeer() both read `sync_peers` + this
// node's identity, so a "gate the peer-derived crossings" sweep reaches both
// of them from one habit. They must not be gated alike. The push-back target
// resolver reads NO switch — it is fed to the chain directly from the row's
// accepted close-out — and these tests pin that by name.
//
// The physics is the same real two-node git world the push-contract suite uses
// (newFinishCleanupFixture: a bare server + a client + a linked worktree), read
// from the PEER's refs, never from the step list alone.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// switchOffSettingsPinsTaskSync points GOPI_SETTINGS at a fixture whose switch
// state is the one under test, so the case never depends on the operator's real
// settings file (whose stale `task_sync` would decide the outcome). The peer
// list rides the fixture too — this file's fixture is a LOCAL bare repo pushed
// over a path remote, so no ssh alias is needed for the push itself.
func switchOffSettingsPinsTaskSync(t *testing.T, taskSyncOn bool) {
	t.Helper()
	dir := t.TempDir()
	body := `{"sync_peers":["switch-test-peer"]}`
	if taskSyncOn {
		body = `{"task_sync":true,"sync_peers":["switch-test-peer"]}`
	}
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, p)
	t.Setenv(config.TaskSyncEnv, "")
}

// TestClientBranchPushBackSurvivesTaskSyncOff is the safety property of the
// epic: with the task-row-sync switch OFF (its default), the close-out chain of
// a client node still pushes the deliverable branch to the peer. The
// observation is the SERVER's own ref, so a report claiming a push cannot pass.
func TestClientBranchPushBackSurvivesTaskSyncOff(t *testing.T) {
	switchOffSettingsPinsTaskSync(t, false)
	f := newFinishCleanupFixture(t)
	tip := pushContractGit(t, f.wtDir, "rev-parse", "HEAD")

	rep := f.runChain()
	assertStep(t, rep, finishCleanupPushVerb, true)

	if got := pushContractGit(t, f.serverDir, "rev-parse", "refs/heads/"+f.featBranch); got != tip {
		t.Fatalf("task_sync off cost the branch push-back: peer has %q, want the deliverable tip %q (report: %+v)",
			got, tip, rep.Steps)
	}
}

// TestClientBranchPushBackTargetResolverIgnoresTaskSync is the STRUCTURAL half
// of the test above, pinned where a future edit would break it: the chain's
// push-target resolver must not consult the task-row-sync switch, and it must
// still derive its target from the DECLARED PEERS.
//
// THE ASSERTION FOLLOWS THE DELEGATION (2026-10-16). The peer lookup moved out
// of this function into the ONE shared resolver every peer-pushing leg now calls
// (peerRemoteTargetFor — the ssh ALIAS is asked of this repo's git remotes, so a
// repo whose remote is named otherwise still delivers). Reading only one
// function's body then failed for the RIGHT reason — "the assertion above would
// be vacuous" — but the vacuity it feared is not what happened: the resolution
// is still peer-derived, one call deeper. So the guard now walks the SAME path a
// reader would (the chain resolver → the shared resolver → the declared-peer
// reader) and asserts the property over the WHOLE path. It is not weaker: the
// switch is refused anywhere on the path, the peer reader must still be reached,
// and every body the assertion reads must EXIST (toolsTestFuncBody fails loudly
// on an absent function, so a rename cannot make this pass by silence).
func TestClientBranchPushBackTargetResolverIgnoresTaskSync(t *testing.T) {
	chainSrc, err := os.ReadFile(tssfChainSrcFile)
	if err != nil {
		t.Fatalf("read %s: %v", tssfChainSrcFile, err)
	}
	resolverSrc, err := os.ReadFile(tssfResolverSrcFile)
	if err != nil {
		t.Fatalf("read %s: %v", tssfResolverSrcFile, err)
	}
	// The path, in the order it is walked. finishCleanupServerTarget is the
	// chain's entry point; the two resolutions are where the row's repo and the
	// process's own checkout are asked; peerRemoteDeclaredAlias is the ONE
	// reader of the declared topology.
	entry := toolsTestFuncBody(t, string(chainSrc), "func finishCleanupServerTarget(")
	path := map[string]string{
		"finishCleanupServerTarget":           entry,
		"finishCleanupServerTargetResolution": toolsTestFuncBody(t, string(chainSrc), "func finishCleanupServerTargetResolution("),
		"finishCleanupRowTargetResolution":    toolsTestFuncBody(t, string(chainSrc), "func finishCleanupRowTargetResolution("),
		"peerRemoteDeclaredAlias":             toolsTestFuncBody(t, string(resolverSrc), "func peerRemoteDeclaredAlias("),
		// The selector's two faces are one implementation (Rule 14), so the ARMS
		// live in the shared body, one call deeper than the face the legs use.
		"peerRemoteDeclaredAliasWithReason": toolsTestFuncBody(t, string(resolverSrc), "func peerRemoteDeclaredAliasWithReason("),
		"peerRemoteDeclaredPeers":           toolsTestFuncBody(t, string(resolverSrc), "func peerRemoteDeclaredPeers("),
	}
	for name, body := range path {
		if strings.Contains(body, "TaskSyncEnabled") {
			t.Fatalf("%s consults the task-row-sync switch — the branch push-back is the DELIVERY axis and must survive the switch being off:\n%s", name, body)
		}
	}
	// The delegation must be REAL: the entry point reaches the shared resolver.
	if !strings.Contains(entry, tssfResolverEntrySymbol) {
		t.Fatalf("finishCleanupServerTarget no longer reaches the shared peer resolver %s — the chain's target would come from somewhere this guard cannot see:\n%s",
			tssfResolverEntrySymbol, entry)
	}
	if !strings.Contains(path["peerRemoteDeclaredAlias"], tssfSelectionSymbol) {
		t.Fatalf("the alias reader no longer reaches the shared selection %s — the two-arm priority would then be decided somewhere this guard cannot see:\n%s",
			tssfSelectionSymbol, path["peerRemoteDeclaredAlias"])
	}
	if !strings.Contains(path["peerRemoteDeclaredAliasWithReason"], tssfPeersArmSymbol) {
		t.Fatalf("the alias selection no longer reaches the sync_peers arm (%s) — the fallback the topology has always had is gone, and the assertion above would be vacuous:\n%s",
			tssfPeersArmSymbol, path["peerRemoteDeclaredAliasWithReason"])
	}
	// The DECLARED DESTINATION is the first arm the selector must reach, and it
	// must be reached through the config layer's own resolver — never by reading
	// the settings key here (Rule 2).
	if !strings.Contains(path["peerRemoteDeclaredAliasWithReason"], tssfSyncToArmSymbol) {
		t.Fatalf("the alias selection no longer reaches %s — the declared delivery destination has no delivery reader again:\n%s",
			tssfSyncToArmSymbol, path["peerRemoteDeclaredAliasWithReason"])
	}
	syncToArm := toolsTestFuncBody(t, string(resolverSrc), "func peerRemoteSyncTo(")
	if !strings.Contains(syncToArm, "config.ResolveNodeRole(") {
		t.Fatalf("the sync_to arm must read the config layer's ONE node-identity resolver (config.ResolveNodeRole), never the settings key itself:\n%s", syncToArm)
	}
	if !strings.Contains(path["peerRemoteDeclaredPeers"], tssfTopologyReaderSymbol) {
		t.Fatalf("the declared-peer reader no longer asks the topology (%s) — the assertion above would be vacuous:\n%s",
			tssfTopologyReaderSymbol, path["peerRemoteDeclaredPeers"])
	}
}

const (
	// tssfChainSrcFile / tssfResolverSrcFile are the two files the asserted path
	// spans: the chain that renders the target, and the ONE shared resolver that
	// derives it (Rule 1: a file name a test names is a constant, not a literal).
	tssfChainSrcFile    = "finish_cleanup_survivors.go"
	tssfResolverSrcFile = "peer_remote_target.go"

	// tssfResolverEntrySymbol is the shared resolver the chain's entry point must
	// reach — the delegation itself is asserted, so a target produced by some
	// other path cannot pass by looking correct.
	tssfResolverEntrySymbol = "finishCleanupServerTargetResolution"

	// tssfDeclaredPeersSymbol / tssfTopologyReaderSymbol are the two links that
	// keep the path peer-derived: the alias selector, and the single reader of
	// the declared topology under it.
	tssfDeclaredPeersSymbol  = "peerRemoteDeclaredPeers("
	tssfTopologyReaderSymbol = "config.ResolveSyncPeers("
	// tssfPeersArmSymbol / tssfSyncToArmSymbol are the two ARMS the alias
	// selector now reaches (Rule 14: sync_to first, sync_peers as the fallback).
	// The selector delegates to one function per arm, so the asserted path is
	// still real: the alias reader must reach the peers arm, and the peers arm
	// must reach the declared topology.
	tssfPeersArmSymbol  = "peerRemoteFirstUsablePeer("
	tssfSyncToArmSymbol = "peerRemoteSyncTo("

	// tssfSelectionSymbol is the ONE body both faces of the selector delegate to
	// (peerRemoteDeclaredAlias answers an alias; the WithReason face answers the
	// alias plus its diagnosis). The ARMS are inside it, so the guard must reach
	// it explicitly rather than assume the face carries the decision.
	tssfSelectionSymbol = "peerRemoteDeclaredAliasWithReason("
)

// toolsTestFuncBody returns the text of one top-level function of a source
// file, from its declaration to the line closing it at column 0. It fails
// loudly when the function is absent (a silent empty body would make the
// assertion above pass vacuously).
func toolsTestFuncBody(t *testing.T, src, decl string) string {
	t.Helper()
	i := strings.Index(src, decl)
	if i < 0 {
		t.Fatalf("%s not found in the source under test", decl)
	}
	rest := src[i:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("%s has no recognizable end", decl)
	}
	return rest[:end]
}
