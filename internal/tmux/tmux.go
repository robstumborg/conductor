package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// PodiumWindow is the window where you orchestrate from.
const PodiumWindow = "podium"

func SessionExists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

func CreateSession(name, dir string) error {
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name, "-n", PodiumWindow, "-c", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-session failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func WindowExists(session, window string) bool {
	cmd := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_name}")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == window {
			return true
		}
	}
	return false
}

func CreateWindow(session, window, dir string) error {
	cmd := exec.Command("tmux", "new-window", "-d", "-t", session, "-n", window, "-c", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux new-window failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func SendKeys(name, value string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", name, value, "C-m")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux send-keys failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func KillSession(name string) error {
	cmd := exec.Command("tmux", "kill-session", "-t", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux kill-session failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func KillWindow(target string) error {
	cmd := exec.Command("tmux", "kill-window", "-t", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux kill-window failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func Open(target string) error {
	if os.Getenv("TMUX") != "" {
		cmd := exec.Command("tmux", "switch-client", "-t", target)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	cmd := exec.Command("tmux", "attach-session", "-t", target)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ListSessions() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(err.Error(), "no server running") {
			return nil, nil
		}
		return nil, err
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

func PaneTitleExists(target, title string) bool {
	cmd := exec.Command("tmux", "list-panes", "-t", target, "-F", "#{pane_title}")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == title {
			return true
		}
	}
	return false
}

func EnsureTailPane(session, window, title, dir, logPath string, height int) error {
	target := session + ":" + window
	if PaneTitleExists(target, title) {
		return nil
	}
	if height <= 0 {
		height = 12
	}
	command := fmt.Sprintf("touch %s && exec tail -n 200 -F %s", shellEscape(logPath), shellEscape(logPath))
	cmd := exec.Command(
		"tmux", "split-window", "-d", "-v", "-t", target,
		"-l", strconv.Itoa(height),
		"-c", dir,
		"-P", "-F", "#{pane_id}",
		"sh", "-lc", command,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux split-window failed: %s", strings.TrimSpace(string(out)))
	}
	paneID := strings.TrimSpace(string(out))
	if paneID == "" {
		return fmt.Errorf("tmux split-window did not return a pane id")
	}
	cmd = exec.Command("tmux", "select-pane", "-t", paneID, "-T", title)
	out, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux select-pane failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
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
