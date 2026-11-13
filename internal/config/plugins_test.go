package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plugins_test.go — the ONE resolver's contract, and the anti-drift gate that
// keeps it the ONLY spelling of the web-search scripts path.
//
// The defect this family closes (measured 2026-09-27): the path
// <gopi dir>/plugins/websearch/scripts was spelled by inline literals inside
// internal/tools, so nothing could compare "what the tool resolves" against
// "what a sandbox materialises" — and inside a Rule-34 sandbox the second half
// did not exist at all, so `gopi web search` answered status=unavailable.

// TestWebSearchScriptsDirOverrideWins pins the env channel: the override is the
// first arm, trimmed, and it is what a deployment mirror (or a test) sets.
func TestWebSearchScriptsDirOverrideWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(WebSearchScriptsDirEnvName, "  "+dir+"  ")
	if got := WebSearchScriptsDir(); got != dir {
		t.Errorf("WebSearchScriptsDir() = %q, want the override %q (trimmed)", got, dir)
	}
}

// TestWebSearchScriptsDirDefaultsInsideTheProfile pins the default arm: with no
// override the resolution lands in the profile's own plugin tree — the path the
// sandbox initializer materialises a link at, expressed through the SAME joins
// the initializer uses (WebSearchScriptsDirIn over ProfileDirIn), so creator and
// reader cannot disagree.
func TestWebSearchScriptsDirDefaultsInsideTheProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv(GopiDirEnv, home)
	t.Setenv(WebSearchScriptsDirEnvName, "")

	want := WebSearchScriptsDirIn(ProfileDirIn(home))
	if got := WebSearchScriptsDir(); got != want {
		t.Fatalf("WebSearchScriptsDir() = %q, want %q", got, want)
	}
	// The leaves are the tree's, named once: a change to any of them must move
	// this expectation with it rather than being absorbed by a literal here.
	for _, leaf := range []string{PluginsDirName, WebSearchPluginDirName, WebSearchScriptsDirName} {
		if !strings.Contains(want, string(filepath.Separator)+leaf+string(filepath.Separator)) &&
			!strings.HasSuffix(want, string(filepath.Separator)+leaf) {
			t.Fatalf("the resolved path %q does not carry the leaf %q", want, leaf)
		}
	}
}

// TestWebSearchScriptsDirEnvHasOneSpelling is the Rule 13 exhaustive gate: the
// env NAME may appear as a string literal in exactly ONE production site — its
// definition (plugins.go). Every other face that needs the name (the catalog
// row, the publisher, the help listing) references the constant, exactly as the
// catalog already does for the other shared names. Any other literal is a second
// spelling, which is the drift that produced a tool and a sandbox creator that
// disagreed about where the scripts live.
//
// Consumers reference config.WebSearchScriptsDirEnvName; the check is a grep
// because the property is "the literal occurs nowhere else", which no
// behavioural test can see.
func TestWebSearchScriptsDirEnvHasOneSpelling(t *testing.T) {
	root := repoRootForConfigTest(t)
	allowed := map[string]bool{
		filepath.Join("internal", "config", "plugins.go"): true, // the definition
	}
	var offenders []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if allowed[rel] {
				return nil
			}
			blob, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(blob), `"`+WebSearchScriptsDirEnvName+`"`) {
				offenders = append(offenders, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", dir, err)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("the env name %s is spelled as a literal in %v — reference config.WebSearchScriptsDirEnvName (Rule 13: a name with two spellings has two meanings)",
			WebSearchScriptsDirEnvName, offenders)
	}
}

// repoRootForConfigTest walks up from the test's working directory to the module
// root (the directory holding go.mod) — the scan above is a whole-tree property,
// so it must not be pinned to this package's own directory.
func repoRootForConfigTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}
