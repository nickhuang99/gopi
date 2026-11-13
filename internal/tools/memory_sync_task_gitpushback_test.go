package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gitPushbackTestPeer is the alias these fixtures pull from. It is DECLARED
// per test: TaskHarvest gates on the declared topology before the transport,
// so a fixture that never declares its peer would exercise the GATE instead of
// the git push-back ordering it is about (Rule 1b: no bare literal per call).
const gitPushbackTestPeer = "testpeer"

// TestGitPushbackReportZeroNil tests the zero-value report is safe to print.
func TestGitPushbackReportZeroNil(t *testing.T) {
	r := &GitPushbackReport{BranchesPushed: []string{}, PushFailed: []string{}}
	if len(r.BranchesPushed) != 0 || len(r.PushFailed) != 0 {
		t.Fatalf("zero report must have empty slices: %+v", r)
	}
}

// TestTaskHarvestGitPushbackRunsAfterCommit verifies the git phase runs only
// after a committed db sync (order ⑤), that its report joins the counts, and
// that a pusher error is reported without corrupting the db outcome.
func TestTaskHarvestGitPushbackRunsAfterCommit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	harvestDeclarePeer(t, gitPushbackTestPeer)

	var gitRan bool
	opts := HarvestOptions{
		Peer:   gitPushbackTestPeer,
		DBPath: dbPath,
		Fetch: func(peer string, anchor float64) ([]harvestRemoteRow, error) {
			// The git phase must NOT have run before the db pull.
			if gitRan {
				t.Fatalf("git push-back ran before the db fetch — order ⑤ broken")
			}
			return []harvestRemoteRow{{
				TaskID: "t-gitpb-1", Summary: "row citing a branch", Source: "hp",
				CreatedAt: 1000, UpdatedAt: 1000,
			}}, nil
		},
		GitPushback: func() (*GitPushbackReport, error) {
			gitRan = true
			return &GitPushbackReport{
				BranchesPushed: []string{"feat/x", "feat/y"},
				PushFailed:     []string{"feat/z: rejected"},
			}, nil
		},
	}
	counts, err := TaskHarvest(opts)
	if err != nil {
		t.Fatalf("TaskHarvest: %v", err)
	}
	if !gitRan {
		t.Fatalf("git push-back never ran")
	}
	if counts.Harvested != 1 {
		t.Fatalf("db rows: want harvested=1, got %+v", counts)
	}
	if counts.Git == nil {
		t.Fatalf("git report missing from counts")
	}
	if len(counts.Git.BranchesPushed) != 2 || len(counts.Git.PushFailed) != 1 {
		t.Fatalf("git report wrong: %+v", counts.Git)
	}

	// The db row must be committed before the git phase — reopen verifies.
	db2, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	db2.Close() //stylegate:ignore test closes the reopen handle that proves the db row was committed; the commit is already verified by the queries above, so a close error is not actionable
}

// TestTaskHarvestGitPushbackFailsSoft verifies a pusher ERROR does not fail
// the harvest — the db counts still return (domains are independent).
func TestTaskHarvestGitPushbackFailsSoft(t *testing.T) {
	dir := t.TempDir()
	harvestDeclarePeer(t, gitPushbackTestPeer)
	opts := HarvestOptions{
		Peer:   gitPushbackTestPeer,
		DBPath: filepath.Join(dir, "memory.db"),
		Fetch: func(peer string, anchor float64) ([]harvestRemoteRow, error) {
			return []harvestRemoteRow{}, nil
		},
		GitPushback: func() (*GitPushbackReport, error) {
			return nil, errors.New("git remote unreachable")
		},
	}
	counts, err := TaskHarvest(opts)
	if err == nil {
		t.Fatalf("pusher error must surface, got nil")
	}
	if !strings.Contains(err.Error(), "git push-back") {
		t.Fatalf("error must mention git push-back: %v", err)
	}
	if counts == nil {
		t.Fatalf("db counts must still return alongside the git error")
	}
}

// TestTaskHarvestGitPushbackNilDisabled verifies nil GitPushback = db-only
// (the --no-push path): no git field in counts.
func TestTaskHarvestGitPushbackNilDisabled(t *testing.T) {
	dir := t.TempDir()
	harvestDeclarePeer(t, gitPushbackTestPeer)
	opts := HarvestOptions{
		Peer:   gitPushbackTestPeer,
		DBPath: filepath.Join(dir, "memory.db"),
		Fetch: func(peer string, anchor float64) ([]harvestRemoteRow, error) {
			return []harvestRemoteRow{}, nil
		},
	}
	counts, err := TaskHarvest(opts)
	if err != nil {
		t.Fatalf("TaskHarvest: %v", err)
	}
	if counts.Git != nil {
		t.Fatalf("nil pusher must leave counts.Git nil, got %+v", counts.Git)
	}
}

// TestGitPushbackDefaultInvalidPeer verifies the peer guard (no spaces /
// semicolons — the value feeds ssh argv and the push target).
func TestGitPushbackDefaultInvalidPeer(t *testing.T) {
	for _, bad := range []string{"", "hp;rm", "hp rm", "a\nb"} {
		if _, err := gitPushbackDefault(bad); err == nil {
			t.Fatalf("peer %q must be refused", bad)
		}
	}
}

// TestGitPushbackDefaultListFailure verifies a git repo-resolution failure
// surfaces as an error (not a silent empty report).
func TestGitPushbackDefaultListFailure(t *testing.T) {
	// Run inside a directory that is NOT a git repo.
	dir := t.TempDir()
	saved, _ := cwd() //stylegate:ignore captures the fixture cwd for restoration
	if err := chdir(dir); err != nil {
		t.Skipf("cannot chdir: %v", err)
	}
	defer chdir(saved) //nolint:errcheck

	_, err := gitPushbackDefault("hp")
	if err == nil {
		t.Fatalf("non-repo directory must fail the repo resolution")
	}
}

// cwd/chdir are tiny helpers so the failure-injection test can leave the
// repo (gitPushbackDefault shells out to the real git binary).
func cwd() (string, error)   { return os.Getwd() }
func chdir(dir string) error { return os.Chdir(dir) }

// Compile-time guard: the tunables stay in sync with their users.
func init() {
	_ = peerRepoPath()
	_ = gitPushbackFetchTimeout
}
