package scratch

// Writer-owned scratch lifetime: the scratch tree is bounded by the processes
// that CREATE it, not only by a clock.
//
// THE DEFECT THIS FILE CLOSES. A per-run scratch root was collectable only when
// it was BOTH unheld AND older than the stale window (6h by default). A run that
// is SIGKILLed leaves a root with NO live process anywhere near it and an mtime
// of the moment it died — so the residue is invisible to the sweep for the whole
// window: the sweep reports `removed=0` while the tree grows. The 2026-09-18
// health report measured exactly that (137 entries, 92 older than an hour, only
// 3 of 175 candidates judged collectable) — the POLICY talking, not the
// mechanism.
//
// THE REPLACEMENT. Every producer of a per-run root REGISTERS an owner record at
// creation time — pid + kernel start ticks + the command line — and the sweep then
// answers the exact question: is the process this record names still, right now,
// the same live process?
//
//	no process at the recorded pid            → GONE       → collect, at any age
//	pid alive, kernel start ticks differ      → RECYCLED   → collect, at any age
//	pid alive, every recorded factor agrees   → LIVE       → keep, at any age
//	the kernel would not answer               → UNKNOWN    → keep (Rule 15a: an
//	                                                        unmeasured fact is
//	                                                        never a death)
//
// NOTHING HERE IS A SECOND IDENTITY IMPLEMENTATION. The judgement is
// procname.RunnerIdentity — the ONE ladder this repository already owns (the tool
// layer routes its pid+start-tick records through it, and
// task_finish_cleanup.go's finishCleanupProcessHolderLive calls the same
// procname.RunnerLive). The registration rides the EXISTING .holder channel
// (holder.go), and the reader is the EXISTING collector, so there is still exactly
// ONE liveness verdict in the system. Both existing delete guards stay in place:
// the /proc cwd|fd|exe|maps scan (DirHolder) still runs, and an entry whose holder
// probe errored is still KEPT.
//
// WHAT THE SWEEP REPORTS. `gopi tool scratch-gc --explain` names every candidate with
// its verdict — the recorded owner it judged (pid + start ticks), the outcome
// (gone/recycled/live/unmeasurable) and the one sentence that decided it — so a
// zero (or a non-zero) removal count is a readable statement rather than a count
// with no evidence behind it. NO new trigger rides on this: the one-shot verb and
// the existing resident tick are the only faces, and the age window is still the
// operator's (DefaultStaleAge / GCStaleAgeEnvName).

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
	// HolderKeyCmd is the .holder field carrying the owner's COMMAND LINE — the
	// identity factor that lets the ladder decide pid reuse even for a record
	// that states no incarnation. The record is line-oriented, so a command line
	// carrying a newline is flattened on write (ownerRecordCmd) rather than
	// allowed to split the record.
	HolderKeyCmd = "cmd"

	// OwnerRecordVersionKey names the record's own version, so a reader can tell
	// a v1 record (pid + start ticks, written before this field existed) from a
	// v2 one without guessing from the fields present.
	OwnerRecordVersionKey = "v"
	// OwnerRecordVersion is the version every writer in this package stamps.
	OwnerRecordVersion = 2
)

// The sentence a candidate's owner judgement carries. They are constants because
// the CLI verdict line, the tests and the resident tick's log all quote them
// (Rule 1b/13: a reason typed twice drifts into a reason nobody can grep).
const (
	// OwnerReasonUnregistered: no .holder at all — this is the WRITER's
	// obligation, and its absence is why the age window still exists.
	OwnerReasonUnregistered = "no owner record — nothing recorded an incarnation, so only the age window can bound it"
	// OwnerReasonLive: the recorded process is still the process at that pid.
	OwnerReasonLive = "the recorded owner is live"
	// OwnerReasonGone: the kernel has no such process any more.
	OwnerReasonGone = "the recorded owner is gone"
	// OwnerReasonRecycled: the pid is alive but it is a different process.
	OwnerReasonRecycled = "the recorded owner's pid was recycled by the kernel"
	// OwnerReasonUnreadable: a record exists but could not be parsed. An
	// unreadable registration is NEVER evidence of death (Rule 15).
	OwnerReasonUnreadable = "the owner record could not be read — unknown is never a delete"
	// OwnerReasonUnverifiable: the kernel would not answer the factor the record
	// states (no procfs, an unparsable stat line, an unreadable cmdline).
	OwnerReasonUnverifiable = "owner unverifiable — unknown is never a delete"
)

// OwnerRecord is the parsed .holder registration: who created this scratch root.
type OwnerRecord struct {
	Pid   int
	Start uint64
	Cmd   string
	// Version is OwnerRecordVersionKey's value (0 for a record written before
	// the field existed).
	Version int
}

