//go:build !embed && !embedgz && !embedq5

package main

/*
#cgo CFLAGS: -I/home/user/work/whisper.cpp/include -I/home/user/work/whisper.cpp/ggml/include
#cgo LDFLAGS: -L/home/user/work/whisper.cpp/build-static/src -L/home/user/work/whisper.cpp/build-static/ggml/src -lwhisper -lggml -lggml-base -lggml-cpu -lm -lstdc++
#include <stdlib.h>
#include <string.h>
#include "whisper.h"

static struct whisper_context* poc_init_file(const char* p) {
    struct whisper_context_params cp = whisper_context_default_params();
    return whisper_init_from_file_with_params(p, cp);
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
	"fmt"
	"os"
	"unsafe"
)

const modelPathEnv = "VOICE_POC_MODEL"

func initEngine() (*C.struct_whisper_context, string, error) {
	p := os.Getenv(modelPathEnv)
	if p == "" {
		p = "/home/user/work/whisper.cpp/models/ggml-small.bin"
	}
	cs := C.CString(p)
	defer C.free(unsafe.Pointer(cs))
	ctx := C.poc_init_file(cs)
	if ctx == nil {
		return nil, "", fmt.Errorf("whisper_init_from_file(%s)", p)
	}
	return ctx, "external-file", nil
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
