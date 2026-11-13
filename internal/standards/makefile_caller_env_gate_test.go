package standardgate

// A Makefile that spells a CALLER-PUBLISHED environment variable as an
// UNCONDITIONAL assignment (`=` or `:=`) silently overrides whatever the caller
// exported, because make takes the assigned value over the inherited one — and
// the variable keeps its EXPORT bit (it was imported from the environment), so
// every child of every recipe sees the Makefile's value instead of the
// caller's. The measured instance (2026-10-11, task
// 01a12d14-513a-7b21-baee-d08e75acd312): Makefile:19 read
// `GOPI_BIN := $(BIN_DIR)/gopi` — the build artifact's path bound to the very
// name internal/testutil publishes as the caller's binary pin
// (testutil.BinaryPinEnv). `GOPI_BIN=<absolute> make test-budget` therefore ran
// every package against the RELATIVE `bin/gopi`; `go test` gives each package
// its own directory as the CWD, the relative path does not resolve, and
// resolveGopiBin's fatal arm (deliberately no silent substitution) turned the
// caller's pin into a red suite. Two meanings, one name.
//
// The gate's boundary, stated so it is not read as a lint over all of make:
// it fires on an assignment whose NAME is one of the CALLER PINS testutil
// publishes — the names whose entire contract is "the caller names the binary"
// (internal/testutil is their ONE definition, Rule 14; the gate references
// those constants and never re-spells them). Rewriting ANY other variable the
// Makefile owns is its own business, and a DEFERRING assignment to a pin
// (`?=` — "adopt the caller's value when there is one") is the fix, not a
// violation.
//
// It also asserts the Makefile still declares its build artifact path under the
// repo-PRIVATE name, so a silent rename back onto the pin is reported rather
// than merely un-caught.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// makefileName is the tracked build entry point this gate reads. The repo
	// has exactly one; a second would be a second answer to "how is this tree
	// built" (Rule 14).
	makefileName = "Makefile"
	// makefileArtifactVarName is the repo-PRIVATE name the Makefile binds its
	// own build artifact path to. It is deliberately NOT the caller pin, so the
	// two meanings cannot collide again.
	makefileArtifactVarName = "BIN_PATH"
	// makefileRecipeTab is the byte that makes a makefile line a RECIPE line
	// rather than an assignment, whatever it looks like.
	makefileRecipeTab = '\t'
	// makefileMinBytes / makefileMinLines / makefileMinAssignments /
	// makefileMinRecipeLines are the read-floors (Rule 15): a file this reader
	// failed to open, or read as a fragment, must FAIL rather than pass green —
	// "found nothing" and "never scanned" must not look alike. Each floor sits
	// well under the shipped Makefile as measured on 2026-10-11 (7914 bytes,
	// 146 lines, 5 column-0 assignments, 18 recipe lines) but far above an
	// empty or truncated read; the assignment count is deliberately small
	// because most of this Makefile's lines are commentary and recipes.
	makefileMinBytes       = 2048
	makefileMinLines       = 60
	makefileMinAssignments = 4
	makefileMinRecipeLines = 8
)

// callerPinnedEnvNames are the environment names whose contract is "the caller
// names the tested binary". They are read from internal/testutil, the package
// that OWNS them — never re-spelled here (Rule 1b/13/14).
var callerPinnedEnvNames = []string{testutil.BinaryPinEnv, testutil.SuiteBinEnv}

// makefileAssignRE matches a makefile variable assignment at column 0, with the
// optional `override`/`export`/`private`/`unexport` prefixes that may precede
// it, and captures the variable name and the operator.
var makefileAssignRE = regexp.MustCompile(
	`^(?:override\s+|export\s+|private\s+|unexport\s+)*([A-Za-z_][A-Za-z0-9_]*)\s*([:+?!]?=)`)

// makefileAssignment is one parsed assignment, keyed by its 1-based line.
type makefileAssignment struct {
	line int
	name string
	op   string
	text string
}

// makefileStats is what the reader actually saw — the floors above are checked
// against these, so a file that shrank under the parser cannot read as green.
type makefileStats struct {
	bytes       int
	lines       int
	assignments int
	recipeLines int
	hasArtifact bool
}

// makefileGateReport is one scan's verdict: the pin-clobbering assignments
// found (empty = green) plus what the reader saw.
type makefileGateReport struct {
	findings []makefileAssignment
	stats    makefileStats
}

// makefileAssignments returns every variable assignment in src. Recipe lines
// (tab-indented) and comment lines are not assignments, whatever they spell.
func makefileAssignments(src string) []makefileAssignment {
	var out []makefileAssignment
	for i, line := range strings.Split(src, "\n") {
		if line == "" || line[0] == makefileRecipeTab {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := makefileAssignRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, makefileAssignment{line: i + 1, name: m[1], op: m[2], text: trimmed})
	}
	return out
}

