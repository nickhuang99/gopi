package main

// voice_live_engines.go — THE ENGINE SEAM of the live arm: one adapter per
// streaming channel, so the utterance's control flow (feeder, clock, wait,
// in-hand flush, finish key — voice_stream_runtime.go) is written ONCE and the
// two protocols differ only in what they are opened with and what they answer.
//
// WHY A SEAM RATHER THAN A SECOND RUNNER. The v2 channel (doubaostream) and the
// v3 SAUC channel (doubaosauc) do the same WORK with different bytes, and the
// package doc of the latter refuses one implementation behind two names. The
// runner above this seam is neither: it holds an OBLIGATION (nothing spoken may
// be dropped, and the first failure must be the one named) that has nothing to
// do with which envelope carries the audio. Copying it per engine would be two
// copies of that obligation — the drift voice_stream_runtime.go's own header
// records having already happened once, between three surfaces.
//
// WHAT EACH ADAPTER OWNS, and what it deliberately does not:
//
//	owns      the engine's options type, its Open, its result type, its partial
//	          type, and its two refusal sentinels — i.e. everything written in
//	          that engine's own vocabulary
//	not owns  the feeder, the clock, the end-of-utterance wait, the in-hand
//	          flush, the failure precedence, the finish key — all of which live
//	          above the seam and are shared
//
// NOTHING HERE IS A FALLBACK. A run whose engine has no live backend is refused
// BY NAME by voiceLiveEngineSelected (chat_voice_live.go) before a session is
// built, so the seam never has to guess which engine the operator meant.

import (
	"context"
	"errors"
	"fmt"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/doubaosauc"
	"github.com/nickhuang99/gopi/internal/doubaostream"
)

// voiceLiveSessionV2 adapts the v2 channel's session to the runner's vocabulary.
// The type assertion in Open's adapter is what keeps a wrong options value from
// reaching the engine as a zero value: an adapter that guessed would open a
// session with no credential and report the resulting 401 as the operator's
// mistake.
//
// Say / PartialCount / FinishTimedOut are PROMOTED from the embedded session —
// they mean the same thing in both vocabularies — and only Finish is translated,
// because its result type is the engine's own.
type voiceLiveSessionV2 struct{ *doubaostream.Session }

// Finish translates this engine's result into the runner's vocabulary.
func (s voiceLiveSessionV2) Finish(tail []byte) (*voiceLiveResult, error) {
	res, err := s.Session.Finish(tail)
	if res == nil {
		// The engine reports its own failure through err; a nil result means the
		// session never produced one, and inventing an empty result here would
		// hide that (Rule 15).
		return nil, err
	}
	return &voiceLiveResult{
		Text: res.Text, LogID: res.LogID, ReqID: res.ReqID,
		AudioMS: res.AudioMS, PartialCount: len(res.Partials),
		FirstPartialAt: res.FirstPartialAt, ServiceDurationMS: res.DurationMS,
		Elapsed: res.Elapsed,
	}, err
}

// voiceLiveSessionV3 adapts the v3 SAUC channel's session the same way.
type voiceLiveSessionV3 struct{ *doubaosauc.Session }

// Finish translates this engine's result into the runner's vocabulary.
func (s voiceLiveSessionV3) Finish(tail []byte) (*voiceLiveResult, error) {
	res, err := s.Session.Finish(tail)
	if res == nil {
		return nil, err
	}
	return &voiceLiveResult{
		Text: res.Text, LogID: res.LogID, ReqID: res.ReqID,
		AudioMS: res.AudioMS, PartialCount: s.Session.PartialCount(),
		FirstPartialAt: res.FirstTextAt, Elapsed: res.Elapsed,
	}, err
}

// voiceLiveBackendStreaming is the v2 channel's backend.
func voiceLiveBackendStreaming() voiceLiveBackend {
	return voiceLiveBackend{
		Open: func(ctx context.Context, opts any) (voiceLiveSession, error) {
			o, err := voiceLiveOptsV2(opts)
			if err != nil {
				return nil, err
			}
			sess, err := doubaostream.Open(ctx, o)
			if err != nil {
				return nil, err
			}
			return voiceLiveSessionV2{sess}, nil
		},
		// THE TWO SENTINELS THE FEEDER MUST NOT REPORT AS A BROKEN STREAM: a
		// packet composed in the instant the utterance was finalised is a race
		// the operator cannot see (see voiceLiveFeeder.absorb).
		IsRaceRefusal: func(err error) bool {
			return isOneOf(err, doubaostream.ErrSessionFinished, doubaostream.ErrSessionClosed)
		},
		ConfigurePartial: func(opts any, onPartial func(voiceLivePartial)) any {
			o, err := voiceLiveOptsV2(opts)
			if err != nil {
				// The SAME refusal Open would produce, produced earlier: a run
				// whose options are not this engine's is a programming error, and
				// leaving the options untouched here lets Open name it once.
				return opts
			}
			o.OnPartial = func(p doubaostream.Partial) {
				onPartial(voiceLivePartial{Index: p.Index, Text: p.Text, Definite: p.Definite, At: p.At})
			}
			return o
		},
	}
}

