// Package machine identifies the computer nsp is running on, so a presenter
// can tell whether a speaker is sitting on the same one.
//
// That matters because the presenter drives slides by synthesising a key
// press, and a synthesised key lands in whichever window has focus. If that
// window is a speaker's terminal on the same machine, the speaker reads the
// key as input and sends it straight back: the two then feed each other
// without anyone touching the keyboard.
package machine

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// Fingerprint is a stable, opaque name for this machine, or "" if the
// machine cannot be identified.
//
// It is the hostname, hashed. Hashing is not for secrecy - a presenter could
// be told the hostname and nothing bad would follow - but there is no reason
// to put someone's machine name on the wire, and a fixed width value is
// easier for the signalling server to validate.
//
// Two machines that share a hostname look like one here. The cost of that is
// a remote speaker arriving muted with the presenter told why, which is a
// keystroke to undo, so the trade is worth it against a feedback loop that
// takes a talk down.
func Fingerprint() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(host))

	return hex.EncodeToString(sum[:16])
}

// SameAs reports whether fingerprint belongs to this machine. An empty
// fingerprint belongs to nothing: a machine that could not identify itself
// must not be mistaken for every other machine that could not either.
func SameAs(fingerprint string) bool {
	if fingerprint == "" {
		return false
	}

	return fingerprint == Fingerprint()
}
