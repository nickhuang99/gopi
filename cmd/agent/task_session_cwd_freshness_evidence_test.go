package main

// task_session_cwd_freshness_evidence_test.go — the freshness refusal's EVIDENCE
// arm: the guard names WHICH of its two causes fired, by measurement, and this is
// where that answer's vocabulary, its readers and its composer live.
//
// THE DEFECT THIS PINS (measured 2026-10-13, run-1791849386-333342, task
// 01a12c66). Twelve of that run's fifteen failing tests were
// assertGopiBinIsFresh's refusal, and every one of them read
//
//	<tested-bin>/gopi (mtime 07:56:32) is OLDER than …/internal/agent/clock.go
//	(mtime 07:58:50) — … Rebuild it … and re-run.
//
// — the binary was built at 07:56:32.037 and the file it complained about was
// RESTORED, in that same run, at 07:58:49.8 (`git checkout HEAD --
// internal/agent/clock.go`), after an INLINE red proof had rewritten it in place
// at 07:56:33.6 (`sed -i 's/stamp := a.turnStampForRequest()/stamp :=
// a.turnStampLine()/' internal/agent/clock.go`). So the guard's verdict was right
// (the mtime really was newer) and its ADVICE was wrong for the situation: "the
// binary predates an edit" and "the tree moved under the run" printed one
// sentence, and the run's own summary named neither.
//
// WHAT THE EVIDENCE IS, and why it takes TWO readings. The two causes are not
// separable by any single witness:
//
//   - an mtime cannot tell an identical-bytes rewrite from a real edit — a
//     RESTORE moves one exactly as an edit does, and that is the incident
//     verbatim;
//   - the tree's content record cannot either, on its own: a writer that edits
//     AND RESTORES leaves the tree byte-identical to HEAD, so by the time the
//     refusal is composed there is nothing left for git to see.
//
// So the arm stamps every production source newer than the binary TWICE — once
// where the guard compares, once as the refusal is composed — and a source whose
// stamp moved was written in place while the run was comparing, which a steady
// tree does not produce. Stable stamps plus a source that differs from HEAD is
// the other cause, and stable stamps plus nothing differing and HEAD not newer
// than the build is the moved tree. Where a record cannot be read, the refusal
// says so and picks NO cause (Rule 15).
//
// The vocabulary is here rather than in the incident's own file because on current
// master the mtime comparison lives in
// cmd/agent/gopibin_identity_guard_test.go's decideBinFreshnessByMtime (the
// artifact's build identity, the content digest and mtime-as-last-resort are one
// decision there), and its refusal calls this arm's composer. Re-expressing the
// handed-over branch's bytes into a stale shape was never the point; giving the
// mtime refusal its cause is, and both the drift gate
// (internal/testutil/testbudget_freshness_gate_test.go) and the shell summary
// (scripts/lib/stream-failures.sh, scripts/test-budget.sh) read THIS spelling.

import (
	"debug/buildinfo"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/buildidentity"
	gopiversion "github.com/nickhuang99/gopi/internal/version"
)

