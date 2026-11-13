package testutil

// testbudget_refusal_inline_gate_test.go — THE INLINE HALF of the stale-binary
// refusal: the runner must mark a refusing test ON ITS OWN failing line, and must
// print the conclusion ABOVE the failure list.
//
// THE DEFECT THIS PINS (measured 2026-10-16, task 01a14161). The run started
// 11:48:30, the tested binary was stamped 11:48:37, and the tree's own commit
// fc074a8d1 landed at 11:48:34 — three seconds before the build. Eight of that
// run's failing tests were therefore the CLI e2e guards' STALE-TESTED-BINARY
// REFUSAL: no test body ran for any of them. The failures block printed all eight
// with the marker a reader (and an agent) reads as a code verdict —
//
//	--- FAIL: <test>  (<pkg>)
//
// — and the sentence that corrected it ("never ran a test body") arrived only in
// the STALE-TESTED-BINARY REFUSALS block BELOW the list. The rows were then
// reported as code reds/flake, which is a defect of the REPORT: a reader who
// greps the failures block cannot be relied on to reach a trailing paragraph.
//
// WHAT THE FIX IS (and what it deliberately is not). Each refusing test's marker
// line now carries refusal_inline_marker after the package, and a lead line
// (failures_refusals_lead) with the refusal count is printed BEFORE the list. The
// refusal's own verdict is untouched: the STALE-TESTED-BINARY REFUSALS block still
// prints below with the guard's verbatim VERDICT, the artifact, and the runner's
// design note — this change only stops the rows from being MISPRESENTED while the
// reader is still looking at them.
//
// WHY IT IS TESTED THIS WAY. The readers that decide WHICH tests refused are the
// shared shell predicates (scripts/lib/stream-failures.sh, Rule 14) — this file
// never re-reads the JSON. The end-to-end arms run the REAL script
// (scripts/test-budget.sh) against a stub `go`, exactly as
// testbudget_failures_gate_test.go and testbudget_run_condition_gate_test.go do:
// cheap, deterministic, no test bodies, no load (Rule 27).
//
// THE RED PROOF. The shipped script is run beside a copy whose three rendering
// constants are emptied — the PRE-FIX rendering by construction — and the arm
// asserts the distinction is absent there and present in the shipped script.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	// budgetRefusalInlineFixturePkgTail is the package half of the fixture's
	// stream; the module prefix is the REPO's (read at run time), so the run's own
	// out-of-scope check stays silent.
	budgetRefusalInlineFixturePkgTail = "/cmd/agent"
	// budgetRefusalInlineRefusingName is the test whose output carries the guard's
	// sentence — the row that must be marked; …OtherName fails for an unrelated
	// reason and is the control: the distinction must not swallow ordinary reds.
	budgetRefusalInlineRefusingName = "TestRefusesAStaleBinary"
	budgetRefusalInlineOtherName    = "TestFailsForAGenuineAssertion"
	// budgetRefusalInlineSignature is the guard's refusal signature as the shell
	// side spells it (scripts/lib/stream-failures.sh's stream_freshness_signature).
	// The EXPECTATION is read from the library at run time; this constant is the
	// fixture's content, and the arm asserts the library still agrees with it.
	budgetRefusalInlineSignature = "this test drives the BUILT binary, so it would verify code that is not in that binary."
	// budgetRefusalInlineRefusals is how many distinct refusals the fixture
	// carries — the number the lead line substitutes into its `%s`.
	budgetRefusalInlineRefusals = "1"
	// budgetRefusalInlineMarkerConst / …LeadConst / …LeadNoteConst are the script's
	// own constants: the gate READS them (Rule 13) rather than copying the text, so
	// a reword cannot leave the gate asserting a string no longer printed.
	budgetRefusalInlineMarkerConst   = "refusal_inline_marker"
	budgetRefusalInlineLeadConst     = "failures_refusals_lead"
	budgetRefusalInlineLeadNoteConst = "failures_refusals_lead_note"
	// budgetRefusalInlineMarkerLine is what every failing test is rendered with —
	// the shape `go test` prints, kept so a reader still recognises the row.
	budgetRefusalInlineMarkerLine = "--- FAIL: "
)

