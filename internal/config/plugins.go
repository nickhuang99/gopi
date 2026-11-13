package config

import (
	"os"
	"path/filepath"
	"strings"
)

// plugins.go — the ONE definition of where a profile's PLUGIN TREE lives, and
// of the environment variable that overrides the web-search scripts directory.
//
// WHY IT IS HERE (Rule 2/13/14). The web-search scripts path used to be spelled
// only inside internal/tools, as a chain of bare literals
// (`filepath.Join(gd, "plugins", "websearch", "scripts")`) with an env name
// declared in that same file. Two consumers need the SAME answer and only one
// could name it:
//
//   - internal/tools resolves the path a RUN reads;
//   - the sandbox initializer (internal/sandbox) must materialise the same path
//     in a sandbox profile, or `GOPI_HOME=<sandbox> gopi web search x` resolves
//     a directory nobody created — the measured "dead inside a sandbox" defect.
//
// A second join with retyped literals is exactly the drift Rule 13 closes, so
// the leaves, the env name and the join live here, in a leaf-adjacent package
// both consumers already import (internal/sandbox imports internal/config for
// the profile join; internal/tools imports it for the gopi data root).
//
// THE CHANNELS. The scripts directory is resolved with ONE precedence:
//
//	$GOPI_WEBSEARCH_SCRIPTS_DIR (deployment override / hermetic tests)
//	> <gopi data root>/plugins/websearch/scripts
//
// and the SECOND channel is a join of the plugin-tree root — which is the
// directory a sandbox LINKS (see internal/sandbox): the sandbox materialises
// <profile>/plugins as a read-only link to the operator's real tree, so the
// default arm resolves inside a sandbox exactly where it resolves outside one.

// WebSearchScriptsDirEnvName is the environment variable that overrides the
// web-search plugin scripts directory (deployment override / hermetic tests).
// ONE spelling (Rule 1b): the catalog (settings.go), the resolver below and the
// help text all name it through this constant.
const WebSearchScriptsDirEnvName = "GOPI_WEBSEARCH_SCRIPTS_DIR"

// The plugin tree's shape, as directory leaves: <gopi data root>/<plugins>/
// <websearch>/<scripts>. Named (Rule 1a/1b) because the sandbox links the
// `plugins` level while the tool reads down to `scripts` — the two halves must
// agree on every leaf between them, and they are the same leaves for both.
const (
	// PluginsDirName is the plugin-tree root inside a profile
	// (<gopi data root>/plugins).
	PluginsDirName = "plugins"
	// WebSearchPluginDirName is the web-search plugin's own directory under the
	// tree root.
	WebSearchPluginDirName = "websearch"
	// WebSearchScriptsDirName is the directory holding the plugin's python3
	// scripts inside the plugin's directory.
	WebSearchScriptsDirName = "scripts"
)

// PluginsRootIn is THE spelling of the plugin-tree join under an explicit gopi
// data root: <gopiDir>/plugins. The sandbox initializer materialises exactly
// this path inside the profile, and it is the same join the default
// WebSearchScriptsDir arm descends through — one function, so the creator and
// the reader cannot disagree (Rule 2/14, the shape ProfilePathIn established
// for the sessions/raw/logs entries).
func PluginsRootIn(gopiDir string) string {
	return filepath.Join(gopiDir, PluginsDirName)
}

// WebSearchScriptsDirIn is THE spelling of the web-search scripts join under an
// explicit gopi data root: <gopiDir>/plugins/websearch/scripts — the path the
// default arm of WebSearchScriptsDir resolves. A caller that already holds a
// root (the sandbox initializer, the tests that drive this chain) names the
// directory through this function rather than re-joining the leaves.
func WebSearchScriptsDirIn(gopiDir string) string {
	return filepath.Join(PluginsRootIn(gopiDir), WebSearchPluginDirName, WebSearchScriptsDirName)
}

// WebSearchScriptsDir resolves the directory the web-search plugin scripts are
// run from, in the CURRENT process: the env override first, else the profile's
// own plugin tree (<gopi data root>/plugins/websearch/scripts, which is
// <GOPI_HOME>/.gopi/plugins/websearch/scripts when GOPI_HOME is set — the path
// a sandbox materialises).
//
// It never fails and never falls back silently to a path the caller cannot see:
// the gopi-data-root failure arm names the unresolved root explicitly and stays
// inside it, so the caller reports a path it actually asked for (Rule 15: a
// missing directory surfaces as a NAMED path in the caller's message, not as a
// silent substitution).
//
// This is the ONE reader of WebSearchScriptsDirEnvName in the process
// environment (Rule 2).
func WebSearchScriptsDir() string {
	if v := strings.TrimSpace(os.Getenv(WebSearchScriptsDirEnvName)); v != "" {
		return v
	}
	gd, err := GopiDir()
	if err != nil {
		return filepath.Join(ProfileDirName, PluginsDirName, WebSearchPluginDirName, WebSearchScriptsDirName)
	}
	return WebSearchScriptsDirIn(gd)
}
