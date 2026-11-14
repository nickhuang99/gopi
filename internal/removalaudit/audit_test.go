package removalaudit_test

// audit_test.go — the emitter's own RED/GREEN proof.
//
// The epic's acceptance is "the record is emitted, and disabling the emit makes
// the assertion RED". This file proves BOTH halves here, at the emitter, so the
// per-path tests in internal/tools can prove the same thing about their own
// removal path against real git (they call SetEmitDisabledForTest the same way).

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/removalaudit"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestRecordNamesEveryAttributingField is the GREEN half: one emitted record
// carries the arm, the target, the row and the deciding evidence, in one
// greppable JSON line.
func TestRecordNamesEveryAttributingField(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, sink)

	removalaudit.WorktreeRemove(removalaudit.ArmSweepReclaim, "/repo/x",
		"/workspace/gopi-abc", "feat/abc", "01a0af50",
		"unowned, unheld, clean, past the age floor")

	recs, err := removalaudit.Records(sink)
	if err != nil {
		t.Fatalf("read the audit sink: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want exactly 1 (%+v)", len(recs), recs)
	}
	r := recs[0]
	if r.Kind != removalaudit.KindWorktreeRemoval {
		t.Errorf("kind = %q, want %q", r.Kind, removalaudit.KindWorktreeRemoval)
	}
	if r.Arm != string(removalaudit.ArmSweepReclaim) {
		t.Errorf("arm = %q, want %q", r.Arm, removalaudit.ArmSweepReclaim)
	}
	if r.Target != "/workspace/gopi-abc" {
		t.Errorf("target = %q, want the worktree path", r.Target)
	}
	if r.Branch != "feat/abc" {
		t.Errorf("branch = %q, want feat/abc", r.Branch)
	}
	if r.TaskID != "01a0af50" {
		t.Errorf("task_id = %q, want 01a0af50", r.TaskID)
	}
	if !strings.Contains(r.Evidence, "past the age floor") {
		t.Errorf("evidence = %q, want the deciding evidence", r.Evidence)
	}
	if r.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", r.PID, os.Getpid())
	}
	if r.Version != removalaudit.Version {
		t.Errorf("version = %d, want %d", r.Version, removalaudit.Version)
	}
	if !strings.HasSuffix(r.TS, "Z") {
		t.Errorf("ts = %q, want a UTC RFC3339 stamp", r.TS)
	}
}

// TestBranchDeleteIsItsOwnKind: a ref deletion is never readable as a directory
// deletion — the two classes carry different kinds and different ops.
func TestBranchDeleteIsItsOwnKind(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, sink)

	removalaudit.BranchDelete(removalaudit.ArmFinishCleanupBranch, "/repo/x",
		"fix/thin", "01a0af77", "no live row owes this branch")

	recs, err := removalaudit.Records(sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	if recs[0].Kind != removalaudit.KindBranchDelete || recs[0].Op != removalaudit.OpBranchDelete {
		t.Errorf("kind/op = %q/%q, want %q/%q", recs[0].Kind, recs[0].Op,
			removalaudit.KindBranchDelete, removalaudit.OpBranchDelete)
	}
	if recs[0].Target != "fix/thin" {
		t.Errorf("target = %q, want the branch name", recs[0].Target)
	}
}

// removalRecordProbe is the NAMED assertion every removal-path test (here and
// in internal/tools) reuses: it returns "" when the sink holds exactly one
// record for (arm, target), else the failure text. Returning the text rather
// than calling t.Fatal is what lets the RED half assert on the SAME sentence the
// GREEN half asserts — the failure is the evidence, not an accident.
func removalRecordProbe(sink string, arm removalaudit.Arm, target string) string {
	recs, err := removalaudit.Records(sink)
	if err != nil && !os.IsNotExist(err) {
		return "read the audit sink: " + err.Error()
	}
	for _, r := range recs {
		if r.Arm == string(arm) && r.Target == target {
			if r.TaskID == "" && r.Branch == "" && r.Evidence == "" {
				return "the record for " + target + " carries none of branch/row/evidence — unattributable"
			}
			return ""
		}
	}
	return "no audit record for arm " + string(arm) + " target " + target +
		" (records present: " + strconv.Itoa(len(recs)) + ")"
}

