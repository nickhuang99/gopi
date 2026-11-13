package main

// cli_term_links.go — the TEXT BROWSER half of `gopi cli`: discovery, exec,
// and the actionable error when it is missing.
//
// LINKS IS AN EXTERNAL TOOL (the job-class rule: like vcmi or wine, a
// multi-MB third-party binary stays out of the Go build). It is NOT a go.mod
// dependency and it is NOT linked: the shipped artifact is still
// `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`, and the terminal carrier
// execs the browser as a child process (`os/exec`). Discovery policy, in
// order (Rule 2: one reader per source):
//
//  1. GOPI_LINKS_BIN — an explicit path. Wins outright (that is what an
//     override is for); a path that is missing or not executable is a LOUD
//     error naming the variable, never a silent fallback to (2) — otherwise
//     the operator would watch their override be quietly ignored.
//  2. the VENDORED binary: `scripts/links/<GOARCH>/links`, built by
//     scripts/build-links-static.sh and committed (the repo's existing place
//     for externally built artifacts is scripts/). The repo root is searched
//     upward from the running binary and from the working directory.
//
// When neither resolves, the error names the variable, the vendored path that
// was looked for, the recipe script and the reason it may be absent — an
// operator reading it never has to guess what to install (Rule 15: no silent
// failure).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// termLinksEnv is the override environment variable for the text browser
// binary path. Exactly one reader: resolveTermBrowser (Rule 2).
const termLinksEnv = "GOPI_LINKS_BIN"

// termVendoredLinksRel is the vendored binary's path relative to the repo
// root, as a slash-separated template with the GOARCH substituted in. The
// per-arch layout keeps one binary per platform directory, so a repo checkout
// on another architecture cannot exec the wrong one by accident.
const termVendoredLinksRel = "scripts/links/" + termLinksArchPlaceholder + "/" + termLinksBinaryName

// termLinksArchPlaceholder is the token substituted with runtime.GOARCH.
const termLinksArchPlaceholder = "<GOARCH>"

// termLinksBinaryName is the file name of the built browser inside its
// per-arch directory.
const termLinksBinaryName = "links"

// termLinksRecipeRel is the build recipe the error message points at: the
// exact configure line and the static-link proof live there, so "build it" is
// one command rather than a research task.
const termLinksRecipeRel = "scripts/build-links-static.sh"

// termRootWalkUp bounds the upward search for the repo root from each start
// point. Deep enough for the usual `<repo>/bin/gopi` and a nested working
// directory; shallow enough to be free.
const termRootWalkUp = 4

// termRepoMarkerFile is the file that identifies the repo root: its presence is
// the only reason a directory is treated as the checkout the vendored path is
// relative to.
const termRepoMarkerFile = "go.mod"

// termExecCommand is the exec seam: the launcher's argv is asserted through it
// without starting a browser in a test (the same injection pattern the web
// server uses for its agent factory).
var termExecCommand = exec.Command

// resolveTermBrowser resolves the text browser binary to exec, per the policy
// above. The returned error is the operator-facing one: it names every channel
// that was tried and what to do about it.
func resolveTermBrowser() (string, error) {
	if override := strings.TrimSpace(os.Getenv(termLinksEnv)); override != "" {
		info, err := os.Stat(override)
		if err != nil {
			return "", fmt.Errorf("%s=%s: %w (the override must be an existing executable; unset %s to use the vendored binary)",
				termLinksEnv, override, err, termLinksEnv)
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s=%s is a directory, not the browser binary", termLinksEnv, override)
		}
		if info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("%s=%s is not executable (chmod +x it, or unset %s)", termLinksEnv, override, termLinksEnv)
		}
		return override, nil
	}

	rel := strings.Replace(termVendoredLinksRel, termLinksArchPlaceholder, runtime.GOARCH, 1)
	return resolveTermBrowserIn(termRootCandidates(), rel)
}

// resolveTermBrowserIn is the root-list seam: resolveTermBrowser hands it the
// candidates derived from the process (the running binary's directory and the
// working directory, each walked up to the checkout), and the test hands it a
// fixture tree. One resolution rule, two entry points.
func resolveTermBrowserIn(roots []string, rel string) (string, error) {
	for _, root := range roots {
		candidate := filepath.Join(root, filepath.FromSlash(rel))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}

	return "", fmt.Errorf(`no text browser found: set %s=/path/to/links, or build the vendored one
  looked for: %s (relative to the gopi checkout)
  build it with: sh %s   (static links2, no new Go dependency)
  why it may be absent: the binary is built off the Go build path on purpose — `+
		"`go build ./cmd/agent/` never needs it, so a checkout that has not run the recipe has no copy",
		termLinksEnv, rel, termLinksRecipeRel)
}

// termRootCandidates lists the directories to resolve the vendored path
// against, most specific first: the directory containing the running binary
// (so an installed `<repo>/bin/gopi` finds `<repo>/scripts/links/...` walking
// up) and the working directory (so `go run`/a test from inside the checkout
// resolves). Each entry is already walked up to a directory holding the repo
// marker; duplicates are dropped so the same path is not stat'd twice.
func termRootCandidates() []string {
	var starts []string
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	var out []string
	seen := make(map[string]bool)
	for _, start := range starts {
		if root, ok := termRepoRoot(start); ok && !seen[root] {
			seen[root] = true
			out = append(out, root)
		}
	}
	return out
}

// termRepoRoot walks up from start looking for the repo marker. It reports
// whether a root was found, so a miss never silently resolves the vendored
// path against an unrelated directory.
func termRepoRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for i := 0; i <= termRootWalkUp; i++ {
		if info, err := os.Stat(filepath.Join(dir, termRepoMarkerFile)); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

// runTermBrowser execs the text browser on the chat URL, on the CURRENT
// terminal (the browser owns stdin/stdout/stderr — this process becomes the
// carrier only in the sense that it waits). It returns when the browser exits;
// the caller ends the chat on that event.
func runTermBrowser(links, url string) error {
	cmd := termExecCommand(links, url)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s %s: %w", links, url, err)
	}
	return nil
}
