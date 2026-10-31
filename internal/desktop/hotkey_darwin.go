package desktop

import (
	"context"
	"fmt"
	"runtime"

	"github.com/ebitengine/purego"
)

// Event tap constants. The session tap is early enough to take a key before
// the focused application sees it, which is what lets a chord nsp owns be
// swallowed rather than also typed into whatever is in front.
const (
	sessionEventTap  = 1
	headInsertTap    = 0
	tapOptionDefault = 0

	eventKeyDown              = 10
	maskKeyDown               = 1 << eventKeyDown
	tapDisabledByTimeout      = 0xFFFFFFFE
	tapDisabledByUserInput    = 0xFFFFFFFF
	keyboardEventKeycodeField = 9
)

// Modifier flags, and the subset of them nsp looks at. Caps lock, the
// numeric pad bit and the function bit are deliberately outside the mask:
// macOS sets the last two on the arrow keys, so a chord that insisted on
// their absence would never match.
const (
	flagShift     = 1 << 17
	flagControl   = 1 << 18
	flagAlternate = 1 << 19
	flagCommand   = 1 << 20

	modifierMask = flagShift | flagControl | flagAlternate | flagCommand

	// Modifiers is the combination every nsp hotkey carries: control and
	// option together, held with nothing else. Chosen because macOS and the
	// applications presenters actually use have left it alone.
	chordModifiers = flagControl | flagAlternate
)

// keycodes are macOS virtual keycodes, which are positional and have nothing
// to do with the character a key produces. Only the keys the presenter's list
// binds are here; asking for anything else is a programming error rather than
// something to handle at runtime.
var keycodes = map[string]int64{
	"a":     0,
	"l":     37,
	"n":     45,
	"space": 49,
	"left":  123,
	"right": 124,
	"down":  125,
	"up":    126,
}

// Listen reports the named keys whenever they are pressed with control and
// option held, from any application, and stops them reaching the application
// in front. Every other keystroke on the machine passes through untouched.
//
// The returned channel is closed when ctx is cancelled. It is buffered and
// lossy: the callback this feeds runs inside the window server's event path,
// where a send that blocked would stall every keystroke on the machine, so a
// chord that arrives with the buffer full is dropped rather than queued.
//
// This needs the same Accessibility permission as pressing a key, and fails
// with ErrDenied when it has not been granted.
func Listen(ctx context.Context, keys []string) (<-chan string, error) {
	if err := load(); err != nil {
		return nil, err
	}

	wanted := make(map[int64]string, len(keys))

	for _, key := range keys {
		code, ok := keycodes[key]
		if !ok {
			return nil, fmt.Errorf("no keycode for %q", key)
		}

		wanted[code] = key
	}

	out := make(chan string, 16)
	ready := make(chan error, 1)

	go listen(ctx, wanted, out, ready)

	if err := <-ready; err != nil {
		return nil, err
	}

	return out, nil
}

// listen owns the run loop the event tap is attached to. A CFRunLoop belongs
// to the thread that asked for it, so this thread is pinned for as long as
// the tap is running and does nothing else.
func listen(ctx context.Context, wanted map[int64]string, out chan<- string, ready chan<- error) {
	runtime.LockOSThread()

	defer runtime.UnlockOSThread()
	defer close(out)

	// Assigned below, but captured here: the callback is one of the arguments
	// used to create the very port it needs to re-enable.
	var port uintptr

	callback := purego.NewCallback(func(proxy uintptr, eventType uint32, event uintptr, userInfo uintptr) uintptr {
		switch eventType {
		// macOS switches a tap off if its callback ever takes too long, and
		// says so through the callback itself. Turning it back on is the only
		// way a talk survives a laptop that stalled under load.
		case tapDisabledByTimeout, tapDisabledByUserInput:
			cgEventTapEnable(port, true)
			return event

		case eventKeyDown:

		default:
			return event
		}

		if cgEventGetFlags(event)&modifierMask != chordModifiers {
			return event
		}

		key, ok := wanted[cgEventGetIntegerValueField(event, keyboardEventKeycodeField)]
		if !ok {
			return event
		}

		select {
		case out <- key:
		default:
		}

		// Swallowed, so the chord does not also land in the slides.
		return 0
	})

	port = cgEventTapCreate(sessionEventTap, headInsertTap, tapOptionDefault, maskKeyDown, callback, 0)
	if port == 0 {
		ready <- fmt.Errorf("creating an event tap: %w", ErrDenied)
		return
	}
	defer cfRelease(port)

	source := cfMachPortCreateRunLoopSource(0, port, 0)
	if source == 0 {
		ready <- fmt.Errorf("attaching the event tap to a run loop")
		return
	}
	defer cfRelease(source)

	runLoop := cfRunLoopGetCurrent()
	cfRunLoopAddSource(runLoop, source, runLoopDefaultMode)
	cgEventTapEnable(port, true)

	ready <- nil

	// CFRunLoopStop is safe from another thread, and is the only thing that
	// brings CFRunLoopRun back.
	stopped := make(chan struct{})
	defer close(stopped)

	go func() {
		select {
		case <-ctx.Done():
			cfRunLoopStop(runLoop)
		case <-stopped:
		}
	}()

	cfRunLoopRun()
}
