package tools

// master_claim_report_test.go — the SEAL's end-to-end proof for the
// implementation-claim check (master_claim_report.go): a report whose sentences
// say the integration branch ALREADY has something, and a REAL repository in
// which the commit those sentences name is NOT an ancestor of master but IS kept
// alive by an archive tag — the motivating case (session 01a0f3de), where the
// run concluded "master already has it, no gap" and closed exit=0 while its own
// observations said the branch ref was gone.
//
// Contract proven here, end to end through the ONE choke point every close-out
// passes (SealTaskReport): the finding is RECORDED on the digest and said on
// stderr, the report is NEVER rewritten, and the seal is NEVER refused. The
// GREEN leg is the same report with a CORRECT sentence (and with the commit
// landed) — no record at all.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/taskreport"
)

// masterClaimSealLabel is the finding's full operator label — the family
// prefix AND the label, because the sibling ran-check's line legitimately names
// the task id (`t-master-claim`), and a bare `master-claim` substring would read
// that line as this finding (measured: it did, on the first run of this test).
const masterClaimSealLabel = taskReportClaimWarnPrefix + MasterClaimWarningLabel

// masterClaimSealTaskID is the row these tests seal (each test gets its own
// sandbox via reportSandbox, so the id needs no uniqueness beyond readability).
const masterClaimSealTaskID = "t-master-claim"

// masterClaimSealReport is the MODEL-AUTHORED body under test: its Verification
// section makes the motivating claim about the fixture's archived tip.
func masterClaimSealReport(sha string) string {
	return `# Task Report: is the feature already on master?

## Completed
- audited the archived tip against the integration branch

## Verification
- the listing feature is already implemented on ` + "`master`" + ` per ` + "`" + sha + "`" + `
- ` + "`go build ./...`" + ` → exit=0 (this line is about the BUILD, not about master)

## Files
- internal/shot/x11list.go
`
}

// masterClaimCaptureStderr runs fn with os.Stderr redirected to a pipe and
// returns what was written (the precedent merge_build_gate_contained_test.go
// uses for the same reason: the finding's operator line is part of its record).
func masterClaimCaptureStderr(t *testing.T, fn func()) string {
	t.Helper()
	prev := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r) //stylegate:ignore reads the fake peer's response body for the report comparison below
		done <- buf.String()
	}()
	fn()
	os.Stderr = prev
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	return <-done
}

// TestSealRecordsMasterClaimBackedByAnArchivedCommit is the RED leg end to end.
func TestSealRecordsMasterClaimBackedByAnArchivedCommit(t *testing.T) {
	f := masterClaimBuildRepo(t)
	t.Chdir(f.Repo) // the seal measures in the repo the run was driven in
	_, path := seedReport(t, masterClaimSealTaskID, masterClaimSealReport(f.SideTip))

	var digest string
	stderr := masterClaimCaptureStderr(t, func() {
		line, err := SealTaskReport(masterClaimSealTaskID)
		if err != nil {
			t.Errorf("SealTaskReport: %v", err)
		}
		digest = line
	})
	t.Logf("seal digest line: %s", digest)
	if !strings.Contains(digest, MasterClaimDigestPrefix) {
		t.Errorf("the seal's digest line must carry the record, got: %q", digest)
	}

	if !strings.Contains(stderr, masterClaimSealLabel) {
		t.Errorf("stderr must carry the %q finding, got:\n%s", MasterClaimWarningLabel, stderr)
	}
	if !strings.Contains(stderr, f.ArchiveRef) {
		t.Errorf("stderr must name the archive ref holding the commit alive, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "exit=1") {
		t.Errorf("stderr must carry the observation's exit status, got:\n%s", stderr)
	}

	// The report itself is the artifact of record: its sentences survive byte
	// for byte (the seal never rewrites a run's prose).
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sealed report: %v", err)
	}
	if !strings.Contains(string(body), "already implemented on `master`") {
		t.Fatalf("the seal altered the authored claim; body:\n%s", body)
	}

	t.Logf("sealed digest fragment:\n%s", recordLine(string(body)))
	// The RECORD is what a reviewer reaches from the row: the same check the
	// seal just ran, rendered as the digest fragment.
	record := recordLine(string(body))
	if !strings.HasPrefix(record, MasterClaimDigestPrefix) {
		t.Fatalf("record = %q, want the %q prefix", record, MasterClaimDigestPrefix)
	}
	if !strings.Contains(record, f.ArchiveRef) {
		t.Errorf("record = %q, want it to name %s", record, f.ArchiveRef)
	}
	if !strings.Contains(record, "merge-base --is-ancestor") {
		t.Errorf("record = %q, want the command whose exit status decided it", record)
	}
}