// budgetRefusalInlineStream is the fixture stream: ONE test refusing with the
// guard's sentence (plus its `--- FAIL:` marker row and its fail event), and ONE
// test failing for an unrelated reason.
func budgetRefusalInlineStream(module string) string {
	pkg := module + budgetRefusalInlineFixturePkgTail
	refusal := "    x_test.go:1: gopi (mtime 2026-10-16T11:48:37+08:00) is OLDER than clock.go — " + budgetRefusalInlineSignature
	row := func(action, test, output string) string {
		if output == "" {
			return `{"Action":"` + action + `","Package":"` + pkg + `","Test":"` + test + `"}`
		}
		return `{"Action":"` + action + `","Package":"` + pkg + `","Test":"` + test + `","Output":"` + output + `\n"}`
	}
	return strings.Join([]string{
		`{"Action":"start","Package":"` + pkg + `"}`,
		row("run", budgetRefusalInlineRefusingName, ""),
		row("output", budgetRefusalInlineRefusingName, refusal),
		row("output", budgetRefusalInlineRefusingName, budgetRefusalInlineMarkerLine+budgetRefusalInlineRefusingName+" (0.01s)"),
		`{"Action":"fail","Package":"` + pkg + `","Test":"` + budgetRefusalInlineRefusingName + `","Elapsed":0.01}`,
		row("run", budgetRefusalInlineOtherName, ""),
		row("output", budgetRefusalInlineOtherName, "    other_test.go:4: got 1, want 2"),
		row("output", budgetRefusalInlineOtherName, budgetRefusalInlineMarkerLine+budgetRefusalInlineOtherName+" (0.02s)"),
		`{"Action":"fail","Package":"` + pkg + `","Test":"` + budgetRefusalInlineOtherName + `","Elapsed":0.02}`,
		`{"Action":"fail","Package":"` + pkg + `","Elapsed":0.05}`,
	}, "\n") + "\n"
}

// budgetRefusalInlineRevert is the PRE-FIX rendering, by construction: the three
// constants the fix introduced are emptied in a copy of the SHIPPED script, so
// what the red arm runs is the shipped bytes minus the fix — never a paraphrase.
func budgetRefusalInlineRevert(t *testing.T, root, script string) string {
	t.Helper()
	raw, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read %s: %v", script, err)
	}
	text := string(raw)
	for _, name := range []string{
		budgetRefusalInlineMarkerConst,
		budgetRefusalInlineLeadConst,
		budgetRefusalInlineLeadNoteConst,
	} {
		re := regexp.MustCompile(`(?m)^` + name + `="[^"]*"`)
		if !re.MatchString(text) {
			t.Fatalf("%s declares no %s=… — the red proof cannot revert what is not there", budgetScriptPath, name)
		}
		text = re.ReplaceAllString(text, name+`=""`)
	}
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("make %s: %v", scriptsDir, err)
	}
	// The script resolves its shared libraries relative to its OWN directory
	// (`$root/scripts/lib/…`), so the copy needs the same neighbour or the
	// missing-library refusal fires before any rendering runs (its own correct
	// behaviour, just not what this proof measures).
	if err := os.Symlink(filepath.Join(root, "scripts", "lib"), filepath.Join(scriptsDir, "lib")); err != nil {
		t.Fatalf("link the shared libraries beside the pre-fix copy: %v", err)
	}
	preFix := filepath.Join(scriptsDir, filepath.Base(script))
	if err := os.WriteFile(preFix, []byte(text), 0o755); err != nil {
		t.Fatalf("write the pre-fix copy: %v", err)
	}
	return preFix
}

