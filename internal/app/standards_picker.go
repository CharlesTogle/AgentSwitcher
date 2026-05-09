package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"agentswitcher/internal/store"
)

type StandardsPicker struct {
	projectDir string

	directoryInput string
	directory      string
	directoryFocus standardsFocus

	candidates []standardCandidate
	selected   map[string]bool

	cursor               int
	directorySuggestions []string
	suggestionIndex      int

	width  int
	height int

	status  string
	errText string

	done      bool
	cancelled bool
}

func NewStandardsPicker(projectDir string, selected []string) StandardsPicker {
	if strings.TrimSpace(projectDir) == "" {
		projectDir = "."
	}

	selectedSet := make(map[string]bool, len(selected))
	for _, path := range selected {
		selectedSet[path] = true
	}

	picker := StandardsPicker{
		projectDir:     projectDir,
		selected:       selectedSet,
		directory:      projectDir,
		directoryInput: relativeDirectoryDisplay(projectDir, projectDir),
		directoryFocus: standardsFocusDirectory,
		status:         "Pick a standards directory. Tab autocompletes directories.",
	}
	_ = picker.LoadDirectory(projectDir)
	return picker
}

func (p StandardsPicker) Init() tea.Cmd {
	return nil
}

func (p StandardsPicker) Update(msg tea.Msg) (StandardsPicker, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.clampCursor()
		return p, nil
	case tea.KeyMsg:
		return p.updateKey(msg)
	default:
		return p, nil
	}
}

func (p StandardsPicker) updateKey(msg tea.KeyMsg) (StandardsPicker, tea.Cmd) {
	if p.done || p.cancelled {
		return p, nil
	}

	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		p.cancelled = true
		p.status = "Standards picker closed."
		return p, nil
	case tea.KeyEnter:
		if p.directoryFocus == standardsFocusDirectory {
			if err := p.applyDirectoryInput(); err != nil {
				p.errText = err.Error()
				return p, nil
			}
			if err := p.LoadDirectory(p.directory); err != nil {
				p.errText = err.Error()
				return p, nil
			}
			p.directoryFocus = standardsFocusFiles
			p.status = "Select markdown files with space. Enter confirms."
			return p, nil
		}
		p.done = true
		p.status = "Standards selection saved."
		return p, nil
	case tea.KeyTab:
		if p.directoryFocus == standardsFocusDirectory {
			if len(p.directorySuggestions) == 0 {
				if err := p.refreshDirectorySuggestions(); err != nil {
					p.errText = err.Error()
					return p, nil
				}
			}
			p.acceptDirectorySuggestion()
			return p, nil
		}
		p.directoryFocus = standardsFocusDirectory
		return p, nil
	case tea.KeyBackspace, tea.KeyDelete:
		if p.directoryFocus == standardsFocusDirectory && len(p.directoryInput) > 0 {
			p.directoryInput = p.directoryInput[:len(p.directoryInput)-1]
			p.directorySuggestions = nil
			p.suggestionIndex = 0
			p.errText = ""
			return p, nil
		}
	case tea.KeySpace:
		if p.directoryFocus == standardsFocusDirectory {
			p.directoryInput += " "
			p.directorySuggestions = nil
			p.suggestionIndex = 0
			p.errText = ""
			return p, nil
		}
		p.toggleCurrentCandidate()
		return p, nil
	case tea.KeyRunes:
		if p.directoryFocus == standardsFocusDirectory {
			p.directoryInput += msg.String()
			p.directorySuggestions = nil
			p.suggestionIndex = 0
			p.errText = ""
			return p, nil
		}
		if msg.String() == " " {
			p.toggleCurrentCandidate()
			return p, nil
		}
	case tea.KeyUp, tea.KeyCtrlP:
		if p.directoryFocus == standardsFocusFiles {
			p.moveCursor(-1)
			return p, nil
		}
	case tea.KeyDown, tea.KeyCtrlN:
		if p.directoryFocus == standardsFocusFiles {
			p.moveCursor(1)
			return p, nil
		}
	}

	return p, nil
}

func (p *StandardsPicker) applyDirectoryInput() error {
	resolved, err := expandDirectoryInput(p.directoryInput, p.projectDir)
	if err != nil {
		return err
	}
	p.directory = resolved
	return nil
}

