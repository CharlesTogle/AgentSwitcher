package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type footerItem struct {
	Key   string
	Label string
}

func renderPageWithFooter(width, height int, content, footer string) string {
	renderedContent := pageStyle.Render(content)
	renderedFooter := footerContainerStyle.Render(footer)

	totalHeight := lipgloss.Height(renderedContent) + lipgloss.Height(renderedFooter)
	if height > 0 && totalHeight < height {
		return renderedContent + strings.Repeat("\n", height-totalHeight) + renderedFooter
	}

	return renderedContent + "\n" + renderedFooter
}

func renderManualFooter(width int, status string, errText string, items []footerItem) string {
	innerWidth := max(20, width-4)
	lines := []string{
		footerStatusStyle.Width(innerWidth).Render(" " + status),
	}

	if strings.TrimSpace(errText) != "" {
		lines = append(lines, footerErrorStyle.Width(innerWidth).Render(" "+errText))
	}

	lines = append(lines, renderFooterHelpLines(innerWidth, items)...)
	return strings.Join(lines, "\n")
}

func renderFooterHelpLines(width int, items []footerItem) []string {
	if len(items) == 0 {
		return nil
	}

	var lines []string
	current := ""

	for _, item := range items {
		segment := renderFooterItem(item)
		candidate := segment
		if current != "" {
			candidate = current + footerDividerStyle.Render("   ") + segment
		}

		if current != "" && lipgloss.Width(candidate) > width {
			lines = append(lines, footerHelpLineStyle.Width(width).Render(" "+current))
			current = segment
			continue
		}

		current = candidate
	}

	if current != "" {
		lines = append(lines, footerHelpLineStyle.Width(width).Render(" "+current))
	}

	return lines
}

func renderFooterItem(item footerItem) string {
	return footerKeyStyle.Render(item.Key) + " " + footerLabelStyle.Render(item.Label)
}

var (
	footerContainerStyle = lipgloss.NewStyle().Padding(0, 2)

	footerStatusStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("214")).
				Foreground(lipgloss.Color("234")).
				Bold(true)

	footerHelpLineStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("236")).
				Foreground(lipgloss.Color("252"))

	footerErrorStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("52")).
				Foreground(lipgloss.Color("224")).
				Bold(true)

	footerKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)

	footerLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("254"))

	footerDividerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("243"))
)
