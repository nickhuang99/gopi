package branchform

import "testing"

// TestDestructive is the vocabulary's own red/green set, one case per SHAPE the
// package claims to know — the proof that the gate built on it is not vacuous for
// a shape this test does not name would be a fiction, so every shape gets a row.
func TestDestructive(t *testing.T) {
	for _, tc := range []struct {
		why  string
		line string
		want string // the flag that must be reported ("" = nothing)
	}{
		// DESTRUCTIVE — the forms that reset an existing ref with no refusal.
		{"worktree add -B", "git worktree add ~/w/gopi-x -B feat/x", FlagCreateForce},
		{"worktree add -B, path last", "git worktree add -B feat/x ~/w/gopi-x", FlagCreateForce},
		{"checkout -B", "git checkout -B feat/x master", FlagCreateForce},
		{"switch -C", "git switch -C feat/x master", FlagSwitchCreateForce},
		{"checkout -b with -f", "git checkout -b feat/x -f master", FlagBranchForce},
		{"switch -c with --force", "git switch -c feat/x --force master", FlagBranchForceLong},
		{"branch -f", "git branch -f feat/x master", FlagBranchForce},
		{"branch --force", "git branch --force feat/x master", FlagBranchForceLong},
		{"git -C <dir> worktree add -B", "git -C /repo worktree add -B feat/x /repo/wt", FlagCreateForce},
		{"command chained after &&", "cd /repo && git worktree add -B feat/x /repo/wt", FlagCreateForce},
		{"markdown code span", "```bash git checkout -B feat/x master ```", FlagCreateForce},

		// SAFE — git refuses an existing name, so these ARE the prescription.
		{"worktree add -b", "git worktree add ~/w/gopi-x -b feat/x", ""},
		{"checkout -b", "git checkout -b feat/x master", ""},
		{"switch -c", "git switch -c feat/x master", ""},
		{"worktree add -b after the path", "git worktree add ~/w/gopi-x -b feat/x master", ""},
		{"branch -D (deliberate, named deletion — GOPI.md Rule 9)", "git tag archive/x && git branch -D feat/x", ""},
		{"branch -d", "git branch -d feat/x", ""},
		{"worktree remove --force (removes a checkout, not a ref)", "git worktree remove --force /w/gopi-x", ""},
		{"worktree list/prune", "git worktree list", ""},
		{"checkout -f (file restore)", "git checkout -f -- .", ""},
		{"the precheck the rule file prescribes", "git rev-parse --verify --quiet refs/heads/feat/x", ""},
		{"a bare flag in prose (no verb)", "| never | `-B`, `-C` | destructive |", ""},
		{"a later command's force flag does not taint an earlier create",
			"git worktree add ~/w/wt -b feat/x && git checkout -f -- .", ""},
		{"an unrelated command carrying -B-like text", "grep -B2 pattern file.go", ""},
	} {
		t.Run(tc.why, func(t *testing.T) {
			got := Destructive(tc.line)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("FALSE POSITIVE on %q: %+v", tc.line, got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("NOT REPORTED: %q must be a destructive form (%s)", tc.line, tc.want)
			}
			if got[0].Flag != tc.want {
				t.Fatalf("reported flag %q, want %q (line: %q)", got[0].Flag, tc.want, tc.line)
			}
		})
	}
}
