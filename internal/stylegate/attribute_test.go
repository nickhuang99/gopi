package stylegate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture's own vocabulary, named (Rule 1/13/14: a test that compares on a
// literal should be reading a constant, and these are the file names, branch
// and line numbers the assertions below are written in terms of).
const (
	attribFixtureInheritedFile = "inherited.go"
	attribFixtureOwnFile       = "own.go"
	attribFixtureUntrackedFile = "untracked.go"
	attribFixtureBranch        = "master"
	// attribFixtureSiteLine is the line carrying the fixture's discard in every
	// fixture file: line 1 is the package clause, line 2 is a blank, line 3 the
	// function and line 4 the discard.
	attribFixtureSiteLine = 4
	// attribFixtureFileBody is the fixture file's content — a real Go file so
	// the reading is exercised against source rather than arbitrary bytes.
	// Line 4 is what a finding points at.
	attribFixtureFileBody = "package fixture\n\nfunc f() {\n\t_ = os.Remove(\"x\")\n}\n"
)

// TestAttributionSeparatesInheritedFromOwn is the reading's own guard: a site
// that was already in the tree at the cut point is INHERITED, a site that
// arrived after it is OWN, and the two are told apart by commit ancestry —
// never by the count, and never by the order the findings happen to arrive in.
//
// The shape it pins is exactly the one this reading exists for: the same rule,
// the same number of sites, one of them belonging to the run and one not. A
// count cannot express that difference; the verdict can.
func TestAttributionSeparatesInheritedFromOwn(t *testing.T) {
	t.Parallel()
	root := attribFixtureRepo(t)

	// The cut point: a run that started HERE.
	cut := attribFixtureCommit(t, root, attribFixtureInheritedFile, "the site that was already here")
	// Work that arrives AFTER the cut point — the run's own.
	own := attribFixtureCommit(t, root, attribFixtureOwnFile, "the site this run added")
	// A third file that is committed but never... left untracked on purpose,
	// below, to prove an undecidable site is reported as unknown.

	findings := []Finding{
		{File: attribFixtureOwnFile, Line: attribFixtureSiteLine, Rule: RuleIgnoredError, Detail: "fixture own"},
		{File: attribFixtureInheritedFile, Line: attribFixtureSiteLine, Rule: RuleIgnoredError, Detail: "fixture inherited"},
	}
	comp, err := Attribute(Options{Root: root}, findings, cut)
	if err != nil {
		t.Fatalf("Attribute(%s, %s): %v", root, cut, err)
	}
	if got, want := len(comp.Sites), len(findings); got != want {
		t.Fatalf("attributed %d of %d site(s)", got, want)
	}
	if comp.Inherited != 1 || comp.Own != 1 || comp.Unknown != 0 {
		t.Fatalf("verdict counts: inherited=%d own=%d unknown=%d, want 1/1/0 — sites:\n%s",
			comp.Inherited, comp.Own, comp.Unknown, attribRender(comp))
	}

	byFile := map[string]Site{}
	for _, s := range comp.Sites {
		byFile[s.Finding.File] = s
	}
	inh, ok := byFile[attribFixtureInheritedFile]
	if !ok {
		t.Fatalf("no site attributed to %s — sites:\n%s", attribFixtureInheritedFile, attribRender(comp))
	}
	if inh.Verdict != AttribInherited {
		t.Errorf("%s:%d: verdict %q, want %q (its commit %s IS the cut point)",
			inh.Finding.File, inh.Finding.Line, inh.Verdict, AttribInherited, cut)
	}
	if inh.Commit != cut {
		t.Errorf("%s:%d: blamed to %s, want %s", inh.Finding.File, inh.Finding.Line, inh.Commit, cut)
	}

	ownSite, ok := byFile[attribFixtureOwnFile]
	if !ok {
		t.Fatalf("no site attributed to %s — sites:\n%s", attribFixtureOwnFile, attribRender(comp))
	}
	if ownSite.Verdict != AttribOwn {
		t.Errorf("%s:%d: verdict %q, want %q (its commit %s is NOT an ancestor of the cut point %s)",
			ownSite.Finding.File, ownSite.Finding.Line, ownSite.Verdict, AttribOwn, own, cut)
	}
	if ownSite.Commit != own {
		t.Errorf("%s:%d: blamed to %s, want %s", ownSite.Finding.File, ownSite.Finding.Line, ownSite.Commit, own)
	}
	if ownSite.Subject != "the site this run added" {
		t.Errorf("%s:%d: subject %q, want the commit's own subject", ownSite.Finding.File, ownSite.Finding.Line, ownSite.Subject)
	}

	// The lines the reading prints must carry BOTH halves — a count-and-verdict
	// string with no commit cannot be checked by a reader.
	rendered := ownSite.String()
	if !strings.Contains(rendered, own[:shortSHALen]) || !strings.Contains(rendered, AttribOwn) {
		t.Errorf("rendered site does not carry its commit and verdict: %q", rendered)
	}
}

