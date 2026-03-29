package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type FileStatus struct {
	XY   string
	Path string
}

func RepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func CurrentBranch(root string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func DetectMainBranch(root string) (string, error) {
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil {
		ref := strings.TrimSpace(string(out))
		const prefix = "refs/remotes/origin/"
		if strings.HasPrefix(ref, prefix) {
			branch := strings.TrimPrefix(ref, prefix)
			if branch != "" {
				return branch, nil
			}
		}
	}

	branch, err := CurrentBranch(root)
	if err != nil {
		return "", err
	}
	if branch == "HEAD" || branch == "" {
		return "", fmt.Errorf("could not detect main branch")
	}
	return branch, nil
}

func BranchExists(root, branch string) bool {
	return RefExists(root, "refs/heads/"+branch)
}

func RefExists(root, ref string) bool {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", ref)
	cmd.Dir = root
	return cmd.Run() == nil
}

func CreateWorktree(root, path, branch, startPoint string, createBranch bool) error {
	var cmd *exec.Cmd
	if createBranch {
		args := []string{"worktree", "add", path, "-b", branch}
		if strings.TrimSpace(startPoint) != "" {
			args = append(args, startPoint)
		}
		cmd = exec.Command("git", args...)
	} else {
		cmd = exec.Command("git", "worktree", "add", path, branch)
	}
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func RemoveWorktree(root, path string) error {
	cmd := exec.Command("git", "worktree", "remove", path)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func RemoveWorktreeForce(root, path string) error {
	cmd := exec.Command("git", "worktree", "remove", path, "--force")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove --force failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func DeleteBranch(root, branch string) error {
	cmd := exec.Command("git", "branch", "-d", branch)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch delete failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func DeleteBranchForce(root, branch string) error {
	cmd := exec.Command("git", "branch", "-D", branch)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch delete -D failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func SquashMerge(root, branch string) error {
	cmd := exec.Command("git", "merge", "--squash", branch)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git merge --squash failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func Status(root string) ([]FileStatus, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result []FileStatus
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if len(line) < 4 {
			continue
		}
		result = append(result, FileStatus{XY: line[:2], Path: strings.TrimSpace(line[3:])})
	}
	return result, nil
}

func IsClean(root string) (bool, []FileStatus, error) {
	status, err := Status(root)
	if err != nil {
		return false, nil, err
	}
	return len(status) == 0, status, nil
}

func HasCommitsAhead(root, baseBranch, branch string) (bool, error) {
	cmd := exec.Command("git", "rev-list", "--count", baseBranch+".."+branch)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "0", nil
}

func DropPath(root, relPath string) error {
	cmd := exec.Command("git", "rm", "-f", "--ignore-unmatch", "--", relPath)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git rm failed for %s: %s", relPath, strings.TrimSpace(string(out)))
	}
	if err := os.Remove(filepath.Join(root, relPath)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func EnsureLocalExcludes(worktreePath string, patterns []string) error {
	gitDir, err := gitDirFor(worktreePath)
	if err != nil {
		return err
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0755); err != nil {
		return err
	}

	var existing string
	if data, err := os.ReadFile(excludePath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}

	var missing []string
	for _, pattern := range patterns {
		if !containsLine(existing, pattern) {
			missing = append(missing, pattern)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	updated := existing
	if updated != "" && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	if updated != "" {
		updated += "\n"
	}
	updated += "# conductor runtime\n"
	for _, pattern := range missing {
		updated += pattern + "\n"
	}

	return os.WriteFile(excludePath, []byte(updated), 0644)
}

func gitDirFor(worktreePath string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = worktreePath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to resolve git dir for %s: %w", worktreePath, err)
	}
	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}
	return gitDir, nil
}

func containsLine(contents, needle string) bool {
	start := 0
	for start <= len(contents) {
		end := start
		for end < len(contents) && contents[end] != '\n' {
			end++
		}
		if contents[start:end] == needle {
			return true
		}
		if end == len(contents) {
			break
		}
		start = end + 1
	}
	return false
}
