package testutil

// The CONCURRENCY half of the budget entry point's contract: a run that starts
// while another run on this host is alive AND asking for one of the same
// packages must SAY SO in its own output header.
//
// WHY THIS FILE EXISTS — the measured defect (2026-10-16, bug row 01a142e4).
// Two `scripts/test-budget.sh` runs executed on this host: pid 2885273 asked for
// four packages, a neighbour session for two, and the sets intersected in two.
// Both wrote their start records, both started `go test -count=1 -json`, and
// NEITHER printed a word — so every red observed inside that window was quotable
// as one run's own verdict, which it was not. The repository's own rule text had
// already named the shape an observed failure ("Two go test sets in one run");
// the run that suffered from it simply was not told.
//
// WHAT IS ASSERTED, AND WHY BOTH HALVES
// ------------------------------------
// The static half reads the SCRIPT: the check is a declared glob over the
// EXISTING start records (no new artifact — Rule 35), the reporter is defined,
// and the call sits inside the header's guard so an unreadable command line can
// never reach the reader as an empty value under `set -u`.
//
// The dynamic half RUNS the reporter on synthetic records (overlap / disjoint /
// dead pid / this run's own pid) and then runs the REAL script beside a live
// neighbour, reading its stdout through a pipe — the epic's acceptance, executed.
// A synthetic arm is what makes the three cases assertable: waiting for a real
// concurrency accident to exercise a concurrency reporter is how the defect above
// survived in the first place (the same reasoning scripts/lib/stream-scope.sh
// records for the mingled stream).

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// budgetOverlapLib is the shared predicate: sourced by the script, executed
	// directly here with synthetic records.
	budgetOverlapLib = "scripts/lib/budget-overlap.sh"
	// budgetOverlapLibConst is the script's own `budget_overlap_lib` assignment —
	// the single definition the source line references (Rule 1b/13).
	budgetOverlapLibConst = `budget_overlap_lib="` + budgetOverlapLib + `"`
	// budgetOverlapRecordsGlob is the script's glob over the EXISTING start
	// records of other runs: the input of the check, and the reason it needs no
	// new artifact.
	budgetOverlapRecordsGlob = `budget_overlap_records_glob="run-*-start.txt"`
	// budgetOverlapReportCall is the reporter the header must call.
	budgetOverlapReportCall = "report_concurrent_overlap"
	// budgetOverlapHeaderFunc is the function the call must live INSIDE: the run's
	// header is where a caller already looks, and it is printed before the two
	// long silences (the tested-binary build, then the suite).
	budgetOverlapHeaderFunc = "report_run_header"
	// budgetOverlapCallGuardPrefix is how the call must be spelled: as the body
	// of a NEGATED `if`, so a check that could not inspect the host is REPORTED
	// rather than left reading as "no neighbour found" (Rule 1: the guard shape
	// is a constant, and the gate holds the script to it).
	budgetOverlapCallGuardPrefix = "if ! "
	// budgetOverlapMarker is the warning headline — what a reader greps for.
	budgetOverlapMarker = "CONCURRENT BUDGET RUN OVERLAPS THIS SCOPE"
	// budgetOverlapMarkerConst is how that headline is DECLARED in the script
	// (Rule 1b/13: the constant exists once, and the reporter echoes the name).
	budgetOverlapMarkerConst = `overlap_marker="` + budgetOverlapMarker + `"`
	// budgetOverlapMarkerUse is the reporter's USE of that constant. The report
	// goes through the declared name, never a second copy of the headline
	// (Rule 1b/13), so the gate reads the use site as well as the declaration.
	budgetOverlapMarkerUse = "$overlap_marker"
	// budgetOverlapAttributionUse is the reporter's use of the attribution
	// caveat — the sentence that says what the overlap costs a reader.
	budgetOverlapAttributionUse = "$overlap_attribution_note"
	// budgetOverlapStartsUse is the reporter's use of the "starts anyway" note.
	budgetOverlapStartsUse = "$overlap_starts_anyway_note"
	// budgetOverlapStartsNote is the half that keeps the report honest about what
	// it is NOT: a refusal. A run that stopped when it saw a neighbour would let a
	// neighbour's start decide a third party's verdict.
	budgetOverlapStartsNote = "STARTS ANYWAY"
	// budgetOverlapAttributionNote is the half that says what the overlap COSTS:
	// a red seen here is not a verdict about anybody's change until it is
	// reproduced alone. Pinned in both spellings so a reword cannot drop either.
	budgetOverlapAttributionNote = "重叠窗口内的红不作代码归属"
	// budgetOverlapAttributionNoteEN is the English half of the same sentence.
	budgetOverlapAttributionNoteEN = "does NOT belong to any one run's change"
	// budgetOverlapSelfPidMarker prefixes the reporter's self-exclusion: the
	// record whose pid is ours is not a peer (pids recycle).
	budgetOverlapSelfPidMarker = `"$$"`
	// budgetOverlapPeerStart is the start_iso a fixture record carries.
	budgetOverlapPeerStart = "2026-10-16T11:59:56+08:00"
	// budgetOverlapRecordPrefix is the name shape of a start record: the run tag
	// closed by the suffix. A fixture that does not match it would be invisible to
	// the glob the script actually uses.
	budgetOverlapRecordPrefix = "run-1792000000-"
	// budgetOverlapRecordSuffix is the start-record suffix (the script's own
	// constant; repeated here as the gate's expectation, like the sibling gates).
	budgetOverlapRecordSuffix = "-start.txt"
	// budgetOverlapDeadPid is a pid that cannot be a live process on Linux: the
	// pid space is bounded by /proc/sys/kernel/pid_max (4194304 by default) and
	// pid 0 is never a process. Using an impossible rather than a merely-unused
	// number keeps the arm deterministic instead of racing another pid's death.
	budgetOverlapDeadPid = "0"
	// budgetOverlapFixtureModule is the module path the synthetic records are
	// resolved under: a name no real module uses.
	budgetOverlapFixtureModule = "example.test/overlapfixture"
	// budgetOverlapFixturePkg is the package both sides ask for in the overlap
	// arm (and what the real run requests in the end-to-end arm).
	budgetOverlapFixturePkg = "./internal/smokegate/"
	// budgetOverlapDisjointPkg is a package the fixture scope does NOT cover.
	budgetOverlapDisjointPkg = "./internal/sitekey/"
	// budgetOverlapFieldSep is the row separator the library declares, repeated
	// as the gate's expectation for the fixture whose module is synthetic (the
	// real separator is read from the library in the dynamic arm below).
	budgetOverlapFieldSep = "\t"
	// budgetOverlapReadDeadline bounds how long the end-to-end arm waits for the
	// run's header before declaring the report absent.
	budgetOverlapReadDeadline = 60 * time.Second
	// budgetOverlapStubSeconds is how long the stub `go` keeps working, in
	// seconds: a bounded sleep, so the arm can read a header that was printed
	// while a neighbour was still alive (Rule 27: no load is generated).
	budgetOverlapStubSeconds = "3"
	// budgetOverlapReadGoneReturn is the library's DEAD-process branch: the one
	// line a mutation anchors on, so the red proof cannot drift away from the
	// function it is mutating.
	budgetOverlapReadGoneReturn = "if (read < 0) return 0"
	// budgetOverlapEmptyCmdlineKeep is the PRE-FIX spelling this bug removed: a
	// successful read of a ZERO-LENGTH command line answered "peer". Reproduced
	// here as the mutation that must bring the phantom back.
	budgetOverlapEmptyCmdlineKeep = "if (read == 0) return 1"
	// budgetOverlapKernelThreadPid is the preferred empty-command-line fixture:
	// kthreadd (pid 2) is a kernel thread on every Linux host with a normal
	// initial namespace, so the shape does not depend on this kernel's thread
	// inventory. The scan below falls back to any readable empty-cmdline entry.
	budgetOverlapKernelThreadPid = "2"
	// budgetOverlapProcPidDir is the process-table root the fixture is scanned in.
	budgetOverlapProcPidDir = "/proc"
	// budgetOverlapCmdlineName is the per-process file whose SIZE answers "does
	// this process have an argv at all".
	budgetOverlapCmdlineName = "cmdline"
	// budgetOverlapStatName is the per-process file whose existence separates a
	// live process from an already-reaped one.
	budgetOverlapStatName = "stat"
)

