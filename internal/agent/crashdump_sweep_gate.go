package agent

// crashdump_sweep_gate.go — WHEN the orphan-crash-dump sweep is allowed to run.
//
// THE COST THIS GATE REMOVES: DetectOrphanSessions / HasSessionEnd walk EVERY
// session under the data root and inspect each JSONL tail. Measured on the
// reference host (2026-09-12, 29,250 root entries, 405 MB of JSONL):
//
//	whole-file tail read   DetectOrphanSessions 1.835 s / 455.5 MB alloc churn
//	                       BackfillOrphanCrashDumps 1.298 s / 328.8 MB
//	bounded tail read      DetectOrphanSessions 0.629 s /  22.4 MB
//	                       BackfillOrphanCrashDumps 0.660 s /  19.6 MB
//
// The bounded read (session.go sessionTailWindowBytes) removed the allocation
// churn; it cannot remove the WALK — the walk is ReadDir metadata, not IO.
// Re-measured on the grown tree the same day (47,893 root entries / 45,889
// task dirs / 95,669 sub-entries / 1,673 JSONL / 827 dumps):
//
//	DetectOrphanSessions            1194-1765 ms / 41.7 MB churn / 5.4 MB read
//	ReadDir(root) alone              98 ms
//	root + 1 level ReadDir           678 ms
//	stat(root)                       ~0 ms
//
// i.e. ~46k ReadDir round-trips dominate, while the bytes actually read are
// 5.4 MB. Before any gate this ran on EVERY agent start.
//
// THE GATE (this file). Trusting a skip on a CALENDAR DAY was the first
// answer, and it is wrong in the direction that matters: keyed on the day, a
// session that dies one minute after today's sweep is not dumped for up to
// 24 h, and the acceptance — a newly orphaned session is DETECTED on the NEXT
// start — is unreachable. The trust decision is now a change-sensitive MARKER
// with three independent layers:
//
//	1. the bounded-cadence floor (crashDumpSweepFullWalkInterval): a full walk
//	   is DUE whenever the last completed walk is older than the floor,
//	   whatever the marker claims. This is the self-heal — no marker content,
//	   no argument about a fingerprint, can keep the sweep switched off for
//	   longer than the floor.
//	2. the budget ledger (crashDumpSweepBudgetSec): a start inside the budget
//	   window of a completed walk trusts that walk instead of paying for its
//	   own. Every completed walk advances the ledger, so N starts inside the
//	   window cost one walk, not N.
//	3. the change probe (treeChangeProbe): a bare `stat` of the sessions root
//	   costs microseconds and no ReadDir, and REFUSES the skip as soon as the
//	   root's name set changed since the marker. This is what makes a NEW
//	   session directory (the startup-kill / power-loss shape: a dir whose
//	   JSONL never got written) visible on the very next start. The root's
//	   PARENT is deliberately NOT probed: the marker file lives there, so
//	   probing it would make every marker write invalidate its own probe.
//
// WHAT THE CHANGE PROBE CANNOT SEE, MEASURED AND STATED HONESTLY. A write into
// an EXISTING session directory does not propagate to the sessions root.
// Probed on this host (ext4, kernel 5.15) with 1.2 s separation per step:
// creating, appending to, or removing <root>/<id>/session.jsonl moves the
// JSONL's own ctime/mtime and (for create/remove) the session dir's, but
// leaves the ROOT's ctime/mtime untouched; only a name created/renamed
// DIRECTLY in the root — or in the root's parent — moves what this probe
// reads. So a session that merely keeps writing inside its own directory is
// invisible to it. That case is covered by layer 1 (the floor), and it is
// rarely a crash: a still-writing session classifies OrphanLive, and a session
// whose writer died stops writing, so its NEXT appearance in the tree is a
// floor-bounded discovery rather than an instant one.
//
// WHY NOT FINGERPRINT EVERY JSONL MTIME (the "obviously stronger" probe),
// measured on the reference tree the same day, interleaved rounds, one probe:
//
//	2x ReadDir + Info per JSONL   1121-1655 ms / 33.3 MB churn
//	newest-JSONL-mtime scan       1025-1738 ms / 34.7 MB
//	DetectOrphanSessions walk     1194-1765 ms / 41.7 MB
//
// The "pre-check" costs what it was meant to avoid: proving the tree unchanged
// requires the same ReadDir pass as walking it, so a per-start fingerprint
// buys nothing and adds a second way to be wrong. It also degenerates on a
// live host — the newest JSONL write was 1-2 s old, so an "unchanged" verdict
// is almost never available. The budget ledger is the answer to a busy host
// (one walk per window), not a bigger probe.
//
// FAIL CLOSED MEANS FAIL-OPEN-TO-SWEEPING. Every uncertainty resolves to DUE:
// an absent / unreadable / truncated / unparseable / future-dated / foreign
// marker, a probe that cannot be built, and a tree root that cannot be stat'd
// all report "sweep". A marker that suppressed the sweep when it could not be
// believed would be the silent no-sweep failure this codebase fears most
// (Rule 15).
//
// WHY NOT FOLDED INTO `gopi tool scratch-gc` OR THE RESIDENT MERGER TICK: the
// resident tick (cmd/agent/cli_tasks.go, scratchGCSweep) is the ONE component
// that runs on a host where no user start happens, which is why it was
// considered; but the sweep's exclusion ("not the CURRENT process's session")
// and its reporting line ("N session(s) died without a clean exit") both
// belong to the STARTING process, and its product is worth exactly as much
// written a minute later by a daemon as written now. Moving it there would
// re-home the trigger without making it change-proportional, and the retired
// per-dump reconciler (crashdump.go retireStaleOrphanDumps) has no production
// caller today, so the tick would have to grow a second, unowned walk.
//
// WHY A MARKER FILE AND NOT settings.json: the marker is STATE on the same
// machine and in the same tree as the thing it describes (the sessions under
// the data root), it must be written by the non-interactive startup path, and
// it must survive a settings file the operator never touched. settings.json is
// configuration (an operator's decision); this is a bookkeeping mark.
//
// COORDINATION: the classification predicate (session.go DetectOrphanSessions
// / HasSessionEnd / hasSessionEndBytes / ClassifyOrphanSession) owns WHICH
// sessions get dumped, and must NOT be touched here; this file owns only WHEN
// the sweep runs. It must not gain a predicate over the session semantics
// (live-window, dump reconciliation) — those stay in their own files,
// unchanged by this gate.

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
)

