package testutil

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nick/gopi/internal/scratch"
)

// WHICH ENVIRONMENT IS THIS — the READ-ONLY half of a placement verdict.
//
// WHY THIS FILE EXISTS (measured on this host, 2026-10-12): a memory base
// (/dev/shm, 7804604416 bytes of tmpfs) can sit 1-2 MiB below
// scratch.MinMemoryPlacementFreeBytes for hours, because a SHARED 1777
// directory fills from work this tree does not own. While it does, the ambient
// placement tests fail — and the failure TEXT said the only thing the assertion
// knew: "this suite's root is NOT memory-backed although /dev/shm is". That
// sentence is TRUE and MISLEADS: it reads as "the placement logic is broken"
// when the placement has in fact done exactly what it is designed to do (decline
// a candidate with no room) and the machine is simply full.
//
// The two situations need different readers and different remedies:
//
//	the PLACEMENT is broken  → a code regression; nothing on the host can fix it
//	the BASE is saturated    → an environment condition; the host needs room
//
// So the verdict is built from the LIVE measurement alone (what the host
// actually has), while the DECISION and its decline notes come from the
// placement itself: a decision that declined while the base HAS room is the
// defect, and a decline that carries the measured bytes is the logic working.
// Nothing here judges a fixture's opinion about free space — a fixture can
// break the reading (that is what it is for), never the host.
//
// EVERYTHING IN THIS FILE IS READ-ONLY (Rule 27 is not in play: no load is
// generated, no byte is written, and the base is never filled to reproduce the
// condition). The sweeps it consults are the PLAN faces, which delete nothing —
// a report that changed the machine would not be evidence.

const (
	// baseEvidenceTopOccupants bounds the occupant list a saturated-base report
	// prints: the reader needs the LARGEST entries, not 408 rows.
	baseEvidenceTopOccupants = 12
	// baseEvidenceWalkFileLimit bounds how many files may be visited while
	// measuring the listed occupants. A shared base holds far more than this
	// (measured: 408 top-level entries, 2.6 GiB), and a census that walked all
	// of it would cost the suite more than the finding is worth — a truncated
	// size is REPORTED as truncated rather than silently under-counted
	// (Rule 15).
	baseEvidenceWalkFileLimit = 20000
	// baseEvidenceNameWidth is the column width an entry's name is rendered in:
	// wider than the longest residue name measured, so a name is shown whole
	// whenever it fits.
	baseEvidenceNameWidth = 44
)

// baseOccupant is one top-level entry of a memory base: what it is, how big,
// and — the half that matters for a human decision — what the NAME PREDICATE
// says about it.
type baseOccupant struct {
	Name      string
	Bytes     int64
	Truncated bool
	Dir       bool
	Age       time.Duration
	// Residue is the predicate's verdict (scratch.IsBaseResidueName): this
	// system's own residue namespace, collectable by the base sweep.
	Residue bool
	// Verdict words WHY, for a reader who has to decide. The DECISION is
	// Residue; this is its explanation, and the two are built in one place
	// (baseNameVerdict) so they cannot disagree.
	Verdict string
}

// baseSaturationEvidence is the read-only answer to "why is this base full?".
// It is built on demand, printed once, and never used to change anything.
type baseSaturationEvidence struct {
	Base          string
	Free          uint64
	FreeErr       error
	Floor         uint64
	OlderThan     time.Duration
	ReadErr       error
	Entries       int
	Occupants     []baseOccupant // largest first, REFUSED and accepted together
	Refused       []baseOccupant // largest first: the human decision list
	Accepted      int
	AcceptedBytes int64
	RefusedBytes  int64
	// Collectable is what the base sweep's PLAN face would collect right now
	// (scratch.BaseResiduePlan — the same predicate the sweep runs); the count
	// is the honest answer to "is the reclaimer refusing to work?", which is the
	// FIRST hypothesis to test and usually the wrong one.
	Collectable    int
	CollectableErr error
	// AutoResidueEnabled records WHICH mode the plan above was taken in: in
	// automatic mode the per-run suite-root family is refused by construction
	// (see baseNameVerdictAgainst), so a zero plan count there is not evidence
	// of a broken reclaimer. It is DERIVED from the entries themselves — a
	// per-run suite root that came back refused is the mode, measured rather
	// than read from a second source.
	AutoResidueEnabled bool
	// RefusedSuiteRuns counts the top-level entries that are per-run suite
	// roots refused BY the automatic stand-down.
	RefusedSuiteRuns int
	// suiteRunTotal counts every per-run suite root seen (accepted or refused),
	// which is what makes the mode above MEASURED.
	suiteRunTotal int
	// refusedAll / acceptedAll are the COMPLETE lists (largest first), kept for
	// the census a human decision needs — the report prints only the head of
	// each so a test's output stays readable.
	refusedAll  []baseOccupant
	acceptedAll []baseOccupant
	// SuiteRunCollectable is the EXPLICIT arm's plan (per-run suite roots: age
	// + live-holder rule), which the automatic pass deliberately refuses.
	SuiteRunCollectable int
	SuiteRunErr         error
	WalkTruncated       bool
}

