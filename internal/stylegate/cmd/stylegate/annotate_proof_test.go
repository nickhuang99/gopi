package main

import "testing"

// TestRefuseNonCommentChangeRedProof pins the annotator's own -prove: an
// annotation may add COMMENTS and nothing else, so a change to the token
// stream must be refused even when the result still parses.
//
// RED PROOF (the shapes are the ones measured on 2026-09-27, not invented):
//   - a marker inserted inside a string literal
//   - a marker inserted inside an identifier
//
// Both produce syntactically valid Go, so a parse-only guard accepts them and
// the seeded program silently changes. Here they must be refused, and the one
// legitimate edit — a trailing comment — must be accepted.
func TestRefuseNonCommentChangeRedProof(t *testing.T) {
	cases := []struct {
		name        string
		before      string
		after       string
		wantRefused bool
	}{
		{
			name:        "marker inside a string literal",
			before:      "package p\n\nvar q = \"e2e q2 beta?\"\n",
			after:       "package p\n\nvar q = \"e2e q2 beta //stylegate:ignore reason?\"\n",
			wantRefused: true,
		},
		{
			name:        "marker splitting an identifier",
			before:      "package p\n\nvar x = drainTestCtx(1)\n",
			after:       "package p\n\nvar x = dra //stylegate:ignore reasoninTestCtx(1)\n",
			wantRefused: true,
		},
		{
			name:        "trailing marker is comment-only",
			before:      "package p\n\nfunc f() { _ = g() }\n\nfunc g() error { return nil }\n",
			after:       "package p\n\nfunc f() { _ = g() } //stylegate:ignore fixture teardown\n\nfunc g() error { return nil }\n",
			wantRefused: false,
		},
		{
			name:        "marker on the line above is comment-only",
			before:      "package p\n\nfunc f() { _ = g() }\n\nfunc g() error { return nil }\n",
			after:       "package p\n\n//stylegate:ignore fixture teardown\nfunc f() { _ = g() }\n\nfunc g() error { return nil }\n",
			wantRefused: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := refuseNonCommentChange([]byte(tc.before), []byte(tc.after))
			if tc.wantRefused && err == nil {
				t.Fatalf("the change altered the token stream and was accepted")
			}
			if !tc.wantRefused && err != nil {
				t.Fatalf("a comment-only change was refused: %v", err)
			}
		})
	}
}
