package tools

// task_delivery_owed_redrive_test.go — the leg's OWN loop, measured against
// injected seams.
//
// WHY THIS TEST EXISTS (Rule 31: the surface this change created must be RUN,
// not merely compiled). The leg moved from cmd/agent into this package on
// 2026-11-05 (row 01a1ab49) to take its call out of a scanned tree of the
// cwd-release gate. The move is behaviour-preserving by intention, and an
// intention is not a measurement: the loop it wraps is NOT what
// DeliveryRetryTaskID's own tests cover — nothing in the package asserted that
// the leg calls the repair once per owed row, that it keeps going after a
// refused row, or that it counts a row whose report still says `Owed()` as still
// owed. Those are the numbers the pass prints and an operator acts on.
//
// The two seams (deliveryRedriveOwed / deliveryRedriveOne) are package vars with
// production defaults, restored by t.Cleanup, so the production path is what
// ships and the loop is what these cases drive (Rule 17: the restore uses the
// snapshot helper, not a hand-rolled getenv pair — these are not env vars at all,
// but the discipline is the same: restore what you replaced).

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

const (
	redriveTestProg = "gopi task sync-pass"
	redriveTestHead = "t-owed-"
)

// redriveSeams replaces both seams for one case and restores them after it.
func redriveSeams(t *testing.T, owed []string, unreadable []string, owedErr error,
	one func(string) (*DeliveryRetryReport, error)) {
	t.Helper()
	prevOwed, prevOne := deliveryRedriveOwed, deliveryRedriveOne
	t.Cleanup(func() {
		deliveryRedriveOwed, deliveryRedriveOne = prevOwed, prevOne
	})
	deliveryRedriveOwed = func() ([]string, []string, error) {
		return owed, unreadable, owedErr
	}
	deliveryRedriveOne = one
}

