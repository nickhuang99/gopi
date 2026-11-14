package standardgate

// The `man` target has TWO legs, and only ONE of them is mandatory. The first
// leg regenerates the tracked man/*.md sources from the built binary's `-h`
// pages (internal/standards/testdata/mancensus.go); the second RENDERS them to
// untracked .1 pages under bin/man/man1 and needs `pandoc`, an OPTIONAL
// dependency (Rule 35: the recipe adds no script, service or state file of its
// own).
//
// Measured 2026-10-14 (peer1, task 01a139cb065075a39186da7c0ad2d8bb): the
// shipped recipe ran the render call under `set -e` with no guard, so on a host
// without pandoc `make man` died with exit 127 — `/bin/sh: 1: pandoc: not
// found` — AFTER the source leg had already succeeded, printing no hint that
// man/*.md had in fact been regenerated. A user reads 127 as "the man pipeline
// is broken", while the only casualty is a regenerable artifact (Rule 39).
//
// Rule 15 is the norm this gate asserts: a missing renderer is not silent
// information. The broken run must say WHAT it skipped, WHAT did happen, and
// WHERE to verify — `make man-check`.
//
// Three things are checked, and each has a planted arm that must FIRE:
//   - the render call is guarded at all (the pre-fix recipe is the plant);
//   - the guard does not delete the rendering (a `command -v` that never calls
//     pandoc is a different bug, not a fix);
//   - the skip is LOUD and POINTS at `make man-check` (a bare guard with no
//     notice is the silent arm).
//
// And one arm RUNS the shipped guard rather than reading it: the recipe's own
// `if command -v pandoc …` block is extracted, its make variables substituted,
// and executed twice under /bin/sh — once with a PATH that cannot resolve
// pandoc, once with a PATH that carries a shim which records its arguments. The
// shipped text is the input, so a future edit that reads fine but selects the
// wrong branch fails here.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// manTargetHeader is the target under test, spelled as the Makefile spells
	// it (Rule 13: a target name is a keyword, not a literal to retype).
	manTargetHeader = "man: build"
	// manCheckTargetName is the sibling target the skip notice must point at.
	manCheckTargetName = "man-check"
	// manRecipeIndent is the byte that makes a Makefile line a recipe line.
	manRecipeIndent = '\t'
	// pandocCommandName is the OPTIONAL renderer: the one external program whose
	// absence must not fail this target. Referenced (never bare-written) by the
	// guard probe and the invocation assertion below (Rule 13).
	pandocCommandName = "pandoc"
	// manRenderInvocation is the render leg's real call — its absence means the
	// leg was deleted rather than made optional.
	manRenderInvocation = pandocCommandName + " -s -t man"
	// manSkipNoticeMarker is the loud-skip token the notice must carry.
	manSkipNoticeMarker = "SKIPPED"
	// manRenderedNoticeMarker is the success token, and it must NOT appear on
	// the skip arm: a notice that prints both would be noise, not information.
	manRenderedNoticeMarker = "rendered"
	// manSourceNoticeMarker names what DID happen when the render was skipped.
	manSourceNoticeMarker = "regenerated"
	// manPageNamesFixture stands in for $(MAN_PAGE_NAMES) when the guard block is
	// executed: the arm under test is the branch SELECTION, and the shim ignores
	// the page list.
	manPageNamesFixture = "gopi gopi-chat"
	// manShimLogEnv is the environment name the planted shim appends its argv to.
	manShimLogEnv = "MAN_RENDER_SHIM_LOG"
	// manMinRecipeBytes is the read-floor (Rule 15): a recipe this reader failed
	// to find must FAIL rather than read as green. It is deliberately small —
	// the PRE-FIX recipe measured 324 bytes and the fixed one ~1.4 KB, so the
	// floor only has to separate "a recipe was read" from "nothing was read",
	// never to discriminate between the shapes (that is manRenderLegFindings'
	// job).
	manMinRecipeBytes = 96
)

// manRenderLegFinding is one reason the render leg is not optional-and-loud.
type manRenderLegFinding struct {
	arm    string
	detail string
}

// manRecipe returns the recipe body of the shipped `man` target — the
// tab-indented lines following its header, indentation stripped.
func manRecipe(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if line != manTargetHeader {
			continue
		}
		var out []string
		for _, l := range lines[i+1:] {
			if !strings.HasPrefix(l, string(manRecipeIndent)) {
				break
			}
			out = append(out, strings.TrimLeft(l, " \t"))
		}
		return strings.Join(out, "\n")
	}
	return ""
}

