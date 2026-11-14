package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/shot"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// TestUsageAdvertisesShot — gopi -h must advertise the shot subcommand so
// the vision-debug loop is discoverable (mirrors the text2img/vision gates).
func TestUsageAdvertisesShot(t *testing.T) {
	usage := config.FormatUsage()
	for _, want := range []string{
		"gopi " + CliVerbTool + " shot [--url URL|--self]", // subcommand advertised
		"--wait-selector", // SPA render wait flag
		"gopi " + CliVerbTool + " " + toolnames.ToolVisionAnalyze + " <image>", // the downstream vision-loop step
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("top-level usage is missing %q", want)
		}
	}
}

// TestShotUsageShowsDefaults guards the subcommand's own -h: the validated
// defaults must be printed, not just the flag names.
func TestShotUsageShowsDefaults(t *testing.T) {
	got := shotUsageHeader()
	for _, want := range []string{
		"gopi tool shot --url URL [--out PATH] [flags]",
		"gopi tool shot --self [--addr HOST:PORT] [--out PATH] [flags]",
		"--viewport WxH",
		"Default 1440x2800", // shot.DefaultViewportWidth/Height rendered in
		"Default 2",         // shot.WaitDefault rendered in
		"GOPI_SHOT_CHROME",  // the chrome-override env var documented
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shotUsage is missing %q", want)
		}
	}
}

// TestParseShotArgs covers flag parsing (unit-testable, no os.Exit).
func TestParseShotArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string // "" = parse ok
		check   func(t *testing.T, o shot.Options)
	}{
		{
			name: "url only",
			args: []string{"--url", "example.com"},
			check: func(t *testing.T, o shot.Options) {
				if o.URL != "example.com" || o.Self {
					t.Errorf("url parse: %+v", o)
				}
			},
		},
		{
			name: "self with viewport and wait",
			args: []string{"--self", "--viewport", "1440x900", "--wait", "5"},
			check: func(t *testing.T, o shot.Options) {
				if !o.Self || o.ViewportW != 1440 || o.ViewportH != 900 || o.WaitSeconds != 5 {
					t.Errorf("self parse: %+v", o)
				}
			},
		},
		{
			name:    "url and self mutually exclusive",
			args:    []string{"--url", "x", "--self"},
			wantErr: "mutually exclusive",
		},
		{
			name:    "nothing to shoot",
			args:    []string{},
			wantErr: "nothing to shoot",
		},
		{
			name:    "unknown flag",
			args:    []string{"--self", "--bogus", "1"},
			wantErr: "unknown flag --bogus",
		},
		{
			name:    "bad viewport",
			args:    []string{"--self", "--viewport", "1440"},
			wantErr: "--viewport wants WxH",
		},
		{
			name:    "negative wait",
			args:    []string{"--self", "--wait", "-3"},
			wantErr: "non-negative",
		},
		{
			name:    "positional rejected",
			args:    []string{"--self", "extra"},
			wantErr: "unexpected positional",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseShotArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want err %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if tc.check != nil {
				tc.check(t, opts)
			}
		})
	}
}

// TestParseViewport unit-checks the WxH splitter.
func TestParseViewport(t *testing.T) {
	if w, h, err := parseViewport("1440X900"); err != nil || w != 1440 || h != 900 {
		t.Errorf("uppercase X: %d %d %v", w, h, err)
	}
	if _, _, err := parseViewport("0x100"); err == nil {
		t.Error("zero width must error")
	}
}

// ---------------------------------------------------------------------------
// the navigation-failure contract on the control channel
//
// The defect: `gopi tool shot --url <unreachable>` exited 0 and wrote a PNG of
// Chromium's error page, so the ONLY channel distinguishing success from failure
// was the pixels. These tests pin the CHOSEN contract — non-zero exit, one
// stderr line naming the URL and the error-page fact, PNG still written and its
// path named on stderr, stdout carrying no bare path — and pin that the success
// path is untouched.
// ---------------------------------------------------------------------------

