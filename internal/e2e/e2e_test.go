// Package e2e exercises a presenter and several speakers against a real
// signalling server, without a terminal in the way. It is skipped unless
// NSP_SIGNAL_SERVER points at one:
//
//	(cd signal && npm run dev)
//	NSP_SIGNAL_SERVER=http://localhost:8787 go test ./internal/e2e/...
package e2e

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/codingconcepts/next-slide-please/internal/present"
	"github.com/codingconcepts/next-slide-please/internal/rtc"
	"github.com/codingconcepts/next-slide-please/internal/speak"
)

const settle = 15 * time.Second

func server(t *testing.T) string {
	t.Helper()

	server := os.Getenv("NSP_SIGNAL_SERVER")
	if server == "" {
		t.Skip("set NSP_SIGNAL_SERVER to run end to end tests")
	}

	return server
}

// presses records what the presenter was asked to type, standing in for the
// keyboard.
type presses struct {
	mu   sync.Mutex
	keys []string
}

func (p *presses) press(key string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.keys = append(p.keys, key)

	return nil
}

func (p *presses) all() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]string(nil), p.keys...)
}

// watcher keeps the presenter's events so assertions can wait on them,
// because the TUI that normally consumes them is not running here.
type watcher struct {
	mu     sync.Mutex
	events []present.Event
}

func watch(session *present.Session) *watcher {
	w := &watcher{}

	go func() {
		for event := range session.Events() {
			w.mu.Lock()
			w.events = append(w.events, event)
			w.mu.Unlock()
		}
	}()

	return w
}

// idOf returns the presenter's id for the speaker that joined under name,
// once that speaker's data channel is open.
func (w *watcher) idOf(t *testing.T, name string) string {
	t.Helper()

	var id string

	waitFor(t, fmt.Sprintf("speaker %q to be ready", name), func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()

		var candidate string

		for _, event := range w.events {
			if joined, ok := event.(present.Joined); ok && joined.Name == name {
				candidate = joined.ID
			}
		}

		for _, event := range w.events {
			if ready, ok := event.(present.Ready); ok && ready.ID == candidate {
				id = candidate
				return true
			}
		}

		return false
	})

	return id
}

// joinedOf returns the join event for name, which carries the presenter's
// verdict on whether that speaker is sitting on its own machine.
func (w *watcher) joinedOf(t *testing.T, name string) present.Joined {
	t.Helper()

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, event := range w.events {
		if joined, ok := event.(present.Joined); ok && joined.Name == name {
			return joined
		}
	}

	t.Fatalf("no speaker joined under the name %q", name)

	return present.Joined{}
}

func TestSeveralSpeakersDriveOnePresenter(t *testing.T) {
	server := server(t)

	ctx := t.Context()

	keys := &presses{}
	code := fmt.Sprintf("test-%d", time.Now().UnixNano())

	// Both speakers are in this process, so they are on the presenter's
	// machine and could never be enabled. This test is about everything
	// else, so it says they are elsewhere; the rule itself is covered by
	// TestASpeakerOnThePresentersMachineArrivesMuted.
	session, err := present.New(ctx, server, code, keys.press,
		present.WithSameMachine(func(string) bool { return false }))
	if err != nil {
		t.Fatalf("starting presenter: %v", err)
	}

	watcher := watch(session)
	go session.Run(ctx)

	alice, err := speak.Connect(ctx, server, code, "alice")
	if err != nil {
		t.Fatalf("connecting alice: %v", err)
	}
	defer alice.Close()

	bob, err := speak.Connect(ctx, server, code, "bob")
	if err != nil {
		t.Fatalf("connecting bob: %v", err)
	}
	defer bob.Close()

	aliceID := watcher.idOf(t, "alice")
	bobID := watcher.idOf(t, "bob")

	// Speakers arrive logging, so the presenter has to promote each of them
	// the way it would at the start of a talk.
	session.SetMode(aliceID, rtc.ModeEnabled)
	session.SetMode(bobID, rtc.ModeEnabled)

	waitFor(t, "both speakers to be told they can drive", func() bool {
		return modeOf(t, alice) == rtc.ModeEnabled && modeOf(t, bob) == rtc.ModeEnabled
	})

	if err = alice.Send(rtc.KeyRight); err != nil {
		t.Fatalf("alice sending: %v", err)
	}

	if err = bob.Send(rtc.KeyLeft); err != nil {
		t.Fatalf("bob sending: %v", err)
	}

	waitFor(t, "both keys to be pressed", func() bool {
		return len(keys.all()) == 2
	})

	// Muting alice drops her keys and leaves bob driving.
	session.SetMode(aliceID, rtc.ModeMuted)

	waitFor(t, "alice to be told she is muted", func() bool {
		return modeOf(t, alice) == rtc.ModeMuted
	})

	if err = alice.Send(rtc.KeyRight); err != nil {
		t.Fatalf("alice sending while muted: %v", err)
	}

	if err = bob.Send(rtc.KeyRight); err != nil {
		t.Fatalf("bob sending: %v", err)
	}

	waitFor(t, "bob's key to be pressed", func() bool {
		return len(keys.all()) == 3
	})

	// Give alice's dropped key every chance to show up before asserting that
	// it did not.
	time.Sleep(500 * time.Millisecond)

	if got := keys.all(); len(got) != 3 {
		t.Fatalf("expected a muted speaker's key to be dropped, pressed %v", got)
	}

	// And unmuting puts her back.
	session.SetMode(aliceID, rtc.ModeEnabled)

	waitFor(t, "alice to be told she can drive again", func() bool {
		return modeOf(t, alice) == rtc.ModeEnabled
	})

	if err = alice.Send(rtc.KeyLeft); err != nil {
		t.Fatalf("alice sending after unmute: %v", err)
	}

	waitFor(t, "alice's key to be pressed", func() bool {
		return len(keys.all()) == 4
	})
}