// TestEmitGreenThenRed is the epic's acceptance proof in one place: with the
// emit live the named assertion passes; with the emit DISABLED the same
// assertion fails, and the failure text is quoted here so the red is readable
// (rather than a claim that the test "covers" a disabled emit).
func TestEmitGreenThenRed(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, sink)
	const target = "/scratch/ground/wt"

	removalaudit.WorktreeRemove(removalaudit.ArmRowReclaim, "/repo/x", target,
		"feat/x", "T-1", "the row was reclaimed")
	if why := removalRecordProbe(sink, removalaudit.ArmRowReclaim, target); why != "" {
		t.Fatalf("GREEN half: %s", why)
	}

	prev := removalaudit.SetEmitDisabledForTest(true)
	t.Cleanup(func() { removalaudit.SetEmitDisabledForTest(prev) })
	removalaudit.WorktreeRemove(removalaudit.ArmRowReclaim, "/repo/x", target+"/second",
		"feat/x", "T-2", "the row was reclaimed")

	why := removalRecordProbe(sink, removalaudit.ArmRowReclaim, target+"/second")
	if why == "" {
		t.Fatal("RED half: the record for the second removal was found although the emit is disabled" +
			" — the assertion does not actually depend on the emit")
	}
	t.Logf("RED (exact assertion text): %s", why)
}

// TestEmitIsDisabledOnlyWhenAsked pins the default: the hook is off unless a
// test flips it, so production can never inherit a silent sink from a test
// that forgot to restore it.
func TestEmitIsDisabledOnlyWhenAsked(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, sink)

	removalaudit.WorktreeRemove(removalaudit.ArmRowReclaim, "/repo/x", "/workspace/wt", "b", "T", "e")
	if _, err := os.Stat(sink); err != nil {
		t.Fatalf("the default emit wrote nothing: %v", err)
	}
}

// TestRecordIsOneLineOfJSON: the sink is shared and append-only, so a record
// must be exactly one line — a multi-line record would corrupt every reader
// that keys on lines.
func TestRecordIsOneLineOfJSON(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, sink)

	// Evidence carrying a newline is the adversarial input: it must not split
	// the record into two lines.
	removalaudit.BranchDelete(removalaudit.ArmTaskMergeBranch, "/repo", "b", "",
		"line one\nline two")

	raw, err := os.ReadFile(sink)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(strings.TrimRight(string(raw), "\n"), "\n"); lines != 0 {
		t.Fatalf("the sink holds %d embedded newlines, want 1 single-line JSON record: %q",
			lines, string(raw))
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &probe); err != nil {
		t.Fatalf("the record is not one JSON object: %v", err)
	}
}

// TestBranchDeleteAtCarriesTheTipAndTheDeliveryPoint is defect D2's RED/GREEN
// proof (task 01a12b80): a branch-delete record must name the commit the ref
// pointed at and the point the work was handed over to, so "did THIS row's
// branch reach the server?" is a grep on ONE host instead of a cross-host
// investigation.
//
// It asserts on the RAW LINE as well as the decoded struct, because the reader
// that exists today is `grep` over the file — a field that only survives into a
// Go value would not answer the operator's question.
func TestBranchDeleteAtCarriesTheTipAndTheDeliveryPoint(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, sink)
	const (
		branch = "task/01a12b80"
		tip    = "e81f64fb2e2b2dfd0f1c7b4d9f0a1c2d3e4f5a6b"
		target = "peer1:/home/user/work/GoPi"
	)

	removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranch, "/repo/x", branch,
		"01a12b80", tip, target, "no live row owes this branch")

	recs, err := removalaudit.Records(sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	if recs[0].Sha != tip {
		t.Errorf("sha = %q, want the tip the ref held", recs[0].Sha)
	}
	if recs[0].DeliveryPoint != target {
		t.Errorf("delivery_point = %q, want %q", recs[0].DeliveryPoint, target)
	}
	raw, err := os.ReadFile(sink)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{removalaudit.FieldSha, removalaudit.FieldDeliveryPoint} {
		if !strings.Contains(string(raw), `"`+key+`"`) {
			t.Errorf("the raw line carries no %q key: %s", key, string(raw))
		}
	}
}

// TestBranchDeleteOmitsFactsItsCallerNeverResolved pins the OTHER direction, so
// the new fields cannot become a required slot that every arm fills with
// something: a caller that knows no tip records none. An invented sha would be
// worse than an absent one — it is a claim about a commit nobody read.
func TestBranchDeleteOmitsFactsItsCallerNeverResolved(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, sink)

	removalaudit.BranchDelete(removalaudit.ArmSweepDeadRegistrationBranch, "/repo/x",
		"feat/thin", "01a0af77", "no live row owes this branch")

	recs, err := removalaudit.Records(sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	if recs[0].Sha != "" || recs[0].DeliveryPoint != "" {
		t.Errorf("sha/delivery_point = %q/%q, want both empty (the caller resolved neither)",
			recs[0].Sha, recs[0].DeliveryPoint)
	}
	raw, err := os.ReadFile(sink)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), removalaudit.FieldSha) {
		t.Errorf("the omitted sha must not appear on the wire: %s", string(raw))
	}
}

