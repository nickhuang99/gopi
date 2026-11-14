package testutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// placementGateSuite labels the reporting-gate cases below. It is a value with
// meaning (it is printed and asserted), so it has one definition here rather
// than a literal at each call site (Rule 1b/13).
const placementGateSuite = "gate"

// Scratch-placement gates: the suite's throwaway root must land on a MEMORY
// filesystem when the host has one, and a device-backed run must SAY so.
//
// It is the largest test in this package, so TestMain stubs the binary name
// every subprocess error text prints (legacy_residue_test.go): a suite that
// formats four words per test is not worth a per-test source walk, and the
// walk is memoized behind TestMainBinaryName.
//
// WHY THESE EXIST (2026-09-26): the suite root sat on the block device the
// scratch home lives on (measured device 2052, ext4), where the first open of
// every fresh ledger pays that device's journal commits — shared with every
// other writer on it (two live `gopi worker` daemons and a `gopi merge --every
// 5m` at the time of the measurement). The same first open on tmpfs costs
// 16-24 ms instead of 431-2101 ms. The placement is what removes that cost; the
// REPORT is what stops it from hiding again.

// TestScratchPlacementIsMemoryBackedOnThisHost is the live assertion on the
// running pin: this process's scratch root is memory-backed and the decision
// names the channel that produced it.
//
// It asserts the AMBIENT channel (no caller pinned a root), so a run that DID
// pin one has nothing to assert here and says so: GOPI_TEST_TMPDIR names the
// scratch home itself, and the placement then takes that one location from the
// caller — the device arm. The device-arm run is not uncovered, it is covered
// by the other arm of TestSuiteScratchRootIsPinnedUnderTheScratchHome (the
// caller's choice is an explicit operator decision, never a failure — the same
// rule TestDeviceScratchDir applies when it skips on a memory-backed host).
func TestScratchPlacementIsMemoryBackedOnThisHost(t *testing.T) {
	if override := SuiteTempRootOverride(); override != "" {
		t.Skipf("%s names the scratch home itself (%s), so no ambient channel decides this run's placement — "+
			"the assertion about which AMBIENT channel decides has nothing to measure here; the device arm "+
			"this override produces is asserted by TestSuiteScratchRootIsPinnedUnderTheScratchHome",
			SuiteTempRootEnvName, override)
	}
	base := MemoryBase()
	if mem, description := ScratchRootFilesystem(base); !mem {
		t.Skipf("host memory base %s is %s — a device-backed host has no cheap root to assert", base, description)
	}
	// The decision, taken through the PRODUCTION entry point (reclaim-before-
	// decline included), and the LIVE state of the base measured separately: the
	// first is what the code decides, the second is what the machine has, and
	// the gap between them is the whole point (classifyAmbientPlacement).
	p, liveFree, liveErr := AwaitAmbientPlacement(base)
	switch verdict := classifyAmbientPlacement(p, liveFree, liveErr); verdict {
	case placementOutcomeDefect:
		// The ONLY situation this test may call a code regression: the host HAD
		// the room and the placement did not use it.
		reportBaseSaturation(base)
		t.Fatalf("PLACEMENT DEFECT (a code regression, not an environment condition): live base %s has %s free "+
			"— above the %s floor — yet the decision is %q and this run's root is %s. The placement declined a "+
			"candidate that had room, or chose somewhere it should not have; scratchPlacementFor / "+
			"memoryPlacementCandidate is where that decision is taken. (The MEMORY BASE evidence above is "+
			"printed by this line's own report — read it for the numbers, not as a cause: the cause here is "+
			"the code.)",
			base, byteNote(liveFree, liveErr), scratch.FormatBytes(MinMemoryPlacementFreeBytes), p.label,
			os.Getenv(TempDirEnvName))
	case placementOutcomeBaseSaturated:
		// THE HOST IS FULL — and this is NOT a placement finding. Declining a
		// candidate with no room is the placement working as designed; the run's
		// root is on the device because there is nowhere cheap to put it. The
		// evidence above is read-only (no byte was written, nothing deleted) and
		// answers the question the misleading old text buried: WHY is the base
		// full, and is the reclaimer failing?
		reportBaseSaturation(base)
		t.Errorf("BASE SATURATED — THE PLACEMENT LOGIC IS NOT THE FINDING HERE. %s has %s free, below the %s "+
			"floor, so the placement declined it (decision %q) and this run's root is %s. The placement is doing "+
			"exactly what it is designed to do; the remedy is room on the memory filesystem, or a human decision "+
			"about the entries the name predicate refuses (the HUMAN DECISION LIST above). Declined candidates, as "+
			"the decision itself reported them: %s",
			base, byteNote(liveFree, liveErr), scratch.FormatBytes(MinMemoryPlacementFreeBytes), p.label,
			os.Getenv(TempDirEnvName), declinesNote(p))
	default:
		// THE PLACEMENT CHOSE A MEMORY-BACKED ROOT. The label and the directory
		// asserted here are the PIN'S OWN DECISION (scratchPlacementDecision),
		// not the ambient re-derivation above: the pin ran once, at startup, with
		// the caller's real channels, and that is the decision this test exists to
		// audit.
		decided := scratchPlacementDecision
		if decided.label == "" {
			t.Fatal("the suite pin recorded no placement decision — the assertion below would be vacuous (Rule 15)")
		}
		if !decided.memory {
			t.Fatalf("placement %q: this suite's root is NOT memory-backed although %s is — a fresh ledger built "+
				"under the root pays the device's journal commits (the placement decision is in scratch_placement.go)",
				decided.label, base)
		}
		if decided.label != placementMemoryBase {
			t.Fatalf("placement label = %q, want %q (no GOPI_TEST_TMPDIR and no memory-backed scratch home here, so "+
				"the host base is the channel that decides)", decided.label, placementMemoryBase)
		}
		if decided.dir == "" {
			t.Fatal("a memory-backed placement must name the directory it chose")
		}
		root := os.Getenv(TempDirEnvName)
		if !strings.HasPrefix(root, decided.dir+string(filepath.Separator)) {
			t.Fatalf("%s = %q, want a path under %q (the placement must be the root TMPDIR is pinned to)", TempDirEnvName, root, decided.dir)
		}
		if decided.freeBytes < MinMemoryPlacementFreeBytes {
			t.Errorf("the decision took %s with freeBytes=%d — below the %s floor it must decide on",
				decided.dir, decided.freeBytes, scratch.FormatBytes(MinMemoryPlacementFreeBytes))
		}
	}
}

