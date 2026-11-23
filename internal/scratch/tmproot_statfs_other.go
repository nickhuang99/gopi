//go:build !linux && !darwin

package scratch

import (
	"fmt"
	"runtime"
)

// statfsAt is the NAMED REFUSAL for every target this tree has no statfs
// binding for.
//
// Rule 15's discipline, in the shape internal/sigsource/install_other.go already
// established: a platform that cannot answer says so, by name, and never
// returns a zero that a caller could read as a real reading. A zero device id
// is indistinguishable from "the path is on device 0" — and the temp-root ban
// would then be comparing two placements against a number nothing measured.
//
// This file is what makes `GOOS=windows go build` fail on the BINDING rather
// than on four copies of a POSIX struct: the rest of tmproot.go is
// platform-free by construction.
//
// TO ENABLE A TARGET, add its own statfsAt (a `_windows.go` etc.), not a
// fallback here — the reader that cannot measure must be able to tell "this
// host has no probe" apart from "this host measured zero".
func statfsAt(path string) (statfsFacts, error) {
	return statfsFacts{}, fmt.Errorf(
		"scratch: no statfs binding for %s/%s — the temp-root probe cannot measure %q on this host",
		runtime.GOOS, runtime.GOARCH, path)
}
