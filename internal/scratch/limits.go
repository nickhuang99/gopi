// Package scratch — the child-process resource clamp.
//
// WHY THIS FILE EXISTS (ranked guard #1 of the 2026-09-13 /dev/shm assessment):
// every other guard in that report is a PATH policy — it decides whether a
// command may run. None of them bounds what the command does once it runs. The
// measured hazard (H1) is not a mistyped path but an UNBOUNDED ALLOCATION: the
// host has 15.5 GB of RAM, its 4 GB swap image has ~1.1 GB free, and a tmpfs
// page cannot be reclaimed to disk — so an overrun lands on the host-wide OOM
// killer, which picks its own victim (the orchestrator, a sibling agent, or
// Xorg). A path policy cannot stop that. A per-child address-space and
// process-count limit can: the failure is then confined to the child that
// caused it.
//
// THE TWO LIMITS, and why exactly these:
//
//	address space (ulimit -v / RLIMIT_AS) — how much a child may map. A runaway
//	    suite, a pathological allocation or a `dd` into tmpfs dies inside its
//	    own process instead of taking the host's memory.
//	process count (ulimit -u / RLIMIT_NPROC) — the fork-bomb bound. It is
//	    counted per UID by the kernel, so it is shared with every other process
//	    of the same user; that is why the default sits an order of magnitude
//	    above the measured live count rather than at it.
//
// HOW IT IS CARRIED (and why not the obvious way): the natural Go mechanism
// would be SysProcAttr.Rlimits, but that field exists only on the gvisor fork
// of os/exec — upstream `syscall.SysProcAttr` has no Rlimits field (checked on
// this host, go1.24.5). The zero-dependency carrier is therefore a SHELL
// PREAMBLE applied by the very same `bash -c` that runs the command (every
// child gopi spawns goes through bash), verified on this host to reach
// descendants: `ulimit -v 262144` inside bash made a later `/usr/bin/python3`
// allocation of 800 MiB fail with MemoryError.
//
// WHAT IT IS NOT: not a cgroup (no delegation, no root, zero dependencies — the
// zero-dep default of Rule 10.7), so it is not a hard scheduler-level cap. A
// systemd slice with MemoryMax/TasksMax remains the stronger option for an
// operator who has delegation; this is the cheapest guard that contains the
// measured failure mode.

package scratch

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

const (
	// ChildMemLimitEnvName bounds a child's address space, in MiB. Read in
	// exactly ONE place (ChildLimits, Rule 2). 0 = this knob is OFF — the
	// documented escape hatch, because a legitimate workload (a large build, a
	// memory-hungry analysis) may need more than the default, and the
	// alternative would be an operator editing code.
	ChildMemLimitEnvName = "GOPI_CHILD_MEM_LIMIT_MB"
	// ChildProcLimitEnvName bounds a child's process count (per user). Read in
	// the same ONE place; 0 = OFF.
	ChildProcLimitEnvName = "GOPI_CHILD_PROC_LIMIT"

	// DefaultChildMemLimitMB is the address-space ceiling applied when nothing
	// declares one. MEASURED basis (this host, 2026-09-13): the repository's
	// heaviest workloads — `go build ./...` and `go test ./...` across every
	// package — complete well inside 12 GiB, while the host has 15.5 GB of RAM
	// with a 4 GB swap image holding ~1.1 GB free (so the OOM path is closer
	// than the RAM total suggests). 12 GiB bounds a runaway BELOW the host's
	// memory while leaving every measured legitimate workload untouched — a
	// clamp that turns a slow test into a false failure is a clamp that gets
	// switched off.
	DefaultChildMemLimitMB = 12288
	// DefaultChildProcLimit is the process-count ceiling. MEASURED basis. 368
	// processes were live for this user while a full suite ran, and the kernel
	// counts per-uid (so the budget is shared with sibling agents and every
	// shell they own). 4096 is an order of magnitude above the measured working
	// set — high enough never to fail a legitimate parallel build (which spawns
	// hundreds of compilers), low enough that a fork bomb stops at a few
	// thousand processes instead of exhausting the host's pid space.
	DefaultChildProcLimit = 4096

	// rlimitProcResource is RLIMIT_NPROC, the process-count resource (6,
	// <asm-generic/resource.h>). Go's syscall package exports RLIMIT_AS but NOT
	// RLIMIT_NPROC on linux/amd64 (checked on this host, go1.24.5), so the value
	// is declared here with its kernel source rather than inlined at the call
	// site (Rule 1a: a magic number gets a name and a provenance).
	rlimitProcResource = 6

	// bytesPerMiB converts the env spelling (MiB) to the rlimit unit (bytes).
	bytesPerMiB = 1024 * 1024
	// bytesPerKiB is the unit `ulimit -v` expects: the shell builtin takes
	// KILOBYTES, not bytes (measured on this host — emitting bytes silently
	// multiplies the ceiling by 1024 and the clamp becomes a no-op; the kernel
	// reported Max address space 1 TiB for a "1024 MiB" clamp, which is exactly
	// 1073741824 KiB). Rule 1a: the conversion factor gets a name, because a
	// wrong one is invisible.
	bytesPerKiB = 1024

	// rlimitInfinity is RLIM_INFINITY as the kernel reports it in a struct
	// rlimit (all bits set) — the "no limit" value.
	rlimitInfinity = ^uint64(0)

	// limitPrefix is the shell builtin carrying the clamp. It is a constant
	// because the preamble, its tests and the operator-facing report must agree
	// on one spelling (Rule 1b).
	limitPrefix = "ulimit"

	// limitSilenceSuffix discards a refused limit's message. Swallowing it is
	// deliberate and is NOT a silent failure (Rule 15b): the clamp is decided
	// parent-side against the inherited limits (clampToParent), so a refusal in
	// the child can only mean the child inherited something STRICTER than the
	// policy — the effective limit is then already at least as tight as the one
	// that failed to apply, and printing that per command would be noise in a
	// tool result that an agent has to read.
	limitSilenceSuffix = " 2>/dev/null; "
)

