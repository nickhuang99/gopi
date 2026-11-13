package main

// chat_voice_audio.go — WHICH AUDIO `/voice` READS (task 01a11f13's second child).
//
// THE ONE LINE THIS FILE EXISTS TO CROSS. cli_voice_audio_source.go put the
// capture behind a seam (voiceAudioSource), so "where the audio comes from" is a
// choice rather than a fact about the machine — but BOTH of the `/voice` mode's
// capture points still spelled that choice away:
//
//	chat_voice_recorder_staged.go  openVoiceStagedStream(nil, &sel.Device)   // window arm
//	chat_voice_live.go             openVoiceStagedStream(nil, r.dev)         // live arm
//
// The `nil` is the options the source is chosen from, and `nil` means "the
// production recorder on a device". On a host with no capture device there is no
// device to resolve, so the whole mode — the window plan, the boundary rules, the
// line, the status row — could not be exercised at all. This file gives the chat
// surface a CHANNEL for the same choice the tool surface already has
// (`gopi tool voice_input --audio-source FILE`), so one audio file drives the
// chat path end to end without a microphone and without a sound card.
//
// WHAT IT IS NOT, and this is the whole reason it is a channel rather than a
// second implementation: nothing here decodes, paces, validates or closes
// anything. The bytes come from voiceAudioSourceFor → newVoiceFileSource (Rule
// 14: one seam, one geometry, one refusal vocabulary) and the chat surface only
// decides WHICH SOURCE the mode is built over. A second file reader here would be
// exactly the drift the seam was introduced to make impossible.
//
// THE DEVICE, WHEN THE AUDIO IS A FILE. The mode's own vocabulary is built on a
// device: the entered row names the capture it records from, and the recorder
// hands the device to the two capture points. A file run chooses no device, so
// the device it carries is a STATEMENT rather than a handle — the two capture
// points pass it through and voiceAudioSourceFor never looks at it, because a
// named file wins before the device is consulted. It is still non-nil
// deliberately: a nil device is the mode's own "no capture at all" refusal, and
// this path has a capture — a file.

import (
	"fmt"
	"os"
	"strings"
)

const (
	// chatVoiceAudioFlag is the chat surface's spelling of the audio-source
	// choice. It takes a value, so it also joins the verb's valued-flag list
	// (chatResumeValuedFlags) and its accepted vocabulary (chatREPLFlags) — the
	// closed-vocabulary walk refuses a word that is in neither.
	chatVoiceAudioFlag = "--voice-audio"

	// chatVoiceAudioEnv is the ENVIRONMENT channel of the same choice, and it is
	// the reason the flag is not the only way in: the chat verb is reachable from
	// scripts and from the film/replay drivers, and a capability that only a flag
	// can reach is one a driver cannot arm. The precedence is the flag's
	// (GOPI_CHAT_DUMP's own rule): a value stated on the command line is the
	// operator's current statement and wins over an exported variable.
	chatVoiceAudioEnv = "GOPI_CHAT_VOICE_AUDIO"

	// chatVoiceAudioDeviceID is the ID of the device a FILE run carries — the
	// stable handle spelled the way every other device ID is
	// ("<backend>:<name>"), so a message that prints a device ID prints one that
	// looks like a device ID. It is never handed to a recorder process: a named
	// file wins in voiceAudioSourceFor before the device is consulted.
	chatVoiceAudioDeviceID = "file"

	// chatVoiceFileCaptureNote is the row's sentence for a file capture. It says
	// the file, the geometry that is required of it, and the fact that no device
	// was opened — the three things an operator needs to read that row correctly
	// (a run that said "recording 6s from file" would send them looking for a
	// microphone).
	chatVoiceFileCaptureNote = "the file %s (%s — no capture device is opened)"
)

// chatVoiceAudioPath resolves the audio source this run was given: the flag
// when it was stated, the environment otherwise, and "" when neither spoke (the
// production capture, which is what every run before this channel did).
//
// IT IS THE ONE READER of the two channels. The flag's value is already parsed
// and validated by the argv walk; this function answers only "which of the two
// spellings this run means", so a second call site cannot invent a third
// precedence (Rule 2).
func chatVoiceAudioPath(flagPath string) string {
	if p := strings.TrimSpace(flagPath); p != "" {
		return p
	}
	return strings.TrimSpace(os.Getenv(chatVoiceAudioEnv))
}

