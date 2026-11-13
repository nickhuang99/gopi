// cli_act_test.go — the `gopi tool act type` text/escape contract.
//
// The bug these tests pin (attachment1, 2026-09-21): `gopi tool act type --text
// "say hello\n"` reported "typed 11 character(s)" — it typed a backslash and
// an n — so the command was never submitted. The escape decoding and the
// `--enter` flag are the fix; these cases are the regression guard.

package main

import (
	"strings"
	"testing"
)

// TestDecodeActText pins the escape grammar: every supported escape decodes,
// and an unsupported one is refused with a message NAMING it (never typed
// literally — Rule 15).
func TestDecodeActText(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "plain text untouched", in: "say hello", want: "say hello"},
		{name: "trailing newline", in: `say hello\n`, want: "say hello\n"},
		{name: "newline in the middle", in: `a\nb`, want: "a\nb"},
		{name: "carriage return", in: `a\r`, want: "a\r"},
		{name: "tab and backspace", in: `a\tb\b`, want: "a\tb\b"},
		{name: "formfeed vertical tab bell", in: `\f\v\a`, want: "\f\v\a"},
		{name: "nul", in: `a\0b`, want: "a\x00b"},
		{name: "literal backslash", in: `a\\b`, want: `a\b`},
		{name: "literal quote", in: `say \"hi\"`, want: `say "hi"`},
		{name: "hex escape", in: `\x41`, want: "A"},
		{name: "unicode 4-digit", in: `\u4e2d`, want: "中"},
		{name: "unicode 8-digit", in: `\U0001F600`, want: "\U0001F600"},
		{name: "two escapes", in: `x\ny\tz`, want: "x\ny\tz"},

		{name: "unknown escape", in: `say\q`, wantErr: "unknown escape \\q"},
		{name: "dangling backslash", in: `say\`, wantErr: "dangling backslash"},
		{name: "short hex", in: `\x4`, wantErr: "\\x needs 2 hex digits"},
		{name: "non-hex digits", in: `\xZZ`, wantErr: "not 2 hex digits"},
		{name: "hex above ascii", in: `\x80`, wantErr: "printable ASCII ceiling"},
		{name: "surrogate", in: `\uD800`, wantErr: "not a valid Unicode code point"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeActText(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("decodeActText(%q) = %q, want an error containing %q", tc.in, got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("decodeActText(%q) error = %q, want it to contain %q", tc.in, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeActText(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("decodeActText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseActArgsTypeEnter pins the type sub-action's contract end to end
// (parse only — no display, no injection): the decoded text plus the Return
// the flag guarantees.
func TestParseActArgsTypeEnter(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantText  string
		wantEnter bool
		wantErr   string
	}{
		{
			name:     "trailing newline escape submits",
			args:     []string{actActionType, "--" + actFlagText, `say hello\n`},
			wantText: "say hello\n",
		},
		{
			name:      "enter flag appends the return",
			args:      []string{actActionType, "--" + actFlagText, "say hello", "--" + actFlagEnter},
			wantText:  "say hello\n",
			wantEnter: true,
		},
		{
			name:      "enter flag alone presses return",
			args:      []string{actActionType, "--" + actFlagEnter},
			wantText:  "\n",
			wantEnter: true,
		},
		{
			name:      "flag and escape do not double the return",
			args:      []string{actActionType, "--" + actFlagText, `say hello\n`, "--" + actFlagEnter},
			wantText:  "say hello\n",
			wantEnter: true,
		},
		{
			name:     "plain text and enter_key verb are independent",
			args:     []string{actActionType, "--" + actFlagText, "say hello"},
			wantText: "say hello",
		},
		{
			name:    "no text and no enter is refused",
			args:    []string{actActionType},
			wantErr: "needs --text STR",
		},
		{
			name:    "an unknown escape never reaches the screen",
			args:    []string{actActionType, "--" + actFlagText, `say\q`},
			wantErr: "unknown escape",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := parseActArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parseActArgs(%q) = %+v, want an error containing %q", tc.args, req, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parseActArgs(%q) error = %q, want it to contain %q", tc.args, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseActArgs(%q) unexpected error: %v", tc.args, err)
			}
			if req.text != tc.wantText {
				t.Fatalf("parseActArgs(%q).text = %q, want %q", tc.args, req.text, tc.wantText)
			}
			if req.enter != tc.wantEnter {
				t.Fatalf("parseActArgs(%q).enter = %v, want %v", tc.args, req.enter, tc.wantEnter)
			}
		})
	}
}

// TestActTextEvidenceIsReadable pins the JSON echo: a newline shows as the
// escape the caller wrote (so the evidence is checkable in a terminal), and a
// long value is elided rather than re-emitted whole.
func TestActTextEvidenceIsReadable(t *testing.T) {
	if got := actTextEvidence("say hello\n"); got != `say hello\n` {
		t.Fatalf("actTextEvidence = %q, want %q", got, `say hello\n`)
	}
	if got := actTextEvidence("a\\b"); got != `a\\b` {
		t.Fatalf("actTextEvidence = %q, want %q", got, `a\\b`)
	}
	long := strings.Repeat("x", actTextSpan+10)
	got := actTextEvidence(long)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("actTextEvidence(overflow) = %q, want it elided with …", got)
	}
	if n := len([]rune(got)); n > actTextSpan+1 {
		t.Fatalf("actTextEvidence(overflow) still carries %d runes, want <= %d", n, actTextSpan+1)
	}
}
