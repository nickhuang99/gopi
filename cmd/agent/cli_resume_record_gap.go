package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
)

// cli_resume_record_gap.go — THE REFUSAL for "a resume resolved a checkpoint but
// no session RECORD", plus the ONE opt-in that turns it back into a mint.
//
// THE DEFECT THIS CLOSES (measured 2026-10-28, isolated profile, this host).
// `gopi chat --resume <run-dir>/<id>.raw.log` with the JSONL deleted:
//
//	$ gopi chat --resume .../PRUN/cccc1111-….raw.log
//	[gopi] chat: seeded 1 history message(s) from .../PRUN/cccc1111-….raw.log
//	exit=0
//	$ ls .../PRUN          → cccc1111-….raw.log          (the old dir, untouched)
//	$ ls .../sessions      → … PRUN a49c1546-4da1-…       (a BRAND-NEW directory)
//
// The process succeeded, said nothing about the lost session, and wrote every
// byte of the run into a new sessions/<uuid>/ — the operator's belief that an
// old session grew was false, and nothing on stderr contradicted it. That is
// the Rule 15 shape this file removes: a degradation is either REFUSED (with
// the paths it looked at and what is actually on disk) or, when the operator
// explicitly asked for it, ANNOUNCED FIRST and recorded.
//
// WHY THE RECORD IS THE GATE, not the checkpoint. A raw log is enough to
// replay a CONVERSATION; the session RECORD is what names the session — its id,
// its directory, the options it was started with. With only the checkpoint
// there is no id to reuse, so a "resume" can only mint, and a mint is not a
// resume.

// resumeRecordGapLookedUnderFormat is the refusal's core sentence. It names the
// target the operator typed, the checkpoint that WAS found, and the directory
// whose contents decide the case — in that order, because the reader's first
// question is "did it even find my log?", and the answer is yes.
const resumeRecordGapLookedUnderFormat = "%s: found a checkpoint at %s, but no session RECORD to continue — a resume grows a session that exists and never mints one.\n" +
	"  looked in: %s\n  it holds: %s\n"

// resumeRecordGapRepairFormat spells the two ways out. The opt-in is named by
// its VARIABLE (config.ResumeAllowNewSessionEnv is the ONE spelling) so a reader
// who wants the old behaviour has the exact word to type, and the command below
// it is the WHOLE line to paste — face, switch and the target the operator
// already typed — because a repair hint with the operand missing sends the
// reader back to the help page (measured: the first draft printed
// `GOPI_RESUME_ALLOW_NEW_SESSION=1 gopi chat --resume` and nothing else).
const resumeRecordGapRepairFormat = "  to start a FRESH session instead, say so:\n    %s=1 %s %s\n" +
	"  (a fresh run gets its OWN session id and directory, so the session behind %s does not grow)\n"

// resumeRecordGapAllowedWarningFormat is the FIRST line printed when the
// operator armed the opt-in. It is printed before anything else this run emits
// — the seed message included — because the operator's mental model must be
// corrected at the top of the output, not discovered at the bottom.
const resumeRecordGapAllowedWarningFormat = "WARNING: %s: starting a NEW session — %s holds no session RECORD, so this run does NOT continue the session behind %s (opted in with %s=1).\n"

// resumeRecordRebuiltNoticeFormat announces a RECOVERY that rebuilds a session
// record from its checkpoint. It is not a refusal: the id and the directory are
// the checkpoint's own, so the session the operator named is the one that
// continues. It is printed because the alternative — rebuilding a record in
// silence — is indistinguishable from "your old session grew", which is the
// belief this row exists to stop (Rule 15).
const resumeRecordRebuiltNoticeFormat = "%s: this run directory held no session RECORD (its JSONL is gone) — rebuilding it from the checkpoint under the SAME id and directory (%s). The conversation continues where it stopped.\n"

