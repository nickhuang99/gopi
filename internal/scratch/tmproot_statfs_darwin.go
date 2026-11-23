//go:build darwin

package scratch

import (
	"fmt"
	"syscall"
)

// statfsAt is the DARWIN half of the platform split described in tmproot.go's
// statfsFacts. Field names are taken from `go doc syscall.Statfs_t` for
// GOOS=darwin, not from memory — and they DIFFER from linux's on all three
// fields that matter, which is the whole reason this file exists:
//
//	Fsid struct { Val [2]int32 }   ← "Val" on darwin, "X__val" on linux
//	Type uint32                    ← UNSIGNED here, int64 on linux
//	Bsize uint32                   ← UNSIGNED here, int64 on linux
//
// The two unsigned fields are widened explicitly (int64(st.Type),
// int64(st.Bsize)); a bare assignment is what produced the compile error this
// split repairs ("cannot use st.Type (variable of type uint32) as int64 value").
//
// WHAT DARWIN WILL ANSWER, stated so nobody reads this as a working tmpfs
// probe: ClassifyFilesystemType matches the LINUX tmpfs/ramfs `f_type` magic
// numbers, and darwin's `f_type` values are a different namespace — so a darwin
// run classifies essentially every path as FilesystemOthers. That is the honest
// answer, not a degradation: darwin has no Linux tmpfs to find, and the
// caller's question ("is this a memory filesystem that competes for the / mount's
// space?") is answered "no memory filesystem here" rather than by a guess.
func statfsAt(path string) (statfsFacts, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return statfsFacts{}, fmt.Errorf("statfs(%s): %w", path, err)
	}
	f := st.Fsid
	return statfsFacts{
		deviceID: uint64(uint32(f.Val[0]))<<32 | uint64(uint32(f.Val[1])),
		fsType:   int64(st.Type),
		bsize:    int64(st.Bsize),
		blocks:   st.Blocks,
		bavail:   st.Bavail,
	}, nil
}
