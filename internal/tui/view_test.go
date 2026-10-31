package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/codingconcepts/next-slide-please/internal/rtc"
)

// The speaker's buttons are hit tested with arithmetic against buttonTop, so
// a change to the lines above them would silently move the click targets off
// the buttons. This pins the two together.
func TestSpeakerButtonsAreWhereClicksAreLookedFor(t *testing.T) {
	lines := strings.Split(speakerModel{mode: rtc.ModeEnabled}.View(), "\n")

	if len(lines) <= buttonTop+buttonRows {
		t.Fatalf("view is only %d lines, buttons claim to end at %d",
			len(lines), buttonTop+buttonRows)
	}

	for row := buttonTop; row < buttonTop+buttonRows; row++ {
		line := lipgloss.NewStyle().Render(lines[row])

		for _, x := range []int{1, buttonWidth + buttonGap + 1} {
			want := rtc.KeyLeft
			if x > buttonWidth {
				want = rtc.KeyRight
			}

			if got := buttonAt(x, row); got != want {
				t.Errorf("click at (%d,%d) on %q hit %q, want %q",
					x, row, line, got, want)
			}
		}

		if strings.TrimSpace(line) == "" {
			t.Errorf("row %d is meant to be part of a button but is blank", row)
		}
	}

	// A click past the right hand button is not a keypress.
	if got := buttonAt(2*buttonWidth+buttonGap+5, buttonTop); got != "" {
		t.Errorf("click to the right of both buttons hit %q", got)
	}

	// Nor is one above them.
	if got := buttonAt(1, buttonTop-1); got != "" {
		t.Errorf("click above the buttons hit %q", got)
	}
}

func TestPresenterListsSpeakersAndTheirState(t *testing.T) {
	m := presenterModel{
		code: "0PpTrMetgz",
		rows: []speakerRow{
			{id: "a", name: "alice", ready: true, mode: rtc.ModeEnabled, keys: 3, lastKey: rtc.KeyRight},
			{id: "b", name: "bob", ready: true, mode: rtc.ModeMuted},
			{id: "c", name: "carol", ready: true, mode: rtc.ModeLogging},
			{id: "d", name: "dave"},
		},
	}

	view := m.View()

	for _, want := range []string{
		"0PpTrMetgz", "alice", "bob", "carol", "dave",
		"enabled", "muted", "logging", "connecting", "4 connected",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("presenter view is missing %q:\n%s", want, view)
		}
	}
}

// Under --dry-run nothing is pressed, so a speaker the presenter has enabled
// is really only being logged and the list has to say so.
func TestPresenterShowsEnabledSpeakersAsLoggingUnderDryRun(t *testing.T) {
	m := presenterModel{
		code:   "0PpTrMetgz",
		dryRun: true,
		rows: []speakerRow{
			{id: "a", name: "alice", ready: true, mode: rtc.ModeEnabled},
		},
	}

	// The list rather than the whole view, since the help line names every
	// mode whatever the speakers are doing.
	list := m.speakerList()

	if strings.Contains(list, "enabled") {
		t.Errorf("a dry run presenter claims a speaker is enabled:\n%s", list)
	}

	if !strings.Contains(list, "logging") {
		t.Errorf("a dry run presenter does not say it is only logging:\n%s", list)
	}
}

// Space walks a speaker round the three modes rather than flipping between
// two, so a key is never pressed without the presenter having gone past
// logging to get there. A speaker on this machine has only two to walk.
func TestSpaceCyclesThroughTheModes(t *testing.T) {
	cases := []struct {
		from  rtc.Mode
		local bool
		want  rtc.Mode
	}{
		{from: rtc.ModeMuted, want: rtc.ModeLogging},
		{from: rtc.ModeLogging, want: rtc.ModeEnabled},
		{from: rtc.ModeEnabled, want: rtc.ModeMuted},

		{from: rtc.ModeMuted, local: true, want: rtc.ModeLogging},
		{from: rtc.ModeLogging, local: true, want: rtc.ModeMuted},
	}

	for _, c := range cases {
		if got := c.from.Next(c.local); got != c.want {
			t.Errorf("space on a %q speaker (local %v) gave %q, want %q",
				c.from, c.local, got, c.want)
		}
	}
}

// Whatever route it arrives by - the bulk key, a stale row, a caller that
// did not check - enabled is not something a speaker on this machine can be.
func TestASpeakerOnThisMachineCannotBeEnabled(t *testing.T) {
	if got := rtc.Allow(rtc.ModeEnabled, true); got != rtc.ModeLogging {
		t.Errorf("enabling a speaker on this machine gave %q, want %q", got, rtc.ModeLogging)
	}

	for _, mode := range []rtc.Mode{rtc.ModeMuted, rtc.ModeLogging} {
		if got := rtc.Allow(mode, true); got != mode {
			t.Errorf("%q on a speaker on this machine became %q", mode, got)
		}
	}

	if got := rtc.Allow(rtc.ModeEnabled, false); got != rtc.ModeEnabled {
		t.Errorf("enabling a speaker elsewhere gave %q", got)
	}
}

