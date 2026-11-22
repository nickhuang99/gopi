package main

// chat_sgr_stream.go — `gopi chat`'s RAW byte-stream switch.
//
// WHAT IT ARMS. uiclient.SgrStream (internal/uiclient/sgrstream.go) appends every
// byte the render stream writes to `<session>/<sid>.sgr.stream`, verbatim and
// length-prefixed. It is the artifact a render REGRESSION or a REPLAY question
// needs, and it is deliberately not the screen dump: chat-screen.dump is this
// program's INTERPRETATION of the bytes (throttled to dumpMinInterval, reduced to
// the cells the grid model expresses), so it cannot tell a reader whether a
// wrong cell came from the renderer or from the interpreter.
//
// THE SWITCH IS OFF BY DEFAULT, and the OFF state costs the render path nothing
// at all: DumpConfig.Sgr stays nil, and uiclient.ScreenDumper.Write checks the
// field before it does anything (one branch, no allocation, no syscall). The
// byte-identity of an unarmed run — dump included — is asserted by
// uiclient's own tests rather than by review.
//
// PRECEDENCE (CONFIG.md §1: flag > env > settings key > default): this capability
// has no settings key, so the chain is flag > GOPI_SCREEN_STREAM > off. The flag
// is `--sgr-stream[=PATH]`; the env var follows the dump's own shape (`1`/truthy =
// on at the resolved path, any other value IS the path).
//
// WHERE IT GOES. The same resolution SHAPE chatResolveDumpPath uses (Rule 13: one
// shape, one meaning): the explicit path wins; otherwise the run's OWN session
// directory, named after the session so a reader of the file knows whose bytes
// these are; otherwise the chat's own scratch class (Rule 22 — never /tmp). A
// REPLAY is redirected out of the recording for the same reason the dump is: the
// recording is what a replay READS, and writing into it would make the verb's own
// promise ("it writes nothing at all") false while staying invisible until
// somebody hashed the recording.

import (
	"io"

	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/replaymode"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/uiclient"
	"github.com/nickhuang99/gopi/internal/version"
)

