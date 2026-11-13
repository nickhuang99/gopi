package main

// The /proc/<pid>/stat parse has ONE home: internal/procname.StatField.
//
// This gate is the mechanical half of Rules 13/14 for that parse. The repo had
// FOUR copies of "count the fields after the LAST ')'" (scratch's
// ParseProcStartTime, tools' procStartTicks, tools' finishCleanupProcParent, and
// a fourth inside tools' takeover state reader); three of them are now
// delegations and a fifth (testutil.parseProcStat) was folded in when this gate
// found it. A re-inlined body is a FIFTH copy, and its failure mode is SILENT —
// a whitespace-split read of a stat line whose comm carries spaces returns a
// neighbouring NUMBER, which every caller accepts as a tick count.
//
// THE PROBE (a copy shape, not a spelling): the gate strips every comment and
// string literal, then looks for a `LastIndex(..., ')')` (or the byte form)
// whose result feeds a `Fields(` split within a few lines of CODE. That is the
// hand-rolled parse. A reader that instead indexes ONE fixed offset after the
// last ')' — the three single-character state/zombie probes — is deliberately
// NOT flagged: only comm content AFTER the last ')' can shift such a read, and a
// comm ending in ')' is the shape the kernel already truncates away (comm is a
// basename, and this is the `(sd-pam)`-style single-trailing-paren case). This
// task consolidated the field-COUNTERS; the single-character probes are a
// separate, behaviour-unchanged follow-up.
//
// IT PRINTS, IT DOES NOT BLOCK (the repo's gate convention): `go test ./cmd/agent/`
// prints the finding and stays green, so a merge is never held hostage by a
// detector that cannot tell a fix from a regression. The enforcing half lives in
// internal/tools' TestProcStatParseHasOneImplementation, whose scope is the
// three delegating sites plus the repo-wide shape scan.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// procStatParseHomeRel is the ONE file allowed to hold the parse.
	procStatParseHomeRel = "internal/procname/procname.go"
	// procStatParseWindow is how many lines of CODE after the ')' search a
	// `Fields(` split still counts as the same hand-rolled parse. The retired
	// copies were 4-6 lines long; 8 leaves room without reaching into an
	// unrelated function.
	procStatParseWindow = 8
	// procStatParseProbeFloor is the read floor: the scan must see at least this
	// many production files, or it is scanning a broken tree and its silence
	// means nothing.
	procStatParseProbeFloor = 200
	// procStatParseReports is the max number of findings printed (a report, not
	// a wall of text; the count line states the true total).
	procStatParseReports = 10
)

// TestProcStatParseSingleHomeDetector reports every production file that
// re-inlines the /proc/<pid>/stat field parse.
func TestProcStatParseSingleHomeDetector(t *testing.T) {
	root := procStatParseRepoRoot(t)
	home := filepath.Join(root, filepath.FromSlash(procStatParseHomeRel))

	var findings []string
	scanned := 0
	for _, dir := range []string{"internal", "cmd"} {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil //nolint:nilerr // an unreadable entry is skipped, not fatal: the read floor guards the whole scan
			}
			if path == home {
				return nil
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil //nolint:nilerr // same: the floor below catches a walk that read nothing
			}
			scanned++
			rel, _ := filepath.Rel(root, path) //stylegate:ignore path arithmetic in a fixture; the compared string is the assertion
			findings = append(findings, procStatParseFindReinlines(rel, string(src))...)
			return nil
		})
		if walkErr != nil {
			t.Fatalf("scan %s: %v", dir, walkErr)
		}
	}

	if scanned < procStatParseProbeFloor {
		t.Fatalf("the detector scanned only %d production .go files (floor %d) — it is looking at a broken "+
			"tree, so its silence would mean nothing", scanned, procStatParseProbeFloor)
	}
	if _, err := os.Stat(home); err != nil {
		t.Fatalf("the single home %s is missing: %v — the parse has no definition site left", procStatParseHomeRel, err)
	}
	if len(findings) > 0 {
		t.Errorf("%d production file(s)/site(s) re-inline the /proc/<pid>/stat parse — it has ONE home, "+
			"%s (Rules 13/14). Report (first %d):\n  %s",
			len(findings), procStatParseHomeRel, procStatParseReports,
			strings.Join(headStrings(findings, procStatParseReports), "\n  "))
	}
	t.Logf("procname stat-parse detector: %d production files scanned, %d re-inlined site(s)", scanned, len(findings))
}

// procStatParseFindReinlines reports the `LastIndex of ')'` → `Fields` shape in
// one file's source, as "<rel>:<line>" plus the offending code.
func procStatParseFindReinlines(rel, src string) []string {
	lines := stripGoCommentsAndStrings(src)
	var out []string
	for i, line := range lines {
		if !strings.Contains(line, "LastIndex") {
			continue
		}
		// Both spellings of the search key are in use across the retired copies
		// (`')'` the rune, and `")"` the string).
		if !strings.Contains(line, "')'") && !strings.Contains(line, `")"`) {
			continue
		}
		for j := i + 1; j < len(lines) && j <= i+procStatParseWindow; j++ {
			if strings.Contains(lines[j], "Fields(") {
				out = append(out, fmt.Sprintf("%s:%d (LastIndex of ')' at %d feeding a Fields split at %d)",
					rel, i+1, i+1, j+1))
				break
			}
		}
	}
	return out
}

