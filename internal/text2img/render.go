// Package text2img renders plain text to PNG images using a pure-Go TrueType
// rasterizer — zero external dependencies, standard library only (no CGO, no
// x/image/font). It embeds a minimal SFNT/TrueType parser (cmap 4/12,
// head/maxp/hhea/hmtx/loca/glyf, composite glyphs and TTC collections),
// outline flattening and a 4x supersampled scanline rasterizer with
// non-zero-winding fill.
//
// The renderer was ported from the standalone claude-version text2img tool
// (IoU 99.5% vs FreeType, mean OCR accuracy 96.06% at the default settings)
// so the gopi binary keeps a single-binary, dependency-free text→PNG path.
// CJK is supported via the system TrueType font (autodetected), so Chinese
// text renders natively without any font bundling.
//
// Entry point: RenderFile(src, out, opts) — reads src, wraps long lines,
// paginates automatically and writes one or more PNG files (out-1.png,
// out-2.png … when the text exceeds one page).
package text2img

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/nickhuang99/gopi/internal/scratch"
)

// Default rendering parameters — the validated claude-version settings
// (20px font is the sweet spot for unhinted rendering; 90 lines ≈ 2800
// chars/page). These are the single source of truth; the CLI and the agent
// tool both fall back to them when the user does not override.
const (
	DefaultSize   = 20 // 字号(px, 16px 无 hinting 下略糊, 20px 为无 hinting 最优)
	DefaultCols   = 50 // 超长行折行字符数(短行保留原文行结构)
	DefaultLh     = 32 // 行高(px, ≈字号×1.6)
	DefaultLines  = 90 // 每页最大行数(≈2800 字符/页; 超出自动分页)
	DefaultMargin = 12 // 页边距(px)

	// lineHeightRatio is the line height as a multiple of the font size
	// (DefaultLh/DefaultSize = 32/20 = 1.6) — the ONE place the prose path and
	// the grid path both derive their default from.
	lineHeightRatio = 1.6
	// defaultCellWidthDivisor derives the grid path's default cell width from
	// the font size: a monospace face at N px advances ≈ N/2 px per cell.
	defaultCellWidthDivisor = 2
	// scrollbackRule is the character the band separating a capture's
	// scrollback header from its screen is drawn with, and gridRuleCols how
	// many of them a rule row carries.
	scrollbackRule = "-"
	gridRuleCols   = 72
)

// maxRasterDim caps the 4x supersample grid per glyph (sanity guard against
// corrupt fonts producing absurdly large bitmaps).
const maxRasterDim = 16384

// SS is the supersampling factor for scanline rasterization.
const SS = 4

// The numeric-flag contract shared by the `gopi tool text2img` CLI and this
// package.
//
// An Options field's ZERO VALUE means "the user did not spell this flag":
// RenderFile's norm() reroutes each zero to the Default* below. That is the
// library's contract and it is NOT the CLI's — the subcommand refuses a flag
// that was given as 0 (a positive integer is the stated rule), so the sentinel
// and the floor live here next to the defaults as the ONE definition both
// layers read.
const (
	// MinPositiveInt is the smallest value a NUMERIC text2img flag accepts —
	// the floor `gopi tool text2img` enforces for --size/--cols/--lh/--lines/
	// --margin, and the value its refusal message states.
	MinPositiveInt = 1
	// InvalidFlagInt is the sentinel the CLI's flag parser reports for a value
	// that is not a positive integer (non-numeric, zero or negative). It sits
	// below MinPositiveInt by construction.
	InvalidFlagInt = 0
)

// Options controls rendering. Zero values fall back to the package defaults.
type Options struct {
	Size   int    // 字号(px)
	Cols   int    // 超长行折行字符数
	Lh     int    // 行高(px)
	Lines  int    // 每页最大行数(超出自动分页)
	Margin int    // 页边距(px)
	Font   string // 字体路径("" = 自动探测系统 CJK TrueType 字体)
}

// Result reports what RenderFile produced. Ink and Missing are MEASURED facts
// about the picture, not promises about the code path: a caller (the CLI
// summary, the agent tool) can therefore never report success over a page that
// carries no glyph at all — see the empty-render guard in renderPage.
type Result struct {
	Pages   int      // 写入的图片页数
	Chars   int      // 输入字符总数(含空白)
	Lines   int      // 折行后总行数
	Size    int      // 实际字号(px)
	Lh      int      // 实际行高(px)
	Cols    int      // 实际折行宽度(字符)
	Font    string   // 主字体路径(布局度量真值)
	Fonts   []string // 实际使用的字体链(字形回退顺序)
	Ink     int      // 实测落笔像素数(所有页合计; 0 = 空白页)
	Missing int      // 字体链中找不到字形的不同 rune 数
	Outputs []string // 写出的 PNG 路径(按页)
}

// FontChain renders the chain as "a → b → c" for diagnostics — the font
// report the empty-render guard and the CLI summary both carry.
func (r *Result) FontChain() string { return strings.Join(r.Fonts, " → ") }

