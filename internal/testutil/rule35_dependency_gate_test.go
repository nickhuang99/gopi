// The RULE-35 half of the scripts gate (PROJECT.md Rule 35: "Zero Dependency Is a
// Discipline — Nothing New Is Created Casually"). The rule's own post-mortem is
// a cron line nobody knew about: `*/10 * * * * /home/nick/bin/hp-worker-autostart.sh`,
// installed by a task that had been asked to fix two launchers, 1,214
// delegations later still running, COVERING a worker-death loop. This file makes
// the shapes a gate CAN see a build failure, and — just as important — says out
// loud which half it cannot see.
//
// The rule requires the four STATEMENTS (what breaks without it / why it cannot
// be fixed in-process / who owns its lifecycle / what the next reader sees). No
// static check can read an author's mind, so this gate does not try: it detects
// the ARTIFACTS of a persistent dependency and forces the four statements into
// the review. That is a detector, not a proof of intent.
//
// WHAT IT CAN SEE (repo-side, three shapes, each zero-tolerance):
//
//  1. SELF-SCHEDULING SCRIPT — an audited script that WRITES a scheduler: a
//     crontab-installing pipe (`… | crontab -`) or a non-comment line carrying a
//     literal cron EXPRESSION (`*/30 * * * * cmd`). A script may carry cron
//     DOCUMENTATION; it may not carry the entry.
//  2. HOST-LEVEL MUTATION — an audited script appending/replacing content in the
//     operator's host configuration: a `>>`/`>`/`tee -a` into .bashrc, .profile,
//     .bash_profile, environment.d, autostart, or tmpfiles.d. Rule 22 forbids the
//     host-level fix by name; this is the machine check behind that sentence.
//  3. TRACKED RUNTIME STATE — a git-TRACKED path that is runtime state by shape:
//     `*.lock` / `*.pid`, a `.gopi/memory.db` or `.gopi/tasks.db`, a
//     scratch-queue entry. A file the source tree carries but the runtime owns is
//     state that outlives every run that wrote it.
//
// WHAT IT CANNOT SEE (the honest half — no test in this repo can decide these):
//
//  * a crontab line already INSTALLED in the operator's crontab (`crontab -l`
//    state, not tree content);
//  * a systemd unit/timer already installed under ~/.config/systemd/user;
//  * a RESIDENT process already running on this host;
//  * any dependency that leaves NO artifact in this tree at all.
//
// Those four are answered by the ONE-SHOT verb `gopi tool scratch-gc deps` (this
// repo's existing sweep verb, arm `deps`) — the shape this gate structurally
// cannot check is exactly the shape that verb reports. Neither half is a
// substitute for the other; together they are the coverage.
//
// A detector that cannot tell a quotation from a command gets relaxed to a
// no-op the first time it fires on prose (the /tmp ban's isCommentLine lesson,
// and this repo's own devdoc/ evidence files quote the retired cron line). So
// every rule below strips comments first.

package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// rule35CrontabBin is the crontab BINARY whose invocation installs an entry:
	// named so the detector and the messages that cite it cannot drift.
	rule35CrontabBin = "crontab"

	// rule35MaxReportedHits caps one rule's listing; the total is always stated
	// alongside it (the withheld-hits contract this file's siblings established).
	rule35MaxReportedHits = 15

	// rule35MinScannedFiles is the non-vacuity floor: three audited roots hold
	// well over a hundred .sh/.py files, so a scan that finds a handful is
	// looking in the wrong place (a moved directory must not turn this into a
	// vacuous pass).
	rule35MinScannedFiles = 40
)

// rule35AuditedRoots are the roots whose SCRIPTS this gate reads: the same three
// the /tmp ban walks, for the same reason — a persistent dependency is created
// by an entry point, and these are the repo's entry points.
var rule35AuditedRoots = []string{scriptsDirName, devdocDirName, testdataDirName}

// rule35CronExpressionRes match a literal cron EXPRESSION — five fields, each
// made of the cron alphabet, followed by the command. The trailing `\s` is what
// keeps the rule honest: it separates a schedule from prose about scheduling.
// rule35CronShorthandRes match the shorthand schedule spellings, which install
// just as well as five fields.
var rule35CronShorthandRes = regexp.MustCompile(`@(reboot|yearly|annually|monthly|weekly|daily|hourly|midnight)\s+\S`)

