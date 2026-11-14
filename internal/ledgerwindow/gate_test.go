package ledgerwindow_test

// THE EXHAUSTIVE GATE: no checked-in source may compare a ledger epoch column
// against a wall-clock literal converted WITHOUT the 'utc' modifier.
//
// WHY A SOURCE SCAN AND NOT ONLY A UNIT TEST. The unit tests in this package
// pin the helper — but the trap lives in the SURFACES, and a new report surface
// (a script, a one-off harness committed next to its finding, a new CLI query)
// does not have to call the helper at all. It only has to type the bare form
// once, and the number it prints is wrong by the host's UTC offset with no
// error anywhere. That is the failure this gate makes a build failure.
//
// WHY IT LIVES IN THE PACKAGE'S OWN _test PACKAGE: the rule it enforces is this
// package's reason to exist, and the detector derives what a correct predicate
// looks like from the package's own exported constants (FuncStrftime,
// FormatEpochSecondsSQL, UTCModifierSQL, NowKeyword) — so the detector and the
// shipped predicate can never describe different SQL.
//
// WHAT IT SCANS (all repo-relative, walking from the module root):
//   - every production Go file under internal/ and cmd/ (*_test.go excluded:
//     the surfaces are production code),
//   - every .sh/.py under scripts/, devdoc/ and testdata/ — the same three
//     roots the sibling /tmp gate audits, because a one-off harness lives next
//     to its finding in devdoc/ and the smoke driver lives in testdata/.
//
// WHAT IT FLAGS: a call to strftime() or unixepoch() that (a) does not carry
// the 'utc' modifier AND (b) has an argument spelling a calendar date
// (YYYY-MM-DD). The second half is what keeps the gate honest: `'now'` is an
// absolute instant and a placeholder bound is resolved at runtime, so neither
// is a wall-clock literal being misread. THE MAGIC `'now'` IS NOT EXEMPTED BY
// NAME — it is exempt because it carries no calendar date.
//
// WHAT IT DELIBERATELY DOES NOT COVER (so a later reader does not "complete"
// it into a false-positive machine): datetime()/date() FORMATTING of a literal
// (the class the ledger's epoch columns are not compared against), and a
// wall-clock string passed as a query PARAMETER (?) — that is the helper's own
// sanctioned shape (Where), and its correctness is pinned by the window-result
// test, not by a text scan. The boundary is stated in
// devdoc/ledger-wallclock-window.md.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/ledgerwindow"
)

const (
	// dirInternal / dirCmd hold the production Go sources.
	dirInternal = "internal"
	dirCmd      = "cmd"
	// dirScripts / dirDevdoc / dirTestdata hold the script entry points: the
	// repo's dev/test scripts, one-off harnesses committed next to their
	// findings, and the smoke driver.
	dirScripts     = "scripts"
	dirDevdoc      = "devdoc"
	dirTestdata    = "testdata"
	goFileSuffix   = ".go"
	testFileSuffix = "_test.go"
	shellSuffix    = ".sh"
	pythonSuffix   = ".py"
	// goModName is the module root marker.
	goModName = "go.mod"
	// archiveDirName holds historical material: not built, not run, not audited
	// (the same exclusion the /tmp gate makes).
	archiveDirName = "archive"

	// funcUnixEpoch is SQLite's dedicated epoch conversion (the modern spelling
	// of strftime('%s', …)); it parses a date string exactly the same way, so
	// it is the same trap.
	funcUnixEpoch = "unixepoch"

	// gateMinGoFiles / gateMinScriptFiles are the vacuity floors: a scan that
	// suddenly sees a handful of files is looking in the wrong place, and a
	// green gate over the wrong tree is worse than no gate.
	gateMinGoFiles     = 200
	gateMinScriptFiles = 20
	// gateMinCallSites is the anti-vacuous-pass floor for the DETECTOR: the
	// tree carries strftime('%s','now') call sites, which are correct and must
	// still be SEEN. A detector that matched nothing would pass this gate while
	// enforcing nothing.
	gateMinCallSites = 2

	// gateEscapeMarker documents a deliberately retained bare-form site. It is
	// the ONE accepted form (never a wildcard, never a whole file): it carries
	// a reason of at least escapeMinReasonLen characters, and it must sit on the
	// call's own line or the line directly above it.
	gateEscapeMarker = "//ledgerwindow:allow-wallclock"
	// shellEscapeMarker is the same marker in a shell comment.
	shellEscapeMarker = "#ledgerwindow:allow-wallclock"
	// escapeMinReasonLen is the shortest accepted reason ("because" is not one).
	escapeMinReasonLen = 10

	// predicateBounds is how many bounds an interval predicate carries — the
	// number of date-function calls the detector must see in a correct
	// predicate (`strftime(…) BETWEEN` is written once per bound). It is
	// restated here because this file is the package's EXTERNAL test package:
	// the count is part of the detector's contract, and the internal test
	// asserts the shipped predicate uses the same number.
	predicateBounds = 2
)

