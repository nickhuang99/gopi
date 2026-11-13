package main

// cli_notify_deliver_spill_test.go — the DELIVER face's SIZE GATE: a message
// larger than the input line's own rune bound is SPILLED to the session's paste
// directory and the inbox carries the REFERENCE, never the payload bytes.
//
// WHAT ONLY THIS FILE CAN SHOW. internal/lineedit's own cases prove the reference
// spelling and the store; nothing else proves that the CLI verb's gate fires on
// the real threshold, that the file lands where a running session's editor spills
// its own pastes, and that the record the session will read at its next round
// boundary names the payload instead of quoting it.
//
// THE DEFECT IT GUARDS is measured, not hypothetical: before the gate a 100 KB
// `--message` was appended verbatim to `<session>/messages/inbox.jsonl`, a file
// every round boundary of that session reads.
//
// THE SHORT CASE IS HALF THE DELIVERABLE. A gate that spilled everything would
// pass a large-message assertion and quietly change every ordinary delivery, so
// the below-bound case asserts the historic path BYTE FOR BYTE: the record IS the
// message and nothing is written to disk.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/tools"
)

const (
	// notifySpillSessionID is the recipient's id; its directory under the
	// sessions root carries a record, so the verb resolves it by the bare-name
	// arm every real chat session uses.
	notifySpillSessionID = "notify-spill-fixture"
	// notifySpillBodyRunes is the spilled message's size in RUNES: one past the
	// input line's own bound, which is the threshold under test. Kept at the
	// boundary rather than at a megabyte so this case measures the GATE and not
	// the store's throughput.
	notifySpillBodyRunes = lineedit.InputLineRuneBound + 1
)

// notifySpillHome materialises a profile whose sessions root holds ONE session
// directory with a record in it — the shape `gopi notify --session <id>`
// resolves by bare directory name.
func notifySpillHome(t *testing.T, sessionID string) (home, dir string) {
	t.Helper()
	home = t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	dir = filepath.Join(home, config.ProfileDirName, config.SessionsDirName, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionlayout.RecordName), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write session record: %v", err)
	}
	return home, dir
}

// notifySpillEnv is the isolated environment for a binary-level delivery: the
// shared scrub (no ambient GOPI_*, no operator sink) plus this case's profile.
func notifySpillEnv(t *testing.T, home string) []string {
	t.Helper()
	return append(notifyScrubbedEnv(),
		"GOPI_HOME="+home,
		"GOPI_SETTINGS="+filepath.Join(home, "settings.json"),
	)
}

// notifySpillFiles returns every spilled payload under a session directory, in
// walk order. The directory name comes from the production constant, so a gate
// that started spilling somewhere else would not be found by this probe.
func notifySpillFiles(t *testing.T, dir string) []string {
	t.Helper()
	pasteDir := filepath.Join(dir, lineedit.PasteDirName)
	var out []string
	entries, err := os.ReadDir(pasteDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read paste dir %s: %v", pasteDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, filepath.Join(pasteDir, e.Name()))
		}
	}
	return out
}

// notifyInboxRaw reads the session's inbox, or "" when nothing was written.
func notifyInboxRaw(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(tools.SessionInboxPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read inbox: %v", err)
	}
	return string(raw)
}

