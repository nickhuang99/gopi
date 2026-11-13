package scratch

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The SANDBOX arm of the scratch GC.
//
// WHY IT LIVES IN internal/scratch: the sandbox tree needs a reclaimer, and the
// reclaimer must not be a new daemon or a new tick (the epic's hard
// constraint). internal/scratch already owns the ONE sweep that the
// `gopi tool scratch-gc` face runs (its `--tick` and `sandboxes` arms), and it
// already owns the ONE cross-process liveness probe (DirHolder / LiveHolders —
// the /proc cwd|fd|exe+maps scan plus the .holder registration). A second
// probe, or a second sweep schedule, is how memory #2632's fail-open drift
// returns. So the sandbox arm is a THIRD predicate inside this package, built
// on the same two primitives:
//
//	reclaimable = OURS (name marker) AND AGED (own mtime) AND UNOWNED
//	              (no live incarnation of the recorded owner)
//	              AND UNHELD (no live process standing inside it)
//
// EVERY unknown is a KEEP. That is not caution for its own sake: the two
// failure directions are not symmetric. A sandbox that survives one more sweep
// costs disk; a sandbox deleted out from under its owner destroys a run's
// private state with no way to tell what was lost. So an unreadable /proc, an
// unparseable marker, a directory with no marker at all, or a probe that
// returned an error all resolve to KEEP (Rule 15: the uncertainty is REPORTED
// as the reason, never silently reinterpreted as permission).

const (
	// SandboxMarkerPrefix is the name marker of ONE sandbox under the sandbox
	// root: `sandbox-<label>-<rand>`. This is the DEFINITION (Rule 14) —
	// internal/config aliases it for the initializer, because this package
	// cannot import internal/config (config imports scratch).
	//
	// The marker is what keeps the arm scoped: the root is an ordinary
	// directory the operator may share with other things, and a child whose
	// name does not carry the prefix is never a candidate at any age. A sweep
	// that cleaned by LOCATION would be one mistyped configuration key away
	// from deleting the operator's own directories.
	SandboxMarkerPrefix = "sandbox-"

	// SandboxOwnerFileName is the ownership marker inside each sandbox (pid,
	// kernel start ticks, task id, created-at). Also the DEFINITION, aliased by
	// internal/config for the initializer.
	SandboxOwnerFileName = ".sandbox-owner.json"

	// SandboxStaleAge is the window after which an UNOWNED sandbox is garbage.
	//
	// It is deliberately far longer than the suite scratch's 6h default (which
	// bounds per-test throwaways): a sandbox is a whole private profile that an
	// operator may have created to reproduce something, and 6h is a normal
	// working day's fragment. 72h is long enough that no human workflow loses a
	// sandbox between two runs, and short enough that a week of crashed runs
	// cannot accumulate without bound.
	SandboxStaleAge = 72 * time.Hour

	// SandboxDefaultLeaf is the built-in leaf of the sandbox root under the
	// workspace (<home>/workspace/gopi-sandbox). This is the DEFINITION;
	// internal/config.SandboxDefaultLeaf aliases it (this package cannot import
	// internal/config — config imports scratch).
	SandboxDefaultLeaf = "gopi-sandbox"
)

// SandboxKeepReason names WHY a candidate was kept. They are constants (Rule 1b)
// because the tick's log line and the tests both match on them, and a reason
// typed twice drifts into a reason nobody can grep for.
const (
	SandboxKeepFresh         = "not aged"
	SandboxKeepUnknownOwner  = "owner unverifiable — /proc unreadable, marker unparseable, or no start ticks (unknown is never a delete)"
	SandboxKeepLiveOwner     = "live owner"
	SandboxKeepHeld          = "a live process stands inside it"
	SandboxKeepSelfCwd       = "this sweep's own working directory"
	SandboxKeepMarkerMissing = "no ownership marker and the age window has not been exceeded"
)