// dateLiteralRe matches a calendar date inside a string — the signature of a
// wall-clock literal being handed to a date function.
var dateLiteralRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// sqlFuncNames are the date functions whose second/only argument is parsed
// against a timezone.
var sqlFuncNames = []string{ledgerwindow.FuncStrftime, funcUnixEpoch}

// scannedGoFile reports whether a production Go file is audited.
func scannedGoFile(name string) bool {
	return strings.HasSuffix(name, goFileSuffix) && !strings.HasSuffix(name, testFileSuffix)
}

// callSite is one call of a scanned SQL date function.
type callSite struct {
	File  string
	Line  int
	Args  []string
	Whole string
}

// TestNoBareWallclockStrftimeInCheckedInSources is the gate.
func TestNoBareWallclockStrftimeInCheckedInSources(t *testing.T) {
	root := moduleRoot(t)
	var sites []callSite
	goFiles, scriptFiles := 0, 0

	for _, file := range auditedFiles(t, root, &goFiles, &scriptFiles) {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			t.Fatalf("relativize %s: %v", file, err)
		}
		rel = filepath.ToSlash(rel)
		escaped := escapedLines(string(raw))
		for _, site := range findDateFunctionCalls(rel, string(raw)) {
			sites = append(sites, site)
			if reason, ok := escaped[site.Line]; ok && len(reason) >= escapeMinReasonLen {
				continue
			}
			if reason, ok := escaped[site.Line-1]; ok && len(reason) >= escapeMinReasonLen {
				continue
			}
			if carriesUTCModifier(site.Args) {
				continue
			}
			if !carriesDateLiteral(site.Args) {
				// strftime('%s','now') and the placeholder form: no wall-clock
				// literal is being read here, so there is nothing to misread.
				continue
			}
			t.Errorf("%s:%d converts a wall-clock literal without the %s modifier: %s\n"+
				"SQLite reads a bare 'YYYY-MM-DD HH:MM' as UTC, while the ledger's %s/%s/%s columns hold TRUE "+
				"epoch seconds (the local clock's `date +%%s`). The window silently shifts by the host's UTC "+
				"offset (28800 s here) and the report prints a wrong NUMBER with no error. FIX: build the "+
				"predicate with internal/ledgerwindow (ledgerwindow.Where / WhereLiteral / CountInWindow), or "+
				"append %s to the conversion.",
				site.File, site.Line, ledgerwindow.UTCModifierSQL, strings.TrimSpace(site.Whole),
				ledgerwindow.ColumnCreatedAt, ledgerwindow.ColumnClaimedAt, ledgerwindow.ColumnEndedAt,
				ledgerwindow.UTCModifierSQL)
		}
	}

	if goFiles < gateMinGoFiles {
		t.Fatalf("the gate audited %d production Go files (want at least %d) — the scan is not looking where the "+
			"sources live (vacuous-pass guard)", goFiles, gateMinGoFiles)
	}
	if scriptFiles < gateMinScriptFiles {
		t.Fatalf("the gate audited %d script files (want at least %d) — the scan is not looking where the scripts "+
			"live (vacuous-pass guard)", scriptFiles, gateMinScriptFiles)
	}
	if len(sites) < gateMinCallSites {
		t.Fatalf("the detector found %d call site(s) of %v in the whole tree (want at least %d) — it is not "+
			"matching the shape it is supposed to police, so this gate would pass while enforcing nothing "+
			"(vacuous-pass guard)", len(sites), sqlFuncNames, gateMinCallSites)
	}
	t.Logf("gate: %d production Go files + %d script files scanned, %d date-function call site(s) seen",
		goFiles, scriptFiles, len(sites))
}

