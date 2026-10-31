package id

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const (
	hexLen       = 16
	legacyHexLen = 8
)

func New(prefix string) string {
	b := make([]byte, hexLen/2)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}

func Valid(prefix, s string) bool {
	rest, ok := strings.CutPrefix(s, prefix+"_")
	if !ok || len(rest) != hexLen && len(rest) != legacyHexLen {
		return false
	}
	for _, c := range rest {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
