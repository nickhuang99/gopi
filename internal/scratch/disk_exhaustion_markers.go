package scratch

// The DISK-EXHAUSTION marker vocabulary — the ONE definition, shared by the two
// surfaces that classify a failed build.
//
// THE TWO READERS, and why neither can simply import the other:
//
//   - the MERGE-TIME build gate (internal/tools/merge_build_gate.go) classifies a
//     failed build as an environment refusal and ABSTAINS instead of archiving a
//     branch whose code was never linked (the 2026-10-12 incident);
//   - the DECLARED TEST ENTRY POINT (scripts/test-budget.sh) reports it as its own
//     verdict with its own exit code, because the surface a developer runs is the
//     one that kept calling it a test failure (measured 2026-10-15: /home at
//     100%, `FAIL <pkg> [build failed]` for a package the linker never linked).
//
// One is Go and one is shell, so a shell variable cannot reference a Go constant.
// A bare literal on each side drifts silently — the defect Rules 13/14 exist to
// prevent: a kernel phrase re-spelled slightly (`no space left` vs `no space left
// on device`) matches on one surface and not the other, and the surface that
// stopped matching reports a full disk as a code failure again with NOTHING to
// say that it did. So the vocabulary has exactly ONE home — this file — both Go
// readers reference it, and the shell side is pinned TO it by a gate that reads
// the script's own declaration (disk_exhaustion_markers_test.go).
//
// WHY THIS PACKAGE: it is the small leaf every consumer can import (Rule 14) —
// internal/tools already imports it. NOTHING here imports "testing" or any test
// framework: this is production code on the shipped path.

// DiskExhaustionMarkers are the phrases that mean THE WRITE ITSELF could not be
// satisfied — the filesystem refused the bytes, whatever the revision says.
//
// WHY A LIST AND NOT ONE PHRASE: the kernel's strerror text is only one of the
// spellings the same refusal arrives in. Measured on this host: the kernel text
// from the linker, the errno NAME from some toolchains, modernc.org/sqlite's
// (this repo's driver) own phrasing, and the quota-enforced form — four doors to
// one condition. A classifier that knew only the first would call three of them
// code failures.
//
// Matched case-insensitively: the phrase is a symptom, not an identifier, and the
// toolchain's spelling of it varies.
var DiskExhaustionMarkers = []string{
	// The kernel's own spelling (the toolchain, the linker and sqlite all report
	// it verbatim).
	DiskExhaustionNoSpaceMarker,
	// The errno NAME, which some toolchains print instead of the strerror text.
	DiskExhaustionENOSPCMarker,
	// modernc.org/sqlite (this repo's driver) and the Go runtime's WAL/db paths.
	DiskExhaustionDiskFullMarker,
	// A filesystem that enforces a quota rather than reporting a full device:
	// the same class — the write is refused by the environment, not by the code.
	DiskExhaustionQuotaMarker,
}

// DiskExhaustionDiagnosticShape is the SHAPE of a compiler diagnostic
// (`file:line:col:`), which is what makes a failed build a statement about the
// REVISION rather than about the machine.
//
// IT IS THE GUARD THAT KEEPS THE CLASS HONEST, on both surfaces: a build output
// carrying a diagnostic was COMPILED, so the failure is about the code, whatever
// else it mentions. Without it a repo whose build runs a test suite could print a
// disk phrase inside a failing test's name and have its real compile error
// relabelled as a full disk — a wrong answer that reads exactly like a right one
// (the 2026-09-13 incident shape). It is deliberately the SHAPE and not one
// language's file extension: the gate builds whatever a repo declares.
//
// WRITTEN IN THE ONE DIALECT BOTH READERS ACCEPT — `[0-9]`, never `\d`. The Go
// side compiles this with regexp (RE2, which accepts either), but the shell side
// matches it with `grep -E`, whose POSIX ERE has no `\d` at all (GNU grep reads it
// as a literal `d`). A `\d` spelling here would therefore be a guard that quietly
// stopped matching on the entry point — while still matching for the Go gate, so
// nothing would look wrong. The drift gate caught exactly this on the first run.
const DiskExhaustionDiagnosticShape = `[0-9]+:[0-9]+:`

const (
	// DiskExhaustionNoSpaceMarker is ENOSPC's strerror text — the ONE spelling
	// (Rule 13) referenced by the class below and by the module-resolution marker
	// list in internal/tools.
	DiskExhaustionNoSpaceMarker = "no space left on device"
	// DiskExhaustionENOSPCMarker is the errno's name as the toolchain may print
	// it.
	DiskExhaustionENOSPCMarker = "enospc"
	// DiskExhaustionDiskFullMarker is the driver's own phrasing of the same
	// refusal.
	DiskExhaustionDiskFullMarker = "database or disk is full"
	// DiskExhaustionQuotaMarker is the quota-enforced form of the same refusal.
	DiskExhaustionQuotaMarker = "disk quota exceeded"

	// DiskExhaustionShellScriptPath is the declared test entry point, relative to
	// the repository root — the file carrying the shell half. The gate below
	// resolves it; nothing else reads it.
	DiskExhaustionShellScriptPath = "scripts/test-budget.sh"
	// DiskExhaustionShellMarkersVar / DiskExhaustionShellShapeVar are the shell
	// side's own declarations of the two values above. Named (Rule 1b) so the
	// gate that pins the two spellings together does not carry a second copy of
	// them.
	DiskExhaustionShellMarkersVar = "disk_exhaustion_markers"
	DiskExhaustionShellShapeVar   = "disk_exhaustion_diagnostic_reshaper"
)
