// Package target decides which application a speaker's keypress is typed
// into.
//
// The default answer used to be "whichever window has focus", which is what
// the hardware does and so what a synthesised key does too. That is fine
// right up until the presenter wants to look at their own list of speakers:
// bringing it forward makes the terminal the focused window, and from then
// on every key a speaker sends is typed into a text interface that has no
// use for it while the slides sit still.
//
// So the presenter can instead name the application their slides are in, and
// nsp delivers there whatever is in front. Naming it is optional: left to
// itself, nsp watches which application the presenter uses and follows it,
// ignoring the one nsp was started from - which is the terminal, and the one
// place a slide key must never go.
package target

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/codingconcepts/next-slide-please/internal/desktop"
	"github.com/codingconcepts/next-slide-please/internal/keys"
)

// pollInterval is how often the front application is checked while nsp is
// following it. Reading the window list is cheap, and the only thing waiting
// on the answer is a presenter who has just switched to their slides, so this
// is tuned to feel immediate rather than to save anything.
const pollInterval = 500 * time.Millisecond

// Tracker is the presenter's answer to "where do the keys go". The zero value
// is not usable; call New.
type Tracker struct {
	// name is the application the presenter named, empty when nsp is
	// following them instead.
	name string

	// home is the application nsp was started from, which is the terminal the
	// list is drawn in. Never a target, however long it sits in front.
	home int32

	// How the windowing system is asked. Fields rather than direct calls so
	// that the rules above them - never target home, never forget on a bad
	// reading - can be tested on a machine with no windows at all.
	frontmost func() (desktop.App, error)
	find      func(name string) (desktop.App, error)

	mu  sync.Mutex
	app desktop.App
}

// New starts a tracker. A name pins the target to one application; an empty
// name follows the presenter instead.
//
// The application nsp was started from is noted here rather than later,
// because it is only reliably in front at the moment the presenter typed the
// command.
func New(name string) (*Tracker, error) {
	if !desktop.Supported() {
		return nil, desktop.ErrUnsupported
	}

	t := &Tracker{
		name:      strings.TrimSpace(name),
		frontmost: desktop.Frontmost,
		find:      desktop.Find,
	}

	// Not fatal: a presenter who started nsp from something without a window
	// - a launcher, a login shell on a tty - simply has no application to
	// exclude, and following the front one still works.
	if home, err := t.frontmost(); err == nil {
		t.home = home.PID
	}

	// Named targets resolve once up front, so a name that matches nothing is
	// reported before the talk rather than discovered during it.
	if t.name != "" {
		app, err := t.find(t.name)
		if err != nil {
			return nil, fmt.Errorf("finding %q: %w", t.name, err)
		}

		t.app = app
	}

	return t, nil
}

// Run follows the front application until ctx is cancelled. A tracker that is
// never run still works, on whatever it resolved at startup.
func (t *Tracker) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			t.poll()
		}
	}
}

// poll takes one reading. A failure leaves the last known target in place:
// the window list is momentarily empty often enough - during a space switch,
// or while an application is launching - that forgetting on every miss would
// drop keys for no reason.
func (t *Tracker) poll() {
	if t.name != "" {
		// A named application that has been quit and reopened comes back
		// under a new pid, so the name is re-resolved rather than trusted.
		if app, err := t.find(t.name); err == nil {
			t.set(app)
		}

		return
	}

	app, err := t.frontmost()
	if err != nil || app.PID == t.home {
		return
	}

	t.set(app)
}

func (t *Tracker) set(app desktop.App) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.app = app
}

// App is the application keys are currently going to, and whether there is
// one yet. The presenter's list shows this, because a presenter who cannot
// see where their keys are going has no way to tell a target that is wrong
// from slides that are not listening.
func (t *Tracker) App() (desktop.App, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.app, t.app.PID != 0
}

// Press is a present.Presser that types into the tracked application.
//
// Before anything has been resolved it falls back to the focused window,
// which is what nsp did before targeting existed. That window is quite
// possibly the presenter's own terminal, so this is a worse place for a key
// than the slides - but it is not a worse place than nowhere, and it is only
// reachable in the first half second of a talk or on a machine whose window
// list nsp cannot read.
func (t *Tracker) Press(key string) error {
	app, ok := t.App()
	if !ok {
		return keys.Press(key)
	}

	return keys.PressTo(app.PID, key)
}
