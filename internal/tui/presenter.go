package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codingconcepts/next-slide-please/internal/present"
	"github.com/codingconcepts/next-slide-please/internal/rtc"
)

// flashDuration is how long a speaker's row lights up when a key arrives
// from it. Long enough to catch out of the corner of an eye mid-sentence.
const flashDuration = 250 * time.Millisecond

// nameWidth is the speaker name column, wide enough for a name plus the
// note that marks a speaker on this machine.
const nameWidth = 28

type speakerRow struct {
	id      string
	name    string
	ready   bool
	mode    rtc.Mode
	local   bool
	keys    int
	lastKey string
	flash   time.Time
}

type presenterModel struct {
	session *present.Session
	code    string
	dryRun  bool

	rows     []speakerRow
	cursor   int
	err      error
	quitting bool
}

// NewPresenter builds the presenter's program. The session is already
// listening on its code by the time this is called, so the code can be shown
// immediately rather than after a round trip.
func NewPresenter(session *present.Session, dryRun bool) *tea.Program {
	return tea.NewProgram(presenterModel{
		session: session,
		code:    session.Code(),
		dryRun:  dryRun,
	})
}

func (m presenterModel) Init() tea.Cmd {
	return waitFor(m.session.Events())
}

func (m presenterModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.onKey(msg)

	case flashMsg:
		// Nothing to change: the redraw this triggers is the point, since
		// View compares each row's flash against the current time.
		return m, nil

	case eventMsg:
		return m.onEvent(msg.event)

	case closedMsg:
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

func (m presenterModel) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		m.quitting = true
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}

	// Space walks one speaker round muted, logging, enabled. Driving a talk
	// is therefore always something the presenter did on purpose, and the
	// stop on the way shows the keys arriving before they can move a slide.
	case " ", "enter":
		if row := m.current(); row != nil {
			m.setMode(m.cursor, row.mode.Next(row.local))
		}

	// Bulk settings, for the moment a demo starts or a heckler appears.
	case "a":
		m.setAll(rtc.ModeEnabled)

	case "l":
		m.setAll(rtc.ModeLogging)

	case "n":
		m.setAll(rtc.ModeMuted)
	}

	return m, nil
}

func (m presenterModel) onEvent(event present.Event) (tea.Model, tea.Cmd) {
	next := waitFor(m.session.Events())

	switch event := event.(type) {
	case present.Joined:
		// A speaker that reconnects arrives under a new id, so this is always
		// an addition rather than an update.
		m.rows = append(m.rows, speakerRow{
			id:    event.ID,
			name:  event.Name,
			mode:  event.Mode,
			local: event.Local,
		})

	case present.Ready:
		if i := m.indexOf(event.ID); i >= 0 {
			m.rows[i].ready = true
		}

	case present.Left:
		if i := m.indexOf(event.ID); i >= 0 {
			m.rows = append(m.rows[:i], m.rows[i+1:]...)
			if m.cursor >= len(m.rows) && m.cursor > 0 {
				m.cursor--
			}
		}

	case present.Key:
		// Counted whatever became of it. A muted speaker still mashing the
		// arrow keys is worth seeing, and under logging the count is the
		// whole point of the mode.
		if i := m.indexOf(event.ID); i >= 0 {
			m.rows[i].lastKey = event.Key
			m.rows[i].keys++
			m.rows[i].flash = time.Now().Add(flashDuration)
		}

		return m, tea.Batch(next, flashAfter())

	case present.Failed:
		m.err = event.Err
	}

	return m, next
}

