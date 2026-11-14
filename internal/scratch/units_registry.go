// units_registry.go — the SHOT UNIT REGISTRY: the lifetime of a systemd
// transient unit that a writer may never get to reap.
//
// THE DEFECT (measured 2026-09-23 on this host, 13 active units / 4h55m): a
// headless chrome must escape the scratch address-space clamp, and the ONLY
// escape is the systemd USER SERVICE form (`systemd-run --user --unit=… --collect`
// — a transient SCOPE inherits the caller's limits; see launch.go's header).
// That form has a structural consequence: chrome's parent is the user MANAGER,
// not the process that started it, so the unit outlives its creator by design.
// Teardown is therefore a `defer proc.stop()` → `systemctl --user stop <unit>`,
// which runs ONLY on a normal return. A SIGKILLed shot (a test timeout, a
// cancelled run) never reaches its defer: the unit and its ~9 chrome
// descendants stay ACTIVE forever, holding RSS until the host reboots.
//
// SHAPE A (this file) vs SHAPE B (documented, deliberately NOT built).
//
//	Shape A — an EXTERNAL, mechanical reclamation. The unit's lifetime is
//	decoupled from the writer reaching its defer: the creator REGISTERS the
//	unit (its own pid + kernel start ticks + command line, the unit name, the
//	time) the moment it holds the name, and the EXISTING one-shot sweeper
//	stops the registered units whose creator is MEASURABLY gone. The sweep is
//	`gopi tool scratch-gc` (the CLI face a host watchdog already runs) and the
//	already-installed scratch tick (`gopi tool scratch-gc --tick`) — no new daemon, no new
//	tick, no new schedule.
//
//	Shape B — PR_SET_PDEATHSIG on chrome: chrome dies when its PARENT dies.
//	It is IMPOSSIBLE for this path, and adding it would be a half-measure that
//	looks like a fix: the kernel delivers that signal to a process whose
//	parent THREAD exits, and chrome's parent here is systemd (PID 1's user
//	manager), which never dies. The signal would have to be armed IN CHROME —
//	the flag is not inherited across exec by the child... it IS cleared on
//	exec, and the child here cannot be asked to set it (systemd exec's it).
//	Setting it on the systemd-run CLIENT is meaningless: that client is a
//	short-lived process that always exits normally. So Shape B cannot reach
//	the case that leaks, and is recorded here as supplementary ONLY (it would
//	help the ATTACHED launch path, a different, already-reaped case).
//
// THE CONSERVATIVE RULE IS INHERITED, NOT REINVENTED. This package's whole
// contract is "an entry we cannot measure is KEPT" (gc.go's Collectable and
// unheld; see also holder.go). The unit sweep obeys the same rule through the
// SAME liveness implementation: internal/procname's RunnerIdentity — the ONE
// ladder the task ledger uses (taskproc.go's identityLiveness /
// finishCleanupProcessHolderLive). No second probe is written (Rule 14: a
// second ladder drifts, and this drift is fail-open in both directions:
// "unmeasurable" read as "gone" stops a LIVE capture's chrome).
//
// WHY THE REGISTRY IS A DIRECTORY OF RECORDS, NOT A LINE IN ONE FILE:
//
//   - the WRITER is concurrent (several `gopi tool shot` runs at once) and must
//     never corrupt another run's registration — one atomically-written
//     directory per unit gives that for free (no read-modify-write);
//   - the REAPER must remove exactly one unit's registration, which is one
//     RemoveAll of one directory;
//   - a crashed reaper leaves no partial state (the record either exists,
//     complete, or it does not).
//
// The registry directory is NOT a scratch entry: it carries no namespace
// marker, it is not a queue child, so gc.go's Collectable refuses it at any
// age (asserted by TestShotUnitRegistrySurvivesTheHomeSweep). Its lifetime is
// its records'.
package scratch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
)

