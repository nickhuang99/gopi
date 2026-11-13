package tools

// task_briefing_prose_test_names.go — the SEVENTH measurement on the dispatch
// seam, and the other half of the vacuum-green defect the sixth half opened.
//
// THE MEASURED CASE (bug row 01a18f86, 2026-10-31). That epic's acceptance read,
// in prose, verbatim: 「三条 delivery-retry 测试 … 必须转绿」 naming
// `TestDeliveryRetryBlockedThenDeliveredOnTheSamePath` and
// `TestDeliveryRetryIsIdempotentOnADeliveredRow` — two names that exist NOWHERE
// on that ground (commit `8d0ee448` rewrote/retired them). The executor ran
// `go test ./internal/tools/ -run '<those two names>'` and got
//
//	testing: warning: no tests to run
//	ok  	github.com/nick/gopi/internal/tools	4.299s [no tests to run]
//
// — exit 0, a green that measured nothing. Its own report states it: 「两个名字
// 在本树上匹配不到任何测试，这个"绿"是真空绿」.
//
// WHY THE SIXTH CLAUSE DOES NOT COVER IT, measured before this file was written
// (evidence/red-green-before.txt in this run's session directory): that clause
// reads `-run` TOKENS off a line that also names `go test`, because a `-run`
// outside such a sentence is somebody's prose rather than a selection. A briefing
// that names its tests in SENTENCES carries no `-run` token at all, so the sixth
// clause is silent on it — correctly, by its own contract — and the briefing
// reaches the child unjudged.
//
// WHAT IT DOES: collects the `Test…` identifiers the briefing names ANYWHERE (a
// sentence, a table cell, a bullet), resolves each against this ground's whole
// test corpus — every `*_test.go` under the ground, top-level `func Test…` — and
// stamps the ones that reach NO test function. A name is judged the way `go test
// -run` judges it: UNANCHORED, so a name that is a strict prefix of a live test
// is LIVE, not dead (an implementation that compared strings exactly would stamp
// a briefing whose command would in fact run tests — the same defect pointing the
// other way).
//
// WHAT IT MAY NOT CONCLUDE: never that the acceptance criterion was met. It says
// one NAME the briefing treats as evidence cannot be produced by any test in this
// tree, and that the child must find the test that exists or write the one it
// means. A briefing whose every name is live stamps nothing — the ordinary case.
//
// THE VACUUM FLOOR (Rule 15): a census that judged nothing never passes as green.
// A ground this clause cannot resolve, a walk that reached no `*_test.go`, a
// corpus that yielded no `func Test…` line, and a censused corpus larger than the
// cap are each recorded as UNMEASURED with the reason — never as "zero reach".
//
// IT IS A WARNING, NEVER A REFUSAL: a legitimate epic may name a test the child is
// being asked to WRITE, and this clause must not block that run. It says the name
// reaches nothing HERE, NOW, so the child cannot read a green as evidence.
//
// NO NEW MECHANISM (Rule 35): no tool, no script, no subprocess, no resident
// anything — one clause on a seam that already exists, reading files in this
// ground with the standard library. ADDITIVE by the same contract as its six
// siblings: best-effort, never errors, never blocks a run, silent on the ordinary
// briefing naming no test, and idempotent.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TaskBriefingProseTestNamesLabel heads the dead-name clause for names written in
// PROSE. Spelled once and exported for the same reason as its six siblings
// (Rules 1/13/14): producer, seam record and regression tests read ONE string.
const TaskBriefingProseTestNamesLabel = "[Briefing acceptance — a `Test…` name this briefing writes in PROSE reaches NO test function anywhere in this ground]"

// TaskBriefingProseTestNamesVerifyOnly is the instruction the clause carries.
const TaskBriefingProseTestNamesVerifyOnly = "NAME THE TEST THAT EXISTS (OR WRITE THE ONE YOU MEAN) AND SAY WHICH; a `go test -run <dead name>` prints `no tests to run` and exits 0"