// The switch's own vocabulary (Rule 1b/13): the parser, the help page, the
// notices and the tests all reference these, so a second spelling of the flag
// cannot appear.
const (
	// chatSgrStreamFlag is `--screen-stream[=PATH]`. Joined form only, for the
	// same reason `--dump-screen` is: this verb's remaining positional argument
	// is a session seed, so a separated path would be read as "continue that
	// session" (a flag that quietly did something else).
	//
	// THE LOCAL NAME STILL SAYS `Sgr` ON PURPOSE, and it is not a second
	// definition (`chatSgrStreamFlag = cliverbs.ScreenStream` — the VALUE has one
	// definition, Rule 13). The rename this package underwent is the FLAG's, not
	// the recorder's: the sink type this file arms is `uiclient.SgrStream`, the
	// digest function is `uiclient.Shake256SgrStream`, and the ~30 call sites that
	// speak about the RECORDING still read as `sgr`. Renaming the identifier
	// without the type it names would make the file lie twice; the flag string,
	// which the caller types, is what carries the user-visible name.
	chatSgrStreamFlag = cliverbs.ScreenStream
	// chatSgrStreamDeprecatedFlag / chatNoSgrStreamDeprecatedFlag are the
	// PRE-RENAME SPELLINGS. They still PARSE (see parseChatSgrArg) and each one
	// prints cliverbs.DeprecatedFlagNoticeFormat, so a shell history from before
	// the rename keeps working while telling its owner the current word.
	chatSgrStreamDeprecatedFlag   = cliverbs.SgrStream
	chatNoSgrStreamDeprecatedFlag = cliverbs.NoSgrStream
	// chatNoSgrStreamFlag is the OFF SWITCH `--no-screen-stream`: the stream is
	// recorded by DEFAULT (operator ruling: 数据的不可再生性高于代码完整性), so
	// the default needs a documented way out. It lives in internal/cliverbs
	// beside DumpScreen/NoDumpScreen (Rule 13/14 — a default's two directions are
	// one capability's constants, and this one takes NO value), exactly as
	// chatNoDumpFlag does.
	chatNoSgrStreamFlag = cliverbs.NoScreenStream
	// chatSgrStreamOffNotice announces a DECLINED stream before the terminal is
	// taken. The switch is armed by DEFAULT, so silence would be ambiguous: a
	// caller who typed the off switch (or exported a falsey value) must be able
	// to tell "declined on purpose" from "the default silently failed to arm"
	// (Rule 15 — the off state is never a silent one). It is the sgr leg's
	// counterpart to the dump's own `screen dump off — …` banner.
	chatSgrStreamOffNotice = "gopi chat: raw screen byte stream OFF (%s) — no %s artifact will be written for this run\n"
	// chatSgrStreamOffReportFormat is the closing record of a DECLINED stream:
	// the same `#gopi-chat-sgr-stream-report` marker as the armed one, carrying
	// the DECISION instead of an artifact's numbers, so a reader scanning the
	// tail of a run learns which state it ended in without walking the session
	// directory to prove a file is not there (Rule 15).
	chatSgrStreamOffReportFormat = "#gopi-chat-sgr-stream-report decision=%s\n"
	// chatSgrStreamDecisionOff is the `decision=` word of a declined run.
	chatSgrStreamDecisionOff = "off"
	// chatSgrStreamEnv is the environment channel, and it now ALIASES the one
	// constant in internal/cliverbs (Rule 14): the recording is no longer a
	// chat-only capability — a worker run's output is worth recording too — so the
	// name left the chat flag namespace and lives where every surface can name it.
	// The old spelling is still READ through cliverbs.LegacyScreenStreamEnv, and
	// the run that finds it says so (see chatSgrEnvDecidedBy).
	chatSgrStreamEnv = cliverbs.ScreenStreamEnv
	// chatSgrStreamNotice announces the armed artifact BEFORE the layout takes
	// the terminal, so a file nobody can find is impossible.
	chatSgrStreamNotice = "gopi chat: raw screen byte stream → %s (verbatim render bytes, length-prefixed per write — no interpretation; replay it with any interpreter)\n"
	// chatSgrStreamReportFormat is the closing one-line record, printed beside
	// the dump's own report: what the raw leg produced, and its last error.
	chatSgrStreamReportFormat = "#gopi-chat-sgr-stream-report path=%s bytes=%d frames=%d error=%q\n"
	// chatSgrStreamDirPattern names the scratch directory the resolver creates
	// when no explicit path was given and the run has no session directory. It
	// is distinct from the dump's pattern so the two artifacts of one session
	// never share a directory name.
	chatSgrStreamDirPattern = "chat-sgr-stream"
)

// chatSgrOptions is the resolved request, parsed from the flag and the env
// (flag wins). It mirrors chatDumpOptions field for field, deliberately: the two
// artifacts are one capability family (`--dump-screen[=PATH]|--no-dump-screen`
// and `--sgr-stream[=PATH]|--no-sgr-stream`), and "off" and "on, path unknown"
// must not collapse into one empty string in either of them.
type chatSgrOptions struct {
	// path is the explicit destination; "" means the resolver chooses one.
	path string
	// enabled records the EFFECTIVE decision to record — the default below, the
	// flag, or the environment. It is chatDumpOptions.requested: the resolver
	// arms unless disabled is set, so `--no-sgr-stream` is one predicate away
	// rather than a second spelling of "nothing was said".
	enabled bool
	// disabled records that the run was told to turn the stream OFF
	// (`--no-sgr-stream`, or a falsey GOPI_SCREEN_STREAM). It is the ONE state
	// in which no artifact is written.
	disabled bool
	// stated records that the stream was named EXPLICITLY on this run's command
	// line or environment. The resume chain keys on it (chatApplyResumeFlags'
	// sgrStated): the DEFAULT is not a statement, so it must not supersede a
	// destination a resumed session recorded.
	stated bool
}

