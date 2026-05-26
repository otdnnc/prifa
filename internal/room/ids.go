package room

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns an n-byte random hex token. It panics only if the OS RNG
// is unavailable, which is treated as fatal.
func NewID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("room: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// newID is the internal alias used by package types.
func newID(n int) string { return NewID(n) }