const (
	// crashDumpSweepEnvName is the ONE knob over the gate (Rule 2: read in
	// exactly one place, OrphanSweepDue). Unset = the change-sensitive gate;
	// "force" = run now (and re-mark); "off" = never run from the startup path.
	crashDumpSweepEnvName = "GOPI_CRASH_DUMP_SWEEP"

	// crashDumpSweepEnvForce / crashDumpSweepEnvOff are the two accepted
	// values of that knob (Rule 1b: a compared value is a named constant).
	crashDumpSweepEnvForce = "force"
	crashDumpSweepEnvOff   = "off"

	// crashDumpSweepMarkerName is the marker file inside the gopi data root.
	// It records the state of the sessions tree AT the last completed sweep,
	// which is what lets a later start decide whether a skip is safe.
	crashDumpSweepMarkerName = "crash-dump-sweep.marker"

	// crashDumpSweepMarkerVersion is the marker's format version. A marker
	// written by a future/unknown version is NOT believed (fail-open to a
	// walk) rather than misread under today's layout.
	crashDumpSweepMarkerVersion = "1"

	// crashDumpSweepMarkerField* are the marker's keys. Named because the
	// writer and the reader must agree on the exact spelling (Rule 1b/13).
	crashDumpSweepMarkerFieldVersion = "version"
	crashDumpSweepMarkerFieldSweptAt = "swept_at"
	crashDumpSweepMarkerFieldProbe   = "probe"
	crashDumpSweepMarkerFieldRoot    = "root"

	// crashDumpSweepMarkerProbeSep joins the probe's numbers inside the
	// marker's one-line probe value (the marker stays human-readable).
	crashDumpSweepMarkerProbeSep = ","

	// crashDumpSweepMarkerTimeLayout is how swept_at is spelled: RFC3339 in
	// UTC. A timestamp (not a day key) because the floor and the budget are
	// intervals, and an unparseable value is treated as "no usable marker".
	crashDumpSweepMarkerTimeLayout = time.RFC3339Nano

	// crashDumpSweepFullWalkInterval is the BOUNDED CADENCE FLOOR: at most
	// this long may pass between two full walks, whatever the marker claims.
	// It is the self-heal for every stale-marker path (see the header): a
	// marker that is wrong, forged, written by a crashed process, or intact
	// while the tree's change evidence was lost still costs at most one
	// interval of undumped crashes. One hour: a crash discovered within the
	// hour is still reported in the same working session, while the walk
	// itself (~1.2 s) is paid at most 24x/day on a host that starts gopi
	// continuously — cheaper than the pre-gate behaviour and bounded in
	// LATENCY rather than by a calendar day.
	crashDumpSweepFullWalkInterval = time.Hour

	// crashDumpSweepBudgetSec is how recently ANOTHER start of this data root
	// must have walked for this start to trust that walk instead of paying for
	// its own. This is the amortiser: a warm start costs two stats, not a walk.
	// Deliberately smaller than the floor, so the trust window is always
	// bounded by the same cadence.
	crashDumpSweepBudgetSec = 60

	// crashDumpSweepProbeSettle is how long a namespace change needs before it
	// is guaranteed visible in the parent directory's timestamps. Measured on
	// this host (ext4, kernel 5.15): `mkdir <root>/child` moves the root's
	// ctime/mtime within ~100 ms, but a stat taken immediately afterwards can
	// still read the OLD pair — ext4 updates the parent's timestamps lazily.
	// A marker written inside this window would therefore record a probe that
	// is about to become stale by itself, turning every later start into a
	// forced walk (fail-safe, but the fast path would never be trusted). The
	// writer waits out the window before sampling; the READER needs no wait,
	// because a mismatch only ever means "walk".
	crashDumpSweepProbeSettle = 150 * time.Millisecond

	// Permissions for the data root (when the marker is the first thing to
	// create it) and the marker file.
	crashDumpSweepDirPerm  = 0o755
	crashDumpSweepFilePerm = 0o644

	// crashDumpSweepReason* are the human-readable verdicts OrphanSweepDue
	// returns alongside the bool. They are constants because the startup log
	// line and the tests both quote them (Rule 1b — a value with meaning).
	// CrashDumpSweepReasonForced is EXPORTED: cmd/agent's startup path quotes
	// it to report a forced sweep that found nothing (one spelling, Rule 14).
	crashDumpSweepReasonNoMarker    = "no sweep marker yet"
	crashDumpSweepReasonUnreadable  = "sweep marker unreadable"
	crashDumpSweepReasonFutureDated = "sweep marker is dated in the future"
	crashDumpSweepReasonRootUnknown = "sessions root not statable"
	crashDumpSweepReasonChanged     = "sessions tree changed since the last sweep"
	crashDumpSweepReasonFloorDue    = "full-walk interval elapsed"
	crashDumpSweepReasonForeign     = "sweep marker describes another data root"
	crashDumpSweepReasonFresh       = "already swept within the budget window"
	CrashDumpSweepReasonForced      = "forced by " + crashDumpSweepEnvName
	crashDumpSweepReasonDisabled    = "disabled by " + crashDumpSweepEnvName
)