// manRenderLegFindings applies the predicate to a recipe body.
func manRenderLegFindings(recipe string) []manRenderLegFinding {
	var out []manRenderLegFinding
	guarded := strings.Contains(recipe, "command -v "+pandocCommandName)
	if !guarded {
		out = append(out, manRenderLegFinding{
			arm: "unguarded-render",
			detail: "the render leg calls " + pandocCommandName + " unconditionally under `set -e`, so a host " +
				"without it dies with exit 127 after the source leg already succeeded (measured 2026-10-14)",
		})
	}
	if !strings.Contains(recipe, manRenderInvocation) {
		out = append(out, manRenderLegFinding{
			arm: "render-deleted",
			detail: "the recipe no longer invokes `" + manRenderInvocation + "`: making the render optional must not " +
				"remove it — with " + pandocCommandName + " present the behaviour has to stay exactly as it is",
		})
	}
	if guarded && !strings.Contains(recipe, manSkipNoticeMarker) {
		out = append(out, manRenderLegFinding{
			arm:    "silent-skip",
			detail: "the guard skips the render without printing anything (Rule 15: a missing renderer must be reported, not swallowed)",
		})
	}
	if guarded && !strings.Contains(recipe, manCheckTargetName) {
		out = append(out, manRenderLegFinding{
			arm:    "skip-points-nowhere",
			detail: "the skip notice does not point the reader at `make " + manCheckTargetName + "`, the check that proves the sources were regenerated",
		})
	}
	if guarded && !strings.Contains(recipe, manSourceNoticeMarker) {
		out = append(out, manRenderLegFinding{
			arm:    "skip-hides-the-source-leg",
			detail: "the skip notice does not say that the source leg DID run, which is the whole reason 127 read as a broken pipeline",
		})
	}
	return out
}

// scanManRenderLeg reads the Makefile and returns the recipe plus its findings.
// A recipe that could not be found is an ERROR, never an empty green.
func scanManRenderLeg(path string) (string, []manRenderLegFinding, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("reading %s: %w", path, err)
	}
	recipe := manRecipe(string(raw))
	if len(recipe) < manMinRecipeBytes {
		return recipe, nil, fmt.Errorf("%s carries no %q recipe (read %d bytes, floor %d): a scan that found "+
			"nothing must not look like a pass", path, manTargetHeader, len(recipe), manMinRecipeBytes)
	}
	return recipe, manRenderLegFindings(recipe), nil
}

// TestManRenderLegIsOptionalAndLoud is the gate over the shipped tree.
func TestManRenderLegIsOptionalAndLoud(t *testing.T) {
	recipe, findings, err := scanManRenderLeg(filepath.Join(repoRoot(t), makefileName))
	if err != nil {
		t.Fatalf("%v\nA scan that could not read the recipe is a FAILURE, never a green gate.", err)
	}
	if len(findings) > 0 {
		var lines []string
		for _, f := range findings {
			lines = append(lines, "  ["+f.arm+"] "+f.detail)
		}
		t.Errorf("%s: the %q target's render leg is not optional-and-loud:\n%s\n"+
			"The target has two legs and only the first is mandatory. Guard the render leg with "+
			"`command -v %s`, keep the render call when the renderer IS present, and on the absent arm print "+
			"what was skipped, that the sources were regenerated, and that `make %s` verifies them "+
			"(Rule 15), exiting 0.\n\nrecipe under test:\n%s",
			makefileName, manTargetHeader, strings.Join(lines, "\n"),
			pandocCommandName, manCheckTargetName, recipe)
	}
}

// preFixManRecipe is the recipe as it shipped before the fix (commit 63a11386f),
// verbatim — the plant for the arm that must FIRE.
const preFixManRecipe = "@mkdir -p $(MAN_OUT_DIR)\n" +
	"go run internal/standards/testdata/mancensus.go . $(BIN_PATH)\n" +
	"@set -e; for p in $(MAN_PAGE_NAMES); do \\\n" +
	"  pandoc -s -t man $(MAN_SRC_DIR)/$$p.md -o $(MAN_OUT_DIR)/$$p.1; \\\n" +
	"done\n" +
	"@echo \"man: rendered $(MAN_OUT_DIR)/ for: $(MAN_PAGE_NAMES)\"\n" +
	"@echo \"man: read them with  MANPATH=$(CURDIR)/bin/man man gopi\""