// TestScratchPlacementHonoursACallerRootAndReportsADevice is the decision table
// for scratchPlacementFor, driven through real directories whose filesystem
// types are MEASURED (never assumed from a path spelling — Rule 22).
func TestScratchPlacementHonoursACallerRootAndReportsADevice(t *testing.T) {
	deviceDir := TestDeviceScratchDir(t, "placement") // skips on a host with no device
	if mem, _ := ScratchRootFilesystem(deviceDir); mem {
		t.Fatalf("%s is memory-backed, so this case cannot measure the device arm", deviceDir)
	}

	// ① GOPI_TEST_TMPDIR names the scratch HOME itself: the location is the
	// caller's, and the DEVICE arm is entered rather than overridden.
	got := scratchPlacementFor(deviceDir, "")
	if got.memory || got.label != placementDeviceRoot {
		t.Fatalf("a caller home on a device: got {memory=%v label=%q}, want the device arm", got.memory, got.label)
	}

	// ② a caller TMPDIR on a MEMORY filesystem is that root (it is memory-safe,
	// so there is nothing to place) — UNLESS that filesystem has no room, in
	// which case the same saturation reading as above applies and the placement
	// is right to decline it (asserting the memory-root arm unconditionally
	// would fail on a full memory filesystem for a reason that is not a defect).
	base := MemoryBase()
	ambient, liveFree, liveErr := ambientPlacementRead(base)
	if mem, description := ScratchRootFilesystem(base); mem {
		got = scratchPlacementFor("", base)
		switch classifyAmbientPlacement(got, liveFree, liveErr) {
		case placementOutcomeChosen:
			if !got.memory || got.label != placementMemoryRoot || got.dir != base {
				t.Fatalf("a caller TMPDIR on %s: got {memory=%v label=%q dir=%q}, want the memory-root arm",
					description, got.memory, got.label, got.dir)
			}
		case placementOutcomeBaseSaturated:
			reportBaseSaturation(base)
			if got.memory {
				t.Fatalf("a saturated memory filesystem (%s has %s free) was taken as the caller root anyway: %+v",
					base, byteNote(liveFree, liveErr), got)
			}
			if len(got.declines) == 0 {
				t.Errorf("the caller-root decline carries no measured note: %+v — the reader has to be able to "+
					"tell a full memory filesystem from a host that has none", got)
			}
			t.Logf("BASE SATURATED (not a placement defect): the caller's memory filesystem %s has %s free, below "+
				"the %s floor, so the placement declines it too — the evidence is printed above.",
				base, byteNote(liveFree, liveErr), scratch.FormatBytes(MinMemoryPlacementFreeBytes))
		default:
			reportBaseSaturation(base)
			t.Fatalf("a caller TMPDIR on %s: the host has %s free (above the %s floor) yet the caller's own "+
				"memory filesystem was not taken ({memory=%v label=%q}) — a PLACEMENT DEFECT (see the MEMORY BASE "+
				"evidence above)", description, byteNote(liveFree, liveErr),
				scratch.FormatBytes(MinMemoryPlacementFreeBytes), got.memory, got.label)
		}
	} else {
		t.Logf("host base %s is %s — the memory-caller arm is not measurable here", base, description)
	}

	// ③ THE REGRESSION THIS FIX IS ABOUT: a caller TMPDIR on a DEVICE must not
	// drag the whole suite onto that device. The suite's own pin overwrites
	// TMPDIR with its run root, so an ambient TMPDIR never decided this suite's
	// location — the placement looks for a cheap root of its own instead.
	//
	// The expectation is taken from the LIVE measurement of the base (the same
	// decision, minus the caller's TMPDIR): with room, the memory-base arm; with
	// the base saturated, the device arm — and only when the host HAD the room
	// and still produced the device arm is that a placement defect. Asserting
	// `placementMemoryBase` unconditionally made a FULL /dev/shm fail this test
	// with a message about the placement logic (measured 2026-10-12).
	ambient, liveFree, liveErr = ambientPlacementRead(base)
	got = scratchPlacementFor("", deviceDir)
	if mem, _ := ScratchRootFilesystem(base); mem {
		switch want := classifyAmbientPlacement(ambient, liveFree, liveErr); want {
		case placementOutcomeChosen:
			if !got.memory || got.label != placementMemoryBase {
				t.Fatalf("a caller TMPDIR on a device with %s available: got {memory=%v label=%q dir=%q}, want the "+
					"memory-base arm — a device-backed ambient TMPDIR must not decide where this suite's ledgers "+
					"are built", base, got.memory, got.label, got.dir)
			}
		case placementOutcomeDefect:
			reportBaseSaturation(base)
			t.Fatalf("a caller TMPDIR on a device: live base %s has %s free (above the %s floor) so the caller's "+
				"device TMPDIR must not displace the memory base, yet the ambient decision is %q — a PLACEMENT "+
				"DEFECT (see the MEMORY BASE evidence above)",
				base, byteNote(liveFree, liveErr), scratch.FormatBytes(MinMemoryPlacementFreeBytes), ambient.label)
		default:
			// BASE SATURATED: the memory base has no room, so the device arm is
			// the CORRECT answer here — the placement is not broken, the host is
			// full. The independent conclusion and its evidence are printed by
			// the assertion above (TestScratchPlacementIsMemoryBackedOnThisHost);
			// this case states its own verdict so a reader of THIS test's output
			// is not left guessing which of the two it is.
			reportBaseSaturation(base)
			if got.memory || got.label != placementDeviceRoot {
				t.Fatalf("memory base %s has %s free (below the %s floor) but a caller TMPDIR on a device produced "+
					"{memory=%v label=%q dir=%q} — the saturated base was selected anyway", base,
					byteNote(liveFree, liveErr), scratch.FormatBytes(MinMemoryPlacementFreeBytes), got.memory, got.label, got.dir)
			}
			t.Logf("BASE SATURATED (not a placement defect): %s has %s free, below the %s floor, so both the "+
				"ambient decision and this case correctly report the device arm — the placement logic is what "+
				"placed the caller's TMPDIR second and the memory base first; the evidence is printed above.",
				base, byteNote(liveFree, liveErr), scratch.FormatBytes(MinMemoryPlacementFreeBytes))
		}
	} else if got.memory {
		t.Fatalf("no memory filesystem exists here, yet the placement claims one: %+v", got)
	}
}