// baseNameVerdict is baseNameVerdictAgainst's simple form (see it for the
// auto/explicit asymmetry the report also explains).
func baseNameVerdict(name string) (bool, string) { return baseNameVerdictAgainst(name) }

// baseNameVerdictAgainst answers, for ONE name, whether this system's base
// sweep may collect it, and in words a reader can act on. The verdict is
// scratch.IsBaseResidueName — the ONE predicate the sweep itself decides on,
// which ALREADY reflects the automatic/explicit mode — and the wording is
// derived in the SAME function so an explanation can never drift from the
// decision it explains.
//
// The mode is not passed in and not read from a second source: it is INFERRED
// from the predicate's own answer, because the two are one fact. In AUTOMATIC
// mode the per-run suite-root family is collectable (it is this namespace's
// residue); in a suite process the arm is stood down, so those names come back
// refused — and a user decision about THEIR data needs to know which of the two
// reasons applies.
func baseNameVerdictAgainst(name string) (bool, string) {
	if name == scratch.RootDirName {
		return false, "refused: the scratch home itself (name == scratch.RootDirName) — owned by the HOME sweep, and a base sweep that collected it would delete the live tree"
	}
	if strings.Contains(name, scratch.LegacyScratchMarker) {
		return false, "refused: the legacy suite-scratch namespace, which internal/testutil's OWN sweep collects (two collectors for one directory is the defect that arm avoids)"
	}
	if scratch.IsBaseResidueName(name) {
		return true, "accepted: a residue family with a generated token — collectable by the age + live-holder rule"
	}
	if baseNameIsSuiteRun(name) {
		return false, "refused by the AUTOMATIC pass only: a per-run suite root (a suite running on this host may own it) — the EXPLICIT sweep and the placement's reclaim step collect this family, and it is this namespace's largest contributor"
	}
	return false, "refused: no generated token in the name (every dash-separated token is a chosen word, or the family is bare) — a name this system never created, so a sweep may not delete it"
}

// baseNameIsSuiteRun reports whether a name is the per-run suite-root family.
// The shape question is answered by scratch (ONE definition of the namespace),
// so this cannot drift from the sweep's own kind test.
func baseNameIsSuiteRun(name string) bool {
	return strings.HasPrefix(name, scratch.SuiteRunMarker) && strings.Contains(name, runSuffixPattern)
}

// baseEntrySize sums the apparent size of path, visiting at most *limit files
// across the whole report. A directory whose walk hit the cap reports
// truncated=true, so a partial number is never passed off as a total.
func baseEntrySize(path string, limit *int) (int64, bool, error) {
	if *limit <= 0 {
		return 0, true, nil
	}
	var total int64
	truncated := false
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			// An entry that vanished mid-walk (a live suite cleaning up) is not
			// a reason to abandon the census: the rest of the base still needs
			// measuring. The walk reports what it could read.
			return nil
		}
		if *limit <= 0 {
			truncated = true
			return fs.SkipAll
		}
		*limit--
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		return total, truncated, fmt.Errorf("measure %s: %w", path, err)
	}
	return total, truncated, nil
}

