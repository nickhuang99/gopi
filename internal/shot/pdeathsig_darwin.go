//go:build darwin

package shot

import "syscall"

// filmChildSysProcAttr is the darwin half of the split described in the linux
// file. `Pdeathsig` does not exist on darwin's syscall.SysProcAttr (checked with
// `go doc syscall.SysProcAttr` for GOOS=darwin), and the kernel offers no
// "signal this child when its parent dies" request — so the field is OMITTED
// rather than set to zero. Setpgid stays, because it does exist here.
//
// WHAT THIS COSTS, named rather than hidden (Rule 15): a filmed display or
// terminal whose gopi parent is SIGKILLed is NOT reaped by the kernel on darwin.
// It keeps running until it exits on its own or the operator kills it — a real
// leak, not a cosmetic gap. That is why this is a written degradation instead of
// a silent one, and why the shot's own run reports the platform it filmed on.
func filmChildSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
