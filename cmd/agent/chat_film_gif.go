package main

// chat_film_gif.go — the FILM's animation arm: ONE command line, one playable GIF.
//
//	gopi chat --replay <session> --film DIR --out FILE.gif
//
// WHAT WAS MISSING. `--film` (chat_film.go) already photographs the replayed
// session once per drawn beat and leaves a directory of numbered PNGs plus the
// beat ledger beside them. What it did NOT do was produce the artifact an
// operator actually hands to someone: an animation. The frames were the film's
// own record and the animation was a second command away (`gopi tool gif --frames
// <every png in order> --out …`), which is exactly the shape this epic removes —
// the pixels were already captured and the encoder already exists.
//
// WHY internal/gifgen AND NOTHING ELSE. It is the backend behind `gopi tool gif`
// (Rule 3's boundary: no new verb, no new tool), it draws its captions with
// internal/text2img (the repository's ONE rasteriser), and it is pure Go over
// the standard library's image/gif. Rule 35: this file adds no dependency and
// no second encoder — the frames that go in are the PNGs `--film` already
// wrote, read back FROM DISK, so the GIF is provably a function of the
// deliverable and not of a second in-memory copy that could drift from it.
//
// THE PACE IS A DESIGN DECISION, stated here because there is no correct real
// time to fall back on. A beat is a STATE CHANGE of the surface, not a video
// frame: the capture cadence (a settled grab per beat) is the SPEED OF THIS
// MACHINE and of xterm's repaint, and it has nothing to do with the pace the
// recorded conversation actually had. Replaying the capture's real time would
// therefore make the film's speed an artifact of how fast the filmer ran, and
// two films of the SAME recording would play at different speeds on different
// hosts. So the film uses ONE uniform delay per frame, taken from gifgen's own
// default — the pace `gopi tool gif` already uses when its caller names none — and
// `chatFilmGIFDelayCS` REFERENCES that constant rather than re-typing its value
// (Rules 13/14: one definition, referenced everywhere the number is needed).
// The GIF format itself requires the uniformity any reader would want here: a
// GIF's delay is per frame, but a film whose frames each showed for a different
// length of time would be asserting a tempo this run cannot measure.
//
// THE CAPTION BAND IS GONE, AND THE REASON IS THE OPERATOR'S OWN VERDICT.
//
// The band was this file's first design: every frame carried its beat number and
// the ledger's words drawn UNDER the picture, by internal/text2img, on a white
// strip. It is a defensible idea for a film whose subject is text — and it is
// WRONG for this film, whose subject is a SURFACE. Measured 2026-10-04: the
// operator rejected the artifact in one line ("不像 gopi 聊天界面 … 多了描述文字"),
// and the pixels say why. `gifgen`'s band is (a) a WHITE strip appended below a
// DARK interface, so the GIF's geometry (706x392) is not the surface's (~706x342)
// and its overall impression is a white picture; and (b) BLACK TEXT in a
// different rasteriser, so the frames carry two typographic systems, one of
// which the product does not use. The acceptance is "the frame's pixels come from
// the real surface, and nothing else does" — a band drawn by a second renderer is
// pollution of exactly the evidence the film exists to carry.
//
// WHERE THE LEDGER ROW WENT INSTEAD: nowhere new. The rows are already beside the
// frames — `copyLedger` writes the producer's own `beats.tsv` into the film
// directory, and it is the SAME file the run reads to know which beat each frame
// belongs to. A reader who wants to know what frame 7 was does not need it drawn
// on frame 7; the film ships the ledger that says so, in the format the recording
// wrote it in. That is the "separate, non-polluting channel", and it is an
// EXISTING one (Rule 35: nothing new is created for it).
//
// WHY THE PARAMETER IS GONE RATHER THAN PASSED EMPTY: a `captions []string` a
// caller could fill again is an invitation to re-introduce the band, and the
// frame/caption mismatch check that guarded it disappears with it — there is now
// no list to shift onto the wrong frame. The signature states the invariant: what
// goes into the animation is the frames, and nothing else.

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/gifgen"
	"github.com/nick/gopi/internal/shot"
)

