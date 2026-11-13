package testutil

// The DISK EXHAUSTION verdict's gate (measured 2026-10-15, this host, /home at
// 100%).
//
// WHAT IS BEING PINNED, and why it needs a gate rather than a comment. The Go
// linker's ENOSPC death arrives as
//
//	link: mapping output file failed: no space left on device
//	FAIL	<import path> [build failed]
//
// and that LAST line is character-for-character the line a genuine compile error
// produces. The merge-time BUILD gate learned this on 2026-10-12
// (internal/tools/merge_build_gate.go's mergeBuildGateDiskExhausted) and abstains;
// the DECLARED TEST ENTRY POINT did not, so the surface a developer actually runs
// kept reporting every red on a full disk as a code failure. This file drives
// that entry point against a stub `go` and asserts the two answers are told
// apart — plus the false-positive the design exists to prevent.
//
// THE THREE ARMS, and the third is the load-bearing one:
//
//  1. the measured shape (a build refused by the filesystem, no compiler
//     diagnostic anywhere) → its OWN verdict and exit code, naming the package;
//  2. the CONTROL: a build-failed package that DOES carry a compiler diagnostic
//     (`file:line:col:`) → the ordinary failure verdict, and NO disk banner. A
//     verdict that could not tell these apart would relabel every real compile
//     error as a full disk;
//  3. the LIBRARY arm: a stream whose only mention of the phrase lives in a
//     PASSING test's own output → no disk verdict at all. This is not
//     hypothetical: this repository's own gates print such fixtures, so a
//     classifier built on grepping the raw stream would be red on a green run.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// budgetDiskStubName is the file the stub is written as — the member of PATH
	// the entry point resolves `go` to.
	budgetDiskStubName = "go"
	// budgetDiskStubBuildCase is the stub's answer to the entry point's own
	// tested-binary build: success, so the run's "not evidence about this tree"
	// preflight stays out of the way and only the test run is under test.
	budgetDiskStubBuildCase = "*build*"

	// budgetDiskPkg is the package the stub reports as failed. It is the same
	// one the no-events gate uses, so the entry point's stream-scope check has a
	// resolvable scope and only the verdict under test moves.
	budgetDiskPkg = "./internal/smokegate/"

	// budgetDiskLinkLine is the MEASURED linker death, copied verbatim from the
	// 2026-10-15 run on this host: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`
	// at a full partition printed exactly this one line and nothing else.
	budgetDiskLinkLine = "link: mapping output file failed: no space left on device"
	// budgetDiskCompilerLine is arm 2's diagnostic: the shape that proves the
	// revision WAS compiled, so the failure is about the code.
	budgetDiskCompilerLine = "./broken.go:3:1: syntax error"

	// budgetDiskBanner is the verdict's own headline — the string the report is
	// grepped for and the only place it is spelled on the test side.
	budgetDiskBanner = "DISK EXHAUSTION, NOT A TEST VERDICT"
	// budgetDiskExit is the verdict's own exit code. It is NOT 1: a filesystem
	// refusal is not a failing assertion, and 1 is exactly the reading that hid
	// the cause.
	budgetDiskExit = 7
)

// budgetDiskStream renders a `go test -json` stream for the requested package:
// one build-failed package whose compiler rows carry diagnostic (arm 2) or not
// (arm 1).
//
// The rows are the SHAPE the toolchain emits (measured on run-1791849386-333342
// and pinned by the failures gate): the diagnostics arrive as `build-output`
// events attributed by ImportPath with the `[<pkg>.test]` suffix, and the verdict
// arrives as an `Action=="fail"` event carrying FailedBuild.
func budgetDiskStream(t *testing.T, root string, diagnostic string) string {
	t.Helper()
	pkg := budgetDiskPackagePath(t, root)
	rows := []string{
		`{"Action":"start","Package":"` + pkg + `"}`,
		`{"Action":"output","Package":"` + pkg + `","Output":"FAIL\t` + pkg + ` [build failed]\n"}`,
		`{"ImportPath":"` + pkg + ` [` + pkg + `.test]","Action":"build-output","Output":"# ` + pkg + ` [` + pkg + `.test]\n"}`,
	}
	if diagnostic != "" {
		rows = append(rows, `{"ImportPath":"`+pkg+` [`+pkg+`.test]","Action":"build-output","Output":"`+diagnostic+`\n"}`)
	} else {
		// The measured shape: the refusal ITSELF is printless. The linker's one
		// line is the whole evidence, and it is not a compiler diagnostic.
		rows = append(rows, `{"ImportPath":"`+pkg+` [`+pkg+`.test]","Action":"build-output","Output":"`+budgetDiskLinkLine+`\n"}`)
	}
	rows = append(rows,
		`{"ImportPath":"`+pkg+` [`+pkg+`.test]","Action":"build-fail"}`,
		`{"Action":"fail","Package":"`+pkg+`","Elapsed":0,"FailedBuild":"`+pkg+` [`+pkg+`.test]"}`,
	)
	return strings.Join(rows, "\n") + "\n"
}