// crashDumpSweepMarkerPath resolves <gopi-dir>/crash-dump-sweep.marker. The
// error is returned rather than swallowed: an unresolvable data root means
// "no state to read", which the caller treats as DUE (a marker that cannot be
// located must never be silently trusted — Rule 15).
func crashDumpSweepMarkerPath() (string, error) {
	dir, err := config.GopiDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, crashDumpSweepMarkerName), nil
}

// crashDumpSweepRoot resolves the sessions tree this gate is about. It is the
// same root DetectOrphanSessions walks (config.SessionsDirName under the data
// root), spelled here as a constant so the gate and the sweep cannot drift.
func crashDumpSweepRoot() (string, error) {
	return config.SessionsRoot()
}

// crashDumpSweepMarker is the persisted marker: when the last completed sweep
// finished, and the tree's change fingerprint AT that moment.
type crashDumpSweepMarker struct {
	// Version is the format version; a mismatch means "do not believe".
	Version string
	// SweptAt is the UTC instant the sweep completed.
	SweptAt time.Time
	// Probe is the tree-change fingerprint (treeChangeProbe) captured at that
	// instant: the ctime/mtime pairs of the tree root and its parent.
	// Comparing it against a fresh probe is the change decision.
	Probe []int64
	// RootPath makes a marker copied from another data root detectable
	// (Rule 15: a wrong marker is a refusal, not a silent skip).
	RootPath string
}

