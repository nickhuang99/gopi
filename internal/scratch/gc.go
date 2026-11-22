// Package scratch — the bounded sweep of the scratch tree (age + holder).
//
// WHY THIS FILE EXISTS: the scratch home (<home>/workspace/gopi-test-scratch)
// was bounded only by a sweep INLINE IN THE TEST PIN (internal/testutil's
// PinSuiteTempRoot): the tree was swept whenever someone ran a test, and never
// otherwise. A host that stops running suites therefore keeps every root its
// killed suites abandoned — and the standalone hook that was supposed to cover
// that case (GOPI_TEST_SCRATCH_GC_MIN) had NO READER at all: a hook with no
// consumer, which is exactly the defect the env-reader gate (Rule 21) now
// makes unrepresentable.
//
// The sweep moved here (from internal/testutil) because the only resident
// components that exist are PRODUCTION code — cmd/agent's worker and merger
// tick loops — and internal/testutil imports "testing", so a production import
// would drag the test framework into the shipped binary. internal/scratch is
// the leaf package (Rule 14), importable by every consumer without a cycle.
// internal/testutil keeps its suite-facing API by delegating here, so the
// suite contract (and its tests) are unchanged.
//
// THE SWEEP IS NAMESPACE-LIMITED BY CONSTRUCTION — it can never become a global
// delete:
//
//  1. It walks ONLY the scratch home (HomeDir: $GOPI_TEST_SCRATCH_HOME, else
//     <GOPI_TEST_HOME|~>/workspace/gopi-test-scratch). Never /, never /tmp,
//     never a foreign tree.
//  2. It collects ONLY names in this system's namespace (suite run roots, the
//     previous naming scheme's roots, queue/runtime entries). Anything else — a
//     neighbouring program's directory, a user's file — does not match and is
//     never touched.
//  3. It collects ONLY entries older than the age threshold AND held by no live
//     process (the /proc cwd+fd scan and the .holder registration).
//  4. It never recurses: only direct children of the home, bounded by
//     ScanLimit. A nested foreign directory is left entirely alone.
//
// ONE DELIBERATE EXCEPTION TO 4 (the queue): `<home>/queue` is the one
// container this system owns whose DIRECT CHILDREN are themselves the runs
// ("queue-<what>-<pid>", each possibly holding tens of MB of gocache/
// gomodcache). It is therefore swept one level down — per child, NOT as a unit
// (a container is not a unit of ownership: on the measured host one fresh child
// shielded 5,583 stale siblings). Foreign entries inside it are still never
// touched, and the queue directory itself is never removed.
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
)

const (
	// DefaultStaleAge is the age past which an unheld scratch directory is
	// garbage — the DEFAULT of the stale-window knob (GCStaleAgeEnvName /
	// GCStaleAgeSettingsKey / the --scratch-gc-stale-min flag), not a frozen
	// policy.
	//
	// WHAT IT WAS, AND WHY IT MOVED. Until 2026-09-18 this was `StaleAge`, a
	// bare constant carrying a stated premise that is measurably FALSE on this
	// host: "longer than any healthy suite run (the whole GoPi test tree is
	// minutes)". Measured on the live scratch home (2026-09-18, probe recorded
	// in devdoc/scratch-gc-stale-window.md §2): of 137 top-level entries, 92
	// were older than an hour and 34 older than a day, and a whole-filesystem
	// health report of `removed=0` at six hours was the POLICY talking, not the
	// mechanism — the entries the sweep selected were overwhelmingly vetoed by
	// the live-holder rule (172 of 175), i.e. their owning suite, a still
	// running parallel session, holds them.
	//
	// WHAT THE NUMBER MEANS NOW: it is the DEFAULT, and the operator owns it.
	// 6h is the conservative value for a host whose suites are unattended —
	// nothing is reaped while a run may plausibly still be alive — while a host
	// measured to fill in hours (the 2026-09-15 incident: 7.8G tmpfs at 100%
	// inside a 6h window) sets its own window from outside the process. Rule 1:
	// the value lives here ONCE; the resolution that applies it lives in ONE
	// place (ResolveStaleAge below), which every sweep face calls.
	DefaultStaleAge = 6 * time.Hour

	// GCStaleAgeEnvName is the ONE name of the stale-window knob: the age, in
	// MINUTES, past which an unheld scratch entry is garbage. It is defined HERE
	// (the leaf package, Rule 14) because the resident tick (production), the
	// one-shot CLI verb and internal/testutil all spell it, and a second literal
	// would be drift waiting to happen. It is the THRESHOLD sibling of
	// GCIntervalEnvName (the cadence): the system already accepted that this
	// sweep has an operator-tunable dimension, and only the threshold lacked one
	// — that asymmetry was the defect.
	//
	// Read in exactly ONE place — ResolveStaleAge below (Rule 2), which every
	// sweep face calls.
	GCStaleAgeEnvName = "GOPI_TEST_SCRATCH_STALE_MIN"

	// GCStaleAgeSettingsKey is the settings.json key carrying the same value
	// (minutes), the declarative layer of the same knob — the THRESHOLD
	// counterpart of the `scratch_gc` cadence/off switch. internal/config's
	// catalog row references this constant, so the key is spelled once.
	GCStaleAgeSettingsKey = "scratch_gc_stale_min"

	// GCLockFileName is the GC mutex inside the scratch home: tests and
	// resident ticks can run concurrently (several suites, several agent
	// sessions, a worker and a merger on one host), and an unsynchronised
	// sweep would have two GCs race the same directory. The lock is advisory
	// and self-healing (GCLockStaleAge), never a hard dependency.
	GCLockFileName = ".gc.lock"
	// GCLockStaleAge is how old a GC lock must be before it is broken: a
	// crashed GC must not disable the sweep forever.
	GCLockStaleAge = 10 * time.Minute

	// GCIntervalEnvName is the ONE name for the sweep's cadence knob: minutes
	// between two sweeps. It is defined HERE (the leaf package, Rule 14)
	// because BOTH the tick (cmd/agent/scratch_gc_tick.go, whose production
	// door is `gopi tool scratch-gc --tick`) and the suite layer
	// (internal/testutil) spell it, and a second literal would be drift waiting
	// to happen. 0 disables the tick (the same meaning the explicit off switch
	// has). Corrected 2026-10-30: this said "the resident worker/merger tick",
	// and neither process calls it any more.
	GCIntervalEnvName = "GOPI_TEST_SCRATCH_GC_MIN"

	// ScanLimit caps how many directory entries one GC pass walks, so a
	// pathological scratch home cannot turn GC into a hang.
	ScanLimit = 10000

	// SuiteRunMarker / LegacyScratchMarker / QueueMarker identify the
	// directories this system owns and may collect: a per-run root
	// ("suite-<name>-run-<rand>"), a root left by the previous naming scheme
	// ("gopi-suite-tmp-"), and a queue entry ("queue-<what>-<pid>").
	SuiteRunMarker       = "suite-"
	LegacyScratchMarker  = "gopi-suite-tmp-"
	QueueMarker          = QueueDirName + "-"
	readmeFileNameLegacy = "README.md"

	// SuitePackageLockPrefix names the PER-PACKAGE cross-run lock directory
	// family that lives in this home ("suite-package-<suite>", see
	// internal/testutil's suite-package mutual exclusion): two whole-suite runs
	// of ONE package must not interleave, and the directory holding the flock is
	// the address both of them agree on.
	//
	// It is defined HERE, beside SuiteRunMarker, because the SWEEP has to
	// recognise the family (Rule 14: the writer and the sweeper must read the
	// same constant, and internal/scratch is the leaf both can import). It
	// carries the "suite-" marker so the name is inside this tree's own
	// namespace — a lock created anywhere else would be a lock no sweep and no
	// reader of this home knows about.
	SuitePackageLockPrefix = "suite-package-"

	// EntryKeepPackageLock is the KEEP a package lock's directory carries at ANY
	// age, and it is not an age heuristic: the lock's subject is the INODE of the
	// file inside (syscall.Flock), so removing the entry while a live run holds
	// it would let a third process create a fresh inode at the same path and
	// "hold" the same package — two runs at once, which is the defect the
	// mechanism exists to prevent. The kernel releases the lock when its holder
	// ends, so nothing accumulates that a sweep needs to collect.
	EntryKeepPackageLock = "a package lock directory is the flock's own subject — never collected at any age"

	// DirPerm / FilePerm are the standard owns-everything modes for the
	// scratch tree (nothing here is secret, everything is deletable).
	DirPerm  = 0o755
	FilePerm = 0o644

	// RepairDirPerm / RepairFilePerm are the modes the sweep restores while
	// digging an already-collectable entry out of a permission block. A
	// directory taken to 0o000 (or any mode without owner write+execute)
	// makes every descendant unreachable, so RemoveAll fails with EACCES and
	// the WHOLE entry survives the sweep forever. Owner-only, minimal: the
	// repair re-opens exactly what the deletion needs and nothing more.
	RepairDirPerm  = 0o700
	RepairFilePerm = 0o600
)