// recordLine runs the seal's own record function over a sealed body — the
// digest fragment a reviewer reaches from the row (named so both legs below
// read it through ONE call, and so a test can PRINT what the row would keep).
func recordLine(body string) string {
	return taskReportMasterClaimRecord(masterClaimSealTaskID, body)
}

// TestSealSilentForAMeasuredClaimOnMaster is the GREEN leg: the SAME report
// shape, but the commit it names IS an ancestor of master — nothing is recorded
// and nothing is said. A check that fires here would fire on honest work.
func TestSealSilentForAMeasuredClaimOnMaster(t *testing.T) {
	f := masterClaimBuildRepo(t)
	t.Chdir(f.Repo)
	_, path := seedReport(t, masterClaimSealTaskID, masterClaimSealReport(f.MasterTip))

	stderr := masterClaimCaptureStderr(t, func() {
		if _, err := SealTaskReport(masterClaimSealTaskID); err != nil {
			t.Errorf("SealTaskReport: %v", err)
		}
	})
	if strings.Contains(stderr, masterClaimSealLabel) {
		t.Fatalf("a claim backed by a commit ON master must not be recorded, got:\n%s", stderr)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sealed report: %v", err)
	}
	if record := taskReportMasterClaimRecord(masterClaimSealTaskID, string(body)); record != "" {
		t.Fatalf("record = %q, want none for a measured claim", record)
	}
}

// TestSealRecordsAClaimThatNamesNoCommit: the claim nobody can re-measure is
// still recorded — as a gap, not as an accusation.
func TestSealRecordsAClaimThatNamesNoCommit(t *testing.T) {
	f := masterClaimBuildRepo(t)
	t.Chdir(f.Repo)
	body := `# Task Report: unnamed claim

## ` + taskreport.SectionEvidence + `
- the feature already landed on master (the audit says so)

## Not Done
- nothing
`
	_, path := seedReport(t, masterClaimSealTaskID, body)
	stderr := masterClaimCaptureStderr(t, func() {
		if _, err := SealTaskReport(masterClaimSealTaskID); err != nil {
			t.Errorf("SealTaskReport: %v", err)
		}
	})
	if !strings.Contains(stderr, "no commit named") {
		t.Fatalf("a claim naming no revision must be recorded as such, got:\n%s", stderr)
	}
	sealed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sealed report: %v", err)
	}
	record := taskReportMasterClaimRecord(masterClaimSealTaskID, string(sealed))
	if !strings.Contains(record, "no commit named") {
		t.Fatalf("record = %q, want the no-commit gap", record)
	}
}

// TestSealRecordStatesWhenTheRepoCannotBeResolved: an unmeasurable claim is
// STATED, never passed over (a silent hole is the defect this family closes).
func TestSealRecordStatesWhenTheRepoCannotBeResolved(t *testing.T) {
	notARepo := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(notARepo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(notARepo)
	_, _ = seedReport(t, masterClaimSealTaskID, masterClaimSealReport("a1b2c3d4"))
	record := taskReportMasterClaimRecord(masterClaimSealTaskID, masterClaimSealReport("a1b2c3d4"))
	if !strings.Contains(record, "unmeasured") {
		t.Fatalf("record = %q, want the explicit unmeasured statement", record)
	}
}