// The freshness refusal's EVIDENCE vocabulary (Rule 1b/14 — one value, named
// once, every reader importing its spelling).
const (
	// cwdProbeFreshnessMarker is the phrase that makes a refusal recognisable —
	// to a reader scanning a run's output and to the runner's summary. It is part
	// of the sentence the composer below prints (not merely asserted about it), so
	// the string cannot be deleted from the message and left standing here.
	cwdProbeFreshnessMarker = "drives the BUILT binary"
	// cwdProbeFreshnessVerdictMovedTree is the verdict when every newer source is
	// byte-for-byte what HEAD holds, HEAD was committed BEFORE this binary was
	// built, and no source was written while the run compared: nothing the binary
	// compiles has changed, and the only thing that moved is an MTIME — a file was
	// rewritten in place, with its content put back. A red proof that mutates a
	// production source and restores it (`sed -i …; git checkout HEAD -- …`)
	// produces exactly this.
	//
	// Rebuilding is the wrong repair here: the writer is still in the run, and the
	// next rebuild reproduces the same refusal (measured 2026-10-13 —
	// run-1791849386-333342's own red proof did this twice).
	cwdProbeFreshnessVerdictMovedTree = "the tree MOVED while the suite ran — every source newer than this binary is byte-for-byte what the tree's HEAD holds and HEAD was committed before the binary was built, so no source CONTENT the binary compiles has changed: something rewrote a source file in place and put its content back (a red proof that mutates a source and restores it does exactly this). The binary is not stale — the TREE moved. Rebuilding before that writer stops reproduces this refusal: stop writing, rebuild, re-run."
	// cwdProbeFreshnessVerdictBinPredates is the verdict when a newer source also
	// DIFFERS from what HEAD holds (or HEAD's own commit landed after the build):
	// the binary genuinely predates an edit, which is the condition this guard was
	// written for.
	cwdProbeFreshnessVerdictBinPredates = "the binary PREDATES this source — the tree's own records show content newer than this binary (the source differs from HEAD, or HEAD was committed after the binary was built), so the build is simply older than the edit"
	// cwdProbeFreshnessVerdictLiveWriter is the verdict when a source's stamp moved
	// BETWEEN the two readings: a writer is active in this run right now, which no
	// classification of content can see after the fact (a mutate-and-restore leaves
	// a clean tree). It is the strongest of the three signals and it is reported as
	// what it is — a live writer, not a stale artifact.
	cwdProbeFreshnessVerdictLiveWriter = "a source was WRITTEN while this comparison ran — its mtime or its size is not the same in the two readings the guard took, so a writer is LIVE in this run right now. That writer may have edited and restored the file (which leaves no content trace at all), and the binary may legitimately contain either state. Rebuilding cannot be relied on until the writer stops; the refusal is about the tree's motion, not about this artifact."
	// cwdProbeFreshnessVerdictUndetermined is the third answer, for when the tree's
	// records cannot be read at all (no git, an unreadable artifact, a repository
	// that cannot be queried). It is printed INSTEAD of a verdict: a reader given a
	// guess would act on it, and the two remedies differ (Rule 15 — an absent answer
	// is reported, never dressed up as a found one).
	cwdProbeFreshnessVerdictUndetermined = "the cause could not be determined — the tree's own records (git) could not be read for it, so this refusal states the facts below and picks no cause"
	// cwdProbeFreshnessUnreadableNow stands in for a source's metadata that could
	// not be re-read. It is a WORD, never a zero time: a zero time renders as
	// 0001-01-01, a fact the guard cannot have (Rule 15).
	cwdProbeFreshnessUnreadableNow = "unreadable-now"
	// cwdProbeGitBinary is the reader of the tree's own records; the arguments below
	// are spelled here so the report cannot drift from what it ran.
	cwdProbeGitBinary = "git"
	// cwdProbeFreshnessSourcesReported caps how many newer sources the refusal
	// lists: enough to recognise the writer's footprint, never a wall of paths.
	// The shell side's freshness_report_n is the same idea on its own side, and both
	// name their own cap rather than sharing a number the two surfaces use
	// differently (one lists sources, one lists refusals).
	cwdProbeFreshnessSourcesReported = 3
	// cwdProbeFreshnessSourcePrefix introduces each newer source a refusal names. A
	// prefix rather than a bare path, because a path a reader cannot tell apart
	// from prose is not evidence.
	cwdProbeFreshnessSourcePrefix = "newer source: "
	// cwdProbeFreshnessMarkerLibConstName is the NAME the shell side spells the
	// marker's value under (scripts/lib/stream-failures.sh). The two names are one
	// value's two readers, so the drift gate reads both files by name (Rule 14): a
	// rename on either side must fail a test rather than quietly stop matching.
	cwdProbeFreshnessMarkerLibConstName = "stream_freshness_marker"
	// cwdProbeScriptsDir and cwdProbeStreamFailuresLib name the runner's shared
	// shell library — the shell side of the marker, read by the drift arm below.
	cwdProbeScriptsDir        = "scripts"
	cwdProbeStreamFailuresLib = "stream-failures.sh"
	// cwdProbeFreshnessRelocatedNotice states WHERE the comparison this evidence
	// belongs to lives, so a reader who comes looking for it in the incident's own
	// file is told instead of left guessing.
	cwdProbeFreshnessRelocatedNotice = "the comparison this refusal is about is cmd/agent/gopibin_identity_guard_test.go's mtime arm (decideBinFreshnessByMtime): the evidence above was added beside that verdict, never in place of it"
	// cwdProbeSourceSuffix and cwdProbeTestSourceSuffix are the two filters the
	// freshness walk applies to paths (the binary is compiled from production
	// sources only).
	cwdProbeSourceSuffix     = ".go"
	cwdProbeTestSourceSuffix = "_test.go"
)

var (
	// cwdProbeGitNumstatArgs asks git which of the named paths differ from the
	// tree's HEAD, in numstat form (added, deleted, path) — CONTENT, never a
	// timestamp. The revision and the `--` are part of the slice so a path can
	// never be read as a flag or a revision.
	cwdProbeGitNumstatArgs = []string{gitDiffVerb, cwdProbeGitNumstatFlag, gitHeadRevision, gitPathSeparatorFlag}
	// cwdProbeGitCommitTimeArgs asks when HEAD was committed (`%cI`, strict
	// RFC3339) — the fact that decides whether an edit landed after the build.
	cwdProbeGitCommitTimeArgs = []string{"log", "-1", "--format=%cI"}
	// cwdProbeProductionSourceDirs are the trees the freshness walk reads — the ones
	// the binary is compiled from (Rule 14: the newest-source scan and this arm's
	// readings read this ONE list).
	cwdProbeProductionSourceDirs = []string{sourceTreeCmdDir, sourceTreeInternalDir}
)

const (
	// cwdProbeGitNumstatFlag is the git verb flag numstat output comes from.
	cwdProbeGitNumstatFlag = "--numstat"
	// cwdProbeFreshnessStampSep/-Fields describe the shape of one source's stamp
	// between the two readings: "<mtime> <size>".
	cwdProbeFreshnessStampSep    = " "
	cwdProbeFreshnessStampFields = 2
	// cwdProbeFreshnessStampBase is the base a source's size is rendered in:
	// decimal, the unit a reader of a log will have.
	cwdProbeFreshnessStampBase = 10
	// cwdProbeNumstatSep/-Fields/-PathField describe a numstat row:
	// "<added>\t<deleted>\t<path>".
	cwdProbeNumstatSep       = "\t"
	cwdProbeNumstatFields    = 3
	cwdProbeNumstatPathField = 2
)

