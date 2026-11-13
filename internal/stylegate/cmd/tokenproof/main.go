// Command tokenproof proves that a set of edits changed NO code: it compares
// the working copy of each file with its HEAD version as a token stream with
// comments stripped. Identical token streams mean the program the compiler
// sees is unchanged — the strongest available evidence for "no runtime
// behaviour change" on a comment-only batch.
//
// Usage:
//
//	tokenproof <file> [file...]        # exits 1 if any file's tokens differ
package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"strings"
)

// tokenStream returns every token of src except comments, as "POS:TEXT" items
// (positions omitted — only the token sequence matters).
func tokenStream(src []byte, name string) ([]string, error) {
	fset := token.NewFileSet()
	f := fset.AddFile(name, fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(f, src, func(pos token.Position, msg string) {}, 0) // 0 = skip comments
	var out []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return out, nil
		}
		out = append(out, tok.String()+":"+lit)
	}
}

// headVersion returns the committed content of rel at HEAD.
func headVersion(rel string) ([]byte, error) {
	cmd := exec.Command("git", "show", "HEAD:"+rel)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show HEAD:%s: %w (%s)", rel, err, errBuf.String())
	}
	return out, nil
}

func main() {
	files := os.Args[1:]
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tokenproof <file> [file...]")
		os.Exit(2)
	}
	if n := proveFiles(files); n > 0 {
		fmt.Printf("%d file(s) changed their token stream — the batch is NOT comment-only\n", n)
		os.Exit(1)
	}
	fmt.Printf("OK: %d file(s) — identical token streams (comments stripped)\n", len(files))
}

func proveFiles(files []string) int {
	failed := 0
	for _, rel := range files {
		old, err := headVersion(rel)
		if err != nil {
			fmt.Println("ERROR", err)
			failed++
			continue
		}
		cur, err := os.ReadFile(rel)
		if err != nil {
			fmt.Println("ERROR", err)
			failed++
			continue
		}
		a, err := tokenStream(old, "old/"+rel)
		if err != nil {
			fmt.Println("ERROR", err)
			failed++
			continue
		}
		b, err := tokenStream(cur, "new/"+rel)
		if err != nil {
			fmt.Println("ERROR", err)
			failed++
			continue
		}
		if len(a) != len(b) {
			fmt.Printf("DIFFER %s: %d tokens at HEAD vs %d now\n", rel, len(a), len(b))
			failed++
			continue
		}
		for i := range a {
			if a[i] != b[i] {
				fmt.Printf("DIFFER %s: token %d is %q at HEAD vs %q now\n", rel, i, a[i], b[i])
				failed++
				break
			}
		}
	}
	return failed
}
