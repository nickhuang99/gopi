// Holder detection: the HARD RULE of the scratch tree — a directory a live
// process holds (as its working directory, as an open file descriptor, or as
// its registered scratch root) is NEVER deleted.
//
// WHY THIS LIVES IN internal/scratch (and not in internal/testutil, where it
// was written): the sweep needed a PRODUCTION consumer (its door today is the
// `--tick` arm of `gopi tool scratch-gc`; corrected 2026-10-30 — the
// worker/merger loops this used to name no longer call it), and production
// code cannot import a package that links the testing
// framework — internal/testutil imports "testing", so a production import
// drags the whole test framework into the shipped binary. internal/scratch is
// the leaf package (it imports nothing from the project), which is exactly
// Rule 14's shape: one definition, importable by everyone, no cycle. The age
// GC and its holder checks moved here unchanged (internal/testutil now
// delegates, so the suite-facing API and its tests are untouched).
//
// The two channels, and why BOTH exist:
//
//  1. /proc/<pid>/cwd + /proc/<pid>/fd — the kernel's own answer. Exact, but
//     readable only for descendants (ptrace_scope=1 on this shared host: 179
//     unreadable same-user entries were measured). An entry /proc will not let
//     us inspect is SKIPPED, never treated as a holder — treating "unreadable"
//     as "held" is what once froze the GC entirely.
//  2. the .holder registration — pid + kernel START TIME, written by the
//     scratch root's owner and read from /proc/<pid>/stat, which IS readable
//     for every process of the same user. This is the permission-free channel
//     that protects a long-lived suite (and a long-lived resident process)
//     exactly, and the start time makes pid reuse detectable.
//
// A check we cannot complete never authorises a delete (Rule 15: no silent
// guessing) — the caller keeps the directory, because a leaked directory is
// disk usage while a deleted live directory is data loss.
package scratch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// ProcRootDir / ProcCwdName / ProcFdName are the kernel interfaces the
	// holder check reads. They are constants, not literals, because the check
	// is a safety rule (never delete a live working directory) and a typo'd
	// path would silently disable it (Rule 1b). Each VALUE lives in
	// internal/procname — the leaf package every process-fact consumer can
	// import — and is referenced from here rather than re-typed (Rules 13/14).
	ProcRootDir = procname.ProcRootDefault
	ProcCwdName = procname.CwdName
	ProcFdName  = "fd"
	// ProcMapsName is the per-process memory map: the THIRD holder channel,
	// and the one a cwd+fd check cannot see. A process EXECUTING a binary out
	// of a directory has that path in /proc/<pid>/maps (the text segment) but
	// neither as its cwd nor necessarily as an open fd (measured on this host:
	// `gopi-merger merger --every 1h` ran its binary from
	// /dev/shm/gopi-test-scratch/.../merger-daemon-bin/). Deleting the
	// directory under such a process is the blast-radius incident this channel
	// exists to prevent.
	ProcMapsName = "maps"
	// ProcStatName is the process status file the kernel start time lives in.
	ProcStatName = procname.StatName
	// ProcExeName is the per-process executable link — the fourth spelling of
	// "this process runs code from there" (a mapped binary whose mapping was
	// dropped after exec still shows up here).
	ProcExeName = "exe"
	// ProcMapsReadLimit caps how much of one /proc/<pid>/maps file the holder
	// check reads. A process with more mapping text than this is pathological;
	// reading it would cost the sweep more than it protects, so the read is
	// bounded (and a read failure is a silent "no evidence from this channel",
	// never a veto — the cwd/fd/exe channels still stand).
	ProcMapsReadLimit = 4 << 20
	// procMapsPathFieldIndex is the 0-based field of a maps line that carries
	// the mapped path: "addr perms offset dev inode  path".
	procMapsPathFieldIndex = 5
	// deletedMappingSuffix is what the kernel appends to the path of a file
	// that was unlinked while still mapped.
	deletedMappingSuffix = " (deleted)"

	// HolderFileName is the liveness registration inside a scratch root: the
	// pid and the kernel start time of the process that owns the directory.
	HolderFileName = ".holder"
	// HolderKeyPID / HolderKeyStart name the registration fields.
	HolderKeyPID   = "pid"
	HolderKeyStart = "start"
)

