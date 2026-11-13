package testutil

// The START-RECORD half of the budget entry point's artifact contract: a run's
// existence and its stream name must be durable BEFORE `go test` starts, so a
// KILLED run is queryable instead of orphaned.
//
// WHY THIS FILE EXISTS — the measured defect (2026-10-13, task 01a12f82). The
// script writes its per-run stream from the FIRST byte, but every artifact that
// NAMES it — the stamps.log line, the last.json pointer, the printed summary —
// is produced after `go test` RETURNS. A delivery run that was killed mid-suite
// (10 minutes of zero output looked indistinguishable from a hang) therefore left
// `run-1791789379-784665.json` holding REAL verdict events — `fail
// internal/standards 150.298` plus seven named failing tests — while stamps.log
// carried no line for it and last.json never pointed at it. The verdict
// survived; the query that would find it did not: recovering it took matching
// the job's jsonl TIMESTAMP against the file's epoch, i.e. forensic
// re-derivation, and in the meantime the run's own report quoted a different,
// later, filtered run as "the final suite". That is Rule 15's silent failure at
// the level of the RUN, not of a test.
//
// THE FIX these gates pin: the run writes a START RECORD — a flat
// `run-<epoch>-<pid>-start.txt` beside its stream, carrying start/pid/host/pkg/
// budget/stream — on the line before the `go test` redirect. It survives
// SIGKILL by construction (complete on disk before `go test` is exec'd, nothing
// removes it) and it is NOT a second stamp: a line is appended to stamps.log on
// COMPLETION and only on completion, so "is there a stamps.log line for this
// run?" still answers "did it finish?" (Rule 14: the two jobs stay in two
// places, and the completed-run stamp contract is untouched — the third test
// below asserts exactly that).
//
// BOTH HALVES ARE CHECKED, because either one alone is satisfiable by the
// other's absence: the static half reads the SCRIPT (the write precedes the
// redirect, on an executable line), and the dynamic half RUNS the real script
// with a blocking stub `go` and SIGKILLs the run's process group — the epic's
// acceptance (a), executed. The red proof drives the same pure predicate with
// the write REMOVED, so the gate's own verdict is shown to be falsifiable
// rather than asserted to be.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// budgetStartRecordSuffix is the script's `start_record_suffix`: the flat
	// name a run's start record carries beside its stream.
	budgetStartRecordSuffix = "-start.txt"
	// budgetStartRecordWriteCall is the function the script must CALL on an
	// executable line, before the redirect.
	budgetStartRecordWriteCall = "write_start_record"
	// budgetStartRecordStreamLabel is the field inside the record that makes the
	// orphaned stream FINDABLE — the whole point of the file.
	budgetStartRecordStreamLabel = "stream="
	// budgetStartRecordPathAssignment is how the record's path is derived: from
	// the run's own tag, under the run's own artifact directory (never a shared
	// name — a second run must not be able to overwrite a first one's record).
	budgetStartRecordPathAssignment = `start_record="$budget_dir/$run_tag$start_record_suffix"`
	// budgetStartRecordTagSource is the per-run tag the record's name must come
	// from, shared with the stream's own name.
	budgetStartRecordTagSource = `run_tag="$raw_json_prefix${start_epoch}-$$"`
	// budgetStartRecordSuffixConst is the declared constant (Rule 1a) the path
	// assignment must reference rather than repeat the literal.
	budgetStartRecordSuffixConst = `start_record_suffix="` + budgetStartRecordSuffix + `"`
	// budgetStartRecordHeading is the record's first line: a reader who finds
	// the file cold must be able to tell which side of the run wrote it.
	budgetStartRecordHeading = "--- start record: written BEFORE go test started (survives a kill) ---"
	// budgetStartRecordScopeField is the field carrying the requested scope — a
	// reader of an orphaned run needs what it was asked to run.
	budgetStartRecordScopeField = `pkg=`
	// budgetGoTestRedirect is the redirect whose line the record must PRECEDE.
	budgetGoTestRedirect = `>"$raw_json"`
	// budgetKillStubPauseSeconds is how long the stub `go` blocks, in seconds.
	// It bounds the kill window and nothing else: the test kills the run as soon
	// as the record appears, so this is a ceiling, not a wait (and it is a
	// `sleep`, not load — Rule 27).
	budgetKillStubPauseSeconds = "20"
	// budgetStartRecordPollDeadline bounds how long the dynamic test waits for
	// the record to appear before declaring the fix absent.
	budgetStartRecordPollDeadline = 10 * time.Second
	// budgetStartRecordPollInterval is the poll step while waiting.
	budgetStartRecordPollInterval = 50 * time.Millisecond
	// budgetStampLineFields is the COMPLETED-run stamp contract, in order. It is
	// asserted (not merely read) so a start record cannot quietly become a
	// second, differently-shaped stamp: the shape below is what every reader and
	// report already quotes.
	// budgetStampLinePkgField is the stamp line's scope field — asserted so a start record cannot
	// quietly become a second, differently-shaped stamp.
	budgetStampLinePkgField = "pkg="
	// budgetStartRecordStampFile is the variable the start record must NOT write:
	// appending there is what "a started run" and "a finished run" being the same
	// line would look like.
	budgetStartRecordStampFile = `"$stamp_file"`
)

