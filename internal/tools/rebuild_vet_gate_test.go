package tools

// Post-merge GATE RE-RUN tests (rebuild_vet_gate.go).
//
// The acceptance is behavioural, not structural: a planted red gate must make
// the round REPORT the failure — gate name + file:line — through the close
// loop's existing notify channel, and a clean tree must be SILENT. The fixture
// is a real Go module compiled by the real toolchain (the gate's whole job is
// to run a subprocess and read its output, so a mocked toolchain would measure
// nothing), and the planted break is the class this gate exists for: a source
// file that does NOT COMPILE, which `go build` reports and `go vet` reports
// with the same file:line shape.
//
// The suite's TestMain already points the gate's shared build cache at a
// disk-backed sandbox (toolsPinBuildCacheSandbox) and the module cache
// resolver reads the host's populated one, so these tests exercise the real
// resolution path rather than a stubbed contract.

import (
	"encoding/json"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	// vgFixtureModulePath / vgFixtureGoMod / vgFixturePkgDir are the tiny Go
	// module the fixtures vet: one package, no dependencies — the gate must
	// never need the network.
	vgFixtureModulePath = "module vgfixture\n\ngo 1.21\n"
	vgFixtureGoMod      = "go.mod"
	vgFixturePkgDir     = "p"
	vgFixturePkgFile    = "p.go"
	// vgFixtureCleanBody is the GREEN tree: it vets.
	vgFixtureCleanBody = "package p\n\nimport \"fmt\"\n\n// F prints a literal.\nfunc F() { fmt.Println(\"ok\") }\n"
	// vgFixtureRedFile is the PLANTED RED: a file that does not compile — it
	// calls a symbol that is defined nowhere. Measured toolchain shape for
	// this break (Go 1.24.5): both `go build` and `go vet` reject it, and the
	// vet report carries the file:line.
	// vgFixtureRedBody is the file body; the call sits on line 4, which is the
	// position the report must carry.
	vgFixtureRedFile = "z_red.go"
	vgFixtureRedBody = "package p\n\n// Planted red: vgNeverDefinedSymbol exists nowhere in the module.\nfunc Broken() { vgNeverDefinedSymbol() }\n"
	// vgFixtureRedRef is the file:line the report MUST carry. It is spelled
	// as the fixture writes it (the function is on line 4 of the file above),
	// and the assertion searches for the file name and the position rather
	// than for a whole compiled message, so a toolchain that rewords its
	// diagnostic does not break the acceptance.
	vgFixtureRedFileRef  = vgFixtureRedFile
	vgFixtureRedLineText = ":4:"
	// vgFixtureSlowCommand / vgFixtureSlowTimeoutSec drive the BOUND arm: a
	// command that outlives its budget must ABSTAIN (a timeout is not a red
	// tree), and the whole call must return within a few multiples of that
	// budget. A HOST LOAD GENERATOR IS NEVER THE INSTRUMENT (Rule 27): the
	// subject measured here is the gate's own timeout, and the slow command is
	// the gate's own subprocess.
	vgFixtureSlowCommand        = "sleep 30"
	vgFixtureSlowTimeoutSec     = 1
	vgFixtureTimeoutCeilingSec  = 20
	vgFixtureSettingsFile       = "settings.json"
	vgFixtureSettingsPermission = 0o644
)

