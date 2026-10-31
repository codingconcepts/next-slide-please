package keys

import (
	"fmt"
	"sync"
	"time"

	"github.com/ebitengine/purego"
)

const supported = true

// CoreGraphics is part of every macOS install, so it is loaded by path at
// first use rather than linked. This is what keeps the build free of cgo:
// purego resolves the symbols at runtime.
const coreGraphics = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"

// macOS virtual keycodes, which are positional and have nothing to do with
// the character a key produces.
const (
	keyCodeLeft  = 123
	keyCodeRight = 124
)

// kCGHIDEventTap posts the event as though it came from the hardware, which
// is what gets it to whichever application currently has focus.
const hidEventTap = 0

var (
	loadOnce sync.Once
	loadErr  error

	createKeyboardEvent func(source uintptr, keycode uint16, keyDown bool) uintptr
	setFlags            func(event uintptr, flags uint64)
	postEvent           func(tap uint32, event uintptr)
	postEventToPID      func(pid int32, event uintptr)
	release             func(ptr uintptr)

	// Serialises the down/up pair. Several speakers can be enabled at once,
	// so without this two simultaneous presses could interleave into
	// down(left), down(right), up(left), up(right) and leave a key stuck.
	pressMu sync.Mutex
)

// keyUpDelay gives the receiving application a moment to notice the key went
// down before it comes back up. Some apps miss a tap posted with no gap.
const keyUpDelay = 8 * time.Millisecond

func load() error {
	loadOnce.Do(func() {
		lib, err := purego.Dlopen(coreGraphics, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("loading CoreGraphics: %w", err)
			return
		}

		purego.RegisterLibFunc(&createKeyboardEvent, lib, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&setFlags, lib, "CGEventSetFlags")
		purego.RegisterLibFunc(&postEvent, lib, "CGEventPost")
		purego.RegisterLibFunc(&postEventToPID, lib, "CGEventPostToPid")
		purego.RegisterLibFunc(&release, lib, "CFRelease")
	})

	return loadErr
}

func pressTo(pid int32, key string) error {
	if err := load(); err != nil {
		return err
	}

	keycode := uint16(keyCodeRight)
	if key == Left {
		keycode = keyCodeLeft
	}

	pressMu.Lock()
	defer pressMu.Unlock()

	if err := post(pid, keycode, true); err != nil {
		return err
	}

	time.Sleep(keyUpDelay)

	return post(pid, keycode, false)
}

// post builds a single key event and sends it. The event is created with a
// nil source, which inherits the current modifier state - so the flags are
// cleared explicitly. Otherwise a presenter who happens to be holding cmd
// would turn a speaker's right arrow into cmd+right.
//
// A pid of zero posts to the HID tap, which is the hardware's own path and
// so lands in whichever window has focus. Any other pid delivers to that
// process alone, whether or not it is in front - which is what lets the
// presenter bring their own list forward without the slides going deaf.
func post(pid int32, keycode uint16, down bool) error {
	event := createKeyboardEvent(0, keycode, down)
	if event == 0 {
		return fmt.Errorf("creating keyboard event for keycode %d", keycode)
	}
	defer release(event)

	setFlags(event, 0)

	if pid == 0 {
		postEvent(hidEventTap, event)
	} else {
		postEventToPID(pid, event)
	}

	return nil
}