// DirHolder returns the PID of a live process holding dir as its working
// directory or as one of its open file descriptors ("" = nobody). It is the
// public face of the hard rule; see dirHolder for the guarantees.
func DirHolder(dir string) (string, error) { return dirHolder(dir) }

// dirHolder scans /proc/<pid>/cwd and /proc/<pid>/fd and returns the first live
// process whose working directory, or one of whose open file descriptors, is
// inside dir (or is dir itself). The caller's own process is INCLUDED.
//
// Guarantees (each one is the reason the check is written this way):
//
//   - It compares the RESOLVED link target per component prefix, so a process
//     in dir/sub/x counts as a holder of dir — scratch usage is nested, not
//     flat.
//   - A vanished process (readlink → ENOENT) is skipped.
//   - A process /proc will not let us inspect (EACCES) is skipped, not reported
//     as an error. Such a process can never veto a sweep, or a shared host
//     would freeze the whole GC — which is exactly what happened when this rule
//     was "unreadable = error" (179 unreadable same-user entries on this host,
//     GC removing nothing).
//   - The scan is bounded by the live process table, which is the thing it must
//     be exact about.
func dirHolder(dir string) (string, error) {
	entries, err := os.ReadDir(ProcRootDir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", ProcRootDir, err)
	}
	targets := HolderTargets(dir)
	for _, entry := range entries {
		pid := entry.Name()
		if !entry.IsDir() || !IsPID(pid) {
			continue
		}
		for _, name := range []string{ProcCwdName, ProcFdName, ProcExeName} {
			for _, target := range targets {
				held, err := procEntryHolds(filepath.Join(ProcRootDir, pid, name), target)
				if err != nil {
					continue // not observable: cannot veto (see the doc above)
				}
				if held {
					return pid, nil
				}
			}
		}
		for _, target := range targets {
			if procMapsHolds(filepath.Join(ProcRootDir, pid, ProcMapsName), target) {
				return pid, nil
			}
		}
	}
	return "", nil
}

// LiveHolders collects, in ONE /proc pass, every path a live process holds as
// its working directory or as an open file descriptor. It is the batch form of
// DirHolder — same policy (a process /proc will not let us inspect is skipped,
// never treated as a holder), same guarantee (targets are compared resolved).
//
// The batch form exists because a sweep asks the holder question about every
// candidate: a per-entry scrub of /proc would cost a full process-table walk
// per directory (587 candidates on this host).
//
// DEPRECATED: returns the holder paths WITHOUT the scan's completeness, so a
// truncated scan is indistinguishable from a complete one (Rule 15). Callers
// should use ScanHolders / ScanHoldersWithin, which carry the truncation. It
// remains only so a non-destructive reader that never authorises a delete is
// not forced through the new shape.
func LiveHolders() []string { return ScanHolders().Paths }

// HolderScanDefaultBudget is the DEFAULT wall-clock bound of ONE holder scan.
// A bound exists because the scan's cost is not bounded by its inputs: it
// walks the WHOLE live process table (478 entries measured on this host) and
// reads each process's cwd, exe, every fd link and a whole maps file (up to
// ProcMapsReadLimit). Measured on this host 2026-10-12 at load average 1.9-7.9:
// 1.46-1.53s per call and 56,257 paths per call — the last segment of
// cmd/agent's package budget, and the shape that burned a 10m0s timeout
// (panic: test timed out after 10m0s, stack LiveHolders -> BaseSuiteRunReport).
//
// Margin: a NORMAL scan here is ~1.5s and 8s is a host roughly 5x more loaded
// than this one, while a scan only becomes pathological if the process table
// or a maps file does — so the bound sits ABOVE every load this host has been
// measured under, and it NEVER truncates a healthy scan. Measured: with the
// bound in place the scan took 1.353-1.435s and truncated=false on this host,
// including at load average 7.92. The truncation arm itself is pinned by a test
// that drives an ALREADY-EXPIRED deadline, never by manufactured load
// (Rule 27).
const HolderScanDefaultBudget = 8 * time.Second

