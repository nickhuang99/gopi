package tools

// THE DECLARED DELIVERY DESTINATION COMES FIRST — the priority table of
// peerRemoteDeclaredAlias, asked of the ONE reader every delivery leg shares.
//
// WHY THIS FILE EXISTS (measured 2026-10-16 on this branch): `sync_to` — the key
// the operator declares to say "this client reports/syncs to THAT peer" — had NO
// delivery reader anywhere in the tree. `internal/config/node_role.go` only
// memoised the value (it fed the startup banner), and every git leg took its
// alias from `sync_peers[0]` (peer_remote_target.go:203). The consequence on a
// client that lists more than one peer is not cosmetic: the branches are
// delivered to whichever alias happens to come first in the TASK-LEDGER
// whitelist, and the destination the operator named for delivery is unused
// without a word (devdoc/hp-push-topology-audit.md G8).
//
// WHAT IS PINNED, in the order the function decides:
//
//	① sync_to declared and usable  → IT is the alias (sync_peers may disagree;
//	   the declared destination wins).
//	② sync_to empty                → the first usable sync_peers entry, which is
//	   the pre-existing behaviour kept verbatim as the fallback arm.
//	③ sync_to declared but UNUSABLE→ the fallback is taken AND the reason names
//	   the declared value, the env var that overrides it, the validator's own
//	   sentence and the destination actually in force (Rule 15: never a silent
//	   substitution).
//	④ neither usable               → "" — there is nobody to deliver to, the one
//	   meaning that answer has always had.
//
// The environment is pinned per case with t.Setenv (Rule 17) and the node
// identity is reset through the config layer's own test seam, because
// config.ResolveNodeRole memoises process-wide — without the reset a case would
// read the previous case's identity and the table would be meaningless.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

const (
	// prstSyncToPeer is the DECLARED delivery destination of the cases below:
	// a valid ssh alias that no checkout here configures as a remote, so the
	// cases are about the ALIAS CHOICE and not about remote resolution.
	prstSyncToPeer = "declared-delivery-destination"

	// prstPeersFirst / prstPeersSecond are the task-ledger whitelist of the
	// cases: two usable aliases, deliberately DIFFERENT from the declared
	// destination, so "which one was chosen" is observable rather than a
	// tautology.
	prstPeersFirst  = "ledger-peer-first"
	prstPeersSecond = "ledger-peer-second"

	// prstOptionShaped is an alias the ssh-alias validator refuses for a measured
	// reason: a leading '-' makes ssh parse it as an OPTION (it can run a local
	// command), so it must never reach ssh or git. It is the shape a mistyped or
	// hostile `sync_to` takes.
	prstOptionShaped = "-oProxyCommand=evil"
)

// prstAlias asks the ONE reader for its answer and its diagnosis, with a clean
// node-identity cache on both sides of the call.
func prstAlias(t *testing.T) (string, string) {
	t.Helper()
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)
	return peerRemoteDeclaredAliasWithReason()
}

