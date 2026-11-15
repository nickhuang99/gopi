// Command aucfile calls the Volcengine (ByteDance) Doubao "录音文件识别"
// (audio-file recognition) HTTP API — the product line this account actually
// has quota for (volc.bigasr.auc / volc.bigasr.auc_turbo; the streaming
// SAUC resource_ids are refused with 45000030 "requested resource not
// granted").
//
// Zero third-party dependencies: net/http + encoding/json only.
//
// Flow: POST .../auc/bigmodel/submit (X-Api-Sequence: -1) -> poll
// POST .../auc/bigmodel/query until the result body carries text.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	submitURL = "https://openspeech.bytedance.com/api/v3/auc/bigmodel/submit"
	queryURL  = "https://openspeech.bytedance.com/api/v3/auc/bigmodel/query"

	// ResourceAUC is 录音文件识别 (standard, asynchronous).
	ResourceAUC = "volc.bigasr.auc"
	// ResourceAUCTurbo is 录音文件识别极速版.
	ResourceAUCTurbo = "volc.bigasr.auc_turbo"

	modelName = "bigmodel"

	pollIntervalS = 2
	pollTimeoutS  = 600
	httpTimeoutS  = 120
	maxBodyBytes  = 32 << 20
)

type audioMeta struct {
	URL     string `json:"url,omitempty"`
	Data    string `json:"data,omitempty"`
	Format  string `json:"format"`
	Codec   string `json:"codec"`
	Rate    int    `json:"rate"`
	Bits    int    `json:"bits"`
	Channel int    `json:"channel"`
}

type requestMeta struct {
	ModelName         string          `json:"model_name"`
	EnableITN         bool            `json:"enable_itn"`
	EnablePUNC        bool            `json:"enable_punc"`
	EnableDDC         bool            `json:"enable_ddc"`
	EnableSpeakerInfo bool            `json:"enable_speaker_info"`
	EnableLID         bool            `json:"enable_lid"`
	ShowUtterances    bool            `json:"show_utterances"`
	Corpus            json.RawMessage `json:"corpus,omitempty"` // JSON-encoded string
}

type requestBody struct {
	User    map[string]string `json:"user"`
	Audio   audioMeta         `json:"audio"`
	Request requestMeta       `json:"request"`
}

type responseBody struct {
	Header struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		ReqID   string `json:"reqid"`
	} `json:"header"`
	Result *struct {
		Text       string `json:"text"`
		Utterances []struct {
			Definite bool   `json:"definite"`
			Text     string `json:"text"`
			Speaker  string `json:"speaker_id,omitempty"`
			Start    int    `json:"start_time"`
			End      int    `json:"end_time"`
		} `json:"utterances"`
	} `json:"result"`
	AudioInfo *struct {
		Duration int `json:"duration"`
	} `json:"audio_info"`
}

type metrics struct {
	ResourceID      string   `json:"resource_id"`
	Mode            string   `json:"mode"`
	AudioSeconds    float64  `json:"audio_seconds"`
	BytesUploaded   int      `json:"bytes_uploaded"`
	SubmitMS        int64    `json:"submit_ms"`
	QueryMS         int64    `json:"query_ms"`
	TotalMS         int64    `json:"total_ms"`
	RTF             float64  `json:"rtf"`
	PollCount       int      `json:"poll_count"`
	SubmitLogID     string   `json:"submit_log_id"`
	QueryLogID      string   `json:"query_log_id"`
	StatusCodes     []string `json:"status_codes"`
	ServerDurationM int      `json:"server_audio_duration_ms"`
	Chars           int      `json:"chars"`
	Error           string   `json:"error,omitempty"`
}