const (
	// proseTestNamePrefix is the identifier prefix a Go test function's name
	// carries — the same prefix the sixth clause counts with, referenced rather
	// than re-typed (Rules 1/13).
	proseTestNamePrefix = runAltTestNamePrefix
	// proseTestNameHook is the one `Test…` identifier that is a hook, not a test
	// a `-run` pattern can select; the sixth clause refuses it by the same
	// constant.
	proseTestNameHook = runAltTestMainName
	// proseTestInstrument is the instrument whose presence means a line's names
	// belong to the SIXTH clause (a `go test … -run` command) rather than to
	// prose. This file skips such a line so one dead name is never stamped twice.
	proseTestInstrument = runAltGoTestInstrument
	// proseTestFlag is the flag that makes a line a SELECTION rather than a
	// sentence; beside proseTestInstrument it hands the line to the sixth clause.
	proseTestFlag = runAltFlagToken
	// proseTestIdentifierPattern matches the identifier shape a Go test name
	// carries: the prefix, then an uppercase first letter (Go's own convention
	// for an exported identifier), then identifier characters.
	proseTestIdentifierPattern = proseTestNamePrefix + `[A-Z][A-Za-z0-9_]*`
	// proseTestMaxNames caps how many names this clause will judge from ONE
	// briefing: a stamp goes into a child's prompt and must stay a stamp.
	proseTestMaxNames = 24
	// proseTestMaxDirs caps the directories one census walks.
	proseTestMaxDirs = 4000
	// proseTestMaxFiles caps the test files one census reads. MEASURED on this
	// rev: `./` carries 2192 `*_test.go` files (28.9 MB) across 86 directories,
	// read in 25 ms by a plain `cat` pipeline. The cap is set far above that and,
	// when it IS hit, the census is STATED as unmeasured rather than returned
	// short — a partial corpus turns a live name into a false ZERO, which is this
	// clause's own defect one level down.
	proseTestMaxFiles = 8000
	// proseTestMaxFileBytes caps one file's size, for the same reason.
	proseTestMaxFileBytes = 8 << 20
	// proseTestMinCensusNames is the VACUUM FLOOR: fewer top-level test names
	// than this means the SCAN failed, never that the briefing's names are dead.
	proseTestMinCensusNames = 1
	// proseTestMaxSkippedDirs are the directory names a census never descends
	// into: not this repo's Go source, and walking them would cost the seam
	// seconds for nothing.
	proseTestSkipGit        = ".git"
	proseTestSkipVendor     = "vendor"
	proseTestSkipNodeMods   = "node_modules"
	proseTestSkipHiddenDirs = "."
)

// The stated reasons a census or a name could not be judged. Named, because a
// bare sentence at a fmt site is the shape Rule 1 forbids and because the record
// is the only place a reader learns WHY a briefing went unjudged.
const (
	proseTestNoRepoReason      = "there is no ground to census the briefing's test names against"
	proseTestNoTestFilesReason = "no *_test.go file was found under this ground (a census that judged nothing is not a pass)"
	proseTestNoNamesReason     = "the ground's test files yielded no top-level `func Test…` line (a census that judged nothing is not a pass)"
	proseTestUnreadableReason  = "the ground could not be walked"
	proseTestTruncatedReason   = "this ground carries more test files (or larger ones) than this clause will read, and a PARTIAL census produces false ZERO verdicts — so it is stated, never returned as a count"
	proseTestTooManyNamesText  = "the briefing names more `Test…` identifiers than this clause will judge"
	proseTestAlreadyStamped    = "the briefing already carries a dead prose-test-name stamp (an earlier pass appended it); no name was judged and nothing was appended"
	// TaskBriefingProseTestNamesAlreadyStampedExcerpt is quoted in the record
	// when this clause meets its own stamp a second time.
	TaskBriefingProseTestNamesAlreadyStampedExcerpt = "the briefing already carries a dead prose-test-name stamp"
	// proseTestSiblingOwnedReason states why a line's names were handed to the
	// sixth clause rather than judged a second time here.
	proseTestSiblingOwnedReason = "the name stands on a `go test … -run` line, which the sixth clause judges (an alternative census); it is not judged twice"
)