const (
	// chatFlagOut is the switch naming the animation to write. It TAKES A VALUE
	// (the .gif path) in both spellings, `--out FILE` and `--out=FILE`, exactly
	// like `--film` — one feature's two halves, parsed the same way.
	chatFlagOut = "--out"
	// chatFilmGIFDelayCS is how long each frame of the film shows, in
	// centiseconds. It is gifgen's OWN default, referenced rather than restated
	// (Rules 13/14): the film and `gopi tool gif` must not hold two ideas of what a
	// frame's duration is. 25 cs = 250 ms per beat — slow enough to read the
	// state a beat drew, fast enough that the film reads as motion.
	chatFilmGIFDelayCS = gifgen.DefaultDelayCentiseconds
	// chatFilmFrameLedgerHint is the read-alongside sentence: the film ships the
	// ledger beside the frames, and this is how a reader joins a frame index to
	// the row it came from now that the row is NOT drawn on the picture (see this
	// file's header — the caption band is gone).
	chatFilmFrameLedgerHint = chatFilmNotice + "frame N of %s corresponds to row N of %s (the ledger ships beside the frames; the film draws no caption into its pixels)"
	// chatFilmGifLabel is the file name a refusal uses, in the vocabulary the
	// CALLER typed to ask for the animation (never the driver's own word for it).
	chatFilmGifLabel = chatFlagOut
	// chatFilmOutNeedsFilm is the refusal for `--out` with nothing to animate: an
	// animation is the FILM's product, and a live conversation has no beats.
	chatFilmOutNeedsFilm = chatFlagOut + " writes a FILM's animation and no " + chatFlagFilm +
		" was given — write " + chatFlagReplay + " SESSION " + chatFlagFilm + " DIR " + chatFlagOut + " FILE.gif"
	// chatFilmGIFReport is the closing line for the animation: the GIF's own
	// measured arithmetic beside the ledger's, so the two accounts are readable
	// in one place.
	chatFilmGIFReport = chatFilmNotice + "animation %s: %d frame(s) at %dcs (%.1f frame(s)/s), %d byte(s) — %d ledger row(s), %d frame(s) photographed"
	// chatFilmGIFDisagreement is the refusal when the encoded stream and the
	// ledger disagree about how many beats the run drew: a GIF that holds more
	// frames than the recording has beats is a film of something nobody recorded.
	chatFilmGIFDisagreement = "%s holds %d frame(s) but the ledger holds %d row(s) — the animation and the recording disagree, so this film is refused rather than reported as complete"
	// chatFilmGIFWarning prefixes a warning the encoder raised about the film
	// (a caption its canvas cannot hold): reported, never swallowed.
	chatFilmGIFWarning = chatFilmNotice + "the encoder warned about %s: %s\n"
	// chatFilmEvenFrameNotice is the sentence a frame whose photographed size was
	// ODD gets: what was shot, what was written, and the fact that the difference
	// is one column/row removed rather than the picture resampled. Args: frame
	// name, photographed w, photographed h, written w, written h.
	chatFilmEvenFrameNotice = chatFilmNotice + "%s was photographed %dx%d px (odd) and is written %dx%d px — the last column/row is removed so both dimensions are even, which an encoder that subsamples chroma requires; the surviving pixels are the display's own, un-resampled"
	// chatFilmEvenCroppedReport is the closing count of that correction, so a run
	// that had to make its own frames even is readable as one (on the reference
	// face it is zero and nothing is printed). Args: cropped frames, frames total.
	chatFilmEvenCroppedReport = chatFilmNotice + "%d of %d frame(s) were written one pixel narrower/one row shorter than they were photographed, to make both frame dimensions even (see chat_film_frames.go for why an odd height is fatal)"
)

// chatParseOutArg consumes the `--out` switch and its value, in both spellings.
// It is chatParseFilmArg's twin and exists as a function for the same reason: the
// argv walk must consume the value in the next slot, which a ranged loop cannot
// express.
func chatParseOutArg(a, next string, hasNext bool) (path string, consumed bool, err error) {
	if a != chatFlagOut && !strings.HasPrefix(a, chatFlagOut+chatFlagValueSep) {
		return "", false, nil
	}
	if k := strings.Index(a, chatFlagValueSep); k > 0 {
		inline := a[k+1:]
		if inline == "" {
			return "", true, fmt.Errorf("%s needs a file — write %s FILE.gif", chatFlagOut, chatFlagOut)
		}
		return inline, true, nil
	}
	if !hasNext || strings.HasPrefix(next, chatFlagPrefix) {
		return "", true, fmt.Errorf("%s needs a file — write %s FILE.gif", chatFlagOut, chatFlagOut)
	}
	return next, true, nil
}

// chatFilmDumpRefusal is B3's fix, and it is the general rule rather than a
// special case for one flag.
//
// THE RULE IT STATES. A FILM RUN IS A DRIVER: it does not paint anything. It
// starts a private X display and a private terminal, and runs a SECOND copy of
// this binary inside that terminal as the producer — and the argv it hands that
// copy is built by this driver (runChatFilmOut), so a flag THIS process read is
// simply absent there. `--dump-screen` is therefore the one class of word a film
// cannot honour: accepting it would arm a sink on a process with no surface (an
// artifact that is silently never written, or an empty grid), which is exactly
// the silent acceptance the epic calls B3.
//
// WHY THE ENVIRONMENT IS DIFFERENT, AND WHY THAT IS NOT AN EXCEPTION. GOPI_CHAT_DUMP
// is inherited by the producer (shot.StartFilmTerminalChild passes the caller's
// environment with DISPLAY set), so an env-armed dump DOES work in a film run —
// and that is worth saying, because a caller who wants a dump from a filmed run
// has a working route. The default (no env, no flag) is likewise fine: the
// producer arms its own dump beside its own session artifacts, which is the
// ordinary behaviour of every chat run and needs no word from this driver.
//
// WHAT IS REFUSED: a dump word on THIS command line, in either direction —
// `--dump-screen[=PATH]` (inert here) and `--no-dump-screen` (a decline that
// would not be delivered, so the run would silently keep dumping). Both are
// named with the spelling that works instead, because a refusal a caller cannot
// act on is only half an answer.
//
// chatFilmNoDumpRefused is the OFF SWITCH's refusal: the filmed process is where
// a dump would be written, and this command line's decline cannot reach it — so
// the filmed run would silently keep writing one.
const chatFilmNoDumpRefused = "%s films a session and %s would decline an artifact the FILMED run writes — this process paints no screen of its own, so the flag cannot reach the run it would apply to (the filmed run would keep writing its dump). To decline it for the producer, set %s=0 in the environment (the producer inherits it), or drop %s"

// chatFilmDumpRefused is the ARM FLAG's refusal — B3 closed.
const chatFilmDumpRefused = "%s names a destination for a screen THIS process does not paint: a film is a driver, and the screen is painted by a second gopi inside the private terminal — a flag cannot reach that process, so accepting it would write nothing and say nothing (the silent acceptance this refusal closes). To dump the FILMED run's screen, set %s=PATH in the environment (the producer inherits it) and read the path the filmed run prints; or drop %s"

