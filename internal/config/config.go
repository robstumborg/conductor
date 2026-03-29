package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	ConductDir      = ".conduct"
	ConfigPath      = ".conduct/config.yaml"
	WorkDir         = ".conduct/work"
	ActiveWorkDir   = ".conduct/work/active"
	ArchiveWorkDir  = ".conduct/work/archive"
	WorktreesDir    = ".conduct/worktrees"
	CurrentWorkPath = ".conduct/current.md"
)

type Config struct {
	Project       ProjectConfig      `yaml:"project"`
	Agent         AgentConfig        `yaml:"agent"`
	Tmux          TmuxConfig         `yaml:"tmux"`
	Notifications NotificationConfig `yaml:"notifications"`
}

type ProjectConfig struct {
	MainBranch string `yaml:"main_branch"`
}

type AgentConfig struct {
	Command      string   `yaml:"command"`
	Args         []string `yaml:"args"`
	DefaultModel string   `yaml:"default_model"`
	Prompt       string   `yaml:"prompt"`
}

type TmuxConfig struct {
	SessionPrefix string `yaml:"session_prefix"`
}

type NotificationConfig struct {
	Enabled *bool                  `yaml:"enabled"`
	LogPath string                 `yaml:"log_path"`
	Tmux    TmuxNotificationConfig `yaml:"tmux"`
}

type TmuxNotificationConfig struct {
	Enabled   *bool  `yaml:"enabled"`
	Window    string `yaml:"window"`
	PaneTitle string `yaml:"pane_title"`
	Height    int    `yaml:"height"`
}

func Default() *Config {
	return &Config{
		Project: ProjectConfig{MainBranch: "main"},
		Agent: AgentConfig{
			Command:      "opencode",
			DefaultModel: "openai/gpt-5.4",
			Args: []string{
				"--model", "{model}",
				"--prompt", "{prompt}",
			},
			Prompt: "Open `.conduct/current.md` for your assignment. Complete the requested work in this worktree. If helpful, update `.conduct/current.md` with notes before stopping; conductor will sync those notes back into the durable work record later. Do not include `.conduct/current.md` in your final commit. When you are finished, stage and commit your actual task changes on this branch before stopping.",
		},
		Tmux: TmuxConfig{SessionPrefix: "conduct"},
		Notifications: NotificationConfig{
			Enabled: boolPtr(true),
			LogPath: filepath.Join(ConductDir, "notifications.log"),
			Tmux: TmuxNotificationConfig{
				Enabled:   boolPtr(true),
				Window:    "podium",
				PaneTitle: "conductor-notifications",
				Height:    12,
			},
		},
	}
}

func Load(root string) (*Config, error) {
	configFile := filepath.Join(root, ConfigPath)
	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	defaults := Default()
	if cfg.Project.MainBranch == "" {
		cfg.Project.MainBranch = defaults.Project.MainBranch
	}
	if cfg.Agent.Command == "" {
		cfg.Agent.Command = defaults.Agent.Command
	}
	if cfg.Agent.DefaultModel == "" {
		cfg.Agent.DefaultModel = defaults.Agent.DefaultModel
	}
	if len(cfg.Agent.Args) == 0 {
		cfg.Agent.Args = defaults.Agent.Args
	}
	if cfg.Agent.Prompt == "" {
		cfg.Agent.Prompt = defaults.Agent.Prompt
	}
	if cfg.Tmux.SessionPrefix == "" {
		cfg.Tmux.SessionPrefix = defaults.Tmux.SessionPrefix
	}
	if cfg.Notifications.Enabled == nil {
		cfg.Notifications.Enabled = defaults.Notifications.Enabled
	}
	if cfg.Notifications.LogPath == "" {
		cfg.Notifications.LogPath = defaults.Notifications.LogPath
	}
	if cfg.Notifications.Tmux.Enabled == nil {
		cfg.Notifications.Tmux.Enabled = defaults.Notifications.Tmux.Enabled
	}
	if cfg.Notifications.Tmux.Window == "" {
		cfg.Notifications.Tmux.Window = defaults.Notifications.Tmux.Window
	}
	if cfg.Notifications.Tmux.PaneTitle == "" {
		cfg.Notifications.Tmux.PaneTitle = defaults.Notifications.Tmux.PaneTitle
	}
	if cfg.Notifications.Tmux.Height == 0 {
		cfg.Notifications.Tmux.Height = defaults.Notifications.Tmux.Height
	}

	return &cfg, nil
}

