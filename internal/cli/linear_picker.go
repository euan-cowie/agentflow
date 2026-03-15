package cli

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/euan-cowie/agentflow/internal/agentflow"
)

var errLinearIssueSelectionCancelled = errors.New("linear issue selection cancelled")

type linearIssuePickerModel struct {
	issues    []agentflow.LinearIssue
	filtered  []int
	cursor    int
	query     string
	selected  string
	width     int
	height    int
	cancelled bool
}

func newLinearIssuePickerModel(issues []agentflow.LinearIssue) linearIssuePickerModel {
	model := linearIssuePickerModel{
		issues: issues,
	}
	model.applyFilter()
	return model
}

func (m linearIssuePickerModel) Init() tea.Cmd {
	return nil
}

func (m linearIssuePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEnter:
			if len(m.filtered) == 0 {
				return m, nil
			}
			m.selected = m.issues[m.filtered[m.cursor]].Identifier
			return m, tea.Quit
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case tea.KeyDown:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
				m.applyFilter()
			}
			return m, nil
		default:
			switch msg.String() {
			case "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "j":
				if m.cursor < len(m.filtered)-1 {
					m.cursor++
				}
			case "ctrl+u":
				m.query = ""
				m.applyFilter()
			default:
				if text := msg.String(); len(text) == 1 && text[0] >= 32 && text[0] != 127 {
					m.query += text
					m.applyFilter()
				}
			}
			return m, nil
		}
	}
	return m, nil
}

func (m linearIssuePickerModel) View() string {
	lines := []string{
		"Select a Linear issue",
		"",
		"Search: " + m.query,
		"",
		"type to filter, enter to select, esc to cancel",
		"",
	}
	if len(m.filtered) == 0 {
		lines = append(lines, "No issues match the current filter.")
		return strings.Join(lines, "\n")
	}

	available := m.height - len(lines) - 1
	if available < 5 {
		available = 5
	}
	start := 0
	if m.cursor >= available {
		start = m.cursor - available + 1
	}
	end := start + available
	if end > len(m.filtered) {
		end = len(m.filtered)
		start = maxInt(0, end-available)
	}

	for idx := start; idx < end; idx++ {
		issue := m.issues[m.filtered[idx]]
		cursor := "  "
		if idx == m.cursor {
			cursor = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s  %s  [%s]  %s", cursor, issue.Identifier, issue.Title, issue.Team.Key, issue.State.Name))
	}
	return strings.Join(lines, "\n")
}

func pickLinearIssue(issues []agentflow.LinearIssue) (string, error) {
	if len(issues) == 0 {
		return "", errors.New("no Linear issues available for selection")
	}
	model := newLinearIssuePickerModel(issues)
	result, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return "", err
	}
	finalModel := result.(linearIssuePickerModel)
	if finalModel.cancelled {
		return "", errLinearIssueSelectionCancelled
	}
	if strings.TrimSpace(finalModel.selected) == "" {
		return "", errors.New("no Linear issue selected")
	}
	return finalModel.selected, nil
}

func (m *linearIssuePickerModel) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.query))
	m.filtered = m.filtered[:0]
	for idx, issue := range m.issues {
		haystack := strings.ToLower(strings.Join([]string{
			issue.Identifier,
			issue.Title,
			issue.Team.Key,
			issue.State.Name,
		}, " "))
		if query == "" || strings.Contains(haystack, query) {
			m.filtered = append(m.filtered, idx)
		}
	}
	if len(m.filtered) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
