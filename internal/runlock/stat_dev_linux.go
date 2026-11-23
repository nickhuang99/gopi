//go:build linux

package runlock

import "syscall"

// statDeviceID returns the device number of a stat result, widened to the type
// the lock-ledger key is built from.
//
// THE SPLIT EXISTS BECAUSE THE FIELD TYPE DIFFERS (checked with `go doc
// syscall.Stat_t` per GOOS, not from memory): linux declares
// `Dev uint64`, darwin declares `Dev int32`. A bare `st.Dev` is what produced
// "cannot use st.Dev (variable of type int32) as uint64 value" on darwin.
func statDeviceID(st *syscall.Stat_t) uint64 { return st.Dev }
