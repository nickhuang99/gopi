package uiclient

// sgrstream_meta_test.go — the READ side's numeric metadata fields are PARSED,
// not guessed.
//
// The reader's header/trailer fields (version, grid, frames, bytes) are the
// stream's own claims about itself. Until this file, a field the writer could
// never have produced was silently turned into a ZERO and the read continued:
//
//	version=one  → meta.Version = 0, and the refusal said "declares format
//	               version 0" — quoting a value the file does not contain.
//	grid=10      → meta.Grid = [0 0], no error at all.
//	frames=x     → meta.Trailer = true and meta.Frames = 0: a HALF RESULT, which
//	               reads exactly like a completed run that wrote no frames. That
//	               is the shape Rule 15 forbids ("an error discarded … hides the
//	               failure"), and it is worse here than a raw error would be,
//	               because the reader then reports the run as CLOSED.
//
// Each case below therefore asserts two things about the refusal, and the pair
// is what makes the test red before the fix and green after:
//
//	① the unparseable raw value must appear in the refusal — an operator who
//	   reads it can see WHAT the file said;
//	② the refusal must NOT claim a value the file never carried — "declares
//	   format version 0" on a file saying version=one is a lie, not a diagnosis.
//
// Tolerance is not weakened by any of this: a stream cut mid-frame still reads
// (TestSgrStreamTruncatedStreamStillReads pins that separately). What is refused
// is a RECORD whose own field cannot be read — a record is a claim, and a claim
// the reader could not decode is not the same thing as a record that is absent.

import (
	"bytes"
	"strings"
	"testing"
)

// sgrStreamRecord renders one record line from raw key/value words, so a test
// can hand the reader a header or trailer the writer would never produce.
func sgrStreamRecord(prefix string, kv []string) string {
	return prefix + " " + strings.Join(kv, " ") + "\n"
}

// sgrStreamHeaderWords is a header the reader accepts, as words — the base every
// case below perturbs by exactly one field.
func sgrStreamHeaderWords() []string {
	return []string{
		"version=1",
		"format=" + SgrStreamFormat,
		"terminator=#",
		"grid=10x2",
		"revision=deadbeef",
		"started=2026-01-01T00:00:00Z",
	}
}

// sgrStreamTrailerWords is a trailer the reader accepts, as words — the base
// the trailer cases below perturb by exactly one field.
func sgrStreamTrailerWords() []string {
	return []string{"frames=1", "bytes=4", "grid=10x2"}
}

// kvReplaced returns words with key's value swapped for val.
func kvReplaced(words []string, key, val string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		if strings.HasPrefix(w, key+"=") {
			out = append(out, key+"="+val)
			continue
		}
		out = append(out, w)
	}
	return out
}

// kvDropped returns words with key's word removed entirely.
func kvDropped(words []string, key string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		if strings.HasPrefix(w, key+"=") {
			continue
		}
		out = append(out, w)
	}
	return out
}

// TestSgrStreamMetaFieldsMustParse drives every numeric field the reader decodes.
func TestSgrStreamMetaFieldsMustParse(t *testing.T) {
	cases := []struct {
		name     string
		header   []string
		trailer  []string // nil = the stream ends without a trailer
		frames   []string // the frame payloads; nil = one 4-byte frame
		wantWord string   // ① must appear in the refusal
		notClaim string   // ② must NOT appear ("" = no such claim to deny)
	}{
		{
			name:     "header version is not a number",
			header:   kvReplaced(sgrStreamHeaderWords(), "version", "one"),
			wantWord: "one",
			notClaim: "version 0",
		},
		{
			name:     "header names no version at all",
			header:   kvDropped(sgrStreamHeaderWords(), "version"),
			wantWord: "version",
			notClaim: "version 0",
		},
		{
			name:     "header names no grid",
			header:   kvDropped(sgrStreamHeaderWords(), "grid"),
			wantWord: "grid",
		},
		{
			name:     "grid has no separator",
			header:   kvReplaced(sgrStreamHeaderWords(), "grid", "10"),
			wantWord: "10",
		},
		{
			name:     "grid column count is not a number",
			header:   kvReplaced(sgrStreamHeaderWords(), "grid", "ax2"),
			wantWord: "ax2",
		},
		{
			name:     "grid row count is not a number",
			header:   kvReplaced(sgrStreamHeaderWords(), "grid", "10xb"),
			wantWord: "10xb",
		},
		{
			name:     "trailer frames is not a number",
			header:   sgrStreamHeaderWords(),
			trailer:  kvReplaced(sgrStreamTrailerWords(), "frames", "x"),
			frames:   []string{"abcd"},
			wantWord: "frames",
		},
		{
			name:     "trailer bytes is not a number",
			header:   sgrStreamHeaderWords(),
			trailer:  kvReplaced(sgrStreamTrailerWords(), "bytes", "y"),
			frames:   []string{"abcd"},
			wantWord: "bytes",
		},
		{
			name:     "trailer names no frames",
			header:   sgrStreamHeaderWords(),
			trailer:  kvDropped(sgrStreamTrailerWords(), "frames"),
			frames:   []string{"abcd"},
			wantWord: "frames",
		},
		{
			name:     "trailer names no bytes",
			header:   sgrStreamHeaderWords(),
			trailer:  kvDropped(sgrStreamTrailerWords(), "bytes"),
			frames:   []string{"abcd"},
			wantWord: "bytes",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var blob bytes.Buffer
			blob.WriteString(sgrStreamRecord(ScreenStreamHeaderPrefix, tc.header))
			for _, p := range tc.frames {
				blob.WriteString(itoa(len(p)) + "\n")
				blob.WriteString(p)
			}
			if tc.trailer != nil {
				blob.WriteString(sgrStreamRecord(ScreenStreamTrailerPrefix, tc.trailer))
			}
			_, _, err := ReadSgrFrames(bytes.NewReader(blob.Bytes()))
			if err == nil {
				t.Fatalf("a record field the reader could not decode was accepted: the stream reads as a completed run with a zero in it")
			}
			if !strings.Contains(err.Error(), tc.wantWord) {
				t.Errorf("refusal does not name %q, so the operator cannot see what the file said: %v", tc.wantWord, err)
			}
			if tc.notClaim != "" && strings.Contains(err.Error(), tc.notClaim) {
				t.Errorf("refusal claims %q — a value the file never carried: %v", tc.notClaim, err)
			}
		})
	}
}

// itoa is a tiny decimal renderer, so this file does not import strconv purely
// to write a frame length the format itself spells in decimal.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
