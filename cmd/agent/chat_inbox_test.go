package main

// chat_inbox_test.go — the delivery box's minimal reproductions.
//
// Every arm here is a shape a LIVE chat turn can meet: a box that does not
// exist, a picture that goes out exactly once, a text file that becomes a note,
// and the two ways a file must be REFUSED rather than dropped. The last of
// those is the one worth having: a silent skip is the failure this file exists
// to prevent, so the refusal must be observable (Problems) AND the file must
// still be there for a human to fix.

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// chatInboxTestPNG returns a real, valid PNG's bytes.
func chatInboxTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 0xFF, A: 0xFF})
	img.Set(1, 1, color.RGBA{B: 0xFF, A: 0xFF})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

// chatInboxTestDrop writes name with data into the session's delivery box.
func chatInboxTestDrop(t *testing.T, sessionDir, name string, data []byte) string {
	t.Helper()
	dir := chatInboxPath(sessionDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir inbox: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestChatInboxAbsentDirectoryIsNoEvent pins the documented "no directory = no
// event": nothing is delivered, nothing is reported, and — the half that is
// easy to get wrong — no directory is CREATED as a side effect, so a chat run
// that never uses the box leaves no trace of it.
func TestChatInboxAbsentDirectoryIsNoEvent(t *testing.T) {
	sessionDir := t.TempDir()
	got := takeChatInbox(sessionDir)
	if !got.Empty() {
		t.Fatalf("delivery = %+v, want empty", got)
	}
	if len(got.Problems) != 0 {
		t.Fatalf("problems = %v, want none", got.Problems)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, chatInboxParentDirName)); !os.IsNotExist(err) {
		t.Fatalf("the scan created %q (err=%v); an absent box must stay absent", chatInboxParentDirName, err)
	}
	// No session directory at all is the same answer, not an error.
	if got := takeChatInbox(""); !got.Empty() || len(got.Problems) != 0 {
		t.Fatalf("no session dir: delivery = %+v", got)
	}
}

// TestChatInboxImageBecomesImageBlockAndIsConsumedOnce is the core arm: the
// picture leaves as a base64 image block (NOT as a path in the message), the
// file is MOVED to inbox-sent, and the very next turn's scan is empty — the
// consumption marker, which is what stops the same picture riding two turns.
func TestChatInboxImageBecomesImageBlockAndIsConsumedOnce(t *testing.T) {
	sessionDir := t.TempDir()
	raw := chatInboxTestPNG(t)
	path := chatInboxTestDrop(t, sessionDir, "shot.png", raw)

	got := takeChatInbox(sessionDir)
	if len(got.Problems) != 0 {
		t.Fatalf("problems = %v, want none", got.Problems)
	}
	if len(got.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(got.Images))
	}
	if got.Images[0].Type != "base64" || got.Images[0].MediaType != "image/png" {
		t.Fatalf("image source = %+v, want a base64 image/png block", got.Images[0])
	}
	if decoded, err := base64.StdEncoding.DecodeString(got.Images[0].Data); err != nil || !bytes.Equal(decoded, raw) {
		t.Fatalf("image block does not carry the file's bytes (err=%v)", err)
	}
	if got.Note != "" {
		t.Fatalf("note = %q, want empty for an image", got.Note)
	}
	if len(got.Delivered) != 1 || got.Delivered[0] != "shot.png" {
		t.Fatalf("delivered = %v, want [shot.png]", got.Delivered)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s is still in the inbox after delivery (err=%v)", path, err)
	}
	filed, globErr := filepath.Glob(filepath.Join(sessionDir, chatInboxParentDirName, chatInboxSentDirName, "*shot.png"))
	if globErr != nil || len(filed) != 1 {
		t.Fatalf("inbox-sent entries = %v (err=%v), want exactly one", filed, globErr)
	}

	// THE MARKER: the next turn must find nothing.
	next := takeChatInbox(sessionDir)
	if !next.Empty() || len(next.Problems) != 0 {
		t.Fatalf("second turn's delivery = %+v, want empty", next)
	}
}