// budgetOverlapFaults is the gate's PURE predicate over the script's text: one
// finding per way the concurrency-report contract is unmet. Pure (no filesystem,
// no environment) so the red proof can feed it a mutated script and drive the
// SAME decider the real gate uses, instead of a copy of its logic.
func budgetOverlapFaults(script string) []string {
	var findings []string
	for _, want := range []string{
		budgetOverlapRecordsGlob,
		budgetOverlapLibConst,
		budgetOverlapReportCall + "()",
		budgetOverlapMarkerConst,
		budgetOverlapStartsNote,
		budgetOverlapAttributionNote,
		budgetOverlapAttributionNoteEN,
	} {
		if !strings.Contains(script, want) {
			findings = append(findings, fmt.Sprintf("the script no longer contains %q", want))
		}
	}
	// The check reads the EXISTING records: the reporter must not be the writer of
	// anything (Rule 35 — a report that creates a state file is a new dependency).
	body, ok := budgetFunctionBody(script, budgetOverlapReportCall)
	if !ok {
		findings = append(findings, "the script no longer DEFINES "+budgetOverlapReportCall+"()")
		return findings
	}
	if !strings.Contains(script, `. "$root/$budget_overlap_lib"`) {
		findings = append(findings, "the script does not SOURCE the shared predicate ($budget_overlap_lib) — "+
			"the gate would then be exercising a copy of the logic instead of the one the run uses")
	}
	// The call must sit INSIDE the header function, as the body of an `if`.
	header, headerOK := budgetFunctionBody(script, budgetOverlapHeaderFunc)
	if !headerOK {
		findings = append(findings, "the script no longer DEFINES "+budgetOverlapHeaderFunc+"()")
		return findings
	}
	callGuarded := false
	for _, line := range strings.Split(header, "\n") {
		code := strings.TrimSpace(stripComment(line))
		if code == "" {
			continue
		}
		if strings.HasPrefix(code, budgetOverlapCallGuardPrefix+budgetOverlapReportCall) {
			callGuarded = true
		}
		if strings.TrimSpace(code) == budgetOverlapReportCall {
			findings = append(findings, "the header calls "+budgetOverlapReportCall+" BARE — a check that could "+
				"not run then looks exactly like \"no neighbour found\", the silent failure this block exists to remove")
		}
	}
	if !callGuarded {
		findings = append(findings, "the header does not call "+budgetOverlapReportCall+" as the body of a NEGATED "+
			"`if` (it must be called from "+budgetOverlapHeaderFunc+", so a caller sees it before the build and the "+
			"suite, and the negation is what turns \"the check could not run\" into a REPORT instead of a header "+
			"that reads as \"no neighbour found\")")
	}
	// The reporter must not decide anything by itself: a refusal is banned (a
	// later run must never cancel an earlier one), so it may not exit, and it may
	// not write a shared artifact.
	for _, banned := range []string{"exit ", ">\"$stamp_file\"", ">\"$start_record\""} {
		if strings.Contains(body, banned) {
			findings = append(findings, fmt.Sprintf(
				"the concurrency reporter contains %q — it must REPORT and return (Rule 35: no new artifact, and "+
					"the epic's constraint: starting a run is never refused because a neighbour is running)", banned))
		}
	}
	// The liveness criterion is the process table, never the ppid (Rule 24): the
	// recorded pid's own /proc entry is what answers "is that run still here".
	for _, use := range []string{budgetOverlapMarkerUse, budgetOverlapAttributionUse, budgetOverlapStartsUse} {
		if !strings.Contains(body, use) {
			findings = append(findings, fmt.Sprintf(
				"the concurrency reporter never prints %s — the report would run and tell the reader nothing "+
					"(or would spell the sentence a second time instead of using the declared constant, Rule 1b)", use))
		}
	}
	if !strings.Contains(body, budgetOverlapSelfPidMarker) {
		findings = append(findings, "the concurrency report does not pass this run's own pid ("+
			budgetOverlapSelfPidMarker+") to the shared predicate — a recycled pid would then present this run as its "+
			"own neighbour, the one overlap that is always false")
	}
	return findings
}

