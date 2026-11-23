//go:build linux

package agent

import (
	"syscall"
	"time"
)

// fileChangeTimes returns a file's (ctime, mtime) in nanoseconds.
//
// THE SPLIT EXISTS BECAUSE THE FIELD NAMES DIFFER (checked with `go doc
// syscall.Stat_t` per GOOS, not from memory): linux spells the change/-
// modification times Ctim/Mtim, darwin spells them Ctimespec/Mtimespec. A bare
// st.Ctim is what produced "st.Ctim undefined" on darwin.
//
// WHY NANOSECONDS RATHER THAN time.Time: the caller compares two readings for
// change, so it needs one comparable integer; a duration since the epoch is
// that, and it keeps the syscall surface (one stat, no allocation) visible.
func fileChangeTimes(st *syscall.Stat_t) (ctime, mtime int64) {
	return st.Ctim.Sec*int64(time.Second) + st.Ctim.Nsec,
		st.Mtim.Sec*int64(time.Second) + st.Mtim.Nsec
}