// HolderScanDeadlineEnvName is the OPERATOR's knob: a positive whole number of
// SECONDS. "0" (or any non-positive value) disables the bound entirely — the
// unbounded form a diagnostic comparison needs, spelled as a deliberate
// choice rather than as a missing limit.
const HolderScanDeadlineEnvName = "GOPI_HOLDER_SCAN_DEADLINE_SEC"

// HolderScanDeadline reads HolderScanDeadlineEnvName — the ONE reader of that
// variable (Rule 2/21). It returns the bound to use for an ordinary scan:
// HolderScanDefaultBudget when nothing is configured, the configured duration
// when a positive number is set, and 0 (no bound) when the operator asks for
// the unbounded form.
func HolderScanDeadline() time.Duration {
	v := strings.TrimSpace(os.Getenv(HolderScanDeadlineEnvName))
	if v == "" {
		return HolderScanDefaultBudget
	}
	secs, err := strconv.Atoi(v)
	if err != nil {
		// An unparsable value is NOT silently treated as "no bound": the
		// operator asked for something and did not get it, and a silent fall
		// back to unbounded is exactly the defect this knob exists to close.
		fmt.Fprintf(os.Stderr, "scratch: %s=%q is not a whole number of seconds — using the default %s\n",
			HolderScanDeadlineEnvName, v, HolderScanDefaultBudget)
		return HolderScanDefaultBudget
	}
	if secs <= 0 {
		return 0 // explicitly unbounded
	}
	return time.Duration(secs) * time.Second
}

// HolderScan is the result of ONE /proc holder pass: the paths a live process
// was SEEN to hold, whether the pass finished, and what it cost.
//
// The type is the carrier of Rule 15 for this scan. A holder answer has two
// parts that a destructive caller must not conflate: "the paths I saw" and
// "whether I stopped before the end". A truncated pass has NOT looked at part
// of the process table, so "no holder found" is not evidence that nobody is
// holding — it is MISSING evidence, and a caller that deletes on missing
// evidence is deleting a directory it may not have looked at.
type HolderScan struct {
	// Paths is every holder path observed. Partial when Truncated.
	Paths []string
	// Truncated reports that the pass stopped at its bound with processes (or
	// fd entries) left unvisited. A caller MUST treat "not held" as unknown.
	Truncated bool
	// Elapsed is the measured wall time of the pass. Kept so a caller can
	// report WHAT the scan cost rather than assert a number it did not see.
	Elapsed time.Duration
}

// Complete reports whether the pass looked at every process it intended to.
// It is the readable face of !Truncated, so a caller's condition reads as the
// fact it is testing rather than as a double negative.
func (s HolderScan) Complete() bool { return !s.Truncated }

// HeldByProcesses reports whether any observed holder path lies inside dir.
// A truncated scan cannot answer this: a complete scan sees EVERY live holder,
// so "none inside dir" IS evidence nobody holds it; a partial one has not
// looked, so the answer is UNKNOWN and this method refuses to collapse it into
// false (Rule 15 — an unmeasured fact must never render as a measured
// negative). It returns ok=false in that case, and the caller decides.
func (s HolderScan) HeldByProcesses(dir string) (held, ok bool) {
	if s.Truncated {
		return false, false // unknown, NOT "not held"
	}
	return pathHeldByProcesses(dir, s.Paths), true
}

// ScanHolders is the ONE /proc holder pass under a wall-clock bound, reading
// its bound from HolderScanDeadline (the single env reader). Every production
// caller takes its scan from here so there stays exactly ONE liveness
// implementation (memory #2632: a second, drifting probe is how a live
// worktree gets deleted).
func ScanHolders() HolderScan { return ScanHoldersWithin(HolderScanDeadline()) }