// chatFilmDumpRefusal returns the refusal for a dump word this run was GIVEN on a
// film command line, or nil when none was. See the comment above for why the
// ENVIRONMENT is deliberately not covered by it.
func chatFilmDumpRefusal(given bool, opts chatDumpOptions) error {
	if !given {
		return nil
	}
	if opts.disabled {
		return fmt.Errorf(chatFilmNoDumpRefused, chatFlagFilm, chatNoDumpFlag, chatDumpEnv, chatNoDumpFlag)
	}
	return fmt.Errorf(chatFilmDumpRefused, chatDumpFlag, chatDumpEnv, chatDumpFlag)
}

// THE ANIMATION IS A DELIVERABLE, SO IT CARRIES A DELIVERABLE'S CONTRACT.
//
// A film is made to be HANDED to someone, and the place it is handed over now is
// a phone: measured 2026-10-04, a 1186x521 animation (the surface's own ideal
// 117x26 at xterm's 6x13 face) is REFUSED by WeChat, whose conversation view
// accepts an animation only up to 500 px wide. The refusal is not an error the
// caller sees — the file simply arrives unplayable — so this run holds its own
// output to the limit instead of discovering it after the hand-over.
//
// The width is a property of the FILM (which cells it was drawn at), not a
// post-processing step: resampling a 117-column terminal picture down to 82
// columns SHRINKS THE GLYPHS the layout was measured for (the surface's
// legibility floor is px-per-character, and a downscaled glyph also costs MORE
// bytes — the palette grows intermediate colours on every antialiased edge).
// Narrowing the CELL COUNT re-lays the surface out at a width it is designed to
// draw, so the picture stays the product's own. See
// devdoc/film-gif-wechat-playable.md for the measurement.
const (
	// playableGIFMaxWidth is the widest animation this repository delivers: a
	// phone conversation view refuses one wider than this. It is a constraint of
	// the READER, so it is a ceiling on the FILM's canvas (filmPlayableMaxCols
	// converts it to the cell count the film is shot at).
	//
	// TWO MEASUREMENTS STAND BEHIND THIS NUMBER, AND THE SECOND ONE CLOSES THE
	// QUESTION THE FIRST LEFT OPEN — neither is replaced by the other (Rule 38:
	// a reading is evidence of ONE run and stays where it was taken).
	//
	//   - 2026-10-04, 1186x521 px / 151 frames / 3,713,383 B: REFUSED by WeChat
	//     (the surface's own ideal 117x26 at xterm's 6x13 face). That measurement
	//     put the ceiling BELOW 1186 px and derived the 82-column reference cap;
	//     it did not say where between "the film we ship" and 1186 px the line
	//     actually is.
	//   - 2026-10-11, OPERATOR'S PHONE, sent through WeChat's own album path:
	//     494x502 px / 20 frames / 495,163 B PLAYS; 634x520 px / 12 frames /
	//     322,242 B does NOT (nor do 714x520, 954x520, 1134x502). The control is
	//     clean in the direction that matters: the 634 px sample is SMALLER in
	//     bytes (322,242 < 495,163) and SHORTER in frames (12 < 20) than the one
	//     that plays, so the only dimension in which it exceeds the working
	//     sample is its WIDTH — 140 px more. Width is the variable, not the byte
	//     count and not the frame count: narrow it and the file plays.
	//
	// So the check this constant feeds is not a conservative guess: 494 px sits
	// INSIDE the gate and plays, 634 px sits outside it and does not. THIS VALUE
	// THEREFORE STAYS 500 — and it is deliberately not raised to 634 px, because
	// 500 is the number the measurement SUPPORTS: the reading fixes the line
	// somewhere in (494, 634], and nothing in this repository has measured where
	// in that interval (## Not Done, devdoc/film-gif-wechat-playable.md §8).
	//
	// HOW TO USE THE READING WITHOUT RE-RUNNING THE EXPERIMENT (it cost a real
	// phone, a night and several round trips): an animation that will not play is
	// diagnosed by its WIDTH FIRST — `od -j6 -N4 file.gif` reads the logical
	// screen width, and over 500 px is the answer. The loop block, the file
	// header and the sending route are NOT the cause (each was measured and
	// excluded; §8).
	playableGIFMaxWidth = 500
	// playableGIFParity is the alignment a frame must have in BOTH dimensions. A
	// wide range of decoders carries chroma at half resolution in EACH axis (the
	// 2×2 subsampling every common GIF/video path uses), so an odd width OR an
	// odd height is padded or dropped by the reader, depending on which one it
	// is — refused here rather than delivered silently misaligned.
	//
	// It is ONE constant for the two halves because it is ONE requirement met by
	// one mechanism (the frame writer's crop, chat_film_frames.go). The former
	// name said "Width", which was true while only the width half was checked and
	// became an actively wrong signpost the moment the height half landed: the
	// value still means "the alignment", so the name follows the meaning rather
	// than a second number being invented for the height (Rule 13/14).
	playableGIFParity = 2
)

