package notifyfeed

// sink_test.go — which file the consumer should read, and the refusal when no
// file is named.
//
// The recogniser exists because `notify.cmd` is an arbitrary shell command, so
// there is no field anywhere that says "the sink is X". A recogniser that
// GUESSED would be worse than one that refuses: a consumer reading the wrong
// file reports "no events" forever, and a silent wrong answer is the exact
// failure mode this whole package is about.

import (
	"strings"
	"testing"
)

const (
	testSinkPath     = "/home/operator/.gopi/notify/task-events.jsonl"
	testOtherSinkTSV = "/var/log/notify.tsv"
)

func TestSinkFromNotifyCmdRecognisesTheDocumentedShape(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want string
	}{
		{
			name: "the documented cat >> FILE sink",
			cmd:  "cat >> " + testSinkPath,
			want: testSinkPath,
		},
		{
			name: "a fan-out through tee -a",
			cmd:  "awk '{print}' | tee -a " + testSinkPath + " > /dev/null",
			want: testSinkPath,
		},
		{
			name: "an arbitrary pipeline that still redirects",
			cmd:  "/usr/local/bin/render | gzip >> " + testSinkPath,
			want: testSinkPath,
		},
		{
			name: "a quoted path",
			cmd:  `cat >> "` + testSinkPath + `"`,
			want: testSinkPath,
		},
		{
			name: "a redirect fused with a file descriptor",
			cmd:  "sh -c 'render 1>>' >> " + testSinkPath,
			want: testSinkPath,
		},
		{
			name: "a different file without a jsonl extension",
			cmd:  "cat >> " + testOtherSinkTSV,
			want: testOtherSinkTSV,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SinkFromNotifyCmd(tc.cmd)
			if !ok {
				t.Fatalf("SinkFromNotifyCmd(%q) found no file, want %q", tc.cmd, tc.want)
			}
			if got != tc.want {
				t.Errorf("SinkFromNotifyCmd(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
		})
	}
}

// TestSinkFromNotifyCmdRefusesWhatItCannotKnow is the half that matters. The
// ssh shape names a path on ANOTHER host: resolving it here would have the
// consumer read this host's file (or nothing) and call it the peer's stream.
// Every other shape below names no local file at all, and the refusal is the
// answer — the caller then tells the operator to pass --sink.
func TestSinkFromNotifyCmdRefusesWhatItCannotKnow(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
	}{
		{"an ssh leg — the path belongs to the peer", `cat | ssh peer "cat >> /remote/task-events.jsonl"`},
		{"a webhook only", "curl -sS -X POST -d @- https://example.invalid/hook"},
		{"empty command", ""},
		{"whitespace only", "   "},
		{"a redirect with no target", "cat >>"},
		{"tee -a with no file", "cat | tee -a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SinkFromNotifyCmd(tc.cmd)
			if ok {
				t.Errorf("SinkFromNotifyCmd(%q) = %q, want a refusal (a wrong path is worse than none)", tc.cmd, got)
			}
		})
	}
}

// TestResolveSinkPrecedence: the flag wins, because a consumer that already
// knows its file must not have to reshape a shell command to be understood.
func TestResolveSinkPrecedence(t *testing.T) {
	flagPath := "/explicit/sink.jsonl"
	cmdLine := "cat >> " + testSinkPath

	got, whence := ResolveSink(flagPath, cmdLine)
	if got != flagPath || whence != SinkSourceFlag {
		t.Errorf("ResolveSink(flag, cmd) = (%q, %q), want (%q, %q)", got, whence, flagPath, SinkSourceFlag)
	}

	got, whence = ResolveSink("", cmdLine)
	if got != testSinkPath || whence != SinkSourceNotifyCmd {
		t.Errorf("ResolveSink(\"\", cmd) = (%q, %q), want (%q, %q)", got, whence, testSinkPath, SinkSourceNotifyCmd)
	}

	got, whence = ResolveSink("", "curl -X POST https://example.invalid")
	if got != "" || whence != SinkSourceNone {
		t.Errorf("ResolveSink(\"\", webhook) = (%q, %q), want (\"\", %q)", got, whence, SinkSourceNone)
	}
}

// TestSinkRefusalNamesTheChannel: a refusal that does not say what to do is the
// silent failure one layer up (Rule 15).
func TestSinkRefusalNamesTheChannel(t *testing.T) {
	// The refusal names the env var the operator sets. It comes from this
	// package's alias of the config constant, so the message cannot advertise a
	// spelling the resolver does not read (Rule 14).
	empty := SinkRefusal("")
	if empty == "" {
		t.Fatal("SinkRefusal(\"\") is empty — a refusal must say something")
	}
	if !strings.Contains(empty, NotifyCmdEnv) {
		t.Errorf("the no-configuration refusal does not name %s: %s", NotifyCmdEnv, empty)
	}
	guarded := SinkRefusal("cat | ssh peer \"cat >> /remote\"")
	if !strings.Contains(guarded, "--sink") {
		t.Errorf("the unrecognised-command refusal does not name --sink: %s", guarded)
	}
}
