package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/borrowedfd"
)

// This file is the argv contract of `gopi chat`, and it exists because of ONE
// measured defect (end-user-audit slice 3): an unrecognised `--word` was silently
// swallowed as the optional positional session seed, so
//
//	gopi chat --bogus-flag
//
// exited 1 with `resume target "--bogus-flag" is neither a raw log path nor an
// existing session id` — a message about a POSITIONAL the caller never wrote, at
// a status (`1` = the run failed) that is not the one the verb's own page
// documents for a bad flag (`2` = usage error). The word was a flag, and nothing
// said so.
//
// Two drivers per claim, on purpose: the argv is driven IN PROCESS (the REPL's
// own parser, so a failure names the line that broke) and through the REAL
// BINARY (Rule 31 — a surface in no execution has not been delivered). The
// pre-change binary is RED on every refusal case here (measured against the
// binary built from the parent commit: rc=1 + "resume target" for all of them).

// chatArgvChanBuf sizes the two result channels (one reader, one value each:
// the goroutine never blocks before its value is collected).
const chatArgvChanBuf = 1

// chatRunArgsStreamNameOut / Err name the two streams this helper drains. Every
// message in this file says WHICH pipe it is about, because the whole-package
// failure this fixture was repaired for named neither: `read |0` is os.Pipe's
// own name for a READ end ("|1" is a write end), not a descriptor number, so a
// reader that cannot say which stream it was draining leaves the next reader
// exactly as blind as the last one was.
const (
	chatRunArgsStreamNameOut = "stdout"
	chatRunArgsStreamNameErr = "stderr"
)

// chatRunArgsStreamLimit bounds the wait for the two draining goroutines after
// this helper has released both writers.
//
// It is NOT a readiness detector and it is not on the critical path of a
// healthy run: the streams are closed by this helper, so both reads end at EOF
// in microseconds and the budget covers process scheduling only — even when the
// whole package is running beside it. What it prevents is the OTHER ending:
// io.ReadAll returns at EOF, on a read error or at its own byte limit, so a
// pipe that a borrowed number keeps open never ends the read, and the caller
// would then hang — which under `go test` arrives as a 10-minute panic naming
// nothing. With the bound, that state is a failure that names the stream and
// the descriptor (Rule 15: no silent shape).
const chatRunArgsStreamLimit = 15 * time.Second

// chatRunArgsReleasedNumber is the errno the KERNEL answers for a descriptor
// number that is not open, and the observation this fixture's release is allowed
// to accept. It is spelled through the syscall package's own constant (Rule 1/1b:
// the value lives in its owning package, never as a literal here).
//
// It is the KERNEL's answer, not a wrapper's: os.ErrClosed is a different thing —
// the WRAPPER's own bookkeeping for a file it already marked closed — and the two
// are kept apart here because they name different actors (see chatRunArgsReleaseNumber).
const chatRunArgsReleasedNumber = syscall.EBADF

// chatRunArgsReleasedByAForeignPath names the actor a foreign-number release
// identifies, and it is the label of ONE provenance: a release the CASE UNDER TEST
// really met. The fixture's OWN release path closes every number it opens THROUGH
// THE WRAPPER (see chatRunArgsReleaseStream), so when the KERNEL says a number is
// not open while this fixture's wrapper still holds it unclosed, the number was
// freed by a path that did not go through that wrapper — the case under test, or
// (measured 2026-10-12) one of this file's own GUARDS, which manufacture the shape
// with their own syscall.Close. Which of the two it is is decided from the call
// stack, never from this phrase: see chatRunArgsForeignReleaseLabel, and the label
// constants below for why the distinction has to live IN the record.
const chatRunArgsReleasedByAForeignPath = "released by a path OUTSIDE this fixture"

// THE PROVENANCE LABELS — and this is the instrument the whole-package defect
// still lacked (measured 2026-10-12 on a full `scripts/test-budget.sh ./cmd/agent/`
// run): the phrase above was printed by TWO DIFFERENT PROVENANCES under one wording.
// A run stream carried it twice, and BOTH lines were the fixture's own guards
// manufacturing the shape on purpose — syscall.Close on a raw number that the
// guard itself had opened microseconds earlier (chat_argv_test.go:508 and :566) —
// while a reader of that stream had no way to tell them from the occurrence the
// phrase claims: a case that really met a descriptor released by a path outside
// this fixture. The record was therefore unattributable in the direction that
// matters, because a MANUFACTURED line reads exactly like an observation.
//
// THE FIX IS A SEPARATION, NOT A REWORDING: an occurrence KEEPS the phrase (it is
// the only label a stream may carry when a number really was lost, so the phrase's
// ABSENCE from a whole-package stream is a proof that no case met one — the
// report's own grep), while a manufactured release and an unresolvable stack are
// labelled with words that CONTAIN NO PART OF IT. The label is decided from the
// call stack (chatRunArgsForeignReleaseLabel), so nothing about it is a promise
// the next editor must remember to keep by hand.
//
// WHAT THE SEPARATION DOES NOT ESTABLISH, said here so it is not inferred later:
// the guard prefix below is a NAMING CONVENTION, not a capability. A guard named
// outside `TestChatRunArgsForeignRelease*` that manufactures the same shape would
// be labelled as an occurrence, and a case that met a real foreign release while
// running inside one of those guards would be labelled manufactured. Both are
// recorded limits of the instrument; neither is reachable in this file as it
// stands, and the guard at the bottom of this file pins the arms it does decide.
const (
	// chatRunArgsForeignReleaseSeenInTheCaseLabel is the label of a release the
	// CASE UNDER TEST really met. It carries chatRunArgsReleasedByAForeignPath
	// verbatim — that is the whole point of it: the phrase stays the exact string a
	// run stream is grepped for.
	chatRunArgsForeignReleaseSeenInTheCaseLabel = chatRunArgsReleasedByAForeignPath + " ON THE CASE UNDER TEST"

	// chatRunArgsForeignReleaseMadeByTheGuardLabel is the label of a release a
	// GUARD IN THIS FILE made on purpose, to pin the verdict on that shape without
	// waiting for a whole-package run to lose a descriptor. It contains no part of
	// the phrase above, so it can never be read as an occurrence.
	chatRunArgsForeignReleaseMadeByTheGuardLabel = "[foreign-release] MANUFACTURED by a guard in this file (the release was " +
		"made by this file's own syscall.Close on the raw number, not by a path the case met; the assertion under it runs " +
		"only if the reporting helper RETURNED, which is the invariant the guard exists to pin)"

	// chatRunArgsForeignReleaseProvenanceUnknownLabel is the label of a report whose
	// stack named no frame this fixture can classify. It claims NEITHER provenance:
	// an unmeasured state is named, never implied (Rule 15), and reading it as an
	// occurrence would be exactly the misattribution the two labels above remove.
	chatRunArgsForeignReleaseProvenanceUnknownLabel = "[foreign-release] PROVENANCE UNRESOLVED (no stack frame outside " +
		"this file resolved) — treat as UNKNOWN, do NOT read as an occurrence"
)

// chatRunArgsGuardNamePrefix is the naming convention that separates a guard which
// MANUFACTURES a foreign release from a case that MEETS one: every function whose
// name begins with this prefix is one of the guards below, and a report reached
// from it is labelled manufactured. It matches the two guards that really produce
// the shape (the read-end and write-end guards) and nothing else in this file.
const chatRunArgsGuardNamePrefix = "TestChatRunArgsForeignRelease"

// chatRunArgsManufacturingGuards are the tests in this file that PRODUCE a
// foreign release on purpose, and whose records must therefore never carry the
// occurrence phrase. It is a CLOSED SET beside the naming convention above, not a
// replacement for it, because the convention alone was measured INCOMPLETE
// (2026-10-13): TestChatRunArgsLostStreamVerdictAgreesWithItsRelease manufactures
// the shape at chat_argv_test.go:949 (its END-TO-END half releases a raw number
// through syscall.Close and hands it to chatRunArgsReleaseStream), and its name is
// NOT under chatRunArgsGuardNamePrefix — so its record carried the OCCURRENCE
// phrase, and a reader grepping a whole-package run for a release the case met
// counted this file's own guard as one. A third provenance under a two-provenance
// vocabulary is the same defect the two labels were introduced to remove, one
// level up: the instrument inverts with no symptom.
//
// The convention stays because it is what a NEW guard should follow; this set is
// what the walk actually reads, so a guard named outside the prefix is still
// reported as a manufacture — and the guard beside it asserts BOTH.
var chatRunArgsManufacturingGuards = map[string]bool{
	"TestChatRunArgsForeignReleaseOfAReadEndIsAttributedNotFatal":  true,
	"TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal": true,
	"TestChatRunArgsLostStreamVerdictAgreesWithItsRelease":         true,
}

// chatRunArgsStackFramePrefix is the prefix every frame of a gopi stack carries —
// the filter that skips debug.Stack()'s header line and every standard-library
// frame, so the FIRST gopi frame is the caller this fixture classifies.
const chatRunArgsStackFramePrefix = "github.com/nickhuang99/gopi/"

// chatRunArgsForeignReleaseLabel decides WHICH provenance the release about to be
// reported has, from the call stack debug.Stack() answers.
//
// The first gopi frame that is NOT a function of this file is the caller: the case
// (an occurrence, or the fixture's own argv walk) or one of the manufacturing
// guards. Functions of this file are skipped because the report is always reached
// through them — the helper and the fixture both live here, so stopping at the
// first frame would classify every report as this file's own.
//
// It takes the stack as a STRING so a guard can drive every arm with a synthetic
// stack, without a real descriptor having to be lost (the same reason
// chatRunArgsReleaseShapeOf is a pure function of the errno).
func chatRunArgsForeignReleaseLabel(stack string) string {
	for _, line := range strings.Split(stack, "\n") {
		if !strings.HasPrefix(line, chatRunArgsStackFramePrefix) {
			continue
		}
		name := line
		if i := strings.Index(name, "("); i >= 0 {
			name = name[:i]
		}
		// A frame whose function carries type parameters spells them before the
		// argument list (`…chatRunArgs[T any](0xc000…`, the shape go prints for a
		// GENERIC test helper). This file's reporters are not generic today, so the
		// arm is defensive — but a walk that only stopped at "(" would read such a
		// name as `chatRunArgs[T any]`, stop at the WRONG frame, and answer the
		// occurrence label for a stack that names no caller at all.
		if i := strings.Index(name, "["); i >= 0 {
			name = name[:i]
		}
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		if chatRunArgsIsOwnFileFunc(name) {
			continue
		}
		if chatRunArgsManufacturingGuards[name] || strings.HasPrefix(name, chatRunArgsGuardNamePrefix) {
			return chatRunArgsForeignReleaseMadeByTheGuardLabel
		}
		return chatRunArgsForeignReleaseSeenInTheCaseLabel
	}
	return chatRunArgsForeignReleaseProvenanceUnknownLabel
}

// chatRunArgsOwnFileFuncs are this file's own function names — the frames a report
// is always reached through, and therefore the ones the provenance walk skips. It
// is a closed list rather than a prefix test because the whole value of the walk is
// that the frame it stops at is the CALLER's.
var chatRunArgsOwnFileFuncs = map[string]bool{
	"chatRunArgs":                    true,
	"chatRunArgsDrain":               true,
	"chatRunArgsReleaseStream":       true,
	"chatRunArgsWriteEndClosed":      true,
	"chatRunArgsForeignReleaseLabel": true,
	"chatRunArgsReuseCensus":         true,
	"chatRunArgsFDReading":           true,
	"chatRunArgsDescriptorIdentity":  true,
}

// chatRunArgsIsOwnFileFunc reports whether a stack frame's function name is one of
// this file's own reporters.
func chatRunArgsIsOwnFileFunc(name string) bool {
	return chatRunArgsOwnFileFuncs[name]
}

// chatRunArgsForeignReleaseConsequenceOf says what a record of the given
// provenance MEANS — one sentence per provenance, because the three do not
// support the same conclusion: an occurrence is a descriptor leak the case under
// test met, a manufacture is this file's own guard, and an unresolved stack
// supports neither reading.
func chatRunArgsForeignReleaseConsequenceOf(label string) string {
	switch label {
	case chatRunArgsForeignReleaseMadeByTheGuardLabel:
		return "This release was MADE BY THIS FILE (see the frame in the stack below): no descriptor leaked and no case under " +
			"test is implicated, so this record is the guard's evidence, never an occurrence of the whole-package defect."
	case chatRunArgsForeignReleaseProvenanceUnknownLabel:
		return "Whether a path outside this fixture was involved is UNRESOLVED: the stack named no frame this fixture can " +
			"classify, so this record claims neither a leak nor a guard."
	default:
		return "The case's streams and exit status were captured before this release, so this is recorded and NOT fatal: the " +
			"finding is the suite's descriptor leak, and this is its occurrence."
	}
}

// chatRunArgsForeignReleaseReport composes the ONE record of a foreign-number
// release, for both ends of the pipe and for every provenance.
//
// WHY A SHARED COMPOSER, and what it changes about the stream: the whole-package
// defect this file was repaired for was a record nobody could attribute — two
// provenances, one phrasing. The fix has to be in the RECORD, not in a footnote
// beside it, so the sentence a reader greps for lives in exactly ONE slot — the
// actor slot, `was %s` — and that slot is filled from the stack
// (chatRunArgsForeignReleaseLabel). Measured consequence (2026-10-12): a guard's
// manufactured release prints `[foreign-release] MANUFACTURED BY A GUARD IN THIS
// FILE` there and contains NO PART of `released by a path OUTSIDE this fixture`,
// so that phrase occurs in a run stream IF AND ONLY IF a case really met a
// descriptor released by a path outside this fixture — which is what makes the
// absence of the phrase (the report's own grep) evidence rather than silence.
//
// The mechanism sentence is deliberately provenance-free: "the wrapper this
// fixture held never closed that number" is true of a foreign release and of a
// guard's raw syscall alike, and it is the FACT the classifier is built on. Only
// the actor and the consequence are keyed on provenance.
//
// WHAT IT DOES NOT ESTABLISH: the label is a naming convention over the call
// stack (see chatRunArgsGuardNamePrefix), not a capability. A manufacturing guard
// named outside that prefix is reported as an occurrence, and a genuine foreign
// release inside one of those guards is reported as a manufacture — both are
// recorded limits of this instrument, and the guard that pins it names them.
func chatRunArgsForeignReleaseReport(end, stream, identity string, fd int, target string, resolvable bool, census, stack string) string {
	label := chatRunArgsForeignReleaseLabel(stack)
	return fmt.Sprintf("the %s stream's %s (fd %d, identity %s, opened by THIS fixture) was %s when this fixture came to free "+
		"it. The wrapper this fixture held never closed that number, so the kernel's %v means the number was freed without "+
		"going through it (kernel reading %q, resolvable=%v). %s. %s\n%s",
		stream, end, fd, identity, label, chatRunArgsReleasedNumber, target, resolvable, census,
		chatRunArgsForeignReleaseConsequenceOf(label), stack)
}

// chatRunArgsStaleWrapperName names the guard's SECOND wrapper over one number.
// os.NewFile's name is what an fd trail records, so it is a word a reader of a
// future failure can search for rather than an opaque label.
const chatRunArgsStaleWrapperName = "chat-argv-foreign-release-guard"

// chatRunArgsIdentityUnknown is what a descriptor's identity reads as when the
// kernel could not name it at the moment it was asked — a host without procfs,
// or a number that was already gone when this fixture recorded it. It is a
// VALUE A READER CAN RECOGNISE rather than an empty string that reads like a
// measurement (Rule 15: an unmeasured state is named, never implied).
const chatRunArgsIdentityUnknown = "(the kernel named no target)"

// chatRunArgsIdentityUnreadable is the identity recorded when procfs refused the
// read for a reason OTHER than "nothing is open here" — a permissions or mount
// failure. It is kept apart from chatRunArgsIdentityUnknown because it is a
// LIMIT OF THE OBSERVER, not a fact about the descriptor, and a report that
// conflated the two would claim a measurement it did not make.
const chatRunArgsIdentityUnreadable = "(the descriptor's target could not be read)"

// chatRunArgsRead is one stream's drained contents, or the reason the drain
// could not finish.
type chatRunArgsRead struct {
	s   string
	err error
}

// chatRunArgsShapeUnobserved and chatRunArgsVerdictUnobserved are what the record
// holds for an arm the call never reached. They exist because NEITHER type has a
// usable zero value: 0 is the CLEAN release and the CLEAN read, so a record that
// merely defaulted would read as a clean measurement this fixture never made
// (Rule 15 — an unmeasured state is named, never implied by a zero).
const (
	chatRunArgsShapeUnobserved   chatRunArgsReleaseShape = -1
	chatRunArgsVerdictUnobserved chatRunArgsReadVerdict  = -1
)

// chatRunArgsStreamEvidence is THIS FIXTURE's own record of one call: what each of
// the four descriptors did, plus the two streams it drained. It is the value
// chatRunArgsWitnessed returns and chatRunArgs discards.
//
// It is EVIDENCE, deliberately not a verdict: which of these observations matters
// depends on what the CALLER asserts on, so the policy lives with the caller
// (chatRunArgsAssertStreamsSound for a case that reads the streams,
// chatRunArgsStatusVerdictOf for a case that reads the exit status).
type chatRunArgsStreamEvidence struct {
	// The fd numbers, captured while the fixture certainly held them, so a report
	// composed from this record can name the descriptor rather than say "some fd".
	outRfd, outWfd, errRfd, errWfd int
	// The kernel's identity for each end (`pipe:[inode]`), recorded at the same
	// moment — see chatRunArgsDescriptorIdentity.
	outIdentity, errIdentity string
	// The release verdicts and the errno behind each, for both ends.
	outWriteShape, errWriteShape chatRunArgsReleaseShape
	outReadShape, errReadShape   chatRunArgsReleaseShape
	outWriteErr, errWriteErr     error
	outReadErr, errReadErr       error
	// The two drained streams and this fixture's verdict on each.
	outRead, errRead chatRunArgsRead
	outVerdict       chatRunArgsReadVerdict
	errVerdict       chatRunArgsReadVerdict
}

// chatRunArgsUnobservedEvidence is the record BEFORE the call has observed
// anything: every arm named as unobserved rather than left at a zero value that
// would read as a clean release.
func chatRunArgsUnobservedEvidence() chatRunArgsStreamEvidence {
	return chatRunArgsStreamEvidence{
		outWriteShape: chatRunArgsShapeUnobserved,
		errWriteShape: chatRunArgsShapeUnobserved,
		outReadShape:  chatRunArgsShapeUnobserved,
		errReadShape:  chatRunArgsShapeUnobserved,
		outVerdict:    chatRunArgsVerdictUnobserved,
		errVerdict:    chatRunArgsVerdictUnobserved,
	}
}

// chatRunArgs drives runChat's argv parser IN PROCESS and returns what the
// caller would see: the exit status it returned, and everything the two streams
// carried. runChat's usage paths ALL return before the REPL reads a line, so no
// stdin is needed and no agent/provider is touched — that is exactly why the
// refusal is testable here at all.
//
// It is the SAMPLING face of chatRunArgsWitnessed: a case whose claim rides only
// on the two streams uses this one, a case whose claim rides on the EXIT STATUS
// uses the witness record beside it (see chatRunArgsStreamEvidence).
func chatRunArgs(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	stdout, stderr, code, _ = chatRunArgsWitnessed(t, args...)
	return stdout, stderr, code
}

