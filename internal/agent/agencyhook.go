package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/agencyscan"
	"github.com/nickhuang99/gopi/internal/config"
)

// The agency-proposal pass — the task-end hook's LEVEL-3 face.
//
// WHY HERE: the epic requires zero new daemon and zero new top-level tool, and
// it forbids spending main-loop ATTENTION. finishSessionTaskRecorded is the
// process's existing "a session just ended" edge: the agent is done thinking,
// the prompt is already flushed, and a bounded ledger scan costs nobody a
// token. Running there (form A of the epic) satisfies all three constraints at
// once — no prompt text is added anywhere.
//
// WHAT IT DOES: internal/agencyscan mines the ledger for recurring patterns
// (recurring task families above a failure gate; duplicate knowledge rows) and,
// when enabled, proposes each ONE time as an ordinary pending task via the
// existing TaskSubmit path. Nothing is dispatched, nothing is fixed, no code is
// touched: the output channel is a task row an operator reads.
//
// WHY IT CANNOT FLOOD (the merge-backlog branch-scan precedent, ~600 duplicate
// rows): every proposal carries a fingerprint fence and the miner's dedupe
// probe matches rows in ANY status — a cancelled or completed proposal still
// suppresses its pattern, so a re-run over unchanged data proposes nothing.
// Asserted by TestScan_IdempotentSecondPassHasZeroProposals and by the live
// historical-replay gate (`gopi task improvements`).
//
// WHY IT CANNOT STALL THE EXIT: the pass is bounded (one ledger scan + at most
// a handful of INSERTs), it runs at the very END of the session, and a slow or
// wedged pass can never block the process — the run exits while the detached
// goroutine is still working, and every outcome is a stderr line, never a
// panic (PanicRule: no panic outside init/main/Must*).
const (
	// agencyScanTimeout bounds one mining pass. A wedge must not outlive the
	// session it belongs to: the process exits anyway, and the next session's
	// pass re-reads the same ledger (the work is stateless).
	agencyScanTimeout = 30 * time.Second

	// agencyProposalLogPrefix labels the one stderr line the pass may emit.
	agencyProposalLogPrefix = "[agency]"
)

// agencyPassRunning latches that this process already ran (or is running) the
// pass — one scan per session, no matter how many times the end hook fires
// (the panic-recover path calls it a second time).
var (
	agencyPassMu      sync.Mutex
	agencyPassRunning bool
)

// maybeRunAgencyPass is the end hook's agency face. It is a NO-OP unless
// GOPI_AGENCY is truthy (config.AgencyProposalsEnabled), so a disabled pass has
// provably zero behaviour change — not merely zero output.
//
// FAIL-SOFT: every error path writes one stderr line and returns; the caller's
// close-out sequence is never affected (the row is already terminal by the time
// this runs).
func (a *Agent) maybeRunAgencyPass() {
	if !config.AgencyProposalsEnabled() {
		return
	}
	agencyPassMu.Lock()
	if agencyPassRunning {
		agencyPassMu.Unlock()
		return
	}
	agencyPassRunning = true
	agencyPassMu.Unlock()

	// SYNCHRONOUS on purpose. A detached goroutine loses the race with process
	// exit (the run's last act is os.Exit right after the trajectory stats), so
	// an async pass would be a mechanism that never actually proposes — a
	// silent no-op wearing an enabled flag. Measured cost of one full pass over
	// the live ledger (2789 rows, 6 proposals, real submits): ~0.6s; the
	// timeout is the wedge guard, not the expected path.
	ctx, cancel := context.WithTimeout(context.Background(), agencyScanTimeout)
	defer cancel()
	res, err := agencyscan.Scan(ctx, agencyscan.Options{
		Submit: true,
		// The gate is a named constant in the miner; the operator knob
		// (GOPI_AGENCY_MIN_FAMILY) only ever raises/lowers that default.
		MinFamilySize: config.AgencyMinFamilySize(agencyscan.MinTaskFamilySize),
	})
	if err != nil {
		// Fail-soft: the session is already closed out; a mining failure must
		// never turn a completed task into a failed one.
		fmt.Fprintf(os.Stderr, "%s scan failed: %v\n", agencyProposalLogPrefix, err)
		return
	}
	if res.Proposed == 0 && res.Deduped == 0 && len(res.Candidates) == 0 {
		return // nothing to say — silence is the conservative default
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", agencyProposalLogPrefix, agencyPassSummary(res))
}

// agencyPassSummary renders the pass's one-line outcome.
func agencyPassSummary(res agencyscan.ScanResult) string {
	parts := []string{fmt.Sprintf("scanned=%d candidates=%d", res.Scanned, len(res.Candidates))}
	if res.Proposed > 0 {
		parts = append(parts, fmt.Sprintf("proposed=%d (see `gopi task list`)", res.Proposed))
	}
	if res.Deduped > 0 {
		parts = append(parts, fmt.Sprintf("deduped=%d", res.Deduped))
	}
	if res.Dropped > 0 {
		parts = append(parts, fmt.Sprintf("dropped=%d", res.Dropped))
	}
	return strings.Join(parts, " ")
}