const (
	// ShotUnitPrefix names the transient unit the capture path starts. It is
	// the SAME value internal/shot starts units with (shot.shotUnitPrefix)
	// and the sweep REFUSES any record that does not carry it, so a hand-edited
	// or foreign record can never ask this code to stop a unit it does not own.
	ShotUnitPrefix = "gopi-shot-"
	// ShotUnitRegistryDirName is the registry's directory leaf under the
	// scratch home. Named as a constant because the writer (internal/shot), the
	// reaper (this package) and the CLI/tick faces all spell it (Rule 1b).
	ShotUnitRegistryDirName = "shot-units"
	// ShotUnitRecordFileName is the record file inside each unit directory.
	ShotUnitRecordFileName = "unit.json"

	// ShotUnitRegistryEnvName overrides the registry ROOT — the testability
	// seam for a caller that must not touch the live tree, exactly as
	// EnvProcRoot is procname's. A NAME with a reader (ShotUnitDir), never a
	// value with none (Rule 21).
	ShotUnitRegistryEnvName = "GOPI_SHOT_UNIT_REGISTRY"

	// shotUnitRecordPerm / shotUnitDirPerm: the record carries a pid and a
	// command line, nothing secret; every mode is the scratch tree's own.
	shotUnitDirPerm    = DirPerm
	shotUnitRecordPerm = FilePerm

	// shotUnitRecordJSONIndent makes the record readable by an operator who is
	// diagnosing a stuck capture by hand — the primary consumer of this file in
	// an incident.
	shotUnitRecordJSONIndent = "  "

	// shotUnitRegistryWriteBit is the "may this process add/remove a
	// registration here" permission bit the start-up probe tests. It is asked
	// only as a PROBE (a diagnosis to report), never as a decision: the sweep's
	// own per-unit handling is unchanged, and a bitset is a cheap, honest
	// approximation of the write attempt that would follow.
	shotUnitRegistryWriteBit = 0o200

	// shotUnitCreatorGone / …Recycled / …Live / …Unmeasurable are the sweep's
	// verdict WORDS. One word per reading, so an operator (and the tests) can
	// never mistake "could not measure" for "gone" (Rule 15a) — the distinction
	// the whole conservative rule rests on.
	shotUnitCreatorGone         = "creator-gone"
	shotUnitCreatorRecycled     = "creator-recycled"
	shotUnitCreatorLive         = "creator-live"
	shotUnitCreatorUnmeasurable = "creator-unmeasurable"

	// ShotUnitReclaimLogFmt is the reclamation's one-line report format: the
	// counts, then one line per unit the arm acted on. Named once (Rule 1b)
	// because every face shares it and the acceptance grep reads it verbatim.
	//
	// `renewed` counts the units this pass STOPPED — the arm's own word for what
	// it did, because the report has to be readable on its own: `stopped` read
	// like a failure, and an operator reading `gopi tool shot`'s output needs to know
	// that "renewed" means a previous capture's unit was taken down at start-up.
	// `denied` counts the units whose stop the HOST refused (systemctl failed) —
	// a separate number because it is a separate fact: those units are still
	// ACTIVE and their registrations are kept for the next pass (Rule 15: a
	// failed reclamation must never render as a successful one).
	ShotUnitReclaimLogFmt = "shot-unit-reclaim role=%s scanned=%d renewed=%d denied=%d kept=%d"
	// ShotUnitRenewedLogFmt / ShotUnitDeniedLogFmt / ShotUnitKeptLogFmt are its
	// per-unit lines. Every unit the pass judged gets exactly one of them, so the
	// counts can always be read back as named evidence.
	ShotUnitRenewedLogFmt = "shot-unit-reclaim role=%s renewed-unit=%s"
	ShotUnitDeniedLogFmt  = "shot-unit-reclaim role=%s denied-unit=%s verdict=%s reason=%q"
	ShotUnitKeptLogFmt    = "shot-unit-reclaim role=%s kept-unit=%s verdict=%s reason=%q"

	// ShotUnitStopFailedReason is the marker appended to a kept record's reason
	// when the STOP itself failed (the unit was reclaimable and systemctl
	// refused). It is a constant because two readers depend on the same spelling:
	// the report splits denials out of the kept records, and the tests assert it.
	ShotUnitStopFailedReason = " — the stop failed, registration kept for the next sweep"

	// ShotUnitWouldStopWord / ShotUnitKeepWord are the dry-run's verdict words.
	// They are constants because the operator's vetting report is a shared
	// vocabulary with the sweep's own log (Rule 1b): "keep" here must mean the
	// same thing as the sweep's kept= count.
	ShotUnitWouldStopWord = "would-stop"
	ShotUnitKeepWord      = "keep"
)