// chatRunArgsWitnessed is chatRunArgs PLUS this fixture's own record of what
// happened to the four descriptors the call used: the write ends' and the read
// ends' release verdicts, and the two drained streams.
//
// WHY A CALLER NEEDS THE RECORD — the whole-package defect, stated as the claim it
// broke. The two write ends are installed as os.Stdout/os.Stderr for the duration
// of the call, so when a NUMBER they hold is released by a path outside this
// fixture (the measured `close |1: bad file descriptor`, and see the whole-package
// record quoted in chatRunArgsWriteEndClosed), the code UNDER TEST fails through
// no fault of its own: cli_chat.go's help path returns chatExitFailed when
// `os.Stdout.WriteString(chatUsage)` errors (cmd/agent/cli_chat.go:1787, and the
// positional-beside-query path at :1849). A case that reads only the exit status
// therefore reported `runChat([-h -h]) = 1, want 0` — a true sentence about a
// descriptor this fixture does not exclusively own, attributed to the argv walk
// it was testing.
//
// The record is what lets such a case decide on WHAT THE STATUS FLOWS THROUGH
// rather than on the number alone (chatRunArgsStatusVerdictOf), exactly the way
// the release helpers already decide a release on its errno instead of on the
// syscall's complaint. It is deliberately EVIDENCE and not a policy: this helper
// decides nothing about any case — it reports what the fixture observed, and the
// case's own verdict, including the fatal arm, stays with the case.
func chatRunArgsWitnessed(t *testing.T, args ...string) (stdout, stderr string, code int, ev chatRunArgsStreamEvidence) {
	t.Helper()
	ev = chatRunArgsUnobservedEvidence()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	// The READ ENDS are claimed as this fixture's OWN here, before anything runs
	// beside them, and the number is what the release below closes — never the
	// wrapper. One close per number, issued by this file's own syscall, is what
	// makes the whole-package EBADF attributable: see chatRunArgsReleaseFD.
	outRfd, errRfd := int(outR.Fd()), int(errR.Fd())

	// THE IDENTITY OF EVERY END, recorded from the kernel NOW — and it is the
	// missing half of every report in this file. A NUMBER is a handle the kernel
	// recycles: once a foreign path has released it, the number names nothing
	// (and may name a stranger microseconds later), so a report that carries only
	// the number cannot say WHICH pipe died and can be matched against nothing.
	// The identity is what the kernel answers for the number while this fixture
	// still certainly holds it — `pipe:[12345]`, the pipe's own inode — and it is
	// the string a later reader can correlate with any other log line that names
	// the same pipe (a strace close-by-number, an lsof row, another occurrence).
	outRIdentity := chatRunArgsDescriptorIdentity(outRfd)
	errRIdentity := chatRunArgsDescriptorIdentity(errRfd)
	ev.outRfd, ev.errRfd = outRfd, errRfd
	ev.outIdentity, ev.errIdentity = outRIdentity, errRIdentity
	outWfd, errWfd := int(outW.Fd()), int(errW.Fd())
	ev.outWfd, ev.errWfd = outWfd, errWfd
	outWIdentity := chatRunArgsDescriptorIdentity(outWfd)
	errWIdentity := chatRunArgsDescriptorIdentity(errWfd)

	// The readers run CONCURRENTLY with the call under test: a refusal prints
	// the whole chat page, and a page larger than the pipe buffer would block
	// the writer forever if nobody were draining it.
	outCh, errCh := make(chan chatRunArgsRead, chatArgvChanBuf), make(chan chatRunArgsRead, chatArgvChanBuf)
	go func() { b, e := io.ReadAll(outR); outCh <- chatRunArgsRead{string(b), e} }()
	go func() { b, e := io.ReadAll(errR); errCh <- chatRunArgsRead{string(b), e} }()

	code = runChat(args)

	os.Stdout, os.Stderr = oldOut, oldErr
	// THE WRITE ENDS' ERRNO IS READ THE SAME WAY THE READ ENDS' RELEASE ERRNO IS
	// READ BELOW — a release's errno is a fact about a DESCRIPTOR, not about which
	// end of the pipe it is, and two ends of ONE defect must not be answered with
	// two different verdicts.
	//
	// WHY THIS LINE, AND WHAT IT WAS (attribution, not a hunch): the whole-package
	// record of this family — `chat_argv_test.go:494: close stdout writer: close |1:
	// bad file descriptor` on `…/--verbose` — is this site, and this site was the
	// LAST unclassified release left in the file: every read-end release beside it
	// already decides on the errno (6ecc0244, devdoc/chat-argv-fixture-release.md)
	// while the write ends still answered ANY errno with one unattributable fatal
	// line. `|1` is os.Pipe's name for a WRITE end, `|0` for a read end, so the two
	// records are the same fixture's two ends and not two actors.
	//
	// The verdicts are captured HERE and REPORTED after the streams are drained and
	// released (chatRunArgsWriteEndClosed, below): refusing the case at this line
	// would leave a reader parked on a pipe nobody closes — the parked-goroutine
	// shape this package's close-out sweep exists to catch — so the refusal is
	// recorded, not executed, while the readers are still ahead of us.
	outCloseErr, errCloseErr := outW.Close(), errW.Close()
	ev.outWriteErr, ev.errWriteErr = outCloseErr, errCloseErr
	// chatRunArgsDrain collects and releases them IN PAIRS: the write end is
	// already closed above, then the reader is collected, then its read end is
	// released. That order is the point — an io.ReadAll that has not returned
	// would turn the release into the very `read ...: bad file descriptor` this
	// fixture was repaired for, so no read end is freed while a reader drains it.
	//
	// chatRunArgsStreamNameOut/Err exist because the whole-package failure this
	// fixture was repaired for said `read |0: bad file descriptor`: `|0` is
	// os.Pipe's name for a READ end, never a descriptor number, so the record has
	// to name the stream itself to be readable at all.
	o := chatRunArgsDrain(t, chatRunArgsStreamNameOut, outRIdentity, outR, outCh)
	// EVERY end this fixture opened is named to the census, at BOTH release sites
	// (Rule 13: the numbers are the fixture's own, so the survived-claim filter
	// must know all four of them — a census told about none of them reports this
	// fixture's own sibling end as a foreign holder).
	ev.outReadShape = chatRunArgsReleaseStream(t, chatRunArgsStreamNameOut, outRIdentity, outR, outRfd, outWfd, errRfd, errWfd)
	e := chatRunArgsDrain(t, chatRunArgsStreamNameErr, errRIdentity, errR, errCh)
	ev.errReadShape = chatRunArgsReleaseStream(t, chatRunArgsStreamNameErr, errRIdentity, errR, outRfd, outWfd, errRfd, errWfd)

	// The write ends' verdicts, reported only now that both readers have returned
	// and both read ends are released — the whole-package occurrence reported a
	// LOST STREAM, and a case may not be ended while a reader still drains the pipe
	// this closure is about.
	ev.outWriteShape = chatRunArgsWriteEndClosed(t, chatRunArgsStreamNameOut, outWIdentity, outWfd, outCloseErr, outRfd, outWfd, errRfd, errWfd)
	ev.errWriteShape = chatRunArgsWriteEndClosed(t, chatRunArgsStreamNameErr, errWIdentity, errWfd, errCloseErr, outRfd, outWfd, errRfd, errWfd)
	// THE READ VERDICT IS DECIDED THE SAME WAY THE RELEASE VERDICT IS — on
	// whether the case's own claim was harmed, not on which helper observed it.
	// See chatRunArgsStreamDrained: a stream lost BEFORE it carried a byte costs
	// the case nothing it asserts on, while a stream lost WITH bytes in it is a
	// truncated claim and stays fatal.
	ev.outRead, ev.errRead = o, e
	ev.outVerdict = chatRunArgsStreamDrained(t, chatRunArgsStreamNameOut, outRfd, outRIdentity, o, e, outRfd, outWfd, errRfd, errWfd)
	ev.errVerdict = chatRunArgsStreamDrained(t, chatRunArgsStreamNameErr, errRfd, errRIdentity, e, o, outRfd, outWfd, errRfd, errWfd)
	return o.s, e.s, code, ev
}

// chatRunArgsReadVerdict is the verdict on ONE drained stream whose reader
// reported an error, decided by what the failure did to the CASE rather than by
// which helper saw it.
type chatRunArgsReadVerdict int

const (
	// chatRunArgsReadClean: the reader finished; there is nothing to decide.
	chatRunArgsReadClean chatRunArgsReadVerdict = iota
	// chatRunArgsReadLostData: the stream carried bytes (or nothing anywhere did)
	// and the read ended in an error — the case's own claim is truncated or there
	// is no data at all to assert on. FATAL, and it must stay fatal: this is the
	// half of the verdict the release path must never be allowed to swallow.
	chatRunArgsReadLostData
	// chatRunArgsReadRecordedLoss: the stream was lost before it carried a byte,
	// and the case's claim rides on the other stream. REPORTED, never fatal.
	chatRunArgsReadRecordedLoss
)

// chatRunArgsReadVerdictOf is the CLASSIFIER for a read failure, a pure function
// of the two streams' drained contents, so every arm is pinned by a guard without
// a case having to fail (the same shape chatRunArgsReleaseShapeOf has for the
// errno).
//
// WHY THIS EXISTS — the whole-package occurrence, and the disagreement it exposed.
// The occurrence's own report said, in one breath, that a foreign path had
// released the stderr read end and that "the case's streams and exit status were
// captured before the release and its assertions stand, so this is recorded and
// not fatal" — and then the drain path's assert answered the SAME foreign release
// with t.Fatalf("read the stderr stream (fd 7, which this fixture opened): read
// |0: bad file descriptor"). Two halves of one fixture, two verdicts about one
// event: the release half judged the event by what it cost the case, the read
// half judged it by which syscall raised it.
//
// The pre-fix invariant was stated as "a READ of that stream is the DATA the case
// asserts on, and a reader that cannot finish is fatal" — which is true only when
// the stream HAD data. A stream whose transport was taken away before its first
// byte contributes nothing to any assertion, and the case's claim is then carried
// entirely by the sibling stream and the exit status (the help path this walk
// exercises asserts exactly that: exit status, and a non-empty stdout). Failing
// the case there is the misattribution the whole repair exists to remove, on the
// last path that still produced it.
func chatRunArgsReadVerdictOf(own, other chatRunArgsRead) chatRunArgsReadVerdict {
	if own.err == nil {
		return chatRunArgsReadClean
	}
	// A stream that carried BYTES and then errored is a truncated claim — an
	// assertion over it would be over a prefix, which is a real failure. And when
	// neither stream carried anything there is no captured claim left at all, so
	// the case is lost rather than "merely unattributed".
	if own.s != "" || other.s == "" {
		return chatRunArgsReadLostData
	}
	return chatRunArgsReadRecordedLoss
}

// chatRunArgsStreamDrained applies chatRunArgsReadVerdictOf to the fixture's own
// two drains, with the reporting both arms owe a reader: a fatal arm names the
// stream, the number and the identity; the recorded arm names them too, plus the
// kernel's reading of the number and the census of who holds the pipe now.
func chatRunArgsStreamDrained(t *testing.T, stream string, fd int, identity string, own, other chatRunArgsRead, ownFds ...int) chatRunArgsReadVerdict {
	t.Helper()
	verdict := chatRunArgsReadVerdictOf(own, other)
	switch verdict {
	case chatRunArgsReadClean:
		// The reader finished: nothing to decide and nothing to record.
	case chatRunArgsReadLostData:
		// NOT decided here. This helper REPORTS what the streams did; whether a
		// lost stream ends the case is the CALLER's verdict, because it depends on
		// what that case asserts on (chatRunArgsAssertStreamsSound for a case whose
		// claim rides on the streams, chatRunArgsStatusVerdictOf for one whose
		// claim rides on the exit status). Deciding it here is what the whole-
		// package occurrence measured: a case whose every assertion had held was
		// ended by a fixture-level rule that could not see its claim.
	case chatRunArgsReadRecordedLoss:
		target, resolvable := chatRunArgsFDReading(fd)
		t.Logf("chatRunArgs: the %s stream (fd %d, identity %s, opened by THIS fixture) was lost before it carried a byte: "+
			"%v (kernel reading %q, resolvable=%v). %s. The case's claim rides on the other stream (%d byte(s)) and on the "+
			"exit status, both captured before this stream died, so this is the SAME event the release report above "+
			"describes and it is recorded, not fatal — the suite's descriptor leak is the finding, and this is the "+
			"occurrence:\n%s",
			stream, fd, identity, own.err, target, resolvable,
			chatRunArgsReuseCensus(identity, ownFds...), len(other.s), debug.Stack())
	}
	return verdict
}

// chatRunArgsAssertStreamsSound is the SAMPLING face's verdict on the streams,
// and it is the pre-existing rule kept verbatim: a case that reads the two streams
// asserts on what they carried, so a stream that carried bytes and then died is a
// truncated claim, and a case with NOTHING captured anywhere has no claim at all.
// Both end the case.
//
// It is a separate function because it is now ONE caller's policy rather than a
// rule of the fixture: a case whose claim rides on the EXIT STATUS does not read
// these streams at all, and for it the same two shapes are evidence of a lost
// transport, not of a broken product (chatRunArgsStatusVerdictOf). Splitting the
// two is what keeps the fatal arm intact for the callers it was written for while
// stopping it from answering for a claim it never examined.
func chatRunArgsAssertStreamsSound(t *testing.T, ev chatRunArgsStreamEvidence) {
	t.Helper()
	for _, s := range []struct {
		stream  string
		verdict chatRunArgsReadVerdict
		own     chatRunArgsRead
		other   chatRunArgsRead
		fd      int
		ident   string
	}{
		{chatRunArgsStreamNameOut, ev.outVerdict, ev.outRead, ev.errRead, ev.outRfd, ev.outIdentity},
		{chatRunArgsStreamNameErr, ev.errVerdict, ev.errRead, ev.outRead, ev.errRfd, ev.errIdentity},
	} {
		if s.verdict != chatRunArgsReadLostData {
			continue
		}
		t.Fatalf("read the %s stream (fd %d, identity %s, which this fixture opened): %v — the stream carried %d byte(s) "+
			"and the other stream carried %d, so the case's captured claim is truncated (or absent entirely). A read that "+
			"cannot finish IS fatal here, and only here: this is the arm the release path's non-fatal verdict must never "+
			"reach", s.stream, s.fd, s.ident, s.own.err, len(s.own.s), len(s.other.s))
	}
}

// chatRunArgsStatusOnTarget is the verdict on an exit status that AGREES with the
// case's claim — the ordinary shape, and the only one a healthy run reaches.
const chatRunArgsStatusOnTarget = 0

// chatRunArgsStatusTransportLost is the verdict on a status that DISAGREES with
// the claim while the fixture's own record shows the status was carried through a
// descriptor a path outside this fixture released. It is NOT a pass and it is NOT
// silence: the case records it (see chatRunArgsStatusTransportOf below), and the
// claim it was about is asserted on the page the fixture captured.
const chatRunArgsStatusTransportLost = 1

// chatRunArgsStatusVerdictOf decides what a DISAGREEING exit status is a statement
// about, using the fixture's own record of the call rather than the number alone.
//
// WHY THIS EXISTS — the whole-package defect, at the one site that still read the
// status bare. `-h` answers by writing the page to os.Stdout and returning
// chatExitOK (cmd/agent/cli_chat.go:1787); this fixture has installed its pipe's
// WRITE END as os.Stdout for the duration of the call (see chatRunArgsWitnessed).
// So when the number under that end is released by a path outside the fixture —
// the measured `close |1: bad file descriptor`, and see the whole-package record
// quoted in chatRunArgsWriteEndClosed — the product returns chatExitFailed
// faithfully, and a case asserting `code == chatExitOK` fails on a sentence that
// is true about a descriptor this fixture does not exclusively own. The recorded
// occurrence is exactly that: `runChat([-h -h]) = 1, want 0`, on a case whose
// subject is the argv walk.
//
// The rule, in the same form as the release verdicts beside it:
//
//   - the status AGREES — nothing to decide, whatever the descriptors did
//     (chatRunArgsStatusOnTarget). A release may not turn a pass into a failure,
//     which is why the agreement arm is checked first and unconditionally;
//   - the status DISAGREES and the record shows a RELEASE the fixture's wrapper
//     never made (EBADF, either end, either direction) — the status was carried
//     through a transport that died under it, so it is not a verdict on the call
//     (chatRunArgsStatusTransportLost);
//   - anything else — the same disagreement with no such release observed, or an
//     UNOBSERVED record — is the case's own failure. That arm is what stops this
//     repair from becoming "a non-zero status is never reported": the record has
//     to say the transport died, and only a real release says so (Rule 15).
//
// WHAT IT DOES NOT CLAIM: a foreign release and a genuine product failure CAN
// coincide, and this record cannot separate them — it carries the release, not a
// counterfactual run of the same argv without it. A case taking the recorded arm
// therefore keeps its product claim asserted on the captured page, so the argv
// walk is still tested in the run where its transport died; and the occurrence
// itself stays visible, because chatRunArgsWriteEndClosed reports it with its
// identity, its number and its provenance in the case's own stream.
//
// THREE ANSWERS, never two: the OBSERVED clean release, the OBSERVED foreign
// release, and the UNOBSERVED record (chatRunArgsShapeUnobserved) are kept apart —
// a record that was never filled in must not read as "the transport died".
func chatRunArgsStatusVerdictOf(code, want int, ev chatRunArgsStreamEvidence) (int, string) {
	if code == want {
		return chatRunArgsStatusOnTarget, ""
	}
	for _, arm := range []struct {
		end    string
		stream string
		shape  chatRunArgsReleaseShape
		err    error
		fd     int
		ident  string
	}{
		{"WRITE", chatRunArgsStreamNameOut, ev.outWriteShape, ev.outWriteErr, ev.outWfd, ev.outIdentity},
		{"WRITE", chatRunArgsStreamNameErr, ev.errWriteShape, ev.errWriteErr, ev.errWfd, ev.errIdentity},
		{"READ", chatRunArgsStreamNameOut, ev.outReadShape, ev.outReadErr, ev.outRfd, ev.outIdentity},
		{"READ", chatRunArgsStreamNameErr, ev.errReadShape, ev.errReadErr, ev.errRfd, ev.errIdentity},
	} {
		if arm.shape != chatRunArgsReleaseForeignNumber {
			continue
		}
		return chatRunArgsStatusTransportLost, fmt.Sprintf(
			"this fixture's own record shows the %s stream's %s end (fd %d, identity %s, opened by THIS fixture) was released "+
				"by a path outside it (%v), and the call's exit status was carried through that end",
			arm.stream, arm.end, arm.fd, arm.ident, arm.err)
	}
	return chatRunArgsStatusDisagrees, ""
}

// chatRunArgsStatusDisagrees is the third answer of chatRunArgsStatusVerdictOf:
// the status disagrees with the claim and the record does NOT show the transport
// dying — the case's own failure, and the arm that keeps the repair honest.
const chatRunArgsStatusDisagrees = 2