// ParseOwnerRecord reads the line-oriented "key=value" registration. A missing
// pid is an error; a missing incarnation or command line is NOT — the ladder
// compares only the factors the record states, and a record stating none is
// reported UNMEASURABLE rather than dead.
//
// It is the ONE reader of the .holder format: ParseHolder (the incarnation-only
// face every older caller uses) is defined on top of it, so a field added here
// cannot be understood by one face and not the other (Rule 14).
func ParseOwnerRecord(raw string) (OwnerRecord, error) {
	var rec OwnerRecord
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch key {
		case HolderKeyPID:
			n, err := strconv.Atoi(value)
			if err != nil {
				return OwnerRecord{}, fmt.Errorf("pid field: %w", err)
			}
			rec.Pid = n
		case HolderKeyStart:
			n, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return OwnerRecord{}, fmt.Errorf("start field: %w", err)
			}
			rec.Start = n
		case HolderKeyCmd:
			rec.Cmd = value
		case OwnerRecordVersionKey:
			n, err := strconv.Atoi(value)
			if err != nil {
				return OwnerRecord{}, fmt.Errorf("version field: %w", err)
			}
			rec.Version = n
		}
	}
	if rec.Pid == 0 {
		return OwnerRecord{}, fmt.Errorf("no %s field", HolderKeyPID)
	}
	return rec, nil
}

// ownerRecordCmd flattens an argv vector into the record's ONE field: argv joined
// the way procfs reports a command line, with any newline replaced — the record is
// line-oriented, and an argv carrying a newline must not be able to split it (and
// thereby hide the fields after it).
func ownerRecordCmd(argv []string) string {
	joined := strings.Join(argv, " ")
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(joined)
}

// OwnerRecordText renders the registration body for a live process, reading its
// own incarnation and command line from /proc — the ONE writer's spelling, so a
// v1 reader and a v2 writer agree on the format by construction.
//
// A command line the kernel will not hand over is NOT an error: the record is
// still worth writing with its two remaining factors (the incarnation is the
// strong one), and the ladder treats a record that states no cmdline correctly.
func OwnerRecordText(pid int) (string, error) {
	start, err := ProcStartTime(pid)
	if err != nil {
		return "", err
	}
	body := fmt.Sprintf("%s=%d\n%s=%d\n%s=%d\n",
		OwnerRecordVersionKey, OwnerRecordVersion, HolderKeyPID, pid, HolderKeyStart, start)
	if argv, ok := procname.Cmdline(ProcRootDir, strconv.Itoa(pid)); ok {
		body += fmt.Sprintf("%s=%s\n", HolderKeyCmd, ownerRecordCmd(argv))
	}
	return body, nil
}

// WriteOwnerRecord registers pid's identity inside root. It RETURNS the error
// rather than printing it, so a caller decides (RegisterHolder reports it, a test
// asserts it).
func WriteOwnerRecord(root string, pid int) error {
	body, err := OwnerRecordText(pid)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, HolderFileName), []byte(body), FilePerm)
}

// OwnerRecorded reports whether root carries an owner registration at all. It is
// the question a REPORT asks ("was this root judgeable by its writer?"), never a
// delete authorisation on its own.
func OwnerRecorded(root string) bool {
	_, err := os.Stat(filepath.Join(root, HolderFileName))
	return err == nil
}

// OwnerVerdict is the judged disposition of one root's recorded owner: the outcome
// the identity ladder returned, whether that outcome authorises a delete, and the
// sentence that decided it (carried verbatim into the report).
type OwnerVerdict struct {
	// Outcome is the ladder's answer, verbatim (procname.Outcome).
	Outcome procname.Outcome
	// Registered reports whether a record existed at all. A verdict with
	// Registered=false judged nothing: it is the statement "no writer recorded an
	// owner", and no delete may rest on it.
	Registered bool
	// Collectable is true ONLY for a PROVEN-dead owner — gone or recycled. An
	// unmeasurable answer is never collectable (Rule 15a), and an unregistered
	// root is not collectable HERE (the age window is what bounds it, see
	// OwnerKeeps).
	Collectable bool
	// Reason is the evidence: which recorded factors were compared and what the
	// kernel said.
	Reason string
	// Record is the parsed registration (zero for an unregistered root).
	Record OwnerRecord
}

// OwnerVerdictFor judges the owner record root carries for itself. It is the ONE
// writer-owned liveness verdict on the scratch tree: every arm that judges a
// candidate root asks this function, so there is one answer per root and no
// second probe to drift from it.
//
// The order of the branches is the contract. An UNREADABLE record and an
// UNMEASURABLE probe both land on "keep", because the two failure directions are
// not symmetric: a root that survives one more sweep costs disk, a root deleted
// from under a live run destroys a run's state.
func OwnerVerdictFor(root string) OwnerVerdict {
	raw, err := os.ReadFile(filepath.Join(root, HolderFileName))
	if err != nil {
		return OwnerVerdict{
			Outcome:    procname.OutcomeUnmeasurable,
			Reason:     OwnerReasonUnregistered,
			Registered: false,
		}
	}
	rec, err := ParseOwnerRecord(string(raw))
	if err != nil {
		return OwnerVerdict{
			Outcome:    procname.OutcomeUnmeasurable,
			Registered: true,
			Reason:     fmt.Sprintf("%s: %v", OwnerReasonUnreadable, err),
		}
	}
	outcome, why := procname.RunnerIdentity(ProcRootDir, procname.Liveness{
		Pid: strconv.Itoa(rec.Pid),
		// The incarnation is the strong factor; the command line is consulted
		// only when the record states no incarnation (the ladder's own rule).
		StartTicks:  int64(rec.Start),
		CmdlineText: rec.Cmd,
	})
	v := OwnerVerdict{Outcome: outcome, Registered: true, Record: rec}
	switch outcome {
	case procname.OutcomeLive:
		v.Reason = fmt.Sprintf("%s (pid %d, start ticks %d)", OwnerReasonLive, rec.Pid, rec.Start)
	case procname.OutcomeGone:
		v.Collectable = true
		v.Reason = fmt.Sprintf("%s (pid %d, start ticks %d)", OwnerReasonGone, rec.Pid, rec.Start)
	case procname.OutcomeRecycled:
		v.Collectable = true
		v.Reason = fmt.Sprintf("%s (recorded pid %d, start ticks %d)", OwnerReasonRecycled, rec.Pid, rec.Start)
	default:
		v.Reason = fmt.Sprintf("%s (%s)", OwnerReasonUnverifiable, string(why))
	}
	return v
}

