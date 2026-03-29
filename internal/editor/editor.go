package editor

import (
	"fmt"
	"os"
	"os/exec"
)

func Open(path string) error {
	ed := os.Getenv("EDITOR")
	if ed == "" {
		ed = "vi"
	}

	cmd := commandForEditor(ed, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}
	return nil
}

func commandForEditor(editor, path string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", `eval "set -- $CONDUCT_EDITOR"; exec "$@" "$CONDUCT_PATH"`)
	cmd.Env = append(os.Environ(), "CONDUCT_EDITOR="+editor, "CONDUCT_PATH="+path)
	return cmd
}
