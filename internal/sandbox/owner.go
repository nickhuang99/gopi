package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/scratch"
)

// The ownership marker — how a sandbox becomes reclaimable WITHOUT anyone
// remembering it exists.
//
// WHY A FILE INSIDE THE DIRECTORY, AND NOT A REGISTRY: a registry is a second
// thing to keep in step (and the thing that is missing exactly when a run is
// killed mid-flight). A marker that travels with the directory means the
// reclaimer's whole input is the directory it is standing in — no ledger read,
// no daemon, no cleanup hook that a SIGKILL would skip (`t.Cleanup` never runs
// on SIGTERM/SIGKILL; that is the failure this design exists to survive).
//
// WHAT MAKES THE VERDICT SAFE is the (pid, start ticks) PAIR. A pid alone is
// recycled within minutes on a busy host, so "pid 4711 is alive" proves nothing
// about whether 4711 is still the process that made this sandbox. The kernel's
// start time for that pid (field 22 of /proc/<pid>/stat, in clock ticks since
// boot) is stable for the life of the process and different for its successor —
// so a recycled pid FAILS the match and the sandbox is treated as unowned
// (memory #2633: an unverifiable liveness claim must never authorise a delete,
// and a verifiable one must not be defeated by pid reuse).

// Owner is the content of `.sandbox-owner.json`.
type Owner struct {
	// TaskID is the ledger row that made the sandbox, when there was one.
	TaskID string `json:"task_id,omitempty"`
	// Label is the sandbox's caller-supplied label (the middle of its name),
	// kept here so the marker alone says what the sandbox was for.
	Label string `json:"label,omitempty"`
	// PID is the process that created it.
	PID int `json:"pid"`
	// StartTicks is that pid's kernel start time at creation — the field that
	// makes the pid a verifiable identity rather than a reusable number.
	StartTicks uint64 `json:"start_ticks"`
	// CreatedAt is when the sandbox was materialised (RFC3339, UTC). The
	// reclaimer's AGE arm reads the directory's own age instead, so this field
	// is evidence for a human, not a threshold.
	CreatedAt string `json:"created_at"`
	// Cmd is the creator's argv0 basename — evidence, never a verdict input.
	Cmd string `json:"cmd,omitempty"`
	// Version is the marker's schema version, so a future field can be added
	// without a reader having to guess which shape it is holding.
	Version int `json:"version"`
}

// ownerMarkerVersion is the schema version this build writes.
const ownerMarkerVersion = 1

// newOwner builds the marker for a sandbox about to be materialised.
func newOwner(dir string, opts Options) Owner {
	pid := os.Getpid()
	owner := Owner{
		TaskID:    opts.TaskID,
		Label:     sanitizeLabel(opts.Label),
		PID:       pid,
		CreatedAt: opts.now().UTC().Format(time.RFC3339),
		Cmd:       filepath.Base(os.Args[0]),
		Version:   ownerMarkerVersion,
	}
	if ticks, err := scratch.ProcStartTime(pid); err == nil {
		owner.StartTicks = ticks
	}
	// The creator's argv is not always the identifying one (a `go test` binary
	// runs the suite), so fall back to what the process actually IS.
	if owner.Cmd == "" || owner.Cmd == "." {
		if exe, err := os.Executable(); err == nil {
			owner.Cmd = filepath.Base(exe)
		}
	}
	return owner
}

// now is the marker's clock: injectable so a test can pin CreatedAt without
// waiting, and named so `time.Now()` has one site in this package.
func (o Options) now() time.Time {
	if !o.Now.IsZero() {
		return o.Now
	}
	return time.Now()
}

// writeOwner persists the marker, replacing any previous one — a re-run of the
// initializer must record the CURRENT owner, and a leftover marker naming a
// dead pid is exactly the stale evidence the reclaimer must not read.
func writeOwner(dir string, owner Owner) error {
	blob, err := json.MarshalIndent(owner, "", "  ")
	if err != nil {
		return fmt.Errorf("sandbox: encode the ownership marker: %w", err)
	}
	blob = append(blob, '\n')
	p := filepath.Join(dir, config.SandboxOwnerFileName)
	if err := os.WriteFile(p, blob, sandboxMarkerPerm); err != nil {
		return fmt.Errorf("sandbox: write the ownership marker %s: %w", p, err)
	}
	return nil
}

// ReadOwner reads a sandbox's ownership marker. A missing or unparseable
// marker is an ERROR, not a default: "no marker" and "an owner who is gone" are
// different facts, and a reclaimer that conflates them deletes the wrong thing.
// The caller decides what an absent marker means (scratch.SweepSandboxes treats
// it as "not ours to judge" and keeps the directory unless it is aged).
func ReadOwner(dir string) (Owner, error) {
	var owner Owner
	p := filepath.Join(dir, config.SandboxOwnerFileName)
	blob, err := os.ReadFile(p)
	if err != nil {
		return owner, fmt.Errorf("sandbox: read the ownership marker %s: %w", p, err)
	}
	if err := json.Unmarshal(blob, &owner); err != nil {
		return owner, fmt.Errorf("sandbox: parse the ownership marker %s: %w", p, err)
	}
	return owner, nil
}

// ThisProcess reports whether the marker's recorded incarnation is THIS
// process. It is the property that separates the two meanings of "the sandbox
// for this label": a caller asking AGAIN (idempotence — hand the directory
// back) and a DIFFERENT live run standing in it (never hand it out: two callers
// would share one private profile, and the first one's cleanup deletes the
// second one's ground).
//
// An unverifiable identity is NOT this process: offering a directory we cannot
// prove is ours is the defect, declining to is not (the same one-way rule as
// Live's ok=false).
func (o Owner) ThisProcess() bool {
	if o.PID <= 0 || o.PID != os.Getpid() {
		return false
	}
	ticks, err := scratch.ProcStartTime(o.PID)
	if err != nil {
		return false
	}
	if o.StartTicks == 0 {
		// Written without ticks (an old build, an unreadable /proc at creation
		// time): identity cannot be established either way.
		return false
	}
	return ticks == o.StartTicks
}

// Live reports whether the marker's recorded process incarnation is still
// running on this host: the pid must exist AND its kernel start ticks must
// match. A pid that has been recycled fails the match, and an unreadable
// /proc entry is reported as NOT live but with ok=false — the caller must treat
// "I could not tell" as a KEEP, never as a delete (the conservative one-way
// rule; see scratch.SweepSandboxes, which applies it).
//
// This is the marker's OWN incarnation check. It does not replace the
// /proc-inside scan (scratch.HeldByProcesses) that the reclaimer also applies:
// the two answer different questions — "is the process that made this still
// running?" and "is ANY live process standing inside it?" — and the reclaimer
// keeps the sandbox when either answer is yes.
func (o Owner) Live() (live bool, ok bool) {
	if o.PID <= 0 {
		return false, false
	}
	if !scratch.IsPID(fmt.Sprintf("%d", o.PID)) {
		return false, false
	}
	ticks, err := scratch.ProcStartTime(o.PID)
	if err != nil {
		// ENOENT (the process is gone) is a RELIABLE "not live"; anything else
		// (EACCES, an unreadable /proc) is "cannot tell".
		if os.IsNotExist(err) {
			return false, true
		}
		return false, false
	}
	if o.StartTicks == 0 {
		// A marker written without ticks (an old build, or a /proc that could
		// not be read at creation time) cannot prove identity either way.
		return true, false
	}
	return ticks == o.StartTicks, true
}
