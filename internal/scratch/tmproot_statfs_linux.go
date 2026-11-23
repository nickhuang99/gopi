//go:build linux

package scratch

import (
	"fmt"
	"syscall"
)

// statfsAt is the LINUX half of the platform split described in tmproot.go's
// statfsFacts. Field names are taken from `go doc syscall.Statfs_t` for
// GOOS=linux, not from memory:
//
//	Fsid struct { X__val [2]int32 }   ← the field is X__val on linux
//	Type int64                        ← signed
//	Bsize int64                       ← signed
func statfsAt(path string) (statfsFacts, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return statfsFacts{}, fmt.Errorf("statfs(%s): %w", path, err)
	}
	f := st.Fsid
	return statfsFacts{
		deviceID: uint64(uint32(f.X__val[0]))<<32 | uint64(uint32(f.X__val[1])),
		fsType:   st.Type,
		bsize:    st.Bsize,
		blocks:   st.Blocks,
		bavail:   st.Bavail,
	}, nil
}
