package tools

// Pre-fix control repro (RED at master) and its switch-era successor.
//
// ORIGINAL CONTRACT (epic remote-submit-central-ledger, 2026-09-19): a submit
// issued on a MIRROR leg (role=client) must either reach the authoritative
// ledger or refuse LOUDLY naming the local path it would have written. Pre-fix
// it did neither: it wrote the row into the node-local ledger and reported
// success — a black hole.
//
// WHAT CHANGED (epic: the five conflated axes). "role=client" is the node's
// IDENTITY (who arbitrates a conflict, whose finish-cleanup push-back runs);
// "this node's ledger is a remote mirror" is the COLLABORATION question, and it
// is now declared by its own switch, config.TaskSyncSettingsKey (default OFF).
// The black hole is still the defect — but the way to a black hole is no longer
// "wrote locally", because on a node that exchanges NOTHING the local ledger IS
// the queue that claims, runs and notifies the row. So this repro asserts BOTH
// halves: with the exchange ON and no peer the refusal must fire (unchanged),
// and with the exchange OFF (its default) the row must land locally AND be
// really there — a success receipt with no row behind it would still be the
// silent failure Rule 15b bans.
//
// Seam-free on purpose: it uses only the public submit API + env, so it
// compiles unchanged against the pre-fix and the post-fix tree.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

func TestReproRemoteLegSubmitMustNotWriteNodeLocalLedgerSilently(t *testing.T) {
	dir := t.TempDir()
	mirror := filepath.Join(dir, "mirror-memory.db")
	t.Setenv(config.MemoryDBEnv, mirror)
	t.Setenv(config.MemoryDBEnvLegacy, "")
	// A mirror leg: the node declares itself a client and names NO
	// authoritative peer — so no forward is possible.
	t.Setenv(config.RoleEnv, string(config.NodeRoleClient))
	t.Setenv(config.SyncToEnv, "")
	// Hermetic topology: the resolved identity must not depend on the
	// operator's real settings.json (a stray sync_to there would turn this
	// refusal case into a forward case).
	empty := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(empty, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write empty settings: %v", err)
	}
	t.Setenv(config.SettingsEnv, empty)
	config.ResetNodeIdentityForTest()
	t.Cleanup(config.ResetNodeIdentityForTest)

	// ── ① the ORIGINAL contract, under the configuration it was written for:
	// the node DOES exchange task rows, and has no peer to do it with. The
	// only honest outcome is still a loud refusal naming the local path.
	t.Setenv(config.TaskSyncEnv, config.EnvBoolOn)
	out, err := TaskSubmit(TaskSubmitOptions{Summary: "mirror-leg submit", Source: "human"})
	if err == nil {
		t.Fatalf("BLACK-HOLE: the submit reported success on a mirror leg with no authoritative peer: %q (row written to the node-local ledger %s)", out, mirror)
	}
	if !strings.Contains(err.Error(), mirror) {
		t.Fatalf("a refusal must NAME the ledger path it would have written; got: %v", err)
	}
	if rows := taskRowsIn(t, mirror); len(rows) != 0 {
		t.Fatalf("the refusal wrote %d row(s) into the node-local ledger %v", len(rows), rows)
	}

	// ── ② the DEFAULT (switch off, the upgrade posture): the same node is now
	// allowed to submit locally, and the receipt must be HONEST — the row is
	// really in the ledger the receipt names, not merely reported.
	t.Setenv(config.TaskSyncEnv, "")
	out, err = TaskSubmit(TaskSubmitOptions{Summary: "local submit with task sync off", Source: "human"})
	if err != nil {
		t.Fatalf("a client node that exchanges no task rows must be able to submit locally: %v", err)
	}
	id := taskIDFromConfirmation(t, out)
	if row := taskRowIn(t, mirror, id); row == nil {
		t.Fatalf("SILENT FAILURE: the receipt claims %q was queued but the local ledger %s does not carry it", id, mirror)
	}
}