// recorded is the ONE predicate the resolver and the arm block read, so "off" has
// a single definition (Rule 13) instead of two spellings that can drift.
func (o chatSgrOptions) recorded() bool { return o.enabled && !o.disabled }

// chatSgrDefault is this run's decision when nobody said anything: the stream IS
// recorded. It is a function (not a literal spread through the parse) so the one
// place that answers "what happens by default?" is here — the same shape
// chatDumpDefault has, for the same reason.
//
// WHY THE DEFAULT IS ON (operator ruling, 2026-10-31: 数据的不可再生性高于代码
// 完整性): the bytes of a paint sequence cannot be re-created afterwards — a run
// that did not record has nothing to replay, while the cost of recording is one
// file beside the dump. The paired defect on the READ side (`grid=`,
// 01a1a654-cdce) does not block this: a recorded stream that today's reader
// mis-partitions is still data a FIXED reader will read, and the converse is not
// true.
func chatSgrDefault() chatSgrOptions {
	return chatSgrOptions{enabled: true}
}

// chatSgrFromEnv reads the environment channel THROUGH the one shared parser
// (internal/config/screenstream.go), so the chat no longer owns a second copy of
// the three-state rule (Rule 13/14). A run with NO GOPI_SCREEN_STREAM gets the
// chat's surface default (recorded, path resolved by the resolver); a value
// config.IsFalseValue accepts TURNS IT OFF; `GOPI_SCREEN_STREAM=1` (any truthy
// spelling) asks for the stream at the resolver's chosen path; any OTHER non-empty
// value IS the path. The precedence itself is config.ScreenStreamResolve's, which
// is the ONE statement of it for every surface (argv > env > surface default).
//
// Precedence: the flag wins over this (the caller overwrites the whole struct).
func chatSgrFromEnv() chatSgrOptions {
	env := config.ScreenStreamFromEnv()
	return chatSgrOptionsFromChoice(config.ScreenStreamResolve(config.ScreenStreamChoice{}, env, config.ScreenStreamDefaultChat))
}

// chatSgrOptionsFromChoice is the ONE translation from a resolved
// config.ScreenStreamChoice into this package's request struct, so the three
// states cannot be re-partitioned at a second call site.
func chatSgrOptionsFromChoice(c config.ScreenStreamChoice) chatSgrOptions {
	stated := c.Origin == config.ScreenStreamOriginEnv || c.Origin == config.ScreenStreamOriginArgv
	switch c.State {
	case config.ScreenStreamStateOff:
		return chatSgrOptions{disabled: true, stated: stated}
	case config.ScreenStreamStatePath:
		return chatSgrOptions{path: c.Path, enabled: true, stated: stated}
	case config.ScreenStreamStateOn:
		return chatSgrOptions{enabled: true, stated: stated}
	}
	// Unset can only arrive from a caller that bypassed the resolver; the chat's
	// default is the honest answer, and `stated` stays false (a default is not a
	// statement — chatApplyResumeFlags keys on that).
	return chatSgrDefault()
}

// chatSgrEnvSource answers WHICH spelling decided this run and its value, and it
// defers to the shared reader (config.ScreenStreamFromEnv) rather than calling
// os.Getenv itself — TWO readers of one variable is how the current name and the
// alias start disagreeing (Rule 2). The legacy name travels as a `Source`, not as
// a silent fallback: chatSgrEnvAliasWarn prints it, so a shell profile written
// before the rename is visible rather than merely tolerated (Rule 15).
func chatSgrEnvSource() (name, value string) {
	c := config.ScreenStreamFromEnv()
	if c.State == config.ScreenStreamStateUnset {
		return "", ""
	}
	if c.State == config.ScreenStreamStatePath {
		return c.Source, c.Path
	}
	return c.Source, c.State
}

