package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureLocalExcludes(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "init")

	worktreePath := filepath.Join(root, "wt")
	runGit(t, root, "worktree", "add", worktreePath, "-b", "conduct/0001-test")

	patterns := []string{".conduct/current.md", ".conduct/worktrees/"}
	if err := EnsureLocalExcludes(worktreePath, patterns); err != nil {
		t.Fatal(err)
	}
	if err := EnsureLocalExcludes(worktreePath, patterns); err != nil {
		t.Fatal(err)
	}

	gitDir, err := gitDirFor(worktreePath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(gitDir, "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(data)
	for _, pattern := range patterns {
		if !strings.Contains(contents, pattern) {
			t.Fatalf("exclude missing %q in %q", pattern, contents)
		}
	}
	if strings.Count(contents, ".conduct/current.md") != 1 {
		t.Fatalf("expected one current.md entry, got %q", contents)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitArgs := append([]string{"-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
