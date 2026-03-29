package editor

import (
	"strings"
	"testing"
)

func TestCommandForEditorUsesShellWrapper(t *testing.T) {
	cmd := commandForEditor("code --wait", "/tmp/work item.md")
	if len(cmd.Args) < 3 {
		t.Fatalf("unexpected args: %v", cmd.Args)
	}
	if cmd.Args[0] != "sh" || cmd.Args[1] != "-c" {
		t.Fatalf("unexpected command: %v", cmd.Args)
	}
	joinedEnv := strings.Join(cmd.Env, "\n")
	if !strings.Contains(joinedEnv, "CONDUCT_EDITOR=code --wait") {
		t.Fatalf("missing editor env in %q", joinedEnv)
	}
	if !strings.Contains(joinedEnv, "CONDUCT_PATH=/tmp/work item.md") {
		t.Fatalf("missing path env in %q", joinedEnv)
	}
}
