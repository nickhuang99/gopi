package main

// cli_voice_doubao_nospeech_test.go — the engine's SILENCE VERDICT is not a
// failure (task 01a11b39).
//
// THE DEFECT THIS PINS. The doubao AUC channel answers a window that carried no
// speech with a COMPLETED job and an empty transcript, and it says so with
// X-Api-Status-Code 20000003 ("Normal silence audio … no valid speech in
// audio"). `transcribeFileDoubao` returned that as an ERROR, and the chat
// `/voice` loop treats a non-nil error from a round as "the round FAILED": it
// reports the failure and LEAVES THE MODE. So ONE quiet window — a thinking
// pause, a breath, a window that opened before the operator started speaking —
// switched the microphone off for the rest of the session, and every later
// window the operator spoke into belonged to a mode that was already off.
//
// Measured (this host, 2026-10-08, logid 2026100819203030ED41BEECA127FF8C6E): a
// 6 s window came back 20000003 with duration 6000 and text "" — the audio
// ARRIVED (the duration proves it), the engine judged it speechless. The whole
// incident is that sentence being classified as a failure.
//
// WHY A FAKE SERVICE AND NOT A UNIT ASSERTION on the classifier: the fact under
// test is the CLASSIFICATION OF A REAL HTTP RESPONSE — the header the service
// sends, read through the transport, decoded into the typed error, and turned
// into a transcript-or-error by the production entry point. A test that called
// the predicate directly would assert the branch it was handed, not the branch
// the wire produces (Rule 31: a surface in no execution is untested).
//
// THE THREE LEGS ARE THE WHOLE CONTRACT, and the third is why the first is not a
// blanket "swallow every engine error":
//   1. 20000003 → ("", nil): a silence verdict, reported as an EMPTY TRANSCRIPT.
//   2. a real transcript → the text, untouched (the ordinary path still works).
//   3. a refusal → an error that still carries the vendor's own verdict.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/doubaoasr"
)

// doubaoNoSpeechStatus is the service's silence verdict, spelled through its ONE
// definition (Rule 1/13: never a literal at a comparison site).
const doubaoNoSpeechStatus = doubaoasr.StatusNoSpeech

// doubaoRefusalStatus is a REAL failure code — the resource-not-granted refusal
// this account produced while the streaming resource was probed. It is the
// control leg: it must keep failing.
const doubaoRefusalStatus = "45000030"

// doubaoNoSpeechService stands up the two AUC legs with a scripted query verdict
// and returns options pointed at it. `text` is the transcript the query body
// carries, so the same fixture serves the speech leg and the silence leg (the
// service sends BOTH as a completed job — the difference is the header and the
// empty result).
func doubaoNoSpeechService(t *testing.T, queryCode, text string) *voiceOptions {
	t.Helper()
	keyFile := filepath.Join(t.TempDir(), "apikey.txt")
	if err := os.WriteFile(keyFile, []byte("fixture-key-redaction-sentinel\n"), config.SettingsPrivateMode); err != nil {
		t.Fatalf("write the fixture key file: %v", err)
	}
	audio := filepath.Join(t.TempDir(), "window.wav")
	if err := os.WriteFile(audio, voiceTestWAVBytes(), config.SettingsPrivateMode); err != nil {
		t.Fatalf("write the fixture recording: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Api-Status-Code", doubaoasr.StatusOK)
		w.Header().Set("X-Tt-Logid", "SUBMIT-LOGID")
		fmt.Fprint(w, `{}`)
	})
	mux.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		// THE VERDICT IS THE HEADER, and the body is what the service sends with
		// it: a COMPLETED job whose audio_info.duration states the window really
		// was received. Replaying that shape is the point of the fixture.
		w.Header().Set("X-Api-Status-Code", queryCode)
		w.Header().Set("X-Api-Message", "Normal silence audio Handle response: no valid speech in audio")
		w.Header().Set("X-Tt-Logid", "2026100819203030ED41BEECA127FF8C6E")
		fmt.Fprintf(w, `{"audio_info":{"duration":6000},"result":{"text":%q}}`, text)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &voiceOptions{
		engine:        config.AudioEngineRecording,
		audio:         audio,
		doubaoKeyFile: keyFile,
		doubaoSubmit:  srv.URL + "/submit",
		doubaoQuery:   srv.URL + "/query",
	}
}

