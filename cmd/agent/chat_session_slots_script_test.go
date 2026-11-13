package main

// chat_session_slots_script_test.go — the REGISTRY's bound is read by a SHELL
// script, and the shell cannot import a Go constant. The two spellings of "10"
// and of the file name therefore live in two languages, and nothing in the
// compiler compares them: a limit raised here and left at 10 there would leave
// slots `gopi chat` fills that the sink never scans (notifications lost in
// silence — Rule 15's defect, one language over).
//
// So the script is the SUBJECT of a test rather than the subject of a comment:
// this test reads scripts/notify/notify.sh and pins its three declarations
// against the Go constants. It is the same shape as the environment gate in
// internal/standards: the constant is the single source of truth, and a second
// spelling is caught where it drifts.

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// notifyScriptRelPath is the sample sink, relative to this package directory
// (cmd/agent) — it is a shipped deliverable of this repository, not a file the
// test writes.
const notifyScriptRelPath = "../../scripts/notify/notify.sh"

// The declared parts, each spelled ONCE here (Rule 13/14): the script must
// EXPAND them, and the slot-name literal the pin looks for is BUILT from them,
// so renaming a part is a one-place change on both sides.
const (
	notifyScriptSlotLimitBound      = "$SLOT_LIMIT"
	notifyScriptSlotPrefixExpansion = "${SLOT_PREFIX}"
	notifyScriptSlotSuffixExpansion = "${SLOT_SUFFIX}"
)

// notifyScriptShellValue extracts `readonly NAME=...` (quoted or bare) from the
// script's text.
func notifyScriptShellValue(t *testing.T, body, name string) string {
	t.Helper()
	// Two alternations rather than one backreference: RE2 (Go's engine) has no
	// backreferences, so `("?)…\1` would not compile — and a quoted and a bare
	// spelling are the only two shapes the script may use.
	re := regexp.MustCompile(`(?m)^readonly ` + regexp.QuoteMeta(name) + `=(?:"([^"\n]*)"|([^"\n\s]*))\s*$`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("scripts/notify/notify.sh declares no `readonly %s=…` — the sink's contract cannot be pinned", name)
	}
	if m[1] != "" {
		return m[1]
	}
	return m[2]
}

// notifyScriptSlotLoopVar READS the name of the script's own slot-walking
// counter instead of pinning a spelling of it. The walk is an `IDENT=0`
// initialiser followed by a `while [ "$IDENT" -lt "$SLOT_LIMIT" ]` test, and the
// slot-name literal is built from IDENT — so a commit that renames the counter
// renames the literal with it. Measured 2026-10-12 (commit 9e8446fdd, the
// peer-pull edit that lifted the loop body into `deliver_all`): the counter went
// `i` -> `_da_i`, the literal `${SLOT_PREFIX}${i}${SLOT_SUFFIX}` went to
// `${SLOT_PREFIX}${_da_i}${SLOT_SUFFIX}`, and a pin that pinned the OLD spelling
// failed against a script that had built the name correctly — it asserted a
// literal no correct version of the script could carry.
func notifyScriptSlotLoopVar(body string) (string, bool) {
	init := regexp.MustCompile(`^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)=0[[:space:]]*$`)
	lines := strings.Split(body, "\n")
	for idx, line := range lines {
		m := init.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, next := range lines[idx+1:] {
			if strings.TrimSpace(next) == "" {
				continue
			}
			if strings.Contains(next, `"$`+m[1]+`"`) && strings.Contains(next, notifyScriptSlotLimitBound) {
				return m[1], true
			}
			break
		}
	}
	return "", false
}

