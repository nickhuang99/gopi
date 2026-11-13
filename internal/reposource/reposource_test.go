package reposource

// reposource_test.go — the four shapes the retirement turns on, and the ONE
// refusal that replaced the deprecation line.
//
// The cases, in the order the resolver tries the sources:
//
//	① row repo + a cwd in a DIFFERENT repo  → the row wins, no warning
//	② cwd inside a git repo, dev_repo UNSET → resolves from the cwd, silently
//	                                              (the operator's 2026-10-23
//	                                              ruling: this is the NORMAL
//	                                              path, not a deprecated one)
//	③ cwd outside any repo, dev_repo set     → resolves from dev_repo
//	④ nothing anywhere                      → ONE refusal, and it must name
//	                                              ALL THREE sources with their
//	                                              own verdicts
//
// ④ is the anti-regression arm (the briefing's acceptance item 5): the shape
// this replaces reported the LAST source alone ("dev_repo is not configured"),
// which told a reader standing in a broken cwd exactly one third of the truth.
// The assertions below therefore pin the OTHER two source names as strongly as
// they pin dev_repo.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitFixture makes a real git work tree holding one commit, and returns its top
// level. A probe that is answered by a real `git rev-parse` is the instrument
// this package is built on, so the cases use real repositories rather than a
// stubbed probe.
func gitFixture(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid",
			"commit", "-q", "--allow-empty", "-m", "fixture"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
		}
	}
	return samePathFor(t, dir)
}

// samePathFor resolves the fixture's own top level through the SAME instrument
// the resolver uses, so a macOS-style /private symlink or a /tmp symlink cannot
// make a correct answer compare unequal.
func samePathFor(t *testing.T, dir string) string {
	t.Helper()
	top, why := topLevel(dir)
	if top == "" {
		t.Fatalf("the fixture at %s is not a git work tree: %s", dir, why)
	}
	return top
}

// nonRepo returns a real directory that is NOT inside any git work tree.
func nonRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "no-repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if top, _ := topLevel(dir); top != "" {
		// A scratch root that is itself inside a repository would make the
		// "declines" case untestable — say so instead of asserting something
		// false.
		t.Skipf("the test scratch root %s is inside the git work tree %s; skipping the not-a-repo arms", dir, top)
	}
	return dir
}

// TestCWDRepoWithNoDevRepoResolvesSilently is case ②: the operator's ruling in
// one test. A cwd inside a git repository with NO dev_repo configured is the
// normal path — it resolves, and nothing prints.
func TestCWDRepoWithNoDevRepoResolvesSilently(t *testing.T) {
	repo := gitFixture(t, "cwd-repo")
	res, err := Resolve(Input{CWD: repo, DevRepo: ""})
	if err != nil {
		t.Fatalf("a cwd inside a git repository must resolve with dev_repo unset — this is the NORMAL path, "+
			"not a deprecated one (operator ruling 2026-10-23); got: %v", err)
	}
	if res.Dir != repo {
		t.Errorf("resolved %q, want the cwd's own top level %q", res.Dir, repo)
	}
	if res.Source != SourceCWD {
		t.Errorf("resolved from %q, want %q", res.Source, SourceCWD)
	}
}

// TestAllThreeSourcesDeclinedNamesEachOne is case ④ and the anti-regression
// arm: with nothing available, the refusal must carry all three source labels
// AND three distinct measured verdicts — never a single "dev_repo is empty"
// sentence.
func TestAllThreeSourcesDeclinedNamesEachOne(t *testing.T) {
	noRepo := nonRepo(t)
	res, err := Resolve(Input{CWD: noRepo, DevRepo: ""})
	if err == nil {
		t.Fatalf("nothing is available, so the resolution must fail; got %+v", res)
	}
	refusal, ok := err.(*Refusal)
	if !ok {
		t.Fatalf("a failed resolution must be a *Refusal carrying the probes, got %T", err)
	}
	text := refusal.Error()
	// (a) every source is NAMED — a reader learns which three things were tried.
	for _, want := range []string{SourceRowRepo, SourceCWD, SourceDevRepo} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal must name the source %q; it did not:\n%s", want, text)
		}
	}
	// (b) every source carries its OWN verdict, not one shared sentence. The
	// three verdicts are distinct texts by construction; pin the two that are
	// specific to this input, so a regression to "dev_repo is not configured"
	// alone fails here.
	if !strings.Contains(text, noRepo) {
		t.Errorf("the cwd's verdict must quote the directory that was probed (%q):\n%s", noRepo, text)
	}
	if !strings.Contains(text, DeclinedNoDevRepo) {
		t.Errorf("dev_repo's verdict must state that it is unset AND optional:\n%s", text)
	}
	if !strings.Contains(text, DeclinedNoRowRepo) {
		t.Errorf("the row's verdict must be stated even though no row names anything:\n%s", text)
	}
	// (c) the three probes ARE three entries, in the resolution's order.
	if len(refusal.Probes) != 3 {
		t.Fatalf("the refusal must carry one probe per source (3), got %d: %+v", len(refusal.Probes), refusal.Probes)
	}
	wantOrder := []string{SourceRowRepo, SourceCWD, SourceDevRepo}
	for i, want := range wantOrder {
		if refusal.Probes[i].Source != want {
			t.Errorf("probe %d is %q, want %q (the order IS the resolution chain)", i+1, refusal.Probes[i].Source, want)
		}
		if strings.TrimSpace(refusal.Probes[i].Verdict) == "" {
			t.Errorf("probe %q carries no verdict — a source that declined must say why", refusal.Probes[i].Source)
		}
	}
	// (d) and the text offers a next step per source, so a reader knows whether
	// to cd, repair the row, or declare dev_repo.
	for _, want := range []string{"cd into", SourceRowRepo, "dev_repo"} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal must offer the %q remedy:\n%s", want, text)
		}
	}
	// (e) THE ANTI-REGRESSION ARM, spelled at the level of the withdrawn
	// notice's own vocabulary: the state is "no repository", never "deprecated".
	// dev_repo is an OPTIONAL fallback (operator ruling 2026-10-23), so a
	// refusal that calls anything deprecated is the old shape wearing a new
	// label and must fail here.
	if strings.Contains(strings.ToUpper(text), "DEPRECATED") {
		t.Errorf("the refusal must not call any source deprecated — dev_repo is an OPTIONAL fallback, "+
			"not a switch that lapsed:\n%s", text)
	}
}

