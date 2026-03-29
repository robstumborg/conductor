package app

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robstumborg/conductor/internal/config"
	"github.com/robstumborg/conductor/internal/git"
	"github.com/robstumborg/conductor/internal/notify"
	"github.com/robstumborg/conductor/internal/work"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func initGitRepo(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("init\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "init")
}

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

func TestBuildAgentCommandIncludesConductEnv(t *testing.T) {
	cmd := buildAgentCommand("/tmp/repo root", "conduct-project", config.Default(), work.New(1, work.CreateOptions{Title: "Test task"}), "build", "openai/gpt-5.4")
	if !strings.Contains(cmd, "CONDUCT_ROOT='/tmp/repo root'") {
		t.Fatalf("expected CONDUCT_ROOT in %q", cmd)
	}
	if !strings.Contains(cmd, "CONDUCT_SESSION_NAME=conduct-project") {
		t.Fatalf("expected session name in %q", cmd)
	}
	if !strings.Contains(cmd, "opencode") {
		t.Fatalf("expected agent command in %q", cmd)
	}
	if !strings.Contains(cmd, "--agent build") {
		t.Fatalf("expected agent flag in %q", cmd)
	}
}

func TestRunNotifyWritesNotificationLog(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}

	origRepoRoot := repoRootFn
	defer func() {
		repoRootFn = origRepoRoot
		_ = os.Unsetenv("CONDUCT_ROOT")
		_ = os.Unsetenv("CONDUCT_SESSION_NAME")
	}()
	repoRootFn = func() (string, error) { return root, nil }
	if err := os.Setenv("CONDUCT_ROOT", root); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("CONDUCT_SESSION_NAME", "conduct-project"); err != nil {
		t.Fatal(err)
	}

	if err := runNotify([]string{"--event", "question", "--message", "Need an answer", "--task", "0001"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, config.ConductDir, "notifications.log"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(data)
	if !strings.Contains(contents, "question") || !strings.Contains(contents, "Need an answer") || !strings.Contains(contents, "task=0001") {
		t.Fatalf("unexpected notification log contents %q", contents)
	}
}

func TestRunDoctorReportsOpencodePlugin(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(root, ".opencode", "plugins", "conductor-notify.js")
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte("export default {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origRepoRoot := repoRootFn
	defer func() {
		repoRootFn = origRepoRoot
	}()
	repoRootFn = func() (string, error) { return root, nil }

	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = stdout
	}()

	if err := runDoctor(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if !strings.Contains(output, "opencode plugin: ok ("+pluginPath+")") {
		t.Fatalf("expected plugin status in output, got %q", output)
	}
	if _, err := exec.LookPath("opencode"); err == nil && !strings.Contains(output, "agent command: ok (opencode)") {
		t.Fatalf("expected doctor output to include agent command status, got %q", output)
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
	origAgentValidate := agentpkgValidate
	origModelValidate := modelpkgValidate
	defer func() {
		repoRootFn = origRepoRoot
		agentpkgValidate = origAgentValidate
		modelpkgValidate = origModelValidate
	}()

	repoRootFn = func() (string, error) { return root, nil }
	agentpkgValidate = func(string, string) error { return nil }
	modelpkgValidate = func(string, string) error { return wantErr }

	err := runWorkCreate([]string{"--model", "bad/model"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want=%v", err, wantErr)
	}
}

func TestRunWorkCreateDraftPrefillsDefaultAgentAndModel(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}

	origRepoRoot := repoRootFn
	origOpenEditor := openEditorFn
	defer func() {
		repoRootFn = origRepoRoot
		openEditorFn = origOpenEditor
	}()

	repoRootFn = func() (string, error) { return root, nil }
	openEditorFn = func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		contents := string(data)
		if !strings.Contains(contents, "agent: build") {
			t.Fatalf("expected default agent in draft, got %q", contents)
		}
		if !strings.Contains(contents, "model: openai/gpt-5.4") {
			t.Fatalf("expected default model in draft, got %q", contents)
		}
		contents = strings.Replace(contents, "title: \"\"", "title: Draft task", 1)
		return os.WriteFile(path, []byte(contents), 0644)
	}

	if err := runWorkCreate(nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunWorkCreateDraftAllowsDescriptionOnly(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}

	origRepoRoot := repoRootFn
	origOpenEditor := openEditorFn
	defer func() {
		repoRootFn = origRepoRoot
		openEditorFn = origOpenEditor
	}()

	repoRootFn = func() (string, error) { return root, nil }
	openEditorFn = func(path string) error {
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := strings.Replace(string(contents), "## Description\n", "## Description\n\nUse the description as the task request.\n", 1)
		return os.WriteFile(path, []byte(updated), 0644)
	}

	if err := runWorkCreate(nil); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(root, config.ActiveWorkDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%d want 1", len(entries))
	}
	if got := entries[0].Name(); got != "0001-work-0001.md" {
		t.Fatalf("name=%q", got)
	}
	item, err := work.Parse(filepath.Join(root, config.ActiveWorkDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "" {
		t.Fatalf("title=%q want empty", item.Title)
	}
	if !strings.Contains(item.Body, "Use the description as the task request.") {
		t.Fatalf("body=%q", item.Body)
	}
}

func TestRunWorkCreateDraftRejectsMissingTitleAndDescription(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}

	origRepoRoot := repoRootFn
	origOpenEditor := openEditorFn
	defer func() {
		repoRootFn = origRepoRoot
		openEditorFn = origOpenEditor
	}()

	repoRootFn = func() (string, error) { return root, nil }
	openEditorFn = func(path string) error {
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := strings.Replace(string(contents), "## Description\n", "## Description\n\n", 1)
		return os.WriteFile(path, []byte(updated), 0644)
	}

	err := runWorkCreate(nil)
	if err == nil || err.Error() != "aborted: title or description is required" {
		t.Fatalf("err=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, config.ActiveWorkDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries=%d want 0", len(entries))
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
	origAgentValidate := agentpkgValidate
	origModelValidate := modelpkgValidate
	origNotifyDispatch := notifyDispatchFn
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
		agentpkgValidate = origAgentValidate
		modelpkgValidate = origModelValidate
		notifyDispatchFn = origNotifyDispatch
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
	agentpkgValidate = func(string, string) error { return nil }
	modelpkgValidate = func(string, string) error { return nil }

	err := startWorkItem(root, item, "", "")
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

func TestStartWorkItemSyncsOpencodeIntoWorktree(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(root, ".opencode", "plugins", "conductor-notify.js")
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0755); err != nil {
		t.Fatal(err)
	}
	const pluginBody = "export default {}\n"
	if err := os.WriteFile(pluginPath, []byte(pluginBody), 0644); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Test task"})

	origBranchExists := branchExistsFn
	origRefExists := refExistsFn
	origCreateWorktree := createWorktreeFn
	origEnsureLocalExcludes := ensureLocalExcludesFn
	origSessionExists := sessionExistsFn
	origCreateSession := createSessionFn
	origWindowExists := windowExistsFn
	origCreateWindow := createWindowFn
	origSendKeys := sendKeysFn
	origOpenTmux := openTmuxFn
	origSaveWork := saveWorkFn
	origAgentValidate := agentpkgValidate
	origModelValidate := modelpkgValidate
	defer func() {
		branchExistsFn = origBranchExists
		refExistsFn = origRefExists
		createWorktreeFn = origCreateWorktree
		ensureLocalExcludesFn = origEnsureLocalExcludes
		sessionExistsFn = origSessionExists
		createSessionFn = origCreateSession
		windowExistsFn = origWindowExists
		createWindowFn = origCreateWindow
		sendKeysFn = origSendKeys
		openTmuxFn = origOpenTmux
		saveWorkFn = origSaveWork
		agentpkgValidate = origAgentValidate
		modelpkgValidate = origModelValidate
	}()

	branchExistsFn = func(_ string, branch string) bool { return branch == "main" }
	refExistsFn = func(string, string) bool { return false }
	createWorktreeFn = func(_ string, path, _ string, _ string, _ bool) error {
		return os.MkdirAll(path, 0755)
	}
	ensureLocalExcludesFn = func(string, []string) error { return nil }
	sessionExistsFn = func(string) bool { return true }
	createSessionFn = func(string, string) error { return nil }
	windowExistsFn = func(string, string) bool { return false }
	createWindowFn = func(string, string, string) error { return nil }
	sendKeysFn = func(string, string) error { return nil }
	openTmuxFn = func(string) error { return nil }
	saveWorkFn = func(string, *work.Item, bool) error { return nil }
	agentpkgValidate = func(string, string) error { return nil }
	modelpkgValidate = func(string, string) error { return nil }
	notifyDispatchFn = func(string, string, *config.Config, notify.Event) error { return nil }

	if err := startWorkItem(root, item, "", ""); err != nil {
		t.Fatal(err)
	}
	worktreePluginPath := filepath.Join(root, config.WorktreesDir, item.WorktreeDir(), ".opencode", "plugins", "conductor-notify.js")
	data, err := os.ReadFile(worktreePluginPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != pluginBody {
		t.Fatalf("unexpected plugin contents %q", string(data))
	}
}

func TestStartWorkItemWarnsWhenNotificationSetupFails(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Test task"})

	wantErr := errors.New("notification setup failed")
	sentKeys := false
	saved := false

	origBranchExists := branchExistsFn
	origRefExists := refExistsFn
	origCreateWorktree := createWorktreeFn
	origEnsureLocalExcludes := ensureLocalExcludesFn
	origSessionExists := sessionExistsFn
	origCreateSession := createSessionFn
	origWindowExists := windowExistsFn
	origCreateWindow := createWindowFn
	origSendKeys := sendKeysFn
	origOpenTmux := openTmuxFn
	origSaveWork := saveWorkFn
	origAgentValidate := agentpkgValidate
	origModelValidate := modelpkgValidate
	origNotifyDispatch := notifyDispatchFn
	defer func() {
		branchExistsFn = origBranchExists
		refExistsFn = origRefExists
		createWorktreeFn = origCreateWorktree
		ensureLocalExcludesFn = origEnsureLocalExcludes
		sessionExistsFn = origSessionExists
		createSessionFn = origCreateSession
		windowExistsFn = origWindowExists
		createWindowFn = origCreateWindow
		sendKeysFn = origSendKeys
		openTmuxFn = origOpenTmux
		saveWorkFn = origSaveWork
		agentpkgValidate = origAgentValidate
		modelpkgValidate = origModelValidate
		notifyDispatchFn = origNotifyDispatch
	}()

	branchExistsFn = func(_ string, branch string) bool { return branch == "main" }
	refExistsFn = func(string, string) bool { return false }
	createWorktreeFn = func(_ string, path, _ string, _ string, _ bool) error {
		return os.MkdirAll(path, 0755)
	}
	ensureLocalExcludesFn = func(string, []string) error { return nil }
	sessionExistsFn = func(string) bool { return true }
	createSessionFn = func(string, string) error { return nil }
	windowExistsFn = func(string, string) bool { return false }
	createWindowFn = func(string, string, string) error { return nil }
	sendKeysFn = func(string, string) error {
		sentKeys = true
		return nil
	}
	openTmuxFn = func(string) error { return nil }
	saveWorkFn = func(string, *work.Item, bool) error {
		saved = true
		return nil
	}
	agentpkgValidate = func(string, string) error { return nil }
	modelpkgValidate = func(string, string) error { return nil }
	notifyDispatchFn = func(string, string, *config.Config, notify.Event) error { return wantErr }

	stderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = stderr
	}()

	if err := startWorkItem(root, item, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if !strings.Contains(output, "warning: notification setup failed: "+wantErr.Error()) {
		t.Fatalf("expected warning in stderr, got %q", output)
	}
	if !sentKeys {
		t.Fatal("expected agent command to still start")
	}
	if !saved {
		t.Fatal("expected work item to still be saved")
	}
	if item.Status != "in-progress" {
		t.Fatalf("status=%q want in-progress", item.Status)
	}
}

func TestStartWorkItemRejectsExistingNonGitDirectory(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Test task"})
	item.EnsureBranch()
	worktreePath := filepath.Join(root, config.WorktreesDir, item.WorktreeDir())
	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatal(err)
	}

	origEnsureLocalExcludes := ensureLocalExcludesFn
	origSessionExists := sessionExistsFn
	origCreateSession := createSessionFn
	origWindowExists := windowExistsFn
	origCreateWindow := createWindowFn
	origSendKeys := sendKeysFn
	origOpenTmux := openTmuxFn
	origSaveWork := saveWorkFn
	origAgentValidate := agentpkgValidate
	origModelValidate := modelpkgValidate
	origNotifyDispatch := notifyDispatchFn
	defer func() {
		ensureLocalExcludesFn = origEnsureLocalExcludes
		sessionExistsFn = origSessionExists
		createSessionFn = origCreateSession
		windowExistsFn = origWindowExists
		createWindowFn = origCreateWindow
		sendKeysFn = origSendKeys
		openTmuxFn = origOpenTmux
		saveWorkFn = origSaveWork
		agentpkgValidate = origAgentValidate
		modelpkgValidate = origModelValidate
		notifyDispatchFn = origNotifyDispatch
	}()

	ensureLocalExcludesFn = func(string, []string) error { return nil }
	sessionExistsFn = func(string) bool { return true }
	createSessionFn = func(string, string) error { return nil }
	windowExistsFn = func(string, string) bool { return true }
	createWindowFn = func(string, string, string) error { return nil }
	sendKeysFn = func(string, string) error { return nil }
	openTmuxFn = func(string) error { return nil }
	saveWorkFn = func(string, *work.Item, bool) error { return nil }
	agentpkgValidate = func(string, string) error { return nil }
	modelpkgValidate = func(string, string) error { return nil }

	err := startWorkItem(root, item, "", "")
	if err == nil || !strings.Contains(err.Error(), "resolves to git root") {
		t.Fatalf("expected non-git worktree error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(worktreePath, config.CurrentWorkPath)); !os.IsNotExist(statErr) {
		t.Fatalf("expected current work file to remain absent, got %v", statErr)
	}
}

func TestStartWorkItemRejectsExistingWorktreeOnWrongBranch(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Test task"})
	item.EnsureBranch()
	worktreePath := filepath.Join(root, config.WorktreesDir, item.WorktreeDir())
	runGit(t, root, "worktree", "add", worktreePath, "-b", "conduct/9999-other")

	origEnsureLocalExcludes := ensureLocalExcludesFn
	origSessionExists := sessionExistsFn
	origCreateSession := createSessionFn
	origWindowExists := windowExistsFn
	origCreateWindow := createWindowFn
	origSendKeys := sendKeysFn
	origOpenTmux := openTmuxFn
	origSaveWork := saveWorkFn
	origAgentValidate := agentpkgValidate
	origModelValidate := modelpkgValidate
	defer func() {
		ensureLocalExcludesFn = origEnsureLocalExcludes
		sessionExistsFn = origSessionExists
		createSessionFn = origCreateSession
		windowExistsFn = origWindowExists
		createWindowFn = origCreateWindow
		sendKeysFn = origSendKeys
		openTmuxFn = origOpenTmux
		saveWorkFn = origSaveWork
		agentpkgValidate = origAgentValidate
		modelpkgValidate = origModelValidate
	}()

	ensureLocalExcludesFn = func(string, []string) error { return nil }
	sessionExistsFn = func(string) bool { return true }
	createSessionFn = func(string, string) error { return nil }
	windowExistsFn = func(string, string) bool { return true }
	createWindowFn = func(string, string, string) error { return nil }
	sendKeysFn = func(string, string) error { return nil }
	openTmuxFn = func(string) error { return nil }
	saveWorkFn = func(string, *work.Item, bool) error { return nil }
	agentpkgValidate = func(string, string) error { return nil }
	modelpkgValidate = func(string, string) error { return nil }

	err := startWorkItem(root, item, "", "")
	if err == nil || !strings.Contains(err.Error(), "expected \""+item.Branch+"\"") {
		t.Fatalf("expected wrong-branch error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(worktreePath, config.CurrentWorkPath)); !os.IsNotExist(statErr) {
		t.Fatalf("expected current work file to remain absent, got %v", statErr)
	}
}

func TestStartWorkItemAcceptsExistingMatchingWorktree(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Test task"})
	item.EnsureBranch()
	worktreePath := filepath.Join(root, config.WorktreesDir, item.WorktreeDir())
	runGit(t, root, "worktree", "add", worktreePath, "-b", item.Branch)

	origEnsureLocalExcludes := ensureLocalExcludesFn
	origSessionExists := sessionExistsFn
	origCreateSession := createSessionFn
	origWindowExists := windowExistsFn
	origCreateWindow := createWindowFn
	origSendKeys := sendKeysFn
	origOpenTmux := openTmuxFn
	origSaveWork := saveWorkFn
	origAgentValidate := agentpkgValidate
	origModelValidate := modelpkgValidate
	defer func() {
		ensureLocalExcludesFn = origEnsureLocalExcludes
		sessionExistsFn = origSessionExists
		createSessionFn = origCreateSession
		windowExistsFn = origWindowExists
		createWindowFn = origCreateWindow
		sendKeysFn = origSendKeys
		openTmuxFn = origOpenTmux
		saveWorkFn = origSaveWork
		agentpkgValidate = origAgentValidate
		modelpkgValidate = origModelValidate
	}()

	ensureLocalExcludesFn = func(string, []string) error { return nil }
	sessionExistsFn = func(string) bool { return true }
	createSessionFn = func(string, string) error { return nil }
	windowExistsFn = func(string, string) bool { return false }
	createWindowFn = func(string, string, string) error { return nil }
	sendKeysFn = func(string, string) error { return nil }
	openTmuxFn = func(string) error { return nil }
	saveWorkFn = func(string, *work.Item, bool) error { return nil }
	agentpkgValidate = func(string, string) error { return nil }
	modelpkgValidate = func(string, string) error { return nil }
	notifyDispatchFn = func(string, string, *config.Config, notify.Event) error { return nil }

	if err := startWorkItem(root, item, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(worktreePath, config.CurrentWorkPath)); err != nil {
		t.Fatalf("expected current work file to be written, got %v", err)
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

func TestSyncCurrentRejectsBranchMismatch(t *testing.T) {
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
	tampered := "---\nid: 1\ntitle: Valid title\nstatus: in-progress\nbranch: conduct/9999-other\ncreated_at: \"2026-03-29T00:00:00Z\"\nupdated_at: \"2026-03-29T00:00:00Z\"\n---\n\n## Description\n"
	if err := os.WriteFile(currentPath, []byte(tampered), 0644); err != nil {
		t.Fatal(err)
	}

	err := syncCurrent(root, item)
	if err == nil || !strings.Contains(err.Error(), "branch mismatch") {
		t.Fatalf("expected branch mismatch error, got %v", err)
	}
}

func TestSyncCurrentOnlyAppliesEditableFields(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Original title"})
	item.Status = "in-progress"
	item.Model = "openai/gpt-5.4"
	item.Scope = []string{"old scope"}
	item.Accept = []string{"old accept"}
	item.Constraints = []string{"old constraint"}
	item.Body = "## Description\n\nOld body\n"
	item.EnsureBranch()
	if err := work.Save(root, item, false); err != nil {
		t.Fatal(err)
	}

	currentPath := filepath.Join(root, config.WorktreesDir, item.WorktreeDir(), config.CurrentWorkPath)
	if err := os.MkdirAll(filepath.Dir(currentPath), 0755); err != nil {
		t.Fatal(err)
	}
	updated := "---\nid: 1\ntitle: Changed title\nstatus: landed\nmodel: anthropic/claude\nbranch: " + item.Branch + "\nscope:\n  - new scope\naccept:\n  - new accept\nconstraints:\n  - new constraint\ncreated_at: \"2026-03-29T00:00:00Z\"\nupdated_at: \"2026-03-29T00:00:00Z\"\n---\n\n## Description\n\nNew body\n"
	if err := os.WriteFile(currentPath, []byte(updated), 0644); err != nil {
		t.Fatal(err)
	}

	if err := syncCurrent(root, item); err != nil {
		t.Fatal(err)
	}

	if item.Title != "Original title" {
		t.Fatalf("title=%q want original", item.Title)
	}
	if item.Status != "in-progress" {
		t.Fatalf("status=%q want in-progress", item.Status)
	}
	if item.Model != "openai/gpt-5.4" {
		t.Fatalf("model=%q want original", item.Model)
	}
	if item.Branch == "conduct/9999-other" || item.Branch == "" {
		t.Fatalf("branch=%q want original branch", item.Branch)
	}
	if !reflect.DeepEqual(item.Scope, []string{"new scope"}) {
		t.Fatalf("scope=%v", item.Scope)
	}
	if !reflect.DeepEqual(item.Accept, []string{"new accept"}) {
		t.Fatalf("accept=%v", item.Accept)
	}
	if !reflect.DeepEqual(item.Constraints, []string{"new constraint"}) {
		t.Fatalf("constraints=%v", item.Constraints)
	}
	if !strings.Contains(item.Body, "New body") {
		t.Fatalf("body=%q", item.Body)
	}

	saved, err := work.Parse(item.Path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Title != "Original title" {
		t.Fatalf("saved title=%q want original", saved.Title)
	}
	if saved.Status != "in-progress" {
		t.Fatalf("saved status=%q want in-progress", saved.Status)
	}
	if saved.Model != "openai/gpt-5.4" {
		t.Fatalf("saved model=%q want original", saved.Model)
	}
	if saved.Branch != item.Branch {
		t.Fatalf("saved branch=%q want %q", saved.Branch, item.Branch)
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
	want := []string{"--accept", "--agent", "--constraint", "--edit", "--model", "--scope", "--start", "--title"}
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
	want := []string{"--agent", "--model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestRunWorkCreateValidatesAgentBeforeEditorFlow(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("bad agent")
	origRepoRoot := repoRootFn
	origAgentValidate := agentpkgValidate
	defer func() {
		repoRootFn = origRepoRoot
		agentpkgValidate = origAgentValidate
	}()

	repoRootFn = func() (string, error) { return root, nil }
	agentpkgValidate = func(string, string) error { return wantErr }

	err := runWorkCreate([]string{"--agent", "bad-agent"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want=%v", err, wantErr)
	}
}

func TestRunWorkStartValidatesAgentOverride(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Test task"})
	if err := work.Save(root, item, false); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("bad agent")
	origRepoRoot := repoRootFn
	origAgentValidate := agentpkgValidate
	defer func() {
		repoRootFn = origRepoRoot
		agentpkgValidate = origAgentValidate
	}()

	repoRootFn = func() (string, error) { return root, nil }
	agentpkgValidate = func(string, string) error { return wantErr }

	err := runWorkStart("1", []string{"--agent", "missing"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want=%v", err, wantErr)
	}
}

func TestDropConfirmationMessageWarnsForDirtyAheadWork(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Risky task"})
	item.EnsureBranch()
	if err := os.MkdirAll(filepath.Join(root, config.WorktreesDir, item.WorktreeDir()), 0755); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	origIsClean := isCleanFn
	origHasCommitsAhead := hasCommitsAheadFn
	defer func() {
		isCleanFn = origIsClean
		hasCommitsAheadFn = origHasCommitsAhead
	}()

	isCleanFn = func(string) (bool, []git.FileStatus, error) {
		return false, []git.FileStatus{{XY: " M", Path: "notes.txt"}}, nil
	}
	hasCommitsAheadFn = func(string, string, string) (bool, error) { return true, nil }

	message, err := dropConfirmationMessage(root, cfg, item)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "WARNING: dropping work item 1 (Risky task) will permanently remove local task state.") {
		t.Fatalf("missing warning in %q", message)
	}
	if !strings.Contains(message, "Uncommitted changes will be lost:  M notes.txt.") {
		t.Fatalf("missing dirty status in %q", message)
	}
	if !strings.Contains(message, "has commits ahead of main that will be deleted") {
		t.Fatalf("missing ahead warning in %q", message)
	}
	if !strings.Contains(message, "Continue anyway?") {
		t.Fatalf("missing continue prompt in %q", message)
	}
}

func TestRunWorkDropPromptsBeforeRiskyCleanup(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Risky task"})
	item.EnsureBranch()
	if err := work.Save(root, item, false); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, config.WorktreesDir, item.WorktreeDir()), 0755); err != nil {
		t.Fatal(err)
	}

	origRepoRoot := repoRootFn
	origConfirmPrompt := confirmPromptFn
	origIsClean := isCleanFn
	origHasCommitsAhead := hasCommitsAheadFn
	defer func() {
		repoRootFn = origRepoRoot
		confirmPromptFn = origConfirmPrompt
		isCleanFn = origIsClean
		hasCommitsAheadFn = origHasCommitsAhead
	}()

	repoRootFn = func() (string, error) { return root, nil }
	isCleanFn = func(string) (bool, []git.FileStatus, error) {
		return false, []git.FileStatus{{XY: " M", Path: "notes.txt"}}, nil
	}
	hasCommitsAheadFn = func(string, string, string) (bool, error) { return true, nil }

	var prompt string
	var defaultYes bool
	confirmPromptFn = func(message string, yes bool) bool {
		prompt = message
		defaultYes = yes
		return false
	}

	if err := runWorkDrop("1"); err != nil {
		t.Fatal(err)
	}
	if !defaultYes {
		t.Fatal("expected risky drop prompt to default to yes")
	}
	if !strings.Contains(prompt, "WARNING: dropping work item 1 (Risky task)") {
		t.Fatalf("missing risky warning in %q", prompt)
	}
	if _, err := os.Stat(item.Path); err != nil {
		t.Fatalf("expected active work item to remain after declined drop, got %v", err)
	}
}

func TestRunWorkDropReturnsCleanupErrors(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Cleanup task"})
	item.EnsureBranch()
	if err := work.Save(root, item, false); err != nil {
		t.Fatal(err)
	}

	origRepoRoot := repoRootFn
	origConfirmPrompt := confirmPromptFn
	origIsClean := isCleanFn
	origHasCommitsAhead := hasCommitsAheadFn
	origKillWindow := killWindowFn
	origCleanupRemoveWorktreeForce := cleanupRemoveWorktreeForceFn
	origCleanupDeleteBranchForce := cleanupDeleteBranchForceFn
	defer func() {
		repoRootFn = origRepoRoot
		confirmPromptFn = origConfirmPrompt
		isCleanFn = origIsClean
		hasCommitsAheadFn = origHasCommitsAhead
		killWindowFn = origKillWindow
		cleanupRemoveWorktreeForceFn = origCleanupRemoveWorktreeForce
		cleanupDeleteBranchForceFn = origCleanupDeleteBranchForce
	}()

	repoRootFn = func() (string, error) { return root, nil }
	confirmPromptFn = func(string, bool) bool { return true }
	isCleanFn = func(string) (bool, []git.FileStatus, error) { return true, nil, nil }
	hasCommitsAheadFn = func(string, string, string) (bool, error) { return false, nil }
	killWindowFn = func(string) error { return errors.New("tmux failed") }
	cleanupRemoveWorktreeForceFn = func(string, string) error { return errors.New("worktree failed") }
	cleanupDeleteBranchForceFn = func(string, string) error { return nil }

	err := runWorkDrop("1")
	if err == nil || !strings.Contains(err.Error(), "cleanup failed") || !strings.Contains(err.Error(), "worktree failed") {
		t.Fatalf("expected cleanup error, got %v", err)
	}
	if _, statErr := os.Stat(item.Path); statErr != nil {
		t.Fatalf("expected active work item to remain after failed cleanup, got %v", statErr)
	}
	archivedPath := filepath.Join(root, config.ArchiveWorkDir, item.Filename())
	if _, statErr := os.Stat(archivedPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no archived work item after failed cleanup, got %v", statErr)
	}
}

func TestCleanupWorkReturnsJoinedErrors(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Cleanup task"})
	item.EnsureBranch()

	origKillWindow := killWindowFn
	origCleanupRemoveWorktree := cleanupRemoveWorktreeFn
	origCleanupDeleteBranch := cleanupDeleteBranchFn
	defer func() {
		killWindowFn = origKillWindow
		cleanupRemoveWorktreeFn = origCleanupRemoveWorktree
		cleanupDeleteBranchFn = origCleanupDeleteBranch
	}()

	killWindowFn = func(string) error { return errors.New("window failed") }
	cleanupRemoveWorktreeFn = func(string, string) error { return errors.New("worktree failed") }
	cleanupDeleteBranchFn = func(string, string) error { return errors.New("branch failed") }

	err := cleanupWork(root, item, cleanupOptions{})
	if err == nil {
		t.Fatal("expected cleanup error")
	}
	for _, want := range []string{"worktree failed", "branch failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "window failed") {
		t.Fatalf("expected tmux window failures to be ignored, got %v", err)
	}
}

func TestDropConfirmationMessageNotesAlreadyCleanWorktree(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := work.New(1, work.CreateOptions{Title: "Cleanup task"})
	item.EnsureBranch()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}

	origIsClean := isCleanFn
	origHasCommitsAhead := hasCommitsAheadFn
	defer func() {
		isCleanFn = origIsClean
		hasCommitsAheadFn = origHasCommitsAhead
	}()

	isCleanFn = func(string) (bool, []git.FileStatus, error) {
		return false, nil, errors.New("missing worktree")
	}
	hasCommitsAheadFn = func(string, string, string) (bool, error) { return false, nil }

	message, err := dropConfirmationMessage(root, cfg, item)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "WARNING: dropping work item 1 (Cleanup task)") {
		t.Fatalf("missing warning in %q", message)
	}
	if !strings.Contains(message, "could not be inspected; it may already be clean") {
		t.Fatalf("missing already-clean note in %q", message)
	}
	if strings.Contains(message, "Uncommitted changes will be lost") {
		t.Fatalf("did not expect dirty warning in %q", message)
	}
}

func TestConfirmPromptDefaultYesAcceptsEnter(t *testing.T) {
	origStdin := os.Stdin
	origStdout := os.Stdout
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = inR
	os.Stdout = outW
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	}()

	if _, err := inW.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	if err := inW.Close(); err != nil {
		t.Fatal(err)
	}

	if !confirmPrompt("Continue?", true) {
		t.Fatal("expected empty input to accept default-yes prompt")
	}
	if err := outW.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(outR)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[Y/n]:") {
		t.Fatalf("expected Y/n prompt, got %q", string(data))
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
