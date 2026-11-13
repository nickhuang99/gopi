package scratch

// suite_package_lock.go — the ONE cross-run exclusion for a whole test suite of
// one package: at most one `go test ./<pkg>` per host at a time.
//
// THE DEFECT THIS CLOSES. Every suite already gets a PER-RUN scratch root
// (internal/testutil's PinSuiteTempRoot → os.MkdirTemp — unique by construction),
// so it is tempting to conclude two concurrent runs are isolated. They are not,
// because the root is not the only shared thing:
//
//   - the scratch HOME itself is ONE directory per host: every run's GC scan and
//     removal pass walks it, and its queue subtree is deliberately not per-run;
//   - the operator profile's settings document and append-only ledger, reachable
//     through anything a suite does not pin;
//   - the machine's TCP surface and process table, which the web/voice arms
//     exercise with real listeners and real child processes.
//
// Measured before this file existed: with a sibling run of the same package in
// flight the target suite went quiet (the progress watcher printed NO PROGRESS);
// with no sibling, three consecutive whole-package runs printed it zero times.
//
// WHY NOT RAISE THE WATCHDOG, WHY NOT `-p 1`. Both are the documented non-answers:
// raising runNoProgressStall hides a stall instead of removing it, and `-p 1` only
// makes the collision rarer while removing the parallelism the suite is sized for.
// Neither makes the violation SAY ANYTHING (Rule 15), which is the property this
// file exists for.
//
// WHY THIS IS NOT A NEW MECHANISM (Rule 35, Rule 14). It is the scratch tree's OWN
// two primitives, unchanged:
//
//	the ADVISORY LOCK   a directory created with os.Mkdir — the kernel decides who
//	                    wins (EEXIST), never a check-then-act sequence
//	the HOLDER RECORD   the .holder file this tree already writes (owner_record.go),
//	                    judged by OwnerVerdictFor's ONE ladder: live / gone /
//	                    recycled / unmeasurable
//
// So a lock abandoned by a killed run is broken by the SAME judgement the sweep
// already applies to every other entry, a recycled pid is decided rather than
// guessed, and an unmeasurable owner is never collected (unknown is never a yes).
// No new dependency: nothing scheduled, nothing resident, and the entry lives in
// the scratch home under a name the sweep explicitly excludes (gc.go's
// EntryKeepPackageLock — a package lock directory is never collected at any age).

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nick/gopi/internal/procname"
)

const (
	// SuitePackageLockWorkDirKey is the lock record's diagnostic field carrying the
	// directory the holder was working in. It is what lets a refusal name a SIBLING
	// RUN by the tree an operator recognises, rather than by a bare pid. The record
	// is line-oriented and its parser ignores keys it does not know, so this field
	// is readable by this build and harmless to an older one.
	SuitePackageLockWorkDirKey = "workdir"

	// SuitePackageLockAcquiredAtKey is the record's other diagnostic field: the
	// instant the holder took the lock (RFC3339, UTC). A sibling operator reads it to
	// judge "is that run nearly done" — which a bare pid cannot answer.
	SuitePackageLockAcquiredAtKey = "acquired_at"

	// suitePackageLockBreakingRetries is how many times the take is retried after a
	// lock whose recorded owner is provably dead has been broken. ONE is
	// deliberate: a second EEXIST means a third process created the lock in that
	// window — a live competitor, which retrying again would only spin on.
	suitePackageLockBreakingRetries = 1

	// suitePackageLockOffEnvName is the documented operator escape: "0" means take
	// no package lock, for an operator who has deliberately accepted the overlap.
	// Read in ONE place (suitePackageLockEnabled) — Rule 2/21.
	suitePackageLockOffEnvName = "GOPI_SUITE_PACKAGE_LOCK"

	// suitePackageAncestorWalkLimit bounds the parent-chain walk that recognises a
	// suite started BY a suite. The chain is short; the limit keeps a corrupt
	// procfs from turning the refusal path into a loop.
	suitePackageAncestorWalkLimit = 64
)