// TestNotifyDeliverShortMessageTakesTheHistoricPath is the negative half: a
// message under the bound reaches the inbox verbatim, with no reference and no
// file written anywhere in the session directory.
func TestNotifyDeliverShortMessageTakesTheHistoricPath(t *testing.T) {
	const text = "NOTIFY-SHORT-1c4a: an ordinary message"
	home, dir := notifySpillHome(t, notifySpillSessionID)

	stdout, stderr, err := runNotifyDeliverArgv(t, notifySpillEnv(t, home),
		notifySessionFlag, notifySpillSessionID, notifyMessageFlag, text)
	if err != nil {
		t.Fatalf("gopi notify failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "queued: message ") {
		t.Fatalf("the delivery was not confirmed:\n%s", stdout)
	}
	if !strings.Contains(notifyInboxRaw(t, dir), text) {
		t.Errorf("the inbox does not carry the message verbatim:\n%s", notifyInboxRaw(t, dir))
	}
	// why: correct — set = the <paste> dir's entries, and the guard that must not be too broad is notifyDeliverBody's own threshold: it returns at lineedit.ExceedsInputLineBound BEFORE any PasteStore is built (cli_notify.go), so the spill block is unreachable for this text and nothing spill-like can be wrongly excluded.
	// Had the refusal not fired, this message's bytes would be a second on-disk copy in the session dir while the inbox still carried them verbatim; the positive half is the large-message sibling, crossing the SAME bound and asserting the spill DOES land.
	if files := notifySpillFiles(t, dir); len(files) != 0 {
		t.Errorf("a below-threshold message spilled %v — the historic path must write no file", files)
	}
	if strings.Contains(stdout, "payload:") {
		t.Errorf("the delivery reported a payload for a message that was not spilled:\n%s", stdout)
	}
}

// TestNotifyDeliverLargeMessageSpillsAndReferences is the acceptance: the
// payload lands in `<session>/paste/`, the inbox record carries a reference that
// NAMES it with source and size, and the record does not quote the bytes.
func TestNotifyDeliverLargeMessageSpillsAndReferences(t *testing.T) {
	text := strings.Repeat("z", notifySpillBodyRunes)
	home, dir := notifySpillHome(t, notifySpillSessionID)

	stdout, stderr, err := runNotifyDeliverArgv(t, notifySpillEnv(t, home),
		notifySessionFlag, notifySpillSessionID, notifyMessageFlag, text)
	if err != nil {
		t.Fatalf("gopi notify failed on a large message: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "queued: message ") {
		t.Fatalf("the spilled delivery was not confirmed:\n%s", stdout)
	}

	files := notifySpillFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("want exactly one spilled payload under %s, got %v", filepath.Join(dir, lineedit.PasteDirName), files)
	}
	payload, readErr := os.ReadFile(files[0])
	if readErr != nil {
		t.Fatalf("read the spilled payload: %v", readErr)
	}
	if string(payload) != text {
		t.Errorf("the payload is not byte-exact: %d bytes on disk, %d sent", len(payload), len(text))
	}

	inbox := notifyInboxRaw(t, dir)
	info, ok := lineedit.PasteRefInfoFor(inbox)
	if !ok {
		t.Fatalf("the inbox record carries no paste reference:\n%s", inbox)
	}
	if info.Path != files[0] {
		t.Errorf("the reference names %q, the payload is at %q", info.Path, files[0])
	}
	if info.Source != inputsource.Notify {
		t.Errorf("the reference's source is %q, want %q", info.Source, inputsource.Notify)
	}
	if info.Size != len(payload) {
		t.Errorf("the reference's size is %d, the payload is %d bytes", info.Size, len(payload))
	}
	if info.Runes != notifySpillBodyRunes {
		t.Errorf("the reference's rune count is %d, want %d", info.Runes, notifySpillBodyRunes)
	}
	// THE RECORD IS NOT THE PAYLOAD: quoting it would be the defect the gate
	// exists to prevent, and a record that both quotes and references would have
	// passed every assertion above.
	if strings.Contains(inbox, text[:128]) {
		t.Errorf("the inbox record quotes the payload it was supposed to reference:\n%s", inbox)
	}
	if !strings.Contains(stdout, "payload:") {
		t.Errorf("the delivery did not report where the payload went:\n%s", stdout)
	}
}

// TestNotifyDeliverBodyRefusesWhenItCannotSpill pins the Rule 15 half: a
// payload that cannot be written is a REFUSAL, never a downgrade to the raw
// append that is the defect the gate exists for.
func TestNotifyDeliverBodyRefusesWhenItCannotSpill(t *testing.T) {
	message := strings.Repeat("y", notifySpillBodyRunes)
	body, payload, err := notifyDeliverBody("", message)
	if err == nil {
		t.Fatalf("an unsplillable message was accepted (body %q, payload %q)", body, payload)
	}
	if body != "" || payload != "" {
		t.Errorf("the refusal returned a body (%q) or a payload (%q)", body, payload)
	}
	if !strings.Contains(err.Error(), "NOT delivered") {
		t.Errorf("the refusal does not say the message was not delivered: %v", err)
	}
	if !strings.Contains(err.Error(), lineedit.PasteRefKeyword) && !strings.Contains(err.Error(), "larger than") {
		t.Errorf("the refusal does not name the size gate: %v", err)
	}
}

// TestNotifyDeliverBodyBelowBoundIsTheMessageItself is the in-process half of
// the short case: the body IS the message and no payload path is reported.
func TestNotifyDeliverBodyBelowBoundIsTheMessageItself(t *testing.T) {
	const short = "below the bound"
	body, payload, err := notifyDeliverBody(t.TempDir(), short)
	if err != nil {
		t.Fatalf("notifyDeliverBody on a short message: %v", err)
	}
	if body != short || payload != "" {
		t.Errorf("short message returned (%q, %q), want (%q, \"\")", body, payload, short)
	}
}