// makefileUnconditionalAssignments keeps the assignments that OVERWRITE a
// value the caller already exported: `=` and `:=`. `?=` defers to the caller,
// `+=`/`!=` are not a rebinding of a caller's value.
func makefileUnconditionalAssignments(src string) []makefileAssignment {
	var out []makefileAssignment
	for _, a := range makefileAssignments(src) {
		if a.op == "=" || a.op == ":=" {
			out = append(out, a)
		}
	}
	return out
}

// makefileCallerClobberFindings applies the gate's predicate: the assignments
// that unconditionally rebind a caller pin.
func makefileCallerClobberFindings(src string) []makefileAssignment {
	var out []makefileAssignment
	for _, a := range makefileUnconditionalAssignments(src) {
		for _, pin := range callerPinnedEnvNames {
			if a.name == pin {
				out = append(out, a)
			}
		}
	}
	return out
}

// scanMakefile reads path and applies the gate, returning an error (never a
// silent zero) when the file cannot be read or does not look like the Makefile
// this gate is written against.
func scanMakefile(path string) (makefileGateReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return makefileGateReport{}, fmt.Errorf("reading %s: %w", path, err)
	}
	src := string(raw)
	assignments := makefileAssignments(src)
	stats := makefileStats{bytes: len(raw), lines: strings.Count(src, "\n")}
	stats.assignments = len(assignments)
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, string(makefileRecipeTab)) {
			stats.recipeLines++
		}
	}
	for _, a := range assignments {
		if a.name == makefileArtifactVarName {
			stats.hasArtifact = true
		}
	}
	rep := makefileGateReport{findings: makefileCallerClobberFindings(src), stats: stats}
	if err := stats.checkFloors(path); err != nil {
		return rep, err
	}
	return rep, nil
}

// checkFloors refuses a scan that read too little to mean anything (Rule 15).
func (s makefileStats) checkFloors(path string) error {
	var short []string
	if s.bytes < makefileMinBytes {
		short = append(short, fmt.Sprintf("%d bytes < %d", s.bytes, makefileMinBytes))
	}
	if s.lines < makefileMinLines {
		short = append(short, fmt.Sprintf("%d lines < %d", s.lines, makefileMinLines))
	}
	if s.assignments < makefileMinAssignments {
		short = append(short, fmt.Sprintf("%d assignments < %d", s.assignments, makefileMinAssignments))
	}
	if s.recipeLines < makefileMinRecipeLines {
		short = append(short, fmt.Sprintf("%d recipe lines < %d", s.recipeLines, makefileMinRecipeLines))
	}
	if !s.hasArtifact {
		short = append(short, fmt.Sprintf("no assignment to %s (the repo-private build artifact path)", makefileArtifactVarName))
	}
	if len(short) > 0 {
		return fmt.Errorf("%s does not look like the Makefile this gate is written against: %s",
			path, strings.Join(short, "; "))
	}
	return nil
}

// TestMakefileNeverClobbersTheCallerBinaryPin is the gate: the shipped Makefile
// must not unconditionally assign a caller-published binary pin.
func TestMakefileNeverClobbersTheCallerBinaryPin(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, makefileName)
	rep, err := scanMakefile(path)
	if err != nil {
		t.Fatalf("%v\nA scan that could not read the file is a FAILURE, never a green gate.", err)
	}
	pins := strings.Join(callerPinnedEnvNames, ", ")
	if len(rep.findings) > 0 {
		var lines []string
		for _, f := range rep.findings {
			lines = append(lines, fmt.Sprintf("%s:%d  %s  (%s)", makefileName, f.line, f.text, f.op))
		}
		t.Errorf("%s unconditionally assigns a CALLER-PUBLISHED environment name (%s):\n  %s\n"+
			"make takes its own value over the inherited one and keeps the variable's export bit, so every\n"+
			"child of every recipe sees the Makefile's value instead of the caller's — in this repo that is how\n"+
			"`GOPI_BIN=<absolute> make test-budget` came to run every package against the RELATIVE bin/gopi and\n"+
			"fail in resolveGopiBin's fatal arm.\n"+
			"Fix: bind the build artifact's path to a repo-PRIVATE variable (%s), and either leave the caller pin\n"+
			"alone or adopt it with a DEFERRING assignment (`%s ?= …`).",
			makefileName, pins, strings.Join(lines, "\n  "),
			makefileArtifactVarName, testutil.BinaryPinEnv)
	}
	if rep.stats.assignments >= makefileMinAssignments && !rep.stats.hasArtifact {
		t.Errorf("%s declares no %s assignment: the repo-private build artifact path variable is gone — "+
			"a rename back onto the caller pin (%s) is exactly what this gate exists to catch",
			makefileName, makefileArtifactVarName, pins)
	}
}

