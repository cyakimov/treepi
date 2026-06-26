package repo

import (
	"path/filepath"
	"testing"
)

func TestRenderBaseDir(t *testing.T) {
	tests := []struct {
		tmpl, root, want string
	}{
		{"{repo}.worktrees", "/a/b/hypervisor", "/a/b/hypervisor.worktrees"},
		{"", "/a/b/repo", "/a/b/repo.worktrees"}, // default
		{"trees", "/a/b/repo", "/a/b/trees"},     // relative -> under parent
		{"/abs/{repo_dir}-wt", "/a/b/repo", "/abs/repo-wt"},
	}
	for _, tt := range tests {
		if got := RenderBaseDir(tt.tmpl, tt.root); got != tt.want {
			t.Errorf("RenderBaseDir(%q, %q) = %q, want %q", tt.tmpl, tt.root, got, tt.want)
		}
	}
}

func TestBranchNameAndPaths(t *testing.T) {
	if got := BranchName("{type}/{task}", "feat", "login-fix"); got != "feat/login-fix" {
		t.Errorf("BranchName = %q", got)
	}
	if got := BranchName("", "feat", "login-fix"); got != "feat/login-fix" {
		t.Errorf("BranchName default = %q", got)
	}
	if got := BranchName("wip/{task}", "feat", "x"); got != "wip/x" {
		t.Errorf("BranchName template = %q", got)
	}
	r := &Repo{CommonDir: "/a/b/repo/.git", BaseDir: "/a/b/repo.worktrees"}
	if got := r.TaskPath("login"); got != "/a/b/repo.worktrees/login" {
		t.Errorf("TaskPath = %q", got)
	}
	if got, want := r.StateDir(), filepath.Join("/a/b/repo/.git", "treepi"); got != want {
		t.Errorf("StateDir = %q, want %q", got, want)
	}
}
