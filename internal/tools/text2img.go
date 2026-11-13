package tools

// text2img tool: render a text file to PNG images via the pure-Go TrueType
// rasterizer (internal/text2img — zero external dependencies, standard
// library only). The agent tool and the CLI subcommand (gopi tool text2img) share
// this function so CLI == tool_use behavior by construction.

import (
	"fmt"
	"os"

	"github.com/nick/gopi/internal/text2img"
)

// text2imgMaxFileBytes caps the input text file size at 8 MiB.
const text2imgMaxFileBytes = 8 << 20 // 8 MiB

// Text2Img renders src to one or more PNG files. out may be "" (default: src
// with .png extension; multi-page output appends -1, -2 …). opts may be the
// zero value — every field falls back to the validated package defaults
// (20px font / 50 cols / 32px line height / 90 lines per page / 12px margin).
// It returns a human-readable summary (pages, chars, lines, font, output
// paths) suitable for both the agent tool result and the CLI stdout.
func Text2Img(src, out string, opts text2img.Options) (string, error) {
	summary, _, err := Text2ImgDetail(src, out, opts)
	return summary, err
}

// Text2ImgDetail is Text2Img plus the render Result, so a caller that needs the
// MEASURED facts (Ink pixels, missing-glyph count, font chain) can read them
// instead of parsing the summary line.
func Text2ImgDetail(src, out string, opts text2img.Options) (string, *text2img.Result, error) {
	if src == "" {
		return "", nil, fmt.Errorf("missing <input.txt> argument")
	}
	fi, err := os.Stat(src)
	if err != nil {
		return "", nil, fmt.Errorf("无法读取输入文件: %w", err)
	}
	if fi.Size() > text2imgMaxFileBytes {
		return "", nil, fmt.Errorf("输入文件 %.1f MiB 超过上限 8 MiB", float64(fi.Size())/(1<<20))
	}
	res, err := text2img.RenderFile(src, out, opts)
	if err != nil {
		return "", nil, err
	}
	// The summary carries the MEASURED ink count and the font CHAIN, so a
	// "✅" can never sit over a page that inked nothing (the reported bug:
	// a Latin string on a CJK-only face drew zero glyphs and still printed
	// "✅ … 1 页"). A render that inks nothing is already refused by the
	// renderer, so this line is a report of a measurement, not a promise.
	s := fmt.Sprintf("✅ %s → %d 页 (%d 字符, %d 行, 字号 %dpx, 行高 %d, 折行 %d, 墨迹 %d 像素, 字体链 %s)",
		src, res.Pages, res.Chars, res.Lines, res.Size, res.Lh, res.Cols, res.Ink, res.FontChain())
	if res.Missing > 0 {
		s += fmt.Sprintf("\n  ⚠ %d 个字符在字体链中无字形, 未绘制", res.Missing)
	}
	for _, p := range res.Outputs {
		s += fmt.Sprintf("\n  output: %s", p)
	}
	return s, res, nil
}