// TestMakefileCallerPinGateRedProof drives the gate's predicate over planted
// makefiles: the arms that MUST fire, and the arms that must stay silent so the
// gate cannot be satisfied by refusing every assignment. The same predicate and
// the same reader the shipped-tree test uses are exercised here.
func TestMakefileCallerPinGateRedProof(t *testing.T) {
	pin := testutil.BinaryPinEnv
	artifact := makefileArtifactVarName
	// filler is a makefile that CLEARS the read-floors while naming no caller
	// pin: the plants below are the only place a pin can appear, so a finding
	// can only come from the plant. Its recipes are tab-indented on purpose —
	// the arm that must stay silent for a recipe line is exercised by them.
	filler := "BIN_DIR := bin\n" +
		artifact + " := $(BIN_DIR)/gopi\n" +
		"GOFLAGS := -count=1\n" +
		"MAN_SRC_DIR := man\n" +
		"MAN_OUT_DIR := bin/man/man1\n" +
		"MAN_PAGE_NAMES := gopi gopi-chat gopi-config gopi-memory gopi-merge gopi-notify gopi-session\n" +
		"EXTRA_NAMES := gopi-task gopi-tool gopi-ui_state gopi-version gopi-web gopi-worker\n" +
		"BUDGET_SEC := 600\n" +
		"CENSUS_ARGS := . --check\n" +
		"ARCHIVE_DIR := bin/archive\n" +
		"LABEL := local\n" +
		"build:\n\tCGO_ENABLED=0 go build -o $(" + artifact + ") ./cmd/agent/\n" +
		"test:\n\tscripts/test-budget.sh ./...\n" +
		"test-budget:\n\tscripts/test-budget.sh $(PKGS)\n" +
		"test-heavy:\n\tGOPI_HEAVY_TESTS=1 scripts/test-budget.sh $(PKGS)\n" +
		"vet:\n\tgo vet ./...\n" +
		"fmt:\n\tgofmt -l -w .\n" +
		"man:\n\tgo run internal/standards/testdata/mancensus.go . $(" + artifact + ")\n" +
		"man-check:\n\tgo run internal/standards/testdata/mancensus.go . $(" + artifact + ") --check\n" +
		"clean:\n\trm -f $(" + artifact + ")\n" +
		"all: build\n"
	// Comment padding: content for the byte/line floors, and nothing else — a
	// comment cannot be an assignment, so it can never be the source of a hit.
	for i := 0; i < 30; i++ {
		filler += "# padding line so the read-floors are met by content, not by the plant — " +
			strings.Repeat("x", 80) + "\n"
	}

	cases := []struct {
		name    string
		plant   string
		wantHit bool
	}{
		{"immediate assignment is the measured defect", pin + " := $(BIN_DIR)/gopi\n", true},
		{"recursive assignment clobbers too", pin + " = $(BIN_DIR)/gopi\n", true},
		{"exported immediate assignment", "export " + pin + " := $(BIN_DIR)/gopi\n", true},
		{"override immediate assignment", "override " + pin + " := $(BIN_DIR)/gopi\n", true},
		{"deferring assignment adopts the caller's value", pin + " ?= $(BIN_DIR)/gopi\n", false},
		{"append is not a rebinding", pin + " += extra\n", false},
		{"the suite pin is guarded the same way", testutil.SuiteBinEnv + " := bin/other\n", true},
		{"another variable is the Makefile's own business", "GOPI_BIN_BACKUP := bin/old\n", false},
		{"a comment that names the pin is prose", "# " + pin + " := whatever\n", false},
		{"a recipe line that spells the pin is not an assignment", "envprobe:\n\t@echo " + pin + "=$(BIN_DIR)/gopi\n", false},
	}
	for _, tc := range cases {
		path := filepath.Join(t.TempDir(), makefileName)
		if err := os.WriteFile(path, []byte(filler+tc.plant), 0o644); err != nil {
			t.Fatalf("planting %q: %v", tc.name, err)
		}
		rep, err := scanMakefile(path)
		if err != nil {
			t.Fatalf("%s: the planted makefile must clear the read-floors, got: %v", tc.name, err)
		}
		if tc.wantHit && len(rep.findings) == 0 {
			t.Errorf("%s: the planted assignment %q was NOT reported — the gate is vacuous here",
				tc.name, strings.TrimSpace(tc.plant))
		}
		if !tc.wantHit && len(rep.findings) > 0 {
			t.Errorf("%s: reported %v, but %q is not an unconditional assignment to a caller pin",
				tc.name, rep.findings, strings.TrimSpace(tc.plant))
		}
	}
}

// TestMakefileGateRefusesAnEmptyRead pins the read-floor: a truncated or
// unreadable Makefile has to FAIL, because a gate that reads nothing reports
// green (Rule 15).
func TestMakefileGateRefusesAnEmptyRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), makefileName)
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("writing an empty makefile: %v", err)
	}
	if _, err := scanMakefile(path); err == nil {
		t.Fatal("an empty Makefile was accepted: the read-floors did not fire, so the gate could read green-blind")
	}
	if _, err := scanMakefile(filepath.Join(t.TempDir(), "absent-Makefile")); err == nil {
		t.Fatal("a missing Makefile was accepted: a scan that never ran must not look like a pass")
	}
}