// ShotUnitRecord is one registered transient unit: WHO started it (the
// identity triple of the creating process, in the same shape the task ledger
// records a runner), WHAT unit to stop, and WHEN.
//
// The identity triple is the strong factor order procname states: pid +
// kernel start ticks (the incarnation) decide, and the command line rides
// along as the diagnostic spelling a human reads in an incident and as the
// ONLY comparison factor for a legacy record that carries no ticks.
type ShotUnitRecord struct {
	Unit              string `json:"unit"`
	CreatorPid        int    `json:"creator_pid"`
	CreatorStartTicks int64  `json:"creator_start_ticks,omitempty"`
	CreatorCmd        string `json:"creator_cmd,omitempty"`
	CreatedAt         string `json:"created_at"`
}

// ShotUnitDir is the registry ROOT: $GOPI_SHOT_UNIT_REGISTRY when set (the test
// seam), else <scratch home>/shot-units. It lives inside the scratch tree so it
// inherits the tree's location discipline (never on `/`, Rule 22) and its
// discoverability — while remaining OUTSIDE the home sweep's namespace, so no
// sweep can collect a registration while its unit is still running.
func ShotUnitDir() string {
	if dir := strings.TrimSpace(os.Getenv(ShotUnitRegistryEnvName)); dir != "" {
		return dir
	}
	return filepath.Join(HomeDir(), ShotUnitRegistryDirName)
}

// shotUnitRecordDir is one unit's directory. The unit name is validated against
// the prefix and refused if it is not a bare name — a record is a file on disk
// that a reaper feeds to `systemctl`, so a name carrying a path separator or a
// stray argument is refused rather than joined (Rule 15: never act on an input
// this code cannot vouch for).
func shotUnitRecordDir(dir, unit string) (string, error) {
	if err := ValidateShotUnitName(unit); err != nil {
		return "", err
	}
	return filepath.Join(dir, unit), nil
}

// ValidateShotUnitName reports whether unit is a name this system may stop: it
// carries the capture prefix and is a single path component (no separators, no
// traversal, no leading dash that could be read as a systemctl flag).
func ValidateShotUnitName(unit string) error {
	if unit == "" {
		return errors.New("empty unit name")
	}
	if !strings.HasPrefix(unit, ShotUnitPrefix) {
		return fmt.Errorf("unit %q does not carry the %q prefix — not a capture unit", unit, ShotUnitPrefix)
	}
	if strings.ContainsRune(unit, filepath.Separator) || unit != filepath.Base(unit) {
		return fmt.Errorf("unit %q is not a bare name", unit)
	}
	if strings.HasPrefix(unit, "-") {
		return fmt.Errorf("unit %q starts with a dash", unit)
	}
	return nil
}