// rule35CronFieldOK reports whether a token is a valid cron TIME field. Two
// bounds keep the detector off prose, and both were MEASURED false positives
// before they were added:
//
//   - a field must START and END with a digit or `*`. A trailing comma belongs
//     to an integer tuple, never to a cron field:
//     `for d in (0, 1, 7, 30, 90, 180, 365):` (scripts/test_s3loggrowth.py)
//     matched a laxer class;
//   - a single digit IS a valid field, which is why the POSITION check in
//     rule35CronExpressionIn is not optional: `for i in 0 1 2 3 4 5 6 7 8 9`
//     (scripts/evidence-pushonly-taskid.sh) is five valid fields in a row.
func rule35CronFieldOK(field string) bool {
	if field == "" {
		return false
	}
	edge := func(b byte) bool { return (b >= '0' && b <= '9') || b == '*' }
	if !edge(field[0]) || !edge(field[len(field)-1]) {
		return false
	}
	for i := 0; i < len(field); i++ {
		switch c := field[i]; {
		case c >= '0' && c <= '9', c == '*', c == '/', c == ',', c == '-':
		default:
			return false
		}
	}
	return true
}

// rule35SchedulePrefixes are the characters a schedule may follow: a cron entry
// begins a LINE, a quoted string, a shell assignment or a command substitution.
// A bare word before the fields (`for i in …`) is prose about numbers, not a
// schedule — requiring the POSITION is what keeps that control green.
const rule35SchedulePrefixes = "\"'=("

// rule35CronExpressionIn reports whether a script line carries a literal cron
// EXPRESSION (five time fields followed by the command). Comments are stripped
// by the caller: this repo's devdoc/ evidence files QUOTE the retired cron line,
// and a detector that fires on a quotation gets relaxed the first time a
// reviewer meets it.
func rule35CronExpressionIn(line string) bool {
	if rule35CronShorthandRes.MatchString(strings.TrimSpace(line)) {
		return true
	}
	for _, start := range rule35ScheduleStarts(line) {
		if rule35FiveFieldsThenCommand(line[start:]) {
			return true
		}
	}
	return false
}

// rule35ScheduleStarts returns the offsets in a line where a cron EXPRESSION
// may begin: the first non-space character, and every position just after a
// quoting/assignment/subshell character. Tokenising from an arbitrary offset is
// what makes `entry="*/10 * * * * cmd"` visible — its first whitespace-split
// token is `entry="*/10`, which is no cron field at all.
func rule35ScheduleStarts(line string) []int {
	var starts []int
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == ' ' || c == '\t' {
			continue
		}
		if i == 0 || strings.ContainsRune(rule35SchedulePrefixes, rune(line[i-1])) {
			starts = append(starts, i)
		}
	}
	return starts
}

// rule35FiveFieldsThenCommand reports whether a fragment is a cron schedule:
// five valid time fields followed by a COMMAND (a token that is a command, not
// a sixth field — a schedule with nothing to run schedules nothing).
func rule35FiveFieldsThenCommand(frag string) bool {
	fields := strings.Fields(frag)
	if len(fields) < 6 {
		return false
	}
	for _, f := range fields[:5] {
		if !rule35CronFieldOK(f) {
			return false
		}
	}
	return rule35ActionTokenIsCommand(fields[5])
}

// rule35CronQuoteChars are the quotes a schedule's COMMAND token may carry only
// as a WRAPPER (the quotes closing `entry="*/30 * * * * cmd"`). Cron's five time
// fields are digits and operators, so a quote surviving INSIDE the token that
// follows them says that token is not a command word but a fragment of another
// language's literal.
const rule35CronQuoteChars = "\"'"

// rule35CommandTailPunct is the shell punctuation that may follow a wrapper's
// closing quote when the schedule sits at the tail of a command substitution
// (`(crontab -l; echo "0 3 * * 1 /x.sh") | crontab -`). It is trimmed only
// AFTER the five fields, never before them, and it never widens the field test.
const rule35CommandTailPunct = ");"

