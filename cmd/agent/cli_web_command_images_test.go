package main

// cli_web_command_images_test.go — the line that was missing: a SLASH COMMAND'S
// RENDERED PICTURE now reaches the model.
//
// The defect this closes (measured 2026-11-07): `/text2img` rendered a PNG and
// returned the PATH as text. The page drew the path, the next turn shipped
// nothing, and the model never saw the picture — so the whole point of
// rendering (a picture costs a fixed token budget, prose costs per character)
// was lost.
//
// Four arms, each on the REAL surface rather than on a helper:
//
//  1. the command frame carries the rendered page as one {media_type,data_b64}
//     — the same object /api/shot returns, so the page needs no translation;
//  2. THE CHAIN: the picture the frame carried, posted back through /api/chat
//     the way the page ships it, arrives at the model as a Files API reference
//     with NO inline base64 — this is the acceptance criterion, asserted on the
//     captured request body;
//  3. an unreadable, oversize or non-PNG page is an EXPLICIT refusal naming the
//     file (Rule 15) — never a path printed as if the picture had been sent;
//  4. the page's own JS stages the frame's pictures into images[] and draws
//     them (the harness scenario file drives the shipped script).

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// cmdFrame is the `event: command` body the page parses.
type cmdFrame struct {
	Command string         `json:"command"`
	Output  string         `json:"output"`
	Error   string         `json:"error"`
	Images  []webChatImage `json:"images"`
}

// commandFrameOf drives one slash input through the REAL /api/chat handler and
// returns its command frame. It fails when the turn produced a model frame —
// a slash command must never spend a model call (R4).
func commandFrameOf(t *testing.T, ws *webServer, line string) cmdFrame {
	t.Helper()
	body := postChat(t, ws, line, nil)
	data, ok := sseDataOf(body, webEventCommand)
	if !ok {
		t.Fatalf("%q: no command frame (body %q)", line, body)
	}
	if strings.Contains(body, "event: "+webEventDelta) {
		t.Errorf("%q: a slash command must not run a model turn (body %q)", line, body)
	}
	var f cmdFrame
	if err := json.Unmarshal([]byte(data), &f); err != nil {
		t.Fatalf("%q: bad command frame payload %q: %v", line, data, err)
	}
	return f
}

// TestText2ImgCommandFrameCarriesTheRenderedPage is the frame-shape arm: the
// PNG the command rendered is ON the frame, as the object /api/chat accepts.
func TestText2ImgCommandFrameCarriesTheRenderedPage(t *testing.T) {
	ws, _ := filesTestServer(t, http.StatusOK)
	out := filepath.Join(t.TempDir(), "slash-page.png")

	f := commandFrameOf(t, ws, "/text2img --out "+out+" GOPI-FRAME-CHECK")
	if f.Error != "" {
		t.Fatalf("/text2img failed: %s", f.Error)
	}
	if !strings.Contains(f.Output, out) {
		t.Errorf("frame output does not name the rendered file (%q)", f.Output)
	}
	if len(f.Images) != 1 {
		t.Fatalf("frame images = %d, want exactly 1 (the rendered page)", len(f.Images))
	}
	if f.Images[0].MediaType != webShotMediaType {
		t.Errorf("frame image media_type = %q, want %q", f.Images[0].MediaType, webShotMediaType)
	}
	// The frame's bytes must BE the file the renderer wrote — a frame carrying
	// some other picture would render, ship and prove nothing.
	onDisk, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the rendered page: %v", err)
	}
	if len(onDisk) == 0 {
		t.Fatal("the renderer wrote an empty file")
	}
	sent, err := base64.StdEncoding.DecodeString(f.Images[0].DataB64)
	if err != nil {
		t.Fatalf("frame image data_b64 does not decode: %v", err)
	}
	if !bytes.Equal(sent, onDisk) {
		t.Errorf("frame image bytes != the rendered file (frame %d bytes, file %d bytes)", len(sent), len(onDisk))
	}
}

