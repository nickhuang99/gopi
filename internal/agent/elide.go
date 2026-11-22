package agent

// elide.go — MIDDLE ELISION, the repository's one spelling of "text was cut out
// of the middle here".
//
// It used to live in compact.go, written for the summarizer's input, and it was
// carried out of that file before the file was deleted because its caller
// (toolresult_cap.go, the per-tool-result token cap) is NOT part of the
// compaction mechanism: a tool result is capped on the way IN, so the cap keeps
// working when nothing compacts. Two functions, both pure, no allocation of a
// rune slice.

import "unicode/utf8"

// elideAtBytes keeps the first headByte bytes and the last tailByte bytes of s
// and splices marker between them.
//
// IT IS EXPRESSED IN BYTE OFFSETS because its caller has already resolved them:
// converting a megabyte string to []rune to slice it costs two allocations and
// two full copies — measured on this host, a 560 KB tool result spent ~1.3 s in
// that conversion alone, more than the excerpt's own token count. The
// rune-offset arithmetic lives in ONE place (runeOffsets) and both callers share
// this splicing code.
func elideAtBytes(s string, headByte, tailByte int, marker string) string {
	if headByte < 0 {
		headByte = 0
	}
	if headByte > len(s) {
		headByte = len(s)
	}
	if tailByte < headByte {
		tailByte = headByte
	}
	if tailByte > len(s) {
		tailByte = len(s)
	}
	if tailByte == len(s) && headByte == 0 {
		return s
	}
	if tailByte >= len(s) {
		return s[:headByte] + "..."
	}
	return s[:headByte] + marker + s[tailByte:]
}

// runeOffsets resolves "keep the first head and the last tail RUNES" into byte
// offsets in ONE pass, with no rune slice allocated.
func runeOffsets(s string, head, tail int) (headByte, tailByte int) {
	if head < 0 {
		head = 0
	}
	if tail < 0 {
		tail = 0
	}
	// Head: advance `head` runes from the start.
	i, seen := 0, 0
	for i < len(s) && seen < head {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		seen++
	}
	headByte = i
	// Tail: walk back `tail` runes from the end.
	j, seen := len(s), 0
	for j > headByte && seen < tail {
		_, size := utf8.DecodeLastRuneInString(s[:j])
		j -= size
		seen++
	}
	return headByte, j
}