// TestBudgetConcurrentOverlapIsReportedFromTheHeader is the STATIC half: the
// script declares the records glob, sources the shared predicate, defines the
// reporter and calls it (guarded) from the header.
func TestBudgetConcurrentOverlapIsReportedFromTheHeader(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, budgetScriptPath))
	if err != nil {
		t.Fatalf("read %s: %v", budgetScriptPath, err)
	}
	for _, f := range budgetOverlapFaults(string(raw)) {
		t.Errorf("%s: %s", budgetScriptPath, f)
	}
}

// TestBudgetOverlapGatePredicateIsFalsifiable is the RED PROOF, executed rather
// than asserted: the SAME predicate the real gate runs is fed mutated scripts and
// is required to report each one. Without it, "the gate passes" and "the gate
// cannot fail" are indistinguishable.
func TestBudgetOverlapGatePredicateIsFalsifiable(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, budgetScriptPath))
	if err != nil {
		t.Fatalf("read %s: %v", budgetScriptPath, err)
	}
	script := string(raw)
	if findings := budgetOverlapFaults(script); len(findings) != 0 {
		t.Fatalf("the REAL script already fails the concurrency predicate (%v) — every red proof below would be vacuous", findings)
	}

	// Mutation 1: the call is deleted — the pre-fix script, exactly (this is what
	// the measured defect looked like: the check simply was not there).
	if findings := budgetOverlapFaults(budgetOverlapDropCall(script)); len(findings) == 0 {
		t.Errorf("a script with the %s call removed passes the predicate — the gate cannot catch the silent "+
			"concurrent run it exists for", budgetOverlapReportCall)
	}

	// Mutation 2: the call is present but UNGUARDED. Under `set -u` an unguarded
	// `local x="$(failing)"` aborts the run on its own header — the fix would
	// then kill every budget run instead of reporting a neighbour.
	unguarded := strings.Replace(script,
		budgetOverlapCallGuardPrefix+budgetOverlapReportCall, budgetOverlapReportCall, 1)
	if unguarded == script {
		t.Fatalf("the guarded call (%q) was not found — the red proof's anchor rotted",
			budgetOverlapCallGuardPrefix+budgetOverlapReportCall)
	}
	if findings := budgetOverlapFaults(unguarded); len(findings) == 0 {
		t.Errorf("a BARE call passes the predicate — a check that could not run would then be indistinguishable " +
			"from \"no neighbour found\"")
	}

	// Mutation 3: the reporter stops printing the marker — the report exists,
	// runs, and tells the reader nothing.
	if findings := budgetOverlapFaults(budgetOverlapMutateReporter(t, script, budgetOverlapMarkerUse, `"SOMETHING ELSE"`)); len(findings) == 0 {
		t.Errorf("a report that never prints %q passes the predicate — the whole deliverable is that line",
			budgetOverlapMarker)
	}

	// Mutation 4: the caveat sentence goes away, leaving a bare "a neighbour
	// exists" line that reads like a host-load note and says nothing about what
	// the reader may believe of a red.
	if findings := budgetOverlapFaults(budgetOverlapMutateReporter(t, script, budgetOverlapAttributionUse, `"overlap detected"`)); len(findings) == 0 {
		t.Errorf("a report without the attribution caveat (%q) passes the predicate — a warning that does not say "+
			"what the overlap costs is a warning a reader learns to ignore", budgetOverlapAttributionNote)
	}
}