// rule35ActionTokenIsCommand reports whether the token following the five time
// fields can be a cron COMMAND rather than the tail of some other language's
// field LAYOUT. Two rules, the second measured against a real false positive
// (2026-09-27):
//
//   - the token may not itself be a valid time field: six fields in a row are
//     ambiguous, and `for i in 0 1 2 3 4 5 6 7 8 9` is a loop bound, not a
//     schedule (scripts/evidence-pushonly-taskid.sh);
//   - the token may carry a quote ONLY as a wrapper's closing quote. This is the
//     MEASURED false positive that made the rule: `split("3 4 5 6 7 8",cols," ")`
//     (scripts/materials-coverage.sh:242) reaches the field test with five clean
//     digit tokens ONLY because the opening quote was left behind at the
//     fragment start, and the sixth token then straddles two awk string literals
//     (`8",cols,"`) — the surviving quote is the evidence that the fragment is an
//     awk FIELD LAYOUT (it carves columns of a TSV, with the comma as its own
//     delimiter), and an awk layout schedules nothing. A wrapper's closing quote
//     is dropped together with the shell punctuation that may follow it, so the
//     quoted-assignment and command-substitution spellings keep matching.
func rule35ActionTokenIsCommand(action string) bool {
	if rule35CronFieldOK(action) {
		return false
	}
	tok := strings.TrimRight(action, rule35CommandTailPunct)
	return !strings.ContainsAny(rule35TrimmedQuotes(tok), rule35CronQuoteChars)
}

// rule35TrimmedQuotes drops ONE trailing quote from a token, never a leading
// one: a schedule may be wrapped as a whole (`entry="*/10 * * * * cmd"`), and
// the closing quote of that wrapper is not part of the command word. The
// leading quote of a wrapper is not reachable here — rule35ScheduleStarts begins
// the fragment AT the token after it — which is why only the trailing edge is
// trimmed.
func rule35TrimmedQuotes(token string) string {
	if token == "" {
		return token
	}
	if strings.ContainsRune(rule35CronQuoteChars, rune(token[len(token)-1])) {
		return token[:len(token)-1]
	}
	return token
}

// rule35CommandSeparators splits a shell line where one command ends and the
// next begins (the same deliberately-small set the baseline gate uses).
var rule35CommandSeparators = regexp.MustCompile(`&&|\|\||;|\|`)

// rule35CrontabInstallIn reports whether a script line INSTALLS a crontab. The
// check is on the COMMAND POSITION, never on the bare word: measured false
// positives of a substring test were `sec "root crontab / cron.d"` and
// `echo "--- crontab (user) ---"` (devdoc/daemon-survives-logout-probes/) — both
// DESCRIBE a crontab, neither installs one. READING is never flagged: the
// discovery arm exists to read it.
func rule35CrontabInstallIn(line string) bool {
	for _, seg := range rule35CommandSeparators.Split(line, -1) {
		seg = strings.TrimSpace(seg)
		for {
			switch {
			case strings.HasPrefix(seg, "if "):
				seg = strings.TrimSpace(seg[len("if "):])
			case strings.HasPrefix(seg, "then "):
				seg = strings.TrimSpace(seg[len("then "):])
			case strings.HasPrefix(seg, "!"):
				seg = strings.TrimSpace(seg[1:])
			case strings.HasPrefix(seg, "{"):
				seg = strings.TrimSpace(seg[1:])
			case strings.HasPrefix(seg, "}"):
				seg = strings.TrimSpace(seg[1:])
			default:
				goto done
			}
		}
	done:
		if !strings.HasPrefix(seg, rule35CrontabBin) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(seg, rule35CrontabBin))
		// `crontab -l` READS. Anything else — `crontab -`, `crontab <file>`,
		// `crontab -u nick <file>` — installs.
		if rest == "" || strings.HasPrefix(rest, "-l") || strings.HasPrefix(rest, "--list") {
			continue
		}
		return true
	}
	return false
}

// rule35HostMutationRes match content being WRITTEN into the operator's host
// configuration. Rule 22 names these files as out of scope for gopi's own
// process tree; a script that edits them is the violation, not the fix.
var rule35HostMutationRes = []*regexp.Regexp{
	regexp.MustCompile(`(>>?|tee +-a?)\s*[^|;&]*\.(bashrc|profile|bash_profile|bash_login|zshenv|zshrc|bash_aliases)\b`),
	regexp.MustCompile(`(>>?|tee +-a?)\s*[^|;&]*(environment\.d|\.config/autostart|tmpfiles\.d)\b`),
}