// ErrSweepNotOwned is deliberately not an error path: see SweepHome.

// ResolveStaleAge is the ONE reader of the stale-window knob (Rule 2): the age
// past which an unheld scratch entry is garbage. EVERY sweep face calls it — the
// resident tick and the one-shot CLI verb (cmd/agent), the dry-run plan, the
// memory-base arm and the suite pin (internal/testutil) — so the window is
// decided in exactly one place and cannot differ between faces.
//
// Channels, all landing in the SAME variable and therefore needing no ranking
// of their own (the project's precedence is applied by the machinery that owns
// it: a flag overwrites the env, settings only FILL an unset variable):
//
//	--scratch-gc-stale-min N    config.applyFlag, and config.BootstrapEnvFromArgs
//	                            hoists it before the subcommand chain
//	GOPI_TEST_SCRATCH_STALE_MIN=N           (GCStaleAgeEnvName)
//	settings.json "scratch_gc_stale_min": N (config.ApplyEnv publishes it)
//	default = DefaultStaleAge
//
// The unit is MINUTES — the unit the cadence knob already uses, because a knob
// whose unit is guessable is a knob that will be mis-set (a raw Duration would
// make "600" mean ten microseconds).
//
// A malformed or non-positive value is REPORTED and falls back to the default
// (Rule 15: no silent failure). It is deliberately NOT an off switch: a
// threshold that can never be exceeded would reap NOTHING, which is precisely
// the defect this knob exists to fix. The off switch is the cadence knob
// (GCIntervalEnvName=0 / the settings `scratch_gc` switch).
func ResolveStaleAge() time.Duration {
	raw := strings.TrimSpace(os.Getenv(GCStaleAgeEnvName))
	if raw == "" {
		return DefaultStaleAge
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "scratch: %s=%q is not a positive number of minutes — using the default %v\n",
			GCStaleAgeEnvName, raw, DefaultStaleAge)
		return DefaultStaleAge
	}
	return time.Duration(n) * time.Minute
}

// ScratchEntryStale reports whether a scratch entry is old enough to collect.
// Age is the WHOLE ENTRY's mtime (a directory's mtime moves when a child is
// created or removed, and a per-run root itself is written only at setup), so a
// suite whose process was killed mid-run still ages out.
func ScratchEntryStale(path string, now time.Time, olderThan time.Duration) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return now.Sub(fi.ModTime()) > olderThan, nil
}

