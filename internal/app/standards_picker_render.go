package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (p StandardsPicker) View() string {
	content := strings.Join([]string{
		standardsPickerTitleStyle.Render("Standards Picker"),
		standardsPickerHintStyle.Render("Tab autocomplete in directory mode. Enter opens a directory or confirms the selection. Space toggles markdown files."),
		p.renderDirectoryBlock(),
		p.renderFileBlock(),
	}, "\n\n")

	return renderPageWithFooter(p.width, p.height, content, p.renderFooter())
}

func (p StandardsPicker) renderDirectoryBlock() string {
	focusLabel := "Directory"
	if p.directoryFocus == standardsFocusDirectory {
		focusLabel = standardsPickerActiveStyle.Render("Directory")
	}

	body := []string{
		fmt.Sprintf("%s: %s", focusLabel, standardsPickerInputStyle.Render(p.directoryInput)),
		fmt.Sprintf("Resolved: %s", standardsPickerMutedStyle.Render(relativeDirectoryDisplay(p.directory, p.projectDir))),
	}

	if len(p.directorySuggestions) > 0 {
		var suggestions []string
		for i, suggestion := range p.directorySuggestions {
			line := fmt.Sprintf("%s  %s", standardsPickerSuggestionIndexStyle.Render(fmt.Sprintf("%d", i+1)), suggestion)
			if i == p.suggestionIndex {
				line = standardsPickerSelectedStyle.Render(line)
			}
			suggestions = append(suggestions, line)
		}
		body = append(body, "Autocomplete:", strings.Join(suggestions, "\n"))
	}

	if p.errText != "" {
		body = append(body, standardsPickerErrorStyle.Render(p.errText))
	}

	return standardsPickerPanelStyle.Render(strings.Join(body, "\n"))
}

func (p StandardsPicker) renderFileBlock() string {
	if p.directoryFocus == standardsFocusFiles {
		return standardsPickerPanelStyle.Render(p.renderFiles())
	}
	return standardsPickerPanelStyle.Render(p.renderFiles())
}

func (p StandardsPicker) renderFiles() string {
	if len(p.candidates) == 0 {
		return strings.Join([]string{
			standardsPickerSectionTitleStyle.Render("Markdown Files"),
			standardsPickerMutedStyle.Render("No markdown files were found in this directory."),
		}, "\n\n")
	}

	start, end := visibleListWindow(len(p.candidates), p.cursor, 10)
	lines := []string{standardsPickerSectionTitleStyle.Render("Markdown Files")}
	for i := start; i < end; i++ {
		candidate := p.candidates[i]
		checked := "[ ]"
		if p.selected[candidate.Path] {
			checked = "[x]"
		}

		line := fmt.Sprintf("%s %s", checked, candidate.Name)
		if i == p.cursor {
			if p.directoryFocus == standardsFocusFiles {
				line = standardsPickerSelectedStyle.Render(line)
			} else {
				line = standardsPickerActiveStyle.Render(line)
			}
		}
		lines = append(lines, line)
	}
	if end < len(p.candidates) {
		lines = append(lines, standardsPickerMutedStyle.Render(fmt.Sprintf("... %d more", len(p.candidates)-end)))
	}
	return strings.Join(lines, "\n")
}

func (p StandardsPicker) renderFooter() string {
	status := p.status
	if selected := p.SelectedPaths(); len(selected) > 0 {
		status = fmt.Sprintf("%s  %d selected", status, len(selected))
	}

	if p.done {
		status = "Standards selection saved."
	}
	if p.cancelled {
		status = "Standards picker closed."
	}

	items := []footerItem{
		{Key: "Tab", Label: "autocomplete/focus"},
		{Key: "Enter", Label: "open/save"},
		{Key: "Space", Label: "toggle file"},
		{Key: "↑/↓", Label: "move"},
		{Key: "Esc", Label: "cancel"},
	}

	return renderManualFooter(p.width, status, p.errText, items)
}

var (
	standardsPickerPageStyle  = lipgloss.NewStyle().Padding(1, 2)
	standardsPickerPanelStyle = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(lipgloss.Color("63")).
					Padding(1, 2)
	standardsPickerTitleStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("230"))
	standardsPickerHintStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("245"))
	standardsPickerSectionTitleStyle = lipgloss.NewStyle().
						Bold(true).
						Foreground(lipgloss.Color("229"))
	standardsPickerInputStyle = lipgloss.NewStyle().
					Border(lipgloss.NormalBorder()).
					BorderForeground(lipgloss.Color("242")).
					Padding(0, 1)
	standardsPickerMutedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("244"))
	standardsPickerSelectedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("230")).
					Background(lipgloss.Color("33")).
					Bold(true)
	standardsPickerActiveStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("153")).
					Bold(true)
	standardsPickerErrorStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("204"))
	standardsPickerSuccessStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("120")).
					Bold(true)
	standardsPickerSuggestionIndexStyle = lipgloss.NewStyle().
						Foreground(lipgloss.Color("245")).
						Bold(true)
)
