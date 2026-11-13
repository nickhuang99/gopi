package agent

// text2img tool tests: schema (src required, out/size/cols/lh/lines/margin/
// font optional), validateToolArgs, and the options conversion (JSON numbers
// → int options).

import (
	"strings"
	"testing"
)

// TestText2imgToolSchema: src required, everything else optional; the
// description teaches when/how to use (文本变图片 / 零依赖 / CJK) and the
// validated defaults.
func TestText2imgToolSchema(t *testing.T) {
	td := toolDefByName(t, "text2img")
	if td == nil {
		t.Fatal("text2img missing from claudeToolDefs")
	}
	props := schemaProps(t, td)
	for _, key := range []string{"src", "out", "size", "cols", "lh", "lines", "margin", "font"} {
		if _, ok := props[key]; !ok {
			t.Errorf("text2img schema missing parameter %s", key)
		}
	}
	if req := schemaRequired(t, td); len(req) != 1 || req[0] != "src" {
		t.Fatalf("text2img required = %v, want [src] (out/size/cols/... stay optional)", req)
	}
	for _, want := range []string{
		"text→PNG", "零依赖", "CJK", "TrueType",
		"auto-paginates", "out-1.png",
	} {
		if !strings.Contains(td.Description, want) {
			t.Errorf("text2img description missing %q", want)
		}
	}
}

// TestText2imgValidateToolArgs: src enforced.
func TestText2imgValidateToolArgs(t *testing.T) {
	if msg := validateToolArgs("text2img", map[string]interface{}{}); msg != "Missing required parameter: src" {
		t.Fatalf("empty args msg = %q, want src error", msg)
	}
	if msg := validateToolArgs("text2img", map[string]interface{}{"src": "  "}); msg != "Missing required parameter: src" {
		t.Fatalf("blank src msg = %q, want src error", msg)
	}
	if msg := validateToolArgs("text2img", map[string]interface{}{"src": "/tmp/a.txt"}); msg != "" {
		t.Fatalf("valid args msg = %q, want empty", msg)
	}
}

// TestText2imgOptionsFromArgs: JSON numbers (float64) become int options;
// missing values stay 0 so the renderer defaults apply.
func TestText2imgOptionsFromArgs(t *testing.T) {
	o := text2imgOptionsFromArgs(map[string]interface{}{
		"src": "/tmp/a.txt", "size": float64(28), "cols": float64(60),
		"lh": float64(36), "lines": float64(60), "margin": float64(20),
		"font": "/tmp/f.ttf",
	})
	if o.Size != 28 || o.Cols != 60 || o.Lh != 36 || o.Lines != 60 || o.Margin != 20 {
		t.Errorf("options wrong: %+v", o)
	}
	if o.Font != "/tmp/f.ttf" {
		t.Errorf("font = %q, want /tmp/f.ttf", o.Font)
	}
	zero := text2imgOptionsFromArgs(map[string]interface{}{"src": "/tmp/a.txt"})
	if zero.Size != 0 || zero.Cols != 0 || zero.Lines != 0 {
		t.Errorf("missing args should stay 0 (defaults apply in renderer): %+v", zero)
	}
}
