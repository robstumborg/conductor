package notify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/robstumborg/conductor/internal/config"
	"github.com/robstumborg/conductor/internal/tmux"
)

type Event struct {
	Name    string
	TaskID  string
	Title   string
	Message string
	Branch  string
	Model   string
	Time    time.Time
}

func Dispatch(root, session string, cfg *config.Config, event Event) error {
	if !cfg.Notifications.EnabledValue() {
		return nil
	}
	logPath := cfg.Notifications.LogPath
	if !filepath.IsAbs(logPath) {
		logPath = filepath.Join(root, logPath)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return err
	}
	line := FormatLine(event)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return err
	}

	if !cfg.Notifications.Tmux.EnabledValue() || strings.TrimSpace(session) == "" || !tmux.SessionExists(session) {
		return nil
	}
	window := cfg.Notifications.Tmux.Window
	if !tmux.WindowExists(session, window) {
		if err := tmux.CreateWindow(session, window, root); err != nil {
			return err
		}
	}
	return tmux.EnsureTailPane(session, window, cfg.Notifications.Tmux.PaneTitle, root, logPath, cfg.Notifications.Tmux.Height)
}

func FormatLine(event Event) string {
	stamp := event.Time
	if stamp.IsZero() {
		stamp = time.Now()
	}
	parts := []string{stamp.Format("2006-01-02 15:04:05"), normalize(event.Name)}
	if value := normalize(event.TaskID); value != "" {
		parts = append(parts, "task="+value)
	}
	if value := normalize(event.Branch); value != "" {
		parts = append(parts, "branch="+value)
	}
	if value := normalize(event.Model); value != "" {
		parts = append(parts, "model="+value)
	}
	text := strings.TrimSpace(strings.Join([]string{normalize(event.Title), normalize(event.Message)}, " - "))
	text = strings.Trim(text, " -")
	if text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, " | ")
}

func normalize(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.Join(strings.Fields(value), " ")
}

func EnvRoot() string {
	return strings.TrimSpace(os.Getenv("CONDUCT_ROOT"))
}

func EnvSession() string {
	return strings.TrimSpace(os.Getenv("CONDUCT_SESSION_NAME"))
}

func EnvAssignment(root, session string) string {
	return fmt.Sprintf("env CONDUCT_ROOT=%s CONDUCT_SESSION_NAME=%s", shellEscape(root), shellEscape(session))
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
