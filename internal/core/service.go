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
	"strings"
	"time"

	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
	"github.com/cyakimov/treepi/internal/git"
	"github.com/cyakimov/treepi/internal/repo"
	"github.com/cyakimov/treepi/internal/state"
)

// Clock supplies time and ids; injected for deterministic tests.
type Clock interface {
	Now() time.Time
	NewID() string
}

// Service is the orchestration layer. Construct it with Open.
type Service struct {
	git   *git.Client
	repo  *repo.Repo
	store *state.Store
	cfg   config.Config
	clock Clock
	warn  io.Writer
}

// Open discovers the repo containing dir, resolves the trunk, and opens the
// state store under the shared .git dir.
func Open(ctx context.Context, dir string, cfg config.Config, clock Clock, warn io.Writer) (*Service, error) {
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
	return &Service{git: g, repo: r, store: st, cfg: cfg, clock: clock, warn: warn}, nil
}

// Repo exposes the resolved layout (used by the cli for display).
func (s *Service) Repo() *repo.Repo { return s.repo }

// LeaseInfo mirrors a lease for the --json/display surface.
type LeaseInfo struct {
	Owner   string    `json:"owner"`
	Host    string    `json:"host"`
	Expires time.Time `json:"expires"`
}

// TaskInfo is the typed result for a single worktree, marshalled to --json.
type TaskInfo struct {
	Task   string         `json:"task"`
	Type   string         `json:"type"`
	Branch string         `json:"branch"`
	Path   string         `json:"path"`
	Slot   int            `json:"slot"`
	Status string         `json:"status"`
	Ahead  int            `json:"ahead,omitempty"`
	Behind int            `json:"behind,omitempty"`
	Dirty  bool           `json:"dirty,omitempty"`
	Lease  *LeaseInfo     `json:"lease,omitempty"`
	Extras map[string]any `json:"extras,omitempty"`
}

func taskInfo(t *state.Task) *TaskInfo {
	ti := &TaskInfo{
		Task:   t.Name,
		Type:   t.Type,
		Branch: t.Branch,
		Path:   t.Path,
		Slot:   t.Slot,
		Status: string(t.Status),
		Extras: t.Extras,
	}
	if t.Lease != nil {
		ti.Lease = &LeaseInfo{Owner: t.Lease.Owner, Host: t.Lease.Host, Expires: t.Lease.Expires}
	}
	return ti
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

// resolveBase resolves the commit to cut/rebase from: the freshly-fetched
// remote-tracking trunk when an upstream exists, else the local trunk. offline
// reports that an upstream existed but the fetch failed (callers may warn).
func (s *Service) resolveBase(ctx context.Context) (oid string, offline bool, err error) {
	dir := s.repo.Root
	up, uerr := s.git.UpstreamRef(ctx, dir, s.repo.Trunk)
	if uerr == nil && up != "" {
		remote := remoteFromTrackingRef(up)
		if ferr := s.git.Fetch(ctx, dir, remote, s.repo.Trunk); ferr == nil {
			if oid, rerr := s.git.ResolveRef(ctx, dir, up); rerr == nil {
				return oid, false, nil
			}
		} else {
			offline = true
		}
	}
	// Fall back to the local trunk ref (offline / no upstream).
	if oid, rerr := s.git.ResolveRef(ctx, dir, "refs/heads/"+s.repo.Trunk); rerr == nil {
		return oid, offline, nil
	}
	if oid, rerr := s.git.ResolveRef(ctx, dir, s.repo.Trunk); rerr == nil {
		return oid, offline, nil
	}
	return "", offline, exit.New(exit.NoTrunk, "no_trunk", "could not resolve trunk "+s.repo.Trunk)
}

// remoteFromTrackingRef extracts "origin" from "refs/remotes/origin/main".
func remoteFromTrackingRef(ref string) string {
	const p = "refs/remotes/"
	if !strings.HasPrefix(ref, p) {
		return "origin"
	}
	rest := strings.TrimPrefix(ref, p)
	if i := strings.IndexByte(rest, '/'); i > 0 {
		return rest[:i]
	}
	return "origin"
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

// defaultOwner identifies the lease holder: $TREEPI_OWNER, else user@host.
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
