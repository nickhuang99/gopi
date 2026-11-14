package sandbox

// plugins_test.go — the PROFILE'S PLUGIN TREE, driven through the REAL
// initializer and the REAL resolver.
//
// The defect this pins (measured 2026-09-27): `gopi tool sandbox create` produced a
// profile with no `plugins` entry, so inside it the web-search tool resolved
// <sandbox>/.gopi/plugins/websearch/scripts — a directory nobody created — and
// answered {"status":"unavailable",…}. Every test below asserts a property of
// that link, and each would have failed on the pre-fix tree.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/testutil"
)

// linkPluginTree is a convenience for the tests that only care about the link
// (not about the source resolution): a sandbox created with an explicit fixture
// tree.
func linkPluginTree(t *testing.T, root string) (*Sandbox, string) {
	t.Helper()
	tree := pluginsTree(t, root)
	sb, err := Create(root, "", Options{
		Label:               "plug",
		ProfileSettingsPath: settingsFile(t, root),
		PluginsPath:         tree,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return sb, tree
}

// TestPluginTreeIsALinkReadOnlyAndIdempotent asserts the SHAPE of the entry: a
// symlink whose target exists (never a copy), replacing a stale link rather than
// writing through it, and a second Ensure provably not fighting the first.
func TestPluginTreeIsALinkReadOnlyAndIdempotent(t *testing.T) {
	root := demoRoot(t)
	sb, tree := linkPluginTree(t, root)

	link := config.ProfilePathIn(sb.Path, config.PluginsDirName)
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("the sandbox profile carries no %s entry: %v", config.PluginsDirName, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink (mode %s) — a COPY drifts the moment the operator updates the real tree", link, info.Mode())
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("read the plugin-tree link: %v", err)
	}
	if target != tree {
		t.Errorf("the link points at %s, want the tree the caller named (%s)", target, tree)
	}
	if got, want := sb.PluginsLink, tree; got != want {
		t.Errorf("Sandbox.PluginsLink = %q, want %q", got, want)
	}

	// A sandbox whose link already exists is REPAIRED, not duplicated: a second
	// Ensure is a no-op, and a link that was re-pointed is corrected (the
	// settings link's own contract, one directory higher).
	if _, err := Ensure(sb.Path, sb.Root, sb.RootSource, Options{
		ProfileSettingsPath: settingsFile(t, root),
		PluginsPath:         tree,
	}); err != nil {
		t.Fatalf("Ensure on an already-shaped sandbox: %v", err)
	}
	if cur, err := os.Readlink(link); err != nil || cur != tree {
		t.Fatalf("after a second Ensure the link is %q (err %v), want %s", cur, err, tree)
	}

	// A stale link is REPLACED (unlink+link), and the directory it pointed at is
	// untouched by that replacement.
	other := pluginsTree(t, root)
	if err := os.Remove(link); err != nil {
		t.Fatalf("prepare the stale-link case: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "nowhere"), link); err != nil {
		t.Fatalf("plant the stale link: %v", err)
	}
	if _, err := Ensure(sb.Path, sb.Root, sb.RootSource, Options{
		// The settings SOURCE is named explicitly: this fixture tree has no
		// ambient settings file (the suite points GOPI_SETTINGS at a
		// deliberately absent path — testutil/suiteenv.go), and since 2026-09-27
		// the profile's settings entry is a COPY, so the source is READ (a
		// missing one is a loud error) where a symlink only recorded a name.
		ProfileSettingsPath: settingsFile(t, root),
		PluginsPath:         other,
	}); err != nil {
		t.Fatalf("Ensure with a stale plugin link: %v", err)
	}
	if cur, err := os.Readlink(link); err != nil || cur != other {
		t.Errorf("the stale link was not replaced: %q (err %v), want %s", cur, err, other)
	}
}

// TestPluginTreeResolvesWhereTheToolLooks is the LOAD-BEARING assertion: with
// GOPI_HOME pointing at the sandbox, the ONE resolver the web-search tool uses
// answers a directory that the initializer MATERIALISED. Creator and reader are
// two halves of one path here; the pre-fix tree failed at the very first check.
func TestPluginTreeResolvesWhereTheToolLooks(t *testing.T) {
	root := demoRoot(t)

	// The DERIVED source (no Options.PluginsPath): the fixture tree is what the
	// resolver points at, so the link follows the same derivation production
	// uses — the resolver is asked, not assumed.
	tree := pluginsTree(t, root)
	scripts := filepath.Join(tree, config.WebSearchPluginDirName, config.WebSearchScriptsDirName)
	t.Setenv(config.WebSearchScriptsDirEnvName, scripts)

	sb, err := Create(root, "", Options{
		Label:               "derived",
		ProfileSettingsPath: settingsFile(t, root),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sb.PluginsLink != tree {
		t.Errorf("PluginsLink = %q, want the tree the resolver descends from (%s)", sb.PluginsLink, tree)
	}

	// The reader's own resolution, under THIS sandbox as GOPI_HOME and with the
	// deployment override cleared — i.e. exactly what a child process started
	// with GOPI_HOME=<sandbox> resolves.
	t.Setenv(config.GopiDirEnv, sb.Path)
	t.Setenv(config.WebSearchScriptsDirEnvName, "")
	resolved := config.WebSearchScriptsDir()
	if got, want := resolved, config.WebSearchScriptsDirIn(config.ProfileDirIn(sb.Path)); got != want {
		t.Fatalf("inside the sandbox the tool resolves %s, want %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(resolved, sandboxFixtureScriptName)); err != nil {
		t.Fatalf("the directory the tool resolves holds no %s: %v — the sandbox is still dead for web search",
			sandboxFixtureScriptName, err)
	}
}

// TestPluginTreeAbsentIsReportedNotSwallowed pins Rule 15 for this entry: a host
// with no plugin tree still gets a sandbox, but the initializer NAMES the source
// it looked for and leaves the link empty — the CLI prints that as the create
// line's `plugins=…(absent-not-linked)`. A silent skip would be the pre-fix
// shape: a profile that looks complete and fails much later.
func TestPluginTreeAbsentIsReportedNotSwallowed(t *testing.T) {
	root := demoRoot(t)
	missing := filepath.Join(testutil.TestScratchDir(t, "sandbox"), "no-such-tree")
	sb, err := Create(root, "", Options{
		Label:               "absent",
		ProfileSettingsPath: settingsFile(t, root),
		PluginsPath:         missing,
	})
	if err != nil {
		t.Fatalf("Create on a host with no plugin tree must succeed (the sandbox is still valid): %v", err)
	}
	if sb.PluginsLink != "" {
		t.Errorf("PluginsLink = %q, want empty — nothing was linked", sb.PluginsLink)
	}
	if sb.PluginsSource != missing {
		t.Errorf("PluginsSource = %q, want the resolved source it looked for (%s)", sb.PluginsSource, missing)
	}
	link := config.ProfilePathIn(sb.Path, config.PluginsDirName)
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("%s exists (%v) although its source does not — an absent source must not be materialised as a dangling link", link, err)
	}
}

// TestPluginTreeInsideTheSandboxIsRefused is the self-referential shape: a link
// to a directory INSIDE the sandbox would make the sandbox configured by its own
// contents (the settings link refuses the same way). It is an ERROR, not a
// skip — the caller asked for a source that cannot mean what it says.
func TestPluginTreeInsideTheSandboxIsRefused(t *testing.T) {
	root := demoRoot(t)
	settings := settingsFile(t, root)
	// A real sandbox first (its directory must exist before a source can be
	// INSIDE it — the refusal is about the sandbox the ledger is materialised
	// in, which is the directory Ensure was handed).
	missing := filepath.Join(root, "no-such-tree")
	sb, err := Create(root, "", Options{
		Label:               "selfref",
		ProfileSettingsPath: settings,
		PluginsPath:         missing,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Now someone plants a plugin tree inside the sandbox and points the
	// initializer at it.
	inside := filepath.Join(sb.Path, config.PluginsDirName, config.WebSearchPluginDirName, config.WebSearchScriptsDirName)
	if err := os.MkdirAll(inside, sandboxEntryPerm); err != nil {
		t.Fatalf("create the inside-the-sandbox tree: %v", err)
	}
	_, err = Ensure(sb.Path, sb.Root, sb.RootSource, Options{
		ProfileSettingsPath: settings,
		PluginsPath:         filepath.Dir(filepath.Dir(inside)),
	})
	if err == nil {
		t.Fatalf("Ensure linked a plugin tree inside the sandbox — the self-referential shape must be refused")
	}
	if !strings.Contains(err.Error(), "INSIDE the sandbox") {
		t.Errorf("the refusal does not name the reason: %v", err)
	}
}

// TestPluginTreeSourceMustBeADirectory pins the remaining refusal: a FILE where
// the tree should be is not a tree, and linking it would hand the sandbox a
// path no resolver can descend.
func TestPluginTreeSourceMustBeADirectory(t *testing.T) {
	root := demoRoot(t)
	file := filepath.Join(root, "not-a-tree")
	if err := os.WriteFile(file, []byte("x"), sandboxMarkerPerm); err != nil {
		t.Fatalf("write the not-a-directory source: %v", err)
	}
	_, err := Create(root, "", Options{
		Label:               "filesrc",
		ProfileSettingsPath: settingsFile(t, root),
		PluginsPath:         file,
	})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("a file as the plugin-tree source was not refused by name: %v", err)
	}
}
