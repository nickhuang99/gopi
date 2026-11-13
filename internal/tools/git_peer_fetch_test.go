package tools

// git_peer_fetch_test.go — the ONE peer fetch, and the one runner under it
// (epic worktree-base-align, 2026-09-26).
//
// Two things are asserted here, and the second is the one that keeps the
// consolidation from rotting:
//
//	① SyncPeerFetch really fetches: against a REAL two-repo world it mirrors a
//	   peer's integration-branch tip into the reserved ref, returns that sha,
//	   and returns an ERROR (never a local fallback) when the peer is not there.
//	② There is exactly ONE `exec` of git in the peer-fetch path. That is not a
//	   style assertion: three wrappers is the defect this epic removes, and a
//	   future edit that re-inlines a fourth would otherwise be invisible from
//	   the outside — the symptom (fetches that disagree) appears months later,
//	   in a merge, on another host.
import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// peerFetchGit runs one git command in dir with a deterministic identity and no
// ambient configuration, failing the test on error.
func peerFetchGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// peerFetchWorld is a real peer + client pair: a bare "server" repository whose
// integration branch the test moves, and a client clone that fetches from it.
type peerFetchWorld struct {
	root   string
	server string // the bare repo (the "target" every fetch names)
	client string // the fetching working repo
}

func newPeerFetchWorld(t *testing.T) *peerFetchWorld {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	w := &peerFetchWorld{
		root:   root,
		server: filepath.Join(root, "server.git"),
		client: filepath.Join(root, "client"),
	}
	peerFetchGit(t, root, "init", "--bare", "-b", IntegrationBranchName, w.server)
	peerFetchGit(t, root, "clone", w.server, w.client)
	peerFetchGit(t, w.client, "config", "user.email", "t@t")
	peerFetchGit(t, w.client, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(w.client, "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	peerFetchGit(t, w.client, "add", "a.txt")
	peerFetchGit(t, w.client, "commit", "-m", "seed")
	peerFetchGit(t, w.client, "push", "origin", IntegrationBranchName)
	return w
}

// advanceMaster commits one file to the server's integration branch through a
// scratch clone and returns the new tip.
func (w *peerFetchWorld) advanceMaster(t *testing.T, name, content string) string {
	t.Helper()
	clone := filepath.Join(w.root, "advance-"+strings.ReplaceAll(name, "/", "-"))
	peerFetchGit(t, w.root, "clone", w.server, clone)
	peerFetchGit(t, clone, "config", "user.email", "t@t")
	peerFetchGit(t, clone, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(clone, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	peerFetchGit(t, clone, "add", name)
	peerFetchGit(t, clone, "commit", "-m", "advance "+name)
	peerFetchGit(t, clone, "push", "origin", IntegrationBranchName)
	return peerFetchGit(t, w.server, "rev-parse", IntegrationBranchName)
}

// TestSyncPeerFetchReturnsThePeerTip: the tip is the PEER'S, read after a real
// fetch — not the client's own stale master (which the fixture makes different
// on purpose, so a local fallback cannot pass this test).
func TestSyncPeerFetchReturnsThePeerTip(t *testing.T) {
	w := newPeerFetchWorld(t)
	// Move the peer ahead FIRST, so the client's local master is genuinely
	// behind by the time it fetches.
	want := w.advanceMaster(t, "peer.txt", "peer work")
	local := peerFetchGit(t, w.client, "rev-parse", IntegrationBranchName)
	if local == want {
		t.Fatalf("fixture: the client's master must start BEHIND the peer's (%s)", local)
	}

	got, err := SyncPeerFetch(context.Background(), w.client, w.server)
	if err != nil {
		t.Fatalf("SyncPeerFetch: %v", err)
	}
	if got != want {
		t.Fatalf("SyncPeerFetch returned %s, want the peer's tip %s (got the client's local master %s?)", got, want, local)
	}
	// The mirror ref is what carries it, and it is DURABLE (a ref, not
	// FETCH_HEAD) — the close-out's peer resolution reads this ref.
	if mirror := peerFetchGit(t, w.client, "rev-parse", gitPushbackCheckRef); mirror != want {
		t.Errorf("%s = %s, want %s", gitPushbackCheckRef, mirror, want)
	}
	// Rule 29: the fetch wrote no branch of this repo, and left the tree clean.
	if after := peerFetchGit(t, w.client, "rev-parse", IntegrationBranchName); after != local {
		t.Errorf("the fetch moved the client's own master (%s → %s) — the mirror must not touch local branches", local, after)
	}
	if st := peerFetchGit(t, w.client, "status", "--porcelain"); st != "" {
		t.Errorf("the fetch left the working tree dirty: %q", st)
	}
}

// TestSyncPeerFetchNeverFallsBackToLocalMaster: an unreachable peer is an ERROR
// (Rule 15). The tempting "fall back to the local master" would return a
// plausible sha for a peer that was never reached — the caller could not tell
// the two apart, which is exactly how a stale baseline becomes invisible.
func TestSyncPeerFetchNeverFallsBackToLocalMaster(t *testing.T) {
	w := newPeerFetchWorld(t)
	if _, err := SyncPeerFetch(context.Background(), w.client, filepath.Join(w.root, "no-such-peer")); err == nil {
		t.Fatal("an unreachable peer returned no error — a caller cannot distinguish this from a real tip")
	}
	if _, err := SyncPeerFetch(context.Background(), w.client, ""); err == nil {
		t.Fatal("an empty target returned no error")
	}
	if _, err := SyncPeerFetch(context.Background(), "", w.server); err == nil {
		t.Fatal("an empty repo argument returned no error")
	}
}

// TestSyncPeerFetchReportsGitStderr: the failure names what git said. A caller
// that only learns "exit status 128" cannot tell a missing repository from a
// refused ssh host — and the ground's degradation note prints exactly this
// text (Rule 15: failure reported BY NAME, including stderr).
func TestSyncPeerFetchReportsGitStderr(t *testing.T) {
	w := newPeerFetchWorld(t)
	_, err := SyncPeerFetch(context.Background(), w.client, filepath.Join(w.root, "no-such-peer"))
	if err == nil {
		t.Fatal("want an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no-such-peer") {
		t.Errorf("the failure must name the target it could not reach; got %q", msg)
	}
	if !strings.Contains(msg, "does not appear to be a git repository") {
		t.Errorf("the failure must carry git's own stderr, not just exit status; got %q", msg)
	}
}

// peerFetchGateFiles are the files that used to each own a `exec.Command*("git")`
// in the peer-fetch path, plus the one file that owns the survivor.
var peerFetchGateFiles = []string{
	"git_peer_fetch.go",
	"task_sync_git.go",
	"task_sync_git_pushback.go",
	"task_finish_premerge.go",
	"task_run_ground.go",
	"task_ground_baseline.go",
}

// TestPeerFetchPathHasExactlyOneGitRunner is the consolidation's own gate
// (Rules 13/14). It requires that the ONLY exec of git among those files is
// gitPeerRun's — every other way of reaching git in the peer-fetch path must be
// a delegation.
//
// It is deliberately a SOURCE gate rather than a behaviour test: behaviour
// cannot distinguish "one runner called twice" from "two runners that happen to
// agree today", and it is that second shape (three definitions, one meaning)
// that this epic removes.
func TestPeerFetchPathHasExactlyOneGitRunner(t *testing.T) {
	const runnerFile = "git_peer_fetch.go"
	found := 0
	for _, name := range peerFetchGateFiles {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v (this gate must run with the package as its working directory)", name, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			code := peerFetchCodeLine(line)
			if !strings.Contains(code, "exec.Command") || !strings.Contains(code, `"git"`) {
				continue
			}
			found++
			if name == runnerFile && peerFetchLineIsInRunner(string(src), i) {
				continue
			}
			t.Errorf("%s:%d runs git outside the ONE runner (gitPeerRun in %s): %s",
				name, i+1, runnerFile, strings.TrimSpace(line))
		}
	}
	// A scan that found NOTHING is not a pass: an empty result must be
	// distinguishable from a scan that never ran (Rule 15a).
	if found == 0 {
		t.Fatal("no git exec found in any scanned file — the gate did not read what it thinks it read")
	}
	if found != 1 {
		t.Fatalf("found %d git exec call sites in the peer-fetch path; want exactly 1", found)
	}
}

// peerFetchCodeLine strips a line's comment half, so the gate reads CODE, not
// prose: this very file and the runner discuss `exec.Command*("git", …)` in
// their comments, and a gate that counted those would fire on its own
// documentation (and on the history it exists to preserve).
func peerFetchCodeLine(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// peerFetchLineIsInRunner reports whether the line at index i sits inside
// gitPeerRun's body: it walks backwards to the nearest top-level `func` and
// checks the name. Comment- and formatting-independent, so reformatting the
// file cannot make the gate lie.
func peerFetchLineIsInRunner(src string, i int) bool {
	lines := strings.Split(src, "\n")
	for j := i; j >= 0; j-- {
		line := strings.TrimSpace(lines[j])
		if !strings.HasPrefix(line, "func ") {
			continue
		}
		return strings.HasPrefix(line, "func gitPeerRun(")
	}
	return false
}