func main() {
	var (
		urlArg     = flag.String("audio_url", "", "public URL of the audio (mutually exclusive with -file)")
		fileArg    = flag.String("file", "", "local audio file, sent as base64 audio.data")
		resourceID = flag.String("resource_id", ResourceAUC, "X-Api-Resource-Id: "+ResourceAUC+" or "+ResourceAUCTurbo)
		format     = flag.String("format", "wav", "audio format")
		rate       = flag.Int("rate", 16000, "sample rate")
		channel    = flag.Int("channel", 1, "channels")
		bits       = flag.Int("bits", 16, "bits per sample")
		itn        = flag.Bool("itn", true, "enable_itn")
		punc       = flag.Bool("punc", true, "enable_punc")
		ddc        = flag.Bool("ddc", false, "enable_ddc")
		speaker    = flag.Bool("speaker", false, "enable_speaker_info")
		lid        = flag.Bool("lid", false, "enable_lid")
		corpus     = flag.String("corpus", "", "raw JSON for request.corpus (hotwords etc.)")
		outDir     = flag.String("outdir", ".", "output directory")
		tag        = flag.String("tag", "run", "output file label")
		apiKeyPath = flag.String("api_key_file", os.Getenv("HOME")+"/.doubao/apikey.txt", "file holding the API key")
	)
	flag.Parse()

	if (*urlArg == "") == (*fileArg == "") {
		fatalf("provide exactly one of -audio_url or -file")
	}
	key, err := readKey(*apiKeyPath)
	if err != nil {
		fatalf("api key: %v", err)
	}

	body := requestBody{
		User: map[string]string{"uid": "gopi-doubao-eval"},
		Audio: audioMeta{
			URL: *urlArg, Format: *format, Codec: "raw",
			Rate: *rate, Bits: *bits, Channel: *channel,
		},
		Request: requestMeta{
			ModelName: modelName, EnableITN: *itn, EnablePUNC: *punc,
			EnableDDC: *ddc, EnableSpeakerInfo: *speaker, EnableLID: *lid,
			ShowUtterances: true,
		},
	}
	if *corpus != "" {
		// corpus is a struct on the wire; its `context` member is itself a
		// JSON *string* (the API double-encodes hotwords), e.g.
		//   {"context":"{\"hotwords\":[{\"word\":\"RAG\"}]}"}
		body.Request.Corpus = json.RawMessage(*corpus)
	}
	m := &metrics{ResourceID: *resourceID, Mode: ternaryString(*fileArg != "", "file/base64", "url")}

	if *fileArg != "" {
		raw, err := os.ReadFile(*fileArg)
		if err != nil {
			fatalf("read audio: %v", err)
		}
		m.BytesUploaded = len(raw)
		body.Audio.Data = base64.StdEncoding.EncodeToString(raw)
	}

	reqID := newUUID()
	start := time.Now()

	st, hdr, respBody, err := post(key, *resourceID, reqID, submitURL, body, -1)
	if err != nil {
		fatalf("submit: %v", err)
	}
	m.SubmitMS = time.Since(start).Milliseconds()
	m.SubmitLogID = hdr.Get("X-Tt-Logid")
	apiCode := hdr.Get("X-Api-Status-Code")
	m.StatusCodes = append(m.StatusCodes, apiCode)
	if st != http.StatusOK || apiCode != "20000000" {
		m.Error = fmt.Sprintf("submit HTTP %d status=%s message=%s body=%s",
			st, apiCode, hdr.Get("X-Api-Message"), truncate(respBody))
		emit(*outDir, *tag, "", m, "")
		fatalf("submit rejected: %s", m.Error)
	}

	final, err := poll(key, *resourceID, reqID, m, start)
	if err != nil {
		emit(*outDir, *tag, "", m, "")
		fatalf("query: %v", err)
	}
	m.TotalMS = time.Since(start).Milliseconds()
	if final.AudioInfo != nil {
		m.ServerDurationM = final.AudioInfo.Duration
		m.AudioSeconds = float64(final.AudioInfo.Duration) / 1000.0
	}
	if m.AudioSeconds > 0 {
		m.RTF = float64(m.TotalMS) / 1000.0 / m.AudioSeconds
	}
	text := ""
	if final.Result != nil {
		text = final.Result.Text
	}
	m.Chars = len([]rune(text))
	emit(*outDir, *tag, text, m, mustJSON(final))

	fmt.Printf("[%s] resource=%s mode=%s submit=%dms query=%dms total=%dms rtf=%.3f polls=%d chars=%d submit_logid=%s query_logid=%s\n",
		*tag, *resourceID, m.Mode, m.SubmitMS, m.QueryMS, m.TotalMS, m.RTF,
		m.PollCount, m.Chars, m.SubmitLogID, m.QueryLogID)
	if text == "" {
		os.Exit(2)
	}
}

// poll repeatedly queries until the job finishes or the timeout expires.
func poll(key, resourceID, reqID string, m *metrics, start time.Time) (*responseBody, error) {
	deadline := start.Add(pollTimeoutS * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(pollIntervalS * time.Second)
		m.PollCount++
		st, hdr, bodyBytes, err := post(key, resourceID, reqID, queryURL, map[string]any{}, -1)
		if err != nil {
			return nil, err
		}
		m.QueryMS = time.Since(start).Milliseconds()
		m.QueryLogID = hdr.Get("X-Tt-Logid")
		code := hdr.Get("X-Api-Status-Code")
		m.StatusCodes = append(m.StatusCodes, code)
		if st != http.StatusOK {
			return nil, fmt.Errorf("query HTTP %d status=%s body=%s", st, code, truncate(bodyBytes))
		}
		var rb responseBody
		if err := json.Unmarshal(bodyBytes, &rb); err != nil {
			return nil, fmt.Errorf("decode query body: %w (raw=%s)", err, truncate(bodyBytes))
		}
		switch code {
		case "20000000":
			return &rb, nil
		case "20000001", "20000002":
			continue // still processing
		default:
			return &rb, fmt.Errorf("query status=%s message=%s body=%s", code, rb.Header.Message, truncate(bodyBytes))
		}
	}
	return nil, fmt.Errorf("timed out after %ds (%d polls)", pollTimeoutS, m.PollCount)
}

func post(apiKey, resourceID, reqID, url string, body any, sequence int) (int, http.Header, []byte, error) {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, nil, err
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, url, buf)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("X-Api-Resource-Id", resourceID)
	req.Header.Set("X-Api-Request-Id", reqID)
	req.Header.Set("X-Api-Sequence", fmt.Sprint(sequence))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: httpTimeoutS * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, maxBodyBytes)
	data, err := io.ReadAll(limited)
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, resp.Header, data, nil
}

func emit(dir, tag, transcript string, m *metrics, rawJSON string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatalf("mkdir: %v", err)
	}
	write(filepath.Join(dir, tag+".txt"), transcript)
	write(filepath.Join(dir, tag+".metrics.json"), string(mustJSONIndent(m)))
	if rawJSON != "" {
		write(filepath.Join(dir, tag+".raw.json"), rawJSON)
	}
}

func readKey(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("empty key file")
	}
	return s, nil
}

func newUUID() string {
	b := make([]byte, 16)
	f, err := os.Open("/proc/sys/kernel/random/uuid")
	if err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	defer f.Close()
	if _, err := io.ReadFull(f, b); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	s := strings.TrimSpace(string(b))
	if len(s) == 36 {
		return s
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func ternaryString(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func truncate(b []byte) string {
	const maxLen = 400
	s := string(b)
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func mustJSONIndent(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatalf("marshal: %v", err)
	}
	return b
}

func write(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fatalf("write %s: %v", path, err)
	}
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", a...)
	os.Exit(1)
}