const (
	// proseTestCensusRecordFmt is the CENSUS line — one per name judged,
	// whatever the count, so a zero is visible against its neighbours.
	proseTestCensusRecordFmt = "briefing prose test-name census → %q: %d top-level test function(s) of %d in this ground reach it\n"
	// proseTestUnresolvedRecordFmt is the stated arm: a name this clause could
	// not judge, which is never a pass (Rule 15).
	proseTestUnresolvedRecordFmt = "briefing prose test-name census → UNMEASURED (%s) — nothing was judged, which is not a pass: %s\n"
	// proseTestStampedRecordFmt is the record for a ZERO name.
	proseTestStampedRecordFmt = "briefing prose test-name census → ZERO REACH %q: no test function under this ground matches it — STAMPED into the child's briefing (%s)\n"
	// proseTestSiblingRecordFmt states the hand-off.
	proseTestSiblingRecordFmt = "briefing prose test-name census → SKIP %q (%s)\n"
	// proseTestCensusCostFmt states the measurement's own cost, because this
	// seam now reads a whole corpus and a reader is entitled to know what it
	// cost (measured, not estimated).
	proseTestCensusCostFmt = "briefing prose test-name census → ground %s: %d test file(s), %d top-level test function(s), walked in %s\n"
	// proseTestHeadFmt introduces the clause.
	proseTestHeadFmt = "%s\n" +
		"%d `Test…` name(s) this briefing writes as ACCEPTANCE EVIDENCE reach NO test function anywhere in this ground. `go test -run <such a name>` prints `no tests to run` and exits 0, so a report can cite a green that measured nothing.\n"
	// proseTestClaimFmt is the clause's per-name block: the name, the counts,
	// the instruction, the denial of the completion reading, the quoting line.
	proseTestClaimFmt = "- %q reaches NO test function under this ground (it carries %d top-level test function(s), and none matches).\n" +
		"  %s: run `grep -rn '^func %s' --include='*_test.go' .` HERE to read the names that exist, then re-write the acceptance text with a name that does — or, if the test is yours to write, say so in your report and do not cite a green from it.\n" +
		"  This clause is NOT evidence that the work is done, and it does not block the run: a name you are being asked to CREATE may legitimately not exist yet.\n" +
		"  briefing line: %q\n"
)

// proseTestNameLine is one name read off one briefing line. dedupe says this
// LINE was already quoted in the clause above (another dead name stood on it),
// so the line is pointed at rather than reprinted.
type proseTestNameLine struct {
	name   string
	line   string
	dedupe bool
}

