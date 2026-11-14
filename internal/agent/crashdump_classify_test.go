package agent

// crashdump_classify_test.go — the regression guard for the CONSUMER side of
// the 2026-10-16 misattributed-timeout family (the producer half — the
// sentence itself — is internal/runend + devdoc/ctx-ended-run-reason.md).
//
// WHAT WAS WRONG. loop.go's round-boundary fallback ended a run with
// `agent timed out after %d iterations`. collectErrorRecord tests the run
// error's TEXT against a keyword switch, and that switch matched
// `"timeout"` — while the message says "timed out" (one space apart, no
// substring). The message therefore fell to `default:`, which asserted
// `RecoverableHint = false`. Two real zpeer ledger rows consequently read
//
//	error(category=unknown iter=52 recoverable=false)
//	error(category=unknown iter=43 recoverable=false)
//
// i.e. an error nothing had classified was stamped as certainly
// UNRECOVERABLE (Rule 15a: false ≠ unknown), while the same row's own resume
// pointer offered `gopi session resume`.
//
// WHAT IS ASSERTED HERE, and why this is the minimal reproduction: the
// MEASURED error text is pushed through collectErrorRecord — the real
// collection path, not a helper — and the resulting record is checked on all
// three evidence surfaces a reader sees (the ledger digest, the session_end
// error block, the crash-dump document). The first assertion is the one that
// fails on the pre-fix classifier; the second pins the honest default for a
// failure NOTHING recognises, which is the other half of the same defect.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// measuredTimeoutIters is the iteration figure the two real zpeer rows carry
// (52 and 43 in that batch; this is the count the row that reproduced the
// defect on the live surface printed). It is only a payload — the classifier
// must reach its verdict from the WORDING, never from the number.
const measuredTimeoutIters = 53

// measuredTimeoutErrorText is the verbatim sentence the producer emitted
// (runend.RunContextCancelled's predecessor in loop.go). It is spelled out as
// a FORMAT because the producer's own text parameterised the count.
const measuredTimeoutErrorText = "agent timed out after %d iterations"

// crashErrTimeoutMarkerNetwork / crashErrRecoverableUnknown are the two
// spellings this guard turns on. They are the RENDERED forms, not the
// constants the writers use: a test that asserted the constant would agree
// with itself no matter what a reader sees.
const (
	crashErrNetworkCategory   = "network"
	crashErrUnknownCategory   = "unknown"
	crashErrRecoverableTrue   = "recoverable=true"
	crashErrRecoverableFalse  = "recoverable=false"
	crashErrRecoverableUnknwn = "recoverable=unknown"
	// crashErrRecoverableKnownKey is the session_end block's companion field:
	// present-and-false is the only way a JSON reader can tell "asserted no"
	// from "never decided" when the hint itself is a bool.
	crashErrRecoverableKnownKey = "recoverable_known"
)

// newCrashErrRecord runs the REAL collection path with the given terminal run
// error. No session logger is needed (collectErrorRecord never touches one).
func newCrashErrRecord(t *testing.T, runErr error) *ErrorRecord {
	t.Helper()
	a := &Agent{}
	er := a.collectErrorRecord(runErr, measuredTimeoutIters, 100, 12.5)
	if er == nil {
		t.Fatal("collectErrorRecord returned nil — the run error was dropped")
	}
	return er
}

