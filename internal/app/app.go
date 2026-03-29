package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	agentpkg "github.com/robstumborg/conductor/internal/agent"
	"github.com/robstumborg/conductor/internal/config"
	"github.com/robstumborg/conductor/internal/editor"
	"github.com/robstumborg/conductor/internal/git"
	"github.com/robstumborg/conductor/internal/model"
	"github.com/robstumborg/conductor/internal/notify"
	"github.com/robstumborg/conductor/internal/tmux"
	"github.com/robstumborg/conductor/internal/work"
)

var version = "dev"
var buildCommit = "dev"
var buildDate = "unknown"

var repoRootFn = git.RepoRoot
var ensureLayoutFn = config.EnsureLayout
var branchExistsFn = git.BranchExists
var refExistsFn = git.RefExists
var createWorktreeFn = git.CreateWorktree
var removeWorktreeFn = git.RemoveWorktree
var deleteBranchFn = git.DeleteBranch
var ensureLocalExcludesFn = git.EnsureLocalExcludes
var sessionExistsFn = tmux.SessionExists
var createSessionFn = tmux.CreateSession
var windowExistsFn = tmux.WindowExists
var createWindowFn = tmux.CreateWindow
var sendKeysFn = tmux.SendKeys
var openTmuxFn = tmux.Open
var killSessionFn = tmux.KillSession
var killWindowFn = tmux.KillWindow
var listSessionsFn = tmux.ListSessions
var saveWorkFn = work.Save
var openEditorFn = editor.Open
var agentpkgValidate = agentpkg.ValidateAvailable
var modelpkgValidate = model.ValidateAvailable
var notifyDispatchFn = notify.Dispatch

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func Run(args []string) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}

	switch args[0] {
	case "__complete":
		return runComplete(args[1:])
	case "completion":
		return runCompletion(args[1:])
	case "init":
		return runInit()
	case "new":
		return runWorkCreate(args[1:])
	case "list":
		return runWorkList()
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("usage: conduct show <id>")
		}
		return runWorkShow(args[1])
	case "edit":
		if len(args) < 2 {
			return fmt.Errorf("usage: conduct edit <id>")
		}
		return runWorkEdit(args[1])
	case "start":
		if len(args) < 2 {
			return fmt.Errorf("usage: conduct start <id>")
		}
		return runWorkStart(args[1], args[2:])
	case "open":
		if len(args) < 2 {
			return fmt.Errorf("usage: conduct open <id>")
		}
		return runWorkOpen(args[1])
	case "land":
		if len(args) < 2 {
			return fmt.Errorf("usage: conduct land <id>")
		}
		return runWorkLand(args[1])
	case "drop":
		if len(args) < 2 {
			return fmt.Errorf("usage: conduct drop <id>")
		}
		return runWorkDrop(args[1])
	case "status":
		return runStatus()
	case "config":
		return runConfig(args[1:])
	case "doctor":
		return runDoctor()
	case "notify":
		return runNotify(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("build commit: %s\n", buildCommit)
		fmt.Printf("build date: %s\n", buildDate)
		return nil
	case "help", "--help", "-h":
		printHelp()
		return nil
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runInit() error {
	root, err := git.RepoRoot()
	if err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}
	configPath := filepath.Join(root, config.ConfigPath)
	_, configStatErr := os.Stat(configPath)
	configCreated := false
	if err := config.EnsureLayout(root); err != nil {
		return err
	}
	if os.IsNotExist(configStatErr) {
		cfg := config.Default()
		if branch, err := git.DetectMainBranch(root); err == nil {
			cfg.Project.MainBranch = branch
		}
		if err := config.Save(root, cfg); err != nil {
			return err
		}
		configCreated = true
	}

	if configCreated {
		fmt.Printf("Initialized conductor in %s\n", root)
		fmt.Println()
		fmt.Println("Runtime files are ignored under .conduct/worktrees/ and .conduct/current.md.")
		fmt.Println()
		fmt.Printf("Configured main branch: %s\n", cfgMainBranch(root))
		fmt.Println()
		fmt.Println("Next: review and commit the new .conduct/ project files before starting work.")
		return nil
	}

	fmt.Printf("conductor is already initialized in %s\n", root)
	return nil
}