// TaskBriefingProseTestNamesRefresh judges, at dispatch time, every `Test…`
// identifier the briefing names in PROSE against this ground's whole test corpus,
// and returns the clause to APPEND to the briefing the child receives plus the
// census record lines.
//
// Returns ("", nil) when the briefing names no `Test…` identifier outside a
// `go test … -run` command — the ordinary case.
func TaskBriefingProseTestNamesRefresh(repo, briefing string) (clause string, record []string) {
	if strings.Contains(briefing, TaskBriefingProseTestNamesLabel) {
		return "", []string{fmt.Sprintf(proseTestUnresolvedRecordFmt,
			proseTestAlreadyStamped, TaskBriefingProseTestNamesAlreadyStampedExcerpt)}
	}
	named, skipped := proseTestNamesIn(briefing)
	for _, s := range skipped {
		record = append(record, fmt.Sprintf(proseTestSiblingRecordFmt, s.name, proseTestSiblingOwnedReason))
	}
	if len(named) == 0 {
		return "", record
	}
	r := strings.TrimSpace(repo)
	if r == "" {
		return "", append(record, fmt.Sprintf(proseTestUnresolvedRecordFmt, proseTestNoRepoReason, proseTestLineOf(named)))
	}
	if len(named) > proseTestMaxNames {
		record = append(record, fmt.Sprintf(proseTestUnresolvedRecordFmt,
			proseTestTooManyNamesText, proseTestLineOf(named[proseTestMaxNames:])))
		named = named[:proseTestMaxNames]
	}
	started := time.Now()
	names, files, reason := proseTestCorpus(r)
	elapsed := time.Since(started)
	if reason != "" {
		return "", append(record, fmt.Sprintf(proseTestUnresolvedRecordFmt, reason, proseTestLineOf(named)))
	}
	record = append(record, fmt.Sprintf(proseTestCensusCostFmt, r, files, len(names), elapsed.Round(time.Millisecond)))
	var dead []proseTestNameLine
	quoted := map[string]bool{}
	for _, n := range named {
		re, err := regexp.Compile(n.name)
		if err != nil {
			record = append(record, fmt.Sprintf(proseTestUnresolvedRecordFmt, err.Error(), n.line))
			continue
		}
		hits := 0
		for _, real := range names {
			// UNANCHORED, exactly as `go test -run` matches: a name that is a
			// strict prefix of a live test SELECTS that test, so it is LIVE.
			if re.MatchString(real) {
				hits++
			}
		}
		record = append(record, fmt.Sprintf(proseTestCensusRecordFmt, n.name, hits, len(names)))
		if hits > 0 {
			continue
		}
		record = append(record, fmt.Sprintf(proseTestStampedRecordFmt, n.name, n.line))
		d := proseTestNameLine{name: n.name, line: n.line}
		d.dedupe = quoted[n.line]
		quoted[n.line] = true
		dead = append(dead, d)
	}
	if len(dead) == 0 {
		return "", record
	}
	var b strings.Builder
	fmt.Fprintf(&b, proseTestHeadFmt, TaskBriefingProseTestNamesLabel, len(dead))
	for _, d := range dead {
		fmt.Fprintf(&b, proseTestClaimFmt, d.name, len(names),
			TaskBriefingProseTestNamesVerifyOnly, runAltTestFuncPrefix, d.line)
		if d.dedupe {
			// The line already stands, in full, above (it named another dead
			// name too) — a pointer, never a truncation.
			fmt.Fprint(&b, taskBriefingRunAltLineRepeatNote)
		}
	}
	return strings.TrimRight(b.String(), "\n"), record
}

