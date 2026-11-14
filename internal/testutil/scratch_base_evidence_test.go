package testutil

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// Scratch-placement verdict tests: the two situations a red ambient placement
// test can mean, and the read-only evidence a saturated base reports about
// itself.
//
// THE EPIC THIS SERVES: on a host whose memory base is FULL (measured
// 2026-10-12: /dev/shm 7804604416 bytes, 1075273728 bytes free — 1.2 MiB above
// the 1.0 GiB floor), the ambient placement tests fail with a sentence that
// reads as "the placement logic is broken". It is not: declining a candidate
// with no room IS the logic. These tests pin that the two situations stay
// distinguishable, that the saturated one carries evidence, and that a BROKEN
// placement still fails.
//
// NO BYTE IS WRITTEN TO ANY FILESYSTEM TO REPRODUCE SATURATION (Rule 27): the
// condition is produced on the fixture seam (memoryFreeBytes / baseFreeBytes),
// never by filling a device.

const (
	// verdictFixtureScenarioEnvName selects the scenario inside the worker
	// process this test starts. A value with meaning, so it has ONE definition.
	verdictFixtureScenarioEnvName = "GOPI_PLACEMENT_VERDICT_SCENARIO"
	// The scenarios.
	verdictScenarioSaturated = "saturated"
	verdictScenarioRoomy     = "roomy-base-device-decision"
	verdictScenarioBroken    = "broken-placement"
	// verdictScenarioCensus prints the FULL census (every refused entry, largest
	// first) instead of a verdict: it is the artifact a human decision about
	// the base needs, produced by the shipped code rather than by a scratch
	// script (Rule 5: the deliverable is reproducible from the tree).
	verdictScenarioCensus = "census"
	// The verdict markers the worker prints for its parent to assert on.
	verdictMarkerPrefix    = "VERDICT:"
	censusMarkerBegin      = "CENSUS-BEGIN"
	censusMarkerEnd        = "CENSUS-END"
	evidenceMarkerBegin    = "EVIDENCE-BEGIN"
	evidenceMarkerEnd      = "EVIDENCE-END"
	verdictScenarioUnknown = "(unknown scenario)"
)

// TestBaseNameVerdictAnswersFromTheOnePredicate pins that the report's
// explanation cannot drift from the sweep's decision: for every probed name the
// verdict's bool IS scratch.IsBaseResidueName(name), and each refusal carries
// wording naming WHY (a refusal a reader cannot act on is the failure this
// report exists to replace).
func TestBaseNameVerdictAnswersFromTheOnePredicate(t *testing.T) {
	names := []string{
		scratch.RootDirName,                      // the scratch home itself: never this sweep's
		scratch.LegacyScratchMarker + "abc",      // the legacy family: testutil's own sweep
		"suite-internal-testutil-run-4018809286", // a per-run suite root
		"gopi-ctl-NYRf",                          // a chosen-word tail: not generated
		"gopi-verify-01a0a387",                   // generated: collectable
		"gocache-01a0a392",                       // generated: collectable
		"probe1.db",                              // no family prefix at all
		"master-check",                           // a human's name
	}
	for _, name := range names {
		collectable, verdict := baseNameVerdict(name)
		if want := scratch.IsBaseResidueName(name); collectable != want {
			t.Errorf("baseNameVerdict(%q) = %v but scratch.IsBaseResidueName = %v — the report's explanation "+
				"must be the predicate's own answer, never a second rule", name, collectable, want)
		}
		if verdict == "" {
			t.Errorf("baseNameVerdict(%q) carries no wording — a refusal a reader cannot act on is the defect "+
				"this report exists to remove", name)
		}
		if !collectable && !strings.Contains(verdict, "refused") {
			t.Errorf("baseNameVerdict(%q): a refused name must say so in words, got %q", name, verdict)
		}
	}
	// The two named refusals that are DECISIONS about ownership, not shape.
	if _, v := baseNameVerdict(scratch.RootDirName); !strings.Contains(v, "scratch home") {
		t.Errorf("the scratch home must be refused BY NAME (it is the home sweep's), got %q", v)
	}
	if _, v := baseNameVerdict("suite-x-run-1"); !strings.Contains(v, "AUTOMATIC") {
		t.Errorf("a per-run suite root in stood-down mode must be refused with the automatic-pass reason, got %q", v)
	}
	// The enum the two failures live in must never collapse into one value.
	if placementOutcomeDefect == placementOutcomeBaseSaturated {
		t.Fatal("the saturated and defect outcomes are the same value — the whole point is that a reader can " +
			"tell an environment condition from a code regression")
	}
	for _, want := range []string{"SATURATED", "NOT the finding", "environment condition"} {
		if !strings.Contains(placementOutcomeBaseSaturated.String(), want) {
			t.Errorf("the saturated verdict's text does not carry %q: %q", want, placementOutcomeBaseSaturated.String())
		}
	}
	if !strings.Contains(placementOutcomeDefect.String(), "DEFECT") {
		t.Errorf("the defect verdict's text does not name the defect: %q", placementOutcomeDefect.String())
	}
}