// TestText2ImgCommandPictureReachesTheModel is the acceptance arm, end to end
// over the real handlers: the frame's picture is posted back as a turn exactly
// as the page posts it, and the MODEL REQUEST carries a file reference with no
// inline bytes.
//
// Both halves are asserted on the SAME captured body: a test that checked
// "file_id present" alone would stay green while the pixels rode along beside
// it.
func TestText2ImgCommandPictureReachesTheModel(t *testing.T) {
	ws, cap := filesTestServer(t, http.StatusOK)
	out := filepath.Join(t.TempDir(), "slash-page.png")

	frame := commandFrameOf(t, ws, "/text2img --out "+out+" GOPI-CHAIN-CHECK")
	if len(frame.Images) != 1 {
		t.Fatalf("frame images = %d, want 1", len(frame.Images))
	}

	// What the page does with that frame: push into images[], then send.
	if body := postImageTurn(t, ws, "read the picture the command drew", frame.Images); !strings.Contains(body, "event: "+webEventDone) {
		t.Fatalf("the picture turn did not complete (body %q)", body)
	}

	bodies, betas, uploads := cap.snapshot()
	if uploads != 1 {
		t.Fatalf("uploads = %d, want exactly 1 (the command's picture, uploaded once)", uploads)
	}
	if len(bodies) != 1 {
		t.Fatalf("model requests = %d, want exactly 1", len(bodies))
	}
	wire := string(bodies[0])
	if !strings.Contains(wire, `"type":"image"`) || !strings.Contains(wire, `"type":"file"`) {
		t.Errorf("the model request carries no image/file reference: %s", wire)
	}
	if !strings.Contains(wire, filesMockFileIDPrefix) {
		t.Errorf("the model request does not reference the uploaded file id: %s", wire)
	}
	onDisk, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read the rendered page: %v", err)
	}
	if b64 := base64.StdEncoding.EncodeToString(onDisk); strings.Contains(wire, b64) {
		t.Error("the model request carries the picture's INLINE base64 — the Files API substitution did not happen")
	}
	if betas[0] == "" {
		t.Errorf("the model request carries no %s header, so the file reference is not legal on this endpoint", deepseek.FilesAPIBetaHeader)
	}
}

// TestText2ImgCommandRefusesAnUnusablePage is the failure arm (Rule 15): a page
// that cannot be read back is REFUSED BY NAME, never printed as a path the
// operator would read as "sent".
//
// Every case is a REAL file on disk, because the branch's whole subject is
// what happens to bytes that arrived from a renderer.
func TestText2ImgCommandRefusesAnUnusablePage(t *testing.T) {
	dir := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\n" + "fixture-bytes")
	good := filepath.Join(dir, "good.png")
	if err := os.WriteFile(good, png, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	notPNG := filepath.Join(dir, "not-a-png.png")
	if err := os.WriteFile(notPNG, []byte("plain text pretending to be a picture"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	oversize := filepath.Join(dir, "huge.png")
	{
		f, err := os.Create(oversize)
		if err != nil {
			t.Fatalf("create fixture: %v", err)
		}
		// One byte past the ceiling is all the reader needs to see; writing it
		// as a sparse file costs no disk and no time (no load is generated —
		// this is a file size, not a device stress).
		if err := f.Truncate(webMaxChatImageBytes + 1); err != nil {
			f.Close() //stylegate:ignore closing a fixture file whose truncate already failed
			t.Fatalf("truncate fixture: %v", err)
		}
		f.Close() //stylegate:ignore closing a fixture file; no write to flush
	}
	missing := filepath.Join(dir, "never-written.png")

	cases := []struct {
		name string
		path string
		want string
	}{
		{"unreadable", missing, "无法读取"},
		{"oversize", oversize, "上限"},
		{"not a png", notPNG, "不是 PNG"},
	}
	for _, c := range cases {
		got, _, err := webSlashCommandImages([]string{c.path})
		if err == nil {
			t.Errorf("%s: accepted %s (returned %d images) — an unusable page must be refused", c.name, c.path, len(got))
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: refusal %q does not say %q", c.name, err.Error(), c.want)
		}
		if !strings.Contains(err.Error(), c.path) {
			t.Errorf("%s: refusal %q does not name the file %s", c.name, err.Error(), c.path)
		}
	}

	// The good fixture still goes through, so the refusals above are the
	// VALIDATOR refusing, not the function refusing everything.
	got, note, err := webSlashCommandImages([]string{good})
	if err != nil || len(got) != 1 || note != "" {
		t.Fatalf("a valid PNG was not attached: images=%d note=%q err=%v", len(got), note, err)
	}
	if got[0].MediaType != webShotMediaType {
		t.Errorf("media type = %q, want %q", got[0].MediaType, webShotMediaType)
	}
}

// TestText2ImgCommandPageOverTheImageBudget is the truncation arm: more pages
// than one turn may carry attaches the ones it can AND says so. A silent
// truncation would be the same defect in a new place (a command that drew five
// pages looking like it drew three).
func TestText2ImgCommandPageOverTheImageBudget(t *testing.T) {
	dir := t.TempDir()
	png := []byte("\x89PNG\r\n\x1a\n" + "fixture-bytes")
	paths := make([]string, 0, webMaxImagesPerRequest+2)
	for i := 0; i < webMaxImagesPerRequest+2; i++ {
		p := filepath.Join(dir, "page-"+string(rune('a'+i))+".png")
		if err := os.WriteFile(p, png, 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		paths = append(paths, p)
	}
	got, note, err := webSlashCommandImages(paths)
	if err != nil {
		t.Fatalf("over-budget pages must attach what fits, not fail: %v", err)
	}
	if len(got) != webMaxImagesPerRequest {
		t.Errorf("attached %d pages, want the %d-page cap", len(got), webMaxImagesPerRequest)
	}
	if note == "" {
		t.Error("the truncation was SILENT — a render that lost pages must say how many")
	}
	if !strings.Contains(note, "2") {
		t.Errorf("note %q does not say how many pages fell out (2)", note)
	}
}
