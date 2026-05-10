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
	titleStyle       = tuistyle.SectionStyle
	bodyStyle        = tuistyle.NormalStyle
	inputPromptStyle = tuistyle.LabelStyle
)

func NewHandler(suspend, resume func()) func(*model.UIStepRequest) (model.UIStepResult, error) {
	return func(req *model.UIStepRequest) (model.UIStepResult, error) {
		if req == nil {
			return model.UIStepResult{}, fmt.Errorf("ui step request is nil")
		}
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
		m := newModel(req)
		p := tea.NewProgram(m)
		final, err := p.Run()
		if err != nil {
			return model.UIStepResult{}, err
		}
		fm := final.(*Model)
		return fm.Result(), nil
	}
}

type uiModel = Model

type Model struct {
	req        *model.UIStepRequest
	focus      int
	selections []int
	width      int
	result     model.UIStepResult
	done       bool
}

func NewModel(req *model.UIStepRequest) *Model {
	return newModel(req)
}

func newModel(req *model.UIStepRequest) *Model {
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
	return &Model{
		req:        req,
		selections: selections,
		width:      80,
	}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.result = model.UIStepResult{Canceled: true}
			m.done = true
			return m, tea.Quit
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		case "left", "h":
			m.moveActionFocus(-1)
		case "right", "l":
			m.moveActionFocus(1)
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

func (m *Model) SetWidth(width int) {
	m.width = width
}

func (m *Model) Done() bool {
	return m.done
}

func (m *Model) Result() model.UIStepResult {
	return m.result
}

func (m *Model) HelpParts() []string {
	var parts []string
	if _, ok := m.focusedInputIndex(); ok {
		parts = append(parts, "↑↓ option")
		if m.elementCount() > 1 {
			parts = append(parts, "tab focus")
		}
		if m.isSimplePicker() {
			parts = append(parts, "enter select")
		} else {
			parts = append(parts, "enter next")
		}
	} else if _, ok := m.focusedActionIndex(); ok {
		if len(m.req.Actions) > 1 {
			parts = append(parts, "←→ action")
		}
		if m.elementCount() > 1 {
			parts = append(parts, "tab focus")
		}
		parts = append(parts, "enter select")
	}
	parts = append(parts, "esc cancel")
	return parts
}

func (m *Model) elementCount() int {
	if m.isSimplePicker() {
		return len(m.req.Inputs)
	}
	return len(m.req.Inputs) + len(m.req.Actions)
}

func (m *Model) isSimplePicker() bool {
	return len(m.req.Inputs) == 1 && len(m.req.Actions) == 1 && m.req.Actions[0].Outcome == "continue"
}

func (m *Model) focusedInputIndex() (int, bool) {
	if m.focus >= 0 && m.focus < len(m.req.Inputs) {
		return m.focus, true
	}
	return 0, false
}

func (m *Model) focusedActionIndex() (int, bool) {
	idx := m.focus - len(m.req.Inputs)
	if idx >= 0 && idx < len(m.req.Actions) {
		return idx, true
	}
	return 0, false
}

func (m *Model) moveSelection(delta int) {
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

func (m *Model) moveFocus(delta int) {
	count := m.elementCount()
	if count == 0 {
		return
	}
	m.focus = (m.focus + delta + count) % count
}

func (m *Model) moveActionFocus(delta int) {
	idx, ok := m.focusedActionIndex()
	if !ok || len(m.req.Actions) == 0 {
		return
	}
	next := (idx + delta + len(m.req.Actions)) % len(m.req.Actions)
	m.focus = len(m.req.Inputs) + next
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if _, ok := m.focusedInputIndex(); ok {
		if m.isSimplePicker() {
			m.result = model.UIStepResult{
				Outcome: m.req.Actions[0].Outcome,
				Inputs:  m.selectedInputs(),
			}
			m.done = true
			return m, tea.Quit
		}
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
	m.done = true
	return m, tea.Quit
}

func (m *Model) selectedInputs() map[string]string {
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

func (m *Model) View() string {
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
				fmt.Fprintf(&b, "  %s %s\n", tuistyle.FocusedSelectorPrefix, opt)
			case i == m.selections[idx]:
				fmt.Fprintf(&b, "  %s %s\n", tuistyle.SelectedSelectorPrefix, opt)
			default:
				fmt.Fprintf(&b, "    %s\n", opt)
			}
		}
		b.WriteString("\n")
	}

	if !m.isSimplePicker() {
		labels := make([]string, len(m.req.Actions))
		for i, action := range m.req.Actions {
			labels[i] = action.Label
		}
		b.WriteString(tuistyle.RenderButtonRow(labels, m.focus-len(m.req.Inputs), 0))

		if len(m.req.Actions) > 0 {
			b.WriteString("\n")
		}
	}
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