func (m presenterModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render("next slide please"))
	b.WriteString("\n\n")

	b.WriteString(labelStyle.Render("Share this code"))
	b.WriteString("\n")
	b.WriteString(codeStyle.Render(m.code))
	b.WriteString("\n\n")

	if m.dryRun {
		b.WriteString(labelStyle.Render("Dry run: every key is logged, none is pressed"))
		b.WriteString("\n\n")
	}

	b.WriteString(panelStyle.Render(m.speakerList()))
	b.WriteString("\n")

	if m.hasLocal() {
		b.WriteString(labelStyle.Render(
			"A speaker on this machine starts muted and goes no further than logging: " +
				"a key pressed for it would land in its own window and come straight back.",
		))
		b.WriteString("\n")
	}

	if m.err != nil {
		b.WriteString(badStyle.Render(m.err.Error()))
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render(
		"↑/↓ select · space muted/logging/enabled · a enable all · l log all · n mute all · q quit",
	))

	return b.String() + "\n"
}

func (m presenterModel) speakerList() string {
	if len(m.rows) == 0 {
		return labelStyle.Render("Waiting for someone to join...")
	}

	now := time.Now()
	lines := make([]string, 0, len(m.rows)+1)
	lines = append(lines, labelStyle.Render(
		fmt.Sprintf("%d connected", len(m.rows)),
	))

	for i, row := range m.rows {
		cursor := "  "
		name := row.name

		// Marked on the name rather than taking the state column, so the
		// row can say both what this speaker is and why it will not go
		// past logging.
		if row.local {
			name += " (this machine)"
		}

		if i == m.cursor {
			cursor = selectedStyle.Render("\u203a ")
			name = selectedStyle.Render(name)
		}

		// The state column is always the mode, including for a speaker on
		// this machine: being on this machine is why that speaker is muted,
		// not a state of its own, and a column that hid the mode behind the
		// reason left no way to see what space had just done.
		//
		// Under --dry-run an enabled speaker is shown as logging, because
		// that is all that is happening to its keys: the list should say
		// what nsp is doing, not what it would do with a presser.
		var state string

		switch {
		case !row.ready:
			state = labelStyle.Render("connecting")
		case row.mode == rtc.ModeEnabled && !m.dryRun:
			state = goodStyle.Render("enabled")
		case row.mode == rtc.ModeEnabled, row.mode == rtc.ModeLogging:
			state = labelStyle.Render("logging")
		default:
			state = badStyle.Render("muted")
		}

		activity := labelStyle.Render("-")
		if row.keys > 0 {
			activity = fmt.Sprintf("%s x%d", arrow(row.lastKey), row.keys)
			if now.Before(row.flash) {
				activity = selectedStyle.Render(activity)
			} else {
				activity = labelStyle.Render(activity)
			}
		}

		lines = append(lines, fmt.Sprintf(
			"%s%s  %s  %s",
			cursor,
			lipgloss.NewStyle().Width(nameWidth).Render(name),
			lipgloss.NewStyle().Width(12).Render(state),
			activity,
		))
	}

	return strings.Join(lines, "\n")
}

func (m presenterModel) current() *speakerRow {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}

	return &m.rows[m.cursor]
}

// hasLocal reports whether any speaker is sitting on this machine, which is
// the only time the explanation is worth the line it takes up. Unlike the
// old warning this stays while that speaker is logging, because the ceiling
// it describes is still in force.
func (m presenterModel) hasLocal() bool {
	for _, row := range m.rows {
		if row.local {
			return true
		}
	}

	return false
}

// setMode moves one speaker and tells the session, so the row and the
// speaker's own display never disagree about what is happening. The session
// applies the same limit to a speaker on this machine; applying it here too
// is what keeps the row honest about the mode that was actually taken.
func (m presenterModel) setMode(i int, mode rtc.Mode) {
	mode = rtc.Allow(mode, m.rows[i].local)

	m.rows[i].mode = mode
	m.session.SetMode(m.rows[i].id, mode)
}

func (m presenterModel) setAll(mode rtc.Mode) {
	for i := range m.rows {
		m.setMode(i, mode)
	}
}

func (m presenterModel) indexOf(id string) int {
	for i, row := range m.rows {
		if row.id == id {
			return i
		}
	}

	return -1
}

func arrow(key string) string {
	if key == "left" {
		return "←"
	}

	return "→"
}