// IsScratchEntry reports whether a scratch-home entry is one of this system's
// throwaway directories: a per-run ROOT ("suite-<name>-run-<rand>"), a root
// left by the previous naming scheme, a nested per-test directory inside a root
// that was killed mid-run, and a queue entry ("queue-<what>-<pid>").
//
// A per-run root's own mtime does NOT move when a test binary creates and
// abandons t.TempDir() entries inside it, so a root abandoned by a KILLED suite
// never ages out on its children's activity — it must be collectable by its own
// age, or it leaks forever. (Measured before the fix: 39 abandoned roots / 53M
// in the scratch home from the suites' own killed runs — the same
// unbounded-growth mechanism as the /tmp 13,044-directory incident.)
//
// Collecting a run root is SAFE because two independent holders veto it: the
// /proc/<pid>/cwd|fd scan (a live process inside it) and the .holder
// registration (pid + kernel start time, written by the owner — the
// permission-free channel that also covers same-user siblings /proc will not
// let us inspect). A live suite is therefore never collected; only an
// age-stale, unheld root is.
func IsScratchEntry(name string) bool {
	for _, marker := range []string{SuiteRunMarker, LegacyScratchMarker, QueueMarker} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

// SweepHome removes scratch entries under home that are older than olderThan
// AND held by no live process. It returns how many entries were removed.
//
// It is:
//
//   - safe under concurrency (an advisory lock file inside the home, so two
//     suites starting at the same moment do not sweep each other's removals);
//   - conservative (an entry whose holder check errored is KEPT — deleting a
//     directory a live process cannot see is a data-loss bug, a leaked
//     directory is only disk usage);
//   - a no-op when the home does not exist (nothing to do, not an error).
//
// The home argument is explicit (rather than read from HomeDir() here) so a
// caller can never accidentally sweep a tree it did not resolve itself, and so
// a test can sweep a fixture.
func SweepHome(home string, olderThan time.Duration) (int, error) {
	removed, _, err := SweepHomeDetailed(home, olderThan)
	return removed, err
}

// SweepHomeDetailed is SweepHome's reporting twin: it performs the SAME sweep
// and additionally hands the caller the LOCK verdict that decided whether the
// sweep ran at all (GCLockVerdict — acquired / broke-abandoned / deferred-live /
// deferred-unmeasurable, with the holder pid and the sentence).
//
// WHY IT EXISTS (Rule 15, measured 2026-10-12): the sweep is a no-op whenever
// somebody else holds the lock, and that no-op used to be INDISTINGUISHABLE from
// "ran, and there was nothing to collect" — both printed `removed=0` with an
// empty stderr. Worse, a `.gc.lock` left behind by a CRASHED GC (its pid written
// inside, that pid long dead) deferred every pass forever, because the only
// release path was the 10-minute mtime valve. The verdict is therefore the
// answer to a question the caller could not previously ask, and the CLI and the
// resident tick print it (see cmd/agent's scratch-gc lock line).
//
// The plain `SweepHome(home, olderThan) (int, error)` shape is kept as a thin
// wrapper: twenty-odd contract tests exercise the sweep, and none of them is
// about the lock evidence — rewriting them would trade a real contract for a
// signature change.
func SweepHomeDetailed(home string, olderThan time.Duration) (int, GCLockVerdict, error) {
	entries, err := os.ReadDir(home)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, GCLockVerdict{Outcome: GCLockOutcomeAcquired, Reason: "scratch home does not exist: nothing to sweep"}, nil
		}
		return 0, GCLockVerdict{}, fmt.Errorf("read scratch home %s: %w", home, err)
	}
	release, verdict, err := AcquireGCLockVerdict(home)
	if err != nil {
		return 0, verdict, err
	}
	if !verdict.Locked {
		return 0, verdict, nil // another GC holds the lock: the verdict says who
	}
	defer release()

	now := time.Now()
	removed := 0
	self := strconv.Itoa(os.Getpid())
	// ONE /proc pass for the whole sweep: the per-entry form would cost a full
	// process-table walk per candidate (6,064 queue children on the measured
	// host — the live dry-run hung). Collected lazily, on the first queue
	// entry that needs it, so a home with no queue pays nothing.
	var holders HolderScan
	holdersLoaded := false
	for i, entry := range entries {
		if i >= ScanLimit {
			return removed, verdict, fmt.Errorf("scratch home %s has more than %d entries — sweep stopped early", home, ScanLimit)
		}
		path := filepath.Join(home, entry.Name())
		// The collectability predicate is SHARED with SweepHomePlan (the
		// dry-run face): one place decides what may be deleted, so a
		// watchdog's "would-remove" line can never promise something the
		// real sweep would refuse (or vice versa).
		if name := entry.Name(); name == QueueDirName {
			// The queue is this system's own NAMESPACE, not a name-marker
			// citizen: its children need no marker, and each is judged on its
			// own age + holder (see queueChildCollectable). Swept PER CHILD
			// rather than as a unit — one fresh or held run must never shield
			// the stale ones (see queueSweepChildren).
			if !holdersLoaded {
				holders = ScanHolders()
				holdersLoaded = true
			}
			removed += queueSweepChildren(path, now, olderThan, holders)
			continue
		}
		verdict := EntryVerdictFor(path, entry.Name(), now, olderThan, self)
		if !verdict.Collectable {
			continue
		}
		if err := removeCollectable(path); err != nil {
			fmt.Fprintf(os.Stderr, "scratch: GC could not remove %s: %v\n", path, err)
			continue
		}
		reportCollected(verdict)
		removed++
	}
	return removed, verdict, nil
}

// reportCollected writes the ONE line that makes a removal readable: the path,
// the recorded owner the verdict was measured against, and the outcome that
// authorised the delete. It is the reason a removal count is a signal rather than
// a bare number — an operator can see WHICH writer's root went and on what
// evidence, without re-running the judgement by hand (Rule 15: name the cause).
//
// It reports on stderr so the logfmt summary on stdout stays a single machine-
// readable line per arm.
func reportCollected(v EntryVerdict) {
	fmt.Fprintf(os.Stderr, "scratch: collected %s %s (%s)\n", v.Path, OwnerEvidence(v), v.Reason)
}

