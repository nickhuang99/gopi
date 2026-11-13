package testutil

// Constants of the daemon lifetime binding that belong to the TEST side: the fd
// number contract between a test process and the `gopi <verb> --daemon` process
// it starts.
//
// The mechanism itself (the pipe, the watchdog, the arming) lives in
// internal/daemonlifetime, which is production code because the WATCHER runs
// inside the daemon. This file holds only what a test needs to know about the
// numbers.
//
// THE ONE NUMBER THAT MATTERS: os/exec numbers ExtraFiles entries starting at fd
// 3, so the fd a descriptor arrives on in the child is NOT the fd it had in the
// parent. Getting that wrong is not a crash — the daemon starts, the watchdog
// refuses a descriptor that is not what it expects, and the daemon outlives its
// test while everything looks right (measured 2026-09-14). Every site therefore
// derives its number from the helpers below instead of writing one.

import "github.com/nick/gopi/internal/daemonlifetime"

const (
	// DaemonDeathHoldEnv is the environment variable that names the fd, IN THE
	// `gopi <verb> --daemon` PROCESS, holding the death pipe's read end. The
	// test suite's TestMain exports it for the whole test binary (see
	// internal/daemonlifetime.ArmThisProcess), so every gopi process a test
	// starts inherits it.
	DaemonDeathHoldEnv = daemonlifetime.HoldEnvName
	// DaemonDeathChildFD is the fd number the read end has inside a
	// `gopi <verb> --daemon` process when it was passed as the FIRST ExtraFiles
	// entry (the ordinary case). A caller that already passes other ExtraFiles
	// entries to that process uses ExtraFilesOffset instead of this constant.
	DaemonDeathChildFD = 3
)

// ExtraFilesOffset maps an ExtraFiles position (0-based, the index the death fd
// will occupy) to the fd number the receiving process sees. It delegates to the
// production constant source rather than restating the arithmetic (Rule 13/14).
func ExtraFilesOffset(index int) int { return daemonlifetime.ExtraFilesOffset(index) }
