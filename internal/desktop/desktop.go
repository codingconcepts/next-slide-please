// Package desktop is what nsp knows about the windowing system it is
// presenting into: which application is in front, and which keys the
// presenter has pressed anywhere on the machine.
//
// Both answers exist for the same reason. A key nsp synthesises goes to
// whichever window has focus, so the presenter's own list is unusable while a
// talk is running: bringing it to the front is exactly what stops the slides
// receiving anything. Knowing which application the slides are in means the
// keys can be delivered there instead, and a hotkey that works from any
// window means the list can be steered without bringing it to the front at
// all.
//
// Like internal/keys, nothing here uses cgo, and every platform without an
// implementation gets a stub that says so rather than a build failure.
package desktop

import "errors"

// ErrUnsupported is returned by everything in this package on a platform it
// has not been written for. It is not fatal: a presenter that cannot find the
// front window falls back to typing into whatever has focus, which is all nsp
// ever did.
var ErrUnsupported = errors.New("not supported on this platform")

// ErrDenied means the operating system refused, which on macOS means
// Accessibility permission has not been granted to the terminal nsp is
// running in. Worth telling apart from ErrUnsupported, because this one the
// presenter can fix.
var ErrDenied = errors.New("permission denied")

// App is a running application with a window on screen.
type App struct {
	PID  int32
	Name string
}

// Supported reports whether this build can see the front window and listen
// for hotkeys, so the presenter can be told at startup rather than finding
// out when neither works.
func Supported() bool {
	return supported
}