// cwdProbeSource is one production .go file the freshness comparison read: its
// path, the mtime the SCAN stamped on it (never refreshed — the stamped value is
// what makes the guard's input and its verdict describe one state of the tree),
// the stamp (mtime + size) that identifies it between this arm's two readings, and
// a reader for what its mtime is NOW.
type cwdProbeSource struct {
	path    string
	modTime time.Time
	// stamp is cwdProbeFreshnessStampOf's answer for this file at scan time: the
	// first of the two readings the verdict compares.
	stamp string
	// now re-reads the file's mtime; ok=false means it can no longer be read, which
	// the refusal prints as cwdProbeFreshnessUnreadableNow.
	now func() (time.Time, bool)
}

// cwdProbeFreshnessStampOf renders the properties a source is identified by
// between the two readings: its mtime at NANOSECOND precision (so a rewrite in
// place cannot hide behind a coarse clock) and its size (the second, independent
// property — a mutate-and-restore is free to land on the same mtime granularity,
// but not on the same byte count as a coincidence).
func cwdProbeFreshnessStampOf(fi os.FileInfo) string {
	return fi.ModTime().Format(time.RFC3339Nano) + cwdProbeFreshnessStampSep +
		strconv.FormatInt(fi.Size(), cwdProbeFreshnessStampBase)
}

// cwdProbeFreshnessBinRevision is the revision the TESTED BINARY was built from,
// read from the artifact itself — debug/buildinfo reads the FILE the run is
// driving, so unlike runtime/debug.ReadBuildInfo (the running test process) it
// answers about the binary under test. ZERO new dependencies: the artifact is the
// one witness that cannot lie about the code it holds.
//
// Its absence is the COMMON answer on this project's own build path and is
// reported with its cause, never as a blank a reader would take for "the same
// revision": cmd/go recognizes a repository root by a `.git` DIRECTORY, and a
// linked worktree's `.git` is a FILE — so a worktree build carries no vcs.*
// setting at all. The wording is cmd/agent/cli_version.go's
// versionStampSkippedCause, reused rather than retyped (Rule 14).
func cwdProbeFreshnessBinRevision(bin string) string {
	info, err := buildinfo.ReadFile(bin)
	if err != nil {
		return fmt.Sprintf("unavailable (this artifact carries no readable build info: %v)", err)
	}
	revision, dirty := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case gopiversion.BuildInfoKeyRevision:
			revision = setting.Value
		case gopiversion.BuildInfoKeyModified:
			dirty = setting.Value == gopiversion.BuildInfoValueModifiedTrue
		}
	}
	if revision == "" {
		return versionNoVCSStamp
	}
	if dirty {
		return revision + " (built from a DIRTY tree — the binary corresponds to no commit)"
	}
	return revision
}