// budgetStampLineFields is the completed-run stamp's field set, in order.
var budgetStampLineFields = []string{
	"host=", "pkg=", "budget=", "elapsed=", "load_start=", "load_end=", "cores=", "exit=", "outcome=", "stream=",
}

// budgetStartRecordFaults is the gate's PURE predicate: given the script's text,
// return one finding per way the start-record contract is unmet. Pure (no
// filesystem, no environment) so the red proof can feed it a mutated script and
// drive the SAME decider the real gate uses, instead of a copy of its logic.
//
// The record's OWN fields are checked in the writer's BODY, not across the whole
// script: `stream=` and `pkg=` occur elsewhere (the stamp line carries both), so
// a file-wide substring test would stay green while the record itself stopped
// naming anything — a gate that cannot fail.
func budgetStartRecordFaults(script string) []string {
	var findings []string
	for _, want := range []string{
		budgetStartRecordSuffixConst,
		budgetStartRecordHeading,
		budgetStartRecordPathAssignment,
		budgetStartRecordWriteCall + "()",
		budgetStartRecordTagSource,
	} {
		if !strings.Contains(script, want) {
			findings = append(findings, fmt.Sprintf("the script no longer contains %q", want))
		}
	}
	body, ok := budgetFunctionBody(script, budgetStartRecordWriteCall)
	if !ok {
		findings = append(findings, "the script no longer DEFINES "+budgetStartRecordWriteCall+"()")
	} else {
		// The record must name the STREAM — a record that omits it leaves the
		// orphaned stream exactly as unfindable as before the fix.
		if !strings.Contains(body, budgetStartRecordStreamLabel) {
			findings = append(findings, "the start record's own body no longer carries the stream name ("+
				budgetStartRecordStreamLabel+")")
		}
		// The record must carry the requested scope: an orphaned stream whose
		// scope is unknown cannot be quoted as evidence for anything.
		if !strings.Contains(body, budgetStartRecordScopeField) {
			findings = append(findings, "the start record's own body no longer carries the requested scope ("+
				budgetStartRecordScopeField+")")
		}
		// The completed-run contract: the start record must not append to the
		// stamp file. If it did, "a run started" and "a run finished" would
		// become the same line and "did it finish?" would lose its answer.
		if strings.Contains(body, budgetStartRecordStampFile) {
			findings = append(findings, "the start record writes to the completed-run stamp file ("+
				budgetStartRecordStampFile+") — a started run and a finished run must stay different lines")
		}
	}
	call, redirect := -1, -1
	for i, line := range strings.Split(script, "\n") {
		code := strings.TrimSpace(stripComment(line))
		if code == "" {
			continue
		}
		if call < 0 && strings.HasPrefix(code, budgetStartRecordWriteCall) && !strings.Contains(code, "()") {
			call = i
		}
		if redirect < 0 && strings.Contains(code, "go test") && strings.Contains(code, budgetGoTestRedirect) {
			redirect = i
		}
	}
	if call < 0 {
		findings = append(findings, "the script never CALLS "+budgetStartRecordWriteCall+" on an executable line "+
			"(a commented-out call satisfies a substring test while the record is never written)")
	}
	if redirect < 0 {
		findings = append(findings, "no `go test … >\"$raw_json\"` redirect found — the gate is looking at the wrong shape (vacuous pass guard)")
	}
	if call >= 0 && redirect >= 0 && call > redirect {
		findings = append(findings, fmt.Sprintf(
			"the start record is written AFTER `go test` starts (call at line %d, redirect at line %d) — a run killed in between would still be orphaned",
			call+1, redirect+1))
	}
	return findings
}