// budgetOverlapMutateReporter replaces old with new INSIDE the reporter's own
// body, so a mutation lands on the report rather than on a same-named echo
// elsewhere. It fails the test when the anchor is not found there, because a
// mutation that silently did nothing would make the red proof vacuous.
func budgetOverlapMutateReporter(t *testing.T, script, old, new string) string {
	t.Helper()
	body, ok := budgetFunctionBody(script, budgetOverlapReportCall)
	if !ok {
		t.Fatalf("no %s() body to mutate", budgetOverlapReportCall)
	}
	mutated := strings.Replace(body, old, new, 1)
	if mutated == body {
		t.Fatalf("the reporter's body does not contain %q — the red proof's anchor rotted", old)
	}
	return strings.Replace(script, body, mutated, 1)
}

// budgetOverlapDropCall removes the reporter's call from the header, leaving the
// definition in place (the shape a partial revert produces).
func budgetOverlapDropCall(script string) string {
	var out []string
	dropped := false
	for _, line := range strings.Split(script, "\n") {
		if !dropped && strings.Contains(line, budgetOverlapCallGuardPrefix+budgetOverlapReportCall) {
			dropped = true
			out = append(out, "# (removed for the red proof)")
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// budgetOverlapFixtureRecord writes one start record in the script's own shape.
func budgetOverlapFixtureRecord(t *testing.T, dir, pid, pkgs string) string {
	t.Helper()
	path := filepath.Join(dir, budgetOverlapRecordPrefix+pid+budgetOverlapRecordSuffix)
	body := fmt.Sprintf("--- start record: written BEFORE go test started (survives a kill) ---\n"+
		"start_iso=%s\nstart_epoch=1792000000\nhost=fixture\npid=%s\npkg=%s\nbudget=45m\nstream=%s%s.json\n",
		budgetOverlapPeerStart, pid, pkgs, budgetOverlapRecordPrefix, pid)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the fixture start record: %v", err)
	}
	return path
}

// budgetOverlapLivePeer starts a process whose command line NAMES the entry
// point, and returns its pid — the attribution half of liveness (a live pid is
// not a peer until its command line says so). The process is a bounded `sleep`,
// so nothing is generated and the arm costs one pid (Rule 27).
func budgetOverlapLivePeer(t *testing.T, dir string) int {
	t.Helper()
	// The name matters: /proc/<pid>/cmdline must mention test-budget.sh. Running a
	// file that carries that name reproduces the real shape (the entry point is
	// what a peer's command line holds) without executing a second budget run.
	script := filepath.Join(dir, budgetOverlapPeerName(t))
	if err := os.WriteFile(script, []byte("#!/bin/bash\nsleep "+budgetOverlapStubSeconds+"\n"), 0o755); err != nil {
		t.Fatalf("write the peer stub: %v", err)
	}
	cmd := exec.Command(budgetShellBin, script)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the peer stub: %v", err)
	}
	killed := false
	t.Cleanup(func() {
		if !killed {
			_ = cmd.Process.Kill()    //stylegate:ignore best-effort cleanup of the stub in a test cleanup; the assertions already ran
			_, _ = cmd.Process.Wait() //stylegate:ignore reaping the stub; its status is not the assertion
		}
	})
	return cmd.Process.Pid
}

// budgetOverlapPeerName is the file name a peer's command line must carry — the
// script's own entry-point name, read from the library so the two cannot drift.
func budgetOverlapPeerName(t *testing.T) string {
	t.Helper()
	name := budgetEntryPointNameForGate(t)
	if name == "" {
		t.Fatalf("the shared predicate declares no entry-point name — liveness attribution would be spelled twice")
	}
	return name
}

// budgetEntryPointNameForGate reads the entry-point name out of the shared
// library by sourcing it in the same interpreter the run uses (Rule 14: the
// library owns the spelling, this gate reads it rather than repeating it).
func budgetEntryPointNameForGate(t *testing.T) string {
	t.Helper()
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	out, err := exec.Command(budgetShellBin, "-c",
		`. "$1" && printf '%s\n' "$gopi_budget_entry_point_name"`,
		"bash", filepath.Join(root, filepath.FromSlash(budgetOverlapLib))).CombinedOutput()
	if err != nil {
		t.Fatalf("read the entry-point name from %s: %v\n%s", budgetOverlapLib, err, out)
	}
	return strings.TrimSpace(string(out))
}

// budgetOverlapEmptyCmdlinePeer finds a LIVE process on this host whose command
// line is READABLE and EMPTY — the shape the removed bug mis-read as a peer. It
// prefers pid 1 (no argv on Linux, so the shape does not depend on this kernel's
// thread inventory) and otherwise scans the process table. A fixture that cannot
// be found fails the test rather than skipping it: skips are listed, and a silent
// skip is a deleted test wearing a passing green.
func budgetOverlapEmptyCmdlinePeer(t *testing.T) int {
	t.Helper()
	// The SIZE of a procfs cmdline entry is ZERO for every process, whether it
	// carries an argv or not (measured: /proc/1/cmdline has size 0 and 54 bytes
	// of content). The empty shape is therefore decided by READING the file, the
	// same way the library reads it — never by stat.
	empty := func(pid string) bool {
		dir := filepath.Join(budgetOverlapProcPidDir, pid)
		content, err := os.ReadFile(filepath.Join(dir, budgetOverlapCmdlineName))
		if err != nil || len(content) != 0 {
			return false
		}
		// Empty AND still present: a process that is already gone is the DEAD
		// branch, which is a different arm.
		if _, err := os.Stat(filepath.Join(dir, budgetOverlapStatName)); err != nil {
			return false
		}
		return true
	}
	if empty(budgetOverlapKernelThreadPid) {
		pid, err := strconv.Atoi(budgetOverlapKernelThreadPid)
		if err != nil {
			t.Fatalf("parse the kernel-thread fixture pid %q: %v", budgetOverlapKernelThreadPid, err)
		}
		return pid
	}
	entries, err := os.ReadDir(budgetOverlapProcPidDir)
	if err != nil {
		t.Fatalf("scan %s for an empty-cmdline fixture: %v", budgetOverlapProcPidDir, err)
	}
	for _, entry := range entries {
		pid, convErr := strconv.Atoi(entry.Name())
		if convErr != nil || !empty(entry.Name()) {
			continue
		}
		return pid
	}
	t.Fatalf("no live process with a READABLE and EMPTY command line exists under %s — the kernel-thread arm "+
		"cannot be exercised on this host", budgetOverlapProcPidDir)
	return 0
}

// TestBudgetOverlapIgnoresAnEmptyCmdlinePeer is the KERNEL-THREAD arm, beside the
// dead-pid arm in TestBudgetOverlapPredicateNamesOnlyLiveIntersectingPeers:
//
//   - a live process with a READABLE, EMPTY command line is NOT a peer. An empty
//     command line is a MEASUREMENT (this process has no argv, so it can never be
//     the entry point), not a failure to measure — Rule 15 keeps the UNREADABLE
//     case, and that case is the `read < 0` branch, which still answers "dead".
//   - the SAME record under the PRE-FIX criterion is required to come back, and
//     the REAL sibling (a live peer whose command line names the entry point) is
//     required to stay reported through both. A red proof that also kills the
//     true neighbour would "fix" the bug by reporting nobody.
func TestBudgetOverlapIgnoresAnEmptyCmdlinePeer(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	dir := t.TempDir()
	emptyPid := budgetOverlapEmptyCmdlinePeer(t)
	emptyRecord := budgetOverlapFixtureRecord(t, dir, strconv.Itoa(emptyPid), budgetOverlapFixturePkg)
	livePid := budgetOverlapLivePeer(t, dir)
	liveRecord := budgetOverlapFixtureRecord(t, dir, strconv.Itoa(livePid), budgetOverlapFixturePkg)

	// peers runs the SHARED library through bash — never a Go re-implementation
	// (Rule 14) — and returns its rows.
	peers := func(libPath string, records ...string) [][]string {
		t.Helper()
		args := append([]string{"-c",
			`. "$1" && gopi_budget_live_peer_records "$2" "${@:3}"`, "bash", libPath,
			strconv.Itoa(os.Getpid())}, records...)
		out, runErr := exec.Command(budgetShellBin, args...).CombinedOutput()
		if runErr != nil {
			t.Fatalf("run the predicate (%s): %v\n%s", libPath, runErr, out)
		}
		var rows [][]string
		for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			if line == "" {
				continue
			}
			rows = append(rows, strings.Split(line, budgetOverlapFieldSep))
		}
		return rows
	}
	hasPid := func(rows [][]string, pid int) bool {
		want := strconv.Itoa(pid)
		for _, row := range rows {
			if len(row) > 0 && row[0] == want {
				return true
			}
		}
		return false
	}

	entryName := budgetOverlapPeerName(t)
	realLib := filepath.Join(root, filepath.FromSlash(budgetOverlapLib))
	rows := peers(realLib, emptyRecord, liveRecord)
	if hasPid(rows, emptyPid) {
		t.Errorf("pid %d has an EMPTY command line and was reported as a live peer — a process with no argv can "+
			"never be %s, and this is the reading that kept pid 2 (kthreadd) a peer for ever:\n%v",
			emptyPid, entryName, rows)
	}
	if !hasPid(rows, livePid) {
		t.Errorf("the REAL peer (pid %d, a live process whose command line names %s) was NOT reported — the "+
			"empty-command-line rule must not suppress a genuine neighbour:\n%v",
			livePid, entryName, rows)
	}

	// THE RED PROOF: restore the PRE-FIX criterion in a COPY of the real library
	// (the deliverable is never rewritten) and require the phantom back, with the
	// true neighbour still reported.
	raw, err := os.ReadFile(realLib)
	if err != nil {
		t.Fatalf("read %s: %v", budgetOverlapLib, err)
	}
	mutated := strings.Replace(string(raw),
		budgetOverlapReadGoneReturn, budgetOverlapReadGoneReturn+"\n            "+budgetOverlapEmptyCmdlineKeep, 1)
	if mutated == string(raw) {
		t.Fatalf("the library no longer carries %q — the red proof's anchor rotted",
			budgetOverlapReadGoneReturn)
	}
	mutatedPath := filepath.Join(dir, "budget-overlap-prefix.sh")
	if err := os.WriteFile(mutatedPath, []byte(mutated), 0o644); err != nil {
		t.Fatalf("write the mutated library copy: %v", err)
	}
	preRows := peers(mutatedPath, emptyRecord, liveRecord)
	if !hasPid(preRows, emptyPid) {
		t.Errorf("with the PRE-FIX criterion restored (%q) the empty-command-line pid %d is STILL not reported — "+
			"the arm cannot distinguish the fix from the bug, so it proves nothing:\n%v",
			budgetOverlapEmptyCmdlineKeep, emptyPid, preRows)
	}
	if !hasPid(preRows, livePid) {
		t.Errorf("the mutated library lost the REAL peer (pid %d) — the fixture, not the criterion, is what "+
			"changed:\n%v", livePid, preRows)
	}
}

