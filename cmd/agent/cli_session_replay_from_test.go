package main

// cli_session_replay_from_test.go — the DATA-PLANE grammar of
// `gopi session replay`: which artifact is drawn, and what happens when a caller
// demands one that is not there.
//
// The parser is driven DIRECTLY (it is a pure function: argv in, options out), so
// these arms cannot be satisfied by a page's wording drifting into agreement with
// a flag the binary does not accept — the flag vocabulary is asserted here, not
// read off `-h`.

import (
	"os"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/replayplay"
)

// TestSessionReplayFromFlagGrammar pins the three sources and the DEFAULT. The
// default is the whole point of this surface (the verbatim leg wins whenever the
// session has one), so it is asserted as a value rather than described.
func TestSessionReplayFromFlagGrammar(t *testing.T) {
	t.Run("the default is auto", func(t *testing.T) {
		opts, err := parseSessionReplayArgs([]string{"some-session"})
		if err != nil {
			t.Fatalf("parseSessionReplayArgs: %v", err)
		}
		if opts.from != replayplay.StreamFromAuto {
			t.Fatalf("default --from = %q, want %q (the verbatim leg must win by default)",
				opts.from, replayplay.StreamFromAuto)
		}
		if opts.hasVerify {
			t.Fatal("--verify was reported present on a command line that never named it")
		}
	})
	for _, word := range []string{replayplay.StreamFromAuto, replayplay.StreamFromStream, replayplay.StreamFromDump} {
		t.Run("separated "+word, func(t *testing.T) {
			opts, err := parseSessionReplayArgs([]string{"some-session", sessionReplayFromFlag, word})
			if err != nil {
				t.Fatalf("parseSessionReplayArgs(%s %s): %v", sessionReplayFromFlag, word, err)
			}
			if opts.from != word {
				t.Fatalf("--from %s parsed as %q", word, opts.from)
			}
		})
		t.Run("joined "+word, func(t *testing.T) {
			opts, err := parseSessionReplayArgs([]string{"some-session", sessionReplayFromFlag + "=" + word})
			if err != nil {
				t.Fatalf("parseSessionReplayArgs(%s=%s): %v", sessionReplayFromFlag, word, err)
			}
			if opts.from != word {
				t.Fatalf("--from=%s parsed as %q", word, opts.from)
			}
		})
	}
	t.Run("a misspelling is refused by name", func(t *testing.T) {
		_, err := parseSessionReplayArgs([]string{"some-session", sessionReplayFromFlag, "dum"})
		if err == nil {
			t.Fatal("--from dum was accepted: a caller who typed it believes they forced the dump")
		}
		if !strings.Contains(err.Error(), replayplay.StreamFromDump) {
			t.Fatalf("the refusal %q does not name the words it does accept", err)
		}
	})
	t.Run("a missing operand is refused", func(t *testing.T) {
		if _, err := parseSessionReplayArgs([]string{"some-session", sessionReplayFromFlag}); err == nil {
			t.Fatal("--from with no operand was accepted")
		}
	})
}

// TestSessionReplayVerifyFlagGrammar: --verify is a MEASUREMENT, so it is
// mutually exclusive with every view flag — an accepted-and-ignored option
// teaches a caller something false about their own command line (Rule 15).
func TestSessionReplayVerifyFlagGrammar(t *testing.T) {
	t.Run("it records its operand and its presence", func(t *testing.T) {
		opts, err := parseSessionReplayArgs([]string{"a-session", sessionReplayVerifyFlag, "other-session"})
		if err != nil {
			t.Fatalf("parseSessionReplayArgs: %v", err)
		}
		if !opts.hasVerify || opts.verify != "other-session" {
			t.Fatalf("--verify parsed as hasVerify=%v verify=%q", opts.hasVerify, opts.verify)
		}
	})
	t.Run("joined form", func(t *testing.T) {
		opts, err := parseSessionReplayArgs([]string{"a-session", sessionReplayVerifyFlag + "=/tmp/x.screen.stream"})
		if err != nil {
			t.Fatalf("parseSessionReplayArgs: %v", err)
		}
		if !opts.hasVerify || opts.verify != "/tmp/x.screen.stream" {
			t.Fatalf("joined --verify parsed as hasVerify=%v verify=%q", opts.hasVerify, opts.verify)
		}
	})
	t.Run("an empty operand is refused", func(t *testing.T) {
		if _, err := parseSessionReplayArgs([]string{"a-session", sessionReplayVerifyFlag}); err == nil {
			t.Fatal("--verify with no operand was accepted")
		}
		if _, err := parseSessionReplayArgs([]string{"a-session", sessionReplayVerifyFlag + "="}); err == nil {
			t.Fatal("--verify= (empty) was accepted")
		}
	})
	t.Run("every view flag is refused beside it, by name", func(t *testing.T) {
		views := [][]string{
			{"--turns"},
			{"--rounds"},
			{"--turn", "1"},
			{"--round", "1"},
			{sessionReplayTerminalFlag, "xterm"},
		}
		for _, view := range views {
			args := append([]string{"a-session", sessionReplayVerifyFlag, "other"}, view...)
			_, err := parseSessionReplayArgs(args)
			if err == nil {
				t.Fatalf("%v was accepted beside %s: it plays nothing, so the view would be silently dropped",
					view, sessionReplayVerifyFlag)
			}
			if !strings.Contains(err.Error(), "plays nothing") {
				t.Fatalf("the refusal for %v does not say why: %v", view, err)
			}
		}
	})
}

