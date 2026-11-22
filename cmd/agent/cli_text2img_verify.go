package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/text2img"
)

// cli_text2img_verify.go — the `gopi tool text2img-verify` verb: render `知` through
// internal/text2img at a given size, print the font used, and write the PNG for
// pixel-level IoU verification.
//
// IT WAS `cmd/text2img-verify`, A SECOND BINARY, UNTIL 2026-10-02. It exists to
// prove the renderer on the SMALLEST sample (PROJECT.md Rule 10.4: the z.txt
// fixture, never "looks right"), and it was built separately for no reason the
// program ever stated — it imports internal/text2img and internal/scratch, both
// of which cmd/agent already links.
//
// Output goes to the GoPi scratch home (internal/scratch), never /tmp: /tmp is
// on the `/` partition whose 2026-09-11 exhaustion is why the repo bans it as a
// test/dev workspace — see devdoc/tmp-ban-scratch-root.md.
// GOPI_T2I_VERIFY_DIR overrides the output directory for a one-off check.

const (
	// text2imgVerifyInputName is the sample text file the renderer reads (the
	// z.txt fixture the case-study used).
	text2imgVerifyInputName = "z.txt"
	// text2imgVerifyDefaultSize is the render size when no argument is given.
	text2imgVerifyDefaultSize = 200
	// text2imgVerifySample is the sample's content, printed in the refusal that
	// tells a caller how to create it.
	text2imgVerifySample = "知知知"
	// text2imgVerifyOutPrefix / text2imgVerifyOutSuffix build the PNG's name.
	text2imgVerifyOutPrefix = "z_"
	text2imgVerifyOutSuffix = "px.png"
	// text2imgVerifyDirEnv overrides the output directory.
	text2imgVerifyDirEnv = "GOPI_T2I_VERIFY_DIR"
	// text2imgVerifyUsage is the whole interface: an optional size in px.
	text2imgVerifyUsage = "usage: gopi tool text2img-verify [size-px]\n" +
		"  renders the z.txt sample from the scratch dir (or $GOPI_T2I_VERIFY_DIR) at size-px (default 200),\n" +
		"  prints font/size/pages/chars, and writes z_<size>px.png for pixel-level (IoU) verification."
	// text2imgVerifyExitUsage / text2imgVerifyExitError are the verb's two
	// failure statuses: a misuse (2) and a run that could not render (1).
	text2imgVerifyExitUsage = 2
	text2imgVerifyExitError = 1
)

// isText2imgVerifySubcommand is the argv predicate; like every other subcommand
// it consumes the rest of argv and handles its own exit.
func isText2imgVerifySubcommand(args []string) bool {
	if len(args) == 0 || args[0] != CliVerbText2imgVerify {
		return false
	}
	os.Exit(runText2imgVerifySubcommand(args[1:]))
	return true
}

func runText2imgVerifySubcommand(args []string) int {
	size := text2imgVerifyDefaultSize
	if len(args) > 0 {
		// -h is a HELP request, not a size: print the interface and exit 0.
		if args[0] == "-h" || args[0] == "--help" {
			fmt.Println(text2imgVerifyUsage)
			return 0
		}
		n, err := strconv.Atoi(args[0])
		if err != nil || n <= 0 {
			fmt.Fprintf(os.Stderr, "usage: gopi tool text2img-verify [size-px]\n")
			return text2imgVerifyExitUsage
		}
		size = n
	}
	dir := os.Getenv(text2imgVerifyDirEnv)
	if dir == "" {
		dir = scratch.DumpDir(scratch.RuntimeClassText2Img)
	}
	if err := scratch.MkdirAllWithSuiteRunOwnerPerm(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "scratch dir:", err)
		return text2imgVerifyExitError
	}
	src := filepath.Join(dir, text2imgVerifyInputName)
	if _, err := os.Stat(src); err != nil {
		fmt.Fprintf(os.Stderr, "sample %s missing: %v\ncreate it with: printf '%s\\n' > %s\n",
			src, err, text2imgVerifySample, src)
		return text2imgVerifyExitError
	}
	out := filepath.Join(dir, fmt.Sprintf("%s%d%s", text2imgVerifyOutPrefix, size, text2imgVerifyOutSuffix))
	res, err := text2img.RenderFile(src, out, text2img.Options{Size: size})
	if err != nil {
		fmt.Fprintln(os.Stderr, "render failed:", err)
		return text2imgVerifyExitError
	}
	fmt.Printf("font=%s size=%d pages=%d chars=%d out=%v\n", res.Font, res.Size, res.Pages, res.Chars, res.Outputs)
	return 0
}