func (p *StandardsPicker) refreshDirectorySuggestions() error {
	suggestions, err := autocompleteDirectory(p.directoryInput, p.projectDir)
	if err != nil {
		return err
	}
	p.directorySuggestions = suggestions
	p.suggestionIndex = 0
	return nil
}

func (p *StandardsPicker) acceptDirectorySuggestion() {
	if len(p.directorySuggestions) == 0 {
		return
	}
	if p.suggestionIndex < 0 || p.suggestionIndex >= len(p.directorySuggestions) {
		p.suggestionIndex = 0
	}
	p.directoryInput = p.directorySuggestions[p.suggestionIndex]
	if len(p.directorySuggestions) > 1 {
		p.suggestionIndex = (p.suggestionIndex + 1) % len(p.directorySuggestions)
	}
}

func (p *StandardsPicker) LoadDirectory(dir string) error {
	dir = filepath.Clean(dir)
	candidates, err := listStandardCandidates(dir, p.selected)
	if err != nil {
		return err
	}

	p.directory = dir
	p.directoryInput = relativeDirectoryDisplay(dir, p.projectDir)
	p.candidates = candidates
	p.directorySuggestions = nil
	p.suggestionIndex = 0
	p.clampCursor()
	if len(p.candidates) == 0 {
		p.status = "No markdown standards found in this directory."
	} else {
		p.status = fmt.Sprintf("Loaded %d markdown files.", len(p.candidates))
	}
	p.errText = ""
	return nil
}

func (p *StandardsPicker) moveCursor(delta int) {
	if len(p.candidates) == 0 {
		p.cursor = 0
		return
	}
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.candidates) {
		p.cursor = len(p.candidates) - 1
	}
}

func (p *StandardsPicker) clampCursor() {
	if len(p.candidates) == 0 {
		p.cursor = 0
		return
	}
	if p.cursor >= len(p.candidates) {
		p.cursor = len(p.candidates) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (p *StandardsPicker) toggleCurrentCandidate() {
	if len(p.candidates) == 0 {
		return
	}
	current := p.candidates[p.cursor]
	p.selected[current.Path] = !p.selected[current.Path]
	p.candidates[p.cursor].Selected = p.selected[current.Path]
	p.status = fmt.Sprintf("%s %s", map[bool]string{true: "Selected", false: "Deselected"}[p.selected[current.Path]], current.Name)
}

func (p StandardsPicker) SelectedPaths() []string {
	paths := selectedStandardPaths(p.selected)
	return paths
}

func (p StandardsPicker) SelectedCount() int {
	return len(p.SelectedPaths())
}

func (p StandardsPicker) CurrentDirectory() string {
	return p.directory
}

func (p StandardsPicker) Summary() string {
	var b strings.Builder
	if len(p.candidates) == 0 {
		b.WriteString("No markdown standards found.")
		return b.String()
	}
	fmt.Fprintf(&b, "%d markdown file(s) in %s", len(p.candidates), relativeDirectoryDisplay(p.directory, p.projectDir))
	if selected := p.SelectedPaths(); len(selected) > 0 {
		b.WriteString("\nSelected:")
		limit := len(selected)
		if limit > 5 {
			limit = 5
		}
		for i := 0; i < limit; i++ {
			fmt.Fprintf(&b, "\n- %s", selected[i])
		}
		if len(selected) > limit {
			fmt.Fprintf(&b, "\n- +%d more", len(selected)-limit)
		}
	}
	return b.String()
}

func (p StandardsPicker) Candidates() []standardCandidate {
	out := make([]standardCandidate, len(p.candidates))
	copy(out, p.candidates)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func (p StandardsPicker) Complete() bool {
	return p.done
}

func (p StandardsPicker) Cancelled() bool {
	return p.cancelled
}

func (p StandardsPicker) IsDirectoryFocus() bool {
	return p.directoryFocus == standardsFocusDirectory
}

func (p StandardsPicker) StandardPaths() []string {
	return p.SelectedPaths()
}

func (p StandardsPicker) LoadSelectedStandards() ([]promptStandard, error) {
	records, err := loadPromptStandards(p.SelectedStandards())
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (p StandardsPicker) SelectedStandards() []store.Standard {
	paths := p.SelectedPaths()
	out := make([]store.Standard, 0, len(paths))
	for _, path := range paths {
		out = append(out, store.Standard{Path: path})
	}
	return out
}
