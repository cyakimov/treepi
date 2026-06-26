// Package repo discovers the git repository and computes treepi's path layout:
// the absolute common-dir (where state lives), the trunk branch, the sibling
// worktree base dir, and per-task branch/path names.
package repo

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Git is the small slice of git plumbing repo discovery needs. It is satisfied
// by *git.Client; defining it here keeps repo decoupled and testable.
type Git interface {
	Toplevel(ctx context.Context, dir string) (string, error)
	CommonDir(ctx context.Context, dir string) (string, error)
}

// Repo is the resolved layout of a treepi-managed repository.
type Repo struct {
	Root      string // absolute worktree root of the main clone
	CommonDir string // absolute shared .git dir
	Trunk     string // trunk branch name, e.g. "main"
	BaseDir   string // absolute dir holding worktrees (<repo>.worktrees by default)
}

// StateDir is where treepi keeps its manifest, journal, and leases.
func (r *Repo) StateDir() string { return filepath.Join(r.CommonDir, "treepi") }

// TaskPath renders the worktree path for a task under BaseDir.
func (r *Repo) TaskPath(task string) string { return filepath.Join(r.BaseDir, task) }

// BranchName renders the branch for a task from a template whose tokens are
// {type} and {task}, e.g. "{type}/{task}" -> "feat/login-fix". An empty template
// defaults to "{type}/{task}". The rendered name is validated by the caller via
// git check-ref-format.
func BranchName(template, typ, task string) string {
	if template == "" {
		template = "{type}/{task}"
	}
	out := strings.ReplaceAll(template, "{type}", typ)
	out = strings.ReplaceAll(out, "{task}", task)
	return out
}

// Discover resolves the repo containing dir. trunk and baseTemplate come from
// resolved config; baseTemplate supports the {repo} (absolute repo path) and
// {repo_dir} tokens and defaults to "{repo}.worktrees".
func Discover(ctx context.Context, g Git, dir, trunk, baseTemplate string) (*Repo, error) {
	root, err := g.Toplevel(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("repo: not inside a git worktree: %w", err)
	}
	common, err := g.CommonDir(ctx, dir)
	if err != nil {
		return nil, err
	}
	return &Repo{
		Root:      root,
		CommonDir: common,
		Trunk:     trunk,
		BaseDir:   RenderBaseDir(baseTemplate, root),
	}, nil
}

// RenderBaseDir expands a base-dir template against an absolute repo root.
func RenderBaseDir(tmpl, root string) string {
	if tmpl == "" {
		tmpl = "{repo}.worktrees"
	}
	out := strings.ReplaceAll(tmpl, "{repo}", root)
	out = strings.ReplaceAll(out, "{repo_dir}", filepath.Base(root))
	if !filepath.IsAbs(out) {
		out = filepath.Join(filepath.Dir(root), out)
	}
	return filepath.Clean(out)
}
