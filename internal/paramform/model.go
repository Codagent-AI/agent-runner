package paramform

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

// SubmittedMsg carries validated parameter values back to the caller.
type SubmittedMsg map[string]string

// CancelledMsg signals that the param form was cancelled.
type CancelledMsg struct{}

// Model is the bubbletea model for the workflow parameter form.
type Model struct {
	entry   discovery.WorkflowEntry
	inputs  []textinput.Model
	focused int
	errors  []string
	width   int
}

// New constructs a param form for the workflow entry.
func New(entry *discovery.WorkflowEntry) *Model {
	inputs := make([]textinput.Model, 0, len(entry.Params))
	errors := make([]string, len(entry.Params))
	for i, param := range entry.Params {
		input := textinput.New()
		input.Prompt = ""
		input.SetValue(param.Default)
		input.Cursor.Style = tuistyle.CursorStyle
		input.TextStyle = tuistyle.NormalStyle
		input.PlaceholderStyle = tuistyle.DimStyle
		if i == 0 {
			input.Focus()
		}
		inputs = append(inputs, input)
	}

	focused := -1
	if len(inputs) > 0 {
		focused = 0
	}

	return &Model{
		entry:   *entry,
		inputs:  inputs,
		focused: focused,
		errors:  errors,
		width:   80,
	}
}

// WithWidth returns a copy of the model sized for the given terminal width.
func (m *Model) WithWidth(width int) *Model {
	m.width = width
	return m
}

// Update handles keyboard navigation, validation, and submission.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			return m, func() tea.Msg { return CancelledMsg{} }
		case tea.KeyTab:
			m.focusNext()
			return m, nil
		case tea.KeyShiftTab:
			m.focusPrev()
			return m, nil
		case tea.KeyEnter:
			if m.focused == -1 || m.focused == len(m.inputs)-1 {
				return m.submit()
			}
			return m, nil
		}
	}

	if m.focused >= 0 && m.focused < len(m.inputs) {
		var cmd tea.Cmd
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
		m.errors[m.focused] = ""
		return m, cmd
	}

	return m, nil
}

// View renders the form.
func (m *Model) View() string {
	var b strings.Builder

	b.WriteString(tuistyle.ScreenMargin + tuistyle.HeaderStyle.Render(m.entry.CanonicalName) + "\n")
	if m.entry.Description != "" {
		b.WriteString(tuistyle.ScreenMargin + tuistyle.DimStyle.Render(m.entry.Description) + "\n")
	}
	b.WriteString("\n")

	labelWidth := 0
	for _, param := range m.entry.Params {
		if w := lipgloss.Width(param.Name); w > labelWidth {
			labelWidth = w
		}
	}
	inputWidth := m.inputWidth(labelWidth)

	for i, param := range m.entry.Params {
		label := lipgloss.NewStyle().Width(labelWidth).Align(lipgloss.Right).Render(param.Name)
		marker := " "
		if param.IsRequired() {
			marker = tuistyle.StatusFailed.Render("*")
		}
		b.WriteString(tuistyle.ScreenMargin)
		b.WriteString(label)
		b.WriteString(" ")
		b.WriteString(marker)
		b.WriteString(" ")
		b.WriteString(m.renderInput(i, inputWidth))
		b.WriteString("\n")
		if m.errors[i] != "" {
			b.WriteString(tuistyle.ScreenMargin)
			b.WriteString(strings.Repeat(" ", labelWidth+4))
			b.WriteString(tuistyle.StatusFailed.Render(m.errors[i]))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(strings.Repeat(" ", max(0, labelWidth+4)))
	b.WriteString(m.renderStartButton())
	b.WriteString("\n\n")
	b.WriteString(tuistyle.ScreenMargin + tuistyle.HelpStyle.Render("tab/shift+tab navigate  enter start  esc cancel"))

	return b.String()
}

// Init satisfies tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

// InputCount returns the number of rendered inputs.
func (m *Model) InputCount() int { return len(m.inputs) }

// InputName returns the parameter name at index.
func (m *Model) InputName(index int) string { return m.entry.Params[index].Name }

// InputValue returns the current input value at index.
func (m *Model) InputValue(index int) string { return m.inputs[index].Value() }

// InputError returns the validation error for the input at index.
func (m *Model) InputError(index int) string { return m.errors[index] }

// FocusedIndex returns the focused input index, or -1 when the Start button is focused.
func (m *Model) FocusedIndex() int { return m.focused }

func (m *Model) focusNext() {
	if len(m.inputs) == 0 {
		m.focused = -1
		return
	}

	switch m.focused {
	case -1:
		m.focused = 0
	default:
		m.focused++
		if m.focused >= len(m.inputs) {
			m.focused = -1
		}
	}
	m.syncFocus()
}

func (m *Model) focusPrev() {
	if len(m.inputs) == 0 {
		m.focused = -1
		return
	}

	switch m.focused {
	case -1:
		m.focused = len(m.inputs) - 1
	case 0:
		m.focused = -1
	default:
		m.focused--
	}
	m.syncFocus()
}

func (m *Model) syncFocus() {
	for i := range m.inputs {
		if i == m.focused {
			m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
}

func (m *Model) submit() (tea.Model, tea.Cmd) {
	values := make(map[string]string, len(m.entry.Params))
	nextErrors := make([]string, len(m.entry.Params))
	hasErrors := false

	for i, param := range m.entry.Params {
		value := m.inputs[i].Value()
		values[param.Name] = value
		if param.IsRequired() && strings.TrimSpace(value) == "" {
			nextErrors[i] = param.Name + " is required"
			hasErrors = true
		}
	}

	m.errors = nextErrors
	if hasErrors {
		return m, nil
	}

	return m, func() tea.Msg { return SubmittedMsg(values) }
}

func (m *Model) renderInput(index, width int) string {
	input := m.inputs[index]
	input.Width = width

	borderStyle := tuistyle.DimStyle
	if index == m.focused {
		borderStyle = tuistyle.AccentStyle
	}

	content := input.View()
	padding := max(0, width-lipgloss.Width(content))
	return borderStyle.Render("│") + content + strings.Repeat(" ", padding) + borderStyle.Render("│")
}

func (m *Model) renderStartButton() string {
	style := tuistyle.AccentStyle
	if m.focused == -1 {
		style = style.Bold(true)
	}
	return style.Render("‹ Start ›")
}

func (m *Model) inputWidth(labelWidth int) int {
	width := m.width
	if width <= 0 {
		width = 80
	}

	available := width - labelWidth - 10
	if available < 20 {
		return 20
	}
	if available > 60 {
		return 60
	}
	return available
}
