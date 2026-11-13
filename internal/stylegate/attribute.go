package stylegate

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// attribute.go supplies the COMPOSITION reading behind a global-count gate.
//
// A ceiling gate reports one number ("ignored-error: 68 test hit(s), ceiling
// 66") and a number cannot answer the only question the reader has: WHICH sites
// are the two, and were they already in the tree when my run started? Without
// that question answered, a run reads its own ground's number, compares it with
// a ceiling that was measured somewhere else, and concludes the wrong thing —
// measured cost, row 01a1a629: a ground read 68 against 66 and reported "the
// two parallel rows still do not land green", while the merged state was 66 and
// green.
//
// This reading answers it in the two halves the count cannot express:
//
//  1. WHO — every finding is blamed to the commit that introduced the line, so
//     the delta is attributed to a file AND a commit, never to a guess.
//  2. WHEN — that commit is asked whether it already belongs to the history of
//     the cut point the caller names. An ancestor is INHERITED (it was there
//     before this run began; the excess is not this run's to pay for). A
//     non-ancestor is OWN (it arrived after the cut point).
//
// Everything here is READ-ONLY over the checkout: three git verbs, none of
// which writes a ref, an index entry or a file.

// Named constants for every value this file compares on or hands to a
// subprocess (Rule 1/13/14: a value carrying meaning is never typed inline).
const (
	// gitBin is the VCS binary the reading shells out to. There is no
	// in-process alternative: this module takes no VCS library dependency, and
	// Rule 35 keeps it that way.
	gitBin = "git"
	// the three read-only git surfaces the reading uses.
	gitBlameVerb     = "blame"
	gitMergeBaseVerb = "merge-base"
	gitShowVerb      = "show"
	// gitLinePorcelainFlag asks blame for one record per line with metadata in
	// front of each content line. That is what makes the parse unambiguous: a
	// content line is TAB-prefixed and a record header never is.
	gitLinePorcelainFlag = "--line-porcelain"
	// gitIsAncestorFlag turns merge-base into the single question this reading
	// asks: does <commit> already belong to <since>'s history?
	gitIsAncestorFlag = "--is-ancestor"
	// gitSubjectFlag prints a commit's subject line alone.
	gitSubjectFlag = "--format=%s"
	// gitNoPatchFlag suppresses the diff `git show` would otherwise print.
	gitNoPatchFlag = "-s"
	// gitPathSeparator separates the verb's flags from the path arguments.
	gitPathSeparator = "--"
	// fullSHAHexLen is the length of an unabbreviated object name, which is what
	// a blame record header carries.
	fullSHAHexLen = 40
	// gitExitAncestor / gitExitNotAncestor are merge-base --is-ancestor's own
	// answers. Any other status is an error and is reported as one (Rule 15).
	gitExitAncestor    = 0
	gitExitNotAncestor = 1
)

// The three verdicts a site can carry. They are the vocabulary the report
// prints, so they are constants rather than literals at the printing site.
const (
	// AttribInherited means the introducing commit already belongs to the cut
	// point's history: the site was in the tree before this run began.
	AttribInherited = "inherited"
	// AttribOwn means the introducing commit does NOT belong to the cut point's
	// history: the site arrived after it.
	AttribOwn = "own"
	// AttribUnknown means the reading could not decide (the file is untracked,
	// the commit is not an object here, git refused). It is never silently
	// folded into either other verdict.
	AttribUnknown = "unknown"
)

// ErrAttributeNoCutPoint is returned when no cut point is named. A composition
// reading without a cut point can say WHO but not WHEN, which is exactly the
// half that avoids the mistaken conclusion — so it refuses instead of degrading.
var ErrAttributeNoCutPoint = errors.New(
	"stylegate: attribution needs the cut point this run started from (--since <rev>): " +
		"without it every site is 'unknown' and the inherited/own question is unanswered")

