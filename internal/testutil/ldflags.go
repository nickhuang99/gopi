package testutil

// ldflags.go — the suite builder's half of the tested binary's identity: the
// linker flag that stamps the artifact with the revision it was built from.
//
// The spelling of the operand lives in internal/buildidentity (a leaf
// constants-only package) rather than here or in internal/version, because BOTH
// the recipes (this package, cmd/agent's own test-binary builder) and the reader
// (cmd/agent's freshness guard) need it, and internal/version's tests import
// internal/testutil — so a value parked in internal/version could not be read
// from here without closing an import cycle (Rule 14).
//
// The stamp exists because the toolchain cannot supply it: `vcs.revision` is
// written only for a checkout whose `.git` is a DIRECTORY, and a gopi task ground
// is a linked worktree (`.git` is a FILE), so a worktree build carries no
// identity at all. Without it the freshness guard can only compare mtimes, which
// cannot tell a byte-identical rewrite from a real edit — the defect
// cmd/agent/gopibin_identity_guard_test.go closes. The experiment is
// devdoc/binary-identity-on-a-worktree-ground.md.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nick/gopi/internal/buildidentity"
)

// gitExecutable resolves the revision a build stands at. Named (Rule 1a) so a
// host without git fails by name rather than by a bare exec error.
const gitExecutable = "git"

// LdflagsFlag returns the `go build` arguments that stamp a build of the tree at
// dir with its own revision, e.g. {"-ldflags", "-X <path>.<name>=<40-hex>"}.
//
// It returns NO arguments — and says why on stderr — when the revision cannot be
// resolved (no git, no repository), because a build that silently drops the
// identity is exactly the state this helper exists to end: the freshness guard
// would fall back to mtimes and its verdict says so, but a run whose artifacts
// are unstamped should hear about it at build time, not only at refusal time
// (Rule 15).
func LdflagsFlag(dir string) []string {
	cmd := exec.Command(gitExecutable, "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "testutil: the tested binary will carry no build identity (%s rev-parse HEAD in %s: %v) — "+
			"the freshness guards fall back to mtimes, which cannot tell a byte-identical rewrite from a real edit\n", gitExecutable, dir, err)
		return nil
	}
	rev := strings.TrimSpace(string(out))
	if rev == "" {
		fmt.Fprintf(os.Stderr, "testutil: the tested binary will carry no build identity (%s rev-parse HEAD in %s printed nothing)\n", gitExecutable, dir)
		return nil
	}
	return []string{buildidentity.LdflagsFlag, buildidentity.LdflagXKey + " " + buildidentity.RevisionLdflagPath + "=" + rev}
}