// budgetRefusalInlineRun runs `bash <script> ./cmd/agent/` with a stub `go` in
// front of the PATH. The stub builds nothing and answers `go test` with the
// fixture stream, so the run reaches its real failure-reporting path.
func budgetRefusalInlineRun(t *testing.T, root, script string) string {
	t.Helper()
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}
	module := repoModulePath(t, root)
	stream := budgetRefusalInlineStream(module)
	for _, want := range []string{budgetRefusalInlineSignature} {
		if !strings.Contains(stream, want) {
			t.Fatalf("the fixture stream does not carry %q", want)
		}
	}
	// The library's own signature is READ, so the fixture and the predicate cannot
	// drift apart silently (Rule 13/14: one value, two readers). The stream is
	// retargeted to the repo's module for the same reason the other budget gates
	// do it: the run's out-of-scope check must stay silent.
	lib := budgetSiteReadFile(t, budgetFailuresLib)
	if !strings.Contains(lib, `stream_freshness_signature="`+budgetRefusalInlineSignature+`"`) {
		t.Fatalf("%s no longer declares stream_freshness_signature=%q — this arm's fixture would stop standing "+
			"in for the refusal the runner recognises", budgetFailuresLib, budgetRefusalInlineSignature)
	}
	stubDir := t.TempDir()
	scratchHome := t.TempDir()
	streamPath := filepath.Join(t.TempDir(), "refusal-inline-stream.json")
	if err := os.WriteFile(streamPath, []byte(stream), 0o644); err != nil {
		t.Fatalf("write the fixture stream: %v", err)
	}
	stub := "#!/bin/bash\n" +
		"# Stands in for `go`: a build succeeds (no artifact needed — the freshness\n" +
		"# reader classifies the STREAM, not the binary), and `go test` answers with the\n" +
		"# fixture stream and the failing status a real red run returns.\n" +
		"for a in \"$@\"; do\n" +
		"    if [ \"$a\" = \"build\" ]; then\n" +
		"        out=\"\"\n" +
		"        while [ $# -gt 0 ]; do case \"$1\" in -o) out=\"$2\";; esac; shift; done\n" +
		"        [ -n \"$out\" ] && : >\"$out\"\n" +
		"        exit 0\n" +
		"    fi\n" +
		"done\n" +
		"cat <<'REFUSAL_INLINE_STREAM'\n" + stream + "REFUSAL_INLINE_STREAM\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(stubDir, testbudgetRunConditionGoBin), []byte(stub), 0o755); err != nil {
		t.Fatalf("write the stub go: %v", err)
	}
	cmd := exec.Command(budgetShellBin, script, "./cmd/agent/")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		ScratchHomeEnvName+"="+scratchHome,
		"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	out, _ := cmd.CombinedOutput() //stylegate:ignore the exit code is asserted through the report text, which is the signal under test
	return string(out)
}

// budgetRefusalInlineMarker is the script's inline marker, read from the script.
func budgetRefusalInlineMarker(t *testing.T, script string) string {
	t.Helper()
	marker := budgetReportPipeStringConst(t, script, budgetRefusalInlineMarkerConst)
	if strings.TrimSpace(marker) == "" {
		t.Fatalf("%s: %s is empty — a failing test would be marked with nothing, which is the defect of "+
			"2026-10-16 unchanged", budgetScriptPath, budgetRefusalInlineMarkerConst)
	}
	return marker
}