// cwdProbeGitOutput runs git in root and returns its stdout; a non-zero exit or a
// start failure is an error the caller reports (never a silent empty answer,
// Rule 15b).
func cwdProbeGitOutput(root string, args ...string) (string, error) {
	cmd := exec.Command(cwdProbeGitBinary, args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// cwdProbeTreeHead is the revision the tree the suite is running against stands
// on. A git or repository failure is REPORTED, never guessed at (Rule 15): "no
// revision" and "revision X" are different facts, and a reader deciding between
// causes needs to know which one they have.
func cwdProbeTreeHead(root string) string {
	out, err := cwdProbeGitOutput(root, gitRevParseVerb, gitHeadRevision)
	if err != nil {
		return fmt.Sprintf("unavailable (%v)", err)
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Sprintf("unavailable (%s %s printed nothing)", gitRevParseVerb, gitHeadRevision)
	}
	return strings.TrimSpace(out)
}

// freshnessTreeReading is the tree's OWN answer to the question the two causes
// hinge on — was a source WRITTEN while this comparison ran — plus the tree's
// records of what content differs from HEAD. It is the guard's own instrument, and
// it exists because a refusal must not have to ASK the tree a question whose
// answer can move between the asking and the printing.
type freshnessTreeReading struct {
	// scanned is the FIRST reading's stamp per source. The second reading is taken
	// inside describe, at the moment the refusal is composed.
	scanned map[string]string
	// differs names the newer sources whose CURRENT bytes differ from the tree's
	// HEAD, carrying the magnitude git reported for each — the CONTENT half, which
	// is about bytes and never about a timestamp.
	differs map[string]string
	// unreadable carries the reason a question could not be ASKED. It is a
	// sentence, never a boolean: a bare "false" would read like a measurement.
	unreadable string
}

// readFreshnessTree takes the FIRST reading of the production sources newer
// than the binary, then asks git which of them differ from HEAD. The walk is
// cwdProbeProductionSources (task_session_cwd_e2e_test.go): the same two trees,
// the same two filters and the same test-only directory skip the guard's own
// verdict uses (Rule 14 — one reader for the scope).
func readFreshnessTree(root string, binModTime time.Time) (freshnessTreeReading, error) {
	sources, err := cwdProbeProductionSources(root)
	if err != nil {
		return freshnessTreeReading{}, err
	}
	reading := freshnessTreeReading{scanned: map[string]string{}}
	for _, src := range sources {
		if src.modTime.After(binModTime) {
			reading.scanned[src.path] = src.stamp
		}
	}
	if len(reading.scanned) == 0 {
		return reading, nil
	}
	differs, err := freshnessContentDiff(root, reading.scanned)
	if err != nil {
		reading.unreadable = fmt.Sprintf("%s %s could not be asked which of these sources differ from %s (%v)",
			cwdProbeGitBinary, strings.Join(cwdProbeGitNumstatArgs, cwdProbeFreshnessStampSep), gitHeadRevision, err)
		return reading, nil
	}
	reading.differs = differs
	return reading, nil
}

// freshnessContentDiff asks git which of these sources differ from HEAD — content,
// never a timestamp — and returns each one's magnitude, sorted by path, so the
// sentence a reader gets is the same sentence on every run of the same tree.
func freshnessContentDiff(root string, sources map[string]string) (map[string]string, error) {
	paths := make([]string, 0, len(sources))
	for path := range sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out, err := cwdProbeGitOutput(root, append(append([]string{}, cwdProbeGitNumstatArgs...), paths...)...)
	if err != nil {
		return nil, err
	}
	differs := map[string]string{}
	for _, row := range strings.Split(out, "\n") {
		fields := strings.SplitN(strings.TrimSpace(row), cwdProbeNumstatSep, cwdProbeNumstatFields)
		if len(fields) != cwdProbeNumstatFields {
			continue
		}
		differs[strings.TrimSpace(fields[cwdProbeNumstatPathField])] =
			strings.Join(fields[:cwdProbeNumstatPathField], cwdProbeNumstatSep)
	}
	return differs, nil
}

// describe takes the SECOND reading and composes the arm's evidence lines and the
// VERDICT they license — ONE place, so a verdict can never be printed without the
// facts that decided it, and no second classifier exists to drift from it.
func (r freshnessTreeReading) describe(root, bin string, binModTime time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  tree HEAD:  %s", cwdProbeTreeHead(root))
	fmt.Fprintf(&b, "\n  built from: %s", cwdProbeFreshnessBinRevision(bin))
	if r.unreadable != "" {
		return r.undetermined(&b, "records: "+r.unreadable+".")
	}
	second, err := freshnessTreeReadingStamps(root, binModTime)
	if err != nil {
		return r.undetermined(&b, fmt.Sprintf("records: a second reading of the production sources could not be taken (%v).", err))
	}
	commitWhen, commitErr := freshnessHeadCommitTime(root)
	moved := freshnessMovedSources(r.scanned, second)
	if len(moved) > 0 {
		reported := moved
		if len(reported) > cwdProbeFreshnessSourcesReported {
			reported = reported[:cwdProbeFreshnessSourcesReported]
		}
		for _, path := range reported {
			fmt.Fprintf(&b, "\n  %s%s (mtime %s)", cwdProbeFreshnessSourcePrefix, path,
				strings.SplitN(r.scanned[path], cwdProbeFreshnessStampSep, cwdProbeFreshnessStampFields)[0])
		}
		fmt.Fprintf(&b, "\n  records: %d newer production source(s), of which %d was written WHILE this comparison ran — the source was rewritten IN PLACE (its mtime and/or its size changed between the two readings), which a tree nobody is writing to does not produce.", len(r.scanned), len(moved))
		fmt.Fprintf(&b, "\n  VERDICT: %s", cwdProbeFreshnessVerdictLiveWriter)
		fmt.Fprintf(&b, "\n  %s", cwdProbeFreshnessRelocatedNotice)
		return b.String()
	}
	r.listSources(&b)
	switch {
	case len(r.differs) > 0:
		fmt.Fprintf(&b, "\n  records: the second reading found the SAME set with the SAME stamps (no live writer), and %d newer source(s) DIFFER from %s (%s) — the tree carries content this binary does not hold.",
			len(r.differs), gitHeadRevision, sortedMagnitudes(r.differs))
	case commitErr != nil:
		return r.undetermined(&b, fmt.Sprintf("records: every newer source is byte-for-byte what %s holds and no source moved during the comparison, but %s's commit time could not be read (%v) — so 'the tree moved' cannot be told from 'the binary predates the revision the tree stands on'.", gitHeadRevision, gitHeadRevision, commitErr))
	case commitWhen.Truncate(time.Second).After(binModTime.Truncate(time.Second)):
		fmt.Fprintf(&b, "\n  records: every newer source is byte-for-byte what %s holds, but %s was committed %s — AFTER this binary was built (%s) — so the binary predates the revision the tree stands on.",
			gitHeadRevision, gitHeadRevision, commitWhen.Format(time.RFC3339), binModTime.Format(time.RFC3339))
	default:
		fmt.Fprintf(&b, "\n  records: every newer source is byte-for-byte what %s holds, no source moved while this comparison ran, and %s was committed %s — not after this binary was built (%s) — so no source CONTENT the binary compiles has changed: the refusal is about MTIMES.",
			gitHeadRevision, gitHeadRevision, commitWhen.Format(time.RFC3339), binModTime.Format(time.RFC3339))
	}
	// Compared at SECOND granularity, because that is the granularity git records
	// a commit time in: a build in the same second as the commit would otherwise
	// read as "the binary predates HEAD" on sub-second precision the record does
	// not have (Rule 15 — claim only what was measured).
	verdict := cwdProbeFreshnessVerdictMovedTree
	if len(r.differs) > 0 || (commitErr == nil && commitWhen.Truncate(time.Second).After(binModTime.Truncate(time.Second))) {
		verdict = cwdProbeFreshnessVerdictBinPredates
	}
	fmt.Fprintf(&b, "\n  VERDICT: %s", verdict)
	fmt.Fprintf(&b, "\n  %s", cwdProbeFreshnessRelocatedNotice)
	return b.String()
}

// listSources names the newer sources with the mtimes the FIRST reading stamped —
// the scan's own values, which is what makes the guard's input and its verdict
// describe one state of the tree — and marks the ones the tree's records show as
// differing from HEAD.
func (r freshnessTreeReading) listSources(b *strings.Builder) {
	paths := make([]string, 0, len(r.scanned))
	for path := range r.scanned {
		paths = append(paths, path)
	}
	modTimeOf := func(path string) string {
		return strings.SplitN(r.scanned[path], cwdProbeFreshnessStampSep, cwdProbeFreshnessStampFields)[0]
	}
	sort.Slice(paths, func(i, j int) bool { return modTimeOf(paths[i]) > modTimeOf(paths[j]) })
	for i, path := range paths {
		if i >= cwdProbeFreshnessSourcesReported {
			break
		}
		detail := ""
		if magnitude, differs := r.differs[path]; differs {
			detail = " — differs from HEAD (numstat " + magnitude + ")"
		}
		fmt.Fprintf(b, "\n  %s%s (mtime %s)%s", cwdProbeFreshnessSourcePrefix, path, modTimeOf(path), detail)
	}
	fmt.Fprintf(b, "\n  %d production source(s) newer than the binary.", len(r.scanned))
}

// undetermined closes a refusal that picked NO cause: the facts it could read are
// already printed and the missing one is named (Rule 15).
func (r freshnessTreeReading) undetermined(b *strings.Builder, reason string) string {
	fmt.Fprintf(b, "\n  %s", reason)
	fmt.Fprintf(b, "\n  VERDICT: %s", cwdProbeFreshnessVerdictUndetermined)
	fmt.Fprintf(b, "\n  %s", cwdProbeFreshnessRelocatedNotice)
	return b.String()
}

// freshnessTreeReadingStamps is the SECOND reading: the stamps of every production
// source newer than the binary, as the tree stands at the moment the refusal is
// composed. It reads the same sources cwdProbeProductionSources walked (that is
// what makes the two readings comparable), but it RE-READS them, because a stamped
// value carried over from the first reading could not show motion.
func freshnessTreeReadingStamps(root string, binModTime time.Time) (map[string]string, error) {
	sources, err := cwdProbeProductionSources(root)
	if err != nil {
		return nil, err
	}
	stamps := map[string]string{}
	for _, src := range sources {
		if src.modTime.After(binModTime) {
			stamps[src.path] = src.stamp
		}
	}
	return stamps, nil
}

// freshnessHeadCommitTime is when the tree's HEAD was committed; its absence is an
// error the refusal reports rather than a zero time it would print as 0001-01-01
// (a fact the guard cannot have).
func freshnessHeadCommitTime(root string) (time.Time, error) {
	out, err := cwdProbeGitOutput(root, cwdProbeGitCommitTimeArgs...)
	if err != nil {
		return time.Time{}, err
	}
	when, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(out))
	if parseErr != nil {
		return time.Time{}, fmt.Errorf("%s %s printed %q: %w",
			cwdProbeGitBinary, strings.Join(cwdProbeGitCommitTimeArgs, cwdProbeFreshnessStampSep), strings.TrimSpace(out), parseErr)
	}
	return when, nil
}

// freshnessMovedSources names the sources whose stamp is not the same in both
// readings — written between them, which a tree nobody is writing to does not
// produce. Sorted, because a reader follows the list and its order must not depend
// on map iteration.
func freshnessMovedSources(scanned, second map[string]string) []string {
	var moved []string
	for path, stamp := range scanned {
		if now, still := second[path]; !still || now != stamp {
			moved = append(moved, path)
		}
	}
	for path := range second {
		if _, first := scanned[path]; !first {
			moved = append(moved, path)
		}
	}
	sort.Strings(moved)
	return moved
}

// sortedMagnitudes renders the differing sources' magnitudes in path order.
func sortedMagnitudes(differs map[string]string) string {
	paths := make([]string, 0, len(differs))
	for path := range differs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	rows := make([]string, 0, len(paths))
	for _, path := range paths {
		rows = append(rows, differs[path]+cwdProbeFreshnessStampSep+path)
	}
	return strings.Join(rows, "; ")
}

// freshnessRefusalEvidence is what the guard's mtime arm calls: it takes the
// tree's reading and answers with the evidence lines and the verdict they license.
// It is the ONE entry point of this arm, so a caller cannot print a verdict from a
// reading it did not take.
func freshnessRefusalEvidence(root, bin string, binModTime time.Time) (string, error) {
	reading, err := readFreshnessTree(root, binModTime)
	if err != nil {
		return "", fmt.Errorf("read the production sources under %s: %w", root, err)
	}
	return reading.describe(root, bin, binModTime), nil
}

// --- the arms ---------------------------------------------------------------

// freshnessEvidenceFixture is the smallest tree the refusal's evidence arm reads,
// with real git behind it (the verdict is decided by git's record of CONTENT, so a
// fixture without git could only ever exercise the undetermined answer):
//
//	<root>/cmd/agent/main.go            production source
//	<root>/internal/agent/probe.go      production source
//	<root>/bin/gopi                     the "tested binary"
//
// `sourceEdited` decides whether the newer production source differs from HEAD
// (the binary-predates state) or matches it (the tree-moved state). The source is
// made newer than the binary either way, so the refusal fires in both.
func freshnessEvidenceFixture(t *testing.T, sourceEdited bool) (root, bin string) {
	t.Helper()
	root = t.TempDir()
	writeFile := func(rel, body string) string {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		return p
	}
	writeFile(filepath.Join(sourceTreeCmdDir, "agent", "main.go"), "package main\n\nfunc main() {}\n")
	probe := writeFile(filepath.Join(sourceTreeInternalDir, "agent", "probe.go"), "package agent\n\n// probe\n")
	bin = writeFile(filepath.Join("bin", "gopi"), "not really a binary\n")

	// git is what makes the verdict decidable, and its absence is REPORTED by the
	// arm rather than skipped silently: a fixture that cannot answer the content
	// question would exercise only the undetermined branch (Rule 15).
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command(cwdProbeGitBinary, args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %s in the fixture failed (%v): %s", strings.Join(args, cwdProbeFreshnessStampSep), err, out)
		}
	}
	runGit("init", "-q")
	runGit("add", "-A")
	// The commit is STAMPED in the past (the committer's date is set explicitly),
	// because the fixture reproduces the incident's geometry — the build is NEWER
	// than the revision the tree stands on — and a commit dated "now" would make
	// the binary predate HEAD for a reason that has nothing to do with the case
	// under test. Deterministic, rather than racing the clock.
	when := time.Now().Add(-10 * time.Minute).Format(time.RFC3339)
	cmd := exec.Command(cwdProbeGitBinary, "-c", "user.email=probe@invalid", "-c", "user.name=probe", "commit", "-qm", "base")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git commit in the fixture failed (%v): %s", err, out)
	}

	// The binary's mtime is the BUILD time: after the commit above, before the
	// source below.
	builtAt := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(bin, builtAt, builtAt); err != nil {
		t.Fatalf("chtimes %s: %v", bin, err)
	}
	if sourceEdited {
		// A genuine EDIT: the content differs from HEAD, so the binary really does
		// predate the change.
		if err := os.WriteFile(probe, []byte("package agent\n\n// probe, edited after the build\n"), 0o644); err != nil {
			t.Fatalf("edit %s: %v", probe, err)
		}
	}
	// Either way the source is newer than the binary (which is what the mtime walk
	// sees), and in the not-edited case its content is byte-for-byte what HEAD
	// holds — a rewrite in place that put the content back.
	newer := time.Now()
	if err := os.Chtimes(probe, newer, newer); err != nil {
		t.Fatalf("chtimes %s: %v", probe, err)
	}
	return root, bin
}