// declinesNote renders the decision's own decline notes for a failing verdict,
// and says so plainly when the decision carries none — a missing note is itself
// the finding for that case (the two need different fixes).
func declinesNote(p scratchPlacement) string {
	if len(p.declines) == 0 {
		return "(none: the decision carries no measured decline — with the live base below the floor, a decision " +
			"that does not name the candidate it refused is a reporting gap, not an environment one)"
	}
	return strings.Join(p.declines, ", ")
}

// TestScratchPlacementReportNamesTheDeviceCost pins BOTH halves of the report:
// a device-backed root is reported (naming the root and the channel that
// changes it), and a memory-backed one says which channel decided — a run whose
// placement is unstated is a run whose placement cannot be audited.
func TestScratchPlacementReportNamesTheDeviceCost(t *testing.T) {
	deviceDir := TestDeviceScratchDir(t, "placement-report")
	device := captureStderr(t, func() {
		reportScratchPlacement("gate", deviceDir, scratchPlacement{label: placementDeviceRoot})
	})
	for _, want := range []string{deviceDir, ScratchHomeEnvName, "journal commits"} {
		if !strings.Contains(device, want) {
			t.Errorf("the device report does not carry %q — a device-backed run must be loud about what its "+
				"ledgers pay:\n%s", want, device)
		}
	}
	// The memory arm names the root and the channel; it must NOT carry the
	// device warning (a false alarm is as bad as a silent one).
	memory := captureStderr(t, func() {
		reportScratchPlacement("gate", MemoryBase(), scratchPlacement{memory: true, dir: MemoryBase(), label: placementMemoryBase})
	})
	for _, want := range []string{MemoryBase(), placementMemoryBase} {
		if !strings.Contains(memory, want) {
			t.Errorf("the memory report does not carry %q — the deciding channel must be observable:\n%s", want, memory)
		}
	}
	if strings.Contains(memory, ScratchHomeEnvName) {
		t.Errorf("the memory report carries the device warning, which is false here:\n%s", memory)
	}
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what
// was written. It restores os.Stderr even when fn panics (deferred), so a
// failing case cannot leave the test binary's stderr unreachable.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = wr
	defer func() { os.Stderr = saved }()
	fn()
	if err := wr.Close(); err != nil {
		t.Fatalf("close write end: %v", err) // Rule 15b
	}
	buf, err := io.ReadAll(rd)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err) // Rule 15b: an empty capture is a value, a read error is not
	}
	if err := rd.Close(); err != nil {
		t.Fatalf("close read end: %v", err) // Rule 15b
	}
	return string(buf)
}

