package scratch

// mount_detach.go — the ACCOUNTED-FOR reclaim path for a scratch subtree pinned
// by a DEAD FUSE mount.
//
// THE DEFECT (measured on this host, 2026-10-15). The sweep selects a stale,
// unheld queue child, calls RemoveAll, and gets
//
//	openfdat /home/nick/workspace/gopi-test-scratch/queue/queue-replay-4007787/runtime/gvfs: transport endpoint is not connected
//
// The path IS the mount point of a `fuse.gvfsd-fuse` filesystem whose daemon
// (gvfsd-fuse) is GONE: the kernel keeps the mount-table entry, every path into
// it answers ENOTCONN (measured: `stat -c %F` and `ls` on the three reported
// paths both print "Transport endpoint is not connected" while
// /proc/self/mounts still carries the line). The DIRECTORY ITSELF cannot be
// removed at all while the entry stands, so the same three paths were reported
// by every tick and reclaimed by none — Rule 34/35 in its pure form: residue
// with no reclaim path.
//
// The permission repair next door (removeCollectable's grantOwnerAccess) cannot
// touch this: it is not a mode problem, and a mode repair does not make a
// dangling FUSE mount deletable.
//
// WHO PINNED THE TREE — and why the mount is IN the swept subtree. The producer
// was `tools/termemu/termemu.py` (deleted in 971400a72, 2026-10-03): its
// private session bus exported `XDG_RUNTIME_DIR=<scratch>/runtime`
// (termemu.py:262 `env = dict(os.environ, XDG_RUNTIME_DIR=rt)` with
// `rt = os.path.join(scratch, "runtime")`, termemu.py:252) and then exported the
// same value into its own process environment (termemu.py:281). GLib's
// gvfs module mounts its FUSE filesystem at `$XDG_RUNTIME_DIR/gvfs` — so the
// mount point lands one level inside the scratch queue child the age GC owns.
// MEASURED, not inferred: `/proc/<pid>/environ` of the two still-LIVE
// gvfsd-fuse daemons carries exactly
//
//	XDG_RUNTIME_DIR=/home/nick/workspace/gopi-test-scratch/queue/queue-replay-1080449/runtime
//
// with the pids' mount targets matching (1080553, 2438498). The producers of the
// three DEAD ones are the same pipeline (the queue children are named
// `queue-replay-*`, and the runtime dir's own mtime is 2026-10-02/03 — the two
// days that pipeline ran); their daemons are gone, which is why their mounts are
// dangling. Termemu.py is deleted, so no producer writes that variable today;
// the residue it left is what this file reclaims.
//
// THE THREE DECISIONS THIS ARM MAKES, and the measurement behind each:
//
//  1. ONLY `fuse.gvfsd-fuse`, and only INSIDE the entry being collected. A
//     scratch sweep is not a general unmounter: another fuse type (a user's
//     sshfs, an rclone mount, a gvfs `portal`) is not this system's to detach.
//  2. ONLY a mount whose daemon is MEASURABLY GONE (`stat` answers ENOTCONN).
//     A live mount is left alone and REPORTED: something is still using it, and
//     it is not residue. Any other probe error is UNMEASURABLE, so nothing is
//     detached and the entry is kept — the same "cannot measure = keep" contract
//     the rest of the sweep obeys (Rule 15).
//  3. The detach is `fusermount3 -u`, lazily (`-uz`) as the second try — never
//     `sudo`, never `umount` by a privileged path (Rule 25: a privilege is not
//     the remedy for a cgroup/mount problem, and asking for one is the bug). If
//     neither tool is on PATH the arm reports that BY NAME and the entry stays:
//     an operator in a terminal may detach it by hand, and the report says so.
//
// It never fsyncs, never sleeps, never loops: one bounded mount-table read, one
// stat per mount under the entry, one detach command per dead mount with a hard
// timeout. A failed detachment leaves the entry in place with the reason printed
// — the sweep's own "GC could not remove" line then carries a cause instead of a
// bare ENOTCONN.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// SelfMountsPath is the mount table this arm reads. It is /proc/self/mounts
	// (the classic five-column table) rather than /proc/self/mountinfo: this arm
	// needs exactly two fields per mount — the target and the filesystem type —
	// and the classic table carries both with no escaping passthrough.
	SelfMountsPath = "/proc/self/mounts"

	// GvfsFuseFSType is the ONE filesystem type this arm will detach, and the
	// reason it is a constant: it is compared against a field of the kernel's own
	// table, so a typo would silently disarm the whole reclaim path (Rule 1b).
	GvfsFuseFSType = "fuse.gvfsd-fuse"

	// selfMountsReadLimit bounds the mount table read. A host with more mounts
	// than this has a mount problem of its own; the read is refused rather than
	// truncated, because a partial table is a "cannot measure" answer.
	selfMountsReadLimit = 1 << 20

	// The classic mount table's columns: "device target fstype options freq pass".
	mountsFieldIndexTarget     = 1
	mountsFieldIndexFSType     = 2
	mountsFieldCountMin        = 3
	mountsFieldSeparator       = " "
	mountsTableLineSeparator   = "\n"
	mountsTableCommentPrefix   = "#"
	selfMountsOctalEscapeWidth = 4
	selfMountsOctalBase        = 8
	selfMountsEscapeBackslash  = `\`
	selfMountsEscapeSpace      = `\040`
	selfMountsEscapeTab        = `\011`
	selfMountsEscapeNewline    = `\012`

	// fuseUnmountTool3 / fuseUnmountTool are the unprivileged FUSE detach
	// programs, tried in this order. Both are set-user-id helpers owned by the
	// fuse package; neither needs a privilege of ours (Rule 25).
	fuseUnmountTool3 = "fusermount3"
	fuseUnmountTool  = "fusermount"
	// fuseUnmountDetachFlag is the ordinary detach, fuseUnmountLazyFlag the lazy
	// one. The lazy spelling is the SECOND attempt only: on a daemon that is
	// already gone the ordinary detach usually works, and when it does not the
	// lazy form is what the fuse tooling itself prescribes for a disconnected
	// mount.
	fuseUnmountDetachFlag = "-u"
	fuseUnmountLazyFlag   = "-uz"

	// mountDetachTimeout bounds ONE detach command. A detach that would hang is
	// a detach this sweep does not wait for (the sweep is a tick, not a repair
	// tool), so the context kills it and the failure is reported.
	mountDetachTimeout = 5 * time.Second

	// maxMountsPerEntry bounds how many mounts one collected entry may carry.
	// Reaching it is reported, not silently honoured: an entry with more dead
	// mounts than this is a shape nothing else in the tree produces.
	maxMountsPerEntry = 16
)

// fuseUnmountTools is the ONE ordered list of detach programs (Rule 1: the
// spellings live here, and fuseUnmountToolPath walks it).
var fuseUnmountTools = []string{fuseUnmountTool3, fuseUnmountTool}

// scratchMountTable / scratchMountProbe / scratchMountDetach are the three
// seams this arm's decisions go through, and they exist because a REAL dangling
// FUSE mount cannot be produced inside a test: mounting is a kernel/filesystem
// privilege-changing act that the test suite must never perform on a shared host
// (Rule 22's neighbourhood, and the reason the epic rules out a real
// mount/umount). Every measurement that decides whether something is detached is
// therefore a function value the test can supply, and production supplies the
// real one.
var (
	// scratchMountTable reads the mount table (production: readSelfMounts).
	scratchMountTable = readSelfMounts
	// scratchMountProbe reports whether one mount point's filesystem is
	// MEASURABLY dead (production: probeMountPoint).
	scratchMountProbe = probeMountPoint
	// scratchMountDetach detaches one mount point (production: detachFuseMount).
	scratchMountDetach = detachFuseMount
)

// mountPoint is one row of the mount table this arm reads: the target (the
// directory the filesystem is mounted ON) and its type. The device is kept for
// the report — an operator reading a detach line wants to know who owned it.
type mountPoint struct {
	Device string
	Target string
	FSType string
}

// readSelfMounts reads and parses /proc/self/mounts. A read that fails or is
// over the limit is an error, never an empty table: "no mounts" and "could not
// measure" must not look alike (Rule 15).
func readSelfMounts() ([]mountPoint, error) {
	data, err := os.ReadFile(SelfMountsPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", SelfMountsPath, err)
	}
	if len(data) > selfMountsReadLimit {
		return nil, fmt.Errorf("%s holds %d bytes, over the %d-byte limit — refusing to measure a partial table", SelfMountsPath, len(data), selfMountsReadLimit)
	}
	return parseSelfMounts(string(data)), nil
}

// parseSelfMounts turns the classic table's text into rows. It is a pure
// function so the real lines this host produced can be the test's fixture (the
// evidence for the defect is a mount-table line, and a parser that cannot read
// that exact line would report "no mounts" for the very case this file exists
// for).
//
// Spaces inside a field arrive escaped (\040 and friends) — the unescaping is
// what lets a target with a space in its path be compared at all.
func parseSelfMounts(data string) []mountPoint {
	var out []mountPoint
	for _, line := range strings.Split(data, mountsTableLineSeparator) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, mountsTableCommentPrefix) {
			continue
		}
		fields := strings.Split(line, mountsFieldSeparator)
		if len(fields) < mountsFieldCountMin {
			continue
		}
		out = append(out, mountPoint{
			Device: unescapeMountField(fields[0]),
			Target: unescapeMountField(fields[mountsFieldIndexTarget]),
			FSType: unescapeMountField(fields[mountsFieldIndexFSType]),
		})
	}
	return out
}

// unescapeMountField decodes the octal escapes the kernel writes into a mount
// table field, so a target containing a space compares equal to the path the
// caller holds.
func unescapeMountField(field string) string {
	field = strings.ReplaceAll(field, selfMountsEscapeSpace, mountsFieldSeparator)
	field = strings.ReplaceAll(field, selfMountsEscapeTab, "\t")
	field = strings.ReplaceAll(field, selfMountsEscapeNewline, "\n")
	if strings.Contains(field, selfMountsEscapeBackslash) {
		// A backslash that is not one of the three escapes above: decode the
		// generic octal form rather than leave the table's spelling in a path.
		field = decodeOctalEscapes(field)
	}
	return field
}

// decodeOctalEscapes rewrites every `\NNN` in s as the byte it names. Anything
// that does not spell a complete octal escape is left as written — a mount
// target is never guessed at.
func decodeOctalEscapes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != selfMountsEscapeBackslash[0] || i+selfMountsOctalEscapeWidth > len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}
		var v int
		ok := true
		for _, c := range s[i+1 : i+selfMountsOctalEscapeWidth] {
			if c < '0' || c > '7' {
				ok = false
				break
			}
			v = v*selfMountsOctalBase + int(c-'0')
		}
		if !ok {
			b.WriteByte(s[i])
			i++
			continue
		}
		b.WriteByte(byte(v))
		i += selfMountsOctalEscapeWidth
	}
	return b.String()
}

// mountPointsUnder returns the rows whose target IS root or lies under it. The
// comparison is on the resolved spelling plus a separator, so `/a/bc` is never
// read as being under `/a/b`.
func mountPointsUnder(mounts []mountPoint, root string) []mountPoint {
	var out []mountPoint
	prefix := strings.TrimSuffix(root, string(os.PathSeparator)) + string(os.PathSeparator)
	for _, m := range mounts {
		if m.Target == root || strings.HasPrefix(m.Target, prefix) {
			out = append(out, m)
		}
	}
	return out
}

// probeMountPoint reports whether the filesystem mounted at target is
// MEASURABLY DEAD.
//
// THE THREE ANSWERS, and why they are three and not two:
//
//	reachable (no error)      → a LIVE mount: not residue, never detached here.
//	ENOTCONN                  → the daemon is gone (the measured defect); this is
//	                            the ONLY answer that authorises a detach.
//	anything else             → UNMEASURABLE: the caller keeps the entry and
//	                            reports the reason. "Could not tell" must never
//	                            read as "dead", or the sweep would detach a
//	                            mount it never measured.
func probeMountPoint(target string) (bool, error) {
	_, err := os.Stat(target)
	if err == nil {
		return false, nil
	}
	if errors.Is(err, syscall.ENOTCONN) {
		return true, nil
	}
	return false, fmt.Errorf("stat %s: %w", target, err)
}

// detachFuseMount detaches one dead FUSE mount point, unprivileged.
//
// The tool is resolved by name from fuseUnmountTools; if neither is on PATH the
// refusal is returned WITH the operator's route (a terminal, by hand) — never a
// privilege escalation (Rule 25). The ordinary detach runs first and the lazy
// form second, each bounded by mountDetachTimeout.
func detachFuseMount(target string) error {
	tool, err := fuseUnmountToolPath()
	if err != nil {
		return err
	}
	eager := runFuseUnmount(tool, fuseUnmountDetachFlag, target)
	if eager == nil {
		return nil
	}
	lazy := runFuseUnmount(tool, fuseUnmountLazyFlag, target)
	if lazy == nil {
		return nil
	}
	return fmt.Errorf("%s could not detach %s: the lazy form failed: %w (the ordinary %s had already failed: %s)",
		tool, target, lazy, fuseUnmountDetachFlag, eager.Error())
}

// fuseUnmountToolPath resolves the first detach program present on PATH.
func fuseUnmountToolPath() (string, error) {
	for _, name := range fuseUnmountTools {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("neither %s nor %s is on PATH: gopi cannot detach a dead FUSE mount by itself (and a privilege is not the remedy — Rule 25); an operator in a terminal can run `%s %s <mount-point>` by hand, after which the next sweep collects the entry",
		fuseUnmountTool3, fuseUnmountTool, fuseUnmountTool3, fuseUnmountDetachFlag)
}

// runFuseUnmount runs ONE detach attempt with a hard timeout and returns the
// failure with the tool's own output attached — the sweep's report is the only
// place an operator can see WHY a detach was refused.
func runFuseUnmount(tool, flag, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), mountDetachTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, flag, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s %s: %w (%s)", tool, flag, target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// releaseStaleGvfsMounts is the arm itself: measure the mounts inside root,
// detach the gvfs-owned ones whose daemon is measurably gone, and report every
// other decision. It returns how many mounts it detached and the FIRST failure
// it met (a later mount is still attempted — one unresolvable mount must not
// strand its siblings, Rule 15b).
//
// A measurement that cannot be taken is an error, never a silent zero: the
// caller must be able to say "could not measure" instead of "nothing there".
func releaseStaleGvfsMounts(root string) (int, error) {
	mounts, err := scratchMountTable()
	if err != nil {
		return 0, fmt.Errorf("measuring the mounts under %s: %w", root, err)
	}
	under := mountPointsUnder(mounts, root)
	if len(under) > maxMountsPerEntry {
		return 0, fmt.Errorf("%s carries %d mounts, over the %d this arm handles — refusing to act on a partial view", root, len(under), maxMountsPerEntry)
	}
	detached := 0
	var firstErr error
	record := func(e error) {
		if firstErr == nil {
			firstErr = e
		}
	}
	for _, m := range under {
		if m.FSType != GvfsFuseFSType {
			fmt.Fprintf(os.Stderr, "scratch: keeping the %s mount at %s (under %s): only %s is this sweep's to detach\n",
				m.FSType, m.Target, root, GvfsFuseFSType)
			continue
		}
		dead, perr := scratchMountProbe(m.Target)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "scratch: keeping the mount at %s (under %s): could not measure whether its daemon is gone: %v\n", m.Target, root, perr)
			record(fmt.Errorf("probe %s: %w", m.Target, perr))
			continue
		}
		if !dead {
			fmt.Fprintf(os.Stderr, "scratch: keeping the LIVE %s mount at %s (under %s): its daemon still answers\n", m.FSType, m.Target, root)
			continue
		}
		if derr := scratchMountDetach(m.Target); derr != nil {
			fmt.Fprintf(os.Stderr, "scratch: could not detach the dead %s mount at %s (under %s): %v\n", m.FSType, m.Target, root, derr)
			record(fmt.Errorf("detach %s: %w", m.Target, derr))
			continue
		}
		fmt.Fprintf(os.Stderr, "scratch: detached the dead %s mount at %s (under %s): the entry can now be collected\n", m.FSType, m.Target, root)
		detached++
	}
	return detached, firstErr
}

// selfMountTargetFor is the test-facing helper that names the mount target a
// path resolves to, or "" when the path is not a mount point itself. It exists
// so a caller (and a test) can ask the SAME parser the sweep uses instead of
// re-spelling the table's format.
func selfMountTargetFor(path string) (mountPoint, bool) {
	mounts, err := scratchMountTable()
	if err != nil {
		return mountPoint{}, false
	}
	clean := filepath.Clean(path)
	for _, m := range mounts {
		if filepath.Clean(m.Target) == clean {
			return m, true
		}
	}
	return mountPoint{}, false
}
