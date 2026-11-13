package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The REAL report merger.log recorded when the rebuild probe auto-reverted
// every tagged merge (2026-09-13): `bin/gopi --version` answers with three
// lines, and the probe handed all three to the single-token parser.
const (
	liveReportRevision = "faf5f2190e8176d5c3d8b18562df696c427247c2"
	liveReportTip      = "faf5f219"
	liveReport         = "v20260913-1244-faf5f219 (faf5f2190e81)\n" +
		"revision: faf5f2190e8176d5c3d8b18562df696c427247c2\n" +
		"tag:      version/v20260913-1244-faf5f219"
)

// Replay: a report that names the tip must MATCH — as the revision binaryVersion
// now recovers from it (the fact line), and as the whole display a caller may
// still pass straight through. This is the assertion whose false verdict
// auto-reverted the merge.
func TestVersionMatchesReadsMultiLineReport(t *testing.T) {
	// What binaryVersion returns for liveReport once it reads the report.
	if !versionMatches(liveReportRevision, liveReportTip) {
		t.Errorf("versionMatches(%q, %q) = false; the reported revision IS the tip", liveReportRevision, liveReportTip)
	}
	// The whole report, passed through: still the tip.
	if !versionMatches(liveReport, liveReportTip) {
		t.Errorf("versionMatches(report, tip) = false; the report names the tip")
	}
	// The short revision the display line carries names the same commit.
	if !versionMatches("faf5f2190e81", liveReportTip) {
		t.Errorf("versionMatches(display revision group, tip) = false; it names the tip")
	}
	// No blanket always-true: a genuinely different tip still fails, and so
	// does an unrelated report.
	if versionMatches(liveReportRevision, "18f61785") {
		t.Errorf("versionMatches(%q, otherTip) = true; a wrong tip must fail", liveReportRevision)
	}
	if versionMatches("v20260913-1244-18f61785 (18f617856fe8)\nrevision: 18f617856fe8792867e0711561adc13bc6fe8697\ntag:      version/v20260913-1244-18f61785", liveReportTip) {
		t.Errorf("versionMatches(other report, tip) = true; a stale binary must fail")
	}
	// The zero-value case (Rule 15a): an empty revision — what the broken
	// probe produced out of a correct answer — never matches, and neither
	// does an empty report or a dirty one.
	if versionMatches("", liveReportTip) {
		t.Errorf("versionMatches(\"\", tip) = true; an unread version must fail")
	}
	if versionMatches(liveReport, "") {
		t.Errorf("versionMatches(report, \"\") = true; an unknown tip must fail")
	}
	if versionMatches(liveReportRevision+"-dirty", liveReportTip) {
		t.Errorf("versionMatches(dirty revision, tip) = true; a dirty build must fail")
	}
}

// The probe: the rebuild must ACCEPT a freshly built binary whose --version
// prints the real three-line report shape (display line + revision fact +
// tag fact). Before the fix this exact fixture failed with "version
// mismatch" and (in production) reverted the merge.
func TestRebuildAcceptsMultiLineVersionReport(t *testing.T) {
	repo, tip := rebuildTestRepo(t)
	full := rebuildTestGit(t, repo, "rev-parse", "HEAD")
	binDir := filepath.Join(repo, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+eventsFile)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "build-failed")
	// The report the fixture binary prints: the REAL multi-line shape
	// (merger.log 2026-09-13) naming THIS repo's tip, exactly as cmd/agent
	// prints it for a correctly built, tagged binary.
	report := "v20260913-1244-" + tip + " (" + full[:versionProbeShortRevLen] + ")\n" +
		"revision: " + full + "\n" +
		"tag:      version/v20260913-1244-" + tip
	reportFile := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(reportFile, []byte(report+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "gopi")
	buildCmd := "printf '#!/bin/sh\\nif [ \"$1\" = \"--version\" ]; then cat " + reportFile + "; fi\\n' > " +
		bin + " && chmod +x " + bin
	rebuildTestSettings(t, map[string]map[string]any{
		repo: {"build": buildCmd, "build_timeout_sec": 60},
	})
	res := RebuildAfterMerge(repo)
	if !res.OK {
		t.Fatalf("rebuild rejected a correct multi-line --version report: Err=%q", res.Err)
	}
	if strings.Contains(res.Err, "version mismatch") {
		t.Fatalf("version mismatch on a correct report: %q", res.Err)
	}
	if res.ActualVersion != report {
		t.Errorf("ActualVersion = %q, want the report", res.ActualVersion)
	}
	// No build-failed alarm: nothing failed.
	if data, err := os.ReadFile(eventsFile); err == nil && strings.Contains(string(data), "build-failed") {
		t.Errorf("build-failed fired for a correct build: %s", data)
	}
}

// versionProbeShortRevLen is the revision length the display's parenthesised
// group carries (internal/version.shortRevisionLen's value, stated here for
// the fixture's report rather than imported — the fixture must spell the
// report the way the producer does).
const versionProbeShortRevLen = 12

// The display is never handed to the single-token parser (Rule 13
// exhaustive-gate shape): gopiversion.RevisionOf may appear in production
// code ONLY at its own definition site in internal/version/version.go — the
// report-aware RevisionOfDisplay is the reader every other caller uses. A
// future re-inline of the one-token parse fails here instead of reverting
// merges in production.
func TestRevisionOfHasNoProductionCallersOutsideItsDefinition(t *testing.T) {
	const allowedFile = "version.go"
	root := filepath.Join("..", "..")
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.ToSlash(path) == filepath.ToSlash(filepath.Join(root, "internal", "version", allowedFile)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		if strings.Contains(string(data), gopiversionCallSite) {
			t.Errorf("%s calls %s — the version display must be read with RevisionOfDisplay (report-aware); the single-token parser drops the revision out of a multi-line report", path, gopiversionCallSite)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned == 0 {
		t.Fatal("scanned no production files — the guard would pass blind")
	}
}

// gopiversionCallSite is the single-token parser invocation this guard bans
// outside its definition site.
const gopiversionCallSite = ".RevisionOf("