// budgetFunctionBody returns the text of `name() { … }`, closed at the first
// line that is exactly `}`. It is deliberately simple: the predicate needs the
// function's own body, and a brace-counting parser would be a second
// implementation of the shell for no benefit.
func budgetFunctionBody(script, name string) (string, bool) {
	lines := strings.Split(script, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), name+"()") {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	for j := start; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "}" {
			return strings.Join(lines[start:j+1], "\n"), true
		}
	}
	return strings.Join(lines[start:], "\n"), true
}

// TestBudgetStartRecordGatePredicateIsFalsifiable is the RED PROOF, executed
// rather than asserted: the SAME predicate the real gate runs is fed a script
// with the start-record write REMOVED, and is required to report it. Without
// this, "the gate passes" and "the gate cannot fail" are indistinguishable.
func TestBudgetStartRecordGatePredicateIsFalsifiable(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, budgetScriptPath))
	if err != nil {
		t.Fatalf("read %s: %v", budgetScriptPath, err)
	}
	script := string(raw)

	if findings := budgetStartRecordFaults(script); len(findings) != 0 {
		t.Fatalf("the REAL script already fails the start-record predicate (%v) — every red proof below would be vacuous", findings)
	}

	// Mutation 1: the call is deleted (the pre-fix script, exactly).
	mutated := budgetRemoveLine(script, budgetStartRecordWriteCall)
	if findings := budgetStartRecordFaults(mutated); len(findings) == 0 {
		t.Errorf("a script with the %s CALL removed passes the predicate — the gate cannot catch the "+
			"orphaned-stream defect it exists for", budgetStartRecordWriteCall)
	}

	// Mutation 2: the record is written AFTER the redirect (present, called, and
	// useless for a killed run) — the shape a careless "fix" would take.
	reordered := budgetMoveCallAfterRedirect(script, budgetStartRecordWriteCall)
	if findings := budgetStartRecordFaults(reordered); len(findings) == 0 {
		t.Errorf("a script that writes the start record AFTER `go test` starts passes the predicate — " +
			"the ordering is the whole property (a run killed in between stays orphaned)")
	}

	// Mutation 3: the record stops naming the stream. The mutation is applied
	// INSIDE the writer's body, which is where the predicate reads it: `stream=`
	// also occurs in the stamp line further down, so replacing it file-wide would
	// mutate the completed-run stamp instead of the record and the check would
	// look unfalsifiable (measured while writing this gate — that is exactly what
	// the first version did).
	unnamed := budgetMutateWriterBody(t, script, budgetStartRecordStreamLabel, "streamname=")
	if findings := budgetStartRecordFaults(unnamed); len(findings) == 0 {
		t.Errorf("a start record that does not name the stream passes the predicate — the stream is " +
			"what must become findable")
	}

	// Mutation 4: the start record also appends to the completed-run stamp file:
	// that is how "a run started" and "a run finished" would become one line.
	stamped := budgetMutateWriterBody(t, script, budgetStartRecordStreamLabel,
		budgetStartRecordStreamLabel+"\n        printf 'x\\n' >>"+budgetStartRecordStampFile)
	if findings := budgetStartRecordFaults(stamped); len(findings) == 0 {
		t.Errorf("a start record that also appends to stamps.log passes the predicate — the completed-run " +
			"stamp contract would then be satisfied by a run that never finished")
	}
}

