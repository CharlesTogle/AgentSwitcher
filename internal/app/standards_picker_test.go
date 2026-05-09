package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	termansi "github.com/charmbracelet/x/ansi"
)

func TestStandardsPickerDirectoryFlowAndSelection(t *testing.T) {
	projectDir := t.TempDir()
	standardsDir := filepath.Join(projectDir, "standards")
	if err := os.Mkdir(standardsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha.md", "beta.md", "skip.txt"} {
		if err := os.WriteFile(filepath.Join(standardsDir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	picker := NewStandardsPicker(projectDir, nil)
	picker.directoryInput = filepath.Join(projectDir, "standards")

	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	picker = updated

	if picker.directoryFocus != standardsFocusFiles {
		t.Fatalf("expected file focus after loading directory")
	}
	if len(picker.candidates) != 2 {
		t.Fatalf("expected 2 markdown candidates, got %d", len(picker.candidates))
	}

	updated, _ = picker.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	picker = updated
	got := picker.SelectedPaths()
	want := []string{filepath.Join(standardsDir, "alpha.md")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected selected paths %v, got %v", want, got)
	}

	summary := picker.Summary()
	if !strings.Contains(summary, "1 markdown file(s)") && !strings.Contains(summary, "2 markdown file(s)") {
		t.Fatalf("unexpected picker summary: %q", summary)
	}
	if !strings.Contains(summary, "alpha.md") {
		t.Fatalf("expected selected file to appear in summary: %q", summary)
	}
}

func TestStandardsPickerAutocompleteCyclesSuggestions(t *testing.T) {
	projectDir := t.TempDir()
	for _, name := range []string{"docs-one", "docs-two"} {
		if err := os.Mkdir(filepath.Join(projectDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	picker := NewStandardsPicker(projectDir, nil)
	picker.directoryInput = filepath.Join(projectDir, "docs")

	if err := picker.refreshDirectorySuggestions(); err != nil {
		t.Fatal(err)
	}
	if len(picker.directorySuggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(picker.directorySuggestions))
	}

	first := picker.directorySuggestions[0]
	second := picker.directorySuggestions[1]
	picker.acceptDirectorySuggestion()
	if picker.directoryInput != first {
		t.Fatalf("expected first suggestion to be accepted, got %q", picker.directoryInput)
	}
	picker.acceptDirectorySuggestion()
	if picker.directoryInput != second {
		t.Fatalf("expected second suggestion to be accepted on next cycle, got %q", picker.directoryInput)
	}
}

func TestStandardsPickerLoadSelectedStandards(t *testing.T) {
	projectDir := t.TempDir()
	filePath := filepath.Join(projectDir, "RULES.md")
	if err := os.WriteFile(filePath, []byte("be precise\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	picker := NewStandardsPicker(projectDir, []string{filePath})
	standards, err := picker.LoadSelectedStandards()
	if err != nil {
		t.Fatal(err)
	}

	if len(standards) != 1 {
		t.Fatalf("expected 1 loaded standard, got %d", len(standards))
	}
	if standards[0].Content != "be precise" {
		t.Fatalf("unexpected standard content: %q", standards[0].Content)
	}
}

func TestStandardsPickerCancel(t *testing.T) {
	picker := NewStandardsPicker(t.TempDir(), nil)
	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !updated.Cancelled() {
		t.Fatalf("expected picker to be cancelled")
	}
}

func TestStandardsPickerRenderDirectoryBlockStacksInputBelowLabel(t *testing.T) {
	projectDir := t.TempDir()
	picker := NewStandardsPicker(projectDir, nil)
	picker.directoryInput = "typed/path"
	picker.directory = projectDir

	lines := strings.Split(termansi.Strip(picker.renderDirectoryBlock()), "\n")

	labelLine := -1
	inputLine := -1
	for i, line := range lines {
		if labelLine == -1 && strings.Contains(line, "Directory") {
			labelLine = i
		}
		if inputLine == -1 && strings.Contains(line, "typed/path") {
			inputLine = i
		}
	}

	if labelLine == -1 {
		t.Fatalf("expected directory block to include label, got:\n%s", strings.Join(lines, "\n"))
	}
	if inputLine == -1 {
		t.Fatalf("expected directory block to include input value, got:\n%s", strings.Join(lines, "\n"))
	}
	if inputLine <= labelLine {
		t.Fatalf("expected directory input to render below the label, got:\n%s", strings.Join(lines, "\n"))
	}
}