// ---------------------------------------------------------------------------
// The SATURATION CHECK (the epic's acceptance: a deliberately saturated memory
// base must not be selected, and the decision must carry measured bytes).
// ---------------------------------------------------------------------------

// TestScratchPlacementDeclinesASaturatedMemoryBase is the red-first proof of the
// saturation check. The condition is reproduced WITHOUT filling a real memory
// filesystem: a test that saturated /dev/shm would consume the host's RAM and
// break every other process on the machine — a load generator under another name
// (Rule 27). The seam is the ONE measurement the placement consults
// (memoryFreeBytes), so this test pins the DECISION, and
// TestScratchPlacementSaturationIsMeasuredOnTheHost pins the measurement.
//
// RED BEFORE THE FIX: before the check existed, scratchPlacementFor took a
// memory-backed base on filesystem TYPE alone, so this fixture (a memory base
// with 100 MiB free) returned the memory-base arm and the run was placed on a
// filesystem it could not fill — the state in which the measured failures
// ("is not a git work tree" for a path the suite had just created) happen.
func TestScratchPlacementDeclinesASaturatedMemoryBase(t *testing.T) {
	base := MemoryBase()
	home := ScratchHomeDir()
	if mem, description := ScratchRootFilesystem(base); !mem {
		t.Skipf("host memory base %s is %s — the saturation branch is not reachable here", base, description)
	}
	if homeMem, _ := ScratchRootFilesystem(home); homeMem {
		t.Skipf("the scratch home %s is memory-backed, so the base is not the candidate that decides", home)
	}
	saved := memoryFreeBytes
	defer func() { memoryFreeBytes = saved }()

	// ① SATURATED: below the floor → the base is NOT selected, the device arm
	// is, and the measured bytes are in the reason.
	memoryFreeBytes = func(string) (uint64, error) { return MinMemoryPlacementFreeBytes / 100, nil }
	got := scratchPlacementFor("", "")
	if got.memory {
		t.Fatalf("a memory base with %s free was selected anyway (placement %q) — that is the defect: the "+
			"run lands on a filesystem with no room and reports the shortage as an unrelated error about a "+
			"path it just created", scratch.FormatBytes(MinMemoryPlacementFreeBytes/100), got.label)
	}
	if got.label != placementDeviceRoot {
		t.Errorf("placement label = %q, want %q", got.label, placementDeviceRoot)
	}
	if len(got.declines) == 0 || !strings.Contains(strings.Join(got.declines, " "), formatBytesForTest(MinMemoryPlacementFreeBytes/100)) {
		t.Errorf("the refusal does not carry the measured free bytes: %v — a saturated host must be "+
			"distinguishable from a host with no memory filesystem at all", got.declines)
	}

	// ② EXACTLY AT THE FLOOR is enough (a floor, not a strict above).
	memoryFreeBytes = func(string) (uint64, error) { return MinMemoryPlacementFreeBytes, nil }
	if atFloor := scratchPlacementFor("", ""); !atFloor.memory || atFloor.label != placementMemoryBase {
		t.Errorf("a base exactly at the floor was declined: %+v — the floor is inclusive", atFloor)
	}

	// ③ WELL ABOVE: the memory base is taken, and its measured bytes ride the
	// decision.
	roomy := uint64(4) * MinMemoryPlacementFreeBytes
	memoryFreeBytes = func(string) (uint64, error) { return roomy, nil }
	ok := scratchPlacementFor("", "")
	if !ok.memory || ok.label != placementMemoryBase || ok.dir != base {
		t.Fatalf("a base with %s free was not taken: %+v", scratch.FormatBytes(roomy), ok)
	}
	if ok.freeBytes != roomy {
		t.Errorf("the decision carries freeBytes=%d, want the measured %d", ok.freeBytes, roomy)
	}

	// ④ UNMEASURABLE is not a yes: a memory filesystem whose free space cannot
	// be measured is declined (Rule 15).
	memoryFreeBytes = func(string) (uint64, error) { return 0, errFreeSpaceUnmeasurable }
	if unmeasurable := scratchPlacementFor("", ""); unmeasurable.memory {
		t.Errorf("an unmeasurable memory base was selected: %+v — a number that could not be obtained is not a yes", unmeasurable)
	}
}