// TestBaseSaturationEvidenceIsReadOnlyAndNamesItsNumbers pins the evidence
// itself on a FIXTURE base (a device directory under the scratch root, so
// nothing here needs a memory filesystem): the numbers, the two reclaim faces,
// the human decision list, and — the property that makes it evidence — that the
// report changed nothing.
func TestBaseSaturationEvidenceIsReadOnlyAndNamesItsNumbers(t *testing.T) {
	base := TestDeviceScratchDir(t, "base-evidence")
	// A fixture base with one entry of each interesting verdict. The scratch
	// home's own name is refused; a per-run suite root is refused in
	// stood-down mode; a generated residue name is accepted.
	rootEntry := filepath.Join(base, scratch.RootDirName)
	for _, name := range []string{scratch.RootDirName, "suite-internal-testutil-run-1", "gopi-verify-01a0a387"} {
		if err := os.MkdirAll(filepath.Join(base, name), dirPerm); err != nil {
			t.Fatalf("fixture entry %s: %v", name, err)
		}
	}
	// The accepted entry is given a file, so the size census has something real
	// to measure (a zero-size directory would make the number unfalsifiable).
	if err := os.WriteFile(filepath.Join(base, "gopi-verify-01a0a387", "probe.bin"), make([]byte, 4096), filePerm); err != nil {
		t.Fatalf("fixture payload: %v", err)
	}
	before, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("read fixture base: %v", err)
	}
	fingerprint := baseFingerprint(t, base)

	ev := baseSaturationEvidenceFor(base, scratch.ResolveStaleAge())
	if ev.ReadErr != nil {
		t.Fatalf("the fixture base could not be read: %v", ev.ReadErr)
	}
	if ev.FreeErr != nil || ev.Free == 0 {
		t.Errorf("the free-space reading is %s (err %v) — a report about room must carry a measured number",
			byteNote(ev.Free, ev.FreeErr), ev.FreeErr)
	}
	if ev.Entries != len(before) {
		t.Errorf("the report counts %d entries, the base holds %d", ev.Entries, len(before))
	}
	if ev.Accepted != 1 {
		t.Errorf("accepted = %d, want 1 (only the generated-token name) — the fixture decides this", ev.Accepted)
	}
	if ev.RefusedSuiteRuns != 1 {
		t.Errorf("RefusedSuiteRuns = %d, want 1 (the per-run suite root, stood down in this process)", ev.RefusedSuiteRuns)
	}
	if ev.AutoResidueEnabled {
		t.Error("the report claims the automatic arm is ON while a per-run suite root came back refused — the " +
			"mode must be MEASURED from the entries, not assumed")
	}
	found := false
	for _, oc := range ev.Refused {
		if oc.Name == scratch.RootDirName {
			found = true
		}
	}
	if !found {
		t.Errorf("the HUMAN DECISION LIST does not name %q, the largest entry a human must decide about: %+v",
			scratch.RootDirName, ev.Refused)
	}
	if ev.CollectableErr != nil || ev.SuiteRunErr != nil {
		t.Errorf("a reclaim plan failed on a readable base: %v / %v", ev.CollectableErr, ev.SuiteRunErr)
	}
	report := ev.Report()
	for _, want := range []string{
		base, "HUMAN DECISION LIST", "RECLAIM FACES", "base residue plan", "per-run suite roots",
		"top-level entries", scratch.FormatBytes(scratch.MinMemoryPlacementFreeBytes),
		rootEntry, // the largest occupant's path is derivable from its name in the listing
	} {
		if want == rootEntry {
			continue // the listing names NAMES, not paths; asserted via the refused list above
		}
		if !strings.Contains(report, want) {
			t.Errorf("the evidence does not carry %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "<nil>") || strings.Contains(report, "%!") {
		t.Errorf("the evidence carries a formatting artefact — a report that cannot be read is not evidence:\n%s", report)
	}
	// READ-ONLY: the report walked the base and deleted nothing.
	after, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("re-read fixture base: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("the evidence report changed the base: %d entries before, %d after", len(before), len(after))
	}
	if got := baseFingerprint(t, base); got != fingerprint {
		t.Fatalf("the evidence report mutated the base it read — a report that changes the machine is not "+
			"evidence:\nbefore: %s\nafter:  %s", fingerprint, got)
	}
	// A base that does not exist is REPORTED as such, never as an empty base.
	ev = baseSaturationEvidenceFor(filepath.Join(base, "missing"), scratch.ResolveStaleAge())
	if ev.ReadErr == nil {
		t.Error("a base that cannot be read was reported as if it had been read (Rule 15)")
	}
	if !strings.Contains(ev.Report(), "could not be read") {
		t.Errorf("the unreadable-base report does not say so:\n%s", ev.Report())
	}
}

// TestPlacementVerdictSeparatesSaturationFromDefect is the ACCEPTANCE test: the
// three situations are driven through the production classification
// (classifyAmbientPlacement + the evidence report) and must land on three
// different verdicts.
//
// Each scenario runs in a SUBPROCESS, because the classification consults
// process-global state (the placement decision, the memoryFreeBytes seam, the
// suite's own GC registration) — a stub installed in-process would leak into
// every sibling test in this package. The subprocess is this test binary with a
// scenario name in the environment; nothing is written to any filesystem except
// the fixture base's own directory.
func TestPlacementVerdictSeparatesSaturationFromDefect(t *testing.T) {
	base := MemoryBase()
	if mem, description := ScratchRootFilesystem(base); !mem {
		t.Skipf("host memory base %s is %s — the saturated-base verdict has no candidate to be about here",
			base, description)
	}
	cases := []struct {
		scenario string
		want     placementOutcome
		wantText []string
	}{
		{
			// THE HOST IS FULL: the placement has nothing to take, and the
			// verdict must say the ENVIRONMENT is the finding.
			scenario: verdictScenarioSaturated,
			want:     placementOutcomeBaseSaturated,
			wantText: []string{"BASE SATURATED", "below the", "HUMAN DECISION LIST", "RECLAIM FACES"},
		},
		{
			// The placement declined while the LIVE base has room: a code
			// regression, and the verdict must say so.
			scenario: verdictScenarioRoomy,
			want:     placementOutcomeDefect,
			wantText: []string{"PLACEMENT DEFECT", "code regression", "had room"},
		},
		{
			// The placement logic itself is broken (the decision forced to the
			// device arm while the host has room). This is the epic's
			// acceptance requirement: a broken placement must still FAIL.
			scenario: verdictScenarioBroken,
			want:     placementOutcomeDefect,
			wantText: []string{"PLACEMENT DEFECT", "code regression", "had room"},
		},
	}
	// The base is SHARED and LIVE: a count taken before and after cannot be an
	// assertion (see the note below), and each fixture proves its own
	// read-only-ness from inside its worker.
	for _, tc := range cases {
		t.Run(tc.scenario, func(t *testing.T) {
			verdict, out := runVerdictFixture(t, tc.scenario)
			if verdict == verdictScenarioUnknown {
				t.Fatalf("the fixture printed no verdict:\n%s", out)
			}
			if verdict != tc.want.String() {
				t.Fatalf("scenario %q: verdict = %q, want %q\n%s", tc.scenario, verdict, tc.want.String(), out)
			}
			for _, want := range tc.wantText {
				if !strings.Contains(out, want) {
					t.Errorf("scenario %q: the output does not carry %q — the verdict must be actionable\n%s",
						tc.scenario, want, out)
				}
			}
			if !strings.Contains(out, evidenceMarkerBegin) {
				t.Errorf("scenario %q: no evidence block was printed\n%s", tc.scenario, out)
			}
		})
	}
	// READ-ONLY is NOT asserted here by re-counting the base: it is a SHARED
	// 1777 directory whose contents other processes change WHILE these fixtures
	// run (measured 2026-10-12: 408 → 402 top-level entries between two readings
	// taken seconds apart, with this test doing nothing to it). A count
	// assertion here could only be red for someone else's work. The property is
	// proven where it CAN be proven — on a PRIVATE fixture base, by
	// TestBaseSaturationEvidenceIsReadOnlyAndNamesItsNumbers, and by the
	// read-only construction of every function in scratch_base_evidence.go
	// (os.ReadDir / Info / WalkDir / the PLAN faces, which delete nothing).
	if len(cases) == 0 {
		t.Fatal("no scenario was exercised — the acceptance matrix is empty (Rule 15)")
	}
}

// baseFingerprint renders the entry set of a base with each entry's size and
// modification time, so a report that touched or re-created anything is
// detectable (a bare entry count would not see a replaced directory). It is
// used on a PRIVATE fixture base only — the shared memory base changes under
// other processes' hands while a test runs.
func baseFingerprint(t *testing.T, base string) string {
	t.Helper()
	entries, err := os.ReadDir(base)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	var b strings.Builder
	for _, e := range entries {
		info, ierr := e.Info()
		if ierr != nil {
			fmt.Fprintf(&b, "%s:vanished;", e.Name())
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d;", e.Name(), info.Size(), info.ModTime().UnixNano())
	}
	return b.String()
}

// runVerdictFixture runs one scenario in a subprocess of this test binary and
// returns the verdict it printed plus its whole output (the evidence block
// included, which is what the assertions read).
func runVerdictFixture(t *testing.T, scenario string) (string, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestPlacementVerdictWorkerProcess", "-test.v")
	cmd.Dir = ScratchRootQueue()
	cmd.Env = append(os.Environ(), verdictFixtureScenarioEnvName+"="+scenario)
	// A fixture must never fall through to the developer's real profile: the
	// child inherits this run's scratch-root pin, so its TMPDIR is this suite's
	// own run root (scratchroot.go).
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		// A worker that fails is a finding, not an infrastructure hiccup: the
		// output is the evidence.
		t.Fatalf("the %q fixture failed: %v\n%s", scenario, err, text)
	}
	var verdict string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, verdictMarkerPrefix) {
			verdict = strings.TrimSpace(strings.TrimPrefix(line, verdictMarkerPrefix))
		}
	}
	return verdict, text
}

