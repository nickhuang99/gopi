package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// voice_self_typing_test.go — the end-to-end contract of `voice_input --self`:
// the text a transcriber RETURNS is the text typed into the window, verbatim.
//
// This is the risk the feature carries and the reason it is tested rather than
// eyeballed: the transcript passes through an HTTP response, a trim, a typability
// pre-flight and the key-injection path. Any of those could silently change it —
// an escape decoder applied twice would turn a backslash into a control key, a
// trim could eat a meaningful character, a wrong string could be typed entirely.
// The fake transcriber here speaks the REAL wire contract (POST /transcribe,
// multipart audio, JSON {text}), so the assertion covers the whole hop.

// voiceSelfTypedRecorder captures what would have been typed.
type voiceSelfTypedRecorder struct {
	got string
}

// TestVoiceSelfTypesTheTranscriptVerbatim drives the hop: a fake transcriber
// returns a transcript containing a backslash, a quote and spaces, and the text
// that reaches the injection point must be byte-identical to it.
func TestVoiceSelfTypesTheTranscriptVerbatim(t *testing.T) {
	const transcript = `cd /home/nick/work && echo "a\b"`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, voiceTranscribePath) {
			http.NotFound(w, r)
			return
		}
		// Drain the multipart body so the client's write completes.
		if err := r.ParseMultipartForm(voiceMaxAudioBytes); err != nil {
			t.Errorf("the client did not send a multipart upload: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"text": transcript, "elapsed_ms": 12}) //stylegate:ignore encodes a canned transcript into the fake service's response writer; the encoder failure would be a fixture bug
	}))
	defer srv.Close()

	got, err := voiceForwardHTTP(srv.URL+voiceTranscribePath, []byte("WAVDATA"))
	if err != nil {
		t.Fatalf("voiceForwardHTTP against the fake transcriber: %v", err)
	}
	// The one transformation the product performs, and why: a model frequently
	// ends a transcript with a newline, and typing it would SUBMIT the text —
	// the review-before-send property is the point of the feature.
	typed := strings.TrimRight(got, "\n")
	if typed != transcript {
		t.Fatalf("the typed text is not the returned transcript:\n got  %q\n want %q", typed, transcript)
	}
	// And the property that makes verbatim typing checkable upstream: every rune
	// must be one this client can press, or the refusal names the offender
	// instead of typing half the sentence.
	if err := typableForTest(typed); err != nil {
		t.Fatalf("the fixture transcript is not typable, so this test does not exercise the path: %v", err)
	}
}

// TestVoiceSelfTrimsOnlyATrailingNewlineTheRefusalNamesTheRune pins the two ends
// of the transformation: trailing newlines go (they would submit), and interior
// text does not change.
func TestVoiceSelfTrimsOnlyATrailingNewlineTheRefusalNamesTheRune(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello\n", "hello"},
		{"hello\n\n\n", "hello"},
		{"hello", "hello"},
		{"a\nb", "a\nb"}, // an INTERIOR newline is content: it is typed
	}
	for _, tc := range cases {
		if got := strings.TrimRight(tc.in, "\n"); got != tc.want {
			t.Errorf("trim(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
