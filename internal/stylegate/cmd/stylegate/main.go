// Command stylegate is the CLI front end of the internal/stylegate scanner —
// the reproducible before/after evidence source for the style sweep.
//
// Usage:
//
//	go run ./internal/stylegate/cmd/stylegate                  # scan, counts only
//	go run ./internal/stylegate/cmd/stylegate -list            # every finding
//	go run ./internal/stylegate/cmd/stylegate -json -o r.json  # report for -fix
//	go run ./internal/stylegate/cmd/stylegate -fix r.json      # apply byte-exact fixes
//	go run ./internal/stylegate/cmd/stylegate -rule errorf-verb -tests=false
//
// -fix applies ONLY the byte-exact mechanical edits the scanner located with
// the parser (currently %v→%w), then re-parses each file and refuses to write
// a file that no longer parses. Everything else is reported for a human.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/nickhuang99/gopi/internal/stylegate"
)

func main() {
	root := flag.String("root", ".", "module root to scan")
	rule := flag.String("rule", "", "only report/apply this rule")
	tests := flag.Bool("tests", true, "include _test.go files for errorf-verb / ignored-error")
	all := flag.Bool("list", false, "list every finding (default: counts only)")
	attribute := flag.Bool("attribute", false,
		"with -list: blame every finding to its introducing commit and mark it inherited (already in --since's history) or own (arrived after it)")
	since := flag.String("since", "",
		"the cut point the ownership question is asked against (required by -attribute; e.g. the revision this ground was cut at)")
	asJSON := flag.Bool("json", false, "emit JSON")
	out := flag.String("o", "", "write the JSON report to this path")
	exclude := flag.String("exclude", "", "skip files whose repo-relative path contains this substring")
	table := flag.String("annotate", "", "apply reason comments from this per-SITE JSON table (keys are `file.go:LINE`) to the report given by -fix; a callee-wide or whole-file key is REFUSED")
	fixReport := flag.String("fix", "", "apply the mechanical edits carried by this JSON report")
	flag.Parse()

	if *table != "" {
		n, err := annotate(*root, *fixReport, *table, *rule)
		if err != nil {
			fmt.Fprintln(os.Stderr, "stylegate -annotate:", err)
			os.Exit(2)
		}
		fmt.Printf("annotated %d site(s)\n", n)
		return
	}
	if *fixReport != "" {
		n, err := applyFixes(*root, *fixReport, *rule)
		if err != nil {
			fmt.Fprintln(os.Stderr, "stylegate -fix:", err)
			os.Exit(2)
		}
		fmt.Printf("applied %d mechanical edit(s)\n", n)
		return
	}

	rep, err := stylegate.Scan(stylegate.Options{Root: *root, IncludeTests: *tests, Exclude: *exclude})
	if err != nil {
		fmt.Fprintln(os.Stderr, "stylegate:", err)
		os.Exit(2)
	}
	if *out != "" {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, "stylegate: encode:", err)
			os.Exit(2)
		}
		if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "stylegate: write report:", err)
			os.Exit(2)
		}
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(os.Stderr, "stylegate: encode:", err)
			os.Exit(2)
		}
		return
	}

	byRule := map[string][]stylegate.Finding{}
	for _, f := range rep.Findings {
		byRule[f.Rule] = append(byRule[f.Rule], f)
	}
	if *rule != "" {
		byRule = map[string][]stylegate.Finding{*rule: byRule[*rule]}
	}
	rules := make([]string, 0, len(byRule))
	for r := range byRule {
		rules = append(rules, r)
	}
	sort.Strings(rules)

	total := 0
	for _, r := range rules {
		fmt.Printf("%-16s %5d\n", r, len(byRule[r]))
		total += len(byRule[r])
	}
	fmt.Printf("%-16s %5d\n", "TOTAL", total)
	fmt.Printf("%-16s %5d  (not decidable statically — reported, never guessed)\n", "undecidable", rep.Undecidable)
	fmt.Printf("%-16s %5d  (deferred closes — informational, not findings)\n", "defer-close", rep.DeferredCloses)
	if len(rep.PackagesFailed) > 0 {
		fmt.Printf("%-16s %d  %v\n", "pkgs-failed", len(rep.PackagesFailed), rep.PackagesFailed)
	}
	if *all {
		fmt.Println()
		for _, r := range rules {
			fs := byRule[r]
			sort.Slice(fs, func(i, j int) bool {
				if fs[i].File != fs[j].File {
					return fs[i].File < fs[j].File
				}
				return fs[i].Line < fs[j].Line
			})
			for _, f := range fs {
				fmt.Println(f.String())
			}
		}
	}
	if *attribute {
		printAttribution(*root, *since, *rule, rep.Findings)
	}
}

// printAttribution emits the composition reading: every finding blamed to its
// introducing commit and classified against the cut point. It is the answer to
// "N != ceiling — which sites are the difference, and were they mine?" that a
// bare count cannot give.
//
// An empty cut point is a REFUSAL, not a degraded listing: see
// stylegate.ErrAttributeNoCutPoint.
func printAttribution(root, since, rule string, findings []stylegate.Finding) {
	if rule != "" {
		kept := make([]stylegate.Finding, 0, len(findings))
		for _, f := range findings {
			if f.Rule == rule {
				kept = append(kept, f)
			}
		}
		findings = kept
	}
	comp, err := stylegate.Attribute(stylegate.Options{Root: root, IncludeTests: true}, findings, since)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stylegate -attribute:", err)
		os.Exit(2)
	}
	fmt.Println()
	fmt.Printf("%-16s %d site(s) attributed against %s\n", "ATTRIBUTION", len(comp.Sites), since)
	fmt.Printf("%-16s %d inherited, %d own, %d unknown\n", "", comp.Inherited, comp.Own, comp.Unknown)
	files := make([]string, 0, len(comp.ByFile))
	for f := range comp.ByFile {
		files = append(files, f)
	}
	sort.Strings(files)
	fmt.Printf("%-16s %d file(s)\n", "BY FILE", len(files))
	for _, f := range files {
		fmt.Printf("  %4d  %s\n", comp.ByFile[f], f)
	}
	if comp.Unknown > 0 {
		fmt.Printf("%-16s %d site(s) could not be decided — listed as 'unknown', never guessed\n",
			"UNDECIDED", comp.Unknown)
	}
	fmt.Printf("%-16s\n", "SITES")
	for _, s := range comp.Sites {
		fmt.Println("  " + s.String())
	}
}