// chatSgrEnvAliasNotice is printed ONCE when the recording was switched by the
// pre-rename spelling. It names both, so the fix is copy-pasteable.
const chatSgrEnvAliasNotice = "gopi chat: the screen-stream switch was read from %s, which is the PRE-RENAME spelling of %s (still honoured) — export %s to stay current\n"

// parseChatSgrArg handles ONE argv element in the flag's vocabulary and reports
// whether it was a raw-stream word — the ARM flag (`--sgr-stream`,
// `--sgr-stream=PATH`) or the OFF switch (`--no-sgr-stream`).
//
// The OFF switch takes NO value: it names the absence of an artifact, so
// `--no-sgr-stream=x` is refused BY NAME rather than silently read as "off"
// (Rule 15 — a flag that quietly discards its own value teaches a caller
// something false about the command line). This is chatDumpArg's shape exactly,
// down to the joined-form refusal naming the flag that DOES take a destination.
//
// The separated form of the ARM flag is refused BY NAME (a path here would be
// read as a session seed), and the refusal names the joined form that works.
//
// BOTH WORDS ON ONE COMMAND LINE: the walk applies them in argv order, so the
// LAST one wins (`--no-sgr-stream --sgr-stream` records, and the reverse does
// not). It is not silent: the run announces the state it ended in, and a resume
// records the winning word. Last-wins is chosen over a refusal because the two
// spellings are the two directions of one switch — `export GOPI_SCREEN_STREAM=0`
// in a shell profile and `--sgr-stream` on one command line is the ordinary way
// a caller re-arms it, and refusing the pair would make the profile an obstacle.
func parseChatSgrArg(a string, next string, hasNext bool) (opts chatSgrOptions, matched bool, err error) {
	return parseChatSgrArgWithNotice(a, next, hasNext, nil)
}

// parseChatSgrArgWithNotice is parseChatSgrArg with the DEPRECATION line routed
// to `warn` — the seam the parser needs so that a pre-rename spelling can still
// be honoured while saying so. A nil `warn` (the silent reader: a recorded resume
// option, the known-word probe) parses exactly the same and prints nothing,
// which is the point: the NOTICE is about what the operator TYPED, and a resume
// option is not typed at this moment.
//
// The two deprecated words are matched BEFORE their current counterparts and
// carry their own arm only so the notice can name the pair; the resulting
// request is byte-identical (Rule 13 — one behaviour, two spellings).
func parseChatSgrArgWithNotice(a string, next string, hasNext bool, warn io.Writer) (opts chatSgrOptions, matched bool, err error) {
	// DEPRECATED SPELLINGS FIRST: they must reach the SAME arms below, so the
	// translation happens here, once, and the rest of the function never learns
	// that a second spelling exists.
	for _, alias := range [...]struct{ from, to string }{
		{chatNoSgrStreamDeprecatedFlag, chatNoSgrStreamFlag},
		{chatSgrStreamDeprecatedFlag, chatSgrStreamFlag},
	} {
		if a == alias.from || strings.HasPrefix(a, alias.from+chatFlagValueSep) {
			if warn != nil {
				fmt.Fprintf(warn, cliverbs.DeprecatedFlagNoticeFormat, alias.from, alias.to)
			}
			a = alias.to + strings.TrimPrefix(a, alias.from)
			break
		}
	}
	if a == chatNoSgrStreamFlag {
		if hasNext && next != "" && !strings.HasPrefix(next, chatFlagPrefix) {
			return chatSgrOptions{}, true, fmt.Errorf(
				"%s takes no value (it turns the raw screen stream OFF); write %s alone, or %s=%s to choose a destination",
				chatNoSgrStreamFlag, chatNoSgrStreamFlag, chatSgrStreamFlag, next)
		}
		return chatSgrOptions{disabled: true, stated: true}, true, nil
	}
	if strings.HasPrefix(a, chatNoSgrStreamFlag+chatFlagValueSep) {
		// The joined form is refused TOO, rather than falling through to the
		// generic unknown-flag message: `--no-sgr-stream=x` is a caller who
		// believes the off switch takes a value, and the honest answer names the
		// flag that does (Rule 15 — "unknown flag" would send them looking for the
		// right spelling in a list that already contains it).
		return chatSgrOptions{}, true, fmt.Errorf(
			"%s takes no value (it turns the raw screen stream OFF); write %s alone, or %s=%s to choose a destination",
			chatNoSgrStreamFlag, chatNoSgrStreamFlag, chatSgrStreamFlag, strings.TrimPrefix(a, chatNoSgrStreamFlag+chatFlagValueSep))
	}
	if a != chatSgrStreamFlag && !strings.HasPrefix(a, chatSgrStreamFlag+chatFlagValueSep) {
		return chatSgrOptions{}, false, nil
	}
	inline := ""
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		inline = a[k+1:]
	}
	if inline == "" && hasNext && !strings.HasPrefix(next, chatFlagPrefix) {
		return chatSgrOptions{}, true, fmt.Errorf(
			"%s takes its path joined — write %s=%s (or the bare %s for the stream beside this run's own session artifacts)",
			chatSgrStreamFlag, chatSgrStreamFlag, next, chatSgrStreamFlag)
	}
	return chatSgrOptions{path: inline, enabled: true, stated: true}, true, nil
}

