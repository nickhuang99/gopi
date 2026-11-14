package devworkspace

// disk_exhaustion.go — the build step's SECOND outcome: the filesystem refused
// the write.
//
// THE DEFECT THIS FILE CLOSES (measured on this host). Every clone-channel run
// compiles inside its ground under `/home/user/workspace`, a 3.6 TiB partition
// that had reached 2.3 GiB free. The declared build then died with
//
//	no space left on device
//
// and the channel classified it exactly like a compile error: `BlockerKindBuild`,
// "the build step refused; the tool's own words: …". The row told its reader that
// the CODE was broken, so the next round re-ran the same build (same bytes, same
// refusal), and the agent went looking for a defect in a revision that had never
// been linked. Reclamation was never proposed, because nothing in the sentence
// named a filesystem.
//
// The partition was not full of task grounds: measured in the same session on
// that partition, the Go TOOLCHAIN cache `/home/user/workspace/.gocache` held
// 559.4 GiB — `gopi tool scratch-gc gocache` prints
// `body=559.4 GiB … bound=25.0 GiB … reclaimable=436.3 GiB` — while every task
// ground together is a rounding error beside it. So the one useful thing to say
// to the reader of an ENOSPC blocker is WHICH filesystem, HOW MUCH is free, and
// the command that reclaims the shared cache. That is what this kind says.
//
// WHY THE VOCABULARY IS NOT WRITTEN HERE (Rule 43): `internal/scratch` already
// owns the disk-exhaustion marker list and the compiler-diagnostic shape —
// `DiskExhaustionMarkers` / `DiskExhaustionDiagnosticShape` — because the
// merge-time build gate needed the same classification and a second copy would
// drift (a phrase re-spelled slightly matches on one surface and not the other,
// and the surface that stopped matching reports a full disk as a code failure
// again). This file IMPORTS that vocabulary; it does not restate it.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// diskExhaustionDiagnosticShape is scratch's diagnostic shape compiled once
// (Rule 2: one reader of the one definition). It is the guard that keeps the
// class honest: a build output carrying `file.go:line:col:` was COMPILED, so
// the failure is a statement about the revision whatever else it mentions — a
// repo whose build runs a test suite can print `no space left on device` inside
// a failing test's message while the compile error is the real defect, and
// relabelling that as a full disk sends the reader to the wrong machine.
var diskExhaustionDiagnosticShape = regexp.MustCompile(scratch.DiskExhaustionDiagnosticShape)

// diskExhaustedBuild reports whether a FAILED build's output is the filesystem
// refusing the write, and names the marker that matched.
//
// The diagnostic-shape guard runs FIRST and wins: an output with a compiler
// position is a code failure (the 2026-09-13 incident shape), and it stays one
// even when a disk phrase appears somewhere in it.
//
// Matched case-insensitively — the phrase is a symptom, not an identifier, and
// the toolchain's spelling of it varies (see scratch.DiskExhaustionMarkers).
func diskExhaustedBuild(out string) (marker string, yes bool) {
	if diskExhaustionDiagnosticShape.MatchString(out) {
		return "", false
	}
	lower := strings.ToLower(out)
	for _, m := range scratch.DiskExhaustionMarkers {
		if strings.Contains(lower, strings.ToLower(m)) {
			return m, true
		}
	}
	return "", false
}

// buildBlocker classifies ONE failed build into the blocker the channel hands
// back. It is the only place the build step's two outcomes are decided, so the
// caller cannot accidentally keep the code-failure wording for a full disk.
func buildBlocker(dir, out string) *Blocker {
	words := tail(out)
	if marker, yes := diskExhaustedBuild(out); yes {
		return &Blocker{
			Kind:   BlockerKindDiskExhaustion,
			Step:   buildStepCommand,
			Words:  words,
			Ground: dir,
			Marker: marker,
		}
	}
	return &Blocker{Kind: BlockerKindBuild, Words: words}
}