// TestDetectorFlagsTheBareFormAndSparesTheCorrectOne is the detector's own
// red-proof, run on synthetic sources so it holds on every `go test` (the
// file-level demonstration — a probe file added to the worktree — is the
// manual half, recorded in the devdoc).
func TestDetectorFlagsTheBareFormAndSparesTheCorrectOne(t *testing.T) {
	// The bare form: the modifier absent, a wall-clock literal present.
	bare := "SELECT COUNT(*) FROM " + ledgerwindow.TableTasks + " WHERE " + ledgerwindow.ColumnClaimedAt +
		" BETWEEN strftime('" + ledgerwindow.FormatEpochSeconds + "','2026-09-16 22:00') AND strftime('" +
		ledgerwindow.FormatEpochSeconds + "','2026-09-17 05:00')"
	// The correct form: the same query with the modifier.
	good := ledgerwindow.WhereLiteral(ledgerwindow.ColumnClaimedAt,
		ledgerwindow.Window{Since: "2026-09-16 22:00", Until: "2026-09-17 05:00"})
	// The two shapes that must NEVER be flagged: the absolute instant, and a
	// runtime parameter.
	nowForm := "UPDATE " + ledgerwindow.TableTasks + " SET updated_at = strftime('" +
		ledgerwindow.FormatEpochSeconds + "','" + ledgerwindow.NowKeyword + "')"
	paramForm := ledgerwindow.Where(ledgerwindow.ColumnClaimedAt)
	// unixepoch() is the same trap under a different name.
	bareUnixEpoch := "SELECT unixepoch('2026-09-16 22:00')"

	cases := []struct {
		name     string
		src      string
		wantHits int
	}{
		{"bare strftime (two bounds, both bare)", bare, 2},
		{"correct strftime", good, 0},
		{"absolute now", nowForm, 0},
		{"placeholder bound", paramForm, 0},
		{"bare unixepoch", bareUnixEpoch, 1},
	}
	for _, tc := range cases {
		sites := findDateFunctionCalls("probe.go", tc.src)
		hits := 0
		for _, site := range sites {
			if !carriesUTCModifier(site.Args) && carriesDateLiteral(site.Args) {
				hits++
			}
		}
		if hits != tc.wantHits {
			t.Errorf("%s: detector reported %d violation(s), want %d — %s", tc.name, hits, tc.wantHits, tc.src)
		}
	}

	if got := len(findDateFunctionCalls("probe.go", good)); got != predicateBounds {
		t.Errorf("the correct predicate produced %d call site(s), want %d (one per bound) — the detector's shape "+
			"match is wrong", got, predicateBounds)
	}
}

// TestGateSeesMultiLineSQLAndEscapes pins the two properties that make the
// gate usable rather than merely strict: a call whose arguments span lines is
// still parsed, and a documented escape silences a site (with a reason).
func TestGateSeesMultiLineSQLAndEscapes(t *testing.T) {
	multi := "q := `SELECT COUNT(*) FROM " + ledgerwindow.TableTasks + "\n" +
		"  WHERE " + ledgerwindow.ColumnClaimedAt + " BETWEEN strftime('" + ledgerwindow.FormatEpochSeconds + "',\n" +
		"    '2026-09-16 22:00')`"
	sites := findDateFunctionCalls("probe.go", multi)
	if len(sites) != 1 {
		t.Fatalf("a call whose arguments span lines was not parsed (%d site(s)) — a multi-line SQL string is how "+
			"half the surfaces in this repo spell their queries:\n%s", len(sites), multi)
	}
	if !carriesDateLiteral(sites[0].Args) {
		t.Errorf("the multi-line call's arguments were not recovered: %q", sites[0].Args)
	}

	escaped := escapedLines("// probe — this one is deliberate\n" +
		"q := `strftime('" + ledgerwindow.FormatEpochSeconds + "','2026-09-16 22:00')` " + gateEscapeMarker +
		" the fixture pins the pre-fix behaviour\n")
	if reason, ok := escaped[2]; !ok || len(reason) < escapeMinReasonLen {
		t.Errorf("a marked line did not register as escaped (reason %q, ok %v)", reason, ok)
	}
	if _, ok := escaped[1]; ok {
		t.Errorf("the line above a marked line must not be escaped by it")
	}
}

