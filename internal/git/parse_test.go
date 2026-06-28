package git

import "testing"

func TestParseWorktreeList(t *testing.T) {
	out := "worktree /repo\n" +
		"HEAD 1111111111111111111111111111111111111111\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /repo.worktrees/login-fix\n" +
		"HEAD 2222222222222222222222222222222222222222\n" +
		"branch refs/heads/feat/login-fix\n" +
		"locked\n" +
		"\n" +
		"worktree /repo.worktrees/detached\n" +
		"HEAD 3333333333333333333333333333333333333333\n" +
		"detached\n"

	got := parseWorktreeList([]byte(out))
	if len(got) != 3 {
		t.Fatalf("got %d worktrees, want 3", len(got))
	}

	if got[0].Path != "/repo" || got[0].ShortBranch() != "main" || got[0].Detached {
		t.Errorf("worktree[0] = %+v", got[0])
	}
	if got[1].ShortBranch() != "feat/login-fix" || !got[1].Locked {
		t.Errorf("worktree[1] = %+v", got[1])
	}
	if !got[2].Detached || got[2].Branch != "" {
		t.Errorf("worktree[2] = %+v", got[2])
	}

	if w, ok := FindWorktreeOnBranch(got, "feat/login-fix"); !ok || w.Path != "/repo.worktrees/login-fix" {
		t.Errorf("FindWorktreeOnBranch = %+v, %v", w, ok)
	}
	if _, ok := FindWorktreeOnBranch(got, "nope"); ok {
		t.Errorf("FindWorktreeOnBranch found a nonexistent branch")
	}
}

func TestParseAheadBehind(t *testing.T) {
	tests := []struct {
		in           string
		ahead, behind int
	}{
		{"0\t0", 0, 0},
		{"3\t5", 5, 3},   // left=behind=3, right=ahead=5
		{"  2\t1  \n", 1, 2},
		{"garbage", 0, 0}, // tolerated as 0/0
	}
	for _, tt := range tests {
		ahead, behind, err := parseAheadBehind(tt.in)
		if err != nil {
			t.Fatalf("parseAheadBehind(%q) error: %v", tt.in, err)
		}
		if ahead != tt.ahead || behind != tt.behind {
			t.Errorf("parseAheadBehind(%q) = ahead %d behind %d, want ahead %d behind %d",
				tt.in, ahead, behind, tt.ahead, tt.behind)
		}
	}
}
