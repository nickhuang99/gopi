//go:build !linux && !darwin

package runlock

import "syscall"

// statDeviceID → the platform has no stat binding this tree can read, so it
// reports the sentinel `0` and the CALLER must not mistake it for a real device:
// LiveHolderOf only reaches this after a successful os.Stat, and on such a
// target the /proc/locks ledger it needs does not exist either, so the read that
// follows fails and the state is LockStateUnmeasured with that failure named.
//
// This is deliberately NOT a fallback that pretends to measure: a fabricated
// key would look up nothing and read as "no process holds this lock" — a free
// verdict nobody observed, which is the one answer this whole package exists to
// never give.
func statDeviceID(st *syscall.Stat_t) uint64 { return 0 }