// rule35CronReviewExemptRels are files allowed to carry the WITNESS. Two of
// them are the same review, and it is the one Rule 35 itself ratifies: an
// operator-chosen host-level upkeep unit is a persistent dependency that a HUMAN
// decided on (Rule 34's timer, the watchdog). The gate cannot tell that from
// the retired autostart cron line — so a file that installs a schedule may not
// be silently grandfathered by a pattern; it is NAMED here, one sentence each,
// and the sentence is the review.
//
// Rule 35's remaining text is what makes this list small rather than a
// loophole: a dependency must refuse by default and explain itself, and this
// list is the machine-readable form of "an operator read its reason".
var rule35CronReviewExemptRels = map[string]string{
	"scripts/install-scratch-gc-watchdog.sh": "installs the operator's own 30-minute scratch-GC " +
		"watchdog timer (an OS timer, per Rule 34; the unit is in scripts/systemd/ with its reasoning in its header) " +
		"and falls back to a crontab only when no user bus answers",
	"scripts/gopi-home-gc.sh": "installs the same operator-chosen upkeep as a weekly crontab " +
		"entry when the systemd --user route is unavailable",
}

// rule35StateShapeRes match a TRACKED path that is runtime state by SHAPE. The
// path is judged, not its content: a source file named like a lock is the same
// defect as a lock whose bytes are committed.
var rule35StateShapeRes = []*regexp.Regexp{
	regexp.MustCompile(`(^|/)\.gopi/(memory|tasks)\.db$`),
	regexp.MustCompile(`\.(lock|pid)$`),
	regexp.MustCompile(`(^|/)scratch-queue(\.|\b|/)`),
}

// rule35StateExemptRels are TRACKED paths the gate accepts by SHAPE, each with
// its reason. The tracked `sandbox/` trees are the shape that FORCED this to be
// a list rather than a pattern: three agent sandboxes were committed to this
// index (measured on `sandbox/abtest`, `sandbox/abtest2`, `sandbox/sbx` — a
// sandbox's own ledger, lock and session logs), which is EXACTLY the artifact
// Rule 34 says belongs where the runtime owns it, outside the source tree. So
// the rule flagged them and the review answered — one sentence per path, and a
// reviewer who disagrees deletes the tree instead of editing the list.
//
// THE TABLE IS NOW EMPTY, and that is the ratchet working: scope 3/5 of epic
// `01a0fd2e` untracked `sandbox/**` outright, so the reviewer who disagreed
// deleted the trees. Every permit below is retained as a comment rather than an
// entry, because a permit whose path is gone is an ERROR here (see
// TestRule35RealTreeControlsAreClean) — re-adding one without its file would
// make the gate red, which is the point.
var rule35StateExemptRels = map[string]string{
	// RETIRED — the paths are no longer tracked (untracked by the sandbox/
	// evidence removal, epic 01a0fd2e scope 3/5). Their reasons, kept for the
	// record:
	//   sandbox/abtest/.gopi/memory.db  — committed sandbox ledger, rescued by
	//     6a6aef6d (2026-09-17); not a convention: a sandbox belongs under the
	//     sandbox root (Rule 34)
	//   sandbox/abtest2/.gopi/memory.db — same review as sandbox/abtest
	//   sandbox/sbx/.gopi/memory.db     — same review as sandbox/abtest
	//   sandbox/sbx/.gopi/tasks.db      — committed sandbox task ledger, same
	//     review as sandbox/abtest
	//   sandbox/sbx/.gopi/serve.lock    — committed sandbox singleton lock (an
	//     empty placeholder; the flock is the kernel's), kept as evidence of the
	//     rescue
	//
	// The whole tree is now ignored by the root-anchored `/sandbox/` entry in
	// .gitignore, so no path of this shape can return to the index as tracked
	// state; a new sandbox is obtained by ASKING (`gopi tool sandbox create`, Rule 34).
}

// rule35FileExemptRels are files allowed to CARRY the detector's own patterns
// (this gate, whose messages quote them).
var rule35FileExemptRels = map[string]string{
	"internal/testutil/rule35_dependency_gate_test.go": "the gate's own pattern definitions",
}

// rule35Hit is one detected shape.
type rule35Hit struct {
	rel  string // repo-root-relative, slash-separated
	line int
	text string
	rule string
}

// rule35File is one audited script, reduced to the lines a detector judges with
// comments removed (index-aligned with the file's own lines so a hit still
// reports a REAL line number).
type rule35File struct {
	rel   string
	lines []string // comment-stripped, blank for a comment line
}