// buildStepCommand is the ONE spelling of the step this blocker names (Rule 1b).
// The declared command itself is the repository's declaration, not this
// channel's, so what is named here is the step — the reader copies the refusal,
// not a command that would differ per repo.
const buildStepCommand = "the repository's declared build command"

const (
	// diskExhaustionReportFmt is the disk kind's OWN sentence. It replaces —
	// never decorates — the code-failure wording: the four facts a reader acts
	// on are that the write was refused (so the revision was never linked and
	// nothing about it was measured), WHICH path was measured, what that
	// filesystem has left, and the command that reclaims the shared cache.
	diskExhaustionReportFmt = CloneChannelPrefix + "the build step was refused by the FILESYSTEM, not by the code — %q means the write itself could not be satisfied, so the revision was NEVER LINKED and this run measured nothing about the branch. Filesystem measured at the ground %s: %s. Task grounds are not what fills it: the Go toolchain cache is a SHARED cross-project asset and the usual holder — measure it with `gopi tool scratch-gc gocache`, and reclaim it with `gopi tool scratch-gc gocache --trim` (the operator's decision: this channel never reclaims a shared cache, and Rule 42 forbids deleting what this run did not create)"
	// diskExhaustionGroundUnmeasuredFmt is printed in place of the figures when
	// the measurement itself fails (Rule 15b: an unmeasurable path is reported,
	// never replaced by a default that would read as a healthy disk).
	diskExhaustionGroundUnmeasuredFmt = "<could not be measured: %v>"
	// diskExhaustionGroundFmt renders one filesystem reading: WHAT it has free,
	// at which device (Rule 22: a layout is judged by device id, never by a
	// mount spelling).
	diskExhaustionGroundFmt = "%s (device %d) has %s free"
	// diskExhaustionUnmeasurableDeviceFmt is the half-measured case: the free
	// space was read but the device could not be — still a measurement, still
	// reported, never dropped for the sake of one uniform sentence.
	diskExhaustionUnmeasurableDeviceFmt = "%s has %s free (device unmeasured: %v)"
)

// diskExhaustionReport renders the disk kind's refusal by MEASURING the ground's
// filesystem at render time.
//
// WHY AT RENDER TIME: the free space at the moment the build died is a fact
// about that moment, and a stale figure printed next to "the disk was full" is
// the contradiction this kind exists to avoid (the merge gate learned the same
// lesson: `/` printing 131 GiB free beside a build that died of ENOSPC reads as
// a code defect). Measurements are read-only and zero-dependency (Rule 35) —
// statfs, no subprocess, nothing written.
func diskExhaustionReport(b *Blocker) string {
	return fmt.Sprintf(diskExhaustionReportFmt, b.Marker, b.Ground, diskExhaustionGroundState(b.Ground))
}

// diskExhaustionGroundState renders the ground's filesystem, or says why it
// could not.
func diskExhaustionGroundState(dir string) string {
	free, err := scratch.FreeBytes(dir)
	if err != nil {
		return fmt.Sprintf(diskExhaustionGroundUnmeasuredFmt, err)
	}
	fstype, _ := scratch.FilesystemTypeAt(dir)
	dev, devErr := scratch.DeviceID(dir)
	if devErr != nil {
		return fmt.Sprintf(diskExhaustionUnmeasurableDeviceFmt, fstype, scratch.FormatBytes(free), devErr)
	}
	return fmt.Sprintf(diskExhaustionGroundFmt, fstype, dev, scratch.FormatBytes(free))
}

// diskExhaustionDecisivePatterns is the disk kind's decisive-line vocabulary,
// DERIVED from scratch's marker list (Rule 43/14: one definition, referenced).
// The markers are the decision here: they are the words that say WHICH refusal
// this was, and the extractor matches them case-insensitively as substrings.
func diskExhaustionDecisivePatterns() []string {
	out := make([]string, 0, len(scratch.DiskExhaustionMarkers))
	for _, m := range scratch.DiskExhaustionMarkers {
		out = append(out, strings.ToLower(m))
	}
	return out
}
