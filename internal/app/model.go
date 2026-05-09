package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"

	"agentswitcher/internal/agent"
	"agentswitcher/internal/store"
)

const (
	sessionListLimit = 20
	requestTimeout   = 10 * time.Minute
	minInputHeight   = 1
	maxInputHeight   = 8
)

type screen int
type homeFocus int

const (
	screenHome screen = iota
	screenChat
	screenStandards
	screenAgentPicker
)

const (
	focusAgents homeFocus = iota
	focusSessions
)

type agentResponseMsg struct {
	sessionID string
	prompt    string
	reply     string
	warning   string
	err       error
}

type compactionDoneMsg struct {
	sessionID            string
	summary              string
	compactedPromptCount int
	err                  error
}

type homeDataMsg struct {
	sessions []store.Session
	err      error
}

type Model struct {
	repo *store.Repository

	width  int
	height int

	screen screen

	agents             []agent.Definition
	selectedAgentIndex int
	homeFocus          homeFocus
	allSessions        []store.Session
	selectedSessionIdx int
	agentPickerIdx     int

	activeSession store.Session
	messages      []store.Message
	standards     []store.Standard
	pendingPrompt string
	viewport      viewport.Model
	input         textarea.Model
	spinner       spinner.Model
	projectDir    string

	standardsPicker StandardsPicker

	promptHistory []string
	historyIndex  int    // -1 means not browsing history
	historySaved  string // stash current input when entering history

	loading    bool
	compacting bool
	status     string
	errText    string
}