// baseSaturationEvidenceFor builds the picture. It reads the base's own
// directory, asks the name predicate about every entry, measures the largest
// ones within a bounded walk, and consults both reclaim PLAN faces — the
// read-only twins of the two sweeps that own this namespace.
//
// A base that has vanished (or was never there) is reported as such: "there is
// no memory base on this host" is a fact the report must state, not a crash.
func baseSaturationEvidenceFor(base string, olderThan time.Duration) baseSaturationEvidence {
	ev := baseSaturationEvidence{
		Base:      base,
		Floor:     scratch.MinMemoryPlacementFreeBytes,
		OlderThan: olderThan,
	}
	ev.Free, ev.FreeErr = baseFreeBytes(base)
	entries, err := os.ReadDir(base)
	if err != nil {
		ev.ReadErr = fmt.Errorf("read memory base %s: %w", base, err)
		return ev
	}
	ev.Entries = len(entries)
	now := time.Now()
	remaining := baseEvidenceWalkFileLimit
	all := make([]baseOccupant, 0, len(entries))
	for _, entry := range entries {
		oc := baseOccupant{Name: entry.Name(), Dir: entry.IsDir()}
		oc.Residue, oc.Verdict = baseNameVerdictAgainst(oc.Name)
		if !oc.Residue && baseNameIsSuiteRun(oc.Name) {
			ev.RefusedSuiteRuns++
		}
		if baseNameIsSuiteRun(oc.Name) {
			ev.suiteRunTotal++
		}
		if info, lerr := entry.Info(); lerr == nil {
			oc.Age = now.Sub(info.ModTime())
			if !oc.Dir {
				oc.Bytes = info.Size()
			}
		}
		if oc.Dir && remaining > 0 {
			size, truncated, serr := baseEntrySize(filepath.Join(base, oc.Name), &remaining)
			if serr != nil {
				ev.WalkTruncated = true
			}
			oc.Bytes, oc.Truncated = size, truncated
		}
		if oc.Residue {
			ev.Accepted++
			ev.AcceptedBytes += oc.Bytes
		} else {
			ev.RefusedBytes += oc.Bytes
		}
		all = append(all, oc)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Bytes > all[j].Bytes })
	ev.Occupants = head(all, baseEvidenceTopOccupants)
	refused := make([]baseOccupant, 0, len(all))
	accepted := make([]baseOccupant, 0, len(all))
	for _, oc := range all {
		if oc.Residue {
			accepted = append(accepted, oc)
			continue
		}
		refused = append(refused, oc)
	}
	ev.refusedAll = refused
	ev.acceptedAll = accepted
	ev.Refused = head(refused, baseEvidenceTopOccupants)
	// The mode, MEASURED from the entries rather than read from a second
	// source: a per-run suite root that the predicate refused can only mean the
	// automatic arm is stood down (in every other mode that family is
	// collectable). With no such entry present the question is unobservable, so
	// the report claims nothing about it.
	ev.AutoResidueEnabled = !(ev.suiteRunTotal > 0 && ev.RefusedSuiteRuns > 0)
	if plan, perr := scratch.BaseResiduePlan(base, olderThan); perr != nil {
		ev.CollectableErr = perr
	} else {
		ev.Collectable = len(plan)
	}
	if plan, perr := scratch.BaseSuiteRunPlan(base, olderThan); perr != nil {
		ev.SuiteRunErr = perr
	} else {
		ev.SuiteRunCollectable = len(plan)
	}
	return ev
}

// head returns the first n items of a sorted slice, or the slice itself.
func head(items []baseOccupant, n int) []baseOccupant {
	if len(items) <= n {
		return items
	}
	return items[:n]
}