// errFreeSpaceUnmeasurable stands in for a statfs failure in the fixture above.
var errFreeSpaceUnmeasurable = errors.New("statfs fixture: unmeasurable")

// TestScratchPlacementReclaimsEveryOwnerFamilyBeforeDeclining is the regression
// gate for the RECLAIM STEP'S COVERAGE, in two halves that fail for different
// reasons:
//
//	① the no-room branch of the placement REACHES the reclaim step, for the
//	   saturated base itself (the seam), and
//	② the reclaim step asks EVERY face whose evidence says "ours and stale" —
//	   not only the suite-run arm whose victim set happens to be empty on a host
//	   whose roots are all inside the age window.
//
// RED BEFORE THE FIX (measured on this host 2026-10-12, run-1791787081): the
// step called scratch.SweepBaseSuiteRuns alone, so a /dev/shm at 935.4 MiB free
// with 0 stale suite roots printed `reclaimed 0 stale suite root(s) (kept 1)` and
// three suites went to the block device — while the same base held 2.4 GiB of
// residue (`gopi-*`/`gocache-*`/`probe-*`, plus the legacy `gopi-suite-tmp-*`
// family) that two sibling sweeps already knew how to collect. Half ② below is
// exactly that state: a FRESH suite root (nothing to collect) beside a STALE
// entry of the base namespace (collectable, and left behind by the old body).
//
// Rule 27: no real tmpfs is touched. The free-space reading and the reclaim seam
// are the placement's two measurement vars, and the fixture base is a directory
// of its own (the caller's own tree — the same shape the base sweep's fixtures
// use). Nothing here generates load or fills a device.
func TestScratchPlacementReclaimsEveryOwnerFamilyBeforeDeclining(t *testing.T) {
	base := MemoryBase()
	home := ScratchHomeDir()
	if mem, description := ScratchRootFilesystem(base); !mem {
		t.Skipf("host memory base %s is %s — the saturation branch is not reachable here", base, description)
	}
	if homeMem, _ := ScratchRootFilesystem(home); homeMem {
		t.Skipf("the scratch home %s is memory-backed, so the base is not the candidate that decides", home)
	}

	savedFree := memoryFreeBytes
	defer func() { memoryFreeBytes = savedFree }()
	savedReclaim := memoryReclaim
	defer func() { memoryReclaim = savedReclaim }()

	// ① THE SEAM: with the base below the floor, the reclaim step is invoked
	// with the saturated base — and it is invoked BEFORE the decline is
	// recorded, so the operator's report carries the post-reclaim measurement.
	var reclaimed []string
	memoryReclaim = func(dir string) { reclaimed = append(reclaimed, dir) }
	memoryFreeBytes = func(string) (uint64, error) { return MinMemoryPlacementFreeBytes / 100, nil }
	got := scratchPlacementFor("", "")
	if got.memory {
		t.Fatalf("a saturated base was placed on anyway: %+v", got)
	}
	if len(reclaimed) == 0 {
		t.Fatalf("the saturated base %s was declined without ever asking the reclaim step — declining alone "+
			"makes the saturation permanent (nothing frees the base, so the cheap root is never used again)", base)
	}
	if filepath.Clean(reclaimed[0]) != filepath.Clean(base) {
		t.Errorf("the reclaim step was asked about %q, want the saturated candidate %q", reclaimed[0], base)
	}

	// ② THE COVERAGE: run the REAL reclaim step against a fixture base that
	// holds one STALE entry of the base namespace and one FRESH suite root. The
	// stale one must go, the fresh one must stay, and the suite-run arm on its
	// own would have collected neither.
	memoryReclaim = savedReclaim
	baseAliasDir := t.TempDir()
	t.Setenv(scratch.MemoryBaseEnvName, baseAliasDir)
	staleBaseEntry := filepath.Join(baseAliasDir, "gopi-verify-01a0a387")
	if err := os.Mkdir(staleBaseEntry, dirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", staleBaseEntry, err)
	}
	freshSuiteRoot := filepath.Join(baseAliasDir, "suite-cmd-agent-run-3113361481")
	if err := os.Mkdir(freshSuiteRoot, dirPerm); err != nil {
		t.Fatalf("mkdir %s: %v", freshSuiteRoot, err)
	}
	old := time.Now().Add(-2 * scratch.ResolveStaleAge())
	if err := os.Chtimes(staleBaseEntry, old, old); err != nil {
		t.Fatalf("age %s: %v", staleBaseEntry, err)
	}

	sweepSaturatedMemoryBase(baseAliasDir)

	if _, err := os.Lstat(staleBaseEntry); !os.IsNotExist(err) {
		t.Errorf("%s survived the reclaim (lstat err = %v) — this is the family the old body never asked about: "+
			"the base was saturated, the suite-run arm had nothing stale, and the reclaim collected nothing", staleBaseEntry, err)
	}
	if _, err := os.Lstat(freshSuiteRoot); err != nil {
		t.Errorf("%s was collected (lstat err = %v) — it is inside the stale window, so it is a RUNNING suite's "+
			"root on a shared base, and the age rule is what protects it", freshSuiteRoot, err)
	}
}

