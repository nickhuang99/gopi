// filteredclaim-census.go — the FALSE-POSITIVE / COVERAGE CENSUS for the
// filtered-package claim predicate (Rule 38: a census figure names its
// generator and the commit it was measured at).
//
// It reads the reports this machine has ACTUALLY written — every
// `task-report.md` (and the committed `tasks/*-report.md` corpus) — through the
// SAME predicate the seal uses (internal/taskreport.FilteredClaimsIn), and
// prints, per corpus and in total:
//
//   - reports scanned, and reports the predicate fires on;
//   - the sentences it fires on, so a reviewer can judge the HONEST OVERLAP by
//     reading them rather than trusting a count.
//
// Why it exists: the record this predicate adds is a RECORD, not a refusal, and
// that choice rests on a measured claim — that the honest branch-scoped shape
// (`go test -run 'X|Y' ./pkg/` reported as a TEST-level reading) must not be
// touched, and that the defect shape is narrow. A number in a report that
// nobody can reproduce is a rumour with a number attached (Rule 38); this file
// is the reproduction.
//
// Usage (READ-ONLY — it writes nothing, anywhere):
//
//	go run internal/taskreport/testdata/filteredclaim-census.go \
//	    ~/.gopi/sessions ./tasks ./.git
//
// The last argument (if it names a git directory) is resolved to the commit the
// census was measured at, so the figure names its rev.
//
// It lives under testdata/ so the go tool ignores it (no test, no vet, no
// build of the module — the repo-hygiene-census.go precedent).

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nick/gopi/internal/taskreport"
)

// reportName is the report file the census reads: taskreport.FileName's value,
// resolved through the one definition rather than re-spelled here (Rules 1b/13).
var reportName = taskreport.FileName

// reportGlobSuffix is the committed-corpus spelling (`tasks/<id>-report.md`).
const reportGlobSuffix = "-report.md"

// excerptMaxRunes bounds a printed sentence, so the census stays readable.
const excerptMaxRunes = 160

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: go run filteredclaim-census.go <dir|git-dir>...\n")
		os.Exit(2)
	}
	total, hits, files := 0, 0, 0
	var fired []string
	var rawSentences []string
	gitWorkDir := ""
	for _, arg := range os.Args[1:] {
		info, err := os.Stat(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", arg, err)
			continue
		}
		// The git argument is checked BEFORE the directory test: in a LINKED
		// WORKTREE `.git` is a FILE holding "gitdir: …", not a directory, and an
		// IsDir guard here silently dropped it — the census printed no
		// `measured at commit` line at all (Rule 15: a scan that found nothing
		// must be distinguishable from one that never ran; Rule 38: a census
		// figure names the commit it was measured at).
		if filepath.Base(arg) == ".git" {
			gitWorkDir = filepath.Dir(arg)
			continue
		}
		if !info.IsDir() {
			fmt.Fprintf(os.Stderr, "skip %s: not a directory and not .git\n", arg)
			continue
		}
		paths := []string{}
		walkErr := filepath.WalkDir(arg, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			base := filepath.Base(p)
			if base == reportName || (strings.HasPrefix(base, "01") && strings.HasSuffix(base, reportGlobSuffix)) {
				paths = append(paths, p)
			}
			return nil
		})
		if walkErr != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", arg, walkErr)
			continue
		}
		sort.Strings(paths)
		corpus, corpusHits, corpusFired, corpusRaw := scan(paths)
		total += corpus
		hits += corpusHits
		files += len(paths)
		fired = append(fired, corpusFired...)
		rawSentences = append(rawSentences, corpusRaw...)
		fmt.Printf("corpus %s: reports=%d fired=%d\n", arg, corpus, corpusHits)
	}
	fmt.Printf("total: files=%d reports=%d fired=%d\n", files, total, hits)
	if len(fired) > 0 {
		// The distinct count is PRINTED, never tallied by hand: a figure a
		// reader acts on names its generator (Rule 38), and the hand-tallied
		// version of this number was wrong once already (445 against the 585
		// the same lines yield when deduplicated).
		fmt.Printf("\ndistinct fired sentences: %d (over %d occurrences)\n",
			len(distinctSentences(rawSentences)), len(rawSentences))
		fmt.Printf("\nthe sentences the predicate fires on (%d):\n", len(fired))
		sort.Strings(fired)
		for _, s := range fired {
			fmt.Printf("  - %s\n", s)
		}
	}
	if gitWorkDir != "" {
		// `git -C <worktree>` resolves HEAD in a linked worktree (where .git is
		// a file) AND in a main checkout — one form for both, so the figure's
		// rev is printed rather than silently omitted.
		out, err := exec.Command("git", "-C", gitWorkDir, "rev-parse", "HEAD").Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not resolve HEAD in %s: %v\n", gitWorkDir, err)
			os.Exit(1)
		}
		fmt.Printf("\nmeasured at commit: %s\n", strings.TrimSpace(string(out)))
	}
}

// scan reads every path through the production predicate and returns the report
// count, the count it fired on, the printed fired sentences (bounded and
// path-annotated), and the RAW sentence texts the distinct count is over.
func scan(paths []string) (reports, fired int, sentences, raw []string) {
	for _, p := range paths {
		body, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		reports++
		v := taskreport.FilteredClaimsIn(string(body))
		if len(v.Claims) == 0 {
			continue
		}
		fired++
		for _, c := range v.Claims {
			raw = append(raw, c.Sentence)
			sentences = append(sentences, excerpt(c.Sentence)+"   ["+p+"]")
		}
	}
	return reports, fired, sentences, raw
}

// distinctSentences counts the distinct spellings among the fired sentences —
// the number a reader uses to judge the honest overlap (585 of them on the
// corpus this was measured over, where the hand-tallied figure said 445).
func distinctSentences(sentences []string) []string {
	seen := make(map[string]struct{}, len(sentences))
	out := make([]string, 0, len(sentences))
	for _, s := range sentences {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// excerpt bounds a sentence for printing.
func excerpt(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= excerptMaxRunes {
		return string(r)
	}
	return string(r[:excerptMaxRunes]) + "…"
}