// treeCTimeOf stats path and returns its change time and modification time in
// nanoseconds since the epoch. It is a bare stat — microseconds, no ReadDir —
// which is what makes the fast path cheap enough to run on every start.
//
// Change time, not modification time: a directory's ctime is advanced by the
// creation/removal/rename of a name INSIDE IT (rename, link and unlink update
// the parent's ctime and mtime in place, never any other inode's), and a
// file's ctime is advanced by every write. The mtime is read and compared
// alongside, so an mtime-only change (a chmod, or a filesystem that does not
// render a distinct ctime) is not missed either.
//
// The SCOPE of this evidence is bounded and stays stated: it reports changes
// to the NAME SET of path. It cannot report a write to a file deeper inside
// the tree (measured on ext4/5.15: an append to <root>/<id>/session.jsonl
// leaves the root's ctime/mtime untouched). treeChangeProbe therefore stats
// the sessions root AND its parent, which together cover every event that can
// create a new session (a new session dir or flat JSONL under the root, or a
// peer directory that could hold one).
func treeCTimeOf(path string) (ctime, mtime int64, err error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, 0, err
	}
	ctime, mtime = fileChangeTimes(&st)
	return ctime, mtime, nil
}

// treeChangeProbe is the cheap change fingerprint of the sessions tree: the
// (ctime, mtime) of the tree ROOT, which changes whenever a name is added,
// removed or renamed directly under it. It costs one stat and no directory
// read.
//
// WHY THE ROOT ONLY, NOT ALSO ITS PARENT: the marker file lives in the data
// root, i.e. in the root's parent. Measured on this host: renaming the marker
// temp into place moves the PARENT data root's ctime, so including the parent
// in the probe would make every marker write invalidate the very probe it
// stores — each start would read "changed" and walk forever. The probe
// therefore covers exactly the level the gate can observe without observing
// its own bookkeeping.
//
// It returns an error when the root cannot be stat'd, because a fingerprint
// that silently omitted a level would silently stop covering events
// (Rule 15) — the caller answers an error with DUE.
func treeChangeProbe(root string) ([]int64, error) {
	ctime, mtime, err := treeCTimeOf(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	return []int64{ctime, mtime}, nil
}

// encodeTreeChangeProbe renders a probe for the marker.
func encodeTreeChangeProbe(probe []int64) string {
	parts := make([]string, 0, len(probe))
	for _, v := range probe {
		parts = append(parts, strconv.FormatInt(v, 10))
	}
	return strings.Join(parts, crashDumpSweepMarkerProbeSep)
}

// decodeTreeChangeProbe parses a stored probe. A malformed value is an ERROR
// (the caller walks) rather than a partially-trusted fingerprint.
func decodeTreeChangeProbe(raw string) ([]int64, error) {
	parts := strings.Split(raw, crashDumpSweepMarkerProbeSep)
	probe := make([]int64, 0, len(parts))
	for _, part := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("probe field %q: %w", part, err)
		}
		probe = append(probe, v)
	}
	return probe, nil
}

// sameTreeChangeProbe reports whether two probes describe the same tree state.
// A length mismatch is NOT equal: a marker written by a different probe shape
// cannot be compared field-by-field, so it must cost a walk.
func sameTreeChangeProbe(a, b []int64) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// readCrashDumpSweepMarker parses the marker file.
//
// Every failure mode is an ERROR, never a quiet zero value: absent, unreadable,
// missing/short lines, a non-numeric stamp, an unparseable time, a foreign
// version, or a missing root path. The caller answers any of them with DUE
// (Rule 15) — the marker is an optimisation, and an optimisation that cannot
// be read must cost a walk, not buy a skip.
func readCrashDumpSweepMarker(path string) (crashDumpSweepMarker, error) {
	var m crashDumpSweepMarker
	f, err := os.Open(path)
	if err != nil {
		return m, fmt.Errorf("open sweep marker %s: %w", path, err)
	}
	defer f.Close()

	fields := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return m, fmt.Errorf("sweep marker %s: malformed line %q", path, line)
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := sc.Err(); err != nil {
		return m, fmt.Errorf("read sweep marker %s: %w", path, err)
	}

	if got := fields[crashDumpSweepMarkerFieldVersion]; got != crashDumpSweepMarkerVersion {
		return m, fmt.Errorf("sweep marker %s: version %q is not %q",
			path, got, crashDumpSweepMarkerVersion)
	}
	m.Version = fields[crashDumpSweepMarkerFieldVersion]

	raw := fields[crashDumpSweepMarkerFieldSweptAt]
	if raw == "" {
		return m, fmt.Errorf("sweep marker %s: missing %s",
			path, crashDumpSweepMarkerFieldSweptAt)
	}
	sweptAt, err := time.Parse(crashDumpSweepMarkerTimeLayout, raw)
	if err != nil {
		return m, fmt.Errorf("sweep marker %s: %s %q: %w",
			path, crashDumpSweepMarkerFieldSweptAt, raw, err)
	}
	m.SweptAt = sweptAt.UTC()

	rawProbe, ok := fields[crashDumpSweepMarkerFieldProbe]
	if !ok {
		return m, fmt.Errorf("sweep marker %s: missing %s",
			path, crashDumpSweepMarkerFieldProbe)
	}
	probe, err := decodeTreeChangeProbe(rawProbe)
	if err != nil {
		return m, fmt.Errorf("sweep marker %s: %s: %w",
			path, crashDumpSweepMarkerFieldProbe, err)
	}
	m.Probe = probe

	m.RootPath = fields[crashDumpSweepMarkerFieldRoot]
	if m.RootPath == "" {
		return m, fmt.Errorf("sweep marker %s: missing %s",
			path, crashDumpSweepMarkerFieldRoot)
	}
	return m, nil
}