// TestManRenderLegRedProof drives the predicate over planted recipes: the arms
// that MUST fire, and the arms that must stay silent so the gate cannot be
// satisfied by refusing everything.
func TestManRenderLegRedProof(t *testing.T) {
	guardedOK := "@set -e; if command -v pandoc >/dev/null 2>&1; then \\\n" +
		"  pandoc -s -t man man/$p.md -o out/$p.1; \\\n" +
		"  echo \"man: rendered out/\"; \\\n" +
		"else \\\n" +
		"  echo \"man: SKIPPED the render leg - the source step regenerated man/*.md\"; \\\n" +
		"  echo \"man: verify with  make man-check\"; \\\n" +
		"fi"

	cases := []struct {
		name       string
		recipe     string
		wantArms   []string
		wantSilent bool
	}{
		{
			name:     "the pre-fix recipe: an unguarded render call",
			recipe:   preFixManRecipe,
			wantArms: []string{"unguarded-render"},
		},
		{
			name: "silent skip: guarded, but `|| true` and no notice at all",
			recipe: "@set -e; for p in man; do \\\n" +
				"  pandoc -s -t man man/$p.md -o out/$p.1 || true; \\\n" +
				"done",
			wantArms: []string{"unguarded-render"},
		},
		{
			name: "guard with no else branch: the skip prints nothing",
			recipe: "@set -e; if command -v pandoc >/dev/null 2>&1; then \\\n" +
				"  pandoc -s -t man man/$p.md -o out/$p.1; \\\n" +
				"fi",
			wantArms: []string{"silent-skip", "skip-points-nowhere", "skip-hides-the-source-leg"},
		},
		{
			name: "skip notice that names nothing to verify with",
			recipe: "@set -e; if command -v pandoc >/dev/null 2>&1; then \\\n" +
				"  pandoc -s -t man man/$p.md -o out/$p.1; \\\n" +
				"else \\\n" +
				"  echo \"man: SKIPPED the render leg; the source step regenerated man/*.md\"; \\\n" +
				"fi",
			wantArms: []string{"skip-points-nowhere"},
		},
		{
			name: "render deleted instead of made optional",
			recipe: "@set -e; if command -v pandoc >/dev/null 2>&1; then \\\n" +
				"  echo \"man: SKIPPED\" \"regenerated\"; echo \"make man-check\"; \\\n" +
				"fi",
			wantArms: []string{"render-deleted"},
		},
		{
			name:       "the shape the fix must have stays silent",
			recipe:     guardedOK,
			wantSilent: true,
		},
	}

	for _, tc := range cases {
		got := manRenderLegFindings(tc.recipe)
		seen := map[string]bool{}
		for _, f := range got {
			seen[f.arm] = true
		}
		if tc.wantSilent {
			if len(got) > 0 {
				t.Errorf("%s: the planted recipe is the shape the gate accepts, but it was reported: %v", tc.name, got)
			}
			continue
		}
		if len(got) == 0 {
			t.Errorf("%s: NOTHING was reported — the gate is vacuous on this arm", tc.name)
			continue
		}
		for _, arm := range tc.wantArms {
			if !seen[arm] {
				t.Errorf("%s: arm %q did not fire; got %v", tc.name, arm, got)
			}
		}
	}
}

// TestManRenderLegRefusesAnEmptyRead pins the read-floor (Rule 15).
func TestManRenderLegRefusesAnEmptyRead(t *testing.T) {
	empty := filepath.Join(t.TempDir(), makefileName)
	if err := os.WriteFile(empty, []byte("# no recipes here\n"), 0o644); err != nil {
		t.Fatalf("writing an empty makefile: %v", err)
	}
	if _, _, err := scanManRenderLeg(empty); err == nil {
		t.Fatal("a Makefile with no `man` recipe was accepted: the read-floor did not fire, so the gate could read green-blind")
	}
	if _, _, err := scanManRenderLeg(filepath.Join(t.TempDir(), "absent-Makefile")); err == nil {
		t.Fatal("a missing Makefile was accepted: a scan that never ran must not look like a pass")
	}
}

// manGuardScript extracts the shipped guard block (`if command -v pandoc … fi`)
// from a recipe and substitutes the make variables it references, so the
// SHIPPED text — not a retyped copy — is what gets executed.
func manGuardScript(t *testing.T, recipe string) string {
	t.Helper()
	lines := strings.Split(recipe, "\n")
	start := -1
	for i, l := range lines {
		if strings.Contains(l, "command -v "+pandocCommandName) {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("the recipe has no guard block to extract (no `command -v %s`):\n%s", pandocCommandName, recipe)
	}
	end := -1
	for j := start; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "fi" {
			end = j
			break
		}
	}
	if end < 0 {
		t.Fatalf("the recipe's guard block has no `fi`:\n%s", recipe)
	}
	script := strings.Join(lines[start:end+1], "\n")
	for _, sub := range [][2]string{
		{"$(MAN_SRC_DIR)", "man"},
		{"$(MAN_OUT_DIR)", "bin/man/man1"},
		{"$(MAN_PAGE_NAMES)", manPageNamesFixture},
		{"$(CURDIR)", "/nonexistent"},
		{"$$p", "$p"},
	} {
		script = strings.ReplaceAll(script, sub[0], sub[1])
	}
	return strings.TrimPrefix(script, "@")
}