// RegisterShotUnit records a unit this process just started, and returns the
// record's path. It is called at the moment the unit name is HELD (before the
// DevTools port is awaited), so a kill anywhere after the unit exists leaves a
// registration behind — a record written after the port appeared would leave
// the leak open for the whole startup window.
//
// Failure is REPORTED and returned, never swallowed (Rule 15b): the caller
// announces it and continues, because losing the registration degrades the
// unit's teardown to the pre-fix behaviour (the defer) rather than breaking the
// capture. A record that cannot be written is one leaked unit, not a failed
// shot.
func RegisterShotUnit(dir, unit string, creatorPid int) (string, error) {
	recordDir, err := shotUnitRecordDir(dir, unit)
	if err != nil {
		return "", err
	}
	if creatorPid <= 0 {
		return "", fmt.Errorf("register %s: creator pid %d is not a process id", unit, creatorPid)
	}
	if err := os.MkdirAll(recordDir, shotUnitDirPerm); err != nil {
		return "", fmt.Errorf("register %s: %w", unit, err)
	}
	rec := ShotUnitRecord{
		Unit:       unit,
		CreatorPid: creatorPid,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	if ticks, err := ProcStartTime(creatorPid); err == nil {
		rec.CreatorStartTicks = int64(ticks)
	}
	rec.CreatorCmd = shotUnitCreatorCmd(creatorPid)
	if rec.CreatorStartTicks == 0 && rec.CreatorCmd == "" {
		// No recorded factor: the sweep would have to answer UNMEASURABLE and
		// keep the unit forever. Say so now, where the cause is (Rule 15: an
		// unmeasured fact must not arrive silently at the reaper).
		return "", fmt.Errorf("register %s: neither pid %d's start ticks nor its command line could be read",
			unit, creatorPid)
	}
	raw, err := json.MarshalIndent(rec, "", shotUnitRecordJSONIndent)
	if err != nil {
		return "", fmt.Errorf("register %s: %w", unit, err)
	}
	path := filepath.Join(recordDir, ShotUnitRecordFileName)
	if err := os.WriteFile(path, append(raw, '\n'), shotUnitRecordPerm); err != nil {
		return "", fmt.Errorf("register %s: %w", unit, err)
	}
	return path, nil
}

// DeregisterShotUnit removes a unit's registration. It is idempotent and never
// fails the caller's teardown: an absent registration is the normal outcome
// when the unit's chrome already exited (and, with --collect, was reaped).
func DeregisterShotUnit(dir, unit string) error {
	recordDir, err := shotUnitRecordDir(dir, unit)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(recordDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deregister %s: %w", unit, err)
	}
	return nil
}

// shotUnitCreatorCmd renders the creating process's command line as text (the
// diagnostic spelling, and the fallback comparison factor). An unreadable
// cmdline is not an error: the ticks are the strong factor.
func shotUnitCreatorCmd(pid int) string {
	argv, ok := procname.Cmdline(procname.DefaultProcRoot, strconv.Itoa(pid))
	if !ok {
		return ""
	}
	return strings.Join(argv, " ")
}

// ReadShotUnitRecord loads one registration file. A malformed record is an
// ERROR, never a default: a record this code cannot vouch for must keep its
// unit, not authorise a stop.
func ReadShotUnitRecord(path string) (ShotUnitRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ShotUnitRecord{}, err
	}
	var rec ShotUnitRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return ShotUnitRecord{}, fmt.Errorf("record %s: %w", path, err)
	}
	if err := ValidateShotUnitName(rec.Unit); err != nil {
		return ShotUnitRecord{}, fmt.Errorf("record %s: %w", path, err)
	}
	if rec.CreatorPid <= 0 {
		return ShotUnitRecord{}, fmt.Errorf("record %s: creator pid %d is not a process id", path, rec.CreatorPid)
	}
	if rec.CreatorStartTicks == 0 && strings.TrimSpace(rec.CreatorCmd) == "" {
		return ShotUnitRecord{}, fmt.Errorf("record %s: states neither the creator's start ticks nor its command line", path)
	}
	return rec, nil
}

// ShotUnitRegistryDenyReason reports why THIS process will probably not be able
// to reclaim the units registered under dir, or "" when nothing is known to be
// against it. It is the probe the two START-UP triggers run before their sweep
// (internal/shot.RenewNotice) so that a whole-registry denial is announced ONCE,
// naming the path, the reason and the channel — instead of leaving an operator
// to infer it from per-unit failure lines they may never see (Rule 15).
//
// It is a PROBE, not a policy, and it decides nothing: the sweep's keep/deny
// decisions are made per registration in ReclaimShotUnits, unchanged. A MISSING
// directory is not a denial — a host that never ran a capture has nothing to
// reclaim and must stay silent. Neither is an empty result a promise: the probe
// answers "is anything known against this process", and returns "" when it is
// not.
func ShotUnitRegistryDenyReason(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		return fmt.Sprintf("the capture unit registry %s cannot be read (%v)", dir, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Sprintf("the capture unit registry %s cannot be inspected (%v)", dir, err)
	}
	if info.Mode().Perm()&shotUnitRegistryWriteBit == 0 {
		return fmt.Sprintf("the capture unit registry %s is not writable by this user (mode %s, %d registration(s) present): "+
			"a stopped unit's registration could not be removed",
			dir, info.Mode().Perm(), len(entries))
	}
	return ""
}

