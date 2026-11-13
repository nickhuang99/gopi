package testutil

// The DYNAMIC half of the shared-stream fix: two budget runs racing on ONE
// scratch root must each keep their own stream.
//
// WHY IT EXISTS
// -------------
// The defect (measured 2026-09-19) was a shared WRITE TARGET: every run
// redirected `go test -json` into ONE host-wide name, so one file held 2422
// cmd/agent events followed by 1328 internal/tools events from a neighbour.
// A static predicate over the script's text
// (assertBudgetStreamIsPerRunAndTheLatestIsAPointer) pins the redirect SHAPE;
// this test pins the BEHAVIOUR that shape exists for — two runs, started
// together, on one scratch root. A shape can be edited into something that
// still races, and the behaviour can regress while every string the static gate
// greps stays in place.
//
// WHY A STUB `go`
// ---------------
// The property under test is the REDIRECT — which file each run's events land
// in — not what the packages under test do. Standing a stub `go` ahead of the
// real one on PATH makes the race deterministic and cheap (two ~1.5 s runs, no
// test bodies, and NO device work at all: Rule 27 bans generating load, and a
// test that manufactures the shared-file interleave without touching a device
// needs none). The stub emits the same stream line shape `go test -json` does,
// one package per run, with a pause between lines — the exact window in which a
// shared target would interleave.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// budgetConcurrentPackageRe pulls the Package value out of a stream line. It is
// deliberately narrower than a JSON parser: the test reads the SAME text the
// script's own readers match on, so a stream line shape change is caught here.
var budgetConcurrentPackageRe = regexp.MustCompile(`"Package":"([^"]+)"`)

const (
	// budgetConcurrentStreamPrefix is the per-run stream name prefix the script
	// declares (`raw_json_prefix`), repeated here as the gate's expectation.
	budgetConcurrentStreamPrefix = "run-"
	// budgetConcurrentBudgetSubdir is the artifact directory the script uses
	// under the scratch queue (`budget_subdir`).
	budgetConcurrentBudgetSubdir = "test-budget"
	// budgetConcurrentModuleFallback is used only if go.mod cannot be read.
	budgetConcurrentModuleFallback = "github.com/nick/gopi"
	// budgetConcurrentRawJSONLabel is the summary line naming the stream a run
	// rendered its numbers from.
	budgetConcurrentRawJSONLabel = "raw json: "
	// budgetConcurrentGoStubEnv carries the module path to the stub `go`.
	budgetConcurrentGoStubEnv = "TB_GATE_STUB_MODULE"
	// budgetConcurrentStubPause is the pause between the stub's stream lines:
	// long enough that two runs overlap, short enough to stay a smoke test.
	budgetConcurrentStubPause = "0.7"
)

// budgetConcurrentRun is one launched budget run.
type budgetConcurrentRun struct {
	pkg    string
	out    string
	stream string
}

// TestBudgetConcurrentRunsKeepTheirOwnStreams runs the real script twice in
// parallel against ONE scratch root and asserts each run's stream carries
// exactly the package that run was asked for.
func TestBudgetConcurrentRunsKeepTheirOwnStreams(t *testing.T) {
	root, err := repoRootFromCwd()
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	if _, err := exec.LookPath(budgetShellBin); err != nil {
		t.Skipf("%s not available: %v", budgetShellBin, err)
	}

	module := budgetConcurrentModulePath(t, root)
	scratchHome := t.TempDir()
	stubDir := budgetConcurrentWriteGoStub(t, t.TempDir())

	// Two DIFFERENT packages, launched together on ONE scratch root.
	runs := []*budgetConcurrentRun{
		{pkg: "./internal/smokegate/"},
		{pkg: "./internal/heavytest/"},
	}
	cmds := make([]*exec.Cmd, len(runs))
	files := make([]*os.File, len(runs))
	for i, r := range runs {
		r.out = filepath.Join(scratchHome, fmt.Sprintf("run-%d.out", i))
		fh, err := os.Create(r.out)
		if err != nil {
			t.Fatalf("create %s: %v", r.out, err)
		}
		files[i] = fh
		cmd := exec.Command(budgetShellBin, filepath.Join(root, budgetScriptPath), r.pkg)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			ScratchHomeEnvName+"="+scratchHome,
			"PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"),
			budgetConcurrentGoStubEnv+"="+module,
		)
		cmd.Stdout, cmd.Stderr = fh, fh
		cmds[i] = cmd
	}
	// Start every run BEFORE waiting on any: that overlap is the whole test.
	for i, cmd := range cmds {
		if err := cmd.Start(); err != nil {
			for _, fh := range files {
				if fh != nil {
					fh.Close() //stylegate:ignore closes a fixture handle the test owns
				}
			}
			t.Fatalf("start run for %s: %v", runs[i].pkg, err)
		}
	}
	for i, cmd := range cmds {
		waitErr := cmd.Wait()
		files[i].Close()                    //stylegate:ignore closes fixture files during teardown
		body, _ := os.ReadFile(runs[i].out) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
		if waitErr != nil {
			t.Fatalf("run for %s failed: %v\n%s", runs[i].pkg, waitErr, body)
		}
		runs[i].stream = budgetConcurrentStreamFromOutput(t, string(body), runs[i].pkg)
	}

	budgetDir := filepath.Join(scratchHome, "queue", budgetConcurrentBudgetSubdir)

	// (1) The two runs must not render from ONE stream, and each stream must
	// carry exactly its own package — the measured defect in miniature.
	if runs[0].stream == runs[1].stream {
		t.Fatalf("both runs rendered from ONE stream (%s) — the shared write target is back", runs[0].stream)
	}
	for _, r := range runs {
		got := budgetConcurrentPackagesIn(t, r.stream)
		want := budgetConcurrentPackageFor(module, r.pkg)
		if len(got) != 1 || got[0] != want {
			t.Errorf("the stream %s (rendered for %s) carries %v, want exactly [%s] — a neighbour's events are being summarised as this run's own",
				filepath.Base(r.stream), r.pkg, got, want)
		}
	}

	// (2) Every run's stream is a per-run name under the budget dir, existing
	// as a file in its own right — never a name two runs share.
	for _, r := range runs {
		if filepath.Dir(r.stream) != budgetDir {
			t.Errorf("stream %s is not under the budget dir %s", r.stream, budgetDir)
		}
		if !strings.HasPrefix(filepath.Base(r.stream), budgetConcurrentStreamPrefix) {
			t.Errorf("stream %s does not carry the per-run prefix %q", r.stream, budgetConcurrentStreamPrefix)
		}
	}

	// (3) The canonical latest name is a POINTER to a completed stream, never a
	// regular file: a writable file at that name is the defect itself, and a
	// pointer to a COMPLETED run is what makes it safe to follow.
	latest := filepath.Join(budgetDir, budgetLatestName)
	fi, err := os.Lstat(latest)
	if err != nil {
		t.Fatalf("%s must be published as a pointer: %v", budgetLatestName, err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is a %s, not a symlink — a shared write target is exactly the defect this fix removes", budgetLatestName, fi.Mode().Type())
	}
	target, err := filepath.EvalSymlinks(latest)
	if err != nil {
		t.Fatalf("resolve %s: %v", latest, err)
	}
	if target != runs[0].stream && target != runs[1].stream {
		t.Errorf("%s points at %s, which is neither run's stream (%s / %s)", budgetLatestName, target, runs[0].stream, runs[1].stream)
	}
}