// resumeFreshSessionNotice announces the OTHER recovery: the checkpoint is not a
// run directory of this profile, so it names no session — there is no id to
// continue and no directory to rebuild in.
//
// THE DEFECT IT REPLACES (measured 2026-10-28, row 01a180f5). The resume took the
// checkpoint's FILE NAME as the session id, so a run on `gopi-raw-resume-e2e.log`
// called itself session `gopi-raw-resume-e2e.log`, wrote its record beside the
// copy (outside the sessions tree, there for nobody) and tried to stamp
// `resumed:gopi-raw-resume-e2e.log` into its row — an id no profile holds, refused
// by tools.TaskTagResumed, so the row never recorded its lineage at all.
//
// A FILE NAME IS NOT AN IDENTITY. This run therefore mints its own — and says so,
// because the alternative (silently doing what this line announces) is what made
// the lineage gap invisible for as long as it lasted (Rule 15).
//
// WHY IT IS FIVE SENTENCES AND NOT ONE (measured 2026-10-30, row 01a18d6c). The
// single sentence this replaces ended "the checkpoint sits outside the sessions
// root", and it was printed for a checkpoint whose parent directory IS the
// sessions root:
//
//	$ gopi session resume <sessions-root>/loose-export.raw.log
//	… is not a run directory of this profile (the checkpoint sits outside the
//	  sessions root), so it names no session to continue …
//
// The gate's answer (false) was right — a file lying loose in the root is not a
// run directory of the profile — but the REASON it printed was the opposite of
// where the file was. Two different false answers (inside the root but not in a
// run directory / not in this tree at all) shared one fixed string, so the
// operator was told to go looking somewhere the checkpoint demonstrably was not.
// Rule 15's own defect shape in miniature: one sentence covering two causes.
//
// Each case below therefore names a fact about THIS checkpoint, and the reader
// can act on it without a second command: the root is printed (so "outside"
// means something checkable), the parent is printed for the nested shape, and
// the run directory is printed when the checkpoint sits below it.
const (
	// resumeFreshSessionLooseInRootFormat is the shape a loose checkpoint in the
	// sessions root takes: the parent IS the root. Say exactly that — the file is
	// IN the tree and simply is not an entry of it.
	resumeFreshSessionLooseInRootFormat = "%s: %s lies loose IN the sessions root %s — a run directory is a direct CHILD of that root (sessions/<run-id>/), and the root itself is not one, so the checkpoint names no session to continue — this run STARTS session %s and continues the CONVERSATION from that checkpoint.\n"

	// resumeFreshSessionNestedFormat is the shape a checkpoint inside a run
	// directory's SUBDIRECTORY takes (a capture under artifacts/, a copy in a
	// retry container's child). The directory above it is a run directory, but the
	// checkpoint is not that run's own checkpoint.
	resumeFreshSessionNestedFormat = "%s: %s sits BELOW the run directory %s, not beside its record — a run's checkpoint is a direct child of the run directory, so this one names no session to continue — this run STARTS session %s and continues the CONVERSATION from that checkpoint.\n"

	// resumeFreshSessionOutsideRootFormat is the shape the old sentence was WRITTEN
	// for, and the only shape allowed to print it: the checkpoint is not in this
	// profile's sessions tree at all (a log copied out of a tree, an export, a
	// fixture).
	resumeFreshSessionOutsideRootFormat = "%s: %s is not a run directory of this profile (the checkpoint sits outside the sessions root %s), so it names no session to continue — this run STARTS session %s and continues the CONVERSATION from that checkpoint.\n"

	// resumeFreshSessionInRunDirFormat is unreachable through the current call
	// sites — resumeIdentityFor answers this shape with a record beside the
	// checkpoint — and it is stated rather than left to fall through a switch: a
	// classifier that silently reused another case's sentence would rebuild the
	// defect this file just removed.
	resumeFreshSessionInRunDirFormat = "%s: %s is in a run directory of this profile but that directory resolved no session RECORD, and its name is not an id, so it names no session to continue — this run STARTS session %s and continues the CONVERSATION from that checkpoint.\n"

	// resumeFreshSessionUnanchoredFormat is the honest answer when the sessions
	// root cannot be resolved at all: nothing can be claimed about WHERE the
	// checkpoint is, so the notice says that instead of guessing a location.
	resumeFreshSessionUnanchoredFormat = "%s: %s is not a run directory of this profile (the sessions root could not be resolved: %v), so it names no session to continue — this run STARTS session %s and continues the CONVERSATION from that checkpoint.\n"
)

// resumeCheckpointLocale is WHERE a checkpoint sits relative to this profile's
// sessions root — the answer the notice above branches on. It is a separate
// question from resumeCheckpointNamesARunDir's (which answers a yes/no), and
// keeping them separate is deliberate: the gate's criterion stays exactly one
// comparison (see that function's comment for why it must not be tightened),
// while the RECORD of why it said no can be as precise as the filesystem
// allows.
type resumeCheckpointLocale int