// TestBudgetOverlapEmptyCmdlineIsAMeasurementNotAnUnknown pins the HALF that makes
// the fix a measurement rather than a blanket rule: the UNREADABLE case keeps its
// Rule 15 behaviour, so the fix cannot be "drop the record whenever the command
// line is not a match". The two outcomes are separate branches in the library.
func TestBudgetOverlapEmptyCmdlineIsAMeasurementNotAnUnknown(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(budgetOverlapLib)))
	if err != nil {
		t.Fatalf("read %s: %v", budgetOverlapLib, err)
	}
	lib := string(raw)
	if !strings.Contains(lib, budgetOverlapReadGoneReturn) {
		t.Errorf("%s no longer treats an UNREADABLE command line (%q) as the dead-process branch — the "+
			"empty-command-line fix must not be implemented by collapsing the two read outcomes",
			budgetOverlapLib, budgetOverlapReadGoneReturn)
	}
	if !strings.Contains(lib, budgetOverlapEmptyCmdlineKeep) {
		return // the fix under test: the keep-on-empty branch is gone
	}
	t.Errorf("%s still answers \"peer\" for a SUCCESSFUL read of a zero-length command line (%q): a process "+
		"with no argv can never be %s, so that branch keeps every kernel-thread record a live neighbour for ever",
		budgetOverlapLib, budgetOverlapEmptyCmdlineKeep, budgetOverlapPeerName(t))
}

