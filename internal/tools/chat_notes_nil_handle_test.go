package tools

// chat_notes_nil_handle_test.go — the CALLER-OWNED-handle face of the note
// index, with NO handle.
//
// THE DEFECT IT PINS (measured): ChatNoteSearchOn's first statement called
// (*sql.DB).QueryContext on the handle it was given, and database/sql
// dereferences it internally — so a nil handle did not return an error, it took
// the whole PROCESS down with a SIGSEGV whose stack trace named neither this
// package nor the missing ingredient (cmd/agent's prefetch could reach here with
// a released handle; the acceptance run of task 01a132e2 saw the result as a
// package-level fail with no test name).
//
// Rule 15: the fix is a NAMED error, never a recover — a panic that is swallowed
// still leaves the caller with no idea what it did wrong.

import (
	"context"
	"errors"
	"testing"
)

// TestChatNoteSearchOnRefusesANilHandle: the exported face reports the misuse by
// name instead of dereferencing nothing. The assertion is on errors.Is against
// the exported sentinel, because that is the contract a caller branches on.
func TestChatNoteSearchOnRefusesANilHandle(t *testing.T) {
	hits, tier, err := ChatNoteSearchOn(context.Background(), nil, "any query at all", 0)
	if !errors.Is(err, ErrChatNoteUnusableHandle) {
		t.Fatalf("a nil handle produced err=%v, want ErrChatNoteUnusableHandle", err)
	}
	if hits != nil {
		t.Fatalf("a refused search returned %d hits", len(hits))
	}
	if tier != ChatNoteTierNone {
		t.Fatalf("a refused search named tier %q, want the none tier", tier)
	}
	// The refusal is a STATE, not a panic that got caught: the process is still
	// running and can record it (the line below is only reachable if it is).
	t.Logf("nil handle refused: %v", err)
}

// TestChatNoteSearchOnRefusesAnEmptyQueryBeforeTheHandle is the ORDER, pinned: a
// caller that supplied both an unusable handle AND an empty query is told about
// the query first. It is the pre-existing contract (the empty-query refusal came
// first) and the handle check must not have been pushed above it.
func TestChatNoteSearchOnRefusesAnEmptyQueryBeforeTheHandle(t *testing.T) {
	_, _, err := ChatNoteSearchOn(context.Background(), nil, "   ", 0)
	if err == nil {
		t.Fatal("an empty query was accepted")
	}
	if errors.Is(err, ErrChatNoteUnusableHandle) {
		t.Fatalf("the handle was blamed for an empty query: %v", err)
	}
}