// OwnerKeeps reports whether a candidate's OWNER RECORD forbids collecting it —
// the writer-owned half of the hard rule, asked by every arm that judges an entry
// whose path is known. A registered owner the kernel confirms LIVE keeps the entry
// at any age, and a registration that cannot be read or measured keeps it too. A
// candidate with NO registration is not held back by this (false): there is no
// record to honour, and the age window is what bounds it — the same division the
// .holder rule always had.
//
// The read is bounded (one file inside the candidate) and walks no process table,
// which is why it is applied on top of the caller's /proc pass rather than
// instead of it.
func OwnerKeeps(path string) bool {
	v := OwnerVerdictFor(path)
	return v.Registered && !v.Collectable
}

// EntryOutcomeUnregistered is the Outcome of a candidate no writer registered. It
// is a distinct WORD from the ladder's four, because "nobody recorded an owner" is
// exactly the gap this file exists to close and must not be rendered as if the
// writer had done its job and been judged dead.
const EntryOutcomeUnregistered procname.Outcome = "unregistered"

// The KEEP reasons a candidate report carries. Constants for the same reason as
// every other reason string here (Rule 1b/13): the CLI line, the sweep's log and
// the tests quote them.
const (
	EntryKeepNotOurs      = "not this system's namespace"
	EntryKeepFresh        = "younger than the stale window"
	EntryKeepUnmeasurable = "could not be measured — unknown is never a delete"
	// EntryKeepDeclaredRoot is the keep a DECLARED tree's own root carries: the
	// tree's container is structure, never a candidate, at any age (the
	// GOTMPDIR incident — a sweep that took its own tree's root).
	EntryKeepDeclaredRoot = "kept — the root of a declared tree is never a candidate"
	EntryKeepLiveHolder   = "a live process holds it"
	EntryKeepArtifact     = "a flat entry is not a run (queue artifacts are kept, never a shield)"
	EntryKeepReserved     = "the GC lock and the tree README are not scratch entries"
)

// EntryCollectablePrefix is the ONE marker a collectable verdict's sentence
// carries, so a reader can tell "why this MAY go" from "why this is KEPT" without
// a second field (Rule 1b/13: a prefix typed twice drifts).
const EntryCollectablePrefix = "collectable: "

// EntryVerdict is one candidate's whole disposition: what it is, how old it is,
// whether this sweep may collect it, and the sentence that decided it. It is the
// unit the reporting face prints and the real sweep records — ONE shape for both
// faces, so a report can never disagree with what the sweep did.
type EntryVerdict struct {
	// Path is the candidate.
	Path string
	// Age is the entry's own mtime age at judgement time.
	Age time.Duration
	// Collectable is the verdict: true = this sweep may remove it.
	Collectable bool
	// Reason is the deciding sentence (why collectable / why kept).
	Reason string
	// Outcome is the owner judgement the verdict rests on — one of
	// procname.Outcome's four values, or EntryOutcomeUnregistered when no writer
	// recorded an owner. It is the word the report carries so a removal count is
	// readable: age-expiry, owner-gone, owner-recycled and owner-unregistered are
	// four different reasons to delete, and owner-live is one reason to keep.
	Outcome procname.Outcome
	// Owner is the recorded incarnation the verdict was measured against (zero
	// when nothing was recorded).
	Owner OwnerRecord
	// HasOwner reports whether a registration existed.
	HasOwner bool
}

// OwnerEvidence renders a candidate's owner evidence for a report: the outcome
// word and the recorded incarnation it was measured against. ONE renderer, so the
// sweep's collection line and the CLI's per-candidate verdict line spell the same
// evidence the same way.
func OwnerEvidence(v EntryVerdict) string {
	if !v.HasOwner {
		return fmt.Sprintf("outcome=%s (no %s record)", EntryOutcomeUnregistered, HolderFileName)
	}
	return fmt.Sprintf("outcome=%s owner=pid:%d/start:%d", v.Outcome, v.Owner.Pid, v.Owner.Start)
}