func runWorkCreate(args []string) error {
	root, err := repoReady()
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("work", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var title, agent, model string
	var editFlag, startFlag bool
	var accept, constraints, scope stringList
	fs.StringVar(&title, "title", "", "work title")
	fs.StringVar(&title, "t", "", "work title")
	fs.StringVar(&agent, "agent", "", "initial agent override")
	fs.StringVar(&model, "model", "", "model override")
	fs.BoolVar(&editFlag, "edit", false, "open in editor before saving")
	fs.BoolVar(&startFlag, "start", false, "start after creation")
	fs.Var(&accept, "accept", "acceptance criterion")
	fs.Var(&accept, "a", "acceptance criterion")
	fs.Var(&constraints, "constraint", "constraint")
	fs.Var(&constraints, "c", "constraint")
	fs.Var(&scope, "scope", "scope")
	fs.Var(&scope, "s", "scope")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if agent != "" {
		if err := agentpkgValidate(cfg.Agent.Command, agent); err != nil {
			return err
		}
	}
	if model != "" {
		if err := modelpkgValidate(cfg.Agent.Command, model); err != nil {
			return err
		}
	}

	id, err := work.NextID(root)
	if err != nil {
		return err
	}

	insertBody := title == "" || editFlag
	item := work.New(id, work.CreateOptions{
		Title:       title,
		Agent:       agent,
		Model:       model,
		Scope:       scope,
		Accept:      accept,
		Constraints: constraints,
		InsertBody:  insertBody,
	})

	if title == "" {
		if err := ensureLayoutFn(root); err != nil {
			return err
		}
		if strings.TrimSpace(item.Agent) == "" {
			item.Agent = cfg.Agent.DefaultAgent
		}
		if strings.TrimSpace(item.Model) == "" {
			item.Model = cfg.Agent.DefaultModel
		}
		item.Path = filepath.Join(root, config.ActiveWorkDir, fmt.Sprintf("%s-draft.md", item.PaddedID()))
		data, err := item.Marshal()
		if err != nil {
			return err
		}
		if err := os.WriteFile(item.Path, data, 0644); err != nil {
			return err
		}
		if err := openEditorFn(item.Path); err != nil {
			_ = os.Remove(item.Path)
			return err
		}
		edited, err := work.Parse(item.Path)
		if err != nil {
			_ = os.Remove(item.Path)
			return err
		}
		if strings.TrimSpace(edited.Title) == "" {
			_ = os.Remove(item.Path)
			return errors.New("aborted: title is required")
		}
		oldPath := edited.Path
		edited.Path = ""
		if err := saveWorkFn(root, edited, false); err != nil {
			_ = os.Remove(oldPath)
			return err
		}
		if oldPath != edited.Path {
			_ = os.Remove(oldPath)
		}
		item = edited
	} else {
		if err := saveWorkFn(root, item, false); err != nil {
			return err
		}
		if editFlag {
			if err := runWorkEditByItem(item); err != nil {
				return err
			}
			item, err = work.Parse(item.Path)
			if err != nil {
				return err
			}
		}
	}

	fmt.Printf("Created %s\n", filepath.Base(item.Path))
	if startFlag {
		return startWorkItem(root, item, "", "")
	}
	return nil
}

func runWorkList() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	active, archive, err := work.List(root)
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	projectSession := projectSessionName(root, cfg)
	sessions, _ := listSessionsFn()
	sessionSet := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		sessionSet[s] = true
	}

	printItems("ACTIVE", active, sessionSet, projectSession)
	if len(archive) > 0 {
		fmt.Println()
		printItems("ARCHIVE", archive, sessionSet, projectSession)
	}
	return nil
}

func runWorkShow(id string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	item, err := work.Find(root, id)
	if err != nil {
		return err
	}
	showItem(root, item)
	return nil
}

func runWorkEdit(id string) error {
	root, err := repoReady()
	if err != nil {
		return err
	}
	item, err := work.FindActive(root, id)
	if err != nil {
		return err
	}
	return runWorkEditByItem(item)
}