// TestPlacementVerdictWorkerProcess is the subprocess half of the fixture
// above. It is skipped when no scenario is named, so it is an ordinary
// (passing) no-op in a normal run — and it can never be mistaken for a test of
// production behaviour.
func TestPlacementVerdictWorkerProcess(t *testing.T) {
	scenario := os.Getenv(verdictFixtureScenarioEnvName)
	if scenario == "" {
		t.Skip("not a fixture worker (no scenario named) — see TestPlacementVerdictSeparatesSaturationFromDefect")
	}
	base := MemoryBase()
	if mem, description := ScratchRootFilesystem(base); !mem {
		t.Skipf("worker: host memory base %s is %s", base, description)
	}
	if scenario == verdictScenarioCensus {
		fmt.Printf("%s\n%s%s\n", censusMarkerBegin,
			baseSaturationEvidenceFor(base, scratch.ResolveStaleAge()).Census(), censusMarkerEnd)
		return
	}
	switch scenario {
	case verdictScenarioSaturated:
		// THE SEAM, and the ONLY way to reproduce this without filling a real
		// filesystem (Rule 27). The placement's own measurement AND the live
		// measurement are stubbed to the same value, so the decision really does
		// decline the candidate: this is the placement working, not broken.
		saturated := func(string) (uint64, error) { return MinMemoryPlacementFreeBytes / 4, nil }
		restoreMemory, restoreBase := swapFreeBytes(saturated, saturated)
		defer restoreMemory()
		defer restoreBase()
	case verdictScenarioRoomy:
		// The base HAS room by measurement, and the decision is forced to the
		// device arm — the misconfiguration shape. Nothing about the host is
		// touched; only the reading is replaced.
		restoreBase := swapBaseFreeBytes(func(string) (uint64, error) { return 4 * MinMemoryPlacementFreeBytes, nil })
		defer restoreBase()
		restoreAmbient := swapAmbientPlacement(func() scratchPlacement {
			return scratchPlacement{label: placementDeviceRoot}
		})
		defer restoreAmbient()
	case verdictScenarioBroken:
		// THE PLACEMENT LOGIC IS BROKEN: the decision never takes a memory-backed
		// candidate at all, while the host is measured to have room. This is the
		// end-to-end proof that the tests in this package still fail when the
		// placement — not the environment — is the problem.
		restoreAmbient := swapAmbientPlacement(func() scratchPlacement {
			return scratchPlacement{label: placementDeviceRoot, declines: []string{"fixture: placement logic broken"}}
		})
		defer restoreAmbient()
	default:
		fmt.Printf("worker: unknown scenario %q\n", scenario)
		os.Exit(1)
	}
	p, free, err := ambientPlacementRead(base)
	verdict := classifyAmbientPlacement(p, free, err)
	fmt.Printf("%s%s\n", verdictMarkerPrefix, verdict)
	fmt.Printf("%s\n%s%s\n", evidenceMarkerBegin, baseSaturationEvidenceFor(base, scratch.ResolveStaleAge()).Report(), evidenceMarkerEnd)
}