// norm applies an option's fallback to the package default when it is unset
// (<= 0). Rule: defaults live in exactly one place — the constants above.
func norm(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// splitOutputPath splits an output path into the stem a page number is appended
// to and the extension it carries, honouring the default render's class SUFFIX
// (scratch.Text2ImgDumpFileSuffix, `.dump.png`) as ONE unit.
//
// WHY THIS EXISTS AT ALL: `filepath.Ext("x.dump.png")` is `.png`, so the naive
// split would name page 2 `x.dump-1.png` — collectable by nothing, and
// `scratch.IsText2ImgDumpName` refuses it by design. The class suffix is named
// here by REFERENCE, never re-spelled (Rule 14), so the renderer and the
// reclaim predicate cannot drift apart.
//
// A path without that suffix keeps the ordinary behaviour (`out` → `out-1.png`,
// `out-2.png`), which is what every explicit --out caller already gets.
func splitOutputPath(out string) (root, ext string) {
	if stem, ok := strings.CutSuffix(out, scratch.Text2ImgDumpFileSuffix); ok && stem != "" {
		return stem, scratch.Text2ImgDumpFileSuffix
	}
	root = strings.TrimSuffix(out, filepath.Ext(out))
	ext = filepath.Ext(out)
	if ext == "" {
		ext = ".png"
	}
	return root, ext
}

// RenderFile renders the text file at src to one or more PNG files and
// returns the page/font summary. out may be empty, and the DEFAULT IS A
// DECLARED SCRATCH CLASS TREE (`<scratch home>/dumps/gopi-text2img`, resolved
// by scratch.Text2ImgDumpFile) — never a file beside src, which is the defect
// scratch_text2img.go measures: src usually lives in the caller's git work
// tree, so the old default left an untracked PNG no sweep collects (Rule 30).
// Pagination appends -1, -2 … to the output stem when the text exceeds one
// page; on a default render the stem stops BEFORE the class suffix, so page 2
// is `text2img-<stamp>-2.dump.png`. All defaults mirror the claude-version
// validated settings.
func RenderFile(src, out string, o Options) (*Result, error) {
	opts := Options{
		Size:   norm(o.Size, DefaultSize),
		Cols:   norm(o.Cols, DefaultCols),
		Lines:  norm(o.Lines, DefaultLines),
		Margin: norm(o.Margin, DefaultMargin),
		Font:   o.Font,
	}
	// 行高默认随字号缩放(≈字号×1.6): Lh 是"每行像素高", 必须 ≥ 字形高度,
	// 否则大字(Size > Lh)会被页高裁切 — DefaultLh=32 只在默认字号 20 下成立。
	// 固定 Lh 会让大字号(如 200px)页面高度不足, 字形被裁切成底部残片。
	if o.Lh <= 0 {
		opts.Lh = int(float64(opts.Size)*1.6 + 0.5)
	} else {
		opts.Lh = o.Lh
	}
	text, err := os.ReadFile(src)
	if err != nil {
		return nil, fmt.Errorf("读取失败: %w", err)
	}
	chain, err := loadFontChain(opts.Font)
	if err != nil {
		return nil, err
	}
	all := wrapLines(string(text), opts.Cols)

	// Missing-glyph census over the whole document, against the WHOLE chain —
	// the class of rune that can never ink (a CJK-only face on a Latin string
	// was exactly this bug), surfaced instead of silently dropped.
	chainMissing := missingGlyphs(string(text), chain)

	// Pagination: split the wrapped lines into pages of opts.Lines.
	// Default output: a DECLARED scratch class tree, never beside the source
	// file (scratch_text2img.go carries the measurement and the four Rule-35
	// answers). `out == ""` is the ONLY path that reaches the resolver, so an
	// explicit --out keeps its behaviour byte for byte.
	if out == "" {
		path, derr := scratch.Text2ImgDumpFile()
		if derr != nil {
			return nil, derr
		}
		out = path
	}
	// The stem/extension split is the MULTI-PAGE contract, and it is done on the
	// class SUFFIX when the path carries one: a default render must paginate as
	// `<stem>-1.dump.png`, `<stem>-2.dump.png` — never `<stem>.dump-1.png`,
	// which no reclaim predicate accepts (scratch.IsText2ImgDumpName) and which
	// a sweep would therefore leave behind forever.
	root, ext := splitOutputPath(out)
	pages := 0
	for i := 0; i < len(all); i += opts.Lines {
		pages++
	}
	res := &Result{
		Chars:   len([]rune(string(text))),
		Lines:   len(all),
		Size:    opts.Size,
		Lh:      opts.Lh,
		Cols:    opts.Cols,
		Font:    chain[0].path,
		Fonts:   chain.paths(),
		Missing: chainMissing,
		Pages:   pages,
	}
	for i := 0; i < len(all); i += opts.Lines {
		end := i + opts.Lines
		if end > len(all) {
			end = len(all)
		}
		name := root + ext
		if pages > 1 {
			name = fmt.Sprintf("%s-%d%s", root, i/opts.Lines+1, ext)
		}
		ink, err := renderPage(chain, float64(opts.Size), all[i:end], opts, name)
		if err != nil {
			return nil, fmt.Errorf("渲染失败: %w", err)
		}
		res.Ink += ink
		res.Outputs = append(res.Outputs, name)
	}
	return res, nil
}

// ---------------- SFNT / TrueType 最小解析器 ----------------

type cmapGroup struct{ start, end, gid uint32 }

type sfnt struct {
	path                string // 字体文件路径(diagnostic: 字体链报告用)
	upem, numGlyph      uint16
	locFmt              int16
	advances            []uint16
	ascender, descender float64 // 字体单位
	cmapF4              map[uint16]uint16
	cmap12              []cmapGroup
	loca                []uint32
	glyf                []byte
}

func be16(b []byte) uint16 { return binary.BigEndian.Uint16(b) }
func be32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }

// parseTTF 解析单字体 TTF 或 TTC(取第 index 个字体)。仅支持 TrueType(glyf) 轮廓。
func parseTTF(data []byte, index int) (*sfnt, error) {
	off := 0
	if len(data) >= 4 && string(data[:4]) == "ttcf" {
		if len(data) < 12 {
			return nil, fmt.Errorf("TTC 头损坏: 长度 %d < 12", len(data))
		}
		n := be32(data[8:12])
		if index < 0 || index >= int(n) {
			return nil, fmt.Errorf("TTC 字体索引 %d 越界(共 %d)", index, n)
		}
		// TTC offset table: each entry is a 4-byte big-endian offset, located
		// at 12 + 4*index. The access below would slice out of range on a
		// truncated collection header.
		p := 12 + 4*index
		if p+4 > len(data) {
			return nil, fmt.Errorf("TTC 表偏移越界: 长度 %d < %d", len(data), p+4)
		}
		off = int(be32(data[p:]))
	}
	if off+12 > len(data) {
		return nil, fmt.Errorf("SFNT 头越界")
	}
	switch string(data[off : off+4]) {
	case "\x00\x01\x00\x00", "true":
	case "OTTO", "typ1":
		return nil, fmt.Errorf("字体为 CFF/PostScript 轮廓，仅支持 TrueType(glyf) 字体")
	default:
		return nil, fmt.Errorf("未知字体格式 %q", data[off:off+4])
	}
	nt := int(be16(data[off+4 : off+6]))
	// The offset table (12 bytes) is followed by nt 16-byte table records.
	// A truncated/forged header could claim an nt larger than the data can
	// hold — validate the whole directory before any record read.
	if off+12+16*nt > len(data) {
		return nil, fmt.Errorf("表目录越界: 声明 %d 个表，需 %d 字节，数据仅 %d", nt, 16*nt, len(data))
	}
	tabs := map[string][2]int{}
	for i := 0; i < nt; i++ {
		p := off + 12 + 16*i
		tag := string(data[p : p+4])
		to, tl := int(be32(data[p+8:p+12])), int(be32(data[p+12:p+16]))
		tabs[tag] = [2]int{to, tl}
	}
	for _, t := range []string{"head", "maxp", "hhea", "hmtx", "cmap", "loca", "glyf"} {
		if _, ok := tabs[t]; !ok {
			return nil, fmt.Errorf("缺少表 %s", t)
		}
	}
	// tableOK validates a table's declared [offset, offset+length) window
	// against the actual data — the core guard against truncated/corrupt
	// font blobs. Every subsequent data[to:to+...] access below relies on it,
	// so a bad window returns a graceful error instead of a slice-bounds panic.
	tableOK := func(t string) (int, error) {
		to, tl := tabs[t][0], tabs[t][1]
		if to < 0 || tl < 0 || to > len(data) || tl > len(data)-to {
			return 0, fmt.Errorf("表 %s 越界: offset %d length %d 超出数据 %d 字节", t, to, tl, len(data))
		}
		return to, nil
	}
	for _, t := range []string{"head", "maxp", "hhea", "hmtx", "cmap", "loca", "glyf"} {
		if _, err := tableOK(t); err != nil {
			return nil, err
		}
	}
	f := &sfnt{}
	// head needs up to offset 52 (locFmt at 50:52) — validate the window the
	// reads actually span, not just start.
	if _, tl := tabs["head"][0], tabs["head"][1]; tl < 52 {
		return nil, fmt.Errorf("head 表过短: %d 字节, 需 >= 52", tl)
	}
	h := tabs["head"][0]
	f.upem = be16(data[h+18 : h+20])
	if f.upem == 0 {
		f.upem = 2048
	}
	f.locFmt = int16(be16(data[h+50 : h+52]))
	// maxp needs glyph count at offset 4:6.
	if _, tl := tabs["maxp"][0], tabs["maxp"][1]; tl < 6 {
		return nil, fmt.Errorf("maxp 表过短: %d 字节, 需 >= 6", tl)
	}
	f.numGlyph = be16(data[tabs["maxp"][0]+4 : tabs["maxp"][0]+6])
	// hhea needs numberOfHMetrics at 34:36; ascender/descender at 4:8.
	if _, tl := tabs["hhea"][0], tabs["hhea"][1]; tl < 36 {
		return nil, fmt.Errorf("hhea 表过短: %d 字节, 需 >= 36", tl)
	}
	f.ascender = float64(int16(be16(data[tabs["hhea"][0]+4 : tabs["hhea"][0]+6])))
	f.descender = float64(int16(be16(data[tabs["hhea"][0]+6 : tabs["hhea"][0]+8])))
	nh := int(be16(data[tabs["hhea"][0]+34 : tabs["hhea"][0]+36]))
	// hmtx holds nh 4-byte advance/lsb pairs. Reading entries [0,nh) needs
	// hm+4*nh <= len(data); reading the fallback advance (f.advances[nh-1])
	// needs hm+4*(nh-1)+2 = hm+4*nh-2 < len(data), implied by the first.
	hm, hmtl := tabs["hmtx"][0], tabs["hmtx"][1]
	if hmtl < 4*nh {
		return nil, fmt.Errorf("hmtx 表过短: %d 字节, 需 >= %d (numberOfHMetrics=%d)", hmtl, 4*nh, nh)
	}
	f.advances = make([]uint16, f.numGlyph)
	for i := 0; i < nh && i < int(f.numGlyph); i++ {
		f.advances[i] = be16(data[hm+4*i : hm+4*i+2])
	}
	if nh > 0 {
		for i := nh; i < int(f.numGlyph); i++ {
			f.advances[i] = f.advances[nh-1]
		}
	}
	cmok, _ := tableOK("cmap") //stylegate:ignore font-table probe; a missing table surfaces later as a render failure, not here
	if err := parseCMap(f, data[cmok:cmok+tabs["cmap"][1]]); err != nil {
		return nil, err
	}
	lo := tabs["loca"]
	n := int(f.numGlyph) + 1
	if f.locFmt == 0 {
		// loca format 0: n 2-byte offsets — needs lo+2*n <= len(data).
		if lo[1] < 2*n {
			return nil, fmt.Errorf("loca 表损坏: %d 字节, 需 >= %d", lo[1], 2*n)
		}
		f.loca = make([]uint32, n)
		for i := 0; i < n; i++ {
			f.loca[i] = uint32(be16(data[lo[0]+2*i:lo[0]+2*i+2])) * 2
		}
	} else {
		if lo[1] < 4*n {
			return nil, fmt.Errorf("loca 表损坏")
		}
		f.loca = make([]uint32, n)
		for i := 0; i < n; i++ {
			f.loca[i] = be32(data[lo[0]+4*i : lo[0]+4*i+4])
		}
	}
	f.glyf = data[tabs["glyf"][0] : tabs["glyf"][0]+tabs["glyf"][1]]
	return f, nil
}

