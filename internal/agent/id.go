package agent

import (
	"crypto/rand"
	"encoding/hex"
)

func NewID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "_fallback"
	}
	return prefix + "_" + hex.EncodeToString(buf)
}
