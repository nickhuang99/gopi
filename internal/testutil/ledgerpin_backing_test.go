package testutil

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLedgerSandboxBackingNoticeIsMeasuredNotSpelled pins the notice's job: it
// NAMES the backing filesystem when the sandbox is device-backed, so an
// operator reading one line knows which device every fresh ledger's first open
// queues behind. The device-backed case is now the INTENDED state (operator
// ruling, 2026-09-18: the scratch home is a real directory off the / device,
// never a tmpfs), so the notice STOPS at the measurement — see the second half
// of this test, which fails if a remediation sentence comes back.
//
// The device arm is created through testutil.TestDeviceScratchDir, which places
// it on the shared block-backed scratch home — MEASURED, never spelled. It used
// to create `ledgerpin-device-*` inside this package's own source tree, the same
// in-checkout shape that leaked 341 `.db` files into internal/tools (see
// .gitignore's note and devdoc/testdb-scaffold-contract.md); a directory under
// the source tree is captured by any `git add` run while the test is mid-flight
// and is invisible to the repo-root junk gate. The scratch home is the same
// device (a real directory off `/`, operator ruling 2026-09-18), so the
// measurement is unchanged and the tree no longer accumulates.
//
// When that home is memory-backed there is no device to measure and the arm
// SKIPS with the reason named.
func TestLedgerSandboxBackingNoticeIsMeasuredNotSpelled(t *testing.T) {
	dir := TestDeviceScratchDir(t, "ledgerpin")
	deviceLedger := filepath.Join(dir, "memory.db")
	notice := LedgerSandboxBackingNotice(deviceLedger)
	if notice == "" {
		t.Skipf("the scratch home (%s) is memory-backed — there is no block device to report here", dir)
	}
	if !strings.Contains(notice, deviceLedger) {
		t.Errorf("notice %q must name the ledger it is about", notice)
	}
	if !strings.Contains(notice, "device-backed") {
		t.Errorf("notice %q must say the sandbox is device-backed — that is the fact the operator acts on", notice)
	}
	// The remediation sentence must NOT come back. The scratch home of record is
	// the block device, so advising a tmpfs (or naming the knob that switches to
	// one) would be advising the operator to undo their own ruling.
	for _, retired := range []string{SuiteTempRootEnvName, "tmpfs"} {
		if strings.Contains(notice, retired) {
			t.Errorf("notice %q offers %q — the memory-backed route is retired; the notice reports "+
				"the cost and stops", notice, retired)
		}
	}

	// The SILENT arm (a sandbox that owes no sync is told nothing) has no
	// fixture here any more: every scratch path this suite can reach is on the
	// block device, which is the intended state (operator ruling, 2026-09-18).
	// Asserting it would need a tmpfs the ruling removed, so it is left to the
	// hosts that still have one — the memory arm is exercised by
	// TestScratchRootSharesRootFSAnswersFromStatfs, which builds its own
	// memory-backed fixture rather than assuming the suite's root is one.
}