// notifyScriptAssignedSlotName returns the script line that ASSIGNS a slot name
// built from the declared parts (`IDENT=… ${SLOT_PREFIX}${<loopVar>}${SLOT_SUFFIX}`),
// or "" when no line does. The ASSIGNMENT is what the pin is for: the same three
// parts inside a comment or inside a message string say nothing about the name
// the script actually writes, and the pre-fix pin was satisfied by either.
func notifyScriptAssignedSlotName(body, loopVar string) string {
	assign := regexp.MustCompile(`^[[:space:]]*(local[[:space:]]+)?[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=`)
	// The counter may be expanded either way, and BOTH are correct shell: the
	// shipped script writes the braced form. Spelling only the braced form here
	// would re-introduce the very defect this repairs — a pin that fails a
	// correct script over a spelling it did not think of (Rule 13's mirror is
	// the sibling shape: one definition, asked for by the predicate).
	shapes := []string{
		notifyScriptSlotPrefixExpansion + "${" + loopVar + "}" + notifyScriptSlotSuffixExpansion,
		notifyScriptSlotPrefixExpansion + "$" + loopVar + notifyScriptSlotSuffixExpansion,
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		shapeIdx := -1
		for _, shape := range shapes {
			if at := strings.Index(line, shape); at >= 0 && (shapeIdx < 0 || at < shapeIdx) {
				shapeIdx = at
			}
		}
		if shapeIdx < 0 {
			continue
		}
		if m := assign.FindStringIndex(line); m != nil && m[1] <= shapeIdx {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// notifyScriptBuildsSlotName is the pin's OWN verdict, in ONE place, so the
// regression arm can drive the SAME predicate the pin runs. That single point is
// the whole reason the repair holds: written twice, a verdict drifts and the arm
// would test an opinion the pin never consults; written once, a revert of this
// predicate to the pre-fix literal fails the ARM — over a body whose counter is
// renamed — and not only the pin on the shipped script. The arm's fixtures are
// the `_da_i` shape, so the literal is FALSE on them and the reverted predicate
// cannot pass (the pristine-master failure, reproduced inside the arm).
//
// It returns the counter it READ off the body and whether the name is built from
// it; a false verdict with a counter means "walked, but never built the name",
// which the pin reports differently from "never walked".
func notifyScriptBuildsSlotName(body string) (loopVar string, built bool) {
	loopVar, found := notifyScriptSlotLoopVar(body)
	if !found {
		return "", false
	}
	return loopVar, notifyScriptAssignedSlotName(body, loopVar) != ""
}

// TestNotifyScriptAgreesWithTheRegistryConstants is the cross-language pin: the
// script's slot limit and slot-name parts must equal the constants `gopi chat`
// registers with, and the script must actually READ the limits it declares
// (a declaration nothing consumes is a comment wearing a variable's name).
func TestNotifyScriptAgreesWithTheRegistryConstants(t *testing.T) {
	body, err := os.ReadFile(filepath.Clean(notifyScriptRelPath))
	if err != nil {
		t.Fatalf("read %s: %v", notifyScriptRelPath, err)
	}
	text := string(body)

	limit := notifyScriptShellValue(t, text, "SLOT_LIMIT")
	got, convErr := strconv.Atoi(limit)
	if convErr != nil {
		t.Fatalf("SLOT_LIMIT=%q is not an integer", limit)
	}
	if got != ChatSessionSlotLimit {
		t.Fatalf("notify.sh SLOT_LIMIT=%d, but gopi chat registers %d slots — the sink would miss slots %d..%d",
			got, ChatSessionSlotLimit, got, ChatSessionSlotLimit-1)
	}
	if prefix := notifyScriptShellValue(t, text, "SLOT_PREFIX"); prefix != ChatSessionSlotPrefix {
		t.Fatalf("notify.sh SLOT_PREFIX=%q, want %q", prefix, ChatSessionSlotPrefix)
	}
	if suffix := notifyScriptShellValue(t, text, "SLOT_SUFFIX"); suffix != ChatSessionSlotSuffix {
		t.Fatalf("notify.sh SLOT_SUFFIX=%q, want %q", suffix, ChatSessionSlotSuffix)
	}

	// The loop bound is the declared limit, and the name is built from the
	// declared parts: both must appear as expansions, not as bare literals the
	// declarations merely shadow.
	for _, want := range []string{notifyScriptSlotLimitBound, notifyScriptSlotPrefixExpansion, notifyScriptSlotSuffixExpansion} {
		if !strings.Contains(text, want) {
			t.Fatalf("notify.sh never expands %s — a declared bound nothing reads is a comment, not a contract", want)
		}
	}
	// THE FILE NAME THIS TEST ASSERTS ON: the script must DERIVE a slot's name
	// from the declared parts, so the name `chat_session_id_<n>.txt` has exactly
	// ONE spelling in each language. The counter is read off the script, not
	// pinned by name (see notifyScriptSlotLoopVar): what must not drift is that
	// the name is BUILT from the parts, not which identifier walks the slots.
	loopVar, built := notifyScriptBuildsSlotName(text)
	if !built {
		if loopVar == "" {
			t.Fatalf("notify.sh has no slot walk — no `IDENT=0` followed by a `while [ \"$IDENT\" -lt %s ]` test", notifyScriptSlotLimitBound)
		}
		t.Fatalf("notify.sh does not BUILD the slot name from %s — it walks with %q but the name is spelled somewhere else",
			notifyScriptSlotPrefixExpansion+"${"+loopVar+"}"+notifyScriptSlotSuffixExpansion, loopVar)
	}
	// The script must be runnable as the configured command: a shell that cannot
	// read it makes every delivery fail before the anchor.
	if fi, statErr := os.Stat(filepath.Clean(notifyScriptRelPath)); statErr != nil {
		t.Fatalf("stat %s: %v", notifyScriptRelPath, statErr)
	} else if fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("notify.sh mode is %o — the configured `notify.cmd` is exec'd, so it must be executable", fi.Mode().Perm())
	}
}