// ---------------------------------------------------------------------------
// the scanner
// ---------------------------------------------------------------------------

// auditedFiles walks the audited roots and returns the files to scan.
func auditedFiles(t *testing.T, root string, goFiles, scriptFiles *int) []string {
	t.Helper()
	var out []string
	for _, dirName := range []string{dirInternal, dirCmd} {
		base := filepath.Join(root, dirName)
		walkErr := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipDir(path, root, d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !scannedGoFile(d.Name()) {
				return nil
			}
			*goFiles++
			out = append(out, path)
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", base, walkErr)
		}
	}
	for _, dirName := range []string{dirScripts, dirDevdoc, dirTestdata} {
		base := filepath.Join(root, dirName)
		if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
			continue // an absent root is not a failure (a docs-only checkout)
		}
		walkErr := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if skipDir(path, root, d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, shellSuffix) && !strings.HasSuffix(name, pythonSuffix) {
				return nil
			}
			*scriptFiles++
			out = append(out, path)
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", base, walkErr)
		}
	}
	return out
}

// skipDir reports whether a directory is outside the audit: version control,
// historical material, built artifacts and dependency trees. The module root
// itself is never skipped.
func skipDir(path, root, name string) bool {
	if path == root {
		return false
	}
	switch name {
	case ".git", archiveDirName, "bin", "node_modules", "vendor":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// findDateFunctionCalls returns every call of a scanned date function in src,
// with its arguments (top-level comma split). Comments are blanked first, so
// documentation that NAMES the shape is not an offence — the lesson of the
// device-load gate, which fired on its own incident reports.
func findDateFunctionCalls(rel, src string) []callSite {
	code := blankComments(src)
	var out []callSite
	for _, fn := range sqlFuncNames {
		for from := 0; ; {
			idx := indexIdentifier(code, fn, from)
			if idx < 0 {
				break
			}
			open := skipSpace(code, idx+len(fn))
			if open >= len(code) || code[open] != '(' {
				from = idx + len(fn)
				continue
			}
			args, end, ok := parseArgList(code, open)
			if !ok {
				from = open + 1
				continue
			}
			out = append(out, callSite{
				File:  rel,
				Line:  1 + strings.Count(code[:idx], "\n"),
				Args:  args,
				Whole: code[idx:min(end+1, len(code))],
			})
			from = end + 1
		}
	}
	return out
}

// indexIdentifier finds the next occurrence of name in s at or after from that
// is a whole identifier (not a suffix of a longer word): `unixepoch` must not
// match inside `myunixepoch`, and `strftime` must not match `strftimex`.
func indexIdentifier(s, name string, from int) int {
	for {
		idx := strings.Index(s[from:], name)
		if idx < 0 {
			return -1
		}
		idx += from
		beforeOK := idx == 0 || !isIdentByte(s[idx-1])
		afterOK := idx+len(name) >= len(s) || !isIdentByte(s[idx+len(name)])
		if beforeOK && afterOK {
			return idx
		}
		from = idx + len(name)
	}
}

// isIdentByte reports whether b can be part of a SQL/Go identifier.
func isIdentByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// skipSpace advances past spaces and tabs.
func skipSpace(s string, from int) int {
	for from < len(s) && (s[from] == ' ' || s[from] == '\t' || s[from] == '\n' || s[from] == '\r') {
		from++
	}
	return from
}

// parseArgList parses a balanced argument list starting at the '(' in open and
// returns the top-level arguments, the offset of the closing ')', and whether
// the list was terminated. Quoted literals (', ", `) are skipped, so a comma or
// paren INSIDE a string does not split or close anything.
func parseArgList(s string, open int) ([]string, int, bool) {
	var args []string
	depth := 0
	argStart := open + 1
	for i := open; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'', '"', '`':
			end := skipQuoted(s, i, c)
			if end < 0 {
				return nil, 0, false
			}
			i = end
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				last := strings.TrimSpace(s[argStart:i])
				if last != "" {
					args = append(args, last)
				}
				return args, i, true
			}
		case ',':
			if depth == 1 {
				args = append(args, strings.TrimSpace(s[argStart:i]))
				argStart = i + 1
			}
		}
	}
	return nil, 0, false
}