// Report renders the evidence as the text a reader needs: the numbers the
// placement decides on, the occupants that produce them, the name predicate's
// verdict per occupant, and what each reclaim face could collect RIGHT NOW —
// because "the reclaimer collects nothing" has to be measured before it is
// called a failure.
func (e baseSaturationEvidence) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "MEMORY BASE %s: %s free, floor %s (the placement declines a candidate below the floor)\n",
		e.Base, byteNote(e.Free, e.FreeErr), scratch.FormatBytes(e.Floor))
	if e.ReadErr != nil {
		fmt.Fprintf(&b, "  the base could not be read: %v\n", e.ReadErr)
		return b.String()
	}
	fmt.Fprintf(&b, "  top-level entries: %d | names the base sweep ACCEPTS: %d (%s) | names it REFUSES: %d (%s)\n",
		e.Entries, e.Accepted, byteCount(e.AcceptedBytes), e.Entries-e.Accepted, byteCount(e.RefusedBytes))
	if !e.AutoResidueEnabled && e.RefusedSuiteRuns > 0 {
		fmt.Fprintf(&b, "  of those refusals, %d are the per-run suite-root family: refused by the AUTOMATIC pass BY "+
			"CONSTRUCTION (a suite running on this host may own them) — the explicit sweep owns that family, see the "+
			"plans below\n", e.RefusedSuiteRuns)
	}
	if e.WalkTruncated {
		fmt.Fprintf(&b, "  sizes below are PARTIAL: the %d-file measurement budget was exhausted\n", baseEvidenceWalkFileLimit)
	}
	b.WriteString("  LARGEST OCCUPANTS (name predicate verdict per entry):\n")
	for _, oc := range e.Occupants {
		fmt.Fprintf(&b, "    %9s  %-34s age %-6s %s\n", sizeNote(oc), truncateName(oc.Name), shortAge(oc.Age), oc.Verdict)
	}
	b.WriteString("  HUMAN DECISION LIST — the largest names the sweep REFUSES (a refusal is a decision about someone else's data, never a bug to paper over):\n")
	if len(e.Refused) == 0 {
		b.WriteString("    (none: every name here is in this system's residue namespace)\n")
	}
	for _, oc := range e.Refused {
		fmt.Fprintf(&b, "    %9s  %-34s %s\n", sizeNote(oc), truncateName(oc.Name), oc.Verdict)
	}
	fmt.Fprintf(&b, "  RECLAIM FACES, measured (a plan deletes nothing — these are what a sweep WOULD collect; mode: %s):\n",
		autoResidueMode(e.AutoResidueEnabled))
	fmt.Fprintf(&b, "    base residue plan (age+holder predicate): %s\n", countNote(e.Collectable, e.CollectableErr))
	fmt.Fprintf(&b, "    per-run suite roots (explicit arm):       %s\n", countNote(e.SuiteRunCollectable, e.SuiteRunErr))
	fmt.Fprintf(&b, "  stale window the plans were asked with: %s\n", e.OlderThan)
	return b.String()
}

// Census renders EVERY refused entry (largest first) with its size and the
// predicate's wording — the artifact a HUMAN decision about the base needs,
// where the report in Report() prints only the largest few so a test's output
// stays readable. It is read-only in the same way: it describes entries it
// never touches.
//
// The accepted set is summarised, not listed: those are this system's own
// residue, and their collection is a sweep's decision (already reported by the
// plans), not a human one.
func (e baseSaturationEvidence) Census() string {
	var b strings.Builder
	fmt.Fprintf(&b, "base census %s: %s free (floor %s), %d top-level entries\n",
		e.Base, byteNote(e.Free, e.FreeErr), scratch.FormatBytes(e.Floor), e.Entries)
	if e.ReadErr != nil {
		fmt.Fprintf(&b, "  the base could not be read: %v\n", e.ReadErr)
		return b.String()
	}
	fmt.Fprintf(&b, "ACCEPTED BY THE NAME PREDICATE (this system's own residue — a sweep's decision, not a human's): %d entries, %s\n",
		len(e.acceptedAll), byteCount(e.AcceptedBytes))
	for _, oc := range e.acceptedAll {
		fmt.Fprintf(&b, "  accepted  %9s  %s\n", sizeNote(oc), oc.Name)
	}
	fmt.Fprintf(&b, "REFUSED BY THE NAME PREDICATE (the human decision list): %d entries, %s\n",
		len(e.refusedAll), byteCount(e.RefusedBytes))
	total := int64(0)
	for _, oc := range e.refusedAll {
		total += oc.Bytes
		fmt.Fprintf(&b, "  refused   %9s  age %-6s  %-40s %s\n", sizeNote(oc), shortAge(oc.Age), oc.Name, oc.Verdict)
	}
	fmt.Fprintf(&b, "  (refused total measured: %s; the sum of the lines above is %s — a difference means a size "+
		"could not be taken, and it is stated rather than hidden)\n", byteCount(e.RefusedBytes), byteCount(total))
	fmt.Fprintf(&b, "RECLAIM FACES: base residue plan %s; per-run suite roots %s (stale window %s)\n",
		countNote(e.Collectable, e.CollectableErr), countNote(e.SuiteRunCollectable, e.SuiteRunErr), e.OlderThan)
	return b.String()
}