// stripRule35Comment removes a script's comment body: `#` for shell and python,
// and `//` for the Go fixtures inside testdata/. A line that is ONLY a comment
// becomes empty; a trailing comment is cut at its marker. Quotation is the
// reason: this repo's devdoc/ files QUOTE the retired cron line, and a detector
// that fires on prose is a detector that gets relaxed.
func stripRule35Comment(line string) string {
	out := line
	for _, marker := range []string{"#", "//"} {
		if i := strings.Index(out, marker); i >= 0 {
			out = out[:i]
		}
	}
	return strings.TrimSpace(out)
}

// collectRule35Scripts walks the audited roots and returns every .sh/.py file
// with its comment-stripped lines.
func collectRule35Scripts(t *testing.T, root string) []rule35File {
	t.Helper()
	var out []rule35File
	for _, dirName := range rule35AuditedRoots {
		base := filepath.Join(root, dirName)
		if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
			continue // an absent root is not a failure (a docs-only checkout)
		}
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".sh") && !strings.HasSuffix(name, ".py") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			lines := strings.Split(string(raw), "\n")
			stripped := make([]string, len(lines))
			for i, l := range lines {
				stripped[i] = stripRule35Comment(l)
			}
			out = append(out, rule35File{rel: filepath.ToSlash(rel), lines: stripped})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	return out
}

// rule35ScriptHits applies the two SCRIPT rules (self-scheduling, host
// mutation) to one file.
func rule35ScriptHits(f rule35File) []rule35Hit {
	var hits []rule35Hit
	if _, exempt := rule35FileExemptRels[f.rel]; exempt {
		return nil
	}
	for i, line := range f.lines {
		if line == "" {
			continue
		}
		if rule35CrontabInstallIn(line) {
			if _, exempt := rule35CronReviewExemptRels[f.rel]; !exempt {
				hits = append(hits, rule35Hit{rel: f.rel, line: i + 1, text: line, rule: "self-scheduling: crontab install"})
				continue
			}
		}
		if rule35CronExpressionIn(line) {
			if _, exempt := rule35CronReviewExemptRels[f.rel]; !exempt {
				hits = append(hits, rule35Hit{rel: f.rel, line: i + 1, text: line, rule: "self-scheduling: cron expression"})
				continue
			}
		}
		if rule35Matches(rule35HostMutationRes, line) {
			hits = append(hits, rule35Hit{rel: f.rel, line: i + 1, text: line, rule: "host-level mutation"})
		}
	}
	return hits
}

// rule35Matches reports whether any pattern matches the line.
func rule35Matches(res []*regexp.Regexp, line string) bool {
	for _, re := range res {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func gitLsFiles(root string) (string, error) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git ls-files in %s: %w", root, err)
	}
	return strings.ReplaceAll(string(out), "\x00", "\n"), nil
}

// rule35TrackedPaths runs `git ls-files` in root and returns the tracked paths
// (slash-separated). The scan is over the INDEX, not the working tree: an
// untracked lock file is the runtime's business, a COMMITTED one is the defect.
func rule35TrackedPaths(t *testing.T, root string) []string {
	t.Helper()
	out, err := gitLsFiles(root)
	if err != nil {
		t.Skipf("git ls-files unavailable (%v) — the runtime-state rule needs the index", err)
	}
	paths := strings.Split(out, "\n")
	kept := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kept = append(kept, filepath.ToSlash(p))
	}
	return kept
}

// rule35StateHits applies the tracked-runtime-state rule to the index.
func rule35StateHits(paths []string) []rule35Hit {
	var hits []rule35Hit
	for _, p := range paths {
		if _, exempt := rule35StateExemptRels[p]; exempt {
			continue
		}
		if rule35Matches(rule35StateShapeRes, p) {
			hits = append(hits, rule35Hit{rel: p, text: p, rule: "tracked runtime state"})
		}
	}
	return hits
}

