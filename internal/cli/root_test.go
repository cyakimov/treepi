package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/cyakimov/treepi/internal/exit"
)

func TestRewriteArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"bare -> ls", nil, []string{"ls"}},
		{"known command untouched", []string{"ls"}, []string{"ls"}},
		{"new passthrough", []string{"new", "feat", "x"}, []string{"new", "feat", "x"}},
		{"leading flag untouched", []string{"--json", "ls"}, []string{"--json", "ls"}},
		{"short flag untouched", []string{"-h"}, []string{"-h"}},
		{"where untouched", []string{"where", "t"}, []string{"where", "t"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rewriteArgs(c.in); !reflect.DeepEqual(got, c.want) {
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