// swapFreeBytes replaces BOTH free-space seams and returns their restores. The
// placement reads one (memoryFreeBytes) and the verdict reads the other
// (baseFreeBytes); a fixture that replaced only one would be measuring two
// different machines.
func swapFreeBytes(placement, base func(string) (uint64, error)) (func(), func()) {
	return swapMemoryFreeBytes(placement), swapBaseFreeBytes(base)
}

// swapMemoryFreeBytes replaces the placement's ONE measurement.
func swapMemoryFreeBytes(fn func(string) (uint64, error)) func() {
	saved := memoryFreeBytes
	memoryFreeBytes = fn
	return func() { memoryFreeBytes = saved }
}

// swapBaseFreeBytes replaces the live-base reading the verdict uses.
func swapBaseFreeBytes(fn func(string) (uint64, error)) func() {
	saved := baseFreeBytes
	baseFreeBytes = fn
	return func() { baseFreeBytes = saved }
}

// swapAmbientPlacement replaces the ambient decision entry point.
func swapAmbientPlacement(fn func() scratchPlacement) func() {
	saved := ambientPlacementFor
	ambientPlacementFor = fn
	return func() { ambientPlacementFor = saved }
}

// errVerdictFixture keeps the seam signatures honest in one place: a fixture
// that wants an unmeasurable reading returns it through here, so the failure
// path is exercised with the same shape production sees.
var errVerdictFixture = errors.New("verdict fixture: unmeasurable")