// SandboxPlan is one sandbox's verdict: the collector's REPORTING form (the
// same shape as ScratchPlanEntry — the destructive arm and the reporting arm
// share this predicate, so a dry run cannot disagree with a sweep).
type SandboxPlan struct {
	// Path is the sandbox directory.
	Path string
	// Keep is true when the sandbox must survive this sweep.
	Keep bool
	// Reason is why it is kept — or, when Keep is false, the evidence that
	// authorised collecting it ("owner pid 4711 (start ticks 998877) is gone").
	Reason string
}

// gopiProfileDir is the resolved PRODUCTION profile (<GOPI_HOME|$HOME>/.gopi).
// It exists for one guard: a `sandbox_dir` that points AT the operator's profile
// would turn this arm into a production-data sweep, so that value is refused by
// name before any directory is read. "" when no profile can be resolved — then
// there is nothing to confuse this root with.
func gopiProfileDir() string {
	if v := strings.TrimSpace(os.Getenv(GopiDirEnvName)); v != "" {
		return filepath.Join(v, gopiDirName)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, gopiDirName)
}

// sandboxOwnerMarker mirrors internal/sandbox's marker. It is a MIRROR by
// necessity — this package cannot import internal/sandbox (sandbox imports
// scratch) — and it is checked rather than trusted: the two structs' JSON tags
// are asserted equal by TestSandboxMarkerShapeAgreesWithTheInitializer, so a
// field renamed on one side fails a test instead of silently reading as zero.
type sandboxOwnerMarker struct {
	TaskID     string `json:"task_id,omitempty"`
	Label      string `json:"label,omitempty"`
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
	CreatedAt  string `json:"created_at"`
	Cmd        string `json:"cmd,omitempty"`
	Version    int    `json:"version"`
}

// SandboxRootRefusal reports why a path must never be swept as a sandbox root.
// It is the guard that keeps a mistyped configuration value from turning this
// arm into a delete-anything machine: sweeping happens one level down, so the
// root's jurisdiction is real, and the three worst values are refused by NAME.
func SandboxRootRefusal(root string) error {
	// The emptiness check reads the RAW argument: filepath.Clean("") is ".",
	// so a canonical first would turn "no value configured" into a refusal
	// about the current directory — the wrong sentence for the wrong defect.
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("sandbox GC: the sandbox root is empty")
	}
	clean := CanonicalPath(root)
	if clean == "" || clean == "." {
		return fmt.Errorf("sandbox GC: the sandbox root %q does not resolve to a directory", root)
	}
	if clean == "/" {
		return fmt.Errorf("sandbox GC: refusing the filesystem root %q as a sandbox root", clean)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && CanonicalPath(home) == clean {
		return fmt.Errorf("sandbox GC: refusing the user's home %q as a sandbox root", clean)
	}
	if profile := gopiProfileDir(); profile != "" && CanonicalPath(profile) == clean {
		return fmt.Errorf("sandbox GC: refusing the gopi profile %q as a sandbox root — that is production state, "+
			"not a sandbox tree", clean)
	}
	return nil
}

// PlanSandboxes reports the verdict for every sandbox directly under root. It
// is read-only (no lock, no delete) — the form `--dry-run` and an operator's
// question both need.
func PlanSandboxes(root string, olderThan time.Duration) ([]SandboxPlan, error) {
	if err := SandboxRootRefusal(root); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no root: nothing to judge, not an error
		}
		return nil, fmt.Errorf("sandbox GC: read %s: %w", root, err)
	}
	if len(entries) > ScanLimit {
		return nil, fmt.Errorf("sandbox GC: %s holds more than %d entries — refusing to sweep an unbounded tree",
			root, ScanLimit)
	}

	now := time.Now()
	// ONE /proc pass for the whole arm (the batch form exists because a
	// per-entry probe costs a process-table walk per candidate).
	holders := ScanHolders()
	self, _ := os.Getwd() //stylegate:ignore an unavailable cwd leaves self empty, which never matches a candidate's parent and so vetoes nothing (every unknown is a KEEP)

	var plans []SandboxPlan
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), SandboxMarkerPrefix) {
			continue // not a sandbox of ours (by name — never by location)
		}
		path := filepath.Join(root, e.Name())
		plans = append(plans, judgeSandbox(path, now, olderThan, holders, self))
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].Path < plans[j].Path })
	return plans, nil
}