// ShotUnitCreatorVerdict is the ONE liveness read this package makes about a
// registration's creator: the recorded pid + kernel start ticks fed to
// internal/procname's RunnerIdentity — the SAME ladder the task ledger asks
// about its own runners. The verdict word and its reason are both returned, so
// every face (the sweep, the dry-run plan, the operator's report) speaks the
// same vocabulary.
//
// An UNMEASURABLE creator is named as such, never as a death: that is this
// file's conservative contract, and it is the direction that cannot destroy a
// live capture.
func ShotUnitCreatorVerdict(rec ShotUnitRecord, procRoot string) (verdict, reason string) {
	want := procname.Liveness{Pid: strconv.Itoa(rec.CreatorPid)}
	if rec.CreatorStartTicks > 0 {
		want.StartTicks = rec.CreatorStartTicks
	} else {
		want.CmdlineText = rec.CreatorCmd
	}
	outcome, why := procname.RunnerIdentity(procRoot, want)
	switch outcome {
	case procname.OutcomeGone:
		return shotUnitCreatorGone, fmt.Sprintf("creator pid %d is gone", rec.CreatorPid)
	case procname.OutcomeRecycled:
		return shotUnitCreatorRecycled, fmt.Sprintf("creator pid %d was reassigned to another process", rec.CreatorPid)
	case procname.OutcomeLive:
		return shotUnitCreatorLive, fmt.Sprintf("creator pid %d is still running", rec.CreatorPid)
	default:
		return shotUnitCreatorUnmeasurable, fmt.Sprintf("creator pid %d could not be measured (%s) — kept", rec.CreatorPid, why)
	}
}

// ShotUnitPlan is one registration's disposition: what the sweep would do to the
// unit and why. It is the dry-run's unit of report, so an operator can see a
// host's orphaned captures BEFORE letting the sweep stop anything.
type ShotUnitPlan struct {
	Unit    string
	Verdict string
	Reason  string
	Keep    bool
}

// ShotUnitOutcome reports one sweep's work.
//
// Scanned counts the registrations found, Renewed those whose unit was stopped
// and whose registration was removed, Denied those whose unit was reclaimable
// but whose STOP the host refused (their registrations are kept, and they are
// still ACTIVE — the report must never let a denial read as a success), Kept
// those left alone. KeptRecords carries the reason for every record that keeps
// its unit — a live/unmeasurable creator, an UNREADABLE registration, or a
// failed stop — and DeniedRecords the denied ones, so a reader can tell the
// three apart without re-deriving the predicate.
type ShotUnitOutcome struct {
	Scanned       int
	Renewed       int
	Denied        int
	Kept          int
	Unreadable    int
	RenewedUnits  []string
	DeniedRecords []ShotUnitPlan
	KeptRecords   []ShotUnitPlan
}

// PlanShotUnits reports, per registration, what the sweep would decide —
// deleting and stopping nothing. It is the dry-run face of ReclaimShotUnits and
// shares its predicate and its reading rule, so a plan can never promise
// something the sweep would refuse (or the reverse).
func PlanShotUnits(procRoot, dir string) ([]ShotUnitPlan, error) {
	recs, err := shotUnitRegistrations(dir)
	if err != nil {
		return nil, err
	}
	plans := make([]ShotUnitPlan, 0, len(recs))
	for _, r := range recs {
		verdict, reason := shotUnitDisposition(r, procRoot)
		plans = append(plans, ShotUnitPlan{
			Unit:    r.rec.Unit,
			Verdict: verdict,
			Reason:  reason,
			Keep:    shotUnitVerdictKeeps(verdict),
		})
	}
	return plans, nil
}

// shotUnitVerdictKeeps is the ONE decision function: a creator that is live or
// unmeasurable keeps its unit; only a MEASURED gone/recycled creator authorises
// the stop. Both directions are named here so no caller re-derives the policy.
func shotUnitVerdictKeeps(verdict string) bool {
	return verdict != shotUnitCreatorGone && verdict != shotUnitCreatorRecycled
}