// reportBaseSaturation prints the evidence to stderr — the channel this
// package already reports a run's placement on (reportScratchPlacement) — and
// returns it, so a failing test can add its own conclusion to the same numbers
// instead of inventing a second story about them.
func reportBaseSaturation(base string) baseSaturationEvidence {
	ev := baseSaturationEvidenceFor(base, scratch.ResolveStaleAge())
	fmt.Fprintf(os.Stderr, "%s", ev.Report())
	return ev
}

// byteNote renders a measured free-space reading, or says it could not be
// taken (a number that could not be obtained is not a zero — Rule 15).
func byteNote(n uint64, err error) string {
	if err != nil {
		return "free space UNMEASURABLE (" + err.Error() + ")"
	}
	return scratch.FormatBytes(n)
}

// byteCount renders a summed size for the report.
func byteCount(n int64) string {
	if n < 0 {
		return "unknown" // never a negative total dressed up as a size
	}
	return scratch.FormatBytes(uint64(n))
}

// sizeNote renders one occupant's size, marking a partial measurement.
func sizeNote(oc baseOccupant) string {
	if !oc.Dir {
		return scratch.FormatBytes(uint64(maxInt64(oc.Bytes, 0)))
	}
	if oc.Truncated {
		return ">=" + scratch.FormatBytes(uint64(maxInt64(oc.Bytes, 0)))
	}
	return scratch.FormatBytes(uint64(maxInt64(oc.Bytes, 0)))
}

// autoResidueMode names which way the automatic base-residue arm is set, so a
// plan count of zero is read with the right expectation.
func autoResidueMode(enabled bool) string {
	if enabled {
		return "automatic arm ON (every residue family, per-run suite roots included)"
	}
	return "automatic arm OFF (per-run suite roots are the EXPLICIT sweep's; a zero plan count above is expected and is not a broken reclaimer)"
}

// countNote renders one reclaim face's plan count, or its own failure.
func countNote(n int, err error) string {
	if err != nil {
		return fmt.Sprintf("could not be planned: %v", err)
	}
	return fmt.Sprintf("%d entr(y|ies) collectable now", n)
}

// truncateName keeps the report's columns readable without hiding which entry a
// line is about: a name longer than the column keeps its TAIL, because the tail
// is what distinguishes one generated name from another.
func truncateName(name string) string {
	if len(name) <= baseEvidenceNameWidth {
		return name
	}
	return "…" + name[len(name)-(baseEvidenceNameWidth-1):]
}

// shortAge renders an entry's age compactly (the report lists many).
func shortAge(age time.Duration) string {
	if age <= 0 {
		return "-"
	}
	switch {
	case age >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	case age >= time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	case age >= time.Minute:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(age.Seconds()))
	}
}

// maxInt64 keeps a size formatter from turning a negative into an enormous
// unsigned value (Rule 15: a nonsense number is not a measurement).
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// The VERDICT: saturation vs defect.
// ---------------------------------------------------------------------------

// placementOutcome is WHICH situation an ambient run is in. The two failure
// situations are different readers' problems, so they are different values and
// never one "FAIL" (the defect this file exists to remove).
type placementOutcome int

const (
	// placementOutcomeChosen: the placement took a memory-backed candidate and
	// the assertion about the ambient channel holds.
	placementOutcomeChosen placementOutcome = iota
	// placementOutcomeBaseSaturated: THE HOST is full. The placement declined a
	// candidate that genuinely has no room — the logic working. Nothing here is
	// a code regression: the remedy is room on the memory filesystem.
	placementOutcomeBaseSaturated
	// placementOutcomeDefect: the placement declined although the LIVE base has
	// room (or took something it should not have). This IS a code regression,
	// and only this outcome may be reported as one.
	placementOutcomeDefect
)

func (o placementOutcome) String() string {
	switch o {
	case placementOutcomeChosen:
		return "placement chose a memory-backed root"
	case placementOutcomeBaseSaturated:
		return "BASE SATURATED: the memory base has no room (the placement logic is NOT the finding — this is an environment condition)"
	case placementOutcomeDefect:
		return "PLACEMENT DEFECT (code regression): the placement declined or chose wrongly while the host had room"
	default:
		return fmt.Sprintf("unknown placement outcome %d", int(o))
	}
}