func runWorkEditByItem(item *work.Item) error {
	if strings.TrimSpace(item.Body) == "" {
		item.EnsureDescriptionHeading()
		if err := saveByPath(item); err != nil {
			return err
		}
	}
	return openEditorFn(item.Path)
}

func saveByPath(item *work.Item) error {
	data, err := item.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(item.Path, data, 0644)
}

func runWorkStart(id string, args []string) error {
	root, err := repoReady()
	if err != nil {
		return err
	}
	item, err := work.FindActive(root, id)
	if err != nil {
		return err
	}
	agent := ""
	model := ""
	rootCfg, err := config.Load(root)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("work start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&agent, "agent", "", "initial agent override")
	fs.StringVar(&model, "model", "", "model override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if agent != "" {
		if err := agentpkgValidate(rootCfg.Agent.Command, agent); err != nil {
			return err
		}
	}
	if model != "" {
		if err := modelpkgValidate(rootCfg.Agent.Command, model); err != nil {
			return err
		}
	}
	return startWorkItem(root, item, agent, model)
}

func startWorkItem(root string, item *work.Item, startAgent, startModel string) (retErr error) {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	updated := *item
	updated.EnsureBranch()
	if updated.Status == "draft" {
		updated.Status = "in-progress"
	}
	resolvedAgent := agentpkg.Resolve(cfg.Agent.DefaultAgent, updated.Agent, startAgent)
	if err := agentpkgValidate(cfg.Agent.Command, resolvedAgent); err != nil {
		return err
	}
	resolvedModel := model.Resolve(cfg.Agent.DefaultModel, updated.Model, startModel)
	if err := modelpkgValidate(cfg.Agent.Command, resolvedModel); err != nil {
		return err
	}
	if strings.TrimSpace(updated.Agent) == "" && strings.TrimSpace(startAgent) == "" {
		updated.Agent = resolvedAgent
	}
	if strings.TrimSpace(updated.Model) == "" && strings.TrimSpace(startModel) == "" {
		updated.Model = resolvedModel
	}

	createBranch := !branchExistsFn(root, updated.Branch)
	startPoint := ""
	if createBranch {
		startPoint, err = resolveBaseStartPoint(root, cfg.Project.MainBranch)
		if err != nil {
			return err
		}
	}

	worktreePath := filepath.Join(root, config.WorktreesDir, updated.WorktreeDir())
	createdWorktree := false
	createdSession := false
	createdWindow := false
	sessionName := projectSessionName(root, cfg)
	windowName := updated.WindowName()
	defer func() {
		if retErr == nil {
			return
		}
		if createdWindow {
			_ = killWindowFn(sessionTarget(sessionName, windowName))
		}
		if createdSession {
			_ = killSessionFn(sessionName)
		}
		if createdWorktree {
			_ = removeWorktreeFn(root, worktreePath)
			if createBranch {
				_ = deleteBranchFn(root, updated.Branch)
			}
		}
	}()

	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		if err := createWorktreeFn(root, worktreePath, updated.Branch, startPoint, createBranch); err != nil {
			return err
		}
		createdWorktree = true
	} else if err != nil {
		return err
	}
	if err := ensureLocalExcludesFn(worktreePath, []string{config.CurrentWorkPath, config.WorktreesDir + "/"}); err != nil {
		return err
	}
	if err := syncOpencodeDir(root, worktreePath); err != nil {
		return err
	}

	currentPath := filepath.Join(worktreePath, config.CurrentWorkPath)
	if err := os.MkdirAll(filepath.Dir(currentPath), 0755); err != nil {
		return err
	}
	data, err := updated.Marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(currentPath, data, 0644); err != nil {
		return err
	}

	if !sessionExistsFn(sessionName) {
		if err := createSessionFn(sessionName, root); err != nil {
			return err
		}
		createdSession = true
	}
	if cfg.Notifications.EnabledValue() && cfg.Notifications.Tmux.EnabledValue() {
		if err := notifyDispatchFn(root, sessionName, cfg, notify.Event{
			Name:    "session_started",
			TaskID:  updated.PaddedID(),
			Title:   updated.Title,
			Branch:  updated.Branch,
			Model:   resolvedModel,
			Message: "Agent session started",
			Time:    time.Now(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: notification setup failed: %v\n", err)
		}
	}
	if !windowExistsFn(sessionName, windowName) {
		if err := createWindowFn(sessionName, windowName, worktreePath); err != nil {
			return err
		}
		createdWindow = true
		if err := sendKeysFn(sessionTarget(sessionName, windowName), buildAgentCommand(root, sessionName, cfg, &updated, resolvedAgent, resolvedModel)); err != nil {
			return err
		}
	}
	if err := saveWorkFn(root, &updated, false); err != nil {
		return err
	}
	*item = updated

	if os.Getenv("TMUX") != "" {
		fmt.Printf("Started in background window %s\n", sessionTarget(sessionName, windowName))
		fmt.Printf("Open it with: conduct open %d\n", updated.ID)
		return nil
	}
	return openTmuxFn(sessionTarget(sessionName, windowName))
}

func resolveBaseStartPoint(root, baseBranch string) (string, error) {
	if branchExistsFn(root, baseBranch) {
		return baseBranch, nil
	}
	remoteRef := "refs/remotes/origin/" + baseBranch
	if refExistsFn(root, remoteRef) {
		return "origin/" + baseBranch, nil
	}
	return "", fmt.Errorf("configured main branch %q not found locally or at origin/%s", baseBranch, baseBranch)
}

func syncOpencodeDir(root, worktreePath string) error {
	srcRoot := filepath.Join(root, ".opencode")
	info, err := os.Stat(srcRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	dstRoot := filepath.Join(worktreePath, ".opencode")
	return copyDirContents(srcRoot, dstRoot)
}

func copyDirContents(srcRoot, dstRoot string) error {
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dstRoot, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "node_modules" {
			continue
		}
		srcPath := filepath.Join(srcRoot, name)
		dstPath := filepath.Join(dstRoot, name)
		if entry.IsDir() {
			if err := copyDirContents(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return err
	}
	info, err := src.Stat()
	if err != nil {
		return err
	}
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
	if err != nil {
		return err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return nil
}

func buildAgentCommand(root, sessionName string, cfg *config.Config, item *work.Item, resolvedAgent, resolvedModel string) string {
	parts := []string{notify.EnvAssignment(root, sessionName), shellEscape(cfg.Agent.Command)}
	prompt := strings.ReplaceAll(cfg.Agent.Prompt, "{id}", item.PaddedID())
	prompt = strings.ReplaceAll(prompt, "{title}", item.Title)
	prompt = strings.ReplaceAll(prompt, "{branch}", item.Branch)
	for _, arg := range cfg.Agent.Args {
		arg = strings.ReplaceAll(arg, "{agent}", resolvedAgent)
		arg = strings.ReplaceAll(arg, "{model}", resolvedModel)
		arg = strings.ReplaceAll(arg, "{prompt}", prompt)
		arg = strings.ReplaceAll(arg, "{id}", item.PaddedID())
		arg = strings.ReplaceAll(arg, "{title}", item.Title)
		parts = append(parts, shellEscape(arg))
	}
	return strings.Join(parts, " ")
}

func runNotify(args []string) error {
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var eventName, title, message, taskID, branch, modelName string
	fs.StringVar(&eventName, "event", "", "notification event")
	fs.StringVar(&title, "title", "", "notification title")
	fs.StringVar(&message, "message", "", "notification message")
	fs.StringVar(&taskID, "task", "", "task id")
	fs.StringVar(&branch, "branch", "", "branch name")
	fs.StringVar(&modelName, "model", "", "model name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(eventName) == "" {
		return fmt.Errorf("usage: conduct notify --event EVENT [--title TITLE] [--message MESSAGE]")
	}

	root := notify.EnvRoot()
	if root == "" {
		var err error
		root, err = repoRoot()
		if err != nil {
			return err
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	sessionName := notify.EnvSession()
	if sessionName == "" {
		sessionName = projectSessionName(root, cfg)
	}
	return notify.Dispatch(root, sessionName, cfg, notify.Event{
		Name:    eventName,
		TaskID:  taskID,
		Title:   title,
		Message: message,
		Branch:  branch,
		Model:   modelName,
		Time:    time.Now(),
	})
}

func shellEscape(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n\"'$&()<>;|*?{}[]`!#~") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func runWorkOpen(id string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	item, err := work.FindActive(root, id)
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	sessionName := projectSessionName(root, cfg)
	windowName := item.WindowName()
	if !sessionExistsFn(sessionName) || !windowExistsFn(sessionName, windowName) {
		return fmt.Errorf("window %s not found", sessionTarget(sessionName, windowName))
	}
	return openTmuxFn(sessionTarget(sessionName, windowName))
}

func runWorkLand(id string) error {
	root, err := repoReady()
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	item, err := work.FindActive(root, id)
	if err != nil {
		return err
	}
	branch, err := git.CurrentBranch(root)
	if err != nil {
		return err
	}
	if branch != cfg.Project.MainBranch {
		return fmt.Errorf("must land from configured main branch %q (currently on %s)", cfg.Project.MainBranch, branch)
	}
	worktreePath := filepath.Join(root, config.WorktreesDir, item.WorktreeDir())
	clean, status, err := git.IsClean(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to inspect task worktree: %w", err)
	}
	if !clean {
		return fmt.Errorf("cannot land work %s: task worktree has uncommitted changes (%s)", item.PaddedID(), summarizeStatuses(status))
	}
	hasCommits, err := git.HasCommitsAhead(root, cfg.Project.MainBranch, item.Branch)
	if err != nil {
		return fmt.Errorf("failed to compare %s against %s: %w", item.Branch, cfg.Project.MainBranch, err)
	}
	if !hasCommits {
		return fmt.Errorf("cannot land work %s: branch %s has no committed changes ahead of %s", item.PaddedID(), item.Branch, cfg.Project.MainBranch)
	}
	if err := syncCurrent(root, item); err != nil {
		return err
	}
	if item.Branch == "" {
		return fmt.Errorf("work item has no branch")
	}
	if err := git.SquashMerge(root, item.Branch); err != nil {
		return err
	}
	if err := git.DropPath(root, config.CurrentWorkPath); err != nil {
		return err
	}
	item.Status = "landed"
	if err := work.Archive(root, item); err != nil {
		return err
	}
	cleanupWork(root, item)
	fmt.Printf("Landed %s\n", item.Title)
	fmt.Println("Squash merge applied. Review and commit the resulting changes.")
	return nil
}

func runWorkDrop(id string) error {
	root, err := repoReady()
	if err != nil {
		return err
	}
	item, err := work.FindActive(root, id)
	if err != nil {
		return err
	}
	if !confirm(fmt.Sprintf("Drop work item %d (%s)?", item.ID, item.Title)) {
		return nil
	}
	item.Status = "dropped"
	if err := work.Archive(root, item); err != nil {
		return err
	}
	cleanupWork(root, item)
	fmt.Printf("Dropped %s\n", item.Title)
	return nil
}

func cleanupWork(root string, item *work.Item) {
	if cfg, err := config.Load(root); err == nil {
		_ = killWindowFn(sessionTarget(projectSessionName(root, cfg), item.WindowName()))
	}
	_ = removeWorktreeFn(root, filepath.Join(root, config.WorktreesDir, item.WorktreeDir()))
	if item.Branch != "" {
		_ = deleteBranchFn(root, item.Branch)
	}
}

func syncCurrent(root string, item *work.Item) error {
	currentPath := filepath.Join(root, config.WorktreesDir, item.WorktreeDir(), config.CurrentWorkPath)
	if _, err := os.Stat(currentPath); err != nil {
		return nil
	}
	data, err := os.ReadFile(currentPath)
	if err != nil {
		return err
	}
	synced, err := work.Parse(currentPath)
	if err != nil {
		return fmt.Errorf("invalid current work file: %w", err)
	}
	if err := synced.Validate(); err != nil {
		return fmt.Errorf("invalid current work file: %w", err)
	}
	if err := os.WriteFile(item.Path, data, 0644); err != nil {
		return err
	}
	item.Body = synced.Body
	item.Accept = synced.Accept
	item.Constraints = synced.Constraints
	item.Scope = synced.Scope
	item.Model = synced.Model
	item.Title = synced.Title
	item.Status = synced.Status
	item.Branch = synced.Branch
	return nil
}

func runStatus() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	active, _, err := work.List(root)
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	projectSession := projectSessionName(root, cfg)
	sessions, _ := listSessionsFn()
	sessionSet := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		sessionSet[s] = true
	}
	printItems("STATUS", active, sessionSet, projectSession)
	return nil
}

func runConfig(args []string) error {
	if len(args) == 0 || args[0] != "show" {
		return fmt.Errorf("usage: conduct config show")
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func runDoctor() error {
	root, err := repoRoot()
	if err != nil {
		return fmt.Errorf("git repo: not found")
	}
	fmt.Printf("repo: ok (%s)\n", root)
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git: not found")
	}
	fmt.Println("git: ok")
	if _, err := exec.LookPath("tmux"); err != nil {
		fmt.Println("tmux: missing")
	} else {
		fmt.Println("tmux: ok")
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath(cfg.Agent.Command); err != nil {
		fmt.Printf("agent command: missing (%s)\n", cfg.Agent.Command)
	} else {
		fmt.Printf("agent command: ok (%s)\n", cfg.Agent.Command)
	}
	if err := agentpkg.ValidateAvailable(cfg.Agent.Command, cfg.Agent.DefaultAgent); err != nil {
		fmt.Printf("default agent: invalid (%v)\n", err)
	} else {
		fmt.Printf("default agent: ok (%s)\n", cfg.Agent.DefaultAgent)
	}
	if err := model.ValidateAvailable(cfg.Agent.Command, cfg.Agent.DefaultModel); err != nil {
		fmt.Printf("default model: invalid (%v)\n", err)
	} else {
		fmt.Printf("default model: ok (%s)\n", cfg.Agent.DefaultModel)
	}
	missing, err := config.MissingLayout(root)
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		fmt.Println("layout: ok")
	} else {
		fmt.Printf("layout: missing (%s)\n", strings.Join(missing, ", "))
	}
	pluginPath := filepath.Join(root, ".opencode", "plugins", "conductor-notify.js")
	if info, err := os.Stat(pluginPath); err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("opencode plugin: missing (%s)\n", pluginPath)
		} else {
			fmt.Printf("opencode plugin: error (%v)\n", err)
		}
	} else if info.IsDir() {
		fmt.Printf("opencode plugin: invalid (%s is a directory)\n", pluginPath)
	} else {
		fmt.Printf("opencode plugin: ok (%s)\n", pluginPath)
	}
	return nil
}

func runCompletion(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: conduct completion <bash|zsh|fish>")
	}

	script, err := completionScript(args[0])
	if err != nil {
		return err
	}
	fmt.Print(script)
	return nil
}

func runComplete(args []string) error {
	for _, suggestion := range completeArgs(args) {
		fmt.Println(suggestion)
	}
	return nil
}

func completeArgs(args []string) []string {
	if len(args) == 0 {
		return append([]string{}, rootCompletionItems()...)
	}

	words := args[:len(args)-1]
	current := args[len(args)-1]
	if len(args) == 1 {
		words = nil
	}

	if len(words) == 0 {
		if strings.HasPrefix(current, "-") {
			return completeFlags(current, rootLongFlags(), rootShortFlags())
		}
		return filterPrefix(rootCompletionItems(), current)
	}

	command := words[0]
	restWords := words[1:]

	switch command {
	case "new":
		return completeFlagSet(restWords, current, newLongFlags(), newShortFlags())
	case "start":
		return completeIDCommand(restWords, current, startLongFlags(), nil)
	case "show", "edit", "open", "land", "drop":
		return completeIDCommand(restWords, current, nil, nil)
	case "config":
		if len(restWords) > 0 {
			return nil
		}
		return filterPrefix([]string{"show"}, current)
	case "completion":
		if len(restWords) > 0 {
			return nil
		}
		return filterPrefix([]string{"bash", "zsh", "fish"}, current)
	case "help", "version", "doctor", "list", "status", "init":
		return nil
	default:
		return filterPrefix(rootCompletionItems(), current)
	}
}

func completeFlagSet(words []string, current string, longFlags, shortFlags []string) []string {
	if previousExpectsValue(words) {
		return nil
	}
	return completeFlags(current, longFlags, shortFlags)
}

func completeIDCommand(words []string, current string, longFlags, shortFlags []string) []string {
	if len(words) == 0 {
		if strings.HasPrefix(current, "-") {
			return completeFlags(current, longFlags, shortFlags)
		}
		return filterPrefix(activeWorkIDSuggestions(), current)
	}

	if previousExpectsValue(words) {
		return nil
	}

	if len(words) == 1 && words[0] != "" && !strings.HasPrefix(words[0], "-") {
		if strings.HasPrefix(current, "-") {
			return completeFlags(current, longFlags, shortFlags)
		}
		return nil
	}

	if strings.HasPrefix(current, "-") {
		return completeFlags(current, longFlags, shortFlags)
	}
	return nil
}

func previousExpectsValue(words []string) bool {
	if len(words) == 0 {
		return false
	}
	return flagExpectsValue(words[len(words)-1])
}

func flagExpectsValue(flagName string) bool {
	switch flagName {
	case "-t", "--title", "--agent", "--model", "-a", "--accept", "-c", "--constraint", "-s", "--scope":
		return true
	default:
		return false
	}
}

func activeWorkIDSuggestions() []string {
	root, err := repoRootFn()
	if err != nil {
		return nil
	}
	active, _, err := work.List(root)
	if err != nil {
		return nil
	}
	values := make([]string, 0, len(active))
	for _, item := range active {
		values = append(values, item.PaddedID())
	}
	return uniqueSorted(values)
}

func rootCompletionItems() []string {
	items := append(commandNames(), rootLongFlags()...)
	return uniqueSorted(items)
}

func commandNames() []string {
	return []string{
		"completion",
		"config",
		"doctor",
		"drop",
		"edit",
		"help",
		"init",
		"land",
		"list",
		"new",
		"open",
		"show",
		"start",
		"status",
		"version",
	}
}

func rootLongFlags() []string {
	return []string{"--help", "--version"}
}

func rootShortFlags() []string {
	return []string{"-h", "-v"}
}

func newLongFlags() []string {
	return []string{"--accept", "--agent", "--constraint", "--edit", "--model", "--scope", "--start", "--title"}
}

func newShortFlags() []string {
	return []string{"-a", "-c", "-s", "-t"}
}

func startLongFlags() []string {
	return []string{"--agent", "--model"}
}

func completeFlags(current string, longFlags, shortFlags []string) []string {
	if current == "" || strings.HasPrefix(current, "--") {
		return filterPrefix(longFlags, current)
	}
	if strings.HasPrefix(current, "-") {
		return filterPrefix(shortFlags, current)
	}
	return filterPrefix(longFlags, current)
}

func filterPrefix(values []string, prefix string) []string {
	if prefix == "" {
		return uniqueSorted(values)
	}
	matched := make([]string, 0, len(values))
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			matched = append(matched, value)
		}
	}
	return uniqueSorted(matched)
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func completionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return `# bash completion for conduct
_conduct_completion() {
	local cur out
	local -a words
	COMPREPLY=()
	cur="${COMP_WORDS[COMP_CWORD]}"
	words=("${COMP_WORDS[@]:1}")
	out="$(${COMP_WORDS[0]} __complete "${words[@]}" 2>/dev/null)" || return 0
	COMPREPLY=( $(compgen -W "$out" -- "$cur") )
}
complete -F _conduct_completion conduct
`, nil
	case "zsh":
		return `#compdef conduct
_conduct() {
	local -a words reply
	words=("${words[@]:2}" "$PREFIX")
	reply=("${(@f)$(conduct __complete "${words[@]}" 2>/dev/null)}")
	_describe 'values' reply
}
compdef _conduct conduct
`, nil
	case "fish":
		return `function __conduct_complete
	set -l tokens (commandline -opc)
	set -e tokens[1]
	conduct __complete $tokens (commandline -ct) 2>/dev/null
end

complete -c conduct -f -a '(__conduct_complete)'
`, nil
	default:
		return "", fmt.Errorf("unsupported shell %q (want bash, zsh, or fish)", shell)
	}
}