// TestPlacementVerdictTreatsAnUnmeasurableHostAsTheEnvironment pins the third
// reading: a host whose base cannot be MEASURED cannot prove room, so the
// verdict is the environment condition with the failure named — never a code
// regression, and never a silent yes (Rule 15).
func TestPlacementVerdictTreatsAnUnmeasurableHostAsTheEnvironment(t *testing.T) {
	if got := classifyAmbientPlacement(scratchPlacement{label: placementDeviceRoot}, 0, errVerdictFixture); got != placementOutcomeBaseSaturated {
		t.Errorf("an unmeasurable base classified as %v, want %v — a number that could not be obtained is not a "+
			"yes AND is not a code regression", got, placementOutcomeBaseSaturated)
	}
	// With room measured, the same decision IS the defect.
	if got := classifyAmbientPlacement(scratchPlacement{label: placementDeviceRoot}, 4*MinMemoryPlacementFreeBytes, nil); got != placementOutcomeDefect {
		t.Errorf("a declined decision with room on the host classified as %v, want %v", got, placementOutcomeDefect)
	}
	// Exactly at the floor is room (the floor is inclusive — the same rule the
	// placement applies when it TAKES a candidate).
	if got := classifyAmbientPlacement(scratchPlacement{label: placementDeviceRoot}, MinMemoryPlacementFreeBytes, nil); got != placementOutcomeDefect {
		t.Errorf("a base exactly at the floor classified as %v, want %v — the floor is inclusive on both sides",
			got, placementOutcomeDefect)
	}
	// A chosen memory root is chosen, whatever the host says.
	if got := classifyAmbientPlacement(scratchPlacement{memory: true, label: placementMemoryBase}, 0, nil); got != placementOutcomeChosen {
		t.Errorf("a memory-backed decision classified as %v, want %v", got, placementOutcomeChosen)
	}
}

