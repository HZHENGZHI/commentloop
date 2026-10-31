package coderepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocalGitRunInheritsEnvironment(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", repo, "init", "-q").Run(); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(config, []byte("[commentloop]\n\ttest = inherited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The child git process must inherit GIT_CONFIG_GLOBAL; appending to a nil
	// Env would drop it and every other caller variable.
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	out, err := localGitRun(context.Background(), repo, "config", "--get", "commentloop.test")
	if err != nil {
		t.Fatal(err)
	}
	if out != "inherited" {
		t.Fatalf("git output = %q, want inherited", out)
	}
}