func parseCMap(f *sfnt, cm []byte) error {
	if len(cm) < 4 {
		return fmt.Errorf("cmap 损坏")
	}
	n := int(be16(cm[2:4]))
	for i := 0; i < n; i++ {
		if len(cm) < 12+8*i {
			break
		}
		pid, eid := be16(cm[4+8*i:6+8*i]), be16(cm[6+8*i:8+8*i])
		off := be32(cm[8+8*i : 12+8*i])
		if pid == 3 && eid != 1 {
			continue // 只解析 (3,1) 与 (3,10)
		}
		if int(off)+16 > len(cm) {
			continue
		}
		sub := cm[off:]
		switch be16(sub[0:2]) {
		case 4: // 段式映射(BMP)
			seg := int(be16(sub[6:8])) / 2
			if len(sub) < 16+8*seg {
				continue
			}
			ends := make([]uint16, seg)
			for j := 0; j < seg; j++ {
				ends[j] = be16(sub[14+2*j : 16+2*j])
			}
			starts := make([]uint16, seg)
			for j := 0; j < seg; j++ {
				starts[j] = be16(sub[16+2*seg+2*j : 18+2*seg+2*j])
			}
			deltas := make([]int16, seg)
			for j := 0; j < seg; j++ {
				deltas[j] = int16(be16(sub[16+4*seg+2*j : 18+4*seg+2*j]))
			}
			ro := make([]uint16, seg)
			for j := 0; j < seg; j++ {
				ro[j] = be16(sub[16+6*seg+2*j : 18+6*seg+2*j])
			}
			gidArr := sub[16+8*seg:]
			f.cmapF4 = map[uint16]uint16{}
			for j := 0; j < seg; j++ {
				if ends[j] < starts[j] || int(ends[j]) > 0xFFFF {
					continue
				}
				for r := int(starts[j]); r <= int(ends[j]); r++ {
					g := uint16(0)
					if ro[j] == 0 {
						g = uint16(r+int(deltas[j])) & 0xFFFF
					} else {
						// idRangeOffset 相对 idRangeOffset[i] 元素本身:
						// 数组索引 = ro/2 + (c-start) + i - segCount
						idx := int(ro[j])/2 + r - int(starts[j]) + j - seg
						if idx >= 0 && idx < len(gidArr)/2 {
							g = be16(gidArr[2*idx:])
							if g != 0 {
								g = uint16(int(g)+int(deltas[j])) & 0xFFFF
							}
						}
					}
					if g != 0 && g < f.numGlyph {
						f.cmapF4[uint16(r)] = g
					}
				}
			}
		case 12: // 分组映射(全 Unicode)
			ng := int(be32(sub[12:16]))
			if len(sub) < 16+12*ng {
				continue
			}
			f.cmap12 = make([]cmapGroup, 0, ng)
			for j := 0; j < ng; j++ {
				p := 16 + 12*j
				f.cmap12 = append(f.cmap12, cmapGroup{
					start: be32(sub[p : p+4]),
					end:   be32(sub[p+4 : p+8]),
					gid:   be32(sub[p+8 : p+12]),
				})
			}
		}
	}
	return nil
}

// glyph 返回 rune 对应字形 ID(0 = 缺失)。
func (f *sfnt) glyph(r rune) uint16 {
	if r <= 0xFFFF {
		if g, ok := f.cmapF4[uint16(r)]; ok {
			return g
		}
	}
	u := uint32(r)
	for _, g := range f.cmap12 {
		if u >= g.start && u <= g.end {
			return uint16(g.gid + u - g.start)
		}
	}
	return 0
}

// advance 返回字形前进宽度(字体单位)。
func (f *sfnt) advance(gid uint16) float64 {
	if int(gid) >= len(f.advances) {
		return float64(f.upem) // 缺失字形按全宽计
	}
	return float64(f.advances[gid])
}

// ---------------- 字形轮廓 ----------------

type pt struct {
	x, y float64
	on   bool
}

