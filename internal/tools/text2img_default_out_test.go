// text2img_default_out_test.go — the REGRESSION GUARD for the text2img family's
// default output when no `out` is given (measured 2026-11-07, task 01a1b3cf).
//
// THE DEFECT IT PINS: `gopi tool text2img <src>` and the model's `text2img` tool
// both reached ONE writer (tools.Text2ImgDetail → text2img.RenderFile) whose
// `out == ""` branch named `<src 去扩展名>.png` — A FILE BESIDE THE SOURCE. When
// src lives inside a git work tree — and a task run's cwd IS its clone ground —
// the render leaves an untracked PNG in that tree: PROJECT.md Rule 30's exact
// defect (`completed-unclean` at close-out, and no merge path collects it). This
// is the capture family's defect (task 01a1b3b1, commit 9988901f) one tool over,
// and it gets the same answer: a DECLARED scratch class tree that an existing GC
// arm already walks.
//
// THE ASSERTIONS ARE THE ACCEPTANCE CRITERIA THEMSELVES:
//
//	a. the git fixture is clean after the render (git status --porcelain empty);
//	b. the reported path is ABSOLUTE and inside scratch.Text2ImgDumpRoot();
//	c. the NAME is accepted by scratch.IsText2ImgDumpName — a destination no
//	   collector accepts is not a fix, only a different place to leak;
//	d. EVERY PAGE of a multi-page default render is accepted, and the page
//	   number sits BEFORE the class suffix (`<stem>-2.dump.png`), never after it;
//	e. an explicit out is still honoured verbatim (the pre-existing contract,
//	   which this change must not disturb).
//
// On the pre-fix writer (a), (b) and (c) are all red for the same invocation:
// the path was `<fixture>/probe.png`, the repository gained `?? probe.png`.
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/text2img"
)

// text2imgOutputPaths extracts the `output: <path>` lines a render summary
// carries — the ONLY surface the CLI and the model tool both report, so the test
// judges what a caller actually sees.
func text2imgOutputPaths(summary string) []string {
	var paths []string
	for _, line := range strings.Split(summary, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "output:"); ok {
			paths = append(paths, strings.TrimSpace(p))
		}
	}
	return paths
}