// TestSessionReplayHeaderReadingNoteStatesTheGap pins the DEGRADE EXPRESSION of a
// screen stream whose header line could not be read. The frames still play, so
// without this clause the replay looks entirely normal while a bool that claims
// "written by the current build" was never actually read (Rule 15). Both silent
// cases are asserted too: a taken reading says nothing, and a material with no
// screen stream at all is the ORDINARY shape, not a degrade.
func TestSessionReplayHeaderReadingNoteStatesTheGap(t *testing.T) {
	t.Run("unreadable header is stated with its reason", func(t *testing.T) {
		mat := replayplay.Material{
			Stream: &replayplay.ScreenStream{HeaderUnreadable: "permission denied"},
		}
		got := sessionReplayHeaderReadingNote(mat)
		for _, want := range []string{"header reading unavailable", "UNKNOWN", "permission denied"} {
			if !strings.Contains(got, want) {
				t.Fatalf("the note %q never says %q", got, want)
			}
		}
	})
	t.Run("a taken reading is silent", func(t *testing.T) {
		mat := replayplay.Material{Stream: &replayplay.ScreenStream{LegacyHeader: true}}
		if got := sessionReplayHeaderReadingNote(mat); got != "" {
			t.Fatalf("a stream whose header WAS read printed the degrade %q", got)
		}
	})
	t.Run("no screen stream is not a degrade", func(t *testing.T) {
		if got := sessionReplayStreamNote(replayplay.Material{}); got != "" {
			t.Fatalf("a session that recorded no screen printed %q; that is the ordinary worker-run shape", got)
		}
	})
	t.Run("both notes ride together", func(t *testing.T) {
		mat := replayplay.Material{
			StreamNote: "forced by --from dump",
			Stream:     &replayplay.ScreenStream{HeaderUnreadable: "is a directory"},
		}
		got := sessionReplayStreamNote(mat)
		if !strings.Contains(got, "verbatim leg skipped") || !strings.Contains(got, "header reading unavailable") {
			t.Fatalf("the combined note lost one of its two clauses: %q", got)
		}
	})
}

// TestPrintSessionReplayTurnsCarriesTheUnreadableHeaderNote is the arm that proves
// the clause REACHES the printed line rather than only existing as a helper: a
// reader of `gopi session replay <id> --turns` is the one who would otherwise take
// `LegacyHeader=false` for a finding.
func TestPrintSessionReplayTurnsCarriesTheUnreadableHeaderNote(t *testing.T) {
	mat := replayplay.Material{
		SessionID:   "sid",
		SourceLabel: replayplay.SourceLabelStream,
		Cols:        24,
		Rows:        4,
		Source:      "session.jsonl",
		Stream:      &replayplay.ScreenStream{HeaderUnreadable: "operation not permitted"},
	}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, rerr := r.Read(buf)
			sb.Write(buf[:n])
			if rerr != nil {
				break
			}
		}
		done <- sb.String()
	}()
	printSessionReplayTurns(mat)
	w.Close() //stylegate:ignore closes the write end of a pipe this test created, after the view has printed
	os.Stdout = old
	out := <-done
	for _, want := range []string{"artifacts=session.jsonl", "header reading unavailable", "operation not permitted"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the --turns view never printed %q:\n%s", want, out)
		}
	}
}

// TestSessionReplayUsageDocumentsBothNewFlags pins the `-h` page against the
// parser: every word the parser accepts is on the page, with the degrade
// vocabulary a reader must be able to recognise.
func TestSessionReplayUsageDocumentsBothNewFlags(t *testing.T) {
	for _, want := range []string{
		sessionReplayFromFlag,
		sessionReplayVerifyFlag,
		replayplay.StreamFromAuto,
		replayplay.StreamFromStream,
		replayplay.StreamFromDump,
		"first divergence: none",
		replayplay.SourceLabelStream,
		replayplay.SourceLabelDump,
	} {
		if !strings.Contains(sessionReplayUsage, want) {
			t.Fatalf("the -h page never mentions %q", want)
		}
	}
}