// ScanHoldersWithin is ScanHolders with the bound supplied by the caller, so a
// probe or a test can drive a specific deadline without touching the
// environment. budget > 0 bounds the pass; budget == 0 means NO bound, which
// is the deliberate diagnostic form, never a default.
//
// WHAT IS CHECKED AND WHAT IS NOT, stated exactly because the difference is
// the whole point of this function:
//
//   - Before each PID is opened, the clock is compared against the deadline.
//     Past it, the pass STOPS and sets Truncated: the unvisited tail of the
//     process table is unmeasured, and a caller that deletes on this scan must
//     keep what it could not check.
//   - The truncating READ is NOT abandoned mid-flight. A cwd/exe/fd readlink or
//     a maps read that is already under way runs to completion, because an
//     abandoned read is not evidence of anything.
//   - ProcMapsReadLimit still bounds one maps file. That per-file cap is NOT a
//     truncation signal and does not set Truncated: a process with a larger
//     maps file than the cap has some of its mappings unread, so it is
//     reported by neither HeldByProcesses nor the disclosure — the fail-CLOSED
//     direction for nothing, so deleting on it cannot lose a live directory,
//     only a directory whose sole holder evidence lives past the cap.
//   - An error (no /proc at all) is NOT truncation: it is measured in the
//     caller's own channels (ProcReadable), and it leaves Truncated false.
func ScanHoldersWithin(budget time.Duration) HolderScan {
	start := time.Now()
	scan := HolderScan{}
	entries, err := os.ReadDir(ProcRootDir)
	if err != nil {
		scan.Elapsed = time.Since(start)
		return scan // no /proc: the holder channel degrades to the .holder file
	}
	var deadline time.Time
	if budget > 0 {
		deadline = start.Add(budget)
	}
	for _, entry := range entries {
		if !deadline.IsZero() && time.Now().After(deadline) {
			// FAIL-CLOSED, and the only place this flag is set: the tail of the
			// process table was never looked at.
			scan.Truncated = true
			break
		}
		pid := entry.Name()
		if !entry.IsDir() || !IsPID(pid) {
			continue
		}
		if target, err := os.Readlink(filepath.Join(ProcRootDir, pid, ProcCwdName)); err == nil {
			scan.Paths = append(scan.Paths, target)
		}
		if target, err := os.Readlink(filepath.Join(ProcRootDir, pid, ProcExeName)); err == nil {
			scan.Paths = append(scan.Paths, target)
		}
		scan.Paths = append(scan.Paths, procMapsPaths(filepath.Join(ProcRootDir, pid, ProcMapsName))...)
		fdDir := filepath.Join(ProcRootDir, pid, ProcFdName)
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // unreadable: cannot veto (see dirHolder's docs)
		}
		for _, fd := range fds {
			if target, err := os.Readlink(filepath.Join(fdDir, fd.Name())); err == nil {
				scan.Paths = append(scan.Paths, target)
			}
		}
	}
	scan.Elapsed = time.Since(start)
	return scan
}

// HolderVetoReason is the sentence a delete arm reports when the holder scan
// SAW a live process inside the path. It is a named constant because every
// arm must render the SAME sentence for the SAME fact (Rule 1b/13): a second
// spelling is how two arms start disagreeing about what they measured.
const HolderVetoReason = "held by a live process (cwd/fd/exe/maps)"

// HolderVeto answers the delete arms' ONE question — "is this path forbidden
// to collect?" — and returns the sentence that forbids it ("" = permitted).
// It is the exported face of the hard rule, so a caller outside this package
// never re-derives it from the path list and thus can never silently skip the
// truncation half.
//
// The two forbidding answers are DIFFERENT FACTS and are rendered as such:
// a measured holder (HolderVetoReason) versus a scan that stopped before the
// end of the process table (HolderScanWithheldReason). Both keep the path; only
// the first one lets a report say a process was seen.
func HolderVeto(dir string, scan HolderScan) string {
	live, ok := scan.HeldByProcesses(dir)
	if !ok {
		return HolderScanWithheldReason()
	}
	if live {
		return HolderVetoReason
	}
	return ""
}