// budgetDiskPackagePath is the import path the stub's events carry (the module's
// own path plus the requested package), composed the way the entry point's own
// scope check composes it — never typed twice by hand.
func budgetDiskPackagePath(t *testing.T, root string) string {
	t.Helper()
	return budgetConcurrentModulePath(t, root) + "/" +
		strings.TrimPrefix(strings.TrimSuffix(budgetDiskPkg, "/"), "./")
}

// budgetDiskPathToken is the import path the verdict must NAME — the package it
// is about, mapped the same way the stream's events are.
func budgetDiskPathToken(t *testing.T, root string) string {
	t.Helper()
	return budgetDiskPackagePath(t, root)
}

// budgetDiskStubGoOnPath installs a stub `go` that answers the entry point's
// build with success and its test run with one build-failed package — carrying
// `diagnostic` when it is given, the linker's own line when it is not.
func budgetDiskStubGoOnPath(t *testing.T, root, diagnostic string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "stubbin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the stub bin dir: %v", err)
	}
	stream := budgetDiskStream(t, root, diagnostic)
	body := "#!/usr/bin/env bash\n" +
		"# test-budget disk-exhaustion gate fixture: a `go` whose build is refused" +
		" by the filesystem. Generated in a test's TempDir, never committed.\n" +
		"case \"$*\" in\n" +
		"  " + budgetDiskStubBuildCase + ") exit 0 ;;\n" +
		"esac\n" +
		"printf '%s\\n' " + shellQuote(stream) + "\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, budgetDiskStubName), []byte(body), 0o755); err != nil {
		t.Fatalf("write the stub go: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// shellQuote renders s as a single-quoted shell word (the helper this package's
// launcher gate already uses — reused, not re-declared: one definition, Rule 14),
// so a multi-line stream can be printed by the stub without being re-parsed by
// the shell. The fixture must not be a quoting experiment — the embedded `"` and
// `\t` are the stream's, and the stub prints them byte for byte.

// TestBudgetDiskExhaustionIsItsOwnVerdict is the acceptance gate for the verdict:
// the measured shape gets its OWN reported condition and exit code, and a build
// failure that carries a compiler diagnostic does NOT.
func TestBudgetDiskExhaustionIsItsOwnVerdict(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}

	for _, tc := range []struct {
		name       string
		diagnostic string
		wantCode   int
		wantBanner bool
	}{
		// Arm 1 — the measured incident: the filesystem refused the write and the
		// toolchain never named a file, a line and a column. The verdict is NOT a
		// test failure.
		{name: "the filesystem refuses the build", diagnostic: "", wantCode: budgetDiskExit, wantBanner: true},
		// Arm 2 — the control. Same package, same `[build failed]` line, but the
		// build carried a diagnostic: the revision WAS compiled, so this is an
		// ordinary red and relabelling it would be a worse defect than the one
		// being fixed.
		{name: "a real compile error keeps the failure verdict", diagnostic: budgetDiskCompilerLine, wantCode: 1, wantBanner: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budgetDiskStubGoOnPath(t, root, tc.diagnostic)
			out, code := budgetRunThroughEntryPoint(t, root, budgetDiskPkg)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d\n%s", code, tc.wantCode, out)
			}
			got := strings.Contains(out, budgetDiskBanner)
			if got != tc.wantBanner {
				t.Errorf("disk-exhaustion banner present = %t, want %t\n%s", got, tc.wantBanner, out)
			}
			if tc.wantBanner {
				// The verdict must NAME what it is about, and must say the red
				// above it is not a code verdict — the two sentences a reader
				// acts on.
				if !strings.Contains(out, "FALSE RED") {
					t.Errorf("the verdict does not state that the [build failed] lines are false reds\n%s", out)
				}
				if !strings.Contains(out, budgetDiskPathToken(t, root)) {
					t.Errorf("the verdict does not name the package it is about (%q)\n%s", budgetDiskPathToken(t, root), out)
				}
				if !strings.Contains(out, "disk:") {
					t.Errorf("the verdict carries no filesystem figures\n%s", out)
				}
			}
			// Whatever the verdict, the run must still report its OWN artifacts:
			// a verdict that lost the stream path is not evidence.
			if !strings.Contains(out, budgetNoMatchRawJSONLine) {
				t.Errorf("the run printed no %q line\n%s", budgetNoMatchRawJSONLine, out)
			}
		})
	}
}
