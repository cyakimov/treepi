// Package config holds treepi's resolved configuration and the layered TOML
// loader that builds it: built-in defaults < ~/.config/treepi/config.toml <
// <root>/.treepi.toml < <root>/.treepi.local.toml < TREEPI_* env < flags. It is
// a leaf package - it never imports git or exit, so the cli resolves the repo
// root and passes it in, and maps any load error to exit code 2.
package config

import "time"

// Config is the resolved settings for a repo.
type Config struct {
	BranchTemplate  string   // branch-name template; tokens {type} {task}; "" -> "{type}/{task}"
	Trunk           string   // "" means autodetect (origin/HEAD, else "main")
	BaseDirTemplate string   // supports {repo} and {repo_dir}
	Verify          []string // merge verify command (argv); empty skips
	IncludeIgnored  []string // snapshot globs for precious gitignored files

	// Hooks are the resolved lifecycle hooks, keyed by event name; nil when none
	// are configured. ConfigPath is the absolute path of the repo's .treepi.toml
	// ("" when none), surfaced to hooks as TREEPI_CONFIG_PATH.
	Hooks      map[string]HookSpec
	ConfigPath string
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		BranchTemplate:  "{type}/{task}",
		Trunk:           "",
		BaseDirTemplate: "{repo}.worktrees",
		Verify:          nil,
		IncludeIgnored:  nil,
	}
}

// Lifecycle hook events, in lifecycle order.
const (
	EventPostCreate = "post_create"
	EventPostSync   = "post_sync"
	EventPreMerge   = "pre_merge"
	EventPostMerge  = "post_merge"
	EventPreRemove  = "pre_remove"
)

// HookEvents lists every valid hook event.
var HookEvents = []string{EventPostCreate, EventPostSync, EventPreMerge, EventPostMerge, EventPreRemove}

// on_failure dispositions.
const (
	OnFailureRollback = "rollback" // snapshot partial work, then undo the operation
	OnFailureAbort    = "abort"    // stop the operation, leaving prior state intact
	OnFailureWarn     = "warn"     // surface the failure but proceed
)

// HookSpec is a resolved lifecycle hook.
type HookSpec struct {
	Command   []string
	Timeout   time.Duration // 0 = no deadline (the parent context still cancels)
	OnFailure string        // rollback | abort | warn
}

// DefaultOnFailure is the on_failure disposition for event when the config
// leaves it unset: post_create rolls back, pre_merge aborts, everything else
// warns (those hooks fire after their effect is already committed).
func DefaultOnFailure(event string) string {
	switch event {
	case EventPostCreate:
		return OnFailureRollback
	case EventPreMerge:
		return OnFailureAbort
	default:
		return OnFailureWarn
	}
}

// HookFor returns the spec for event with OnFailure defaulted. ok is false when
// no command is configured, so callers no-op without special-casing.
func (c Config) HookFor(event string) (HookSpec, bool) {
	h, ok := c.Hooks[event]
	if !ok || len(h.Command) == 0 {
		return HookSpec{}, false
	}
	if h.OnFailure == "" {
		h.OnFailure = DefaultOnFailure(event)
	}
	return h, true
}

func isValidEvent(ev string) bool {
	for _, e := range HookEvents {
		if e == ev {
			return true
		}
	}
	return false
}

func isValidOnFailure(s string) bool {
	return s == OnFailureRollback || s == OnFailureAbort || s == OnFailureWarn
}
