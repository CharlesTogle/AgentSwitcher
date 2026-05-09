package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"agentswitcher/internal/store"
)

func TestLoadPromptStandardsReadsAndTrimsContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "RULES.md")
	if err := os.WriteFile(path, []byte("  first line\nsecond line \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := loadPromptStandards([]store.Standard{{Path: path}})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 standard, got %d", len(got))
	}
	if got[0].Name != "RULES.md" {
		t.Fatalf("expected file name RULES.md, got %q", got[0].Name)
	}
	if got[0].Content != "first line\nsecond line" {
		t.Fatalf("unexpected trimmed content: %q", got[0].Content)
	}
}

func TestSelectedStandardHelpers(t *testing.T) {
	standards := []store.Standard{
		{Path: "/tmp/b.md"},
		{Path: "/tmp/a.md"},
	}

	selected := selectedStandardPathSet(standards)
	if !selected["/tmp/a.md"] || !selected["/tmp/b.md"] {
		t.Fatalf("expected selected path set to include all standards: %#v", selected)
	}

	got := selectedStandardPaths(selected)
	want := []string{"/tmp/a.md", "/tmp/b.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected sorted standard paths %v, got %v", want, got)
	}
}

func TestDefaultStandardsDirectory(t *testing.T) {
	projectDir := "/tmp/project"

	if got := defaultStandardsDirectory(projectDir, nil); got != projectDir {
		t.Fatalf("expected project dir fallback, got %q", got)
	}

	standards := []store.Standard{{Path: "/tmp/project/docs/rules.md"}}
	if got := defaultStandardsDirectory(projectDir, standards); got != "/tmp/project/docs" {
		t.Fatalf("expected standards directory, got %q", got)
	}
}

func TestListStandardCandidatesFiltersMarkdownAndKeepsSelection(t *testing.T) {
	dir := t.TempDir()
	selectedPath := filepath.Join(dir, "b.markdown")

	files := []string{
		"a.md",
		"b.markdown",
		"c.mdx",
		"skip.txt",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := listStandardCandidates(dir, map[string]bool{selectedPath: true})
	if err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(got))
	var selected bool
	for _, candidate := range got {
		names = append(names, candidate.Name)
		if candidate.Path == selectedPath {
			selected = candidate.Selected
		}
	}

	wantNames := []string{"a.md", "b.markdown", "c.mdx"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("expected markdown candidates %v, got %v", wantNames, names)
	}
	if !selected {
		t.Fatalf("expected selected candidate flag to be preserved")
	}
}

func TestExpandDirectoryInput(t *testing.T) {
	projectDir := "/tmp/project"

	got, err := expandDirectoryInput("docs/standards", projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean("/tmp/project/docs/standards") {
		t.Fatalf("unexpected expanded relative path: %q", got)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err = expandDirectoryInput("~/standards", projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(home, "standards") {
		t.Fatalf("unexpected expanded home path: %q", got)
	}
}

func TestAutocompleteDirectory(t *testing.T) {
	projectDir := t.TempDir()
	for _, name := range []string{"alpha", "alpine", "beta"} {
		if err := os.Mkdir(filepath.Join(projectDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got, err := autocompleteDirectory("al", projectDir)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		filepath.Join(projectDir, "alpha") + string(filepath.Separator),
		filepath.Join(projectDir, "alpine") + string(filepath.Separator),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected directory suggestions %v, got %v", want, got)
	}
}

func TestRelativeDirectoryDisplayAndVisibleListWindow(t *testing.T) {
	projectDir := "/tmp/project"
	if got := relativeDirectoryDisplay("/tmp/project/docs", projectDir); got != filepath.Join("docs") {
		t.Fatalf("unexpected relative directory display: %q", got)
	}
	if got := relativeDirectoryDisplay("/outside/project", projectDir); !strings.HasPrefix(got, "/outside/project") {
		t.Fatalf("expected absolute fallback, got %q", got)
	}

	start, end := visibleListWindow(20, 10, 5)
	if start != 8 || end != 13 {
		t.Fatalf("unexpected visible window: start=%d end=%d", start, end)
	}
}
