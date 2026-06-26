package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
)

func TestRewriteArgs(t *testing.T) {
	known := map[string]bool{"new": true, "ls": true, "where": true, "help": true}
	def := config.Default()
	custom := config.Config{BranchTypes: []string{"spike"}}
	cases := []struct {
		name string
		in   []string
		cfg  config.Config
		want []string
	}{
		{"bare -> dashboard", nil, def, []string{"dash"}},
		{"known command untouched", []string{"ls"}, def, []string{"ls"}},
		{"branch type -> new", []string{"feat", "x"}, def, []string{"new", "feat", "x"}},
		{"custom type -> new", []string{"spike", "x"}, custom, []string{"new", "spike", "x"}},
		{"non-type first arg untouched", []string{"spike", "x"}, def, []string{"spike", "x"}},
		{"leading flag untouched", []string{"--json", "ls"}, def, []string{"--json", "ls"}},
		{"short flag untouched", []string{"-h"}, def, []string{"-h"}},
		{"unknown first arg untouched", []string{"bogus", "x"}, def, []string{"bogus", "x"}},
		{"where untouched", []string{"where", "t"}, def, []string{"where", "t"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rewriteArgs(c.in, known, c.cfg); !reflect.DeepEqual(got, c.want) {
				t.Errorf("rewriteArgs(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestOpenServiceDefersBadConfig(t *testing.T) {
	saved := loaded
	defer func() { loaded = saved }()
	loaded = loadedConfig{err: errors.New("malformed config")}

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	_, err := openService(cmd)
	if err == nil {
		t.Fatal("expected an error when config failed to load")
	}
	if exit.CodeOf(err) != exit.Usage {
		t.Errorf("exit code = %d, want %d (Usage)", exit.CodeOf(err), exit.Usage)
	}
}

func TestShellInitScript(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		s, err := shellInitScript(sh)
		if err != nil || s == "" {
			t.Errorf("shellInitScript(%q) = %q, %v", sh, s, err)
		}
	}
	if _, err := shellInitScript("nope"); err == nil {
		t.Error("expected error for unknown shell")
	}
}
