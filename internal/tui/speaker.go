package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codingconcepts/next-slide-please/internal/rtc"
	"github.com/codingconcepts/next-slide-please/internal/speak"
)

// The buttons are laid out at fixed coordinates rather than centred, so a
// click can be matched to a button with arithmetic instead of a layout
// engine. Everything above them is a fixed number of lines, which is what
// makes buttonTop a constant.
const (
	buttonWidth = 18
	buttonGap   = 2
	buttonTop   = 4
	buttonRows  = 3
)

type speakerModel struct {
	session *speak.Session

	mode     rtc.Mode
	sent     int
	lastKey  string
	flash    time.Time
	err      error
	quitting bool
}

// NewSpeaker builds the speaker's program. Mouse reporting is on so the
// buttons can be clicked as well as driven from the keyboard.
func NewSpeaker(session *speak.Session) *tea.Program {
	// Starts logging rather than enabled, because that is how a speaker
	// arrives at the presenter. The presenter's first verdict overwrites
	// this within a moment of the channel opening.
	return tea.NewProgram(
		speakerModel{session: session, mode: rtc.ModeLogging},
		tea.WithMouseCellMotion(),
	)
}

func (m speakerModel) Init() tea.Cmd {
	return waitFor(m.session.Events())
}

func (m speakerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quitting = true
			return m, tea.Quit

		case "left", "h":
			return m.send(rtc.KeyLeft)

		case "right", "l", " ":
			return m.send(rtc.KeyRight)
		}

	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
			return m, nil
		}

		if key := buttonAt(msg.X, msg.Y); key != "" {
			return m.send(key)
		}

	case flashMsg:
		return m, nil

	case eventMsg:
		return m.onEvent(msg.event)

	case closedMsg:
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

func (m speakerModel) onEvent(event any) (tea.Model, tea.Cmd) {
	next := waitFor(m.session.Events())

	switch event := event.(type) {
	case speak.Mode:
		m.mode = event.Mode

	case speak.Closed:
		m.err = event.Err
		m.quitting = true

		return m, tea.Quit
	}

	return m, next
}

func (m speakerModel) send(key string) (tea.Model, tea.Cmd) {
	if err := m.session.Send(key); err != nil {
		m.err = err
		return m, nil
	}

	m.sent++
	m.lastKey = key
	m.flash = time.Now().Add(flashDuration)

	return m, flashAfter()
}

func (m speakerModel) View() string {
	if m.quitting {
		if m.err != nil {
			return badStyle.Render(m.err.Error()) + "\n"
		}

		return ""
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render("next slide please"))
	b.WriteString("\n\n")
	b.WriteString(m.status())
	b.WriteString("\n\n")
	b.WriteString(m.buttons())
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(badStyle.Render(m.err.Error()))
	} else if m.sent > 0 {
		b.WriteString(labelStyle.Render("sent " + arrow(m.lastKey)))
	} else {
		b.WriteString(labelStyle.Render("-"))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("←/→ or click · q quit"))

	return b.String() + "\n"
}

func (m speakerModel) status() string {
	switch m.mode {
	case rtc.ModeEnabled:
		return goodStyle.Render("● connected") + labelStyle.Render(" · you can drive the slides")

	case rtc.ModeLogging:
		return labelStyle.Render("● logging") +
			labelStyle.Render(" · the presenter can see your keys but is not pressing them yet")

	default:
		return badStyle.Render("● muted") + labelStyle.Render(" · the presenter has turned your keys off")
	}
}

func (m speakerModel) buttons() string {
	active := time.Now().Before(m.flash)

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.button("←  previous", active && m.lastKey == rtc.KeyLeft),
		strings.Repeat(" ", buttonGap),
		m.button("next  →", active && m.lastKey == rtc.KeyRight),
	)
}

func (m speakerModel) button(label string, lit bool) string {
	style := panelStyle.
		Width(buttonWidth - 2).
		Align(lipgloss.Center)

	// A muted speaker's buttons still work locally; it is the presenter that
	// drops the key. Showing them greyed would suggest otherwise.
	if lit {
		style = style.BorderForeground(accent).Foreground(accent).Bold(true)
	}

	return style.Render(label)
}

// buttonAt maps a click to a key, or to "" if it missed both buttons.
func buttonAt(x, y int) string {
	if y < buttonTop || y >= buttonTop+buttonRows {
		return ""
	}

	switch {
	case x >= 0 && x < buttonWidth:
		return rtc.KeyLeft
	case x >= buttonWidth+buttonGap && x < 2*buttonWidth+buttonGap:
		return rtc.KeyRight
	}

	return ""
}