// TestBudgetOverlapPredicateNamesOnlyLiveIntersectingPeers is the DYNAMIC half of
// the predicate: synthetic records are fed to the SHARED library through bash —
// the same interpreter the run uses, never a Go re-implementation (Rule 14) —
// and both directions are pinned:
//
//   - a live peer whose packages INTERSECT this run's is reported, with its pid,
//     its own packages and the shared packages;
//   - a live peer on DISJOINT packages is reported as a row with an EMPTY
//     intersection (so "we looked and were alone" and "we never looked" are
//     different observable states);
//   - a dead pid is not reported at all;
//   - this run's OWN pid is not reported (a recycled pid).
func TestBudgetOverlapPredicateNamesOnlyLiveIntersectingPeers(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	dir := t.TempDir()
	overlapPid := budgetOverlapLivePeer(t, dir)
	disjointPid := budgetOverlapLivePeer(t, dir)
	overlapRecord := budgetOverlapFixtureRecord(t, dir, strconv.Itoa(overlapPid), budgetOverlapFixturePkg)
	disjointRecord := budgetOverlapFixtureRecord(t, dir, strconv.Itoa(disjointPid), budgetOverlapDisjointPkg)
	deadRecord := budgetOverlapFixtureRecord(t, dir, budgetOverlapDeadPid, budgetOverlapFixturePkg)
	selfRecord := budgetOverlapFixtureRecord(t, dir, strconv.Itoa(os.Getpid()), budgetOverlapFixturePkg)

	scopeFile := filepath.Join(dir, "scope.txt")
	out, err := exec.Command(budgetShellBin, "-c",
		`. "$1" && . "$2" && gopi_scope_packages "$3" "$4" >"$5" && `+
			`gopi_budget_overlapping_runs "$3" "$5" "$6" "${@:7}"`,
		"bash", filepath.Join(root, filepath.FromSlash(budgetStreamScopeLib)),
		filepath.Join(root, filepath.FromSlash(budgetOverlapLib)),
		budgetOverlapFixtureModule, budgetOverlapFixturePkg, scopeFile,
		strconv.Itoa(os.Getpid()),
		overlapRecord, disjointRecord, deadRecord, selfRecord).CombinedOutput()
	if err != nil {
		t.Fatalf("run the overlap predicate: %v\n%s", err, out)
	}

	rows := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(rows) != 2 {
		t.Fatalf("the predicate returned %d row(s), want exactly 2 (the two LIVE peers; the dead pid and this "+
			"run's own pid are excluded):\n%s", len(rows), out)
	}
	overlapRow := overlapRowFor(t, rows, overlapPid)
	if got := budgetOverlapRowField(t, overlapRow, 2); got != budgetOverlapFixturePkg {
		t.Errorf("the live overlapping peer's own packages = %q, want %q", got, budgetOverlapFixturePkg)
	}
	if got := budgetOverlapRowField(t, overlapRow, 3); !strings.Contains(got, "smokegate") {
		t.Errorf("the overlap field = %q, want the shared package (the overlap the warning exists to name)", got)
	}
	if got := budgetOverlapRowField(t, overlapRow, 1); got != budgetOverlapPeerStart {
		t.Errorf("the overlapping peer's start time = %q, want %q", got, budgetOverlapPeerStart)
	}
	disjointRow := overlapRowFor(t, rows, disjointPid)
	if got := budgetOverlapRowField(t, disjointRow, 3); got != "" {
		t.Errorf("the DISJOINT peer reports overlap %q, want empty (its packages do not intersect this run's)", got)
	}
	// The pid assertions compare ROWS, never substrings: "0\t" is a substring of
	// every pid that ends in a zero ("1614320\t"), so a substring test here would
	// report a false failure (measured while writing this arm).
	for _, row := range rows {
		pid := budgetOverlapRowField(t, row, 0)
		if pid == budgetOverlapDeadPid {
			t.Errorf("a DEAD pid (%s) was reported as a live peer: %s", pid, row)
		}
		if pid == strconv.Itoa(os.Getpid()) {
			t.Errorf("this run's OWN pid was reported as a neighbour (a recycled pid is the one overlap that "+
				"is always false): %s", row)
		}
	}
}

