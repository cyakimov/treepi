package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

// LoadOptions controls which layers Load reads. Root and Home may be "" to skip
// the repo-file and global-file layers respectively (e.g. when not inside a
// repo). Getenv is an injection seam for tests; nil means os.Getenv.
type LoadOptions struct {
	Root   string
	Home   string
	Getenv func(string) string
}

// Load resolves the effective configuration by layering, low to high:
//
//	built-in defaults
//	< ~/.config/treepi/config.toml   (global; [hooks.*] ignored - hooks are repo-scoped)
//	< <root>/.treepi.toml            (committed project policy; sets ConfigPath)
//	< <root>/.treepi.local.toml      (gitignored per-machine override)
//	< TREEPI_* env allowlist
//
// Flags are applied by the caller after Load. Missing files are skipped; a
// malformed file or invalid value is returned as a plain error (the cli maps it
// to exit code 2). Load never imports exit and never touches git.
func Load(o LoadOptions) (Config, error) {
	getenv := o.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	cfg := Default()

	if o.Home != "" {
		global := filepath.Join(o.Home, ".config", "treepi", "config.toml")
		if err := mergeFileLayer(&cfg, global, false); err != nil {
			return cfg, err
		}
	}
	if o.Root != "" {
		committed := filepath.Join(o.Root, ".treepi.toml")
		if err := mergeFileLayer(&cfg, committed, true); err != nil {
			return cfg, err
		}
		if fileExists(committed) {
			cfg.ConfigPath = committed
		}
		if err := mergeFileLayer(&cfg, filepath.Join(o.Root, ".treepi.local.toml"), true); err != nil {
			return cfg, err
		}
	}
	if err := applyEnv(&cfg, getenv); err != nil {
		return cfg, err
	}
	if err := validate(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func mergeFileLayer(dst *Config, path string, allowHooks bool) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	var fc fileConfig
	if err := toml.Unmarshal(b, &fc); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	return applyFileConfig(dst, fc, allowHooks, path)
}

// applyFileConfig applies the set fields of fc onto dst. Only fields a layer
// actually specified (non-nil pointers / non-empty slices) override the lower
// layer.
func applyFileConfig(dst *Config, fc fileConfig, allowHooks bool, path string) error {
	if fc.Placement != nil && fc.Placement.BaseDir != nil {
		dst.BaseDirTemplate = *fc.Placement.BaseDir
	}
	if b := fc.Branch; b != nil {
		if len(b.Types) > 0 {
			dst.BranchTypes = append([]string(nil), b.Types...)
		}
		if b.Template != nil {
			dst.BranchTemplate = *b.Template
		}
		if b.Trunk != nil {
			dst.Trunk = *b.Trunk
		}
		if b.DefaultType != nil {
			dst.DefaultType = *b.DefaultType
		}
	}
	if fc.Merge != nil && fc.Merge.Verify != nil {
		dst.Verify = append([]string(nil), fc.Merge.Verify...)
	}
	if fc.Slots != nil && fc.Slots.Range != nil {
		lo, hi, err := parseSlotRange(*fc.Slots.Range)
		if err != nil {
			return fmt.Errorf("config %s: %w", path, err)
		}
		dst.SlotLo, dst.SlotHi = lo, hi
	}
	if fc.Lease != nil && fc.Lease.TTL != nil {
		dst.LeaseTTL = fc.Lease.TTL.Duration
	}
	if fc.Snapshot != nil && fc.Snapshot.IncludeIgnored != nil {
		dst.IncludeIgnored = append([]string(nil), fc.Snapshot.IncludeIgnored...)
	}
	if len(fc.Hooks) > 0 {
		// Hooks live only in the committed repo config (and its .local override),
		// never in the user-global config: the knowledge is project-specific and
		// must match across every human and agent on the repo. Global hooks are
		// silently ignored.
		if !allowHooks {
			return nil
		}
		if dst.Hooks == nil {
			dst.Hooks = map[string]HookSpec{}
		}
		for ev, hf := range fc.Hooks {
			if !isValidEvent(ev) {
				return fmt.Errorf("config %s: unknown hook event %q (valid: %s)", path, ev, strings.Join(HookEvents, ", "))
			}
			spec := HookSpec{Command: append([]string(nil), hf.Command...)}
			if hf.Timeout != nil {
				spec.Timeout = hf.Timeout.Duration
			}
			if hf.OnFailure != nil {
				if !isValidOnFailure(*hf.OnFailure) {
					return fmt.Errorf("config %s: hook %s: invalid on_failure %q (rollback|abort|warn)", path, ev, *hf.OnFailure)
				}
				spec.OnFailure = *hf.OnFailure
			}
			dst.Hooks[ev] = spec
		}
	}
	return nil
}

func applyEnv(dst *Config, getenv func(string) string) error {
	if v := getenv("TREEPI_TRUNK"); v != "" {
		dst.Trunk = v
	}
	if v := getenv("TREEPI_DEFAULT_TYPE"); v != "" {
		dst.DefaultType = v
	}
	if v := getenv("TREEPI_BASE_DIR"); v != "" {
		dst.BaseDirTemplate = v
	}
	if v := getenv("TREEPI_LEASE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("config: TREEPI_LEASE_TTL %q: %w", v, err)
		}
		dst.LeaseTTL = d
	}
	return nil
}

func validate(c *Config) error {
	if c.SlotLo > c.SlotHi {
		return fmt.Errorf("config: slot range lo (%d) > hi (%d)", c.SlotLo, c.SlotHi)
	}
	if c.DefaultType != "" && !c.IsType(c.DefaultType) {
		return fmt.Errorf("config: default_type %q is not in branch types %v", c.DefaultType, c.BranchTypes)
	}
	if !strings.Contains(c.BranchTemplate, "{task}") {
		return fmt.Errorf("config: branch template %q must contain {task}", c.BranchTemplate)
	}
	return nil
}

// parseSlotRange parses "lo-hi" (e.g. "0-15") into inclusive bounds.
func parseSlotRange(s string) (lo, hi int, err error) {
	parts := strings.SplitN(strings.TrimSpace(s), "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid slot range %q (want \"lo-hi\")", s)
	}
	lo, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid slot range %q: %w", s, err)
	}
	hi, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid slot range %q: %w", s, err)
	}
	if lo > hi {
		return 0, 0, fmt.Errorf("invalid slot range %q: lo > hi", s)
	}
	return lo, hi, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