// ChildLimits resolves the clamp policy: (maxAddressSpaceBytes, maxProcs,
// enabled). Both values come from the environment with the measured defaults
// (Rule 2: this is the ONE reader), and an explicit 0 turns a knob off.
//
// A malformed value is REPORTED and treated as "unset" (the default applies):
// silently ignoring a typo would leave an operator believing the clamp is
// tighter than it is, and refusing to clamp at all on a typo would leave the
// host unbounded. The default is the honest answer to "I could not read your
// knob".
func ChildLimits() (maxASBytes, maxProcs uint64, enabled bool) {
	asMB := envLimitValue(ChildMemLimitEnvName, DefaultChildMemLimitMB)
	procs := envLimitValue(ChildProcLimitEnvName, DefaultChildProcLimit)
	if asMB == 0 && procs == 0 {
		return 0, 0, false
	}
	return asMB * bytesPerMiB, procs, true
}

// envLimitValue is the ONE numeric knob reader (Rule 1b/2: one implementation,
// two names — the env-name constants — documenting the unit each carries). The
// knob name is passed IN, and every call site passes a constant, so the
// env-reader gate (Rule 21) sees each declared env name inside an env call's
// argument list.
func envLimitValue(name string, fallback uint64) uint64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		os.Stderr.WriteString("scratch: " + name + "=" + raw + " is not a number (" + err.Error() + ") — using the default clamp\n")
		return fallback
	}
	return n
}

// ChildLimitPreamble returns the shell prefix that clamps a child process, or
// "" when the clamp is off. Callers prepend it to the command they hand to
// `bash -c` (the bash tool and the job runner), so the limit is in force for
// the command AND for everything it spawns.
//
// Every value is resolved against the limits this process actually inherited
// (clampToParent): a knob the parent already holds tighter than the policy is
// OMITTED rather than raised, because raising a limit is loosening the guard
// (and `ulimit` cannot raise above the hard limit anyway — it would fail and
// print on every command).
func ChildLimitPreamble() string {
	maxAS, maxProcs, ok := ChildLimits()
	if !ok {
		return ""
	}
	var parts []string
	if v, apply := clampToParent(syscall.RLIMIT_AS, maxAS); apply {
		parts = append(parts, "-v "+strconv.FormatUint(kibRoundedUp(v), 10))
	}
	if v, apply := clampToParent(rlimitProcResource, maxProcs); apply {
		parts = append(parts, "-u "+strconv.FormatUint(v, 10))
	}
	if len(parts) == 0 {
		return ""
	}
	return limitPrefix + " " + strings.Join(parts, " ") + limitSilenceSuffix
}

// kibRoundedUp converts a byte count to the KiB value `ulimit -v` takes.
// Rounding is UP, so the applied ceiling is never tighter than the policy —
// a clamp that behaves as if the operator declared a smaller number than they
// did is a false failure waiting to happen.
func kibRoundedUp(bytes uint64) uint64 {
	return (bytes + bytesPerKiB - 1) / bytesPerKiB
}

// clampToParent returns the value the child may actually be given: the policy
// value, or the inherited HARD limit when that is lower. apply=false means "do
// not touch this knob" — either the knob is off (want == 0) or the inherited
// ceiling is already at or below the policy (nothing left to tighten).
func clampToParent(resource int, want uint64) (uint64, bool) {
	if want == 0 {
		return 0, false
	}
	var current syscall.Rlimit
	if err := syscall.Getrlimit(resource, &current); err == nil {
		if current.Max != rlimitInfinity && current.Max <= want {
			return 0, false // already at least as strict: never RAISE a limit
		}
	}
	return want, true
}

// LimitReport renders the resolved clamp for an operator-facing line (a startup
// banner, an acceptance script). It names the env source of each value so a
// reader can tell a defaulted knob from a declared one (Rule 15: name the
// cause), and says OFF explicitly rather than printing zeros.
func LimitReport() string {
	maxAS, maxProcs, ok := ChildLimits()
	if !ok {
		return "child limits: OFF (" + ChildMemLimitEnvName + "=0 and " + ChildProcLimitEnvName + "=0)"
	}
	return "child limits: AS=" + strconv.FormatUint(maxAS/bytesPerMiB, 10) + "MiB" +
		" NPROC=" + strconv.FormatUint(maxProcs, 10) +
		" (" + ChildMemLimitEnvName + "/" + ChildProcLimitEnvName + ")"
}
