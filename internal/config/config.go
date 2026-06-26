// Package config holds treepi's resolved configuration. The layered TOML loader
// (built-in < ~/.config < .treepi.toml < .local < env < flags) lands later; for
// now Default() supplies the built-in values so the core is runnable and
// provably agnostic before any hook or config file exists.
package config

import "time"

// Config is the resolved settings for a repo.
type Config struct {
	DefaultType     string
	BranchTypes     []string
	Trunk           string   // "" means autodetect (origin/HEAD, else "main")
	BaseDirTemplate string   // supports {repo} and {repo_dir}
	Verify          []string // merge verify command (argv); empty skips
	IncludeIgnored  []string // snapshot globs for precious gitignored files
	SlotLo          int
	SlotHi          int
	LeaseTTL        time.Duration
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		DefaultType:     "feat",
		BranchTypes:     []string{"feat", "fix", "chore", "docs", "refactor"},
		Trunk:           "",
		BaseDirTemplate: "{repo}.worktrees",
		Verify:          nil,
		IncludeIgnored:  nil,
		SlotLo:          0,
		SlotHi:          15,
		LeaseTTL:        2 * time.Hour,
	}
}

// IsType reports whether t is an allowed branch type.
func (c Config) IsType(t string) bool {
	for _, x := range c.BranchTypes {
		if x == t {
			return true
		}
	}
	return false
}
