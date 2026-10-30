// Package keys synthesises the arrow keypresses a presenter's slides react
// to. It is the only part of nsp that touches the machine it runs on, and
// the only part that is platform specific.
//
// Nothing here uses cgo. The platform files reach the operating system
// through whatever dynamic loading it already offers, so nsp builds with
// CGO_ENABLED=0 and cross compiles to any target from any machine.
package keys

import "fmt"

// The two keys nsp exists to carry, named as they arrive over the data
// channel. Deliberately not imported from the rtc package: this package is
// about a keyboard and should not know that a network exists.
const (
	Left  = "left"
	Right = "right"
)

// Press taps a key on this machine. It refuses anything other than the two
// keys nsp carries. That rule is enforced here as well as on the way in,
// because the peer is whoever holds the code and a data channel is not a
// licence to synthesise arbitrary input.
func Press(key string) error {
	switch key {
	case Left, Right:
		return press(key)
	default:
		return fmt.Errorf("refusing to press unexpected key %q", key)
	}
}

// Supported reports whether this build can press keys at all, so the
// presenter can say so at startup rather than failing on the first keypress
// of a talk.
func Supported() bool {
	return supported
}