// vgFixtureRepo makes a sandbox Go module on master with the CLEAN body
// committed, and declares its build contract in settings.json so both halves of
// the close loop are applicable (the build is a no-op success: this suite's
// subject is the gate, and the rebuild engine has its own tests).
func vgFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, vgFixturePkgDir), 0o755); err != nil {
		t.Fatal(err)
	}
	vgWriteFile(t, repo, vgFixtureGoMod, vgFixtureModulePath)
	vgWriteFile(t, repo, filepath.Join(vgFixturePkgDir, vgFixturePkgFile), vgFixtureCleanBody)
	vetGateFixtureGit(t, repo, "init", "-q", "-b", "master")
	vetGateFixtureGit(t, repo, "config", "user.email", "t@t")
	vetGateFixtureGit(t, repo, "config", "user.name", "t")
	vetGateFixtureGit(t, repo, "add", vgFixtureGoMod, filepath.Join(vgFixturePkgDir, vgFixturePkgFile))
	vetGateFixtureGit(t, repo, "commit", "-q", "-m", "fixture: a clean module")

	settingsPath := filepath.Join(t.TempDir(), vgFixtureSettingsFile)
	body, err := json.Marshal(map[string]any{
		// "true" is the POSIX no-op success: a declared build that succeeds
		// and touches nothing, so the close loop reaches the gate with the
		// tree exactly as the fixture left it.
		"repos": map[string]any{repo: map[string]any{"build": "true", "build_timeout_sec": 60}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, body, vgFixtureSettingsPermission); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPI_SETTINGS", settingsPath)
	t.Setenv("GOPI_HOME", t.TempDir())
	return repo
}

// vgWriteFile writes rel inside repo (creating parents).
func vgWriteFile(t *testing.T, repo, rel, body string) {
	t.Helper()
	path := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(rel), err)
	}
	if err := os.WriteFile(path, []byte(body), vgFixtureSettingsPermission); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// vgGit runs git in repo (the fixture's repo construction).
func vetGateFixtureGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out := rebuildTestGit(t, repo, args...)
	return out
}

// vgNotifySink points the notify pipeline at a file the test reads back, and
// subscribes it to the close loop's failure event — the SAME subscription an
// operator already has (the gate reports through the existing channel, so no
// reconfiguration is part of this contract).
func vgNotifySink(t *testing.T) string {
	t.Helper()
	eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+eventsFile)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", BuildFailedEvent)
	return eventsFile
}

// TestPostMergeVetGate_PlantedRedReportsGateAndFileLine is the EPIC's
// acceptance: a planted red gate must make the round REPORT the failure (the
// gate's name and the offending file:line) instead of reporting success.
//
// The plant is a real compile break in a real module, and the assertion is on
// the DELIVERED notify payload — not on an internal field — because "reported"
// is the whole deliverable: a verdict nobody receives is the silent success
// this gate exists to remove.
func TestPostMergeVetGate_PlantedRedReportsGateAndFileLine(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo := vgFixtureRepo(t)
	eventsFile := vgNotifySink(t)
	// PLANT the red gate: a file that does not compile, on the tree the round
	// just closed on.
	vgWriteFile(t, repo, filepath.Join(vgFixturePkgDir, vgFixtureRedFile), vgFixtureRedBody)

	res := runPostMergeVetGate(repo)

	if res.Status != mergeBuildGateStatusFail {
		t.Fatalf("planted red gate must FAIL, got status=%s reason=%q", res.Status, res.Reason)
	}
	if res.Gate != vetGateName {
		t.Fatalf("the verdict must name the gate %q, got %q", vetGateName, res.Gate)
	}
	// The verdict carries the position: the file the plant wrote, at the line
	// the break is on.
	if len(res.Findings) == 0 {
		t.Fatalf("no finding parsed out of the gate output: %q", res.OutputTail)
	}
	first := res.Findings[0]
	if !strings.Contains(first.File, vgFixtureRedFileRef) {
		t.Fatalf("the reported finding must name the planted file %q, got %q", vgFixtureRedFileRef, first.String())
	}
	if !strings.Contains(first.RefFull(), vgFixtureRedLineText) {
		t.Fatalf("the reported finding must name the planted line (%s), got %q", vgFixtureRedLineText, first.RefFull())
	}

	// The REPORT itself: the notify payload must carry the gate name and the
	// file:line, and must NOT read as a success.
	payload := vgReadEvents(t, eventsFile)
	if payload == "" {
		t.Fatal("a red gate delivered NO notify event — the round reported nothing (the defect this gate closes)")
	}
	for _, want := range []string{BuildFailedEvent, vetGateName, vgFixtureRedFileRef, vgFixtureRedLineText} {
		if !strings.Contains(payload, want) {
			t.Fatalf("notify payload must carry %q, got: %s", want, payload)
		}
	}
	if strings.Contains(payload, "gate passed") {
		t.Fatalf("the payload reads as a pass: %s", payload)
	}

	// The summary is the ONE rendering: the same sentence the payload carries
	// says FAILED, names the gate and the position.
	summary := res.Summary()
	for _, want := range []string{vetGateName, vgFixtureRedFileRef, vgFixtureRedLineText} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary must carry %q, got: %s", want, summary)
		}
	}
	if res.ErrorCount == 0 {
		t.Fatal("a red gate must count at least one measured finding")
	}
}