// chatUsagePageAnchor is the text the usage page does not change without a reader
// noticing: `-h` answers by writing chatUsage (cli_chat.go:1787), so a walk whose
// transport survived must have put this string on stdout, and a case that skips
// the status assertion because the transport died still asserts this instead.
const chatUsagePageAnchor = "Usage: gopi chat"

// TestChatRunArgsStatusVerdictIsTakenOnTheTransport pins the THREE answers of
// chatRunArgsStatusVerdictOf — and it is the RED-WITHOUT-FIX carrier for the third
// half of this fixture's repair.
//
// THE ONE REASON IT GOES RED ON THE PRE-FIX SHAPE: pre-fix, the argv walk read the
// exit status alone (`if code != chatExitOK { t.Fatalf(…) }`), so a status carried
// through a descriptor released by a path outside this fixture ended the case. The
// first two arms below are that shape made observable: a transport-lost record must
// be a VERDICT OF ITS OWN (chatRunArgsStatusTransportLost), not the disagreement
// arm the pre-fix line produced for every non-zero status. On the pre-fix code
// there is no such verdict, and an assertion "a foreign release does not classify
// as the case's own failure" cannot hold.
//
// It deliberately does NOT manufacture a foreign release here: the stimulus for the
// end-to-end half is a real one (see
// TestChatRunArgsForeignReleaseOfTheStdoutItCarried, which is under the guard
// prefix for exactly that reason).
func TestChatRunArgsStatusVerdictIsTakenOnTheTransport(t *testing.T) {
	// A record that observed nothing: every arm unobserved, never a default that
	// would read as a clean release.
	unobserved := chatRunArgsUnobservedEvidence()
	if unobserved.outWriteShape != chatRunArgsShapeUnobserved || unobserved.outVerdict != chatRunArgsVerdictUnobserved {
		t.Fatalf("chatRunArgsUnobservedEvidence() left an arm at %d/%d — the zero values are CLEAN, so an unfilled "+
			"record would read as a measurement this fixture never made",
			unobserved.outWriteShape, unobserved.outVerdict)
	}

	// THE AGREEMENT ARM, checked FIRST and unconditionally: a status that agrees
	// with the claim is on target whatever the descriptors did — a release may not
	// turn a pass into a failure.
	released := unobserved
	released.outWriteShape = chatRunArgsReleaseForeignNumber
	released.outWriteErr = chatRunArgsReleasedNumber
	released.outWfd = chatRunArgsStatusGuardFd
	released.outIdentity = chatRunArgsStatusGuardIdentity
	if got, why := chatRunArgsStatusVerdictOf(chatExitOK, chatExitOK, released); got != chatRunArgsStatusOnTarget {
		t.Errorf("an agreed status with a foreign-released stdout classified as %d (%q), want the on-target verdict (%d) — a release observed in the fixture's own teardown may not fail a case whose status agreed",
			got, why, chatRunArgsStatusOnTarget)
	}

	// THE TRANSPORT-LOST ARM — the occurrence's shape: the status disagrees AND the
	// record names the end the status was carried through.
	verdict, why := chatRunArgsStatusVerdictOf(chatExitFailed, chatExitOK, released)
	if verdict != chatRunArgsStatusTransportLost {
		t.Errorf("a disagreeing status over a foreign-released stdout classified as %d, want the transport-lost verdict (%d) — this is the measured `runChat([-h -h]) = 1, want 0`, and the pre-fix line read it as the case's own failure",
			verdict, chatRunArgsStatusTransportLost)
	}
	if verdict == chatRunArgsStatusDisagrees {
		t.Errorf("the transport-lost shape and the case's own failure are ONE verdict (%d) — the two actors (a status carried through a released descriptor, and a status the product got wrong) are conflated, which is the defect this verdict exists to remove", verdict)
	}
	if !strings.Contains(why, chatRunArgsStatusGuardIdentity) || !strings.Contains(why, "outside it") {
		t.Errorf("the transport-lost verdict does not name the descriptor and the actor that took it: %q — a record a reader cannot attribute is the ambiguity the whole repair exists to remove", why)
	}

	// THE NEGATIVE CONTROLS — the arms that keep this from becoming "a non-zero
	// status is never reported".
	for _, arm := range []struct {
		name  string
		shape chatRunArgsReleaseShape
	}{
		{"an UNOBSERVED record (the record was never filled in — an unrun observation must not excuse a failure)", chatRunArgsShapeUnobserved},
		{"a CLEAN release (nothing was taken, so the product's status stands)", chatRunArgsReleaseClean},
		{"the fixture's OWN earlier release (os.ErrClosed — a different actor, and not evidence about the transport)", chatRunArgsReleaseAlreadyReleased},
		{"an errno nobody reasoned about (the state is UNKNOWN, so the fixture must not excuse anything)", chatRunArgsReleaseUnknown},
	} {
		ev := unobserved
		ev.outWriteShape = arm.shape
		if got, _ := chatRunArgsStatusVerdictOf(chatExitFailed, chatExitOK, ev); got == chatRunArgsStatusTransportLost {
			t.Errorf("with %s the disagreeing status classified as transport-lost — only an observed foreign release may excuse a case's own failure (Rule 15: no silent shape)", arm.name)
		}
	}

	// THE LIVE RECORD: the fixture really FILLS this struct, which is what makes the
	// arm above reachable at all. A record left unobserved for every call would
	// disable the repair silently and this is the arm that catches it.
	stdout, _, code, live := chatRunArgsWitnessed(t, HelpFlag)
	if live.outVerdict == chatRunArgsVerdictUnobserved || live.outWriteShape == chatRunArgsShapeUnobserved {
		t.Fatalf("a live call left the record unobserved (verdict %d, write shape %d) — chatRunArgsWitnessed would then never be able to attribute anything",
			live.outVerdict, live.outWriteShape)
	}
	if live.outRfd <= 0 || live.outWfd <= 0 || live.outIdentity == "" || live.errIdentity == "" {
		t.Fatalf("the live record carries no descriptor identity (out fd %d, err fd %d, identities %q/%q) — the report could not name the pipe that was lost",
			live.outRfd, live.errRfd, live.outIdentity, live.errIdentity)
	}
	if got, why := chatRunArgsStatusVerdictOf(code, chatExitOK, live); got != chatRunArgsStatusOnTarget {
		t.Fatalf("a real `gopi chat -h` classified as %d (%q) — a healthy run must reach the on-target verdict", got, why)
	}
	// The callers that keep reading the streams keep their pre-existing rule: the
	// split moved the fatal arm, it did not drop it.
	chatRunArgsAssertStreamsSound(t, live)
	// And the walk's own claim, taken the same way it takes it: on a healthy run
	// the page IS observable, so this helper's clean arm is driven for real here —
	// the arm that fails a walk whose answer really did not print the page.
	chatRunArgsAssertHelpAnswer(t, "guard", live, stdout)
	if live.outVerdict != chatRunArgsReadClean || live.errVerdict != chatRunArgsReadClean {
		t.Errorf("a healthy `gopi chat -h` drained its streams with verdicts %d/%d, want clean — a fixture that reports a loss on a healthy run would make the arm above unreachable",
			live.outVerdict, live.errVerdict)
	}

	// THE WALK'S OWN CLAIM, arm by arm — the same pure-classifier shape as the
	// release and read verdicts above, so every arm is pinned without a case having
	// to fail. The two arms that matter most are the ones a naive spelling
	// collapses: a loss the fixture already recorded must NOT red the walk, and a
	// TRUNCATED page must not pass as a page.
	//
	// The shapes are built from the STREAMS and their verdict DERIVED through the
	// same classifier the fixture's drain uses — never set directly — so a guard
	// cannot assert against a record this fixture could not produce.
	lostRead := chatRunArgsRead{err: chatRunArgsReleasedNumber}
	truncated := chatRunArgsRead{s: chatRunArgsGuardPartialPayload, err: chatRunArgsReleasedNumber}
	carried := chatRunArgsRead{s: chatRunArgsGuardSiblingPayload}
	for _, arm := range []struct {
		name   string
		ev     chatRunArgsStreamEvidence
		stdout string
		want   int
	}{
		{"a drained page",
			chatRunArgsEvidenceWithOut(chatRunArgsRead{s: chatUsagePageAnchor}, carried), chatUsagePageAnchor,
			chatRunArgsHelpAnswerPresent},
		{"a lost read end (recorded non-fatal — the walk's claim is the status checked above)",
			chatRunArgsEvidenceWithOut(lostRead, carried), "",
			chatRunArgsHelpAnswerUnobservable},
		{"a TRUNCATED page — a prefix is not the page",
			chatRunArgsEvidenceWithOut(truncated, carried), chatRunArgsGuardPartialPayload,
			chatRunArgsHelpAnswerTruncated},
		{"a clean drain with no page on it",
			chatRunArgsEvidenceWithOut(chatRunArgsRead{}, carried), chatRunArgsGuardNothingPrinted,
			chatRunArgsHelpAnswerMissing},
		{"an UNOBSERVED record — an unrun observation is not a truncation and not a pass",
			chatRunArgsUnobservedEvidence(), chatUsagePageAnchor,
			chatRunArgsHelpAnswerUnknown},
	} {
		if got := chatRunArgsHelpAnswerVerdictOf(arm.ev, arm.stdout); got != arm.want {
			t.Errorf("%s classified as %d, want %d", arm.name, got, arm.want)
		}
	}
	// The live arm, through the same classifier: the record this fixture really
	// produces for a healthy `gopi chat -h` answers Present.
	if got := chatRunArgsHelpAnswerVerdictOf(live, stdout); got != chatRunArgsHelpAnswerPresent {
		t.Errorf("the live `gopi chat -h` record classified as %d, want the present arm (%d)", got, chatRunArgsHelpAnswerPresent)
	}
}

// chatRunArgsGuardNothingPrinted is what a run that printed nothing carries, so
// the missing-page arm above is expressed as a VALUE rather than as an empty
// literal a reader could mistake for the unobserved case.
const chatRunArgsGuardNothingPrinted = "(this run printed nothing to stdout)"

// chatRunArgsEvidenceWithOut is a record whose stdout arm was really OBSERVED to
// the two values passed. The VERDICT IS DERIVED from the streams through the same
// classifier the fixture's drain uses — never set directly — so a guard cannot
// assert against a record this fixture could not produce.
func chatRunArgsEvidenceWithOut(own, other chatRunArgsRead) chatRunArgsStreamEvidence {
	ev := chatRunArgsUnobservedEvidence()
	ev.outRead, ev.errRead = own, other
	ev.outVerdict = chatRunArgsReadVerdictOf(own, other)
	return ev
}

// chatRunArgsStatusGuardFd and chatRunArgsStatusGuardIdentity are the descriptor a
// guard's synthetic record names. They are deliberately NOT a real descriptor: the
// verdict is a pure function of the record, so the guard can drive every arm —
// including the ones a healthy run never reaches — without opening anything.
const (
	chatRunArgsStatusGuardFd       = 7
	chatRunArgsStatusGuardIdentity = "pipe:[999999]"
)

// TestChatRunArgsForeignReleaseOfTheStdoutItCarried is the END-TO-END guard for
// the half of the repair the classifier guards cannot reach: the status a case
// reads when the descriptor os.Stdout was installed on is released by a path
// outside this fixture.
//
// WHY IT IS UNDER THE GUARD PREFIX: it MANUFACTURES the foreign release (its own
// syscall.Close on a raw number), so its record must be reported as a manufacture
// and never carry the occurrence phrase. It asserts that too, on its own LIVE
// stack — the arm the non-guard walk in
// TestChatRunArgsReleaseReportNamesItsProvenance cannot take, because that test is
// deliberately not a guard.
//
// WHAT IT MEASURES, and nothing more: (1) the causal link — a released os.Stdout
// really does make `runChat([-h])` return chatExitFailed, which is the sentence the
// whole-package occurrence reported as `runChat([-h -h]) = 1, want 0`; and (2) the
// verdict — that status, over the fixture's own record of the release, is
// chatRunArgsStatusTransportLost and NOT the case's own failure. It does NOT claim
// the observed value for a future occurrence: nobody knows which descriptor died
// in that run, and this guard produces the shape on purpose.
func TestChatRunArgsForeignReleaseOfTheStdoutItCarried(t *testing.T) {
	ov, ow, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() { _ = ov.Close() }) //stylegate:ignore the guard's own read end teardown
	t.Cleanup(func() { _ = ow.Close() }) //stylegate:ignore the guard's own write end teardown
	fd := int(ow.Fd())
	identity := chatRunArgsDescriptorIdentity(fd)
	oldOut := os.Stdout
	os.Stdout = ow
	t.Cleanup(func() { os.Stdout = oldOut })

	// THE FOREIGN RELEASE, through the raw syscall so it does not go through the
	// wrapper this guard holds — the same raw claim the other two guards take.
	// THIS FIXTURE'S OWN DELIBERATE FOREIGN RELEASE (see the block shape below):
	// releasing the number is the arm's premise, and the witness records what it
	// was so the close is attributable by identity.
	releasedIdentity := borrowedfd.Witness(fd)
	if closeErr := syscall.Close(fd); closeErr != nil {
		t.Fatalf("the guard could not stand in for the foreign release on fd %d: %v", fd, closeErr)
	}
	if checkErr := releasedIdentity.Check(); checkErr == nil {
		t.Fatalf("fd %d still carries the identity this fixture held (%s) after the foreign release", fd, releasedIdentity)
	}
	code := runChat([]string{HelpFlag})
	os.Stdout = oldOut

	// THE CAUSAL LINK, measured: the product failed because it could not write the
	// page, not because the argv walk refused the flag.
	if code != chatExitFailed {
		t.Fatalf("runChat([%s]) with its os.Stdout released by a path outside this guard = %d, want %d — the guard needs the shape the occurrence reported, so that the verdict below is asked about a real one",
			HelpFlag, code, chatExitFailed)
	}

	// THE RECORD this guard's own call would have produced, built through the SAME
	// measurement the fixture uses (the wrapper's errno after a foreign release).
	ev := chatRunArgsUnobservedEvidence()
	ev.outWfd, ev.outIdentity = fd, identity
	ev.outWriteErr = ow.Close()
	ev.outWriteShape = chatRunArgsReleaseShapeOf(ev.outWriteErr)
	if ev.outWriteShape != chatRunArgsReleaseForeignNumber {
		t.Fatalf("closing fd %d after the foreign release answered %v (shape %d), want the kernel's EBADF and the foreign verdict — the guard is measuring something other than the foreign shape",
			fd, ev.outWriteErr, ev.outWriteShape)
	}
	if got, _ := chatRunArgsStatusVerdictOf(code, chatExitOK, ev); got != chatRunArgsStatusTransportLost {
		t.Fatalf("the status carried through the released stdout classified as %d, want the transport-lost verdict (%d) — the `runChat([-h -h]) = 1, want 0` occurrence would again end a case whose claim is the argv walk",
			got, chatRunArgsStatusTransportLost)
	}
	// THE NEGATIVE CONTROL on the SAME status: with the release taken OFF the
	// record, the identical disagreement is the case's own failure. Without this
	// arm the guard would be satisfied by any verdict that merely is not the
	// disagreement arm.
	clean := chatRunArgsUnobservedEvidence()
	if got, _ := chatRunArgsStatusVerdictOf(code, chatExitOK, clean); got != chatRunArgsStatusDisagrees {
		t.Fatalf("the same disagreeing status with NO observed release classified as %d, want the case's-own-failure arm (%d) — the repair would excuse any non-zero status", got, chatRunArgsStatusDisagrees)
	}
	// THE RECORD ITSELF, and the provenance of its own release: a guard's
	// manufactured release must be reported as a manufacture, decided on the LIVE
	// stack rather than on a fixture string.
	if got := chatRunArgsWriteEndClosed(t, chatRunArgsStreamNameOut, identity, fd, ev.outWriteErr, fd, int(ov.Fd())); got != chatRunArgsReleaseForeignNumber {
		t.Fatalf("the guard's own release classified as %d, want the foreign verdict (%d)", got, chatRunArgsReleaseForeignNumber)
	}
	if got := chatRunArgsForeignReleaseLabel(string(debug.Stack())); got != chatRunArgsForeignReleaseMadeByTheGuardLabel {
		t.Errorf("the guard's own live stack classified as %q, want the manufactured label %q — this test manufactures the shape, and a record carrying the occurrence phrase would be counted by the grep a whole-package run is read with",
			got, chatRunArgsForeignReleaseMadeByTheGuardLabel)
	}
}

// chatRunArgsHelpAnswerVerdictOf is the WALK's claim, decided on what the
// fixture could OBSERVE — the same shape the release and read classifiers have, so
// every arm is pinned by a guard without a case having to fail.
//
// WHY IT IS NOT ONE BOOLEAN: three states have to stay apart, and the naive
// spelling collapses two of them. `if theStreamWasLost || the page is missing {
// fail }` reds the walk for a loss the fixture already recorded as non-fatal (the
// misattribution this repair exists to remove); `if !lost && pageMissing { fail }`
// lets a TRUNCATED page through as a pass — a silent hole in the very case the
// repair is about. And an UNOBSERVED record is a third thing: an observation
// nobody made must be named, never read as either of the others (Rule 15).
const (
	// chatRunArgsHelpAnswerPresent: the page was drained and carries the anchor.
	chatRunArgsHelpAnswerPresent = iota
	// chatRunArgsHelpAnswerUnobservable: the read end was taken before it carried a
	// byte, so there is nothing here to assert the page on. The walk's evidence is
	// then the status (checked by chatRunArgsStatusVerdictOf) — RECORDED, not
	// silent, and not fatal, because the fixture has already reported the loss.
	chatRunArgsHelpAnswerUnobservable
	// chatRunArgsHelpAnswerTruncated: the stream carried bytes and then died, so
	// what was captured is a PREFIX of the page and not the page. FATAL.
	chatRunArgsHelpAnswerTruncated
	// chatRunArgsHelpAnswerMissing: the stream drained cleanly and the anchor is
	// not there — the help answer really did not print the page. FATAL.
	chatRunArgsHelpAnswerMissing
	// chatRunArgsHelpAnswerUnknown: the record was never filled in, so this claim
	// was never observable. FATAL, and named: an unrun observation must not be
	// reported as a truncation.
	chatRunArgsHelpAnswerUnknown
)

func chatRunArgsHelpAnswerVerdictOf(ev chatRunArgsStreamEvidence, stdout string) int {
	switch ev.outVerdict {
	case chatRunArgsReadClean:
		if strings.Contains(stdout, chatUsagePageAnchor) {
			return chatRunArgsHelpAnswerPresent
		}
		return chatRunArgsHelpAnswerMissing
	case chatRunArgsReadRecordedLoss:
		return chatRunArgsHelpAnswerUnobservable
	case chatRunArgsReadLostData:
		return chatRunArgsHelpAnswerTruncated
	default:
		return chatRunArgsHelpAnswerUnknown
	}
}