// HolderScanWithheldReason is the sentence a REPORTING caller prints in place
// of a holder claim when the scan that produced its evidence was truncated. It
// exists because the two facts are different and only one of them is true: the
// arm kept the directory because it could NOT finish looking, not because it
// SAW a process inside. Printing the second sentence would be a fabricated
// observation (Rule 15), and an operator acting on it would go hunting for a
// process that was never seen.
func HolderScanWithheldReason() string {
	return "the holder scan was TRUNCATED at its wall-clock bound before it finished the process table — an unscanned process could hold it, so it is kept; nothing was observed holding it"
}

// HolderScanDisclosure renders WHAT the scan cost and whether it finished,
// for a caller's report line. An honest scan that completed is stated as such
// (a negative that WAS measured), never left to the reader's assumption.
func (s HolderScan) Disclosure() string {
	state := "complete"
	if s.Truncated {
		state = "TRUNCATED"
	}
	return fmt.Sprintf("%s scan of %s (looking for holders); %d path(s) seen%s",
		state, ProcRootDir, len(s.Paths), holderScanElapsedNote(s))
}

// holderScanElapsedNote keeps Disclosure's one numeric part in one place.
func holderScanElapsedNote(s HolderScan) string {
	return fmt.Sprintf(" in %s", s.Elapsed.Round(time.Millisecond))
}

