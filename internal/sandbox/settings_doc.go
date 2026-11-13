package sandbox

// settings_doc.go — the PROFILE'S SETTINGS DOCUMENT: a read-only COPY of the
// operator's file, never a link to it.
//
// WHAT THIS FILE REPLACES, AND WHY (measured 2026-09-27). The initializer used
// to create <profile>/settings.json as a SYMLINK to the real settings file, and
// documented it as "read-only by contract". That contract held only as long as
// nothing in the sandbox write-opened the path — and a `>` redirect does not
// ask: `printf '{"tmpdir":"%s/tmp"}' "$SB" > "$SB/.gopi/settings.json"` follows
// the symlink, truncates the TARGET, and the REAL settings document is gone.
//
// That is exactly what happened (a worker session's own fixture command, in a
// sandbox created by `gopi tool sandbox create`), and it destroyed 12 keys of the
// operator's live ~/.gopi/settings.json — the api_key, the repos table `gopi
// merge` keys on, the notify sink — leaving a 155-byte test fixture behind. The
// same sandbox later wrote `repos` at an e2merge-* scratch repo, so the
// surviving file carried two writers' fixtures and the operator's merge run
// reported success about a repository that was not theirs.
//
// A "read-only" SYMLINK cannot be enforced: the permission bits are the
// TARGET's, the only file whose permissions could restrict the write is the one
// the shape exists to protect, and POSIX has no way to say "follow for readers,
// refuse for writers". So the shape is replaced by one that cannot be misused:
//
//	<profile>/settings.json   a COPY, mode 0444, of the operator's document
//
//   - A COPY, because a sandbox must READ the operator's real configuration
//     (the same reason the link existed): the reader-side resolution
//     (<GOPI_HOME>/.gopi/settings.json) is unchanged, and the sandbox answers
//     with the operator's own keys.
//   - mode 0444, because the document is not a write target: a run that needs a
//     setting of its own declares it in its OWN settings file (GOPI_SETTINGS
//     points wherever it likes) — writing into the PROFILE path is a mistake,
//     and now it fails loudly at the write instead of silently destroying a file
//     outside the sandbox.
//   - A copy can go stale when the operator edits settings.json mid-sandbox.
//     That is the trade this shape buys, and it buys it deliberately: Create and
//     Ensure are the documented repair path, while the LINK's failure mode was
//     irreversible data loss.
//
// WHAT STILL IS A LINK: the plugin TREE (plugins.go). A tree is read by every
// consumer through the profile root, is never write-opened, and a copy would
// drift the moment the operator deploys a script — so it keeps the link, and the
// settings DOCUMENT is the one entry whose target was a single mutable file.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

const (
	// settingsDocCopyPerm is the mode of the profile's settings COPY.
	// Read-only for the owner as well: the sandbox profile's settings document
	// is an INPUT, and a write to it is a defect the kernel can refuse
	// (Rule 1a — one named literal).
	settingsDocCopyPerm os.FileMode = 0o444
	// settingsDocSuffix names the temporary file the copy is built in before it
	// is renamed into place, so a reader never observes a half-written document.
	settingsDocSuffix = ".copy-"
	// settingsDocRandomBytes is the width of the temporary file's random tail.
	settingsDocRandomBytes = 8
)