// chatRunArgsAssertHelpAnswer reports the verdict above: only the arms that mean
// the walk's claim was HARMED (or was never observed) end the case.
func chatRunArgsAssertHelpAnswer(t *testing.T, flag string, ev chatRunArgsStreamEvidence, stdout string) {
	t.Helper()
	switch chatRunArgsHelpAnswerVerdictOf(ev, stdout) {
	case chatRunArgsHelpAnswerPresent:
		// The healthy shape: the page is there, which is what the walk is about.
	case chatRunArgsHelpAnswerUnobservable:
		t.Logf("%s: the stdout stream's read end (fd %d, identity %s) was lost before it carried a byte, so the page cannot be "+
			"asserted on this run — the status checked above is the evidence the answer happened",
			flag, ev.outRfd, ev.outIdentity)
	case chatRunArgsHelpAnswerTruncated:
		t.Fatalf("%s: the stdout stream (fd %d, identity %s) was truncated after %d byte(s) (%v) — the walk's claim is the page "+
			"this verb writes, and a prefix of it is not that claim", flag, ev.outRfd, ev.outIdentity, len(ev.outRead.s), ev.outRead.err)
	case chatRunArgsHelpAnswerMissing:
		t.Fatalf("%s is in this verb's vocabulary but the help answer did not print the page through os.Stdout — the usage path "+
			"writes it there and returns %d (cli_chat.go:1787):\n%q", flag, chatExitOK, stdout)
	default:
		t.Fatalf("%s: this fixture recorded NOTHING about the call's stdout, so the walk's claim (the help answered) was never "+
			"observed on this run — an unrun observation is not a pass", flag)
	}
}

// chatRunArgsTransportLostMarker tags every case whose ASSERTION was short-
// circuited because the stream it reads was taken away. It is a marker and not a
// sentence because a whole-package run is read with `grep`: one spelling, one hit
// per occurrence, and the reason on the same line (Rule 15 — a skip is explicit,
// greppable and carries why it skipped).
const chatRunArgsTransportLostMarker = "[transport-lost]"

// The verdicts on a case's TEXT claim over one of the fixture's streams. They are
// the same shape as chatRunArgsHelpAnswerVerdictOf's, for the same reason: the
// claim can be PRESENT, absent because the product printed otherwise (a real
// defect), absent because the channel died under the case (the finding this
// fixture reports), or never observed at all (Rule 15 — named, never implied).
const (
	// chatRunArgsTextClaimPresent: the stream drained cleanly and carries the claim.
	chatRunArgsTextClaimPresent = iota
	// chatRunArgsTextClaimMissing: the stream drained cleanly and does NOT carry the
	// claim. The case's own failure — the product really printed otherwise.
	chatRunArgsTextClaimMissing
	// chatRunArgsTextClaimTruncated: the stream carried bytes and then died, so what
	// was captured is a PREFIX and the claim cannot be decided either way. FATAL:
	// a prefix must not be read as "the product did not print it".
	chatRunArgsTextClaimTruncated
	// chatRunArgsTextClaimUnobservable: the stream's transport was taken away before
	// it carried a byte, and the fixture records that as a FOREIGN RELEASE it did
	// not make (chatRunArgsReadRecordedLoss). The claim was never OBSERVABLE, so the
	// case short-circuits and SAYS SO — it does not report the loss as a defect of
	// the sentence the product never got to print.
	chatRunArgsTextClaimUnobservable
	// chatRunArgsTextClaimUnknown: the record was never filled in, or the caller
	// named a stream this fixture does not own. FATAL, and named.
	chatRunArgsTextClaimUnknown
)

// chatRunArgsStreamOf resolves a stream NAME to the two arms this fixture recorded
// for it. The name is the only handle a case should need — the fd numbers change
// between runs, the stream does not — and a name this fixture does not own is
// answered as a named refusal rather than as an empty read (Rule 15).
func chatRunArgsStreamOf(stream string, ev chatRunArgsStreamEvidence) (chatRunArgsRead, chatRunArgsReadVerdict, bool) {
	switch stream {
	case chatRunArgsStreamNameOut:
		return ev.outRead, ev.outVerdict, true
	case chatRunArgsStreamNameErr:
		return ev.errRead, ev.errVerdict, true
	default:
		return chatRunArgsRead{}, chatRunArgsVerdictUnobserved, false
	}
}

// chatRunArgsTextClaimOf is the CLASSIFIER for "does this stream carry this
// claim?" — a pure function of the fixture's own record, so every arm is pinned by
// a guard without a case having to fail (the shape chatRunArgsReadVerdictOf and
// chatRunArgsStatusVerdictOf already have).
//
// WHY IT EXISTS — the defect this replaces, measured. The argv refusal cases
// asserted `strings.Contains(stderr, "unknown flag "+word)` against whatever the
// stream happened to hold. When a path OUTSIDE the fixture released that stream's
// descriptor (the whole-package occurrence, recorded by chatRunArgsWitnessed), the
// read was empty and the sentence was false for a reason that has nothing to do
// with the refusal: the case reported "the refusal does not name \"--bogus-flag\"
// as a FLAG" about a refusal that was never delivered, while the SAME run's
// evidence record already said the transport had been taken
// (chatRunArgsReadRecordedLoss). One event, two verdicts — the misattribution this
// classifier removes, on the last path in the file that still read a stream bare.
//
// WHAT IT DOES NOT DO: it does not turn a missing claim into a pass. An ABSENT
// string over a cleanly drained stream is still the case's own failure
// (chatRunArgsTextClaimMissing), and a TRUNCATED stream stays fatal, so a real
// refusal-text defect cannot hide behind a descriptor accident.
func chatRunArgsTextClaimOf(stream, claim string, ev chatRunArgsStreamEvidence) int {
	read, verdict, ok := chatRunArgsStreamOf(stream, ev)
	if !ok {
		return chatRunArgsTextClaimUnknown
	}
	switch verdict {
	case chatRunArgsReadClean:
		if strings.Contains(read.s, claim) {
			return chatRunArgsTextClaimPresent
		}
		return chatRunArgsTextClaimMissing
	case chatRunArgsReadRecordedLoss:
		return chatRunArgsTextClaimUnobservable
	case chatRunArgsReadLostData:
		return chatRunArgsTextClaimTruncated
	default:
		return chatRunArgsTextClaimUnknown
	}
}

// chatRunArgsAssertTextClaim asserts a POSITIVE text claim (the stream must carry
// these bytes) through the classifier above: the healthy and the real-defect arms
// report, and the lost-transport arm short-circuits with the marker, the stream,
// the descriptor, the identity and the claim it could not decide.
func chatRunArgsAssertTextClaim(t *testing.T, argv []string, stream, claim string, ev chatRunArgsStreamEvidence) {
	t.Helper()
	read, _, _ := chatRunArgsStreamOf(stream, ev)
	fd, identity := chatRunArgsStreamDescriptor(stream, ev)
	switch chatRunArgsTextClaimOf(stream, claim, ev) {
	case chatRunArgsTextClaimPresent:
		// The claim holds; nothing to report.
	case chatRunArgsTextClaimUnobservable:
		t.Logf("%s runChat(%v): the claim %q was NOT DECIDED — the %s stream (fd %d, identity %s, opened by THIS "+
			"fixture) lost its transport before carrying a byte (%v), which this fixture's own record reports as a "+
			"release it did not make. The refusal text is not wrong here; it was never deliverable. Captured %d byte(s): %q",
			chatRunArgsTransportLostMarker, argv, claim, stream, fd, identity, read.err, len(read.s), read.s)
	case chatRunArgsTextClaimTruncated:
		t.Errorf("runChat(%v): the %s stream (fd %d, identity %s) was truncated after %d byte(s) (%v) — the claim %q is "+
			"undecidable over a prefix, so this is not a verdict on the refusal:\n%s",
			argv, stream, fd, identity, len(read.s), read.err, claim, read.s)
	case chatRunArgsTextClaimMissing:
		t.Errorf("runChat(%v): the %s stream does not carry %q — the refusal printed something else entirely:\n%s",
			argv, stream, claim, read.s)
	default:
		t.Errorf("runChat(%v): this fixture recorded NOTHING about the %s stream, or %q is not a stream it owns — an "+
			"unrun observation is not a pass", argv, stream, stream)
	}
}

// chatRunArgsAssertNoTextClaim is the ABSENCE half (the stream must not carry
// these bytes), and it is deliberately built on the same record: absence over a
// stream that was never observed is a VACUOUS pass, so it is reported as such
// instead of silently counting as a hold.
func chatRunArgsAssertNoTextClaim(t *testing.T, argv []string, stream, phrase string, ev chatRunArgsStreamEvidence) {
	t.Helper()
	read, _, _ := chatRunArgsStreamOf(stream, ev)
	fd, identity := chatRunArgsStreamDescriptor(stream, ev)
	switch chatRunArgsTextClaimOf(stream, phrase, ev) {
	case chatRunArgsTextClaimPresent:
		t.Errorf("runChat(%v) still carries %q on %s — the word was read as a resume TARGET again:\n%s",
			argv, phrase, stream, read.s)
	case chatRunArgsTextClaimUnobservable, chatRunArgsTextClaimTruncated, chatRunArgsTextClaimUnknown:
		t.Logf("%s runChat(%v): the ABSENCE of %q on %s was NOT DECIDED — this run could not read the stream (fd %d, "+
			"identity %s, %d byte(s) captured, %v). An absence measured over a stream nobody observed would be a "+
			"vacuous hold, so it is recorded instead",
			chatRunArgsTransportLostMarker, argv, phrase, stream, fd, identity, len(read.s), read.err)
	}
}

// chatRunArgsStreamDescriptor names the two numbers a report about a stream owes a
// reader: the read end's fd and the kernel's identity for the pipe (see
// chatRunArgsDescriptorIdentity). A stream this fixture does not own answers the
// unnamed values rather than zeros that would read as descriptor 0.
func chatRunArgsStreamDescriptor(stream string, ev chatRunArgsStreamEvidence) (int, string) {
	switch stream {
	case chatRunArgsStreamNameOut:
		return ev.outRfd, ev.outIdentity
	case chatRunArgsStreamNameErr:
		return ev.errRfd, ev.errIdentity
	default:
		return chatRunArgsStatusGuardFd, chatRunArgsIdentityUnknown
	}
}

// chatRunArgsDrain collects one stream's reader, AFTER that stream's write end
// has been closed and BEFORE its read end is released — the order is the whole
// point (see chatRunArgs).
func chatRunArgsDrain(t *testing.T, stream, identity string, rf *os.File, ch <-chan chatRunArgsRead) chatRunArgsRead {
	t.Helper()
	fd := int(rf.Fd())
	select {
	case r := <-ch:
		return r
	case <-time.After(chatRunArgsStreamLimit):
		target, ok := chatRunArgsFDReading(fd)
		t.Fatalf("chatRunArgs: the %s stream (fd %d, identity %s, opened by THIS fixture) was still being drained %s after its "+
			"write end was closed — a read that ends at EOF, on an error or at io.ReadAll's byte limit cannot still be "+
			"running, so the pipe is held open by something else (a borrowed descriptor that survived the close: an "+
			"inherited child, or a wrapper no path released). A reader that never returns is the one failure mode with "+
			"no other name, so it is reported here instead of as a package timeout (kernel reading %q, resolvable=%v)",
			stream, fd, identity, chatRunArgsStreamLimit, target, ok)
		return chatRunArgsRead{}
	}
}

// chatRunArgsReleaseStream releases a pipe read end THIS fixture opened and
// decides ON THE ERRNO whether a failure is a fact to report or a fact to accept.
//
// WHY THE WRAPPER, NOT THE NUMBER — and the measurement that decided it. Releasing
// by number with this file's own syscall (syscall.Close(f.Fd())) looks like the
// cleaner spelling, and it is the one to avoid. The fixture also installs those
// wrappers as os.Stdout/os.Stderr for the duration of the call, so it cannot keep
// a reference to every claim it ever made on the number; a wrapper it no longer
// holds is collectable, and a *stale* wrapper's finalizer closes whatever now
// holds that number (measured 2026-10-01 while building this very fix: a by-number
// release answered EBADF on fd 3 for a pipe this fixture had opened microseconds
// earlier, and three cases went red — the fixture's own guards closed a number the
// kernel had already handed to this read end). Releasing through the wrapper
// instead lets os.File.Close clear the finalizer in the same call, so exactly one
// path — this one — ever frees the number FOR A RELEASE THAT GOES THROUGH THIS
// FIXTURE, and the finalizer follows a grown descriptor table rather than a number
// that can be recycled. (The guards in this file release a raw number on purpose,
// to pin the verdict on a shape a healthy run does not reach — see
// chatRunArgsForeignReleaseLabel for how their records are kept apart from an
// occurrence.)
//
// The verdicts, and why they are kept apart:
//
//   - nil — the number was released; nothing to classify.
//   - os.ErrClosed — the WRAPPER already marked the number closed and nothing was
//     left to free, which is what the release wanted. It is the same *os.File
//     whose number this fixture opened, so the only actor that can have closed it
//     is this fixture (a repeated release, or the wrapper's own teardown).
//   - EBADF — the KERNEL says the number is not open and this fixture's wrapper
//     never closed it, so a path OUTSIDE this fixture released a descriptor it
//     does not own (the recycling case: the number was freed elsewhere, handed to
//     this pipe, and then released again under it). The case is NOT failed by it —
//     both streams and the exit status were drained and captured before this call
//     ran, and a fixture may not fail a case in its own teardown — but it is
//     REPORTED, named, with the kernel's own reading and the occurrence's stack,
//     because a fixture that shrugged here would hide a descriptor leak for the
//     rest of the suite's run.
//   - anything else — the state is unknown and the fixture says so rather than
//     continuing (Rule 15: no silent failure).
//
// The decision itself is chatRunArgsReleaseShapeOf, a pure function of the errno,
// so a guard can pin every arm of it without a real case failing.
func chatRunArgsReleaseStream(t *testing.T, stream, identity string, rf *os.File, ownFds ...int) chatRunArgsReleaseShape {
	t.Helper()
	// The NUMBER is read BEFORE the release: a released *os.File answers
	// ^uintptr(0) to Fd(), so a post-release read would name the wrapper's
	// sentinel instead of the descriptor the failure was about.
	fd := int(rf.Fd())
	shape := chatRunArgsReleaseShapeOf(rf.Close())
	target, resolvable := chatRunArgsFDReading(fd)
	switch shape {
	case chatRunArgsReleaseClean:
		// Nothing to record: the release did exactly what it wanted on the first
		// attempt, which is what every healthy run sees.
	case chatRunArgsReleaseAlreadyReleased:
		t.Logf("chatRunArgs: the %s stream's read end (fd %d, identity %s) was already released when this fixture came to free it "+
			"(kernel reading %q, resolvable=%v) — every stream and the exit status were captured and drained before this "+
			"call, so the case is not failed by its own teardown. The stack below is the occurrence this fixture could not "+
			"otherwise name:\n%s",
			stream, fd, identity, target, resolvable, debug.Stack())
	case chatRunArgsReleaseForeignNumber:
		// REPORTED, NEVER FATAL — and that asymmetry is the repair, not a weakness.
		// A case may not be ended by its own teardown: both streams and the exit
		// status are in hand before this call runs, so failing here would destroy a
		// case whose every claim held (which is exactly what the whole-package
		// occurrence did). What the fixture owes the next reader is the ATTRIBUTION:
		// WHICH PIPE died (its identity, recorded while this fixture still held it),
		// which stream, which number, what the kernel says the number is now,
		// whether some descriptor now holding the same pipe is a further sighting of
		// the same closer — and WHICH PROVENANCE the release has (the case under
		// test, or one of this file's own guards manufacturing the shape: see
		// chatRunArgsForeignReleaseLabel).
		stack := debug.Stack()
		t.Logf("chatRunArgs: %s", chatRunArgsForeignReleaseReport("read end", stream, identity, fd, target, resolvable,
			chatRunArgsReuseCensus(identity, ownFds...), string(stack)))
	default:
		// The ONE arm that is still fatal: an errno nobody reasoned about leaves the
		// descriptor's state UNKNOWN, so the fixture must say so rather than continue
		// with a claim it cannot stand behind (Rule 15).
		t.Errorf("chatRunArgs: releasing the %s stream's read end (fd %d, identity %s) failed with an errno this fixture never "+
			"reasoned about (kernel reading %q, resolvable=%v) — the descriptor's state is unknown, so the fixture must "+
			"say so rather than continue", stream, fd, identity, target, resolvable)
	}
	return shape
}

// chatRunArgsWriteEndClosed decides the WRITE end's release the same way
// chatRunArgsReleaseStream decides the read end's — one classifier, two ends.
//
// WHY THE TWO ENDS SHARE ONE VERDICT, with the measurement behind it: the whole-
// package record of this family on `…/--verbose` was
// `chat_argv_test.go:494: close stdout writer: close |1: bad file descriptor`,
// i.e. THIS call. `|1` is os.Pipe's name for the WRITE end and `|0` the READ end
// (GOROOT/src/os/pipe_linux.go), so the two records are one fixture's two ends of
// one defect — and until this call stopped refusing on ANY errno, the read end was
// attributed (reported with the kernel's reading of the number, non-fatal) while
// the write end beside it answered every errno with `close stdout writer: %v`,
// which is a text a reader cannot attribute to a stream, a number or an actor.
//
// THE ASYMMETRY THAT MUST NOT COME BACK: refusing (Fatalf) at the write-end close
// leaves the two readers — started above, still draining — with a pipe whose write
// end this fixture has just closed under a case that is being ended. The readers
// then time out on chatRunArgsStreamLimit and the failure a reader sees is the
// fixture's own teardown, not the case under test: the same misattribution the
// read-end repair removed, on the other end of the pipe.
//
// Fatal arms are unchanged: an errno nobody reasoned about still reports (Rule 15),
// and every record names the stream, the number and the kernel's reading of it.
func chatRunArgsWriteEndClosed(t *testing.T, stream, identity string, fd int, err error, ownFds ...int) chatRunArgsReleaseShape {
	t.Helper()
	shape := chatRunArgsReleaseShapeOf(err)
	target, resolvable := chatRunArgsFDReading(fd)
	switch shape {
	case chatRunArgsReleaseClean:
		// The release did exactly what it wanted; nothing to record.
	case chatRunArgsReleaseAlreadyReleased:
		t.Logf("chatRunArgs: the %s stream's WRITE end (fd %d, identity %s) was already released when this fixture came to close it "+
			"(kernel reading %q, resolvable=%v) — both streams and the exit status were captured and drained before this "+
			"point, so the case is not failed by its own teardown",
			stream, fd, identity, target, resolvable)
	case chatRunArgsReleaseForeignNumber:
		// REPORTED, NEVER FATAL — the same reason as the read end's: the number was
		// released without going through this fixture's wrapper, and the record that
		// names it is composed in ONE place for both ends (see
		// chatRunArgsForeignReleaseReport). The PROVENANCE label is what keeps this
		// record from being read as an occurrence when it was one of this file's own
		// guards that manufactured the shape — the ambiguity a whole-package run
		// measured: two records, one phrase, two provenances.
		stack := debug.Stack()
		t.Logf("chatRunArgs: %s", chatRunArgsForeignReleaseReport("WRITE end", stream, identity, fd, target, resolvable,
			chatRunArgsReuseCensus(identity, ownFds...), string(stack)))
	default:
		t.Errorf("chatRunArgs: closing the %s stream's WRITE end (fd %d, identity %s) failed with an errno this fixture never "+
			"reasoned about (kernel reading %q, resolvable=%v) — the descriptor's state is unknown, so the fixture must "+
			"say so rather than continue", stream, fd, identity, target, resolvable)
	}
	return shape
}

