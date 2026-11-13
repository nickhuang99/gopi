package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// peakGuardRemovalDoc is the devdoc recording WHY the peak-hour cost guard was
// deleted. Every assertion in this file cites it, so a future reader who finds
// one of these tests failing gets the decision (and the four bug tasks + five
// merge-conflict branches behind it) instead of a mystery (PROJECT.md Rule 7).
const peakGuardRemovalDoc = "devdoc/peak-guard-removal.md"

// removedPeakGuardSymbols are the identifiers the guard was built out of: the
// start gate and its argv classifier (cmd/agent), the decision/predicate family
// and the refusal renderer (internal/config), and the exit class a refusal used.
// They are named ONCE here — as the SUBJECT of the absence proofs below, never
// as something production code may reference (that is what the tests assert).
var removedPeakGuardSymbols = []string{
	"applyPeakGuard",
	"peakGuardVerb",
	"costFreeInvocation",
	"PeakHoursDecision",
	"PeakSettings",
	"ResolvePeakHours",
	"ParsePeakFlags",
	"PeakRefusal",
	"PeakDeclarationWarning",
	"IgnorePeakHoursFlagSpelling",
	"IgnorePeakHoursFlag",
	"IgnorePeakHoursEnv",
	"PeakHoursEnv",
	"PeakHoursFlag",
	"PeakGuardExitCode",
}

// removedPeakGuardWireTokens are the operator-facing spellings that must no
// longer exist anywhere in the tree: the CLI flag, the env var, the settings
// keys and the argv exception the merger used to prefix onto its verify child.
var removedPeakGuardWireTokens = []string{
	"--ignore-peak-hours",
	"--peak-hours",
	"GOPI_IGNORE_PEAK_HOURS",
	"GOPI_PEAK_HOURS",
	"ignore_peak_hours",
	"peak_hours",
}

// peakGuardRemovalTrees are the source trees scanned for the symbols above. They
// are the trees the guard lived in; devdoc/ and briefings/ are deliberately
// EXCLUDED — the history of a removed feature is supposed to remain readable
// there (GOPI.md: delete the code, never the history).
var peakGuardRemovalTrees = []string{
	"cmd/agent",
	"internal/config",
	"internal/pricing",
}

// peakGuardScanAllowed names the files that may mention the removed guard, EACH
// with its reason. A negative test has to spell what it forbids, so the removal
// proofs themselves are the one class that must be exempt or the scan would flag
// its own subject. The exemption is a PATH list with a written reason (never a
// keyword allow-list), so a new mention anywhere else still fails the scan.
var peakGuardScanAllowed = map[string]string{
	"cmd/agent/peak_guard_removal_test.go":       "this file: the absence proofs must name the symbols and wire spellings they forbid",
	"internal/config/peak_guard_removed_test.go": "the backward-tolerance proof: it feeds a settings.json carrying the removed keys",
	"internal/config/config.go":                  "the tombstone comment recording where the removed settings keys went (no code honours them)",
}

// peakGuardAllowedScan reports whether rel may mention the removed guard.
func peakGuardAllowedScan(t *testing.T, rel string) bool {
	t.Helper()
	if reason, ok := peakGuardScanAllowed[filepath.ToSlash(rel)]; ok {
		t.Logf("exempt from the removal scan: %s — %s", filepath.ToSlash(rel), reason)
		return true
	}
	return false
}