// overlapRowFor returns the row whose first field is pid, failing the test when
// it is absent (an assertion against a row that does not exist must never read as
// a silent pass).
func overlapRowFor(t *testing.T, rows []string, pid int) string {
	t.Helper()
	want := strconv.Itoa(pid)
	for _, row := range rows {
		if budgetOverlapRowField(t, row, 0) == want {
			return row
		}
	}
	t.Fatalf("no row for pid %d in:\n%s", pid, strings.Join(rows, "\n"))
	return ""
}

// budgetOverlapRowField reads one tab-separated field of a peer row.
func budgetOverlapRowField(t *testing.T, row string, idx int) string {
	t.Helper()
	fields := strings.Split(row, budgetOverlapFieldSep)
	if idx >= len(fields) {
		t.Fatalf("row %q has %d field(s), want at least %d", row, len(fields), idx+1)
	}
	return fields[idx]
}

// TestBudgetRunWarnsWhenANeighbourOverlapsItsScope is the epic's acceptance,
// executed: the REAL script runs beside a LIVE neighbour whose start record asks
// for one of the same packages, and the warning must appear on the run's own
// stdout — read through a pipe WHILE the child is alive, because that is the
// measurement a caller's idle-output net performs.
func TestBudgetRunWarnsWhenANeighbourOverlapsItsScope(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	scratchHome := t.TempDir()
	budgetDir := filepath.Join(scratchHome, "queue", budgetConcurrentBudgetSubdir)
	if err := os.MkdirAll(budgetDir, 0o755); err != nil {
		t.Fatalf("create the fixture artifact directory: %v", err)
	}
	peerPid := budgetOverlapLivePeer(t, t.TempDir())
	budgetOverlapFixtureRecord(t, budgetDir, strconv.Itoa(peerPid), budgetOverlapFixturePkg)

	stubDir := t.TempDir()
	// A stub `go` that emits one package's events and then keeps working for a
	// bounded moment, so the header is read while the neighbour is still alive.
	stub := "#!/bin/bash\n" +
		"printf '%s\\n' '" + budgetObservableEventRun + "'\n" +
		"sleep " + budgetOverlapStubSeconds + "\n" +
		"printf '%s\\n' '" + budgetObservableEventPass + "'\n" +
		"printf '%s\\n' '" + budgetObservableEventDone + "'\n"
	module := budgetConcurrentModulePath(t, root)
	stub = strings.ReplaceAll(stub, "MODULEPKG", module+"/internal/smokegate")
	if err := os.WriteFile(filepath.Join(stubDir, budgetStubBinName), []byte(stub), 0o755); err != nil {
		t.Fatalf("write the stub go: %v", err)
	}

	cmd := exec.Command(budgetShellBin, filepath.Join(root, filepath.FromSlash(budgetScriptPath)), budgetOverlapFixturePkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		ScratchHomeEnvName+"="+scratchHome,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", budgetScriptPath, err)
	}

	// ORDERING, and it is not cosmetic: the scanner goroutine is the ONLY reader,
	// it closes the channel when the child's stdout reaches EOF, and `cmd.Wait()`
	// runs AFTER that. Waiting first closes the pipe under the scanner and yields
	// `read |0: file already closed` — a failure of the INSTRUMENT reading exactly
	// like a failure of the surface (measured while writing the sibling gate).
	var lines []string
	lineCh := make(chan string, 1)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			lineCh <- scanner.Text()
		}
		close(lineCh)
		readErr <- scanner.Err()
	}()
	deadline := time.After(budgetOverlapReadDeadline)
