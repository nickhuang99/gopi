package standardgate

// capdemo_external_precondition_test.go — the regression arms for
// capdemo_external_gate_test.go's ABSENCE rule.
//
// WHY THESE ARMS EXIST. The gate used to fail whenever `docs/archive/demo/external`
// was not on disk, which is the NORMAL state of a clone (the bundle is untracked
// run output — see the gate file's header), so `go test ./internal/standards/`
// could not be green in any fresh worktree. Relaxing that into a skip would have
// been the cheap fix and the wrong one: it would also have turned a DELETED
// bundle green. So the rule is narrowed instead, and the narrowing is what these
// arms pin — each drives the REAL gate function over a throwaway checkout:
//
//	P1 a checkout whose history never carried the bundle → no subject, green, and
//	   the reason is printed (a silent pass would be indistinguishable from a
//	   deleted bundle).
//	P2 a checkout whose history DID carry the bundle and no longer has it → RED,
//	   naming the commit that carried it (the deletion arm — the one a skip would
//	   have broken).
//	P3 a checkout that is not a repository at all → RED: an absence that cannot be
//	   explained is not a pass (Rule 15: found-nothing vs never-scanned).
//	P4 the bundle present but the checkpoint reporting stale anchors → still RED
//	   (the gate's original verdict, re-measured after the change so the tightening
//	   cannot have been traded away).
//
// Rule 22: every directory here comes from t.TempDir() under the suite's pinned
// scratch root, never a hand-spelled /tmp. Rule 27: nothing here generates load —
// these are three-commit throwaway repositories and one stub script.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// capdemoArmBundleFile is a path INSIDE the bundle used to give a fixture
	// repository a history that carries the bundle.
	capdemoArmBundleFile = capdemoCheckBundleRel + "/stills/README.md"
	// capdemoArmStubStale is the checkpoint stand-in for the present-bundle arm:
	// it reports a stale anchor, exactly as the real script does when a caption's
	// digest stops matching its still.
	capdemoArmStubStale = "echo 'STALE  A3 digest mismatch (fixture)'\nexit 1\n"
	// capdemoArmScriptMode is the mode the driven checkpoint is executed with.
	capdemoArmScriptMode = 0o755
)

// capdemoRecorder is the capdemoFataler the arms hand to the gate: it records the
// verdict instead of ending the test process, so the arm can assert on WHICH
// verdict came out.
type capdemoRecorder struct {
	fatal []string
	errs  []string
}

func (r *capdemoRecorder) Helper() {}

func (r *capdemoRecorder) Fatalf(format string, args ...any) {
	r.fatal = append(r.fatal, fmt.Sprintf(format, args...))
}

func (r *capdemoRecorder) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

// failed reports whether the gate refused, and why.
func (r *capdemoRecorder) failed() (bool, string) {
	if len(r.fatal) > 0 {
		return true, strings.Join(r.fatal, " / ")
	}
	if len(r.errs) > 0 {
		return true, strings.Join(r.errs, " / ")
	}
	return false, ""
}

// runCapdemoArm drives the real gate over root, capturing what it printed.
func runCapdemoArm(t *testing.T, root string) (*capdemoRecorder, string) {
	t.Helper()
	var buf bytes.Buffer
	prev := capdemoReport
	capdemoReport = &buf
	t.Cleanup(func() { capdemoReport = prev })
	rec := &capdemoRecorder{}
	runCapdemoCheck(rec, root)
	return rec, buf.String()
}