// Site is one finding together with what the reading learned about it.
type Site struct {
	Finding Finding
	// Commit is the full object name blame attributes the offending line to,
	// empty when blame could not be read.
	Commit string
	// Subject is that commit's subject line, empty when unavailable.
	Subject string
	// Verdict is AttribInherited, AttribOwn or AttribUnknown.
	Verdict string
	// Why explains an AttribUnknown verdict in the reading's own words; empty
	// for the two decided verdicts.
	Why string
}

// Composition is the whole reading: every site, plus the counts that make the
// delta arithmetic legible.
type Composition struct {
	// Root is the module root the findings' paths are relative to.
	Root string
	// Since is the cut point the inherited/own question was asked against.
	Since string
	Sites []Site
	// Inherited / Own / Unknown count the verdicts.
	Inherited int
	Own       int
	Unknown   int
	// ByFile counts the sites per file, so the reader sees which file carries
	// the excess without scrolling the site list.
	ByFile map[string]int
}

// Attribute blames every finding to its introducing commit and classifies it
// against the cut point in since.
//
// It refuses an empty since (ErrAttributeNoCutPoint) rather than printing a list
// of undecided sites: a reading whose every line says "unknown" costs the same
// as no reading and invites the very conclusion this exists to prevent.
func Attribute(o Options, findings []Finding, since string) (*Composition, error) {
	root := o.Root
	if root == "" {
		root = "."
	}
	if since == "" {
		return nil, ErrAttributeNoCutPoint
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("stylegate: attribute: %w", err)
	}
	c := &Composition{Root: root, Since: since, ByFile: map[string]int{}}
	// Two caches, because a file's blame is one subprocess for all its sites
	// and a commit's ancestry is one subprocess for all the sites it introduced.
	blameCache := map[string]map[int]string{}
	ancestorCache := map[string]bool{}
	subjectCache := map[string]string{}
	for _, f := range findings {
		s := Site{Finding: f}
		lines, ok := blameCache[f.File]
		if !ok {
			lines, err = blameFile(abs, f.File)
			if err != nil {
				// Not fatal: one unreadable file must not blind the reading for
				// every other site. It is recorded per site as unknown+why.
				lines = map[int]string{}
			}
			blameCache[f.File] = lines
		}
		c.classify(&s, abs, lines, ancestorCache, subjectCache)
		c.Sites = append(c.Sites, s)
		c.ByFile[s.Finding.File]++
		c.countVerdicts(&c.Sites[len(c.Sites)-1])
	}
	sort.SliceStable(c.Sites, func(i, j int) bool {
		if c.Sites[i].Finding.File != c.Sites[j].Finding.File {
			return c.Sites[i].Finding.File < c.Sites[j].Finding.File
		}
		return c.Sites[i].Finding.Line < c.Sites[j].Finding.Line
	})
	return c, nil
}

// classify fills in s's WHO/WHEN verdict from the file's blame map, caching both
// halves so N sites in one file cost one blame and M sites from one commit cost
// one ancestry question.
func (c *Composition) classify(s *Site, abs string, lines map[int]string, ancestorCache map[string]bool, subjectCache map[string]string) {
	sha, blamed := lines[s.Finding.Line]
	if !blamed {
		s.Verdict = AttribUnknown
		s.Why = "no blame record for this line (untracked file, uncommitted line, or unreadable)"
		return
	}
	s.Commit = sha
	s.Subject = subjectOf(abs, sha, subjectCache)
	anc, ok := ancestorCache[sha]
	if !ok {
		var err error
		anc, err = commitIsAncestor(abs, sha, c.Since)
		if err != nil {
			s.Verdict = AttribUnknown
			s.Why = err.Error()
			return
		}
		ancestorCache[sha] = anc
	}
	if anc {
		s.Verdict = AttribInherited
		return
	}
	s.Verdict = AttribOwn
}