// TestDeliveryRedriveLegCallsTheRepairOncePerOwedRow is the positive arm: two
// owed rows, two repair calls, one delivered and one still owed — the leg must
// report BOTH and return the count an operator acts on (1), not zero.
func TestDeliveryRedriveLegCallsTheRepairOncePerOwedRow(t *testing.T) {
	var seen []string
	redriveSeams(t,
		[]string{redriveTestHead + "a", redriveTestHead + "b"}, nil, nil,
		func(id string) (*DeliveryRetryReport, error) {
			seen = append(seen, id)
			if id == redriveTestHead+"a" {
				return &DeliveryRetryReport{TaskID: id, Status: deliveryRetryStatusDelivered}, nil
			}
			return &DeliveryRetryReport{TaskID: id, Status: deliveryRetryStatusBlocked, Blocked: "peer does not hold it"}, nil
		})

	var buf bytes.Buffer
	still := DeliveryRedriveLeg(&buf, redriveTestProg)

	if len(seen) != 2 || seen[0] != redriveTestHead+"a" || seen[1] != redriveTestHead+"b" {
		t.Fatalf("the repair was called %v — the leg must call it ONCE per owed row, in inventory order %v",
			seen, []string{redriveTestHead + "a", redriveTestHead + "b"})
	}
	if still != 1 {
		t.Fatalf("stillOwed = %d, want 1 (one delivered, one blocked) — this is the number the pass reports and an operator acts on.\noutput:\n%s", still, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, redriveTestProg+": "+DeliveryRedriveMark+": done") {
		t.Errorf("the leg's closing line does not carry the caller's prefix %q:\n%s", redriveTestProg, out)
	}
	if !strings.Contains(out, "1 re-driven, 1 still owed") {
		t.Errorf("the closing counts are not the measured ones:\n%s", out)
	}
}

// TestDeliveryRedriveLegKeepsGoingAfterARefusedRow is the fail-soft arm: a row
// whose repair returns an ERROR must not stop the loop (Rule 15 — reported, not
// swallowed, and not promoted to a whole-pass failure). Both a refused row and
// a blocklisted row count as still owed; only a delivered row does not.
func TestDeliveryRedriveLegKeepsGoingAfterARefusedRow(t *testing.T) {
	calls := 0
	redriveSeams(t,
		[]string{redriveTestHead + "bad", redriveTestHead + "good"}, nil, nil,
		func(id string) (*DeliveryRetryReport, error) {
			calls++
			if id == redriveTestHead+"bad" {
				return nil, fmt.Errorf("ledger row unreadable")
			}
			return &DeliveryRetryReport{TaskID: id, Status: deliveryRetryStatusDelivered}, nil
		})

	var buf bytes.Buffer
	still := DeliveryRedriveLeg(&buf, redriveTestProg)

	if calls != 2 {
		t.Fatalf("the repair was called %d time(s) — a refused row must not stop the loop over the others:\n%s", calls, buf.String())
	}
	if still != 1 {
		t.Fatalf("stillOwed = %d, want 1 (the refused row) — a refused repair is not a delivered one.\noutput:\n%s", still, buf.String())
	}
	if !strings.Contains(buf.String(), "ledger row unreadable") {
		t.Errorf("the refusal's own detail was not printed — the leg swallowed it:\n%s", buf.String())
	}
}

// TestDeliveryRedriveLegSeparatesEmptyFromUnreadable is the Rule 15 arm, and it
// is the one the pass's own wiring test depends on: "nothing owed" and "the
// inventory could not be read" must not print the same thing, or a log cannot
// tell a quiet axis from a broken one.
func TestDeliveryRedriveLegSeparatesEmptyFromUnreadable(t *testing.T) {
	noop := func(string) (*DeliveryRetryReport, error) {
		t.Fatalf("the repair was called for an inventory that owes nothing")
		return nil, nil
	}

	t.Run("empty inventory", func(t *testing.T) {
		redriveSeams(t, nil, nil, nil, noop)
		var buf bytes.Buffer
		if still := DeliveryRedriveLeg(&buf, redriveTestProg); still != 0 {
			t.Fatalf("stillOwed = %d for an empty inventory, want 0", still)
		}
		if !strings.Contains(buf.String(), DeliveryRedriveNoneText) {
			t.Fatalf("the empty inventory was not reported as nothing owed:\n%s", buf.String())
		}
	})

	t.Run("unreadable inventory", func(t *testing.T) {
		redriveSeams(t, nil, nil, fmt.Errorf("open ledger: permission denied"), noop)
		var buf bytes.Buffer
		DeliveryRedriveLeg(&buf, redriveTestProg)
		out := buf.String()
		if !strings.Contains(out, "inventory unreadable") {
			t.Fatalf("an unreadable inventory was not reported:\n%s", out)
		}
		if strings.Contains(out, DeliveryRedriveNoneText) {
			t.Fatalf("\"nothing owed\" was printed for an inventory that could not be READ — the two facts are indistinguishable, which is the defect:\n%s", out)
		}
	})

	t.Run("unreadable rows are named, not dropped", func(t *testing.T) {
		redriveSeams(t, nil, []string{"t-x (no such directory)"}, nil, noop)
		var buf bytes.Buffer
		DeliveryRedriveLeg(&buf, redriveTestProg)
		if !strings.Contains(buf.String(), "t-x (no such directory)") {
			t.Fatalf("a row the probe could not measure was silently dropped:\n%s", buf.String())
		}
	})
}

// TestDeliveryRedriveLegTakesTheCallersPrefix pins the parameter's contract: an
// empty prefix must not produce lines starting with ": " (a log line nobody can
// grep), and a real prefix must be carried on every line the leg prints.
func TestDeliveryRedriveLegTakesTheCallersPrefix(t *testing.T) {
	redriveSeams(t, nil, nil, nil, func(string) (*DeliveryRetryReport, error) { return nil, nil })

	var blank bytes.Buffer
	DeliveryRedriveLeg(&blank, "   ")
	if strings.HasPrefix(blank.String(), ":") {
		t.Fatalf("an empty caller prefix produced a line starting with ':':\n%s", blank.String())
	}
	if !strings.Contains(blank.String(), DeliveryRedriveMark) {
		t.Fatalf("the fallback prefix dropped the leg's own marker:\n%s", blank.String())
	}
}