// TestPeakGuardSymbolsAreAbsentFromTheTree is the acceptance criterion "the
// negative check is executed and shown": the guard's identifiers must occur
// NOWHERE in the production trees. It is exhaustive over every .go file in those
// trees (not a sample), and it is a NEGATIVE test on purpose — a deletion
// verified by reading the diff proves what the author believed, while a scan
// proves what is on disk.
//
// WHY IT DOES NOT SCRAPE THE SOURCE OF A NON-EXISTENT FILE: an earlier shape of
// this proof asserted "peak_guard.go is gone", which passes trivially and says
// nothing about a symbol surviving in a DIFFERENT file (the failure mode of a
// partial removal — half the wiring left behind). The scan is what catches a
// resurrected call site.
func TestPeakGuardSymbolsAreAbsentFromTheTree(t *testing.T) {
	for _, tree := range peakGuardRemovalTrees {
		root := filepath.Join(repoRootForTest(t), tree)
		var scanned int
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			scanned++
			if peakGuardAllowedScan(t, relForTest(t, path)) {
				return nil
			}
			body, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			text := string(body)
			for _, sym := range removedPeakGuardSymbols {
				if i := indexSymbol(text, sym); i >= 0 {
					t.Errorf("%s carries the removed peak-hour guard symbol %q (line %d) — the "+
						"guard was deleted outright and no production path may reference it (%s)",
						relForTest(t, path), sym, lineOf(text, i), peakGuardRemovalDoc)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", tree, err)
		}
		if scanned == 0 {
			t.Fatalf("scan of %s found no .go files — the absence proof would be vacuous", tree)
		}
	}
}

// TestPeakGuardWireTokensAreAbsentOutsideHistoryAndBackwardTolerance pins the
// OPERATOR-FACING half: the flag, the env var and the settings keys must be gone
// from production, from scripts and from every test EXCEPT the two places that
// deliberately mention them —
//
//   - internal/config/peak_guard_removed_test.go, which PROVES an operator's
//     existing `peak_hours` / `ignore_peak_hours` keys still load (the backward
//     tolerance the removal owes every configured deployment), and
//   - internal/config/config.go's tombstone comment, which tells the next reader
//     where those keys went.
//
// Both exceptions are named as PATHS, not as a keyword allow-list, so a new
// mention in any other file fails here (Rule 13's gate shape: the literal may
// live only where the reason is written down).
func TestPeakGuardWireTokensAreAbsentOutsideHistoryAndBackwardTolerance(t *testing.T) {
	for _, tree := range peakGuardRemovalTrees {
		root := filepath.Join(repoRootForTest(t), tree)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel := relForTest(t, path)
			if peakGuardAllowedScan(t, rel) {
				return nil
			}
			body, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for _, tok := range removedPeakGuardWireTokens {
				if i := strings.Index(string(body), tok); i >= 0 {
					t.Errorf("%s still spells the removed guard token %q (line %d) — a leftover "+
						"--ignore-peak-hours / GOPI_IGNORE_PEAK_HOURS workaround is exactly what the "+
						"removal must not leave behind (%s)", rel, tok, lineOf(string(body), i), peakGuardRemovalDoc)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", tree, err)
		}
	}
}

// TestRemovedPeakHoursFlagsNoLongerAffectAStart is the RUNTIME half of the
// operator-facing removal, and it answers briefly asking "what happened to
// --peak-hours?": neither flag changes how a gopi start behaves any more.
//
// The two flags leave in DIFFERENT shapes, and both are asserted separately
// because collapsing them would hide one:
//
//   - `--ignore-peak-hours` (a boolean) is now simply an unknown flag: main's
//     flag scan refuses it by name, so an operator whose script still passes it
//     learns immediately instead of believing an exemption is in force.
//   - `--peak-hours VALUE` is now an INERT token: it is neither parsed nor
//     refused, and the run proceeds exactly as if it had not been passed. That
//     is the documented tolerance for the removed value-taking flag, and the
//     assertion is the load-bearing one — "not refused" alone would also be
//     satisfied by a guard that silently kept working.
//
// Nothing may exit the retired refusal class (4) for a clock reason, which is
// the class check both cases carry.
func TestRemovedPeakHoursFlagsNoLongerAffectAStart(t *testing.T) {
	bin := peakGuardProofBinary(t)
	home := t.TempDir()

	// 1. The boolean exemption flag is refused BY NAME as unknown.
	out, code := runGopiIn(t, bin, guardTestEnv(t, home)(), "", "--ignore-peak-hours", "task", "list")
	if code == peakGuardExitCodeRetired {
		t.Errorf("`gopi --ignore-peak-hours task list` exited %d, the RETIRED peak-refusal class — the "+
			"guard is gone, so nothing may exit %d for a clock reason (%s):\n%s",
			code, peakGuardExitCodeRetired, peakGuardRemovalDoc, out)
	}
	if code == 0 {
		t.Errorf("`gopi --ignore-peak-hours task list` exited 0 — the removed boolean flag must be "+
			"refused by name, or a script passing it believes an exemption is in force (%s):\n%s",
			peakGuardRemovalDoc, out)
	}
	if !strings.Contains(out, "unknown") && !strings.Contains(out, "unhandled") {
		t.Errorf("`gopi --ignore-peak-hours task list` refusal does not name the flag as unknown — "+
			"the message must teach the operator the flag is gone (%s):\n%s", peakGuardRemovalDoc, out)
	}

	// 2. The value-taking window declaration is inert: no refusal, no guard.
	out, code = runGopiIn(t, bin, guardTestEnv(t, home)(), "", "--peak-hours", "09:00-12:00", "task", "list")
	if code == peakGuardExitCodeRetired {
		t.Errorf("`gopi --peak-hours … task list` exited %d — the removed flag must not be able to "+
			"trigger a refusal any more (%s):\n%s", code, peakGuardRemovalDoc, out)
	}
	if strings.Contains(out, "peak-hour cost guard") {
		t.Errorf("`gopi --peak-hours … task list` still prints the guard's refusal wording — removal "+
			"is incomplete (%s):\n%s", peakGuardRemovalDoc, out)
	}
}

// peakGuardExitCodeRetired is the exit code a peak refusal used to carry (4).
// It is spelled as a literal here on purpose: the constant that defined it was
// deleted with the guard, and this test asserts nothing else reclaimed the class
// — a value a test must know about, and the ONE place it is allowed to live now.
const peakGuardExitCodeRetired = 4

// retiredPeakRefusalHead is the first sentence of the retired
// internal/config.PeakRefusal, verbatim from the pre-removal tree
// (`git show 7099edf0:internal/config/workhours.go`). It is pinned as the second
// half of the acceptance pair — "exit code 4 AND the config.PeakRefusal banner" —
// so the control's refusal is identified by its WORDS, not only its exit class.
// The banner's subject is the refused argv, which is why the assertion also
// requires the invocation to be named; a constant that merely restated the exit
// code would prove nothing about which refusal ran.
const retiredPeakRefusalHead = "[gopi] peak-hour cost guard: refusing to start —"

// TestCostFreeVerbsRunInsideAPeakWindow is the POSITIVE half of the removal —
// "prove the verbs work any time of day", not merely "I deleted it".
//
// HOW THE WINDOW IS MADE REAL (not assumed): the removed guard resolved its
// windows from GOPI_PEAK_HOURS, so setting that variable to a window that covers
// the CURRENT clock reproduces the exact condition that used to refuse — the
// decision is taken over fmt.Sprintf("%02d:00", time.Now().Hour()), the hour the
// process is in right now. The window grammar was whole-hours, so an hour-wide
// window is a real window and not an approximation, and this is decisive at ANY
// clock time: a test that hardcoded "run at 14:30" would silently stop measuring
// outside the window, which is precisely how the old guard went unnoticed for
// four hours a day.
//
// THE CONTROL IS THE POINT: the SAME argv, SAME clock and SAME environment are
// also run against a pristine-master binary when one is provided via
// GOPI_REMOVAL_CONTROL_BIN (see devdoc/peak-guard-removal.md §proof for the
// one-line build). This tree must exit 0 while the master binary exits 4 —
// without the control, exit 0 past midnight proves only that the clock was off.
func TestCostFreeVerbsRunInsideAPeakWindow(t *testing.T) {
	// The BINARY is resolved FIRST: the resolver builds with a relative
	// `go build .`, which needs this package's directory, while the repo-context
	// pin below chdirs out of it (pin first and the build fails → the test fails
	// with a build error instead of measuring anything).
	bin := peakGuardProofBinary(t)
	// repo-context sandbox: the A/B below also runs `task run` and `worker
	// --once` against the real binary, so the child must not inherit this
	// process's cwd — the live repository. See repo_context_sandbox_test.go. The
	// CONTROL binary (an operator-supplied pre-removal build, which refuses those
	// two verbs before they can act) is run from the package directory
	// explicitly, by peakGuardRunGopiControl.
	SandboxRepoContext(t)
	nowWindow := fmtWindowCovering(time.Now())
	home := t.TempDir()
	env := guardTestEnv(t, home)()

	verbCases := [][]string{
		{"task", "list"},
		{"version"},
		{"--help"},
		{"task", "show", "does-not-exist"},
	}
	for _, argv := range verbCases {
		out, code := runGopiInWithPeakEnv(t, bin, env, nowWindow, argv...)
		if code == peakGuardExitCodeRetired {
			t.Errorf("`gopi %s` exited %d inside window %s covering the current hour — the guard "+
				"still refuses a start (%s):\n%s",
				strings.Join(argv, " "), code, nowWindow, peakGuardRemovalDoc, out)
		}
		if strings.Contains(out, "peak-hour cost guard") {
			t.Errorf("`gopi %s` still prints the guard's refusal inside %s — removal is incomplete (%s):\n%s",
				strings.Join(argv, " "), nowWindow, peakGuardRemovalDoc, out)
		}
	}

	control := os.Getenv(peakGuardControlBinEnv)
	if control == "" {
		t.Logf("no %s provided — the window case above ran without a control binary; the "+
			"A/B (master refuses, this tree runs) is recorded in %s",
			peakGuardControlBinEnv, peakGuardRemovalDoc)
		return
	}
	if _, err := os.Stat(control); err != nil {
		t.Fatalf("%s=%q is not usable: %v", peakGuardControlBinEnv, control, err)
	}
	// The control MUST still refuse — otherwise the window above proves nothing
	// about the guard and this test would be green for the wrong reason. The
	// verbs are the cost-free ones the guard used to let through (version) and
	// the ones that used to be refused, so the control measures the exact
	// behaviour change.
	//
	// THE SPENDING argv BELONG HERE, ON THE CONTROL SIDE ONLY, and the
	// asymmetry is the finding (devdoc/peak-orphan-audit-20260915.md §2.2): the
	// guard refused `task run` / `worker --once` (which CAN issue an LLM call)
	// AND `task list` / `task show` (which are ledger READS and cannot) — one
	// refusal class for both. Asserting the refusal on the control is what
	// proves the class was clock-driven rather than cost-driven; asserting it on
	// THIS tree would assert the thing the removal deleted. `worker --once` is
	// safe to run here because the control refuses before it ever claims, and
	// the repaired `worker_daemon_flag_e2e_test.go` owns the current tree's
	// worker start (a daemon-leak-safe rig).
	// The subject each case's refusal must name. The retired guard rendered the
	// VERB — the argv classifier's output (`peakGuardVerb`) — not the full
	// invocation, so `worker --once` refuses as `gopi worker`: the guard could not
	// tell `--once` from the never-returning daemon form, which is the guessing
	// the removal answered. Pinning the subject per case keeps that visible.
	controlRefused := []struct {
		argv    []string
		subject string
	}{
		{[]string{"task", "list"}, "`gopi task list`"},
		{[]string{"task", "show", "does-not-exist"}, "`gopi task show`"},
		{[]string{"task", "run", "does-not-exist"}, "`gopi task run`"},
		{[]string{"worker", "--once"}, "`gopi worker`"},
	}
	for _, tc := range controlRefused {
		argv := tc.argv
		out, code := peakGuardRunGopiControl(t, control, env, nowWindow, argv...)
		if code != peakGuardExitCodeRetired {
			t.Errorf("CONTROL binary (pre-removal master) exited %d for `gopi %s` inside %s, want the "+
				"retired refusal %d — without a refusing control the window above proves nothing (%s):\n%s",
				code, strings.Join(argv, " "), nowWindow, peakGuardExitCodeRetired, peakGuardRemovalDoc, out)
		}
		// The BANNER is asserted with the code, not instead of it: the epic's
		// acceptance names the pair (exit 4 + config.PeakRefusal), and a refusal
		// is only the SAME refusal if it renders the same words. `config` is gone
		// from this tree, so the refused sentence is pinned here
		// (retiredPeakRefusalHead, verbatim from the retired
		// internal/config/workhours.go PeakRefusal) and matched against the
		// control's stderr. The banner is verb-subject-specific, so the subject is
		// asserted too — head alone would pass for a refusal about another verb.
		if !strings.Contains(out, retiredPeakRefusalHead) {
			t.Errorf("CONTROL binary refused `gopi %s` with exit %d but did NOT render the refusal "+
				"banner %q — the exit class alone does not prove which refusal it was (%s):\n%s",
				strings.Join(argv, " "), code, retiredPeakRefusalHead, peakGuardRemovalDoc, out)
		}
		if !strings.Contains(out, tc.subject) {
			t.Errorf("CONTROL binary's refusal for `gopi %s` does not name %s — a banner that does not "+
				"say WHAT it refused is not the PeakRefusal text (%s):\n%s",
				strings.Join(argv, " "), tc.subject, peakGuardRemovalDoc, out)
		}
	}

	// The CURRENT tree must not refuse the zero-cost argv the control refused —
	// same clock, same window. The argv is the CURRENT spelling of the same act
	// (`worker <task-id>`, epic worker-run-entry), which is safe to run here: it stops
	// at the singleton/claims guard before doing any work. The spending
	// `worker --once` row of the matrix is measured by hand in the decision
	// record, because starting a real worker inside a unit test needs the
	// daemon-leak rigs that live in their own file.
	if out, code := runGopiInWithPeakEnv(t, bin, env, nowWindow, "worker", "does-not-exist"); code == peakGuardExitCodeRetired {
		t.Errorf("`gopi worker <task-id>` exited %d inside window %s on THIS tree — the removed guard's "+
			"refusal class is still reachable (%s):\n%s", code, nowWindow, peakGuardRemovalDoc, out)
	}
}

// TestWorkerIdleWaitDoesNotDeferToAPeakWindow pins the RESIDENT half of the
// removal: the worker loop used to consult the guard before every claim and used
// to stretch a drained idle wait to the peak boundary. With the guard gone, a
// whole-hour window covering the current hour must not change the wait: the call
// returns within its requested duration and prints nothing.
//
// It measures the loop's own pacing function rather than a claim (a claim needs a
// queue): waitWorkerIdleOrStop sleeps the whole requested span and returns, so a
// resurrected guard (which would clamp the sleep to the window's remaining time,
// up to an hour) shows up here as a timeout — the exact regression a
// "deleted the file" review cannot see.
func TestWorkerIdleWaitDoesNotDeferToAPeakWindow(t *testing.T) {
	t.Setenv("GOPI_PEAK_HOURS", fmtWindowCovering(time.Now()))
	// Not a guard knob — it is what the suite already clears for every rig; the
	// value must be absent so nothing else shortens the wait.
	const wait = 300 * time.Millisecond
	done := make(chan struct{})
	go func() {
		waitWorkerIdleOrStop(wait)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(wait + 5*time.Second):
		t.Fatalf("waitWorkerIdleOrStop(%s) did not return with a peak window covering the current "+
			"hour — the resident loop is still deferring to the clock, i.e. the guard survives inside "+
			"the worker (%s)", wait, peakGuardRemovalDoc)
	}
}

// fmtWindowCovering renders a whole-hour peak window containing `at` — the same
// spelling (and the same whole-hour semantics) the removed guard parsed. It is
// the mechanism that makes the window in these tests REAL at any clock time
// rather than assumed.
func fmtWindowCovering(at time.Time) string {
	h := at.Hour()
	return fmt.Sprintf("%02d:00-%02d:00", h, (h+1)%24)
}

// peakGuardControlBinEnv names the OPTIONAL pristine-master binary the A/B case
// in TestCostFreeVerbsRunInsideAPeakWindow compares against.
const peakGuardControlBinEnv = "GOPI_REMOVAL_CONTROL_BIN"

// The two environment names the removed guard resolved its windows and its
// escape hatch from. They are the REMOVED wire spellings, named here as the
// subject of the window proof: the window test declares the first one to make
// the peak condition REAL at the current clock, and the stripper above removes
// both so the declared value is the one the child actually reads.
const (
	removedPeakWindowEnvName = "GOPI_PEAK_HOURS"
	removedPeakIgnoreEnvName = "GOPI_IGNORE_PEAK_HOURS"
)

// --- binary + path helpers -------------------------------------------------

// peakGuardProofBinary returns THIS tree's gopi binary (this test lives in
// package main, so `.` IS the binary) — the package's ONE build slot
// (gopibin_test.go), built from this tree into the suite scratch root. A build
// failure is FATAL, never skipped: this is an acceptance test, and a skip
// reports a green removal that never ran (the suite's sweep-zombies lesson).
func peakGuardProofBinary(t *testing.T) string {
	t.Helper()
	return gopiTestBinPath(t)
}

// peakGuardRunGopiControl runs the operator-supplied CONTROL binary with the peak
// window declared, FROM THIS PACKAGE'S DIRECTORY rather than the repo-context
// sandbox the test pinned.
//
// WHY THE CONTROL KEEPS THE PACKAGE CWD: the two spending cases in the table
// (`task run` / `worker --once`) are asserted to be REFUSED by the pre-removal
// build, and the refusal is decided before any action runs — so the control is
// measuring a refusal, not a lifecycle, and it must be able to resolve this
// package (it is a real gopi that may shell out relative to its cwd). The cwd is
// still EXPLICIT here (runGopiInDir) instead of inherited, which is what keeps
// this call out of the "hands the child the ambient live repository" class the
// repo-context guard is about.
func peakGuardRunGopiControl(t *testing.T, bin string, env []string, window string, args ...string) (string, int) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("peak guard control: read the package directory: %v", err)
	}
	return runGopiInDir(t, bin, peakGuardWindowEnv(env, window), "", dir, args...)
}

