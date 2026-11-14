//go:build embedgz

package main

/*
#cgo CFLAGS: -I/home/user/work/whisper.cpp/include -I/home/user/work/whisper.cpp/ggml/include
#cgo LDFLAGS: -L/home/user/work/whisper.cpp/build-static/src -L/home/user/work/whisper.cpp/build-static/ggml/src -lwhisper -lggml -lggml-base -lggml-cpu -lm -lstdc++
#include <stdlib.h>
#include <string.h>
#include "whisper.h"

static struct whisper_context* poc_init_buf(void* buf, size_t n) {
    struct whisper_context_params cp = whisper_context_default_params();
    return whisper_init_from_buffer_with_params(buf, n, cp);
}
static void* poc_copy_to_c(const void* src, size_t n) {
    void* d = malloc(n);
    if (d) memcpy(d, src, n);
    return d;
}
static int poc_run(struct whisper_context* ctx, const float* s, int n, const char* lang,
                   char* out, int outcap) {
    struct whisper_full_params p = whisper_full_default_params(WHISPER_SAMPLING_GREEDY);
    p.print_progress = 0; p.print_realtime = 0; p.no_timestamps = 1;
    p.language = lang; p.n_threads = 2;
    int rc = whisper_full(ctx, p, s, n);
    if (rc != 0) return rc;
    int ns = whisper_full_n_segments(ctx);
    int off = 0;
    for (int i = 0; i < ns; i++) {
        const char* t = whisper_full_get_segment_text(ctx, i);
        int len = (int)strlen(t);
        if (off + len + 1 >= outcap) break;
        memcpy(out + off, t, len); off += len;
    }
    out[off] = 0;
    return 0;
}
*/
import "C"

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"os"
	"time"
	"unsafe"
)

// modelGz is the gzip-deflated ASR model image. Decompressing at startup is
// the price of the size win; decompress_ms is printed so the price is
// measured, never assumed.
//
//go:embed model.bin.gz
var modelGz []byte

func initEngine() (*C.struct_whisper_context, string, error) {
	t0 := time.Now()
	zr, err := gzip.NewReader(bytes.NewReader(modelGz))
	if err != nil {
		return nil, "", fmt.Errorf("gzip: %w", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, "", fmt.Errorf("gunzip: %w", err)
	}
	if err := zr.Close(); err != nil {
		return nil, "", fmt.Errorf("gunzip close: %w", err)
	}
	decompressMS := time.Since(t0).Milliseconds()

	buf := C.poc_copy_to_c(unsafe.Pointer(&raw[0]), C.size_t(len(raw)))
	if buf == nil {
		return nil, "", fmt.Errorf("model buffer alloc failed")
	}
	ctx := C.poc_init_buf(buf, C.size_t(len(raw)))
	if ctx == nil {
		return nil, "", fmt.Errorf("whisper_init_from_buffer (%d bytes)", len(raw))
	}
	fmt.Fprintf(os.Stderr, "decompress_ms=%d gz_bytes=%d raw_bytes=%d\n", decompressMS, len(modelGz), len(raw))
	return ctx, "embed-gzip", nil
}

func transcribe(ctx *C.struct_whisper_context, pcm []float32, lang string) (string, error) {
	cl := C.CString(lang)
	defer C.free(unsafe.Pointer(cl))
	buf := make([]byte, textBufBytes)
	rc := C.poc_run(ctx, (*C.float)(unsafe.Pointer(&pcm[0])), C.int(len(pcm)), cl,
		(*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf)))
	if rc != 0 {
		return "", fmt.Errorf("whisper_full rc=%d", int(rc))
	}
	return string(buf[:bytesUntilNUL(buf)]), nil
}
