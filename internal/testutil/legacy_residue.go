package testutil

// Legacy scratch-residue sweep — the half of the scratch contract that the
// per-run pin alone cannot cover.
//
// ROOT CAUSE (measured on this host, 2026-09-11, devdoc/scratch-residue-audit.md):
// the pin that predates commit 5f64422 created its per-run root DIRECTLY inside
// a shared memory base (os.MkdirTemp(memoryBase, "gopi-suite-tmp-") — and
// internal/tools' original implementation spelled its own suite name into the
// pattern, "gopi-tools-suite-tmp-"), and the ONLY cleanup hook was TestMain's
// success path. A suite killed by a timeout, a ^C or the OOM killer therefore
// left its root behind forever. When the pin moved to the per-run scratch home
// (<home>/workspace/gopi-test-scratch/<suite>/<run>), the age GC followed it —
// and those older roots live OUTSIDE the home that GC walks, so nothing ever
// collected them. Measured before this file existed: 43 "gopi-suite-tmp-*" +
// 537 "gopi-tools-suite-tmp-*" orphans in /dev/shm. Same unbounded-growth
// mechanism as the /tmp pN_dump incident (13,044 dirs), one naming scheme
// earlier: a scratch root with no collector.
//
// THE SWEEP IS NAMESPACE-LIMITED BY CONSTRUCTION — it can never become a
// global delete:
//
//  1. It walks ONLY the memory bases resolved by MemoryBase() (the ONE reader
//     of the ONE definition of GOPI_SCRATCH_MEMORY_BASE,
//     internal/scratch.MemoryBaseEnvName — this package's MemoryBaseEnvName is
//     a true alias of that constant, never a second literal) — never /, never
//     /tmp, never a home tree.
//     A base measured to share the device of `/` is skipped outright.
//  2. It collects ONLY names in this system's legacy scratch namespace: a
//     "gopi-" prefix AND a "suite-tmp-" infix. Anything else in the base —
//     another program's directory, a "gopi-acc-*" account dir, the scratch
//     home itself, a user's own file — does not match and is never touched.
//  3. It collects ONLY a directory (Lstat: a symlink or a regular file is left
//     alone) older than the age threshold AND held by no live process (the
//     /proc cwd+fd scan and the .holder registration, the same two channels as
//     the home GC).
//  4. It never recurses: only direct children of a base, bounded by
//     scratch.ScanLimit.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/worktreeroots"
)

const (
	// MemoryBaseEnvName is the memory-backed base the scratch home is linked
	// onto. It is a TRUE ALIAS (Rule 14) of the ONE definition,
	// internal/scratch.MemoryBaseEnvName (tmproot.go) — this package already
	// imports internal/scratch, so the name adds no dependency and no import
	// cycle, and a rename touches exactly one line. It is the SAME variable
	// scripts/setup-scratch-home.sh reads — the suite-facing name and the
	// production name are one variable, never a second spelling of the value
	// (Rule 13): a mirror would let one copy be re-spelled while the setup
	// script, the sweep and this reader all kept believing they agreed.
	// TestMemoryBaseNameMatchesTheSetupScript is the drift gate that pins the
	// script's spelling to it.
	MemoryBaseEnvName = scratch.MemoryBaseEnvName
	// MemoryBaseDefault is that base when the variable is unset: the standard
	// Linux shared-memory filesystem (a tmpfs, so no fsync ever reaches a
	// block device). ShmDir is the same value under its historical name; the
	// VALUE has one definition (worktreeroots.ShmDir, Rule 14) and this name is
	// the suite-facing alias of it.
	MemoryBaseDefault = worktreeroots.ShmDir

	// gopiScratchPrefix / legacySuiteInfix name this system's legacy
	// suite-scratch namespace. BOTH must appear for a name to be collectable:
	// "gopi-suite-tmp-<rand>" (internal/testutil's pin) and
	// "gopi-tools-suite-tmp-<rand>" (internal/tools' original pin) match, while
	// "gopi-acc-<pid>", "gopi-diag-<pid>", "gopi-gate-probe-<pid>",
	// "gopi-test-scratch" and any foreign name do not. That is the namespace
	// limit that keeps this sweep bounded (Rule 1b: the markers are constants,
	// never literals at the match site).
	gopiScratchPrefix  = "gopi-"
	legacySuiteInfix   = "suite-tmp-"
	legacyResidueLabel = "suite-scratch residue"
)