// The words a take is reported under (Rule 1b/13: the caller's exit-code decision,
// the refusal text and the tests all quote them).
const (
	// SuitePackageLockAcquired: this process holds the package for its whole life.
	SuitePackageLockAcquired = "acquired"
	// SuitePackageLockNested: the holder is an ANCESTOR of this process — a suite
	// started BY a suite (a gate that spawns `go test ./cmd/agent` as a child), not
	// a sibling run. It proceeds without taking a second lock.
	SuitePackageLockNested = "nested-suite"
	// SuitePackageLockOff: the operator's escape is engaged; the run proceeds and
	// says so.
	SuitePackageLockOff = "disabled-by-env"

	// SuitePackageLockBusyLive: the recorded owner is still that live process.
	SuitePackageLockBusyLive = "another run of this package is in flight"
	// SuitePackageLockBusyUnmeasured: the kernel would not answer the identity
	// question, and the lock exists. The run is refused — an unmeasured sibling is
	// never a licence to run beside it (Rule 15a).
	SuitePackageLockBusyUnmeasured = "the package lock exists and its recorded holder could not be measured"
)

// SuitePackageLockOffEnvName is the escape's name, exported so a refusal's FIX line
// names the knob through its single definition (Rule 1b/13).
const SuitePackageLockOffEnvName = suitePackageLockOffEnvName

// SuitePackageLockEnabled reports whether the package lock is in force. It is the ONE
// reader of the escape (Rule 2/21): the default is ON, so the escape must be asked
// for explicitly.
func SuitePackageLockEnabled() bool {
	return strings.TrimSpace(os.Getenv(suitePackageLockOffEnvName)) != "0"
}

// IsSuitePackageLockEntry reports whether a scratch-home entry name belongs to this
// mechanism's namespace — the predicate the sweep excludes by (gc.go's
// EntryKeepPackageLock) and a caller may use to recognise the family.
func IsSuitePackageLockEntry(name string) bool {
	return strings.HasPrefix(name, SuitePackageLockPrefix)
}

// SuitePackageLock is a taken (or deliberately not taken) package lock.
type SuitePackageLock struct {
	// Dir is the lock directory this take addressed.
	Dir string
	// Outcome is which take happened: SuitePackageLockAcquired, …Nested, …Off.
	Outcome string
	// Held reports whether this process actually holds the lock.
	Held bool
}

// SuitePackageBusyError is the refusal: the lock exists and its recorded owner is
// not an ancestor of this process. It carries every fact a human needs — the
// address, the holder's pid, when it started and the tree it works in — so the
// caller renders a sentence without a second investigation.
type SuitePackageBusyError struct {
	// Dir is the lock directory that could not be taken.
	Dir string
	// Outcome is which kind of holder it is (one of the Busy* words).
	Outcome string
	// Evidence is the rendered holder record, with absent fields named absent
	// rather than printed as zeros (Rule 15a).
	Evidence string
	// HolderPid is the recorded pid (0 when none was recorded).
	HolderPid int
}

// Error renders the refusal: the outcome word, the address, and the holder.
func (e *SuitePackageBusyError) Error() string {
	return fmt.Sprintf("%s: %s (%s)", e.Outcome, e.Dir, e.Evidence)
}

// SuitePackageLockDir is the ONE definition of a package's lock address under a
// scratch home: <home>/suite-package-<suite>. A caller that spells the name itself
// is a second definition of the address two runs must agree on (Rule 13).
func SuitePackageLockDir(home, suite string) string {
	name := suitePackageLockElem(suite)
	if name == "" {
		name = "unnamed"
	}
	return filepath.Join(strings.TrimSpace(home), SuitePackageLockPrefix+name)
}

// suitePackageLockElem keeps a suite name a single, stable path element: a name
// carrying a separator would otherwise put the lock where neither the sweep nor a
// sibling run looks (and two packages could silently share one lock).
func suitePackageLockElem(suite string) string {
	var sb strings.Builder
	for _, r := range suite {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			sb.WriteRune(r)
		default:
			sb.WriteByte('_')
		}
	}
	return sb.String()
}

