//go:build !darwin

package keys

import (
	"fmt"
	"runtime"
)

const supported = false

// press is the stub every platform that has no implementation yet falls back
// to. The rest of nsp builds and runs on these platforms - a speaker needs
// nothing from this package - so the gap is only reached if someone tries to
// present from one.
//
// Windows wants SendInput through user32.dll, and Linux wants /dev/uinput.
// Neither needs cgo, so both can slot in here without changing how nsp is
// built.
func pressTo(int32, string) error {
	return fmt.Errorf("pressing keys is not implemented on %s yet", runtime.GOOS)
}
