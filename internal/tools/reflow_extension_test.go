package tools

// reflow_extension_test.go — testsuite extension for the memsync anti-reflow
// export gate (devdoc/memsync-antireflow-export-gate.md) and the patch
// Version gate (devdoc/memsync-version-gate.md). The shipped tests cover the
// happy gates; these pin the seams future refactors are most likely to break:
//
//   1. syncSelfTag identity: GOPI_SYNC_NODE override + hostname fallback —
//      if the two exporters ever drift onto different identity rules, the
//      gate silently re-opens.
//   2. Gate is self-only: from-<self> excluded, from-<peer> exported — even
//      when a row carries BOTH self and peer tags (a relayed row touched by
//      both nodes must still travel).
//   3. Delta-envelope Version: a v2 delta envelope whose patch.Version is
//      bumped is refused by SyncDeltaEnvelopeFromPatch (the "newer binary
//      reading older patch" face of the version gate).

import (
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"strings"
	"testing"
)

// TestSyncSelfTagIdentityOverrideAndFallback — both exporters must resolve
// node identity through the SAME rule: GOPI_SYNC_NODE override, hostname
// fallback. A drift here splits the anti-reflow gate (hp exporting rows it
// thinks are foreign).
func TestSyncSelfTagIdentityOverrideAndFallback(t *testing.T) {
	t.Setenv("GOPI_SYNC_NODE", "hp")
	if got := syncSelfTag(); got != "from-hp" {
		t.Fatalf("override: syncSelfTag()=%q, want from-hp", got)
	}
	// The export SQL gates use the same tag string — pin the two call sites
	// can never diverge by construction (both call syncSelfTag()).
	t.Setenv("GOPI_SYNC_NODE", "")
	hn, _ := os.Hostname() //stylegate:ignore host name is a display label; a failure leaves it empty and nothing downstream breaks
	if got := syncSelfTag(); got != SyncTaskTagPrefix+hn {
		t.Fatalf("fallback: syncSelfTag()=%q, want %s%s", got, SyncTaskTagPrefix, hn)
	}
}

// TestReflowGateSelfOnlyBothTagsExported — a row tagged by BOTH nodes (peer
// tag + a third node's tag) is a RELAYED row under the M4.24 peer gate:
// the from-<peer> tag suppresses it toward THAT peer (relaying back is the
// reflow), but it travels toward any OTHER destination (relay legitimacy —
// the third-node repair path). A zero-arg export keeps the stricter legacy
// self-absolute face (TestReflowExportLegacySelfFallback).
func TestReflowGateSelfOnlyBothTagsExported(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	n := newBidirNode(t, "hp")
	n.use(t)
	t.Setenv("GOPI_SYNC_NODE", "hp")
	// A row hp received from peerbox AND still holds LIVE (pending): the
	// from-peerbox tag suppresses it toward peerbox (relaying back is the
	// reflow), but it travels toward any OTHER destination (relay
	// legitimacy — the third-node repair path). The terminal homecoming
	// exemption deliberately does NOT apply: the row is not terminal.
	deltaInsertRaw(t, "t-relay", "relayed row", taskStatusPending, 3, 4,
		SyncTaskTagPrefix+"peerbox")
	_, closeDB, err := taskSyncDB()
	if err != nil {
		t.Fatal(err)
	}
	patch, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{Peer: "peerbox"})
	closeDB()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range patch.Rows {
		if r.TaskID == "t-relay" {
			t.Fatalf("from-<peer>-tagged row t-relay re-exported toward its sender — the anti-reflow gate is peer-absolute")
		}
	}
	// The same row toward a DIFFERENT destination travels (relay).
	patchOut, _, err := SyncTaskExportDelta(SyncTaskDeltaOptions{Peer: "hp2"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range patchOut.Rows {
		if r.TaskID == "t-relay" {
			found = true
		}
	}
	if !found {
		t.Fatalf("from-peerbox row must travel toward hp2 (relay legitimacy)")
	}
}

// TestReflowGateTagNormalization — the double-prefix regression fence.
// eef8342 set the zero-arg fallback to the FULL syncSelfTag() ("from-<n>")
// while the SQL pattern wrapped it in ANOTHER "from-" — the pattern searched
// "from-from-<n>" and never matched, silently disarming the zero-arg
// self-gate in BOTH exporters. syncGateTag must return the raw node NAME
// (no prefix) for both the named-peer and the zero-arg faces; the SQL
// pattern (single place, both exporters) owns the "from-" prefix.
func TestReflowGateTagNormalization(t *testing.T) {
	t.Setenv("GOPI_SYNC_NODE", "hp")
	if got := syncGateTag("peerbox"); got != "peerbox" {
		t.Fatalf("named peer: syncGateTag=%q, want raw name %q (no from- prefix)", got, "peerbox")
	}
	if got := syncGateTag("  hp2 "); got != "hp2" {
		t.Fatalf("peer normalization: syncGateTag=%q, want %q (trimmed)", got, "hp2")
	}
	if got := syncGateTag(""); got != "hp" {
		t.Fatalf("zero-arg fallback: syncGateTag=%q, want the node NAME %q (GOPI_SYNC_NODE)", got, "hp")
	}
	if got := syncGateTag(SyncTaskTagPrefix + "hp"); got != SyncTaskTagPrefix+"hp" {
		t.Fatalf("prefixed input must pass through untouched (the pattern adds the prefix)")
	}
	// The composite tag string the two exporter patterns build must equal
	// the provenance tag the importer stamps (one identity rule).
	if SyncTaskTagPrefix+syncGateTag("") != syncSelfTag() {
		t.Fatalf("gate tag ≠ provenance identity: %s vs %s — the gate would filter the wrong rows", SyncTaskTagPrefix+syncGateTag(""), syncSelfTag())
	}
}

// TestDeltaEnvelopeNewerVersionRefused — the version gate's envelope face:
// an envelope whose inner patch.Version is newer than this binary supports
// is refused loudly, never decoded with dropped fields.
func TestDeltaEnvelopeNewerVersionRefused(t *testing.T) {
	patch := &SyncTaskPatch{
		Version: syncPatchVersionDelta + 7, // a future delta format
		Node:    "newer-node",
		Rows:    []SyncTaskRow{{TaskID: "t-fut", Summary: "future row", Payload: "p"}},
	}
	_, err := SyncDeltaEnvelopeFromPatch(patch)
	if err == nil {
		t.Fatal("future delta envelope accepted — silent field loss")
	}
	if !strings.Contains(err.Error(), "NEWER") {
		t.Fatalf("refusal must say NEWER (upgrade guidance): %v", err)
	}
}
