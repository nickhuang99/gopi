package tools

import (
	"os"
	"strings"
	"testing"
)

// worktreeRecycleLiveDiagEnv names the env var that turns the live
// diagnostic on (Rule 1b: the string is a value, not a literal).
const worktreeRecycleLiveDiagEnv = "GOPI_WTRECYCLE_LIVE_DIAG"

// TestZZWorktreeRecycleLiveDiag is an env-gated LIVE diagnostic seam: it runs
// the real finish-cleanup chain against the production ledger for a
// comma-separated list of row ids and logs each report (skipped by default —
// zero cost in a normal run). It was written for the 2026-09-12 stranded-
// worktree disposition and is KEPT deliberately: judging "may this directory
// be recycled?" from outside the chain means re-implementing the ownership
// gate, and re-implementations drift. Run
//
//	GOPI_WTRECYCLE_LIVE_DIAG=<id>[,<id>...] go test -v -run TestZZWorktreeRecycleLiveDiag ./internal/tools/
//
// and read why= per row (empty + skipped=false means the chain would act).
// The permanent coverage lives in the WTRECYCLE tests.
func TestZZWorktreeRecycleLiveDiag(t *testing.T) {
	if os.Getenv(worktreeRecycleLiveDiagEnv) == "" {
		t.Skip("live diagnostic disabled")
	}
	ids := strings.Split(os.Getenv(worktreeRecycleLiveDiagEnv), ",")
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		rep, err := taskFinishCleanupSessionID(id)
		if err != nil {
			t.Logf("LIVE %s: ERROR %v", id, err)
			continue
		}
		t.Logf("LIVE %s: skipped=%v why=%q branch=%q worktree=%q steps=%v",
			id, rep.Skipped, rep.SkipReason, rep.Branch, rep.Worktree, rep.Steps)
	}
}