// THE WECHAT PRODUCT — the operator's named request, made a first-class one.
//
// cc18cbe5 gave EVERY film with an `--out` the three properties a phone
// conversation view needs, because a film with an `--out` IS a hand-over
// artifact. That default stays exactly as it is (the acceptance holds the
// no-flag run to being byte-identical to it). What the operator asked for is the
// ability to SAY the product is for the phone — so `--wechat` names the
// DESTINATION, and a destination can carry constraints the default does not.
//
// WHAT THE FLAG ADDS ON TOP OF THE DEFAULT (and nothing else):
//   - a BYTE CEILING (wechatGIFMaxBytes) — a file over it is refused with the
//     numbers, never delivered and hoped for (Rule 15).
//
// The OTHER dimension parity (height) is deliberately NOT in that list any more:
// it is checked for EVERY product, not only the named one. An odd height is not
// a destination's preference, it is a property of the file — an encoder that
// subsamples chroma refuses it outright, so a film shot at an odd height could
// not be encoded by anyone, phone or not. It is also fixed at the frame source
// (chat_film_frames.go's crop), and this gate is the SECOND half of the same
// requirement: a pre-existing film directory, or a later change to the crop
// path, is still composed through this seam.
const (
	// chatFlagWechat is the switch asking for the phone-shaped product. It is a
	// BARE switch (no value), in the same shape as the other boolean words on
	// this verb, so it is spelled `--wechat` and nothing else.
	chatFlagWechat = "--wechat"
	// wechatGIFMaxBytes is the ceiling ONE animation may have when it is handed
	// over as a phone conversation animation: 5 MiB. Measured/decided 2026-10-04
	// from the widely documented limit a WeChat user's single added 表情 obeys
	// ("单个表情不超过 5M") — the operator-facing convention rather than a
	// primary-source measurement by this repository: no WeChat specification was
	// reachable from this host, and the report's ## Not Done says so. It is
	// deliberately the CEILING WE ENFORCE, not a claim about the exact byte at
	// which a given client refuses a file: a film that stays under it is
	// deliverable, and one that does not is refused with the numbers so the
	// caller can short the recording or the width.
	wechatGIFMaxBytes = 5 * 1024 * 1024
	// chatFilmFrameParity is the alignment a film frame must have, named for the
	// FRAME the film writes. It is the SAME requirement as playableGIFParity —
	// the picture the film writes and the picture the delivery gate accepts
	// cannot disagree, so the two names resolve to one definition rather than
	// being two numbers that can drift apart (Rules 13/14).
	chatFilmFrameParity = playableGIFParity
)

// filmAxisWidth and filmAxisHeight are the axis WORDS the parity refusal is
// rendered with (Rule 1b: a string that carries meaning comes from ONE place).
// They are deliberately NOT a second parity number: the alignment itself is
// playableGIFParity, referenced everywhere it is needed (Rules 13/14), and these
// two are only how the failing axis is named so a reader is not sent to look at
// the wrong dimension.
const (
	filmAxisWidth  = "width"
	filmAxisHeight = "height"
)

// chatFilmWechatNeedsOut is the refusal for the DESTINATION without a product: a
// phone-shaped request names an artifact the phone will be handed, and the film
// directory alone is not one. Naming both spellings is the half of the refusal a
// caller can act on.
const chatFilmWechatNeedsOut = "%s asks for the phone-playable GIF and no %s was given — the frames alone are not what a phone is sent: write %s SESSION %s DIR %s FILE.gif %s"

// chatFilmOddDimensionRefused is the parity half of the delivery contract,
// stated for BOTH axes in ONE sentence because it is one requirement
// (playableGIFParity) met by one mechanism (the frame writer's crop,
// chat_film_frames.go): chroma rides at half resolution in each axis, so a
// reader given an odd width OR an odd height pads or drops a line of pixels, and
// which one it does is the reader's choice rather than ours.
//
// The dimension that failed is NAMED, so a reader is not sent to look at the
// wrong axis, and the refusal is never a silent resample (Rule 15). Args: path,
// the offending value, the axis word (filmAxisWidth / filmAxisHeight), the
// alignment the value must have.
//
// The predecessor stated only the width half (chatFilmOddWidthRefused) because
// only the width half was checked; the word "wide" and the axis it names are
// wrong the moment the same sentence has to carry a height, so the sentence is
// parameterised rather than duplicated — a copy for the height would be a second
// place for the same refusal to drift.
const chatFilmOddDimensionRefused = "%s is %d px %s (ODD) — a reader that carries chroma at half resolution in each axis pads or drops a line of pixels from a frame whose dimension is not %d-aligned, so this animation is refused rather than delivered silently misaligned"

// chatFilmWechatTooBigRefused is the ceiling half: a file over the limit is
// REFUSED with its own size and the limit, because a silent over-limit hand-over
// is the exact failure this flag exists to prevent — the file is produced, the
// operator believes it is deliverable, and the phone is the thing that says
// otherwise.
const chatFilmWechatTooBigRefused = "%s is %d byte(s), over the %d byte(s) (%s) limit one animation may have in a phone conversation — refused rather than handed over: shorten the recording, narrow it (%s/%s), or raise the frame spacing so fewer frames carry the same beats"

// chatFilmWechatReport is the closing line for the phone-shaped product: the
// four readings the acceptance names, so the report and the run agree on what
// was actually delivered.
const chatFilmWechatReport = chatFilmNotice +
	"wechat: %s is %dx%d px, even in both dimensions, %d byte(s) of %d allowed, Netscape loop block read back %s — %d frame(s)\n"

// chatParseWechatArg consumes the bare `--wechat` switch. It is a bool switch,
// so it has no value to consume (unlike its neighbours on this command line) and
// it never advances the argument walk. A spelling carrying a value
// (`--wechat=1`) is refused rather than silently ignored: a switch whose value
// is dropped is a flag the caller believes did something.
func chatParseWechatArg(a string) (matched bool, err error) {
	if a == chatFlagWechat {
		return true, nil
	}
	if strings.HasPrefix(a, chatFlagWechat+chatFlagValueSep) {
		return true, fmt.Errorf("%s is a switch and takes no value — write %s (it requests the phone-playable GIF; %s FILE.gif is what it wraps)", chatFlagWechat, chatFlagWechat, chatFlagOut)
	}
	return false, nil
}