// removeCollectable deletes a scratch entry that has ALREADY passed the
// collectability predicate (stale + this system's namespace + no live holder).
//
// THE DEFECT IT CLOSES (measured on this host, 2026-09-15): the deletion was a
// bare os.RemoveAll. An entry whose subtree contains a directory without owner
// write+execute — a fixture's os.Chmod(dir, 0) probe left behind by a KILLED
// suite, so the test's own restore never ran — fails every RemoveAll with
// EACCES from the openat of that descendant. The entry is measurable, stale and
// unheld, so the sweep SELECTS it every pass and fails to delete it every pass:
// immortal residue. Measured: /dev/shm at 100% (7.8G/0 free), a 17h-old
// `suite-internal-testutil-run-*` root reported `would-remove` by --dry-run
// while the real sweep printed `removed=0` + `permission denied`, so the whole
// subtree (and the tmpfs space it held) could never be drained. `chmod 0700`
// on that one directory flipped the same sweep to `removed=1` — the differential
// that names the cause.
//
// THE RULE THIS RESPECTS: "an entry the sweep cannot MEASURE is kept" is a
// contract (Collectable / entryCollectable both return false when Lstat or the
// holder pass fails), and it must keep holding. The repair is therefore scoped
// to the one case that does not weaken it: the sweep has ALREADY measured the
// entry — it is stale, in-namespace and unheld — and only the DELETE fails. An
// entry that could not be measured is never a candidate here, so it is still
// reported and kept; a measurable entry with a permission-blocked DESCENDANT is
// repaired (owner-only access restored) and retried ONCE.
func removeCollectable(path string) error {
	// THE MOUNT ARM RUNS FIRST, and it must: an entry carrying a DEAD FUSE mount
	// point cannot be deleted at all while the kernel keeps the mount-table
	// entry (measured 2026-10-15 — see mount_detach.go). It is not a mode
	// problem, so the permission repair below cannot help, and a first RemoveAll
	// would only walk into the mount and fail ENOTCONN. Detaching first means
	// the ONE deletion below is the one that counts.
	//
	// A failure here is REPORTED (the sweep's own line carries the cause instead
	// of a bare ENOTCONN) and does not stop the attempt: an entry with several
	// independent problems should not need several ticks to become visible.
	if _, err := releaseStaleGvfsMounts(path); err != nil {
		fmt.Fprintf(os.Stderr, "scratch: mount release under %s reported a failure: %v\n", path, err)
	}
	err := os.RemoveAll(path)
	if err == nil {
		return nil
	}
	if !entryReadable(path) {
		return err // unmeasurable: report the ORIGINAL refusal, keep it (Rule 15)
	}
	if !grantOwnerAccess(path) {
		return err // nothing was repaired: report the ORIGINAL refusal
	}
	return os.RemoveAll(path)
}

// entryReadable reports whether the sweep can see INSIDE the entry — the
// measurement that authorises the repair, and the one that keeps the contract
// intact.
//
// WHY "CAN SEE INSIDE" AND NOT Lstat: Lstat needs only the PARENT's execute bit,
// so it succeeds for an entry whose own mode is 0o000 — the very shape the
// contract test TestGCUnreadableScratchEntryDoesNotPanic pins as KEEP (Rule 15:
// never guess about a tree you cannot inspect). Opening the entry and reading one
// name is the cheapest probe of the property that actually matters: whether the
// sweep is looking at a tree it has measured, or at an opaque directory it must
// leave alone. It also keeps the real defect collectable — there the ENTRY was
// drwx------ (fully readable) and only a DESCENDANT was mode 0.
func entryReadable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		// Already gone (a concurrent sweep, or RemoveAll emptied the tree):
		// nothing is left to repair, and reporting the original error is
		// correct behaviour, so treat it as not-authorised.
		return false
	}
	defer func() { _ = f.Close() }() //stylegate:ignore best-effort close of a probe handle; the read below already decided the verdict
	_, err = f.Readdirnames(1)
	// io.EOF means the directory is EMPTY — fully measured, so a repair of
	// nothing is still authorised (RemoveAll will simply retry).
	return err == nil || errors.Is(err, io.EOF)
}

// grantOwnerAccess restores owner rwx/read on every entry under root that is
// missing it (a directory's own mode is what gates reaching its children, so
// parents are repaired before the walk descends). Returns whether the walk
// completed — a partially repaired tree is still worth the retry, so one
// unreadable child does not abandon the rest (Rule 15b).
func grantOwnerAccess(root string) bool {
	ok := true
	// The root's own mode has to be repaired first: nothing below it can be
	// seen at all while it lacks owner execute.
	if err := repairOwnerPerm(root, true); err != nil {
		ok = false
	}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error { //stylegate:ignore best-effort repair walk; its verdict is carried by ok, and an unreadable child must not abandon the rest (Rule 15b)
		if walkErr != nil {
			ok = false // unreadable: report, keep going
			return nil
		}
		// A symlink's own mode is ignored by Linux, and following it would
		// step outside the entry being collected — never descend one.
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if err := repairOwnerPerm(p, d.IsDir()); err != nil {
			ok = false
		}
		return nil
	})
	return ok
}

// repairOwnerPerm adds the owner's write (and, for a directory, execute) bit
// when the current mode lacks it — the minimum a deletion needs, never a
// broader grant.
func repairOwnerPerm(path string, isDir bool) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	mode := fi.Mode().Perm()
	want := os.FileMode(RepairFilePerm)
	if isDir {
		want = RepairDirPerm
	}
	if mode&want == want {
		return nil // already reachable/deletable
	}
	return os.Chmod(path, mode|want)
}

// ScratchPlanEntry is one scratch-home entry a sweep would collect: its path
// and how old it already is. It is the dry-run's unit of report — a watchdog
// operator can see WHAT is about to go before letting the timer delete it.
type ScratchPlanEntry struct {
	Path string
	Age  time.Duration
}

// SweepHomePlan reports the entries SweepHome would collect, deleting
// nothing. It walks the same entries under the same ownership and age rules
// and asks the same Collectable predicate — the ONLY difference is that it
// never calls RemoveAll and takes no lock.
//
// WHY IT EXISTS: the one-shot CLI face (gopi tool scratch-gc --dry-run) is how an
// operator vets a host-level watchdog BEFORE arming it. Verification that
// depends on deleting first is not verification — so the plan path shares the
// predicate and the full sweep shares it too.
func SweepHomePlan(home string, olderThan time.Duration) ([]ScratchPlanEntry, error) {
	entries, err := os.ReadDir(home)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // nothing to do is not an error (mirrors SweepHome)
		}
		return nil, fmt.Errorf("read scratch home %s: %w", home, err)
	}
	now := time.Now()
	self := strconv.Itoa(os.Getpid())
	plan := make([]ScratchPlanEntry, 0, len(entries))
	for i, entry := range entries {
		if i >= ScanLimit {
			return plan, fmt.Errorf("scratch home %s has more than %d entries — plan stopped early", home, ScanLimit)
		}
		path := filepath.Join(home, entry.Name())
		if name := entry.Name(); name == QueueDirName {
			// The queue is planned PER CHILD, exactly as SweepHome collects
			// it — the dry-run face must never promise a different set than
			// the real sweep (see queueSweepChildren).
			for _, child := range queuePlanChildren(path, now, olderThan, ScanHolders()) {
				plan = append(plan, child)
			}
			continue
		}
		if !Collectable(path, entry.Name(), now, olderThan, self) {
			continue
		}
		fi, err := os.Lstat(path)
		if err != nil {
			continue // vanished between the predicate and the measurement — not a finding
		}
		plan = append(plan, ScratchPlanEntry{Path: path, Age: now.Sub(fi.ModTime())})
	}
	return plan, nil
}

