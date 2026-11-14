package sandbox

// plugins.go — the PROFILE'S PLUGIN TREE: the read-only link that makes a
// sandbox able to run the tools that live in the operator's profile.
//
// WHY (the defect this closes, measured 2026-09-27). `GOPI_HOME=<sandbox> gopi
// web search x` answered
//
//	{"status":"unavailable","method":"merged",…,
//	 "message":"websearch scripts dir not found: <sandbox>/.gopi/plugins/websearch/scripts"}
//
// and exited 0. The tool resolves its python3 scripts through
// config.WebSearchScriptsDir — <GOPI_HOME>/.gopi/plugins/websearch/scripts — and
// the initializer materialised sessions/, raw/, logs/, settings.json and the
// ledger, but NOT the plugin tree, so a sandbox was a profile in which the
// web-search plugin could not exist. The operator's profile has the tree; the
// sandbox pointed at a path nobody created.
//
// THE SHAPE OF THE FIX is a LINK to the tree the resolution descends from —
// never a copy of it. (The settings DOCUMENT takes the opposite shape for the
// opposite reason: it is a single mutable file a fixture destroyed through the
// old write-through link, so it is copied read-only — see settings_doc.go. A
// TREE is never write-opened, so it keeps the link.)
//
//	<profile>/plugins -> <the real plugin tree>   (SYMLINK, read-only by contract)
//
//   - A LINK, because a copy drifts: the operator's plugin tree is updated in
//     place (deployed scripts, new methods), and a sandbox holding a stale copy
//     would silently test a plugin that no longer exists.
//   - The TREE and not one script directory, because the profile root is the
//     level at which a link serves every consumer: the web-search scripts, any
//     other plugin under the tree, and the resolution chain the tool itself
//     walks (config.PluginsRootIn → websearch → scripts) all land inside it.
//   - READ-ONLY BY CONTRACT: nothing in the sandbox write-opens this path, and
//     the sandbox owner must not be able to mutate the operator's plugin tree by
//     running inside a sandbox. The tree keeps a LINK where the settings
//     document now takes a COPY (settings_doc.go): a tree is read through the
//     profile root and never write-opened, while a single mutable document was
//     destroyed by a `>` redirect through the link (2026-09-27).
//
// ABSENCE IS NAMED, NEVER SWALLOWED (Rule 15). A host without a plugin tree is a
// legal host — the sandbox is still created — but it is not a SILENT host: the
// initializer reports the resolved source it considered (Sandbox.PluginsSource)
// and leaves Sandbox.PluginsLink empty, and the CLI prints that as a warning. A
// sandbox whose plugin tree is missing therefore says so at creation time, in
// the same breath as the path it hands the caller, instead of surfacing much
// later as an unavailable search.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// pluginsLinkName is the name of the linked plugin tree inside the profile
// root. It is config.PluginsDirName — the same leaf the resolution descends
// through (Rule 13: the constant exists to be referenced, so the link and the
// reader cannot spell the tree differently).
const pluginsLinkName = config.PluginsDirName

// pluginTreeSource resolves the directory the sandbox's `plugins` entry links
// to, and reports whether that directory exists.
//
// The source is DERIVED from config.WebSearchScriptsDir — the ONE resolver both
// halves share — by climbing the two leaves that name the web-search plugin and
// its scripts directory (config.WebSearchPluginDirName,
// config.WebSearchScriptsDirName). That derivation is what makes the link
// identity-preserving:
//
//   - no override in force → the resolver answers <gopi data root>/plugins/
//     websearch/scripts, and the climb yields <gopi data root>/plugins — the
//     tree a second sandbox layer links through.
//   - $GOPI_WEBSEARCH_SCRIPTS_DIR naming a deployment mirror → the climb yields
//     that mirror's tree, so the sandbox links what the process actually reads
//     instead of a hard-coded profile default. (The override is an ENVIRONMENT
//     contract: a child that does not inherit it reads the profile default, which
//     is why the tree — not the scripts directory — is what gets linked.)
//   - Options.PluginsPath (a test's, or a caller's, explicit choice) wins.
//
// It returns (source, exists, err): a source that does not exist is NOT an error
// (see the file comment — absence is reported, not refused), while a source that
// is inside the sandbox, or that is not a directory, IS one.
func pluginTreeSource(dir string, opts Options) (string, bool, error) {
	target := strings.TrimSpace(opts.PluginsPath)
	if target == "" {
		scripts := config.WebSearchScriptsDir()
		target = filepath.Dir(filepath.Dir(scripts))
	}
	if strings.TrimSpace(target) == "" {
		return "", false, nil
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", false, fmt.Errorf("sandbox: resolve the plugin tree %s: %w", target, err)
	}
	// The filesystem root is what an override too shallow to hold a tree climbs
	// to (a one-leaf override, or the resolver's own unresolved fallback). Linking
	// it would hand the sandbox the whole host, so nothing is linked and the
	// source is reported instead.
	if abs == string(filepath.Separator) {
		return abs, false, nil
	}
	// A source INSIDE the sandbox is the self-referential shape the settings copy
	// already refuses: the sandbox would then be configured by its own contents,
	// which tests nothing. Refuse by name rather than link a directory the caller
	// is standing in.
	if scratch.PathWithin(scratch.CanonicalPath(abs), scratch.CanonicalPath(dir)) {
		return abs, false, fmt.Errorf("sandbox: refusing a plugin-tree link to %s — it is INSIDE the sandbox %s "+
			"(a sandbox reads the operator's real plugins, never its own directory)", abs, dir)
	}
	info, err := os.Stat(abs)
	if err != nil {
		// Absent: a legal host, an incomplete sandbox — reported by the caller,
		// never silently treated as success.
		return abs, false, nil
	}
	if !info.IsDir() {
		return abs, false, fmt.Errorf("sandbox: the plugin tree source %s is not a directory — refusing to link it", abs)
	}
	return abs, true, nil
}

// ensurePluginTreeLink materialises <profile>/plugins as a read-only link to
// the real plugin tree, idempotently (ensureSymlink replaces a stale link and
// never writes through one). It returns the source it considered and the target
// it linked (empty when the source is absent).
func ensurePluginTreeLink(dir string, opts Options) (source, linked string, err error) {
	source, exists, err := pluginTreeSource(dir, opts)
	if err != nil {
		return source, "", err
	}
	if !exists {
		return source, "", nil
	}
	link := filepath.Join(config.ProfileDirIn(dir), pluginsLinkName)
	if err := ensureSymlink(link, source, "the real plugin tree"); err != nil {
		return source, "", err
	}
	return source, source, nil
}