// voiceLiveBackendSauc is the v3 SAUC channel's backend.
func voiceLiveBackendSauc() voiceLiveBackend {
	return voiceLiveBackend{
		Open: func(ctx context.Context, opts any) (voiceLiveSession, error) {
			o, err := voiceLiveOptsV3(opts)
			if err != nil {
				return nil, err
			}
			sess, err := doubaosauc.Open(ctx, o)
			if err != nil {
				return nil, err
			}
			return voiceLiveSessionV3{sess}, nil
		},
		IsRaceRefusal: func(err error) bool {
			return isOneOf(err, doubaosauc.ErrSessionFinished, doubaosauc.ErrSessionClosed)
		},
		ConfigurePartial: func(opts any, onPartial func(voiceLivePartial)) any {
			o, err := voiceLiveOptsV3(opts)
			if err != nil {
				return opts
			}
			o.OnPartial = func(p doubaosauc.Partial) {
				onPartial(voiceLivePartial{Index: p.Index, Text: p.Text, Definite: p.Definite, At: p.At})
			}
			return o
		},
	}
}

// voiceLiveOptsV2 / voiceLiveOptsV3 are the two type assertions, written once so
// the Open and ConfigurePartial ladders of one engine cannot disagree about which
// type this engine's options are.
func voiceLiveOptsV2(opts any) (doubaostream.Options, error) {
	o, ok := opts.(doubaostream.Options)
	if !ok {
		return doubaostream.Options{}, fmt.Errorf(
			"the %s live arm was handed %T as its session options, want %T",
			config.AudioEngineStreaming, opts, doubaostream.Options{})
	}
	return o, nil
}

func voiceLiveOptsV3(opts any) (doubaosauc.Options, error) {
	o, ok := opts.(doubaosauc.Options)
	if !ok {
		return doubaosauc.Options{}, fmt.Errorf(
			"the %s live arm was handed %T as its session options, want %T",
			config.AudioEngineSauc, opts, doubaosauc.Options{})
	}
	return o, nil
}

// voiceLiveEngineFor resolves the ENGINE's backend and the session options it
// will be opened with, from the run's resolved engine — ONE function, so the
// mode's dispatch, the backend it runs and the options it carries cannot disagree
// about which engine a run is on (Rule 2).
//
// A RUN WITH NO LIVE BACKEND IS REFUSED BY NAME. That is unreachable through
// voiceLiveEngineSelected (which only selects engines that have one), and it is
// still stated rather than left to a nil call: the two functions are the two
// halves of one contract, and the half that ASSUMES is the one that lies when
// they drift (Rule 15).
func voiceLiveEngineFor(opts *voiceOptions) (voiceLiveBackend, any, error) {
	switch voiceResolveEngine(opts) {
	case config.AudioEngineStreaming:
		cred, _, err := voiceStreamCredentials(opts)
		if err != nil {
			return voiceLiveBackend{}, nil, err
		}
		// THE SAME ENDPOINT RESOLUTION THE CLI ARM USES (Rule 2): the live arm
		// used to take the package default silently, which meant a host whose
		// endpoint was configured for the file arm would still stream to the
		// published one — two arms of one engine disagreeing about where the
		// audio went.
		return voiceLiveBackendStreaming(), doubaostream.Options{
			AppID:    cred.AppID,
			Token:    cred.Token,
			Endpoint: voiceStreamEffectiveEndpoint(opts),
		}, nil
	case config.AudioEngineSauc:
		// THE CREDENTIAL'S SHAPE IS THE V3 CHANNEL'S WHOLE SUBJECT (see
		// cli_voice_sauc.go's header): the legacy console pair travels as the
		// APPID/TOKEN lines of the one credential document.
		saucOpts, path, err := voiceSaucLiveOpts(opts)
		if err != nil {
			return voiceLiveBackend{}, nil, fmt.Errorf("the %s engine needs a credential (%s): %w",
				config.AudioEngineSauc, path, err)
		}
		return voiceLiveBackendSauc(), saucOpts, nil
	default:
		return voiceLiveBackend{}, nil, fmt.Errorf(
			"engine %q has no live streaming arm (the live arm is %s or %s)",
			voiceResolveEngine(opts), config.AudioEngineStreaming, config.AudioEngineSauc)
	}
}

// voiceLiveResultFromV2 translates the v2 channel's result into the runner's
// vocabulary, for the ONE v2 shape that does not go through a live session at all:
// the one-shot Transcribe (cli_voice_stream.go), which answers a file. It is a
// named function rather than an inline literal so that shape's report keeps going
// through the SAME summary printer as every other run (Rule 2/13).
func voiceLiveResultFromV2(res *doubaostream.Result) *voiceLiveResult {
	if res == nil {
		return nil
	}
	return &voiceLiveResult{
		Text: res.Text, LogID: res.LogID, ReqID: res.ReqID,
		AudioMS: res.AudioMS, PartialCount: len(res.Partials),
		FirstPartialAt: res.FirstPartialAt, ServiceDurationMS: res.DurationMS,
		Elapsed: res.Elapsed,
	}
}

// voiceLivePartialV2 hands the runner's partial back to the v2 printer, which is
// the one a v2 run's own output has always gone through. It exists so the two
// tool arms' per-partial lines stay byte-identical to what they printed before
// the seam (task 01a13303's acceptance: the streaming path is unchanged), and it
// is lossless in both directions because voiceLivePartial carries what
// doubaostream.Partial carries.
func voiceLivePartialV2(p voiceLivePartial) doubaostream.Partial {
	return doubaostream.Partial{Index: p.Index, Text: p.Text, Definite: p.Definite, At: p.At}
}

// isOneOf reports whether err is any of the named sentinels. It is the shared
// body of the two backends' race classification, so the two engines differ only
// in WHICH sentinels they name.
func isOneOf(err error, sentinels ...error) bool {
	for _, s := range sentinels {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}