// TestPostMergeVetGate_CleanTreeIsSilent is the counterpart acceptance: on a
// clean tree the gate runs, reports PASS, and delivers NO notify event — a gate
// that alarms on every round is a gate an operator learns to ignore.
func TestPostMergeVetGate_CleanTreeIsSilent(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo := vgFixtureRepo(t)
	eventsFile := vgNotifySink(t)

	res := runPostMergeVetGate(repo)

	if res.Status != mergeBuildGateStatusPass {
		t.Fatalf("a clean tree must PASS, got status=%s reason=%q findings=%v", res.Status, res.Reason, res.Findings)
	}
	if data := vgReadEvents(t, eventsFile); data != "" {
		t.Fatalf("a clean round must deliver NO event, got: %s", data)
	}
	if res.ErrorCount != 0 {
		t.Fatalf("a clean tree must count no diagnostics, got %d", res.ErrorCount)
	}
}

// TestPostMergeVetGate_ReportsThroughTheCloseLoop is the WIRING acceptance: the
// gate is reached from the close loop itself (RebuildAfterMerge), not only when
// called directly — a gate that exists but is never invoked is the "reports
// success while the gate is red" defect wearing a new name.
func TestPostMergeVetGate_ReportsThroughTheCloseLoop(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo := vgFixtureRepo(t)
	eventsFile := vgNotifySink(t)
	vgWriteFile(t, repo, filepath.Join(vgFixturePkgDir, vgFixtureRedFile), vgFixtureRedBody)

	res := RebuildAfterMerge(repo)

	if res.Vet == nil {
		t.Fatal("the close loop result carries no gate verdict — the gate is not wired into RebuildAfterMerge")
	}
	if !res.Vet.Failed() {
		t.Fatalf("the close loop must report the planted red gate, got status=%s reason=%q", res.Vet.Status, res.Vet.Reason)
	}
	payload := vgReadEvents(t, eventsFile)
	if !strings.Contains(payload, vgFixtureRedFileRef) {
		t.Fatalf("the close loop delivered no file:line report, got: %s", payload)
	}
}

// TestPostMergeVetGate_RedIsReportOnlyNeverARollback pins Rule 12's half of the
// contract: the gate REPORTS, it never changes the close loop's own verdict.
// The build succeeded and the binary follows master — a red gate is evidence,
// not grounds to unwind a good merge.
func TestPostMergeVetGate_RedIsReportOnlyNeverARollback(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo := vgFixtureRepo(t)
	vgNotifySink(t)
	vgWriteFile(t, repo, filepath.Join(vgFixturePkgDir, vgFixtureRedFile), vgFixtureRedBody)

	res := RebuildAfterMerge(repo)

	if !res.OK {
		t.Fatalf("the declared build succeeded, so the close loop's own verdict must stay OK, got %+v (err=%q)", res, res.Err)
	}
	if res.Err != "" {
		t.Fatalf("a red GATE must not become the close loop's error: %q", res.Err)
	}
	if !res.Vet.Failed() {
		t.Fatalf("the gate verdict must still be reported alongside the OK build, got %s", res.Vet.Status)
	}
}

// TestPostMergeVetGate_NotAModuleAbstains: a repo that is not a Go module has
// no declared way to be vetted — the gate ABSTAINS with its reason and reports
// nothing, rather than inventing a command (Rule 12's declared-not-discovered
// rule).
func TestPostMergeVetGate_NotAModuleAbstains(t *testing.T) {
	repo := t.TempDir()
	eventsFile := vgNotifySink(t)

	res := runPostMergeVetGate(repo)

	if res.Status != mergeBuildGateStatusAbstain {
		t.Fatalf("a non-module repo must ABSTAIN, got %s", res.Status)
	}
	if res.Reason == "" {
		t.Fatal("an abstention must state its reason (Rule 15b: never a silent nothing)")
	}
	if data := vgReadEvents(t, eventsFile); data != "" {
		t.Fatalf("an abstention must not alarm, got: %s", data)
	}
}

