package machine

import "testing"

func TestFingerprintIsStableAndOpaque(t *testing.T) {
	first := Fingerprint()
	if first == "" {
		t.Skip("this machine cannot name itself")
	}

	if second := Fingerprint(); second != first {
		t.Fatalf("fingerprint changed between calls: %q then %q", first, second)
	}

	if len(first) != 32 {
		t.Fatalf("fingerprint is %d characters, the Worker only relays 32", len(first))
	}
}

func TestSameAs(t *testing.T) {
	if !SameAs(Fingerprint()) && Fingerprint() != "" {
		t.Error("this machine does not recognise itself")
	}

	if SameAs("") {
		t.Error("an unidentified machine must not match this one")
	}

	if SameAs("0123456789abcdef0123456789abcdef") {
		t.Error("another machine's fingerprint matched this one")
	}
}
