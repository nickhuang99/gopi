// The two USER-facing input shapes of `gopi tool tokens`: --file and --session.
//
// Both exist because the tool's first two shapes were its own verification
// inputs. `--raw <raw.log>` and `--body <request.json>` answer "did OUR template
// render reproduce the server's number", which is a question about this repo.
// A person holding an ordinary text file asks a different question ("how many
// tokens is this?") and got a usage refusal instead — measured, and the reason
// this file exists.
package main

import (
	"fmt"
	"os"

	"github.com/nickhuang99/gopi/internal/gopilog"
	"github.com/nickhuang99/gopi/internal/tokenizer"
)

// tokensFileReportFmt is the one line `--file` prints. It names the PATH it
// read, so a count is never an orphan number (Rule 15: the answer says which
// input it is about), and carries the byte/line size beside the count because
// tokens-per-byte is what makes a surprising number checkable.
const tokensFileReportFmt = "%s: %d tokens (%d bytes, %d lines, %s)\n"

// tokensSessionHeadFmt names the session a verification report is about, and
// the raw log the numbers were read from — the two paths a reader needs to
// re-run the command by hand.
const tokensSessionHeadFmt = "session %s\nraw log %s\n\n"

// tokensSessionNoRequests says the session has a raw log but no request in it:
// a real answer for a run that logged its start and nothing else, and it must
// not read like the empty table printHeader would produce.
const tokensSessionNoRequests = "no request records in this session's raw log"

// runFile counts one ordinary text file through the embedded DeepSeek
// tokenizer's own encoder — the same `Count` the agent's context readouts use,
// so the number a person sees here is the number the model would be charged.
//
// An unreadable path is a FAILURE, never a zero: a count of 0 reads as a
// measurement, and this tool exists to report measurements (Rule 15).
func runFile(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gopi tool tokens: cannot read file "+path+": "+err.Error())
		return tokensExitError
	}
	n, err := tokenizer.CountDefault(string(raw))
	if err != nil {
		fmt.Fprintln(os.Stderr, "gopi tool tokens: tokenizer: "+err.Error())
		return tokensExitError
	}
	lines := 0
	if len(raw) > 0 {
		lines = 1
		for _, b := range raw {
			if b == '\n' {
				lines++
			}
		}
	}
	fmt.Printf(tokensFileReportFmt, path, n, len(raw), lines, tokenizerName())
	return 0
}

// tokensVocabName is the vocabulary a count came from. It is a constant rather
// than an inline string (Rule 1) and it names the package's own claim — the
// embedded data in internal/tokenizer is the DeepSeek-V4.1 BPE vocabulary
// (`internal/tokenizer/tokenizer.go` line 1: "counts DeepSeek-V4.1 BPE tokens
// locally"). A count from another vocabulary is another number, so the report
// says which one answered.
const tokensVocabName = "deepseek-v4.1-bpe"

// tokenizerName is the vocabulary a count came from, printed so a reader knows
// WHICH tokenizer answered (a number from the wrong vocabulary is a different
// number, and the page must not leave that implicit).
func tokenizerName() string { return tokensVocabName }

// runSession prints the verification results a session ALREADY holds: for each
// request in its raw HTTP log, what this repo's template rendered (ours) beside
// what the server reported for those same bytes (server), paired by position
// inside one file.
//
// Nothing is sent anywhere: `--session` reads the log the run already wrote.
// That is the difference from `--body --compare`, which asks the endpoint.
func runSession(id string) int {
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gopi tool tokens: cannot resolve the sessions directory: "+err.Error())
		return tokensExitError
	}
	path, err := gopilog.FindSession(dir, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gopi tool tokens: session "+id+": "+err.Error())
		return tokensExitError
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "gopi tool tokens: no session with id "+id+" under "+dir)
		return tokensExitError
	}
	rawLog, err := gopilog.ResolveRawLogForSession(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gopi tool tokens: session "+id+" has no raw HTTP log: "+err.Error())
		return tokensExitError
	}
	fmt.Printf(tokensSessionHeadFmt, id, rawLog)
	reqs, perr := pairRawLog(rawLog)
	if perr != nil {
		// Partial results are still results (Rule 15): print the rows that
		// paired and say why the rest did not.
		fmt.Fprintln(os.Stderr, "gopi tool tokens: "+perr.Error())
	}
	if len(reqs) == 0 {
		fmt.Println(tokensSessionNoRequests)
		if perr != nil {
			return tokensExitError
		}
		return 0
	}
	printHeader()
	var matched, exact int
	for _, r := range reqs {
		if r.server > 0 {
			matched++
			if r.res.ours == r.server {
				exact++
			}
		}
		printRow(r.res)
	}
	fmt.Printf("\nrequests: %d, with a server number: %d, exactly equal: %d\n",
		len(reqs), matched, exact)
	if perr != nil {
		return tokensExitError
	}
	return 0
}