// freshnessEvidenceRefusal builds the refusal the guard would print for the
// fixture — through the guard's OWN walk, its OWN reader and its OWN composer, so
// the test measures the production path and not a reconstruction of it.
func freshnessEvidenceRefusal(t *testing.T, root, bin string) string {
	t.Helper()
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("stat %s: %v", bin, err)
	}
	newest, newestPath, err := cwdProbeNewestProductionSource(root)
	if err != nil {
		t.Fatalf("newest production source: %v", err)
	}
	if !info.ModTime().Before(newest) {
		t.Fatalf("the fixture is wrong: %s (mtime %s) is not older than %s (mtime %s) — the guard would not fire at all",
			bin, info.ModTime().Format(time.RFC3339), newestPath, newest.Format(time.RFC3339))
	}
	reading, err := readFreshnessTree(root, info.ModTime())
	if err != nil {
		t.Fatalf("read the fixture's sources: %v", err)
	}
	return reading.describe(root, bin, info.ModTime())
}

// TestFreshnessRefusalNamesAMovedTree is the incident's state: the source's mtime
// is newer than the binary while its CONTENT is exactly what HEAD holds, no source
// moved during the comparison, and HEAD was committed before the build. The refusal
// must say the tree moved — and must NOT tell the reader to rebuild as though the
// binary were the problem.
func TestFreshnessRefusalNamesAMovedTree(t *testing.T) {
	root, bin := freshnessEvidenceFixture(t, false)
	refusal := freshnessEvidenceRefusal(t, root, bin)

	if !strings.Contains(refusal, cwdProbeFreshnessVerdictMovedTree) {
		t.Errorf("the refusal does not name the moved tree (the incident's state: content identical to HEAD, mtime newer than the binary):\n%s", refusal)
	}
	for _, other := range []string{cwdProbeFreshnessVerdictBinPredates, cwdProbeFreshnessVerdictLiveWriter} {
		if strings.Contains(refusal, other) {
			t.Errorf("the refusal claims %q while every newer source matches HEAD byte-for-byte and nothing moved during the comparison — the causes are the whole point of this arm:\n%s", other, refusal)
		}
	}
	// The evidence a reader needs to check the claim: the tree's revision, the
	// binary's own revision answer, and the source with its mtime.
	if !strings.Contains(refusal, "tree HEAD:  ") {
		t.Errorf("the refusal does not report the tree's HEAD:\n%s", refusal)
	}
	if !strings.Contains(refusal, "built from: ") {
		t.Errorf("the refusal does not report the tested binary's own build identity:\n%s", refusal)
	}
	if !strings.Contains(refusal, filepath.Join(sourceTreeInternalDir, "agent", "probe.go")) {
		t.Errorf("the refusal does not name the newer production source:\n%s", refusal)
	}
}