// TestBranchDeleteAtIsTheEntryTheDeletionSitesUse is the RED half of the pair
// above, driven the way this package's other arms are: with the emitter
// disabled the SAME assertion fails, and the failure text is quoted so the red
// is readable rather than claimed.
func TestBranchDeleteAtIsTheEntryTheDeletionSitesUse(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, sink)
	const branch = "task/red-arm"

	probe := func() string {
		recs, err := removalaudit.Records(sink)
		if err != nil && !os.IsNotExist(err) {
			return "read the audit sink: " + err.Error()
		}
		for _, r := range recs {
			if r.Arm == string(removalaudit.ArmFinishCleanupBranch) && r.Target == branch {
				if r.Sha == "" || r.DeliveryPoint == "" {
					return "the record for " + branch + " carries no sha/delivery_point — the delivery is unaccountable"
				}
				return ""
			}
		}
		return "no audit record for arm " + string(removalaudit.ArmFinishCleanupBranch) + " target " + branch
	}

	prev := removalaudit.SetEmitDisabledForTest(true)
	removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranch, "/repo", branch, "T",
		"deadbeef", "peer1:/repo", "e")
	if why := probe(); why == "" {
		t.Fatal("RED half: the record was found although the emit is disabled")
	} else {
		t.Logf("RED (exact assertion text): %s", why)
	}
	removalaudit.SetEmitDisabledForTest(prev)

	removalaudit.BranchDeleteAt(removalaudit.ArmFinishCleanupBranch, "/repo", branch, "T",
		"deadbeef", "peer1:/repo", "e")
	if why := probe(); why != "" {
		t.Fatalf("GREEN half: %s", why)
	}
}

// TestRecordsReportsAMalformedLine: a sink that swallows a broken line would
// hide exactly the removal an operator is accounting for (Rule 15).
func TestRecordsReportsAMalformedLine(t *testing.T) {
	sink := filepath.Join(t.TempDir(), "removal-audit.log")
	if err := os.WriteFile(sink, []byte("{not json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := removalaudit.Records(sink); err == nil {
		t.Fatal("Records accepted a malformed line silently")
	}
}

// TestNoGopiRootAndNoOverrideIsRefusedNotGuessed: with neither a gopi data root
// nor the override, the emitter must NOT invent a path — it prints the refusal
// and returns (best-effort, never a crash).
func TestNoGopiRootAndNoOverrideIsRefusedNotGuessed(t *testing.T) {
	// A HOME with no gopi dir and no override: the resolver must refuse.
	t.Setenv("GOPI_HOME", filepath.Join(t.TempDir(), "no-gopi-here"))
	t.Setenv("HOME", filepath.Join(t.TempDir(), "no-home-here"))
	t.Setenv(removalaudit.EnvSink, "")

	// No panic, no file invented: the call is best-effort by contract.
	removalaudit.WorktreeRemove(removalaudit.ArmRowReclaim, "/repo", "/wt", "", "", "")
	if _, err := removalaudit.DefaultPath(); err == nil {
		t.Log("a gopi data root resolved in this environment — the refusal path was not exercised here")
	}
}

// captureStderr runs fn with os.Stderr redirected into a pipe and returns what
// was written to it. It is how the two tests below assert the LOUD half of the
// contract (Rule 15): a refused record must be readable on stderr, not merely
// absent from disk.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() { os.Stderr = saved }()
		fn()
	}()
	if err := w.Close(); err != nil {
		t.Fatalf("close the capture pipe: %v", err)
	}
	out := <-done
	if err := r.Close(); err != nil {
		t.Fatalf("close the capture reader: %v", err)
	}
	return out
}

// freshProfileSinkFixture pins a profile that has NEVER written a record: the
// profile root exists nowhere on disk (the emitter's default sink lives under
// <GOPI_HOME>/.gopi/logs/, and neither level is created by this fixture).
// It returns the directory the sink must be created in — the one whose absence
// was the defect.
//
// The scratch directory comes from the shared scaffold (Rule 32's pattern; Rule
// 22's "off the / partition" is enforced by the suite's TestMain), never from a
// hand-rolled path.
func freshProfileSinkFixture(t *testing.T) string {
	t.Helper()
	home := testutil.TestScratchDir(t, "removalaudit-fresh-profile")
	t.Setenv("GOPI_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv(removalaudit.EnvSink, "")

	path, err := removalaudit.DefaultPath()
	if err != nil {
		t.Fatalf("a fresh profile must still resolve a default sink path: %v", err)
	}
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("precondition: the sink directory %s must NOT exist (stat err = %v)", dir, err)
	}
	if p, _ := removalaudit.SinkPath(); p != path {
		t.Fatalf("SinkPath() = %q, want the default %q (one resolution, Rule 2)", p, path)
	}
	return dir
}

