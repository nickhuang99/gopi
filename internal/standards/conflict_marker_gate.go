package standardgate

import "fmt"

// conflictMarkerGate catches a git conflict marker left in a text file — the
// half-resolved merge that landed because nothing ever looked at the file
// again. Prose-only context, and why this is a gate rather than a habit:
// devdoc/conflict-marker-gate.md.
//
// WHY IT EXISTS (measured, not hypothetical): master shipped
// devdoc/repo-context-gate-reverify.md carrying the verbatim three-line conflict
// shape that the fix/heavy-tests-classification merge produced. That copy is
// EVIDENCE — it is the document's own subject — but it was read by the next
// agent as an unresolved marker pair and dispatched as a defect: a task whose
// briefing instructed the executor to delete the two marker lines and the
// rejected side, i.e. to destroy the evidence to satisfy a scan. So the gate's
// job is twofold and the second half matters more than the first: it must catch
// a marker that is NOT a quote (there is none in the tree today — the baseline
// is empty by construction), and it must be FENCE-AWARE so the quoted marker can
// never be brought into its coverage by deleting the quote (conflictMarkerFind).
//
// SCOPE: the whole module tree, not internal/+cmd/ — a conflict marker is a
// property of a text file, and the very case that motivated the gate lives under
// devdoc/.
func conflictMarkerGate() gate {
	return gate{
		id:             "Rule 16-21 family (no unresolved conflict marker in a text file)",
		checklistRow:   "gate-family/conflict-marker",
		probe:          "TestGateConflictMarkerRedProof",
		scanTextRoot:   textScanRoot,
		escape:         conflictMarkerEscape,
		baselineReason: "EMPTY by construction: an unresolved marker is never an acceptable state, so there is nothing to permit. The one marker shape in the tree (devdoc/repo-context-gate-reverify.md, a quoted conflict inside a fenced block) is not a hit at all — it is the fence rule's own subject, and a permit for it would be the mechanism by which a future agent deletes the quote.",
		baseline:       conflictMarkerBaseline,
		find:           conflictMarkerFind,
	}
}

// conflictMarkerBaseline is empty BY CONSTRUCTION. A conflict marker is either
// residue (resolve the merge) or a quoted marker (put it in a fence, or escape
// the line with conflictMarkerEscape and say why) — neither is a state to be
// recorded as a permit, and a baseline entry here would be a permanent
// exemption for a file that shipped an unresolved merge.
var conflictMarkerBaseline []string

// conflictMarkerProbeDoc is the probe document the red-proof test writes into a
// throwaway tree. It carries BOTH halves of the rule in one artifact, because a
// red proof that only showed the positive half would not pin the fence rule:
//
//   - a fenced block quoting the verbatim three-line conflict shape (the GoPi
//     document's own shape, marker lines included) — must NOT be reported;
//   - the same shape UNFENCED, twice (below and after the fence) — must be
//     reported, with the exact line numbers the test asserts.
//
// The marker runs are assembled from the gate's own vocabulary constants, so the
// probe cannot drift from the gate: a change to the marker shape changes the
// probe too, and the probe can never become a document that no longer exercises
// what the gate looks for.
func conflictMarkerProbeDoc() string {
	marker := func(side string, runLen int, rest string) string {
		return fmt.Sprintf("%s%s", repeatRune(side, runLen), rest)
	}
	return "docs/probe.md" + "\n" +
		"# Conflict-marker probe" + "\n" +
		"\n" +
		"Residue, unfenced — reported:" + "\n" +
		"\n" +
		marker(conflictMarkerOpener, conflictMarkerRunLen, " HEAD") + "\n" +
		"\tfencedSide(t)" + "\n" +
		"\n" +
		"Quoted evidence, fenced — NOT reported:" + "\n" +
		"\n" +
		conflictFence + "\n" +
		marker(conflictMarkerOpener, conflictMarkerRunLen, " HEAD") + "\n" +
		"\tSandboxRepoContext(t)" + "\n" +
		marker(conflictMarkerCloser, conflictMarkerRunLen, " fix/heavy-tests-classification") + "\n" +
		conflictFence + "\n" +
		"\n" +
		"Residue again, unfenced — reported:" + "\n" +
		marker(conflictMarkerOpener, conflictMarkerRunLen, "") + "\n"
}

// repeatRune is strings.Repeat for a one-character side constant, kept local so
// the probe builds a marker from the gate's vocabulary without importing
// strings here as well.
func repeatRune(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