// skipQuoted returns the offset of the closing quote at or after i, or -1.
func skipQuoted(s string, i int, quote byte) int {
	for j := i + 1; j < len(s); j++ {
		if s[j] == '\\' {
			j++
			continue
		}
		if s[j] == quote {
			return j
		}
	}
	return -1
}

// carriesUTCModifier reports whether any argument IS the modifier literal.
func carriesUTCModifier(args []string) bool {
	for _, a := range args {
		if a == ledgerwindow.UTCModifierSQL {
			return true
		}
	}
	return false
}

// carriesDateLiteral reports whether any argument spells a calendar date — the
// signature of a wall-clock literal. `'now'`, a placeholder and a column name
// all fail this test, which is why the gate does not need an allowlist of
// "good" literals.
func carriesDateLiteral(args []string) bool {
	for _, a := range args {
		if dateLiteralRe.MatchString(a) {
			return true
		}
	}
	return false
}

// escapedLines maps a 1-based line number to the reason recorded by the escape
// marker on that line. The marker is read from the ORIGINAL text (blankComments
// erases it), and a marker with no reason maps to an empty reason, which the
// caller rejects.
func escapedLines(src string) map[int]string {
	out := map[int]string{}
	for i, line := range strings.Split(src, "\n") {
		marker := -1
		for _, m := range []string{gateEscapeMarker, shellEscapeMarker} {
			if idx := strings.Index(line, m); idx >= 0 && (marker < 0 || idx < marker) {
				marker = idx + len(m)
			}
		}
		if marker < 0 {
			continue
		}
		out[i+1] = strings.TrimSpace(line[marker:])
	}
	return out
}

// blankComments replaces comment text with spaces, preserving every newline so
// line numbers survive. It handles Go line comments (//), shell/Python line
// comments (# at a word start), and block comments (/* */). A '#' or '//'
// inside a quoted string is NOT a comment.
func blankComments(src string) string {
	b := []byte(src)
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case '\'', '"', '`':
			end := skipQuoted(src, i, b[i])
			if end < 0 {
				return string(b)
			}
			i = end
		case '/':
			if i+1 < len(b) && b[i+1] == '/' {
				i = blankToEOL(b, i)
				continue
			}
			if i+1 < len(b) && b[i+1] == '*' {
				i = blankBlock(b, i)
				continue
			}
		case '#':
			if i == 0 || b[i-1] == '\n' || b[i-1] == ' ' || b[i-1] == '\t' || b[i-1] == ';' {
				i = blankToEOL(b, i)
			}
		}
	}
	return string(b)
}

// blankToEOL blanks bytes from i to (not including) the next newline.
func blankToEOL(b []byte, i int) int {
	for ; i < len(b) && b[i] != '\n'; i++ {
		b[i] = ' '
	}
	return i
}

// blankBlock blanks a /* … */ comment.
func blankBlock(b []byte, i int) int {
	for ; i < len(b); i++ {
		if b[i] == '\n' {
			continue
		}
		if b[i] == '*' && i+1 < len(b) && b[i+1] == '/' {
			b[i], b[i+1] = ' ', ' '
			return i + 1
		}
		b[i] = ' '
	}
	return len(b)
}

// moduleRoot walks up from the test binary's working directory to the module
// root (the directory holding go.mod).
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, goModName)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no %s above %s — cannot locate the module root", goModName, dir)
		}
		dir = parent
	}
}