// budgetConcurrentPackagesIn returns the distinct packages a stream carries.
func budgetConcurrentPackagesIn(t *testing.T, stream string) []string {
	t.Helper()
	body, err := os.ReadFile(stream)
	if err != nil {
		t.Fatalf("read stream %s: %v", stream, err)
	}
	seen := map[string]bool{}
	var pkgs []string
	for _, line := range strings.Split(string(body), "\n") {
		for _, m := range budgetConcurrentPackageRe.FindAllStringSubmatch(line, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				pkgs = append(pkgs, m[1])
			}
		}
	}
	return pkgs
}

// budgetConcurrentStreamFromOutput reads the `raw json: <path>` line the run
// printed and returns the path it summarised from.
func budgetConcurrentStreamFromOutput(t *testing.T, output, pkg string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, budgetConcurrentRawJSONLabel)
		if idx < 0 {
			continue
		}
		path := strings.TrimSpace(line[idx+len(budgetConcurrentRawJSONLabel):])
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("run for %s named the stream %s, which does not exist: %v", pkg, path, err)
		}
		return path
	}
	t.Fatalf("no %q line in the run for %s output — a run must name the stream it rendered from", budgetConcurrentRawJSONLabel, pkg)
	return ""
}

// budgetConcurrentModulePath reads the module path from go.mod, falling back to
// the repo's own path (the stub must emit the same import paths a real `go test`
// would, or the run's scope check would fire).
func budgetConcurrentModulePath(t *testing.T, root string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err == nil {
		for _, line := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(line, "module ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "module "))
			}
		}
	}
	return budgetConcurrentModuleFallback
}

// budgetConcurrentPackageFor maps a `./internal/x/` argument to its import path.
func budgetConcurrentPackageFor(module, pkg string) string {
	return module + "/" + strings.TrimSuffix(strings.TrimPrefix(pkg, "./"), "/")
}

// budgetConcurrentWriteGoStub writes a `go` stub that answers `go test …` with a
// stream naming the package it was asked for, pausing between lines so two
// concurrent runs genuinely overlap. `exec.LookPath` finds it via PATH.
func budgetConcurrentWriteGoStub(t *testing.T, dir string) string {
	t.Helper()
	script := "#!/bin/bash\n" +
		"# Stands in for `go test -json`: one package, the stream line shape a real\n" +
		"# run emits, with a gap between lines so concurrent runs overlap. The module\n" +
		"# path arrives in $" + budgetConcurrentGoStubEnv + " (the gate sets it), so the\n" +
		"# stub emits the same import paths a real `go test` would and the run's own\n" +
		"# scope check stays silent — a stub with a hardcoded path would fire it.\n" +
		"module=\"${" + budgetConcurrentGoStubEnv + ":?the gate must supply the module path}\"\n" +
		"pkg=\"\"\n" +
		"for a in \"$@\"; do case \"$a\" in ./*|*/*) pkg=\"$a\";; esac; done\n" +
		"import_path=\"$module/${pkg#./}\"\n" +
		"import_path=\"${import_path%/}\"\n" +
		"printf '{\"Action\":\"start\",\"Package\":\"%s\"}\\n' \"$import_path\"\n" +
		"sleep " + budgetConcurrentStubPause + "\n" +
		"printf '{\"Action\":\"output\",\"Package\":\"%s\",\"Test\":\"TestStub\",\"Output\":\"=== RUN\\n\"}\\n' \"$import_path\"\n" +
		"sleep " + budgetConcurrentStubPause + "\n" +
		"printf '{\"Action\":\"pass\",\"Package\":\"%s\",\"Test\":\"TestStub\",\"Elapsed\":0.1}\\n' \"$import_path\"\n" +
		"printf '{\"Action\":\"pass\",\"Package\":\"%s\",\"Elapsed\":1.0}\\n' \"$import_path\"\n" +
		"exit 0\n"
	path := filepath.Join(dir, "go")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write go stub: %v", err)
	}
	return dir
}