// glyphContours 提取字形轮廓(递归展开复合字形), 字体单位, y 向上。
func (f *sfnt) glyphContours(gid uint16) [][]pt {
	if int(gid) >= len(f.loca)-1 {
		return nil
	}
	data := f.glyf[f.loca[gid]:f.loca[gid+1]]
	if len(data) < 10 {
		return nil
	}
	nc := int(int16(be16(data[0:2])))
	if nc >= 0 {
		cs, ok := parseSimple(data, nc)
		if !ok {
			return nil
		}
		return cs
	}
	// 复合字形
	var out [][]pt
	p := 10
	for p+4 <= len(data) {
		flags := be16(data[p : p+2])
		cg := be16(data[p+2 : p+4])
		p += 4
		dx, dy := 0.0, 0.0
		if flags&1 != 0 { // 16 位偏移
			if p+4 > len(data) {
				break
			}
			dx = float64(int16(be16(data[p : p+2])))
			dy = float64(int16(be16(data[p+2 : p+4])))
			p += 4
		} else {
			if p+2 > len(data) {
				break
			}
			dx = float64(int8(data[p]))
			dy = float64(int8(data[p+1]))
			p += 2
		}
		a, b, c, d := 1.0, 0.0, 0.0, 1.0
		switch {
		case flags&8 != 0: // 等比缩放
			if p+2 > len(data) {
				break
			}
			s := float64(int16(be16(data[p:p+2]))) / 16384
			a, d = s, s
			p += 2
		case flags&64 != 0: // x/y 缩放
			if p+4 > len(data) {
				break
			}
			a = float64(int16(be16(data[p:p+2]))) / 16384
			d = float64(int16(be16(data[p+2:p+4]))) / 16384
			p += 4
		case flags&128 != 0: // 2x2 变换
			if p+8 > len(data) {
				break
			}
			a = float64(int16(be16(data[p:p+2]))) / 16384
			b = float64(int16(be16(data[p+2:p+4]))) / 16384
			c = float64(int16(be16(data[p+4:p+6]))) / 16384
			d = float64(int16(be16(data[p+6:p+8]))) / 16384
			p += 8
		}
		for _, cont := range f.glyphContours(cg) {
			tr := make([]pt, len(cont))
			for i, q := range cont {
				tr[i] = pt{a*q.x + c*q.y + dx, b*q.x + d*q.y + dy, q.on}
			}
			out = append(out, tr)
		}
		if flags&32 == 0 { // 无更多组件
			break
		}
	}
	return out
}

// parseSimple 解析简单字形轮廓, 按 endPtsOfContours 切分为独立闭合轮廓。
// 每个轮廓内保留 on/off 标记; 相邻两 off 点之间插入中点 on
// (TrueType 二次贝塞尔约定)。坐标 y 向上。
func parseSimple(data []byte, nc int) ([][]pt, bool) {
	if len(data) < 10+2*nc {
		return nil, false
	}
	ends := make([]int, nc)
	for j := 0; j < nc; j++ {
		ends[j] = int(be16(data[10+2*j : 12+2*j]))
	}
	n := ends[nc-1] + 1
	p := 10 + 2*nc
	if p+2 > len(data) {
		return nil, false
	}
	p += 2 + int(be16(data[p:p+2])) // 跳过指令
	flags := make([]byte, n)
	for i := 0; i < n; {
		if p >= len(data) {
			return nil, false
		}
		fl := data[p]
		p++
		flags[i] = fl
		i++
		if fl&8 != 0 { // repeat
			if p >= len(data) {
				return nil, false
			}
			r := int(data[p])
			p++
			for ; r > 0 && i < n; r-- {
				flags[i] = fl
				i++
			}
		}
	}
	xs := make([]int32, n)
	x := int32(0)
	for i := 0; i < n; i++ {
		fl := flags[i]
		switch {
		case fl&2 != 0: // x-short
			if p >= len(data) {
				return nil, false
			}
			v := int32(data[p])
			p++
			if fl&16 == 0 {
				v = -v
			}
			x += v
		case fl&16 == 0: // 带符号 16 位
			if p+2 > len(data) {
				return nil, false
			}
			x += int32(int16(be16(data[p : p+2])))
			p += 2
		}
		xs[i] = x
	}
	ys := make([]int32, n)
	y := int32(0)
	for i := 0; i < n; i++ {
		fl := flags[i]
		switch {
		case fl&4 != 0: // y-short
			if p >= len(data) {
				return nil, false
			}
			v := int32(data[p])
			p++
			if fl&32 == 0 {
				v = -v
			}
			y += v
		case fl&32 == 0:
			if p+2 > len(data) {
				return nil, false
			}
			y += int32(int16(be16(data[p : p+2])))
			p += 2
		}
		ys[i] = y
	}
	// 按 endPtsOfContours 切分轮廓
	var out [][]pt
	start := 0
	for _, e := range ends {
		m := e - start + 1
		raw := make([]pt, 0, m*2)
		for i := 0; i < m; i++ {
			k := start + i
			q := pt{float64(xs[k]), float64(ys[k]), flags[k]&1 != 0}
			j := start + (i+1)%m
			raw = append(raw, q)
			if !q.on && flags[j]&1 == 0 { // 两 off 相邻 → 插中点 on
				raw = append(raw, pt{(q.x + float64(xs[j])) / 2, (q.y + float64(ys[j])) / 2, true})
			}
		}
		out = append(out, raw)
		start = e + 1
	}
	return out, true
}

// ---------------- 轮廓扁平化 + 扫描线栅格化 ----------------

// flatten 把轮廓序列转线段(px 空间)。约定: on→on 为直线; on→off→on 为二次段。
func flatten(pts []pt, tol float64) []float64 {
	var segs []float64 // x1,y1,x2,y2 交错
	n := len(pts)
	if n < 2 {
		return segs
	}
	s := 0
	for i := 0; i < n; i++ {
		if pts[i].on {
			s = i
			break
		}
	}
	i := s
	for {
		p0 := pts[i]
		j := (i + 1) % n
		if pts[j].on {
			segs = append(segs, p0.x, p0.y, pts[j].x, pts[j].y)
			i = j
		} else {
			k := (i + 2) % n
			segs = append(segs, flattenQuad(p0, pts[j], pts[k], tol)...)
			i = k
		}
		if i == s {
			break
		}
	}
	return segs
}

// flattenQuad 递归细分二次贝塞尔至直线段(px 空间)。
func flattenQuad(p0, c, p1 pt, tol float64) []float64 {
	type qseg struct{ a, b, c pt }
	stack := []qseg{{p0, c, p1}}
	var segs []float64
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		dx := s.a.x - 2*s.b.x + s.c.x
		dy := s.a.y - 2*s.b.y + s.c.y
		if dx*dx+dy*dy <= tol*tol {
			segs = append(segs, s.a.x, s.a.y, s.c.x, s.c.y)
			continue
		}
		m := pt{(s.a.x + 2*s.b.x + s.c.x) / 4, (s.a.y + 2*s.b.y + s.c.y) / 4, true}
		l := pt{(s.a.x + s.b.x) / 2, (s.a.y + s.b.y) / 2, true}
		r := pt{(s.b.x + s.c.x) / 2, (s.b.y + s.c.y) / 2, true}
		// 左半 (a,l,m) + 右半 (m,r,c) — 两半在 m 处连续
		stack = append(stack, qseg{m, r, s.c}, qseg{s.a, l, m})
	}
	return segs
}

