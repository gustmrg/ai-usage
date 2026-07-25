package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/gustmrg/ai-usage/internal/app"
	"github.com/gustmrg/ai-usage/internal/model"
	"github.com/gustmrg/ai-usage/internal/render"
)

type providerState struct {
	id          string
	name        string
	available   bool
	setup       string
	snapshot    model.Snapshot
	err         error
	loading     bool
	requestID   int
	lastRequest time.Time
}

type fetchMsg struct {
	id        string
	requestID int
	result    app.Result
}

type tickMsg time.Time

type Model struct {
	service     *app.Service
	ctx         context.Context
	cancel      context.CancelFunc
	providers   []providerState
	selected    int
	width       int
	height      int
	helpVisible bool
	now         time.Time
}

const manualRefreshCooldown = 10 * time.Second

func New(service *app.Service) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	states := make([]providerState, 0, len(service.Providers()))
	for _, p := range service.Providers() {
		detection := p.Detect()
		states = append(states, providerState{id: p.ID(), name: p.DisplayName(), available: detection.Available, setup: detection.Detail})
	}
	return &Model{service: service, ctx: ctx, cancel: cancel, providers: states, now: time.Now()}
}

func (m *Model) Init() tea.Cmd {
	commands := []tea.Cmd{tick()}
	for i := range m.providers {
		if m.providers[i].available {
			commands = append(commands, m.fetch(i, false))
		}
	}
	return tea.Batch(commands...)
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
	case tickMsg:
		m.now = time.Time(message)
		commands := []tea.Cmd{tick()}
		for i := range m.providers {
			if m.providers[i].available && !m.providers[i].loading && m.now.Sub(m.providers[i].lastRequest) >= time.Minute {
				commands = append(commands, m.fetch(i, false))
			}
		}
		return m, tea.Batch(commands...)
	case fetchMsg:
		for i := range m.providers {
			state := &m.providers[i]
			if state.id != message.id || state.requestID != message.requestID {
				continue
			}
			state.loading = false
			state.err = message.result.Err
			if !message.result.Snapshot.CollectedAt.IsZero() {
				state.snapshot = message.result.Snapshot
			}
			break
		}
	case tea.KeyPressMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m *Model) handleKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(message, key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"))):
		if m.helpVisible {
			m.helpVisible = false
			return m, nil
		}
		m.cancel()
		return m, tea.Quit
	case key.Matches(message, key.NewBinding(key.WithKeys("?"))):
		m.helpVisible = !m.helpVisible
	case key.Matches(message, key.NewBinding(key.WithKeys("tab", "l", "right"))):
		if len(m.providers) > 0 {
			m.selected = (m.selected + 1) % len(m.providers)
		}
	case key.Matches(message, key.NewBinding(key.WithKeys("shift+tab", "h", "left"))):
		if len(m.providers) > 0 {
			m.selected = (m.selected - 1 + len(m.providers)) % len(m.providers)
		}
	case key.Matches(message, key.NewBinding(key.WithKeys("1", "2"))):
		index := int(message.Text[0] - '1')
		if index >= 0 && index < len(m.providers) {
			m.selected = index
		}
	case key.Matches(message, key.NewBinding(key.WithKeys("r"))):
		if len(m.providers) > 0 {
			return m, m.fetch(m.selected, true)
		}
	case key.Matches(message, key.NewBinding(key.WithKeys("R"))):
		commands := make([]tea.Cmd, 0, len(m.providers))
		for i := range m.providers {
			commands = append(commands, m.fetch(i, true))
		}
		return m, tea.Batch(commands...)
	}
	return m, nil
}

func (m *Model) fetch(index int, force bool) tea.Cmd {
	state := &m.providers[index]
	p := m.service.Provider(state.id)
	detection := p.Detect()
	state.available = detection.Available
	state.setup = detection.Detail
	if !state.available {
		state.loading = false
		return nil
	}
	if state.loading || (force && time.Since(state.lastRequest) < manualRefreshCooldown) {
		return nil
	}
	state.loading = true
	state.requestID++
	state.lastRequest = time.Now()
	id, requestID := state.id, state.requestID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		return fetchMsg{id: id, requestID: requestID, result: m.service.Fetch(ctx, id, force)}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return tickMsg(now) })
}

