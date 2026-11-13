package tools

// Adoption helpers that need the conflict-hunk reader + deliverable JSON
// decode (split from task_adoption.go for testability).

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// taskDeliverableBranch extracts deliverable.branch from a deliverable JSON
// string ("" when absent/undecodable — branch-less rows never match ①).
func taskDeliverableBranch(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	var d struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return "", err
	}
	return strings.TrimSpace(d.Branch), nil
}

// taskDeliverableWorktree extracts deliverable.worktree from a deliverable
// JSON string ("" when absent/undecodable). The creation-time backfill
// (bugfix epic 2026-09-03) stamps it so recycle/cleanup/resume can pair a
// live row to its worktree even before the branch diverges.
func taskDeliverableWorktree(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var d struct {
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return ""
	}
	return strings.TrimSpace(d.Worktree)
}

// adoptionConflictFiles lists the files git reports as conflicted right
// now (`git diff --name-only --diff-filter=U`) — empty when none.
func adoptionConflictFiles(repo string) ([]string, error) {
	out, err := adoptionGit(repo, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

// adoptionHunkCap bounds each extracted conflict block (Rule 1: named).
const adoptionHunkCap = 40

// adoptionMergeConflictBriefing appends the per-file conflict hunks
// (<<<<<<< … >>>>>>>) for every conflicted file — the self-contained part
// of the ③b briefing (P2.5 precedent: the executor never re-derives the
// conflict scene; it reads it from the payload).
func adoptionMergeConflictBriefing(repo string, br adoptionBranch, b *strings.Builder) error {
	files, err := adoptionConflictFiles(repo)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		b.WriteString("\nNo conflicted files reported by git (the probe failed for another reason — inspect the repo state manually).\n")
		return nil
	}
	fmt.Fprintf(b, "\nConflicted files (%d):\n", len(files))
	for _, f := range files {
		fmt.Fprintf(b, "\n--- %s ---\n", f)
		out, err := exec.Command("git", "-C", repo, "show", ":"+f).Output()
		if err != nil {
			// Binary or unmerged-index edge: name the file, move on.
			fmt.Fprintf(b, "(conflict block unavailable: %v)\n", err)
			continue
		}
		inHunk := false
		n := 0
		for _, line := range strings.Split(string(out), "\n") {
			switch {
			case strings.HasPrefix(line, "<<<<<<<"):
				inHunk = true
				n = 0
				b.WriteString(line + "\n")
			case strings.HasPrefix(line, ">>>>>>>"):
				inHunk = false
				b.WriteString(line + "\n")
			case inHunk:
				if n < adoptionHunkCap {
					b.WriteString(line + "\n")
					n++
				} else if n == adoptionHunkCap {
					b.WriteString("… (hunk truncated)\n")
					n++
				}
			}
		}
	}
	return nil
}