// budgetMutateWriterBody replaces old with new INSIDE the start-record writer's
// own body, so a mutation lands on the record rather than on a same-shaped
// occurrence elsewhere in the script. It fails the test when the anchor is not
// found there, because a mutation that silently did nothing would make the red
// proof vacuous (the failure mode this whole file exists to avoid).
func budgetMutateWriterBody(t *testing.T, script, old, new string) string {
	t.Helper()
	body, ok := budgetFunctionBody(script, budgetStartRecordWriteCall)
	if !ok {
		t.Fatalf("no %s() body to mutate", budgetStartRecordWriteCall)
	}
	mutated := strings.Replace(body, old, new, 1)
	if mutated == body {
		t.Fatalf("the writer's body does not contain %q — the red proof's anchor rotted", old)
	}
	return strings.Replace(script, body, mutated, 1)
}

// budgetRemoveLine deletes the first executable line that mentions needle.
func budgetRemoveLine(script, needle string) string {
	var out []string
	removed := false
	for _, line := range strings.Split(script, "\n") {
		code := strings.TrimSpace(stripComment(line))
		if !removed && strings.HasPrefix(code, needle) {
			removed = true
			out = append(out, "# (removed for the red proof)")
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// budgetMoveCallAfterRedirect relocates the first executable call of name to
// just after the `go test` redirect line: the record exists, is called, and
// cannot help a run killed before `go test` returns.
func budgetMoveCallAfterRedirect(script, name string) string {
	lines := strings.Split(script, "\n")
	callAt, redirectAt := -1, -1
	for i, line := range lines {
		code := strings.TrimSpace(stripComment(line))
		if callAt < 0 && strings.HasPrefix(code, name) && !strings.Contains(code, "()") {
			callAt = i
		}
		if redirectAt < 0 && strings.Contains(code, "go test") && strings.Contains(code, budgetGoTestRedirect) {
			redirectAt = i
		}
	}
	if callAt < 0 || redirectAt < 0 {
		return script
	}
	call := lines[callAt]
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		switch {
		case i == callAt:
			out = append(out, "# (moved for the red proof)")
		case i == redirectAt:
			out = append(out, line, call)
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// TestBudgetStartRecordIsReadableBeforeGoTestIsInvoked is the STATIC half: the
// write is an executable call that precedes the redirect, and the record is
// derived from the run's own tag under the run's own directory.
func TestBudgetStartRecordIsReadableBeforeGoTestIsInvoked(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, budgetScriptPath))
	if err != nil {
		t.Fatalf("read %s: %v", budgetScriptPath, err)
	}
	for _, f := range budgetStartRecordFaults(string(raw)) {
		t.Errorf("%s: %s — a run killed before `go test` returns must be queryable from its own "+
			"artifact directory (Rule 15: an unnameable stream is a lost verdict)",
			budgetScriptPath, f)
	}
}

// TestBudgetKilledRunLeavesAStartRecordNamingItsStream is the DYNAMIC half and
// the epic's acceptance (a), executed inside the suite: the REAL script runs
// with a stub `go` that blocks, the run's whole process group is SIGKILLed
// mid-flight, and what survives must name the stream the kill orphaned — while
// nothing claims the run finished.
//
// A stub `go` is what makes this cheap and deterministic: the property is the
// ARTIFACT that appears before `go test` is invoked, not what the packages do.
// The kill is a real SIGKILL to the run's process group (Rule 31: the surface is
// executed, and the failure mode it must survive is the one that produced the
// defect).
func TestBudgetKilledRunLeavesAStartRecordNamingItsStream(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	scratchHome := t.TempDir()
	blocking := budgetStartRecordWriteBlockingGoStub(t, t.TempDir())
	out, _ := os.CreateTemp(t.TempDir(), "killed-run-*.out") //stylegate:ignore temp file inside t.TempDir(); a create failure surfaces as a nil handle the write path below fails on loudly

	cmd := exec.Command(budgetShellBin, filepath.Join(root, budgetScriptPath), "./internal/smokegate/")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		ScratchHomeEnvName+"="+scratchHome,
		"PATH="+blocking+string(os.PathListSeparator)+os.Getenv("PATH"),
		budgetConcurrentGoStubEnv+"="+budgetConcurrentModulePath(t, root),
	)
	cmd.Stdout, cmd.Stderr = out, out
	// Its OWN process group, so the kill takes the script AND the stub `go`:
	// that is what a parent job's SIGKILL to a run looks like from here.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the budget run: %v", err)
	}
	pgid := cmd.Process.Pid
	killed := false
	defer func() {
		if !killed {
			_ = syscall.Kill(-pgid, syscall.SIGKILL) //stylegate:ignore best-effort group cleanup in a test defer; the assertion already ran
			_ = cmd.Wait()                           //stylegate:ignore reap the child started above
		}
	}()

	budgetDir := filepath.Join(scratchHome, "queue", budgetConcurrentBudgetSubdir)
	record := budgetStartRecordAwait(t, budgetDir)
	// The kill happens at the moment the record exists — i.e. as early as the
	// contract claims it is durable, and long before the run could finish.
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL the run's process group %d: %v", pgid, err)
	}
	killed = true
	_ = cmd.Wait() //stylegate:ignore reaps the killed child; its exit status is not the assertion

	if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
		t.Fatalf("the killed run's process %d is still alive", pgid)
	}

	// (1) The record survives the SIGKILL — it is written before `go test`, and
	// nothing removes it on the way out.
	body, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the start record %s did not survive the kill: %v — a killed run's stream is orphaned again", record, err)
	}
	// (2) It names the stream, and that stream EXISTS in the same directory: the
	// record is a pointer to real evidence, not a note about an intention.
	stream := budgetStartRecordField(t, string(body), budgetStartRecordStreamLabel)
	if !strings.HasPrefix(stream, budgetConcurrentStreamPrefix) || !strings.HasSuffix(stream, rawJSONSuffixForGate) {
		t.Errorf("the start record names the stream %q, want a per-run %s*%s name",
			stream, budgetConcurrentStreamPrefix, rawJSONSuffixForGate)
	}
	streamPath := filepath.Join(budgetDir, stream)
	if _, err := os.Stat(streamPath); err != nil {
		t.Errorf("the start record names %s, which does not exist in the same directory: %v", stream, err)
	}
	// (3) It carries the run's identity and its scope — the two facts a reader of
	// an orphaned run needs to quote it (pid/start, and what was asked to run).
	for _, want := range []string{"start_epoch=", "start_iso=", "pid=", "host=", budgetStartRecordScopeField, "budget="} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the start record does not carry %q:\n%s", want, body)
		}
	}
	if !strings.Contains(string(body), budgetStartRecordHeading) {
		t.Errorf("the start record does not open with its heading %q — a reader arriving cold cannot tell "+
			"which side of the run wrote it", budgetStartRecordHeading)
	}
	// (4) NOTHING claims the run finished: no stamps.log line, no last.json
	// pointer. This is the discrimination the whole fix preserves — "started" and
	// "finished" must never be the same evidence.
	if stampPath := filepath.Join(budgetDir, "stamps.log"); budgetFileHasStream(t, stampPath, stream) {
		t.Errorf("%s carries a line for the KILLED run's stream %s — a start record must not be written as "+
			"a completion stamp (the completed-run contract, Rule 14)", stampPath, stream)
	}
	latest := filepath.Join(budgetDir, budgetLatestName)
	if target, err := filepath.EvalSymlinks(latest); err == nil && filepath.Base(target) == stream {
		t.Errorf("%s points at the killed run's stream %s — only a COMPLETED run may be pointed at", budgetLatestName, stream)
	}
}