func NewModel(repo *store.Repository) (*Model, error) {
	projectDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}

	input := textarea.New()
	input.Placeholder = "Type a prompt. Enter send. Alt+Enter newline. ↑/↓ history."
	input.Focus()
	input.Prompt = "┃ "
	input.CharLimit = 0
	input.MaxHeight = maxInputHeight
	input.SetHeight(minInputHeight)
	input.ShowLineNumbers = false

	spin := spinner.New()
	spin.Spinner = spinner.Dot

	vp := viewport.New(0, 0)

	return &Model{
		repo:         repo,
		agents:       agent.Definitions(),
		homeFocus:    focusAgents,
		input:        input,
		viewport:     vp,
		spinner:      spin,
		projectDir:   projectDir,
		historyIndex: -1,
		status:       "Loading sessions...",
	}, nil
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadHomeDataCmd(), m.spinner.Tick)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeComponents()
		if m.screen == screenStandards {
			var cmd tea.Cmd
			m.standardsPicker, cmd = m.standardsPicker.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if m.screen == screenAgentPicker {
			return m.updateAgentPicker(msg)
		}
		if m.screen == screenStandards {
			return m.updateStandards(msg)
		}
		if m.screen == screenHome {
			return m.updateHome(msg)
		}
		return m.updateChat(msg)

	case spinner.TickMsg:
		if m.loading || m.compacting {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			m.syncViewport()
			return m, cmd
		}
		return m, nil

	case homeDataMsg:
		if msg.err != nil {
			m.errText = msg.err.Error()
			m.status = "Failed to load sessions."
			return m, nil
		}
		m.allSessions = msg.sessions
		m.status = "Enter starts a new session. Tab switches focus."
		m.clampSelection()
		return m, nil

	case agentResponseMsg:
		m.loading = false
		m.pendingPrompt = ""
		if msg.err != nil {
			m.errText = msg.err.Error()
			if msg.warning != "" {
				m.errText += "\n" + msg.warning
			}
			m.input.SetValue(msg.prompt)
			m.syncInputHeight()
			m.syncViewport()
			m.status = "Request failed."
			return m, nil
		}

		updated, err := m.repo.AddExchange(context.Background(), msg.sessionID, msg.prompt, msg.reply)
		if err != nil {
			m.errText = err.Error()
			return m, nil
		}

		m.activeSession = updated
		m.status = "Reply received."
		if msg.warning != "" {
			m.status = "Reply received with warnings."
			m.errText = msg.warning
		} else {
			m.errText = ""
		}

		if err := m.reloadActiveSession(); err != nil {
			m.errText = err.Error()
			return m, nil
		}

		cmds := []tea.Cmd{m.loadHomeDataCmd()}
		if m.repo.NeedCompaction(updated) {
			m.compacting = true
			m.status = "Compacting session..."
			cmds = append(cmds, m.compactSessionCmd(updated))
		}
		return m, tea.Batch(cmds...)

	case compactionDoneMsg:
		m.compacting = false
		if msg.err != nil {
			m.errText = msg.err.Error()
			m.status = "Compaction failed."
			return m, nil
		}

		updated, err := m.repo.SaveCompaction(
			context.Background(),
			msg.sessionID,
			msg.summary,
			msg.compactedPromptCount,
		)
		if err != nil {
			m.errText = err.Error()
			return m, nil
		}

		m.activeSession = updated
		m.status = "Session compacted."
		if err := m.reloadActiveSession(); err != nil {
			m.errText = err.Error()
			return m, nil
		}
		return m, m.loadHomeDataCmd()
	}

	switch msg.(type) {
	case tea.MouseMsg:
		if m.screen == screenChat {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if m.screen == screenChat {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.syncInputHeight()
		return m, cmd
	}

	return m, nil
}

func (m *Model) View() string {
	if m.screen == screenAgentPicker {
		return m.agentPickerView()
	}
	if m.screen == screenStandards {
		return m.standardsPicker.View()
	}
	if m.screen == screenChat {
		return m.chatView()
	}
	return m.homeView()
}

func (m *Model) updateHome(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.loading || m.compacting {
		switch msg.String() {
		case "q":
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "tab":
		if m.homeFocus == focusAgents {
			m.homeFocus = focusSessions
		} else {
			m.homeFocus = focusAgents
		}
	case "up", "k":
		if m.homeFocus == focusAgents {
			if m.selectedAgentIndex > 0 {
				m.selectedAgentIndex--
			}
		} else {
			m.moveSessionSelection(-1)
		}
	case "down", "j":
		if m.homeFocus == focusAgents {
			if m.selectedAgentIndex < len(m.agents)-1 {
				m.selectedAgentIndex++
			}
		} else {
			m.moveSessionSelection(1)
		}
	case "r":
		m.status = "Refreshing sessions..."
		return m, m.loadHomeDataCmd()
	case "enter":
		if m.homeFocus == focusSessions {
			session, ok := m.currentSelectedSession()
			if ok {
				if err := m.openSession(session); err != nil {
					m.errText = err.Error()
					return m, nil
				}
				return m, nil
			}
		}
		session, err := m.repo.CreateSession(context.Background(), m.currentAgent().Kind)
		if err != nil {
			m.errText = err.Error()
			return m, nil
		}
		if err := m.openSession(session); err != nil {
			m.errText = err.Error()
			return m, nil
		}
		return m, m.loadHomeDataCmd()
	}

	return m, nil
}

func (m *Model) updateChat(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.loading || m.compacting {
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEsc:
		m.screen = screenHome
		m.status = "Returned home."
		return m, m.loadHomeDataCmd()
	case tea.KeyCtrlT:
		m.openStandardsPicker()
		return m, nil
	case tea.KeyCtrlG:
		m.openAgentPicker()
		return m, nil
	case tea.KeyEnter:
		if msg.Alt {
			m.input.InsertString("\n")
			m.syncInputHeight()
			return m, nil
		}
		prompt := strings.TrimSpace(m.input.Value())
		if prompt == "" {
			return m, nil
		}
		m.promptHistory = append(m.promptHistory, prompt)
		m.historyIndex = -1
		m.historySaved = ""
		m.loading = true
		m.status = fmt.Sprintf("Sending prompt to %s...", describeAgent(m.activeSession.Agent))
		m.errText = ""
		m.pendingPrompt = prompt
		m.input.Reset()
		m.syncViewport()
		return m, tea.Batch(m.runAgentCmd(m.activeSession, prompt), m.spinner.Tick)
	case tea.KeyPgUp:
		m.viewport.HalfViewUp()
		return m, nil
	case tea.KeyPgDown:
		m.viewport.HalfViewDown()
		return m, nil
	case tea.KeyUp:
		if len(m.promptHistory) > 0 {
			if m.historyIndex == -1 {
				m.historySaved = m.input.Value()
				m.historyIndex = len(m.promptHistory) - 1
			} else if m.historyIndex > 0 {
				m.historyIndex--
			}
			m.input.SetValue(m.promptHistory[m.historyIndex])
			m.input.CursorEnd()
			m.syncInputHeight()
			return m, nil
		}
	case tea.KeyDown:
		if m.historyIndex != -1 {
			if m.historyIndex < len(m.promptHistory)-1 {
				m.historyIndex++
				m.input.SetValue(m.promptHistory[m.historyIndex])
			} else {
				m.historyIndex = -1
				m.input.SetValue(m.historySaved)
				m.historySaved = ""
			}
			m.input.CursorEnd()
			m.syncInputHeight()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.syncInputHeight()
	return m, cmd
}

func (m *Model) updateStandards(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	picker, cmd := m.standardsPicker.Update(msg)
	m.standardsPicker = picker

	if m.standardsPicker.cancelled {
		m.screen = screenChat
		m.status = "Standards picker closed."
		m.errText = ""
		return m, nil
	}

	if !m.standardsPicker.done {
		return m, cmd
	}

	paths := m.standardsPicker.SelectedPaths()
	if err := m.repo.ReplaceStandards(context.Background(), m.activeSession.ID, paths); err != nil {
		m.screen = screenChat
		m.errText = err.Error()
		m.status = "Failed to save standards."
		return m, nil
	}

	if err := m.reloadActiveSession(); err != nil {
		m.screen = screenChat
		m.errText = err.Error()
		m.status = "Failed to reload session."
		return m, nil
	}

	m.screen = screenChat
	m.errText = ""
	m.status = fmt.Sprintf("Saved %d standards.", len(paths))
	return m, m.loadHomeDataCmd()
}

func (m *Model) openAgentPicker() {
	m.agentPickerIdx = m.agentIndexFor(m.activeSession.Agent)
	m.screen = screenAgentPicker
	m.status = "Select agent engine. History is preserved."
}

func (m *Model) agentIndexFor(kind agent.Kind) int {
	for i, def := range m.agents {
		if def.Kind == kind {
			return i
		}
	}
	return 0
}

func (m *Model) updateAgentPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.screen = screenChat
		m.status = "Agent switch cancelled."
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP:
		if m.agentPickerIdx > 0 {
			m.agentPickerIdx--
		}
	case tea.KeyDown, tea.KeyCtrlN:
		if m.agentPickerIdx < len(m.agents)-1 {
			m.agentPickerIdx++
		}
	case tea.KeyEnter:
		selected := m.agents[m.agentPickerIdx]
		updated, err := m.repo.UpdateSessionAgent(context.Background(), m.activeSession.ID, selected.Kind)
		if err != nil {
			m.screen = screenChat
			m.errText = err.Error()
			m.status = "Failed to switch agent."
			return m, nil
		}
		m.activeSession = updated
		m.screen = screenChat
		m.status = fmt.Sprintf("Switched to %s.", selected.Label)
		m.errText = ""
		return m, m.loadHomeDataCmd()
	}
	return m, nil
}

func (m *Model) loadHomeDataCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, err := m.repo.ListAllSessions(context.Background(), sessionListLimit)
		if err != nil {
			return homeDataMsg{err: err}
		}
		return homeDataMsg{sessions: sessions}
	}
}