collect:
	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				break collect
			}
			lines = append(lines, line)
		case <-deadline:
			t.Fatalf("the run printed nothing about its live neighbour within %s; output so far:\n%s",
				budgetOverlapReadDeadline, strings.Join(lines, "\n"))
		}
	}
	if err := <-readErr; err != nil {
		t.Fatalf("reading the run's stdout failed: %v", err)
	}
	_ = cmd.Wait() //stylegate:ignore reaps the child; its exit status is not the assertion

	text := strings.Join(lines, "\n")
	if !strings.Contains(text, budgetOverlapMarker) {
		t.Fatalf("the run printed no %q line while a live neighbour held an overlapping start record:\n%s",
			budgetOverlapMarker, text)
	}
	for _, want := range []string{
		strconv.Itoa(peerPid),
		budgetOverlapStartsNote,
		budgetOverlapAttributionNote,
		budgetOverlapAttributionNoteEN,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the warning does not carry %q:\n%s", want, text)
		}
	}
	// The warning must be in the HEADER's half of the output, not buried after the
	// suite: a caller reading the top of the run must already know.
	headerAt := indexOfLine(lines, budgetObservableHeaderMarker)
	warnAt := indexOfLine(lines, budgetOverlapMarker)
	if headerAt < 0 || warnAt < 0 {
		t.Fatalf("the header (%d) or the warning (%d) never appeared:\n%s", headerAt, warnAt, text)
	}
	if warnAt < headerAt {
		t.Errorf("the warning precedes the run header (warning at %d, header at %d) — it belongs to the header, "+
			"where a caller is already looking", warnAt, headerAt)
	}
}

// indexOfLine returns the index of the first line containing needle, or -1.
func indexOfLine(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}