// peakGuardWindowEnv renders the environment one run sees: the caller's vector
// with any ambient copy of the removed guard's variables STRIPPED, then the
// window declared.
//
// The stripping is load-bearing, not tidiness: exec passes a duplicated name
// through to the child in order, and the child's lookup then resolves to the
// ambient value rather than the declared one — a test that appends without
// stripping can silently run OUTSIDE the window it thinks it declared (the
// measurement would then be vacuous, not red).
func peakGuardWindowEnv(env []string, window string) []string {
	clean := make([]string, 0, len(env)+1)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name == removedPeakWindowEnvName || name == removedPeakIgnoreEnvName {
			continue
		}
		clean = append(clean, kv)
	}
	return append(clean, removedPeakWindowEnvName+"="+window)
}

// runGopiInWithPeakEnv runs a gopi binary with the given window declared in the
// environment the removed guard read, and returns the combined output plus the
// exit code. peakGuardWindowEnv renders the vector (see it for why the ambient
// pair is stripped rather than shadowed).
func runGopiInWithPeakEnv(t *testing.T, bin string, env []string, window string, args ...string) (string, int) {
	t.Helper()
	return runGopiIn(t, bin, peakGuardWindowEnv(env, window), "", args...)
}

// repoRootForTest resolves the repository root from this test file's location
// (cmd/agent is two levels below it), so the scans do not depend on the caller's
// working directory.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return abs
}

// relForTest renders a path relative to the repo root for readable failures.
func relForTest(t *testing.T, path string) string {
	t.Helper()
	rel, err := filepath.Rel(repoRootForTest(t), path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// indexSymbol finds `sym` in text as a WHOLE identifier. Substring matching
// would flag an unrelated longer name, so the neighbours are checked for
// identifier characters (a gate that cries wolf gets relaxed — the standards
// gates' own lesson).
func indexSymbol(text, sym string) int {
	for off := 0; ; {
		i := strings.Index(text[off:], sym)
		if i < 0 {
			return -1
		}
		i += off
		before := byte('\n')
		if i > 0 {
			before = text[i-1]
		}
		after := byte('\n')
		if end := i + len(sym); end < len(text) {
			after = text[end]
		}
		if !isIdentByte(before) && !isIdentByte(after) {
			return i
		}
		off = i + 1
	}
}

// isIdentByte reports whether b can be part of a Go identifier (plus '_').
func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// lineOf renders the 1-based line number of byte offset i.
func lineOf(text string, i int) int {
	return strings.Count(text[:i], "\n") + 1
}