// TestCrashdumpClassifier_RealTimeoutWordingIsNetworkAndRecoverable is the
// minimal reproduction: the EXACT measured error text must classify as a
// transport failure, and must never carry the false `recoverable=false`.
func TestCrashdumpClassifier_RealTimeoutWordingIsNetworkAndRecoverable(t *testing.T) {
	msg := fmt.Sprintf(measuredTimeoutErrorText, measuredTimeoutIters)
	er := newCrashErrRecord(t, fmt.Errorf("%s", msg))

	if er.Category != crashErrNetworkCategory {
		t.Fatalf("category = %q for %q — the measured timeout wording must classify as a transport failure, not fall through unclassified",
			er.Category, msg)
	}
	if !er.RecoverableHint {
		t.Fatalf("RecoverableHint = false for %q — a timeout the provider was responsible for must not be recorded unrecoverable", msg)
	}
	// The error TEXT survives on the record (evidence, not just a verdict).
	if !strings.Contains(er.ErrorBody, "timed out") {
		t.Fatalf("ErrorBody = %q, want the raw error text", er.ErrorBody)
	}

	// Surface 3: the ledger digest (the notification pipe → PA).
	digest := er.SummaryDigest()
	// Logged so a -v run of this test IS the observable invocation of the
	// shipped classifier: the record it produces for the measured text.
	t.Logf("classifier(%-*q) -> category=%s recoverable=%t", len(msg), msg, er.Category, er.RecoverableHint)
	t.Logf("ledger digest: %s", digest)
	if !strings.Contains(digest, crashErrRecoverableTrue) {
		t.Fatalf("ledger digest = %q, want %s", digest, crashErrRecoverableTrue)
	}
	if strings.Contains(digest, crashErrRecoverableUnknwn) {
		t.Fatalf("ledger digest = %q — a CLASSIFIED failure must not report an unknown recoverability", digest)
	}
}

// TestCrashdumpClassifier_UnclassifiedIsUnknownNotFalse is the other half:
// an error NOTHING recognises must not be stamped as certainly unrecoverable.
//
// `recoverable=false` and `recoverable=unknown` are different claims — the
// first tells an operator "resume cannot help", the second tells them nobody
// knows (Rule 15a). The pre-fix `default:` arm asserted the first for the
// second's case.
func TestCrashdumpClassifier_UnclassifiedIsUnknownNotFalse(t *testing.T) {
	const opaque = "cc: tool dispatch failed for reasons the taxonomy cannot see"
	er := newCrashErrRecord(t, fmt.Errorf("%s", opaque))

	if er.Category != crashErrUnknownCategory {
		t.Fatalf("category = %q, want %q — nothing recognises this text", er.Category, crashErrUnknownCategory)
	}
	if er.RecoverableHint {
		t.Fatalf("an unclassified failure must not claim recoverability either: %+v", er)
	}

	digest := er.SummaryDigest()
	t.Logf("classifier(%q) -> category=%s recoverable=%t", opaque, er.Category, er.RecoverableHint)
	t.Logf("ledger digest: %s", digest)
	if !strings.Contains(digest, crashErrRecoverableUnknwn) {
		t.Fatalf("ledger digest = %q, want %s — an unclassified failure has NO verdict, and printing `false` is the defect",
			digest, crashErrRecoverableUnknwn)
	}
	if strings.Contains(digest, crashErrRecoverableFalse) {
		t.Fatalf("ledger digest = %q — `recoverable=false` is an ASSERTION and this failure carries none", digest)
	}

	// Surface 1: the session_end error block. The hint stays a bool (the
	// field's shape is written by older dumps too, so it is not migrated),
	// and its companion field is what makes the two cases distinguishable to
	// a JSON reader.
	block := er.SessionEndBlock()
	known, present := block[crashErrRecoverableKnownKey]
	if !present {
		t.Fatalf("session_end error block carries no %q — a reader cannot tell `false` from `unknown`: %v",
			crashErrRecoverableKnownKey, block)
	}
	if known != false {
		t.Fatalf("%s = %v for an unclassified failure, want false", crashErrRecoverableKnownKey, known)
	}

	// ...and a CLASSIFIED unrecoverable failure does NOT carry it (the field
	// exists to separate the two, so its absence must mean "asserted").
	classified := &ErrorRecord{Category: crashErrNetworkCategory}
	if _, present := classified.SessionEndBlock()[crashErrRecoverableKnownKey]; present {
		t.Fatalf("a classified failure must not carry %q", crashErrRecoverableKnownKey)
	}
}

