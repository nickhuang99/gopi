package doubaosauc

// audio_level.go — HOW LOUD THE AUDIO IS, measured HERE, beside the client.
//
// WHY THE CLIENT MEASURES ITS OWN AUDIO. The channel has two ways to end with an
// empty transcript and they mean OPPOSITE things:
//
//   - the audio carried no speech, so "" is the CORRECT answer. The batch engine
//     (`--engine recording`) already answers this way: exit 0, empty transcript
//     (measured on the same silence.wav, 2026-11-15);
//   - the channel broke — a refused handshake, a peer that closed, a stream that
//     ended without a code — and "" is a LOSS.
//
// MEASURED (2026-11-15, this host): 4 s of digital silence given to `--engine
// recording` produced exit 0 and an empty transcript, while `--engine sauc`
// produced exit 1 and "the stream ended without a transcript and without a code
// saying why". An operator reading those two lines cannot tell "my microphone
// recorded nothing" from "the vendor channel is broken" — which is the defect
// this file removes: the level is a fact this process can measure for itself, so
// the verdict no longer has to be guessed from the service's silence.
//
// THE MEASUREMENT IS DELIBERATELY CONSERVATIVE. It fires only on audio whose PEAK
// is also below a floor — i.e. essentially digital silence — because the
// expensive direction of this judgement is the wrong one: telling an operator
// "there was no speech" about a quiet recording that DID carry speech sends them
// to re-record audio that was fine. A quiet-but-real recording peaks far above
// the floor below and is never called silent.

import (
	"encoding/binary"
	"math"
	"strconv"
)

// The two floors, in dBFS (decibels relative to full scale: 0 is clipping,
// negative numbers are quieter). A sample vector counts as carrying NO speech
// only when BOTH its RMS and its peak sit below their floors.
//
// WHY BOTH. RMS alone would call a recording silent when it holds a few loud
// transients in a long quiet stretch (a click, a cough at the far end of the
// file) — the mean is small, the audio is not empty. The peak alone would call a
// continuous low hum "speech". Requiring both is what makes the verdict survive
// the shapes a real capture produces.
//
// THE VALUES. `speechFloorPeakdBFS` is about 1/180 of full scale: quieter than any
// usable recording and far above the zero of digital silence. A capture of a
// person speaking at an unusually low gain peaks many dB above it.
const (
	speechFloorRMSdBFS  = -55.0
	speechFloorPeakdBFS = -45.0

	// fullScaleSample is the magnitude 16-bit PCM is measured against: the
	// negative full scale (one more than the positive maximum), so the ratio
	// never exceeds 1.
	fullScaleSample = 32768.0

	// dBPerBel converts a magnitude ratio to decibels.
	dBPerBel = 20.0

	// silentLevelText is how a mathematically silent vector is printed: the
	// logarithm of zero is negative infinity, and "-Inf dBFS" reads as a bug
	// rather than as the measurement it is.
	silentLevelText = "digital silence"

	// levelDecimals is how many decimals a dBFS figure is printed with. One is
	// the resolution a level measurement deserves; more would claim precision the
	// floors above do not have.
	levelDecimals = 1
)

// audioLevel is what the client measured about the audio it is about to send, or
// about the audio it has already sent. It is the EVIDENCE behind a "no speech"
// verdict, carried to the operator instead of being reduced to a boolean.
type audioLevel struct {
	// RMSdBFS / PeakdBFS are the two figures the verdict is made of.
	RMSdBFS  float64
	PeakdBFS float64
	// AudioMS is how much audio was measured, so the operator can see the
	// verdict is about the whole input and not a prefix of it.
	AudioMS int
	// Measured distinguishes "this audio was measured and is silent" from
	// "nothing was measured": the zero value must never claim silence
	// (Rule 15 — an unmeasured quantity is not a finding).
	Measured bool
}

// measureAudioLevel folds 16 kHz mono PCM16 samples into the two figures. It is
// the FILE arm's measurement, taken from the decoder's output before the socket
// opens.
func measureAudioLevel(samples []byte) audioLevel {
	var acc levelAccumulator
	acc.add(samples)
	return acc.level()
}

// levelAccumulator folds PCM into a level INCREMENTALLY, which is what the LIVE
// arm needs: its audio arrives one capture packet at a time, and it has no whole
// file to measure at the end.
type levelAccumulator struct {
	sumSquares float64
	peak       float64
	frames     int
}

// add folds one packet of 16 kHz mono PCM16 into the accumulator.
func (a *levelAccumulator) add(pcm []byte) {
	frames := len(pcm) / bytesPerSample16
	for i := 0; i < frames; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[i*bytesPerSample16:])))
		if v < 0 {
			v = -v
		}
		if v > a.peak {
			a.peak = v
		}
		a.sumSquares += v * v
	}
	a.frames += frames
}

// level renders what was folded so far. An accumulator that folded nothing
// reports Measured=false — never silence.
func (a *levelAccumulator) level() audioLevel {
	if a.frames == 0 {
		return audioLevel{}
	}
	rms := math.Sqrt(a.sumSquares / float64(a.frames))
	return audioLevel{
		RMSdBFS:  ratioToDBFS(rms),
		PeakdBFS: ratioToDBFS(a.peak),
		AudioMS:  a.frames * milliPerSecond / audioSampleRate,
		Measured: true,
	}
}

// ratioToDBFS converts a sample magnitude to dBFS. A zero magnitude is negative
// infinity, which is the honest value and is rendered as silence by levelText.
func ratioToDBFS(magnitude float64) float64 {
	if magnitude <= 0 {
		return math.Inf(-1)
	}
	return dBPerBel * math.Log10(magnitude/fullScaleSample)
}

// CarriesSpeech reports whether this audio is loud enough to hold speech. An
// UNMEASURED level answers true: "I did not measure it" must never be turned into
// "there was nothing to hear" (Rule 15).
func (l audioLevel) CarriesSpeech() bool {
	if !l.Measured {
		return true
	}
	return l.RMSdBFS > speechFloorRMSdBFS || l.PeakdBFS > speechFloorPeakdBFS
}

// Evidence renders the measurement as the sentence a verdict carries. It is the
// BASIS the epic asks for: a "no speech" claim names the figures it was made from
// (threshold AND energy), so a reader can check it instead of trusting it.
func (l audioLevel) Evidence() string {
	if !l.Measured {
		return "the audio was not measured"
	}
	return "measured RMS " + l.levelText(l.RMSdBFS) + ", peak " + l.levelText(l.PeakdBFS) +
		" over " + strconv.Itoa(l.AudioMS) + " ms of audio (speech is above peak " +
		l.levelText(speechFloorPeakdBFS) + " or RMS " + l.levelText(speechFloorRMSdBFS) + ")"
}

// levelText prints one dBFS figure, naming the silence case rather than rendering
// a logarithm of zero.
func (l audioLevel) levelText(dbfs float64) string {
	if math.IsInf(dbfs, -1) {
		return silentLevelText
	}
	return strconv.FormatFloat(dbfs, 'f', levelDecimals, 64) + " dBFS"
}