// TestRule35NoCasualPersistentDependency is the repo-side gate: the shapes a
// static check CAN see, in one audit. It is deliberately one test, so a
// contributor reads a single report and fixes the tree once.
func TestRule35NoCasualPersistentDependency(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	files := collectRule35Scripts(t, root)
	if len(files) < rule35MinScannedFiles {
		t.Fatalf("the Rule 35 audit scanned %d script file(s), want at least %d — "+
			"the scan is not looking where the entry points are (vacuous pass guard)",
			len(files), rule35MinScannedFiles)
	}
	var hits []rule35Hit
	for _, f := range files {
		hits = append(hits, rule35ScriptHits(f)...)
	}
	paths := rule35TrackedPaths(t, root)
	stateHits := rule35StateHits(paths)
	hits = append(hits, stateHits...)

	byRule := map[string]int{}
	for _, h := range hits {
		byRule[h.rule]++
	}
	t.Logf("rule35 audit: %d script file(s), %d tracked path(s), %d hit(s) %v",
		len(files), len(paths), len(hits), byRule)

	for _, h := range hits {
		where := h.rel
		if h.line > 0 {
			where = h.rel + ":" + itoa(h.line)
		}
		t.Errorf("%s — %s: %q\n"+
			"  Rule 35: a persistent dependency is created only after its four statements are written "+
			"in the task's report (what breaks without it / why it cannot be fixed in-process / who owns "+
			"its lifecycle and how it is reclaimed / what the next reader sees).\n"+
			"  If this is a REVIEWED dependency, it carries its own refusal-by-default and self-explanation "+
			"(the WORKER-OWNERSHIP: OPERATOR-ONLY header is the precedent) — name that in the task report.",
			h.rule, where, h.text)
	}
	// The withheld-hits contract: a capped report that does not state its total
	// lets a reader treat a partial list as the whole defect.
	if extra := len(hits) - rule35MaxReportedHits; extra > 0 {
		t.Errorf("… and %d more hit(s) not listed above (the total above is the blast radius)", extra)
	}
	if len(hits) > 0 {
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].rel != hits[j].rel {
				return hits[i].rel < hits[j].rel
			}
			return hits[i].line < hits[j].line
		})
	}
}

// TestRule35CronExpressionPredicateTightened is the regression guard for the
// MEASURED false positive of 2026-09-27, planted as a table of literal lines
// through the predicate itself (no exemption list is consulted here, so no file
// can be grandfathered into passing): scripts/materials-coverage.sh:242 carries
// the awk field LAYOUT `split("3 4 5 6 7 8",cols," ")`, whose leading integers
// look like five cron fields to a laxer detector. The controls below are the
// shapes the predicate EXISTS for — if the tightening ever costs one of them,
// this test is red, which is the only way "tighter" can be distinguished from
// "quieter".
func TestRule35CronExpressionPredicateTightened(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool // true = this line carries a cron EXPRESSION
	}{
		// ---- the measured false positive (must NOT be flagged) ----
		{
			name: "awk field layout: split(\"3 4 5 6 7 8\",cols,\" \")",
			line: `      split("3 4 5 6 7 8",cols," ");`,
			want: false,
		},
		{
			name: "the same layout as an awk program on one line",
			line: `awk -F'\t' '{ split("3 4 5 6 7 8",cols," "); print cols[1] }' "$ROWS"`,
			want: false,
		},
		{
			name: "an integer tuple in parentheses",
			line: `for d in (0, 1, 7, 30, 90, 180, 365):`,
			want: false,
		},
		{
			name: "a shell loop bound",
			line: `for i in 0 1 2 3 4 5 6 7 8 9; do echo "$i"; done`,
			want: false,
		},

		// ---- the true positives the tightening must keep ----
		{
			name: "a plain installed cron line",
			line: `*/10 * * * * /home/nick/bin/hp-worker-autostart.sh`,
			want: true,
		},
		{
			name: "a schedule inside a quoted shell assignment",
			line: `  local entry="*/30 * * * * ${stable_watchdog} >/dev/null 2>&1"`,
			want: true,
		},
		{
			name: "@reboot shorthand",
			line: `@reboot /home/nick/bin/gopi serve`,
			want: true,
		},
		{
			name: "a schedule in a command substitution",
			line: `(crontab -l; echo "0 3 * * 1 /home/nick/bin/gopi-home-gc.sh") | crontab -`,
			want: true,
		},
	}

	for _, tc := range cases {
		if got := rule35CronExpressionIn(tc.line); got != tc.want {
			t.Errorf("%s: flagged = %v, want %v\n  input: %q\n"+
				"  A tighter predicate that drops a true positive is a quieter gate, not a better one; "+
				"one that keeps this false positive turns the whole Rule 35 audit red on an untouched tree.",
				tc.name, got, tc.want, tc.line)
		}
	}
}