// AcquireSuitePackageLock takes the per-package cross-run lock under home, or
// returns *SuitePackageBusyError naming the holder.
//
// Outcomes, in the order they are decided:
//
//	the lock dir was created          → Acquired, this process holds it
//	the recorded holder is an ANCESTOR → Nested, no second lock (a suite started by
//	                                    a suite is not a sibling RUN)
//	the holder is provably gone       → broken, and retried once (the ladder's
//	                                    own judgement, not an age heuristic)
//	anything else                     → *SuitePackageBusyError, and the caller
//	                                    refuses with its own exit code
//
// workDir is DIAGNOSTIC (it renders into a refusal); an empty value is recorded as
// absent, never invented.
func AcquireSuitePackageLock(home, suite, workDir string) (*SuitePackageLock, error) {
	dir := SuitePackageLockDir(home, suite)
	if !SuitePackageLockEnabled() {
		// The operator asked for this. The caller ANNOUNCES it (Rule 15: a downgrade
		// is never silent), which is why this outcome is a value and not a silent
		// "success".
		return &SuitePackageLock{Dir: dir, Outcome: SuitePackageLockOff}, nil
	}
	for attempt := 0; ; attempt++ {
		mkErr := os.Mkdir(dir, DirPerm)
		if mkErr == nil {
			if err := writeSuitePackageLockRecord(dir, workDir); err != nil {
				// A lock nobody can attribute is not usable for the diagnosis it
				// exists for, so the take is released rather than half-done — with
				// the removal error reported alongside (Rule 15).
				if rmErr := os.RemoveAll(dir); rmErr != nil {
					return nil, fmt.Errorf("scratch: package lock %q: %w; removing it: %w", dir, err, rmErr)
				}
				return nil, fmt.Errorf("scratch: package lock %q: %w", dir, err)
			}
			return &SuitePackageLock{Dir: dir, Outcome: SuitePackageLockAcquired, Held: true}, nil
		}
		if !os.IsExist(mkErr) {
			// Not "somebody else holds it": the home cannot be written. That is a
			// fact about this host, and it must never be reported as a sibling
			// (Rule 15).
			return nil, fmt.Errorf("scratch: package lock %q: %w", dir, mkErr)
		}
		verdict := OwnerVerdictFor(dir)
		if verdict.Registered && suitePackageHolderIsAncestor(verdict) {
			return &SuitePackageLock{Dir: dir, Outcome: SuitePackageLockNested}, nil
		}
		if verdict.Collectable {
			// GONE or RECYCLED: the recorded owner is not the process that holds
			// this lock, so it is abandoned. Break it and retry once.
			if attempt < suitePackageLockBreakingRetries {
				if rmErr := os.RemoveAll(dir); rmErr != nil {
					return nil, fmt.Errorf("scratch: abandoned package lock %q could not be broken: %w", dir, rmErr)
				}
				continue
			}
		}
		outcome := SuitePackageLockBusyLive
		if verdict.Outcome == procname.OutcomeUnmeasurable || !verdict.Registered {
			outcome = SuitePackageLockBusyUnmeasured
		}
		return nil, &SuitePackageBusyError{
			Dir:       dir,
			Outcome:   outcome,
			Evidence:  suitePackageHolderEvidence(dir, verdict),
			HolderPid: suitePackageHolderPid(verdict),
		}
	}
}

// suitePackageHolderPid is the recorded holder's pid (0 when the record does not
// state one — Rule 15a: an unmeasured value never reads as a value).
func suitePackageHolderPid(v OwnerVerdict) int {
	if !v.Registered {
		return 0
	}
	return v.Record.Pid
}

// suitePackageHolderIsAncestor reports whether the recorded holder is this process
// or one of its ancestors.
//
// THIS IS THE ONE DECISION that separates "a sibling run" (refuse) from "a suite
// started BY a suite" (proceed): a gate inside the suite spawns
// `go test ./cmd/agent` as a CHILD, and refusing that child would make every such
// gate unable to run. The walk reads the kernel's own parent field, so "the holder
// is my parent" is a reading, not a guess. An unreadable chain answers false —
// which refuses, because running beside a real sibling is the defect while
// refusing a nested suite is at worst a noisy re-run (Rule 15a).
func suitePackageHolderIsAncestor(v OwnerVerdict) bool {
	holder := suitePackageHolderPid(v)
	if holder <= 0 {
		return false
	}
	pid := os.Getpid()
	for i := 0; i < suitePackageAncestorWalkLimit; i++ {
		if pid == holder {
			return true
		}
		if pid <= 1 {
			return false
		}
		line, ok := procname.StatFieldString(procname.StatStatPath(procname.DefaultProcRoot, strconv.Itoa(pid)), procname.StatFieldParent)
		if !ok {
			return false
		}
		parent, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || parent <= 0 || parent == pid {
			return false
		}
		pid = parent
	}
	return false
}

