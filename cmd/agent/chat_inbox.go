package main

// THE CHAT DELIVERY BOX — <session-dir>/attachments/inbox/.
//
// The CLI's positional arguments and the web face's JSON `images` both hand an
// image to the model as a native image content block (loop.go's Run/Chat take
// `images ...*deepseek.ClaudeImageSource`; the vision model sees the PIXELS,
// never a path string). An INTERACTIVE `gopi chat` had no way to receive one:
// the operator types prose, and a screenshot taken by another process had no
// door into the turn.
//
// This file is that door, and it changes nothing about the wire: a turn whose
// inbox holds images calls the SAME chat entry point with the SAME variadic
// images the two existing faces already pass. Nothing here writes a path into
// the user message — an image leaves this file as base64 bytes and arrives at
// the model as an image block.
//
// THE CONSUMPTION MARKER is the move itself: a delivered file is moved out of
// the inbox into <session-dir>/attachments/inbox-sent/<stamp>-<name> in the
// same turn that attaches it, so the next turn's scan finds an empty inbox and
// the same picture is never attached twice. The move — not a sidecar flag —
// is what makes that durable: there is no second file that can disagree with
// the attachment, and a crash between "attached" and "marked" cannot leave a
// delivered image marked undelivered (it leaves it in the inbox, which is the
// honest state: it may or may not have gone out, and it will be sent again
// rather than silently lost).
//
// RECLAIM (Rule 35): inbox-sent/ is INSIDE the session's own directory, which
// every session-owned artifact already lives under and which the session's
// lifecycle reclaims. Nothing new is created outside it, and nothing grows
// unboundedly: a delivery MOVES bytes it already had, so the total on disk is
// what the operator put there, not a second copy of it.
//
// A FILE THAT CANNOT BE DELIVERED IS REPORTED AND LEFT IN PLACE. It is never
// deleted and never silently skipped: the problem is printed (problemes are
// the turn's own stderr notice) and the file stays in the inbox, so the next
// turn says the same thing again until a human fixes or removes it. That is
// Rule 15 applied to the delivery box: the failure is louder than the feature.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/tools"
)

const (
	// chatInboxParentDirName is the attachments level of a session directory —
	// the same name a task session's own attachments live under
	// (internal/tools' taskAttachmentsDirName), because both are "what a run
	// was handed" in one session's folder.
	chatInboxParentDirName = "attachments"

	// chatInboxDirName is where a sender drops a file for the next turn.
	chatInboxDirName = "inbox"

	// chatInboxSentDirName is where a delivered file is MOVED to. It is the
	// consumption marker: the inbox holds only what has not gone out yet.
	chatInboxSentDirName = "inbox-sent"

	// chatInboxNoteHeader wraps a text attachment's content inside the turn's
	// user message. The file's NAME travels; its path does not — a path in the
	// message is the string-instead-of-pixels defect this file exists to
	// avoid, and for text the content is the payload anyway.
	chatInboxNoteHeader = "[inbox attachment %q]\n"

	// chatInboxSignatureBytes is how many leading bytes the format check
	// reads. The longest signature in use is WebP's "RIFF....WEBP" (12).
	chatInboxSignatureBytes = 12
)

// chatInboxDelivery is one turn's delivery-box contents, already split into
// the two channels a chat turn has: image blocks (pixels) and notes (text).
type chatInboxDelivery struct {
	// Images ride the turn's `images ...` parameter.
	Images []*deepseek.ClaudeImageSource
	// Note is appended to the turn's user message; empty when the inbox held
	// no readable text.
	Note string
	// Delivered names what this turn attached (base names, in scan order).
	Delivered []string
	// Problems names what could NOT be delivered, with the reason. Those
	// files are LEFT IN the inbox (see the file header).
	Problems []string
}

// Empty reports whether the box handed this turn nothing at all.
func (d chatInboxDelivery) Empty() bool {
	return len(d.Images) == 0 && d.Note == ""
}

// chatInboxPath is the delivery box of the session directory sessionDir, or
// "" when there is no session directory (a run that keeps no record has no box,
// which is the same answer as an absent directory: nothing happens).
func chatInboxPath(sessionDir string) string {
	if strings.TrimSpace(sessionDir) == "" {
		return ""
	}
	return filepath.Join(sessionDir, chatInboxParentDirName, chatInboxDirName)
}

// chatInboxSentPath is where a delivered file is filed, stamped so two
// deliveries of the same file NAME never overwrite each other.
func chatInboxSentPath(sessionDir, name string, stamp time.Time) string {
	return filepath.Join(sessionDir, chatInboxParentDirName, chatInboxSentDirName,
		fmt.Sprintf("%s-%s", stamp.Format("20060102T150405.000000000"), name))
}

