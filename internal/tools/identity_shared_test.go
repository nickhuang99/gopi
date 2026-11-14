package tools

// ONE QUESTION, ONE ANSWER — the regression guard for the consolidation of the
// three-factor identity ladders (epic proc-identity-single-predicate).
//
// Five callers used to answer "is the recorded runner still, right now, the same
// live process?" with their OWN ladder, and two of them disagreed about the very
// same record:
//
//	shape                                    tools ladder            procname ladder
//	---------------------------------------  ----------------------  ------------------
//	stat readable, incarnation DIFFERS        dead / not-live         recycled / not-live
//	stat UNREADABLE, dir present              NOT-LIVE (a death!)     unknown
//	incarnation matches, cmdline differs     live (in-place exec)    recycled (a death!)
//	no incarnation recorded                  unknown                 (not routed here)
//
// The two middle rows are the fail-open drift: a caller reading "not-live" for a
// factor the kernel never answered, or for an in-place exec, is exactly the
// reading that let a sweep free a live run's worktree (memory #2632, measured
// on this host 2026-09-17).
//
// These tests drive the REAL production callers (not the predicate directly)
// over a synthetic procfs tree, so a caller that re-rolls its own ladder — or
// that reads the shared predicate's "cannot prove it live" as "it is dead" —
// fails here. On the branch base, the tests in
// TestUnmeasurableIsNotADeathFor every caller that reported the unmeasurable
// shape as a death, and TestCallersOfOneQuestionAgreeFor the cmdline/no-record
// rows.

import (
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// identityTestPid is the pid slot the synthetic trees use.
	identityTestPid = 4242
	// identityTestTicks is the incarnation the fixtures record.
	identityTestTicks = 900001
	// identityTestComm is the fixture's comm — deliberately carrying a space and
	// parentheses, the shape that shifts every stat column for a naive parser.
	identityTestComm = "probe (gopi) worker"
	// identityTestState is a running state letter.
	identityTestState = "R"
	// minProductionGoFiles is a read-floor for the gate scan: a broken glob must
	// FAIL rather than pass over zero files.
	minProductionGoFiles = 20
)

// withIdentityProcRoot points the package's identity reads at a synthetic procfs
// root for one test and restores the production value. The seam is what lets the
// real callers be driven over a fixture the test controls; production reads
// procname.DefaultProcRoot (see identityProcRoot's doc for why it is not an env
// variable).
func withIdentityProcRoot(t *testing.T, root string) {
	t.Helper()
	prev := identityProcRoot
	identityProcRoot = root
	t.Cleanup(func() { identityProcRoot = prev })
}

