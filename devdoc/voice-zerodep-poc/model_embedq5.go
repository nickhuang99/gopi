//go:build embedq5

package main

/*
#cgo CFLAGS: -I/home/user/work/whisper.cpp/include -I/home/user/work/whisper.cpp/ggml/include
#cgo LDFLAGS: -L/home/user/work/whisper.cpp/build-static/src -L/home/user/work/whisper.cpp/build-static/ggml/src -lwhisper -lggml -lggml-base -lggml-cpu -lm -lstdc++
#include <stdlib.h>
#include <string.h>
#include <string.h>
#include "whisper.h"

// poc_init_buf returns the context build from an in-memory model image.
// The buffer must stay alive for the context's whole life (whisper keeps a
// pointer to it), which is why poc_copy_to_c makes an owned copy.
static struct whisper_context* poc_init_buf(void* buf, size_t n) {
    struct whisper_context_params cp = whisper_context_default_params();
    return whisper_init_from_buffer_with_params(buf, n, cp);
}
static void* poc_malloc(size_t n) { return malloc(n); }
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
	_ "embed"
	"fmt"
	"os"
	"unsafe"
)

// modelImage is the whole ASR model, shipped inside the executable: no file
// on disk, no runtime dependency, nothing to scp separately.
//
//go:embed model-q5_0.bin
var modelImage []byte

// copyToCEnv selects the ownership strategy for the model image:
//   unset/0 — hand whisper the embedded bytes directly (zero copy; the image
//             lives in the binary's .rodata, which never moves)
//   1       — memcpy into C memory first (strict cgo-pointer-rule shape)
const copyToCEnv = "VOICE_POC_COPY"

func initEngine() (*C.struct_whisper_context, string, error) {
	shape := "embed-direct"
	var buf unsafe.Pointer
	if os.Getenv(copyToCEnv) == "1" {
		shape = "embed-copy"
		buf = C.poc_copy_to_c(unsafe.Pointer(&modelImage[0]), C.size_t(len(modelImage)))
	} else {
		buf = unsafe.Pointer(&modelImage[0])
	}
	if buf == nil {
		return nil, "", fmt.Errorf("model buffer alloc failed")
	}
	ctx := C.poc_init_buf(buf, C.size_t(len(modelImage)))
	if ctx == nil {
		return nil, "", fmt.Errorf("whisper_init_from_buffer (%d bytes)", len(modelImage))
	}
	return ctx, shape, nil
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
	cText := string(buf[:bytesUntilNUL(buf)])
	return cText, nil
}
