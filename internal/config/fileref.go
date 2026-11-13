package config

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// FileRefPrefix marks a CLI argument value that should be replaced by the
// contents of the file at the given path: `@path/to/file` (or `@-` to read
// standard input). This exists because submitting multi-line text — task
// briefings, --prompt, --reason — on the command line is awkward: shell
// quoting mangles special characters and destroys formatting that the task
// execution depends on. A file reference keeps the text byte-exact:
//
//	gopi task submit "Fix the parser" --payload @briefing.md
//	gopi --prompt @instructions.txt
//	gopi task submit "T" --reason @rejection-notes.txt
//
// The prefix is a single literal '@' at the START of the value only — a
// path that merely contains '@' later (emails, user@host) is untouched.
// A literal '@' at the start is still reachable via '@@' escaping.
const FileRefPrefix = "@"

// stdinFileRef reads the payload from standard input (e.g. --payload @- <
// briefing.md). Two characters: prefix + the stdin marker.
const stdinFileRef = "@-"

// maxFileRefBytes caps a file reference expansion. Command-line-submitted
// payloads feed agent briefings, not data dumps — a runaway expansion
// (accidentally pointing at a multi-GB log) must fail fast with a clear
// error instead of OOMing the process.
const maxFileRefBytes = 8 << 20 // 8 MiB

// literalAtRef is the escape hatch for a value that genuinely starts with a
// single '@': `@@foo` expands to the literal text `@foo`.
const literalAtRef = "@@"

// IsFileRef reports whether value is a file reference: it starts with '@'
// (or is exactly the stdin marker "@-") and is longer than the bare prefix.
// A bare "@" is NOT a file reference — there is no path to read — so it
// passes through as literal text.
func IsFileRef(value string) bool {
	return len(value) > len(FileRefPrefix) && strings.HasPrefix(value, FileRefPrefix)
}

// ExpandFileRef replaces a file-reference argument value with the contents
// of the referenced file. Non-references pass through unchanged; a bare "@"
// passes through as literal text; "@@x" unescapes to "@x"; "@-" reads stdin.
// References are only recognized when the file EXISTS — prose that happens
// to start with '@' (e.g. "@nick mentioned...") must never be swallowed.
// Returns the expanded value; an unreadable or oversized referenced file is
// an error naming the path.
func ExpandFileRef(value string) (string, error) {
	if !IsFileRef(value) {
		return value, nil
	}
	if strings.HasPrefix(value, literalAtRef) {
		// "@@..." → literal "@" + the rest (escape hatch).
		return value[len(literalAtRef)-1:], nil
	}
	path := value[len(FileRefPrefix):]
	// A file ref must be a single path token — a value with whitespace after
	// '@' is prose that merely STARTS with '@' (e.g. "@nick mentioned this"),
	// not a reference. Without this guard such prose would break with a
	// confusing "open <words>" file error.
	if strings.ContainsAny(path, " \t\n\r") {
		return value, nil
	}
	if path == "-" {
		return readStdinFileRef()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("@file reference %q: %w", value, err)
	}
	if len(data) > maxFileRefBytes {
		return "", fmt.Errorf("@file reference %q is %d bytes — over the %d byte limit; a payload this large should be shipped another way", value, len(data), maxFileRefBytes)
	}
	return string(data), nil
}

// ExpandFileRef TrimRight is intentionally NOT applied here: a briefing's
// trailing newline can be significant to the consumer. Callers that want
// trimming (the one-shot prompt path) do it themselves.

// readStdinFileRef reads the "@-" stdin expansion. Stdin is only read when
// it is piped (not a TTY) — a bare `--payload @-` on an interactive terminal
// would otherwise hang silently waiting for EOF.
func readStdinFileRef() (string, error) {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return "", fmt.Errorf("@- stdin reference: %w", err)
	}
	if fi.Mode()&os.ModeCharDevice != 0 {
		return "", fmt.Errorf("@- reads standard input, but stdin is a terminal — pipe the payload in (e.g. gopi task submit S --payload @- < briefing.md)")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("@- stdin reference: %w", err)
	}
	if len(data) > maxFileRefBytes {
		return "", fmt.Errorf("@- stdin payload is %d bytes — over the %d byte limit", len(data), maxFileRefBytes)
	}
	return string(data), nil
}
