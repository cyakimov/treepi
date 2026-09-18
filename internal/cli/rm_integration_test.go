//go:build integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cyakimov/treepi/internal/config"
	"github.com/cyakimov/treepi/internal/exit"
)

func rmTestRepo(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "treepi-test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@treepi")
	t.Setenv("GIT_COMMITTER_NAME", "treepi-test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@treepi")
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "seed.txt")
	git("commit", "-q", "-m", "seed")
	t.Chdir(dir)
	saved := loaded
	loaded = loadedConfig{cfg: config.Default()}
	t.Cleanup(func() { loaded = saved })
}

func runRmCLI(t *testing.T, args ...string) (map[string]any, error) {
	t.Helper()
	root := newRoot()
	root.SetArgs(args)
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	err := root.ExecuteContext(context.Background())
	if out.Len() == 0 {
		return nil, err
	}
	var envelope map[string]any
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("%v stdout is not JSON: %s (%v); stderr: %s", args, out.String(), decodeErr, stderr.String())
	}
	return envelope, err
}

func TestIntegrationRmJSONContract(t *testing.T) {
	rmTestRepo(t)
	if _, err := runRmCLI(t, "new", "feat", "single", "--json"); err != nil {
		t.Fatal(err)
	}
	single, err := runRmCLI(t, "rm", "single", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if data, ok := single["data"].(map[string]any); !ok || data["task"] != "single" {
		t.Fatalf("single rm changed its JSON data shape: %+v", single)
	}
	unknown, err := runRmCLI(t, "rm", "missing", "--json")
	if exit.CodeOf(err) != exit.NotFound || unknown["data"] != nil {
		t.Fatalf("single failure changed its JSON shape: %+v, error=%v", unknown, err)
	}
	for _, name := range []string{"a", "b"} {
		if _, err := runRmCLI(t, "new", "feat", name, "--json"); err != nil {
			t.Fatal(err)
		}
	}
	batch, err := runRmCLI(t, "rm", "a", "missing", "b", "--json")
	if exit.CodeOf(err) != exit.NotFound || batch["ok"] != false {
		t.Fatalf("batch outcome = %+v, error=%v", batch, err)
	}
	data, ok := batch["data"].(map[string]any)
	if !ok || len(data["removed"].([]any)) != 2 || len(data["failed"].([]any)) != 1 {
		t.Fatalf("batch data = %+v", batch)
	}
	if failed := data["failed"].([]any)[0].(map[string]any); failed["task"] != "missing" || failed["code"] != "not_found" {
		t.Fatalf("failure = %+v", failed)
	}
	if top := batch["error"].(map[string]any); top["code"] != "rm_failed" {
		t.Fatalf("aggregate error = %+v", top)
	}
	if _, err := runRmCLI(t, "new", "feat", "dupe", "--json"); err != nil {
		t.Fatal(err)
	}
	duplicate, err := runRmCLI(t, "rm", "dupe", "dupe", "--json")
	if err != nil {
		t.Fatal(err)
	}
	data = duplicate["data"].(map[string]any)
	if len(data["removed"].([]any)) != 1 || len(data["failed"].([]any)) != 0 {
		t.Fatalf("duplicate result = %+v", duplicate)
	}
	allFailed, err := runRmCLI(t, "rm", "missing-a", "missing-b", "--json")
	if exit.CodeOf(err) != exit.NotFound {
		t.Fatalf("all failed exit = %d", exit.CodeOf(err))
	}
	data = allFailed["data"].(map[string]any)
	if len(data["removed"].([]any)) != 0 || len(data["failed"].([]any)) != 2 {
		t.Fatalf("all failed result = %+v", allFailed)
	}
}

type failingOutput struct{ err error }

func (w failingOutput) Write([]byte) (int, error) { return 0, w.err }

func TestIntegrationRmJSONWriteFailure(t *testing.T) {
	rmTestRepo(t)
	if _, err := runRmCLI(t, "new", "feat", "demo", "--json"); err != nil {
		t.Fatal(err)
	}
	want := errors.New("output unavailable")
	root := newRoot()
	root.SetArgs([]string{"rm", "demo", "--json"})
	root.SetOut(failingOutput{err: want})
	root.SetErr(&bytes.Buffer{})
	if err := root.ExecuteContext(context.Background()); !errors.Is(err, want) {
		t.Fatalf("rm returned %v, want output error", err)
	}
	root = newRoot()
	root.SetArgs([]string{"rm", "missing", "--json"})
	root.SetOut(failingOutput{err: want})
	root.SetErr(&bytes.Buffer{})
	if err := root.ExecuteContext(context.Background()); !errors.Is(err, want) {
		t.Fatalf("failed rm returned %v, want output error", err)
	}
}

func TestIntegrationRmJSONIgnoredWarning(t *testing.T) {
	rmTestRepo(t)
	if err := os.WriteFile(".gitignore", []byte("secret.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", ".gitignore"}, {"commit", "-q", "-m", "ignore secret"}} {
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	created, err := runRmCLI(t, "new", "feat", "demo", "--json")
	if err != nil {
		t.Fatal(err)
	}
	worktree := created["data"].(map[string]any)["path"].(string)
	if err := os.WriteFile(filepath.Join(worktree, "secret.txt"), []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	response, err := runRmCLI(t, "rm", "demo", "--force", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if warnings, ok := response["warnings"].([]any); !ok || len(warnings) != 1 {
		t.Fatalf("ignored-file warning missing from JSON: %+v", response)
	}
}