// TestDefaultText2ImgOutLeavesTheGroundClean is the regression guard proper: no
// `out`, standing inside a real git repository.
func TestDefaultText2ImgOutLeavesTheGroundClean(t *testing.T) {
	// Pin the scratch home so every assertion is about a directory this test
	// owns (Rule 17: t.Setenv, never os.Setenv).
	home := t.TempDir()
	t.Setenv(scratch.HomeDirEnvName, home)

	repo := captureFixtureGitRepo(t) // the same minimal real-git fixture the capture guard uses
	if dirty := captureFixturePorcelain(t, repo); dirty != "" {
		t.Fatalf("fixture repository is dirty before the render: %q", dirty)
	}
	// The defect was cwd/src-dependent, so the test must stand IN the
	// repository (cwd == clone ground is the task-run shape), and src must live
	// there too — that is exactly the file the old default wrote beside.
	src := filepath.Join(repo, "probe.txt")
	if err := os.WriteFile(src, []byte("hello text2img\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	summary, err := Text2Img(src, "", text2img.Options{})
	if err != nil {
		t.Fatalf("Text2Img (default out): %v", err)
	}
	pages := text2imgOutputPaths(summary)
	if len(pages) != 1 {
		t.Fatalf("summary names %d page(s), want 1:\n%s", len(pages), summary)
	}
	out := pages[0]

	root, rerr := scratch.Text2ImgDumpRoot()
	if rerr != nil || root == "" {
		t.Fatalf("Text2ImgDumpRoot = %q, %v — the suite must have a scratch home", root, rerr)
	}
	if !filepath.IsAbs(out) {
		t.Errorf("default output %q is not absolute — a relative path is resolved against the caller's cwd, which is the defect", out)
	}
	if filepath.Dir(out) != root {
		t.Errorf("default output %q is not inside the class root %q", out, root)
	}
	if !scratch.IsText2ImgDumpName(filepath.Base(out)) {
		t.Errorf("default output name %q is refused by scratch.IsText2ImgDumpName — no sweep would ever collect it", filepath.Base(out))
	}
	if _, serr := os.Stat(out); serr != nil {
		t.Fatalf("reported page %s is not on disk: %v", out, serr)
	}
	// The old default's exact filename must NOT exist — this is the line that
	// goes red on the pre-fix writer even if everything above were satisfied by
	// accident.
	if legacy := strings.TrimSuffix(src, filepath.Ext(src)) + ".png"; fileExists(legacy) {
		t.Errorf("legacy default %s was written beside the source (the defect)", legacy)
	}

	// (a) the ground stays clean apart from the input this test itself created.
	dirty := captureFixturePorcelain(t, repo)
	var leftover []string
	for _, line := range strings.Split(strings.TrimSpace(dirty), "\n") {
		if line == "" || strings.HasSuffix(line, "probe.txt") {
			continue
		}
		leftover = append(leftover, line)
	}
	if len(leftover) != 0 {
		t.Fatalf("the render polluted the git ground: %v (git status --porcelain was %q)", leftover, dirty)
	}
}

// TestDefaultText2ImgMultiPageNamesAreAllCollectable pins (d): a default render
// that paginates must produce names the reclaim predicate ACCEPTS, every page,
// with the page number before the class suffix.
func TestDefaultText2ImgMultiPageNamesAreAllCollectable(t *testing.T) {
	home := t.TempDir()
	t.Setenv(scratch.HomeDirEnvName, home)

	src := filepath.Join(t.TempDir(), "long.txt")
	if err := os.WriteFile(src, []byte("line one\nline two\nline three\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, err := Text2Img(src, "", text2img.Options{Lines: 1})
	if err != nil {
		t.Fatalf("Text2Img (default out, Lines:1): %v", err)
	}
	pages := text2imgOutputPaths(summary)
	if len(pages) < 2 {
		t.Fatalf("default multi-page render produced %d page(s), want >= 2:\n%s", len(pages), summary)
	}
	root, rerr := scratch.Text2ImgDumpRoot()
	if rerr != nil || root == "" {
		t.Fatalf("Text2ImgDumpRoot = %q, %v", root, rerr)
	}
	for i, p := range pages {
		if filepath.Dir(p) != root {
			t.Errorf("page %d (%s) is not inside the class root %q", i+1, p, root)
		}
		name := filepath.Base(p)
		if !scratch.IsText2ImgDumpName(name) {
			t.Errorf("page %d name %q is refused by scratch.IsText2ImgDumpName — that page would never be collected", i+1, name)
		}
		if !strings.HasSuffix(name, scratch.Text2ImgDumpFileSuffix) {
			t.Errorf("page %d name %q does not end in the class suffix %q (the page number must sit BEFORE it, not after)",
				i+1, name, scratch.Text2ImgDumpFileSuffix)
		}
		if _, serr := os.Stat(p); serr != nil {
			t.Errorf("page %d (%s) is not on disk: %v", i+1, p, serr)
		}
	}
}

// TestExplicitText2ImgOutIsHonouredVerbatim pins (e): the pre-existing explicit
// contract is untouched — the caller's path is used as given, pagination appends
// -N to its stem, and nothing about the default changes it.
func TestExplicitText2ImgOutIsHonouredVerbatim(t *testing.T) {
	home := t.TempDir()
	t.Setenv(scratch.HomeDirEnvName, home)

	dir := t.TempDir()
	src := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(src, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "render.png")

	summary, err := Text2Img(src, out, text2img.Options{Lines: 1})
	if err != nil {
		t.Fatalf("Text2Img (explicit out): %v", err)
	}
	pages := text2imgOutputPaths(summary)
	if len(pages) < 2 {
		t.Fatalf("explicit-out pagination produced %d page(s), want >= 2:\n%s", len(pages), summary)
	}
	for i, p := range pages {
		if filepath.Dir(p) != dir || !strings.HasPrefix(filepath.Base(p), "render-") || !strings.HasSuffix(p, ".png") {
			t.Errorf("page %d path %q must be <explicit out stem>-N.png in %s", i+1, p, dir)
		}
	}
	// The explicit destination is the caller's; the class tree must NOT have
	// collected this render instead.
	root, rerr := scratch.Text2ImgDumpRoot()
	if rerr != nil {
		t.Fatalf("resolve the default class root: %v", rerr)
	}
	if root != "" {
		if _, serr := os.Stat(root); serr == nil {
			entries, derr := os.ReadDir(root)
			if derr != nil {
				t.Fatalf("read the default class tree %s: %v", root, derr)
			}
			if len(entries) != 0 {
				t.Errorf("an explicit --out still wrote into the default class tree %s: %d entr(ies)", root, len(entries))
			}
		}
	}
}
