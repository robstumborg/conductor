package editor

import (
	"reflect"
	"testing"
)

func TestCommandForEditorParsesArgumentsSafely(t *testing.T) {
	cmd, err := commandForEditor("code --wait", "/tmp/work item.md")
	if err != nil {
		t.Fatalf("commandForEditor returned error: %v", err)
	}
	if want := []string{"code", "--wait", "/tmp/work item.md"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("unexpected args: got %v want %v", cmd.Args, want)
	}
}

func TestCommandForEditorSupportsQuotedArguments(t *testing.T) {
	cmd, err := commandForEditor("'Visual Studio Code' --wait --reuse-window", "/tmp/work item.md")
	if err != nil {
		t.Fatalf("commandForEditor returned error: %v", err)
	}
	if want := []string{"Visual Studio Code", "--wait", "--reuse-window", "/tmp/work item.md"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("unexpected args: got %v want %v", cmd.Args, want)
	}
}

func TestCommandForEditorRejectsUnterminatedQuotes(t *testing.T) {
	if _, err := commandForEditor("code 'unterminated", "/tmp/work item.md"); err == nil {
		t.Fatal("expected error for unterminated quote")
	}
}

func TestSplitCommandTreatsShellMetacharactersAsLiterals(t *testing.T) {
	args, err := splitCommand("vim ';' touch /tmp/pwned")
	if err != nil {
		t.Fatalf("splitCommand returned error: %v", err)
	}
	if want := []string{"vim", ";", "touch", "/tmp/pwned"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args: got %v want %v", args, want)
	}
}