// Collectable reports whether one scratch-home entry may be removed: it is in
// this system's namespace, it is age-stale, and no live process holds it.
// Every veto is a KEEP (the hard rule), and the predicate is deliberately
// conservative about what it cannot measure.
//
// self is the sweeping process's own pid string: a directory we hold
// ourselves is collectable (a previous run of OUR process cannot be alive).
func Collectable(path string, name string, now time.Time, olderThan time.Duration, self string) bool {
	return EntryVerdictFor(path, name, now, olderThan, self).Collectable
}

// EntryVerdictFor is Collectable in its REPORTING form: the same predicate, in the
// same order, returning the verdict AND the sentence that decided it. There is
// exactly ONE such function, so the reporting face can never promise something a
// real sweep refuses (or vice versa) — the invariant the plan face already guards,
// extended to the per-candidate reasons.
//
// The order of the arms is the rule, cheapest first:
//
//	① the reserved names (the GC lock, the tree README) are never scratch entries
//	② the NAMESPACE — a name marker, or the pooled batch prefix; anything else
//	  belongs to a neighbour this system must not touch, at any age
//	③ the entry ALREADY in this system's namespace: the writer's own record, the
//	  /proc holder veto, then — only for a root nobody registered — the age window
//	  (see entryVerdictInNamespace)
func EntryVerdictFor(path string, name string, now time.Time, olderThan time.Duration, self string) EntryVerdict {
	v := EntryVerdict{Path: path}
	if name == GCLockFileName || name == readmeFileNameLegacy {
		v.Reason = EntryKeepReserved
		return v
	}
	if strings.HasPrefix(name, SuitePackageLockPrefix) {
		// ①D the PACKAGE LOCK family: a directory holding a kernel flock's subject.
		// It is inside this system's namespace (the marker), so the arms below
		// would judge by age and holder — and both answers are wrong here: the
		// entry's own mtime says nothing about whether a run holds the lock, and
		// the holder veto would only save it while the /proc scan happens to see
		// an open descriptor. Deleting the entry while it is held does not merely
		// lose a file: it re-opens the "second run takes a fresh inode" hole.
		v.Reason = EntryKeepPackageLock
		return v
	}
	pool := strings.HasPrefix(name, BatchPoolDirPrefix)
	if !IsScratchEntry(name) && !pool {
		// ①B/①C the SUBTREE and BATCH-POOL cases: entries whose namespace is not
		// in their own NAME as a marker. Both are arms rather than markers,
		// because neither name contains one — the queue dir is the bare constant
		// `queue` (swept one level down by queueSweepChildren /
		// queuePlanChildren, where the CONTAINER is the namespace declaration),
		// and a pooled batch dir carries only its prefix, with the pool's own
		// WIDER window (BatchPoolStaleAge) because the pool exists to survive
		// across runs.
		//
		// Measured leaks this closes: 5,887 entries under <home>/queue including
		// orphaned queue-runtime-merge-build-* run directories (their merger was
		// killed, their pid never returns) that a direct-children-only sweep
		// could not reach (2.2G on this host); and a fully built batch pool whose
		// only readers were tests, so a batch directory whose rsync died was
		// collectable by NOTHING (a hook with no consumer: the Rule 21 defect).
		v.Reason = EntryKeepNotOurs
		return v
	}
	window := olderThan
	if pool {
		window = BatchPoolStaleAge
	}
	// The /proc scan is the caller's (a sweep may have collected ONE pass for a
	// whole subtree). An unmeasurable scan is not a hold — and it must not be read
	// as permission either, which is why it travels to the verdict.
	holder, herr := DirHolder(path)
	held, evidence := false, ""
	if herr == nil && holder != "" && holder != self {
		held, evidence = true, fmt.Sprintf("held by live pid %s", holder)
	}
	v = entryVerdictInNamespace(path, now, window, held, evidence, herr)
	return v
}

// entryVerdictInNamespace judges an entry ALREADY known to be in this system's
// namespace: the writer's OWNER RECORD first (a proven-dead owner needs no clock),
// then the /proc holder veto, then — only for a root nobody registered — the age
// window. An entry whose measurement failed is KEPT and says so: the conservative
// direction both vetoes have always carried (Rule 15: never guess about a tree you
// cannot inspect).
//
// THE ORDER IS THE CONTRACT, and the owner arm is FIRST because that is the whole
// point of a writer-owned lifetime: an owner the kernel PROVES gone (or whose pid
// was recycled) authorises the delete at any age — the question "may this be
// collected?" was answered by the process that made it, not by a clock. A root
// with NO record has no such proof and falls through to the clock; a record saying
// the owner is live (or that the kernel would not answer) keeps the entry whatever
// its age.
func entryVerdictInNamespace(path string, now time.Time, olderThan time.Duration, held bool, holderEvidence string, holderErr error) EntryVerdict {
	v := EntryVerdict{Path: path}
	owner := OwnerVerdictFor(path)
	v.HasOwner, v.Owner, v.Outcome = owner.Registered, owner.Record, owner.Outcome
	if !owner.Registered {
		v.Outcome = EntryOutcomeUnregistered
	} else if !owner.Collectable {
		// Proven live, or unreadable/unverifiable: KEEP at any age.
		v.Reason = owner.Reason
		return v
	}
	if fi, err := os.Lstat(path); err == nil {
		v.Age = now.Sub(fi.ModTime())
	}
	// ② THE /proc HOLDER: a live process standing in the directory keeps it, at any
	// age — the hard rule, and the one veto the writer's own record cannot
	// substitute for (a worker may be running inside a root it did not write).
	if holderErr != nil {
		// A check we cannot complete never authorises a delete.
		v.Reason = fmt.Sprintf("%s: %v", EntryKeepUnmeasurable, holderErr)
		return v
	}
	if held {
		if holderEvidence == HolderScanWithheldReason() {
			// The veto was an UNFINISHED scan, not a sighting. The
			// EntryKeepLiveHolder prefix would assert a process nobody
			// observed, so the disclosure stands alone as the reason
			// (Rule 15: the two facts must not render the same).
			v.Reason = holderEvidence
			return v
		}
		// holderEvidence names WHICH channel saw the holder (the /proc scan, a
		// pid, the registration file), so the sentence the report prints is the
		// measurement, not a second opinion about it.
		v.Reason = fmt.Sprintf("%s (%s)", EntryKeepLiveHolder, holderEvidence)
		return v
	}
	if owner.Registered {
		v.Collectable = true
		v.Reason = EntryCollectablePrefix + owner.Reason
		return v
	}
	// ③ UNREGISTERED: the clock is the ONLY bound the writer left behind, so it is
	// applied here and nowhere earlier.
	stale, err := ScratchEntryStale(path, now, olderThan)
	if err != nil {
		v.Reason = EntryKeepUnmeasurable
		return v
	}
	if !stale {
		v.Reason = fmt.Sprintf("%s (%s)", EntryKeepFresh, olderThan)
		return v
	}
	v.Collectable = true
	v.Reason = EntryCollectablePrefix + OwnerReasonUnregistered
	return v
}