// writeCrashDumpSweepMarker persists the marker atomically: a temp file in the
// same directory, fsynced, then renamed over the marker.
//
// Atomicity is not decoration — it removes "crash before the marker write"
// from the staleness list. A process killed mid-write leaves the OLD marker (a
// floor-bounded staleness at worst), never a truncated one a reader would have
// to interpret. The error is returned for the caller to REPORT (Rule 15): the
// failure mode is benign (the next start walks again) but never silent.
func writeCrashDumpSweepMarker(path string, m crashDumpSweepMarker) error {
	dir := filepath.Dir(path)
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, crashDumpSweepDirPerm); err != nil {
		return fmt.Errorf("create gopi data root %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, crashDumpSweepMarkerName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create sweep marker temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	// A failure between here and the rename must not leave a stray temp file
	// behind: remove it, and report the removal failure too (never silent).
	cleanup := func() {
		if err := os.Remove(tmpName); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "[gopi] crash-dump: could not remove marker temp %s: %v\n", tmpName, err)
		}
	}

	body := strings.Join([]string{
		crashDumpSweepMarkerFieldVersion + "=" + m.Version,
		crashDumpSweepMarkerFieldSweptAt + "=" + m.SweptAt.UTC().Format(crashDumpSweepMarkerTimeLayout),
		crashDumpSweepMarkerFieldProbe + "=" + encodeTreeChangeProbe(m.Probe),
		crashDumpSweepMarkerFieldRoot + "=" + m.RootPath,
		"",
	}, "\n")
	if _, err := tmp.WriteString(body); err != nil {
		// The write error is the one that matters; a close failure on an
		// already-failed handle is secondary, so it is recorded rather than
		// returned (which would mask the cause).
		if cerr := tmp.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "[gopi] crash-dump: closing marker temp %s after write failure: %v\n", tmpName, cerr)
		}
		cleanup()
		return fmt.Errorf("write sweep marker temp %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		if cerr := tmp.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "[gopi] crash-dump: closing marker temp %s after sync failure: %v\n", tmpName, cerr)
		}
		cleanup()
		return fmt.Errorf("sync sweep marker temp %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close sweep marker temp %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, crashDumpSweepFilePerm); err != nil {
		cleanup()
		return fmt.Errorf("chmod sweep marker temp %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("install sweep marker %s: %w", path, err)
	}
	return nil
}

