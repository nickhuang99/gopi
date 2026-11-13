package main

// cli_archive_verify_commit_test.go — the CLI face of the master-claim epic:
// `gopi task archive --verify-commit <rev>` as ONE command that falsifies
// "X is already on master".
//
// The subject is the EXIT STATUS (the verdict) plus the observation the output
// carries, so the test drives the REAL binary against a REAL repository whose
// archived tip is not an ancestor of master — the motivating shape (session
// 01a0f3de read exactly such a tip as "master already implements it" and closed
// exit=0). The GREEN leg lands that commit on master and the SAME invocation
// flips to exit 0: one command, two verdicts, no change in how the revision is
// spelled.

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// archiveVerifyGitTimeout bounds every git child this file spawns (Rule 27).
const archiveVerifyGitTimeout = 60 * time.Second

const (
	archiveVerifyFeatureMsg   = "feature"
	archiveVerifySeedMsg      = "seed"
	archiveVerifySideBranch   = "feat/capture-self-window-fixture"
	archiveVerifyArchiveTag   = toolsArchivePrefix + archiveVerifySideBranch
	archiveVerifyShortShaHint = "merge-base --is-ancestor"
)

// toolsArchivePrefix mirrors tools.ArchiveBranchPrefix for the fixture's tag
// name — read from the exported alias rather than typed, so a rename cannot
// leave this file behind (Rules 1b/13/14).
const toolsArchivePrefix = "archive/"

// archiveVerifyRepo builds the fixture: master at a seed, a side branch one
// commit ahead, its tip tagged into the archive namespace, and NO branch ref
// left (the archived state the motivating run found).
func archiveVerifyRepo(t *testing.T) (repo, sideTip, masterTip string) {
	t.Helper()
	repo = t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), archiveVerifyGitTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "master")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("commit", "--allow-empty", "-q", "-m", archiveVerifySeedMsg)
	masterTip = run("rev-parse", "HEAD")
	run("checkout", "-q", "-b", archiveVerifySideBranch)
	run("commit", "--allow-empty", "-q", "-m", archiveVerifyFeatureMsg)
	sideTip = run("rev-parse", "HEAD")
	run("tag", archiveVerifyArchiveTag, archiveVerifySideBranch)
	run("update-ref", "-d", "refs/heads/"+archiveVerifySideBranch)
	run("checkout", "-q", "master")
	return repo, sideTip, masterTip
}

// TestArchiveVerifyCommitFalsifiesAnArchivedTip is the RED leg: the revision
// resolves, the verdict is NOT, the archive tag is named, and the exit status
// is 1.
func TestArchiveVerifyCommitFalsifiesAnArchivedTip(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo, sideTip, _ := archiveVerifyRepo(t)
	out, err := runArchiveProbe(t, bin, t.TempDir(), repo, archiveVerifyLedgerPath(t), "archive", "--verify-commit", sideTip)
	if err == nil {
		t.Fatalf("an archived tip master does not contain must exit non-zero; output:\n%s", out)
	}
	for _, want := range []string{
		"NOT an ancestor",
		archiveVerifyArchiveTag,
		archiveVerifyShortShaHint,
		"exit=1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output must carry %q, got:\n%s", want, out)
		}
	}
	// The revision's SHORT form is what a report writes — and the tag NAME is
	// what names it when no refs/heads does.
	outShort, errShort := runArchiveProbe(t, bin, t.TempDir(), repo, archiveVerifyLedgerPath(t),
		"archive", "--verify-commit", sideTip[:8])
	if errShort == nil {
		t.Fatalf("the short sha must be measured the same way; output:\n%s", outShort)
	}
	if !strings.Contains(outShort, archiveVerifyArchiveTag) {
		t.Errorf("the short-sha run must also name the archive tag, got:\n%s", outShort)
	}
	outTag, errTag := runArchiveProbe(t, bin, t.TempDir(), repo, archiveVerifyLedgerPath(t),
		"archive", "--verify-commit", archiveVerifyArchiveTag)
	if errTag == nil {
		t.Fatalf("the tag NAME must be accepted as a revision; output:\n%s", outTag)
	}
}

// TestArchiveVerifyCommitAcceptsALandedCommit is the GREEN leg.
func TestArchiveVerifyCommitAcceptsALandedCommit(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo, sideTip, masterTip := archiveVerifyRepo(t)
	ctx, cancel := context.WithTimeout(context.Background(), archiveVerifyGitTimeout)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "git", "-C", repo, "merge", "--ff-only", sideTip).CombinedOutput(); err != nil {
		t.Fatalf("land the side tip: %v: %s", err, out)
	}
	for _, rev := range []string{sideTip, "master", masterTip} {
		out, err := runArchiveProbe(t, bin, t.TempDir(), repo, archiveVerifyLedgerPath(t), "archive", "--verify-commit", rev)
		if err != nil {
			t.Fatalf("rev %s is an ancestor of master and must exit 0: %v\n%s", rev, err, out)
		}
		if !strings.Contains(out, "IS an ancestor") {
			t.Errorf("rev %s: output must say IS an ancestor, got:\n%s", rev, out)
		}
	}
}

// TestArchiveVerifyCommitRefusesMisuse: an unresolvable revision exits non-zero
// and SAYS SO (never a silent pass), and the flag-parsing contract holds.
func TestArchiveVerifyCommitRefusesMisuse(t *testing.T) {
	bin := gopiTestBinPath(t)
	repo, _, _ := archiveVerifyRepo(t)
	out, err := runArchiveProbe(t, bin, t.TempDir(), repo, archiveVerifyLedgerPath(t),
		"archive", "--verify-commit", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err == nil {
		t.Fatalf("an unresolvable revision must exit non-zero; output:\n%s", out)
	}
	if !strings.Contains(out, "UNRESOLVED") {
		t.Errorf("an unresolvable revision must say UNRESOLVED, got:\n%s", out)
	}
	// A branch argument beside --verify-commit is a misread flag, refused rather
	// than ignored.
	out, err = runArchiveProbe(t, bin, t.TempDir(), repo, archiveVerifyLedgerPath(t), "archive", "feat/x", "--verify-commit", "master")
	if err == nil {
		t.Fatalf("a positional branch beside --verify-commit must be refused; output:\n%s", out)
	}
	if !strings.Contains(out, "takes no branch argument") {
		t.Errorf("the refusal must name the misuse, got:\n%s", out)
	}
	// The usage text teaches the new mode (one definition: the help IS the face
	// the operator reads first).
	out, err = runArchiveProbe(t, bin, t.TempDir(), repo, archiveVerifyLedgerPath(t), "archive", "-h")
	if err != nil {
		t.Fatalf("archive -h: %v\n%s", err, out)
	}
	if !strings.Contains(out, "--verify-commit") || !strings.Contains(out, "EXIT STATUS is the verdict") {
		t.Errorf("the usage text must document --verify-commit and its criterion, got:\n%s", out)
	}
}

// archiveVerifyLedgerPath gives the probe its OWN ledger: the verb must never
// resolve the operator's real one (a CLI probe that opens the append-only
// operator ledger is the defect Rule 32's scaffold exists to prevent).
func archiveVerifyLedgerPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "memory.db")
}