// proseTestNamesIn extracts every `Test…` identifier the briefing names in prose,
// plus the ones this file hands to the sixth clause (a `go test … -run` line).
// Duplicates are collapsed: a name written twice is one finding.
//
// THE BOUNDARY RULE, and why it is not optional (MEASURED, first real run of this
// clause: the e2e in cmd/agent stamped a name that was a GROUND PATH). By the time
// this clause runs, the briefing already carries the child's ground announcement —
// a DIRECTORY PATH such as
// `/dev/shm/suite-cmd-agent-run-…/TestTaskRunStaysSilentOnAProseBriefingOfLiveNames3952180565/005/gopi-…`,
// and a Go test's temp directory is named after the TEST THAT CREATED IT. So a
// path component is very often spelled exactly like a test name, and a census that
// read every `Test…` substring would stamp the run's own ground directory as a dead
// test — a false accusation on the one briefing that is perfectly correct.
//
// So a candidate counts only when it stands as its OWN token: the byte before it is
// absent or is not an identifier/path character (no alnum, `_`, `-`, `.`, `/`), and
// the byte after it is not alnum/`_` (the regex is greedy, so that half is Go's own
// rule). `TestFoo/Sub` still counts — the separator there FOLLOWS the name — while
// `…/TestFoo1234/` and `MyTestFoo` do not.
func proseTestNamesIn(briefing string) (named, skipped []proseTestNameLine) {
	re := regexp.MustCompile(proseTestIdentifierPattern)
	seen := map[string]bool{}
	for _, line := range strings.Split(briefing, "\n") {
		sibling := strings.Contains(line, proseTestInstrument) && strings.Contains(line, proseTestFlag)
		for _, loc := range re.FindAllStringIndex(line, -1) {
			name := line[loc[0]:loc[1]]
			if !proseTestTokenBoundary(line, loc[0], loc[1]) {
				continue
			}
			if name == proseTestNameHook {
				// TestMain is a hook, not a test a `-run` pattern selects — the
				// sixth clause refuses it by the same constant.
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			n := proseTestNameLine{name: name, line: taskBriefingRunAltLineExcerpt(line)}
			if sibling {
				skipped = append(skipped, n)
				continue
			}
			named = append(named, n)
		}
	}
	return named, skipped
}

// proseTestTokenBoundary reports whether the candidate at [start,end) of line
// stands as its own token — see proseTestNamesIn's measured note. A path
// separator, a hyphen, a dot, an underscore or an alphanumeric character directly
// BEFORE the name means it is a path component or a longer identifier, not a name
// the briefing writes as evidence.
func proseTestTokenBoundary(line string, start, end int) bool {
	if start > 0 {
		prev := line[start-1]
		if prev == '/' || prev == '\\' || prev == '-' || prev == '.' || prev == '_' ||
			(prev >= '0' && prev <= '9') || (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') {
			return false
		}
	}
	if end < len(line) {
		next := line[end]
		if next == '_' || (next >= '0' && next <= '9') ||
			(next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') {
			return false
		}
	}
	return true
}

// proseTestLineOf renders the briefing lines a stated arm could not judge, as a
// pointer a reader can follow (never a truncation of the briefing itself).
func proseTestLineOf(named []proseTestNameLine) string {
	if len(named) == 0 {
		return "the briefing names no Test… identifier in prose"
	}
	var names []string
	for _, n := range named {
		names = append(names, n.name)
	}
	return strings.Join(names, ", ")
}

// proseTestCorpus reads this ground's whole test corpus: every top-level
// `func Test…` name under `repo`, walking `*_test.go` files only. It is the
// population a prose-named test must exist in, and the place the vacuum floor
// lives — a walk that judged nothing states WHY rather than returning an empty
// list that would make every name look dead.
func proseTestCorpus(repo string) (names []string, files int, reason string) {
	rootAbs, err := filepath.Abs(repo)
	if err != nil {
		return nil, 0, fmt.Sprintf("%s: %v", proseTestUnreadableReason, err)
	}
	if info, err := os.Stat(rootAbs); err != nil || !info.IsDir() {
		return nil, 0, fmt.Sprintf("%s: %v", proseTestUnreadableReason, err)
	}
	dirs, truncated := 0, false
	walkErr := filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory makes the corpus PARTIAL, and a partial
			// corpus turns a live name into a false ZERO. Stated, never skipped.
			truncated = true
			return nil
		}
		if d.IsDir() {
			dirs++
			if dirs > proseTestMaxDirs {
				truncated = true
				return fs.SkipAll
			}
			name := d.Name()
			if path != rootAbs && (name == proseTestSkipGit || name == proseTestSkipVendor ||
				name == proseTestSkipNodeMods || strings.HasPrefix(name, proseTestSkipHiddenDirs)) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), runAltTestFileSuffix) {
			return nil
		}
		files++
		if files > proseTestMaxFiles {
			truncated = true
			return fs.SkipAll
		}
		info, err := d.Info()
		if err != nil || info.Size() > proseTestMaxFileBytes {
			truncated = true
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			truncated = true
			return nil
		}
		for _, line := range strings.Split(string(body), "\n") {
			t := strings.TrimSpace(line)
			// A test function OPENS with the prefix at the start of a
			// left-trimmed line; a comment, a string literal or a call site
			// never does. The name reader is the sixth clause's, referenced
			// rather than re-implemented (Rules 1/13/14) — so the two clauses
			// cannot disagree about what counts as a test function.
			if !strings.HasPrefix(t, runAltTestFuncPrefix) {
				continue
			}
			if name, ok := runAltTestFuncName(t); ok {
				names = append(names, name)
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, files, fmt.Sprintf("%s: %v", proseTestUnreadableReason, walkErr)
	}
	if truncated {
		return nil, files, proseTestTruncatedReason
	}
	if files == 0 {
		return nil, 0, proseTestNoTestFilesReason
	}
	if len(names) < proseTestMinCensusNames {
		return nil, files, proseTestNoNamesReason
	}
	sort.Strings(names)
	return names, files, ""
}