// TestRule35DetectorFlagsASyntheticShape is the detector's OWN red-proof and
// negative control in one table: every PLANTS case must be flagged (a detector
// that silently matched nothing would otherwise read as a green gate), and every
// CONTROL case must not — including the two shapes this repo really carries
// (the crontab-installing watchdog installer under scripts/, and a devdoc
// evidence file QUOTING the retired cron line). A negative control that is not
// taken from the real tree proves nothing about the real tree.
func TestRule35DetectorFlagsASyntheticShape(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		body string
		want bool // true = this body MUST be flagged
	}{
		// ---- PLANTS (red-proof: each must be caught) ----
		{
			name: "crontab install pipe",
			rel:  "scripts/planted-installer.sh",
			body: "  printf '%s\\n' \"$entry\" | crontab -\n",
			want: true,
		},
		{
			name: "literal cron expression in a script",
			rel:  "scripts/planted-scheduler.sh",
			body: "entry=\"*/10 * * * * /home/nick/bin/hp-worker-autostart.sh\"\n",
			want: true,
		},
		{
			name: "@reboot shorthand",
			rel:  "scripts/planted-reboot.sh",
			body: "echo \"@reboot /home/nick/bin/gopi serve\" >> \"$HOME/.config/autostart/x\"\n",
			want: true,
		},
		{
			name: "writing into the operator's bashrc",
			rel:  "scripts/planted-bashrc.sh",
			body: "echo 'export GOPI_HOME=/x' >> ~/.bashrc\n",
			want: true,
		},
		{
			name: "appending to systemd environment.d",
			rel:  "scripts/planted-envd.sh",
			body: "printf 'X=1\\n' >> \"$HOME/.config/environment.d/gopi.conf\"\n",
			want: true,
		},
		{
			name: "a tracked lock file",
			rel:  "sandbox/planted/.gopi/serve.lock",
			body: "",
			want: true,
		},
		{
			name: "a tracked ledger database",
			rel:  "sandbox/planted/.gopi/memory.db",
			body: "",
			want: true,
		},
		{
			name: "a tracked pid file",
			rel:  "scripts/planted.pid",
			body: "",
			want: true,
		},

		// ---- NEGATIVE CONTROLS (none may be flagged) ----
		{
			name: "READING a crontab is the discovery verb's job, not a dependency",
			rel:  "scripts/planted-read.sh",
			body: "crontab -l 2>/dev/null || true\n",
			want: false,
		},
		{
			name: "a COMMENT quoting the retired cron line is documentation",
			rel:  "devdoc/planted-evidence.md.sh",
			body: "# */10 * * * * /home/nick/bin/hp-worker-autostart.sh   <- the line nobody knew about\n",
			want: false,
		},
		{
			name: "the real cron-expr carrying installer (scripts/install-scratch-gc-watchdog.sh)",
			rel:  "scripts/install-scratch-gc-watchdog.sh",
			body: "  local entry=\"*/30 * * * * ${stable_watchdog} >/dev/null 2>&1\"\n",
			want: false,
		},
		{
			name: "an UNTRACKED lock is the runtime's business",
			rel:  "sandbox/planted/.gopi/serve.lock",
			body: "",
			want: false,
		},
		{
			name: "a systemd unit template in the tree is a unit file, not a tracked runtime artifact",
			rel:  "scripts/systemd/gopi-scratch-gc-watchdog.timer",
			body: "",
			want: false,
		},
		{
			name: "an ordinary script with a loop bound (no cron alphabet)",
			rel:  "scripts/planted-loop.sh",
			body: "for i in 0 1 2 3 4 5 6 7 8 9; do echo \"$i\"; done\n",
			want: false,
		},
	}

	for _, tc := range cases {
		var got bool
		var why string
		if strings.HasSuffix(tc.rel, ".sh") || strings.HasSuffix(tc.rel, ".py") {
			lines := strings.Split(tc.body, "\n")
			stripped := make([]string, len(lines))
			for i, l := range lines {
				stripped[i] = stripRule35Comment(l)
			}
			hits := rule35ScriptHits(rule35File{rel: tc.rel, lines: stripped})
			got = len(hits) > 0
			if got {
				why = hits[0].rule
			}
		} else {
			tracked := []string{tc.rel}
			if tc.name == "an UNTRACKED lock is the runtime's business" {
				tracked = nil // the index does not carry it — that is the whole control
			}
			hits := rule35StateHits(tracked)
			got = len(hits) > 0
			if got {
				why = hits[0].rule
			}
		}
		if got != tc.want {
			t.Errorf("%s: flagged = %v (rule %q), want %v\n  input: %q\n"+
				"  A detector that misses a plant is a vacuous gate; one that fires on a control "+
				"gets relaxed to a no-op the first time a reviewer meets it.",
				tc.name, got, why, tc.want, strings.TrimSpace(tc.body))
		}
	}
}