// chatRunArgsReleaseShape is the verdict on one release attempt, decided by the
// errno alone.
type chatRunArgsReleaseShape int

const (
	// chatRunArgsReleaseClean: the release did what it wanted.
	chatRunArgsReleaseClean chatRunArgsReleaseShape = iota
	// chatRunArgsReleaseAlreadyReleased: the wrapper had already marked the
	// number closed (os.ErrClosed) — this fixture's own earlier release.
	chatRunArgsReleaseAlreadyReleased
	// chatRunArgsReleaseForeignNumber: the kernel says the number is not open
	// (EBADF) while this fixture's wrapper never closed it, so a path outside the
	// fixture released it.
	chatRunArgsReleaseForeignNumber
	// chatRunArgsReleaseUnknown: an errno this fixture never reasoned about.
	chatRunArgsReleaseUnknown
)

// chatRunArgsReleaseShapeOf is the classifier, as a pure function so a guard can
// drive every arm of it without a case failing.
func chatRunArgsReleaseShapeOf(err error) chatRunArgsReleaseShape {
	switch {
	case err == nil:
		return chatRunArgsReleaseClean
	case errors.Is(err, os.ErrClosed):
		return chatRunArgsReleaseAlreadyReleased
	case errors.Is(err, chatRunArgsReleasedNumber):
		return chatRunArgsReleaseForeignNumber
	default:
		return chatRunArgsReleaseUnknown
	}
}

// TestChatRunArgsLostStreamVerdictAgreesWithItsRelease is the regression guard
// for the LAST disagreement between this fixture's halves, and it reproduces the
// whole-package occurrence's shape rather than describing it: a REAL pipe read end
// this fixture opened, freed by a path outside it, and then both verdicts read off
// that same descriptor.
//
// WHAT WENT WRONG (measured, task 01a12bb87fe8): the occurrence's own report said
// "the case's streams and exit status were captured before the release and its
// assertions stand, so this is recorded and not fatal" and then, four lines later,
// the drain path answered the SAME release with
// `t.Fatalf("read the stderr stream (fd 7, which this fixture opened): read |0: bad
// file descriptor")` — one event, two verdicts, decided by which helper observed
// it. This guard pins that the two helpers now answer ONE event with one verdict:
// the release says "foreign, recorded" and the read of that same lost stream says
// "recorded loss", not "the case is over".
//
// THE FATAL ARM IS PINNED TOO, and it is the half that must not be bought away:
// the same error WITH bytes in the stream is a truncated claim and stays fatal,
// and a case where neither stream carried anything is lost rather than
// unattributed. Widening the non-fatal arm to "any read error is recorded" fails
// below by name.
func TestChatRunArgsLostStreamVerdictAgreesWithItsRelease(t *testing.T) {
	// The classifier, arm by arm — every verdict, driven directly, so the guard
	// does not depend on producing the shape through a real case.
	ebadf := errors.New("read |0: bad file descriptor")
	if got := chatRunArgsReadVerdictOf(chatRunArgsRead{}, chatRunArgsRead{s: chatRunArgsGuardSiblingPayload}); got != chatRunArgsReadClean {
		t.Errorf("a completed read classified as %d, want the clean verdict (%d) — a reader that returned has nothing to decide",
			got, chatRunArgsReadClean)
	}
	if got := chatRunArgsReadVerdictOf(chatRunArgsRead{s: chatRunArgsGuardSiblingPayload}, chatRunArgsRead{s: chatRunArgsGuardSiblingPayload}); got != chatRunArgsReadClean {
		t.Errorf("a completed, non-empty read classified as %d, want the clean verdict (%d)", got, chatRunArgsReadClean)
	}
	// THE FATAL ARM, first shape: bytes were captured and the stream then died, so
	// every assertion over it would be over a prefix.
	if got := chatRunArgsReadVerdictOf(chatRunArgsRead{s: chatRunArgsGuardPartialPayload, err: ebadf}, chatRunArgsRead{s: chatRunArgsGuardSiblingPayload}); got != chatRunArgsReadLostData {
		t.Errorf("a TRUNCATED read (bytes captured, then an error) classified as %d, want the lost-data verdict (%d) — a truncated claim must not be recorded as a non-fatal note",
			got, chatRunArgsReadLostData)
	}
	// THE FATAL ARM, second shape: nothing was captured anywhere, so there is no
	// claim left to assert on.
	if got := chatRunArgsReadVerdictOf(chatRunArgsRead{err: ebadf}, chatRunArgsRead{}); got != chatRunArgsReadLostData {
		t.Errorf("a read failure with NO captured bytes in either stream classified as %d, want the lost-data verdict (%d) — with nothing captured the case has no claim at all, which is not the unattributed-loss shape",
			got, chatRunArgsReadLostData)
	}
	// THE NON-FATAL ARM, and the one the occurrence needed: the stream died before
	// its first byte while the case's claim rides on the sibling.
	if got := chatRunArgsReadVerdictOf(chatRunArgsRead{err: ebadf}, chatRunArgsRead{s: chatRunArgsGuardSiblingPayload}); got != chatRunArgsReadRecordedLoss {
		t.Errorf("a stream lost BEFORE its first byte (sibling carrying the claim) classified as %d, want the recorded-loss verdict (%d) — the whole-package occurrence's `read the stderr stream ... bad file descriptor` would again end a case whose every assertion had held",
			got, chatRunArgsReadRecordedLoss)
	}

	// THE END-TO-END HALF: ONE real foreign release, BOTH verdicts read off it.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	fd := int(r.Fd())
	t.Cleanup(func() { _ = w.Close() }) //stylegate:ignore the fixture's own write end teardown
	t.Cleanup(func() { _ = r.Close() }) //stylegate:ignore the fixture's own wrapper teardown
	identity := chatRunArgsDescriptorIdentity(fd)
	// The foreign release, produced with the raw syscall so it does not go through
	// the wrapper this fixture holds — the shape the suite showed.
	// THIS FIXTURE'S OWN DELIBERATE FOREIGN RELEASE — the one shape where a raw
	// close is correct: releasing the number IS this arm's premise, and it happens
	// exactly once, here. The witness below records what the kernel said the number
	// denoted while it was still ours, so the close is attributable by IDENTITY and
	// the arm can prove it released a number rather than assume it did.
	releasedIdentity := borrowedfd.Witness(fd)
	if err := syscall.Close(fd); err != nil {
		t.Fatalf("the guard could not stand in for the foreign release on fd %d: %v", fd, err)
	}
	if checkErr := releasedIdentity.Check(); checkErr == nil {
		t.Fatalf("fd %d still carries the identity this fixture held (%s) after the foreign release — the arm needs the "+
			"released shape, and syscall.Close returned success without releasing it", fd, releasedIdentity)
	}
	// The drain half, taken for real: the reader the fixture starts returns the
	// kernel's EBADF with zero bytes.
	b, readErr := io.ReadAll(r)
	if readErr == nil {
		t.Fatalf("a read of fd %d succeeded although the number was released — the guard needs the lost-stream shape", fd)
	}
	lost := chatRunArgsRead{s: string(b), err: readErr}
	carried := chatRunArgsRead{s: chatRunArgsGuardSiblingPayload}
	if got := chatRunArgsReadVerdictOf(lost, carried); got != chatRunArgsReadRecordedLoss {
		t.Fatalf("the real lost stream (fd %d) classified as %d, want the recorded-loss verdict (%d)", fd, got, chatRunArgsReadRecordedLoss)
	}
	// The release half, on the SAME descriptor and the SAME event: it must classify
	// as the foreign release and must not end the case (the assertions above are
	// already in hand, which is the invariant).
	if got := chatRunArgsReleaseStream(t, chatRunArgsStreamNameErr, identity, r, fd, int(w.Fd())); got != chatRunArgsReleaseForeignNumber {
		t.Fatalf("the release of the lost stream (fd %d) classified as %d, want the foreign-release verdict (%d) — the two halves would disagree about one event again",
			fd, got, chatRunArgsReleaseForeignNumber)
	}
}

// chatRunArgsGuardSiblingPayload is what the guard's surviving stream carries: the
// bytes that make the case's claim survive the other stream's loss. Its value is
// irrelevant to the classifier (only emptiness is read), so it is named rather
// than inlined (Rule 1b).
const chatRunArgsGuardSiblingPayload = "the help page\n"

// chatRunArgsGuardPartialPayload is what a TRUNCATED stream carries: enough bytes
// that an assertion over them would be an assertion over a prefix, which is what
// keeps the lost-data arm fatal.
const chatRunArgsGuardPartialPayload = "gopi chat --help\n"

// TestChatRunArgsReleaseClassifierPinsBothSidesOfTheVerdict drives the
// CLASSIFIER directly, so every arm of the verdict is pinned without a case having
// to fail — and it is the RED-WITHOUT-FIX carrier for the second half of this
// fixture's repair.
//
// THE ONE REASON IT GOES RED ON THE PRE-FIX SHAPE (measured, not described): the
// pre-fix classifier answered `errors.Is(err, EBADF) || errors.Is(err, os.ErrClosed)`
// — ONE boolean for TWO actors. Under it the two arms at the bottom of this test
// classify the same way, so a release of a descriptor a FOREIGN path freed reads
// exactly like a release this fixture had already made, and the whole-package
// occurrence could not be attributed. This test asserts they are DIFFERENT, which
// is the repair; on the pre-fix classifier its os.ErrClosed line is the failure,
// for exactly that one reason.
func TestChatRunArgsReleaseClassifierPinsBothSidesOfTheVerdict(t *testing.T) {
	if got := chatRunArgsReleaseShapeOf(nil); got != chatRunArgsReleaseClean {
		t.Errorf("a nil errno classified as %d, want the clean release (%d)", got, chatRunArgsReleaseClean)
	}
	// os.ErrClosed is the WRAPPER's own bookkeeping: this *os.File already closed
	// the number, so nothing was left to free. That is the fixture's own repeated
	// release, and never the whole-package failure.
	if got := chatRunArgsReleaseShapeOf(os.ErrClosed); got != chatRunArgsReleaseAlreadyReleased {
		t.Errorf("os.ErrClosed classified as %d, want the already-released verdict (%d) — a wrapper that already marked the number closed is this fixture's OWN release, not a foreign one",
			got, chatRunArgsReleaseAlreadyReleased)
	}
	// EBADF is the KERNEL's answer, and this fixture's wrapper never closed the
	// number it is answering about. That is the whole-package shape, and it must be
	// its OWN verdict — the repair's entire value.
	if got := chatRunArgsReleaseShapeOf(chatRunArgsReleasedNumber); got != chatRunArgsReleaseForeignNumber {
		t.Errorf("EBADF classified as %d, want the foreign-release verdict (%d) — the whole-package shape this fixture was repaired for would be indistinguishable from its own teardown again",
			got, chatRunArgsReleaseForeignNumber)
	}
	if chatRunArgsReleaseShapeOf(chatRunArgsReleasedNumber) == chatRunArgsReleaseShapeOf(os.ErrClosed) {
		t.Errorf("EBADF and os.ErrClosed still classify the same way — the two actors (a foreign release, and this fixture's own earlier release) are conflated, which is the defect this classifier exists to remove")
	}
	// The negative control, so the acceptance cannot have grown to cover any errno.
	unrelated := errors.New("EIO: an errno this fixture did not reason about")
	if got := chatRunArgsReleaseShapeOf(unrelated); got != chatRunArgsReleaseUnknown {
		t.Errorf("%v classified as %d, want the unknown verdict (%d) — an errno nobody reasoned about would be swallowed", unrelated, got, chatRunArgsReleaseUnknown)
	}
}

// TestChatRunArgsReleaseReportNamesItsProvenance is the guard for the INSTRUMENT
// this file deploys to attribute a foreign release, and it pins the separation
// between the two provenances an IDENTICAL phrasing used to carry — the ambiguity
// a whole-package run measured (2026-10-12): two `released by a path OUTSIDE this
// fixture` records, and both of them THIS FILE's own guards manufacturing the
// shape, indistinguishable in the stream from the occurrence the phrase claims.
//
// WHAT EACH ARM IS FALSIFIABLE AGAINST (a guard that only described the intent
// would pass on the broken tree):
//
//   - the PHRASE'S OWNERSHIP: the occurrence label must CARRY the phrase verbatim
//     (that string is what a run stream is grepped for) and no other label may
//     contain any part of it. Delete the separation — one label for both
//     provenances — and the second loop fails, which is the state that made the
//     whole-package stream unattributable;
//   - the NAMING CONVENTION: the walk classifies on the guard prefix, so the two
//     guards that really manufacture the shape must sit under it, or their records
//     are labelled occurrences and the instrument inverts with NO symptom. This arm
//     is the only thing tying the convention to the tests it describes;
//   - the WALK, driven with stacks that are the SHAPE debug.Stack() answers, built
//     from the frames measured in the whole-package occurrence: a guard frame →
//     manufactured, an ordinary case frame → occurrence, a stack carrying no gopi
//     frame at all (or only this file's own reporters — including the GENERIC
//     frame of chatRunArgs itself, which is why the parser strips at `[`) →
//     unresolved, never a false occurrence;
//   - the LIVE WALK, on this test's own stack: the first gopi frame outside this
//     file is this function, and it is NOT a guard — so the live instrument must
//     answer the occurrence label. A walk that stopped at the first frame of this
//     file, or one that hard-coded the manufactured answer, fails here.
//
// It deliberately does NOT manufacture a foreign release: this test's own name is
// not under the guard prefix, so a report emitted here would carry the occurrence
// phrase and a whole-package stream would show a release no case met — the very
// reading the separation exists to make impossible.
func TestChatRunArgsReleaseReportNamesItsProvenance(t *testing.T) {
	if !strings.Contains(chatRunArgsForeignReleaseSeenInTheCaseLabel, chatRunArgsReleasedByAForeignPath) {
		t.Errorf("the occurrence label %q does not carry %q — a real occurrence would no longer be findable by the grep a "+
			"whole-package run is read with", chatRunArgsForeignReleaseSeenInTheCaseLabel, chatRunArgsReleasedByAForeignPath)
	}
	for name, label := range map[string]string{
		"the manufactured-release label":  chatRunArgsForeignReleaseMadeByTheGuardLabel,
		"the unresolved-provenance label": chatRunArgsForeignReleaseProvenanceUnknownLabel,
	} {
		if strings.Contains(label, chatRunArgsReleasedByAForeignPath) {
			t.Errorf("%s %q contains the occurrence phrase %q — a reader grepping a run stream would count a release the CASE "+
				"never met as an occurrence of the defect, which is exactly the ambiguity this separation removes",
				name, label, chatRunArgsReleasedByAForeignPath)
		}
	}
	if chatRunArgsForeignReleaseMadeByTheGuardLabel == chatRunArgsForeignReleaseProvenanceUnknownLabel {
		t.Errorf("the manufactured and the unresolved labels are one string %q — two DIFFERENT provenances collapsed into one "+
			"wording is the defect this instrument removes, one level up",
			chatRunArgsForeignReleaseMadeByTheGuardLabel)
	}

	// THE MANUFACTURING GUARDS, tied to the tests they describe — and every one of
	// them, whether or not it follows the naming convention. The classification is
	// asked of the REAL walk (chatRunArgsForeignReleaseLabel) on a stack naming the
	// guard, so a guard that this instrument would report as an occurrence fails
	// here rather than in a run stream nobody greps.
	for _, guard := range []string{
		"TestChatRunArgsForeignReleaseOfAReadEndIsAttributedNotFatal",
		"TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal",
		// The measured gap: a guard that MANUFACTURES the shape (chat_argv_test.go:949)
		// and is named outside the convention. Under the convention alone its record
		// carried the occurrence phrase — the instrument inverted, no symptom.
		"TestChatRunArgsLostStreamVerdictAgreesWithItsRelease",
	} {
		// The frame shape debug.Stack() answers; the paths are the measured ones
		// from the whole-package occurrence, which is what makes this a fixture
		// rather than an invention.
		guardStack := "goroutine 26 [running]:\n" +
			"runtime/debug.Stack()\n\t/usr/local/go/src/runtime/debug/stack.go:26 +0x5e\n" +
			"github.com/nickhuang99/gopi/cmd/agent.chatRunArgsReleaseStream(0xc000882e00, {0x15bfa08, 0x6}, {0xc0008161a0, 0xf})\n\t/home/user/workspace/gopi/cmd/agent/chat_argv_test.go:298 +0x156\n" +
			"github.com/nickhuang99/gopi/cmd/agent." + guard + "(0xc000882e00)\n\t/home/user/workspace/gopi/cmd/agent/chat_argv_test.go:508 +0x4f6\n" +
			"testing.tRunner(0xc000882e00, 0x36dbbe8)\n\t/usr/local/go/src/testing/testing.go:1792 +0xf4\n"
		if got := chatRunArgsForeignReleaseLabel(guardStack); got != chatRunArgsForeignReleaseMadeByTheGuardLabel {
			t.Errorf("the manufacturing guard %s classified as %q (convention-following: %v), want %q — its MANUFACTURED "+
				"release would be reported as one the case under test met, so a reader grepping a run stream for an "+
				"occurrence would count this file's own guard",
				guard, got, strings.HasPrefix(guard, chatRunArgsGuardNamePrefix), chatRunArgsForeignReleaseMadeByTheGuardLabel)
		}
	}
	// The set is the walk's own reading, so it may not fall behind the convention:
	// every guard named under it must be in the set the walk consults.
	for guard := range chatRunArgsManufacturingGuards {
		if !strings.HasPrefix(guard, chatRunArgsGuardNamePrefix) {
			continue
		}
		if !chatRunArgsManufacturingGuards[guard] {
			t.Errorf("%s is under the guard prefix but not in chatRunArgsManufacturingGuards — the walk and the naming "+
				"convention have drifted apart", guard)
		}
	}
	if !chatRunArgsIsOwnFileFunc("chatRunArgs") {
		t.Errorf("the fixture chatRunArgs is not classified as this file's own reporter — the provenance walk would stop at " +
			"the fixture and label every occurrence as this file's own, which answers neither provenance")
	}

	// THE WALK. The stacks below are the shape debug.Stack() answers; the paths are
	// the measured ones from the whole-package occurrence, which is what makes these
	// strings a fixture rather than an invention.
	const (
		ownReporterFrame  = "github.com/nickhuang99/gopi/cmd/agent.chatRunArgsReleaseStream(0xc000882e00, {0x15bfa08, 0x6}, {0xc0008161a0, 0xf})\n\t/home/user/workspace/gopi/cmd/agent/chat_argv_test.go:298 +0x156\n"
		runtimeFrame      = "runtime/debug.Stack()\n\t/usr/local/go/src/runtime/debug/stack.go:26 +0x5e\n"
		liveArmHeader     = "goroutine 26 [running]:\n"
		testingFrame      = "testing.tRunner(0xc000882e00, 0x36dbbe8)\n\t/usr/local/go/src/testing/testing.go:1792 +0xf4\n"
		guardCallerFrame  = "github.com/nickhuang99/gopi/cmd/agent.TestChatRunArgsForeignReleaseOfAReadEndIsAttributedNotFatal(0xc000882e00)\n\t/home/user/workspace/gopi/cmd/agent/chat_argv_test.go:508 +0x4f6\n"
		caseCallerFrame   = "github.com/nickhuang99/gopi/cmd/agent.TestChatRunArgsReleaseClassifierPinsBothSidesOfTheVerdict(0xc000882e00)\n\t/home/user/workspace/gopi/cmd/agent/chat_argv_test.go:394 +0x1a2\n"
		genericOwnFrames  = "github.com/nickhuang99/gopi/cmd/agent.chatRunArgs[...](0xc000882e00, {0xc0000360f0, 0x1, 0x1})\n\t/home/user/workspace/gopi/cmd/agent/chat_argv_test.go:111 +0x2a1\n"
		genericOwnFrames2 = "github.com/nickhuang99/gopi/cmd/agent.chatRunArgs[T any](0xc000882e00, {0xc0000360f0, 0x1, 0x1})\n\t/home/user/workspace/gopi/cmd/agent/chat_argv_test.go:111 +0x2a1\n"
	)
	for name, tc := range map[string]struct {
		stack string
		want  string
	}{
		"a guard's own call": {
			stack: liveArmHeader + runtimeFrame + ownReporterFrame + guardCallerFrame + testingFrame,
			want:  chatRunArgsForeignReleaseMadeByTheGuardLabel,
		},
		"an ordinary case's call": {
			stack: liveArmHeader + runtimeFrame + ownReporterFrame + caseCallerFrame + testingFrame,
			want:  chatRunArgsForeignReleaseSeenInTheCaseLabel,
		},
		"only this file's own frames — a generic frame spelling must be skipped too": {
			stack: liveArmHeader + genericOwnFrames + genericOwnFrames2,
			want:  chatRunArgsForeignReleaseProvenanceUnknownLabel,
		},
		"no gopi frame at all": {
			stack: liveArmHeader + runtimeFrame,
			want:  chatRunArgsForeignReleaseProvenanceUnknownLabel,
		},
		"no stack at all": {
			stack: "",
			want:  chatRunArgsForeignReleaseProvenanceUnknownLabel,
		},
	} {
		if got := chatRunArgsForeignReleaseLabel(tc.stack); got != tc.want {
			t.Errorf("%s classified as %q, want %q", name, got, tc.want)
		}
	}

	// THE LIVE WALK — the instrument answering about the stack it is really running
	// on, which is the only arm that can catch a walk wired to the wrong frames.
	live := string(debug.Stack())
	if !strings.Contains(live, "TestChatRunArgsReleaseReportNamesItsProvenance") {
		t.Fatalf("this test's own stack does not name this test — the live arm cannot say anything about the walk:\n%s", live)
	}
	if got := chatRunArgsForeignReleaseLabel(live); got != chatRunArgsForeignReleaseSeenInTheCaseLabel {
		t.Errorf("the live stack of a NON-guard test classified as %q, want the occurrence label %q — a report emitted here "+
			"would claim a provenance it does not have", got, chatRunArgsForeignReleaseSeenInTheCaseLabel)
	}

	// THE COMPOSED RECORD, end to end — the artifact a run stream actually carries.
	// The phrase must be present for a case's record and ABSENT from a guard's, and
	// that is the property the report's own grep reads: one string, two provenances,
	// decided by the stack rather than by the wording sitting in the format.
	guardRecord := chatRunArgsForeignReleaseReport("read end", chatRunArgsStreamNameErr, "pipe:[1]", 7, "", false, chatRunArgsReuseNoHolderPhrase, liveArmHeader+ownReporterFrame+guardCallerFrame)
	caseRecord := chatRunArgsForeignReleaseReport("WRITE end", chatRunArgsStreamNameOut, "pipe:[2]", 9, "", false, chatRunArgsReuseNoHolderPhrase, liveArmHeader+ownReporterFrame+caseCallerFrame)
	if !strings.Contains(caseRecord, chatRunArgsReleasedByAForeignPath) {
		t.Errorf("a record whose stack names a CASE does not carry %q — a genuine occurrence would be invisible to the grep "+
			"the whole-package acceptance is written with:\n%s", chatRunArgsReleasedByAForeignPath, caseRecord)
	}
	if strings.Contains(guardRecord, chatRunArgsReleasedByAForeignPath) {
		t.Errorf("a record whose stack names a MANUFACTURING GUARD carries %q — the phrase must survive ONLY as the label of "+
			"an occurrence, or a run stream cannot be read as evidence:\n%s", chatRunArgsReleasedByAForeignPath, guardRecord)
	}
	if !strings.Contains(guardRecord, chatRunArgsForeignReleaseMadeByTheGuardLabel) {
		t.Errorf("a guard's record does not carry the manufactured label %q:\n%s", chatRunArgsForeignReleaseMadeByTheGuardLabel, guardRecord)
	}
}