// A speaker on the presenter's own machine is the one case where a key
// cannot be pressed safely: it would land in whichever window has focus,
// which may well be that speaker's, and come back as another keypress. So it
// arrives muted and the presenter is told why.
func TestASpeakerOnThePresentersMachineArrivesMuted(t *testing.T) {
	server := server(t)

	ctx := t.Context()

	keys := &presses{}
	code := fmt.Sprintf("local-%d", time.Now().UnixNano())

	session, err := present.New(ctx, server, code, keys.press)
	if err != nil {
		t.Fatalf("starting presenter: %v", err)
	}

	watcher := watch(session)
	go session.Run(ctx)

	alice, err := speak.Connect(ctx, server, code, "alice")
	if err != nil {
		t.Fatalf("connecting alice: %v", err)
	}
	defer alice.Close()

	watcher.idOf(t, "alice")

	if !watcher.joinedOf(t, "alice").Local {
		t.Fatal("a speaker in the same process was not recognised as being on the same machine")
	}

	if got := watcher.joinedOf(t, "alice").Mode; got != rtc.ModeMuted {
		t.Fatalf("a speaker on the presenter's machine arrived %q, want %q", got, rtc.ModeMuted)
	}

	waitFor(t, "alice to be told she is muted", func() bool {
		return modeOf(t, alice) == rtc.ModeMuted
	})

	if err = alice.Send(rtc.KeyRight); err != nil {
		t.Fatalf("alice sending: %v", err)
	}

	// Give the key every chance to arrive before asserting that it was not
	// pressed.
	time.Sleep(500 * time.Millisecond)

	if got := keys.all(); len(got) != 0 {
		t.Fatalf("a speaker on the presenter's machine drove the slides, pressed %v", got)
	}

	// Asking for enabled anyway gets logging. The session holds that line
	// itself, so no caller can hand this speaker the keyboard.
	session.SetMode(watcher.joinedOf(t, "alice").ID, rtc.ModeEnabled)

	waitFor(t, "alice to be told she is being logged", func() bool {
		return modeOf(t, alice) == rtc.ModeLogging
	})

	if err = alice.Send(rtc.KeyLeft); err != nil {
		t.Fatalf("alice sending after being enabled: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	if got := keys.all(); len(got) != 0 {
		t.Fatalf("a speaker on the presenter's machine was enabled after all, pressed %v", got)
	}
}

// Two things at once, because they share a presenter: a speaker arrives
// logging rather than enabled, and under --dry-run it stays logging however
// far the presenter promotes it, since nothing is being pressed.
func TestADryRunPresenterOnlyEverLogs(t *testing.T) {
	server := server(t)

	ctx := t.Context()

	code := fmt.Sprintf("dry-%d", time.Now().UnixNano())

	session, err := present.New(ctx, server, code, nil)
	if err != nil {
		t.Fatalf("starting presenter: %v", err)
	}

	watcher := watch(session)
	go session.Run(ctx)

	alice, err := speak.Connect(ctx, server, code, "alice")
	if err != nil {
		t.Fatalf("connecting alice: %v", err)
	}
	defer alice.Close()

	id := watcher.idOf(t, "alice")

	joined := watcher.joinedOf(t, "alice")
	if joined.Local {
		t.Fatal("a dry run presenter muted a speaker it could not possibly feed back to")
	}

	if joined.Mode != rtc.ModeLogging {
		t.Fatalf("a speaker arrived %q, want %q", joined.Mode, rtc.ModeLogging)
	}

	// Enabled as far as the presenter is concerned, but there is no presser
	// behind it, so the speaker is told the truth instead.
	session.SetMode(id, rtc.ModeEnabled)

	waitFor(t, "alice to be told she is only being logged", func() bool {
		return modeOf(t, alice) == rtc.ModeLogging
	})

	if err = alice.Send(rtc.KeyRight); err != nil {
		t.Fatalf("alice sending: %v", err)
	}

	waitFor(t, "alice's key to be reported as logged", func() bool {
		watcher.mu.Lock()
		defer watcher.mu.Unlock()

		for _, event := range watcher.events {
			if key, ok := event.(present.Key); ok && key.Mode == rtc.ModeLogging {
				return true
			}
		}

		return false
	})
}

func TestSpeakerWithoutAPresenterIsRejected(t *testing.T) {
	server := server(t)

	ctx, cancel := context.WithTimeout(context.Background(), settle)
	defer cancel()

	code := fmt.Sprintf("empty-%d", time.Now().UnixNano())

	if _, err := speak.Connect(ctx, server, code, "nobody"); err == nil {
		t.Fatal("expected joining a code with no presenter to fail")
	}
}

// modeOf drains whatever the presenter has told this speaker and reports the
// latest verdict.
func modeOf(t *testing.T, session *speak.Session) rtc.Mode {
	t.Helper()

	mode := lastMode[session]

	for {
		select {
		case event := <-session.Events():
			if told, ok := event.(speak.Mode); ok {
				mode = told.Mode
			}
		default:
			lastMode[session] = mode
			return mode
		}
	}
}

// lastMode remembers the most recent verdict per speaker, since reading the
// channel consumes it.
var lastMode = map[*speak.Session]rtc.Mode{}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()

	deadline := time.Now().Add(settle)
	for time.Now().Before(deadline) {
		if done() {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}
