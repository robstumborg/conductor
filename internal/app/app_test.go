package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robstumborg/conductor/internal/config"
	"github.com/robstumborg/conductor/internal/work"
)

func TestRunStatusDoesNotCreateLayout(t *testing.T) {
	root := t.TempDir()

	origRepoRoot := repoRootFn
	origListSessions := listSessionsFn
	origWindowExists := windowExistsFn
	defer func() {
		repoRootFn = origRepoRoot
		listSessionsFn = origListSessions
		windowExistsFn = origWindowExists
	}()

	repoRootFn = func() (string, error) { return root, nil }
	listSessionsFn = func() ([]string, error) { return nil, nil }
	windowExistsFn = func(string, string) bool { return false }

	if err := runStatus(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, config.ConductDir)); !os.IsNotExist(err) {
		t.Fatalf("expected %s to remain absent, got %v", config.ConductDir, err)
	}
}

func TestResolveBaseStartPointPrefersLocalBranch(t *testing.T) {
	origBranchExists := branchExistsFn
	origRefExists := refExistsFn
	defer func() {
		branchExistsFn = origBranchExists
		refExistsFn = origRefExists
	}()

	branchExistsFn = func(string, string) bool { return true }
	refExistsFn = func(string, string) bool { return false }

	got, err := resolveBaseStartPoint("/tmp/project", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != "main" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveBaseStartPointFallsBackToOrigin(t *testing.T) {
	origBranchExists := branchExistsFn
	origRefExists := refExistsFn
	defer func() {
		branchExistsFn = origBranchExists
		refExistsFn = origRefExists
	}()

	branchExistsFn = func(string, string) bool { return false }
	refExistsFn = func(string, string) bool { return true }

	got, err := resolveBaseStartPoint("/tmp/project", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != "origin/main" {
		t.Fatalf("got %q", got)
	}
}

func TestRunWorkCreateValidatesModelBeforeEditorFlow(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("bad model")
	origRepoRoot := repoRootFn
	origModelValidate := modelpkgValidate
	defer func() {
		repoRootFn = origRepoRoot
		modelpkgValidate = origModelValidate
	}()

	repoRootFn = func() (string, error) { return root, nil }
	modelpkgValidate = func(string, string) error { return wantErr }

	err := runWorkCreate([]string{"--model", "bad/model"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want=%v", err, wantErr)
	}
}

func TestStartWorkItemRollsBackOnWindowFailure(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Test task"})

	wantErr := errors.New("create window failed")
	removedWorktree := false
	deletedBranch := false
	saved := false

	origBranchExists := branchExistsFn
	origRefExists := refExistsFn
	origCreateWorktree := createWorktreeFn
	origRemoveWorktree := removeWorktreeFn
	origDeleteBranch := deleteBranchFn
	origEnsureLocalExcludes := ensureLocalExcludesFn
	origSessionExists := sessionExistsFn
	origCreateSession := createSessionFn
	origWindowExists := windowExistsFn
	origCreateWindow := createWindowFn
	origSendKeys := sendKeysFn
	origOpenTmux := openTmuxFn
	origSaveWork := saveWorkFn
	origModelValidate := modelpkgValidate
	defer func() {
		branchExistsFn = origBranchExists
		refExistsFn = origRefExists
		createWorktreeFn = origCreateWorktree
		removeWorktreeFn = origRemoveWorktree
		deleteBranchFn = origDeleteBranch
		ensureLocalExcludesFn = origEnsureLocalExcludes
		sessionExistsFn = origSessionExists
		createSessionFn = origCreateSession
		windowExistsFn = origWindowExists
		createWindowFn = origCreateWindow
		sendKeysFn = origSendKeys
		openTmuxFn = origOpenTmux
		saveWorkFn = origSaveWork
		modelpkgValidate = origModelValidate
	}()

	branchExistsFn = func(_ string, branch string) bool { return branch == "main" }
	refExistsFn = func(string, string) bool { return false }
	createWorktreeFn = func(_ string, path, _ string, _ string, _ bool) error {
		return os.MkdirAll(path, 0755)
	}
	removeWorktreeFn = func(_ string, path string) error {
		removedWorktree = true
		return os.RemoveAll(path)
	}
	deleteBranchFn = func(string, string) error {
		deletedBranch = true
		return nil
	}
	ensureLocalExcludesFn = func(string, []string) error { return nil }
	sessionExistsFn = func(string) bool { return false }
	createSessionFn = func(string, string) error { return nil }
	windowExistsFn = func(string, string) bool { return false }
	createWindowFn = func(string, string, string) error { return wantErr }
	sendKeysFn = func(string, string) error { return nil }
	openTmuxFn = func(string) error { return nil }
	saveWorkFn = func(string, *work.Item, bool) error {
		saved = true
		return nil
	}
	modelpkgValidate = func(string, string) error { return nil }

	err := startWorkItem(root, item, "")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want=%v", err, wantErr)
	}
	if item.Status != "draft" {
		t.Fatalf("status=%q want draft", item.Status)
	}
	if item.Branch != "" {
		t.Fatalf("branch=%q want empty", item.Branch)
	}
	if saved {
		t.Fatal("expected work item not to be saved on startup failure")
	}
	if !removedWorktree {
		t.Fatal("expected created worktree to be removed")
	}
	if !deletedBranch {
		t.Fatal("expected created branch to be deleted")
	}
	if _, statErr := os.Stat(filepath.Join(root, config.WorktreesDir, item.WorktreeDir())); !os.IsNotExist(statErr) {
		t.Fatalf("expected rolled back worktree, got %v", statErr)
	}
}

func TestSyncCurrentRejectsInvalidCurrentFile(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Valid title"})
	item.EnsureBranch()
	if err := work.Save(root, item, false); err != nil {
		t.Fatal(err)
	}

	currentPath := filepath.Join(root, config.WorktreesDir, item.WorktreeDir(), config.CurrentWorkPath)
	if err := os.MkdirAll(filepath.Dir(currentPath), 0755); err != nil {
		t.Fatal(err)
	}
	invalid := "---\ntitle: \"\"\nstatus: in-progress\n---\n"
	if err := os.WriteFile(currentPath, []byte(invalid), 0644); err != nil {
		t.Fatal(err)
	}

	err := syncCurrent(root, item)
	if err == nil {
		t.Fatal("expected invalid current file error")
	}
}

func TestCompleteArgsRootCommandSuggestions(t *testing.T) {
	got := completeArgs([]string{"st"})
	want := []string{"start", "status"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestCompleteArgsNewFlags(t *testing.T) {
	got := completeArgs([]string{"new", "--s"})
	want := []string{"--scope", "--start"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestCompleteArgsNewFlagsPreferLongByDefault(t *testing.T) {
	got := completeArgs([]string{"new", ""})
	want := []string{"--accept", "--constraint", "--edit", "--model", "--scope", "--start", "--title"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestCompleteArgsNewFlagsSuggestShortAliasesForShortPrefix(t *testing.T) {
	got := completeArgs([]string{"new", "-"})
	want := []string{"-a", "-c", "-s", "-t"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestCompleteArgsStartSuggestsActiveIDs(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item1 := work.New(1, work.CreateOptions{Title: "First task"})
	item2 := work.New(12, work.CreateOptions{Title: "Second task"})
	if err := work.Save(root, item1, false); err != nil {
		t.Fatal(err)
	}
	if err := work.Save(root, item2, false); err != nil {
		t.Fatal(err)
	}

	origRepoRoot := repoRootFn
	defer func() { repoRootFn = origRepoRoot }()
	repoRootFn = func() (string, error) { return root, nil }

	got := completeArgs([]string{"start", ""})
	want := []string{"0001", "0012"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestCompleteArgsStartSuggestsFlagsAfterID(t *testing.T) {
	got := completeArgs([]string{"start", "1", "--"})
	want := []string{"--model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestRunCompletionBash(t *testing.T) {
	output := captureStdout(t, func() {
		if err := runCompletion([]string{"bash"}); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "__complete") {
		t.Fatalf("expected completion helper in output, got %q", output)
	}
	if !strings.Contains(output, "complete -F _conduct_completion conduct") {
		t.Fatalf("expected bash registration in output, got %q", output)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
