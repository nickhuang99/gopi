// sqlitecode_test.go — the ONE definition of SQLite's codes, pinned.
//
// The reason this package exists is that TWO seams hit the same driver error
// (cmd/agent's warm read-only ledger opener and internal/tools' chat-index
// opener). Before it, each carried its own copy of `14` and its own sentence,
// and one of them being corrected would have left the other telling an operator
// to hunt memory pressure for a path defect. These arms make the shared
// definition load-bearing rather than decorative.
package sqlitecode

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeCoder is a driver error stand-in: the classifier keys on nothing but the
// `Code() int` method, which is why it can be satisfied without importing the
// driver (the whole point of reading the code through an anonymous interface).
type fakeCoder struct {
	code int
	msg  string
}

func (f fakeCoder) Error() string { return f.msg }
func (f fakeCoder) Code() int     { return f.code }

// TestIsRecognisesCantOpenThroughTheWholeWrapChain: the error that reaches a
// caller is usually wrapped at least twice (the opener wraps the driver, the
// face wraps the opener), so a classifier that looked only at the top would
// miss the very failure this package exists to recognise.
func TestIsRecognisesCantOpenThroughTheWholeWrapChain(t *testing.T) {
	driver := fakeCoder{code: CantOpen, msg: "unable to open database file: out of memory (14)"}
	// The measured shape: opener → face → caller, each wrapping with %w.
	wrapped := fmt.Errorf("cannot open the ledger index /x/memory.db: %w",
		fmt.Errorf("open: %w", driver))

	if !Is(wrapped) {
		t.Errorf("Is(%q) = false, want true — a CANTOPEN two wraps down must still be recognised; without this "+
			"the translation silently never applies", wrapped)
	}
}

// TestIsRefusesEverythingElse is the refuse-to-overreach half: the classifier is
// narrow. A different result code, a plain error and nil all stay unclassified —
// otherwise every unrelated failure would be given the CANTOPEN cause and a
// reader would be told a false diagnosis (the exact defect being fixed).
func TestIsRefusesEverythingElse(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{name: "nil", err: nil},
		{name: "plain error", err: errors.New("boom")},
		{name: "different code", err: fakeCoder{code: 26, msg: "file is not a database (26)"}},
		{name: "zero code", err: fakeCoder{code: 0, msg: "no code"}},
	}
	for _, c := range cases {
		if Is(c.err) {
			t.Errorf("Is(%v) = true for the %s case, want false — the classifier must not coerce an unrelated "+
				"failure into a CANTOPEN diagnosis", c.err, c.name)
		}
	}
}

// TestCauseDispatchesTheAllocationReading pins the sentence's LOAD-BEARING
// phrase. The driver says "out of memory (14)"; the fix is worthless if gopi
// repeats that instead of denying it, because the reader's next move is
// `free -m` or the OOM killer either way.
func TestCauseDispatchesTheAllocationReading(t *testing.T) {
	if !strings.Contains(CantOpenCause, "NOT an allocation failure") {
		t.Errorf("CantOpenCause does not deny the allocation reading: %q — a reader following the driver's "+
			"wording goes hunting memory pressure for a path defect", CantOpenCause)
	}
	if !strings.Contains(CantOpenCause, "permission") {
		t.Errorf("CantOpenCause does not name the permission arm: %q — a mode-000 ledger is one of the two "+
			"measured shapes of this code", CantOpenCause)
	}
}