// ReclaimShotUnits stops the registered units whose creating process is
// provably gone, and removes each such registration. stop is INJECTED so this
// function holds the policy (verdict + conservative keep + deregistration) while
// the actual teardown stays with the package that owns the systemctl argv
// (internal/shot) — one definition of the stop, no duplicated command line.
//
// A stop that FAILS leaves the registration in place and is REPORTED: the next
// sweep tries again, and the record keeps naming the unit (Rule 15: never a
// silent failure, never a deregistration that hid a live unit).
func ReclaimShotUnits(procRoot, dir string, stop func(unit string) error) (ShotUnitOutcome, error) {
	recs, err := shotUnitRegistrations(dir)
	if err != nil {
		return ShotUnitOutcome{}, err
	}
	var out ShotUnitOutcome
	out.Scanned = len(recs)
	for _, r := range recs {
		verdict, reason := shotUnitDisposition(r, procRoot)
		if shotUnitVerdictKeeps(verdict) {
			out.Kept++
			out.KeptRecords = append(out.KeptRecords, ShotUnitPlan{
				Unit: r.rec.Unit, Verdict: verdict, Reason: reason, Keep: true,
			})
			continue
		}
		if stop == nil {
			return out, fmt.Errorf("reclaim %s: no stop function supplied", r.rec.Unit)
		}
		if err := stop(r.rec.Unit); err != nil {
			// The stop itself failed: the unit is STILL ACTIVE, so this is
			// reported as a DENIAL of its own (never folded into the keeps as a
			// neutral outcome) — the registration stays for the next pass and the
			// record is kept in BOTH lists' vocabulary so neither reading loses it.
			fmt.Fprintf(os.Stderr, "scratch: shot unit %s not stopped (%s): %v\n", r.rec.Unit, reason, err)
			denied := ShotUnitPlan{
				Unit: r.rec.Unit, Verdict: verdict,
				Reason: reason + ShotUnitStopFailedReason,
				Keep:   true,
			}
			out.Denied++
			out.DeniedRecords = append(out.DeniedRecords, denied)
			out.Kept++
			out.KeptRecords = append(out.KeptRecords, denied)
			continue
		}
		if err := DeregisterShotUnit(dir, r.rec.Unit); err != nil {
			// The unit is stopped; the stale registration is not a leak, and
			// the next sweep will stop the (already gone) unit harmlessly.
			fmt.Fprintf(os.Stderr, "scratch: shot unit %s stopped but its registration was not removed: %v\n",
				r.rec.Unit, err)
		}
		out.Renewed++
		out.RenewedUnits = append(out.RenewedUnits, r.rec.Unit)
	}
	return out, nil
}

// registeredShotUnit is one registration on disk: the record it parsed to, or
// the reason it could not be parsed (a registration this code cannot vouch for
// is KEPT and NAMED — Rule 15 — never silently skipped).
type registeredShotUnit struct {
	rec        ShotUnitRecord
	unreadable string
}

// shotUnitDisposition is the ONE reading of one registration: an unreadable
// registration keeps its unit with the reason that it could not be read (Rule
// 15a: "could not read" is a THIRD answer, not "gone"), and a readable one is
// judged by the creator's incarnation through procname's single ladder.
func shotUnitDisposition(r registeredShotUnit, procRoot string) (verdict, reason string) {
	if r.unreadable != "" {
		return ShotUnitRegistryUnreadable, r.unreadable
	}
	return ShotUnitCreatorVerdict(r.rec, procRoot)
}

// shotUnitRegistrations reads every registration under dir. A directory that
// does not exist is EMPTY, not an error (no capture has run on this host —
// mirroring SweepHome's contract). A directory without a readable record is
// reported as UNREADABLE, with a synthetic record-shaped entry so the caller
// still names it and keeps it.
func shotUnitRegistrations(dir string) ([]registeredShotUnit, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read shot unit registry %s: %w", dir, err)
	}
	var out []registeredShotUnit
	for _, entry := range entries {
		if !entry.IsDir() {
			continue // a stray file in our own directory is not a registration
		}
		rec, err := ReadShotUnitRecord(filepath.Join(dir, entry.Name(), ShotUnitRecordFileName))
		if err != nil {
			// Unreadable registrations are KEPT and NAMED (Rule 15). They are
			// represented by a record whose pid is 0, which the verdict maps to
			// UNMEASURABLE — the keep direction, with the reason spelled out.
			fmt.Fprintf(os.Stderr, "scratch: shot unit registration %s is unreadable and is kept: %v\n",
				entry.Name(), err)
			out = append(out, registeredShotUnit{
				rec:        ShotUnitRecord{Unit: entry.Name()},
				unreadable: fmt.Sprintf("registration %s could not be read (%v) — kept", entry.Name(), err),
			})
			continue
		}
		out = append(out, registeredShotUnit{rec: rec})
	}
	return out, nil
}

// ShotUnitRegistryUnreadable is the verdict word used for a registration this
// code could not read: it is kept, and the word says exactly why.
const ShotUnitRegistryUnreadable = "registration-unreadable"