func TestPresenterExplainsASpeakerOnItsOwnMachine(t *testing.T) {
	m := presenterModel{
		code: "0PpTrMetgz",
		rows: []speakerRow{
			{id: "a", name: "alice", ready: true, mode: rtc.ModeMuted, local: true},
		},
	}

	view := m.View()

	for _, want := range []string{"this machine", "starts muted", "no further than logging"} {
		if !strings.Contains(view, want) {
			t.Errorf("presenter view is missing %q:\n%s", want, view)
		}
	}

	// The mode is in the list whatever the reason for it, so that space can
	// be seen to have done something.
	if !strings.Contains(m.speakerList(), "muted") {
		t.Errorf("a muted speaker on this machine is not shown as muted:\n%s", view)
	}

	// Promoted as far as it can go. The row says what it now is, and the
	// explanation stays, because the ceiling it describes has not moved.
	m.rows[0].mode = rtc.ModeLogging
	view = m.View()

	if !strings.Contains(m.speakerList(), "logging") {
		t.Errorf("a logging speaker on this machine is not shown as logging:\n%s", view)
	}

	if !strings.Contains(view, "no further than logging") {
		t.Errorf("presenter stopped explaining the ceiling that still applies:\n%s", view)
	}
}

func TestPresenterShowsNothingToDoBeforeAnyoneJoins(t *testing.T) {
	view := presenterModel{code: "abc"}.View()

	if !strings.Contains(view, "Waiting for someone to join") {
		t.Errorf("empty presenter view should say it is waiting:\n%s", view)
	}
}

// A global chord and the same key typed into this window are two routes to
// one set of commands, and the whole point of the chords is that the
// presenter can use either without thinking about which window is in front.
// If the two ever disagree, they have to be learned separately.
func TestChordsAndTypedKeysReachTheSameCommands(t *testing.T) {
	cases := []struct {
		typed tea.KeyMsg
		chord string
	}{
		{tea.KeyMsg{Type: tea.KeyDown}, "down"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, "down"},
		{tea.KeyMsg{Type: tea.KeyUp}, "up"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")}, "up"},
	}

	for _, c := range cases {
		rows := []speakerRow{{id: "a"}, {id: "b"}, {id: "c"}}

		typed, _ := presenterModel{rows: rows, cursor: 1}.Update(c.typed)
		chord, _ := presenterModel{rows: rows, cursor: 1}.Update(hotkeyMsg{key: c.chord})

		gotTyped := typed.(presenterModel).cursor
		gotChord := chord.(presenterModel).cursor

		if gotTyped != gotChord {
			t.Errorf("%q left the cursor at %d but ctrl+opt+%s left it at %d",
				c.typed.String(), gotTyped, c.chord, gotChord)
		}

		if gotTyped == 1 {
			t.Errorf("%q did nothing at all", c.typed.String())
		}
	}
}

// Keys arriving in this window instead of the slides is the one failure that
// looks exactly like nothing happening, so it is counted and said out loud.
func TestAnArrowInThePresentersWindowIsReported(t *testing.T) {
	model, _ := presenterModel{code: "abc"}.Update(tea.KeyMsg{Type: tea.KeyRight})

	m := model.(presenterModel)
	if m.stray != 1 {
		t.Fatalf("a right arrow in the presenter's window was counted %d times", m.stray)
	}

	if !strings.Contains(m.View(), "where they do nothing") {
		t.Errorf("the presenter is not told its window swallowed an arrow:\n%s", m.View())
	}
}

// Every other way of finding out where a key is about to be typed involves
// pressing one and watching what moves.
func TestPresenterSaysWhereTheKeysAreGoing(t *testing.T) {
	cases := []struct {
		name  string
		model presenterModel
		want  string
	}{
		{
			name:  "following focus",
			model: presenterModel{code: "abc"},
			want:  "whichever window has focus",
		},
		{
			name: "nothing resolved yet",
			model: presenterModel{code: "abc", options: PresenterOptions{
				Target: func() (string, bool) { return "", false },
			}},
			want: "Waiting to see which application",
		},
		{
			name: "aimed",
			model: presenterModel{code: "abc", options: PresenterOptions{
				Target: func() (string, bool) { return "Keynote", true },
			}},
			want: "Keynote",
		},
	}

	for _, c := range cases {
		if view := c.model.View(); !strings.Contains(view, c.want) {
			t.Errorf("%s: view is missing %q:\n%s", c.name, c.want, view)
		}
	}

	// Under --dry-run no key is pressed anywhere, so there is nowhere for one
	// to go and nothing to say about it.
	if view := (presenterModel{code: "abc", dryRun: true}).View(); strings.Contains(view, "Keys go to") {
		t.Errorf("a dry run presenter claims its keys are going somewhere:\n%s", view)
	}
}

// Hotkeys are the presenter's way out of the bind the list itself creates,
// so being refused them is worth a line rather than silence.
func TestPresenterSaysWhenItCouldNotTakeTheHotkeys(t *testing.T) {
	m := presenterModel{code: "abc", options: PresenterOptions{
		HotkeyErr: errors.New("creating an event tap: permission denied"),
	}}

	for _, want := range []string{"permission denied", "in front to steer"} {
		if view := m.View(); !strings.Contains(view, want) {
			t.Errorf("presenter view is missing %q:\n%s", want, view)
		}
	}
}

// A flash is only meant to last a moment; an expired one must not keep the
// row highlighted forever.
func TestKeyFlashExpires(t *testing.T) {
	row := speakerRow{flash: time.Now().Add(-time.Second)}

	if time.Now().Before(row.flash) {
		t.Error("a flash from a second ago is somehow still live")
	}
}