// TestChatInboxTextBecomesANote pins the text channel: the file's NAME and its
// content ride the user message, and the file is consumed like an image.
func TestChatInboxTextBecomesANote(t *testing.T) {
	sessionDir := t.TempDir()
	chatInboxTestDrop(t, sessionDir, "note.txt", []byte("build failed on hp\n"))

	got := takeChatInbox(sessionDir)
	if len(got.Problems) != 0 {
		t.Fatalf("problems = %v, want none", got.Problems)
	}
	if len(got.Images) != 0 {
		t.Fatalf("images = %d, want 0 for a text file", len(got.Images))
	}
	if !strings.Contains(got.Note, "note.txt") || !strings.Contains(got.Note, "build failed on hp") {
		t.Fatalf("note = %q, want the file's name and content", got.Note)
	}
	joined := chatInboxTurnText("what happened?", got)
	if !strings.HasPrefix(joined, "what happened?") || !strings.Contains(joined, "build failed on hp") {
		t.Fatalf("turn text = %q, want the prompt followed by the note", joined)
	}
	if empty := chatInboxTurnText("bare", chatInboxDelivery{}); empty != "bare" {
		t.Fatalf("turn text without a note = %q, want the prompt unchanged", empty)
	}
}

// TestChatInboxRefusesBadFilesAndKeepsThem is the Rule 15 arm: a file that
// cannot be delivered is REPORTED (never silently skipped) and LEFT IN PLACE,
// so the next turn says the same thing until a human acts.
func TestChatInboxRefusesBadFilesAndKeepsThem(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		data     []byte
		wantWord string
	}{
		{
			name:     "extension lies",
			file:     "broken.png",
			data:     []byte("this is not a PNG at all"),
			wantWord: "not a png file",
		},
		{
			name:     "neither image nor text",
			file:     "blob.bin",
			data:     []byte{0x00, 0xFF, 0x00, 0xFE, 0x00},
			wantWord: "not an image",
		},
		{
			name:     "unsupported image format",
			file:     "vector.svg",
			data:     []byte("<svg/>"),
			wantWord: "", // an .svg is TEXT: it rides as a note, see below
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessionDir := t.TempDir()
			path := chatInboxTestDrop(t, sessionDir, tc.file, tc.data)
			got := takeChatInbox(sessionDir)
			if tc.wantWord == "" {
				// .svg is not in the image table, so it is text — deliverable.
				if len(got.Problems) != 0 || !strings.Contains(got.Note, "vector.svg") {
					t.Fatalf("delivery = %+v, want it delivered as a note", got)
				}
				return
			}
			if len(got.Problems) != 1 {
				t.Fatalf("problems = %v, want exactly one refusal", got.Problems)
			}
			if !strings.Contains(got.Problems[0], tc.wantWord) {
				t.Fatalf("problem = %q, want it to name %q", got.Problems[0], tc.wantWord)
			}
			if len(got.Images) != 0 || got.Note != "" {
				t.Fatalf("a refused file still reached a channel: %+v", got)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("%s was consumed despite the refusal (err=%v)", path, err)
			}
		})
	}
}

// TestChatInboxDirectoryEntryIsRefusedNotFollowed pins the one non-regular
// shape: a directory inside the box is reported and left alone (delivering it
// is impossible; dropping it silently is the defect).
func TestChatInboxDirectoryEntryIsRefusedNotFollowed(t *testing.T) {
	sessionDir := t.TempDir()
	dir := chatInboxPath(sessionDir)
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := takeChatInbox(sessionDir)
	if len(got.Problems) != 1 || !strings.Contains(got.Problems[0], "not a regular file") {
		t.Fatalf("problems = %v, want one 'not a regular file' refusal", got.Problems)
	}
	if _, err := os.Stat(filepath.Join(dir, "nested")); err != nil {
		t.Fatalf("the directory was removed despite the refusal (err=%v)", err)
	}
}

// TestChatInboxDotfileIsIgnored pins the one entry that is neither delivered nor
// reported: a dot-prefixed file is someone else's droppings, not a delivery, and
// it is not consumed either (a human decides what it is).
func TestChatInboxDotfileIsIgnored(t *testing.T) {
	sessionDir := t.TempDir()
	path := chatInboxTestDrop(t, sessionDir, ".DS_Store", []byte{0x00, 0x01})
	got := takeChatInbox(sessionDir)
	if !got.Empty() || len(got.Problems) != 0 {
		t.Fatalf("delivery = %+v, want nothing delivered and nothing reported", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the dotfile was consumed (err=%v)", err)
	}
}
