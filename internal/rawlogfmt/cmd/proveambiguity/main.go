package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// The nastiest payload a request body can carry: every legacy marker line AND a
// header-shaped line of its own.
var nasty = []byte("prefix\n→ REQUEST\n← CHUNK\n← RESPONSE\nHTTP-ERROR\nR 5\nC 0\nS 12\n" +
	"# gopi session gc — TRIMMED\nR 999999\nE 1\n← CHUNK (1214-guard, stream)\nsuffix")

func main() {
	dir := os.Args[1]
	// record 1: request whose payload quotes the whole marker vocabulary
	// record 2: a normal response, so a mis-split shows up as extra rounds
	recs := []struct {
		tag, ann string
		body     []byte
	}{
		{rawlogfmt.TagRequest, "", nasty},
		{rawlogfmt.TagChunk, "", []byte("event: content_block_delta\ndata: {\"i\":0}\n")},
		{rawlogfmt.TagResponse, "", []byte(`{"id":"msg_2"}`)},
	}

	var framed []byte
	for _, r := range recs {
		framed = append(framed, rawlogfmt.EncodeAnnotated(r.tag, r.ann, r.body)...)
	}
	if err := os.WriteFile(filepath.Join(dir, "framed.log"), framed, 0o600); err != nil {
		panic(err)
	}

	// The SAME session in the legacy framing, for the equivalence check. It
	// cannot be byte-identical by construction (that is the whole defect), so
	// only the first record is rendered — enough to show the legacy reader
	// mis-splits the same payload into extra rounds.
	var legacy []byte
	for _, r := range recs {
		b, err := rawlogfmt.EncodeLegacy(r.tag, r.ann, r.body)
		if err != nil {
			panic(err)
		}
		legacy = append(legacy, b...)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy.log"), legacy, 0o600); err != nil {
		panic(err)
	}
	fmt.Println("wrote framed.log", len(framed), "bytes; legacy.log", len(legacy), "bytes")
}
