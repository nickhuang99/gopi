package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// expandHome expands a leading "~" or "~/" in a filesystem path to the user's
// home directory, returning the resolved path. Paths without a tilde prefix
// are returned unchanged. This is the single home-expansion routine shared by
// read/write/edit (previously each tool re-implemented its own ~/ block with
// subtly different bare-"~" handling and error text — the drift that caused
// read to support bare "~" while write/edit did not).
//
//   - "~"     → the home directory itself
//   - "~/x/y" → <home>/x/y
//   - anything else → returned unchanged
//
// A non-tilde path is never an error (no home lookup is attempted), so
// callers that pass an absolute or relative path are unaffected.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}