// TestAmbientPlacementReadReadsOnceAndReturnsTheDecision pins the two
// properties the reader depends on: the decision comes from the ambient entry
// point (a substitute is honoured, which is what the broken-placement fixture
// needs), and the call returns the free space of the SAME pass rather than
// stalling (measured 2026-10-12: a 30 s settle loop turned a 5 s verdict into a
// 40 s one for no information).
func TestAmbientPlacementReadReadsOnceAndReturnsTheDecision(t *testing.T) {
	base := MemoryBase()
	if mem, _ := ScratchRootFilesystem(base); !mem {
		t.Skipf("host memory base %s is not memory-backed", base)
	}
	injected := scratchPlacement{label: placementDeviceRoot, declines: []string{"fixture: injected decision"}}
	restore := swapAmbientPlacement(func() scratchPlacement { return injected })
	defer restore()
	start := time.Now()
	p, free, err := ambientPlacementRead(base)
	elapsed := time.Since(start)
	if p.label != placementDeviceRoot {
		t.Errorf("the ambient decision was not taken from the entry point: %+v", p)
	}
	if len(p.declines) != 1 || p.declines[0] != injected.declines[0] {
		t.Errorf("the decision's decline notes were not carried through: %+v", p.declines)
	}
	if err != nil || free == 0 {
		t.Errorf("the same pass must return the measured free space (free=%d, err=%v)", free, err)
	}
	// A generous ceiling: the point is that there is no WAIT loop, not a
	// performance measurement (Rule 27 forbids manufacturing one).
	if elapsed > time.Second {
		t.Errorf("the reader took %s — it must read once and return", elapsed)
	}
}
