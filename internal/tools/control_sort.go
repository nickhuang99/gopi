// control_sort.go — the deterministic order of a component's live instance
// listing, kept beside the listing itself (Rule 2: one ordering, not a
// sort.Slice closure repeated at each call site).

package tools

import "sort"

// sortControlInstances orders live instances by pid ascending — the order
// tests and operators read them in.
func sortControlInstances(in []ControlInstance) {
	sort.Slice(in, func(i, j int) bool { return in[i].PID < in[j].PID })
}

// sortInts orders pids ascending (the stop-request listing).
func sortInts(in []int) { sort.Ints(in) }
