package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	termansi "github.com/charmbracelet/x/ansi"

	"agentswitcher/internal/agent"
	"agentswitcher/internal/store"
)

func TestRenderMessagesShowsPendingPromptAndThinkingPlaceholder(t *testing.T) {
	spin := spinner.New()
	spin.Spinner = spinner.Dot

	model := &Model{
		activeSession: store.Session{Agent: agent.Codex},
		pendingPrompt: "Write repository tests.",
		loading:       true,
		spinner:       spin,
	}
	model.viewport.Width = 80

	got := model.renderMessages()

	wantContains := []string{
		"Write repository tests.",
		"Thinking...",
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered messages missing %q\nfull output:\n%s", want, got)
		}
	}
	if strings.Contains(got, "USER") {
		t.Fatalf("rendered messages should not include USER label\nfull output:\n%s", got)
	}
	if strings.Contains(got, "CODEX") {
		t.Fatalf("rendered messages should not include assistant agent label\nfull output:\n%s", got)
	}
}

func TestRenderStandardsSummaryShowsSelectedStandards(t *testing.T) {
	model := &Model{
		width: 80,
		standards: []store.Standard{
			{Path: "/tmp/docs/alpha.md"},
			{Path: "/tmp/docs/beta.md"},
		},
	}

	got := model.renderStandardsSummary()
	for _, want := range []string{"Selected Standards", "alpha.md", "beta.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("standards summary missing %q\nfull output:\n%s", want, got)
		}
	}
}

