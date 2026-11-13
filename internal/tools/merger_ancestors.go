package tools

// merger_ancestors.go — "is this process a DESCENDANT of that one?", answered by
// walking the process tree in /proc.
//
// WHY IT EXISTS (and why it is not a new mechanism): the merger's single-instance
// guard has to tell two populations apart that look identical from the outside —
//
//	① a face the lock holder RUNS (the take-over's own `gopi merger --once`, whose
//	   close loop carries the rebuild contract of Rule 12): it inherits no lock fd
//	   and MUST be admitted, because refusing it would break the landing it is
//	   part of — and the holder is WAITING on it, so a refusal is a deadlock, not
//	   a safety property; and
//	② an INDEPENDENT gopi that happens to run at the same moment: it must be
//	   REFUSED, naming the holder — that refusal is the whole point of the lock.
//
// The divider is the process tree, read from the kernel. This package already had
// the same walk inside a lifecycle sweep (finishCleanupSelfChain, whose guard
// excludes the caller's own ancestors for the mirror-image reason: a run must not
// be blocked by its own process tree). It is EXPORTED here as the one
// implementation both callers share (Rule 2/14) rather than copied a second time.

import (
	"os"
	"strconv"
)

const (
	// ancestorWalkMaxHops bounds the upward walk. It exists so a procfs cycle (or
	// the re-parenting race around pid 1) always terminates; the deepest chain on
	// a real host is far shorter, and the bound is the SAME one the lifecycle
	// sweep's walk uses (Rule 2: one walk, one bound).
	ancestorWalkMaxHops = 64

	// ancestorRootPID is the pid every chain ends at; reaching it is the normal
	// terminus, not a failure.
	ancestorRootPID = 1
)

// AncestorPIDs returns every ancestor of pid, NEAREST FIRST, excluding pid
// itself. The chain stops at init (1), when a parent cannot be read (the process
// left the table, or /proc is not walkable) or at the hop bound — all three are
// ordinary termini, and none of them invents a parent.
//
// The caller decides what an ancestor means; this function asserts nothing about
// liveness beyond what the walk itself proves (each hop is read from a LIVE
// process's own stat entry, so a returned pid was alive when it was read).
func AncestorPIDs(pid int) []int {
	out := make([]int, 0, ancestorWalkMaxHops)
	seen := map[int]bool{pid: true}
	for hops := 0; hops < ancestorWalkMaxHops; hops++ {
		if pid <= 0 || pid == ancestorRootPID {
			break
		}
		parent, ok := finishCleanupProcParent(strconv.Itoa(pid))
		if !ok || parent <= 0 {
			break
		}
		if seen[parent] {
			break // a cycle (or a self-parent): stop rather than spin
		}
		seen[parent] = true
		out = append(out, parent)
		pid = parent
	}
	return out
}

// SelfAncestorPIDs is AncestorPIDs for the CALLING process — the form every
// caller in this tree needs, so "my own process tree" is one call rather than a
// walk spelled out at each site.
func SelfAncestorPIDs() []int {
	return AncestorPIDs(os.Getpid())
}
