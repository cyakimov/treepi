package cli

import (
	"reflect"
	"testing"
)

func TestRewriteArgs(t *testing.T) {
	known := map[string]bool{"new": true, "ls": true, "where": true, "help": true}
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"bare -> dashboard", nil, []string{"ls"}},
		{"known command untouched", []string{"ls"}, []string{"ls"}},
		{"branch type -> new", []string{"feat", "x"}, []string{"new", "feat", "x"}},
		{"leading flag untouched", []string{"--json", "ls"}, []string{"--json", "ls"}},
		{"short flag untouched", []string{"-h"}, []string{"-h"}},
		{"unknown first arg untouched", []string{"bogus", "x"}, []string{"bogus", "x"}},
		{"where untouched", []string{"where", "t"}, []string{"where", "t"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rewriteArgs(c.in, known); !reflect.DeepEqual(got, c.want) {
				t.Errorf("rewriteArgs(%v) = %v, want %v", c.in, got, c.want)
			}
		})
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