// filmFrameWidthPx is the pixel width a film shot at cols comes out at ON THE
// REFERENCE face (the SAME derivation the help page's pixel table uses —
// chatRefCellW / chatRefBorderPx), referenced rather than re-typed (Rules 13/14).
//
// IT IS THE REFERENCE, NOT THE RUN'S OWN FIGURE. The reference face (xterm's
// built-in fixed face) is what the help page publishes and what the frame
// arithmetic was historically taken with; the film's REAL frame width on the
// terminal it is shot on is filmFrameWidthPxOn(filmDeliveryFace, cols), and on
// this host's default terminal the two differ by 340 px at 82 columns
// (measured 2026-10-04: 836 vs 496). A caller reporting a run's own expectation
// must use the face-aware function, which says which face it used.
func filmFrameWidthPx(cols int) int {
	return filmFrameWidthPxOn(filmReferenceFace(), cols)
}

// filmReferenceFace is the driver's REFERENCE face — xterm's built-in `fixed`
// metric, 6x13 px per cell plus its 4 px border — expressed as a FACE VALUE so
// that the arithmetic which has no run behind it (the help page's pixel table,
// the fallback when no face was measured) goes through the SAME derivation the
// measured faces go through (Rules 13/14: one formula, two inputs).
//
// It is a real model, not a stand-in: xterm publishes exactly this in
// WM_NORMAL_HINTS (measured 2026-10-04 by reading that property), and the
// constants it is built from are the same chatRefCellW/chatRefCellH/
// chatRefBorderPx the help page has always printed.
func filmReferenceFace() shot.TerminalFace {
	return shot.TerminalFace{
		CellW: chatRefCellW, CellH: chatRefCellH,
		BorderW: chatRefBorderPx, BorderH: chatRefBorderPx,
		Measured: true,
	}
}

// filmFrameWidthPxOn is the pixel width a canvas of cols comes out at ON face:
// the model the terminal itself published (base + increment × cells), with the
// reference face as the fallback when the caller passes an unknown one.
//
// The fallback is a COUNT, not a silent substitution: filmFrameWidthPxOn cannot
// know whether a face is unknown because no run read one (a pure-arithmetic
// caller, the help page) or because the run tried and failed (reported by
// filmProbeTerminalFace). The driver therefore reports WHICH face its figures
// used (filmFrameGeometry takes the face), so the fallback is always visible in
// the run's own words.
func filmFrameWidthPxOn(face shot.TerminalFace, cols int) int {
	if face.Known() {
		return face.WidthForCols(cols)
	}
	return cols*chatRefCellW + chatRefBorderPx
}

// filmFrameHeightPxOn is filmFrameWidthPxOn's row half.
func filmFrameHeightPxOn(face shot.TerminalFace, rows int) int {
	if face.Known() {
		return face.HeightForRows(rows)
	}
	return rows*chatRefCellH + chatRefBorderPx
}

// filmPlayableMaxCols is the largest cell count whose frame is still playable
// ON THE FACE THIS FILM IS SHOT ON — derived from the limit and that face,
// never a second number typed beside them.
//
// WHY IT IS NOT A CONSTANT ANY MORE (measured 2026-10-04, this host): a single
// "82 columns" is a fact about XTERM's built-in face (6 px per cell + 4 px
// border = 496 px), while the film's own default terminal is xfce4-terminal,
// whose face is 10x19 px per cell with a 16x27 px border — the SAME 82 columns
// arrive 836 px wide and are refused. The limit is about the READER (a phone
// conversation view); the column count is a fact about the CAMERA, and deriving
// one from the other through a face the film is not using is how 496 became 836.
//
// The face enters through ONE door (filmPlayableFaceGeometry below), so this
// function, the refusal texts and the driver's notice cannot disagree about
// which face the count belongs to (Rules 13/14).
func filmPlayableMaxCols() int {
	return filmPlayableMaxColsForFace(filmDeliveryFace)
}

// filmReferenceMaxCols is the same derivation on the REFERENCE face: the count
// the pre-face contract implied (82 columns at 6 px per cell and a 4 px border),
// kept as its own NAME because it is a fact about xterm that a reader may still
// need to state — the cap a run uses is filmPlayableMaxCols, which is about the
// terminal this film is really shot on.
func filmReferenceMaxCols() int {
	return filmPlayableMaxColsForFace(filmReferenceFace())
}

// filmDeliveryFace is the face of the terminal THE FILM THIS PROCESS IS
// DELIVERING was shot on, published by the driver at the moment it read it
// (filmProbeTerminalFace, before the producer started).
//
// WHY A PACKAGE VALUE AND NOT A PARAMETER THREADED EVERYWHERE: the composer
// (chatFilmRequirePlayable) is reached from this arm's product path and from
// pre-existing-film paths that have no terminal at all, and a refusal that says
// "shoot it narrower" must name the SAME face the cap was derived from even
// there. Its zero value is declared UNKNOWN (shot.TerminalFace{Measured:false}),
// so a run that read no face says so out loud rather than deriving a 500 px cap
// from 0 px cells.
var filmDeliveryFace = shot.TerminalFace{Why: "no terminal face was read in this process"}

// chatFilmFaceNoticeFmt is how the driver announces the face it just read and
// the width limit THAT face implies. It is printed BEFORE the producer starts,
// because the cell count the run will shoot at is decided from it — a notice
// after the fact would describe a decision already made. Args: the face (its
// own String), the limit in px, and the cell count derived from it.
const chatFilmFaceNoticeFmt = chatFilmNotice + "the filmed terminal's face is %s — at the %d px playable width that is at most %d column(s)\n"

