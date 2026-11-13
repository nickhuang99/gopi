package config

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test working dir to the go.mod owner (the
// config package tests run with CWD=internal/config; go.mod sits at the
// repo root, one level up from internal/).
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

// TestSessionsDirNameNoHardcodedLiterals is the exhaustive guard for the
// sessions dir-name infra contract (Rule 1/2 single source): every non-test
// .go source under internal/ and cmd/ must reference the dir as
// config.SessionsDirName — never spell "sessions" inside a filepath.Join /
// path.Join. The dir name merged task containers + session logs + task-run
// children under one root (session-dir-unify S1), so a rename must hit every
// consumer; a missed literal silently points one writer/reader at a
// different root.
//
// Exemptions (each reviewed, all constants aliasing config.SessionsDirName,
// never a second spelling):
//   - internal/gopilog/reader.go: const sessionsDir = config.SessionsDirName
//     (package-local alias — gopilog predates the config constant).
//   - internal/gopilog/stats.go Sessions field: json:"sessions" — a JSON
//     wire tag, not a dir name.
//   - cmd/agent/commands.go: logSubSessions = "sessions" — a REPL VERB, not a
//     dir name (see sessionsLiteralReason: the /log subcommand runSlashLog's
//     switch answers).
//
// _test.go files are exempt (fixtures intentionally write real sandbox
// paths) — they exercise the production join, so a production miss fails
// the runtime suites; the exhaustive guard covers the shipped surface.
func TestSessionsDirNameNoHardcodedLiterals(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	// The reasons live in sessionsLiteralReason (below): one reviewed verdict
	// per NON-directory meaning of the literal, so the exemption list carries
	// its justification instead of a bare string match.
	var hits []string
	for _, dir := range []string{"internal", "cmd"} {
		walk := filepath.Join(root, dir)
		filepath.WalkDir(walk, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if !bytes.Contains(b, []byte(`"sessions"`)) {
				return nil
			}
			rel := strings.TrimPrefix(p, root+string(filepath.Separator))
			for i, line := range strings.Split(string(b), "\n") {
				if !strings.Contains(line, `"sessions"`) {
					continue
				}
				trim := strings.TrimSpace(line)
				if r := sessionsLiteralReason(rel, trim); r != "" {
					t.Logf("[exempted] %s:%d: %s  ->  %s", rel, i+1, trim, r)
					continue
				}
				hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, i+1, trim))
			}
			return nil
		})
	}
	if len(hits) > 0 {
		t.Fatalf("hardcoded %q dir literals in non-test source (use config.SessionsDirName):\n%s",
			"sessions", strings.Join(hits, "\n"))
	}
}

// The exemption arms below are keyed on these two facts about a REPL verb's
// definition site — the package that defines the words, and the prefix the
// subcommand constants share (cmd/agent/commands.go's logSub* block). They are
// named constants so the reason text and the check cannot drift apart (Rule 1b).
const (
	replPackageDir      = "cmd/agent/"
	logSubCommandPrefix = "logSub"
)

// sessionsLiteralReason returns WHY a line carrying the `"sessions"` literal is
// not a second spelling of the sessions directory name — or "" when the line IS
// a hardcoded dir literal, which the guard must report. It is one reviewed
// verdict per meaning (Rule 1b/13), and every non-empty reason names the thing
// the literal actually is, so a reader can tell a verb from a directory.
//
// It is deliberately exact per line: a bare literal forming a path is never
// exempt (that is the defect the guard exists for), so a new
// filepath.Join(root, "sessions") anywhere in internal/ or cmd/ stays RED.
func sessionsLiteralReason(rel, trim string) string {
	switch {
	case strings.HasPrefix(trim, "//"):
		// A DOC COMMENT that names the directory in prose (e.g.
		// "joins this constant instead of spelling \"sessions\""). A comment
		// builds no path: the text carries no value any code can consume, so
		// it cannot point a writer or reader at a second root. A CODE line
		// can never reach this arm — Go requires the // itself — so hiding a
		// literal here means writing it in prose only.
		return "a doc comment naming the directory in prose, not a path"
	case strings.Contains(trim, `json:"sessions"`):
		// A JSON WIRE TAG (internal/gopilog/stats.go's Sessions field): the
		// name of a field on the wire, and no path is ever built from it.
		return "JSON wire tag, not a path"
	case strings.Contains(trim, `const SessionsDirName = "sessions"`):
		// The constant's own definition (internal/config/gopihome.go): the one
		// line that IS the dir name.
		return "the constant's own definition"
	case strings.Contains(trim, `= config.SessionsDirName`):
		// An alias of the constant (internal/gopilog/reader.go's sessionsDir):
		// already single-sourced, the literal only names the alias target.
		return "an alias of config.SessionsDirName"
	case strings.Contains(trim, `= "sessions"`):
		// A CONSTANT DECLARATION whose value is the literal — and ONLY the
		// /log SUBCOMMAND list table in cmd/agent/commands.go, whose
		// identifiers are logSub* (logSubSessions is the entry here). It is a
		// REPL VERB list: runSlashLog's switch answers
		// last|sessions|summary|read|search, and this is the `sessions` word —
		// the subcommand that prints the profile's session table, the REPL
		// spelling of the CLI's `gopi session log --sessions`. The literal is argument
		// text typed after "/log", never joined into a path; the ROOT it reads
		// is resolved by internal/gopilog through config.SessionsDirName.
		//
		// The identifier check is what keeps this arm NARROW: a declaration of
		// some other name (sessionsDir := "sessions") or any joined path
		// (filepath.Join(root, "sessions")) fails it and stays RED, and a join
		// can never even be spelled in this shape.
		lhs := strings.TrimSpace(strings.SplitN(trim, "=", 2)[0])
		if strings.HasPrefix(rel, replPackageDir) && strings.HasPrefix(lhs, logSubCommandPrefix) &&
			!strings.ContainsAny(lhs, " (.\t") {
			return "a REPL VERB — the /log SUBCOMMAND table (logSub* in cmd/agent/commands.go, the words runSlashLog's switch answers), not a directory"
		}
		return ""
	case strings.Contains(trim, `case "sessions":`):
		// A switch CASE LABEL naming the same verb, for a switch that spells
		// its own cases rather than referencing the constants. Same meaning as
		// the declaration arm above: a subcommand word, not a dir.
		return "a REPL VERB switch case label, not a directory"
	}
	return ""
}
