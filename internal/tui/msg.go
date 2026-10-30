package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// eventMsg carries one session event into the bubbletea loop. Both halves of
// nsp publish their events as `any`, so one pump serves both.
type eventMsg struct{ event any }

// closedMsg means the session's event channel has been closed and there is
// nothing left to draw.
type closedMsg struct{}

// flashMsg forces a redraw once a row's highlight has expired.
type flashMsg struct{}

// waitFor pulls the next event off a session channel. bubbletea runs each
// command on its own goroutine, so blocking here is the intended way to wait.
// Every handler that consumes an eventMsg must return this again, or the pump
// stops.
func waitFor[T any](ch <-chan T) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return closedMsg{}
		}

		return eventMsg{event: event}
	}
}

func flashAfter() tea.Cmd {
	return tea.Tick(flashDuration, func(time.Time) tea.Msg {
		return flashMsg{}
	})
}