// countVerdicts tallies s. It takes a pointer because the caller has just
// appended the site to the slice and an AttribUnknown decision made here must be
// the one the reader sees.
func (c *Composition) countVerdicts(s *Site) {
	switch s.Verdict {
	case AttribInherited:
		c.Inherited++
	case AttribOwn:
		c.Own++
	default:
		c.Unknown++
		if s.Verdict == "" {
			s.Verdict = AttribUnknown
			s.Why = "verdict not decided"
		}
	}
}

// String renders one site the way the CLI prints it: file:line, rule, detail,
// then the WHO and WHEN halves.
func (s Site) String() string {
	head := fmt.Sprintf("%s:%d: [%s] %s", s.Finding.File, s.Finding.Line, s.Finding.Rule, s.Finding.Detail)
	if s.Commit == "" {
		return fmt.Sprintf("%s\n      %s (%s)", head, s.Verdict, s.Why)
	}
	return fmt.Sprintf("%s\n      %s %s — %s", head, shortSHA(s.Commit), s.Verdict, s.Subject)
}

// shortSHA abbreviates a full object name for display.
func shortSHA(sha string) string {
	if len(sha) <= shortSHALen {
		return sha
	}
	return sha[:shortSHALen]
}

// shortSHALen is the abbreviated object name length the report prints.
const shortSHALen = 8

// blameFile returns line→commit for one repo-relative file.
func blameFile(root, rel string) (map[int]string, error) {
	out, err := gitOutput(root, gitBlameVerb, gitLinePorcelainFlag, gitPathSeparator, rel)
	if err != nil {
		return nil, err
	}
	return parseBlame(out), nil
}

// parseBlame reads `git blame --line-porcelain` output.
//
// The shape that makes this unambiguous: with --line-porcelain every record
// carries its full object name in its header, and every CONTENT line is
// TAB-prefixed while no header ever is. So a non-TAB line whose first field is
// 40 hex characters is a record header, and the third field is the final line
// number the record describes. Nothing in the file being blamed can forge a
// header, because a source line is always emitted behind the TAB.
func parseBlame(out []byte) map[int]string {
	m := map[int]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" || strings.HasPrefix(line, "\t") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 || len(f[0]) != fullSHAHexLen || !isHex(f[0]) {
			continue
		}
		n, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		m[n] = f[0]
	}
	return m
}

// isHex reports whether every byte of s is a lowercase hex digit.
func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// commitIsAncestor answers whether commit already belongs to since's history.
func commitIsAncestor(root, commit, since string) (bool, error) {
	cmd := exec.Command(gitBin, gitMergeBaseVerb, gitIsAncestorFlag, commit, since)
	cmd.Dir = root
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == gitExitNotAncestor {
		return false, nil
	}
	if exitErr != nil {
		return false, fmt.Errorf("stylegate: git %s %s %s %s in %s: exit %d",
			gitMergeBaseVerb, gitIsAncestorFlag, commit, since, root, exitErr.ExitCode())
	}
	return false, fmt.Errorf("stylegate: git %s %s %s %s in %s: %w",
		gitMergeBaseVerb, gitIsAncestorFlag, commit, since, root, err)
}

// subjectOf returns a commit's subject line, cached.
func subjectOf(root, commit string, cache map[string]string) string {
	if s, ok := cache[commit]; ok {
		return s
	}
	out, err := gitOutput(root, gitShowVerb, gitNoPatchFlag, gitSubjectFlag, commit)
	if err != nil {
		cache[commit] = ""
		return ""
	}
	s := strings.TrimSpace(string(out))
	cache[commit] = s
	return s
}

// gitOutput runs one read-only git verb in root and returns its stdout.
func gitOutput(root string, args ...string) ([]byte, error) {
	cmd := exec.Command(gitBin, args...)
	cmd.Dir = root
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("stylegate: git %s in %s: %w (stderr: %s)",
			strings.Join(args, " "), root, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