func (m *Model) View() tea.View {
	var view tea.View
	view.AltScreen = true
	view.SetContent(m.render())
	return view
}

func (m *Model) render() string {
	width := m.width
	if width < 52 {
		width = 52
	}
	contentWidth := width - 4
	var body string
	if len(m.providers) == 0 {
		body = errorStyle.Render("No providers configured") + "\n\n" + mutedStyle.Render("Run `codex login`, set KIMI_API_KEY, or set OPENCODE_AUTH_COOKIE, then restart ai-usage.")
	} else {
		state := m.providers[m.selected]
		body = m.renderProvider(state, contentWidth)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		m.renderTabs(contentWidth),
		panelStyle.Width(contentWidth-2).Render(body),
		m.renderFooter(contentWidth),
	)
	if m.helpVisible {
		content = lipgloss.JoinVertical(lipgloss.Left, content, helpStyle.Width(contentWidth-2).Render("Tab / h / l  switch provider\n1 / 2          select provider\nr              refresh selected\nR              refresh all\n?              close help\nq / Esc        quit"))
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(content)
}

func (m *Model) renderTabs(width int) string {
	if len(m.providers) == 0 {
		return logoStyle.Render("ai-usage")
	}
	tabs := make([]string, 0, len(m.providers))
	for i, state := range m.providers {
		style := inactiveTabStyle
		if i == m.selected {
			style = activeTabStyle
		}
		tabs = append(tabs, style.Render(state.name))
	}
	return lipgloss.NewStyle().Width(width).Render(lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...))
}

func (m *Model) renderProvider(state providerState, width int) string {
	if !state.available {
		return titleStyle.Render(state.name) + "\n\n" + warningStyle.Render("Not configured") + "\n\n" + mutedStyle.Render(state.setup) + "\n\n" + mutedStyle.Render("Configure the provider, then restart ai-usage.")
	}
	if state.snapshot.CollectedAt.IsZero() {
		if state.loading {
			return titleStyle.Render(state.name) + "\n\n" + mutedStyle.Render("Loading usage...")
		}
		if state.err != nil {
			return titleStyle.Render(state.name) + "\n\n" + errorStyle.Render(state.err.Error())
		}
	}
	text := render.Snapshot(state.snapshot, width-4, m.now)
	lines := strings.Split(text, "\n")
	if len(lines) > 0 {
		lines[0] = titleStyle.Render(lines[0])
	}
	if state.loading {
		lines = append(lines, "", mutedStyle.Render("Refreshing..."))
	}
	if state.err != nil {
		prefix := "Last refresh failed: "
		if state.snapshot.Stale {
			prefix = "Showing cached data: "
		}
		lines = append(lines, "", warningStyle.Render(prefix+state.err.Error()))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderFooter(width int) string {
	status := "tab/h/l switch   r refresh   ? help   q quit"
	if len(m.providers) > 0 && m.providers[m.selected].loading {
		status = "refreshing " + strings.ToLower(m.providers[m.selected].name) + "..."
	}
	return footerStyle.Width(width).Render(status)
}

var (
	accent           = lipgloss.Color("#61AFEF")
	textColor        = lipgloss.Color("#ABB2BF")
	mutedColor       = lipgloss.Color("#5C6370")
	logoStyle        = lipgloss.NewStyle().Bold(true).Foreground(accent)
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E5C07B"))
	activeTabStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1E222A")).Background(accent).Padding(0, 2)
	inactiveTabStyle = lipgloss.NewStyle().Foreground(textColor).Background(lipgloss.Color("#2C313A")).Padding(0, 2)
	panelStyle       = lipgloss.NewStyle().Foreground(textColor).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#3E4451")).Padding(1, 2)
	footerStyle      = lipgloss.NewStyle().Foreground(mutedColor).Padding(0, 1)
	mutedStyle       = lipgloss.NewStyle().Foreground(mutedColor)
	errorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#E06C75"))
	warningStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#D19A66"))
	helpStyle        = lipgloss.NewStyle().Foreground(textColor).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2)
)