// rasterGlyph 栅格化字形。返回 alpha 掩码与相对笔位的偏移(px)。
// 坐标: 笔位在 (0,0), 基线过 (0,0), 屏幕 y 向下(基线下方为正)。
// 掩码原点 = (floor(bbox.xMin), floor(bbox.yMin)) 的屏幕坐标。
func (f *sfnt) rasterGlyph(gid uint16, scale float64) (alpha []byte, w, h, ox, oy int) {
	contours := f.glyphContours(gid)
	if len(contours) == 0 {
		return nil, 0, 0, 0, 0
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range contours {
		for _, q := range c {
			if q.x < minX {
				minX = q.x
			}
			if q.x > maxX {
				maxX = q.x
			}
			if q.y < minY {
				minY = q.y
			}
			if q.y > maxY {
				maxY = q.y
			}
		}
	}
	// 屏幕坐标(px): sx = x*scale, sy = -y*scale
	sx0, sy0 := minX*scale, -maxY*scale
	sx1, sy1 := maxX*scale, -minY*scale
	if sx1 <= sx0 || sy1 <= sy0 {
		return nil, 0, 0, 0, 0
	}
	oxf, oyf := math.Floor(sx0), math.Floor(sy0)
	// 4x 网格
	gw := int(math.Ceil(sx1)-oxf) * SS
	gh := int(math.Ceil(sy1)-oyf) * SS
	if gw <= 0 || gh <= 0 || gw > maxRasterDim || gh > maxRasterDim {
		return nil, 0, 0, 0, 0
	}
	// 线段 → 4x 网格坐标(flatten 在 px 空间, 平坦度 ≤ 0.05px)
	const tol = 0.05
	segs := make([]float64, 0, 64)
	for _, c := range contours {
		sc := make([]pt, len(c))
		for i, q := range c {
			sc[i] = pt{q.x * scale, -q.y * scale, q.on} // 屏幕 y 向下
		}
		segs = append(segs, flatten(sc, tol)...)
	}
	// 扫描线: 每 4x 行收集交点(带方向, 用于非零环绕填充)
	type xd struct {
		x, d float64 // 交点位置与方向(+1 向下 / -1 向上)
	}
	xsec := make([][]xd, gh)
	for k := 0; k+3 < len(segs); k += 4 {
		x1, y1, x2, y2 := segs[k]*SS-oxf*SS, segs[k+1]*SS-oyf*SS, segs[k+2]*SS-oxf*SS, segs[k+3]*SS-oyf*SS
		if y1 == y2 {
			continue
		}
		lo, hi := y1, y2
		if lo > hi {
			lo, hi = hi, lo
		}
		r0, r1 := int(math.Floor(lo)), int(math.Ceil(hi))
		if r0 < 0 {
			r0 = 0
		}
		if r1 > gh {
			r1 = gh
		}
		dir := 1.0
		if y2 < y1 {
			dir = -1
		}
		for r := r0; r < r1; r++ {
			// Half-open vertex rule: a crossing is emitted only for rows whose
			// center lies STRICTLY inside [lo, hi). Two segments sharing a
			// vertex at non-integer y otherwise both contribute a crossing in
			// the vertex's row → the winding count doubles (odd crossings,
			// leaks) and filled regions over-cover. This is the standard
			// polygon-scanline "count the vertex once" convention.
			yc := float64(r) + 0.5
			if yc < lo || yc >= hi {
				continue
			}
			x := x1 + (yc-y1)*(x2-x1)/(y2-y1)
			xsec[r] = append(xsec[r], xd{x, dir})
		}
	}
	// 非零环绕填充(TrueType 原生规则, 正确处理 0 宽 slit 结构)
	cells := make([]float64, gh*gw)
	for r := 0; r < gh; r++ {
		xs := xsec[r]
		if len(xs) == 0 {
			continue
		}
		sort.Slice(xs, func(i, j int) bool { return xs[i].x < xs[j].x })
		row := cells[r*gw:]
		w := 0.0
		for i := 0; i < len(xs); i++ {
			if w != 0 && i > 0 {
				a, b := xs[i-1].x, xs[i].x
				if b > 0 && a < float64(gw) {
					if a < 0 {
						a = 0
					}
					if b > float64(gw) {
						b = float64(gw)
					}
					for c := int(math.Floor(a)); c < int(math.Ceil(b)); c++ {
						row[c] += math.Min(b, float64(c+1)) - math.Max(a, float64(c))
					}
				}
			}
			w += xs[i].d
		}
	}
	// 4x4 降采样 → 输出掩码
	w, h = gw/SS, gh/SS
	alpha = make([]byte, w*h)
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			var s float64
			for sy := 0; sy < SS; sy++ {
				for sx := 0; sx < SS; sx++ {
					s += cells[(py*SS+sy)*gw+px*SS+sx]
				}
			}
			a := s / 16 * 255 // 16 个子像素
			if a > 255 {
				a = 255
			}
			if a < 0 {
				a = 0
			}
			alpha[py*w+px] = byte(a + 0.5)
		}
	}
	return alpha, w, h, int(oxf), int(oyf)
}

// ---------------- 网格渲染(终端捕获用) ----------------

// GridOptions controls RenderGrid — the monospace-grid path a terminal capture
// takes. It differs from Options (the prose path) in two ways that matter for
// a screen: the input is already a GRID of rows (never re-wrapped, so a
// terminal capture's layout survives intact) and every cell is one global
// advance, so column N lands at the same x on every row.
type GridOptions struct {
	Size       int    // 字号(px)
	CellW      int    // 单元格宽(px); <=0 → 从字体实测算出终端的等宽前进量
	Lh         int    // 行高(px); <=0 → 字号×1.6
	Margin     int    // 页边距(px)
	Font       string // 字体路径("" = 自动探测)
	Scrollback int    // 顶部附带的回滚行数(0 = 不附带)
}

// GridFonts are the monospace faces tried first on the grid path. A terminal
// cell is a MONOSPACE fact: DejaVuSansMono covers Latin/box-drawing at one
// advance, and the CJK fallback (internal/lineedit's width rule gives CJK two
// cells) is honoured by the renderer painting a wide rune across two cells.
// The order is the detection order — the first existing file wins, and
// findFont's system chain is the fallback when none of these exist.
var GridFonts = []string{
	"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
	"/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
}

// RenderGrid rasterizes a terminal cell grid to a PNG at out and writes the
// scrollback header the caller asks for. rows are the screen's lines, already
// laid out; each row is painted with ONE global advance (the monospace cell),
// and a row WIDER than cols is clipped rather than wrapped — the grid is the
// truth and re-wrapping it would be a lie about the screen.
func RenderGrid(out string, rows, scrollback []string, o GridOptions) (*Result, error) {
	res, img, err := gridImage(rows, scrollback, o)
	if err != nil {
		return nil, err
	}
	fh, err := os.Create(out)
	if err != nil {
		return nil, err
	}
	defer fh.Close() //stylegate:ignore deferred close of a file whose encode error is propagated below
	if err := png.Encode(fh, img); err != nil {
		return nil, fmt.Errorf("PNG 编码失败: %w", err)
	}
	res.Outputs = []string{out}
	return res, nil
}

// RenderGridBytes is RenderGrid's in-memory form — the shape a capture pipeline
// needs, so a terminal capture never round-trips through a temp file.
func RenderGridBytes(rows, scrollback []string, o GridOptions) ([]byte, *Result, error) {
	res, img, err := gridImage(rows, scrollback, o)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, nil, fmt.Errorf("PNG 编码失败: %w", err)
	}
	res.Outputs = nil
	return buf.Bytes(), res, nil
}

