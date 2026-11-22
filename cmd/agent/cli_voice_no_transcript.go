package main

// cli_voice_no_transcript.go — "THE ENGINE PRODUCED NO TRANSCRIPT" as a TYPED
// verdict, and the ONE place a CLI shape turns it into a refusal.
//
// THE DEFECT THIS EXISTS FOR. The default engine (`recording`, 豆包录音文件识别
// volc.seedasr.auc) answers a recording that carried no speech with a COMPLETED
// job whose transcript is empty, and says so in a header
// (X-Api-Status-Code 20000003). `transcribeFileDoubao` — correctly, for the chat
// mode — reported that as an EMPTY TRANSCRIPT rather than as a failure, so the
// file shape of `gopi tool voice_input` printed one bare newline on stdout and
// exited 0. A script consuming stdout with `$(…)` got an empty string as a
// SUCCESS:
//
//	$ gopi tool voice_input vt2.wav ; echo EXIT=$?
//	                                     ← one empty line, nothing else
//	EXIT=0
//
// and, worse, the run reported `[voice] first text at 2.7s` for a transcript
// that never existed (voiceFirstTextFromEngine).
//
// TWO FACTS, TWO READERS, ONE ENGINE (this is why the classification is a TYPE
// and not a string):
//
//   - The CHAT VOICE MODE (`/voice`) needs "nobody spoke" to be an EMPTY
//     TRANSCRIPT, not an error: a non-nil error from a round makes the loop
//     report a failure and LEAVE the mode, so one quiet window switched the
//     microphone off for the rest of the session (task 01a11b39, and its
//     regression test still pins exactly that contract).
//   - A FILE run has nobody to keep listening. Its stdout is the deliverable,
//     and a silent empty stdout with exit 0 is indistinguishable from "this
//     recording said nothing" and "this tool did not work".
//
// So the ENGINE now answers with a typed error that carries the vendor's own
// verdict (the code, the message, the logid), the narrow seam that the mode and
// the HTTP server share translates it into the empty transcript they want, and
// the CLI's file shapes — the two that PRINT a transcript for a human — refuse
// it with a non-zero exit status and a sentence naming what happened.
//
// ZERO NEW DEPENDENCIES (Rule 35): a struct, one method set, one printer.

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	// voiceNoTranscriptVerdictFormat is the sentence for a refusal the SERVICE
	// explained. The vendor's own rendering is appended verbatim — the status
	// code, its message and the logid are the three things its support asks
	// for, and a refusal that dropped them would start every incident from
	// zero (Rule 15: no silent failure).
	voiceNoTranscriptVerdictFormat = "%s engine: this recording produced no transcript — the service's own verdict: %v"

	// voiceNoTranscriptNoVerdictFormat is the same refusal for a job that
	// completed with an empty result and NO verdict saying why (the local
	// runtime's `/sil` marker, or a 20000000 answer whose text is empty). It
	// says which of the two silences this is, because "no code" and "the code
	// said silence" are different facts (Rule 15).
	voiceNoTranscriptNoVerdictFormat = "%s engine: this recording produced no transcript — the job completed with an empty result and no service verdict saying why"

	// voiceNoTranscriptRefusalPrefix is the CLI's own line prefix, the same one
	// every other fatal in this verb prints, so an operator cannot tell a
	// refusal from a crash by its shape.
	voiceNoTranscriptRefusalPrefix = "gopi tool voice_input: "
)

// voiceNoTranscriptError is the engine's "no transcript" verdict, typed.
//
// It is NOT a failure of the run in the `failed` sense — the job completed —
// so it is deliberately a distinct type rather than an *APIError: a caller that
// wants the vendor's fields reaches them through Unwrap (errors.As, Rule 16),
// while a caller that only asks "did this produce text?" branches on the type
// and never has to guess from an empty string.
type voiceNoTranscriptError struct {
	// engine is voiceEngineLabel's spelling, so the refusal and the announcing
	// rows cannot name different engines (Rule 13/14).
	engine string
	// verdict is the vendor's typed error when the service said WHY (the
	// 20000003 silence verdict), and nil when the job simply carried no text.
	verdict error
}

// voiceNoTranscriptSentence renders the ONE sentence that says "this run produced
// no transcript", given the engine and (when the service explained itself) its
// verdict. Both readers go through it — the error's Error() method and the CLI's
// refusal — so a script matching the line and a human reading it cannot meet two
// different spellings of the same verdict.
func voiceNoTranscriptSentence(engine string, verdict error) string {
	if verdict != nil {
		return fmt.Sprintf(voiceNoTranscriptVerdictFormat, engine, verdict)
	}
	return fmt.Sprintf(voiceNoTranscriptNoVerdictFormat, engine)
}

// Error renders the refusal sentence a human reads.
func (e *voiceNoTranscriptError) Error() string {
	if e == nil {
		return ""
	}
	return voiceNoTranscriptSentence(e.engine, e.verdict)
}

// Unwrap keeps the vendor's verdict reachable (Rule 16), so a caller can ask
// `errors.As(err, &apiErr)` and read the status code, message and logid that
// produced this refusal.
func (e *voiceNoTranscriptError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.verdict
}

// voiceNoTranscriptVerdict builds the typed refusal around the service's own
// verdict — the shape the batch engine produces for X-Api-Status-Code 20000003.
func voiceNoTranscriptVerdict(engine string, verdict error) error {
	return &voiceNoTranscriptError{engine: engine, verdict: verdict}
}

// voiceAsNoTranscript reports whether err is this verdict, through whatever
// wrapping the engine arms added on the way (Rule 16).
func voiceAsNoTranscript(err error) (*voiceNoTranscriptError, bool) {
	var verdict *voiceNoTranscriptError
	if errors.As(err, &verdict) {
		return verdict, true
	}
	return nil, false
}

// voiceIsNoTranscript answers the ONLY question the call sites ask of the pair
// above — "is this the silence verdict?" — as a bare bool, so a caller that has
// no use for the typed value never discards an error (stylegate ignored-error,
// which counts a discarded error return even when it is a matched verdict).
func voiceIsNoTranscript(err error) bool {
	verdict, ok := voiceAsNoTranscript(err)
	return ok && verdict != nil
}

// voiceRefuseEmptyTranscript is the ONE place a CLI file shape refuses a run
// that produced no transcript: the sentence on stderr and a NON-ZERO exit
// status, in that order.
//
// `verdict` is the engine's typed answer when it had one (the 20000003 silence
// verdict travels here), and nil when the text was simply empty — the two are
// rendered as different sentences, because they are different facts and the
// operator's next step differs (retry with louder audio versus look for a
// broken engine).
//
// IT NEVER PRINTS TO STDOUT. stdout carries the transcript and nothing else;
// a refusal that put a sentence there would be consumed by the very `$(…)` this
// rule exists to protect.
func voiceRefuseEmptyTranscript(opts *voiceOptions, verdict error) {
	reason := ""
	if v, ok := voiceAsNoTranscript(verdict); ok {
		reason = v.Error()
	} else {
		reason = voiceNoTranscriptSentence(voiceEngineLabel(opts), nil)
	}
	fmt.Fprintf(os.Stderr, "%s%s\n", voiceNoTranscriptRefusalPrefix, strings.TrimSpace(reason))
	os.Exit(1)
}
