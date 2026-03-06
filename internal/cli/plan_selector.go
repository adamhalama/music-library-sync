package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/jaa/update-downloads/internal/engine"
)

type planSelectorModel struct {
	sourceID string
	rows     []engine.PlanRow
	cursor   int
	selected map[int]bool
	width    int
	height   int
	canceled bool
	keys     planSelectorKeys
}

type planSelectorKeys struct {
	up        key.Binding
	down      key.Binding
	toggle    key.Binding
	selectAll key.Binding
	clearAll  key.Binding
	confirm   key.Binding
	cancel    key.Binding
}

func runPlanSelector(app *AppContext, sourceID string, rows []engine.PlanRow) ([]int, bool, error) {
	model := newPlanSelectorModel(sourceID, rows)
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithInput(app.IO.In),
		tea.WithOutput(app.IO.Out),
	)
	finalModel, err := program.Run()
	if err != nil {
		return nil, false, err
	}
	resolved, ok := finalModel.(planSelectorModel)
	if !ok {
		return nil, false, fmt.Errorf("unexpected selector model type %T", finalModel)
	}
	if resolved.canceled {
		return nil, true, nil
	}
	return resolved.selectedIndices(), false, nil
}

func newPlanSelectorModel(sourceID string, rows []engine.PlanRow) planSelectorModel {
	selected := map[int]bool{}
	for _, row := range rows {
		if row.Toggleable && row.SelectedByDefault {
			selected[row.Index] = true
		}
	}
	return planSelectorModel{
		sourceID: sourceID,
		rows:     append([]engine.PlanRow{}, rows...),
		selected: selected,
		keys:     defaultPlanSelectorKeys(),
	}
}

func (m planSelectorModel) Init() tea.Cmd {
	return nil
}

func (m planSelectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = typed.Width
		m.height = typed.Height
		return m, nil
	case tea.KeyMsg:
		switch {
		case key.Matches(typed, m.keys.cancel):
			m.canceled = true
			return m, tea.Quit
		case key.Matches(typed, m.keys.confirm):
			return m, tea.Quit
		case key.Matches(typed, m.keys.up):
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case key.Matches(typed, m.keys.down):
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
			return m, nil
		case key.Matches(typed, m.keys.toggle):
			if len(m.rows) == 0 {
				return m, nil
			}
			row := m.rows[m.cursor]
			if !row.Toggleable {
				return m, nil
			}
			m.selected[row.Index] = !m.selected[row.Index]
			return m, nil
		case key.Matches(typed, m.keys.selectAll):
			for _, row := range m.rows {
				if row.Toggleable {
					m.selected[row.Index] = true
				}
			}
			return m, nil
		case key.Matches(typed, m.keys.clearAll):
			for _, row := range m.rows {
				if row.Toggleable {
					m.selected[row.Index] = false
				}
			}
			return m, nil
		default:
			return m, nil
		}
	}
	return m, nil
}

func (m planSelectorModel) View() string {
	lines := []string{
		fmt.Sprintf("udl sync --plan  source=%s", m.sourceID),
		"up/down or j/k: move   space: toggle   a: select all   n: clear all   enter: confirm   q/esc: cancel",
		"",
	}

	if len(m.rows) == 0 {
		lines = append(lines, "No tracks found in selected preflight window.")
		return strings.Join(lines, "\n")
	}

	start, end := planSelectorWindow(len(m.rows), m.cursor, m.height)
	for i := start; i < end; i++ {
		row := m.rows[i]
		cursor := " "
		if i == m.cursor {
			cursor = ">"
		}

		marker := "[-]"
		if row.Toggleable {
			if m.selected[row.Index] {
				marker = "[x]"
			} else {
				marker = "[ ]"
			}
		}

		title := strings.TrimSpace(row.Title)
		if title == "" {
			title = "(untitled)"
		}
		lines = append(lines, fmt.Sprintf(
			"%s %s %3d  %-16s  %s (%s)",
			cursor,
			marker,
			row.Index,
			string(row.Status),
			title,
			row.RemoteID,
		))
	}

	selectedCount := 0
	totalToggleable := 0
	for _, row := range m.rows {
		if row.Toggleable {
			totalToggleable++
			if m.selected[row.Index] {
				selectedCount++
			}
		}
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Selected: %d/%d toggleable tracks", selectedCount, totalToggleable))
	return strings.Join(lines, "\n")
}

func (m planSelectorModel) selectedIndices() []int {
	out := make([]int, 0, len(m.selected))
	for _, row := range m.rows {
		if !row.Toggleable {
			continue
		}
		if m.selected[row.Index] {
			out = append(out, row.Index)
		}
	}
	sort.Ints(out)
	return out
}

func planSelectorWindow(total, cursor, height int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	// Keep room for header/footer text.
	maxRows := 12
	if height > 0 {
		usable := height - 6
		if usable > 3 {
			maxRows = usable
		}
	}
	if total <= maxRows {
		return 0, total
	}

	start := cursor - (maxRows / 2)
	if start < 0 {
		start = 0
	}
	end := start + maxRows
	if end > total {
		end = total
		start = end - maxRows
	}
	return start, end
}

func defaultPlanSelectorKeys() planSelectorKeys {
	return planSelectorKeys{
		up: key.NewBinding(
			key.WithKeys("up", "k"),
		),
		down: key.NewBinding(
			key.WithKeys("down", "j"),
		),
		toggle: key.NewBinding(
			key.WithKeys(" "),
		),
		selectAll: key.NewBinding(
			key.WithKeys("a"),
		),
		clearAll: key.NewBinding(
			key.WithKeys("n"),
		),
		confirm: key.NewBinding(
			key.WithKeys("enter"),
		),
		cancel: key.NewBinding(
			key.WithKeys("ctrl+c", "q", "esc"),
		),
	}
}