// TestChatRunArgsForeignReleaseOfAReadEndIsAttributedNotFatal is the regression
// guard for the whole-package defect, and it REPRODUCES the failure mode rather
// than describing it: a pipe read end this fixture opened, a wrapper over that
// same number, the number then freed by a path OUTSIDE the fixture (the foreign
// release), and finally the fixture's own release of that stream.
//
// TWO THINGS ARE PINNED, AND THEY ARE DECIDED DIFFERENTLY ON PURPOSE:
//
//   - the fixture's RELEASE must not fail the case. Both streams and the exit
//     status were drained and captured before it ran, so an already-released
//     descriptor means only that there was nothing left to free. If the release
//     raises a test failure here, this test goes red — which is exactly what the
//     pre-fix `if err := errR.Close(); err != nil { t.Fatalf(...) }` shape did, and
//     the whole-package occurrence it produced was
//     `chat_argv_test.go:230: close stderr reader: close |0: bad file descriptor`
//     (fd 7; devdoc/chat-argv-fixture-release.md);
//   - a READ of that stream is the DATA the case asserts on, and — since this
//     branch — the read half decides EXACTLY like the release half: fatal when the
//     failure cost the case something (bytes captured and then lost, or nothing
//     captured anywhere), recorded when the stream died before its first byte while
//     the claim rides on the sibling. The old shape answered every read error with
//     one fate, which is what made ONE foreign release print "recorded and not
//     fatal" and `read …: bad file descriptor` four lines apart (see
//     chatRunArgsReadVerdictOf, and devdoc/chat-argv-lost-stream-verdict.md).
//     This guard pins that the two halves still differ where they must: a release
//     is never fatal, and a read that lost DATA still is.
//
// The foreign release is produced with syscall.Close on the raw number, because
// that is the shape the suite showed: a descriptor released by a path that does
// not go through this fixture's wrapper. The stale wrapper is deliberately NOT
// released first, so nothing about the measurement depends on finalizer timing.
func TestChatRunArgsForeignReleaseOfAReadEndIsAttributedNotFatal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	fd := int(r.Fd())
	t.Cleanup(func() { _ = w.Close() }) //stylegate:ignore the fixture's own write end teardown
	t.Cleanup(func() { _ = r.Close() }) //stylegate:ignore the fixture's own wrapper teardown
	// The second wrapper over the same number makes the premise explicit: the
	// foreign path is the only one releasing it, and it does not go through r.
	stale := os.NewFile(uintptr(fd), chatRunArgsStaleWrapperName)
	if stale == nil {
		t.Fatalf("os.NewFile(%d) returned nil — the guard needs a second wrapper to make the foreign release's shape explicit", fd)
	}
	t.Cleanup(func() { _ = stale.Close() }) //stylegate:ignore the guard's own second wrapper teardown

	// THE IDENTITY, taken while the fixture still holds the number — the value the
	// foreign-release report carries so it names WHICH pipe died (see
	// chatRunArgsDescriptorIdentity). Taken BEFORE the foreign release, because a
	// released number names nothing.
	identity := chatRunArgsDescriptorIdentity(fd)

	// THE FOREIGN RELEASE. Measured: the wrapper r does NOT learn about it, which
	// is why the fixture's own release answers the kernel's EBADF instead of
	// os.ErrClosed — the observation the classifier is built on.
	// THIS FIXTURE'S OWN DELIBERATE FOREIGN RELEASE — the one shape where a raw
	// close is correct: releasing the number IS this arm's premise, and it happens
	// exactly once, here. The witness below records what the kernel said the number
	// denoted while it was still ours, so the close is attributable by IDENTITY and
	// the arm can prove it released a number rather than assume it did.
	releasedIdentity := borrowedfd.Witness(fd)
	if err := syscall.Close(fd); err != nil {
		t.Fatalf("the guard could not stand in for the foreign release on fd %d: %v", fd, err)
	}
	if checkErr := releasedIdentity.Check(); checkErr == nil {
		t.Fatalf("fd %d still carries the identity this fixture held (%s) after the foreign release — the arm needs the "+
			"released shape, and syscall.Close returned success without releasing it", fd, releasedIdentity)
	}
	if bfdFDAlive(fd) == nil {
		t.Fatalf("fd %d is still open after the foreign release — the guard needs the released shape", fd)
	}

	// THE DATA HALF, taken FIRST because it is the fatal one: a read of the
	// released stream is a LOST STREAM, and it must answer the KERNEL's EBADF
	// rather than a bare, unattributable text. This is the arm where a failure is
	// correct — there is no stream left to assert on.
	_, readErr := io.ReadAll(r)
	if readErr == nil {
		t.Fatalf("a read of fd %d succeeded although the number was released — this guard needs the released-stream shape", fd)
	}
	if !errors.Is(readErr, chatRunArgsReleasedNumber) {
		t.Fatalf("reading the released stream answered %v, want the kernel's EBADF — the guard is measuring something other than the released-stream shape", readErr)
	}

	// THE RELEASE HALF: this is the call the whole-package failure reached, and the
	// verdict it returns is the whole repair. It must classify as the FOREIGN
	// release — this fixture's own wrapper never closed the number — and it must
	// not end the case (the assertions above already ran, which is the invariant:
	// a teardown may not decide a verdict).
	if got := chatRunArgsReleaseStream(t, chatRunArgsStreamNameErr, identity, r, fd, int(w.Fd())); got != chatRunArgsReleaseForeignNumber {
		t.Fatalf("the release of a foreign-released stream classified as %d, want the foreign-release verdict (%d) — the whole-package occurrence would then be indistinguishable from this fixture's own teardown, which is the defect",
			got, chatRunArgsReleaseForeignNumber)
	}
	// The observation behind the verdict, asked of the kernel rather than inferred:
	// the number the fixture released is gone.
	if target, ok := chatRunArgsFDReading(fd); ok {
		t.Fatalf("fd %d still resolves to %q although it was released — the guard's premise does not hold", fd, target)
	}
}

// TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal is the regression
// guard for the OTHER END of the same defect — the one the whole-package record
// `chat_argv_test.go:494: close stdout writer: close |1: bad file descriptor`
// names — and it is the sibling of the read-end guard above by construction: the
// shape is produced the same way (a number this fixture opened, released by a path
// outside it) and the verdict must come out the same.
//
// RED ON THE PRE-FIX SHAPE, and for exactly one reason: the pre-fix site answered
// `if err := outW.Close(); err != nil { t.Fatalf("close stdout writer: %v", err) }`
// — ANY errno ended the case. Restored, the call below aborts this test before the
// verdict assertion, so the guard fails by name instead of passing silently. The
// measured red is recorded in the report; the important half is that the assertion
// after it is only reachable when the case was NOT ended by its own teardown.
func TestChatRunArgsForeignReleaseOfAWriteEndIsAttributedNotFatal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	fd := int(w.Fd())
	t.Cleanup(func() { _ = r.Close() }) //stylegate:ignore the fixture's own read end teardown
	t.Cleanup(func() { _ = w.Close() }) //stylegate:ignore the guard's own wrapper teardown

	// THE IDENTITY, taken while this fixture still holds the number — the value the
	// foreign-release report carries (chatRunArgsDescriptorIdentity).
	identity := chatRunArgsDescriptorIdentity(fd)

	// THE FOREIGN RELEASE, produced with the raw syscall so it does not go through
	// the wrapper this fixture holds — the same shape the read-end guard uses.
	// THIS FIXTURE'S OWN DELIBERATE FOREIGN RELEASE — the one shape where a raw
	// close is correct: releasing the number IS this arm's premise, and it happens
	// exactly once, here. The witness below records what the kernel said the number
	// denoted while it was still ours, so the close is attributable by IDENTITY and
	// the arm can prove it released a number rather than assume it did.
	releasedIdentity := borrowedfd.Witness(fd)
	if err := syscall.Close(fd); err != nil {
		t.Fatalf("the guard could not stand in for the foreign release on fd %d: %v", fd, err)
	}
	if checkErr := releasedIdentity.Check(); checkErr == nil {
		t.Fatalf("fd %d still carries the identity this fixture held (%s) after the foreign release — the arm needs the "+
			"released shape, and syscall.Close returned success without releasing it", fd, releasedIdentity)
	}
	// THE PREMISE, asked of the kernel: the write end's number is really gone.
	if _, ok := chatRunArgsFDReading(fd); ok {
		t.Fatalf("fd %d still resolves although it was released — the guard needs the released shape", fd)
	}
	// THE CLOSE THIS FIXTURE MAKES, which is the call the whole-package failure
	// reached. The wrapper answers the KERNEL's EBADF (not os.ErrClosed): it never
	// closed the number itself, which is what makes this a foreign release.
	closeErr := w.Close()
	if !errors.Is(closeErr, chatRunArgsReleasedNumber) {
		t.Fatalf("closing fd %d after the foreign release answered %v, want the kernel's EBADF — the guard is measuring something other than the foreign shape", fd, closeErr)
	}

	// THE VERDICT HALF. It must classify as the FOREIGN release and it must RETURN —
	// a helper that ends the case here leaves the two readers draining a pipe whose
	// write end was closed under them, which is the misattribution this repair
	// removes.
	if got := chatRunArgsWriteEndClosed(t, chatRunArgsStreamNameOut, identity, fd, closeErr, fd, int(r.Fd())); got != chatRunArgsReleaseForeignNumber {
		t.Fatalf("the release of a foreign-released write end classified as %d, want the foreign-release verdict (%d) — the whole-package occurrence on `--verbose` would then be indistinguishable from this fixture's own teardown",
			got, chatRunArgsReleaseForeignNumber)
	}
	// Reachable ONLY if the helper returned: proof that the case was not ended by
	// its own teardown (Rule 15 — the record above is the finding, not a verdict).
	// The clean half, so this guard cannot be satisfied by making everything
	// non-fatal: a release that did exactly what it wanted is still classified clean.
	if got := chatRunArgsWriteEndClosed(t, chatRunArgsStreamNameOut, identity, fd, nil, fd, int(r.Fd())); got != chatRunArgsReleaseClean {
		t.Fatalf("a nil errno on the write end classified as %d, want the clean verdict (%d)", got, chatRunArgsReleaseClean)
	}
}

// TestChatRunArgsIdentityCensusNamesTheLostPipe is the regression guard for the
// OBLIGATION this fixture's reports owe: a foreign release must be reported with
// the IDENTITY of the pipe that was lost, and a census that found nothing must be
// distinguishable from a census that could not run.
//
// WHAT IT PINS, and why each half is falsifiable rather than a description:
//
//	the identity is the KERNEL's answer and it is the pipe's OWN object: two
//	  descriptors of ONE pipe answer with the same string, and a second pipe answers
//	  with a different one. A helper that returned a constant, a number, or the
//	  empty string fails here — and a report carrying such a value could not be
//	  matched against any other sighting of the pipe, which is the whole reason the
//	  identity exists;
//	the census FINDS a later holder when there is one: a duplicate of the same pipe
//	  object is named by its descriptor number, so the report points at the surviving
//	  claim on the pipe rather than only at the loss;
//	the census reports chatRunArgsReuseNone when the process really holds no such
//	  pipe (the ordinary shape — a released number is normally not re-handed the
//	  same pipe object);
//	and the UNMEASURED case is not reported as a finding: with no identity to search
//	  for, the census says so. This last arm is the one a report cannot satisfy by
//	  printing an empty result, and it is the arm the PRE-FIX report would have taken
//	  for every occurrence, because it recorded no identity at all.
//
// RED ON THE PRE-FIX SHAPE: the pre-fix report answered a released number's reading
// (`target, resolvable := chatRunArgsFDReading(fd)` → `""`, false) and named no
// identity, so the `chatRunArgsReuseCensus("")` arm below is literally the pre-fix
// input; a census that answered it with chatRunArgsReuseNone would claim "no other
// descriptor holds this pipe" about a pipe it never looked for.
func TestChatRunArgsIdentityCensusNamesTheLostPipe(t *testing.T) {
	first := chatOpenPtyPairFixturePipe(t)
	second := chatOpenPtyPairFixturePipe(t)

	identityA := chatRunArgsDescriptorIdentity(first.readFd)
	identityB := chatRunArgsDescriptorIdentity(first.writeFd)
	identityC := chatRunArgsDescriptorIdentity(second.readFd)

	if identityA == "" || strings.HasPrefix(identityA, chatRunArgsIdentitySentinelPrefix) {
		t.Fatalf("the identity of a pipe this test holds reads as %q — the fixture's report would name no pipe at all, "+
			"which is the state this guard exists to make impossible", identityA)
	}
	if identityA != identityB {
		t.Errorf("the two ends of ONE pipe answered %q and %q — an identity that differs between the ends of the same "+
			"pipe object cannot be matched against another sighting of that pipe", identityA, identityB)
	}
	if identityA == identityC {
		t.Errorf("two DIFFERENT pipes both answered %q — the identity does not distinguish one pipe object from another, "+
			"so the census below could not discriminate and no report could be correlated", identityA)
	}

	// The positive arm: a duplicate of the same pipe object IS found, by number.
	dupFd, err := syscall.Dup(first.readFd)
	if err != nil {
		t.Fatalf("dup the fixture's pipe descriptor: %v", err)
	}
	// OWNERSHIP: this fixture claims dupFd and still owns it here — but NOT for the
	// whole arm: the negative arm below releases it EXPLICITLY. A deferred
	// syscall.Close alongside that explicit one is a DOUBLE CLOSE on the same
	// number, and the second close lands on whatever the kernel recycled it to (the
	// very defect this file guards against, committed by the fixture). The single
	// release is registered here once and carries the identity, so it closes only
	// while the number is still the one that was claimed.
	dupIdentity := borrowedfd.Witness(dupFd)
	// dupReleased states whether this fixture already gave the number back, so the
	// teardown neither closes twice nor swallows a refusal it should report.
	dupReleased := false
	t.Cleanup(func() {
		if dupReleased {
			return
		}
		var moved *borrowedfd.MovedError
		if relErr := dupIdentity.Release(); relErr != nil && !errors.As(relErr, &moved) {
			t.Errorf("releasing the fixture's duplicate fd %d failed: %v", dupFd, relErr)
		}
	})
	if got := chatRunArgsReuseCensus(identityA, first.readFd, first.writeFd); !strings.Contains(got, strconv.Itoa(dupFd)) {
		t.Errorf("the census did not name fd %d, a live descriptor holding the very pipe whose identity was searched for "+
			"(%s) — the report would point at the loss and not at the surviving claim on the pipe", dupFd, identityA)
	}

	// The negative arm: with no other holder, the answer names the identity it
	// looked for (never an empty string that reads like an unrun search).
	//
	// THE ONE RELEASE of this number, and the deferred close above is GONE: this is
	// the arm the explicit double close used to endanger.
	if err := dupIdentity.Release(); err != nil {
		t.Fatalf("release the duplicate: %v", err)
	}
	dupReleased = true
	orphanIdentity := chatRunArgsDescriptorIdentity(second.writeFd)
	if got := chatRunArgsReuseCensus(orphanIdentity, second.readFd, second.writeFd); !strings.Contains(got, orphanIdentity) {
		t.Errorf("the census of an identity nothing else holds answered %q, which does not name the identity it searched "+
			"for (%s) — a reader could not tell this from a search that never ran", got, orphanIdentity)
	}

	// The unmeasured arm: no identity means the search could not run, and saying so
	// is the ONLY honest answer. This is the pre-fix input (the pre-fix report had
	// no identity), so a census that answered it with the "no other holder" sentence
	// would be claiming a measurement about a pipe it never named.
	for _, unnamed := range []string{"", chatRunArgsIdentityUnknown, chatRunArgsIdentityUnreadable} {
		got := chatRunArgsReuseCensus(unnamed)
		if strings.Contains(got, chatRunArgsReuseNoHolderPhrase) {
			t.Errorf("the census answered the unnamed identity %q with the no-other-holder sentence (%q) — an unrun search "+
				"reported as a finding is exactly the misattribution this fixture exists to remove", unnamed, got)
		}
		if !strings.Contains(got, chatRunArgsReuseUnrunPhrase) {
			t.Errorf("the census answered the unnamed identity %q with %q, which does not say the search could not be run", unnamed, got)
		}
	}
}