// EntryVerdictsForBatch is entryVerdictInNamespace for a caller that has ALREADY
// made the one /proc pass (LiveHolders) for a whole subtree — the queue's 6,064
// children cost 6,064 process-table walks otherwise (the measured hang that forced
// the batch form into the sweep). Same predicate, same order, same verdicts; only
// the holder SOURCE differs.
func EntryVerdictsForBatch(path string, now time.Time, olderThan time.Duration, holders HolderScan) EntryVerdict {
	held, evidence := false, ""
	if live, ok := holders.HeldByProcesses(path); !ok {
		// NOT looked at: the scan stopped before the end of the process table,
		// so "nobody inside" is missing evidence, not a measurement. The entry
		// is kept, and the sentence says WHICH of the two facts this is — the
		// caller must not read a truncated scan as "a process is inside".
		held, evidence = true, HolderScanWithheldReason()
	} else if live {
		// WHO is inside it, from the SAME pass (holder_identity.go): a proven-dead
		// owner whose only visitors are inherited descriptors from an orphaned log
		// pipeline is collectable, while a gopi image or a process standing in the
		// directory keeps it exactly as the hard rule always said.
		ids, total := HolderIdentitiesFromScan(path, holders)
		released, classEvidence := holdersRelease(ids, total)
		if released && ownerProvenGone(path) {
			return releasedVerdict(path, now, classEvidence)
		}
		evidence = holderEvidenceSentence(ids, total)
		if released {
			// The holders are all unrelated observers, but the OWNER is not proven
			// gone: the release rule stands on a dead owner, and releasing without
			// that proof would be deleting a root something may still own. The
			// sentence still NAMES the observers and the cure, so the operator can
			// see the pipeline behind the keep instead of an anonymous claim.
			evidence = fmt.Sprintf(HolderObserverOnlyFmt, holderPopulationClass(classEvidence), classEvidence)
		}
		held = true
	}
	return entryVerdictInNamespace(path, now, olderThan, held, evidence, nil)
}

// HomeVerdicts judges EVERY direct child of home (and every child of the queue,
// one level down, exactly as the sweep judges them) and returns one EntryVerdict
// each — the reporting face of SweepHome. It deletes nothing and takes no lock: a
// report must have no side effects, and it must not be able to block a sweep.
func HomeVerdicts(home string, olderThan time.Duration) ([]EntryVerdict, error) {
	entries, err := os.ReadDir(home)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read scratch home %s: %w", home, err)
	}
	now := time.Now()
	self := strconv.Itoa(os.Getpid())
	var holders HolderScan
	holdersLoaded := false
	out := make([]EntryVerdict, 0, len(entries))
	for i, entry := range entries {
		if i >= ScanLimit {
			return out, fmt.Errorf("scratch home %s has more than %d entries — report stopped early", home, ScanLimit)
		}
		path := filepath.Join(home, entry.Name())
		if entry.Name() == QueueDirName {
			if !holdersLoaded {
				holders = ScanHolders()
				holdersLoaded = true
			}
			out = append(out, queueVerdicts(path, now, olderThan, holders)...)
			continue
		}
		out = append(out, EntryVerdictFor(path, entry.Name(), now, olderThan, self))
	}
	return out, nil
}

// queueVerdicts is the queue's reporting twin: the SAME children the sweep judges,
// one level down, with the SAME verdicts (queueChildVerdict). An unreadable queue
// is reported as one verdict naming the failure rather than as silence — a
// candidate the report could not look at is a fact, not an empty set.
func queueVerdicts(root string, now time.Time, olderThan time.Duration, holders HolderScan) []EntryVerdict {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []EntryVerdict{{Path: root, Reason: fmt.Sprintf("%s: %v", EntryKeepUnmeasurable, err)}}
	}
	var out []EntryVerdict
	for i, entry := range entries {
		if i >= ScanLimit {
			return out
		}
		path := filepath.Join(root, entry.Name())
		if trees := declaredTreesForRoot(path); len(trees) > 0 {
			// The reporting twin of queueSweepChildren's interposition: a
			// declared tree's container is reported AS ITS OWN ENTRIES, never as
			// one verdict. Reporting the container was the 2026-10-12 defect's
			// readable half — `scratch-verdict …/queue/test-budget verdict=keep
			// … a live process holds it` was the ONLY line about a subtree that
			// held 320 run directories, so the container's own verdict decided
			// whether 10.27 GB was visible.
			//
			// ONE reporting twin serves both faces (DeclaredTreeVerdicts): the
			// queue arm and the trees arm judge the same tree the same way, so
			// a second walk here would be a second answer. This arm ignores the
			// census the twin returns — the scratch home has no per-tree summary
			// line, and its counts belong to the arm that prints them.
			//
			// EVERY row rooted here reports, for the reason declaredTreesForRoot
			// states: the test-budget root carries two rows with disjoint
			// predicates, and reporting only the first would leave the streams
			// (the 17.7 GiB half) with no verdict line at all — the exact silence
			// this half exists to end.
			for _, tree := range trees {
				verdicts, _ := DeclaredTreeVerdicts(tree)
				out = append(out, verdicts...)
			}
			continue
		}
		out = append(out, queueChildVerdict(path, entry.IsDir(), now, olderThan, holders))
	}
	return out
}

