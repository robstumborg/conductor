package work

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/robstumborg/conductor/internal/config"
)

func TestNewTitleOnly(t *testing.T) {
	item := New(12, CreateOptions{Title: "Review backend code"})
	if item.Title != "Review backend code" {
		t.Fatalf("unexpected title: %q", item.Title)
	}
	if item.Body != "" {
		t.Fatalf("expected empty body, got %q", item.Body)
	}
	if item.Status != "draft" {
		t.Fatalf("unexpected status: %q", item.Status)
	}
}

func TestEnsureDescriptionHeading(t *testing.T) {
	item := New(12, CreateOptions{Title: "Review backend code"})
	item.EnsureDescriptionHeading()
	if item.Body != "## Description\n" {
		t.Fatalf("unexpected body: %q", item.Body)
	}
}

func TestSaveAndParse(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	item := New(12, CreateOptions{Title: "Review backend code", InsertBody: true})
	if err := Save(root, item, false); err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(item.Path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Title != item.Title {
		t.Fatalf("title mismatch: %q != %q", parsed.Title, item.Title)
	}
	if parsed.Body != item.Body {
		t.Fatalf("body mismatch: %q != %q", parsed.Body, item.Body)
	}
	if parsed.Agent != item.Agent {
		t.Fatalf("agent mismatch: %q != %q", parsed.Agent, item.Agent)
	}
}

func TestNewTrimsAgent(t *testing.T) {
	item := New(12, CreateOptions{Title: "Review backend code", Agent: "  plan  "})
	if item.Agent != "plan" {
		t.Fatalf("unexpected agent: %q", item.Agent)
	}
}

func TestNextID(t *testing.T) {
	root := t.TempDir()
	if err := config.EnsureLayout(root); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(root, config.ActiveWorkDir, "0003-foo.md"),
		filepath.Join(root, config.ArchiveWorkDir, "0009-bar.md"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NextID(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != 10 {
		t.Fatalf("unexpected next id: %d", got)
	}
}

func TestParseSupportsCRLFFrontmatter(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "0001-test.md")
	contents := "---\r\nid: 1\r\ntitle: CRLF item\r\nstatus: draft\r\ncreated_at: 2024-01-01T00:00:00Z\r\nupdated_at: 2024-01-01T00:00:00Z\r\n---\r\n\r\n## Description\r\nBody\r\n"
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}

	item, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "CRLF item" {
		t.Fatalf("title=%q", item.Title)
	}
	if item.Status != "draft" {
		t.Fatalf("status=%q", item.Status)
	}
	if item.Body == "" {
		t.Fatal("expected body")
	}
}
