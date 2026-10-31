package target

import (
	"fmt"
	"testing"

	"github.com/codingconcepts/next-slide-please/internal/desktop"
)

// The one application a slide key must never be typed into is the terminal
// the presenter's list is drawn in, and the presenter brings that to the
// front every time they want to look at it. Following it would turn every
// glance at the list into a stuck slide.
func TestTheApplicationNspWasStartedFromIsNeverFollowed(t *testing.T) {
	terminal := desktop.App{PID: 1, Name: "Terminal"}
	keynote := desktop.App{PID: 2, Name: "Keynote"}

	front := keynote
	tracker := &Tracker{
		home:      terminal.PID,
		frontmost: func() (desktop.App, error) { return front, nil },
	}

	tracker.poll()

	if app, _ := tracker.App(); app != keynote {
		t.Fatalf("following the front application gave %+v, want %+v", app, keynote)
	}

	// The presenter looks at their list, then looks away again.
	front = terminal

	tracker.poll()

	if app, _ := tracker.App(); app != keynote {
		t.Errorf("looking at the list moved the target to %+v", app)
	}

	front = keynote

	tracker.poll()

	if app, _ := tracker.App(); app != keynote {
		t.Errorf("after looking away the target is %+v, want %+v", app, keynote)
	}
}

// The window list comes back empty often enough - mid space switch, or while
// something is launching - that treating one bad reading as "no target" would
// drop a slide key for no reason.
func TestABadReadingLeavesTheLastTargetAlone(t *testing.T) {
	keynote := desktop.App{PID: 2, Name: "Keynote"}

	var err error

	tracker := &Tracker{
		frontmost: func() (desktop.App, error) { return keynote, err },
	}

	tracker.poll()

	err = fmt.Errorf("no application has a window on screen")

	tracker.poll()

	app, ok := tracker.App()
	if !ok || app != keynote {
		t.Errorf("a failed reading left the target as %+v (ok %v), want %+v", app, ok, keynote)
	}
}

// An application that's  quit and reopened comes back under a new pid, and a
// presenter who named it meant the application rather than that process.
func TestANamedApplicationIsResolvedAgainEveryPoll(t *testing.T) {
	pid := int32(2)
	tracker := &Tracker{
		name: "Keynote",
		find: func(string) (desktop.App, error) {
			return desktop.App{PID: pid, Name: "Keynote"}, nil
		},
		// Set, and deliberately wrong, to catch a named tracker that follows
		// focus anyway.
		frontmost: func() (desktop.App, error) {
			return desktop.App{PID: 99, Name: "Terminal"}, nil
		},
	}

	tracker.poll()

	if app, _ := tracker.App(); app.PID != 2 {
		t.Fatalf("a named tracker resolved to %+v", app)
	}

	pid = 3

	tracker.poll()

	if app, _ := tracker.App(); app.PID != 3 {
		t.Errorf("a named tracker held on to the old pid: %+v", app)
	}
}

// Before anything has been resolved there is no pid to deliver to, and the
// caller has to be told so rather than being handed a zero that would be
// taken for "whichever window has focus" without saying so.
func TestAnUnresolvedTrackerHasNoApplication(t *testing.T) {
	if app, ok := (&Tracker{}).App(); ok {
		t.Errorf("an unresolved tracker claims to be aiming at %+v", app)
	}
}