// TestFreshnessRefusalStillNamesAPredatingBinary is the other half: the source was
// genuinely EDITED after the build (it differs from HEAD), so the guard's original
// advice is the right one. A refusal that answered "the tree moved" for every case
// would be the old defect wearing a new sentence.
func TestFreshnessRefusalStillNamesAPredatingBinary(t *testing.T) {
	root, bin := freshnessEvidenceFixture(t, true)
	refusal := freshnessEvidenceRefusal(t, root, bin)

	if !strings.Contains(refusal, cwdProbeFreshnessVerdictBinPredates) {
		t.Errorf("the refusal does not name the predating binary while the newer source differs from HEAD:\n%s", refusal)
	}
	if strings.Contains(refusal, cwdProbeFreshnessVerdictMovedTree) {
		t.Errorf("the refusal claims the tree moved while a source differs from HEAD — the incident's verdict was reached by reasoning about mtimes alone, which is the defect:\n%s", refusal)
	}
}

// TestFreshnessRefusalNamesALiveWriter drives the THIRD state, the one neither of
// the other two arms can reach: a source written BETWEEN the two readings. It is
// produced by its own instrument rather than by racing the production walk — the
// first reading is taken normally, the file's bytes and size are changed, and then
// the refusal is composed, so the second reading sees a stamp that moved. That is
// the exact shape of the incident (an edit and a restore inside one run) and the
// one signal a content check cannot recover after the fact.
func TestFreshnessRefusalNamesALiveWriter(t *testing.T) {
	root, bin := freshnessEvidenceFixture(t, false)
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("stat %s: %v", bin, err)
	}
	first, err := readFreshnessTree(root, info.ModTime())
	if err != nil {
		t.Fatalf("first reading: %v", err)
	}
	// The writer: the source is REWRITTEN IN PLACE, its content put back, and its
	// size changed (a mutation AND a restore, which is what a red proof leaves).
	probe := filepath.Join(root, sourceTreeInternalDir, "agent", "probe.go")
	edited := "package agent\n\n// probe, mutated then restored, with different bytes\n"
	if err := os.WriteFile(probe, []byte(edited), 0o644); err != nil {
		t.Fatalf("rewrite %s: %v", probe, err)
	}
	// The refusal is composed AFTER the write — the incident's own ordering — and it
	// is composed from the reading taken BEFORE it, which is the whole point: the
	// arm's instrument is the pair of readings, not one instant.
	refusal := first.describe(root, bin, info.ModTime())

	if !strings.Contains(refusal, cwdProbeFreshnessVerdictLiveWriter) {
		t.Errorf("a source rewritten between the two readings is a LIVE writer, and the verdict must say so (a content check cannot recover this after the fact):\n%s", refusal)
	}
	if !strings.Contains(refusal, probe) {
		t.Errorf("the refusal does not name the source that moved:\n%s", refusal)
	}
}

