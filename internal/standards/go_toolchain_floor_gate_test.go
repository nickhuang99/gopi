// go_toolchain_floor_gate_test.go — the toolchain floor is a FACT with two
// halves that must agree, and nothing else checked either of them.
//
// THE DEFECT THIS PINS (task 01a0f0f4, 2026-09-30, host host-a):
//
//	go: errors parsing go.mod:
//	go.mod:3: invalid go version '1.24.0': must match format 1.23
//
// That message reads like a malformed directive and is not one. It is emitted
// by Go 1.16's parser, which accepts only `%d.%d` in the `go` directive — the
// host's sole toolchain was gccgo 1.16.5 (/usr/bin/go -> go-11, package
// gccgo-go), while apt's jammy candidate is 1.18 and Go 1.24 itself writes the
// two-word form `go 1.24.0`. So NO spelling satisfies both ends; lowering the
// directive is equally impossible, because the module's own direct deps
// (charmbracelet/bubbletea v1.3.10, modernc.org/sqlite v1.44.0) declare
// `go 1.24.0` themselves. The floor is real and 1.24 is mandatory.
//
// What went unwatched: `go.mod` names a floor, and the toolchain that builds it
// must MEET that floor — but no test compared the two. On a host whose `go` was
// three years too old, the mismatch surfaced as a confusing parser error at
// package-load time, indistinguishable from a typo.
//
// This gate is offline and deterministic on purpose (it reads two files and
// asks the running toolchain its version): a toolchain-floor check that needed
// the network or a warm module cache would be skipped exactly when it matters.

package standardgate

import (
	"go/version"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

const (
	// goDirectiveFloor is the minimum the module's DIRECT dependencies impose.
	// Not a copy of a number someone liked: both of these declare `go 1.24.0`
	// in their own go.mod, so any build of this module needs >= 1.24.
	goDirectiveFloor = "1.24.0"

	// goModDirectiveRe captures the `go <version>` line of a go.mod.
	goModDirectiveRe = `(?m)^go\s+(\S+)\s*$`
)

// TestGate7GoToolchainMeetsTheModuleFloor asserts the two halves of the floor
// agree: go.mod declares >= the dependency-imposed floor, and the toolchain
// actually running this test satisfies go.mod's own declaration.
//
// RED PROOF: on the reported host (`go1.16.5 gccgo`) this package cannot even
// be compiled, which is the honest red for this defect — the build fails before
// any assertion runs. The assertion below is what catches the subtler
// regression: go.mod lowered to an older floor while the deps still need 1.24
// (or the floor raised while every builder is still on 1.22).
func TestGate7GoToolchainMeetsTheModuleFloor(t *testing.T) {
	root := repoRoot(t)

	declared, ok := readGoDirective(t, filepath.Join(root, moduleFileName))
	if !ok {
		t.Fatalf("%s carries no `go` directive — the module floor is undeclared", moduleFileName)
	}

	// Half 1: the module must not declare a floor BELOW what its deps need.
	// `go/version.Compare` (stdlib) handles the ordering; string comparison of
	// "1.9" vs "1.24" would not.
	if version.Compare("go"+declared, "go"+goDirectiveFloor) < 0 {
		t.Errorf("go.mod declares `go %s` but this module's direct dependencies "+
			"(bubbletea v1.3.10, modernc.org/sqlite v1.44.0) declare `go %s`: "+
			"lowering the directive does not remove the requirement, it hides it",
			declared, goDirectiveFloor)
	}

	// Half 2: the toolchain in use must MEET the declared floor. This is the
	// check whose absence let a 1.16 gccgo host report a parser error instead
	// of "your Go is too old".
	//
	// The RAW runtime version is compared, never version.Lang(): Lang returns
	// only major.minor ("go1.24"), and Compare orders "go1.24" BELOW "go1.24.0"
	// — so lang-ing the running version would fail a perfectly good toolchain.
	// The declared floor keeps its patch word, so both sides stay full.
	running := runtime.Version()
	if !version.IsValid(running) {
		// A "devel" or otherwise non-canonical toolchain carries no orderable
		// version. Skipped BY NAME, never silently: an unclassifiable toolchain
		// is a fact the log should carry.
		t.Skipf("running toolchain reports %q, which has no orderable version — "+
			"cannot compare it against the go.mod floor; build with a released Go", running)
	}
	if version.Compare(running, "go"+declared) < 0 {
		t.Fatalf("toolchain %s cannot build this module (go.mod declares `go %s`); "+
			"install a Go >= %s — see scripts/setup-go-toolchain.sh",
			running, declared, declared)
	}
}

// readGoDirective returns the version string of a go.mod's `go` directive.
func readGoDirective(t *testing.T, path string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	m := regexp.MustCompile(goModDirectiveRe).FindSubmatch(b)
	if m == nil {
		return "", false
	}
	return string(m[1]), true
}