// rawJSONSuffixForGate is the script's `raw_json_suffix`, repeated as the gate's
// expectation (the same convention as the concurrent-stream gate above).
const rawJSONSuffixForGate = ".json"

// budgetStartRecordWriteBlockingGoStub writes a `go` stub that passes any
// NON-test invocation through to the real `go` (the budget run builds this
// tree's CLI before the suite starts) and answers a TEST with one stream line
// followed by a bounded block — so the caller can SIGKILL a run that is
// genuinely mid-suite. The block is a bounded `sleep`: no load is generated
// (Rule 27).
//
// The pass-through is what makes the stub usable here at all: a stub that
// swallowed the build would fail the run's own CLI build, and the record would
// never be written for a reason that has nothing to do with the contract under
// test (measured while writing this gate: the first version returned "success"
// for the build and produced no record).
func budgetStartRecordWriteBlockingGoStub(t *testing.T, dir string) string {
	t.Helper()
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no real `go` on PATH to pass the build through to: %v", err)
	}
	script := "#!/bin/bash\n" +
		"# Stands in for `go test -json`: the BUILD passes through to the real `go`,\n" +
		"# while a TEST is answered with one stream line and then a bounded block.\n" +
		"module=\"${" + budgetConcurrentGoStubEnv + ":?the gate must supply the module path}\"\n" +
		"if [ \"${1:-}\" != \"test\" ]; then exec " + realGo + " \"$@\"; fi\n" +
		"pkg=\"\"\n" +
		"for a in \"$@\"; do case \"$a\" in ./*|*/*) pkg=\"$a\";; esac; done\n" +
		"import_path=\"$module/${pkg#./}\"\n" +
		"import_path=\"${import_path%/}\"\n" +
		"printf '{\"Action\":\"start\",\"Package\":\"%s\"}\\n' \"$import_path\"\n" +
		"sleep " + budgetKillStubPauseSeconds + "\n" +
		"exit 0\n"
	path := filepath.Join(dir, "go")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write blocking go stub: %v", err)
	}
	return dir
}