// runManGuard executes the shipped guard block under /bin/sh with PATH set to
// exactly dirs (so the caller decides whether `pandoc` resolves).
func runManGuard(t *testing.T, script string, dirs []string, extraEnv []string) (string, int) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = t.TempDir()
	cmd.Env = append([]string{"PATH=" + strings.Join(dirs, ":"), "HOME=" + t.TempDir()}, extraEnv...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("running the guard block: %v (output: %s)", err, out)
		}
	}
	return string(out), code
}

// asExitError is a tiny errors.As wrapper kept local so this file needs no
// import beyond the stdlib's os/exec surface it already uses.
func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// TestManRenderLegGuardSelectsTheRightBranch runs the shipped guard block twice:
// once with a PATH that cannot resolve the renderer (the skip arm — the measured
// peer1 case), once with a PATH carrying a shim that records its argv (the
// render arm). Reading the recipe cannot tell these apart; executing it can.
func TestManRenderLegGuardSelectsTheRightBranch(t *testing.T) {
	recipe, _, err := scanManRenderLeg(filepath.Join(repoRoot(t), makefileName))
	if err != nil {
		t.Fatalf("%v", err)
	}
	script := manGuardScript(t, recipe)

	shimDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(shimDir, pandocCommandName),
		[]byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$"+manShimLogEnv+"\"\n"), 0o755); err != nil {
		t.Fatalf("planting the %s shim: %v", pandocCommandName, err)
	}
	logPath := filepath.Join(t.TempDir(), "shim.log")

	absentOut, absentCode := runManGuard(t, script, []string{t.TempDir()}, nil)
	if absentCode != 0 {
		t.Errorf("the skip arm exited %d, want 0: a host without %s must still complete the target\noutput:\n%s",
			absentCode, pandocCommandName, absentOut)
	}
	if !strings.Contains(absentOut, manSkipNoticeMarker) {
		t.Errorf("the skip arm printed no %q notice, so the skipped render is SILENT (Rule 15)\noutput:\n%s",
			manSkipNoticeMarker, absentOut)
	}
	if !strings.Contains(absentOut, manCheckTargetName) {
		t.Errorf("the skip arm does not point at `make %s` — the reader is left with no way to verify\noutput:\n%s",
			manCheckTargetName, absentOut)
	}
	if strings.Contains(absentOut, manRenderedNoticeMarker) {
		t.Errorf("the skip arm claims the pages were rendered when no renderer was present\noutput:\n%s", absentOut)
	}

	presentOut, presentCode := runManGuard(t, script, []string{shimDir}, []string{manShimLogEnv + "=" + logPath})
	if presentCode != 0 {
		t.Errorf("the render arm exited %d, want 0, with %s present on PATH\noutput:\n%s",
			presentCode, pandocCommandName, presentOut)
	}
	if strings.Contains(presentOut, manSkipNoticeMarker) {
		t.Errorf("the render arm skipped even though %s was on PATH — the guard selects the wrong branch\noutput:\n%s",
			pandocCommandName, presentOut)
	}
	if !strings.Contains(presentOut, manRenderedNoticeMarker) {
		t.Errorf("the render arm printed no success notice, so the render became invisible\noutput:\n%s", presentOut)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("the shim recorded nothing: the render leg did not call %s at all (%v)", pandocCommandName, err)
	}
	invocations := strings.Split(strings.TrimSpace(string(raw)), "\n")
	wantCalls := len(strings.Fields(manPageNamesFixture))
	if len(invocations) != wantCalls {
		t.Fatalf("%s was invoked %d times, want %d (one per page name)\nlog:\n%s",
			pandocCommandName, len(invocations), wantCalls, raw)
	}
	for _, inv := range invocations {
		if !strings.Contains(inv, "-s -t man") {
			t.Errorf("the render invocation lost its arguments: %q does not carry `-s -t man`", inv)
		}
		if !strings.Contains(inv, "bin/man/man1/") {
			t.Errorf("the render invocation writes outside $(MAN_OUT_DIR): %q", inv)
		}
	}
}