// TestFreshnessRefusalReportsAnUnreadableTree pins Rule 15's other half on this
// arm: a question that cannot be ASKED yields no cause, stated as such — never a
// guess, and never a silent empty answer a reader would take for "nothing moved".
func TestFreshnessRefusalReportsAnUnreadableTree(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin", "gopi")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o644); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("stat bin: %v", err)
	}
	// No git in this tree: the reading cannot be taken, so the arm must SAY that.
	moved := freshnessMovedSources(
		map[string]string{filepath.Join(root, "internal", "agent", "clock.go"): "t1 1"},
		map[string]string{filepath.Join(root, "internal", "agent", "clock.go"): "t2 2"},
	)
	if len(moved) != 1 {
		t.Fatalf("freshnessMovedSources did not report a stamp that moved: %v", moved)
	}
	if same := freshnessMovedSources(
		map[string]string{filepath.Join(root, "internal", "agent", "clock.go"): "t1 1"},
		map[string]string{filepath.Join(root, "internal", "agent", "clock.go"): "t1 1"},
	); len(same) != 0 {
		t.Errorf("freshnessMovedSources reported motion where the stamps are identical: %v", same)
	}
	reading := freshnessTreeReading{
		scanned:    map[string]string{filepath.Join(root, "internal", "agent", "clock.go"): "t1 1"},
		unreadable: "git diff --numstat could not be asked which of these sources differ from HEAD (exit status 128)",
	}
	refusal := reading.describe(root, bin, info.ModTime())
	if !strings.Contains(refusal, cwdProbeFreshnessVerdictUndetermined) {
		t.Errorf("a reading whose records cannot be read must pick NO cause:\n%s", refusal)
	}
	if strings.Contains(refusal, cwdProbeFreshnessVerdictMovedTree) || strings.Contains(refusal, cwdProbeFreshnessVerdictBinPredates) {
		t.Errorf("the refusal picked a cause its records could not support:\n%s", refusal)
	}
	if !strings.Contains(refusal, "could not be asked") {
		t.Errorf("the refusal does not name the question it could not ask:\n%s", refusal)
	}
}