// chatResolveSgrPath decides where the raw stream goes. Precedence and failure
// behaviour are chatResolveDumpPath's (Rule 13): explicit path, then the run's
// own session directory, then the chat's scratch class; a failure is an error,
// never a silent decline (Rule 15 — a run that believes it is recording and is
// not is the worst of both states).
func chatResolveSgrPath(opts chatSgrOptions, ag *agent.Agent) (string, error) {
	if !opts.recorded() {
		return "", nil
	}
	if opts.path != "" {
		// A REPLAY still honours the operator's explicit choice — the redirect
		// below exists to protect the recording from a path this run DERIVED,
		// not to override a path a human typed.
		if dir := filepath.Dir(opts.path); dir != "" && dir != "." {
			if err := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, dumpDirPerm); err != nil {
				return "", fmt.Errorf("cannot create the SGR stream directory %s: %w", dir, err)
			}
		}
		return opts.path, nil
	}
	if replaymode.Active() {
		return chatReplaySgrPath()
	}
	if ag != nil && ag.SessionLog != nil && ag.SessionLog.Path() != "" {
		// The SESSION-DIRECTORY branch, with the session in the NAME: two runs
		// that share a container directory (the flat layout) still produce two
		// distinguishable streams.
		rec := ag.SessionLog.Path()
		dir := sessionlayout.RecordContainer(rec)
		return filepath.Join(dir, sessionlayout.SessionID(rec)+uiclient.ScreenStreamSuffix), nil
	}
	dir, err := scratch.MkdirTemp(scratch.RuntimeClassUISession, chatSgrStreamDirPattern)
	if err != nil {
		return "", fmt.Errorf("no destination is available for the raw screen stream: %w", err)
	}
	return filepath.Join(dir, uiclient.ChatDumpFileName+uiclient.ScreenStreamSuffix), nil
}

// chatReplaySgrPath is the ONE destination a replay's raw stream may have: the
// chat's own scratch class, never the recording's session directory (the same
// contract chatReplayDumpPath keeps, and for the same measured reason).
func chatReplaySgrPath() (string, error) {
	dir, err := scratch.MkdirTemp(scratch.RuntimeClassUISession, chatSgrStreamDirPattern)
	if err != nil {
		return "", fmt.Errorf("a replay's raw screen stream needs a scratch destination, and none could be created (it is never written into the recording's session directory): %w", err)
	}
	return filepath.Join(dir, uiclient.ChatDumpFileName+uiclient.ScreenStreamSuffix), nil
}