func (m *Model) runAgentCmd(session store.Session, prompt string) tea.Cmd {
	return func() tea.Msg {
		snapshot, err := m.repo.GetContextSnapshot(context.Background(), session.ID)
		if err != nil {
			return agentResponseMsg{sessionID: session.ID, prompt: prompt, err: err}
		}

		standards, err := loadPromptStandards(snapshot.Standards)
		if err != nil {
			return agentResponseMsg{sessionID: session.ID, prompt: prompt, err: err}
		}

		runner, err := agent.NewRunner(session.Agent)
		if err != nil {
			return agentResponseMsg{sessionID: session.ID, prompt: prompt, err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()

		result, err := runner.Run(
			ctx,
			buildAgentPromptWithStandards(snapshot.Session, standards, snapshot.RecentMessages, prompt),
		)
		return agentResponseMsg{
			sessionID: session.ID,
			prompt:    prompt,
			reply:     result.Output,
			warning:   result.Stderr,
			err:       err,
		}
	}
}

func (m *Model) compactSessionCmd(session store.Session) tea.Cmd {
	return func() tea.Msg {
		messages, err := m.repo.GetMessagesForCompaction(context.Background(), session.ID)
		if err != nil {
			return compactionDoneMsg{sessionID: session.ID, err: err}
		}

		selectedStandards, err := m.repo.ListStandards(context.Background(), session.ID)
		if err != nil {
			return compactionDoneMsg{sessionID: session.ID, err: err}
		}

		standards, err := loadPromptStandards(selectedStandards)
		if err != nil {
			return compactionDoneMsg{sessionID: session.ID, err: err}
		}

		runner, err := agent.NewRunner(session.Agent)
		if err != nil {
			return compactionDoneMsg{sessionID: session.ID, err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()

		result, err := runner.Run(ctx, buildCompactionPromptWithStandards(session, messages, standards))
		return compactionDoneMsg{
			sessionID:            session.ID,
			summary:              result.Output,
			compactedPromptCount: session.UserPromptCount,
			err:                  err,
		}
	}
}

func (m *Model) openSession(session store.Session) error {
	m.activeSession = session
	m.pendingPrompt = ""
	m.historyIndex = -1
	m.historySaved = ""
	if err := m.reloadActiveSession(); err != nil {
		return err
	}
	m.screen = screenChat
	m.status = fmt.Sprintf("Session %s opened.", session.ID)
	m.errText = ""
	m.input.Reset()
	m.input.Focus()
	m.syncInputHeight()
	return nil
}

func (m *Model) openStandardsPicker() {
	current := selectedStandardPaths(selectedStandardPathSet(m.standards))
	picker := NewStandardsPicker(m.projectDir, current)
	if dir := defaultStandardsDirectory(m.projectDir, m.standards); strings.TrimSpace(dir) != "" {
		_ = picker.LoadDirectory(dir)
	}
	picker.width = m.width
	picker.height = m.height
	m.standardsPicker = picker
	m.screen = screenStandards
}

func (m *Model) reloadActiveSession() error {
	session, err := m.repo.GetSession(context.Background(), m.activeSession.ID)
	if err != nil {
		return err
	}
	standards, err := m.repo.ListStandards(context.Background(), m.activeSession.ID)
	if err != nil {
		return err
	}
	messages, err := m.repo.ListMessages(context.Background(), m.activeSession.ID)
	if err != nil {
		return err
	}
	m.activeSession = session
	m.standards = standards
	m.messages = messages
	m.syncViewport()
	return nil
}

func (m *Model) syncViewport() {
	if m.width == 0 || m.height == 0 {
		return
	}
	m.viewport.SetContent(m.renderMessages())
	m.viewport.GotoBottom()
}

func (m *Model) resizeComponents() {
	if m.width == 0 || m.height == 0 {
		return
	}
	headerHeight := 4
	footerHeight := 5

	m.input.SetWidth(max(20, m.width-4))
	inputHeight := m.inputAreaHeight()
	m.viewport.Width = max(20, m.width-4)
	m.viewport.Height = max(8, m.height-headerHeight-inputHeight-footerHeight)
	m.syncViewport()
}

func (m *Model) inputAreaHeight() int {
	return m.input.Height() + 2
}

func (m *Model) syncInputHeight() {
	height := m.input.LineCount()
	if height < minInputHeight {
		height = minInputHeight
	}
	if height > maxInputHeight {
		height = maxInputHeight
	}
	if m.input.Height() != height {
		m.input.SetHeight(height)
	}
	if m.width > 0 && m.height > 0 {
		m.resizeComponents()
	}
}

func (m *Model) currentAgent() agent.Definition {
	return m.agents[m.selectedAgentIndex]
}

func (m *Model) moveSessionSelection(delta int) {
	if len(m.allSessions) == 0 {
		m.selectedSessionIdx = 0
		return
	}
	index := m.selectedSessionIdx + delta
	if index < 0 {
		index = 0
	}
	if index >= len(m.allSessions) {
		index = len(m.allSessions) - 1
	}
	m.selectedSessionIdx = index
}

func (m *Model) currentSelectedSession() (store.Session, bool) {
	if len(m.allSessions) == 0 {
		return store.Session{}, false
	}
	if m.selectedSessionIdx < 0 || m.selectedSessionIdx >= len(m.allSessions) {
		return store.Session{}, false
	}
	return m.allSessions[m.selectedSessionIdx], true
}

func (m *Model) clampSelection() {
	if m.selectedSessionIdx >= len(m.allSessions) {
		m.selectedSessionIdx = max(0, len(m.allSessions)-1)
	}
}

func (m *Model) renderMessages() string {
	var parts []string
	if strings.TrimSpace(m.activeSession.Summary) != "" {
		parts = append(parts, summaryBoxStyle.Width(max(20, m.viewport.Width-4)).Render(m.activeSession.Summary))
	}

	if len(m.messages) == 0 && m.pendingPrompt == "" {
		parts = append(parts, mutedStyle.Render("No messages yet. Send the first prompt."))
	}

	for _, message := range m.messages {
		parts = append(parts, renderChatBubble(message.Role, message.Content, false, m.viewport.Width))
	}

	if m.pendingPrompt != "" {
		parts = append(parts, renderChatBubble("user", m.pendingPrompt, false, m.viewport.Width))
		thinking := "Thinking..."
		if m.loading {
			thinking = "Thinking... " + m.spinner.View()
		}
		parts = append(parts, renderChatBubble("assistant", thinking, true, m.viewport.Width))
	}
	return strings.Join(parts, "\n")
}

func (m *Model) renderStandardsSummary() string {
	if len(m.standards) == 0 {
		return ""
	}

	lines := []string{"Selected Standards"}
	limit := len(m.standards)
	if limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		lines = append(lines, "• "+filepath.Base(m.standards[i].Path))
	}
	if len(m.standards) > limit {
		lines = append(lines, fmt.Sprintf("• +%d more", len(m.standards)-limit))
	}
	return standardsBoxStyle.Width(max(20, m.width-4)).Render(strings.Join(lines, "\n"))
}

func renderChatBubble(role, content string, thinking bool, width int) string {
	style := userBoxStyle
	body := content
	if role == "assistant" {
		style = assistantBoxStyle
		if thinking {
			style = style.Italic(true)
		} else {
			body = renderMarkdown(content, max(20, width-8))
		}
	}
	return style.Width(max(20, width-4)).Render(body)
}

func renderMarkdown(content string, width int) string {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(assistantMarkdownStyle()),
		glamour.WithWordWrap(width),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return content
	}

	rendered, err := renderer.Render(strings.TrimSpace(content))
	if err != nil {
		return content
	}
	return strings.TrimRight(rendered, "\n")
}

func assistantMarkdownStyle() ansi.StyleConfig {
	style := glamourstyles.DarkStyleConfig
	white := "255"
	style.Document.StylePrimitive.Color = &white
	return style
}

func (m *Model) homePanelHeight() int {
	// header (title + subtitle + blank) = 3 lines, footer ~= 3 lines, padding = 2
	available := m.height - 8
	if available < 10 {
		return 10
	}
	return available
}

func (m *Model) homeView() string {
	leftWidth := max(24, m.width/3)
	rightWidth := max(30, m.width-leftWidth-6)
	panelH := m.homePanelHeight()

	agentsPane := panelStyle.Width(leftWidth).Height(panelH).Render(m.renderAgentList())
	sessionsPane := panelStyle.Width(rightWidth).Height(panelH).Render(m.renderSessionList(panelH))
	row := lipgloss.JoinHorizontal(lipgloss.Top, agentsPane, sessionsPane)

	content := titleStyle.Render("Agent Switcher") + "\n" +
		mutedStyle.Render("Choose an agent for new sessions, or open any recent session.") + "\n\n" +
		row

	footer := m.renderFooter()
	return renderPageWithFooter(m.width, m.height, content, footer)
}

func (m *Model) chatView() string {
	agentName := describeAgent(m.activeSession.Agent)
	state := fmt.Sprintf("%s  Session %s", agentName, m.activeSession.ID)
	if m.loading || m.compacting {
		state += "  " + m.spinner.View()
	}

	meta := fmt.Sprintf(
		"prompts: %d  compacted through: %d  standards: %d",
		m.activeSession.UserPromptCount,
		m.activeSession.CompactedPromptCnt,
		len(m.standards),
	)

	content := titleStyle.Render(m.activeSession.Title) + "\n" +
		mutedStyle.Render(state) + "\n" +
		mutedStyle.Render(meta)

	if standards := m.renderStandardsSummary(); standards != "" {
		content += "\n" + standards
	}

	content += "\n\n" +
		m.viewport.View() + "\n" +
		m.input.View()

	return renderPageWithFooter(m.width, m.height, content, m.renderFooter())
}

func (m *Model) agentPickerView() string {
	var lines []string
	lines = append(lines, titleStyle.Render("Switch Agent"))
	lines = append(lines, mutedStyle.Render("Select the engine for this session. All history is preserved."))
	lines = append(lines, "")

	for i, def := range m.agents {
		current := ""
		if def.Kind == m.activeSession.Agent {
			current = "  [current]"
		}
		line := fmt.Sprintf("%s  %-8s  %s%s", shortcut(i), def.Label, def.Description, current)
		if i == m.agentPickerIdx {
			line = selectedStyle.Render(line)
		} else if def.Kind == m.activeSession.Agent {
			line = focusedStyle.Render(line)
		}
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	footer := renderManualFooter(m.width, m.status, m.errText, []footerItem{
		{Key: "↑/↓", Label: "move"},
		{Key: "Enter", Label: "confirm"},
		{Key: "Esc", Label: "cancel"},
	})
	return renderPageWithFooter(m.width, m.height, content, footer)
}

func (m *Model) renderAgentList() string {
	var lines []string
	for i, def := range m.agents {
		line := fmt.Sprintf("%s  %s", shortcut(i), def.Label)
		if i == m.selectedAgentIndex {
			if m.homeFocus == focusAgents {
				line = selectedStyle.Render(line)
			} else {
				line = focusedStyle.Render(line)
			}
		}
		lines = append(lines, line)
		lines = append(lines, mutedStyle.Render(def.Description))
		if i < len(m.agents)-1 {
			lines = append(lines, "")
		}
	}
	return "Agents\n\n" + strings.Join(lines, "\n")
}

func (m *Model) renderSessionList(panelH int) string {
	if len(m.allSessions) == 0 {
		return "Recent sessions\n\n" + mutedStyle.Render("No saved sessions yet.")
	}

	// Each session takes 3 lines (text + date + blank), last one takes 2.
	// Available lines inside panel = panelH - 2 (header "Recent sessions\n\n").
	linesPerItem := 3
	availableLines := panelH - 2
	visibleCount := max(1, availableLines/linesPerItem)

	// Scroll window: keep selected item visible.
	start := 0
	if m.selectedSessionIdx >= visibleCount {
		start = m.selectedSessionIdx - visibleCount + 1
	}
	end := start + visibleCount
	if end > len(m.allSessions) {
		end = len(m.allSessions)
		start = max(0, end-visibleCount)
	}

	var lines []string

	if start > 0 {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("  ↑ %d more", start)))
	}

	for i := start; i < end; i++ {
		session := m.allSessions[i]
		agentTag := fmt.Sprintf("[%s]", string(session.Agent))
		plain := fmt.Sprintf("%s  %s  %s", session.ID[:8], session.Title, agentTag)
		var line string
		if i == m.selectedSessionIdx {
			if m.homeFocus == focusSessions {
				line = selectedStyle.Render(plain)
			} else {
				line = focusedStyle.Render(plain)
			}
		} else {
			line = fmt.Sprintf("%s  %s  %s", session.ID[:8], session.Title, mutedStyle.Render(agentTag))
		}
		lines = append(lines, line)
		lines = append(lines, mutedStyle.Render(session.UpdatedAt.Local().Format("2006-01-02 15:04")))
		if i < end-1 {
			lines = append(lines, "")
		}
	}

	if end < len(m.allSessions) {
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.allSessions)-end)))
	}

	return "Recent sessions\n\n" + strings.Join(lines, "\n")
}