// TestAttributionRefusesAnEmptyCutPoint pins the refusal: without a cut point
// the inherited/own question has no answer, and a listing of "unknown" verdicts
// reads like a reading while carrying none of the information.
func TestAttributionRefusesAnEmptyCutPoint(t *testing.T) {
	t.Parallel()
	root := attribFixtureRepo(t)
	attribFixtureCommit(t, root, attribFixtureOwnFile, "anything")

	_, err := Attribute(Options{Root: root}, []Finding{
		{File: attribFixtureOwnFile, Line: attribFixtureSiteLine, Rule: RuleIgnoredError},
	}, "")
	if !errors.Is(err, ErrAttributeNoCutPoint) {
		t.Fatalf("Attribute with an empty cut point: err = %v, want ErrAttributeNoCutPoint", err)
	}
}

// TestAttributionReportsAnUndecidedSiteAsUnknown pins the third verdict: a site
// in a file git does not track cannot be blamed, and the reading says so WITH
// ITS REASON instead of folding it into either decided verdict (Rule 15: no
// silent failure — an undecided site is never guessed into "own").
func TestAttributionReportsAnUndecidedSiteAsUnknown(t *testing.T) {
	t.Parallel()
	root := attribFixtureRepo(t)
	cut := attribFixtureCommit(t, root, attribFixtureInheritedFile, "the baseline")

	// Committed, then modified in the worktree: the line exists, its blame is
	// an uncommitted blob, and the reproduction is the ordinary one — a run
	// asking the question BEFORE it commits its own work.
	attribFixtureCommit(t, root, attribFixtureUntrackedFile, "the file that will be edited")
	attribWrite(t, root, attribFixtureUntrackedFile, attribFixtureFileBody+"\n// edited in the worktree, not yet committed\n")
	if err := os.WriteFile(filepath.Join(root, attribFixtureOwnFile), []byte(attribFixtureFileBody), 0o644); err != nil {
		t.Fatalf("write an untracked fixture file: %v", err)
	}

	comp, err := Attribute(Options{Root: root}, []Finding{
		{File: attribFixtureOwnFile, Line: attribFixtureSiteLine, Rule: RuleIgnoredError, Detail: "untracked file"},
	}, cut)
	if err != nil {
		t.Fatalf("Attribute: %v", err)
	}
	if len(comp.Sites) != 1 {
		t.Fatalf("attributed %d sites, want 1", len(comp.Sites))
	}
	s := comp.Sites[0]
	if s.Verdict != AttribUnknown {
		t.Fatalf("%s:%d: verdict %q, want %q — an untracked file has no introducing commit",
			s.Finding.File, s.Finding.Line, s.Verdict, AttribUnknown)
	}
	if s.Why == "" {
		t.Errorf("%s:%d: unknown verdict with no reason — an undecided site must say why",
			s.Finding.File, s.Finding.Line)
	}
	if comp.Unknown != 1 || comp.Own != 0 || comp.Inherited != 0 {
		t.Errorf("verdict counts: inherited=%d own=%d unknown=%d, want 0/0/1",
			comp.Inherited, comp.Own, comp.Unknown)
	}
}

// attribRender is the failure message's own view of a composition, so a broken
// verdict prints the whole reading rather than only the number that disagreed.
func attribRender(c *Composition) string {
	var b strings.Builder
	for _, s := range c.Sites {
		b.WriteString("  " + s.String() + "\n")
	}
	return b.String()
}

// attribFixtureRepo builds a throwaway repository with one committed Go file and
// returns its root. It is a REAL repo with REAL commits, because the reading's
// whole subject is commit ancestry — a stub cannot exercise it.
func attribFixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	attribGit(t, root, "init", "-q", "-b", attribFixtureBranch)
	attribGit(t, root, "config", "user.email", "fixture@example.invalid")
	attribGit(t, root, "config", "user.name", "fixture")
	attribGit(t, root, "config", "commit.gpgsign", "false")
	return root
}

// attribFixtureCommit writes content to name, commits it with message, and
// returns the new commit's full object name.
func attribFixtureCommit(t *testing.T, root, name, message string) string {
	t.Helper()
	attribWrite(t, root, name, attribFixtureFileBody)
	attribGit(t, root, "add", name)
	attribGit(t, root, "commit", "-q", "-m", message)
	return attribGit(t, root, "rev-parse", "HEAD")
}

// attribWrite materialises one fixture file.
func attribWrite(t *testing.T, root, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// attribGit runs one git verb in root and returns its trimmed stdout.
func attribGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command(gitBin, args...)
	cmd.Dir = root
	// Pin identity and signing through the environment as well: the fixture
	// must commit even when the invoking run has no global git identity.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), root, err, out)
	}
	return strings.TrimSpace(string(out))
}
