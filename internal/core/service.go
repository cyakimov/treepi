// Package core orchestrates git plumbing and the state store into the treepi
// operations. It is the single seam the cli layer drives; every method takes a
// context and returns typed results that marshal directly to the --json
// envelope.
package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
	"github.com/cyakimov/treepi/internal/git"
	"github.com/cyakimov/treepi/internal/hooks"
	"github.com/cyakimov/treepi/internal/repo"
	"github.com/cyakimov/treepi/internal/state"
)

// Clock supplies time and ids; injected for deterministic tests.
type Clock interface {
	Now() time.Time
	NewID() string
}

// HookRunner executes a resolved lifecycle hook. The default implementation is
// *hooks.Runner; tests inject a fake via WithHookRunner.
type HookRunner interface {
	Run(ctx context.Context, spec config.HookSpec, hc hooks.Context) error
}

// Option customizes a Service at construction (used by tests).
type Option func(*Service)

// WithHookRunner overrides the default hook runner (a test seam).
func WithHookRunner(h HookRunner) Option {
	return func(s *Service) { s.hooks = h }
}

// Service is the orchestration layer. Construct it with Open.
type Service struct {
	git      *git.Client
	repo     *repo.Repo
	store    *state.Store
	cfg      config.Config
	clock    Clock
	warn     io.Writer
	hooks    HookRunner
	warnings []string
}

// Open discovers the repo containing dir, resolves the trunk, and opens the
// state store under the shared .git dir. Optional Options (e.g. a fake hook
// runner) are applied last.
func Open(ctx context.Context, dir string, cfg config.Config, clock Clock, warn io.Writer, opts ...Option) (*Service, error) {
	g := git.NewClient(git.ExecRunner{})

	trunk := cfg.Trunk
	if trunk == "" {
		if b, err := g.RemoteHeadBranch(ctx, dir, "origin"); err == nil && b != "" {
			trunk = b
		} else {
			trunk = "main"
		}
	}

	r, err := repo.Discover(ctx, g, dir, trunk, cfg.BaseDirTemplate)
	if err != nil {
		return nil, exit.Wrap(exit.Usage, "not_a_repo", "not inside a git repository", err)
	}
	st, err := state.Open(r.StateDir(), clock)
	if err != nil {
		return nil, err
	}
	if warn == nil {
		warn = io.Discard
	}
	s := &Service{git: g, repo: r, store: st, cfg: cfg, clock: clock, warn: warn, hooks: hooks.New(warn)}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Repo exposes the resolved layout (used by the cli for display).
func (s *Service) Repo() *repo.Repo { return s.repo }

// TaskInfo is the typed result for a single worktree, marshalled to --json.
type TaskInfo struct {
	Task   string `json:"task"`
	Type   string `json:"type"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Ahead  int    `json:"ahead,omitempty"`
	Behind int    `json:"behind,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
}

func taskInfo(t *state.Task) *TaskInfo {
	return &TaskInfo{
		Task:   t.Name,
		Type:   t.Type,
		Branch: t.Branch,
		Path:   t.Path,
		Status: string(t.Status),
	}
}

func sortedTasks(m *state.Manifest) []*state.Task {
	out := make([]*state.Task, 0, len(m.Tasks))
	for _, t := range m.Tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// gitReconciler adapts *git.Client to state.Reconciler, the seam that keeps the
// state package from importing git.
type gitReconciler struct {
	g   *git.Client
	dir string
}

func (r gitReconciler) ListWorktrees(ctx context.Context) ([]state.WorktreeView, error) {
	wts, err := r.g.WorktreeList(ctx, r.dir)
	if err != nil {
		return nil, err
	}
	views := make([]state.WorktreeView, 0, len(wts))
	for _, w := range wts {
		views = append(views, state.WorktreeView{
			Path:     w.Path,
			Branch:   w.ShortBranch(),
			Detached: w.Detached,
			Locked:   w.Locked,
			Prunable: w.Prunable,
		})
	}
	return views, nil
}

func (r gitReconciler) Prune(ctx context.Context) error { return r.g.WorktreePrune(ctx, r.dir) }

func (s *Service) reconciler() state.Reconciler {
	return gitReconciler{g: s.git, dir: s.repo.Root}
}

// resolveBase resolves the commit to cut/rebase from: the local trunk ref. treepi
// branches and rebases from your actual local trunk, so committing to trunk and
// immediately creating a worktree works without a push. Keeping local trunk
// current with the remote (e.g. `git pull` on trunk) is the user's choice - treepi
// does not fetch behind your back or silently branch from a stale remote tip.
func (s *Service) resolveBase(ctx context.Context) (string, error) {
	dir := s.repo.Root
	if oid, err := s.git.ResolveRef(ctx, dir, "refs/heads/"+s.repo.Trunk); err == nil {
		return oid, nil
	}
	if oid, err := s.git.ResolveRef(ctx, dir, s.repo.Trunk); err == nil {
		return oid, nil
	}
	return "", exit.New(exit.NoTrunk, "no_trunk", "could not resolve trunk "+s.repo.Trunk)
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

// defaultOwner identifies the actor for snapshot authorship and hook context:
// $TREEPI_OWNER, else user@host.
func defaultOwner() string {
	if o := os.Getenv("TREEPI_OWNER"); o != "" {
		return o
	}
	u := os.Getenv("USER")
	if u == "" {
		u = "user"
	}
	return fmt.Sprintf("%s@%s", u, hostname())
}

func cleanPath(p string) string { return filepath.Clean(p) }