func repoReady() (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	if err := ensureLayoutFn(root); err != nil {
		return "", err
	}
	return root, nil
}

func repoRoot() (string, error) {
	root, err := repoRootFn()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return root, nil
}

func printItems(header string, items []*work.Item, sessionSet map[string]bool, projectSession string) {
	fmt.Println(header)
	fmt.Printf("%-6s %-12s %-8s %-28s %s\n", "ID", "STATUS", "WINDOW", "BRANCH", "TITLE")
	if len(items) == 0 {
		fmt.Println("(empty)")
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for _, item := range items {
		session := "-"
		branch := item.Branch
		if branch == "" {
			branch = "-"
		}
		if sessionSet[projectSession] && windowExistsFn(projectSession, item.WindowName()) {
			session = "active"
		}
		fmt.Printf("%-6s %-12s %-8s %-28s %s\n", item.PaddedID(), item.Status, session, branch, item.Title)
	}
}

func showItem(root string, item *work.Item) {
	cfg, _ := config.Load(root)
	sessionName := projectSessionName(root, cfg)
	fmt.Printf("ID: %s\n", item.PaddedID())
	fmt.Printf("Title: %s\n", item.Title)
	fmt.Printf("Status: %s\n", item.Status)
	if item.Model != "" {
		fmt.Printf("Model: %s\n", item.Model)
	}
	if item.Branch != "" {
		fmt.Printf("Branch: %s\n", item.Branch)
	}
	fmt.Printf("Session: %s\n", sessionName)
	fmt.Printf("Window: %s\n", item.WindowName())
	fmt.Printf("Target: %s\n", sessionTarget(sessionName, item.WindowName()))
	fmt.Printf("Worktree: %s\n", filepath.Join(root, config.WorktreesDir, item.WorktreeDir()))
	if len(item.Scope) > 0 {
		fmt.Printf("Scope: %s\n", strings.Join(item.Scope, ", "))
	}
	if len(item.Accept) > 0 {
		fmt.Println("Accept:")
		for _, value := range item.Accept {
			fmt.Printf("- %s\n", value)
		}
	}
	if len(item.Constraints) > 0 {
		fmt.Println("Constraints:")
		for _, value := range item.Constraints {
			fmt.Printf("- %s\n", value)
		}
	}
	if strings.TrimSpace(item.Body) != "" {
		fmt.Println()
		fmt.Print(item.Body)
		if !strings.HasSuffix(item.Body, "\n") {
			fmt.Println()
		}
	}
}

func projectSessionName(root string, cfg *config.Config) string {
	project := slugify(filepath.Base(root))
	if project == "" {
		project = "project"
	}
	return cfg.Tmux.SessionPrefix + "-" + project
}

func sessionTarget(sessionName, windowName string) string {
	return sessionName + ":" + windowName
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	var value string
	_, _ = fmt.Scanln(&value)
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "y" || value == "yes"
}

func summarizeStatuses(statuses []git.FileStatus) string {
	if len(statuses) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(statuses))
	limit := 5
	for i, status := range statuses {
		if i >= limit {
			parts = append(parts, fmt.Sprintf("and %d more", len(statuses)-limit))
			break
		}
		parts = append(parts, fmt.Sprintf("%s %s", status.XY, status.Path))
	}
	return strings.Join(parts, ", ")
}

func printHelp() {
	fmt.Print(`conductor

Usage:
	conduct completion <bash|zsh|fish>
  conduct init
  conduct new
	 conduct new -t "Review backend code" [--agent plan] [--model gpt-5-mini] [--edit] [--start]
  conduct list
  conduct show <id>
  conduct edit <id>
	 conduct start <id> [--agent AGENT] [--model MODEL]
  conduct open <id>
  conduct notify --event EVENT [--title TITLE] [--message MESSAGE]
  conduct land <id>
  conduct drop <id>
  conduct status
  conduct config show
  conduct doctor
  conduct version

Notes:
	- conduct completion prints a shell completion script.
	- conduct new opens your editor for a new work item.
	- conduct new -t ... creates a title-only work item.
	- Work only starts with --start or conduct start <id>.
	- conduct notify is intended as a command target for OpenCode notification plugins.
`)
}

func cfgMainBranch(root string) string {
	cfg, err := config.Load(root)
	if err != nil {
		return config.Default().Project.MainBranch
	}
	return cfg.Project.MainBranch
}
