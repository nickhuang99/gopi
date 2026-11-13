package tokenizer

// masked_test.go — EncodeMasked's REFUSAL contract (PROJECT.md Rule 19, Rule 10.5).
//
// The regression this file exists for: EncodeMasked used to panic when mask was
// not exactly len(text) entries long. A panic there takes the caller's choice
// away — the caller that built a mismatched mask is the one that can say what to
// do about it — and the Rule 19 gate (no panic outside init/main/Must*) failed
// the tree over it. The contract now is an error, and the error is the one named
// value ErrMaskLengthMismatch, so a caller can test for it rather than string
// matching.

import (
	"errors"
	"testing"
)

// TestEncodeMaskedRefusalIsTheNamedSentinel is the identity arm: both a SHORT and
// a LONG mask must come back as an error (never a panic, never a silent count),
// the error must be ErrMaskLengthMismatch through errors.Is, and a refused call
// must return no ids.
//
// REBASE NOTE (delivery round 2/3, ground 01a1a674): origin/master's fa77f68f
// landed `TestEncodeMaskedRefusesAMismatchedMask` in unit_test.go, which asserts
// the SAME behaviour but only that an error is non-nil. This arm is kept beside
// it because it asserts the two properties the sibling's does not: the sentinel
// identity (a caller can errors.Is rather than string-match) and the absence of a
// partial result. It carries a DISTINCT name on purpose — two same-named tests in
// one package is a build failure.
func TestEncodeMaskedRefusalIsTheNamedSentinel(t *testing.T) {
	tk := loadTk(t)
	const text = "hello"

	for _, tc := range []struct {
		name string
		mask []bool
	}{
		{"short by one", make([]bool, len(text)-1)},
		{"long by one", make([]bool, len(text)+1)},
		{"empty but non-nil", []bool{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids, err := tk.EncodeMasked(text, tc.mask)
			if err == nil {
				t.Fatalf("EncodeMasked(%q, mask of %d entries) returned %d ids and no error — "+
					"a mask that does not match its text is a caller error, not a count",
					text, len(tc.mask), len(ids))
			}
			if !errors.Is(err, ErrMaskLengthMismatch) {
				t.Fatalf("error %v is not ErrMaskLengthMismatch — a caller cannot test for it, "+
					"so every caller has to match the string", err)
			}
			if ids != nil {
				t.Fatalf("a refused encoding returned %d ids beside its error — a partial result "+
					"invites a caller to use it", len(ids))
			}
		})
	}
}

// TestEncodeMaskedAcceptsNilAndExactMasks pins the other half: the two LEGAL
// shapes still succeed. nil means "every byte is template text", an exact-length
// mask means what it says. Both are the shapes every real caller builds, so a fix
// that refused them would be a fix that broke the product.
func TestEncodeMaskedAcceptsNilAndExactMasks(t *testing.T) {
	tk := loadTk(t)
	text := markUser + "hi" + markAssistant

	// nil mask == the unmasked encoding of the same string.
	nilMask, err := tk.EncodeMasked(text, nil)
	if err != nil {
		t.Fatalf("nil mask was refused: %v", err)
	}
	if want := tk.Encode(text); len(nilMask) != len(want) {
		t.Fatalf("nil mask gave %d ids, Encode gave %d — nil is documented as "+
			"'every byte is template text'", len(nilMask), len(want))
	}

	// An exact-length all-true mask is the same thing spelled out.
	all := make([]bool, len(text))
	for i := range all {
		all[i] = true
	}
	exactMask, err := tk.EncodeMasked(text, all)
	if err != nil {
		t.Fatalf("an exact-length mask was refused: %v", err)
	}
	if len(exactMask) != len(nilMask) {
		t.Fatalf("all-true mask gave %d ids, nil gave %d — they describe the same input",
			len(exactMask), len(nilMask))
	}

	// An all-false mask forbids every added-token match, so the markers are BPE.
	none := make([]bool, len(text))
	for i := range none {
		none[i] = false
	}
	bpeOnly, err := tk.EncodeMasked(text, none)
	if err != nil {
		t.Fatalf("an all-false mask was refused: %v", err)
	}
	if len(bpeOnly) <= len(nilMask) {
		t.Fatalf("an all-false mask gave %d ids against %d with matching allowed — "+
			"the mask is not reaching the added-token matcher", len(bpeOnly), len(nilMask))
	}
}

// TestEncodeMaskedEmptyTextIsNotAMismatch pins that the empty string short-
// circuits BEFORE the length check: "" with a non-nil empty mask is an exact
// match anyway, and "" with any mask is a legal empty encoding, not an error.
func TestEncodeMaskedEmptyTextIsNotAMismatch(t *testing.T) {
	tk := loadTk(t)
	for _, mask := range [][]bool{nil, {}, {true}} {
		ids, err := tk.EncodeMasked("", mask)
		if err != nil {
			t.Fatalf(`EncodeMasked("", mask of %d entries): %v`, len(mask), err)
		}
		if ids != nil {
			t.Fatalf(`EncodeMasked("") returned %d ids`, len(ids))
		}
	}
}
