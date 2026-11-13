package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"testing"
)

// salvage-web-m2-unit-B6: the server's request-body cap must cover every
// request the server's OWN per-image validator accepts. The page caps the
// base64 it ships by the DECODED ceiling (32 MiB → ~42.7 MiB of base64, plus
// its 4-byte padding allowance), so a maximal legal image body is ~45.8 MiB —
// while webMaxChatBodyBytes cut the reader at a flat 4 MiB, i.e. the decoder
// refused a body validateWebImages accepts.
//
// The assertion runs the handler's OWN read path (the LimitReader + Decode
// pair handleChat uses) over the body the page would ship, so it needs no
// turn, no model and no network: a body the validator accepts must decode.
func TestWebM2B6_BodyCapCoversTheLargestLegalImageRequest(t *testing.T) {
	// base64's expansion as a fraction, spelled here so this test reads the
	// SAME relationship the page's MAX_B64 arithmetic uses.
	const (
		b64Num = 4
		b64Den = 3
	)
	// textSlack is the room a body must leave for the message text and the
	// JSON framing on top of the image payload.
	const textSlack = 1 << 20

	// 1. The maximal image the server's validator accepts.
	raw := make([]byte, webMaxChatImageBytes)
	for i := range raw {
		raw[i] = byte(i)
	}
	b64 := base64.StdEncoding.EncodeToString(raw)

	// 2. The page's own client-side budget must admit it, otherwise the page
	//    would refuse an image the server is willing to take.
	maxB64 := len(raw)*b64Num/b64Den + 4
	if len(b64) > maxB64 {
		t.Fatalf("base64 of a maximal image is %d bytes, over the page's budget %d", len(b64), maxB64)
	}

	// 3. The server's validator accepts it (the same call handleChat makes).
	if _, err := validateWebImages([]webChatImage{{MediaType: "image/png", DataB64: b64}}); err != nil {
		t.Fatalf("validateWebImages refused a maximal legal image: %v", err)
	}

	// 4. THE INVARIANT: a request the validator accepts must survive the
	//    handler's body reader. Run the handler's own LimitReader + Decode.
	body, err := json.Marshal(webChatRequest{
		Message: "what is in this image?",
		Images:  []webChatImage{{MediaType: "image/png", DataB64: b64}},
	})
	if err != nil {
		t.Fatalf("marshal the maximal legal request: %v", err)
	}
	var got webChatRequest
	if err := json.NewDecoder(io.LimitReader(bytes.NewReader(body), webMaxChatBodyBytes)).Decode(&got); err != nil {
		t.Fatalf("the handler's own reader could not decode a request its own validator accepts: "+
			"body is %d bytes, webMaxChatBodyBytes is %d, decode error: %v",
			len(body), webMaxChatBodyBytes, err)
	}
	if len(got.Images) != 1 || got.Images[0].DataB64 != b64 {
		t.Fatalf("the decoded request lost the image payload: %d image(s)", len(got.Images))
	}

	// 5. One budget, derived (Rule 1): the cap must cover the page's own
	//    maximal message, not a flat hand-typed number.
	required := maxB64*webMaxImagesPerRequest + textSlack
	if webMaxChatBodyBytes < required {
		t.Errorf("webMaxChatBodyBytes = %d does not cover the page's own maximal message "+
			"(%d images → %d bytes): the client ships a body the server's decoder cuts",
			webMaxChatBodyBytes, webMaxImagesPerRequest, required)
	}
}
