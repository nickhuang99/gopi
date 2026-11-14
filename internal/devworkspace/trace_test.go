package devworkspace

// trace_test.go — the source-repository fallback's four red lines (bug
// 01a1515c, second half). Each test below pins ONE of them:
//
//	① a repo OUTSIDE the workspace root is not traced at all (TraceToSource
//	   refuses without touching git — the normal repo's behaviour is untouched);
//	② an ATTEMPTED trace always prints TraceLineFormat, whether it hits or
//	   misses (a fallback nobody can read in the log is a silent repair);
//	③ a MISS rescues nothing: ok=false, no build command invented;
//	④ nothing is cached — a second call sees a repository that gained an origin
//	   in between, so no stale answer can ever authorize a push.

import (
	"bytes"
	"context"
	"github.com/nickhuang99/gopi/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// traceFixture makes: a SOURCE repository, a CLONE of it inside the workspace
// root (the shape a reframe child inherits), and a settings document declaring
// the source's build command. Returns (source, workspace clone, root).
func traceFixture(t *testing.T, declareSource bool) (string, string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: the trace asks a real checkout for its origin")
	}
	base := t.TempDir()
	source := filepath.Join(base, "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	traceGit(t, source, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	traceGit(t, source, "add", "a.txt")
	traceGit(t, source, "commit", "-q", "-m", "init")

	root := filepath.Join(base, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(root, "01a1515c-0000-7000-8000-00000000c10e")
	traceGit(t, base, "clone", "-q", source, clone)

	body := `{"repos":{`
	if declareSource {
		body += `"` + source + `":{"build":"echo TRACED-BUILD"}`
	}
	body += `}}`
	settings := filepath.Join(base, "settings.json")
	if err := os.WriteFile(settings, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsPathEnv, settings)
	return source, clone, root
}

// traceGit runs one git command, failing the test on error.
func traceGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testutil.GitEnv(
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestTraceFindsTheSourceRepositorysDeclaredBuild: the measured defect's exact
// shape — a row naming its parent's WORKSPACE — is rescued by the source's
// declaration, and the rescue is LOUD.
func TestTraceFindsTheSourceRepositorysDeclaredBuild(t *testing.T) {
	source, clone, root := traceFixture(t, true)
	var log bytes.Buffer
	cfg := Config{Root: root, RootSource: "settings workspace_root", Log: &log}

	rc, tr, ok := TraceToSource(context.Background(), cfg, clone)
	if !ok {
		t.Fatalf("the trace found no declaration for %s (err=%v); the source repo %s declares one", clone, tr.Err, source)
	}
	if rc.Build != "echo TRACED-BUILD" {
		t.Fatalf("traced build = %q, want the source repository's declared command", rc.Build)
	}
	if !sameTracePath(tr.Display, source) {
		t.Fatalf("trace display path = %q, want the settings spelling %q", tr.Display, source)
	}
	// ② the line is printed, and it names the THREE facts the red line demands:
	// the workspace path, the root it lies under, and the origin it resolved.
	for _, want := range []string{"source trace:", clone, source} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("the trace's log line must name %q; got:\n%s", want, log.String())
		}
	}
	if !strings.Contains(log.String(), TraceOutcomeHitFormat[:12]) {
		t.Fatalf("the hit must be reported too; got:\n%s", log.String())
	}
}

// TestTraceRefusesARepoOutsideTheWorkspaceRootWithoutTouchingGit: ① — a normal
// repository is never asked anything, so its log and its git calls are
// byte-identical to the pre-fallback run.
func TestTraceRefusesARepoOutsideTheWorkspaceRootWithoutTouchingGit(t *testing.T) {
	source, _, root := traceFixture(t, true)
	var log bytes.Buffer
	cfg := Config{Root: root, Log: &log}

	_, tr, ok := TraceToSource(context.Background(), cfg, source)
	if ok {
		t.Fatalf("a repo OUTSIDE the workspace root must never be traced")
	}
	if tr.Origin != "" {
		t.Fatalf("git was consulted for an out-of-root repo: origin=%q", tr.Origin)
	}
	if log.Len() != 0 {
		t.Fatalf("an out-of-root repo must produce NO trace line; got:\n%s", log.String())
	}
}

// TestTraceMissStillRefusesAndSaysWhy: ③ — the source is found but declares
// nothing; the fallback invents no command and the caller keeps its refusal.
func TestTraceMissStillRefusesAndSaysWhy(t *testing.T) {
	_, clone, root := traceFixture(t, false)
	var log bytes.Buffer
	cfg := Config{Root: root, Log: &log}

	rc, tr, ok := TraceToSource(context.Background(), cfg, clone)
	if ok {
		t.Fatalf("no declaration exists, yet the trace rescued the run with %q", rc.Build)
	}
	if rc.Build != "" {
		t.Fatalf("a miss must invent no build command; got %q", rc.Build)
	}
	if tr.Err == nil {
		t.Fatalf("a miss must carry its own reason")
	}
	// ② an ATTEMPTED trace is narrated even when it misses.
	if !strings.Contains(log.String(), "source trace:") {
		t.Fatalf("the attempted trace must print its line even on a miss; got:\n%s", log.String())
	}
}

// TestTraceCachesNothing: ④ — the same call twice, with the repository gaining
// its origin in between, must see the change. A cached answer would keep
// refusing (and, worse, a cache built on a stale hit could authorize a push to
// a repository the row no longer points at).
func TestTraceCachesNothing(t *testing.T) {
	source, clone, root := traceFixture(t, true)
	// Break the provenance FIRST: a checkout with no origin.
	traceGit(t, clone, "remote", "remove", "origin")
	var log bytes.Buffer
	cfg := Config{Root: root, Log: &log}

	if _, tr, ok := TraceToSource(context.Background(), cfg, clone); ok {
		t.Fatalf("a clone with no origin must not trace (got %+v)", tr)
	}
	// The origin comes back BETWEEN the two calls.
	traceGit(t, clone, "remote", "add", "origin", source)
	rc, tr, ok := TraceToSource(context.Background(), cfg, clone)
	if !ok {
		t.Fatalf("the second call must SEE the origin added between the calls (caching?): %v", tr.Err)
	}
	if rc.Build != "echo TRACED-BUILD" {
		t.Fatalf("second call build = %q, want the declared one", rc.Build)
	}
}

// TestSourceRepoDisplayPathReducesOriginURLsToTheDeclaredSpelling pins the
// mapping from git's own origin spellings onto the repos table's key.
func TestSourceRepoDisplayPathReducesOriginURLsToTheDeclaredSpelling(t *testing.T) {
	abs := filepath.Join(string(filepath.Separator), "home", "the operator", "work", "gopi")
	cases := []struct{ origin, want string }{
		{abs, abs},
		{"file://" + abs, abs},
		{"the operator@example-host:" + abs, abs},
		{"example-host:" + abs, abs},
		{"git@github.com:some/remote.git", ""}, // no local checkout: not declarable
	}
	for _, c := range cases {
		got, err := SourceRepoDisplayPath(c.origin)
		if c.want == "" {
			if err == nil {
				t.Errorf("SourceRepoDisplayPath(%q) = %q, want an error (no local path)", c.origin, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("SourceRepoDisplayPath(%q): %v", c.origin, err)
			continue
		}
		if got != c.want {
			t.Errorf("SourceRepoDisplayPath(%q) = %q, want %q", c.origin, got, c.want)
		}
	}
}

// sameTracePath compares two paths after symlink resolution.
func sameTracePath(a, b string) bool {
	ra, erra := filepath.EvalSymlinks(a)
	rb, errb := filepath.EvalSymlinks(b)
	if erra != nil || errb != nil {
		return a == b
	}
	return ra == rb
}