const (
	// resumeCheckpointLocaleInRunDir — the parent IS a direct entry of the root:
	// a run directory. (resumeCheckpointNamesARunDir answers true here.)
	resumeCheckpointLocaleInRunDir resumeCheckpointLocale = iota
	// resumeCheckpointLocaleLooseInRoot — the parent IS the root: the file lies
	// loose among the run directories and belongs to none of them.
	resumeCheckpointLocaleLooseInRoot
	// resumeCheckpointLocaleBelowRunDir — under the root and deeper than a run
	// directory's own checkpoint (inside a run dir's subdirectory).
	resumeCheckpointLocaleBelowRunDir
	// resumeCheckpointLocaleOutsideRoot — not under the root at all.
	resumeCheckpointLocaleOutsideRoot
	// resumeCheckpointLocaleUnanchored — the root could not be resolved, so no
	// comparison was possible.
	resumeCheckpointLocaleUnanchored
)

// resumePathSeparatorSuffix and resumeParentDirName are the two spellings the
// containment test needs; named so the comparison reads as a rule rather than a
// coincidence (Rule 1).
const (
	resumeParentDirName     = ".."
	resumePathSeparatorRune = os.PathSeparator
)

// resumeCheckpointLocaleOf classifies WHERE a checkpoint sits relative to this
// profile's sessions root. It is a READ: no path is created, no directory is
// listed, and an unresolvable root answers resumeCheckpointLocaleUnanchored
// rather than being folded into "outside" (Rule 15 — the two are not the same
// claim, and only one of them names the root the operator should look at).
func resumeCheckpointLocaleOf(rawLogPath string) (resumeCheckpointLocale, string, error) {
	root, err := config.SessionsRoot()
	if err != nil || root == "" {
		return resumeCheckpointLocaleUnanchored, "", err
	}
	rootAbs, rerr := filepath.Abs(root)
	if rerr != nil {
		return resumeCheckpointLocaleUnanchored, "", rerr
	}
	dir, derr := filepath.Abs(filepath.Dir(rawLogPath))
	if derr != nil {
		return resumeCheckpointLocaleUnanchored, rootAbs, derr
	}
	switch {
	case dir == rootAbs:
		// The checkpoint lies loose in the root itself.
		return resumeCheckpointLocaleLooseInRoot, rootAbs, nil
	case filepath.Dir(dir) == rootAbs:
		// A direct child of the root — a run directory.
		return resumeCheckpointLocaleInRunDir, rootAbs, nil
	}
	rel, relErr := filepath.Rel(rootAbs, dir)
	if relErr == nil && rel != resumeParentDirName &&
		!strings.HasPrefix(rel, resumeParentDirName+string(resumePathSeparatorRune)) {
		return resumeCheckpointLocaleBelowRunDir, rootAbs, nil
	}
	return resumeCheckpointLocaleOutsideRoot, rootAbs, nil
}

// resumeCheckpointRunDirAbove names the run directory a BELOW-RUN-DIR checkpoint
// sits under — the first path element below the sessions root — so the notice can
// print it instead of describing it. An empty answer means the relative path had
// no element to take, and the caller then says nothing about it rather than
// printing a bare separator.
func resumeCheckpointRunDirAbove(rootAbs, rawLogPath string) string {
	if rootAbs == "" {
		return ""
	}
	dir, err := filepath.Abs(filepath.Dir(rawLogPath))
	if err != nil {
		return ""
	}
	rel, relErr := filepath.Rel(rootAbs, dir)
	if relErr != nil {
		return ""
	}
	first := strings.SplitN(rel, string(resumePathSeparatorRune), 2)[0]
	if first == "" || first == resumeParentDirName {
		return ""
	}
	return filepath.Join(rootAbs, first)
}

// resumeFreshSessionNotice renders the notice for one checkpoint. It branches on
// resumeCheckpointLocaleOf, so the sentence a reader gets names the place the
// checkpoint ACTUALLY is: "outside the sessions root" is printed only when the
// checkpoint is outside the sessions root.
//
// The returned string is the whole line, newline included — the caller prints it
// once, so two cases can never be concatenated by accident.
func resumeFreshSessionNotice(progName, rawLogPath, sessionID string) string {
	locale, rootAbs, localeErr := resumeCheckpointLocaleOf(rawLogPath)
	switch locale {
	case resumeCheckpointLocaleLooseInRoot:
		return fmt.Sprintf(resumeFreshSessionLooseInRootFormat, progName, rawLogPath, rootAbs, sessionID)
	case resumeCheckpointLocaleBelowRunDir:
		if runDir := resumeCheckpointRunDirAbove(rootAbs, rawLogPath); runDir != "" {
			return fmt.Sprintf(resumeFreshSessionNestedFormat, progName, rawLogPath, runDir, sessionID)
		}
		// No run directory above it to name: the containing claim would be false,
		// so fall back to the sentence about the tree as a whole.
		return fmt.Sprintf(resumeFreshSessionOutsideRootFormat, progName, rawLogPath, rootAbs, sessionID)
	case resumeCheckpointLocaleInRunDir:
		return fmt.Sprintf(resumeFreshSessionInRunDirFormat, progName, rawLogPath, sessionID)
	case resumeCheckpointLocaleUnanchored:
		if localeErr == nil {
			// No error and no root is not a state this profile can be in, and
			// printing `%v` of a nil error would say "<nil>" — say the fact.
			localeErr = errSessionsRootUnresolved
		}
		return fmt.Sprintf(resumeFreshSessionUnanchoredFormat, progName, rawLogPath, localeErr, sessionID)
	}
	return fmt.Sprintf(resumeFreshSessionOutsideRootFormat, progName, rawLogPath, rootAbs, sessionID)
}