// filmProbeTerminalFace reads the face off the window the driver is about to
// photograph and publishes it as filmDeliveryFace, reporting what it read on the
// caller's stream.
//
// WHY IT IS DONE BETWEEN WINDOW RESOLUTION AND FIRST FRAME, AND NOT LATER. The
// window exists (the terminal has mapped it) but no beat has been drawn yet, so
// reading WM_NORMAL_HINTS costs one GetProperty and changes nothing about the
// picture; and the answer still arrives in time to decide the canvas, because
// the DELIVERY cap is applied to the cell count the producer is given. A run
// that read the face after the first frame would be reporting a limit that was
// applied with a face it had not read.
//
// A face that cannot be read does NOT end the run: it is reported, and the
// pixel arithmetic falls back to the reference face with the fallback stated
// (Rule 15 — a degradation that is named is not a silent one). The reading is
// also reported with its provenance, so a later reader can ask the same window
// the same question.
func filmProbeTerminalFace(term *shot.FilmTerminal, errOut io.Writer) {
	if term == nil {
		return
	}
	face, err := term.Face()
	if err != nil {
		fmt.Fprintf(errOut, chatFilmNotice+"the filmed terminal's face could not be read from window 0x%x (%v) — pixel figures for this run fall back to the reference face (%dx%d px per cell + %d px border), and the playable cap is derived from THAT\n",
			term.Window, err, chatRefCellW, chatRefCellH, chatRefBorderPx)
		return
	}
	filmDeliveryFace = face
	fmt.Fprintf(errOut, chatFilmFaceNoticeFmt, face, playableGIFMaxWidth, filmPlayableMaxColsForFace(face))
}

// filmPlayableMaxColsForFace is the ONE derivation from a pixel limit to a cell
// count, and its whole content is: on THIS face, how many cells still fit?
//
// cols is the largest count whose frame width (border + cols × cell) is at or
// below the limit. The division is integer, so the answer is the largest count
// that FITS rather than the nearest one — a cap that rounded up would hand the
// producer a canvas whose frame is over the limit by construction.
//
// The face must be KNOWN: an unknown face states no cell size, and a cell count
// derived from "0 px per cell" would be infinite. It returns 0 for an unknown
// face, and the caller states the fallback in words (filmPlayableFaceFallback)
// rather than treating 0 as a capping decision (Rule 15).
func filmPlayableMaxColsForFace(face shot.TerminalFace) int {
	if !face.Known() || playableGIFMaxWidth <= face.BorderW {
		return 0
	}
	return (playableGIFMaxWidth - face.BorderW) / face.CellW
}

// filmFaceMaxColsRefused is the refusal for a cap that could not be derived: no
// face means no cell count, and a run that proceeded would shoot at a column
// count nobody could justify.
const filmFaceMaxColsRefused = "the playable cell count cannot be derived: the filmed terminal's face is %s, which states no cell size — the driver reads the terminal's own WM_NORMAL_HINTS before the producer starts, so this means the window published none (the run reports which of the two it was)"

// filmPlayableFaceGeometry is what the caller prints when the cap had to be
// applied without a face: the reference face is used, AND SAID to be a
// fallback. It returns the sentence, so the caller's notice and its test read
// the same words (one definition, Rules 13/14).
func filmPlayableFaceFallback(face shot.TerminalFace) string {
	return fmt.Sprintf("no face was measured for this run (%s), so the playable cell count is derived from the driver's REFERENCE face (%dx%d px per cell + %d px border, xterm's built-in fixed face) — on a terminal with a wider face the same cell count is a wider frame, which is what the composer's own refusal then reports",
		face, chatRefCellW, chatRefCellH, chatRefBorderPx)
}

// chatFilmWechatMaxWidthFmt is the report line the playable check prints: the
// numbers a reader can re-derive. Args: path, width, height, max width, the
// alignment, loop.
const chatFilmWechatMaxWidthFmt = chatFilmNotice +
	"playable: %s is %dx%d px (max %d px wide, both dimensions %d-aligned), Netscape loop block read back %s\n"

// chatFilmTooWideRefused is the refusal for an animation this run's OWN encoder
// produced above the playable width. It is a refusal rather than a silent
// shrink: a film narrower than the surface it photographs is a DIFFERENT picture,
// and nothing in this path is allowed to decide that behind the caller's back.
//
// IT NAMES THE FACE AND THE CELL COUNT, because "shoot it narrower" is not an
// instruction a caller can carry out without them: the number of columns that
// fits depends on the terminal's own pixel-per-cell step, and the run that
// produced the refusal is the one run that measured it. Args: path, width,
// height, max width, the face the count was derived on, the fix (composed by
// chatFilmRequirePlayable from the SAME face, so the refusal and the cap cannot
// name different terminals).
const chatFilmTooWideRefused = "%s is %dx%d px, over the %d px limit a phone conversation view accepts — this film was shot at a cell count whose frame is too wide on %s, so no playable animation can be written from it (%s)"

// chatFilmNoLoopRefused is the third half: an animation that does not loop plays
// once and stops, which is exactly the "it does not play" report this arm was
// built to answer. The check is on the READ-BACK value, not on what was asked:
// the bytes are the claim.
const chatFilmNoLoopRefused = "%s carries no Netscape loop block (the file reads back %s, want %s = forever) — a conversation view shows such a file once and stops"