// judgeSandbox applies the four-part predicate to ONE sandbox.
func judgeSandbox(path string, now time.Time, olderThan time.Duration, holders HolderScan, self string) SandboxPlan {
	keep := SandboxPlan{Path: path, Keep: true}

	if PathWithin(CanonicalPath(self), CanonicalPath(path)) {
		keep.Reason = SandboxKeepSelfCwd
		return keep
	}

	// ① AGE — the directory's own mtime. Deliberately the mtime and not the
	// marker's created_at: the marker is written once, so a sandbox that has
	// been USED for a week would look a week old and be collected while its
	// owner is still writing in it. mtime moves on use, which is what an age
	// rule must measure.
	stale, err := ScratchEntryStale(path, now, olderThan)
	if err != nil {
		keep.Reason = fmt.Sprintf("age unmeasurable (%v)", err)
		return keep
	}
	if !stale {
		keep.Reason = SandboxKeepFresh
		return keep
	}

	// ② OWNERSHIP — the marker inside the directory.
	marker, markerErr := readSandboxOwner(path)
	switch {
	case markerErr == nil:
		live, certain := sandboxOwnerLive(marker)
		switch {
		case !certain:
			keep.Reason = SandboxKeepUnknownOwner
			return keep
		case live:
			keep.Reason = fmt.Sprintf("%s (pid %d, start ticks %d, cmd %s)",
				SandboxKeepLiveOwner, marker.PID, marker.StartTicks, marker.Cmd)
			return keep
		}
	default:
		// No marker (or an unreadable one): the sandbox cannot prove an owner,
		// so its claim rests on the generic scratch contract alone — aged AND
		// unheld. Reported as its own reason so the operator can see that the
		// absence of a marker, not a proven dead owner, is what allowed it.
		keep.Reason = SandboxKeepMarkerMissing
	}

	// ③ LIVENESS — no live process standing inside it (the /proc cwd|fd|exe
	// scan). A process whose /proc entry cannot be read is SKIPPED by that
	// scan, so an unreadable process can never veto the sweep; the marker arm
	// above is the channel that covers a same-user sibling. The two facts the
	// scan can report — "not looked at" and "a live process is inside" — are
	// separate guards, so neither is read as the other.
	held, scanned := holders.HeldByProcesses(path)
	if !scanned {
		keep.Reason = HolderScanWithheldReason()
		return keep
	}
	if held {
		return SandboxPlan{Path: path, Keep: true, Reason: SandboxKeepHeld}
	}

	// Collectable. The reason names the evidence, so the log line an operator
	// reads says WHY a directory disappeared.
	reason := "aged and unowned"
	if markerErr == nil {
		reason = fmt.Sprintf("aged and unowned (recorded owner pid %d, start ticks %d, is gone)",
			marker.PID, marker.StartTicks)
	}
	return SandboxPlan{Path: path, Keep: false, Reason: reason}
}

// readSandboxOwner loads a sandbox's ownership marker.
func readSandboxOwner(dir string) (sandboxOwnerMarker, error) {
	var m sandboxOwnerMarker
	blob, err := os.ReadFile(filepath.Join(dir, SandboxOwnerFileName))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(blob, &m); err != nil {
		return m, err
	}
	if m.PID <= 0 {
		return m, fmt.Errorf("marker records no pid")
	}
	return m, nil
}