// chatParseVoiceAudioArg consumes `--voice-audio FILE` in both spellings, and is
// chatParseOutArg's twin for the same reason that one is a function: the argv
// walk must consume the value in the NEXT slot, which a ranged loop cannot
// express.
//
// A BARE FLAG IS A USAGE ERROR, never "the default": a run that silently fell
// back to the microphone on a host that has none would look like a broken
// capture rather than a mistyped command line (Rule 15).
func chatParseVoiceAudioArg(a, next string, hasNext bool) (path string, consumed bool, err error) {
	if a != chatVoiceAudioFlag && !strings.HasPrefix(a, chatVoiceAudioFlag+chatFlagValueSep) {
		return "", false, nil
	}
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		inline := a[k+1:]
		if inline == "" {
			return "", true, fmt.Errorf("%s needs a file — write %s FILE (16kHz mono s16le, or a WAV of the same geometry)",
				chatVoiceAudioFlag, chatVoiceAudioFlag)
		}
		return inline, true, nil
	}
	if !hasNext || strings.HasPrefix(next, chatFlagPrefix) {
		return "", true, fmt.Errorf("%s needs a file — write %s FILE (16kHz mono s16le, or a WAV of the same geometry)",
			chatVoiceAudioFlag, chatVoiceAudioFlag)
	}
	return next, true, nil
}

// chatVoiceFileDevice describes the "device" a file run carries: the statement
// the entered row prints, and the value the two capture points pass through.
//
// THE LABEL IS THE PATH, and the ID says what it is. It is built here rather than
// at each call site so the window arm and the live arm cannot describe the same
// run differently.
func chatVoiceFileDevice(path string) (voiceDevice, string) {
	dev := voiceDevice{
		ID:      chatVoiceAudioDeviceID,
		Backend: chatVoiceAudioDeviceID,
		Name:    path,
		Label:   path,
	}
	return dev, fmt.Sprintf(chatVoiceFileCaptureNote, path, voiceCaptureGeometryText())
}

// chatVoiceAudioFilmRefused is the FILM arm's refusal. A film is a DRIVER: it
// paints nothing itself and runs a second copy of this binary inside a private
// terminal, and the argv it hands that copy is built by the driver — so a flag
// THIS process read simply is not there, and accepting it would arm a capture no
// process ever reads (the silent acceptance Rule 15 forbids). It is
// chatFilmDumpRefusal's shape for the same reason, and the ENVIRONMENT is the
// route that does work, because the producer inherits it.
const chatVoiceAudioFilmRefused = "%s names the audio `/voice` reads, and %s films a session: this process runs no chat surface of its own — a second gopi inside a private terminal is the one that would read it — so a flag cannot reach that process (the filmed run would capture from the microphone). To drive the FILMED run's `/voice`, set %s=FILE in the environment (the producer inherits it); or drop %s"

// chatVoiceAudioOnceRefused is the ONE-SHOT arm's refusal: `--once` runs a single
// turn with no prompt loop and no editor, so the mode the word configures cannot
// exist in it. The sentence names the shape that HAS the mode, because a refusal
// a caller cannot act on is only half an answer.
const chatVoiceAudioOnceRefused = "%s %s names the audio `/voice` reads, and this run is one-shot (`%s`): the voice mode is a REPL command, so it exists only in an interactive chat"

// chatVoiceAudioRefusal answers the two command lines that were GIVEN an audio
// source and have no `/voice` mode to read it: a film run (a driver) and a
// one-shot run (no loop). It is ONE function with two call sites — the reason the
// two shapes cannot drift into two different sentences for one mistake — and it
// returns nil when there is nothing to refuse.
func chatVoiceAudioRefusal(audioPath string, film, oneShot bool) error {
	if strings.TrimSpace(audioPath) == "" {
		return nil
	}
	if film {
		return fmt.Errorf(chatVoiceAudioFilmRefused, chatVoiceAudioFlag, chatFlagFilm, chatVoiceAudioEnv, chatVoiceAudioFlag)
	}
	if oneShot {
		return fmt.Errorf(chatVoiceAudioOnceRefused, chatVoiceAudioFlag, audioPath, chatOnceFlag)
	}
	return nil
}

// chatVoiceRecorder builds the `/voice` recorder for this run: the production
// staged recorder, carrying the audio source the run was given.
//
// ONE CONSTRUCTION SITE, so "which recorder the mode gets" and "which audio it
// reads" cannot be answered in two places. A run that named no source gets the
// zero value of the field, which is the production capture.
func chatVoiceRecorder(audioPath string) *voiceChatRecorderStaged {
	return &voiceChatRecorderStaged{audioSource: audioPath}
}