// gridImage lays out and paints a grid crop, returning the image and the
// summary both public entry points report.
func gridImage(rows, scrollback []string, o GridOptions) (*Result, *image.RGBA, error) {
	if len(rows) == 0 {
		// An empty grid would render a blank page that LOOKS like a successful
		// capture of a blank screen. Refuse it: a capture with nothing on it is
		// a failed capture (Rule 15).
		return nil, nil, fmt.Errorf("网格渲染: 没有屏幕行可渲染(空捕获)")
	}
	o.Size = norm(o.Size, DefaultSize)
	o.Margin = norm(o.Margin, DefaultMargin)
	if o.Lh <= 0 {
		o.Lh = int(float64(o.Size)*lineHeightRatio + 0.5)
	}
	if o.CellW <= 0 {
		o.CellW = norm(o.Size, DefaultSize) / defaultCellWidthDivisor
	}
	if o.CellW <= 0 {
		o.CellW = 1
	}

	// The grid's chain: the monospace face is the metric font (one global
	// advance per cell IS the layout), one wordspace face, and a CJK face
	// behind it — a mono Latin face has no CJK glyphs, so without the fallback
	// a Chinese screen would clip to blank cells. gridFont keeps the historical
	// single-path query for the monospace-nature test.
	var gridPaths []string
	if gp := gridFont(o.Font); gp != "" {
		gridPaths = []string{gp}
	}
	gridPaths = append(gridPaths, fontSearchPaths(wordspaceFontNames, fontDirs())...)
	gridPaths = append(gridPaths, fontSearchPaths(cjkFontNames, fontDirs())...)
	if o.Font != "" {
		gridPaths = append([]string{o.Font}, gridPaths...)
	}
	chain, err := loadChainFromPaths(gridPaths, coverageLatinProbe)
	if err != nil {
		return nil, nil, err
	}
	f := chain[0]
	scale := float64(o.Size) / float64(f.upem)
	cell := float64(o.CellW)

	// The scrollback band sits above the screen, closed by a rule row so the
	// boundary between "history" and "what is on screen now" is visible in the
	// image itself (a reader must not have to count rows to find the screen).
	// The scrollback band sits above the screen as a bare band; the rule that
	// closes it is added once the screen's width is known (below), so the band
	// cannot be wider than the terminal it came from.
	head := make([]string, 0, len(scrollback)+1)
	head = append(head, scrollback...)
	all := append(append([]string{}, head...), rows...)

	// The screen's own width is the image's width: maxCols is measured from the
	// SCREEN rows only, so neither a longer scrollback row nor the rule that
	// closes the band can widen the picture past the terminal it came from
	// (both are clipped to the screen's columns below).
	maxCols := 0
	for _, l := range rows {
		if n := runeCount(l); n > maxCols {
			maxCols = n
		}
	}
	// The band is closed by a rule row drawn to the screen's width, so the
	// boundary between "history" and "what is on screen now" is visible in the
	// image itself — a reader must not have to count rows to find the screen.
	if len(scrollback) > 0 {
		// The rule row is drawn to the TERMINAL's width, never wider: the band
		// marks where the screen starts and must not change the image's shape.
		rule := strings.Repeat(scrollbackRule, maxCols)
		head = append(head[:len(scrollback):len(scrollback)], rule)
	}
	all = append(append([]string{}, head...), rows...)
	W := int(math.Ceil(float64(maxCols)*cell)) + 2*o.Margin
	H := len(all)*o.Lh + 2*o.Margin
	if W < 1 || H < 1 {
		return nil, nil, fmt.Errorf("网格渲染尺寸非法: %dx%d", W, H)
	}
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	black := color.RGBA{0, 0, 0, 255}
	baseline0 := float64(o.Margin) + f.ascender*scale

	ink := 0
	for li, line := range all {
		baseline := baseline0 + float64(li)*float64(o.Lh)
		col := 0
		for _, r := range line {
			w := runeCellsForGrid(r) // 0/1/2 cells, the terminal's own rule
			if col+w > maxCols {
				break // clip: the grid's width is the truth, never a wrap
			}
			if w > 0 {
				ink += drawChainGlyph(img, chain, float64(o.Size), r, float64(o.Margin)+float64(col)*cell, baseline, black)
			}
			col += w
			if col >= maxCols {
				break
			}
		}
	}
	// Ink guard for the grid path too: a capture whose cells all mapped to
	// glyph 0 would otherwise ship a blank PNG as a successful capture (the
	// same false-success shape as the prose path's bug). Rule 15.
	nonSpace := 0
	for _, l := range all {
		for _, r := range l {
			if !unicode.IsSpace(r) {
				nonSpace++
			}
		}
	}
	if nonSpace > 0 && ink == 0 {
		return nil, nil, fmt.Errorf("网格渲染: %d 个非空白单元落笔 0 像素 — 字体链 %s 无可用字形, 拒绝写出空白捕获", nonSpace, chain.String())
	}
	return &Result{
		Pages:   1,
		Lines:   len(all),
		Size:    o.Size,
		Lh:      o.Lh,
		Cols:    maxCols,
		Font:    chain[0].path,
		Fonts:   chain.paths(),
		Ink:     ink,
		Missing: missingGlyphs(strings.Join(all, "\n"), chain),
	}, img, nil
}

// drawChainGlyph paints rune r with the FIRST font in the chain that can: the
// glyph-fallback rule. The pen position is always supplied by the caller from
// the PRIMARY font's advance, so mixing faces never moves the pen and the
// layout the primary font measured stays the truth. Each face rasterizes at its
// OWN units-per-em scale so a fallback glyph is drawn at the requested size.
//
// It returns the number of pixels inked — 0 when no font in the chain has a
// drawable glyph for r. That count is what makes ink a MEASUREMENT instead of
// a claim (Rule 10.2): the caller sums it and refuses a page that carries
// none.
func drawChainGlyph(img *image.RGBA, chain fontChain, size float64, r rune, penX, baseline float64, ink color.RGBA) int {
	for _, f := range chain {
		gid := f.glyph(r)
		if gid == 0 {
			continue
		}
		alpha, w, h, ox, oy := f.rasterGlyph(gid, size/float64(f.upem))
		if w == 0 {
			continue // 空字形(空格, 或仅有 cmap 映射): 让链上后面的字体有机会落笔
		}
		return rasterInto(img, alpha, w, h, ox, oy, penX, baseline, ink)
	}
	return 0
}

// rasterInto blits one already-rasterized glyph box onto img at
// (penX+ox, baseline+oy) and returns how many pixels it inked. Off-image
// pixels are clipped and not counted.
func rasterInto(img *image.RGBA, alpha []byte, w, h, ox, oy int, penX, baseline float64, ink color.RGBA) int {
	if w == 0 || h == 0 {
		return 0
	}
	dx, dy := int(penX)+ox, int(baseline)+oy
	b := img.Bounds()
	inked := 0
	for y := 0; y < h; y++ {
		py := dy + y
		if py < b.Min.Y || py >= b.Max.Y {
			continue
		}
		for x := 0; x < w; x++ {
			px := dx + x
			if px < b.Min.X || px >= b.Max.X {
				continue
			}
			a := alpha[y*w+x]
			if a == 0 {
				continue
			}
			img.SetRGBA(px, py, blend(ink, a))
			inked++
		}
	}
	return inked
}

// blend composites glyph coverage a over the white page: full coverage is the
// ink colour itself, partial coverage the grey in between.
func blend(ink color.RGBA, a byte) color.RGBA {
	if a == 255 {
		return ink
	}
	v := uint8(255 - int(a))
	return color.RGBA{v, v, v, 255}
}

