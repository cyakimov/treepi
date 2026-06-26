package core

import (
	"context"
	"fmt"

	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/git"
	"github.com/cyakimov/treepi/internal/hooks"
	"github.com/cyakimov/treepi/internal/version"
)

// baseHookContext assembles the common TREEPI_* contract for an event on a task.
// Callers set the per-event fields (Op, OldHead, NewHead, HasSubmodules) before
// firing.
func (s *Service) baseHookContext(ctx context.Context, event, task, typ, branch, path string, slot int) hooks.Context {
	return hooks.Context{
		Event:        event,
		Task:         task,
		Type:         typ,
		Branch:       branch,
		Slot:         slot,
		Trunk:        s.repo.Trunk,
		RepoRoot:     s.repo.Root,
		MainPath:     s.mainPath(ctx),
		WorktreePath: path,
		BaseDir:      s.repo.BaseDir,
		LeaseOwner:   defaultOwner(),
		ConfigPath:   s.cfg.ConfigPath,
		Version:      version.Value,
	}
}

// mainPath returns the trunk worktree's path, or "" when the trunk is not
// checked out anywhere (the natural feature-trees-only layout).
func (s *Service) mainPath(ctx context.Context) string {
	wts, err := s.git.WorktreeList(ctx, s.repo.Root)
	if err != nil {
		return ""
	}
	if wt, ok := git.FindWorktreeOnBranch(wts, s.repo.Trunk); ok {
		return wt.Path
	}
	return ""
}

// fireHook runs the hook configured for hc.Event, if any. It returns the extras
// the hook reported, the resolved spec (so the caller can read OnFailure), and a
// *hooks.HookError on failure. When no hook is configured it is a clean no-op.
func (s *Service) fireHook(ctx context.Context, hc hooks.Context) (map[string]any, config.HookSpec, error) {
	spec, ok := s.cfg.HookFor(hc.Event)
	if !ok {
		return nil, config.HookSpec{}, nil
	}
	extras, err := s.hooks.Run(ctx, spec, hc)
	return extras, spec, err
}

// warnf records a warning, surfaced both on stderr (text mode) and in the --json
// envelope's warnings[] array.
func (s *Service) warnf(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	s.warnings = append(s.warnings, msg)
	fmt.Fprintln(s.warn, "treepi: "+msg)
}

// Warnings returns the warnings accumulated during the operation, for the --json
// envelope. The cli passes these into emitJSONWarn.
func (s *Service) Warnings() []string { return s.warnings }

// mergeExtras shallow-merges src into dst (later keys win), allocating dst when
// needed. A nil/empty src leaves dst unchanged.
func mergeExtras(dst, src map[string]any) map[string]any {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]any, len(src))
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