// TestPeerRemoteDeclaredAliasPrefersTheDeclaredDestination is the priority
// table itself.
func TestPeerRemoteDeclaredAliasPrefersTheDeclaredDestination(t *testing.T) {
	t.Run("sync_to declared and usable wins over a disagreeing sync_peers list", func(t *testing.T) {
		t.Setenv(config.SyncToEnv, prstSyncToPeer)
		t.Setenv(config.SyncPeersEnv, prstPeersFirst+","+prstPeersSecond)

		alias, reason := prstAlias(t)
		if alias != prstSyncToPeer {
			t.Fatalf("the declared delivery destination must win, got %q (want %q) — sync_peers would have answered %q",
				alias, prstSyncToPeer, prstPeersFirst)
		}
		if reason != "" {
			t.Fatalf("nothing was overridden, so there is nothing to explain, got %q", reason)
		}
		if got := peerRemoteDeclaredAlias(); got != prstSyncToPeer {
			t.Fatalf("the plain face must answer what the diagnosed face answers, got %q", got)
		}
	})

	t.Run("empty sync_to falls back to the first usable sync_peers entry", func(t *testing.T) {
		t.Setenv(config.SyncToEnv, "")
		t.Setenv(config.SyncPeersEnv, prstPeersFirst+","+prstPeersSecond)

		alias, reason := prstAlias(t)
		if alias != prstPeersFirst {
			t.Fatalf("the fallback arm must answer the first declared peer, got %q (want %q)", alias, prstPeersFirst)
		}
		if reason != "" {
			t.Fatalf("nothing was declared and overridden, got a diagnosis %q", reason)
		}
	})

	t.Run("an unusable peer entry is skipped, the next usable one is taken", func(t *testing.T) {
		t.Setenv(config.SyncToEnv, "")
		t.Setenv(config.SyncPeersEnv, prstOptionShaped+","+prstPeersSecond)

		alias, _ := prstAlias(t)
		if alias != prstPeersSecond {
			t.Fatalf("an option-shaped alias must never reach ssh or git; got %q (want %q)", alias, prstPeersSecond)
		}
	})

	t.Run("an UNUSABLE sync_to is overridden loudly, never silently", func(t *testing.T) {
		t.Setenv(config.SyncToEnv, prstOptionShaped)
		t.Setenv(config.SyncPeersEnv, prstPeersFirst)

		alias, reason := prstAlias(t)
		if alias != prstPeersFirst {
			t.Fatalf("the fallback must be taken when the declared destination cannot be used, got %q", alias)
		}
		// Rule 15: the sentence must carry the four facts an operator needs —
		// what was declared, which channel declares it, why it was refused, and
		// what is in force instead.
		for _, want := range []string{config.SyncToEnv, prstOptionShaped, prstPeersFirst} {
			if !strings.Contains(reason, want) {
				t.Errorf("the override's diagnosis must name %q, got %q", want, reason)
			}
		}
		if !strings.Contains(reason, "UNUSED") {
			t.Errorf("the diagnosis must say the declared destination is UNUSED (not honoured), got %q", reason)
		}
	})

	t.Run("an unusable sync_to with no usable peer answers NOBODY and says why", func(t *testing.T) {
		t.Setenv(config.SyncToEnv, prstOptionShaped)
		t.Setenv(config.SyncPeersEnv, "")

		alias, reason := prstAlias(t)
		if alias != "" {
			t.Fatalf("with no usable peer the answer is empty (nobody to deliver to), got %q", alias)
		}
		for _, want := range []string{config.SyncToEnv, prstOptionShaped, "NO peer"} {
			if !strings.Contains(reason, want) {
				t.Errorf("the empty answer's diagnosis must name %q, got %q", want, reason)
			}
		}
	})

	t.Run("neither declared answers empty with nothing to explain", func(t *testing.T) {
		t.Setenv(config.SyncToEnv, "")
		t.Setenv(config.SyncPeersEnv, "")

		alias, reason := prstAlias(t)
		if alias != "" || reason != "" {
			t.Fatalf("nothing was declared: want (\"\", \"\"), got (%q, %q)", alias, reason)
		}
	})
}

// TestPeerRemoteDeclaredAliasIsDeclaredNotGuessed states the fail-soft property
// as its own case: a client whose ONLY declaration is `sync_to` and whose
// sync_peers list is empty still has a delivery target — the topology's peer
// list is the FALLBACK, never a precondition for honouring the declared
// destination.
func TestPeerRemoteDeclaredAliasIsDeclaredNotGuessed(t *testing.T) {
	t.Setenv(config.SyncToEnv, prstSyncToPeer)
	t.Setenv(config.SyncPeersEnv, "")

	alias, reason := prstAlias(t)
	if alias != prstSyncToPeer {
		t.Fatalf("a node that declares ONLY sync_to must still have a delivery destination, got %q", alias)
	}
	if reason != "" {
		t.Fatalf("nothing was overridden, got %q", reason)
	}
}