// gridFont picks the monospace face for the grid path.
func gridFont(custom string) string {
	if custom != "" {
		return custom
	}
	for _, c := range GridFonts {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// runeCount is the number of CELLS a grid row occupies (CJK counts two).
func runeCount(s string) int {
	n := 0
	for _, r := range s {
		n += runeCellsForGrid(r)
	}
	return n
}

// runeCellsForGrid mirrors internal/lineedit's runeCells — the terminal's own
// width rule — without importing it (internal/lineedit imports nothing from
// here, and this package stays a leaf). The RANGES are the same East Asian
// Wide/Fullwidth set; the asymmetry this could introduce is closed by a test
// that asserts the two functions agree (TestGridRuneCellsAgreesWithLineedit).
func runeCellsForGrid(r rune) int {
	switch {
	case r == 0, r == '\n', r == '\r':
		return 0
	case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r), unicode.Is(unicode.Cf, r):
		return 0
	case isWideRuneForGrid(r):
		return 2
	default:
		return 1
	}
}

// gridWideRanges is the East Asian Wide/Fullwidth set as a table — the same
// ranges internal/lineedit's isWideRune carries, kept here because this package
// must stay a leaf (it imports nothing from the module). The agreement is
// asserted by a test, not by hope.
var gridWideRanges = []struct{ lo, hi rune }{
	{0x1100, 0x115F},
	{0x2E80, 0x303E},
	{0x3041, 0x33FF},
	{0x3400, 0x4DBF},
	{0x4E00, 0x9FFF},
	{0xA000, 0xA4CF},
	{0xAC00, 0xD7A3},
	{0xF900, 0xFAFF},
	{0xFE30, 0xFE6F},
	{0xFF00, 0xFF60},
	{0xFFE0, 0xFFE6},
	{0x1F300, 0x1F64F},
	{0x1F900, 0x1F9FF},
	{0x20000, 0x2FFFD},
	{0x30000, 0x3FFFD},
}

// isWideRuneForGrid reports whether r occupies two terminal cells.
func isWideRuneForGrid(r rune) bool {
	for _, rg := range gridWideRanges {
		if r >= rg.lo && r <= rg.hi {
			return true
		}
	}
	return false
}

// ---------------- 布局 + 渲染 ----------------

// wrapLines 按 cols 折行; 短行保留原文行结构; \r\n/\r 归一化为 \n; 去 BOM。
func wrapLines(text string, cols int) [][]rune {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimPrefix(text, "\uFEFF") // BOM
	var out [][]rune
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.ReplaceAll(ln, "\t", "    ")
		r := []rune(ln)
		if len(r) == 0 {
			out = append(out, nil)
			continue
		}
		for len(r) > cols {
			out = append(out, r[:cols])
			r = r[cols:]
		}
		out = append(out, r)
	}
	return out
}

func lineWidth(f *sfnt, scale float64, line []rune) float64 {
	var w float64
	for _, r := range line {
		w += f.advance(f.glyph(r)) * scale
	}
	return w
}

// renderPage 把一页 lines 渲染为白底黑字 PNG 写入 path, 返回实测落笔像素数。
//
// A page whose text inks ZERO pixels is a FAILURE, not a success: the caller
// must never report "✅" over a blank picture (this is the reported bug — the
// Latin text mapped to glyph 0 in a CJK-only face, the layout still sized the
// image to the text and the summary still said "1 页"). Rule 15 + 10.2: ink is
// MEASURED here and a zero measurement refuses the page loudly, naming the
// font chain and the measured count.
func renderPage(chain fontChain, size float64, lines [][]rune, opts Options, path string) (int, error) {
	primary := chain[0]
	scale := size / float64(primary.upem) // 主字体度量: 行宽/基线/笔位全由它决定
	maxW := 0.0
	chars := 0
	for _, l := range lines {
		if w := lineWidth(primary, scale, l); w > maxW {
			maxW = w
		}
		for _, r := range l {
			// The layout's own idea of "text": anything that is neither
			// whitespace nor a line break must leave ink somewhere.
			if !unicode.IsSpace(r) {
				chars++
			}
		}
	}
	W := int(math.Ceil(maxW)) + 2*opts.Margin
	H := len(lines)*opts.Lh + 2*opts.Margin
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	// 白底
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	black := color.RGBA{0, 0, 0, 255}
	baseline0 := float64(opts.Margin) + primary.ascender*scale
	ink := 0
	for li, line := range lines {
		baseline := baseline0 + float64(li)*float64(opts.Lh)
		penX := float64(opts.Margin)
		for _, r := range line {
			// 笔位恒由主字体推进: 主字体缺失字形时 advance() 按全宽计, 布局不会塌缩。
			gid := primary.glyph(r)
			ink += drawChainGlyph(img, chain, size, r, penX, baseline, black)
			penX += primary.advance(gid) * scale
		}
	}
	if chars > 0 && ink == 0 {
		return 0, fmt.Errorf("空白渲染: %d 个非空白字符落笔 0 像素 — 字体链 %s 对这段文本没有可用字形(链上每个字体都缺该 rune 的 glyph); 不写出空白图",
			chars, chain.String())
	}
	fh, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer fh.Close()
	if err := png.Encode(fh, img); err != nil {
		return 0, err
	}
	return ink, nil
}

// ---------------- 字体链(字形回退) ----------------

// fontChain is an ORDERED glyph-fallback chain. The FIRST font is the metric
// font: it alone advances the pen and measures line width, so the layout is
// exactly what the primary face says. Each member rasterizes at its OWN
// units-per-em scale (faces differ: DejaVu 2048, DroidSansFallback 256), so a
// fallback glyph keeps the requested point size instead of inheriting the
// primary's scale — the baseline stays the primary's, because the baseline is
// a layout fact.
type fontChain []*sfnt

// paths lists the chain's font files in order — the diagnostic string the
// empty-render guard and the CLI summary report.
func (c fontChain) paths() []string {
	out := make([]string, 0, len(c))
	for _, f := range c {
		out = append(out, f.path)
	}
	return out
}

// String renders the chain for a diagnostic: "primary → fallback → …".
func (c fontChain) String() string { return strings.Join(c.paths(), " → ") }

// Coverage probes are only meaningful for the SINGLE fonts we might put in a
// chain — never for a whole face (font.go's chain already carries real
// coverage by construction). Keep the set small and generic: the point is to
// catch a face with NO Latin coverage (DroidSansFallbackFull.ttf: ASCII maps
// to glyph 0, so every ASCII rune silently inks nothing) and to ignore CFF
// faces this package cannot outline at all.
var (
	// coverageLatinProbe is the rune set a general-purpose chain must ink.
	coverageLatinProbe = []rune("Aa0.")
	// coverageCJKProbe is the rune set a CJK fallback must ink.
	coverageCJKProbe = []rune("中文")
)

// faceHas reports whether f maps EVERY rune in rs to a non-zero glyph.
func faceHas(f *sfnt, rs []rune) bool {
	if f == nil {
		return false
	}
	for _, r := range rs {
		if f.glyph(r) == 0 {
			return false
		}
	}
	return true
}

