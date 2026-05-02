package uistep

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

var (
	titleStyle         = tuistyle.SectionStyle
	bodyStyle          = tuistyle.NormalStyle
	inputPromptStyle   = tuistyle.LabelStyle
	focusedButtonStyle = lipgloss.NewStyle().Foreground(tuistyle.SelectedText).Background(tuistyle.AccentCyan).Padding(0, 1)
	buttonStyle        = lipgloss.NewStyle().Foreground(tuistyle.BodyText).Padding(0, 1)
)

func NewHandler(suspend, resume func()) func(model.UIStepRequest) (model.UIStepResult, error) {
	return func(req model.UIStepRequest) (model.UIStepResult, error) {
		if len(req.Actions) == 0 {
			return model.UIStepResult{}, fmt.Errorf("ui step %s has no actions", req.StepID)
		}
		if suspend != nil {
			suspend()
		}
		defer func() {
			if resume != nil {
				resume()
			}
		}()
		m := newModel(&req)
		p := tea.NewProgram(m)
		final, err := p.Run()
		if err != nil {
			return model.UIStepResult{}, err
		}
		fm := final.(*uiModel)
		return fm.result, nil
	}
}

type uiModel struct {
	req        *model.UIStepRequest
	focus      int
	selections []int
	width      int
	result     model.UIStepResult
}

func newModel(req *model.UIStepRequest) *uiModel {
	selections := make([]int, len(req.Inputs))
	for i, input := range req.Inputs {
		if input.Default == "" {
			continue
		}
		for j, option := range input.Options {
			if option == input.Default {
				selections[i] = j
				break
			}
		}
	}
	return &uiModel{
		req:        req,
		selections: selections,
		width:      80,
	}
}

func (m *uiModel) Init() tea.Cmd { return nil }

func (m *uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.result = model.UIStepResult{Canceled: true}
			return m, tea.Quit
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		case "tab":
			m.moveFocus(1)
		case "shift+tab":
			m.moveFocus(-1)
		case "enter":
			return m.handleEnter()
		}
	}
	return m, nil
}

func (m *uiModel) elementCount() int {
	return len(m.req.Inputs) + len(m.req.Actions)
}

func (m *uiModel) focusedInputIndex() (int, bool) {
	if m.focus >= 0 && m.focus < len(m.req.Inputs) {
		return m.focus, true
	}
	return 0, false
}

func (m *uiModel) focusedActionIndex() (int, bool) {
	idx := m.focus - len(m.req.Inputs)
	if idx >= 0 && idx < len(m.req.Actions) {
		return idx, true
	}
	return 0, false
}

func (m *uiModel) moveSelection(delta int) {
	idx, ok := m.focusedInputIndex()
	if !ok {
		return
	}
	options := len(m.req.Inputs[idx].Options)
	if options == 0 {
		return
	}
	m.selections[idx] = clamp(m.selections[idx]+delta, 0, options-1)
}

func (m *uiModel) moveFocus(delta int) {
	count := m.elementCount()
	if count == 0 {
		return
	}
	m.focus = (m.focus + delta + count) % count
}

func (m *uiModel) handleEnter() (tea.Model, tea.Cmd) {
	if _, ok := m.focusedInputIndex(); ok {
		m.moveFocus(1)
		return m, nil
	}
	idx, ok := m.focusedActionIndex()
	if !ok {
		return m, nil
	}
	action := m.req.Actions[idx]
	m.result = model.UIStepResult{
		Outcome: action.Outcome,
		Inputs:  m.selectedInputs(),
	}
	return m, tea.Quit
}

func (m *uiModel) selectedInputs() map[string]string {
	inputMap := make(map[string]string, len(m.req.Inputs))
	for i, input := range m.req.Inputs {
		if len(input.Options) == 0 {
			continue
		}
		selection := clamp(m.selections[i], 0, len(input.Options)-1)
		inputMap[input.ID] = input.Options[selection]
	}
	return inputMap
}

func (m *uiModel) View() string {
	var b strings.Builder
	contentWidth := m.width - 4
	if contentWidth <= 0 {
		contentWidth = 76
	}

	b.WriteString(titleStyle.Render(m.req.Title))
	b.WriteString("\n\n")

	if m.req.Body != "" {
		b.WriteString(lipgloss.NewStyle().Width(contentWidth).Render(bodyStyle.Render(m.req.Body)))
		b.WriteString("\n\n")
	}

	for idx, input := range m.req.Inputs {
		fmt.Fprintf(&b, "%s:\n", inputPromptStyle.Render(input.Prompt))
		for i, opt := range input.Options {
			switch {
			case idx == m.focus && i == m.selections[idx]:
				fmt.Fprintf(&b, "  ▶ %s\n", opt)
			case i == m.selections[idx]:
				fmt.Fprintf(&b, "  • %s\n", opt)
			default:
				fmt.Fprintf(&b, "    %s\n", opt)
			}
		}
		b.WriteString("\n")
	}

	for i, action := range m.req.Actions {
		label := "[ " + action.Label + " ]"
		if len(m.req.Inputs)+i == m.focus {
			fmt.Fprintf(&b, "  %s\n", focusedButtonStyle.Render(label))
		} else {
			fmt.Fprintf(&b, "  %s\n", buttonStyle.Render(label))
		}
	}

	b.WriteString("\n↑↓ Navigate  Tab Focus  Enter Select")
	return b.String()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
