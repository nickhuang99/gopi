package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// visionUsage is the help text for the vision_analyze CLI subcommand. It is
// printed when the subcommand is missing required arguments, passes an invalid
// flag, or the user passes -h/--help.
const visionUsage = `GoPi vision_analyze subcommand — expose the vision_analyze tool as a CLI command.
Behavior is identical to the tool_use call (same function path — it reads the
local image, base64-encodes it inline and sends ONE lightweight standalone
request to the vision model; the image bytes never enter the main context,
only the model's understanding is returned).

Usage:
  gopi tool vision_analyze <image_path> [--prompt "text"]
      Analyze a local image and print the model's understanding (description /
      OCR text) plus the per-image token cost.

  gopi tool vision_analyze screenshot.png
      Default prompt: 描述这张图片，并识别其中的文字（如有）
  gopi tool vision_analyze chart.png --prompt "describe the chart data"
  gopi tool vision_analyze photo.png --prompt "识别图片中的所有中文文字"

Flags:
  --prompt TEXT   Optional instruction for the vision model. Default:
                  描述这张图片，并识别其中的文字（如有）

Supported formats: jpg, jpeg, png, gif, webp (max 32 MiB).
Model:    GOPI_VISION_MODEL    (default deepseek-v4-flash-vision-exp)
Endpoint: GOPI_VISION_BASE_URL (default https://api.deepseek.com/anthropic)
`

// isVisionSubcommand reports whether argv begins with the vision_analyze
// subcommand. When it does it runs the subcommand and returns true (the caller
// must not treat the remaining args as an input file).
func isVisionSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == toolnames.ToolVisionAnalyze {
		runVisionSubcommand(args[1:])
		return true
	}
	return false
}

// visionOptions is the parsed result of a vision_analyze subcommand invocation.
type visionOptions struct {
	imagePath string
	prompt    string
}

// parseVisionArgs parses args for the vision_analyze subcommand without
// touching the process (no os.Exit, no env mutation). It returns the parsed
// options or a usage-style error. Separate from dispatch so the parsing is
// unit-testable.
func parseVisionArgs(args []string) (*visionOptions, error) {
	fs := newMemoryFlagSet() // same hand-rolled `--name value` parser
	positionals, err := fs.parse(args)
	if err != nil {
		return nil, err
	}
	for _, fname := range fs.order {
		if !visionFlagMember(fname) {
			return nil, fmt.Errorf("unknown flag --%s", fname)
		}
	}

	if len(positionals) == 0 {
		return nil, fmt.Errorf("missing <image_path> argument")
	}

	opts := &visionOptions{
		imagePath: strings.TrimSpace(positionals[0]),
		prompt:    strings.TrimSpace(fs.get("prompt")),
	}

	// Image path is required (empty-string positional counts as missing).
	if opts.imagePath == "" {
		return nil, fmt.Errorf("missing <image_path> argument")
	}

	// At most one positional argument.
	if len(positionals) > 1 {
		return nil, fmt.Errorf("unexpected extra argument %q", positionals[1])
	}

	return opts, nil
}

// visionFlagMember reports whether a flag name is valid for the vision_analyze
// subcommand.
func visionFlagMember(flag string) bool {
	switch flag {
	case "prompt":
		return true
	}
	return false
}

// runVisionSubcommand parses the subcommand's own flags and dispatches to the
// shared tools.VisionAnalyze function so CLI behavior is identical to
// tool_use. It exits the process with a non-zero status on argument/usage
// errors and on vision analysis failures.
func runVisionSubcommand(args []string) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(visionUsage)
			os.Exit(0)
		}
	}

	opts, err := parseVisionArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool vision_analyze: %v\n%s", err, visionUsage)
		os.Exit(1)
	}

	out, err := tools.VisionAnalyze(opts.imagePath, opts.prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi tool vision_analyze: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out)
}