// formatBytesForTest is the suite-facing alias of the ONE byte formatter
// (internal/scratch), so an assertion on the REPORT cannot drift from the
// formatter that wrote it (Rule 14).
func formatBytesForTest(n uint64) string { return scratch.FormatBytes(n) }

// TestScratchPlacementSaturationIsMeasuredOnTheHost pins the measurement
// half: the free space the placement reads is the kernel's own number for the
// real directory — never a constant, never a guess. On this host (measured
// 2026-09-28) /dev/shm sat at 284 MiB free, i.e. BELOW the floor, which is why
// the live run above reports the device arm with a saturation note.
func TestScratchPlacementSaturationIsMeasuredOnTheHost(t *testing.T) {
	base := MemoryBase()
	if base == "" {
		t.Skip("no memory base resolved on this host")
	}
	got, err := scratch.FreeBytes(base)
	if err != nil {
		t.Fatalf("measure %s: %v", base, err)
	}
	if got == 0 {
		t.Fatalf("FreeBytes(%s) = 0 — a measurement that always returns zero would decline every base", base)
	}
	// The live path must agree with the measurement: whatever the placement
	// decided, it decided on THIS number.
	p := scratchPlacementFor("", "")
	if p.memory {
		if p.freeBytes != got {
			t.Errorf("the decision carries freeBytes=%d but the live measurement of %s is %d — a decision must "+
				"be reported with the number it was taken on", p.freeBytes, p.dir, got)
		}
		t.Logf("placement %s on %s with %s free (floor %s)", p.label, p.dir,
			scratch.FormatBytes(got), scratch.FormatBytes(MinMemoryPlacementFreeBytes))
		return
	}
	if got >= MinMemoryPlacementFreeBytes {
		t.Fatalf("%s has %s free (above the %s floor) yet the placement reports %q — the floor and the decision "+
			"disagree", base, scratch.FormatBytes(got), scratch.FormatBytes(MinMemoryPlacementFreeBytes), p.label)
	}
	t.Logf("device arm as expected: %s has %s free, below the %s floor", base,
		scratch.FormatBytes(got), scratch.FormatBytes(MinMemoryPlacementFreeBytes))
}