// TestCrashdumpClassifier_SpellingTable pins the recognition set as a TABLE,
// so the next missing spelling fails here rather than in a production row.
// The three prior arms matched only the spelling in their own literal; this is
// the same defect class ("timed out" beside "timeout") made visible.
func TestCrashdumpClassifier_SpellingTable(t *testing.T) {
	const (
		wantNetwork = crashErrNetworkCategory
		wantVendor  = "vendor"
		wantUnknown = crashErrUnknownCategory
	)
	for _, tc := range []struct {
		msg  string
		want string
	}{
		// The measured real-world wording, and its sibling spellings.
		{fmt.Sprintf(measuredTimeoutErrorText, measuredTimeoutIters), wantNetwork},
		{"client: request timeout after 30s", wantNetwork},
		{"agent run ended before the task was finished: context deadline exceeded (deadline-exceeded)", wantNetwork},
		{"agent run ended before the task was finished: context canceled (run-context-cancelled)", wantNetwork},
		{"Post \"https://api.deepseek.com/messages\": dial tcp: connection refused", wantNetwork},
		{"unexpected EOF", wantNetwork},
		{"dial tcp: lookup api.deepseek.com: no such host", wantNetwork},
		// Vendor-protocol failures.
		{"stream error: overloaded", wantVendor},
		// Unrecognised text is UNKNOWN, never a verdict.
		{"some opaque failure", wantUnknown},
		{"", wantUnknown},
	} {
		if got := newCrashErrRecord(t, fmt.Errorf("%s", tc.msg)).Category; got != tc.want {
			t.Errorf("category(%q) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

// TestCrashdumpClassifier_DumpDocumentCarriesTheVerdict runs the record all
// the way to the crash-dump FILE — the surface an operator triaging later
// reads — so the fix is proven where it is consumed, not only in memory.
func TestCrashdumpClassifier_DumpDocumentCarriesTheVerdict(t *testing.T) {
	sl := newTestSession(t)
	er := newCrashErrRecord(t, fmt.Errorf(measuredTimeoutErrorText, measuredTimeoutIters))
	sl.WriteCrashDump(er)
	if er.DumpPath == "" {
		t.Fatal("WriteCrashDump did not record DumpPath")
	}
	raw, err := os.ReadFile(er.DumpPath) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("dump is not valid JSON: %v", err)
	}
	if doc["category"] != crashErrNetworkCategory || doc["recoverable_hint"] != true {
		t.Fatalf("dump category/hint = %v/%v, want %s/true",
			doc["category"], doc["recoverable_hint"], crashErrNetworkCategory)
	}
}

// TestCrashdumpDigest_StatesTheZeroFieldsATransportFailureIsMadeOf pins the
// hand-off from typo-fixes the digest used to LOSE (Rule 15a), measured on the
// live ledger 2026-10-29: 481 failed rows, ZERO carrying ` http=0` or ` iter=0`,
// because both fields were rendered only when non-zero — and zero is exactly
// what "no response ever arrived" and "the run died on its first iteration" are.
//
// Without this, a dispatch-side reader cannot tell a transport-dead row from one
// whose digest simply never recorded the fields. (The reader this paragraph used
// to name — internal/tools/task_transport_failure.go — was deleted 2026-10-30,
// task 01a18ef7, with the reroute leg it served; the DIGEST contract it relied
// on is unchanged and still worth pinning, because `gopi task show` and every
// human reader face the same ambiguity.)
func TestCrashdumpDigest_StatesTheZeroFieldsATransportFailureIsMadeOf(t *testing.T) {
	er := newCrashErrRecord(t, fmt.Errorf(measuredTimeoutErrorText, measuredTimeoutIters))

	// Force the exact transport-dead shape: no status, no iteration completed.
	er.HTTPStatus = 0
	er.FailedIteration = 0

	digest := er.SummaryDigest()
	t.Logf("ledger digest: %s", digest)
	if !strings.Contains(digest, " http=0") {
		t.Fatalf("digest = %q, want an explicit `http=0` — an OMITTED status is indistinguishable from an unrecorded one", digest)
	}
	if !strings.Contains(digest, " iter=0") {
		t.Fatalf("digest = %q, want an explicit `iter=0` — an OMITTED iteration is indistinguishable from an unrecorded one", digest)
	}
}