// capdemoArmRepo materialises a throwaway checkout carrying the checkpoint script
// and, when withBundleHistory is true, a commit that puts the bundle in history.
func capdemoArmRepo(t *testing.T, withBundleHistory bool) string {
	t.Helper()
	root := t.TempDir()
	script := filepath.Join(root, capdemoCheckScriptRel)
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(script, []byte(capdemoArmStubStale), capdemoArmScriptMode); err != nil {
		t.Fatalf("write stub checkpoint: %v", err)
	}
	capdemoArmGit(t, root, "init", "-q")
	capdemoArmGit(t, root, "add", capdemoCheckScriptRel)
	capdemoArmGit(t, root, "commit", "-qm", "fixture: the checkpoint script")
	if !withBundleHistory {
		return root
	}
	bundleFile := filepath.Join(root, capdemoArmBundleFile)
	if err := os.MkdirAll(filepath.Dir(bundleFile), 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	if err := os.WriteFile(bundleFile, []byte("fixture still caption\n"), 0o644); err != nil {
		t.Fatalf("write bundle file: %v", err)
	}
	capdemoArmGit(t, root, "add", capdemoArmBundleFile)
	capdemoArmGit(t, root, "commit", "-qm", "fixture: the bundle lands")
	// The bundle leaves the checkout the way a careless cleanup removes it: from
	// the WORKTREE only, so the commit still carries it.
	if err := os.RemoveAll(filepath.Join(root, capdemoCheckBundleRel)); err != nil {
		t.Fatalf("remove bundle from worktree: %v", err)
	}
	return root
}

// capdemoArmGit runs one git command in a fixture repository, failing the arm on
// a non-zero exit so a broken fixture is never read as a gate verdict.
func capdemoArmGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// TestCapdemoExternalGateHasNoSubjectInACheckoutWithoutTheBundle is P1 — the
// accepted state of a fresh worktree: green, and the reason printed.
func TestCapdemoExternalGateHasNoSubjectInACheckoutWithoutTheBundle(t *testing.T) {
	root := capdemoArmRepo(t, false)
	rec, printed := runCapdemoArm(t, root)
	if failed, why := rec.failed(); failed {
		t.Fatalf("a checkout that never carried the bundle was refused: %s", why)
	}
	if !strings.Contains(printed, capdemoAbsentMarker) {
		t.Fatalf("the gate passed without naming its precondition — a silent pass cannot be told "+
			"from a deleted bundle; printed=%q", printed)
	}
	if !strings.Contains(printed, capdemoCheckBundleRel) {
		t.Errorf("the precondition does not name the bundle it is about: %q", printed)
	}
}

// TestCapdemoExternalGateStillFailsWhenTheBundleWasDeleted is P2 — the arm a
// `t.Skip` would have broken: the bundle is in this checkout's own history, so
// its absence means somebody deleted it.
func TestCapdemoExternalGateStillFailsWhenTheBundleWasDeleted(t *testing.T) {
	root := capdemoArmRepo(t, true)
	rec, _ := runCapdemoArm(t, root)
	failed, why := rec.failed()
	if !failed {
		t.Fatal("a bundle its own history carries was deleted and the gate passed — the absence " +
			"rule swallowed a deletion")
	}
	if !strings.Contains(why, "DELETED") || !strings.Contains(why, capdemoCheckBundleRel) {
		t.Errorf("the refusal does not say the bundle was deleted, nor name it: %s", why)
	}
}

// TestCapdemoExternalGateRefusesAnUnexplainableAbsence is P3 — Rule 15: an
// absence the checkout's history cannot explain is not a pass.
func TestCapdemoExternalGateRefusesAnUnexplainableAbsence(t *testing.T) {
	root := t.TempDir() // no git repository at all
	script := filepath.Join(root, capdemoCheckScriptRel)
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(script, []byte(capdemoArmStubStale), capdemoArmScriptMode); err != nil {
		t.Fatalf("write stub checkpoint: %v", err)
	}
	rec, _ := runCapdemoArm(t, root)
	failed, why := rec.failed()
	if !failed {
		t.Fatal("an absence that no history explains passed the gate")
	}
	if !strings.Contains(why, "could not be read") {
		t.Errorf("the refusal does not say the history was unreadable: %s", why)
	}
}

// TestCapdemoExternalGateStillFailsOnStaleAnchors is P4 — the gate's original
// verdict, re-measured with the bundle PRESENT so the precondition rule cannot
// have traded it away.
func TestCapdemoExternalGateStillFailsOnStaleAnchors(t *testing.T) {
	root := capdemoArmRepo(t, true)
	bundleFile := filepath.Join(root, capdemoArmBundleFile)
	if err := os.MkdirAll(filepath.Dir(bundleFile), 0o755); err != nil {
		t.Fatalf("mkdir bundle: %v", err)
	}
	if err := os.WriteFile(bundleFile, []byte("fixture still caption, re-shot\n"), 0o644); err != nil {
		t.Fatalf("write bundle file: %v", err)
	}
	rec, printed := runCapdemoArm(t, root)
	failed, why := rec.failed()
	if !failed {
		t.Fatalf("a present bundle with stale anchors passed the gate; printed=%q", printed)
	}
	if !strings.Contains(why, capdemoCheckBundleRel) {
		t.Errorf("the stale-anchor refusal does not name the bundle: %s", why)
	}
	if strings.Contains(printed, capdemoAbsentMarker) {
		t.Errorf("the gate reported the no-subject precondition for a bundle that IS present: %q", printed)
	}
}
