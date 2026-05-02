package uistep

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codagent/agent-runner/internal/model"
)

var titleStyle = lipgloss.NewStyle().Bold(true)

func NewHandler(suspend, resume func()) func(model.UIStepRequest) (model.UIStepResult, error) {
	return func(req model.UIStepRequest) (model.UIStepResult, error) {
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
		fm := final.(uiModel)
		return fm.result, nil
	}
}

type uiModel struct {
	req    model.UIStepRequest
	cursor int
	phase  phase
	inputs []int
	result model.UIStepResult
}

type phase int

const (
	phaseInputs phase = iota
	phaseActions
)

func newModel(req model.UIStepRequest) uiModel {
	m := uiModel{
		req:    req,
		inputs: make([]int, len(req.Inputs)),
	}
	if len(req.Inputs) == 0 {
		m.phase = phaseActions
	}
	return m
}

func (m uiModel) Init() tea.Cmd { return nil }

func (m uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.result = model.UIStepResult{Canceled: true}
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			m.cursor = min(m.cursor+1, m.maxCursor())
		case "enter":
			return m.handleSelect()
		}
	}
	return m, nil
}

func (m uiModel) maxCursor() int {
	switch m.phase {
	case phaseInputs:
		return len(m.req.Inputs[m.currentInput()].Options) - 1
	case phaseActions:
		return len(m.req.Actions) - 1
	}
	return 0
}

func (m uiModel) currentInput() int {
	for i := range m.inputs {
		if m.inputs[i] == 0 {
			return i
		}
	}
	return len(m.inputs) - 1
}

func (m uiModel) handleSelect() (tea.Model, tea.Cmd) {
	switch m.phase {
	case phaseInputs:
		idx := m.currentInput()
		m.inputs[idx] = m.cursor + 1
		if !slices.Contains(m.inputs, 0) {
			m.phase = phaseActions
		}
		m.cursor = 0
		return m, nil
	case phaseActions:
		action := m.req.Actions[m.cursor]
		inputMap := make(map[string]string)
		for i, input := range m.req.Inputs {
			if m.inputs[i] > 0 {
				inputMap[input.ID] = input.Options[m.inputs[i]-1]
			}
		}
		m.result = model.UIStepResult{
			Outcome: action.Outcome,
			Inputs:  inputMap,
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m uiModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render(m.req.Title))
	b.WriteString("\n\n")

	if m.req.Body != "" {
		b.WriteString(m.req.Body)
		b.WriteString("\n\n")
	}

	switch m.phase {
	case phaseInputs:
		idx := m.currentInput()
		input := m.req.Inputs[idx]
		fmt.Fprintf(&b, "%s:\n\n", input.Prompt)
		for i, opt := range input.Options {
			if i == m.cursor {
				fmt.Fprintf(&b, "  ▶ %s\n", opt)
			} else {
				fmt.Fprintf(&b, "    %s\n", opt)
			}
		}
	case phaseActions:
		for i, action := range m.req.Actions {
			if i == m.cursor {
				fmt.Fprintf(&b, "  ▶ %s\n", action.Label)
			} else {
				fmt.Fprintf(&b, "    %s\n", action.Label)
			}
		}
	}

	b.WriteString("\n↑↓ Navigate  Enter Select")
	return b.String()
}