// TestPostMergeVetGate_UnparsableTimeoutAbstains: an operator's malformed
// budget is an abstention that NAMES the variable, never a silent fallback to
// the default (Rule 15b) — and never a red round for a number the gate could
// not read.
func TestPostMergeVetGate_UnparsableTimeoutAbstains(t *testing.T) {
	repo := vgFixtureRepo(t)
	vgNotifySink(t)
	t.Setenv(vetGateTimeoutEnvName, "soon")

	res := runPostMergeVetGate(repo)

	if res.Status != mergeBuildGateStatusAbstain {
		t.Fatalf("an unparsable budget must ABSTAIN, got %s", res.Status)
	}
	if !strings.Contains(res.Reason, vetGateTimeoutEnvName) {
		t.Fatalf("the reason must name the variable, got %q", res.Reason)
	}
}

// TestPostMergeVetGate_TimeoutIsBoundedAndAbstains is the BOUND arm: the run is
// capped by the gate's own named budget, a command that outlives it ABSTAINS
// (a timeout is not a red tree), and the call RETURNS — the merge/verify path
// must never be held hostage by a hung toolchain.
//
// The instrument is the gate's OWN subprocess (vgFixtureSlowCommand), driven
// through the spec the resolver produces: no host load is generated (Rule 27).
func TestPostMergeVetGate_TimeoutIsBoundedAndAbstains(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo := t.TempDir()
	spec := vetGateSpec{
		Argv:        []string{"sh", "-c", vgFixtureSlowCommand},
		TimeoutSec:  vgFixtureSlowTimeoutSec,
		GoCacheDir:  t.TempDir(),
		ModCacheDir: t.TempDir(),
	}
	start := time.Now()
	out := vetGateExecute(repo, spec)
	elapsed := time.Since(start)

	if !out.TimedOut {
		t.Fatalf("the slow command must hit the budget: %+v", out)
	}
	if out.Abstain == "" {
		t.Fatal("a timeout must ABSTAIN with a reason, never red a tree")
	}
	if elapsed > vgFixtureTimeoutCeilingSec*time.Second {
		t.Fatalf("the gate must return at its budget: took %s for a %ds budget (ceiling %ds)",
			elapsed, vgFixtureSlowTimeoutSec, vgFixtureTimeoutCeilingSec)
	}
	if !strings.Contains(out.Abstain, "budget") {
		t.Fatalf("the abstention must say the budget was exceeded, got %q", out.Abstain)
	}
}

// TestPostMergeVetGate_DefaultBudgetIsTheNamedConstant pins Rule 1a: with no
// override the budget IS the named constant — the value an operator reads in
// the source is the value the gate runs with.
func TestPostMergeVetGate_DefaultBudgetIsTheNamedConstant(t *testing.T) {
	got, err := vetGateTimeout()
	if err != nil {
		t.Fatalf("the default budget must resolve: %v", err)
	}
	if got != vetGateDefaultTimeoutSec {
		t.Fatalf("default budget = %d, want the named constant %d", got, vetGateDefaultTimeoutSec)
	}
	t.Setenv(vetGateTimeoutEnvName, "42")
	got, err = vetGateTimeout()
	if err != nil {
		t.Fatalf("the override must resolve: %v", err)
	}
	if got != 42 {
		t.Fatalf("override = %d, want 42", got)
	}
}

// TestPostMergeVetGate_DisabledByTheEscapeHatch pins the operator's lever: the
// one value turns the gate off (an unforeseen false report must always be one
// env var away from gone), and nothing is reported.
func TestPostMergeVetGate_DisabledByTheEscapeHatch(t *testing.T) {
	repo := vgFixtureRepo(t)
	eventsFile := vgNotifySink(t)
	vgWriteFile(t, repo, filepath.Join(vgFixturePkgDir, vgFixtureRedFile), vgFixtureRedBody)
	t.Setenv(vetGateDisabledEnvName, vetGateDisabledValue)

	res := runPostMergeVetGate(repo)

	if res.Status != mergeBuildGateStatusAbstain {
		t.Fatalf("the escape hatch must turn the gate into an abstention, got %s", res.Status)
	}
	if data := vgReadEvents(t, eventsFile); data != "" {
		t.Fatalf("a disabled gate must be silent, got: %s", data)
	}
}