// TestVoiceDoubaoNoSpeechIsAnEmptyTranscriptNotAFailure is the regression for the
// defect above, and it is the WHOLE defect: the same verdict, through the same
// production entry point, must come back as silence rather than as a failed
// round.
func TestVoiceDoubaoNoSpeechIsAnEmptyTranscriptNotAFailure(t *testing.T) {
	opts := doubaoNoSpeechService(t, doubaoNoSpeechStatus, "")

	text, _, err := transcribeFile(opts, opts.audio)
	if err != nil {
		t.Fatalf("the service's silence verdict (X-Api-Status-Code %s) came back as a FAILURE: %v\n"+
			"a failed round makes the chat voice mode LEAVE, so this is the defect that switched the microphone off after the first quiet window", doubaoNoSpeechStatus, err)
	}
	if text != "" {
		t.Fatalf("the silence verdict produced %q, want an empty transcript", text)
	}
}

// TestVoiceDoubaoSpeechStillTranscribes is the other side of the same seam: a
// window the service DID hear must come back with its text, so the silence
// branch above cannot have swallowed the ordinary path.
func TestVoiceDoubaoSpeechStillTranscribes(t *testing.T) {
	const spoken = "现在开始连续语音输入测试。"
	opts := doubaoNoSpeechService(t, doubaoasr.StatusOK, spoken)

	text, _, err := transcribeFile(opts, opts.audio)
	if err != nil {
		t.Fatalf("transcribeFile: %v", err)
	}
	if text != spoken {
		t.Fatalf("transcript = %q, want %q", text, spoken)
	}
}

// TestVoiceDoubaoRefusalStillFails is the CONTROL leg: the silence verdict is
// classified, everything else is not. A refusal must keep failing and keep
// carrying the vendor's own code — a blanket "ignore the engine's errors" fix
// would pass the case above and fail here (Rule 15: no silent failure).
func TestVoiceDoubaoRefusalStillFails(t *testing.T) {
	opts := doubaoNoSpeechService(t, doubaoRefusalStatus, "")

	text, _, err := transcribeFile(opts, opts.audio)
	if err == nil {
		t.Fatalf("a refusal (X-Api-Status-Code %s) was reported as success with transcript %q — every real engine failure must stay loud", doubaoRefusalStatus, text)
	}
	if !strings.Contains(err.Error(), doubaoRefusalStatus) {
		t.Fatalf("the refusal error does not carry the vendor's verdict %s: %v", doubaoRefusalStatus, err)
	}
}

// TestDoubaoNoSpeechPredicateIsTheOneCode pins the predicate itself at the
// package that owns the vocabulary: the silence verdict is ONE code, and every
// neighbouring verdict (including a transport failure that carries no code at
// all) is not it.
func TestDoubaoNoSpeechPredicateIsTheOneCode(t *testing.T) {
	cases := []struct {
		name string
		err  *doubaoasr.APIError
		want bool
	}{
		{"the service's silence verdict", &doubaoasr.APIError{StatusCode: doubaoNoSpeechStatus}, true},
		{"success", &doubaoasr.APIError{StatusCode: doubaoasr.StatusOK}, false},
		{"still processing", &doubaoasr.APIError{StatusCode: doubaoasr.StatusProcessing}, false},
		{"a refusal", &doubaoasr.APIError{StatusCode: doubaoRefusalStatus}, false},
		{"no code at all (the request never reached a verdict)", &doubaoasr.APIError{}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := c.err.NoSpeech(); got != c.want {
			t.Errorf("%s: NoSpeech() = %v, want %v", c.name, got, c.want)
		}
	}
}
