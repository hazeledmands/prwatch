package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type model struct {
	events []string
	width  int
	height int
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyPressMsg:
		if msg.String() == "q" {
			return m, tea.Quit
		}
	case tea.MouseClickMsg:
		m.events = append(m.events, fmt.Sprintf("CLICK x=%d y=%d btn=%v", msg.X, msg.Y, msg.Button))
	case tea.MouseReleaseMsg:
		m.events = append(m.events, fmt.Sprintf("RELEASE x=%d y=%d", msg.X, msg.Y))
	case tea.MouseMotionMsg:
		m.events = append(m.events, fmt.Sprintf("MOTION x=%d y=%d", msg.X, msg.Y))
	case tea.MouseWheelMsg:
		m.events = append(m.events, fmt.Sprintf("WHEEL x=%d y=%d btn=%v", msg.X, msg.Y, msg.Button))
	}
	// Keep last 20 events
	if len(m.events) > 20 {
		m.events = m.events[len(m.events)-20:]
	}
	return m, nil
}

func (m model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion

	var b strings.Builder
	b.WriteString("Mouse Event Tester (press q to quit)\n\n")
	for _, e := range m.events {
		b.WriteString(e + "\n")
	}
	v.SetContent(b.String())
	return v
}

func main() {
	p := tea.NewProgram(model{})
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