// unheld reports whether no live process holds path — the second half of the
// hard rule, shared by every collectable arm (the namespace/age half differs
// per arm; the liveness half never does).
func unheld(path, self string) bool {
	holder, err := DirHolder(path)
	if err != nil {
		return false // a check we cannot complete never authorises a delete
	}
	if holder != "" && holder != self {
		return false // LIVE owner — hard rule: never delete a held directory
	}
	return unheldRegistered(path)
}

// unheldBatch is unheld against ONE /proc pass already collected by the caller.
//
// WHY THE BATCH FORM IS MANDATORY HERE (measured on this host, 2026-09-14): a
// full DirHolder walk costs one process-table scan. Applied per entry it turns a
// sweep of the queue's 6,064 children into 6,064 walks — the live dry-run did
// not finish inside 120s, i.e. the fix meant to bound the tree HUNG the sweep
// that runs it. LiveHolders collects every held path in a single pass, and
// HeldByProcesses answers the same question per entry in memory, so the cost is
// one scan plus 6,064 comparisons. The per-entry form remains for the callers
// that judge a single directory (the public Collectable predicate).
//
// The .holder registration check is deliberately NOT batched: it reads a file
// inside the candidate itself (bounded, no process table involved).
func unheldBatch(path string, holders HolderScan) bool {
	// FAIL-CLOSED on truncation FIRST: a scan that did not reach the end of the
	// process table has not established that nobody holds the path, and a
	// delete on missing evidence is exactly the incident the hard rule exists
	// to prevent (a leaked directory is disk usage; a deleted live directory is
	// data loss).
	if holders.Truncated {
		return false
	}
	if pathHeldByProcesses(path, holders.Paths) {
		return false // LIVE owner — hard rule: never delete a held directory
	}
	return unheldRegistered(path)
}

// unheldRegistered applies the permission-free half of the hard rule: a
// registered, still-running owner (its .holder file, readable without /proc
// permission games) vetoes the delete.
func unheldRegistered(path string) bool {
	// The writer-owned half of the bound, in ONE call: a recorded owner the kernel
	// confirms live keeps the entry at ANY age, an unreadable record keeps it too,
	// and a record that is provably gone or recycled does not veto the delete. A
	// candidate with NO record falls through to the age rule exactly as before
	// (owner_record.go: OwnerKeeps).
	return !OwnerKeeps(path)
}

// queueSweepChildren removes the queue's own stale, unheld in-namespace
// children ONE BY ONE and reports how many it took.
//
// WHY PER-CHILD AND NOT ALL-OR-NOTHING (measured on this host, 2026-09-14): the
// subtree arm above keeps the WHOLE queue unless every child is collectable.
// On a host where suites run continuously that is the common case — of 6,064
// queue children, 5,583 were stale while one fresh (or one held, e.g. the run
// whose merger is alive right now) shielded all of them, so the queue grew
// unbounded while the sweep reported zero reclaimable entries. A subtree is not
// a unit of ownership: each child is its own run and is judged on its own
// merits (see each entry's own mtime, its own holder) — the same predicate the
// top level applies, applied one level down.
//
// The queue DIRECTORY is never removed here (it is the tree's structure, and
// the next writer recreates it at once — see TestSweepHomeKeepsAnEmptyQueueDirectory).
func queueSweepChildren(root string, now time.Time, olderThan time.Duration, holders HolderScan) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scratch: GC could not read %s: %v\n", root, err)
		return 0
	}
	removed := 0
	for i, entry := range entries {
		if i >= ScanLimit {
			fmt.Fprintf(os.Stderr, "scratch: %s has more than %d entries — child sweep stopped early\n", root, ScanLimit)
			return removed
		}
		path := filepath.Join(root, entry.Name())
		if trees := declaredTreesForRoot(path); len(trees) > 0 {
			// A queue child that is ANOTHER tree's declared ROOT is not a run of
			// the queue: it is that tree's container, and the tree's own row
			// judges what is INSIDE it. Judging the container here is the
			// 2026-10-12 defect exactly — one entry for the whole subtree, kept
			// by one live child, so a tree that is 10.27 GB full reported nothing
			// collectable. The container itself is never removed here: the row's
			// root survives every sweep (declaredEntryCollectable).
			//
			// EVERY row rooted here is swept, not the first: two rows can share
			// one root with disjoint predicates and different windows (see
			// declaredTreesForRoot), and stopping at the first left the second
			// row's candidates unplanned from this arm.
			for _, tree := range trees {
				removed += sweepDeclaredTreeFromQueue(tree, olderThan, holders, now)
			}
			continue
		}
		verdict := queueChildVerdict(path, entry.IsDir(), now, olderThan, holders)
		if !verdict.Collectable {
			continue // an artifact, fresh, or live-held child of the namespace
		}
		if err := removeCollectable(path); err != nil {
			fmt.Fprintf(os.Stderr, "scratch: GC could not remove %s: %v\n", path, err)
			continue
		}
		reportCollected(verdict)
		removed++
	}
	return removed
}