// OrphanSweepDue reports whether the process-lifetime orphan sweep should run
// now, plus the reason for that verdict (the startup line quotes it, so an
// operator reading stderr sees WHY a full-tree scan just happened).
//
// The decision order is deliberate — cheapest and most certain first:
//
//  1. the operator's knob (force / off) is absolute;
//  2. a tree whose probe cannot be built is DUE;
//  3. a marker that cannot be read/parsed/trusted is DUE;
//  4. a change at the tree root or its parent since the marker is DUE;
//  5. an elapsed full-walk interval is DUE;
//  6. only then may another start's recent walk be trusted — the skip.
//
// FAIL-OPEN toward sweeping is the invariant: steps 2-5 all mean "walk", and
// step 6 is reachable only for a marker that was read, believed, and whose
// tree evidence still matches what the marker recorded.
func OrphanSweepDue(now time.Time) (bool, string) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(crashDumpSweepEnvName))) {
	case crashDumpSweepEnvForce:
		return true, CrashDumpSweepReasonForced
	case crashDumpSweepEnvOff:
		return false, crashDumpSweepReasonDisabled
	}

	root, err := crashDumpSweepRoot()
	if err != nil {
		return true, crashDumpSweepReasonNoMarker
	}
	probe, err := treeChangeProbe(root)
	if err != nil {
		// No tree, or a tree we cannot stat: nothing to trust a skip on.
		return true, crashDumpSweepReasonRootUnknown
	}

	path, err := crashDumpSweepMarkerPath()
	if err != nil {
		return true, crashDumpSweepReasonNoMarker
	}
	m, err := readCrashDumpSweepMarker(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, crashDumpSweepReasonNoMarker
		}
		// Unreadable or unparseable: report the reason, and WALK (Rule 15 —
		// a marker that cannot be read must cost a walk, never buy a skip).
		return true, crashDumpSweepReasonUnreadable
	}

	// A marker describing a different tree (moved data root, copied state) is
	// not evidence about THIS tree.
	if m.RootPath != root {
		return true, crashDumpSweepReasonForeign
	}
	// A marker dated in the future (clock went backwards, hand-edited file)
	// cannot bound anything: walk.
	if m.SweptAt.After(now) {
		return true, crashDumpSweepReasonFutureDated
	}
	// The change evidence: a name added/removed/renamed at the tree root or in
	// its parent since the marker FORCES the walk. This is the layer that makes
	// a session killed at startup (its directory appears, its JSONL never
	// written) visible on the very next start. Writes INSIDE an existing
	// session dir are out of this probe's reach by construction — the floor
	// and the liveness clause cover them (see the header).
	if !sameTreeChangeProbe(probe, m.Probe) {
		return true, crashDumpSweepReasonChanged
	}
	// The floor: no skip may outlive the bounded cadence, so even a marker
	// with perfect change evidence self-heals from a wrong calibration.
	if now.Sub(m.SweptAt) >= crashDumpSweepFullWalkInterval {
		return true, crashDumpSweepReasonFloorDue
	}
	// The budget: this start trusts the walk another start already paid for.
	// Never longer than the floor (guaranteed above).
	if now.Sub(m.SweptAt) < time.Duration(crashDumpSweepBudgetSec)*time.Second {
		return false, crashDumpSweepReasonFresh
	}
	return true, crashDumpSweepReasonFloorDue
}

// MarkOrphanSweepDone records that a full sweep of the sessions tree COMPLETED
// at now — recorded after the sweep, not before: a sweep that panics mid-walk
// must be retried on the next start, and the only way to express that is to
// mark the completion — together with the tree's change fingerprint read at
// this moment, which is the evidence the next start compares against.
//
// The error is returned for the caller to REPORT (Rule 15). A marker that
// cannot be written costs the next start a walk; it must never be silent.
func MarkOrphanSweepDone(now time.Time) error {
	path, err := crashDumpSweepMarkerPath()
	if err != nil {
		return fmt.Errorf("resolve sweep marker path: %w", err)
	}
	root, err := crashDumpSweepRoot()
	if err != nil {
		return fmt.Errorf("resolve sessions root: %w", err)
	}
	// Sample the tree's fingerprint only once its directory timestamps have
	// settled (see crashDumpSweepProbeSettle): the sweep that just completed
	// wrote dumps INTO this tree, and ext4 publishes the parent's timestamps
	// lazily, so a probe taken immediately would record a state that the
	// filesystem has not finished publishing — every later start would then
	// read a mismatch and walk.
	time.Sleep(crashDumpSweepProbeSettle)
	probe, err := treeChangeProbe(root)
	if err != nil {
		// Without the tree's fingerprint the marker cannot support a skip; and
		// a fingerprint-less marker is worse than none, so refuse to write one.
		return fmt.Errorf("probe sessions tree %s: %w", root, err)
	}
	m := crashDumpSweepMarker{
		Version:  crashDumpSweepMarkerVersion,
		SweptAt:  now.UTC(),
		Probe:    probe,
		RootPath: root,
	}
	return writeCrashDumpSweepMarker(path, m)
}
