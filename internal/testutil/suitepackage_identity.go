package testutil

// The IDENTITY a package's cross-run lock is keyed on, and why the suite LABEL
// cannot be it.
//
// THE DEFECT THIS CLOSES (measured 2026-11-08, this repository, `go test -run '^$'
// ./...`). The lock was keyed on the hand-written suite label
// (PinSuiteForTestMain("stylegate")), and TWO DIFFERENT PACKAGES declare that same
// label: internal/stylegate and internal/stylegate/cmd/stylegate. `go test` runs
// packages in PARALLEL, so the second one took the first one's lock and refused
// itself with SuiteBusyExitCode:
//
//	FATAL: suite "stylegate": another run of this package is in flight.
//	  lock:     .../gopi-test-scratch/suite-package-stylegate
//	  holder:   pid …, directory …/internal/stylegate, verdict live
//	FAIL	github.com/nickhuang99/gopi/internal/stylegate/cmd/stylegate
//
// Nothing was concurrent: the "sibling run" was the OTHER package, started by the
// same `go test` invocation. A refusal that fires on a correct command is a false
// positive, and it is the worst kind — exit 99 is a HARD failure that fails the
// whole run.
//
// WHY THE PACKAGE PATH AND NOT THE LABEL. The property the lock must key on is
// "two runs of the SAME PACKAGE share state outside their clones". The package's
// import path IS that identity, and it is not an author's choice: it is what the
// go tool built, and it reaches the running binary through its own build info
// (debug.ReadBuildInfo → "<import path of the test binary>"). The label stays a
// HUMAN description — it is what every message prints — but it no longer decides
// which lock a process takes.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// SuitePackageLockDir is the ONE definition of a package's lock address under THIS
// suite's scratch home (Rule 13: the mechanism computes the name, this face supplies
// the home the suite layer is the authority for).
//
// suite is the caller's LABEL (what the messages print); the ADDRESS is derived from
// the package identity, so two packages that share a label never share a lock.
func SuitePackageLockDir(suite string) string {
	return scratch.SuitePackageLockDir(ScratchHomeDir(), suitePackageLockKey(suite))
}

// IsSuitePackageLock reports whether a scratch-home entry belongs to the lock family.
func IsSuitePackageLock(name string) bool { return scratch.IsSuitePackageLockEntry(name) }

// testBinaryImportPath returns the import path of the package this test binary was
// built from ("github.com/nickhuang99/gopi/internal/stylegate"), or "" when the binary
// carries no build info (a stripped/hand-rolled binary — never a `go test` one).
//
// The go tool names the TEST binary "<import path>.test", which is the one suffix
// trimmed here; a missing suffix leaves the path alone rather than guessing a
// different one (Rule 15a: an unmeasured value is reported, never invented).
func testBinaryImportPath() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSpace(info.Path), testBinarySuffix)
}

// suitePackageLockKey is the lock's identity for a caller that names its suite
// label: the PACKAGE's import path when the binary carries one, else the label.
//
// The fallback is announced by the caller (AcquireSuitePackageLock prints it), never
// silent: on a binary with no build info the lock degrades to the label, which is
// exactly the collision this file removes — an operator must be able to see that it
// happened rather than debug it a second time (Rule 15).
func suitePackageLockKey(suite string) string {
	return suitePackageLockKeyFor(testBinaryImportPath(), suite)
}

// suitePackageLockKeyFor is the pure half, so the collision can be asserted without
// needing two packages that happen to share a label.
//
// The key is the package's import path, made collision-free: internal/scratch reduces
// a name to ONE path element (a separator would put the lock where no sibling looks),
// and that reduction maps "/" to "_" — so `a/b` and `a_b` would land on ONE address.
// A short digest of the path is therefore appended, which no two distinct paths
// share (a truncated SHA-256; Rule 35 — stdlib, nothing resident).
//
// The label is NOT folded in: it is a diagnosis, and mixing it into the key would let
// one package's label rename another package's address.
func suitePackageLockKeyFor(importPath, label string) string {
	identity := strings.TrimSpace(importPath)
	if identity == "" {
		return label
	}
	sum := sha256.Sum256([]byte(identity))
	return identity + "-" + hex.EncodeToString(sum[:])[:suitePackageLockDigestLen]
}

// suitePackageLockDigestLen is how many hex characters of the identity digest the lock
// name carries: 8 hex = 32 bits, which separates this repository's package count by
// many orders of magnitude while keeping the name readable.
const suitePackageLockDigestLen = 8

// announceSuitePackageLockIdentity reports WHICH identity the lock was keyed on when
// it is not the package path. It is called before the take so the line precedes any
// refusal (a refusal is only readable if the reader can see what was keyed).
func announceSuitePackageLockIdentity(suite string) {
	if testBinaryImportPath() != "" {
		return
	}
	fmt.Fprintf(os.Stderr, "testutil: suite %q: this test binary carries no build info, so its package lock is keyed "+
		"on the LABEL %q instead of the package import path — two packages that share that label would refuse each "+
		"other. Reported, never silent.\n", suite, suite)
}