// HolderNames names the live processes holding dir, as "pid N (<channel> <path>)",
// up to limit of them (limit <= 0 means HolderProbeMax). It is the EVIDENCE form
// of HeldByProcesses: the same /proc channels, the same skip policy, but the
// answer is who was seen rather than merely whether anybody was.
//
// A REPORTING helper only. It answers no question a destructive arm may act on —
// those ask HeldByProcesses/LiveHolders, so there stays exactly ONE liveness
// verdict in the system (memory #2632: a second, drifting probe is how a live
// worktree gets deleted). It exists because a render that says "a live process is
// inside" without naming it gives the operator nothing to verify.
func HolderNames(dir string, limit int) []string {
	if limit <= 0 {
		limit = HolderProbeMax
	}
	entries, err := os.ReadDir(ProcRootDir)
	if err != nil {
		return nil // no /proc: no name evidence from this channel
	}
	targets := HolderTargets(dir)
	var out []string
	for _, entry := range entries {
		pid := entry.Name()
		if !entry.IsDir() || !IsPID(pid) {
			continue
		}
		if path, channel, ok := processInside(pid, targets); ok {
			out = append(out, fmt.Sprintf("pid %s (%s %s)", pid, channel, path))
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// processInside answers whether ONE process holds any of targets, naming the
// FIRST channel that proved it. Channel order matches dirHolder's (cwd, fd, exe,
// then maps) so the two can never disagree about what a hold rests on.
func processInside(pid string, targets []string) (path, channel string, ok bool) {
	base := filepath.Join(ProcRootDir, pid)
	for _, name := range []string{ProcCwdName, ProcExeName} {
		if target, err := os.Readlink(filepath.Join(base, name)); err == nil && withinAny(target, targets) {
			return target, name, true
		}
	}
	fdDir := filepath.Join(base, ProcFdName)
	if fds, err := os.ReadDir(fdDir); err == nil { // unreadable: cannot report a hold
		for _, fd := range fds {
			if target, err := os.Readlink(filepath.Join(fdDir, fd.Name())); err == nil && withinAny(target, targets) {
				return target, ProcFdName, true
			}
		}
	}
	for _, mapped := range procMapsPaths(filepath.Join(base, ProcMapsName)) {
		if withinAny(mapped, targets) {
			return mapped, ProcMapsName, true
		}
	}
	return "", "", false
}

// withinAny reports whether path lies inside any of the target spellings.
func withinAny(path string, targets []string) bool {
	for _, t := range targets {
		if PathWithin(path, t) {
			return true
		}
	}
	return false
}

// HolderProbeMax bounds how many holders ONE directory's report names. A
// checkout held by more is pathological; by then the count is what matters.
// Bounded like every other read in this file, so one status render cannot become
// an unbounded walk.
const HolderProbeMax = 8

// ProcReadable reports whether the /proc holder channel can be read at all. It
// exists so a REPORTING caller can tell "the scan ran and found nobody" from
// "the scan could not run" — LiveHolders/HeldByProcesses collapse both into an
// empty/false answer, which is the fail-closed direction a destructive arm needs
// but the wrong sentence for an operator (Rule 15: an unmeasured fact must never
// render as a measured negative).
//
// It is NOT a liveness verdict and no arm may consult it: the arms keep asking
// HeldByProcesses (one verdict, one implementation).
func ProcReadable() bool {
	_, err := os.ReadDir(ProcRootDir)
	return err == nil
}

// pathHeldByProcesses reports whether any collected holder path lies inside
// dir. Both spellings of dir are compared (HolderTargets), because the kernel
// reports resolved paths while a scratch home may be reached through a symlink.
//
// It answers about PATHS ONLY and has no opinion on completeness — which is
// why it is unexported: a caller that can reach this without going through
// HolderScan.HeldByProcesses is a caller that was never asked whether the scan
// finished (Rule 15).
func pathHeldByProcesses(dir string, holders []string) bool {
	for _, target := range HolderTargets(dir) {
		for _, holder := range holders {
			if PathWithin(holder, target) {
				return true
			}
		}
	}
	return false
}

// HolderTargets lists the spellings a holder's /proc link may carry for dir.
// The kernel always reports a RESOLVED path, so a scratch home that reaches
// its tree through a symlink (the unprivileged setup the stopped
// scripts/setup-scratch-home.sh performed) would never match the literal path: both the
// literal and the resolved spelling are compared (a missing resolution is not
// an error — the literal spelling still stands).
func HolderTargets(dir string) []string {
	clean := filepath.Clean(dir)
	targets := []string{clean}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != clean {
		targets = append(targets, resolved)
	}
	return targets
}

// readProcFile reads at most limit bytes of a /proc file. It exists so a holder
// channel never reads an unbounded kernel pseudo-file: a process with a
// pathological maps file must not turn one sweep into an unbounded read.
func readProcFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, limit)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return buf[:n], nil
}

// procMapsHolds reports whether any mapped file of a process lies inside
// target. It is the maps half of the holder rule; a maps file that cannot be
// read yields false (the cwd/fd/exe channels still stand) — never an error that
// could veto a sweep, and never "held" by default.
func procMapsHolds(mapsPath, target string) bool {
	for _, path := range procMapsPaths(mapsPath) {
		if PathWithin(path, target) {
			return true
		}
	}
	return false
}

// procMapsPaths lists the file-backed mappings of one process, in the order
// /proc prints them (the first hit names the process that maps the file, which
// is the pid the caller reports).
//
// The mapped-path field can carry decorations a naive comparison gets wrong:
//
//   - " (deleted)" for a file unlinked while mapped (and a literal path may
//     itself end in " (deleted)", so the suffix is stripped, never searched);
//   - a path spelled with escaped newlines when the file name contains one,
//     which is left alone rather than mis-decoded;
//   - no path at all for anonymous/device mappings, which are skipped.
func procMapsPaths(mapsPath string) []string {
	raw, err := readProcFile(mapsPath, ProcMapsReadLimit)
	if err != nil {
		return nil // unreadable (or gone): no mapping evidence from this channel
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) <= procMapsPathFieldIndex {
			continue // anonymous mapping: no path to compare
		}
		path := strings.TrimSuffix(fields[procMapsPathFieldIndex], deletedMappingSuffix)
		if path == "" || path[0] != filepath.Separator {
			continue // not an absolute file-backed mapping
		}
		out = append(out, path)
	}
	return out
}

