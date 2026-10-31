//go:build !darwin

package desktop

import "context"

const supported = false

// The stubs every platform without an implementation falls back to. A
// presenter on one of these cannot press keys either, so nothing here is on
// the path of a working talk - but the presenter half still has to build and
// run, because --dry-run works everywhere.
//
// Windows wants EnumWindows and GetForegroundWindow for the first two and a
// WH_KEYBOARD_LL hook for the third; Linux wants whatever its compositor
// offers, which under Wayland may be nothing. Neither needs cgo, so both can
// slot in here without changing how nsp is built.

func Frontmost() (App, error) {
	return App{}, ErrUnsupported
}

func Find(string) (App, error) {
	return App{}, ErrUnsupported
}

func Listen(context.Context, []string) (<-chan string, error) {
	return nil, ErrUnsupported
}
