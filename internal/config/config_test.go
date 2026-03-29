package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureLayout(t *testing.T) {
	root := t.TempDir()
	if err := EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(root, ConductDir),
		filepath.Join(root, ConductDir, ".gitignore"),
		filepath.Join(root, ActiveWorkDir),
		filepath.Join(root, ArchiveWorkDir),
		filepath.Join(root, WorktreesDir),
		filepath.Join(root, WorktreesDir, ".gitignore"),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(data)
	for _, needle := range []string{".conduct/current.md", ".conduct/worktrees/"} {
		if !containsGitignoreLine(contents, needle) {
			t.Fatalf("expected .gitignore to contain %q, got %q", needle, contents)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := Default()
	if cfg.Agent.Command == "" {
		t.Fatal("expected agent command")
	}
	if cfg.Agent.DefaultModel == "" {
		t.Fatal("expected default model")
	}
}

func TestEnsureRootGitignorePreservesExistingEntries(t *testing.T) {
	root := t.TempDir()
	initial := "node_modules/\n.conduct/current.md\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureLayout(root); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(data)
	if !containsGitignoreLine(contents, "node_modules/") {
		t.Fatalf("expected existing entry to remain, got %q", contents)
	}
	if !containsGitignoreLine(contents, ".conduct/current.md") {
		t.Fatalf("expected current.md ignore, got %q", contents)
	}
	if !containsGitignoreLine(contents, ".conduct/worktrees/") {
		t.Fatalf("expected worktrees ignore, got %q", contents)
	}
}

func TestMissingLayout(t *testing.T) {
	root := t.TempDir()
	missing, err := MissingLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) == 0 {
		t.Fatal("expected missing layout entries")
	}

	if err := EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	missing, err = MissingLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected complete layout, got %v", missing)
	}
}