// queueChildCollectable reports whether ONE child of the queue may be removed.
//
// THE QUEUE IS ITS OWN NAMESPACE — this is the whole point of the arm, and the
// reason it is not spelled as another marker. Above the queue, an entry must
// carry its namespace in its NAME (IsScratchEntry: "suite-"/"gopi-suite-tmp-"/
// "queue-"), because the home is SHARED with neighbours this system must never
// touch. The queue is not shared: <home>/queue (QueueDirName) is this system's
// own declared container, so the CONTAINER is the namespace declaration and a
// child does not have to repeat it in its own name. Requiring the repetition
// was the defect (measured on this host, 2026-09-14): children produced without
// the marker — `verify-01a0a248-modcache-manual` (415 MB of gocache/gomodcache),
// `accept`, `test-budget` — matched no marker, so they were invisible to the
// sweep AND to --dry-run: never planned, never deleted, unbounded growth in a
// tree whose entire contract is that it is bounded. Adding one more marker
// string per producer would have left the hole open for the NEXT producer; the
// namespace declaration closes it for all of them (a new child is in-namespace
// by construction).
//
// The namespace widens WHAT is eligible, never WHETHER it is protected. Both
// existing vetoes still apply, unchanged and in this order:
//
//  1. AGE — a child younger than olderThan is kept (it may be a run that is
//     starting right now, before its owner has registered anything).
//  2. LIVE HOLDER — the /proc cwd|fd|exe|maps scan and the .holder
//     registration (pid + kernel start time). A live run is never collected;
//     this is the hard rule and it is deliberately checked LAST so the cheaper
//     measurement can reject first (see entryCollectable).
//
// Plus the ONE keep the namespace declaration itself adds: a FLAT entry (a
// regular file, a symlink, anything that is not a directory) is not a run. The
// queue is also a drop point for artifacts that carry no run semantics and
// cannot be attributed to one — a verifier's report (`01a09fdf-verify-report.md`),
// the shell library's cleanup registry (`.cleanup-<pid>`), a commit-message
// scratch file. Some are evidence a human is still reading; none of them is a
// run whose owner could be asked. A flat entry can therefore never be
// COLLECTED — but it also never SHIELDS, and it can never grow without bound on
// its own (bytes live in directories, which do age out). Rule 15: what cannot
// be attributed to a reclaimable run is left alone, not guessed at.
//
// isDir is passed in rather than re-stat'ed: the caller already holds the
// DirEntry from its own ReadDir, and a second Lstat would also change the
// symlink answer (entry.IsDir() is false for a symlink, deliberately: the sweep
// never follows one out of the tree).
func queueChildCollectable(path string, isDir bool, now time.Time, olderThan time.Duration, holders HolderScan) bool {
	return queueChildVerdict(path, isDir, now, olderThan, holders).Collectable
}

// queueChildVerdict is queueChildCollectable in its REPORTING form — the same
// judgement, plus the one KEEP the namespace declaration itself adds (a FLAT entry
// is an artifact, not a run, so it can never be collected — and never shield its
// siblings either).
func queueChildVerdict(path string, isDir bool, now time.Time, olderThan time.Duration, holders HolderScan) EntryVerdict {
	if !isDir {
		return EntryVerdict{Path: path, Reason: EntryKeepArtifact}
	}
	return EntryVerdictsForBatch(path, now, olderThan, holders)
}

// queuePlanChildren is the dry-run twin of queueSweepChildren: the SAME
// children, judged by the SAME predicate, reported instead of deleted. One
// predicate, two faces (the plan can never promise something the sweep refuses).
func queuePlanChildren(root string, now time.Time, olderThan time.Duration, holders HolderScan) []ScratchPlanEntry {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil // unreadable: nothing planned, and the sweep will report it too
	}
	var out []ScratchPlanEntry
	for i, entry := range entries {
		if i >= ScanLimit {
			return out
		}
		childPath := filepath.Join(root, entry.Name())
		if trees := declaredTreesForRoot(childPath); len(trees) > 0 {
			// The dry-run face must plan what the real sweep collects: a
			// declared tree's container is planned AS ITS OWN entries (see
			// queueSweepChildren), never as one unit — and EVERY row rooted here
			// contributes, or the plan would promise less than the sweep takes.
			for _, tree := range trees {
				planned, perr := DeclaredTreePlan(tree)
				if perr != nil && planned == nil {
					fmt.Fprintf(os.Stderr, "scratch: could not plan declared tree %s at %s: %v\n", tree.Name, childPath, perr)
					continue
				}
				for _, e := range planned {
					out = append(out, ScratchPlanEntry{Path: e.Path, Age: e.Age})
				}
			}
			continue
		}
		if !queueChildCollectable(childPath, entry.IsDir(), now, olderThan, holders) {
			continue
		}
		path := childPath
		fi, lerr := os.Lstat(path)
		if lerr != nil {
			continue // vanished between the predicate and the measurement
		}
		out = append(out, ScratchPlanEntry{Path: path, Age: now.Sub(fi.ModTime())})
	}
	return out
}

// entryCollectable is the namespace-independent half of Collectable: an entry
// already known to be in this system's namespace, judged on age THEN liveness
// (the same order, and the same conservative "cannot measure ⇒ keep" rule).
func entryCollectable(path string, now time.Time, olderThan time.Duration, holders HolderScan) bool {
	return EntryVerdictsForBatch(path, now, olderThan, holders).Collectable
}

// SweepHomeVerdicts is SweepHome's reporting twin: it judges every candidate and
// deletes nothing (the CLI's --explain face reads it). It never takes the GC lock,
// because a report must not be able to block a sweep.
func SweepHomeVerdicts(home string, olderThan time.Duration) ([]EntryVerdict, error) {
	return HomeVerdicts(home, olderThan)
}

// AcquireGCLock takes the advisory GC lock inside the scratch home, reporting
// only whether it got it — the pre-existing shape, kept as a THIN WRAPPER over
// AcquireGCLockVerdict (gc_lock.go), which is the ONE lock decision and the one
// that carries the evidence (who holds the lock, whether an abandoned one was
// broken, and why). A caller that must REPORT the lock — the CLI and the
// resident tick — uses the verdict form; a caller that only needs the mutex
// (or a test) keeps this one.
func AcquireGCLock(home string) (release func(), locked bool, err error) {
	release, v, err := AcquireGCLockVerdict(home)
	if err != nil {
		return nil, false, err
	}
	return release, v.Locked, nil
}

// ErrNoScratchHome reports that the scratch home could not be created, so a
// resident sweep has nowhere to look. It is a NAMED error so a resident caller
// can treat it as the expected non-event it is (a host that never ran a suite)
// rather than as a failure to report (Rule 15: no silent failure, but also no
// manufactured noise).
var ErrNoScratchHome = errors.New("scratch home unavailable")

// EnsureHomeDir creates the scratch home if it is missing and returns it. A
// resident sweep uses it once per tick so the FIRST sweep on a fresh host has a
// directory to walk instead of an error.
func EnsureHomeDir() (string, error) {
	home := HomeDir()
	if err := MkdirAllWithSuiteRunOwnerPerm(home, DirPerm); err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrNoScratchHome, home, err)
	}
	return home, nil
}
