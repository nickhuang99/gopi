// chrome.go — headless Chromium binary discovery for gopi tool shot.
//
// Rule 1: every literal used for locating/probing is a named constant here.
// The detection chain (devdoc/shot-design.md):
//  1. GOPI_SHOT_CHROME env (explicit override)
//  2. ~/.cache/ms-playwright/chromium-*/chrome-linux*/chrome (newest version
//     wins; HP's verified cache is chromium-1228/chrome-linux64/chrome)
//  3. PATH lookup: chromium-browser / chromium / google-chrome /
//     google-chrome-stable
//  4. not found → error listing every attempted path with the fix hint.
package shot

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// chromeEnvVar is the explicit binary override (Rule 2: exactly one env read).
const chromeEnvVar = "GOPI_SHOT_CHROME"

// versionZeroDigit is playwright's cache-version padding digit: cache dirs are
// zero-padded (chromium-0900), and leading zeroes carry no weight in a number.
const versionZeroDigit = "0"

// ChromeEnvName exports chromeEnvVar's value for the callers that must WRITE the
// variable instead of reading it — this package's own tests and a caller-side
// probe that has to inject a stand-in browser. It is an ALIAS, not a second
// definition (Rule 14): the value has exactly one holder, above.
//
// env-reader-exempt: an alias of chromeEnvVar; the reader is os.Getenv(chromeEnvVar)
// in findChrome, and no code reads a knob named "GOPI_SHOT_CHROME" through this
// identifier.
const ChromeEnvName = chromeEnvVar

// execBits is the POSIX execute-bit mask (Rule 1: no bare 0o111 at a use site).
// A located candidate must carry at least one of these bits to be usable.
const execBits = os.FileMode(0o111)

// Diagnostic messages with a single definition (Rule 1b/13): tests and callers
// assert on these exact substrings, so a reword lands in exactly one place.
const (
	errNotExecutableFile = "shot: %s=%s is not an executable file"
	msgNoChromeFound     = "shot: no headless chrome found"
)

// isExecutableFile reports whether path is a REGULAR file carrying at least one
// execute bit. Every discovery branch uses it so a candidate that cannot be
// started is rejected at discovery time, with an actionable message — instead
// of surfacing later as an opaque "shot: start …: permission denied" from deep
// inside Capture (bug 01a08c89: the env branch accepted a chmod 644 file).
func isExecutableFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return st.Mode().IsRegular() && st.Mode()&execBits != 0
}

// pathCandidates are the bare binary names tried via exec.LookPath, in order.
var pathCandidates = []string{
	"chromium-browser",
	"chromium",
	"google-chrome",
	"google-chrome-stable",
}

// chromeBin is a located headless-capable Chrome binary.
type chromeBin struct {
	Path   string // absolute path to the executable
	Source string // where it was found (env / playwright cache / PATH)
}

// findChrome locates a Chrome/Chromium binary using the detection chain.
func findChrome(homeDir string) (chromeBin, error) {
	// 1. Explicit override wins outright.
	if v := os.Getenv(chromeEnvVar); v != "" {
		if isExecutableFile(v) {
			return chromeBin{Path: v, Source: chromeEnvVar}, nil
		}
		return chromeBin{}, fmt.Errorf(errNotExecutableFile, chromeEnvVar, v)
	}

	var tried []string

	// 2. Playwright browser cache: ~/.cache/ms-playwright/chromium-*/chrome-linux*/chrome.
	cacheDir := filepath.Join(homeDir, ".cache", "ms-playwright")
	hits, _ := filepath.Glob(filepath.Join(cacheDir, "chromium-*", "chrome-linux*", "chrome")) //stylegate:ignore glob for optional cleanup; the pattern is a constant so ErrBadPattern is unreachable
	// Highest version directory wins. glob returns its hits in lexical order, so
	// the version segments must be compared NUMERICALLY: a string compare hands
	// back the stale chromium-9 over chromium-1228 ("9" > "1228" as strings).
	// SliceStable keeps glob's order for equal versions (deterministic pick).
	sort.SliceStable(hits, func(i, j int) bool {
		return comparePlaywrightVersions(playwrightVersion(hits[i]), playwrightVersion(hits[j])) > 0
	})
	for _, h := range hits {
		if isExecutableFile(h) {
			return chromeBin{Path: h, Source: "playwright cache"}, nil
		}
		tried = append(tried, h)
	}

	// 3. PATH lookup of the well-known names.
	for _, name := range pathCandidates {
		if p, err := exec.LookPath(name); err == nil {
			return chromeBin{Path: p, Source: "PATH"}, nil
		}
		tried = append(tried, name)
	}

	return chromeBin{}, fmt.Errorf(
		"%s (tried %s=%q, playwright cache %s, PATH %s) — "+
			"install one via `npx playwright install chromium` or set %s to a chrome binary",
		msgNoChromeFound, chromeEnvVar, os.Getenv(chromeEnvVar), cacheDir, strings.Join(pathCandidates, "/"), chromeEnvVar)
}

// playwrightVersion extracts the numeric version segment from a playwright
// cache chrome path (.../chromium-1228/chrome-linux64/chrome → "1228").
// Non-numeric suffixes don't match (a plain "chromium-browser" PATH hit
// yields "").
func playwrightVersion(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, p := range parts {
		if v, found := strings.CutPrefix(p, "chromium-"); found && isAllDigits(v) {
			return v
		}
	}
	return ""
}

// isAllDigits reports whether s is a non-empty digit string.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// comparePlaywrightVersions orders two playwrightVersion results the way a
// human reads them: NUMERICALLY, so "9" sorts BELOW "1228" — the ordering the
// chromium-9 vs chromium-1228 defect got wrong ("9" > "1228" lexically).
//
// It returns -1 / 0 / +1 like strings.Compare: negative when a is the older
// version, positive when a is the newer one. Both sides of findChrome's cache
// sort come from playwrightVersion, so real versions are digit strings; the
// comparison is exact without parsing or overflow: after dropping leading
// zeroes, MORE DIGITS means a larger number, and equal digit counts compare
// lexically — which for digit strings IS a numeric compare.
//
// The one non-numeric input is playwrightVersion's "" (a path whose version
// segment did not match): it is the weakest candidate, ranking below every
// real version, and two of them rank equal (the stable sort keeps glob's order).
func comparePlaywrightVersions(a, b string) int {
	aReal, bReal := isAllDigits(a), isAllDigits(b)
	if aReal != bReal {
		if aReal {
			return 1 // a is a real version, b did not match → a is newer
		}
		return -1
	}
	if !aReal {
		return 0 // neither matched → equal rank, keep the existing order
	}
	a, b = trimVersionZeroes(a), trimVersionZeroes(b)
	if len(a) != len(b) {
		if len(a) > len(b) {
			return 1
		}
		return -1
	}
	return strings.Compare(a, b)
}

// trimVersionZeroes drops the leading zeroes of a digit string, keeping one
// digit for an all-zero version (playwright pads cache names, e.g. chromium-0900).
func trimVersionZeroes(v string) string {
	if trimmed := strings.TrimLeft(v, versionZeroDigit); trimmed != "" {
		return trimmed
	}
	return versionZeroDigit
}