// MemoryBase is the ONE place GOPI_SCRATCH_MEMORY_BASE is read (Rule 2).
// It is deliberately independent of SuiteTempRootOverride: the base names the
// host's memory filesystem, while the override names a scratch HOME.
func MemoryBase() string {
	if base := os.Getenv(MemoryBaseEnvName); base != "" {
		return scratch.CanonicalPath(base)
	}
	return MemoryBaseDefault
}

// LegacyScratchBases lists the bases whose legacy suite roots the sweep may
// collect. Empty means "sweep nothing", and that is the answer whenever an
// operator pinned GOPI_TEST_TMPDIR: an override points at a scratch HOME (a
// fixture, per PinSuiteTempRoot), and sweeping a host's real memory base on
// behalf of such a run would delete data outside the tree the caller owns.
func LegacyScratchBases() []string {
	if SuiteTempRootOverride() != "" {
		return nil
	}
	base := MemoryBase()
	if base == "" {
		return nil
	}
	return []string{base}
}

// IsLegacyScratchResidue reports whether a directory NAME belongs to this
// system's legacy suite-scratch namespace. It is the sweep's whole safety
// argument, so it is exported and table-tested directly.
func IsLegacyScratchResidue(name string) bool {
	rest, ok := strings.CutPrefix(name, gopiScratchPrefix)
	if !ok {
		return false
	}
	idx := strings.Index(rest, legacySuiteInfix)
	if idx < 0 {
		return false
	}
	// The legacy pattern always carried a random suffix after the infix
	// (os.MkdirTemp's "XXXX…"); a name that stops AT the infix is not a root
	// this system created.
	return len(rest) > idx+len(legacySuiteInfix)
}

// GCLegacyScratchResidue collects legacy suite-scratch roots older than the
// EFFECTIVE stale window (scratch.ResolveStaleAge — the SAME age policy as the
// home GC, one threshold rather than a second one to keep in sync) and returns
// how many were removed. It is called inline by PinSuiteTempRoot, so the sweep
// runs whenever anyone runs a test.
func GCLegacyScratchResidue() (int, error) {
	return GCLegacyScratchResidueOlderThan(scratch.ResolveStaleAge())
}

// GCLegacyScratchResidueOlderThan is GCLegacyScratchResidue with an explicit
// age threshold (the boundary tests' hook).
func GCLegacyScratchResidueOlderThan(olderThan time.Duration) (int, error) {
	bases := LegacyScratchBases()
	if len(bases) == 0 {
		return 0, nil
	}
	// ONE /proc pass for the whole sweep: the holder question is asked once
	// for every candidate, and a per-entry scrub of /proc would cost a full
	// process-table walk per directory (587 candidates on this host).
	holders := scratch.ScanHolders()
	now := time.Now()
	removed := 0
	var firstErr error
	for _, base := range bases {
		// A base on the / filesystem is never swept: the ban's whole point is
		// that `/` is not scratch space (and the measurement failure answer is
		// "do not touch", never "assume safe").
		if shares, err := ScratchRootSharesRootFS(base); err != nil || shares {
			continue
		}
		entries, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("read memory base %s: %w", base, err)
			}
			continue
		}
		for i, entry := range entries {
			if i >= scratch.ScanLimit {
				if firstErr == nil {
					firstErr = fmt.Errorf("memory base %s has more than %d entries — sweep stopped early", base, scratch.ScanLimit)
				}
				break
			}
			if !IsLegacyScratchResidue(entry.Name()) {
				continue // NOT our namespace: never touched, whatever its age
			}
			path := filepath.Join(base, entry.Name())
			fi, err := os.Lstat(path)
			if err != nil {
				continue // vanished between readdir and lstat
			}
			if !fi.IsDir() {
				continue // a symlink or a file is not a root this system created
			}
			if now.Sub(fi.ModTime()) <= olderThan {
				continue
			}
			if why := scratch.HolderVeto(path, holders); why != "" {
				continue // held, OR unmeasured because the scan was truncated
			}
			if alive, _ := ScratchHolderAlive(path); alive {
				continue
			}
			if err := os.RemoveAll(path); err != nil {
				// Report, never swallow (Rule 15b) — and keep sweeping: one
				// unremovable entry must not stop the rest.
				fmt.Fprintf(os.Stderr, "testutil: legacy %s %s not removed: %v\n", legacyResidueLabel, path, err)
				continue
			}
			removed++
		}
	}
	return removed, firstErr
}