// budgetStartRecordAwait polls the budget directory until exactly one start
// record appears, and fails with the directory listing if none does.
func budgetStartRecordAwait(t *testing.T, budgetDir string) string {
	t.Helper()
	deadline := time.Now().Add(budgetStartRecordPollDeadline)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(budgetDir)
		if err == nil {
			var found []string
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), budgetStartRecordSuffix) {
					found = append(found, e.Name())
				}
			}
			if len(found) == 1 {
				return filepath.Join(budgetDir, found[0])
			}
			if len(found) > 1 {
				sort.Strings(found)
				t.Fatalf("the run left %d start records (%v) — the record's name must be per-run, or two "+
					"concurrent runs share one write target", len(found), found)
			}
		}
		time.Sleep(budgetStartRecordPollInterval) //stylegate:allow-sleep poll step of a bounded wait for the child suite's own start record to appear in the budget directory; the loop's deadline bounds the wait, and the step only keeps the re-read cheap
	}
	entries, _ := os.ReadDir(budgetDir) //stylegate:ignore lists a directory the test populated; the entries are the assertion
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	t.Fatalf("no run's start record (a *%s file) appeared in %s within %s — a run killed before `go test` "+
		"returns leaves its stream unnameable (found: %v)",
		budgetStartRecordSuffix, budgetDir, budgetStartRecordPollDeadline, names)
	return ""
}

// budgetStartRecordField reads one `key=value` line out of a start record. A
// missing field is a failure, never an empty string: the record's fields are the
// contract.
func budgetStartRecordField(t *testing.T, body, key string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, key) {
			return strings.TrimSpace(strings.TrimPrefix(line, key))
		}
	}
	t.Fatalf("the start record carries no %q line:\n%s", key, body)
	return ""
}

// budgetFileHasStream reports whether the named file carries a completion line
// for stream. A missing file is "no line" — that is the wanted verdict for a
// killed run, so it must not be an error here.
func budgetFileHasStream(t *testing.T, path, stream string) bool {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(body), "stream="+stream+"\n") || strings.HasSuffix(strings.TrimSpace(string(body)), "stream="+stream)
}