// chatOpenSgrStream opens the artifact, or returns (nil, nil) when the switch is
// off — the ONE place the switch's off state becomes a nil stream.
func chatOpenSgrStream(path string, cols, rows int) (*uiclient.SgrStream, error) {
	if path == "" {
		return nil, nil
	}
	s, err := uiclient.OpenSgrStream(uiclient.SgrStreamConfig{
		Path:     path,
		Cols:     cols,
		Rows:     rows,
		Revision: version.Revision,
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// chatReportSgrStream prints what the raw leg recorded, AFTER the terminal is
// the operator's again. The OFF state prints its OWN line rather than nothing:
// the switch is armed by default, so a caller who declined it must be able to
// read that decision back — "no report" would be indistinguishable from "the run
// died before it could report" (Rule 15, the same reason chatSgrStreamOffNotice
// exists at the front).
func chatReportSgrStream(s *uiclient.SgrStream, opts chatSgrOptions) {
	if s == nil {
		if !opts.recorded() {
			fmt.Fprintf(os.Stderr, chatSgrStreamOffReportFormat, chatSgrStreamDecisionOff)
		}
		return
	}
	fmt.Fprintf(os.Stderr, chatSgrStreamReportFormat, s.Path(), s.Bytes(), s.Frames(), s.Err())
}

// chatFilmSgrRefused is the flag's refusal on a film command line. It is the
// RAW leg's counterpart to chatFilmDumpRefused, and it says the same two things:
// why the word cannot work here, and which channel does.
const chatFilmSgrRefused = "%s names an artifact for a screen THIS process does not paint: a film is a driver, and the screen is painted by a second gopi inside the private terminal — a flag cannot reach that process, so accepting it would write nothing and say nothing. Set %s=PATH in the environment instead (the producer inherits it) and read the path the filmed run prints"

// chatFilmNoSgrRefused is the OFF SWITCH's refusal on a film command line: the
// filmed process is where a stream would be written, and this command line's
// decline cannot reach it — so the filmed run would keep recording. The stream is
// armed by DEFAULT, which makes this the arm a film caller is most likely to
// reach for and the one it must be told about by name.
const chatFilmNoSgrRefused = "%s films a session and %s would decline an artifact the FILMED run writes — this process paints no screen of its own, so the flag cannot reach the run it would apply to (the filmed run would keep writing its raw stream). To decline it for the producer, set %s=0 in the environment (the producer inherits it), or drop %s"

// chatFilmSgrRefusal returns the refusal for a raw-stream word this run was
// GIVEN on a film command line, or nil when none was. The two DIRECTIONS get
// different sentences (chatFilmDumpRefusal's shape): the arm flag names the
// destination it cannot deliver, the OFF switch names the artifact it cannot
// decline. The ENVIRONMENT is not covered, for the reason chatFilmDumpRefusal
// states: an exported variable is ambient context the producer inherits, so it
// works there.
func chatFilmSgrRefusal(given bool, opts chatSgrOptions) error {
	if !given {
		return nil
	}
	if opts.disabled {
		return fmt.Errorf(chatFilmNoSgrRefused, chatFlagFilm, chatNoSgrStreamFlag, chatSgrStreamEnv, chatNoSgrStreamFlag)
	}
	return fmt.Errorf(chatFilmSgrRefused, chatSgrStreamFlag, chatSgrStreamEnv)
}

// chatSgrEnvAliasWarn prints the pre-rename notice when THIS run's decision came
// from the legacy spelling. It is a separate function, called from the one place
// the resolved options are used, so chatSgrFromEnv stays a pure function a test
// can drive without capturing stderr.
func chatSgrEnvAliasWarn(out io.Writer) {
	name, _ := chatSgrEnvSource()
	if name != cliverbs.LegacyScreenStreamEnv {
		return
	}
	fmt.Fprintf(out, chatSgrEnvAliasNotice, cliverbs.LegacyScreenStreamEnv,
		cliverbs.ScreenStreamEnv, cliverbs.ScreenStreamEnv)
}
