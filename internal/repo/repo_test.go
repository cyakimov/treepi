package repo

import (
	"path/filepath"
	"runtime"
	"testing"
)

// osAbs turns a /-rooted test path into one that is absolute on the current OS,
// so RenderBaseDir's filepath.IsAbs branch behaves the same on Windows (where a
// volume is required) as on Unix.
func osAbs(p string) string {
	if runtime.GOOS == "windows" {
		return filepath.Clean("C:" + filepath.FromSlash(p))
	}
	return filepath.FromSlash(p)
}

func TestRenderBaseDir(t *testing.T) {
	tests := []struct {
		tmpl, root, want string
	}{
		{"{repo}.worktrees", osAbs("/a/b/hypervisor"), osAbs("/a/b/hypervisor.worktrees")},
		{"", osAbs("/a/b/repo"), osAbs("/a/b/repo.worktrees")}, // default
		{"trees", osAbs("/a/b/repo"), osAbs("/a/b/trees")},     // relative -> under parent
		{osAbs("/abs/{repo_dir}-wt"), osAbs("/a/b/repo"), osAbs("/abs/repo-wt")},
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
	// An empty type collapses the leading slash so `new <task>` yields a clean name.
	if got := BranchName("{type}/{task}", "", "login-fix"); got != "login-fix" {
		t.Errorf("BranchName empty type = %q, want login-fix", got)
	}
	r := &Repo{CommonDir: osAbs("/a/b/repo/.git"), BaseDir: osAbs("/a/b/repo.worktrees")}
	if got, want := r.TaskPath("login"), filepath.Join(r.BaseDir, "login"); got != want {
		t.Errorf("TaskPath = %q, want %q", got, want)
	}
	if got, want := r.StateDir(), filepath.Join(r.CommonDir, "treepi"); got != want {
		t.Errorf("StateDir = %q, want %q", got, want)
	}
}