// chatFilmRequirePlayable holds a written animation to the delivery contract
// above: at most playableGIFMaxWidth px wide, BOTH dimensions aligned to
// playableGIFParity, and a Netscape loop block in the FILE (read back, never
// assumed).
//
// The parity leg is checked on BOTH axes and the failing one is NAMED, so a
// reader is sent to the dimension that is actually wrong. It is met by the frame
// writer's crop (chat_film_frames.go) on this arm's own product, so this gate
// should never fire on a film this run shot — it is kept because the requirement
// belongs to the DELIVERED FILE and the file is what this function reads: a
// pre-existing film directory, a later change to the crop path, or an encoder
// that changes geometry has to be caught here rather than discovered on a phone.
//
// It runs INSIDE the composer, so every caller of this arm — the film driver,
// the tests — is checked, and a film that cannot be handed over is refused at
// the moment it is made rather than reported as complete (Rule 15: the failure
// is stated, never smoothed into the summary).
//
// `wechat` adds the property a PHONE CONVERSATION adds on top of that default:
// the BYTE CEILING. It is a refusal with numbers, never a silent hand-over. The
// HEIGHT leg deliberately sits OUTSIDE that switch: an odd height is a property
// of the FILE (an encoder that subsamples chroma refuses it outright) rather
// than a destination's preference, so it is checked for every product. The
// predecessor of this change checked it only under the flag, which meant the
// DEFAULT product silently handed over a file many encoders cannot open at all.
func chatFilmRequirePlayable(res *gifgen.Result, wechat bool) error {
	if res.Width > playableGIFMaxWidth {
		face := filmDeliveryFace
		max := filmPlayableMaxColsForFace(face)
		faceText := "the reference face (" + strconv.Itoa(chatRefCellW) + "x" + strconv.Itoa(chatRefCellH) + " px per cell + " + strconv.Itoa(chatRefBorderPx) + " px border)"
		fitText := "no terminal face was measured in this run, so no column count can be derived for YOUR terminal — re-run with the film's terminal available and the refusal will name the " + chatFlagCols + " that fits on its face"
		if face.Known() {
			faceText = face.String()
			// The ACTIONABLE half: the flag to type, the count that fits ON THIS
			// face, and the frame that count comes out at — the three numbers a
			// caller needs to shoot the film again without guessing, taken from
			// the SAME face the refusal was derived on (Rules 13/14).
			fitText = fmt.Sprintf("shoot it narrower: %s %d, which is %d px on that face", chatFlagCols, max, face.WidthForCols(max))
		}
		return fmt.Errorf(chatFilmTooWideRefused, res.Out, res.Width, res.Height, playableGIFMaxWidth,
			faceText, fitText)
	}
	if res.Width%playableGIFParity != 0 {
		return fmt.Errorf(chatFilmOddDimensionRefused, res.Out, res.Width, filmAxisWidth, playableGIFParity)
	}
	if res.Height%playableGIFParity != 0 {
		return fmt.Errorf(chatFilmOddDimensionRefused, res.Out, res.Height, filmAxisHeight, playableGIFParity)
	}
	if res.LoopCount != gifgen.LoopForever || res.LoopReadBack != gifgen.LoopForever {
		return fmt.Errorf(chatFilmNoLoopRefused, res.Out,
			loopReadBackSpelling(res.LoopReadBack), loopReadBackSpelling(gifgen.LoopForever))
	}
	if !wechat {
		return nil
	}
	if res.Bytes > wechatGIFMaxBytes {
		return fmt.Errorf(chatFilmWechatTooBigRefused, res.Out, res.Bytes, wechatGIFMaxBytes,
			wechatByteCeilingSpelling(), chatFlagCols, chatFlagRows)
	}
	return nil
}

// wechatByteCeilingSpelling renders the byte ceiling as the unit a caller reads
// it in, from the constant rather than a second number typed beside it
// (Rule 1b). It is a presentation of wechatGIFMaxBytes, never its definition.
func wechatByteCeilingSpelling() string {
	return fmt.Sprintf("%d MiB", wechatGIFMaxBytes/(1024*1024))
}

// loopReadBackSpelling renders a loop number the way gifgen's own summary does,
// for the refusal above: 0 is the format's "forever" and -1 is the decoder's
// "this stream carries no looping extension" — the two readings a reader has to
// be able to tell apart (Rule 1b: one spelling, referenced).
func loopReadBackSpelling(n int) string {
	if n < 0 {
		return "none(-1)"
	}
	return fmt.Sprintf("forever(%d)", n)
}

// chatFilmPlayableNarrowedFmt is the notice the cap prints. A size change is a
// FACT ABOUT THE PICTURE, so it is never silent (Rule 15): the sentence names the
// cells the film was resolved to, the cells it will actually be shot at, and the
// pixel width of both — on the face this run MEASURED, with the face named, so a
// reader comparing runs on two terminals can see which of the two numbers moved.
// Args: resolved cols, resolved rows, the resolved frame's px width, the shot
// cols, the shot rows, the shot frame's px width, the face the figures are on,
// the limit, the flag that changes it.
const chatFilmPlayableNarrowedFmt = chatFilmNotice +
	"the resolved canvas is %d" + config.ChatIdealGeometryDimSep + "%d (%d px wide on %s, over the %d px a phone conversation view accepts), " +
	"so this film is shot at %d" + config.ChatIdealGeometryDimSep + "%d (%d px wide) instead — the surface is RE-LAID OUT at the narrower width rather than resampled down, " +
	"which keeps the glyphs at the size the layout measured them for (a %s/%s smaller than that is shot as stated and refused if its frame is still too wide)\n"