// classifyAmbientPlacement answers which of the three situations this is, from
// the DECISION and the LIVE measurement of the base — never from a fixture's
// opinion about free space (a fixture can break the reading, which is exactly
// why the decision and the environmental verdict are separate inputs here).
//
// An unmeasurable LIVE base cannot prove room, so it is reported as the
// environment condition with the failure named in the text: the same verdict
// the placement itself reaches for an unmeasurable candidate (Rule 15 — a
// number that could not be obtained is not a yes, and it is not a code
// regression either).
func classifyAmbientPlacement(p scratchPlacement, liveFree uint64, liveErr error) placementOutcome {
	if p.memory {
		return placementOutcomeChosen
	}
	if liveErr != nil {
		return placementOutcomeBaseSaturated
	}
	if liveFree < scratch.MinMemoryPlacementFreeBytes {
		return placementOutcomeBaseSaturated
	}
	return placementOutcomeDefect
}

// AwaitAmbientPlacement is how a test asks the AMBIENT channel what it decides
// — through the production entry point, reclaim step included — and then
// measures the base itself.
//
// It waits for the base to settle before judgement, and the wait is bounded
// (placementAmbientSettleTimeout). WHY (measured on this host, 2026-10-12): the
// free space of the shared /dev/shm moves while a suite runs, and it recovers
// as soon as a stale root is collected. Reading it ONCE at t=0 turns a
// recovering host into a red assertion; the placement's own reclaim-before-
// decline step is what makes the second reading different from the first, so
// the loop drives THAT step (scratchPlacementFor) rather than sleeping on a
// number. A base that is still full at the deadline is reported as saturated —
// the wait never manufactures the outcome it wants.
// ambientPlacementRead is how a test asks the AMBIENT channel what it decides
// — through the production entry point, whose reclaim-before-decline step is
// included — and then measures the base itself. The two halves are returned
// together because the VERDICT is about the gap between them: the decision the
// code reached, and what the machine actually has.
//
// It reads ONCE, deliberately. An earlier version waited up to 30 s for a
// saturated base to recover, which was both useless and wrong (measured
// 2026-10-12): the placement's own reclaim-before-decline already re-measures
// after it collects, so a second reading by the CALLER cannot change the answer
// (the placement's inputs are the filesystem kinds and the caller's channels —
// deterministic state), and the wait turned a 5 s verdict into a 40 s one. The
// one thing a caller could re-read is the free space, which is exactly what
// baseFreeBytes returns on the same pass.
func ambientPlacementRead(base string) (scratchPlacement, uint64, error) {
	p := ambientPlacementFor()
	free, err := baseFreeBytes(base)
	return p, free, err
}

// AwaitAmbientPlacement is the call-site name the two ambient placement tests
// use (see ambientPlacementRead for why it reads once and why the placement's
// own reclaim step is included).
func AwaitAmbientPlacement(base string) (scratchPlacement, uint64, error) {
	return ambientPlacementRead(base)
}

// production decision, taken through scratchPlacementFor with the channels a
// test can honestly supply.
//
// WHY IT IS A VARIABLE (the same reason memoryFreeBytes is one): the epic's
// acceptance requires that a BROKEN placement still turns these tests red, and
// the only way to show that is to inject the broken decision. Nothing in
// production replaces it; a test that does so says so in its name, restores the
// previous value, and never runs outside a fixture's own subprocess.
//
// What is NOT passed in, deliberately: an ambient TMPDIR. By the time a test
// body runs, the suite pin has already OVERWRITTEN TMPDIR with the run's own
// root (scratchroot.go) — so that value is the pin's OUTPUT, not a caller's
// channel, and feeding it back would make this call answer "memory-root" and
// assert a property of the pin's own product (measured 2026-10-12).
var ambientPlacementFor = func() scratchPlacement { return scratchPlacementFor("", "") }

// baseFreeBytes is the ONE place this file measures a base's room. It is a
// variable for the same reason: a fixture can then reproduce the SATURATED
// verdict on a host that happens to have room, which is how the two conclusions
// stay demonstrable on any machine (Rule 27 bans producing the condition for
// real). Production never replaces it.
var baseFreeBytes = scratch.FreeBytes