func TestUpdateStandardsPersistsSelectedFiles(t *testing.T) {
	projectDir := t.TempDir()
	standardsDir := filepath.Join(projectDir, "standards")
	if err := os.Mkdir(standardsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	selectedFile := filepath.Join(standardsDir, "alpha.md")
	if err := os.WriteFile(selectedFile, []byte("be direct"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo, err := store.NewRepository(filepath.Join(projectDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	model, err := NewModel(repo)
	if err != nil {
		t.Fatal(err)
	}
	model.projectDir = projectDir

	session, err := repo.CreateSession(context.Background(), agent.Claude)
	if err != nil {
		t.Fatal(err)
	}
	if err := model.openSession(session); err != nil {
		t.Fatal(err)
	}

	model.openStandardsPicker()
	model.standardsPicker.directoryInput = standardsDir

	updatedModel, _ := model.updateStandards(tea.KeyMsg{Type: tea.KeyEnter})
	model = updatedModel.(*Model)
	if model.standardsPicker.IsDirectoryFocus() {
		t.Fatalf("expected standards picker to move into file selection mode")
	}

	updatedModel, _ = model.updateStandards(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	model = updatedModel.(*Model)
	if got := model.standardsPicker.SelectedPaths(); !reflect.DeepEqual(got, []string{selectedFile}) {
		t.Fatalf("expected selected file %v, got %v", []string{selectedFile}, got)
	}

	updatedModel, _ = model.updateStandards(tea.KeyMsg{Type: tea.KeyEnter})
	model = updatedModel.(*Model)
	if model.screen != screenChat {
		t.Fatalf("expected standards picker to return to chat screen")
	}

	standards, err := repo.ListStandards(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := selectedStandardPaths(selectedStandardPathSet(standards)); !reflect.DeepEqual(got, []string{selectedFile}) {
		t.Fatalf("expected persisted standards %v, got %v", []string{selectedFile}, got)
	}
	if got := selectedStandardPaths(selectedStandardPathSet(model.standards)); !reflect.DeepEqual(got, []string{selectedFile}) {
		t.Fatalf("expected active session standards %v, got %v", []string{selectedFile}, got)
	}
}

func TestUpdateAgentPickerSwitchesSessionAgentInPlace(t *testing.T) {
	projectDir := t.TempDir()
	repo, err := store.NewRepository(filepath.Join(projectDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	model, err := NewModel(repo)
	if err != nil {
		t.Fatal(err)
	}
	model.projectDir = projectDir

	session, err := repo.CreateSession(context.Background(), agent.Codex)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddExchange(context.Background(), session.ID, "keep history", "history stays"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceStandards(context.Background(), session.ID, []string{"/tmp/rules.md"}); err != nil {
		t.Fatal(err)
	}
	if err := model.openSession(session); err != nil {
		t.Fatal(err)
	}

	model.openAgentPicker()
	if model.screen != screenAgentPicker {
		t.Fatalf("expected agent picker screen to open")
	}

	model.agentPickerIdx = model.agentIndexFor(agent.Claude)
	updatedModel, _ := model.updateAgentPicker(tea.KeyMsg{Type: tea.KeyEnter})
	model = updatedModel.(*Model)

	if model.screen != screenChat {
		t.Fatalf("expected to return to chat after switching agent")
	}
	if model.activeSession.ID != session.ID {
		t.Fatalf("expected session id %q to be preserved, got %q", session.ID, model.activeSession.ID)
	}
	if model.activeSession.Agent != agent.Claude {
		t.Fatalf("expected active agent %q, got %q", agent.Claude, model.activeSession.Agent)
	}
	if len(model.messages) != 2 {
		t.Fatalf("expected existing history to remain loaded, got %d messages", len(model.messages))
	}
	if len(model.standards) != 1 || model.standards[0].Path != "/tmp/rules.md" {
		t.Fatalf("expected standards to remain attached after agent switch, got %#v", model.standards)
	}

	updated, err := repo.GetSession(context.Background(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Agent != agent.Claude {
		t.Fatalf("expected persisted session agent %q, got %q", agent.Claude, updated.Agent)
	}
}

func TestSyncInputHeightGrowsAndCaps(t *testing.T) {
	model, err := NewModel(nil)
	if err != nil {
		t.Fatal(err)
	}

	model.width = 120
	model.height = 40
	model.input.SetValue(strings.Join([]string{
		"one",
		"two",
		"three",
		"four",
		"five",
		"six",
		"seven",
		"eight",
		"nine",
	}, "\n"))

	model.syncInputHeight()

	if got := model.input.Height(); got != maxInputHeight {
		t.Fatalf("expected input height to cap at %d, got %d", maxInputHeight, got)
	}
}

func TestAssistantMarkdownStyleUsesWhiteDocumentText(t *testing.T) {
	style := assistantMarkdownStyle()

	if style.Document.StylePrimitive.Color == nil || *style.Document.StylePrimitive.Color != "255" {
		t.Fatalf("expected white assistant document text, got %#v", style.Document.StylePrimitive.Color)
	}
	if style.Document.StylePrimitive.BlockPrefix != "" || style.Document.StylePrimitive.BlockSuffix != "" {
		t.Fatalf("expected assistant document markdown to render without extra block padding, got prefix=%q suffix=%q", style.Document.StylePrimitive.BlockPrefix, style.Document.StylePrimitive.BlockSuffix)
	}
	if style.Document.Margin == nil || *style.Document.Margin != 0 {
		t.Fatalf("expected assistant document markdown to render without side margins, got %#v", style.Document.Margin)
	}

	if style.Code.StylePrimitive.Color == nil || *style.Code.StylePrimitive.Color != "203" {
		t.Fatalf("expected inline code styling to remain unchanged, got %#v", style.Code.StylePrimitive.Color)
	}

	if style.CodeBlock.StyleBlock.StylePrimitive.Color == nil || *style.CodeBlock.StyleBlock.StylePrimitive.Color != "244" {
		t.Fatalf("expected code block styling to remain unchanged, got %#v", style.CodeBlock.StyleBlock.StylePrimitive.Color)
	}
}

func TestRenderMarkdownPlainTextHasNoLeadingPadding(t *testing.T) {
	got := termansi.Strip(renderMarkdown("Opus 4.6, bro.", 72))
	if strings.HasPrefix(got, "\n") || strings.HasPrefix(got, " ") {
		t.Fatalf("expected plain assistant markdown to stay flush on the left, got %q", got)
	}
	if strings.TrimRight(got, " ") != "Opus 4.6, bro." {
		t.Fatalf("expected plain assistant markdown content to remain unchanged, got %q", got)
	}
}