// suitePackageHolderEvidence renders every field the record states, naming absent
// fields as absent rather than printing a zero (Rule 15a).
func suitePackageHolderEvidence(dir string, v OwnerVerdict) string {
	if !v.Registered {
		return fmt.Sprintf("no holder record in the lock directory (%s)", v.Reason)
	}
	fields := SuitePackageLockRecordFields(dir)
	rec := v.Record
	parts := make([]string, 0, 5)
	if rec.Pid != 0 {
		parts = append(parts, fmt.Sprintf("pid %d", rec.Pid))
	}
	if at := fields[SuitePackageLockAcquiredAtKey]; at != "" {
		parts = append(parts, "started "+at)
	} else {
		parts = append(parts, "started (not recorded)")
	}
	if rec.Start != 0 {
		parts = append(parts, fmt.Sprintf("startticks %d", rec.Start))
	}
	if wd := fields[SuitePackageLockWorkDirKey]; wd != "" {
		parts = append(parts, fmt.Sprintf("directory %s", wd))
	} else {
		parts = append(parts, "directory (not recorded)")
	}
	parts = append(parts, "verdict "+string(v.Outcome))
	return strings.Join(parts, ", ")
}

// SuitePackageLockRecordFields reads the lock record's key=value lines into a map.
// It is deliberately separate from ParseOwnerRecord: that one is the LADDER's input
// (and fails on a record it cannot parse), while these are the DIAGNOSTIC fields a
// refusal displays — a field this build does not recognise, or one an older build
// wrote, must never turn a readable record into an unreadable one.
func SuitePackageLockRecordFields(dir string) map[string]string {
	fields := map[string]string{}
	raw, err := os.ReadFile(filepath.Join(dir, HolderFileName))
	if err != nil {
		return fields
	}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return fields
}

// writeSuitePackageLockRecord writes the holder registration: the identity fields
// from the ONE writer (OwnerRecordText — pid, kernel start ticks, command line) plus
// this lock's diagnostic working directory.
func writeSuitePackageLockRecord(dir, workDir string) error {
	body, err := OwnerRecordText(os.Getpid())
	if err != nil {
		return fmt.Errorf("ownership record: %w", err)
	}
	body += fmt.Sprintf("%s=%s\n", SuitePackageLockAcquiredAtKey, time.Now().UTC().Format(time.RFC3339))
	if workDir = strings.TrimSpace(workDir); workDir != "" {
		body += fmt.Sprintf("%s=%s\n", SuitePackageLockWorkDirKey, strings.NewReplacer("\n", " ", "\r", " ").Replace(workDir))
	}
	if err := os.WriteFile(filepath.Join(dir, HolderFileName), []byte(body), FilePerm); err != nil {
		return fmt.Errorf("write %s: %w", HolderFileName, err)
	}
	return nil
}

// ReleaseSuitePackageLock frees the package for the next run. Safe on nil and on a
// take that holds nothing (the nested path), so a caller can defer it
// unconditionally.
//
// The DIRECTORY is removed here — deliberately, and unlike a flock lock file: this
// lock's identity IS the directory's existence (os.Mkdir is the whole verdict), so
// removal is the release. A crash between create and remove leaves the entry behind
// and the holder record decides it on the next take (gone/recycled → broken), which
// is the same judgement every other entry in this tree gets.
func (l *SuitePackageLock) Release() error {
	if l == nil || !l.Held {
		return nil
	}
	l.Held = false
	if err := os.RemoveAll(l.Dir); err != nil {
		return fmt.Errorf("scratch: release package lock %q: %w", l.Dir, err)
	}
	return nil
}