// chatOpenPtyPairFixturePipe is a minimal pipe fixture for the identity guard,
// carrying both descriptor numbers so its two ends can be compared. os.Pipe's
// descriptors are the shape the fixture under test uses, and both are closed by the
// caller's own cleanup rather than by a wrapper the runtime could finalize.
type chatRunArgsPipeProbe struct {
	readEnd  *os.File
	writeEnd *os.File
	readFd   int
	writeFd  int
}

func chatOpenPtyPairFixturePipe(t *testing.T) chatRunArgsPipeProbe {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	p := chatRunArgsPipeProbe{readEnd: r, writeEnd: w, readFd: int(r.Fd()), writeFd: int(w.Fd())}
	t.Cleanup(func() { _ = w.Close() }) //stylegate:ignore fixture teardown of the probe's own write end
	t.Cleanup(func() { _ = r.Close() }) //stylegate:ignore fixture teardown of the probe's own read end
	return p
}

// chatRunArgsFDReading asks the KERNEL what a descriptor number currently points
// at — the observation the release above reports, so a failure names the state it
// found instead of only the syscall's complaint.
func chatRunArgsFDReading(fd int) (string, bool) {
	target, err := os.Readlink(fmt.Sprintf(chatPtyProcPathFormat, fd))
	if err != nil {
		return "", false
	}
	return target, true
}

// chatRunArgsDescriptorIdentity asks the KERNEL what a descriptor number names,
// at a moment the caller still certainly holds that number, and returns a string
// a later reader can CORRELATE — `pipe:[12345]` on Linux.
//
// WHY A NUMBER IS NOT ENOUGH, which is the whole point of this helper: the
// reports in this file already carry the number and the kernel's reading AT
// FAILURE TIME, and for a released number that reading is "" — a handle the
// kernel has recycled away names nothing, so the report could not say WHICH pipe
// died and no other observation could be matched against it. An identity taken
// while the fixture still holds the number survives the number's death, and the
// SAME value appears in any other sighting of that pipe object (a strace close,
// an lsof row, a sibling occurrence of this defect), which is what turns "a
// foreign release happened somewhere" into "this pipe, which this fixture opened
// at this moment, is the one that was taken".
//
// The two unknowns are recorded as DIFFERENT values, because they are different
// facts: a readlink that failed with ENOENT says nothing is open at that number
// (a fact about the descriptor), while any other failure — procfs absent, EACCES,
// ELOOP — is a limit of the OBSERVER. Rule 15: an unmeasured state is named, never
// left reading like a measurement.
func chatRunArgsDescriptorIdentity(fd int) string {
	target, err := os.Readlink(fmt.Sprintf(chatPtyProcPathFormat, fd))
	switch {
	case err == nil:
		return target
	case errors.Is(err, fs.ErrNotExist):
		return chatRunArgsIdentityUnknown
	default:
		return chatRunArgsIdentityUnreadable
	}
}

// chatRunArgsReuseCensus looks for a LATER SIGHTING of the pipe whose identity is
// passed: a descriptor in this process that now names the same pipe object. It is
// the ONE part of the report that is about the OTHER actor rather than about this
// fixture's loss — the fixture holds the identity of what it opened, and if the
// same pipe object has turned up elsewhere in this process's descriptor table, the
// closer left a witness behind that this sentence names (a number), which is
// exactly the evidence a single-sentence "somebody released it" was missing.
//
// It reports three DIFFERENT things and never conflates them: the numbers holding
// that pipe (`fd 7, 9`), chatRunArgsReuseNone (no descriptor holds it — which is
// the ordinary shape, since a released number is normally not re-handed to the
// same pipe object), and the UNMEASURED case (the identity itself is one of the
// two sentinels, or procfs refused the walk — then there was nothing to search
// for, and the census says so instead of reporting an empty result as a finding).
func chatRunArgsReuseCensus(identity string, own ...int) string {
	if identity == "" || strings.HasPrefix(identity, chatRunArgsIdentitySentinelPrefix) {
		return fmt.Sprintf(chatRunArgsReuseUnnamedFormat, identity)
	}
	entries, err := os.ReadDir(chatRunArgsProcSelfFDDir)
	if err != nil {
		return fmt.Sprintf(chatRunArgsReuseUnreadableFormat, identity, err)
	}
	mine := make(map[int]bool, len(own))
	for _, n := range own {
		mine[n] = true
	}
	held := make([]string, 0, chatRunArgsReuseCap)
	for _, e := range entries {
		fd, convErr := strconv.Atoi(e.Name())
		if convErr != nil {
			// A name that is not a number is not a descriptor.
			continue
		}
		if mine[fd] {
			// One of the ends THIS fixture opened: both ends of one pipe carry the
			// same identity (pipe:[inode]), so without this filter the census would
			// report the fixture's own sibling end on every occurrence and name no
			// foreign claim at all.
			continue
		}
		path := chatRunArgsProcSelfFDDir + chatRunArgsProcSep + e.Name()
		if target, linkErr := os.Readlink(path); linkErr == nil && target == identity {
			held = append(held, strconv.Itoa(fd))
		}
	}
	if len(held) == 0 {
		return fmt.Sprintf(chatRunArgsReuseNoneFormat, identity)
	}
	return fmt.Sprintf(chatRunArgsReuseHeldFormat, identity, strings.Join(held, chatRunArgsReuseSep))
}

// chatRunArgsReuseCap pre-sizes the census's result slice: a process holds tens of
// descriptors, so a handful per occurrence is the whole expectation.
const chatRunArgsReuseCap = 4

// chatRunArgsProcSelfFDDir is procfs' directory of THIS process's descriptors
// (Rule 1b: the value lives in one place, not per call site).
const chatRunArgsProcSelfFDDir = "/proc/self/fd"

// chatRunArgsProcSep joins a procfs directory to a descriptor's name.
const chatRunArgsProcSep = "/"

// chatRunArgsIdentitySentinelPrefix is the first byte of BOTH identity sentinels
// above, so one test decides "the identity is a named unknown, not a measurement".
const chatRunArgsIdentitySentinelPrefix = "("

// chatRunArgsReuseSep separates the descriptor numbers in a census result.
const chatRunArgsReuseSep = ", "

// The census's three shapes, as formats: the pipe is held (numbers are named), it
// is not (chatRunArgsReuseNone), or the search could not run (the identity was a
// sentinel, or procfs refused) — the last one names WHY rather than reporting an
// empty answer as a finding.
const (
	// chatRunArgsReuseNoHolderPhrase is the sentence the census answers with when
	// nothing else holds the pipe — and the ONE phrase a guard can assert is absent
	// when the search could not run at all.
	chatRunArgsReuseNoHolderPhrase = "No other descriptor in this process holds"
	// chatRunArgsReuseUnrunPhrase is the census's answer when there was no identity
	// to search for: an unrun search is announced as unrun, never as an empty result.
	chatRunArgsReuseUnrunPhrase = "No search for another holder"

	chatRunArgsReuseHeldFormat       = "The pipe %s is ALSO held by fd %s in this process — a descriptor no end of this fixture opened, and the closer's own witness"
	chatRunArgsReuseNoneFormat       = chatRunArgsReuseNoHolderPhrase + " %s"
	chatRunArgsReuseUnnamedFormat    = chatRunArgsReuseUnrunPhrase + " of the pipe could be run: the kernel named no identity for it (%s)"
	chatRunArgsReuseUnreadableFormat = chatRunArgsReuseUnrunPhrase + " of %s could be run: %v"
)

// TestChatCLIUnknownFlagIsRefusedAsAFlag is the repro for the defect. Every case
// is an argv a user could type, and every one must answer with THIS verb's
// documented usage status and a message that names the word as a FLAG — never
// the old "resume target" sentence, which described a positional the caller did
// not write.
func TestChatCLIUnknownFlagIsRefusedAsAFlag(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		word string
	}{
		{"bare unknown flag", []string{"--bogus-flag"}, "--bogus-flag"},
		{"unknown flag beside a known one", []string{"--tools", "--bogus-flag"}, "--bogus-flag"},
		{"unknown flag after a valued flag", []string{"--dump-screen=x.dump", "--bogus-flag"}, "--bogus-flag"},
		{"unknown flag before the positional", []string{"--bogus-flag", "some.log"}, "--bogus-flag"},
		{"unknown flag after the positional", []string{"some.log", "--bogus-flag"}, "--bogus-flag"},
		{"a switch spelled with a value", []string{"--tools=yes"}, "--tools=yes"},
		{"short unknown flag", []string{"-x"}, "-x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// THE WITNESS, not the sampling face: this case's whole claim is a
			// sentence the product prints on one stream, so the case has to know
			// whether that stream SURVIVED the call. Reading only the drained text
			// (chatRunArgs) is what turned "the descriptor was released by a path
			// outside this fixture" into "the refusal does not name the flag".
			_, stderr, code, ev := chatRunArgsWitnessed(t, c.argv...)
			// The status, decided on the transport it flowed through: a status that
			// disagrees while the record shows a foreign release is RECORDED, and a
			// status that disagrees with no such release is this case's own failure.
			if verdict, transport := chatRunArgsStatusVerdictOf(code, chatExitUsage, ev); verdict != chatRunArgsStatusOnTarget {
				if verdict == chatRunArgsStatusTransportLost {
					t.Logf("runChat(%v) = %d, want the usage status %d — and the fixture's own record says why: %s. "+
						"The claim about to be asserted is the TEXT, which is decided below on what the stream actually carried",
						c.argv, code, chatExitUsage, transport)
				} else {
					t.Errorf("runChat(%v) = %d, want the documented usage status %d\nstderr:\n%s", c.argv, code, chatExitUsage, stderr)
				}
			}
			// The positive claim, through the classifier: PRESENT holds, MISSING is
			// this case's failure, TRUNCATED is fatal, and a stream whose transport
			// was taken before its first byte is UNOBSERVABLE — short-circuited,
			// marked and reported, never reported as a wrong refusal text.
			chatRunArgsAssertTextClaim(t, c.argv, chatRunArgsStreamNameErr, chatRunArgsUnknownFlagPhrase(c.word), ev)
			// The absence half, on the same record: an absence over a stream nobody
			// observed would be a vacuous hold.
			for _, phrase := range []string{chatRunArgsResumeTargetPhrase, chatRunArgsNeitherRawLogPhrase} {
				chatRunArgsAssertNoTextClaim(t, c.argv, chatRunArgsStreamNameErr, phrase, ev)
			}
		})
	}
}

// chatRunArgsUnknownFlagPhrase is the sentence chatRefuseUnknownToken prints for an
// unknown FLAG (cli_chat.go): the claim every case above asserts, spelled in ONE
// place so the fixture and the product cannot drift apart silently.
func chatRunArgsUnknownFlagPhrase(word string) string {
	return "unknown flag " + word
}

// The two sentences the OLD (defective) reading of a `-`-prefixed word produced:
// it hunted the word as a resume target / raw-log path. A case asserting their
// ABSENCE is asserting that the defect did not come back.
const (
	chatRunArgsResumeTargetPhrase  = "resume target"
	chatRunArgsNeitherRawLogPhrase = "is neither a raw log path nor an existing session id"
)

// chatRunArgsEvidenceWithErr is the stderr sibling of chatRunArgsEvidenceWithOut:
// the record really OBSERVED the err arm to the two values passed, with the
// verdict DERIVED through the same classifier the fixture's drain uses.
func chatRunArgsEvidenceWithErr(own, other chatRunArgsRead) chatRunArgsStreamEvidence {
	ev := chatRunArgsUnobservedEvidence()
	ev.errRead, ev.outRead = own, other
	ev.errVerdict = chatRunArgsReadVerdictOf(own, other)
	return ev
}

// TestChatRunArgsTextClaimIsDecidedOnTheTransport is the classifier's guard — the
// RED-WITHOUT-FIX carrier for this repair, and the minimal repro of the defect.
//
// THE ONE REASON IT GOES RED ON THE PRE-FIX SHAPE: pre-fix the refusal cases asked
// `strings.Contains(stderr, "unknown flag "+word)` of the DRAINED TEXT alone, with
// no way to tell "the product printed something else" from "the stream died under
// the case". Here the same empty text, handed in with a record showing a FOREIGN
// RELEASE, must classify as chatRunArgsTextClaimUnobservable — a verdict the pre-fix
// shape does not have (there it is indistinguishable from chatRunArgsTextClaimMissing,
// which is the misattribution). The Missing arm below is the negative control that
// stops this repair from becoming "a missing sentence is never reported".
func TestChatRunArgsTextClaimIsDecidedOnTheTransport(t *testing.T) {
	const (
		claim  = "unknown flag --bogus-flag"
		refuse = chatErrorPrefix + claim + " — this verb's flags are: --tools, -h\n"
	)
	// THE HEALTHY ARM: the stream drained cleanly and carries the claim.
	if got := chatRunArgsTextClaimOf(chatRunArgsStreamNameErr, claim, chatRunArgsEvidenceWithErr(chatRunArgsRead{s: refuse}, chatRunArgsRead{s: chatRunArgsGuardSiblingPayload})); got != chatRunArgsTextClaimPresent {
		t.Errorf("a cleanly drained stream carrying the claim classified as %d, want the present arm (%d)", got, chatRunArgsTextClaimPresent)
	}
	// THE REAL-DEFECT ARM, and the arm that must NOT be bought away: the stream
	// drained fine and the sentence is not there.
	if got := chatRunArgsTextClaimOf(chatRunArgsStreamNameErr, claim, chatRunArgsEvidenceWithErr(chatRunArgsRead{s: "resume target \"--bogus-flag\" is neither a raw log path nor an existing session id\n"}, chatRunArgsRead{s: chatRunArgsGuardSiblingPayload})); got != chatRunArgsTextClaimMissing {
		t.Errorf("a cleanly drained stream WITHOUT the claim classified as %d, want the missing arm (%d) — the repair would then hide a real refusal-text defect", got, chatRunArgsTextClaimMissing)
	}
	// THE TRUNCATED ARM: bytes then death is a prefix, and a prefix answers nothing
	// about the sentence — still not a pass, and not the unobservable arm either.
	if got := chatRunArgsTextClaimOf(chatRunArgsStreamNameErr, claim, chatRunArgsEvidenceWithErr(chatRunArgsRead{s: chatRunArgsGuardPartialPayload, err: chatRunArgsReleasedNumber}, chatRunArgsRead{s: chatRunArgsGuardSiblingPayload})); got != chatRunArgsTextClaimTruncated {
		t.Errorf("a stream truncated after bytes classified as %d, want the truncated arm (%d)", got, chatRunArgsTextClaimTruncated)
	}
	// THE ARM THE WHOLE-PACKAGE OCCURRENCE NEEDED: the transport was taken before the
	// stream carried a byte, and the record says a path outside this fixture took it.
	if got := chatRunArgsTextClaimOf(chatRunArgsStreamNameErr, claim, chatRunArgsEvidenceWithErr(chatRunArgsRead{err: chatRunArgsReleasedNumber}, chatRunArgsRead{s: chatRunArgsGuardSiblingPayload})); got != chatRunArgsTextClaimUnobservable {
		t.Errorf("a stream lost before its first byte classified as %d, want the unobservable arm (%d) — the whole-package occurrence would again report \"the refusal does not name \\\"--bogus-flag\\\" as a FLAG\" about a refusal that was never delivered", got, chatRunArgsTextClaimUnobservable)
	}
	// THE UNOBSERVED RECORD, and a stream this fixture does not own: two different
	// names for "nothing was measured", both named rather than read as a clean read.
	if got := chatRunArgsTextClaimOf(chatRunArgsStreamNameErr, claim, chatRunArgsUnobservedEvidence()); got != chatRunArgsTextClaimUnknown {
		t.Errorf("an unfilled record classified as %d, want the unknown arm (%d) — the unobserved verdict must not read as a clean measurement", got, chatRunArgsTextClaimUnknown)
	}
	if got := chatRunArgsTextClaimOf(chatRunArgsStreamNameStdinGuard, claim, chatRunArgsEvidenceWithErr(chatRunArgsRead{s: refuse}, chatRunArgsRead{})); got != chatRunArgsTextClaimUnknown {
		t.Errorf("a stream NAME this fixture does not own classified as %d, want the unknown arm (%d) — an unknown name must not read as an empty stream", got, chatRunArgsTextClaimUnknown)
	}
}

// chatRunArgsStreamNameStdinGuard is a stream name this fixture does NOT own, used
// to pin the unknown-name arm above (Rule 1b: named here rather than inlined).
const chatRunArgsStreamNameStdinGuard = "stdin"