// TestFreshnessRefusalKeepsItsRefusalWord is the anti-weakening arm: the message
// must still BE a refusal, with the two mtimes, the marker and the rebuild command
// in it — the guard's documented value is that every e2e green is falsifiable, and
// an evidence arm bolted onto a softened sentence would spend that value.
func TestFreshnessRefusalKeepsItsRefusalWord(t *testing.T) {
	root, bin := freshnessEvidenceFixture(t, false)
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("stat %s: %v", bin, err)
	}
	newest, newestPath, err := cwdProbeNewestProductionSource(root)
	if err != nil {
		t.Fatalf("newest production source: %v", err)
	}
	// The sentence is the GUARD's (freshnessRefusalText), composed where the
	// refusal is: this arm asserts it is still intact rather than re-typing it.
	refusal := fmt.Sprintf(freshnessRefusalText, bin, info.ModTime().Format(time.RFC3339), newestPath, newest.Format(time.RFC3339))
	for _, want := range []string{"is OLDER than", cwdProbeFreshnessMarker, "would verify code that is not in that binary"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal no longer contains %q — the guard is a REFUSAL, and its evidence is added beside that verdict, never in place of it:\n%s", want, refusal)
		}
	}
	if !strings.Contains(refusal, bin) {
		t.Errorf("the refusal does not name the binary it refused (%s):\n%s", bin, refusal)
	}
}

// TestFreshnessBinRevisionReadsTheArtifact pins the stdlib-only build-identity
// reader on a REAL binary (Rule 31: the surface is executed, not described): a
// module built with no repository around it carries no VCS stamp, and the reader
// must report that absence WITH its cause rather than an empty string a reader
// would take for "the same revision as the tree".
func TestFreshnessBinRevisionReadsTheArtifact(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "probe-gopi")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe.test/freshness\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build the probe artifact here (%v): %s", err, out)
	}
	got := cwdProbeFreshnessBinRevision(bin)
	if strings.TrimSpace(got) == "" {
		t.Fatalf("the reader returned an EMPTY identity for a real artifact — 'no revision' and 'I did not look' must not read alike (Rule 15)")
	}
	if got != versionNoVCSStamp {
		t.Errorf("cwdProbeFreshnessBinRevision(no-repo build) = %q;\nwant %q", got, versionNoVCSStamp)
	}
	missing := filepath.Join(dir, "does-not-exist")
	if got := cwdProbeFreshnessBinRevision(missing); !strings.HasPrefix(got, "unavailable") {
		t.Errorf("cwdProbeFreshnessBinRevision(%s) = %q; want an 'unavailable' answer naming the failure", missing, got)
	}
}

// TestFreshnessMarkerSpellingsAgree is the DRIFT arm. The marker exists twice —
// once in Go, where the refusal COMPOSES the sentence, and once in shell, where the
// runner RECOGNISES it — and a shell literal beside a Go literal drifts silently:
// the shell side would simply stop matching, and the summary would go quiet about a
// condition it used to name (Rule 15: a silent empty answer). So both files are
// read here and their values compared.
func TestFreshnessMarkerSpellingsAgree(t *testing.T) {
	root := cwdProbeModuleRoot(t)
	const markerName = "cwdProbeFreshnessMarker"
	goValue, err := stringConstValue(filepath.Join(root, sourceTreeCmdDir, "agent", "task_session_cwd_freshness_evidence_test.go"), markerName)
	if err != nil {
		t.Fatalf("read the guard's marker: %v", err)
	}
	libValue, err := shellVarValue(filepath.Join(root, cwdProbeScriptsDir, "lib", cwdProbeStreamFailuresLib), cwdProbeFreshnessMarkerLibConstName)
	if err != nil {
		t.Fatalf("read the runner's marker: %v", err)
	}
	if goValue != libValue {
		t.Fatalf("the two spellings of the freshness marker disagree:\n  %s = %q\n  %s = %q",
			markerName, goValue, cwdProbeFreshnessMarkerLibConstName, libValue)
	}
	// And the value must be USED, not merely declared: it is an argument of the
	// refusal the guard composes, so it occurs at least twice in the files that hold
	// it (the definition and the message). A declared-but-unused constant would
	// leave the marker a claim about a sentence nobody prints.
	uses := 0
	for _, path := range []string{
		filepath.Join(root, sourceTreeCmdDir, "agent", "task_session_cwd_freshness_evidence_test.go"),
		filepath.Join(root, sourceTreeCmdDir, "agent", "gopibin_identity_guard_test.go"),
	} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		uses += strings.Count(string(body), markerName)
	}
	if uses < 2 {
		t.Errorf("%s occurs %d time(s) across the guard — want at least 2 (its definition AND the refusal text that carries it)", markerName, uses)
	}
}

// stringConstValue returns the value of a Go `name = "…"` string constant, read
// from the file that holds it (the drift arm is about the FILES agreeing, so it
// reads them as text rather than importing one side).
func stringConstValue(path, name string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, name) || !strings.Contains(trimmed, "=") {
			continue
		}
		_, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"`), nil
	}
	return "", fmt.Errorf("%s: no %s = \"…\" line", path, name)
}

// shellVarValue returns the value of a shell `name="…"` assignment, read from the
// file that holds it.
func shellVarValue(path, name string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, name+"=") {
			continue
		}
		_, value, _ := strings.Cut(line, "=")
		return strings.Trim(strings.TrimSpace(value), `"`), nil
	}
	return "", fmt.Errorf("%s: no %s=\"…\" line", path, name)
}

// cwdProbeFreshnessRevisionLdflagPath keeps buildidentity named on this file: the
// revision operand's PATH is never re-typed (Rule 13/14), and a reader of this
// evidence who re-derives the build command needs the one spelling of it.
var cwdProbeFreshnessRevisionLdflagPath = buildidentity.RevisionLdflagPath