func (m *Model) renderFooter() string {
	var items []footerItem
	if m.screen == screenHome {
		items = []footerItem{
			{Key: "Enter", Label: "new/open"},
			{Key: "Tab", Label: "switch focus"},
			{Key: "↑/↓", Label: "move"},
			{Key: "R", Label: "refresh"},
			{Key: "Q", Label: "quit"},
		}
	} else {
		items = []footerItem{
			{Key: "Enter", Label: "send"},
			{Key: "↑/↓", Label: "history"},
			{Key: "PgUp/PgDn", Label: "scroll"},
			{Key: "Ctrl+T", Label: "standards"},
			{Key: "Ctrl+G", Label: "switch agent"},
			{Key: "Esc", Label: "home"},
			{Key: "Ctrl+C", Label: "quit"},
		}
	}

	status := m.status
	if m.loading || m.compacting {
		status = "Waiting for agent response..."
	}

	return renderManualFooter(m.width, status, m.errText, items)
}

func shortcut(index int) string {
	return string(rune('A' + index))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	pageStyle = lipgloss.NewStyle().Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230"))

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("230")).
			Background(lipgloss.Color("33"))

	focusedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("153"))

	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("204"))

	userBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Padding(0, 1)

	assistantBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("106")).
				Foreground(lipgloss.Color("255")).
				Padding(0, 1)

	standardsBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("81")).
				Padding(0, 1)

	summaryBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("215")).
			Padding(0, 1).
			Italic(true)
)