// TestBudgetCompletedRunStillStampsIdentically pins the OTHER half of the epic's
// acceptance (b): the completed-run contract is UNCHANGED — a finished run still
// appends exactly one stamp line with the same field set, still publishes the
// latest pointer as a symlink to its stream, and still prints the summary block.
// A start record must be an ADDITION, never a replacement.
func TestBudgetCompletedRunStillStampsIdentically(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	scratchHome := t.TempDir()
	stubDir := budgetConcurrentWriteGoStub(t, t.TempDir())

	cmd := exec.Command(budgetShellBin, filepath.Join(root, budgetScriptPath), "./internal/smokegate/")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		ScratchHomeEnvName+"="+scratchHome,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		budgetConcurrentGoStubEnv+"="+budgetConcurrentModulePath(t, root),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the completed run failed: %v\n%s", err, out)
	}

	budgetDir := filepath.Join(scratchHome, "queue", budgetConcurrentBudgetSubdir)

	// (1) Exactly one stamp line, carrying the declared field set IN ORDER — the
	// shape every reader and report already quotes.
	stampPath := filepath.Join(budgetDir, "stamps.log")
	raw, err := os.ReadFile(stampPath)
	if err != nil {
		t.Fatalf("a COMPLETED run left no %s: %v — the stamp contract is broken", stampPath, err)
	}
	lines := nonEmptyLines(string(raw))
	if len(lines) != 1 {
		t.Fatalf("%s carries %d lines for ONE completed run, want exactly 1:\n%s", stampPath, len(lines), raw)
	}
	last := -1
	for _, field := range budgetStampLineFields {
		at := strings.Index(lines[0], field)
		if at < 0 {
			t.Errorf("the completed run's stamp line no longer carries %q:\n%s", field, lines[0])
			continue
		}
		if at < last {
			t.Errorf("the stamp line's %q field moved out of order — the completed-run stamp shape must stay "+
				"byte-shaped as it was:\n%s", field, lines[0])
		}
		last = at
	}
	if !strings.Contains(lines[0], "outcome=pass") || !strings.Contains(lines[0], "exit=0") {
		t.Errorf("the stamp line does not report the run's own outcome:\n%s", lines[0])
	}
	stream := budgetStampField(t, lines[0], "stream=")

	// (2) The latest pointer is still a SYMLINK to this completed run's stream.
	latest := filepath.Join(budgetDir, budgetLatestName)
	fi, err := os.Lstat(latest)
	if err != nil {
		t.Fatalf("%s was not published for a completed run: %v", budgetLatestName, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is a %s, not a symlink", budgetLatestName, fi.Mode().Type())
	}
	if target, err := filepath.EvalSymlinks(latest); err != nil || filepath.Base(target) != stream {
		t.Errorf("%s resolves to %q (err=%v), want the completed run's stream %s", budgetLatestName, target, err, stream)
	}

	// (3) The summary block still prints, and still names the stream it rendered
	// from — the reader's route to the evidence.
	for _, want := range []string{"--- summary (", budgetConcurrentRawJSONLabel, "stamp: "} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the completed run's output no longer carries %q:\n%s", want, out)
		}
	}

	// (4) The start record is ALSO there for the completed run: a reader gets one
	// file per run from START, and the stamp is the completion added later.
	entries, err := os.ReadDir(budgetDir)
	if err != nil {
		t.Fatalf("read %s: %v", budgetDir, err)
	}
	var records []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), budgetStartRecordSuffix) {
			records = append(records, e.Name())
		}
	}
	if len(records) != 1 {
		t.Errorf("a completed run left %d start record(s) (%v), want exactly 1 — the record is written per run "+
			"and keeps the run's scope and stream name", len(records), records)
	}
}

// budgetStampField reads one `key=value` field out of a stamp line. A missing
// field is a failure, never an empty string: the stamp's fields are the contract.
func budgetStampField(t *testing.T, line, key string) string {
	t.Helper()
	at := strings.Index(line, key)
	if at < 0 {
		t.Fatalf("the stamp line carries no %q field:\n%s", key, line)
	}
	rest := line[at+len(key):]
	if cut := strings.IndexAny(rest, " \n"); cut >= 0 {
		rest = rest[:cut]
	}
	return strings.TrimSpace(rest)
}

// nonEmptyLines splits body into its non-blank lines.
func nonEmptyLines(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