// takeChatInbox scans and CONSUMES the delivery box for one turn.
//
// Absent directory = nothing happens (the documented "directory does not exist
// = no event": no error, no notice, no directory created). An entry that cannot
// be delivered is reported in Problems and left untouched; the rest of the box
// still goes out, because one unreadable file must not silence a whole turn.
func takeChatInbox(sessionDir string) chatInboxDelivery {
	var out chatInboxDelivery
	dir := chatInboxPath(sessionDir)
	if dir == "" {
		return out
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out
		}
		out.Problems = append(out.Problems, fmt.Sprintf("%s: %v", dir, err))
		return out
	}
	// Deterministic order: a directory read is sorted by name, so a turn's
	// images ride in the order a sender named them.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	stamp := time.Now()
	var notes []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			// A dotfile is not a delivery (an editor's droppings, a .DS_Store);
			// it is not consumed either — a human decides what it is.
			continue
		}
		path := filepath.Join(dir, name)
		if e.IsDir() || !e.Type().IsRegular() {
			out.Problems = append(out.Problems,
				fmt.Sprintf("%s: not a regular file (only files can be delivered)", path))
			continue
		}
		if tools.IsImagePath(path) {
			img, ierr := tools.LoadImage(path)
			if ierr != nil {
				out.Problems = append(out.Problems, ierr.Error())
				continue
			}
			if serr := chatInboxCheckImageSignature(path, img.MediaType); serr != nil {
				out.Problems = append(out.Problems, serr.Error())
				continue
			}
			// THE IMAGE CHANNEL: base64 bytes become an image content block.
			// A path never enters the message.
			out.Images = append(out.Images, &deepseek.ClaudeImageSource{
				Type:      "base64",
				MediaType: img.MediaType,
				Data:      img.Base64,
			})
		} else {
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				out.Problems = append(out.Problems, fmt.Sprintf("%s: %v", path, rerr))
				continue
			}
			if !utf8.Valid(data) {
				// Not an image, not text: there is no third channel, so this is
				// reported rather than dropped (the box never guesses).
				out.Problems = append(out.Problems,
					fmt.Sprintf("%s: unsupported attachment — not an image (jpg/jpeg/png/gif/webp) and not UTF-8 text", path))
				continue
			}
			notes = append(notes, fmt.Sprintf(chatInboxNoteHeader, name)+string(data))
		}
		// CONSUME: the move is the marker, and it happens only after the bytes
		// are in hand. A failed move is reported and the file stays.
		if mvErr := os.MkdirAll(filepath.Dir(chatInboxSentPath(sessionDir, name, stamp)), 0o755); mvErr != nil {
			out.Problems = append(out.Problems, fmt.Sprintf("%s: cannot file a delivered attachment: %v", path, mvErr))
			continue
		}
		if mvErr := os.Rename(path, chatInboxSentPath(sessionDir, name, stamp)); mvErr != nil {
			out.Problems = append(out.Problems, fmt.Sprintf("%s: cannot file a delivered attachment: %v", path, mvErr))
			continue
		}
		out.Delivered = append(out.Delivered, name)
	}
	out.Note = strings.Join(notes, "\n\n")
	return out
}

// chatInboxCheckImageSignature refuses an image whose BYTES contradict its
// extension. tools.LoadImage validates the extension and the size only, so a
// garbage file named shot.png would otherwise reach the wire and be rejected
// there — a failure the operator would read as "the model ignored my image".
// The check is the two strings that matter, named rather than typed at a call
// site: the media type the extension promised, and the bytes actually there.
func chatInboxCheckImageSignature(path, mediaType string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer func() { _ = f.Close() }() //stylegate:ignore the read is complete; a close error on a read-only handle reports nothing the caller can act on
	head := make([]byte, chatInboxSignatureBytes)
	n, _ := f.Read(head) //stylegate:ignore a short read is exactly what an empty/truncated file gives, and the signature comparison below refuses it by name
	head = head[:n]
	if !chatInboxSignatureMatches(mediaType, head) {
		return fmt.Errorf("%s: declared %s by its extension, but the bytes are not a %s file — the image would be refused by the API",
			path, mediaType, strings.TrimPrefix(mediaType, "image/"))
	}
	return nil
}

// chatInboxSignatureMatches compares the leading bytes against the signature
// the declared media type requires.
func chatInboxSignatureMatches(mediaType string, head []byte) bool {
	has := func(sig ...byte) bool {
		if len(head) < len(sig) {
			return false
		}
		for i, b := range sig {
			if head[i] != b {
				return false
			}
		}
		return true
	}
	switch mediaType {
	case "image/png":
		return has(0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A)
	case "image/jpeg":
		return has(0xFF, 0xD8, 0xFF)
	case "image/gif":
		return has('G', 'I', 'F', '8')
	case "image/webp":
		// "RIFF" <4-byte size> "WEBP"
		return has('R', 'I', 'F', 'F') && len(head) >= 12 &&
			head[8] == 'W' && head[9] == 'E' && head[10] == 'B' && head[11] == 'P'
	}
	return false
}

// chatInboxTurnText appends the box's text notes to the turn's message.
func chatInboxTurnText(prompt string, d chatInboxDelivery) string {
	if d.Note == "" {
		return prompt
	}
	if strings.TrimSpace(prompt) == "" {
		return d.Note
	}
	return prompt + "\n\n" + d.Note
}

// reportChatInbox prints what this turn's delivery box did. Delivered files are
// named (the operator can see the picture arrived); problems are printed as
// problems and are NOT dressed up as delivery.
func reportChatInbox(d chatInboxDelivery) {
	if len(d.Delivered) > 0 {
		chatNoticef("[gopi] inbox: attached %d file(s): %s\n", len(d.Delivered), strings.Join(d.Delivered, ", "))
	}
	for _, p := range d.Problems {
		chatNoticef("[gopi] inbox: NOT attached — %s\n", p)
	}
}
