package work

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/robstumborg/conductor/internal/config"
)

var frontmatterRE = regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---\r?\n?(.*)$`)
var idRE = regexp.MustCompile(`^(\d+)`)
var nonSlugRE = regexp.MustCompile(`[^a-z0-9-]+`)

type Item struct {
	ID          int      `yaml:"id"`
	Title       string   `yaml:"title"`
	Status      string   `yaml:"status"`
	Agent       string   `yaml:"agent,omitempty"`
	Model       string   `yaml:"model,omitempty"`
	Branch      string   `yaml:"branch,omitempty"`
	Scope       []string `yaml:"scope,omitempty"`
	Accept      []string `yaml:"accept,omitempty"`
	Constraints []string `yaml:"constraints,omitempty"`
	CreatedAt   string   `yaml:"created_at"`
	UpdatedAt   string   `yaml:"updated_at"`
	Body        string   `yaml:"-"`
	Path        string   `yaml:"-"`
}

type CreateOptions struct {
	Title       string
	Agent       string
	Model       string
	Scope       []string
	Accept      []string
	Constraints []string
	InsertBody  bool
	Status      string
}

func New(id int, opts CreateOptions) *Item {
	now := time.Now().UTC().Format(time.RFC3339)
	item := &Item{
		ID:          id,
		Title:       strings.TrimSpace(opts.Title),
		Status:      opts.Status,
		Agent:       strings.TrimSpace(opts.Agent),
		Model:       strings.TrimSpace(opts.Model),
		Scope:       clone(opts.Scope),
		Accept:      clone(opts.Accept),
		Constraints: clone(opts.Constraints),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if item.Status == "" {
		item.Status = "draft"
	}
	if opts.InsertBody {
		item.EnsureDescriptionHeading()
	}
	return item
}

func (i *Item) Validate() error {
	if strings.TrimSpace(i.Title) == "" && !i.HasDescription() {
		return fmt.Errorf("title or description is required")
	}
	if i.Status == "" {
		return fmt.Errorf("status is required")
	}
	return nil
}

func (i *Item) HasDescription() bool {
	body := strings.TrimSpace(i.Body)
	if body == "" {
		return false
	}
	if body == "## Description" {
		return false
	}
	if strings.HasPrefix(body, "## Description\n") {
		return strings.TrimSpace(strings.TrimPrefix(body, "## Description\n")) != ""
	}
	return true
}

func (i *Item) Slug() string {
	text := strings.ToLower(strings.TrimSpace(i.Title))
	text = strings.ReplaceAll(text, " ", "-")
	text = nonSlugRE.ReplaceAllString(text, "-")
	text = strings.Trim(text, "-")
	text = regexp.MustCompile(`-+`).ReplaceAllString(text, "-")
	if text == "" {
		return fmt.Sprintf("work-%04d", i.ID)
	}
	if len(text) > 48 {
		text = strings.Trim(text[:48], "-")
	}
	return text
}

func (i *Item) PaddedID() string {
	return fmt.Sprintf("%04d", i.ID)
}

func (i *Item) Filename() string {
	return fmt.Sprintf("%s-%s.md", i.PaddedID(), i.Slug())
}

func (i *Item) WindowName() string {
	return fmt.Sprintf("%s-%s", i.PaddedID(), i.Slug())
}

func (i *Item) WorktreeDir() string {
	if i.Branch != "" {
		return strings.ReplaceAll(strings.TrimPrefix(i.Branch, "conduct/"), "/", "-")
	}
	return fmt.Sprintf("%s-%s", i.PaddedID(), i.Slug())
}

func (i *Item) DefaultBranch() string {
	return fmt.Sprintf("conduct/%s-%s", i.PaddedID(), i.Slug())
}

func (i *Item) EnsureBranch() {
	if strings.TrimSpace(i.Branch) == "" {
		i.Branch = i.DefaultBranch()
	}
}

func (i *Item) EnsureDescriptionHeading() {
	if strings.TrimSpace(i.Body) == "" {
		i.Body = "## Description\n"
	}
}

func (i *Item) Touch() {
	i.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
}

func Parse(path string) (*Item, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	matches := frontmatterRE.FindSubmatch(data)
	if len(matches) != 3 {
		return nil, fmt.Errorf("invalid frontmatter in %s", path)
	}

	var item Item
	if err := yaml.Unmarshal(matches[1], &item); err != nil {
		return nil, err
	}
	item.Body = strings.TrimLeft(string(matches[2]), "\n")
	item.Path = path
	return &item, nil
}

func (i *Item) Marshal() ([]byte, error) {
	copyItem := *i
	copyItem.Path = ""
	copyItem.Body = ""
	copyItem.Touch()
	data, err := yaml.Marshal(&copyItem)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(data)
	buf.WriteString("---\n")
	if strings.TrimSpace(i.Body) != "" {
		buf.WriteString("\n")
		buf.WriteString(strings.TrimRight(i.Body, "\n"))
		buf.WriteString("\n")
	}
	return buf.Bytes(), nil
}

func Save(root string, item *Item, archived bool) error {
	if err := item.Validate(); err != nil {
		return err
	}
	item.Touch()
	baseDir := filepath.Join(root, config.ActiveWorkDir)
	if archived {
		baseDir = filepath.Join(root, config.ArchiveWorkDir)
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return err
	}
	if item.Path == "" || archived || !strings.HasPrefix(item.Path, baseDir) {
		item.Path = filepath.Join(baseDir, item.Filename())
	}
	data, err := item.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(item.Path, data, 0644)
}

func NextID(root string) (int, error) {
	maxID := 0
	for _, dir := range []string{filepath.Join(root, config.ActiveWorkDir), filepath.Join(root, config.ArchiveWorkDir)} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			m := idRE.FindStringSubmatch(entry.Name())
			if len(m) != 2 {
				continue
			}
			id, err := strconv.Atoi(m[1])
			if err == nil && id > maxID {
				maxID = id
			}
		}
	}
	return maxID + 1, nil
}

func Find(root, id string) (*Item, error) {
	needle := id
	if num, err := strconv.Atoi(id); err == nil {
		needle = fmt.Sprintf("%04d", num)
	}
	for _, dir := range []string{filepath.Join(root, config.ActiveWorkDir), filepath.Join(root, config.ArchiveWorkDir)} {
		matches, _ := filepath.Glob(filepath.Join(dir, needle+"*.md"))
		if len(matches) > 0 {
			return Parse(matches[0])
		}
	}
	return nil, fmt.Errorf("work item %s not found", id)
}

func FindActive(root, id string) (*Item, error) {
	needle := id
	if num, err := strconv.Atoi(id); err == nil {
		needle = fmt.Sprintf("%04d", num)
	}
	matches, _ := filepath.Glob(filepath.Join(root, config.ActiveWorkDir, needle+"*.md"))
	if len(matches) == 0 {
		return nil, fmt.Errorf("active work item %s not found", id)
	}
	return Parse(matches[0])
}

func List(root string) ([]*Item, []*Item, error) {
	active, err := listDir(filepath.Join(root, config.ActiveWorkDir))
	if err != nil {
		return nil, nil, err
	}
	archive, err := listDir(filepath.Join(root, config.ArchiveWorkDir))
	if err != nil {
		return nil, nil, err
	}
	return active, archive, nil
}

func listDir(dir string) ([]*Item, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	items := make([]*Item, 0, len(matches))
	for _, match := range matches {
		item, err := Parse(match)
		if err == nil {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func Archive(root string, item *Item) error {
	oldPath := item.Path
	item.Path = filepath.Join(root, config.ArchiveWorkDir, item.Filename())
	if err := Save(root, item, true); err != nil {
		return err
	}
	if oldPath != "" && oldPath != item.Path {
		_ = os.Remove(oldPath)
	}
	return nil
}

func clone(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}