// sandboxOwnerLive reports whether the marker's process incarnation is still
// running, and whether that answer is CERTAIN. A recycled pid fails the
// start-ticks match (that is the whole reason the ticks are recorded); an
// unreadable /proc or a marker without ticks is NOT certain, and an uncertain
// answer never authorises a delete.
func sandboxOwnerLive(m sandboxOwnerMarker) (live, certain bool) {
	if m.StartTicks == 0 {
		return false, false
	}
	pid := fmt.Sprintf("%d", m.PID)
	if !IsPID(pid) {
		return false, false
	}
	ticks, err := ProcStartTime(m.PID)
	if err != nil {
		if os.IsNotExist(err) {
			return false, true // the process is provably gone
		}
		return false, false // EACCES and friends: cannot tell
	}
	return ticks == m.StartTicks, true
}

// SweepSandboxes removes the collectable sandboxes under root and reports how
// many were removed and how many bytes came back. It is IDEMPOTENT (a second
// run finds nothing collectable), BOUNDED (one ReadDir, at most ScanLimit
// entries, root untouched), and SAFE: every verdict it acts on comes from the
// same predicate --dry-run reports.
//
// It NEVER removes root itself — not at any age, not when empty. root is the
// configured container, the same "the declared tree's root is never removed"
// rule the declared-trees arm follows (a sweep that deleted its own root broke
// the next build there; here it would silently relocate every future sandbox).
//
// The plain 3-value shape is the thin wrapper the pre-existing callers keep;
// SweepSandboxesDetailed is the reporting form, and it hands the caller the LOCK
// verdict for the same reason SweepHomeDetailed does (Rule 15: a deferred sweep
// must not look like an empty one).
func SweepSandboxes(root string, olderThan time.Duration) (removed int, bytes int64, err error) {
	removed, bytes, _, err = SweepSandboxesDetailed(root, olderThan)
	return removed, bytes, err
}

func SweepSandboxesDetailed(root string, olderThan time.Duration) (removed int, bytes int64, lock GCLockVerdict, err error) {
	plans, err := PlanSandboxes(root, olderThan)
	if err != nil {
		return 0, 0, GCLockVerdict{}, err
	}
	if len(plans) == 0 {
		return 0, 0, GCLockVerdict{Outcome: GCLockOutcomeAcquired, Reason: "no sandbox plan entries: the lock was not needed"}, nil
	}
	release, v, lerr := AcquireGCLockVerdict(root)
	if lerr != nil {
		return 0, 0, v, fmt.Errorf("sandbox GC: lock %s: %w", root, lerr)
	}
	if !v.Locked {
		// Another sweep owns this tree right now. Not an error: the other sweep
		// is doing the same work (fail-open, like every other arm) — but the
		// verdict says WHO, so the caller's `removed=0` stops being ambiguous.
		return 0, 0, v, nil
	}
	defer release()

	for _, p := range plans {
		if p.Keep {
			continue
		}
		size := DirBytes(p.Path)
		if rerr := os.RemoveAll(p.Path); rerr != nil {
			// Report and continue: one sandbox that cannot be removed must not
			// abort the sweep of the others (Rule 15 — narrated, not swallowed).
			fmt.Fprintf(os.Stderr, "scratch: sandbox GC could not remove %s: %v\n", p.Path, rerr)
			continue
		}
		removed++
		bytes += size
	}
	return removed, bytes, v, nil
}

// DirBytes sums the sizes of the regular files under dir, best effort: the
// reclaimed-bytes figure is operator information, and a file that vanishes
// mid-walk must not fail the sweep that is deleting the tree it lives in.
//
// ONE measurement, not two: the sandbox arm and the worktree-stub arm both report
// this figure in the log line's bytes= field, so a reader comparing the two lines
// is comparing the same number (Rule 14: a shared value has one definition, and
// the consumers here are two arms of the same verb).
//
// (The pre-existing unexported spelling needed no rename: Go allows one exported
// wrapper and one unexported function with different names, and the call site
// above reads unchanged — so the diff stays surgical.)
func DirBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, ierr := d.Info(); ierr == nil {
				total += info.Size()
			}
		}
		return nil
	}) //stylegate:ignore the sweep is best-effort by contract: an unreadable entry is skipped and reported through the callback, and every unknown is a KEEP
	return total
}