// TestRule35RealTreeControlsAreClean runs the detector's NEGATIVE CONTROLS
// against the tree AS IT IS: the two shapes this repo really carries (the
// crontab-installing scratch-gc installer, and the devdoc/evidence prose that
// quotes the retired line) must not be flagged, and the only tracked
// state-shaped paths must be the ones the exemption list names WITH a reason. A
// control taken from a synthetic string proves the regex; a control taken from
// the real tree proves the TREE.
func TestRule35RealTreeControlsAreClean(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	// Control 1: the real installer. It INSTALLS a scheduler, and it is allowed
	// to — a host-level upkeep unit is the operator's own deliberate switch
	// (Rule 35 explicit: an OS timer for disk hygiene is not a hidden wrapper).
	// The gate's job is to make sure such a thing is not created by ACCIDENT,
	// and its own installer carries the reasoning in its header.
	installer := filepath.Join(root, "scripts", "install-scratch-gc-watchdog.sh")
	raw, err := os.ReadFile(installer)
	if err != nil {
		t.Fatalf("read %s: %v — the host-level installer this control is taken from is gone", installer, err)
	}
	var installerHits []rule35Hit
	lines := strings.Split(string(raw), "\n")
	stripped := make([]string, len(lines))
	for i, l := range lines {
		stripped[i] = stripRule35Comment(l)
	}
	installerHits = rule35ScriptHits(rule35File{rel: "scripts/install-scratch-gc-watchdog.sh", lines: stripped})
	if len(installerHits) > 0 {
		t.Errorf("the gate fired on the REAL scripts/install-scratch-gc-watchdog.sh (rule %q) — "+
			"a scheduler installer that a reviewer accepted is a control this detector must survive; "+
			"if it is a violation, the exemption belongs in the file with its reason, not in a relaxed pattern",
			installerHits[0].rule)
	}
	// Control 2: the tracked-index state rule, measured against the real index.
	paths := rule35TrackedPaths(t, root)
	tracked := make(map[string]bool, len(paths))
	for _, p := range paths {
		tracked[p] = true
	}
	for _, p := range paths {
		if !rule35Matches(rule35StateShapeRes, p) {
			continue
		}
		reason, ok := rule35StateExemptRels[p]
		if !ok {
			t.Errorf("TRACKED runtime state in the real index: %s\n"+
				"  Rule 35: this file outlives every run that wrote it. Either untrack it, or add it to "+
				"rule35StateExemptRels WITH a reason a reviewer can disagree with.", p)
			continue
		}
		t.Logf("rule35 state exemption: %s — %s", p, reason)
	}
	// ...and the table ratchets DOWN only: a permit whose path is no longer
	// tracked is a STALE permit, not a harmless leftover — it keeps a decision
	// alive for a file nobody can read, and an all-stale table would make the
	// audit above vacuous (Rule 39's own class on a different table). An empty
	// table is therefore the correct end state, and it is reached by DELETING
	// the entry in the same change that untracked the path.
	for rel, reason := range rule35StateExemptRels {
		if reason == "" {
			t.Errorf("rule35StateExemptRels entry %s has no reason — an unreasoned exception is a silent un-gate", rel)
			continue
		}
		if !tracked[rel] {
			t.Errorf("STALE permit in rule35StateExemptRels: %s is not in the tracked index (untracked, or never committed)\n"+
				"  Delete the entry in the same change that untracked the path — a permit for a file nobody "+
				"can read hides the next one.", rel)
		}
	}
	// Control 3: the ungated side of the crontab decision, stated as a test so it
	// cannot quietly change. The gate deliberately does NOT flag the cron
	// EXPRESSION inside the real installer (a corpus that carries one is not a
	// shape to forbid); the pipe into `crontab -` inside that same file is what
	// makes it a scheduler, and it is exempt by REVIEW, not by pattern. This
	// asserts the two halves of that sentence.
	if !strings.Contains(string(raw), "crontab -") {
		t.Log("scripts/install-scratch-gc-watchdog.sh no longer installs a crontab — " +
			"the review that exempted it may now be retired (check before removing the control)")
	}
}
