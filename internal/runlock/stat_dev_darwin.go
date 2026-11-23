//go:build darwin

package runlock

import "syscall"

// statDeviceID is the darwin half of the split described in the linux file.
// `Dev` is int32 here against linux's uint64, so it is widened through uint32 —
// the device number is a non-negative 32-bit value on both platforms, and
// widening a negative int32 straight to uint64 would sign-extend into a number
// no inode ever carried.
//
// NOTE WHAT DOES NOT WORK ON DARWIN, stated so this file is not read as a
// working lock probe: the ledger this key is looked up in is /proc/locks, and
// darwin has no procfs. LiveHolderOf therefore reports LockStateUnmeasured on
// this platform with the read failure named — the honest answer, not a
// fabricated "free".
func statDeviceID(st *syscall.Stat_t) uint64 { return uint64(uint32(st.Dev)) }