// errSessionsRootUnresolved is the named fact printed when config.SessionsRoot
// answered an empty root without an error — an unverifiable address, which is
// never the one to blame on a path (Rule 15).
var errSessionsRootUnresolved = errors.New("no sessions root is configured for this profile")

// resumeCheckpointNamesARunDir reports whether a checkpoint sits INSIDE a run
// directory — i.e. its parent IS an entry of the sessions root, the address a
// session id resolves to.
//
// THIS IS THE GATE'S SCOPE, and it is narrow on purpose. A bare raw log handed
// to a face from anywhere on disk (a fixture, a copied capture, an exported
// transcript) names no session at all: there is no identity to lose, and
// seeding a conversation from its checkpoint is the documented behaviour, not a
// fork. The defect this file refuses is the OTHER shape — the operator named a
// RUN (`sessions/<id>/`), the checkpoint inside it is theirs, and the record
// that would have continued THAT session is gone. Being unable to tell the two
// apart is why the first draft of this gate failed two e2e arms
// (TestG5ChatRunsAsAdvertised, TestResumeStampsTheFreshSessionOntoTheRow): both
// seed from a loose log, and both were refused by a gate that judged the
// checkpoint's absence alone.
func resumeCheckpointNamesARunDir(rawLogPath string) bool {
	root, err := config.SessionsRoot()
	if err != nil || root == "" {
		// No resolvable root: nothing can be claimed about the address, and an
		// unverifiable target is never the one to refuse (Rule 15 — a refusal
		// must name something the operator can act on).
		return false
	}
	dir, derr := filepath.Abs(filepath.Dir(rawLogPath))
	if derr != nil {
		return false
	}
	rootAbs, rerr := filepath.Abs(root)
	if rerr != nil {
		return false
	}
	// The checkpoint's parent must BE a direct entry of the sessions root. That
	// single comparison is the whole test, and it is the one the previous cut
	// tried to state twice: it followed this comparison with
	//
	//	return strings.TrimSpace(filepath.Base(dir)) != ""
	//
	// which is TRUE BY CONSTRUCTION here — a direct child of an absolute root
	// path can never render an empty base name — so the "require the directory
	// to BE an address" half of the doc comment above was never executed. That
	// is the Rule 15 shape this file exists to remove, arriving in the gate
	// itself: a comment promising a check, and a line that always answers yes.
	//
	// WHY THE TAUTOLOGY IS NOT REPLACED BY A STRICTER NAME TEST — measured on
	// this host's own sessions root (2026-10-30): of 3302 root entries,
	//
	//	canonical 8-4-4-4-12 dirs  3153 (1063 hold a checkpoint)
	//	dashless 32-hex dirs         31 (   0 hold a checkpoint)
	//	EVERY OTHER NAME            118 (  46 hold a checkpoint)
	//
	// and those 46 are not noise: they are `…-r1` / `…-r2` / `…-c1`, the RETRY
	// containers the task lifecycle mints beside the row's own directory, each
	// holding that retry's real checkpoint and record. A test demanding a
	// canonical uuid here would refuse to resume a real retry's session — a
	// worse defect than the tautology (the over-blocking lesson
	// internal/sessionid states for the same reason). A run directory's address
	// is therefore "a direct child of the sessions root", full stop, which is
	// exactly what the comparison below says.
	return filepath.Dir(dir) == rootAbs
}

// resumeDirEntries renders what a directory ACTUALLY holds, for a refusal an
// operator can act on without a second command: a reader who is told "no record
// here" and sees `cccc1111-….raw.log` beside it knows instantly which file was
// found and which is missing.
//
// An unreadable or absent directory is NAMED as such rather than rendered as an
// empty list: "this directory does not exist" and "this directory is empty" are
// different facts, and a refusal that conflated them would send the operator
// looking for a deletion that never happened (Rule 15).
func resumeDirEntries(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "(the directory does not exist)"
		}
		return fmt.Sprintf("(unreadable: %v)", err)
	}
	if len(entries) == 0 {
		return "(empty)"
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += string(os.PathSeparator)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, " ")
}