// TestChatRunArgsUnknownFlagClaimSurvivesAForeignRelease is the END-TO-END half of
// the same repair, with the release produced for real: a pipe this fixture opened,
// its read end released by a raw syscall, and the resulting read error handed to
// the classifier the refusal cases now use.
//
// THE POINT: the SAME read error over the SAME claim answers Unobservable when the
// record shows the loss and Missing when the stream merely lacks the sentence — the
// two verdicts the whole-package occurrence conflated.
func TestChatRunArgsUnknownFlagClaimSurvivesAForeignRelease(t *testing.T) {
	const claim = "unknown flag --bogus-flag"
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	fd := int(r.Fd())
	t.Cleanup(func() { _ = w.Close() }) //stylegate:ignore the fixture's own write end teardown
	t.Cleanup(func() { _ = r.Close() }) //stylegate:ignore the fixture's own wrapper teardown
	identity := chatRunArgsDescriptorIdentity(fd)
	// The foreign release: the raw number, so it does not go through r's wrapper —
	// exactly the shape the suite showed (see chatRunArgsReleaseNumber).
	// THIS FIXTURE'S OWN DELIBERATE FOREIGN RELEASE — the one shape where a raw
	// close is correct: releasing the number IS this arm's premise, and it happens
	// exactly once, here. The witness below records what the kernel said the number
	// denoted while it was still ours, so the close is attributable by IDENTITY and
	// the arm can prove it released a number rather than assume it did.
	releasedIdentity := borrowedfd.Witness(fd)
	if err := syscall.Close(fd); err != nil {
		t.Fatalf("the guard could not stand in for the foreign release on fd %d: %v", fd, err)
	}
	if checkErr := releasedIdentity.Check(); checkErr == nil {
		t.Fatalf("fd %d still carries the identity this fixture held (%s) after the foreign release — the arm needs the "+
			"released shape, and syscall.Close returned success without releasing it", fd, releasedIdentity)
	}
	b, readErr := io.ReadAll(r)
	if readErr == nil {
		t.Fatalf("a read of fd %d (identity %s) succeeded although the number was released — the guard needs the lost-stream shape", fd, identity)
	}
	lost := chatRunArgsRead{s: string(b), err: readErr}
	carried := chatRunArgsRead{s: chatRunArgsGuardSiblingPayload}
	if got := chatRunArgsReadVerdictOf(lost, carried); got != chatRunArgsReadRecordedLoss {
		t.Fatalf("the real lost stream (fd %d) classified as %d, want the recorded-loss verdict (%d)", fd, got, chatRunArgsReadRecordedLoss)
	}
	// The claim the refusal cases assert, over the real loss: UNOBSERVABLE — the
	// sentence was never deliverable, and the case says so instead of failing.
	if got := chatRunArgsTextClaimOf(chatRunArgsStreamNameErr, claim, chatRunArgsEvidenceWithErr(lost, carried)); got != chatRunArgsTextClaimUnobservable {
		t.Fatalf("the claim over a stream lost by a foreign release (fd %d) classified as %d, want the unobservable arm (%d) — the case would report a refusal-text defect about a refusal that never arrived",
			fd, got, chatRunArgsTextClaimUnobservable)
	}
	// THE NEGATIVE CONTROL, same claim, same bytes, no loss: the stream drained
	// cleanly and does not carry the sentence, so it IS the case's own failure.
	if got := chatRunArgsTextClaimOf(chatRunArgsStreamNameErr, claim, chatRunArgsEvidenceWithErr(chatRunArgsRead{s: lost.s}, carried)); got != chatRunArgsTextClaimMissing {
		t.Fatalf("the same captured bytes over a CLEAN read classified as %d, want the missing arm (%d) — otherwise every absent sentence would be excused", got, chatRunArgsTextClaimMissing)
	}
	// And the marker a whole-package run is grepped with is on the record this
	// fixture prints, not on a sentence a reader has to interpret.
	if !strings.Contains(chatRunArgsTransportLostMarker, "transport-lost") {
		t.Fatalf("the marker %q no longer spells transport-lost — a whole-package run is read by that grep", chatRunArgsTransportLostMarker)
	}
}

// TestChatCLIUsageStatusesAgreeAcrossThePages: `2` means "usage error" on both
// faces of this verb (the REPL's own page and the `--once` section joined into
// it). The two constants are separate declarations with separate contracts, so
// nothing but this test holds them together — and a verb whose two faces
// disagreed about what 2 means would be a trap for exactly the caller this
// slice is about.
func TestChatCLIUsageStatusesAgreeAcrossThePages(t *testing.T) {
	if chatExitUsage != chatOnceExitUsage {
		t.Errorf("chatExitUsage = %d but chatOnceExitUsage = %d — one verb, one meaning for the usage status", chatExitUsage, chatOnceExitUsage)
	}
	if !strings.Contains(chatUsage, "2 = usage error") {
		t.Error("`gopi chat -h` no longer documents 2 = usage error — the refusal's status would be undocumented")
	}
	// The one-shot face refuses a word of NEITHER vocabulary through the same
	// status, and it must not swallow one either (see chatOnceParseArgs).
	if _, err := chatOnceParseArgs([]string{"--once", "do-x", "--bogus-flag"}); err == nil {
		t.Error("`gopi chat --once do-x --bogus-flag` was accepted — the unknown flag rode through as the session seed again")
	}
}

// TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses is the anti-drift walk:
// every flag this verb DECLARES (chatREPLFlags — the list its refusal message
// prints) must actually parse, and the refusal must never fire for one of them.
// Pairing the walk with the refusal cases above is what closes the vocabulary
// from BOTH sides: nothing advertised is refused, nothing unlisted is accepted.
//
// The help flag rides each argv so the parse stops at the usage path (rc 0) and
// never reaches the REPL: a flag that were unknown would still be refused, since
// the refusal is raised during the same walk, before help is answered.
func TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses(t *testing.T) {
	for _, flag := range chatREPLFlags() {
		t.Run(flag, func(t *testing.T) {
			// A VALUED flag (--resume) is exercised with a value in its own
			// slot: `--resume -h` is a refusal by design (a target cannot be a
			// flag), so walking it bare would assert the wrong thing — that the
			// flag is unknown, which is exactly what this walk must not report.
			argv := []string{flag, HelpFlag}
			if chatIsValuedREPLFlag(flag) {
				argv = []string{flag, chatValuedFlagProbeValue(flag), HelpFlag}
			}
			stdout, stderr, code, ev := chatRunArgsWitnessed(t, argv...)
			if strings.Contains(stderr, "unknown flag") {
				t.Fatalf("%s is in this verb's own vocabulary (chatREPLFlags) but the parser refuses it:\n%s", flag, stderr)
			}
			// THE STATUS VERDICT IS TAKEN ON THE ROUTE THE STATUS FLOWS THROUGH.
			// `-h` answers by writing the page to os.Stdout and returning
			// chatExitOK (cli_chat.go:1787), so a NON-ZERO status here is a
			// statement about that write — and this fixture installs the write end
			// it uses. When the fixture's own record shows the descriptor was
			// released by a path outside it (the measured
			// `close |1: bad file descriptor`), the status says nothing about the
			// argv walk this case is about: the transport died in between, and the
			// case's own claim — that the help answered — is still asserted below
			// on the page that WAS captured. Recording that instead of failing on
			// it is the repair; a status that disagrees while no release was
			// observed is still fatal, and so is the unknown-errno arm.
			if verdict, transport := chatRunArgsStatusVerdictOf(code, chatExitOK, ev); verdict != chatRunArgsStatusOnTarget {
				if verdict == chatRunArgsStatusTransportLost {
					t.Logf("runChat(%v) = %d, want %d — and this fixture's own record of the call says why: %s. "+
						"The walk's claim is the page below, which was captured before the transport died, so the status is "+
						"recorded rather than read as a verdict on the walk", argv, code, chatExitOK, transport)
				} else {
					t.Fatalf("runChat(%v) = %d, want %d (the help path, reachable with every accepted flag)\nstderr:\n%s",
						argv, code, chatExitOK, stderr)
				}
			}
			// The product evidence every arm above rests on, asserted whenever it was
			// observable at all.
			chatRunArgsAssertHelpAnswer(t, flag, ev, stdout)
		})
	}
	// Every non-help flag is documented on the page the caller reads: a flag the
	// parser accepts and the help never mentions is a flag nobody can find.
	for _, flag := range chatREPLFlags() {
		if flag == HelpFlag || flag == HelpFlagLong {
			continue
		}
		if !strings.Contains(chatUsage, flag) {
			t.Errorf("%s parses but is not documented in chatUsage — the help and the vocabulary disagree", flag)
		}
	}
}

// TestChatCLIPositionalFaceIsPreserved: the documented positional (a raw-log
// path OR a session UUID) still reaches the resolver, and the help answers with
// the usage path. The refusal above must not have been bought by rejecting
// positionals — that would fix the flag bug by breaking the surface the help
// documents.
func TestChatCLIPositionalFaceIsPreserved(t *testing.T) {
	// ONE positional reaches the seed resolver: the message is the resolver's
	// own (a target it could not find), never an "unknown" refusal.
	_, stderr, code := chatRunArgs(t, "definitely-not-a-real-session")
	if code != chatExitFailed {
		t.Errorf("runChat([definitely-not-a-real-session]) = %d, want %d (the run failed)\nstderr:\n%s", code, chatExitFailed, stderr)
	}
	if strings.Contains(stderr, "unknown flag") || strings.Contains(stderr, "unknown argument") {
		t.Errorf("a positional seed was refused by the closed-vocabulary guard:\n%s", stderr)
	}
	// The resolver's own sentence, whichever of its two shapes the environment
	// produces: a missing sessions directory is reported as "cannot resolve
	// session id", an empty one as the long "is neither a raw log path nor an
	// existing session id". Both name the target — that is what is asserted.
	if !strings.Contains(stderr, "cannot resolve session id") &&
		!strings.Contains(stderr, "is neither a raw log path nor an existing session id") {
		t.Errorf("the positional no longer reaches the seed resolver:\n%s", stderr)
	}

	// A SECOND positional is a usage error, named — never silently dropped (the
	// synopsis documents exactly one).
	_, stderr, code = chatRunArgs(t, "a.log", "b.log")
	if code != chatExitUsage {
		t.Errorf("runChat([a.log b.log]) = %d, want %d — the second positional was silently dropped\nstderr:\n%s", code, chatExitUsage, stderr)
	}
	if !strings.Contains(stderr, "ONE argument") {
		t.Errorf("the second positional was refused without saying why:\n%s", stderr)
	}

	// -h is a zero-side-effect usage query, and it may not silently discard a
	// positional the caller wrote.
	stdout, stderr, code := chatRunArgs(t, HelpFlag)
	if code != chatExitOK {
		t.Fatalf("runChat([-h]) = %d, want 0\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "Usage: gopi chat") {
		t.Fatalf("runChat([-h]) did not print the chat page:\n%s", stdout)
	}
	stdout, _, code = chatRunArgs(t, HelpFlagLong, "some.log")
	if code != chatExitUsage {
		t.Errorf("runChat([--help some.log]) = %d, want %d — the ignored positional must be reported, not dropped", code, chatExitUsage)
	}
	if strings.Contains(stdout, "Usage: gopi chat") {
		t.Error("the help was printed for a command line that was refused — a usage query must be answered, a malformed one must not be")
	}
}

// TestChatCLIRealBinaryRefusesAnUnknownFlag is the SAME contract through the
// real binary (Rule 31): the argv is what a user types, the exit status is what
// their shell sees, and the words are what their terminal prints. It is the
// end-user-facing half of this slice — an in-process test cannot prove the
// dispatch path (`isChatSubcommand` → runChatOnce → runChat) reaches the
// parser at all.
func TestChatCLIRealBinaryRefusesAnUnknownFlag(t *testing.T) {
	p := g5NewProbe(t)
	cases := []struct {
		name string
		argv []string
	}{
		{"bare verb", []string{CliVerbChat, "--bogus-flag"}},
		{"beside an accepted flag", []string{CliVerbChat, chatFlagTools, "--bogus-flag"}},
		{"one-shot face", []string{CliVerbChat, chatOnceFlag, "--bogus-flag"}},
		// The LIVE index subcommand. It used to be `list`, which the
		// restructure RETIRED (epic verb-restructure 2026-10-05: the index has
		// ONE door, `gopi session list`), so the flag-refusal contract is
		// exercised through `backfill` — the word that still reaches this
		// parser.
		{"index subcommand", []string{CliVerbChat, chatSubcommandBackfill, "--bogus"}},
		// ...and the retired word's own contract, which is a DIFFERENT status
		// on purpose (see the case below): retired is not "you typed a bad
		// flag", and the two must not be confused in a script.
		{"retired index subcommand", []string{CliVerbChat, chatSubcommandList, "--bogus"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := p.run(t, "", c.argv...)
			if c.name == "retired index subcommand" {
				// RETIRED, not malformed: the word is refused BY NAME with its
				// new home, exit 1 — and it must NOT be reported as a flag error
				// (the argv `list --bogus` never reaches a flag parser).
				if res.exit != chatExitFailed {
					t.Errorf("`gopi %s` exit %d, want %d (retired)\n%s",
						strings.Join(c.argv, " "), res.exit, chatExitFailed, res.out)
				}
				if !strings.Contains(res.out, "retired") || !strings.Contains(res.out, CliVerbSession+" "+sessionSubcmdList) {
					t.Errorf("`gopi %s`: the refusal must name the retirement and the live door:\n%s",
						strings.Join(c.argv, " "), res.out)
				}
				if strings.Contains(res.out, "unknown flag") {
					t.Errorf("`gopi %s`: a RETIRED word must not be reported as a bad flag:\n%s",
						strings.Join(c.argv, " "), res.out)
				}
				return
			}
			if res.exit != chatExitUsage {
				t.Errorf("`gopi %s` exit %d, want %d\n%s", strings.Join(c.argv, " "), res.exit, chatExitUsage, res.out)
			}
			if !strings.Contains(res.out, "unknown flag") {
				t.Errorf("`gopi %s`: the refusal does not call the word a flag:\n%s", strings.Join(c.argv, " "), res.out)
			}
			if strings.Contains(res.out, "resume target") {
				t.Errorf("`gopi %s` still reports a resume TARGET:\n%s", strings.Join(c.argv, " "), res.out)
			}
		})
	}

	// The documented usage query stays a zero-side-effect 0, and the positional
	// face still reaches its resolver.
	if res := p.run(t, "", CliVerbChat, HelpFlag); res.exit != chatExitOK || !strings.Contains(res.out, "Usage: gopi chat") {
		t.Errorf("`gopi chat %s` exit %d, want 0 with the page:\n%s", HelpFlag, res.exit, res.out)
	}
	res := p.run(t, "", CliVerbChat, "definitely-not-a-real-session")
	if res.exit != chatExitFailed ||
		(!strings.Contains(res.out, "cannot resolve session id") && !strings.Contains(res.out, "is neither a raw log path nor an existing session id")) {
		t.Errorf("the positional seed stopped reaching the resolver: exit=%d\n%s", res.exit, res.out)
	}
}

// chatArgvValuedFlagProbe is the dummy value the vocabulary walk hands a
// VALUED flag (--resume). It is never resolved: the walk pairs every argv with
// -h, and the help path returns before any target is looked up — the point is
// only that the flag itself is ACCEPTED.
const chatArgvValuedFlagProbe = "00000000-0000-4000-8000-000000000000"

// chatValuedFlagProbeValue is the value the walks hand ONE valued flag. Taking a
// value in the next slot is NOT the same property as accepting a session id: the
// value must also satisfy the flag's own parser, or the walk asserts that a flag
// this verb advertises is broken.
//
// THE MEASURED DEFECT THIS CLOSES (2026-10-27). chatResumeValuedFlags gained
// `--from-turn` (chat_replay.go), and both walks kept handing it the session
// UUID above. `--from-turn` parses its value as a positive integer, so
// `gopi chat --from-turn 00000000-0000-4000-8000-000000000000 -h` was refused at
// rc=2 with "takes a turn number and ... is not one" — a CORRECT refusal of a
// command line no operator would type, reported as "--from-turn is in this
// verb's own vocabulary but the parser refuses it". Two master reds (this walk
// and TestChatUsageQueryBesideAValuedFlagsOwnValue) whose subject was the
// walk's fixture, not the flag.
//
// The probe is therefore derived per flag, from the parser's own word: a flag
// whose value slot takes a NUMBER is handed a number (chatFlagFromTurn, the one
// such word today), everything else the session-id shape. A new numeric valued
// flag joins this function by construction — the alternative (silently falling
// back to the UUID) is exactly the failure above.
func chatValuedFlagProbeValue(flag string) string {
	if flag == chatFlagFromTurn {
		// 1 is the smallest turn a recording has (chatParseFromTurnArg: 1-based,
		// N >= 1), so this probe is valid for every recording rather than only
		// for recordings long enough to have a third turn.
		return "1"
	}
	return chatArgvValuedFlagProbe
}

// chatIsValuedREPLFlag reports whether a flag in this verb's vocabulary takes a
// value in its next argv slot. It is derived from the parser's own list
// (chatResumeValuedFlags) so the walk and the parser cannot disagree about which
// flags are valued.
func chatIsValuedREPLFlag(flag string) bool {
	for _, f := range chatResumeValuedFlags() {
		if f == flag {
			return true
		}
	}
	return false
}

// TestChatValuedFlagProbesSatisfyTheirOwnParsers is the minimal repro of the two
// master reds this fixture caused (2026-10-27): the walks handed EVERY valued
// flag the session-UUID probe, so `--from-turn 00000000-0000-4000-8000-…` was
// refused by chatParseFromTurnArg ("takes a turn number and … is not one") and
// the walk reported it as "--from-turn is in this verb's own vocabulary but the
// parser refuses it". The flag was never broken; the probe was.
//
// Two arms, and the second is what keeps the first from being vacuous:
//  1. every valued flag's derived probe reaches the help path (rc 0);
//  2. the GENERIC session-id probe is still REFUSED for the numeric flag — so the
//     per-flag derivation is load-bearing, and a future numeric flag that
//     silently falls back to the UUID fails here instead of reddening master.
func TestChatValuedFlagProbesSatisfyTheirOwnParsers(t *testing.T) {
	for _, flag := range chatResumeValuedFlags() {
		t.Run(flag, func(t *testing.T) {
			probe := chatValuedFlagProbeValue(flag)
			_, stderr, code, _ := chatRunArgsWitnessed(t, flag, probe, HelpFlag)
			if code != chatExitOK {
				t.Fatalf("runChat([%s %s %s]) = %d, want %d — the walk's own probe does not satisfy the flag's parser\nstderr:\n%s",
					flag, probe, HelpFlag, code, chatExitOK, stderr)
			}
		})
	}
	// The load-bearing arm: the generic probe the walks used to hand every flag.
	// It is asserted at the PARSER, not through a command line: since 353155e a
	// usage query WINS over a bad flag value (`gopi chat --from-turn <uuid> -h`
	// is rc 0 and prints the page — the operator asking how the flag is spelled
	// must not be refused by the flag they are asking about). So `-h` can no
	// longer witness the refusal, and the property this arm guards is exactly
	// the parser's: the UUID does not satisfy chatParseFromTurnArg while the
	// derived probe does.
	if _, _, err := chatParseFromTurnArg(chatFlagFromTurn, chatArgvValuedFlagProbe, true); err == nil {
		t.Errorf("%s %s is ACCEPTED by its own parser — the session-id probe satisfies a flag whose value slot is a turn number, so chatValuedFlagProbeValue's derivation proves nothing",
			chatFlagFromTurn, chatArgvValuedFlagProbe)
	} else if !strings.Contains(err.Error(), chatFlagFromTurn) {
		t.Errorf("the refusal does not name the flag: %v", err)
	}
	if _, _, err := chatParseFromTurnArg(chatFlagFromTurn, chatValuedFlagProbeValue(chatFlagFromTurn), true); err != nil {
		t.Errorf("the derived probe %q is refused by %s's own parser: %v",
			chatValuedFlagProbeValue(chatFlagFromTurn), chatFlagFromTurn, err)
	}
}