// TestVetGateParseFindings_RealOutputShapes pins the parser against the output
// shapes MEASURED from the toolchain on this host, so a future reword is
// visible here rather than in a silently empty report:
//
//	stderr "p/p.go:7:2: fmt.Printf format %d has arg s of wrong type string"
//	stderr "vet: p/z.go:4:2: undefined: vgNeverDefinedSymbol"
//	stdout "FAIL\tvgfixture/p [build failed]"
//	stderr "# vgfixture/p"
func TestVetGateParseFindings_RealOutputShapes(t *testing.T) {
	stderr := "# vgfixture/p\n" +
		"p/p.go:7:2: fmt.Printf format %d has arg s of wrong type string\n" +
		"vet: p/z.go:4:2: undefined: vgNeverDefinedSymbol\n"
	stdout := "FAIL\tvgfixture/p [build failed]\nFAIL\n"
	findings, total := vetGateParseFindings(stdout, stderr)
	if total != 2 {
		t.Fatalf("measured 2 diagnostics, got %d (%v)", total, findings)
	}
	if findings[0].Ref() != "p/p.go:7" || findings[0].Col != "2" {
		t.Fatalf("first finding position = %q col=%q, want p/p.go:7 col 2", findings[0].Ref(), findings[0].Col)
	}
	if !strings.Contains(findings[0].Message, "wrong type") {
		t.Fatalf("first finding message = %q", findings[0].Message)
	}
	// The "vet: " prefix is dropped, so the file is a file and not "vet:".
	if findings[1].File != "p/z.go" || findings[1].Line != "4" {
		t.Fatalf("second finding = %q, want p/z.go:4", findings[1].RefFull())
	}
	if got := vetGateCountPackages(stdout); got != 1 {
		t.Fatalf("package verdict count = %d, want 1 (the FAIL line, not the bare 'FAIL' terminator)", got)
	}
	// The empty-module announcement is recognised, so an empty repo ABSTAINS
	// instead of alarming: measured text with exit 1.
	if !strings.Contains("go: warning: \"./...\" matched no packages\nno packages to vet\n", vetGateNoPackagesMarker) {
		t.Fatal("the empty-module marker must match the toolchain's own announcement")
	}
	// A package banner is never a finding; prose is never a finding.
	for _, line := range []string{"# vgfixture/p", "FAIL\tvgfixture/p [build failed]", "no packages to vet", "", "FAIL"} {
		if f, ok := vetGateDiagnostic(line); ok {
			t.Fatalf("line %q must not parse as a diagnostic, got %+v", line, f)
		}
	}
}

// TestVetGateFinding_RefRendersFileAndLine pins the reference contract the
// acceptance reads: Ref is "file:line" and RefFull adds the column only when
// the toolchain printed one.
func TestVetGateFinding_RefRendersFileAndLine(t *testing.T) {
	withCol := vetGateFinding{File: "a/b.go", Line: "12", Col: "7", Message: "x"}
	if withCol.Ref() != "a/b.go:12" {
		t.Fatalf("Ref = %q, want a/b.go:12", withCol.Ref())
	}
	if withCol.RefFull() != "a/b.go:12:7" {
		t.Fatalf("RefFull = %q, want a/b.go:12:7", withCol.RefFull())
	}
	noCol := vetGateFinding{File: "a/b.go", Line: "12", Message: "x"}
	if noCol.RefFull() != "a/b.go:12" {
		t.Fatalf("RefFull without a column = %q, want a/b.go:12", noCol.RefFull())
	}
}

// vgReadEvents reads the notify sink ("" when the file does not exist — the
// silent case is a legitimate outcome for the clean/abstention fixtures).
func vgReadEvents(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read the notify sink %s: %v", path, err)
	}
	return string(data)
}
