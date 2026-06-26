package core

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/cyakimov/treepi/internal/exit"
)

//go:embed initdata/treepi.toml.tmpl initdata/hooks/*.sh
var initFS embed.FS

// gitignoreLine is the per-machine override file init adds to .gitignore.
const gitignoreLine = ".treepi.local.toml"

// InitResult reports what Init scaffolded.
type InitResult struct {
	ConfigPath       string   `json:"config_path"`
	HooksDir         string   `json:"hooks_dir"`
	Created          []string `json:"created"`
	GitignoreUpdated bool     `json:"gitignore_updated"`
}

// Init scaffolds a committed .treepi.toml, generic hook stubs under
// .treepi/hooks/, and adds .treepi.local.toml to .gitignore (idempotently). It
// refuses to run inside a linked worktree - the config belongs at the main repo
// root - and refuses to clobber an existing .treepi.toml unless force is set.
func (s *Service) Init(ctx context.Context, force bool) (*InitResult, error) {
	gitDir, err := s.git.GitDir(ctx, s.repo.Root)
	if err != nil {
		return nil, exit.Wrap(exit.Internal, "git_dir_failed", "could not resolve the git dir", err)
	}
	if filepath.Clean(gitDir) != filepath.Clean(s.repo.CommonDir) {
		return nil, exit.New(exit.Usage, "linked_worktree", "run `treepi init` from the main worktree, not a linked one")
	}

	root := s.repo.Root
	cfgPath := filepath.Join(root, ".treepi.toml")
	hooksDir := filepath.Join(root, ".treepi", "hooks")
	res := &InitResult{ConfigPath: cfgPath, HooksDir: hooksDir}

	if fileExists(cfgPath) && !force {
		return nil, exit.New(exit.Refused, "config_exists", ".treepi.toml already exists (use --force to overwrite)")
	}

	tmpl, err := initFS.ReadFile("initdata/treepi.toml.tmpl")
	if err != nil {
		return nil, exit.Wrap(exit.Internal, "embed_failed", "missing embedded template", err)
	}
	if err := os.WriteFile(cfgPath, tmpl, 0o644); err != nil {
		return nil, exit.Wrap(exit.Internal, "write_failed", "could not write .treepi.toml", err)
	}
	res.Created = append(res.Created, cfgPath)

	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return nil, exit.Wrap(exit.Internal, "write_failed", "could not create .treepi/hooks", err)
	}
	entries, err := initFS.ReadDir("initdata/hooks")
	if err != nil {
		return nil, exit.Wrap(exit.Internal, "embed_failed", "missing embedded hook stubs", err)
	}
	for _, e := range entries {
		dst := filepath.Join(hooksDir, e.Name())
		if fileExists(dst) && !force {
			continue // do not clobber an existing stub
		}
		b, rerr := initFS.ReadFile("initdata/hooks/" + e.Name())
		if rerr != nil {
			continue
		}
		if err := os.WriteFile(dst, b, 0o755); err != nil { //nolint:gosec // executable hook stub
			return nil, exit.Wrap(exit.Internal, "write_failed", "could not write hook stub "+e.Name(), err)
		}
		res.Created = append(res.Created, dst)
	}

	updated, err := ensureGitignoreLine(filepath.Join(root, ".gitignore"), gitignoreLine)
	if err != nil {
		return nil, exit.Wrap(exit.Internal, "write_failed", "could not update .gitignore", err)
	}
	res.GitignoreUpdated = updated

	return res, nil
}

// ensureGitignoreLine appends line to the .gitignore at path if not already
// present, creating the file when missing. It reports whether it wrote.
func ensureGitignoreLine(path, line string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == line {
			return false, nil
		}
	}
	var sb strings.Builder
	sb.Write(b)
	if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString(line + "\n")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