// budgetRefusalInlinePreFixFromRevision is the SECOND red arm, and it is
// REVISION-ANCHORED rather than constructed: the script as it stood at the
// revision that PREDATES the fix is read straight out of the object store
// (`git show <parent>:scripts/test-budget.sh`) — no mutation of the working tree,
// no regex, the exact bytes that shipped before. It returns "" when the
// introducing commit cannot be resolved (an uncommitted edit during development),
// which the caller reports as a skip naming the reason rather than passing
// silently.
//
// WHY BOTH ARMS. The constructed arm answers "is the DISTINCTION the thing being
// measured" (it holds everything else constant); this one answers "was the
// DISTINCTION actually absent before" (it measures the shipped artifact, not a
// reverted copy). Either alone leaves one half of the claim untested.
func budgetRefusalInlinePreFixFromRevision(t *testing.T, root, scriptRel string) string {
	t.Helper()
	introducing, err := exec.Command(gitExecutable, "-C", root, "log", "--format=%H", "--max-count=1",
		"-S", budgetRefusalInlineMarkerConst, "--", scriptRel).Output()
	if err != nil {
		return ""
	}
	rev := strings.TrimSpace(string(introducing))
	if rev == "" {
		return ""
	}
	parent, err := exec.Command(gitExecutable, "-C", root, "rev-parse", rev+"^").Output()
	if err != nil {
		return ""
	}
	blob, err := exec.Command(gitExecutable, "-C", root, "show",
		strings.TrimSpace(string(parent))+":"+scriptRel).Output()
	if err != nil {
		return ""
	}
	text := string(blob)
	if strings.Contains(text, budgetRefusalInlineMarkerConst+"=") &&
		!strings.Contains(text, budgetRefusalInlineMarkerConst+`=""`) {
		t.Fatalf("the revision BEFORE the fix already declares %s=… — the red proof cannot measure a distinction "+
			"that was already there", budgetRefusalInlineMarkerConst)
	}
	dir := t.TempDir()
	scriptsDir := filepath.Join(dir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("make %s: %v", scriptsDir, err)
	}
	if err := os.Symlink(filepath.Join(root, "scripts", "lib"), filepath.Join(scriptsDir, "lib")); err != nil {
		t.Fatalf("link the shared libraries beside the pre-fix revision's script: %v", err)
	}
	path := filepath.Join(scriptsDir, filepath.Base(scriptRel))
	if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
		t.Fatalf("write the pre-fix revision's script: %v", err)
	}
	return path
}