// writeIdentityProcTree builds a synthetic procfs tree holding ONE pid slot, and
// reports the tree root. withStat=false writes the process DIRECTORY and its
// cmdline but NO stat file — the "the directory is there, the kernel did not
// answer the incarnation question" shape (equally what a restricted /proc gives).
func writeIdentityProcTree(t *testing.T, withStat bool) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, strconv.Itoa(identityTestPid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("fixture: mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, procCmdlineName),
		[]byte(strings.Join([]string{"/usr/bin/gopi", "task", "run"}, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatalf("fixture: write cmdline: %v", err)
	}
	if !withStat {
		return root
	}
	// The stat line is built from the package's OWN field numbers, so the fixture
	// can never spell a column differently from the reader. The tokens after the
	// parenthesised comm run field procStatFieldOffset .. procStatStartTimeField
	// (3..22), so column N sits at index N-procStatFieldOffset and the slice is one
	// longer than the offset: a fixture that stopped at field 21 would leave field
	// 22 reading "0", and parseTickCount rejects 0 as POSIX — every record would
	// then be UNMEASURABLE, which is how this fixture was caught.
	after := make([]string, procStatStartTimeField-procStatFieldOffset+1)
	for i := range after {
		after[i] = "0"
	}
	after[0] = identityTestState                          // field 3
	after[len(after)-1] = strconv.Itoa(identityTestTicks) // field 22
	line := strconv.Itoa(identityTestPid) + " (" + identityTestComm + ") " + strings.Join(after, " ") + "\n"
	if err := os.WriteFile(filepath.Join(dir, procStatName), []byte(line), 0o644); err != nil {
		t.Fatalf("fixture: write stat: %v", err)
	}
	return root
}

// TestUnmeasurableIsNotADeathForEveryCaller is the per-caller regression: with
// the incarnation question UNANSWERED by the kernel (the process directory is
// present, its stat is not readable), each caller must answer "cannot prove it
// is live/dead" — never the death answer, which is the one the destructive arms
// act on. The base branch's ladders are quoted at each assertion.
func TestUnmeasurableIsNotADeathForEveryCaller(t *testing.T) {
	root := writeIdentityProcTree(t, false)
	withIdentityProcRoot(t, root)
	incarnation := int64(identityTestTicks)

	// ① the ledger verdict (`task status` / `gopi session resume`). Base: unknown too —
	// kept as the anchor that says the OTHER callers are the ones that drifted.
	if got := taskIdentityVerdict(identityTestPid, taskProcIdentity{
		Pid: identityTestPid, Start: incarnation, HasStr: true,
	}); got.State != TaskProcessStateUnknown {
		t.Errorf("task status answered %q for an unreadable incarnation, want %q — a factor the kernel did not "+
			"answer must never authorise a reclaim", got.State, TaskProcessStateUnknown)
	}

	// ② the finish-cleanup VETO. Base: it returned false here — the directory was
	// present but its stat was unreadable, and the ladder treated an unreadable
	// factor as a proven death, which is what freed a live run's worktree.
	ident := procIdentity{Pid: identityTestPid, StartTicks: incarnation}
	if !finishCleanupProcessHolderLive(ident) {
		t.Errorf("the cleanup veto was LIFTED for a runner whose incarnation could not be read — the base branch " +
			"did exactly this (finishCleanupProcessHolderLive's ladder read an unreadable stat as 'return false'), " +
			"so the host cannot prove the runner is gone and must hold the directory")
	}
	// …and the DISPOSITION question (`is this row a dead local holder?`) must
	// answer NO for the same record: a release needs a MEASURED death, not the
	// absence of a measurement.
	if finishCleanupDeadLocalHolder(taskStatusInProgress, claimStampForTest(identityTestPid), ident) {
		t.Errorf("the row was classified a DEAD LOCAL HOLDER while its runner's incarnation was unmeasurable — " +
			"that disposition names the freed directory in a release note and is the fabricated death Rule 15a forbids")
	}

	// ③ the resume probe. Base: it fell through to the cwd cross-check and then
	// declared holderProbeDead, so an unmeasurable record authorised a resume.
	probe := holderProbe{ident: procIdentity{Pid: identityTestPid, StartTicks: incarnation}}
	if got := probe.resolve(); got != holderProbeUnreadable {
		t.Errorf("the resume probe answered %q for an unmeasurable incarnation, want %q — resuming on a fact the "+
			"host did not measure double-runs a live task", got, holderProbeUnreadable)
	}

	// ④ the take-over progress guard. Its SUBJECT was retired on master
	// (93009688 deleted the retired merger daemon's takeover-progress
	// implementation, merger_takeover_progress.go included), so the guard this
	// epic routed no longer exists and there is nothing to drive here. The
	// question it asked is now asked ONLY through procname — asserted directly,
	// on the same unmeasurable shape.
	if outcome, why := procname.Match(
		procname.Liveness{Pid: strconv.Itoa(identityTestPid), StartTicks: incarnation},
		procname.Liveness{Pid: strconv.Itoa(identityTestPid)},
	); outcome == procname.OutcomeLive || why == "" {
		t.Errorf("the one comparison ACCEPTED (or silently measured) a record whose incarnation could not be "+
			"compared: (%q, %q) — an unmeasured factor must never be promoted to a match (Rule 15a)", outcome, why)
	}
}

// claimStampForTest builds the claim stamp shape the ledger writes
// ("<hostname>:<pid>") for this host, so the fixtures exercise the real
// claim-resolution path rather than a string the code does not parse.
func claimStampForTest(pid int) string {
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	return host + ":" + strconv.Itoa(pid)
}

// TestCallersOfOneQuestionAgreeForTheSharedRecord is the agreement proof: ONE
// recorded identity, every caller. The incarnation-mismatch row and the
// no-procfs row must produce the same verdict everywhere, and the cmdline row
// must not turn an in-place exec into a death.
//
// On the branch base this test FAILS for the cmdline row: the ledger ladder said
// "running" (matching ticks ARE the identity) while the finish-cleanup ladder and
// the resume probe said "not live / dead". Both callers were answering the same
// question, so one of them was wrong about a live process — the disagreement is
// the defect, whichever way it resolves.
func TestCallersOfOneQuestionAgreeForTheSharedRecord(t *testing.T) {
	root := writeIdentityProcTree(t, true)
	withIdentityProcRoot(t, root)
	incarnation := int64(identityTestTicks)
	// A command line that does NOT match the fixture's — the in-place-exec shape.
	const foreignCmd = "/bin/sh -c sleep 30"

	// Row 1: the incarnation matches, the recorded command line differs.
	// Authoritative answer: LIVE (the process replaced its image in place).
	identMatch := procIdentity{Pid: identityTestPid, StartTicks: incarnation, Cmd: foreignCmd}
	ledger := taskIdentityVerdict(identityTestPid, taskProcIdentity{
		Pid: identityTestPid, Start: incarnation, HasStr: true, Cmd: foreignCmd,
	})
	if ledger.State != TaskProcessStateRunning {
		t.Errorf("the ledger verdict answered %q for a matching incarnation, want %q", ledger.State, TaskProcessStateRunning)
	}
	if !finishCleanupProcessHolderLive(identMatch) {
		t.Errorf("the finish-cleanup veto said NOT LIVE for a matching incarnation (%q) while the ledger said %q — "+
			"two callers of one question disagreeing about a live process is the defect this epic closes",
			identMatch, ledger.State)
	}
	if got := (holderProbe{ident: identMatch}).resolve(); got != holderProbeLive {
		t.Errorf("the resume probe answered %q for a matching incarnation while the ledger said %q — a mismatched "+
			"recorded cmdline is a HINT (the runner may have re-exec'd), never a death", got, ledger.State)
	}

	// Row 2: the incarnation DIFFERS (the number was recycled). Authoritative
	// answer: not live — and every caller must say so.
	identRecycled := procIdentity{Pid: identityTestPid, StartTicks: incarnation + 1}
	if got := taskIdentityVerdict(identityTestPid, taskProcIdentity{
		Pid: identityTestPid, Start: incarnation + 1, HasStr: true,
	}); got.State != TaskProcessStateRecycled {
		t.Errorf("the ledger verdict answered %q for a differing incarnation, want %q", got.State, TaskProcessStateRecycled)
	}
	if finishCleanupProcessHolderLive(identRecycled) {
		t.Errorf("the finish-cleanup veto HELD a runner whose pid was recycled — the number now belongs to a stranger")
	}
	if got := (holderProbe{ident: identRecycled}).resolve(); got != holderProbeDead {
		t.Errorf("the resume probe answered %q for a recycled pid, want %q", got, holderProbeDead)
	}
	// (The take-over comparison was retired with its subject — see ④ above.)

	// Row 3: NO incarnation recorded. The command line is then the only factor,
	// and a mismatch is a role change — the worker count's question. Every caller
	// agrees that this is a MEASURED not-live.
	roleOnly := taskProcIdentity{Pid: identityTestPid, Cmd: "/usr/bin/gopi task serve"}
	if got := taskIdentityVerdict(identityTestPid, roleOnly); got.State != TaskProcessStateRecycled {
		t.Errorf("a record with no incarnation and a different cmdline answered %q, want %q — with no tick the "+
			"cmdline is the only factor there is", got.State, TaskProcessStateRecycled)
	}
}

// TestIdentityPredicateIsTheOnlyLadder is the Rule 13/14 gate for this epic: a
// caller may not compare the incarnation factor itself. A second ladder drifts
// fail-open and deletes a live worktree (memory #2632), so the check is
// mechanical rather than a promise in a doc comment.
//
// The pattern is precise on purpose: comparing a RECORDED starttime against a
// LIVE one (the two names the callers use for that pair), NOT incidental tick
// arithmetic — an age rendering, a display conversion or a "the row recorded
// none" guard is not an identity ladder. `procname.Match`, `procname.RunnerLive`
// and `procname.RunnerIdentity` are the routes. The two inline ladders this epic
// found are both gone: merger_takeover_progress.go's progressTripleMatches was
// routed, and then that whole file was retired on master (93009688).
func TestIdentityPredicateIsTheOnlyLadder(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDiskFixture)
	banned := []struct{ name, re string }{
		{"recorded-vs-live starttime comparison",
			`(rec|recorded|want|row)\.(StartTicks|Start)\b[^\n]*[!=]=[^\n]*(startTicks|live|start)\b`},
		{"live-vs-recorded starttime comparison",
			`\b(live|startTicks|start)\b[^\n]*[!=]=[^\n]*(rec|recorded|want)\.(StartTicks|Start)\b`},
	}
	files := productionGoFiles(t)
	if len(files) < minProductionGoFiles {
		t.Fatalf("scanned only %d production files — the glob is broken and this gate would pass "+
			"vacuously", len(files))
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			code := stripGoLineComment(line)
			for _, b := range banned {
				//stylegate:ignore matches against a constant pattern the test controls
				if ok, _ := regexp.MatchString(b.re, code); ok {
					t.Errorf("%s:%d: %s — the three-factor comparison lives in internal/procname "+
						"(procname.Match / RunnerLive / RunnerIdentity); route this call site through it "+
						"instead of re-inlining the ladder. Offending line: %s",
						f, i+1, b.name, strings.TrimSpace(code))
				}
			}
		}
	}
}

// stripGoLineComment removes a trailing // comment so a sentence ABOUT the rule
// (every caller's doc comment quotes the old comparison) is not mistaken for the
// rule being broken.
func stripGoLineComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// productionGoFiles lists this package's non-test Go files.
func productionGoFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		out = append(out, e.Name())
	}
	return out
}