// TestScratchRootGuardNamesPathFilesystemAndBytes is the LOUD-GUARD test: a
// suite root with too little room must fail naming the path, the filesystem and
// the free bytes — never the misleading "is not a git work tree" a bare failure
// produced (the measured 2026-09-28 shape).
func TestScratchRootGuardNamesPathFilesystemAndBytes(t *testing.T) {
	deviceDir := TestDeviceScratchDir(t, "guard")
	// A floor nothing can satisfy, so the refusal branch is exercised on a
	// real, measurable directory without writing a byte.
	err := scratch.AssertFreeSpace(deviceDir, ^uint64(0))
	if err == nil {
		t.Fatalf("a root with an unsatisfiable floor (%s) was accepted", scratch.FormatBytes(^uint64(0)))
	}
	if !errors.Is(err, scratch.ErrInsufficientSpace) {
		t.Errorf("the refusal is not matchable as ErrInsufficientSpace: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{deviceDir, "free", "filesystem is", "scratch-gc"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not carry %q — a guard whose message names no cause is the failure it "+
				"was written to replace:\n%s", want, msg)
		}
	}
	// The creation refusal — the text a suite prints when its root cannot be
	// made — must carry the same three facts plus the suite name and the fix.
	// A root that HAS room: the creation failure is not a shortage, so the
	// status says so (97) while the report still carries the measured numbers.
	report, code := scratchRootCreationRefusal("gate", deviceDir, errScratchRootFixture)
	if code != SuiteScratchUnsafeExitCode {
		t.Errorf("exit code = %d, want %d — a root with room must not be reported as a SPACE refusal",
			code, SuiteScratchUnsafeExitCode)
	}
	// A root that cannot be measured at all: unmeasurable is not "has room", so
	// the refusal is reported as the shortage (98) rather than sending the
	// operator after a location bug that does not exist.
	missing := filepath.Join(deviceDir, "missing", "root")
	_, spaceCode := scratchRootCreationRefusal("gate", missing, errScratchRootFixture)
	if spaceCode != SuiteScratchNoSpaceExitCode {
		t.Errorf("exit code = %d for an unmeasurable root, want %d — a root that cannot be measured cannot be "+
			"proven to have room", spaceCode, SuiteScratchNoSpaceExitCode)
	}
	for _, want := range []string{"gate", deviceDir, "FIX:", ScratchHomeEnvName, "free"} {
		if !strings.Contains(report, want) {
			t.Errorf("the creation refusal does not carry %q:\n%s", want, report)
		}
	}
	// A root with room passes, and the report still measures (no silent pass).
	if err := scratch.AssertFreeSpace(deviceDir, scratch.MinSuiteRootFreeBytes); err != nil {
		t.Errorf("a device root with room was refused: %v", err)
	}
	if note := scratchRootSpaceReport(deviceDir); !strings.Contains(note, deviceDir) {
		t.Errorf("the space report does not name the path: %s", note)
	}
	// An UNMEASURABLE path is a refusal, never a pass (Rule 15).
	if err := scratch.AssertFreeSpace(filepath.Join(deviceDir, "missing", "deeper"), 1); err == nil {
		t.Errorf("an unmeasurable root was accepted — a number that could not be obtained is not a yes")
	}
}

// errScratchRootFixture stands in for the creation error a suite sees when its
// scratch root cannot be made.
var errScratchRootFixture = errors.New("mkdir fixture: no space left on device")
