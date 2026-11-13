package main

// inputtext.go — the TEXT contract for a file-supplied prompt (epic
// binary-summary-guard, 2026-09-15).
//
// THE DEFECT THIS FILE CLOSES: `gopi task list --status failed` rendered a row
// whose summary WAS the head of an ELF executable
// (01a09f6d-7753-7321-ba33-8b32b09213e7). Measured path, not a guess: the run
// started in a directory holding a freshly built ./gopi, the operator's prompt
// positional was `gopi`, and readInput's "an existing non-image file is a text
// input file" rule read the EXECUTABLE as the prompt. 351 bytes of ELF then
// flowed through the ledger as a task headline.
//
// WHY REFUSE INSTEAD OF "READ IT AND TRY": the positional-file feature exists
// to read a PROMPT from a file. A binary is never that prompt, and every
// downstream consumer of a prompt is a text consumer — the ledger summary, the
// session JSONL, the log renderers. A run that silently sends an executable's
// bytes to the model spends money on garbage AND corrupts the ledger; failing
// fast with the offending path and byte offset is strictly more useful (Rule
// 15: no silent failure — the message names what happened and how to override
// it deliberately).
//
// The predicate is the classic one and is deliberately conservative: a NUL byte
// or an invalid UTF-8 sequence. It has no false positives on the input this
// feature serves (UTF-8 prompt text) and it is the SAME test the ledger's
// write path applies (internal/tools tasksummarytext.go), so a file accepted
// here can always be stored and displayed as a summary.

import (
	"fmt"
	"unicode/utf8"
)

const (
	// inputTextNulByte is the byte whose presence settles the question: no
	// text format a prompt is written in carries NUL.
	inputTextNulByte = 0x00

	// inputTextEvidenceWindow is how many bytes around the first offending
	// byte the error message renders — enough to see WHAT was read (a magic
	// number, a UTF-16 BOM) without pasting a megabyte into stderr.
	inputTextEvidenceWindow = 8
)

// nonTextFileReason reports whether data cannot be prompt text, with a short
// human-facing reason and the offset of the first offending byte. The empty
// reason means "this is text".
func nonTextFileReason(data []byte) (reason string, offset int) {
	for i, b := range data {
		if b == inputTextNulByte {
			return "contains a NUL byte (" + inputTextEvidence(data, i) + ")", i
		}
	}
	if !utf8.Valid(data) {
		// Locate the first invalid byte so the message can show it, using the
		// same decoding the rest of the program uses.
		for i := 0; i < len(data); {
			r, size := utf8.DecodeRune(data[i:])
			if r == utf8.RuneError && size == 1 {
				return "is not valid UTF-8 (" + inputTextEvidence(data, i) + ")", i
			}
			i += size
		}
		return "is not valid UTF-8", 0
	}
	return "", 0
}

// inputTextEvidence renders the window of bytes around offset as \xNN pairs —
// the bytes a reader needs to recognise the file for what it is.
func inputTextEvidence(data []byte, offset int) string {
	start := offset - inputTextEvidenceWindow
	if start < 0 {
		start = 0
	}
	end := offset + inputTextEvidenceWindow
	if end > len(data) {
		end = len(data)
	}
	out := ""
	for _, b := range data[start:end] {
		out += fmt.Sprintf("\\x%02x", b)
	}
	return out
}
