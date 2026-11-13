package main

// gitcmd_test.go — the git-invocation helpers the CLI test suite shares.
//
// Harvested out of merge_run.go (Rule 43) when the one-shot merge verb was
// deleted (epic merge-verb-removal, item 3). None of these ever belonged to
// that mechanism: each one's only readers are OTHER tests in this package
// (devdoc_class_header_test.go, cli_version_stamp_test.go,
// suite_caller_head_guard_test.go, retired_merge_suite_survivors_test.go), and
// they spent their lives as production code that only tests called. They move
// here unchanged and stay unexported, so the deletion takes nothing a surviving
// test needs.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nick/gopi/internal/tools"
)

// gitBinaryName is the git executable the command shells out to. Named
// (Rule 1b) rather than typed at each call site.
const gitBinaryName = "git"

const (
	// mergeGitLocale is the locale pinned onto every git child these helpers run.
	// "C" keeps git's diagnostics byte-stable across hosts — the contract any
	// classification that PARSES a git message depends on.
	mergeGitLocale = "C"
	// mergeGitLocaleEnv names the one override, following the precedent
	// internal/tools/bash.go set for its own children (GOPI_BASH_LOCALE). It
	// exists so an operator can ask for a translated diagnostic when they are
	// reading one by hand; nothing here may PARSE it.
	mergeGitLocaleEnv = "GOPI_MERGE_LOCALE"
)

// mergeGitEnv assembles a git child's environment: the parent's, with the locale
// variables replaced by the pin above (Rule 2: mergeGitLocaleEnv is read HERE
// and nowhere else).
//
// IT DELEGATES, AND THAT IS THE POINT (Rule 14, measured 2026-10-14). The
// strip-and-append POLICY — which names are written, which gettext selectors are
// stripped, and that the inherited entries are REPLACED rather than appended
// beside (a duplicate key resolves to the FIRST entry, so an appended override
// loses) — has exactly ONE owner, tools.ChildLocaleEnvPinning. Spell the same
// four names a second time here and a fifth selector added to that helper would
// never reach these git children, with no test in this package able to see it.
func mergeGitEnv() []string {
	locale := mergeGitLocale
	if v := os.Getenv(mergeGitLocaleEnv); v != "" {
		locale = v
	}
	return tools.ChildLocaleEnvPinning(locale)
}

// mergeGit runs git in dir and returns its combined output. The error carries
// what git said, because a caller that only knows "git failed" has to re-run it
// to find out why.
func mergeGit(dir string, args ...string) (string, error) {
	cmd := exec.Command(gitBinaryName, args...)
	cmd.Dir = dir
	cmd.Env = mergeGitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