// chatFilmPlayableGeometry caps the cell count a film is shot at so its frames
// are playable on a phone — the DELIVERY half of the film's geometry.
//
// WHY THE CELL COUNT AND NOT THE PIXELS (the (b)-over-(a) decision, measured in
// devdoc/film-gif-wechat-playable.md): resampling a finished film down to the
// limit shrinks every glyph below the size the surface's legibility floor was
// measured for, and on text it can INFLATE the file (each antialiased edge gains
// intermediate palette entries). Narrowing the cell count makes the surface draw
// a narrower layout at its designed font size — a picture the product can
// actually produce — and it stays inside the limit by construction: the cap is
// derived from the limit and the face this run is shooting on
// (filmPlayableMaxColsForFace), never guessed.
//
// THE FACE IS THE ARGUMENT THAT FIXES THE DEFECT (01a10603). The predecessor
// derived this cap from xterm's REFERENCE face while the film's default terminal
// is xfce4-terminal, so 82 columns was a cap of 496 px on a terminal that draws
// the same 82 columns 836 px wide: the cap let a canvas through that the
// composer then refused, and the parity half of the contract (the frame crop)
// was never reached on a real session. Passing the run's own face closes that
// gap by construction — the cap is now a count whose frame is inside the limit
// ON THE TERMINAL THAT WILL BE PHOTOGRAPHED.
//
// The cap applies to the RESOLUTION, not to an explicit statement: a caller that
// names its own size keeps it (this function only ever narrows a canvas the
// driver resolved), and a frame that is still too wide is refused by
// chatFilmRequirePlayable with the numbers, never silently resampled.
func chatFilmPlayableGeometry(face shot.TerminalFace, cols, rows int, errOut io.Writer) (int, int) {
	if !face.Known() {
		// NO FACE, NO DERIVATION. The two candidate answers are both worse than
		// saying so: capping on the reference face is the defect this change
		// removes (it lets an over-wide canvas through), and capping on the widest
		// conceivable face would shrink films on terminals nobody measured. So the
		// resolved canvas is shot AS STATED, the fallback is PRINTED with the
		// reference face named, and a frame that is still too wide is refused by
		// the composer with the face it used (Rule 15).
		fmt.Fprintf(errOut, chatFilmNotice+"%s\n", filmPlayableFaceFallback(face))
		return cols, rows
	}
	max := filmPlayableMaxColsForFace(face)
	if max <= 0 {
		// The limit does not fit even ONE cell on this face. Capping to zero
		// columns would hand the producer a canvas of no width; the caller refuses
		// with both numbers instead (filmDeliveryCanvas).
		return 0, rows
	}
	if cols <= max {
		return cols, rows
	}
	fmt.Fprintf(errOut, chatFilmPlayableNarrowedFmt,
		cols, rows, filmFrameWidthPxOn(face, cols), face,
		playableGIFMaxWidth,
		max, rows, filmFrameWidthPxOn(face, max), chatFlagCols, chatFlagRows)
	return max, rows
}

// chatFilmComposeGIF turns the frames THIS RUN photographed into ONE animation,
// through internal/gifgen — the encoder `gopi tool gif` is built on.
//
// frames are in BEAT ORDER (the order the ledger recorded them, which is the
// order they played). NOTHING ELSE GOES IN: no caption, no band, no overlay — the
// animation's pixels ARE the photographed surface's pixels, which is the whole of
// what this arm is for (see this file's header for the measurement that removed
// the band, and for where the ledger's words live instead).
//
// The encoder's own read-back result is then held to the DELIVERY contract
// (chatFilmRequirePlayable): the film arm's product must be one a phone will
// play, and that is a property of the file, not of the intent. `wechat` names
// the stricter product (`--wechat`), whose two extra properties — an even height
// and the byte ceiling — are checked here as well, because a film directory shot
// before the even-height fix is still composed through this seam.
func chatFilmComposeGIF(out string, frames []string, wechat bool) (*gifgen.Result, error) {
	if len(frames) == 0 {
		return nil, fmt.Errorf("no frames to animate: a GIF with no frames is not a film")
	}
	seq := make([]gifgen.Frame, 0, len(frames))
	for _, p := range frames {
		seq = append(seq, gifgen.Frame{Path: p})
	}
	res, err := gifgen.Encode(out, gifgen.Options{DelayCS: chatFilmGIFDelayCS, Frames: seq})
	if err != nil {
		return nil, err
	}
	if err := chatFilmRequirePlayable(res, wechat); err != nil {
		return nil, err
	}
	return res, nil
}

// filmFrameGeometry renders the COLSxROWS a film was shot at as the pixel frame
// those cells come out at, using the SAME reference face the playable ceiling is
// derived from (filmFrameWidthPx — one derivation, Rules 13/14).
//
// WHY THE DRIVER REPORTS IT. When the film arm narrows a canvas (the delivery
// limit), the window it photographs is NOT the pixel size the terminal's cell
// count would give on this host's default face: a desktop terminal draws a WIDER
// face, so 82 cells can arrive as 836 px — over the limit again. That is what the
// composer's refusal reports, and a reader who cannot see the two numbers side by
// side cannot tell "the film is too wide" from "the terminal drew a bigger face
// than this driver's reference". The cell count and the expected frame are
// therefore stated together, at the moment the terminal is started (Rule 31's
// spirit: the surface that decides the pixels says what it expects).
func filmFrameGeometry(cols, rows int) string {
	return fmt.Sprintf("%dx%dpx", filmFrameWidthPx(cols), filmFrameHeightPx(rows))
}

// filmFrameGeometryOn is filmFrameGeometry on a MEASURED face, with the face's
// own name in the answer: the run's closing arithmetic reports what this
// terminal will actually map, and a reader comparing two runs' figures can see
// which face each was computed on (Rules 13/14 — one derivation, the face named
// at the site that used it).
func filmFrameGeometryOn(face shot.TerminalFace, cols, rows int) string {
	return fmt.Sprintf("%dx%dpx on %s", filmFrameWidthPxOn(face, cols), filmFrameHeightPxOn(face, rows), face)
}

// filmFrameHeightPx is filmFrameWidthPx's row half: rows × the reference cell
// height plus the same border.
func filmFrameHeightPx(rows int) int {
	return rows*chatRefCellH + chatRefBorderPx
}
