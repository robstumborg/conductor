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

func TestCleanupHelpersHonorAndBypassGitSafety(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "init")
	initialBranch := gitOutput(t, root, "branch", "--show-current")

	worktreePath := filepath.Join(root, "wt")
	runGit(t, root, "worktree", "add", worktreePath, "-b", "conduct/0001-test")
	if err := os.WriteFile(filepath.Join(worktreePath, "README.md"), []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveWorktree(root, worktreePath); err == nil {
		t.Fatal("expected safe worktree removal to fail for dirty worktree")
	}
	if err := RemoveWorktreeForce(root, worktreePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected forced worktree removal, got %v", err)
	}

	runGit(t, root, "checkout", "-b", "conduct/0002-unmerged")
	if err := os.WriteFile(filepath.Join(root, "feature.txt"), []byte("feature\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "feature.txt")
	runGit(t, root, "commit", "-m", "feature")
	runGit(t, root, "checkout", initialBranch)

	if err := DeleteBranch(root, "conduct/0002-unmerged"); err == nil {
		t.Fatal("expected safe branch delete to fail for unmerged branch")
	}
	if err := DeleteBranchForce(root, "conduct/0002-unmerged"); err != nil {
		t.Fatal(err)
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

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	gitArgs := append([]string{"-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", gitArgs...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}