// resumeResolveRefusalFormat is the OTHER refusal of this family: the target
// resolved NO checkpoint at all, so there is no conversation to seed and no
// session to continue. It is the shape row 01a17df9-99c4's epic names as "the
// directory does not exist" and, with one entry of difference, "the directory
// exists but the record is gone".
//
// WHY IT EXISTS (measured 2026-10-29, sandbox profile, this host). The refusal
// used to be ONE line —
//
//	resume target "01a1ffff-…" is neither a raw log path nor an existing
//	session id (looked under /…/sessions)
//
// — which answers "where" but not "what did you expect" and not "what is
// actually there". An operator whose run directory DOES exist and is EMPTY
// (the jsonl deleted, the raw log pruned) read the same sentence as an operator
// whose id was a typo, and had to run a second command to tell the two apart.
// Both facts are now in the refusal: the layouts it looked for, the run
// directory and what it holds, and the root's own listing.
//
// The last line states what did NOT happen. It deliberately does NOT offer
// config.ResumeAllowNewSessionEnv, and that omission is a correction made on
// measurement (2026-10-29): that switch opens the RECORD-GAP door only — the
// case where a checkpoint WAS found and only the record is missing. HERE there
// is no checkpoint at all, so nothing could seed a conversation and the switch
// would change nothing; naming it would have sent the operator to a knob that
// cannot help. The route that does work is dropping the operand.
const resumeResolveRefusalFormat = "resume target %q is neither a raw log path nor an existing session id.\n" +
	"  looked for: %s\n" +
	"  run directory: %s — %s\n" +
	"  session root %s holds: %s\n" +
	"  nothing was started, and no session was created: a resume CONTINUES a session and never mints one.\n" +
	"  (a new session is started by running the same verb WITHOUT a resume target; %s does not help here — it opens the missing-RECORD door, and this target has no checkpoint at all)\n"

// resumeExpectedLayouts renders the three addresses a session operand resolves
// to — the same three gopilog.FindSession walks (legacy flat record, bare
// <id>/session.jsonl, task-run <id>/<run-uuid>.jsonl). Every segment comes from
// its constant package (Rules 1/13), so a layout rename cannot leave this hint
// describing a tree that no longer exists.
func resumeExpectedLayouts(root, target string) string {
	return strings.Join([]string{
		filepath.Join(root, target+sessionlayout.JSONLSuffix),
		filepath.Join(root, target, sessionlayout.RecordName),
		filepath.Join(root, target, "<run-id>"+sessionlayout.JSONLSuffix),
	}, " OR ")
}

// resumeResolveRefusalLine builds the refusal above. It READS and never writes:
// the run directory's contents are listed with resumeDirEntries, which
// distinguishes "does not exist" from "empty" from "unreadable" — three facts an
// operator acts on differently (Rule 15).
func resumeResolveRefusalLine(target, root string) string {
	return fmt.Sprintf(resumeResolveRefusalFormat,
		target,
		resumeExpectedLayouts(root, target),
		filepath.Join(root, target), resumeDirEntries(filepath.Join(root, target)),
		root, resumeDirEntries(root),
		config.ResumeAllowNewSessionEnv)
}

// resumeRecordGapCheck decides what a resume does when its target resolved a
// CHECKPOINT but no session record.
//
// proceed=true means the operator armed config.ResumeAllowNewSessionEnv and the
// warning above has already been printed — the caller continues on its ordinary
// mint path, having said what that means. proceed=false carries the exit code
// the caller must return: the run never starts, so no directory, no JSONL and no
// raw log are created.
//
// The warning goes to stderr and is the FIRST thing on it for this run: every
// caller invokes this before it prints its seed line, so the correction is not
// buried under a report of what was successfully loaded.
func resumeRecordGapCheck(progName, target, rawLogPath string) (proceed bool, code int) {
	dir := filepath.Dir(rawLogPath)
	if config.ResumeAllowNewSession() {
		fmt.Fprintf(os.Stderr, resumeRecordGapAllowedWarningFormat,
			progName, dir, target, config.ResumeAllowNewSessionEnv)
		return true, 0
	}
	fmt.Fprintf(os.Stderr, resumeRecordGapLookedUnderFormat+resumeRecordGapRepairFormat,
		progName, rawLogPath, dir, resumeDirEntries(dir),
		config.ResumeAllowNewSessionEnv, progName, target, target)
	return false, chatExitFailed
}