// materialiseSettingsDoc writes <profile>/settings.json as a read-only COPY of
// the source named by opts.ProfileSettingsPath (default: the resolved settings
// path — the same document production reads), and returns (source, path).
//
// The copy is built in a sibling temporary file and RENAMED onto the target, so
// the document a reader opens is either the previous complete one or the new
// complete one — never a partial write. Existing content is REPLACED (the copy
// tracks the operator's document; it holds no state of its own), while a target
// that is a SYMLINK is unlinked first: that is the pre-fix shape, and writing
// "over" it would follow it into the operator's real file — the very defect this
// file exists to end.
func materialiseSettingsDoc(dir string, opts Options) (source, path string, err error) {
	source, err = settingsDocSource(dir, opts)
	if err != nil {
		// AN ABSENT DOCUMENT IS NAMED, NEVER SWALLOWED (Rule 15, and the same
		// contract plugins.go keeps for a host with no plugin tree). A source that
		// is not there is a LEGAL host — a test profile, an operator who has
		// never written a settings file, GOPI_SETTINGS pointing at a
		// deliberately absent path (testutil/suiteenv.go) — and the pre-fix LINK
		// was happy to point at nothing while reporting nothing. So absence
		// returns the source it looked for with an EMPTY path, which the creation
		// line renders as `settings=<source>(absent-not-linked)`. Any other
		// failure (an unreadable document, a self-referential source) stays a
		// loud error.
		if errors.Is(err, fs.ErrNotExist) {
			return source, "", nil
		}
		return source, "", err
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return source, "", fmt.Errorf("sandbox: read the settings document %s to copy it into the sandbox: %w", source, err)
	}
	path = SettingsFileNameIn(dir)

	// THE PRE-FIX SHAPE IS REFUSED, NOT QUIETLY REPAIRED. Until 2026-09-27 this
	// path was a SYMLINK to the real settings file, and a symlink is followed by
	// the NEXT writer: `printf '{...}' > "$SB/.gopi/settings.json"` (the command
	// that destroyed the operator's live document) reaches the target. Removing
	// the link and installing the copy would leave exactly that window — anything
	// opening the path between the two steps writes through. So an existing
	// symlink is an ERROR naming the path, the target and the repair
	// (Create/Ensure on a fresh path, or delete the profile), and the legacy
	// MIGRATION — replacing the link without ever following it — lives on the
	// same entry point behind opts.ReplaceLegacySettingsLink, which the one
	// migration test sets. Refusing by default is what makes the failure loud
	// and early (Rule 15) instead of one redirect wide.
	info, lerr := os.Lstat(path)
	if lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(path) //stylegate:ignore a non-symlink settings file is the ordinary profile case, answered as 'not a sandbox profile'
		if !opts.ReplaceLegacySettingsLink {
			return source, "", fmt.Errorf("sandbox: refusing to build the settings copy at %s — it is a SYMLINK to %s, "+
				"the pre-fix write-through shape (a redirect into the profile path would reach the target and, on "+
				"2026-09-27, destroyed the operator's live settings document). Remove the sandbox and recreate it "+
				"(gopi tool sandbox create), or pass ReplaceLegacySettingsLink to migrate the link in place", path, target)
		}
		// os.Remove on a symlink removes the LINK NODE, never the file it points
		// at — the only safe way to retire this shape.
		if rerr := os.Remove(path); rerr != nil {
			return source, "", fmt.Errorf("sandbox: remove the settings symlink %s (the pre-fix write-through shape): %w", path, rerr)
		}
	}

	tmp, err := settingsDocTemp(path)
	if err != nil {
		return source, "", err
	}
	if werr := os.WriteFile(tmp, raw, settingsDocCopyPerm); werr != nil {
		return source, "", fmt.Errorf("sandbox: write the settings copy %s: %w", tmp, werr)
	}
	if cerr := os.Chmod(tmp, settingsDocCopyPerm); cerr != nil {
		return source, "", fmt.Errorf("sandbox: seal the settings copy %s read-only: %w", tmp, cerr)
	}
	if rerr := os.Rename(tmp, path); rerr != nil {
		return source, "", fmt.Errorf("sandbox: install the settings copy %s: %w", path, rerr)
	}
	return source, path, nil
}

// settingsDocTemp names the temporary file the copy is built in — a sibling of
// the target, so the rename stays on one filesystem.
func settingsDocTemp(path string) (string, error) {
	b := make([]byte, settingsDocRandomBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sandbox: random name for the settings copy beside %s: %w", path, err)
	}
	return path + settingsDocSuffix + hex.EncodeToString(b), nil
}

// settingsDocSource resolves the REAL settings document the sandbox's copy is
// taken from — the same resolution production uses, with the refusals the link
// shape already made: an empty resolution, a source that cannot be read, and a
// source INSIDE the sandbox (the self-referential shape that would make the
// sandbox configured by its own contents) are each named errors, never a silent
// empty profile.
func settingsDocSource(dir string, opts Options) (string, error) {
	target := strings.TrimSpace(opts.ProfileSettingsPath)
	if target == "" {
		target = config.ResolveSettingsPath()
	}
	if target == "" {
		return "", fmt.Errorf("sandbox: no settings path to copy (GOPI_SETTINGS unset and no default resolved)")
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("sandbox: resolve the settings path %s: %w", target, err)
	}
	if scratch.PathWithin(scratch.CanonicalPath(abs), scratch.CanonicalPath(dir)) {
		return "", fmt.Errorf("sandbox: refusing to copy the settings document %s — it is INSIDE the sandbox %s "+
			"(a sandbox must read the operator's real configuration, never its own copy)", abs, dir)
	}
	if _, err := os.Stat(abs); err != nil {
		return abs, fmt.Errorf("sandbox: stat the settings document %s to copy it into the sandbox: %w", abs, err)
	}
	return abs, nil
}
