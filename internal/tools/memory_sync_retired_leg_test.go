package tools

// memory_sync_retired_leg_test.go — the retired remote SESSION leg refuses
// locally, and carries the sentinel its callers classify on.
//
// WHY THIS FILE EXISTS AT ALL, AND WHY IT IS NOT CALLED WHAT IT REPLACED: it
// started life as the surviving arm of the 2026-10-28 defect's test file
// (memory_sync_full_bound_test.go). Renaming it is not tidying — a file name
// says WHY it was created, and the arms that name justified are gone, so
// keeping the name would leave a reader looking for the bound model's arms in a
// file that holds a refusal arm. The rename is part of resolving that
// modify/delete conflict honestly: upstream's DELETION stands, and the one
// surviving assertion lands under the subject it actually measures.
//
// THE DEFECT IT WAS WRITTEN FOR: the ssh leg of a FULL reconciliation
// (`gopi task sync --full <peer>`) was bounded by the DELTA channel's fixed 15s
// stall detector (syncSSHTimeout), while the work tracked ROW COUNT — a ~1300
// row peer was killed at 15s, an 8-row peer passed. Its arms pinned ① a large
// ledger completing under a work-sized ceiling, ② a stuck peer still killed at
// it with the error naming the bound, and ③ the delta leg keeping its stall
// detector.
//
// THE SUBJECT IS GONE, IN TWO SEPARATE STEPS, AND THIS FILE FOLLOWS IT:
//
//   - the FULL pass itself (SyncFullReconcile, fullSyncSSHBound, the full
//     timeout knob and fullSyncSSHTimeoutError's caller) was deleted upstream
//     with the cross-node sync surface (dc6e439b, 2026-11-05), so arms ① and ②
//     have no production path and no producer to read a bound from;
//   - the ssh invocation behind BOTH shapes is retired at its composition
//     point (remote_leg_retired.go): the verb the leg dialled
//     (`gopi task sync --serve-remote`) is deleted and answers with a removal
//     notice + exit 1, so SyncPeerSession now refuses before any ssh argv
//     exists. Arm ③ measured `LegBoundSec`, which a refusal never reports.
//
// WHAT IS ASSERTED HERE INSTEAD is the half a pure function cannot state: the
// surviving leg must REFUSE, must carry the sentinel its callers classify on
// (`errors.Is(err, errRemoteLegRetired)` — a flattened error reads to
// classifyClaimLockRound as an unreachable peer and YIELDS the claim), and must
// not have contacted anyone on the way. The bound MODEL's pure-function arms
// went with `memory_sync_full_bound.go`; nothing here re-derives them.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
)

// retiredLegSandbox pins a node that declares one peer and an in-memory ledger,
// and puts an ssh stand-in on PATH that RECORDS its argv when run. The record is
// the point: a resolve-then-refuse ordering would run it, so the file's absence
// is positive evidence the refusal happened before any argv existed.
func retiredLegSandbox(t *testing.T, peer string) (argvFile string) {
	t.Helper()
	path := testutil.NewTestDBPath(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	db, err := openTasksDB(t.Context(), path)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}
	t.Setenv(config.SyncPeersEnv, peer)
	t.Setenv("GOPI_SYNC_NODE", "local-node")
	// The anchor guard opens its OWN ssh leg (a pending COUNT query) whose
	// answer is sqlite3 -json, not the session envelope — disarm it so this arm
	// measures the SESSION leg rather than the guard's dial.
	t.Setenv(gopiAnchorGuardEnv, "0")
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)

	dir := t.TempDir()
	argvFile = filepath.Join(dir, "argv")
	shim := filepath.Join(dir, "ssh")
	body := "#!/bin/sh\necho \"$@\" > " + argvFile + "\n" +
		"printf '{\"version\":2,\"node\":\"zpeer\",\"exported_at\":7,\"rows\":[]}'\n"
	if err := os.WriteFile(shim, []byte(body), 0o755); err != nil {
		t.Fatalf("write ssh stand-in: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvFile
}

// TestDeltaLegRefusesWithoutDialling — the delta session is the ONE remote
// shape this binary still HAS a signature for, and the signature is kept
// deliberately (its callers and its seam outlive the verb), so this arm is the
// one that can still go red if a future change reinstates the composition.
func TestDeltaLegRefusesWithoutDialling(t *testing.T) {
	const peer = "zpeer"
	argvFile := retiredLegSandbox(t, peer)
	deltaInsertRaw(t, retiredLegTaskID(0), "retired row", "pending", 1000, 1000, "")

	_, err := SyncPeerSession(peer, nil)
	if err == nil {
		t.Fatal("a retired leg must refuse, not report success")
	}
	if !errors.Is(err, errRemoteLegRetired) {
		t.Fatalf("the refusal must carry the retired sentinel (callers classify it with errors.Is — a "+
			"flattened error reads as an unreachable peer), got: %v", err)
	}
	// Rule 15: it names its own cause, and the peer it did NOT contact.
	msg := err.Error()
	for _, want := range []string{"RETIRED", peer} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal must name %q:\n%s", want, msg)
		}
	}
	if argv, readErr := os.ReadFile(argvFile); readErr == nil {
		t.Fatalf("a retired leg DIALLED — the stand-in recorded argv %q; the refusal must happen before any "+
			"ssh argv exists", argv)
	}
}

// retiredLegTaskID renders a stable, unique task id for the fixture rows.
func retiredLegTaskID(i int) string {
	const alphabet = "0123456789abcdef"
	b := []byte("01a17e7f-2ce0-702f-bc13-dc5629ee0277")
	// Vary the last 6 characters so ids stay distinct and UUID-shaped.
	for k := 0; k < 6; k++ {
		b[len(b)-1-k] = alphabet[(i>>(4*k))&0xf]
	}
	return string(b)
}