// loadFontChain builds the ordered glyph-fallback font chain for opts' font
// request and returns it READY TO USE: every entry parsed, the first usable
// face first, and a chain that has no glyph at all refused outright.
//
// Ordering rules (the bug this closes): the metric font is the first face that
// covers Latin — a face with zero Latin coverage cannot be the layout metric,
// because Latin text would then map to glyph 0 and ink NOTHING while the page
// count and ✅ summary still ran. CJK-capable faces are appended behind it, so
// one chain renders mixed Latin+CJK text. A caller-supplied font (-font PATH)
// pins the metric font and is honoured first; if it lacks Latin coverage the
// chain LOUDLY notes the fallback rather than silently drawing blank Latin.
func loadFontChain(custom string) (fontChain, error) {
	dirs := fontDirs()
	if custom != "" {
		if _, err := os.Stat(custom); err != nil {
			return nil, fmt.Errorf("字体不存在: %s", custom)
		}
		// A pinned font is the metric font and is honoured first. If it lacks
		// Latin or CJK coverage the chain appends a fallback and SAYS SO on
		// stderr — a silent fallback would make the layout face a surprise.
		f, err := loadFontFile(custom)
		if err != nil {
			return nil, fmt.Errorf("解析字体失败: %w", err)
		}
		paths := []string{custom}
		if !faceHas(f, coverageLatinProbe) {
			fmt.Fprintf(os.Stderr, "gopi tool text2img: 注意: 字体 %s 无 Latin 字形, Latin 文本将由字体链回退绘制\n", custom)
			paths = append(paths, fontSearchPaths(latinFontNames, dirs)...)
		}
		if !faceHas(f, coverageCJKProbe) {
			paths = append(paths, fontSearchPaths(cjkFontNames, dirs)...)
		}
		paths = append(paths, fontSearchPaths(wordspaceFontNames, dirs)...)
		// A pinned face is honoured as the metric font even when it lacks Latin
		// coverage (the user asked for exactly that face; the warning above is
		// the loud part) — the chain still supplies the missing glyphs, so the
		// page is never blank. Only AUTODETECTION refuses a Latin-less metric
		// font, because no user asked for one.
		return loadChainFromPaths(paths, nil)
	}
	// Default chain: a face with BOTH Latin and CJK when the host has one (the
	// validated metric font — keeps the layout identical to the single-face
	// reference), else a Latin metric face first (its advance drives layout),
	// then a face with a real wordspace advance (the layout-bbox constant),
	// then a CJK face. CJK last means a host whose ONLY font is CJK still
	// renders Chinese (the reported bug's host) while Latin is never left to a
	// Latin-less face.
	paths := fontSearchPaths(preferredFontNames, dirs)
	paths = append(paths, fontSearchPaths(latinFontNames, dirs)...)
	paths = append(paths, fontSearchPaths(wordspaceFontNames, dirs)...)
	paths = append(paths, fontSearchPaths(cjkFontNames, dirs)...)
	return loadChainFromPaths(paths, coverageLatinProbe)
}

// loadChainFromPaths parses the candidate paths in order and returns the
// ordered fallback chain: every path that PARSES becomes a chain member (order
// preserved, deduplicated), so glyph lookups fall through to the next face.
// requirable is the coverage the FIRST member must have — a chain whose metric
// font cannot draw Latin text is refused outright, which is the exact failure
// the reported bug shipped silently.
func loadChainFromPaths(paths []string, requirable []rune) (fontChain, error) {
	seen := map[string]bool{}
	var chain fontChain
	var firstErr error
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		f, err := loadFontFile(p)
		if err != nil {
			if firstErr == nil && !os.IsNotExist(err) {
				firstErr = fmt.Errorf("%s: %w", p, err)
			}
			continue
		}
		chain = append(chain, f)
	}
	if len(chain) == 0 {
		msg := fmt.Sprintf("未找到可用字体: 字体链为空, 已尝试 %d 个路径", len(paths))
		if firstErr != nil {
			msg = fmt.Sprintf("%s (首个错误: %v)", msg, firstErr)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	// A nil requirable means "the caller pinned the metric font on purpose" —
	// honour it (the pin's warning is the loud part) and let the chain's
	// fallbacks supply the glyphs the pinned face lacks.
	if requirable != nil && !faceHas(chain[0], requirable) {
		return nil, fmt.Errorf("字体链首字体 %s 缺少 %q 的字形 — 布局度量不可信, 拒绝渲染(这会画出空白图)", chain[0].path, string(requirable))
	}
	return chain, nil
}

// loadFontFile reads and parses one font file, stamping its path onto the
// parsed face so diagnostics can name every member of a chain.
func loadFontFile(path string) (*sfnt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := parseTTF(data, 0)
	if err != nil {
		return nil, err
	}
	f.path = path
	return f, nil
}

// findFont keeps the historical single-path query (a font the caller pinned, or
// the chain's metric font) for callers that only need the layout face.
func findFont(custom string) (string, error) {
	chain, err := loadFontChain(custom)
	if err != nil {
		return "", err
	}
	return chain[0].path, nil
}

// fontDirs is the fixed system font search path (font discovery is a
// filesystem fact; no Unicode→coverage table lives in this repo).
func fontDirs() []string {
	return []string{
		"/usr/share/fonts",
		"/usr/local/share/fonts",
	}
}

// latinFontNames / wordspaceFontNames / cjkFontNames are the candidate file
// names tried under each search directory, in priority order. The first
// candidate that PARSES and covers what is needed wins — a face is verified by
// probing its cmap, never by trusting its name.
var (
	latinFontNames = []string{
		"truetype/dejavu/DejaVuSans.ttf",
		"truetype/liberation/LiberationSans-Regular.ttf",
		"truetype/noto/NotoSans-Regular.ttf",
		"truetype/noto/NotoSansMono-Regular.ttf",
		"truetype/freefont/FreeSans.ttf",
		"truetype/droid/DroidSans.ttf",
	}
	// preferredFontNames are faces covering BOTH Latin and CJK; when one exists
	// it becomes the metric font, which keeps the layout identical to the
	// validated single-face behaviour (the IoU-99.5% reference was measured on
	// wqy-zenhei). On a host without them the Latin faces above take over.
	preferredFontNames = []string{
		"truetype/wqy/wqy-zenhei.ttc",
		"truetype/wqy/wqy-microhei.ttc",
	}
	// wordspaceFontNames drives the wordspace advance a word-wrapping bbox
	// needs. wqy-zenhei's advance(space) is its Latin advance (the value the
	// layout fixture pins); DejaVu is the generic fallback.
	wordspaceFontNames = []string{
		"truetype/wqy/wqy-zenhei.ttc",
		"truetype/wqy/wqy-microhei.ttc",
		"truetype/dejavu/DejaVuSans.ttf",
	}
	cjkFontNames = []string{
		"truetype/wqy/wqy-zenhei.ttc",
		"truetype/wqy/wqy-microhei.ttc",
		"truetype/droid/DroidSansFallbackFull.ttf",
		"truetype/droid/DroidSansFallback.ttf",
		"truetype/noto/NotoSansCJK-Regular.ttc", // CFF: 解析会失败并被跳过
	}
)

// fontSearchPaths expands candidate file names under each directory that
// exists. The list is deterministic (fixed names, fixed directory order) so a
// render is reproducible on a given host.
func fontSearchPaths(names, dirs []string) []string {
	var out []string
	for _, d := range dirs {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			continue
		}
		for _, n := range names {
			out = append(out, filepath.Join(d, n))
		}
	}
	return out
}

// missingGlyphs counts the DISTINCT runes in text that no font in the chain can
// map to a glyph — the measured "字符画不出来" fact the summary reports.
func missingGlyphs(text string, chain fontChain) int {
	seen := map[rune]bool{}
	for _, r := range text {
		if r == '\n' || r == '\r' || unicode.IsSpace(r) || seen[r] {
			continue
		}
		seen[r] = true
	}
	n := 0
	for r := range seen {
		ok := false
		for _, f := range chain {
			if f.glyph(r) != 0 {
				ok = true
				break
			}
		}
		if !ok {
			n++
		}
	}
	return n
}