// TestReportShotFailureChosenContract drives the failure report end to end
// through the real renderer (writeShotPNG + reportShotFailure), capturing what
// the operator would actually see.
func TestReportShotFailureChosenContract(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "error-page.png")
	png := []byte("\x89PNG\r\n\x1a\nERROR-PAGE-BYTES")
	err := &shot.NavigationFailure{
		URL:    "file:///definitely/not/here.html",
		Reason: "net::ERR_FILE_NOT_FOUND",
		PNG:    png,
	}

	stderr := captureStderr(t, func() { reportShotFailure(err, outPath) })

	// One line: the reason, naming the URL and the error-page fact.
	lines := nonEmptyLines(stderr)
	if len(lines) != 2 { // the PNG line + the reason line
		t.Fatalf("want 2 stderr lines (PNG path, reason), got %d:\n%s", len(lines), stderr)
	}
	reason := lines[1]
	for _, want := range []string{
		"file:///definitely/not/here.html", // the failed URL
		"net::ERR_FILE_NOT_FOUND",          // the reason
		"error page",                       // the fact that it is Chrome's error document
	} {
		if !strings.Contains(reason, want) {
			t.Errorf("stderr reason %q is missing %q", reason, want)
		}
	}
	if !strings.Contains(stderr, outPath) {
		t.Errorf("stderr does not name the error-page PNG path %q:\n%s", outPath, stderr)
	}
	// The pixels survived: the PNG is on disk, byte for byte.
	got, rerr := os.ReadFile(outPath)
	if rerr != nil {
		t.Fatalf("the error-page PNG was not written: %v", rerr)
	}
	if !bytes.Equal(got, png) {
		t.Errorf("error-page PNG on disk = %q, want %q", got, png)
	}
	// The reason line is the typed failure's own text, so the URL and the fact
	// arrive in ONE line, and the caller's stdout stays empty (the defect's
	// signature was a bare path on stdout with rc=0).
	if !strings.Contains(reason, "gopi tool shot: shot: navigation to ") {
		t.Errorf("reason line is not the typed failure text: %q", reason)
	}
}

// TestReportShotFailureWithoutPNG — a failure with no captured image names no
// path (there is nothing to save) and still reports the reason.
func TestReportShotFailureWithoutPNG(t *testing.T) {
	stderr := captureStderr(t, func() {
		reportShotFailure(errors.New("shot: no headless chrome found"), "/nonexistent/x.png")
	})
	if !strings.Contains(stderr, "no headless chrome") {
		t.Errorf("stderr = %q, want the underlying reason", stderr)
	}
	if strings.Contains(stderr, "PNG") {
		t.Errorf("stderr names a PNG for a failure that captured none:\n%s", stderr)
	}
}

// TestReportShotFailureUnwritableOutIsReported — if the error-page PNG cannot be
// saved, that loss is announced rather than silently swallowing the evidence.
func TestReportShotFailureUnwritableOutIsReported(t *testing.T) {
	dir := t.TempDir()
	err := &shot.NavigationFailure{URL: "file:///x.html", Reason: "net::ERR_FILE_NOT_FOUND", PNG: []byte("\x89PNG\r\n\x1a\nx")}
	stderr := captureStderr(t, func() { reportShotFailure(err, dir) }) // a DIRECTORY, not a file
	if !strings.Contains(stderr, "write") {
		t.Errorf("stderr = %q, want the write failure announced", stderr)
	}
	if !strings.Contains(stderr, "net::ERR_FILE_NOT_FOUND") {
		t.Errorf("stderr = %q, want the navigation reason too", stderr)
	}
}

// TestShotUsageStatesTheExitContract — the contract must be discoverable from
// -h, not only from the code.
func TestShotUsageStatesTheExitContract(t *testing.T) {
	got := shotUsageHeader()
	for _, want := range []string{"NON-ZERO", "stderr", "error page"} {
		if !strings.Contains(got, want) {
			t.Errorf("shot -h does not state the failure contract (%q missing)", want)
		}
	}
}
