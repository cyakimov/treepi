package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func noEnv(string) string { return "" }

func TestLoadDefaultsWhenNoFiles(t *testing.T) {
	cfg, err := Load(LoadOptions{Getenv: noEnv})
	if err != nil {
		t.Fatal(err)
	}
	def := Default()
	if cfg.BranchTemplate != def.BranchTemplate || cfg.BaseDirTemplate != def.BaseDirTemplate {
		t.Errorf("Load with no layers != Default: %+v", cfg)
	}
	if cfg.ConfigPath != "" {
		t.Errorf("ConfigPath should be empty without a repo file, got %q", cfg.ConfigPath)
	}
}

func TestLoadRepoOverridesAndConfigPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".treepi.toml", `
[branch]
trunk = "develop"
template = "{task}"

[placement]
base_dir = "{repo}.trees"

[merge]
verify = ["just", "build"]

[snapshot]
include_ignored = [".env", ".env.local"]
`)
	cfg, err := Load(LoadOptions{Root: root, Getenv: noEnv})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Trunk != "develop" || cfg.BranchTemplate != "{task}" {
		t.Errorf("branch overrides not applied: %+v", cfg)
	}
	if cfg.BaseDirTemplate != "{repo}.trees" {
		t.Errorf("base_dir = %q", cfg.BaseDirTemplate)
	}
	if len(cfg.Verify) != 2 || cfg.Verify[0] != "just" {
		t.Errorf("verify = %v", cfg.Verify)
	}
	if len(cfg.IncludeIgnored) != 2 {
		t.Errorf("include_ignored = %v", cfg.IncludeIgnored)
	}
	if cfg.ConfigPath != filepath.Join(root, ".treepi.toml") {
		t.Errorf("ConfigPath = %q", cfg.ConfigPath)
	}
}

func TestLoadLayerPrecedence(t *testing.T) {
	home := t.TempDir()
	globalDir := filepath.Join(home, ".config", "treepi")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, globalDir, "config.toml", "[branch]\ntrunk = \"from-global\"\n")

	root := t.TempDir()
	writeFile(t, root, ".treepi.toml", "[branch]\ntrunk = \"from-repo\"\n")
	writeFile(t, root, ".treepi.local.toml", "[branch]\ntrunk = \"from-local\"\n")

	// local beats repo beats global.
	cfg, err := Load(LoadOptions{Root: root, Home: home, Getenv: noEnv})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Trunk != "from-local" {
		t.Errorf("expected local to win, got %q", cfg.Trunk)
	}

	// env beats every file.
	env := map[string]string{"TREEPI_TRUNK": "from-env"}
	cfg, err = Load(LoadOptions{Root: root, Home: home, Getenv: func(k string) string { return env[k] }})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Trunk != "from-env" {
		t.Errorf("expected env to win, got %q", cfg.Trunk)
	}
}

func TestLoadHooks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".treepi.toml", `
[hooks.post_create]
command = ["bash", ".treepi/hooks/post-create.sh"]
timeout = "5m"

[hooks.pre_merge]
command = ["just", "test"]
on_failure = "abort"
`)
	cfg, err := Load(LoadOptions{Root: root, Getenv: noEnv})
	if err != nil {
		t.Fatal(err)
	}
	pc, ok := cfg.HookFor(EventPostCreate)
	if !ok {
		t.Fatal("post_create hook missing")
	}
	if pc.Timeout != 5*time.Minute || pc.OnFailure != OnFailureRollback {
		t.Errorf("post_create = %+v (want timeout 5m, default rollback)", pc)
	}
	pm, _ := cfg.HookFor(EventPreMerge)
	if pm.OnFailure != OnFailureAbort {
		t.Errorf("pre_merge on_failure = %q", pm.OnFailure)
	}
	if _, ok := cfg.HookFor(EventPostMerge); ok {
		t.Error("post_merge should not be configured")
	}
}

func TestLoadGlobalHooksIgnored(t *testing.T) {
	home := t.TempDir()
	gd := filepath.Join(home, ".config", "treepi")
	if err := os.MkdirAll(gd, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, gd, "config.toml", "[hooks.post_create]\ncommand = [\"echo\", \"global\"]\n")
	cfg, err := Load(LoadOptions{Home: home, Getenv: noEnv})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.HookFor(EventPostCreate); ok {
		t.Error("global hooks must be ignored (hooks are repo-scoped)")
	}
}

func TestLoadRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"malformed toml": "this is not = valid = toml",
		"unknown event":  "[hooks.on_explode]\ncommand = [\"x\"]\n",
		"bad on_failure": "[hooks.post_create]\ncommand = [\"x\"]\non_failure = \"explode\"\n",
		"bad template":   "[branch]\ntemplate = \"{type}-only\"\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, ".treepi.toml", body)
			if _, err := Load(LoadOptions{Root: root, Getenv: noEnv}); err == nil {
				t.Errorf("%s: expected an error", name)
			}
		})
	}
}
