package notify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robstumborg/conductor/internal/config"
)

func TestFormatLine(t *testing.T) {
	event := Event{
		Name:    "question",
		TaskID:  "0001",
		Branch:  "conduct/0001-test",
		Model:   "openai/gpt-5.4",
		Title:   "Need review\nnow",
		Message: "Please answer\r\nsoon",
		Time:    time.Date(2026, 3, 29, 7, 30, 0, 0, time.UTC),
	}
	got := FormatLine(event)
	want := "2026-03-29 07:30:00 | question | task=0001 | branch=conduct/0001-test | model=openai/gpt-5.4 | Need review now - Please answer soon"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEnvAssignment(t *testing.T) {
	got := EnvAssignment("/tmp/root dir", "conduct-project")
	if !strings.Contains(got, "CONDUCT_ROOT='/tmp/root dir'") {
		t.Fatalf("root missing from %q", got)
	}
	if !strings.Contains(got, "CONDUCT_SESSION_NAME=conduct-project") {
		t.Fatalf("session missing from %q", got)
	}
}

func TestDispatchWritesLogWithoutTmux(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Notifications.LogPath = filepath.Join(config.ConductDir, "notify.log")
	disabled := false
	cfg.Notifications.Tmux.Enabled = &disabled

	if err := Dispatch(root, "", cfg, Event{Name: "complete", Message: "All done"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, config.ConductDir, "notify.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "complete") || !strings.Contains(string(data), "All done") {
		t.Fatalf("unexpected log contents %q", string(data))
	}
}
