package tools

// vision_analyze tool: image understanding / OCR via the vision model
// (deepseek-v4-flash-vision-exp by default). The image is read from a local
// file, base64-encoded inline into an Anthropic image block and sent to the
// vision endpoint in ONE lightweight standalone request — NOT the main loop
// model. The image bytes never enter the main context: only the model's
// understanding (description / OCR text) is returned, protecting the context
// window.
//
// Replaces external OCR scripts: screenshot text recognition / chart analysis
// call vision_analyze directly.
//
// Settings-configurable (zero code change to swap providers):
//   - vision_model:      deepseek-v4-flash-vision-exp (default)
//   - vision_base_url:   https://api.deepseek.com/anthropic (default)
//   - api_key:           reused (same DeepSeek key)

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

const (
	// visionMaxFileBytes caps the image file size at 32 MiB (the documented
	// vision upload ceiling). Larger files are rejected before any bytes are
	// read or base64-encoded.
	visionMaxFileBytes = 32 << 20 // 32 MiB

	// visionDefaultPrompt is used when the model omits the prompt argument.
	visionDefaultPrompt = "描述这张图片，并识别其中的文字（如有）"
)

// visionMediaTypes maps a supported file extension to its Anthropic
// media_type. The vision endpoint accepts JPEG/PNG/GIF/WebP.
var visionMediaTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// IsImagePath reports whether path has a supported image extension. It does
// NOT check existence — a path that merely *looks* like an image is enough
// for the caller to route it to the image path (the load then validates).
func IsImagePath(path string) bool {
	_, ok := visionMediaTypes[strings.ToLower(filepath.Ext(strings.TrimSpace(path)))]
	return ok
}

// ImageFile is a loaded, validated image ready for inline base64 transport.
// It is shared by the vision_analyze tool (standalone OCR request) and the
// image-as-first-class-input CLI path (native image blocks in the main loop),
// so format/size validation lives in exactly one place.
type ImageFile struct {
	Path      string
	MediaType string
	Base64    string
	Bytes     int
}

// LoadImage reads a local image file and validates format + size (extension
// → media_type mapping, ≤ 32 MiB). Returns the base64-encoded bytes and the
// media type. Errors are user-actionable: missing file, unsupported format,
// oversized file, or an OS read failure.
func LoadImage(imagePath string) (*ImageFile, error) {
	imagePath = strings.TrimSpace(imagePath)
	if imagePath == "" {
		return nil, fmt.Errorf("image: path must not be empty")
	}

	// Format support check (extension → media_type) before reading any bytes.
	mediaType, ok := visionMediaTypes[strings.ToLower(filepath.Ext(imagePath))]
	if !ok {
		return nil, fmt.Errorf("unsupported image format %q — supported: jpg/jpeg, png, gif, webp", filepath.Ext(imagePath))
	}

	// Existence + size check (hard ceiling, Rule 1: named constant).
	fi, err := os.Stat(imagePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", imagePath)
		}
		return nil, fmt.Errorf("cannot stat %s: %w", imagePath, err)
	}
	if fi.Size() > visionMaxFileBytes {
		return nil, fmt.Errorf("file too large: %d bytes (max %d)", fi.Size(), visionMaxFileBytes)
	}

	data, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", imagePath, err)
	}

	return &ImageFile{
		Path:      imagePath,
		MediaType: mediaType,
		Base64:    base64.StdEncoding.EncodeToString(data),
		Bytes:     len(data),
	}, nil
}

// VisionAnalyze runs the vision_analyze tool: read a local image file,
// base64-encode it, send it to the vision model with the (optional) prompt,
// and return the model's understanding as plain text. The returned string
// also reports the API usage (input/output tokens) so the caller can see the
// per-image cost. Errors are user-actionable: missing file, unsupported
// format, oversized file, or a transport/API failure.
func VisionAnalyze(imagePath, prompt string) (string, error) {
	img, err := LoadImage(imagePath)
	if err != nil {
		return "", fmt.Errorf("vision_analyze: %w", err)
	}

	client, err := deepseek.NewVisionClient()
	if err != nil {
		return "", err
	}

	text, usage, err := client.VisionAnalyze(context.Background(), img.Base64, img.MediaType, prompt)
	if err != nil {
		return "", fmt.Errorf("vision_analyze: %w", err)
	}

	model := client.VisionModel
	if model == "" {
		model = deepseek.ResolveVisionModel(nil) // fallback for env-only resolution
	}
	return fmt.Sprintf("【vision 分析结果】(模型 %s, 输入 %d tokens / 输出 %d tokens, 图片 %d bytes)\n%s",
		model, usage.InputTokens, usage.OutputTokens, img.Bytes, text), nil
}