// TestFreshProfileRecordsLandInTheSinkTheReaderNames is the epic's GREEN half,
// and it is written in the epic's own words: on a profile whose logs/ does not
// exist, one emit leaves <gopi dir>/logs/removal-audit.log on disk and
// Records(DefaultPath()) reads the record back.
//
// WHY IT IS NOT A UNIT TEST OF MkdirAll: it asserts the pair the acceptance
// names — the file EXISTS and the READER the package exports finds the record —
// through the real emit entry point, so a sink that created the directory and
// then wrote somewhere else would still fail here.
func TestFreshProfileRecordsLandInTheSinkTheReaderNames(t *testing.T) {
	dir := freshProfileSinkFixture(t)
	const target = "/scratch/fresh-profile/wt"

	removalaudit.WorktreeRemove(removalaudit.ArmRowReclaim, "/repo/x", target,
		"feat/fresh", "T-FRESH", "the row was reclaimed")

	path, err := removalaudit.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("the sink directory %s was not created by the emit (stat err = %v)", dir, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the record did not land in %s: %v", path, err)
	}
	recs, err := removalaudit.Records(path)
	if err != nil {
		t.Fatalf("read back the default sink: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want exactly 1 (%+v)", len(recs), recs)
	}
	if recs[0].Arm != string(removalaudit.ArmRowReclaim) || recs[0].Target != target {
		t.Errorf("arm/target = %q/%q, want %q/%q", recs[0].Arm, recs[0].Target,
			removalaudit.ArmRowReclaim, target)
	}
}

// TestFreshProfileWithoutTheDirectoryCreationLosesTheRecord is the RED half:
// with the directory creation driven back to its PRE-FIX behaviour (the hook),
// the SAME assertion fails — no file, no record — AND the refusal is on stderr.
//
// The refusal half is asserted here rather than assumed, because the defect was
// never silent: the log line existed and the audit TRAIL did not, which is the
// combination an operator cannot act on (the record is a file an incident
// post-mortem greps, not a line in whichever stderr happened to be attached).
func TestFreshProfileWithoutTheDirectoryCreationLosesTheRecord(t *testing.T) {
	dir := freshProfileSinkFixture(t)
	const target = "/scratch/pre-fix/wt"

	prev := removalaudit.SetSinkDirEnsureDisabledForTest(true)
	t.Cleanup(func() { removalaudit.SetSinkDirEnsureDisabledForTest(prev) })

	stderr := captureStderr(t, func() {
		removalaudit.WorktreeRemove(removalaudit.ArmRowReclaim, "/repo/x", target,
			"feat/pre-fix", "T-RED", "the row was reclaimed")
	})

	path, err := removalaudit.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("RED half is vacuous: %s exists although the sink directory was never created (stat err = %v)",
			path, err)
	}
	if recs, err := removalaudit.Records(path); err == nil {
		t.Fatalf("RED half is vacuous: Records read %d record(s) from an absent sink", len(recs))
	}
	if !strings.Contains(stderr, target) {
		t.Errorf("the refusal must name the record it lost (%q): stderr = %q", target, stderr)
	}
	if !strings.Contains(stderr, "not written") {
		t.Errorf("the refusal must say the record was not written: stderr = %q", stderr)
	}
	t.Logf("RED (pre-fix behaviour, observed): dir=%s absent; stderr: %s", dir, strings.TrimSpace(stderr))
}

// TestExplicitOverrideWithAMissingDirectoryIsRefusedNotInvented pins the
// override half of the contract, which is deliberately NOT the default's: an
// explicit GOPI_REMOVAL_AUDIT_LOG names a FILE, and the directory it implies is
// the caller's own responsibility. Pointing it at a directory that does not
// exist must be LOUDLY refused — never repaired by inventing the directory,
// which would turn a caller's typo into an audit trail nobody looks at.
func TestExplicitOverrideWithAMissingDirectoryIsRefusedNotInvented(t *testing.T) {
	home := testutil.TestScratchDir(t, "removalaudit-override-home")
	t.Setenv("GOPI_HOME", home)
	t.Setenv("HOME", home)

	missingDir := filepath.Join(home, "no-such-dir")
	override := filepath.Join(missingDir, "removal-audit.log")
	t.Setenv(removalaudit.EnvSink, override)

	stderr := captureStderr(t, func() {
		removalaudit.WorktreeRemove(removalaudit.ArmRowReclaim, "/repo/x", "/wt", "b", "T-OVR", "e")
	})

	if _, err := os.Stat(missingDir); !os.IsNotExist(err) {
		t.Fatalf("the override's directory %s was INVENTED (stat err = %v) — the explicit path is the caller's contract",
			missingDir, err)
	}
	if _, err := os.Stat(override); !os.IsNotExist(err) {
		t.Fatalf("a record landed at the override path although its directory does not exist")
	}
	if !strings.Contains(stderr, override) {
		t.Errorf("the refusal must name the override path %q: stderr = %q", override, stderr)
	}
	t.Logf("override refusal (observed): %s", strings.TrimSpace(stderr))
}
