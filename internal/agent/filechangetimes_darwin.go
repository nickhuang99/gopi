//go:build darwin

package agent

import (
	"syscall"
	"time"
)

// fileChangeTimes is the darwin half of the split described in the linux file:
// darwin spells the change time Ctimespec and the modification time Mtimespec,
// where linux uses Ctim/Mtim.
//
// WHAT STILL WORKS HERE: a stat, and both timestamps — the crashdump sweep gate's
// fingerprint ("has a new session appeared under this root?") is answerable on
// darwin with this reading, unlike the /proc-based probes elsewhere in the tree.
func fileChangeTimes(st *syscall.Stat_t) (ctime, mtime int64) {
	return st.Ctimespec.Sec*int64(time.Second) + st.Ctimespec.Nsec,
		st.Mtimespec.Sec*int64(time.Second) + st.Mtimespec.Nsec
}