// stripGoCommentsAndStrings removes line/block comments and ELIDES the contents
// of interpreted string literals longer than a couple of bytes, so a mention in a
// comment or a diagnostic message cannot be reported as a production site (a gate
// that fails on its own docs is a gate people delete).
//
// Short literals are KEPT verbatim on purpose: the parse's search key is itself a
// one-byte literal — `')'` (a rune, untouched here) or `")"` (a string) — and
// eliding it would hide the very shape the detector looks for. Rune literals are
// left alone for the same reason.
func stripGoCommentsAndStrings(src string) []string {
	var out []string
	inBlock := false
	for _, raw := range strings.Split(src, "\n") {
		var b strings.Builder
		inString := false
		escaped := false
		var lit strings.Builder
		for i := 0; i < len(raw); i++ {
			c := raw[i]
			if inBlock {
				if c == '*' && i+1 < len(raw) && raw[i+1] == '/' {
					inBlock = false
					i++
				}
				continue
			}
			if inString {
				if escaped {
					escaped = false
					lit.WriteByte(c)
					continue
				}
				switch c {
				case '\\':
					escaped = true
				case '"':
					inString = false
					if lit.Len() <= shortLiteralMax {
						b.WriteString(`"` + lit.String() + `"`)
					} else {
						b.WriteString(`""`) // elided: prose, not a syntactic marker
					}
				default:
					lit.WriteByte(c)
				}
				continue
			}
			if c == '/' && i+1 < len(raw) {
				if raw[i+1] == '/' {
					break // line comment: rest of the line is prose
				}
				if raw[i+1] == '*' {
					inBlock = true
					i++
					continue
				}
			}
			if c == '"' {
				inString = true
				lit.Reset()
				continue
			}
			b.WriteByte(c)
		}
		out = append(out, b.String())
	}
	return out
}

// shortLiteralMax is how many bytes of string-literal content survive the strip:
// enough for the parse's own search key (`)`), too short for a sentence.
const shortLiteralMax = 2

// headStrings returns at most n entries.
func headStrings(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

// TestProcStatParseDetectorPredicate pins the predicate BOTH ways on synthetic
// sources: it must fire on the hand-rolled shape and stay silent on a mention in
// a comment, a string literal or a doc (a detector that fails on prose is one
// people delete). The retired copies are replayed verbatim as the positive
// probes.
func TestProcStatParseDetectorPredicate(t *testing.T) {
	positive := []struct {
		name string
		src  string
	}{
		{"bytes form (the retired tools copy)", `package p

func ticks(raw []byte) int {
	i := bytes.LastIndexByte(raw, ')')
	if i < 0 {
		return 0
	}
	fields := strings.Fields(string(raw)[i+1:])
	return len(fields)
}
`},
		{"strings form (the retired scratch copy)", `package p

func parse(raw string) error {
	close := strings.LastIndex(raw, ")")
	fields := strings.Fields(raw[close+1:])
	_ = fields
	return nil
}
`},
	}
	for _, tc := range positive {
		if got := procStatParseFindReinlines("fixture.go", tc.src); len(got) == 0 {
			t.Errorf("%s: the predicate missed the hand-rolled parse — it would let a FIFTH copy in", tc.name)
		}
	}

	negative := []struct {
		name string
		src  string
	}{
		{"a comment describing the trap", `package p

// The parser counts fields after the LAST ')' (strings.Fields is used there).
func ok() {}
`},
		{"a string literal mentioning it", `package p

const doc = "strings.LastIndex(line, \")\") then strings.Fields(...)"

func ok() {}
`},
		{"the DELEGATION (the shape we want)", `package p

func ticks(pid int) (int64, bool) {
	raw, ok := procname.StatFieldString(statPath(pid), procname.StatFieldStartTime)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	return n, err == nil && n > 0
}
`},
		{"a single-character probe after the last ')'", `package p

func isZombie(b []byte) bool {
	i := bytes.LastIndexByte(b, ')')
	if i < 0 || i+2 >= len(b) {
		return false
	}
	return b[i+2] == 'Z'
}
`},
	}
	for _, tc := range negative {
		if got := procStatParseFindReinlines("fixture.go", tc.src); len(got) != 0 {
			t.Errorf("%s: the predicate fired on %v — a false positive here turns the gate into noise", tc.name, got)
		}
	}
}

// TestProcStatParseDetectorReadsTheRealTree is the read-floor half: the scan
// must see the production tree (a broken walk must fail, never pass silently).
func TestProcStatParseDetectorReadsTheRealTree(t *testing.T) {
	root := procStatParseRepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(procStatParseHomeRel))); err != nil {
		t.Fatalf("%s: %v", procStatParseHomeRel, err)
	}
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(procStatParseHomeRel)))
	if err != nil {
		t.Fatalf("read the single home: %v", err)
	}
	if n := strings.Count(string(src), "func StatField("); n != 1 {
		t.Errorf("%s defines StatField %d times, want exactly 1 — it is the ONE implementation", procStatParseHomeRel, n)
	}
}

// procStatParseRepoRoot walks up from the test's working directory to the module
// root (the dir holding go.mod), so the detector works from cmd/agent's cwd.
func procStatParseRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("no go.mod found above %s", dir)
	return ""
}