// TestDevRepoResolvesWhenTheCWDIsNotARepo is case ③.
func TestDevRepoResolvesWhenTheCWDIsNotARepo(t *testing.T) {
	noRepo := nonRepo(t)
	fallback := gitFixture(t, "fallback-repo")
	res, err := Resolve(Input{CWD: noRepo, DevRepo: fallback})
	if err != nil {
		t.Fatalf("a declared dev_repo must resolve when the cwd is not a repo: %v", err)
	}
	if res.Dir != fallback {
		t.Errorf("resolved %q, want the declared dev_repo %q", res.Dir, fallback)
	}
	if res.Source != SourceDevRepo {
		t.Errorf("resolved from %q, want %q", res.Source, SourceDevRepo)
	}
}

// TestDevRepoThroughTheRenderCallback pins the `host:/path` spelling: the
// declared value is not a local path, and the caller's own renderer is what
// turns it into one. Proving the callback is USED (rather than the raw value
// probed) is what keeps a `host:/path` dev_repo from silently failing.
func TestDevRepoThroughTheRenderCallback(t *testing.T) {
	noRepo := nonRepo(t)
	local := gitFixture(t, "rendered-repo")
	declared := "peer:~/work/gopi"
	var sawDeclared string
	res, err := Resolve(Input{
		CWD: noRepo, DevRepo: declared,
		RenderDevRepo: func(v string) (string, error) {
			sawDeclared = v
			return local, nil
		},
	})
	if err != nil {
		t.Fatalf("the rendered dev_repo must resolve: %v", err)
	}
	if sawDeclared != declared {
		t.Errorf("the renderer was handed %q, want the declared value %q", sawDeclared, declared)
	}
	if res.Dir != local || res.Source != SourceDevRepo {
		t.Errorf("resolved (%q, %q), want (%q, %q)", res.Dir, res.Source, local, SourceDevRepo)
	}
}

// TestRowRepoWinsOverTheCWDRepo is case ①: the row is the most specific
// statement, so a run started inside a DIFFERENT repository still works in the
// row's own — and says nothing about the cwd having been ignored.
func TestRowRepoWinsOverTheCWDRepo(t *testing.T) {
	rowRepo := gitFixture(t, "row-repo")
	cwdRepo := gitFixture(t, "cwd-repo")
	res, err := Resolve(Input{RowRepo: rowRepo, CWD: cwdRepo})
	if err != nil {
		t.Fatalf("a row naming a repo must resolve: %v", err)
	}
	if res.Dir != rowRepo {
		t.Errorf("resolved %q, want the row's own repo %q", res.Dir, rowRepo)
	}
	if res.Source != SourceRowRepo {
		t.Errorf("resolved from %q, want %q", res.Source, SourceRowRepo)
	}
}

// TestRowRepoThatIsNotARepoFallsThrough pins the ORDER's other half: a row
// naming a directory git cannot answer for does not stop the chain — the cwd
// still decides — and the refusal (when it does fire) records what the row
// actually offered.
func TestRowRepoThatIsNotARepoFallsThrough(t *testing.T) {
	noRepo := nonRepo(t)
	cwdRepo := gitFixture(t, "cwd-repo")
	res, err := Resolve(Input{RowRepo: noRepo, CWD: cwdRepo})
	if err != nil {
		t.Fatalf("a dead row repo must not stop the chain: %v", err)
	}
	if res.Source != SourceCWD || res.Dir != cwdRepo {
		t.Errorf("resolved (%q, %q), want the cwd (%q, %q)", res.Dir, res.Source, cwdRepo, SourceCWD)
	}
	// The row's probe is still on the record, with the directory it named.
	if len(res.Probes) == 0 || res.Probes[0].Source != SourceRowRepo {
		t.Fatalf("the row's probe must be recorded first, got %+v", res.Probes)
	}
	if res.Probes[0].Declared != noRepo {
		t.Errorf("the row's probe recorded %q, want the named directory %q", res.Probes[0].Declared, noRepo)
	}
}
