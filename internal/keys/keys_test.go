package keys

import "testing"

// The peer on the other end of the data channel is whoever holds the code, so
// everything that reaches this package is checked before anything is
// synthesised. Both entry points, because a rule enforced on one of two doors
// is not enforced.
//
// Only the refusals are exercised here: a test that got as far as pressing
// something would type an arrow key into whatever the person running it had
// in front of them.
func TestNothingButTheTwoKeysIsEverPressed(t *testing.T) {
	for _, key := range []string{"up", "down", "a", "", "LEFT", "left left"} {
		if err := Press(key); err == nil {
			t.Errorf("Press(%q) was allowed", key)
		}

		if err := PressTo(1, key); err == nil {
			t.Errorf("PressTo(1, %q) was allowed", key)
		}
	}
}

// A negative pid is not a process, and the platform call below takes one
// without complaining.
func TestAKeyIsNotPressedIntoANonsensePID(t *testing.T) {
	if err := PressTo(-1, Right); err == nil {
		t.Error("pressing into pid -1 was allowed")
	}
}
