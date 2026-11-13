package main

// chat_film_frames.go — the frames the film WRITES, made even.
//
// THE DEFECT THIS FILE CLOSES (measured 2026-10-04). The film writes the X11
// window grab verbatim (chat_film.go's shootBeat writes grab.PNG straight to
// frame-NNNN.png), and a window's pixel size is the terminal's FACE times the
// cell count, not the cell count:
//
//	--film-terminal xterm          26 rows × 13 px/cell + 4 = 342 px (even)
//	(no --film-terminal) xfce4     26 rows × 19.88 px/cell + 4 = 521 px (ODD)
//
// Same 82 columns, same 26 rows, two different heights — and the second is ODD.
// An odd height is not a cosmetic detail: every downstream video encoder that
// works in yuv420p subsamples chroma 2×2 and refuses an odd frame dimension
// outright, so a film whose height is 521 cannot be encoded at all. The
// predecessor of this change narrowed WIDTH for the same class of reason (a
// reader's limit) and left height alone, because height parity is not a limit
// the phone states — it is a limit the ENCODER states, and the film is the
// encoder's input.
//
// WHY A CROP AND NOT A RESAMPLE. The fix removes at most ONE column at the
// right and ONE row at the bottom — the smallest change that makes the
// dimension even. Every surviving pixel is THE SAME BYTE it was in the grab:
// there is no interpolation, no scaling kernel, no second renderer, so the
// frame's pixels are still the real window's pixels (which is the whole of what
// this film exists to carry — devdoc/chat-film-pixels-are-the-surface.md).
// Resampling to an even size would re-introduce exactly the artifact the width
// half of this contract was built to avoid.
//
// WHY IT IS A NO-OP ON THE REFERENCE FACE. At xterm's face the canvas the
// playable narrowness produces is 496×342 — already even — so this function
// returns the ORIGINAL grab bytes untouched and the default product is
// byte-identical to what it was before this file existed. That property is
// pinned by a test, because it is the acceptance's own criterion for "without
// --wechat the product is unchanged", and an "improvement" that silently
// re-encoded every frame would break it.

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
)

// evenFramePNG holds a frame to the even-dimension contract above and reports
// what it did, so the caller can SAY it (Rule 15: a picture whose geometry
// moved is a fact about the deliverable, never a silent correction).
//
// The returned width/height are the frame's dimensions AFTER the call — the
// caller reports those, never the ones it hoped for. `cropped` is true only
// when bytes were actually removed; on a frame that was already even, `out` IS
// `raw` (the same slice), so the value written down is the value the display
// produced.
type evenFrame struct {
	Out     []byte
	Width   int
	Height  int
	SrcW    int
	SrcH    int
	Cropped bool
}

// chatFilmEvenFrame parses ONE photographed frame and returns its bytes with
// both dimensions even. The parity it targets is the film's own named constant
// (chatFilmFrameParity) — the same number the delivery gate checks the finished
// animation against, referenced rather than restated (Rules 13/14).
func chatFilmEvenFrame(raw []byte) (evenFrame, error) {
	src, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return evenFrame{}, fmt.Errorf("the frame bytes are not a PNG this run can measure: %w", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	ew, eh := evenDim(w), evenDim(h)
	if ew == w && eh == h {
		// NOTHING TO DO — and returning the ORIGINAL bytes is the point: a
		// re-encode here would be a lossless-looking change that is not the
		// display's own output any more.
		return evenFrame{Out: raw, Width: w, Height: h, SrcW: w, SrcH: h}, nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, ew, eh))
	// draw.Src copies the top-left ew×eh rectangle PIXEL FOR PIXEL: no scaling,
	// no blending, no gamma — the smallest change that yields an even frame.
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return evenFrame{}, fmt.Errorf("encode the %dx%d frame: %w", ew, eh, err)
	}
	return evenFrame{Out: buf.Bytes(), Width: ew, Height: eh, SrcW: w, SrcH: h, Cropped: true}, nil
}

// evenDim is the largest even dimension at or below n. A value below the parity
// cannot yield an even positive dimension, so it is returned unchanged and the
// encode below fails LOUDLY on a zero-width image rather than this function
// inventing a size nobody measured.
func evenDim(n int) int {
	if n < chatFilmFrameParity {
		return n
	}
	return n - n%chatFilmFrameParity
}