// TestTestBudgetRefusalIsMarkedInlineAndItsConclusionHoisted is the epic's
// acceptance arm: the REAL script must (a) mark the refusing test's own failing
// line and NOT the genuinely-failing control's, and (b) print the refusal
// conclusion ABOVE the failure list — and the PRE-FIX rendering of the same
// script must fail both assertions (the red proof).
func TestTestBudgetRefusalIsMarkedInlineAndItsConclusionHoisted(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	script := filepath.Join(root, filepath.FromSlash(budgetScriptPath))
	shipped := budgetSiteReadFile(t, budgetScriptPath)
	marker := budgetRefusalInlineMarker(t, shipped)
	lead := budgetReportPipeStringConst(t, shipped, budgetRefusalInlineLeadConst)
	if !strings.Contains(lead, "%s") {
		t.Fatalf("%s: %s=%q carries no %%s — the count would be printed by nobody", budgetScriptPath,
			budgetRefusalInlineLeadConst, lead)
	}
	leadWithCount := strings.Replace(lead, "%s", budgetRefusalInlineRefusals, 1)
	pkg := repoModulePath(t, root) + budgetRefusalInlineFixturePkgTail

	report := budgetRefusalInlineRun(t, root, script)

	// (a) INLINE, on the refusing row.
	refusedLine := budgetRefusalInlineMarkerLine + budgetRefusalInlineRefusingName + "  (" + pkg + ")  " + marker
	if !strings.Contains(report, refusedLine) {
		t.Errorf("the refusing test's own failing line is not marked — a reader who never reaches the summary "+
			"still cannot tell a refusal from a code verdict (want %q):\n%s", refusedLine, report)
	}
	// (b) THE CONTROL: a genuine red keeps the plain marker, so the distinction
	// cannot pass by marking everything.
	plainLine := budgetRefusalInlineMarkerLine + budgetRefusalInlineOtherName + "  (" + pkg + ")"
	if !strings.Contains(report, plainLine) {
		t.Errorf("the genuinely failing test (%s) is not rendered as a plain %s line:\n%s",
			budgetRefusalInlineOtherName, budgetRefusalInlineMarkerLine, report)
	}
	if strings.Contains(report, plainLine+"  "+marker) {
		t.Errorf("the genuinely failing test was marked as a refusal — the distinction swallowed an ordinary red, "+
			"which is worse than the defect it fixes:\n%s", report)
	}
	// (c) THE CONCLUSION BEFORE THE LIST: a `grep FAIL` reader cannot count a
	// refusal as a defect without meeting the count first.
	leadAt := strings.Index(report, leadWithCount)
	firstFailAt := strings.Index(report, budgetRefusalInlineMarkerLine)
	if leadAt < 0 {
		t.Fatalf("the report does not carry the refusal lead line %q — the conclusion is still only below the "+
			"list:\n%s", leadWithCount, report)
	}
	if firstFailAt < 0 || leadAt > firstFailAt {
		t.Errorf("the refusal conclusion (at %d) does not precede the failure list (first %s at %d): the block "+
			"that corrects the rows must be read BEFORE them:\n%s",
			leadAt, budgetRefusalInlineMarkerLine, firstFailAt, report)
	}
	// (d) THE REFUSAL VERDICT ITSELF IS UNTOUCHED: the block below still prints,
	// with the guard's own sentence and the artifact it refused.
	if !strings.Contains(report, freshnessHeadingMarker) {
		t.Errorf("the inline fix removed the %s block — the fix must correct the PRESENTATION, never weaken the "+
			"refusal's own verdict:\n%s", freshnessHeadingMarker, report)
	}
	if !strings.Contains(report, budgetRefusalInlineSignature) {
		t.Errorf("the report no longer carries the guard's own refusal sentence — the reader is asked to take the "+
			"marker on trust:\n%s", report)
	}

	// (e) THE RED PROOF: the same script with the fix reverted must fail (a) and
	// (c). A red proof that cannot go red proves nothing about the shipped bytes.
	reverted := budgetRefusalInlineRun(t, root, budgetRefusalInlineRevert(t, root, script))
	if strings.Contains(reverted, refusedLine) {
		t.Fatalf("the PRE-FIX rendering already marks the refusing row — the arm is not measuring the fix:\n%s", reverted)
	}
	if strings.Contains(reverted, leadWithCount) {
		t.Fatalf("the PRE-FIX rendering already prints the hoisted conclusion:\n%s", reverted)
	}
	// …and the reverted copy still renders the rows the same way, so the red arm
	// is the RENDERING and not a broken run.
	if !strings.Contains(reverted, plainLine) {
		t.Fatalf("the PRE-FIX rendering does not even render the control's marker line — the red arm is not "+
			"measuring the rendering difference:\n%s", reverted)
	}

	// (f) THE SECOND RED ARM, REVISION-ANCHORED: the script read straight out of
	// the object store at the revision BEFORE the fix — the shipped pre-fix bytes,
	// nothing constructed — must also fail both halts. The constructed arm above
	// holds everything but the distinction constant; this one measures the
	// artifact that actually shipped. The FIX FILE ITSELF is never mutated, so
	// there is nothing to restore: the delivered file's identity is asserted
	// against the commit instead (by content hash, not by an index-relative diff).
	if preFix := budgetRefusalInlinePreFixFromRevision(t, root, budgetScriptPath); preFix != "" {
		atRevision := budgetRefusalInlineRun(t, root, preFix)
		if strings.Contains(atRevision, refusedLine) {
			t.Errorf("the script at the revision BEFORE the fix already marks the refusing row:\n%s", atRevision)
		}
		if strings.Contains(atRevision, leadWithCount) {
			t.Errorf("the script at the revision BEFORE the fix already prints the hoisted conclusion:\n%s", atRevision)
		}
		if !strings.Contains(atRevision, plainLine) {
			t.Errorf("the script at the revision BEFORE the fix does not render the control's marker line — "+
				"that arm is not measuring the rendering difference:\n%s", atRevision)
		}
	} else {
		t.Logf("the revision-anchored red arm did not run: the fix's introducing commit could not be resolved " +
			"from git (uncommitted work in progress). The constructed arm above still ran.")
	}
}