func (c NotificationConfig) EnabledValue() bool {
	return c.Enabled == nil || *c.Enabled
}

func (c TmuxNotificationConfig) EnabledValue() bool {
	return c.Enabled == nil || *c.Enabled
}

func boolPtr(value bool) *bool {
	return &value
}

func Save(root string, cfg *Config) error {
	configFile := filepath.Join(root, ConfigPath)
	if err := os.MkdirAll(filepath.Dir(configFile), 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(configFile, data, 0644)
}

func EnsureLayout(root string) error {
	dirs := []string{
		filepath.Join(root, ConductDir),
		filepath.Join(root, ActiveWorkDir),
		filepath.Join(root, ArchiveWorkDir),
		filepath.Join(root, WorktreesDir),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	if err := ensureRootGitignore(root); err != nil {
		return err
	}

	conductGitignore := filepath.Join(root, ConductDir, ".gitignore")
	if _, err := os.Stat(conductGitignore); os.IsNotExist(err) {
		contents := []byte("current.md\nworktrees/\n")
		if err := os.WriteFile(conductGitignore, contents, 0644); err != nil {
			return err
		}
	}

	gitignorePath := filepath.Join(root, WorktreesDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitignorePath, []byte("*\n"), 0644); err != nil {
			return err
		}
	}

	return nil
}

func MissingLayout(root string) ([]string, error) {
	checks := []struct {
		label string
		path  string
	}{
		{label: ConductDir, path: filepath.Join(root, ConductDir)},
		{label: ActiveWorkDir, path: filepath.Join(root, ActiveWorkDir)},
		{label: ArchiveWorkDir, path: filepath.Join(root, ArchiveWorkDir)},
		{label: WorktreesDir, path: filepath.Join(root, WorktreesDir)},
		{label: filepath.Join(ConductDir, ".gitignore"), path: filepath.Join(root, ConductDir, ".gitignore")},
		{label: filepath.Join(WorktreesDir, ".gitignore"), path: filepath.Join(root, WorktreesDir, ".gitignore")},
	}

	missing := make([]string, 0, len(checks)+2)
	for _, check := range checks {
		if _, err := os.Stat(check.path); err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, check.label)
				continue
			}
			return nil, err
		}
	}

	gitignorePath := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			missing = append(missing, ".gitignore:.conduct/current.md", ".gitignore:.conduct/worktrees/")
			return missing, nil
		}
		return nil, err
	}
	contents := string(data)
	for _, line := range []string{".conduct/current.md", ".conduct/worktrees/"} {
		if !containsGitignoreLine(contents, line) {
			missing = append(missing, ".gitignore:"+line)
		}
	}

	return missing, nil
}

func ensureRootGitignore(root string) error {
	gitignorePath := filepath.Join(root, ".gitignore")
	required := []string{
		".conduct/current.md",
		".conduct/worktrees/",
	}

	var existing string
	if data, err := os.ReadFile(gitignorePath); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}

	var toAppend []string
	for _, line := range required {
		if !containsGitignoreLine(existing, line) {
			toAppend = append(toAppend, line)
		}
	}

	if len(toAppend) == 0 {
		return nil
	}

	updated := existing
	if updated != "" && updated[len(updated)-1] != '\n' {
		updated += "\n"
	}
	if updated != "" {
		updated += "\n"
	}
	updated += "# conductor runtime\n"
	for _, line := range toAppend {
		updated += line + "\n"
	}

	return os.WriteFile(gitignorePath, []byte(updated), 0644)
}

func containsGitignoreLine(contents, needle string) bool {
	start := 0
	for start <= len(contents) {
		end := start
		for end < len(contents) && contents[end] != '\n' {
			end++
		}
		line := contents[start:end]
		if line == needle {
			return true
		}
		if end == len(contents) {
			break
		}
		start = end + 1
	}
	return false
}