// procEntryHolds answers whether one /proc/<pid>/<cwd|fd> entry holds target.
func procEntryHolds(linkPath, target string) (bool, error) {
	resolved, err := os.Readlink(linkPath)
	if err != nil {
		// The process is gone (or the fd closed between readdir and readlink):
		// not a holder, and not an error worth failing the sweep over. Any
		// OTHER error means the check could not be made.
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return PathWithin(resolved, target), nil
}

// PathWithin reports whether path is target itself or nested under it. It
// compares cleaned paths component-wise — a prefix STRING match would call
// "/scratch-other" a child of "/scratch".
func PathWithin(path, target string) bool {
	if target == "" || target == "." || target == string(filepath.Separator) {
		return false // never claim the whole filesystem as "held"
	}
	path = filepath.Clean(path)
	if path == target {
		return true
	}
	return strings.HasPrefix(path, target+string(filepath.Separator))
}

// IsPID reports whether a /proc entry name is a numeric process id.
func IsPID(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// HolderAlive reports whether the process that registered dir as its scratch
// root (the .holder file) is still running.
//
// This is the permission-free half of the hard rule: /proc/<pid>/cwd and
// /proc/<pid>/fd links are readable only for descendants (ptrace_scope=1 on a
// shared host), but /proc/<pid>/stat is readable for every process of the same
// user — so an owner REGISTERS itself (pid + kernel start time, which makes pid
// reuse detectable) and the sweep can ask a question the kernel answers without
// permission games. Same-user processes that never registered are covered by
// DirHolder where observable, and by the age rule everywhere else.
func HolderAlive(dir string) (bool, string) {
	raw, err := os.ReadFile(filepath.Join(dir, HolderFileName))
	if err != nil {
		return false, "" // unregistered: nothing to say
	}
	rec, err := ParseOwnerRecord(string(raw))
	if err != nil {
		return false, fmt.Sprintf("holder file in %s is unreadable: %v", dir, err)
	}
	verdict := OwnerVerdictFor(dir)
	if verdict.Outcome != procname.OutcomeLive {
		if verdict.Outcome == procname.OutcomeUnmeasurable {
			// The kernel would not answer (no procfs, an unparsable stat line, an
			// unreadable cmdline). Reported AS SUCH: an unmeasured owner is not a
			// dead one, and a caller that keeps the directory on this answer is
			// reading it correctly.
			return false, verdict.Reason
		}
		return false, fmt.Sprintf("pid %d was reassigned or is gone (recorded start ticks %d)", rec.Pid, rec.Start)
	}
	return true, fmt.Sprintf("pid %d registered %s as its scratch root", rec.Pid, dir)
}

// ParseHolder reads the "pid=<n>\nstart=<n>" registration — the incarnation-only
// face of ParseOwnerRecord (the ONE reader of the .holder format), kept because a
// caller that needs only the pid and the incarnation must not be forced through
// the command-line field.
func ParseHolder(raw string) (pid int, start uint64, err error) {
	rec, err := ParseOwnerRecord(raw)
	if err != nil {
		return 0, 0, err
	}
	return rec.Pid, rec.Start, nil
}

// RegisterHolder writes the liveness registration into a scratch root so the
// sweep can protect it without /proc permission games. Failure is reported, never
// fatal: the registration is an extra guarantee, not the only one (Rule 15b).
//
// It writes the CURRENT process's identity (pid + kernel start ticks + command
// line) through WriteOwnerRecord, the one writer of the format — see
// owner_record.go for what the record is for.
func RegisterHolder(root string) {
	if err := WriteOwnerRecord(root, os.Getpid()); err != nil {
		fmt.Fprintf(os.Stderr, "scratch: holder registration in %s: %v\n", root, err)
	}
}

// ProcStartTime reads the kernel start time (field 22 of /proc/<pid>/stat, in
// clock ticks since boot) of a live process.
func ProcStartTime(pid int) (uint64, error) {
	raw, err := os.ReadFile(filepath.Join(ProcRootDir, strconv.Itoa(pid), ProcStatName))
	if err != nil {
		return 0, err
	}
	return ParseProcStartTime(string(raw))
}

// ParseProcStartTime extracts field 22 from /proc/<pid>/stat. It delegates to
// internal/procname's StatStartTime — the ONE implementation of "starttime is
// field 22, counted from after the LAST ')'" (Rules 13/14), the hole the two
// copies had already started to drift through: the parsing trap (a command name
// carrying spaces or parentheses) can only be fixed in one place if there IS
// one place. The (0, error) shape is unchanged, so a caller reporting the
// failure never reads a fabricated zero as a value.
func ParseProcStartTime(raw string) (uint64, error) {
	return procname.StatStartTime(raw)
}
